package handler

import (
	"io"
	"net/http"

	"github.com/example/llm-gateway/internal/core/video"
	"github.com/gin-gonic/gin"
)

// AssetHandler 素材库 Handler
type AssetHandler struct {
	relayAdapter      video.AssetProvider
	volcengineAdapter video.AssetProvider
}

// NewAssetHandler 创建素材 Handler
func NewAssetHandler(relayAdapter, volcengineAdapter video.AssetProvider) *AssetHandler {
	return &AssetHandler{
		relayAdapter:      relayAdapter,
		volcengineAdapter: volcengineAdapter,
	}
}

// Handle 统一入口，按 Action 分发
func (h *AssetHandler) Handle(c *gin.Context) {
	action := c.Param("action")
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}

	// 默认使用中转适配器
	result, err := h.relayAdapter.DoAction(c.Request.Context(), action, body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.Data(http.StatusOK, "application/json", result)
}
