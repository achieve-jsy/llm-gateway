package provider

import (
	"context"

	"github.com/example/llm-gateway/internal/gateway/model"
)

// Provider 厂商通道统一接口
type Provider interface {
	// Chat 普通调用（非流式）
	Chat(ctx context.Context, req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error)

	// ChatStream 流式调用，返回 chunk channel
	ChatStream(ctx context.Context, req *model.ChatCompletionRequest) (<-chan model.StreamChunk, error)

	// ListModels 返回该 Provider 支持的模型列表
	ListModels(ctx context.Context) ([]model.ModelInfo, error)

	// HealthCheck 健康检查
	HealthCheck(ctx context.Context) error

	// Name 返回 Provider 名称
	Name() string
}
