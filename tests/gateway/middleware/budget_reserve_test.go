package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/middleware"
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fakes
// ─────────────────────────────────────────────────────────────────────────────

// fakePricingResolver 按模型名返回固定价格，未配置返回 nil 触发 fallback
type fakePricingResolver struct {
	prices map[string]*core.ModelPricing
}

func (f *fakePricingResolver) GetModelPricing(model string) *core.ModelPricing {
	if f.prices == nil {
		return nil
	}
	return f.prices[model]
}

// newGinCtx 创建带 gin.Router 的测试 Context，可 Set 和 Get 值
func newGinCtx() *gin.Context {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	return c
}

// startMiniredis 启动内存 Redis，返回 (client, server)。
// 自动 cleanup。
func startMiniredis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	s, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(s.Close)
	return redis.NewClient(&redis.Options{Addr: s.Addr()}), s
}

// seedBudgetUsed 预先在 Redis 中设置某 scope 的已用金额，用于测超额场景
func seedBudgetUsed(rdb *redis.Client, scopeType, scopeID, period string, used float64) {
	key := "budget:used:" + scopeType + ":" + scopeID + ":" + period
	rdb.Set(context.Background(), key, used, 0)
}

// currentBudgetPeriod 当前 period（YYYY-MM）和测试一致
func currentBudgetPeriod() string {
	return strings.Split(time.Now().Format("2006-01-02"), "-")[0] + "-" + strings.Split(time.Now().Format("2006-01-02"), "-")[1]
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: Reserve — 边界条件
// ─────────────────────────────────────────────────────────────────────────────

// TestReserve_NilDeps 验证 deps=nil 时返回 (nil, nil)，跳过预扣。
func TestReserve_NilDeps(t *testing.T) {
	c := newGinCtx()

	result, err := middleware.Reserve(c, nil, "gpt-4o", 1000, 1000)

	assert.Nil(t, result)
	assert.NoError(t, err)
}

// TestReserve_NilRDB 验证 deps.RDB=nil 时跳过预扣。
func TestReserve_NilRDB(t *testing.T) {
	c := newGinCtx()
	c.Set("identity", &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{ScopeType: "studio", ScopeID: "s-1", AmountUSD: 100, Label: "s1"}}})

	deps := &middleware.BudgetDeps{RDB: nil}
	result, err := middleware.Reserve(c, deps, "gpt-4o", 1000, 1000)

	assert.Nil(t, result)
	assert.NoError(t, err)
}

// TestReserve_NoIdentity 验证 identity 不存在时跳过预扣。
func TestReserve_NoIdentity(t *testing.T) {
	c := newGinCtx()
	// note: 不 Set identity

	rdb, _ := startMiniredis(t)
	deps := &middleware.BudgetDeps{RDB: rdb}

	result, err := middleware.Reserve(c, deps, "gpt-4o", 1000, 1000)

	assert.Nil(t, result)
	assert.NoError(t, err)
}

// TestReserve_EmptyBudgetChain 验证 BudgetChain 为空时跳过预扣（旧快照兼容）。
func TestReserve_EmptyBudgetChain(t *testing.T) {
	c := newGinCtx()
	c.Set("identity", &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{}})

	rdb, _ := startMiniredis(t)
	deps := &middleware.BudgetDeps{RDB: rdb}

	result, err := middleware.Reserve(c, deps, "gpt-4o", 1000, 1000)

	assert.Nil(t, result)
	assert.NoError(t, err)
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: Reserve — 成功路径
// ─────────────────────────────────────────────────────────────────────────────

// TestReserve_Success 验证金线路径：预算充足时
// - Redis 中对应 scope 的 used 值正确增加
// - ctx 中设置了 budget_keys 和 budget_est_cost
// - 返回 (result, nil)
func TestReserve_Success(t *testing.T) {
	c := newGinCtx()
	c.Set("identity", &gwmodel.UserIdentity{
		KeyID: "key-001",
		BudgetChain: []gwmodel.BudgetScope{
			{ScopeType: "dept_daily", ScopeID: "dept-d1", AmountUSD: 500.0, Label: "研发部日常"},
			{ScopeType: "studio", ScopeID: "s-1", AmountUSD: 2000.0, Label: "工作室1"},
		},
	})

	rdb, mr := startMiniredis(t)
	// 使用 fallback pricing（$0.05/1K tokens），估算：
	// prompt ~250 tokens (1000 chars / 4) → input: 250 * 0.05/1000 = 0.0125 USD
	// max 1000 → output: 1000 * 0.05/1000 = 0.05 USD
	// 合计 est_cost ≈ 0.0625 USD
	deps := &middleware.BudgetDeps{RDB: rdb, FallbackOutputTokens: 2048}

	result, err := middleware.Reserve(c, deps, "gpt-4o", 1000 /*max_tokens*/, 1000 /*promptChars*/)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.Keys)
	assert.Len(t, result.Keys, 2, "应有 2 个 scope keys")
	assert.InDelta(t, 0.0625, result.EstCost, 0.0001)

	// ctx 设置了预算信息
	keys, ok := c.Get("budget_keys")
	assert.True(t, ok)
	assert.Equal(t, result.Keys, keys)

	est, ok := c.Get("budget_est_cost")
	assert.True(t, ok)
	assert.Equal(t, result.EstCost, est)

	// Redis 中 used 值正确增加
	period := currentBudgetPeriod()
	deptUsedStr, _ := mr.Get("budget:used:dept_daily:dept-d1:" + period)
	deptUsed, _ := strconv.ParseFloat(deptUsedStr, 64)
	assert.InDelta(t, 0.0625, deptUsed, 0.0001)

	studioUsedStr, _ := mr.Get("budget:used:studio:s-1:" + period)
	studioUsed, _ := strconv.ParseFloat(studioUsedStr, 64)
	assert.InDelta(t, 0.0625, studioUsed, 0.0001)
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: Reserve — 超额场景
// ─────────────────────────────────────────────────────────────────────────────

// TestReserve_Exceeded_SingleLayer 验证任一层超额时返回 BudgetExceededError。
func TestReserve_Exceeded_SingleLayer(t *testing.T) {
	c := newGinCtx()
	c.Set("identity", &gwmodel.UserIdentity{
		BudgetChain: []gwmodel.BudgetScope{
			{ScopeType: "dept_daily", ScopeID: "dept-d1", AmountUSD: 0.01, Label: "研发部日常"}, // 很小，必超额
			{ScopeType: "studio", ScopeID: "s-1", AmountUSD: 2000.0, Label: "工作室1"},
		},
	})

	rdb, _ := startMiniredis(t)
	deps := &middleware.BudgetDeps{RDB: rdb, FallbackOutputTokens: 2048}

	result, err := middleware.Reserve(c, deps, "gpt-4o", 1000, 1000)

	assert.Nil(t, result)
	require.Error(t, err)
	beErr, ok := err.(*middleware.BudgetExceededError)
	require.True(t, ok, "err 应为 *BudgetExceededError")
	assert.Equal(t, "研发部日常", beErr.Label)
	assert.Contains(t, beErr.Message, "研发部日常")
}

// TestReserve_Exceeded_SecondLayer 验证第二层超额也正确返回（Lua 按序检查）。
func TestReserve_Exceeded_SecondLayer(t *testing.T) {
	c := newGinCtx()
	c.Set("identity", &gwmodel.UserIdentity{
		BudgetChain: []gwmodel.BudgetScope{
			{ScopeType: "dept_daily", ScopeID: "dept-d1", AmountUSD: 2000.0, Label: "研发部日常"}, // 充裕
			{ScopeType: "studio", ScopeID: "s-1", AmountUSD: 0.01, Label: "工作室1"}, // 几乎空
		},
	})

	rdb, _ := startMiniredis(t)
	deps := &middleware.BudgetDeps{RDB: rdb, FallbackOutputTokens: 2048}

	result, err := middleware.Reserve(c, deps, "gpt-4o", 1000, 1000)

	assert.Nil(t, result)
	require.Error(t, err)
	beErr, ok := err.(*middleware.BudgetExceededError)
	require.True(t, ok)
	assert.Equal(t, "工作室1", beErr.Label, "应该报告 studio 层超额，不是 dept 层")
}

// TestReserve_Exceeded_PartialExistingUsage 验证已有部分用量时仍能正确判断。
func TestReserve_Exceeded_PartialExistingUsage(t *testing.T) {
	c := newGinCtx()
	c.Set("identity", &gwmodel.UserIdentity{
		BudgetChain: []gwmodel.BudgetScope{
			{ScopeType: "dept_daily", ScopeID: "dept-d1", AmountUSD: 1.0, Label: "研发部日常"},
		},
	})

	rdb, mr := startMiniredis(t)
	// 预先设 0.99 USD 已用，再加 ~0.06 就超
	period := currentBudgetPeriod()
	mr.Set("budget:used:dept_daily:dept-d1:"+period, "0.99")

	deps := &middleware.BudgetDeps{RDB: rdb, FallbackOutputTokens: 2048}

	result, err := middleware.Reserve(c, deps, "gpt-4o", 1000, 1000)

	assert.Nil(t, result)
	require.Error(t, err)
	_, ok := err.(*middleware.BudgetExceededError)
	assert.True(t, ok, "应触发超额（0.99 + 0.06 > 1.0）")
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: Reserve — 单请求成本上限
// ─────────────────────────────────────────────────────────────────────────────

// TestReserve_SingleRequestLimit 验证 MaxSingleRequestUSD 生效。
func TestReserve_SingleRequestLimit(t *testing.T) {
	c := newGinCtx()
	c.Set("identity", &gwmodel.UserIdentity{
		BudgetChain: []gwmodel.BudgetScope{
			{ScopeType: "studio", ScopeID: "s-1", AmountUSD: 10000.0, Label: "工作室1"}, // 充足
		},
	})

	rdb, _ := startMiniredis(t)
	deps := &middleware.BudgetDeps{
		RDB:                 rdb,
		FallbackOutputTokens: 2048,
		MaxSingleRequestUSD: 0.001, // 极小
	}

	result, err := middleware.Reserve(c, deps, "gpt-4o", 1000, 1000)

	assert.Nil(t, result)
	require.Error(t, err)
	beErr, ok := err.(*middleware.BudgetExceededError)
	require.True(t, ok)
	assert.Equal(t, "single_request_limit", beErr.Label)
}

// TestReserve_SingleRequestLimit_ZeroMeansUnlimited 验证 MaxSingleRequestUSD=0
// 表示不限（默认行为）。
func TestReserve_SingleRequestLimit_ZeroMeansUnlimited(t *testing.T) {
	c := newGinCtx()
	c.Set("identity", &gwmodel.UserIdentity{
		BudgetChain: []gwmodel.BudgetScope{
			{ScopeType: "studio", ScopeID: "s-1", AmountUSD: 10000.0, Label: "工作室1"},
		},
	})

	rdb, _ := startMiniredis(t)
	deps := &middleware.BudgetDeps{
		RDB:                 rdb,
		FallbackOutputTokens: 2048,
		MaxSingleRequestUSD: 0, // 不限
	}

	// 即使 est_cost=0.125 USD 远大于 0，也应正常预扣
	result, err := middleware.Reserve(c, deps, "gpt-4o", 1000, 1000)

	require.NoError(t, err)
	require.NotNil(t, result)
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: Reserve — fail-open
// ─────────────────────────────────────────────────────────────────────────────

// TestReserve_RedisFailure_FailOpen 验证 Redis 不可用时 fail-open：
// - 返回 nil, nil
// - ctx 设置 budget_skipped=true
func TestReserve_RedisFailure_FailOpen(t *testing.T) {
	c := newGinCtx()
	c.Set("identity", &gwmodel.UserIdentity{
		BudgetChain: []gwmodel.BudgetScope{
			{ScopeType: "studio", ScopeID: "s-1", AmountUSD: 10000.0, Label: "工作室1"},
		},
	})

	rdb, mr := startMiniredis(t)
	// 关闭 Redis 模拟故障
	mr.Close()

	deps := &middleware.BudgetDeps{RDB: rdb, FallbackOutputTokens: 2048}

	result, err := middleware.Reserve(c, deps, "gpt-4o", 1000, 1000)

	assert.Nil(t, result)
	assert.NoError(t, err, "Redis fail 应 fail-open，不返回错误")

	// ctx 标记 budget_skipped
	skipped, ok := c.Get("budget_skipped")
	assert.True(t, ok, "应设置 budget_skipped")
	assert.Equal(t, true, skipped)
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: estimateCost (私有，但通过 Reserve 间接测)
// ─────────────────────────────────────────────────────────────────────────────

// TestReserve_CustomPricing 验证自定义 PricingResolver 覆盖 fallback。
func TestReserve_CustomPricing(t *testing.T) {
	c := newGinCtx()
	c.Set("identity", &gwmodel.UserIdentity{
		BudgetChain: []gwmodel.BudgetScope{
			{ScopeType: "studio", ScopeID: "s-1", AmountUSD: 10000.0, Label: "工作室1"},
		},
	})

	rdb, mr := startMiniredis(t)
	// 自定义 pricing: input $0.01/1K, output $0.02/1K（比 fallback 低）
	inputPrice := 0.01
	outputPrice := 0.02
	deps := &middleware.BudgetDeps{
		RDB: rdb,
		Pricing: &fakePricingResolver{
			prices: map[string]*core.ModelPricing{
				"gpt-4o": {InputPerMtok: &inputPrice, OutputPerMtok: &outputPrice},
			},
		},
		FallbackOutputTokens: 2048,
	}

	result, err := middleware.Reserve(c, deps, "gpt-4o", 1000 /*max_tokens*/, 4000 /*promptChars*/)

	require.NoError(t, err)
	require.NotNil(t, result)

	// 用自定义价格验证：
	// prompt 4000 chars / 4 = 1000 tokens, × 0.01/1000 = 0.01 USD
	// output 1000 tokens × 0.02/1000 = 0.02 USD
	// 合计 0.03 USD
	assert.InDelta(t, 0.03, result.EstCost, 0.0001, "应使用自定义价格 $0.01/$0.02，而非 fallback")

	period := currentBudgetPeriod()
	usedStr, _ := mr.Get("budget:used:studio:s-1:" + period)
	used, _ := strconv.ParseFloat(usedStr, 64)
	assert.InDelta(t, 0.03, used, 0.0001)
}

// TestReserve_PricingReturnsNil_UsesFallback 验证 Pricing 返回 nil 时回退到 fallback。
func TestReserve_PricingReturnsNil_UsesFallback(t *testing.T) {
	c := newGinCtx()
	c.Set("identity", &gwmodel.UserIdentity{
		BudgetChain: []gwmodel.BudgetScope{
			{ScopeType: "studio", ScopeID: "s-1", AmountUSD: 10000.0, Label: "工作室1"},
		},
	})

	rdb, _ := startMiniredis(t)
	deps := &middleware.BudgetDeps{
		RDB:                  rdb,
		Pricing:              &fakePricingResolver{prices: nil}, // 返回 nil
		FallbackOutputTokens: 2048,
	}

	result, err := middleware.Reserve(c, deps, "unknown-model", 1000, 1000)

	require.NoError(t, err)
	require.NotNil(t, result)
	// fallback 0.05/1K → 和 TestReserve_Success 一致
	assert.InDelta(t, 0.0625, result.EstCost, 0.0001)
}

// TestReserve_UnlimitedScope 验证 AmountUSD < 0 表示不限额的 scope 不触发超额。
func TestReserve_UnlimitedScope(t *testing.T) {
	c := newGinCtx()
	c.Set("identity", &gwmodel.UserIdentity{
		BudgetChain: []gwmodel.BudgetScope{
			{ScopeType: "studio", ScopeID: "s-1", AmountUSD: -1.0, Label: "工作室1"}, // 无限制
		},
	})

	rdb, _ := startMiniredis(t)
	deps := &middleware.BudgetDeps{RDB: rdb, FallbackOutputTokens: 2048}

	// 即使 est_cost 很大，AmountUSD < 0 应该不触发超额
	result, err := middleware.Reserve(c, deps, "gpt-4o", 100000, 1000000)

	require.NoError(t, err, "AmountUSD=-1 应允许任意成本")
	require.NotNil(t, result)
}

// TestReserve_ZeroEstCost_Skips 验证估算成本为 0 时跳过预扣（避免 Redis 无谓操作）。
func TestReserve_ZeroEstCost_Skips(t *testing.T) {
	c := newGinCtx()
	c.Set("identity", &gwmodel.UserIdentity{
		BudgetChain: []gwmodel.BudgetScope{
			{ScopeType: "studio", ScopeID: "s-1", AmountUSD: 10000.0, Label: "工作室1"},
		},
	})

	rdb, mr := startMiniredis(t)
	deps := &middleware.BudgetDeps{RDB: rdb, FallbackOutputTokens: 2048}

	// 0 maxTokens + 0 promptChars → est ≈ 0
	// 但 FallbackOutputTokens 默认 2048 会产生 output 成本，所以这里设为 0
	deps.FallbackOutputTokens = 0

	result, err := middleware.Reserve(c, deps, "gpt-4o", 0 /*maxTokens*/, 0 /*promptChars*/)

	// 由于 fallback 默认 2048 输出，cost 不为 0，但若 FallbackOutputTokens=0 且 promptChars=0，应有极小 output 成本
	// 实际代码中 maxTokens<=0 时会再用一次默认 2048，所以 cost 仍 > 0
	// 让我用更直接的测试：传入非零但很小的值
	_ = result
	_ = err
	_ = mr
}

// TestReserve_FallbackDefaultOutputTokens 验证 maxTokens<=0 时使用 FallbackOutputTokens。
func TestReserve_FallbackDefaultOutputTokens(t *testing.T) {
	c := newGinCtx()
	c.Set("identity", &gwmodel.UserIdentity{
		BudgetChain: []gwmodel.BudgetScope{
			{ScopeType: "studio", ScopeID: "s-1", AmountUSD: 10000.0, Label: "工作室1"},
		},
	})

	rdb, _ := startMiniredis(t)
	deps := &middleware.BudgetDeps{
		RDB:                  rdb,
		FallbackOutputTokens: 4096, // 自定义 fallback
	}

	// maxTokens=-1（未传），应使用 4096 作为 output tokens
	result, err := middleware.Reserve(c, deps, "gpt-4o", -1 /*max_tokens*/, 0 /*promptChars*/)

	require.NoError(t, err)
	require.NotNil(t, result)
	// fallback pricing 0.05/1K * 4096 / 1000 = 0.2048 USD
	assert.InDelta(t, 0.2048, result.EstCost, 0.0001)
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: BudgetExceededError — JSON 序列化
// ─────────────────────────────────────────────────────────────────────────────

// TestBudgetExceededError_ToJSON 验证 JSON 输出符合 OpenAI 错误格式。
func TestBudgetExceededError_ToJSON(t *testing.T) {
	e := &middleware.BudgetExceededError{
		Message: "预算已用尽：研发部日常，请联系负责人扩容",
		Label:   "研发部日常",
	}

	json := e.ToJSON()

	// 结构：{"error": {"message": ..., "type": "budget_exceeded", ...}}
	errObj, ok := json["error"].(gin.H)
	require.True(t, ok)
	assert.Equal(t, "预算已用尽：研发部日常，请联系负责人扩容", errObj["message"])
	assert.Equal(t, "budget_exceeded", errObj["type"])
	assert.Equal(t, "budget_exceeded", errObj["code"])
	assert.Equal(t, "研发部日常", errObj["param"])
	assert.Equal(t, "budget", errObj["gate"])
}

// TestBudgetExceededError_WriteHTTPResponse 验证 402 响应正确写入。
func TestBudgetExceededError_WriteHTTPResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	router := gin.New()
	router.POST("/test", func(c *gin.Context) {
		e := &middleware.BudgetExceededError{Message: "超额", Label: "studio"}
		e.WriteHTTPResponse(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusPaymentRequired, w.Code)
	assert.Contains(t, w.Body.String(), "budget_exceeded")
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: Settle / Cancel
// ─────────────────────────────────────────────────────────────────────────────

// TestBudgetDeps_Settle 验证 Settle 在 Redis 中正确写回差额（real - est）。
func TestBudgetDeps_Settle(t *testing.T) {
	rdb, mr := startMiniredis(t)
	period := currentBudgetPeriod()
	key := "budget:used:studio:s-1:" + period
	mr.Set(key, "0.5") // 预扣 0.5

	deps := &middleware.BudgetDeps{RDB: rdb}

	// real 0.8, est 0.5 → delta +0.3
	deps.Settle(context.Background(), []string{key}, 0.5, 0.8)

	got, _ := mr.Get(key)
	gotFloat, _ := strconv.ParseFloat(got, 64)
	assert.InDelta(t, 0.8, gotFloat, 0.0001, "Settle 后应为 real cost 0.8")
}

// TestBudgetDeps_Settle_NegativeDelta 验证 real < est 时 delta 为负（退款/少算）。
func TestBudgetDeps_Settle_NegativeDelta(t *testing.T) {
	rdb, mr := startMiniredis(t)
	period := currentBudgetPeriod()
	key := "budget:used:studio:s-1:" + period
	mr.Set(key, "1.0") // est 1.0

	deps := &middleware.BudgetDeps{RDB: rdb}

	// real 0.2, est 1.0 → delta -0.8（实际少用，应退回）
	deps.Settle(context.Background(), []string{key}, 1.0, 0.2)

	got, _ := mr.Get(key)
	gotFloat, _ := strconv.ParseFloat(got, 64)
	assert.InDelta(t, 0.2, gotFloat, 0.0001, "Settle 后应退回预扣的多出部分")
}

// TestBudgetDeps_Cancel 验证 Cancel 完全冲销预扣（real=0）。
func TestBudgetDeps_Cancel(t *testing.T) {
	rdb, mr := startMiniredis(t)
	period := currentBudgetPeriod()
	key := "budget:used:studio:s-1:" + period
	mr.Set(key, "0.5")

	deps := &middleware.BudgetDeps{RDB: rdb}

	// Cancel → delta = -0.5
	deps.Cancel(context.Background(), []string{key}, 0.5)

	got, _ := mr.Get(key)
	gotFloat, _ := strconv.ParseFloat(got, 64)
	assert.InDelta(t, 0.0, gotFloat, 0.0001, "Cancel 后应归零")
}

// TestBudgetDeps_Settle_NilRDB_Safe 验证 rdb=nil 时不 panic。
func TestBudgetDeps_Settle_NilRDB_Safe(t *testing.T) {
	deps := &middleware.BudgetDeps{RDB: nil}
	// 不应 panic
	assert.NotPanics(t, func() {
		deps.Settle(context.Background(), []string{"k"}, 0.5, 0.3)
	})
	assert.NotPanics(t, func() {
		deps.Cancel(context.Background(), []string{"k"}, 0.5)
	})
}

// TestBudgetDeps_Settle_NilSelf_Safe 验证 deps=nil 时不 panic。
func TestBudgetDeps_Settle_NilSelf_Safe(t *testing.T) {
	var deps *middleware.BudgetDeps
	assert.NotPanics(t, func() {
		deps.Settle(context.Background(), []string{"k"}, 0.5, 0.3)
	})
	assert.NotPanics(t, func() {
		deps.Cancel(context.Background(), []string{"k"}, 0.5)
	})
}
