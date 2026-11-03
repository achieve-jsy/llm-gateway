package gemini

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/llmclient"
)

// NativeRelayFormatFor 报告 model 能否零转换接收 Gemini generateContent 请求体。
//
// model 可能是对外模型名、组名或 Vertex publisher 路径
// （projects/p/locations/x/publishers/google/models/gemini-2.5-pro），
// 统一经 normalizeGeminiModelID 归一后再按 "gemini-" 前缀判定：
// claude / gpt 等非 Gemini 模型即使挂在本渠道上也不能声明原生。
//
// 注意与 RelayNative 的分工：请求体里的 model 已由 Router 改写成上游 ID，
// 这里只做"能不能走原生"的分流判断，不改写任何调用方数据。
func (p *Provider) NativeRelayFormatFor(model string, format core.NativeRelayFormat) (core.NativeRelayFormat, bool) {
	if format != core.NativeRelayGemini {
		return "", false
	}
	if strings.TrimSpace(model) == "" {
		return "", false
	}
	// 只认 generateContent 系的 gemini-*；text-embedding-* 走不了该端点。
	if !strings.HasPrefix(normalizeGeminiModelID(model), "gemini-") {
		return "", false
	}
	return core.NativeRelayGemini, true
}

// RelayNative 以原生 Gemini 请求体调用 :generateContent，返回上游原始响应体。
// 不做任何协议转换：contents / generationConfig / tools / safetySettings 等
// 原样透传，这正是本次改造的目的。
func (p *Provider) RelayNative(ctx context.Context, body []byte) ([]byte, error) {
	if err := p.ready(); err != nil {
		return nil, err
	}
	model, payload, err := p.buildNativeRelayBody(body)
	if err != nil {
		return nil, err
	}

	resp, err := p.nativeClient.DoRaw(ctx, llmclient.Request{
		Method:   http.MethodPost,
		Endpoint: nativeGenerateEndpoint(model),
		RawBody:  payload,
		Headers:  nativeRelayHeaders(false),
	})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// RelayNativeStream 以原生 Gemini 请求体调用 :streamGenerateContent?alt=sse。
// Gemini 的 SSE 本身就是标准 data: 帧，直接返回上游 body 即可；
// 调用方 Close 会关闭底层连接，这里不额外起 goroutine，也就没有泄漏。
func (p *Provider) RelayNativeStream(ctx context.Context, body []byte) (io.ReadCloser, error) {
	if err := p.ready(); err != nil {
		return nil, err
	}
	model, payload, err := p.buildNativeRelayBody(body)
	if err != nil {
		return nil, err
	}

	// DoStream 在非 200 时读完错误体并返回 provider error，不会给调用方空流。
	return p.nativeClient.DoStream(ctx, llmclient.Request{
		Method:   http.MethodPost,
		Endpoint: nativeStreamEndpoint(model),
		RawBody:  payload,
		Headers:  nativeRelayHeaders(true),
	})
}

// buildNativeRelayBody 抽出 model 拼 URL，并把 model 字段从请求体里删掉。
//
// Gemini 的模型名只存在于 URL 路径中，请求体不认识顶层 model 字段；
// 而 Router 为了路由一定会把 model 写进 body，所以这里必须摘掉，否则
// 上游会因未知字段直接 400。
//
// 用 map[string]json.RawMessage 而不是 map[string]any：数字、嵌套结构都按
// 原始字节搬运，topK/topP 之类不会被 float64 往返改写。
func (p *Provider) buildNativeRelayBody(body []byte) (string, []byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return "", nil, core.NewInvalidRequestError("gemini native relay body must be a JSON object: "+err.Error(), err)
	}
	if fields == nil {
		return "", nil, core.NewInvalidRequestError("gemini native relay body must be a JSON object", nil)
	}

	var model string
	if raw, ok := fields["model"]; ok {
		if err := json.Unmarshal(raw, &model); err != nil {
			return "", nil, core.NewInvalidRequestError("gemini native relay body has a non-string model", err)
		}
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return "", nil, core.NewInvalidRequestError("gemini native relay body requires a model", nil)
	}
	delete(fields, "model")

	payload, err := json.Marshal(fields)
	if err != nil {
		return "", nil, core.NewInvalidRequestError("gemini native relay body marshal failed: "+err.Error(), err)
	}
	return model, payload, nil
}

// nativeRelayHeaders 补齐 Content-Type（RawBody 不会被 llmclient 自动设置）。
func nativeRelayHeaders(stream bool) http.Header {
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	if stream {
		h.Set("Accept", "text/event-stream")
	}
	return h
}

var _ core.NativeRelayProvider = (*Provider)(nil)
