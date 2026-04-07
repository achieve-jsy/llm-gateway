package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	authrepo "github.com/example/llm-gateway/internal/auth/repository"
	"github.com/example/llm-gateway/internal/budgetperiod"
	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ExpansionHandler 处理预算扩容申请的发起与审批。
type ExpansionHandler struct {
	svc      *service.ExpansionService
	rbac     *rbac.Checker
	userRepo *authrepo.UserRepository
}

func NewExpansionHandler(svc *service.ExpansionService, rbacChecker *rbac.Checker, userRepo *authrepo.UserRepository) *ExpansionHandler {
	return &ExpansionHandler{svc: svc, rbac: rbacChecker, userRepo: userRepo}
}

// Create POST /api/v1/expansion-requests
// 部门负责人发起扩容申请。
func (h *ExpansionHandler) Create(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)

	var req struct {
		ScopeType             string                     `json:"scope_type" binding:"required"`
		ScopeID               string                     `json:"scope_id" binding:"required"`
		PeriodType            string                     `json:"period_type"`
		DimensionType         budgetdomain.DimensionType `json:"dimension_type"`
		ModelTierID           *uuid.UUID                 `json:"model_tier_id"`
		DurationType          budgetdomain.DurationType  `json:"duration_type"`
		Priority              string                     `json:"priority"`
		CurrentAmount         float64                    `json:"current_amount"`
		CurrentAmountSnapshot budgetdomain.Amount        `json:"current_amount_snapshot"`
		UsedAmountSnapshot    budgetdomain.Amount        `json:"used_amount_snapshot"`
		RequestedAmount       *float64                   `json:"requested_amount"`
		TargetAmountUSD       *string                    `json:"target_amount_usd"`
		Reason                string                     `json:"reason" binding:"required"`
		UrgentReason          string                     `json:"urgent_reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": gin.H{
			"code": CodeExpansionReasonRequired, "message": service.ErrExpansionReasonRequired.Error(),
		}})
		return
	}
	if req.Priority == model.ExpansionPriorityUrgent && strings.TrimSpace(req.UrgentReason) == "" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": gin.H{
			"code": "EXPANSION_URGENT_REASON_REQUIRED", "message": "紧急申请必须填写紧急原因",
		}})
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
				c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
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
	if req.DimensionType == "" {
		req.DimensionType = budgetdomain.DimensionTotal
	}
	if req.DurationType == "" {
		req.DurationType = budgetdomain.DurationPermanent
	}
	if req.Priority == "" {
		req.Priority = model.ExpansionPriorityNormal
	}
	target, err := resolveExpansionTarget(req.RequestedAmount, req.TargetAmountUSD)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": gin.H{"code": "INVALID_EXPANSION_INPUT", "message": err.Error()}})
		return
	}
	tierID := ""
	if req.ModelTierID != nil {
		tierID = req.ModelTierID.String()
	}
	precheck, err := h.svc.Precheck(c.Request.Context(), expansionActor(actor), service.PrecheckInput{
		ScopeType: req.ScopeType, ScopeID: req.ScopeID, PeriodType: periodType,
		DimensionType: string(req.DimensionType), DurationType: string(req.DurationType),
		Priority: req.Priority, TargetAmountUSD: target.String(),
		ModelTierID: func() *string {
			if tierID == "" {
				return nil
			}
			return &tierID
		}(),
	})
	if err != nil {
		writeExpansionWorkflowError(c, err)
		return
	}
	if !precheck.CanSubmit {
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{
			"code": "EXPANSION_PRECHECK_BLOCKED", "message": "当前预检不允许提交",
			"next_action": func() string {
				if precheck.CanNotifyOwner {
					return "notify_owner"
				}
				return "raise_target"
			}(),
		}})
		return
	}
	currentSnapshot, _ := budgetdomain.ParseAmount(precheck.CurrentLimitUSD)
	usedSnapshot, _ := budgetdomain.ParseAmount(precheck.CurrentUsedUSD)
	currentAmount, _ := currentSnapshot.Decimal.Float64()
	requestedAmount, _ := target.Decimal.Float64()
	expiresAt := precheck.DataAsOf.Add(30 * 24 * time.Hour)
	if req.DurationType == budgetdomain.DurationCurrentPeriod {
		if window, resolveErr := budgetperiod.Resolve(precheck.DataAsOf, budgetdomain.PeriodType(periodType), budgetperiod.Config{
			Timezone: budgetperiod.DefaultTimezone, WeekStart: budgetperiod.CanonicalWeekStart,
		}); resolveErr == nil {
			expiresAt = window.ResetAt
		}
	}
	result, err := h.svc.CreateRequestAdvanced(c.Request.Context(), service.CreateExpansionRequestInput{
		ScopeType: req.ScopeType, ScopeID: req.ScopeID, RequesterID: requesterID,
		PeriodType: budgetdomain.PeriodType(periodType), DimensionType: req.DimensionType,
		ModelTierID: req.ModelTierID, DurationType: req.DurationType, Priority: req.Priority,
		CurrentAmount: currentAmount, RequestedAmount: requestedAmount,
		CurrentAmountSnapshot: currentSnapshot, UsedAmountSnapshot: usedSnapshot,
		PrecheckVersion: precheck.PrecheckVersion, PrecheckedAt: precheck.DataAsOf, ExpiresAt: expiresAt,
		Reason: req.Reason, UrgentReason: req.UrgentReason,
	})
	if err != nil {
		if errors.Is(err, service.ErrInvalidExpansionDimension) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": gin.H{"code": CodeInvalidExpansionDimension, "message": "invalid expansion dimension"}})
			return
		}
		if errors.Is(err, service.ErrDuplicatePendingExpansion) {
			c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": CodeDuplicatePendingExpansion, "message": "a pending expansion request already exists"}})
			return
		}
		if errors.Is(err, service.ErrExpansionReasonRequired) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": gin.H{"code": CodeExpansionReasonRequired, "message": "expansion reason is required"}})
			return
		}
		if errors.Is(err, service.ErrInvalidExpansionPeriod) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": gin.H{"code": CodeInvalidBudgetPeriod, "message": "invalid budget period"}})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	c.JSON(http.StatusCreated, expansionResponse(result, target.String()))
}

// Precheck POST /api/v1/expansion-requests/precheck
func (h *ExpansionHandler) Precheck(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	var req service.PrecheckInput
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_EXPANSION_INPUT", "message": err.Error()})
		return
	}
	result, err := h.svc.Precheck(c.Request.Context(), expansionActor(rbac.GetActor(c)), req)
	if err != nil {
		writeExpansionWorkflowError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func expansionActor(actor rbac.Actor) service.ActorScope {
	role := "user"
	switch {
	case actor.IsGlobal:
		role = "admin"
	case len(actor.StudioIDs) > 0:
		role = "studio_admin"
	case len(actor.DeptIDs) > 0:
		role = "dept_admin"
	}
	return service.ActorScope{
		UserID: actor.UserID, Role: role, IsGlobal: actor.IsGlobal,
		DeptIDs: actor.DeptIDs, StudioIDs: actor.StudioIDs,
	}
}

func resolveExpansionTarget(legacy *float64, raw *string) (budgetdomain.Amount, error) {
	var legacyAmount *budgetdomain.Amount
	if legacy != nil {
		value, err := budgetdomain.ParseAmount(fmt.Sprintf("%.6f", *legacy))
		if err != nil {
			return budgetdomain.Amount{}, err
		}
		legacyAmount = &value
	}
	var target *budgetdomain.Amount
	if raw != nil {
		value, err := budgetdomain.ParseAmount(*raw)
		if err != nil {
			return budgetdomain.Amount{}, err
		}
		target = &value
	}
	if legacyAmount == nil && target == nil {
		return budgetdomain.Amount{}, service.ErrExpansionTargetRequired
	}
	if legacyAmount != nil && target != nil && legacyAmount.String() != target.String() {
		return budgetdomain.Amount{}, errors.New("requested_amount and target_amount_usd must match")
	}
	if target != nil {
		return *target, nil
	}
	return *legacyAmount, nil
}

func expansionResponse(request *model.ExpansionRequest, target string) gin.H {
	return gin.H{
		"id": request.ID, "scope_type": request.ScopeType, "scope_id": request.ScopeID,
		"requester_id": request.RequesterID, "period_type": request.PeriodType,
		"dimension_type": request.DimensionType, "model_tier_id": request.ModelTierID,
		"duration_type": request.DurationType, "priority": request.Priority,
		"current_amount": request.CurrentAmount, "requested_amount": request.RequestedAmount,
		"current_amount_snapshot": request.CurrentAmountSnapshot,
		"used_amount_snapshot":    request.UsedAmountSnapshot, "target_amount_usd": target,
		"reason": request.Reason, "status": request.Status, "current_level": request.CurrentLevel,
		"precheck_version": request.PrecheckVersion, "prechecked_at": request.PrecheckedAt,
		"expires_at": request.ExpiresAt, "sync_status": request.SyncStatus,
		"created_at": request.CreatedAt, "updated_at": request.UpdatedAt,
	}
}

func writeExpansionWorkflowError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrExpansionForbidden):
		c.JSON(http.StatusForbidden, gin.H{"code": "EXPANSION_FORBIDDEN", "message": err.Error()})
	case errors.Is(err, service.ErrExpansionTerminalConflict):
		c.JSON(http.StatusConflict, gin.H{"code": "EXPANSION_TERMINAL_CONFLICT", "message": err.Error()})
	case errors.Is(err, service.ErrExpansionUsageUnavailable):
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "BUDGET_USAGE_UNAVAILABLE", "message": err.Error(), "next_action": "retry"})
	default:
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": "INVALID_EXPANSION_INPUT", "message": err.Error()})
	}
}

func writeExpansionDecisionError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrPrecheckStale):
		c.JSON(http.StatusConflict, gin.H{
			"code": "EXPANSION_PRECHECK_STALE", "message": "预算或用量已变化，请重新预检后确认",
			"next_action": "reprecheck",
		})
	case errors.Is(err, service.ErrExpansionUsedExceedsTarget):
		c.JSON(http.StatusConflict, gin.H{
			"code": "EXPANSION_USED_EXCEEDS_TARGET", "message": err.Error(), "next_action": "raise_target",
		})
	case errors.Is(err, service.ErrExpansionRequestExpired):
		c.JSON(http.StatusConflict, gin.H{
			"code": "EXPANSION_REQUEST_EXPIRED", "message": err.Error(),
		})
	case errors.Is(err, service.ErrConfigSyncPending):
		c.JSON(http.StatusAccepted, gin.H{
			"code": "BUDGET_CONFIG_SYNC_PENDING", "message": err.Error(), "next_action": "retry",
		})
	case errors.Is(err, service.ErrDecisionCommentRequired):
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"code": "DECISION_COMMENT_REQUIRED", "message": "驳回和上报必须填写审批意见",
		})
	case errors.Is(err, service.ErrNotInApprovalPool), errors.Is(err, service.ErrApprovalPoolEmpty):
		c.JSON(http.StatusForbidden, gin.H{"code": "EXPANSION_APPROVAL_FORBIDDEN", "message": err.Error()})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_EXPANSION_DECISION", "message": err.Error()})
	}
}

// Decide POST /api/v1/expansion-requests/:id/decision
// 审批人做决策（approve/reject/escalate）。
func (h *ExpansionHandler) Decide(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	actor := rbac.GetActor(c)

	requestID := c.Param("id")
	var req struct {
		Action          string `json:"action" binding:"required"`
		Comment         string `json:"comment"`
		PrecheckVersion string `json:"precheck_version"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if req.Action != model.ExpansionActionCancel && !actor.IsManager() {
		c.JSON(http.StatusForbidden, gin.H{"error": "需要管理员权限"})
		return
	}

	approverID, _ := uuid.Parse(c.GetString("user_id"))
	if err := h.svc.AssertActorCanAccessRequest(c.Request.Context(), expansionActor(actor), requestID); err != nil {
		writeExpansionWorkflowError(c, err)
		return
	}
	_, err := h.svc.DecideWorkflow(c.Request.Context(), requestID, approverID, service.DecisionInput{
		Action: req.Action, Comment: req.Comment, PrecheckVersion: req.PrecheckVersion,
	})
	if err != nil {
		writeExpansionDecisionError(c, err)
		return
	}
	detail, detailErr := h.svc.GetDetail(c.Request.Context(), expansionActor(actor), requestID)
	if detailErr != nil {
		writeExpansionWorkflowError(c, detailErr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": detail})
}

func (h *ExpansionHandler) Get(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	detail, err := h.svc.GetDetail(c.Request.Context(), expansionActor(rbac.GetActor(c)), c.Param("id"))
	if err != nil {
		writeExpansionWorkflowError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": detail})
}

func (h *ExpansionHandler) Update(c *gin.Context) {
	var req struct {
		TargetAmountUSD string `json:"target_amount_usd" binding:"required"`
		Reason          string `json:"reason" binding:"required"`
		UrgentReason    string `json:"urgent_reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_EXPANSION_INPUT", "message": err.Error()})
		return
	}
	request, err := h.svc.UpdateRequest(c.Request.Context(), c.Param("id"), c.GetString("user_id"), service.UpdateExpansionInput{
		TargetAmountUSD: req.TargetAmountUSD, Reason: req.Reason, UrgentReason: req.UrgentReason,
	})
	if err != nil {
		writeExpansionWorkflowError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": request})
}

func (h *ExpansionHandler) Reprecheck(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	result, err := h.svc.Reprecheck(c.Request.Context(), expansionActor(rbac.GetActor(c)), c.Param("id"))
	if err != nil {
		writeExpansionWorkflowError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *ExpansionHandler) NotifyOwner(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	var req service.NotifyOwnerInput
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_EXPANSION_INPUT", "message": err.Error()})
		return
	}
	if err := h.svc.NotifyOwner(c.Request.Context(), expansionActor(rbac.GetActor(c)), req); err != nil {
		writeExpansionWorkflowError(c, err)
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
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

	requesterName := c.Query("name")
	items, err := h.svc.ListPendingScoped(c.Request.Context(), level, actor.IsGlobal, actor.DeptIDs, actor.StudioIDs, requesterName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	// 批量查申请人姓名
	requesterIDs := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		requesterIDs = append(requesterIDs, item.RequesterID)
	}
	userMap, _ := h.userRepo.FindByIDs(requesterIDs)

	// 补查每条申请对应 scope 的当前使用率
	type PendingItem struct {
		model.ExpansionRequest
		CurrentUsageRatio float64 `json:"current_usage_ratio"`
		RequesterName     string  `json:"requester_name"`
	}
	result := make([]PendingItem, 0, len(items))
	for _, item := range items {
		pi := PendingItem{ExpansionRequest: item}
		budget, _ := h.svc.GetBudgetUsedRatio(c.Request.Context(), item.ScopeType, item.ScopeID)
		pi.CurrentUsageRatio = budget
		if u, ok := userMap[item.RequesterID]; ok {
			pi.RequesterName = u.Name
		}
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
			ID              string `json:"id" binding:"required"`
			Action          string `json:"action" binding:"required"`
			Comment         string `json:"comment"`
			PrecheckVersion string `json:"precheck_version" binding:"required"`
		} `json:"decisions" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
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
		if err := h.svc.AssertActorCanAccessRequest(c.Request.Context(), expansionActor(actor), d.ID); err != nil {
			results = append(results, gin.H{"id": d.ID, "ok": false, "code": "EXPANSION_FORBIDDEN", "error": err.Error()})
			continue
		}
		if _, err := h.svc.DecideWorkflow(c.Request.Context(), d.ID, approverID, service.DecisionInput{
			Action: d.Action, Comment: d.Comment, PrecheckVersion: d.PrecheckVersion,
		}); err != nil {
			item := gin.H{"id": d.ID, "ok": false, "error": err.Error()}
			if errors.Is(err, service.ErrPrecheckStale) {
				item["code"] = "EXPANSION_PRECHECK_STALE"
				item["next_action"] = "reprecheck"
			}
			results = append(results, item)
		} else {
			results = append(results, gin.H{"id": d.ID, "ok": true})
		}
	}
	c.JSON(http.StatusOK, gin.H{"results": results})
}

// ListSteps GET /api/v1/expansion-requests/:id/steps
// 查询某扩容申请的审批流水。
func (h *ExpansionHandler) ListSteps(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	requestID := c.Param("id")
	steps, err := h.svc.ListStepsScoped(c.Request.Context(), expansionActor(rbac.GetActor(c)), requestID)
	if err != nil {
		writeExpansionWorkflowError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"steps": steps})
}
