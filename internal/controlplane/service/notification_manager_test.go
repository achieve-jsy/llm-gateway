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
		"ScopeType": "项目",
		"ScopeID":   "scope-001",
		"Threshold": "90",
		"UsedUSD":   "9500.00",
		"AmountUSD": "10000.00",
	}

	subject, body, err := mgr.renderTemplates("", "", model.NotificationTypeBudgetAlert, params)
	if err != nil {
		t.Fatalf("renderTemplates failed: %v", err)
	}

	if !strings.Contains(subject, "预算告警") {
		t.Errorf("subject should contain '预算告警', got: %s", subject)
	}

	if !strings.Contains(body, "9500.00") {
		t.Errorf("body should contain '9500.00', got: %s", body)
	}

	if !strings.Contains(body, "90%") {
		t.Errorf("body should contain '90%%', got: %s", body)
	}

	if !strings.Contains(body, "scope-001") {
		t.Errorf("body should contain 'scope-001', got: %s", body)
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
			expectInSub: "扩容审批申请",
			expectInBod: "EXP-001",
		},
		{
			notifType: model.NotificationTypeExpansionApproved,
			params: map[string]interface{}{
				"ExpansionID": "EXP-002",
				"Reason":      "业务增长",
			},
			expectInSub: "已批准并生效",
			expectInBod: "EXP-002",
		},
		{
			notifType: model.NotificationTypeExpansionRejected,
			params: map[string]interface{}{
				"ExpansionID": "EXP-003",
				"Reason":      "预算不足",
			},
			expectInSub: "已驳回",
			expectInBod: "预算不足",
		},
		{
			notifType: model.NotificationTypeExpansionApplyFailed,
			params: map[string]interface{}{
				"ExpansionID": "EXP-004",
				"Error":       "执行超时",
			},
			expectInSub: "生效失败",
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
		"EventType":  "new_source",
		"KeyName":    "KEY-123",
		"UserAgent":  "Chrome/120",
		"IPAddress":  "1.2.3.4",
		"Detail":     "过去1小时调用量激增",
	}

	subject, body, err := mgr.renderTemplates("", "", model.NotificationTypeKeyActivity, params)
	if err != nil {
		t.Fatalf("renderTemplates failed: %v", err)
	}

	if !strings.Contains(subject, "Key 新设备使用提醒") {
		t.Errorf("subject should contain 'Key 新设备使用提醒', got: %s", subject)
	}

	if !strings.Contains(body, "KEY-123") {
		t.Errorf("body should contain 'KEY-123', got: %s", body)
	}

	if !strings.Contains(body, "1.2.3.4") {
		t.Errorf("body should contain '1.2.3.4', got: %s", body)
	}

	if !strings.Contains(body, "Chrome/120") {
		t.Errorf("body should contain 'Chrome/120', got: %s", body)
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
		"Violations": "100",
		"SourceRows": "1000",
		"KeysWritten": "50",
		"Cooldown":   "30m0s",
	}

	subject, body, err := mgr.renderTemplates("", "", "platform_risk_counter_integrity", params)
	if err != nil {
		t.Fatalf("renderTemplates failed: %v", err)
	}

	if !strings.Contains(subject, "计数器完整性告警") {
		t.Errorf("subject should contain '计数器完整性告警', got: %s", subject)
	}

	if !strings.Contains(body, "100") {
		t.Errorf("body should contain '100', got: %s", body)
	}

	if !strings.Contains(body, "1000") {
		t.Errorf("body should contain '1000', got: %s", body)
	}

	if !strings.Contains(body, "50") {
		t.Errorf("body should contain '50', got: %s", body)
	}
}

// TestRenderTemplates_BudgetAlertPeriodLabel 验证：budget_alert 模板按 PeriodType
// 动态渲染周期措辞（monthly→本月、weekly→本周、daily→今日），不再硬编码"本月"。
func TestRenderTemplates_BudgetAlertPeriodLabel(t *testing.T) {
	settingsRepo := newMockSettingsRepo()
	mgr := &notificationManager{
		configRepo:   nil,
		settingsRepo: settingsRepo,
		logger:       slog.Default(),
	}

	params := map[string]interface{}{
		"ScopeType":  "studio",
		"ScopeID":    "studio-001",
		"Threshold":  "80",
		"UsedUSD":    "800.00",
		"AmountUSD":  "1000.00",
	}

	cases := []struct {
		periodType string
		want       string
		notWant    []string
	}{
		{"monthly", "本月", []string{"今日", "本周"}},
		{"weekly", "本周", []string{"今日", "本月"}},
		{"daily", "今日", []string{"本周", "本月"}},
	}
	for _, c := range cases {
		params["PeriodType"] = c.periodType
		_, body, err := mgr.renderTemplates("", "", "budget_alert", params)
		if err != nil {
			t.Fatalf("renderTemplates(%s) failed: %v", c.periodType, err)
		}
		if !strings.Contains(body, c.want) {
			t.Errorf("PeriodType=%s: body should contain %q, got: %s", c.periodType, c.want, body)
		}
		for _, nw := range c.notWant {
			if strings.Contains(body, nw) {
				t.Errorf("PeriodType=%s: body should NOT contain %q, got: %s", c.periodType, nw, body)
			}
		}
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

func TestRenderTemplates_OpsAlertTemplates(t *testing.T) {
	settingsRepo := newMockSettingsRepo()
	mgr := &notificationManager{
		configRepo:   nil,
		settingsRepo: settingsRepo,
		logger:       slog.Default(),
	}

	t.Run(model.NotificationTypeOpsChannelDown, func(t *testing.T) {
		params := map[string]interface{}{
			"Source":       "provider_health",
			"ProviderName": "OpenAI",
			"ProviderID":   "abc-123",
			"Failures":     3,
			"Threshold":    3,
			"Error":        "conn refused",
		}
		subject, body, err := mgr.renderTemplates("", "", model.NotificationTypeOpsChannelDown, params)
		if err != nil {
			t.Fatalf("renderTemplates failed: %v", err)
		}
		if !strings.Contains(subject, "渠道探活失败") {
			t.Errorf("subject should contain '渠道探活失败', got: %s", subject)
		}
		if !strings.Contains(subject, "OpenAI") {
			t.Errorf("subject should contain provider name, got: %s", subject)
		}
		if !strings.Contains(body, "provider_health") {
			t.Errorf("body should contain source, got: %s", body)
		}
		if !strings.Contains(body, "abc-123") {
			t.Errorf("body should contain provider id, got: %s", body)
		}
		if !strings.Contains(body, "conn refused") {
			t.Errorf("body should contain error, got: %s", body)
		}
	})

	t.Run(model.NotificationTypeOpsChannelRecovered, func(t *testing.T) {
		params := map[string]interface{}{
			"Source":       "provider_health",
			"ProviderName": "OpenAI",
			"ProviderID":   "abc-123",
		}
		subject, body, err := mgr.renderTemplates("", "", model.NotificationTypeOpsChannelRecovered, params)
		if err != nil {
			t.Fatalf("renderTemplates failed: %v", err)
		}
		if !strings.Contains(subject, "渠道已恢复") {
			t.Errorf("subject should contain '渠道已恢复', got: %s", subject)
		}
		if !strings.Contains(body, "已恢复") {
			t.Errorf("body should mention recovery, got: %s", body)
		}
	})

	t.Run(model.NotificationTypeOpsDeliveryFailed, func(t *testing.T) {
		params := map[string]interface{}{
			"Type":     "budget_alert",
			"Title":    "预算告警",
			"Reason":   "max attempts exceeded (deadletter)",
			"Attempts": 5,
		}
		subject, body, err := mgr.renderTemplates("", "", model.NotificationTypeOpsDeliveryFailed, params)
		if err != nil {
			t.Fatalf("renderTemplates failed: %v", err)
		}
		if !strings.Contains(subject, "通知投递失败") {
			t.Errorf("subject should contain '通知投递失败', got: %s", subject)
		}
		if !strings.Contains(body, "budget_alert") {
			t.Errorf("body should contain type, got: %s", body)
		}
		if !strings.Contains(body, "死信") {
			t.Errorf("body should mention deadletter, got: %s", body)
		}
	})

	t.Run("missing optional fields render empty", func(t *testing.T) {
		// 所有可选字段缺失时模板不应报错，也不应渲染 <no value>
		_, body, err := mgr.renderTemplates("", "", model.NotificationTypeOpsChannelDown,
			map[string]interface{}{"Source": "channel_monitor"})
		if err != nil {
			t.Fatalf("renderTemplates failed: %v", err)
		}
		if strings.Contains(body, "<no value>") {
			t.Errorf("body should not contain '<no value>', got: %s", body)
		}
	})
}

func TestNotificationRegistry_OpsAlertTypesRegistered(t *testing.T) {
	for _, name := range []string{
		model.NotificationTypeOpsChannelDown,
		model.NotificationTypeOpsChannelRecovered,
		model.NotificationTypeOpsDeliveryFailed,
	} {
		def, ok := FindNotificationTypeDef(name)
		if !ok {
			t.Errorf("FindNotificationTypeDef(%q) should find the type", name)
			continue
		}
		if len(def.Channels) == 0 {
			t.Errorf("type %q should declare channels", name)
		}
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
		configRepo:         nil,
		settingsRepo:       settingsRepo,
		legacyWebhookSender: nil,
		emailSender:        nil,
		logger:             slog.Default(),
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
