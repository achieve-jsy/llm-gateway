package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	cpsvc "github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/observability/report/v2"
	"github.com/example/llm-gateway/internal/observability/service"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/example/llm-gateway/internal/orgservice/rbac/policy"
)

// ReportHandler 承载 token 使用分析报告的 API：
//   - GET  /api/v1/usage/analysis-report                → 个人/部门/工作室 Report JSON（report_snapshot 缓存命中直返，未命中装配后落库）
//   - GET  /api/v1/usage/analysis-report/members        → 四象限成员点集（轻量 payload，隔离大 scope）
//   - POST /api/v1/usage/analysis-report/anomaly/:user_id/feedback → 严重异常待核查反馈回写
//
// 全部复用 UsagePolicy.EffectiveScope/QueryScope 做 RBAC（usage:view:scoped，管理角色），不新建权限模型。
type ReportHandler struct {
	db       *gorm.DB
	snapshot *cpsvc.ReportSnapshotService
}

// NewReportHandler 创建报告 API handler。
func NewReportHandler(db *gorm.DB) *ReportHandler {
	return &ReportHandler{
		db:       db,
		snapshot: cpsvc.NewReportSnapshotService(db),
	}
}

// assembleReport 用 DB 版 ReportDataProvider（带 actor 行级 RBAC + 周期）驱动 Orchestrator 的
// GetReport(ctx, ReportParams) 产出 *Report。DataSource 由本 handler 注入（api-eng 的 t9 职责）。
func (h *ReportHandler) assembleReport(c *gin.Context, scopeLevel, scopeID, scopeName, from, periodType string) (*report.Report, error) {
	actor := rbac.GetActor(c)
	periodEnd, _ := time.Parse("2006-01-02", from)
	periodEnd = periodEndOf(periodEnd, periodType)

	provider := service.NewDBReportDataProvider(h.db).
		WithPeriod(from, periodEnd.Format("2006-01-02")).
		WithRBACScope(policy.UsagePolicy{}.QueryScope(actor))

	params := service.ReportParams{
		Level: scopeLevel,
		Scope: report.Scope{ID: scopeID, Name: scopeName},
		Period: report.Period{
			From: from, To: periodEnd.Format("2006-01-02"), IsoWeek: isoWeekStr(from),
		},
		Currency:    report.Currency{Report: "USD", USDCNYRate: 1.0},
		RuleVersion: "v0.6",
		DataSource:  provider,
		Semantic:    service.DefaultSemanticGateway(),
	}
	return service.GetReport(c.Request.Context(), params)
}

// resolveReportScope 用 EffectiveScope 把请求 id 收敛到 actor 管辖范围，返回最终 report level + scope id + 展示名。
func (h *ReportHandler) resolveReportScope(c *gin.Context, reqLevel, reqID string) (string, string, string, error) {
	actor := rbac.GetActor(c)
	userID := c.GetString("user_id")

	scopeKey := levelToScopeKey(reqLevel)
	effScope, effID, _ := policy.UsagePolicy{}.EffectiveScope(actor, scopeKey, reqID, userID)
	level := scopeKeyToLevel(effScope)
	if effID == "" && level == report.LevelPersonal {
		effID = userID
	}
	// 非 admin 查看个人报告：收敛目标必须是 actor 自己或其在管范围成员，否则拒绝（防止越权）。
	if !actor.IsGlobal && level == report.LevelPersonal && effID != userID {
		ok, err := h.actorCanViewUser(c, actor, effID)
		if err != nil {
			return "", "", "", err
		}
		if !ok {
			return "", "", "", errForbidden
		}
	}
	return level, effID, h.scopeName(c, level, effID), nil
}

// errForbidden 表示越权访问被拒绝。
var errForbidden = &forbiddenError{}

type forbiddenError struct{}

func (*forbiddenError) Error() string { return "forbidden: target user outside your managed scope" }

// actorCanViewUser 校验非全局管理角色能否查看目标用户（所在部门/工作室在其管辖内）。
func (h *ReportHandler) actorCanViewUser(c *gin.Context, actor rbac.Actor, userID string) (bool, error) {
	if h.db == nil {
		return false, nil
	}
	if actor.IsGlobal || userID == actor.UserID {
		return true, nil
	}
	var conds []string
	args := make([]any, 0, 4)
	if len(actor.DeptIDs) > 0 {
		conds = append(conds, "CAST(u.dept_id AS TEXT) IN ?")
		args = append(args, actor.DeptIDs)
		conds = append(conds, "d.dept_id IN ?")
		args = append(args, actor.DeptIDs)
	}
	if len(actor.StudioIDs) > 0 {
		conds = append(conds, "CAST(d.studio_id AS TEXT) IN ?")
		args = append(args, actor.StudioIDs)
		conds = append(conds, "CAST(dsm.studio_id AS TEXT) IN ?")
		args = append(args, actor.StudioIDs)
	}
	if len(conds) == 0 {
		return false, nil // 无任何可管辖范围：不授予跨用户查看
	}
	var ok bool
	qargs := append([]any{userID}, args...)
	err := h.db.WithContext(c.Request.Context()).Raw(`
		SELECT EXISTS(
			SELECT 1 FROM users u
			LEFT JOIN departments d ON CAST(d.id AS TEXT) = CAST(u.dept_id AS TEXT)
			LEFT JOIN dept_studio_map dsm ON dsm.dept_id = CAST(u.dept_id AS TEXT)
			WHERE CAST(u.id AS TEXT) = ? AND (`+strings.Join(conds, " OR ")+`)
		)`,
		qargs...).Scan(&ok).Error
	if err != nil {
		return false, err
	}
	return ok, nil
}

// levelToScopeKey 把报告层级映射为 UsagePolicy.EffectiveScope 的 scope 字符串。
func levelToScopeKey(level string) string {
	switch level {
	case report.LevelPersonal:
		return "user"
	case report.LevelDepartment:
		return "dept"
	default:
		return "studio"
	}
}

// scopeKeyToLevel 把 EffectiveScope 返回的 scope 反向映射为报告层级。
func scopeKeyToLevel(scope string) string {
	switch scope {
	case "user":
		return report.LevelPersonal
	case "dept":
		return report.LevelDepartment
	default:
		return report.LevelStudio
	}
}

// scopeName 查询范围展示名（可空，由前端兜底）。
func (h *ReportHandler) scopeName(c *gin.Context, level, id string) string {
	if h.db == nil || id == "" {
		return ""
	}
	var name string
	switch level {
	case report.LevelPersonal:
		_ = h.db.WithContext(c.Request.Context()).Raw(`SELECT COALESCE(u.name,'') FROM users u WHERE CAST(u.id AS TEXT) = ?`, id).Scan(&name).Error
	case report.LevelDepartment:
		_ = h.db.WithContext(c.Request.Context()).Raw(`SELECT COALESCE(d.dept_name, d.name, '') FROM departments d WHERE d.dept_id = ? OR CAST(d.id AS TEXT) = ?`, id, id).Scan(&name).Error
	case report.LevelStudio:
		_ = h.db.WithContext(c.Request.Context()).Raw(`SELECT COALESCE(s.name,'') FROM studios s WHERE CAST(s.id AS TEXT) = ?`, id).Scan(&name).Error
	}
	return name
}

// parseReportPeriod 解析 period_start + period_type，返回周期的闭区间起点与（用于缓存 key 的）周期类型。
func parseReportPeriod(c *gin.Context) (from, periodType string, ok bool) {
	periodType = c.DefaultQuery("period_type", "week")
	if periodType != "week" && periodType != "month" && periodType != "day" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "period_type 必须为 week|month|day"})
		return from, periodType, false
	}
	from = c.Query("period_start")
	if from == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "period_start 必填（YYYY-MM-DD）"})
		return from, periodType, false
	}
	if _, err := time.Parse("2006-01-02", from); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "period_start 格式非法（YYYY-MM-DD）"})
		return from, periodType, false
	}
	return from, periodType, true
}

// Report GET /api/v1/usage/analysis-report — 返回 Report JSON。
func (h *ReportHandler) Report(c *gin.Context) {
	level := c.Query("level")
	if level != report.LevelPersonal && level != report.LevelDepartment && level != report.LevelStudio {
		c.JSON(http.StatusBadRequest, gin.H{"error": "level 必须为 personal|department|studio"})
		return
	}
	from, periodType, ok := parseReportPeriod(c)
	if !ok {
		return
	}
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "storage not configured"})
		return
	}
	scopeLevel, scopeID, scopeName, err := h.resolveReportScope(c, level, c.Query("id"))
	if err != nil {
		if _, isForbidden := err.(*forbiddenError); isForbidden {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	q := cpsvc.ReportSnapshotQuery{Level: scopeLevel, ScopeID: scopeID, PeriodType: periodType, PeriodStart: parsePeriodDate(from)}
	// 1) 命中 report_snapshot 缓存 → 直接返回（与 MD/HTML 同源）。
	if cached, err := h.snapshot.Get(c.Request.Context(), q); err == nil && cached != nil {
		c.JSON(http.StatusOK, cached)
		return
	}

	// 2) 未命中：调用 Orchestrator GetReport（DB 版 ReportDataProvider 注入）。
	r, err := h.assembleReport(c, scopeLevel, scopeID, scopeName, from, periodType)
	if err != nil {
		slog.Error("report assemble failed", "level", scopeLevel, "id", scopeID, "period_start", from, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "report assemble failed", "details": err.Error()})
		return
	}

	// 3) 落库缓存（失败不阻断响应）。
	periodEnd, _ := time.Parse("2006-01-02", from)
	periodEnd = periodEndOf(periodEnd, periodType)
	if err := h.snapshot.Upsert(c.Request.Context(), q, periodEnd, r); err != nil {
		slog.Error("report snapshot upsert failed", "level", scopeLevel, "id", scopeID, "error", err)
	}
	c.JSON(http.StatusOK, r)
}

// Members GET /api/v1/usage/analysis-report/members — 返回四象限成员点集（只允许 department|studio）。
func (h *ReportHandler) Members(c *gin.Context) {
	level := c.Query("level")
	if level != report.LevelDepartment && level != report.LevelStudio {
		c.JSON(http.StatusBadRequest, gin.H{"error": "level 必须为 department|studio"})
		return
	}
	from, periodType, ok := parseReportPeriod(c)
	if !ok {
		return
	}
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "storage not configured"})
		return
	}
	scopeLevel, scopeID, scopeName, err := h.resolveReportScope(c, level, c.Query("id"))
	if err != nil {
		if _, isForbidden := err.(*forbiddenError); isForbidden {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	q := cpsvc.ReportSnapshotQuery{Level: scopeLevel, ScopeID: scopeID, PeriodType: periodType, PeriodStart: parsePeriodDate(from)}
	if m, err := h.snapshot.Members(c.Request.Context(), q); err == nil && m != nil {
		c.JSON(http.StatusOK, gin.H{"member_evaluation": m})
		return
	}

	r, err := h.assembleReport(c, scopeLevel, scopeID, scopeName, from, periodType)
	if err != nil {
		slog.Error("report members assemble failed", "level", scopeLevel, "id", scopeID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "report assemble failed", "details": err.Error()})
		return
	}
	periodEnd, _ := time.Parse("2006-01-02", from)
	periodEnd = periodEndOf(periodEnd, periodType)
	if err := h.snapshot.Upsert(c.Request.Context(), q, periodEnd, r); err != nil {
		slog.Error("report snapshot upsert failed", "level", scopeLevel, "id", scopeID, "error", err)
	}
	c.JSON(http.StatusOK, gin.H{"member_evaluation": r.MemberEvaluation})
}

// AnomalyFeedbackRequest 是 3.6 待核查反馈回写请求体。
type AnomalyFeedbackRequest struct {
	// Status 目标状态：confirmed_ok | need_check | false_positive。
	Status      string `json:"status"`
	PeriodStart string `json:"period_start"`
}

// AnomalyFeedback POST /api/v1/usage/analysis-report/anomaly/:user_id/feedback — 负责人回写 3.6 待核查状态。
func (h *ReportHandler) AnomalyFeedback(c *gin.Context) {
	userID := c.Param("user_id")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id 必填"})
		return
	}
	var body AnomalyFeedbackRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体 JSON 非法"})
		return
	}
	reviewer := c.GetString("user_id")
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "storage not configured"})
		return
	}
	// RBAC：回写 3.6 待核查终裁同样必须受管辖边界约束（与 GET /report、/report/members 的
	// EffectiveScope + QueryScope + actorCanViewUser 口径一致）。非全局角色只能对本人或其所辖
	// 部门/工作室内的用户回写，避免跨 scope 写待核查终裁。
	actor := rbac.GetActor(c)
	if !actor.IsGlobal && userID != reviewer {
		ok, err := h.actorCanViewUser(c, actor, userID)
		if err != nil {
			slog.Error("anomaly feedback rbac check failed", "user_id", userID, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "rbac check failed"})
			return
		}
		if !ok {
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden: target user outside your managed scope"})
			return
		}
	}
	err := h.snapshot.WriteAnomalyFeedback(c.Request.Context(), userID, reviewer, cpsvc.AnomalyFeedback{
		Status:      body.Status,
		PeriodStart: body.PeriodStart,
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "未找到该用户待核查记录"})
		return
	}
	if err != nil {
		slog.Error("anomaly feedback write failed", "user_id", userID, "status", body.Status, "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"user_id": userID, "status": body.Status})
}

// periodEndOf 由 start + period_type 求闭区间末端（用于 report_snapshot.period_end）。
func periodEndOf(start time.Time, periodType string) time.Time {
	switch periodType {
	case "month":
		return time.Date(start.Year(), start.Month()+1, 0, 0, 0, 0, 0, start.Location())
	case "day":
		return start
	default: // week
		return start.AddDate(0, 0, 6)
	}
}

// parsePeriodDate 解析 YYYY-MM-DD，失败返回零值。
func parsePeriodDate(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// isoWeekStr 返回 ISO 周字符串（"2026-W33"，供 meta.Period.IsoWeek）。
func isoWeekStr(from string) string {
	t, err := time.Parse("2006-01-02", from)
	if err != nil {
		return from
	}
	year, week := t.ISOWeek()
	return strconv.Itoa(year) + "-W" + strconv.Itoa(week)
}
