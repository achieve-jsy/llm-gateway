// Package bailian provides Alibaba DashScope (百炼) API integration for the LLM gateway.
// Chat/Embeddings 走 OpenAI 兼容模式，Rerank 走原生 API。
// 兼容 API (/compatible-api/v1/reranks) 只支持 qwen3-rerank，不支持 gte-rerank-v2、
// qwen3-vl-rerank 等，原生 API 全支持。
package bailian

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"

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
	embedSem     chan struct{}     // 全局 embedding 并发限流，所有请求共享
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
		embedSem:     make(chan struct{}, maxEmbeddingConcurrency),
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
		embedSem:     make(chan struct{}, maxEmbeddingConcurrency),
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
// 根据 model_type 精确路由：embedding_vl → multimodal API，embedding/空或未知 → text-embedding API。
func (p *Provider) Embeddings(ctx context.Context, req *core.EmbeddingRequest) (*core.EmbeddingResponse, error) {
	if req == nil {
		return nil, core.NewInvalidRequestError("embedding request is required", nil)
	}

	isMultimodal := isMultimodalInput(req.Input)
	slog.Info("Embeddings: received request",
		"model", req.Model,
		"model_type", req.ModelType,
		"input_type", fmt.Sprintf("%T", req.Input),
		"is_multimodal", isMultimodal)

	// 精确路由：根据 model_type 选择 API
	switch req.ModelType {
	case "embedding_vl":
		return p.multimodalEmbeddings(ctx, req)
	case "embedding":
		if isMultimodal {
			return nil, core.NewInvalidRequestError("multimodal input requires an embedding_vl model", nil)
		}
		return p.nativeEmbeddings(ctx, req)
	default:
		if isMultimodal {
			return nil, core.NewInvalidRequestError("multimodal input requires an embedding_vl model", nil)
		}
		return p.nativeEmbeddings(ctx, req)
	}
}

// ─── 原生 text-embedding API ──────────────────────────────────────────────

const (
	// maxEmbeddingBatchSize 单次原生 embedding 请求的最大文本条数。
	// 百炼 text-embedding-v4 官方文档限制最多 10 条/次。
	// 参考: https://help.aliyun.com/zh/model-studio/text-embedding-api
	maxEmbeddingBatchSize = 10

	// maxEmbeddingConcurrency 分批请求的最大并发数（Provider 全局级别）。
	// 百炼实测阈值约 30-35 QPS，取 10 留足安全边际。
	// 所有并发请求共享此 semaphore，避免 100 个请求 × 各自 100 批 = 万级并发。
	maxEmbeddingConcurrency = 10
)

type bailianTextEmbeddingReq struct {
	Model      string                      `json:"model"`
	Input      bailianTextEmbeddingInput   `json:"input"`
	Parameters *bailianTextEmbeddingParams `json:"parameters,omitempty"`
}

type bailianTextEmbeddingInput struct {
	Texts []string `json:"texts"`
}

type bailianTextEmbeddingParams struct {
	TextType   string `json:"text_type,omitempty"`   // "query" or "document"
	Dimension  *int   `json:"dimension,omitempty"`   // 输出向量维度，仅 v3/v4 支持
	OutputType string `json:"output_type,omitempty"` // "dense", "sparse", "dense&sparse"，仅 v3/v4
	Instruct   string `json:"instruct,omitempty"`    // 自定义任务指令
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

	params := extractTextEmbeddingParams(req)

	// 大输入分批处理，避免单次请求体超过百炼 API 限制
	batches := splitTextsByCount(texts, maxEmbeddingBatchSize)
	if len(batches) == 1 {
		return p.doNativeEmbedding(ctx, req.Model, batches[0], 0, params)
	}

	// 多批次并发请求，合并结果（限流避免触发百炼限流）
	type batchResult struct {
		data  []core.EmbeddingData
		usage core.EmbeddingUsage
	}
	results := make([]batchResult, len(batches))
	var mu sync.Mutex
	var wg sync.WaitGroup
	var firstErr error

	offset := 0
	for i, batch := range batches {
		batchOffset := offset
		offset += len(batch)
		wg.Add(1)
		go func(idx int, b []string, off int) {
			defer wg.Done()
			p.embedSem <- struct{}{}        // 全局限流
			defer func() { <-p.embedSem }() // 释放
			resp, err := p.doNativeEmbedding(ctx, req.Model, b, off, params)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			results[idx] = batchResult{
				data:  resp.Data,
				usage: resp.Usage,
			}
		}(i, batch, batchOffset)
	}
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}

	// 合并结果
	allData := make([]core.EmbeddingData, 0, len(texts))
	totalTokens := 0
	for _, r := range results {
		allData = append(allData, r.data...)
		totalTokens += r.usage.TotalTokens
	}

	return &core.EmbeddingResponse{
		Object: "list",
		Data:   allData,
		Model:  req.Model,
		Usage: core.EmbeddingUsage{
			PromptTokens: totalTokens,
			TotalTokens:  totalTokens,
		},
	}, nil
}

// doNativeEmbedding 发送单个批次的 embedding 请求到原生 text-embedding API。
// indexOffset 用于修正分批后的索引，确保返回的 Index 与原始输入顺序一致。
func (p *Provider) doNativeEmbedding(ctx context.Context, model string, texts []string, indexOffset int, params *bailianTextEmbeddingParams) (*core.EmbeddingResponse, error) {
	bailianReq := bailianTextEmbeddingReq{
		Model:      model,
		Input:      bailianTextEmbeddingInput{Texts: texts},
		Parameters: params,
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
			Index:     indexOffset + e.TextIndex,
		}
	}

	return &core.EmbeddingResponse{
		Object: "list",
		Data:   data,
		Model:  model,
		Usage: core.EmbeddingUsage{
			PromptTokens: bailianResp.Usage.TotalTokens,
			TotalTokens:  bailianResp.Usage.TotalTokens,
		},
	}, nil
}

// splitTextsByCount 将文本切片数组按条数分批次，每批不超过 maxSize 条。
// 百炼 text-embedding-v4 官方限制最多 10 条/次。
func splitTextsByCount(texts []string, maxSize int) [][]string {
	var batches [][]string
	for i := 0; i < len(texts); i += maxSize {
		end := i + maxSize
		if end > len(texts) {
			end = len(texts)
		}
		batches = append(batches, texts[i:end])
	}
	return batches
}

// ─── 多模态 embedding API (qwen3-vl-embedding 等) ─────────────────────────
// 参考 DashScope Python SDK: dashscope/embeddings/multimodal_embedding.py

type bailianMultimodalEmbeddingReq struct {
	Model      string                            `json:"model"`
	Input      bailianMultimodalEmbeddingInput   `json:"input"`
	Parameters *bailianMultimodalEmbeddingParams `json:"parameters,omitempty"`
}

type bailianMultimodalEmbeddingInput struct {
	Contents []bailianMultimodalContent `json:"contents"`
}

// bailianMultimodalContent 对齐 Python SDK 的 MultiModalEmbeddingItem* 类型。
// 每个 item 至少包含 text/image/audio 之一，可选 factor 权重。
type bailianMultimodalContent struct {
	Text   string   `json:"text,omitempty"`
	Image  string   `json:"image,omitempty"`  // URL 或 base64 编码的图片
	Audio  string   `json:"audio,omitempty"`  // URL 或 base64 编码的音频
	Factor *float64 `json:"factor,omitempty"` // 权重，默认 1.0
}

type bailianMultimodalEmbeddingParams struct {
	Dimension      *int     `json:"dimension,omitempty"`
	OutputType     string   `json:"output_type,omitempty"`      // "dense"
	FPS            *float64 `json:"fps,omitempty"`              // 视频帧提取率 [0,1]
	Instruct       string   `json:"instruct,omitempty"`         // 自定义任务指令
	EnableFusion   *bool    `json:"enable_fusion,omitempty"`    // 仅 qwen3-vl-embedding，融合为单向量
	ResLevel       *int     `json:"res_level,omitempty"`        // 分辨率层级 0/1/2/3
	MaxVideoFrames *int     `json:"max_video_frames,omitempty"` // 最大视频采样帧数，最多 64
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
	contents, err := parseMultimodalInputs(req.Input)
	if err != nil {
		return nil, core.NewInvalidRequestError(err.Error(), nil)
	}
	if len(contents) == 0 {
		return nil, core.NewInvalidRequestError("input must be a non-empty string, array of strings, or array of {text,image,audio} objects", nil)
	}

	params := extractMultimodalEmbeddingParams(req)

	// 大输入分批处理，避免单次请求体超过百炼 API 限制
	batches := splitMultimodalByCount(contents, maxEmbeddingBatchSize)
	if len(batches) == 1 {
		return p.doMultimodalEmbedding(ctx, req.Model, batches[0], 0, params)
	}

	// 多批次并发请求，合并结果
	type batchResult struct {
		data  []core.EmbeddingData
		usage core.EmbeddingUsage
	}
	results := make([]batchResult, len(batches))
	var mu sync.Mutex
	var wg sync.WaitGroup
	var firstErr error

	offset := 0
	for i, batch := range batches {
		batchOffset := offset
		offset += len(batch)
		wg.Add(1)
		go func(idx int, b []bailianMultimodalContent, off int) {
			defer wg.Done()
			p.embedSem <- struct{}{}        // 全局限流
			defer func() { <-p.embedSem }() // 释放
			resp, err := p.doMultimodalEmbedding(ctx, req.Model, b, off, params)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			results[idx] = batchResult{
				data:  resp.Data,
				usage: resp.Usage,
			}
		}(i, batch, batchOffset)
	}
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}

	allData := make([]core.EmbeddingData, 0, len(contents))
	totalTokens := 0
	for _, r := range results {
		allData = append(allData, r.data...)
		totalTokens += r.usage.TotalTokens
	}

	return &core.EmbeddingResponse{
		Object: "list",
		Data:   allData,
		Model:  req.Model,
		Usage: core.EmbeddingUsage{
			PromptTokens: totalTokens,
			TotalTokens:  totalTokens,
		},
	}, nil
}

// doMultimodalEmbedding 发送单个批次的 embedding 请求到多模态 embedding API。
func (p *Provider) doMultimodalEmbedding(ctx context.Context, model string, contents []bailianMultimodalContent, indexOffset int, params *bailianMultimodalEmbeddingParams) (*core.EmbeddingResponse, error) {
	bailianReq := bailianMultimodalEmbeddingReq{
		Model:      model,
		Input:      bailianMultimodalEmbeddingInput{Contents: contents},
		Parameters: params,
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
			Index:     indexOffset + e.Index,
		}
	}

	return &core.EmbeddingResponse{
		Object: "list",
		Data:   data,
		Model:  model,
		Usage: core.EmbeddingUsage{
			PromptTokens: bailianResp.Usage.TotalTokens,
			TotalTokens:  bailianResp.Usage.TotalTokens,
		},
	}, nil
}

// parseMultimodalInputs 将 core.EmbeddingRequest.Input 解析为 []bailianMultimodalContent。
// 支持三种格式：
// isMultimodalInput 检查输入是否包含多模态内容（图片/音频）
func isMultimodalInput(input any) bool {
	if input == nil {
		return false
	}
	if list, ok := input.([]interface{}); ok {
		for _, item := range list {
			if m, ok := item.(map[string]interface{}); ok {
				if _, hasImage := m["image"]; hasImage {
					return true
				}
				if _, hasAudio := m["audio"]; hasAudio {
					return true
				}
			}
		}
	}
	return false
}

// parseMultimodalInputs 将 core.EmbeddingRequest.Input 解析为多模态内容切片。
// 支持三种格式：
//   - string → [{Text: s}]
//   - []string → [{Text: s}, ...]
//   - []interface{} 其中每个元素是 map[string]interface{}，提取 text/image/audio/factor
func parseMultimodalInputs(input any) ([]bailianMultimodalContent, error) {
	switch v := input.(type) {
	case string:
		if v == "" {
			return nil, nil
		}
		return []bailianMultimodalContent{{Text: v}}, nil
	case []string:
		contents := make([]bailianMultimodalContent, len(v))
		for i, s := range v {
			contents[i] = bailianMultimodalContent{Text: s}
		}
		return contents, nil
	case []interface{}:
		contents := make([]bailianMultimodalContent, 0, len(v))
		for _, item := range v {
			m, ok := item.(map[string]interface{})
			if !ok {
				// 可能是普通 string
				if s, ok := item.(string); ok {
					contents = append(contents, bailianMultimodalContent{Text: s})
					continue
				}
				return nil, fmt.Errorf("unsupported multimodal input element type: %T", item)
			}
			c := bailianMultimodalContent{}
			if text, ok := m["text"].(string); ok {
				c.Text = text
			}
			if image, ok := m["image"].(string); ok {
				c.Image = image
			}
			if audio, ok := m["audio"].(string); ok {
				c.Audio = audio
			}
			if factor, ok := m["factor"].(float64); ok {
				f := factor
				c.Factor = &f
			}
			// 至少需要 text/image/audio 之一
			if c.Text == "" && c.Image == "" && c.Audio == "" {
				return nil, fmt.Errorf("multimodal content item must have at least one of text/image/audio")
			}
			contents = append(contents, c)
		}
		return contents, nil
	default:
		return nil, fmt.Errorf("unsupported multimodal input type: %T", input)
	}
}

// splitMultimodalByCount 将多模态内容按条数分批次，每批不超过 maxSize 条。
func splitMultimodalByCount(contents []bailianMultimodalContent, maxSize int) [][]bailianMultimodalContent {
	var batches [][]bailianMultimodalContent
	for i := 0; i < len(contents); i += maxSize {
		end := i + maxSize
		if end > len(contents) {
			end = len(contents)
		}
		batches = append(batches, contents[i:end])
	}
	return batches
}

// extractTextEmbeddingParams 从 core.EmbeddingRequest 提取 text-embedding 参数。
// 参数来源：Dimensions 是标准字段，其余从 ExtraFields 读取。
func extractTextEmbeddingParams(req *core.EmbeddingRequest) *bailianTextEmbeddingParams {
	params := &bailianTextEmbeddingParams{}
	hasParams := false

	if req.Dimensions != nil {
		params.Dimension = req.Dimensions
		hasParams = true
	}

	if v := req.ExtraFields.Lookup("text_type"); v != nil {
		var s string
		if json.Unmarshal(v, &s) == nil {
			params.TextType = s
			hasParams = true
		}
	}
	if v := req.ExtraFields.Lookup("output_type"); v != nil {
		var s string
		if json.Unmarshal(v, &s) == nil {
			params.OutputType = s
			hasParams = true
		}
	}
	if v := req.ExtraFields.Lookup("instruct"); v != nil {
		var s string
		if json.Unmarshal(v, &s) == nil {
			params.Instruct = s
			hasParams = true
		}
	}

	if !hasParams {
		return nil
	}
	return params
}

// extractMultimodalEmbeddingParams 从 core.EmbeddingRequest 提取 multimodal-embedding 参数。
func extractMultimodalEmbeddingParams(req *core.EmbeddingRequest) *bailianMultimodalEmbeddingParams {
	params := &bailianMultimodalEmbeddingParams{}
	hasParams := false

	if req.Dimensions != nil {
		params.Dimension = req.Dimensions
		hasParams = true
	}

	lookupString := func(key string) string {
		v := req.ExtraFields.Lookup(key)
		if v == nil {
			return ""
		}
		var s string
		if json.Unmarshal(v, &s) == nil {
			return s
		}
		return ""
	}
	lookupFloat := func(key string) *float64 {
		v := req.ExtraFields.Lookup(key)
		if v == nil {
			return nil
		}
		var f float64
		if json.Unmarshal(v, &f) == nil {
			return &f
		}
		return nil
	}
	lookupBool := func(key string) *bool {
		v := req.ExtraFields.Lookup(key)
		if v == nil {
			return nil
		}
		var b bool
		if json.Unmarshal(v, &b) == nil {
			return &b
		}
		return nil
	}
	lookupInt := func(key string) *int {
		v := req.ExtraFields.Lookup(key)
		if v == nil {
			return nil
		}
		var i int
		if json.Unmarshal(v, &i) == nil {
			return &i
		}
		return nil
	}

	if s := lookupString("output_type"); s != "" {
		params.OutputType = s
		hasParams = true
	}
	if s := lookupString("instruct"); s != "" {
		params.Instruct = s
		hasParams = true
	}
	if f := lookupFloat("fps"); f != nil {
		params.FPS = f
		hasParams = true
	}
	if b := lookupBool("enable_fusion"); b != nil {
		params.EnableFusion = b
		hasParams = true
	}
	if i := lookupInt("res_level"); i != nil {
		params.ResLevel = i
		hasParams = true
	}
	if i := lookupInt("max_video_frames"); i != nil {
		params.MaxVideoFrames = i
		hasParams = true
	}

	if !hasParams {
		return nil
	}
	return params
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
	Query     any   `json:"query"`
	Documents []any `json:"documents"`
}

type bailianRerankParameters struct {
	TopN            int      `json:"top_n,omitempty"`
	Instruct        string   `json:"instruct,omitempty"` // 自定义任务指令
	ReturnDocuments *bool    `json:"return_documents,omitempty"`
	FPS             *float64 `json:"fps,omitempty"` // 视频抽帧，仅 VL rerank
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

	params := bailianRerankParameters{TopN: topN}
	if req.ReturnDocuments != nil {
		params.ReturnDocuments = req.ReturnDocuments
	}
	if v := req.ExtraFields.Lookup("instruct"); v != nil {
		var s string
		if json.Unmarshal(v, &s) == nil {
			params.Instruct = s
		}
	}
	if v := req.ExtraFields.Lookup("fps"); v != nil {
		var f float64
		if json.Unmarshal(v, &f) == nil && f > 0 {
			params.FPS = &f
		}
	}

	query := normalizeBailianRerankQuery(req.Query)
	docs := normalizeBailianRerankDocuments(req.Documents)

	bailianReq := bailianRerankReq{
		Model: req.Model,
		Input: bailianRerankInput{
			Query:     query,
			Documents: docs,
		},
		Parameters: params,
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
		if req.ReturnDocuments != nil && *req.ReturnDocuments && r.Index >= 0 && r.Index < len(req.Documents) {
			if text, ok := req.Documents[r.Index].(string); ok {
				results[i].Document = &core.RerankDocument{Text: text}
			} else if m, ok := req.Documents[r.Index].(map[string]any); ok {
				if t, ok := m["text"].(string); ok {
					results[i].Document = &core.RerankDocument{Text: t}
				}
			}
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

// normalizeBailianRerankQuery 将 image_url 收敛为 image（防御性，handler 通常已处理）。
func normalizeBailianRerankQuery(query any) any {
	m, ok := query.(map[string]any)
	if !ok {
		return query
	}
	if _, hasImage := m["image"]; !hasImage {
		if url, ok := m["image_url"].(string); ok && strings.TrimSpace(url) != "" {
			return map[string]any{"image": url}
		}
	}
	return query
}

// normalizeBailianRerankDocuments 兼容硅基流动式混排：
// 当 documents 中出现任意 object 时，把裸 string 统一收成 {text}，避免百炼「类型不一致」。
func normalizeBailianRerankDocuments(docs []any) []any {
	if len(docs) == 0 {
		return docs
	}
	hasObject := false
	for _, d := range docs {
		if _, ok := d.(map[string]any); ok {
			hasObject = true
			break
		}
	}
	if !hasObject {
		return docs
	}

	out := make([]any, len(docs))
	for i, d := range docs {
		switch v := d.(type) {
		case string:
			out[i] = map[string]any{"text": v}
		case map[string]any:
			m := v
			if _, hasImage := m["image"]; !hasImage {
				if url, ok := m["image_url"].(string); ok && strings.TrimSpace(url) != "" {
					m = map[string]any{"image": url}
				}
			}
			out[i] = m
		default:
			out[i] = d
		}
	}
	return out
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
