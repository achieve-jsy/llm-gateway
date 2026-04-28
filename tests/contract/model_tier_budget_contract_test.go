package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/handler"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type routeContract struct {
	Permission                 string              `json:"permission"`
	RequestBodyFields          []string            `json:"request_body_fields"`
	QueryFields                []string            `json:"query_fields"`
	SuccessStatus              int                 `json:"success_status"`
	AlternativeSuccessStatuses map[string][]string `json:"alternative_success_statuses"`
	ErrorStatuses              map[string][]string `json:"error_statuses"`
	ResponseFields             []string            `json:"response_fields"`
	Empty                      map[string]any      `json:"empty"`
}

type contractDocument struct {
	ModelTierBudget struct {
		MyBudgetTierPeriodStatuses []string                 `json:"my_budget_tier_period_statuses"`
		Routes                     map[string]routeContract `json:"routes"`
	} `json:"model_tier_budget"`
	ModelTierBudgetP2 struct {
		MoneyFormat         string   `json:"money_format"`
		ExpansionStatusEnum []string `json:"expansion_status_enum"`
		Routes              []struct {
			Method         string   `json:"method"`
			Path           string   `json:"path"`
			ResponseStatus []int    `json:"response_status"`
			ResponseFields []string `json:"response_fields"`
		} `json:"routes"`
	} `json:"model_tier_budget_p2"`
}

func TestModelTierBudgetP2ContractIsParsedAndValidatesCriticalRoutes(t *testing.T) {
	document := loadContract(t)
	require.ElementsMatch(t,
		[]string{"tier_disabled", "limit_disabled", "unset", "blocked", "exhausted", "available", "restricted"},
		document.ModelTierBudget.MyBudgetTierPeriodStatuses,
	)
	require.ElementsMatch(t,
		[]string{"preview_token", "expires_at", "data_as_of", "stale", "items"},
		document.ModelTierBudget.Routes["POST /budgets/tier-limits/batch/preview"].ResponseFields,
	)
	require.Equal(t, `^-?[0-9]+\.[0-9]{6}$`, document.ModelTierBudgetP2.MoneyFormat)
	require.Equal(t, []string{"pending", "precheck_required", "pending_reconfirmation", "approved", "rejected", "cancelled", "expired"}, document.ModelTierBudgetP2.ExpansionStatusEnum)
	require.NotEmpty(t, document.ModelTierBudgetP2.Routes)
	routes := make(map[string]struct {
		status []int
		fields []string
	})
	for _, route := range document.ModelTierBudgetP2.Routes {
		require.NotEmpty(t, route.Method)
		require.NotEmpty(t, route.Path)
		require.NotEmpty(t, route.ResponseStatus)
		routes[route.Method+" "+route.Path] = struct {
			status []int
			fields []string
		}{route.ResponseStatus, route.ResponseFields}
	}
	for _, key := range []string{
		"POST /api/v1/expansion-requests/:id/reprecheck",
		"POST /api/v1/expansion-requests/:id/decision",
		"POST /api/v1/expansion-requests/batch-decision",
		"GET /api/v1/budget/risk-events",
		"GET /api/v1/my/budget/tiers",
	} {
		require.Contains(t, routes, key)
	}
	require.Contains(t, routes["POST /api/v1/expansion-requests/:id/decision"].status, http.StatusConflict)
	require.ElementsMatch(t,
		[]string{"items", "total", "data_as_of", "stale"},
		routes["GET /api/v1/budget/risk-events"].fields,
	)
}

func loadContract(t *testing.T) contractDocument {
	t.Helper()
	raw, err := os.ReadFile("contracts.json")
	require.NoError(t, err)
	var document contractDocument
	require.NoError(t, json.Unmarshal(raw, &document))
	return document
}

func TestModelTierBudgetContractFullyDefinesEveryP0Route(t *testing.T) {
	document := loadContract(t)
	expectedRoutes := []string{
		"GET /model-tiers", "POST /model-tiers", "PUT /model-tiers/reorder", "GET /model-tiers/:id",
		"PUT /model-tiers/:id", "PATCH /model-tiers/:id/status", "PATCH /model-tiers/:id/archive",
		"DELETE /model-tiers/:id", "PUT /groups/budget-tiers", "PUT /groups/:id/budget-tier",
		"GET /budgets/:allocation_id/tier-limits", "PUT /budgets/:allocation_id/tier-limits",
		"POST /budgets/tier-limits/batch/preview", "POST /budgets/tier-limits/batch/apply",
		"GET /budgets/tier-usage", "GET /my/budget/tiers",
	}
	require.Len(t, document.ModelTierBudget.Routes, len(expectedRoutes))
	for _, route := range expectedRoutes {
		got, ok := document.ModelTierBudget.Routes[route]
		require.True(t, ok, route)
		require.NotEmpty(t, got.Permission, route+" must define permission")
		require.Equal(t, expectedPermission(route), got.Permission, route)
		require.NotNil(t, got.RequestBodyFields, route+" must explicitly define request_body_fields")
		require.NotNil(t, got.QueryFields, route+" must explicitly define query_fields")
		require.NotZero(t, got.SuccessStatus, route+" must define success_status")
		require.GreaterOrEqual(t, got.SuccessStatus, 200, route)
		require.Less(t, got.SuccessStatus, 300, route)
		require.NotNil(t, got.AlternativeSuccessStatuses, route+" must explicitly define alternative_success_statuses")
		assertStatusCodes(t, route, got.AlternativeSuccessStatuses, 200, 299)
		require.NotEmpty(t, got.ErrorStatuses, route+" must define error_statuses")
		assertStatusCodes(t, route, got.ErrorStatuses, 400, 599)
		require.NotNil(t, got.ResponseFields, route+" must explicitly define response_fields")
		require.NotEmpty(t, got.ResponseFields, route)
		if strings.HasPrefix(route, "GET ") {
			require.Empty(t, got.RequestBodyFields, route)
		} else {
			require.NotEmpty(t, got.RequestBodyFields, route)
		}
	}
	require.Equal(t, []string{"include_archived"}, document.ModelTierBudget.Routes["GET /model-tiers"].QueryFields)
	require.Equal(t, []string{"scope_type", "scope_id", "period_type", "model_tier_id", "group_id"}, document.ModelTierBudget.Routes["GET /budgets/tier-usage"].QueryFields)
	require.Equal(t, []any{}, document.ModelTierBudget.Routes["GET /model-tiers"].Empty["data"])
	require.Equal(t, []any{}, document.ModelTierBudget.Routes["GET /budgets/:allocation_id/tier-limits"].Empty["tier_limits"])
	require.Equal(t, []any{}, document.ModelTierBudget.Routes["GET /budgets/tier-usage"].Empty["items"])
}

func TestP0FoundationOwnsTierConstraintSnapshotSchemaAndContract(t *testing.T) {
	foundationPath := "../../atlas-migrations/20260711132830_add_model_tier_budget_foundation.sql"
	foundation, err := os.ReadFile(foundationPath)
	if errors.Is(err, os.ErrNotExist) {
		foundationPath = "../../atlas-migrations/20260716025000_add_model_tier_budget_foundation.sql"
		foundation, err = os.ReadFile(foundationPath)
	}
	require.NoError(t, err)
	require.Contains(t, string(foundation), `"budget_tier_constraints_snapshot" jsonb NOT NULL DEFAULT '[]'`, foundationPath)
	_, err = os.Stat("../../atlas-migrations/20260711140938_add_call_logs_budget_tier_constraints_snapshot.sql")
	require.ErrorIs(t, err, os.ErrNotExist, "P1 must not own a separate schema migration")

	raw, err := os.ReadFile("contracts.json")
	require.NoError(t, err)
	require.Contains(t, string(raw), `"budget_tier_constraints_snapshot"`)
}

func expectedPermission(route string) string {
	switch {
	case route == "GET /model-tiers" || route == "GET /model-tiers/:id":
		return "model_tier:view"
	case strings.Contains(route, "/model-tiers") || strings.Contains(route, "/groups/"):
		return "model_tier:manage"
	case route == "GET /my/budget/tiers":
		return "authenticated"
	case route == "GET /budgets/:allocation_id/tier-limits" || route == "GET /budgets/tier-usage":
		return "budget:view:scoped"
	default:
		return "budget:set"
	}
}

func assertStatusCodes(t *testing.T, route string, statuses map[string][]string, minStatus, maxStatus int) {
	t.Helper()
	for rawStatus, codes := range statuses {
		status, err := strconv.Atoi(rawStatus)
		require.NoError(t, err, route)
		require.GreaterOrEqual(t, status, minStatus, route)
		require.LessOrEqual(t, status, maxStatus, route)
		require.NotEmpty(t, codes, route+" "+rawStatus)
		for _, code := range codes {
			require.NotEmpty(t, code, route+" "+rawStatus)
		}
	}
}

type contractModelTierService struct {
	tier      *model.ModelTier
	createErr error
	deleteErr error
}

func (s *contractModelTierService) List(context.Context, bool) ([]model.ModelTier, error) {
	return []model.ModelTier{*s.tier}, nil
}
func (s *contractModelTierService) Get(context.Context, uuid.UUID) (*model.ModelTier, error) {
	return s.tier, nil
}
func (s *contractModelTierService) Create(context.Context, service.CreateModelTierInput) (*model.ModelTier, error) {
	return s.tier, s.createErr
}
func (s *contractModelTierService) Update(context.Context, uuid.UUID, service.UpdateModelTierInput) (*model.ModelTier, error) {
	return s.tier, nil
}
func (s *contractModelTierService) SetEnabled(context.Context, uuid.UUID, bool, string) (*model.ModelTier, error) {
	return s.tier, nil
}
func (s *contractModelTierService) Archive(context.Context, uuid.UUID, string) (*model.ModelTier, error) {
	return s.tier, nil
}
func (s *contractModelTierService) Delete(context.Context, uuid.UUID, string) error {
	return s.deleteErr
}
func (s *contractModelTierService) Reorder(context.Context, []uuid.UUID, string) error {
	return nil
}

type contractBudgetTierService struct {
	allocation *model.BudgetAllocation
	setErr     error
	validator  *service.BudgetTierService
}

func (s *contractBudgetTierService) ListTierLimits(context.Context, uuid.UUID) ([]service.TierLimitView, error) {
	return []service.TierLimitView{}, nil
}
func (s *contractBudgetTierService) SetTierLimit(context.Context, service.SetTierLimitInput) (*service.TierLimitView, error) {
	return nil, s.setErr
}
func (s *contractBudgetTierService) GetAllocation(context.Context, uuid.UUID) (*model.BudgetAllocation, error) {
	return s.allocation, nil
}
func (s *contractBudgetTierService) PreviewBatch(ctx context.Context, actor rbac.Actor, input service.BatchTierLimitInput) (*service.BatchTierLimitPreview, error) {
	return s.validator.PreviewBatch(ctx, actor, input)
}
func (s *contractBudgetTierService) ApplyBatch(context.Context, rbac.Actor, service.ApplyBatchTierLimitInput) (*service.BatchTierLimitApplyResult, error) {
	return &service.BatchTierLimitApplyResult{}, nil
}
func (s *contractBudgetTierService) QueryTierUsage(context.Context, rbac.Actor, service.TierUsageQueryInput) (*service.TierUsageResponse, error) {
	return &service.TierUsageResponse{}, nil
}
func (s *contractBudgetTierService) GetMyBudgetTiers(context.Context, rbac.Actor) (*service.MyBudgetTiersResponse, error) {
	return &service.MyBudgetTiersResponse{}, nil
}

func TestModelTierBudgetContractMatchesGinHandlerStatuses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	document := loadContract(t)
	tierID, allocationID := uuid.New(), uuid.New()
	modelService := &contractModelTierService{tier: &model.ModelTier{ID: tierID, Name: "高级"}}
	budgetService := &contractBudgetTierService{
		allocation: &model.BudgetAllocation{ID: allocationID, ScopeType: model.BudgetScopeCompany, ScopeID: model.BudgetScopeGlobal},
		validator:  service.NewBudgetTierService(nil, nil, nil, time.Now),
	}
	modelHandler := handler.NewModelTierHandler(modelService)
	budgetHandler := handler.NewBudgetTierHandler(budgetService)
	router := gin.New()
	router.GET("/api/v1/model-tiers", modelHandler.List)
	router.POST("/api/v1/model-tiers", modelHandler.Create)
	router.DELETE("/api/v1/model-tiers/:id", modelHandler.Delete)
	router.PUT("/api/v1/budgets/:allocation_id/tier-limits", func(c *gin.Context) {
		rbac.SetActor(c, rbac.Actor{UserID: uuid.NewString(), IsGlobal: true})
		budgetHandler.Set(c)
	})
	router.POST("/api/v1/budgets/tier-limits/batch/preview", func(c *gin.Context) {
		rbac.SetActor(c, rbac.Actor{UserID: uuid.NewString(), IsGlobal: true})
		budgetHandler.PreviewBatch(c)
	})

	createBody := `{"name":"高级","description":"高成本模型","color":"#7C3AED","sort_order":10,"reason":"分层"}`
	response := contractRequest(router, http.MethodPost, "/api/v1/model-tiers", createBody)
	require.Equal(t, document.ModelTierBudget.Routes["POST /model-tiers"].SuccessStatus, response.Code)

	modelService.createErr = service.ErrBudgetConfigSyncPending
	response = contractRequest(router, http.MethodPost, "/api/v1/model-tiers", createBody)
	assertDeclaredAlternative(t, document.ModelTierBudget.Routes["POST /model-tiers"], response, "BUDGET_CONFIG_SYNC_PENDING")

	modelService.deleteErr = service.ErrModelTierReferenced
	response = contractRequest(router, http.MethodDelete, "/api/v1/model-tiers/"+tierID.String(), `{"reason":"still used"}`)
	assertDeclaredError(t, document.ModelTierBudget.Routes["DELETE /model-tiers/:id"], response, "MODEL_TIER_REFERENCED")

	budgetService.setErr = service.ErrPendingScheduleExists
	setBody := `{"items":[{"model_tier_id":"` + tierID.String() + `","period_type":"monthly","limit_usd":"100","enabled":true,"effective_mode":"next_period"}],"reason":"schedule"}`
	response = contractRequest(router, http.MethodPut, "/api/v1/budgets/"+allocationID.String()+"/tier-limits", setBody)
	assertDeclaredError(t, document.ModelTierBudget.Routes["PUT /budgets/:allocation_id/tier-limits"], response, "PENDING_BUDGET_SCHEDULE_EXISTS")

	response = contractRequest(router, http.MethodPost, "/api/v1/budgets/tier-limits/batch/preview", `{"operation":"set_fixed","items":[],"reason":"batch"}`)
	assertDeclaredError(t, document.ModelTierBudget.Routes["POST /budgets/tier-limits/batch/preview"], response, "bad_request")
}

func contractRequest(router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func assertDeclaredAlternative(t *testing.T, contract routeContract, response *httptest.ResponseRecorder, code string) {
	t.Helper()
	codes := contract.AlternativeSuccessStatuses[strconv.Itoa(response.Code)]
	require.Contains(t, codes, code)
	require.Contains(t, response.Body.String(), `"`+code+`"`)
}

func assertDeclaredError(t *testing.T, contract routeContract, response *httptest.ResponseRecorder, code string) {
	t.Helper()
	codes := contract.ErrorStatuses[strconv.Itoa(response.Code)]
	require.Contains(t, codes, code)
	require.Contains(t, response.Body.String(), `"`+code+`"`)
}
