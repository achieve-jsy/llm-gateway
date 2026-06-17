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
	FailoverUsed     bool    `json:"failover_used"`
	EstCostUSD       float64 `json:"est_cost_usd"`
}

type logsResponse struct {
	Rows     []LogRow `json:"rows"`
	Total    int64    `json:"total"`
	Page     int      `json:"page"`
	PageSize int      `json:"page_size"`
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

// DashboardUserMetric 是同岗分位所需的最小用户聚合行。
type DashboardUserMetric struct {
	UserID      string  `json:"user_id"`
	TotalTokens int64   `json:"total_tokens"`
	EstCostUSD  float64 `json:"est_cost_usd"`
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
// userID 非空时走 /usage/scoped/logs（服务端按用户过滤），否则走 /usage/logs。
// 服务端声明的 total 超过配置上限或最终未拉全时返回错误，不返回部分数据。
func (c *Client) FetchLogs(ctx context.Context, from, to, keyType, userID, deptName, studioID string) ([]LogRow, error) {
	var out []LogRow
	var total int64 = -1
	page := 1
	path := "/usage/logs"
	if userID != "" {
		path = "/usage/scoped/logs"
	}
	for {
		// /usage/logs 用 from/to；/usage/scoped/logs 用 start/end（ScopedLogs 忽略 from/to，默认 30 天窗）。
		q := map[string]string{
			"page": strconv.Itoa(page), "page_size": strconv.Itoa(maxPageSize),
		}
		if userID != "" {
			q["start"] = from
			q["end"] = to
		} else {
			q["from"] = from
			q["to"] = to
		}
		if keyType != "" {
			q["key_type"] = keyType
		}
		if userID != "" {
			q["user_id"] = userID
		}
		if deptName != "" {
			q["dept_name"] = deptName
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
		page++
	}
	if int64(len(out)) != total {
		return nil, fmt.Errorf("incomplete usage logs: fetched %d of %d", len(out), total)
	}
	return out, nil
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

// FetchDashboardUsers 分页拉取报告期用户聚合指标，避免扫描全公司原始调用明细。
func (c *Client) FetchDashboardUsers(ctx context.Context, from, to string) ([]DashboardUserMetric, error) {
	var out []DashboardUserMetric
	var total int64 = -1
	for page := 1; ; page++ {
		b, err := c.do(ctx, "GET", "/usage/dashboard/users", map[string]string{
			"from": from, "to": to, "page": strconv.Itoa(page), "page_size": "100",
		})
		if err != nil {
			return nil, err
		}
		var resp dashboardUsersResponse
		if err := json.Unmarshal(b, &resp); err != nil {
			return nil, err
		}
		if total < 0 {
			total = resp.TotalCount
		} else if total != resp.TotalCount {
			return nil, fmt.Errorf("dashboard users total changed during pagination: was %d, now %d", total, resp.TotalCount)
		}
		out = append(out, resp.Rows...)
		if int64(len(out)) >= total {
			break
		}
		if len(resp.Rows) == 0 {
			return nil, fmt.Errorf("incomplete dashboard users: fetched %d of %d", len(out), total)
		}
	}
	if int64(len(out)) != total {
		return nil, fmt.Errorf("incomplete dashboard users: fetched %d of %d", len(out), total)
	}
	return out, nil
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
		Status: r.Status, DurationMs: r.DurationMs,
		PromptTokens: r.PromptTokens, CompletionTokens: r.CompletionTokens, ReasoningTokens: r.ReasoningTokens,
		CachedTokens: r.CachedTokens, CacheWriteTokens: r.CacheWriteTokens,
		PromptBeforeCached: r.PromptTokens - r.CachedTokens,
		EstCostUSD:         r.EstCostUSD, FailoverUsed: r.FailoverUsed,
	}
}

func urlEncode(s string) string { return strings.ReplaceAll(urlEscape(s), "+", "%20") }
func urlEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' || r == '~':
			b.WriteRune(r)
		default:
			b.WriteString(fmt.Sprintf("%%%02X", r))
		}
	}
	return b.String()
}

func trimBytes(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n]) + "..."
	}
	return string(b)
}
