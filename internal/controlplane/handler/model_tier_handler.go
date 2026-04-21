package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/example/llm-gateway/internal/controlplane/audit"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type ModelTierServiceInterface interface {
	List(context.Context, bool) ([]model.ModelTier, error)
	Get(context.Context, uuid.UUID) (*model.ModelTier, error)
	Create(context.Context, service.CreateModelTierInput) (*model.ModelTier, error)
	Update(context.Context, uuid.UUID, service.UpdateModelTierInput) (*model.ModelTier, error)
	SetEnabled(context.Context, uuid.UUID, bool, string) (*model.ModelTier, error)
	Archive(context.Context, uuid.UUID, string) (*model.ModelTier, error)
	Delete(context.Context, uuid.UUID, string) error
	Reorder(context.Context, []uuid.UUID, string) error
}

type ModelTierHandler struct {
	service      ModelTierServiceInterface
	groupService GroupBudgetTierServiceInterface
}

type GroupBudgetTierServiceInterface interface {
	SetBudgetTier(context.Context, uint, *uuid.UUID, string) error
	SetBudgetTierBatch(context.Context, []uint, *uuid.UUID, string) error
	GetByID(uint) (*model.Group, error)
}

func NewModelTierHandler(service ModelTierServiceInterface) *ModelTierHandler {
	return &ModelTierHandler{service: service}
}

func (h *ModelTierHandler) WithGroupService(service GroupBudgetTierServiceInterface) *ModelTierHandler {
	h.groupService = service
	return h
}

type modelTierWriteRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Color       string `json:"color"`
	SortOrder   int    `json:"sort_order"`
	Reason      string `json:"reason"`
}

func (h *ModelTierHandler) List(c *gin.Context) {
	includeArchived, _ := strconv.ParseBool(c.DefaultQuery("include_archived", "false"))
	tiers, err := h.service.List(c.Request.Context(), includeArchived)
	if err != nil {
		writeModelTierError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tiers})
}

func (h *ModelTierHandler) Get(c *gin.Context) {
	id, ok := parseTierUUID(c)
	if !ok {
		return
	}
	tier, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		writeModelTierError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tier})
}

func (h *ModelTierHandler) Create(c *gin.Context) {
	var request modelTierWriteRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeModelTierError(c, service.ErrInvalidModelTier)
		return
	}
	tier, err := h.service.Create(c.Request.Context(), service.CreateModelTierInput{
		Name: request.Name, Description: request.Description, Color: request.Color, SortOrder: request.SortOrder,
		Reason: request.Reason,
	})
	if err != nil {
		if errors.Is(err, service.ErrBudgetConfigSyncPending) && tier != nil {
			writeModelTierPersistedSyncPending(c, nil, tier)
			return
		}
		writeModelTierError(c, err)
		return
	}
	audit.SetResourceName(c, tier.Name)
	audit.SetBeforeAfter(c, nil, tier)
	c.JSON(http.StatusCreated, gin.H{"data": tier})
}

func (h *ModelTierHandler) Update(c *gin.Context) {
	id, ok := parseTierUUID(c)
	if !ok {
		return
	}
	var request struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Color       *string `json:"color"`
		SortOrder   *int    `json:"sort_order"`
		Reason      string  `json:"reason"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		writeModelTierError(c, service.ErrInvalidModelTier)
		return
	}
	before := h.getTierForAudit(c.Request.Context(), id, "update_before")
	tier, err := h.service.Update(c.Request.Context(), id, service.UpdateModelTierInput{
		Name: request.Name, Description: request.Description, Color: request.Color, SortOrder: request.SortOrder,
		Reason: request.Reason,
	})
	if err != nil {
		if errors.Is(err, service.ErrBudgetConfigSyncPending) && tier != nil {
			writeModelTierPersistedSyncPending(c, before, tier)
			return
		}
		writeModelTierError(c, err)
		return
	}
	audit.SetResourceName(c, tier.Name)
	audit.SetBeforeAfter(c, before, tier)
	c.JSON(http.StatusOK, gin.H{"data": tier})
}

func (h *ModelTierHandler) SetStatus(c *gin.Context) {
	id, ok := parseTierUUID(c)
	if !ok {
		return
	}
	var request struct {
		Enabled bool   `json:"enabled"`
		Reason  string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		writeModelTierError(c, service.ErrInvalidModelTier)
		return
	}
	before := h.getTierForAudit(c.Request.Context(), id, "status_before")
	tier, err := h.service.SetEnabled(c.Request.Context(), id, request.Enabled, request.Reason)
	if err != nil {
		if errors.Is(err, service.ErrBudgetConfigSyncPending) && tier != nil {
			writeModelTierPersistedSyncPending(c, before, tier)
			return
		}
		writeModelTierError(c, err)
		return
	}
	audit.SetResourceName(c, tier.Name)
	audit.SetBeforeAfter(c, before, tier)
	c.JSON(http.StatusOK, gin.H{"data": tier})
}

func (h *ModelTierHandler) Delete(c *gin.Context) {
	id, ok := parseTierUUID(c)
	if !ok {
		return
	}
	var request struct {
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		writeModelTierError(c, service.ErrReasonRequired)
		return
	}
	before := h.getTierForAudit(c.Request.Context(), id, "delete_before")
	if err := h.service.Delete(c.Request.Context(), id, request.Reason); err != nil {
		if errors.Is(err, service.ErrBudgetConfigSyncPending) {
			audit.SetBeforeAfter(c, before, nil)
			c.JSON(http.StatusAccepted, gin.H{
				"data": gin.H{"id": id}, "status": "persisted_sync_pending", "code": CodeBudgetConfigSyncPending,
			})
			return
		}
		writeModelTierError(c, err)
		return
	}
	audit.SetBeforeAfter(c, before, nil)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"id": id}})
}

func (h *ModelTierHandler) Archive(c *gin.Context) {
	id, ok := parseTierUUID(c)
	if !ok {
		return
	}
	var request struct {
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		writeModelTierError(c, service.ErrReasonRequired)
		return
	}
	before := h.getTierForAudit(c.Request.Context(), id, "archive_before")
	tier, err := h.service.Archive(c.Request.Context(), id, request.Reason)
	if err != nil {
		if errors.Is(err, service.ErrBudgetConfigSyncPending) && tier != nil {
			writeModelTierPersistedSyncPending(c, before, tier)
			return
		}
		writeModelTierError(c, err)
		return
	}
	audit.SetResourceName(c, tier.Name)
	audit.SetBeforeAfter(c, before, tier)
	c.JSON(http.StatusOK, gin.H{"data": tier})
}

func (h *ModelTierHandler) Reorder(c *gin.Context) {
	var request struct {
		IDs    []uuid.UUID `json:"ids"`
		Reason string      `json:"reason"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		writeModelTierError(c, service.ErrInvalidTierOrder)
		return
	}
	before := h.listTiersForAudit(c.Request.Context(), "reorder_before")
	if err := h.service.Reorder(c.Request.Context(), request.IDs, request.Reason); err != nil {
		if errors.Is(err, service.ErrBudgetConfigSyncPending) {
			after := h.listTiersForAudit(c.Request.Context(), "reorder_after_sync_pending")
			audit.SetBeforeAfter(c, before, after)
			c.JSON(http.StatusAccepted, gin.H{
				"data": gin.H{"ids": request.IDs}, "status": "persisted_sync_pending", "code": CodeBudgetConfigSyncPending,
			})
			return
		}
		writeModelTierError(c, err)
		return
	}
	after := h.listTiersForAudit(c.Request.Context(), "reorder_after")
	audit.SetBeforeAfter(c, before, after)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"ids": request.IDs}})
}

func (h *ModelTierHandler) SetGroupBudgetTier(c *gin.Context) {
	groupID64, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request", "code": CodeBadRequest, "message": "invalid group id"}})
		return
	}
	var request struct {
		ModelTierID *uuid.UUID `json:"model_tier_id"`
		Reason      string     `json:"reason"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		writeModelTierError(c, service.ErrInvalidModelTier)
		return
	}
	if h.groupService == nil {
		writeModelTierError(c, errors.New("group budget tier service unavailable"))
		return
	}
	before := h.getGroupForAudit(c.Request.Context(), uint(groupID64), "group_tier_before")
	if err := h.groupService.SetBudgetTier(c.Request.Context(), uint(groupID64), request.ModelTierID, request.Reason); err != nil {
		if errors.Is(err, service.ErrBudgetConfigSyncPending) {
			after := h.getGroupForAudit(c.Request.Context(), uint(groupID64), "group_tier_after_sync_pending")
			audit.SetBeforeAfter(c, before, after)
			c.JSON(http.StatusAccepted, gin.H{
				"data":   gin.H{"updated_group_ids": []uint{uint(groupID64)}, "model_tier_id": request.ModelTierID},
				"status": "persisted_sync_pending", "code": CodeBudgetConfigSyncPending,
			})
			return
		}
		writeModelTierError(c, err)
		return
	}
	after := h.getGroupForAudit(c.Request.Context(), uint(groupID64), "group_tier_after")
	audit.SetBeforeAfter(c, before, after)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"updated_group_ids": []uint{uint(groupID64)}, "model_tier_id": request.ModelTierID}})
}

func (h *ModelTierHandler) SetGroupBudgetTierBatch(c *gin.Context) {
	var request struct {
		GroupIDs    []uint     `json:"group_ids" binding:"required,min=1"`
		ModelTierID *uuid.UUID `json:"model_tier_id"`
		Reason      string     `json:"reason"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		writeModelTierError(c, service.ErrInvalidModelTier)
		return
	}
	if h.groupService == nil {
		writeModelTierError(c, errors.New("group budget tier service unavailable"))
		return
	}
	before := make([]*model.Group, 0, len(request.GroupIDs))
	for _, groupID := range request.GroupIDs {
		group := h.getGroupForAudit(c.Request.Context(), groupID, "group_batch_before")
		before = append(before, group)
	}
	if err := h.groupService.SetBudgetTierBatch(c.Request.Context(), request.GroupIDs, request.ModelTierID, request.Reason); err != nil {
		if errors.Is(err, service.ErrBudgetConfigSyncPending) {
			after := make([]*model.Group, 0, len(request.GroupIDs))
			for _, groupID := range request.GroupIDs {
				group := h.getGroupForAudit(c.Request.Context(), groupID, "group_batch_after_sync_pending")
				after = append(after, group)
			}
			audit.SetBeforeAfter(c, before, after)
			c.JSON(http.StatusAccepted, gin.H{
				"data":   gin.H{"updated_group_ids": request.GroupIDs, "model_tier_id": request.ModelTierID},
				"status": "persisted_sync_pending", "code": CodeBudgetConfigSyncPending,
			})
			return
		}
		writeModelTierError(c, err)
		return
	}
	after := make([]*model.Group, 0, len(request.GroupIDs))
	for _, groupID := range request.GroupIDs {
		group := h.getGroupForAudit(c.Request.Context(), groupID, "group_batch_after")
		after = append(after, group)
	}
	audit.SetBeforeAfter(c, before, after)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"updated_group_ids": request.GroupIDs, "model_tier_id": request.ModelTierID}})
}

func parseTierUUID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request", "code": CodeBadRequest, "message": "invalid model tier id"}})
		return uuid.Nil, false
	}
	return id, true
}

func writeModelTierPersistedSyncPending(c *gin.Context, before any, tier *model.ModelTier) {
	audit.SetResourceName(c, tier.Name)
	audit.SetBeforeAfter(c, before, tier)
	c.JSON(http.StatusAccepted, gin.H{
		"data": tier, "status": "persisted_sync_pending", "code": CodeBudgetConfigSyncPending,
	})
}

func (h *ModelTierHandler) getTierForAudit(ctx context.Context, id uuid.UUID, operation string) *model.ModelTier {
	tier, err := h.service.Get(ctx, id)
	if err != nil {
		slog.ErrorContext(ctx, "failed to capture model tier audit state",
			"operation", operation, "model_tier_id", id, "error", err)
	}
	return tier
}

func (h *ModelTierHandler) listTiersForAudit(ctx context.Context, operation string) []model.ModelTier {
	tiers, err := h.service.List(ctx, false)
	if err != nil {
		slog.ErrorContext(ctx, "failed to capture model tier audit state",
			"operation", operation, "error", err)
	}
	return tiers
}

func (h *ModelTierHandler) getGroupForAudit(ctx context.Context, id uint, operation string) *model.Group {
	group, err := h.groupService.GetByID(id)
	if err != nil {
		slog.ErrorContext(ctx, "failed to capture group tier audit state",
			"operation", operation, "group_id", id, "error", err)
	}
	return group
}

func writeModelTierError(c *gin.Context, err error) {
	status, kind, code, message := http.StatusInternalServerError, "internal_error", CodeInternalError, "internal server error"
	switch {
	case errors.Is(err, service.ErrModelTierNotFound):
		status, kind, code, message = http.StatusNotFound, "not_found", CodeModelTierNotFound, "model tier not found"
	case errors.Is(err, service.ErrModelTierNameExists):
		status, kind, code, message = http.StatusConflict, "conflict", CodeModelTierNameConflict, "model tier name already exists"
	case errors.Is(err, service.ErrModelTierReferenced):
		status, kind, code, message = http.StatusConflict, "conflict", CodeModelTierReferenced, "model tier is referenced"
	case errors.Is(err, service.ErrModelTierDisabled):
		status, kind, code, message = http.StatusUnprocessableEntity, "unprocessable_entity", CodeModelTierDisabled, "model tier is disabled"
	case errors.Is(err, service.ErrInvalidTierOrder):
		status, kind, code, message = http.StatusUnprocessableEntity, "unprocessable_entity", CodeInvalidModelTierOrder, "invalid model tier order"
	case errors.Is(err, service.ErrInvalidModelTier):
		status, kind, code, message = http.StatusBadRequest, "invalid_request", CodeBadRequest, "invalid model tier"
	case errors.Is(err, service.ErrReasonRequired):
		status, kind, code, message = http.StatusUnprocessableEntity, "unprocessable_entity", CodeBadRequest, "reason is required"
	case errors.Is(err, service.ErrGroupNotFound):
		status, kind, code, message = http.StatusNotFound, "not_found", CodeGroupNotFound, "group not found"
	case errors.Is(err, service.ErrBudgetConfigSyncPending):
		status, kind, code, message = http.StatusAccepted, "sync_pending", CodeBudgetConfigSyncPending, "budget configuration synchronization is pending"
	}
	c.JSON(status, gin.H{"error": gin.H{"type": kind, "code": code, "message": message}})
}
