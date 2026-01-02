package handler

import (
	"net/http"
	"strconv"

	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// EntitlementHandler 处理模型组开放模式设置与两级授权（工作室+部门）。
type EntitlementHandler struct {
	svc  *service.EntitlementService
	rbac *rbac.Checker
}

func NewEntitlementHandler(svc *service.EntitlementService, rbacChecker *rbac.Checker) *EntitlementHandler {
	return &EntitlementHandler{svc: svc, rbac: rbacChecker}
}

// SetOpenMode PUT /api/v1/groups/:id/open-mode
// 设置模型组开放模式（仅平台 admin）。
func (h *EntitlementHandler) SetOpenMode(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)
	if !actor.IsGlobal {
		c.JSON(http.StatusForbidden, gin.H{"error": "只有平台管理员可以设置开放模式"})
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group id"})
		return
	}

	var req struct {
		OpenMode string `json:"open_mode" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.svc.SetOpenMode(c.Request.Context(), uint(id), req.OpenMode); err != nil {
		switch err {
		case service.ErrInvalidOpenMode:
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case service.ErrGroupNotFound:
			c.JSON(http.StatusNotFound, gin.H{"error": "group not found"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Grant POST /api/v1/group-entitlements
// 下放/开通授权（studio_admin 下放给部门，平台 admin 授权工作室）。
// Body: { group_id, scope_type, scope_id }
func (h *EntitlementHandler) Grant(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)

	var req struct {
		GroupID   uint   `json:"group_id" binding:"required"`
		ScopeType string `json:"scope_type" binding:"required"` // studio | dept
		ScopeID   string `json:"scope_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// RBAC 范围校验：
	// - scope_type=studio：仅平台 admin 可授权给工作室
	// - scope_type=dept：studio_admin（管辖工作室下的部门）或 admin
	switch req.ScopeType {
	case "studio":
		if !actor.IsGlobal {
			c.JSON(http.StatusForbidden, gin.H{"error": "只有平台管理员可以授权工作室"})
			return
		}
	case "dept":
		if !actor.IsGlobal && !actor.HasDept(req.ScopeID) {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权为该部门开通授权"})
			return
		}
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "scope_type must be studio or dept"})
		return
	}

	grantedBy, _ := uuid.Parse(c.GetString("user_id"))
	if err := h.svc.Grant(c.Request.Context(), req.GroupID, req.ScopeType, req.ScopeID, grantedBy); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Revoke DELETE /api/v1/group-entitlements
// 撤销授权（软撤销）。
// Body: { group_id, scope_type, scope_id }
func (h *EntitlementHandler) Revoke(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)

	var req struct {
		GroupID   uint   `json:"group_id" binding:"required"`
		ScopeType string `json:"scope_type" binding:"required"`
		ScopeID   string `json:"scope_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	switch req.ScopeType {
	case "studio":
		if !actor.IsGlobal {
			c.JSON(http.StatusForbidden, gin.H{"error": "只有平台管理员可以撤销工作室授权"})
			return
		}
	case "dept":
		if !actor.IsGlobal && !actor.HasDept(req.ScopeID) {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权撤销该部门授权"})
			return
		}
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "scope_type must be studio or dept"})
		return
	}

	if err := h.svc.Revoke(c.Request.Context(), req.GroupID, req.ScopeType, req.ScopeID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ListByGroup GET /api/v1/groups/:id/entitlements
// 查询某模型组的所有授权记录（平台 admin + studio_admin 可见）。
func (h *EntitlementHandler) ListByGroup(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)
	if !actor.IsManager() {
		c.JSON(http.StatusForbidden, gin.H{"error": "需要管理员权限"})
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid group id"})
		return
	}

	list, err := h.svc.ListByGroup(c.Request.Context(), uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": list})
}
