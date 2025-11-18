package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/example/llm-gateway/internal/gateway/observability/model"
)

// RollupService 按天聚合服务
type RollupService struct {
	db *gorm.DB
}

// NewRollupService 创建 RollupService
func NewRollupService(db *gorm.DB) *RollupService {
	return &RollupService{db: db}
}

// Run 执行一次聚合：整日重算指定日期的 usage_daily
// day: 要聚合的日期（按 call_logs.ts 筛选）
func (s *RollupService) Run(ctx context.Context, day time.Time) error {
	dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	dayEnd := dayStart.Add(24 * time.Hour)

	slog.InfoContext(ctx, "rollup_start",
		"day", dayStart.Format("2006-01-02"),
	)

	result := s.db.WithContext(ctx).Exec(`
		INSERT INTO usage_daily (
			stat_date, key_id, user_id, dept_id, studio_id, model,
			req_count, success_count, prompt_tokens, cached_tokens,
			cache_write_tokens, completion_tokens, reasoning_tokens, est_cost_usd)
		SELECT
			date(ts) AS stat_date,
			key_id,
			COALESCE(MAX(user_id), ''),
			COALESCE(dept_id, ''),
			COALESCE(studio_id, ''),
			model,
			COUNT(*) AS req_count,
			COUNT(*) FILTER (WHERE status = 200) AS success_count,
			SUM(prompt_tokens),
			SUM(cached_tokens),
			SUM(cache_write_tokens),
			SUM(completion_tokens),
			SUM(reasoning_tokens),
			SUM(est_cost_usd)
		FROM call_logs
		WHERE ts >= ? AND ts < ?
		GROUP BY date(ts), key_id, COALESCE(dept_id, ''), COALESCE(studio_id, ''), model
		ON CONFLICT (stat_date, key_id, dept_id, studio_id, model) DO UPDATE SET
			req_count          = EXCLUDED.req_count,
			success_count      = EXCLUDED.success_count,
			prompt_tokens      = EXCLUDED.prompt_tokens,
			cached_tokens      = EXCLUDED.cached_tokens,
			cache_write_tokens = EXCLUDED.cache_write_tokens,
			completion_tokens  = EXCLUDED.completion_tokens,
			reasoning_tokens   = EXCLUDED.reasoning_tokens,
			est_cost_usd       = EXCLUDED.est_cost_usd
	`, dayStart, dayEnd)

	if result.Error != nil {
		slog.ErrorContext(ctx, "rollup_error",
			"day", dayStart.Format("2006-01-02"),
			"error", result.Error,
		)
		return fmt.Errorf("rollup failed for %s: %w", dayStart.Format("2006-01-02"), result.Error)
	}

	slog.InfoContext(ctx, "rollup_done",
		"day", dayStart.Format("2006-01-02"),
		"rows_affected", result.RowsAffected,
	)

	return nil
}

// RunTodayAndYesterday 覆盖今天 + 昨天（跨零点延迟到达）
func (s *RollupService) RunTodayAndYesterday(ctx context.Context) error {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	yesterday := today.Add(-24 * time.Hour)

	if err := s.Run(ctx, today); err != nil {
		return err
	}
	return s.Run(ctx, yesterday)
}

// UsageDailyQueryParams 查询参数
type UsageDailyQueryParams struct {
	From     time.Time
	To       time.Time
	GroupBy  string // day / model / dept / studio / key
	ScopeKey string // 鉴权注入：dept_id=xxx 或 user_id=xxx
	KeyID    string
	UserID   string
	DeptID   string
	Model    string
}

// UsageDailyRow 查询结果行
type UsageDailyRow struct {
	StatDate         *string `json:"stat_date,omitempty"   gorm:"column:stat_date"`
	KeyID            *string `json:"key_id,omitempty"      gorm:"column:key_id"`
	DeptID           *string `json:"dept_id,omitempty"     gorm:"column:dept_id"`
	Model            *string `json:"model,omitempty"       gorm:"column:model"`
	ReqCount         int64   `json:"req_count"             gorm:"column:req_count"`
	SuccessCount     int64   `json:"success_count,omitempty" gorm:"column:success_count"`
	PromptTokens     int64   `json:"prompt_tokens"         gorm:"column:prompt_tokens"`
	CachedTokens     int64   `json:"cached_tokens"         gorm:"column:cached_tokens"`
	CompletionTokens int64   `json:"completion_tokens"     gorm:"column:completion_tokens"`
	EstCostUSD       float64 `json:"est_cost_usd"          gorm:"column:est_cost_usd"`
}

// UsageDailyResponse 查询响应
type UsageDailyResponse struct {
	From    string          `json:"from"`
	To      string          `json:"to"`
	GroupBy string          `json:"group_by"`
	Rows    []UsageDailyRow `json:"rows"`
	Total   *UsageDailyRow  `json:"total,omitempty"`
}

// Query 查询用量数据
func (s *RollupService) Query(ctx context.Context, params UsageDailyQueryParams) (*UsageDailyResponse, error) {
	// 构建 GROUP BY 子句
	groupCols, selectCols := buildGroupBy(params.GroupBy)

	// 构建查询
	query := s.db.WithContext(ctx).
		Table("usage_daily").
		Select(selectCols).
		Where("stat_date BETWEEN ? AND ?", params.From.Format("2006-01-02"), params.To.Format("2006-01-02"))

	// 鉴权注入
	if params.ScopeKey != "" {
		query = query.Where(params.ScopeKey)
	}

	// 可选过滤
	if params.KeyID != "" {
		query = query.Where("key_id = ?", params.KeyID)
	}
	if params.DeptID != "" {
		query = query.Where("dept_id = ?", params.DeptID)
	}
	if params.Model != "" {
		query = query.Where("model = ?", params.Model)
	}

	query = query.Group(groupCols).Order("est_cost_usd DESC")

	var rows []UsageDailyRow
	if err := query.Find(&rows).Error; err != nil {
		// 表不存在时返回空结果（迁移未执行场景）
		if isTableNotExistError(err) {
			return &UsageDailyResponse{
				From:    params.From.Format("2006-01-02"),
				To:      params.To.Format("2006-01-02"),
				GroupBy: params.GroupBy,
				Rows:    []UsageDailyRow{},
			}, nil
		}
		return nil, fmt.Errorf("query usage_daily: %w", err)
	}

	resp := &UsageDailyResponse{
		From:    params.From.Format("2006-01-02"),
		To:      params.To.Format("2006-01-02"),
		GroupBy: params.GroupBy,
		Rows:    rows,
	}

	// 计算总计
	if len(rows) > 0 {
		total := UsageDailyRow{}
		for _, r := range rows {
			total.ReqCount += r.ReqCount
			total.SuccessCount += r.SuccessCount
			total.PromptTokens += r.PromptTokens
			total.CachedTokens += r.CachedTokens
			total.CompletionTokens += r.CompletionTokens
			total.EstCostUSD += r.EstCostUSD
		}
		resp.Total = &total
	}

	return resp, nil
}

func buildGroupBy(groupBy string) (string, string) {	switch groupBy {
	case "model":
		return "model", "model, SUM(req_count) AS req_count, SUM(success_count) AS success_count, SUM(prompt_tokens) AS prompt_tokens, SUM(cached_tokens) AS cached_tokens, SUM(completion_tokens) AS completion_tokens, SUM(est_cost_usd) AS est_cost_usd"
	case "dept":
		return "dept_id", "dept_id, SUM(req_count) AS req_count, SUM(success_count) AS success_count, SUM(prompt_tokens) AS prompt_tokens, SUM(cached_tokens) AS cached_tokens, SUM(completion_tokens) AS completion_tokens, SUM(est_cost_usd) AS est_cost_usd"
	case "key":
		return "key_id", "key_id, SUM(req_count) AS req_count, SUM(success_count) AS success_count, SUM(prompt_tokens) AS prompt_tokens, SUM(cached_tokens) AS cached_tokens, SUM(completion_tokens) AS completion_tokens, SUM(est_cost_usd) AS est_cost_usd"
	default: // "day"
		return "stat_date", "stat_date, SUM(req_count) AS req_count, SUM(success_count) AS success_count, SUM(prompt_tokens) AS prompt_tokens, SUM(cached_tokens) AS cached_tokens, SUM(completion_tokens) AS completion_tokens, SUM(est_cost_usd) AS est_cost_usd"
	}
}

// UsageDaily aggregation model for direct scan
type UsageDailyAgg struct {
	StatDate         time.Time
	KeyID            string
	UserID           string
	DeptID           string
	StudioID         string
	Model            string
	ReqCount         int
	SuccessCount     int
	PromptTokens     int64
	CachedTokens     int64
	CacheWriteTokens int64
	CompletionTokens int64
	ReasoningTokens  int64
	EstCostUSD       float64
}

func (UsageDailyAgg) TableName() string {
	return "usage_daily"
}

// InsertDailyUpsert inserts or updates a usage_daily row
func InsertDailyUpsert(ctx context.Context, db *gorm.DB, row model.UsageDaily) error {
	return db.WithContext(ctx).
		Table("usage_daily").
		Where("stat_date = ? AND key_id = ? AND dept_id = ? AND studio_id = ? AND model = ?",
			row.StatDate, row.KeyID, row.DeptID, row.StudioID, row.Model).
		Assign(map[string]interface{}{
			"user_id":           row.UserID,
			"req_count":         row.ReqCount,
			"success_count":     row.SuccessCount,
			"prompt_tokens":     row.PromptTokens,
			"cached_tokens":     row.CachedTokens,
			"cache_write_tokens": row.CacheWriteTokens,
			"completion_tokens":  row.CompletionTokens,
			"reasoning_tokens":   row.ReasoningTokens,
			"est_cost_usd":       row.EstCostUSD,
		}).
		FirstOrCreate(&row).Error
}

// LogsQueryParams 明细查询参数
type LogsQueryParams struct {
	From     time.Time
	To       time.Time
	KeyID    string
	UserID   string
	DeptID   string
	Model    string
	Page     int
	PageSize int
	// 鉴权注入
	ScopeSQL string
}

// LogRow 明细记录行
type LogRow struct {
	RequestID        string   `json:"request_id"          gorm:"column:request_id"`
	TS               string   `json:"ts"                  gorm:"column:ts"`
	KeyID            string   `json:"key_id"              gorm:"column:key_id"`
	UserID           string   `json:"user_id"             gorm:"column:user_id"`
	UserName         string   `json:"user_name"           gorm:"column:user_name"`
	DeptID           string   `json:"dept_id"             gorm:"column:dept_id"`
	DeptName         string   `json:"dept_name"           gorm:"column:dept_name"`
	StudioName       string   `json:"studio_name"         gorm:"column:studio_name"`
	Model            string   `json:"model"               gorm:"column:model"`
	RoutedModel      string   `json:"routed_model"        gorm:"column:routed_model"`
	GroupName        string   `json:"group_name"          gorm:"column:group_name"`
	Provider         string   `json:"provider"            gorm:"column:provider"`
	PromptTokens     int      `json:"prompt_tokens"       gorm:"column:prompt_tokens"`
	CachedTokens     int      `json:"cached_tokens"       gorm:"column:cached_tokens"`
	CacheWriteTokens int      `json:"cache_write_tokens"  gorm:"column:cache_write_tokens"`
	CompletionTokens int      `json:"completion_tokens"   gorm:"column:completion_tokens"`
	ReasoningTokens  int      `json:"reasoning_tokens"    gorm:"column:reasoning_tokens"`
	TotalTokens      int      `json:"total_tokens"        gorm:"column:total_tokens"`
	Status           int      `json:"status"              gorm:"column:status"`
	UpstreamStatus   int      `json:"upstream_status"     gorm:"column:upstream_status"`
	DurationMs       int      `json:"duration_ms"         gorm:"column:duration_ms"`
	FirstTokenMs     *int     `json:"first_token_ms"      gorm:"column:first_token_ms"`
	Stream           bool     `json:"stream"              gorm:"column:stream"`
	FailoverUsed     bool     `json:"failover_used"       gorm:"column:failover_used"`
	UsageMissing     bool     `json:"usage_missing"       gorm:"column:usage_missing"`
	ReasoningEffort  string   `json:"reasoning_effort"    gorm:"column:reasoning_effort"`
	UserAgent        string   `json:"user_agent"          gorm:"column:user_agent"`
	IPAddress        string   `json:"ip_address"          gorm:"column:ip_address"`
	InputCostUSD        float64  `json:"input_cost_usd"        gorm:"column:input_cost_usd"`
	OutputCostUSD       float64  `json:"output_cost_usd"       gorm:"column:output_cost_usd"`
	CacheReadCostUSD    float64  `json:"cache_read_cost_usd"   gorm:"column:cache_read_cost_usd"`
	EstCostUSD          float64  `json:"est_cost_usd"          gorm:"column:est_cost_usd"`
	InputPriceSnapshot  *float64 `json:"input_price_snapshot"  gorm:"column:input_price_snapshot"`
	OutputPriceSnapshot *float64 `json:"output_price_snapshot" gorm:"column:output_price_snapshot"`
}

// LogsResponse 明细查询响应
type LogsResponse struct {
	Rows     []LogRow `json:"rows"`
	Total    int64    `json:"total"`
	Page     int      `json:"page"`
	PageSize int      `json:"page_size"`
}

// QueryLogs 查询调用明细
func (s *RollupService) QueryLogs(ctx context.Context, params LogsQueryParams) (*LogsResponse, error) {
	if params.Page < 1 {
		params.Page = 1
	}
	if params.PageSize < 1 || params.PageSize > 200 {
		params.PageSize = 50
	}

	query := s.db.WithContext(ctx).
		Table("call_logs").
		Where("ts >= ? AND ts < ?", params.From, params.To.Add(24*time.Hour))

	if params.ScopeSQL != "" {
		query = query.Where(params.ScopeSQL)
	}
	if params.KeyID != "" {
		query = query.Where("key_id = ?", params.KeyID)
	}
	if params.DeptID != "" {
		query = query.Where("dept_id = ?", params.DeptID)
	}
	if params.Model != "" {
		query = query.Where("model = ? OR routed_model = ? OR group_name = ?", params.Model, params.Model, params.Model)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		if isTableNotExistError(err) {
			return &LogsResponse{Rows: []LogRow{}, Total: 0, Page: params.Page, PageSize: params.PageSize}, nil
		}
		return nil, fmt.Errorf("count call_logs: %w", err)
	}

	var rows []LogRow
	offset := (params.Page - 1) * params.PageSize
	if err := query.
		Select("request_id, ts, key_id, user_id, user_name, dept_id, dept_name, studio_name, " +
			"model, routed_model, group_name, provider, " +
			"prompt_tokens, cached_tokens, cache_write_tokens, completion_tokens, reasoning_tokens, total_tokens, " +
			"status, upstream_status, duration_ms, first_token_ms, stream, failover_used, usage_missing, " +
			"reasoning_effort, user_agent, ip_address, " +
			"input_cost_usd, output_cost_usd, cache_read_cost_usd, est_cost_usd, " +
			"input_price_snapshot, output_price_snapshot").
		Order("ts DESC").
		Offset(offset).
		Limit(params.PageSize).
		Find(&rows).Error; err != nil {
		if isTableNotExistError(err) {
			return &LogsResponse{Rows: []LogRow{}, Total: 0, Page: params.Page, PageSize: params.PageSize}, nil
		}
		return nil, fmt.Errorf("query call_logs: %w", err)
	}

	return &LogsResponse{Rows: rows, Total: total, Page: params.Page, PageSize: params.PageSize}, nil
}
func isTableNotExistError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "no such table") ||
		strings.Contains(msg, "table") && strings.Contains(msg, "not found")
}