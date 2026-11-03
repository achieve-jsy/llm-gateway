package service

import (
	"context"
	"strings"

	"github.com/example/llm-gateway/internal/providers"
	"github.com/example/llm-gateway/internal/providers/vertex"
)

// VertexProbeResult Google Vertex (native) 共享探针结果。
type VertexProbeResult struct {
	Success      bool
	ResponseTime int
	StatusCode   int
	Message      string
}

// ProbeVertexGenerate 对 Google Vertex 原生面做一次 generateContent 调用。
//
// 与 ProbeAnthropicMessages / ProbeGeminiGenerate 同理：走真实 adapter，而不是
// 在这份探针里重新拼一遍 generateContent 的 URL、鉴权头和请求体。
// adapter 会把 {endpoint}/publishers/google 解析成 native base（VertexBaseURLs），
// 再拼 /models/{model}:generateContent —— 与线上转发同一条代码路径。
//
// 探针一次调用同时验证：网络可达 + Key 有效 + 模型名正确。
//
// 有意不继承 adapter 的重试与熔断：探测必须快速失败，重试会把一次超时
// 放大成三次，渠道判定被拖长。
func ProbeVertexGenerate(ctx context.Context, endpoint, apiKey, model string) VertexProbeResult {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	model = strings.TrimSpace(model)
	if endpoint == "" || model == "" {
		return VertexProbeResult{Success: false, Message: "vertex probe: endpoint and model are required"}
	}

	p := vertex.New(providers.ProviderConfig{
		BaseURL: endpoint,
		APIKey:  apiKey,
	}, providers.ProviderOptions{})

	ok, elapsed, errMsg := probeViaAdapter(ctx, p, model)
	if !ok {
		return VertexProbeResult{Success: false, ResponseTime: elapsed, Message: errMsg}
	}
	return VertexProbeResult{
		Success:      true,
		ResponseTime: elapsed,
		Message:      "ok: generateContent via adapter",
	}
}
