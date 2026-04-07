package pricing_test

import (
	"context"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/pricing"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestResolveUsagePricing_RerankManualOverridesCatalog(t *testing.T) {
	resolved := pricing.ResolveUsagePricing(pricing.UsagePricingInput{
		ModelType:         "rerank_vl",
		PriceMode:         "manual",
		InputPrice:        0.125,
		CatalogRawPayload: `{"rerank_per_search_unit":0.04}`,
	})
	require.NotNil(t, resolved.RerankPerSearchUnit)
	require.Equal(t, 0.125, *resolved.RerankPerSearchUnit)
}

func TestPricingTable_ResolvesRerankByActualProviderAndPriceMode(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:rerank_pricing?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE providers (id text PRIMARY KEY, name text, type text)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE model_catalog (
		id text PRIMARY KEY, input_price_per_million real, output_price_per_million real,
		cache_read_price real, cache_write_price real, tier_threshold integer,
		input_price_above_tier real, output_price_above_tier real, raw_payload text
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE models (
		id text PRIMARY KEY, name text, provider_id text, model_type text, price_mode text,
		input_price real, output_price real, catalog_id text
	)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO providers(id,name,type) VALUES
		('p1','manual-provider','cohere'), ('p2','catalog-provider','cohere')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO model_catalog(
		id,input_price_per_million,output_price_per_million,raw_payload
	) VALUES ('c1',0,0,'{"input_cost_per_query":0.04}')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO models(
		id,name,provider_id,model_type,price_mode,input_price,output_price,catalog_id
	) VALUES
		('m1','rerank-v3','p1','rerank','manual',0.125,0,NULL),
		('m2','rerank-v3','p2','rerank_vl','catalog',0,0,'c1')`).Error)

	table := pricing.NewPricingTable()
	require.NoError(t, table.Refresh(context.Background(), db))
	manual, ok := table.ResolveForProvider("rerank-v3", "manual-provider")
	require.True(t, ok)
	require.Equal(t, 0.125, *manual.RerankPerSearchUnit)
	fromCatalog, ok := table.ResolveForProvider("rerank-v3", "catalog-provider")
	require.True(t, ok)
	require.Equal(t, 0.04, *fromCatalog.RerankPerSearchUnit)
	_, ok = table.Resolve("rerank-v3")
	require.False(t, ok, "ambiguous provider-less lookup must not select another provider's price")
}

func TestResolveUsagePricing_RerankCatalogReadsSearchUnitPrice(t *testing.T) {
	resolved := pricing.ResolveUsagePricing(pricing.UsagePricingInput{
		ModelType:         "rerank",
		PriceMode:         "catalog",
		CatalogRawPayload: `{"input_cost_per_query":0.04}`,
	})
	require.NotNil(t, resolved.RerankPerSearchUnit)
	require.Equal(t, 0.04, *resolved.RerankPerSearchUnit)
}

func TestResolveUsagePricing_RerankMissingPriceRemainsZero(t *testing.T) {
	resolved := pricing.ResolveUsagePricing(pricing.UsagePricingInput{
		ModelType:         "rerank",
		PriceMode:         "catalog",
		CatalogRawPayload: `{}`,
	})
	require.Nil(t, resolved.RerankPerSearchUnit)
}
