package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/gateway/budget"
	"github.com/example/llm-gateway/internal/gateway/server"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestBuildBudgetAlertRequests_SuppressesRedisFailOpenPlans(t *testing.T) {
	constraint := budget.Constraint{
		Dimension: budget.DimensionTotal, ScopeType: "member", ScopeID: "u-alert",
	}
	require.Empty(t, server.BuildBudgetAlertRequests(&budget.AccountingPlan{
		RedisCheckSucceeded: true, RedisFailOpen: true, Constraints: []budget.Constraint{constraint},
	}))
	require.Empty(t, server.BuildBudgetAlertRequests(&budget.AccountingPlan{
		RedisCheckSucceeded: false, Constraints: []budget.Constraint{constraint},
	}))
	require.Len(t, server.BuildBudgetAlertRequests(&budget.AccountingPlan{
		RedisCheckSucceeded: true, Constraints: []budget.Constraint{constraint},
	}), 1)
}

func TestNonReservationAlertEvaluationRunsAfterSuccessfulAccount(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	key := "budget:v2:used:member:u-normal-alert:2026-07"
	require.NoError(t, rdb.Set(context.Background(), key, 79, 0).Err())
	plan := &budget.AccountingPlan{
		Generation: "legacy", RedisCheckSucceeded: true,
		Constraints: []budget.Constraint{{
			Dimension: budget.DimensionTotal, ScopeType: "member", ScopeID: "u-normal-alert",
			Key: key, PeriodType: budgetdomain.PeriodMonthly, ResetAt: time.Now().Add(time.Hour),
		}},
	}
	gate := budget.NewGate(nil, budget.NewRedisStore(rdb), nil, time.Now)

	result := gate.Account(context.Background(), plan, 2)
	require.NoError(t, result.V2Error)
	require.Len(t, server.BuildBudgetAlertRequests(plan), 1)
	value, err := rdb.Get(context.Background(), key).Int64()
	require.NoError(t, err)
	require.Equal(t, int64(81), value)
}

func TestBudgetAlertDispatcher_BoundsQueueAndDrainsOnShutdown(t *testing.T) {
	oldMeter := otel.GetMeterProvider()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		otel.SetMeterProvider(oldMeter)
		_ = provider.Shutdown(context.Background())
	})
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	var delivered atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		started <- struct{}{}
		<-release
		delivered.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	ctx, cancel := context.WithCancel(context.Background())
	dispatcher := server.NewBudgetAlertDispatcher(
		ctx,
		server.NewBudgetAlertClient(upstream.Client(), upstream.URL, "token"),
		server.BudgetAlertDispatcherConfig{QueueSize: 1, Workers: 1, RequestTimeout: time.Second},
	)
	request := server.BudgetAlertRequest{ScopeType: "member", ScopeID: "u-1", PeriodType: "daily"}
	require.True(t, dispatcher.Enqueue(request))
	<-started
	require.True(t, dispatcher.Enqueue(request))
	require.False(t, dispatcher.Enqueue(request), "full queue must not spawn an unbounded goroutine")
	var metrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &metrics))
	require.True(t, containsMetric(metrics, "gateway_budget_alert_queue_full_total"))

	close(release)
	cancel()
	waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Second)
	defer waitCancel()
	require.NoError(t, dispatcher.Wait(waitCtx))
	require.Equal(t, int32(2), delivered.Load(), "shutdown must drain accepted alerts")
}

func containsMetric(metrics metricdata.ResourceMetrics, name string) bool {
	for _, scope := range metrics.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name == name {
				return true
			}
		}
	}
	return false
}
