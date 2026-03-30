// Package providers 测试 Router.Rerank 路由逻辑。
// 使用手写 mockLookup 和 mockProvider 模拟 core.ModelLookup 和 core.Provider 接口。
package providers

import (
	"context"
	"io"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// mockProvider — 模拟 core.Provider，所有方法默认返回 nil。Rerank 可配置返回值。
// ---------------------------------------------------------------------------

type mockProvider struct {
	name       string
	ptype      string
	rerankResp *core.RerankResponse
	rerankErr  error
	// rerankFunc 如果设置，则 Rerank 调用此函数，否则返回 rerankResp/rerankErr。
	// 用于捕获转发后的请求，验证 provider hint 剥离等行为。
	rerankFunc func(ctx context.Context, req *core.RerankRequest) (*core.RerankResponse, error)
}

func (m *mockProvider) ChatCompletion(_ context.Context, _ *core.ChatRequest) (*core.ChatResponse, error) {
	return nil, nil
}

func (m *mockProvider) StreamChatCompletion(_ context.Context, _ *core.ChatRequest) (io.ReadCloser, error) {
	return nil, nil
}

func (m *mockProvider) ListModels(_ context.Context) (*core.ModelsResponse, error) {
	return nil, nil
}

func (m *mockProvider) Responses(_ context.Context, _ *core.ResponsesRequest) (*core.ResponsesResponse, error) {
	return nil, nil
}

func (m *mockProvider) StreamResponses(_ context.Context, _ *core.ResponsesRequest) (io.ReadCloser, error) {
	return nil, nil
}

func (m *mockProvider) Embeddings(_ context.Context, _ *core.EmbeddingRequest) (*core.EmbeddingResponse, error) {
	return nil, nil
}

func (m *mockProvider) Rerank(ctx context.Context, req *core.RerankRequest) (*core.RerankResponse, error) {
	if m.rerankFunc != nil {
		return m.rerankFunc(ctx, req)
	}
	if m.rerankResp != nil || m.rerankErr != nil {
		return m.rerankResp, m.rerankErr
	}
	return nil, nil
}

// ---------------------------------------------------------------------------
// mockLookup — 模拟 core.ModelLookup，同时实现 modelWithProviderLister 以支持
// qualified selector 解析。控制模型到 Provider 的映射关系。
// ---------------------------------------------------------------------------

// lookupEntry 保存一个模型的 Provider、实例名、类型和裸模型 ID。
type lookupEntry struct {
	provider     core.Provider
	providerName string
	providerType string
	bareModelID  string
}

type mockLookup struct {
	// entries 按 "providerName/modelId" 的 qualified 形式索引
	entries map[string]*lookupEntry
	// bareToProvider 记录裸 model ID → 首个注册的 provider 名称，供 unqualified 解析用
	bareToProvider map[string]string
}

func newMockLookup() *mockLookup {
	return &mockLookup{
		entries:        make(map[string]*lookupEntry),
		bareToProvider: make(map[string]string),
	}
}

// addModel 注册一个模型到指定 provider。同模型 ID 的首个注册者作为 unqualified 解析的默认 provider。
func (m *mockLookup) addModel(bareModelID, providerName, providerType string, provider core.Provider) {
	qualified := providerName + "/" + bareModelID
	m.entries[qualified] = &lookupEntry{
		provider:     provider,
		providerName: providerName,
		providerType: providerType,
		bareModelID:  bareModelID,
	}
	if _, exists := m.bareToProvider[bareModelID]; !exists {
		m.bareToProvider[bareModelID] = providerName
	}
}

// ---- core.ModelLookup 接口实现 ----

func (m *mockLookup) Supports(model string) bool {
	_, ok := m.entries[model]
	return ok
}

func (m *mockLookup) GetProvider(model string) core.Provider {
	if e, ok := m.entries[model]; ok {
		return e.provider
	}
	return nil
}

func (m *mockLookup) GetProviderType(model string) string {
	if e, ok := m.entries[model]; ok {
		return e.providerType
	}
	return ""
}

func (m *mockLookup) GetProviderName(model string) string {
	if p, ok := m.bareToProvider[model]; ok {
		return p
	}
	return ""
}

func (m *mockLookup) GetProviderNameForType(_ string) string { return "" }

func (m *mockLookup) GetProviderTypeForName(_ string) string { return "" }

func (m *mockLookup) ListModels() []core.Model { return nil }

func (m *mockLookup) ModelCount() int {
	return len(m.bareToProvider)
}

// ---- modelWithProviderLister 接口实现 ----
// resolveQualifiedSelector 依赖此接口进行 provider-hint 路由匹配。

func (m *mockLookup) ListModelsWithProvider() []ModelWithProvider {
	result := make([]ModelWithProvider, 0, len(m.entries))
	for _, entry := range m.entries {
		result = append(result, ModelWithProvider{
			ProviderName: entry.providerName,
			ProviderType: entry.providerType,
			Model: core.Model{
				ID: entry.bareModelID,
			},
		})
	}
	return result
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestRouterRerankSuccess 验证正常路由：注册模型后，Rerank 找到对应 Provider 并返回正确结果。
func TestRouterRerankSuccess(t *testing.T) {
	expectedResp := &core.RerankResponse{
		ID:    "rerank-123",
		Model: "rerank-model",
		Results: []core.RerankResult{
			{Index: 0, RelevanceScore: 0.95},
			{Index: 1, RelevanceScore: 0.42},
		},
		Usage: core.RerankUsage{TotalTokens: 128},
	}

	mp := &mockProvider{
		name:       "openai",
		ptype:      "openai",
		rerankResp: expectedResp,
	}

	lookup := newMockLookup()
	lookup.addModel("rerank-model", "openai", "openai", mp)

	router, err := NewRouter(lookup)
	require.NoError(t, err)

	req := &core.RerankRequest{
		Model:     "rerank-model",
		Query:     "What is the capital of France?",
		Documents: []any{"Paris is the capital of France.", "London is the capital of England."},
	}

	resp, err := router.Rerank(context.Background(), req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "rerank-123", resp.ID)
	assert.Equal(t, 2, len(resp.Results))
	assert.Equal(t, 0.95, resp.Results[0].RelevanceScore)
	assert.Equal(t, 0.42, resp.Results[1].RelevanceScore)
}

// TestRouterRerankProviderHint 验证 provider hint 被正确剥离：
// 请求中的 Provider 字段被清除后才转发给上游 Provider，且路由仍到达正确的 Provider。
func TestRouterRerankProviderHint(t *testing.T) {
	var capturedReq *core.RerankRequest

	mp := &mockProvider{
		name:  "my-openai",
		ptype: "openai",
		rerankFunc: func(ctx context.Context, req *core.RerankRequest) (*core.RerankResponse, error) {
			capturedReq = req
			return &core.RerankResponse{
				ID:    "rerank-hint",
				Model: "rerank-model",
			}, nil
		},
	}

	lookup := newMockLookup()
	lookup.addModel("rerank-model", "my-openai", "openai", mp)

	router, err := NewRouter(lookup)
	require.NoError(t, err)

	req := &core.RerankRequest{
		Model:     "rerank-model",
		Provider:  "openai", // 内部路由 hint，应被剥离
		Query:     "test query",
		Documents: []any{"doc1", "doc2"},
	}

	resp, err := router.Rerank(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// 验证原始请求的 Provider 字段未被修改（路由复制了请求）
	assert.Equal(t, "openai", req.Provider, "原始请求的 Provider 字段不应被修改")

	// 验证路由成功到达正确的 Provider
	assert.Equal(t, "rerank-hint", resp.ID)

	// 验证转发后的请求中 Provider 已被剥离
	require.NotNil(t, capturedReq, "应捕获转发后的请求")
	assert.Equal(t, "", capturedReq.Provider, "转发请求的 Provider 应被清空")
	assert.Equal(t, "rerank-model", capturedReq.Model, "转发请求的 Model 应为裸模型 ID")
}

// TestRouterRerankModelNotFound 验证查询未注册的模型时返回错误。
func TestRouterRerankModelNotFound(t *testing.T) {
	mp := &mockProvider{
		name:  "openai",
		ptype: "openai",
	}

	lookup := newMockLookup()
	lookup.addModel("rerank-model", "openai", "openai", mp)

	router, err := NewRouter(lookup)
	require.NoError(t, err)

	req := &core.RerankRequest{
		Model:     "nonexistent-model",
		Query:     "test",
		Documents: []any{"doc1"},
	}

	resp, err := router.Rerank(context.Background(), req)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "model not found")
}

// TestRouterRerankStampProvider 验证 RerankResponse 的 Provider 字段被正确回填
// 为 provider type，而非 provider name。
func TestRouterRerankStampProvider(t *testing.T) {
	mp := &mockProvider{
		name:  "my-openai",
		ptype: "openai",
		rerankResp: &core.RerankResponse{
			ID:      "rerank-stamp",
			Model:   "rerank-model",
			Results: []core.RerankResult{},
			Usage:   core.RerankUsage{},
		},
	}

	lookup := newMockLookup()
	// provider name 故意与 provider type 不同，以验证 stamp 使用的是 type
	lookup.addModel("rerank-model", "my-openai", "openai", mp)

	router, err := NewRouter(lookup)
	require.NoError(t, err)

	req := &core.RerankRequest{
		Model:     "rerank-model",
		Query:     "test",
		Documents: []any{"doc1"},
	}

	resp, err := router.Rerank(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// stampProvider 应使用 provider type（"openai"），而非 provider name（"my-openai"）
	assert.Equal(t, "openai", resp.Provider, "response.Provider 应为 provider type，而非 provider name")
}

// TestRouterRerankEmptyRegistry 验证注册表为空时调用 Rerank 返回 ErrRegistryNotInitialized 错误。
func TestRouterRerankEmptyRegistry(t *testing.T) {
	lookup := newMockLookup() // 空 lookup，ModelCount == 0
	router, err := NewRouter(lookup)
	require.NoError(t, err)

	req := &core.RerankRequest{
		Model:     "rerank-model",
		Query:     "test",
		Documents: []any{"doc1"},
	}

	resp, err := router.Rerank(context.Background(), req)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "model registry has no models")
}