package service

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/example/llm-gateway/internal/controlplane/audit"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
)

func newAuditTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	// 用测试名作为内存库名，避免同进程内不同测试共享 cache=shared 导致数据串扰。
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	createTable := `CREATE TABLE IF NOT EXISTS audit_logs (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		user_name TEXT,
		action TEXT NOT NULL,
		resource_type TEXT,
		resource_id TEXT,
		resource_name TEXT,
		dept_id TEXT,
		dept_name TEXT,
		status_code INTEGER DEFAULT 200,
		client_ip TEXT,
		user_agent TEXT,
		details TEXT,
		created_at DATETIME NOT NULL
	)`
	if err := db.Exec(createTable).Error; err != nil {
		t.Fatalf("create table: %v", err)
	}
	return db
}

// TestAuditLogWriter_AsyncWriteAndFlush 验证异步批量写器能落库。
func TestAuditLogWriter_AsyncWriteAndFlush(t *testing.T) {
	db := newAuditTestDB(t)
	w := NewAuditLogWriterWithConfig(db, 64, 10, 30*time.Millisecond)
	w.Start()
	defer w.Stop()

	w.Write(&model.AuditLog{
		UserID:       uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Action:       "test.create",
		ResourceType: "test",
		CreatedAt:    time.Now(),
	})

	time.Sleep(150 * time.Millisecond)

	var logs []model.AuditLog
	if err := db.Find(&logs).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 flushed log, got %d", len(logs))
	}
	if w.WrittenCount() != 1 {
		t.Errorf("written count = %d, want 1", w.WrittenCount())
	}
}

// TestAuditLogWriter_DropWhenQueueFull 验证队列满时丢弃计数与 metric 调用不 panic。
func TestAuditLogWriter_DropWhenQueueFull(t *testing.T) {
	db := newAuditTestDB(t)
	// bufferSize=0 → 无缓冲 channel，且不 Start（无消费者），Write 必然走丢弃分支。
	w := NewAuditLogWriterWithConfig(db, 0, 10, time.Second)
	entry := &model.AuditLog{
		UserID:       uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Action:       "test.drop",
		ResourceType: "test",
		CreatedAt:    time.Now(),
	}
	w.Write(entry)
	if w.DroppedCount() != 1 {
		t.Fatalf("expected 1 dropped, got %d", w.DroppedCount())
	}
	if w.WrittenCount() != 0 {
		t.Fatalf("expected 0 written, got %d", w.WrittenCount())
	}
	if w.WriteFailures() != 0 {
		t.Fatalf("expected 0 write failures, got %d", w.WriteFailures())
	}
}

// TestAuditLogService_AsyncRecord 验证 service 走异步写路径仍能查询到。
func TestAuditLogService_AsyncRecord(t *testing.T) {
	db := newAuditTestDB(t)
	w := NewAuditLogWriterWithConfig(db, 64, 10, 30*time.Millisecond)
	w.Start()
	defer w.Stop()

	repo := repository.NewAuditLogRepo(db)
	svc := NewAuditLogService(repo, w)

	svc.Record(audit.Input{
		UserID:       uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		UserName:     "alice",
		Action:       audit.ActionProviderCreate,
		ResourceType: audit.ResourceProvider,
	})

	time.Sleep(150 * time.Millisecond)

	var uid = uuid.MustParse("22222222-2222-2222-2222-222222222222")
	_, total, err := svc.List(0, 10, &uid, "", "", "", nil, nil, nil, "", "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected 1 async-recorded log, got %d", total)
	}
}
