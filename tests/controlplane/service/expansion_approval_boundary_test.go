package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type countingLegacyQuotaWriter struct {
	calls int
}

type retryingExpansionNotifier struct {
	calls        int
	failAttempts int
	items        []*model.Notification
}

func (n *retryingExpansionNotifier) Notify(_ context.Context, item *model.Notification) error {
	n.calls++
	if n.calls <= n.failAttempts {
		return assert.AnError
	}
	n.items = append(n.items, item)
	return nil
}

func (w *countingLegacyQuotaWriter) ApplyLegacyExpansionQuota(context.Context, *model.ExpansionRequest, uuid.UUID) error {
	w.calls++
	return nil
}

func newExpansionApprovalFixture(t *testing.T) (*gorm.DB, *service.ExpansionService) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	for _, ddl := range []string{
		`CREATE TABLE expansion_requests (
			id text primary key, scope_type text, scope_id text, requester_id text, period_type text,
			dimension_type text, dimension_key text, model_tier_id text, duration_type text, priority text,
			current_amount real, requested_amount real, current_amount_snapshot numeric, used_amount_snapshot numeric,
			precheck_version text, prechecked_at datetime, expires_at datetime, applied_at datetime,
			applied_target_type text, applied_target_id text, sync_status text, reason text, urgent_reason text,
			status text, current_level text, created_at datetime, updated_at datetime
		)`,
		`CREATE TABLE expansion_approval_steps (
			id text primary key, request_id text, level text, approver_id text,
			action text, comment text, created_at datetime
		)`,
		`CREATE TABLE budget_allocations (
			id text primary key, scope_type text, scope_id text, amount_usd real,
			daily_amount_usd real, weekly_amount_usd real, period text, enabled boolean,
			created_by text, updated_by text, created_at datetime, updated_at datetime,
			unique(scope_type, scope_id)
		)`,
		`CREATE TABLE budget_alert_records (
			id text primary key, scope_type text, scope_id text, period text
		)`,
		`CREATE TABLE budget_temporary_overrides (
			id text primary key, budget_allocation_id text, dimension_type text, dimension_key text,
			model_tier_id text, period_type text, target_limit_usd numeric,
			starts_at datetime, expires_at datetime, expansion_request_id text,
			status text, failure_reason text, created_at datetime, updated_at datetime
		)`,
		`CREATE TABLE studios (id text primary key, name text)`,
		`CREATE TABLE departments (id text primary key, dept_id text, dept_name text, studio_id text)`,
		`CREATE TABLE dept_studio_map (dept_id text, studio_id text)`,
		`CREATE TABLE users (id text primary key, dept_id text)`,
		`CREATE TABLE model_groups (id text primary key, name text, budget_tier_id text, deleted_at datetime)`,
		`CREATE TABLE notification_outbox (
			id text primary key, dedupe_key text unique, user_id text, type text, level text, title text, content text,
			ref_type text, ref_id text, status text, attempts integer, next_attempt datetime, last_error text,
			locked_at datetime, lease_owner text, lease_expires_at integer, delivered_at datetime, created_at datetime, updated_at datetime
		)`,
	} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	expansionRepo := repository.NewExpansionRepo(db)
	budgetRepo := repository.NewBudgetRepo(db)
	budgetSvc := service.NewBudgetService(budgetRepo, repository.NewBudgetAlertRepo(db))
	mr := miniredis.RunT(t)
	budgetSvc.SetRedis(redis.NewClient(&redis.Options{Addr: mr.Addr()}))
	expansionSvc := service.NewExpansionService(expansionRepo)
	expansionSvc.SetBudgetService(budgetSvc)
	return db, expansionSvc
}

func seedApprovalChain(t *testing.T, db *gorm.DB, memberID uuid.UUID) {
	t.Helper()
	deptRow, deptID, studioID := uuid.NewString(), "dept-"+memberID.String(), uuid.NewString()
	require.NoError(t, db.Exec(`INSERT INTO studios(id,name) VALUES (?,?)`, studioID, "Studio").Error)
	require.NoError(t, db.Exec(`INSERT INTO departments(id,dept_id,dept_name,studio_id) VALUES (?,?,?,?)`, deptRow, deptID, "Dept", studioID).Error)
	require.NoError(t, db.Exec(`INSERT INTO users(id,dept_id) VALUES (?,?)`, memberID.String(), deptRow).Error)
	for _, row := range []struct{ scopeType, scopeID string }{
		{model.BudgetScopeDeptDaily, deptID}, {model.BudgetScopeStudio, studioID}, {model.BudgetScopeCompany, model.BudgetScopeGlobal},
	} {
		require.NoError(t, db.Exec(`INSERT OR IGNORE INTO budget_allocations
			(id,scope_type,scope_id,amount_usd,daily_amount_usd,weekly_amount_usd,period,enabled)
			VALUES (?,?,?,?,?,?,?,?)`, uuid.New(), row.scopeType, row.scopeID, 1000, 1000, 1000, "monthly", true).Error)
	}
}

func TestExpansionPrecheckResolvesParentChainWithoutActorDeptIDs(t *testing.T) {
	db, svc := newExpansionApprovalFixture(t)
	memberID, studioID := uuid.NewString(), uuid.NewString()
	require.NoError(t, db.Exec(`INSERT INTO studios(id,name) VALUES (?,?)`, studioID, "Studio").Error)
	require.NoError(t, db.Exec(`INSERT INTO departments(id,dept_id,dept_name,studio_id) VALUES ('row-dept','dept-a','Dept',?)`, studioID).Error)
	require.NoError(t, db.Exec(`INSERT INTO users(id,dept_id) VALUES (?,'row-dept')`, memberID).Error)
	for _, row := range []struct {
		id, scopeType, scopeID string
		amount                 float64
	}{
		{uuid.NewString(), model.BudgetScopeMember, memberID, 50},
		{uuid.NewString(), model.BudgetScopeDeptDaily, "dept-a", 80},
		{uuid.NewString(), model.BudgetScopeStudio, studioID, 100},
		{uuid.NewString(), model.BudgetScopeCompany, model.BudgetScopeGlobal, 1000},
	} {
		require.NoError(t, db.Exec(`INSERT INTO budget_allocations
			(id,scope_type,scope_id,amount_usd,daily_amount_usd,weekly_amount_usd,period,enabled)
			VALUES (?,?,?,?,?,?,?,?)`, row.id, row.scopeType, row.scopeID, row.amount, -1, -1, "monthly", true).Error)
	}

	got, err := svc.Precheck(context.Background(), service.ActorScope{
		UserID: memberID, Role: "user",
	}, service.PrecheckInput{
		ScopeType: model.BudgetScopeMember, ScopeID: memberID, PeriodType: "monthly",
		DimensionType: "total", DurationType: "permanent", TargetAmountUSD: "90.000000",
	})

	require.NoError(t, err)
	require.False(t, got.CanSubmit)
	require.True(t, got.CanNotifyOwner)
	require.NotNil(t, got.BlockingReason)
	require.Equal(t, "dept", got.BlockingReason.SourceLevel)
	require.NotEmpty(t, got.ParentConstraints)
}

func TestExpansionPrecheckEnforcesParentChainForStudioAndDepartment(t *testing.T) {
	tests := []struct {
		name, scopeType, scopeID, target, wantSource string
	}{
		{name: "工作室受公司预算限制", scopeType: model.BudgetScopeStudio, scopeID: "studio-a", target: "120.000000", wantSource: "company"},
		{name: "部门受工作室预算限制", scopeType: model.BudgetScopeDeptDaily, scopeID: "dept-a", target: "90.000000", wantSource: "studio"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, svc := newExpansionApprovalFixture(t)
			require.NoError(t, db.Exec(`INSERT INTO studios(id,name) VALUES ('studio-a','Studio')`).Error)
			require.NoError(t, db.Exec(`INSERT INTO departments(id,dept_id,dept_name,studio_id) VALUES ('dept-row','dept-a','Dept','studio-a')`).Error)
			for _, row := range []struct {
				scopeType, scopeID string
				amount             float64
			}{
				{model.BudgetScopeCompany, model.BudgetScopeGlobal, 100},
				{model.BudgetScopeStudio, "studio-a", 80},
				{model.BudgetScopeDeptDaily, "dept-a", 60},
			} {
				require.NoError(t, db.Exec(`INSERT INTO budget_allocations
					(id,scope_type,scope_id,amount_usd,daily_amount_usd,weekly_amount_usd,period,enabled)
					VALUES (?,?,?,?,?,?,?,?)`, uuid.New(), row.scopeType, row.scopeID, row.amount, -1, -1, "monthly", true).Error)
			}
			got, err := svc.Precheck(context.Background(), service.ActorScope{
				UserID: uuid.NewString(), Role: "admin", IsGlobal: true,
			}, service.PrecheckInput{
				ScopeType: tt.scopeType, ScopeID: tt.scopeID, PeriodType: "monthly",
				DimensionType: "total", DurationType: "permanent", TargetAmountUSD: tt.target,
			})
			require.NoError(t, err)
			require.False(t, got.CanSubmit)
			require.NotNil(t, got.BlockingReason)
			require.Equal(t, tt.wantSource, got.BlockingReason.SourceLevel)
		})
	}
}

func TestExpansionWorkflowMarksStalePrecheckAndRequiresReprecheck(t *testing.T) {
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

	_, err := svc.DecideWorkflow(context.Background(), requestID.String(), approverID, service.DecisionInput{
		Action: model.ExpansionActionApprove, PrecheckVersion: "old-version",
	})
	require.ErrorIs(t, err, service.ErrPrecheckStale)
	var request model.ExpansionRequest
	require.NoError(t, db.First(&request, "id = ?", requestID).Error)
	require.Equal(t, model.ExpansionStatusPrecheckRequired, request.Status)
	require.NotEqual(t, "old-version", request.PrecheckVersion)
}

func TestExpansionRejectAlsoFreezesWhenPrecheckVersionIsStale(t *testing.T) {
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
		PrecheckVersion: "stale-version", ExpiresAt: &expiresAt, Reason: "reject stale",
		Status: model.ExpansionStatusPending, CurrentLevel: model.ExpansionLevelDept,
	}).Error)

	_, err := svc.DecideWorkflow(context.Background(), requestID.String(), approverID, service.DecisionInput{
		Action: model.ExpansionActionReject, Comment: "reject", PrecheckVersion: "stale-version",
	})

	require.ErrorIs(t, err, service.ErrPrecheckStale)
	var request model.ExpansionRequest
	require.NoError(t, db.First(&request, "id = ?", requestID).Error)
	require.Equal(t, model.ExpansionStatusPrecheckRequired, request.Status)
	var steps int64
	require.NoError(t, db.Model(&model.ExpansionApprovalStep{}).Where("request_id = ?", requestID).Count(&steps).Error)
	require.Zero(t, steps)
}

func TestExpansionTerminalRequestCannotBeReprecheckedOrEdited(t *testing.T) {
	db, svc := newExpansionApprovalFixture(t)
	requestID, requesterID := uuid.New(), uuid.New()
	require.NoError(t, db.Create(&model.ExpansionRequest{
		ID: requestID, ScopeType: model.BudgetScopeMember, ScopeID: requesterID.String(),
		RequesterID: requesterID, PeriodType: "monthly", DimensionType: budgetdomain.DimensionTotal,
		DimensionKey: "total", DurationType: budgetdomain.DurationPermanent, RequestedAmount: 120,
		Reason: "terminal", Status: model.ExpansionStatusApproved, CurrentLevel: model.ExpansionLevelDept,
	}).Error)
	_, err := svc.Reprecheck(context.Background(), service.ActorScope{UserID: requesterID.String(), Role: "user"}, requestID.String())
	require.ErrorIs(t, err, service.ErrExpansionTerminalConflict)
	_, err = svc.UpdateRequest(context.Background(), requestID.String(), requesterID.String(), service.UpdateExpansionInput{
		TargetAmountUSD: "130.000000", Reason: "edit terminal",
	})
	require.ErrorIs(t, err, service.ErrExpansionTerminalConflict)
	var request model.ExpansionRequest
	require.NoError(t, db.First(&request, "id = ?", requestID).Error)
	require.Equal(t, 120.0, request.RequestedAmount)
	require.Equal(t, model.ExpansionStatusApproved, request.Status)
}

func TestExpansionDecisionNotificationPersistsWithDecision(t *testing.T) {
	db, svc := newExpansionApprovalFixture(t)
	requestID, requesterID, approverID := uuid.New(), uuid.New(), uuid.New()
	seedApprovalChain(t, db, requesterID)
	require.NoError(t, db.Exec(`INSERT INTO budget_allocations
		(id,scope_type,scope_id,amount_usd,daily_amount_usd,weekly_amount_usd,period,enabled)
		VALUES (?,?,?,?,?,?,?,?)`,
		uuid.New(), model.BudgetScopeMember, requesterID.String(), 80, -1, -1, "monthly", true).Error)
	precheck, err := svc.Precheck(context.Background(), service.ActorScope{Role: "admin", IsGlobal: true}, service.PrecheckInput{
		ScopeType: model.BudgetScopeMember, ScopeID: requesterID.String(), PeriodType: "monthly",
		DimensionType: "total", DurationType: "permanent", TargetAmountUSD: "120.000000",
	})
	require.NoError(t, err)
	expiresAt := time.Now().Add(time.Hour)
	require.NoError(t, db.Create(&model.ExpansionRequest{
		ID: requestID, ScopeType: model.BudgetScopeMember, ScopeID: requesterID.String(),
		RequesterID: requesterID, PeriodType: "monthly", DimensionType: budgetdomain.DimensionTotal,
		DimensionKey: "total", DurationType: budgetdomain.DurationPermanent, RequestedAmount: 120,
		PrecheckVersion: precheck.PrecheckVersion, ExpiresAt: &expiresAt, Reason: "notify result",
		Status: model.ExpansionStatusPending, CurrentLevel: model.ExpansionLevelDept,
	}).Error)
	notifier := &retryingExpansionNotifier{failAttempts: 2}
	svc.WithNotifier(notifier)

	_, err = svc.DecideWorkflow(context.Background(), requestID.String(), approverID, service.DecisionInput{
		Action: model.ExpansionActionReject, Comment: "not approved", PrecheckVersion: precheck.PrecheckVersion,
	})

	require.NoError(t, err)
	require.Zero(t, notifier.calls)
	var outboxCount int64
	require.NoError(t, db.Model(&model.NotificationOutbox{}).Where("user_id = ? AND type = ?", requesterID, model.NotificationTypeExpansionRejected).Count(&outboxCount).Error)
	require.EqualValues(t, 1, outboxCount)
	var request model.ExpansionRequest
	require.NoError(t, db.First(&request, "id = ?", requestID).Error)
	require.Equal(t, model.ExpansionStatusRejected, request.Status)
}

func TestExpansionApprovalPreservesLegacyTotalPermanentBehavior(t *testing.T) {
	db, svc := newExpansionApprovalFixture(t)
	requestID, requesterID, approverID, allocationID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	seedApprovalChain(t, db, requesterID)
	require.NoError(t, db.Exec(`INSERT INTO budget_allocations
		(id,scope_type,scope_id,amount_usd,daily_amount_usd,weekly_amount_usd,period,enabled,created_by,updated_by)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		allocationID.String(), model.BudgetScopeMember, requesterID.String(), 80, -1, -1, "monthly", true,
		requesterID.String(), requesterID.String()).Error)
	require.NoError(t, db.Create(&model.ExpansionRequest{
		ID: requestID, ScopeType: model.BudgetScopeMember, ScopeID: requesterID.String(),
		RequesterID: requesterID, PeriodType: string(budgetdomain.PeriodMonthly),
		DimensionType: budgetdomain.DimensionTotal, DimensionKey: "total",
		DurationType: budgetdomain.DurationPermanent, RequestedAmount: 120,
		Reason: "increase total", Status: model.ExpansionStatusPending, CurrentLevel: model.ExpansionLevelDept,
	}).Error)

	require.NoError(t, svc.Decide(context.Background(), requestID.String(), approverID, model.ExpansionActionApprove, "approved"))
	var allocation model.BudgetAllocation
	require.NoError(t, db.First(&allocation, "id = ?", allocationID.String()).Error)
	require.Equal(t, 120.0, allocation.AmountUSD)
	var request model.ExpansionRequest
	require.NoError(t, db.First(&request, "id = ?", requestID.String()).Error)
	require.Equal(t, model.ExpansionStatusApproved, request.Status)
}

func TestExpansionApprovalStatusFailureDoesNotCallQuotaWriter(t *testing.T) {
	db, svc := newExpansionApprovalFixture(t)
	requestID, requesterID := uuid.New(), uuid.New()
	require.NoError(t, db.Create(&model.ExpansionRequest{
		ID: requestID, ScopeType: model.BudgetScopeMember, ScopeID: requesterID.String(),
		RequesterID: requesterID, PeriodType: string(budgetdomain.PeriodMonthly),
		DimensionType: budgetdomain.DimensionTotal, DimensionKey: "total",
		DurationType: budgetdomain.DurationPermanent, RequestedAmount: 120,
		Reason: "increase total", Status: model.ExpansionStatusPending, CurrentLevel: model.ExpansionLevelDept,
	}).Error)
	require.NoError(t, db.Exec(`CREATE TRIGGER reject_expansion_status_update
		BEFORE UPDATE ON expansion_requests
		BEGIN SELECT RAISE(FAIL, 'status update rejected'); END`).Error)
	writer := &countingLegacyQuotaWriter{}
	svc.SetLegacyQuotaWriter(writer)

	err := svc.Decide(context.Background(), requestID.String(), uuid.New(), model.ExpansionActionApprove, "approved")
	require.Error(t, err)
	require.Contains(t, err.Error(), "update expansion status")
	require.Zero(t, writer.calls)
}

func TestExpansionApprovalRejectsUnsupportedDimensionWithoutMutation(t *testing.T) {
	tests := []struct {
		name      string
		dimension budgetdomain.DimensionType
		duration  budgetdomain.DurationType
		tierID    *uuid.UUID
	}{
		{name: "tier permanent", dimension: budgetdomain.DimensionTier, duration: budgetdomain.DurationPermanent, tierID: ptrUUID(uuid.New())},
		{name: "total current period", dimension: budgetdomain.DimensionTotal, duration: budgetdomain.DurationCurrentPeriod},
		{name: "tier current period", dimension: budgetdomain.DimensionTier, duration: budgetdomain.DurationCurrentPeriod, tierID: ptrUUID(uuid.New())},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, svc := newExpansionApprovalFixture(t)
			requestID, requesterID := uuid.New(), uuid.New()
			require.NoError(t, db.Create(&model.ExpansionRequest{
				ID: requestID, ScopeType: model.BudgetScopeMember, ScopeID: requesterID.String(),
				RequesterID: requesterID, PeriodType: string(budgetdomain.PeriodMonthly),
				DimensionType: test.dimension, DimensionKey: "total", ModelTierID: test.tierID,
				DurationType: test.duration, RequestedAmount: 120, Reason: "unsupported before P2",
				Status: model.ExpansionStatusPending, CurrentLevel: model.ExpansionLevelDept,
			}).Error)

			err := svc.Decide(context.Background(), requestID.String(), uuid.New(), model.ExpansionActionApprove, "approved")
			require.ErrorIs(t, err, service.ErrExpansionApprovalUnsupported)
			var request model.ExpansionRequest
			require.NoError(t, db.First(&request, "id = ?", requestID.String()).Error)
			require.Equal(t, model.ExpansionStatusPending, request.Status)
			var overrideCount int64
			require.NoError(t, db.Table("budget_temporary_overrides").Count(&overrideCount).Error)
			require.Zero(t, overrideCount)
		})
	}
}

func TestExpansionApplicationRequiresNonEmptyReason(t *testing.T) {
	_, svc := newExpansionApprovalFixture(t)
	_, err := svc.CreateRequestAdvanced(context.Background(), service.CreateExpansionRequestInput{
		ScopeType: model.BudgetScopeMember, ScopeID: uuid.NewString(), RequesterID: uuid.New(),
		PeriodType: budgetdomain.PeriodMonthly, DimensionType: budgetdomain.DimensionTotal,
		DurationType: budgetdomain.DurationPermanent, RequestedAmount: 100, Reason: " ",
	})
	require.ErrorIs(t, err, service.ErrExpansionReasonRequired)
}

func TestExpansionCreateRejectsInvalidPeriod(t *testing.T) {
	_, svc := newExpansionApprovalFixture(t)
	_, err := svc.CreateRequestAdvanced(context.Background(), service.CreateExpansionRequestInput{
		ScopeType: model.BudgetScopeMember, ScopeID: uuid.NewString(), RequesterID: uuid.New(),
		PeriodType: budgetdomain.PeriodType("quarterly"), DimensionType: budgetdomain.DimensionTotal,
		DurationType: budgetdomain.DurationPermanent, RequestedAmount: 100, Reason: "capacity",
	})
	require.ErrorIs(t, err, service.ErrInvalidExpansionPeriod)

	_, err = svc.CreateRequest(
		context.Background(), model.BudgetScopeMember, uuid.NewString(), uuid.New(),
		10, 100, "quarterly", "capacity",
	)
	require.ErrorIs(t, err, service.ErrInvalidExpansionPeriod)
}

func ptrUUID(value uuid.UUID) *uuid.UUID {
	return &value
}
