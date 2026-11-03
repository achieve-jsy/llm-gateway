package observability_test

import (
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func obsF64(v float64) *float64 { return &v }

func perM(tokens int, pricePerMtok float64) float64 {
	return float64(tokens) / 1e6 * pricePerMtok
}

// TestEstimateMediaCost_Buckets 五种媒体口径逐桶算对金额；0/NULL 一律判为无价。
func TestEstimateMediaCost_Buckets(t *testing.T) {
	tests := []struct {
		name        string
		in          core.MediaCostInput
		pricing     core.ModelPricing
		nilPricing  bool
		wantCost    float64
		wantMissing bool
	}{
		{
			name:     "per_image：3 张 × 0.04",
			in:       core.MediaCostInput{Images: 3},
			pricing:  core.ModelPricing{PerImage: obsF64(0.04)},
			wantCost: 0.12,
		},
		{
			name:     "input_per_image：2 张输入 × 0.02",
			in:       core.MediaCostInput{InputImages: 2},
			pricing:  core.ModelPricing{InputPerImage: obsF64(0.02)},
			wantCost: 0.04,
		},
		{
			name:     "per_second_output：10.5 秒 × 0.05",
			in:       core.MediaCostInput{OutputSeconds: 10.5},
			pricing:  core.ModelPricing{PerSecondOutput: obsF64(0.05)},
			wantCost: 0.525,
		},
		{
			name:     "per_second_input：4 秒 × 0.4",
			in:       core.MediaCostInput{InputSeconds: 4},
			pricing:  core.ModelPricing{PerSecondInput: obsF64(0.4)},
			wantCost: 1.6,
		},
		{
			name:     "per_request：2 次 × 0.005",
			in:       core.MediaCostInput{Requests: 2},
			pricing:  core.ModelPricing{PerRequest: obsF64(0.005)},
			wantCost: 0.01,
		},
		{
			name:     "per_image_token：1000 图像 token × 0.00004（gpt-image）",
			in:       core.MediaCostInput{OutputImageTokens: 1000},
			pricing:  core.ModelPricing{OutputPerImageToken: obsF64(0.00004)},
			wantCost: 0.04,
		},
		{
			name:     "input_per_image_token：2000 图像 token × 0.00001",
			in:       core.MediaCostInput{InputImageTokens: 2000},
			pricing:  core.ModelPricing{InputPerImageToken: obsF64(0.00001)},
			wantCost: 0.02,
		},
		{
			name: "dall-e-3 式命名回退：只有 input_cost_per_image 时，生成张数也按它计价",
			in:   core.MediaCostInput{Images: 2},
			// dall-e-3 在 LiteLLM 里只有 input_cost_per_image = 0.04
			pricing:  core.ModelPricing{InputPerImage: obsF64(0.04)},
			wantCost: 0.08,
		},
		{
			name:        "回退也要求有价：input_cost_per_image = 0 仍是 missing",
			in:          core.MediaCostInput{Images: 2},
			pricing:     core.ModelPricing{InputPerImage: obsF64(0)},
			wantMissing: true,
		},
		{
			name:        "图像 token 有用量但无价 → missing",
			in:          core.MediaCostInput{OutputImageTokens: 100},
			pricing:     core.ModelPricing{PerImage: obsF64(0.04)},
			wantMissing: true,
		},
		{
			name: "组合口径：图像 + 输入图 + 秒 + 按次",
			in:   core.MediaCostInput{Images: 2, InputImages: 1, OutputSeconds: 3, Requests: 1},
			pricing: core.ModelPricing{
				PerImage: obsF64(0.04), InputPerImage: obsF64(0.02),
				PerSecondOutput: obsF64(0.5), PerRequest: obsF64(0.005),
			},
			wantCost: 0.08 + 0.02 + 1.5 + 0.005,
		},
		{
			name:        "字段存在但值为 0 → missing=true、cost=0",
			in:          core.MediaCostInput{Images: 1},
			pricing:     core.ModelPricing{PerImage: obsF64(0)},
			wantMissing: true,
		},
		{
			name:        "字段为 NULL → missing=true、cost=0",
			in:          core.MediaCostInput{Images: 1},
			pricing:     core.ModelPricing{},
			wantMissing: true,
		},
		{
			name:        "负价 → missing=true",
			in:          core.MediaCostInput{OutputSeconds: 1},
			pricing:     core.ModelPricing{PerSecondOutput: obsF64(-0.5)},
			wantMissing: true,
		},
		{
			name:        "有用量但只有别口径的价 → missing=true（不允许串口径）",
			in:          core.MediaCostInput{OutputSeconds: 5},
			pricing:     core.ModelPricing{PerImage: obsF64(0.04)},
			wantMissing: true,
		},
		{
			name:        "无任何用量 → missing=true（拿不到用量不能当免费）",
			in:          core.MediaCostInput{},
			pricing:     core.ModelPricing{PerImage: obsF64(0.04)},
			wantMissing: true,
		},
		{
			name:        "nil pricing → 不 panic，missing=true",
			in:          core.MediaCostInput{Images: 1},
			nilPricing:  true,
			wantMissing: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var pricing *core.ModelPricing
			if !tt.nilPricing {
				p := tt.pricing
				pricing = &p
			}
			cost, missing := gwobs.EstimateMediaCost(tt.in, pricing)
			assert.Equal(t, tt.wantMissing, missing, "missing")
			assert.InDelta(t, tt.wantCost, cost, 1e-9, "cost")
		})
	}
}

// TestEstimateCost_TokenSemanticsUnchanged 回归：EstimateCost 的 token 语义与
// 返回值必须与改动前完全一致（含缺价、nil pricing 的 missing 行为）。
func TestEstimateCost_TokenSemanticsUnchanged(t *testing.T) {
	full := core.ModelPricing{
		InputPerMtok:       obsF64(1.0),
		OutputPerMtok:      obsF64(2.0),
		CachedInputPerMtok: obsF64(0.5),
		// 同时挂着媒体价：token 口径不得被它影响。
		PerImage: obsF64(0.04),
	}
	missingOutput := core.ModelPricing{InputPerMtok: obsF64(1.0)}

	tests := []struct {
		name        string
		in          core.CostInput
		pricing     *core.ModelPricing
		wantCost    float64
		wantMissing bool
	}{
		{
			name:     "普通 token 计价",
			in:       core.CostInput{PromptTokens: 1000, CompletionTokens: 500},
			pricing:  &full,
			wantCost: perM(1000, 1.0) + perM(500, 2.0),
		},
		{
			name:     "带缓存读折扣",
			in:       core.CostInput{PromptTokens: 1000, CachedTokens: 400},
			pricing:  &full,
			wantCost: perM(600, 1.0) + perM(400, 0.5),
		},
		{
			name:        "缺 output 价 → missing（旧语义不变）",
			in:          core.CostInput{PromptTokens: 10},
			pricing:     &missingOutput,
			wantMissing: true,
		},
		{
			name:        "nil pricing → missing（旧语义不变）",
			in:          core.CostInput{PromptTokens: 10},
			pricing:     nil,
			wantMissing: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost, missing := gwobs.EstimateCost(tt.in, tt.pricing)
			assert.Equal(t, tt.wantMissing, missing)
			assert.InDelta(t, tt.wantCost, cost, 1e-9)
		})
	}
}

// TestUsageEvent_EstimateCost_MediaPath 端到端：媒体链路写入 media_usage 后
// 采集器按媒体口径锁价，而不是被 InputPerMtok 误判成 token 计价。
func TestUsageEvent_EstimateCost_MediaPath(t *testing.T) {
	gin.SetMode(gin.TestMode)

	imagePrice := 0.04
	collector := gwobs.NewCollector(gwobs.DefaultCollectorConfig(), gwobs.PricingResolverFunc(
		func(string) (core.ModelPricing, bool) {
			return core.ModelPricing{PerImage: &imagePrice}, true
		},
	))

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/images/generations", nil)
	ctx.Set("model", "dall-e-3")
	ctx.Set("status", 200)
	ctx.Set(gwobs.CtxKeyMediaUsage, core.MediaCostInput{Images: 3})

	event := collector.NewUsageEvent(ctx)
	assert.Equal(t, 3, event.Media.Images)
	assert.InDelta(t, 0.12, event.EstCostUSD, 1e-9, "3 张 × 0.04")
	assert.InDelta(t, 0.12, event.OutputCostUSD, 1e-9, "输出分项")
	assert.False(t, event.UsageMissing, "有价时不应标记 usage_missing")
}

// TestUsageEvent_EstimateCost_MediaNoPriceIsMissing 无价（缺失 / 0 占位）时必须
// usage_missing=true 且 EstCostUSD=0，绝不静默记 0 元。
func TestUsageEvent_EstimateCost_MediaNoPriceIsMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name    string
		pricing core.ModelPricing
	}{
		{name: "没有媒体价", pricing: core.ModelPricing{}},
		{name: "媒体价字段存在但为 0（catalog 0 占位）", pricing: core.ModelPricing{PerImage: obsF64(0)}},
		{name: "只有 0 值的 token 价，且本次没有 token 用量", pricing: core.ModelPricing{InputPerMtok: obsF64(0), OutputPerMtok: obsF64(0)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := tt.pricing
			collector := gwobs.NewCollector(gwobs.DefaultCollectorConfig(), gwobs.PricingResolverFunc(
				func(string) (core.ModelPricing, bool) { return p, true },
			))

			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", "/v1/images/generations", nil)
			ctx.Set("model", "dall-e-3")
			ctx.Set(gwobs.CtxKeyMediaUsage, core.MediaCostInput{Images: 3})

			event := collector.NewUsageEvent(ctx)
			assert.True(t, event.UsageMissing, "无价必须标记 usage_missing")
			assert.Zero(t, event.EstCostUSD, "无价不得产出成本")
		})
	}
}

// TestUsageEvent_EstimateCost_MediaFallsBackToTokenPrice 媒体请求同时带 token 用量、
// 且模型配了正 token 价时，允许回退 token 口径（gpt-image 的 image token 计价）。
func TestUsageEvent_EstimateCost_MediaFallsBackToTokenPrice(t *testing.T) {
	gin.SetMode(gin.TestMode)

	inputPrice, outputPrice := 5.0, 40.0
	collector := gwobs.NewCollector(gwobs.DefaultCollectorConfig(), gwobs.PricingResolverFunc(
		func(string) (core.ModelPricing, bool) {
			return core.ModelPricing{InputPerMtok: &inputPrice, OutputPerMtok: &outputPrice}, true
		},
	))

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/images/generations", nil)
	ctx.Set("model", "gpt-image-1")
	ctx.Set("prompt_tokens", 1000)
	ctx.Set("completion_tokens", 100)
	ctx.Set(gwobs.CtxKeyMediaUsage, core.MediaCostInput{Images: 1})

	event := collector.NewUsageEvent(ctx)
	require.False(t, event.UsageMissing)
	assert.InDelta(t, perM(1000, 5.0)+perM(100, 40.0), event.EstCostUSD, 1e-9)
}
