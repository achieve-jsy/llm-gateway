package orchestrator

import (
	"context"
	"log/slog"
	"time"

	"github.com/example/llm-gateway/internal/gateway/errormap"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/normalizer"
	"github.com/example/llm-gateway/internal/gateway/provider"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	// 默认超时时间 30 秒
	defaultTimeout = 30 * time.Second
)

// ChatOrchestrator 聊天编排器，协调 Registry、Provider、Normalizer
type ChatOrchestrator struct {
	registry *provider.Registry
}

// NewChatOrchestrator 创建编排器实例
func NewChatOrchestrator(registry *provider.Registry) *ChatOrchestrator {
	return &ChatOrchestrator{
		registry: registry,
	}
}

// Chat 执行普通聊天调用
// 校验模型可用性 → 解析 Provider → 调用 Chat → 归一化响应
func (o *ChatOrchestrator) Chat(ctx context.Context, req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	tracer := otel.Tracer("llm-gateway/orchestrator")

	// 超时控制
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	// 从 Registry 解析 Provider
	p, err := o.registry.Resolve(req.Model)
	if err != nil {
		return nil, model.ErrModelNotFound
	}

	// 创建上游调用 span
	ctx, span := tracer.Start(ctx, "upstream.chat",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("llm.provider", p.Name()),
			attribute.String("llm.model", req.Model),
			attribute.Int("llm.messages_count", len(req.Messages)),
		),
	)
	defer span.End()

	// 调用 Provider
	resp, err := p.Chat(ctx, req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		slog.Error("provider chat error", "provider", p.Name(), "model", req.Model, "error", err.Error())
		// 经 errormap 转换为 UnifiedError
		return nil, errormap.MapProviderError(err)
	}

	// 记录 token 用量
	if resp.Usage != nil {
		span.SetAttributes(
			attribute.Int("llm.usage.prompt_tokens", resp.Usage.PromptTokens),
			attribute.Int("llm.usage.completion_tokens", resp.Usage.CompletionTokens),
			attribute.Int("llm.usage.total_tokens", resp.Usage.TotalTokens),
		)
	}
	span.SetStatus(codes.Ok, "")

	// 归一化响应
	resp = normalizer.NormalizeResponse(resp)
	return resp, nil
}
