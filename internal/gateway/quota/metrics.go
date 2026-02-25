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
	initOnce            sync.Once
	rejectedCounter     metric.Int64Counter
)

// InitMetrics 初始化配额 Prometheus 指标（在 gateway 启动时调用一次）
func InitMetrics() {
	initOnce.Do(func() {
		meter := otel.Meter(meterName)
		var err error
		rejectedCounter, err = meter.Int64Counter(
			"gateway_quota_rejected_total",
			metric.WithDescription("Total number of requests rejected by quota enforcement"),
			metric.WithUnit("1"),
		)
		if err != nil {
			slog.Warn("quota: failed to init metrics", "error", err)
		}
	})
}

// RecordRejection 记录一次配额拒绝事件
func RecordRejection(ctx context.Context, layer, gate, subjectType string) {
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
