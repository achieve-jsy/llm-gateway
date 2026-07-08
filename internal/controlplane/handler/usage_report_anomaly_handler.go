package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/example/llm-gateway/internal/controlplane/model"
)

// ===== 方案 X：异常反馈闭环（真人工反馈闭环）control-plane 后端接口 =====
//
// 表 anomaly_records 由 db-eng 走 atlas-db-migration 建立（GORM model 见
// internal/controlplane/model/anomaly_record.go）。报告工具（usage-report）零库权限，只经
//   POST /api/v1/usage/report/anomalies              批量 upsert 命中异常
//   GET  /api/v1/usage/report/anomaly-status         读回真实状态
//   POST /api/v1/usage/report/anomalies/:id/feedback 人工反馈状态
// 与 client/anomaly.go 的 PostReportAnomalies / FetchAnomalyStatus 契约严格对齐。
//
// 数据访问收拢到 AnomalyRecordRepo（repository 层），handler 只做编排/校验/序列化。
// 唯一 upsert 键：(scope_level, scope_id, rule, window_start, member_id)。
// window_start 列 NOT NULL：非按窗规则（A3–A5）上报 window_start=""，以 Go 零时间
// (0001-01-01T00:00:00Z) 落库并参与唯一键等值匹配，保证同一非按窗规则只登记一条。
// 窗口时间序列化统一 +08:00 RFC3339 / 分钟精度（与报告侧 anomalyMinuteTS 对齐），避免漂移。

// anomalyStatusEnum 5 态合法值（与 report.AnomalyStatus* 常量及 GORM model 注释对齐；应用层校验，DB 仅存字符串）。
var anomalyStatusEnum = map[string]bool{
	"待确认":   true,
	"已确认正常": true,
	"修复中":   true,
	"已恢复":   true,
	"误报":    true,
}

// anomalyUpsertItem 上报载荷（与 client.AnomalyUpsert 字段一致，flat JSON array 元素）。
type anomalyUpsertItem struct {
	ScopeLevel  string `json:"scope_level"`
	ScopeID     string `json:"scope_id"`
	Rule        string `json:"rule"`
	RuleVersion string `json:"rule_version"`
	WindowStart string `json:"window_start"` // +08:00 RFC3339，分钟精度；非按窗规则为空串
	MemberID    string `json:"member_id"`
}

// anomalyUpsertResult 上报返回（每条 {id, status}）。
type anomalyUpsertResult struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// anomalyStatusRow 读回的真实状态行（供报告覆盖 AnomalyHit.Status）。
type anomalyStatusRow struct {
	Rule        string `json:"rule"`
	WindowStart string `json:"window_start"` // +08:00 RFC3339，分钟精度
	MemberID    string `json:"member_id"`
	Status      string `json:"status"`
}

// anomalyFeedbackReq 反馈请求体 {status, reason}。
type anomalyFeedbackReq struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

// AnomalyRecordRepoInterface 方案 X 异常反馈闭环 repo 依赖（consumer-side，随 handler 定义，
// 便于测试替身；具体实现见 internal/controlplane/repository/anomaly_record_repo.go）。
type AnomalyRecordRepoInterface interface {
	// FindByUpsertKey 按唯一键查找既有记录；未命中返回 gorm.ErrRecordNotFound。
	FindByUpsertKey(scopeLevel, scopeID, rule, memberID string, ws time.Time) (*model.AnomalyRecord, error)
	// Create 新建一条异常记录（id 由 gen_random_uuid 回填）。
	Create(rec *model.AnomalyRecord) error
	// TouchUpdatedAt 命中已有记录时仅刷 updated_at（不覆盖用户反馈态）。
	TouchUpdatedAt(scopeLevel, scopeID, rule, memberID string, ws time.Time) error
	// FindByScopeWindow 读回指定范围+时间窗的真实状态行。
	FindByScopeWindow(scopeLevel, scopeID string, from, to *time.Time) ([]model.AnomalyRecord, error)
	// UpdateFeedback 写 status/feedback_at/feedback_by/reason；返回 RowsAffected。
	UpdateFeedback(id uuid.UUID, status, feedbackBy, reason string) (int64, error)
}

// AnomalyFeedbackHandler 方案 X 异常反馈闭环 control-plane 接口的依赖容器。
type AnomalyFeedbackHandler struct {
	repo AnomalyRecordRepoInterface
}

// NewAnomalyFeedbackHandler 构造方案 X 异常反馈闭环 handler（注入 repo）。
func NewAnomalyFeedbackHandler(repo AnomalyRecordRepoInterface) *AnomalyFeedbackHandler {
	return &AnomalyFeedbackHandler{repo: repo}
}

// anomalyWindowStartParse 解析 +08:00 RFC3339 命中窗口起点并规范化为**分钟精度**（与读回 anomalyWindowStartStr
// 的分钟精度口径完全对称，避免 DB timestamptz 秒/微秒级漂移导致 upsert→读回→再匹配三元组失配）。
// 成功返回 (t, true)；空串返回零时间且 ok=false（调用方按「非按窗规则」处理，落库用零时间哨兵）。
func anomalyWindowStartParse(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			// 统一到分钟精度（+08:00 为整时偏移，Truncate(Minute) 跨时区安全）。
			return t.Truncate(time.Minute), true
		}
	}
	return time.Time{}, false
}

// anomalyWindowStartStr 输出 +08:00 RFC3339 / 分钟精度（与报告侧 anomalyMinuteTS 口径一致）。
func anomalyWindowStartStr(t time.Time) string {
	// 零时间哨兵（非按窗规则）：回显空串，保持报告侧「无窗」语义。
	if t.IsZero() {
		return ""
	}
	loc := time.FixedZone("Asia/Shanghai", 8*3600)
	return t.In(loc).Truncate(time.Minute).Format(time.RFC3339)
}

// anomalyParseDay 把 YYYY-MM-DD 解析为 Asia/Shanghai 日起点。非法返回 (zero,false)。
func anomalyParseDay(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	loc := time.FixedZone("Asia/Shanghai", 8*3600)
	if t, err := time.ParseInLocation("2006-01-02", s, loc); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// ReportAnomalyUpsert POST /api/v1/usage/report/anomalies —— 批量 upsert 异常命中。
// 请求体：flat JSON array（元素=anomalyUpsertItem）。按唯一键 upsert：
//   - 已存在：不覆盖用户反馈态（status/feedback_* 不动），仅刷 updated_at；
//   - 不存在：以默认态「待确认」新建。
//
// 返回 []anomalyUpsertResult（每条 {id,status}）。表未建（老版本）时返回空数组，不 panic。
func (h *AnomalyFeedbackHandler) ReportAnomalyUpsert(c *gin.Context) {
	if h.repo == nil {
		c.JSON(http.StatusOK, []anomalyUpsertResult{})
		return
	}
	var items []anomalyUpsertItem
	if err := c.ShouldBindJSON(&items); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体必须是 anomaly 数组: " + err.Error()})
		return
	}
	if len(items) == 0 {
		c.JSON(http.StatusOK, []anomalyUpsertResult{})
		return
	}
	results := make([]anomalyUpsertResult, 0, len(items))
	for _, it := range items {
		if it.ScopeLevel == "" || it.ScopeID == "" || it.Rule == "" {
			results = append(results, anomalyUpsertResult{Status: "invalid"})
			continue
		}
		ws, ok := anomalyWindowStartParse(it.WindowStart)
		if it.WindowStart != "" && !ok {
			results = append(results, anomalyUpsertResult{Status: "invalid"})
			continue
		}
		// 已存在：不覆盖用户反馈态，仅刷 updated_at。
		if existing, err := h.repo.FindByUpsertKey(it.ScopeLevel, it.ScopeID, it.Rule, it.MemberID, ws); err == nil && existing != nil {
			h.repo.TouchUpdatedAt(it.ScopeLevel, it.ScopeID, it.Rule, it.MemberID, ws)
			results = append(results, anomalyUpsertResult{ID: existing.ID.String(), Status: firstNonEmpty(existing.Status, "待确认")})
			continue
		}
		// 不存在：以默认态「待确认」新建。
		rec := &model.AnomalyRecord{
			ScopeLevel:  it.ScopeLevel,
			ScopeID:     it.ScopeID,
			Rule:        it.Rule,
			RuleVersion: firstNonEmpty(it.RuleVersion, "v1"),
			WindowStart: ws,
			MemberID:    it.MemberID,
			Status:      "待确认",
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
		}
		if err := h.repo.Create(rec); err == nil {
			results = append(results, anomalyUpsertResult{ID: rec.ID.String(), Status: "待确认"})
		} else {
			// 并发 upsert 冲突（唯一键已存在）：回读现有 ID + 状态。
			if existing, e := h.repo.FindByUpsertKey(it.ScopeLevel, it.ScopeID, it.Rule, it.MemberID, ws); e == nil && existing != nil {
				results = append(results, anomalyUpsertResult{ID: existing.ID.String(), Status: firstNonEmpty(existing.Status, "待确认")})
			} else {
				results = append(results, anomalyUpsertResult{Status: "待确认"})
			}
		}
	}
	c.JSON(http.StatusOK, results)
}

// ReportAnomalyStatus GET /api/v1/usage/report/anomaly-status —— 读回指定范围 + 时间窗的真实状态。
// query：scope_level, scope_id, from, to（from/to 为 YYYY-MM-DD 可选，Asia/Shanghai 日窗）。
// 返回 []anomalyStatusRow（{rule, window_start(+08:00分钟精度), member_id, status}）。
// 无表/无记录返回空数组，不 panic（报告侧降级「待确认」）。
func (h *AnomalyFeedbackHandler) ReportAnomalyStatus(c *gin.Context) {
	if h.repo == nil {
		c.JSON(http.StatusOK, []anomalyStatusRow{})
		return
	}
	scopeLevel := c.Query("scope_level")
	scopeID := c.Query("scope_id")
	if scopeLevel == "" || scopeID == "" {
		c.JSON(http.StatusOK, []anomalyStatusRow{})
		return
	}
	var fromPtr, toPtr *time.Time
	if fs, ok := anomalyParseDay(c.Query("from")); ok {
		fromPtr = &fs
	}
	if te, ok := anomalyParseDay(c.Query("to")); ok {
		tEnd := te.AddDate(0, 0, 1) // [from 00:00, to+1 00:00)
		toPtr = &tEnd
	}
	rows, err := h.repo.FindByScopeWindow(scopeLevel, scopeID, fromPtr, toPtr)
	if err != nil {
		c.JSON(http.StatusOK, []anomalyStatusRow{})
		return
	}
	out := make([]anomalyStatusRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, anomalyStatusRow{
			Rule:        r.Rule,
			WindowStart: anomalyWindowStartStr(r.WindowStart),
			MemberID:    r.MemberID,
			Status:      r.Status,
		})
	}
	c.JSON(http.StatusOK, out)
}

// ReportAnomalyFeedback POST /api/v1/usage/report/anomalies/:id/feedback —— 人工反馈状态。
// body：{status, reason}。校验 5 态枚举（非法400/不存在404），写 status/feedback_at/feedback_by/reason。
// feedback_by 取当前登录用户（JWTAuth 注入 user_name；为空则 user_id，再空则空串）。
func (h *AnomalyFeedbackHandler) ReportAnomalyFeedback(c *gin.Context) {
	if h.repo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "anomaly repo not configured"})
		return
	}
	idStr := c.Param("id")
	if idStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 缺失"})
		return
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 非法: " + idStr})
		return
	}
	var req anomalyFeedbackReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误: " + err.Error()})
		return
	}
	if !anomalyStatusEnum[req.Status] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法状态，仅支持 待确认|已确认正常|修复中|已恢复|误报: " + req.Status})
		return
	}
	feedbackBy := firstNonEmpty(c.GetString("user_name"), c.GetString("user_id"))
	affected, err := h.repo.UpdateFeedback(id, req.Status, feedbackBy, req.Reason)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if affected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "异常记录不存在: " + idStr})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "id": idStr, "status": req.Status})
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
