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

// ModelServiceInterface 模型服务抽象
// 实现者：internal/controlplane/service.ModelService
type ModelServiceInterface interface {
	CreateModel(input service.CreateModelInput) (*model.Model, error)
	ListModels(filter repository.ModelListFilter, page, pageSize int) ([]model.Model, int64, error)
	UpdateModel(id uuid.UUID, input service.UpdateModelInput) (*model.Model, error)
	DeleteModel(id uuid.UUID) error
	CheckModelName(name string) (bool, error)
	BatchDeleteModels(ids []uuid.UUID) (int64, error)
	BatchUpdateModelStatus(ids []uuid.UUID, enabled bool) (int64, error)
}

// ModelHandler 模型管理 Handler
type ModelHandler struct {
	svc ModelServiceInterface
}

// NewModelHandler 创建 ModelHandler 实例
func NewModelHandler(svc ModelServiceInterface) *ModelHandler {
	return &ModelHandler{svc: svc}
}

// CreateModel POST /api/v1/models
func (h *ModelHandler) CreateModel(c *gin.Context) {
	var input service.CreateModelInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": err.Error(), "code": CodeBadRequest},
		})
		return
	}

	m, err := h.svc.CreateModel(input)
	if err != nil {
		if errors.Is(err, service.ErrModelNameExists) {
			c.JSON(http.StatusConflict, gin.H{
				"error": gin.H{"type": "conflict", "message": "model name already exists", "code": CodeModelNameConflict},
			})
			return
		}
		if errors.Is(err, service.ErrModelProviderInvalid) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"type": "invalid_request", "message": "provider not found or disabled", "code": CodeModelProviderInvalid},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "failed to create model", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": m})
}

// ListModels GET /api/v1/models
func (h *ModelHandler) ListModels(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	var providerID *uuid.UUID
	if pidStr := c.Query("provider_id"); pidStr != "" {
		pid, err := uuid.Parse(pidStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"type": "invalid_request", "message": "invalid provider_id", "code": CodeBadRequest},
			})
			return
		}
		providerID = &pid
	}

	filter := repository.ModelListFilter{
		ProviderID: providerID,
		Name:       c.Query("name"),
		ModelType:  c.Query("model_type"),
	}

	models, total, err := h.svc.ListModels(filter, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "failed to list models", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": models, "total": total, "page": page, "page_size": pageSize})
}

// UpdateModel PUT /api/v1/models/:id
func (h *ModelHandler) UpdateModel(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "invalid model id", "code": CodeBadRequest},
		})
		return
	}

	var input service.UpdateModelInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": err.Error(), "code": CodeBadRequest},
		})
		return
	}

	m, err := h.svc.UpdateModel(id, input)
	if err != nil {
		if errors.Is(err, service.ErrModelNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{"type": "not_found", "message": "model not found", "code": CodeModelNotFound},
			})
			return
		}
		if errors.Is(err, service.ErrModelNameExists) {
			c.JSON(http.StatusConflict, gin.H{
				"error": gin.H{"type": "conflict", "message": "model name already exists", "code": CodeModelNameConflict},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "failed to update model", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": m})
}

// CheckModelName GET /api/v1/models/check-name?name=…
func (h *ModelHandler) CheckModelName(c *gin.Context) {
	name := c.Query("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "name query parameter is required", "code": CodeBadRequest},
		})
		return
	}

	available, err := h.svc.CheckModelName(name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "failed to check model name", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": gin.H{"available": available}})
}

// DeleteModel DELETE /api/v1/models/:id
func (h *ModelHandler) DeleteModel(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "invalid model id", "code": CodeBadRequest},
		})
		return
	}

	err = h.svc.DeleteModel(id)
	if err != nil {
		if errors.Is(err, service.ErrModelNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{"type": "not_found", "message": "model not found", "code": CodeModelNotFound},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "failed to delete model", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "model deleted"})
}

// BatchDeleteModels DELETE /api/v1/models/batch
func (h *ModelHandler) BatchDeleteModels(c *gin.Context) {
	var input struct {
		ModelIDs []string `json:"model_ids" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": err.Error(), "code": CodeBadRequest},
		})
		return
	}

	ids := make([]uuid.UUID, 0, len(input.ModelIDs))
	for _, idStr := range input.ModelIDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"type": "invalid_request", "message": "invalid model id: " + idStr, "code": CodeBadRequest},
			})
			return
		}
		ids = append(ids, id)
	}

	deleted, err := h.svc.BatchDeleteModels(ids)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "failed to batch delete models", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": gin.H{"deleted": deleted}})
}

// BatchUpdateModelStatus PATCH /api/v1/models/batch-status
func (h *ModelHandler) BatchUpdateModelStatus(c *gin.Context) {
	var input struct {
		ModelIDs []string `json:"model_ids" binding:"required,min=1"`
		Enabled  *bool    `json:"enabled" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": err.Error(), "code": CodeBadRequest},
		})
		return
	}

	ids := make([]uuid.UUID, 0, len(input.ModelIDs))
	for _, idStr := range input.ModelIDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"type": "invalid_request", "message": "invalid model id: " + idStr, "code": CodeBadRequest},
			})
			return
		}
		ids = append(ids, id)
	}

	updated, err := h.svc.BatchUpdateModelStatus(ids, *input.Enabled)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "failed to batch update model status", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": gin.H{"updated": updated}})
}
