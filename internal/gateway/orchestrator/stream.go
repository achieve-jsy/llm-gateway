package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/errormap"
	"github.com/example/llm-gateway/internal/gateway/middleware"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/streaming"
	"github.com/gin-gonic/gin"
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

	gc := ginContextFrom(ctx)
	if gc != nil {
		gc.Set(string(middleware.ModelKey), req.Model)
		gc.Set(string(middleware.IsStreamKey), true)
		gc.Set(string(middleware.MessagesKey), req.Messages)
	}

	// 用 ObservedSSEStream 包装原始流：
	//   - 字节流原样透传给客户端（零拷贝，不影响主链路）
	//   - OTelObserver 旁路统计 chunk 数量，流关闭时写入 span
	otelObserver := streaming.NewOTelObserver(span)
	observed := streaming.NewObservedSSEStream(rawStream, otelObserver)

	// cancelOnCloseStream 确保流关闭时同步取消 context + 结束 span
	cancelClose := &cancelOnCloseStream{
		ReadCloser: observed,
		cancel: func() {
			span.End()
			cancel()
		},
	}

	// usageTrackingReader 捕获流经的 SSE 数据，在 Close() 时解析最后一个
	// SSE chunk 提取 token usage，注入 gin.Context 供 UsageCollector 收集。
	return newUsageTrackingReader(cancelClose, gc), nil
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

// usageTrackingReader wraps an io.ReadCloser and captures all SSE data passing
// through Read calls. On Close(), it scans the buffered data for the final
// SSE chunk containing token usage (OpenAI format) and injects the values
// into gin.Context for the UsageCollector middleware.
// It also accumulates response content from SSE delta chunks for call_payloads.
type usageTrackingReader struct {
	io.ReadCloser
	ginCtx          *gin.Context
	buf             bytes.Buffer
	parser          *StreamUsageParser
	responseBuilder strings.Builder
}

// sseDeltaContent is a minimal parse target for extracting delta.content
// from streaming SSE chunks.
type sseDeltaContent struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

func newUsageTrackingReader(inner io.ReadCloser, ginCtx *gin.Context) *usageTrackingReader {
	return &usageTrackingReader{
		ReadCloser: inner,
		ginCtx:     ginCtx,
		parser:     NewStreamUsageParser(),
	}
}

func (r *usageTrackingReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if n > 0 {
		r.buf.Write(p[:n])
		r.accumulateResponseContent(p[:n])
	}
	return n, err
}

// accumulateResponseContent parses SSE data chunks for delta.content fields
// and appends them to the response builder for call_payloads.
func (r *usageTrackingReader) accumulateResponseContent(data []byte) {
	const dataPrefix = "data: "
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, dataPrefix) {
			continue
		}
		payload := strings.TrimPrefix(trimmed, dataPrefix)
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var chunk sseDeltaContent
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			r.responseBuilder.WriteString(chunk.Choices[0].Delta.Content)
		}
	}
}

func (r *usageTrackingReader) Close() error {
	r.extractUsage()
	// 在关闭时存储累积的响应文本供 call_payloads 收集
	if r.ginCtx != nil && r.responseBuilder.Len() > 0 {
		r.ginCtx.Set(string(middleware.ResponseTextKey), r.responseBuilder.String())
	}
	return r.ReadCloser.Close()
}

func (r *usageTrackingReader) extractUsage() {
	if r.ginCtx == nil {
		return
	}
	data := r.buf.Bytes()
	if len(data) == 0 {
		return
	}
	usage := r.extractUsageFromSSE(data)
	if usage == nil || usage.UsageMissing {
		return
	}
	r.ginCtx.Set(string(middleware.PromptTokensKey), usage.PromptTokens)
	r.ginCtx.Set(string(middleware.CompletionTokensKey), usage.CompletionTokens)
	r.ginCtx.Set(string(middleware.ReasoningTokensKey), usage.ReasoningTokens)
	r.ginCtx.Set(string(middleware.CachedTokensKey), usage.CachedTokens)
}

// extractUsageFromSSE walks the buffered SSE data backwards to find the last
// non-empty JSON data payload and attempts to parse OpenAI-format token usage from it.
// SSE format: "data: {...}\n\n" with a final "data: [DONE]\n\n" sentinel.
func (r *usageTrackingReader) extractUsageFromSSE(data []byte) *StreamUsage {
	const dataPrefix = "data: "

	lines := strings.Split(string(data), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, dataPrefix) {
			continue
		}
		payload := strings.TrimPrefix(line, dataPrefix)
		if payload == "" || payload == "[DONE]" {
			continue
		}
		if usage := r.parser.ParseOpenAIChunk([]byte(payload)); usage != nil {
			return usage
		}
	}
	return nil
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
