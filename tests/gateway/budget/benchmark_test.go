package budget_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/configsync"
	"github.com/example/llm-gateway/internal/gateway/budget"
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/redis/go-redis/v9"
)

func BenchmarkGateCheck_TwoScopesThreePeriods(b *testing.B) {
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Cleanup(func() { slog.SetDefault(previousLogger) })
	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	identity := &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{
		{
			ScopeType: "member", ScopeID: "u-1", AmountUSD: 1000, DailyAmountUSD: 100, WeeklyAmountUSD: 500,
			TierLimits: []gwmodel.TierLimit{
				{TierID: "tier-high", PeriodType: "daily", EffectiveLimitUSD: "50", Enabled: true, EffectiveAt: now.Add(-time.Hour)},
				{TierID: "tier-high", PeriodType: "weekly", EffectiveLimitUSD: "250", Enabled: true, EffectiveAt: now.Add(-time.Hour)},
				{TierID: "tier-high", PeriodType: "monthly", EffectiveLimitUSD: "500", Enabled: true, EffectiveAt: now.Add(-time.Hour)},
			},
		},
		{
			ScopeType: "dept_daily", ScopeID: "dept-1", AmountUSD: 5000, DailyAmountUSD: 500, WeeklyAmountUSD: 2500,
			TierLimits: []gwmodel.TierLimit{
				{TierID: "tier-high", PeriodType: "daily", EffectiveLimitUSD: "250", Enabled: true, EffectiveAt: now.Add(-time.Hour)},
				{TierID: "tier-high", PeriodType: "weekly", EffectiveLimitUSD: "1250", Enabled: true, EffectiveAt: now.Add(-time.Hour)},
				{TierID: "tier-high", PeriodType: "monthly", EffectiveLimitUSD: "2500", Enabled: true, EffectiveAt: now.Add(-time.Hour)},
			},
		},
	}}
	tier := budget.TierContext{TierID: "tier-high", TierName: "高级", GroupName: "premium", AccountingEligible: true, EnforcementEligible: true}

	for _, mode := range []struct {
		name     string
		master   bool
		shadow   bool
		enforced bool
	}{
		{name: "disabled"},
		{name: "shadow", master: true, shadow: true},
		{name: "enforced", master: true, enforced: true},
	} {
		b.Run(mode.name, func(b *testing.B) {
			mr := miniredis.RunT(b)
			rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
			b.Cleanup(func() { _ = rdb.Close() })
			holder := budget.NewSnapshotHolder()
			holder.Set(&configsync.BudgetTierSnapshot{
				SchemaVersion: 1, Revision: 1, MasterEnabled: mode.master,
				AccountingEnabled: mode.master, ShadowEnabled: mode.shadow,
				PositiveEnforcementEnabled: mode.enforced,
				V2TotalEnforcementEnabled:  mode.master,
				Timezone:                   "Asia/Shanghai", WeekStart: "monday",
			})
			gate := budget.NewGate(holder, budget.NewRedisStore(rdb), nil, func() time.Time { return now })
			ctx := newBudgetContext(identity, tier)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := gate.Check(ctx); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			_ = rdb.FlushDB(context.Background()).Err()
		})
	}
}
