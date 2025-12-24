package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	"github.com/example/llm-gateway/internal/observability/service"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/example/llm-gateway/internal/orgservice/rbac/policy"
)

// DashboardSummary 汇总卡片
type DashboardSummary struct {
	TotalTokens      int64   `json:"total_tokens"`
	CostEstimate     float64 `json:"cost_estimate"`
	UserCount        int     `json:"user_count"`
	CallCount        int64   `json:"call_count"`
	ErrorCount       int64   `json:"error_count"`
	SuccessRate      float64 `json:"success_rate"`
	PrevCostEstimate float64 `json:"prev_cost_estimate"`
	PrevCallCount    int64   `json:"prev_call_count"`
}

// DashboardBreakdownItem 排行列表行
type DashboardBreakdownItem struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	TotalTokens int64   `json:"total_tokens"`
	CostEst     float64 `json:"cost_estimate"`
	CallCount   int64   `json:"call_count"`
	ErrorCount  int64   `json:"error_count"`
	UserCount   int     `json:"user_count"`
	Rank        int     `json:"rank"`
}

// TrendPoint 时间趋势数据点（按日聚合）
type TrendPoint struct {
	Date        string  `json:"date"`
	Calls       int64   `json:"calls"`
	Errors      int64   `json:"errors"`
	TotalTokens int64   `json:"total_tokens"`
	Cost        float64 `json:"cost"`
}

// ModelUsage 模型用量排行
type ModelUsage struct {
	Model       string  `json:"model"`
	Calls       int64   `json:"calls"`
	TotalTokens int64   `json:"total_tokens"`
	Cost        float64 `json:"cost"`
}

// DashboardResponse 分层用量看板响应
type DashboardResponse struct {
	Scope     string                   `json:"scope"`
	ID        string                   `json:"id"`
	ScopeName string                   `json:"scope_name"`
	Period    map[string]string        `json:"period"`
	Summary   DashboardSummary         `json:"summary"`
	Breakdown []DashboardBreakdownItem `json:"breakdown"`
	Total     int                      `json:"total"`
	Page      int                      `json:"page"`
	PageSize  int                      `json:"page_size"`
}

// UsageHandler 用量与成本查询接口
type UsageHandler struct {
	svc *service.RollupService
	db  *gorm.DB
}

// NewUsageHandler 创建 UsageHandler
func NewUsageHandler(svc *service.RollupService, db ...*gorm.DB) *UsageHandler {
	h := &UsageHandler{svc: svc}
	if len(db) > 0 {
		h.db = db[0]
	}
	return h
}

// Trace GET /api/v1/usage/logs/:request_id/trace — 单条请求完整 trace
func (h *UsageHandler) Trace(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "trace storage not configured"})
		return
	}
	rid := c.Param("request_id")
	if rid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request_id required"})
		return
	}
	var trace obsmodel.CallTrace
	if err := h.db.WithContext(c.Request.Context()).First(&trace, "request_id = ?", rid).Error; err != nil {
		if err.Error() == "record not found" {
			c.JSON(http.StatusNotFound, gin.H{"error": "trace not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	c.JSON(http.StatusOK, trace)
}

// Query GET /api/v1/usage — 聚合用量查询
func (h *UsageHandler) Query(c *gin.Context) {
	from, to, ok := parseDateRange(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	params := service.UsageDailyQueryParams{
		From:      from,
		To:        to,
		GroupBy:   c.DefaultQuery("group_by", "day"),
		KeyID:     c.Query("key_id"),
		KeyName:   c.Query("key_name"),
		UserName:  c.Query("user_name"),
		DeptName:  c.Query("dept_name"),
		StudioID:  c.Query("studio_id"),
		ModelName: c.Query("model_name"),
		GroupName: c.Query("group_name"),
		Provider:  c.Query("provider"),
		Status:    c.Query("status"),
		Page:      page,
		PageSize:  pageSize,
	}
	resp, err := h.svc.Query(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// Options GET /api/v1/usage/options — 返回可用过滤枚举（provider / dept / studio）
func (h *UsageHandler) Options(c *gin.Context) {
	opts, err := h.svc.QueryOptions(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, opts)
}

// Logs GET /api/v1/usage/logs — 调用明细查询
func (h *UsageHandler) Logs(c *gin.Context) {
	from, to, ok := parseDateRange(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	params := service.LogsQueryParams{
		From:      from,
		To:        to,
		KeyID:     c.Query("key_id"),
		KeyName:   c.Query("key_name"),
		UserName:  c.Query("user_name"),
		DeptName:  c.Query("dept_name"),
		StudioID:  c.Query("studio_id"),
		ModelName: c.Query("model_name"),
		GroupName: c.Query("group_name"),
		Provider:  c.Query("provider"),
		Status:    c.Query("status"),
		Page:      page,
		PageSize:  pageSize,
	}
	resp, err := h.svc.QueryLogs(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query logs failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// MyQuery GET /api/v1/my/usage — 当前登录用户的聚合用量统计
func (h *UsageHandler) MyQuery(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	from, to, ok := parseDateRange(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	params := service.MyStatsQueryParams{
		From:     from,
		To:       to,
		UserID:   userID,
		GroupBy:  c.DefaultQuery("group_by", "day"),
		Model:    c.Query("model_name"), // 前端传 model_name，与 Query 接口对齐
		Page:     page,
		PageSize: pageSize,
	}
	resp, err := h.svc.QueryMyStats(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// MyLogs GET /api/v1/my/usage/logs — 当前登录用户的调用明细（user_id 隔离）
func (h *UsageHandler) MyLogs(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	from, to, ok := parseDateRange(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	params := service.LogsQueryParams{
		From:      from,
		To:        to,
		UserID:    userID,
		KeyID:     c.Query("key_id"),
		KeyName:   c.Query("key_name"),
		UserName:  c.Query("user_name"),
		ModelName: c.Query("model_name"),
		GroupName: c.Query("group_name"),
		Page:      page,
		PageSize:  pageSize,
	}
	resp, err := h.svc.QueryLogs(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query logs failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// parseDateRange 解析并校验日期范围参数
func parseDateRange(c *gin.Context) (from, to time.Time, ok bool) {
	fromStr := c.Query("from")
	toStr := c.Query("to")
	if fromStr == "" || toStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "from and to are required (YYYY-MM-DD)"})
		return
	}
	var err error
	from, err = time.Parse("2006-01-02", fromStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid from date"})
		return
	}
	to, err = time.Parse("2006-01-02", toStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid to date"})
		return
	}
	if from.After(to) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "from must be before or equal to to"})
		return
	}
	if to.Sub(from) > 90*24*time.Hour {
		c.JSON(http.StatusBadRequest, gin.H{"error": "date range must not exceed 90 days"})
		return
	}
	ok = true
	return
}

// GetDashboard GET /api/v1/usage/dashboard
// 按 scope(company|studio|dept|user) 聚合用量，支持按角色注入过滤条件
func (h *UsageHandler) GetDashboard(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "db not configured"})
		return
	}

	scope := c.DefaultQuery("scope", "company")
	id := c.Query("id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	startStr := c.DefaultQuery("start", time.Now().AddDate(0, -1, 0).Format("2006-01-02"))
	endStr := c.DefaultQuery("end", time.Now().Format("2006-01-02"))
	modelName   := c.Query("model_name")
	filterStatus := c.Query("status")

	// 用 Actor 替代手动查 user_roles 的 getEffectiveRole
	// Guard(PermUsageViewScoped) middleware 已在路由层注入 Actor
	userID := c.GetString("user_id")
	actor := rbac.GetActor(c)
	usagePolicy := policy.UsagePolicy{}
	scope, id, _ = usagePolicy.EffectiveScope(actor, scope, id, userID)

	// 构建聚合 SQL
	// scope 语义：
	//   company              → 按工作室聚合（全局）
	//   studio (id="")       → 按工作室聚合（studio_admin 过滤到自己管辖）
	//   studio (id=<uuid>)   → 按部门聚合，过滤到该工作室下的部门（下钻）
	//   dept   (id=<sso_id>) → 按用户聚合，过滤到该部门的成员
	//   user   (id=<uid>)    → 按模型聚合，只看该用户
	var rows []struct {
		GroupID     string  `gorm:"column:group_id"`
		GroupName   string  `gorm:"column:group_name"`
		TotalTokens int64   `gorm:"column:total_tokens"`
		CostEst     float64 `gorm:"column:cost_est"`
		UserCount   int     `gorm:"column:user_count"`
		CallCount   int64   `gorm:"column:call_count"`
		ErrorCount  int64   `gorm:"column:error_count"`
	}

	db := h.db.WithContext(c.Request.Context())

	var groupExpr, nameExpr, whereClause string
	var args []interface{}

	switch scope {
	case "dept":
		// 按用户聚合，可选按部门过滤
		// cl.dept_id 可能为空（历史数据）或 SSO EhrDeptID 或 departments.id UUID
		groupExpr = "cl.user_id"
		nameExpr = "COALESCE(u.name, cl.user_id)"
		if id != "" {
			whereClause = "AND (cl.dept_id = ? OR cl.dept_id IN (SELECT id::text FROM departments WHERE dept_id = ?) OR cl.user_id IN (SELECT u2.id::text FROM users u2 JOIN departments d2 ON d2.id = u2.dept_id WHERE d2.dept_id = ?))"
			args = append(args, id, id, id)
		}
	case "studio":
		if id != "" {
			// 下钻到工作室内的部门列表
			// 用 COALESCE(d.dept_id, cl.dept_id) 归一化 dept_id：
			// call_logs.dept_id 历史上混用了 SSO dept_id 和 departments.id UUID，
			// 通过 d.dept_id（SSO 标准 ID）统一，避免同一部门出现两行
			groupExpr = "COALESCE(d.dept_id, cl.dept_id)"
			nameExpr = "COALESCE(d.dept_name, cl.dept_id)"
			whereClause = "AND dsm.studio_id::text = ?"
			args = append(args, id)
		} else {
			// 按工作室聚合（studio_admin 起始视图）
			groupExpr = "dsm.studio_id::text"
			nameExpr = "COALESCE(s.name, dsm.studio_id::text)"
		}
	case "user":
		// 按模型聚合，只看指定用户
		groupExpr = "cl.model"
		nameExpr = "cl.model"
		if id != "" {
			whereClause = "AND cl.user_id = ?"
			args = append(args, id)
		}
	default: // company
		groupExpr = "dsm.studio_id::text"
		nameExpr = "COALESCE(s.name, dsm.studio_id::text)"
	}

	// 模型/状态筛选（可选，与 scope 筛选叠加）
	if modelName != "" {
		whereClause += " AND (cl.model ILIKE ? OR cl.routed_model ILIKE ? OR cl.group_name ILIKE ?)"
		like := "%" + modelName + "%"
		args = append(args, like, like, like)
	}
	if filterStatus == "success" {
		whereClause += " AND cl.status = 200"
	} else if filterStatus == "error" {
		whereClause += " AND cl.status != 200"
	}

	// studio_admin 钻取 dept scope 时，额外加工作室限制保障数据安全
	// 用占位符展开 IN 子句，避免 ANY(?) 与 GORM raw SQL 的兼容问题
	// 同时兼容 call_logs.studio_id 为空的历史数据：通过 dept_studio_map / departments 反查
	if scope == "dept" && len(actor.StudioIDs) > 0 {
		placeholders := make([]string, len(actor.StudioIDs))
		for i := range actor.StudioIDs {
			placeholders[i] = "?"
		}
		studioList := strings.Join(placeholders, ",")
		// 三个 IN 子句各需要一份 studioIDs 参数
		for j := 0; j < 3; j++ {
			for _, sid := range actor.StudioIDs {
				args = append(args, sid)
			}
		}
		whereClause += " AND (" +
			"cl.studio_id IN (" + studioList + ")" +
			" OR cl.dept_id IN (SELECT dsm.dept_id FROM dept_studio_map dsm WHERE dsm.studio_id::text IN (" + studioList + "))" +
			" OR cl.dept_id IN (SELECT id::text FROM departments dpt WHERE dpt.studio_id::text IN (" + studioList + ")))"
	}

	baseArgs := []interface{}{startStr, endStr}
	queryArgs := append(baseArgs, args...)

	rawSQL := `
		SELECT ` + groupExpr + ` AS group_id,
		       ` + nameExpr + ` AS group_name,
		       SUM(cl.prompt_tokens + cl.completion_tokens + cl.reasoning_tokens) AS total_tokens,
		       SUM(cl.est_cost_usd) AS cost_est,
		       COUNT(DISTINCT cl.user_id) AS user_count,
		       COUNT(*) AS call_count,
		       COUNT(*) FILTER (WHERE cl.status != 200) AS error_count
		FROM call_logs cl
		LEFT JOIN departments d ON (d.dept_id = cl.dept_id OR d.id::text = cl.dept_id)
		LEFT JOIN dept_studio_map dsm ON dsm.dept_id = COALESCE(d.dept_id, cl.dept_id)
		LEFT JOIN studios s ON s.id = dsm.studio_id
		LEFT JOIN users u ON u.id::text = cl.user_id
		WHERE cl.ts::date BETWEEN ?::date AND ?::date
		` + whereClause + `
		GROUP BY ` + groupExpr + `, ` + nameExpr + `
		ORDER BY cost_est DESC
	`

	if err := db.Raw(rawSQL, queryArgs...).Scan(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 汇总当期（cost/tokens/calls 从排行直接加总；user_count 需全局去重避免跨组重复计数）
	var summary DashboardSummary
	for _, r := range rows {
		summary.TotalTokens += r.TotalTokens
		summary.CostEstimate += r.CostEst
		summary.CallCount += r.CallCount
		summary.ErrorCount += r.ErrorCount
		// 不累加 r.UserCount，下面单独查全局去重人数
	}
	// 全局去重：用相同的 Actor 过滤条件查当期内活跃的唯一用户数
	var userCountRow struct {
		Count int `gorm:"column:cnt"`
	}
	userCountSQL := `
		SELECT COUNT(DISTINCT cl.user_id) AS cnt
		FROM call_logs cl
		LEFT JOIN dept_studio_map dsm ON dsm.dept_id = cl.dept_id
		WHERE cl.ts::date BETWEEN ?::date AND ?::date
		` + whereClause
	_ = db.Raw(userCountSQL, queryArgs...).Scan(&userCountRow).Error
	summary.UserCount = userCountRow.Count
	if summary.CallCount > 0 {
		summary.SuccessRate = float64(summary.CallCount-summary.ErrorCount) / float64(summary.CallCount) * 100
	} else {
		summary.SuccessRate = 100
	}

	// 计算上一周期（环比）：相同时长的前一段
	startDate, _ := time.Parse("2006-01-02", startStr)
	endDate, _ := time.Parse("2006-01-02", endStr)
	dur := endDate.Sub(startDate) + 24*time.Hour
	prevEnd := startDate.AddDate(0, 0, -1)
	prevStart := prevEnd.Add(-dur + 24*time.Hour)
	var prevRow struct {
		Cost  float64 `gorm:"column:cost"`
		Calls int64   `gorm:"column:calls"`
	}
	_ = db.Table("call_logs").
		Select("SUM(est_cost_usd) AS cost, COUNT(*) AS calls").
		Where("ts::date BETWEEN ?::date AND ?::date", prevStart.Format("2006-01-02"), prevEnd.Format("2006-01-02")).
		Scopes(usagePolicy.QueryScope(actor)).
		Scan(&prevRow).Error
	summary.PrevCostEstimate = prevRow.Cost
	summary.PrevCallCount = prevRow.Calls

	// 分页
	total := len(rows)
	start := (page - 1) * pageSize
	end := start + pageSize
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}
	paged := rows[start:end]

	breakdown := make([]DashboardBreakdownItem, 0, len(paged))
	for i, r := range paged {
		breakdown = append(breakdown, DashboardBreakdownItem{
			ID:          r.GroupID,
			Name:        r.GroupName,
			TotalTokens: r.TotalTokens,
			CostEst:     r.CostEst,
			CallCount:   r.CallCount,
			ErrorCount:  r.ErrorCount,
			UserCount:   r.UserCount,
			Rank:        start + i + 1,
		})
	}

	c.JSON(http.StatusOK, DashboardResponse{
		Scope:     scope,
		ID:        id,
		Period:    map[string]string{"start": startStr, "end": endStr},
		Summary:   summary,
		Breakdown: breakdown,
		Total:     total,
		Page:      page,
		PageSize:  pageSize,
	})
}

// DashboardTrend GET /api/v1/usage/dashboard/trend
// 返回时间段内每日聚合：调用量、失败量、Token、成本（供前端趋势图使用）
func (h *UsageHandler) DashboardTrend(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "db not configured"})
		return
	}
	startStr := c.DefaultQuery("start", time.Now().AddDate(0, -1, 0).Format("2006-01-02"))
	endStr := c.DefaultQuery("end", time.Now().Format("2006-01-02"))
	filterUserID  := c.Query("user_id")
	filterModel   := c.Query("model_name")
	filterStatus  := c.Query("status")

	actor := rbac.GetActor(c)
	usagePol := policy.UsagePolicy{}

	var rows []struct {
		Date        string  `gorm:"column:date"`
		Calls       int64   `gorm:"column:calls"`
		Errors      int64   `gorm:"column:errors"`
		TotalTokens int64   `gorm:"column:total_tokens"`
		Cost        float64 `gorm:"column:cost"`
	}
	q := h.db.WithContext(c.Request.Context()).
		Table("call_logs").
		Select(`ts::date::text AS date,
			COUNT(*) AS calls,
			COUNT(*) FILTER (WHERE status != 200) AS errors,
			SUM(prompt_tokens + completion_tokens + reasoning_tokens) AS total_tokens,
			SUM(est_cost_usd) AS cost`).
		Where("ts::date BETWEEN ?::date AND ?::date", startStr, endStr).
		Scopes(usagePol.QueryScope(actor))
	if filterUserID != "" {
		q = q.Where("user_id = ?", filterUserID)
	}
	if filterModel != "" {
		q = q.Where("(model ILIKE ? OR routed_model ILIKE ? OR group_name ILIKE ?)", "%"+filterModel+"%", "%"+filterModel+"%", "%"+filterModel+"%")
	}
	if filterStatus == "success" {
		q = q.Where("status = 200")
	} else if filterStatus == "error" {
		q = q.Where("status != 200")
	}
	if err := q.Group("ts::date").
		Order("ts::date ASC").
		Scan(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	points := make([]TrendPoint, 0, len(rows))
	for _, r := range rows {
		points = append(points, TrendPoint{
			Date:        r.Date,
			Calls:       r.Calls,
			Errors:      r.Errors,
			TotalTokens: r.TotalTokens,
			Cost:        r.Cost,
		})
	}
	c.JSON(http.StatusOK, gin.H{"rows": points})
}

// DashboardModels GET /api/v1/usage/dashboard/models
// 返回时间段内 Top N 模型成本分布
func (h *UsageHandler) DashboardModels(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "db not configured"})
		return
	}
	startStr := c.DefaultQuery("start", time.Now().AddDate(0, -1, 0).Format("2006-01-02"))
	endStr := c.DefaultQuery("end", time.Now().Format("2006-01-02"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if limit < 1 || limit > 30 {
		limit = 10
	}
	filterUserID := c.Query("user_id")
	filterStatus := c.Query("status")

	actor := rbac.GetActor(c)
	usagePol := policy.UsagePolicy{}

	var rows []struct {
		Model       string  `gorm:"column:model"`
		Calls       int64   `gorm:"column:calls"`
		TotalTokens int64   `gorm:"column:total_tokens"`
		Cost        float64 `gorm:"column:cost"`
	}
	q := h.db.WithContext(c.Request.Context()).
		Table("call_logs").
		Select(`model,
			COUNT(*) AS calls,
			SUM(prompt_tokens + completion_tokens + reasoning_tokens) AS total_tokens,
			SUM(est_cost_usd) AS cost`).
		Where("ts::date BETWEEN ?::date AND ?::date", startStr, endStr).
		Scopes(usagePol.QueryScope(actor))
	if filterUserID != "" {
		q = q.Where("user_id = ?", filterUserID)
	}
	if filterStatus == "success" {
		q = q.Where("status = 200")
	} else if filterStatus == "error" {
		q = q.Where("status != 200")
	}
	if err := q.Group("model").
		Order("cost DESC").
		Limit(limit).
		Scan(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	result := make([]ModelUsage, 0, len(rows))
	for _, r := range rows {
		result = append(result, ModelUsage{
			Model:       r.Model,
			Calls:       r.Calls,
			TotalTokens: r.TotalTokens,
			Cost:        r.Cost,
		})
	}
	c.JSON(http.StatusOK, gin.H{"rows": result})
}

// ScopedLogs GET /api/v1/usage/scoped/logs
// 按 JWT role 自动注入 scope 过滤，只返回元数据（无 prompt/response 内容）：
//   admin       → 全公司所有记录
//   studio_admin → 自己工作室下所有部门成员的记录
//   dept_admin  → 自己部门成员的记录
//   其他         → 仅自己的记录（等同 /my/usage/logs）
func (h *UsageHandler) ScopedLogs(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "db not configured"})
		return
	}

	startStr := c.DefaultQuery("start", time.Now().AddDate(0, -1, 0).Format("2006-01-02"))
	endStr := c.DefaultQuery("end", time.Now().Format("2006-01-02"))
	modelName    := c.Query("model_name")
	statusStr    := c.Query("status")   // "success" | "error" | ""
	filterUserID := c.Query("user_id")  // 查看特定成员时传入（精确）
	filterUser   := c.Query("user_name") // 成员姓名模糊搜索
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}

	db := h.db.WithContext(c.Request.Context())

	// 用 Actor 过滤：Guard(PermUsageViewScoped) 已在路由层注入 Actor
	actor := rbac.GetActor(c)
	usagePol := policy.UsagePolicy{}

	base := db.Table("call_logs").
		Where("ts::date BETWEEN ? AND ?", startStr, endStr).
		Scopes(usagePol.QueryScope(actor))

	// 可选过滤条件
	if filterUserID != "" {
		base = base.Where("user_id = ?", filterUserID)
	}
	if filterUser != "" {
		base = base.Where("user_name ILIKE ?", "%"+filterUser+"%")
	}
	if modelName != "" {
		base = base.Where("model ILIKE ?", "%"+modelName+"%")
	}
	switch statusStr {
	case "success":
		base = base.Where("status = 200")
	case "error":
		base = base.Where("status <> 200")
	}

	// Count
	var total int64
	if err := base.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	cols := `request_id, ts,
		key_name, user_id, user_name, dept_id, dept_name, studio_id, studio_name,
		model, routed_model, group_name, provider,
		prompt_tokens, cached_tokens, cache_write_tokens,
		completion_tokens, reasoning_tokens, total_tokens,
		status, duration_ms, first_token_ms,
		est_cost_usd, input_cost_usd, output_cost_usd,
		stream, failover_used, usage_missing, reasoning_effort,
		ip_address, request_body, response_body`

	var rows []map[string]interface{}
	if err := base.Select(cols).
		Order("ts DESC").
		Limit(pageSize).Offset((page - 1) * pageSize).
		Find(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"rows":      rows,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

func toString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func toString2(v interface{}, _ bool) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
