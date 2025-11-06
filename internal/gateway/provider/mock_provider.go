package provider

import (
	"context"

	"github.com/example/llm-gateway/internal/gateway/model"
)

// MockProvider 用于测试的 Provider 实现
type MockProvider struct {
	name            string
	chatFunc        func(ctx context.Context, req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error)
	chatStreamFunc  func(ctx context.Context, req *model.ChatCompletionRequest) (<-chan model.StreamChunk, error)
	listModelsFunc  func(ctx context.Context) ([]model.ModelInfo, error)
	healthCheckFunc func(ctx context.Context) error
}

// NewMockProvider 创建 MockProvider 实例
func NewMockProvider(name string) *MockProvider {
	return &MockProvider{
		name: name,
		chatFunc: func(ctx context.Context, req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
			return &model.ChatCompletionResponse{
				ID:      "test-id",
				Object:  "chat.completion",
				Created: 1234567890,
				Model:   req.Model,
				Choices: []model.Choice{
					{
						Index:   0,
						Message: &model.Message{Role: "assistant", Content: "mock response"},
					},
				},
				Usage: &model.Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
			}, nil
		},
		chatStreamFunc: func(ctx context.Context, req *model.ChatCompletionRequest) (<-chan model.StreamChunk, error) {
			ch := make(chan model.StreamChunk, 2)
			go func() {
				defer close(ch)
				ch <- model.StreamChunk{
					Data: &model.ChatCompletionChunk{
						ID:      "test-id",
						Object:  "chat.completion.chunk",
						Created: 1234567890,
						Model:   req.Model,
						Choices: []model.Choice{
							{
								Index:        0,
								Delta:        &model.Delta{Content: "mock"},
								FinishReason: nil,
							},
						},
					},
				}
				ch <- model.StreamChunk{Done: true}
			}()
			return ch, nil
		},
		listModelsFunc: func(ctx context.Context) ([]model.ModelInfo, error) {
			return []model.ModelInfo{
				{ID: "mock-model-1", Object: "model", OwnedBy: name},
			}, nil
		},
		healthCheckFunc: func(ctx context.Context) error {
			return nil
		},
	}
}

// Chat 实现 Provider 接口
func (m *MockProvider) Chat(ctx context.Context, req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error) {
	if m.chatFunc != nil {
		return m.chatFunc(ctx, req)
	}
	return nil, nil
}

// ChatStream 实现 Provider 接口
func (m *MockProvider) ChatStream(ctx context.Context, req *model.ChatCompletionRequest) (<-chan model.StreamChunk, error) {
	if m.chatStreamFunc != nil {
		return m.chatStreamFunc(ctx, req)
	}
	return nil, nil
}

// ListModels 实现 Provider 接口
func (m *MockProvider) ListModels(ctx context.Context) ([]model.ModelInfo, error) {
	if m.listModelsFunc != nil {
		return m.listModelsFunc(ctx)
	}
	return nil, nil
}

// HealthCheck 实现 Provider 接口
func (m *MockProvider) HealthCheck(ctx context.Context) error {
	if m.healthCheckFunc != nil {
		return m.healthCheckFunc(ctx)
	}
	return nil
}

// Name 实现 Provider 接口
func (m *MockProvider) Name() string {
	return m.name
}

// SetChatFunc 设置 Chat 行为
func (m *MockProvider) SetChatFunc(f func(ctx context.Context, req *model.ChatCompletionRequest) (*model.ChatCompletionResponse, error)) {
	m.chatFunc = f
}

// SetChatStreamFunc 设置 ChatStream 行为
func (m *MockProvider) SetChatStreamFunc(f func(ctx context.Context, req *model.ChatCompletionRequest) (<-chan model.StreamChunk, error)) {
	m.chatStreamFunc = f
}

// SetListModelsFunc 设置 ListModels 行为
func (m *MockProvider) SetListModelsFunc(f func(ctx context.Context) ([]model.ModelInfo, error)) {
	m.listModelsFunc = f
}

// SetHealthCheckFunc 设置 HealthCheck 行为
func (m *MockProvider) SetHealthCheckFunc(f func(ctx context.Context) error) {
	m.healthCheckFunc = f
}
