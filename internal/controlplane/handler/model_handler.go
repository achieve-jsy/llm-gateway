package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

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

	// Catalog 联动（CAT-1）
	CreateModelFromCatalog(catalogID, providerID uuid.UUID, name string) (*model.Model, error)
	ResetOverride(modelID uuid.UUID, field string) error
	GetModel(id uuid.UUID) (*model.Model, error)

	// Catalog 绑定/换绑/解绑（CAT-2）
	BindCatalog(modelID, catalogID uuid.UUID, resetFields bool) (*model.Model, error)
	UnbindCatalog(modelID uuid.UUID) (*model.Model, error)
	BatchBindCatalog(items []service.BatchBindInput) (*service.BatchBindResult, error)
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

	var enabled *bool
	if enabledStr := c.Query("enabled"); enabledStr != "" {
		parsed, err := strconv.ParseBool(enabledStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"type": "invalid_request", "message": "invalid enabled", "code": CodeBadRequest},
			})
			return
		}
		enabled = &parsed
	}

	filter := repository.ModelListFilter{
		ProviderID: providerID,
		Name:       c.Query("name"),
		ModelType:  c.Query("model_type"),
		Enabled:    enabled,
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

// CreateModelFromCatalog POST /api/v1/models/from-catalog
//
// 请求体：{ catalog_id, provider_id, name }
// 行为：从 model_catalog 复制一份快照建出 Model，并设置 catalog_id 外键。
type createModelFromCatalogInput struct {
	CatalogID  uuid.UUID `json:"catalog_id" binding:"required"`
	ProviderID uuid.UUID `json:"provider_id" binding:"required"`
	Name       string    `json:"name" binding:"required,min=1,max=200"`
}

func (h *ModelHandler) CreateModelFromCatalog(c *gin.Context) {
	var input createModelFromCatalogInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": err.Error(), "code": CodeBadRequest},
		})
		return
	}

	m, err := h.svc.CreateModelFromCatalog(input.CatalogID, input.ProviderID, input.Name)
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
		// catalog 找不到时，service 层用 fmt.Errorf("catalog %s not found", ...)，
		// 通过字符串匹配映射到契约约定的 catalog_not_found
		if errMsg := err.Error(); len(errMsg) > 0 && (containsCatalogNotFound(errMsg)) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{"type": "not_found", "message": errMsg, "code": CodeCatalogNotFound},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": err.Error(), "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": m})
}

// ResetModelOverride POST /api/v1/models/:id/reset-override
//
// 请求体：{ field: "context_length" | "max_output_tokens" }
// 行为：取消该字段的 overridden 锁，并把字段值拉回 catalog 当前值。
type resetOverrideInput struct {
	Field string `json:"field" binding:"required"`
}

func (h *ModelHandler) ResetModelOverride(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "invalid model id", "code": CodeBadRequest},
		})
		return
	}

	var input resetOverrideInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": err.Error(), "code": CodeBadRequest},
		})
		return
	}

	if err := h.svc.ResetOverride(id, input.Field); err != nil {
		if errors.Is(err, service.ErrModelNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{"type": "not_found", "message": "model not found", "code": CodeModelNotFound},
			})
			return
		}
		errMsg := err.Error()
		if containsNoCatalogLink(errMsg) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"type": "invalid_request", "message": errMsg, "code": CodeCatalogNotBound},
			})
			return
		}
		if containsUnsupportedField(errMsg) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"type": "invalid_request", "message": errMsg, "code": CodeBadRequest},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": errMsg, "code": CodeInternalError},
		})
		return
	}

	m, err := h.svc.GetModel(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "fetch model after reset: " + err.Error(), "code": CodeInternalError},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": m})
}

func containsCatalogNotFound(s string) bool {
	return strings.Contains(s, "not found") && strings.Contains(s, "catalog")
}

func containsNoCatalogLink(s string) bool {
	return strings.Contains(s, "no catalog link")
}

func containsUnsupportedField(s string) bool {
	return strings.Contains(s, "unsupported") || strings.Contains(s, "invalid field")
}

// ========== CAT-2: Catalog 绑定/换绑/解绑 ==========

// BindCatalog POST /api/v1/models/:id/bind-catalog
//
// 请求体：{ "catalog_id": "uuid", "reset_fields": false }
// 行为：把当前 model 关联到指定 catalog 条目；reset_fields=true 时同步把价格/容量/上游 ID 全部
//      回写为新 catalog 的当前值；标志位 ctx_overridden / max_output_overridden 始终重置为 false。
type bindCatalogInput struct {
	CatalogID   uuid.UUID `json:"catalog_id" binding:"required"`
	ResetFields bool      `json:"reset_fields"`
}

func (h *ModelHandler) BindCatalog(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "invalid model id", "code": CodeBadRequest},
		})
		return
	}
	var input bindCatalogInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": err.Error(), "code": CodeBadRequest},
		})
		return
	}

	m, err := h.svc.BindCatalog(id, input.CatalogID, input.ResetFields)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrModelNotFound):
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{"type": "not_found", "message": "model not found", "code": CodeModelNotFound},
			})
		case errors.Is(err, service.ErrCatalogAlreadyBoundToSame):
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"type": "invalid_request", "message": err.Error(), "code": CodeAlreadyBoundToSame},
			})
		case containsCatalogNotFound(err.Error()):
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{"type": "not_found", "message": err.Error(), "code": CodeCatalogNotFound},
			})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": gin.H{"type": "internal_error", "message": err.Error(), "code": CodeInternalError},
			})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": m})
}

// UnbindCatalog POST /api/v1/models/:id/unbind-catalog
//
// 行为：catalog_id → NULL；ctx_overridden / max_output_overridden 重置为 false；价格/容量保留 snapshot。
func (h *ModelHandler) UnbindCatalog(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "invalid model id", "code": CodeBadRequest},
		})
		return
	}
	m, err := h.svc.UnbindCatalog(id)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrModelNotFound):
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{"type": "not_found", "message": "model not found", "code": CodeModelNotFound},
			})
		case errors.Is(err, service.ErrCatalogNotBound):
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"type": "invalid_request", "message": err.Error(), "code": CodeCatalogNotBound},
			})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": gin.H{"type": "internal_error", "message": err.Error(), "code": CodeInternalError},
			})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": m})
}

// BatchBindCatalog POST /api/v1/models/batch-bind-catalog
//
// 请求体：{ "bindings": [ { "model_id": "uuid", "catalog_id": "uuid", "reset_fields": false }, ... ] }
//
// 单条失败不阻塞，返回 applied 数 + 失败映射 model_id → reason。
type batchBindRequestItem struct {
	ModelID     string `json:"model_id" binding:"required,uuid"`
	CatalogID   string `json:"catalog_id" binding:"required,uuid"`
	ResetFields bool   `json:"reset_fields"`
}
type batchBindCatalogInput struct {
	Bindings []batchBindRequestItem `json:"bindings" binding:"required,min=1,dive"`
}

func (h *ModelHandler) BatchBindCatalog(c *gin.Context) {
	var input batchBindCatalogInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": err.Error(), "code": CodeBadRequest},
		})
		return
	}
	items := make([]service.BatchBindInput, 0, len(input.Bindings))
	for _, b := range input.Bindings {
		mid, err := uuid.Parse(b.ModelID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"type": "invalid_request", "message": "invalid model_id: " + b.ModelID, "code": CodeBadRequest},
			})
			return
		}
		cid, err := uuid.Parse(b.CatalogID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"type": "invalid_request", "message": "invalid catalog_id: " + b.CatalogID, "code": CodeBadRequest},
			})
			return
		}
		items = append(items, service.BatchBindInput{
			ModelID:     mid,
			CatalogID:   cid,
			ResetFields: b.ResetFields,
		})
	}
	res, err := h.svc.BatchBindCatalog(items)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": err.Error(), "code": CodeInternalError},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": res})
}
