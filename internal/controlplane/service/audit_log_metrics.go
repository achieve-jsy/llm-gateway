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
	auditLogMetricsOnce        sync.Once
	auditLogWrittenCounter     metric.Int64Counter
	auditLogDroppedCounter     metric.Int64Counter
	auditLogWriteFailedCounter metric.Int64Counter
)

func ensureAuditLogMetrics() {
	auditLogMetricsOnce.Do(func() {
		meter := otel.Meter("llm-gateway/controlplane-audit")
		var err error
		auditLogWrittenCounter, err = meter.Int64Counter("controlplane_audit_log_written_total")
		if err != nil {
			slog.Warn("audit log written metric init failed", "error", err)
		}
		auditLogDroppedCounter, err = meter.Int64Counter("controlplane_audit_log_dropped_total")
		if err != nil {
			slog.Warn("audit log dropped metric init failed", "error", err)
		}
		auditLogWriteFailedCounter, err = meter.Int64Counter("controlplane_audit_log_write_failed_total")
		if err != nil {
			slog.Warn("audit log write-failed metric init failed", "error", err)
		}
	})
}

func recordAuditLogWritten(ctx context.Context, batchSize int) {
	ensureAuditLogMetrics()
	if auditLogWrittenCounter != nil {
		auditLogWrittenCounter.Add(ctx, 1, metric.WithAttributes(attribute.Int("batch_size", batchSize)))
	}
}

func recordAuditLogDropped(ctx context.Context) {
	ensureAuditLogMetrics()
	if auditLogDroppedCounter != nil {
		auditLogDroppedCounter.Add(ctx, 1)
	}
}

func recordAuditLogWriteFailed(ctx context.Context, batchSize int) {
	ensureAuditLogMetrics()
	if auditLogWriteFailedCounter != nil {
		auditLogWriteFailedCounter.Add(ctx, 1, metric.WithAttributes(attribute.Int("batch_size", batchSize)))
	}
}
