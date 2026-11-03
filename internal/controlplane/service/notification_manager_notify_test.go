package service

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/audit"
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
			read_at datetime,
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
			delivered_at datetime,
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
		`CREATE TABLE notification_webhook_targets (
			id text PRIMARY KEY,
			notification_type varchar(100) NOT NULL,
			name varchar(100) NOT NULL DEFAULT '',
			url text NOT NULL,
			secret text,
			enabled numeric NOT NULL DEFAULT 1,
			sort_order integer NOT NULL DEFAULT 0,
			created_at datetime,
			updated_at datetime
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

// captureRecorder 捕获 Record 输入的 fake audit.Recorder。
type captureRecorder struct {
	inputs []audit.Input
}

func (r *captureRecorder) Record(in audit.Input) { r.inputs = append(r.inputs, in) }

// fakeWebhookSender 记录 SendText 调用次数的 fake webhookSenderIface。
type fakeWebhookSender struct {
	mu      sync.Mutex
	calls   int
	lastErr error
}

func (f *fakeWebhookSender) SendText(_ context.Context, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.lastErr
}

func TestRecordWebhookAudit_SuccessAndFailure(t *testing.T) {
	rec := &captureRecorder{}
	mgr := &notificationManager{auditRecorder: rec, logger: slog.Default()}
	url := "https://hooks.example.com/secret-token"
	cfg := model.NotificationChannelConfig{
		NotificationType: model.NotificationTypePlatformRiskRedisHealth,
		Channel:          "webhook",
		WebhookURL:       &url,
	}
	targetID := uuid.New()

	mgr.recordWebhookAudit(cfg, true, "", "ops-bot", targetID)
	mgr.recordWebhookAudit(cfg, false, "send webhook: http 400", "ops-bot", targetID)

	if len(rec.inputs) != 2 {
		t.Fatalf("expected 2 audit records, got %d", len(rec.inputs))
	}
	ok := rec.inputs[0]
	if ok.Action != audit.ActionWebhookAlert || ok.ResourceType != audit.ResourceWebhook {
		t.Fatalf("success record: got action=%s resource=%s", ok.Action, ok.ResourceType)
	}
	if ok.StatusCode != 200 {
		t.Fatalf("success record: expected status 200, got %d", ok.StatusCode)
	}
	okDetails := ok.Details.(map[string]any)
	if okDetails["notification_type"] != model.NotificationTypePlatformRiskRedisHealth || okDetails["success"] != true {
		t.Fatalf("success record details: %v", okDetails)
	}
	if okDetails["target_name"] != "ops-bot" || okDetails["target_id"] != targetID.String() {
		t.Fatalf("success record target fields: %v", okDetails)
	}

	fail := rec.inputs[1]
	if fail.StatusCode != 500 || fail.Details.(map[string]any)["success"] != false {
		t.Fatalf("failure record: expected status 500 + success=false, got %+v", fail)
	}
	if fail.Details.(map[string]any)["error"] != "send webhook: http 400" {
		t.Fatalf("failure record: error not propagated, got %v", fail.Details)
	}
}

func TestRecordWebhookAudit_NilRecorderNoPanic(t *testing.T) {
	mgr := &notificationManager{logger: slog.Default()}
	mgr.recordWebhookAudit(model.NotificationChannelConfig{}, true, "", "", uuid.Nil)
	mgr.recordWebhookAudit(model.NotificationChannelConfig{}, false, "boom", "", uuid.Nil)
}

// TestDispatchWebhook_FanOutToTargets 验证一条通知 fan-out 到该类型下所有启用端点,
// 每个端点各落一条投递记录;禁用端点不发。
func TestDispatchWebhook_FanOutToTargets(t *testing.T) {
	db := newNotifyTestDB(t)
	cfgRepo := repository.NewNotificationConfigRepo(db)
	targetRepo := repository.NewNotificationWebhookTargetRepo(db)
	if err := db.Create(&model.NotificationChannelConfig{
		NotificationType: model.NotificationTypePlatformRiskRedisHealth,
		Channel:          "webhook",
		Enabled:          true,
	}).Error; err != nil {
		t.Fatalf("seed config: %v", err)
	}
	targetA := model.NotificationWebhookTarget{
		ID: uuid.New(), NotificationType: model.NotificationTypePlatformRiskRedisHealth,
		Name: "a", URL: "https://a.invalid/hook", Enabled: true, SortOrder: 0,
	}
	targetB := model.NotificationWebhookTarget{
		ID: uuid.New(), NotificationType: model.NotificationTypePlatformRiskRedisHealth,
		Name: "b", URL: "https://b.invalid/hook", Enabled: true, SortOrder: 1,
	}
	disabled := model.NotificationWebhookTarget{
		ID: uuid.New(), NotificationType: model.NotificationTypePlatformRiskRedisHealth,
		Name: "off", URL: "https://off.invalid/hook", Enabled: false,
	}
	for _, tgt := range []model.NotificationWebhookTarget{targetA, targetB, disabled} {
		if err := db.Create(&tgt).Error; err != nil {
			t.Fatalf("seed target: %v", err)
		}
	}
	// 禁用行不能靠 gorm Create(Enabled:false 会被 default:true tag 当成零值省略列 → DB 默认 true),
	// 需显式 UPDATE 落 false。生产走 raw SQL Upsert,不受此影响。
	if err := db.Model(&model.NotificationWebhookTarget{}).
		Where("id = ?", disabled.ID).Update("enabled", false).Error; err != nil {
		t.Fatalf("disable target: %v", err)
	}

	fake := &fakeWebhookSender{}
	mgr := &notificationManager{
		configRepo: cfgRepo, db: db, logger: slog.Default(),
		webhookTargetRepo: targetRepo, webhookSender: fake,
	}
	n := &model.Notification{
		Type: model.NotificationTypePlatformRiskRedisHealth,
		Level: model.NotificationLevelCritical, Title: "标题", Content: "内容",
		DedupeKey: "wh:fanout:1",
	}
	if err := mgr.Notify(context.Background(), n); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	fake.mu.Lock()
	calls := fake.calls
	fake.mu.Unlock()
	if calls != 2 {
		t.Fatalf("expected 2 sends (enabled targets), got %d", calls)
	}
	var count int64
	if err := db.Model(&model.NotificationDelivery{}).
		Where("channel = ? AND dedupe_key IN (?, ?)", "webhook",
			"wh:fanout:1#"+targetA.ID.String(), "wh:fanout:1#"+targetB.ID.String()).
		Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected 2 per-target delivery rows, got %d", count)
	}
	var offCount int64
	if err := db.Model(&model.NotificationDelivery{}).
		Where("channel = ? AND dedupe_key = ?", "webhook", "wh:fanout:1#"+disabled.ID.String()).
		Count(&offCount).Error; err != nil {
		t.Fatal(err)
	}
	if offCount != 0 {
		t.Fatalf("disabled target should have no delivery row, got %d", offCount)
	}
}

// TestDispatchWebhook_PerTargetDedupe 验证端点 A 已投递时只发端点 B(per-target 幂等)。
func TestDispatchWebhook_PerTargetDedupe(t *testing.T) {
	db := newNotifyTestDB(t)
	cfgRepo := repository.NewNotificationConfigRepo(db)
	targetRepo := repository.NewNotificationWebhookTargetRepo(db)
	if err := db.Create(&model.NotificationChannelConfig{
		NotificationType: model.NotificationTypePlatformRiskRedisHealth,
		Channel:          "webhook",
		Enabled:          true,
	}).Error; err != nil {
		t.Fatalf("seed config: %v", err)
	}
	targetA := model.NotificationWebhookTarget{
		ID: uuid.New(), NotificationType: model.NotificationTypePlatformRiskRedisHealth,
		Name: "a", URL: "https://a.invalid/hook", Enabled: true, SortOrder: 0,
	}
	targetB := model.NotificationWebhookTarget{
		ID: uuid.New(), NotificationType: model.NotificationTypePlatformRiskRedisHealth,
		Name: "b", URL: "https://b.invalid/hook", Enabled: true, SortOrder: 1,
	}
	for _, tgt := range []model.NotificationWebhookTarget{targetA, targetB} {
		if err := db.Create(&tgt).Error; err != nil {
			t.Fatalf("seed target: %v", err)
		}
	}
	key := "wh:per-target:1"
	if err := db.Create(&model.NotificationDelivery{
		ID: uuid.New(), DedupeKey: key + "#" + targetA.ID.String(),
		Channel: "webhook", Status: "delivered",
	}).Error; err != nil {
		t.Fatalf("seed target A delivery: %v", err)
	}

	fake := &fakeWebhookSender{}
	mgr := &notificationManager{
		configRepo: cfgRepo, db: db, logger: slog.Default(),
		webhookTargetRepo: targetRepo, webhookSender: fake,
	}
	n := &model.Notification{
		Type: model.NotificationTypePlatformRiskRedisHealth,
		Level: model.NotificationLevelCritical, Title: "标题", Content: "内容",
		DedupeKey: key,
	}
	if err := mgr.Notify(context.Background(), n); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	fake.mu.Lock()
	calls := fake.calls
	fake.mu.Unlock()
	if calls != 1 {
		t.Fatalf("expected 1 send (only target B), got %d", calls)
	}
	var delA model.NotificationDelivery
	if err := db.Where("dedupe_key = ?", key+"#"+targetA.ID.String()).First(&delA).Error; err != nil {
		t.Fatal(err)
	}
	if delA.Attempts != 0 {
		t.Fatalf("delivered target A should not be re-attempted, attempts=%d", delA.Attempts)
	}
	var delB model.NotificationDelivery
	if err := db.Where("dedupe_key = ?", key+"#"+targetB.ID.String()).First(&delB).Error; err != nil {
		t.Fatal(err)
	}
	if delB.Status != "delivered" {
		t.Fatalf("target B should be delivered, got %s", delB.Status)
	}
}

// TestDispatchWebhook_LegacyFallback 验证无端点子表时回退 config 行 URL(单飞书配置,裸去重 key)。
func TestDispatchWebhook_LegacyFallback(t *testing.T) {
	db := newNotifyTestDB(t)
	cfgRepo := repository.NewNotificationConfigRepo(db)
	url := "https://legacy.invalid/hook"
	if err := db.Create(&model.NotificationChannelConfig{
		NotificationType: model.NotificationTypePlatformRiskRedisHealth,
		Channel:          "webhook",
		Enabled:          true,
		WebhookURL:       &url,
	}).Error; err != nil {
		t.Fatalf("seed config: %v", err)
	}

	fake := &fakeWebhookSender{}
	mgr := &notificationManager{
		configRepo: cfgRepo, db: db, logger: slog.Default(),
		webhookSender: fake,
	}
	n := &model.Notification{
		Type: model.NotificationTypePlatformRiskRedisHealth,
		Level: model.NotificationLevelCritical, Title: "标题", Content: "内容",
		DedupeKey: "wh:legacy:1",
	}
	if err := mgr.Notify(context.Background(), n); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	fake.mu.Lock()
	calls := fake.calls
	fake.mu.Unlock()
	if calls != 1 {
		t.Fatalf("expected 1 send (legacy dest), got %d", calls)
	}
	var del model.NotificationDelivery
	if err := db.Where("dedupe_key = ? AND channel = ?", "wh:legacy:1", "webhook").First(&del).Error; err != nil {
		t.Fatal(err)
	}
	if del.Status != "delivered" {
		t.Fatalf("legacy dest should be delivered, got %s", del.Status)
	}
}

// TestDispatchWebhook_DisabledConfigSkips 验证 webhook 总开关关闭时即使有端点也不发。
func TestDispatchWebhook_DisabledConfigSkips(t *testing.T) {
	db := newNotifyTestDB(t)
	cfgRepo := repository.NewNotificationConfigRepo(db)
	targetRepo := repository.NewNotificationWebhookTargetRepo(db)
	if err := db.Create(&model.NotificationChannelConfig{
		NotificationType: model.NotificationTypePlatformRiskRedisHealth,
		Channel:          "webhook",
		Enabled:          false,
	}).Error; err != nil {
		t.Fatalf("seed config: %v", err)
	}
	// 同 FanOut 测试:Enabled:false 经 gorm Create 会被 default:true 省略列,需显式 UPDATE。
	if err := db.Model(&model.NotificationChannelConfig{}).
		Where("notification_type = ? AND channel = ?", model.NotificationTypePlatformRiskRedisHealth, "webhook").
		Update("enabled", false).Error; err != nil {
		t.Fatalf("disable config: %v", err)
	}
	if err := db.Create(&model.NotificationWebhookTarget{
		ID: uuid.New(), NotificationType: model.NotificationTypePlatformRiskRedisHealth,
		Name: "a", URL: "https://a.invalid/hook", Enabled: true,
	}).Error; err != nil {
		t.Fatalf("seed target: %v", err)
	}

	fake := &fakeWebhookSender{}
	mgr := &notificationManager{
		configRepo: cfgRepo, db: db, logger: slog.Default(),
		webhookTargetRepo: targetRepo, webhookSender: fake,
	}
	n := &model.Notification{
		Type: model.NotificationTypePlatformRiskRedisHealth,
		Level: model.NotificationLevelCritical, Title: "标题", Content: "内容",
		DedupeKey: "wh:off:1",
	}
	if err := mgr.Notify(context.Background(), n); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	fake.mu.Lock()
	calls := fake.calls
	fake.mu.Unlock()
	if calls != 0 {
		t.Fatalf("disabled config should not send, got %d calls", calls)
	}
}
