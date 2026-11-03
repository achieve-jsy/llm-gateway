package service

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// fakeGate 固定值的 ChannelGate mock。
type fakeGate struct {
	enabled bool
}

func (g *fakeGate) IsEnabled(_ context.Context, _, _ string) bool { return g.enabled }

// newNotifyTestDB 内存 sqlite，手工建通知相关三张表。
// 不用 AutoMigrate：模型的 default:gen_random_uuid() 在 sqlite 下生成的 DDL 非法。
func newNotifyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	stmts := []string{
		`CREATE TABLE notifications (
			id text PRIMARY KEY,
			user_id text NOT NULL,
			type varchar(32) NOT NULL,
			level varchar(16) NOT NULL DEFAULT 'info',
			title varchar(200) NOT NULL,
			content text NOT NULL,
			source varchar(32) NOT NULL DEFAULT 'direct',
			dedupe_key varchar(255),
			ref_type varchar(32) NOT NULL DEFAULT '',
			ref_id varchar(64) NOT NULL DEFAULT '',
			is_read numeric NOT NULL DEFAULT 0,
			read_at timestamptz,
			created_at datetime NOT NULL
		)`,
		`CREATE UNIQUE INDEX uniq_notifications_source_dedupe ON notifications (source, dedupe_key)`,
		`CREATE TABLE users (
			id text PRIMARY KEY,
			email text NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE notification_deliveries (
			id text PRIMARY KEY,
			dedupe_key varchar(255) NOT NULL,
			channel varchar(32) NOT NULL,
			status varchar(16) NOT NULL DEFAULT 'pending',
			attempts integer NOT NULL DEFAULT 0,
			last_error text NOT NULL DEFAULT '',
			delivered_at timestamptz,
			notification_type varchar(32) NOT NULL DEFAULT '',
			title varchar(200) NOT NULL DEFAULT '',
			content text NOT NULL DEFAULT '',
			user_id text,
			created_at datetime,
			updated_at datetime
		)`,
		`CREATE UNIQUE INDEX uniq_notification_delivery_channel ON notification_deliveries (dedupe_key, channel)`,
		`CREATE TABLE notification_channel_configs (
			notification_type varchar(100) NOT NULL,
			channel varchar(50) NOT NULL,
			enabled boolean NOT NULL DEFAULT 1,
			webhook_url text,
			webhook_secret text,
			email_recipients text,
			subject_template text,
			body_template text,
			created_at datetime,
			updated_at datetime,
			PRIMARY KEY (notification_type, channel)
		)`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("create table: %v\nSQL: %s", err, s)
		}
	}
	return db
}

func TestNotify_InappGateDisabled(t *testing.T) {
	db := newNotifyTestDB(t)
	mgr := &notificationManager{
		db:        db,
		inappRepo: repository.NewNotificationRepo(db),
		rulesGate: &fakeGate{enabled: false},
		logger:    slog.Default(),
	}
	n := &model.Notification{
		UserID: uuid.New(), Type: model.NotificationTypeBudgetAlert,
		Level: model.NotificationLevelInfo, Title: "标题", Content: "内容",
	}
	if err := mgr.Notify(context.Background(), n); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	var count int64
	if err := db.Model(&model.Notification{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("inapp gate disabled: expected 0 rows, got %d", count)
	}
}

func TestNotify_InappWritesAndDedupes(t *testing.T) {
	db := newNotifyTestDB(t)
	mgr := &notificationManager{
		db:        db,
		inappRepo: repository.NewNotificationRepo(db),
		rulesGate: &fakeGate{enabled: true},
		logger:    slog.Default(),
	}
	key := "inapp:dedupe:1"
	n := &model.Notification{
		UserID: uuid.New(), Type: model.NotificationTypeBudgetAlert,
		Level: model.NotificationLevelInfo, Title: "标题", Content: "内容",
		DedupeKey: key,
	}
	if err := mgr.Notify(context.Background(), n); err != nil {
		t.Fatalf("first Notify: %v", err)
	}
	if err := mgr.Notify(context.Background(), n); err != nil {
		t.Fatalf("second Notify (dedupe): %v", err)
	}
	var count int64
	if err := db.Model(&model.Notification{}).Where("dedupe_key = ?", key).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("inapp dedupe: expected 1 row, got %d", count)
	}
}

func TestNotify_WebhookDeliveredSkipsResend(t *testing.T) {
	db := newNotifyTestDB(t)
	cfgRepo := repository.NewNotificationConfigRepo(db)
	url := "https://example.invalid/hook"
	if err := db.Create(&model.NotificationChannelConfig{
		NotificationType: model.NotificationTypePlatformRiskRedisHealth,
		Channel:          "webhook",
		Enabled:          true,
		WebhookURL:       &url,
	}).Error; err != nil {
		t.Fatalf("seed config: %v", err)
	}
	key := "wh:dedupe:1"
	if err := db.Create(&model.NotificationDelivery{
		ID: uuid.New(), DedupeKey: key, Channel: "webhook", Status: "delivered",
	}).Error; err != nil {
		t.Fatalf("seed delivery: %v", err)
	}

	mgr := &notificationManager{configRepo: cfgRepo, db: db, logger: slog.Default()}
	n := &model.Notification{
		Type: model.NotificationTypePlatformRiskRedisHealth,
		Level: model.NotificationLevelCritical, Title: "标题", Content: "内容",
		DedupeKey: key,
	}
	if err := mgr.Notify(context.Background(), n); err != nil {
		t.Fatalf("Notify on delivered webhook key should return nil: %v", err)
	}
	var del model.NotificationDelivery
	if err := db.Where("dedupe_key = ? AND channel = ?", key, "webhook").First(&del).Error; err != nil {
		t.Fatal(err)
	}
	if del.Attempts != 0 {
		t.Fatalf("delivered webhook key should not be re-attempted, attempts=%d", del.Attempts)
	}
}

func TestNotify_EmailDeliveredSkipsResend(t *testing.T) {
	db := newNotifyTestDB(t)
	settings := newMockSettingsRepo()
	settings.data[SettingKeyEmailEnabled] = "true"
	settings.data[SettingKeySMTPHost] = "smtp.example.com"
	settings.data[SettingKeySMTPFrom] = "no-reply@example.com"
	cfgRepo := repository.NewNotificationConfigRepo(db)
	userID := uuid.New()
	if err := db.Exec(`INSERT INTO users (id, email) VALUES (?, ?)`, userID.String(), "ops@example.com").Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create(&model.NotificationChannelConfig{
		NotificationType: model.NotificationTypeExpansionApproved,
		Channel:          "email",
		Enabled:          true,
	}).Error; err != nil {
		t.Fatalf("seed config: %v", err)
	}
	key := "email:dedupe:1"
	if err := db.Create(&model.NotificationDelivery{
		ID: uuid.New(), DedupeKey: key, Channel: "email", Status: "delivered",
	}).Error; err != nil {
		t.Fatalf("seed delivery: %v", err)
	}

	mgr := &notificationManager{
		configRepo:   cfgRepo,
		settingsRepo: settings,
		db:           db,
		email:        NewEmailNotifier(repository.NewSettingsRepo(db)),
		logger:       slog.Default(),
	}
	n := &model.Notification{
		UserID: userID, Type: model.NotificationTypeExpansionApproved,
		Level: model.NotificationLevelInfo, Title: "审批通过", Content: "内容",
		DedupeKey: key,
	}
	if err := mgr.Notify(context.Background(), n); err != nil {
		t.Fatalf("Notify on delivered email key should return nil: %v", err)
	}
	var del model.NotificationDelivery
	if err := db.Where("dedupe_key = ? AND channel = ?", key, "email").First(&del).Error; err != nil {
		t.Fatal(err)
	}
	if del.Attempts != 0 {
		t.Fatalf("delivered email key should not be re-attempted, attempts=%d", del.Attempts)
	}
}

func TestSettleDelivery_PropagatesSendError(t *testing.T) {
	db := newNotifyTestDB(t)
	persisted := model.NotificationDelivery{
		ID: uuid.New(), DedupeKey: "k", Channel: "webhook", Status: "pending",
	}
	if err := db.Create(&persisted).Error; err != nil {
		t.Fatalf("seed delivery: %v", err)
	}
	sendErr := errors.New("smtp down")
	got := settleDelivery(context.Background(), db, persisted, sendErr)
	if got == nil {
		t.Fatal("settleDelivery must propagate sendErr so outbox retries")
	}
	var del model.NotificationDelivery
	if err := db.First(&del, "id = ?", persisted.ID).Error; err != nil {
		t.Fatal(err)
	}
	if del.Status != "failed" || del.LastError != sendErr.Error() || del.Attempts != 1 {
		t.Fatalf("expected failed/attempts=1/last_error, got status=%s attempts=%d last_error=%q",
			del.Status, del.Attempts, del.LastError)
	}
}
