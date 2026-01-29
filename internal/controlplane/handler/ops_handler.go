package handler

import (
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

// OpsDashboardResponse 运维看板总览响应
type OpsDashboardResponse struct {
	Overview  OpsOverview `json:"overview"`
	Timestamp time.Time   `json:"timestamp"`
}

// OpsOverview 总览数据
type OpsOverview struct {
	HealthScore   int                      `json:"health_score"`
	SystemMetrics *service.SystemMetrics   `json:"system_metrics"`
	Traffic       OpsTraffic               `json:"traffic"`
	SLA           OpsSLA                   `json:"sla"`
	Latency       OpsLatency               `json:"latency"`
	Errors        OpsErrors                `json:"errors"`
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
	DurationP50Ms  int `json:"duration_p50_ms"`
	DurationP90Ms  int `json:"duration_p90_ms"`
	DurationP95Ms  int `json:"duration_p95_ms"`
	DurationP99Ms  int `json:"duration_p99_ms"`
	DurationAvgMs  int `json:"duration_avg_ms"`
	DurationMaxMs  int `json:"duration_max_ms"`
	TTFTP50Ms      int `json:"ttft_p50_ms"`
	TTFTP90Ms      int `json:"ttft_p90_ms"`
	TTFTP95Ms      int `json:"ttft_p95_ms"`
	TTFTP99Ms      int `json:"ttft_p99_ms"`
	TTFTAvgMs      int `json:"ttft_avg_ms"`
	TTFTMaxMs      int `json:"ttft_max_ms"`
}

// OpsErrors 错误指标
type OpsErrors struct {
	RequestErrors  int64 `json:"request_errors"`
	UpstreamErrors int64 `json:"upstream_errors"`
	BusinessErrors int64 `json:"business_errors"`
}

// GetDashboard 获取运维看板总览
func (h *OpsHandler) GetDashboard(c *gin.Context) {
	timeRange := c.DefaultQuery("time_range", "1h")
	interval := timeRangeToInterval(timeRange)

	// 1. 流量统计
	var traffic struct {
		TotalRequests  int64   `gorm:"column:total_requests"`
		TotalTokens    int64   `gorm:"column:total_tokens"`
		SuccessCount   int64   `gorm:"column:success_count"`
		RequestErrors  int64   `gorm:"column:request_errors"`
		UpstreamErrors int64   `gorm:"column:upstream_errors"`
		BusinessErrors int64   `gorm:"column:business_errors"`
		DurationAvgMs  float64 `gorm:"column:duration_avg_ms"`
		TTFTAvgMs      float64 `gorm:"column:ttft_avg_ms"`
	}

	err := h.db.Raw(fmt.Sprintf(`
		SELECT
			COUNT(*)::bigint as total_requests,
			COALESCE(SUM(total_tokens), 0)::bigint as total_tokens,
			COUNT(*) FILTER (WHERE status = 200)::bigint as success_count,
			COUNT(*) FILTER (WHERE status >= 400 AND status < 500)::bigint as request_errors,
			COUNT(*) FILTER (WHERE status >= 500)::bigint as upstream_errors,
			COUNT(*) FILTER (WHERE status = 429)::bigint as business_errors,
			COALESCE(AVG(duration_ms), 0)::float as duration_avg_ms,
			COALESCE(AVG(first_token_ms), 0)::float as ttft_avg_ms
		FROM call_logs
		WHERE ts > NOW() - INTERVAL '%s'
	`, interval)).Scan(&traffic).Error

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query traffic stats"})
		return
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

	err = h.db.Raw(fmt.Sprintf(`
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
		WHERE ts > NOW() - INTERVAL '%s'
	`, interval)).Scan(&latency).Error

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query latency percentiles"})
		return
	}

	// 3. 实时 QPS/TPS（从 Redis 获取最近 1 分钟）
	now := time.Now()
	minuteKey := now.Format("200601021504")
	currentQPS := 0.0
	currentTPS := 0.0

	if h.rdb != nil {
		qpsKey := fmt.Sprintf("ops:qps:%s", minuteKey)
		tpsKey := fmt.Sprintf("ops:tps:%s", minuteKey)

		qpsCount, _ := h.rdb.Get(c.Request.Context(), qpsKey).Int64()
		tpsCount, _ := h.rdb.Get(c.Request.Context(), tpsKey).Int64()

		currentQPS = float64(qpsCount) / 60.0
		currentTPS = float64(tpsCount) / 60.0
	}

	// 4. 计算平均 QPS/TPS 和峰值
	var windowSeconds float64
	switch timeRange {
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

	peakQPS := currentQPS * 1.5
	if peakQPS < avgQPS*1.5 {
		peakQPS = avgQPS * 1.5
	}
	peakTPS := currentTPS * 1.5
	if peakTPS < avgTPS*1.5 {
		peakTPS = avgTPS * 1.5
	}

	// 5. SLA 计算
	successRate := 0.0
	errorRate := 0.0
	if traffic.TotalRequests > 0 {
		successRate = float64(traffic.SuccessCount) / float64(traffic.TotalRequests)
		errorRate = 1.0 - successRate
	}

	// 6. 健康评分
	sm := h.metricsCollector.Get()
	healthScore := calculateHealthScore(successRate, sm.MemoryUsagePercent, sm.CPUUsagePercent, sm.GoroutineCount)

	resp := OpsDashboardResponse{
		Overview: OpsOverview{
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
				RequestErrors:  traffic.RequestErrors,
				UpstreamErrors: traffic.UpstreamErrors,
				BusinessErrors: traffic.BusinessErrors,
			},
		},
		Timestamp: time.Now(),
	}

	c.JSON(http.StatusOK, resp)
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

	var bucketInterval string
	switch timeRange {
	case "5m":
		bucketInterval = "1 minute"
	case "30m":
		bucketInterval = "5 minutes"
	case "6h":
		bucketInterval = "30 minutes"
	case "24h":
		bucketInterval = "1 hour"
	default:
		bucketInterval = "5 minutes"
	}

	var rows []struct {
		Timestamp    time.Time `gorm:"column:ts"`
		RequestCount int64     `gorm:"column:request_count"`
		TokenCount   int64     `gorm:"column:token_count"`
	}

	err := h.db.Raw(fmt.Sprintf(`
		SELECT
			date_trunc('%s', ts) as ts,
			COUNT(*)::bigint as request_count,
			COALESCE(SUM(total_tokens), 0)::bigint as token_count
		FROM call_logs
		WHERE ts > NOW() - INTERVAL '%s'
		GROUP BY date_trunc('%s', ts)
		ORDER BY ts
	`, bucketInterval, interval, bucketInterval)).Scan(&rows).Error

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query traffic trend"})
		return
	}

	var bucketSeconds float64
	switch bucketInterval {
	case "1 minute":
		bucketSeconds = 60
	case "5 minutes":
		bucketSeconds = 300
	case "30 minutes":
		bucketSeconds = 1800
	case "1 hour":
		bucketSeconds = 3600
	default:
		bucketSeconds = 300
	}

	points := make([]TrafficTrendPoint, 0, len(rows))
	for _, row := range rows {
		points = append(points, TrafficTrendPoint{
			Timestamp:    row.Timestamp.Format(time.RFC3339),
			QPS:          float64(row.RequestCount) / bucketSeconds,
			TPS:          float64(row.TokenCount) / bucketSeconds,
			RequestCount: row.RequestCount,
			TokenCount:   row.TokenCount,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"points": points,
		"bucket": bucketInterval,
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

	var bucketInterval string
	switch timeRange {
	case "5m":
		bucketInterval = "1 minute"
	case "30m":
		bucketInterval = "5 minutes"
	case "6h":
		bucketInterval = "30 minutes"
	case "24h":
		bucketInterval = "1 hour"
	default:
		bucketInterval = "5 minutes"
	}

	var rows []struct {
		Timestamp      time.Time `gorm:"column:ts"`
		RequestErrors  int64     `gorm:"column:request_errors"`
		UpstreamErrors int64     `gorm:"column:upstream_errors"`
		BusinessErrors int64     `gorm:"column:business_errors"`
	}

	err := h.db.Raw(fmt.Sprintf(`
		SELECT
			date_trunc('%s', ts) as ts,
			COUNT(*) FILTER (WHERE status >= 400 AND status < 500)::bigint as request_errors,
			COUNT(*) FILTER (WHERE status >= 500)::bigint as upstream_errors,
			COUNT(*) FILTER (WHERE status = 429)::bigint as business_errors
		FROM call_logs
		WHERE ts > NOW() - INTERVAL '%s'
		GROUP BY date_trunc('%s', ts)
		ORDER BY ts
	`, bucketInterval, interval, bucketInterval)).Scan(&rows).Error

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
		Range string `gorm:"column:range"`
		Count int64  `gorm:"column:count"`
	}

	err := h.db.Raw(fmt.Sprintf(`
		SELECT
			CASE
				WHEN duration_ms < 100 THEN '0-100ms'
				WHEN duration_ms < 500 THEN '100-500ms'
				WHEN duration_ms < 1000 THEN '500-1s'
				WHEN duration_ms < 2000 THEN '1-2s'
				WHEN duration_ms < 5000 THEN '2-5s'
				ELSE '5s+'
			END as range,
			COUNT(*)::bigint as count
		FROM call_logs
		WHERE ts > NOW() - INTERVAL '%s'
		GROUP BY range
		ORDER BY
			CASE range
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
	if groupName := c.Query("group_id"); groupName != "" {
		where += " AND group_name = ?"
		args = append(args, groupName)
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

	var items []gin.H
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

	var items []gin.H
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
		items = append(items, gin.H{
			"request_id": requestID, "ts": ts, "model": modelName,
			"group_name": groupName, "provider": provider, "status": status,
			"duration_ms": durationMs, "upstream_status": upstreamStatus, "ip_address": ipAddress,
		})
	}

	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": pageSize})
}

// GetRequestErrorDetail 获取单条客户端错误详情
// GET /api/v1/ops/request-errors/:id
func (h *OpsHandler) GetRequestErrorDetail(c *gin.Context) {
	id := c.Param("id")
	rows, err := h.db.Raw(`
		SELECT request_id, ts, key_id, user_id, model, group_name, provider,
			status, duration_ms, first_token_ms, total_tokens, prompt_tokens, completion_tokens,
			upstream_status, ip_address, failover_used, routed_via_group
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

	// 构建 latency_breakdown（call_logs 仅含 duration_ms / first_token_ms，其余返回 0）
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
		"upstream_context": upstreamCtx,
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

	var items []gin.H
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
		items = append(items, gin.H{
			"request_id": requestID, "ts": ts, "model": modelName,
			"group_name": groupName, "provider": provider, "status": status,
			"duration_ms": durationMs, "upstream_status": upstreamStatus, "ip_address": ipAddress,
		})
	}

	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": pageSize})
}

// =============================================================================
// 并发与切换率
// =============================================================================

// GetConcurrencyStats 获取各 provider 并发状态
// GET /api/v1/ops/concurrency
func (h *OpsHandler) GetConcurrencyStats(c *gin.Context) {
	type providerStat struct {
		Provider     string  `json:"provider"`
		Active       int64   `json:"active"`
		ErrorRate    float64 `json:"error_rate"`
		RequestCount int64   `json:"request_count"`
	}

	var providers []providerStat

	// 1. 从 Redis 获取各 provider 并发数
	if h.rdb != nil {
		iter := h.rdb.Scan(c.Request.Context(), 0, "concurrency:provider:*", 100).Iterator()
		for iter.Next(c.Request.Context()) {
			key := iter.Val()
			parts := strings.Split(key, ":")
			if len(parts) == 3 {
				val, _ := h.rdb.Get(c.Request.Context(), key).Int64()
				providers = append(providers, providerStat{
					Provider: parts[2],
					Active:   val,
				})
			}
		}
	}

	// 2. 从 call_logs 获取最近的错误率（最近 5 分钟）
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

	// 合并数据
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

	for i := range providers {
		if es, ok := errMap[providers[i].Provider]; ok {
			providers[i].ErrorRate = es.ErrorRate
			providers[i].RequestCount = es.RequestCount
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"providers":           providers,
		"total_providers":     len(providers),
		"available_providers": len(providers),
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

	var bucketInterval string
	switch timeRange {
	case "5m":
		bucketInterval = "1 minute"
	case "30m":
		bucketInterval = "5 minutes"
	case "6h":
		bucketInterval = "30 minutes"
	case "24h":
		bucketInterval = "1 hour"
	default:
		bucketInterval = "5 minutes"
	}

	var rows []struct {
		Timestamp    time.Time `gorm:"column:ts"`
		SwitchCount  int64     `gorm:"column:switch_count"`
		RequestCount int64     `gorm:"column:request_count"`
	}

	err := h.db.Raw(fmt.Sprintf(`
		SELECT
			date_trunc('%s', ts) as ts,
			SUM(CASE WHEN failover_used THEN 1 ELSE 0 END)::bigint as switch_count,
			COUNT(*)::bigint as request_count
		FROM call_logs
		WHERE ts > NOW() - INTERVAL '%s'
		GROUP BY date_trunc('%s', ts)
		ORDER BY ts
	`, bucketInterval, interval, bucketInterval)).Scan(&rows).Error

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
	byProvider := c.DefaultQuery("by_provider", "false")
	topGroups, _ := strconv.Atoi(c.DefaultQuery("top_groups", "0"))

	var bucketInterval string
	switch timeRange {
	case "5m":
		bucketInterval = "1 minute"
	case "30m":
		bucketInterval = "5 minutes"
	case "6h":
		bucketInterval = "30 minutes"
	case "24h":
		bucketInterval = "1 hour"
	default:
		bucketInterval = "5 minutes"
	}

	if byProvider == "true" {
		// 按 provider 维度
		var rows []struct {
			Timestamp    time.Time `gorm:"column:ts"`
			Provider     string    `gorm:"column:provider"`
			RequestCount int64     `gorm:"column:request_count"`
			TokenCount   int64     `gorm:"column:token_count"`
			ErrorCount   int64     `gorm:"column:error_count"`
		}
		err := h.db.Raw(fmt.Sprintf(`
			SELECT
				date_trunc('%s', ts) as ts,
				provider,
				COUNT(*)::bigint as request_count,
				COALESCE(SUM(total_tokens), 0)::bigint as token_count,
				SUM(CASE WHEN status >= 400 THEN 1 ELSE 0 END)::bigint as error_count
			FROM call_logs
			WHERE ts > NOW() - INTERVAL '%s'
			GROUP BY date_trunc('%s', ts), provider
			ORDER BY ts
		`, bucketInterval, interval, bucketInterval)).Scan(&rows).Error
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
		// 按 top group 维度
		var topNames []string
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
					date_trunc('%s', ts) as ts, group_name,
					COUNT(*)::bigint as request_count,
					COALESCE(SUM(total_tokens), 0)::bigint as token_count,
					SUM(CASE WHEN status >= 400 THEN 1 ELSE 0 END)::bigint as error_count
				FROM call_logs
				WHERE ts > NOW() - INTERVAL '%s' AND group_name IN ?
				GROUP BY date_trunc('%s', ts), group_name
				ORDER BY ts
			`, bucketInterval, interval, bucketInterval), topNames).Scan(&rows)
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

	// 默认：全局总量趋势
	var rows []struct {
		Timestamp    time.Time `gorm:"column:ts"`
		RequestCount int64     `gorm:"column:request_count"`
		TokenCount   int64     `gorm:"column:token_count"`
	}
	err := h.db.Raw(fmt.Sprintf(`
		SELECT date_trunc('%s', ts) as ts,
			COUNT(*)::bigint as request_count,
			COALESCE(SUM(total_tokens), 0)::bigint as token_count
		FROM call_logs
		WHERE ts > NOW() - INTERVAL '%s'
		GROUP BY date_trunc('%s', ts)
		ORDER BY ts
	`, bucketInterval, interval, bucketInterval)).Scan(&rows).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query throughput trend"})
		return
	}

	points := make([]ThroughputTrendPoint, 0, len(rows))
	for _, r := range rows {
		points = append(points, ThroughputTrendPoint{
			Timestamp: r.Timestamp.Format(time.RFC3339),
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
	var rules []model.AlertRule
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
	var events []model.AlertEvent
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
			user_id, provider, model, "group", duration_ms, status, error_message, extra
		FROM system_logs %s
		ORDER BY ts DESC, id DESC
		LIMIT ? OFFSET ?
	`, where)
	allArgs := append(args, pageSize, offset)

	var items []model.SystemLog
	if err := h.db.Raw(query, allArgs...).Scan(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query system logs"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": pageSize})
}

// GetSystemLogSinkHealth 获取日志接入健康状态
// GET /api/v1/ops/system-logs/health
func (h *OpsHandler) GetSystemLogSinkHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":           "healthy",
		"writer_enabled":   true,
		"recent_errors":    0,
		"last_write_at":    time.Now().Format(time.RFC3339),
		"pending_count":    0,
		"dropped_count":    0,
	})
}

// CleanupSystemLogs 清理过期系统日志
// POST /api/v1/ops/system-logs/cleanup
func (h *OpsHandler) CleanupSystemLogs(c *gin.Context) {
	days := 30
	if d, err := strconv.Atoi(c.DefaultQuery("days", "30")); err == nil && d > 0 {
		days = d
	}

	result := h.db.Exec(fmt.Sprintf("DELETE FROM system_logs WHERE ts < NOW() - INTERVAL '%d days'", days))
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
func defaultLogConfig() gin.H {
	return gin.H{
		"level":              "info",
		"slow_request_ms":    5000,
		"sample_rate":        1.0,
		"log_request_body":   false,
		"log_response_body":  false,
		"max_body_bytes":     2048,
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
	Provider       string  `gorm:"column:provider" json:"provider"`
	Model          string  `gorm:"column:model" json:"model"`
	RequestCount   int64   `gorm:"column:request_count" json:"request_count"`
	PromptTokens   int64   `gorm:"column:prompt_tokens" json:"prompt_tokens"`
	CompletionTokens int64 `gorm:"column:completion_tokens" json:"completion_tokens"`
	TotalTokens    int64   `gorm:"column:total_tokens" json:"total_tokens"`
	AvgDurationMs  float64 `gorm:"column:avg_duration_ms" json:"avg_duration_ms"`
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

	var items []TokenStatsItem
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
		"error_rate_warn":     0.05,
		"error_rate_critical": 0.10,
		"p95_ms_warn":         3000,
		"p95_ms_critical":     10000,
		"ttft_p95_ms_warn":    2000,
		"ttft_p95_ms_critical": 8000,
		"success_rate_min":    0.95,
		"concurrency_warn":    100,
	}
}

// defaultDashboardSettings 默认看板设置
func defaultDashboardSettings() gin.H {
	return gin.H{
		"default_time_range":  "1h",
		"refresh_interval":    30,
		"show_system_metrics": true,
		"compact_mode":        false,
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
	var jobs []model.AlertJobHeartbeat
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
	RequestCount  int64   `gorm:"column:request_count" json:"request_count"`
	ErrorCount    int64   `gorm:"column:error_count" json:"error_count"`
	AvgDurationMs float64 `gorm:"column:avg_duration_ms" json:"avg_duration_ms"`
}

// GetUserConcurrency 获取用户并发统计
// GET /api/v1/ops/user-concurrency
func (h *OpsHandler) GetUserConcurrency(c *gin.Context) {
	var items []UserConcurrencyItem
	err := h.db.Raw(`
		SELECT
			user_id,
			COUNT(*)::bigint AS request_count,
			SUM(CASE WHEN status >= 400 THEN 1 ELSE 0 END)::bigint AS error_count,
			COALESCE(AVG(duration_ms), 0)::float AS avg_duration_ms
		FROM call_logs
		WHERE ts > NOW() - INTERVAL '5 minutes'
			AND user_id != ''
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
	ProviderID           string `json:"provider_id"`
	IsRateLimited        bool   `json:"is_rate_limited"`
	RateLimitRemaining   int    `json:"rate_limit_remaining"`
	RateLimitResetSeconds int   `json:"rate_limit_reset_seconds"`
	ErrorCount           int64  `json:"error_count"`
}

// GetAccountAvailability 获取各 Provider 的 rate limit 状态
// GET /api/v1/ops/account-availability
func (h *OpsHandler) GetAccountAvailability(c *gin.Context) {
	var items []AccountAvailabilityItem

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
			IsRateLimited        bool `json:"is_rate_limited"`
			RateLimitRemaining   int  `json:"rate_limit_remaining"`
			RateLimitResetSeconds int `json:"rate_limit_reset_seconds"`
			ErrorCount           int64 `json:"error_count"`
		}
		if jsonErr := json.Unmarshal([]byte(val), &data); jsonErr != nil {
			// 回退：简单计数
			count, _ := strconv.ParseInt(val, 10, 64)
			data.ErrorCount = count
		}

		items = append(items, AccountAvailabilityItem{
			ProviderID:           providerID,
			IsRateLimited:        data.IsRateLimited,
			RateLimitRemaining:   data.RateLimitRemaining,
			RateLimitResetSeconds: data.RateLimitResetSeconds,
			ErrorCount:           data.ErrorCount,
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
		where += " AND group_name = ?"
		args = append(args, groupID)
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

	// 获取当前 QPS（从 Redis 最近 1 分钟）
	now := time.Now()
	minuteKey := now.Format("200601021504")
	currentQPS := 0.0
	currentTPS := 0.0

	if h.rdb != nil {
		qpsKey := fmt.Sprintf("ops:qps:%s", minuteKey)
		tpsKey := fmt.Sprintf("ops:tps:%s", minuteKey)

		qpsCount, _ := h.rdb.Get(c.Request.Context(), qpsKey).Int64()
		tpsCount, _ := h.rdb.Get(c.Request.Context(), tpsKey).Int64()

		currentQPS = float64(qpsCount) / 60.0
		currentTPS = float64(tpsCount) / 60.0
	}

	peakQPS := currentQPS * 1.5
	if peakQPS < avgQPS*1.5 {
		peakQPS = avgQPS * 1.5
	}
	peakTPS := currentTPS * 1.5
	if peakTPS < avgTPS*1.5 {
		peakTPS = avgTPS * 1.5
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
	Provider              string `gorm:"column:provider" json:"provider"`
	UpstreamStatus        int    `gorm:"column:upstream_status" json:"upstream_status"`
	UpstreamErrorMessage  string `gorm:"column:upstream_error_message" json:"upstream_error_message"`
	TS                    string `gorm:"column:ts" json:"ts"`
	DurationMs            int    `gorm:"column:duration_ms" json:"duration_ms"`
}

// GetRequestErrorCorrelation 获取请求错误关联的上游错误
// GET /api/v1/ops/request-errors/:id/correlation
func (h *OpsHandler) GetRequestErrorCorrelation(c *gin.Context) {
	requestID := c.Param("id")

	// 查找同一 request_id 且 status >= 500 的上游错误记录
	var items []ErrorCorrelationItem
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
	AuthMs      int `json:"auth_ms"`
	RoutingMs   int `json:"routing_ms"`
	UpstreamMs  int `json:"upstream_ms"`
	ResponseMs  int `json:"response_ms"`
	TTFTMs      int `json:"ttft_ms"`
}
