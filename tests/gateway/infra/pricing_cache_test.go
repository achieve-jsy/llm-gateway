package infra_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/gateway/infra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// floatPtr 返回 *float64，用于构建含指针字段的 ModelPricing。
func floatPtr(v float64) *float64 { return &v }

// TestMemoryPricingCache_Success 测试 MemoryPricingCache 正常拉取并缓存定价。
//
// 测试目的：httptest server 返回合法定价 JSON，验证 Start（内部同步调用 refresh）
// 后 Get 能正确返回各字段值，包括 *float64 类型的 ReasoningPrice 和 CachedPrice。
func TestMemoryPricingCache_Success(t *testing.T) {
	infra.UsageMetricsInstance.Reset()

	pricingData, err := json.Marshal(map[string]interface{}{
		"models": map[string]infra.ModelPricing{
			"gpt-4": {
				PromptPrice:     2.0,
				CompletionPrice: 8.0,
				ReasoningPrice:  floatPtr(12.0),
				CachedPrice:     nil,
				PricingMissing:  false,
			},
			"claude-3-5": {
				PromptPrice:     3.0,
				CompletionPrice: 15.0,
				ReasoningPrice:  nil,
				CachedPrice:     floatPtr(1.5),
				PricingMissing:  false,
			},
		},
	})
	require.NoError(t, err, "构建测试定价 JSON 不应失败")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/internal/v1/pricing", r.URL.Path, "请求路径应为 /internal/v1/pricing")
		assert.Equal(t, "test-token", r.Header.Get("X-Internal-Token"), "应携带内部 token")
		w.Header().Set("Content-Type", "application/json")
		w.Write(pricingData) //nolint:errcheck
	}))
	defer server.Close()

	cache := infra.NewMemoryPricingCache(server.URL, "test-token")
	cache.Start(context.Background())
	defer cache.Stop()

	t.Run("gpt-4 完整定价", func(t *testing.T) {
		pricing, ok := cache.Get("gpt-4")
		require.True(t, ok, "应找到 gpt-4 的定价")
		assert.Equal(t, 2.0, pricing.PromptPrice, "prompt_price 应为 2.0")
		assert.Equal(t, 8.0, pricing.CompletionPrice, "completion_price 应为 8.0")
		require.NotNil(t, pricing.ReasoningPrice, "reasoning_price 不应为 nil")
		assert.Equal(t, 12.0, *pricing.ReasoningPrice, "reasoning_price 应为 12.0")
		assert.Nil(t, pricing.CachedPrice, "cached_price 应为 nil")
		assert.False(t, pricing.PricingMissing, "pricing_missing 应为 false")
	})

	t.Run("claude-3-5 含 cached_price 但无 reasoning_price", func(t *testing.T) {
		pricing, ok := cache.Get("claude-3-5")
		require.True(t, ok, "应找到 claude-3-5 的定价")
		assert.Equal(t, 3.0, pricing.PromptPrice)
		assert.Equal(t, 15.0, pricing.CompletionPrice)
		assert.Nil(t, pricing.ReasoningPrice, "reasoning_price 应为 nil（server 返回 null）")
		require.NotNil(t, pricing.CachedPrice, "cached_price 不应为 nil")
		assert.Equal(t, 1.5, *pricing.CachedPrice, "cached_price 应为 1.5")
		assert.False(t, pricing.PricingMissing)
	})

	t.Run("不存在的模型返回 false", func(t *testing.T) {
		_, ok := cache.Get("nonexistent")
		assert.False(t, ok, "不存在的模型应返回 false")
	})
}

// TestMemoryPricingCache_InitialRefreshFailure 测试初始化拉取失败时的行为。
//
// 测试目的：server 返回 500 导致 refresh 失败，应递增 PricingCacheRefreshErrs
// 计数器且不崩溃，缓存保持空状态（Get 返回 false）。
func TestMemoryPricingCache_InitialRefreshFailure(t *testing.T) {
	infra.UsageMetricsInstance.Reset()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cache := infra.NewMemoryPricingCache(server.URL, "test-token")
	cache.Start(context.Background())
	defer cache.Stop()

	t.Run("错误计数器递增", func(t *testing.T) {
		assert.Greater(t, infra.UsageMetricsInstance.PricingCacheRefreshErrs.Value(), int64(0),
			"刷新失败应递增错误计数")
	})

	t.Run("缓存保持空状态", func(t *testing.T) {
		_, ok := cache.Get("gpt-4")
		assert.False(t, ok, "刷新失败后缓存应为空，Get 返回 false")
		_, ok = cache.Get("any-model")
		assert.False(t, ok, "刷新失败后任何模型均不应命中缓存")
	})
}

// TestMemoryPricingCache_EmptyPricing 测试空价表场景。
//
// 测试目的：server 返回合法但空的价表 {"models":{}}，Start 应正常完成，
// 不产生错误计数，缓存保持空状态，不崩溃、无脏数据。
func TestMemoryPricingCache_EmptyPricing(t *testing.T) {
	infra.UsageMetricsInstance.Reset()

	emptyData, err := json.Marshal(map[string]interface{}{
		"models": map[string]infra.ModelPricing{},
	})
	require.NoError(t, err, "构建空价表 JSON 不应失败")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(emptyData) //nolint:errcheck
	}))
	defer server.Close()

	cache := infra.NewMemoryPricingCache(server.URL, "test-token")
	cache.Start(context.Background())
	defer cache.Stop()

	t.Run("Get 对任何模型均返回 false", func(t *testing.T) {
		_, ok := cache.Get("any-model")
		assert.False(t, ok, "空价表下 Get 应返回 false")
		_, ok = cache.Get("")
		assert.False(t, ok, "空价表下空字符串也不应命中")
	})

	t.Run("错误计数器未递增", func(t *testing.T) {
		assert.Equal(t, int64(0), infra.UsageMetricsInstance.PricingCacheRefreshErrs.Value(),
			"合法空价表不应递增错误计数")
	})
}
