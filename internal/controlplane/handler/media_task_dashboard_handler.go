package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
)

// 本文件是「媒体任务维度运维看板」的 HTTP 入口（E 队 Batch 3 · E1 任务 4）。
//
// 分层：**本文件只做参数校验 + 窗口解析 + 响应组装**，口径与聚合在
// service.MediaTaskDashboardService，SQL 在 repository.MediaTaskStatsRepo。

// MediaTaskDashboardHandler 媒体任务看板 Handler。
type MediaTaskDashboardHandler struct {
	svc *service.MediaTaskDashboardService
}

// NewMediaTaskDashboardHandler 创建 MediaTaskDashboardHandler 实例。
func NewMediaTaskDashboardHandler(svc *service.MediaTaskDashboardService) *MediaTaskDashboardHandler {
	return &MediaTaskDashboardHandler{svc: svc}
}

// mediaTaskDashboardRangePresets 允许的预置时间跨度（**白名单**，非法值一律 400）。
//
// 上限 90d 是硬护栏（见 service.MediaTaskDashboardMaxRange）：media_tasks 目前只有百余行，
// 但这里的写法必须能扛住增长，也绝不能允许「不传时间参数 = 全表扫描」。
var mediaTaskDashboardRangePresets = map[string]time.Duration{
	"1h":  time.Hour,
	"6h":  6 * time.Hour,
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
	"90d": 90 * 24 * time.Hour,
}

// mediaTaskDashboardTimeLayouts 允许的时间参数格式（RFC3339 优先，其次纯日期/空格分隔）。
var mediaTaskDashboardTimeLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// GetDashboard 媒体任务运维看板统计。
//
// GET /api/v1/ops/media-tasks/dashboard
//
// 查询参数：
//   - range  字符串，可选，取值 {@link mediaTaskDashboardRangePresets}（1h|6h|24h|7d|30d|90d），
//     默认 24h。与 start/end 同时出现时 **start/end 优先**，range 仅作展示（回显 "custom"）。
//   - start  字符串，可选，RFC3339 / "2006-01-02 15:04:05" / "2006-01-02"，窗口左端（含）。
//   - end    字符串，可选，同上格式，窗口右端（**不含**）；默认现在，未来时间会被钳到当前时刻。
//   - kind   字符串，可选，取值 image|video；不传 = 全部（含 kind 为空串的 legacy 行）。
//
// 强制上限：窗口跨度最大 90d，超过则左端截断并在 `data.window.clamped` 标记；
// 未传任何时间参数时使用默认 24h（`data.window.range_defaulted = true`）。
//
// 响应：200 `{"data": {window, totals, status_distribution, success_rate, duration_seconds,
// cost_micro_usd, anomalies, caliber}}`（字段类型见 service.MediaTaskDashboard*）；
// 参数非法返回 400 `{"error": "..."}`；查询失败返回 500。
func (h *MediaTaskDashboardHandler) GetDashboard(c *gin.Context) {
	kind := strings.TrimSpace(c.Query("kind"))
	if kind != "" && !model.IsValidMediaTaskKind(kind) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid kind: must be image or video"})
		return
	}

	window, ok := parseMediaTaskDashboardWindow(c)
	if !ok {
		return // parseMediaTaskDashboardWindow 已写 400
	}

	result, err := h.svc.GetDashboard(service.MediaTaskDashboardQuery{
		Start:          window.start,
		End:            window.end,
		Kind:           kind,
		Scope:          videoTaskScopeFromContext(c),
		Range:          window.rangeName,
		RangeDefaulted: window.rangeDefaulted,
		Clamped:        window.clamped,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

// mediaTaskDashboardWindow 解析后的生效窗口。
type mediaTaskDashboardWindow struct {
	start, end     time.Time
	rangeName      string
	rangeDefaulted bool
	clamped        bool
}

// parseMediaTaskDashboardWindow 解析并钳制查询窗口；失败时已写 400 并返回 ok=false。
func parseMediaTaskDashboardWindow(c *gin.Context) (mediaTaskDashboardWindow, bool) {
	now := time.Now().UTC()

	startStr := strings.TrimSpace(c.Query("start"))
	endStr := strings.TrimSpace(c.Query("end"))
	rangeStr := strings.TrimSpace(c.Query("range"))

	end := now
	if endStr != "" {
		parsed, err := parseMediaTaskDashboardTime(endStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid end: expect RFC3339 or YYYY-MM-DD"})
			return mediaTaskDashboardWindow{}, false
		}
		end = parsed
	}

	out := mediaTaskDashboardWindow{end: end}

	switch {
	case startStr != "":
		parsed, err := parseMediaTaskDashboardTime(startStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid start: expect RFC3339 or YYYY-MM-DD"})
			return mediaTaskDashboardWindow{}, false
		}
		out.start = parsed
		out.rangeName = "custom"
	case rangeStr != "":
		d, ok := mediaTaskDashboardRangePresets[rangeStr]
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "invalid range: must be one of 1h, 6h, 24h, 7d, 30d, 90d",
			})
			return mediaTaskDashboardWindow{}, false
		}
		out.rangeName = rangeStr
		out.start = end.Add(-d)
	default:
		out.rangeName = service.MediaTaskDashboardDefaultRange
		out.rangeDefaulted = true
		out.start = end.Add(-mediaTaskDashboardRangePresets[service.MediaTaskDashboardDefaultRange])
	}

	// 未来时间：钳到当前时刻（否则窗口会滑进未来，徒增无意义扫描与误导性回显）。
	if out.end.After(now) {
		out.end = now
		out.clamped = true
	}
	if !out.start.Before(out.end) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid window: start must be before end"})
		return mediaTaskDashboardWindow{}, false
	}

	// 强制上限：任何输入组合都不会产生超过 90d 的窗口（防全表扫描的硬护栏）。
	if maxSpan := mediaTaskDashboardRangePresets[service.MediaTaskDashboardMaxRange]; out.end.Sub(out.start) > maxSpan {
		out.start = out.end.Add(-maxSpan)
		out.clamped = true
	}
	return out, true
}

// parseMediaTaskDashboardTime 按白名单布局解析时间参数（全部视为 UTC）。
func parseMediaTaskDashboardTime(value string) (time.Time, error) {
	var lastErr error
	for _, layout := range mediaTaskDashboardTimeLayouts {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed.UTC(), nil
		}
		lastErr = err
	}
	return time.Time{}, lastErr
}
