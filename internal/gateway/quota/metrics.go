package quota

import (
	"context"
	"log/slog"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	meterName = "llm-gateway/quota"
)

var (
	metricsOnce     sync.Once
	rejectedCounter metric.Int64Counter
	failOpenCounter metric.Int64Counter
)

// ensureMetrics 懒初始化 OTel 计数器（首次调用时自动初始化）。
func ensureMetrics() {
	metricsOnce.Do(func() {
		meter := otel.Meter(meterName)
		var err error
		rejectedCounter, err = meter.Int64Counter(
			"gateway_quota_rejected_total",
			metric.WithDescription("Total number of requests rejected by quota enforcement"),
			metric.WithUnit("1"),
		)
		if err != nil {
			slog.Warn("quota: failed to init rejected counter", "error", err)
		}
		failOpenCounter, err = meter.Int64Counter(
			"gateway_quota_fail_open_total",
			metric.WithDescription("Total number of requests passed due to Redis errors (fail-open)"),
			metric.WithUnit("1"),
		)
		if err != nil {
			slog.Warn("quota: failed to init fail_open counter", "error", err)
		}
	})
}

// RecordRejection 记录一次配额拒绝事件
func RecordRejection(ctx context.Context, layer, gate, subjectType string) {
	ensureMetrics()
	if rejectedCounter == nil {
		return
	}
	rejectedCounter.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("layer", layer),
			attribute.String("gate", gate),
			attribute.String("subject_type", subjectType),
		),
	)
}

// RecordFailOpen 记录一次 Redis fail-open 事件（Redis 不可用时放行请求）。
func RecordFailOpen(ctx context.Context, gate string) {
	ensureMetrics()
	if failOpenCounter == nil {
		return
	}
	failOpenCounter.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("gate", gate),
		),
	)
}
