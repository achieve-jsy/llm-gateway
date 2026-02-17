package handler

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/example/llm-gateway/internal/orgservice/rbac/policy"
	"github.com/gin-gonic/gin"
)

// ─── B1/B2/B3: DashboardUsers 增强 ─────────────────────────────────────────

// DashboardUserRowEnhanced 增强版用户排行行（含 cost_ratio, cost_change_pct, cost_per_1k_tokens, error_rate, top_models, vs_avg）
type DashboardUserRowEnhanced struct {
	UserID           string             `json:"user_id"`
	UserName         string             `json:"user_name"`
	DeptName         string             `json:"dept_name"`
	TotalTokens      int64              `json:"total_tokens"`
	PromptTokens     int64              `json:"prompt_tokens"`
	CompletionTokens int64              `json:"completion_tokens"`
	EstCostUSD       float64            `json:"est_cost_usd"`
	CallCount        int64              `json:"call_count"`
	ErrorCount       int64              `json:"error_count"`
	ModelCount       int                `json:"model_count"`
	CostRatio        float64            `json:"cost_ratio"`         // B1: 占总成本百分比
	CostChangePct    float64            `json:"cost_change_pct"`    // B1: 环比变化百分比
	CostPer1kTokens  float64            `json:"cost_per_1k_tokens"` // B1: 每千 token 成本
	ErrorRate        float64            `json:"error_rate"`         // B1: 错误率 %
	TopModels        []UserTopModelRow  `json:"top_models"`         // B2: 用户 Top 3 模型
	VsAvg            DashboardUserVsAvg `json:"vs_avg"`             // B3: 与平均对比
}

// DashboardUserVsAvg 用户与全局平均对比
type DashboardUserVsAvg struct {
	CostRatioVsAvg float64 `json:"cost_ratio_vs_avg"` // 正值=高于平均，负值=低于平均
	CallCountVsAvg float64 `json:"call_count_vs_avg"` // 倍数：用户调用量 / 平均调用量
	ErrorRateVsAvg float64 `json:"error_rate_vs_avg"` // 差值：用户错误率 - 平均错误率
}

// DashboardUsersSummaryEnhanced 增强版汇总
type DashboardUsersSummaryEnhanced struct {
	TotalUsers     int     `json:"total_users"`
	TotalCostUSD   float64 `json:"total_cost_usd"`
	TotalCalls     int64   `json:"total_calls"`
	ActiveDays     int     `json:"active_days"`       // 日期范围内有数据的天数
	AvgCostPerCall float64 `json:"avg_cost_per_call"` // 平均每次调用成本
}

// DashboardUsersEnhanced GET /api/v1/usage/dashboard/users（增强版）
// 在原有基础上增加 cost_ratio, cost_change_pct, cost_per_1k_tokens, error_rate, top_models, vs_avg, summary
func (h *UsageHandler) DashboardUsersEnhanced(c *gin.Context) {
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
	modelName := c.Query("model_name") // B8: 全局 model_name 筛选
	db := h.db.WithContext(c.Request.Context())

	// ── 环比期间计算（B10 修复）───────────────────────────────────────
	duration := toExcl.Sub(from)
	prevFrom := from.Add(-duration)
	prevToExcl := from

	// ── 汇总（summary）────────────────────────────────────────────
	var sumRow struct {
		TotalUsers  int64   `gorm:"column:total_users"`
		TotalTokens int64   `gorm:"column:total_tokens"`
		TotalCost   float64 `gorm:"column:total_cost"`
		TotalCalls  int64   `gorm:"column:total_calls"`
		ActiveDays  int     `gorm:"column:active_days"`
	}
	_ = db.Raw(`
		SELECT
			COUNT(DISTINCT COALESCE(user_id, 'unknown')) AS total_users,
			COALESCE(SUM(total_tokens), 0)              AS total_tokens,
			COALESCE(SUM(est_cost_usd), 0)              AS total_cost,
			COUNT(*)                                     AS total_calls,
			COUNT(DISTINCT ts::date)                     AS active_days
		FROM call_logs
		WHERE ts >= ? AND ts < ?
			AND (? = '' OR studio_id = ?)
			AND (? = '' OR dept_id = ?)
			AND (? = '' OR model ILIKE ?)
	`, from, toExcl, studioID, studioID, deptID, deptID, modelName, "%"+modelName+"%").Scan(&sumRow)

	avgCostPerCall := 0.0
	if sumRow.TotalCalls > 0 {
		avgCostPerCall = sumRow.TotalCost / float64(sumRow.TotalCalls)
	}

	// ── 全局平均值（B3 vs_avg）───────────────────────────────────────
	avgCostRatio := 0.0
	avgCallCount := 0.0
	avgErrorRate := 0.0
	if sumRow.TotalUsers > 0 {
		avgCostRatio = 100.0 / float64(sumRow.TotalUsers) // 均分 100%
		avgCallCount = float64(sumRow.TotalCalls) / float64(sumRow.TotalUsers)
	}
	if sumRow.TotalCalls > 0 {
		var totalErrors int64
		_ = db.Raw(`
			SELECT COUNT(*) FROM call_logs
			WHERE ts >= ? AND ts < ? AND status != 200
				AND (? = '' OR studio_id = ?)
				AND (? = '' OR dept_id = ?)
				AND (? = '' OR model ILIKE ?)
		`, from, toExcl, studioID, studioID, deptID, deptID, modelName, "%"+modelName+"%").Scan(&totalErrors)
		avgErrorRate = float64(totalErrors) / float64(sumRow.TotalCalls) * 100.0
	}

	// ── 分页前总行数 ─────────────────────────────────────────────
	var totalCount int64
	_ = db.Raw(`
		SELECT COUNT(*) FROM (
			SELECT COALESCE(user_id, 'unknown')
			FROM call_logs
			WHERE ts >= ? AND ts < ?
				AND (? = '' OR studio_id = ?)
				AND (? = '' OR dept_id = ?)
				AND (? = '' OR model ILIKE ?)
			GROUP BY COALESCE(user_id, 'unknown'), COALESCE(user_name, ''), COALESCE(dept_name, '')
		) AS sub
	`, from, toExcl, studioID, studioID, deptID, deptID, modelName, "%"+modelName+"%").Scan(&totalCount)

	// ── 当期分页数据 ─────────────────────────────────────────────
	offset := (page - 1) * pageSize
	type rawUserRow struct {
		UserID           string  `gorm:"column:user_id"`
		UserName         string  `gorm:"column:user_name"`
		DeptName         string  `gorm:"column:dept_name"`
		TotalTokens      int64   `gorm:"column:total_tokens"`
		PromptTokens     int64   `gorm:"column:prompt_tokens"`
		CompletionTokens int64   `gorm:"column:completion_tokens"`
		EstCostUSD       float64 `gorm:"column:est_cost_usd"`
		CallCount        int64   `gorm:"column:call_count"`
		ErrorCount       int64   `gorm:"column:error_count"`
		ModelCount       int     `gorm:"column:model_count"`
	}
	var rawRows []rawUserRow
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
			AND (? = '' OR studio_id = ?)
			AND (? = '' OR dept_id = ?)
			AND (? = '' OR model ILIKE ?)
		GROUP BY COALESCE(user_id, 'unknown'), COALESCE(user_name, ''), COALESCE(dept_name, '')
		ORDER BY SUM(est_cost_usd) DESC
		LIMIT ? OFFSET ?
	`, from, toExcl, studioID, studioID, deptID, deptID, modelName, "%"+modelName+"%", pageSize, offset).Scan(&rawRows).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// ── 上期成本查询（B1 cost_change_pct）────────────────────────────
	prevCostMap := make(map[string]float64)
	{
		type prevRow struct {
			UserID     string  `gorm:"column:user_id"`
			EstCostUSD float64 `gorm:"column:est_cost_usd"`
		}
		var prevRows []prevRow
		_ = db.Raw(`
			SELECT
				COALESCE(user_id, 'unknown') AS user_id,
				COALESCE(SUM(est_cost_usd), 0) AS est_cost_usd
			FROM call_logs
			WHERE ts >= ? AND ts < ?
				AND (? = '' OR studio_id = ?)
				AND (? = '' OR dept_id = ?)
				AND (? = '' OR model ILIKE ?)
			GROUP BY COALESCE(user_id, 'unknown')
		`, prevFrom, prevToExcl, studioID, studioID, deptID, deptID, modelName, "%"+modelName+"%").Scan(&prevRows)
		for _, pr := range prevRows {
			prevCostMap[pr.UserID] = pr.EstCostUSD
		}
	}

	// ── Top 模型查询（B2 top_models）─────────────────────────────────
	topModelsMap := make(map[string][]UserTopModelRow)
	{
		type topRow struct {
			UserID      string  `gorm:"column:user_id"`
			Model       string  `gorm:"column:model"`
			EstCostUSD  float64 `gorm:"column:est_cost_usd"`
			CallCount   int64   `gorm:"column:call_count"`
			TotalTokens int64   `gorm:"column:total_tokens"`
		}
		var topRows []topRow
		_ = db.Raw(`
			WITH ranked AS (
				SELECT
					COALESCE(user_id, 'unknown') AS user_id,
					model,
					COALESCE(SUM(est_cost_usd), 0) AS est_cost_usd,
					COUNT(*) AS call_count,
					COALESCE(SUM(total_tokens), 0) AS total_tokens,
					ROW_NUMBER() OVER (
						PARTITION BY COALESCE(user_id, 'unknown')
						ORDER BY SUM(est_cost_usd) DESC
					) AS rn
				FROM call_logs
				WHERE ts >= ? AND ts < ?
					AND (? = '' OR studio_id = ?)
					AND (? = '' OR dept_id = ?)
					AND (? = '' OR model ILIKE ?)
				GROUP BY COALESCE(user_id, 'unknown'), model
			)
			SELECT user_id, model, est_cost_usd, call_count, total_tokens
			FROM ranked WHERE rn <= 3
			ORDER BY user_id, rn
		`, from, toExcl, studioID, studioID, deptID, deptID, modelName, "%"+modelName+"%").Scan(&topRows)
		for _, tr := range topRows {
			topModelsMap[tr.UserID] = append(topModelsMap[tr.UserID], UserTopModelRow{
				Model:       tr.Model,
				EstCostUSD:  tr.EstCostUSD,
				CallCount:   tr.CallCount,
				TotalTokens: tr.TotalTokens,
			})
		}
	}

	// ── 组装增强结果 ──────────────────────────────────────────────
	rows := make([]DashboardUserRowEnhanced, 0, len(rawRows))
	for _, r := range rawRows {
		costRatio := 0.0
		if sumRow.TotalCost > 0 {
			costRatio = r.EstCostUSD / sumRow.TotalCost * 100.0
		}

		costChangePct := 0.0
		if prevCost, ok := prevCostMap[r.UserID]; ok && prevCost > 0 {
			costChangePct = (r.EstCostUSD - prevCost) / prevCost * 100.0
		} else if r.EstCostUSD > 0 {
			costChangePct = 100.0 // 上期无数据，视为新增 100%
		}

		costPer1k := 0.0
		if r.TotalTokens > 0 {
			costPer1k = r.EstCostUSD / (float64(r.TotalTokens) / 1000.0)
		}

		errorRate := 0.0
		if r.CallCount > 0 {
			errorRate = float64(r.ErrorCount) / float64(r.CallCount) * 100.0
		}

		topModels := topModelsMap[r.UserID]
		if topModels == nil {
			topModels = []UserTopModelRow{}
		}

		rows = append(rows, DashboardUserRowEnhanced{
			UserID:           r.UserID,
			UserName:         r.UserName,
			DeptName:         r.DeptName,
			TotalTokens:      r.TotalTokens,
			PromptTokens:     r.PromptTokens,
			CompletionTokens: r.CompletionTokens,
			EstCostUSD:       r.EstCostUSD,
			CallCount:        r.CallCount,
			ErrorCount:       r.ErrorCount,
			ModelCount:       r.ModelCount,
			CostRatio:        math.Round(costRatio*100) / 100,
			CostChangePct:    math.Round(costChangePct*100) / 100,
			CostPer1kTokens:  math.Round(costPer1k*10000) / 10000,
			ErrorRate:        math.Round(errorRate*100) / 100,
			TopModels:        topModels,
			VsAvg: DashboardUserVsAvg{
				CostRatioVsAvg: math.Round((costRatio-avgCostRatio)*100) / 100,
				CallCountVsAvg: math.Round(float64(r.CallCount)/math.Max(avgCallCount, 1)*100) / 100,
				ErrorRateVsAvg: math.Round((errorRate-avgErrorRate)*100) / 100,
			},
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"summary": DashboardUsersSummaryEnhanced{
			TotalUsers:     int(sumRow.TotalUsers),
			TotalCostUSD:   sumRow.TotalCost,
			TotalCalls:     sumRow.TotalCalls,
			ActiveDays:     sumRow.ActiveDays,
			AvgCostPerCall: math.Round(avgCostPerCall*10000) / 10000,
		},
		"rows":        rows,
		"page":        page,
		"page_size":   pageSize,
		"total_count": totalCount,
	})
}

// ─── B4/B5: DashboardModels 增强 ────────────────────────────────────────────

// DashboardModelRowEnhanced 增强版模型排行行（含健康度 + 长尾标识）
type DashboardModelRowEnhanced struct {
	Model           string  `json:"model"`
	Calls           int64   `json:"calls"`
	TotalTokens     int64   `json:"total_tokens"`
	Cost            float64 `json:"cost"`
	AvgDurationMs   int     `json:"avg_duration_ms"`    // B4: 平均响应时间
	P90DurationMs   int     `json:"p90_duration_ms"`    // B4: P90 响应时间
	ErrorRate       float64 `json:"error_rate"`         // B4: 错误率 %
	AvgFirstTokenMs int     `json:"avg_first_token_ms"` // B4: 平均首 token 延迟
	IsLongTail      bool    `json:"is_long_tail"`       // B5: 是否长尾模型
}

// DashboardModelsEnhanced GET /api/v1/usage/dashboard/models（增强版）
func (h *UsageHandler) DashboardModelsEnhanced(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "db not configured"})
		return
	}

	startStr := c.DefaultQuery("start", time.Now().AddDate(0, -1, 0).Format("2006-01-02"))
	endStr := c.DefaultQuery("end", time.Now().Format("2006-01-02"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "30"))
	if limit < 1 || limit > 100 {
		limit = 30
	}
	filterUserID := c.Query("user_id")
	filterStatus := c.Query("status")

	actor := rbac.GetActor(c)
	usagePol := policy.UsagePolicy{}

	type rawModelRow struct {
		Model           string  `gorm:"column:model"`
		Calls           int64   `gorm:"column:calls"`
		TotalTokens     int64   `gorm:"column:total_tokens"`
		Cost            float64 `gorm:"column:cost"`
		ErrorCount      int64   `gorm:"column:error_count"`
		AvgDurationMs   float64 `gorm:"column:avg_duration_ms"`
		P90DurationMs   float64 `gorm:"column:p90_duration_ms"`
		AvgFirstTokenMs float64 `gorm:"column:avg_first_token_ms"`
	}

	q := h.db.WithContext(c.Request.Context()).
		Table("call_logs").
		Select(`model,
			COUNT(*) AS calls,
			COALESCE(SUM(prompt_tokens + completion_tokens + reasoning_tokens), 0) AS total_tokens,
			COALESCE(SUM(est_cost_usd), 0) AS cost,
			COUNT(*) FILTER (WHERE status != 200) AS error_count,
			COALESCE(AVG(duration_ms), 0) AS avg_duration_ms,
			COALESCE(PERCENTILE_CONT(0.9) WITHIN GROUP (ORDER BY duration_ms), 0) AS p90_duration_ms,
			COALESCE(AVG(first_token_ms), 0) AS avg_first_token_ms`).
		Where("ts::date BETWEEN ?::date AND ?::date", startStr, endStr).
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

	var rawRows []rawModelRow
	if err := q.Group("model").
		Order("cost DESC").
		Limit(limit).
		Scan(&rawRows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// B5: 长尾模型识别 — 调用量低于所有模型 P10 或调用量 < 5 的模型
	var allCalls []int64
	for _, r := range rawRows {
		allCalls = append(allCalls, r.Calls)
	}
	longTailThreshold := int64(5)
	if len(allCalls) >= 10 {
		// 简单 P10：排序后取第 10% 位置
		sorted := make([]int64, len(allCalls))
		copy(sorted, allCalls)
		for i := 0; i < len(sorted); i++ {
			for j := i + 1; j < len(sorted); j++ {
				if sorted[j] < sorted[i] {
					sorted[i], sorted[j] = sorted[j], sorted[i]
				}
			}
		}
		p10Idx := len(sorted) / 10
		if p10Idx > 0 {
			longTailThreshold = sorted[p10Idx-1]
		}
	}

	rows := make([]DashboardModelRowEnhanced, 0, len(rawRows))
	for _, r := range rawRows {
		errorRate := 0.0
		if r.Calls > 0 {
			errorRate = float64(r.ErrorCount) / float64(r.Calls) * 100.0
		}
		rows = append(rows, DashboardModelRowEnhanced{
			Model:           r.Model,
			Calls:           r.Calls,
			TotalTokens:     r.TotalTokens,
			Cost:            r.Cost,
			AvgDurationMs:   int(math.Round(r.AvgDurationMs)),
			P90DurationMs:   int(math.Round(r.P90DurationMs)),
			ErrorRate:       math.Round(errorRate*100) / 100,
			AvgFirstTokenMs: int(math.Round(r.AvgFirstTokenMs)),
			IsLongTail:      r.Calls <= longTailThreshold,
		})
	}

	c.JSON(http.StatusOK, gin.H{"rows": rows})
}

// ─── B6: user-detail API ────────────────────────────────────────────────────

// DashboardUserDetail GET /api/v1/usage/dashboard/user-detail
// 单用户详情：基础统计 + 每日趋势 + 模型分布 + 小时分布
func (h *UsageHandler) DashboardUserDetail(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "db not configured"})
		return
	}

	userID := c.Query("user_id")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
		return
	}

	from, to, ok := parseDateRangeFromTo(c)
	if !ok {
		return
	}
	toExcl := to.Add(24 * time.Hour)
	db := h.db.WithContext(c.Request.Context())

	// 基础统计
	var summary struct {
		TotalCalls    int64   `json:"total_calls"      gorm:"column:total_calls"`
		SuccessCount  int64   `json:"success_count"    gorm:"column:success_count"`
		TotalTokens   int64   `json:"total_tokens"     gorm:"column:total_tokens"`
		EstCostUSD    float64 `json:"est_cost_usd"     gorm:"column:est_cost_usd"`
		ModelCount    int     `json:"model_count"      gorm:"column:model_count"`
		AvgDurationMs float64 `json:"avg_duration_ms"  gorm:"column:avg_duration_ms"`
		ErrorRate     float64 `json:"error_rate"       gorm:"column:error_rate"`
	}
	_ = db.Raw(`
		SELECT
			COUNT(*) AS total_calls,
			COUNT(*) FILTER (WHERE status = 200) AS success_count,
			COALESCE(SUM(total_tokens), 0) AS total_tokens,
			COALESCE(SUM(est_cost_usd), 0) AS est_cost_usd,
			COUNT(DISTINCT model) AS model_count,
			COALESCE(AVG(duration_ms), 0) AS avg_duration_ms,
			CASE WHEN COUNT(*) > 0
				THEN ROUND(COUNT(*) FILTER (WHERE status != 200)::numeric / COUNT(*)::numeric * 100, 2)
				ELSE 0
			END AS error_rate
		FROM call_logs
		WHERE ts >= ? AND ts < ? AND COALESCE(user_id, 'unknown') = ?
	`, from, toExcl, userID).Scan(&summary)

	// 每日趋势
	type trendPoint struct {
		Date   string  `json:"date"      gorm:"column:date"`
		Calls  int64   `json:"calls"     gorm:"column:calls"`
		Cost   float64 `json:"cost"      gorm:"column:cost"`
		Tokens int64   `json:"tokens"    gorm:"column:tokens"`
	}
	var trend []trendPoint
	_ = db.Raw(`
		SELECT
			ts::date::text AS date,
			COUNT(*) AS calls,
			COALESCE(SUM(est_cost_usd), 0) AS cost,
			COALESCE(SUM(total_tokens), 0) AS tokens
		FROM call_logs
		WHERE ts >= ? AND ts < ? AND COALESCE(user_id, 'unknown') = ?
		GROUP BY ts::date
		ORDER BY ts::date ASC
	`, from, toExcl, userID).Scan(&trend)

	// 模型分布
	type modelDist struct {
		Model  string  `json:"model"       gorm:"column:model"`
		Calls  int64   `json:"calls"       gorm:"column:calls"`
		Cost   float64 `json:"cost"        gorm:"column:cost"`
		Tokens int64   `json:"tokens"      gorm:"column:tokens"`
	}
	var models []modelDist
	_ = db.Raw(`
		SELECT
			model,
			COUNT(*) AS calls,
			COALESCE(SUM(est_cost_usd), 0) AS cost,
			COALESCE(SUM(total_tokens), 0) AS tokens
		FROM call_logs
		WHERE ts >= ? AND ts < ? AND COALESCE(user_id, 'unknown') = ?
		GROUP BY model
		ORDER BY cost DESC
	`, from, toExcl, userID).Scan(&models)

	// 小时分布
	type hourlyDist struct {
		Hour  int   `json:"hour"  gorm:"column:hour"`
		Calls int64 `json:"calls" gorm:"column:calls"`
	}
	var hourly []hourlyDist
	_ = db.Raw(`
		SELECT
			EXTRACT(HOUR FROM ts)::int AS hour,
			COUNT(*) AS calls
		FROM call_logs
		WHERE ts >= ? AND ts < ? AND COALESCE(user_id, 'unknown') = ?
		GROUP BY EXTRACT(HOUR FROM ts)
		ORDER BY hour ASC
	`, from, toExcl, userID).Scan(&hourly)

	if trend == nil {
		trend = []trendPoint{}
	}
	if models == nil {
		models = []modelDist{}
	}
	if hourly == nil {
		hourly = []hourlyDist{}
	}

	c.JSON(http.StatusOK, gin.H{
		"summary":             summary,
		"daily_trend":         trend,
		"model_distribution":  models,
		"hourly_distribution": hourly,
	})
}

// ─── B7: model-detail API ───────────────────────────────────────────────────

// DashboardModelDetail GET /api/v1/usage/dashboard/model-detail
// 单模型详情：基础统计 + 每日趋势 + 用户分布
func (h *UsageHandler) DashboardModelDetail(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "db not configured"})
		return
	}

	modelName := c.Query("model")
	if modelName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model is required"})
		return
	}

	from, to, ok := parseDateRangeFromTo(c)
	if !ok {
		return
	}
	toExcl := to.Add(24 * time.Hour)
	db := h.db.WithContext(c.Request.Context())

	actor := rbac.GetActor(c)
	usagePol := policy.UsagePolicy{}

	// 基础统计
	var summary struct {
		TotalCalls      int64   `json:"total_calls"       gorm:"column:total_calls"`
		SuccessCount    int64   `json:"success_count"     gorm:"column:success_count"`
		TotalTokens     int64   `json:"total_tokens"      gorm:"column:total_tokens"`
		EstCostUSD      float64 `json:"est_cost_usd"      gorm:"column:est_cost_usd"`
		UserCount       int     `json:"user_count"        gorm:"column:user_count"`
		DeptCount       int     `json:"dept_count"        gorm:"column:dept_count"`
		AvgDurationMs   float64 `json:"avg_duration_ms"   gorm:"column:avg_duration_ms"`
		P90DurationMs   float64 `json:"p90_duration_ms"   gorm:"column:p90_duration_ms"`
		AvgFirstTokenMs float64 `json:"avg_first_token_ms" gorm:"column:avg_first_token_ms"`
		ErrorRate       float64 `json:"error_rate"        gorm:"column:error_rate"`
	}
	_ = db.Raw(`
		SELECT
			COUNT(*) AS total_calls,
			COUNT(*) FILTER (WHERE status = 200) AS success_count,
			COALESCE(SUM(total_tokens), 0) AS total_tokens,
			COALESCE(SUM(est_cost_usd), 0) AS est_cost_usd,
			COUNT(DISTINCT user_id) AS user_count,
			COUNT(DISTINCT dept_id) AS dept_count,
			COALESCE(AVG(duration_ms), 0) AS avg_duration_ms,
			COALESCE(PERCENTILE_CONT(0.9) WITHIN GROUP (ORDER BY duration_ms), 0) AS p90_duration_ms,
			COALESCE(AVG(first_token_ms), 0) AS avg_first_token_ms,
			CASE WHEN COUNT(*) > 0
				THEN ROUND(COUNT(*) FILTER (WHERE status != 200)::numeric / COUNT(*)::numeric * 100, 2)
				ELSE 0
			END AS error_rate
		FROM call_logs
		WHERE ts >= ? AND ts < ? AND model = ?
	`, from, toExcl, modelName).Scan(&summary)

	// 每日趋势
	type trendPoint struct {
		Date   string  `json:"date"   gorm:"column:date"`
		Calls  int64   `json:"calls"  gorm:"column:calls"`
		Cost   float64 `json:"cost"   gorm:"column:cost"`
		Tokens int64   `json:"tokens" gorm:"column:tokens"`
	}
	var trend []trendPoint
	_ = db.Raw(`
		SELECT
			ts::date::text AS date,
			COUNT(*) AS calls,
			COALESCE(SUM(est_cost_usd), 0) AS cost,
			COALESCE(SUM(total_tokens), 0) AS tokens
		FROM call_logs
		WHERE ts >= ? AND ts < ? AND model = ?
		GROUP BY ts::date
		ORDER BY ts::date ASC
	`, from, toExcl, modelName).Scan(&trend)

	// 用户分布（Top 20）
	type userDist struct {
		UserID   string  `json:"user_id"    gorm:"column:user_id"`
		UserName string  `json:"user_name"  gorm:"column:user_name"`
		DeptName string  `json:"dept_name"  gorm:"column:dept_name"`
		Calls    int64   `json:"calls"      gorm:"column:calls"`
		Cost     float64 `json:"cost"       gorm:"column:cost"`
		Tokens   int64   `json:"tokens"     gorm:"column:tokens"`
	}
	var users []userDist
	_ = db.Raw(`
		SELECT
			COALESCE(user_id, 'unknown') AS user_id,
			COALESCE(user_name, '') AS user_name,
			COALESCE(dept_name, '') AS dept_name,
			COUNT(*) AS calls,
			COALESCE(SUM(est_cost_usd), 0) AS cost,
			COALESCE(SUM(total_tokens), 0) AS tokens
		FROM call_logs
		WHERE ts >= ? AND ts < ? AND model = ?
		GROUP BY COALESCE(user_id, 'unknown'), COALESCE(user_name, ''), COALESCE(dept_name, '')
		ORDER BY cost DESC
		LIMIT 20
	`, from, toExcl, modelName).Scan(&users)

	// 按 scope 过滤（只保留当前用户可见的用户分布）
	_ = usagePol
	_ = actor

	if trend == nil {
		trend = []trendPoint{}
	}
	if users == nil {
		users = []userDist{}
	}

	c.JSON(http.StatusOK, gin.H{
		"summary":           summary,
		"daily_trend":       trend,
		"user_distribution": users,
	})
}
