package handler

import (
	"fmt"
	"net/http"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ModelTagRepoInterface 模型标签仓储抽象
// 实现者：internal/controlplane/repository.ModelTagRepo
type ModelTagRepoInterface interface {
	ListTags() ([]model.ModelTag, error)
	CreateTag(tag *model.ModelTag) error
	UpdateTag(tag *model.ModelTag) error
	DeleteTag(id uuid.UUID) error
	GetTagByID(id uuid.UUID) (*model.ModelTag, error)
	ExistsTagByName(name string) (bool, error)
	SetModelTags(modelID uuid.UUID, tagIDs []uuid.UUID) error
	GetModelTags(modelID uuid.UUID) ([]model.ModelTag, error)
	BatchGetModelTags(modelIDs []uuid.UUID) (map[uuid.UUID][]model.ModelTag, error)
}

// ModelTagHandler 模型标签 Handler
type ModelTagHandler struct {
	repo ModelTagRepoInterface
}

// NewModelTagHandler 创建实例
func NewModelTagHandler(repo ModelTagRepoInterface) *ModelTagHandler {
	return &ModelTagHandler{repo: repo}
}

// ListTags GET /api/v1/model-tags
func (h *ModelTagHandler) ListTags(c *gin.Context) {
	tags, err := h.repo.ListTags()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "failed to list tags", "code": CodeInternalError},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tags})
}

// CreateTag POST /api/v1/model-tags
func (h *ModelTagHandler) CreateTag(c *gin.Context) {
	var input struct {
		Name        string `json:"name" binding:"required,max=50"`
		Color       string `json:"color" binding:"required,max=30"`
		Description string `json:"description" binding:"max=200"`
		SortOrder   int    `json:"sort_order"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": err.Error(), "code": CodeBadRequest},
		})
		return
	}

	exists, _ := h.repo.ExistsTagByName(input.Name)
	if exists {
		c.JSON(http.StatusConflict, gin.H{
			"error": gin.H{"type": "conflict", "message": "tag name already exists", "code": "name_conflict"},
		})
		return
	}

	tag := &model.ModelTag{
		Name:        input.Name,
		Color:       input.Color,
		Description: input.Description,
		SortOrder:   input.SortOrder,
	}
	if err := h.repo.CreateTag(tag); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "failed to create tag", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": tag})
}

// UpdateTag PUT /api/v1/model-tags/:id
func (h *ModelTagHandler) UpdateTag(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "invalid tag id", "code": CodeBadRequest},
		})
		return
	}

	tag, err := h.repo.GetTagByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{"type": "not_found", "message": "tag not found", "code": "not_found"},
		})
		return
	}

	var input struct {
		Name        string `json:"name" binding:"omitempty,max=50"`
		Color       string `json:"color" binding:"omitempty,max=30"`
		Description string `json:"description" binding:"max=200"`
		SortOrder   *int   `json:"sort_order"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": err.Error(), "code": CodeBadRequest},
		})
		return
	}

	if input.Name != "" && input.Name != tag.Name {
		exists, _ := h.repo.ExistsTagByName(input.Name)
		if exists {
			c.JSON(http.StatusConflict, gin.H{
				"error": gin.H{"type": "conflict", "message": "tag name already exists", "code": "name_conflict"},
			})
			return
		}
		tag.Name = input.Name
	}
	if input.Color != "" {
		tag.Color = input.Color
	}
	tag.Description = input.Description
	if input.SortOrder != nil {
		tag.SortOrder = *input.SortOrder
	}

	if err := h.repo.UpdateTag(tag); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "failed to update tag", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": tag})
}

// DeleteTag DELETE /api/v1/model-tags/:id
func (h *ModelTagHandler) DeleteTag(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "invalid tag id", "code": CodeBadRequest},
		})
		return
	}

	if err := h.repo.DeleteTag(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "failed to delete tag", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "tag deleted"})
}

// SetModelTags PUT /api/v1/models/:id/tags
func (h *ModelTagHandler) SetModelTags(c *gin.Context) {
	modelID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "invalid model id", "code": CodeBadRequest},
		})
		return
	}

	var input struct {
		TagIDs []string `json:"tag_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": err.Error(), "code": CodeBadRequest},
		})
		return
	}

	tagIDs := make([]uuid.UUID, 0, len(input.TagIDs))
	for _, idStr := range input.TagIDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{
					"type":    "invalid_request",
					"message": fmt.Sprintf("invalid tag_id: %s", idStr),
					"code":    CodeInvalidTagID,
				},
			})
			return
		}
		tagIDs = append(tagIDs, id)
	}

	if err := h.repo.SetModelTags(modelID, tagIDs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "failed to set model tags", "code": CodeInternalError},
		})
		return
	}

	tags, _ := h.repo.GetModelTags(modelID)
	c.JSON(http.StatusOK, gin.H{"data": tags})
}

// BatchSetModelTags PUT /api/v1/models/batch-tags
func (h *ModelTagHandler) BatchSetModelTags(c *gin.Context) {
	var input struct {
		ModelIDs []string `json:"model_ids" binding:"required,min=1"`
		TagIDs   []string `json:"tag_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": err.Error(), "code": CodeBadRequest},
		})
		return
	}

	tagIDs := make([]uuid.UUID, 0, len(input.TagIDs))
	for _, idStr := range input.TagIDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{
					"type":    "invalid_request",
					"message": fmt.Sprintf("invalid tag_id: %s", idStr),
					"code":    CodeInvalidTagID,
				},
			})
			return
		}
		tagIDs = append(tagIDs, id)
	}

	modelIDs := make([]uuid.UUID, 0, len(input.ModelIDs))
	for _, idStr := range input.ModelIDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{
					"type":    "invalid_request",
					"message": fmt.Sprintf("invalid model_id: %s", idStr),
					"code":    CodeInvalidModelID,
				},
			})
			return
		}
		modelIDs = append(modelIDs, id)
	}

	updated := 0
	for _, modelID := range modelIDs {
		if err := h.repo.SetModelTags(modelID, tagIDs); err != nil {
			continue
		}
		updated++
	}

	c.JSON(http.StatusOK, gin.H{"data": gin.H{"updated": updated}})
}

// GetModelTags GET /api/v1/models/:id/tags
func (h *ModelTagHandler) GetModelTags(c *gin.Context) {
	modelID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "invalid model id", "code": CodeBadRequest},
		})
		return
	}

	tags, err := h.repo.GetModelTags(modelID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "failed to get model tags", "code": CodeInternalError},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": tags})
}

// BatchGetModelTags POST /api/v1/models/batch-tags/query
func (h *ModelTagHandler) BatchGetModelTags(c *gin.Context) {
	var input struct {
		ModelIDs []string `json:"model_ids" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": err.Error(), "code": CodeBadRequest},
		})
		return
	}

	modelIDs := make([]uuid.UUID, 0, len(input.ModelIDs))
	for _, idStr := range input.ModelIDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			continue
		}
		modelIDs = append(modelIDs, id)
	}

	if len(modelIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "message": "no valid model ids provided", "code": CodeBadRequest},
		})
		return
	}

	tagMap, err := h.repo.BatchGetModelTags(modelIDs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"type": "internal_error", "message": "failed to batch get model tags", "code": CodeInternalError},
		})
		return
	}

	result := make(map[string][]model.ModelTag)
	for modelID, tags := range tagMap {
		result[modelID.String()] = tags
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}
