package handler

import (
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/example/llm-gateway/internal/orgservice/rbac/policy"
)

// ─── API 1: Model Hourly ────────────────────────────────────────────────────

// ModelHourlyRequest parsed query params for GET /api/v1/usage/dashboard/model-hourly.
type ModelHourlyRequest struct {
	Start     time.Time
	End       time.Time
	ModelName string
	TierName  string
	GroupBy   string // "model" | "tier"
}

// ModelHourlyPoint one hourly bucket.
type ModelHourlyPoint struct {
	Hour             string  `json:"hour"` // YYYY-MM-DD HH:00
	GroupKey         string  `json:"group_key"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	ReasoningTokens  int64   `json:"reasoning_tokens"`
	CachedTokens     int64   `json:"cached_tokens"`
	CacheWriteTokens int64   `json:"cache_write_tokens"`
	EstCostUSD       float64 `json:"est_cost_usd"`
	CallCount        int64   `json:"call_count"`
}

// ModelDailyCumulative cumulative totals for one calendar day.
type ModelDailyCumulative struct {
	Date                    string  `json:"date"` // YYYY-MM-DD
	CumPromptTokens         int64   `json:"cum_prompt_tokens"`
	CumCompletionTokens     int64   `json:"cum_completion_tokens"`
	CumReasoningTokens      int64   `json:"cum_reasoning_tokens"`
	CumCachedTokens         int64   `json:"cum_cached_tokens"`
	CumCacheWriteTokens     int64   `json:"cum_cache_write_tokens"`
	CumEstCostUSD           float64 `json:"cum_est_cost_usd"`
	CumCallCount            int64   `json:"cum_call_count"`
}

// ModelHourlyResponse full response for model-hourly.
type ModelHourlyResponse struct {
	Start     string                 `json:"start"`
	End       string                 `json:"end"`
	GroupBy   string                 `json:"group_by"`
	Hourly    []ModelHourlyPoint     `json:"hourly"`
	Daily     []ModelDailyCumulative `json:"daily"`
}

// ModelHourly GET /api/v1/usage/dashboard/model-hourly
func (h *UsageHandler) ModelHourly(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "db not configured"})
		return
	}

	from, to, ok := parseDateRangeFromTo(c)
	if !ok {
		return
	}
	toExcl := to.Add(24 * time.Hour)

	modelName := c.Query("model_name")
	tierName := c.Query("tier_name")
	groupBy := c.DefaultQuery("group_by", "model")
	if groupBy != "model" && groupBy != "tier" {
		groupBy = "model"
	}

	actor := rbac.GetActor(c)
	usagePol := policy.UsagePolicy{}

	// group expression
	var groupExpr, groupCol string
	if groupBy == "tier" {
		groupExpr = "COALESCE(NULLIF(budget_tier_name_snapshot, ''), 'unclassified')"
		groupCol = "tier"
	} else {
		groupExpr = "model"
		groupCol = "model"
	}

	db := h.db.WithContext(c.Request.Context())

	// hourly breakdown
	var hourlyRows []struct {
		Hour             string  `gorm:"column:hour"`
		GroupKey         string  `gorm:"column:group_key"`
		PromptTokens     int64   `gorm:"column:prompt_tokens"`
		CompletionTokens int64   `gorm:"column:completion_tokens"`
		ReasoningTokens  int64   `gorm:"column:reasoning_tokens"`
		CachedTokens     int64   `gorm:"column:cached_tokens"`
		CacheWriteTokens int64   `gorm:"column:cache_write_tokens"`
		EstCostUSD       float64 `gorm:"column:est_cost_usd"`
		CallCount        int64   `gorm:"column:call_count"`
	}

	hourlySQL := `
		SELECT
			to_char(date_trunc('hour', ts) AT TIME ZONE 'Asia/Shanghai', 'YYYY-MM-DD HH24:00') AS hour,
			` + groupExpr + ` AS group_key,
			COALESCE(SUM(prompt_tokens), 0)      AS prompt_tokens,
			COALESCE(SUM(completion_tokens), 0)  AS completion_tokens,
			COALESCE(SUM(reasoning_tokens), 0)   AS reasoning_tokens,
			COALESCE(SUM(cached_tokens), 0)      AS cached_tokens,
			COALESCE(SUM(cache_write_tokens), 0) AS cache_write_tokens,
			COALESCE(SUM(est_cost_usd), 0)       AS est_cost_usd,
			COUNT(*)                              AS call_count
		FROM call_logs
		WHERE ts >= ? AND ts < ?
			AND (` + obsmodel.WhereExcludeServiceKeyLogs + `)
			AND (? = '' OR model ILIKE ?)
			AND (? = '' OR COALESCE(NULLIF(budget_tier_name_snapshot, ''), 'unclassified') = ?)
		GROUP BY date_trunc('hour', ts), ` + groupExpr + `
		ORDER BY hour ASC, est_cost_usd DESC
	`
	like := "%" + modelName + "%"
	err := db.Raw(hourlySQL,
		from, toExcl,
		modelName, like,
		tierName, tierName,
	).Scopes(usagePol.QueryScope(actor)).Scan(&hourlyRows).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// daily cumulative totals
	var dailyRows []struct {
		Date                string  `gorm:"column:date"`
		PromptTokens        int64   `gorm:"column:prompt_tokens"`
		CompletionTokens    int64   `gorm:"column:completion_tokens"`
		ReasoningTokens     int64   `gorm:"column:reasoning_tokens"`
		CachedTokens        int64   `gorm:"column:cached_tokens"`
		CacheWriteTokens    int64   `gorm:"column:cache_write_tokens"`
		EstCostUSD          float64 `gorm:"column:est_cost_usd"`
		CallCount           int64   `gorm:"column:call_count"`
	}
	dailySQL := `
		SELECT
			(ts AT TIME ZONE 'Asia/Shanghai')::date::text AS date,
			COALESCE(SUM(prompt_tokens), 0)      AS prompt_tokens,
			COALESCE(SUM(completion_tokens), 0)  AS completion_tokens,
			COALESCE(SUM(reasoning_tokens), 0)   AS reasoning_tokens,
			COALESCE(SUM(cached_tokens), 0)      AS cached_tokens,
			COALESCE(SUM(cache_write_tokens), 0) AS cache_write_tokens,
			COALESCE(SUM(est_cost_usd), 0)       AS est_cost_usd,
			COUNT(*)                              AS call_count
		FROM call_logs
		WHERE ts >= ? AND ts < ?
			AND (` + obsmodel.WhereExcludeServiceKeyLogs + `)
			AND (? = '' OR model ILIKE ?)
			AND (? = '' OR COALESCE(NULLIF(budget_tier_name_snapshot, ''), 'unclassified') = ?)
		GROUP BY (ts AT TIME ZONE 'Asia/Shanghai')::date
		ORDER BY date ASC
	`
	_ = db.Raw(dailySQL,
		from, toExcl,
		modelName, like,
		tierName, tierName,
	).Scopes(usagePol.QueryScope(actor)).Scan(&dailyRows).Error

	hourly := make([]ModelHourlyPoint, 0, len(hourlyRows))
	for _, r := range hourlyRows {
		hourly = append(hourly, ModelHourlyPoint{
			Hour:             r.Hour,
			GroupKey:         r.GroupKey,
			PromptTokens:     r.PromptTokens,
			CompletionTokens: r.CompletionTokens,
			ReasoningTokens:  r.ReasoningTokens,
			CachedTokens:     r.CachedTokens,
			CacheWriteTokens: r.CacheWriteTokens,
			EstCostUSD:       r.EstCostUSD,
			CallCount:        r.CallCount,
		})
	}

	daily := make([]ModelDailyCumulative, 0, len(dailyRows))
	var cumPrompt, cumCompletion, cumReasoning, cumCached, cumCacheWrite, cumCalls int64
	var cumCost float64
	for _, r := range dailyRows {
		cumPrompt += r.PromptTokens
		cumCompletion += r.CompletionTokens
		cumReasoning += r.ReasoningTokens
		cumCached += r.CachedTokens
		cumCacheWrite += r.CacheWriteTokens
		cumCost += r.EstCostUSD
		cumCalls += r.CallCount
		daily = append(daily, ModelDailyCumulative{
			Date:                r.Date,
			CumPromptTokens:     cumPrompt,
			CumCompletionTokens: cumCompletion,
			CumReasoningTokens:  cumReasoning,
			CumCachedTokens:     cumCached,
			CumCacheWriteTokens: cumCacheWrite,
			CumEstCostUSD:       math.Round(cumCost*1e6) / 1e6,
			CumCallCount:        cumCalls,
		})
	}

	_ = groupCol

	c.JSON(http.StatusOK, ModelHourlyResponse{
		Start:   from.Format("2006-01-02"),
		End:     to.Format("2006-01-02"),
		GroupBy: groupBy,
		Hourly:  hourly,
		Daily:   daily,
	})
}

// ─── API 2: Model Users ─────────────────────────────────────────────────────

// ModelUsersRequest parsed params for GET /api/v1/usage/dashboard/model-users.
type ModelUsersRequest struct {
	Start     time.Time
	End       time.Time
	ModelName string
	TierName  string
	DeptName  string
	GroupBy   string // "dept" | "user"
	Period    string // "day" | "month"
}

// ModelDeptSummaryRow one dept in dept-summary mode.
type ModelDeptSummaryRow struct {
	DeptName         string  `json:"dept_name"`
	UserCount        int     `json:"user_count"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	ReasoningTokens  int64   `json:"reasoning_tokens"`
	CachedTokens     int64   `json:"cached_tokens"`
	EstCostUSD       float64 `json:"est_cost_usd"`
	CostPerUser      float64 `json:"cost_per_user"`
	CostPct          float64 `json:"cost_pct"`
	CallCount        int64   `json:"call_count"`
}

// ModelUserDayRow one user-per-day row.
type ModelUserDayRow struct {
	Date             string  `json:"date"`
	UserID           string  `json:"user_id"`
	UserName         string  `json:"user_name"`
	DeptName         string  `json:"dept_name"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	ReasoningTokens  int64   `json:"reasoning_tokens"`
	CachedTokens     int64   `json:"cached_tokens"`
	EstCostUSD       float64 `json:"est_cost_usd"`
	CostPct          float64 `json:"cost_pct"`
	CallCount        int64   `json:"call_count"`
}

// ModelMonthlyRow monthly aggregation row.
type ModelMonthlyRow struct {
	Month            string  `json:"month"` // YYYY-MM
	GroupKey         string  `json:"group_key"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	ReasoningTokens  int64   `json:"reasoning_tokens"`
	CachedTokens     int64   `json:"cached_tokens"`
	EstCostUSD       float64 `json:"est_cost_usd"`
	CostPct          float64 `json:"cost_pct"`
	CallCount        int64   `json:"call_count"`
	UserCount        int     `json:"user_count"`
}

// ModelUsersSummary aggregate totals.
type ModelUsersSummary struct {
	TotalUsers   int     `json:"total_users"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	TotalCalls   int64   `json:"total_calls"`
}

// ModelUsersResponse full response.
type ModelUsersResponse struct {
	Start      string                `json:"start"`
	End        string                `json:"end"`
	GroupBy    string                `json:"group_by"`
	Period     string                `json:"period"`
	Summary    ModelUsersSummary     `json:"summary"`
	DeptRows   []ModelDeptSummaryRow `json:"dept_rows,omitempty"`
	UserDayRows []ModelUserDayRow    `json:"user_day_rows,omitempty"`
	MonthRows  []ModelMonthlyRow     `json:"month_rows,omitempty"`
}

// ModelUsers GET /api/v1/usage/dashboard/model-users
func (h *UsageHandler) ModelUsers(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "db not configured"})
		return
	}

	from, to, ok := parseDateRangeFromTo(c)
	if !ok {
		return
	}
	toExcl := to.Add(24 * time.Hour)

	modelName := c.Query("model_name")
	tierName := c.Query("tier_name")
	deptName := c.Query("dept_name")
	groupBy := c.DefaultQuery("group_by", "dept")
	if groupBy != "dept" && groupBy != "user" {
		groupBy = "dept"
	}
	period := c.DefaultQuery("period", "day")
	if period != "day" && period != "month" {
		period = "day"
	}

	actor := rbac.GetActor(c)
	usagePol := policy.UsagePolicy{}
	db := h.db.WithContext(c.Request.Context())

	like := "%" + modelName + "%"

	whereArgs := []interface{}{
		from, toExcl,
		modelName, like,
		tierName, tierName,
		deptName, "%" + deptName + "%",
	}

	commonWhere := `ts >= ? AND ts < ?
			AND (` + obsmodel.WhereExcludeServiceKeyLogs + `)
			AND (? = '' OR model ILIKE ?)
			AND (? = '' OR COALESCE(NULLIF(budget_tier_name_snapshot, ''), 'unclassified') = ?)
			AND (? = '' OR COALESCE(dept_name, '') ILIKE ?)`

	// ── grand total for cost_pct ──
	var grandTotal struct {
		Cost float64 `gorm:"column:cost"`
	}
	_ = db.Raw(`SELECT COALESCE(SUM(est_cost_usd), 0) AS cost FROM call_logs WHERE `+commonWhere,
		whereArgs...).Scopes(usagePol.QueryScope(actor)).Scan(&grandTotal).Error

	var resp ModelUsersResponse
	resp.Start = from.Format("2006-01-02")
	resp.End = to.Format("2006-01-02")
	resp.GroupBy = groupBy
	resp.Period = period

	// Mode 1: dept summary with user_count
	if groupBy == "dept" && period == "day" {
		var rows []struct {
			DeptName         string  `gorm:"column:dept_name"`
			UserCount        int     `gorm:"column:user_count"`
			PromptTokens     int64   `gorm:"column:prompt_tokens"`
			CompletionTokens int64   `gorm:"column:completion_tokens"`
			ReasoningTokens  int64   `gorm:"column:reasoning_tokens"`
			CachedTokens     int64   `gorm:"column:cached_tokens"`
			EstCostUSD       float64 `gorm:"column:est_cost_usd"`
			CallCount        int64   `gorm:"column:call_count"`
		}
		sql := `
			SELECT
				COALESCE(NULLIF(dept_name, ''), 'unknown') AS dept_name,
				COUNT(DISTINCT COALESCE(user_id, ''))      AS user_count,
				COALESCE(SUM(prompt_tokens), 0)            AS prompt_tokens,
				COALESCE(SUM(completion_tokens), 0)        AS completion_tokens,
				COALESCE(SUM(reasoning_tokens), 0)         AS reasoning_tokens,
				COALESCE(SUM(cached_tokens), 0)            AS cached_tokens,
				COALESCE(SUM(est_cost_usd), 0)             AS est_cost_usd,
				COUNT(*)                                    AS call_count
			FROM call_logs
			WHERE ` + commonWhere + `
			GROUP BY COALESCE(NULLIF(dept_name, ''), 'unknown')
			ORDER BY est_cost_usd DESC
		`
		err := db.Raw(sql, whereArgs...).Scopes(usagePol.QueryScope(actor)).Scan(&rows).Error
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		var totalUsers int
		deptRows := make([]ModelDeptSummaryRow, 0, len(rows))
		for _, r := range rows {
			totalUsers += r.UserCount
			costPerUser := 0.0
			if r.UserCount > 0 {
				costPerUser = math.Round(r.EstCostUSD/float64(r.UserCount)*1e4) / 1e4
			}
			costPct := 0.0
			if grandTotal.Cost > 0 {
				costPct = math.Round(r.EstCostUSD/grandTotal.Cost*1e4) / 1e2 // percentage with 2 decimals
			}
			deptRows = append(deptRows, ModelDeptSummaryRow{
				DeptName:         r.DeptName,
				UserCount:        r.UserCount,
				PromptTokens:     r.PromptTokens,
				CompletionTokens: r.CompletionTokens,
				ReasoningTokens:  r.ReasoningTokens,
				CachedTokens:     r.CachedTokens,
				EstCostUSD:       r.EstCostUSD,
				CostPerUser:      costPerUser,
				CostPct:          costPct,
				CallCount:        r.CallCount,
			})
		}
		resp.Summary = ModelUsersSummary{
			TotalUsers:   totalUsers,
			TotalCostUSD: grandTotal.Cost,
			TotalCalls:   sumDeptCalls(rows),
		}
		resp.DeptRows = deptRows

	// Mode 2: user-per-day breakdown
	} else if groupBy == "user" && period == "day" {
		var rows []struct {
			Date             string  `gorm:"column:date"`
			UserID           string  `gorm:"column:user_id"`
			UserName         string  `gorm:"column:user_name"`
			DeptName         string  `gorm:"column:dept_name"`
			PromptTokens     int64   `gorm:"column:prompt_tokens"`
			CompletionTokens int64   `gorm:"column:completion_tokens"`
			ReasoningTokens  int64   `gorm:"column:reasoning_tokens"`
			CachedTokens     int64   `gorm:"column:cached_tokens"`
			EstCostUSD       float64 `gorm:"column:est_cost_usd"`
			CallCount        int64   `gorm:"column:call_count"`
		}
		sql := `
			SELECT
				(ts AT TIME ZONE 'Asia/Shanghai')::date::text  AS date,
				COALESCE(user_id, 'unknown')                    AS user_id,
				COALESCE(user_name, '')                         AS user_name,
				COALESCE(dept_name, '')                         AS dept_name,
				COALESCE(SUM(prompt_tokens), 0)                AS prompt_tokens,
				COALESCE(SUM(completion_tokens), 0)            AS completion_tokens,
				COALESCE(SUM(reasoning_tokens), 0)             AS reasoning_tokens,
				COALESCE(SUM(cached_tokens), 0)                AS cached_tokens,
				COALESCE(SUM(est_cost_usd), 0)                 AS est_cost_usd,
				COUNT(*)                                        AS call_count
			FROM call_logs
			WHERE ` + commonWhere + `
			GROUP BY (ts AT TIME ZONE 'Asia/Shanghai')::date,
			         COALESCE(user_id, 'unknown'),
			         COALESCE(user_name, ''),
			         COALESCE(dept_name, '')
			ORDER BY date ASC, est_cost_usd DESC
		`
		err := db.Raw(sql, whereArgs...).Scopes(usagePol.QueryScope(actor)).Scan(&rows).Error
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		seen := make(map[string]bool)
		var totalCalls int64
		userDayRows := make([]ModelUserDayRow, 0, len(rows))
		for _, r := range rows {
			seen[r.UserID] = true
			totalCalls += r.CallCount
			costPct := 0.0
			if grandTotal.Cost > 0 {
				costPct = math.Round(r.EstCostUSD/grandTotal.Cost*1e4) / 1e2
			}
			userDayRows = append(userDayRows, ModelUserDayRow{
				Date:             r.Date,
				UserID:           r.UserID,
				UserName:         r.UserName,
				DeptName:         r.DeptName,
				PromptTokens:     r.PromptTokens,
				CompletionTokens: r.CompletionTokens,
				ReasoningTokens:  r.ReasoningTokens,
				CachedTokens:     r.CachedTokens,
				EstCostUSD:       r.EstCostUSD,
				CostPct:          costPct,
				CallCount:        r.CallCount,
			})
		}
		resp.Summary = ModelUsersSummary{
			TotalUsers:   len(seen),
			TotalCostUSD: grandTotal.Cost,
			TotalCalls:   totalCalls,
		}
		resp.UserDayRows = userDayRows

	// Mode 3: monthly aggregation
	} else if period == "month" {
		var groupExpr string
		if groupBy == "dept" {
			groupExpr = "COALESCE(NULLIF(dept_name, ''), 'unknown')"
		} else {
			groupExpr = "COALESCE(user_id, 'unknown')"
		}

		var rows []struct {
			Month            string  `gorm:"column:month"`
			GroupKey         string  `gorm:"column:group_key"`
			PromptTokens     int64   `gorm:"column:prompt_tokens"`
			CompletionTokens int64   `gorm:"column:completion_tokens"`
			ReasoningTokens  int64   `gorm:"column:reasoning_tokens"`
			CachedTokens     int64   `gorm:"column:cached_tokens"`
			EstCostUSD       float64 `gorm:"column:est_cost_usd"`
			CallCount        int64   `gorm:"column:call_count"`
			UserCount        int     `gorm:"column:user_count"`
		}
		sql := `
			SELECT
				to_char(date_trunc('month', ts AT TIME ZONE 'Asia/Shanghai'), 'YYYY-MM') AS month,
				` + groupExpr + ` AS group_key,
				COALESCE(SUM(prompt_tokens), 0)      AS prompt_tokens,
				COALESCE(SUM(completion_tokens), 0)  AS completion_tokens,
				COALESCE(SUM(reasoning_tokens), 0)   AS reasoning_tokens,
				COALESCE(SUM(cached_tokens), 0)      AS cached_tokens,
				COALESCE(SUM(est_cost_usd), 0)       AS est_cost_usd,
				COUNT(*)                              AS call_count,
				COUNT(DISTINCT COALESCE(user_id, '')) AS user_count
			FROM call_logs
			WHERE ` + commonWhere + `
			GROUP BY date_trunc('month', ts AT TIME ZONE 'Asia/Shanghai'), ` + groupExpr + `
			ORDER BY month ASC, est_cost_usd DESC
		`
		err := db.Raw(sql, whereArgs...).Scopes(usagePol.QueryScope(actor)).Scan(&rows).Error
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		seen := make(map[string]bool)
		var totalCalls int64
		monthRows := make([]ModelMonthlyRow, 0, len(rows))
		for _, r := range rows {
			seen[r.GroupKey] = true
			totalCalls += r.CallCount
			costPct := 0.0
			if grandTotal.Cost > 0 {
				costPct = math.Round(r.EstCostUSD/grandTotal.Cost*1e4) / 1e2
			}
			monthRows = append(monthRows, ModelMonthlyRow{
				Month:            r.Month,
				GroupKey:         r.GroupKey,
				PromptTokens:     r.PromptTokens,
				CompletionTokens: r.CompletionTokens,
				ReasoningTokens:  r.ReasoningTokens,
				CachedTokens:     r.CachedTokens,
				EstCostUSD:       r.EstCostUSD,
				CostPct:          costPct,
				CallCount:        r.CallCount,
				UserCount:        r.UserCount,
			})
		}
		resp.Summary = ModelUsersSummary{
			TotalUsers:   len(seen),
			TotalCostUSD: grandTotal.Cost,
			TotalCalls:   totalCalls,
		}
		resp.MonthRows = monthRows
	}

	if resp.DeptRows == nil {
		resp.DeptRows = []ModelDeptSummaryRow{}
	}
	if resp.UserDayRows == nil {
		resp.UserDayRows = []ModelUserDayRow{}
	}
	if resp.MonthRows == nil {
		resp.MonthRows = []ModelMonthlyRow{}
	}

	c.JSON(http.StatusOK, resp)
}

func sumDeptCalls(rows []struct {
	DeptName         string  `gorm:"column:dept_name"`
	UserCount        int     `gorm:"column:user_count"`
	PromptTokens     int64   `gorm:"column:prompt_tokens"`
	CompletionTokens int64   `gorm:"column:completion_tokens"`
	ReasoningTokens  int64   `gorm:"column:reasoning_tokens"`
	CachedTokens     int64   `gorm:"column:cached_tokens"`
	EstCostUSD       float64 `gorm:"column:est_cost_usd"`
	CallCount        int64   `gorm:"column:call_count"`
}) int64 {
	var total int64
	for _, r := range rows {
		total += r.CallCount
	}
	return total
}

// ─── Route registration helper ──────────────────────────────────────────────

// RegisterModelAnalyticsRoutes adds model analytics routes to the given router group.
// Call this from the control-plane router setup, e.g.:
//
//	handler.RegisterModelAnalyticsRoutes(apiV1, usageHandler)
func RegisterModelAnalyticsRoutes(g *gin.RouterGroup, h *UsageHandler) {
	g.GET("/usage/dashboard/model-hourly", h.ModelHourly)
	g.GET("/usage/dashboard/model-users", h.ModelUsers)
}

// suppress unused import warnings
var _ = strings.TrimSpace
