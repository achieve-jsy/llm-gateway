package budget_test

import (
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestNewTierContext_ResolvesCorrectly verifies that SetTierContext and
// GetTierContext round-trip a fully populated TierContext.
func TestNewTierContext_ResolvesCorrectly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)

	tier := budget.TierContext{
		TierID:              "tier-abc",
		TierName:            "Premium",
		GroupName:           "premium-group",
		SnapshotRevision:    42,
		AccountingEligible:  true,
		EnforcementEligible: true,
	}
	budget.SetTierContext(ctx, tier)

	got, ok := budget.GetTierContext(ctx)
	require.True(t, ok)
	require.Equal(t, "tier-abc", got.TierID)
	require.Equal(t, "Premium", got.TierName)
	require.Equal(t, "premium-group", got.GroupName)
	require.Equal(t, int64(42), got.SnapshotRevision)
	require.True(t, got.AccountingEligible)
	require.True(t, got.EnforcementEligible)
}

// TestNewTierContext_NoTierAssigned verifies that GetTierContext returns false
// when no tier has been set on the context.
func TestNewTierContext_NoTierAssigned(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)

	got, ok := budget.GetTierContext(ctx)
	require.False(t, ok)
	require.Empty(t, got.TierID)
	require.Empty(t, got.TierName)
}

// TestNewTierContext_DisabledTier verifies that a tier with both
// AccountingEligible and EnforcementEligible set to false is stored and
// retrieved correctly (the gate will skip enforcement for it).
func TestNewTierContext_DisabledTier(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)

	tier := budget.TierContext{
		TierID:              "tier-disabled",
		TierName:            "Archived",
		GroupName:           "archived-group",
		SnapshotRevision:    10,
		AccountingEligible:  false,
		EnforcementEligible: false,
	}
	budget.SetTierContext(ctx, tier)

	got, ok := budget.GetTierContext(ctx)
	require.True(t, ok)
	require.Equal(t, "tier-disabled", got.TierID)
	require.Equal(t, "Archived", got.TierName)
	require.False(t, got.AccountingEligible)
	require.False(t, got.EnforcementEligible)
}

// TestTierContext_AccountingPlanRoundTrip verifies that SetAccountingPlan and
// GetAccountingPlan round-trip correctly.
func TestTierContext_AccountingPlanRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)

	plan := &budget.AccountingPlan{
		RedisCheckSucceeded: true,
		TierAccountingEligible: true,
	}
	budget.SetAccountingPlan(ctx, plan)

	got, ok := budget.GetAccountingPlan(ctx)
	require.True(t, ok)
	require.NotNil(t, got)
	require.True(t, got.RedisCheckSucceeded)
	require.True(t, got.TierAccountingEligible)
}

// TestTierContext_BlockContextRoundTrip verifies that SetBlockContext and
// GetBlockContext round-trip correctly.
func TestTierContext_BlockContextRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)

	block := budget.BlockContext{
		Code:      "tier_budget_exceeded",
		Dimension: budget.DimensionTier,
		ScopeType: "member",
		ScopeID:   "u-1",
	}
	budget.SetBlockContext(ctx, block)

	got, ok := budget.GetBlockContext(ctx)
	require.True(t, ok)
	require.Equal(t, "tier_budget_exceeded", got.Code)
	require.Equal(t, budget.DimensionTier, got.Dimension)
	require.Equal(t, "member", got.ScopeType)
	require.Equal(t, "u-1", got.ScopeID)
}

// TestTierContext_NoBlockContext verifies that GetBlockContext returns false
// when no block has been set.
func TestTierContext_NoBlockContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)

	got, ok := budget.GetBlockContext(ctx)
	require.False(t, ok)
	require.Empty(t, got.Code)
}
