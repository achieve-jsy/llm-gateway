package service_test

import (
	"context"
	"fmt"
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
		budget_block_scope_id text, budget_block_period_type text, ts datetime
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
		require.Equal(t, "全公司", item.ScopeName)
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

func TestRiskEventCreatedWhenBudgetApproachesLimit(t *testing.T) {
	now := time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE call_logs (
		status integer, budget_tier_id text, budget_block_code text,
		budget_block_dimension text, budget_block_scope_type text,
		budget_block_scope_id text, budget_block_period_type text, ts datetime
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE budget_alert_records (
		id text primary key, scope_type text, scope_id text, period_type text, period text,
		threshold integer, dimension_key text, dimension_type text, model_tier_id text, notified_at datetime
	)`).Error)

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
	// Set usage at 85% — above 80% threshold
	require.NoError(t, redisClient.Set(context.Background(),
		"budget:v2:used:company:GLOBAL:2026-07", int64(85_000_000), 0).Err())

	tierSvc := service.NewBudgetTierService(
		&myBudgetStore{limits: map[uuid.UUID][]model.BudgetTierLimit{}, overrides: map[uuid.UUID][]model.BudgetTemporaryOverride{}},
		myBudgetReader{}, newFakeModelTierRepo(), func() time.Time { return now },
	)
	riskSvc := service.NewBudgetRiskService(repository.NewBudgetRiskRepo(db), budgetSvc, tierSvc, func() time.Time { return now })

	page, err := riskSvc.List(context.Background(), service.ActorScope{Role: "admin", IsGlobal: true}, service.RiskFilter{
		ScopeType: model.BudgetScopeCompany, ScopeID: model.BudgetScopeGlobal, PeriodType: "monthly",
	})
	require.NoError(t, err)
	require.NotEmpty(t, page.Items)
	found := false
	for _, item := range page.Items {
		if item.RiskType == "threshold_reached" && item.Severity == "warning" {
			found = true
			require.Equal(t, "85.000000", item.UsedUSD)
			require.Equal(t, "100.000000", item.EffectiveLimitUSD)
		}
	}
	require.True(t, found, "expected threshold_reached warning event at 85% usage")
}

func TestRiskEventCreatedWhenBudgetExhausted(t *testing.T) {
	now := time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE call_logs (
		status integer, budget_tier_id text, budget_block_code text,
		budget_block_dimension text, budget_block_scope_type text,
		budget_block_scope_id text, budget_block_period_type text, ts datetime
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE budget_alert_records (
		id text primary key, scope_type text, scope_id text, period_type text, period text,
		threshold integer, dimension_key text, dimension_type text, model_tier_id text, notified_at datetime
	)`).Error)

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
	// Set usage at 100% — triggers budget_exhausted/critical
	require.NoError(t, redisClient.Set(context.Background(),
		"budget:v2:used:company:GLOBAL:2026-07", int64(100_000_000), 0).Err())

	tierSvc := service.NewBudgetTierService(
		&myBudgetStore{limits: map[uuid.UUID][]model.BudgetTierLimit{}, overrides: map[uuid.UUID][]model.BudgetTemporaryOverride{}},
		myBudgetReader{}, newFakeModelTierRepo(), func() time.Time { return now },
	)
	riskSvc := service.NewBudgetRiskService(repository.NewBudgetRiskRepo(db), budgetSvc, tierSvc, func() time.Time { return now })

	page, err := riskSvc.List(context.Background(), service.ActorScope{Role: "admin", IsGlobal: true}, service.RiskFilter{
		ScopeType: model.BudgetScopeCompany, ScopeID: model.BudgetScopeGlobal, PeriodType: "monthly",
	})
	require.NoError(t, err)
	found := false
	for _, item := range page.Items {
		if item.RiskType == "budget_exhausted" && item.Severity == "critical" {
			found = true
		}
	}
	require.True(t, found, "expected budget_exhausted critical event at 100% usage")
}

func TestRiskEventCreatedWhenRequestIsBlocked(t *testing.T) {
	now := time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE call_logs (
		status integer, budget_tier_id text, budget_block_code text,
		budget_block_dimension text, budget_block_scope_type text,
		budget_block_scope_id text, budget_block_period_type text, ts datetime
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE budget_alert_records (
		id text primary key, scope_type text, scope_id text, period_type text, period text,
		threshold integer, dimension_key text, dimension_type text, model_tier_id text, notified_at datetime
	)`).Error)
	// Insert blocked call_log entries (402 status with budget_exceeded code)
	for range 5 {
		require.NoError(t, db.Exec(`INSERT INTO call_logs VALUES (402,NULL,?,?,?,?,?,?)`,
			"budget_exceeded", "total", model.BudgetScopeCompany, model.BudgetScopeGlobal, "monthly", now).Error)
	}

	allocRepo := newFakeAllocRepo()
	allocRepo.allocations[allocationKey(model.BudgetScopeCompany, model.BudgetScopeGlobal)] = &model.BudgetAllocation{
		ID: uuid.New(), ScopeType: model.BudgetScopeCompany, ScopeID: model.BudgetScopeGlobal,
		AmountUSD: 100, DailyAmountUSD: -1, WeeklyAmountUSD: -1, Enabled: true,
	}
	budgetSvc := service.NewBudgetService(allocRepo, newFakeAlertRepo())
	mini := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	budgetSvc.SetRedis(redisClient)
	require.NoError(t, redisClient.Set(context.Background(),
		"budget:v2:used:company:GLOBAL:2026-07", int64(100_000_000), 0).Err())

	tierSvc := service.NewBudgetTierService(
		&myBudgetStore{limits: map[uuid.UUID][]model.BudgetTierLimit{}, overrides: map[uuid.UUID][]model.BudgetTemporaryOverride{}},
		myBudgetReader{}, newFakeModelTierRepo(), func() time.Time { return now },
	)
	riskSvc := service.NewBudgetRiskService(repository.NewBudgetRiskRepo(db), budgetSvc, tierSvc, func() time.Time { return now })

	page, err := riskSvc.List(context.Background(), service.ActorScope{Role: "admin", IsGlobal: true}, service.RiskFilter{
		ScopeType: model.BudgetScopeCompany, ScopeID: model.BudgetScopeGlobal, PeriodType: "monthly",
	})
	require.NoError(t, err)
	var blockedCount int
	for _, item := range page.Items {
		if item.RiskType == "request_blocked" {
			blockedCount++
			require.Equal(t, "critical", item.Severity)
			require.EqualValues(t, 5, item.BlockedRequests)
		}
	}
	require.Equal(t, 1, blockedCount, "expected exactly 1 request_blocked event aggregating 5 blocked calls")
}

func TestRiskSummaryAggregatesBySeverityAndType(t *testing.T) {
	now := time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE call_logs (
		status integer, budget_tier_id text, budget_block_code text,
		budget_block_dimension text, budget_block_scope_type text,
		budget_block_scope_id text, budget_block_period_type text, ts datetime
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE budget_alert_records (
		id text primary key, scope_type text, scope_id text, period_type text, period text,
		threshold integer, dimension_key text, dimension_type text, model_tier_id text, notified_at datetime
	)`).Error)
	// Add 2 blocked call_logs
	for range 2 {
		require.NoError(t, db.Exec(`INSERT INTO call_logs VALUES (402,NULL,?,?,?,?,?,?)`,
			"budget_exceeded", "total", model.BudgetScopeCompany, model.BudgetScopeGlobal, "monthly", now).Error)
	}

	allocRepo := newFakeAllocRepo()
	allocRepo.allocations[allocationKey(model.BudgetScopeCompany, model.BudgetScopeGlobal)] = &model.BudgetAllocation{
		ID: uuid.New(), ScopeType: model.BudgetScopeCompany, ScopeID: model.BudgetScopeGlobal,
		AmountUSD: 100, DailyAmountUSD: -1, WeeklyAmountUSD: -1, Enabled: true,
	}
	budgetSvc := service.NewBudgetService(allocRepo, newFakeAlertRepo())
	mini := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	budgetSvc.SetRedis(redisClient)
	// 85% usage triggers threshold_reached warning, plus blocked events are critical
	require.NoError(t, redisClient.Set(context.Background(),
		"budget:v2:used:company:GLOBAL:2026-07", int64(85_000_000), 0).Err())

	tierSvc := service.NewBudgetTierService(
		&myBudgetStore{limits: map[uuid.UUID][]model.BudgetTierLimit{}, overrides: map[uuid.UUID][]model.BudgetTemporaryOverride{}},
		myBudgetReader{}, newFakeModelTierRepo(), func() time.Time { return now },
	)
	riskSvc := service.NewBudgetRiskService(repository.NewBudgetRiskRepo(db), budgetSvc, tierSvc, func() time.Time { return now })

	summary, err := riskSvc.Summary(context.Background(), service.ActorScope{Role: "admin", IsGlobal: true}, service.RiskFilter{
		ScopeType: model.BudgetScopeCompany, ScopeID: model.BudgetScopeGlobal, PeriodType: "monthly",
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, summary.Total, 2)
	require.Greater(t, summary.Warning, 0, "expected at least one warning (threshold_reached)")
	require.Greater(t, summary.Critical, 0, "expected at least one critical (request_blocked)")
	require.GreaterOrEqual(t, summary.ByType["threshold_reached"], 1)
	require.GreaterOrEqual(t, summary.ByType["request_blocked"], 1)
}

func TestRiskEventDeduplicatesAlertAndThresholdSources(t *testing.T) {
	// When both a budget_alert_record and a threshold projection exist for the
	// same identity (scope+period+dimension+tier), they should merge into a
	// single event — not duplicate.
	now := time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE call_logs (
		status integer, budget_tier_id text, budget_block_code text,
		budget_block_dimension text, budget_block_scope_type text,
		budget_block_scope_id text, budget_block_period_type text, ts datetime
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE budget_alert_records (
		id text primary key, scope_type text, scope_id text, period_type text, period text,
		threshold integer, dimension_key text, dimension_type text, model_tier_id text, notified_at datetime
	)`).Error)
	// Alert record for the same identity as the threshold projection
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
		"budget:v2:used:company:GLOBAL:2026-07", int64(85_000_000), 0).Err())

	tierSvc := service.NewBudgetTierService(
		&myBudgetStore{limits: map[uuid.UUID][]model.BudgetTierLimit{}, overrides: map[uuid.UUID][]model.BudgetTemporaryOverride{}},
		myBudgetReader{}, newFakeModelTierRepo(), func() time.Time { return now },
	)
	riskSvc := service.NewBudgetRiskService(repository.NewBudgetRiskRepo(db), budgetSvc, tierSvc, func() time.Time { return now })

	page, err := riskSvc.List(context.Background(), service.ActorScope{Role: "admin", IsGlobal: true}, service.RiskFilter{
		ScopeType: model.BudgetScopeCompany, ScopeID: model.BudgetScopeGlobal, PeriodType: "monthly",
	})
	require.NoError(t, err)
	// Only 1 threshold event — the alert record merged its detected_at into it, not a duplicate
	var thresholdCount int
	for _, item := range page.Items {
		if item.RiskType == "threshold_reached" || item.RiskType == "budget_exhausted" {
			thresholdCount++
		}
	}
	require.Equal(t, 1, thresholdCount, "alert and threshold should deduplicate into 1 event")
}

func TestRiskNoEventBelowThreshold(t *testing.T) {
	now := time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE call_logs (
		status integer, budget_tier_id text, budget_block_code text,
		budget_block_dimension text, budget_block_scope_type text,
		budget_block_scope_id text, budget_block_period_type text, ts datetime
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE budget_alert_records (
		id text primary key, scope_type text, scope_id text, period_type text, period text,
		threshold integer, dimension_key text, dimension_type text, model_tier_id text, notified_at datetime
	)`).Error)

	allocRepo := newFakeAllocRepo()
	allocRepo.allocations[allocationKey(model.BudgetScopeCompany, model.BudgetScopeGlobal)] = &model.BudgetAllocation{
		ID: uuid.New(), ScopeType: model.BudgetScopeCompany, ScopeID: model.BudgetScopeGlobal,
		AmountUSD: 100, DailyAmountUSD: -1, WeeklyAmountUSD: -1, Enabled: true,
	}
	budgetSvc := service.NewBudgetService(allocRepo, newFakeAlertRepo())
	mini := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	budgetSvc.SetRedis(redisClient)
	// 10% usage — well below 80% threshold
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
	require.Empty(t, page.Items, "no risk events expected below 80% threshold")
}

func TestBudgetRiskHistoricalAlertDoesNotStayActiveAfterUsageRecovers(t *testing.T) {
	now := time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE call_logs (
		status integer, budget_tier_id text, budget_block_code text,
		budget_block_dimension text, budget_block_scope_type text,
		budget_block_scope_id text, budget_block_period_type text, ts datetime
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

func TestBudgetRiskIgnoresAlertRecordsFromOtherPeriods(t *testing.T) {
	now := time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE call_logs (
		status integer, budget_tier_id text, budget_block_code text,
		budget_block_dimension text, budget_block_scope_type text,
		budget_block_scope_id text, budget_block_period_type text, ts datetime
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE budget_alert_records (
		id text primary key, scope_type text, scope_id text, period_type text, period text,
		threshold integer, dimension_key text, dimension_type text, model_tier_id text, notified_at datetime
	)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO budget_alert_records VALUES (?,?,?,?,?,?,?,?,?,?)`,
		uuid.New(), model.BudgetScopeCompany, model.BudgetScopeGlobal, "monthly", "2026-06",
		80, "total", "total", nil, now.Add(-30*24*time.Hour)).Error)

	allocRepo := newFakeAllocRepo()
	allocRepo.allocations[allocationKey(model.BudgetScopeCompany, model.BudgetScopeGlobal)] = &model.BudgetAllocation{
		ID: uuid.New(), ScopeType: model.BudgetScopeCompany, ScopeID: model.BudgetScopeGlobal,
		AmountUSD: 100, DailyAmountUSD: -1, WeeklyAmountUSD: -1, Enabled: true,
	}
	budgetSvc := service.NewBudgetService(allocRepo, newFakeAlertRepo())
	mini := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	budgetSvc.SetRedis(redisClient)
	require.NoError(t, redisClient.Set(context.Background(),
		"budget:v2:used:company:GLOBAL:2026-07", int64(85_000_000), 0).Err())

	tierSvc := service.NewBudgetTierService(
		&myBudgetStore{limits: map[uuid.UUID][]model.BudgetTierLimit{}, overrides: map[uuid.UUID][]model.BudgetTemporaryOverride{}},
		myBudgetReader{}, newFakeModelTierRepo(), func() time.Time { return now },
	)
	riskSvc := service.NewBudgetRiskService(repository.NewBudgetRiskRepo(db), budgetSvc, tierSvc, func() time.Time { return now })

	page, err := riskSvc.List(context.Background(), service.ActorScope{Role: "admin", IsGlobal: true}, service.RiskFilter{
		ScopeType: model.BudgetScopeCompany, ScopeID: model.BudgetScopeGlobal, PeriodType: "monthly",
	})

	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, "threshold_reached", page.Items[0].RiskType)
	require.Equal(t, now, page.Items[0].DetectedAt)
}

func TestBudgetRiskPaginationDoesNotAffectSummary(t *testing.T) {
	now := time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE call_logs (
		status integer, budget_tier_id text, budget_block_code text,
		budget_block_dimension text, budget_block_scope_type text,
		budget_block_scope_id text, budget_block_period_type text, ts datetime
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE budget_alert_records (
		id text primary key, scope_type text, scope_id text, period_type text, period text,
		threshold integer, dimension_key text, dimension_type text, model_tier_id text, notified_at datetime
	)`).Error)

	allocRepo := newFakeAllocRepo()
	budgetSvc := service.NewBudgetService(allocRepo, newFakeAlertRepo())
	mini := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	budgetSvc.SetRedis(redisClient)
	for _, scopeID := range []string{"member-1", "member-2"} {
		allocRepo.allocations[allocationKey(model.BudgetScopeMember, scopeID)] = &model.BudgetAllocation{
			ID: uuid.New(), ScopeType: model.BudgetScopeMember, ScopeID: scopeID,
			AmountUSD: 100, DailyAmountUSD: -1, WeeklyAmountUSD: -1, Enabled: true,
		}
		require.NoError(t, redisClient.Set(context.Background(),
			"budget:v2:used:member:"+scopeID+":2026-07", int64(85_000_000), 0).Err())
	}

	tierSvc := service.NewBudgetTierService(
		&myBudgetStore{limits: map[uuid.UUID][]model.BudgetTierLimit{}, overrides: map[uuid.UUID][]model.BudgetTemporaryOverride{}},
		myBudgetReader{}, newFakeModelTierRepo(), func() time.Time { return now },
	)
	riskSvc := service.NewBudgetRiskService(repository.NewBudgetRiskRepo(db), budgetSvc, tierSvc, func() time.Time { return now })

	page, err := riskSvc.List(context.Background(), service.ActorScope{Role: "admin", IsGlobal: true}, service.RiskFilter{
		ScopeType: model.BudgetScopeMember, PeriodType: "monthly", Page: 1, PageSize: 1,
	})
	require.NoError(t, err)
	require.Equal(t, 2, page.Total)
	require.Len(t, page.Items, 1)
	require.Equal(t, 1, page.Page)
	require.Equal(t, 1, page.PageSize)

	summary, err := riskSvc.Summary(context.Background(), service.ActorScope{Role: "admin", IsGlobal: true}, service.RiskFilter{
		ScopeType: model.BudgetScopeMember, PeriodType: "monthly", Page: 1, PageSize: 1,
	})
	require.NoError(t, err)
	require.Equal(t, 2, summary.Total)
	require.Equal(t, 2, summary.Warning)
}

func TestBudgetRiskReturnsUsageUnavailableWhenRedisFailsWithoutLastKnown(t *testing.T) {
	now := time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)
	db := newBudgetRiskTestDB(t)
	allocRepo := newFakeAllocRepo()
	allocRepo.allocations[allocationKey(model.BudgetScopeCompany, model.BudgetScopeGlobal)] = &model.BudgetAllocation{
		ID: uuid.New(), ScopeType: model.BudgetScopeCompany, ScopeID: model.BudgetScopeGlobal,
		AmountUSD: 100, DailyAmountUSD: -1, WeeklyAmountUSD: -1, Enabled: true,
	}
	budgetSvc := service.NewBudgetService(allocRepo, newFakeAlertRepo())
	mini, err := miniredis.Run()
	require.NoError(t, err)
	redisClient := redis.NewClient(&redis.Options{
		Addr: mini.Addr(), DialTimeout: 20 * time.Millisecond,
		ReadTimeout: 20 * time.Millisecond, WriteTimeout: 20 * time.Millisecond,
	})
	t.Cleanup(func() { _ = redisClient.Close() })
	mini.Close()
	budgetSvc.SetRedis(redisClient)
	tierSvc := service.NewBudgetTierService(
		&myBudgetStore{limits: map[uuid.UUID][]model.BudgetTierLimit{}, overrides: map[uuid.UUID][]model.BudgetTemporaryOverride{}},
		myBudgetReader{}, newFakeModelTierRepo(), func() time.Time { return now },
	)
	riskSvc := service.NewBudgetRiskService(repository.NewBudgetRiskRepo(db), budgetSvc, tierSvc, func() time.Time { return now })

	page, err := riskSvc.List(context.Background(), service.ActorScope{Role: "admin", IsGlobal: true}, service.RiskFilter{
		ScopeType: model.BudgetScopeCompany, ScopeID: model.BudgetScopeGlobal, PeriodType: "monthly",
	})

	require.ErrorIs(t, err, service.ErrBudgetUsageUnavailable)
	require.Nil(t, page)
}

func TestBudgetRiskUsesLastKnownUsageAndMarksStaleWhenRedisFails(t *testing.T) {
	now := time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)
	later := now.Add(5 * time.Minute)
	db := newBudgetRiskTestDB(t)
	allocRepo := newFakeAllocRepo()
	allocRepo.allocations[allocationKey(model.BudgetScopeCompany, model.BudgetScopeGlobal)] = &model.BudgetAllocation{
		ID: uuid.New(), ScopeType: model.BudgetScopeCompany, ScopeID: model.BudgetScopeGlobal,
		AmountUSD: 100, DailyAmountUSD: -1, WeeklyAmountUSD: -1, Enabled: true,
	}
	budgetSvc := service.NewBudgetService(allocRepo, newFakeAlertRepo())
	mini, err := miniredis.Run()
	require.NoError(t, err)
	redisClient := redis.NewClient(&redis.Options{
		Addr: mini.Addr(), DialTimeout: 20 * time.Millisecond,
		ReadTimeout: 20 * time.Millisecond, WriteTimeout: 20 * time.Millisecond,
	})
	t.Cleanup(func() { _ = redisClient.Close() })
	budgetSvc.SetRedis(redisClient)
	require.NoError(t, redisClient.Set(context.Background(),
		"budget:v2:used:company:GLOBAL:2026-07", int64(85_000_000), 0).Err())
	tierSvc := service.NewBudgetTierService(
		&myBudgetStore{limits: map[uuid.UUID][]model.BudgetTierLimit{}, overrides: map[uuid.UUID][]model.BudgetTemporaryOverride{}},
		myBudgetReader{}, newFakeModelTierRepo(), func() time.Time { return now },
	)
	clock := func() time.Time { return now }
	riskSvc := service.NewBudgetRiskService(repository.NewBudgetRiskRepo(db), budgetSvc, tierSvc, func() time.Time { return clock() })

	fresh, err := riskSvc.List(context.Background(), service.ActorScope{Role: "admin", IsGlobal: true}, service.RiskFilter{
		ScopeType: model.BudgetScopeCompany, ScopeID: model.BudgetScopeGlobal, PeriodType: "monthly",
	})
	require.NoError(t, err)
	require.False(t, fresh.Stale)
	require.Len(t, fresh.Items, 1)
	require.Equal(t, now, fresh.Items[0].DataAsOf)

	mini.Close()
	clock = func() time.Time { return later }
	stale, err := riskSvc.List(context.Background(), service.ActorScope{Role: "admin", IsGlobal: true}, service.RiskFilter{
		ScopeType: model.BudgetScopeCompany, ScopeID: model.BudgetScopeGlobal, PeriodType: "monthly",
	})

	require.NoError(t, err)
	require.True(t, stale.Stale)
	require.Len(t, stale.Items, 1)
	require.True(t, stale.Items[0].Stale)
	require.Equal(t, now, stale.Items[0].DataAsOf)
	require.Equal(t, "85.000000", stale.Items[0].UsedUSD)
}

func TestBudgetRiskFiltersSpecificModelTierDimension(t *testing.T) {
	now := time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)
	db := newBudgetRiskTestDB(t)
	allocationID := uuid.New()
	standardTierID, premiumTierID := uuid.New(), uuid.New()
	allocRepo := newFakeAllocRepo()
	allocRepo.allocations[allocationKey(model.BudgetScopeCompany, model.BudgetScopeGlobal)] = &model.BudgetAllocation{
		ID: allocationID, ScopeType: model.BudgetScopeCompany, ScopeID: model.BudgetScopeGlobal,
		AmountUSD: 1000, DailyAmountUSD: -1, WeeklyAmountUSD: -1, Enabled: true,
	}
	budgetSvc := service.NewBudgetService(allocRepo, newFakeAlertRepo())
	mini := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	budgetSvc.SetRedis(redisClient)
	require.NoError(t, redisClient.Set(context.Background(),
		"budget:v2:tier:used:company:GLOBAL:"+standardTierID.String()+":2026-07", int64(95_000_000), 0).Err())
	require.NoError(t, redisClient.Set(context.Background(),
		"budget:v2:tier:used:company:GLOBAL:"+premiumTierID.String()+":2026-07", int64(90_000_000), 0).Err())
	tierStore := &myBudgetStore{limits: map[uuid.UUID][]model.BudgetTierLimit{allocationID: {
		{
			ID: uuid.New(), BudgetAllocationID: allocationID, ModelTierID: standardTierID,
			PeriodType: budgetdomain.PeriodMonthly, LimitUSD: mustAmount(t, "100"), Enabled: true,
			EffectiveAt: now.Add(-time.Hour), ModelTier: model.ModelTier{ID: standardTierID, Name: "标准"},
		},
		{
			ID: uuid.New(), BudgetAllocationID: allocationID, ModelTierID: premiumTierID,
			PeriodType: budgetdomain.PeriodMonthly, LimitUSD: mustAmount(t, "100"), Enabled: true,
			EffectiveAt: now.Add(-time.Hour), ModelTier: model.ModelTier{ID: premiumTierID, Name: "高级"},
		},
	}}, overrides: map[uuid.UUID][]model.BudgetTemporaryOverride{}}
	tierSvc := service.NewBudgetTierService(tierStore, myBudgetReader{}, newFakeModelTierRepo(), func() time.Time { return now })
	riskSvc := service.NewBudgetRiskService(repository.NewBudgetRiskRepo(db), budgetSvc, tierSvc, func() time.Time { return now })

	page, err := riskSvc.List(context.Background(), service.ActorScope{Role: "admin", IsGlobal: true}, service.RiskFilter{
		ScopeType: model.BudgetScopeCompany, ScopeID: model.BudgetScopeGlobal, PeriodType: "monthly",
		DimensionType: string(budgetdomain.DimensionTier), ModelTierID: premiumTierID.String(),
	})

	require.NoError(t, err)
	require.Equal(t, 1, page.Total)
	require.Len(t, page.Items, 1)
	require.Equal(t, "高级", page.Items[0].ModelTierName)
	require.NotNil(t, page.Items[0].ModelTierID)
	require.Equal(t, premiumTierID.String(), *page.Items[0].ModelTierID)
	require.Equal(t, "90.000000", page.Items[0].UsedUSD)
}

func TestBudgetRiskResolvesScopeNamesFromItemsForGlobalAdmin(t *testing.T) {
	now := time.Date(2026, 7, 16, 10, 45, 0, 0, time.UTC)
	memberID := uuid.New()
	allocationID := uuid.New()
	db := newBudgetRiskTestDB(t)

	allocRepo := newFakeAllocRepo()
	allocRepo.allocations[allocationKey(model.BudgetScopeMember, memberID.String())] = &model.BudgetAllocation{
		ID: allocationID, ScopeType: model.BudgetScopeMember, ScopeID: memberID.String(),
		AmountUSD: 10, DailyAmountUSD: -1, WeeklyAmountUSD: -1, Enabled: true,
	}
	allocRepo.resolveScopeNames = func(scopeType string, scopeIDs []string) (map[string]string, error) {
		require.Equal(t, model.BudgetScopeMember, scopeType)
		require.Equal(t, []string{memberID.String()}, scopeIDs)
		return map[string]string{memberID.String(): "张三"}, nil
	}

	budgetSvc := service.NewBudgetService(allocRepo, newFakeAlertRepo())
	mini := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	budgetSvc.SetRedis(redisClient)
	require.NoError(t, redisClient.Set(context.Background(),
		fmt.Sprintf("budget:v2:used:member:%s:2026-07", memberID), int64(10_190_000), 0).Err())

	tierSvc := service.NewBudgetTierService(
		&myBudgetStore{limits: map[uuid.UUID][]model.BudgetTierLimit{}, overrides: map[uuid.UUID][]model.BudgetTemporaryOverride{}},
		myBudgetReader{}, newFakeModelTierRepo(), func() time.Time { return now },
	)
	riskSvc := service.NewBudgetRiskService(repository.NewBudgetRiskRepo(db), budgetSvc, tierSvc, func() time.Time { return now })

	page, err := riskSvc.List(context.Background(), service.ActorScope{Role: "admin", IsGlobal: true}, service.RiskFilter{
		PeriodType: "monthly",
	})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, "张三", page.Items[0].ScopeName)
	require.Equal(t, memberID.String(), page.Items[0].ScopeID)
}

func newBudgetRiskTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE call_logs (
		status integer, budget_tier_id text, budget_block_code text,
		budget_block_dimension text, budget_block_scope_type text,
		budget_block_scope_id text, budget_block_period_type text, ts datetime
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE budget_alert_records (
		id text primary key, scope_type text, scope_id text, period_type text, period text,
		threshold integer, dimension_key text, dimension_type text, model_tier_id text, notified_at datetime
	)`).Error)
	return db
}
