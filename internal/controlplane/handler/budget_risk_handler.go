package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/gin-gonic/gin"
)

type BudgetRiskHandler struct {
	service *service.BudgetRiskService
	rbac    *rbac.Checker
}

func NewBudgetRiskHandler(service *service.BudgetRiskService, checker *rbac.Checker) *BudgetRiskHandler {
	return &BudgetRiskHandler{service: service, rbac: checker}
}

func (h *BudgetRiskHandler) List(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	result, err := h.service.List(c.Request.Context(), expansionActor(rbac.GetActor(c)), riskFilter(c))
	if err != nil {
		writeBudgetRiskError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *BudgetRiskHandler) Summary(c *gin.Context) {
	h.rbac.BuildAndInjectActor(c)
	result, err := h.service.Summary(c.Request.Context(), expansionActor(rbac.GetActor(c)), riskFilter(c))
	if err != nil {
		writeBudgetRiskError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func riskFilter(c *gin.Context) service.RiskFilter {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))
	if page < 1 {
		page = 1
	}
	if pageSize < 0 {
		pageSize = 0
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return service.RiskFilter{
		ScopeType: c.Query("scope_type"), ScopeID: c.Query("scope_id"),
		PeriodType: c.Query("period_type"), DimensionType: c.Query("dimension_type"),
		ModelTierID: c.Query("model_tier_id"), RiskType: c.Query("risk_type"),
		Severity: c.Query("severity"),
		Page:     page, PageSize: pageSize,
	}
}

func writeBudgetRiskError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrBudgetForbidden):
		c.JSON(http.StatusForbidden, gin.H{"code": "BUDGET_RISK_FORBIDDEN", "message": err.Error()})
	case errors.Is(err, service.ErrBudgetUsageUnavailable):
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"code": "BUDGET_USAGE_UNAVAILABLE", "message": err.Error(), "next_action": "retry",
		})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"code": "BUDGET_RISK_QUERY_FAILED", "message": err.Error()})
	}
}
