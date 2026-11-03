package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/middleware"
	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// 本文件覆盖 A2「媒体准入计价闸门」的判定与对外契约：
//   - 三态：有价放行 / 无价 + 开关开 → 402 / 无价 + 开关关 → 放行且打 usage_missing 标记
//   - 判定矩阵：字段存在但 NULL 或 0 占位必须判无价（model_catalog 的刻意语义）
//   - 指标：gateway_media_pricing_decision_total{decision,provider,endpoint}（E 队告警口径）
//   - 价表不可用时的 fail-open（与 budget 的基础设施 fail-open 同范式）
//
// 端到端（HTTP + call_logs 落库）在 media_pricing_pipeline_test.go。

// f 是 float64 取指针的快捷函数（与 tests/gateway/observability 的写法一致）。
func f(v float64) *float64 { return &v }

// gateResolver 构造一个固定的 provider 感知价表；found=false 模拟「模型不在价表里」。
func gateResolver(p core.ModelPricing, found bool) gwobs.PricingResolver {
	return gwobs.ProviderPricingResolverFunc(func(string, string) (core.ModelPricing, bool) {
		return p, found
	})
}

// newGateContext 构造一个带 request_id 的 gin 上下文（模拟 handler 里调用闸门的现场）。
func newGateContext(method, path, requestID string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, path, nil)
	c.Set("request_id", requestID)
	return c, w
}

// TestMediaPricingGate_ThreeStates 闸门三态判定 + 402 错误体契约。
func TestMediaPricingGate_ThreeStates(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		description    string
		requirePricing bool
		pricing        core.ModelPricing
		found          bool
		wantRejected   bool
		wantMarked     bool
	}{
		{
			name:           "有价放行",
			description:    "模型配了 per_image=0.04（图像按张），开关默认 true，闸门应放行且不打无价标记",
			requirePricing: true,
			pricing:        core.ModelPricing{PerImage: f(0.04)},
			found:          true,
			wantRejected:   false,
			wantMarked:     false,
		},
		{
			name:           "无价 + 开关开 → 拒绝",
			description:    "媒体价列为 NULL（字段存在但无价），开关 true，闸门应拒绝并返回 402 media_pricing_missing",
			requirePricing: true,
			pricing:        core.ModelPricing{},
			found:          true,
			wantRejected:   true,
			wantMarked:     true,
		},
		{
			name:           "无价 + 开关关 → 放行但打标",
			description:    "同一无价模型，开关 false，闸门应放行并且写入 usage_missing 标记（绝不允许静默 0 元）",
			requirePricing: false,
			pricing:        core.ModelPricing{},
			found:          true,
			wantRejected:   false,
			wantMarked:     true,
		},
		{
			name:           "模型不在价表里 → 拒绝",
			description:    "价表查不到该模型（ok=false），等价于「该模型无媒体价」，开关 true 时应拒绝",
			requirePricing: true,
			pricing:        core.ModelPricing{},
			found:          false,
			wantRejected:   true,
			wantMarked:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gate := middleware.NewMediaPricingGate(middleware.MediaPricingGateConfig{
				RequirePricing: tt.requirePricing,
				Resolver:       gateResolver(tt.pricing, tt.found),
			})
			c, w := newGateContext(http.MethodPost, "/v1/images/generations", "req-"+tt.name)

			err := gate.Require(c, "dall-e-3", "openai")

			info, marked := middleware.MediaPricingMissingInfoFrom(c)
			if !tt.wantRejected {
				require.NoError(t, err)
				// 有价路径不得写无价标记（否则会把正常计价的调用误标 usage_missing）；
				// 无价放行路径必须写标记。
				require.Equal(t, tt.wantMarked, marked)
				return
			}

			// 拒绝路径：必须是 *MediaPricingMissingError + 402 + 契约里的 code
			var missingErr *middleware.MediaPricingMissingError
			require.True(t, errors.As(err, &missingErr), "应返回 *MediaPricingMissingError")
			missingErr.WriteHTTPResponse(c)

			require.Equal(t, http.StatusPaymentRequired, w.Code, "状态码必须是 402")
			require.Contains(t, w.Body.String(), `"code":"media_pricing_missing"`,
				"错误体 code 必须与 tests/contract/contracts.json 的 402 约定一致")
			require.Contains(t, w.Body.String(), `"gate":"media_pricing"`)
			// 拒绝也必须打标：采集侧据此把这次调用记成 usage_missing
			require.True(t, marked, "被拒绝的无价请求同样要打标，保证埋点可查")
			require.True(t, info.Rejected, "标记里必须记录「本次是被拒绝」")
			require.Equal(t, "dall-e-3", info.Model)
			require.Equal(t, "openai", info.Provider)
			require.Equal(t, "/v1/images/generations", info.Endpoint)
		})
	}
}

// TestMediaPricingGate_UnpricedMatrix 判定矩阵：只有正价才算「有价」。
func TestMediaPricingGate_UnpricedMatrix(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		description string
		pricing     core.ModelPricing
		found       bool
		wantPriced  bool
	}{
		{
			name:        "per_image 正价",
			description: "dall-e-3 式：input_cost_per_image=0.04 → 有价",
			pricing:     core.ModelPricing{InputPerImage: f(0.04)},
			found:       true,
			wantPriced:  true,
		},
		{
			name:        "per_second_output 正价（视频）",
			description: "Kling/Vidu 式按秒计价 → 有价",
			pricing:     core.ModelPricing{PerSecondOutput: f(0.5)},
			found:       true,
			wantPriced:  true,
		},
		{
			name:        "per_request 正价",
			description: "按次计费 → 有价",
			pricing:     core.ModelPricing{PerRequest: f(0.01)},
			found:       true,
			wantPriced:  true,
		},
		{
			name:        "图像 token 正价（gpt-image）",
			description: "input/output_cost_per_image_token → 有价",
			pricing:     core.ModelPricing{OutputPerImageToken: f(0.00004)},
			found:       true,
			wantPriced:  true,
		},
		{
			name:        "只有 token 价 → 仍算有价",
			description: "准入阶段拿不到真实用量时不能误拒：终态可回退 token 口径，因此正 token 价视为有价",
			pricing:     core.ModelPricing{InputPerMtok: f(5), OutputPerMtok: f(40)},
			found:       true,
			wantPriced:  true,
		},
		{
			name:        "字段存在但为 0 占位 → 无价",
			description: "catalog 里 per_image=0 是占位不是免费，必须判无价",
			pricing:     core.ModelPricing{PerImage: f(0)},
			found:       true,
			wantPriced:  false,
		},
		{
			name:        "全部媒体列 NULL → 无价",
			description: "A1 迁移把媒体价列默认设成 NULL 表示无价，必须被识别为无价",
			pricing:     core.ModelPricing{Currency: "USD"},
			found:       true,
			wantPriced:  false,
		},
		{
			name:        "负价 → 无价",
			description: "负价是脏数据，不能当成可计价",
			pricing:     core.ModelPricing{PerSecondOutput: f(-1)},
			found:       true,
			wantPriced:  false,
		},
		{
			name:        "价表查不到 → 无价",
			description: "ok=false（模型未配价/不在目录）等价无价",
			pricing:     core.ModelPricing{},
			found:       false,
			wantPriced:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gate := middleware.NewMediaPricingGate(middleware.MediaPricingGateConfig{
				RequirePricing: true,
				Resolver:       gateResolver(tt.pricing, tt.found),
			})
			c, _ := newGateContext(http.MethodPost, "/v1/videos", "req-matrix")
			err := gate.Require(c, "model-x", "kling")

			if tt.wantPriced {
				require.NoError(t, err)
				_, marked := middleware.MediaPricingMissingInfoFrom(c)
				require.False(t, marked)
				return
			}
			var missingErr *middleware.MediaPricingMissingError
			require.True(t, errors.As(err, &missingErr), "应判为无价并拒绝")
		})
	}
}

// TestMediaPricingGate_ResolverUnavailableFailsOpen 价表不可用时 fail-open（不是 fail-closed）。
//
// 理由：价表不可用属于基础设施故障，与「模型没有价」是两回事。fail-closed 会把
// 价表故障放大成媒体全链路 402，与 budget 的 AdmissionInfrastructureFailOpen 范式相反。
// 但必须**计数 + 强告警 + 打 usage_missing 标记**，不能悄悄放行。
func TestMediaPricingGate_ResolverUnavailableFailsOpen(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, tc := range []struct {
		name string
		gate *middleware.MediaPricingGate
	}{
		{"resolver 为 nil", middleware.NewMediaPricingGate(middleware.MediaPricingGateConfig{RequirePricing: true})},
		{"闸门为 nil", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newGateContext(http.MethodPost, "/v1/images/generations", "req-failopen")
			require.NoError(t, tc.gate.Require(c, "dall-e-3", "openai"), "价表不可用必须放行")

			info, marked := middleware.MediaPricingMissingInfoFrom(c)
			require.True(t, marked, "放行也必须打标，埋点侧按 usage_missing 处理")
			require.False(t, info.Rejected, "fail-open 不是拒绝")
		})
	}
}

// TestMediaPricingGate_DefaultGateWiring 进程级默认闸门（cmd/gateway 注入的那一个）。
func TestMediaPricingGate_DefaultGateWiring(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previous := middleware.DefaultMediaPricingGate()
	t.Cleanup(func() {
		if previous == nil {
			return
		}
		middleware.ConfigureMediaPricingGate(middleware.MediaPricingGateConfig{
			RequirePricing: previous.RequirePricing(),
		})
	})

	// 接线：无价 + 开关开 → RequireMediaPricing 返回 402 错误
	gate := middleware.ConfigureMediaPricingGate(middleware.MediaPricingGateConfig{
		RequirePricing: true,
		Resolver:       gateResolver(core.ModelPricing{}, true),
	})
	require.True(t, gate.RequirePricing(), "开关应为 true")

	c, _ := newGateContext(http.MethodPost, "/v1/images/generations", "req-default-gate")
	var missingErr *middleware.MediaPricingMissingError
	require.True(t, errors.As(middleware.RequireMediaPricing(c, "dall-e-3", "openai"), &missingErr))

	// 未接线：不得 panic，按基础设施不可用 fail-open
	middleware.ConfigureMediaPricingGate(middleware.MediaPricingGateConfig{RequirePricing: false})
	c2, _ := newGateContext(http.MethodPost, "/v1/videos", "req-unwired")
	require.NoError(t, middleware.RequireMediaPricing(c2, "kling-v1", "kling"))
}

// TestMediaPricingGate_MetricsDecisionTotal 决策计数（E 队告警口径）。
//
// 指标契约：gateway_media_pricing_decision_total{decision,provider,endpoint}
//
//	decision ∈ priced | rejected | admitted_unpriced | resolver_unavailable
//
// 刻意不带 model 标签：model 来自客户端请求体（用户可控），打标签会打爆指标基数。
func TestMediaPricingGate_MetricsDecisionTotal(t *testing.T) {
	gin.SetMode(gin.TestMode)

	oldMeter := otel.GetMeterProvider()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		otel.SetMeterProvider(oldMeter)
		_ = provider.Shutdown(context.Background())
	})

	// 四种决策各触发一次（priced / rejected / admitted_unpriced / resolver_unavailable）
	pricedGate := middleware.NewMediaPricingGate(middleware.MediaPricingGateConfig{
		RequirePricing: true, Resolver: gateResolver(core.ModelPricing{PerImage: f(0.04)}, true),
	})
	c1, _ := newGateContext(http.MethodPost, "/v1/images/generations", "m1")
	require.NoError(t, pricedGate.Require(c1, "dall-e-3", "openai"))

	unpricedGate := middleware.NewMediaPricingGate(middleware.MediaPricingGateConfig{
		RequirePricing: true, Resolver: gateResolver(core.ModelPricing{}, true),
	})
	c2, _ := newGateContext(http.MethodPost, "/v1/images/generations", "m2")
	require.Error(t, unpricedGate.Require(c2, "dall-e-3", "openai"))

	relaxedGate := middleware.NewMediaPricingGate(middleware.MediaPricingGateConfig{
		RequirePricing: false, Resolver: gateResolver(core.ModelPricing{}, true),
	})
	c3, _ := newGateContext(http.MethodPost, "/v1/videos", "m3")
	require.NoError(t, relaxedGate.Require(c3, "kling-v1", "kling"))

	brokenGate := middleware.NewMediaPricingGate(middleware.MediaPricingGateConfig{RequirePricing: true})
	c4, _ := newGateContext(http.MethodPost, "/v1/videos", "m4")
	require.NoError(t, brokenGate.Require(c4, "kling-v1", "kling"))

	var metrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &metrics))

	decisions := map[string]int64{}
	attrKeys := map[string]struct{}{}
	for _, scope := range metrics.ScopeMetrics {
		for _, item := range scope.Metrics {
			if item.Name != "gateway_media_pricing_decision_total" {
				continue
			}
			sum, ok := item.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, point := range sum.DataPoints {
				var decision string
				for _, attr := range point.Attributes.ToSlice() {
					attrKeys[string(attr.Key)] = struct{}{}
					if attr.Key == "decision" {
						decision = attr.Value.AsString()
					}
					require.NotEqual(t, "model", string(attr.Key), "不得带 model 标签（基数风险）")
				}
				decisions[decision] += point.Value
			}
		}
	}

	require.Equal(t, int64(1), decisions["priced"])
	require.Equal(t, int64(1), decisions["rejected"], "无价拒绝必须被计数")
	require.Equal(t, int64(1), decisions["admitted_unpriced"], "无价放行必须被计数")
	require.Equal(t, int64(1), decisions["resolver_unavailable"], "价表不可用必须被计数")
	require.Contains(t, attrKeys, "provider")
	require.Contains(t, attrKeys, "endpoint")
}
