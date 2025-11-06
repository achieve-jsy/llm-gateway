package handler

import (
	"net/http"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/gin-gonic/gin"
)

// DefinitionRepoInterface 渠道定义仓储抽象
// 实现者：internal/controlplane/repository.ProviderDefinitionRepo
type DefinitionRepoInterface interface {
	List() ([]model.ProviderDefinition, error)
	GetByID(id string) (*model.ProviderDefinition, error)
}

// DefinitionHandler 渠道定义 Handler
type DefinitionHandler struct {
	repo DefinitionRepoInterface
}

// NewDefinitionHandler 创建实例
func NewDefinitionHandler(repo DefinitionRepoInterface) *DefinitionHandler {
	return &DefinitionHandler{repo: repo}
}

// ListDefinitions GET /api/v1/provider-definitions
// 前端创建渠道时调用，获取可选的渠道类型列表及凭证 schema
func (h *DefinitionHandler) ListDefinitions(c *gin.Context) {
	defs, err := h.repo.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "failed to list definitions", "code": CodeInternalError},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": defs})
}

// GetDefinition GET /api/v1/provider-definitions/:id
func (h *DefinitionHandler) GetDefinition(c *gin.Context) {
	id := c.Param("id")
	def, err := h.repo.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{"type": "not_found", "message": "definition not found", "code": "not_found"},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": def})
}
