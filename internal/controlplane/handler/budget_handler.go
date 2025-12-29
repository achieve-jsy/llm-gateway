package handler

import (
	"net/http"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// BudgetHandler 处理预算额度的查询与设置。
type BudgetHandler struct {
	svc  *service.BudgetService
	rbac *rbac.Checker
}

func NewBudgetHandler(svc *service.BudgetService, rbacChecker *rbac.Checker) *BudgetHandler {
	return &BudgetHandler{svc: svc, rbac: rbacChecker}
}

// List GET /api/v1/budgets?scope_type=xxx&scope_id=xxx
// 按管辖范围列出预算配置（admin 查全公司，manager 查自己范围）。
func (h *BudgetHandler) List(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)
	if !actor.IsManager() {
		c.JSON(http.StatusForbidden, gin.H{"error": "需要管理员权限"})
		return
	}

	scopeType := c.Query("scope_type")
	scopeID := c.Query("scope_id")

	// 单条精确查询
	if scopeType != "" && scopeID != "" {
		// RBAC 范围校验
		if !actor.IsGlobal {
			if (scopeType == model.BudgetScopeDeptDaily || scopeType == model.BudgetScopeDeptService) && !actor.HasDept(scopeID) {
				c.JSON(http.StatusForbidden, gin.H{"error": "无权查看该部门预算"})
				return
			}
			if scopeType == model.BudgetScopeStudio && !actor.HasStudio(scopeID) {
				c.JSON(http.StatusForbidden, gin.H{"error": "无权查看该工作室预算"})
				return
			}
		}
		item, err := h.svc.GetBudget(c.Request.Context(), scopeType, scopeID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": []service.BudgetItem{*item}})
		return
	}

	// 批量查询：按管辖域构造 scope 列表
	var scopeTypes []string
	var scopeIDs []string
	if actor.IsGlobal {
		// admin 可查全部类型，此处先返回错误提示需要指定 scope_type
		c.JSON(http.StatusBadRequest, gin.H{"error": "请指定 scope_type 和 scope_id 查询"})
		return
	}
	// manager：dept + studio 维度
	scopeTypes = []string{
		model.BudgetScopeDeptDaily, model.BudgetScopeDeptService,
		model.BudgetScopeStudio,
	}
	scopeIDs = append(scopeIDs, actor.DeptIDs...)
	scopeIDs = append(scopeIDs, actor.StudioIDs...)

	items, err := h.svc.ListScoped(c.Request.Context(), scopeTypes, scopeIDs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// Set PUT /api/v1/budgets
// 设置/更新预算额度（按管辖域 RBAC 过滤）。
func (h *BudgetHandler) Set(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)
	if !actor.IsManager() {
		c.JSON(http.StatusForbidden, gin.H{"error": "需要管理员权限"})
		return
	}

	var req struct {
		ScopeType string  `json:"scope_type" binding:"required"`
		ScopeID   string  `json:"scope_id" binding:"required"`
		AmountUSD float64 `json:"amount_usd" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// RBAC 数据范围校验
	if !actor.IsGlobal {
		switch req.ScopeType {
		case model.BudgetScopeDeptDaily, model.BudgetScopeDeptService:
			if !actor.HasDept(req.ScopeID) {
				c.JSON(http.StatusForbidden, gin.H{"error": "无权设置该部门预算"})
				return
			}
		case model.BudgetScopeStudio:
			if !actor.HasStudio(req.ScopeID) {
				c.JSON(http.StatusForbidden, gin.H{"error": "无权设置该工作室预算"})
				return
			}
		case model.BudgetScopeCompany:
			// 公司层只有 admin 能改
			c.JSON(http.StatusForbidden, gin.H{"error": "只有平台管理员可以设置公司层预算"})
			return
		}
	}

	actorID, _ := uuid.Parse(c.GetString("user_id"))
	if err := h.svc.SetBudget(c.Request.Context(), req.ScopeType, req.ScopeID, req.AmountUSD, actorID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "scope_type": req.ScopeType, "scope_id": req.ScopeID, "amount_usd": req.AmountUSD})
}

// MyBudget GET /api/v1/my/budget
// 员工查看自己的预算链（成员额度 + 部门 + 工作室 + 公司）。
func (h *BudgetHandler) MyBudget(c *gin.Context) {
	deptID := c.GetString("dept_id")
	studioID := c.GetString("studio_id")
	userID := c.GetString("user_id")

	if deptID == "" {
		c.JSON(http.StatusOK, gin.H{"chain": []interface{}{}})
		return
	}

	period := time.Now().Format("2006-01")
	type chainItem struct {
		ScopeType string  `json:"scope_type"`
		Label     string  `json:"label"`
		AmountUSD float64 `json:"amount_usd"`
		UsedUSD   float64 `json:"used_usd"`
		UsedRatio float64 `json:"used_ratio"`
		Period    string  `json:"period"`
	}

	scopes := []struct {
		scopeType, scopeID, label string
	}{
		{model.BudgetScopeMember, userID, "我的额度"},
		{model.BudgetScopeDeptDaily, deptID, "部门成员日常"},
		{model.BudgetScopeStudio, studioID, "工作室"},
		{model.BudgetScopeCompany, model.BudgetScopeGlobal, "全公司"},
	}

	chain := make([]chainItem, 0, len(scopes))
	for _, s := range scopes {
		if s.scopeID == "" {
			continue
		}
		item := chainItem{ScopeType: s.scopeType, Label: s.label, Period: period, AmountUSD: -1}
		alloc, _ := h.svc.GetBudget(c.Request.Context(), s.scopeType, s.scopeID)
		if alloc != nil {
			item.AmountUSD = alloc.AmountUSD
			item.UsedUSD = alloc.UsedUSD
			item.UsedRatio = alloc.UsedRatio
		}
		// 成员层未配额度则跳过
		if s.scopeType == model.BudgetScopeMember && item.AmountUSD < 0 {
			continue
		}
		chain = append(chain, item)
	}
	c.JSON(http.StatusOK, gin.H{"chain": chain})
}
