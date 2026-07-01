package client

// usage_client.go —— 只读 HTTP 客户端，走 /api/v1/usage/*、/api/v1/org/*、/api/v1/model-catalog/*。
// 复用网关现有 RBAC + 查询逻辑；工具零库权限、不新增加端点、不直连 DB。
// 相关接口响应结构见 internal/observability/service/rollup_service.go 与 internal/orgservice/*。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/observability/report"
)

const (
	maxPageSize    = 200
	defaultMaxLogs = 20000
)

// asiaShanghai 报告统一时区（口径：日切与 A5 非工作时段按 Asia/Shanghai）。
var asiaShanghai = func() *time.Location {
	l, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.UTC
	}
	return l
}()

type Client struct {
	base    string
	token   string
	http    *http.Client
	maxLogs int
}

type Option func(*Client)

// WithMaxLogs 设置单次 FetchLogs 可接受的最大日志数；非正值使用默认值。
func WithMaxLogs(maxLogs int) Option {
	return func(c *Client) {
		if maxLogs > 0 {
			c.maxLogs = maxLogs
		}
	}
}

// WithHTTPTimeout 设置单次 HTTP 请求超时；非正值使用默认 30s。数据量大的
// -agg（工作室/大部门）拉全公司聚合时宜调大，避免单请求超时打断。
func WithHTTPTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		if timeout > 0 {
			c.http.Timeout = timeout
		}
	}
}

func New(base, token string, options ...Option) *Client {
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	c := &Client{base: base, token: token, http: &http.Client{Timeout: 30 * time.Second}, maxLogs: defaultMaxLogs}
	for _, option := range options {
		option(c)
	}
	return c
}

// ===== 传输层结构（只读，不参与派生）=====

type LogRow struct {
	RequestID        string  `json:"request_id"`
	TS               string  `json:"ts"`
	KeyID            string  `json:"key_id"`
	KeyType          string  `json:"key_type"`
	UserID           string  `json:"user_id"`
	UserName         string  `json:"user_name"`
	DeptID           string  `json:"dept_id"`
	DeptName         string  `json:"dept_name"`
	StudioName       string  `json:"studio_name"`
	Model            string  `json:"model"`
	GroupName        string  `json:"group_name"`
	Provider         string  `json:"provider"`
	PromptTokens     int     `json:"prompt_tokens"`
	CachedTokens     int     `json:"cached_tokens"`
	CacheWriteTokens int     `json:"cache_write_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	ReasoningTokens  int     `json:"reasoning_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	Status           int     `json:"status"`
	UpstreamStatus   int     `json:"upstream_status"`
	DurationMs       int     `json:"duration_ms"`
	FirstTokenMs     int     `json:"first_token_ms"`
	FailoverUsed     bool    `json:"failover_used"`
	EstCostUSD       float64 `json:"est_cost_usd"`
}

type logsResponse struct {
	Rows       []LogRow `json:"rows"`
	Total      int64    `json:"total"`
	Page       int      `json:"page"`
	PageSize   int      `json:"page_size"`
	NextCursor string   `json:"next_cursor"`
}

// DashboardSummary /usage/dashboard 的汇总卡片（对账基准：total_tokens=SUM(prompt+completion+reasoning)，不含 cached）。
type DashboardSummary struct {
	TotalTokens         int64   `json:"total_tokens"`
	CostEstimate        float64 `json:"cost_estimate"`
	UserCount           int     `json:"user_count"`
	CallCount           int64   `json:"call_count"`
	ErrorCount          int64   `json:"error_count"`
	SuccessRate         float64 `json:"success_rate"`
	ServiceCostEstimate float64 `json:"service_cost_estimate"`
	ServiceCallCount    int64   `json:"service_call_count"`
	ServiceTotalTokens  int64   `json:"service_total_tokens"`
}

type dashboardResponse struct {
	Scope     string           `json:"scope"`
	ID        string           `json:"id"`
	ScopeName string           `json:"scope_name"`
	Summary   DashboardSummary `json:"summary"`
}

// DashboardUserMetric 是同岗分位所需的用户聚合行（对应 /usage/dashboard/users 的 rows）。
type DashboardUserMetric struct {
	UserID           string            `json:"user_id"`
	UserName         string            `json:"user_name"`
	DeptName         string            `json:"dept_name"`
	TotalTokens      int64             `json:"total_tokens"`
	PromptTokens     int64             `json:"prompt_tokens"`
	CompletionTokens int64             `json:"completion_tokens"`
	ReasoningTokens  int64             `json:"reasoning_tokens"`
	CachedTokens     int64             `json:"cached_tokens"`
	EstCostUSD       float64           `json:"est_cost_usd"`
	CallCount        int64             `json:"call_count"`
	ErrorCount       int64             `json:"error_count"`
	ModelCount       int               `json:"model_count"`
	ActiveDays       int64             `json:"active_days"`
	CostPer1kTokens  float64           `json:"cost_per_1k_tokens"`
	TopModel         string            `json:"top_model"`
	TopModelCostUSD  float64           `json:"top_model_cost_usd"`
	TopModelCalls    int64             `json:"top_model_calls"`
	WowReqPct        float64           `json:"wow_req_pct"`
	WowTokenPct      float64           `json:"wow_token_pct"`
	CostChangePct    float64           `json:"cost_change_pct"`
	TopModels        []UserTopModelRow `json:"top_models"`
}

// UserTopModelRow 用户 Top 模型行（对应服务端 top_models）。
type UserTopModelRow struct {
	Model       string  `json:"model"`
	EstCostUSD  float64 `json:"est_cost_usd"`
	CallCount   int64   `json:"call_count"`
	TotalTokens int64   `json:"total_tokens"`
}

type dashboardUsersResponse struct {
	Rows       []DashboardUserMetric `json:"rows"`
	Page       int                   `json:"page"`
	PageSize   int                   `json:"page_size"`
	TotalCount int64                 `json:"total_count"`
}

// DeptMember 组织成员（/org/departments/:dept_id/members）。
type DeptMember struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	JoinedAt    string `json:"joined_at"`
	EmployeeID  string `json:"employee_id"`
	JobTitle    string `json:"job_title"`
	JobFamily   string `json:"job_family"`
	JobCategory string `json:"job_category"`
	DeptSource  string `json:"dept_source"`
	LoginStatus string `json:"login_status"`
	HasConflict bool   `json:"has_conflict"`
}

type membersResponse struct {
	Members  []DeptMember `json:"members"`
	Total    int64        `json:"total"`
	Page     int          `json:"page"`
	PageSize int          `json:"page_size"`
}

// OrgDepartment 组织部门（/org/departments）。
type OrgDepartment struct {
	ID       string `json:"id"`
	DeptID   string `json:"dept_id"`
	DeptName string `json:"dept_name"`
	StudioID string `json:"studio_id"`
}

type departmentsResponse struct {
	Departments []OrgDepartment `json:"departments"`
	Total       int64           `json:"total"`
}

// DeptStudioMapping 部门↔工作室映射（/org/dept-studio-map）。
type DeptStudioMapping struct {
	DeptID   string `json:"dept_id"`
	StudioID string `json:"studio_id"`
}

type mappingsResponse struct {
	Mappings []DeptStudioMapping `json:"mappings"`
}

// Studio 工作室（/org/studios）。
type Studio struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type studiosResponse struct {
	Studios []Studio `json:"studios"`
}

// ModelCap 模型能力（/model-catalog/search）—— A4 有效输入预算用。
type ModelCap struct {
	Model           string `json:"model"`
	ContextLength   int64  `json:"context_length"`
	MaxOutputTokens int64  `json:"max_output_tokens"`
}

type catalogResponse struct {
	Data  []ModelCap `json:"data"`
	Total int64      `json:"total"`
}

func (c *Client) do(ctx context.Context, method, path string, q map[string]string) ([]byte, error) {
	// base 由 New 保证以 "/" 结尾；TrimLeft 去掉 path 前导 "/"，避免拼出 "//"。
	u := c.base + strings.TrimLeft(strings.TrimPrefix(path, "/api/v1"), "/")
	if len(q) > 0 {
		parts := make([]string, 0, len(q))
		for k, v := range q {
			parts = append(parts, k+"="+urlEncode(v))
		}
		u += "?" + strings.Join(parts, "&")
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 500 * time.Millisecond) // 指数退避 0.5s/1s
		}
		b, err := c.doOnce(ctx, method, u)
		if err == nil {
			return b, nil
		}
		lastErr = err
		if !isRetryable(err) {
			return nil, err
		}
	}
	return nil, lastErr
}

func (c *Client) doOnce(ctx context.Context, method, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, &httpError{status: resp.StatusCode, path: u, body: trimBytes(b, 300)}
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return nil, &httpError{status: 0, path: u, body: "empty body"}
	}
	return b, nil
}

type httpError struct {
	status     int
	path, body string
}

func (e *httpError) Error() string {
	return fmt.Sprintf("GET %s: status %d: %s", e.path, e.status, e.body)
}

// isRetryable 仅对限流(429)与上游故障(5xx)重试；4xx(如 403 越权)不重试。
func isRetryable(err error) bool {
	he, ok := err.(*httpError)
	if !ok {
		return true
	}
	return he.status == 429 || he.status >= 500
}

// FetchLogs 拉取调用明细（分页 ≤ maxPageSize，上限 c.maxLogs 条）。
// 恒走专用 keyset 接口 /usage/report/logs（服务端按 user_id/dept_id/studio_id 稳定键过滤），
// 服务端声明 total 超上限或最终未拉全时返回错误，不返回部分数据。
// csv 参数：userID→user_id（personal），deptID→dept_id（department），studioID→studio_id（studio）。
func (c *Client) FetchLogs(ctx context.Context, from, to, keyType, userID, deptID, studioID string) ([]LogRow, error) {
	var out []LogRow
	var total int64 = -1
	path := "/usage/report/logs"
	// 先以首屏（无 cursor）探测服务端 keyset：支持则后续带 cursor 逐页拉到底，无深 OFFSET。
	page := 1
	cursor := ""
	useKeyset := false
	for {
		q := map[string]string{
			"page": strconv.Itoa(page), "page_size": strconv.Itoa(maxPageSize),
			"start": from, "end": to,
		}
		if useKeyset {
			q["cursor"] = cursor
		}
		if keyType != "" {
			q["key_type"] = keyType
		}
		// 稳定键：新端点只接受 user_id/dept_id/studio_id。deptName 旧路径废弃。
		if userID != "" {
			q["user_id"] = userID
		}
		if deptID != "" {
			q["dept_id"] = deptID
		}
		if studioID != "" {
			q["studio_id"] = studioID
		}
		b, err := c.do(ctx, "GET", path, q)
		if err != nil {
			return nil, err
		}
		var resp logsResponse
		if err := json.Unmarshal(b, &resp); err != nil {
			return nil, err
		}
		// 首屏探测服务端 keyset：支持则本页即含 next_cursor，后续随 cursor 提交。
		if page == 1 && resp.NextCursor != "" && !useKeyset {
			useKeyset = true
			cursor = resp.NextCursor
		}
		if total < 0 {
			total = resp.Total
		} else if resp.Total != total {
			return nil, fmt.Errorf("usage logs total changed during pagination: was %d, now %d", total, resp.Total)
		}
		if total > int64(c.maxLogs) {
			return nil, fmt.Errorf("usage logs exceed configured max logs %d: total %d", c.maxLogs, total)
		}
		out = append(out, resp.Rows...)
		if int64(len(out)) >= total {
			break
		}
		if len(resp.Rows) == 0 {
			return nil, fmt.Errorf("incomplete usage logs: fetched %d of %d", len(out), total)
		}
		if useKeyset {
			if resp.NextCursor == "" {
				return nil, fmt.Errorf("incomplete usage logs: keyset ended early at %d of %d", len(out), total)
			}
			cursor = resp.NextCursor
		} else {
			page++
		}
	}
	if int64(len(out)) != total {
		return nil, fmt.Errorf("incomplete usage logs: fetched %d of %d", len(out), total)
	}
	return out, nil
}

// BaselineDaily 基线日聚合：某天非 service 调用数（供 28 天稳健基线物化）。
type BaselineDaily struct {
	Date     string `json:"date"`
	ReqCount int64  `json:"req_count"`
}

// BaselineBucket 基线分钟桶：某分钟的非 service 调用数与成本（供 A1/A2 历史窗口分位重建）。
type BaselineBucket struct {
	Minute string  `json:"minute"`
	Count  int64   `json:"count"`
	Cost   float64 `json:"cost"`
}

// BaselineResponse 期前基线聚合：daily（日序列）+ minute_buckets（分钟桶序列）。
type BaselineResponse struct {
	Daily         []BaselineDaily  `json:"daily"`
	MinuteBuckets []BaselineBucket `json:"minute_buckets"`
}

// FetchBaseline 拉取期前基线的「日聚合 + 分钟桶聚合」（零明细，走 /usage/report/baseline）。
// 供 28 天稳健基线物化（daily）与 A1/A2 历史窗口分位重建（minute_buckets）。
// 返回的 daily 为每日非 service 调用数；minute_buckets 为每分钟非 service 调用数 + 成本。
func (c *Client) FetchBaseline(ctx context.Context, from, to, userID, deptID, studioID string) (*BaselineResponse, error) {
	q := map[string]string{"start": from, "end": to}
	if userID != "" {
		q["user_id"] = userID
	}
	if deptID != "" {
		q["dept_id"] = deptID
	}
	if studioID != "" {
		q["studio_id"] = studioID
	}
	b, err := c.do(ctx, "GET", "/usage/report/baseline", q)
	if err != nil {
		return nil, err
	}
	var resp BaselineResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// MemberAggRow 成员级聚合单行：按 user 返回核心单一指标（口径=排除 service）。
// 与 /usage/report/members 响应行对齐；供「按人统计 + 逐层横向对比」方案的人工层地基。
type MemberAggRow struct {
	UserID           string  `json:"user_id"`
	UserName         string  `json:"user_name"`
	JobCategory      string  `json:"job_category"`
	JobFamily        string  `json:"job_family"`
	JobTitle         string  `json:"job_title"`
	DeptID           string  `json:"dept_id"`
	StudioID         string  `json:"studio_id"`
	ReqCount         int64   `json:"req_count"`
	SuccessCount     int64   `json:"success_count"`
	ErrorCount       int64   `json:"error_count"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	ReasoningTokens  int64   `json:"reasoning_tokens"`
	CachedTokens     int64   `json:"cached_tokens"`
	TotalTokens      int64   `json:"total_tokens"`
	CostUSD          float64 `json:"cost_usd"`
	ActiveDays       int64   `json:"active_days"`
	ModelCount       int     `json:"model_count"`
	// 耗时/TTFT/上下文 派生字段（服务端已算好）。用 float64 对齐服务端 AVG/PERCENTILE_CONT 浮点输出，
	// 避免 JSON 反序列化时把小数（如 123.456）塞进 int 报错。
	AvgDurationMs     float64 `json:"avg_duration_ms"`
	P95DurationMs     float64 `json:"p95_duration_ms"`
	AvgFirstTokenMs   float64 `json:"avg_first_token_ms"`
	AvgInputCtxTokens float64 `json:"avg_input_ctx_tokens"`
}

// ServiceKeyRow 程序 Service Key 聚合单行（/usage/report/service-keys 响应行）。
// 与服务端 ServiceKeyAggRow 精确对齐（owner 字段名是 service 端的 owner_name）；供「ServiceKey 表」装配。
type ServiceKeyRow struct {
	KeyID        string  `json:"key_id"`
	KeyName      string  `json:"key_name"`
	OwnerName    string  `json:"owner_name"`
	ReqCount     int64   `json:"req_count"`
	SuccessCount int64   `json:"success_count"`
	ErrorCount   int64   `json:"error_count"`
	TotalTokens  int64   `json:"total_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	ModelCount   int     `json:"model_count"`
	ActiveDays   int64   `json:"active_days"`
	StartDate    string  `json:"start_date"`
	EndDate      string  `json:"end_date"`
	// 前一周期（from-7d ~ from-1d，同 dept/studio 范围）环比 + 当期主用模型。
	PrevReqCount    int64   `json:"prev_req_count"`
	PrevTotalTokens int64   `json:"prev_total_tokens"`
	PrevCostUSD     float64 `json:"prev_cost_usd"`
	TopModel        string  `json:"top_model"`
}

// FetchMemberAgg 拉取成员级聚合（每人一行核心单一指标，排除 service），走 /usage/report/members。
// 供上层按 dept/studio rollup、横向分位、部门/工作室画像；零明细。
func (c *Client) FetchMemberAgg(ctx context.Context, from, to, userID, deptID, studioID string) ([]MemberAggRow, error) {
	q := map[string]string{"start": from, "end": to}
	if userID != "" {
		q["user_id"] = userID
	}
	if deptID != "" {
		q["dept_id"] = deptID
	}
	if studioID != "" {
		q["studio_id"] = studioID
	}
	b, err := c.do(ctx, "GET", "/usage/report/members", q)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Rows []MemberAggRow `json:"rows"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, err
	}
	return resp.Rows, nil
}

// FetchServiceKeys 拉取程序 Service Key 聚合（每 Key 一行），走 /usage/report/service-keys。
// 仅聚合 key_type='service' 的调用；deptID/studioID 非空时作为服务端过滤。zero 明细、零分页。
func (c *Client) FetchServiceKeys(ctx context.Context, from, to, deptID, studioID string) ([]ServiceKeyRow, error) {
	q := map[string]string{"start": from, "end": to}
	if deptID != "" {
		q["dept_id"] = deptID
	}
	if studioID != "" {
		q["studio_id"] = studioID
	}
	b, err := c.do(ctx, "GET", "/usage/report/service-keys", q)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Rows []ServiceKeyRow `json:"rows"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, err
	}
	return resp.Rows, nil
}

// BudgetExec 预算执行结果（工作室「一页摘要」预算执行行；服务端 /usage/report/budget-exec 响应）。
type BudgetExec struct {
	Supported     bool    `json:"supported"`
	ScopeType     string  `json:"scope_type"`
	ScopeID       string  `json:"scope_id"`
	Month         string  `json:"month"`
	MonthLimitUSD float64 `json:"month_limit_usd"`
	ActualCostUSD float64 `json:"actual_cost_usd"`
	Percent       float64 `json:"percent"`
	Note          string  `json:"note"`
}

// FetchBudgetExec 拉取预算执行：按 scope(studio|dept)+scope_id+month(YYYY-MM)。
// 返回 Supported=false 时 MonthLimitUSD/Percent 无效（未配置/额度<=0/入参缺月）。
func (c *Client) FetchBudgetExec(ctx context.Context, scope, scopeID, month string) (BudgetExec, error) {
	var res BudgetExec
	if scope == "" || scopeID == "" || month == "" {
		return res, nil
	}
	q := map[string]string{"scope": scope, "id": scopeID, "month": month}
	b, err := c.do(ctx, "GET", "/usage/report/budget-exec", q)
	if err != nil {
		return res, err
	}
	if err := json.Unmarshal(b, &res); err != nil {
		return res, err
	}
	return res, nil
}

// FetchDashboard 拉取 GetDashboard 汇总（对账基准）。level→scope 映射：personal→user, department→dept, studio→studio。
// 返回 (summary, scope, id)。任一步失败返回 error；调用方决定是否用其做对账。
func (c *Client) FetchDashboard(ctx context.Context, level, id, from, to string) (DashboardSummary, error) {
	scope := "company"
	switch level {
	case "personal":
		scope = "user"
	case "department":
		scope = "dept"
	case "studio":
		scope = "studio"
	}
	q := map[string]string{
		"scope": scope, "start": from, "end": to, "page": "1", "page_size": "20",
	}
	if id != "" {
		q["id"] = id
	}
	b, err := c.do(ctx, "GET", "/usage/dashboard", q)
	if err != nil {
		return DashboardSummary{}, err
	}
	var resp dashboardResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		return DashboardSummary{}, err
	}
	return resp.Summary, nil
}

// FetchUserMetrics 分页拉取报告期用户聚合指标，避免扫描全公司原始调用明细。
// deptID/studioID 非空时作为服务端过滤（dept_id/studio_id），支持按部门/工作室范围拉取。
func (c *Client) FetchUserMetrics(ctx context.Context, from, to, deptID, studioID string) ([]DashboardUserMetric, error) {
	var out []DashboardUserMetric
	for page := 1; ; page++ {
		q := map[string]string{
			"from": from, "to": to, "page": strconv.Itoa(page), "page_size": "100",
		}
		if deptID != "" {
			q["dept_id"] = deptID
		}
		if studioID != "" {
			q["studio_id"] = studioID
		}
		b, err := c.do(ctx, "GET", "/usage/dashboard/users", q)
		if err != nil {
			return nil, err
		}
		var resp dashboardUsersResponse
		if err := json.Unmarshal(b, &resp); err != nil {
			return nil, err
		}
		out = append(out, resp.Rows...)
		// 终止条件：本页无数据，或已达 total_count（total_count 可能因活跃数据变化有小偏差，不报错）
		if len(resp.Rows) == 0 {
			break
		}
		if resp.TotalCount > 0 && int64(len(out)) >= resp.TotalCount {
			break
		}
	}
	return out, nil
}

// FetchUserMetricsAll 拉取全公司（无部门/工作室过滤）报告期用户聚合指标，供匿名的「全公司横比」行。
// 复用 FetchUserMetrics；deptID/studioID 传空即不被服务端过滤（仍受调用方 RBAC 范围约束）。
func (c *Client) FetchUserMetricsAll(ctx context.Context, from, to string) ([]DashboardUserMetric, error) {
	return c.FetchUserMetrics(ctx, from, to, "", "")
}

// FetchMembers 拉取部门成员（/org/departments/:dept_id/members）。
func (c *Client) FetchMembers(ctx context.Context, deptID string) ([]DeptMember, error) {
	var out []DeptMember
	page := 1
	for {
		b, err := c.do(ctx, "GET", "/org/departments/"+urlEncode(deptID)+"/members", map[string]string{
			"page": strconv.Itoa(page), "page_size": "100",
		})
		if err != nil {
			return nil, err
		}
		var resp membersResponse
		if err := json.Unmarshal(b, &resp); err != nil {
			return nil, err
		}
		out = append(out, resp.Members...)
		if len(resp.Members) == 0 || page*100 >= int(resp.Total) {
			break
		}
		page++
	}
	return out, nil
}

func (c *Client) FetchDepartments(ctx context.Context, status string) ([]OrgDepartment, error) {
	var out []OrgDepartment
	page := 1
	for {
		q := map[string]string{"page": strconv.Itoa(page), "page_size": "100"}
		if status != "" {
			q["status"] = status
		}
		b, err := c.do(ctx, "GET", "/org/departments", q)
		if err != nil {
			return nil, err
		}
		var resp departmentsResponse
		if err := json.Unmarshal(b, &resp); err != nil {
			return nil, err
		}
		out = append(out, resp.Departments...)
		// 注意：/org/departments 不回显 page/page_size，按 total 推进。
		if len(resp.Departments) == 0 || page*100 >= int(resp.Total) {
			break
		}
		page++
	}
	return out, nil
}

func (c *Client) FetchDeptStudioMap(ctx context.Context) ([]DeptStudioMapping, error) {
	b, err := c.do(ctx, "GET", "/org/dept-studio-map", map[string]string{"page": "1", "page_size": "500"})
	if err != nil {
		return nil, err
	}
	var resp mappingsResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, err
	}
	return resp.Mappings, nil
}

func (c *Client) FetchStudios(ctx context.Context) ([]Studio, error) {
	b, err := c.do(ctx, "GET", "/org/studios", map[string]string{"page": "1", "page_size": "200"})
	if err != nil {
		return nil, err
	}
	var resp studiosResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, err
	}
	return resp.Studios, nil
}

// ModelContextBudget 拉取 model_catalog 的≤有效输入预算（context_length - max_output_tokens），A4 用。
func (c *Client) ModelContextBudget(ctx context.Context, models []string) map[string]int64 {
	bud := map[string]int64{}
	for _, m := range models {
		if m == "" {
			continue
		}
		if _, ok := bud[m]; ok {
			continue
		}
		b, err := c.do(ctx, "GET", "/model-catalog/search", map[string]string{"q": m, "limit": "5"})
		if err != nil {
			continue
		}
		var resp catalogResponse
		if err := json.Unmarshal(b, &resp); err != nil {
			continue
		}
		for _, cap := range resp.Data {
			if cap.Model == "" {
				continue
			}
			eff := cap.ContextLength - cap.MaxOutputTokens
			if eff <= 0 {
				eff = cap.ContextLength
			}
			if cap.Model == m {
				bud[m] = eff
				break
			}
			if bud[m] == 0 && eff > 0 {
				bud[m] = eff
			}
		}
	}
	return bud
}

// ToCallEvent 把 LogRow 转为报告中间表示 CallEvent（口径：total_tokens=prompt+completion+reasoning）。
func (r LogRow) ToCallEvent() report.CallEvent {
	ts, err := time.Parse(time.RFC3339, r.TS)
	if err != nil {
		ts, _ = time.Parse("2006-01-02 15:04:05", r.TS)
	}
	// 统一转换到 Asia/Shanghai，保证日切与 A5 非工作时段按上海时间判定（口径）。
	ts = ts.In(asiaShanghai)
	kt := r.KeyType
	if kt == "" {
		kt = "personal"
	}
	return report.CallEvent{
		TS: ts, KeyID: r.KeyID, KeyType: kt, UserID: r.UserID,
		Model: r.Model, GroupName: r.GroupName,
		Status: r.Status, DurationMs: r.DurationMs, FirstTokenMs: r.FirstTokenMs,
		PromptTokens: r.PromptTokens, CompletionTokens: r.CompletionTokens, ReasoningTokens: r.ReasoningTokens,
		CachedTokens: r.CachedTokens, CacheWriteTokens: r.CacheWriteTokens,
		PromptBeforeCached: r.PromptTokens - r.CachedTokens,
		EstCostUSD:         r.EstCostUSD, FailoverUsed: r.FailoverUsed,
	}
}

// ToMemberAgg 把成员级聚合行转为报告中间表示 MemberAgg（每人一行、周期聚合、排除 service）。
func (r MemberAggRow) ToMemberAgg() report.MemberAgg {
	return report.MemberAgg{
		UserID: r.UserID, UserName: r.UserName,
		JobCategory: r.JobCategory, JobFamily: r.JobFamily, JobTitle: r.JobTitle,
		DeptID: r.DeptID, StudioID: r.StudioID,
		ReqCount: r.ReqCount, SuccessCount: r.SuccessCount, ErrorCount: r.ErrorCount,
		PromptTokens: r.PromptTokens, CompletionTokens: r.CompletionTokens, ReasoningTokens: r.ReasoningTokens,
		CachedTokens: r.CachedTokens, TotalTokens: r.TotalTokens,
		CostUSD: r.CostUSD, ActiveDays: r.ActiveDays, ModelCount: r.ModelCount,
		AvgDurationMs: r.AvgDurationMs, P95DurationMs: r.P95DurationMs,
		AvgFirstTokenMs: r.AvgFirstTokenMs, AvgInputCtxTokens: r.AvgInputCtxTokens,
	}
}

// urlEncode 编码 query/path 值：标准 URL 百分号编码（UTF-8 字节正确，中文 dept_id/user 名不破坏编码；
// 空格统一为 %20）。
func urlEncode(s string) string { return strings.ReplaceAll(url.QueryEscape(s), "+", "%20") }

func trimBytes(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n]) + "..."
	}
	return string(b)
}
