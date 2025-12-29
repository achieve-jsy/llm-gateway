package handler

import (
	"net/http"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ExpansionHandler 处理预算扩容申请的发起与审批。
type ExpansionHandler struct {
	svc  *service.ExpansionService
	rbac *rbac.Checker
}

func NewExpansionHandler(svc *service.ExpansionService, rbacChecker *rbac.Checker) *ExpansionHandler {
	return &ExpansionHandler{svc: svc, rbac: rbacChecker}
}

// Create POST /api/v1/expansion-requests
// 部门负责人发起扩容申请。
func (h *ExpansionHandler) Create(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)
	if !actor.IsManager() {
		c.JSON(http.StatusForbidden, gin.H{"error": "需要管理员权限"})
		return
	}

	var req struct {
		ScopeType       string  `json:"scope_type" binding:"required"`
		ScopeID         string  `json:"scope_id" binding:"required"`
		RequestedAmount float64 `json:"requested_amount" binding:"required"`
		Reason          string  `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// RBAC 范围校验
	if !actor.IsGlobal {
		if (req.ScopeType == model.BudgetScopeDeptDaily || req.ScopeType == model.BudgetScopeDeptService) &&
			!actor.HasDept(req.ScopeID) {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权为该部门发起扩容申请"})
			return
		}
	}

	requesterID, _ := uuid.Parse(c.GetString("user_id"))
	result, err := h.svc.CreateRequest(c.Request.Context(), req.ScopeType, req.ScopeID,
		requesterID, 0, req.RequestedAmount, req.Reason)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, result)
}

// Decide POST /api/v1/expansion-requests/:id/decision
// 审批人做决策（approve/reject/escalate）。
func (h *ExpansionHandler) Decide(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)
	if !actor.IsManager() {
		c.JSON(http.StatusForbidden, gin.H{"error": "需要管理员权限"})
		return
	}

	requestID := c.Param("id")
	var req struct {
		Action  string `json:"action" binding:"required"`
		Comment string `json:"comment"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	approverID, _ := uuid.Parse(c.GetString("user_id"))
	if err := h.svc.Decide(c.Request.Context(), requestID, approverID, req.Action, req.Comment); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ListMine GET /api/v1/expansion-requests?role=requester
// 查询当前用户发起的申请。
func (h *ExpansionHandler) ListMine(c *gin.Context) {
	userID := c.GetString("user_id")
	status := c.Query("status")
	items, err := h.svc.ListForRequester(c.Request.Context(), userID, status)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// ListPending GET /api/v1/expansion-requests?role=approver
// 查询待审批的申请（按当前 actor 的角色过滤级别）。
func (h *ExpansionHandler) ListPending(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)
	if !actor.IsManager() {
		c.JSON(http.StatusForbidden, gin.H{"error": "需要管理员权限"})
		return
	}

	level := model.ExpansionLevelStudio
	if actor.IsGlobal {
		level = model.ExpansionLevelPlatform
	}

	items, err := h.svc.ListPending(c.Request.Context(), level)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
