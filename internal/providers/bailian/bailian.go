// Package bailian provides Alibaba DashScope (百炼) API integration for the LLM gateway.
// Chat/Embeddings 走 OpenAI 兼容模式，Rerank 走原生 API。
// 兼容 API (/compatible-api/v1/reranks) 只支持 qwen3-rerank，不支持 gte-rerank-v2、
// qwen3-vl-rerank 等，原生 API 全支持。
package bailian

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

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
// 先用原生 text-embedding API，失败时 fallback 到多模态 embedding API。
func (p *Provider) Embeddings(ctx context.Context, req *core.EmbeddingRequest) (*core.EmbeddingResponse, error) {
	if req == nil {
		return nil, core.NewInvalidRequestError("embedding request is required", nil)
	}

	// 1. 尝试原生 text-embedding API
	resp, err := p.nativeEmbeddings(ctx, req)
	if err == nil {
		return resp, nil
	}

	// 2. text-embedding API 失败，fallback 到多模态 embedding API
	// （qwen3-vl-embedding 等模型不支持 text-embedding 格式）
	return p.multimodalEmbeddings(ctx, req)
}

// ─── 原生 text-embedding API ──────────────────────────────────────────────

type bailianTextEmbeddingReq struct {
	Model      string                       `json:"model"`
	Input      bailianTextEmbeddingInput    `json:"input"`
	Parameters bailianTextEmbeddingParams   `json:"parameters,omitempty"`
}

type bailianTextEmbeddingInput struct {
	Texts []string `json:"texts"`
}

type bailianTextEmbeddingParams struct {
	TextType string `json:"text_type,omitempty"` // "query" or "document"
}

type bailianTextEmbeddingResp struct {
	Output struct {
		Embeddings []bailianTextEmbeddingItem `json:"embeddings"`
	} `json:"output"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

type bailianTextEmbeddingItem struct {
	Embedding []float64 `json:"embedding"`
	TextIndex int       `json:"text_index"`
}

func (p *Provider) nativeEmbeddings(ctx context.Context, req *core.EmbeddingRequest) (*core.EmbeddingResponse, error) {
	texts := embeddingInputToStrings(req.Input)
	if len(texts) == 0 {
		return nil, core.NewInvalidRequestError("input must be a non-empty string or array of strings", nil)
	}

	bailianReq := bailianTextEmbeddingReq{
		Model: req.Model,
		Input: bailianTextEmbeddingInput{Texts: texts},
	}
	if req.EncodingFormat != "" && req.EncodingFormat != "float" {
		// 原生 API 只支持 float，base64 暂不支持
	}

	var bailianResp bailianTextEmbeddingResp
	err := p.nativeClient.Do(ctx, llmclient.Request{
		Method:   http.MethodPost,
		Endpoint: "/api/v1/services/embeddings/text-embedding/text-embedding",
		Body:     bailianReq,
	}, &bailianResp)
	if err != nil {
		return nil, err
	}

	data := make([]core.EmbeddingData, len(bailianResp.Output.Embeddings))
	for i, e := range bailianResp.Output.Embeddings {
		b, _ := json.Marshal(e.Embedding)
		data[i] = core.EmbeddingData{
			Object:    "embedding",
			Embedding: b,
			Index:     e.TextIndex,
		}
	}

	return &core.EmbeddingResponse{
		Object: "list",
		Data:   data,
		Model:  req.Model,
		Usage: core.EmbeddingUsage{
			PromptTokens: bailianResp.Usage.TotalTokens,
			TotalTokens:  bailianResp.Usage.TotalTokens,
		},
	}, nil
}

// ─── 多模态 embedding API (qwen3-vl-embedding 等) ─────────────────────────

type bailianMultimodalEmbeddingReq struct {
	Model string                            `json:"model"`
	Input bailianMultimodalEmbeddingInput   `json:"input"`
}

type bailianMultimodalEmbeddingInput struct {
	Contents []bailianMultimodalContent `json:"contents"`
}

type bailianMultimodalContent struct {
	Text string `json:"text,omitempty"`
}

type bailianMultimodalEmbeddingResp struct {
	Output struct {
		Embeddings []bailianMultimodalEmbeddingItem `json:"embeddings"`
	} `json:"output"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

type bailianMultimodalEmbeddingItem struct {
	Embedding []float64 `json:"embedding"`
	Index     int       `json:"index"`
}

func (p *Provider) multimodalEmbeddings(ctx context.Context, req *core.EmbeddingRequest) (*core.EmbeddingResponse, error) {
	texts := embeddingInputToStrings(req.Input)
	if len(texts) == 0 {
		return nil, core.NewInvalidRequestError("input must be a non-empty string or array of strings", nil)
	}

	contents := make([]bailianMultimodalContent, len(texts))
	for i, t := range texts {
		contents[i] = bailianMultimodalContent{Text: t}
	}

	bailianReq := bailianMultimodalEmbeddingReq{
		Model: req.Model,
		Input: bailianMultimodalEmbeddingInput{Contents: contents},
	}

	var bailianResp bailianMultimodalEmbeddingResp
	err := p.nativeClient.Do(ctx, llmclient.Request{
		Method:   http.MethodPost,
		Endpoint: "/api/v1/services/embeddings/multimodal-embedding/multimodal-embedding",
		Body:     bailianReq,
	}, &bailianResp)
	if err != nil {
		return nil, err
	}

	data := make([]core.EmbeddingData, len(bailianResp.Output.Embeddings))
	for i, e := range bailianResp.Output.Embeddings {
		b, _ := json.Marshal(e.Embedding)
		data[i] = core.EmbeddingData{
			Object:    "embedding",
			Embedding: b,
			Index:     e.Index,
		}
	}

	return &core.EmbeddingResponse{
		Object: "list",
		Data:   data,
		Model:  req.Model,
		Usage: core.EmbeddingUsage{
			PromptTokens: bailianResp.Usage.TotalTokens,
			TotalTokens:  bailianResp.Usage.TotalTokens,
		},
	}, nil
}

// embeddingInputToStrings 将 core.EmbeddingRequest.Input 转换为 []string。
func embeddingInputToStrings(input any) []string {
	switch v := input.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	case []string:
		return v
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	default:
		return nil
	}
}

// isModelNotSupported 判断错误是否为模型不支持。
func isModelNotSupported(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "model_not_supported") ||
		strings.Contains(msg, "ModelNotSupported") ||
		strings.Contains(msg, "unsupported model")
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