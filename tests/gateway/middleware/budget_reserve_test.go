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
	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/example/llm-gateway/internal/gateway/middleware"
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func newGinCtx() *gin.Context {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	return c
}

func startMiniredis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	s, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(s.Close)
	return redis.NewClient(&redis.Options{Addr: s.Addr()}), s
}

func currentMonthPeriod() string {
	parts := strings.Split(time.Now().Format("2006-01-02"), "-")
	return parts[0] + "-" + parts[1]
}

func currentDayPeriod() string {
	return time.Now().Format("2006-01-02")
}

func currentWeekPeriod() string {
	year, week := time.Now().ISOWeek()
	return strconv.Itoa(year) + "-W" + weekStr(week)
}

func weekStr(w int) string {
	if w < 10 {
		return "0" + strconv.Itoa(w)
	}
	return strconv.Itoa(w)
}

func setUsed(rdb *redis.Client, scopeType, scopeID, period string, used float64) {
	key := "budget:used:" + scopeType + ":" + scopeID + ":" + period
	rdb.Set(context.Background(), key, used, 0)
}

func TestCheckLegacyBudget_ReturnsStructuredTotalConstraint(t *testing.T) {
	rdb, _ := startMiniredis(t)
	constraint := budget.Constraint{
		Dimension: budget.DimensionTotal, ScopeType: "member", ScopeID: "u-1",
		Label: "成员", PeriodType: budgetdomain.PeriodMonthly, Period: "2026-07",
		LimitMicroUSD: 1_000_000,
	}
	constraint.Key = budget.TotalUsedKey(constraint.ScopeType, constraint.ScopeID, constraint.Period)
	plan := &budget.AccountingPlan{
		Constraints:     []budget.Constraint{constraint},
		LegacyKeys:      []string{middleware.BudgetUsedKey("member", "u-1", "2026-07")},
		LegacyScopeArgs: []any{1.0, -1.0, -1.0, "成员"},
	}
	require.NoError(t, rdb.Set(context.Background(), plan.LegacyKeys[0], "1", 0).Err())

	blocked, err := middleware.CheckLegacyBudget(context.Background(), rdb, plan)
	require.NoError(t, err)
	require.NotNil(t, blocked)
	require.Equal(t, "u-1", blocked.ScopeID)
	require.Equal(t, budget.DimensionTotal, blocked.Dimension)
}

func getUsed(mr *miniredis.Miniredis, scopeType, scopeID, period string) float64 {
	v, _ := mr.Get("budget:used:" + scopeType + ":" + scopeID + ":" + period)
	f, _ := strconv.ParseFloat(v, 64)
	return f
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: Reserve — 跳过场景
// ─────────────────────────────────────────────────────────────────────────────

func TestReserve_NilDeps(t *testing.T) {
	result, err := middleware.Reserve(newGinCtx(), nil)
	assert.Nil(t, result)
	assert.NoError(t, err)
}

func TestReserve_NilRDB(t *testing.T) {
	c := newGinCtx()
	c.Set("identity", &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{
		{ScopeType: "studio", ScopeID: "s-1", AmountUSD: 100, WeeklyAmountUSD: -1, DailyAmountUSD: -1, Label: "s1"},
	}})
	result, err := middleware.Reserve(c, &middleware.BudgetDeps{RDB: nil})
	assert.Nil(t, result)
	assert.NoError(t, err)
}

func TestReserve_NoIdentity(t *testing.T) {
	rdb, _ := startMiniredis(t)
	result, err := middleware.Reserve(newGinCtx(), &middleware.BudgetDeps{RDB: rdb})
	assert.Nil(t, result)
	assert.NoError(t, err)
}

func TestReserve_EmptyBudgetChain(t *testing.T) {
	rdb, _ := startMiniredis(t)
	c := newGinCtx()
	c.Set("identity", &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{}})
	result, err := middleware.Reserve(c, &middleware.BudgetDeps{RDB: rdb})
	assert.Nil(t, result)
	assert.NoError(t, err)
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: Reserve — 通过场景
// ─────────────────────────────────────────────────────────────────────────────

// TestReserve_Success 预算充足时检查通过，ctx 设置 budget_keys 和 budget_scope_args。
func TestReserve_Success(t *testing.T) {
	rdb, _ := startMiniredis(t)
	c := newGinCtx()
	c.Set("identity", &gwmodel.UserIdentity{
		KeyID: "key-001",
		BudgetChain: []gwmodel.BudgetScope{
			{ScopeType: "dept_daily", ScopeID: "dept-1", AmountUSD: 500, WeeklyAmountUSD: -1, DailyAmountUSD: -1, Label: "研发部"},
			{ScopeType: "studio", ScopeID: "s-1", AmountUSD: 2000, WeeklyAmountUSD: -1, DailyAmountUSD: -1, Label: "工作室"},
		},
	})

	result, err := middleware.Reserve(c, &middleware.BudgetDeps{RDB: rdb})

	require.NoError(t, err)
	require.NotNil(t, result)
	// 2 scope × 3 维度 = 6 keys
	assert.Len(t, result.Keys, 6)

	// ctx 已设置
	keys, ok := c.Get("budget_keys")
	assert.True(t, ok)
	assert.Equal(t, result.Keys, keys)

	args, ok := c.Get("budget_scope_args")
	assert.True(t, ok)
	assert.NotEmpty(t, args)
}

// TestReserve_UnlimitedScope 全维度 -1（不限额）时始终通过。
func TestReserve_UnlimitedScope(t *testing.T) {
	rdb, _ := startMiniredis(t)
	c := newGinCtx()
	c.Set("identity", &gwmodel.UserIdentity{
		BudgetChain: []gwmodel.BudgetScope{
			{ScopeType: "studio", ScopeID: "s-1", AmountUSD: -1, WeeklyAmountUSD: -1, DailyAmountUSD: -1, Label: "工作室"},
		},
	})
	result, err := middleware.Reserve(c, &middleware.BudgetDeps{RDB: rdb})
	require.NoError(t, err)
	require.NotNil(t, result)
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: Reserve — 超额场景（v0.9：used >= limit 即拒绝）
// ─────────────────────────────────────────────────────────────────────────────

// TestReserve_Exceeded_MonthlyLimit 月累计已达上限时拒绝。
func TestReserve_Exceeded_MonthlyLimit(t *testing.T) {
	rdb, _ := startMiniredis(t)
	period := currentMonthPeriod()
	// 预设已用 = 500（等于月限额），应拒绝
	setUsed(rdb, "dept_daily", "dept-1", period, 500)

	c := newGinCtx()
	c.Set("identity", &gwmodel.UserIdentity{
		BudgetChain: []gwmodel.BudgetScope{
			{ScopeType: "dept_daily", ScopeID: "dept-1", AmountUSD: 500, WeeklyAmountUSD: -1, DailyAmountUSD: -1, Label: "研发部"},
		},
	})
	result, err := middleware.Reserve(c, &middleware.BudgetDeps{RDB: rdb})

	assert.Nil(t, result)
	require.Error(t, err)
	beErr, ok := err.(*middleware.BudgetExceededError)
	require.True(t, ok)
	assert.Contains(t, beErr.Label, "研发部")
	assert.Contains(t, beErr.Label, "月限额")
}

// TestReserve_Exceeded_DailyLimit 日累计已达上限时拒绝。
func TestReserve_Exceeded_DailyLimit(t *testing.T) {
	rdb, _ := startMiniredis(t)
	day := currentDayPeriod()
	setUsed(rdb, "dept_daily", "dept-1", day, 50)

	c := newGinCtx()
	c.Set("identity", &gwmodel.UserIdentity{
		BudgetChain: []gwmodel.BudgetScope{
			{ScopeType: "dept_daily", ScopeID: "dept-1", AmountUSD: -1, WeeklyAmountUSD: -1, DailyAmountUSD: 50, Label: "研发部"},
		},
	})
	result, err := middleware.Reserve(c, &middleware.BudgetDeps{RDB: rdb})

	assert.Nil(t, result)
	require.Error(t, err)
	beErr, ok := err.(*middleware.BudgetExceededError)
	require.True(t, ok)
	assert.Contains(t, beErr.Label, "日限额")
}

// TestReserve_Exceeded_WeeklyLimit 周累计已达上限时拒绝。
func TestReserve_Exceeded_WeeklyLimit(t *testing.T) {
	rdb, _ := startMiniredis(t)
	year, week := time.Now().ISOWeek()
	weekPeriod := strconv.Itoa(year) + "-W" + weekStr(week)
	setUsed(rdb, "studio", "s-1", weekPeriod, 200)

	c := newGinCtx()
	c.Set("identity", &gwmodel.UserIdentity{
		BudgetChain: []gwmodel.BudgetScope{
			{ScopeType: "studio", ScopeID: "s-1", AmountUSD: -1, WeeklyAmountUSD: 200, DailyAmountUSD: -1, Label: "工作室"},
		},
	})
	result, err := middleware.Reserve(c, &middleware.BudgetDeps{RDB: rdb})

	assert.Nil(t, result)
	require.Error(t, err)
	beErr, ok := err.(*middleware.BudgetExceededError)
	require.True(t, ok)
	assert.Contains(t, beErr.Label, "周限额")
}

// TestReserve_Exceeded_SecondLayer 第一层充足、第二层超限时报告第二层。
func TestReserve_Exceeded_SecondLayer(t *testing.T) {
	rdb, _ := startMiniredis(t)
	period := currentMonthPeriod()
	setUsed(rdb, "studio", "s-1", period, 5000) // 第二层超限

	c := newGinCtx()
	c.Set("identity", &gwmodel.UserIdentity{
		BudgetChain: []gwmodel.BudgetScope{
			{ScopeType: "dept_daily", ScopeID: "dept-1", AmountUSD: 500, WeeklyAmountUSD: -1, DailyAmountUSD: -1, Label: "研发部"},
			{ScopeType: "studio", ScopeID: "s-1", AmountUSD: 5000, WeeklyAmountUSD: -1, DailyAmountUSD: -1, Label: "工作室"},
		},
	})
	result, err := middleware.Reserve(c, &middleware.BudgetDeps{RDB: rdb})

	assert.Nil(t, result)
	require.Error(t, err)
	beErr, ok := err.(*middleware.BudgetExceededError)
	require.True(t, ok)
	assert.Contains(t, beErr.Label, "工作室", "应报告 studio 层超额")
}

// TestReserve_BelowLimit_Passes 已用接近但未达上限时通过。
func TestReserve_BelowLimit_Passes(t *testing.T) {
	rdb, _ := startMiniredis(t)
	period := currentMonthPeriod()
	setUsed(rdb, "dept_daily", "dept-1", period, 499.99)

	c := newGinCtx()
	c.Set("identity", &gwmodel.UserIdentity{
		BudgetChain: []gwmodel.BudgetScope{
			{ScopeType: "dept_daily", ScopeID: "dept-1", AmountUSD: 500, WeeklyAmountUSD: -1, DailyAmountUSD: -1, Label: "研发部"},
		},
	})
	result, err := middleware.Reserve(c, &middleware.BudgetDeps{RDB: rdb})

	require.NoError(t, err)
	require.NotNil(t, result, "499.99 < 500，应通过")
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: Reserve — fail-open
// ─────────────────────────────────────────────────────────────────────────────

func TestReserve_RedisFailure_FailOpen(t *testing.T) {
	rdb, mr := startMiniredis(t)
	mr.Close() // 模拟 Redis 不可用

	c := newGinCtx()
	c.Set("identity", &gwmodel.UserIdentity{
		BudgetChain: []gwmodel.BudgetScope{
			{ScopeType: "studio", ScopeID: "s-1", AmountUSD: 100, WeeklyAmountUSD: -1, DailyAmountUSD: -1, Label: "工作室"},
		},
	})
	result, err := middleware.Reserve(c, &middleware.BudgetDeps{RDB: rdb})

	assert.Nil(t, result)
	assert.NoError(t, err, "Redis 故障应 fail-open，不返回错误")

	skipped, ok := c.Get("budget_skipped")
	assert.True(t, ok)
	assert.Equal(t, true, skipped)
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: BudgetAdd — 事后累加
// ─────────────────────────────────────────────────────────────────────────────

// TestBudgetAdd_Success 验证 BudgetAdd 正确累加真实成本到月/周/日计数器，并设 TTL。
func TestBudgetAdd_Success(t *testing.T) {
	rdb, mr := startMiniredis(t)
	period := currentMonthPeriod()
	day := currentDayPeriod()
	year, week := time.Now().ISOWeek()
	weekPeriod := strconv.Itoa(year) + "-W" + weekStr(week)

	keys := []string{
		"budget:used:dept_daily:dept-1:" + period,
		"budget:used:dept_daily:dept-1:" + weekPeriod,
		"budget:used:dept_daily:dept-1:" + day,
	}
	// scopeArgs: [monthly_amount, weekly_amount, daily_amount, label]
	scopeArgs := []interface{}{500.0, -1.0, 50.0, "研发部"}

	middleware.BudgetAdd(context.Background(), rdb, keys, scopeArgs, 0.15)

	// 月 key 应累加（monthly_amount=500 >= 0）
	monthUsed := getUsed(mr, "dept_daily", "dept-1", period)
	assert.InDelta(t, 0.15, monthUsed, 0.0001)

	// 周 key 不应累加（weekly_amount=-1 < 0）
	weekUsed := getUsed(mr, "dept_daily", "dept-1", weekPeriod)
	assert.InDelta(t, 0.0, weekUsed, 0.0001, "weekly_amount=-1，不应累加")

	// 日 key 应累加（daily_amount=50 >= 0）
	dayUsed := getUsed(mr, "dept_daily", "dept-1", day)
	assert.InDelta(t, 0.15, dayUsed, 0.0001)
}

// TestBudgetAdd_Accumulates 多次调用累加。
func TestBudgetAdd_Accumulates(t *testing.T) {
	rdb, mr := startMiniredis(t)
	period := currentMonthPeriod()
	day := currentDayPeriod()
	year, week := time.Now().ISOWeek()
	weekPeriod := strconv.Itoa(year) + "-W" + weekStr(week)

	keys := []string{
		"budget:used:studio:s-1:" + period,
		"budget:used:studio:s-1:" + weekPeriod,
		"budget:used:studio:s-1:" + day,
	}
	scopeArgs := []interface{}{2000.0, 500.0, 100.0, "工作室"}

	middleware.BudgetAdd(context.Background(), rdb, keys, scopeArgs, 1.0)
	middleware.BudgetAdd(context.Background(), rdb, keys, scopeArgs, 2.5)
	middleware.BudgetAdd(context.Background(), rdb, keys, scopeArgs, 0.3)

	monthUsed := getUsed(mr, "studio", "s-1", period)
	assert.InDelta(t, 3.8, monthUsed, 0.0001, "三次累加应为 3.8")
}

// TestBudgetAdd_ZeroCost_NoOp 成本为 0 时不操作 Redis。
func TestBudgetAdd_ZeroCost_NoOp(t *testing.T) {
	rdb, mr := startMiniredis(t)
	period := currentMonthPeriod()
	day := currentDayPeriod()
	year, week := time.Now().ISOWeek()
	weekPeriod := strconv.Itoa(year) + "-W" + weekStr(week)

	keys := []string{
		"budget:used:studio:s-1:" + period,
		"budget:used:studio:s-1:" + weekPeriod,
		"budget:used:studio:s-1:" + day,
	}
	middleware.BudgetAdd(context.Background(), rdb, keys, []interface{}{1000.0, -1.0, -1.0, "工作室"}, 0)

	v, _ := mr.Get("budget:used:studio:s-1:" + period)
	assert.Empty(t, v, "cost=0 时不应写 Redis")
}

// TestBudgetAdd_NilRDB_Safe nil Redis 不 panic。
func TestBudgetAdd_NilRDB_Safe(t *testing.T) {
	assert.NotPanics(t, func() {
		middleware.BudgetAdd(context.Background(), nil, []string{"k"}, nil, 0.5)
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: Reserve + BudgetAdd 端到端
// ─────────────────────────────────────────────────────────────────────────────

// TestReserve_ThenAdd_CorrectFlow 验证完整流程：检查通过 → 请求成功 → 累加真实成本。
func TestReserve_ThenAdd_CorrectFlow(t *testing.T) {
	rdb, mr := startMiniredis(t)
	period := currentMonthPeriod()
	day := currentDayPeriod()
	year, week := time.Now().ISOWeek()
	weekPeriod := strconv.Itoa(year) + "-W" + weekStr(week)
	_ = weekPeriod

	c := newGinCtx()
	c.Set("identity", &gwmodel.UserIdentity{
		BudgetChain: []gwmodel.BudgetScope{
			{ScopeType: "dept_daily", ScopeID: "dept-1", AmountUSD: 500, WeeklyAmountUSD: -1, DailyAmountUSD: 50, Label: "研发部"},
		},
	})

	// 1. 检查（已用 0，未超限）
	result, err := middleware.Reserve(c, &middleware.BudgetDeps{RDB: rdb})
	require.NoError(t, err)
	require.NotNil(t, result)

	// 2. 请求成功，累加真实成本 $0.12
	argsVal, _ := c.Get(middleware.CtxKeyBudgetScopeArgs)
	scopeArgs := argsVal.([]interface{})
	middleware.BudgetAdd(context.Background(), rdb, result.Keys, scopeArgs, 0.12)

	// 3. 验证月/日 counter 均为 0.12
	assert.InDelta(t, 0.12, getUsed(mr, "dept_daily", "dept-1", period), 0.0001)
	assert.InDelta(t, 0.12, getUsed(mr, "dept_daily", "dept-1", day), 0.0001)
}

// TestReserve_ConcurrentWindow 验证并发窗口：两个请求同时通过检查，
// 各自累加后总量可能超限（软超限，对内部场景可接受）。
func TestReserve_ConcurrentWindow(t *testing.T) {
	rdb, mr := startMiniredis(t)
	period := currentMonthPeriod()
	// 初始已用 490，限额 500，单次成本 8，两个并发请求各自看到 490 < 500 → 都通过
	setUsed(rdb, "studio", "s-1", period, 490)

	identity := &gwmodel.UserIdentity{
		BudgetChain: []gwmodel.BudgetScope{
			{ScopeType: "studio", ScopeID: "s-1", AmountUSD: 500, WeeklyAmountUSD: -1, DailyAmountUSD: -1, Label: "工作室"},
		},
	}

	c1 := newGinCtx()
	c1.Set("identity", identity)
	c2 := newGinCtx()
	c2.Set("identity", identity)

	deps := &middleware.BudgetDeps{RDB: rdb}
	r1, err1 := middleware.Reserve(c1, deps)
	r2, err2 := middleware.Reserve(c2, deps)

	// 两个请求都通过（490 < 500）
	require.NoError(t, err1)
	require.NoError(t, err2)
	require.NotNil(t, r1)
	require.NotNil(t, r2)

	// 累加各自真实成本（8+8=16）
	args := []interface{}{500.0, -1.0, -1.0, "工作室"}
	day := currentDayPeriod()
	year, week := time.Now().ISOWeek()
	weekPeriod := strconv.Itoa(year) + "-W" + weekStr(week)
	keys := []string{
		"budget:used:studio:s-1:" + period,
		"budget:used:studio:s-1:" + weekPeriod,
		"budget:used:studio:s-1:" + day,
	}
	middleware.BudgetAdd(context.Background(), rdb, keys, args, 8)
	middleware.BudgetAdd(context.Background(), rdb, keys, args, 8)

	// 最终用量 506 > 500（软超限）— 这是 v0.9 已知的并发窗口，对内部场景可接受
	finalUsed := getUsed(mr, "studio", "s-1", period)
	assert.InDelta(t, 506, finalUsed, 0.001, "并发窗口导致软超限 506 > 500")
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: BudgetExceededError
// ─────────────────────────────────────────────────────────────────────────────

func TestBudgetExceededError_ToJSON(t *testing.T) {
	e := &middleware.BudgetExceededError{
		Message: "预算已用尽：研发部，请联系负责人扩容",
		Label:   "研发部 [月限额]",
	}
	j := e.ToJSON()
	errObj, ok := j["error"].(gin.H)
	require.True(t, ok)
	assert.Equal(t, "预算已用尽：研发部，请联系负责人扩容", errObj["message"])
	assert.Equal(t, "budget_exceeded", errObj["type"])
	assert.Equal(t, "budget_exceeded", errObj["code"])
	assert.Equal(t, "研发部 [月限额]", errObj["param"])
	assert.Equal(t, "budget", errObj["gate"])
}

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
