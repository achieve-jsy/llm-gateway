package infra

import (
	"context"
	"log/slog"
	"time"

	"github.com/example/llm-gateway/internal/llmclient"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// NewLLMClientHooks 返回接入到 LLMClient 的可观测性 Hooks。
//
// OnRequestStart: 在每次上游 HTTP 请求发出前记录 DEBUG 日志，并将 provider/model 附加到 OTel span。
// OnRequestEnd:   在每次上游 HTTP 请求完成后记录日志（INFO 级别），包含耗时、状态码、重试次数等。
func NewLLMClientHooks() llmclient.Hooks {
	return llmclient.Hooks{
		OnRequestStart: func(ctx context.Context, info llmclient.RequestInfo) context.Context {
			slog.DebugContext(ctx, "llm_upstream_start",
				"provider", info.Provider,
				"model", info.Model,
				"endpoint", info.Endpoint,
				"method", info.Method,
				"stream", info.Stream,
			)

			// 将 provider 信息附加到当前 OTel span
			span := trace.SpanFromContext(ctx)
			if span.IsRecording() {
				span.SetAttributes(
					attribute.String("llm.provider", info.Provider),
					attribute.String("llm.upstream_model", info.Model),
					attribute.String("llm.endpoint", info.Endpoint),
				)
			}

			// 将 start time 存入 context 供 OnRequestEnd 计算精确耗时
			return context.WithValue(ctx, hookStartTimeKey{}, time.Now())
		},

		OnRequestEnd: func(ctx context.Context, info llmclient.ResponseInfo) {
			// 尝试从 context 取出 start time（OnRequestStart 存入）
			var durationMs int64
			if t, ok := ctx.Value(hookStartTimeKey{}).(time.Time); ok {
				durationMs = time.Since(t).Milliseconds()
			} else {
				durationMs = info.Duration.Milliseconds()
			}

			attrs := []any{
				"provider", info.Provider,
				"model", info.Model,
				"endpoint", info.Endpoint,
				"status_code", info.StatusCode,
				"duration_ms", durationMs,
				"stream", info.Stream,
			}

			if info.Error != nil {
				attrs = append(attrs, "error", info.Error.Error())
				slog.WarnContext(ctx, "llm_upstream_error", attrs...)
			} else {
				slog.InfoContext(ctx, "llm_upstream_done", attrs...)
			}

			// 将 provider 回写到 OTel span
			span := trace.SpanFromContext(ctx)
			if span.IsRecording() {
				span.SetAttributes(
					attribute.String("llm.provider", info.Provider),
					attribute.Int("llm.upstream_status_code", info.StatusCode),
					attribute.Int64("llm.upstream_duration_ms", durationMs),
				)
			}
		},
	}
}

type hookStartTimeKey struct{}
