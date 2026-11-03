package service

import (
	"context"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newOutboxTableDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec(`CREATE TABLE notification_outbox (
		id TEXT PRIMARY KEY, dedupe_key TEXT NOT NULL UNIQUE, user_id TEXT NOT NULL,
		type TEXT NOT NULL, level TEXT NOT NULL, title TEXT NOT NULL, content TEXT NOT NULL,
		ref_type TEXT NOT NULL DEFAULT '', ref_id TEXT NOT NULL DEFAULT '',
		template_data TEXT,
		status TEXT NOT NULL DEFAULT 'pending', attempts INTEGER NOT NULL DEFAULT 0,
		next_attempt DATETIME NOT NULL, last_error TEXT NOT NULL DEFAULT '',
		locked_at DATETIME, lease_owner TEXT NOT NULL DEFAULT '', lease_expires_at INTEGER NOT NULL DEFAULT 0,
		delivered_at DATETIME, created_at DATETIME, updated_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("migrate outbox: %v", err)
	}
	return db
}

func TestOutboxNotifier_DedupeKeyDedupes(t *testing.T) {
	db := newOutboxTableDB(t)
	n := &model.Notification{
		UserID: uuid.New(), Type: model.NotificationTypeBudgetAlert,
		Level: model.NotificationLevelInfo, Title: "标题", Content: "内容",
		DedupeKey: "k:1",
	}
	nf := NewOutboxNotifier(db)
	for range 2 {
		if err := nf.Notify(context.Background(), n); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}
	var total int64
	if err := db.Model(&model.NotificationOutbox{}).Count(&total).Error; err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("same dedupe_key: expected 1 outbox row, got %d", total)
	}
}

func TestOutboxNotifier_NoDedupeKeyEnqueuesEach(t *testing.T) {
	db := newOutboxTableDB(t)
	nf := NewOutboxNotifier(db)
	for i := 0; i < 3; i++ {
		n := &model.Notification{
			UserID: uuid.New(), Type: model.NotificationTypeBudgetAlert,
			Level: model.NotificationLevelInfo, Title: "标题", Content: "内容",
		}
		if err := nf.Notify(context.Background(), n); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}
	var total int64
	if err := db.Model(&model.NotificationOutbox{}).Count(&total).Error; err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Fatalf("no dedupe_key: expected 3 outbox rows, got %d", total)
	}
}
