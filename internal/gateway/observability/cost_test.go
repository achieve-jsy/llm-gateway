package observability

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/example/llm-gateway/internal/core"
)

func f64(v float64) *float64 { return &v }

func TestExtractCostInput_OpenAI(t *testing.T) {
	u := core.Usage{
		PromptTokens:     6074,
		CompletionTokens: 285,
		PromptTokensDetails: &core.PromptTokensDetails{
			CachedTokens: 3456,
		},
	}
	ci := ExtractCostInput(u)
	require.Equal(t, 6074, ci.PromptTokens)
	require.Equal(t, 3456, ci.CachedTokens)
	require.Equal(t, 0, ci.CacheWriteTokens)
	require.Equal(t, 285, ci.CompletionTokens)
}

func TestExtractCostInput_Anthropic(t *testing.T) {
	// Anthropic input_tokens = 未命中部分（5000），不含 cached(1000) 和 write(500)。
	// ExtractCostInput 应将 PromptTokens 补偿为 5000+1000+500=6500，
	// 使其满足「prompt_tokens 包含全部输入」的不变式，与 LiteLLM 保持一致。
	u := core.Usage{
		PromptTokens:     5000,
		CompletionTokens: 200,
		RawUsage: map[string]any{
			"cache_read_input_tokens":     float64(1000),
			"cache_creation_input_tokens": float64(500),
		},
	}
	ci := ExtractCostInput(u)
	require.Equal(t, 6500, ci.PromptTokens) // 5000 + 1000 + 500
	require.Equal(t, 1000, ci.CachedTokens)
	require.Equal(t, 500, ci.CacheWriteTokens)
	require.Equal(t, 200, ci.CompletionTokens)
}

func TestExtractCostInput_DeepSeek(t *testing.T) {
	// DeepSeek 通过 prompt_cache_hit_tokens / prompt_cache_miss_tokens 上报缓存。
	// prompt_tokens = hit + miss（已包含两者），不需要补偿。
	u := core.Usage{
		PromptTokens:     8000,
		CompletionTokens: 300,
		RawUsage: map[string]any{
			"prompt_cache_hit_tokens":  float64(3000),
			"prompt_cache_miss_tokens": float64(5000),
		},
	}
	ci := ExtractCostInput(u)
	require.Equal(t, 8000, ci.PromptTokens) // 无补偿
	require.Equal(t, 3000, ci.CachedTokens)
	require.Equal(t, 0, ci.CacheWriteTokens)
	require.Equal(t, 300, ci.CompletionTokens)
}

func TestExtractCostInput_DeepSeekFallsBackToAnthropicWhenDetailsPresent(t *testing.T) {
	// 同时存在 OpenAI details.cached_tokens 与 DeepSeek top-level 时，OpenAI details 优先。
	u := core.Usage{
		PromptTokens:     8000,
		CompletionTokens: 300,
		PromptTokensDetails: &core.PromptTokensDetails{
			CachedTokens: 2500,
		},
		RawUsage: map[string]any{
			"prompt_cache_hit_tokens": float64(3000),
		},
	}
	ci := ExtractCostInput(u)
	require.Equal(t, 8000, ci.PromptTokens)
	require.Equal(t, 2500, ci.CachedTokens) // OpenAI details 优先
}

func TestExtractCostInput_OpenAIPriority(t *testing.T) {
	// OpenAI PromptTokensDetails.CachedTokens 优先于 raw
	u := core.Usage{
		PromptTokens:     6074,
		CompletionTokens: 285,
		PromptTokensDetails: &core.PromptTokensDetails{
			CachedTokens: 3456,
		},
		RawUsage: map[string]any{
			"cache_read_input_tokens": float64(999),
		},
	}
	ci := ExtractCostInput(u)
	require.Equal(t, 3456, ci.CachedTokens)
}

func TestExtractCostInput_NoUsage(t *testing.T) {
	u := core.Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
	}
	ci := ExtractCostInput(u)
	require.Equal(t, 100, ci.PromptTokens)
	require.Equal(t, 0, ci.CachedTokens)
	require.Equal(t, 0, ci.CacheWriteTokens)
	require.Equal(t, 50, ci.CompletionTokens)
}

func TestEstimateCost_Golden(t *testing.T) {
	// LiteLLM golden 向量
	// input=2.5, cache_read=0.25, output=15 (USD/1M)
	// prompt=6074, cached=3456, completion=285
	// uncached = 6074 - 3456 - 0 = 2618
	// cost = 2618/1e6*2.5 + 3456/1e6*0.25 + 285/1e6*15
	//      = 0.006545 + 0.000864 + 0.004275 = 0.011684
	in := core.CostInput{
		PromptTokens:     6074,
		CachedTokens:     3456,
		CompletionTokens: 285,
	}
	p := &core.ModelPricing{
		InputPerMtok:       f64(2.5),
		OutputPerMtok:      f64(15),
		CachedInputPerMtok: f64(0.25),
	}
	got, missing := EstimateCost(in, p)
	require.False(t, missing)
	require.InDelta(t, 0.011684, got, 1e-9)
}

func TestEstimateCost_MissingPrice(t *testing.T) {
	in := core.CostInput{
		PromptTokens:     100,
		CompletionTokens: 50,
	}
	_, missing := EstimateCost(in, nil)
	require.True(t, missing)

	p := &core.ModelPricing{}
	_, missing = EstimateCost(in, p)
	require.True(t, missing)
}

func TestEstimateCost_CachedExceedsPrompt(t *testing.T) {
	// cached > prompt 时 uncached 截断为 0
	in := core.CostInput{
		PromptTokens:     100,
		CachedTokens:     200,
		CompletionTokens: 50,
	}
	p := &core.ModelPricing{
		InputPerMtok:       f64(2.0),
		OutputPerMtok:      f64(10.0),
		CachedInputPerMtok: f64(0.2),
	}
	got, missing := EstimateCost(in, p)
	require.False(t, missing)
	// uncached = 0, cached = 200, completion = 50
	expected := perM(0, 2.0) + perM(200, 0.2) + perM(50, 10.0)
	require.InDelta(t, expected, got, 1e-9)
}

func TestEstimateCost_NoCaching(t *testing.T) {
	in := core.CostInput{
		PromptTokens:     1000,
		CompletionTokens: 500,
	}
	p := &core.ModelPricing{
		InputPerMtok:  f64(2.0),
		OutputPerMtok: f64(10.0),
	}
	got, missing := EstimateCost(in, p)
	require.False(t, missing)
	expected := perM(1000, 2.0) + perM(500, 10.0)
	require.InDelta(t, expected, got, 1e-9)
}

func TestEstimateCost_PureCache(t *testing.T) {
	// 全部输入都是缓存命中
	in := core.CostInput{
		PromptTokens:     1000,
		CachedTokens:     1000,
		CompletionTokens: 100,
	}
	p := &core.ModelPricing{
		InputPerMtok:       f64(2.0),
		OutputPerMtok:      f64(10.0),
		CachedInputPerMtok: f64(0.2),
	}
	got, missing := EstimateCost(in, p)
	require.False(t, missing)
	expected := perM(0, 2.0) + perM(1000, 0.2) + perM(100, 10.0)
	require.InDelta(t, expected, got, 1e-9)
}

func TestEstimateCost_Tier(t *testing.T) {
	// 阶梯价：超过 200K token 用阶梯价
	in := core.CostInput{
		PromptTokens:     300000,
		CompletionTokens: 1000,
	}
	p := &core.ModelPricing{
		InputPerMtok:  f64(2.0),
		OutputPerMtok: f64(10.0),
		Tiers: []core.ModelPricingTier{
			{
				UpToTokens:    f64(200000),
				InputPerMtok:  f64(1.5),
				OutputPerMtok: f64(8.0),
			},
		},
	}
	got, missing := EstimateCost(in, p)
	require.False(t, missing)
	// 300000 > 200000，不匹配阶梯，使用默认价
	expected := perM(300000, 2.0) + perM(1000, 10.0)
	require.InDelta(t, expected, got, 1e-9)
}

func TestEstimateCost_TierMatched(t *testing.T) {
	// 150K <= 200K，匹配阶梯价
	in := core.CostInput{
		PromptTokens:     150000,
		CompletionTokens: 1000,
	}
	p := &core.ModelPricing{
		InputPerMtok:  f64(2.0),
		OutputPerMtok: f64(10.0),
		Tiers: []core.ModelPricingTier{
			{
				UpToTokens:    f64(200000),
				InputPerMtok:  f64(1.5),
				OutputPerMtok: f64(8.0),
			},
		},
	}
	got, missing := EstimateCost(in, p)
	require.False(t, missing)
	expected := perM(150000, 1.5) + perM(1000, 8.0)
	require.InDelta(t, expected, got, 1e-9)
}

func TestEstimateCost_CacheWrite(t *testing.T) {
	in := core.CostInput{
		PromptTokens:     5000,
		CachedTokens:     1000,
		CacheWriteTokens: 2000,
		CompletionTokens: 500,
	}
	p := &core.ModelPricing{
		InputPerMtok:       f64(2.0),
		OutputPerMtok:      f64(10.0),
		CachedInputPerMtok: f64(0.2),
		CacheWritePerMtok:  f64(1.0),
	}
	got, missing := EstimateCost(in, p)
	require.False(t, missing)
	// uncached = 5000-1000-2000 = 2000
	expected := perM(2000, 2.0) + perM(1000, 0.2) + perM(2000, 1.0) + perM(500, 10.0)
	require.InDelta(t, expected, got, 1e-9)
}