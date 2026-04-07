//go:build integration

package integration_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/configsync"
	"github.com/example/llm-gateway/internal/gateway/budget"
	gwinfra "github.com/example/llm-gateway/internal/gateway/infra"
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestModelTierBudgetFlow_SnapshotResolveCheckAndSettle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	infra := startIntegrationInfra(t)
	rdb := infra.Redis
	tierID := uuid.MustParse("00bb2e65-9c67-45f7-b061-d297a3944876")
	snapshot := &configsync.BudgetTierSnapshot{
		SchemaVersion: 1, Revision: 7, MasterEnabled: true, AccountingEnabled: true,
		PositiveEnforcementEnabled: true, Timezone: "Asia/Shanghai", WeekStart: "monday",
		GroupTiers: map[string]configsync.BudgetTierGroupSnapshot{
			"premium": {TierID: tierID, TierName: "高级", Enabled: true},
		},
	}
	require.NoError(t, configsync.SaveBudgetTierSnapshot(context.Background(), rdb, snapshot))
	holder := budget.NewSnapshotHolder()
	loader := budget.NewConfigLoader(rdb, holder)
	require.NoError(t, loader.Load(context.Background()))
	tier, ok := budget.NewTierResolver(holder).Resolve("premium")
	require.True(t, ok)

	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	ctx.Set("identity", &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "u-1", AmountUSD: -1, DailyAmountUSD: -1, WeeklyAmountUSD: -1,
		TierLimits: []gwmodel.TierLimit{{
			TierID: tierID.String(), PeriodType: "monthly", EffectiveLimitUSD: "10",
			Enabled: true, EffectiveAt: now.Add(-time.Hour),
		}},
	}}})
	budget.SetTierContext(ctx, tier)
	gate := budget.NewGate(holder, budget.NewRedisStore(rdb), nil, func() time.Time { return now })
	outcome, err := gate.Check(ctx)
	require.NoError(t, err)
	require.NoError(t, gate.Account(context.Background(), outcome.Plan, 250_001).V2Error)

	used, err := rdb.Get(context.Background(),
		budget.TierUsedKey("member", "u-1", tierID.String(), "2026-07")).Int64()
	require.NoError(t, err)
	require.Equal(t, int64(250_001), used)

	// Real Redis hot update: the in-flight context stays fixed while newly
	// resolved requests immediately see the disabled tier as ineligible.
	updated := *snapshot
	updated.Revision = 8
	updated.GroupTiers = map[string]configsync.BudgetTierGroupSnapshot{
		"premium": {TierID: tierID, TierName: "高级", Enabled: false},
	}
	require.NoError(t, configsync.SaveBudgetTierSnapshot(context.Background(), rdb, &updated))
	loader.HandleChangeEvent(gwinfra.ChangeEvent{
		Type: configsync.BudgetTierSnapshotUpdated, SnapshotVersion: 8, Timestamp: time.Now(),
	})
	require.True(t, tier.AccountingEligible, "in-flight tier context must stay immutable")
	reloaded, ok := budget.NewTierResolver(holder).Resolve("premium")
	require.True(t, ok)
	require.False(t, reloaded.AccountingEligible)
}
