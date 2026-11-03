package xai

import (
	"context"
	"io"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers"
)

// 原生旁路：把客户端原始 body 零转换直发上游。
//
// 本包没有 openai.CompatibleProvider 字段，无法用转发方法继承，因此调用
// providers 包的公共 helper（deepseek/groq/xai/ollama 同构，共用一份实现）。

var _ core.NativeRelayProvider = (*Provider)(nil)

func (p *Provider) NativeRelayFormatFor(model string, format core.NativeRelayFormat) (core.NativeRelayFormat, bool) {
	return providers.NativeFormatForOpenAI(model, format)
}

func (p *Provider) RelayNative(ctx context.Context, body []byte) ([]byte, error) {
	return providers.RelayNativeRaw(ctx, p.client, body)
}

func (p *Provider) RelayNativeStream(ctx context.Context, body []byte) (io.ReadCloser, error) {
	return providers.RelayNativeRawStream(ctx, p.client, body)
}
