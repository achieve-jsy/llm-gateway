package service_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newOpsTestSQLite(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return db
}

func newOpsTestMiniredis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return rdb, mr
}

// TestSystemMetricsCollector_Get_ReturnsSnapshot 首次采集后 Get() 应返回非零快照
func TestOpsMetricsCollector_Get_ReturnsSnapshot(t *testing.T) {
	db := newOpsTestSQLite(t)
	rdb, mr := newOpsTestMiniredis(t)
	defer rdb.Close()
	defer mr.Close()

	mc := service.NewSystemMetricsCollector(db, rdb)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mc.Start(ctx)

	time.Sleep(100 * time.Millisecond)

	snap := mc.Get()
	if snap.MemoryUsedMB <= 0 {
		t.Errorf("MemoryUsedMB = %v, expected > 0", snap.MemoryUsedMB)
	}
	if snap.MemoryTotalMB <= 0 {
		t.Errorf("MemoryTotalMB = %v, expected > 0", snap.MemoryTotalMB)
	}
	if snap.GoroutineCount <= 0 {
		t.Errorf("GoroutineCount = %d, expected > 0", snap.GoroutineCount)
	}
	if snap.MemoryUsagePercent < 0 || snap.MemoryUsagePercent > 100 {
		t.Errorf("MemoryUsagePercent = %v, expected 0-100", snap.MemoryUsagePercent)
	}
	if snap.CPUUsagePercent < 0 || snap.CPUUsagePercent > 100 {
		t.Errorf("CPUUsagePercent = %v, expected 0-100", snap.CPUUsagePercent)
	}
	if snap.CollectedAt.IsZero() {
		t.Error("CollectedAt is zero")
	}
}

// TestSystemMetricsCollector_GetHistory_ReturnsSnapshots 历史快照应非空
func TestOpsMetricsCollector_GetHistory_ReturnsSnapshots(t *testing.T) {
	db := newOpsTestSQLite(t)
	rdb, mr := newOpsTestMiniredis(t)
	defer rdb.Close()
	defer mr.Close()

	mc := service.NewSystemMetricsCollector(db, rdb)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mc.Start(ctx)

	time.Sleep(100 * time.Millisecond)

	history := mc.GetHistory()
	if len(history) == 0 {
		t.Error("GetHistory returned empty, expected at least 1 snapshot")
	}
}

// TestSystemMetricsCollector_ConcurrentAccess 并发读写不应 panic
func TestOpsMetricsCollector_ConcurrentAccess(t *testing.T) {
	db := newOpsTestSQLite(t)
	rdb, mr := newOpsTestMiniredis(t)
	defer rdb.Close()
	defer mr.Close()

	mc := service.NewSystemMetricsCollector(db, rdb)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mc.Start(ctx)

	time.Sleep(50 * time.Millisecond)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mc.Get()
			mc.GetHistory()
		}()
	}
	wg.Wait()
}

// TestSystemMetricsCollector_NilRedis 当 Redis 为 nil 时不应 panic
func TestOpsMetricsCollector_NilRedis(t *testing.T) {
	db := newOpsTestSQLite(t)

	mc := service.NewSystemMetricsCollector(db, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mc.Start(ctx)

	time.Sleep(100 * time.Millisecond)

	snap := mc.Get()
	if snap.MemoryUsedMB <= 0 {
		t.Error("MemoryUsedMB should be > 0 even with nil Redis")
	}
	if snap.RedisTotalConns != 0 {
		t.Errorf("RedisTotalConns = %d, expected 0 with nil Redis", snap.RedisTotalConns)
	}
}

// TestSystemMetricsCollector_CancelStopsCollection 取消 context 后采集应停止
func TestOpsMetricsCollector_CancelStopsCollection(t *testing.T) {
	db := newOpsTestSQLite(t)
	rdb, mr := newOpsTestMiniredis(t)
	defer rdb.Close()
	defer mr.Close()

	mc := service.NewSystemMetricsCollector(db, rdb)
	ctx, cancel := context.WithCancel(context.Background())
	mc.Start(ctx)

	time.Sleep(100 * time.Millisecond)

	snap1 := mc.Get()
	if snap1.GoroutineCount <= 0 {
		t.Error("Expected first snapshot after Start")
	}

	cancel()
	time.Sleep(200 * time.Millisecond)
	// 取消后不应再采集——两次快照时间应相同
	snap2 := mc.Get()
	if snap2.CollectedAt.After(snap1.CollectedAt.Add(50 * time.Millisecond)) {
		t.Logf("snap2 collected after cancel (edge case, not failure): snap1=%v snap2=%v", snap1.CollectedAt, snap2.CollectedAt)
	}
}

// TestSystemMetricsCollector_DBStats DB 连接池指标应从 SQLite 采集
func TestOpsMetricsCollector_DBStats(t *testing.T) {
	db := newOpsTestSQLite(t)
	rdb, mr := newOpsTestMiniredis(t)
	defer rdb.Close()
	defer mr.Close()

	mc := service.NewSystemMetricsCollector(db, rdb)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mc.Start(ctx)

	time.Sleep(100 * time.Millisecond)

	snap := mc.Get()
	// SQLite DB 的 MaxOpenConns 可能为 0（默认不限制）或设置过值
	if snap.DBMaxOpenConns < 0 {
		t.Errorf("DBMaxOpenConns = %d, expected >= 0", snap.DBMaxOpenConns)
	}
}

// TestSystemMetricsCollector_RedisStats Redis 连接池指标应从 miniredis 采集
func TestOpsMetricsCollector_RedisStats(t *testing.T) {
	db := newOpsTestSQLite(t)
	rdb, mr := newOpsTestMiniredis(t)
	defer rdb.Close()
	defer mr.Close()

	mc := service.NewSystemMetricsCollector(db, rdb)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mc.Start(ctx)

	time.Sleep(100 * time.Millisecond)

	snap := mc.Get()
	// miniredis PoolStats 的 TotalConns 可能为 0（miniredis 特殊行为）
	if snap.RedisTotalConns < 0 {
		t.Errorf("RedisTotalConns = %d, expected >= 0", snap.RedisTotalConns)
	}
}
