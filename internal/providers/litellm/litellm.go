// Package litellm provides LiteLLM proxy API integration for the LLM gateway.
// LiteLLM exposes an OpenAI-compatible API with a configurable base URL.
package litellm

import (
	"context"
	"io"
	"net/http"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/llmclient"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/example/llm-gateway/internal/providers/openai"
)

// Registration provides factory registration for the LiteLLM provider.
var Registration = providers.Registration{
	Type: "litellm",
	New:  New,
	Discovery: providers.DiscoveryConfig{
		RequireBaseURL:  true,
		AllowAPIKeyless: true,
	},
}

// Provider implements the core.Provider interface for LiteLLM.
type Provider struct {
	compatible *openai.CompatibleProvider
}

// New creates a new LiteLLM provider.
func New(cfg providers.ProviderConfig, opts providers.ProviderOptions) core.Provider {
	baseURL := providers.ResolveBaseURL(cfg.BaseURL, "http://localhost:4000/v1")
	return &Provider{
		compatible: openai.NewCompatibleProvider(cfg.APIKey, opts, openai.CompatibleProviderConfig{
			ProviderName: "litellm",
			BaseURL:      baseURL,
			SetHeaders:   setHeaders,
		}),
	}
}

// NewWithHTTPClient creates a new LiteLLM provider with a custom HTTP client.
// If httpClient is nil, http.DefaultClient is used.
func NewWithHTTPClient(apiKey string, baseURL string, httpClient *http.Client, hooks llmclient.Hooks) *Provider {
	resolvedBaseURL := providers.ResolveBaseURL(baseURL, "http://localhost:4000/v1")
	return &Provider{
		compatible: openai.NewCompatibleProviderWithHTTPClient(apiKey, httpClient, hooks, openai.CompatibleProviderConfig{
			ProviderName: "litellm",
			BaseURL:      resolvedBaseURL,
			SetHeaders:   setHeaders,
		}),
	}
}

// SetBaseURL allows configuring a custom base URL for the provider.
func (p *Provider) SetBaseURL(url string) {
	p.compatible.SetBaseURL(url)
}

func setHeaders(req *http.Request, apiKey string) {
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if requestID := core.GetRequestID(req.Context()); requestID != "" {
		req.Header.Set("X-Request-Id", requestID)
	}
}

// ChatCompletion sends a chat completion request to LiteLLM.
func (p *Provider) ChatCompletion(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	return p.compatible.ChatCompletion(ctx, req)
}

// StreamChatCompletion returns a raw response body for streaming.
func (p *Provider) StreamChatCompletion(ctx context.Context, req *core.ChatRequest) (io.ReadCloser, error) {
	return p.compatible.StreamChatCompletion(ctx, req)
}

// ListModels retrieves the list of available models from LiteLLM.
func (p *Provider) ListModels(ctx context.Context) (*core.ModelsResponse, error) {
	return p.compatible.ListModels(ctx)
}

// Responses sends a Responses API request to LiteLLM using chat-completions translation.
func (p *Provider) Responses(ctx context.Context, req *core.ResponsesRequest) (*core.ResponsesResponse, error) {
	return providers.ResponsesViaChat(ctx, p, req)
}

// StreamResponses streams a Responses API request to LiteLLM using chat-completions translation.
func (p *Provider) StreamResponses(ctx context.Context, req *core.ResponsesRequest) (io.ReadCloser, error) {
	return providers.StreamResponsesViaChat(ctx, p, req, "litellm")
}

// Embeddings sends an embeddings request to LiteLLM.
func (p *Provider) Embeddings(ctx context.Context, req *core.EmbeddingRequest) (*core.EmbeddingResponse, error) {
	return p.compatible.Embeddings(ctx, req)
}

// Rerank sends a rerank request to LiteLLM.
func (p *Provider) Rerank(ctx context.Context, req *core.RerankRequest) (*core.RerankResponse, error) {
	return p.compatible.Rerank(ctx, req)
}

// Passthrough routes an opaque provider-native request to LiteLLM.
func (p *Provider) Passthrough(ctx context.Context, req *core.PassthroughRequest) (*core.PassthroughResponse, error) {
	return p.compatible.Passthrough(ctx, req)
}

// CreateBatch creates a native LiteLLM batch job.
func (p *Provider) CreateBatch(ctx context.Context, req *core.BatchRequest) (*core.BatchResponse, error) {
	return p.compatible.CreateBatch(ctx, req)
}

// GetBatch retrieves a native LiteLLM batch job.
func (p *Provider) GetBatch(ctx context.Context, id string) (*core.BatchResponse, error) {
	return p.compatible.GetBatch(ctx, id)
}

// ListBatches lists native LiteLLM batch jobs.
func (p *Provider) ListBatches(ctx context.Context, limit int, after string) (*core.BatchListResponse, error) {
	return p.compatible.ListBatches(ctx, limit, after)
}

// CancelBatch cancels a native LiteLLM batch job.
func (p *Provider) CancelBatch(ctx context.Context, id string) (*core.BatchResponse, error) {
	return p.compatible.CancelBatch(ctx, id)
}

// GetBatchResults fetches LiteLLM batch results via the output file API.
func (p *Provider) GetBatchResults(ctx context.Context, id string) (*core.BatchResultsResponse, error) {
	return p.compatible.GetBatchResults(ctx, id)
}
