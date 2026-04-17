package middleware_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/configsync"
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

func newMiddlewareGinCtx() *gin.Context {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	return c
}

func startMiddlewareMiniredis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	s, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(s.Close)
	return redis.NewClient(&redis.Options{Addr: s.Addr()}), s
}

func setMiddlewareUsed(rdb *redis.Client, scopeType, scopeID, period string, used float64) {
	key := "budget:used:" + scopeType + ":" + scopeID + ":" + period
	rdb.Set(context.Background(), key, used, 0)
}

func currentMiddlewareMonthPeriod() string {
	parts := strings.Split(time.Now().Format("2006-01-02"), "-")
	return parts[0] + "-" + parts[1]
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: Middleware passes request when budget is available
// ─────────────────────────────────────────────────────────────────────────────

// TestMiddleware_PassesRequestWhenBudgetAvailable verifies that the legacy
// Reserve middleware passes through when counters are under limits.
func TestMiddleware_PassesRequestWhenBudgetAvailable(t *testing.T) {
	rdb, _ := startMiddlewareMiniredis(t)
	c := newMiddlewareGinCtx()
	c.Set("identity", &gwmodel.UserIdentity{
		KeyID: "key-001",
		BudgetChain: []gwmodel.BudgetScope{
			{ScopeType: "dept", ScopeID: "dept-1", AmountUSD: 500, WeeklyAmountUSD: -1, DailyAmountUSD: -1, Label: "Engineering"},
		},
	})

	result, err := middleware.Reserve(c, &middleware.BudgetDeps{RDB: rdb})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Len(t, result.Keys, 3, "1 scope × 3 periods = 3 keys")
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: Middleware returns 402 when budget is exceeded
// ─────────────────────────────────────────────────────────────────────────────

// TestMiddleware_Returns402WhenBudgetExceeded verifies that when a monthly
// counter is at or above the limit, the Reserve function returns a
// BudgetExceededError that can write a 402 response.
func TestMiddleware_Returns402WhenBudgetExceeded(t *testing.T) {
	rdb, _ := startMiddlewareMiniredis(t)
	period := currentMiddlewareMonthPeriod()
	setMiddlewareUsed(rdb, "dept", "dept-1", period, 500)

	c := newMiddlewareGinCtx()
	c.Set("identity", &gwmodel.UserIdentity{
		BudgetChain: []gwmodel.BudgetScope{
			{ScopeType: "dept", ScopeID: "dept-1", AmountUSD: 500, WeeklyAmountUSD: -1, DailyAmountUSD: -1, Label: "Engineering"},
		},
	})

	result, err := middleware.Reserve(c, &middleware.BudgetDeps{RDB: rdb})
	assert.Nil(t, result)
	require.Error(t, err)

	beErr, ok := err.(*middleware.BudgetExceededError)
	require.True(t, ok, "error should be BudgetExceededError")
	assert.Contains(t, beErr.Label, "Engineering")
	assert.Contains(t, beErr.Label, "月限额")
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: Middleware includes budget tier info in 402 response (v2 gate)
// ─────────────────────────────────────────────────────────────────────────────

// TestMiddleware_IncludesBudgetTierInfoIn402Response verifies that the v2
// ExceededError writes a 402 response with tier-specific error codes and
// additional constraints.
func TestMiddleware_IncludesBudgetTierInfoIn402Response(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()

	// Simulate the v2 gate's ExceededError response
	router := gin.New()
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		exceededErr := &budget.ExceededError{
			Code: "tier_budget_exceeded",
			Primary: budget.Constraint{
				Dimension:  budget.DimensionTier,
				ScopeType:  "member",
				ScopeID:    "u-1",
				PeriodType: budgetdomain.PeriodMonthly,
			},
			Additional: []budget.PublicConstraint{
				{
					Dimension:   budget.DimensionTotal,
					PeriodType:  budgetdomain.PeriodWeekly,
					SourceLevel: "member",
				},
			},
		}
		budget.WriteHTTPError(c, exceededErr)
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusPaymentRequired, w.Code)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))

	errObj, ok := body["error"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "tier_budget_exceeded", errObj["type"])
	assert.Equal(t, "tier_budget_exceeded", errObj["code"])
	assert.Equal(t, "budget", errObj["gate"])

	// Verify additional constraints are included
	additional, ok := body["additional_constraints"].([]interface{})
	require.True(t, ok)
	require.Len(t, additional, 1)
	first := additional[0].(map[string]interface{})
	assert.Equal(t, "total", first["dimension"])
	assert.Equal(t, "weekly", first["period_type"])
	assert.Equal(t, "member", first["source_level"])
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: Middleware skips check for non-budget-gated requests
// ─────────────────────────────────────────────────────────────────────────────

// TestMiddleware_SkipsCheckForNoIdentity verifies that Reserve skips the
// budget check when no identity is set on the context.
func TestMiddleware_SkipsCheckForNoIdentity(t *testing.T) {
	rdb, _ := startMiddlewareMiniredis(t)
	c := newMiddlewareGinCtx()
	// No identity set

	result, err := middleware.Reserve(c, &middleware.BudgetDeps{RDB: rdb})
	assert.Nil(t, result)
	assert.NoError(t, err, "should skip check when no identity")
}

// TestMiddleware_SkipsCheckForEmptyBudgetChain verifies that Reserve skips
// the budget check when the identity has no budget scopes.
func TestMiddleware_SkipsCheckForEmptyBudgetChain(t *testing.T) {
	rdb, _ := startMiddlewareMiniredis(t)
	c := newMiddlewareGinCtx()
	c.Set("identity", &gwmodel.UserIdentity{
		BudgetChain: []gwmodel.BudgetScope{},
	})

	result, err := middleware.Reserve(c, &middleware.BudgetDeps{RDB: rdb})
	assert.Nil(t, result)
	assert.NoError(t, err, "should skip check when budget chain is empty")
}

// TestMiddleware_SkipsCheckForNilDeps verifies that Reserve skips the check
// when deps is nil (e.g., no Redis configured).
func TestMiddleware_SkipsCheckForNilDeps(t *testing.T) {
	c := newMiddlewareGinCtx()
	c.Set("identity", &gwmodel.UserIdentity{
		BudgetChain: []gwmodel.BudgetScope{
			{ScopeType: "studio", ScopeID: "s-1", AmountUSD: 100, WeeklyAmountUSD: -1, DailyAmountUSD: -1, Label: "s1"},
		},
	})

	result, err := middleware.Reserve(c, nil)
	assert.Nil(t, result)
	assert.NoError(t, err, "should skip check when deps is nil")
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: V2 Gate Check integration with middleware context
// ─────────────────────────────────────────────────────────────────────────────

// TestMiddleware_V2GateCheckPassesRequest verifies that the v2 Gate.Check
// passes when budget is available and sets the correct context keys.
func TestMiddleware_V2GateCheckPassesRequest(t *testing.T) {
	rdb, mr := startMiddlewareMiniredis(t)
	_ = mr
	holder := budget.NewSnapshotHolder()
	holder.Set(&configsync.BudgetTierSnapshot{
		SchemaVersion: 1, Revision: 1, MasterEnabled: true, AccountingEnabled: true,
		PositiveEnforcementEnabled: true, Timezone: "Asia/Shanghai", WeekStart: "monday",
	})
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)

	c := newMiddlewareGinCtx()
	c.Set("identity", &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "u-1", AmountUSD: 100, WeeklyAmountUSD: -1, DailyAmountUSD: -1,
	}}})

	gate := budget.NewGate(holder, budget.NewRedisStore(rdb), nil, func() time.Time { return now })
	outcome, err := gate.Check(c)
	require.NoError(t, err)
	require.NotNil(t, outcome)
	require.True(t, outcome.Plan.RedisCheckSucceeded)
}

// TestMiddleware_V2GateCheckReturns402 verifies that the v2 Gate.Check returns
// an ExceededError that writes a 402 response when budget is exceeded.
func TestMiddleware_V2GateCheckReturns402(t *testing.T) {
	rdb, mr := startMiddlewareMiniredis(t)
	_ = mr
	holder := budget.NewSnapshotHolder()
	holder.Set(&configsync.BudgetTierSnapshot{
		SchemaVersion: 1, Revision: 1,
		V2TotalEnforcementEnabled: true,
		Timezone:                  "Asia/Shanghai", WeekStart: "monday",
	})
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)

	totalKey := budget.TotalUsedKey("member", "u-1", "2026-07")
	require.NoError(t, rdb.Set(context.Background(), totalKey, "100000000", 0).Err())

	c := newMiddlewareGinCtx()
	c.Set("identity", &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "u-1", AmountUSD: 100, WeeklyAmountUSD: -1, DailyAmountUSD: -1,
	}}})

	gate := budget.NewGate(holder, budget.NewRedisStore(rdb), nil, func() time.Time { return now })
	_, err := gate.Check(c)

	var exceeded *budget.ExceededError
	require.ErrorAs(t, err, &exceeded)
	require.Equal(t, "budget_exceeded", exceeded.Code)
	require.Equal(t, budget.DimensionTotal, exceeded.Primary.Dimension)

	// Verify HTTP response
	w := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(w)
	ginCtx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	exceeded.WriteHTTPResponse(ginCtx)
	require.Equal(t, http.StatusPaymentRequired, w.Code)
}

// TestMiddleware_V2GateCheckSetsContextKeys verifies that after a successful
// Gate.Check, the accounting plan and tier eligibility are stored in the
// context for downstream middleware.
func TestMiddleware_V2GateCheckSetsContextKeys(t *testing.T) {
	rdb, mr := startMiddlewareMiniredis(t)
	_ = mr
	holder := budget.NewSnapshotHolder()
	holder.Set(&configsync.BudgetTierSnapshot{
		SchemaVersion: 1, Revision: 1, MasterEnabled: true, AccountingEnabled: true,
		Timezone: "Asia/Shanghai", WeekStart: "monday",
	})
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)

	c := newMiddlewareGinCtx()
	c.Set("identity", &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "u-1", AmountUSD: 100, WeeklyAmountUSD: -1, DailyAmountUSD: -1,
	}}})

	gate := budget.NewGate(holder, budget.NewRedisStore(rdb), nil, func() time.Time { return now })
	_, err := gate.Check(c)
	require.NoError(t, err)

	// Accounting plan should be set
	plan, ok := budget.GetAccountingPlan(c)
	require.True(t, ok)
	require.NotNil(t, plan)

	// Tier accounting eligible should be set
	eligible, exists := c.Get(budget.CtxKeyTierAccountingEligible)
	require.True(t, exists)
	require.NotNil(t, eligible)

	// Check result should be set
	checkResult, exists := c.Get(budget.CtxKeyCheckResult)
	require.True(t, exists)
	assert.Equal(t, "pass", checkResult)
}

// TestMiddleware_LegacyBudgetAddAfterReserve verifies the end-to-end flow:
// Reserve passes → request succeeds → BudgetAdd writes cost to Redis.
func TestMiddleware_LegacyBudgetAddAfterReserve(t *testing.T) {
	rdb, mr := startMiddlewareMiniredis(t)
	period := currentMiddlewareMonthPeriod()
	dayPeriod := time.Now().Format("2006-01-02")
	year, week := time.Now().ISOWeek()
	weekPeriod := strconv.Itoa(year) + "-W" + func() string {
		if week < 10 {
			return "0" + strconv.Itoa(week)
		}
		return strconv.Itoa(week)
	}()

	c := newMiddlewareGinCtx()
	c.Set("identity", &gwmodel.UserIdentity{
		BudgetChain: []gwmodel.BudgetScope{
			{ScopeType: "dept", ScopeID: "dept-1", AmountUSD: 500, WeeklyAmountUSD: 100, DailyAmountUSD: 50, Label: "Engineering"},
		},
	})

	// Step 1: Reserve passes
	result, err := middleware.Reserve(c, &middleware.BudgetDeps{RDB: rdb})
	require.NoError(t, err)
	require.NotNil(t, result)

	// Step 2: Request succeeds, add real cost
	argsVal, _ := c.Get(middleware.CtxKeyBudgetScopeArgs)
	scopeArgs := argsVal.([]interface{})
	require.NoError(t, middleware.BudgetAdd(context.Background(), rdb, result.Keys, scopeArgs, 0.25))

	// Step 3: Verify counters
	monthUsed := func() float64 {
		v, _ := mr.Get("budget:used:dept:dept-1:" + period)
		f, _ := strconv.ParseFloat(v, 64)
		return f
	}()
	dayUsed := func() float64 {
		v, _ := mr.Get("budget:used:dept:dept-1:" + dayPeriod)
		f, _ := strconv.ParseFloat(v, 64)
		return f
	}()
	weekUsed := func() float64 {
		v, _ := mr.Get("budget:used:dept:dept-1:" + weekPeriod)
		f, _ := strconv.ParseFloat(v, 64)
		return f
	}()

	assert.InDelta(t, 0.25, monthUsed, 0.001, "monthly counter should be updated")
	assert.InDelta(t, 0.25, dayUsed, 0.001, "daily counter should be updated")
	assert.InDelta(t, 0.25, weekUsed, 0.001, "weekly counter should be updated")
}
