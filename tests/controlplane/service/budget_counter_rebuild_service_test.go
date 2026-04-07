package service_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/budgetperiod"
	"github.com/example/llm-gateway/internal/calllogstream"
	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	cpmodel "github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	gwbudget "github.com/example/llm-gateway/internal/gateway/budget"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestBudgetCounterRebuildV2_AggregatesSuccessfulEligibleCalls(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallLog{}))
	createRebuildBudgetTables(t, db)
	tierID := uuid.MustParse("00bb2e65-9c67-45f7-b061-d297a3944876")
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	allocation := cpmodel.BudgetAllocation{
		ID: uuid.New(), ScopeType: cpmodel.BudgetScopeMember, ScopeID: "u-1",
		AmountUSD: 100, DailyAmountUSD: 10, WeeklyAmountUSD: 20, Enabled: true,
	}
	require.NoError(t, db.Create(&allocation).Error)
	amount, err := budgetdomain.ParseAmount("25.000000")
	require.NoError(t, err)
	require.NoError(t, db.Create(&cpmodel.BudgetTierLimit{
		ID: uuid.New(), BudgetAllocationID: allocation.ID, ModelTierID: tierID,
		PeriodType: budgetdomain.PeriodMonthly, LimitUSD: amount,
		EffectiveAt: now.Add(-24 * time.Hour), Enabled: true,
	}).Error)
	rows := []obsmodel.CallLog{
		{
			RequestID: "ok", TS: now.Add(-time.Hour), Status: 201, UserID: "u-1", KeyType: "personal",
			EstCostUSD: 1.250001, BudgetTierID: &tierID, BudgetTierAccountingEligible: true,
			BudgetTierConstraintsSnapshot: []obsmodel.BudgetTierConstraintSnapshot{{
				RedisKey:  "budget:v2:tier:used:member:u-1:" + tierID.String() + ":2026-07",
				ScopeType: "member", ScopeID: "u-1", TierID: tierID.String(),
				PeriodType: "monthly", Period: "2026-07", ResetAt: time.Date(2026, 7, 31, 16, 0, 0, 0, time.UTC),
			}},
		},
		{RequestID: "failed", TS: now.Add(-time.Hour), Status: 500, UserID: "u-1", KeyType: "personal", EstCostUSD: 9, BudgetTierID: &tierID, BudgetTierAccountingEligible: true, BudgetTierConstraintsSnapshot: []obsmodel.BudgetTierConstraintSnapshot{}},
		{RequestID: "ineligible", TS: now.Add(-time.Hour), Status: 200, UserID: "u-1", KeyType: "personal", EstCostUSD: 2, BudgetTierID: &tierID, BudgetTierAccountingEligible: false, BudgetTierConstraintsSnapshot: []obsmodel.BudgetTierConstraintSnapshot{}},
	}
	require.NoError(t, db.Create(&rows).Error)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	result, err := service.NewBudgetCounterRebuildService(db, rdb, budgetperiod.Config{
		Timezone: "Asia/Shanghai", WeekStart: "monday",
	}).RebuildCurrentPeriods(context.Background(), now)
	require.NoError(t, err)
	require.Positive(t, result.KeysWritten)
	total, err := rdb.Get(context.Background(), activeCounterKey(t, rdb, "budget:v2:used:member:u-1:2026-07")).Int64()
	require.NoError(t, err)
	require.Equal(t, int64(3_250_001), total)
	tier, err := rdb.Get(context.Background(), activeCounterKey(t, rdb, "budget:v2:tier:used:member:u-1:"+tierID.String()+":2026-07")).Int64()
	require.NoError(t, err)
	require.Equal(t, int64(1_250_001), tier)
	require.Equal(t, int64(0), rdb.Exists(context.Background(),
		"budget:v2:used:company:GLOBAL:2026-07",
		"budget:v2:used:company:global:2026-07",
		"budget:v2:tier:used:member:u-1:"+tierID.String()+":2026-07-12",
	).Val())
}

func TestBudgetCounterRebuildV2_UsesEarliestCurrentPeriodStart(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:rebuild_range?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallLog{}))
	createRebuildBudgetTables(t, db)
	require.NoError(t, db.Create(&cpmodel.BudgetAllocation{
		ID: uuid.New(), ScopeType: cpmodel.BudgetScopeMember, ScopeID: "u-1",
		AmountUSD: 100, DailyAmountUSD: 10, WeeklyAmountUSD: 20, Enabled: true,
	}).Error)
	now := time.Date(2026, 7, 1, 4, 0, 0, 0, time.UTC) // 上海 7 月 1 日，ISO 周从 6 月 29 日开始
	require.NoError(t, db.Create(&obsmodel.CallLog{
		RequestID: "weekly-before-month", TS: time.Date(2026, 6, 29, 16, 0, 0, 0, time.UTC),
		Status: 200, UserID: "u-1", KeyType: "personal", EstCostUSD: 1,
	}).Error)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	_, err = service.NewBudgetCounterRebuildService(db, rdb, budgetperiod.Config{
		Timezone: "Asia/Shanghai", WeekStart: "monday",
	}).RebuildCurrentPeriods(context.Background(), now)
	require.NoError(t, err)
	weekly, err := rdb.Get(context.Background(), activeCounterKey(t, rdb, "budget:v2:used:member:u-1:2026-W27")).Int64()
	require.NoError(t, err)
	require.Equal(t, int64(1_000_000), weekly)
	require.Equal(t, "0", rdb.Get(context.Background(), activeCounterKey(t, rdb, "budget:v2:used:member:u-1:2026-07")).Val())
}

func TestBudgetCounterRebuildV2_PreservesEligibleUsageAfterTierLimitDisabled(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:rebuild_disabled_tier?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallLog{}))
	createRebuildBudgetTables(t, db)
	tierID := uuid.MustParse("00bb2e65-9c67-45f7-b061-d297a3944876")
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	allocation := cpmodel.BudgetAllocation{
		ID: uuid.New(), ScopeType: cpmodel.BudgetScopeMember, ScopeID: "u-1",
		AmountUSD: 100, DailyAmountUSD: -1, WeeklyAmountUSD: -1, Enabled: true,
	}
	require.NoError(t, db.Create(&allocation).Error)
	amount, err := budgetdomain.ParseAmount("25.000000")
	require.NoError(t, err)
	limit := cpmodel.BudgetTierLimit{
		ID: uuid.New(), BudgetAllocationID: allocation.ID, ModelTierID: tierID,
		PeriodType: budgetdomain.PeriodMonthly, LimitUSD: amount,
		EffectiveAt: now.Add(-24 * time.Hour), Enabled: true,
	}
	require.NoError(t, db.Create(&limit).Error)
	require.NoError(t, db.Model(&cpmodel.BudgetTierLimit{}).
		Where("id = ?", limit.ID).Update("enabled", false).Error)
	require.NoError(t, db.Create(&obsmodel.CallLog{
		RequestID: "eligible-before-disable", TS: now.Add(-time.Hour), Status: 200,
		UserID: "u-1", KeyType: "personal", EstCostUSD: 1.250001,
		BudgetTierID: &tierID, BudgetTierAccountingEligible: true,
		BudgetTierConstraintsSnapshot: []obsmodel.BudgetTierConstraintSnapshot{{
			RedisKey:  "budget:v2:tier:used:member:u-1:" + tierID.String() + ":2026-07",
			ScopeType: "member", ScopeID: "u-1", TierID: tierID.String(),
			PeriodType: "monthly", Period: "2026-07", ResetAt: time.Date(2026, 7, 31, 16, 0, 0, 0, time.UTC),
		}},
	}).Error)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	_, err = service.NewBudgetCounterRebuildService(db, rdb, budgetperiod.Config{
		Timezone: "Asia/Shanghai", WeekStart: "monday",
	}).RebuildCurrentPeriods(context.Background(), now)
	require.NoError(t, err)
	used, err := rdb.Get(context.Background(), activeCounterKey(t, rdb,
		"budget:v2:tier:used:member:u-1:"+tierID.String()+":2026-07")).Int64()
	require.NoError(t, err)
	require.Equal(t, int64(1_250_001), used)
}

func TestBudgetCounterRebuildV2_ReportsLegacyToV2ShadowDiff(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:rebuild_legacy_diff?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallLog{}))
	createRebuildBudgetTables(t, db)
	require.NoError(t, db.Create(&cpmodel.BudgetAllocation{
		ID: uuid.New(), ScopeType: cpmodel.BudgetScopeMember, ScopeID: "u-1",
		AmountUSD: 100, DailyAmountUSD: -1, WeeklyAmountUSD: -1, Enabled: true,
	}).Error)
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&obsmodel.CallLog{
		RequestID: "ok", TS: now.Add(-time.Hour), Status: 200,
		UserID: "u-1", KeyType: "personal", EstCostUSD: 1.5,
	}).Error)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	v2Key := "budget:v2:used:member:u-1:2026-07"
	require.NoError(t, rdb.Set(context.Background(), v2Key, "1000000", 0).Err())
	require.NoError(t, rdb.Set(context.Background(), "budget:used:member:u-1:2026-07", "1.250001", 0).Err())

	result, err := service.NewBudgetCounterRebuildService(db, rdb, budgetperiod.Config{
		Timezone: "Asia/Shanghai", WeekStart: "monday",
	}).RebuildCurrentPeriods(context.Background(), now)
	require.NoError(t, err)
	require.Len(t, result.LegacyV2Diffs, 1)
	require.Equal(t, v2Key, result.LegacyV2Diffs[0].Key)
	require.Equal(t, int64(1_250_001), result.LegacyV2Diffs[0].ExpectedMicroUSD)
	require.Equal(t, int64(1_000_000), result.LegacyV2Diffs[0].ActualMicroUSD)
	require.Equal(t, int64(250_001), result.LegacyV2Diffs[0].AbsDiffMicroUSD)
}

func TestBudgetCounterRebuildV2_UsesHistoricalTierConstraintFacts(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:rebuild_historical_constraints?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallLog{}))
	createRebuildBudgetTables(t, db)
	tierID := uuid.MustParse("00bb2e65-9c67-45f7-b061-d297a3944876")
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	resetAt := time.Date(2026, 7, 31, 16, 0, 0, 0, time.UTC)
	memberKey := "budget:v2:tier:used:member:u-1:" + tierID.String() + ":2026-07"
	companyKey := "budget:v2:tier:used:company:GLOBAL:" + tierID.String() + ":2026-07"
	require.NoError(t, db.Create(&obsmodel.CallLog{
		RequestID: "historical-facts", TS: now.Add(-time.Hour), Status: 200,
		UserID: "u-1", KeyType: "personal", EstCostUSD: 1.250001,
		BudgetTierID: &tierID, BudgetTierAccountingEligible: true,
		BudgetTierConstraintsSnapshot: []obsmodel.BudgetTierConstraintSnapshot{
			{RedisKey: memberKey, ScopeType: "member", ScopeID: "u-1", TierID: tierID.String(), PeriodType: "monthly", Period: "2026-07", ResetAt: resetAt},
			{RedisKey: companyKey, ScopeType: "company", ScopeID: "GLOBAL", TierID: tierID.String(), PeriodType: "monthly", Period: "2026-07", ResetAt: resetAt},
		},
	}).Error)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	result, err := service.NewBudgetCounterRebuildService(db, rdb, budgetperiod.Config{
		Timezone: "Asia/Shanghai", WeekStart: "monday",
	}).RebuildCurrentPeriods(context.Background(), now)

	require.NoError(t, err)
	require.Equal(t, 1, result.SourceRows)
	memberUsed, err := rdb.Get(context.Background(), activeCounterKey(t, rdb, memberKey)).Int64()
	require.NoError(t, err)
	companyUsed, err := rdb.Get(context.Background(), activeCounterKey(t, rdb, companyKey)).Int64()
	require.NoError(t, err)
	require.Equal(t, int64(1_250_001), memberUsed)
	require.Equal(t, int64(1_250_001), companyUsed)
}

func TestBudgetCounterRebuildV2_AtomicallyClearsKnownZeroKeyAndUsesResetTTL(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:rebuild_zero_keys?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallLog{}))
	createRebuildBudgetTables(t, db)
	require.NoError(t, db.Create(&cpmodel.BudgetAllocation{
		ID: uuid.New(), ScopeType: cpmodel.BudgetScopeMember, ScopeID: "u-zero",
		AmountUSD: 100, DailyAmountUSD: -1, WeeklyAmountUSD: -1, Enabled: true,
	}).Error)
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	key := "budget:v2:used:member:u-zero:2026-07"
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	require.NoError(t, rdb.Set(context.Background(), key, "9999999", 0).Err())

	result, err := service.NewBudgetCounterRebuildService(db, rdb, budgetperiod.Config{
		Timezone: "Asia/Shanghai", WeekStart: "monday",
	}).RebuildCurrentPeriods(context.Background(), now)

	require.NoError(t, err)
	require.Zero(t, result.SourceRows, "empty aggregation must remain distinguishable from a verified cutover")
	require.False(t, result.CutoverReady(), "empty aggregation must never be cutover ready")
	require.Equal(t, 1, result.KeysWritten)
	activeKey := activeCounterKey(t, rdb, key)
	require.Equal(t, "0", rdb.Get(context.Background(), activeKey).Val())
	window, err := budgetperiod.Resolve(now, budgetdomain.PeriodMonthly, budgetperiod.Config{
		Timezone: "Asia/Shanghai", WeekStart: "monday",
	})
	require.NoError(t, err)
	require.Equal(t, window.ResetAt.Sub(now)+70*24*time.Hour, mr.TTL(activeKey))
}

func TestBudgetCounterRebuildV2_ScansAndClearsOrphanedCurrentTierKeys(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:rebuild_scan?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallLog{}))
	createRebuildBudgetTables(t, db)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	ctx := context.Background()
	orphanMonthly := "budget:v2:tier:used:member:deleted-user:deleted-tier:2026-07"
	orphanDaily := "budget:v2:tier:used:studio:disabled-studio:disabled-tier:2026-07-11"
	previousPeriod := "budget:v2:tier:used:member:deleted-user:deleted-tier:2026-06"
	require.NoError(t, rdb.Set(ctx, orphanMonthly, "9000000", time.Hour).Err())
	require.NoError(t, rdb.Set(ctx, orphanDaily, "3000000", time.Hour).Err())
	require.NoError(t, rdb.Set(ctx, previousPeriod, "7000000", time.Hour).Err())

	result, err := service.NewBudgetCounterRebuildService(db, rdb, budgetperiod.Config{
		Timezone: "Asia/Shanghai", WeekStart: "monday",
	}).RebuildCurrentPeriods(ctx, time.Date(2026, 7, 11, 15, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Equal(t, int64(0), mustRedisInt64(t, rdb, activeCounterKey(t, rdb, orphanMonthly)))
	require.Equal(t, int64(0), mustRedisInt64(t, rdb, activeCounterKey(t, rdb, orphanDaily)))
	require.Equal(t, int64(7_000_000), mustRedisInt64(t, rdb, previousPeriod))
	require.Equal(t, 2, result.KeysWritten)
	require.False(t, result.CutoverReady(), "empty source aggregation must not be cutover ready")
}

func TestBudgetCounterRebuildV2_LocksAdmissionAndWaitsForExistingInflight(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:rebuild_concurrent_delta?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallLog{}))
	createRebuildBudgetTables(t, db)
	require.NoError(t, db.Create(&cpmodel.BudgetAllocation{
		ID: uuid.New(), ScopeType: cpmodel.BudgetScopeMember, ScopeID: "u-race",
		AmountUSD: 100, DailyAmountUSD: -1, WeeklyAmountUSD: -1, Enabled: true,
	}).Error)
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	rows := make([]obsmodel.CallLog, 200)
	for i := range rows {
		rows[i] = obsmodel.CallLog{
			RequestID: fmt.Sprintf("historical-%04d", i), TS: now.Add(-time.Hour), Status: 200,
			UserID: "u-race", KeyType: "personal", EstCostUSD: 0.000001,
			BudgetTierConstraintsSnapshot: []obsmodel.BudgetTierConstraintSnapshot{},
		}
	}
	require.NoError(t, db.CreateInBatches(rows, 200).Error)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	key := "budget:v2:used:member:u-race:2026-07"
	require.NoError(t, rdb.Set(context.Background(), key, "999", time.Hour).Err())
	require.NoError(t, rdb.ZAdd(context.Background(), gwbudget.BillableInflightKey,
		redis.Z{Score: float64(time.Now().Add(time.Minute).UnixMilli()), Member: "already-running"},
	).Err())
	resultCh := make(chan error, 1)
	go func() {
		_, rebuildErr := service.NewBudgetCounterRebuildService(db, rdb, budgetperiod.Config{
			Timezone: "Asia/Shanghai", WeekStart: "monday",
		}).RebuildCurrentPeriods(context.Background(), now)
		resultCh <- rebuildErr
	}()
	require.Eventually(t, func() bool {
		return rdb.Exists(context.Background(), gwbudget.RebuildLockKey).Val() == 1
	}, time.Second, time.Millisecond)
	store := gwbudget.NewRedisStore(rdb)
	require.ErrorIs(t, store.Reserve(context.Background(), "new-during-rebuild", time.Now()), gwbudget.ErrRebuildInProgress)
	select {
	case err := <-resultCh:
		t.Fatalf("rebuild completed before inflight drained: %v", err)
	default:
	}
	require.NoError(t, rdb.ZRem(context.Background(), gwbudget.BillableInflightKey, "already-running").Err())
	require.NoError(t, <-resultCh)
	require.Equal(t, int64(200), mustRedisInt64(t, rdb, activeCounterKey(t, rdb, key)))
	require.Zero(t, rdb.Exists(context.Background(), gwbudget.RebuildLockKey).Val())
}

func TestBudgetCounterRebuildV2_LostLeaseAbortsWithoutSwitching(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:rebuild_lost_lease?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallLog{}))
	createRebuildBudgetTables(t, db)
	require.NoError(t, db.Create(&cpmodel.BudgetAllocation{
		ID: uuid.New(), ScopeType: cpmodel.BudgetScopeMember, ScopeID: "u-lost",
		AmountUSD: 100, DailyAmountUSD: -1, WeeklyAmountUSD: -1, Enabled: true,
	}).Error)
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	key := "budget:v2:used:member:u-lost:2026-07"
	require.NoError(t, rdb.Set(context.Background(), key, "999", time.Hour).Err())
	require.NoError(t, rdb.ZAdd(context.Background(), gwbudget.BillableInflightKey,
		redis.Z{Score: float64(time.Now().Add(time.Minute).UnixMilli()), Member: "pause"},
	).Err())
	done := make(chan error, 1)
	go func() {
		_, rebuildErr := service.NewBudgetCounterRebuildService(db, rdb, budgetperiod.Config{
			Timezone: "Asia/Shanghai", WeekStart: "monday",
		}).RebuildCurrentPeriods(context.Background(), now)
		done <- rebuildErr
	}()
	require.Eventually(t, func() bool {
		return rdb.Exists(context.Background(), gwbudget.RebuildLockKey).Val() == 1
	}, time.Second, time.Millisecond)
	require.NoError(t, rdb.Del(context.Background(), gwbudget.RebuildLockKey).Err())
	require.NoError(t, rdb.ZRem(context.Background(), gwbudget.BillableInflightKey, "pause").Err())
	require.Error(t, <-done)
	require.Equal(t, "999", rdb.Get(context.Background(), key).Val())
	require.Equal(t, "", rdb.Get(context.Background(), gwbudget.ActiveGenerationKey).Val())
}

func TestBudgetCounterRebuildV2_AtomicallySwitchesGenerationAndKeepsOldForRollback(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:rebuild_generation_switch?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallLog{}))
	createRebuildBudgetTables(t, db)
	require.NoError(t, db.Create(&cpmodel.BudgetAllocation{
		ID: uuid.New(), ScopeType: cpmodel.BudgetScopeMember, ScopeID: "u-generation",
		AmountUSD: 100, DailyAmountUSD: -1, WeeklyAmountUSD: -1, Enabled: true,
	}).Error)
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&obsmodel.CallLog{
		RequestID: "generation-fact", TS: now.Add(-time.Hour), Status: 200,
		UserID: "u-generation", KeyType: "personal", EstCostUSD: 0.000123,
	}).Error)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	baseKey := "budget:v2:used:member:u-generation:2026-07"
	require.NoError(t, rdb.Set(context.Background(), baseKey, "999", time.Hour).Err())

	result, err := service.NewBudgetCounterRebuildService(db, rdb, budgetperiod.Config{
		Timezone: "Asia/Shanghai", WeekStart: "monday",
	}).RebuildCurrentPeriods(context.Background(), now)
	require.NoError(t, err)
	require.NotEmpty(t, result.ActiveGeneration)
	require.Equal(t, gwbudget.LegacyGeneration, result.PreviousGeneration)
	require.Equal(t, result.ActiveGeneration, rdb.Get(context.Background(), gwbudget.ActiveGenerationKey).Val())
	require.Equal(t, "123", rdb.Get(context.Background(),
		gwbudget.GenerationKey(result.ActiveGeneration, baseKey)).Val())
	require.Equal(t, "999", rdb.Get(context.Background(), baseKey).Val())

	require.NoError(t, rdb.Set(context.Background(), gwbudget.ActiveGenerationKey, gwbudget.LegacyGeneration, 0).Err())
	require.Equal(t, "999", rdb.Get(context.Background(),
		gwbudget.GenerationKey(gwbudget.LegacyGeneration, baseKey)).Val())
}

func TestBudgetCounterRebuildV2_RenewsLeaseWhileWaitingForInflight(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:rebuild_lease_renew?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallLog{}))
	createRebuildBudgetTables(t, db)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	require.NoError(t, rdb.ZAdd(context.Background(), gwbudget.BillableInflightKey,
		redis.Z{Score: float64(time.Now().Add(time.Minute).UnixMilli()), Member: "long-running"},
	).Err())
	rebuild := service.NewBudgetCounterRebuildServiceWithConfig(
		db, rdb, budgetperiod.Config{Timezone: "Asia/Shanghai", WeekStart: "monday"},
		service.BudgetCounterRebuildConfig{LeaseTTL: 90 * time.Millisecond, WaitInterval: 5 * time.Millisecond},
	)
	done := make(chan error, 1)
	go func() {
		_, rebuildErr := rebuild.RebuildCurrentPeriods(
			context.Background(), time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC),
		)
		done <- rebuildErr
	}()
	require.Eventually(t, func() bool {
		return rdb.Exists(context.Background(), gwbudget.RebuildLockKey).Val() == 1
	}, time.Second, time.Millisecond)
	time.Sleep(250 * time.Millisecond)
	require.Equal(t, int64(1), rdb.Exists(context.Background(), gwbudget.RebuildLockKey).Val())
	require.NoError(t, rdb.ZRem(context.Background(), gwbudget.BillableInflightKey, "long-running").Err())
	require.NoError(t, <-done)
}

func TestBudgetCounterRebuildV2_IncludesStreamFactsPendingAtCutoff(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:rebuild_stream_cutoff?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallLog{}))
	createRebuildBudgetTables(t, db)
	require.NoError(t, db.Create(&cpmodel.BudgetAllocation{
		ID: uuid.New(), ScopeType: cpmodel.BudgetScopeMember, ScopeID: "u-spool",
		AmountUSD: 100, DailyAmountUSD: -1, WeeklyAmountUSD: -1, Enabled: true,
	}).Error)
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	pending := obsmodel.CallLog{
		RequestID: "pending-stream", TS: now.Add(-time.Hour), Status: 200,
		UserID: "u-spool", KeyType: "personal", EstCostUSD: 1.25,
		BudgetTierConstraintsSnapshot: []obsmodel.BudgetTierConstraintSnapshot{},
	}
	payload, err := json.Marshal(pending)
	require.NoError(t, err)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	require.NoError(t, rdb.XAdd(context.Background(), &redis.XAddArgs{
		Stream: calllogstream.Spool,
		Values: map[string]any{"request_id": pending.RequestID, "payload": string(payload), "retries": 0},
	}).Err())

	result, err := service.NewBudgetCounterRebuildService(db, rdb, budgetperiod.Config{
		Timezone: "Asia/Shanghai", WeekStart: "monday",
	}).RebuildCurrentPeriods(context.Background(), now)
	require.NoError(t, err)
	require.Equal(t, 1, result.SourceRows)
	require.Equal(t, int64(1_250_000), mustRedisInt64(t, rdb,
		activeCounterKey(t, rdb, "budget:v2:used:member:u-spool:2026-07")))
}

func TestBudgetCounterRebuildV2_FailureKeepsOfficialCounterAndCanRetry(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:rebuild_failure_retry?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallLog{}))
	createRebuildBudgetTables(t, db)
	require.NoError(t, db.Create(&cpmodel.BudgetAllocation{
		ID: uuid.New(), ScopeType: cpmodel.BudgetScopeMember, ScopeID: "u-retry",
		AmountUSD: 100, DailyAmountUSD: -1, WeeklyAmountUSD: -1, Enabled: true,
	}).Error)
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	key := "budget:v2:used:member:u-retry:2026-07"
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	require.NoError(t, rdb.Set(context.Background(), key, "corrupt", time.Hour).Err())
	rebuild := service.NewBudgetCounterRebuildService(db, rdb, budgetperiod.Config{
		Timezone: "Asia/Shanghai", WeekStart: "monday",
	})
	_, err = rebuild.RebuildCurrentPeriods(context.Background(), now)
	require.Error(t, err)
	require.Equal(t, "corrupt", rdb.Get(context.Background(), key).Val())
	require.Zero(t, rdb.Exists(context.Background(), gwbudget.RebuildLockKey).Val())

	require.NoError(t, rdb.Set(context.Background(), key, "7", time.Hour).Err())
	_, err = rebuild.RebuildCurrentPeriods(context.Background(), now)
	require.NoError(t, err)
	require.Equal(t, "0", rdb.Get(context.Background(), activeCounterKey(t, rdb, key)).Val())
}

func TestBudgetCounterRebuildV2_SwitchesLargeKeysetsInBoundedBatches(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:rebuild_batched_switch?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallLog{}))
	createRebuildBudgetTables(t, db)
	allocations := make([]cpmodel.BudgetAllocation, 205)
	for i := range allocations {
		allocations[i] = cpmodel.BudgetAllocation{
			ID: uuid.New(), ScopeType: cpmodel.BudgetScopeMember, ScopeID: fmt.Sprintf("batch-%03d", i),
			AmountUSD: 100, DailyAmountUSD: -1, WeeklyAmountUSD: -1, Enabled: true,
		}
	}
	require.NoError(t, db.CreateInBatches(allocations, 50).Error)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)

	result, err := service.NewBudgetCounterRebuildService(db, rdb, budgetperiod.Config{
		Timezone: "Asia/Shanghai", WeekStart: "monday",
	}).RebuildCurrentPeriods(context.Background(), now)
	require.NoError(t, err)
	require.Equal(t, 205, result.KeysWritten)
	require.Equal(t, 3, result.SwitchBatches)
	require.Equal(t, 2, result.AllocationPages)
	for _, allocation := range allocations {
		require.Equal(t, "0", rdb.Get(context.Background(), activeCounterKey(t, rdb,
			"budget:v2:used:member:"+allocation.ScopeID+":2026-07")).Val())
	}
}

func TestBudgetCounterRebuildV2_PagesStreamFactsBeyondSingleBatch(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:rebuild_stream_pages?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallLog{}))
	createRebuildBudgetTables(t, db)
	require.NoError(t, db.Create(&cpmodel.BudgetAllocation{
		ID: uuid.New(), ScopeType: cpmodel.BudgetScopeMember, ScopeID: "u-stream-pages",
		AmountUSD: 100, DailyAmountUSD: -1, WeeklyAmountUSD: -1, Enabled: true,
	}).Error)
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	pipe := rdb.Pipeline()
	for i := 0; i < 501; i++ {
		row := obsmodel.CallLog{
			RequestID: fmt.Sprintf("stream-page-%03d", i), TS: now.Add(-time.Hour), Status: 200,
			UserID: "u-stream-pages", KeyType: "personal", EstCostUSD: 0.000001,
			BudgetTierConstraintsSnapshot: []obsmodel.BudgetTierConstraintSnapshot{},
		}
		payload, marshalErr := json.Marshal(row)
		require.NoError(t, marshalErr)
		pipe.XAdd(context.Background(), &redis.XAddArgs{
			Stream: calllogstream.Spool,
			Values: map[string]any{"request_id": row.RequestID, "payload": string(payload), "retries": 0},
		})
	}
	_, err = pipe.Exec(context.Background())
	require.NoError(t, err)

	result, err := service.NewBudgetCounterRebuildService(db, rdb, budgetperiod.Config{
		Timezone: "Asia/Shanghai", WeekStart: "monday",
	}).RebuildCurrentPeriods(context.Background(), now)
	require.NoError(t, err)
	require.Equal(t, 501, result.SourceRows)
	require.Equal(t, 3, result.CallLogPages)
	require.Equal(t, int64(501), mustRedisInt64(t, rdb,
		activeCounterKey(t, rdb, "budget:v2:used:member:u-stream-pages:2026-07")))
}

func mustRedisInt64(t *testing.T, rdb *redis.Client, key string) int64 {
	t.Helper()
	value, err := rdb.Get(context.Background(), key).Int64()
	require.NoError(t, err)
	return value
}

func activeCounterKey(t *testing.T, rdb *redis.Client, baseKey string) string {
	t.Helper()
	generation, err := rdb.Get(context.Background(), gwbudget.ActiveGenerationKey).Result()
	require.NoError(t, err)
	return gwbudget.GenerationKey(generation, baseKey)
}

func createRebuildBudgetTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Exec(`CREATE TABLE budget_allocations (
		id text PRIMARY KEY, scope_type text NOT NULL, scope_id text NOT NULL,
		amount_usd real NOT NULL, daily_amount_usd real NOT NULL, weekly_amount_usd real NOT NULL,
		period text, enabled integer NOT NULL, created_by text, updated_by text,
		created_at datetime, updated_at datetime
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE budget_tier_limits (
		id text PRIMARY KEY, budget_allocation_id text NOT NULL, model_tier_id text NOT NULL,
		period_type text NOT NULL, limit_usd numeric NOT NULL, effective_at datetime NOT NULL,
		enabled integer NOT NULL, created_at datetime, updated_at datetime
	)`).Error)
}
