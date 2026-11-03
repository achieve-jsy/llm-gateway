package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/errormap"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/streaming"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// ChatNative 通过原生协议旁路执行非流式调用。
// body 与返回值都是上游原始字节，网关不做任何协议转换。
//
// 渠道/模型不支持原生格式时返回 core.ErrNativeNotSupported 原样上抛，
// 由调用方用 errors.Is 识别并降级到常规 Chat 路径。
// 调用方负责在传入的 ctx 上设置超时。
func (o *ChatOrchestrator) ChatNative(ctx context.Context, req *core.ChatRequest, body []byte, format core.NativeRelayFormat) ([]byte, error) {
	tracer := otel.Tracer("llm-gateway/orchestrator")

	// 快照式取 router：热更新期间单请求内只用一个 Router 实例，避免半新半旧。
	router := o.routerSnapshot()
	if router == nil || router.ModelCount() == 0 {
		return nil, model.ErrModelNotFound
	}

	ctx, span := tracer.Start(ctx, "upstream.chat_native",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("llm.model", req.Model),
			attribute.Int("llm.request_bytes", len(body)),
			attribute.Bool("llm.native_relay", true),
		),
	)
	defer span.End()

	slog.DebugContext(ctx, "upstream_native_start",
		"model", req.Model,
		"request_bytes", len(body),
		"stream", false,
	)

	start := time.Now()

	// 阶段计时：上游开始前，记录 route 完成时间（与 Chat 一致）
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

	respBody, err := router.RelayNative(ctx, req, body, format)
	elapsed := time.Since(start)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		// 原生格式不可用是预期降级路径：不写 error_message/upstream_status，
		// 免得一次成功降级的请求在 call_logs 里留下一条假错误。
		if errors.Is(err, core.ErrNativeNotSupported) {
			slog.DebugContext(ctx, "upstream_native_unsupported",
				"model", req.Model,
				"error", err.Error(),
			)
			return nil, err
		}
		slog.Error("upstream_native_error",
			append([]any{
				"model", req.Model,
				"duration_ms", elapsed.Milliseconds(),
				"error", err.Error(),
			}, extractUpstreamErrorAttrs(err)...)...,
		)
		if gc := ginContextFrom(ctx); gc != nil {
			gc.Set("error_message", err.Error())
			gc.Set("upstream_status", upstreamStatus(err))
		}
		// native_relay 在 span 起点就设了 true，降级时它会误导排查（看图以为走了原生）。
		// 这里按错误类型改写：哨兵 → 明确标记降级；其他 → 标记原生失败。
		if errors.Is(err, core.ErrNativeNotSupported) {
			span.SetAttributes(
				attribute.Bool("llm.native_relay", false),
				attribute.Bool("llm.native_degraded", true),
			)
		} else {
			span.SetAttributes(attribute.Bool("llm.native_failed", true))
		}
		return nil, errormap.MapProviderError(err)
	}

	span.SetAttributes(attribute.Int("llm.response_bytes", len(respBody)))
	span.SetStatus(codes.Ok, "")

	// 非流式也必须回填 usage：原生响应是上游原始字节，链路上没有任何环节会解析它，
	// 漏掉这步 UsageCollector 会记 UsageMissing，这条路径的请求直接按 0 计费。
	if gc := ginContextFrom(ctx); gc != nil {
		gc.Set("stream", false)
		observeNativeUsage(gc, respBody)
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
		gc.Delete("upstream_status")
	}

	slog.DebugContext(ctx, "upstream_native_done",
		"model", req.Model,
		"duration_ms", elapsed.Milliseconds(),
		"response_bytes", len(respBody),
	)

	return respBody, nil
}

// observeNativeUsage 从非流式原生响应体里提取 usage 写回 gin context。
// 兼容 Chat / Responses / Anthropic 三套命名，与流式路径共用 usageFromMap。
// 响应里没有可识别 usage 时保持静默：不写比写错更好，UsageCollector 会标记缺失。
func observeNativeUsage(gc *gin.Context, body []byte) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return
	}
	usageMap := extractUsageMap(payload)
	if usageMap == nil {
		return
	}
	u := usageFromMap(usageMap)
	if u.PromptTokens == 0 && u.CompletionTokens == 0 && u.TotalTokens == 0 {
		return
	}
	gc.Set("usage", u)
	gc.Set("prompt_tokens", u.PromptTokens)
	gc.Set("completion_tokens", u.CompletionTokens)
}

// ChatNativeStream 通过原生协议旁路执行流式调用，返回上游原始 SSE 流（调用方关闭）。
// 结构与 ChatStream 对齐：同样包 ObservedSSEStream 旁路统计 chunk 与 usage，
// 否则原生旁路的计费会整个漏掉。
func (o *ChatOrchestrator) ChatNativeStream(ctx context.Context, req *core.ChatRequest, body []byte, format core.NativeRelayFormat) (io.ReadCloser, error) {
	tracer := otel.Tracer("llm-gateway/orchestrator")

	router := o.routerSnapshot()
	if router == nil || router.ModelCount() == 0 {
		return nil, model.ErrModelNotFound
	}

	ctx, span := tracer.Start(ctx, "upstream.chat_native_stream",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("llm.model", req.Model),
			attribute.Int("llm.request_bytes", len(body)),
			attribute.Bool("llm.native_relay", true),
			attribute.Bool("llm.stream", true),
		),
	)

	slog.DebugContext(ctx, "upstream_native_stream_start",
		"model", req.Model,
		"request_bytes", len(body),
		"stream", true,
	)

	start := time.Now()

	// 阶段计时：上游开始（与 ChatStream 一致）
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

	rawStream, err := router.RelayNativeStream(ctx, req, body, format)
	if err != nil {
		elapsed := time.Since(start)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.End()
		// native_relay 在 span 起点就设了 true，降级时它会误导排查。
		if errors.Is(err, core.ErrNativeNotSupported) {
			span.SetAttributes(
				attribute.Bool("llm.native_relay", false),
				attribute.Bool("llm.native_degraded", true),
			)
		} else {
			span.SetAttributes(attribute.Bool("llm.native_failed", true))
		}
		// 原生格式不可用：原样上抛供调用方降级，不写 error_message/upstream_status。
		if errors.Is(err, core.ErrNativeNotSupported) {
			slog.DebugContext(ctx, "upstream_native_stream_unsupported",
				"model", req.Model,
				"error", err.Error(),
			)
			return nil, err
		}
		slog.Error("upstream_native_stream_error",
			append([]any{
				"model", req.Model,
				"duration_ms", elapsed.Milliseconds(),
				"error", err.Error(),
			}, extractUpstreamErrorAttrs(err)...)...,
		)
		if gc := ginContextFrom(ctx); gc != nil {
			gc.Set("error_message", err.Error())
			gc.Set("upstream_status", upstreamStatus(err))
		}
		return nil, errormap.MapProviderError(err)
	}

	elapsed := time.Since(start)
	slog.DebugContext(ctx, "upstream_native_stream_connected",
		"model", req.Model,
		"ttfb_ms", elapsed.Milliseconds(),
	)

	span.SetStatus(codes.Ok, "")

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
		// TTFB：上游请求发出到收到响应头
		if ttfb := elapsed.Milliseconds(); ttfb > 0 {
			gc.Set("t_ttfb_ms", int(ttfb))
		}
		usageObserver = newStreamUsageObserver(gc, start)
		gc.Delete("upstream_status")
	}

	// 与 ChatStream 相同的包装：字节原样透传 + OTel chunk 统计 + usage 旁路提取。
	otelObserver := streaming.NewOTelObserver(span)
	observed := streaming.NewObservedSSEStream(rawStream, otelObserver, usageObserver)

	return &cancelOnCloseStream{
		ReadCloser: observed,
		cancel: func() {
			span.End()
		},
	}, nil
}
