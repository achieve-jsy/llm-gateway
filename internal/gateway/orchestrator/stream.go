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

// ChatStream 执行流式聊天调用
// 返回 io.ReadCloser（原始 SSE 字节流），调用方负责关闭。
// 流被 ObservedSSEStream 包装，旁路解析每个 SSE chunk 并写入 OTel span（chunk 计数）。
// 调用方负责在传入的 ctx 上设置超时，ChatStream 本身不再创建超时 context。
func (o *ChatOrchestrator) ChatStream(ctx context.Context, req *core.ChatRequest) (io.ReadCloser, error) {
	tracer := otel.Tracer("llm-gateway/orchestrator")

	// 注入 stream_options.include_usage=true 使上游末尾 chunk 返回 usage
	if req.StreamOptions == nil {
		req.StreamOptions = &core.StreamOptions{IncludeUsage: true}
	} else {
		req.StreamOptions.IncludeUsage = true
	}

	router := o.routerSnapshot()
	if router == nil || router.ModelCount() == 0 {
		return nil, model.ErrModelNotFound
	}

	// 创建上游流式调用 span（在流关闭时由 OTelObserver 写入 chunk 计数）
	ctx, span := tracer.Start(ctx, "upstream.chat_stream",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("llm.model", req.Model),
			attribute.Int("llm.messages_count", len(req.Messages)),
			attribute.Bool("llm.stream", true),
		),
	)

	slog.DebugContext(ctx, "upstream_stream_start",
		"model", req.Model,
		"messages_count", len(req.Messages),
		"stream", true,
	)

	start := time.Now()

	// 阶段计时：上游开始
	if gc := ginContextFrom(ctx); gc != nil {
		if reqStart, ok := gc.Get("start_time"); ok {
			if t, ok := reqStart.(time.Time); ok && !t.IsZero() {
				elapsed := time.Since(t).Milliseconds()
				if elapsed >= 0 && elapsed <= 32767 {
					ms := int16(elapsed)
					gc.Set("t_route_ms", ms)
					gc.Set("t_upstream_ms", ms)
				}
			}
		}
	}

	// 通过 Router 获取流
	rawStream, err := router.StreamChatCompletion(ctx, req)
	if err != nil {
		elapsed := time.Since(start)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.End()
		slog.Error("upstream_stream_error",
			"model", req.Model,
			"duration_ms", elapsed.Milliseconds(),
			"error", err.Error(),
		)
		if gc := ginContextFrom(ctx); gc != nil {
			gc.Set("error_message", err.Error())
		}
		return nil, errormap.MapProviderError(err)
	}

	elapsed := time.Since(start)
	slog.DebugContext(ctx, "upstream_stream_connected",
		"model", req.Model,
		"ttfb_ms", elapsed.Milliseconds(),
	)

	span.SetStatus(codes.Ok, "")

	// 将流式标记、路由模型写回 Gin context 供 Logger / 埋点采集器使用。
	// routed_model 仅在组调度未写入时才填（直接请求 model 名的场景）；
	// 组调度路径已由 GroupAwareDispatcher 写入 models.name，此处不覆盖。
	var usageObserver *streamUsageObserver
	if gc := ginContextFrom(ctx); gc != nil {
		gc.Set("stream", true)
		if req.Model != "" {
			if _, exists := gc.Get("routed_model"); !exists {
				gc.Set("routed_model", req.Model)
			}
		}
		if _, exists := gc.Get("provider"); !exists {
			if name := router.GetProviderName(req.Model); name != "" {
				gc.Set("provider", name)
			}
		}
		// TTFB：从上游请求发出到收到响应头（HTTP 200）的耗时
		if ttfb := elapsed.Milliseconds(); ttfb > 0 {
			gc.Set("t_ttfb_ms", int(ttfb))
		}
		usageObserver = newStreamUsageObserver(gc, start)
	}

	// 用 ObservedSSEStream 包装原始流：
	//   - 字节流原样透传给客户端（零拷贝，不影响主链路）
	//   - OTelObserver 旁路统计 chunk 数量，流关闭时写入 span
	//   - streamUsageObserver 旁路提取末尾 usage 与首 token 延迟，流关闭时回写 context
	otelObserver := streaming.NewOTelObserver(span)
	observed := streaming.NewObservedSSEStream(rawStream, otelObserver, usageObserver)

	// cancelOnCloseStream 确保流关闭时结束 OTel span（ctx/cancel 由调用方持有）
	return &cancelOnCloseStream{
		ReadCloser: observed,
		cancel: func() {
			span.End()
		},
	}, nil
}

// cancelOnCloseStream wraps an io.ReadCloser and cancels the associated
// context when Close() is called. This ensures the upstream connection
// is released promptly when the client disconnects or the handler exits.
type cancelOnCloseStream struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (s *cancelOnCloseStream) Close() error {
	s.cancel()
	return s.ReadCloser.Close()
}

// EmbeddingRequest executes an embeddings request through the Router.
func (o *ChatOrchestrator) Embeddings(ctx context.Context, req *core.EmbeddingRequest) (*core.EmbeddingResponse, error) {
	router := o.routerSnapshot()
	if router == nil || router.ModelCount() == 0 {
		return nil, model.ErrModelNotFound
	}

	slog.DebugContext(ctx, "upstream_embedding_start", "model", req.Model)
	start := time.Now()

	resp, err := router.Embeddings(ctx, req)
	if err != nil {
		slog.Error("upstream_embedding_error",
			"model", req.Model,
			"duration_ms", time.Since(start).Milliseconds(),
			"error", err.Error(),
		)
		return nil, errormap.MapProviderError(err)
	}

	slog.DebugContext(ctx, "upstream_embedding_done",
		"model", req.Model,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	if gc := ginContextFrom(ctx); gc != nil {
		gc.Set("stream", false)
		gc.Set("usage", resp.Usage)
		gc.Set("resp_object", resp)
	}

	return resp, nil
}
