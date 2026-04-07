//go:build integration

package repository_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestExpansionWorkflowPostgresConcurrentApprovalIsExactlyOnce(t *testing.T) {
	dsn := postgresIntegrationDSN(t)
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	schema := "expansion_concurrency_" + strings.ReplaceAll(uuid.NewString()[:12], "-", "")
	require.NoError(t, admin.Exec(`CREATE SCHEMA "`+schema+`"`).Error)
	t.Cleanup(func() { _ = admin.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`).Error })
	separator := "?"
	if parsed, parseErr := url.Parse(dsn); parseErr == nil && parsed.RawQuery != "" {
		separator = "&"
	}
	db, err := gorm.Open(postgres.Open(dsn+separator+"search_path="+schema), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`
		CREATE TABLE budget_allocations (
			id uuid PRIMARY KEY, scope_type text NOT NULL, scope_id text NOT NULL,
			amount_usd double precision NOT NULL, daily_amount_usd double precision NOT NULL,
			weekly_amount_usd double precision NOT NULL, period text, enabled boolean NOT NULL,
			created_by uuid, updated_by uuid, created_at timestamptz, updated_at timestamptz,
			UNIQUE(scope_type, scope_id)
		);
		CREATE TABLE studios (id uuid PRIMARY KEY, name text);
		CREATE TABLE departments (id uuid PRIMARY KEY, dept_id text UNIQUE, dept_name text, studio_id uuid);
		CREATE TABLE dept_studio_map (dept_id text, studio_id uuid);
		CREATE TABLE users (id uuid PRIMARY KEY, dept_id uuid);
		CREATE TABLE model_groups (id uuid PRIMARY KEY, name text, budget_tier_id uuid, deleted_at timestamptz);
		CREATE TABLE notification_outbox (
			id uuid PRIMARY KEY, dedupe_key text UNIQUE, user_id uuid, type text, level text, title text, content text,
			ref_type text, ref_id text, status text, attempts bigint, next_attempt timestamptz, last_error text,
			locked_at timestamptz, lease_owner text, lease_expires_at bigint, delivered_at timestamptz, created_at timestamptz, updated_at timestamptz
		);
		CREATE TABLE budget_alert_records (
			id uuid PRIMARY KEY, scope_type text, scope_id text, period_type text, period text,
			threshold integer, dimension_key text, dimension_type text, model_tier_id uuid, notified_at timestamptz
		);
		CREATE TABLE expansion_requests (
			id uuid PRIMARY KEY, scope_type text NOT NULL, scope_id text NOT NULL, requester_id uuid NOT NULL,
			period_type text NOT NULL, dimension_type text NOT NULL, dimension_key text NOT NULL,
			model_tier_id uuid, duration_type text NOT NULL, priority text, current_amount numeric,
			requested_amount numeric NOT NULL, current_amount_snapshot numeric, used_amount_snapshot numeric,
			precheck_version text, prechecked_at timestamptz, expires_at timestamptz, applied_at timestamptz,
			applied_target_type text, applied_target_id uuid, sync_status text, reason text, urgent_reason text,
			status text NOT NULL, current_level text NOT NULL, created_at timestamptz, updated_at timestamptz
		);
		CREATE TABLE expansion_approval_steps (
			id uuid PRIMARY KEY, request_id uuid NOT NULL, level text NOT NULL, approver_id uuid,
			action text NOT NULL, comment text, created_at timestamptz
		);
		CREATE TABLE budget_tier_limits (
			id uuid PRIMARY KEY, budget_allocation_id uuid NOT NULL, model_tier_id uuid NOT NULL,
			period_type varchar(8) NOT NULL, limit_usd numeric(18,6) NOT NULL,
			effective_at timestamptz NOT NULL, enabled boolean NOT NULL,
			created_at timestamptz, updated_at timestamptz,
			UNIQUE (budget_allocation_id, model_tier_id, period_type)
		);
		CREATE TABLE budget_temporary_overrides (
			id uuid PRIMARY KEY, budget_allocation_id uuid NOT NULL, dimension_type text NOT NULL,
			dimension_key text NOT NULL, model_tier_id uuid, period_type text NOT NULL,
			target_limit_usd numeric(18,6) NOT NULL, starts_at timestamptz NOT NULL, expires_at timestamptz NOT NULL,
			status text NOT NULL, expansion_request_id uuid, reason text, created_by uuid,
			failure_reason text, created_at timestamptz, updated_at timestamptz
		);
		CREATE UNIQUE INDEX uniq_override_expansion
			ON budget_temporary_overrides(expansion_request_id) WHERE expansion_request_id IS NOT NULL;
	`).Error)

	budgetRepo := repository.NewBudgetRepo(db)
	budgetSvc := service.NewBudgetService(budgetRepo, repository.NewBudgetAlertRepo(db))
	mini := miniredis.RunT(t)
	budgetSvc.SetRedis(redis.NewClient(&redis.Options{Addr: mini.Addr()}))
	expansionSvc := service.NewExpansionService(repository.NewExpansionRepo(db))
	expansionSvc.SetBudgetService(budgetSvc)
	tierRepo := repository.NewBudgetTierRepo(db)
	expansionSvc.SetBudgetTierService(service.NewBudgetTierService(tierRepo, budgetRepo, repository.NewModelTierRepo(db), time.Now))
	actor := service.ActorScope{Role: "admin", IsGlobal: true}
	approverID, requesterID := uuid.New(), uuid.New()

	createAllocation := func(scopeID string, amount float64) uuid.UUID {
		memberID := uuid.MustParse(scopeID)
		deptRow, studioID, deptID := uuid.New(), uuid.New(), "dept-"+memberID.String()
		require.NoError(t, db.Exec(`INSERT INTO studios(id,name) VALUES (?,?)`, studioID, "Studio").Error)
		require.NoError(t, db.Exec(`INSERT INTO departments(id,dept_id,dept_name,studio_id) VALUES (?,?,?,?)`, deptRow, deptID, "Dept", studioID).Error)
		require.NoError(t, db.Exec(`INSERT INTO users(id,dept_id) VALUES (?,?)`, memberID, deptRow).Error)
		for _, parent := range []struct {
			id                 uuid.UUID
			scopeType, scopeID string
		}{
			{uuid.New(), model.BudgetScopeDeptDaily, deptID}, {uuid.New(), model.BudgetScopeStudio, studioID.String()},
			{uuid.New(), model.BudgetScopeCompany, model.BudgetScopeGlobal},
		} {
			require.NoError(t, db.Exec(`INSERT INTO budget_allocations
				(id,scope_type,scope_id,amount_usd,daily_amount_usd,weekly_amount_usd,period,enabled)
				VALUES (?,?,?,?,?,?,?,true) ON CONFLICT (scope_type,scope_id) DO NOTHING`,
				parent.id, parent.scopeType, parent.scopeID, 1000, 1000, 1000, "monthly").Error)
		}
		id := uuid.New()
		require.NoError(t, db.Exec(`INSERT INTO budget_allocations
			(id,scope_type,scope_id,amount_usd,daily_amount_usd,weekly_amount_usd,period,enabled)
			VALUES (?, 'member', ?, ?, -1, -1, 'monthly', true)`, id, scopeID, amount).Error)
		return id
	}
	createRequest := func(scopeID string, tierID *uuid.UUID, duration budgetdomain.DurationType, target string) (*model.ExpansionRequest, string) {
		tierString := (*string)(nil)
		dimension, dimensionKey := budgetdomain.DimensionTotal, "total"
		if tierID != nil {
			value := tierID.String()
			tierString = &value
			dimension, dimensionKey = budgetdomain.DimensionTier, "tier:"+value
		}
		precheck, precheckErr := expansionSvc.Precheck(context.Background(), actor, service.PrecheckInput{
			ScopeType: model.BudgetScopeMember, ScopeID: scopeID, PeriodType: string(budgetdomain.PeriodMonthly),
			DimensionType: string(dimension), ModelTierID: tierString, DurationType: string(duration),
			TargetAmountUSD: target,
		})
		require.NoError(t, precheckErr)
		targetAmount, _ := budgetdomain.ParseAmount(target)
		targetFloat, _ := targetAmount.Float64()
		expiresAt := time.Now().Add(time.Hour)
		request := &model.ExpansionRequest{
			ID: uuid.New(), ScopeType: model.BudgetScopeMember, ScopeID: scopeID, RequesterID: requesterID,
			PeriodType: string(budgetdomain.PeriodMonthly), DimensionType: dimension, DimensionKey: dimensionKey,
			ModelTierID: tierID, DurationType: duration, RequestedAmount: targetFloat,
			PrecheckVersion: precheck.PrecheckVersion, PrecheckedAt: &precheck.DataAsOf, ExpiresAt: &expiresAt,
			Reason: "postgres concurrency", Status: model.ExpansionStatusPending, CurrentLevel: model.ExpansionLevelDept,
		}
		require.NoError(t, db.Create(request).Error)
		return request, precheck.PrecheckVersion
	}
	type approvalRun struct {
		result *service.ApplyResult
		err    error
	}
	approveConcurrently := func(request *model.ExpansionRequest, version string) []approvalRun {
		start := make(chan struct{})
		runs := make(chan approvalRun, 2)
		var wg sync.WaitGroup
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				result, approveErr := expansionSvc.DecideWorkflow(context.Background(), request.ID.String(), approverID, service.DecisionInput{
					Action: model.ExpansionActionApprove, PrecheckVersion: version,
				})
				runs <- approvalRun{result: result, err: approveErr}
			}()
		}
		close(start)
		wg.Wait()
		close(runs)
		result := make([]approvalRun, 0, 2)
		for run := range runs {
			result = append(result, run)
		}
		return result
	}

	permanentScope := uuid.NewString()
	permanentAllocationID := createAllocation(permanentScope, 80)
	permanentRequest, permanentVersion := createRequest(permanentScope, nil, budgetdomain.DurationPermanent, "120.000000")
	permanentRuns := approveConcurrently(permanentRequest, permanentVersion)
	idempotentCount := 0
	for _, run := range permanentRuns {
		require.NoError(t, run.err)
		require.NotNil(t, run.result)
		if run.result.AlreadyApplied {
			idempotentCount++
		}
	}
	require.Equal(t, 1, idempotentCount)
	var applied model.ExpansionRequest
	require.NoError(t, db.First(&applied, "id = ?", permanentRequest.ID).Error)
	require.NotNil(t, applied.AppliedAt)
	var amount float64
	require.NoError(t, db.Raw(`SELECT amount_usd FROM budget_allocations WHERE id = ?`, permanentAllocationID).Scan(&amount).Error)
	require.Equal(t, 120.0, amount)
	var stepCount int64
	require.NoError(t, db.Model(&model.ExpansionApprovalStep{}).Where("request_id = ?", permanentRequest.ID).Count(&stepCount).Error)
	require.EqualValues(t, 1, stepCount)

	var studioScope, deptScope string
	require.NoError(t, db.Raw(`SELECT CAST(d.studio_id AS TEXT), d.dept_id FROM users u JOIN departments d ON d.id = u.dept_id WHERE u.id = ?`, uuid.MustParse(permanentScope)).
		Row().Scan(&studioScope, &deptScope))
	require.NoError(t, db.Model(&model.BudgetAllocation{}).Where("scope_type = ? AND scope_id = ?", model.BudgetScopeCompany, model.BudgetScopeGlobal).Update("amount_usd", 100).Error)
	studioPrecheck, err := expansionSvc.Precheck(context.Background(), actor, service.PrecheckInput{
		ScopeType: model.BudgetScopeStudio, ScopeID: studioScope, PeriodType: string(budgetdomain.PeriodMonthly),
		DimensionType: string(budgetdomain.DimensionTotal), DurationType: string(budgetdomain.DurationPermanent), TargetAmountUSD: "120",
	})
	require.NoError(t, err)
	require.False(t, studioPrecheck.CanSubmit)
	require.Equal(t, "company", studioPrecheck.BlockingReason.SourceLevel)
	require.NoError(t, db.Model(&model.BudgetAllocation{}).Where("scope_type = ? AND scope_id = ?", model.BudgetScopeCompany, model.BudgetScopeGlobal).Update("amount_usd", 1000).Error)
	require.NoError(t, db.Model(&model.BudgetAllocation{}).Where("scope_type = ? AND scope_id = ?", model.BudgetScopeStudio, studioScope).Update("amount_usd", 100).Error)
	require.NoError(t, db.Model(&model.BudgetAllocation{}).Where("scope_type = ? AND scope_id = ?", model.BudgetScopeDeptDaily, deptScope).Update("amount_usd", 80).Error)
	deptPrecheck, err := expansionSvc.Precheck(context.Background(), actor, service.PrecheckInput{
		ScopeType: model.BudgetScopeDeptDaily, ScopeID: deptScope, PeriodType: string(budgetdomain.PeriodMonthly),
		DimensionType: string(budgetdomain.DimensionTotal), DurationType: string(budgetdomain.DurationPermanent), TargetAmountUSD: "120",
	})
	require.NoError(t, err)
	require.False(t, deptPrecheck.CanSubmit)
	require.Equal(t, "studio", deptPrecheck.BlockingReason.SourceLevel)
	require.NoError(t, db.Model(&model.BudgetAllocation{}).Where("scope_type = ? AND scope_id = ?", model.BudgetScopeStudio, studioScope).Update("amount_usd", 1000).Error)

	staleScope := uuid.NewString()
	staleAllocationID := createAllocation(staleScope, 70)
	staleRequest, _ := createRequest(staleScope, nil, budgetdomain.DurationPermanent, "110.000000")
	_, err = expansionSvc.DecideWorkflow(context.Background(), staleRequest.ID.String(), approverID, service.DecisionInput{
		Action: model.ExpansionActionApprove, PrecheckVersion: "stale-version",
	})
	require.ErrorIs(t, err, service.ErrPrecheckStale)
	require.NoError(t, db.Raw(`SELECT amount_usd FROM budget_allocations WHERE id = ?`, staleAllocationID).Scan(&amount).Error)
	require.Equal(t, 70.0, amount)
	applied = model.ExpansionRequest{}
	require.NoError(t, db.First(&applied, "id = ?", staleRequest.ID).Error)
	require.Equal(t, model.ExpansionStatusPrecheckRequired, applied.Status)
	require.Nil(t, applied.AppliedAt)
	require.NoError(t, db.Model(&model.ExpansionApprovalStep{}).
		Where("request_id = ?", staleRequest.ID).Count(&stepCount).Error)
	require.Zero(t, stepCount)

	racingScope := uuid.NewString()
	createAllocation(racingScope, 80)
	racingRequest, racingVersion := createRequest(racingScope, nil, budgetdomain.DurationPermanent, "120.000000")
	var racingParentIDText string
	require.NoError(t, db.Raw(`SELECT ba.id FROM budget_allocations ba
		JOIN departments d ON d.dept_id = ba.scope_id
		JOIN users u ON u.dept_id = d.id
		WHERE u.id = ? AND ba.scope_type = ?`, uuid.MustParse(racingScope), model.BudgetScopeDeptDaily).
		Scan(&racingParentIDText).Error)
	racingParentID := uuid.MustParse(racingParentIDText)
	budgetTx := db.Begin()
	require.NoError(t, budgetTx.Error)
	var lockedAllocation model.BudgetAllocation
	require.NoError(t, budgetTx.Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&lockedAllocation, "id = ?", racingParentID).Error)
	require.NoError(t, budgetTx.Model(&model.BudgetAllocation{}).
		Where("id = ?", racingParentID).Update("amount_usd", 95).Error)
	approvalDone := make(chan error, 1)
	go func() {
		_, approveErr := expansionSvc.DecideWorkflow(
			context.Background(), racingRequest.ID.String(), approverID,
			service.DecisionInput{Action: model.ExpansionActionApprove, PrecheckVersion: racingVersion},
		)
		approvalDone <- approveErr
	}()
	time.Sleep(150 * time.Millisecond)
	require.NoError(t, budgetTx.Commit().Error)
	select {
	case approveErr := <-approvalDone:
		require.ErrorIs(t, approveErr, service.ErrPrecheckStale)
	case <-time.After(5 * time.Second):
		t.Fatal("approval did not resume after concurrent budget transaction committed")
	}
	require.NoError(t, db.Raw(`SELECT amount_usd FROM budget_allocations WHERE id = ?`, racingParentID).Scan(&amount).Error)
	require.Equal(t, 95.0, amount)
	applied = model.ExpansionRequest{}
	require.NoError(t, db.First(&applied, "id = ?", racingRequest.ID).Error)
	require.Equal(t, model.ExpansionStatusPrecheckRequired, applied.Status)
	require.Nil(t, applied.AppliedAt)

	editScope := uuid.NewString()
	createAllocation(editScope, 80)
	editRequest, editVersion := createRequest(editScope, nil, budgetdomain.DurationPermanent, "120.000000")
	start := make(chan struct{})
	editDone, approveEditDone := make(chan error, 1), make(chan error, 1)
	go func() {
		<-start
		_, updateErr := expansionSvc.UpdateRequest(context.Background(), editRequest.ID.String(), requesterID.String(), service.UpdateExpansionInput{
			TargetAmountUSD: "130.000000", Reason: "concurrent edit",
		})
		editDone <- updateErr
	}()
	go func() {
		<-start
		_, approveErr := expansionSvc.DecideWorkflow(context.Background(), editRequest.ID.String(), approverID, service.DecisionInput{
			Action: model.ExpansionActionApprove, PrecheckVersion: editVersion,
		})
		approveEditDone <- approveErr
	}()
	close(start)
	editErr, approveEditErr := <-editDone, <-approveEditDone
	require.True(t,
		(editErr == nil && errors.Is(approveEditErr, service.ErrPrecheckStale)) ||
			(errors.Is(editErr, service.ErrExpansionTerminalConflict) && approveEditErr == nil),
		"edit=%v approve=%v", editErr, approveEditErr,
	)
	applied = model.ExpansionRequest{}
	require.NoError(t, db.First(&applied, "id = ?", editRequest.ID).Error)
	if editErr == nil {
		require.Equal(t, model.ExpansionStatusPrecheckRequired, applied.Status)
		require.Equal(t, 130.0, applied.RequestedAmount)
	} else {
		require.Equal(t, model.ExpansionStatusApproved, applied.Status)
		require.Equal(t, 120.0, applied.RequestedAmount)
	}

	overrideScope := uuid.NewString()
	createAllocation(overrideScope, 90)
	tierID := uuid.New()
	require.NoError(t, db.Exec(`INSERT INTO budget_tier_limits
		(id,budget_allocation_id,model_tier_id,period_type,limit_usd,enabled,effective_at,created_at,updated_at)
		SELECT gen_random_uuid(), id, ?, 'monthly', 1000, true, now(), now(), now()
		FROM budget_allocations WHERE scope_type <> 'member'
		ON CONFLICT (budget_allocation_id,model_tier_id,period_type) DO NOTHING`, tierID).Error)
	overrideRequest, overrideVersion := createRequest(overrideScope, &tierID, budgetdomain.DurationCurrentPeriod, "40.000000")
	overrideRuns := approveConcurrently(overrideRequest, overrideVersion)
	idempotentCount = 0
	for _, run := range overrideRuns {
		require.NoError(t, run.err)
		require.NotNil(t, run.result)
		if run.result.AlreadyApplied {
			idempotentCount++
		}
	}
	require.Equal(t, 1, idempotentCount)
	var overrideCount int64
	require.NoError(t, db.Model(&model.BudgetTemporaryOverride{}).
		Where("expansion_request_id = ?", overrideRequest.ID).Count(&overrideCount).Error)
	require.EqualValues(t, 1, overrideCount)
	require.NoError(t, db.Model(&model.ExpansionApprovalStep{}).
		Where("request_id = ?", overrideRequest.ID).Count(&stepCount).Error)
	require.EqualValues(t, 1, stepCount)
}

func TestBudgetTierPostgresConcurrencyContracts(t *testing.T) {
	dsn := postgresIntegrationDSN(t)
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	schema := "budget_p0_" + uuid.NewString()
	schema = "budget_p0_" + schema[len(schema)-12:]
	require.NoError(t, admin.Exec(`CREATE SCHEMA "`+schema+`"`).Error)
	t.Cleanup(func() { _ = admin.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`).Error })

	separator := "?"
	if parsed, parseErr := url.Parse(dsn); parseErr == nil && parsed.RawQuery != "" {
		separator = "&"
	}
	db, err := gorm.Open(postgres.Open(dsn+separator+"search_path="+schema), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`
		CREATE TABLE budget_allocations (id uuid PRIMARY KEY);
		CREATE TABLE budget_tier_limits (
			id uuid PRIMARY KEY, budget_allocation_id uuid NOT NULL, model_tier_id uuid NOT NULL,
			period_type varchar(8) NOT NULL, limit_usd numeric(18,6) NOT NULL,
			effective_at timestamptz NOT NULL, enabled boolean NOT NULL,
			created_at timestamptz, updated_at timestamptz,
			UNIQUE (budget_allocation_id, model_tier_id, period_type)
		);
		CREATE TABLE expansion_requests (
			id uuid PRIMARY KEY, scope_type text NOT NULL, scope_id text NOT NULL, requester_id uuid NOT NULL,
			period_type text NOT NULL, dimension_type text NOT NULL, dimension_key text NOT NULL,
			model_tier_id uuid, duration_type text NOT NULL, priority text, current_amount numeric,
			requested_amount numeric NOT NULL, current_amount_snapshot numeric, used_amount_snapshot numeric,
			precheck_version text, prechecked_at timestamptz, expires_at timestamptz, applied_at timestamptz,
			applied_target_type text, applied_target_id uuid, sync_status text, reason text, urgent_reason text,
			status text NOT NULL, current_level text NOT NULL, created_at timestamptz, updated_at timestamptz
		);
		CREATE UNIQUE INDEX uniq_expansion_request_pending
			ON expansion_requests(requester_id, scope_type, scope_id, period_type, dimension_key, duration_type)
			WHERE status IN ('pending', 'precheck_required');
		CREATE TABLE budget_temporary_overrides (
			id uuid PRIMARY KEY, budget_allocation_id uuid NOT NULL, dimension_type text NOT NULL,
			dimension_key text NOT NULL, model_tier_id uuid, period_type text NOT NULL,
			target_limit_usd numeric NOT NULL, starts_at timestamptz NOT NULL, expires_at timestamptz NOT NULL,
			status text NOT NULL, expansion_request_id uuid, reason text, created_by uuid,
			failure_reason text, created_at timestamptz, updated_at timestamptz
		);
		CREATE TABLE budget_limit_schedules (
			id uuid PRIMARY KEY, budget_allocation_id uuid NOT NULL, dimension_type text NOT NULL,
			dimension_key text NOT NULL, model_tier_id uuid, period_type text NOT NULL,
			target_limit_usd numeric NOT NULL, effective_at timestamptz NOT NULL, reason text NOT NULL,
			created_by uuid NOT NULL, status text NOT NULL, failure_reason text,
			created_at timestamptz, updated_at timestamptz
		)
	`).Error)
	require.NoError(t, db.Exec(`CREATE UNIQUE INDEX uniq_budget_schedule_pending
		ON budget_limit_schedules(budget_allocation_id, dimension_key, period_type)
		WHERE status = 'pending'`).Error)

	tierRepo := repository.NewBudgetTierRepo(db)
	allocationID, tierID := uuid.New(), uuid.New()
	require.NoError(t, db.Exec(`INSERT INTO budget_allocations (id) VALUES (?)`, allocationID).Error)
	first, err := tierRepo.UpsertTierLimit(context.Background(), repository.UpsertTierLimitParams{
		BudgetAllocationID: allocationID, ModelTierID: tierID, PeriodType: budgetdomain.PeriodMonthly,
		LimitUSD: mustRepoAmount(t, "10"), EffectiveAt: time.Now().Add(-time.Hour), Enabled: true,
	})
	require.NoError(t, err)
	second, err := tierRepo.UpsertTierLimit(context.Background(), repository.UpsertTierLimitParams{
		BudgetAllocationID: allocationID, ModelTierID: tierID, PeriodType: budgetdomain.PeriodMonthly,
		LimitUSD: mustRepoAmount(t, "20"), EffectiveAt: time.Now(), Enabled: true,
	})
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)

	expansionRepo := repository.NewExpansionRepo(db)
	requesterID := uuid.New()
	buildRequest := func() *model.ExpansionRequest {
		return &model.ExpansionRequest{
			ID: uuid.New(), ScopeType: model.BudgetScopeMember, ScopeID: "member-a", RequesterID: requesterID,
			PeriodType: model.ExpansionPeriodMonthly, DimensionType: budgetdomain.DimensionTotal,
			DimensionKey: "total", DurationType: budgetdomain.DurationPermanent,
			RequestedAmount: 100, Status: model.ExpansionStatusPending, CurrentLevel: model.ExpansionLevelDept,
		}
	}
	require.NoError(t, expansionRepo.Create(buildRequest()))
	require.ErrorIs(t, expansionRepo.Create(buildRequest()), repository.ErrDuplicatePendingExpansion)

	overrideID := uuid.New()
	require.NoError(t, db.Exec(`INSERT INTO budget_temporary_overrides
		(id, budget_allocation_id, dimension_type, dimension_key, period_type, target_limit_usd, starts_at, expires_at, status)
		VALUES (?, ?, 'total', 'total', 'monthly', 100, now() - interval '1 hour', now() - interval '1 minute', 'active')`,
		overrideID, allocationID).Error)
	tx1 := db.Begin()
	require.NoError(t, tx1.Error)
	defer tx1.Rollback()
	var locked model.BudgetTemporaryOverride
	require.NoError(t, tx1.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, "id = ?", overrideID).Error)
	tx2 := db.Begin()
	require.NoError(t, tx2.Error)
	defer tx2.Rollback()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = tierRepo.LockOverrideForUpdate(ctx, tx2, overrideID)
	require.True(t, errors.Is(err, gorm.ErrRecordNotFound), "SKIP LOCKED should not wait or return locked row: %v", err)

	buildSchedule := func(amount string) *model.BudgetLimitSchedule {
		value := mustRepoAmount(t, amount)
		return &model.BudgetLimitSchedule{
			ID: uuid.New(), BudgetAllocationID: allocationID, DimensionType: budgetdomain.DimensionTier,
			DimensionKey: "tier:" + tierID.String(), ModelTierID: &tierID, PeriodType: budgetdomain.PeriodMonthly,
			TargetLimitUSD: value, EffectiveAt: time.Now().Add(time.Hour), Reason: "concurrent",
			CreatedBy: uuid.New(), Status: budgetdomain.SchedulePending,
		}
	}
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, amount := range []string{"30", "40"} {
		wg.Add(1)
		go func(schedule *model.BudgetLimitSchedule) {
			defer wg.Done()
			<-start
			_, putErr := tierRepo.PutPendingSchedule(context.Background(), schedule, false)
			errs <- putErr
		}(buildSchedule(amount))
	}
	close(start)
	wg.Wait()
	close(errs)
	successes, conflicts := 0, 0
	for putErr := range errs {
		switch {
		case putErr == nil:
			successes++
		case errors.Is(putErr, repository.ErrPendingScheduleExists):
			conflicts++
		default:
			require.NoError(t, putErr)
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)
	var pendingCount int64
	require.NoError(t, db.Model(&model.BudgetLimitSchedule{}).Where("status = ?", budgetdomain.SchedulePending).Count(&pendingCount).Error)
	require.EqualValues(t, 1, pendingCount)
}

func TestBatchTierLockOrderAvoidsSiblingDeadlocks(t *testing.T) {
	dsn := postgresIntegrationDSN(t)
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	schema := "budget_lock_" + strings.ReplaceAll(uuid.NewString()[:12], "-", "")
	require.NoError(t, admin.Exec(`CREATE SCHEMA "`+schema+`"`).Error)
	t.Cleanup(func() { _ = admin.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`).Error })
	separator := "?"
	if parsed, parseErr := url.Parse(dsn); parseErr == nil && parsed.RawQuery != "" {
		separator = "&"
	}
	db, err := gorm.Open(postgres.Open(dsn+separator+"search_path="+schema), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`
		CREATE TABLE budget_allocations (
			id uuid PRIMARY KEY, scope_type varchar(32) NOT NULL, scope_id varchar(128) NOT NULL,
			amount_usd numeric NOT NULL DEFAULT 0, daily_amount_usd numeric NOT NULL DEFAULT 0,
			weekly_amount_usd numeric NOT NULL DEFAULT 0, period varchar(16) NOT NULL DEFAULT 'monthly',
			enabled boolean NOT NULL DEFAULT true, created_by uuid, updated_by uuid,
			created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
		);
		CREATE TABLE budget_tier_limits (
			id uuid PRIMARY KEY, budget_allocation_id uuid NOT NULL, model_tier_id uuid NOT NULL,
			period_type varchar(8) NOT NULL, limit_usd numeric(18,6) NOT NULL,
			effective_at timestamptz NOT NULL, enabled boolean NOT NULL,
			created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
			UNIQUE (budget_allocation_id, model_tier_id, period_type)
		);
	`).Error)
	parentID, leftID, rightID, tierID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	require.NoError(t, db.Exec(`
		INSERT INTO budget_allocations (id, scope_type, scope_id) VALUES
			(?, 'company', ?), (?, 'studio', 'left'), (?, 'studio', 'right')
	`, parentID, model.BudgetScopeGlobal, leftID, rightID).Error)
	require.NoError(t, db.Exec(`
		INSERT INTO budget_tier_limits
			(id, budget_allocation_id, model_tier_id, period_type, limit_usd, effective_at, enabled)
		VALUES (?, ?, ?, 'monthly', 100, now(), true),
		       (?, ?, ?, 'monthly', 40, now(), true),
		       (?, ?, ?, 'monthly', 40, now(), true)
	`, uuid.New(), parentID, tierID, uuid.New(), leftID, tierID, uuid.New(), rightID, tierID).Error)

	repo := repository.NewBudgetRepo(db)
	for iteration := 0; iteration < 20; iteration++ {
		start := make(chan struct{})
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for _, allocationID := range []uuid.UUID{leftID, rightID} {
			wg.Add(1)
			go func(allocationID uuid.UUID) {
				defer wg.Done()
				<-start
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				errs <- db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
					allocation, err := repo.GetAllocationByIDInTx(ctx, tx, allocationID)
					if err != nil {
						return err
					}
					risk, err := repo.GetBatchTierRiskSnapshotInTx(ctx, tx, *allocation, tierID, budgetdomain.PeriodMonthly)
					if err != nil {
						return err
					}
					if risk.TargetAllocation == nil || risk.TargetAllocation.ID != allocationID ||
						risk.TargetLimit == nil || risk.ParentLimitUSD == nil {
						return fmt.Errorf("locked state missing target or parent tier state")
					}
					time.Sleep(5 * time.Millisecond)
					return nil
				})
			}(allocationID)
		}
		close(start)
		wg.Wait()
		close(errs)
		for runErr := range errs {
			require.NoError(t, runErr)
		}
	}
}

func TestLifecycleAllocationFirstAvoidsConfigDeadlockAndStaleFailure(t *testing.T) {
	dsn := postgresIntegrationDSN(t)
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	schema := "budget_lifecycle_" + strings.ReplaceAll(uuid.NewString()[:12], "-", "")
	require.NoError(t, admin.Exec(`CREATE SCHEMA "`+schema+`"`).Error)
	t.Cleanup(func() { _ = admin.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`).Error })
	separator := "?"
	if parsed, parseErr := url.Parse(dsn); parseErr == nil && parsed.RawQuery != "" {
		separator = "&"
	}
	db, err := gorm.Open(postgres.Open(dsn+separator+"search_path="+schema), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`
		CREATE TABLE budget_allocations (
			id uuid PRIMARY KEY, amount_usd numeric NOT NULL DEFAULT 0,
			daily_amount_usd numeric NOT NULL DEFAULT 0, weekly_amount_usd numeric NOT NULL DEFAULT 0
		);
		CREATE TABLE budget_tier_limits (
			id uuid PRIMARY KEY, budget_allocation_id uuid NOT NULL, model_tier_id uuid NOT NULL,
			period_type varchar(8) NOT NULL, limit_usd numeric(18,6) NOT NULL,
			effective_at timestamptz NOT NULL, enabled boolean NOT NULL,
			created_at timestamptz, updated_at timestamptz,
			UNIQUE (budget_allocation_id, model_tier_id, period_type)
		);
		CREATE TABLE budget_limit_schedules (
			id uuid PRIMARY KEY, budget_allocation_id uuid NOT NULL, dimension_type varchar(8) NOT NULL,
			dimension_key varchar(48) NOT NULL, model_tier_id uuid, period_type varchar(8) NOT NULL,
			target_limit_usd numeric(18,6) NOT NULL, effective_at timestamptz NOT NULL,
			reason text NOT NULL, created_by uuid NOT NULL, status varchar(16) NOT NULL,
			failure_reason text, created_at timestamptz, updated_at timestamptz
		);
		CREATE TABLE budget_temporary_overrides (
			id uuid PRIMARY KEY, budget_allocation_id uuid NOT NULL, dimension_type varchar(8) NOT NULL,
			dimension_key varchar(48) NOT NULL, model_tier_id uuid, period_type varchar(8) NOT NULL,
			target_limit_usd numeric(18,6) NOT NULL, starts_at timestamptz NOT NULL,
			expires_at timestamptz NOT NULL, expansion_request_id uuid, status varchar(16) NOT NULL,
			failure_reason text, created_at timestamptz, updated_at timestamptz
		);
	`).Error)
	allocationID, scheduleID, overrideID := uuid.New(), uuid.New(), uuid.New()
	now := time.Now().UTC()
	require.NoError(t, db.Exec(`INSERT INTO budget_allocations (id) VALUES (?)`, allocationID).Error)
	require.NoError(t, db.Exec(`
		INSERT INTO budget_limit_schedules
			(id, budget_allocation_id, dimension_type, dimension_key, period_type, target_limit_usd,
			 effective_at, reason, created_by, status)
		VALUES (?, ?, 'total', 'total', 'monthly', 100, ?, 'test', ?, 'pending')
	`, scheduleID, allocationID, now.Add(-time.Minute), uuid.New()).Error)

	tierRepo := repository.NewBudgetTierRepo(db)
	budgetRepo := repository.NewBudgetRepo(db)
	runWhileConfigReplaces := func(updateSQL string, args ...any) service.BudgetLifecycleResult {
		t.Helper()
		configTx := db.Begin()
		require.NoError(t, configTx.Error)
		defer configTx.Rollback()
		var lockedID string
		require.NoError(t, configTx.Raw(`SELECT id FROM budget_allocations WHERE id = ? FOR UPDATE`, allocationID).Scan(&lockedID).Error)
		type lifecycleRun struct {
			result service.BudgetLifecycleResult
			err    error
		}
		done := make(chan lifecycleRun, 1)
		go func() {
			executor := service.NewBudgetLifecycleExecutor(
				tierRepo, budgetRepo, tierRepo, nil, service.NoopBudgetLifecycleObserver{},
			)
			result, runErr := executor.RunOnce(context.Background(), now, 10)
			done <- lifecycleRun{result: result, err: runErr}
		}()
		time.Sleep(150 * time.Millisecond)
		updateCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		require.NoError(t, configTx.WithContext(updateCtx).Exec(updateSQL, args...).Error)
		require.NoError(t, configTx.Commit().Error)
		select {
		case run := <-done:
			require.NoError(t, run.err)
			return run.result
		case <-time.After(5 * time.Second):
			t.Fatal("lifecycle/config transactions did not complete")
		}
		return service.BudgetLifecycleResult{}
	}

	scheduleResult := runWhileConfigReplaces(
		`UPDATE budget_limit_schedules SET status = 'replaced' WHERE id = ?`, scheduleID,
	)
	require.Equal(t, 1, scheduleResult.SchedulesSkipped)
	require.Zero(t, scheduleResult.SchedulesFailed)
	var scheduleStatus string
	require.NoError(t, db.Raw(`SELECT status FROM budget_limit_schedules WHERE id = ?`, scheduleID).Scan(&scheduleStatus).Error)
	require.Equal(t, string(budgetdomain.ScheduleReplaced), scheduleStatus)

	require.NoError(t, db.Exec(`
		INSERT INTO budget_temporary_overrides
			(id, budget_allocation_id, dimension_type, dimension_key, period_type, target_limit_usd,
			 starts_at, expires_at, status)
		VALUES (?, ?, 'total', 'total', 'monthly', 100, ?, ?, 'active')
	`, overrideID, allocationID, now.Add(-time.Hour), now.Add(-time.Minute)).Error)
	overrideResult := runWhileConfigReplaces(
		`UPDATE budget_temporary_overrides SET status = 'cancelled' WHERE id = ?`, overrideID,
	)
	require.Equal(t, 1, overrideResult.OverridesSkipped)
	require.Zero(t, overrideResult.OverridesFailed)
	var overrideStatus string
	require.NoError(t, db.Raw(`SELECT status FROM budget_temporary_overrides WHERE id = ?`, overrideID).Scan(&overrideStatus).Error)
	require.Equal(t, string(budgetdomain.OverrideCancelled), overrideStatus)
}

func postgresIntegrationDSN(t *testing.T) string {
	t.Helper()
	if dsn := os.Getenv("TEST_DATABASE_URL"); dsn != "" {
		return dsn
	}
	container := "llm-gateway-budget-test-" + strings.ReplaceAll(uuid.NewString()[:12], "-", "")
	run := exec.Command("docker", "run", "--rm", "-d", "--name", container, "-e", "POSTGRES_PASSWORD=postgres", "-P", "postgres:16")
	output, err := run.CombinedOutput()
	require.NoError(t, err, string(output))
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", container).Run() })
	portOutput, err := exec.Command("docker", "port", container, "5432/tcp").CombinedOutput()
	require.NoError(t, err, string(portOutput))
	parts := strings.Split(strings.TrimSpace(string(portOutput)), ":")
	require.NotEmpty(t, parts)
	port := parts[len(parts)-1]
	for attempt := 0; attempt < 60; attempt++ {
		if exec.Command("docker", "exec", container, "pg_isready", "-U", "postgres").Run() == nil {
			// pg_isready may turn green a fraction before the published host port
			// reliably completes a PostgreSQL startup handshake.
			time.Sleep(500 * time.Millisecond)
			return "postgres://postgres:postgres@127.0.0.1:" + port + "/postgres?sslmode=disable"
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("temporary PostgreSQL did not become ready")
	return ""
}
