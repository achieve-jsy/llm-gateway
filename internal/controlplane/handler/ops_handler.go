package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// OpsHandler 运维看板 API Handler
type OpsHandler struct {
	db               *gorm.DB
	rdb              *redis.Client
	metricsCollector *service.SystemMetricsCollector
	settingsRepo     *repository.SettingsRepo
}

// NewOpsHandler 创建 OpsHandler 实例
func NewOpsHandler(db *gorm.DB, rdb *redis.Client, mc *service.SystemMetricsCollector) *OpsHandler {
	return &OpsHandler{
		db:               db,
		rdb:              rdb,
		metricsCollector: mc,
		settingsRepo:     repository.NewSettingsRepo(db),
	}
}

// timeRangeToInterval 将时间范围参数转换为 SQL INTERVAL
func timeRangeToInterval(tr string) string {
	switch tr {
	case "5m":
		return "5 minutes"
	case "30m":
		return "30 minutes"
	case "6h":
		return "6 hours"
	case "24h":
		return "24 hours"
	default:
		return "1 hour"
	}
}

// bucketTruncExpr 返回用于 GROUP BY 时间分桶的 SQL 表达式
func bucketTruncExpr(timeRange string) string {
	switch timeRange {
	case "5m":
		return "date_trunc('minute', ts)"
	case "30m":
		return "to_timestamp(floor((extract('epoch' from ts)::numeric / 300)) * 300)"
	case "6h":
		return "to_timestamp(floor((extract('epoch' from ts)::numeric / 1800)) * 1800)"
	case "24h":
		return "date_trunc('hour', ts)"
	default:
		return "to_timestamp(floor((extract('epoch' from ts)::numeric / 300)) * 300)"
	}
}

func bucketSeconds(timeRange string) float64 {
	switch timeRange {
	case "5m":
		return 60
	case "30m":
		return 300
	case "6h":
		return 1800
	case "24h":
		return 3600
	default:
		return 300
	}
}

func bucketName(timeRange string) string {
	switch timeRange {
	case "5m":
		return "1 minute"
	case "30m":
		return "5 minutes"
	case "6h":
		return "30 minutes"
	case "24h":
		return "1 hour"
	default:
		return "5 minutes"
	}
}

// OpsDashboardResponse 运维看板总览响应
type OpsDashboardResponse struct {
	Overview  OpsOverview `json:"overview"`
	Timestamp time.Time   `json:"timestamp"`
}

// OpsOverview 总览数据
type OpsOverview struct {
	HealthScore   int                       `json:"health_score"`
	SystemMetrics *service.SystemMetrics    `json:"system_metrics"`
	Traffic       OpsTraffic                `json:"traffic"`
	SLA           OpsSLA                    `json:"sla"`
	Latency       OpsLatency                `json:"latency"`
	Errors        OpsErrors                 `json:"errors"`
	Jobs          []model.AlertJobHeartbeat `json:"jobs"`
}

// OpsTraffic 流量指标
type OpsTraffic struct {
	TotalRequests int64   `json:"total_requests"`
	TotalTokens   int64   `json:"total_tokens"`
	AvgQPS        float64 `json:"avg_qps"`
	AvgTPS        float64 `json:"avg_tps"`
	CurrentQPS    float64 `json:"current_qps"`
	CurrentTPS    float64 `json:"current_tps"`
	PeakQPS       float64 `json:"peak_qps"`
	PeakTPS       float64 `json:"peak_tps"`
}

// OpsSLA SLA 指标
type OpsSLA struct {
	SuccessRate   float64 `json:"success_rate"`
	ErrorRate     float64 `json:"error_rate"`
	SLAErrorCount int64   `json:"sla_error_count"`
}

// OpsLatency 延迟指标
type OpsLatency struct {
	DurationP50Ms int `json:"duration_p50_ms"`
	DurationP90Ms int `json:"duration_p90_ms"`
	DurationP95Ms int `json:"duration_p95_ms"`
	DurationP99Ms int `json:"duration_p99_ms"`
	DurationAvgMs int `json:"duration_avg_ms"`
	DurationMaxMs int `json:"duration_max_ms"`
	TTFTP50Ms     int `json:"ttft_p50_ms"`
	TTFTP90Ms     int `json:"ttft_p90_ms"`
	TTFTP95Ms     int `json:"ttft_p95_ms"`
	TTFTP99Ms     int `json:"ttft_p99_ms"`
	TTFTAvgMs     int `json:"ttft_avg_ms"`
	TTFTMaxMs     int `json:"ttft_max_ms"`
}

// OpsErrors 错误指标
type OpsErrors struct {
	RequestErrors             int64 `json:"request_errors"`
	UpstreamErrors            int64 `json:"upstream_errors"`
	BusinessErrors            int64 `json:"business_errors"`
	UpstreamErrorsExcl429_529 int64 `json:"upstream_errors_excl_429_529"`
	RateLimit429_529          int64 `json:"rate_limit_429_529"`
}

// OpsFilter 运维看板通用过滤参数
type OpsFilter struct {
	TimeRange     string
	Start         string // B12: 自定义时间范围起始(ISO 8601)
	End           string // B12: 自定义时间范围结束(ISO 8601)
	GroupID       string // 模型组 ID
	Provider      string // 提供商名称
	Model         string // 模型名称
	UserID        string // gap#10: 用户 ID
	KeyID         string // gap#10: API Key ID
	Account       string // gap#10: 账户名(对应 provider)
	MaxDurationMs string // gap#10: 最大耗时过滤(duration_ms <= ?)
	Endpoint      string // 入站端点路径，如 /v1/chat/completions, /v1/embeddings, /v1/rerank
}

// buildFilterClause 根据 OpsFilter 生成 SQL WHERE 子句和参数。
// B12/WP4: 支持自定义时间范围(Start/End),使用 BETWEEN 替代相对时间。
func (h *OpsHandler) buildFilterClause(ctx context.Context, filter OpsFilter, timeInterval string) (string, []interface{}) {
	var clause string
	var args []interface{}

	// B12: 自定义时间范围优先(Custom)
	if filter.TimeRange == "custom" && filter.Start != "" && filter.End != "" {
		clause = "ts BETWEEN ? AND ?"
		args = append(args, filter.Start, filter.End)
	} else {
		clause = "ts > NOW() - INTERVAL '" + timeInterval + "'"
	}

	if filter.GroupID != "" {
		// call_logs 没有 group_id 列, 只有 group_name. 需要从 model_groups 查询
		var groupName string
		if err := h.db.WithContext(ctx).Raw(
			"SELECT name FROM model_groups WHERE id = ?", filter.GroupID,
		).Scan(&groupName).Error; err == nil && groupName != "" {
			clause += " AND group_name = ?"
			args = append(args, groupName)
		}
	}
	if filter.Provider != "" {
		clause += " AND provider = ?"
		args = append(args, filter.Provider)
	}
	if filter.Model != "" {
		clause += " AND model = ?"
		args = append(args, filter.Model)
	}
	// gap#10: 补 user/api-key/account/max_duration 过滤维度
	if filter.UserID != "" {
		clause += " AND user_id = ?"
		args = append(args, filter.UserID)
	}
	if filter.KeyID != "" {
		clause += " AND key_id = ?"
		args = append(args, filter.KeyID)
	}
	if filter.Account != "" {
		// Account 是 provider 列的别名筛选；当 Provider 同时设置时，
		// Account 优先（避免 SQL 出现两个 provider = ? 导致数据恒为 0）。
		if filter.Provider == "" {
			clause += " AND provider = ?"
			args = append(args, filter.Account)
		}
	}
	if filter.MaxDurationMs != "" {
		clause += " AND duration_ms <= ?"
		args = append(args, filter.MaxDurationMs)
	}
	if filter.Endpoint != "" {
		clause += " AND inbound_endpoint = ?"
		args = append(args, filter.Endpoint)
	}
	return clause, args
}

// buildOpsOverview 构造完整运维看板概览数据（供 GetDashboard 和 GetSnapshotV2 共用）
func (h *OpsHandler) buildOpsOverview(ctx context.Context, timeRange string) (OpsOverview, error) {
	return h.buildOpsOverviewWithFilter(ctx, OpsFilter{TimeRange: timeRange})
}

// buildOpsOverviewWithFilter 带过滤条件的概览数据构造
func (h *OpsHandler) buildOpsOverviewWithFilter(ctx context.Context, filter OpsFilter) (OpsOverview, error) {
	interval := timeRangeToInterval(filter.TimeRange)
	whereClause, args := h.buildFilterClause(ctx, filter, interval)

	// 1. 流量统计
	var traffic struct {
		TotalRequests             int64   `gorm:"column:total_requests"`
		TotalTokens               int64   `gorm:"column:total_tokens"`
		SuccessCount              int64   `gorm:"column:success_count"`
		RequestErrors             int64   `gorm:"column:request_errors"`
		UpstreamErrors            int64   `gorm:"column:upstream_errors"`
		BusinessErrors            int64   `gorm:"column:business_errors"`
		UpstreamErrorsExcl429_529 int64   `gorm:"column:upstream_errors_excl_429_529"`
		RateLimit429_529          int64   `gorm:"column:rate_limit_429_529"`
		DurationAvgMs             float64 `gorm:"column:duration_avg_ms"`
		TTFTAvgMs                 float64 `gorm:"column:ttft_avg_ms"`
	}

	sql := fmt.Sprintf(`
		SELECT
			COUNT(*)::bigint as total_requests,
			COALESCE(SUM(total_tokens), 0)::bigint as total_tokens,
			COUNT(*) FILTER (WHERE status = 200)::bigint as success_count,
			COUNT(*) FILTER (WHERE status >= 400 AND status < 500)::bigint as request_errors,
			COUNT(*) FILTER (WHERE status >= 500)::bigint as upstream_errors,
			COUNT(*) FILTER (WHERE status = 429)::bigint as business_errors,
			COUNT(*) FILTER (WHERE status >= 500 AND status != 529)::bigint as upstream_errors_excl_429_529,
			COUNT(*) FILTER (WHERE status IN (429, 529))::bigint as rate_limit_429_529,
			COALESCE(AVG(duration_ms), 0)::float as duration_avg_ms,
			COALESCE(AVG(first_token_ms), 0)::float as ttft_avg_ms
		FROM call_logs
		WHERE %s
	`, whereClause)
	if err := h.db.Raw(sql, args...).Scan(&traffic).Error; err != nil {
		return OpsOverview{}, fmt.Errorf("traffic: %w", err)
	}

	// 2. 延迟百分位
	var latency struct {
		DurationP50Ms int `gorm:"column:duration_p50"`
		DurationP90Ms int `gorm:"column:duration_p90"`
		DurationP95Ms int `gorm:"column:duration_p95"`
		DurationP99Ms int `gorm:"column:duration_p99"`
		DurationMaxMs int `gorm:"column:duration_max"`
		TTFTP50Ms     int `gorm:"column:ttft_p50"`
		TTFTP90Ms     int `gorm:"column:ttft_p90"`
		TTFTP95Ms     int `gorm:"column:ttft_p95"`
		TTFTP99Ms     int `gorm:"column:ttft_p99"`
		TTFTMaxMs     int `gorm:"column:ttft_max"`
	}

	latencySQL := fmt.Sprintf(`
		SELECT
			COALESCE(PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY duration_ms), 0)::int as duration_p50,
			COALESCE(PERCENTILE_CONT(0.9) WITHIN GROUP (ORDER BY duration_ms), 0)::int as duration_p90,
			COALESCE(PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY duration_ms), 0)::int as duration_p95,
			COALESCE(PERCENTILE_CONT(0.99) WITHIN GROUP (ORDER BY duration_ms), 0)::int as duration_p99,
			COALESCE(MAX(duration_ms), 0)::int as duration_max,
			COALESCE(PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY COALESCE(first_token_ms, 0)), 0)::int as ttft_p50,
			COALESCE(PERCENTILE_CONT(0.9) WITHIN GROUP (ORDER BY COALESCE(first_token_ms, 0)), 0)::int as ttft_p90,
			COALESCE(PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY COALESCE(first_token_ms, 0)), 0)::int as ttft_p95,
			COALESCE(PERCENTILE_CONT(0.99) WITHIN GROUP (ORDER BY COALESCE(first_token_ms, 0)), 0)::int as ttft_p99,
			COALESCE(MAX(COALESCE(first_token_ms, 0)), 0)::int as ttft_max
		FROM call_logs
		WHERE %s
	`, whereClause)
	if err := h.db.Raw(latencySQL, args...).Scan(&latency).Error; err != nil {
		return OpsOverview{}, fmt.Errorf("latency: %w", err)
	}

	// 3. 实时 QPS/TPS（Redis 最近 1 分钟）
	currentQPS, currentTPS := 0.0, 0.0
	if h.rdb != nil {
		minKey := time.Now().Format("200601021504")
		qpsC, _ := h.rdb.Get(ctx, "ops:qps:"+minKey).Int64()
		tpsC, _ := h.rdb.Get(ctx, "ops:tps:"+minKey).Int64()
		currentQPS = float64(qpsC) / 60.0
		currentTPS = float64(tpsC) / 60.0
	}

	// 4. 平均 + 峰值 QPS/TPS
	var windowSeconds float64
	switch filter.TimeRange {
	case "5m":
		windowSeconds = 300
	case "30m":
		windowSeconds = 1800
	case "6h":
		windowSeconds = 21600
	case "24h":
		windowSeconds = 86400
	default:
		windowSeconds = 3600
	}
	avgQPS := float64(traffic.TotalRequests) / windowSeconds
	avgTPS := float64(traffic.TotalTokens) / windowSeconds

	peakQPS, peakTPS := 0.0, 0.0
	if h.rdb != nil {
		for iter := h.rdb.Scan(ctx, 0, "ops:qps:*", 100).Iterator(); iter.Next(ctx); {
			val, _ := h.rdb.Get(ctx, iter.Val()).Int64()
			if q := float64(val) / 60.0; q > peakQPS {
				peakQPS = q
			}
		}
		for iter := h.rdb.Scan(ctx, 0, "ops:tps:*", 100).Iterator(); iter.Next(ctx); {
			val, _ := h.rdb.Get(ctx, iter.Val()).Int64()
			if t := float64(val) / 60.0; t > peakTPS {
				peakTPS = t
			}
		}
	}
	if peakQPS < currentQPS {
		peakQPS = currentQPS
	}
	if peakTPS < currentTPS {
		peakTPS = currentTPS
	}

	// 5. SLA
	successRate := 0.0
	errorRate := 0.0
	if traffic.TotalRequests > 0 {
		successRate = float64(traffic.SuccessCount) / float64(traffic.TotalRequests)
		errorRate = 1.0 - successRate
	}

	// 6. 健康评分
	sm := h.metricsCollector.Get()
	healthScore := calculateHealthScore(successRate, sm.MemoryUsagePercent, sm.CPUUsagePercent, sm.GoroutineCount)

	// 7. 后台任务心跳
	jobs := make([]model.AlertJobHeartbeat, 0)
	h.db.Order("name").Limit(10).Find(&jobs)

	return OpsOverview{
		HealthScore:   healthScore,
		SystemMetrics: &sm,
		Traffic: OpsTraffic{
			TotalRequests: traffic.TotalRequests,
			TotalTokens:   traffic.TotalTokens,
			AvgQPS:        avgQPS,
			AvgTPS:        avgTPS,
			CurrentQPS:    currentQPS,
			CurrentTPS:    currentTPS,
			PeakQPS:       peakQPS,
			PeakTPS:       peakTPS,
		},
		SLA: OpsSLA{
			SuccessRate:   successRate,
			ErrorRate:     errorRate,
			SLAErrorCount: traffic.TotalRequests - traffic.SuccessCount,
		},
		Latency: OpsLatency{
			DurationP50Ms: latency.DurationP50Ms,
			DurationP90Ms: latency.DurationP90Ms,
			DurationP95Ms: latency.DurationP95Ms,
			DurationP99Ms: latency.DurationP99Ms,
			DurationAvgMs: int(traffic.DurationAvgMs),
			DurationMaxMs: latency.DurationMaxMs,
			TTFTP50Ms:     latency.TTFTP50Ms,
			TTFTP90Ms:     latency.TTFTP90Ms,
			TTFTP95Ms:     latency.TTFTP95Ms,
			TTFTP99Ms:     latency.TTFTP99Ms,
			TTFTAvgMs:     int(traffic.TTFTAvgMs),
			TTFTMaxMs:     latency.TTFTMaxMs,
		},
		Errors: OpsErrors{
			RequestErrors:             traffic.RequestErrors,
			UpstreamErrors:            traffic.UpstreamErrors,
			BusinessErrors:            traffic.BusinessErrors,
			UpstreamErrorsExcl429_529: traffic.UpstreamErrorsExcl429_529,
			RateLimit429_529:          traffic.RateLimit429_529,
		},
		Jobs: jobs,
	}, nil
}

// GetDashboard 获取运维看板总览
func (h *OpsHandler) GetDashboard(c *gin.Context) {
	filter := OpsFilter{
		TimeRange: c.DefaultQuery("time_range", "1h"),
		GroupID:   c.Query("group_id"),
		Provider:  c.Query("provider"),
		Model:     c.Query("model"),
	}
	ov, err := h.buildOpsOverviewWithFilter(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, OpsDashboardResponse{
		Overview:  ov,
		Timestamp: time.Now(),
	})
}

// calculateHealthScore 计算健康评分 (0-100)
func calculateHealthScore(sla float64, memoryPercent float64, cpuPercent float64, goroutineCount int) int {
	score := 100

	if sla < 0.99 {
		score -= int((0.99 - sla) * 500)
	}
	if score < 50 {
		score = 50
	}

	if memoryPercent > 90 {
		score -= 20
	} else if memoryPercent > 80 {
		score -= 10
	} else if memoryPercent > 70 {
		score -= 5
	}

	if cpuPercent > 90 {
		score -= 20
	} else if cpuPercent > 80 {
		score -= 10
	} else if cpuPercent > 70 {
		score -= 5
	}

	if goroutineCount > 2000 {
		score -= 10
	} else if goroutineCount > 1000 {
		score -= 5
	}

	if score < 0 {
		score = 0
	}

	return score
}

// TrafficTrendPoint 流量趋势数据点
type TrafficTrendPoint struct {
	Timestamp    string  `json:"timestamp"`
	QPS          float64 `json:"qps"`
	TPS          float64 `json:"tps"`
	RequestCount int64   `json:"request_count"`
	TokenCount   int64   `json:"token_count"`
}

// GetTrafficTrend 获取流量趋势
func (h *OpsHandler) GetTrafficTrend(c *gin.Context) {
	timeRange := c.DefaultQuery("time_range", "1h")
	interval := timeRangeToInterval(timeRange)
	truncExpr := bucketTruncExpr(timeRange)
	sec := bucketSeconds(timeRange)

	var rows []struct {
		Timestamp    time.Time `gorm:"column:ts"`
		RequestCount int64     `gorm:"column:request_count"`
		TokenCount   int64     `gorm:"column:token_count"`
	}

	err := h.db.Raw(fmt.Sprintf(`
		SELECT
			%s as ts,
			COUNT(*)::bigint as request_count,
			COALESCE(SUM(total_tokens), 0)::bigint as token_count
		FROM call_logs
		WHERE ts > NOW() - INTERVAL '%s'
		GROUP BY %s
		ORDER BY ts
	`, truncExpr, interval, truncExpr)).Scan(&rows).Error

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query traffic trend"})
		return
	}

	points := make([]TrafficTrendPoint, 0, len(rows))
	for _, row := range rows {
		points = append(points, TrafficTrendPoint{
			Timestamp:    row.Timestamp.Format(time.RFC3339),
			QPS:          float64(row.RequestCount) / sec,
			TPS:          float64(row.TokenCount) / sec,
			RequestCount: row.RequestCount,
			TokenCount:   row.TokenCount,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"points": points,
		"bucket": bucketName(timeRange),
	})
}

// ErrorTrendPoint 错误趋势数据点
type ErrorTrendPoint struct {
	Timestamp      string `json:"timestamp"`
	RequestErrors  int64  `json:"request_errors"`
	UpstreamErrors int64  `json:"upstream_errors"`
	BusinessErrors int64  `json:"business_errors"`
}

// GetErrorTrend 获取错误趋势
func (h *OpsHandler) GetErrorTrend(c *gin.Context) {
	timeRange := c.DefaultQuery("time_range", "1h")
	interval := timeRangeToInterval(timeRange)
	truncExpr := bucketTruncExpr(timeRange)

	var rows []struct {
		Timestamp      time.Time `gorm:"column:ts"`
		RequestErrors  int64     `gorm:"column:request_errors"`
		UpstreamErrors int64     `gorm:"column:upstream_errors"`
		BusinessErrors int64     `gorm:"column:business_errors"`
	}

	err := h.db.Raw(fmt.Sprintf(`
		SELECT
			%s as ts,
			COUNT(*) FILTER (WHERE status >= 400 AND status < 500)::bigint as request_errors,
			COUNT(*) FILTER (WHERE status >= 500)::bigint as upstream_errors,
			COUNT(*) FILTER (WHERE status = 429)::bigint as business_errors
		FROM call_logs
		WHERE ts > NOW() - INTERVAL '%s'
		GROUP BY %s
		ORDER BY ts
	`, truncExpr, interval, truncExpr)).Scan(&rows).Error

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query error trend"})
		return
	}

	points := make([]ErrorTrendPoint, 0, len(rows))
	for _, row := range rows {
		points = append(points, ErrorTrendPoint{
			Timestamp:      row.Timestamp.Format(time.RFC3339),
			RequestErrors:  row.RequestErrors,
			UpstreamErrors: row.UpstreamErrors,
			BusinessErrors: row.BusinessErrors,
		})
	}

	c.JSON(http.StatusOK, gin.H{"points": points})
}

// LatencyBucket 延迟分布桶
type LatencyBucket struct {
	Range string `json:"range"`
	Count int64  `json:"count"`
}

// GetLatencyHistogram 获取延迟分布直方图
func (h *OpsHandler) GetLatencyHistogram(c *gin.Context) {
	timeRange := c.DefaultQuery("time_range", "1h")
	interval := timeRangeToInterval(timeRange)

	var rows []struct {
		Range string `gorm:"column:latency_range"`
		Count int64  `gorm:"column:count"`
	}

	err := h.db.Raw(fmt.Sprintf(`
		SELECT latency_range, count FROM (
			SELECT
				CASE
					WHEN duration_ms < 100 THEN '0-100ms'
					WHEN duration_ms < 500 THEN '100-500ms'
					WHEN duration_ms < 1000 THEN '500-1s'
					WHEN duration_ms < 2000 THEN '1-2s'
					WHEN duration_ms < 5000 THEN '2-5s'
					ELSE '5s+'
				END as latency_range,
				COUNT(*)::bigint as count
			FROM call_logs
			WHERE ts > NOW() - INTERVAL '%s'
			GROUP BY latency_range
		) sub ORDER BY
			CASE latency_range
				WHEN '0-100ms' THEN 1
				WHEN '100-500ms' THEN 2
				WHEN '500-1s' THEN 3
				WHEN '1-2s' THEN 4
				WHEN '2-5s' THEN 5
				ELSE 6
			END
	`, interval)).Scan(&rows).Error

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query latency histogram"})
		return
	}

	buckets := make([]LatencyBucket, 0, len(rows))
	var totalRequests int64
	for _, row := range rows {
		buckets = append(buckets, LatencyBucket{
			Range: row.Range,
			Count: row.Count,
		})
		totalRequests += row.Count
	}

	c.JSON(http.StatusOK, gin.H{
		"buckets":        buckets,
		"total_requests": totalRequests,
	})
}

// ErrorDistributionItem 错误分布项
type ErrorDistributionItem struct {
	StatusCode int    `json:"status_code"`
	Count      int64  `json:"count"`
	Category   string `json:"category"`
}

// GetErrorDistribution 获取错误分布
func (h *OpsHandler) GetErrorDistribution(c *gin.Context) {
	timeRange := c.DefaultQuery("time_range", "1h")
	interval := timeRangeToInterval(timeRange)

	var rows []struct {
		StatusCode int   `gorm:"column:status_code"`
		Count      int64 `gorm:"column:count"`
	}

	err := h.db.Raw(fmt.Sprintf(`
		SELECT
			status as status_code,
			COUNT(*)::bigint as count
		FROM call_logs
		WHERE ts > NOW() - INTERVAL '%s'
			AND status >= 400
		GROUP BY status
		ORDER BY count DESC
	`, interval)).Scan(&rows).Error

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query error distribution"})
		return
	}

	distribution := make([]ErrorDistributionItem, 0, len(rows))
	for _, row := range rows {
		category := "other"
		if row.StatusCode >= 400 && row.StatusCode < 500 {
			category = "client"
		} else if row.StatusCode >= 500 && row.StatusCode < 600 {
			category = "upstream"
		}

		distribution = append(distribution, ErrorDistributionItem{
			StatusCode: row.StatusCode,
			Count:      row.Count,
			Category:   category,
		})
	}

	c.JSON(http.StatusOK, gin.H{"distribution": distribution})
}

// =============================================================================
// 请求日志与错误分析
// =============================================================================

// ListRequests 查询调用日志列表
// GET /api/v1/ops/requests
func (h *OpsHandler) ListRequests(c *gin.Context) {
	timeRange := c.DefaultQuery("time_range", "1h")
	interval := timeRangeToInterval(timeRange)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	sortBy := c.DefaultQuery("sort_by", "ts")
	sortOrder := c.DefaultQuery("sort_order", "desc")
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	// 白名单校验 sort_by 避免 SQL 注入
	allowedSorts := map[string]string{
		"ts": "ts", "duration_ms": "duration_ms", "total_tokens": "total_tokens",
		"status": "status", "first_token_ms": "first_token_ms",
	}
	sortCol, ok := allowedSorts[sortBy]
	if !ok {
		sortCol = "ts"
	}
	if strings.ToUpper(sortOrder) != "ASC" {
		sortOrder = "DESC"
	}

	where := fmt.Sprintf("WHERE ts > NOW() - INTERVAL '%s'", interval)
	args := []interface{}{}

	if status := c.Query("status"); status != "" {
		where += " AND status = ?"
		args = append(args, status)
	}
	if provider := c.Query("provider"); provider != "" {
		where += " AND provider = ?"
		args = append(args, provider)
	}
	if groupID := c.Query("group_id"); groupID != "" {
		// group_id 是 model_groups 主键，需要解析为 group_name 列值
		var groupName string
		if err := h.db.Raw(
			"SELECT name FROM model_groups WHERE id = ?", groupID,
		).Scan(&groupName).Error; err == nil && groupName != "" {
			where += " AND group_name = ?"
			args = append(args, groupName)
		}
	}
	if modelFilter := c.Query("model"); modelFilter != "" {
		where += " AND model = ?"
		args = append(args, modelFilter)
	}
	if search := c.Query("search"); search != "" {
		where += " AND (request_id ILIKE ? OR model ILIKE ? OR provider ILIKE ?)"
		like := "%" + search + "%"
		args = append(args, like, like, like)
	}
	if minDur := c.Query("min_duration_ms"); minDur != "" {
		where += " AND duration_ms >= ?"
		args = append(args, minDur)
	}

	// 查询总数
	var total int64
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM call_logs %s", where)
	if err := h.db.Raw(countQuery, args...).Scan(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to count requests"})
		return
	}

	// 查询列表
	offset := (page - 1) * pageSize
	listQuery := fmt.Sprintf(`
		SELECT request_id, ts, key_id, user_id, model, group_name, provider,
			status, duration_ms, first_token_ms, total_tokens, prompt_tokens, completion_tokens,
			upstream_status, ip_address, failover_used, routed_via_group
		FROM call_logs %s
		ORDER BY %s %s
		LIMIT ? OFFSET ?
	`, where, sortCol, sortOrder)
	allArgs := append(args, pageSize, offset)

	items := make([]gin.H, 0)
	rows, err := h.db.Raw(listQuery, allArgs...).Rows()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query requests"})
		return
	}
	defer rows.Close()

	for rows.Next() {
		var item gin.H = gin.H{}
		var requestID, ts, keyID, userID, modelName, groupName, provider, ipAddress string
		var status, durationMs, totalTokens, promptTokens, completionTokens int
		var firstTokenMs *int
		var upstreamStatus int
		var failoverUsed, routedViaGroup bool

		err := rows.Scan(&requestID, &ts, &keyID, &userID, &modelName, &groupName, &provider,
			&status, &durationMs, &firstTokenMs, &totalTokens, &promptTokens, &completionTokens,
			&upstreamStatus, &ipAddress, &failoverUsed, &routedViaGroup)
		if err != nil {
			continue
		}
		item = gin.H{
			"request_id": requestID, "ts": ts, "key_id": keyID, "user_id": userID,
			"model": modelName, "group_name": groupName, "provider": provider,
			"status": status, "duration_ms": durationMs, "first_token_ms": firstTokenMs,
			"total_tokens": totalTokens, "prompt_tokens": promptTokens, "completion_tokens": completionTokens,
			"upstream_status": upstreamStatus, "ip_address": ipAddress,
			"failover_used": failoverUsed, "routed_via_group": routedViaGroup,
		}
		items = append(items, item)
	}

	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// ListRequestErrors 查询客户端错误列表 (status >= 400 AND < 500)
// GET /api/v1/ops/request-errors
func (h *OpsHandler) ListRequestErrors(c *gin.Context) {
	timeRange := c.DefaultQuery("time_range", "1h")
	interval := timeRangeToInterval(timeRange)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	where := fmt.Sprintf("WHERE ts > NOW() - INTERVAL '%s' AND status >= 400 AND status < 500", interval)
	args := []interface{}{}

	if provider := c.Query("provider"); provider != "" {
		where += " AND provider = ?"
		args = append(args, provider)
	}
	if modelFilter := c.Query("model"); modelFilter != "" {
		where += " AND model = ?"
		args = append(args, modelFilter)
	}

	var total int64
	h.db.Raw(fmt.Sprintf("SELECT COUNT(*) FROM call_logs %s", where), args...).Scan(&total)

	offset := (page - 1) * pageSize
	query := fmt.Sprintf(`
		SELECT request_id, ts, model, group_name, provider, status, duration_ms,
			upstream_status, ip_address
		FROM call_logs %s
		ORDER BY ts DESC
		LIMIT ? OFFSET ?
	`, where)
	allArgs := append(args, pageSize, offset)

	items := make([]gin.H, 0)
	rows, err := h.db.Raw(query, allArgs...).Rows()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query request errors"})
		return
	}
	defer rows.Close()

	for rows.Next() {
		var requestID, ts, modelName, groupName, provider, ipAddress string
		var status, durationMs, upstreamStatus int
		rows.Scan(&requestID, &ts, &modelName, &groupName, &provider, &status, &durationMs,
			&upstreamStatus, &ipAddress)
		// B05/WP3: 列表项以 request_id 作为唯一标识。同时回填 id=request_id，
		// 兼容前端 :key="item.id" / open-detail(item.id)（detail 端点按
		// WHERE request_id = ? 查询，故 id=request_id 即可正确打开详情）。
		items = append(items, gin.H{
			"id":              requestID,
			"request_id":      requestID,
			"ts":              ts,
			"model":           modelName,
			"group_name":      groupName,
			"provider":        provider,
			"status":          status,
			"duration_ms":     durationMs,
			"upstream_status": upstreamStatus,
			"ip_address":      ipAddress,
		})
	}

	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": pageSize})
}

// GetRequestErrorDetail 获取单条客户端错误详情
// GET /api/v1/ops/request-errors/:id
func (h *OpsHandler) GetRequestErrorDetail(c *gin.Context) {
	id := c.Param("id")
	rows, err := h.db.Raw(`
		SELECT request_id, ts, key_id, key_name, user_id, model, group_name, provider,
			status, duration_ms, first_token_ms, total_tokens, prompt_tokens, completion_tokens,
			upstream_status, ip_address, failover_used, routed_via_group,
			stage_auth_ms, stage_route_ms, stage_upstream_ms
		FROM call_logs
		WHERE request_id = ? AND status >= 400 AND status < 500
	`, id).Rows()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query request error"})
		return
	}
	defer rows.Close()

	if !rows.Next() {
		c.JSON(http.StatusNotFound, gin.H{"error": "request error not found"})
		return
	}

	var requestID, ts, keyID, keyName, userID, modelName, groupName, provider, ipAddress string
	var status, durationMs, totalTokens, promptTokens, completionTokens int
	var authStage, routingStage, upstreamStage *int16
	var firstTokenMs *int
	var upstreamStatus int
	var failoverUsed, routedViaGroup bool

	if scanErr := rows.Scan(&requestID, &ts, &keyID, &keyName, &userID, &modelName, &groupName, &provider,
		&status, &durationMs, &firstTokenMs, &totalTokens, &promptTokens, &completionTokens,
		&upstreamStatus, &ipAddress, &failoverUsed, &routedViaGroup,
		&authStage, &routingStage, &upstreamStage); scanErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to scan"})
		return
	}

	// 构建 upstream_context
	upstreamCtx := UpstreamContext{
		UpstreamStatusCode: upstreamStatus,
	}

	ttftMs := 0
	if firstTokenMs != nil {
		ttftMs = *firstTokenMs
	}
	authMs, routingMs, upstreamMs := 0, 0, 0
	if authStage != nil {
		authMs = int(*authStage)
	}
	if routingStage != nil {
		routingMs = int(*routingStage)
	}
	if upstreamStage != nil {
		upstreamMs = int(*upstreamStage)
	}
	latencyBreakdown := LatencyBreakdown{
		AuthMs:     authMs,
		RoutingMs:  routingMs,
		UpstreamMs: upstreamMs,
		ResponseMs: max(durationMs-authMs-routingMs-upstreamMs, 0),
		TTFTMs:     ttftMs,
	}

	c.JSON(http.StatusOK, gin.H{
		"request_id": requestID, "ts": ts, "key_id": keyID, "key_name": keyName, "user_id": userID,
		"model": modelName, "group_name": groupName, "provider": provider,
		"status": status, "duration_ms": durationMs, "first_token_ms": firstTokenMs,
		"total_tokens": totalTokens, "prompt_tokens": promptTokens, "completion_tokens": completionTokens,
		"upstream_status": upstreamStatus, "ip_address": ipAddress,
		"failover_used": failoverUsed, "routed_via_group": routedViaGroup,
		"upstream_context":  upstreamCtx,
		"latency_breakdown": latencyBreakdown,
	})
}

// GetUpstreamErrorDetail 获取单条上游错误详情
// GET /api/v1/ops/upstream-errors/:id
func (h *OpsHandler) GetUpstreamErrorDetail(c *gin.Context) {
	id := c.Param("id")
	rows, err := h.db.Raw(`
		SELECT request_id, ts, key_id, user_id, model, group_name, provider,
			status, duration_ms, first_token_ms, total_tokens, prompt_tokens, completion_tokens,
			upstream_status, ip_address, failover_used, routed_via_group
		FROM call_logs
		WHERE request_id = ? AND status >= 500
	`, id).Rows()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query upstream error"})
		return
	}
	defer rows.Close()

	if !rows.Next() {
		c.JSON(http.StatusNotFound, gin.H{"error": "upstream error not found"})
		return
	}

	var requestID, ts, keyID, userID, modelName, groupName, provider, ipAddress string
	var status, durationMs, totalTokens, promptTokens, completionTokens int
	var firstTokenMs *int
	var upstreamStatus int
	var failoverUsed, routedViaGroup bool

	if scanErr := rows.Scan(&requestID, &ts, &keyID, &userID, &modelName, &groupName, &provider,
		&status, &durationMs, &firstTokenMs, &totalTokens, &promptTokens, &completionTokens,
		&upstreamStatus, &ipAddress, &failoverUsed, &routedViaGroup); scanErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to scan"})
		return
	}

	// 构建 upstream_context
	upstreamCtx := UpstreamContext{
		UpstreamStatusCode: upstreamStatus,
	}

	// 构建 latency_breakdown
	ttftMs := 0
	if firstTokenMs != nil {
		ttftMs = *firstTokenMs
	}
	latencyBreakdown := LatencyBreakdown{
		AuthMs:     0,
		RoutingMs:  0,
		UpstreamMs: durationMs,
		ResponseMs: 0,
		TTFTMs:     ttftMs,
	}

	c.JSON(http.StatusOK, gin.H{
		"request_id": requestID, "ts": ts, "key_id": keyID, "user_id": userID,
		"model": modelName, "group_name": groupName, "provider": provider,
		"status": status, "duration_ms": durationMs, "first_token_ms": firstTokenMs,
		"total_tokens": totalTokens, "prompt_tokens": promptTokens, "completion_tokens": completionTokens,
		"upstream_status": upstreamStatus, "ip_address": ipAddress,
		"failover_used": failoverUsed, "routed_via_group": routedViaGroup,
		"upstream_context":  upstreamCtx,
		"latency_breakdown": latencyBreakdown,
	})
}

// ListUpstreamErrors 查询上游错误列表 (status >= 500)
// GET /api/v1/ops/upstream-errors
func (h *OpsHandler) ListUpstreamErrors(c *gin.Context) {
	timeRange := c.DefaultQuery("time_range", "1h")
	interval := timeRangeToInterval(timeRange)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	where := fmt.Sprintf("WHERE ts > NOW() - INTERVAL '%s' AND status >= 500", interval)
	args := []interface{}{}

	if provider := c.Query("provider"); provider != "" {
		where += " AND provider = ?"
		args = append(args, provider)
	}
	if modelFilter := c.Query("model"); modelFilter != "" {
		where += " AND model = ?"
		args = append(args, modelFilter)
	}

	var total int64
	h.db.Raw(fmt.Sprintf("SELECT COUNT(*) FROM call_logs %s", where), args...).Scan(&total)

	offset := (page - 1) * pageSize
	query := fmt.Sprintf(`
		SELECT request_id, ts, model, group_name, provider, status, duration_ms,
			upstream_status, ip_address
		FROM call_logs %s
		ORDER BY ts DESC
		LIMIT ? OFFSET ?
	`, where)
	allArgs := append(args, pageSize, offset)

	items := make([]gin.H, 0)
	rows, err := h.db.Raw(query, allArgs...).Rows()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query upstream errors"})
		return
	}
	defer rows.Close()

	for rows.Next() {
		var requestID, ts, modelName, groupName, provider, ipAddress string
		var status, durationMs, upstreamStatus int
		rows.Scan(&requestID, &ts, &modelName, &groupName, &provider, &status, &durationMs,
			&upstreamStatus, &ipAddress)
		// B05/WP3: 同 ListRequestErrors，列表项 id=request_id 兼容前端 :key/open-detail。
		items = append(items, gin.H{
			"id":              requestID,
			"request_id":      requestID,
			"ts":              ts,
			"model":           modelName,
			"group_name":      groupName,
			"provider":        provider,
			"status":          status,
			"duration_ms":     durationMs,
			"upstream_status": upstreamStatus,
			"ip_address":      ipAddress,
		})
	}

	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": pageSize})
}

// =============================================================================
// 并发与切换率
// =============================================================================

// GetConcurrencyStats 获取各 provider 并发状态
// GET /api/v1/ops/concurrency
//
// 响应 shape 对齐前端 OpsConcurrencyStatsResponse 契约（B03/WP2）：
//   - 每项 provider 字段名/结构与前端 OpsProviderConcurrency 一致：
//     provider_id / provider_name / status / current_load / max_capacity /
//     active_requests / waiting_queue / is_rate_limited / rate_limit_remaining /
//     rate_limit_reset_seconds / error_count_5min / last_error_at /
//     last_error_message / provider_type。
//   - 响应补充聚合项：rate_limited_providers / error_providers /
//     total_active_requests / total_waiting_queue。
//
// 数据源说明（不造假）：
//   - active / max_capacity / waiting_queue 来自 Redis（concurrency:provider:* 与
//     ratelimit:provider:*）。
//   - error_count_5min 由 call_logs 最近 5 分钟错误数推算（error_rate * request_count
//     取整）；无 call_logs 时为 0。
//   - is_rate_limited / rate_limit_remaining / rate_limit_reset_seconds：当前无独立
//     数据源，返回 false / 0 + TODO(WP8)（需接 Redis writer/account 维度）。
//   - group / account 三级并发(参考仓 OpsConcurrencyStatsResponse.platform/group/
//     account)本端点无数据源，留待 WP8 接 Redis 并发 writer 后补，此处仅返回 provider 维度。
//   - last_error_at / last_error_message / provider_type：无数据源，返回空/null + TODO。
func (h *OpsHandler) GetConcurrencyStats(c *gin.Context) {
	type providerStat struct {
		ProviderID            string  `json:"provider_id"`
		ProviderName          string  `json:"provider_name"`
		ProviderType          string  `json:"provider_type"`
		Status                string  `json:"status"`
		CurrentLoad           float64 `json:"current_load"`
		MaxCapacity           int64   `json:"max_capacity"`
		ActiveRequests        int64   `json:"active_requests"`
		WaitingQueue          int64   `json:"waiting_queue"`
		IsRateLimited         bool    `json:"is_rate_limited"`
		RateLimitRemaining    int     `json:"rate_limit_remaining"`
		RateLimitResetSeconds int     `json:"rate_limit_reset_seconds"`
		ErrorCount5Min        int64   `json:"error_count_5min"`
		LastErrorAt           *string `json:"last_error_at"`
		LastErrorMessage      *string `json:"last_error_message"`
	}

	providers := make([]providerStat, 0)

	// 1. 从 Redis 获取各 provider 并发数
	if h.rdb != nil {
		iter := h.rdb.Scan(c.Request.Context(), 0, "concurrency:provider:*", 100).Iterator()
		for iter.Next(c.Request.Context()) {
			key := iter.Val()
			parts := strings.Split(key, ":")
			if len(parts) == 3 {
				val, _ := h.rdb.Get(c.Request.Context(), key).Int64()

				maxCap := int64(0)
				rateKey := fmt.Sprintf("ratelimit:provider:%s", parts[2])
				rateJSON, rateErr := h.rdb.Get(c.Request.Context(), rateKey).Result()
				if rateErr == nil {
					var rl struct {
						MaxCapacity int64 `json:"max_capacity"`
					}
					if json.Unmarshal([]byte(rateJSON), &rl) == nil && rl.MaxCapacity > 0 {
						maxCap = rl.MaxCapacity
					}
				}

				waiting := int64(0)
				if maxCap > 0 && val > maxCap {
					waiting = val - maxCap
				}

				// current_load = active / max_capacity * 100（max 为 0 时记 0，不造数）
				currentLoad := 0.0
				if maxCap > 0 {
					currentLoad = float64(val) / float64(maxCap) * 100.0
				}

				// status 推算：负载 > 90% 视为 overloaded，其余 available。
				// is_rate_limited 暂无独立数据源 → false（TODO WP8 接 account 维度）。
				status := "available"
				if currentLoad >= 90.0 {
					status = "overloaded"
				}

				providers = append(providers, providerStat{
					ProviderID:     parts[2],
					ProviderName:   parts[2],
					MaxCapacity:    maxCap,
					ActiveRequests: val,
					WaitingQueue:   waiting,
					CurrentLoad:    currentLoad,
					Status:         status,
					// IsRateLimited / RateLimitRemaining / RateLimitResetSeconds:
					// 默认 false/0，TODO(WP8) 接真实 rate-limit writer。
				})
			}
		}
	}

	// 2. 从 call_logs 获取最近的错误率（最近 5 分钟）
	//    注意：含 Postgres 方言（::float/::bigint/INTERVAL），SQLite 上不跑；
	//    生产 Postgres 可用。错误统计缺失时 error_count_5min 保持 0（不造假）。
	var errorStats []struct {
		Provider     string  `gorm:"column:provider"`
		ErrorRate    float64 `gorm:"column:error_rate"`
		RequestCount int64   `gorm:"column:request_count"`
	}
	h.db.Raw(`
		SELECT provider,
			(SUM(CASE WHEN status >= 500 THEN 1 ELSE 0 END)::float / NULLIF(COUNT(*), 0))::float as error_rate,
			COUNT(*)::bigint as request_count
		FROM call_logs
		WHERE ts > NOW() - INTERVAL '5 minutes'
		GROUP BY provider
	`).Scan(&errorStats)

	errMap := make(map[string]struct {
		ErrorRate    float64
		RequestCount int64
	})
	for _, es := range errorStats {
		errMap[es.Provider] = struct {
			ErrorRate    float64
			RequestCount int64
		}{es.ErrorRate, es.RequestCount}
	}

	// 聚合计数
	var rateLimitedProviders, errorProviders, totalActive, totalWaiting int64
	for i := range providers {
		if es, ok := errMap[providers[i].ProviderID]; ok {
			// error_count_5min ≈ error_rate * request_count（取整，真实推算非编造）
			providers[i].ErrorCount5Min = int64(es.ErrorRate * float64(es.RequestCount))
		}
		if providers[i].IsRateLimited {
			rateLimitedProviders++
		}
		if providers[i].ErrorCount5Min > 0 {
			errorProviders++
		}
		totalActive += providers[i].ActiveRequests
		totalWaiting += providers[i].WaitingQueue
	}

	c.JSON(http.StatusOK, gin.H{
		"providers":              providers,
		"total_providers":        len(providers),
		"available_providers":    len(providers) - int(rateLimitedProviders),
		"rate_limited_providers": rateLimitedProviders,
		"error_providers":        errorProviders,
		"total_active_requests":  totalActive,
		"total_waiting_queue":    totalWaiting,
	})
}

// SwitchRateTrendPoint 切换率趋势数据点
type SwitchRateTrendPoint struct {
	Timestamp    string  `json:"timestamp"`
	SwitchRate   float64 `json:"switch_rate"`
	SwitchCount  int64   `json:"switch_count"`
	RequestCount int64   `json:"request_count"`
}

// GetSwitchRateTrend 获取切换率趋势
// GET /api/v1/ops/switch-rate-trend
func (h *OpsHandler) GetSwitchRateTrend(c *gin.Context) {
	timeRange := c.DefaultQuery("time_range", "1h")
	interval := timeRangeToInterval(timeRange)
	truncExpr := bucketTruncExpr(timeRange)

	var rows []struct {
		Timestamp    time.Time `gorm:"column:ts"`
		SwitchCount  int64     `gorm:"column:switch_count"`
		RequestCount int64     `gorm:"column:request_count"`
	}

	err := h.db.Raw(fmt.Sprintf(`
		SELECT
			%s as ts,
			SUM(CASE WHEN failover_used THEN 1 ELSE 0 END)::bigint as switch_count,
			COUNT(*)::bigint as request_count
		FROM call_logs
		WHERE ts > NOW() - INTERVAL '%s'
		GROUP BY %s
		ORDER BY ts
	`, truncExpr, interval, truncExpr)).Scan(&rows).Error

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query switch rate trend"})
		return
	}

	points := make([]SwitchRateTrendPoint, 0, len(rows))
	for _, row := range rows {
		rate := 0.0
		if row.RequestCount > 0 {
			rate = float64(row.SwitchCount) / float64(row.RequestCount)
		}
		points = append(points, SwitchRateTrendPoint{
			Timestamp:    row.Timestamp.Format(time.RFC3339),
			SwitchRate:   rate,
			SwitchCount:  row.SwitchCount,
			RequestCount: row.RequestCount,
		})
	}

	c.JSON(http.StatusOK, gin.H{"points": points})
}

// ThroughputTrendPoint 吞吐量趋势数据点（含 provider 维度）
type ThroughputTrendPoint struct {
	Timestamp    string `json:"timestamp"`
	Provider     string `json:"provider,omitempty"`
	GroupName    string `json:"group_name,omitempty"`
	RequestCount int64  `json:"request_count"`
	TokenCount   int64  `json:"token_count"`
	ErrorCount   int64  `json:"error_count"`
}

// GetThroughputTrend 获取多维度吞吐量趋势
// GET /api/v1/ops/throughput-trend
func (h *OpsHandler) GetThroughputTrend(c *gin.Context) {
	timeRange := c.DefaultQuery("time_range", "1h")
	interval := timeRangeToInterval(timeRange)
	truncExpr := bucketTruncExpr(timeRange)
	byProvider := c.DefaultQuery("by_provider", "false")
	topGroups, _ := strconv.Atoi(c.DefaultQuery("top_groups", "0"))

	if byProvider == "true" {
		var rows []struct {
			Timestamp    time.Time `gorm:"column:ts"`
			Provider     string    `gorm:"column:provider"`
			RequestCount int64     `gorm:"column:request_count"`
			TokenCount   int64     `gorm:"column:token_count"`
			ErrorCount   int64     `gorm:"column:error_count"`
		}
		err := h.db.Raw(fmt.Sprintf(`
			SELECT
				%s as ts,
				provider,
				COUNT(*)::bigint as request_count,
				COALESCE(SUM(total_tokens), 0)::bigint as token_count,
				SUM(CASE WHEN status >= 400 THEN 1 ELSE 0 END)::bigint as error_count
			FROM call_logs
			WHERE ts > NOW() - INTERVAL '%s'
			GROUP BY %s, provider
			ORDER BY ts
		`, truncExpr, interval, truncExpr)).Scan(&rows).Error
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query throughput by provider"})
			return
		}
		points := make([]ThroughputTrendPoint, 0, len(rows))
		for _, r := range rows {
			points = append(points, ThroughputTrendPoint{
				Timestamp: r.Timestamp.Format(time.RFC3339), Provider: r.Provider,
				RequestCount: r.RequestCount, TokenCount: r.TokenCount, ErrorCount: r.ErrorCount,
			})
		}
		c.JSON(http.StatusOK, gin.H{"points": points, "dimension": "provider"})
		return
	}

	if topGroups > 0 {
		topNames := make([]string, 0)
		h.db.Raw(fmt.Sprintf(`
			SELECT group_name FROM call_logs
			WHERE ts > NOW() - INTERVAL '%s' AND group_name != '' AND routed_via_group = true
			GROUP BY group_name ORDER BY COUNT(*) DESC LIMIT ?
		`, interval), topGroups).Pluck("group_name", &topNames)

		var rows []struct {
			Timestamp    time.Time `gorm:"column:ts"`
			GroupName    string    `gorm:"column:group_name"`
			RequestCount int64     `gorm:"column:request_count"`
			TokenCount   int64     `gorm:"column:token_count"`
			ErrorCount   int64     `gorm:"column:error_count"`
		}
		if len(topNames) > 0 {
			h.db.Raw(fmt.Sprintf(`
				SELECT
					%s as ts, group_name,
					COUNT(*)::bigint as request_count,
					COALESCE(SUM(total_tokens), 0)::bigint as token_count,
					SUM(CASE WHEN status >= 400 THEN 1 ELSE 0 END)::bigint as error_count
				FROM call_logs
				WHERE ts > NOW() - INTERVAL '%s' AND group_name IN ?
				GROUP BY %s, group_name
				ORDER BY ts
			`, truncExpr, interval, truncExpr), topNames).Scan(&rows)
		}
		points := make([]ThroughputTrendPoint, 0, len(rows))
		for _, r := range rows {
			points = append(points, ThroughputTrendPoint{
				Timestamp: r.Timestamp.Format(time.RFC3339), GroupName: r.GroupName,
				RequestCount: r.RequestCount, TokenCount: r.TokenCount, ErrorCount: r.ErrorCount,
			})
		}
		c.JSON(http.StatusOK, gin.H{"points": points, "dimension": "group"})
		return
	}

	var rows []struct {
		Timestamp    time.Time `gorm:"column:ts"`
		RequestCount int64     `gorm:"column:request_count"`
		TokenCount   int64     `gorm:"column:token_count"`
	}
	err := h.db.Raw(fmt.Sprintf(`
		SELECT %s as ts,
			COUNT(*)::bigint as request_count,
			COALESCE(SUM(total_tokens), 0)::bigint as token_count
		FROM call_logs
		WHERE ts > NOW() - INTERVAL '%s'
		GROUP BY %s
		ORDER BY ts
	`, truncExpr, interval, truncExpr)).Scan(&rows).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query throughput trend"})
		return
	}

	points := make([]ThroughputTrendPoint, 0, len(rows))
	for _, r := range rows {
		points = append(points, ThroughputTrendPoint{
			Timestamp:    r.Timestamp.Format(time.RFC3339),
			RequestCount: r.RequestCount, TokenCount: r.TokenCount,
		})
	}
	c.JSON(http.StatusOK, gin.H{"points": points, "dimension": "global"})
}

// =============================================================================
// 告警规则
// =============================================================================

// ListAlertRules 查询告警规则列表
// GET /api/v1/ops/alert-rules
func (h *OpsHandler) ListAlertRules(c *gin.Context) {
	rules := make([]model.AlertRule, 0)
	if err := h.db.Order("id DESC").Find(&rules).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list alert rules"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": rules})
}

// CreateAlertRule 创建告警规则
// POST /api/v1/ops/alert-rules
func (h *OpsHandler) CreateAlertRule(c *gin.Context) {
	var rule model.AlertRule
	if err := c.ShouldBindJSON(&rule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.db.Create(&rule).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create alert rule"})
		return
	}
	c.JSON(http.StatusOK, rule)
}

// UpdateAlertRule 更新告警规则
// PUT /api/v1/ops/alert-rules/:id
func (h *OpsHandler) UpdateAlertRule(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var rule model.AlertRule
	if err := h.db.First(&rule, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "alert rule not found"})
		return
	}
	var input model.AlertRule
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.db.Model(&rule).Updates(map[string]interface{}{
		"name": input.Name, "description": input.Description, "enabled": input.Enabled,
		"metric_type": input.MetricType, "operator": input.Operator, "threshold": input.Threshold,
		"window_minutes": input.WindowMinutes, "sustained_minutes": input.SustainedMinutes,
		"severity": input.Severity, "cooldown_minutes": input.CooldownMinutes,
		"notify_email": input.NotifyEmail, "filters": input.Filters,
	})
	h.db.First(&rule, id)
	c.JSON(http.StatusOK, rule)
}

// DeleteAlertRule 删除告警规则
// DELETE /api/v1/ops/alert-rules/:id
func (h *OpsHandler) DeleteAlertRule(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	result := h.db.Delete(&model.AlertRule{}, id)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete alert rule"})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "alert rule not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// =============================================================================
// 告警事件
// =============================================================================

// ListAlertEvents 查询告警事件列表（keyset 分页）
// GET /api/v1/ops/alert-events
func (h *OpsHandler) ListAlertEvents(c *gin.Context) {
	timeRange := c.DefaultQuery("time_range", "24h")
	interval := timeRangeToInterval(timeRange)
	limit := 50
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 200 {
		limit = l
	}

	where := fmt.Sprintf("WHERE fired_at > NOW() - INTERVAL '%s'", interval)
	args := []interface{}{}

	if severity := c.Query("severity"); severity != "" {
		where += " AND severity = ?"
		args = append(args, severity)
	}
	if status := c.Query("status"); status != "" {
		where += " AND status = ?"
		args = append(args, status)
	}

	// keyset 分页
	if beforeFiredAt := c.Query("before_fired_at"); beforeFiredAt != "" {
		beforeID, _ := strconv.Atoi(c.DefaultQuery("before_id", "0"))
		where += " AND (fired_at < ? OR (fired_at = ? AND id < ?))"
		args = append(args, beforeFiredAt, beforeFiredAt, beforeID)
	}

	args = append(args, limit)
	events := make([]model.AlertEvent, 0)
	query := fmt.Sprintf("SELECT * FROM alert_events %s ORDER BY fired_at DESC, id DESC LIMIT ?", where)
	if err := h.db.Raw(query, args...).Scan(&events).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list alert events"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": events})
}

// GetAlertEvent 获取单条告警事件
// GET /api/v1/ops/alert-events/:id
func (h *OpsHandler) GetAlertEvent(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var event model.AlertEvent
	if err := h.db.First(&event, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "alert event not found"})
		return
	}
	c.JSON(http.StatusOK, event)
}

// UpdateAlertEventStatus 更新告警事件状态
// PUT /api/v1/ops/alert-events/:id/status
func (h *OpsHandler) UpdateAlertEventStatus(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var event model.AlertEvent
	if err := h.db.First(&event, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "alert event not found"})
		return
	}

	var input struct {
		Status string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updates := map[string]interface{}{"status": input.Status}
	if input.Status == "resolved" && event.ResolvedAt == nil {
		now := time.Now()
		updates["resolved_at"] = &now
	}
	h.db.Model(&event).Updates(updates)
	h.db.First(&event, id)
	c.JSON(http.StatusOK, event)
}

// CreateAlertSilence 创建告警静默
// POST /api/v1/ops/alert-silences
func (h *OpsHandler) CreateAlertSilence(c *gin.Context) {
	var silence model.AlertSilence
	if err := c.ShouldBindJSON(&silence); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.db.Create(&silence).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create silence"})
		return
	}
	c.JSON(http.StatusOK, silence)
}

// =============================================================================
// 系统日志
// =============================================================================

// ListSystemLogs 查询系统日志列表
// GET /api/v1/ops/system-logs
func (h *OpsHandler) ListSystemLogs(c *gin.Context) {
	timeRange := c.DefaultQuery("time_range", "1h")
	interval := timeRangeToInterval(timeRange)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}

	where := fmt.Sprintf("WHERE ts > NOW() - INTERVAL '%s'", interval)
	args := []interface{}{}

	if level := c.Query("level"); level != "" {
		where += " AND level = ?"
		args = append(args, level)
	}
	if component := c.Query("component"); component != "" {
		where += " AND component = ?"
		args = append(args, component)
	}
	if provider := c.Query("provider"); provider != "" {
		where += " AND provider = ?"
		args = append(args, provider)
	}
	if modelFilter := c.Query("model"); modelFilter != "" {
		where += " AND model = ?"
		args = append(args, modelFilter)
	}
	if requestID := c.Query("request_id"); requestID != "" {
		where += " AND request_id = ?"
		args = append(args, requestID)
	}
	if search := c.Query("search"); search != "" {
		where += " AND message ILIKE ?"
		args = append(args, "%"+search+"%")
	}

	var total int64
	h.db.Raw(fmt.Sprintf("SELECT COUNT(*) FROM system_logs %s", where), args...).Scan(&total)

	offset := (page - 1) * pageSize
	query := fmt.Sprintf(`
		SELECT id, ts, level, component, message, request_id, client_request_id,
			user_id, provider, model, "group", duration_ms, status, error_message,
			COALESCE(extra, '{}'::jsonb) as extra
		FROM system_logs %s
		ORDER BY ts DESC, id DESC
		LIMIT ? OFFSET ?
	`, where)
	allArgs := append(args, pageSize, offset)

	items := make([]model.SystemLog, 0)
	if err := h.db.Raw(query, allArgs...).Scan(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query system logs"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": pageSize})
}

// GetSystemLogSinkHealth 获取日志接入健康状态
// GET /api/v1/ops/system-logs/health
//
// 字段对齐前端 OpsSystemLogSinkHealth 契约（B06/WP2）：
// queue_depth / queue_capacity / dropped / write_failures / avg_write_delay_ms /
// status（+ 保留 pending_count / dropped_count / writer_enabled 兼容旧读取）。
//
// 不再硬编码 "healthy" stub。真实可达信息：
//   - writer_enabled: 当前无异步 sink writer 接入 → false（TODO WP7 接真 sink）。
//   - queue_depth / pending_count: 从 Redis 日志缓冲 key 估算（logbuf:* 计数），
//     无则 0。
//   - dropped / dropped_count: 无计数源 → 0 + TODO(WP7)。
//   - write_failures / avg_write_delay_ms: 无指标源 → 0（前端 .toFixed(2) 不再崩）。
//   - status: 无 writer → "degraded"（真实，不编造 healthy）。
func (h *OpsHandler) GetSystemLogSinkHealth(c *gin.Context) {
	queueDepth := int64(0)
	if h.rdb != nil {
		// 估算挂起日志条数：扫描 logbuf:* 计数（若 writer 未启用，这里通常为 0）。
		// 用 SCAN 限量，避免大 key 阻塞。
		iter := h.rdb.Scan(c.Request.Context(), 0, "logbuf:*", 200).Iterator()
		for iter.Next(c.Request.Context()) {
			queueDepth++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		// 前端契约字段（B06）
		"queue_depth":        queueDepth,
		"queue_capacity":     0, // TODO(WP7): 接真 sink 后填真实容量
		"dropped":            0, // TODO(WP7): 接真 sink 的 dropped 计数
		"write_failures":     0, // TODO(WP7): 接真 sink 的写入失败计数
		"avg_write_delay_ms": 0.0,
		"status":             "degraded", // writer 未接入，真实降级态
		// 兼容字段（旧读取 / 既有测试）
		"writer_enabled": false,
		"recent_errors":  0,
		"last_write_at":  nil,
		"pending_count":  queueDepth,
		"dropped_count":  0,
	})
}

// CleanupSystemLogs 清理过期系统日志
// POST /api/v1/ops/system-logs/cleanup
//
// 入参兼容（B06/WP2）：优先读 body JSON {retention_days}，回退 query ?days=。
// 前端 cleanupSystemLogs 发 body，旧客户端可能发 query，两者皆可。
func (h *OpsHandler) CleanupSystemLogs(c *gin.Context) {
	days := 30

	// 1. 优先 body JSON {retention_days}（或兼容 days）
	if c.Request.Body != nil && c.ContentType() == "application/json" {
		var body struct {
			RetentionDays *int `json:"retention_days"`
			Days          *int `json:"days"` // 兼容
		}
		if err := c.ShouldBindJSON(&body); err == nil {
			if body.RetentionDays != nil && *body.RetentionDays > 0 {
				days = *body.RetentionDays
			} else if body.Days != nil && *body.Days > 0 {
				days = *body.Days
			}
		}
	}

	// 2. 回退 query ?days=
	if d, err := strconv.Atoi(c.DefaultQuery("days", "")); err == nil && d > 0 {
		days = d
	}

	// DELETE 兼容 SQLite / Postgres：PG 用 NOW() - INTERVAL，SQLite 用
	// datetime('now','-N days')。方言由 db.Dialector.Name() 判定。
	deleteSQL := fmt.Sprintf("DELETE FROM system_logs WHERE ts < NOW() - INTERVAL '%d days'", days)
	if h.db.Dialector.Name() == "sqlite" {
		deleteSQL = fmt.Sprintf("DELETE FROM system_logs WHERE ts < datetime('now','-%d days')", days)
	}
	result := h.db.Exec(deleteSQL)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to cleanup logs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": result.RowsAffected, "older_than_days": days})
}

// =============================================================================
// 运行时日志配置
// =============================================================================

const settingKeyRuntimeLogConfig = "ops_runtime_log_config"

// defaultLogConfig 默认运行时日志配置
// 字段对齐前端 OpsRuntimeLogConfig 契约（B06/WP2）：补 retention_days /
// enable_caller / enable_stacktrace / stacktrace_level / sampling_initial /
// sampling_thereafter；保留既有 level / slow_request_ms / sample_rate /
// log_request_body / log_response_body / max_body_bytes。
func defaultLogConfig() gin.H {
	return gin.H{
		"level":               "info",
		"slow_request_ms":     5000,
		"sample_rate":         1.0,
		"log_request_body":    false,
		"log_response_body":   false,
		"max_body_bytes":      2048,
		"retention_days":      30,
		"enable_caller":       true,
		"enable_stacktrace":   false,
		"stacktrace_level":    "error",
		"sampling_initial":    5,
		"sampling_thereafter": 200,
	}
}

// GetRuntimeLogConfig 获取运行时日志配置
// GET /api/v1/ops/runtime/logging
func (h *OpsHandler) GetRuntimeLogConfig(c *gin.Context) {
	val, err := h.settingsRepo.GetValue(settingKeyRuntimeLogConfig)
	if err != nil || val == "" {
		c.JSON(http.StatusOK, defaultLogConfig())
		return
	}
	var cfg gin.H
	if err := json.Unmarshal([]byte(val), &cfg); err != nil {
		c.JSON(http.StatusOK, defaultLogConfig())
		return
	}
	// 合并默认值（新字段补全）
	defs := defaultLogConfig()
	for k, v := range defs {
		if _, ok := cfg[k]; !ok {
			cfg[k] = v
		}
	}
	c.JSON(http.StatusOK, cfg)
}

// UpdateRuntimeLogConfig 更新运行时日志配置
// PUT /api/v1/ops/runtime/logging
func (h *OpsHandler) UpdateRuntimeLogConfig(c *gin.Context) {
	var input gin.H
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	data, _ := json.Marshal(input)
	if err := h.settingsRepo.SetMultiple(map[string]string{
		settingKeyRuntimeLogConfig: string(data),
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save config"})
		return
	}
	c.JSON(http.StatusOK, input)
}

// ResetRuntimeLogConfig 重置运行时日志配置为默认值
// POST /api/v1/ops/runtime/logging/reset
func (h *OpsHandler) ResetRuntimeLogConfig(c *gin.Context) {
	defs := defaultLogConfig()
	data, _ := json.Marshal(defs)
	if err := h.settingsRepo.SetMultiple(map[string]string{
		settingKeyRuntimeLogConfig: string(data),
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to reset config"})
		return
	}
	c.JSON(http.StatusOK, defs)
}

// =============================================================================
// Token 统计
// =============================================================================

// TokenStatsItem token 统计聚合项
type TokenStatsItem struct {
	Provider         string  `gorm:"column:provider" json:"provider"`
	Model            string  `gorm:"column:model" json:"model"`
	RequestCount     int64   `gorm:"column:request_count" json:"request_count"`
	PromptTokens     int64   `gorm:"column:prompt_tokens" json:"prompt_tokens"`
	CompletionTokens int64   `gorm:"column:completion_tokens" json:"completion_tokens"`
	TotalTokens      int64   `gorm:"column:total_tokens" json:"total_tokens"`
	AvgDurationMs    float64 `gorm:"column:avg_duration_ms" json:"avg_duration_ms"`
}

// GetTokenStats 获取 token 用量统计
// GET /api/v1/ops/token-stats
func (h *OpsHandler) GetTokenStats(c *gin.Context) {
	timeRange := c.DefaultQuery("time_range", "24h")
	interval := timeRangeToInterval(timeRange)
	limit := 50
	if l, err := strconv.Atoi(c.DefaultQuery("limit", "50")); err == nil && l > 0 && l <= 200 {
		limit = l
	}

	items := make([]TokenStatsItem, 0)
	err := h.db.Raw(fmt.Sprintf(`
		SELECT provider, model,
			COUNT(*)::bigint as request_count,
			COALESCE(SUM(prompt_tokens), 0)::bigint as prompt_tokens,
			COALESCE(SUM(completion_tokens), 0)::bigint as completion_tokens,
			COALESCE(SUM(total_tokens), 0)::bigint as total_tokens,
			COALESCE(AVG(duration_ms), 0)::float as avg_duration_ms
		FROM call_logs
		WHERE ts > NOW() - INTERVAL '%s'
		GROUP BY provider, model
		ORDER BY total_tokens DESC
		LIMIT ?
	`, interval), limit).Scan(&items).Error

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query token stats"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// =============================================================================
// 阈值与看板设置
// =============================================================================

const settingKeyMetricThresholds = "ops_metric_thresholds"
const settingKeyDashboardSettings = "ops_dashboard_settings"

// defaultMetricThresholds 默认指标阈值
func defaultMetricThresholds() gin.H {
	return gin.H{
		"error_rate_warn":      0.05,
		"error_rate_critical":  0.10,
		"p95_ms_warn":          3000,
		"p95_ms_critical":      10000,
		"ttft_p95_ms_warn":     2000,
		"ttft_p95_ms_critical": 8000,
		"success_rate_min":     0.95,
		"concurrency_warn":     100,
	}
}

// defaultDashboardSettings 默认看板设置
// 字段对齐前端 OpsDashboardSettings 契约（B02/WP2）：除原有基础项外，
// 补 show_concurrency_card / show_switch_rate_chart / show_token_stats /
// show_alert_events / show_system_metrics 等"卡片可见性"开关（默认 true），
// 以及 auto_refresh_interval / default_query_mode。UpdateDashboardSettings
// 用 gin.H 反序列化可接住任意子集，缺失字段在 Get 时由本函数补默认。
func defaultDashboardSettings() gin.H {
	return gin.H{
		// 基础项（既有）
		"default_time_range":  "1h",
		"refresh_interval":    30,
		"show_system_metrics": true,
		"compact_mode":        false,
		// 卡片可见性开关（B02：前端 v-if 读这些字段，缺失会导致卡片消失）
		"show_concurrency_card":  true,
		"show_switch_rate_chart": true,
		"show_token_stats":       true,
		"show_alert_events":      true,
		// 自动刷新与查询模式（前端 OpsDashboardSettings 期望字段）
		"auto_refresh_interval": 30,
		"default_query_mode":    "auto",
	}
}

// GetMetricThresholds 获取指标阈值配置
// GET /api/v1/ops/settings/metric-thresholds
func (h *OpsHandler) GetMetricThresholds(c *gin.Context) {
	val, err := h.settingsRepo.GetValue(settingKeyMetricThresholds)
	if err != nil || val == "" {
		c.JSON(http.StatusOK, defaultMetricThresholds())
		return
	}
	var cfg gin.H
	if err := json.Unmarshal([]byte(val), &cfg); err != nil {
		c.JSON(http.StatusOK, defaultMetricThresholds())
		return
	}
	defs := defaultMetricThresholds()
	for k, v := range defs {
		if _, ok := cfg[k]; !ok {
			cfg[k] = v
		}
	}
	c.JSON(http.StatusOK, cfg)
}

// UpdateMetricThresholds 更新指标阈值配置
// PUT /api/v1/ops/settings/metric-thresholds
func (h *OpsHandler) UpdateMetricThresholds(c *gin.Context) {
	var input gin.H
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	data, _ := json.Marshal(input)
	if err := h.settingsRepo.SetMultiple(map[string]string{
		settingKeyMetricThresholds: string(data),
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save thresholds"})
		return
	}
	c.JSON(http.StatusOK, input)
}

// GetDashboardSettings 获取看板设置
// GET /api/v1/ops/settings/dashboard
func (h *OpsHandler) GetDashboardSettings(c *gin.Context) {
	val, err := h.settingsRepo.GetValue(settingKeyDashboardSettings)
	if err != nil || val == "" {
		c.JSON(http.StatusOK, defaultDashboardSettings())
		return
	}
	var cfg gin.H
	if err := json.Unmarshal([]byte(val), &cfg); err != nil {
		c.JSON(http.StatusOK, defaultDashboardSettings())
		return
	}
	defs := defaultDashboardSettings()
	for k, v := range defs {
		if _, ok := cfg[k]; !ok {
			cfg[k] = v
		}
	}
	c.JSON(http.StatusOK, cfg)
}

// UpdateDashboardSettings 更新看板设置
// PUT /api/v1/ops/settings/dashboard
func (h *OpsHandler) UpdateDashboardSettings(c *gin.Context) {
	var input gin.H
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	data, _ := json.Marshal(input)
	if err := h.settingsRepo.SetMultiple(map[string]string{
		settingKeyDashboardSettings: string(data),
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save dashboard settings"})
		return
	}
	c.JSON(http.StatusOK, input)
}

// =============================================================================
// 后台任务心跳
// =============================================================================

// ListJobs 查询后台任务心跳列表
// GET /api/v1/ops/jobs
func (h *OpsHandler) ListJobs(c *gin.Context) {
	jobs := make([]model.AlertJobHeartbeat, 0)
	if err := h.db.Order("name").Find(&jobs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list jobs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": jobs})
}

// =============================================================================
// 用户并发统计
// =============================================================================

// UserConcurrencyItem 用户并发统计项
type UserConcurrencyItem struct {
	UserID        string  `gorm:"column:user_id" json:"user_id"`
	UserName      string  `gorm:"column:user_name" json:"user_name"`
	RequestCount  int64   `gorm:"column:request_count" json:"request_count"`
	ErrorCount    int64   `gorm:"column:error_count" json:"error_count"`
	AvgDurationMs float64 `gorm:"column:avg_duration_ms" json:"avg_duration_ms"`
}

// GetUserConcurrency 获取用户并发统计
// GET /api/v1/ops/user-concurrency
func (h *OpsHandler) GetUserConcurrency(c *gin.Context) {
	items := make([]UserConcurrencyItem, 0)
	err := h.db.Raw(`
		SELECT
			user_id,
			COALESCE(NULLIF(MAX(user_name), ''), user_id) AS user_name,
			COUNT(*)::bigint AS request_count,
			SUM(CASE WHEN status >= 400 THEN 1 ELSE 0 END)::bigint AS error_count,
			COALESCE(AVG(duration_ms), 0)::float AS avg_duration_ms
		FROM call_logs
		WHERE ts > NOW() - INTERVAL '5 minutes'
			AND user_id != ''
			AND COALESCE(NULLIF(key_type, ''), 'personal') != 'service'
		GROUP BY user_id
		ORDER BY request_count DESC
		LIMIT 50
	`).Scan(&items).Error

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query user concurrency"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"items": items,
		"total": len(items),
	})
}

// =============================================================================
// 账户可用性统计（Provider Rate Limit 状态）
// =============================================================================

// AccountAvailabilityItem Provider 可用性项
type AccountAvailabilityItem struct {
	ProviderID            string `json:"provider_id"`
	IsRateLimited         bool   `json:"is_rate_limited"`
	RateLimitRemaining    int    `json:"rate_limit_remaining"`
	RateLimitResetSeconds int    `json:"rate_limit_reset_seconds"`
	ErrorCount            int64  `json:"error_count"`
}

// GetAccountAvailability 获取各 Provider 的 rate limit 状态
// GET /api/v1/ops/account-availability
func (h *OpsHandler) GetAccountAvailability(c *gin.Context) {
	items := make([]AccountAvailabilityItem, 0)

	if h.rdb == nil {
		c.JSON(http.StatusOK, gin.H{"items": []AccountAvailabilityItem{}, "total": 0})
		return
	}

	// 扫描 Redis keys matching pattern
	iter := h.rdb.Scan(c.Request.Context(), 0, "ratelimit:provider:*", 100).Iterator()
	for iter.Next(c.Request.Context()) {
		key := iter.Val()
		// key 格式: ratelimit:provider:{id}
		parts := strings.SplitN(key, ":", 3)
		if len(parts) != 3 {
			continue
		}
		providerID := parts[2]

		val, err := h.rdb.Get(c.Request.Context(), key).Result()
		if err != nil {
			continue
		}

		// 尝试解析 JSON，兼容不同存储格式
		var data struct {
			IsRateLimited         bool  `json:"is_rate_limited"`
			RateLimitRemaining    int   `json:"rate_limit_remaining"`
			RateLimitResetSeconds int   `json:"rate_limit_reset_seconds"`
			ErrorCount            int64 `json:"error_count"`
		}
		if jsonErr := json.Unmarshal([]byte(val), &data); jsonErr != nil {
			// 回退：简单计数
			count, _ := strconv.ParseInt(val, 10, 64)
			data.ErrorCount = count
		}

		items = append(items, AccountAvailabilityItem{
			ProviderID:            providerID,
			IsRateLimited:         data.IsRateLimited,
			RateLimitRemaining:    data.RateLimitRemaining,
			RateLimitResetSeconds: data.RateLimitResetSeconds,
			ErrorCount:            data.ErrorCount,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"items": items,
		"total": len(items),
	})
}

// =============================================================================
// 实时流量窗口
// =============================================================================

// GetRealtimeTraffic 获取实时流量指标
// GET /api/v1/ops/realtime-traffic
func (h *OpsHandler) GetRealtimeTraffic(c *gin.Context) {
	window := c.DefaultQuery("window", "5min")
	platform := c.Query("platform")
	groupID := c.Query("group_id")

	var interval string
	var windowSeconds float64
	switch window {
	case "1min":
		interval = "1 minute"
		windowSeconds = 60
	case "5min":
		interval = "5 minutes"
		windowSeconds = 300
	case "30min":
		interval = "30 minutes"
		windowSeconds = 1800
	case "1h":
		interval = "1 hour"
		windowSeconds = 3600
	default:
		interval = "5 minutes"
		windowSeconds = 300
	}

	where := fmt.Sprintf("WHERE ts > NOW() - INTERVAL '%s'", interval)
	args := []interface{}{}

	if platform != "" {
		where += " AND provider = ?"
		args = append(args, platform)
	}
	if groupID != "" {
		// group_id 是 model_groups 主键，需要解析为 group_name 列值
		var groupName string
		if err := h.db.Raw(
			"SELECT name FROM model_groups WHERE id = ?", groupID,
		).Scan(&groupName).Error; err == nil && groupName != "" {
			where += " AND group_name = ?"
			args = append(args, groupName)
		}
	}

	var traffic struct {
		TotalRequests int64   `gorm:"column:total_requests"`
		TotalTokens   int64   `gorm:"column:total_tokens"`
		DurationAvgMs float64 `gorm:"column:duration_avg_ms"`
	}

	err := h.db.Raw(fmt.Sprintf(`
		SELECT
			COUNT(*)::bigint AS total_requests,
			COALESCE(SUM(total_tokens), 0)::bigint AS total_tokens,
			COALESCE(AVG(duration_ms), 0)::float AS duration_avg_ms
		FROM call_logs %s
	`, where), args...).Scan(&traffic).Error

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query realtime traffic"})
		return
	}

	avgQPS := float64(traffic.TotalRequests) / windowSeconds
	avgTPS := float64(traffic.TotalTokens) / windowSeconds

	// 窗口内按分钟峰值（call_logs），保证切换 1min/5min/30min/1h 时 peak 随窗口变化
	var peakRow struct {
		PeakReqs   int64 `gorm:"column:peak_reqs"`
		PeakTokens int64 `gorm:"column:peak_tokens"`
	}
	_ = h.db.Raw(fmt.Sprintf(`
		SELECT COALESCE(MAX(reqs), 0)::bigint AS peak_reqs,
		       COALESCE(MAX(tokens), 0)::bigint AS peak_tokens
		FROM (
			SELECT date_trunc('minute', ts) AS m,
			       COUNT(*)::bigint AS reqs,
			       COALESCE(SUM(total_tokens), 0)::bigint AS tokens
			FROM call_logs %s
			GROUP BY 1
		) t
	`, where), args...).Scan(&peakRow)
	peakQPS := float64(peakRow.PeakReqs) / 60.0
	peakTPS := float64(peakRow.PeakTokens) / 60.0

	// Redis 最近 1 分钟瞬时值（有则覆盖 current；窗口平均值始终放在 avg_*）
	instantQPS, instantTPS := 0.0, 0.0
	if h.rdb != nil {
		now := time.Now()
		minuteKey := now.Format("200601021504")
		qpsCount, _ := h.rdb.Get(c.Request.Context(), "ops:qps:"+minuteKey).Int64()
		tpsCount, _ := h.rdb.Get(c.Request.Context(), "ops:tps:"+minuteKey).Int64()
		elapsed := float64(now.Second() + 1)
		if elapsed > 60 {
			elapsed = 60
		}
		instantQPS = float64(qpsCount) / elapsed
		instantTPS = float64(tpsCount) / elapsed
		if q := float64(qpsCount) / 60.0; q > peakQPS {
			peakQPS = q
		}
		if t := float64(tpsCount) / 60.0; t > peakTPS {
			peakTPS = t
		}
	}

	// current_*：窗口切换必须可见差异 → 用窗口平均；1min 且 Redis 有瞬时值时优先瞬时
	currentQPS, currentTPS := avgQPS, avgTPS
	if window == "1min" && (instantQPS > 0 || instantTPS > 0) {
		currentQPS, currentTPS = instantQPS, instantTPS
	}

	c.JSON(http.StatusOK, OpsTraffic{
		TotalRequests: traffic.TotalRequests,
		TotalTokens:   traffic.TotalTokens,
		AvgQPS:        avgQPS,
		AvgTPS:        avgTPS,
		CurrentQPS:    currentQPS,
		CurrentTPS:    currentTPS,
		PeakQPS:       peakQPS,
		PeakTPS:       peakTPS,
	})
}

// =============================================================================
// 高级设置
// =============================================================================

const settingKeyAdvancedSettings = "ops_advanced_settings"

// AdvancedSettings 运维看板高级设置
type AdvancedSettings struct {
	AutoRefreshEnabled  bool `json:"auto_refresh_enabled"`
	DataRetentionDays   int  `json:"data_retention_days"`
	ErrorLogRetention   int  `json:"error_log_retention_days"`
	IgnoreContextCancel bool `json:"ignore_context_canceled"`
	Ignore429Errors     bool `json:"ignore_429_errors"`
}

// defaultAdvancedSettings 默认高级设置
func defaultAdvancedSettings() AdvancedSettings {
	return AdvancedSettings{
		AutoRefreshEnabled:  true,
		DataRetentionDays:   30,
		ErrorLogRetention:   90,
		IgnoreContextCancel: false,
		Ignore429Errors:     false,
	}
}

// GetAdvancedSettings 获取高级设置
// GET /api/v1/ops/settings/advanced
func (h *OpsHandler) GetAdvancedSettings(c *gin.Context) {
	val, err := h.settingsRepo.GetValue(settingKeyAdvancedSettings)
	if err != nil || val == "" {
		c.JSON(http.StatusOK, defaultAdvancedSettings())
		return
	}

	var settings AdvancedSettings
	if jsonErr := json.Unmarshal([]byte(val), &settings); jsonErr != nil {
		c.JSON(http.StatusOK, defaultAdvancedSettings())
		return
	}

	// 合并默认值（新字段补全）
	defs := defaultAdvancedSettings()
	if settings.DataRetentionDays == 0 {
		settings.DataRetentionDays = defs.DataRetentionDays
	}
	if settings.ErrorLogRetention == 0 {
		settings.ErrorLogRetention = defs.ErrorLogRetention
	}

	c.JSON(http.StatusOK, settings)
}

// UpdateAdvancedSettings 更新高级设置
// PUT /api/v1/ops/settings/advanced
func (h *OpsHandler) UpdateAdvancedSettings(c *gin.Context) {
	var input AdvancedSettings
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	data, _ := json.Marshal(input)
	if err := h.settingsRepo.SetMultiple(map[string]string{
		settingKeyAdvancedSettings: string(data),
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save advanced settings"})
		return
	}

	c.JSON(http.StatusOK, input)
}

// =============================================================================
// 请求错误关联
// =============================================================================

// ErrorCorrelationItem 上游错误关联项
type ErrorCorrelationItem struct {
	Provider             string `gorm:"column:provider" json:"provider"`
	UpstreamStatus       int    `gorm:"column:upstream_status" json:"upstream_status"`
	UpstreamErrorMessage string `gorm:"column:upstream_error_message" json:"upstream_error_message"`
	TS                   string `gorm:"column:ts" json:"ts"`
	DurationMs           int    `gorm:"column:duration_ms" json:"duration_ms"`
}

// GetRequestErrorCorrelation 获取请求错误关联的上游错误
// GET /api/v1/ops/request-errors/:id/correlation
func (h *OpsHandler) GetRequestErrorCorrelation(c *gin.Context) {
	requestID := c.Param("id")

	// 查找同一 request_id 且 status >= 500 的上游错误记录
	items := make([]ErrorCorrelationItem, 0)
	err := h.db.Raw(`
		SELECT
			provider,
			upstream_status,
			'' AS upstream_error_message,
			ts::text AS ts,
			duration_ms
		FROM call_logs
		WHERE request_id = ?
			AND status >= 500
		ORDER BY ts DESC
		LIMIT 20
	`, requestID).Scan(&items).Error

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query error correlation"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"request_id": requestID,
		"items":      items,
		"total":      len(items),
	})
}

// =============================================================================
// 增强 Request / Upstream Error Detail（upstream_context + latency_breakdown）
// =============================================================================

// UpstreamContext 上游上下文
type UpstreamContext struct {
	UpstreamStatusCode   int    `json:"upstream_status_code"`
	UpstreamErrorMessage string `json:"upstream_error_message"`
	UpstreamEndpoint     string `json:"upstream_endpoint"`
}

// LatencyBreakdown 延迟拆解
type LatencyBreakdown struct {
	AuthMs     int `json:"auth_ms"`
	RoutingMs  int `json:"routing_ms"`
	UpstreamMs int `json:"upstream_ms"`
	ResponseMs int `json:"response_ms"`
	TTFTMs     int `json:"ttft_ms"`
}

// OpsGroupOption 模型组下拉选项
type OpsGroupOption struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

// GetGroupOptions 获取模型组下拉列表
// GET /api/v1/ops/groups
func (h *OpsHandler) GetGroupOptions(c *gin.Context) {
	groups := make([]OpsGroupOption, 0)
	err := h.db.Raw(`SELECT id, name FROM model_groups WHERE deleted_at IS NULL AND enabled = TRUE ORDER BY name`).Scan(&groups).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query groups"})
		return
	}
	c.JSON(http.StatusOK, groups)
}

// OpsProviderOption Provider 选项
type OpsProviderOption struct {
	Name string `json:"name"`
}

// ListProviders 获取所有 providers 列表（去重）
// GET /api/v1/ops/providers
func (h *OpsHandler) ListProviders(c *gin.Context) {
	providers := make([]OpsProviderOption, 0)
	err := h.db.Raw(`
		SELECT DISTINCT provider as name 
		FROM call_logs 
		WHERE ts > NOW() - INTERVAL '7 days'
		ORDER BY provider
	`).Scan(&providers).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query providers"})
		return
	}
	c.JSON(http.StatusOK, providers)
}

// OpsModelOption Model 选项（按使用量排序）
type OpsModelOption struct {
	Name       string `json:"name"`
	TotalCalls int64  `json:"total_calls"`
}

// ListModels 获取所有 models 列表（按使用量排序，最多 50 个）
// GET /api/v1/ops/models
func (h *OpsHandler) ListModels(c *gin.Context) {
	models := make([]OpsModelOption, 0)
	err := h.db.Raw(`
		SELECT model as name, COUNT(*) as total_calls
		FROM call_logs 
		WHERE ts > NOW() - INTERVAL '7 days'
		GROUP BY model
		ORDER BY total_calls DESC
		LIMIT 50
	`).Scan(&models).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query models"})
		return
	}
	c.JSON(http.StatusOK, models)
}

// =============================================================================
// 邮件通知配置
// =============================================================================

const settingKeyEmailNotification = "ops_email_notification_config"

// EmailNotificationConfig 邮件通知配置
type EmailNotificationConfig struct {
	Enabled          bool     `json:"enabled"`
	AlertRecipients  []string `json:"alert_recipients"`
	ReportRecipients []string `json:"report_recipients"`
	MinSeverity      string   `json:"min_severity"`
	IncludeResolved  bool     `json:"include_resolved"`
	DailySummary     bool     `json:"daily_summary"`
}

func defaultEmailNotificationConfig() EmailNotificationConfig {
	return EmailNotificationConfig{
		Enabled:          false,
		AlertRecipients:  []string{},
		ReportRecipients: []string{},
		MinSeverity:      "",
		IncludeResolved:  false,
		DailySummary:     false,
	}
}

// GetEmailNotificationConfig 获取邮件通知配置
// GET /api/v1/ops/settings/email-notification
func (h *OpsHandler) GetEmailNotificationConfig(c *gin.Context) {
	val, err := h.settingsRepo.GetValue(settingKeyEmailNotification)
	if err != nil || val == "" {
		c.JSON(http.StatusOK, defaultEmailNotificationConfig())
		return
	}

	var cfg EmailNotificationConfig
	if jsonErr := json.Unmarshal([]byte(val), &cfg); jsonErr != nil {
		c.JSON(http.StatusOK, defaultEmailNotificationConfig())
		return
	}

	if cfg.AlertRecipients == nil {
		cfg.AlertRecipients = []string{}
	}
	if cfg.ReportRecipients == nil {
		cfg.ReportRecipients = []string{}
	}

	c.JSON(http.StatusOK, cfg)
}

// UpdateEmailNotificationConfig 更新邮件通知配置
// PUT /api/v1/ops/settings/email-notification
func (h *OpsHandler) UpdateEmailNotificationConfig(c *gin.Context) {
	var input EmailNotificationConfig
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if input.AlertRecipients == nil {
		input.AlertRecipients = []string{}
	}
	if input.ReportRecipients == nil {
		input.ReportRecipients = []string{}
	}

	data, _ := json.Marshal(input)
	if err := h.settingsRepo.SetMultiple(map[string]string{
		settingKeyEmailNotification: string(data),
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save email notification config"})
		return
	}

	c.JSON(http.StatusOK, input)
}

// SendTestAlertEmail 发送测试告警邮件
// POST /api/v1/ops/settings/email-notification/test
func (h *OpsHandler) SendTestAlertEmail(c *gin.Context) {
	var req struct {
		Recipient string `json:"recipient" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "recipient is required"})
		return
	}

	// TODO: 接入实际邮件发送（依赖 email_sender 模块）
	c.JSON(http.StatusOK, gin.H{"sent": true, "recipient": req.Recipient, "message": "test email queued (email sender not yet wired)"})
}

// =============================================================================
// 告警运行时设置
// =============================================================================

const settingKeyAlertRuntime = "ops_alert_runtime_settings"

// AlertRuntimeSettings 告警运行时设置
type AlertRuntimeSettings struct {
	EvaluationInterval int                    `json:"evaluation_interval_seconds"`
	Thresholds         AlertThresholdSettings `json:"thresholds"`
	DistributedLock    DistributedLockConfig  `json:"distributed_lock"`
}

// AlertThresholdSettings 阈值覆写
type AlertThresholdSettings struct {
	SLAMin               *float64 `json:"sla_percent_min"`
	TTFTP99Max           *float64 `json:"ttft_p99_ms_max"`
	ReqErrorRateMax      *float64 `json:"request_error_rate_percent_max"`
	UpstreamErrorRateMax *float64 `json:"upstream_error_rate_percent_max"`
}

// DistributedLockConfig 分布式锁配置
type DistributedLockConfig struct {
	Enabled    bool   `json:"enabled"`
	Key        string `json:"key"`
	TTLSeconds int    `json:"ttl_seconds"`
}

func defaultAlertRuntimeSettings() AlertRuntimeSettings {
	sla := 99.0
	ttft := 5000.0
	reqErr := 5.0
	upErr := 10.0
	return AlertRuntimeSettings{
		EvaluationInterval: 60,
		Thresholds: AlertThresholdSettings{
			SLAMin:               &sla,
			TTFTP99Max:           &ttft,
			ReqErrorRateMax:      &reqErr,
			UpstreamErrorRateMax: &upErr,
		},
		DistributedLock: DistributedLockConfig{
			Enabled:    false,
			Key:        "ops:alert_eval_lock",
			TTLSeconds: 30,
		},
	}
}

// GetAlertRuntimeSettings 获取告警运行时设置
// GET /api/v1/ops/settings/alert-runtime
func (h *OpsHandler) GetAlertRuntimeSettings(c *gin.Context) {
	val, err := h.settingsRepo.GetValue(settingKeyAlertRuntime)
	if err != nil || val == "" {
		c.JSON(http.StatusOK, defaultAlertRuntimeSettings())
		return
	}

	var settings AlertRuntimeSettings
	if jsonErr := json.Unmarshal([]byte(val), &settings); jsonErr != nil {
		c.JSON(http.StatusOK, defaultAlertRuntimeSettings())
		return
	}

	if settings.EvaluationInterval <= 0 {
		settings.EvaluationInterval = 60
	}
	if settings.DistributedLock.TTLSeconds <= 0 {
		settings.DistributedLock.TTLSeconds = 30
	}

	c.JSON(http.StatusOK, settings)
}

// UpdateAlertRuntimeSettings 更新告警运行时设置
// PUT /api/v1/ops/settings/alert-runtime
func (h *OpsHandler) UpdateAlertRuntimeSettings(c *gin.Context) {
	var input AlertRuntimeSettings
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if input.EvaluationInterval < 1 || input.EvaluationInterval > 86400 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "evaluation_interval_seconds must be 1~86400"})
		return
	}

	if input.DistributedLock.Enabled {
		if len(input.DistributedLock.Key) < 3 || !strings.HasPrefix(input.DistributedLock.Key, "ops:") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "distributed_lock.key must start with 'ops:' and length >= 3"})
			return
		}
		if input.DistributedLock.TTLSeconds < 1 || input.DistributedLock.TTLSeconds > 86400 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "distributed_lock.ttl_seconds must be 1~86400"})
			return
		}
	}

	data, _ := json.Marshal(input)
	if err := h.settingsRepo.SetMultiple(map[string]string{
		settingKeyAlertRuntime: string(data),
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save alert runtime settings"})
		return
	}

	c.JSON(http.StatusOK, input)
}

// =============================================================================
// 错误日志（增强视图）
// =============================================================================

// ErrorLogEntry 增强版错误日志条目
type ErrorLogEntry struct {
	ID                   uint64  `gorm:"column:id" json:"id"`
	Ts                   string  `gorm:"column:ts" json:"ts"`
	Model                string  `gorm:"column:model" json:"model"`
	Provider             string  `gorm:"column:provider" json:"provider"`
	GroupName            string  `gorm:"column:group_name" json:"group_name"`
	Status               *int    `gorm:"column:status" json:"status"`
	ErrorMessage         string  `gorm:"column:error_message" json:"error_message"`
	DurationMs           *int    `gorm:"column:duration_ms" json:"duration_ms"`
	RequestID            *string `gorm:"column:request_id" json:"request_id"`
	ClientRequestID      *string `gorm:"column:client_request_id" json:"client_request_id"`
	Phase                string  `gorm:"column:phase" json:"phase"`
	UserID               *string `gorm:"column:user_id" json:"user_id"`
	UpstreamStatus       *int    `gorm:"column:upstream_status" json:"upstream_status"`
	UpstreamErrorMessage *string `gorm:"column:upstream_error_message" json:"upstream_error_message"`
	UpstreamEndpoint     *string `gorm:"column:upstream_endpoint" json:"upstream_endpoint"`
	InboundEndpoint      *string `gorm:"column:inbound_endpoint" json:"inbound_endpoint"`
	ApiKeyPrefix         *string `gorm:"column:api_key_prefix" json:"api_key_prefix"`
	Resolved             bool    `gorm:"column:resolved" json:"resolved"`
}

// ListErrorLogs 查询错误日志（从 system_logs 表过滤 ERROR 级别）
// GET /api/v1/ops/error-logs
func (h *OpsHandler) ListErrorLogs(c *gin.Context) {
	timeRange := c.DefaultQuery("time_range", "1h")
	interval := timeRangeToInterval(timeRange)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 20
	}
	if page <= 0 {
		page = 1
	}

	phase := c.Query("phase")
	statusStr := c.Query("status")
	search := c.Query("search")
	sortBy := c.DefaultQuery("sort_by", "ts")
	sortOrder := c.DefaultQuery("sort_order", "desc")

	if sortBy != "ts" && sortBy != "status" && sortBy != "duration_ms" {
		sortBy = "ts"
	}
	if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = "desc"
	}

	baseWhere := fmt.Sprintf(`level IN ('ERROR','WARN') AND ts >= NOW() - INTERVAL '%s'`, interval)

	args := []interface{}{}
	if phase != "" {
		baseWhere += " AND component = ?"
		args = append(args, phase)
	}
	if statusStr != "" {
		if st, err := strconv.Atoi(statusStr); err == nil {
			baseWhere += " AND status = ?"
			args = append(args, st)
		}
	}
	if search != "" {
		baseWhere += " AND (model ILIKE ? OR provider ILIKE ? OR request_id = ? OR message ILIKE ?)"
		like := "%" + search + "%"
		args = append(args, like, like, search, like)
	}

	var total int64
	countSQL := fmt.Sprintf(`SELECT COUNT(*) FROM system_logs WHERE %s`, baseWhere)
	h.db.Raw(countSQL, args...).Scan(&total)

	orderBy := fmt.Sprintf("ORDER BY %s %s, id DESC", sortBy, sortOrder)
	offset := (page - 1) * pageSize

	querySQL := fmt.Sprintf(`
		SELECT
			id, ts,
			COALESCE(model,'') AS model,
			COALESCE(provider,'') AS provider,
			COALESCE("group",'') AS group_name,
			status,
			COALESCE(error_message,'') AS error_message,
			duration_ms,
			request_id, client_request_id,
			component AS phase,
			user_id
		FROM system_logs
		WHERE %s
		%s
		LIMIT %d OFFSET %d
	`, baseWhere, orderBy, pageSize, offset)

	items := make([]ErrorLogEntry, 0)
	h.db.Raw(querySQL, args...).Scan(&items)

	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// ResolveErrorLog 标记/取消标记错误日志为已解决
// PUT /api/v1/ops/error-logs/:id/resolve
func (h *OpsHandler) ResolveErrorLog(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var req struct {
		Resolved bool `json:"resolved"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result := h.db.Exec(`UPDATE system_logs SET extra = COALESCE(extra,'{}')::jsonb || jsonb_build_object('resolved', $1::boolean) WHERE id = $2`, req.Resolved, id)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"id": id, "resolved": req.Resolved})
}

// ResolveRequestError 标记请求错误为已解决
// PUT /api/v1/ops/request-errors/:id/resolve
//
// B05/WP3 降级方案：request-errors / upstream-errors 来源于 call_logs，主键是
// request_id（字符串），无数字 id；而 resolveErrorLog 用 strconv.ParseUint(id)
// 匹配 system_logs.id —— 对这两类 resolve 根本不可用（id 不是数字，且表也不对）。
// 完整方案需新建 error_resolve_states 表（WP11），本次不做。此处返回 409 明确
// 语义，前端对该类按钮禁用并提示"请用错误日志视图"。
func (h *OpsHandler) ResolveRequestError(c *gin.Context) {
	c.JSON(http.StatusConflict, gin.H{
		"error": "request-errors resolve not supported in-place; use the error-logs view",
		"hint":  "该类错误（来自 call_logs）不支持就地标记，请用错误日志视图操作",
	})
}

// ResolveUpstreamError 标记上游错误为已解决
// PUT /api/v1/ops/upstream-errors/:id/resolve
// （同 ResolveRequestError 降级方案，返回 409）
func (h *OpsHandler) ResolveUpstreamError(c *gin.Context) {
	c.JSON(http.StatusConflict, gin.H{
		"error": "upstream-errors resolve not supported in-place; use the error-logs view",
		"hint":  "该类错误（来自 call_logs）不支持就地标记，请用错误日志视图操作",
	})
}

// resolveErrorLog 仅供 error-logs 视图使用：error-logs 本身查 system_logs，
// 主键是数字 id，故 ParseUint + UPDATE system_logs 可用。
// 注意：SQL 含 ::jsonb 方言，SQLite 上不可跑（error-logs resolve 测试在真
// Postgres 或 dialect-aware 后补，见 WP3/WP11）。
func (h *OpsHandler) resolveErrorLog(c *gin.Context, logType string) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var req struct {
		Resolved bool `json:"resolved"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result := h.db.Exec(
		`UPDATE system_logs SET extra = COALESCE(extra,'{}')::jsonb || jsonb_build_object('resolved', $1::boolean) WHERE id = $2`,
		req.Resolved, id,
	)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"id": id, "type": logType, "resolved": req.Resolved})
}

// GetSnapshotV2 聚合响应：overview + throughput_trend + error_trend
// GET /api/v1/ops/dashboard/snapshot-v2
func (h *OpsHandler) GetSnapshotV2(c *gin.Context) {
	filter := OpsFilter{
		TimeRange: c.DefaultQuery("time_range", "1h"),
		GroupID:   c.Query("group_id"),
		Provider:  c.Query("provider"),
		Model:     c.Query("model"),
	}

	// overview 与 GetDashboard 完全共用同一构建函数，保证字段结构一致
	ov, err := h.buildOpsOverviewWithFilter(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// throughput trend（字段需与 GetTrafficTrend / 前端 OpsTrafficPoint 一致：含 qps/tps）
	interval := timeRangeToInterval(filter.TimeRange)
	truncExpr := bucketTruncExpr(filter.TimeRange)
	sec := bucketSeconds(filter.TimeRange)
	whereClause, args := h.buildFilterClause(c.Request.Context(), filter, interval)

	var throughputRows []struct {
		Timestamp    time.Time `gorm:"column:ts" json:"timestamp"`
		RequestCount int64     `gorm:"column:request_count" json:"request_count"`
		TokenCount   int64     `gorm:"column:token_count" json:"token_count"`
	}
	h.db.Raw(fmt.Sprintf(`
		SELECT %s as ts, COUNT(*)::bigint as request_count, COALESCE(SUM(total_tokens), 0)::bigint as token_count
		FROM call_logs WHERE %s
		GROUP BY %s ORDER BY ts
	`, truncExpr, whereClause, truncExpr), args...).Scan(&throughputRows)

	throughputPoints := make([]gin.H, 0, len(throughputRows))
	for _, r := range throughputRows {
		throughputPoints = append(throughputPoints, gin.H{
			"timestamp":     r.Timestamp.Format(time.RFC3339),
			"qps":           float64(r.RequestCount) / sec,
			"tps":           float64(r.TokenCount) / sec,
			"request_count": r.RequestCount,
			"token_count":   r.TokenCount,
		})
	}

	// error trend
	var errorRows []struct {
		Timestamp      time.Time `gorm:"column:ts" json:"timestamp"`
		RequestErrors  int64     `gorm:"column:request_errors" json:"request_errors"`
		UpstreamErrors int64     `gorm:"column:upstream_errors" json:"upstream_errors"`
		BusinessErrors int64     `gorm:"column:business_errors" json:"business_errors"`
	}
	h.db.Raw(fmt.Sprintf(`
		SELECT %s as ts,
			COUNT(*) FILTER (WHERE status >= 400 AND status < 500)::bigint as request_errors,
			COUNT(*) FILTER (WHERE status >= 500)::bigint as upstream_errors,
			COUNT(*) FILTER (WHERE status = 429)::bigint as business_errors
		FROM call_logs WHERE %s
		GROUP BY %s ORDER BY ts
	`, truncExpr, whereClause, truncExpr), args...).Scan(&errorRows)

	errorPoints := make([]gin.H, 0, len(errorRows))
	for _, r := range errorRows {
		errorPoints = append(errorPoints, gin.H{
			"timestamp":       r.Timestamp.Format(time.RFC3339),
			"request_errors":  r.RequestErrors,
			"upstream_errors": r.UpstreamErrors,
			"business_errors": r.BusinessErrors,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"generated_at": time.Now(),
		"overview":     ov,
		"throughput_trend": gin.H{
			"points":    throughputPoints,
			"dimension": "time",
		},
		"error_trend": gin.H{
			"points": errorPoints,
		},
	})
}
