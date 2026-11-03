// Package providers_test 测试 internal/providers 的 Router 模型查找功能。
// 使用手写 fakeLookup 模拟 core.ModelLookup 接口，不依赖真实的 ModelRegistry。
package providers_test

import (
	"context"
	"io"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// fakeProvider — 模拟 LLM 厂商 Provider，所有业务方法返回 nil
// ---------------------------------------------------------------------------

type fakeProvider struct {
	name  string
	ptype string
}

func (f *fakeProvider) ChatCompletion(_ context.Context, _ *core.ChatRequest) (*core.ChatResponse, error) {
	return nil, nil
}
func (f *fakeProvider) StreamChatCompletion(_ context.Context, _ *core.ChatRequest) (io.ReadCloser, error) {
	return nil, nil
}
func (f *fakeProvider) ListModels(_ context.Context) (*core.ModelsResponse, error) {
	return nil, nil
}
func (f *fakeProvider) Responses(_ context.Context, _ *core.ResponsesRequest) (*core.ResponsesResponse, error) {
	return nil, nil
}
func (f *fakeProvider) StreamResponses(_ context.Context, _ *core.ResponsesRequest) (io.ReadCloser, error) {
	return nil, nil
}
func (f *fakeProvider) Embeddings(_ context.Context, _ *core.EmbeddingRequest) (*core.EmbeddingResponse, error) {
	return nil, nil
}

// ---------------------------------------------------------------------------
// fakeLookup — 模拟 core.ModelLookup，控制模型→Provider 的映射关系
// ---------------------------------------------------------------------------

// lookupEntry 保存一个模型的 Provider、实例名和类型
type lookupEntry struct {
	provider     core.Provider
	providerName string
	providerType string
}

type fakeLookup struct {
	// entries 按 "providerName/modelId" 的 qualified 形式索引
	entries map[string]*lookupEntry
	// bareToProvider 记录裸 model ID → 首个注册的 provider 名称，供 unqualified 解析用
	bareToProvider map[string]string
}

func newFakeLookup() *fakeLookup {
	return &fakeLookup{
		entries:        make(map[string]*lookupEntry),
		bareToProvider: make(map[string]string),
	}
}

// addModel 注册一个模型到指定 provider。同模型 ID 的首个注册者作为 unqualified 解析的默认 provider。
// qualifiedID = providerName + "/" + bareModelID。
func (f *fakeLookup) addModel(bareModelID, providerName, providerType string, provider core.Provider) {
	qualified := providerName + "/" + bareModelID
	f.entries[qualified] = &lookupEntry{
		provider:     provider,
		providerName: providerName,
		providerType: providerType,
	}
	if _, exists := f.bareToProvider[bareModelID]; !exists {
		f.bareToProvider[bareModelID] = providerName
	}
}

// Supports 实现 core.ModelLookup，检查 qualified model ID 是否存在
func (f *fakeLookup) Supports(model string) bool {
	_, ok := f.entries[model]
	return ok
}

// GetProvider 实现 core.ModelLookup，返回 qualified model ID 对应的 Provider
func (f *fakeLookup) GetProvider(model string) core.Provider {
	if e, ok := f.entries[model]; ok {
		return e.provider
	}
	return nil
}

// GetProviderType 实现 core.ModelLookup，返回 qualified model ID 对应的 provider type
func (f *fakeLookup) GetProviderType(model string) string {
	if e, ok := f.entries[model]; ok {
		return e.providerType
	}
	return ""
}

// GetProviderName 实现 core.ModelLookup，返回 bare model ID 对应的首个 provider 名称
func (f *fakeLookup) GetProviderName(model string) string {
	if p, ok := f.bareToProvider[model]; ok {
		return p
	}
	return ""
}

// GetProviderNameForType 实现 core.ModelLookup
func (f *fakeLookup) GetProviderNameForType(_ string) string { return "" }

// GetProviderTypeForName 实现 core.ModelLookup
func (f *fakeLookup) GetProviderTypeForName(_ string) string { return "" }

// ListModels 实现 core.ModelLookup
func (f *fakeLookup) ListModels() []core.Model { return nil }

// ModelCount 实现 core.ModelLookup
func (f *fakeLookup) ModelCount() int {
	return len(f.bareToProvider)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestRouter_Supports(t *testing.T) {
	tests := []struct {
		name        string
		description string // 中文：测什么、关键条件、期望结果
		model       string
		setup       func(*fakeLookup) // 注册模型到 fakeLookup
		want        bool
	}{
		{
			name:        "注册 model 后 Supports 返回 true",
			description: "将 gpt-4 注册到 openai provider，验证 Router.Supports('gpt-4') 返回 true",
			model:       "gpt-4",
			setup: func(l *fakeLookup) {
				l.addModel("gpt-4", "openai", "openai", &fakeProvider{name: "openai", ptype: "openai"})
			},
			want: true,
		},
		{
			name:        "未注册 model 的 Supports 返回 false",
			description: "仅注册 gpt-4，查询不存在的 claude-3，Supports 应返回 false",
			model:       "claude-3",
			setup: func(l *fakeLookup) {
				l.addModel("gpt-4", "openai", "openai", &fakeProvider{name: "openai", ptype: "openai"})
			},
			want: false,
		},
		{
			name:        "空注册表时 Supports 返回 false",
			description: "lookup 中没有任何模型，Router.ModelCount()==0 导致 checkReady 返回 ErrRegistryNotInitialized，Supports 应返回 false",
			model:       "gpt-4",
			setup:       nil,
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookup := newFakeLookup()
			if tt.setup != nil {
				tt.setup(lookup)
			}
			router, err := providers.NewRouter(lookup)
			require.NoError(t, err)

			got := router.Supports(tt.model)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRouter_GetProviderName(t *testing.T) {
	p1 := &fakeProvider{name: "my-openai", ptype: "openai"}

	tests := []struct {
		name        string
		description string
		model       string
		setup       func(*fakeLookup)
		want        string
	}{
		{
			name:        "GetProviderName 返回正确的 Provider 实例名",
			description: "gpt-4 注册到 my-openai provider，GetProviderName('gpt-4') 应返回 'my-openai'",
			model:       "gpt-4",
			setup: func(l *fakeLookup) {
				l.addModel("gpt-4", "my-openai", "openai", p1)
			},
			want: "my-openai",
		},
		{
			name:        "未注册 model 返回空字符串",
			description: "查询不存在的模型，GetProviderName 应返回空字符串",
			model:       "unknown-model",
			setup: func(l *fakeLookup) {
				l.addModel("gpt-4", "my-openai", "openai", p1)
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookup := newFakeLookup()
			if tt.setup != nil {
				tt.setup(lookup)
			}
			router, err := providers.NewRouter(lookup)
			require.NoError(t, err)

			got := router.GetProviderName(tt.model)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRouter_GetProviderType(t *testing.T) {
	tests := []struct {
		name        string
		description string
		model       string
		setup       func(*fakeLookup)
		want        string
	}{
		{
			name:        "GetProviderType 返回正确的 provider type",
			description: "gpt-4 注册到 openai provider（type='openai'），GetProviderType('gpt-4') 应返回 'openai'",
			model:       "gpt-4",
			setup: func(l *fakeLookup) {
				l.addModel("gpt-4", "openai", "openai", &fakeProvider{name: "openai", ptype: "openai"})
			},
			want: "openai",
		},
		{
			name:        "未注册 model 返回空字符串",
			description: "查询不存在的模型，GetProviderType 应返回空字符串",
			model:       "unknown-model",
			setup: func(l *fakeLookup) {
				l.addModel("gpt-4", "openai", "openai", &fakeProvider{name: "openai", ptype: "openai"})
			},
			want: "",
		},
		{
			name:        "不同 provider type 正确返回",
			description: "claude-3 注册到 anthropic provider（type='anthropic'），GetProviderType('claude-3') 应返回 'anthropic'",
			model:       "claude-3",
			setup: func(l *fakeLookup) {
				l.addModel("claude-3", "anthropic", "anthropic", &fakeProvider{name: "anthropic", ptype: "anthropic"})
			},
			want: "anthropic",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookup := newFakeLookup()
			if tt.setup != nil {
				tt.setup(lookup)
			}
			router, err := providers.NewRouter(lookup)
			require.NoError(t, err)

			got := router.GetProviderType(tt.model)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRouter_MultipleModelsSameProvider(t *testing.T) {
	// 测试目的：同一 provider 注册多个 model，所有 model 都应能被正确识别
	p := &fakeProvider{name: "openai", ptype: "openai"}

	lookup := newFakeLookup()
	lookup.addModel("gpt-4", "openai", "openai", p)
	lookup.addModel("gpt-4-turbo", "openai", "openai", p)
	lookup.addModel("gpt-4o", "openai", "openai", p)

	router, err := providers.NewRouter(lookup)
	require.NoError(t, err)

	t.Run("多个 model 在同一 provider 下均被支持", func(t *testing.T) {
		assert.True(t, router.Supports("gpt-4"))
		assert.True(t, router.Supports("gpt-4-turbo"))
		assert.True(t, router.Supports("gpt-4o"))
	})

	t.Run("所有 model 的 provider type 一致", func(t *testing.T) {
		assert.Equal(t, "openai", router.GetProviderType("gpt-4"))
		assert.Equal(t, "openai", router.GetProviderType("gpt-4-turbo"))
		assert.Equal(t, "openai", router.GetProviderType("gpt-4o"))
	})

	t.Run("所有 model 的 provider name 一致", func(t *testing.T) {
		assert.Equal(t, "openai", router.GetProviderName("gpt-4"))
		assert.Equal(t, "openai", router.GetProviderName("gpt-4-turbo"))
		assert.Equal(t, "openai", router.GetProviderName("gpt-4o"))
	})
}

func TestRouter_NewRouter_NilLookup(t *testing.T) {
	// 测试目的：传入 nil lookup 时 NewRouter 应返回 error
	router, err := providers.NewRouter(nil)
	assert.Error(t, err, "nil lookup 应返回 error")
	assert.Nil(t, router, "nil lookup 时 router 应为 nil")
}

func TestRouter_ErrRegistryNotInitialized_ChatCompletion(t *testing.T) {
	// 测试目的：lookup 中无任何模型时，调用 ChatCompletion 应返回 ErrRegistryNotInitialized
	lookup := newFakeLookup() // 空 lookup，ModelCount==0
	router, err := providers.NewRouter(lookup)
	require.NoError(t, err)

	_, err = router.ChatCompletion(context.Background(), &core.ChatRequest{Model: "gpt-4"})
	assert.Error(t, err, "空注册表时 ChatCompletion 应返回 error")
	assert.Contains(t, err.Error(), "model registry has no models", "应包含 ErrRegistryNotInitialized 消息")
}

func TestRouter_ModelCount(t *testing.T) {
	tests := []struct {
		name        string
		description string
		setup       func(*fakeLookup)
		want        int
	}{
		{
			name:        "空 lookup 返回 0",
			description: "无任何模型注册时 ModelCount 应为 0",
			setup:       nil,
			want:        0,
		},
		{
			name:        "注册 1 个模型返回 1",
			description: "注册一个模型后 ModelCount 应为 1",
			setup: func(l *fakeLookup) {
				l.addModel("gpt-4", "openai", "openai", &fakeProvider{name: "openai", ptype: "openai"})
			},
			want: 1,
		},
		{
			name:        "注册 3 个模型返回 3",
			description: "注册三个不同模型后 ModelCount 应为 3",
			setup: func(l *fakeLookup) {
				p := &fakeProvider{name: "openai", ptype: "openai"}
				l.addModel("gpt-4", "openai", "openai", p)
				l.addModel("gpt-4-turbo", "openai", "openai", p)
				l.addModel("gpt-4o", "openai", "openai", p)
			},
			want: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookup := newFakeLookup()
			if tt.setup != nil {
				tt.setup(lookup)
			}
			router, err := providers.NewRouter(lookup)
			require.NoError(t, err)
			assert.Equal(t, tt.want, router.ModelCount())
		})
	}
}

func TestRouter_SameModelMultipleProviders(t *testing.T) {
	// 测试目的：同一 model ID 注册到不同 provider，Router 应使用首个注册的 provider 为默认
	// 同模型 ID 的第二个注册者仍可通过 qualified selector 访问（mock 内部保留两条记录），
	// 但 unqualified 查询（bare model ID）始终返回第一个。
	p1 := &fakeProvider{name: "openai", ptype: "openai"}
	p2 := &fakeProvider{name: "azure", ptype: "azure"}

	lookup := newFakeLookup()
	lookup.addModel("gpt-4", "openai", "openai", p1) // 先注册 openai
	lookup.addModel("gpt-4", "azure", "azure", p2)   // 后注册 azure

	router, err := providers.NewRouter(lookup)
	require.NoError(t, err)

	t.Run("Supports 对共享 model 返回 true", func(t *testing.T) {
		assert.True(t, router.Supports("gpt-4"))
	})

	t.Run("GetProviderName 返回首个注册的 provider", func(t *testing.T) {
		// 首个注册的是 openai
		assert.Equal(t, "openai", router.GetProviderName("gpt-4"))
	})

	t.Run("GetProviderType 返回首个注册的 provider type", func(t *testing.T) {
		// 首个注册的是 openai，类型也是 "openai"
		assert.Equal(t, "openai", router.GetProviderType("gpt-4"))
	})
}