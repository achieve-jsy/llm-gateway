package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
)

// TestOpsDeliveryFailed_EndToEndWebhook 端到端验证 ops_delivery_failed 全链路：
// outbox 死信 → OpsAlertSender 入队 → RetryPending 出队 → notificationManager.Notify →
// dispatchWebhook → 真实 WebhookSender HTTP POST 到本地接收器。
// 不触达飞书，但走的是生产同款投递代码（webhookSender 留 nil → 构造真实 NewWebhookSender）。
func TestOpsDeliveryFailed_EndToEndWebhook(t *testing.T) {
	type feishuPayload struct {
		MsgType string `json:"msg_type"`
		Content struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	var mu sync.Mutex
	var received []feishuPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p feishuPayload
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		received = append(received, p)
		mu.Unlock()
		fmt.Fprint(w, `{"code":0}`)
	}))
	defer srv.Close()

	db := newNotifyTestDB(t)
	// 补建 outbox 表（DDL 与生产对齐，newNotifyTestDB 只建通知相关表）。
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
		t.Fatalf("create outbox table: %v", err)
	}

	url := srv.URL
	if err := db.Create(&model.NotificationChannelConfig{
		NotificationType: model.NotificationTypeOpsDeliveryFailed,
		Channel:          "webhook",
		Enabled:          true,
		WebhookURL:       &url,
	}).Error; err != nil {
		t.Fatalf("seed webhook config: %v", err)
	}

	// 生产装配：consumerNotifier(outbox) → OpsAlertSender → 观察者；观察者再连统一 notifier。
	mgr := &notificationManager{configRepo: repository.NewNotificationConfigRepo(db), db: db, logger: slog.Default()}
	obs := NewBudgetLifecycleNotificationObserver(db, nil, nil, mgr)
	obs.SetOpsAlertSender(NewOpsAlertSender(NewOutboxNotifier(db)))

	deadRow := model.NotificationOutbox{
		ID: uuid.New(), DedupeKey: "k:deadletter:1", UserID: uuid.Nil,
		Type: model.NotificationTypeBudgetAlert, Level: model.NotificationLevelInfo,
		Title: "预算告警", Content: "c", Status: "pending", Attempts: maxOutboxAttempts,
		NextAttempt: time.Now().UTC().Add(-time.Minute),
	}
	if err := db.Create(&deadRow).Error; err != nil {
		t.Fatalf("seed deadletter row: %v", err)
	}

	ctx := context.Background()
	// drain 1：死信候选被标记 deadletter，OpsAlertSender 入队 ops_delivery_failed。
	if err := obs.RetryPending(ctx, 100); err != nil {
		t.Fatalf("RetryPending (drain 1): %v", err)
	}
	// drain 2：ops_delivery_failed 出队投递，真实 HTTP POST 到本地接收器。
	if err := obs.RetryPending(ctx, 100); err != nil {
		t.Fatalf("RetryPending (drain 2): %v", err)
	}

	mu.Lock()
	bodies := append([]feishuPayload(nil), received...)
	mu.Unlock()
	if len(bodies) != 1 {
		t.Fatalf("expected 1 webhook delivery, got %d", len(bodies))
	}
	p := bodies[0]
	if p.MsgType != "text" {
		t.Errorf("msg_type = %q, want text", p.MsgType)
	}
	for _, want := range []string{"通知投递失败", "budget_alert", "预算告警", "死信队列", "重试次数"} {
		if !strings.Contains(p.Content.Text, want) {
			t.Errorf("webhook text should contain %q, got:\n%s", want, p.Content.Text)
		}
	}

	// 原死信行与 ops_delivery_failed 行最终都应落 delivered / deadletter。
	var dead model.NotificationOutbox
	if err := db.First(&dead, "id = ?", deadRow.ID).Error; err != nil {
		t.Fatal(err)
	}
	if dead.Status != "deadletter" {
		t.Errorf("deadletter row status = %q, want deadletter", dead.Status)
	}
	var deliveredCount int64
	if err := db.Model(&model.NotificationOutbox{}).
		Where("type = ? AND status = ?", model.NotificationTypeOpsDeliveryFailed, "delivered").
		Count(&deliveredCount).Error; err != nil {
		t.Fatal(err)
	}
	if deliveredCount != 1 {
		t.Errorf("ops_delivery_failed outbox rows delivered = %d, want 1", deliveredCount)
	}
}
