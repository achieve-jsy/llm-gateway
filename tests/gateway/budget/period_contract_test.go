package budget_test

import (
	"strings"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/budgetperiod"
	"github.com/example/llm-gateway/internal/configsync"
	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/gateway/budget"
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/stretchr/testify/require"
)

func TestAccountingPlan_PeriodKeysMatchSharedContract(t *testing.T) {
	now := time.Date(2026, 7, 12, 16, 30, 0, 0, time.UTC)
	config := budgetperiod.Config{Timezone: "Asia/Shanghai", WeekStart: "monday"}
	identity := &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "u-1", AmountUSD: 100, WeeklyAmountUSD: 20, DailyAmountUSD: 5,
	}}}
	plan, err := budget.BuildAccountingPlan(identity, budget.TierContext{}, now, &configsync.BudgetTierSnapshot{
		SchemaVersion: 1, Timezone: config.Timezone, WeekStart: config.WeekStart,
	})
	require.NoError(t, err)
	require.Len(t, plan.Constraints, 3)
	for _, constraint := range plan.Constraints {
		window, resolveErr := budgetperiod.Resolve(now, constraint.PeriodType, config)
		require.NoError(t, resolveErr)
		require.Equal(t, window.Key, constraint.Period)
		require.True(t, strings.HasSuffix(constraint.Key, ":"+window.Key))
		require.Equal(t, window.ResetAt, constraint.ResetAt)
	}
}

func BenchmarkBuildAccountingPlan(b *testing.B) {
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	identity := &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "u-1", AmountUSD: 100, WeeklyAmountUSD: 20, DailyAmountUSD: 5,
		TierLimits: []gwmodel.TierLimit{{
			TierID: "tier-1", PeriodType: string(budgetdomain.PeriodMonthly),
			EffectiveLimitUSD: "25.000000", Enabled: true, EffectiveAt: now.Add(-time.Hour),
		}},
	}}}
	snapshot := &configsync.BudgetTierSnapshot{
		SchemaVersion: 1, MasterEnabled: true, AccountingEnabled: true,
		Timezone: "Asia/Shanghai", WeekStart: "monday",
	}
	tier := budget.TierContext{TierID: "tier-1", AccountingEligible: true, EnforcementEligible: true}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := budget.BuildAccountingPlan(identity, tier, now, snapshot); err != nil {
			b.Fatal(err)
		}
	}
}
