package infra

import (
	"context"
	"log/slog"

	"github.com/example/llm-gateway/internal/gateway/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
)

// InitTracer 初始化 OpenTelemetry Tracer（T008）
func InitTracer(cfg config.OTLPConfig) (func(), error) {
	if !cfg.Enabled {
		slog.Info("OTLP tracing disabled")
		return func() {}, nil
	}

	ctx := context.Background()

	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName("llm-gateway"),
		semconv.ServiceVersion("0.1.0"),
	)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	shutdown := func() {
		_ = tp.Shutdown(context.Background())
	}

	slog.Info("OTLP tracing initialized", "endpoint", cfg.Endpoint)
	return shutdown, nil
}
