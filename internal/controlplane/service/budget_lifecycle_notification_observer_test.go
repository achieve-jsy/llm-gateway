package service

import (
	"context"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
)

// TestRetryPending_DeadletterTriggersOpsAlert 投递重试耗尽转 deadletter 时触发
// ops_delivery_failed 运维告警；正常投递行不触发。
func TestRetryPending_DeadletterTriggersOpsAlert(t *testing.T) {
	db := newOutboxTableDB(t)
	notifStub := &stubNotifier{}
	obs := NewBudgetLifecycleNotificationObserver(db, nil, nil, notifStub)
	opsStub := &stubNotifier{}
	obs.SetOpsAlertSender(NewOpsAlertSender(opsStub))

	now := time.Now().UTC().Add(-time.Minute)
	deadRow := model.NotificationOutbox{
		ID: uuid.New(), DedupeKey: "k:dead", UserID: uuid.Nil,
		Type: model.NotificationTypeBudgetAlert, Level: model.NotificationLevelInfo,
		Title: "预算告警", Content: "c", Status: "pending", Attempts: maxOutboxAttempts,
		NextAttempt: now,
	}
	okRow := model.NotificationOutbox{
		ID: uuid.New(), DedupeKey: "k:ok", UserID: uuid.Nil,
		Type: model.NotificationTypeBudgetAlert, Level: model.NotificationLevelInfo,
		Title: "预算告警", Content: "c", Status: "pending", Attempts: 0,
		NextAttempt: now,
	}
	for _, r := range []model.NotificationOutbox{deadRow, okRow} {
		if err := db.Create(&r).Error; err != nil {
			t.Fatalf("seed outbox row: %v", err)
		}
	}

	if err := obs.RetryPending(context.Background(), 100); err != nil {
		t.Fatalf("RetryPending: %v", err)
	}

	// deadletter 行 → status deadletter + ops 告警
	var dead model.NotificationOutbox
	if err := db.First(&dead, "id = ?", deadRow.ID).Error; err != nil {
		t.Fatal(err)
	}
	if dead.Status != "deadletter" {
		t.Errorf("deadletter row status = %q, want deadletter", dead.Status)
	}
	if len(opsStub.got) != 1 {
		t.Fatalf("expected 1 ops alert, got %d", len(opsStub.got))
	}
	n := opsStub.got[0]
	if n.Type != model.NotificationTypeOpsDeliveryFailed {
		t.Errorf("ops alert type = %q, want ops_delivery_failed", n.Type)
	}
	if n.TemplateData["Type"] != model.NotificationTypeBudgetAlert {
		t.Errorf("Type = %v, want budget_alert", n.TemplateData["Type"])
	}
	if n.TemplateData["Attempts"] != maxOutboxAttempts {
		t.Errorf("Attempts = %v, want %d", n.TemplateData["Attempts"], maxOutboxAttempts)
	}

	// 正常行 → delivered + 无 ops 告警
	var ok model.NotificationOutbox
	if err := db.First(&ok, "id = ?", okRow.ID).Error; err != nil {
		t.Fatal(err)
	}
	if ok.Status != "delivered" {
		t.Errorf("normal row status = %q, want delivered", ok.Status)
	}
}

// TestRetryPending_DeadletterSameHourDedupe 同小时多条死信按类型聚合：两次触发产生的
// ops_delivery_failed DedupeKey 相等（outbox OnConflict(dedupe_key) 拦截递归自循环）。
func TestRetryPending_DeadletterSameHourDedupe(t *testing.T) {
	db := newOutboxTableDB(t)
	opsStub := &stubNotifier{}
	obs := NewBudgetLifecycleNotificationObserver(db, nil, nil, &stubNotifier{})
	obs.SetOpsAlertSender(NewOpsAlertSender(opsStub))

	now := time.Now().UTC().Add(-time.Minute)
	for i := 0; i < 2; i++ {
		row := model.NotificationOutbox{
			ID: uuid.New(), DedupeKey: "k:dup:" + uuid.NewString(), UserID: uuid.Nil,
			Type: model.NotificationTypeExpansionApproved, Level: model.NotificationLevelInfo,
			Title: "扩容审批", Content: "c", Status: "pending", Attempts: maxOutboxAttempts,
			NextAttempt: now,
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("seed outbox row: %v", err)
		}
	}

	if err := obs.RetryPending(context.Background(), 100); err != nil {
		t.Fatalf("RetryPending: %v", err)
	}
	if len(opsStub.got) != 2 {
		t.Fatalf("expected 2 ops alert calls, got %d", len(opsStub.got))
	}
	if opsStub.got[0].DedupeKey != opsStub.got[1].DedupeKey {
		t.Errorf("same-hour dedupe keys should match, got %q vs %q", opsStub.got[0].DedupeKey, opsStub.got[1].DedupeKey)
	}
}

func TestRetryPending_NoDeadletterNoAlert(t *testing.T) {
	db := newOutboxTableDB(t)
	opsStub := &stubNotifier{}
	obs := NewBudgetLifecycleNotificationObserver(db, nil, nil, &stubNotifier{})
	obs.SetOpsAlertSender(NewOpsAlertSender(opsStub))

	row := model.NotificationOutbox{
		ID: uuid.New(), DedupeKey: "k:under", UserID: uuid.Nil,
		Type: model.NotificationTypeBudgetAlert, Level: model.NotificationLevelInfo,
		Title: "预算告警", Content: "c", Status: "pending", Attempts: 2,
		NextAttempt: time.Now().UTC().Add(-time.Minute),
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("seed outbox row: %v", err)
	}

	if err := obs.RetryPending(context.Background(), 100); err != nil {
		t.Fatalf("RetryPending: %v", err)
	}
	if len(opsStub.got) != 0 {
		t.Errorf("expected no ops alert for under-threshold row, got %d", len(opsStub.got))
	}
}
