package handler_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/audit"
	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/handler"
	"github.com/example/llm-gateway/internal/controlplane/middleware"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type syncPendingAuditCapture struct {
	input audit.Input
}

func (c *syncPendingAuditCapture) Record(input audit.Input) {
	c.input = input
}

func TestBudgetTierSyncPendingStillAuditsBeforeAndAfter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	allocationID, tierID := uuid.New(), uuid.New()
	limit, err := budgetdomain.ParseAmount("100.000000")
	require.NoError(t, err)
	fake := &fakeBudgetTierHandlerService{
		allocation: &model.BudgetAllocation{
			ID: allocationID, ScopeType: model.BudgetScopeCompany, ScopeID: model.BudgetScopeGlobal,
		},
		setErr: service.ErrBudgetConfigSyncPending,
		setView: &service.TierLimitView{
			ModelTierID: tierID, PeriodType: budgetdomain.PeriodMonthly, BaseLimitUSD: limit,
		},
	}
	capture := &syncPendingAuditCapture{}
	router := gin.New()
	router.Use(middleware.AuditMiddleware(capture))
	h := handler.NewBudgetTierHandler(fake)
	router.PUT("/api/v1/budgets/:allocation_id/tier-limits", func(c *gin.Context) {
		rbac.SetActor(c, rbac.Actor{UserID: uuid.NewString(), IsGlobal: true})
		h.Set(c)
	})

	body := `{"items":[{"model_tier_id":"` + tierID.String() + `","period_type":"monthly","limit_usd":"100.000000","enabled":true,"effective_mode":"immediate"}],"reason":"调整"}`
	request := httptest.NewRequest(http.MethodPut, "/api/v1/budgets/"+allocationID.String()+"/tier-limits", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusAccepted, response.Code)
	details := capture.input.Details.(map[string]any)
	require.Equal(t, "调整", details["reason"])
	require.Equal(t, model.BudgetScopeCompany, details["scope_type"])
	require.Equal(t, model.BudgetScopeGlobal, details["scope_id"])
	require.Empty(t, details["before"])
	require.Len(t, details["after"], 1)
}
