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

func TestCollector_PersistsFixedTierAndActualBlockSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	ctx.Set("request_id", "req-tier")
	budget.SetTierContext(ctx, budget.TierContext{
		TierID: "00bb2e65-9c67-45f7-b061-d297a3944876", TierName: "高级",
		GroupName: "premium", SnapshotRevision: 9, AccountingEligible: true,
	})
	ctx.Set(budget.CtxKeyTierAccountingEligible, true)
	resetAt := time.Date(2026, 8, 1, 0, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*3600))
	budget.SetAccountingPlan(ctx, &budget.AccountingPlan{Constraints: []budget.Constraint{
		{
			Dimension: budget.DimensionTotal, ScopeType: "member", ScopeID: "u-1",
			PeriodType: budgetdomain.PeriodMonthly, Period: "2026-07", ResetAt: resetAt,
			Key: "budget:v2:used:member:u-1:2026-07",
		},
		{
			Dimension: budget.DimensionTier, ScopeType: "member", ScopeID: "u-1",
			TierID:     "00bb2e65-9c67-45f7-b061-d297a3944876",
			PeriodType: budgetdomain.PeriodMonthly, Period: "2026-07", ResetAt: resetAt,
			Key: "budget:v2:tier:used:member:u-1:00bb2e65-9c67-45f7-b061-d297a3944876:2026-07",
		},
	}})
	budget.SetBlockContext(ctx, budget.BlockContext{
		Code: "tier_budget_exceeded", Dimension: budget.DimensionTier,
		ScopeType: "member", ScopeID: "u-1", PeriodType: budgetdomain.PeriodMonthly,
	})

	event := observability.NewCollector(observability.CollectorConfig{}, nil).NewUsageEvent(ctx)
	callLog, err := event.ToCallLog()
	require.NoError(t, err)
	require.NotNil(t, callLog.BudgetTierID)
	require.Equal(t, "高级", callLog.BudgetTierNameSnapshot)
	require.True(t, callLog.BudgetTierAccountingEligible)
	require.Equal(t, "tier_budget_exceeded", *callLog.BudgetBlockCode)
	require.Equal(t, "tier", *callLog.BudgetBlockDimension)
	require.Equal(t, "member", *callLog.BudgetBlockScopeType)
	require.Equal(t, "u-1", *callLog.BudgetBlockScopeID)
	require.Equal(t, "monthly", *callLog.BudgetBlockPeriodType)
	require.Len(t, callLog.BudgetTierConstraintsSnapshot, 1)
	require.Equal(t, "budget:v2:tier:used:member:u-1:00bb2e65-9c67-45f7-b061-d297a3944876:2026-07",
		callLog.BudgetTierConstraintsSnapshot[0].RedisKey)
	require.Equal(t, resetAt, callLog.BudgetTierConstraintsSnapshot[0].ResetAt)
}

func TestCollector_DisabledTierKeepsReportingWithoutBlock(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/rerank", nil)
	budget.SetTierContext(ctx, budget.TierContext{
		TierID: "7534a819-ceb7-4ce8-a62f-bf78627eabb7", TierName: "归档",
		AccountingEligible: false, EnforcementEligible: false,
	})
	ctx.Set(budget.CtxKeyTierAccountingEligible, false)

	event := observability.NewCollector(observability.CollectorConfig{}, nil).NewUsageEvent(ctx)
	callLog, err := event.ToCallLog()
	require.NoError(t, err)
	require.NotNil(t, callLog.BudgetTierID)
	require.Equal(t, "归档", callLog.BudgetTierNameSnapshot)
	require.False(t, callLog.BudgetTierAccountingEligible)
	require.Nil(t, callLog.BudgetBlockCode)
	require.Nil(t, callLog.BudgetBlockDimension)
	require.Nil(t, callLog.BudgetBlockScopeType)
	require.Nil(t, callLog.BudgetBlockScopeID)
	require.Nil(t, callLog.BudgetBlockPeriodType)
}
