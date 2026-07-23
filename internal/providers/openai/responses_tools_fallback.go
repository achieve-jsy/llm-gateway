package openai

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers"
)

// responsesRequiredHint 上游要求改用 Responses API 的提示词。
//
// Azure OpenAI 的推理类部署（如 gpt-6-astra）在 /chat/completions 上带 function tools
// 时会返回 400：
//
//	Function tools with reasoning_effort are not supported for <deployment> in
//	/v1/chat/completions. To use function tools, use /v1/responses or set
//	reasoning_effort to 'none'.
//
// 这类部署同时不接受 none/minimal 的 reasoning_effort，客户端无法自救，只能由网关
// 改走 /responses —— 即 chat_via_responses.go 里的 Chat→Responses 桥接。
const responsesRequiredHint = "/v1/responses"

// IsResponsesRequiredForToolsError 判断上游错误是否为「带 function tools 必须改走
// Responses API」。只在 400 且文案同时提及 tool 与 /v1/responses 时成立，避免把其它
// 400（如参数非法、上下文超限）也当成可重试情形。
func IsResponsesRequiredForToolsError(err error) bool {
	if err == nil {
		return false
	}
	var gwErr *core.GatewayError
	if !errors.As(err, &gwErr) {
		return false
	}
	if gwErr.StatusCode != http.StatusBadRequest {
		return false
	}
	text := strings.ToLower(gwErr.Message)
	if text == "" && len(gwErr.Body) > 0 {
		text = strings.ToLower(string(gwErr.Body))
	}
	if !strings.Contains(text, "tool") {
		return false
	}
	return strings.Contains(text, responsesRequiredHint)
}

// markToolsRequireResponses 记住「该部署的 function tools 只能走 /responses」。
// 记忆按模型名（即上游部署名）维度保存在 Provider 实例内；Provider 会随配置热重载
// 重建，重新付出一次 400 探测是安全的，不会长期固化错误结论。
func (p *CompatibleProvider) markToolsRequireResponses(model string) {
	if p == nil || strings.TrimSpace(model) == "" {
		return
	}
	p.responsesForTools.Store(model, struct{}{})
}

// toolsRequireResponses 查询该模型是否已知需要走 Responses API 才能用工具。
func (p *CompatibleProvider) toolsRequireResponses(model string) bool {
	if p == nil || strings.TrimSpace(model) == "" {
		return false
	}
	_, ok := p.responsesForTools.Load(model)
	return ok
}

// bridgeChatToResponses 非流式：改走 Responses API 并转换回 Chat 响应。
// reason 仅用于观测，区分「按已知结论直接桥接」与「撞到上游 400 后自愈重试」。
func (p *CompatibleProvider) bridgeChatToResponses(ctx context.Context, req *core.ChatRequest, reason string) (*core.ChatResponse, error) {
	slog.InfoContext(ctx, "chat via responses bridge",
		"provider", p.providerName,
		"model", req.Model,
		"protocol", core.UpstreamProtocolResponsesBridge,
		"tools", len(req.Tools),
		"reason", reason,
	)
	ctx = core.WithUpstreamProtocol(ctx, core.UpstreamProtocolResponsesBridge)
	return providers.ChatViaResponses(ctx, p, req)
}

// bridgeStreamChatToResponses 流式：改走 Responses API 并转换回 Chat SSE。
func (p *CompatibleProvider) bridgeStreamChatToResponses(ctx context.Context, req *core.ChatRequest, reason string) (io.ReadCloser, error) {
	slog.InfoContext(ctx, "stream chat via responses bridge",
		"provider", p.providerName,
		"model", req.Model,
		"protocol", core.UpstreamProtocolResponsesBridge,
		"tools", len(req.Tools),
		"reason", reason,
	)
	ctx = core.WithUpstreamProtocol(ctx, core.UpstreamProtocolResponsesBridge)
	return providers.StreamChatViaResponses(ctx, p, req, p.providerName)
}
