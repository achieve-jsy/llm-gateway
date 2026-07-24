package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

const (
	// Redis leader lock key for scheduled reports
	scheduledReportLockKey = "ops:scheduled_reports:leader"
	// Leader lock TTL
	scheduledReportLockTTL = 120 * time.Second
	// Tick interval for checking schedules
	scheduledReportTickInterval = 1 * time.Minute
	// Redis key prefix for last run timestamps
	scheduledReportLastRunKeyPrefix = "ops:scheduled_reports:last_run:"

	// Default cron schedules
	defaultDailySummaryCron  = "0 9 * * *"   // 每天 9:00
	defaultWeeklySummaryCron = "0 9 * * 1"   // 每周一 9:00
	defaultErrorDigestCron   = "0 */6 * * *" // 每 6 小时
	defaultAccountHealthCron = "0 */3 * * *" // 每 3 小时
)

// Cron parser that includes seconds field for flexibility
var scheduledReportCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// Lua script to release lock safely (only if we own it)
var scheduledReportReleaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`)

// ScheduledReportService 定时报表服务，使用 robfig/cron 调度 + Redis leader lock。
type ScheduledReportService struct {
	db           *gorm.DB
	rdb          *redis.Client
	emailSvc     *EmailService
	settingsRepo interface {
		GetValue(key string) (string, error)
	}

	instanceID string

	startOnce sync.Once
	stopOnce  sync.Once
	stopCtx   context.Context
	stop      context.CancelFunc
	wg        sync.WaitGroup
}

// NewScheduledReportService 创建定时报表服务。
func NewScheduledReportService(db *gorm.DB, rdb *redis.Client, emailSvc *EmailService) *ScheduledReportService {
	return &ScheduledReportService{
		db:         db,
		rdb:        rdb,
		emailSvc:   emailSvc,
		instanceID: uuid.NewString(),
	}
}

// Start 启动定时报表服务。
func (s *ScheduledReportService) Start() {
	if s == nil {
		return
	}
	s.startOnce.Do(func() {
		s.stopCtx, s.stop = context.WithCancel(context.Background())
		s.wg.Add(1)
		go s.run()
		slog.Info("scheduled_report: started", "instance_id", s.instanceID)
	})
}

// Stop 停止定时报表服务。
func (s *ScheduledReportService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		if s.stop != nil {
			s.stop()
		}
	})
	s.wg.Wait()
	slog.Info("scheduled_report: stopped")
}

func (s *ScheduledReportService) run() {
	defer s.wg.Done()

	ticker := time.NewTicker(scheduledReportTickInterval)
	defer ticker.Stop()

	// 启动后立即检查一次
	s.runOnce()

	for {
		select {
		case <-ticker.C:
			s.runOnce()
		case <-s.stopCtx.Done():
			return
		}
	}
}

func (s *ScheduledReportService) runOnce() {
	if s.db == nil {
		return
	}
	ctx, cancel := context.WithTimeout(s.stopCtx, 60*time.Second)
	defer cancel()

	// 尝试获取 leader lock
	release, ok := s.tryAcquireLeaderLock(ctx)
	if !ok {
		return
	}
	if release != nil {
		defer release()
	}

	now := time.Now()
	reports := s.listScheduledReports(ctx, now)
	if len(reports) == 0 {
		return
	}

	for _, report := range reports {
		if report == nil {
			continue
		}
		if report.NextRunAt.After(now) {
			continue
		}

		// 标记已执行（防止重复触发）
		s.setLastRunAt(ctx, report.ReportType, now)

		content, err := s.generateReport(ctx, report, now)
		if err != nil {
			slog.Error("scheduled_report: generate failed",
				"type", report.ReportType, "error", err)
			continue
		}
		if strings.TrimSpace(content) == "" {
			continue
		}

		subject := fmt.Sprintf("[LLM Gateway] %s - %s", report.Name, formatNotificationTime(now))
		if err := s.emailSvc.SendAlertEmail(report.Recipients, subject, content); err != nil {
			slog.Error("scheduled_report: send email failed",
				"type", report.ReportType, "error", err)
		}
	}
}

type scheduledReport struct {
	Name       string
	ReportType string
	Schedule   string
	TimeRange  time.Duration
	Recipients []string
	NextRunAt  time.Time
}

func (s *ScheduledReportService) listScheduledReports(ctx context.Context, now time.Time) []*scheduledReport {
	// 读取邮件通知配置
	cfgVal, err := s.getEmailNotificationConfig(ctx)
	if err != nil || cfgVal == "" {
		return nil
	}

	// 简单解析 JSON 获取 recipients
	reportRecipients := s.parseReportRecipients(cfgVal)
	if len(reportRecipients) == 0 {
		// 没有报表收件人，不发送报表
		return nil
	}

	type reportDef struct {
		name      string
		kind      string
		timeRange time.Duration
		schedule  string
	}

	defs := []reportDef{
		{name: "日报", kind: "daily_summary", timeRange: 24 * time.Hour, schedule: s.parseSchedule(cfgVal, "daily_summary", defaultDailySummaryCron)},
		{name: "周报", kind: "weekly_summary", timeRange: 7 * 24 * time.Hour, schedule: s.parseSchedule(cfgVal, "weekly_summary", defaultWeeklySummaryCron)},
		{name: "错误摘要", kind: "error_digest", timeRange: 24 * time.Hour, schedule: s.parseSchedule(cfgVal, "error_digest", defaultErrorDigestCron)},
		{name: "账号健康", kind: "account_health", timeRange: 24 * time.Hour, schedule: s.parseSchedule(cfgVal, "account_health", defaultAccountHealthCron)},
	}

	out := make([]*scheduledReport, 0, len(defs))
	for _, d := range defs {
		spec := strings.TrimSpace(d.schedule)
		if spec == "" {
			continue
		}
		sched, err := scheduledReportCronParser.Parse(spec)
		if err != nil {
			slog.Warn("scheduled_report: invalid cron spec",
				"type", d.kind, "spec", spec, "error", err)
			continue
		}

		lastRun := s.getLastRunAt(ctx, d.kind)
		base := lastRun
		if base.IsZero() {
			// 首次启动，允许立即触发
			base = now.Add(-1 * time.Minute)
		}
		next := sched.Next(base)
		if next.IsZero() {
			continue
		}

		out = append(out, &scheduledReport{
			Name:       d.name,
			ReportType: d.kind,
			Schedule:   spec,
			TimeRange:  d.timeRange,
			Recipients: reportRecipients,
			NextRunAt:  next,
		})
	}

	return out
}

// parseSchedule 从 JSON 配置中解析某类报表的 cron schedule。
// 配置格式: {"daily_summary_schedule": "0 9 * * *", ...}
func (s *ScheduledReportService) parseSchedule(cfgVal string, reportType string, defaultSchedule string) string {
	key := fmt.Sprintf(`"%s_schedule": "`, reportType)
	idx := strings.Index(cfgVal, key)
	if idx < 0 {
		return defaultSchedule
	}
	start := idx + len(key)
	end := strings.Index(cfgVal[start:], `"`)
	if end < 0 {
		return defaultSchedule
	}
	schedule := cfgVal[start : start+end]
	if strings.TrimSpace(schedule) == "" {
		return defaultSchedule
	}
	return schedule
}

// parseReportRecipients 从 JSON 配置中解析报表收件人列表。
func (s *ScheduledReportService) parseReportRecipients(cfgVal string) []string {
	key := `"report_recipients"`
	idx := strings.Index(cfgVal, key)
	if idx < 0 {
		return nil
	}

	// 找到数组开始位置
	arrStart := strings.Index(cfgVal[idx:], "[")
	if arrStart < 0 {
		return nil
	}
	arrStart += idx
	arrEnd := strings.Index(cfgVal[arrStart:], "]")
	if arrEnd < 0 {
		return nil
	}
	arrStr := cfgVal[arrStart : arrStart+arrEnd+1]

	// 解析 JSON 数组
	var recipients []string
	// 简单解析: 去掉 [ ] 和引号
	arrStr = strings.TrimSpace(arrStr)
	if arrStr == "[]" {
		return nil
	}
	arrStr = arrStr[1 : len(arrStr)-1] // 去掉 [ ]
	parts := strings.Split(arrStr, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, `"`)
		if p != "" {
			recipients = append(recipients, p)
		}
	}
	return recipients
}

func (s *ScheduledReportService) getEmailNotificationConfig(ctx context.Context) (string, error) {
	if s.settingsRepo != nil {
		return s.settingsRepo.GetValue("ops_email_notification_config")
	}
	if s.db == nil {
		return "", nil
	}
	// 直接从 DB 查询
	val := ""
	err := s.db.WithContext(ctx).Raw(
		"SELECT value FROM system_configs WHERE key = ?", "ops_email_notification_config",
	).Scan(&val).Error
	return val, err
}

func (s *ScheduledReportService) tryAcquireLeaderLock(ctx context.Context) (func(), bool) {
	if s.rdb == nil {
		// 无 Redis 时允许单实例运行
		slog.Warn("scheduled_report: redis not configured, running without distributed lock")
		return nil, true
	}

	ok, err := s.rdb.SetNX(ctx, scheduledReportLockKey, s.instanceID, scheduledReportLockTTL).Result()
	if err != nil {
		slog.Warn("scheduled_report: leader lock SetNX failed, skipping this cycle", "error", err)
		return nil, false
	}
	if !ok {
		return nil, false
	}

	return func() {
		_, _ = scheduledReportReleaseScript.Run(ctx, s.rdb, []string{scheduledReportLockKey}, s.instanceID).Result()
	}, true
}

func (s *ScheduledReportService) getLastRunAt(ctx context.Context, reportType string) time.Time {
	if s.rdb == nil {
		return time.Time{}
	}
	key := scheduledReportLastRunKeyPrefix + reportType
	raw, err := s.rdb.Get(ctx, key).Result()
	if err != nil || raw == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		// 尝试解析为 Unix 时间戳
		var unix int64
		if _, err := fmt.Sscanf(raw, "%d", &unix); err == nil && unix > 0 {
			return time.Unix(unix, 0)
		}
		return time.Time{}
	}
	return t
}

func (s *ScheduledReportService) setLastRunAt(ctx context.Context, reportType string, t time.Time) {
	if s.rdb == nil {
		return
	}
	key := scheduledReportLastRunKeyPrefix + reportType
	_ = s.rdb.Set(ctx, key, t.UTC().Format(time.RFC3339), 14*24*time.Hour).Err()
}

// generateReport 生成报表内容。
func (s *ScheduledReportService) generateReport(ctx context.Context, report *scheduledReport, now time.Time) (string, error) {
	if report == nil || report.TimeRange <= 0 {
		return "", fmt.Errorf("invalid report")
	}

	end := now.UTC()
	start := end.Add(-report.TimeRange)

	switch report.ReportType {
	case "daily_summary", "weekly_summary":
		return s.generateSummaryReport(ctx, report.Name, start, end)
	case "error_digest":
		return s.generateErrorDigestReport(ctx, report.Name, start, end)
	case "account_health":
		return s.generateAccountHealthReport(ctx, report.Name, start, end)
	default:
		return "", fmt.Errorf("unknown report type: %s", report.ReportType)
	}
}

// generateSummaryReport 生成日报/周报（请求量、成功率、错误率、token 消耗）。
func (s *ScheduledReportService) generateSummaryReport(ctx context.Context, name string, start, end time.Time) (string, error) {
	var result struct {
		TotalRequests    int64   `gorm:"column:total_requests"`
		SuccessCount     int64   `gorm:"column:success_count"`
		ErrorCount       int64   `gorm:"column:error_count"`
		ErrorRate        float64 `gorm:"column:error_rate"`
		TotalTokens      int64   `gorm:"column:total_tokens"`
		PromptTokens     int64   `gorm:"column:prompt_tokens"`
		CompletionTokens int64   `gorm:"column:completion_tokens"`
		AvgDurationMs    float64 `gorm:"column:avg_duration_ms"`
	}

	err := s.db.WithContext(ctx).Raw(`
		SELECT
			COUNT(*) as total_requests,
			SUM(CASE WHEN status BETWEEN 200 AND 299 THEN 1 ELSE 0 END) as success_count,
			SUM(CASE WHEN status >= 400 THEN 1 ELSE 0 END) as error_count,
			ROUND(CAST(SUM(CASE WHEN status >= 400 THEN 1 ELSE 0 END) AS REAL) / NULLIF(COUNT(*), 0) * 100, 2) as error_rate,
			COALESCE(SUM(total_tokens), 0) as total_tokens,
			COALESCE(SUM(prompt_tokens), 0) as prompt_tokens,
			COALESCE(SUM(completion_tokens), 0) as completion_tokens,
			ROUND(AVG(CAST(duration_ms AS REAL)), 2) as avg_duration_ms
		FROM call_logs
		WHERE ts >= ? AND ts < ?
	`, start, end).Scan(&result).Error

	if err != nil {
		return "", fmt.Errorf("query summary: %w", err)
	}

	successRate := 0.0
	if result.TotalRequests > 0 {
		successRate = float64(result.SuccessCount) / float64(result.TotalRequests) * 100
	}

	return fmt.Sprintf(`%s
================================
%s

请求量：
  总请求：%d
  成功：%d（%.2f%%）
  失败：%d（%.2f%%）

Token 消耗：
  合计：%d
  输入：%d
  输出：%d

平均延迟：%.2f ms
`,
		name,
		reportRangeLine(start, end),
		result.TotalRequests,
		result.SuccessCount, successRate,
		result.ErrorCount, result.ErrorRate,
		result.TotalTokens,
		result.PromptTokens,
		result.CompletionTokens,
		result.AvgDurationMs,
	), nil
}

// generateErrorDigestReport 生成错误摘要（error_rate > 5% 的 provider/model）。
func (s *ScheduledReportService) generateErrorDigestReport(ctx context.Context, name string, start, end time.Time) (string, error) {
	type errorRow struct {
		Provider  string  `gorm:"column:provider"`
		Model     string  `gorm:"column:model"`
		Total     int64   `gorm:"column:total"`
		Errors    int64   `gorm:"column:errors"`
		ErrorRate float64 `gorm:"column:error_rate"`
	}

	var rows []errorRow
	err := s.db.WithContext(ctx).Raw(`
		SELECT
			COALESCE(NULLIF(provider, ''), '未知渠道') as provider,
			model,
			COUNT(*) as total,
			SUM(CASE WHEN status >= 400 THEN 1 ELSE 0 END) as errors,
			ROUND(CAST(SUM(CASE WHEN status >= 400 THEN 1 ELSE 0 END) AS REAL) / NULLIF(COUNT(*), 0) * 100, 2) as error_rate
		FROM call_logs
		WHERE ts >= ? AND ts < ?
		GROUP BY provider, model
		HAVING CAST(SUM(CASE WHEN status >= 400 THEN 1 ELSE 0 END) AS REAL) / NULLIF(COUNT(*), 0) > 0.05
		ORDER BY error_rate DESC
		LIMIT 50
	`, start, end).Scan(&rows).Error

	if err != nil {
		return "", fmt.Errorf("query error digest: %w", err)
	}

	if len(rows) == 0 {
		return fmt.Sprintf("%s\n================================\n%s\n\n统计区间内没有错误率超过 5%% 的渠道 / 模型组合。\n",
			name, reportRangeLine(start, end)), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s\n================================\n%s\n\n错误率超过 5%% 的渠道 / 模型：\n\n",
		name, reportRangeLine(start, end)))

	for _, row := range rows {
		sb.WriteString(fmt.Sprintf("- %s / %s：总请求 %d，错误 %d，错误率 %.2f%%\n",
			truncateTo(row.Provider, 20),
			truncateTo(row.Model, 30),
			row.Total,
			row.Errors,
			row.ErrorRate,
		))
	}

	return sb.String(), nil
}

// generateAccountHealthReport 生成账号健康报表（各 provider 可用率）。
func (s *ScheduledReportService) generateAccountHealthReport(ctx context.Context, name string, start, end time.Time) (string, error) {
	type healthRow struct {
		Provider string  `gorm:"column:provider"`
		Total    int64   `gorm:"column:total"`
		Success  int64   `gorm:"column:success"`
		Rate     float64 `gorm:"column:rate"`
	}

	var rows []healthRow
	err := s.db.WithContext(ctx).Raw(`
		SELECT
			COALESCE(NULLIF(provider, ''), '未知渠道') as provider,
			COUNT(*) as total,
			SUM(CASE WHEN status BETWEEN 200 AND 299 THEN 1 ELSE 0 END) as success,
			ROUND(CAST(SUM(CASE WHEN status BETWEEN 200 AND 299 THEN 1 ELSE 0 END) AS REAL) / NULLIF(COUNT(*), 0) * 100, 2) as rate
		FROM call_logs
		WHERE ts >= ? AND ts < ?
		GROUP BY provider
		ORDER BY rate ASC
	`, start, end).Scan(&rows).Error

	if err != nil {
		return "", fmt.Errorf("query account health: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s\n================================\n%s\n\n渠道可用率（由低到高）：\n\n",
		name, reportRangeLine(start, end)))

	for _, row := range rows {
		sb.WriteString(fmt.Sprintf("- %s：总请求 %d，成功 %d，可用率 %.2f%%\n",
			truncateTo(row.Provider, 20),
			row.Total,
			row.Success,
			row.Rate,
		))
	}

	return sb.String(), nil
}

// reportRangeLine 渲染报表统计区间。SQL 用的是 UTC 窗口，这里统一转北京时间展示，
// 避免收件人看到 2026-09-17 13:00 却不知道那是 UTC 还是本地时间。
func reportRangeLine(start, end time.Time) string {
	return fmt.Sprintf("统计区间：%s ~ %s（北京时间）",
		formatNotificationTime(start), formatNotificationTime(end))
}

func truncateTo(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "…"
}
