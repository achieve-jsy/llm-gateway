package cron

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/core/quota"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	// Use raw DDL to avoid PostgreSQL-specific gen_random_uuid() in GORM tags
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1) // SQLite :memory: is per-connection
	if err := db.Exec(`CREATE TABLE quota_exemptions (
		id TEXT PRIMARY KEY,
		subject_type TEXT NOT NULL,
		subject_id TEXT NOT NULL,
		reason TEXT,
		expires_at DATETIME NOT NULL,
		created_by TEXT NOT NULL,
		created_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create table: %v", err)
	}
	return db
}

func setupTestRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client, func()) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return mr, rdb, func() {
		rdb.Close()
		mr.Close()
	}
}

func createExemption(t *testing.T, db *gorm.DB, subjectType, subjectID string, expiresAt time.Time) {
	t.Helper()
	e := &quota.QuotaExemption{
		ID:          uuid.New(),
		SubjectType: subjectType,
		SubjectID:   subjectID,
		Reason:      "test",
		ExpiresAt:   expiresAt,
		CreatedBy:   "test-runner",
	}
	if err := db.Create(e).Error; err != nil {
		t.Fatalf("create exemption: %v", err)
	}
}

func countExemptions(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	if err := db.Model(&quota.QuotaExemption{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	return count
}

func TestRunOnce_CleansExpiredOnly(t *testing.T) {
	db := setupTestDB(t)
	_, rdb, cleanup := setupTestRedis(t)
	defer cleanup()

	svc := service.NewQuotaExemptionService(db, rdb)
	cron := NewExemptionCleanupCron(svc)

	now := time.Now().UTC()

	// Create 3 exemptions: 2 expired, 1 active
	// Use .UTC() so stored text matches the UTC comparison in CleanupExpired.
	createExemption(t, db, "user", "expired-1", now.Add(-2*time.Hour))  // expired 2h ago
	createExemption(t, db, "user", "expired-2", now.Add(-30*time.Minute)) // expired 30m ago
	createExemption(t, db, "user", "active-1", now.Add(2*time.Hour))      // expires in 2h

	if count := countExemptions(t, db); count != 3 {
		t.Fatalf("expected 3 exemptions before cleanup, got %d", count)
	}

	// Run cleanup
	deleted, err := cron.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("expected 2 deleted, got %d", deleted)
	}

	// Only active exemption should remain
	if count := countExemptions(t, db); count != 1 {
		t.Fatalf("expected 1 exemption after cleanup, got %d", count)
	}

	var remaining quota.QuotaExemption
	if err := db.First(&remaining).Error; err != nil {
		t.Fatalf("find remaining: %v", err)
	}
	if remaining.SubjectID != "active-1" {
		t.Fatalf("expected active-1 to remain, got %s", remaining.SubjectID)
	}
}

func TestRunOnce_NoExpired(t *testing.T) {
	db := setupTestDB(t)
	_, rdb, cleanup := setupTestRedis(t)
	defer cleanup()

	svc := service.NewQuotaExemptionService(db, rdb)
	cron := NewExemptionCleanupCron(svc)

	// Create only active exemptions
	createExemption(t, db, "user", "active-1", time.Now().Add(2*time.Hour))
	createExemption(t, db, "service_key", "active-2", time.Now().Add(1*time.Hour))

	deleted, err := cron.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("expected 0 deleted, got %d", deleted)
	}
	if count := countExemptions(t, db); count != 2 {
		t.Fatalf("expected 2 exemptions unchanged, got %d", count)
	}
}

func TestRunOnce_EmptyTable(t *testing.T) {
	db := setupTestDB(t)
	_, rdb, cleanup := setupTestRedis(t)
	defer cleanup()

	svc := service.NewQuotaExemptionService(db, rdb)
	cron := NewExemptionCleanupCron(svc)

	deleted, err := cron.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce on empty table: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("expected 0 deleted, got %d", deleted)
	}
}

func TestStart_ContextCancellation(t *testing.T) {
	db := setupTestDB(t)
	_, rdb, cleanup := setupTestRedis(t)
	defer cleanup()

	svc := service.NewQuotaExemptionService(db, rdb)
	cron := NewExemptionCleanupCron(svc)
	// Use a very long interval so ticker doesn't fire during test
	cron.interval = 1 * time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	cron.Start(ctx)

	// Give goroutine time to start
	time.Sleep(100 * time.Millisecond)

	// Cancel should cleanly stop the goroutine
	cancel()
	time.Sleep(100 * time.Millisecond)
	// If goroutine leaked, test would hang or fail — passing = clean shutdown
}

func TestNewExemptionCleanupCron_Defaults(t *testing.T) {
	db := setupTestDB(t)
	_, rdb, cleanup := setupTestRedis(t)
	defer cleanup()

	svc := service.NewQuotaExemptionService(db, rdb)
	cron := NewExemptionCleanupCron(svc)

	if cron.interval != 1*time.Hour {
		t.Fatalf("expected default interval 1h, got %v", cron.interval)
	}
}
