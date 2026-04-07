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
