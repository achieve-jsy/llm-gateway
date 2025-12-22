package orchestrator

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/errormap"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/streaming"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Responses executes a Responses API request through the Router.
func (o *ChatOrchestrator) Responses(ctx context.Context, req *core.ResponsesRequest) (*core.ResponsesResponse, error) {
	tracer := otel.Tracer("llm-gateway/orchestrator")

	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	router := o.routerSnapshot()
	if router == nil || router.ModelCount() == 0 {
		return nil, model.ErrModelNotFound
	}

	ctx, span := tracer.Start(ctx, "upstream.responses",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("llm.model", req.Model),
			attribute.Bool("llm.responses", true),
		),
	)
	defer span.End()

	slog.DebugContext(ctx, "upstream_responses_start",
		"model", req.Model,
		"stream", false,
	)

	start := time.Now()
	resp, err := router.Responses(ctx, req)
	elapsed := time.Since(start)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		slog.Error("upstream_responses_error",
			"model", req.Model,
			"duration_ms", elapsed.Milliseconds(),
			"error", err.Error(),
		)
		return nil, errormap.MapProviderError(err)
	}

	if resp.Usage != nil {
		span.SetAttributes(
			attribute.Int("llm.usage.input_tokens", resp.Usage.InputTokens),
			attribute.Int("llm.usage.output_tokens", resp.Usage.OutputTokens),
			attribute.Int("llm.usage.total_tokens", resp.Usage.TotalTokens),
		)
		if gc := ginContextFrom(ctx); gc != nil {
			gc.Set("prompt_tokens", resp.Usage.InputTokens)
			gc.Set("completion_tokens", resp.Usage.OutputTokens)
			gc.Set("stream", false)
		}
	}
	span.SetStatus(codes.Ok, "")

	slog.DebugContext(ctx, "upstream_responses_done",
		"model", req.Model,
		"duration_ms", elapsed.Milliseconds(),
	)

	return resp, nil
}

// ResponsesStream executes a streaming Responses API request through the Router.
func (o *ChatOrchestrator) ResponsesStream(ctx context.Context, req *core.ResponsesRequest) (io.ReadCloser, error) {
	tracer := otel.Tracer("llm-gateway/orchestrator")

	ctx, cancel := context.WithTimeout(ctx, defaultStreamTimeout)

	router := o.routerSnapshot()
	if router == nil || router.ModelCount() == 0 {
		cancel()
		return nil, model.ErrModelNotFound
	}

	ctx, span := tracer.Start(ctx, "upstream.responses_stream",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("llm.model", req.Model),
			attribute.Bool("llm.responses", true),
			attribute.Bool("llm.stream", true),
		),
	)

	slog.DebugContext(ctx, "upstream_responses_stream_start",
		"model", req.Model,
	)

	start := time.Now()
	rawStream, err := router.StreamResponses(ctx, req)
	if err != nil {
		elapsed := time.Since(start)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.End()
		cancel()
		slog.Error("upstream_responses_stream_error",
			"model", req.Model,
			"duration_ms", elapsed.Milliseconds(),
			"error", err.Error(),
		)
		return nil, errormap.MapProviderError(err)
	}

	elapsed := time.Since(start)
	slog.DebugContext(ctx, "upstream_responses_stream_connected",
		"model", req.Model,
		"ttfb_ms", elapsed.Milliseconds(),
	)

	span.SetStatus(codes.Ok, "")

	if gc := ginContextFrom(ctx); gc != nil {
		gc.Set("stream", true)
	}

	otelObserver := streaming.NewOTelObserver(span)
	observed := streaming.NewObservedSSEStream(rawStream, otelObserver)

	return &cancelOnCloseStream{
		ReadCloser: observed,
		cancel: func() {
			span.End()
			cancel()
		},
	}, nil
}