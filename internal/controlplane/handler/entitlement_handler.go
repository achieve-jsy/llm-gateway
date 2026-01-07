package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type EntitlementServiceInterface interface {
	SetOpenMode(ctx context.Context, groupID uint, openMode string, operatorID uuid.UUID) error
	GrantStudio(ctx context.Context, groupID uint, studioID string, grantedBy uuid.UUID) (*service.EntitlementItem, error)
	GrantDept(ctx context.Context, groupID uint, deptID, studioID string, grantedBy uuid.UUID, actor *rbac.Actor) (*service.EntitlementItem, error)
	Revoke(ctx context.Context, entitlementID uuid.UUID, operatorID uuid.UUID) error
	ListEntitlements(ctx context.Context, groupID uint, scopeType string) ([]service.EntitlementItem, error)
}

type DeptStudioLookup interface {
	FindStudioIDByDeptID(deptID string) (string, error)
}

type EntitlementHandler struct {
	svc        EntitlementServiceInterface
	deptLookup DeptStudioLookup
}

func NewEntitlementHandler(svc EntitlementServiceInterface, deptLookup DeptStudioLookup) *EntitlementHandler {
	return &EntitlementHandler{svc: svc, deptLookup: deptLookup}
}

func (h *EntitlementHandler) SetOpenMode(c *gin.Context) {
	groupID, err := parseGroupID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("invalid_request", "invalid group ID", CodeBadRequest))
		return
	}

	var body struct {
		OpenMode string `json:"open_mode" binding:"required,oneof=public controlled"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, errResp("invalid_request", err.Error(), CodeBadRequest))
		return
	}

	operatorID, _ := uuid.Parse(c.GetString("user_id"))

	if err := h.svc.SetOpenMode(c.Request.Context(), groupID, body.OpenMode, operatorID); err != nil {
		h.respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": gin.H{"group_id": groupID, "open_mode": body.OpenMode}})
}

func (h *EntitlementHandler) Grant(c *gin.Context) {
	groupID, err := parseGroupID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("invalid_request", "invalid group ID", CodeBadRequest))
		return
	}

	var body struct {
		ScopeType string `json:"scope_type" binding:"required,oneof=studio dept"`
		ScopeID   string `json:"scope_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, errResp("invalid_request", err.Error(), CodeBadRequest))
		return
	}

	actor := rbac.GetActor(c)
	grantedBy, _ := uuid.Parse(c.GetString("user_id"))

	if body.ScopeType == "studio" {
		if !actor.IsGlobal {
			c.JSON(http.StatusForbidden, errResp("forbidden",
				"only platform admin can grant studio entitlements", CodeEntitlementForbidden))
			return
		}
		item, err := h.svc.GrantStudio(c.Request.Context(), groupID, body.ScopeID, grantedBy)
		if err != nil {
			h.respondError(c, err)
			return
		}
		c.JSON(http.StatusCreated, gin.H{"data": item})
		return
	}

	studioID := ""
	if h.deptLookup != nil {
		if sid, err := h.deptLookup.FindStudioIDByDeptID(body.ScopeID); err == nil {
			studioID = sid
		}
	}

	item, err := h.svc.GrantDept(c.Request.Context(), groupID, body.ScopeID, studioID, grantedBy, &actor)
	if err != nil {
		h.respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": item})
}

func (h *EntitlementHandler) Revoke(c *gin.Context) {
	eid, err := uuid.Parse(c.Param("eid"))
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("invalid_request", "invalid entitlement ID", CodeBadRequest))
		return
	}

	operatorID, _ := uuid.Parse(c.GetString("user_id"))

	if err := h.svc.Revoke(c.Request.Context(), eid, operatorID); err != nil {
		h.respondError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *EntitlementHandler) List(c *gin.Context) {
	groupID, err := parseGroupID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, errResp("invalid_request", "invalid group ID", CodeBadRequest))
		return
	}

	scopeType := c.DefaultQuery("scope_type", "")

	items, err := h.svc.ListEntitlements(c.Request.Context(), groupID, scopeType)
	if err != nil {
		h.respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": gin.H{"items": items}})
}

func (h *EntitlementHandler) respondError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrGroupNotFound):
		c.JSON(http.StatusNotFound, errResp("not_found", "group not found", CodeGroupNotFound))
	case errors.Is(err, service.ErrEntitlementNotFound):
		c.JSON(http.StatusNotFound, errResp("not_found", "entitlement not found", CodeEntitlementNotFound))
	case errors.Is(err, service.ErrEntitlementExists):
		c.JSON(http.StatusConflict, errResp("conflict", "entitlement already exists", CodeEntitlementDuplicate))
	case errors.Is(err, service.ErrGroupMustBeControlled):
		c.JSON(http.StatusUnprocessableEntity, errResp("unprocessable", "group must be in controlled mode", CodeGroupNotControlled))
	case errors.Is(err, service.ErrStudioNotGranted):
		c.JSON(http.StatusUnprocessableEntity, errResp("unprocessable", "studio must be granted first", CodeGroupNotControlled))
	case errors.Is(err, service.ErrEntitlementForbidden):
		c.JSON(http.StatusForbidden, errResp("forbidden", "insufficient permission", CodeEntitlementForbidden))
	default:
		c.JSON(http.StatusInternalServerError, errResp("internal_error", "internal error", CodeInternalError))
	}
}

func errResp(errType, message, code string) gin.H {
	return gin.H{"error": gin.H{"type": errType, "message": message, "code": code}}
}
