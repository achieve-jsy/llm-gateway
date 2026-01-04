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
	svc      *service.BudgetService
	alertSvc *service.BudgetAlertService
	rbac     *rbac.Checker
}

func NewBudgetHandler(svc *service.BudgetService, rbacChecker *rbac.Checker) *BudgetHandler {
	return &BudgetHandler{svc: svc, rbac: rbacChecker}
}

// SetAlertService 注入告警服务（供 EvaluateAlert 使用，可选）。
func (h *BudgetHandler) SetAlertService(svc *service.BudgetAlertService) {
	h.alertSvc = svc
}

// List GET /api/v1/budgets?view=company|studio|department&scope_type=xxx&scope_id=xxx
// 按管辖范围列出预算配置（admin 查全公司，manager 查自己范围）。
// view 参数用于前端三 Tab 布局：
//   - company: 返回公司 + 各工作室分配（admin only）
//   - studio: 返回当前工作室 + 下属部门分配（studio_admin+）
//   - department: 返回当前部门详情 + 成员配额（dept_admin+）
//   - 空：兼容旧逻辑，按 scope_type/scope_id 过滤
func (h *BudgetHandler) List(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)
	if !actor.IsManager() {
		c.JSON(http.StatusForbidden, gin.H{"error": "需要管理员权限"})
		return
	}

	view := c.Query("view")
	scopeType := c.Query("scope_type")
	scopeID := c.Query("scope_id")

	// 使用 view 参数时走新逻辑
	if view != "" {
		var scopeTypes []string
		var scopeIDs []string
		var userDeptMap map[string]string // user_id → dept_id，仅 department 视图使用

		switch view {
		case "company":
			// 公司总览：只有 admin 可见
			if !actor.IsGlobal {
				c.JSON(http.StatusForbidden, gin.H{"error": "只有平台管理员可以查看公司总览"})
				return
			}
			scopeTypes = []string{model.BudgetScopeCompany, model.BudgetScopeStudio}
			// 不限 scopeIDs → 返回全部工作室

		case "studio":
			// 工作室视图：studio_admin+ 可见
			if !actor.IsGlobal && len(actor.StudioIDs) == 0 {
				c.JSON(http.StatusForbidden, gin.H{"error": "无工作室管理权限"})
				return
			}
			scopeTypes = []string{model.BudgetScopeStudio, model.BudgetScopeDeptDaily, model.BudgetScopeDeptService}
			if scopeID != "" {
				// admin 下钻到单个工作室：返回该工作室 + 其下所有部门的预算
				if !actor.IsGlobal && !actor.HasStudio(scopeID) {
					c.JSON(http.StatusForbidden, gin.H{"error": "无权查看该工作室"})
					return
				}
			deptIDs, err := h.svc.ListDeptIDsByStudioID(scopeID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			scopeIDs = append(scopeIDs, scopeID)
			scopeIDs = append(scopeIDs, deptIDs...)
			} else if !actor.IsGlobal {
				// 返回自己管辖的工作室 + 下属部门
				scopeIDs = append(scopeIDs, actor.StudioIDs...)
				scopeIDs = append(scopeIDs, actor.DeptIDs...)
			}

		case "department":
			// 部门视图：dept_admin+ 可见
			// userDeptMap: user_id → dept_id，用于给 member item 附 ParentScopeID
			userDeptMap = make(map[string]string)
			scopeTypes = []string{model.BudgetScopeDeptDaily, model.BudgetScopeDeptService, model.BudgetScopeMember}
			if scopeID != "" {
				if !actor.IsGlobal && !actor.HasDept(scopeID) {
					c.JSON(http.StatusForbidden, gin.H{"error": "无权查看该部门预算"})
					return
				}
				scopeIDs = append(scopeIDs, scopeID)
				memberIDs, err := h.svc.ListMemberIDsByDeptID(scopeID)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
					return
				}
				for _, mID := range memberIDs {
					userDeptMap[mID] = scopeID
				}
				scopeIDs = append(scopeIDs, memberIDs...)
			} else if !actor.IsGlobal {
				scopeIDs = append(scopeIDs, actor.DeptIDs...)
				for _, deptID := range actor.DeptIDs {
					memberIDs, err := h.svc.ListMemberIDsByDeptID(deptID)
					if err != nil {
						c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
						return
					}
					for _, mID := range memberIDs {
						userDeptMap[mID] = deptID
					}
					scopeIDs = append(scopeIDs, memberIDs...)
				}
			}

		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid view parameter, must be company|studio|department"})
			return
		}

		items, err := h.svc.ListScoped(c.Request.Context(), scopeTypes, scopeIDs)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		// 为 member item 填充所属部门（多部门场景路由跳转使用）
		if len(userDeptMap) > 0 {
			for i := range items {
				if items[i].ScopeType == model.BudgetScopeMember {
					items[i].ParentScopeID = userDeptMap[items[i].ScopeID]
				}
			}
		}
		c.JSON(http.StatusOK, gin.H{"items": items, "view": view})
		return
	}

	// 兼容旧逻辑：单条精确查询
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
		if scopeType == "" {
			scopeTypes = []string{
				model.BudgetScopeCompany, model.BudgetScopeStudio,
				model.BudgetScopeDeptDaily, model.BudgetScopeDeptService,
				model.BudgetScopeMember,
			}
		} else {
			scopeTypes = []string{scopeType}
		}
	} else {
		scopeTypes = []string{
			model.BudgetScopeDeptDaily, model.BudgetScopeDeptService,
			model.BudgetScopeStudio,
		}
		scopeIDs = append(scopeIDs, actor.DeptIDs...)
		scopeIDs = append(scopeIDs, actor.StudioIDs...)
	}

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
		ScopeType       string  `json:"scope_type" binding:"required"`
		ScopeID         string  `json:"scope_id" binding:"required"`
		AmountUSD       float64 `json:"amount_usd" binding:"required"`
		DailyAmountUSD  float64 `json:"daily_amount_usd"`  // 可选，默认 -1（不限）
		WeeklyAmountUSD float64 `json:"weekly_amount_usd"` // 可选，默认 -1（不限）
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// 未传日/周限额时默认 -1（不限）
	if req.DailyAmountUSD == 0 {
		req.DailyAmountUSD = -1
	}
	if req.WeeklyAmountUSD == 0 {
		req.WeeklyAmountUSD = -1
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
		case model.BudgetScopeMember:
			ok, err := h.svc.MemberInDeptIDs(req.ScopeID, actor.DeptIDs)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			if !ok {
				c.JSON(http.StatusForbidden, gin.H{"error": "无权设置该成员预算"})
				return
			}
		}
	}

	actorID, _ := uuid.Parse(c.GetString("user_id"))
	if err := h.svc.SetBudget(c.Request.Context(), req.ScopeType, req.ScopeID, req.AmountUSD, req.DailyAmountUSD, req.WeeklyAmountUSD, actorID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "scope_type": req.ScopeType, "scope_id": req.ScopeID, "amount_usd": req.AmountUSD, "daily_amount_usd": req.DailyAmountUSD, "weekly_amount_usd": req.WeeklyAmountUSD})
}

// MyBudget GET /api/v1/my/budget
// 员工查看自己的预算链（成员额度 + 部门 + 工作室 + 公司）。
func (h *BudgetHandler) MyBudget(c *gin.Context) {
	deptID := c.GetString("user_dept_id")
	studioID := c.GetString("user_studio_id")
	userID := c.GetString("user_id")

	if deptID == "" {
		c.JSON(http.StatusOK, gin.H{"chain": []interface{}{}})
		return
	}

	period := time.Now().Format("2006-01")
	type chainItem struct {
		ScopeType       string  `json:"scope_type"`
		Label           string  `json:"label"`
		AmountUSD       float64 `json:"amount_usd"`
		DailyAmountUSD  float64 `json:"daily_amount_usd"`
		WeeklyAmountUSD float64 `json:"weekly_amount_usd"`
		UsedUSD         float64 `json:"used_usd"`
		DailyUsedUSD    float64 `json:"daily_used_usd"`
		WeeklyUsedUSD   float64 `json:"weekly_used_usd"`
		UsedRatio       float64 `json:"used_ratio"`
		Period          string  `json:"period"`
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
		item := chainItem{ScopeType: s.scopeType, Label: s.label, Period: period, AmountUSD: -1, DailyAmountUSD: -1, WeeklyAmountUSD: -1}
		alloc, _ := h.svc.GetBudget(c.Request.Context(), s.scopeType, s.scopeID)
		if alloc != nil {
			item.AmountUSD = alloc.AmountUSD
			item.DailyAmountUSD = alloc.DailyAmountUSD
			item.WeeklyAmountUSD = alloc.WeeklyAmountUSD
			item.UsedUSD = alloc.UsedUSD
			item.DailyUsedUSD = alloc.DailyUsedUSD
			item.WeeklyUsedUSD = alloc.WeeklyUsedUSD
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

// EvaluateAlert POST /internal/v1/budget/evaluate-alert
// 供 gateway 调用，触发预算阈值告警评估（阈值 80%/100% + 去重 + 通知）。
// Body: {"scope_type": "dept_daily", "scope_id": "..."}
// 该接口幂等且异步友好（控制面内部做去重）。
// 由 InternalAuth 鉴权，gateway 通过 X-Internal-Token 调用。
func (h *BudgetHandler) EvaluateAlert(c *gin.Context) {
	var req struct {
		ScopeType string `json:"scope_type" binding:"required"`
		ScopeID   string `json:"scope_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 调用告警服务做阈值评估（需注入 alertSvc）
	if h.alertSvc != nil {
		h.alertSvc.EvaluateAndAlert(c.Request.Context(), req.ScopeType, req.ScopeID)
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// BatchSet PUT /api/v1/budgets/batch
// 批量设置预算额度（前端行内编辑统一提交用）。
// 同一请求中所有变更要么全部成功，要么全部失败（按上级可分配余额校验）。
func (h *BudgetHandler) BatchSet(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)
	if !actor.IsManager() {
		c.JSON(http.StatusForbidden, gin.H{"error": "需要管理员权限"})
		return
	}

	var req struct {
		Items []struct {
			ScopeType       string  `json:"scope_type" binding:"required"`
			ScopeID         string  `json:"scope_id" binding:"required"`
			AmountUSD       float64 `json:"amount_usd"`
			DailyAmountUSD  float64 `json:"daily_amount_usd"`
			WeeklyAmountUSD float64 `json:"weekly_amount_usd"`
		} `json:"items" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	actorID, _ := uuid.Parse(c.GetString("user_id"))
	results := make([]gin.H, 0, len(req.Items))

	for _, item := range req.Items {
		// RBAC 校验
		if !actor.IsGlobal {
			switch item.ScopeType {
			case model.BudgetScopeDeptDaily, model.BudgetScopeDeptService:
				if !actor.HasDept(item.ScopeID) {
					results = append(results, gin.H{"scope_type": item.ScopeType, "scope_id": item.ScopeID, "ok": false, "error": "无权设置该部门预算"})
					continue
				}
			case model.BudgetScopeStudio:
				if !actor.HasStudio(item.ScopeID) {
					results = append(results, gin.H{"scope_type": item.ScopeType, "scope_id": item.ScopeID, "ok": false, "error": "无权设置该工作室预算"})
					continue
				}
			case model.BudgetScopeCompany:
				results = append(results, gin.H{"scope_type": item.ScopeType, "scope_id": item.ScopeID, "ok": false, "error": "只有平台管理员可以设置公司层预算"})
				continue
			case model.BudgetScopeMember:
				ok, err := h.svc.MemberInDeptIDs(item.ScopeID, actor.DeptIDs)
				if err != nil {
					results = append(results, gin.H{"scope_type": item.ScopeType, "scope_id": item.ScopeID, "ok": false, "error": err.Error()})
					continue
				}
				if !ok {
					results = append(results, gin.H{"scope_type": item.ScopeType, "scope_id": item.ScopeID, "ok": false, "error": "无权设置该成员预算"})
					continue
				}
			}
		}

		daily := item.DailyAmountUSD
		if daily == 0 {
			daily = -1
		}
		weekly := item.WeeklyAmountUSD
		if weekly == 0 {
			weekly = -1
		}

		if err := h.svc.SetBudget(c.Request.Context(), item.ScopeType, item.ScopeID, item.AmountUSD, daily, weekly, actorID); err != nil {
			results = append(results, gin.H{"scope_type": item.ScopeType, "scope_id": item.ScopeID, "ok": false, "error": err.Error()})
		} else {
			results = append(results, gin.H{"scope_type": item.ScopeType, "scope_id": item.ScopeID, "ok": true})
		}
	}
	c.JSON(http.StatusOK, gin.H{"results": results})
}

// Delete DELETE /api/v1/budgets
// 删除成员的单独配额，还原为共享部门预算池。仅支持 scope_type=member。
func (h *BudgetHandler) Delete(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)
	if !actor.IsManager() {
		c.JSON(http.StatusForbidden, gin.H{"error": "需要管理员权限"})
		return
	}

	var req struct {
		ScopeType string `json:"scope_type" binding:"required"`
		ScopeID   string `json:"scope_id"   binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.ScopeType != model.BudgetScopeMember {
		c.JSON(http.StatusBadRequest, gin.H{"error": "仅支持删除成员单独配额"})
		return
	}

	if !actor.IsGlobal {
		ok, err := h.svc.MemberInDeptIDs(req.ScopeID, actor.DeptIDs)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if !ok {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权删除该成员配额"})
			return
		}
	}

	if err := h.svc.DeleteMemberBudget(c.Request.Context(), req.ScopeType, req.ScopeID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Context GET /api/v1/budgets/context
// 返回当前用户的预算链路上下文（各层级使用率），供面包屑展示。
// 返回格式：[{scope_type, label, amount_usd, used_usd, used_ratio}, ...]
func (h *BudgetHandler) Context(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)
	if !actor.IsManager() {
		c.JSON(http.StatusForbidden, gin.H{"error": "需要管理员权限"})
		return
	}

	type contextItem struct {
		ScopeType string  `json:"scope_type"`
		ScopeID   string  `json:"scope_id"`
		Label     string  `json:"label"`
		AmountUSD float64 `json:"amount_usd"`
		UsedUSD   float64 `json:"used_usd"`
		UsedRatio float64 `json:"used_ratio"`
	}

	// 构造链路：公司 → 工作室 → 部门（取调用者所属）
	deptID := c.GetString("user_dept_id")
	studioID := c.GetString("user_studio_id")

	scopes := []struct {
		scopeType, scopeID, label string
	}{
		{model.BudgetScopeCompany, model.BudgetScopeGlobal, "公司"},
	}
	if studioID != "" {
		scopes = append(scopes, struct{ scopeType, scopeID, label string }{model.BudgetScopeStudio, studioID, "工作室"})
	}
	if deptID != "" {
		scopes = append(scopes, struct{ scopeType, scopeID, label string }{model.BudgetScopeDeptDaily, deptID, "部门"})
	}

	items := make([]contextItem, 0, len(scopes))
	for _, s := range scopes {
		item := contextItem{ScopeType: s.scopeType, ScopeID: s.scopeID, Label: s.label, AmountUSD: -1}
		alloc, _ := h.svc.GetBudget(c.Request.Context(), s.scopeType, s.scopeID)
		if alloc != nil {
			item.AmountUSD = alloc.AmountUSD
			item.UsedUSD = alloc.UsedUSD
			item.UsedRatio = alloc.UsedRatio
		}
		items = append(items, item)
	}
	c.JSON(http.StatusOK, gin.H{"context": items})
}
