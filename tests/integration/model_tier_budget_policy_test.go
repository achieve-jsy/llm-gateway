//go:build integration

package integration_test

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/configsync"
	"github.com/example/llm-gateway/internal/gateway/budget"
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/streaming"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestModelTierBudgetIntegration_ZeroShadowAndFailOpenPolicies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	infra := startIntegrationInfra(t)
	now := time.Date(2026, 7, 11, 15, 0, 0, 0, time.UTC)

	t.Run("total zero is closed independently of tier zero switch", func(t *testing.T) {
		holder := budget.NewSnapshotHolder()
		holder.Set(&configsync.BudgetTierSnapshot{
			SchemaVersion: 1, MasterEnabled: true, AccountingEnabled: true,
			ZeroBlockEnabled: false, Timezone: "Asia/Shanghai", WeekStart: "monday",
		})
		ctx := integrationBudgetContext(&gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
			ScopeType: "member", ScopeID: "u-zero", AmountUSD: 0, DailyAmountUSD: -1, WeeklyAmountUSD: -1,
		}}})
		_, err := budget.NewGate(holder, budget.NewRedisStore(infra.Redis), nil, func() time.Time { return now }).Check(ctx)
		var exceeded *budget.ExceededError
		require.ErrorAs(t, err, &exceeded)
		require.Equal(t, budget.DimensionTotal, exceeded.Primary.Dimension)
	})

	t.Run("tier zero is closed when enabled", func(t *testing.T) {
		holder := budget.NewSnapshotHolder()
		holder.Set(&configsync.BudgetTierSnapshot{
			SchemaVersion: 1, MasterEnabled: true, AccountingEnabled: true,
			ZeroBlockEnabled: true, Timezone: "Asia/Shanghai", WeekStart: "monday",
		})
		ctx := integrationBudgetContext(&gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
			ScopeType: "member", ScopeID: "u-tier-zero", AmountUSD: -1, DailyAmountUSD: -1, WeeklyAmountUSD: -1,
			TierLimits: []gwmodel.TierLimit{{
				TierID: "tier-zero", PeriodType: "monthly", EffectiveLimitUSD: "0",
				Enabled: true, EffectiveAt: now.Add(-time.Hour),
			}},
		}}})
		budget.SetTierContext(ctx, budget.TierContext{
			TierID: "tier-zero", AccountingEligible: true, EnforcementEligible: true,
		})
		_, err := budget.NewGate(holder, budget.NewRedisStore(infra.Redis), nil, func() time.Time { return now }).Check(ctx)
		var exceeded *budget.ExceededError
		require.ErrorAs(t, err, &exceeded)
		require.Equal(t, budget.DimensionTier, exceeded.Primary.Dimension)
	})

	t.Run("shadow records would-block without rejecting", func(t *testing.T) {
		holder := budget.NewSnapshotHolder()
		holder.Set(&configsync.BudgetTierSnapshot{
			SchemaVersion: 1, MasterEnabled: true, AccountingEnabled: true,
			ShadowEnabled: true, PositiveEnforcementEnabled: false,
			Timezone: "Asia/Shanghai", WeekStart: "monday",
		})
		ctx := integrationBudgetContext(&gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
			ScopeType: "member", ScopeID: "u-shadow", AmountUSD: -1, DailyAmountUSD: -1, WeeklyAmountUSD: -1,
			TierLimits: []gwmodel.TierLimit{{
				TierID: "tier-shadow", PeriodType: "monthly", EffectiveLimitUSD: "1",
				Enabled: true, EffectiveAt: now.Add(-time.Hour),
			}},
		}}})
		budget.SetTierContext(ctx, budget.TierContext{
			TierID: "tier-shadow", AccountingEligible: true, EnforcementEligible: true,
		})
		key := budget.TierUsedKey("member", "u-shadow", "tier-shadow", "2026-07")
		require.NoError(t, infra.Redis.Set(context.Background(), key, "1000001", time.Hour).Err())
		_, err := budget.NewGate(holder, budget.NewRedisStore(infra.Redis), nil, func() time.Time { return now }).Check(ctx)
		require.NoError(t, err)
	})

	t.Run("positive budget fails open when Redis disappears", func(t *testing.T) {
		holder := budget.NewSnapshotHolder()
		holder.Set(&configsync.BudgetTierSnapshot{
			SchemaVersion: 1, MasterEnabled: true, AccountingEnabled: true,
			PositiveEnforcementEnabled: true, Timezone: "Asia/Shanghai", WeekStart: "monday",
		})
		ctx := integrationBudgetContext(&gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
			ScopeType: "member", ScopeID: "u-fail-open", AmountUSD: 10, DailyAmountUSD: -1, WeeklyAmountUSD: -1,
		}}})
		require.NoError(t, infra.Redis.Close())
		_, err := budget.NewGate(holder, budget.NewRedisStore(infra.Redis), nil, func() time.Time { return now }).Check(ctx)
		require.NoError(t, err)
	})
}

type integrationSettlementObserver struct {
	gate  *budget.Gate
	plan  *budget.AccountingPlan
	calls atomic.Int32
}

func (*integrationSettlementObserver) OnJSONEvent(map[string]any) {}
func (o *integrationSettlementObserver) OnStreamClose() {
	o.calls.Add(1)
	o.gate.Account(context.Background(), o.plan, 125_000)
}

func TestModelTierBudgetIntegration_StreamDisconnectSettlesExactlyOnce(t *testing.T) {
	infra := startIntegrationInfra(t)
	now := time.Date(2026, 7, 11, 15, 0, 0, 0, time.UTC)
	holder := budget.NewSnapshotHolder()
	holder.Set(&configsync.BudgetTierSnapshot{
		SchemaVersion: 1, MasterEnabled: true, AccountingEnabled: true,
		PositiveEnforcementEnabled: true, Timezone: "Asia/Shanghai", WeekStart: "monday",
	})
	ctx := integrationBudgetContext(&gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "stream-user", AmountUSD: 10, DailyAmountUSD: -1, WeeklyAmountUSD: -1,
	}}})
	gate := budget.NewGate(holder, budget.NewRedisStore(infra.Redis), nil, func() time.Time { return now })
	outcome, err := gate.Check(ctx)
	require.NoError(t, err)
	observer := &integrationSettlementObserver{gate: gate, plan: outcome.Plan}
	stream := streaming.NewObservedSSEStream(
		io.NopCloser(strings.NewReader(`data: {"usage":{"total_tokens":1}}`)),
		observer,
	)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = stream.Close()
		}()
	}
	wg.Wait()
	require.Equal(t, int32(1), observer.calls.Load())
	used, err := infra.Redis.Get(context.Background(), budget.TotalUsedKey("member", "stream-user", "2026-07")).Int64()
	require.NoError(t, err)
	require.Equal(t, int64(125_000), used)
}

func integrationBudgetContext(identity *gwmodel.UserIdentity) *gin.Context {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	ctx.Set("identity", identity)
	return ctx
}
