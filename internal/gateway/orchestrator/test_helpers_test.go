package orchestrator

import (
	"github.com/example/llm-gateway/internal/core"
)

func newTestChatRequest(modelName string) *core.ChatRequest {
	return &core.ChatRequest{
		Model: modelName,
		Messages: []core.Message{
			{Role: "user", Content: "Hello"},
		},
	}
}

func newTestEmbeddingRequest(modelName string) *core.EmbeddingRequest {
	return &core.EmbeddingRequest{
		Model: modelName,
		Input: "test input",
	}
}
