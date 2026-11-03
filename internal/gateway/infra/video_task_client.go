package infra

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/httpclient"
)

// VideoTaskClient 通过控制面内部 API 读写 video_tasks。
type VideoTaskClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewVideoTaskClient 创建客户端。
func NewVideoTaskClient(baseURL, token string) *VideoTaskClient {
	cfg := httpclient.DefaultConfig()
	cfg.Timeout = 3 * time.Second
	cfg.ResponseHeaderTimeout = 3 * time.Second
	return &VideoTaskClient{
		baseURL:    baseURL,
		token:      token,
		httpClient: httpclient.NewHTTPClient(&cfg),
	}
}

// CreateVideoTaskRequest 与控制面 InternalCreate 对齐。
type CreateVideoTaskRequest struct {
	TaskID       string `json:"task_id"`
	APIKeyID     string `json:"api_key_id"`
	APIKeyName   string `json:"api_key_name"`
	APIKeyType   string `json:"api_key_type"`
	UserID       string `json:"user_id"`
	UserName     string `json:"user_name"`
	Model        string `json:"model"`
	ProviderName string `json:"provider_name"`
	ProviderType string `json:"provider_type"`
	Status       string `json:"status,omitempty"`
	RequestBody  string `json:"request_body,omitempty"`
}

// UpdateVideoTaskRequest 与控制面 InternalUpdate 对齐。
type UpdateVideoTaskRequest struct {
	Status       string `json:"status,omitempty"`
	VideoURL     string `json:"video_url,omitempty"`
	ResponseBody string `json:"response_body,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// Create 写入 video_tasks。
func (c *VideoTaskClient) Create(ctx context.Context, req CreateVideoTaskRequest) error {
	if c == nil || c.baseURL == "" {
		return fmt.Errorf("video task client not configured")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/internal/v1/video/tasks", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Internal-Token", c.token)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("control-plane create video task: status=%d body=%s", resp.StatusCode, string(b))
	}
	return nil
}

// Update 增量更新任务状态和详情字段。
func (c *VideoTaskClient) Update(ctx context.Context, taskID string, req UpdateVideoTaskRequest) error {
	if c == nil || c.baseURL == "" {
		return fmt.Errorf("video task client not configured")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	u := fmt.Sprintf("%s/internal/v1/video/tasks/%s", c.baseURL, url.PathEscape(taskID))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPatch, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Internal-Token", c.token)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("control-plane update video task: status=%d body=%s", resp.StatusCode, string(b))
	}
	return nil
}

// UpdateStatus 更新任务状态（兼容旧调用方）。
func (c *VideoTaskClient) UpdateStatus(ctx context.Context, taskID, status string) error {
	return c.Update(ctx, taskID, UpdateVideoTaskRequest{Status: status})
}

// VideoTaskMeta 任务路由与埋点反查结果。
type VideoTaskMeta struct {
	Model        string
	ProviderType string
	ProviderName string
}

// LookupProviderType 查询任务对应的 provider_type。
func (c *VideoTaskClient) LookupProviderType(ctx context.Context, taskID string) (string, error) {
	meta, err := c.LookupTaskMeta(ctx, taskID)
	if err != nil {
		return "", err
	}
	if meta == nil {
		return "", nil
	}
	return meta.ProviderType, nil
}

// LookupTaskMeta 查询任务 model / provider_type / provider_name。
func (c *VideoTaskClient) LookupTaskMeta(ctx context.Context, taskID string) (*VideoTaskMeta, error) {
	if c == nil || c.baseURL == "" {
		return nil, fmt.Errorf("video task client not configured")
	}
	u := fmt.Sprintf("%s/internal/v1/video/tasks/%s/provider-type", c.baseURL, url.PathEscape(taskID))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("X-Internal-Token", c.token)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("control-plane lookup task meta: status=%d body=%s", resp.StatusCode, string(b))
	}
	var out struct {
		ProviderType string `json:"provider_type"`
		Model        string `json:"model"`
		ProviderName string `json:"provider_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &VideoTaskMeta{
		Model:        out.Model,
		ProviderType: out.ProviderType,
		ProviderName: out.ProviderName,
	}, nil
}

// ════════════════════════════════════════════════════════════════════════════
// 媒体任务平台客户端（Batch 1 · C 队 C3b）
//
// 冻结的内部 API 契约（与 C1 队同一份；**禁止单方面修改**）：
//
//	POST   /internal/v1/video/tasks             新建（201）/ 幂等命中（200）
//	PATCH  /internal/v1/video/tasks/:id         CAS 增量更新 → {updated, task}
//	PATCH  /internal/v1/video/tasks/:id/status  向后兼容（既有，不改）
//	GET    /internal/v1/video/tasks/:id/provider-type  向后兼容（既有，不改）
//	GET    /internal/v1/video/tasks/:id         取单条全量
//
// 幂等语义：(api_key_id, idempotency_key) 已存在且 key 非空 → **200 + 既有任务、不新建**；
// 新建成功 → **201**。调用方（handler）据此区分「幂等命中」（不得再打上游）与「新建成功」。
//
// 路径刻意沿用**既有** /internal/v1/video/tasks，不新建 /internal/v1/media-tasks：
// gateway 与 control-plane 可能分批部署，改路径会留下「新网关打旧控制面」的滚动窗口。
// （注意 tests/contract/contracts.json 的 media_channels.internal_api 段仍写着
// /internal/v1/media-tasks —— 那是设计文档阶段的路径，与冻结契约不一致，已在交付报告中列出。）
//
// 全部请求都带 X-Internal-Token（防护栏 S1）。
// ════════════════════════════════════════════════════════════════════════════

const videoTaskInternalPath = "/internal/v1/video/tasks"

// ErrMediaTaskNotFound 表示控制面明确回答「任务不存在」（HTTP 404）。
//
// 与「控制面不可达」区分开：前者是业务事实（handler 返回 404），
// 后者是基础设施故障（handler 必须返回 5xx），绝不能混为一谈。
var ErrMediaTaskNotFound = errors.New("control-plane: media task not found")

// ControlPlaneError 是控制面内部 API 返回非 2xx 时的结构化错误。
//
// handler 用它区分「4xx 契约不匹配」（大概率是两端契约漂移，需要人介入）
// 与「5xx / 网络故障」（可重试），日志里带上 Status 便于快速定位。
type ControlPlaneError struct {
	Op     string // 操作名，如 create_media_task
	Status int    // HTTP 状态码；0 表示网络/超时等传输层失败
	Body   string // 响应体前若干字节（截断）
	Err    error  // 传输层错误（Status=0 时非空）
}

func (e *ControlPlaneError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return fmt.Sprintf("control-plane %s: transport error: %v", e.Op, e.Err)
	}
	return fmt.Sprintf("control-plane %s: status=%d body=%s", e.Op, e.Status, e.Body)
}

func (e *ControlPlaneError) Unwrap() error { return e.Err }

// IsClientError 报告控制面是否以 4xx 拒绝了本次请求（契约不匹配信号）。
func (e *ControlPlaneError) IsClientError() bool {
	return e != nil && e.Status >= 400 && e.Status < 500
}

// JSONText 兼容 jsonb 列的两种线上表示：**JSON 字符串**（GORM 侧字段类型为 string）
// 与**裸 JSON 对象/数组**（字段类型为 datatypes.JSON / json.RawMessage）。
//
// 为什么需要：media_tasks 的 private_data / usage 是 jsonb 列，其 Go 字段类型由
// C1 队确定（设计文档 §7.1 提案为 string）。读取侧做成宽容的，可以避免
// 「对端换了字段类型就读不出任务」这类纯表示差异导致的故障；写入侧仍统一发字符串。
type JSONText string

func (t *JSONText) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		*t = ""
		return nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return err
		}
		*t = JSONText(s)
		return nil
	}
	*t = JSONText(string(trimmed))
	return nil
}

func (t JSONText) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(t))
}

// String 返回 JSON 文本（无内容时为空串）。
func (t JSONText) String() string { return string(t) }

// IsZero 供 encode/json 的 omitempty 判定（json.Marshaler 需要显式实现）。
func (t JSONText) IsZero() bool { return len(t) == 0 }

// MediaTask 是控制面任务全量视图（GET /internal/v1/video/tasks/:id 与 PATCH 响应里的 task）。
//
// 字段清单 = 既有 video_tasks 列 + C1 队新增的 delta 列（见冻结契约）。
// 全部标注 omitempty 之外的可选字段一律用零值容错：读侧不认识的新字段不会导致解码失败。
type MediaTask struct {
	TaskID       string `json:"task_id"`
	Kind         string `json:"kind"`
	Action       string `json:"action"`
	Status       string `json:"status"`
	Progress     string `json:"progress"`
	VendorTaskID string `json:"vendor_task_id"`

	Model         string `json:"model"`
	UpstreamModel string `json:"upstream_model"`
	ProviderName  string `json:"provider_name"`
	ProviderType  string `json:"provider_type"`

	APIKeyID   string `json:"api_key_id"`
	APIKeyName string `json:"api_key_name"`
	APIKeyType string `json:"api_key_type"`
	UserID     string `json:"user_id"`
	UserName   string `json:"user_name"`
	DeptID     string `json:"dept_id"`
	StudioID   string `json:"studio_id"`

	VideoURL     string `json:"video_url"`
	RequestBody  string `json:"request_body"`
	ResponseBody string `json:"response_body"`
	ErrorMessage string `json:"error_message"`

	Usage        JSONText   `json:"usage"`
	PrivateData  JSONText   `json:"private_data"`
	CostMicroUSD int64      `json:"cost_micro_usd"`
	BilledAt     *time.Time `json:"billed_at"`
	PollFailures int        `json:"poll_failures"`

	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

// MediaCreateRequest 是 POST /internal/v1/video/tasks 的请求体。
//
// 前 9 个字段是与既有控制面 InternalCreate 对齐的字段（不得改名）；
// 其后是冻结契约新增的字段。
type MediaCreateRequest struct {
	TaskID       string `json:"task_id"`
	APIKeyID     string `json:"api_key_id"`
	APIKeyName   string `json:"api_key_name"`
	APIKeyType   string `json:"api_key_type"`
	UserID       string `json:"user_id"`
	UserName     string `json:"user_name"`
	Model        string `json:"model"`
	ProviderName string `json:"provider_name"`
	ProviderType string `json:"provider_type"`
	Status       string `json:"status,omitempty"`
	RequestBody  string `json:"request_body,omitempty"`

	// ── 冻结契约新增字段 ──────────────────────────────────────────────────
	Kind           string `json:"kind,omitempty"`            // image | video
	Action         string `json:"action,omitempty"`          // text_to_video | image_to_video | ...
	VendorTaskID   string `json:"vendor_task_id,omitempty"`  // 上游任务 id（禁止返回给客户端）
	Progress       string `json:"progress,omitempty"`        // "0-100" 或阶段名
	IdempotencyKey string `json:"idempotency_key,omitempty"` // 幂等键（客户端 Idempotency-Key 头）
	// PrivateData 是 jsonb 列，线上必须是**裸 JSON 对象**，不能是 JSON 字符串。
	// 用 json.RawMessage 而不是 string：string 会被 json.Marshal 包成 "\"{...}\""
	// 的双层引号，而控制面按 map 绑定 → 解不开 → 整条创建请求 400（合并后实测确认）。
	PrivateData json.RawMessage `json:"private_data,omitempty"` // 渠道细节，禁止返回用户
	DeptID         string `json:"dept_id,omitempty"`
	StudioID       string `json:"studio_id,omitempty"`
}

// MediaCreateResult 是新建调用的结果。
type MediaCreateResult struct {
	StatusCode int        // 200（幂等命中）或 201（新建成功）
	Created    bool       // true = 201 新建；false = 200 幂等命中
	Task       *MediaTask // 控制面回传的任务（两端都应返回；缺失时为 nil）
}

// MediaUpdateRequest 是 PATCH /internal/v1/video/tasks/:id 的请求体。
//
// 既有字段（status/video_url/response_body/error_message）保持原样；
// 其后是冻结契约新增字段。指针字段用于表达「显式写 0 / 空」与「不写」的区别。
type MediaUpdateRequest struct {
	Status       string `json:"status,omitempty"`
	VideoURL     string `json:"video_url,omitempty"`
	ResponseBody string `json:"response_body,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`

	// ── 冻结契约新增字段 ──────────────────────────────────────────────────
	Progress       string `json:"progress,omitempty"`
	Usage          json.RawMessage `json:"usage,omitempty"` // jsonb：同 PrivateData，必须是裸对象
	CostMicroUSD   *int64 `json:"cost_micro_usd,omitempty"`
	BilledAt       string `json:"billed_at,omitempty"` // RFC3339
	PollFailures   *int   `json:"poll_failures,omitempty"`
	VendorTaskID   string `json:"vendor_task_id,omitempty"`
	ExpectedStatus string `json:"expected_status,omitempty"` // CAS：非空时仅当当前状态相等才更新
}

// MediaUpdateResult 是 PATCH 的响应（冻结契约 {updated, task}）。
//
// Updated=false 表示 CAS 未命中（状态已被其他写入者推进）——调用方**必须丢弃**
// 本次计算结果（包括用量与成本），否则会重复记账。
type MediaUpdateResult struct {
	Updated bool
	Task    *MediaTask
}

// CreateMediaTask 同步新建任务；写失败必须返回 error（禁止 fire-and-forget）。
//
// 返回值语义：Created=true（201）表示本次真的新建；Created=false（200）表示
// (api_key_id, idempotency_key) 已存在，调用方**不得**再调用上游。
func (c *VideoTaskClient) CreateMediaTask(ctx context.Context, req MediaCreateRequest) (*MediaCreateResult, error) {
	if c == nil || c.baseURL == "" {
		return nil, fmt.Errorf("video task client not configured")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+videoTaskInternalPath, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Internal-Token", c.token)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, &ControlPlaneError{Op: "create_media_task", Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, newControlPlaneError("create_media_task", resp)
	}
	task := decodeMediaTask(resp.Body)
	return &MediaCreateResult{
		StatusCode: resp.StatusCode,
		Created:    resp.StatusCode == http.StatusCreated,
		Task:       task,
	}, nil
}

// GetMediaTask 取单条任务全量；任务不存在时返回 ErrMediaTaskNotFound。
func (c *VideoTaskClient) GetMediaTask(ctx context.Context, taskID string) (*MediaTask, error) {
	if c == nil || c.baseURL == "" {
		return nil, fmt.Errorf("video task client not configured")
	}
	u := fmt.Sprintf("%s%s/%s", c.baseURL, videoTaskInternalPath, url.PathEscape(taskID))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("X-Internal-Token", c.token)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, &ControlPlaneError{Op: "get_media_task", Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrMediaTaskNotFound
	}
	if resp.StatusCode >= 300 {
		return nil, newControlPlaneError("get_media_task", resp)
	}
	task := decodeMediaTask(resp.Body)
	if task == nil {
		// 200 但没有任务体：当作协议失败，绝不返回「空任务」让上层误判。
		return nil, &ControlPlaneError{Op: "get_media_task", Status: resp.StatusCode, Body: "empty task body"}
	}
	return task, nil
}

// UpdateMediaTask CAS 增量更新任务；响应 {updated, task}。
func (c *VideoTaskClient) UpdateMediaTask(ctx context.Context, taskID string, req MediaUpdateRequest) (*MediaUpdateResult, error) {
	if c == nil || c.baseURL == "" {
		return nil, fmt.Errorf("video task client not configured")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	u := fmt.Sprintf("%s%s/%s", c.baseURL, videoTaskInternalPath, url.PathEscape(taskID))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPatch, u, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Internal-Token", c.token)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, &ControlPlaneError{Op: "update_media_task", Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrMediaTaskNotFound
	}
	if resp.StatusCode >= 300 {
		return nil, newControlPlaneError("update_media_task", resp)
	}
	result := &MediaUpdateResult{}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var env struct {
		Updated *bool `json:"updated"`
	}
	if err := json.Unmarshal(raw, &env); err == nil && env.Updated != nil {
		result.Updated = *env.Updated
	} else {
		// 缺少 updated 字段：保守地按「未确认更新」处理，避免重复记账。
		result.Updated = false
	}
	result.Task = decodeMediaTaskBytes(raw)
	return result, nil
}

// newControlPlaneError 读取（截断的）响应体并构造结构化错误。
func newControlPlaneError(op string, resp *http.Response) *ControlPlaneError {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	return &ControlPlaneError{Op: op, Status: resp.StatusCode, Body: strings.TrimSpace(string(b))}
}

func decodeMediaTask(r io.Reader) *MediaTask {
	raw, err := io.ReadAll(io.LimitReader(r, 1<<20))
	if err != nil {
		return nil
	}
	return decodeMediaTaskBytes(raw)
}

// decodeMediaTaskBytes 宽容解析任务体：优先 {task:{...}}，其次 {data:{...}}，最后裸任务对象。
//
// 宽容的理由：GET 单条的响应信封在冻结契约里只写了「取单条全量」，
// 而 PATCH 明确是 {updated, task}。两个端点由 C1 并行实现，读取侧同时接受
// 三种常见形状可以避免纯信封差异导致的整链路不可用。
func decodeMediaTaskBytes(raw []byte) *MediaTask {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil
	}
	var env struct {
		Task *MediaTask `json:"task"`
		Data *MediaTask `json:"data"`
	}
	if err := json.Unmarshal(trimmed, &env); err == nil {
		if env.Task != nil && env.Task.TaskID != "" {
			return env.Task
		}
		if env.Data != nil && env.Data.TaskID != "" {
			return env.Data
		}
	}
	var bare MediaTask
	if err := json.Unmarshal(trimmed, &bare); err == nil && bare.TaskID != "" {
		return &bare
	}
	return nil
}
