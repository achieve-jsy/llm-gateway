package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// ===== 方案 X：异常反馈闭环（真人工反馈闭环）control-plane 后端接口 =====
//
// 报告工具 isolation report（usage-report / anomaly_feedback.go）零库权限，只经
//   POST /api/v1/usage/report/anomalies              批量 upsert 命中异常
//   GET  /api/v1/usage/report/anomaly-status         读回真实状态
//   POST /api/v1/usage/report/anomalies/:id/feedback 人工反馈状态
//
// 表 anomaly_records 由 db-eng 走 atlas-db-migration 建立（GORM model 在 controlplane/model，
// 见 internal/controlplane/model/anomaly_record.go）。本文件【不 import controlplane/model】——
// observability/handler 被 controlplane 反向依赖（router 用 obshandler.NewUsageHandler），
// 若此处 import controlplane/model 会形成 import cycle。故采用与 ReportBudgetExec 一致的风格：
// 按表名 "anomaly_records" + 本地 struct 读写，字段与 GORM model/快照 DDL 对齐。
//
// 唯一 upsert 键：(scope_level, scope_id, rule, window_start, member_id)。
// window_start 列 NOT NULL：非按窗规则（A3–A5）上报 window_start=""，服务端以 Go 零时间
// (0001-01-01T00:00:00Z) 落库并用零时间等值参与唯一键匹配，保证同一非按窗规则只登记一条。
// 窗口时间序列化统一 +08:00 RFC3339 / 分钟精度（与 report 侧 anomalyMinuteTS 对齐），避免漂移。

// anomalyStatusEnum 5 态合法值（与 t14/report 常量及 GORM model 注释对齐；应用层校验，DB 仅存字符串）。
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

// anomalyRecordRow 与 anomaly_records 表列对齐的本地 struct（不 import controlplane/model）。
// 仅字段名+gorm column tag 用于 Scan/取列；主键 id 为 uuid，落库/读取都以字符串承载。
type anomalyRecordRow struct {
	ID          string     `gorm:"column:id"`
	ScopeLevel  string     `gorm:"column:scope_level"`
	ScopeID     string     `gorm:"column:scope_id"`
	Rule        string     `gorm:"column:rule"`
	RuleVersion string     `gorm:"column:rule_version"`
	WindowStart time.Time  `gorm:"column:window_start"`
	MemberID    string     `gorm:"column:member_id"`
	Status      string     `gorm:"column:status"`
	FeedbackAt  *time.Time `gorm:"column:feedback_at"`
	FeedbackBy  string     `gorm:"column:feedback_by"`
	Reason      string     `gorm:"column:reason"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
}

// anomalyWindowStartParse 解析 +08:00 RFC3339 命中窗口起点（分钟精度）。成功返回 (t, true)。
// 空串返回零时间且 ok=false（调用方按「非按窗规则」处理，落库用零时间哨兵）。
func anomalyWindowStartParse(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// anomalyWindowStartStr 输出 +08:00 RFC3339 / 分钟精度（与 report 侧 anomalyMinuteTS 口径一致）。
func anomalyWindowStartStr(t time.Time) string {
	// 零时间哨兵（非按窗规则）：回显空串，保持报告侧「无窗」语义。
	if t.IsZero() {
		return ""
	}
	loc := time.FixedZone("Asia/Shanghai", 8*3600)
	return t.In(loc).Truncate(time.Minute).Format(time.RFC3339)
}

// anomalyUpsertKeyWhere 构造唯一键（scope_level, scope_id, rule, window_start, member_id）查询条件。
// window_start 用零时间哨兵参与等值匹配（列 NOT NULL）。
func anomalyUpsertKeyWhere(scopeLevel, scopeID, rule, memberID string, ws time.Time) (string, []interface{}) {
	if ws.IsZero() {
		ws = time.Time{}
	}
	return "scope_level = ? AND scope_id = ? AND rule = ? AND member_id = ? AND window_start = ?",
		[]interface{}{scopeLevel, scopeID, rule, memberID, ws}
}

// ReportAnomalyUpsert POST /usage/report/anomalies —— 批量 upsert 异常命中。
// 请求体：flat JSON array（元素=anomalyUpsertItem）。按唯一键 upsert：
//   - 已存在：不覆盖用户反馈态（status/feedback_* 不动），仅刷 updated_at；
//   - 不存在：以默认态「待确认」新建。
//
// 返回 []anomalyUpsertResult（每条 {id,status}）。Db 未配置或无表（老版本）时返回空数组，不 panic。
func (h *UsageHandler) ReportAnomalyUpsert(c *gin.Context) {
	if h.db == nil {
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
	db := h.db.WithContext(c.Request.Context())
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
		cond, args := anomalyUpsertKeyWhere(it.ScopeLevel, it.ScopeID, it.Rule, it.MemberID, ws)
		var existing anomalyRecordRow
		if err := db.Table("anomaly_records").
			Select("id, status").
			Where(cond, args...).
			Limit(1).
			Scan(&existing).Error; err != nil {
			// 无表（老版本）或查询失败：单条降级默认态，不中断整批。
			results = append(results, anomalyUpsertResult{Status: "待确认"})
			continue
		}
		if existing.ID != "" {
			// 已存在：不覆盖用户反馈态，仅刷 updated_at。
			db.Table("anomaly_records").Where(cond, args...).Update("updated_at", time.Now().UTC())
			results = append(results, anomalyUpsertResult{ID: existing.ID, Status: firstNonEmpty(existing.Status, "待确认")})
			continue
		}
		// 不存在：以默认态「待确认」新建。
		row := anomalyRecordRow{
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
		if err := db.Table("anomaly_records").Create(&row).Error; err == nil {
			results = append(results, anomalyUpsertResult{ID: row.ID, Status: "待确认"})
		} else {
			// 并发 upsert 冲突（唯一键已存在）：回读现有 ID + 状态。
			var got anomalyRecordRow
			db.Table("anomaly_records").Select("id, status").Where(cond, args...).Limit(1).Scan(&got)
			results = append(results, anomalyUpsertResult{ID: got.ID, Status: firstNonEmpty(got.Status, "待确认")})
		}
	}
	c.JSON(http.StatusOK, results)
}

// ReportAnomalyStatus GET /usage/report/anomaly-status —— 读回指定范围 + 时间窗的真实状态。
// query：scope_level, scope_id, from, to（from/to 为 YYYY-MM-DD 可选，Asia/Shanghai 日窗）。
// 返回 []anomalyStatusRow（{rule, window_start(+08:00分钟精度), member_id, status}）。
// 无表/无记录返回空数组，不 panic（报告侧降级「待确认」）。
func (h *UsageHandler) ReportAnomalyStatus(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusOK, []anomalyStatusRow{})
		return
	}
	scopeLevel := c.Query("scope_level")
	scopeID := c.Query("scope_id")
	if scopeLevel == "" || scopeID == "" {
		c.JSON(http.StatusOK, []anomalyStatusRow{})
		return
	}
	from := c.Query("from")
	to := c.Query("to")

	db := h.db.WithContext(c.Request.Context())
	q := db.Table("anomaly_records").
		Select("rule, window_start, member_id, status").
		Where("scope_level = ? AND scope_id = ?", scopeLevel, scopeID)
	loc := time.FixedZone("Asia/Shanghai", 8*3600)
	if from != "" {
		if fs, err := time.ParseInLocation("2006-01-02", from, loc); err == nil {
			q = q.Where("window_start >= ?", fs)
		}
	}
	if to != "" {
		if te, err := time.ParseInLocation("2006-01-02", to, loc); err == nil {
			q = q.Where("window_start < ?", te.AddDate(0, 0, 1))
		}
	}
	var rows []anomalyRecordRow
	if err := q.Order("rule ASC, window_start ASC").Scan(&rows).Error; err != nil {
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

// ReportAnomalyFeedback POST /usage/report/anomalies/:id/feedback —— 人工反馈状态。
// body：{status, reason}。校验 5 态枚举（非法400/不存在404），写 status/feedback_at/feedback_by/reason。
// feedback_by 取当前登录用户（JWTAuth 注入 user_name；为空则 user_id，再空则空串）。
func (h *UsageHandler) ReportAnomalyFeedback(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "db not configured"})
		return
	}
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 缺失"})
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
	db := h.db.WithContext(c.Request.Context())
	updates := map[string]interface{}{
		"status":      req.Status,
		"feedback_at": time.Now().UTC(),
		"feedback_by": firstNonEmpty(c.GetString("user_name"), c.GetString("user_id")),
		"reason":      req.Reason,
		"updated_at":  time.Now().UTC(),
	}
	res := db.Table("anomaly_records").Where("id = ?", id).Updates(updates)
	if res.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": res.Error.Error()})
		return
	}
	if res.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "异常记录不存在: " + id})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "id": id, "status": req.Status})
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
