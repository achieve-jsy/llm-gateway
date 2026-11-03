// Package vidu 实现生数科技 Vidu 异步视频任务适配器。
//
// 上游契约（逐字对齐 New API 的 Go 实现 `relay/channel/task/vidu/adaptor.go`，
// 与设计文档 docs/media-channels-integration-design.md §3.2 一致，本机可对拍）：
//
//   - 鉴权：请求头 `Authorization: Token <apiKey>`（⚠️ 是 `Token ` 前缀，**不是** `Bearer `）；
//   - 提交：`POST {base}/ent/v2/{text2video|img2video|start-end2video|reference2video}`，
//     `Content-Type: application/json` + `Accept: application/json`；
//   - 查询：`GET {base}/ent/v2/tasks/{task_id}/creations`
//     （⚠️ 路径里**没有 action 段**，所以 vendorTaskID 就是逐字的上游 id，
//     理由见 translate.go 的 normalizeVendorTaskID 注释）；
//   - 状态：`created|queueing|processing|success|failed`
//     → `queued|queued|in_progress|completed|failed`，未知状态词一律协议失败返回 error
//     （core/media_task.go 契约 2）；
//   - 产物：`creations[]`（数组，**多产物全取**，不是只取首个）；失败原因：`err_code`；
//   - 用量：上游**不保证**返回 credits——没有就留 nil，绝不伪造（见 buildUsage）。
//
// 本文件只做「配置解析 + HTTP 收发 + 契约落地」；参数翻译与状态映射在 translate.go，
// 凭据解析在 credentials.go。
package vidu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers"
)

// MediaProviderType 是 Vidu 渠道的 provider type，同时是媒体适配器注册表的键。
// 必须与控制面 `provider.type` 的取值逐字一致。
const MediaProviderType = "vidu"

const (
	// defaultBaseURL 是 provider.endpoint 为空时的兜底地址（Vidu 官方 API 根，
	// 与 New API 的 `constant/channel.go:123` 一致）。
	defaultBaseURL = "https://api.vidu.cn"
	// apiPrefix 是 Vidu 开放 API 的固定路径前缀（提交与查询共用）。
	apiPrefix = "/ent/v2"
	// requestTimeout 是单次上游调用的超时上限（提交与查询共用）。
	requestTimeout = 30 * time.Second
	// maxResponseBytes 限制单次响应体读取量，避免异常/恶意上游把网关内存打满。
	// Vidu 的提交/查询响应都是 KB 级，1 MiB 有巨大余量（data URL 是**请求**方向，
	// 不经过这里）。
	maxResponseBytes = 1 << 20
	// maxErrorBodyRunes 是错误信息里回显上游响应体的上限（按 rune 截断，不切坏 UTF-8）。
	maxErrorBodyRunes = 512
)

// adapter 是 core.MediaTaskProvider 的 Vidu 实现。
//
// 它**无状态**（没有任何按任务累积的 map）：Vidu 的查询路径只由上游 task_id 决定，
// 因此不需要像 Kling 那样把 action 编码进 vendorTaskID，也不需要任何锁或缓存
// （对比说明见 translate.go 的 normalizeVendorTaskID）。
type adapter struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// 编译期断言：Vidu 适配器必须实现 core.MediaTaskProvider。
var _ core.MediaTaskProvider = (*adapter)(nil)

// NewMediaAdapter 构造 Vidu 异步视频任务适配器（core.MediaTaskProvider）。
//
// 冻结约定（providers.MediaAdapterBuilder）：
//   - 凭证缺失/非法 → 返回 error，**不返回占位适配器**（占位凭证会真的打上游并产生费用）；
//   - 构造失败不 panic，也不返回 (nil, nil)；
//   - 只做「解析配置 + 建 HTTP client」，**不发任何网络请求**。
func NewMediaAdapter(params providers.MediaAdapterParams) (core.MediaTaskProvider, error) {
	creds, err := parseCredentials(params.Credentials)
	if err != nil {
		return nil, err
	}
	baseURL, err := normalizeBaseURL(params.BaseURL)
	if err != nil {
		return nil, err
	}

	return &adapter{
		baseURL: baseURL,
		apiKey:  creds.APIKey,
		client:  &http.Client{Timeout: requestTimeout},
	}, nil
}

// normalizeBaseURL 归一上游 base URL：空 → 官方默认；非空必须是 http/https 绝对地址。
//
// 为什么构造期就校验：endpoint 写错（漏 scheme、写成主机名前缀）时，宁可在建表阶段
// 就构造失败并打出明确日志（该渠道不出现在注册表里 → 请求得到 501），也不要让每次
// 请求都在运行期变成 502。
//
// 额外容错：允许运维把完整 API 前缀也写进 endpoint（`https://api.vidu.cn/ent/v2`），
// 归一后去掉它。理由：Kling 的 endpoint 是纯根地址，运维很容易按「文档里的调用地址」
// 直接粘贴 `/ent/v2`（New API 的 `BuildRequestURL` 就是 `baseURL + "/ent/v2" + path`，
// 抄文档时极易带上），而重复前缀只会得到一个上游 404——这类配置错误从报文里极难看出。
// 该处理是幂等的：无论有没有前缀，最终拼接结果都只有一个 `/ent/v2`。
func normalizeBaseURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return defaultBaseURL, nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("vidu: endpoint 不是合法 URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("vidu: endpoint 必须是 http/https 绝对地址，收到 %q", raw)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("vidu: endpoint 缺少主机名，收到 %q", raw)
	}
	base := strings.TrimRight(trimmed, "/")
	base = strings.TrimSuffix(base, apiPrefix)
	return strings.TrimRight(base, "/"), nil
}

// Submit 翻译并提交上游任务，返回**逐字的上游任务 id**（Vidu 不需要编码，见 translate.go）。
//
// 参数校验全部前置（7 键 fail-closed / action 与图片张数一致性 / 模型家族约束 /
// data URL 字节上限），校验不过返回 400 类错误（core.NewInvalidRequestError），
// **不发上游请求**——这一点由单测用「假上游收到 0 个请求」断言。
func (a *adapter) Submit(ctx context.Context, req *core.MediaSubmitRequest) (*core.MediaSubmitResult, error) {
	plan, err := buildSubmitPlan(req)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(plan.payload)
	if err != nil {
		return nil, fmt.Errorf("vidu: 序列化提交请求体失败: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost, a.url(apiPrefix+"/"+string(plan.action)), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("vidu: 构造提交请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", "Token "+a.apiKey)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("vidu: 提交上游任务失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := readLimited(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("vidu: 读取提交响应失败: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode > 299 {
		return nil, upstreamStatusError("提交上游任务", resp.StatusCode, respBody)
	}

	var task viduTaskResponse
	if err := json.Unmarshal(respBody, &task); err != nil {
		return nil, fmt.Errorf("vidu: 提交响应不是合法 JSON: %w", err)
	}

	upstreamTaskID := strings.TrimSpace(task.TaskID)
	if upstreamTaskID == "" {
		// 契约 3：拿不到上游 id 就不能落库（伪造一个对外 id 会让任务永远查不到上游）。
		return nil, fmt.Errorf("vidu: 上游提交响应缺少 task_id（协议失败）")
	}

	// 初始状态：上游一般回 created/queueing；缺省按 queued（core.MediaSubmitResult 注释）。
	//
	// ⚠️ 与 Kling 相同的取舍：**未知**初始状态严格报错（与 Query 共用同一张表、同一条契约 2）。
	// 代价是此时上游任务其实已经创建（可能已计费），错误会让上层按「提交失败」返回 5xx，
	// 客户端重试会产生第二个上游任务。仍然严格的原因：出现未枚举状态属于真正的协议漂移，
	// 静默降级成 queued 会把它藏起来；重复计费由 handler 的
	// 「提交路径 WithNoRetry + Idempotency-Key 预占」兜底。
	//
	// 另一处刻意行为：上游若在**提交响应**里直接回 `state=failed`（内容审核当场拒绝等），
	// 本适配器把它按状态映射表返回 `Status=failed`（**不**返回 error）。理由：
	//   - 上游任务已存在且已有 task_id，返回 error 会让客户端重试并**再建一个上游任务**；
	//   - MediaSubmitResult 没有 FailReason 字段，业务失败语义只能靠状态表达
	//     （core/media_task.go 契约 1 的精神：业务失败推进到终态，而不是当传输失败重试）；
	//   - 调用方仍可在后续 Query 里拿到 err_code 作为失败原因。
	// New API 在这里返回 400（`adaptor.go:178-181`），是它与本实现的已知差异。
	status := core.MediaStatusQueued
	if rawStatus := strings.TrimSpace(task.State); rawStatus != "" {
		status, err = mapTaskStatus(rawStatus)
		if err != nil {
			return nil, err
		}
	}

	return &core.MediaSubmitResult{
		VendorTaskID: upstreamTaskID,
		Status:       status,
	}, nil
}

// Query 查询并归一上游任务状态。
//
// 契约（core/media_task.go 文件头）：
//   - 业务失败（上游 failed）→ `(*MediaTaskStatus{Status: failed, FailReason: err_code}, nil)`，
//     **不返回 error**（返回 error 会被上层重试放大成重复计费）；
//   - 任务不存在（HTTP 404）→ `(nil, 包装了 core.ErrMediaTaskNotFound 的 error)`；
//   - 其它传输/协议失败（非 2xx、非法 JSON、未知状态词、success 却无产物）→ `(nil, error)`。
func (a *adapter) Query(ctx context.Context, vendorTaskID string) (*core.MediaTaskStatus, error) {
	upstreamTaskID, err := normalizeVendorTaskID(vendorTaskID)
	if err != nil {
		return nil, err
	}

	// 查询路径不含 action 段（这是本渠道与 Kling 最大的结构差异，见 normalizeVendorTaskID）。
	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodGet,
		a.url(apiPrefix+"/tasks/"+url.PathEscape(upstreamTaskID)+"/creations"), nil)
	if err != nil {
		return nil, fmt.Errorf("vidu: 构造查询请求失败: %w", err)
	}
	// 查询不加 Content-Type（GET 无请求体，与 New API 的 FetchTask 一致）。
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", "Token "+a.apiKey)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("vidu: 查询上游任务失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := readLimited(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("vidu: 读取查询响应失败: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		// 上游明确回答「不存在」：单独归类，调用方据此停止无意义轮询。
		return nil, fmt.Errorf("vidu: 上游任务不存在（HTTP 404）: %w", core.ErrMediaTaskNotFound)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode > 299 {
		return nil, upstreamStatusError("查询上游任务", resp.StatusCode, respBody)
	}

	var task viduTaskResponse
	if err := json.Unmarshal(respBody, &task); err != nil {
		return nil, fmt.Errorf("vidu: 查询响应不是合法 JSON: %w", err)
	}

	status, err := mapTaskStatus(task.State)
	if err != nil {
		return nil, err
	}

	result := &core.MediaTaskStatus{
		Status:   status,
		Progress: progressText(task.Progress),
	}
	switch status {
	case core.MediaStatusCompleted:
		result.Artifacts = buildArtifacts(task.Creations)
		if len(result.Artifacts) == 0 {
			// core 契约：completed 时 Artifacts 必须非空。上游说 success 却给不出产物，
			// 是协议失败：返回 completed 会让上层落库一个「成功但没有交付物」的任务
			// （还会按用量记账），比让轮询失败更糟。
			return nil, fmt.Errorf("vidu: 上游返回 success 但 creations 里没有可用 url（协议失败）")
		}
	case core.MediaStatusFailed:
		result.FailReason = strings.TrimSpace(task.ErrCode)
		if result.FailReason == "" {
			// core 契约：failed 时 FailReason 必须非空。
			result.FailReason = "vidu: 上游返回 failed 但未提供 err_code"
		}
	default:
		// queued / in_progress：无产物、无失败原因。
	}

	// 用量只在终态采集：中间态的 credits/duration 不是最终扣减，记进去会误导对账。
	if status.IsTerminal() {
		result.Usage = buildUsage(task)
	}
	return result, nil
}

// url 拼接上游地址（baseURL 在构造期已归一，且不含尾部 '/' 与重复的 /ent/v2）。
func (a *adapter) url(path string) string {
	return a.baseURL + path
}

// readLimited 读取响应体，上限 maxResponseBytes。
func readLimited(body io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(body, maxResponseBytes))
}

// upstreamStatusError 构造非 2xx 的传输/协议错误。
//
// 文案里**保留 HTTP 状态码数字**：调用方的失败分类器会按 4xx/5xx 数字做 auth/transient
// 归类，带上状态码能让分类与排障都不依赖上游文案（Vidu 的错误体形态未核实，
// 更不能依赖它）。
func upstreamStatusError(op string, statusCode int, body []byte) error {
	return fmt.Errorf("vidu: %s 失败: HTTP %d: %s", op, statusCode, truncateForError(body))
}

// truncateForError 截断上游响应体用于错误文案（按 rune 截断，避免出现非法 UTF-8）。
func truncateForError(body []byte) string {
	text := strings.TrimSpace(string(body))
	runes := []rune(text)
	if len(runes) <= maxErrorBodyRunes {
		return text
	}
	return string(runes[:maxErrorBodyRunes]) + "…(已截断)"
}
