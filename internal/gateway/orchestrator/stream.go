package orchestrator

import (
	"context"

	"github.com/example/llm-gateway/internal/gateway/errormap"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/normalizer"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// ChatStream 执行流式聊天调用
// 校验模型可用性 → 解析 Provider → 调用 ChatStream → 转发 channel（带归一化）
func (o *ChatOrchestrator) ChatStream(ctx context.Context, req *model.ChatCompletionRequest) (<-chan model.StreamChunk, error) {
	tracer := otel.Tracer("llm-gateway/orchestrator")

	// 流式请求超时控制（300s，匹配 Nginx proxy_read_timeout）
	// 从请求 ctx 派生，客户端断开时自动取消上游连接
	ctx, cancel := context.WithTimeout(ctx, defaultStreamTimeout)

	// 从 Registry 解析 Provider
	p, err := o.registry.Resolve(req.Model)
	if err != nil {
		cancel()
		return nil, model.ErrModelNotFound
	}

	// 创建上游流式调用 span
	ctx, span := tracer.Start(ctx, "upstream.chat_stream",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("llm.provider", p.Name()),
			attribute.String("llm.model", req.Model),
			attribute.Int("llm.messages_count", len(req.Messages)),
			attribute.Bool("llm.stream", true),
		),
	)

	// 调用 Provider 获取源 channel
	srcCh, err := p.ChatStream(ctx, req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.End()
		cancel()
		return nil, errormap.MapProviderError(err)
	}

	// 创建输出 channel，启动转发 goroutine（span 在 goroutine 结束时关闭）
	outCh := make(chan model.StreamChunk, 32)
	go streamPipeline(ctx, cancel, srcCh, outCh, span)

	return outCh, nil
}

// streamPipeline 流式管道：从 provider channel 转发到输出 channel（带归一化处理）
func streamPipeline(ctx context.Context, cancel context.CancelFunc, src <-chan model.StreamChunk, out chan<- model.StreamChunk, span trace.Span) {
	defer close(out)
	defer cancel()
	defer span.End()

	var chunkCount int

	sn := normalizer.NewStreamNormalizer()

	for chunk := range src {
		// 错误直接转发
		if chunk.Error != nil {
			span.RecordError(chunk.Error)
			span.SetStatus(codes.Error, chunk.Error.Error())
			out <- model.StreamChunk{Error: errormap.MapProviderError(chunk.Error)}
			return
		}

		// 完成标记直接转发
		if chunk.Done {
			span.SetAttributes(attribute.Int("llm.stream.chunk_count", chunkCount))
			span.SetStatus(codes.Ok, "")
			out <- model.StreamChunk{Done: true}
			return
		}

		chunkCount++

		// 归一化 chunk 数据
		if chunk.Data != nil {
			chunk.Data = sn.NormalizeChunk(chunk.Data)
		}

		// 检测上下文是否已取消
		select {
		case <-ctx.Done():
			span.RecordError(ctx.Err())
			span.SetStatus(codes.Error, "context cancelled")
			out <- model.StreamChunk{Error: errormap.MapProviderError(ctx.Err())}
			return
		case out <- chunk:
		}
	}

	// 源 channel 关闭但未发送 Done，补发
	span.SetAttributes(attribute.Int("llm.stream.chunk_count", chunkCount))
	span.SetStatus(codes.Ok, "")
	out <- model.StreamChunk{Done: true}
}
