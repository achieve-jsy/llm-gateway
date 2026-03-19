// Package bailian provides Alibaba DashScope (百炼) API integration for the LLM gateway.
// Bailian uses the DashScope OpenAI-compatible endpoint for Chat/Embeddings,
// and the compatible-api endpoint for Rerank (DashScope 兼容模式没有 /rerank,
// 但 /compatible-api/v1/reranks 支持标准 Cohere/Jina 格式).
package bailian

import (
	"context"
	"io"
	"net/http"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/llmclient"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/example/llm-gateway/internal/providers/openai"
)

// defaultBaseURL is the DashScope OpenAI-compatible endpoint (Chat/Embeddings).
const defaultBaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"

// rerankBaseURL is the DashScope compatible-api endpoint for Rerank.
// DashScope puts rerank under /compatible-api/v1 instead of /compatible-mode/v1,
// per LiteLLM's approach: https://github.com/BerriAI/litellm
const rerankBaseURL = "https://dashscope.aliyuncs.com/compatible-api/v1"

// Registration provides factory registration for the Bailian provider.
var Registration = providers.Registration{
	Type: "bailian",
	New:  New,
	Discovery: providers.DiscoveryConfig{
		DefaultBaseURL: defaultBaseURL,
	},
}

// Provider implements the core.Provider interface for Bailian (DashScope).
type Provider struct {
	compatible  *openai.CompatibleProvider
	rerankClient *llmclient.Client // 独立的 rerank client，base URL 指向 compatible-api
}

// New creates a new Bailian provider.
func New(cfg providers.ProviderConfig, opts providers.ProviderOptions) core.Provider {
	baseURL := providers.ResolveBaseURL(cfg.BaseURL, defaultBaseURL)
	compatible := openai.NewCompatibleProvider(cfg.APIKey, opts, openai.CompatibleProviderConfig{
		ProviderName: "bailian",
		BaseURL:      baseURL,
		SetHeaders:   setHeaders,
	})
	return &Provider{
		compatible:   compatible,
		rerankClient: llmclient.New(rerankClientConfig(), rerankHeaderSetter(cfg.APIKey)),
	}
}

func rerankClientConfig() llmclient.Config {
	return llmclient.DefaultConfig("bailian-rerank", rerankBaseURL)
}

func rerankHeaderSetter(apiKey string) llmclient.HeaderSetter {
	return func(req *http.Request) {
		setHeaders(req, apiKey)
	}
}

// NewWithHTTPClient creates a new Bailian provider with a custom HTTP client.
func NewWithHTTPClient(apiKey string, baseURL string, httpClient *http.Client, hooks llmclient.Hooks) *Provider {
	resolvedBaseURL := providers.ResolveBaseURL(baseURL, defaultBaseURL)
	return &Provider{
		compatible: openai.NewCompatibleProviderWithHTTPClient(apiKey, httpClient, hooks, openai.CompatibleProviderConfig{
			ProviderName: "bailian",
			BaseURL:      resolvedBaseURL,
			SetHeaders:   setHeaders,
		}),
		rerankClient: llmclient.NewWithHTTPClient(httpClient, rerankClientConfig(), rerankHeaderSetter(apiKey)),
	}
}

// SetBaseURL allows configuring a custom base URL for the provider.
func (p *Provider) SetBaseURL(url string) {
	p.compatible.SetBaseURL(url)
}

func setHeaders(req *http.Request, apiKey string) {
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if requestID := core.GetRequestID(req.Context()); requestID != "" {
		req.Header.Set("X-Request-Id", requestID)
	}
}

// ChatCompletion sends a chat completion request to Bailian.
func (p *Provider) ChatCompletion(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	return p.compatible.ChatCompletion(ctx, req)
}

// StreamChatCompletion returns a raw response body for streaming.
func (p *Provider) StreamChatCompletion(ctx context.Context, req *core.ChatRequest) (io.ReadCloser, error) {
	return p.compatible.StreamChatCompletion(ctx, req)
}

// ListModels retrieves the list of available models from Bailian.
func (p *Provider) ListModels(ctx context.Context) (*core.ModelsResponse, error) {
	return p.compatible.ListModels(ctx)
}

// Responses sends a Responses API request to Bailian using chat-completions translation.
func (p *Provider) Responses(ctx context.Context, req *core.ResponsesRequest) (*core.ResponsesResponse, error) {
	return providers.ResponsesViaChat(ctx, p, req)
}

// StreamResponses streams a Responses API request to Bailian using chat-completions translation.
func (p *Provider) StreamResponses(ctx context.Context, req *core.ResponsesRequest) (io.ReadCloser, error) {
	return providers.StreamResponsesViaChat(ctx, p, req, "bailian")
}

// Embeddings sends an embeddings request to Bailian.
func (p *Provider) Embeddings(ctx context.Context, req *core.EmbeddingRequest) (*core.EmbeddingResponse, error) {
	return p.compatible.Embeddings(ctx, req)
}

// Rerank sends a rerank request to Bailian using the compatible-api endpoint.
// DashScope 兼容模式 /compatible-mode/v1 没有 /rerank 端点，
// 但 /compatible-api/v1/reranks 支持标准 Cohere/Jina 格式。
func (p *Provider) Rerank(ctx context.Context, req *core.RerankRequest) (*core.RerankResponse, error) {
	if req == nil {
		return nil, core.NewInvalidRequestError("rerank request is required", nil)
	}
	var resp core.RerankResponse
	err := p.rerankClient.Do(ctx, llmclient.Request{
		Method:   http.MethodPost,
		Endpoint: "/reranks",
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

// Passthrough routes an opaque provider-native request to Bailian.
func (p *Provider) Passthrough(ctx context.Context, req *core.PassthroughRequest) (*core.PassthroughResponse, error) {
	return p.compatible.Passthrough(ctx, req)
}

// CreateBatch creates a native Bailian batch job.
func (p *Provider) CreateBatch(ctx context.Context, req *core.BatchRequest) (*core.BatchResponse, error) {
	if req == nil {
		return nil, core.NewInvalidRequestError("batch request is required", nil)
	}
	var resp core.BatchResponse
	err := p.compatible.Do(ctx, llmclient.Request{
		Method:   http.MethodPost,
		Endpoint: "/batches",
		Body:     req,
	}, &resp)
	if err != nil {
		return nil, err
	}
	if resp.ProviderBatchID == "" {
		resp.ProviderBatchID = resp.ID
	}
	return &resp, nil
}

// GetBatch retrieves a native Bailian batch job.
func (p *Provider) GetBatch(ctx context.Context, id string) (*core.BatchResponse, error) {
	return p.compatible.GetBatch(ctx, id)
}

// ListBatches lists native Bailian batch jobs.
func (p *Provider) ListBatches(ctx context.Context, limit int, after string) (*core.BatchListResponse, error) {
	return p.compatible.ListBatches(ctx, limit, after)
}

// CancelBatch cancels a native Bailian batch job.
func (p *Provider) CancelBatch(ctx context.Context, id string) (*core.BatchResponse, error) {
	return p.compatible.CancelBatch(ctx, id)
}

// GetBatchResults fetches Bailian batch results via the output file API.
func (p *Provider) GetBatchResults(ctx context.Context, id string) (*core.BatchResultsResponse, error) {
	return p.compatible.GetBatchResults(ctx, id)
}