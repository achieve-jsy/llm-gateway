package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupScheduledReportTestDB 创建 SQLite 内存数据库并创建 call_logs 表
func setupScheduledReportTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// 创建 system_configs 表
	err = db.Exec(`CREATE TABLE IF NOT EXISTS system_configs (
		key TEXT PRIMARY KEY,
		value TEXT
	)`).Error
	require.NoError(t, err)

	// 创建 call_logs 表（简化版）
	err = db.Exec(`
		CREATE TABLE IF NOT EXISTS call_logs (
			request_id TEXT PRIMARY KEY,
			ts DATETIME NOT NULL,
			status INTEGER,
			model TEXT,
			provider TEXT,
			prompt_tokens INTEGER DEFAULT 0,
			completion_tokens INTEGER DEFAULT 0,
			total_tokens INTEGER DEFAULT 0,
			duration_ms INTEGER
		)
	`).Error
	require.NoError(t, err)

	return db
}

// insertScheduledReportCallLogs 插入测试 call_logs 数据
func insertScheduledReportCallLogs(t *testing.T, db *gorm.DB, logs []map[string]interface{}) {
	t.Helper()
	for _, log := range logs {
		err := db.Exec(`
			INSERT INTO call_logs (request_id, ts, status, model, provider, prompt_tokens, completion_tokens, total_tokens, duration_ms)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, log["request_id"], log["ts"], log["status"], log["model"], log["provider"],
			log["prompt_tokens"], log["completion_tokens"], log["total_tokens"], log["duration_ms"]).Error
		require.NoError(t, err)
	}
}

func TestScheduledReportService_GenerateSummaryReport(t *testing.T) {
	db := setupScheduledReportTestDB(t)
	now := time.Now().UTC()

	// 插入测试数据
	insertScheduledReportCallLogs(t, db, []map[string]interface{}{
		{"request_id": "r1", "ts": now.Add(-1 * time.Hour), "status": 200, "model": "gpt-4", "provider": "openai", "prompt_tokens": 100, "completion_tokens": 50, "total_tokens": 150, "duration_ms": 500},
		{"request_id": "r2", "ts": now.Add(-2 * time.Hour), "status": 200, "model": "gpt-4", "provider": "openai", "prompt_tokens": 200, "completion_tokens": 100, "total_tokens": 300, "duration_ms": 800},
		{"request_id": "r3", "ts": now.Add(-3 * time.Hour), "status": 500, "model": "gpt-4", "provider": "openai", "prompt_tokens": 50, "completion_tokens": 0, "total_tokens": 50, "duration_ms": 200},
		{"request_id": "r4", "ts": now.Add(-5 * time.Hour), "status": 429, "model": "claude", "provider": "anthropic", "prompt_tokens": 30, "completion_tokens": 0, "total_tokens": 30, "duration_ms": 100},
	})

	svc := &ScheduledReportService{db: db}
	report := &scheduledReport{
		Name:       "日报",
		ReportType: "daily_summary",
		TimeRange:  24 * time.Hour,
		Recipients: []string{"admin@example.com"},
	}

	content, err := svc.generateReport(context.Background(), report, now)
	require.NoError(t, err)
	assert.NotEmpty(t, content)
	assert.Contains(t, content, "日报")
	assert.Contains(t, content, "Total")
	assert.Contains(t, content, "Success")
	assert.Contains(t, content, "Tokens")
}

func TestScheduledReportService_GenerateErrorDigest(t *testing.T) {
	db := setupScheduledReportTestDB(t)
	now := time.Now().UTC()

	// 插入测试数据：openai/gpt-4 有 50% 错误率 (>5%)，anthropic/claude 全部成功
	insertScheduledReportCallLogs(t, db, []map[string]interface{}{
		{"request_id": "e1", "ts": now.Add(-1 * time.Hour), "status": 200, "model": "gpt-4", "provider": "openai", "prompt_tokens": 100, "completion_tokens": 50, "total_tokens": 150, "duration_ms": 500},
		{"request_id": "e2", "ts": now.Add(-2 * time.Hour), "status": 500, "model": "gpt-4", "provider": "openai", "prompt_tokens": 50, "completion_tokens": 0, "total_tokens": 50, "duration_ms": 200},
		{"request_id": "e3", "ts": now.Add(-3 * time.Hour), "status": 200, "model": "claude", "provider": "anthropic", "prompt_tokens": 100, "completion_tokens": 30, "total_tokens": 130, "duration_ms": 300},
		{"request_id": "e4", "ts": now.Add(-4 * time.Hour), "status": 200, "model": "claude", "provider": "anthropic", "prompt_tokens": 80, "completion_tokens": 20, "total_tokens": 100, "duration_ms": 250},
	})

	svc := &ScheduledReportService{db: db}
	report := &scheduledReport{
		Name:       "错误摘要",
		ReportType: "error_digest",
		TimeRange:  24 * time.Hour,
		Recipients: []string{"admin@example.com"},
	}

	content, err := svc.generateReport(context.Background(), report, now)
	require.NoError(t, err)
	assert.NotEmpty(t, content)
	assert.Contains(t, content, "openai")
	// anthropic 应为 0% 错误率，不应出现在摘要中
	assert.NotContains(t, content, "anthropic")
}

func TestScheduledReportService_GenerateAccountHealth(t *testing.T) {
	db := setupScheduledReportTestDB(t)
	now := time.Now().UTC()

	insertScheduledReportCallLogs(t, db, []map[string]interface{}{
		{"request_id": "a1", "ts": now.Add(-1 * time.Hour), "status": 200, "model": "gpt-4", "provider": "openai", "prompt_tokens": 100, "completion_tokens": 50, "total_tokens": 150, "duration_ms": 500},
		{"request_id": "a2", "ts": now.Add(-2 * time.Hour), "status": 200, "model": "claude", "provider": "anthropic", "prompt_tokens": 80, "completion_tokens": 20, "total_tokens": 100, "duration_ms": 250},
		{"request_id": "a3", "ts": now.Add(-3 * time.Hour), "status": 500, "model": "gpt-4", "provider": "openai", "prompt_tokens": 50, "completion_tokens": 0, "total_tokens": 50, "duration_ms": 200},
	})

	svc := &ScheduledReportService{db: db}
	report := &scheduledReport{
		Name:       "账号健康",
		ReportType: "account_health",
		TimeRange:  24 * time.Hour,
		Recipients: []string{"admin@example.com"},
	}

	content, err := svc.generateReport(context.Background(), report, now)
	require.NoError(t, err)
	assert.NotEmpty(t, content)
	assert.Contains(t, content, "openai")
	assert.Contains(t, content, "anthropic")
	// openai 应该排在前面（更低的可用率）
	assert.True(t, indexOf(content, "openai") < indexOf(content, "anthropic"),
		"openai should appear before anthropic due to lower availability")
}

func TestScheduledReportService_LeaderLock(t *testing.T) {
	// 启动 miniredis
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	emailSvc := NewEmailServiceFromConfig("", "", "", "", "", "")
	svc := NewScheduledReportService(nil, rdb, emailSvc)
	svc.instanceID = "test-instance-1"

	ctx := context.Background()

	// 第一次应获取到锁
	release1, ok1 := svc.tryAcquireLeaderLock(ctx)
	assert.True(t, ok1)
	assert.NotNil(t, release1)

	// 第二次不应获取到锁（同一实例已被锁定）
	release2, ok2 := svc.tryAcquireLeaderLock(ctx)
	assert.False(t, ok2)
	assert.Nil(t, release2)

	// 释放锁
	release1()

	// 释放后应能再次获取
	release3, ok3 := svc.tryAcquireLeaderLock(ctx)
	assert.True(t, ok3)
	assert.NotNil(t, release3)
	release3()
}

func TestScheduledReportService_LeaderLock_NoRedis(t *testing.T) {
	emailSvc := NewEmailServiceFromConfig("", "", "", "", "", "")
	svc := NewScheduledReportService(nil, nil, emailSvc)

	ctx := context.Background()

	// 无 Redis 时应允许运行
	release, ok := svc.tryAcquireLeaderLock(ctx)
	assert.True(t, ok)
	assert.Nil(t, release) // 无 Redis 时无 release 函数
}

func TestScheduledReportService_LastRunAt(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	emailSvc := NewEmailServiceFromConfig("", "", "", "", "", "")
	svc := NewScheduledReportService(nil, rdb, emailSvc)

	ctx := context.Background()
	now := time.Now().UTC()

	// 初始应返回零值
	last := svc.getLastRunAt(ctx, "daily_summary")
	assert.True(t, last.IsZero())

	// 设置后应能读取
	svc.setLastRunAt(ctx, "daily_summary", now)
	last = svc.getLastRunAt(ctx, "daily_summary")
	assert.False(t, last.IsZero())
	// 允许秒级精度损失
	assert.WithinDuration(t, now, last, 2*time.Second)
}

func TestScheduledReportService_StartStop(t *testing.T) {
	emailSvc := NewEmailServiceFromConfig("", "", "", "", "", "")
	svc := NewScheduledReportService(nil, nil, emailSvc)

	// 启动
	svc.Start()
	time.Sleep(100 * time.Millisecond)

	// 停止
	svc.Stop()

	// 验证不会 panic
	assert.NotNil(t, svc.stopCtx)
}

func TestScheduledReportService_NilService(t *testing.T) {
	// 验证 nil service 不 panic
	var svc *ScheduledReportService
	svc.Start() // 不应 panic
	svc.Stop()  // 不应 panic
}

func TestScheduledReportService_ParseSchedule(t *testing.T) {
	svc := &ScheduledReportService{}

	tests := []struct {
		name     string
		cfgVal   string
		rptType  string
		defVal   string
		expected string
	}{
		{
			name:     "explicit schedule",
			cfgVal:   `{"daily_summary_schedule": "0 8 * * *"}`,
			rptType:  "daily_summary",
			defVal:   defaultDailySummaryCron,
			expected: "0 8 * * *",
		},
		{
			name:     "missing key uses default",
			cfgVal:   `{"weekly_summary_schedule": "0 9 * * 1"}`,
			rptType:  "daily_summary",
			defVal:   defaultDailySummaryCron,
			expected: defaultDailySummaryCron,
		},
		{
			name:     "empty config uses default",
			cfgVal:   `{}`,
			rptType:  "daily_summary",
			defVal:   defaultDailySummaryCron,
			expected: defaultDailySummaryCron,
		},
		{
			name:     "empty schedule uses default",
			cfgVal:   `{"daily_summary_schedule": ""}`,
			rptType:  "daily_summary",
			defVal:   defaultDailySummaryCron,
			expected: defaultDailySummaryCron,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := svc.parseSchedule(tt.cfgVal, tt.rptType, tt.defVal)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestScheduledReportService_ParseReportRecipients(t *testing.T) {
	svc := &ScheduledReportService{}

	tests := []struct {
		name     string
		cfgVal   string
		expected []string
	}{
		{
			name:     "single recipient",
			cfgVal:   `{"report_recipients":["admin@example.com"]}`,
			expected: []string{"admin@example.com"},
		},
		{
			name:     "multiple recipients",
			cfgVal:   `{"report_recipients":["a@example.com","b@example.com"]}`,
			expected: []string{"a@example.com", "b@example.com"},
		},
		{
			name:     "empty array",
			cfgVal:   `{"report_recipients":[]}`,
			expected: nil,
		},
		{
			name:     "missing key",
			cfgVal:   `{"alert_recipients":["a@example.com"]}`,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := svc.parseReportRecipients(tt.cfgVal)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestScheduledReportService_ListScheduledReports(t *testing.T) {
	db := setupScheduledReportTestDB(t)
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	emailSvc := NewEmailServiceFromConfig("", "", "", "", "", "")
	svc := NewScheduledReportService(db, rdb, emailSvc)

	// 设置邮件通知配置
	cfg := `{"enabled":true,"alert_recipients":[],"report_recipients":["admin@example.com"],"min_severity":"","include_resolved":false,"daily_summary":true}`
	err = db.Exec(`INSERT INTO system_configs (key, value) VALUES (?, ?)`, "ops_email_notification_config", cfg).Error
	require.NoError(t, err)

	now := time.Now()
	reports := svc.listScheduledReports(context.Background(), now)
	// 应该有 4 个报表（daily, weekly, error_digest, account_health）
	assert.Equal(t, 4, len(reports))

	// 验证报表属性
	reportTypes := map[string]bool{}
	for _, r := range reports {
		reportTypes[r.ReportType] = true
		assert.NotEmpty(t, r.Name)
		assert.NotEmpty(t, r.Recipients)
		assert.Equal(t, "admin@example.com", r.Recipients[0])
	}

	assert.True(t, reportTypes["daily_summary"])
	assert.True(t, reportTypes["weekly_summary"])
	assert.True(t, reportTypes["error_digest"])
	assert.True(t, reportTypes["account_health"])
}

func TestScheduledReportService_ListScheduledReports_NoRecipients(t *testing.T) {
	db := setupScheduledReportTestDB(t)
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	emailSvc := NewEmailServiceFromConfig("", "", "", "", "", "")
	svc := NewScheduledReportService(db, rdb, emailSvc)

	// 配置没有报表收件人
	cfg := `{"enabled":true,"alert_recipients":[],"report_recipients":[],"min_severity":"","include_resolved":false,"daily_summary":true}`
	err = db.Exec(`INSERT INTO system_configs (key, value) VALUES (?, ?)`, "ops_email_notification_config", cfg).Error
	require.NoError(t, err)

	now := time.Now()
	reports := svc.listScheduledReports(context.Background(), now)
	// 没有收件人，不应生成报表
	assert.Empty(t, reports)
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func TestScheduledReportService_GenerateReport_UnknownType(t *testing.T) {
	db := setupScheduledReportTestDB(t)
	svc := &ScheduledReportService{db: db}
	report := &scheduledReport{
		Name:       "未知",
		ReportType: "unknown_type",
		TimeRange:  24 * time.Hour,
	}

	_, err := svc.generateReport(context.Background(), report, time.Now())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown report type")
}

func TestScheduledReportService_InvalidReport(t *testing.T) {
	db := setupScheduledReportTestDB(t)
	svc := &ScheduledReportService{db: db}

	// nil report
	_, err := svc.generateReport(context.Background(), nil, time.Now())
	assert.Error(t, err)

	// zero time range
	report := &scheduledReport{
		Name:       "日报",
		ReportType: "daily_summary",
		TimeRange:  0,
	}
	_, err = svc.generateReport(context.Background(), report, time.Now())
	assert.Error(t, err)
}

func TestScheduledReportService_ErrorDigest_NoErrors(t *testing.T) {
	db := setupScheduledReportTestDB(t)
	now := time.Now().UTC()

	// 全部成功的请求
	insertScheduledReportCallLogs(t, db, []map[string]interface{}{
		{"request_id": "n1", "ts": now.Add(-1 * time.Hour), "status": 200, "model": "gpt-4", "provider": "openai", "prompt_tokens": 100, "completion_tokens": 50, "total_tokens": 150, "duration_ms": 500},
		{"request_id": "n2", "ts": now.Add(-2 * time.Hour), "status": 200, "model": "claude", "provider": "anthropic", "prompt_tokens": 80, "completion_tokens": 20, "total_tokens": 100, "duration_ms": 250},
	})

	svc := &ScheduledReportService{db: db}
	report := &scheduledReport{
		Name:       "错误摘要",
		ReportType: "error_digest",
		TimeRange:  24 * time.Hour,
	}

	content, err := svc.generateReport(context.Background(), report, now)
	require.NoError(t, err)
	// 应该包含 "No provider/model" 信息
	assert.Contains(t, content, "No provider/model")
}

func TestTruncateTo(t *testing.T) {
	assert.Equal(t, "hello", truncateTo("hello", 10))
	assert.Equal(t, "hello...", truncateTo("hello world", 8))
	assert.Equal(t, "", truncateTo("", 5))
}

// TestScheduledReportService_DefaultCronSchedule 验证默认 cron 调度可解析
func TestScheduledReportService_DefaultCronSchedule(t *testing.T) {
	schedules := []string{
		defaultDailySummaryCron,
		defaultWeeklySummaryCron,
		defaultErrorDigestCron,
		defaultAccountHealthCron,
	}

	for _, spec := range schedules {
		t.Run(spec, func(t *testing.T) {
			_, err := scheduledReportCronParser.Parse(spec)
			assert.NoError(t, err, "cron spec %q should be valid", spec)
		})
	}
}

// TestScheduledReportService_LeaderLock_ConcurrentInstances 测试两个实例互斥
func TestScheduledReportService_LeaderLock_ConcurrentInstances(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	emailSvc := NewEmailServiceFromConfig("", "", "", "", "", "")

	svc1 := NewScheduledReportService(nil, rdb, emailSvc)
	svc1.instanceID = "instance-1"
	svc2 := NewScheduledReportService(nil, rdb, emailSvc)
	svc2.instanceID = "instance-2"

	ctx := context.Background()

	// 实例 1 获取锁
	release1, ok1 := svc1.tryAcquireLeaderLock(ctx)
	assert.True(t, ok1)
	assert.NotNil(t, release1)

	// 实例 2 无法获取锁
	release2, ok2 := svc2.tryAcquireLeaderLock(ctx)
	assert.False(t, ok2)
	assert.Nil(t, release2)

	// 实例 1 释放锁
	release1()

	// 实例 2 现在可以获取锁
	release3, ok3 := svc2.tryAcquireLeaderLock(ctx)
	assert.True(t, ok3)
	assert.NotNil(t, release3)
	release3()
}

// TestScheduledReportService_SummaryWithEmptyData 测试空数据场景
func TestScheduledReportService_SummaryWithEmptyData(t *testing.T) {
	db := setupScheduledReportTestDB(t)
	svc := &ScheduledReportService{db: db}
	report := &scheduledReport{
		Name:       "日报",
		ReportType: "daily_summary",
		TimeRange:  24 * time.Hour,
	}

	content, err := svc.generateReport(context.Background(), report, time.Now())
	require.NoError(t, err)
	assert.Contains(t, content, "Total:    0")
}

// TestScheduledReportService_AccountHealth_EmptyData 测试空数据场景
func TestScheduledReportService_AccountHealth_EmptyData(t *testing.T) {
	db := setupScheduledReportTestDB(t)
	svc := &ScheduledReportService{db: db}
	report := &scheduledReport{
		Name:       "账号健康",
		ReportType: "account_health",
		TimeRange:  24 * time.Hour,
	}

	content, err := svc.generateReport(context.Background(), report, time.Now())
	require.NoError(t, err)
	assert.Contains(t, content, "账号健康")
	assert.Contains(t, content, "Provider Availability")
}

// TestScheduledReportService_WeeklySummary 测试周报（7天时间范围）
func TestScheduledReportService_WeeklySummary(t *testing.T) {
	db := setupScheduledReportTestDB(t)
	now := time.Now().UTC()

	// 插入最近 7 天的数据
	insertScheduledReportCallLogs(t, db, []map[string]interface{}{
		{"request_id": "w1", "ts": now.Add(-24 * time.Hour), "status": 200, "model": "gpt-4", "provider": "openai", "prompt_tokens": 500, "completion_tokens": 200, "total_tokens": 700, "duration_ms": 600},
		{"request_id": "w2", "ts": now.Add(-72 * time.Hour), "status": 200, "model": "claude", "provider": "anthropic", "prompt_tokens": 300, "completion_tokens": 100, "total_tokens": 400, "duration_ms": 400},
		{"request_id": "w3", "ts": now.Add(-120 * time.Hour), "status": 500, "model": "gpt-4", "provider": "openai", "prompt_tokens": 100, "completion_tokens": 0, "total_tokens": 100, "duration_ms": 300},
	})

	svc := &ScheduledReportService{db: db}
	report := &scheduledReport{
		Name:       "周报",
		ReportType: "weekly_summary",
		TimeRange:  7 * 24 * time.Hour,
	}

	content, err := svc.generateReport(context.Background(), report, now)
	require.NoError(t, err)
	assert.Contains(t, content, "周报")
	assert.Contains(t, content, "Total:    3")
	assert.Contains(t, content, "Success:  2")
	assert.Contains(t, content, "Errors:   1")
}

// 确保 fmt import 被使用
var _ = fmt.Sprintf