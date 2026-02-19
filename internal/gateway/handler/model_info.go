package handler

import (
	"log/slog"
	"net/http"

	"github.com/example/llm-gateway/internal/gateway/authz"
	"github.com/example/llm-gateway/internal/gateway/binding"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
)

// ModelInfoHandler 提供 LiteLLM 兼容的 GET /v1/model/info 端点。
//
// opencode 的 models-discovery 插件在配置 modelInfoFormat="litellm" 时会读取该端点，
// 用其中的 model_info 字段为发现到的模型补全能力（attachment/tool_call/limit 等）。
// 返回范围与 /v1/models 一致——按当前 API Key 可见的模型组，能力由 control-plane
// 聚合组内成员模型的 catalog 能力得到。
type ModelInfoHandler struct {
	client *binding.Client
}

// NewModelInfoHandler 创建 ModelInfoHandler 实例。
func NewModelInfoHandler(controlPlaneURL, internalToken string) *ModelInfoHandler {
	return &ModelInfoHandler{
		client: binding.NewClient(controlPlaneURL, internalToken),
	}
}

// litellmModelInfoEntry 单个模型的 LiteLLM model_info 条目。
type litellmModelInfoEntry struct {
	ModelName string         `json:"model_name"`
	ModelInfo litellmModelInfo `json:"model_info"`
}

type litellmModelInfo struct {
	Key                     string `json:"key"`
	Mode                    string `json:"mode,omitempty"`
	MaxInputTokens          int    `json:"max_input_tokens,omitempty"`
	MaxOutputTokens         int    `json:"max_output_tokens,omitempty"`
	MaxTokens               int    `json:"max_tokens,omitempty"`
	SupportsVision          bool   `json:"supports_vision"`
	SupportsFunctionCalling bool   `json:"supports_function_calling"`
	SupportsToolChoice      bool   `json:"supports_tool_choice"`
}

type litellmModelInfoResponse struct {
	Data []litellmModelInfoEntry `json:"data"`
}

// Handle 处理 GET /v1/model/info。
func (h *ModelInfoHandler) Handle(c *gin.Context) {
	names := h.resolveGroupNames(c)
	if len(names) == 0 {
		c.JSON(http.StatusOK, litellmModelInfoResponse{Data: []litellmModelInfoEntry{}})
		return
	}

	infoMap, err := h.client.GetGroupModelInfo(c.Request.Context(), names)
	if err != nil {
		slog.Warn("model info: failed to fetch group model info", "error", err)
		// 优雅降级：返回仅含模型名的条目，插件仍可发现模型，只是没有能力富化。
		infoMap = nil
	}

	entries := make([]litellmModelInfoEntry, 0, len(names))
	for _, name := range names {
		if name == "" {
			continue
		}
		entry := litellmModelInfoEntry{
			ModelName: name,
			ModelInfo: litellmModelInfo{Key: name},
		}
		if info, ok := infoMap[name]; ok {
			entry.ModelInfo.Mode = info.Mode
			entry.ModelInfo.MaxInputTokens = info.MaxInputTokens
			entry.ModelInfo.MaxOutputTokens = info.MaxOutputTokens
			entry.ModelInfo.SupportsVision = info.SupportsVision
			entry.ModelInfo.SupportsFunctionCalling = info.SupportsFunctionCalling
			entry.ModelInfo.SupportsToolChoice = info.SupportsToolChoice
		}
		entries = append(entries, entry)
	}

	c.JSON(http.StatusOK, litellmModelInfoResponse{Data: entries})
}

func (h *ModelInfoHandler) resolveGroupNames(c *gin.Context) []string {
	identityVal, exists := c.Get("identity")
	if !exists {
		return nil
	}
	id, ok := identityVal.(*model.UserIdentity)
	if !ok || id.KeyID == "" {
		return nil
	}

	names, err := authz.ResolveVisibleGroupNames(c.Request.Context(), id, h.client)
	if err != nil {
		slog.Warn("model info: failed to resolve visible groups", "key_id", id.KeyID, "error", err)
		return nil
	}
	return names
}
