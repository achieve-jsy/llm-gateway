package volcengine

import (
	"context"
	"io"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers"
)

// Registration 注册信息
var Registration = providers.Registration{
	Type: "seedance-volcengine",
	New:  NewProvider,
	Discovery: providers.DiscoveryConfig{
		DefaultBaseURL: "https://ark.cn-beijing.volces.com",
		RequireBaseURL: true,
	},
}

// NewProvider 创建 Provider（实现 core.Provider 接口）
func NewProvider(cfg providers.ProviderConfig, opts providers.ProviderOptions) core.Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://ark.cn-beijing.volces.com"
	}
	adapter := NewAdapter(cfg.Type, baseURL, cfg.APIKey, "", "cn-beijing")
	return &Provider{
		adapter: adapter,
	}
}

// Provider 包装 Adapter，实现 core.Provider 接口
type Provider struct {
	adapter *Adapter
}

// ChatCompletion 返回不支持错误
func (p *Provider) ChatCompletion(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	return nil, core.NewInvalidRequestError("seedance-volcengine does not support chat completion", nil)
}

// StreamChatCompletion 返回不支持错误
func (p *Provider) StreamChatCompletion(ctx context.Context, req *core.ChatRequest) (io.ReadCloser, error) {
	return nil, core.NewInvalidRequestError("seedance-volcengine does not support stream chat", nil)
}

// ListModels 返回空列表
func (p *Provider) ListModels(ctx context.Context) (*core.ModelsResponse, error) {
	return &core.ModelsResponse{Data: []core.Model{}}, nil
}

// Embeddings 返回不支持错误
func (p *Provider) Embeddings(ctx context.Context, req *core.EmbeddingRequest) (*core.EmbeddingResponse, error) {
	return nil, core.NewInvalidRequestError("seedance-volcengine does not support embeddings", nil)
}

// Rerank 返回不支持错误
func (p *Provider) Rerank(ctx context.Context, req *core.RerankRequest) (*core.RerankResponse, error) {
	return nil, core.NewInvalidRequestError("seedance-volcengine does not support rerank", nil)
}

// Responses 返回不支持错误
func (p *Provider) Responses(ctx context.Context, req *core.ResponsesRequest) (*core.ResponsesResponse, error) {
	return nil, core.NewInvalidRequestError("seedance-volcengine does not support responses", nil)
}

// StreamResponses 返回不支持错误
func (p *Provider) StreamResponses(ctx context.Context, req *core.ResponsesRequest) (io.ReadCloser, error) {
	return nil, core.NewInvalidRequestError("seedance-volcengine does not support stream responses", nil)
}