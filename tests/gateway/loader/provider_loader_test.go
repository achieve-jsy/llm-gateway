package loader_test

import (
	"context"
	"io"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/infra"
	"github.com/example/llm-gateway/internal/gateway/loader"
	gatewaymodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeProvider 模拟 core.Provider 用于测试 ProviderLoader 构建 Router
type fakeProvider struct {
	models []string
}

func (f *fakeProvider) ChatCompletion(_ context.Context, _ *core.ChatRequest) (*core.ChatResponse, error) {
	return nil, nil
}

func (f *fakeProvider) StreamChatCompletion(_ context.Context, _ *core.ChatRequest) (io.ReadCloser, error) {
	return nil, nil
}

func (f *fakeProvider) ListModels(_ context.Context) (*core.ModelsResponse, error) {
	data := make([]core.Model, 0, len(f.models))
	for _, m := range f.models {
		data = append(data, core.Model{ID: m})
	}
	return &core.ModelsResponse{Object: "list", Data: data}, nil
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

func (f *fakeProvider) Rerank(_ context.Context, _ *core.RerankRequest) (*core.RerankResponse, error) {
	return nil, nil
}

// newTestFactory 创建一个注册了 "fake" 类型的 ProviderFactory，用于测试
func newTestFactory() *providers.ProviderFactory {
	factory := providers.NewProviderFactory()
	factory.Add(providers.Registration{
		Type: "fake",
		New: func(cfg providers.ProviderConfig, _ providers.ProviderOptions) core.Provider {
			return &fakeProvider{models: cfg.Models}
		},
	})
	return factory
}

func TestProviderLoader_Router_InitialState(t *testing.T) {
	// 测试目的：ProviderLoader 创建后、未调用 Build 前，Router() 应返回 nil
	pl := loader.NewProviderLoader(newTestFactory(), nil)
	assert.Nil(t, pl.Router(), "首次 Build 前 Router 应为 nil")
}

func TestProviderLoader_Build(t *testing.T) {
	tests := []struct {
		description string
		name        string
		entries     []gatewaymodel.ProviderEntry
		wantModels  int
		wantErr     bool
	}{
		{
			description: "传入有效 ProviderEntry 列表，Build 应返回非 nil Router 且模型数正确",
			name:        "有效条目构建成功",
			entries: []gatewaymodel.ProviderEntry{
				{
					ID:          "p1",
					Name:        "provider-1",
					Type:        "fake",
					Endpoint:    "http://fake.local",
					Credentials: "sk-test",
					Enabled:     true,
					Models:      []string{"model-a", "model-b"},
				},
			},
			wantModels: 2,
			wantErr:    false,
		},
		{
			description: "传入空切片，Build 仍应成功但 Router 内无模型",
			name:        "空条目列表",
			entries:     []gatewaymodel.ProviderEntry{},
			wantModels:  0,
			wantErr:     false,
		},
		{
			description: "条目 Enabled=false 时应跳过，不注册模型",
			name:        "禁用条目被跳过",
			entries: []gatewaymodel.ProviderEntry{
				{
					ID:          "p1",
					Name:        "disabled-provider",
					Type:        "fake",
					Endpoint:    "http://fake.local",
					Credentials: "sk-test",
					Enabled:     false,
					Models:      []string{"model-x"},
				},
			},
			wantModels: 0,
			wantErr:    false,
		},
		{
			description: "条目 Models 为空时跳过该条目，不注册模型",
			name:        "无模型的条目被跳过",
			entries: []gatewaymodel.ProviderEntry{
				{
					ID:          "p1",
					Name:        "empty-models",
					Type:        "fake",
					Endpoint:    "http://fake.local",
					Credentials: "sk-test",
					Enabled:     true,
					Models:      []string{},
				},
			},
			wantModels: 0,
			wantErr:    false,
		},
		{
			description: "未知 provider type 的条目应被跳过（factory 创建失败），其余正常注册",
			name:        "未知类型被跳过",
			entries: []gatewaymodel.ProviderEntry{
				{
					ID:          "p1",
					Name:        "unknown-provider",
					Type:        "nonexistent",
					Endpoint:    "http://fake.local",
					Credentials: "sk-test",
					Enabled:     true,
					Models:      []string{"model-z"},
				},
				{
					ID:          "p2",
					Name:        "good-provider",
					Type:        "fake",
					Endpoint:    "http://fake.local",
					Credentials: "sk-test",
					Enabled:     true,
					Models:      []string{"model-ok"},
				},
			},
			wantModels: 1,
			wantErr:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pl := loader.NewProviderLoader(newTestFactory(), nil)
			router, err := pl.Build(tc.entries)

			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, router)
			assert.Equal(t, tc.wantModels, router.ModelCount())

			// Build 后 Router() 应返回同一个 Router 实例
			assert.Equal(t, router, pl.Router())
		})
	}
}

func TestProviderLoader_Build_MultipleProviders(t *testing.T) {
	// 测试目的：注册多个不同 provider，Router 能正确匹配各自的模型
	factory := providers.NewProviderFactory()
	factory.Add(providers.Registration{
		Type: "fake",
		New: func(cfg providers.ProviderConfig, _ providers.ProviderOptions) core.Provider {
			return &fakeProvider{models: cfg.Models}
		},
	})
	factory.Add(providers.Registration{
		Type: "fake2",
		New: func(cfg providers.ProviderConfig, _ providers.ProviderOptions) core.Provider {
			return &fakeProvider{models: cfg.Models}
		},
	})

	entries := []gatewaymodel.ProviderEntry{
		{
			ID: "p1", Name: "provider-alpha", Type: "fake",
			Endpoint: "http://alpha.local", Credentials: "sk-a",
			Enabled: true, Models: []string{"gpt-4o", "gpt-4o-mini"},
		},
		{
			ID: "p2", Name: "provider-beta", Type: "fake2",
			Endpoint: "http://beta.local", Credentials: "sk-b",
			Enabled: true, Models: []string{"claude-3"},
		},
	}

	pl := loader.NewProviderLoader(factory, nil)
	router, err := pl.Build(entries)
	require.NoError(t, err)
	require.NotNil(t, router)

	// 共 3 个模型
	assert.Equal(t, 3, router.ModelCount())

	// Router 能识别所有已注册模型
	assert.True(t, router.Supports("gpt-4o"), "Router 应支持 gpt-4o")
	assert.True(t, router.Supports("gpt-4o-mini"), "Router 应支持 gpt-4o-mini")
	assert.True(t, router.Supports("claude-3"), "Router 应支持 claude-3")
	assert.False(t, router.Supports("unknown-model"), "Router 不应支持未注册模型")
}

func TestProviderLoader_Build_AtomicSwap(t *testing.T) {
	// 测试目的：第二次 Build 成功后 Router 被原子替换，旧 Router 实例不受影响
	pl := loader.NewProviderLoader(newTestFactory(), nil)

	// 首次构建
	entriesV1 := []gatewaymodel.ProviderEntry{
		{
			ID: "p1", Name: "v1-provider", Type: "fake",
			Endpoint: "http://v1.local", Credentials: "sk-v1",
			Enabled: true, Models: []string{"model-v1"},
		},
	}
	routerV1, err := pl.Build(entriesV1)
	require.NoError(t, err)
	require.NotNil(t, routerV1)
	assert.Equal(t, 1, routerV1.ModelCount())

	// 保留旧 Router 的引用
	oldRouter := pl.Router()
	assert.Equal(t, routerV1, oldRouter)

	// 第二次构建（更多模型）
	entriesV2 := []gatewaymodel.ProviderEntry{
		{
			ID: "p1", Name: "v2-provider", Type: "fake",
			Endpoint: "http://v2.local", Credentials: "sk-v2",
			Enabled: true, Models: []string{"model-v2-a", "model-v2-b", "model-v2-c"},
		},
	}
	routerV2, err := pl.Build(entriesV2)
	require.NoError(t, err)
	require.NotNil(t, routerV2)
	assert.Equal(t, 3, routerV2.ModelCount())

	// pl.Router() 现在应指向新 Router
	assert.Equal(t, routerV2, pl.Router())
	assert.NotEqual(t, oldRouter, pl.Router())

	// 旧 Router 实例仍然有效（1 个模型）
	assert.Equal(t, 1, oldRouter.ModelCount())
}

func TestProviderLoader_Build_FailurePreservesOldRouter(t *testing.T) {
	// 测试目的：当新 entries 中全部条目都是不可识别类型且 Build 不返回 error 时，
	// Router 仍会被替换为空 Router（Build 本身总成功，返回 0 model 的 Router）。
	// 但如果 Build 本身返回 error（即 NewRouter 失败），旧 Router 应被保留。
	//
	// 注意：当前实现中 NewRouter 只在 lookup==nil 时返回 error，而 Build 总是
	// 创建一个 registry（即使没有 provider），所以 Build 实际上不会返回 error。
	// 本测试验证：即使所有 entries 构建失败，返回的 Router 仍是一个有效（但空）的 Router。
	pl := loader.NewProviderLoader(newTestFactory(), nil)

	// 先成功构建一个有 1 个模型的 Router
	entriesGood := []gatewaymodel.ProviderEntry{
		{
			ID: "p1", Name: "good", Type: "fake",
			Endpoint: "http://good.local", Credentials: "sk-good",
			Enabled: true, Models: []string{"good-model"},
		},
	}
	routerGood, err := pl.Build(entriesGood)
	require.NoError(t, err)
	assert.Equal(t, 1, routerGood.ModelCount())

	// 再用全部未知类型条目构建（所有 entry 被跳过）
	entriesBad := []gatewaymodel.ProviderEntry{
		{
			ID: "p1", Name: "bad", Type: "nonexistent",
			Endpoint: "http://bad.local", Credentials: "sk-bad",
			Enabled: true, Models: []string{"bad-model"},
		},
	}
	routerBad, err := pl.Build(entriesBad)
	require.NoError(t, err)
	// Build 成功但 Router 内无模型
	assert.Equal(t, 0, routerBad.ModelCount())

	// Router 已被替换为空 Router（这是预期行为：Build 成功则替换）
	assert.Equal(t, routerBad, pl.Router())
}

func TestProviderLoader_HandleChangeEvent(t *testing.T) {
	// 测试目的：HandleChangeEvent 返回的回调在被调用时应使用 fetchEntries 获取最新配置并重建 Router
	pl := loader.NewProviderLoader(newTestFactory(), nil)

	// 先构建初始 Router
	initial := []gatewaymodel.ProviderEntry{
		{
			ID: "p1", Name: "initial", Type: "fake",
			Endpoint: "http://init.local", Credentials: "sk-init",
			Enabled: true, Models: []string{"init-model"},
		},
	}
	_, err := pl.Build(initial)
	require.NoError(t, err)
	assert.Equal(t, 1, pl.Router().ModelCount())

	// 模拟 fetchEntries 返回新配置
	updated := []gatewaymodel.ProviderEntry{
		{
			ID: "p1", Name: "updated", Type: "fake",
			Endpoint: "http://updated.local", Credentials: "sk-new",
			Enabled: true, Models: []string{"new-model-a", "new-model-b"},
		},
	}
	fetchEntries := func() ([]gatewaymodel.ProviderEntry, error) {
		return updated, nil
	}

	// 获取回调并触发
	callback := pl.HandleChangeEvent(fetchEntries)
	callback(infra.ChangeEvent{
		Type:         "update",
		ResourceType: "provider",
	})

	// Router 应已更新
	assert.Equal(t, 2, pl.Router().ModelCount())
	assert.True(t, pl.Router().Supports("new-model-a"))
	assert.True(t, pl.Router().Supports("new-model-b"))
}

func TestProviderLoader_HandleChangeEvent_FetchError(t *testing.T) {
	// 测试目的：fetchEntries 返回错误时，旧 Router 应被保留不替换
	pl := loader.NewProviderLoader(newTestFactory(), nil)

	initial := []gatewaymodel.ProviderEntry{
		{
			ID: "p1", Name: "stable", Type: "fake",
			Endpoint: "http://stable.local", Credentials: "sk-stable",
			Enabled: true, Models: []string{"stable-model"},
		},
	}
	_, err := pl.Build(initial)
	require.NoError(t, err)
	originalRouter := pl.Router()

	// fetchEntries 模拟失败
	fetchEntries := func() ([]gatewaymodel.ProviderEntry, error) {
		return nil, assert.AnError
	}

	callback := pl.HandleChangeEvent(fetchEntries)
	callback(infra.ChangeEvent{
		Type:         "update",
		ResourceType: "provider",
	})

	// Router 应未变化
	assert.Equal(t, originalRouter, pl.Router())
	assert.Equal(t, 1, pl.Router().ModelCount())
}

func TestProviderLoader_OnReloadCallback(t *testing.T) {
	// 测试目的：Build 成功后通过 HandleChangeEvent 触发时，onReload 回调应被调用
	var reloadedRouter *providers.Router
	onReload := func(router *providers.Router, _ []gatewaymodel.ProviderEntry) {
		reloadedRouter = router
	}

	pl := loader.NewProviderLoader(newTestFactory(), onReload)

	entries := []gatewaymodel.ProviderEntry{
		{
			ID: "p1", Name: "reload-test", Type: "fake",
			Endpoint: "http://reload.local", Credentials: "sk-reload",
			Enabled: true, Models: []string{"reload-model"},
		},
	}
	fetchEntries := func() ([]gatewaymodel.ProviderEntry, error) {
		return entries, nil
	}

	callback := pl.HandleChangeEvent(fetchEntries)
	callback(infra.ChangeEvent{Type: "update", ResourceType: "provider"})

	// onReload 应已被调用，且传入了新 Router
	require.NotNil(t, reloadedRouter)
	assert.Equal(t, pl.Router(), reloadedRouter)
	assert.Equal(t, 1, reloadedRouter.ModelCount())
}

func TestProviderLoader_Initialize(t *testing.T) {
	// 测试目的：Initialize 是 Build 的包装，行为应与 Build 一致
	pl := loader.NewProviderLoader(newTestFactory(), nil)

	entries := []gatewaymodel.ProviderEntry{
		{
			ID: "p1", Name: "init-provider", Type: "fake",
			Endpoint: "http://init.local", Credentials: "sk-init",
			Enabled: true, Models: []string{"init-a", "init-b"},
		},
	}

	router, err := pl.Initialize(context.Background(), entries)
	require.NoError(t, err)
	require.NotNil(t, router)
	assert.Equal(t, 2, router.ModelCount())
	assert.Equal(t, router, pl.Router())
}
