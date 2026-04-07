//go:build integration

package integration_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/budgetperiod"
	"github.com/example/llm-gateway/internal/budgetrebuild"
	"github.com/example/llm-gateway/internal/configsync"
	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	cpinfra "github.com/example/llm-gateway/internal/controlplane/infra"
	cpmodel "github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/core"
	gwbudget "github.com/example/llm-gateway/internal/gateway/budget"
	gwmiddleware "github.com/example/llm-gateway/internal/gateway/middleware"
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
	obsrepo "github.com/example/llm-gateway/internal/gateway/observability/repository"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type transientAdmissionHook struct{ failed atomic.Bool }

func (h *transientAdmissionHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

func (h *transientAdmissionHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if h.failed.CompareAndSwap(false, true) {
			return errors.New("transient admission redis fault")
		}
		return next(ctx, cmd)
	}
}

func (h *transientAdmissionHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func TestModelTierBudgetIntegration_ProductionWiringSpoolsBeforePostgresStartup(t *testing.T) {
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skipf("Docker is required for integration tests: %v", err)
	}
	redisID := runContainer(t, "redis:7-alpine")
	rdb := redis.NewClient(&redis.Options{Addr: mappedAddress(t, redisID, "6379/tcp")})
	require.Eventually(t, func() bool {
		return rdb.Ping(context.Background()).Err() == nil
	}, 30*time.Second, 100*time.Millisecond)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	postgresAddr := listener.Addr().String()
	require.NoError(t, listener.Close())
	host, port, err := net.SplitHostPort(postgresAddr)
	require.NoError(t, err)
	dsn := "host=" + host + " port=" + port + " user=gateway password=gateway dbname=gateway_test sslmode=disable connect_timeout=1"

	cfg := gwobs.DefaultCollectorConfig()
	cfg.DBConnectRetryInterval = 50 * time.Millisecond
	cfg.SpoolReplayInterval = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	collector := gwobs.StartProductionCollector(
		ctx, cfg, rdb, nil,
		func(context.Context) (*gorm.DB, error) { return cpinfra.InitPostgres(dsn) },
		nil,
	)
	collector.Emit(gwobs.UsageEvent{RequestID: "postgres-late-start", Timestamp: time.Now(), Status: 200})
	require.Eventually(t, func() bool {
		return rdb.XLen(context.Background(), gwobs.CallLogSpoolStream).Val() == 1
	}, 5*time.Second, 20*time.Millisecond)

	runContainer(t, "postgres:16-alpine",
		"-p", "127.0.0.1:"+port+":5432",
		"-e", "POSTGRES_PASSWORD=gateway",
		"-e", "POSTGRES_USER=gateway",
		"-e", "POSTGRES_DB=gateway_test",
	)
	require.Eventually(t, func() bool {
		db, connectErr := cpinfra.InitPostgres(dsn)
		if connectErr != nil {
			return false
		}
		sqlDB, dbErr := db.DB()
		return dbErr == nil && sqlDB.Ping() == nil
	}, 30*time.Second, 100*time.Millisecond)
	applyAtlasSchema(t, postgresAddr, "gateway", "gateway", "gateway_test")
	db, err := cpinfra.InitPostgres(dsn)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		var count int64
		_ = db.Model(&obsmodel.CallLog{}).Where("request_id = ?", "postgres-late-start").Count(&count).Error
		return count == 1 && rdb.XLen(context.Background(), gwobs.CallLogSpoolStream).Val() == 0
	}, 30*time.Second, 50*time.Millisecond)

	collector.Emit(gwobs.UsageEvent{RequestID: "postgres-late-start", Timestamp: time.Now(), Status: 200})
	require.Eventually(t, func() bool {
		var count int64
		_ = db.Model(&obsmodel.CallLog{}).Where("request_id = ?", "postgres-late-start").Count(&count).Error
		return count == 1 && rdb.XLen(context.Background(), gwobs.CallLogSpoolStream).Val() == 0
	}, 10*time.Second, 50*time.Millisecond)
}

func TestModelTierBudgetIntegration_RedisDownPositiveBudgetFailsOpenAndPersistsPostgres(t *testing.T) {
	infra := startIntegrationInfra(t)
	require.NoError(t, infra.DB.Create(&cpmodel.BudgetAllocation{
		ID: uuid.New(), ScopeType: cpmodel.BudgetScopeMember, ScopeID: "redis-down-user",
		AmountUSD: 10, DailyAmountUSD: -1, WeeklyAmountUSD: -1, Enabled: true,
	}).Error)
	price := 1.0
	cfg := gwobs.DefaultCollectorConfig()
	cfg.SyncFallbackTimeout = 3 * time.Second
	cfg.DBConnectRetryInterval = 10 * time.Millisecond
	collectorCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	collector := gwobs.StartProductionCollector(
		collectorCtx, cfg, infra.Redis,
		gwobs.PricingResolverFunc(func(string) (core.ModelPricing, bool) {
			return core.ModelPricing{InputPerMtok: &price, OutputPerMtok: &price}, true
		}),
		func(context.Context) (*gorm.DB, error) { return infra.DB, nil }, nil,
	)
	require.Eventually(t, collector.HasLivePostgresSink, 5*time.Second, 10*time.Millisecond)
	holder := gwbudget.NewSnapshotHolder()
	holder.Set(&configsync.BudgetTierSnapshot{
		SchemaVersion: 1, MasterEnabled: true, AccountingEnabled: true,
		V2TotalEnforcementEnabled: true, Timezone: "Asia/Shanghai", WeekStart: "monday",
	})
	gate := gwbudget.NewGate(holder, gwbudget.NewRedisStoreWithAdmission(infra.Redis, gwbudget.AdmissionConfig{
		SpoolCapacity: 10, ReservationTTL: time.Minute,
	}), nil, time.Now)
	require.NoError(t, exec.Command("docker", "stop", "--time", "0", infra.RedisContainerID).Run())

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("request_id", "redis-down-production-request")
		c.Set("user_id", "redis-down-user")
		c.Set("key_type", "personal")
		c.Set("identity", &gwmodel.UserIdentity{
			UserID: "redis-down-user",
			BudgetChain: []gwmodel.BudgetScope{{
				ScopeType: "member", ScopeID: "redis-down-user", AmountUSD: 10,
				DailyAmountUSD: -1, WeeklyAmountUSD: -1,
			}},
		})
		c.Next()
	})
	router.Use(gwmiddleware.UsageCollector(collector, nil, func(c *gin.Context, cost float64, status int) {
		plan, ok := gwbudget.GetAccountingPlan(c)
		if !ok {
			return
		}
		micros, err := gwbudget.MicroUSDFromFloat64(cost)
		if err == nil {
			gate.Account(c.Request.Context(), plan, micros)
		}
	}))
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		outcome, checkErr := gate.Check(c)
		require.NoError(t, checkErr)
		require.True(t, outcome.FailOpen)
		c.Set("model", "priced-model")
		c.Set("prompt_tokens", 1000)
		c.Status(http.StatusOK)
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Eventually(t, func() bool {
		var count int64
		return infra.DB.Model(&obsmodel.CallLog{}).
			Where("request_id = ?", "redis-down-production-request").Count(&count).Error == nil && count == 1
	}, 5*time.Second, 20*time.Millisecond)
	var persisted obsmodel.CallLog
	require.NoError(t, infra.DB.Where("request_id = ?", "redis-down-production-request").First(&persisted).Error)
	require.Equal(t, "redis-down-user", persisted.UserID)
	require.InDelta(t, 0.001, persisted.EstCostUSD, 0.0000001)
	_, rebuildErr := service.NewBudgetCounterRebuildService(infra.DB, infra.Redis, budgetperiod.Config{
		Timezone: "Asia/Shanghai", WeekStart: "monday",
	}).RebuildCurrentPeriods(context.Background(), time.Now())
	require.Error(t, rebuildErr, "rebuild must not run without its Redis lease")

	recoveredRedisID := runContainer(t, "redis:7-alpine")
	recoveredRedis := redis.NewClient(&redis.Options{Addr: mappedAddress(t, recoveredRedisID, "6379/tcp")})
	require.Eventually(t, func() bool {
		return recoveredRedis.Ping(context.Background()).Err() == nil
	}, 30*time.Second, 100*time.Millisecond)
	recoveryNow := time.Now()
	rebuildResult, err := service.NewBudgetCounterRebuildService(infra.DB, recoveredRedis, budgetperiod.Config{
		Timezone: "Asia/Shanghai", WeekStart: "monday",
	}).RebuildCurrentPeriods(context.Background(), recoveryNow)
	require.NoError(t, err)
	monthly, err := budgetperiod.Resolve(recoveryNow, budgetdomain.PeriodMonthly, budgetperiod.Config{
		Timezone: "Asia/Shanghai", WeekStart: "monday",
	})
	require.NoError(t, err)
	recoveredKey := gwbudget.GenerationKey(rebuildResult.ActiveGeneration,
		"budget:v2:used:member:redis-down-user:"+monthly.Key)
	require.Equal(t, "1000", recoveredRedis.Get(context.Background(), recoveredKey).Val())
}

func TestModelTierBudgetIntegration_TransientAdmissionFailureStaysFailOpenAfterCheckRecovery(t *testing.T) {
	infra := startIntegrationInfra(t)
	gateRedis := redis.NewClient(&redis.Options{Addr: infra.Redis.Options().Addr})
	gateRedis.AddHook(&transientAdmissionHook{})
	t.Cleanup(func() { _ = gateRedis.Close() })
	price := 1.0
	cfg := gwobs.DefaultCollectorConfig()
	cfg.DBConnectRetryInterval = 10 * time.Millisecond
	collectorCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	collector := gwobs.StartProductionCollector(
		collectorCtx, cfg, infra.Redis,
		gwobs.PricingResolverFunc(func(string) (core.ModelPricing, bool) {
			return core.ModelPricing{InputPerMtok: &price, OutputPerMtok: &price}, true
		}),
		func(context.Context) (*gorm.DB, error) { return infra.DB, nil }, nil,
	)
	require.Eventually(t, collector.HasLivePostgresSink, 5*time.Second, 10*time.Millisecond)
	holder := gwbudget.NewSnapshotHolder()
	holder.Set(&configsync.BudgetTierSnapshot{
		SchemaVersion: 1, MasterEnabled: true, AccountingEnabled: true,
		V2TotalEnforcementEnabled: true, Timezone: "Asia/Shanghai", WeekStart: "monday",
	})
	gate := gwbudget.NewGate(holder, gwbudget.NewRedisStoreWithAdmission(gateRedis, gwbudget.AdmissionConfig{
		SpoolCapacity: 10, ReservationTTL: time.Minute,
	}), nil, time.Now)
	var checkedPlan *gwbudget.AccountingPlan
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("request_id", "transient-admission-production")
		c.Set("user_id", "transient-user")
		c.Set("key_type", "personal")
		c.Set("identity", &gwmodel.UserIdentity{
			UserID: "transient-user",
			BudgetChain: []gwmodel.BudgetScope{{
				ScopeType: "member", ScopeID: "transient-user", AmountUSD: 10,
				DailyAmountUSD: -1, WeeklyAmountUSD: -1,
			}},
		})
		c.Next()
	})
	router.Use(gwmiddleware.UsageCollector(collector, nil, func(c *gin.Context, cost float64, _ int) {
		plan, ok := gwbudget.GetAccountingPlan(c)
		if !ok {
			return
		}
		micros, err := gwbudget.MicroUSDFromFloat64(cost)
		require.NoError(t, err)
		gate.Account(c.Request.Context(), plan, micros)
	}))
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		outcome, checkErr := gate.Check(c)
		require.NoError(t, checkErr)
		require.True(t, outcome.FailOpen)
		checkedPlan, _ = gwbudget.GetAccountingPlan(c)
		c.Set("model", "priced-model")
		c.Set("prompt_tokens", 1000)
		c.Status(http.StatusOK)
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, checkedPlan)
	require.True(t, checkedPlan.RedisCheckSucceeded)
	require.True(t, checkedPlan.AdmissionInfrastructureFailOpen)
	require.Zero(t, infra.Redis.Exists(context.Background(), checkedPlan.Constraints[0].Key).Val())
	require.Eventually(t, func() bool {
		var count int64
		return infra.DB.Model(&obsmodel.CallLog{}).
			Where("request_id = ?", "transient-admission-production").Count(&count).Error == nil && count == 1
	}, 5*time.Second, 20*time.Millisecond)
}

func TestModelTierBudgetIntegration_DBFailureSpoolsAndReplays(t *testing.T) {
	infra := startIntegrationInfra(t)
	var failWrites atomic.Bool
	failWrites.Store(true)
	require.NoError(t, infra.DB.Callback().Create().Before("gorm:create").Register("integration:fail_call_logs", func(tx *gorm.DB) {
		if failWrites.Load() && tx.Statement.Schema != nil && tx.Statement.Schema.Table == "call_logs" {
			_ = tx.AddError(errors.New("temporary postgres outage"))
		}
	}))
	cfg := gwobs.DefaultCollectorConfig()
	cfg.FlushN = 1
	cfg.DBRetryCount = 2
	cfg.DBRetryBackoff = 10 * time.Millisecond
	cfg.SpoolReplayInterval = 20 * time.Millisecond
	collector := gwobs.NewCollector(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	collector.StartWriterWithSpool(ctx, obsrepo.NewCallLogRepository(infra.DB), infra.Redis)
	collector.Emit(gwobs.UsageEvent{RequestID: "integration-spool", Timestamp: time.Now(), Status: 200})
	require.Eventually(t, func() bool {
		return infra.Redis.XLen(context.Background(), gwobs.CallLogSpoolStream).Val() == 1
	}, 5*time.Second, 20*time.Millisecond)

	failWrites.Store(false)
	require.Eventually(t, func() bool {
		var count int64
		_ = infra.DB.Model(&obsmodel.CallLog{}).Where("request_id = ?", "integration-spool").Count(&count).Error
		return count == 1 && infra.Redis.XLen(context.Background(), gwobs.CallLogSpoolStream).Val() == 0
	}, 5*time.Second, 20*time.Millisecond)
}

func TestModelTierBudgetIntegration_RebuildClearsOrphanedTierCounter(t *testing.T) {
	infra := startIntegrationInfra(t)
	ctx := context.Background()
	key := "budget:v2:tier:used:member:deleted-user:deleted-tier:2026-07"
	require.NoError(t, infra.Redis.Set(ctx, key, "9999999", time.Hour).Err())
	result, err := service.NewBudgetCounterRebuildService(infra.DB, infra.Redis, budgetperiod.Config{
		Timezone: "Asia/Shanghai", WeekStart: "monday",
	}).RebuildCurrentPeriods(ctx, time.Date(2026, 7, 11, 15, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	value, err := infra.Redis.Get(ctx, gwbudget.GenerationKey(result.ActiveGeneration, key)).Int64()
	require.NoError(t, err)
	require.Zero(t, value)
	require.False(t, result.CutoverReady())
}

func TestModelTierBudgetIntegration_InflightDrainsBeforeExclusiveRebuild(t *testing.T) {
	infra := startIntegrationInfra(t)
	allocation := cpmodel.BudgetAllocation{
		ID: uuid.New(), ScopeType: cpmodel.BudgetScopeMember, ScopeID: "docker-race",
		AmountUSD: 100, DailyAmountUSD: -1, WeeklyAmountUSD: -1, Enabled: true,
	}
	require.NoError(t, infra.DB.Create(&allocation).Error)
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	rows := make([]obsmodel.CallLog, 3000)
	for i := range rows {
		rows[i] = obsmodel.CallLog{
			RequestID: fmt.Sprintf("docker-history-%04d", i), TS: now.Add(-time.Hour),
			Status: 200, UserID: "docker-race", KeyType: "personal", EstCostUSD: 0.000001,
			BudgetTierConstraintsSnapshot: []obsmodel.BudgetTierConstraintSnapshot{},
		}
	}
	require.NoError(t, infra.DB.CreateInBatches(rows, 200).Error)
	key := "budget:v2:used:member:docker-race:2026-07"
	require.NoError(t, infra.Redis.Set(context.Background(), key, "999", time.Hour).Err())
	admission := gwbudget.NewRedisStoreWithAdmission(infra.Redis, gwbudget.AdmissionConfig{
		SpoolCapacity: 10_000, ReservationTTL: time.Minute,
	})
	reservation, err := admission.ReserveWithGeneration(context.Background(), "docker-inflight", time.Now())
	require.NoError(t, err)
	rebuildDone := make(chan error, 1)
	go func() {
		_, err := service.NewBudgetCounterRebuildService(infra.DB, infra.Redis, budgetperiod.Config{
			Timezone: "Asia/Shanghai", WeekStart: "monday",
		}).RebuildCurrentPeriods(context.Background(), now)
		rebuildDone <- err
	}()
	require.Eventually(t, func() bool {
		return infra.Redis.Exists(context.Background(), budgetrebuild.LockKey).Val() == 1
	}, 5*time.Second, time.Millisecond)
	require.ErrorIs(t,
		admission.Reserve(context.Background(), "docker-new-during-lock", time.Now()),
		gwbudget.ErrRebuildInProgress,
	)
	require.NoError(t, gwobs.NewCallLogSpool(infra.Redis).StoreReserved(context.Background(), obsmodel.CallLog{
		RequestID: "docker-inflight", TS: now.Add(-30 * time.Minute),
		Status: 200, UserID: "docker-race", KeyType: "personal", EstCostUSD: 0.000025,
		BudgetTierConstraintsSnapshot: []obsmodel.BudgetTierConstraintSnapshot{},
	}))
	require.NoError(t, reservation.Finish(context.Background()))
	require.NoError(t, <-rebuildDone)
	generation := infra.Redis.Get(context.Background(), gwbudget.ActiveGenerationKey).Val()
	value, err := infra.Redis.Get(context.Background(), gwbudget.GenerationKey(generation, key)).Int64()
	require.NoError(t, err)
	require.Equal(t, int64(3025), value)
	require.Zero(t, infra.Redis.XLen(context.Background(), gwobs.CallLogSpoolStream).Val())
	var persisted int64
	require.NoError(t, infra.DB.Model(&obsmodel.CallLog{}).
		Where("request_id = ?", "docker-inflight").Count(&persisted).Error)
	require.Equal(t, int64(1), persisted)
}

func TestModelTierBudgetIntegration_LostRebuildLeaseNeverSwitches(t *testing.T) {
	infra := startIntegrationInfra(t)
	require.NoError(t, infra.DB.Create(&cpmodel.BudgetAllocation{
		ID: uuid.New(), ScopeType: cpmodel.BudgetScopeMember, ScopeID: "docker-lost-lease",
		AmountUSD: 100, DailyAmountUSD: -1, WeeklyAmountUSD: -1, Enabled: true,
	}).Error)
	key := "budget:v2:used:member:docker-lost-lease:2026-07"
	require.NoError(t, infra.Redis.Set(context.Background(), key, "999", time.Hour).Err())
	admission := gwbudget.NewRedisStoreWithAdmission(infra.Redis, gwbudget.AdmissionConfig{
		SpoolCapacity: 10, ReservationTTL: time.Minute,
	})
	pauseReservation, err := admission.ReserveWithGeneration(context.Background(), "docker-pause", time.Now())
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		_, err := service.NewBudgetCounterRebuildService(infra.DB, infra.Redis, budgetperiod.Config{
			Timezone: "Asia/Shanghai", WeekStart: "monday",
		}).RebuildCurrentPeriods(context.Background(), time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC))
		done <- err
	}()
	require.Eventually(t, func() bool {
		return infra.Redis.Exists(context.Background(), budgetrebuild.LockKey).Val() == 1
	}, 5*time.Second, time.Millisecond)
	require.NoError(t, infra.Redis.Del(context.Background(), budgetrebuild.LockKey).Err())
	require.NoError(t, pauseReservation.Finish(context.Background()))
	require.Error(t, <-done)
	require.Equal(t, "999", infra.Redis.Get(context.Background(), key).Val())
	require.Equal(t, "", infra.Redis.Get(context.Background(), gwbudget.ActiveGenerationKey).Val())
}

func TestModelTierBudgetIntegration_HardSpoolCapacityWithoutGlobalConcurrencyLimit(t *testing.T) {
	infra := startIntegrationInfra(t)
	admission := gwbudget.NewRedisStoreWithAdmission(infra.Redis, gwbudget.AdmissionConfig{
		SpoolCapacity: 2, ReservationTTL: time.Minute,
	})
	var accepted atomic.Int32
	var wg sync.WaitGroup
	acceptedIDs := make(chan string, 20)
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			requestID := fmt.Sprintf("hard-cap-%02d", i)
			if _, err := admission.ReserveWithGeneration(context.Background(), requestID, time.Now()); err == nil {
				accepted.Add(1)
				acceptedIDs <- requestID
			}
		}()
	}
	wg.Wait()
	close(acceptedIDs)
	require.Equal(t, int32(2), accepted.Load())
	spool := gwobs.NewCallLogSpoolWithConfig(infra.Redis, gwobs.CallLogSpoolConfig{Capacity: 2})
	for requestID := range acceptedIDs {
		require.NoError(t, spool.StoreReserved(context.Background(), obsmodel.CallLog{
			RequestID: requestID, TS: time.Now(), Status: 200,
		}))
		require.NoError(t, admission.Release(context.Background(), requestID))
	}
	require.Equal(t, int64(2), infra.Redis.XLen(context.Background(), gwobs.CallLogSpoolStream).Val())
	require.Zero(t, infra.Redis.ZCard(context.Background(), gwbudget.BillableInflightKey).Val())
	require.ErrorIs(t,
		admission.Reserve(context.Background(), "hard-cap-overflow", time.Now()),
		gwbudget.ErrSpoolCapacity,
	)
}

func TestModelTierBudgetIntegration_LongStreamHeartbeatSurvivesReservationTTL(t *testing.T) {
	infra := startIntegrationInfra(t)
	store := gwbudget.NewRedisStoreWithAdmission(infra.Redis, gwbudget.AdmissionConfig{
		SpoolCapacity: 10, ReservationTTL: 300 * time.Millisecond,
		HeartbeatInterval: 60 * time.Millisecond,
	})
	holder := gwbudget.NewSnapshotHolder()
	holder.Set(&configsync.BudgetTierSnapshot{
		SchemaVersion: 1, MasterEnabled: true, AccountingEnabled: true,
		V2TotalEnforcementEnabled: true, Timezone: "Asia/Shanghai", WeekStart: "monday",
	})
	gate := gwbudget.NewGate(holder, store, nil, time.Now)
	cfg := gwobs.DefaultCollectorConfig()
	cfg.BillableReservationTTL = 300 * time.Millisecond
	cfg.DBConnectRetryInterval = 10 * time.Millisecond
	collectorCtx, cancelCollector := context.WithCancel(context.Background())
	t.Cleanup(cancelCollector)
	collector := gwobs.StartProductionCollector(collectorCtx, cfg, infra.Redis, nil,
		func(context.Context) (*gorm.DB, error) { return infra.DB, nil }, nil)
	require.Eventually(t, collector.HasLivePostgresSink, 5*time.Second, 10*time.Millisecond)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	started := make(chan struct{})
	finish := make(chan struct{})
	router.Use(func(c *gin.Context) {
		c.Set("request_id", "docker-long-stream")
		c.Set("identity", &gwmodel.UserIdentity{UserID: "docker-long-stream"})
		c.Next()
	})
	router.Use(gwmiddleware.UsageCollector(collector, nil, nil))
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		_, checkErr := gate.Check(c)
		require.NoError(t, checkErr)
		close(started)
		<-finish
		c.Status(http.StatusOK)
	})
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	}()
	<-started
	time.Sleep(750 * time.Millisecond)
	score, err := infra.Redis.ZScore(context.Background(), gwbudget.BillableInflightKey, "docker-long-stream").Result()
	require.NoError(t, err)
	require.Greater(t, score, float64(time.Now().UnixMilli()))

	rebuildDone := make(chan error, 1)
	go func() {
		_, rebuildErr := service.NewBudgetCounterRebuildService(infra.DB, infra.Redis, budgetperiod.Config{
			Timezone: "Asia/Shanghai", WeekStart: "monday",
		}).RebuildCurrentPeriods(context.Background(), time.Now())
		rebuildDone <- rebuildErr
	}()
	require.Eventually(t, func() bool {
		return infra.Redis.Exists(context.Background(), budgetrebuild.LockKey).Val() == 1
	}, 5*time.Second, 10*time.Millisecond)
	select {
	case rebuildErr := <-rebuildDone:
		t.Fatalf("rebuild cleared live long-stream reservation: %v", rebuildErr)
	case <-time.After(500 * time.Millisecond):
	}
	close(finish)
	<-requestDone
	require.NoError(t, <-rebuildDone)
}

func TestModelTierBudgetIntegration_AtomicGenerationSwitchRollbackAndLostLease(t *testing.T) {
	infra := startIntegrationInfra(t)
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	require.NoError(t, infra.DB.Create(&cpmodel.BudgetAllocation{
		ID: uuid.New(), ScopeType: cpmodel.BudgetScopeMember, ScopeID: "docker-generation",
		AmountUSD: 100, DailyAmountUSD: -1, WeeklyAmountUSD: -1, Enabled: true,
	}).Error)
	require.NoError(t, infra.DB.Create(&obsmodel.CallLog{
		RequestID: "docker-generation-fact", TS: now.Add(-time.Hour), Status: 200,
		UserID: "docker-generation", KeyType: "personal", EstCostUSD: 0.000123,
	}).Error)
	baseKey := "budget:v2:used:member:docker-generation:2026-07"
	oldGeneration := "docker-old"
	oldKey := gwbudget.GenerationKey(oldGeneration, baseKey)
	require.NoError(t, infra.Redis.Set(context.Background(), gwbudget.ActiveGenerationKey, oldGeneration, 0).Err())
	require.NoError(t, infra.Redis.Set(context.Background(), oldKey, "999", time.Hour).Err())

	result, err := service.NewBudgetCounterRebuildService(infra.DB, infra.Redis, budgetperiod.Config{
		Timezone: "Asia/Shanghai", WeekStart: "monday",
	}).RebuildCurrentPeriods(context.Background(), now)
	require.NoError(t, err)
	require.Equal(t, result.ActiveGeneration, infra.Redis.Get(context.Background(), gwbudget.ActiveGenerationKey).Val())
	require.Equal(t, "123", infra.Redis.Get(context.Background(),
		gwbudget.GenerationKey(result.ActiveGeneration, baseKey)).Val())
	require.Equal(t, "999", infra.Redis.Get(context.Background(), oldKey).Val())
	require.NoError(t, infra.Redis.Set(context.Background(), gwbudget.ActiveGenerationKey, oldGeneration, 0).Err())
	require.Equal(t, "999", infra.Redis.Get(context.Background(),
		gwbudget.GenerationKey(infra.Redis.Get(context.Background(), gwbudget.ActiveGenerationKey).Val(), baseKey)).Val())
	rollbackReservation, err := gwbudget.NewRedisStoreWithAdmission(infra.Redis, gwbudget.AdmissionConfig{
		SpoolCapacity: 10, ReservationTTL: time.Minute,
	}).ReserveWithGeneration(context.Background(), "docker-rollback-reader", time.Now())
	require.NoError(t, err)
	require.Equal(t, oldGeneration, rollbackReservation.Generation())
	require.NoError(t, rollbackReservation.Finish(context.Background()))

	var failedGeneration string
	rebuild := service.NewBudgetCounterRebuildServiceWithConfig(
		infra.DB, infra.Redis,
		budgetperiod.Config{Timezone: "Asia/Shanghai", WeekStart: "monday"},
		service.BudgetCounterRebuildConfig{BeforeActivate: func(generation string) {
			failedGeneration = generation
			_ = infra.Redis.Del(context.Background(), budgetrebuild.LockKey).Err()
		}},
	)
	_, err = rebuild.RebuildCurrentPeriods(context.Background(), now)
	require.Error(t, err)
	require.Equal(t, oldGeneration, infra.Redis.Get(context.Background(), gwbudget.ActiveGenerationKey).Val())
	if failedGeneration != "" {
		require.Empty(t, infra.Redis.Keys(context.Background(),
			"budget:v2:gen:"+failedGeneration+":*").Val())
	}
}

func TestModelTierBudgetIntegration_LostHeartbeatLateCompletionAccountsNewGenerationOnce(t *testing.T) {
	infra := startIntegrationInfra(t)
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	require.NoError(t, infra.DB.Create(&cpmodel.BudgetAllocation{
		ID: uuid.New(), ScopeType: cpmodel.BudgetScopeMember, ScopeID: "docker-late",
		AmountUSD: 10, DailyAmountUSD: -1, WeeklyAmountUSD: -1, Enabled: true,
	}).Error)
	require.NoError(t, infra.DB.Create(&obsmodel.CallLog{
		RequestID: "docker-late-history", TS: now.Add(-time.Hour), Status: 200,
		UserID: "docker-late", KeyType: "personal", EstCostUSD: 0.0001,
	}).Error)
	oldGeneration := "docker-late-old"
	baseKey := "budget:v2:used:member:docker-late:2026-07"
	require.NoError(t, infra.Redis.Set(context.Background(), gwbudget.ActiveGenerationKey, oldGeneration, 0).Err())
	holder := gwbudget.NewSnapshotHolder()
	holder.Set(&configsync.BudgetTierSnapshot{
		SchemaVersion: 1, MasterEnabled: true, AccountingEnabled: true,
		V2TotalEnforcementEnabled: true, Timezone: "Asia/Shanghai", WeekStart: "monday",
	})
	store := gwbudget.NewRedisStoreWithAdmission(infra.Redis, gwbudget.AdmissionConfig{
		SpoolCapacity: 100, ReservationTTL: time.Second, HeartbeatInterval: 20 * time.Millisecond,
	})
	gate := gwbudget.NewGate(holder, store, nil, func() time.Time { return now })
	ginCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ginCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ginCtx.Set("request_id", "docker-late-completion")
	ginCtx.Set("identity", &gwmodel.UserIdentity{
		UserID: "docker-late",
		BudgetChain: []gwmodel.BudgetScope{{
			ScopeType: "member", ScopeID: "docker-late", AmountUSD: 10,
			DailyAmountUSD: -1, WeeklyAmountUSD: -1,
		}},
	})
	_, err := gate.Check(ginCtx)
	require.NoError(t, err)
	reservation := gwbudget.GetBillableReservation(ginCtx)
	require.NotNil(t, reservation)
	plan, ok := gwbudget.GetAccountingPlan(ginCtx)
	require.True(t, ok)
	require.NoError(t, infra.Redis.ZRem(context.Background(), gwbudget.BillableInflightKey, "docker-late-completion").Err())

	readyToActivate := make(chan struct{})
	allowActivate := make(chan struct{})
	rebuildDone := make(chan error, 1)
	go func() {
		_, rebuildErr := service.NewBudgetCounterRebuildServiceWithConfig(
			infra.DB, infra.Redis,
			budgetperiod.Config{Timezone: "Asia/Shanghai", WeekStart: "monday"},
			service.BudgetCounterRebuildConfig{BeforeActivate: func(string) {
				close(readyToActivate)
				<-allowActivate
			}},
		).RebuildCurrentPeriods(context.Background(), now)
		rebuildDone <- rebuildErr
	}()
	<-readyToActivate
	require.Eventually(t, reservation.Lost, time.Second, 10*time.Millisecond)
	collectorCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cfg := gwobs.DefaultCollectorConfig()
	cfg.DBConnectRetryInterval = 10 * time.Millisecond
	cfg.SpoolReplayInterval = 10 * time.Millisecond
	collector := gwobs.StartProductionCollector(collectorCtx, cfg, infra.Redis, nil,
		func(context.Context) (*gorm.DB, error) { return infra.DB, nil }, nil)
	require.Eventually(t, collector.HasLivePostgresSink, 5*time.Second, 10*time.Millisecond)
	completionDone := make(chan struct{})
	go func() {
		defer close(completionDone)
		collector.Emit(gwobs.UsageEvent{
			RequestID: "docker-late-completion", Timestamp: now, Status: 200,
			BillableReserved: true, BillableReservation: reservation,
			PendingBudgetAccount: &gwbudget.PendingAccount{
				Plan: gwbudget.PlanForAccounting(plan), CostMicroUSD: 23,
			},
		})
	}()
	close(allowActivate)
	require.NoError(t, <-rebuildDone)
	<-completionDone
	newGeneration := infra.Redis.Get(context.Background(), gwbudget.ActiveGenerationKey).Val()
	require.NotEqual(t, oldGeneration, newGeneration)
	require.Equal(t, "123", infra.Redis.Get(context.Background(),
		gwbudget.GenerationKey(newGeneration, baseKey)).Val())
	require.Eventually(t, func() bool {
		var count int64
		return infra.DB.Model(&obsmodel.CallLog{}).
			Where("request_id = ?", "docker-late-completion").Count(&count).Error == nil && count == 1
	}, 5*time.Second, 20*time.Millisecond)
	require.Equal(t, "1", infra.Redis.Get(context.Background(),
		"budget:v2:accounted:docker-late-completion").Val())
}
