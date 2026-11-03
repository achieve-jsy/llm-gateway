package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/example/llm-gateway/internal/gateway/observability/model"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
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
			stat_date, key_id, key_name, user_id, dept_id, dept_name, studio_id, model,
			req_count, success_count, prompt_tokens, cached_tokens,
			cache_write_tokens, completion_tokens, reasoning_tokens, est_cost_usd)
		SELECT
			date(ts) AS stat_date,
			key_id,
			COALESCE(MAX(key_name), ''),
			COALESCE(MAX(user_id), ''),
			COALESCE(dept_id, ''),
			COALESCE(MAX(dept_name), ''),
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
			key_name           = EXCLUDED.key_name,
			dept_name          = EXCLUDED.dept_name,
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

	// ── 新增预聚合表写入 ──────────────────────────────────────────────
	var errs []error

	// 模型维度预聚合（model_daily_stats）
	if err := s.aggregateModelDailyStats(ctx, dayStart); err != nil {
		errs = append(errs, err)
	}

	// 用户维度预聚合（user_daily_stats）
	if err := s.aggregateUserDailyStats(ctx, dayStart); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
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
	From      time.Time
	To        time.Time
	GroupBy   string // hour / day / week / month / model / dept / studio / key
	ScopeKey  string // 鉴权注入：dept_id=xxx 或 user_id=xxx
	KeyID     string
	KeyName   string // 关键词模糊搜索（ILIKE）
	UserID    string
	UserName  string // 用户名关键词搜索（ILIKE）
	DeptName  string // 部门筛选（call_logs 中存 dept_name，用 dept_name 过滤）
	StudioID  string
	ModelName string // 精确/前缀匹配模型名（model / routed_model）
	GroupName string // 模型组名搜索（group_name，仅 call_logs 路径）
	Provider  string // 渠道过滤（仅 call_logs 路径）
	Status    string // "success" | "error" | ""（仅 call_logs 路径）
	Endpoint  string // 入口端点过滤（inbound_endpoint，仅 call_logs 路径）
	TopN      int    // 已废弃，保留兼容；非时间维度分页由 Page/PageSize 控制
	Page      int    // 非时间维度分页页码，从 1 开始；0 表示不分页（返回全量，仅内部使用）
	PageSize  int    // 非时间维度每页行数，0 时使用默认值 20
}

// UsageDailyRow 查询结果行
type UsageDailyRow struct {
	StatDate         *string `json:"stat_date,omitempty"   gorm:"column:stat_date"`
	KeyID            *string `json:"key_id,omitempty"      gorm:"column:key_id"`
	KeyName          *string `json:"key_name,omitempty"    gorm:"column:key_name"`
	DeptID           *string `json:"dept_id,omitempty"     gorm:"column:dept_id"`
	DeptName         *string `json:"dept_name,omitempty"   gorm:"column:dept_name"`
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
	From       string          `json:"from"`
	To         string          `json:"to"`
	GroupBy    string          `json:"group_by"`
	Rows       []UsageDailyRow `json:"rows"`
	Total      *UsageDailyRow  `json:"total,omitempty"`
	TotalCount int64           `json:"total_count"` // 非时间维度：满足条件的实体总数（用于前端分页）
	Page       int             `json:"page,omitempty"`
	PageSize   int             `json:"page_size,omitempty"`
}

// Query 查询用量数据。
// 默认走 call_logs 实时聚合，保证数据一致性（含今日）。
// 仅当无任何精细过滤且跨度超 90 天时才走 usage_daily（性能兜底）。
func (s *RollupService) Query(ctx context.Context, params UsageDailyQueryParams) (*UsageDailyResponse, error) {
	hasFilter := params.Provider != "" || params.Status != "" ||
		params.KeyName != "" || params.GroupName != "" || params.ModelName != ""
	longRange := !params.From.IsZero() && params.To.Sub(params.From) > 90*24*time.Hour
	if !hasFilter && longRange {
		return s.queryFromUsageDaily(ctx, params)
	}
	return s.queryFromCallLogs(ctx, params)
}

// queryFromUsageDaily 从预聚合表查询（day / week / month / model / dept / key）
func (s *RollupService) queryFromUsageDaily(ctx context.Context, params UsageDailyQueryParams) (*UsageDailyResponse, error) {
	groupCols, selectCols := buildGroupByDaily(params.GroupBy)

	query := s.db.WithContext(ctx).
		Table("usage_daily").
		Select(selectCols).
		Where("stat_date BETWEEN ? AND ?", params.From.Format("2006-01-02"), params.To.Format("2006-01-02"))

	if params.ScopeKey != "" {
		query = query.Where(params.ScopeKey)
	}
	if params.KeyID != "" {
		query = query.Where("key_id = ?", params.KeyID)
	}
	if params.DeptName != "" {
		query = query.Where("dept_name = ?", params.DeptName)
	}
	if params.StudioID != "" {
		query = query.Where("studio_id = ?", params.StudioID)
	}
	if params.ModelName != "" {
		query = query.Where("model ILIKE ?", "%"+params.ModelName+"%")
	}
	if params.Endpoint != "" {
		query = query.Where("inbound_endpoint = ?", params.Endpoint)
	}

	query = query.Group(groupCols).Order("est_cost_usd DESC")

	var totalCount int64
	if err := s.db.WithContext(ctx).
		Table("(?) AS sub", query.Session(&gorm.Session{NewDB: false})).
		Select("COUNT(*)").
		Scan(&totalCount).Error; err != nil {
		return nil, fmt.Errorf("count usage_daily: %w", err)
	}
	page := params.Page
	pageSize := params.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	if page < 1 {
		page = 1
	}
	query = query.Limit(pageSize).Offset((page - 1) * pageSize)

	var rows []UsageDailyRow
	if err := query.Find(&rows).Error; err != nil {
		if isTableNotExistError(err) {
			return emptyResponse(params), nil
		}
		return nil, fmt.Errorf("query usage_daily: %w", err)
	}
	resp := buildResponse(params, rows)
	resp.TotalCount = totalCount
	resp.Page = page
	resp.PageSize = pageSize
	return resp, nil
}

// queryFromCallLogs 从原始日志实时聚合（支持 hour 粒度 / provider / status 过滤）
func (s *RollupService) queryFromCallLogs(ctx context.Context, params UsageDailyQueryParams) (*UsageDailyResponse, error) {
	groupCols, selectCols := buildGroupByLogs(params.GroupBy)

	query := s.db.WithContext(ctx).
		Table("call_logs").
		Select(selectCols).
		Where("ts >= ?", params.From)

	// 天精度 +24h 包含整天；小时精度直接用
	if params.To.Hour() == 0 && params.To.Minute() == 0 && params.To.Second() == 0 {
		query = query.Where("ts < ?", params.To.Add(24*time.Hour))
	} else {
		query = query.Where("ts <= ?", params.To)
	}

	if params.ScopeKey != "" {
		query = query.Where(params.ScopeKey)
	}
	if params.KeyID != "" {
		query = query.Where("key_id = ?", params.KeyID)
	}
	if params.KeyName != "" {
		query = query.Where("key_name ILIKE ?", "%"+params.KeyName+"%")
	}
	if params.UserName != "" {
		query = query.Where("user_name ILIKE ?", "%"+params.UserName+"%")
	}
	if params.DeptName != "" {
		query = query.Where("dept_name = ?", params.DeptName)
	}
	if params.StudioID != "" {
		query = query.Where("studio_id = ?", params.StudioID)
	}
	if params.ModelName != "" {
		query = query.Where("(model ILIKE ? OR routed_model ILIKE ?)", "%"+params.ModelName+"%", "%"+params.ModelName+"%")
	}
	if params.GroupName != "" {
		query = query.Where("group_name ILIKE ?", "%"+params.GroupName+"%")
	}
	if params.Provider != "" {
		query = query.Where("provider = ?", params.Provider)
	}
	if params.Endpoint != "" {
		query = query.Where("inbound_endpoint = ?", params.Endpoint)
	}
	switch params.Status {
	case "success":
		query = query.Where("status = 200")
	case "error":
		query = query.Where("status != 200")
	}

	orderCol := "est_cost_usd DESC"
	// 时间维度按时间升序
	timeGroupBy := params.GroupBy == "hour" || params.GroupBy == "day" || params.GroupBy == "week" || params.GroupBy == "month"
	if timeGroupBy {
		orderCol = "stat_date ASC"
	}
	query = query.Group(groupCols).Order(orderCol)

	// 所有维度均支持分页
	var totalCount int64
	if err := s.db.WithContext(ctx).
		Table("(?) AS sub", query.Session(&gorm.Session{NewDB: false})).
		Select("COUNT(*)").
		Scan(&totalCount).Error; err != nil {
		return nil, fmt.Errorf("count call_logs: %w", err)
	}
	page := params.Page
	pageSize := params.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	if page < 1 {
		page = 1
	}
	query = query.Limit(pageSize).Offset((page - 1) * pageSize)

	var rows []UsageDailyRow
	if err := query.Find(&rows).Error; err != nil {
		if isTableNotExistError(err) {
			return emptyResponse(params), nil
		}
		return nil, fmt.Errorf("query call_logs: %w", err)
	}
	resp := buildResponse(params, rows)
	resp.TotalCount = totalCount
	resp.Page = page
	resp.PageSize = pageSize
	return resp, nil
}

func emptyResponse(params UsageDailyQueryParams) *UsageDailyResponse {
	return &UsageDailyResponse{
		From:    params.From.Format("2006-01-02"),
		To:      params.To.Format("2006-01-02"),
		GroupBy: params.GroupBy,
		Rows:    []UsageDailyRow{},
	}
}

func buildResponse(params UsageDailyQueryParams, rows []UsageDailyRow) *UsageDailyResponse {
	resp := &UsageDailyResponse{
		From:    params.From.Format("2006-01-02"),
		To:      params.To.Format("2006-01-02"),
		GroupBy: params.GroupBy,
		Rows:    rows,
	}
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
	return resp
}

const aggCols = "SUM(req_count) AS req_count, SUM(success_count) AS success_count, " +
	"SUM(prompt_tokens) AS prompt_tokens, SUM(cached_tokens) AS cached_tokens, " +
	"SUM(completion_tokens) AS completion_tokens, SUM(est_cost_usd) AS est_cost_usd"

const aggColsLogs = "COUNT(*) AS req_count, COUNT(*) FILTER (WHERE status = 200) AS success_count, " +
	"COALESCE(SUM(prompt_tokens),0) AS prompt_tokens, COALESCE(SUM(cached_tokens),0) AS cached_tokens, " +
	"COALESCE(SUM(completion_tokens),0) AS completion_tokens, COALESCE(SUM(est_cost_usd),0) AS est_cost_usd"

// buildGroupByDaily 生成 usage_daily 的 GROUP BY / SELECT（支持 day/week/month/model/dept/key）
func buildGroupByDaily(groupBy string) (string, string) {
	switch groupBy {
	case "week":
		return "date_trunc('week', stat_date::timestamp)",
			"TO_CHAR(date_trunc('week', stat_date::timestamp), 'YYYY-MM-DD') AS stat_date, " + aggCols
	case "month":
		return "date_trunc('month', stat_date::timestamp)",
			"TO_CHAR(date_trunc('month', stat_date::timestamp), 'YYYY-MM') AS stat_date, " + aggCols
	case "model":
		return "model", "model, " + aggCols
	case "dept":
		return "dept_id", "dept_id, COALESCE(MAX(dept_name),'') AS dept_name, " + aggCols
	case "key":
		return "key_id", "key_id, COALESCE(MAX(key_name),'') AS key_name, " + aggCols
	default: // "day"
		return "stat_date", "TO_CHAR(stat_date, 'YYYY-MM-DD') AS stat_date, " + aggCols
	}
}

// buildGroupByLogs 生成 call_logs 的 GROUP BY / SELECT（支持 hour/day/week/month/model/dept/key）
func buildGroupByLogs(groupBy string) (string, string) {
	switch groupBy {
	case "hour":
		return "date_trunc('hour', ts)",
			"TO_CHAR(date_trunc('hour', ts), 'YYYY-MM-DD HH24:MI') AS stat_date, " + aggColsLogs
	case "week":
		return "date_trunc('week', ts)",
			"TO_CHAR(date_trunc('week', ts), 'YYYY-MM-DD') AS stat_date, " + aggColsLogs
	case "month":
		return "date_trunc('month', ts)",
			"TO_CHAR(date_trunc('month', ts), 'YYYY-MM') AS stat_date, " + aggColsLogs
	case "model":
		return "model", "model, " + aggColsLogs
	case "dept":
		return "dept_id", "dept_id, COALESCE(MAX(dept_name),'') AS dept_name, " + aggColsLogs
	case "key":
		return "key_id", "key_id, COALESCE(MAX(key_name),'') AS key_name, " + aggColsLogs
	default: // "day"
		return "date(ts)", "date(ts)::text AS stat_date, " + aggColsLogs
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
			"user_id":            row.UserID,
			"req_count":          row.ReqCount,
			"success_count":      row.SuccessCount,
			"prompt_tokens":      row.PromptTokens,
			"cached_tokens":      row.CachedTokens,
			"cache_write_tokens": row.CacheWriteTokens,
			"completion_tokens":  row.CompletionTokens,
			"reasoning_tokens":   row.ReasoningTokens,
			"est_cost_usd":       row.EstCostUSD,
		}).
		FirstOrCreate(&row).Error
}

// LogsQueryParams 明细查询参数
type LogsQueryParams struct {
	From      time.Time
	To        time.Time
	KeyID     string
	KeyName   string // 关键词模糊搜索
	KeyType   string // personal | service
	UserID    string
	UserName  string // 用户名关键词搜索（ILIKE）
	DeptName  string // 部门筛选
	StudioID  string
	ModelName string // 模型名搜索
	GroupName string // 模型组名搜索
	Provider  string
	Status    string // "success" | "error" | ""
	Endpoint  string // 入口端点过滤（inbound_endpoint）
	Seedance  bool   // 仅 Seedance 视频/素材相关请求
	Page      int
	PageSize  int
	// 鉴权注入
	ScopeSQL string
}

// MyStatsQueryParams 个人统计查询参数（直接从 call_logs 聚合）
type MyStatsQueryParams struct {
	From      time.Time
	To        time.Time
	UserID    string
	GroupBy   string // day / model / key / hour
	Model     string
	KeyID     string // 按 Key ID 筛选
	GroupName string // 按模型组名筛选
	Status    string // "success" | "error" | ""
	Endpoint  string // 入口端点过滤（inbound_endpoint）
	Page      int
	PageSize  int
}

// LogRow 明细记录行
type LogRow struct {
	RequestID           string          `json:"request_id"          gorm:"column:request_id"`
	TS                  string          `json:"ts"                  gorm:"column:ts"`
	KeyID               string          `json:"key_id"              gorm:"column:key_id"`
	KeyName             string          `json:"key_name"            gorm:"column:key_name"`
	KeyType             string          `json:"key_type"            gorm:"column:key_type"`
	UserID              string          `json:"user_id"             gorm:"column:user_id"`
	UserName            string          `json:"user_name"           gorm:"column:user_name"`
	DeptID              string          `json:"dept_id"             gorm:"column:dept_id"`
	DeptName            string          `json:"dept_name"           gorm:"column:dept_name"`
	StudioName          string          `json:"studio_name"         gorm:"column:studio_name"`
	Model               string          `json:"model"               gorm:"column:model"`
	RoutedModel         string          `json:"routed_model"        gorm:"column:routed_model"`
	GroupName           string          `json:"group_name"          gorm:"column:group_name"`
	Provider            string          `json:"provider"            gorm:"column:provider"`
	PromptTokens        int             `json:"prompt_tokens"       gorm:"column:prompt_tokens"`
	CachedTokens        int             `json:"cached_tokens"       gorm:"column:cached_tokens"`
	CacheWriteTokens    int             `json:"cache_write_tokens"  gorm:"column:cache_write_tokens"`
	CompletionTokens    int             `json:"completion_tokens"   gorm:"column:completion_tokens"`
	ReasoningTokens     int             `json:"reasoning_tokens"    gorm:"column:reasoning_tokens"`
	TotalTokens         int             `json:"total_tokens"        gorm:"column:total_tokens"`
	Status              int             `json:"status"              gorm:"column:status"`
	UpstreamStatus      int             `json:"upstream_status"     gorm:"column:upstream_status"`
	DurationMs          int             `json:"duration_ms"         gorm:"column:duration_ms"`
	FirstTokenMs        *int            `json:"first_token_ms"      gorm:"column:first_token_ms"`
	StageTTFBMs         *int            `json:"stage_ttfb_ms"       gorm:"column:stage_ttfb_ms"`
	StageAuthMs         *int16          `json:"stage_auth_ms"       gorm:"column:stage_auth_ms"`
	StageRouteMs        *int16          `json:"stage_route_ms"      gorm:"column:stage_route_ms"`
	StageUpstreamMs     *int16          `json:"stage_upstream_ms"   gorm:"column:stage_upstream_ms"`
	Stream              bool            `json:"stream"              gorm:"column:stream"`
	FailoverUsed        bool            `json:"failover_used"       gorm:"column:failover_used"`
	UsageMissing        bool            `json:"usage_missing"       gorm:"column:usage_missing"`
	ReasoningEffort     string          `json:"reasoning_effort"    gorm:"column:reasoning_effort"`
	UserAgent           string          `json:"user_agent"          gorm:"column:user_agent"`
	IPAddress           string          `json:"ip_address"          gorm:"column:ip_address"`
	InputCostUSD        float64         `json:"input_cost_usd"        gorm:"column:input_cost_usd"`
	OutputCostUSD       float64         `json:"output_cost_usd"       gorm:"column:output_cost_usd"`
	CacheReadCostUSD    float64         `json:"cache_read_cost_usd"   gorm:"column:cache_read_cost_usd"`
	EstCostUSD          float64         `json:"est_cost_usd"          gorm:"column:est_cost_usd"`
	InputPriceSnapshot  *float64        `json:"input_price_snapshot"  gorm:"column:input_price_snapshot"`
	OutputPriceSnapshot *float64        `json:"output_price_snapshot" gorm:"column:output_price_snapshot"`
	RequestBody         *datatypes.JSON `json:"request_body,omitempty"  gorm:"column:request_body"`
	ResponseBody        *datatypes.JSON `json:"response_body,omitempty" gorm:"column:response_body"`
	TaskID              string          `json:"task_id,omitempty"       gorm:"column:task_id"`
	// DocumentsCount 来自 call_traces.request_body.documents 数组长度（Rerank）
	DocumentsCount *int `json:"documents_count,omitempty" gorm:"column:documents_count"`
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

	// 列名加 call_logs. 前缀：后续 LEFT JOIN call_traces 时避免 ts/request_id 歧义
	query := s.db.WithContext(ctx).
		Table("call_logs").
		Where("call_logs.ts >= ?", params.From)

	// 天精度 +24h 包含整天；小时精度直接用
	if params.To.Hour() == 0 && params.To.Minute() == 0 && params.To.Second() == 0 {
		query = query.Where("call_logs.ts < ?", params.To.Add(24*time.Hour))
	} else {
		query = query.Where("call_logs.ts <= ?", params.To)
	}

	if params.ScopeSQL != "" {
		query = query.Where(params.ScopeSQL)
	}
	if params.UserID != "" {
		query = query.Where("call_logs.user_id = ?", params.UserID)
	}
	if params.KeyID != "" {
		query = query.Where("call_logs.key_id = ?", params.KeyID)
	}
	if params.KeyName != "" {
		query = query.Where("call_logs.key_name ILIKE ?", "%"+params.KeyName+"%")
	}
	if params.KeyType != "" {
		query = query.Where("COALESCE(NULLIF(call_logs.key_type, ''), 'personal') = ?", params.KeyType)
	}
	if params.UserName != "" {
		query = query.Where("call_logs.user_name ILIKE ?", "%"+params.UserName+"%")
	}
	if params.DeptName != "" {
		query = query.Where("call_logs.dept_name = ?", params.DeptName)
	}
	if params.StudioID != "" {
		query = query.Where("call_logs.studio_id = ?", params.StudioID)
	}
	if params.ModelName != "" {
		query = query.Where("(call_logs.model ILIKE ? OR call_logs.routed_model ILIKE ?)", "%"+params.ModelName+"%", "%"+params.ModelName+"%")
	}
	if params.GroupName != "" {
		query = query.Where("call_logs.group_name ILIKE ?", "%"+params.GroupName+"%")
	}
	if params.Provider != "" {
		query = query.Where("call_logs.provider = ?", params.Provider)
	}
	if params.Endpoint != "" {
		query = query.Where("call_logs.inbound_endpoint = ?", params.Endpoint)
	}
	if params.Seedance {
		query = ApplySeedanceLogFilter(query, "call_logs")
	}
	switch params.Status {
	case "success":
		query = query.Where("call_logs.status = 200")
	case "error":
		query = query.Where("call_logs.status != 200")
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
	// request_body / response_body 在 call_traces；列表 LEFT JOIN 供 Rerank 文档数等展示
	if err := query.
		Select("call_logs.request_id, call_logs.ts, call_logs.key_id, COALESCE(call_logs.key_name, '') AS key_name, " +
			"COALESCE(NULLIF(call_logs.key_type, ''), 'personal') AS key_type, " +
			"call_logs.user_id, call_logs.user_name, call_logs.dept_id, call_logs.dept_name, call_logs.studio_name, " +
			"call_logs.model, call_logs.routed_model, call_logs.group_name, call_logs.provider, " +
			"call_logs.prompt_tokens, call_logs.cached_tokens, call_logs.cache_write_tokens, call_logs.completion_tokens, call_logs.reasoning_tokens, call_logs.total_tokens, " +
			"call_logs.status, call_logs.upstream_status, call_logs.duration_ms, call_logs.first_token_ms, " +
			"call_logs.stage_ttfb_ms, call_logs.stage_auth_ms, call_logs.stage_route_ms, call_logs.stage_upstream_ms, " +
			"call_logs.stream, call_logs.failover_used, call_logs.usage_missing, " +
			"call_logs.reasoning_effort, call_logs.user_agent, call_logs.ip_address, " +
			"call_logs.input_cost_usd, call_logs.output_cost_usd, call_logs.cache_read_cost_usd, call_logs.est_cost_usd, " +
			"call_logs.input_price_snapshot, call_logs.output_price_snapshot, " +
			"ct.request_body AS request_body, ct.response_body AS response_body, " +
			"COALESCE(NULLIF(substring(call_logs.inbound_endpoint FROM '/v1/contents/generations/tasks/([^/?#]+)'), ''), ct.response_body->>'id', ct.response_body->>'task_id') AS task_id, " +
			"CASE WHEN jsonb_typeof(ct.request_body->'documents') = 'array' " +
			"THEN jsonb_array_length(ct.request_body->'documents') ELSE NULL END AS documents_count").
		Joins("LEFT JOIN call_traces ct ON ct.request_id = call_logs.request_id").
		Order("call_logs.ts DESC").
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

func ApplySeedanceLogFilter(query *gorm.DB, tableAlias string) *gorm.DB {
	prefix := ""
	if tableAlias != "" {
		prefix = tableAlias + "."
	}
	return query.Where(`(
		`+prefix+`inbound_endpoint = ? OR
		`+prefix+`inbound_endpoint LIKE ? OR
		`+prefix+`inbound_endpoint LIKE ? OR
		`+prefix+`model LIKE ? OR
		`+prefix+`model LIKE ? OR
		`+prefix+`model LIKE ? OR
		`+prefix+`model LIKE ?
	)`,
		"/v1/contents/generations/tasks",
		"/v1/contents/generations/tasks/%",
		"/v1/assets/%",
		"%:CreateTask",
		"%:GetTask",
		"seedance-task:%",
		"seedance-asset:%",
	)
}

// MyStatsRow 个人统计结果行
type MyStatsRow struct {
	StatDate         *string `json:"stat_date,omitempty"   gorm:"column:stat_date"`
	Model            *string `json:"model,omitempty"       gorm:"column:model"`
	KeyID            string  `json:"key_id"                gorm:"column:key_id"`
	KeyName          string  `json:"key_name"              gorm:"column:key_name"`
	ReqCount         int64   `json:"req_count"             gorm:"column:req_count"`
	SuccessCount     int64   `json:"success_count"         gorm:"column:success_count"`
	PromptTokens     int64   `json:"prompt_tokens"         gorm:"column:prompt_tokens"`
	CachedTokens     int64   `json:"cached_tokens"         gorm:"column:cached_tokens"`
	CompletionTokens int64   `json:"completion_tokens"     gorm:"column:completion_tokens"`
	EstCostUSD       float64 `json:"est_cost_usd"          gorm:"column:est_cost_usd"`
}

// MyStatsResponse 个人统计查询响应
type MyStatsResponse struct {
	From       string       `json:"from"`
	To         string       `json:"to"`
	GroupBy    string       `json:"group_by"`
	Rows       []MyStatsRow `json:"rows"`
	Total      *MyStatsRow  `json:"total,omitempty"`
	TotalCount int64        `json:"total_count"`
	Page       int          `json:"page,omitempty"`
	PageSize   int          `json:"page_size,omitempty"`
}

// QueryMyStats 查询个人用量统计（直接从 call_logs 聚合，不依赖 usage_daily）
func (s *RollupService) QueryMyStats(ctx context.Context, params MyStatsQueryParams) (*MyStatsResponse, error) {
	if params.UserID == "" {
		return nil, fmt.Errorf("user_id is required")
	}

	var groupCol, selectCols string
	switch params.GroupBy {
	case "model":
		groupCol = "model"
		selectCols = "model, COUNT(*) AS req_count, COUNT(*) FILTER (WHERE status = 200) AS success_count, " +
			"SUM(prompt_tokens) AS prompt_tokens, SUM(cached_tokens) AS cached_tokens, " +
			"SUM(completion_tokens) AS completion_tokens, SUM(est_cost_usd) AS est_cost_usd"
	case "key":
		groupCol = "COALESCE(key_id, 'unknown'), COALESCE(key_name, '')"
		selectCols = "COALESCE(key_id, 'unknown') AS key_id, COALESCE(key_name, '') AS key_name, " +
			"COUNT(*) AS req_count, COUNT(*) FILTER (WHERE status = 200) AS success_count, " +
			"SUM(prompt_tokens) AS prompt_tokens, SUM(cached_tokens) AS cached_tokens, " +
			"SUM(completion_tokens) AS completion_tokens, SUM(est_cost_usd) AS est_cost_usd"
	case "hour":
		// 按小时聚合（使用画像/使用节律）。ts 为 UTC，转本地时区后再截断到小时，
		// 否则"出没时段"会整体偏移 8 小时；stat_date 带时分，前端方能正确解析小时。
		groupCol = "date_trunc('hour', ts AT TIME ZONE 'Asia/Shanghai')"
		selectCols = "TO_CHAR(date_trunc('hour', ts AT TIME ZONE 'Asia/Shanghai'), 'YYYY-MM-DD HH24:MI') AS stat_date, " +
			"COUNT(*) AS req_count, COUNT(*) FILTER (WHERE status = 200) AS success_count, " +
			"SUM(prompt_tokens) AS prompt_tokens, SUM(cached_tokens) AS cached_tokens, " +
			"SUM(completion_tokens) AS completion_tokens, SUM(est_cost_usd) AS est_cost_usd"
	default: // day
		groupCol = "date(ts)"
		selectCols = "date(ts) AS stat_date, COUNT(*) AS req_count, COUNT(*) FILTER (WHERE status = 200) AS success_count, " +
			"SUM(prompt_tokens) AS prompt_tokens, SUM(cached_tokens) AS cached_tokens, " +
			"SUM(completion_tokens) AS completion_tokens, SUM(est_cost_usd) AS est_cost_usd"
	}

	query := s.db.WithContext(ctx).
		Table("call_logs").
		Select(selectCols).
		Where("user_id = ?", params.UserID).
		Where("ts >= ?", params.From)

	// 如果 To 时间是零点（天精度），则 +24h 包含整天；否则（小时精度）直接用
	if params.To.Hour() == 0 && params.To.Minute() == 0 && params.To.Second() == 0 {
		query = query.Where("ts < ?", params.To.Add(24*time.Hour))
	} else {
		query = query.Where("ts <= ?", params.To)
	}

	if params.Model != "" {
		query = query.Where("model = ? OR routed_model = ? OR group_name = ?", params.Model, params.Model, params.Model)
	}
	if params.KeyID != "" {
		query = query.Where("key_id = ?", params.KeyID)
	}
	if params.GroupName != "" {
		query = query.Where("group_name = ?", params.GroupName)
	}
	if params.Endpoint != "" {
		query = query.Where("inbound_endpoint = ?", params.Endpoint)
	}
	if params.Status == "success" {
		query = query.Where("status = 200")
	} else if params.Status == "error" {
		query = query.Where("status != 200")
	}

	query = query.Group(groupCol).Order("est_cost_usd DESC")

	timeGroupBy := params.GroupBy != "model" && params.GroupBy != "key"

	var totalCount int64
	if !timeGroupBy {
		if err := s.db.WithContext(ctx).
			Table("(?) AS sub", query.Session(&gorm.Session{NewDB: false})).
			Select("COUNT(*)").
			Scan(&totalCount).Error; err != nil {
			return nil, fmt.Errorf("count my stats: %w", err)
		}
		page := params.Page
		pageSize := params.PageSize
		if pageSize <= 0 {
			pageSize = 20
		}
		if page < 1 {
			page = 1
		}
		query = query.Limit(pageSize).Offset((page - 1) * pageSize)
	}

	var rows []MyStatsRow
	if err := query.Find(&rows).Error; err != nil {
		if isTableNotExistError(err) {
			return &MyStatsResponse{
				From: params.From.Format("2006-01-02"), To: params.To.Format("2006-01-02"),
				GroupBy: params.GroupBy, Rows: []MyStatsRow{},
			}, nil
		}
		return nil, fmt.Errorf("query my stats: %w", err)
	}

	resp := &MyStatsResponse{
		From:    params.From.Format("2006-01-02"),
		To:      params.To.Format("2006-01-02"),
		GroupBy: params.GroupBy,
		Rows:    rows,
	}
	if !timeGroupBy {
		resp.TotalCount = totalCount
		resp.Page = params.Page
		resp.PageSize = params.PageSize
		if resp.PageSize <= 0 {
			resp.PageSize = 20
		}
	}
	if len(rows) > 0 {
		total := MyStatsRow{}
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

// NamedOption 带名称的枚举项
type NamedOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// UsageOptions 过滤项枚举（下拉选项）
type UsageOptions struct {
	Providers []string      `json:"providers"`
	Models    []string      `json:"models"`
	Groups    []string      `json:"groups"`
	Keys      []NamedOption `json:"keys"`
	Depts     []NamedOption `json:"depts"`
	Studios   []NamedOption `json:"studios"`
}

// QueryOptions 返回过去 90 天内出现过的 provider / model / group / key / dept / studio 枚举
func (s *RollupService) QueryOptions(ctx context.Context) (*UsageOptions, error) {
	opts := &UsageOptions{
		Providers: []string{},
		Models:    []string{},
		Groups:    []string{},
		Keys:      []NamedOption{},
		Depts:     []NamedOption{},
		Studios:   []NamedOption{},
	}

	// providers
	var providers []string
	if err := s.db.WithContext(ctx).Table("call_logs").
		Select("DISTINCT provider").
		Where("provider != '' AND ts >= NOW() - INTERVAL '90 days'").
		Pluck("provider", &providers).Error; err == nil {
		opts.Providers = providers
	}

	// models — 从 call_logs 取过去 90 天实际出现过的模型名
	var models []string
	if err := s.db.WithContext(ctx).Table("call_logs").
		Select("DISTINCT model").
		Where("model != '' AND ts >= NOW() - INTERVAL '90 days'").
		Pluck("model", &models).Error; err == nil {
		opts.Models = models
	}

	// groups — 从 model_groups 取启用组名
	if err := s.db.WithContext(ctx).Table("model_groups").
		Select("name").
		Where("deleted_at IS NULL AND enabled = TRUE").
		Order("name").
		Pluck("name", &opts.Groups).Error; err != nil {
		opts.Groups = []string{}
	}

	// keys — 从 call_logs 取过去 90 天出现过的 key_id / key_name
	type keyRow struct {
		KeyID   string `gorm:"column:key_id"`
		KeyName string `gorm:"column:key_name"`
	}
	var keys []keyRow
	if err := s.db.WithContext(ctx).Table("call_logs").
		Select("DISTINCT key_id, COALESCE(MAX(key_name),'') AS key_name").
		Where("key_id != '' AND ts >= NOW() - INTERVAL '90 days'").
		Group("key_id").
		Order("key_id").
		Find(&keys).Error; err == nil {
		for _, k := range keys {
			opts.Keys = append(opts.Keys, NamedOption{ID: k.KeyID, Name: k.KeyName})
		}
	}

	// depts — 从 departments 表读取（dept_id 在 call_logs 中为空，用 dept_name 作为筛选 key）
	type deptRow struct {
		DeptID   string `gorm:"column:dept_id"`
		DeptName string `gorm:"column:dept_name"`
	}
	var depts []deptRow
	if err := s.db.WithContext(ctx).Table("departments").
		Select("dept_id, dept_name").
		Order("dept_name").
		Find(&depts).Error; err == nil {
		for _, d := range depts {
			// value = dept_name（call_logs 中存的是 dept_name，用它做筛选）
			opts.Depts = append(opts.Depts, NamedOption{ID: d.DeptName, Name: d.DeptName})
		}
	}

	// studios
	type studioRow struct {
		StudioID   string `gorm:"column:studio_id"`
		StudioName string `gorm:"column:studio_name"`
	}
	var studios []studioRow
	if err := s.db.WithContext(ctx).Table("call_logs").
		Select("DISTINCT studio_id, COALESCE(MAX(studio_name),'') AS studio_name").
		Where("studio_id != '' AND ts >= NOW() - INTERVAL '90 days'").
		Group("studio_id").
		Find(&studios).Error; err == nil {
		for _, st := range studios {
			opts.Studios = append(opts.Studios, NamedOption{ID: st.StudioID, Name: st.StudioName})
		}
	}

	return opts, nil
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

// aggregateModelDailyStats 聚合模型维度每日统计（写入 model_daily_stats 预聚合表）
// PK: (stat_date, model, studio_id) — 同模型跨工作室分行
func (s *RollupService) aggregateModelDailyStats(ctx context.Context, date time.Time) error {
	db := s.db.WithContext(ctx)
	statDate := date.Truncate(24 * time.Hour)

	sql := `
		INSERT INTO model_daily_stats (
			stat_date, model, studio_id,
			req_count, success_count, user_count, dept_count, active_days,
			prompt_tokens, cached_tokens, cache_write_tokens,
			completion_tokens, reasoning_tokens, est_cost_usd,
			avg_duration_ms, p50_duration_ms, p90_duration_ms, avg_first_token_ms
		)
		SELECT
			?::date AS stat_date,
			cl.model,
			COALESCE(dsm.studio_id::text, '') AS studio_id,
			COUNT(*) AS req_count,
			COUNT(*) FILTER (WHERE cl.status = 200) AS success_count,
			COUNT(DISTINCT cl.user_id) AS user_count,
			COUNT(DISTINCT cl.dept_id) AS dept_count,
			CASE WHEN COUNT(*) > 0 THEN 1 ELSE 0 END AS active_days,
			COALESCE(SUM(cl.prompt_tokens), 0) AS prompt_tokens,
			COALESCE(SUM(cl.cached_tokens), 0) AS cached_tokens,
			COALESCE(SUM(cl.cache_write_tokens), 0) AS cache_write_tokens,
			COALESCE(SUM(cl.completion_tokens), 0) AS completion_tokens,
			COALESCE(SUM(cl.reasoning_tokens), 0) AS reasoning_tokens,
			COALESCE(SUM(cl.est_cost_usd), 0) AS est_cost_usd,
			COALESCE(AVG(cl.duration_ms), 0)::int AS avg_duration_ms,
			COALESCE(PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY cl.duration_ms), 0)::int AS p50_duration_ms,
			COALESCE(PERCENTILE_CONT(0.9) WITHIN GROUP (ORDER BY cl.duration_ms), 0)::int AS p90_duration_ms,
			COALESCE(AVG(cl.first_token_ms), 0)::int AS avg_first_token_ms
		FROM call_logs cl
		LEFT JOIN departments d ON (d.dept_id = cl.dept_id OR d.id::text = cl.dept_id)
		LEFT JOIN dept_studio_map dsm ON dsm.dept_id = COALESCE(d.dept_id, cl.dept_id)
		WHERE cl.ts::date = ?::date
		  AND cl.model IS NOT NULL AND cl.model != ''
		GROUP BY cl.model, COALESCE(dsm.studio_id::text, '')
		ON CONFLICT (stat_date, model, studio_id) DO UPDATE SET
			req_count = EXCLUDED.req_count,
			success_count = EXCLUDED.success_count,
			user_count = EXCLUDED.user_count,
			dept_count = EXCLUDED.dept_count,
			active_days = EXCLUDED.active_days,
			prompt_tokens = EXCLUDED.prompt_tokens,
			cached_tokens = EXCLUDED.cached_tokens,
			cache_write_tokens = EXCLUDED.cache_write_tokens,
			completion_tokens = EXCLUDED.completion_tokens,
			reasoning_tokens = EXCLUDED.reasoning_tokens,
			est_cost_usd = EXCLUDED.est_cost_usd,
			avg_duration_ms = EXCLUDED.avg_duration_ms,
			p50_duration_ms = EXCLUDED.p50_duration_ms,
			p90_duration_ms = EXCLUDED.p90_duration_ms,
			avg_first_token_ms = EXCLUDED.avg_first_token_ms
	`

	if err := db.Exec(sql, statDate, statDate).Error; err != nil {
		slog.Error("model_daily_stats_aggregate_failed", "date", statDate, "error", err)
		return err
	}
	slog.Info("model_daily_stats_aggregated", "date", statDate)
	return nil
}

// aggregateUserDailyStats 聚合用户维度每日统计（写入 user_daily_stats 预聚合表）
// PK: (stat_date, user_id) — 一个用户只属于一个部门，studio/dept 通过 JOIN 获取
func (s *RollupService) aggregateUserDailyStats(ctx context.Context, date time.Time) error {
	db := s.db.WithContext(ctx)
	statDate := date.Truncate(24 * time.Hour)

	sql := `
		INSERT INTO user_daily_stats (
			stat_date, user_id, studio_id, dept_id,
			req_count, success_count, error_count, model_count,
			prompt_tokens, cached_tokens, completion_tokens, reasoning_tokens,
			est_cost_usd, hourly_distribution
		)
		SELECT
			?::date AS stat_date,
			sub.user_id,
			sub.studio_id,
			sub.dept_id,
			SUM(sub.req_count) AS req_count,
			SUM(sub.success_count) AS success_count,
			SUM(sub.error_count) AS error_count,
			-- Fix: COUNT DISTINCT models across all hours without overcounting.
			-- Unnest per-hour model arrays, deduplicate across all hours, then count.
			cardinality(array_agg(DISTINCT unnest(sub.model_list))) AS model_count,
			SUM(sub.prompt_tokens) AS prompt_tokens,
			SUM(sub.cached_tokens) AS cached_tokens,
			SUM(sub.completion_tokens) AS completion_tokens,
			SUM(sub.reasoning_tokens) AS reasoning_tokens,
			SUM(sub.est_cost_usd) AS est_cost_usd,
			jsonb_object_agg(sub.hour_key, sub.hour_count) AS hourly_distribution
		FROM (
			SELECT
				COALESCE(cl.user_id, 'unknown') AS user_id,
				COALESCE(dsm.studio_id::text, '') AS studio_id,
				COALESCE(d.dept_id, cl.dept_id, '') AS dept_id,
				COUNT(*) AS req_count,
				COUNT(*) FILTER (WHERE cl.status = 200) AS success_count,
				COUNT(*) FILTER (WHERE cl.status != 200) AS error_count,
				array_agg(DISTINCT cl.model) AS model_list,
				COALESCE(SUM(cl.prompt_tokens), 0) AS prompt_tokens,
				COALESCE(SUM(cl.cached_tokens), 0) AS cached_tokens,
				COALESCE(SUM(cl.completion_tokens), 0) AS completion_tokens,
				COALESCE(SUM(cl.reasoning_tokens), 0) AS reasoning_tokens,
				COALESCE(SUM(cl.est_cost_usd), 0) AS est_cost_usd,
				EXTRACT(HOUR FROM cl.ts)::text AS hour_key,
				COUNT(*) AS hour_count
			FROM call_logs cl
			LEFT JOIN departments d ON (d.dept_id = cl.dept_id OR d.id::text = cl.dept_id)
			LEFT JOIN dept_studio_map dsm ON dsm.dept_id = COALESCE(d.dept_id, cl.dept_id)
			WHERE cl.ts::date = ?::date
				AND (` + obsmodel.WhereExcludeServiceKeyLogsCL + `)
			GROUP BY COALESCE(cl.user_id, 'unknown'),
					 COALESCE(dsm.studio_id::text, ''),
					 COALESCE(d.dept_id, cl.dept_id, ''),
					 EXTRACT(HOUR FROM cl.ts)
		) sub
		GROUP BY sub.user_id, sub.studio_id, sub.dept_id
		ON CONFLICT (stat_date, user_id) DO UPDATE SET
			studio_id = EXCLUDED.studio_id,
			dept_id = EXCLUDED.dept_id,
			req_count = EXCLUDED.req_count,
			success_count = EXCLUDED.success_count,
			error_count = EXCLUDED.error_count,
			model_count = EXCLUDED.model_count,
			prompt_tokens = EXCLUDED.prompt_tokens,
			cached_tokens = EXCLUDED.cached_tokens,
			completion_tokens = EXCLUDED.completion_tokens,
			reasoning_tokens = EXCLUDED.reasoning_tokens,
			est_cost_usd = EXCLUDED.est_cost_usd,
			hourly_distribution = EXCLUDED.hourly_distribution
	`

	if err := db.Exec(sql, statDate, statDate).Error; err != nil {
		slog.Error("user_daily_stats_aggregate_failed", "date", statDate, "error", err)
		return err
	}
	slog.Info("user_daily_stats_aggregated", "date", statDate)
	return nil
}
