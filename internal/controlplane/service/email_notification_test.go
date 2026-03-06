package service

import (
	"testing"
)

func TestEmailService_DegradedMode_NoPanic(t *testing.T) {
	// 不设置任何 SMTP 环境变量，确保降级模式不 panic。
	svc := NewEmailService()
	if svc == nil {
		t.Fatal("NewEmailService returned nil")
	}
	if !svc.isDegraded() {
		t.Log("SMTP env vars are set; test will still verify degraded path via nil/empty service")
	}

	// 测试 nil service
	var nilSvc *EmailService
	err := nilSvc.SendAlertEmail([]string{"test@example.com"}, "subject", "body")
	if err != nil {
		t.Errorf("nil service should not return error in degraded mode: %v", err)
	}

	// 测试 degraded service
	err = svc.SendAlertEmail([]string{"test@example.com"}, "[LLM Gateway] Test Alert", "This is a test body with timestamp")
	if err != nil {
		t.Errorf("degraded service should not return error: %v", err)
	}
}

func TestEmailService_NewEmailServiceFromConfig(t *testing.T) {
	// 空配置应进入降级模式
	svc := NewEmailServiceFromConfig("", "", "", "", "", "")
	if svc == nil {
		t.Fatal("NewEmailServiceFromConfig returned nil")
	}
	if !svc.isDegraded() {
		t.Error("empty config should be degraded")
	}

	// 有 host+from 不应降级
	svc2 := NewEmailServiceFromConfig("smtp.example.com", "587", "user", "pass", "from@example.com", "false")
	if svc2 == nil {
		t.Fatal("NewEmailServiceFromConfig returned nil")
	}
	if svc2.isDegraded() {
		t.Error("valid config should not be degraded")
	}
	if svc2.host != "smtp.example.com" {
		t.Errorf("host = %q, want smtp.example.com", svc2.host)
	}
	if svc2.port != 587 {
		t.Errorf("port = %d, want 587", svc2.port)
	}
	if svc2.from != "from@example.com" {
		t.Errorf("from = %q, want from@example.com", svc2.from)
	}

	// from 回退到 user
	svc3 := NewEmailServiceFromConfig("smtp.example.com", "465", "user@example.com", "pass", "", "true")
	if svc3.from != "user@example.com" {
		t.Errorf("from = %q, want user@example.com", svc3.from)
	}
	if !svc3.skipVerify {
		t.Error("skipVerify should be true")
	}
	if svc3.port != 465 {
		t.Errorf("port = %d, want 465", svc3.port)
	}
}

func TestEmailService_DegradedMode_OutputFormat(t *testing.T) {
	// 验证降级模式下的输出格式正确（不 panic、不报错）
	svc := NewEmailServiceFromConfig("", "", "", "", "", "")

	tests := []struct {
		name       string
		recipients []string
		subject    string
		body       string
	}{
		{
			name:       "single recipient",
			recipients: []string{"admin@example.com"},
			subject:    "[LLM Gateway] Test Alert",
			body:       "Test body",
		},
		{
			name:       "multiple recipients",
			recipients: []string{"a@example.com", "b@example.com"},
			subject:    "[LLM Gateway] Daily Report",
			body:       "Report content here",
		},
		{
			name:       "empty recipients",
			recipients: []string{},
			subject:    "Subject",
			body:       "Body",
		},
		{
			name:       "nil recipients",
			recipients: nil,
			subject:    "Subject",
			body:       "Body",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.SendAlertEmail(tt.recipients, tt.subject, tt.body)
			if err != nil {
				t.Errorf("degraded mode should not return error: %v", err)
			}
		})
	}
}

func TestEmailService_SendAlertEmail_NilService(t *testing.T) {
	var svc *EmailService
	err := svc.SendAlertEmail(nil, "subject", "body")
	if err != nil {
		t.Errorf("nil EmailService.SendAlertEmail should not error: %v", err)
	}
}