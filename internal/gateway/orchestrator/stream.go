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
func (o *ChatOrchestrator) ChatStream(ctx context.Context, req *core.ChatRequest) (io.ReadCloser, error) {
	tracer := otel.Tracer("llm-gateway/orchestrator")

	// 注入 stream_options.include_usage=true 使上游末尾 chunk 返回 usage
	if req.StreamOptions == nil {
		req.StreamOptions = &core.StreamOptions{IncludeUsage: true}
	} else {
		req.StreamOptions.IncludeUsage = true
	}

	// 流式请求超时控制（300s，匹配 Nginx proxy_read_timeout）
	ctx, cancel := context.WithTimeout(ctx, defaultStreamTimeout)

	router := o.routerSnapshot()
	if router == nil || router.ModelCount() == 0 {
		cancel()
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

	// 通过 Router 获取流
	rawStream, err := router.StreamChatCompletion(ctx, req)
	if err != nil {
		elapsed := time.Since(start)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.End()
		cancel()
		slog.Error("upstream_stream_error",
			"model", req.Model,
			"duration_ms", elapsed.Milliseconds(),
			"error", err.Error(),
		)
		return nil, errormap.MapProviderError(err)
	}

	elapsed := time.Since(start)
	slog.DebugContext(ctx, "upstream_stream_connected",
		"model", req.Model,
		"ttfb_ms", elapsed.Milliseconds(),
	)

	span.SetStatus(codes.Ok, "")

	// 将流式标记、路由模型写回 Gin context 供 Logger / 埋点采集器使用。
	// routed_model 取已解析的 req.Model（与非流式 resp.Model 一致），用于价表查询计价。
	var usageObserver *streamUsageObserver
	if gc := ginContextFrom(ctx); gc != nil {
		gc.Set("stream", true)
		if req.Model != "" {
			gc.Set("routed_model", req.Model)
		}
		if name := router.GetProviderName(req.Model); name != "" {
			gc.Set("provider", name)
		}
		usageObserver = newStreamUsageObserver(gc, start)
	}

	// 用 ObservedSSEStream 包装原始流：
	//   - 字节流原样透传给客户端（零拷贝，不影响主链路）
	//   - OTelObserver 旁路统计 chunk 数量，流关闭时写入 span
	//   - streamUsageObserver 旁路提取末尾 usage 与首 token 延迟，流关闭时回写 context
	otelObserver := streaming.NewOTelObserver(span)
	observed := streaming.NewObservedSSEStream(rawStream, otelObserver, usageObserver)

	// cancelOnCloseStream 确保流关闭时同步取消 context + 结束 span
	return &cancelOnCloseStream{
		ReadCloser: observed,
		cancel: func() {
			span.End()
			cancel()
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

	return resp, nil
}
