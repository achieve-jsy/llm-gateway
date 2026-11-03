package service

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
)

// mockSettingsRepo mock SettingsRepository 实现
type mockSettingsRepo struct {
	data map[string]string
}

func newMockSettingsRepo() *mockSettingsRepo {
	return &mockSettingsRepo{data: make(map[string]string)}
}

func (m *mockSettingsRepo) GetValue(key string) (string, error) {
	return m.data[key], nil
}

func (m *mockSettingsRepo) GetMultiple(keys []string) (map[string]string, error) {
	result := make(map[string]string)
	for _, k := range keys {
		if v, ok := m.data[k]; ok {
			result[k] = v
		}
	}
	return result, nil
}

func (m *mockSettingsRepo) SetMultiple(settings map[string]string) error {
	for k, v := range settings {
		m.data[k] = v
	}
	return nil
}

func (m *mockSettingsRepo) GetAll() ([]model.SystemConfig, error) {
	return nil, nil
}

func TestRenderTemplates_WithDefaultTemplates(t *testing.T) {
	// 创建 mock settings repo
	settingsRepo := newMockSettingsRepo()

	// 创建 notificationManager（不需要真实的 configRepo 和 senders，因为只测试模板渲染）
	mgr := &notificationManager{
		configRepo:   nil,
		settingsRepo: settingsRepo,
		logger:       slog.Default(),
	}

	// 测试预算告警默认模板
	params := map[string]interface{}{
		"BudgetName":   "测试预算",
		"CurrentUsage": "95%",
		"Threshold":    "90%",
	}

	subject, body, err := mgr.renderTemplates("", "", model.NotificationTypeBudgetAlert, params)
	if err != nil {
		t.Fatalf("renderTemplates failed: %v", err)
	}

	if !strings.Contains(subject, "预算告警") {
		t.Errorf("subject should contain '预算告警', got: %s", subject)
	}

	if !strings.Contains(body, "测试预算") {
		t.Errorf("body should contain '测试预算', got: %s", body)
	}

	if !strings.Contains(body, "95%") {
		t.Errorf("body should contain '95%%', got: %s", body)
	}

	if !strings.Contains(body, "90%") {
		t.Errorf("body should contain '90%%', got: %s", body)
	}
}

func TestRenderTemplates_WithCustomTemplates(t *testing.T) {
	settingsRepo := newMockSettingsRepo()
	mgr := &notificationManager{
		configRepo:   nil,
		settingsRepo: settingsRepo,
		logger:       slog.Default(),
	}

	customSubject := "自定义主题: {{.CustomVar}}"
	customBody := "自定义内容\n变量: {{.CustomVar}}\n时间: {{.Timestamp}}"

	params := map[string]interface{}{
		"CustomVar": "测试值",
	}

	subject, body, err := mgr.renderTemplates(customSubject, customBody, "test_type", params)
	if err != nil {
		t.Fatalf("renderTemplates failed: %v", err)
	}

	if subject != "自定义主题: 测试值" {
		t.Errorf("subject mismatch, got: %s", subject)
	}

	if !strings.Contains(body, "自定义内容") {
		t.Errorf("body should contain '自定义内容', got: %s", body)
	}

	if !strings.Contains(body, "变量: 测试值") {
		t.Errorf("body should contain '变量: 测试值', got: %s", body)
	}

	// Timestamp 应该被自动注入
	if !strings.Contains(body, "时间: ") {
		t.Errorf("body should contain '时间: ', got: %s", body)
	}
}

func TestRenderTemplates_TimestampAutoInjection(t *testing.T) {
	settingsRepo := newMockSettingsRepo()
	mgr := &notificationManager{
		configRepo:   nil,
		settingsRepo: settingsRepo,
		logger:       slog.Default(),
	}

	bodyTpl := "时间戳: {{.Timestamp}}"
	subject, body, err := mgr.renderTemplates("test", bodyTpl, "test_type", nil)
	if err != nil {
		t.Fatalf("renderTemplates failed: %v", err)
	}

	if subject != "test" {
		t.Errorf("subject mismatch, got: %s", subject)
	}

	// Timestamp 应该包含日期格式 (YYYY-MM-DD HH:MM:SS)
	if !strings.Contains(body, "时间戳: ") {
		t.Errorf("body should contain '时间戳: ', got: %s", body)
	}

	// 应该包含时间戳（至少包含日期部分）
	if len(body) < len("时间戳: 2024-01-01") {
		t.Errorf("body seems too short for timestamp, got: %s", body)
	}
}

func TestRenderTemplates_NotificationTypeInjection(t *testing.T) {
	settingsRepo := newMockSettingsRepo()
	mgr := &notificationManager{
		configRepo:   nil,
		settingsRepo: settingsRepo,
		logger:       slog.Default(),
	}

	bodyTpl := "通知类型: {{.NotificationType}}"
	_, body, err := mgr.renderTemplates("test", bodyTpl, "custom_type", nil)
	if err != nil {
		t.Fatalf("renderTemplates failed: %v", err)
	}

	if !strings.Contains(body, "通知类型: custom_type") {
		t.Errorf("body should contain '通知类型: custom_type', got: %s", body)
	}
}

func TestRenderTemplates_FallbackToGenericTemplate(t *testing.T) {
	settingsRepo := newMockSettingsRepo()
	mgr := &notificationManager{
		configRepo:   nil,
		settingsRepo: settingsRepo,
		logger:       slog.Default(),
	}

	// 测试一个不在 DefaultTemplates 中的类型
	_, body, err := mgr.renderTemplates("", "", "unknown_type", nil)
	if err != nil {
		t.Fatalf("renderTemplates failed: %v", err)
	}

	// 应该使用通用默认模板
	if !strings.Contains(body, "Notification: unknown_type") {
		t.Errorf("body should contain generic template, got: %s", body)
	}
}

func TestRenderTemplates_ExpansionTemplates(t *testing.T) {
	settingsRepo := newMockSettingsRepo()
	mgr := &notificationManager{
		configRepo:   nil,
		settingsRepo: settingsRepo,
		logger:       slog.Default(),
	}

	testCases := []struct {
		notifType   string
		params      map[string]interface{}
		expectInSub string
		expectInBod string
	}{
		{
			notifType: model.NotificationTypeExpansionSubmitted,
			params: map[string]interface{}{
				"ExpansionID": "EXP-001",
				"Applicant":   "张三",
				"Reason":      "业务增长",
			},
			expectInSub: "扩容申请",
			expectInBod: "EXP-001",
		},
		{
			notifType: model.NotificationTypeExpansionApproved,
			params: map[string]interface{}{
				"ExpansionID": "EXP-002",
				"Approver":    "李四",
			},
			expectInSub: "扩容审批通过",
			expectInBod: "EXP-002",
		},
		{
			notifType: model.NotificationTypeExpansionRejected,
			params: map[string]interface{}{
				"ExpansionID": "EXP-003",
				"Approver":    "王五",
				"Reason":      "预算不足",
			},
			expectInSub: "扩容审批拒绝",
			expectInBod: "预算不足",
		},
		{
			notifType: model.NotificationTypeExpansionApplyFailed,
			params: map[string]interface{}{
				"ExpansionID": "EXP-004",
				"Error":       "执行超时",
			},
			expectInSub: "扩容执行失败",
			expectInBod: "执行超时",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.notifType, func(t *testing.T) {
			subject, body, err := mgr.renderTemplates("", "", tc.notifType, tc.params)
			if err != nil {
				t.Fatalf("renderTemplates failed: %v", err)
			}

			if !strings.Contains(subject, tc.expectInSub) {
				t.Errorf("subject should contain '%s', got: %s", tc.expectInSub, subject)
			}

			if !strings.Contains(body, tc.expectInBod) {
				t.Errorf("body should contain '%s', got: %s", tc.expectInBod, body)
			}
		})
	}
}

func TestRenderTemplates_KeyActivityTemplate(t *testing.T) {
	settingsRepo := newMockSettingsRepo()
	mgr := &notificationManager{
		configRepo:   nil,
		settingsRepo: settingsRepo,
		logger:       slog.Default(),
	}

	params := map[string]interface{}{
		"KeyID":    "KEY-123",
		"Activity": "异常调用",
		"Detail":   "过去1小时调用量激增",
	}

	subject, body, err := mgr.renderTemplates("", "", model.NotificationTypeKeyActivity, params)
	if err != nil {
		t.Fatalf("renderTemplates failed: %v", err)
	}

	if !strings.Contains(subject, "密钥活动异常") {
		t.Errorf("subject should contain '密钥活动异常', got: %s", subject)
	}

	if !strings.Contains(body, "KEY-123") {
		t.Errorf("body should contain 'KEY-123', got: %s", body)
	}

	if !strings.Contains(body, "异常调用") {
		t.Errorf("body should contain '异常调用', got: %s", body)
	}

	if !strings.Contains(body, "过去1小时调用量激增") {
		t.Errorf("body should contain detail, got: %s", body)
	}
}

func TestRenderTemplates_PlatformRiskTemplate(t *testing.T) {
	settingsRepo := newMockSettingsRepo()
	mgr := &notificationManager{
		configRepo:   nil,
		settingsRepo: settingsRepo,
		logger:       slog.Default(),
	}

	params := map[string]interface{}{
		"RiskType": "counter_integrity",
		"Counter":  "test_counter",
		"Expected": "100",
		"Actual":   "50",
	}

	subject, body, err := mgr.renderTemplates("", "", "platform_risk_counter_integrity", params)
	if err != nil {
		t.Fatalf("renderTemplates failed: %v", err)
	}

	if !strings.Contains(subject, "平台风险") {
		t.Errorf("subject should contain '平台风险', got: %s", subject)
	}

	if !strings.Contains(body, "test_counter") {
		t.Errorf("body should contain 'test_counter', got: %s", body)
	}

	if !strings.Contains(body, "100") {
		t.Errorf("body should contain '100', got: %s", body)
	}

	if !strings.Contains(body, "50") {
		t.Errorf("body should contain '50', got: %s", body)
	}
}

func TestRenderTemplates_InvalidTemplate(t *testing.T) {
	settingsRepo := newMockSettingsRepo()
	mgr := &notificationManager{
		configRepo:   nil,
		settingsRepo: settingsRepo,
		logger:       slog.Default(),
	}

	// 测试无效的模板语法
	invalidSubject := "{{.Invalid"
	_, _, err := mgr.renderTemplates(invalidSubject, "valid", "test_type", nil)
	if err == nil {
		t.Error("renderTemplates should fail with invalid template syntax")
	}
}

func TestRenderTemplates_MissingVariable(t *testing.T) {
	settingsRepo := newMockSettingsRepo()
	mgr := &notificationManager{
		configRepo:   nil,
		settingsRepo: settingsRepo,
		logger:       slog.Default(),
	}

	// 测试模板引用了不存在的变量（应该成功，变量为空字符串）
	bodyTpl := "值: {{.MissingVar}}"
	_, body, err := mgr.renderTemplates("test", bodyTpl, "test_type", nil)
	if err != nil {
		t.Fatalf("renderTemplates should not fail with missing variable: %v", err)
	}

	// 缺失的变量 Go text/template 渲染为 "<no value>"
	if body != "值: <no value>" {
		t.Errorf("body should be '值: <no value>', got: %s", body)
	}
}

func TestNewNotificationManager(t *testing.T) {
	settingsRepo := newMockSettingsRepo()

	// 测试创建 NotificationManager（使用 nil 依赖，因为构造函数不应该调用它们）
	mgr := NewNotificationManager(nil, settingsRepo, nil, nil, nil)
	if mgr == nil {
		t.Error("NewNotificationManager should return non-nil manager")
	}
}

func TestSendFallback_NoGlobalConfig(t *testing.T) {
	settingsRepo := newMockSettingsRepo()
	// 不设置任何全局配置

	mgr := &notificationManager{
		configRepo:    nil,
		settingsRepo:  settingsRepo,
		webhookSender: nil,
		emailSender:   nil,
		logger:        slog.Default(),
	}

	ctx := context.Background()
	params := map[string]interface{}{"TestVar": "value"}

	// sendFallback 应该成功（即使没有发送任何通知，因为没有配置）
	err := mgr.sendFallback(ctx, "test_type", params)
	if err != nil {
		t.Errorf("sendFallback should succeed even without global config: %v", err)
	}
}

func TestSendFallbackChannel_UnsupportedChannel(t *testing.T) {
	settingsRepo := newMockSettingsRepo()
	mgr := &notificationManager{
		configRepo:   nil,
		settingsRepo: settingsRepo,
		logger:       slog.Default(),
	}

	ctx := context.Background()
	params := map[string]interface{}{}

	err := mgr.sendFallbackChannel(ctx, "test_type", params, "unsupported_channel")
	if err == nil {
		t.Error("sendFallbackChannel should fail with unsupported channel")
	}

	if !strings.Contains(err.Error(), "unsupported fallback channel") {
		t.Errorf("error should mention unsupported channel, got: %v", err)
	}
}
