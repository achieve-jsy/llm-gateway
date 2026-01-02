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

	var req struct {
		ScopeType       string  `json:"scope_type" binding:"required"`
		ScopeID         string  `json:"scope_id" binding:"required"`
		PeriodType      string  `json:"period_type"`
		RequestedAmount float64 `json:"requested_amount" binding:"required"`
		Reason          string  `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	requesterID, _ := uuid.Parse(c.GetString("user_id"))
	// RBAC 范围校验
	if req.ScopeType == model.BudgetScopeMember {
		if req.ScopeID != requesterID.String() && !actor.IsGlobal {
			if !actor.IsManager() {
				c.JSON(http.StatusForbidden, gin.H{"error": "只能为自己发起成员额度申请"})
				return
			}
			ok, err := h.svc.MemberInDeptIDs(req.ScopeID, actor.DeptIDs)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			if !ok {
				c.JSON(http.StatusForbidden, gin.H{"error": "无权为该成员发起扩容申请"})
				return
			}
		}
	} else if !actor.IsManager() {
		c.JSON(http.StatusForbidden, gin.H{"error": "需要管理员权限"})
		return
	} else if !actor.IsGlobal {
		if (req.ScopeType == model.BudgetScopeDeptDaily || req.ScopeType == model.BudgetScopeDeptService) &&
			!actor.HasDept(req.ScopeID) {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权为该部门发起扩容申请"})
			return
		}
		if req.ScopeType == model.BudgetScopeStudio && !actor.HasStudio(req.ScopeID) {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权为该工作室发起扩容申请"})
			return
		}
	}

	periodType := req.PeriodType
	if periodType == "" {
		periodType = model.ExpansionPeriodMonthly
	}
	result, err := h.svc.CreateRequest(c.Request.Context(), req.ScopeType, req.ScopeID,
		requesterID, 0, req.RequestedAmount, periodType, req.Reason)
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

	requestID := c.Param("id")
	var req struct {
		Action  string `json:"action" binding:"required"`
		Comment string `json:"comment"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Action != model.ExpansionActionCancel && !actor.IsManager() {
		c.JSON(http.StatusForbidden, gin.H{"error": "需要管理员权限"})
		return
	}

	approverID, _ := uuid.Parse(c.GetString("user_id"))
	if err := h.svc.DecideScoped(c.Request.Context(), requestID, approverID, req.Action, req.Comment, actor.IsGlobal, actor.DeptIDs, actor.StudioIDs); err != nil {
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

// ListPending GET /api/v1/expansion-requests/pending
// 查询待审批的申请（按当前 actor 的角色过滤级别）。
// 返回数据带 current_usage_ratio，方便审批人判断合理性。
func (h *ExpansionHandler) ListPending(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)
	if !actor.IsManager() {
		c.JSON(http.StatusForbidden, gin.H{"error": "需要管理员权限"})
		return
	}

	level := model.ExpansionLevelDept
	if actor.IsGlobal {
		level = model.ExpansionLevelPlatform
	} else if len(actor.StudioIDs) > 0 {
		level = model.ExpansionLevelStudio
	}

	items, err := h.svc.ListPendingScoped(c.Request.Context(), level, actor.IsGlobal, actor.DeptIDs, actor.StudioIDs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 补查每条申请对应 scope 的当前使用率
	type PendingItem struct {
		model.ExpansionRequest
		CurrentUsageRatio float64 `json:"current_usage_ratio"`
	}
	result := make([]PendingItem, 0, len(items))
	for _, item := range items {
		pi := PendingItem{ExpansionRequest: item}
		budget, _ := h.svc.GetBudgetUsedRatio(c.Request.Context(), item.ScopeType, item.ScopeID)
		pi.CurrentUsageRatio = budget
		result = append(result, pi)
	}
	c.JSON(http.StatusOK, gin.H{"items": result})
}

// BatchDecide POST /api/v1/expansion-requests/batch-decision
// 批量审批（通过/驳回），驳回时每条必须有 comment。
func (h *ExpansionHandler) BatchDecide(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)
	if !actor.IsManager() {
		c.JSON(http.StatusForbidden, gin.H{"error": "需要管理员权限"})
		return
	}

	var req struct {
		Decisions []struct {
			ID      string `json:"id" binding:"required"`
			Action  string `json:"action" binding:"required"`
			Comment string `json:"comment"`
		} `json:"decisions" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	approverID, _ := uuid.Parse(c.GetString("user_id"))
	results := make([]gin.H, 0, len(req.Decisions))
	for _, d := range req.Decisions {
		// 驳回必须有理由
		if d.Action == model.ExpansionActionReject && d.Comment == "" {
			results = append(results, gin.H{"id": d.ID, "ok": false, "error": "驳回必须填写理由"})
			continue
		}
		if err := h.svc.DecideScoped(c.Request.Context(), d.ID, approverID, d.Action, d.Comment, actor.IsGlobal, actor.DeptIDs, actor.StudioIDs); err != nil {
			results = append(results, gin.H{"id": d.ID, "ok": false, "error": err.Error()})
		} else {
			results = append(results, gin.H{"id": d.ID, "ok": true})
		}
	}
	c.JSON(http.StatusOK, gin.H{"results": results})
}

// ListSteps GET /api/v1/expansion-requests/:id/steps
// 查询某扩容申请的审批流水。
func (h *ExpansionHandler) ListSteps(c *gin.Context) {
	requestID := c.Param("id")
	steps, err := h.svc.ListSteps(c.Request.Context(), requestID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"steps": steps})
}
