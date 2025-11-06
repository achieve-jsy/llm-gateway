package orchestrator

import (
	"context"

	"github.com/example/llm-gateway/internal/gateway/errormap"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/normalizer"
)

// ChatStream 执行流式聊天调用
// 校验模型可用性 → 解析 Provider → 调用 ChatStream → 转发 channel（带归一化）
func (o *ChatOrchestrator) ChatStream(ctx context.Context, req *model.ChatCompletionRequest) (<-chan model.StreamChunk, error) {
	// 超时控制
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)

	// 从 Registry 解析 Provider
	p, err := o.registry.Resolve(req.Model)
	if err != nil {
		cancel()
		return nil, model.ErrModelNotFound
	}

	// 调用 Provider 获取源 channel
	srcCh, err := p.ChatStream(ctx, req)
	if err != nil {
		cancel()
		return nil, errormap.MapProviderError(err)
	}

	// 创建输出 channel，启动转发 goroutine
	outCh := make(chan model.StreamChunk, 32)
	go streamPipeline(ctx, cancel, srcCh, outCh)

	return outCh, nil
}

// streamPipeline 流式管道：从 provider channel 转发到输出 channel（带归一化处理）
func streamPipeline(ctx context.Context, cancel context.CancelFunc, src <-chan model.StreamChunk, out chan<- model.StreamChunk) {
	defer close(out)
	defer cancel()

	sn := normalizer.NewStreamNormalizer()

	for chunk := range src {
		// 错误直接转发
		if chunk.Error != nil {
			out <- model.StreamChunk{Error: errormap.MapProviderError(chunk.Error)}
			return
		}

		// 完成标记直接转发
		if chunk.Done {
			out <- model.StreamChunk{Done: true}
			return
		}

		// 归一化 chunk 数据
		if chunk.Data != nil {
			chunk.Data = sn.NormalizeChunk(chunk.Data)
		}

		// 检测上下文是否已取消
		select {
		case <-ctx.Done():
			out <- model.StreamChunk{Error: errormap.MapProviderError(ctx.Err())}
			return
		case out <- chunk:
		}
	}

	// 源 channel 关闭但未发送 Done，补发
	out <- model.StreamChunk{Done: true}
}
