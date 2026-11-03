package service

import (
	"context"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
)

type stubNotifier struct {
	got []*model.Notification
}

func (s *stubNotifier) Notify(_ context.Context, n *model.Notification) error {
	s.got = append(s.got, n)
	return nil
}

func TestOpsAlertSender_ChannelDown_ProviderHealth(t *testing.T) {
	stub := &stubNotifier{}
	sender := NewOpsAlertSender(stub)
	providerID := uuid.New()

	sender.NotifyChannelDown(context.Background(), "provider_health", "OpenAI", providerID, "", 0, 3, 3, "conn refused")

	if len(stub.got) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(stub.got))
	}
	n := stub.got[0]
	if n.Type != model.NotificationTypeOpsChannelDown {
		t.Errorf("type = %q, want %q", n.Type, model.NotificationTypeOpsChannelDown)
	}
	if n.UserID != uuid.Nil {
		t.Errorf("user_id should be uuid.Nil for system alert, got %v", n.UserID)
	}
	if n.Level != model.NotificationLevelCritical {
		t.Errorf("level = %q, want critical", n.Level)
	}
	wantPrefix := "ops:channel_down:provider_health:" + providerID.String() + ":"
	if !strings.HasPrefix(n.DedupeKey, wantPrefix) {
		t.Errorf("dedupe_key = %q, want prefix %q", n.DedupeKey, wantPrefix)
	}
	if n.TemplateData["Source"] != "provider_health" {
		t.Errorf("Source = %v, want provider_health", n.TemplateData["Source"])
	}
	if n.TemplateData["ProviderID"] != providerID.String() {
		t.Errorf("ProviderID = %v, want %v", n.TemplateData["ProviderID"], providerID.String())
	}
	if n.TemplateData["Failures"] != 3 || n.TemplateData["Threshold"] != 3 {
		t.Errorf("Failures/Threshold = %v/%v, want 3/3", n.TemplateData["Failures"], n.TemplateData["Threshold"])
	}
	if n.TemplateData["Error"] != "conn refused" {
		t.Errorf("Error = %v, want conn refused", n.TemplateData["Error"])
	}
	// 无 monitor：MonitorID 不应出现在数据中
	if _, ok := n.TemplateData["MonitorID"]; ok {
		t.Error("MonitorID should not be present when monitorID is 0")
	}
}

func TestOpsAlertSender_ChannelDown_ChannelMonitor(t *testing.T) {
	stub := &stubNotifier{}
	sender := NewOpsAlertSender(stub)

	sender.NotifyChannelDown(context.Background(), "channel_monitor", "anthropic", uuid.Nil, "anthropic-mon", 42, 2, 2, "http 401")

	if len(stub.got) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(stub.got))
	}
	n := stub.got[0]
	if n.Type != model.NotificationTypeOpsChannelDown {
		t.Errorf("type = %q, want ops_channel_down", n.Type)
	}
	wantPrefix := "ops:channel_down:channel_monitor:42:"
	if !strings.HasPrefix(n.DedupeKey, wantPrefix) {
		t.Errorf("dedupe_key = %q, want prefix %q", n.DedupeKey, wantPrefix)
	}
	if n.TemplateData["MonitorName"] != "anthropic-mon" {
		t.Errorf("MonitorName = %v, want anthropic-mon", n.TemplateData["MonitorName"])
	}
	if n.TemplateData["MonitorID"] != int64(42) {
		t.Errorf("MonitorID = %v, want 42", n.TemplateData["MonitorID"])
	}
	// 无 provider：ProviderID 不应出现在数据中
	if _, ok := n.TemplateData["ProviderID"]; ok {
		t.Error("ProviderID should not be present when provider is uuid.Nil")
	}
}

func TestOpsAlertSender_ChannelRecovered(t *testing.T) {
	stub := &stubNotifier{}
	sender := NewOpsAlertSender(stub)
	providerID := uuid.New()

	sender.NotifyChannelRecovered(context.Background(), "provider_health", "OpenAI", providerID, "", 0)

	if len(stub.got) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(stub.got))
	}
	n := stub.got[0]
	if n.Type != model.NotificationTypeOpsChannelRecovered {
		t.Errorf("type = %q, want ops_channel_recovered", n.Type)
	}
	if n.Level != model.NotificationLevelInfo {
		t.Errorf("level = %q, want info", n.Level)
	}
	wantPrefix := "ops:channel_recovered:provider_health:" + providerID.String() + ":"
	if !strings.HasPrefix(n.DedupeKey, wantPrefix) {
		t.Errorf("dedupe_key = %q, want prefix %q", n.DedupeKey, wantPrefix)
	}
}

func TestOpsAlertSender_DeliveryFailed(t *testing.T) {
	stub := &stubNotifier{}
	sender := NewOpsAlertSender(stub)

	sender.NotifyDeliveryFailed(context.Background(), "budget_alert", "预算告警", "max attempts exceeded (deadletter)", 5)

	if len(stub.got) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(stub.got))
	}
	n := stub.got[0]
	if n.Type != model.NotificationTypeOpsDeliveryFailed {
		t.Errorf("type = %q, want ops_delivery_failed", n.Type)
	}
	if n.UserID != uuid.Nil {
		t.Errorf("user_id should be uuid.Nil for system alert, got %v", n.UserID)
	}
	if n.Level != model.NotificationLevelCritical {
		t.Errorf("level = %q, want critical", n.Level)
	}
	wantPrefix := "ops:delivery_failed:budget_alert:"
	if !strings.HasPrefix(n.DedupeKey, wantPrefix) {
		t.Errorf("dedupe_key = %q, want prefix %q", n.DedupeKey, wantPrefix)
	}
	if n.TemplateData["Attempts"] != 5 {
		t.Errorf("Attempts = %v, want 5", n.TemplateData["Attempts"])
	}
	if n.TemplateData["Title"] != "预算告警" {
		t.Errorf("Title = %v, want 预算告警", n.TemplateData["Title"])
	}
}

// TestOpsAlertSender_SameHourDedupeKey 同小时两次发送 DedupeKey 相等（小时级冷却，
// outbox OnConflict(dedupe_key) 拦截重复；自循环递归同小时最多 1 层）。
func TestOpsAlertSender_SameHourDedupeKey(t *testing.T) {
	stub := &stubNotifier{}
	sender := NewOpsAlertSender(stub)

	sender.NotifyDeliveryFailed(context.Background(), "ops_delivery_failed", "投递失败", "deadletter", 5)
	sender.NotifyDeliveryFailed(context.Background(), "ops_delivery_failed", "投递失败", "deadletter", 5)

	if len(stub.got) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(stub.got))
	}
	if stub.got[0].DedupeKey != stub.got[1].DedupeKey {
		t.Errorf("same-hour dedupe keys should match, got %q vs %q", stub.got[0].DedupeKey, stub.got[1].DedupeKey)
	}
}

func TestOpsAlertSender_NilNotifierNoPanic(t *testing.T) {
	sender := NewOpsAlertSender(nil)
	sender.NotifyChannelDown(context.Background(), "provider_health", "X", uuid.New(), "", 0, 3, 3, "err")
	sender.NotifyChannelRecovered(context.Background(), "provider_health", "X", uuid.New(), "", 0)
	sender.NotifyDeliveryFailed(context.Background(), "budget_alert", "t", "r", 5)
}

func TestOpsAlertSender_NilReceiverNoPanic(t *testing.T) {
	var sender *OpsAlertSender
	sender.NotifyChannelDown(context.Background(), "provider_health", "X", uuid.New(), "", 0, 3, 3, "err")
	sender.NotifyChannelRecovered(context.Background(), "provider_health", "X", uuid.New(), "", 0)
	sender.NotifyDeliveryFailed(context.Background(), "budget_alert", "t", "r", 5)
}
