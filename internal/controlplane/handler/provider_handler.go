package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ProviderServiceInterface ProviderHandler 所依赖的 ProviderService 方法集
// 实现者：internal/controlplane/service.ProviderService
type ProviderServiceInterface interface {
	CreateProvider(input service.CreateProviderInput) (*model.Provider, error)
	ListProviders(filter repository.ProviderListFilter, page, pageSize int) ([]service.ProviderResponse, int64, error)
	GetProvider(id uuid.UUID) (*service.ProviderResponse, error)
	UpdateProvider(id uuid.UUID, input service.UpdateProviderInput) (*service.ProviderResponse, error)
	ToggleProviderStatus(id uuid.UUID, enabled bool) (*service.ProviderResponse, error)
	DeleteProvider(id uuid.UUID) error
}

// ProviderHandler 渠道管理 Handler
type ProviderHandler struct {
	svc ProviderServiceInterface
}

// NewProviderHandler 创建 ProviderHandler 实例
func NewProviderHandler(svc ProviderServiceInterface) *ProviderHandler {
	return &ProviderHandler{svc: svc}
}

// CreateProvider POST /api/v1/providers
func (h *ProviderHandler) CreateProvider(c *gin.Context) {
	var input service.CreateProviderInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "invalid_request",
				"message": err.Error(),
				"code":    CodeBadRequest,
			},
		})
		return
	}

	provider, err := h.svc.CreateProvider(input)
	if err != nil {
		if errors.Is(err, service.ErrProviderNameExists) {
			c.JSON(http.StatusConflict, gin.H{
				"error": gin.H{
					"type":    "conflict",
					"message": "provider name already exists",
					"code":    CodeProviderNameConflict,
				},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"type":    "internal_error",
				"message": err.Error(),
				"code":    "internal_error",
			},
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": provider})
}

// ListProviders GET /api/v1/providers
func (h *ProviderHandler) ListProviders(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	filter := repository.ProviderListFilter{
		Name:         c.Query("name"),
		DefinitionID: c.Query("definition_id"),
		Tag:          c.Query("tag"),
	}

	providers, total, err := h.svc.ListProviders(filter, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"type":    "internal_error",
				"message": "failed to list providers",
				"code":    CodeInternalError,
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  providers,
		"total": total,
		"page":  page,
	})
}

// GetProvider GET /api/v1/providers/:id
func (h *ProviderHandler) GetProvider(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "invalid_request",
				"message": "invalid provider id",
				"code":    CodeBadRequest,
			},
		})
		return
	}

	provider, err := h.svc.GetProvider(id)
	if err != nil {
		if errors.Is(err, service.ErrProviderNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{
					"type":    "not_found",
					"message": "provider not found",
					"code":    CodeProviderNotFound,
				},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"type":    "internal_error",
				"message": "failed to get provider",
				"code":    CodeInternalError,
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": provider})
}

// UpdateProvider PUT /api/v1/providers/:id
func (h *ProviderHandler) UpdateProvider(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "invalid_request",
				"message": "invalid provider id",
				"code":    CodeBadRequest,
			},
		})
		return
	}

	var input service.UpdateProviderInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "invalid_request",
				"message": err.Error(),
				"code":    CodeBadRequest,
			},
		})
		return
	}

	provider, err := h.svc.UpdateProvider(id, input)
	if err != nil {
		if errors.Is(err, service.ErrProviderNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{"type": "not_found", "message": "provider not found", "code": CodeProviderNotFound},
			})
			return
		}
		if errors.Is(err, service.ErrProviderNameExists) {
			c.JSON(http.StatusConflict, gin.H{
				"error": gin.H{"type": "conflict", "message": "provider name already exists", "code": CodeProviderNameConflict},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "failed to update provider", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": provider})
}

// ToggleProviderStatus PATCH /api/v1/providers/:id/status
func (h *ProviderHandler) ToggleProviderStatus(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "invalid provider id", "code": CodeBadRequest},
		})
		return
	}

	var input struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": err.Error(), "code": CodeBadRequest},
		})
		return
	}

	provider, err := h.svc.ToggleProviderStatus(id, input.Enabled)
	if err != nil {
		if errors.Is(err, service.ErrProviderNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{"type": "not_found", "message": "provider not found", "code": CodeProviderNotFound},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "failed to toggle status", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": provider})
}

// DeleteProvider DELETE /api/v1/providers/:id
func (h *ProviderHandler) DeleteProvider(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "invalid provider id", "code": CodeBadRequest},
		})
		return
	}

	err = h.svc.DeleteProvider(id)
	if err != nil {
		if errors.Is(err, service.ErrProviderNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{"type": "not_found", "message": "provider not found", "code": CodeProviderNotFound},
			})
			return
		}
		if errors.Is(err, service.ErrProviderHasModels) {
			c.JSON(http.StatusConflict, gin.H{
				"error": gin.H{"type": "conflict", "message": "cannot delete provider with associated models, remove models first", "code": CodeProviderHasModels},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "failed to delete provider", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "provider deleted"})
}
