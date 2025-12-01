package handler

import (
	"net/http"

	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
)

// ModelInfoServiceInterface 模型组能力服务抽象。
// 实现者：internal/controlplane/service.ModelInfoService
type ModelInfoServiceInterface interface {
	GetGroupModelInfo(names []string) (map[string]service.GroupModelInfo, error)
}

// ModelInfoHandler 内部模型能力接口 Handler。
type ModelInfoHandler struct {
	svc ModelInfoServiceInterface
}

// NewModelInfoHandler 创建 ModelInfoHandler 实例。
func NewModelInfoHandler(svc ModelInfoServiceInterface) *ModelInfoHandler {
	return &ModelInfoHandler{svc: svc}
}

// GetGroupModelInfo GET /internal/v1/groups/model-info?names=g1&names=g2
// 返回 {data: map[groupName]GroupModelInfo}；names 为空时返回所有启用组。
func (h *ModelInfoHandler) GetGroupModelInfo(c *gin.Context) {
	names := c.QueryArray("names")
	result, err := h.svc.GetGroupModelInfo(names)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"type":    "internal_error",
				"message": "failed to get group model info",
				"code":    CodeInternalError,
			},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}
