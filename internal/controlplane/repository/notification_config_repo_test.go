package repository

import (
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestNotificationConfigRepo_Upsert(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.NotificationChannelConfig{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewNotificationConfigRepo(db)

	// 插入新配置
	url := "https://example.com/webhook"
	secret := "secret123"
	cfg := model.NotificationChannelConfig{
		NotificationType: "budget_alert",
		Channel:          "webhook",
		Enabled:          true,
		WebhookURL:       &url,
		WebhookSecret:    &secret,
	}
	if err := repo.Upsert(cfg); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// 验证插入成功
	got, err := repo.Get("budget_alert", "webhook")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.NotificationType != "budget_alert" {
		t.Errorf("notification_type: want budget_alert, got %s", got.NotificationType)
	}
	if got.Channel != "webhook" {
		t.Errorf("channel: want webhook, got %s", got.Channel)
	}
	if *got.WebhookURL != url {
		t.Errorf("webhook_url: want %s, got %s", url, *got.WebhookURL)
	}

	// 更新配置（upsert）
	newURL := "https://example.com/webhook2"
	cfg.WebhookURL = &newURL
	cfg.Enabled = false
	if err := repo.Upsert(cfg); err != nil {
		t.Fatalf("upsert update: %v", err)
	}

	// 验证更新成功
	got2, err := repo.Get("budget_alert", "webhook")
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if *got2.WebhookURL != newURL {
		t.Errorf("webhook_url after update: want %s, got %s", newURL, *got2.WebhookURL)
	}
	if got2.Enabled {
		t.Errorf("enabled after update: want false, got true")
	}
}

func TestNotificationConfigRepo_ListByType(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.NotificationChannelConfig{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewNotificationConfigRepo(db)

	// 插入两条配置（同一类型，不同渠道）
	url := "https://example.com/webhook"
	cfg1 := model.NotificationChannelConfig{
		NotificationType: "budget_alert",
		Channel:          "webhook",
		Enabled:          true,
		WebhookURL:       &url,
	}
	cfg2 := model.NotificationChannelConfig{
		NotificationType: "budget_alert",
		Channel:          "email",
		Enabled:          true,
	}
	if err := repo.Upsert(cfg1); err != nil {
		t.Fatalf("upsert cfg1: %v", err)
	}
	if err := repo.Upsert(cfg2); err != nil {
		t.Fatalf("upsert cfg2: %v", err)
	}

	// 插入另一类型的配置
	cfg3 := model.NotificationChannelConfig{
		NotificationType: "key_activity",
		Channel:          "webhook",
		Enabled:          true,
	}
	if err := repo.Upsert(cfg3); err != nil {
		t.Fatalf("upsert cfg3: %v", err)
	}

	// 按类型查询
	list, err := repo.ListByType("budget_alert")
	if err != nil {
		t.Fatalf("list by type: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("list length: want 2, got %d", len(list))
	}

	// 验证排序（按 channel）
	if list[0].Channel != "email" || list[1].Channel != "webhook" {
		t.Errorf("list order: want email then webhook, got %s and %s", list[0].Channel, list[1].Channel)
	}
}

func TestNotificationConfigRepo_ListAll(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.NotificationChannelConfig{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewNotificationConfigRepo(db)

	// 插入多条配置
	cfgs := []model.NotificationChannelConfig{
		{NotificationType: "budget_alert", Channel: "webhook", Enabled: true},
		{NotificationType: "budget_alert", Channel: "email", Enabled: true},
		{NotificationType: "key_activity", Channel: "webhook", Enabled: true},
	}
	for _, cfg := range cfgs {
		if err := repo.Upsert(cfg); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}

	// 查询全部
	list, err := repo.ListAll()
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("list length: want 3, got %d", len(list))
	}
}

func TestNotificationConfigRepo_Delete(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.NotificationChannelConfig{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewNotificationConfigRepo(db)

	// 插入配置
	url := "https://example.com/webhook"
	cfg := model.NotificationChannelConfig{
		NotificationType: "budget_alert",
		Channel:          "webhook",
		Enabled:          true,
		WebhookURL:       &url,
	}
	if err := repo.Upsert(cfg); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// 删除
	if err := repo.Delete("budget_alert", "webhook"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// 验证删除成功
	_, err = repo.Get("budget_alert", "webhook")
	if err == nil {
		t.Errorf("get after delete: want error, got nil")
	}
}

func TestNotificationConfigRepo_GetNotFound(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.NotificationChannelConfig{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewNotificationConfigRepo(db)

	// 查询不存在的配置
	_, err = repo.Get("nonexistent", "webhook")
	if err == nil {
		t.Errorf("get nonexistent: want error, got nil")
	}
}
