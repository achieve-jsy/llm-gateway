package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUpsertTierLimitKeepsIdentityAndEffectiveAt(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE budget_allocations (id text PRIMARY KEY)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE budget_tier_limits (
		id text PRIMARY KEY, budget_allocation_id text NOT NULL, model_tier_id text NOT NULL,
		period_type text NOT NULL, limit_usd numeric NOT NULL, effective_at datetime NOT NULL,
		enabled boolean NOT NULL, created_at datetime NOT NULL, updated_at datetime NOT NULL,
		UNIQUE(budget_allocation_id, model_tier_id, period_type)
	)`).Error)
	repo := repository.NewBudgetTierRepo(db)
	allocationID, tierID := uuid.New(), uuid.New()
	require.NoError(t, db.Exec(`INSERT INTO budget_allocations (id) VALUES (?)`, allocationID).Error)
	effectiveAt := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	first, err := repo.UpsertTierLimit(context.Background(), repository.UpsertTierLimitParams{
		BudgetAllocationID: allocationID, ModelTierID: tierID,
		PeriodType: budgetdomain.PeriodMonthly, LimitUSD: mustRepoAmount(t, "80"),
		EffectiveAt: effectiveAt, Enabled: true,
	})
	require.NoError(t, err)
	got, err := repo.UpsertTierLimit(context.Background(), repository.UpsertTierLimitParams{
		BudgetAllocationID: allocationID, ModelTierID: tierID,
		PeriodType: budgetdomain.PeriodMonthly, LimitUSD: mustRepoAmount(t, "100"),
		EffectiveAt: effectiveAt.Add(24 * time.Hour), Enabled: true,
	})
	require.NoError(t, err)
	require.Equal(t, first.ID, got.ID)
	require.True(t, got.EffectiveAt.Equal(effectiveAt))
	require.Equal(t, "100.000000", got.LimitUSD.String())
}

func TestCreatePersonalAllocationWithTierLimitRollsBackAllocation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE budget_allocations (
		id text PRIMARY KEY, scope_type text NOT NULL, scope_id text NOT NULL,
		amount_usd numeric NOT NULL, daily_amount_usd numeric NOT NULL, weekly_amount_usd numeric NOT NULL,
		period text NOT NULL, enabled boolean NOT NULL, created_by text, updated_by text,
		created_at datetime, updated_at datetime, UNIQUE(scope_type, scope_id)
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE budget_tier_limits (
		id text PRIMARY KEY, budget_allocation_id text NOT NULL, model_tier_id text NOT NULL,
		period_type text NOT NULL, limit_usd numeric NOT NULL, effective_at datetime NOT NULL,
		enabled boolean NOT NULL, created_at datetime, updated_at datetime,
		UNIQUE(budget_allocation_id, model_tier_id, period_type)
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TRIGGER reject_tier_limit BEFORE INSERT ON budget_tier_limits
		BEGIN SELECT RAISE(FAIL, 'tier write failed'); END`).Error)
	repo := repository.NewBudgetRepo(db)
	allocation := &model.BudgetAllocation{
		ID: uuid.New(), ScopeType: model.BudgetScopeMember, ScopeID: "member-a",
		AmountUSD: -1, DailyAmountUSD: -1, WeeklyAmountUSD: -1, Period: "monthly", Enabled: true,
	}
	err = repo.CreatePersonalAllocationWithTierLimit(context.Background(), allocation, repository.UpsertTierLimitParams{
		BudgetAllocationID: allocation.ID, ModelTierID: uuid.New(), PeriodType: budgetdomain.PeriodMonthly,
		LimitUSD: mustRepoAmount(t, "50"), EffectiveAt: time.Now(), Enabled: true,
	})
	require.Error(t, err)
	var count int64
	require.NoError(t, db.Model(&model.BudgetAllocation{}).Where("id = ?", allocation.ID).Count(&count).Error)
	require.Zero(t, count)
}

func TestPutPendingScheduleRequiresExplicitReplaceAndPreservesAuditRow(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE budget_allocations (id text PRIMARY KEY)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE budget_limit_schedules (
		id text PRIMARY KEY, budget_allocation_id text NOT NULL, dimension_type text NOT NULL,
		dimension_key text NOT NULL, model_tier_id text, period_type text NOT NULL,
		target_limit_usd numeric NOT NULL, effective_at datetime NOT NULL, reason text NOT NULL,
		created_by text NOT NULL, status text NOT NULL, failure_reason text,
		created_at datetime, updated_at datetime
	)`).Error)
	require.NoError(t, db.Exec(`CREATE UNIQUE INDEX uniq_budget_schedule_pending
		ON budget_limit_schedules(budget_allocation_id, dimension_key, period_type)
		WHERE status = 'pending'`).Error)
	repo := repository.NewBudgetTierRepo(db)
	allocationID, tierID := uuid.New(), uuid.New()
	require.NoError(t, db.Exec(`INSERT INTO budget_allocations (id) VALUES (?)`, allocationID).Error)
	buildSchedule := func(amount string) *model.BudgetLimitSchedule {
		return &model.BudgetLimitSchedule{
			ID: uuid.New(), BudgetAllocationID: allocationID, DimensionType: budgetdomain.DimensionTier,
			DimensionKey: "tier:" + tierID.String(), ModelTierID: &tierID, PeriodType: budgetdomain.PeriodMonthly,
			TargetLimitUSD: mustRepoAmount(t, amount), EffectiveAt: time.Now().Add(time.Hour),
			Reason: "next cycle", CreatedBy: uuid.New(), Status: budgetdomain.SchedulePending,
		}
	}
	first := buildSchedule("100")
	_, err = repo.PutPendingSchedule(context.Background(), first, false)
	require.NoError(t, err)
	second := buildSchedule("120")
	_, err = repo.PutPendingSchedule(context.Background(), second, false)
	require.True(t, errors.Is(err, repository.ErrPendingScheduleExists))
	var current model.BudgetLimitSchedule
	require.NoError(t, db.First(&current, "id = ?", first.ID).Error)
	require.Equal(t, budgetdomain.SchedulePending, current.Status)
	_, err = repo.PutPendingSchedule(context.Background(), second, true)
	require.NoError(t, err)
	current = model.BudgetLimitSchedule{}
	require.NoError(t, db.First(&current, "id = ?", first.ID).Error)
	require.Equal(t, budgetdomain.ScheduleReplaced, current.Status)
	current = model.BudgetLimitSchedule{}
	require.NoError(t, db.First(&current, "id = ?", second.ID).Error)
	require.Equal(t, budgetdomain.SchedulePending, current.Status)
}

func mustRepoAmount(t *testing.T, raw string) budgetdomain.Amount {
	t.Helper()
	amount, err := budgetdomain.ParseAmount(raw)
	require.NoError(t, err)
	return amount
}
