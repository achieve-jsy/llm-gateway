package observability_test

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/example/llm-gateway/internal/gateway/observability"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestUsageEvent_IncludesBudgetTierID verifies that NewUsageEvent populates
// BudgetTierID from the tier context.
func TestUsageEvent_IncludesBudgetTierID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	ctx.Set("request_id", "req-tier-id")
	budget.SetTierContext(ctx, budget.TierContext{
		TierID:              "550e8400-e29b-41d4-a716-446655440000",
		TierName:            "Enterprise",
		GroupName:           "enterprise-group",
		SnapshotRevision:    15,
		AccountingEligible:  true,
		EnforcementEligible: true,
	})
	ctx.Set(budget.CtxKeyTierAccountingEligible, true)

	event := observability.NewCollector(observability.CollectorConfig{}, nil).NewUsageEvent(ctx)
	require.Equal(t, "550e8400-e29b-41d4-a716-446655440000", event.BudgetTierID)
	require.Equal(t, "Enterprise", event.BudgetTierNameSnapshot)
	require.Equal(t, int64(15), event.BudgetTierSnapshotRevision)
	require.True(t, event.BudgetTierAccountingEligible)
}

// TestUsageEvent_IncludesBudgetTierNameSnapshot verifies that the tier name
// snapshot is captured at event creation time.
func TestUsageEvent_IncludesBudgetTierNameSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	budget.SetTierContext(ctx, budget.TierContext{
		TierID:   "tier-snapshot",
		TierName: "高级会员",
	})

	event := observability.NewCollector(observability.CollectorConfig{}, nil).NewUsageEvent(ctx)
	require.Equal(t, "高级会员", event.BudgetTierNameSnapshot)
}

// TestUsageEvent_IncludesBudgetConstraintsSnapshot verifies that tier
// constraints from the accounting plan are captured in the event.
func TestUsageEvent_IncludesBudgetConstraintsSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	budget.SetTierContext(ctx, budget.TierContext{
		TierID:              "tier-constraints",
		TierName:            "Standard",
		AccountingEligible:  true,
		EnforcementEligible: true,
	})
	resetAt := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	budget.SetAccountingPlan(ctx, &budget.AccountingPlan{
		Constraints: []budget.Constraint{
			{
				Dimension: budget.DimensionTotal,
				ScopeType: "member", ScopeID: "u-1",
				PeriodType: budgetdomain.PeriodMonthly, Period: "2026-07",
				Key: "budget:v2:used:member:u-1:2026-07",
			},
			{
				Dimension: budget.DimensionTier,
				ScopeType: "member", ScopeID: "u-1",
				TierID: "tier-constraints",
				PeriodType: budgetdomain.PeriodMonthly, Period: "2026-07",
				ResetAt: resetAt,
				Key: "budget:v2:tier:used:member:u-1:tier-constraints:2026-07",
			},
			{
				Dimension: budget.DimensionTier,
				ScopeType: "member", ScopeID: "u-1",
				TierID: "tier-constraints",
				PeriodType: budgetdomain.PeriodDaily, Period: "2026-07-11",
				ResetAt: resetAt,
				Key: "budget:v2:tier:used:member:u-1:tier-constraints:2026-07-11",
			},
		},
	})

	event := observability.NewCollector(observability.CollectorConfig{}, nil).NewUsageEvent(ctx)
	require.Len(t, event.BudgetTierConstraintsSnapshot, 2, "only tier constraints should be captured")
	require.Equal(t, "budget:v2:tier:used:member:u-1:tier-constraints:2026-07", event.BudgetTierConstraintsSnapshot[0].RedisKey)
	require.Equal(t, "monthly", event.BudgetTierConstraintsSnapshot[0].PeriodType)
	require.Equal(t, resetAt, event.BudgetTierConstraintsSnapshot[0].ResetAt)
	require.Equal(t, "budget:v2:tier:used:member:u-1:tier-constraints:2026-07-11", event.BudgetTierConstraintsSnapshot[1].RedisKey)
	require.Equal(t, "daily", event.BudgetTierConstraintsSnapshot[1].PeriodType)
}

// TestUsageEvent_BlockedRequestsAreLoggedWithBlockCodeAndDimension verifies
// that when a request is blocked, the block context is captured in the event.
func TestUsageEvent_BlockedRequestsAreLoggedWithBlockCodeAndDimension(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	budget.SetBlockContext(ctx, budget.BlockContext{
		Code:       "tier_budget_exceeded",
		Dimension:  budget.DimensionTier,
		ScopeType:  "member",
		ScopeID:    "u-123",
		PeriodType: budgetdomain.PeriodMonthly,
	})

	event := observability.NewCollector(observability.CollectorConfig{}, nil).NewUsageEvent(ctx)
	require.NotNil(t, event.BudgetBlockCode)
	require.Equal(t, "tier_budget_exceeded", *event.BudgetBlockCode)
	require.NotNil(t, event.BudgetBlockDimension)
	require.Equal(t, "tier", *event.BudgetBlockDimension)
	require.NotNil(t, event.BudgetBlockScopeType)
	require.Equal(t, "member", *event.BudgetBlockScopeType)
	require.NotNil(t, event.BudgetBlockScopeID)
	require.Equal(t, "u-123", *event.BudgetBlockScopeID)
	require.NotNil(t, event.BudgetBlockPeriodType)
	require.Equal(t, "monthly", *event.BudgetBlockPeriodType)
}

// TestUsageEvent_AccountingEligibleFlagIsSetCorrectly verifies that the
// accounting eligibility flag is propagated from the context to the event.
func TestUsageEvent_AccountingEligibleFlagIsSetCorrectly(t *testing.T) {
	for _, eligible := range []bool{true, false} {
		gin.SetMode(gin.TestMode)
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
		budget.SetTierContext(ctx, budget.TierContext{
			TierID:              "tier-eligible",
			TierName:            "Test",
			AccountingEligible:  eligible,
			EnforcementEligible: eligible,
		})
		ctx.Set(budget.CtxKeyTierAccountingEligible, eligible)

		event := observability.NewCollector(observability.CollectorConfig{}, nil).NewUsageEvent(ctx)
		require.Equal(t, eligible, event.BudgetTierAccountingEligible)
	}
}

// TestUsageEvent_NoBudgetContextLeavesFieldsEmpty verifies that when no
// budget context is set, the budget fields remain empty/nil.
func TestUsageEvent_NoBudgetContextLeavesFieldsEmpty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)

	event := observability.NewCollector(observability.CollectorConfig{}, nil).NewUsageEvent(ctx)
	require.Empty(t, event.BudgetTierID)
	require.Empty(t, event.BudgetTierNameSnapshot)
	require.Nil(t, event.BudgetBlockCode)
	require.Nil(t, event.BudgetBlockDimension)
	require.Empty(t, event.BudgetTierConstraintsSnapshot)
}

// TestUsageEvent_TotalBlockDoesNotCaptureTierConstraints verifies that when
// only a total dimension is blocked, no tier constraints are captured unless
// the accounting plan contains them.
func TestUsageEvent_TotalBlockDoesNotCaptureTierConstraints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	budget.SetBlockContext(ctx, budget.BlockContext{
		Code:       "budget_exceeded",
		Dimension:  budget.DimensionTotal,
		ScopeType:  "member",
		ScopeID:    "u-1",
		PeriodType: budgetdomain.PeriodMonthly,
	})

	event := observability.NewCollector(observability.CollectorConfig{}, nil).NewUsageEvent(ctx)
	require.NotNil(t, event.BudgetBlockCode)
	require.Equal(t, "budget_exceeded", *event.BudgetBlockCode)
	require.Equal(t, "total", *event.BudgetBlockDimension)
	require.Empty(t, event.BudgetTierConstraintsSnapshot)
}
