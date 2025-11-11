package normalizer

import (
	"fmt"
	"time"

	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/google/uuid"
)

// StreamNormalizer 流式 chunk 归一化器
// 保证同一次请求内所有 chunk 使用同一 ID
type StreamNormalizer struct {
	id      string
	created int64
}

// NewStreamNormalizer 创建流式归一化器，生成统一 ID 和时间戳
func NewStreamNormalizer() *StreamNormalizer {
	return &StreamNormalizer{
		id:      fmt.Sprintf("chatcmpl-%s", uuid.New().String()[:12]),
		created: time.Now().Unix(),
	}
}

// NormalizeChunk 对单个流式 chunk 进行归一化
// 补全 Object = "chat.completion.chunk"，统一 ID 一致性
func (n *StreamNormalizer) NormalizeChunk(chunk *model.ChatCompletionChunk) *model.ChatCompletionChunk {
	if chunk == nil {
		return chunk
	}

	// 补全 ID 一致性
	if chunk.ID == "" {
		chunk.ID = n.id
	}

	// 补全 Object
	if chunk.Object == "" {
		chunk.Object = "chat.completion.chunk"
	}

	// 补全 Created
	if chunk.Created == 0 {
		chunk.Created = n.created
	}

	return chunk
}
