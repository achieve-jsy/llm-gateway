// Package bailian provides Alibaba DashScope (百炼) API integration for the LLM gateway.
// Chat/Embeddings 走 OpenAI 兼容模式，Rerank 走原生 API。
// 兼容 API (/compatible-api/v1/reranks) 只支持 qwen3-rerank，不支持 gte-rerank-v2、
// qwen3-vl-rerank 等，原生 API 全支持。
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

// nativeBaseURL is the DashScope native API endpoint for Rerank.
// 兼容 API 模型覆盖不全，原生 API 支持所有 rerank 模型。
const nativeBaseURL = "https://dashscope.aliyuncs.com"

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
	compatible   *openai.CompatibleProvider
	nativeClient *llmclient.Client // 原生 API client，用于 Rerank
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
		nativeClient: llmclient.New(nativeClientConfig(), nativeHeaderSetter(cfg.APIKey)),
	}
}

func nativeClientConfig() llmclient.Config {
	return llmclient.DefaultConfig("bailian-native", nativeBaseURL)
}

func nativeHeaderSetter(apiKey string) llmclient.HeaderSetter {
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
		nativeClient: llmclient.NewWithHTTPClient(httpClient, nativeClientConfig(), nativeHeaderSetter(apiKey)),
	}
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

// Responses sends a Responses API request to Bailian.
func (p *Provider) Responses(ctx context.Context, req *core.ResponsesRequest) (*core.ResponsesResponse, error) {
	return providers.ResponsesViaChat(ctx, p, req)
}

// StreamResponses streams a Responses API request to Bailian.
func (p *Provider) StreamResponses(ctx context.Context, req *core.ResponsesRequest) (io.ReadCloser, error) {
	return providers.StreamResponsesViaChat(ctx, p, req, "bailian")
}

// Embeddings sends an embeddings request to Bailian.
func (p *Provider) Embeddings(ctx context.Context, req *core.EmbeddingRequest) (*core.EmbeddingResponse, error) {
	return p.compatible.Embeddings(ctx, req)
}

// ─── 百炼原生 Rerank 请求/响应格式 ────────────────────────────────────────
// 兼容 API 只支持 qwen3-rerank，原生 API 支持所有 rerank 模型
// (qwen3-rerank, gte-rerank-v2, qwen3-vl-rerank 等)。

type bailianRerankReq struct {
	Model      string                  `json:"model"`
	Input      bailianRerankInput      `json:"input"`
	Parameters bailianRerankParameters `json:"parameters,omitempty"`
}

type bailianRerankInput struct {
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
}

type bailianRerankParameters struct {
	TopN int `json:"top_n,omitempty"`
}

type bailianRerankResp struct {
	Output struct {
		Results []bailianRerankResult `json:"results"`
	} `json:"output"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

type bailianRerankResult struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
}

// Rerank 使用百炼原生 API (/api/v1/services/rerank/text-rerank/text-rerank)。
// 兼容 API 模型覆盖不全，原生 API 支持所有 rerank 模型。
func (p *Provider) Rerank(ctx context.Context, req *core.RerankRequest) (*core.RerankResponse, error) {
	if req == nil {
		return nil, core.NewInvalidRequestError("rerank request is required", nil)
	}

	topN := 10
	if req.TopN != nil && *req.TopN > 0 {
		topN = *req.TopN
	}

	bailianReq := bailianRerankReq{
		Model: req.Model,
		Input: bailianRerankInput{
			Query:     req.Query,
			Documents: req.Documents,
		},
		Parameters: bailianRerankParameters{TopN: topN},
	}

	var bailianResp bailianRerankResp
	err := p.nativeClient.Do(ctx, llmclient.Request{
		Method:   http.MethodPost,
		Endpoint: "/api/v1/services/rerank/text-rerank/text-rerank",
		Body:     bailianReq,
	}, &bailianResp)
	if err != nil {
		return nil, err
	}

	results := make([]core.RerankResult, len(bailianResp.Output.Results))
	for i, r := range bailianResp.Output.Results {
		results[i] = core.RerankResult{
			Index:          r.Index,
			RelevanceScore: r.RelevanceScore,
		}
		if req.ReturnDocuments != nil && *req.ReturnDocuments && r.Index < len(req.Documents) {
			results[i].Document = &core.RerankDocument{Text: req.Documents[r.Index]}
		}
	}

	return &core.RerankResponse{
		Model:   req.Model,
		Results: results,
		Usage: core.RerankUsage{
			TotalTokens: bailianResp.Usage.TotalTokens,
		},
	}, nil
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