package handler

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/example/llm-gateway/internal/gateway/authz"
	"github.com/example/llm-gateway/internal/gateway/binding"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
)

// ModelDetailHandler 提供 GET /v1/models/:model_id 端点。
// Claude Code (CC) 等客户端在调用 /v1/messages 前会先请求该端点
// 来验证模型是否存在并获取基本能力信息。
// 响应同时兼容 OpenAI 和 Anthropic 格式。
type ModelDetailHandler struct {
	client *binding.Client
}

// NewModelDetailHandler 创建 ModelDetailHandler 实例。
func NewModelDetailHandler(controlPlaneURL, internalToken string) *ModelDetailHandler {
	return &ModelDetailHandler{
		client: binding.NewClient(controlPlaneURL, internalToken),
	}
}

// modelDetailResponse 同时兼容 OpenAI (object) 和 Anthropic (type) 格式。
type modelDetailResponse struct {
	// OpenAI 标准字段
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`

	// Anthropic 兼容字段
	Type        string `json:"type,omitempty"`
	DisplayName string `json:"display_name,omitempty"`

	// 扩展能力字段（CC 可能用来决定 feature 开关）
	MaxInputTokens  int  `json:"max_input_tokens,omitempty"`
	MaxOutputTokens int  `json:"max_output_tokens,omitempty"`
	SupportsVision  bool `json:"supports_vision,omitempty"`
	SupportsTools   bool `json:"supports_function_calling,omitempty"`
	// SupportsVideoInput 组级是否支持视频输入（保守聚合，全部成员都支持才算）。
	SupportsVideoInput bool `json:"supports_video_input,omitempty"`
}

// Handle 处理 GET /v1/models/:model_id
func (h *ModelDetailHandler) Handle(c *gin.Context) {
	modelID := c.Param("model_id")
	if modelID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "model_id is required",
				"type":    "invalid_request_error",
			},
		})
		return
	}

	// 获取用户身份
	identityVal, exists := c.Get("identity")
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"message": "model not found",
				"type":    "invalid_request_error",
				"code":    "model_not_found",
			},
		})
		return
	}
	id, ok := identityVal.(*model.UserIdentity)
	if !ok || id.KeyID == "" {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"message": "model not found",
				"type":    "invalid_request_error",
				"code":    "model_not_found",
			},
		})
		return
	}

	// 检查用户是否有权限访问该模型组
	if !h.hasAccess(c, id, modelID) {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"message": "model not found",
				"type":    "invalid_request_error",
				"code":    "model_not_found",
			},
		})
		return
	}

	// 获取模型能力信息（可选，失败时仍返回基本信息）
	now := time.Now().Unix()
	resp := modelDetailResponse{
		ID:      modelID,
		Object:  "model",
		Created: now,
		OwnedBy: "organization",
		Type:    "model",
	}

	// 尝试拉取聚合能力信息
	infoMap, err := h.client.GetGroupModelInfo(c.Request.Context(), []string{modelID})
	if err != nil {
		slog.Debug("model detail: failed to fetch model info, returning basic response",
			"model_id", modelID, "error", err)
	} else if info, ok := infoMap[modelID]; ok {
		resp.MaxInputTokens = info.MaxInputTokens
		resp.MaxOutputTokens = info.MaxOutputTokens
		resp.SupportsVision = info.SupportsVision
		resp.SupportsTools = info.SupportsFunctionCalling
		resp.SupportsVideoInput = info.SupportsVideoInput
	}

	c.JSON(http.StatusOK, resp)
}

// hasAccess 检查用户是否有权访问指定模型组。
func (h *ModelDetailHandler) hasAccess(c *gin.Context, id *model.UserIdentity, modelID string) bool {
	names, err := authz.ResolveVisibleGroupNames(c.Request.Context(), id, h.client)
	if err != nil {
		slog.Warn("model detail: failed to resolve visible groups", "key_id", id.KeyID, "error", err)
		return false
	}
	for _, name := range names {
		if name == modelID {
			return true
		}
	}
	return false
}
