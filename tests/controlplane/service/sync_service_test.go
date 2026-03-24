package service_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/stretchr/testify/assert"
)

func TestInferModelType_Scenarios(t *testing.T) {
	tests := []struct {
		name     string
		modelName string
		want     string
	}{
		{name: "chat模型", modelName: "qwen-max", want: "chat"},
		{name: "chat模型(大写)", modelName: "GPT-4O", want: "chat"},
		{name: "普通embedding", modelName: "text-embedding-v4", want: "embedding"},
		{name: "embedding(大写)", modelName: "TEXT-EMBEDDING-ADA-002", want: "embedding"},
		{name: "多模态embedding-vl", modelName: "qwen3-vl-embedding", want: "embedding_vl"},
		{name: "多模态embedding-multimodal", modelName: "multimodal-embedding-v1", want: "embedding_vl"},
		{name: "多模态embedding-vision", modelName: "vision-embedding-v2", want: "embedding_vl"},
		{name: "rerank模型", modelName: "qwen3-rerank", want: "rerank"},
		{name: "rerank模型(大写)", modelName: "GTE-RERANK-V2", want: "rerank"},
		{name: "rerank优先于embed", modelName: "embed-rerank-model", want: "rerank"},
		{name: "vl优先于embed", modelName: "embed-vl-model", want: "embedding_vl"},
		{name: "未知模型默认chat", modelName: "unknown-model", want: "chat"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := service.InferModelType(tt.modelName)
			assert.Equal(t, tt.want, got)
		})
	}
}