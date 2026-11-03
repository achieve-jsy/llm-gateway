package service

import (
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
)

func TestMaybeAlertProviderDown_AlertFires(t *testing.T) {
	stub := &stubNotifier{}
	s := &HealthCheckService{}
	s.SetOpsAlertSender(NewOpsAlertSender(stub))
	providerID := uuid.New()

	s.maybeAlertProviderDown(&CheckResult{ProviderName: "OpenAI", ProviderID: providerID, Error: "conn refused"}, 3, 3)

	if len(stub.got) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(stub.got))
	}
	n := stub.got[0]
	if n.Type != model.NotificationTypeOpsChannelDown {
		t.Errorf("type = %q, want ops_channel_down", n.Type)
	}
	if n.TemplateData["Source"] != "provider_health" {
		t.Errorf("Source = %v, want provider_health", n.TemplateData["Source"])
	}
	if n.TemplateData["ProviderName"] != "OpenAI" {
		t.Errorf("ProviderName = %v, want OpenAI", n.TemplateData["ProviderName"])
	}
	if n.TemplateData["Failures"] != 3 || n.TemplateData["Threshold"] != 3 {
		t.Errorf("Failures/Threshold = %v/%v, want 3/3", n.TemplateData["Failures"], n.TemplateData["Threshold"])
	}
	if n.TemplateData["Error"] != "conn refused" {
		t.Errorf("Error = %v, want conn refused", n.TemplateData["Error"])
	}
	// 达阈值且自动禁用成功的路径才调用此方法；低于阈值/禁用失败路径在调用方不触发。
	if !strings.HasPrefix(n.DedupeKey, "ops:channel_down:provider_health:") {
		t.Errorf("dedupe_key = %q, want prefix ops:channel_down:provider_health:", n.DedupeKey)
	}
}

func TestMaybeAlertProviderRecovered_AlertFiresInfo(t *testing.T) {
	stub := &stubNotifier{}
	s := &HealthCheckService{}
	s.SetOpsAlertSender(NewOpsAlertSender(stub))
	providerID := uuid.New()

	s.maybeAlertProviderRecovered(&model.Provider{Name: "OpenAI", ID: providerID})

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
	if n.TemplateData["ProviderName"] != "OpenAI" {
		t.Errorf("ProviderName = %v, want OpenAI", n.TemplateData["ProviderName"])
	}
}

func TestMaybeAlert_NoSenderNoPanic(t *testing.T) {
	s := &HealthCheckService{}
	s.maybeAlertProviderDown(&CheckResult{ProviderName: "X"}, 3, 3)
	s.maybeAlertProviderRecovered(&model.Provider{Name: "X"})
}
