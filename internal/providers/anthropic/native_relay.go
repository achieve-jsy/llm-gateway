package anthropic

import (
	"context"
	"io"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers"
)

// anthropic 渠道的原生旁路：/v1/messages 入站与上游同协议，零转换直发。
//
// 这是「原生旁路」最该存在的场景 —— 客户端和上游说的是同一种协议，
// 走转换路径等于让 thinking 签名、cache_control、metadata、top_k 这些
// Anthropic 独有字段白白经历一次 Claude → Chat → Claude 的往返丢失。
//
// 端点必须显式给出而不能用 providers.RelayNativeRaw：后者靠 body 形状推断
// 路径，而 Anthropic 请求体同样带 messages 字段，会被判成 OpenAI Chat 的
// /chat/completions。
const anthropicMessagesEndpoint = "/messages"

var _ core.NativeRelayProvider = (*Provider)(nil)

// NativeRelayFormatFor anthropic 渠道只认 Claude 协议。
//
// 忽略 model 是契约允许的例外：本渠道挂的全是 Claude 模型，不存在
// 「同渠道内部分模型支持、部分不支持」的情况（对比 AWS 渠道同时有
// anthropic.claude-* 与 openai.gpt-*）。
func (p *Provider) NativeRelayFormatFor(model string, format core.NativeRelayFormat) (core.NativeRelayFormat, bool) {
	_ = model // 渠道内全部模型统一支持 Claude 原生
	if format == core.NativeRelayClaude {
		return format, true
	}
	return "", false
}

// RelayNative 把客户端原始 Anthropic body 直发 /v1/messages。
//
// baseURL 自带 /v1 前缀（defaultBaseURL = https://api.anthropic.com/v1），
// 所以端点是 /messages。
func (p *Provider) RelayNative(ctx context.Context, body []byte) ([]byte, error) {
	return providers.RelayNativeRawAt(ctx, p.client, anthropicMessagesEndpoint, body)
}

// RelayNativeStream 是 RelayNative 的流式版本。
func (p *Provider) RelayNativeStream(ctx context.Context, body []byte) (io.ReadCloser, error) {
	return providers.RelayNativeRawStreamAt(ctx, p.client, anthropicMessagesEndpoint, body)
}
