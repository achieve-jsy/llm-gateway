package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/example/llm-gateway/internal/gateway/middleware"
	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
	obsrepo "github.com/example/llm-gateway/internal/gateway/observability/repository"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 本文件是 A2 的**端到端证据**：真实 HTTP 路由 + 真实 UsageCollector 中间件 +
// 真实 call_logs 落库（SQLite 承载列名/取值断言，列类型由 atlas-migrations 覆盖）。
//
// 覆盖两件事：
//  1. 闸门三态在整条链路上的可观测结果（状态码 / usage_missing / est_cost_usd）；
//  2. usage_collector.go 的「EstCostUSD > 0」门槛改前/改后行为对比
//     —— 断言无价请求**也能到达记账钩子**（改前会被金额门槛吞掉，hook 0 次）。

// hookRecorder 记录 postHook（预算核销钩子）的调用事实。
// fakePostHook 模拟 server/router.go 注入的 budget hook。
type hookRecorder struct {
	mu    sync.Mutex
	calls []hookCall
}

type hookCall struct {
	CostUSD float64
	Status  int
}

func (h *hookRecorder) hook() middleware.PostCostHook {
	return func(_ *gin.Context, realCostUSD float64, httpStatus int) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.calls = append(h.calls, hookCall{CostUSD: realCostUSD, Status: httpStatus})
	}
}

func (h *hookRecorder) snapshot() []hookCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]hookCall, len(h.calls))
	copy(out, h.calls)
	return out
}

// pipelineCollector 构造一个写 SQLite 的采集器，用于取回落库行。
// resolver 必须与闸门用同一份价表（生产环境就是同一个 provider registry 价），
// 否则会出现「准入判有价、记账算不出钱」的错配。
func pipelineCollector(t *testing.T, dbName string, resolver gwobs.PricingResolver) (*gwobs.Collector, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+dbName+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallLog{}))

	cfg := gwobs.DefaultCollectorConfig()
	cfg.BufferSize = 16
	cfg.FlushN = 1
	cfg.FlushInterval = 5 * time.Millisecond
	cfg.DBRetryCount = 1
	cfg.DBRetryBackoff = time.Millisecond

	collector := gwobs.NewCollector(cfg, resolver)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	collector.StartWriter(ctx, obsrepo.NewCallLogRepository(db))
	return collector, db
}

// waitPipelineCallLog 轮询等待异步批写落库。
func waitPipelineCallLog(t *testing.T, db *gorm.DB, requestID string) obsmodel.CallLog {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var row obsmodel.CallLog
		err := db.Where("request_id = ?", requestID).First(&row).Error
		if err == nil {
			return row
		}
		if time.Now().After(deadline) {
			t.Fatalf("call_logs 未在 5s 内写入 request_id=%s: %v", requestID, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestMediaPricingGate_PipelineThreeStates 三态在整条链路上的可观测结果。
//
// 表里每一行的期望值同时给出「改后」的断言；其中「开关关 + 无价」这一行的
// wantHookCalls=1 在改动前会失败（旧代码 `EstCostUSD > 0` 门槛把金额为 0 的请求吞掉了）。
func TestMediaPricingGate_PipelineThreeStates(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name            string
		description     string
		requirePricing  bool
		pricing         core.ModelPricing
		found           bool
		wantStatus      int
		wantErrCode     string
		wantUsageMiss   bool
		wantCost        float64
		wantInputCost   float64
		wantOutputCost  float64
		wantHookCalls   int
		wantHookCostUSD float64
	}{
		{
			name:            "有价 → 正常计价",
			description:     "dall-e-3 per_image=0.04 × 3 张 = 0.12；usage_missing=false，记账钩子拿到 0.12",
			requirePricing:  true,
			pricing:         core.ModelPricing{PerImage: f(0.04)},
			found:           true,
			wantStatus:      http.StatusOK,
			wantUsageMiss:   false,
			wantCost:        0.12,
			wantOutputCost:  0.12,
			wantHookCalls:   1,
			wantHookCostUSD: 0.12,
		},
		{
			name:           "默认（开关 true）+ 无价 → 402 media_pricing_missing",
			description:    "媒体价列为 NULL，闸门在 handler 里拒绝；请求以 402 落库、usage_missing=true、est_cost=0",
			requirePricing: true,
			pricing:        core.ModelPricing{},
			found:          true,
			wantStatus:     http.StatusPaymentRequired,
			wantErrCode:    "media_pricing_missing",
			wantUsageMiss:  true,
			wantCost:       0,
			wantHookCalls:  0, // 4xx 不记账（既有语义）
		},
		{
			name:            "开关 false + 无价 → 放行且 usage_missing=true",
			description:     "放行但埋点必须标 usage_missing=true；记账钩子必须被调用（金额 0）—— 改前这里会被 `EstCostUSD > 0` 门槛吞掉（hook 0 次）",
			requirePricing:  false,
			pricing:         core.ModelPricing{},
			found:           true,
			wantStatus:      http.StatusOK,
			wantUsageMiss:   true,
			wantCost:        0,
			wantHookCalls:   1,
			wantHookCostUSD: 0,
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, db := pipelineCollector(t, "media_gate_pipeline_"+strconv.Itoa(i), gateResolver(tt.pricing, tt.found))
			recorder := &hookRecorder{}
			requestID := "req-pipeline-" + strconv.Itoa(i)

			gate := middleware.NewMediaPricingGate(middleware.MediaPricingGateConfig{
				RequirePricing: tt.requirePricing,
				Resolver:       gateResolver(tt.pricing, tt.found),
			})

			router := gin.New()
			router.Use(middleware.UsageCollectorWithRedis(collector, nil, recorder.hook(), nil))
			router.POST("/v1/images/generations", func(c *gin.Context) {
				c.Set("request_id", requestID)
				c.Set("key_name", "gw-test-key")
				c.Set("model", "dall-e-3")
				c.Set("routed_model", "dall-e-3")
				c.Set("provider", "openai")
				// 模拟 handler：已过预算准入（拿到了 AccountingPlan）
				budget.SetAccountingPlan(c, &budget.AccountingPlan{Generation: "gen-1"})

				if err := gate.Require(c, "dall-e-3", "openai"); err != nil {
					var missingErr *middleware.MediaPricingMissingError
					if errors.As(err, &missingErr) {
						missingErr.WriteHTTPResponse(c)
					} else {
						c.AbortWithStatus(http.StatusInternalServerError)
					}
					return
				}
				// 有价 / 放行两条路径都登记媒体用量（真实链路由图像 handler 在终态写入）
				c.Set(gwobs.CtxKeyMediaUsage, core.MediaCostInput{Images: 3})
				c.Status(http.StatusOK)
			})

			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil))
			require.Equal(t, tt.wantStatus, w.Code)
			if tt.wantErrCode != "" {
				require.Contains(t, w.Body.String(), `"code":"`+tt.wantErrCode+`"`)
			}

			row := waitPipelineCallLog(t, db, requestID)
			assert.Equal(t, tt.wantStatus, row.Status)
			assert.Equal(t, tt.wantUsageMiss, row.UsageMissing, "usage_missing 必须能区分「无价」与「已计价」")
			assert.InDelta(t, tt.wantCost, row.EstCostUSD, 1e-9)
			assert.InDelta(t, tt.wantInputCost, row.InputCostUSD, 1e-9)
			assert.InDelta(t, tt.wantOutputCost, row.OutputCostUSD, 1e-9)

			calls := recorder.snapshot()
			require.Len(t, calls, tt.wantHookCalls,
				"记账钩子调用次数不符合预期（旧代码在金额为 0 时不会调用钩子）")
			if tt.wantHookCalls > 0 {
				assert.InDelta(t, tt.wantHookCostUSD, calls[0].CostUSD, 1e-9)
			}
		})
	}
}

// TestUsageCollector_AccountingGateNotBypassedOnUnpriced 直击 usage_collector.go 的门槛：
// 「有计价事实但金额为 0」的请求必须到达记账钩子，而不是被 `EstCostUSD > 0` 静默吞掉。
//
// 改前：hook 调用 0 次（门槛把请求挡在钩子之外）。
// 改后：hook 调用 1 次、金额 0（由钩子自身按 `realCostUSD <= 0` 短路，账目不受影响）。
func TestUsageCollector_AccountingGateNotBypassedOnUnpriced(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name         string
		description  string
		pricing      core.ModelPricing
		found        bool
		mediaUsage   *core.MediaCostInput
		wantBillable bool // 是否算「发生了计价事实」
	}{
		{
			name:         "媒体无价（字段为 NULL）+ 有媒体用量",
			description:  "采集器算出金额 0 且 usage_missing=true；这是不能静默丢弃的计费事实",
			pricing:      core.ModelPricing{PerSecondOutput: nil},
			found:        true,
			mediaUsage:   &core.MediaCostInput{OutputSeconds: 5},
			wantBillable: true,
		},
		{
			name:         "媒体有价 + 有媒体用量",
			description:  "正常计价路径，改前改后都必须调用钩子（回归）",
			pricing:      core.ModelPricing{PerSecondOutput: f(0.5)},
			found:        true,
			mediaUsage:   &core.MediaCostInput{OutputSeconds: 5},
			wantBillable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector := gwobs.NewCollector(gwobs.DefaultCollectorConfig(), gateResolver(tt.pricing, tt.found))
			recorder := &hookRecorder{}

			router := gin.New()
			router.Use(middleware.UsageCollectorWithRedis(collector, nil, recorder.hook(), nil))
			router.POST("/v1/videos", func(c *gin.Context) {
				c.Set("request_id", "req-gate-"+tt.name)
				c.Set("model", "kling-v1")
				c.Set("routed_model", "kling-v1")
				budget.SetAccountingPlan(c, &budget.AccountingPlan{Generation: "gen-2"})
				if tt.mediaUsage != nil {
					c.Set(gwobs.CtxKeyMediaUsage, *tt.mediaUsage)
				}
				c.Status(http.StatusOK)
			})

			router.ServeHTTP(httptest.NewRecorder(),
				httptest.NewRequest(http.MethodPost, "/v1/videos", nil))

			calls := recorder.snapshot()
			if tt.wantBillable {
				require.Len(t, calls, 1, "有计价事实的请求必须到达记账钩子（旧代码在此为 0 次）")
			}
		})
	}
}

// TestUsageCollector_TokenRequestsUnchanged 回归：token 链路（chat/embeddings）行为不变。
//
// 断言口径：
//   - 有价 token 请求：est_cost_usd / input_cost_usd / output_cost_usd / usage_missing
//     与改前逐字段一致，钩子拿到的金额与状态码一致（钩子调用次数也不变，因为 cost > 0）；
//   - 无价/无 usage 的 token 请求：**落库事实不变**（est_cost=0、usage_missing=true），
//     仅新增「到达钩子（金额 0）」这一内部行为，钩子按 router.go 的 `realCostUSD <= 0`
//     短路，不会给账目加 0 元条目。
func TestUsageCollector_TokenRequestsUnchanged(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		description    string
		path           string
		pricing        core.ModelPricing
		found          bool
		promptTokens   int
		completion     int
		wantUsageMiss  bool
		wantCost       float64
		wantInputCost  float64
		wantOutputCost float64
		wantHookCalls  int
	}{
		{
			name:        "chat 有价：逐字段与改前一致",
			description: "input=1/Mtok、output=2/Mtok，1000 prompt + 500 completion → 0.002；usage_missing=false；钩子恰好 1 次",
			path:        "/v1/chat/completions",
			pricing:     core.ModelPricing{InputPerMtok: f(1.0), OutputPerMtok: f(2.0)},
			found:       true,

			promptTokens:   1000,
			completion:     500,
			wantUsageMiss:  false,
			wantCost:       0.001 + 0.001,
			wantInputCost:  0.001,
			wantOutputCost: 0.001,
			wantHookCalls:  1,
		},
		{
			name:          "embeddings 无 usage 且无价：落库事实不变",
			description:   "上游不给 usage（usage_missing=true）且模型无价 → est_cost=0；埋点与改前一致，钩子被调用但金额为 0（会被生产钩子短路）",
			path:          "/v1/embeddings",
			pricing:       core.ModelPricing{},
			found:         true,
			wantUsageMiss: true,
			// 钩子调用次数在改后为 1（诚实记录：这是本次改动唯一的行为增量，
			// 但它对账目没有影响，见下方 productionHook 短路断言）。
			wantHookCalls: 1,
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, db := pipelineCollector(t, "token_regression_"+strconv.Itoa(i), gateResolver(tt.pricing, tt.found))
			recorder := &hookRecorder{}
			requestID := "req-token-" + strconv.Itoa(i)

			router := gin.New()
			router.Use(middleware.UsageCollectorWithRedis(collector, nil, recorder.hook(), nil))
			router.POST(tt.path, func(c *gin.Context) {
				c.Set("request_id", requestID)
				c.Set("model", "gpt-4o")
				c.Set("routed_model", "gpt-4o")
				c.Set("key_type", "personal")
				budget.SetAccountingPlan(c, &budget.AccountingPlan{Generation: "gen-3"})
				if tt.promptTokens > 0 {
					c.Set("prompt_tokens", tt.promptTokens)
					c.Set("completion_tokens", tt.completion)
				}
				c.Status(http.StatusOK)
			})

			router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, tt.path, nil))

			row := waitPipelineCallLog(t, db, requestID)
			assert.Equal(t, tt.wantUsageMiss, row.UsageMissing)
			assert.InDelta(t, tt.wantCost, row.EstCostUSD, 1e-9)
			assert.InDelta(t, tt.wantInputCost, row.InputCostUSD, 1e-9)
			assert.InDelta(t, tt.wantOutputCost, row.OutputCostUSD, 1e-9)
			assert.Equal(t, tt.promptTokens, row.PromptTokens)
			assert.Equal(t, tt.completion, row.CompletionTokens)

			require.Len(t, recorder.snapshot(), tt.wantHookCalls)
		})
	}
}

// TestUsageCollector_ZeroCostReachesProductionShapedHook 生产钩子形状：
// internal/gateway/server/router.go 的 buildBudgetPostHook 在 `realCostUSD <= 0` 时立即返回，
// 因此「金额为 0 的请求也调用钩子」不会给预算账目加任何 0 元条目
// （预算累计值不会因为本次改动而变化）。
func TestUsageCollector_ZeroCostReachesProductionShapedHook(t *testing.T) {
	gin.SetMode(gin.TestMode)

	type accountCall struct {
		costUSD float64
	}
	var (
		mu       sync.Mutex
		accounts []accountCall
	)

	// productionShapedHook 复刻 buildBudgetPostHook 的入口守卫（router.go:517）。
	productionShapedHook := func(_ *gin.Context, realCostUSD float64, httpStatus int) {
		if httpStatus >= 400 || realCostUSD <= 0 {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		accounts = append(accounts, accountCall{costUSD: realCostUSD})
	}

	collector := gwobs.NewCollector(gwobs.DefaultCollectorConfig(), gateResolver(core.ModelPricing{}, true))
	router := gin.New()
	router.Use(middleware.UsageCollectorWithRedis(collector, nil, productionShapedHook, nil))
	router.POST("/v1/videos", func(c *gin.Context) {
		c.Set("request_id", "req-zero-cost-hook")
		c.Set("model", "kling-v1")
		c.Set("routed_model", "kling-v1")
		budget.SetAccountingPlan(c, &budget.AccountingPlan{Generation: "gen-4"})
		c.Set(gwobs.CtxKeyMediaUsage, core.MediaCostInput{OutputSeconds: 5})
		c.Status(http.StatusOK)
	})
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/videos", nil))

	mu.Lock()
	defer mu.Unlock()
	require.Empty(t, accounts, "金额为 0 的请求不得写入预算账目（0 元不影响累计值）")
}
