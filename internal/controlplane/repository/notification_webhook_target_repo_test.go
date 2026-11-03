package repository

import (
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newWebhookTargetTestDB 内存 sqlite,手工建表。
// 不用 AutoMigrate:模型的 default:gen_random_uuid() 在 sqlite 下生成的 DDL 非法。
func newWebhookTargetTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec(`CREATE TABLE notification_webhook_targets (
		id text PRIMARY KEY,
		notification_type varchar(100) NOT NULL,
		name varchar(100) NOT NULL DEFAULT '',
		url text NOT NULL,
		secret text,
		enabled numeric NOT NULL DEFAULT 1,
		sort_order integer NOT NULL DEFAULT 0,
		created_at datetime,
		updated_at datetime
	)`).Error; err != nil {
		t.Fatalf("create table: %v", err)
	}
	return db
}

func seedTarget(t *testing.T, db *gorm.DB, tgt model.NotificationWebhookTarget) {
	t.Helper()
	// gorm default:true tag：Create 遇到 Enabled=false 零值会省略列（DB 默认 true），
	// 且会把默认值写回结构体（tgt.Enabled 变 true）。故须在 Create 前捕获期望值。
	wantEnabled := tgt.Enabled
	if err := db.Create(&tgt).Error; err != nil {
		t.Fatalf("seed target: %v", err)
	}
	if !wantEnabled {
		if err := db.Model(&model.NotificationWebhookTarget{}).
			Where("id = ?", tgt.ID).Update("enabled", false).Error; err != nil {
			t.Fatalf("disable target: %v", err)
		}
	}
}

func TestWebhookTargetRepo_Crud(t *testing.T) {
	db := newWebhookTargetTestDB(t)
	repo := NewNotificationWebhookTargetRepo(db)

	id := uuid.New()
	secret := "s3cret"
	created := model.NotificationWebhookTarget{
		ID: id, NotificationType: "budget_alert",
		Name: "ops-a", URL: "https://a.invalid/hook", Secret: &secret, Enabled: true, SortOrder: 1,
	}
	if err := repo.Create(&created); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := repo.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.URL != created.URL || got.Name != "ops-a" || got.SortOrder != 1 {
		t.Fatalf("get mismatch: %+v", got)
	}
	if got.Secret == nil || *got.Secret != secret {
		t.Fatalf("secret not persisted: %v", got.Secret)
	}

	got.Name = "ops-a-renamed"
	if err := repo.Update(got); err != nil {
		t.Fatalf("update: %v", err)
	}
	got2, err := repo.Get(id)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if got2.Name != "ops-a-renamed" {
		t.Fatalf("name after update: want ops-a-renamed, got %s", got2.Name)
	}

	if err := repo.Delete(id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.Get(id); err == nil {
		t.Fatal("get after delete should error")
	}
}

func TestWebhookTargetRepo_ListByType(t *testing.T) {
	db := newWebhookTargetTestDB(t)
	repo := NewNotificationWebhookTargetRepo(db)

	disabled := model.NotificationWebhookTarget{
		ID: uuid.New(), NotificationType: "budget_alert",
		Name: "off", URL: "https://off.invalid/hook", Enabled: false,
	}
	enabledA := model.NotificationWebhookTarget{
		ID: uuid.New(), NotificationType: "budget_alert",
		Name: "a", URL: "https://a.invalid/hook", Enabled: true, SortOrder: 0,
	}
	enabledB := model.NotificationWebhookTarget{
		ID: uuid.New(), NotificationType: "budget_alert",
		Name: "b", URL: "https://b.invalid/hook", Enabled: true, SortOrder: 1,
	}
	other := model.NotificationWebhookTarget{
		ID: uuid.New(), NotificationType: "key_activity",
		Name: "other", URL: "https://other.invalid/hook", Enabled: true,
	}
	for _, tgt := range []model.NotificationWebhookTarget{disabled, enabledA, enabledB, other} {
		seedTarget(t, db, tgt)
	}

	enabled, err := repo.ListEnabledByType("budget_alert")
	if err != nil {
		t.Fatalf("list enabled: %v", err)
	}
	if len(enabled) != 2 {
		t.Fatalf("enabled count = %d, want 2", len(enabled))
	}
	if enabled[0].ID != enabledA.ID || enabled[1].ID != enabledB.ID {
		t.Fatalf("order wrong: %+v", enabled)
	}

	all, err := repo.ListAllTargets()
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("all count = %d, want 4", len(all))
	}
}
