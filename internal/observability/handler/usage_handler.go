package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
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
	// dept 范围下程序调用（Service Key）的独立聚合，用于「成员 + 程序」对账展示。
	ServiceCostEstimate float64 `json:"service_cost_estimate"`
	ServiceCallCount    int64   `json:"service_call_count"`
	ServiceTotalTokens  int64   `json:"service_total_tokens"`
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
	rdb *redis.Client
}

// NewUsageHandler 创建 UsageHandler
func NewUsageHandler(svc *service.RollupService, db ...*gorm.DB) *UsageHandler {
	h := &UsageHandler{svc: svc}
	if len(db) > 0 {
		h.db = db[0]
	}
	return h
}

// SetRedis 注入可选 Redis 缓存，用于全站百分位等低频统计。
func (h *UsageHandler) SetRedis(rdb *redis.Client) {
	h.rdb = rdb
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
		Endpoint:  c.Query("endpoint"),
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
	seedance := parseBoolQuery(c.Query("seedance"))
	from, to, ok := parseDateRangeWithDefault(c, seedance)
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
		KeyType:   c.Query("key_type"),
		UserName:  c.Query("user_name"),
		DeptName:  c.Query("dept_name"),
		StudioID:  c.Query("studio_id"),
		ModelName: c.Query("model_name"),
		GroupName: c.Query("group_name"),
		Provider:  c.Query("provider"),
		Status:    c.Query("status"),
		Endpoint:  c.Query("endpoint"),
		Seedance:  seedance,
		Page:      page,
		PageSize:  pageSize,
		Cursor:    c.Query("cursor"),
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
	seedance := parseBoolQuery(c.Query("seedance"))
	from, to, ok := parseDateRangeWithDefault(c, seedance)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	params := service.MyStatsQueryParams{
		From:      from,
		To:        to,
		UserID:    userID,
		GroupBy:   c.DefaultQuery("group_by", "day"),
		Model:     c.Query("model_name"), // 前端传 model_name，与 Query 接口对齐
		KeyID:     c.Query("key_id"),
		GroupName: c.Query("group_name"),
		Status:    c.Query("status"),
		Endpoint:  c.Query("endpoint"),
		Page:      page,
		PageSize:  pageSize,
	}
	resp, err := h.svc.QueryMyStats(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// UsagePercentilesResponse 全站用户近 30 天 P90 阈值。
type UsagePercentilesResponse struct {
	P90DailyAvgCalls  float64   `json:"p90_daily_avg_calls" gorm:"column:p90_daily_avg_calls"`
	P90CacheHitPct    float64   `json:"p90_cache_hit_pct" gorm:"column:p90_cache_hit_pct"`
	P90OutputRatio    float64   `json:"p90_output_ratio" gorm:"column:p90_output_ratio"`
	P90UniqueModels   float64   `json:"p90_unique_models" gorm:"column:p90_unique_models"`
	P90ReasoningShare float64   `json:"p90_reasoning_share" gorm:"column:p90_reasoning_share"`
	CalculatedAt      time.Time `json:"calculated_at" gorm:"-"`
}

// MyPercentiles GET /api/v1/my/usage/percentiles — 全站用户近 30 天 P90 标签阈值。
func (h *UsageHandler) MyPercentiles(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "db not configured"})
		return
	}

	const cacheKey = "usage:percentiles:p90:30d"
	if h.rdb != nil {
		if raw, err := h.rdb.Get(c.Request.Context(), cacheKey).Bytes(); err == nil {
			var cached UsagePercentilesResponse
			if err := json.Unmarshal(raw, &cached); err == nil {
				c.JSON(http.StatusOK, gin.H{"data": cached})
				return
			}
		}
	}

	var resp UsagePercentilesResponse
	rawSQL := `
		WITH user_stats AS (
			SELECT
				user_id,
				SUM(req_count)::float / COUNT(DISTINCT stat_date) AS daily_avg,
				CASE WHEN SUM(prompt_tokens) > 0
					THEN (SUM(cached_tokens)::float / SUM(prompt_tokens)) * 100
					ELSE 0 END AS cache_pct,
				CASE WHEN SUM(prompt_tokens - cached_tokens) > 0
					THEN SUM(completion_tokens)::float / SUM(prompt_tokens - cached_tokens)
					ELSE 0 END AS out_ratio,
				COUNT(DISTINCT model) AS model_cnt,
				CASE WHEN SUM(completion_tokens) > 0
					THEN SUM(reasoning_tokens)::float / SUM(completion_tokens)
					ELSE 0 END AS reasoning_share
			FROM usage_daily
			WHERE stat_date >= CURRENT_DATE - INTERVAL '30 days'
			GROUP BY user_id
			HAVING SUM(req_count) > 0
		)
		SELECT
			COALESCE(ROUND((PERCENTILE_CONT(0.9) WITHIN GROUP (ORDER BY daily_avg))::numeric, 0), 0)::float AS p90_daily_avg_calls,
			COALESCE(ROUND((PERCENTILE_CONT(0.9) WITHIN GROUP (ORDER BY cache_pct))::numeric, 1), 0)::float AS p90_cache_hit_pct,
			COALESCE(ROUND((PERCENTILE_CONT(0.9) WITHIN GROUP (ORDER BY out_ratio))::numeric, 2), 0)::float AS p90_output_ratio,
			COALESCE(PERCENTILE_CONT(0.9) WITHIN GROUP (ORDER BY model_cnt), 0)::float AS p90_unique_models,
			COALESCE(ROUND((PERCENTILE_CONT(0.9) WITHIN GROUP (ORDER BY reasoning_share))::numeric, 3), 0)::float AS p90_reasoning_share
		FROM user_stats
	`
	if err := h.db.WithContext(c.Request.Context()).Raw(rawSQL).Scan(&resp).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query percentiles failed", "details": err.Error()})
		return
	}
	resp.CalculatedAt = time.Now()

	if h.rdb != nil {
		if raw, err := json.Marshal(resp); err == nil {
			_ = h.rdb.Set(c.Request.Context(), cacheKey, raw, time.Hour).Err()
		}
	}

	c.JSON(http.StatusOK, gin.H{"data": resp})
}

// MyLogs GET /api/v1/my/usage/logs — 当前登录用户的调用明细（user_id 隔离）
func (h *UsageHandler) MyLogs(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}
	seedance := parseBoolQuery(c.Query("seedance"))
	from, to, ok := parseDateRangeWithDefault(c, seedance)
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
		KeyType:   c.Query("key_type"),
		UserName:  c.Query("user_name"),
		ModelName: c.Query("model_name"),
		GroupName: c.Query("group_name"),
		Provider:  c.Query("provider"),
		Status:    c.Query("status"),
		Endpoint:  c.Query("endpoint"),
		Seedance:  seedance,
		Page:      page,
		PageSize:  pageSize,
		Cursor:    c.Query("cursor"),
	}
	resp, err := h.svc.QueryLogs(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query logs failed", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// parseDateRange 解析并校验日期范围参数
// 支持两种格式：YYYY-MM-DD（天精度）和 YYYY-MM-DDTHH:MM / YYYY-MM-DD HH:MM（小时精度）
func parseDateRange(c *gin.Context) (from, to time.Time, ok bool) {
	return parseDateRangeWithDefault(c, false)
}

func parseDateRangeWithDefault(c *gin.Context, allowDefault bool) (from, to time.Time, ok bool) {
	fromStr := c.Query("from")
	toStr := c.Query("to")
	if fromStr == "" || toStr == "" {
		if allowDefault {
			to = time.Now()
			from = to.AddDate(0, 0, -30)
			ok = true
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "from and to are required (YYYY-MM-DD or YYYY-MM-DDTHH:MM)"})
		return
	}
	var err error
	from, err = parseDateTime(fromStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid from date"})
		return
	}
	to, err = parseDateTime(toStr)
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

// parseDateTime 尝试多种格式解析日期时间
func parseDateTime(s string) (time.Time, error) {
	// 尝试带时间的格式
	for _, layout := range []string{
		"2006-01-02T15:04",
		"2006-01-02 15:04",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	// 回退到天精度
	return time.Parse("2006-01-02", s)
}

func parseBoolQuery(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
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
	modelName := c.Query("model_name")
	filterStatus := c.Query("status")
	filterEndpoint := c.Query("endpoint")

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
	if filterEndpoint != "" {
		whereClause += " AND cl.inbound_endpoint = ?"
		args = append(args, filterEndpoint)
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

	// dept 范围：成员排行排除程序调用（Service Key），程序调用单独聚合回填到 summary。
	rawWhere := whereClause
	if scope == "dept" {
		rawWhere = whereClause + " AND (" + obsmodel.WhereExcludeServiceKeyLogsCL + ")"
	}

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
		WHERE cl.ts >= ?::date AND cl.ts < (?::date + INTERVAL '1 day')
		` + rawWhere + `
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

	// dept 范围：程序调用（Service Key）单独聚合，并回填总量，使「成员 + 程序」可对账。
	// 复用同一 whereClause（含部门/模型/状态/端点/工作室安全过滤），仅追加 service 条件。
	if scope == "dept" {
		var svcRow struct {
			Cost   float64 `gorm:"column:cost"`
			Calls  int64   `gorm:"column:calls"`
			Tokens int64   `gorm:"column:tokens"`
			Errors int64   `gorm:"column:errors"`
		}
		svcSQL := `
			SELECT COALESCE(SUM(cl.est_cost_usd), 0) AS cost,
			       COUNT(*) AS calls,
			       COALESCE(SUM(cl.prompt_tokens + cl.completion_tokens + cl.reasoning_tokens), 0) AS tokens,
			       COUNT(*) FILTER (WHERE cl.status != 200) AS errors
			FROM call_logs cl
			WHERE cl.ts >= ?::date AND cl.ts < (?::date + INTERVAL '1 day')
				` + whereClause + `
				AND (` + obsmodel.WhereServiceKeyLogsCL + `)
		`
		if err := db.Raw(svcSQL, queryArgs...).Scan(&svcRow).Error; err == nil {
			summary.ServiceCostEstimate = svcRow.Cost
			summary.ServiceCallCount = svcRow.Calls
			summary.ServiceTotalTokens = svcRow.Tokens
			summary.CostEstimate += svcRow.Cost
			summary.CallCount += svcRow.Calls
			summary.TotalTokens += svcRow.Tokens
			summary.ErrorCount += svcRow.Errors
		}
	}

	// 全局去重：用相同的 Actor 过滤条件查当期内活跃的唯一用户数
	var userCountRow struct {
		Count int `gorm:"column:cnt"`
	}
	userCountSQL := `
		SELECT COUNT(DISTINCT cl.user_id) AS cnt
		FROM call_logs cl
		LEFT JOIN dept_studio_map dsm ON dsm.dept_id = cl.dept_id
		WHERE cl.ts >= ?::date AND cl.ts < (?::date + INTERVAL '1 day')
			AND cl.user_id != ''
			AND (` + obsmodel.WhereExcludeServiceKeyLogsCL + `)
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
		Where("ts >= ?::date AND ts < (?::date + INTERVAL '1 day')", prevStart.Format("2006-01-02"), prevEnd.Format("2006-01-02")).
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
	filterUserID := c.Query("user_id")
	filterModel := c.Query("model_name")
	filterStatus := c.Query("status")
	endpoint := c.Query("endpoint")

	actor := rbac.GetActor(c)
	usagePol := policy.UsagePolicy{}

	type trendRow struct {
		Date        string  `gorm:"column:date"`
		Calls       int64   `gorm:"column:calls"`
		Errors      int64   `gorm:"column:errors"`
		TotalTokens int64   `gorm:"column:total_tokens"`
		Cost        float64 `gorm:"column:cost"`
	}
	var rows []trendRow

	// 快速路径：全局视角且无精细过滤时，历史日期读 usage_daily 预聚合表，今天读 call_logs 实时，
	// 兼顾速度与实时性。usage_daily 按 (stat_date, key_id, dept_id, studio_id, model) 聚合，
	// 跨维度 SUM 与 call_logs 逐日聚合等价；用数据库 CURRENT_DATE 切分「今天/历史」，避免
	// Go 本地时区与 PG 会话时区不一致导致日期错配。
	useFastPath := actor.IsGlobal && filterUserID == "" && filterModel == "" && filterStatus == "" && endpoint == ""
	if useFastPath && !h.db.Migrator().HasTable("usage_daily") {
		useFastPath = false // usage_daily 尚未建表，回退 call_logs
	}
	if useFastPath {
		// 历史段（CURRENT_DATE 之前）：usage_daily 预聚合，避开 call_logs 全表扫描
		var histRows []trendRow
		if err := h.db.WithContext(c.Request.Context()).
			Table("usage_daily").
			Select(`stat_date::text AS date,
				SUM(req_count) AS calls,
				SUM(req_count - success_count) AS errors,
				SUM(prompt_tokens + completion_tokens + reasoning_tokens) AS total_tokens,
				SUM(est_cost_usd) AS cost`).
			Where("stat_date >= ?::date AND stat_date <= ?::date AND stat_date < CURRENT_DATE", startStr, endStr).
			Group("stat_date").
			Order("stat_date ASC").
			Scan(&histRows).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		// 今天段（CURRENT_DATE 起）：call_logs 实时，保证最近数据零滞后；扫描量仅今天一天
		var todayRows []trendRow
		if err := h.db.WithContext(c.Request.Context()).
			Table("call_logs").
			Select(`ts::date::text AS date,
				COUNT(*) AS calls,
				COUNT(*) FILTER (WHERE status != 200) AS errors,
				SUM(prompt_tokens + completion_tokens + reasoning_tokens) AS total_tokens,
				SUM(est_cost_usd) AS cost`).
			Where("ts >= CURRENT_DATE AND ts < (?::date + INTERVAL '1 day')", endStr).
			Group("ts::date").
			Order("ts::date ASC").
			Scan(&todayRows).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		rows = append(histRows, todayRows...)
	}

	if !useFastPath {
		q := h.db.WithContext(c.Request.Context()).
			Table("call_logs").
			Select(`ts::date::text AS date,
				COUNT(*) AS calls,
				COUNT(*) FILTER (WHERE status != 200) AS errors,
				SUM(prompt_tokens + completion_tokens + reasoning_tokens) AS total_tokens,
				SUM(est_cost_usd) AS cost`).
			Where("ts >= ?::date AND ts < (?::date + INTERVAL '1 day')", startStr, endStr).
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
		if endpoint != "" {
			q = q.Where("inbound_endpoint = ?", endpoint)
		}
		if err := q.Group("ts::date").
			Order("ts::date ASC").
			Scan(&rows).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
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
		Where("ts >= ?::date AND ts < (?::date + INTERVAL '1 day')", startStr, endStr).
		Where("LENGTH(model) <= 128"). // 过滤脏数据：model 字段误填 prompt 内容的异常记录
		Scopes(usagePol.QueryScope(actor))
	if filterUserID != "" {
		q = q.Where("user_id = ?", filterUserID)
	}
	if filterStatus == "success" {
		q = q.Where("status = 200")
	} else if filterStatus == "error" {
		q = q.Where("status != 200")
	}
	if endpoint := c.Query("endpoint"); endpoint != "" {
		q = q.Where("inbound_endpoint = ?", endpoint)
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
//
//	admin       → 全公司所有记录
//	studio_admin → 自己工作室下所有部门成员的记录
//	dept_admin  → 自己部门成员的记录
//	其他         → 仅自己的记录（等同 /my/usage/logs）
func (h *UsageHandler) ScopedLogs(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "db not configured"})
		return
	}

	startStr := c.DefaultQuery("start", time.Now().AddDate(0, -1, 0).Format("2006-01-02"))
	endStr := c.DefaultQuery("end", time.Now().Format("2006-01-02"))
	modelName := c.Query("model_name")
	statusStr := c.Query("status")         // "success" | "error" | ""
	filterUserID := c.Query("user_id")     // 查看特定成员时传入（精确）
	filterUser := c.Query("user_name")     // 成员姓名模糊搜索
	filterDeptID := c.Query("dept_id")     // 按部门筛选
	filterStudioID := c.Query("studio_id") // 按工作室筛选
	filterKeyType := c.Query("key_type")   // personal | service
	filterProvider := c.Query("provider")
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

	// 列名加 call_logs. 前缀：后续 LEFT JOIN call_traces 时避免 ts 歧义
	base := db.Table("call_logs").
		Where("call_logs.ts >= ?::date AND call_logs.ts < (?::date + INTERVAL '1 day')", startStr, endStr).
		Scopes(usagePol.QueryScope(actor))

	// 可选过滤条件
	if filterUserID != "" {
		base = base.Where("call_logs.user_id = ?", filterUserID)
	}
	if filterUser != "" {
		base = base.Where("call_logs.user_name ILIKE ?", "%"+filterUser+"%")
	}
	if filterDeptID != "" {
		base = base.Where("call_logs.dept_id = ?", filterDeptID)
	}
	if filterStudioID != "" {
		base = base.Where("call_logs.studio_id = ?", filterStudioID)
	}
	if filterKeyType != "" {
		base = base.Where("COALESCE(NULLIF(call_logs.key_type, ''), 'personal') = ?", filterKeyType)
	}
	if modelName != "" {
		base = base.Where("(call_logs.model ILIKE ? OR call_logs.routed_model ILIKE ? OR call_logs.group_name ILIKE ?)", "%"+modelName+"%", "%"+modelName+"%", "%"+modelName+"%")
	}
	if endpoint := c.Query("endpoint"); endpoint != "" {
		base = base.Where("call_logs.inbound_endpoint = ?", endpoint)
	}
	if filterProvider != "" {
		base = base.Where("call_logs.provider = ?", filterProvider)
	}
	if parseBoolQuery(c.Query("seedance")) {
		base = service.ApplySeedanceLogFilter(base, "call_logs")
	}
	switch statusStr {
	case "success":
		base = base.Where("call_logs.status = 200")
	case "error":
		base = base.Where("call_logs.status <> 200")
	}

	// Count（在 JOIN 前统计，避免 call_traces 放大行数）
	var total int64
	if err := base.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// request_body/response_body 大字段不跨线传输；task_id/documents_count 在 SQL 侧从 call_traces 提取
	cols := `call_logs.request_id, call_logs.ts,
		call_logs.key_id, call_logs.key_name, call_logs.key_type, call_logs.user_id, call_logs.user_name,
		call_logs.dept_id, call_logs.dept_name, call_logs.studio_id, call_logs.studio_name,
		call_logs.model, call_logs.routed_model, call_logs.group_name, call_logs.provider,
		call_logs.prompt_tokens, call_logs.cached_tokens, call_logs.cache_write_tokens,
		call_logs.completion_tokens, call_logs.reasoning_tokens, call_logs.total_tokens,
		call_logs.status, call_logs.duration_ms, call_logs.first_token_ms,
		call_logs.est_cost_usd, call_logs.input_cost_usd, call_logs.output_cost_usd, call_logs.cache_read_cost_usd,
		call_logs.input_price_snapshot, call_logs.output_price_snapshot,
		call_logs.stage_auth_ms, call_logs.stage_route_ms, call_logs.stage_upstream_ms, call_logs.stage_ttfb_ms,
		call_logs.stream, call_logs.failover_used, call_logs.usage_missing, call_logs.reasoning_effort,
		call_logs.ip_address, call_logs.user_agent,
		COALESCE(NULLIF(substring(call_logs.inbound_endpoint FROM '/v1/contents/generations/tasks/([^/?#]+)'), ''), ct.response_body->>'id', ct.response_body->>'task_id') AS task_id,
		CASE WHEN jsonb_typeof(ct.request_body->'documents') = 'array'
			THEN jsonb_array_length(ct.request_body->'documents') ELSE NULL END AS documents_count`

	var rows []map[string]interface{}
	if err := base.
		Joins("LEFT JOIN call_traces ct ON ct.request_id = call_logs.request_id").
		Select(cols).
		Order("call_logs.ts DESC").
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

// ReportLogs GET /api/v1/usage/report/logs
// 离线用量报告的专用明细接口：keyset 分页（无深 OFFSET，大表稳定），通过稳定键过滤：
//   - personal    → user_id（精确）
//   - department  → dept_id（含双 ID 归一：dept_id 或 departments.id 或 user→dept 映射）
//   - studio      → studio_id
//
// 复用 UsagePolicy.QueryScope(actor) 做角色范围（admin 不过滤，dept/studio_admin 受限）。
// 返回 {rows, total, next_cursor, page, page_size}；next_cursor 供客户端 keyset 翻页。
// 对账基准保持 GET /api/v1/usage/dashboard 口径（total_tokens=SUM(prompt+completion+reasoning)）。
func (h *UsageHandler) ReportLogs(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "db not configured"})
		return
	}

	startStr := c.DefaultQuery("start", time.Now().AddDate(0, -1, 0).Format("2006-01-02"))
	endStr := c.DefaultQuery("end", time.Now().Format("2006-01-02"))
	filterUserID := c.Query("user_id")
	filterDeptID := c.Query("dept_id")
	filterStudioID := c.Query("studio_id")
	filterKeyType := c.Query("key_type")
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "200"))
	if pageSize < 1 || pageSize > 200 {
		pageSize = 200
	}
	cursor := c.Query("cursor")

	db := h.db.WithContext(c.Request.Context())

	base := db.Table("call_logs").
		Where("call_logs.ts >= ?::date AND call_logs.ts < (?::date + INTERVAL '1 day')", startStr, endStr).
		Scopes(policy.UsagePolicy{}.QueryScope(rbac.GetActor(c)))

	if filterUserID != "" {
		base = base.Where("call_logs.user_id = ?", filterUserID)
	}
	if filterDeptID != "" {
		// 部门双 ID 归一：直接 dept_id，或 departments.id（UUID），或 user→dept 映射。
		base = base.Where(`call_logs.dept_id = ?
			OR call_logs.dept_id IN (SELECT d.id::text FROM departments d WHERE d.dept_id = ? OR d.id::text = ?)
			OR call_logs.user_id IN (SELECT u.id::text FROM users u JOIN departments d ON d.id = u.dept_id WHERE d.dept_id = ? OR d.id::text = ?)`,
			filterDeptID, filterDeptID, filterDeptID, filterDeptID, filterDeptID)
	}
	if filterStudioID != "" {
		base = base.Where("call_logs.studio_id = ?", filterStudioID)
	}
	if filterKeyType != "" {
		base = base.Where("COALESCE(NULLIF(call_logs.key_type, ''), 'personal') = ?", filterKeyType)
	}

	// Count（JOIN 前统计，避免 call_traces 放大行数）
	var total int64
	if err := base.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	cols := `call_logs.request_id, call_logs.ts,
		call_logs.key_id, call_logs.key_name, call_logs.key_type, call_logs.user_id, call_logs.user_name,
		call_logs.dept_id, call_logs.dept_name, call_logs.studio_id, call_logs.studio_name,
		call_logs.model, call_logs.routed_model, call_logs.group_name, call_logs.provider,
		call_logs.prompt_tokens, call_logs.cached_tokens, call_logs.cache_write_tokens,
		call_logs.completion_tokens, call_logs.reasoning_tokens, call_logs.total_tokens,
		call_logs.status, call_logs.duration_ms, call_logs.first_token_ms,
		call_logs.est_cost_usd, call_logs.input_cost_usd, call_logs.output_cost_usd, call_logs.cache_read_cost_usd,
		call_logs.input_price_snapshot, call_logs.output_price_snapshot,
		call_logs.stage_auth_ms, call_logs.stage_route_ms, call_logs.stage_upstream_ms, call_logs.stage_ttfb_ms,
		call_logs.stream, call_logs.failover_used, call_logs.usage_missing, call_logs.reasoning_effort,
		call_logs.ip_address, call_logs.user_agent`

	q := base.Select(cols).Order("call_logs.ts DESC, call_logs.request_id DESC")

	var cursorTS time.Time
	useKeyset := false
	if cursor != "" {
		parts := strings.SplitN(cursor, "|", 2)
		if len(parts) != 2 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cursor format, want RFC3339Nano|request_id"})
			return
		}
		parsedTS, err := time.Parse(time.RFC3339Nano, parts[0])
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cursor timestamp"})
			return
		}
		cursorTS = parsedTS
		cursorRID := parts[1]
		useKeyset = true
		// 游标条件 3 个占位符：ts < ? OR (ts = ? AND request_id < ?)
		q = q.Where("(call_logs.ts < ?) OR (call_logs.ts = ? AND call_logs.request_id < ?)", cursorTS, cursorTS, cursorRID)
	}

	var rows []map[string]interface{}
	if useKeyset {
		if err := q.Limit(pageSize).Find(&rows).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	} else {
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		if page < 1 {
			page = 1
		}
		if err := q.Limit(pageSize).Offset((page - 1) * pageSize).Find(&rows).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	nextCursor := ""
	if len(rows) > 0 {
		last := rows[len(rows)-1]
		rid, _ := last["request_id"].(string)
		tsStr := ""
		switch t := last["ts"].(type) {
		case string:
			tsStr = t
		case time.Time:
			tsStr = t.Format(time.RFC3339Nano)
		}
		if rid != "" && tsStr != "" {
			nextCursor = tsStr + "|" + rid
		}
	}

	resp := gin.H{"rows": rows, "total": total, "page_size": pageSize}
	// 只要返回了 rows 就给出 next_cursor（含首屏），便于客户端探测并持续 keyset 翻页。
	if nextCursor != "" {
		resp["next_cursor"] = nextCursor
	}
	if !useKeyset {
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		resp["page"] = page
	}
	c.JSON(http.StatusOK, resp)
}

// BaselineDaily 基线日聚合：某天非 service 调用数（供 28 天稳健基线物化）。
type BaselineDaily struct {
	Date     string `json:"date"` // YYYY-MM-DD
	ReqCount int64  `json:"req_count"`
}

// BaselineBucket 基线分钟桶：某分钟的非 service 调用数与成本（供 A1/A2 历史窗口分位重建）。
type BaselineBucket struct {
	Minute string  `json:"minute"` // RFC3339 minute
	Count  int64   `json:"count"`
	Cost   float64 `json:"cost"`
}

// ReportBaseline GET /api/v1/usage/report/baseline
// 只读：期前 28 天基线的「日聚合 + 分钟桶聚合」，从 call_logs 按 level 稳定键 + 排除 service 分桶。
// 供报告工具用日聚合序列物化 28 天稳健基线、用分钟桶重建 A1/A2 历史窗口分位，而无需拉全量明细行。
func (h *UsageHandler) ReportBaseline(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "db not configured"})
		return
	}
	startStr := c.DefaultQuery("start", time.Now().AddDate(0, -1, 0).Format("2006-01-02"))
	endStr := c.DefaultQuery("end", time.Now().Format("2006-01-02"))
	filterUserID := c.Query("user_id")
	filterDeptID := c.Query("dept_id")
	filterStudioID := c.Query("studio_id")

	db := h.db.WithContext(c.Request.Context())

	// 统一用 cl 别名，便于复用排除 service 的常量子串。
	base := db.Table("call_logs cl").
		Where("cl.ts >= ?::date AND cl.ts < (?::date + INTERVAL '1 day')", startStr, endStr).
		Where(obsmodel.WhereExcludeServiceKeyLogsCL).
		Scopes(policy.UsagePolicy{}.QueryScope(rbac.GetActor(c)))

	if filterUserID != "" {
		base = base.Where("cl.user_id = ?", filterUserID)
	}
	if filterDeptID != "" {
		// 部门双 ID 归一：直接 dept_id，或 departments.id（UUID），或 user→dept 映射。
		base = base.Where(`cl.dept_id = ?
			OR cl.dept_id IN (SELECT d.id::text FROM departments d WHERE d.dept_id = ? OR d.id::text = ?)
			OR cl.user_id IN (SELECT u.id::text FROM users u JOIN departments d ON d.id = u.dept_id WHERE d.dept_id = ? OR d.id::text = ?)`,
			filterDeptID, filterDeptID, filterDeptID, filterDeptID, filterDeptID)
	}
	if filterStudioID != "" {
		base = base.Where("cl.studio_id = ?", filterStudioID)
	}

	// 日聚合：供 28 天稳健基线（非 service 调用数按天求和）。
	var days []BaselineDaily
	if err := base.Select(`cl.ts::date::text AS date, COUNT(*) AS req_count`).
		Group("cl.ts::date").
		Order("cl.ts::date ASC").
		Scan(&days).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 分钟桶聚合：供 A1/A2 历史窗口分位重建（调用数 + 成本，按分钟分桶）。
	var buckets []BaselineBucket
	if err := base.Select(`TO_CHAR(date_trunc('minute', cl.ts), 'YYYY-MM-DD HH24:MI:SS') AS minute,
		COUNT(*) AS count, COALESCE(SUM(cl.est_cost_usd), 0) AS cost`).
		Group("date_trunc('minute', cl.ts)").
		Order("date_trunc('minute', cl.ts) ASC").
		Scan(&buckets).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"daily": days, "minute_buckets": buckets})
}

type UserRankRow struct {
	UserID           string  `json:"user_id"           gorm:"column:user_id"`
	UserName         string  `json:"user_name"         gorm:"column:user_name"`
	DeptName         string  `json:"dept_name"         gorm:"column:dept_name"`
	TotalTokens      int64   `json:"total_tokens"      gorm:"column:total_tokens"`
	PromptTokens     int64   `json:"prompt_tokens"     gorm:"column:prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens" gorm:"column:completion_tokens"`
	EstCostUSD       float64 `json:"est_cost_usd"      gorm:"column:est_cost_usd"`
	CallCount        int64   `json:"call_count"        gorm:"column:call_count"`
	ErrorCount       int64   `json:"error_count"       gorm:"column:error_count"`
	ModelCount       int     `json:"model_count"       gorm:"column:model_count"`
}

// UserRankSummary 用户排行汇总
type UserRankSummary struct {
	TotalUsers   int     `json:"total_users"`
	TotalTokens  int64   `json:"total_tokens"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	TotalCalls   int64   `json:"total_calls"`
}

// UserRankResponse 用户排行响应
type UserRankResponse struct {
	Summary    UserRankSummary `json:"summary"`
	Rows       []UserRankRow   `json:"rows"`
	Page       int             `json:"page"`
	PageSize   int             `json:"page_size"`
	TotalCount int64           `json:"total_count"`
}

// UserTopModelRow 用户 Top 模型行
type UserTopModelRow struct {
	Model       string  `json:"model"`
	EstCostUSD  float64 `json:"est_cost_usd"`
	CallCount   int64   `json:"call_count"`
	TotalTokens int64   `json:"total_tokens"`
}

// parseDateRangeFromTo 解析 from/to 参数（支持 RFC3339 和 YYYY-MM-DD）
func parseDateRangeFromTo(c *gin.Context) (from, to time.Time, ok bool) {
	fromStr := c.Query("from")
	toStr := c.Query("to")
	if fromStr == "" || toStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "from and to are required"})
		return
	}
	var err error
	from, err = time.Parse(time.RFC3339, fromStr)
	if err != nil {
		from, err = time.Parse("2006-01-02", fromStr)
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid from date"})
		return
	}
	to, err = time.Parse(time.RFC3339, toStr)
	if err != nil {
		to, err = time.Parse("2006-01-02", toStr)
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid to date"})
		return
	}
	if from.After(to) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "from must not be after to"})
		return
	}
	if to.Sub(from) > 90*24*time.Hour {
		c.JSON(http.StatusBadRequest, gin.H{"error": "date range must not exceed 90 days"})
		return
	}
	ok = true
	return
}

// DashboardUsers GET /api/v1/usage/dashboard/users
// 全局按用户聚合，返回用户排行（实时从 call_logs 聚合）
func (h *UsageHandler) DashboardUsers(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "db not configured"})
		return
	}

	from, to, ok := parseDateRangeFromTo(c)
	if !ok {
		return
	}
	toExcl := to.Add(24 * time.Hour)

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	studioID := c.Query("studio_id")
	deptID := c.Query("dept_id")
	db := h.db.WithContext(c.Request.Context())

	// 全局汇总（不分页）
	var sumRow struct {
		TotalUsers  int64   `gorm:"column:total_users"`
		TotalTokens int64   `gorm:"column:total_tokens"`
		TotalCost   float64 `gorm:"column:total_cost"`
		TotalCalls  int64   `gorm:"column:total_calls"`
	}
	_ = db.Raw(`
		SELECT
			COUNT(DISTINCT COALESCE(user_id, 'unknown')) AS total_users,
			COALESCE(SUM(total_tokens), 0)              AS total_tokens,
			COALESCE(SUM(est_cost_usd), 0)              AS total_cost,
			COUNT(*)                                     AS total_calls
		FROM call_logs
		WHERE ts >= ? AND ts < ?
			AND (`+obsmodel.WhereExcludeServiceKeyLogs+`)
			AND (? = '' OR studio_id = ?)
			AND (? = '' OR dept_id = ?)
	`, from, toExcl, studioID, studioID, deptID, deptID).Scan(&sumRow)

	// 分页前总行数
	var totalCount int64
	_ = db.Raw(`
		SELECT COUNT(*) FROM (
			SELECT COALESCE(user_id, 'unknown')
			FROM call_logs
			WHERE ts >= ? AND ts < ?
				AND (`+obsmodel.WhereExcludeServiceKeyLogs+`)
				AND (? = '' OR studio_id = ?)
				AND (? = '' OR dept_id = ?)
			GROUP BY COALESCE(user_id, 'unknown'), COALESCE(user_name, ''), COALESCE(dept_name, '')
		) AS sub
	`, from, toExcl, studioID, studioID, deptID, deptID).Scan(&totalCount)

	// 分页数据
	offset := (page - 1) * pageSize
	var rows []UserRankRow
	err := db.Raw(`
		SELECT
			COALESCE(user_id, 'unknown')    AS user_id,
			COALESCE(user_name, '')          AS user_name,
			COALESCE(dept_name, '')          AS dept_name,
			COALESCE(SUM(total_tokens), 0)   AS total_tokens,
			COALESCE(SUM(prompt_tokens), 0)  AS prompt_tokens,
			COALESCE(SUM(completion_tokens), 0) AS completion_tokens,
			COALESCE(SUM(est_cost_usd), 0)   AS est_cost_usd,
			COUNT(*)                          AS call_count,
			COUNT(*) FILTER (WHERE status != 200) AS error_count,
			COUNT(DISTINCT model)             AS model_count
		FROM call_logs
		WHERE ts >= ? AND ts < ?
			AND (`+obsmodel.WhereExcludeServiceKeyLogs+`)
			AND (? = '' OR studio_id = ?)
			AND (? = '' OR dept_id = ?)
		GROUP BY COALESCE(user_id, 'unknown'), COALESCE(user_name, ''), COALESCE(dept_name, '')
		ORDER BY SUM(est_cost_usd) DESC
		LIMIT ? OFFSET ?
	`, from, toExcl, studioID, studioID, deptID, deptID, pageSize, offset).Scan(&rows).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if rows == nil {
		rows = []UserRankRow{}
	}

	c.JSON(http.StatusOK, UserRankResponse{
		Summary: UserRankSummary{
			TotalUsers:   int(sumRow.TotalUsers),
			TotalTokens:  sumRow.TotalTokens,
			TotalCostUSD: sumRow.TotalCost,
			TotalCalls:   sumRow.TotalCalls,
		},
		Rows:       rows,
		Page:       page,
		PageSize:   pageSize,
		TotalCount: totalCount,
	})
}

// DashboardUserTopModels GET /api/v1/usage/dashboard/user-top-models
// 批量返回每个用户的 Top N 常用模型（按费用降序）
func (h *UsageHandler) DashboardUserTopModels(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "db not configured"})
		return
	}

	from, to, ok := parseDateRangeFromTo(c)
	if !ok {
		return
	}
	toExcl := to.Add(24 * time.Hour)

	topN, _ := strconv.Atoi(c.DefaultQuery("top_n", "3"))
	if topN < 1 || topN > 10 {
		topN = 3
	}

	studioID := c.Query("studio_id")
	deptID := c.Query("dept_id")

	type topModelRow struct {
		UserID      string  `gorm:"column:user_id"`
		Model       string  `gorm:"column:model"`
		EstCostUSD  float64 `gorm:"column:est_cost_usd"`
		CallCount   int64   `gorm:"column:call_count"`
		TotalTokens int64   `gorm:"column:total_tokens"`
	}

	var rows []topModelRow
	err := h.db.WithContext(c.Request.Context()).Raw(`
		WITH ranked AS (
			SELECT
				COALESCE(user_id, 'unknown') AS user_id,
				model,
				COALESCE(SUM(est_cost_usd), 0)   AS est_cost_usd,
				COUNT(*)                           AS call_count,
				COALESCE(SUM(total_tokens), 0)    AS total_tokens,
				ROW_NUMBER() OVER (
					PARTITION BY COALESCE(user_id, 'unknown')
					ORDER BY SUM(est_cost_usd) DESC
				) AS rn
			FROM call_logs
			WHERE ts >= ? AND ts < ?
				AND (`+obsmodel.WhereExcludeServiceKeyLogs+`)
				AND (? = '' OR studio_id = ?)
				AND (? = '' OR dept_id = ?)
			GROUP BY COALESCE(user_id, 'unknown'), model
		)
		SELECT user_id, model, est_cost_usd, call_count, total_tokens
		FROM ranked
		WHERE rn <= ?
		ORDER BY user_id, rn
	`, from, toExcl, studioID, studioID, deptID, deptID, topN).Scan(&rows).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	result := make(map[string][]UserTopModelRow)
	for _, r := range rows {
		result[r.UserID] = append(result[r.UserID], UserTopModelRow{
			Model:       r.Model,
			EstCostUSD:  r.EstCostUSD,
			CallCount:   r.CallCount,
			TotalTokens: r.TotalTokens,
		})
	}

	c.JSON(http.StatusOK, gin.H{"models": result})
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
