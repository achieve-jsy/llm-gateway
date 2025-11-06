package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/seed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixtureBasic 是 LiteLLM JSON 的最小可解析样本，覆盖：
//   - 普通条目 gpt-4o-2024-11-20
//   - sample_spec 模板（应被跳过）
//   - 阶梯定价（claude 200k）
//   - 无 max_input_tokens、需要回退到 max_tokens 的条目
//   - source_model_id 含 '/' 的国产模型（azure_ai/deepseek-v3.2）
const fixtureBasic = `{
  "sample_spec": {
    "input_cost_per_token": 0.0,
    "litellm_provider": "should-be-skipped",
    "max_input_tokens": "max input tokens, if the provider specifies it. if not default to max_tokens",
    "mode": "chat"
  },
  "gpt-4o-2024-11-20": {
    "cache_read_input_token_cost": 1.25e-06,
    "input_cost_per_token": 2.5e-06,
    "litellm_provider": "openai",
    "max_input_tokens": 128000,
    "max_output_tokens": 16384,
    "max_tokens": 16384,
    "mode": "chat",
    "output_cost_per_token": 1e-05,
    "supports_function_calling": true,
    "supports_parallel_function_calling": true,
    "supports_prompt_caching": true,
    "supports_response_schema": true,
    "supports_tool_choice": true,
    "supports_vision": true,
    "deprecation_date": "2026-12-01"
  },
  "claude-3-5-sonnet-200k": {
    "input_cost_per_token": 3e-06,
    "output_cost_per_token": 1.5e-05,
    "input_cost_per_token_above_200k_tokens": 6e-06,
    "output_cost_per_token_above_200k_tokens": 3e-05,
    "max_input_tokens": 200000,
    "max_output_tokens": 8192,
    "mode": "chat",
    "litellm_provider": "anthropic",
    "supports_function_calling": true,
    "supports_vision": true
  },
  "legacy-only-max-tokens": {
    "input_cost_per_token": 1e-06,
    "output_cost_per_token": 2e-06,
    "max_tokens": 4096,
    "mode": "chat",
    "litellm_provider": "openai"
  },
  "azure_ai/deepseek-v3.2": {
    "input_cost_per_token": 5.8e-07,
    "output_cost_per_token": 1.68e-06,
    "max_input_tokens": 163840,
    "max_output_tokens": 163840,
    "mode": "chat",
    "litellm_provider": "azure_ai",
    "supports_function_calling": true
  }
}`

func parseFixture(t *testing.T, raw string) []*ModelCatalogParsed {
	t.Helper()
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(raw), &m))
	got, err := ParseLiteLLMJSON(m)
	require.NoError(t, err)
	return got
}

func findEntry(items []*ModelCatalogParsed, sourceID string) *ModelCatalogParsed {
	for _, it := range items {
		if it.SourceModelID == sourceID {
			return it
		}
	}
	return nil
}

func TestParseLiteLLMJSON_BasicEntry(t *testing.T) {
	items := parseFixture(t, fixtureBasic)
	gpt := findEntry(items, "gpt-4o-2024-11-20")
	require.NotNil(t, gpt, "gpt-4o-2024-11-20 should be parsed")

	assert.Equal(t, "openai", gpt.Provider)
	assert.Equal(t, "chat", gpt.Mode)
	assert.Equal(t, int64(128000), gpt.ContextLength)
	assert.Equal(t, int64(16384), gpt.MaxOutputTokens)
	// 单位换算：2.5e-6 * 1e6 = 2.5 USD per 1M tokens
	assert.InDelta(t, 2.5, gpt.InputPricePerMillion, 1e-6)
	assert.InDelta(t, 10.0, gpt.OutputPricePerMillion, 1e-6)
	require.NotNil(t, gpt.CacheReadPrice)
	assert.InDelta(t, 1.25, *gpt.CacheReadPrice, 1e-6)
	assert.True(t, gpt.SupportsFunctionCalling)
	assert.True(t, gpt.SupportsVision)
	assert.True(t, gpt.SupportsPromptCaching)
	assert.True(t, gpt.SupportsParallelFunctionCalling)
	require.NotNil(t, gpt.DeprecationDate)
	assert.Equal(t, 2026, gpt.DeprecationDate.Year())
	assert.NotEmpty(t, gpt.RawPayload, "raw payload must be persisted")
}

func TestParseLiteLLMJSON_SkipsSampleSpec(t *testing.T) {
	items := parseFixture(t, fixtureBasic)
	for _, it := range items {
		assert.NotEqual(t, "sample_spec", it.SourceModelID, "sample_spec must be filtered out")
	}
}

func TestParseLiteLLMJSON_TieredPricing(t *testing.T) {
	items := parseFixture(t, fixtureBasic)
	claude := findEntry(items, "claude-3-5-sonnet-200k")
	require.NotNil(t, claude)
	require.NotNil(t, claude.TierThreshold, "tier threshold must be parsed from *_above_200k_tokens key")
	assert.Equal(t, int64(200000), *claude.TierThreshold)
	require.NotNil(t, claude.InputPriceAboveTier)
	assert.InDelta(t, 6.0, *claude.InputPriceAboveTier, 1e-6)
	require.NotNil(t, claude.OutputPriceAboveTier)
	assert.InDelta(t, 30.0, *claude.OutputPriceAboveTier, 1e-6)
}

func TestParseLiteLLMJSON_Capabilities(t *testing.T) {
	items := parseFixture(t, fixtureBasic)
	gpt := findEntry(items, "gpt-4o-2024-11-20")
	require.NotNil(t, gpt)
	legacy := findEntry(items, "legacy-only-max-tokens")
	require.NotNil(t, legacy)
	assert.False(t, legacy.SupportsFunctionCalling, "absent capability flags must default to false")
	assert.False(t, legacy.SupportsVision)
}

func TestParseLiteLLMJSON_FallbackMaxTokens(t *testing.T) {
	items := parseFixture(t, fixtureBasic)
	legacy := findEntry(items, "legacy-only-max-tokens")
	require.NotNil(t, legacy)
	// 没有 max_input_tokens，应当回退到 max_tokens
	assert.Equal(t, int64(4096), legacy.ContextLength)
	assert.Equal(t, int64(4096), legacy.MaxOutputTokens)
}

func TestParseLiteLLMJSON_KeepsSlashedID(t *testing.T) {
	items := parseFixture(t, fixtureBasic)
	deepseek := findEntry(items, "azure_ai/deepseek-v3.2")
	require.NotNil(t, deepseek, "source_model_id with '/' must be preserved verbatim")
	assert.Equal(t, "azure_ai", deepseek.Provider)
}

// TestParseLiteLLMJSON_FullBundledFile 用真实 1.45MB 文件做端到端校验：
// 不能因为某些异常条目就 panic，且有效条目占比必须 > 80%。
func TestParseLiteLLMJSON_FullBundledFile(t *testing.T) {
	require.NotEmpty(t, seed.LiteLLMCatalogJSON, "embed bundle must be non-empty")
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(seed.LiteLLMCatalogJSON, &m))

	got, err := ParseLiteLLMJSON(m)
	require.NoError(t, err)
	assert.Greater(t, len(got), 2000, "should parse at least 2000 entries from bundle")

	// 有效条目（有 source_model_id 且非 sample_spec 且 mode 非空）占比 > 80%
	valid := 0
	for _, it := range got {
		if it.SourceModelID != "" && it.SourceModelID != "sample_spec" && it.Mode != "" {
			valid++
		}
	}
	ratio := float64(valid) / float64(len(got))
	assert.Greater(t, ratio, 0.80, "valid entry ratio should be > 80%%, got %.2f", ratio)

	// 没有 sample_spec 漏网
	for _, it := range got {
		assert.False(t, strings.EqualFold(it.SourceModelID, "sample_spec"))
	}
}
