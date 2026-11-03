package pricing_test

import (
	"context"
	"fmt"
	"testing"

	cppricing "github.com/example/llm-gateway/internal/controlplane/pricing"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// mediaPriceTableDB 建一个只含 Refresh 所需列的内存库（与生产 SELECT 逐列对齐）。
func mediaPriceTableDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:media_pricing_refresh_%s?mode=memory&cache=shared", uuid.NewString())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
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
	require.NoError(t, db.Exec(`INSERT INTO providers(id,name,type) VALUES ('p1','openai-primary','openai')`).Error)
	return db
}

// TestPricingTableRefresh_MediaPrices 验证 Refresh 的 SQL 真能把 model_catalog
// 的媒体价带进内存价表，并且 mode 分派在真实查询路径上生效。
func TestPricingTableRefresh_MediaPrices(t *testing.T) {
	tests := []struct {
		name          string
		catalogMode   string
		modelType     string
		perImage      string
		wantPerImage  *float64
		wantInputImg  *float64
		wantSecondOut *float64
	}{
		{
			name:          "image_generation + per_image 生效",
			catalogMode:   "image_generation",
			modelType:     "image",
			perImage:      "0.04",
			wantPerImage:  mediaPrice(0.04),
			wantInputImg:  mediaPrice(0.02),
			wantSecondOut: mediaPrice(0.5),
		},
		{
			name:        "mode=chat → 媒体列即使有 0.99 也不生效（以 mode 为准）",
			catalogMode: "chat",
			modelType:   "image",
			perImage:    "0.99",
		},
		{
			name:         "catalog mode 为空（未绑定 catalog）→ 回退 model_type=image",
			catalogMode:  "",
			modelType:    "image",
			perImage:     "0.04",
			wantPerImage: mediaPrice(0.04),
		},
		{
			name:         "mode=image_generation 但 model_type=chat → 仍生效（目录 mode 权威）",
			catalogMode:  "image_generation",
			modelType:    "chat",
			perImage:     "0.04",
			wantPerImage: mediaPrice(0.04),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := mediaPriceTableDB(t)
			require.NoError(t, db.Exec(`INSERT INTO model_catalog(
				id,mode,input_price_per_million,output_price_per_million,
				per_image,input_per_image,per_second_input,per_second_output,per_request,
				input_per_image_token,raw_payload
			) VALUES ('c1',NULLIF(?,''),10,20,?,0.02,0.4,0.5,0.005,0.00001,'{}')`,
				tt.catalogMode, tt.perImage).Error)
			require.NoError(t, db.Exec(`INSERT INTO models(
				id,name,provider_id,model_type,price_mode,input_price,output_price,catalog_id
			) VALUES ('m1','media-model','p1',?,'catalog',0,0,'c1')`, tt.modelType).Error)

			table := cppricing.NewPricingTable()
			require.NoError(t, table.Refresh(context.Background(), db))

			got, ok := table.Resolve("media-model")
			require.True(t, ok, "模型应进入价表")

			if tt.wantPerImage == nil {
				assert.Nil(t, got.PerImage, "不该带上媒体价")
				assert.Nil(t, got.InputPerImage)
				assert.Nil(t, got.PerSecondOutput)
			} else {
				require.NotNil(t, got.PerImage)
				assert.Equal(t, *tt.wantPerImage, *got.PerImage)
				require.NotNil(t, got.InputPerImage)
				assert.Equal(t, 0.02, *got.InputPerImage)
				require.NotNil(t, got.PerSecondOutput)
				assert.Equal(t, 0.5, *got.PerSecondOutput)
				require.NotNil(t, got.PerSecondInput)
				assert.Equal(t, 0.4, *got.PerSecondInput)
				require.NotNil(t, got.PerRequest)
				assert.Equal(t, 0.005, *got.PerRequest)
				require.NotNil(t, got.InputPerImageToken)
				assert.Equal(t, 0.00001, *got.InputPerImageToken)
			}
			// token 价在任何分支都不受影响。
			require.NotNil(t, got.InputPerMtok)
			assert.Equal(t, 10.0, *got.InputPerMtok)
			require.NotNil(t, got.OutputPerMtok)
			assert.Equal(t, 20.0, *got.OutputPerMtok)
		})
	}
}

// TestPricingTableRefresh_ZeroMediaPriceIsNotPriced 列里存了 0（旧数据/占位）时
// 不能被当成有价：Resolve 回来必须是 nil。
func TestPricingTableRefresh_ZeroMediaPriceIsNotPriced(t *testing.T) {
	db := mediaPriceTableDB(t)
	require.NoError(t, db.Exec(`INSERT INTO model_catalog(
		id,mode,input_price_per_million,output_price_per_million,per_image,raw_payload
	) VALUES ('c1','image_generation',0,0,0,'{}')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO models(
		id,name,provider_id,model_type,price_mode,input_price,output_price,catalog_id
	) VALUES ('m1','zero-price-image','p1','image','catalog',0,0,'c1')`).Error)

	table := cppricing.NewPricingTable()
	require.NoError(t, table.Refresh(context.Background(), db))

	got, ok := table.Resolve("zero-price-image")
	require.True(t, ok)
	assert.Nil(t, got.PerImage, "0 价不得视为有价")
}

// TestPricingTableGetModelPricing_MediaOnlyModel 只配媒体价的模型不再被判为「查不到」。
func TestPricingTableGetModelPricing_MediaOnlyModel(t *testing.T) {
	db := mediaPriceTableDB(t)
	require.NoError(t, db.Exec(`INSERT INTO model_catalog(
		id,mode,input_price_per_million,output_price_per_million,per_image,raw_payload
	) VALUES ('c1','image_generation',0,0,0.04,'{}')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO models(
		id,name,provider_id,model_type,price_mode,input_price,output_price,catalog_id
	) VALUES ('m1','media-only','p1','image','catalog',0,0,'c1')`).Error)

	table := cppricing.NewPricingTable()
	require.NoError(t, table.Refresh(context.Background(), db))

	got := table.GetModelPricing("media-only")
	require.NotNil(t, got, "只有媒体价的模型也应能解析出定价")
	require.NotNil(t, got.PerImage)
	assert.Equal(t, 0.04, *got.PerImage)
}
