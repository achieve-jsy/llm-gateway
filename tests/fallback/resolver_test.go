package fallback_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/fallback"
	"github.com/example/llm-gateway/internal/llmclientconfig"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- helpers ---

func floatPtr(v float64) *float64 { return &v }
func intPtr(v int) *int           { return &v }

// fakeRegistry 模拟 fallback.Registry 接口，用于测试 Resolver 的候选解析逻辑。
type fakeRegistry struct {
	models map[string]*providers.ModelInfo
}

func (f *fakeRegistry) GetModel(model string) *providers.ModelInfo {
	if f == nil || f.models == nil {
		return nil
	}
	return f.models[model]
}

func (f *fakeRegistry) ListModelsWithProvider() []providers.ModelWithProvider {
	if f == nil || f.models == nil {
		return nil
	}
	result := make([]providers.ModelWithProvider, 0, len(f.models))
	for key, info := range f.models {
		result = append(result, providers.ModelWithProvider{
			Model:        info.Model,
			ProviderType: info.ProviderType,
			ProviderName: info.ProviderName,
			Selector:     key,
		})
	}
	return result
}

// makeModelInfo 创建一个带元数据的 ModelInfo，用于自动匹配候选测试。
func makeModelInfo(providerName, modelID, family string, categories []core.ModelCategory, capabilities map[string]bool, rankings map[string]core.ModelRanking) *providers.ModelInfo {
	return &providers.ModelInfo{
		Model: core.Model{
			ID: modelID,
			Metadata: &core.ModelMetadata{
				Family:       family,
				Categories:   categories,
				Capabilities: capabilities,
				Rankings:     rankings,
			},
		},
		ProviderName: providerName,
		ProviderType: "openai",
	}
}

// makeResolution 创建一个简单的 RequestModelResolution 用于测试。
func makeResolution(model, provider string) *core.RequestModelResolution {
	return &core.RequestModelResolution{
		Requested: core.RequestedModelSelector{
			Model:        model,
			ProviderHint: provider,
		},
		ResolvedSelector: core.ModelSelector{
			Model:    model,
			Provider: provider,
		},
	}
}

// --- ResolveFallbackDefaultMode tests ---

func TestResolveFallbackDefaultMode_Canonicalization(t *testing.T) {
	tests := []struct {
		name        string
		description string
		input       llmclientconfig.FallbackMode
		want        llmclientconfig.FallbackMode
	}{
		{
			name:        "空字符串返回manual默认",
			description: "未设置默认模式时应返回 manual 作为进程默认值",
			input:       "",
			want:        llmclientconfig.FallbackModeManual,
		},
		{
			name:        "已知模式off返回原值",
			description: "显式设置 off 模式时直接返回，不做修改",
			input:       "off",
			want:        llmclientconfig.FallbackModeOff,
		},
		{
			name:        "已知模式auto返回原值",
			description: "显式设置 auto 模式时直接返回，不做修改",
			input:       "auto",
			want:        llmclientconfig.FallbackModeAuto,
		},
		{
			name:        "已知模式manual返回原值",
			description: "显式设置 manual 模式时直接返回，不做修改",
			input:       "manual",
			want:        llmclientconfig.FallbackModeManual,
		},
		{
			name:        "大小写规范化",
			description: "输入 AUTO 应规范化为小写 auto",
			input:       "AUTO",
			want:        llmclientconfig.FallbackModeAuto,
		},
		{
			name:        "混合大小写规范化",
			description: "输入 Manual 应规范化为小写 manual",
			input:       "Manual",
			want:        llmclientconfig.FallbackModeManual,
		},
		{
			name:        "前后空格去除",
			description: "输入带前后空格的 ' auto ' 应去除空格后返回 auto",
			input:       "  auto  ",
			want:        llmclientconfig.FallbackModeAuto,
		},
		{
			name:        "仅空格返回manual默认",
			description: "仅包含空格的输入等同于空字符串，应返回 manual 默认",
			input:       "   ",
			want:        llmclientconfig.FallbackModeManual,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fallback.ResolveFallbackDefaultMode(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

// --- NewResolver tests ---

func TestNewResolver_Construction(t *testing.T) {
	registry := &fakeRegistry{models: map[string]*providers.ModelInfo{
		"openai/gpt-4o": makeModelInfo("openai", "gpt-4o", "gpt", nil, nil, nil),
	}}

	tests := []struct {
		name        string
		description string
		cfg         llmclientconfig.FallbackConfig
		registry    fallback.Registry
		wantNil     bool
	}{
		{
			name:        "off模式且无manual和overrides返回nil",
			description: "当模式为 off 且没有任何 manual 或 overrides 配置时，fallback 完全禁用，返回 nil",
			cfg: llmclientconfig.FallbackConfig{
				DefaultMode: llmclientconfig.FallbackModeOff,
			},
			registry: registry,
			wantNil:  true,
		},
		{
			name:        "off模式但有manual配置返回non-nil",
			description: "即使全局模式为 off，只要有 manual 映射存在，仍需创建 Resolver 以处理 per-model fallback",
			cfg: llmclientconfig.FallbackConfig{
				DefaultMode: llmclientconfig.FallbackModeOff,
				Manual:      map[string][]string{"gpt-4o": {"gpt-3.5-turbo"}},
			},
			registry: registry,
			wantNil:  false,
		},
		{
			name:        "off模式但有overrides配置返回non-nil",
			description: "即使全局模式为 off，只要有 overrides 存在，仍需创建 Resolver 以处理 per-model 模式覆盖",
			cfg: llmclientconfig.FallbackConfig{
				DefaultMode: llmclientconfig.FallbackModeOff,
				Overrides: map[string]llmclientconfig.FallbackModelOverride{
					"gpt-4o": {Mode: llmclientconfig.FallbackModeAuto},
				},
			},
			registry: registry,
			wantNil:  false,
		},
		{
			name:        "nil_registry返回nil",
			description: "没有 registry 时无法解析任何模型候选，直接返回 nil",
			cfg: llmclientconfig.FallbackConfig{
				DefaultMode: llmclientconfig.FallbackModeAuto,
			},
			registry: nil,
			wantNil:  true,
		},
		{
			name:        "auto模式正常配置返回non-nil",
			description: "正常配置下应成功创建 Resolver 实例",
			cfg: llmclientconfig.FallbackConfig{
				DefaultMode: llmclientconfig.FallbackModeAuto,
			},
			registry: registry,
			wantNil:  false,
		},
		{
			name:        "manual模式正常配置返回non-nil",
			description: "manual 模式下有效 registry 应成功创建 Resolver",
			cfg: llmclientconfig.FallbackConfig{
				DefaultMode: llmclientconfig.FallbackModeManual,
				Manual:      map[string][]string{"gpt-4o": {"claude-3"}},
			},
			registry: registry,
			wantNil:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := fallback.NewResolver(tt.cfg, tt.registry)
			if tt.wantNil {
				assert.Nil(t, resolver)
			} else {
				assert.NotNil(t, resolver)
			}
		})
	}
}

// --- ResolveFallbacks tests ---

func TestResolveFallbacks_ManualMode(t *testing.T) {
	// 测试目的：验证 manual 模式下按配置顺序返回 fallback chain
	registry := &fakeRegistry{models: map[string]*providers.ModelInfo{
		"openai/gpt-4o": makeModelInfo("openai", "gpt-4o", "gpt", []core.ModelCategory{core.CategoryTextGeneration}, nil, nil),
		"openai/gpt-3.5-turbo": makeModelInfo("openai", "gpt-3.5-turbo", "gpt",
			[]core.ModelCategory{core.CategoryTextGeneration}, nil, nil),
		"anthropic/claude-3": makeModelInfo("anthropic", "claude-3", "claude",
			[]core.ModelCategory{core.CategoryTextGeneration}, nil, nil),
	}}

	tests := []struct {
		name        string
		description string
		manual      map[string][]string
		resolution  *core.RequestModelResolution
		wantModels  []string
	}{
		{
			name:        "返回manual中配置的fallback链",
			description: "当 manual 中有对应模型的 fallback 列表时，按配置顺序返回所有候选",
			manual:      map[string][]string{"gpt-4o": {"openai/gpt-3.5-turbo", "anthropic/claude-3"}},
			resolution:  makeResolution("gpt-4o", "openai"),
			wantModels:  []string{"gpt-3.5-turbo", "claude-3"},
		},
		{
			name:        "manual中模型不在registry则跳过",
			description: "manual 列表中的模型如果不在 registry 里则不出现在结果中",
			manual:      map[string][]string{"gpt-4o": {"openai/gpt-3.5-turbo", "nonexistent/model-x"}},
			resolution:  makeResolution("gpt-4o", "openai"),
			wantModels:  []string{"gpt-3.5-turbo"},
		},
		{
			name:        "无匹配的manual规则返回空",
			description: "当请求模型没有对应的 manual fallback 配置时，返回 nil",
			manual:      map[string][]string{"other-model": {"openai/gpt-3.5-turbo"}},
			resolution:  makeResolution("gpt-4o", "openai"),
			wantModels:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := llmclientconfig.FallbackConfig{
				DefaultMode: llmclientconfig.FallbackModeManual,
				Manual:      tt.manual,
			}
			resolver := fallback.NewResolver(cfg, registry)
			require.NotNil(t, resolver)

			result := resolver.ResolveFallbacks(tt.resolution, core.OperationChatCompletions)
			if tt.wantModels == nil {
				assert.Empty(t, result)
				return
			}
			require.Len(t, result, len(tt.wantModels))
			for i, sel := range result {
				assert.Equal(t, tt.wantModels[i], sel.Model)
			}
		})
	}
}

func TestResolveFallbacks_AutoMode(t *testing.T) {
	// 测试目的：验证 auto 模式下从 registry 中按能力匹配候选
	elo1200 := floatPtr(1200.0)
	elo1180 := floatPtr(1180.0)
	elo1100 := floatPtr(1100.0)

	registry := &fakeRegistry{models: map[string]*providers.ModelInfo{
		"openai/gpt-4o": makeModelInfo("openai", "gpt-4o", "gpt",
			[]core.ModelCategory{core.CategoryTextGeneration},
			map[string]bool{"tool_use": true, "vision": true},
			map[string]core.ModelRanking{"chatbot_arena": {Elo: elo1200, Rank: intPtr(1)}}),
		"anthropic/claude-3-5": makeModelInfo("anthropic", "claude-3-5", "claude",
			[]core.ModelCategory{core.CategoryTextGeneration},
			map[string]bool{"tool_use": true, "vision": true},
			map[string]core.ModelRanking{"chatbot_arena": {Elo: elo1180, Rank: intPtr(2)}}),
		"deepseek/deepseek-v3": makeModelInfo("deepseek", "deepseek-v3", "deepseek",
			[]core.ModelCategory{core.CategoryTextGeneration},
			map[string]bool{"tool_use": true},
			map[string]core.ModelRanking{"chatbot_arena": {Elo: elo1100, Rank: intPtr(5)}}),
		"openai/text-embedding-3": makeModelInfo("openai", "text-embedding-3", "",
			[]core.ModelCategory{core.CategoryEmbedding}, nil, nil),
	}}

	t.Run("按能力匹配候选排序", func(t *testing.T) {
		// auto 模式下应返回具有相同 ranking 系列的候选，按 ELO 差距排序
		cfg := llmclientconfig.FallbackConfig{
			DefaultMode: llmclientconfig.FallbackModeAuto,
		}
		resolver := fallback.NewResolver(cfg, registry)
		require.NotNil(t, resolver)

		resolution := makeResolution("gpt-4o", "openai")
		result := resolver.ResolveFallbacks(resolution, core.OperationChatCompletions)

		// 应排除自身(openai/gpt-4o)和 embedding 模型
		for _, sel := range result {
			assert.NotEqual(t, "gpt-4o", sel.Model, "不应包含源模型自身")
			assert.NotEqual(t, "text-embedding-3", sel.Model, "不应包含 embedding 模型")
		}
		// 应包含 claude-3-5（ELO 最近）
		require.NotEmpty(t, result)
		assert.Equal(t, "claude-3-5", result[0].Model, "ELO 最近的候选应排首位")
	})

	t.Run("embedding操作不做fallback", func(t *testing.T) {
		// embedding 请求应直接返回 nil，不执行 fallback
		cfg := llmclientconfig.FallbackConfig{
			DefaultMode: llmclientconfig.FallbackModeAuto,
		}
		resolver := fallback.NewResolver(cfg, registry)
		require.NotNil(t, resolver)

		resolution := makeResolution("text-embedding-3", "openai")
		result := resolver.ResolveFallbacks(resolution, core.OperationEmbeddings)
		assert.Nil(t, result)
	})
}

func TestResolveFallbacks_MixedMode(t *testing.T) {
	// 测试目的：验证 auto 模式下 manual 优先 + auto 追加的行为
	elo1200 := floatPtr(1200.0)
	elo1180 := floatPtr(1180.0)
	elo1150 := floatPtr(1150.0)

	registry := &fakeRegistry{models: map[string]*providers.ModelInfo{
		"openai/gpt-4o": makeModelInfo("openai", "gpt-4o", "gpt",
			[]core.ModelCategory{core.CategoryTextGeneration},
			map[string]bool{"tool_use": true},
			map[string]core.ModelRanking{"chatbot_arena": {Elo: elo1200, Rank: intPtr(1)}}),
		"anthropic/claude-3-5": makeModelInfo("anthropic", "claude-3-5", "claude",
			[]core.ModelCategory{core.CategoryTextGeneration},
			map[string]bool{"tool_use": true},
			map[string]core.ModelRanking{"chatbot_arena": {Elo: elo1180, Rank: intPtr(2)}}),
		"deepseek/deepseek-v3": makeModelInfo("deepseek", "deepseek-v3", "deepseek",
			[]core.ModelCategory{core.CategoryTextGeneration},
			map[string]bool{"tool_use": true},
			map[string]core.ModelRanking{"chatbot_arena": {Elo: elo1150, Rank: intPtr(3)}}),
	}}

	t.Run("manual优先加auto追加", func(t *testing.T) {
		// auto 模式下，如果同时有 manual 配置，manual 的候选排在前面，auto 追加在后面
		cfg := llmclientconfig.FallbackConfig{
			DefaultMode: llmclientconfig.FallbackModeAuto,
			Manual:      map[string][]string{"gpt-4o": {"deepseek/deepseek-v3"}},
		}
		resolver := fallback.NewResolver(cfg, registry)
		require.NotNil(t, resolver)

		resolution := makeResolution("gpt-4o", "openai")
		result := resolver.ResolveFallbacks(resolution, core.OperationChatCompletions)

		require.GreaterOrEqual(t, len(result), 2, "至少应包含 manual + auto 候选")
		// manual 中配置的 deepseek-v3 应在最前面
		assert.Equal(t, "deepseek-v3", result[0].Model, "manual 候选应排最前")
		// auto 追加的 claude-3-5 应在后面
		assert.Equal(t, "claude-3-5", result[1].Model, "auto 追加候选排在 manual 之后")
	})

	t.Run("去重验证", func(t *testing.T) {
		// manual 中已包含的模型不应在 auto 追加时重复出现
		cfg := llmclientconfig.FallbackConfig{
			DefaultMode: llmclientconfig.FallbackModeAuto,
			Manual:      map[string][]string{"gpt-4o": {"anthropic/claude-3-5"}},
		}
		resolver := fallback.NewResolver(cfg, registry)
		require.NotNil(t, resolver)

		resolution := makeResolution("gpt-4o", "openai")
		result := resolver.ResolveFallbacks(resolution, core.OperationChatCompletions)

		// 验证结果中没有重复
		seen := make(map[string]struct{})
		for _, sel := range result {
			key := sel.Provider + "/" + sel.Model
			_, exists := seen[key]
			assert.False(t, exists, "fallback chain 不应有重复模型: %s", key)
			seen[key] = struct{}{}
		}
	})
}

func TestResolveFallbacks_ModelOverride(t *testing.T) {
	// 测试目的：验证 per-model override 可以覆盖全局 fallback 模式
	elo1200 := floatPtr(1200.0)
	elo1180 := floatPtr(1180.0)

	registry := &fakeRegistry{models: map[string]*providers.ModelInfo{
		"openai/gpt-4o": makeModelInfo("openai", "gpt-4o", "gpt",
			[]core.ModelCategory{core.CategoryTextGeneration},
			map[string]bool{"tool_use": true},
			map[string]core.ModelRanking{"chatbot_arena": {Elo: elo1200, Rank: intPtr(1)}}),
		"anthropic/claude-3-5": makeModelInfo("anthropic", "claude-3-5", "claude",
			[]core.ModelCategory{core.CategoryTextGeneration},
			map[string]bool{"tool_use": true},
			map[string]core.ModelRanking{"chatbot_arena": {Elo: elo1180, Rank: intPtr(2)}}),
	}}

	t.Run("override将off覆盖为auto", func(t *testing.T) {
		// 全局为 off，但 per-model override 设为 auto，该模型仍应产生 fallback 候选
		cfg := llmclientconfig.FallbackConfig{
			DefaultMode: llmclientconfig.FallbackModeOff,
			Overrides: map[string]llmclientconfig.FallbackModelOverride{
				"gpt-4o": {Mode: llmclientconfig.FallbackModeAuto},
			},
		}
		resolver := fallback.NewResolver(cfg, registry)
		require.NotNil(t, resolver)

		resolution := makeResolution("gpt-4o", "openai")
		result := resolver.ResolveFallbacks(resolution, core.OperationChatCompletions)

		require.NotEmpty(t, result, "override 为 auto 时应产生候选")
		assert.Equal(t, "claude-3-5", result[0].Model)
	})

	t.Run("override将auto覆盖为off", func(t *testing.T) {
		// 全局为 auto，但 per-model override 设为 off，该模型不应产生 fallback
		cfg := llmclientconfig.FallbackConfig{
			DefaultMode: llmclientconfig.FallbackModeAuto,
			Overrides: map[string]llmclientconfig.FallbackModelOverride{
				"gpt-4o": {Mode: llmclientconfig.FallbackModeOff},
			},
		}
		resolver := fallback.NewResolver(cfg, registry)
		require.NotNil(t, resolver)

		resolution := makeResolution("gpt-4o", "openai")
		result := resolver.ResolveFallbacks(resolution, core.OperationChatCompletions)

		assert.Empty(t, result, "override 为 off 时不应产生候选")
	})
}

func TestResolveFallbacks_SelfExclusion(t *testing.T) {
	// 测试目的：验证 fallback chain 中不包含原始请求模型
	elo1200 := floatPtr(1200.0)
	elo1180 := floatPtr(1180.0)

	registry := &fakeRegistry{models: map[string]*providers.ModelInfo{
		"openai/gpt-4o": makeModelInfo("openai", "gpt-4o", "gpt",
			[]core.ModelCategory{core.CategoryTextGeneration},
			nil,
			map[string]core.ModelRanking{"chatbot_arena": {Elo: elo1200, Rank: intPtr(1)}}),
		"anthropic/claude-3": makeModelInfo("anthropic", "claude-3", "claude",
			[]core.ModelCategory{core.CategoryTextGeneration},
			nil,
			map[string]core.ModelRanking{"chatbot_arena": {Elo: elo1180, Rank: intPtr(2)}}),
	}}

	cfg := llmclientconfig.FallbackConfig{
		DefaultMode: llmclientconfig.FallbackModeAuto,
	}
	resolver := fallback.NewResolver(cfg, registry)
	require.NotNil(t, resolver)

	resolution := makeResolution("gpt-4o", "openai")
	result := resolver.ResolveFallbacks(resolution, core.OperationChatCompletions)

	for _, sel := range result {
		qualifiedKey := sel.Provider + "/" + sel.Model
		assert.NotEqual(t, "openai/gpt-4o", qualifiedKey,
			"fallback chain 不应包含源模型自身")
	}
}

func TestResolveFallbacks_EmptyChainWhenNoMatch(t *testing.T) {
	// 测试目的：当模型不在 registry 中且没有 auto 候选时返回空链
	registry := &fakeRegistry{models: map[string]*providers.ModelInfo{
		"openai/text-embedding-3": makeModelInfo("openai", "text-embedding-3", "",
			[]core.ModelCategory{core.CategoryEmbedding}, nil, nil),
	}}

	cfg := llmclientconfig.FallbackConfig{
		DefaultMode: llmclientconfig.FallbackModeAuto,
	}
	resolver := fallback.NewResolver(cfg, registry)
	require.NotNil(t, resolver)

	// 请求一个不存在于 registry 中的模型
	resolution := makeResolution("nonexistent-model", "")
	result := resolver.ResolveFallbacks(resolution, core.OperationChatCompletions)

	assert.Empty(t, result, "不存在的模型且无 auto 候选时应返回空链")
}

func TestResolveFallbacks_NilResolution(t *testing.T) {
	// 测试目的：nil resolution 应安全返回 nil
	registry := &fakeRegistry{models: map[string]*providers.ModelInfo{
		"openai/gpt-4o": makeModelInfo("openai", "gpt-4o", "gpt", nil, nil, nil),
	}}

	cfg := llmclientconfig.FallbackConfig{
		DefaultMode: llmclientconfig.FallbackModeAuto,
	}
	resolver := fallback.NewResolver(cfg, registry)
	require.NotNil(t, resolver)

	result := resolver.ResolveFallbacks(nil, core.OperationChatCompletions)
	assert.Nil(t, result)
}

func TestResolveFallbacks_NilResolver(t *testing.T) {
	// 测试目的：nil resolver 调用 ResolveFallbacks 不 panic，安全返回 nil
	var resolver *fallback.Resolver
	resolution := makeResolution("gpt-4o", "openai")
	result := resolver.ResolveFallbacks(resolution, core.OperationChatCompletions)
	assert.Nil(t, result)
}

func TestResolveFallbacks_ManualSelfExclusion(t *testing.T) {
	// 测试目的：manual 列表中若包含源模型自身，应被自动排除
	registry := &fakeRegistry{models: map[string]*providers.ModelInfo{
		"openai/gpt-4o": makeModelInfo("openai", "gpt-4o", "gpt",
			[]core.ModelCategory{core.CategoryTextGeneration}, nil, nil),
		"anthropic/claude-3": makeModelInfo("anthropic", "claude-3", "claude",
			[]core.ModelCategory{core.CategoryTextGeneration}, nil, nil),
	}}

	cfg := llmclientconfig.FallbackConfig{
		DefaultMode: llmclientconfig.FallbackModeManual,
		Manual:      map[string][]string{"gpt-4o": {"openai/gpt-4o", "anthropic/claude-3"}},
	}
	resolver := fallback.NewResolver(cfg, registry)
	require.NotNil(t, resolver)

	resolution := makeResolution("gpt-4o", "openai")
	result := resolver.ResolveFallbacks(resolution, core.OperationChatCompletions)

	// 结果中不应包含源模型
	for _, sel := range result {
		assert.NotEqual(t, "gpt-4o", sel.Model,
			"manual fallback chain 不应包含源模型自身")
	}
	// 但应包含 claude-3
	require.Len(t, result, 1)
	assert.Equal(t, "claude-3", result[0].Model)
}
