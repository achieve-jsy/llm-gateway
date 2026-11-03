package oracle

import (
	"context"
	"io"
	"net/http"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/llmclient"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/example/llm-gateway/internal/providers/openai"
)

const defaultBaseURL = "https://example.invalid"

var Registration = providers.Registration{
	Type: "oracle",
	New:  New,
	Discovery: providers.DiscoveryConfig{
		RequireBaseURL: true,
	},
}

type Provider struct {
	compat *openai.CompatibleProvider
}

func New(cfg providers.ProviderConfig, opts providers.ProviderOptions) core.Provider {
	baseURL := providers.ResolveBaseURL(cfg.BaseURL, defaultBaseURL)
	return &Provider{
		compat: openai.NewCompatibleProvider(cfg.APIKey, opts, openai.CompatibleProviderConfig{
			ProviderName: "oracle",
			BaseURL:      baseURL,
			SetHeaders:   setHeaders,
		}),
	}
}

func NewWithHTTPClient(apiKey string, httpClient *http.Client, hooks llmclient.Hooks) *Provider {
	return &Provider{
		compat: openai.NewCompatibleProviderWithHTTPClient(apiKey, httpClient, hooks, openai.CompatibleProviderConfig{
			ProviderName: "oracle",
			BaseURL:      defaultBaseURL,
			SetHeaders:   setHeaders,
		}),
	}
}

func (p *Provider) SetBaseURL(baseURL string) {
	p.compat.SetBaseURL(baseURL)
}

func (p *Provider) ChatCompletion(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	return p.compat.ChatCompletion(ctx, req)
}

func (p *Provider) StreamChatCompletion(ctx context.Context, req *core.ChatRequest) (io.ReadCloser, error) {
	return p.compat.StreamChatCompletion(ctx, req)
}

func (p *Provider) ListModels(ctx context.Context) (*core.ModelsResponse, error) {
	return p.compat.ListModels(ctx)
}

func (p *Provider) Responses(ctx context.Context, req *core.ResponsesRequest) (*core.ResponsesResponse, error) {
	return p.compat.Responses(ctx, req)
}

func (p *Provider) StreamResponses(ctx context.Context, req *core.ResponsesRequest) (io.ReadCloser, error) {
	return p.compat.StreamResponses(ctx, req)
}

func (p *Provider) Embeddings(_ context.Context, _ *core.EmbeddingRequest) (*core.EmbeddingResponse, error) {
	return nil, core.NewInvalidRequestError("oracle does not support embeddings", nil)
}

func (p *Provider) Rerank(ctx context.Context, req *core.RerankRequest) (*core.RerankResponse, error) {
	return p.compat.Rerank(ctx, req)
}

// ─── 原生旁路（NativeRelay）─────────────────────────────
// 字段委托不会像指针嵌入那样自动继承方法，这里显式转发到 compat，
// 避免把同一份实现复制一遍。

func (p *Provider) NativeRelayFormatFor(model string, format core.NativeRelayFormat) (core.NativeRelayFormat, bool) {
	return p.compat.NativeRelayFormatFor(model, format)
}

func (p *Provider) RelayNative(ctx context.Context, body []byte) ([]byte, error) {
	return p.compat.RelayNative(ctx, body)
}

func (p *Provider) RelayNativeStream(ctx context.Context, body []byte) (io.ReadCloser, error) {
	return p.compat.RelayNativeStream(ctx, body)
}

var _ core.NativeRelayProvider = (*Provider)(nil)

func setHeaders(req *http.Request, apiKey string) {
	req.Header.Set("Authorization", "Bearer "+apiKey)
}
