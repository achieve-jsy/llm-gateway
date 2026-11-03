package observability_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/example/llm-gateway/internal/gateway/observability"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件覆盖 Batch 1 · B6 的**计价闭环**：handler 写入的媒体用量，
// 经过采集器（NewUsageEvent → EstimateCost）必须真的变成钱。
//
// 这条测试针对的是历史上出现过的三类「静默 0 元」：
//  1. 用量没写进去（context key 错 / 没写）；
//  2. 写了但类型不对（采集器对 core.MediaCostInput 做**精确类型断言**，
//     指针或自定义别名会被静默丢弃）；
//  3. 写了、类型也对，但单价缺失/为 0 → 必须标 usage_missing，不得记 0 元。

func newMediaCollector(t *testing.T, price core.ModelPricing, ok bool) *observability.Collector {
	t.Helper()
	return observability.NewCollector(observability.CollectorConfig{},
		observability.ProviderPricingResolverFunc(func(string, string) (core.ModelPricing, bool) {
			return price, ok
		}))
}

func newMediaEventContext(t *testing.T, media any) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	ctx.Set("request_id", "req-media-1")
	ctx.Set("routed_model", "gpt-image-1")
	ctx.Set("provider", "openai")
	if media != nil {
		ctx.Set(observability.CtxKeyMediaUsage, media)
	}
	return ctx
}

func f64(v float64) *float64 { return &v }

func TestMediaCost_PerImageClosedLoop(t *testing.T) {
	// 场景：dall-e-3 类模型，LiteLLM 只给 input_cost_per_image。
	price := core.ModelPricing{InputPerImage: f64(0.04)}
	collector := newMediaCollector(t, price, true)

	ctx := newMediaEventContext(t, core.MediaCostInput{Images: 2})
	event := collector.NewUsageEvent(ctx)

	assert.Equal(t, core.MediaCostInput{Images: 2}, event.Media)
	assert.False(t, event.UsageMissing, "有真实媒体用量 + 有单价，不得标 usage_missing")
	assert.InDelta(t, 0.08, event.EstCostUSD, 1e-9, "2 张 × $0.04（Images 桶回退 input_cost_per_image）")
}

func TestMediaCost_PerImageWithRequestPrice(t *testing.T) {
	// 完整的「每张 + 每次」价：两个桶都有价，成本相加。
	price := core.ModelPricing{InputPerImage: f64(0.04), PerRequest: f64(0.01)}
	collector := newMediaCollector(t, price, true)

	ctx := newMediaEventContext(t, core.MediaCostInput{Images: 3, Requests: 1})
	event := collector.NewUsageEvent(ctx)

	assert.False(t, event.UsageMissing)
	assert.InDelta(t, 0.13, event.EstCostUSD, 1e-9, "3 × 0.04 + 1 × 0.01")
}

func TestMediaCost_MissingRequestPriceMarksUsageMissing(t *testing.T) {
	// 用量里带了 Requests，但价表没有 PerRequest → 整体 missing（宁缺毋滥，不静默 0 元）。
	price := core.ModelPricing{InputPerImage: f64(0.04)}
	collector := newMediaCollector(t, price, true)

	ctx := newMediaEventContext(t, core.MediaCostInput{Images: 1, Requests: 1})
	event := collector.NewUsageEvent(ctx)

	assert.True(t, event.UsageMissing, "任一有用量的桶缺价都必须标 usage_missing")
	assert.Zero(t, event.EstCostUSD)
}

func TestMediaCost_PerImageToken(t *testing.T) {
	price := core.ModelPricing{InputPerImageToken: f64(0.00001), OutputPerImageToken: f64(0.00004)}
	collector := newMediaCollector(t, price, true)

	ctx := newMediaEventContext(t, core.MediaCostInput{InputImageTokens: 1000, OutputImageTokens: 2000})
	event := collector.NewUsageEvent(ctx)

	assert.False(t, event.UsageMissing)
	assert.InDelta(t, 1000*0.00001, event.InputCostUSD, 1e-9)
	assert.InDelta(t, 2000*0.00004, event.OutputCostUSD, 1e-9)
	assert.InDelta(t, 0.09, event.EstCostUSD, 1e-9)
}

func TestMediaCost_PerSecond(t *testing.T) {
	price := core.ModelPricing{PerSecondInput: f64(0.1), PerSecondOutput: f64(0.2)}
	collector := newMediaCollector(t, price, true)

	ctx := newMediaEventContext(t, core.MediaCostInput{InputSeconds: 2, OutputSeconds: 3})
	event := collector.NewUsageEvent(ctx)

	assert.False(t, event.UsageMissing)
	assert.InDelta(t, 0.2+0.6, event.EstCostUSD, 1e-9)
}

func TestMediaCost_ZeroPriceIsNotFree(t *testing.T) {
	// LiteLLM 常用 0.0 占位；0 价必须视作无价（否则会静默记 0 元）。
	price := core.ModelPricing{InputPerImage: f64(0)}
	collector := newMediaCollector(t, price, true)

	ctx := newMediaEventContext(t, core.MediaCostInput{Images: 1})
	event := collector.NewUsageEvent(ctx)

	assert.True(t, event.UsageMissing)
	assert.Zero(t, event.EstCostUSD)
}

func TestMediaCost_UnknownModelMarksUsageMissing(t *testing.T) {
	collector := newMediaCollector(t, core.ModelPricing{}, false) // ok=false：价表里没有这个模型

	ctx := newMediaEventContext(t, core.MediaCostInput{Images: 1})
	event := collector.NewUsageEvent(ctx)

	assert.True(t, event.UsageMissing)
	assert.Zero(t, event.EstCostUSD)
}

func TestMediaCost_NoUsageMarksUsageMissing(t *testing.T) {
	// 完全没写媒体用量（漏写 context key）→ 不能当作免费。
	collector := newMediaCollector(t, core.ModelPricing{InputPerImage: f64(0.04)}, true)

	ctx := newMediaEventContext(t, nil)
	event := collector.NewUsageEvent(ctx)

	assert.False(t, event.Media.HasUsage())
	assert.True(t, event.UsageMissing)
	assert.Zero(t, event.EstCostUSD)
}

func TestMediaCost_PointerIsSilentlyDropped(t *testing.T) {
	// 精确类型断言的负面用例：写成 *core.MediaCostInput 会被采集器**静默丢弃**，
	// 结果退化成「没有媒体用量」→ usage_missing。这条测试是给后来者的警示牌：
	// 必须写值类型（handler 的 applyImageMediaUsage 就是值类型）。
	collector := newMediaCollector(t, core.ModelPricing{InputPerImage: f64(0.04)}, true)

	ctx := newMediaEventContext(t, &core.MediaCostInput{Images: 2})
	event := collector.NewUsageEvent(ctx)

	assert.False(t, event.Media.HasUsage(), "指针不会被采集器识别（精确类型断言）")
	assert.True(t, event.UsageMissing, "被静默丢弃后必须退化为 usage_missing，而不是 0 元")
	assert.Zero(t, event.EstCostUSD)
}

func TestMediaCost_TokenFallbackNotUsedForImageOnlyPricing(t *testing.T) {
	// 图像请求没有 token 用量：即使价表同时有 token 价，媒体口径无价时也不能回退 token 口径
	// （回退会算出 0 元并掩盖「图没有单价」这个事实）。
	price := core.ModelPricing{InputPerMtok: f64(0.5), OutputPerMtok: f64(1.5)}
	collector := newMediaCollector(t, price, true)

	ctx := newMediaEventContext(t, core.MediaCostInput{Images: 1})
	event := collector.NewUsageEvent(ctx)

	assert.True(t, event.UsageMissing, "无图像单价且无 token 用量 → usage_missing")
	assert.Zero(t, event.EstCostUSD)
}

func TestMediaCost_BucketSemantics(t *testing.T) {
	// 桶口径文档化：Images 桶优先 per_image，缺失才回退 input_cost_per_image；
	// 而 InputImages 桶**只用** InputPerImage，不做反向回退（避免重复计费）。
	t.Run("Images 优先 per_image", func(t *testing.T) {
		price := core.ModelPricing{PerImage: f64(0.05), InputPerImage: f64(0.04)}
		collector := newMediaCollector(t, price, true)
		event := collector.NewUsageEvent(newMediaEventContext(t, core.MediaCostInput{Images: 1}))
		assert.InDelta(t, 0.05, event.EstCostUSD, 1e-9)
	})

	t.Run("Images 回退 input_cost_per_image", func(t *testing.T) {
		price := core.ModelPricing{InputPerImage: f64(0.04)}
		collector := newMediaCollector(t, price, true)
		event := collector.NewUsageEvent(newMediaEventContext(t, core.MediaCostInput{Images: 1}))
		assert.InDelta(t, 0.04, event.EstCostUSD, 1e-9)
	})

	t.Run("InputImages 不回退 per_image", func(t *testing.T) {
		price := core.ModelPricing{PerImage: f64(0.05)}
		collector := newMediaCollector(t, price, true)
		event := collector.NewUsageEvent(newMediaEventContext(t, core.MediaCostInput{InputImages: 1}))
		assert.True(t, event.UsageMissing, "InputImages 桶缺 InputPerImage 就是无价，不回退 per_image")
		assert.Zero(t, event.EstCostUSD)
	})
}

func TestMediaCost_HandlerOutputIsAcceptedByCollector(t *testing.T) {
	// 跨包闭环：handler.ImageMediaCostInput 的产物必须能被采集器原样接受。
	// （两个包各自单测通过、但类型/桶名对不上时，这里会挂。）
	perImage := 0.04
	collector := newMediaCollector(t, core.ModelPricing{PerImage: &perImage, PerRequest: f64(0.01)}, true)

	resp := &core.ImageGenerationResponse{
		Raw:  []byte(`{"created":1,"data":[{"b64_json":"QQ=="},{"b64_json":"Qg=="}]}`),
		Data: []core.ImageData{{B64JSON: "QQ=="}, {B64JSON: "Qg=="}},
		Usage: &core.ImageUsage{
			InputTokens: 10, OutputTokens: 20, TotalTokens: 30,
			InputTokensDetails: &core.ImageInputTokensDetails{ImageTokens: 8, TextTokens: 2},
		},
	}
	media := handler.ImageMediaCostInput(resp)
	require.True(t, media.HasUsage())

	ctx := newMediaEventContext(t, media)
	event := collector.NewUsageEvent(ctx)

	// 带 token usage 时走 token 口径（价表未给 image token 价）→ 不会用 per_image。
	// 这说明：一旦上游给了 usage，就必须配 InputPerImageToken/OutputPerImageToken
	// 才能算钱，否则是 usage_missing（宁缺毋滥）。
	assert.True(t, event.UsageMissing)
	assert.Zero(t, event.EstCostUSD)
}

func TestMediaCost_HandlerFallbackBucketsArePriced(t *testing.T) {
	// 无 usage 的退化口径（Images=len(data), Requests=1）在有 per_image + per_request 时能算出钱。
	perImage := 0.04
	collector := newMediaCollector(t, core.ModelPricing{PerImage: &perImage, PerRequest: f64(0.01)}, true)

	resp := &core.ImageGenerationResponse{
		Raw:  []byte(`{"created":1,"data":[{"b64_json":"QQ=="},{"b64_json":"Qg=="}]}`),
		Data: []core.ImageData{{B64JSON: "QQ=="}, {B64JSON: "Qg=="}},
	}
	media := handler.ImageMediaCostInput(resp)
	assert.Equal(t, core.MediaCostInput{Images: 2, Requests: 1}, media)

	event := collector.NewUsageEvent(newMediaEventContext(t, media))
	assert.False(t, event.UsageMissing)
	assert.InDelta(t, 0.09, event.EstCostUSD, 1e-9, "2 × 0.04 + 1 × 0.01")
}
