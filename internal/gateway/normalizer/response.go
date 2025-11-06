package normalizer

import (
	"fmt"
	"time"

	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/google/uuid"
)

// NormalizeResponse 对普通响应进行归一化处理
// 补全缺失的 ID、Object、Created、Usage 字段
func NormalizeResponse(resp *model.ChatCompletionResponse) *model.ChatCompletionResponse {
	if resp == nil {
		return resp
	}

	// 补全 ID
	if resp.ID == "" {
		resp.ID = generateCompletionID()
	}

	// 补全 Object
	if resp.Object == "" {
		resp.Object = "chat.completion"
	}

	// 补全 Created
	if resp.Created == 0 {
		resp.Created = time.Now().Unix()
	}

	// 确保 Usage 字段存在
	if resp.Usage == nil {
		resp.Usage = &model.Usage{}
	}

	return resp
}

// generateCompletionID 生成 chatcmpl-xxx 格式唯一 ID
func generateCompletionID() string {
	return fmt.Sprintf("chatcmpl-%s", uuid.New().String()[:12])
}
