package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

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

// ===================== tryParseDate =====================

func TestTryParseDate_YYYYMMDD(t *testing.T) {
	got, ok := tryParseDate("2026-12-01")
	assert.True(t, ok)
	assert.Equal(t, 2026, got.Year())
	assert.Equal(t, time.December, got.Month())
	assert.Equal(t, 1, got.Day())
}

func TestTryParseDate_RFC3339(t *testing.T) {
	got, ok := tryParseDate("2026-12-01T00:00:00Z")
	assert.True(t, ok)
	assert.Equal(t, 2026, got.Year())
}

func TestTryParseDate_SlashFormat(t *testing.T) {
	got, ok := tryParseDate("2026/12/01")
	assert.True(t, ok)
	assert.Equal(t, 2026, got.Year())
}

func TestTryParseDate_InvalidFormat(t *testing.T) {
	_, ok := tryParseDate("not-a-date")
	assert.False(t, ok)
}

func TestTryParseDate_EmptyString(t *testing.T) {
	_, ok := tryParseDate("")
	assert.False(t, ok)
}

func TestTryParseDate_Whitespace(t *testing.T) {
	got, ok := tryParseDate("  2026-12-01  ")
	assert.True(t, ok)
	assert.Equal(t, 2026, got.Year())
}

// ===================== DeprecationDate formats =====================

func TestParseLiteLLMJSON_DeprecationDate_RFC3339(t *testing.T) {
	fixture := `{
  "model-with-rfc3339-dep": {
    "input_cost_per_token": 1e-06,
    "output_cost_per_token": 2e-06,
    "max_input_tokens": 32000,
    "mode": "chat",
    "litellm_provider": "openai",
    "deprecation_date": "2026-12-01T23:59:59Z"
  }
}`
	items := parseFixture(t, fixture)
	entry := findEntry(items, "model-with-rfc3339-dep")
	require.NotNil(t, entry)
	require.NotNil(t, entry.DeprecationDate)
	assert.Equal(t, 2026, entry.DeprecationDate.Year())
}

func TestParseLiteLLMJSON_DeprecationDate_Invalid(t *testing.T) {
	fixture := `{
  "model-bad-dep": {
    "input_cost_per_token": 1e-06,
    "max_input_tokens": 32000,
    "mode": "chat",
    "litellm_provider": "openai",
    "deprecation_date": "sometime-in-the-future"
  }
}`
	items := parseFixture(t, fixture)
	entry := findEntry(items, "model-bad-dep")
	require.NotNil(t, entry)
	assert.Nil(t, entry.DeprecationDate, "invalid deprecation_date should be nil, not causing parse error")
}

// ===================== Cache pricing =====================

func TestParseLiteLLMJSON_CachePrices(t *testing.T) {
	fixture := `{
  "model-cache": {
    "input_cost_per_token": 3e-06,
    "output_cost_per_token": 1.5e-05,
    "cache_read_input_token_cost": 1.5e-06,
    "cache_creation_input_token_cost": 3.75e-06,
    "max_input_tokens": 200000,
    "mode": "chat",
    "litellm_provider": "anthropic"
  }
}`
	items := parseFixture(t, fixture)
	entry := findEntry(items, "model-cache")
	require.NotNil(t, entry)
	require.NotNil(t, entry.CacheReadPrice)
	assert.InDelta(t, 1.5, *entry.CacheReadPrice, 1e-6)
	require.NotNil(t, entry.CacheWritePrice)
	assert.InDelta(t, 3.75, *entry.CacheWritePrice, 1e-6)
}

// ===================== numberField / floatField =====================

func TestParseLiteLLMJSON_StringMaxTokensSkipped(t *testing.T) {
	fixture := `{
  "model-string-tokens": {
    "input_cost_per_token": 1e-06,
    "max_input_tokens": "not-a-number",
    "mode": "chat",
    "litellm_provider": "openai"
  }
}`
	items := parseFixture(t, fixture)
	entry := findEntry(items, "model-string-tokens")
	require.NotNil(t, entry)
	assert.Equal(t, int64(0), entry.ContextLength, "string max_input_tokens should be skipped, falling back to 0")
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

	valid := 0
	for _, it := range got {
		if it.SourceModelID != "" && it.SourceModelID != "sample_spec" && it.Mode != "" {
			valid++
		}
	}
	ratio := float64(valid) / float64(len(got))
	assert.Greater(t, ratio, 0.80, "valid entry ratio should be > 80%%, got %.2f", ratio)

	for _, it := range got {
		assert.False(t, strings.EqualFold(it.SourceModelID, "sample_spec"))
	}
}

func TestParseLiteLLMJSON_EmptyInput(t *testing.T) {
	got, err := ParseLiteLLMJSON(map[string]json.RawMessage{})
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestParseLiteLLMJSON_DisplayNameIsSourceID(t *testing.T) {
	fixture := `{
  "my-model": {
    "input_cost_per_token": 1e-06,
    "max_input_tokens": 32000,
    "mode": "chat",
    "litellm_provider": "openai"
  }
}`
	items := parseFixture(t, fixture)
	entry := findEntry(items, "my-model")
	require.NotNil(t, entry)
	assert.Equal(t, "my-model", entry.DisplayName, "display_name defaults to source_model_id")
}

func TestParseLiteLLMJSON_NoPricingFields(t *testing.T) {
	fixture := `{
  "free-model": {
    "max_input_tokens": 32000,
    "mode": "chat",
    "litellm_provider": "openai"
  }
}`
	items := parseFixture(t, fixture)
	entry := findEntry(items, "free-model")
	require.NotNil(t, entry)
	assert.Equal(t, 0.0, entry.InputPricePerMillion)
	assert.Equal(t, 0.0, entry.OutputPricePerMillion)
	assert.Nil(t, entry.CacheReadPrice)
	assert.Nil(t, entry.CacheWritePrice)
}

func TestParseLiteLLMJSON_MultipleTierThresholds(t *testing.T) {
	fixture := `{
  "model-multi-tier": {
    "input_cost_per_token": 1e-06,
    "output_cost_per_token": 2e-06,
    "input_cost_per_token_above_100k_tokens": 2e-06,
    "output_cost_per_token_above_100k_tokens": 4e-06,
    "input_cost_per_token_above_200k_tokens": 3e-06,
    "output_cost_per_token_above_200k_tokens": 6e-06,
    "max_input_tokens": 200000,
    "mode": "chat",
    "litellm_provider": "anthropic"
  }
}`
	items := parseFixture(t, fixture)
	entry := findEntry(items, "model-multi-tier")
	require.NotNil(t, entry)
	require.NotNil(t, entry.TierThreshold)
	assert.Equal(t, int64(100000), *entry.TierThreshold, "should pick the smallest threshold")
	require.NotNil(t, entry.InputPriceAboveTier)
	assert.InDelta(t, 3.0, *entry.InputPriceAboveTier, 1e-6, "last processed tier price for input")
	require.NotNil(t, entry.OutputPriceAboveTier)
	assert.InDelta(t, 6.0, *entry.OutputPriceAboveTier, 1e-6, "last processed tier price for output")
}

func TestParseLiteLLMJSON_SkipsInvalidJSONEntry(t *testing.T) {
	fixture := `{
  "bad-json": "this is not a JSON object",
  "good-entry": {
    "input_cost_per_token": 1e-06,
    "max_input_tokens": 32000,
    "mode": "chat",
    "litellm_provider": "openai"
  }
}`
	items := parseFixture(t, fixture)
	assert.Len(t, items, 1)
	entry := findEntry(items, "good-entry")
	require.NotNil(t, entry)
	assert.Nil(t, findEntry(items, "bad-json"))
}

func TestParseLiteLLMJSON_MaxOutputFallsBackToMaxTokens(t *testing.T) {
	fixture := `{
  "no-max-output": {
    "input_cost_per_token": 1e-06,
    "max_tokens": 8192,
    "mode": "chat",
    "litellm_provider": "openai"
  }
}`
	items := parseFixture(t, fixture)
	entry := findEntry(items, "no-max-output")
	require.NotNil(t, entry)
	assert.Equal(t, int64(8192), entry.MaxOutputTokens, "max_output_tokens should fall back to max_tokens")
	assert.Equal(t, int64(8192), entry.ContextLength)
}

func TestParseLiteLLMJSON_CacheWritePrice(t *testing.T) {
	fixture := `{
  "model-cache-write": {
    "input_cost_per_token": 3e-06,
    "output_cost_per_token": 1.5e-05,
    "cache_creation_input_token_cost": 3.75e-06,
    "max_input_tokens": 200000,
    "mode": "chat",
    "litellm_provider": "anthropic"
  }
}`
	items := parseFixture(t, fixture)
	entry := findEntry(items, "model-cache-write")
	require.NotNil(t, entry)
	assert.Nil(t, entry.CacheReadPrice, "no cache_read field means nil")
	require.NotNil(t, entry.CacheWritePrice)
	assert.InDelta(t, 3.75, *entry.CacheWritePrice, 1e-6)
}
