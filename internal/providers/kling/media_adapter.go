// Package kling 实现快手可灵（Kling）异步视频任务适配器。
//
// 上游契约（逐字对齐 New API 的 Go 实现
// `relay/channel/task/kling/adaptor.go`，本机可对拍）：
//
//   - 鉴权：HS256 JWT（iss=accessKey、exp=now+1800、nbf=now-5、签名密钥=secretKey），
//     请求头 `Authorization: Bearer <token>`；提交加 `Content-Type: application/json`，
//     查询加 `Accept: application/json`，两者都带 `User-Agent: kling-sdk/1.0`；
//   - 提交：`POST {base}/v1/videos/{text2video|image2video}`；
//   - 查询：`GET {base}/v1/videos/{text2video|image2video}/{task_id}`
//     （⚠️ 查询路径的 action 段**必须与提交时一致**，见 encodeVendorTaskID 的选型说明）；
//   - 状态：`submitted|processing|succeed|failed` → `queued|in_progress|completed|failed`，
//     未知状态词一律按协议失败返回 error（core/media_task.go 契约 2）；
//   - 产物：`data.task_result.videos[]`（数组，**多产物全取**，不是只取首个）；
//   - 失败原因：`data.task_status_msg`；用量：`data.final_unit_deduction`。
//
// 本文件只做「配置解析 + HTTP 收发 + 契约落地」；参数翻译与状态映射在 translate.go，
// 鉴权签名与凭据解析在 signer.go。
package kling

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

// MediaProviderType 是 Kling 渠道的 provider type，同时是媒体适配器注册表的键。
// 必须与控制面 `provider.type` 的取值逐字一致。
const MediaProviderType = "kling"

const (
	// defaultBaseURL 是 provider.endpoint 为空时的兜底地址（Kling 官方）。
	defaultBaseURL = "https://api.klingai.com"
	// userAgent 是上游要求的 UA（Kling SDK 约定）。
	userAgent = "kling-sdk/1.0"
	// requestTimeout 是单次上游调用的超时上限（提交与查询共用）。
	requestTimeout = 30 * time.Second
	// maxResponseBytes 限制单次响应体读取量，避免异常/恶意上游把网关内存打满。
	// Kling 的提交与查询响应都是 KB 级，1 MiB 有巨大余量。
	maxResponseBytes = 1 << 20
	// maxErrorBodyRunes 是错误信息里回显上游响应体的上限（按 rune 截断，不切坏 UTF-8）。
	maxErrorBodyRunes = 512
)

// adapter 是 core.MediaTaskProvider 的 Kling 实现。
//
// 它**无状态**（没有任何按任务累积的 map）：所有跨调用需要的信息都编码在 vendorTaskID 里，
// 因此网关多副本、蓝绿切换、进程重启都不会丢轮询路径（理由见 encodeVendorTaskID）。
type adapter struct {
	baseURL   string
	accessKey string
	secretKey string
	client    *http.Client
	// now 注入时钟，便于将来做确定性时间相关诊断（JWT 的 exp/nbf 都来自它）。
	now func() time.Time
}

// 编译期断言：Kling 适配器必须实现 core.MediaTaskProvider。
var _ core.MediaTaskProvider = (*adapter)(nil)

// NewMediaAdapter 构造 Kling 异步视频任务适配器（core.MediaTaskProvider）。
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
		baseURL:   baseURL,
		accessKey: creds.AccessKey,
		secretKey: creds.SecretKey,
		client:    &http.Client{Timeout: requestTimeout},
		now:       time.Now,
	}, nil
}

// normalizeBaseURL 归一上游 base URL：空 → 官方默认；非空必须是 http/https 绝对地址。
//
// 为什么构造期就校验：endpoint 写错（漏 scheme、写成主机名前缀）时，宁可在建表阶段
// 就构造失败并打出明确日志（该渠道不出现在注册表里 → 请求得到 501），也不要让每次
// 请求都在运行期变成 502。
func normalizeBaseURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return defaultBaseURL, nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("kling: endpoint 不是合法 URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("kling: endpoint 必须是 http/https 绝对地址，收到 %q", raw)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("kling: endpoint 缺少主机名，收到 %q", raw)
	}
	return strings.TrimRight(trimmed, "/"), nil
}

// Submit 翻译并提交上游任务，返回上游任务 id（编码了 action，见 encodeVendorTaskID）。
//
// 参数校验全部前置（表外参数 / action 支持性 / 图片数量 / mode / v2-master 不支持 std），
// 校验不过返回 400 类错误（core.NewInvalidRequestError），**不发上游请求**。
func (a *adapter) Submit(ctx context.Context, req *core.MediaSubmitRequest) (*core.MediaSubmitResult, error) {
	plan, err := buildSubmitPlan(req)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(plan.payload)
	if err != nil {
		return nil, fmt.Errorf("kling: 序列化提交请求体失败: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost, a.url("/v1/videos/"+string(plan.action)), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("kling: 构造提交请求失败: %w", err)
	}

	token, err := a.signToken()
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("User-Agent", userAgent)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("kling: 提交上游任务失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := readLimited(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("kling: 读取提交响应失败: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode > 299 {
		return nil, upstreamStatusError("提交上游任务", resp.StatusCode, respBody)
	}

	var envelope klingEnvelope
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return nil, fmt.Errorf("kling: 提交响应不是合法 JSON: %w", err)
	}
	if envelope.Code != 0 {
		return nil, fmt.Errorf("kling: 上游拒绝提交任务（code=%d, message=%s）",
			envelope.Code, strings.TrimSpace(envelope.Message))
	}

	upstreamTaskID := strings.TrimSpace(envelope.Data.TaskID)
	if upstreamTaskID == "" {
		// 契约 3：拿不到上游 id 就不能落库（伪造一个对外 id 会让任务永远查不到上游）。
		return nil, fmt.Errorf("kling: 上游提交响应缺少 data.task_id（协议失败）")
	}

	// 初始状态：上游一般回 submitted；缺省按 queued（core.MediaSubmitResult 注释）。
	status := core.MediaStatusQueued
	if rawStatus := strings.TrimSpace(envelope.Data.TaskStatus); rawStatus != "" {
		status, err = mapTaskStatus(rawStatus)
		if err != nil {
			return nil, err
		}
	}

	return &core.MediaSubmitResult{
		VendorTaskID: encodeVendorTaskID(plan.action, upstreamTaskID),
		Status:       status,
	}, nil
}

// Query 查询并归一上游任务状态。
//
// 契约（core/media_task.go 文件头）：
//   - 业务失败（上游 failed）→ `(*MediaTaskStatus{Status: failed, FailReason: ...}, nil)`，
//     **不返回 error**（返回 error 会被上层重试放大成重复计费）；
//   - 任务不存在（HTTP 404）→ `(nil, 包装了 core.ErrMediaTaskNotFound 的 error)`；
//   - 其它传输/协议失败（非 2xx、非零 code、未知状态词、succeed 却无产物）→ `(nil, error)`。
func (a *adapter) Query(ctx context.Context, vendorTaskID string) (*core.MediaTaskStatus, error) {
	action, upstreamTaskID, err := decodeVendorTaskID(vendorTaskID)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodGet,
		a.url("/v1/videos/"+string(action)+"/"+url.PathEscape(upstreamTaskID)), nil)
	if err != nil {
		return nil, fmt.Errorf("kling: 构造查询请求失败: %w", err)
	}

	token, err := a.signToken()
	if err != nil {
		return nil, err
	}

	// 查询不加 Content-Type（与 New API 的 FetchTask 一致）：GET 无请求体。
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("User-Agent", userAgent)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("kling: 查询上游任务失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := readLimited(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("kling: 读取查询响应失败: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		// 上游明确回答「不存在」：单独归类，调用方据此停止无意义轮询。
		return nil, fmt.Errorf("kling: 上游任务不存在（HTTP 404, action=%s）: %w",
			action, core.ErrMediaTaskNotFound)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode > 299 {
		return nil, upstreamStatusError("查询上游任务", resp.StatusCode, respBody)
	}

	var envelope klingEnvelope
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return nil, fmt.Errorf("kling: 查询响应不是合法 JSON: %w", err)
	}
	if envelope.Code != 0 {
		return nil, fmt.Errorf("kling: 上游查询返回错误（code=%d, message=%s）",
			envelope.Code, strings.TrimSpace(envelope.Message))
	}

	status, err := mapTaskStatus(envelope.Data.TaskStatus)
	if err != nil {
		return nil, err
	}

	result := &core.MediaTaskStatus{Status: status}
	switch status {
	case core.MediaStatusCompleted:
		result.Artifacts = buildArtifacts(envelope.Data.TaskResult.Videos)
		if len(result.Artifacts) == 0 {
			// core 契约：completed 时 Artifacts 必须非空。上游说 succeed 却给不出产物，
			// 是协议失败：返回 completed 会让上层落库一个「成功但没有交付物」的任务
			// （还会按用量记账），比让轮询失败更糟。
			return nil, fmt.Errorf("kling: 上游返回 succeed 但 data.task_result.videos 为空（协议失败）")
		}
	case core.MediaStatusFailed:
		result.FailReason = strings.TrimSpace(envelope.Data.TaskStatusMsg)
		if result.FailReason == "" {
			// core 契约：failed 时 FailReason 必须非空。
			result.FailReason = "kling: 上游返回 failed 但未提供 data.task_status_msg"
		}
	default:
		// queued / in_progress：无产物、无失败原因。
	}

	// 用量只在终态采集：中间态的 final_unit_deduction 不是最终扣减，记进去会误导对账。
	if status.IsTerminal() {
		result.Usage = buildUsage(envelope.Data)
	}
	return result, nil
}

// signToken 签发本次调用的鉴权 JWT。
func (a *adapter) signToken() (string, error) {
	return signJWT(a.accessKey, a.secretKey, a.now())
}

// url 拼接上游地址（baseURL 在构造期已去掉尾部 '/'）。
func (a *adapter) url(path string) string {
	return a.baseURL + path
}

// readLimited 读取响应体，上限 maxResponseBytes。
func readLimited(body io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(body, maxResponseBytes))
}

// upstreamStatusError 构造非 2xx 的传输/协议错误。
//
// 文案里**保留 HTTP 状态码数字**：调用方的失败分类器（classifyMediaPollError）会按
// 4xx/5xx 数字做 auth/transient 归类，带上状态码能让分类与排障都不依赖上游文案。
func upstreamStatusError(op string, statusCode int, body []byte) error {
	return fmt.Errorf("kling: %s 失败: HTTP %d: %s", op, statusCode, truncateForError(body))
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
