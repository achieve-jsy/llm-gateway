package repository_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openKeyRepo(t *testing.T) (*repository.KeyRepo, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	ddl := `CREATE TABLE api_keys (
		id TEXT PRIMARY KEY,
		key_hash TEXT NOT NULL,
		last_used_at DATETIME,
		updated_at DATETIME
	)`
	if err := db.Exec(ddl).Error; err != nil {
		t.Fatalf("create table: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return repository.NewKeyRepo(db), db
}

func lastUsed(t *testing.T, db *gorm.DB, hash string) sql.NullTime {
	t.Helper()
	var got sql.NullTime
	if err := db.Raw("SELECT last_used_at FROM api_keys WHERE key_hash = ?", hash).Scan(&got).Error; err != nil {
		t.Fatalf("query last_used_at: %v", err)
	}
	return got
}

// TouchLastUsed 写库必须节流：切流/冷缓存瞬间每条请求都同步写一次会拖慢控制面，
// 而网关对 verify 有 3s 硬超时，超时即被映射成 503 auth_service_unavailable。
func TestTouchLastUsedThrottlesRecentWrite(t *testing.T) {
	repo, db := openKeyRepo(t)
	const hash = "hash-throttle"
	recent := time.Now().Add(-5 * time.Second)
	if err := db.Exec("INSERT INTO api_keys (id, key_hash, last_used_at) VALUES (?,?,?)", "k1", hash, recent).Error; err != nil {
		t.Fatalf("insert: %v", err)
	}

	repo.TouchLastUsed(hash)

	got := lastUsed(t, db, hash)
	if !got.Valid {
		t.Fatalf("last_used_at 变成 NULL")
	}
	if !got.Time.Truncate(time.Second).Equal(recent.Truncate(time.Second)) {
		t.Fatalf("节流窗口内不应重复写入：%v 变为 %v", recent, got.Time)
	}
}

func TestTouchLastUsedWritesWhenStale(t *testing.T) {
	repo, db := openKeyRepo(t)
	const hash = "hash-stale"
	stale := time.Now().Add(-5 * time.Minute)
	if err := db.Exec("INSERT INTO api_keys (id, key_hash, last_used_at) VALUES (?,?,?)", "k2", hash, stale).Error; err != nil {
		t.Fatalf("insert: %v", err)
	}

	repo.TouchLastUsed(hash)

	got := lastUsed(t, db, hash)
	if !got.Valid || time.Since(got.Time) > time.Minute {
		t.Fatalf("超过节流窗口应更新 last_used_at，实际 %v", got.Time)
	}
}
