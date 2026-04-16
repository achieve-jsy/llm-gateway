package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestExpansionStateMachineTransitions verifies the state machine transitions
// defined in model.NextExpansionStatus.
func TestExpansionStateMachineTransitions(t *testing.T) {
	tests := []struct {
		name        string
		from        string
		action      string
		wantStatus  string
		shouldError bool
	}{
		{
			name:       "pending + approve -> approved",
			from:       model.ExpansionStatusPending,
			action:     model.ExpansionActionApprove,
			wantStatus: model.ExpansionStatusApproved,
		},
		{
			name:       "pending + reject -> rejected",
			from:       model.ExpansionStatusPending,
			action:     model.ExpansionActionReject,
			wantStatus: model.ExpansionStatusRejected,
		},
		{
			name:       "pending + cancel -> cancelled",
			from:       model.ExpansionStatusPending,
			action:     model.ExpansionActionCancel,
			wantStatus: model.ExpansionStatusCancelled,
		},
		{
			name:       "pending + escalate -> pending (level changes)",
			from:       model.ExpansionStatusPending,
			action:     model.ExpansionActionEscalate,
			wantStatus: model.ExpansionStatusPending,
		},
		{
			name:       "pending + require_reprecheck -> precheck_required",
			from:       model.ExpansionStatusPending,
			action:     model.ExpansionActionReprecheckRequired,
			wantStatus: model.ExpansionStatusPrecheckRequired,
		},
		{
			name:       "precheck_required + confirm_precheck -> pending",
			from:       model.ExpansionStatusPrecheckRequired,
			action:     model.ExpansionActionConfirmPrecheck,
			wantStatus: model.ExpansionStatusPending,
		},
		{
			name:        "precheck_required + approve -> error",
			from:        model.ExpansionStatusPrecheckRequired,
			action:      model.ExpansionActionApprove,
			shouldError: true,
		},
		{
			name:        "approved + cancel -> error",
			from:        model.ExpansionStatusApproved,
			action:      model.ExpansionActionCancel,
			shouldError: true,
		},
		{
			name:        "rejected + approve -> error",
			from:        model.ExpansionStatusRejected,
			action:      model.ExpansionActionApprove,
			shouldError: true,
		},
		{
			name:        "expired + approve -> error",
			from:        model.ExpansionStatusExpired,
			action:      model.ExpansionActionApprove,
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := model.NextExpansionStatus(tt.from, tt.action)
			if tt.shouldError {
				require.Error(t, err, "expected error for transition %s + %s", tt.from, tt.action)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.wantStatus, got)
			}
		})
	}
}

// TestExpansionPrecheckVersionChangesWhenBudgetStateChanges verifies that
// the PrecheckVersion (SHA-256 hash) changes when the budget state changes.
func TestExpansionPrecheckVersionChangesWhenBudgetStateChanges(t *testing.T) {
	db, svc := newExpansionApprovalFixture(t)

	memberID, studioID := uuid.NewString(), uuid.NewString()
	require.NoError(t, db.Exec(`INSERT INTO studios(id,name) VALUES (?,?)`, studioID, "Studio").Error)
	require.NoError(t, db.Exec(`INSERT INTO departments(id,dept_id,dept_name,studio_id) VALUES ('row-dept','dept-a','Dept',?)`, studioID).Error)
	require.NoError(t, db.Exec(`INSERT INTO users(id,dept_id) VALUES (?,'row-dept')`, memberID).Error)

	for _, row := range []struct {
		scopeType, scopeID string
		amount             float64
	}{
		{model.BudgetScopeMember, memberID, 50},
		{model.BudgetScopeDeptDaily, "dept-a", 80},
		{model.BudgetScopeStudio, studioID, 100},
		{model.BudgetScopeCompany, model.BudgetScopeGlobal, 1000},
	} {
		require.NoError(t, db.Exec(`INSERT INTO budget_allocations
			(id,scope_type,scope_id,amount_usd,daily_amount_usd,weekly_amount_usd,period,enabled)
			VALUES (?,?,?,?,?,?,?,?)`, uuid.New(), row.scopeType, row.scopeID, row.amount, -1, -1, "monthly", true).Error)
	}

	// First precheck
	result1, err := svc.Precheck(context.Background(), service.ActorScope{
		UserID: memberID, Role: "user",
	}, service.PrecheckInput{
		ScopeType: model.BudgetScopeMember, ScopeID: memberID, PeriodType: "monthly",
		DimensionType: "total", DurationType: "permanent", TargetAmountUSD: "100.000000",
	})
	require.NoError(t, err)

	// Change the budget
	require.NoError(t, db.Exec(`UPDATE budget_allocations SET amount_usd = 60 WHERE scope_id = ?`, memberID).Error)

	// Second precheck
	result2, err := svc.Precheck(context.Background(), service.ActorScope{
		UserID: memberID, Role: "user",
	}, service.PrecheckInput{
		ScopeType: model.BudgetScopeMember, ScopeID: memberID, PeriodType: "monthly",
		DimensionType: "total", DurationType: "permanent", TargetAmountUSD: "100.000000",
	})
	require.NoError(t, err)

	require.NotEqual(t, result1.PrecheckVersion, result2.PrecheckVersion,
		"precheck version should change when budget state changes")
}

// TestExpansionApproveWithStalePrecheckReturnsError verifies that approval
// fails with ErrPrecheckStale when the budget state has changed since precheck.
func TestExpansionApproveWithStalePrecheckReturnsError(t *testing.T) {
	db, svc := newExpansionApprovalFixture(t)

	requestID, requesterID, approverID, allocationID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	seedApprovalChain(t, db, requesterID)
	require.NoError(t, db.Exec(`INSERT INTO budget_allocations
		(id,scope_type,scope_id,amount_usd,daily_amount_usd,weekly_amount_usd,period,enabled,created_by,updated_by)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		allocationID, model.BudgetScopeMember, requesterID.String(), 80, -1, -1, "monthly", true,
		requesterID, requesterID).Error)

	expiresAt := time.Now().Add(time.Hour)
	require.NoError(t, db.Create(&model.ExpansionRequest{
		ID: requestID, ScopeType: model.BudgetScopeMember, ScopeID: requesterID.String(),
		RequesterID: requesterID, PeriodType: string(budgetdomain.PeriodMonthly),
		DimensionType: budgetdomain.DimensionTotal, DimensionKey: "total",
		DurationType: budgetdomain.DurationPermanent, RequestedAmount: 120,
		PrecheckVersion: "old-version", ExpiresAt: &expiresAt,
		Reason: "increase total", Status: model.ExpansionStatusPending, CurrentLevel: model.ExpansionLevelDept,
	}).Error)

	// Try to approve with old precheck version
	_, err := svc.DecideWorkflow(context.Background(), requestID.String(), approverID, service.DecisionInput{
		Action: model.ExpansionActionApprove, PrecheckVersion: "old-version",
	})
	require.ErrorIs(t, err, service.ErrPrecheckStale)

	// Verify request is now in precheck_required state
	var request model.ExpansionRequest
	require.NoError(t, db.First(&request, "id = ?", requestID).Error)
	require.Equal(t, model.ExpansionStatusPrecheckRequired, request.Status)
	require.NotEqual(t, "old-version", request.PrecheckVersion)
}

// TestExpansionRejectRequiresComment verifies that reject action requires a comment.
func TestExpansionRejectRequiresComment(t *testing.T) {
	db, svc := newExpansionApprovalFixture(t)

	requestID, requesterID, approverID := uuid.New(), uuid.New(), uuid.New()
	seedApprovalChain(t, db, requesterID)
	require.NoError(t, db.Exec(`INSERT INTO budget_allocations
		(id,scope_type,scope_id,amount_usd,daily_amount_usd,weekly_amount_usd,period,enabled)
		VALUES (?,?,?,?,?,?,?,?)`,
		uuid.New(), model.BudgetScopeMember, requesterID.String(), 80, -1, -1, "monthly", true).Error)

	expiresAt := time.Now().Add(time.Hour)
	require.NoError(t, db.Create(&model.ExpansionRequest{
		ID: requestID, ScopeType: model.BudgetScopeMember, ScopeID: requesterID.String(),
		RequesterID: requesterID, PeriodType: "monthly", DimensionType: budgetdomain.DimensionTotal,
		DimensionKey: "total", DurationType: budgetdomain.DurationPermanent, RequestedAmount: 120,
		PrecheckVersion: "version-1", ExpiresAt: &expiresAt, Reason: "increase",
		Status: model.ExpansionStatusPending, CurrentLevel: model.ExpansionLevelDept,
	}).Error)

	// Try to reject without comment
	_, err := svc.DecideWorkflow(context.Background(), requestID.String(), approverID, service.DecisionInput{
		Action: model.ExpansionActionReject, Comment: "", PrecheckVersion: "version-1",
	})
	require.ErrorIs(t, err, service.ErrDecisionCommentRequired)
}

// TestExpansionEscalationMovesToNextLevel verifies that escalation moves
// the request to the next approval level (dept -> studio -> platform).
func TestExpansionEscalationMovesToNextLevel(t *testing.T) {
	db, svc := newExpansionApprovalFixture(t)

	requestID, requesterID, approverID, studioID := uuid.New(), uuid.New(), uuid.New(), uuid.NewString()
	require.NoError(t, db.Exec(`INSERT INTO studios(id,name) VALUES (?,?)`, studioID, "Studio").Error)
	require.NoError(t, db.Exec(`INSERT INTO departments(id,dept_id,dept_name,studio_id) VALUES ('row-dept','dept-a','Dept',?)`, studioID).Error)
	require.NoError(t, db.Exec(`INSERT INTO users(id,dept_id) VALUES (?,'row-dept')`, requesterID).Error)

	for _, row := range []struct {
		scopeType, scopeID string
		amount             float64
	}{
		{model.BudgetScopeDeptDaily, "dept-a", 80},
		{model.BudgetScopeStudio, studioID, 100},
		{model.BudgetScopeCompany, model.BudgetScopeGlobal, 1000},
	} {
		require.NoError(t, db.Exec(`INSERT INTO budget_allocations
			(id,scope_type,scope_id,amount_usd,daily_amount_usd,weekly_amount_usd,period,enabled)
			VALUES (?,?,?,?,?,?,?,?)`, uuid.New(), row.scopeType, row.scopeID, row.amount, -1, -1, "monthly", true).Error)
	}

	// Get fresh precheck
	precheck, err := svc.Precheck(context.Background(), service.ActorScope{Role: "admin", IsGlobal: true}, service.PrecheckInput{
		ScopeType: model.BudgetScopeDeptDaily, ScopeID: "dept-a", PeriodType: "monthly",
		DimensionType: "total", DurationType: "permanent", TargetAmountUSD: "120.000000",
	})
	require.NoError(t, err)

	expiresAt := time.Now().Add(time.Hour)
	require.NoError(t, db.Create(&model.ExpansionRequest{
		ID: requestID, ScopeType: model.BudgetScopeDeptDaily, ScopeID: "dept-a",
		RequesterID: requesterID, PeriodType: "monthly", DimensionType: budgetdomain.DimensionTotal,
		DimensionKey: "total", DurationType: budgetdomain.DurationPermanent, RequestedAmount: 120,
		PrecheckVersion: precheck.PrecheckVersion, ExpiresAt: &expiresAt, Reason: "increase",
		Status: model.ExpansionStatusPending, CurrentLevel: model.ExpansionLevelStudio,
	}).Error)

	// Escalate
	_, err = svc.DecideWorkflow(context.Background(), requestID.String(), approverID, service.DecisionInput{
		Action: model.ExpansionActionEscalate, Comment: "needs platform approval", PrecheckVersion: precheck.PrecheckVersion,
	})
	require.NoError(t, err)

	// Verify level changed to platform
	var request model.ExpansionRequest
	require.NoError(t, db.First(&request, "id = ?", requestID).Error)
	require.Equal(t, model.ExpansionStatusPending, request.Status)
	require.Equal(t, model.ExpansionLevelPlatform, request.CurrentLevel)
}

// TestExpansionDuplicatePendingDetection verifies that creating a duplicate
// pending expansion request is rejected.
func TestExpansionDuplicatePendingDetection(t *testing.T) {
	db, svc := newExpansionApprovalFixture(t)

	requesterID := uuid.New()
	require.NoError(t, db.Exec(`INSERT INTO budget_allocations
		(id,scope_type,scope_id,amount_usd,daily_amount_usd,weekly_amount_usd,period,enabled)
		VALUES (?,?,?,?,?,?,?,?)`,
		uuid.New(), model.BudgetScopeMember, requesterID.String(), 80, -1, -1, "monthly", true).Error)

	// Create first request
	_, err := svc.CreateRequestAdvanced(context.Background(), service.CreateExpansionRequestInput{
		ScopeType: model.BudgetScopeMember, ScopeID: requesterID.String(), RequesterID: requesterID,
		PeriodType: budgetdomain.PeriodMonthly, DimensionType: budgetdomain.DimensionTotal,
		DurationType: budgetdomain.DurationPermanent, RequestedAmount: 120, Reason: "first request",
	})
	require.NoError(t, err)

	// Try to create duplicate
	_, err = svc.CreateRequestAdvanced(context.Background(), service.CreateExpansionRequestInput{
		ScopeType: model.BudgetScopeMember, ScopeID: requesterID.String(), RequesterID: requesterID,
		PeriodType: budgetdomain.PeriodMonthly, DimensionType: budgetdomain.DimensionTotal,
		DurationType: budgetdomain.DurationPermanent, RequestedAmount: 130, Reason: "duplicate",
	})
	require.ErrorIs(t, err, service.ErrDuplicatePendingExpansion)
}

// TestExpansionApproveCreatesTotalPermanentModification verifies that approving
// a total+permanent request creates a BudgetAllocation update.
func TestExpansionApproveCreatesTotalPermanentModification(t *testing.T) {
	db, svc := newExpansionApprovalFixture(t)

	requestID, requesterID, approverID, allocationID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	seedApprovalChain(t, db, requesterID)
	require.NoError(t, db.Exec(`INSERT INTO budget_allocations
		(id,scope_type,scope_id,amount_usd,daily_amount_usd,weekly_amount_usd,period,enabled,created_by,updated_by)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		allocationID.String(), model.BudgetScopeMember, requesterID.String(), 80, -1, -1, "monthly", true,
		requesterID.String(), requesterID.String()).Error)

	// Get fresh precheck
	precheck, err := svc.Precheck(context.Background(), service.ActorScope{Role: "admin", IsGlobal: true}, service.PrecheckInput{
		ScopeType: model.BudgetScopeMember, ScopeID: requesterID.String(), PeriodType: "monthly",
		DimensionType: "total", DurationType: "permanent", TargetAmountUSD: "120.000000",
	})
	require.NoError(t, err)

	expiresAt := time.Now().Add(time.Hour)
	require.NoError(t, db.Create(&model.ExpansionRequest{
		ID: requestID, ScopeType: model.BudgetScopeMember, ScopeID: requesterID.String(),
		RequesterID: requesterID, PeriodType: string(budgetdomain.PeriodMonthly),
		DimensionType: budgetdomain.DimensionTotal, DimensionKey: "total",
		DurationType: budgetdomain.DurationPermanent, RequestedAmount: 120,
		PrecheckVersion: precheck.PrecheckVersion, ExpiresAt: &expiresAt, Reason: "increase",
		Status: model.ExpansionStatusPending, CurrentLevel: model.ExpansionLevelDept,
	}).Error)

	// Approve
	result, err := svc.DecideWorkflow(context.Background(), requestID.String(), approverID, service.DecisionInput{
		Action: model.ExpansionActionApprove, Comment: "approved", PrecheckVersion: precheck.PrecheckVersion,
	})
	require.NoError(t, err)
	require.Equal(t, "total_base_limit", result.TargetType)

	// Verify allocation was updated
	var allocation model.BudgetAllocation
	require.NoError(t, db.First(&allocation, "id = ?", allocationID.String()).Error)
	require.Equal(t, 120.0, allocation.AmountUSD)

	// Verify request status
	var request model.ExpansionRequest
	require.NoError(t, db.First(&request, "id = ?", requestID.String()).Error)
	require.Equal(t, model.ExpansionStatusApproved, request.Status)
}

func TestExpansionApproveCreatesMissingMemberAllocation(t *testing.T) {
	db, svc := newExpansionApprovalFixture(t)

	requestID, requesterID, approverID := uuid.New(), uuid.New(), uuid.New()
	seedApprovalChain(t, db, requesterID)
	expiresAt := time.Now().Add(time.Hour)
	require.NoError(t, db.Create(&model.ExpansionRequest{
		ID: requestID, ScopeType: model.BudgetScopeMember, ScopeID: requesterID.String(),
		RequesterID: requesterID, PeriodType: string(budgetdomain.PeriodMonthly),
		DimensionType: budgetdomain.DimensionTotal, DimensionKey: "total",
		DurationType: budgetdomain.DurationPermanent, RequestedAmount: 120,
		ExpiresAt: &expiresAt, Reason: "increase without existing allocation",
		Status: model.ExpansionStatusPending, CurrentLevel: model.ExpansionLevelDept,
	}).Error)
	precheck, err := svc.Precheck(context.Background(), service.ActorScope{Role: "admin", IsGlobal: true}, service.PrecheckInput{
		ScopeType: model.BudgetScopeMember, ScopeID: requesterID.String(), PeriodType: "monthly",
		DimensionType: "total", DurationType: "permanent", TargetAmountUSD: "120.000000",
	})
	require.NoError(t, err)
	require.True(t, precheck.CanSubmit)
	require.NoError(t, db.Model(&model.ExpansionRequest{}).Where("id = ?", requestID).Update("precheck_version", precheck.PrecheckVersion).Error)

	result, err := svc.DecideWorkflow(context.Background(), requestID.String(), approverID, service.DecisionInput{
		Action: model.ExpansionActionApprove, Comment: "approved", PrecheckVersion: precheck.PrecheckVersion,
	})
	require.NoError(t, err)
	require.Equal(t, "total_base_limit", result.TargetType)

	var allocation model.BudgetAllocation
	require.NoError(t, db.First(&allocation, "scope_type = ? AND scope_id = ?", model.BudgetScopeMember, requesterID.String()).Error)
	require.Equal(t, 120.0, allocation.AmountUSD)
	var request model.ExpansionRequest
	require.NoError(t, db.First(&request, "id = ?", requestID.String()).Error)
	require.Equal(t, model.ExpansionStatusApproved, request.Status)
}

func TestExpansionApproveCreatesTotalTemporaryOverride(t *testing.T) {
	db, svc := newExpansionApprovalFixture(t)

	requestID, requesterID, approverID, allocationID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	seedApprovalChain(t, db, requesterID)
	require.NoError(t, db.Exec(`INSERT INTO budget_allocations
		(id,scope_type,scope_id,amount_usd,daily_amount_usd,weekly_amount_usd,period,enabled,created_by,updated_by)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		allocationID.String(), model.BudgetScopeMember, requesterID.String(), 80, -1, -1, "monthly", true,
		requesterID.String(), requesterID.String()).Error)

	precheck, err := svc.Precheck(context.Background(), service.ActorScope{Role: "admin", IsGlobal: true}, service.PrecheckInput{
		ScopeType: model.BudgetScopeMember, ScopeID: requesterID.String(), PeriodType: "monthly",
		DimensionType: "total", DurationType: "current_period", TargetAmountUSD: "120.000000",
	})
	require.NoError(t, err)
	require.True(t, precheck.CanSubmit)

	expiresAt := time.Now().Add(time.Hour)
	require.NoError(t, db.Create(&model.ExpansionRequest{
		ID: requestID, ScopeType: model.BudgetScopeMember, ScopeID: requesterID.String(),
		RequesterID: requesterID, PeriodType: string(budgetdomain.PeriodMonthly),
		DimensionType: budgetdomain.DimensionTotal, DimensionKey: "total",
		DurationType: budgetdomain.DurationCurrentPeriod, RequestedAmount: 120,
		PrecheckVersion: precheck.PrecheckVersion, ExpiresAt: &expiresAt, Reason: "temporary increase",
		Status: model.ExpansionStatusPending, CurrentLevel: model.ExpansionLevelDept,
	}).Error)

	result, err := svc.DecideWorkflow(context.Background(), requestID.String(), approverID, service.DecisionInput{
		Action: model.ExpansionActionApprove, Comment: "approved", PrecheckVersion: precheck.PrecheckVersion,
	})
	require.NoError(t, err)
	require.Equal(t, "temporary_override", result.TargetType)

	var overrideCount int64
	require.NoError(t, db.Table("budget_temporary_overrides").Count(&overrideCount).Error)
	require.Equal(t, int64(1), overrideCount)

	var override model.BudgetTemporaryOverride
	require.NoError(t, db.First(&override, "id = ?", result.TargetID.String()).Error)
	require.Equal(t, allocationID, override.BudgetAllocationID)
	require.Equal(t, budgetdomain.DimensionTotal, override.DimensionType)
	require.Equal(t, "total", override.DimensionKey)
	require.Equal(t, budgetdomain.PeriodMonthly, override.PeriodType)
	require.Equal(t, "120.000000", override.TargetLimitUSD.String())
	require.Equal(t, budgetdomain.OverrideActive, override.Status)
}

func TestExpansionApproveCreatesTierPermanentLimit(t *testing.T) {
	db, svc := newExpansionApprovalFixture(t)

	requestID, requesterID, approverID, allocationID, tierID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	seedApprovalChain(t, db, requesterID)
	require.NoError(t, db.Exec(`INSERT INTO budget_allocations
		(id,scope_type,scope_id,amount_usd,daily_amount_usd,weekly_amount_usd,period,enabled,created_by,updated_by)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		allocationID.String(), model.BudgetScopeMember, requesterID.String(), 300, -1, -1, "monthly", true,
		requesterID.String(), requesterID.String()).Error)
	require.NoError(t, db.Exec(`INSERT INTO model_tiers(id,name,description,color,sort_order,enabled,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?)`, tierID.String(), "高级", "", "#8b5cf6", 1, true, time.Now(), time.Now()).Error)

	tierIDString := tierID.String()
	precheck, err := svc.Precheck(context.Background(), service.ActorScope{Role: "admin", IsGlobal: true}, service.PrecheckInput{
		ScopeType: model.BudgetScopeMember, ScopeID: requesterID.String(), PeriodType: "monthly",
		DimensionType: "tier", ModelTierID: &tierIDString, DurationType: "permanent", TargetAmountUSD: "120.000000",
	})
	require.NoError(t, err)
	require.True(t, precheck.CanSubmit)

	expiresAt := time.Now().Add(time.Hour)
	require.NoError(t, db.Create(&model.ExpansionRequest{
		ID: requestID, ScopeType: model.BudgetScopeMember, ScopeID: requesterID.String(),
		RequesterID: requesterID, PeriodType: string(budgetdomain.PeriodMonthly),
		DimensionType: budgetdomain.DimensionTier, DimensionKey: "tier:" + tierID.String(), ModelTierID: &tierID,
		DurationType: budgetdomain.DurationPermanent, RequestedAmount: 120,
		PrecheckVersion: precheck.PrecheckVersion, ExpiresAt: &expiresAt, Reason: "increase tier",
		Status: model.ExpansionStatusPending, CurrentLevel: model.ExpansionLevelDept,
	}).Error)

	result, err := svc.DecideWorkflow(context.Background(), requestID.String(), approverID, service.DecisionInput{
		Action: model.ExpansionActionApprove, Comment: "approved", PrecheckVersion: precheck.PrecheckVersion,
	})
	require.NoError(t, err)
	require.Equal(t, "tier_base_limit", result.TargetType)

	var limit struct{ LimitUSD string }
	require.NoError(t, db.Table("budget_tier_limits").Select("limit_usd").
		Where("budget_allocation_id = ? AND model_tier_id = ? AND period_type = ?", allocationID.String(), tierID.String(), "monthly").
		Scan(&limit).Error)
	parsed, err := budgetdomain.ParseAmount(limit.LimitUSD)
	require.NoError(t, err)
	require.Equal(t, "120.000000", parsed.String())
}

// TestExpansionApprovalHierarchy verifies that the approval hierarchy
// (dept -> studio -> platform) is respected.
func TestExpansionApprovalHierarchy(t *testing.T) {
	tests := []struct {
		name          string
		scopeType     string
		expectedLevel string
	}{
		{
			name:          "member scope starts at dept level",
			scopeType:     model.BudgetScopeMember,
			expectedLevel: model.ExpansionLevelDept,
		},
		{
			name:          "dept scope starts at studio level",
			scopeType:     model.BudgetScopeDeptDaily,
			expectedLevel: model.ExpansionLevelStudio,
		},
		{
			name:          "studio scope starts at platform level",
			scopeType:     model.BudgetScopeStudio,
			expectedLevel: model.ExpansionLevelPlatform,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, svc := newExpansionApprovalFixture(t)

			requesterID := uuid.New()
			request, err := svc.CreateRequestAdvanced(context.Background(), service.CreateExpansionRequestInput{
				ScopeType: tt.scopeType, ScopeID: uuid.NewString(), RequesterID: requesterID,
				PeriodType: budgetdomain.PeriodMonthly, DimensionType: budgetdomain.DimensionTotal,
				DurationType: budgetdomain.DurationPermanent, RequestedAmount: 120, Reason: "test",
			})
			require.NoError(t, err)
			require.Equal(t, tt.expectedLevel, request.CurrentLevel)
		})
	}
}

// TestExpansionPrecheckTargetMustExceedCurrent verifies that precheck
// rejects targets that don't exceed the current limit.
func TestExpansionPrecheckTargetMustExceedCurrent(t *testing.T) {
	db, svc := newExpansionApprovalFixture(t)

	memberID, studioID := uuid.NewString(), uuid.NewString()
	require.NoError(t, db.Exec(`INSERT INTO studios(id,name) VALUES (?,?)`, studioID, "Studio").Error)
	require.NoError(t, db.Exec(`INSERT INTO departments(id,dept_id,dept_name,studio_id) VALUES ('row-dept','dept-a','Dept',?)`, studioID).Error)
	require.NoError(t, db.Exec(`INSERT INTO users(id,dept_id) VALUES (?,'row-dept')`, memberID).Error)

	for _, row := range []struct {
		scopeType, scopeID string
		amount             float64
	}{
		{model.BudgetScopeMember, memberID, 100},
		{model.BudgetScopeDeptDaily, "dept-a", 200},
		{model.BudgetScopeStudio, studioID, 300},
		{model.BudgetScopeCompany, model.BudgetScopeGlobal, 1000},
	} {
		require.NoError(t, db.Exec(`INSERT INTO budget_allocations
			(id,scope_type,scope_id,amount_usd,daily_amount_usd,weekly_amount_usd,period,enabled)
			VALUES (?,?,?,?,?,?,?,?)`, uuid.New(), row.scopeType, row.scopeID, row.amount, -1, -1, "monthly", true).Error)
	}

	// Request less than current
	result, err := svc.Precheck(context.Background(), service.ActorScope{
		UserID: memberID, Role: "user",
	}, service.PrecheckInput{
		ScopeType: model.BudgetScopeMember, ScopeID: memberID, PeriodType: "monthly",
		DimensionType: "total", DurationType: "permanent", TargetAmountUSD: "80.000000",
	})
	require.NoError(t, err)
	require.False(t, result.CanSubmit)
	require.NotNil(t, result.BlockingReason)
	require.Equal(t, "TARGET_NOT_ABOVE_CURRENT", result.BlockingReason.Code)
}

// TestExpansionCancelOnlyByRequester verifies that only the requester
// can cancel their own expansion request.
func TestExpansionCancelOnlyByRequester(t *testing.T) {
	db, svc := newExpansionApprovalFixture(t)

	requestID, requesterID, otherUserID := uuid.New(), uuid.New(), uuid.New()
	require.NoError(t, db.Create(&model.ExpansionRequest{
		ID: requestID, ScopeType: model.BudgetScopeMember, ScopeID: requesterID.String(),
		RequesterID: requesterID, PeriodType: "monthly", DimensionType: budgetdomain.DimensionTotal,
		DimensionKey: "total", DurationType: budgetdomain.DurationPermanent, RequestedAmount: 120,
		Reason: "test", Status: model.ExpansionStatusPending, CurrentLevel: model.ExpansionLevelDept,
	}).Error)

	// Try to cancel as someone else
	_, err := svc.DecideWorkflow(context.Background(), requestID.String(), otherUserID, service.DecisionInput{
		Action: model.ExpansionActionCancel,
	})
	require.ErrorIs(t, err, service.ErrExpansionForbidden)

	// Cancel as requester
	_, err = svc.DecideWorkflow(context.Background(), requestID.String(), requesterID, service.DecisionInput{
		Action: model.ExpansionActionCancel,
	})
	require.NoError(t, err)

	var request model.ExpansionRequest
	require.NoError(t, db.First(&request, "id = ?", requestID).Error)
	require.Equal(t, model.ExpansionStatusCancelled, request.Status)
}

// TestExpansionExpiredRequestCannotBeApproved verifies that expired
// requests cannot be approved.
func TestExpansionExpiredRequestCannotBeApproved(t *testing.T) {
	db, svc := newExpansionApprovalFixture(t)

	requestID, requesterID, approverID := uuid.New(), uuid.New(), uuid.New()
	seedApprovalChain(t, db, requesterID)
	require.NoError(t, db.Exec(`INSERT INTO budget_allocations
		(id,scope_type,scope_id,amount_usd,daily_amount_usd,weekly_amount_usd,period,enabled)
		VALUES (?,?,?,?,?,?,?,?)`,
		uuid.New(), model.BudgetScopeMember, requesterID.String(), 80, -1, -1, "monthly", true).Error)

	// Set expiration in the past
	expiresAt := time.Now().Add(-time.Hour)
	require.NoError(t, db.Create(&model.ExpansionRequest{
		ID: requestID, ScopeType: model.BudgetScopeMember, ScopeID: requesterID.String(),
		RequesterID: requesterID, PeriodType: "monthly", DimensionType: budgetdomain.DimensionTotal,
		DimensionKey: "total", DurationType: budgetdomain.DurationPermanent, RequestedAmount: 120,
		PrecheckVersion: "version-1", ExpiresAt: &expiresAt, Reason: "expired",
		Status: model.ExpansionStatusPending, CurrentLevel: model.ExpansionLevelDept,
	}).Error)

	_, err := svc.DecideWorkflow(context.Background(), requestID.String(), approverID, service.DecisionInput{
		Action: model.ExpansionActionApprove, PrecheckVersion: "version-1",
	})
	require.ErrorIs(t, err, service.ErrExpansionRequestExpired)
}
