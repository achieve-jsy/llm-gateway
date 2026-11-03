package pricing_test

import (
	"testing"

	cppricing "github.com/example/llm-gateway/internal/controlplane/pricing"
	"github.com/example/llm-gateway/internal/core"
	"github.com/stretchr/testify/assert"
)

func mediaPrice(v float64) *float64 { return &v }

// catalogImageMedia 构造只配了媒体价的 catalog 输入（模拟 model_catalog 新列）。
// mode 取值来自真实 LiteLLM catalog（image_generation/image_edit/video_generation）。
func catalogImageMedia() cppricing.MediaPricingInput {
	return cppricing.MediaPricingInput{
		ModelType:           "image",
		CatalogMode:         "image_generation",
		PriceMode:           "catalog",
		PerImage:            mediaPrice(0.04),
		InputPerImage:       mediaPrice(0.02),
		PerSecondInput:      mediaPrice(0.4),
		PerSecondOutput:     mediaPrice(0.5),
		PerRequest:          mediaPrice(0.005),
		InputPerImageToken:  mediaPrice(0.00001),
		OutputPerImageToken: mediaPrice(0.00004),
		InputPerPixel:       mediaPrice(0.000000019),
		OutputPerPixel:      mediaPrice(0.0), // 0 占位 → 必须保持 nil
		InputPerVideoToken:  mediaPrice(0.0002),
		OutputPerVideoToken: mediaPrice(0.0003),
		PerCharacterInput:   mediaPrice(0.000015),
	}
}

func TestApplyCatalogMediaPricing_ByModelType(t *testing.T) {
	tests := []struct {
		name          string
		modelType     string
		catalogMode   string // "" = 不覆盖 fixture 的 image_generation
		priceMode     string
		input         func() cppricing.MediaPricingInput
		wantMedia     bool
		wantUnchanged bool // token 字段必须一个都不动
	}{
		{
			name:      "image + catalog → 全部媒体价生效",
			modelType: "image",
			priceMode: "catalog",
			input:     catalogImageMedia,
			wantMedia: true,
		},
		{
			name:      "video + catalog → 全部媒体价生效",
			modelType: "video",
			priceMode: "catalog",
			input:     catalogImageMedia,
			wantMedia: true,
		},
		{
			name:      "image + video price_mode → 仍按 catalog 媒体价生效",
			modelType: "image",
			priceMode: "video",
			input:     catalogImageMedia,
			wantMedia: true,
		},
		{
			name:      "大写/带空格的 model_type 归一后生效",
			modelType: "  IMAGE ",
			priceMode: "catalog",
			input:     catalogImageMedia,
			wantMedia: true,
		},
		{
			name:        "catalog mode=image_edit → 生效（编辑链路的输入图价）",
			modelType:   "image",
			catalogMode: "image_edit",
			priceMode:   "catalog",
			input:       catalogImageMedia,
			wantMedia:   true,
		},
		{
			name:        "catalog mode=video_generation → 生效",
			modelType:   "video",
			catalogMode: "video_generation",
			priceMode:   "catalog",
			input:       catalogImageMedia,
			wantMedia:   true,
		},
		{
			name:          "catalog mode=audio_speech → 不动（非图像/视频媒体链路）",
			modelType:     "chat",
			catalogMode:   "audio_speech",
			priceMode:     "catalog",
			input:         catalogImageMedia,
			wantUnchanged: true,
		},
		{
			name:          "catalog mode=chat → 不动（即便 model_type 写的是 image，以 mode 为准）",
			modelType:     "image",
			catalogMode:   "chat",
			priceMode:     "catalog",
			input:         catalogImageMedia,
			wantUnchanged: true,
		},
		{
			name:          "embedding_vl → 不动",
			modelType:     "embedding_vl",
			catalogMode:   "embedding",
			priceMode:     "catalog",
			input:         catalogImageMedia,
			wantUnchanged: true,
		},
		{
			name:          "rerank_vl → 不动（走 ResolveUsagePricing）",
			modelType:     "rerank_vl",
			catalogMode:   "rerank",
			priceMode:     "catalog",
			input:         catalogImageMedia,
			wantUnchanged: true,
		},
		{
			name:          "image + manual → 暂不解析媒体价（models 表还没有媒体价列）",
			modelType:     "image",
			catalogMode:   "image_generation",
			priceMode:     "manual",
			input:         catalogImageMedia,
			wantUnchanged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := tt.input()
			input.ModelType = tt.modelType
			input.PriceMode = tt.priceMode
			if tt.catalogMode != "" {
				input.CatalogMode = tt.catalogMode
			}

			// token 价预置：媒体分支绝不能改到它们。
			p := core.ModelPricing{
				InputPerMtok:        mediaPrice(1.0),
				OutputPerMtok:       mediaPrice(2.0),
				CachedInputPerMtok:  mediaPrice(0.1),
				RerankPerSearchUnit: mediaPrice(0.25),
			}
			cppricing.ApplyCatalogMediaPricing(&p, input)

			assert.Equal(t, 1.0, *p.InputPerMtok, "input_per_mtok 不可被媒体分支改动")
			assert.Equal(t, 2.0, *p.OutputPerMtok, "output_per_mtok 不可被媒体分支改动")
			assert.Equal(t, 0.1, *p.CachedInputPerMtok, "cached_input_per_mtok 不可被媒体分支改动")
			assert.Equal(t, 0.25, *p.RerankPerSearchUnit, "rerank 单价不可被媒体分支改动")

			if tt.wantUnchanged {
				assert.Nil(t, p.PerImage)
				assert.Nil(t, p.InputPerImage)
				assert.Nil(t, p.PerSecondInput)
				assert.Nil(t, p.PerSecondOutput)
				assert.Nil(t, p.PerRequest)
				assert.Nil(t, p.InputPerImageToken)
				assert.Nil(t, p.OutputPerImageToken)
				assert.Nil(t, p.InputPerPixel)
				assert.Nil(t, p.OutputPerPixel)
				assert.Nil(t, p.InputPerVideoToken)
				assert.Nil(t, p.OutputPerVideoToken)
				assert.Nil(t, p.PerCharacterInput)
				return
			}
			if tt.wantMedia {
				assert.Equal(t, 0.04, *p.PerImage)
				assert.Equal(t, 0.02, *p.InputPerImage)
				assert.Equal(t, 0.4, *p.PerSecondInput)
				assert.Equal(t, 0.5, *p.PerSecondOutput)
				assert.Equal(t, 0.005, *p.PerRequest)
				assert.Equal(t, 0.00001, *p.InputPerImageToken)
				assert.Equal(t, 0.00004, *p.OutputPerImageToken)
				assert.Equal(t, 0.000000019, *p.InputPerPixel)
				assert.Nil(t, p.OutputPerPixel, "0 占位不得视为有价")
				assert.Equal(t, 0.0002, *p.InputPerVideoToken)
				assert.Equal(t, 0.0003, *p.OutputPerVideoToken)
				assert.Equal(t, 0.000015, *p.PerCharacterInput)
			}
		})
	}
}

// TestApplyCatalogMediaPricing_ZeroAndNilAreUnpriced 0 与 NULL 都必须是「无价」，
// 不能因为字段存在就当成有价（静默 0 元的根因）。
func TestApplyCatalogMediaPricing_ZeroAndNilAreUnpriced(t *testing.T) {
	p := core.ModelPricing{}
	cppricing.ApplyCatalogMediaPricing(&p, cppricing.MediaPricingInput{
		ModelType:           "image",
		CatalogMode:         "image_generation",
		PriceMode:           "catalog",
		PerImage:            mediaPrice(0),
		InputPerImage:       mediaPrice(-1),
		PerSecondInput:      nil,
		PerSecondOutput:     mediaPrice(0),
		PerRequest:          nil,
		InputPerImageToken:  mediaPrice(0),
		OutputPerImageToken: nil,
		InputPerPixel:       mediaPrice(0),
		InputPerVideoToken:  mediaPrice(0),
		PerCharacterInput:   mediaPrice(0),
	})

	assert.Nil(t, p.PerImage, "0 价不是有价")
	assert.Nil(t, p.InputPerImage, "负价不是有价")
	assert.Nil(t, p.PerSecondInput)
	assert.Nil(t, p.PerSecondOutput)
	assert.Nil(t, p.PerRequest)
	assert.Empty(t, p.FieldSources("database"), "无价模型不应产生任何价格字段来源")
}

func TestApplyCatalogMediaPricing_NilPricingIsSafe(t *testing.T) {
	assert.NotPanics(t, func() {
		cppricing.ApplyCatalogMediaPricing(nil, catalogImageMedia())
	})
}
