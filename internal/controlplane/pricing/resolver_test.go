package pricing

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/example/llm-gateway/internal/core"
)

func f64(v float64) *float64 { return &v }
func i64(v int64) *int64     { return &v }

func TestResolveModelPricing_Catalog(t *testing.T) {
	p := ResolveModelPricing(
		"catalog",
		0, 0, // input/output_price (ignored in catalog mode)
		f64(2.5), f64(15.0), // per 1M from catalog
		f64(0.25), f64(2.5), // cache_read, cache_write
		nil, nil, nil, // no tier
	)
	require.NotNil(t, p.InputPerMtok)
	require.NotNil(t, p.OutputPerMtok)
	assert.Equal(t, 2.5, *p.InputPerMtok)
	assert.Equal(t, 15.0, *p.OutputPerMtok)
	assert.Equal(t, 0.25, *p.CachedInputPerMtok)
	assert.Equal(t, 2.5, *p.CacheWritePerMtok)
}

func TestResolveModelPricing_CatalogMissingBound(t *testing.T) {
	p := ResolveModelPricing("catalog", 0, 0, nil, nil, nil, nil, nil, nil, nil)
	// nil input/output means missing → empty pricing
	assert.Nil(t, p.InputPerMtok)
	assert.Nil(t, p.OutputPerMtok)
}

func TestResolveModelPricing_Manual(t *testing.T) {
	p := ResolveModelPricing(
		"manual",
		3.0, 12.0, // manual input/output price
		f64(999), f64(999), // catalog prices ignored
		nil, nil, nil, nil, nil,
	)
	require.NotNil(t, p.InputPerMtok)
	assert.Equal(t, 3.0, *p.InputPerMtok)
	assert.Equal(t, 12.0, *p.OutputPerMtok)
}

func TestResolveModelPricing_ManualMissingPrice(t *testing.T) {
	p := ResolveModelPricing("manual", 0, 0, nil, nil, nil, nil, nil, nil, nil)
	// manual with no price → empty
	assert.Nil(t, p.InputPerMtok)
}

func TestResolveModelPricing_WithTier(t *testing.T) {
	p := ResolveModelPricing(
		"catalog",
		0, 0,
		f64(3.0), f64(15.0), // base price
		nil, nil,
		i64(200000), f64(6.0), f64(25.0), // above 200K tier
	)
	require.Len(t, p.Tiers, 1)
	assert.Equal(t, float64(200000), *p.Tiers[0].UpToTokens)
	assert.Equal(t, 6.0, *p.Tiers[0].InputPerMtok)
	assert.Equal(t, 25.0, *p.Tiers[0].OutputPerMtok)
}

func TestPricingTable_ResolveAndGetAll(t *testing.T) {
	table := NewPricingTable()

	// Initially empty
	_, ok := table.Resolve("gpt-4o")
	assert.False(t, ok)
	assert.Empty(t, table.GetAll())

	// Manually inject data for test
	table.mu.Lock()
	inP := 2.5
	outP := 15.0
	table.data["gpt-4o"] = core.ModelPricing{
		InputPerMtok:  &inP,
		OutputPerMtok: &outP,
	}
	table.mu.Unlock()

	p, ok := table.Resolve("gpt-4o")
	require.True(t, ok)
	assert.Equal(t, 2.5, *p.InputPerMtok)

	all := table.GetAll()
	assert.Len(t, all, 1)
	assert.Contains(t, all, "gpt-4o")

	// Missing model
	_, ok = table.Resolve("unknown-model")
	assert.False(t, ok)
}