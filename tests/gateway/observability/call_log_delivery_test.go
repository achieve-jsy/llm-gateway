package observability_test

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
	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
	obsrepo "github.com/example/llm-gateway/internal/gateway/observability/repository"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestBatchWriter_DBFailureSpoolsAndReplaysIdempotently(t *testing.T) {
	oldMeter := otel.GetMeterProvider()
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(meterProvider)
	t.Cleanup(func() {
		otel.SetMeterProvider(oldMeter)
		_ = meterProvider.Shutdown(context.Background())
	})
	db, err := gorm.Open(sqlite.Open("file:call_log_spool?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallLog{}))
	var failWrites atomic.Bool
	failWrites.Store(true)
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:fail_call_logs", func(tx *gorm.DB) {
		if failWrites.Load() && tx.Statement.Schema != nil && tx.Statement.Schema.Table == "call_logs" {
			_ = tx.AddError(errors.New("temporary postgres failure"))
		}
	}))
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	cfg := gwobs.DefaultCollectorConfig()
	cfg.BufferSize = 2
	cfg.FlushN = 1
	cfg.FlushInterval = 5 * time.Millisecond
	cfg.DBRetryCount = 2
	cfg.DBRetryBackoff = time.Millisecond
	cfg.SpoolReplayInterval = 10 * time.Millisecond
	collector := gwobs.NewCollector(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	collector.StartWriterWithSpool(ctx, obsrepo.NewCallLogRepository(db), rdb)
	collector.Emit(gwobs.UsageEvent{RequestID: "spooled-request", Timestamp: time.Now(), Status: 200})

	require.Eventually(t, func() bool {
		return rdb.XLen(context.Background(), gwobs.CallLogSpoolStream).Val() == 1
	}, time.Second, 10*time.Millisecond)
	failWrites.Store(false)
	require.Eventually(t, func() bool {
		var count int64
		_ = db.Model(&obsmodel.CallLog{}).Where("request_id = ?", "spooled-request").Count(&count).Error
		return count == 1 && rdb.XLen(context.Background(), gwobs.CallLogSpoolStream).Val() == 0
	}, 2*time.Second, 10*time.Millisecond)

	// 重复 request_id 仍由数据库唯一键幂等保护。
	collector.Emit(gwobs.UsageEvent{RequestID: "spooled-request", Timestamp: time.Now(), Status: 200})
	require.Eventually(t, func() bool {
		var count int64
		_ = db.Model(&obsmodel.CallLog{}).Where("request_id = ?", "spooled-request").Count(&count).Error
		return count == 1
	}, time.Second, 10*time.Millisecond)
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	require.True(t, hasMetric(rm, "gateway_call_log_db_retry_total"))
	require.True(t, hasMetric(rm, "gateway_call_log_spooled_total"))
	require.True(t, hasMetric(rm, "gateway_call_log_spool_replayed_total"))
}

func TestCollector_QueueOverflowUsesSynchronousFallbackAndRecordsMetric(t *testing.T) {
	oldMeter := otel.GetMeterProvider()
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(meterProvider)
	t.Cleanup(func() {
		otel.SetMeterProvider(oldMeter)
		_ = meterProvider.Shutdown(context.Background())
	})

	db, err := gorm.Open(sqlite.Open("file:call_log_fallback?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallLog{}))
	collector := gwobs.NewCollector(gwobs.CollectorConfig{
		Enabled: true, BufferSize: 1, FlushN: 1, FlushInterval: time.Second,
		EnqueueTimeout: 5 * time.Millisecond, SyncFallbackTimeout: time.Second,
	}, nil)
	writerCtx, cancel := context.WithCancel(context.Background())
	cancel()
	collector.StartWriter(writerCtx, obsrepo.NewCallLogRepository(db))
	time.Sleep(10 * time.Millisecond)

	collector.Emit(gwobs.UsageEvent{RequestID: "queued", Timestamp: time.Now(), Status: 200})
	collector.Emit(gwobs.UsageEvent{RequestID: "fallback", Timestamp: time.Now(), Status: 200})

	var persisted int64
	require.NoError(t, db.Model(&obsmodel.CallLog{}).Where("request_id = ?", "fallback").Count(&persisted).Error)
	require.Equal(t, int64(1), persisted)
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	require.True(t, hasMetric(rm, "gateway_call_log_sync_fallback_total"))
}

func TestBatchWriter_ShutdownDrainsBufferedEvents(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:call_log_shutdown_drain?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallLog{}))
	cfg := gwobs.DefaultCollectorConfig()
	cfg.BufferSize = 10
	cfg.FlushN = 100
	cfg.FlushInterval = time.Hour
	collector := gwobs.NewCollector(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	collector.StartWriter(ctx, obsrepo.NewCallLogRepository(db))
	for i := 0; i < 5; i++ {
		collector.Emit(gwobs.UsageEvent{
			RequestID: fmt.Sprintf("shutdown-%d", i), Timestamp: time.Now(), Status: 200,
		})
	}
	cancel()
	waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Second)
	require.NoError(t, collector.Wait(waitCtx))
	waitCancel()
	require.Eventually(t, func() bool {
		var count int64
		_ = db.Model(&obsmodel.CallLog{}).Where("request_id LIKE ?", "shutdown-%").Count(&count).Error
		return count == 5
	}, time.Second, 10*time.Millisecond)
}

func TestCollector_NoRedisReportsFailureAfterBoundedSynchronousRetries(t *testing.T) {
	oldMeter := otel.GetMeterProvider()
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(meterProvider)
	t.Cleanup(func() {
		otel.SetMeterProvider(oldMeter)
		_ = meterProvider.Shutdown(context.Background())
	})
	db, err := gorm.Open(sqlite.Open("file:call_log_no_redis?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallLog{}))
	var attempts atomic.Int32
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:fail_without_redis", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "call_logs" {
			attempts.Add(1)
			_ = tx.AddError(errors.New("postgres unavailable"))
		}
	}))
	cfg := gwobs.DefaultCollectorConfig()
	cfg.BufferSize = 0
	cfg.EnqueueTimeout = time.Millisecond
	cfg.SyncFallbackTimeout = time.Second
	cfg.DBRetryCount = 2
	cfg.DBRetryBackoff = time.Millisecond
	collector := gwobs.NewCollector(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	collector.StartWriter(ctx, obsrepo.NewCallLogRepository(db))
	time.Sleep(5 * time.Millisecond)
	collector.Emit(gwobs.UsageEvent{RequestID: "unrecoverable", Timestamp: time.Now(), Status: 200})
	require.GreaterOrEqual(t, attempts.Load(), int32(3))
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	require.True(t, hasMetric(rm, "gateway_call_log_db_retry_total"))
	require.True(t, hasMetric(rm, "gateway_call_log_unrecoverable_total"))
	require.True(t, hasMetric(rm, "gateway_call_log_delivery_failure_total"))
}

func TestProductionCollector_StreamFirstSurvivesInitialPostgresOutage(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:production_stream_first?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallLog{}))
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	var postgresReady atomic.Bool
	var connectAttempts atomic.Int32
	cfg := gwobs.DefaultCollectorConfig()
	cfg.DBConnectRetryInterval = 10 * time.Millisecond
	cfg.SpoolReplayInterval = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	collector := gwobs.StartProductionCollector(
		ctx,
		cfg,
		rdb,
		nil,
		func(context.Context) (*gorm.DB, error) {
			connectAttempts.Add(1)
			if !postgresReady.Load() {
				return nil, errors.New("postgres starting")
			}
			return db, nil
		},
		nil,
	)

	collector.Emit(gwobs.UsageEvent{RequestID: "pg-startup-outage", Timestamp: time.Now(), Status: 200})
	require.Eventually(t, func() bool {
		return rdb.XLen(context.Background(), gwobs.CallLogSpoolStream).Val() == 1
	}, time.Second, 10*time.Millisecond)
	var before int64
	require.NoError(t, db.Model(&obsmodel.CallLog{}).Count(&before).Error)
	require.Zero(t, before)

	postgresReady.Store(true)
	require.Eventually(t, func() bool {
		var count int64
		_ = db.Model(&obsmodel.CallLog{}).Where("request_id = ?", "pg-startup-outage").Count(&count).Error
		return count == 1 && rdb.XLen(context.Background(), gwobs.CallLogSpoolStream).Val() == 0
	}, 2*time.Second, 10*time.Millisecond)
	require.GreaterOrEqual(t, connectAttempts.Load(), int32(2))

	collector.Emit(gwobs.UsageEvent{RequestID: "pg-startup-outage", Timestamp: time.Now(), Status: 200})
	require.Eventually(t, func() bool {
		var count int64
		_ = db.Model(&obsmodel.CallLog{}).Where("request_id = ?", "pg-startup-outage").Count(&count).Error
		return count == 1 && rdb.XLen(context.Background(), gwobs.CallLogSpoolStream).Val() == 0
	}, time.Second, 10*time.Millisecond)
}

type isolatingSink struct {
	mu       sync.Mutex
	inserted []string
}

func (s *isolatingSink) BatchInsert(_ context.Context, logs []obsmodel.CallLog) error {
	if len(logs) > 1 {
		return errors.New("batch rejected")
	}
	if len(logs) == 1 && logs[0].RequestID == "poison" {
		return errors.New("poison row")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, log := range logs {
		s.inserted = append(s.inserted, log.RequestID)
	}
	return nil
}

func TestCallLogSpool_IsolatesPoisonAndSplitsFailedBatch(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	spool := gwobs.NewCallLogSpoolWithConfig(rdb, gwobs.CallLogSpoolConfig{MaxRetries: 2})
	require.NoError(t, spool.Store(context.Background(), []obsmodel.CallLog{
		{RequestID: "poison", TS: time.Now()},
		{RequestID: "healthy", TS: time.Now()},
	}))
	sink := &isolatingSink{}

	require.NoError(t, spool.ReplayOnce(context.Background(), sink))
	require.Equal(t, []string{"healthy"}, sink.inserted)
	require.Equal(t, int64(1), rdb.XLen(context.Background(), gwobs.CallLogSpoolStream).Val())
	require.NoError(t, spool.ReplayOnce(context.Background(), sink))
	require.Equal(t, int64(0), rdb.XLen(context.Background(), gwobs.CallLogSpoolStream).Val())
	require.Equal(t, int64(1), rdb.XLen(context.Background(), gwobs.CallLogSpoolDLQStream).Val())
	dlq := rdb.XRange(context.Background(), gwobs.CallLogSpoolDLQStream, "-", "+").Val()
	require.Equal(t, "poison", fmt.Sprint(dlq[0].Values["request_id"]))
	require.Equal(t, "2", fmt.Sprint(dlq[0].Values["retries"]))
	require.Contains(t, fmt.Sprint(dlq[0].Values["error"]), "poison row")
}

func TestCallLogSpool_MalformedPayloadMovesToDLQWithoutBlockingNext(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	spool := gwobs.NewCallLogSpoolWithConfig(rdb, gwobs.CallLogSpoolConfig{MaxRetries: 1})
	require.NoError(t, rdb.XAdd(context.Background(), &redis.XAddArgs{
		Stream: gwobs.CallLogSpoolStream,
		Values: map[string]any{"request_id": "malformed", "payload": "{"},
	}).Err())
	require.NoError(t, spool.Store(context.Background(), []obsmodel.CallLog{{
		RequestID: "after-malformed", TS: time.Now(),
	}}))
	sink := &isolatingSink{}

	require.NoError(t, spool.ReplayOnce(context.Background(), sink))
	require.Equal(t, []string{"after-malformed"}, sink.inserted)
	require.Equal(t, int64(1), rdb.XLen(context.Background(), gwobs.CallLogSpoolDLQStream).Val())
}

func TestProductionCollector_HighWatermarkRejectsNewButStillSpoolsInflight(t *testing.T) {
	oldMeter := otel.GetMeterProvider()
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(meterProvider)
	t.Cleanup(func() {
		otel.SetMeterProvider(oldMeter)
		_ = meterProvider.Shutdown(context.Background())
	})
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	cfg := gwobs.DefaultCollectorConfig()
	cfg.SpoolCapacity = 3
	cfg.SpoolHighWatermark = 2
	cfg.SpoolHealthCheckInterval = 10 * time.Millisecond
	cfg.DBConnectRetryInterval = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	collector := gwobs.StartProductionCollector(
		ctx, cfg, rdb, nil,
		func(context.Context) (*gorm.DB, error) { return nil, errors.New("postgres down") },
		nil,
	)
	require.NoError(t, rdb.XAdd(context.Background(), &redis.XAddArgs{
		Stream: gwobs.CallLogSpoolStream,
		Values: map[string]any{"request_id": "queued", "payload": "{}"},
	}).Err())
	require.NoError(t, rdb.ZAdd(context.Background(), budget.BillableInflightKey,
		redis.Z{Score: float64(time.Now().Add(time.Minute).UnixMilli()), Member: "reserved-capacity"},
	).Err())
	require.Eventually(t, func() bool {
		return !collector.CanAcceptBillableRequest()
	}, time.Second, 10*time.Millisecond)
	var metrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &metrics))
	require.True(t, hasMetric(metrics, "gateway_call_log_spool_high_watermark_total"))

	collector.Emit(gwobs.UsageEvent{
		RequestID: "reserved-capacity", Timestamp: time.Now(), Status: 200, BillableReserved: true,
	})
	require.Eventually(t, func() bool {
		return rdb.XLen(context.Background(), gwobs.CallLogSpoolStream).Val() == 2 &&
			rdb.ZCard(context.Background(), budget.BillableInflightKey).Val() == 0
	}, time.Second, 10*time.Millisecond)
}

func TestCallLogSpool_ReservedCompletionAtomicallyAppendsAndReleases(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	require.NoError(t, rdb.ZAdd(context.Background(), budget.BillableInflightKey,
		redis.Z{Score: float64(time.Now().Add(time.Minute).UnixMilli()), Member: "reserved"},
	).Err())
	spool := gwobs.NewCallLogSpool(rdb)

	require.NoError(t, spool.StoreReserved(context.Background(), obsmodel.CallLog{
		RequestID: "reserved", TS: time.Now(), Status: 200,
	}))
	require.Equal(t, int64(1), rdb.XLen(context.Background(), gwobs.CallLogSpoolStream).Val())
	require.Zero(t, rdb.ZCard(context.Background(), budget.BillableInflightKey).Val())
}

func TestCallLogSpool_UnreservedAppendCannotConsumeReservedHardCapacity(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	require.NoError(t, rdb.ZAdd(context.Background(), budget.BillableInflightKey,
		redis.Z{Score: float64(time.Now().Add(time.Minute).UnixMilli()), Member: "reserved"},
	).Err())
	spool := gwobs.NewCallLogSpoolWithConfig(rdb, gwobs.CallLogSpoolConfig{
		MaxRetries: 3, Capacity: 1,
	})

	require.Error(t, spool.Store(context.Background(), []obsmodel.CallLog{{
		RequestID: "unreserved", TS: time.Now(), Status: 403,
	}}))
	require.Zero(t, rdb.XLen(context.Background(), gwobs.CallLogSpoolStream).Val())
	require.Equal(t, int64(1), rdb.ZCard(context.Background(), budget.BillableInflightKey).Val())
}

func TestCallLogSpool_ExpiredReservationFallsBackToCapacityCheckedAppend(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	spool := gwobs.NewCallLogSpoolWithConfig(rdb, gwobs.CallLogSpoolConfig{Capacity: 1})

	require.NoError(t, spool.StoreReserved(context.Background(), obsmodel.CallLog{
		RequestID: "expired-before-completion", TS: time.Now(), Status: 200,
	}))
	require.Equal(t, int64(1), rdb.XLen(context.Background(), gwobs.CallLogSpoolStream).Val())
	require.Error(t, spool.Store(context.Background(), []obsmodel.CallLog{{
		RequestID: "cannot-overflow", TS: time.Now(), Status: 403,
	}}))
}

func TestProductionCollector_RedisAppendFallbackAlwaysFinishesReservation(t *testing.T) {
	oldMeter := otel.GetMeterProvider()
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(meterProvider)
	t.Cleanup(func() {
		otel.SetMeterProvider(oldMeter)
		_ = meterProvider.Shutdown(context.Background())
	})
	for _, tc := range []struct {
		name       string
		failDB     bool
		wantStored int64
	}{
		{name: "postgres fallback succeeds", wantStored: 1},
		{name: "postgres fallback fails", failDB: true, wantStored: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open("file:fallback_finish_"+fmt.Sprint(tc.failDB)+"?mode=memory&cache=shared"), &gorm.Config{})
			require.NoError(t, err)
			require.NoError(t, db.AutoMigrate(&obsmodel.CallLog{}))
			if tc.failDB {
				require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:fail_fallback", func(tx *gorm.DB) {
					if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "call_logs" {
						_ = tx.AddError(errors.New("postgres fallback unavailable"))
					}
				}))
			}
			mr := miniredis.RunT(t)
			spoolClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
			reservationClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
			cfg := gwobs.DefaultCollectorConfig()
			cfg.DBConnectRetryInterval = 5 * time.Millisecond
			cfg.SyncFallbackTimeout = time.Second
			cfg.DBRetryBackoff = time.Millisecond
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			collector := gwobs.StartProductionCollector(ctx, cfg, spoolClient, nil,
				func(context.Context) (*gorm.DB, error) { return db, nil }, nil)
			require.Eventually(t, func() bool { return collector.HasLivePostgresSink() }, time.Second, 5*time.Millisecond)
			requestID := "fallback-finish-" + fmt.Sprint(tc.failDB)
			reservation, err := budget.NewRedisStoreWithAdmission(reservationClient, budget.AdmissionConfig{
				SpoolCapacity: 10, ReservationTTL: time.Minute, HeartbeatInterval: 10 * time.Millisecond,
			}).ReserveWithGeneration(context.Background(), requestID, time.Now())
			require.NoError(t, err)
			require.NoError(t, spoolClient.Close())

			collector.Emit(gwobs.UsageEvent{
				RequestID: requestID, Timestamp: time.Now(), Status: 200,
				BillableReserved: true, BillableReservation: reservation,
			})
			require.Zero(t, reservationClient.ZCard(context.Background(), budget.BillableInflightKey).Val())
			var count int64
			require.NoError(t, db.Model(&obsmodel.CallLog{}).Count(&count).Error)
			require.Equal(t, tc.wantStored, count)
		})
	}
	var metrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &metrics))
	require.True(t, hasMetric(metrics, "gateway_call_log_delivery_failure_total"))
}

func TestProductionCollector_MissingReservationWaitsForRebuildAndAccountsNewGeneration(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	require.NoError(t, rdb.Set(context.Background(), budget.ActiveGenerationKey, "old", 0).Err())
	store := budget.NewRedisStoreWithAdmission(rdb, budget.AdmissionConfig{
		SpoolCapacity: 10, ReservationTTL: time.Minute, HeartbeatInterval: 10 * time.Millisecond,
	})
	reservation, err := store.ReserveWithGeneration(context.Background(), "late-completion", time.Now())
	require.NoError(t, err)
	require.NoError(t, rdb.ZRem(context.Background(), budget.BillableInflightKey, "late-completion").Err())
	require.NoError(t, rdb.Set(context.Background(), budget.RebuildLockKey, "rebuild-owner", time.Minute).Err())
	baseKey := "budget:v2:used:member:u-late:2026-07"
	plan := &budget.AccountingPlan{
		Generation: "old", RedisCheckSucceeded: true,
		Constraints: []budget.Constraint{{
			Dimension: budget.DimensionTotal, Key: baseKey,
			PeriodType: budgetdomain.PeriodMonthly, ResetAt: time.Now().Add(24 * time.Hour),
		}},
	}
	collectorCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cfg := gwobs.DefaultCollectorConfig()
	cfg.DBConnectRetryInterval = time.Hour
	cfg.SyncFallbackTimeout = time.Second
	collector := gwobs.StartProductionCollector(collectorCtx, cfg, rdb, nil,
		func(context.Context) (*gorm.DB, error) { return nil, errors.New("postgres unavailable") }, nil)
	done := make(chan struct{})
	event := gwobs.UsageEvent{
		RequestID: "late-completion", Timestamp: time.Now(), Status: 200,
		BillableReserved: true, BillableReservation: reservation,
		PendingBudgetAccount: &budget.PendingAccount{Plan: plan, CostMicroUSD: 123},
	}
	go func() {
		defer close(done)
		collector.Emit(event)
	}()
	time.Sleep(30 * time.Millisecond)
	require.NoError(t, rdb.Set(context.Background(), budget.ActiveGenerationKey, "new", 0).Err())
	require.NoError(t, rdb.Del(context.Background(), budget.RebuildLockKey).Err())
	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, "123", rdb.Get(context.Background(), budget.GenerationKey("new", baseKey)).Val())
	require.Equal(t, "", rdb.Get(context.Background(), budget.GenerationKey("old", baseKey)).Val())
	require.Equal(t, int64(1), rdb.XLen(context.Background(), gwobs.CallLogSpoolStream).Val())
	collector.Emit(event)
	require.Equal(t, "123", rdb.Get(context.Background(), budget.GenerationKey("new", baseKey)).Val())
}

func TestProductionCollector_UnsafeRebindPersistsFactAndMarksReconciliation(t *testing.T) {
	oldMeter := otel.GetMeterProvider()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		otel.SetMeterProvider(oldMeter)
		_ = provider.Shutdown(context.Background())
	})
	db, err := gorm.Open(sqlite.Open("file:unsafe_rebind_fallback?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallLog{}))
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := budget.NewRedisStoreWithAdmission(rdb, budget.AdmissionConfig{
		SpoolCapacity: 10, ReservationTTL: time.Minute, HeartbeatInterval: 10 * time.Millisecond,
	})
	reservation, err := store.ReserveWithGeneration(context.Background(), "unsafe-rebind", time.Now())
	require.NoError(t, err)
	require.NoError(t, rdb.ZRem(context.Background(), budget.BillableInflightKey, "unsafe-rebind").Err())
	require.NoError(t, rdb.Set(context.Background(), budget.RebuildLockKey, "stuck-rebuild", time.Minute).Err())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cfg := gwobs.DefaultCollectorConfig()
	cfg.SyncFallbackTimeout = 80 * time.Millisecond
	cfg.DBConnectRetryInterval = 5 * time.Millisecond
	collector := gwobs.StartProductionCollector(ctx, cfg, rdb, nil,
		func(context.Context) (*gorm.DB, error) { return db, nil }, nil)
	require.Eventually(t, collector.HasLivePostgresSink, time.Second, 5*time.Millisecond)
	baseKey := "budget:v2:used:member:u-unsafe:2026-07"
	collector.Emit(gwobs.UsageEvent{
		RequestID: "unsafe-rebind", Timestamp: time.Now(), Status: 200,
		BillableReserved: true, BillableReservation: reservation,
		PendingBudgetAccount: &budget.PendingAccount{
			CostMicroUSD: 55,
			Plan: &budget.AccountingPlan{Generation: "legacy", RedisCheckSucceeded: true, Constraints: []budget.Constraint{{
				Dimension: budget.DimensionTotal, Key: baseKey,
				PeriodType: budgetdomain.PeriodMonthly, ResetAt: time.Now().Add(24 * time.Hour),
			}}},
		},
	})
	var count int64
	require.NoError(t, db.Model(&obsmodel.CallLog{}).Where("request_id = ?", "unsafe-rebind").Count(&count).Error)
	require.Equal(t, int64(1), count)
	require.Zero(t, rdb.Exists(context.Background(), baseKey).Val())
	var metrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &metrics))
	require.True(t, hasMetric(metrics, "gateway_budget_counter_reconciliation_required_total"))
}

func TestProductionCollector_AdmissionFailOpenPrefersSynchronousPostgres(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:admission_fail_open_pg_first?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallLog{}))
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cfg := gwobs.DefaultCollectorConfig()
	cfg.DBConnectRetryInterval = 5 * time.Millisecond
	collector := gwobs.StartProductionCollector(ctx, cfg, rdb, nil,
		func(context.Context) (*gorm.DB, error) { return db, nil }, nil)
	require.Eventually(t, collector.HasLivePostgresSink, time.Second, 5*time.Millisecond)

	collector.Emit(gwobs.UsageEvent{
		RequestID: "admission-fail-open-pg-first", Timestamp: time.Now(), Status: 200,
		BudgetAdmissionFailOpen: true,
	})
	var count int64
	require.NoError(t, db.Model(&obsmodel.CallLog{}).
		Where("request_id = ?", "admission-fail-open-pg-first").Count(&count).Error)
	require.Equal(t, int64(1), count)
	require.Zero(t, rdb.XLen(context.Background(), gwobs.CallLogSpoolStream).Val())
}

func TestProductionCollector_UnsafeRebindWithoutPostgresSpoolsReconciliationFact(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := budget.NewRedisStoreWithAdmission(rdb, budget.AdmissionConfig{
		SpoolCapacity: 10, ReservationTTL: time.Minute, HeartbeatInterval: 10 * time.Millisecond,
	})
	reservation, err := store.ReserveWithGeneration(context.Background(), "reconcile-spool", time.Now())
	require.NoError(t, err)
	require.NoError(t, rdb.ZRem(context.Background(), budget.BillableInflightKey, "reconcile-spool").Err())
	require.NoError(t, rdb.Set(context.Background(), budget.RebuildLockKey, "stuck", time.Minute).Err())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cfg := gwobs.DefaultCollectorConfig()
	cfg.SyncFallbackTimeout = 50 * time.Millisecond
	cfg.DBConnectRetryInterval = time.Hour
	collector := gwobs.StartProductionCollector(ctx, cfg, rdb, nil,
		func(context.Context) (*gorm.DB, error) { return nil, errors.New("postgres unavailable") }, nil)
	baseKey := "budget:v2:used:member:u-reconcile:2026-07"
	collector.Emit(gwobs.UsageEvent{
		RequestID: "reconcile-spool", Timestamp: time.Now(), Status: 200,
		BillableReserved: true, BillableReservation: reservation,
		PendingBudgetAccount: &budget.PendingAccount{
			CostMicroUSD: 77,
			Plan: &budget.AccountingPlan{Generation: "legacy", RedisCheckSucceeded: true, Constraints: []budget.Constraint{{
				Dimension: budget.DimensionTotal, Key: baseKey,
				PeriodType: budgetdomain.PeriodMonthly, ResetAt: time.Now().Add(time.Hour),
			}}},
		},
	})
	require.Equal(t, int64(1), rdb.XLen(context.Background(), gwobs.CallLogSpoolStream).Val())
	require.Zero(t, rdb.Exists(context.Background(), baseKey).Val())
}

func TestProductionCollector_AtomicAccountDoesNotPartiallyWriteWhenStreamCorrupt(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := budget.NewRedisStoreWithAdmission(rdb, budget.AdmissionConfig{
		SpoolCapacity: 10, ReservationTTL: time.Minute,
	})
	reservation, err := store.ReserveWithGeneration(context.Background(), "corrupt-stream-account", time.Now())
	require.NoError(t, err)
	require.NoError(t, rdb.Set(context.Background(), gwobs.CallLogSpoolStream, "wrong-type", 0).Err())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cfg := gwobs.DefaultCollectorConfig()
	cfg.DBConnectRetryInterval = time.Hour
	cfg.SyncFallbackTimeout = 50 * time.Millisecond
	collector := gwobs.StartProductionCollector(ctx, cfg, rdb, nil,
		func(context.Context) (*gorm.DB, error) { return nil, errors.New("postgres unavailable") }, nil)
	baseKey := "budget:v2:used:member:u-corrupt-stream:2026-07"
	collector.Emit(gwobs.UsageEvent{
		RequestID: "corrupt-stream-account", Timestamp: time.Now(), Status: 200,
		BillableReserved: true, BillableReservation: reservation,
		PendingBudgetAccount: &budget.PendingAccount{
			CostMicroUSD: 91,
			Plan: &budget.AccountingPlan{Generation: "legacy", RedisCheckSucceeded: true, Constraints: []budget.Constraint{{
				Dimension: budget.DimensionTotal, Key: baseKey,
				PeriodType: budgetdomain.PeriodMonthly, ResetAt: time.Now().Add(time.Hour),
			}}},
		},
	})
	require.Zero(t, rdb.Exists(context.Background(), baseKey).Val())
	require.Zero(t, rdb.Exists(context.Background(), "budget:v2:accounted:corrupt-stream-account").Val())
}

func TestProductionCollector_ThresholdCallbackSeesPostAccountValueExactlyOnce(t *testing.T) {
	for _, tc := range []struct {
		name    string
		initial int64
		cost    int64
		want    int64
	}{
		{name: "crosses 80 percent", initial: 79, cost: 2, want: 81},
		{name: "reaches 100 percent", initial: 99, cost: 1, want: 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mr := miniredis.RunT(t)
			rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
			requestID := "threshold-" + fmt.Sprint(tc.want)
			baseKey := "budget:v2:used:member:u-threshold:2026-07"
			require.NoError(t, rdb.Set(context.Background(), baseKey, tc.initial, 0).Err())
			store := budget.NewRedisStoreWithAdmission(rdb, budget.AdmissionConfig{
				SpoolCapacity: 10, ReservationTTL: time.Minute,
			})
			reservation, err := store.ReserveWithGeneration(context.Background(), requestID, time.Now())
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			cfg := gwobs.DefaultCollectorConfig()
			cfg.DBConnectRetryInterval = time.Hour
			collector := gwobs.StartProductionCollector(ctx, cfg, rdb, nil,
				func(context.Context) (*gorm.DB, error) { return nil, errors.New("postgres unavailable") }, nil)
			var callbackCount atomic.Int32
			var observed atomic.Int64
			event := gwobs.UsageEvent{
				RequestID: requestID, Timestamp: time.Now(), Status: 200,
				BillableReserved: true, BillableReservation: reservation,
				PendingBudgetAccount: &budget.PendingAccount{
					CostMicroUSD: tc.cost,
					Plan: &budget.AccountingPlan{Generation: "legacy", RedisCheckSucceeded: true, Constraints: []budget.Constraint{{
						Dimension: budget.DimensionTotal, Key: baseKey,
						PeriodType: budgetdomain.PeriodMonthly, ResetAt: time.Now().Add(time.Hour),
					}}},
					OnAccounted: func(ctx context.Context) {
						value, _ := rdb.Get(ctx, baseKey).Int64()
						observed.Store(value)
						callbackCount.Add(1)
					},
				},
			}
			collector.Emit(event)
			collector.Emit(event)
			require.Equal(t, tc.want, observed.Load())
			require.Equal(t, int32(1), callbackCount.Load())
			value, err := rdb.Get(context.Background(), baseKey).Int64()
			require.NoError(t, err)
			require.Equal(t, tc.want, value)
		})
	}
}

type blockingReplaySink struct {
	started chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (s *blockingReplaySink) BatchInsert(context.Context, []obsmodel.CallLog) error {
	s.calls.Add(1)
	select {
	case s.started <- struct{}{}:
	default:
	}
	<-s.release
	return nil
}

func TestCallLogSpool_XAutoClaimDoesNotStealActiveMultiInstanceBatch(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	cfg := gwobs.CallLogSpoolConfig{MaxRetries: 3, ClaimMinIdle: 30 * time.Second}
	first := gwobs.NewCallLogSpoolWithConfig(rdb, cfg)
	second := gwobs.NewCallLogSpoolWithConfig(rdb, cfg)
	require.NoError(t, first.Store(context.Background(), []obsmodel.CallLog{{
		RequestID: "active-batch", TS: time.Now(), Status: 200,
	}}))
	firstSink := &blockingReplaySink{started: make(chan struct{}, 1), release: make(chan struct{})}
	firstDone := make(chan error, 1)
	go func() { firstDone <- first.ReplayOnce(context.Background(), firstSink) }()
	<-firstSink.started
	secondSink := &isolatingSink{}

	require.NoError(t, second.ReplayOnce(context.Background(), secondSink))
	require.Empty(t, secondSink.inserted)
	close(firstSink.release)
	require.NoError(t, <-firstDone)
	require.Equal(t, int32(1), firstSink.calls.Load())
}

func hasMetric(rm metricdata.ResourceMetrics, name string) bool {
	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name == name {
				return true
			}
		}
	}
	return false
}
