package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupCleanupTestDB 创建 SQLite 内存数据库并迁移表。
func setupCleanupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// 创建 call_logs 表
	err = db.Exec(`
		CREATE TABLE IF NOT EXISTS call_logs (
			request_id TEXT PRIMARY KEY,
			ts DATETIME NOT NULL,
			status INTEGER,
			model TEXT,
			provider TEXT,
			group_name TEXT
		)
	`).Error
	require.NoError(t, err)

	// 创建 system_logs 表
	err = db.Exec(`
		CREATE TABLE IF NOT EXISTS system_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts DATETIME NOT NULL,
			level TEXT,
			component TEXT,
			message TEXT
		)
	`).Error
	require.NoError(t, err)

	return db
}

// setupCleanupTestRedis 创建 miniredis 实例。
func setupCleanupTestRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return rdb, mr
}

// insertTestCallLogs 插入测试数据到 call_logs。
func insertTestCallLogs(t *testing.T, db *gorm.DB, count int, daysAgo int) {
	t.Helper()
	ts := time.Now().AddDate(0, 0, -daysAgo)
	for i := 0; i < count; i++ {
		err := db.Exec("INSERT INTO call_logs (request_id, ts, status, model, provider, group_name) VALUES (?, ?, ?, ?, ?, ?)",
			fmt.Sprintf("req-%d-%d", daysAgo, i),
			ts,
			200,
			"gpt-4",
			"openai",
			"test-group",
		).Error
		require.NoError(t, err)
	}
}

func insertTestSystemLogs(t *testing.T, db *gorm.DB, count int, daysAgo int) {
	t.Helper()
	ts := time.Now().AddDate(0, 0, -daysAgo)
	for i := 0; i < count; i++ {
		err := db.Exec("INSERT INTO system_logs (ts, level, component, message) VALUES (?, ?, ?, ?)",
			ts,
			"info",
			"test",
			fmt.Sprintf("message-%d-%d", daysAgo, i),
		).Error
		require.NoError(t, err)
	}
}

func countCallLogs(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	err := db.Raw("SELECT COUNT(*) FROM call_logs").Scan(&count).Error
	require.NoError(t, err)
	return count
}

func countSystemLogs(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	err := db.Raw("SELECT COUNT(*) FROM system_logs").Scan(&count).Error
	require.NoError(t, err)
	return count
}

func TestOpsCleanupService_LeaderLock(t *testing.T) {
	db := setupCleanupTestDB(t)
	rdb, mr := setupCleanupTestRedis(t)
	defer mr.Close()

	svc := NewOpsCleanupService(db, rdb)
	svc.SetRetention(1, 1) // 1 day retention for quick test

	ctx := context.Background()

	// 第一次获取锁应该成功
	ok := svc.acquireLeaderLock(ctx)
	assert.True(t, ok, "first instance should acquire lock")

	// 第二次获取锁（另一个实例）应该失败
	svc2 := NewOpsCleanupService(db, rdb)
	svc2.instanceID = "other-instance"
	ok2 := svc2.acquireLeaderLock(ctx)
	assert.False(t, ok2, "second instance should not acquire lock")

	// 释放锁
	svc.releaseLeaderLock(ctx)

	// 释放后另一个实例应该能获取锁
	ok3 := svc2.acquireLeaderLock(ctx)
	assert.True(t, ok3, "second instance should acquire lock after release")
}

func TestOpsCleanupService_cleanupCallLogs(t *testing.T) {
	db := setupCleanupTestDB(t)
	rdb, mr := setupCleanupTestRedis(t)
	defer mr.Close()

	// 插入旧数据（40 天前）
	insertTestCallLogs(t, db, 5, 40)
	// 插入新数据（1 天前）
	insertTestCallLogs(t, db, 3, 1)

	assert.Equal(t, int64(8), countCallLogs(t, db))

	svc := NewOpsCleanupService(db, rdb)
	svc.SetRetention(30, 90) // 30 天保留

	svc.cleanupCallLogs(context.Background())

	// 只保留 1 天前的数据（3 条），40 天前的应该被清理
	assert.Equal(t, int64(3), countCallLogs(t, db))
}

func TestOpsCleanupService_cleanupSystemLogs(t *testing.T) {
	db := setupCleanupTestDB(t)
	rdb, mr := setupCleanupTestRedis(t)
	defer mr.Close()

	// 插入旧数据（100 天前）
	insertTestSystemLogs(t, db, 10, 100)
	// 插入新数据（2 天前）
	insertTestSystemLogs(t, db, 4, 2)

	assert.Equal(t, int64(14), countSystemLogs(t, db))

	svc := NewOpsCleanupService(db, rdb)
	svc.SetRetention(30, 90) // 90 天错误日志保留

	svc.cleanupSystemLogs(context.Background())

	// 只有 100 天前的数据应该被清理（>90 天），2 天前的保留
	assert.Equal(t, int64(4), countSystemLogs(t, db))
}

func TestOpsCleanupService_NoRedisSkipsLock(t *testing.T) {
	db := setupCleanupTestDB(t)

	// 无 Redis 时不应 panic
	svc := NewOpsCleanupService(db, nil)
	svc.SetRetention(1, 1)

	ctx := context.Background()

	// 无 Redis 时 acquireLeaderLock 返回 false
	ok := svc.acquireLeaderLock(ctx)
	assert.False(t, ok)

	// runCleanup 应跳过（不会清理）
	svc.runCleanup(ctx)
}

func TestOpsCleanupService_SetRetention(t *testing.T) {
	db := setupCleanupTestDB(t)
	rdb, mr := setupCleanupTestRedis(t)
	defer mr.Close()

	svc := NewOpsCleanupService(db, rdb)
	svc.SetRetention(15, 45)

	assert.Equal(t, 15, svc.callLogRetentionDays)
	assert.Equal(t, 45, svc.errorLogRetentionDays)
}

func TestOpsCleanupService_SetSchedule(t *testing.T) {
	db := setupCleanupTestDB(t)
	rdb, mr := setupCleanupTestRedis(t)
	defer mr.Close()

	svc := NewOpsCleanupService(db, rdb)
	svc.SetSchedule("0 0 * * *")

	assert.Equal(t, "0 0 * * *", svc.cronSchedule)
}

func TestOpsCleanupService_LoadRetentionFromSettings(t *testing.T) {
	db := setupCleanupTestDB(t)
	rdb, mr := setupCleanupTestRedis(t)
	defer mr.Close()

	svc := NewOpsCleanupService(db, rdb)
	svc.LoadRetentionFromSettings(`{"data_retention_days": 60, "error_log_retention_days": 180}`)

	assert.Equal(t, 60, svc.callLogRetentionDays)
	assert.Equal(t, 180, svc.errorLogRetentionDays)
}

func TestOpsCleanupService_LoadRetentionFromSettings_Empty(t *testing.T) {
	db := setupCleanupTestDB(t)
	rdb, mr := setupCleanupTestRedis(t)
	defer mr.Close()

	svc := NewOpsCleanupService(db, rdb)
	defaultCall := svc.callLogRetentionDays
	defaultError := svc.errorLogRetentionDays

	svc.LoadRetentionFromSettings("")

	assert.Equal(t, defaultCall, svc.callLogRetentionDays)
	assert.Equal(t, defaultError, svc.errorLogRetentionDays)
}

func TestOpsCleanupService_acquireLeaderLock_Release(t *testing.T) {
	db := setupCleanupTestDB(t)
	rdb, mr := setupCleanupTestRedis(t)
	defer mr.Close()

	svc := NewOpsCleanupService(db, rdb)
	ctx := context.Background()

	// 获取锁
	ok := svc.acquireLeaderLock(ctx)
	assert.True(t, ok)

	// 验证锁存在
	val, err := rdb.Get(ctx, opsCleanupLeaderKey).Result()
	assert.NoError(t, err)
	assert.Equal(t, svc.instanceID, val)

	// 释放锁
	svc.releaseLeaderLock(ctx)

	// 验证锁已释放
	_, err = rdb.Get(ctx, opsCleanupLeaderKey).Result()
	assert.ErrorIs(t, err, redis.Nil)
}

func TestOpsCleanupService_ExtendLock(t *testing.T) {
	db := setupCleanupTestDB(t)
	rdb, mr := setupCleanupTestRedis(t)
	defer mr.Close()

	svc := NewOpsCleanupService(db, rdb)
	ctx := context.Background()

	// 获取锁
	ok := svc.acquireLeaderLock(ctx)
	assert.True(t, ok)

	// 续期锁
	svc.extendLeaderLock(ctx)

	// 验证 TTL 已更新
	ttl, err := rdb.TTL(ctx, opsCleanupLeaderKey).Result()
	assert.NoError(t, err)
	// TTL 应该在 extend TTL 附近
	assert.True(t, ttl > 0, "TTL should be positive after extend")
}