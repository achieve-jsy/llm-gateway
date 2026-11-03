package pricing_test

import (
	"testing"

	cppricing "github.com/example/llm-gateway/internal/controlplane/pricing"
	"github.com/stretchr/testify/assert"
)

func ptr(v float64) *float64 { return &v }

func TestResolveModelPricing_ManualCachePriceOverride(t *testing.T) {
	// 中文说明：手动模型 cache 价格为空时保持现有回退；配置后覆盖 catalog cache 价。
	catalogCacheRead := ptr(0.2)
	catalogCacheWrite := ptr(0.8)

	fallback := cppricing.ResolveModelPricing(
		"manual", 1.0, 2.0,
		nil, nil,
		nil, nil,
		catalogCacheRead, catalogCacheWrite,
		nil, nil, nil,
	)
	assert.Equal(t, catalogCacheRead, fallback.CachedInputPerMtok)
	assert.Equal(t, catalogCacheWrite, fallback.CacheWritePerMtok)

	modelCacheRead := ptr(0.1)
	modelCacheWrite := ptr(0.5)
	overridden := cppricing.ResolveModelPricing(
		"manual", 1.0, 2.0,
		modelCacheRead, modelCacheWrite,
		nil, nil,
		catalogCacheRead, catalogCacheWrite,
		nil, nil, nil,
	)
	assert.Equal(t, modelCacheRead, overridden.CachedInputPerMtok)
	assert.Equal(t, modelCacheWrite, overridden.CacheWritePerMtok)
}
