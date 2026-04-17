package telemetry_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/configsync"
	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/example/llm-gateway/internal/gateway/middleware"
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestBudgetMetricsAndTraceContract(t *testing.T) {
	oldMeter := otel.GetMeterProvider()
	oldTracer := otel.GetTracerProvider()
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	spanExporter := tracetest.NewInMemoryExporter()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(spanExporter))
	otel.SetMeterProvider(meterProvider)
	otel.SetTracerProvider(tracerProvider)
	t.Cleanup(func() {
		otel.SetMeterProvider(oldMeter)
		otel.SetTracerProvider(oldTracer)
		_ = meterProvider.Shutdown(context.Background())
		_ = tracerProvider.Shutdown(context.Background())
	})

	now := time.Date(2026, 7, 11, 16, 0, 0, 0, time.UTC)
	holder := budget.NewSnapshotHolder()
	holder.Set(&configsync.BudgetTierSnapshot{
		SchemaVersion: 1, MasterEnabled: true, AccountingEnabled: true,
		ZeroBlockEnabled: true, Timezone: "Asia/Shanghai", WeekStart: "monday",
	})
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Set("identity", &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "u-1", AmountUSD: -1, WeeklyAmountUSD: -1, DailyAmountUSD: -1,
		TierLimits: []gwmodel.TierLimit{{
			TierID: "tier-1", PeriodType: "monthly", EffectiveLimitUSD: "0",
			Enabled: true, EffectiveAt: now.Add(-time.Hour),
		}},
	}}})
	budget.SetTierContext(ctx, budget.TierContext{
		TierID: "tier-1", AccountingEligible: true, EnforcementEligible: true,
	})
	legacy := budget.LegacyTotalFuncs{CheckFunc: func(context.Context, *budget.AccountingPlan) (*budget.Constraint, error) {
		return nil, errors.New("legacy redis unavailable")
	}}
	_, err := budget.NewGate(holder, nil, legacy, func() time.Time { return now }).Check(ctx)
	require.Error(t, err)

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	shadowHolder := budget.NewSnapshotHolder()
	shadowHolder.Set(&configsync.BudgetTierSnapshot{
		SchemaVersion: 1, MasterEnabled: true, AccountingEnabled: true, ShadowEnabled: true,
		Timezone: "Asia/Shanghai", WeekStart: "monday",
	})
	shadowCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	shadowCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	shadowCtx.Set("identity", &gwmodel.UserIdentity{BudgetChain: []gwmodel.BudgetScope{{
		ScopeType: "member", ScopeID: "u-1", AmountUSD: -1, WeeklyAmountUSD: -1, DailyAmountUSD: -1,
		TierLimits: []gwmodel.TierLimit{{
			TierID: "tier-1", PeriodType: "monthly", EffectiveLimitUSD: "1",
			Enabled: true, EffectiveAt: now.Add(-time.Hour),
		}},
	}}})
	budget.SetTierContext(shadowCtx, budget.TierContext{
		TierID: "tier-1", AccountingEligible: true, EnforcementEligible: true,
	})
	require.NoError(t, rdb.Set(context.Background(),
		budget.TierUsedKey("member", "u-1", "tier-1", "2026-07"), "1000000", 0).Err())
	_, err = budget.NewGate(shadowHolder, budget.NewRedisStore(rdb), nil, func() time.Time { return now }).Check(shadowCtx)
	require.NoError(t, err)
	notReadyCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	notReadyCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	_, err = budget.NewGate(nil, nil, nil, func() time.Time { return now }).Check(notReadyCtx)
	require.NoError(t, err)
	admissionCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	admissionCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	admissionCtx.Set("request_id", "rebuild-rejected")
	require.NoError(t, rdb.Set(context.Background(), budget.RebuildLockKey, "owner", time.Minute).Err())
	_, err = budget.NewGate(shadowHolder, budget.NewRedisStore(rdb), nil, func() time.Time { return now }).Check(admissionCtx)
	require.Error(t, err)

	var metrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &metrics))
	found := map[string]bool{}
	expectedLabels := map[string]map[string]struct{}{
		"gateway_budget_check_total": {
			"dimension": {}, "result": {}, "period": {}, "scope_type": {},
		},
		"gateway_budget_tier_reject_total": {
			"tier_id": {}, "scope_type": {}, "period": {},
		},
		"gateway_budget_fail_open_total": {
			"dimension": {},
		},
		"gateway_budget_shadow_would_block_total": {
			"dimension": {}, "tier_id": {},
		},
		"gateway_budget_snapshot_not_ready_total": {},
		"gateway_budget_admission_reject_total": {
			"reason": {},
		},
	}
	for _, scope := range metrics.ScopeMetrics {
		for _, item := range scope.Metrics {
			found[item.Name] = true
			if want, tracked := expectedLabels[item.Name]; tracked {
				sum, ok := item.Data.(metricdata.Sum[int64])
				require.True(t, ok)
				for _, point := range sum.DataPoints {
					got := make(map[string]struct{})
					for _, attr := range point.Attributes.ToSlice() {
						got[string(attr.Key)] = struct{}{}
						require.NotEqual(t, "scope_id", string(attr.Key))
					}
					require.Equal(t, want, got, item.Name)
				}
			}
		}
	}
	require.True(t, found["gateway_budget_check_total"])
	require.True(t, found["gateway_budget_tier_reject_total"])
	require.True(t, found["gateway_budget_fail_open_total"])
	require.True(t, found["gateway_budget_shadow_would_block_total"])
	require.True(t, found["gateway_budget_snapshot_not_ready_total"])
	require.True(t, found["gateway_budget_admission_reject_total"])

	router := gin.New()
	router.Use(middleware.Tracing())
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		budget.SetTierContext(c, budget.TierContext{TierID: "tier-1"})
		c.Set(budget.CtxKeyCheckResult, "block")
		c.Set(budget.CtxKeyBlockReason, "tier_budget_exceeded")
		c.Set(budget.CtxKeyShadowMode, false)
		c.Status(http.StatusPaymentRequired)
	})
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	spans := spanExporter.GetSpans()
	require.Len(t, spans, 1)
	attributes := make(map[string]any, len(spans[0].Attributes))
	for _, attr := range spans[0].Attributes {
		attributes[string(attr.Key)] = attr.Value.AsInterface()
	}
	require.Equal(t, "tier-1", attributes["llm.budget_tier_id"])
	require.Equal(t, "block", attributes["budget.check_result"])
	require.Equal(t, "tier_budget_exceeded", attributes["budget.block_reason"])
	require.Equal(t, false, attributes["budget.shadow_enabled"])
	require.Equal(t, "Error", spans[0].Status.Code.String())
}
