package budget_test

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
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
	"github.com/stretchr/testify/require"
)

func TestBuildAccountingPlan_ExpandsTotalAndEligibleTierConstraints(t *testing.T) {
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	identity := &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "u-1", Label: "成员",
		AmountUSD: 500, WeeklyAmountUSD: 20, DailyAmountUSD: -1,
		TierLimits: []gwmodel.TierLimit{
			{TierID: "tier-1", TierName: "高级", PeriodType: "monthly", EffectiveLimitUSD: "0.000000", Enabled: true, EffectiveAt: now.Add(-time.Hour)},
			{TierID: "tier-1", TierName: "高级", PeriodType: "daily", EffectiveLimitUSD: "1.250001", Enabled: false, EffectiveAt: now.Add(-time.Hour)},
		},
	}}}
	snapshot := &configsync.BudgetTierSnapshot{
		SchemaVersion: 1, MasterEnabled: true, AccountingEnabled: true,
		Timezone: "Asia/Shanghai", WeekStart: "monday",
	}
	tier := budget.TierContext{TierID: "tier-1", TierName: "高级", GroupName: "premium", AccountingEligible: true, EnforcementEligible: true}

	plan, err := budget.BuildAccountingPlan(identity, tier, now, snapshot)
	require.NoError(t, err)
	require.Len(t, plan.Constraints, 3)
	require.Equal(t, budget.DimensionTotal, plan.Constraints[0].Dimension)
	require.Equal(t, "2026-07", plan.Constraints[0].Period)
	require.Equal(t, budget.DimensionTotal, plan.Constraints[1].Dimension)
	require.Equal(t, "2026-W28", plan.Constraints[1].Period)
	require.Equal(t, budget.DimensionTier, plan.Constraints[2].Dimension)
	require.True(t, plan.Constraints[2].StaticZero)
	require.Equal(t, "budget:v2:tier:used:member:u-1:tier-1:2026-07", plan.Constraints[2].Key)
	require.True(t, plan.TierAccountingEligible)
}

func TestBuildAccountingPlan_EnabledSnapshotWritesTierConstraints(t *testing.T) {
	now := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	identity := &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "key", ScopeID: "key-1", Label: "X-Lab",
		AmountUSD: -1, WeeklyAmountUSD: -1, DailyAmountUSD: -1,
		TierLimits: []gwmodel.TierLimit{{
			TierID: "tier-high", TierName: "高级模型", PeriodType: "monthly",
			EffectiveLimitUSD: "45000.000000", Enabled: true, EffectiveAt: now.Add(-time.Hour),
		}},
	}}}
	snapshot := &configsync.BudgetTierSnapshot{
		SchemaVersion: 1, MasterEnabled: true, AccountingEnabled: true,
		Timezone: "Asia/Shanghai", WeekStart: "monday",
	}
	tier := budget.TierContext{
		TierID: "tier-high", TierName: "高级模型", GroupName: "x-lab",
		AccountingEligible: true, EnforcementEligible: true,
	}

	plan, err := budget.BuildAccountingPlan(identity, tier, now, snapshot)
	require.NoError(t, err)
	require.True(t, plan.TierAccountingEligible)
	require.True(t, plan.TierEnforcementEligible)
	require.Len(t, plan.Constraints, 1)
	require.Equal(t, budget.DimensionTier, plan.Constraints[0].Dimension)
	require.Equal(t, "budget:v2:tier:used:key:key-1:tier-high:2026-07", plan.Constraints[0].Key)
	require.Equal(t, int64(45_000_000_000), plan.Constraints[0].LimitMicroUSD)
}

func newBudgetContext(identity *gwmodel.UserIdentity, tier budget.TierContext) *gin.Context {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	ctx.Set("identity", identity)
	if tier.TierID != "" {
		budget.SetTierContext(ctx, tier)
	}
	return ctx
}

func TestGate_StaticTierZeroFailsClosedWhenRedisDown(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	holder := budget.NewSnapshotHolder()
	holder.Set(&configsync.BudgetTierSnapshot{
		SchemaVersion: 1, Revision: 1, MasterEnabled: true, AccountingEnabled: true,
		ZeroBlockEnabled: true, Timezone: "Asia/Shanghai", WeekStart: "monday",
	})
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	identity := &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "u-1", AmountUSD: -1, WeeklyAmountUSD: -1, DailyAmountUSD: -1,
		TierLimits: []gwmodel.TierLimit{{TierID: "tier-1", PeriodType: "monthly", EffectiveLimitUSD: "0", Enabled: true, EffectiveAt: now.Add(-time.Hour)}},
	}}}
	ctx := newBudgetContext(identity, budget.TierContext{TierID: "tier-1", AccountingEligible: true, EnforcementEligible: true})
	mr.Close()

	_, err := budget.NewGate(holder, budget.NewRedisStore(rdb), nil, func() time.Time { return now }).Check(ctx)
	var exceeded *budget.ExceededError
	require.ErrorAs(t, err, &exceeded)
	require.Equal(t, "tier_budget_exceeded", exceeded.Code)
	block, ok := budget.GetBlockContext(ctx)
	require.True(t, ok)
	require.Equal(t, budget.DimensionTier, block.Dimension)
}

func TestGate_StaticTotalZeroFailsClosedWhenRedisDownBeforeV2Cutover(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	holder := budget.NewSnapshotHolder()
	holder.Set(&configsync.BudgetTierSnapshot{
		SchemaVersion: 1, Revision: 1,
		ZeroBlockEnabled:          true,
		V2TotalEnforcementEnabled: false,
		LegacyTotalWriteEnabled:   true,
		Timezone:                  "Asia/Shanghai",
		WeekStart:                 "monday",
	})
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	identity := &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "u-1",
		AmountUSD: 0, WeeklyAmountUSD: -1, DailyAmountUSD: -1,
	}}}
	ctx := newBudgetContext(identity, budget.TierContext{})
	mr.Close()
	legacy := budget.LegacyTotalFuncs{
		CheckFunc: func(ctx context.Context, plan *budget.AccountingPlan) (*budget.Constraint, error) {
			return middleware.CheckLegacyBudget(ctx, rdb, plan)
		},
	}

	_, err := budget.NewGate(holder, budget.NewRedisStore(rdb), legacy, func() time.Time { return now }).Check(ctx)
	var exceeded *budget.ExceededError
	require.ErrorAs(t, err, &exceeded)
	require.Equal(t, "budget_exceeded", exceeded.Code)
	require.Equal(t, budget.DimensionTotal, exceeded.Primary.Dimension)
}

func TestGate_PositiveRedisFailureFailsOpenAndDisablesV2Write(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	holder := budget.NewSnapshotHolder()
	holder.Set(&configsync.BudgetTierSnapshot{SchemaVersion: 1, Revision: 1, MasterEnabled: true, AccountingEnabled: true, PositiveEnforcementEnabled: true, Timezone: "Asia/Shanghai", WeekStart: "monday"})
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	identity := &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "u-1", AmountUSD: 1, WeeklyAmountUSD: -1, DailyAmountUSD: -1,
	}}}
	ctx := newBudgetContext(identity, budget.TierContext{})
	mr.Close()

	outcome, err := budget.NewGate(holder, budget.NewRedisStore(rdb), nil, func() time.Time { return now }).Check(ctx)
	require.NoError(t, err)
	require.True(t, outcome.FailOpen)
	require.False(t, outcome.Plan.RedisCheckSucceeded)
	require.NoError(t, budget.NewGate(holder, budget.NewRedisStore(rdb), nil, func() time.Time { return now }).Account(context.Background(), outcome.Plan, 1).V2Error)
}

func TestGate_ShadowComputesWouldBlockWithoutRejecting(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	holder := budget.NewSnapshotHolder()
	holder.Set(&configsync.BudgetTierSnapshot{SchemaVersion: 1, Revision: 1, MasterEnabled: true, AccountingEnabled: true, ShadowEnabled: true, Timezone: "Asia/Shanghai", WeekStart: "monday"})
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	identity := &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "u-1", AmountUSD: -1, WeeklyAmountUSD: -1, DailyAmountUSD: -1,
		TierLimits: []gwmodel.TierLimit{{TierID: "tier-1", PeriodType: "monthly", EffectiveLimitUSD: "1", Enabled: true, EffectiveAt: now.Add(-time.Hour)}},
	}}}
	tier := budget.TierContext{TierID: "tier-1", AccountingEligible: true, EnforcementEligible: true}
	ctx := newBudgetContext(identity, tier)
	require.NoError(t, rdb.Set(context.Background(), budget.TierUsedKey("member", "u-1", "tier-1", "2026-07"), "1000000", 0).Err())

	outcome, err := budget.NewGate(holder, budget.NewRedisStore(rdb), nil, func() time.Time { return now }).Check(ctx)
	require.NoError(t, err)
	require.Len(t, outcome.WouldBlock, 1)
	require.True(t, ctx.GetBool(budget.CtxKeyShadowMode))
	_, blocked := budget.GetBlockContext(ctx)
	require.False(t, blocked)
}

func TestGate_ExceededErrorPublicResponseDoesNotLeakScope(t *testing.T) {
	err := &budget.ExceededError{
		Code:    "tier_budget_exceeded",
		Primary: budget.Constraint{Dimension: budget.DimensionTier, ScopeID: "secret-u-1"},
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	err.WriteHTTPResponse(ctx)
	require.Equal(t, 402, ctx.Writer.Status())
	require.NotContains(t, recorder.Body.String(), "secret-u-1")
}

func TestGate_MissingSnapshotUsesLegacyOnly(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	holder := budget.NewSnapshotHolder()
	legacyChecks := 0
	gate := budget.NewGate(holder, budget.NewRedisStore(rdb), budget.LegacyTotalFuncs{
		CheckFunc: func(context.Context, *budget.AccountingPlan) (*budget.Constraint, error) {
			legacyChecks++
			return nil, nil
		},
		AddFunc: func(context.Context, []string, []any, float64) error { return nil },
	}, time.Now)
	ctx := newBudgetContext(&gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "u-1", AmountUSD: 1, WeeklyAmountUSD: -1, DailyAmountUSD: -1,
	}}}, budget.TierContext{})

	outcome, err := gate.Check(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, legacyChecks)
	require.False(t, outcome.Plan.RedisCheckSucceeded)
	require.False(t, mr.Exists("budget:v2:used:member:u-1"))
}

func TestGate_LegacyTotalBlocksEveryScopeAndPeriod(t *testing.T) {
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	periods := []struct {
		name       string
		periodType budgetdomain.PeriodType
		period     string
		configure  func(*gwmodel.BudgetScope)
	}{
		{name: "monthly", periodType: budgetdomain.PeriodMonthly, period: "2026-07", configure: func(scope *gwmodel.BudgetScope) { scope.AmountUSD = 1 }},
		{name: "weekly", periodType: budgetdomain.PeriodWeekly, period: "2026-W28", configure: func(scope *gwmodel.BudgetScope) { scope.WeeklyAmountUSD = 1 }},
		{name: "daily", periodType: budgetdomain.PeriodDaily, period: "2026-07-12", configure: func(scope *gwmodel.BudgetScope) { scope.DailyAmountUSD = 1 }},
	}
	scopes := []struct {
		scopeType string
		scopeID   string
	}{
		{scopeType: "member", scopeID: "member-1"},
		{scopeType: "dept_daily", scopeID: "dept-daily-1"},
		{scopeType: "dept_service", scopeID: "dept-service-1"},
		{scopeType: "studio", scopeID: "studio-1"},
		{scopeType: "company", scopeID: "company-1"},
	}

	for _, scopeCase := range scopes {
		for _, periodCase := range periods {
			t.Run(scopeCase.scopeType+"/"+periodCase.name, func(t *testing.T) {
				mr := miniredis.RunT(t)
				rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
				t.Cleanup(func() { _ = rdb.Close() })
				scope := gwmodel.BudgetScope{
					ScopeType: scopeCase.scopeType, ScopeID: scopeCase.scopeID,
					AmountUSD: -1, WeeklyAmountUSD: -1, DailyAmountUSD: -1,
				}
				periodCase.configure(&scope)
				key := middleware.BudgetUsedKey(scope.ScopeType, scope.ScopeID, periodCase.period)
				require.NoError(t, rdb.Set(context.Background(), key, "1", 0).Err())
				legacy := budget.LegacyTotalFuncs{CheckFunc: func(ctx context.Context, plan *budget.AccountingPlan) (*budget.Constraint, error) {
					return middleware.CheckLegacyBudget(ctx, rdb, plan)
				}}

				ctx := newBudgetContext(&gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{scope}}, budget.TierContext{})
				_, err := budget.NewGate(nil, budget.NewRedisStore(rdb), legacy, func() time.Time { return now }).Check(ctx)
				var exceeded *budget.ExceededError
				require.ErrorAs(t, err, &exceeded)
				require.Equal(t, budget.DimensionTotal, exceeded.Primary.Dimension)
				require.Equal(t, scopeCase.scopeType, exceeded.Primary.ScopeType)
				require.Equal(t, periodCase.periodType, exceeded.Primary.PeriodType)
			})
		}
	}
}

func TestLegacyAccountingWritesEveryScopeAndPeriodInBudgetChains(t *testing.T) {
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	snapshot := &configsync.BudgetTierSnapshot{
		SchemaVersion: 1, LegacyTotalWriteEnabled: true,
		Timezone: "Asia/Shanghai", WeekStart: "monday",
	}
	tests := []struct {
		name   string
		scopes []gwmodel.BudgetScope
	}{
		{name: "personal-key", scopes: []gwmodel.BudgetScope{
			{ScopeType: "member", ScopeID: "member-1"},
			{ScopeType: "dept_daily", ScopeID: "dept-daily-1"},
			{ScopeType: "studio", ScopeID: "studio-1"},
			{ScopeType: "company", ScopeID: "company-1"},
		}},
		{name: "service-key", scopes: []gwmodel.BudgetScope{
			{ScopeType: "dept_service", ScopeID: "dept-service-1"},
			{ScopeType: "studio", ScopeID: "studio-1"},
			{ScopeType: "company", ScopeID: "company-1"},
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mr := miniredis.RunT(t)
			rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
			t.Cleanup(func() { _ = rdb.Close() })
			for i := range tc.scopes {
				tc.scopes[i].AmountUSD = 10
				tc.scopes[i].WeeklyAmountUSD = 10
				tc.scopes[i].DailyAmountUSD = 10
			}
			plan, err := budget.BuildAccountingPlan(
				&gwmodel.UserIdentity{BudgetChain: tc.scopes}, budget.TierContext{}, now, snapshot,
			)
			require.NoError(t, err)
			require.Len(t, plan.LegacyKeys, len(tc.scopes)*3)
			require.NoError(t, middleware.BudgetAdd(
				context.Background(), rdb, plan.LegacyKeys, plan.LegacyScopeArgs, 0.25,
			))

			for _, scope := range tc.scopes {
				for _, period := range []string{"2026-07", "2026-W28", "2026-07-12"} {
					value, err := rdb.Get(context.Background(), middleware.BudgetUsedKey(scope.ScopeType, scope.ScopeID, period)).Float64()
					require.NoError(t, err)
					require.InDelta(t, 0.25, value, 0.000001, "%s %s 应正确记账", scope.ScopeType, period)
				}
			}
		})
	}
}

func TestGate_CombinesLegacyAndTierBlockersBeforeSelectingPrimary(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	holder := budget.NewSnapshotHolder()
	holder.Set(&configsync.BudgetTierSnapshot{
		SchemaVersion: 1, MasterEnabled: true, AccountingEnabled: true,
		PositiveEnforcementEnabled: true, Timezone: "Asia/Shanghai", WeekStart: "monday",
	})
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	identity := &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "u-1", AmountUSD: -1, WeeklyAmountUSD: 10, DailyAmountUSD: -1,
		TierLimits: []gwmodel.TierLimit{{
			TierID: "tier-1", PeriodType: "monthly", EffectiveLimitUSD: "1",
			Enabled: true, EffectiveAt: now.Add(-time.Hour),
		}},
	}}}
	legacy := budget.LegacyTotalFuncs{CheckFunc: func(context.Context, *budget.AccountingPlan) (*budget.Constraint, error) {
		return &budget.Constraint{
			Dimension: budget.DimensionTotal, ScopeType: "member", ScopeID: "u-1",
			PeriodType: budgetdomain.PeriodWeekly, ResetAt: now.Add(24 * time.Hour),
		}, nil
	}}
	tierKey := budget.TierUsedKey("member", "u-1", "tier-1", "2026-07")
	require.NoError(t, rdb.Set(context.Background(), tierKey, "1000000", 0).Err())
	ctx := newBudgetContext(identity, budget.TierContext{
		TierID: "tier-1", AccountingEligible: true, EnforcementEligible: true,
	})

	_, err := budget.NewGate(holder, budget.NewRedisStore(rdb), legacy, func() time.Time { return now }).Check(ctx)
	var exceeded *budget.ExceededError
	require.ErrorAs(t, err, &exceeded)
	require.Equal(t, budget.DimensionTier, exceeded.Primary.Dimension)
	require.Len(t, exceeded.Additional, 1)
	require.Equal(t, budget.DimensionTotal, exceeded.Additional[0].Dimension)
}

var _ budget.LegacyTotalAdapter = budget.LegacyTotalFuncs{
	CheckFunc: func(context.Context, *budget.AccountingPlan) (*budget.Constraint, error) {
		return nil, errors.New("unused")
	},
	AddFunc: func(context.Context, []string, []any, float64) error { return nil },
}

func TestBuildAccountingPlan_DisabledTierReportsWithoutAccounting(t *testing.T) {
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	identity := &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "u-1", AmountUSD: 1,
		WeeklyAmountUSD: -1, DailyAmountUSD: -1,
		TierLimits: []gwmodel.TierLimit{{
			TierID: "tier-1", PeriodType: "monthly", EffectiveLimitUSD: "0.000000", Enabled: true, EffectiveAt: now.Add(-time.Hour),
		}},
	}}}
	tier := budget.TierContext{TierID: "tier-1", TierName: "归档", AccountingEligible: false, EnforcementEligible: false}
	snapshot := &configsync.BudgetTierSnapshot{SchemaVersion: 1, MasterEnabled: true, AccountingEnabled: true, Timezone: "Asia/Shanghai", WeekStart: "monday"}

	plan, err := budget.BuildAccountingPlan(identity, tier, now, snapshot)
	require.NoError(t, err)
	require.NotNil(t, plan.Tier)
	require.False(t, plan.TierAccountingEligible)
	require.Len(t, plan.Constraints, 1)
	require.Equal(t, budget.DimensionTotal, plan.Constraints[0].Dimension)
}

func TestBuildAccountingPlan_NegativeLegacyTotalIsUnlimited(t *testing.T) {
	identity := &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "u-1", AmountUSD: -1, WeeklyAmountUSD: -1, DailyAmountUSD: -1,
	}}}
	snapshot := &configsync.BudgetTierSnapshot{SchemaVersion: 1, Timezone: "Asia/Shanghai", WeekStart: "monday"}
	plan, err := budget.BuildAccountingPlan(identity, budget.TierContext{}, time.Now(), snapshot)
	require.NoError(t, err)
	require.Empty(t, plan.Constraints)
}

func TestBuildAccountingPlan_TierEligibleRequiresApplicableConstraint(t *testing.T) {
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	identity := &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "u-1", AmountUSD: -1, WeeklyAmountUSD: -1, DailyAmountUSD: -1,
		TierLimits: []gwmodel.TierLimit{{
			TierID: "tier-1", PeriodType: "monthly", EffectiveLimitUSD: "10",
			Enabled: true, EffectiveAt: now.Add(time.Hour),
		}},
	}}}
	snapshot := &configsync.BudgetTierSnapshot{
		SchemaVersion: 1, MasterEnabled: true, AccountingEnabled: true,
		Timezone: "Asia/Shanghai", WeekStart: "monday",
	}
	plan, err := budget.BuildAccountingPlan(identity, budget.TierContext{
		TierID: "tier-1", TierName: "高级", AccountingEligible: true, EnforcementEligible: true,
	}, now, snapshot)
	require.NoError(t, err)
	require.NotNil(t, plan.Tier)
	require.False(t, plan.TierAccountingEligible)
	require.False(t, plan.TierEnforcementEligible)
	require.Empty(t, plan.Constraints)
}

func TestBuildAccountingPlan_TierConstraintRequiresBothEligibilityFlags(t *testing.T) {
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	identity := &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "u-1",
		AmountUSD: -1, WeeklyAmountUSD: -1, DailyAmountUSD: -1,
		TierLimits: []gwmodel.TierLimit{{
			TierID: "tier-1", PeriodType: "monthly", EffectiveLimitUSD: "10",
			Enabled: true, EffectiveAt: now.Add(-time.Hour),
		}},
	}}}
	snapshot := &configsync.BudgetTierSnapshot{
		SchemaVersion: 1, MasterEnabled: true, AccountingEnabled: true,
		Timezone: "Asia/Shanghai", WeekStart: "monday",
	}
	for _, tier := range []budget.TierContext{
		{TierID: "tier-1", AccountingEligible: true, EnforcementEligible: false},
		{TierID: "tier-1", AccountingEligible: false, EnforcementEligible: true},
	} {
		plan, err := budget.BuildAccountingPlan(identity, tier, now, snapshot)
		require.NoError(t, err)
		require.Empty(t, plan.Constraints)
		require.False(t, plan.TierAccountingEligible)
		require.False(t, plan.TierEnforcementEligible)
	}
}

func TestGate_AccountDualWriteFailuresAreIndependent(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	legacyErr := errors.New("legacy write failed")
	legacyCalls := 0
	gate := budget.NewGate(nil, budget.NewRedisStore(rdb), budget.LegacyTotalFuncs{
		AddFunc: func(context.Context, []string, []any, float64) error {
			legacyCalls++
			return legacyErr
		},
	}, time.Now)
	plan := &budget.AccountingPlan{
		RedisCheckSucceeded: true,
		Snapshot: &configsync.BudgetTierSnapshot{
			SchemaVersion: 1, LegacyTotalWriteEnabled: true,
		},
		Constraints: []budget.Constraint{{
			Dimension: budget.DimensionTotal,
			Key:       "budget:v2:used:member:u-1:2026-07", PeriodType: budgetdomain.PeriodMonthly,
		}},
		LegacyKeys: []string{"budget:used:member:u-1:2026-07"},
	}

	result := gate.Account(context.Background(), plan, 250_001)
	require.NoError(t, result.V2Error)
	require.ErrorIs(t, result.LegacyError, legacyErr)
	require.Equal(t, 1, legacyCalls)
	got, err := rdb.Get(context.Background(), plan.Constraints[0].Key).Int64()
	require.NoError(t, err)
	require.Equal(t, int64(250_001), got)
}

func TestGate_AccountHonorsLegacyWriteSwitch(t *testing.T) {
	legacyCalls := 0
	gate := budget.NewGate(nil, nil, budget.LegacyTotalFuncs{
		AddFunc: func(context.Context, []string, []any, float64) error {
			legacyCalls++
			return nil
		},
	}, time.Now)
	plan := &budget.AccountingPlan{
		RedisCheckSucceeded: true,
		Snapshot: &configsync.BudgetTierSnapshot{
			SchemaVersion: 1, LegacyTotalWriteEnabled: false,
		},
		LegacyKeys: []string{"budget:used:member:u-1:2026-07"},
	}

	result := gate.Account(context.Background(), plan, 250_001)
	require.NoError(t, result.LegacyError)
	require.Zero(t, legacyCalls)
}

func TestGate_V2TotalEnforcementSkipsLegacyTotalBlocker(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	holder := budget.NewSnapshotHolder()
	holder.Set(&configsync.BudgetTierSnapshot{
		SchemaVersion: 1, Revision: 1,
		V2TotalEnforcementEnabled: true,
		LegacyTotalWriteEnabled:   true,
		Timezone:                  "Asia/Shanghai",
		WeekStart:                 "monday",
	})
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	ctx := newBudgetContext(&gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "u-1",
		AmountUSD: 10, WeeklyAmountUSD: -1, DailyAmountUSD: -1,
	}}}, budget.TierContext{})
	legacyChecks := 0
	legacy := budget.LegacyTotalFuncs{
		CheckFunc: func(context.Context, *budget.AccountingPlan) (*budget.Constraint, error) {
			legacyChecks++
			return &budget.Constraint{
				Dimension: budget.DimensionTotal, ScopeType: "member", ScopeID: "u-1",
				PeriodType: budgetdomain.PeriodMonthly, ResetAt: now.Add(24 * time.Hour),
			}, nil
		},
	}

	outcome, err := budget.NewGate(
		holder, budget.NewRedisStore(rdb), legacy, func() time.Time { return now },
	).Check(ctx)
	require.NoError(t, err)
	require.NotNil(t, outcome)
	require.Zero(t, legacyChecks)
	require.True(t, outcome.Plan.RedisCheckSucceeded)
}

func TestGate_AccountRunsLegacyWriteAfterV2CheckFailOpen(t *testing.T) {
	legacyCalls := 0
	gate := budget.NewGate(nil, nil, budget.LegacyTotalFuncs{
		AddFunc: func(context.Context, []string, []any, float64) error {
			legacyCalls++
			return nil
		},
	}, time.Now)
	plan := &budget.AccountingPlan{
		RedisCheckSucceeded: false,
		Snapshot: &configsync.BudgetTierSnapshot{
			SchemaVersion: 1, LegacyTotalWriteEnabled: true,
		},
		LegacyKeys: []string{"budget:used:member:u-1:2026-07"},
	}

	result := gate.Account(context.Background(), plan, 250_001)
	require.NoError(t, result.V2Error)
	require.NoError(t, result.LegacyError)
	require.Equal(t, 1, legacyCalls)
}

func TestGate_ZeroBudgetPolicyMatrix_TotalAlwaysClosedTierFollowsFlag(t *testing.T) {
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	for _, zeroBlockEnabled := range []bool{false, true} {
		for _, tierEligible := range []bool{false, true} {
			name := fmt.Sprintf("flag=%t/tier=%t", zeroBlockEnabled, tierEligible)
			t.Run(name, func(t *testing.T) {
				holder := budget.NewSnapshotHolder()
				holder.Set(&configsync.BudgetTierSnapshot{
					SchemaVersion: 1, MasterEnabled: true, AccountingEnabled: true,
					ZeroBlockEnabled: zeroBlockEnabled, Timezone: "Asia/Shanghai", WeekStart: "monday",
				})
				ctx := newBudgetContext(&gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
					ScopeType: "member", ScopeID: "u-1", AmountUSD: 0, DailyAmountUSD: -1, WeeklyAmountUSD: -1,
					TierLimits: []gwmodel.TierLimit{{
						TierID: "tier-1", PeriodType: "monthly", EffectiveLimitUSD: "0",
						Enabled: true, EffectiveAt: now.Add(-time.Hour),
					}},
				}}}, budget.TierContext{
					TierID: "tier-1", AccountingEligible: tierEligible, EnforcementEligible: tierEligible,
				})

				_, err := budget.NewGate(holder, nil, nil, func() time.Time { return now }).Check(ctx)
				var exceeded *budget.ExceededError
				require.ErrorAs(t, err, &exceeded)
				dimensions := map[budget.Dimension]bool{exceeded.Primary.Dimension: true}
				for _, additional := range exceeded.Additional {
					dimensions[additional.Dimension] = true
				}
				require.True(t, dimensions[budget.DimensionTotal])
				require.Equal(t, zeroBlockEnabled && tierEligible, dimensions[budget.DimensionTier])
			})
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: Reserve / Account / Multi-tier / Period boundary / Disabled tier
// ─────────────────────────────────────────────────────────────────────────────

// TestGate_ReserveSucceedsWhenUnderLimit verifies Check passes with positive
// enforcement when Redis counters are below limits.
func TestGate_ReserveSucceedsWhenUnderLimit(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	holder := budget.NewSnapshotHolder()
	holder.Set(&configsync.BudgetTierSnapshot{
		SchemaVersion: 1, Revision: 1, MasterEnabled: true, AccountingEnabled: true,
		PositiveEnforcementEnabled: true, Timezone: "Asia/Shanghai", WeekStart: "monday",
	})
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	identity := &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "u-1", AmountUSD: 100, WeeklyAmountUSD: -1, DailyAmountUSD: -1,
		TierLimits: []gwmodel.TierLimit{{
			TierID: "tier-1", PeriodType: "monthly", EffectiveLimitUSD: "50",
			Enabled: true, EffectiveAt: now.Add(-time.Hour),
		}},
	}}}
	tier := budget.TierContext{TierID: "tier-1", TierName: "高级", AccountingEligible: true, EnforcementEligible: true}
	ctx := newBudgetContext(identity, tier)

	outcome, err := budget.NewGate(holder, budget.NewRedisStore(rdb), nil, func() time.Time { return now }).Check(ctx)
	require.NoError(t, err)
	require.NotNil(t, outcome)
	require.True(t, outcome.Plan.RedisCheckSucceeded)
	block, blocked := budget.GetBlockContext(ctx)
	require.False(t, blocked)
	require.Empty(t, block.Code)
}

// TestGate_ReserveFailsWhenTierLimitExceeded verifies Check returns ExceededError
// when a tier counter already meets the limit.
func TestGate_ReserveFailsWhenTierLimitExceeded(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	holder := budget.NewSnapshotHolder()
	holder.Set(&configsync.BudgetTierSnapshot{
		SchemaVersion: 1, Revision: 1, MasterEnabled: true, AccountingEnabled: true,
		PositiveEnforcementEnabled: true, Timezone: "Asia/Shanghai", WeekStart: "monday",
	})
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	identity := &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "u-1", AmountUSD: -1, WeeklyAmountUSD: -1, DailyAmountUSD: -1,
		TierLimits: []gwmodel.TierLimit{{
			TierID: "tier-1", PeriodType: "monthly", EffectiveLimitUSD: "1",
			Enabled: true, EffectiveAt: now.Add(-time.Hour),
		}},
	}}}
	tier := budget.TierContext{TierID: "tier-1", TierName: "高级", AccountingEligible: true, EnforcementEligible: true}
	ctx := newBudgetContext(identity, tier)
	tierKey := budget.TierUsedKey("member", "u-1", "tier-1", "2026-07")
	require.NoError(t, rdb.Set(context.Background(), tierKey, "1000000", 0).Err())

	_, err := budget.NewGate(holder, budget.NewRedisStore(rdb), nil, func() time.Time { return now }).Check(ctx)
	var exceeded *budget.ExceededError
	require.ErrorAs(t, err, &exceeded)
	require.Equal(t, "tier_budget_exceeded", exceeded.Code)
	require.Equal(t, budget.DimensionTier, exceeded.Primary.Dimension)
	require.Equal(t, "member", exceeded.Primary.ScopeType)
	require.Equal(t, "u-1", exceeded.Primary.ScopeID)
}

// TestGate_ReserveWithTierSpecificConstraints verifies that tier constraints
// are only emitted when both accounting and enforcement are eligible.
func TestGate_ReserveWithTierSpecificConstraints(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	holder := budget.NewSnapshotHolder()
	holder.Set(&configsync.BudgetTierSnapshot{
		SchemaVersion: 1, Revision: 1, MasterEnabled: true, AccountingEnabled: true,
		PositiveEnforcementEnabled: true, Timezone: "Asia/Shanghai", WeekStart: "monday",
	})
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	identity := &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "u-1", AmountUSD: -1, WeeklyAmountUSD: -1, DailyAmountUSD: -1,
		TierLimits: []gwmodel.TierLimit{
			{TierID: "tier-1", PeriodType: "monthly", EffectiveLimitUSD: "10", Enabled: true, EffectiveAt: now.Add(-time.Hour)},
			{TierID: "tier-1", PeriodType: "daily", EffectiveLimitUSD: "5", Enabled: true, EffectiveAt: now.Add(-time.Hour)},
		},
	}}}
	tier := budget.TierContext{TierID: "tier-1", TierName: "高级", AccountingEligible: true, EnforcementEligible: true}
	ctx := newBudgetContext(identity, tier)

	outcome, err := budget.NewGate(holder, budget.NewRedisStore(rdb), nil, func() time.Time { return now }).Check(ctx)
	require.NoError(t, err)
	require.NotNil(t, outcome)
	// Plan should have 2 tier constraints (monthly + daily)
	var tierCount int
	for _, c := range outcome.Plan.Constraints {
		if c.Dimension == budget.DimensionTier {
			tierCount++
		}
	}
	require.Equal(t, 2, tierCount)
}

func TestGate_BlocksWhenAnyDailyWeeklyOrMonthlyTierLimitIsReached(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	holder := budget.NewSnapshotHolder()
	holder.Set(&configsync.BudgetTierSnapshot{
		SchemaVersion: 1, Revision: 1, MasterEnabled: true, AccountingEnabled: true,
		PositiveEnforcementEnabled: true, Timezone: "Asia/Shanghai", WeekStart: "monday",
	})
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	identity := &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "u-1", AmountUSD: -1, WeeklyAmountUSD: -1, DailyAmountUSD: -1,
		TierLimits: []gwmodel.TierLimit{
			{TierID: "tier-1", PeriodType: "daily", EffectiveLimitUSD: "5", Enabled: true, EffectiveAt: now.Add(-time.Hour)},
			{TierID: "tier-1", PeriodType: "weekly", EffectiveLimitUSD: "5", Enabled: true, EffectiveAt: now.Add(-time.Hour)},
			{TierID: "tier-1", PeriodType: "monthly", EffectiveLimitUSD: "5", Enabled: true, EffectiveAt: now.Add(-time.Hour)},
		},
	}}}
	ctx := newBudgetContext(identity, budget.TierContext{TierID: "tier-1", AccountingEligible: true, EnforcementEligible: true})
	for _, key := range []string{
		budget.TierUsedKey("member", "u-1", "tier-1", "2026-07-12"),
		budget.TierUsedKey("member", "u-1", "tier-1", "2026-W28"),
		budget.TierUsedKey("member", "u-1", "tier-1", "2026-07"),
	} {
		require.NoError(t, rdb.Set(context.Background(), key, "5000000", 0).Err())
	}

	_, err := budget.NewGate(holder, budget.NewRedisStore(rdb), nil, func() time.Time { return now }).Check(ctx)
	var exceeded *budget.ExceededError
	require.ErrorAs(t, err, &exceeded)
	require.Equal(t, "tier_budget_exceeded", exceeded.Code)
	require.Len(t, exceeded.Additional, 2)
}

// TestGate_AccountUpdatesRedisCountersCorrectly verifies that Account writes
// micro-USD amounts to both total and tier counters.
func TestGate_AccountUpdatesRedisCountersCorrectly(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := budget.NewRedisStore(rdb)
	gate := budget.NewGate(nil, store, nil, time.Now)

	rawTotalKey := "budget:v2:used:member:u-1:2026-07"
	rawTierKey := "budget:v2:tier:used:member:u-1:tier-1:2026-07"
	plan := &budget.AccountingPlan{
		RedisCheckSucceeded:    true,
		TierAccountingEligible: true,
		Snapshot:               &configsync.BudgetTierSnapshot{SchemaVersion: 1},
		Constraints: []budget.Constraint{
			{Dimension: budget.DimensionTotal, Key: rawTotalKey, PeriodType: budgetdomain.PeriodMonthly},
			{Dimension: budget.DimensionTier, Key: rawTierKey, PeriodType: budgetdomain.PeriodMonthly, TierID: "tier-1"},
		},
	}

	result := gate.Account(context.Background(), plan, 500_000)
	require.NoError(t, result.V2Error)

	// Add writes via GenerationKey; since generation is empty/legacy the key
	// is used as-is. Read via miniredis to verify the counter.
	totalKey := budget.GenerationKey(budget.LegacyGeneration, rawTotalKey)
	tierKey := budget.GenerationKey(budget.LegacyGeneration, rawTierKey)

	gotTotal, err := rdb.Get(context.Background(), totalKey).Int64()
	require.NoError(t, err, "total counter should exist after Account")
	require.Equal(t, int64(500_000), gotTotal)

	gotTier, err := rdb.Get(context.Background(), tierKey).Int64()
	require.NoError(t, err, "tier counter should exist after Account")
	require.Equal(t, int64(500_000), gotTier)
}

// TestGate_RollbackRevertsReservation verifies that releasing a billable
// reservation removes the inflight entry from the sorted set.
func TestGate_RollbackRevertsReservation(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := budget.NewRedisStoreWithAdmission(rdb, budget.AdmissionConfig{
		SpoolCapacity: 10, ReservationTTL: time.Minute,
	})

	reservation, err := store.ReserveWithGeneration(context.Background(), "rollback-req", time.Now())
	require.NoError(t, err)
	require.Equal(t, int64(1), rdb.ZCard(context.Background(), budget.BillableInflightKey).Val())

	require.NoError(t, reservation.Finish(context.Background()))
	require.Equal(t, int64(0), rdb.ZCard(context.Background(), budget.BillableInflightKey).Val())
}

// TestGate_MultiTierConstraintChecking verifies that both total and tier
// dimensions are checked in a single Redis call, and all blockers are reported.
func TestGate_MultiTierConstraintChecking(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	holder := budget.NewSnapshotHolder()
	holder.Set(&configsync.BudgetTierSnapshot{
		SchemaVersion: 1, Revision: 1, MasterEnabled: true, AccountingEnabled: true,
		V2TotalEnforcementEnabled: true, PositiveEnforcementEnabled: true,
		Timezone: "Asia/Shanghai", WeekStart: "monday",
	})
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	identity := &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "u-1", AmountUSD: 10, WeeklyAmountUSD: -1, DailyAmountUSD: -1,
		TierLimits: []gwmodel.TierLimit{{
			TierID: "tier-1", PeriodType: "monthly", EffectiveLimitUSD: "1",
			Enabled: true, EffectiveAt: now.Add(-time.Hour),
		}},
	}}}
	tier := budget.TierContext{TierID: "tier-1", TierName: "高级", AccountingEligible: true, EnforcementEligible: true}
	ctx := newBudgetContext(identity, tier)

	totalKey := budget.TotalUsedKey("member", "u-1", "2026-07")
	tierKey := budget.TierUsedKey("member", "u-1", "tier-1", "2026-07")
	require.NoError(t, rdb.Set(context.Background(), totalKey, "10000000", 0).Err())
	require.NoError(t, rdb.Set(context.Background(), tierKey, "1000000", 0).Err())

	_, err := budget.NewGate(holder, budget.NewRedisStore(rdb), nil, func() time.Time { return now }).Check(ctx)
	var exceeded *budget.ExceededError
	require.ErrorAs(t, err, &exceeded)
	// Both total and tier should be blockers. Primary is the one with the
	// longer reset window (monthly total), tier is in Additional.
	dimensions := map[budget.Dimension]bool{exceeded.Primary.Dimension: true}
	for _, additional := range exceeded.Additional {
		dimensions[additional.Dimension] = true
	}
	require.True(t, dimensions[budget.DimensionTotal], "total dimension should be blocked")
	require.True(t, dimensions[budget.DimensionTier], "tier dimension should be blocked")
}

// TestGate_PeriodBoundaryHandling verifies that period keys resolve correctly
// across month boundaries.
func TestGate_PeriodBoundaryHandling(t *testing.T) {
	// 2026-07-31 23:30 UTC → monthly period = "2026-07"
	julEnd := time.Date(2026, 7, 31, 23, 30, 0, 0, time.UTC)
	// 2026-08-01 00:30 UTC → monthly period = "2026-08"
	augStart := time.Date(2026, 8, 1, 0, 30, 0, 0, time.UTC)

	identity := &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "u-1", AmountUSD: 100, WeeklyAmountUSD: -1, DailyAmountUSD: -1,
	}}}
	snapshot := &configsync.BudgetTierSnapshot{
		SchemaVersion: 1, Timezone: "UTC", WeekStart: "monday",
	}

	julPlan, err := budget.BuildAccountingPlan(identity, budget.TierContext{}, julEnd, snapshot)
	require.NoError(t, err)
	augPlan, err := budget.BuildAccountingPlan(identity, budget.TierContext{}, augStart, snapshot)
	require.NoError(t, err)

	// Find the monthly total constraints
	var julMonthly, augMonthly string
	for _, c := range julPlan.Constraints {
		if c.Dimension == budget.DimensionTotal && c.PeriodType == budgetdomain.PeriodMonthly {
			julMonthly = c.Key
		}
	}
	for _, c := range augPlan.Constraints {
		if c.Dimension == budget.DimensionTotal && c.PeriodType == budgetdomain.PeriodMonthly {
			augMonthly = c.Key
		}
	}
	require.NotEmpty(t, julMonthly)
	require.NotEmpty(t, augMonthly)
	require.NotEqual(t, julMonthly, augMonthly, "month rollover should produce different keys")
	require.Contains(t, julMonthly, "2026-07")
	require.Contains(t, augMonthly, "2026-08")
}

// TestGate_DisabledTierAllowsPassThrough verifies that a tier with
// enforcement_eligible=false does not block requests even when a $0 tier
// limit exists — the tier dimension is excluded from enforcement.
func TestGate_DisabledTierAllowsPassThrough(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	holder := budget.NewSnapshotHolder()
	holder.Set(&configsync.BudgetTierSnapshot{
		SchemaVersion: 1, Revision: 1, MasterEnabled: true, AccountingEnabled: true,
		ZeroBlockEnabled: true, PositiveEnforcementEnabled: true,
		Timezone: "Asia/Shanghai", WeekStart: "monday",
	})
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	identity := &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "u-1", AmountUSD: -1, WeeklyAmountUSD: -1, DailyAmountUSD: -1,
		TierLimits: []gwmodel.TierLimit{{
			TierID: "tier-1", PeriodType: "monthly", EffectiveLimitUSD: "0",
			Enabled: true, EffectiveAt: now.Add(-time.Hour),
		}},
	}}}
	// enforcement_eligible=false → disabled tier
	tier := budget.TierContext{TierID: "tier-1", TierName: "归档", AccountingEligible: false, EnforcementEligible: false}
	ctx := newBudgetContext(identity, tier)

	outcome, err := budget.NewGate(holder, budget.NewRedisStore(rdb), nil, func() time.Time { return now }).Check(ctx)
	require.NoError(t, err)
	require.NotNil(t, outcome)
	// No tier constraint should have been generated for the disabled tier
	require.False(t, outcome.Plan.TierAccountingEligible)
	require.False(t, outcome.Plan.TierEnforcementEligible)
	_, blocked := budget.GetBlockContext(ctx)
	require.False(t, blocked, "disabled tier should not produce a block")
}
