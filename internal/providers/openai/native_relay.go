package openai

// openai-compatible 渠道的原生旁路：把客户端原始 body 逐字节直发上游，
// 响应体/SSE 也逐字节回传，不塌缩进 core.ChatRequest/ChatResponse。
//
// 端点与 passthrough 语义（passthrough_semantics.go）保持一致：
// /chat/completions（Chat Completions）与 /responses（Responses）。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/llmclient"
)

// 原生旁路的两个上游端点。命名与 passthrough 语义标注保持一致。
const (
	nativeChatEndpoint      = "/chat/completions"
	nativeResponsesEndpoint = "/responses"
)

// NativeRelayFormatFor 报告能否零转换接收 OpenAI 协议请求体。
//
// openai-compatible 渠道本质就说 OpenAI 协议，Chat Completions 与 Responses
// 两种 body 都能原样吃下；Claude / Gemini 需要转换，返回 false 让 handler 降级。
func (p *CompatibleProvider) NativeRelayFormatFor(model string, format core.NativeRelayFormat) (core.NativeRelayFormat, bool) {
	switch format {
	case core.NativeRelayOpenAIChat, core.NativeRelayOpenAIResponses:
	default:
		return "", false
	}
	// 已知该部署的 function tools 只能在 /responses 上用（见 responses_tools_fallback.go）：
	// 原生直发 chat 只会重复撞一次 400，交给 Chat 路径的自愈桥接。
	if p.toolsRequireResponses(strings.TrimSpace(model)) {
		return "", false
	}
	return format, true
}

// RelayNative 以原生 OpenAI 请求体调用上游，返回上游原始响应体。
func (p *CompatibleProvider) RelayNative(ctx context.Context, body []byte) ([]byte, error) {
	endpoint, payload, err := p.prepareNativeRelay(body)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.DoRaw(withNativeProtocol(ctx, endpoint), p.prepareRequest(llmclient.Request{
		Method:   http.MethodPost,
		Endpoint: endpoint,
		RawBody:  payload,
		Headers:  nativeRelayHeaders(),
	}))
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// RelayNativeStream 以原生 OpenAI 请求体调用上游，返回上游原始 SSE 流。
func (p *CompatibleProvider) RelayNativeStream(ctx context.Context, body []byte) (io.ReadCloser, error) {
	endpoint, payload, err := p.prepareNativeRelay(body)
	if err != nil {
		return nil, err
	}
	stream, err := p.client.DoStream(withNativeProtocol(ctx, endpoint), p.prepareRequest(llmclient.Request{
		Method:   http.MethodPost,
		Endpoint: endpoint,
		RawBody:  payload,
		Headers:  nativeRelayHeaders(),
	}))
	if err != nil {
		return nil, err
	}
	return stream, nil
}

// prepareNativeRelay 定稿要发给上游的端点与 body（不做协议转换，只做必要兜底）。
func (p *CompatibleProvider) prepareNativeRelay(body []byte) (string, []byte, error) {
	endpoint, err := nativeOpenAIPath(body)
	if err != nil {
		return "", nil, err
	}
	// 旧路径对「tools + reasoning_effort」这类请求会桥到 /responses；原生直发拿不到
	// 同等结果（会 400），因此直接声明不支持、让 handler 降级回原路径。
	// 宁可少一次零转换，也不悄悄改变请求语义。
	if p.nativeNeedsChatBridge(body) {
		return "", nil, fmt.Errorf("%w: request needs chat-to-responses bridge", core.ErrNativeNotSupported)
	}
	// azure 出站清洗与 ChatCompletion 保持一致（新模型拒绝 max_tokens、剥离 Responses 别名）。
	if p.providerName == "azure" {
		body = sanitizeNativeAzureBody(body)
	}
	return endpoint, body, nil
}

// nativeOpenAIPath 按 body 形状判定上游端点，判定失败的调用方应降级。
//
// 接口只有一个 RelayNative，客户端也不会告知「我要 Chat 还是 Responses」，
// 只能看 body：有 messages → Chat Completions；有 input（无 messages）→ Responses。
func nativeOpenAIPath(body []byte) (string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return "", core.NewInvalidRequestError("native relay body must be a JSON object: "+err.Error(), err)
	}
	if fields == nil {
		return "", core.NewInvalidRequestError("native relay body is not a JSON object", nil)
	}
	if _, ok := fields["messages"]; ok {
		return nativeChatEndpoint, nil
	}
	if _, ok := fields["input"]; ok {
		return nativeResponsesEndpoint, nil
	}
	return "", core.NewInvalidRequestError("native relay body has neither messages nor input", nil)
}

// nativeNeedsChatBridge 复用 ChatCompletion 的桥接判定：这类请求只有桥接路径能成功。
func (p *CompatibleProvider) nativeNeedsChatBridge(body []byte) bool {
	var probe core.ChatRequest
	if err := json.Unmarshal(body, &probe); err != nil {
		return false
	}
	return ShouldBridgeChatToResponsesForProvider(p.providerName, &probe)
}

// sanitizeNativeAzureBody 在原始 body 上套用 azure 出站清洗。
func sanitizeNativeAzureBody(body []byte) []byte {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return body
	}
	out, err := json.Marshal(sanitizeAzureRawMap(raw))
	if err != nil {
		return body
	}
	return out
}

// nativeRelayHeaders 补齐 RawBody 请求缺少的 Content-Type。
func nativeRelayHeaders() http.Header {
	headers := http.Header{}
	headers.Set("Content-Type", "application/json")
	return headers
}

// withNativeProtocol 给 chat 形态的原生调用打上协议标注，和 ChatCompletion 对齐。
func withNativeProtocol(ctx context.Context, endpoint string) context.Context {
	if endpoint == nativeChatEndpoint {
		return core.WithUpstreamProtocol(ctx, core.UpstreamProtocolChat)
	}
	return ctx
}

var (
	_ core.NativeRelayProvider = (*CompatibleProvider)(nil)
	// 指针嵌入的 provider（openai / openrouter / azure）同样具备原生能力。
	_ core.NativeRelayProvider = (*Provider)(nil)
)
