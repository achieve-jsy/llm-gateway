package orchestrator

import (
	"context"
	"log/slog"
	"time"

	"github.com/example/llm-gateway/internal/gateway/errormap"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/normalizer"
	"github.com/example/llm-gateway/internal/gateway/provider"
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
	// 超时控制
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	// 从 Registry 解析 Provider
	p, err := o.registry.Resolve(req.Model)
	if err != nil {
		return nil, model.ErrModelNotFound
	}

	// 调用 Provider
	resp, err := p.Chat(ctx, req)
	if err != nil {
		slog.Error("provider chat error", "provider", p.Name(), "model", req.Model, "error", err.Error())
		// 经 errormap 转换为 UnifiedError
		return nil, errormap.MapProviderError(err)
	}

	// 归一化响应
	resp = normalizer.NormalizeResponse(resp)
	return resp, nil
}
