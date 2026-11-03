package vertex

import (
	"context"
	"io"

	"github.com/example/llm-gateway/internal/core"
)

// 原生旁路转发层。
//
// vertex.Provider 用具名字段持有 gemini.Provider（不是嵌入），Go 不提升具名字段
// 的方法，所以这里必须显式转发，否则 *vertex.Provider 不满足 core.NativeRelayProvider。
//
// 生产上 Gemini 流量全部由 vertex 渠道承载（没有任何 gemini 直连渠道），
// 这层缺失等于 Gemini 零转换完全没生效。
//
// 只做转发，不复制实现：URL 拼接、鉴权头、body 的 model 摘除都在 gemini 侧，
// 避免两份逻辑将来漂移。

var _ core.NativeRelayProvider = (*Provider)(nil)

// NativeRelayFormatFor 判断内部 gemini provider 能否原生处理该模型。
func (p *Provider) NativeRelayFormatFor(model string, format core.NativeRelayFormat) (core.NativeRelayFormat, bool) {
	if p.gemini == nil {
		return "", false
	}
	return p.gemini.NativeRelayFormatFor(model, format)
}

// RelayNative 转发非流式原生调用，返回上游原始响应体。
func (p *Provider) RelayNative(ctx context.Context, body []byte) ([]byte, error) {
	if err := p.ready(); err != nil {
		return nil, err
	}
	if p.gemini == nil {
		return nil, core.NewProviderError("vertex", 502, "vertex gemini adapter not initialized", nil)
	}
	return p.gemini.RelayNative(ctx, body)
}

// RelayNativeStream 转发流式原生调用，返回上游原始 SSE 流（调用方关闭）。
func (p *Provider) RelayNativeStream(ctx context.Context, body []byte) (io.ReadCloser, error) {
	if err := p.ready(); err != nil {
		return nil, err
	}
	if p.gemini == nil {
		return nil, core.NewProviderError("vertex", 502, "vertex gemini adapter not initialized", nil)
	}
	return p.gemini.RelayNativeStream(ctx, body)
}
