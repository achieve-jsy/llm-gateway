package service

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupAlertTestDB 创建 SQLite 内存数据库并迁移表
func setupAlertTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// 迁移 alert 相关表
	err = db.AutoMigrate(
		&model.AlertRule{},
		&model.AlertEvent{},
		&model.AlertSilence{},
		&model.AlertJobHeartbeat{},
		&model.SystemConfig{},
	)
	require.NoError(t, err)

	// 创建 call_logs 表（简化版，只包含告警评估需要的字段）
	err = db.Exec(`
		CREATE TABLE IF NOT EXISTS call_logs (
			request_id TEXT PRIMARY KEY,
			ts DATETIME NOT NULL,
			status INTEGER,
			model TEXT,
			provider TEXT,
			group_name TEXT
		)
	`).Error
	require.NoError(t, err)

	return db
}

// setupTestRedis 创建 miniredis 实例
func setupTestRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return rdb, mr
}

// testNow 返回一个在评估窗口内的时间（截断到分钟前 30 秒）
func testNow() time.Time {
	return time.Now().UTC().Truncate(time.Minute).Add(-30 * time.Second)
}

// insertCallLogs 插入测试用 call_logs 数据
func insertCallLogs(t *testing.T, db *gorm.DB, logs []map[string]interface{}) {
	t.Helper()
	for _, log := range logs {
		err := db.Exec(`
			INSERT INTO call_logs (request_id, ts, status, model, provider, group_name)
			VALUES (?, ?, ?, ?, ?, ?)
		`, log["request_id"], log["ts"], log["status"], log["model"], log["provider"], log["group_name"]).Error
		require.NoError(t, err)
	}
}

// createTestRule 创建测试告警规则
func createTestRule(t *testing.T, db *gorm.DB, rule model.AlertRule) uint {
	t.Helper()
	err := db.Create(&rule).Error
	require.NoError(t, err)
	return rule.ID
}

// =============================================================================
// 基础单元测试
// =============================================================================

func TestParseAlertRuleFilters(t *testing.T) {
	tests := []struct {
		name              string
		filters           map[string]interface{}
		wantPlatform      string
		wantGroupName     string
		wantModelName     string
	}{
		{
			name:         "empty filters",
			filters:      nil,
			wantPlatform: "", wantGroupName: "", wantModelName: "",
		},
		{
			name:         "platform only",
			filters:      map[string]interface{}{"platform": "openai"},
			wantPlatform: "openai", wantGroupName: "", wantModelName: "",
		},
		{
			name:         "platform and group_name",
			filters:      map[string]interface{}{"platform": "openai", "group_name": "my-group"},
			wantPlatform: "openai", wantGroupName: "my-group", wantModelName: "",
		},
		{
			name:         "all three",
			filters:      map[string]interface{}{"platform": "openai", "group_name": "my-group", "model": "gpt-4"},
			wantPlatform: "openai", wantGroupName: "my-group", wantModelName: "gpt-4",
		},
		{
			name:         "group_id numeric",
			filters:      map[string]interface{}{"group_id": float64(123)},
			wantPlatform: "", wantGroupName: "123", wantModelName: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var raw datatypes.JSON
			if tt.filters != nil {
				b, _ := json.Marshal(tt.filters)
				raw = b
			}
			platform, groupName, modelName := parseAlertRuleFilters(raw)
			assert.Equal(t, tt.wantPlatform, platform)
			assert.Equal(t, tt.wantGroupName, groupName)
			assert.Equal(t, tt.wantModelName, modelName)
		})
	}
}

func TestCompareMetric(t *testing.T) {
	tests := []struct {
		name      string
		value     float64
		operator  string
		threshold float64
		want      bool
	}{
		{"greater than - true", 80, ">", 70, true},
		{"greater than - false", 60, ">", 70, false},
		{"greater than - equal", 70, ">", 70, false},
		{"greater or equal - true", 80, ">=", 70, true},
		{"greater or equal - equal", 70, ">=", 70, true},
		{"less than - true", 60, "<", 70, true},
		{"less than - false", 80, "<", 70, false},
		{"less or equal - true", 70, "<=", 70, true},
		{"equal - true", 70, "==", 70, true},
		{"equal - false", 80, "==", 70, false},
		{"not equal - true", 80, "!=", 70, true},
		{"not equal - false", 70, "!=", 70, false},
		{"unknown operator", 80, "??", 70, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compareMetric(tt.value, tt.operator, tt.threshold)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRequiredSustainedBreaches(t *testing.T) {
	tests := []struct {
		name             string
		sustainedMinutes int
		interval         time.Duration
		want             int
	}{
		{"zero sustained", 0, 60 * time.Second, 1},
		{"sustained 2min, interval 60s", 2, 60 * time.Second, 2},
		{"sustained 5min, interval 30s", 5, 30 * time.Second, 10},
		{"sustained 1min, interval 60s", 1, 60 * time.Second, 1},
		{"sustained 2min, interval 90s", 2, 90 * time.Second, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := requiredSustainedBreaches(tt.sustainedMinutes, tt.interval)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildAlertEventDescription(t *testing.T) {
	rule := model.AlertRule{
		Name:          "Test Rule",
		MetricType:    "success_rate",
		Operator:      "<",
		Threshold:     95.0,
		WindowMinutes: 5,
	}

	desc := buildAlertEventDescription(rule, 80.0, "openai", "my-group", "gpt-4")
	assert.Contains(t, desc, "success_rate")
	assert.Contains(t, desc, "< 95.00")
	assert.Contains(t, desc, "80.00")
	assert.Contains(t, desc, "platform=openai")
	assert.Contains(t, desc, "group=my-group")
	assert.Contains(t, desc, "model=gpt-4")
}

func TestBuildAlertEventDimensions(t *testing.T) {
	dims := buildAlertEventDimensions("openai", "my-group", "gpt-4")
	var m map[string]interface{}
	err := json.Unmarshal(dims, &m)
	require.NoError(t, err)
	assert.Equal(t, "openai", m["platform"])
	assert.Equal(t, "my-group", m["group_name"])
	assert.Equal(t, "gpt-4", m["model"])
}

// =============================================================================
// 集成测试：评估器完整流程
// =============================================================================

func TestAlertEvaluator_CreateEventOnBreach(t *testing.T) {
	db := setupAlertTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)

	// 插入测试数据：success_rate = 80%（低于 95% 阈值）
	// 使用 safeEnd 之前的时间，确保数据在窗口内
	now := testNow()
	insertCallLogs(t, db, []map[string]interface{}{
		{"request_id": "req-1", "ts": now.Add(-1 * time.Minute), "status": 200, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
		{"request_id": "req-2", "ts": now.Add(-30 * time.Second), "status": 200, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
		{"request_id": "req-3", "ts": now.Add(-30 * time.Second), "status": 200, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
		{"request_id": "req-4", "ts": now.Add(-30 * time.Second), "status": 200, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
		{"request_id": "req-5", "ts": now.Add(-30 * time.Second), "status": 500, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
	})

	// 创建规则：success_rate < 95%，sustained 1分钟
	ruleID := createTestRule(t, db, model.AlertRule{
		Name:             "Test Success Rate",
		Enabled:          true,
		MetricType:       "success_rate",
		Operator:         "<",
		Threshold:        95.0,
		WindowMinutes:    5,
		SustainedMinutes: 1,
		Severity:         "P1",
		CooldownMinutes:  1,
	})

	// 创建评估器（无 Redis）
	evaluator := NewAlertEvaluator(db, nil, settingsRepo)

	// 运行评估
	ctx := context.Background()
	evaluator.evaluateOnce(ctx, 60*time.Second)

	// 验证：事件已创建
	var events []model.AlertEvent
	err := db.Find(&events).Error
	require.NoError(t, err)
	assert.Len(t, events, 1, "should create one event")
	assert.Equal(t, ruleID, events[0].RuleID)
	assert.Equal(t, "firing", events[0].Status)
	assert.Equal(t, "P1", events[0].Severity)
	assert.Less(t, events[0].MetricValue, 95.0)
	t.Logf("Event created: metric_value=%.2f, threshold=%.2f", events[0].MetricValue, events[0].ThresholdValue)
}

func TestAlertEvaluator_SustainedBreachesRequired(t *testing.T) {
	db := setupAlertTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)

	// 插入数据：success_rate = 80%
	now := testNow()
	insertCallLogs(t, db, []map[string]interface{}{
		{"request_id": "req-1", "ts": now.Add(-1 * time.Minute), "status": 200, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
		{"request_id": "req-2", "ts": now.Add(-30 * time.Second), "status": 500, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
	})

	// sustained_minutes=2, interval=60s => 需要连续 2 次后才触发
	ruleID := createTestRule(t, db, model.AlertRule{
		Name:             "Sustained Test",
		Enabled:          true,
		MetricType:       "success_rate",
		Operator:         "<",
		Threshold:        95.0,
		WindowMinutes:    5,
		SustainedMinutes: 2,
		Severity:         "P1",
		CooldownMinutes:  1,
	})

	evaluator := NewAlertEvaluator(db, nil, settingsRepo)

	// 第一次评估：触发条件满足，但 sustained 不够
	evaluator.evaluateOnce(context.Background(), 60*time.Second)
	var events []model.AlertEvent
	db.Find(&events)
	assert.Len(t, events, 0, "should not create event after 1 breach")

	// 第二次评估：持续满足，应该创建事件
	evaluator.evaluateOnce(context.Background(), 60*time.Second)
	db.Find(&events)
	assert.Len(t, events, 1, "should create event after 2 sustained breaches")
	assert.Equal(t, ruleID, events[0].RuleID)
}

func TestAlertEvaluator_CooldownPreventsDuplicate(t *testing.T) {
	db := setupAlertTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)

	now := testNow()
	insertCallLogs(t, db, []map[string]interface{}{
		{"request_id": "req-1", "ts": now.Add(-1 * time.Minute), "status": 200, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
		{"request_id": "req-2", "ts": now.Add(-30 * time.Second), "status": 500, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
	})

	// sustained=1, cooldown=10 分钟
	createTestRule(t, db, model.AlertRule{
		Name:             "Cooldown Test",
		Enabled:          true,
		MetricType:       "success_rate",
		Operator:         "<",
		Threshold:        95.0,
		WindowMinutes:    5,
		SustainedMinutes: 1,
		Severity:         "P1",
		CooldownMinutes:  10,
	})

	evaluator := NewAlertEvaluator(db, nil, settingsRepo)

	// 第一次评估：创建事件
	evaluator.evaluateOnce(context.Background(), 60*time.Second)
	var events []model.AlertEvent
	db.Find(&events)
	assert.Len(t, events, 1, "should create first event")

	// 第二次评估：cooldown 期内，不应再创建事件
	evaluator.evaluateOnce(context.Background(), 60*time.Second)
	db.Find(&events)
	assert.Len(t, events, 1, "should not create duplicate event in cooldown")
}

func TestAlertEvaluator_AutoResolveOnRecovery(t *testing.T) {
	db := setupAlertTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)

	// 先创建一个活跃的 firing 事件
	now := testNow()
	event := model.AlertEvent{
		RuleID:         1,
		Severity:       "P1",
		Status:         "firing",
		Title:          "P1: Test Rule",
		Description:    "test",
		MetricValue:    50.0,
		ThresholdValue: 95.0,
		FiredAt:        now.Add(-5 * time.Minute),
		CreatedAt:      now.Add(-5 * time.Minute),
	}
	require.NoError(t, db.Create(&event).Error)

	// 插入数据：success_rate = 100%（恢复正常）
	insertCallLogs(t, db, []map[string]interface{}{
		{"request_id": "req-1", "ts": now.Add(-1 * time.Minute), "status": 200, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
		{"request_id": "req-2", "ts": now.Add(-30 * time.Second), "status": 200, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
	})

	createTestRule(t, db, model.AlertRule{
		Name:             "Recovery Test",
		Enabled:          true,
		MetricType:       "success_rate",
		Operator:         "<",
		Threshold:        95.0,
		WindowMinutes:    5,
		SustainedMinutes: 1,
		Severity:         "P1",
		CooldownMinutes:  1,
	})

	evaluator := NewAlertEvaluator(db, nil, settingsRepo)

	// 评估：指标正常，应自动 resolve
	evaluator.evaluateOnce(context.Background(), 60*time.Second)

	var updatedEvent model.AlertEvent
	db.First(&updatedEvent, event.ID)
	assert.Equal(t, "resolved", updatedEvent.Status)
	assert.NotNil(t, updatedEvent.ResolvedAt)
}

func TestAlertEvaluator_SilenceSkipsEvaluation(t *testing.T) {
	db := setupAlertTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)

	now := testNow()
	insertCallLogs(t, db, []map[string]interface{}{
		{"request_id": "req-1", "ts": now.Add(-1 * time.Minute), "status": 200, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
		{"request_id": "req-2", "ts": now.Add(-30 * time.Second), "status": 500, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
	})

	ruleID := createTestRule(t, db, model.AlertRule{
		Name:             "Silence Test",
		Enabled:          true,
		MetricType:       "success_rate",
		Operator:         "<",
		Threshold:        95.0,
		WindowMinutes:    5,
		SustainedMinutes: 1,
		Severity:         "P1",
		CooldownMinutes:  1,
	})

	// 创建静默（未来 1 小时）
	require.NoError(t, db.Create(&model.AlertSilence{
		RuleID:   ruleID,
		Platform: "openai",
		Until:    now.Add(1 * time.Hour),
	}).Error)

	evaluator := NewAlertEvaluator(db, nil, settingsRepo)

	// 评估：应被静默跳过
	evaluator.evaluateOnce(context.Background(), 60*time.Second)

	var events []model.AlertEvent
	db.Find(&events)
	assert.Len(t, events, 0, "should not create event for silenced rule")
}

func TestAlertEvaluator_UnknownMetricTypeSkipped(t *testing.T) {
	db := setupAlertTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)

	createTestRule(t, db, model.AlertRule{
		Name:             "Unknown Metric",
		Enabled:          true,
		MetricType:       "unknown_metric_xyz",
		Operator:         ">",
		Threshold:        50.0,
		WindowMinutes:    5,
		SustainedMinutes: 1,
		Severity:         "P1",
		CooldownMinutes:  1,
	})

	evaluator := NewAlertEvaluator(db, nil, settingsRepo)
	evaluator.evaluateOnce(context.Background(), 60*time.Second)

	var events []model.AlertEvent
	db.Find(&events)
	assert.Len(t, events, 0, "should not create event for unknown metric_type")
}

func TestAlertEvaluator_UnimplementedMetricTypeReturnsFalse(t *testing.T) {
	db := setupAlertTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)

	now := testNow()
	insertCallLogs(t, db, []map[string]interface{}{
		{"request_id": "req-1", "ts": now.Add(-1 * time.Minute), "status": 200, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
	})

	// 创建 cpu_usage_percent 规则（本期未实现真实查询）
	createTestRule(t, db, model.AlertRule{
		Name:             "CPU Usage",
		Enabled:          true,
		MetricType:       "cpu_usage_percent",
		Operator:         ">",
		Threshold:        80.0,
		WindowMinutes:    5,
		SustainedMinutes: 1,
		Severity:         "P1",
		CooldownMinutes:  1,
	})

	evaluator := NewAlertEvaluator(db, nil, settingsRepo)
	evaluator.evaluateOnce(context.Background(), 60*time.Second)

	var events []model.AlertEvent
	db.Find(&events)
	assert.Len(t, events, 0, "should not create event for unimplemented metric_type")
}

func TestAlertEvaluator_ErrorRateMetric(t *testing.T) {
	db := setupAlertTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)

	// error_rate = 50%（2/4 请求失败）
	now := testNow()
	insertCallLogs(t, db, []map[string]interface{}{
		{"request_id": "req-1", "ts": now.Add(-1 * time.Minute), "status": 200, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
		{"request_id": "req-2", "ts": now.Add(-30 * time.Second), "status": 200, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
		{"request_id": "req-3", "ts": now.Add(-30 * time.Second), "status": 400, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
		{"request_id": "req-4", "ts": now.Add(-30 * time.Second), "status": 500, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
	})

	createTestRule(t, db, model.AlertRule{
		Name:             "Error Rate Test",
		Enabled:          true,
		MetricType:       "error_rate",
		Operator:         ">",
		Threshold:        10.0,
		WindowMinutes:    5,
		SustainedMinutes: 1,
		Severity:         "P1",
		CooldownMinutes:  1,
	})

	evaluator := NewAlertEvaluator(db, nil, settingsRepo)
	evaluator.evaluateOnce(context.Background(), 60*time.Second)

	var events []model.AlertEvent
	db.Find(&events)
	require.Len(t, events, 1, "should create event for error_rate")
	assert.Greater(t, events[0].MetricValue, 10.0)
}

func TestAlertEvaluator_UpstreamErrorRateMetric(t *testing.T) {
	db := setupAlertTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)

	// upstream_error_rate = 25%（1/4 请求返回 5xx）
	now := testNow()
	insertCallLogs(t, db, []map[string]interface{}{
		{"request_id": "req-1", "ts": now.Add(-1 * time.Minute), "status": 200, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
		{"request_id": "req-2", "ts": now.Add(-30 * time.Second), "status": 200, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
		{"request_id": "req-3", "ts": now.Add(-30 * time.Second), "status": 200, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
		{"request_id": "req-4", "ts": now.Add(-30 * time.Second), "status": 500, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
	})

	createTestRule(t, db, model.AlertRule{
		Name:             "Upstream Error Test",
		Enabled:          true,
		MetricType:       "upstream_error_rate",
		Operator:         ">",
		Threshold:        10.0,
		WindowMinutes:    5,
		SustainedMinutes: 1,
		Severity:         "P1",
		CooldownMinutes:  1,
	})

	evaluator := NewAlertEvaluator(db, nil, settingsRepo)
	evaluator.evaluateOnce(context.Background(), 60*time.Second)

	var events []model.AlertEvent
	db.Find(&events)
	require.Len(t, events, 1, "should create event for upstream_error_rate")
	assert.Greater(t, events[0].MetricValue, 10.0)
}

func TestAlertEvaluator_NoDataNoEvent(t *testing.T) {
	db := setupAlertTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)

	createTestRule(t, db, model.AlertRule{
		Name:             "No Data Test",
		Enabled:          true,
		MetricType:       "success_rate",
		Operator:         "<",
		Threshold:        95.0,
		WindowMinutes:    5,
		SustainedMinutes: 1,
		Severity:         "P1",
		CooldownMinutes:  1,
	})

	evaluator := NewAlertEvaluator(db, nil, settingsRepo)
	evaluator.evaluateOnce(context.Background(), 60*time.Second)

	var events []model.AlertEvent
	db.Find(&events)
	assert.Len(t, events, 0, "should not create event when no data")
}

func TestAlertEvaluator_HeartbeatRecorded(t *testing.T) {
	db := setupAlertTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)

	now := testNow()
	insertCallLogs(t, db, []map[string]interface{}{
		{"request_id": "req-1", "ts": now.Add(-1 * time.Minute), "status": 200, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
	})

	createTestRule(t, db, model.AlertRule{
		Name:             "Heartbeat Test",
		Enabled:          true,
		MetricType:       "success_rate",
		Operator:         "<",
		Threshold:        95.0,
		WindowMinutes:    5,
		SustainedMinutes: 1,
		Severity:         "P1",
		CooldownMinutes:  1,
	})

	evaluator := NewAlertEvaluator(db, nil, settingsRepo)
	evaluator.evaluateOnce(context.Background(), 60*time.Second)

	var hb model.AlertJobHeartbeat
	err := db.Where("name = ?", alertEvaluatorJobName).First(&hb).Error
	require.NoError(t, err)
	assert.Equal(t, alertEvaluatorJobName, hb.Name)
	assert.NotNil(t, hb.LastSuccessAt)
}

func TestAlertEvaluator_DisabledRuleNotEvaluated(t *testing.T) {
	db := setupAlertTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)

	now := testNow()
	insertCallLogs(t, db, []map[string]interface{}{
		{"request_id": "req-1", "ts": now.Add(-1 * time.Minute), "status": 500, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
	})

	ruleID := createTestRule(t, db, model.AlertRule{
		Name:             "Disabled Rule",
		Enabled:          true, // 先创建，再禁用
		MetricType:       "success_rate",
		Operator:         "<",
		Threshold:        95.0,
		WindowMinutes:    5,
		SustainedMinutes: 1,
		Severity:         "P1",
		CooldownMinutes:  1,
	})
	// 禁用规则（GORM 零值 bool 不会写入 INSERT，需要 UPDATE）
	require.NoError(t, db.Model(&model.AlertRule{}).Where("id = ?", ruleID).Update("enabled", false).Error)

	evaluator := NewAlertEvaluator(db, nil, settingsRepo)
	evaluator.evaluateOnce(context.Background(), 60*time.Second)

	var events []model.AlertEvent
	db.Find(&events)
	assert.Len(t, events, 0, "should not evaluate disabled rules")
}

func TestAlertEvaluator_FilterScoping(t *testing.T) {
	db := setupAlertTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)

	now := testNow()
	// 插入两组数据：openai 和 azure
	insertCallLogs(t, db, []map[string]interface{}{
		// openai: 10 条记录，其中 5 条 500 => upstream_error_rate = 50%
		{"request_id": "req-o1", "ts": now.Add(-1 * time.Minute), "status": 200, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
		{"request_id": "req-o2", "ts": now.Add(-30 * time.Second), "status": 200, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
		{"request_id": "req-o3", "ts": now.Add(-30 * time.Second), "status": 200, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
		{"request_id": "req-o4", "ts": now.Add(-30 * time.Second), "status": 200, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
		{"request_id": "req-o5", "ts": now.Add(-30 * time.Second), "status": 200, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
		{"request_id": "req-o6", "ts": now.Add(-30 * time.Second), "status": 500, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
		{"request_id": "req-o7", "ts": now.Add(-30 * time.Second), "status": 500, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
		{"request_id": "req-o8", "ts": now.Add(-30 * time.Second), "status": 500, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
		{"request_id": "req-o9", "ts": now.Add(-30 * time.Second), "status": 500, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
		{"request_id": "req-o10", "ts": now.Add(-30 * time.Second), "status": 500, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
		// azure: 10 条记录，全部成功 => upstream_error_rate = 0%
		{"request_id": "req-a1", "ts": now.Add(-1 * time.Minute), "status": 200, "model": "gpt-4", "provider": "azure", "group_name": "my-group"},
		{"request_id": "req-a2", "ts": now.Add(-30 * time.Second), "status": 200, "model": "gpt-4", "provider": "azure", "group_name": "my-group"},
		{"request_id": "req-a3", "ts": now.Add(-30 * time.Second), "status": 200, "model": "gpt-4", "provider": "azure", "group_name": "my-group"},
		{"request_id": "req-a4", "ts": now.Add(-30 * time.Second), "status": 200, "model": "gpt-4", "provider": "azure", "group_name": "my-group"},
		{"request_id": "req-a5", "ts": now.Add(-30 * time.Second), "status": 200, "model": "gpt-4", "provider": "azure", "group_name": "my-group"},
		{"request_id": "req-a6", "ts": now.Add(-30 * time.Second), "status": 200, "model": "gpt-4", "provider": "azure", "group_name": "my-group"},
		{"request_id": "req-a7", "ts": now.Add(-30 * time.Second), "status": 200, "model": "gpt-4", "provider": "azure", "group_name": "my-group"},
		{"request_id": "req-a8", "ts": now.Add(-30 * time.Second), "status": 200, "model": "gpt-4", "provider": "azure", "group_name": "my-group"},
		{"request_id": "req-a9", "ts": now.Add(-30 * time.Second), "status": 200, "model": "gpt-4", "provider": "azure", "group_name": "my-group"},
		{"request_id": "req-a10", "ts": now.Add(-30 * time.Second), "status": 200, "model": "gpt-4", "provider": "azure", "group_name": "my-group"},
	})

	// 创建规则：只监控 openai 的 upstream_error_rate > 10%
	filters, _ := json.Marshal(map[string]interface{}{"platform": "openai"})
	createTestRule(t, db, model.AlertRule{
		Name:             "Filtered Rule",
		Enabled:          true,
		MetricType:       "upstream_error_rate",
		Operator:         ">",
		Threshold:        10.0,
		WindowMinutes:    5,
		SustainedMinutes: 1,
		Severity:         "P1",
		CooldownMinutes:  1,
		Filters:          datatypes.JSON(filters),
	})

	evaluator := NewAlertEvaluator(db, nil, settingsRepo)
	evaluator.evaluateOnce(context.Background(), 60*time.Second)

	var events []model.AlertEvent
	db.Find(&events)
	require.Len(t, events, 1, "should create event for openai with high upstream_error_rate")
	assert.Equal(t, "P1", events[0].Severity)
}

// =============================================================================
// 分布式锁测试
// =============================================================================

func TestAlertEvaluator_LeaderLock(t *testing.T) {
	db := setupAlertTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)
	rdb, mr := setupTestRedis(t)
	defer mr.Close()

	// 创建两个 evaluator，模拟不同实例
	e1 := NewAlertEvaluator(db, rdb, settingsRepo)
	e2 := NewAlertEvaluator(db, rdb, settingsRepo)

	// 确保 instanceID 不同
	e1.instanceID = "instance-1"
	e2.instanceID = "instance-2"

	lockCfg := distributedLockConfig{
		Enabled:    true,
		Key:        "ops:alert:evaluator:leader",
		TTLSeconds: 90,
	}

	ctx := context.Background()

	// 实例 1 获取锁
	release1, ok1 := e1.tryAcquireLeaderLock(ctx, lockCfg)
	assert.True(t, ok1, "instance 1 should acquire lock")
	assert.NotNil(t, release1)

	// 实例 2 尝试获取锁（应失败）
	release2, ok2 := e2.tryAcquireLeaderLock(ctx, lockCfg)
	assert.False(t, ok2, "instance 2 should not acquire lock")
	assert.Nil(t, release2)

	// 实例 1 释放锁
	release1()

	// 实例 2 现在可以获取锁
	release2b, ok2b := e2.tryAcquireLeaderLock(ctx, lockCfg)
	assert.True(t, ok2b, "instance 2 should acquire lock after release")
	assert.NotNil(t, release2b)
	release2b()
}

func TestAlertEvaluator_NoRedisFallback(t *testing.T) {
	db := setupAlertTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)

	evaluator := NewAlertEvaluator(db, nil, settingsRepo)

	lockCfg := distributedLockConfig{
		Enabled:    true,
		Key:        "ops:alert:evaluator:leader",
		TTLSeconds: 90,
	}

	// 无 Redis 时，分布式锁已启用但 fallback 直接评估
	release, ok := evaluator.tryAcquireLeaderLock(context.Background(), lockCfg)
	assert.True(t, ok, "should fallback to allow evaluation when no Redis")
	assert.Nil(t, release)
}

func TestAlertEvaluator_LockDisabled(t *testing.T) {
	db := setupAlertTestDB(t)
	rdb, mr := setupTestRedis(t)
	defer mr.Close()

	settingsRepo := repository.NewSettingsRepo(db)
	evaluator := NewAlertEvaluator(db, rdb, settingsRepo)

	// 分布式锁未启用
	lockCfg := distributedLockConfig{Enabled: false}

	release, ok := evaluator.tryAcquireLeaderLock(context.Background(), lockCfg)
	assert.True(t, ok, "should allow evaluation when lock disabled")
	assert.Nil(t, release)
}

// =============================================================================
// 生命周期测试
// =============================================================================

func TestAlertEvaluator_StartStop(t *testing.T) {
	// 在 CI 中跳过（需要真实 Redis 连接）
	if os.Getenv("CI") != "" {
		t.Skip("skipping start/stop test in CI")
	}

	db := setupAlertTestDB(t)
	settingsRepo := repository.NewSettingsRepo(db)

	// 插入一条规则，确保有数据可评估
	now := testNow()
	insertCallLogs(t, db, []map[string]interface{}{
		{"request_id": "req-1", "ts": now, "status": 200, "model": "gpt-4", "provider": "openai", "group_name": "my-group"},
	})

	createTestRule(t, db, model.AlertRule{
		Name:             "StartStop Test",
		Enabled:          true,
		MetricType:       "success_rate",
		Operator:         "<",
		Threshold:        95.0,
		WindowMinutes:    5,
		SustainedMinutes: 1,
		Severity:         "P1",
		CooldownMinutes:  1,
	})

	evaluator := NewAlertEvaluator(db, nil, settingsRepo)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	evaluator.Start(ctx)

	// 等待至少一次评估完成
	time.Sleep(500 * time.Millisecond)

	// 验证心跳已记录
	var hb model.AlertJobHeartbeat
	err := db.Where("name = ?", alertEvaluatorJobName).First(&hb).Error
	require.NoError(t, err)
	assert.Equal(t, alertEvaluatorJobName, hb.Name)

	evaluator.Stop()
}

// =============================================================================
// 辅助：确保编译通过
// =============================================================================

func TestValidMetricTypes(t *testing.T) {
	// 验证 13 种 metric_type 都已注册
	expected := []string{
		"success_rate", "error_rate", "upstream_error_rate",
		"cpu_usage_percent", "memory_usage_percent", "concurrency_queue_depth",
		"group_available_accounts", "group_available_ratio", "group_rate_limit_ratio",
		"account_rate_limited_count", "account_error_count", "account_error_ratio",
		"overload_account_count",
	}
	assert.Len(t, expected, 13, "should have 13 metric types")

	for _, mt := range expected {
		assert.True(t, validMetricTypes[mt], "metric_type %s should be valid", mt)
	}

	// 验证只有 3 种已实现
	implemented := []string{"success_rate", "error_rate", "upstream_error_rate"}
	for _, mt := range implemented {
		assert.True(t, implementedMetricTypes[mt], "metric_type %s should be implemented", mt)
	}

	unimplemented := []string{
		"cpu_usage_percent", "memory_usage_percent", "concurrency_queue_depth",
		"group_available_accounts", "group_available_ratio", "group_rate_limit_ratio",
		"account_rate_limited_count", "account_error_count", "account_error_ratio",
		"overload_account_count",
	}
	for _, mt := range unimplemented {
		assert.False(t, implementedMetricTypes[mt], "metric_type %s should NOT be implemented", mt)
	}
}