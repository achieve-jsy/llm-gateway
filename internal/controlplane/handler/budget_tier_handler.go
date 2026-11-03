package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/example/llm-gateway/internal/controlplane/audit"
	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type BudgetTierHandlerService interface {
	ListTierLimits(context.Context, uuid.UUID) ([]service.TierLimitView, error)
	SetTierLimit(context.Context, service.SetTierLimitInput) (*service.TierLimitView, error)
	GetAllocation(context.Context, uuid.UUID) (*model.BudgetAllocation, error)
	PreviewBatch(context.Context, rbac.Actor, service.BatchTierLimitInput) (*service.BatchTierLimitPreview, error)
	ApplyBatch(context.Context, rbac.Actor, service.ApplyBatchTierLimitInput) (*service.BatchTierLimitApplyResult, error)
	QueryTierUsage(context.Context, rbac.Actor, service.TierUsageQueryInput) (*service.TierUsageResponse, error)
	GetMyBudgetTiers(context.Context, rbac.Actor) (*service.MyBudgetTiersResponse, error)
}

func (h *BudgetTierHandler) TierUsage(c *gin.Context) {
	input := service.TierUsageQueryInput{
		ScopeType: c.Query("scope_type"), ScopeID: c.Query("scope_id"),
		PeriodType: budgetdomain.PeriodType(c.Query("period_type")),
	}
	if raw := c.Query("model_tier_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": CodeBadRequest, "message": "invalid model_tier_id"}})
			return
		}
		input.ModelTierID = &id
	}
	if raw := c.Query("group_id"); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": CodeBadRequest, "message": "invalid group_id"}})
			return
		}
		groupID := uint(id)
		input.GroupID = &groupID
	}
	result, err := h.service.QueryTierUsage(c.Request.Context(), rbac.GetActor(c), input)
	if err != nil {
		writeBudgetTierError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *BudgetTierHandler) MyBudgetTiers(c *gin.Context) {
	actor := rbac.GetActor(c)
	if actor.UserID == "" {
		actor.UserID = c.GetString("user_id")
	}
	result, err := h.service.GetMyBudgetTiers(c.Request.Context(), actor)
	if err != nil {
		writeBudgetTierError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *BudgetTierHandler) PreviewBatch(c *gin.Context) {
	var request service.BatchTierLimitInput
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": gin.H{"type": "unprocessable_entity", "code": CodeInvalidMoneyAmount, "message": "invalid request body"}})
		return
	}
	if strings.TrimSpace(request.Reason) == "" {
		writeBudgetTierError(c, service.ErrBudgetReasonRequired)
		return
	}
	actor := rbac.GetActor(c)
	request.ActorID, _ = uuid.Parse(actor.UserID)
	result, err := h.service.PreviewBatch(c.Request.Context(), actor, request)
	if err != nil {
		writeBudgetTierError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *BudgetTierHandler) DeprecatedSet(c *gin.Context) {
	c.JSON(http.StatusGone, gin.H{"error": gin.H{
		"type": "gone", "code": "BUDGET_PREVIEW_REQUIRED",
		"message": "direct tier limit writes are disabled; use batch preview/apply",
	}})
}

func (h *BudgetTierHandler) ApplyBatch(c *gin.Context) {
	var request service.ApplyBatchTierLimitInput
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": gin.H{"type": "unprocessable_entity", "code": CodeBadRequest, "message": "invalid request body"}})
		return
	}
	result, err := h.service.ApplyBatch(c.Request.Context(), rbac.GetActor(c), request)
	if err != nil {
		if errors.Is(err, service.ErrBudgetForbidden) {
			writeBudgetTierError(c, err)
			return
		}
		if errors.Is(err, service.ErrBatchPreviewInvalid) || errors.Is(err, service.ErrBatchPreviewStale) {
			c.JSON(http.StatusConflict, gin.H{"error": gin.H{"type": "conflict", "code": "budget.preview_stale", "message": "preview token is invalid or stale"}})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"type": "internal_error", "code": CodeInternalError, "message": "internal server error"}})
		return
	}
	audit.SetBeforeAfter(c, result.AuditBefore, result.AuditAfter)
	audit.SetReason(c, result.AuditReason)
	status := http.StatusOK
	failed := 0
	conflicts := 0
	for _, item := range result.Items {
		if item.Status == "failed" {
			failed++
			if item.ErrorCode == "BUDGET_ALLOCATION_CONFLICT" || item.ErrorCode == "PENDING_BUDGET_SCHEDULE_EXISTS" {
				conflicts++
			}
		}
	}
	if failed == len(result.Items) && failed > 0 {
		if conflicts > 0 {
			status = http.StatusConflict
		} else {
			status = http.StatusUnprocessableEntity
		}
	} else if failed > 0 {
		status = http.StatusMultiStatus
	} else if result.SyncPending {
		status = http.StatusAccepted
	}
	c.JSON(status, gin.H{"data": result})
}

type BudgetTierScopeResolver interface {
	MemberInDeptIDs(string, []string) (bool, error)
}

type BudgetTierHandler struct {
	service       BudgetTierHandlerService
	scopeResolver BudgetTierScopeResolver
}

func NewBudgetTierHandler(service BudgetTierHandlerService) *BudgetTierHandler {
	return &BudgetTierHandler{service: service}
}

func (h *BudgetTierHandler) WithScopeResolver(resolver BudgetTierScopeResolver) *BudgetTierHandler {
	h.scopeResolver = resolver
	return h
}

func (h *BudgetTierHandler) List(c *gin.Context) {
	allocationID, ok := parseAllocationID(c)
	if !ok {
		return
	}
	allocation, err := h.service.GetAllocation(c.Request.Context(), allocationID)
	if err != nil {
		writeBudgetTierError(c, err)
		return
	}
	allowed, scopeErr := h.canRead(rbac.GetActor(c), allocation)
	if scopeErr != nil {
		writeBudgetTierError(c, scopeErr)
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"type": "forbidden", "code": "org.forbidden", "message": "forbidden"}})
		return
	}
	limits, err := h.service.ListTierLimits(c.Request.Context(), allocationID)
	if err != nil {
		writeBudgetTierError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"allocation_id": allocationID, "tier_limits": limits}})
}

func (h *BudgetTierHandler) Set(c *gin.Context) {
	allocationID, ok := parseAllocationID(c)
	if !ok {
		return
	}
	allocation, err := h.service.GetAllocation(c.Request.Context(), allocationID)
	if err != nil {
		writeBudgetTierError(c, err)
		return
	}
	audit.SetScope(c, allocation.ScopeType, allocation.ScopeID)
	actor := rbac.GetActor(c)
	allowed, err := h.canWrite(actor, allocation)
	if err != nil {
		writeBudgetTierError(c, err)
		return
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"type": "forbidden", "code": "org.forbidden", "message": "forbidden"}})
		return
	}
	var request struct {
		Items []struct {
			ModelTierID   uuid.UUID               `json:"model_tier_id"`
			PeriodType    budgetdomain.PeriodType `json:"period_type"`
			LimitUSD      budgetdomain.Amount     `json:"limit_usd"`
			Enabled       bool                    `json:"enabled"`
			EffectiveMode service.EffectiveMode   `json:"effective_mode"`
		} `json:"items" binding:"required,min=1"`
		Reason                 string `json:"reason"`
		ConfirmReplaceSchedule bool   `json:"confirm_replace_schedule"`
		ConfirmBelowUsed       bool   `json:"confirm_below_used"`
		ConfirmZeroBlock       bool   `json:"confirm_zero_block"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": gin.H{"type": "unprocessable_entity", "code": CodeInvalidMoneyAmount, "message": "invalid request body"}})
		return
	}
	if strings.TrimSpace(request.Reason) == "" {
		writeBudgetTierError(c, service.ErrBudgetReasonRequired)
		return
	}
	before, beforeErr := h.service.ListTierLimits(c.Request.Context(), allocationID)
	if beforeErr != nil {
		slog.ErrorContext(c.Request.Context(), "failed to capture budget tier audit before-state",
			"budget_allocation_id", allocationID, "error", beforeErr)
	}
	action := audit.ActionBudgetTierLimitSet
	for _, item := range request.Items {
		if item.EffectiveMode == service.EffectiveNextPeriod {
			action = audit.ActionBudgetTierLimitSchedule
			if request.ConfirmReplaceSchedule {
				action = audit.ActionBudgetTierReplaceSchedule
			}
			break
		}
	}
	audit.SetActionOverride(c, action)
	type itemResult struct {
		ModelTierID uuid.UUID               `json:"model_tier_id"`
		PeriodType  budgetdomain.PeriodType `json:"period_type"`
		Status      string                  `json:"status"`
		ErrorCode   string                  `json:"error_code,omitempty"`
		TierLimit   *service.TierLimitView  `json:"tier_limit,omitempty"`
	}
	results := make([]itemResult, 0, len(request.Items))
	actorID, _ := uuid.Parse(actor.UserID)
	syncPending := false
	pendingScheduleConflict := false
	for _, item := range request.Items {
		view, err := h.service.SetTierLimit(c.Request.Context(), service.SetTierLimitInput{
			AllocationID: allocationID, TierID: item.ModelTierID, PeriodType: item.PeriodType,
			LimitUSD: item.LimitUSD, Enabled: item.Enabled, EffectiveMode: item.EffectiveMode,
			Reason: request.Reason, ActorID: actorID, ConfirmReplaceSchedule: request.ConfirmReplaceSchedule,
			ConfirmBelowUsed: request.ConfirmBelowUsed, ConfirmZeroBlock: request.ConfirmZeroBlock,
		})
		if err != nil {
			if errors.Is(err, service.ErrBudgetConfigSyncPending) {
				syncPending = true
				results = append(results, itemResult{
					ModelTierID: item.ModelTierID, PeriodType: item.PeriodType,
					Status: "applied", ErrorCode: CodeBudgetConfigSyncPending, TierLimit: view,
				})
				continue
			}
			if errors.Is(err, service.ErrPendingScheduleExists) {
				pendingScheduleConflict = true
			}
			results = append(results, itemResult{
				ModelTierID: item.ModelTierID, PeriodType: item.PeriodType,
				Status: "failed", ErrorCode: budgetTierItemErrorCode(err),
			})
			continue
		}
		results = append(results, itemResult{ModelTierID: item.ModelTierID, PeriodType: item.PeriodType, Status: "applied", TierLimit: view})
	}
	after, afterErr := h.service.ListTierLimits(c.Request.Context(), allocationID)
	if afterErr != nil {
		slog.ErrorContext(c.Request.Context(), "failed to capture budget tier audit after-state",
			"budget_allocation_id", allocationID, "error", afterErr)
	}
	audit.SetBeforeAfter(c, before, after)
	status := http.StatusOK
	failed := 0
	for _, item := range results {
		if item.Status == "failed" {
			failed++
		}
	}
	if failed == len(results) && failed > 0 {
		if pendingScheduleConflict {
			status = http.StatusConflict
		} else {
			status = http.StatusUnprocessableEntity
		}
	} else if failed > 0 {
		status = http.StatusMultiStatus
	} else if syncPending {
		status = http.StatusAccepted
	}
	data := gin.H{"allocation_id": allocationID, "items": results, "sync_pending": syncPending}
	if syncPending {
		data["code"] = CodeBudgetConfigSyncPending
	}
	c.JSON(status, gin.H{"data": data})
}

func budgetTierItemErrorCode(err error) string {
	switch {
	case errors.Is(err, service.ErrBudgetAllocationNotFound):
		return CodeBudgetAllocationNotFound
	case errors.Is(err, service.ErrModelTierNotFound):
		return CodeModelTierNotFound
	case errors.Is(err, service.ErrModelTierDisabled):
		return CodeModelTierDisabled
	case errors.Is(err, service.ErrInvalidBudgetPeriod):
		return CodeInvalidBudgetPeriod
	case errors.Is(err, service.ErrInvalidEffectiveMode):
		return CodeBadRequest
	case errors.Is(err, service.ErrPendingScheduleExists):
		return CodePendingBudgetSchedule
	case errors.Is(err, service.ErrBudgetForbidden):
		return "ORG_FORBIDDEN"
	case errors.Is(err, service.ErrBudgetConfigSyncPending):
		return CodeBudgetConfigSyncPending
	default:
		return "INTERNAL_ERROR"
	}
}

func (h *BudgetTierHandler) canRead(actor rbac.Actor, allocation *model.BudgetAllocation) (bool, error) {
	if actor.IsGlobal {
		return true, nil
	}
	switch allocation.ScopeType {
	case model.BudgetScopeStudio:
		return actor.HasStudio(allocation.ScopeID), nil
	case model.BudgetScopeDeptDaily, model.BudgetScopeDeptService:
		return actor.HasDept(allocation.ScopeID), nil
	case model.BudgetScopeMember:
		if len(actor.StudioIDs) > 0 || len(actor.DeptIDs) == 0 || h.scopeResolver == nil {
			return false, nil
		}
		return h.scopeResolver.MemberInDeptIDs(allocation.ScopeID, actor.DeptIDs)
	default:
		return false, nil
	}
}

func (h *BudgetTierHandler) canWrite(actor rbac.Actor, allocation *model.BudgetAllocation) (bool, error) {
	if actor.IsGlobal {
		return true, nil
	}
	switch allocation.ScopeType {
	case model.BudgetScopeDeptDaily, model.BudgetScopeDeptService:
		return len(actor.StudioIDs) > 0 && actor.HasDept(allocation.ScopeID), nil
	case model.BudgetScopeMember:
		if len(actor.StudioIDs) > 0 || h.scopeResolver == nil {
			return false, nil
		}
		return h.scopeResolver.MemberInDeptIDs(allocation.ScopeID, actor.DeptIDs)
	default:
		return false, nil
	}
}

func parseAllocationID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("allocation_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request", "code": CodeBadRequest, "message": "invalid allocation id"}})
		return uuid.Nil, false
	}
	return id, true
}

func writeBudgetTierError(c *gin.Context, err error) {
	status, code, message := http.StatusInternalServerError, CodeInternalError, "internal server error"
	switch {
	case errors.Is(err, service.ErrBudgetAllocationNotFound):
		status, code, message = http.StatusNotFound, CodeBudgetAllocationNotFound, "budget allocation not found"
	case errors.Is(err, service.ErrModelTierNotFound):
		status, code, message = http.StatusNotFound, CodeModelTierNotFound, "model tier not found"
	case errors.Is(err, service.ErrModelTierDisabled):
		status, code, message = http.StatusUnprocessableEntity, CodeModelTierDisabled, "model tier is disabled"
	case errors.Is(err, service.ErrInvalidBudgetPeriod):
		status, code, message = http.StatusUnprocessableEntity, CodeInvalidBudgetPeriod, "invalid budget period"
	case errors.Is(err, service.ErrInvalidEffectiveMode):
		status, code, message = http.StatusUnprocessableEntity, CodeBadRequest, "invalid effective mode"
	case errors.Is(err, service.ErrBudgetReasonRequired):
		status, code, message = http.StatusUnprocessableEntity, CodeBadRequest, "budget change reason is required"
	case errors.Is(err, service.ErrBelowUsedConfirmationRequired):
		status, code, message = http.StatusConflict, "BUDGET_LIMIT_BELOW_USED", "confirm_below_used is required"
	case errors.Is(err, service.ErrZeroBlockConfirmationRequired):
		status, code, message = http.StatusConflict, "BUDGET_ZERO_BLOCK_CONFIRMATION_REQUIRED", "confirm_zero_block is required"
	case errors.Is(err, service.ErrBudgetUsageUnavailable):
		status, code, message = http.StatusServiceUnavailable, "BUDGET_USAGE_UNAVAILABLE", "budget usage unavailable"
	case errors.Is(err, service.ErrInvalidBatchRequest):
		status, code, message = http.StatusUnprocessableEntity, CodeBadRequest, "invalid batch request"
	case errors.Is(err, service.ErrBudgetForbidden):
		status, code, message = http.StatusForbidden, "org.forbidden", "forbidden"
	case errors.Is(err, service.ErrPendingScheduleExists):
		status, code, message = http.StatusConflict, CodePendingBudgetSchedule, "pending budget schedule exists"
	case errors.Is(err, service.ErrBudgetConfigSyncPending):
		status, code, message = http.StatusAccepted, CodeBudgetConfigSyncPending, "budget configuration synchronization is pending"
	}
	c.JSON(status, gin.H{"error": gin.H{"type": "budget_error", "code": code, "message": message}})
}
