package budget_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestRedisStore_AddUsesIntegerMicroUSDAndTTL(t *testing.T) {
	mr := miniredis.RunT(t)
	store := budget.NewRedisStore(redis.NewClient(&redis.Options{Addr: mr.Addr()}))
	resetAt := time.Now().Add(4 * time.Hour)
	plan := &budget.AccountingPlan{Constraints: []budget.Constraint{
		{Key: "budget:v2:used:member:u-1:2026-07", LimitMicroUSD: 10_000_000, PeriodType: budgetdomain.PeriodMonthly, ResetAt: resetAt},
		{Key: "budget:v2:tier:used:member:u-1:t-1:2026-07", LimitMicroUSD: 2_000_000, PeriodType: budgetdomain.PeriodMonthly, ResetAt: resetAt},
	}}
	require.NoError(t, store.Add(context.Background(), plan, 100_001))
	require.NoError(t, store.Add(context.Background(), plan, 200_002))
	total, err := mr.Get(plan.Constraints[0].Key)
	require.NoError(t, err)
	tier, err := mr.Get(plan.Constraints[1].Key)
	require.NoError(t, err)
	require.Equal(t, "300003", total)
	require.Equal(t, "300003", tier)
	require.InDelta(t, (70*24*time.Hour + 4*time.Hour).Seconds(), mr.TTL(plan.Constraints[0].Key).Seconds(), 2)
}

func TestRedisStore_CheckReturnsAllPositiveBlockersWithoutFloatPrecision(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := budget.NewRedisStore(rdb)
	plan := &budget.AccountingPlan{Constraints: []budget.Constraint{
		{Key: "total", LimitMicroUSD: 9_007_199_254_740_993, Dimension: budget.DimensionTotal},
		{Key: "tier", LimitMicroUSD: 2, Dimension: budget.DimensionTier},
		{Key: "zero", LimitMicroUSD: 0, StaticZero: true},
	}}
	require.NoError(t, rdb.Set(context.Background(), "total", "9007199254740993", 0).Err())
	require.NoError(t, rdb.Set(context.Background(), "tier", "2", 0).Err())

	result := store.Check(context.Background(), plan)
	require.NoError(t, result.RedisError)
	require.Len(t, result.Blockers, 2)
	require.Equal(t, "total", result.Blockers[0].Key)
	require.Equal(t, "tier", result.Blockers[1].Key)
}

func TestRedisStore_RedisDownReturnsErrorAndDoesNotPanic(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := budget.NewRedisStore(rdb)
	mr.Close()
	result := store.Check(context.Background(), &budget.AccountingPlan{Constraints: []budget.Constraint{{
		Key: "x", LimitMicroUSD: 1,
	}}})
	require.Error(t, result.RedisError)
}

func TestRedisStore_ConcurrentCheckAndAddKeepsExactIntegerTotal(t *testing.T) {
	mr := miniredis.RunT(t)
	store := budget.NewRedisStore(redis.NewClient(&redis.Options{Addr: mr.Addr()}))
	plan := &budget.AccountingPlan{Constraints: []budget.Constraint{{
		Key: "budget:v2:used:member:u-1:2026-07", LimitMicroUSD: 1_000_000_000,
		Dimension: budget.DimensionTotal, PeriodType: budgetdomain.PeriodMonthly,
	}}}
	var wg sync.WaitGroup
	errs := make(chan error, 200)
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := store.Check(context.Background(), plan).RedisError; err != nil {
				errs <- err
			}
			if err := store.Add(context.Background(), plan, 10_001); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	got, err := mr.Get(plan.Constraints[0].Key)
	require.NoError(t, err)
	require.Equal(t, "1000100", got)
}

func TestRedisStore_AddValidatesEveryKeyBeforeAnyWrite(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := budget.NewRedisStore(rdb)
	plan := &budget.AccountingPlan{Constraints: []budget.Constraint{
		{Key: "valid-before", PeriodType: budgetdomain.PeriodMonthly},
		{Key: "corrupt-middle", PeriodType: budgetdomain.PeriodMonthly},
		{Key: "valid-after", PeriodType: budgetdomain.PeriodMonthly},
	}}
	require.NoError(t, rdb.MSet(context.Background(),
		"valid-before", "10", "corrupt-middle", "not-an-integer", "valid-after", "30",
	).Err())

	require.Error(t, store.Add(context.Background(), plan, 5))
	require.Equal(t, "10", rdb.Get(context.Background(), "valid-before").Val())
	require.Equal(t, "not-an-integer", rdb.Get(context.Background(), "corrupt-middle").Val())
	require.Equal(t, "30", rdb.Get(context.Background(), "valid-after").Val())
}

func TestRedisStore_ReserveRejectsRebuildAndAtomicallyEnforcesHardCapacity(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := budget.NewRedisStoreWithAdmission(rdb, budget.AdmissionConfig{
		SpoolCapacity: 2, ReservationTTL: time.Minute,
	})
	now := time.Now()
	require.NoError(t, rdb.Set(context.Background(), budget.RebuildLockKey, "owner", time.Minute).Err())
	require.ErrorIs(t, store.Reserve(context.Background(), "locked", now), budget.ErrRebuildInProgress)
	require.NoError(t, rdb.Del(context.Background(), budget.RebuildLockKey).Err())
	require.NoError(t, rdb.XAdd(context.Background(), &redis.XAddArgs{
		Stream: budget.CallLogSpoolStream,
		Values: map[string]any{"request_id": "existing", "payload": "{}"},
	}).Err())

	require.NoError(t, store.Reserve(context.Background(), "first", now))
	require.ErrorIs(t, store.Reserve(context.Background(), "overflow", now), budget.ErrSpoolCapacity)
	require.Equal(t, int64(1), rdb.ZCard(context.Background(), budget.BillableInflightKey).Val())
	require.NoError(t, store.Release(context.Background(), "first"))
}

func TestRedisStore_ReserveCleansExpiredReservations(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := budget.NewRedisStoreWithAdmission(rdb, budget.AdmissionConfig{
		SpoolCapacity: 1, ReservationTTL: time.Minute,
	})
	now := time.Now()
	require.NoError(t, rdb.ZAdd(context.Background(), budget.BillableInflightKey,
		redis.Z{Score: float64(now.Add(-time.Second).UnixMilli()), Member: "expired"},
	).Err())

	require.NoError(t, store.Reserve(context.Background(), "replacement", now))
	members := rdb.ZRange(context.Background(), budget.BillableInflightKey, 0, -1).Val()
	require.Equal(t, []string{"replacement"}, members)
	require.False(t, errors.Is(store.Reserve(context.Background(), "another", now), budget.ErrRebuildInProgress))
	require.NoError(t, store.Release(context.Background(), "replacement"))
	require.Zero(t, rdb.ZCard(context.Background(), budget.BillableInflightKey).Val())
}

func TestRedisStore_ConcurrentReservationsNeverExceedHardCapacity(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := budget.NewRedisStoreWithAdmission(rdb, budget.AdmissionConfig{
		SpoolCapacity: 10, ReservationTTL: time.Minute,
	})
	var wg sync.WaitGroup
	var accepted atomic.Int32
	for i := range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if store.Reserve(context.Background(), fmt.Sprintf("request-%03d", i), time.Now()) == nil {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int32(10), accepted.Load())
	require.Equal(t, int64(10), rdb.ZCard(context.Background(), budget.BillableInflightKey).Val())
	require.Zero(t, rdb.XLen(context.Background(), budget.CallLogSpoolStream).Val())
	for i := range 100 {
		require.NoError(t, store.Release(context.Background(), fmt.Sprintf("request-%03d", i)))
	}
}

func TestRedisStore_ReservationFixesGenerationAndHeartbeatsPastTTL(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	require.NoError(t, rdb.Set(context.Background(), budget.ActiveGenerationKey, "gen-a", 0).Err())
	store := budget.NewRedisStoreWithAdmission(rdb, budget.AdmissionConfig{
		SpoolCapacity: 10, ReservationTTL: 90 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
	})

	reservation, err := store.ReserveWithGeneration(context.Background(), "long-stream", time.Now())
	require.NoError(t, err)
	require.Equal(t, "gen-a", reservation.Generation())
	require.Equal(t,
		"budget:v2:gen:gen-a:used:member:u-1:2026-07",
		budget.GenerationKey("gen-a", "budget:v2:used:member:u-1:2026-07"),
	)

	time.Sleep(220 * time.Millisecond)
	score, err := rdb.ZScore(context.Background(), budget.BillableInflightKey, "long-stream").Result()
	require.NoError(t, err)
	require.Greater(t, score, float64(time.Now().UnixMilli()))
	require.NoError(t, reservation.Finish(context.Background()))
	require.Zero(t, rdb.ZCard(context.Background(), budget.BillableInflightKey).Val())
}

func TestRedisStore_MissingGenerationPointerUsesLegacyKeys(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := budget.NewRedisStoreWithAdmission(rdb, budget.AdmissionConfig{
		SpoolCapacity: 10, ReservationTTL: time.Minute,
	})

	reservation, err := store.ReserveWithGeneration(context.Background(), "legacy-request", time.Now())
	require.NoError(t, err)
	require.Equal(t, budget.LegacyGeneration, reservation.Generation())
	require.Equal(t,
		"budget:v2:used:member:u-1:2026-07",
		budget.GenerationKey(reservation.Generation(), "budget:v2:used:member:u-1:2026-07"),
	)
	require.NoError(t, reservation.Finish(context.Background()))
}

func TestRedisStore_CheckAndAddUsePlanFixedGeneration(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := budget.NewRedisStore(rdb)
	baseKey := "budget:v2:used:member:u-1:2026-07"
	generatedKey := budget.GenerationKey("gen-fixed", baseKey)
	require.NoError(t, rdb.Set(context.Background(), generatedKey, "90", 0).Err())
	plan := &budget.AccountingPlan{
		Generation: "gen-fixed",
		Constraints: []budget.Constraint{{
			Key: baseKey, LimitMicroUSD: 100, PeriodType: budgetdomain.PeriodMonthly,
		}},
	}

	result := store.Check(context.Background(), plan)
	require.NoError(t, result.RedisError)
	require.Empty(t, result.Blockers)
	require.NoError(t, store.Add(context.Background(), plan, 10))
	require.Equal(t, "100", rdb.Get(context.Background(), generatedKey).Val())
	require.False(t, rdb.Exists(context.Background(), baseKey).Val() == 1)
}

func TestRedisStore_HeartbeatMarksReservationLostWhenMemberMissingDuringRebuild(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := budget.NewRedisStoreWithAdmission(rdb, budget.AdmissionConfig{
		SpoolCapacity: 10, ReservationTTL: time.Second, HeartbeatInterval: 10 * time.Millisecond,
	})
	reservation, err := store.ReserveWithGeneration(context.Background(), "lost-heartbeat", time.Now())
	require.NoError(t, err)
	require.NoError(t, rdb.ZRem(context.Background(), budget.BillableInflightKey, "lost-heartbeat").Err())
	require.NoError(t, rdb.Set(context.Background(), budget.RebuildLockKey, "owner", time.Minute).Err())
	require.Eventually(t, reservation.Lost, time.Second, 10*time.Millisecond)
	require.ErrorContains(t, reservation.Err(), "state=2")
	require.NoError(t, reservation.Finish(context.Background()))
}

func BenchmarkRedisStore_CheckTotalAndTier(b *testing.B) {
	mr := miniredis.RunT(b)
	store := budget.NewRedisStore(redis.NewClient(&redis.Options{Addr: mr.Addr()}))
	plan := &budget.AccountingPlan{Constraints: []budget.Constraint{
		{Key: "budget:v2:used:member:u-1:2026-07", LimitMicroUSD: 10_000_000, Dimension: budget.DimensionTotal},
		{Key: "budget:v2:tier:used:member:u-1:tier-1:2026-07", LimitMicroUSD: 2_000_000, Dimension: budget.DimensionTier},
	}}
	b.ResetTimer()
	for range b.N {
		result := store.Check(context.Background(), plan)
		if result.RedisError != nil || len(result.Blockers) != 0 {
			b.Fatal(result)
		}
	}
}
