package service

import (
	"context"
	"log/slog"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	controlPlaneBudgetMetricsOnce sync.Once
	configSyncCounter             metric.Int64Counter
	expansionApplyCounter         metric.Int64Counter
)

func ensureControlPlaneBudgetMetrics() {
	controlPlaneBudgetMetricsOnce.Do(func() {
		meter := otel.Meter("llm-gateway/controlplane-budget")
		var err error
		configSyncCounter, err = meter.Int64Counter("controlplane_budget_config_sync_total")
		if err != nil {
			slog.Warn("budget config sync metric init failed", "error", err)
		}
		expansionApplyCounter, err = meter.Int64Counter("controlplane_expansion_apply_total")
		if err != nil {
			slog.Warn("expansion apply metric init failed", "error", err)
		}
	})
}

func recordBudgetConfigSync(ctx context.Context, result string) {
	ensureControlPlaneBudgetMetrics()
	if configSyncCounter != nil {
		configSyncCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("result", result)))
	}
}

func recordExpansionApply(ctx context.Context, dimension, duration, result string) {
	ensureControlPlaneBudgetMetrics()
	if expansionApplyCounter != nil {
		expansionApplyCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("dimension", dimension),
			attribute.String("duration", duration),
			attribute.String("result", result),
		))
	}
}
