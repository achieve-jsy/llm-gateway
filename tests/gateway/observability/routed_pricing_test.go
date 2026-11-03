package observability_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/core"
	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCollector_UsesFinallyRoutedModelPriceBucketsAndSearchUnits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rerankPrice := 0.25
	resolvedKey := ""
	resolvedProvider := ""
	collector := gwobs.NewCollector(gwobs.DefaultCollectorConfig(), gwobs.ProviderPricingResolverFunc(
		func(modelKey, provider string) (core.ModelPricing, bool) {
			resolvedKey = modelKey
			resolvedProvider = provider
			return core.ModelPricing{RerankPerSearchUnit: &rerankPrice}, true
		},
	))
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/rerank", nil)
	ctx.Set("model", "public-group")
	ctx.Set("routed_model", "cohere/rerank-v3.5")
	ctx.Set("provider", "cohere-primary")
	ctx.Set("usage", core.Usage{TotalTokens: 42})
	ctx.Set("search_units", 3.0)

	event := collector.NewUsageEvent(ctx)
	require.Equal(t, "cohere/rerank-v3.5", resolvedKey)
	require.Equal(t, "cohere-primary", resolvedProvider)
	require.Equal(t, 3.0, event.SearchUnits)
	require.Equal(t, 0.75, event.EstCostUSD)
}

func TestCollector_MissingPricingKeepsZeroCost(t *testing.T) {
	collector := gwobs.NewCollector(gwobs.DefaultCollectorConfig(), gwobs.PricingResolverFunc(
		func(string) (core.ModelPricing, bool) { return core.ModelPricing{}, false },
	))
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	ctx.Set("routed_model", "unknown")
	ctx.Set("usage", core.Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150})

	event := collector.NewUsageEvent(ctx)
	require.Zero(t, event.EstCostUSD)
}

func TestCollectorConfig_PricingAndReconnectDefaultsAreIndependent(t *testing.T) {
	cfg := gwobs.DefaultCollectorConfig()
	require.Equal(t, 5*time.Second, cfg.DBConnectRetryInterval)
	require.Equal(t, 5*time.Minute, cfg.PriceRefreshInterval)
	require.Equal(t, int64(100_000), cfg.SpoolCapacity)
	require.Equal(t, int64(80_000), cfg.SpoolHighWatermark)
	require.Equal(t, 3, cfg.SpoolMaxRetries)
	require.Equal(t, 30*time.Second, cfg.SpoolClaimMinIdle)
	require.Equal(t, 10*time.Minute, cfg.BillableReservationTTL)
}

func TestProductionCollector_UsesIndependentPriceRefreshOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:pricing_refresh_interval?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE providers (id text PRIMARY KEY, name text, type text)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE model_catalog (
		id text PRIMARY KEY, mode text,
		input_price_per_million real, output_price_per_million real,
		cache_read_price real, cache_write_price real, tier_threshold integer,
		input_price_above_tier real, output_price_above_tier real, raw_payload text,
		per_image real, input_per_image real, per_second_input real, per_second_output real,
		per_request real, input_per_image_token real, output_per_image_token real,
		input_per_pixel real, output_per_pixel real,
		input_per_video_token real, output_per_video_token real, per_character_input real
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE models (
		id text PRIMARY KEY, name text, provider_id text, model_type text, price_mode text,
		input_price real, output_price real, catalog_id text,
		cache_read_price real, cache_write_price real
	)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE call_logs (
		request_id text PRIMARY KEY, key_type text, budget_tier_constraints_snapshot text
	)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO providers(id,name,type) VALUES ('p1','cohere-primary','cohere')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO model_catalog(
		id,input_price_per_million,output_price_per_million,raw_payload
	) VALUES ('c1',0,0,'{"input_cost_per_query":0.04}')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO models(
		id,name,provider_id,model_type,price_mode,input_price,output_price,catalog_id
	) VALUES ('m1','rerank-v3','p1','rerank','catalog',0,0,'c1')`).Error)

	cfg := gwobs.DefaultCollectorConfig()
	cfg.DBConnectRetryInterval = 10 * time.Millisecond
	cfg.PriceRefreshInterval = 250 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	collector := gwobs.StartProductionCollector(
		ctx, cfg, nil, nil,
		func(context.Context) (*gorm.DB, error) { return db, nil },
		nil,
	)
	cost := func() float64 {
		ginCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ginCtx.Request = httptest.NewRequest("POST", "/v1/rerank", nil)
		ginCtx.Set("routed_model", "rerank-v3")
		ginCtx.Set("provider", "cohere-primary")
		ginCtx.Set("usage", core.Usage{TotalTokens: 1})
		ginCtx.Set("search_units", 2.0)
		return collector.NewUsageEvent(ginCtx).EstCostUSD
	}
	require.Eventually(t, func() bool { return cost() == 0.08 }, time.Second, 10*time.Millisecond)

	require.NoError(t, db.Exec(`UPDATE model_catalog SET raw_payload='{"input_cost_per_query":0.09}' WHERE id='c1'`).Error)
	time.Sleep(80 * time.Millisecond)
	require.Equal(t, 0.08, cost(), "DB reconnect ticker must not refresh pricing")
	require.Eventually(t, func() bool { return cost() == 0.18 }, time.Second, 10*time.Millisecond)
}
