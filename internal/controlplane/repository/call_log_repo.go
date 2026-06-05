package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type TierUsageQuery struct {
	ScopeType string
	ScopeID   string
	// ScopeIDs 批量查询：非空时按 IN 过滤并 GROUP BY scope 列，一次返回多个 scope 的用量。
	ScopeIDs []string
	TierID   *uuid.UUID
	GroupID  *uint
	StartAt  time.Time
	EndAt    time.Time
	Total    bool
}

type TierUsageAggregate struct {
	ScopeType         string              `gorm:"column:scope_type" json:"scope_type"`
	ScopeID           string              `gorm:"column:scope_id" json:"scope_id"`
	TierID            *uuid.UUID          `gorm:"column:tier_id" json:"model_tier_id,omitempty"`
	GroupID           *uint               `gorm:"column:group_id" json:"group_id,omitempty"`
	PeriodType        string              `gorm:"-" json:"period_type,omitempty"`
	ReportedCostUSD   budgetdomain.Amount `gorm:"column:reported_cost_usd" json:"reported_cost_usd"`
	AccountingCostUSD budgetdomain.Amount `gorm:"column:accounting_cost_usd" json:"accounting_cost_usd"`
	RequestCount      int64               `gorm:"column:request_count" json:"request_count"`
}

// CallLogRepo 调用日志性能聚合数据访问层
type CallLogRepo struct {
	db *gorm.DB
}

// NewCallLogRepo 创建 CallLogRepo 实例
func NewCallLogRepo(db *gorm.DB) *CallLogRepo {
	return &CallLogRepo{db: db}
}

func (r *CallLogRepo) SumTierUsage(ctx context.Context, q TierUsageQuery) ([]TierUsageAggregate, error) {
	db := r.db.WithContext(ctx).Table("call_logs").
		Where("ts >= ? AND ts < ? AND status < 400", q.StartAt, q.EndAt)
	if !q.Total {
		db = db.Where("budget_tier_id IS NOT NULL")
	}
	if q.TierID != nil {
		db = db.Where("budget_tier_id = ?", *q.TierID)
	}
	scopeColumn := ""
	switch q.ScopeType {
	case "studio":
		scopeColumn = "studio_id"
	case "dept_daily", "dept_service":
		scopeColumn = "dept_id"
	case "member":
		scopeColumn = "user_id"
	}
	if scopeColumn != "" {
		if len(q.ScopeIDs) > 0 {
			db = db.Where(scopeColumn+" IN ?", q.ScopeIDs)
		} else if q.ScopeID != "" {
			db = db.Where(scopeColumn+" = ?", q.ScopeID)
		}
	}
	if q.ScopeType == "dept_daily" {
		db = db.Where("COALESCE(NULLIF(key_type, ''), 'personal') != 'service'")
	} else if q.ScopeType == "dept_service" {
		db = db.Where("key_type = 'service'")
	}
	scopeIDExpr := "''"
	if scopeColumn != "" {
		scopeIDExpr = scopeColumn
	}
	tierExpr := "budget_tier_id"
	if q.Total {
		tierExpr = "NULL"
	}
	groupIDExpr := "NULL"
	if q.GroupID != nil {
		db = db.Joins("JOIN model_groups mg ON mg.name = call_logs.group_name").Where("mg.id = ?", *q.GroupID)
		groupIDExpr = "mg.id"
	}
	accountingCostExpr := "SUM(CASE WHEN budget_tier_accounting_eligible THEN est_cost_usd ELSE 0 END)"
	if q.Total {
		accountingCostExpr = "SUM(est_cost_usd)"
	}
	var rows []TierUsageAggregate
	db = db.Select(
		"? AS scope_type, "+scopeIDExpr+" AS scope_id, "+tierExpr+" AS tier_id, "+groupIDExpr+" AS group_id, "+
			"SUM(est_cost_usd) AS reported_cost_usd, "+
			accountingCostExpr+" AS accounting_cost_usd, "+
			"COUNT(*) AS request_count",
		q.ScopeType,
	)
	if scopeColumn != "" {
		db = db.Group(scopeColumn)
	}
	if !q.Total {
		db = db.Group("budget_tier_id")
	}
	if q.GroupID != nil {
		db = db.Group("mg.id")
	}
	err := db.Scan(&rows).Error
	return rows, err
}

func (r *CallLogRepo) UsageFreshness(ctx context.Context) (time.Time, bool, error) {
	var latest *time.Time
	if err := r.db.WithContext(ctx).Table("call_logs").Select("MAX(ts)").Scan(&latest).Error; err != nil {
		return time.Time{}, true, err
	}
	if latest == nil {
		return time.Time{}, true, nil
	}
	return latest.UTC(), time.Since(*latest) > 10*time.Minute, nil
}

// PerformanceGroupRow 性能聚合查询结果行
type PerformanceGroupRow struct {
	Name          string  `gorm:"column:name"`
	ReqCount      int     `gorm:"column:req_count"`
	SuccessRate   float64 `gorm:"column:success_rate"`
	P50Ms         int     `gorm:"column:p50_ms"`
	P95Ms         int     `gorm:"column:p95_ms"`
	TTFTP50Ms     int     `gorm:"column:ttft_p50_ms"`
	TTFTP95Ms     int     `gorm:"column:ttft_p95_ms"`
	FailoverCount int     `gorm:"column:failover_count"`
	Err429        int     `gorm:"column:err_429"`
	Err5xx        int     `gorm:"column:err_5xx"`
	AvgTps        float64 `gorm:"column:avg_tps"`
	// model view specific
	Model    string `gorm:"column:model"`
	Provider string `gorm:"column:provider"`
}

// PerformanceMemberRow 模型组成员性能查询结果行
type PerformanceMemberRow struct {
	ModelName   string  `gorm:"column:model_name"`
	Provider    string  `gorm:"column:provider"`
	ReqCount    int     `gorm:"column:req_count"`
	TrafficPct  float64 `gorm:"column:traffic_pct"`
	P50Ms       int     `gorm:"column:p50_ms"`
	P95Ms       int     `gorm:"column:p95_ms"`
	SuccessRate float64 `gorm:"column:success_rate"`
	AvgTps      float64 `gorm:"column:avg_tps"`
	TTFTP50Ms   int     `gorm:"column:ttft_p50_ms"`
}

// windowToInterval 将时间窗口参数转换为 PostgreSQL INTERVAL 字符串
func windowToInterval(window string) string {
	switch window {
	case "6h":
		return "6 hours"
	case "12h":
		return "12 hours"
	case "24h":
		return "24 hours"
	case "7d":
		return "7 days"
	default:
		return "1 hour"
	}
}

// GetPerformanceStats 获取性能统计聚合数据
func (r *CallLogRepo) GetPerformanceStats(window, view, search string, offset, limit int) ([]PerformanceGroupRow, error) {
	interval := windowToInterval(window)

	var rows []PerformanceGroupRow

	if view == "model" {
		args := []interface{}{}
		searchCond := ""
		if search != "" {
			searchCond = "AND (model ILIKE ? OR provider ILIKE ?)"
			args = append(args, "%"+search+"%", "%"+search+"%")
		}
		args = append(args, limit, offset)

		query := fmt.Sprintf(`
			SELECT
				model || '::' || provider AS name,
				model,
				provider,
				COUNT(*)::int AS req_count,
				(SUM(CASE WHEN status BETWEEN 200 AND 299 THEN 1 ELSE 0 END)::float / NULLIF(COUNT(*), 0))::float AS success_rate,
				COALESCE((percentile_cont(0.5) WITHIN GROUP (ORDER BY duration_ms))::int, 0) AS p50_ms,
				COALESCE((percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms))::int, 0) AS p95_ms,
				COALESCE((percentile_cont(0.5) WITHIN GROUP (ORDER BY COALESCE(first_token_ms, 0)))::int, 0) AS ttft_p50_ms,
				COALESCE((percentile_cont(0.95) WITHIN GROUP (ORDER BY COALESCE(first_token_ms, 0)))::int, 0) AS ttft_p95_ms,
				SUM(CASE WHEN failover_used THEN 1 ELSE 0 END)::int AS failover_count,
				SUM(CASE WHEN status = 429 THEN 1 ELSE 0 END)::int AS err_429,
				SUM(CASE WHEN status >= 500 THEN 1 ELSE 0 END)::int AS err_5xx,
				CASE WHEN SUM(GREATEST(duration_ms - COALESCE(first_token_ms, 0), 0)) > 0
					THEN ROUND(SUM(completion_tokens)::numeric / (SUM(GREATEST(duration_ms - COALESCE(first_token_ms, 0), 0)) / 1000.0), 1)
					ELSE 0
				END AS avg_tps
			FROM call_logs
			WHERE ts > now() - INTERVAL '%s'
				%s
			GROUP BY model, provider
			ORDER BY req_count DESC
			LIMIT ? OFFSET ?
		`, interval, searchCond)

		err := r.db.Raw(query, args...).Scan(&rows).Error
		return rows, err
	}

	// group view
	args := []interface{}{}
	searchCond := ""
	if search != "" {
		searchCond = "AND group_name ILIKE ?"
		args = append(args, "%"+search+"%")
	}
	args = append(args, limit, offset)

	query := fmt.Sprintf(`
		SELECT
			group_name AS name,
			COUNT(*)::int AS req_count,
			(SUM(CASE WHEN status BETWEEN 200 AND 299 THEN 1 ELSE 0 END)::float / NULLIF(COUNT(*), 0))::float AS success_rate,
			COALESCE((percentile_cont(0.5) WITHIN GROUP (ORDER BY duration_ms))::int, 0) AS p50_ms,
			COALESCE((percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms))::int, 0) AS p95_ms,
			COALESCE((percentile_cont(0.5) WITHIN GROUP (ORDER BY COALESCE(first_token_ms, 0)))::int, 0) AS ttft_p50_ms,
			COALESCE((percentile_cont(0.95) WITHIN GROUP (ORDER BY COALESCE(first_token_ms, 0)))::int, 0) AS ttft_p95_ms,
			SUM(CASE WHEN failover_used THEN 1 ELSE 0 END)::int AS failover_count,
			SUM(CASE WHEN status = 429 THEN 1 ELSE 0 END)::int AS err_429,
			SUM(CASE WHEN status >= 500 THEN 1 ELSE 0 END)::int AS err_5xx,
			CASE WHEN SUM(GREATEST(duration_ms - COALESCE(first_token_ms, 0), 0)) > 0
				THEN ROUND(SUM(completion_tokens)::numeric / (SUM(GREATEST(duration_ms - COALESCE(first_token_ms, 0), 0)) / 1000.0), 1)
				ELSE 0
			END AS avg_tps
		FROM call_logs
		WHERE ts > now() - INTERVAL '%s'
			AND routed_via_group = true
			%s
		GROUP BY group_name
		ORDER BY req_count DESC
		LIMIT ? OFFSET ?
	`, interval, searchCond)

	err := r.db.Raw(query, args...).Scan(&rows).Error
	return rows, err
}

// CountPerformanceItems 统计符合条件的聚合项总数
func (r *CallLogRepo) CountPerformanceItems(window, view, search string) (int, error) {
	interval := windowToInterval(window)
	var count int

	if view == "model" {
		args := []interface{}{}
		searchCond := ""
		if search != "" {
			searchCond = "AND (model ILIKE ? OR provider ILIKE ?)"
			args = append(args, "%"+search+"%", "%"+search+"%")
		}

		query := fmt.Sprintf(`
			SELECT COUNT(DISTINCT model || '::' || provider)::int AS cnt
			FROM call_logs
			WHERE ts > now() - INTERVAL '%s'
				%s
		`, interval, searchCond)

		err := r.db.Raw(query, args...).Scan(&count).Error
		return count, err
	}

	// group view
	args := []interface{}{}
	searchCond := ""
	if search != "" {
		searchCond = "AND group_name ILIKE ?"
		args = append(args, "%"+search+"%")
	}

	query := fmt.Sprintf(`
		SELECT COUNT(DISTINCT group_name)::int AS cnt
		FROM call_logs
		WHERE ts > now() - INTERVAL '%s'
			AND routed_via_group = true
			%s
	`, interval, searchCond)

	err := r.db.Raw(query, args...).Scan(&count).Error
	return count, err
}

// GetGroupMembers 获取模型组内成员的性能数据
func (r *CallLogRepo) GetGroupMembers(window, groupName string) ([]PerformanceMemberRow, error) {
	interval := windowToInterval(window)
	var rows []PerformanceMemberRow

	query := fmt.Sprintf(`
		SELECT
			model AS model_name,
			provider,
			COUNT(*)::int AS req_count,
			(COUNT(*) * 1.0 / SUM(COUNT(*)) OVER ())::float AS traffic_pct,
			COALESCE((percentile_cont(0.5) WITHIN GROUP (ORDER BY duration_ms))::int, 0) AS p50_ms,
			COALESCE((percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms))::int, 0) AS p95_ms,
			(SUM(CASE WHEN status BETWEEN 200 AND 299 THEN 1 ELSE 0 END)::float / NULLIF(COUNT(*), 0))::float AS success_rate,
			COALESCE((percentile_cont(0.5) WITHIN GROUP (ORDER BY COALESCE(first_token_ms, 0)))::int, 0) AS ttft_p50_ms,
			CASE WHEN SUM(GREATEST(duration_ms - COALESCE(first_token_ms, 0), 0)) > 0
				THEN ROUND(SUM(completion_tokens)::numeric / (SUM(GREATEST(duration_ms - COALESCE(first_token_ms, 0), 0)) / 1000.0), 1)
				ELSE 0
			END AS avg_tps
		FROM call_logs
		WHERE ts > now() - INTERVAL '%s'
			AND routed_via_group = true
			AND group_name = ?
		GROUP BY model, provider
		ORDER BY req_count DESC
	`, interval)

	err := r.db.Raw(query, groupName).Scan(&rows).Error
	return rows, err
}

// baselineRow 24h 基线查询内部行
type baselineRow struct {
	GroupName string `gorm:"column:group_name"`
	HourP50   int    `gorm:"column:hour_p50"`
}

// Get24hHourlyP50 获取 24 小时内按小时分桶的 P50 延迟基线（group 视图）
func (r *CallLogRepo) Get24hHourlyP50() (map[string][]int, error) {
	var rows []baselineRow
	err := r.db.Raw(`
		SELECT
			group_name,
			COALESCE((percentile_cont(0.5) WITHIN GROUP (ORDER BY duration_ms))::int, 0) AS hour_p50
		FROM call_logs
		WHERE ts > now() - INTERVAL '24 hours'
			AND routed_via_group = true
		GROUP BY group_name, date_trunc('hour', ts)
	`).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	result := make(map[string][]int)
	for _, row := range rows {
		result[row.GroupName] = append(result[row.GroupName], row.HourP50)
	}
	return result, nil
}

// modelBaselineRow 24h 基线查询内部行（model 视图）
type modelBaselineRow struct {
	Key     string `gorm:"column:key"`
	HourP50 int    `gorm:"column:hour_p50"`
}

// Get24hHourlyP50Model 获取 24 小时内按小时分桶的 P50 延迟基线（model 视图）
func (r *CallLogRepo) Get24hHourlyP50Model() (map[string][]int, error) {
	var rows []modelBaselineRow
	err := r.db.Raw(`
		SELECT
			model || '::' || provider AS key,
			COALESCE((percentile_cont(0.5) WITHIN GROUP (ORDER BY duration_ms))::int, 0) AS hour_p50
		FROM call_logs
		WHERE ts > now() - INTERVAL '24 hours'
		GROUP BY model, provider, date_trunc('hour', ts)
	`).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	result := make(map[string][]int)
	for _, row := range rows {
		result[row.Key] = append(result[row.Key], row.HourP50)
	}
	return result, nil
}

// ModelGroupCount model 被多少组使用
type ModelGroupCount struct {
	Model      string `gorm:"column:model"`
	GroupCount int    `gorm:"column:group_count"`
}

// GetModelUsedByGroups 查询各 model 被多少组使用
func (r *CallLogRepo) GetModelUsedByGroups(window string) (map[string]int, error) {
	interval := windowToInterval(window)
	var rows []ModelGroupCount

	query := fmt.Sprintf(`
		SELECT model, COUNT(DISTINCT group_name)::int AS group_count
		FROM call_logs
		WHERE ts > now() - INTERVAL '%s'
			AND routed_via_group = true
			AND group_name != ''
		GROUP BY model
	`, interval)

	err := r.db.Raw(query).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	result := make(map[string]int, len(rows))
	for _, row := range rows {
		result[row.Model] = row.GroupCount
	}
	return result, nil
}
