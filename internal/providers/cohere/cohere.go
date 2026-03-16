// Package cohere provides Cohere native API integration for the LLM gateway.
// Cohere's Rerank API uses /v2/rerank (not /v1/rerank), so the default
// CompatibleProvider.Rerank() path (/rerank on a /v1 base) is incorrect.
// This provider embeds *openai.CompatibleProvider and overrides only Rerank().
package cohere

import (
	"context"
	"io"
	"net/http"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/llmclient"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/example/llm-gateway/internal/providers/openai"
)

// Registration provides factory registration for the Cohere provider.
var Registration = providers.Registration{
	Type: "cohere",
	New:  New,
	Discovery: providers.DiscoveryConfig{
		RequireBaseURL:  false,
		AllowAPIKeyless: false,
	},
}

// Provider implements the core.Provider interface for Cohere.
// It embeds *openai.CompatibleProvider to inherit all standard OpenAI-compatible
// methods and only overrides Rerank() to target Cohere's /v2/rerank endpoint.
type Provider struct {
	compatible *openai.CompatibleProvider
}

// New creates a new Cohere provider.
// The default base URL is https://api.cohere.com — the Rerank method appends
// /v2/rerank to form the full Cohere v2 Rerank endpoint.
func New(cfg providers.ProviderConfig, opts providers.ProviderOptions) core.Provider {
	baseURL := providers.ResolveBaseURL(cfg.BaseURL, "https://api.cohere.com")
	return &Provider{
		compatible: openai.NewCompatibleProvider(cfg.APIKey, opts, openai.CompatibleProviderConfig{
			ProviderName: "cohere",
			BaseURL:      baseURL,
			SetHeaders:   setHeaders,
		}),
	}
}

func setHeaders(req *http.Request, apiKey string) {
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if requestID := core.GetRequestID(req.Context()); requestID != "" {
		req.Header.Set("X-Request-Id", requestID)
	}
}

// Rerank sends a rerank request to Cohere's /v2/rerank endpoint.
// This overrides the default CompatibleProvider.Rerank() which uses /rerank.
func (p *Provider) Rerank(ctx context.Context, req *core.RerankRequest) (*core.RerankResponse, error) {
	if req == nil {
		return nil, core.NewInvalidRequestError("rerank request is required", nil)
	}
	var resp core.RerankResponse
	err := p.compatible.Do(ctx, llmclient.Request{
		Method:   http.MethodPost,
		Endpoint: "/v2/rerank",
		Body:     req,
	}, &resp)
	if err != nil {
		return nil, err
	}
	if resp.Model == "" {
		resp.Model = req.Model
	}
	return &resp, nil
}

// ChatCompletion delegates to the embedded CompatibleProvider.
func (p *Provider) ChatCompletion(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	return p.compatible.ChatCompletion(ctx, req)
}

// StreamChatCompletion delegates to the embedded CompatibleProvider.
func (p *Provider) StreamChatCompletion(ctx context.Context, req *core.ChatRequest) (io.ReadCloser, error) {
	return p.compatible.StreamChatCompletion(ctx, req)
}

// ListModels delegates to the embedded CompatibleProvider.
func (p *Provider) ListModels(ctx context.Context) (*core.ModelsResponse, error) {
	return p.compatible.ListModels(ctx)
}

// Responses delegates to chat-completions translation.
func (p *Provider) Responses(ctx context.Context, req *core.ResponsesRequest) (*core.ResponsesResponse, error) {
	return providers.ResponsesViaChat(ctx, p, req)
}

// StreamResponses delegates to chat-completions translation.
func (p *Provider) StreamResponses(ctx context.Context, req *core.ResponsesRequest) (io.ReadCloser, error) {
	return providers.StreamResponsesViaChat(ctx, p, req, "cohere")
}

// Embeddings delegates to the embedded CompatibleProvider.
func (p *Provider) Embeddings(ctx context.Context, req *core.EmbeddingRequest) (*core.EmbeddingResponse, error) {
	return p.compatible.Embeddings(ctx, req)
}

// Passthrough delegates to the embedded CompatibleProvider.
func (p *Provider) Passthrough(ctx context.Context, req *core.PassthroughRequest) (*core.PassthroughResponse, error) {
	return p.compatible.Passthrough(ctx, req)
}

// CreateBatch delegates to the embedded CompatibleProvider.
func (p *Provider) CreateBatch(ctx context.Context, req *core.BatchRequest) (*core.BatchResponse, error) {
	return p.compatible.CreateBatch(ctx, req)
}

// GetBatch delegates to the embedded CompatibleProvider.
func (p *Provider) GetBatch(ctx context.Context, id string) (*core.BatchResponse, error) {
	return p.compatible.GetBatch(ctx, id)
}

// ListBatches delegates to the embedded CompatibleProvider.
func (p *Provider) ListBatches(ctx context.Context, limit int, after string) (*core.BatchListResponse, error) {
	return p.compatible.ListBatches(ctx, limit, after)
}

// CancelBatch delegates to the embedded CompatibleProvider.
func (p *Provider) CancelBatch(ctx context.Context, id string) (*core.BatchResponse, error) {
	return p.compatible.CancelBatch(ctx, id)
}

// GetBatchResults delegates to the embedded CompatibleProvider.
func (p *Provider) GetBatchResults(ctx context.Context, id string) (*core.BatchResultsResponse, error) {
	return p.compatible.GetBatchResults(ctx, id)
}