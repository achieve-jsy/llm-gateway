package handler

import (
	"fmt"
	"net/http"
	"time"

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
}

// NewOpsHandler 创建 OpsHandler 实例
func NewOpsHandler(db *gorm.DB, rdb *redis.Client, mc *service.SystemMetricsCollector) *OpsHandler {
	return &OpsHandler{
		db:               db,
		rdb:              rdb,
		metricsCollector: mc,
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
	DurationP50Ms int `json:"duration_p50_ms"`
	DurationP90Ms int `json:"duration_p90_ms"`
	DurationP95Ms int `json:"duration_p95_ms"`
	DurationP99Ms int `json:"duration_p99_ms"`
	DurationAvgMs int `json:"duration_avg_ms"`
	TTFTP50Ms     int `json:"ttft_p50_ms"`
	TTFTP90Ms     int `json:"ttft_p90_ms"`
	TTFTP95Ms     int `json:"ttft_p95_ms"`
	TTFTP99Ms     int `json:"ttft_p99_ms"`
	TTFTAvgMs     int `json:"ttft_avg_ms"`
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
		TTFTP50Ms     int `gorm:"column:ttft_p50"`
		TTFTP90Ms     int `gorm:"column:ttft_p90"`
		TTFTP95Ms     int `gorm:"column:ttft_p95"`
		TTFTP99Ms     int `gorm:"column:ttft_p99"`
	}

	err = h.db.Raw(fmt.Sprintf(`
		SELECT
			COALESCE(PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY duration_ms), 0)::int as duration_p50,
			COALESCE(PERCENTILE_CONT(0.9) WITHIN GROUP (ORDER BY duration_ms), 0)::int as duration_p90,
			COALESCE(PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY duration_ms), 0)::int as duration_p95,
			COALESCE(PERCENTILE_CONT(0.99) WITHIN GROUP (ORDER BY duration_ms), 0)::int as duration_p99,
			COALESCE(PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY COALESCE(first_token_ms, 0)), 0)::int as ttft_p50,
			COALESCE(PERCENTILE_CONT(0.9) WITHIN GROUP (ORDER BY COALESCE(first_token_ms, 0)), 0)::int as ttft_p90,
			COALESCE(PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY COALESCE(first_token_ms, 0)), 0)::int as ttft_p95,
			COALESCE(PERCENTILE_CONT(0.99) WITHIN GROUP (ORDER BY COALESCE(first_token_ms, 0)), 0)::int as ttft_p99
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
				TTFTP50Ms:     latency.TTFTP50Ms,
				TTFTP90Ms:     latency.TTFTP90Ms,
				TTFTP95Ms:     latency.TTFTP95Ms,
				TTFTP99Ms:     latency.TTFTP99Ms,
				TTFTAvgMs:     int(traffic.TTFTAvgMs),
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
