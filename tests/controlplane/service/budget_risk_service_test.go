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
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestBudgetRiskListMergesOverrideAlertAndBlockSources(t *testing.T) {
	now := time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE call_logs (
		status integer, budget_tier_id text, budget_block_code text,
		budget_block_dimension text, budget_block_scope_type text,
		budget_block_scope_id text, budget_block_period_type text, created_at datetime
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE budget_alert_records (
		id text primary key, scope_type text, scope_id text, period_type text, period text,
		threshold integer, dimension_key text, dimension_type text, model_tier_id text, notified_at datetime
	)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO budget_alert_records VALUES (?,?,?,?,?,?,?,?,?,?)`,
		uuid.New(), model.BudgetScopeCompany, model.BudgetScopeGlobal, "monthly", "2026-07",
		80, "total", "total", nil, now.Add(-time.Hour)).Error)
	require.NoError(t, db.Exec(`INSERT INTO call_logs VALUES (402,NULL,?,?,?,?,?,?)`,
		"budget_exceeded", "total", model.BudgetScopeCompany, model.BudgetScopeGlobal, "monthly", now).Error)

	allocationID := uuid.New()
	allocRepo := newFakeAllocRepo()
	allocRepo.allocations[allocationKey(model.BudgetScopeCompany, model.BudgetScopeGlobal)] = &model.BudgetAllocation{
		ID: allocationID, ScopeType: model.BudgetScopeCompany, ScopeID: model.BudgetScopeGlobal,
		AmountUSD: 100, DailyAmountUSD: -1, WeeklyAmountUSD: -1, Enabled: true,
	}
	budgetSvc := service.NewBudgetService(allocRepo, newFakeAlertRepo())
	mini := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	budgetSvc.SetRedis(redisClient)
	require.NoError(t, redisClient.Set(context.Background(),
		"budget:v2:used:company:GLOBAL:2026-07", int64(100_000_000), 0).Err())

	tierStore := &myBudgetStore{
		limits: map[uuid.UUID][]model.BudgetTierLimit{},
		overrides: map[uuid.UUID][]model.BudgetTemporaryOverride{allocationID: {
			{
				BudgetAllocationID: allocationID, DimensionType: budgetdomain.DimensionTotal,
				DimensionKey: "total", PeriodType: budgetdomain.PeriodMonthly,
				TargetLimitUSD: mustAmount(t, "120"), StartsAt: now.Add(-time.Hour),
				ExpiresAt: now.Add(time.Hour), Status: budgetdomain.OverrideActive,
			},
		}},
	}
	tierSvc := service.NewBudgetTierService(tierStore, myBudgetReader{}, newFakeModelTierRepo(), func() time.Time { return now })
	riskSvc := service.NewBudgetRiskService(repository.NewBudgetRiskRepo(db), budgetSvc, tierSvc, func() time.Time { return now })

	page, err := riskSvc.List(context.Background(), service.ActorScope{Role: "admin", IsGlobal: true}, service.RiskFilter{
		ScopeType: model.BudgetScopeCompany, ScopeID: model.BudgetScopeGlobal, PeriodType: "monthly",
	})
	require.NoError(t, err)
	require.Equal(t, 2, page.Total)
	sources := map[string]bool{}
	types := map[string]bool{}
	thresholdCount := 0
	for _, item := range page.Items {
		sources[item.LimitSource] = true
		types[item.RiskType] = true
		require.Equal(t, model.BudgetScopeGlobal, item.ScopeName)
		require.NotEmpty(t, item.EffectiveLimitUSD)
		if item.RiskType == "threshold_reached" {
			thresholdCount++
			require.Equal(t, now.Add(-time.Hour), item.DetectedAt)
		}
	}
	require.True(t, sources["temporary_override"])
	require.True(t, types["threshold_reached"])
	require.True(t, types["request_blocked"])
	require.Equal(t, 1, thresholdCount)
}

func TestBudgetRiskHistoricalAlertDoesNotStayActiveAfterUsageRecovers(t *testing.T) {
	now := time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE call_logs (
		status integer, budget_tier_id text, budget_block_code text,
		budget_block_dimension text, budget_block_scope_type text,
		budget_block_scope_id text, budget_block_period_type text, created_at datetime
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE budget_alert_records (
		id text primary key, scope_type text, scope_id text, period_type text, period text,
		threshold integer, dimension_key text, dimension_type text, model_tier_id text, notified_at datetime
	)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO budget_alert_records VALUES (?,?,?,?,?,?,?,?,?,?)`,
		uuid.New(), model.BudgetScopeCompany, model.BudgetScopeGlobal, "monthly", "2026-07",
		80, "total", "total", nil, now.Add(-time.Hour)).Error)
	allocationID := uuid.New()
	allocRepo := newFakeAllocRepo()
	allocRepo.allocations[allocationKey(model.BudgetScopeCompany, model.BudgetScopeGlobal)] = &model.BudgetAllocation{
		ID: allocationID, ScopeType: model.BudgetScopeCompany, ScopeID: model.BudgetScopeGlobal,
		AmountUSD: 100, DailyAmountUSD: -1, WeeklyAmountUSD: -1, Enabled: true,
	}
	budgetSvc := service.NewBudgetService(allocRepo, newFakeAlertRepo())
	mini := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	budgetSvc.SetRedis(redisClient)
	require.NoError(t, redisClient.Set(context.Background(),
		"budget:v2:used:company:GLOBAL:2026-07", int64(10_000_000), 0).Err())
	tierSvc := service.NewBudgetTierService(
		&myBudgetStore{limits: map[uuid.UUID][]model.BudgetTierLimit{}, overrides: map[uuid.UUID][]model.BudgetTemporaryOverride{}},
		myBudgetReader{}, newFakeModelTierRepo(), func() time.Time { return now },
	)
	riskSvc := service.NewBudgetRiskService(repository.NewBudgetRiskRepo(db), budgetSvc, tierSvc, func() time.Time { return now })

	page, err := riskSvc.List(context.Background(), service.ActorScope{Role: "admin", IsGlobal: true}, service.RiskFilter{
		ScopeType: model.BudgetScopeCompany, ScopeID: model.BudgetScopeGlobal, PeriodType: "monthly",
	})

	require.NoError(t, err)
	require.Empty(t, page.Items)
}
