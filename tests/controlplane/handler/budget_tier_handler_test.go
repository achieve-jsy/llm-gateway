package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/handler"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type fakeBudgetTierHandlerService struct {
	allocation    *model.BudgetAllocation
	setCalls      int
	previewCalls  int
	previewErr    error
	applyErr      error
	setErr        error
	setErrors     []error
	allocationErr error
	setView       *service.TierLimitView
	tierLimits    []service.TierLimitView
	memberAllowed bool
	applyResult   *service.BatchTierLimitApplyResult
	usageResult   *service.TierUsageResponse
	myResult      *service.MyBudgetTiersResponse
	myActor       rbac.Actor
}

func (f *fakeBudgetTierHandlerService) ListTierLimits(context.Context, uuid.UUID) ([]service.TierLimitView, error) {
	return append([]service.TierLimitView(nil), f.tierLimits...), nil
}
func (f *fakeBudgetTierHandlerService) SetTierLimit(context.Context, service.SetTierLimitInput) (*service.TierLimitView, error) {
	f.setCalls++
	setErr := f.setErr
	if f.setCalls <= len(f.setErrors) {
		setErr = f.setErrors[f.setCalls-1]
	}
	view := f.setView
	if view == nil {
		view = &service.TierLimitView{}
	}
	if setErr == nil || errors.Is(setErr, service.ErrBudgetConfigSyncPending) {
		f.tierLimits = []service.TierLimitView{*view}
	}
	return view, setErr
}
func (f *fakeBudgetTierHandlerService) GetAllocation(context.Context, uuid.UUID) (*model.BudgetAllocation, error) {
	return f.allocation, f.allocationErr
}

func TestBudgetTierRepositoryForbiddenMapsTo403(t *testing.T) {
	gin.SetMode(gin.TestMode)
	allocationID := uuid.New()
	h := handler.NewBudgetTierHandler(&fakeBudgetTierHandlerService{allocationErr: service.ErrBudgetForbidden})
	router := gin.New()
	router.GET("/api/v1/budgets/:allocation_id/tier-limits", h.List)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/budgets/"+allocationID.String()+"/tier-limits", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusForbidden, response.Code)
	require.Contains(t, response.Body.String(), `"code":"org.forbidden"`)
}
func (f *fakeBudgetTierHandlerService) MemberInDeptIDs(string, []string) (bool, error) {
	return f.memberAllowed, nil
}
func (f *fakeBudgetTierHandlerService) PreviewBatch(context.Context, rbac.Actor, service.BatchTierLimitInput) (*service.BatchTierLimitPreview, error) {
	f.previewCalls++
	return &service.BatchTierLimitPreview{PreviewToken: "preview-token"}, f.previewErr
}
func (f *fakeBudgetTierHandlerService) ApplyBatch(context.Context, rbac.Actor, service.ApplyBatchTierLimitInput) (*service.BatchTierLimitApplyResult, error) {
	if f.applyErr != nil {
		return nil, f.applyErr
	}
	if f.applyResult != nil {
		return f.applyResult, nil
	}
	return &service.BatchTierLimitApplyResult{}, nil
}

func TestBudgetTierHandlersNeverExposeInternalErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := "postgres://secret@database/internal"
	tests := []struct {
		name    string
		path    string
		body    string
		handler gin.HandlerFunc
	}{
		{
			name: "preview service error", path: "/preview",
			body:    `{"operation":"set_fixed","items":[{"item_id":"row"}],"reason":"test"}`,
			handler: handler.NewBudgetTierHandler(&fakeBudgetTierHandlerService{previewErr: errors.New(secret)}).PreviewBatch,
		},
		{
			name: "apply service error", path: "/apply",
			body:    `{"preview_token":"token"}`,
			handler: handler.NewBudgetTierHandler(&fakeBudgetTierHandlerService{applyErr: errors.New(secret)}).ApplyBatch,
		},
		{
			name: "json decode error", path: "/preview",
			body:    `{"operation":`,
			handler: handler.NewBudgetTierHandler(&fakeBudgetTierHandlerService{}).PreviewBatch,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			router.POST(test.path, test.handler)
			request := httptest.NewRequest(http.MethodPost, test.path, bytes.NewBufferString(test.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.NotContains(t, response.Body.String(), secret)
			require.NotContains(t, response.Body.String(), "unexpected EOF")
		})
	}
}

func TestBudgetTierBatchApplyUses422WhenAllItemsFail(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &fakeBudgetTierHandlerService{applyResult: &service.BatchTierLimitApplyResult{
		Items: []service.BatchTierLimitApplyItem{{ItemID: "row-1", Status: "failed", ErrorCode: "INTERNAL_ERROR"}},
	}}
	h := handler.NewBudgetTierHandler(fake)
	router := gin.New()
	router.POST("/apply", h.ApplyBatch)
	request := httptest.NewRequest(http.MethodPost, "/apply", bytes.NewBufferString(`{"preview_token":"token","confirm_item_ids":["row-1"]}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusUnprocessableEntity, response.Code)
	require.Contains(t, response.Body.String(), `"error_code":"INTERNAL_ERROR"`)
}

func TestBudgetTierBatchApplyMapsUniqueRaceTo409(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &fakeBudgetTierHandlerService{applyResult: &service.BatchTierLimitApplyResult{
		Items: []service.BatchTierLimitApplyItem{{ItemID: "row-1", Status: "failed", ErrorCode: "BUDGET_ALLOCATION_CONFLICT"}},
	}}
	h := handler.NewBudgetTierHandler(fake)
	router := gin.New()
	router.POST("/apply", h.ApplyBatch)
	request := httptest.NewRequest(http.MethodPost, "/apply", bytes.NewBufferString(`{"preview_token":"token","confirm_item_ids":["row-1"]}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusConflict, response.Code)
}

func TestBudgetTierBatchApplyUses207ForPartialFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &fakeBudgetTierHandlerService{applyResult: &service.BatchTierLimitApplyResult{
		Items: []service.BatchTierLimitApplyItem{
			{ItemID: "row-1", Status: "applied"},
			{ItemID: "row-2", Status: "failed", ErrorCode: "ORG_FORBIDDEN"},
		},
	}}
	h := handler.NewBudgetTierHandler(fake)
	router := gin.New()
	router.POST("/apply", h.ApplyBatch)
	request := httptest.NewRequest(http.MethodPost, "/apply", bytes.NewBufferString(`{"preview_token":"token","confirm_item_ids":["row-1","row-2"]}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusMultiStatus, response.Code)
}
func (f *fakeBudgetTierHandlerService) QueryTierUsage(context.Context, rbac.Actor, service.TierUsageQueryInput) (*service.TierUsageResponse, error) {
	if f.usageResult != nil {
		return f.usageResult, nil
	}
	return &service.TierUsageResponse{}, nil
}
func (f *fakeBudgetTierHandlerService) QueryScopedTierUsage(context.Context, rbac.Actor, string, []string, budgetdomain.PeriodType) (*service.TierUsageResponse, error) {
	if f.usageResult != nil {
		return f.usageResult, nil
	}
	return &service.TierUsageResponse{}, nil
}
func (f *fakeBudgetTierHandlerService) GetMyBudgetTiers(_ context.Context, actor rbac.Actor) (*service.MyBudgetTiersResponse, error) {
	f.myActor = actor
	if f.myResult != nil {
		return f.myResult, nil
	}
	return &service.MyBudgetTiersResponse{}, nil
}

func TestBudgetTierStudioAdminCannotEditOwnStudioTierLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	allocationID := uuid.New()
	fake := &fakeBudgetTierHandlerService{allocation: &model.BudgetAllocation{
		ID: allocationID, ScopeType: model.BudgetScopeStudio, ScopeID: "studio-a",
	}}
	h := handler.NewBudgetTierHandler(fake)
	router := gin.New()
	router.PUT("/api/v1/budgets/:allocation_id/tier-limits", func(c *gin.Context) {
		rbac.SetActor(c, rbac.Actor{UserID: uuid.NewString(), StudioIDs: []string{"studio-a"}})
		h.Set(c)
	})

	body := `{"items":[{"model_tier_id":"` + uuid.NewString() + `","period_type":"monthly","limit_usd":"100.000000","enabled":true,"effective_mode":"immediate"}],"reason":"调整"}`
	request := httptest.NewRequest(http.MethodPut, "/api/v1/budgets/"+allocationID.String()+"/tier-limits", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusForbidden, response.Code)
	require.Zero(t, fake.setCalls)
}

func TestBudgetTierDeptAdminCanReadManagedMemberLimits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	allocationID := uuid.New()
	fake := &fakeBudgetTierHandlerService{
		allocation:    &model.BudgetAllocation{ID: allocationID, ScopeType: model.BudgetScopeMember, ScopeID: "member-a"},
		memberAllowed: true,
	}
	h := handler.NewBudgetTierHandler(fake).WithScopeResolver(fake)
	router := gin.New()
	router.GET("/api/v1/budgets/:allocation_id/tier-limits", func(c *gin.Context) {
		rbac.SetActor(c, rbac.Actor{UserID: uuid.NewString(), DeptIDs: []string{"dept-a"}})
		h.List(c)
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/budgets/"+allocationID.String()+"/tier-limits", nil))
	require.Equal(t, http.StatusOK, response.Code)
}

func TestBudgetTierBatchPreviewAcceptsCanonicalRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &fakeBudgetTierHandlerService{}
	h := handler.NewBudgetTierHandler(fake)
	router := gin.New()
	router.POST("/api/v1/budgets/tier-limits/batch/preview", func(c *gin.Context) {
		rbac.SetActor(c, rbac.Actor{UserID: uuid.NewString(), IsGlobal: true})
		h.PreviewBatch(c)
	})
	body := `{"operation":"set_fixed","items":[{"item_id":"row-1","allocation_id":"` + uuid.NewString() + `","model_tier_id":"` + uuid.NewString() + `","period_type":"monthly","value":"100.000000"}],"effective_mode":"immediate","reason":"批量调整"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/budgets/tier-limits/batch/preview", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, 1, fake.previewCalls)
}

func TestBudgetTierPersistedButSnapshotPendingReturns202(t *testing.T) {
	gin.SetMode(gin.TestMode)
	allocationID := uuid.New()
	fake := &fakeBudgetTierHandlerService{
		allocation: &model.BudgetAllocation{
			ID: allocationID, ScopeType: model.BudgetScopeCompany, ScopeID: model.BudgetScopeGlobal,
		},
		setErr: service.ErrBudgetConfigSyncPending,
	}
	h := handler.NewBudgetTierHandler(fake)
	router := gin.New()
	router.PUT("/api/v1/budgets/:allocation_id/tier-limits", func(c *gin.Context) {
		rbac.SetActor(c, rbac.Actor{UserID: uuid.NewString(), IsGlobal: true})
		h.Set(c)
	})

	body := `{"items":[{"model_tier_id":"` + uuid.NewString() + `","period_type":"monthly","limit_usd":"100.000000","enabled":true,"effective_mode":"immediate"}],"reason":"调整"}`
	request := httptest.NewRequest(http.MethodPut, "/api/v1/budgets/"+allocationID.String()+"/tier-limits", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusAccepted, response.Code)
	require.Contains(t, response.Body.String(), `"code":"BUDGET_CONFIG_SYNC_PENDING"`)
	require.Contains(t, response.Body.String(), `"status":"applied"`)
	require.NotContains(t, response.Body.String(), `"status":"failed"`)
}

func TestBudgetTierSetUsesStableCodesAndMultiStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	allocationID := uuid.New()
	fake := &fakeBudgetTierHandlerService{
		allocation: &model.BudgetAllocation{ID: allocationID, ScopeType: model.BudgetScopeCompany},
		setErrors:  []error{nil, service.ErrModelTierDisabled},
	}
	h := handler.NewBudgetTierHandler(fake)
	router := gin.New()
	router.PUT("/api/v1/budgets/:allocation_id/tier-limits", func(c *gin.Context) {
		rbac.SetActor(c, rbac.Actor{UserID: uuid.NewString(), IsGlobal: true})
		h.Set(c)
	})
	body := `{"items":[` +
		`{"model_tier_id":"` + uuid.NewString() + `","period_type":"monthly","limit_usd":"100.000000","enabled":true,"effective_mode":"immediate"},` +
		`{"model_tier_id":"` + uuid.NewString() + `","period_type":"monthly","limit_usd":"50.000000","enabled":true,"effective_mode":"immediate"}` +
		`],"reason":"mixed result"}`
	request := httptest.NewRequest(http.MethodPut, "/api/v1/budgets/"+allocationID.String()+"/tier-limits", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusMultiStatus, response.Code)
	require.Contains(t, response.Body.String(), `"error_code":"MODEL_TIER_DISABLED"`)
	require.NotContains(t, response.Body.String(), service.ErrModelTierDisabled.Error())
}

func TestBudgetTierSetUses422AndHidesRawErrorWhenAllWritesFail(t *testing.T) {
	gin.SetMode(gin.TestMode)
	allocationID := uuid.New()
	fake := &fakeBudgetTierHandlerService{
		allocation: &model.BudgetAllocation{ID: allocationID, ScopeType: model.BudgetScopeCompany},
		setErr:     errors.New("secret database detail"),
	}
	h := handler.NewBudgetTierHandler(fake)
	router := gin.New()
	router.PUT("/api/v1/budgets/:allocation_id/tier-limits", func(c *gin.Context) {
		rbac.SetActor(c, rbac.Actor{UserID: uuid.NewString(), IsGlobal: true})
		h.Set(c)
	})
	body := `{"items":[{"model_tier_id":"` + uuid.NewString() + `","period_type":"monthly","limit_usd":"100.000000","enabled":true,"effective_mode":"immediate"}],"reason":"all fail"}`
	request := httptest.NewRequest(http.MethodPut, "/api/v1/budgets/"+allocationID.String()+"/tier-limits", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusUnprocessableEntity, response.Code)
	require.Contains(t, response.Body.String(), `"error_code":"INTERNAL_ERROR"`)
	require.NotContains(t, response.Body.String(), "secret database detail")
}

func TestBudgetTierSetReturns200WhenAllWritesSucceed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	allocationID := uuid.New()
	fake := &fakeBudgetTierHandlerService{
		allocation: &model.BudgetAllocation{ID: allocationID, ScopeType: model.BudgetScopeCompany},
	}
	h := handler.NewBudgetTierHandler(fake)
	router := gin.New()
	router.PUT("/api/v1/budgets/:allocation_id/tier-limits", func(c *gin.Context) {
		rbac.SetActor(c, rbac.Actor{UserID: uuid.NewString(), IsGlobal: true})
		h.Set(c)
	})
	body := `{"items":[{"model_tier_id":"` + uuid.NewString() + `","period_type":"monthly","limit_usd":"100.000000","enabled":true,"effective_mode":"immediate"}],"reason":"success"}`
	request := httptest.NewRequest(http.MethodPut, "/api/v1/budgets/"+allocationID.String()+"/tier-limits", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
}

func TestBudgetTierListJSONIncludesTemporaryTargetUSD(t *testing.T) {
	gin.SetMode(gin.TestMode)
	allocationID, tierID := uuid.New(), uuid.New()
	base, temporary, effective := handlerAmount(t, "100"), handlerAmount(t, "150"), handlerAmount(t, "150")
	fake := &fakeBudgetTierHandlerService{
		allocation: &model.BudgetAllocation{ID: allocationID, ScopeType: model.BudgetScopeCompany, ScopeID: model.BudgetScopeGlobal},
		tierLimits: []service.TierLimitView{{
			ID: uuid.New(), BudgetAllocationID: allocationID, ModelTierID: tierID, ModelTierName: "高级",
			PeriodType: budgetdomain.PeriodMonthly, BaseLimitUSD: base, TemporaryTargetUSD: &temporary,
			EffectiveLimitUSD: effective, Enabled: true, EffectiveAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		}},
	}
	router := gin.New()
	router.GET("/api/v1/budgets/:allocation_id/tier-limits", func(c *gin.Context) {
		rbac.SetActor(c, rbac.Actor{UserID: uuid.NewString(), IsGlobal: true})
		handler.NewBudgetTierHandler(fake).List(c)
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/budgets/"+allocationID.String()+"/tier-limits", nil))

	require.Equal(t, http.StatusOK, response.Code)
	require.JSONEq(t, `{"data":{"allocation_id":"`+allocationID.String()+`","tier_limits":[{"id":"`+fake.tierLimits[0].ID.String()+`","budget_allocation_id":"`+allocationID.String()+`","model_tier_id":"`+tierID.String()+`","model_tier_name":"高级","period_type":"monthly","base_limit_usd":"100.000000","temporary_target_usd":"150.000000","effective_limit_usd":"150.000000","source_summary":{"source_type":"","temporary_active":false},"enabled":true,"effective_at":"2026-07-01T00:00:00Z"}]}}`, response.Body.String())
}

func TestMyBudgetTiersHandlerReturnsCompleteCanonicalDTO(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tierID := uuid.New()
	amount, temporary := handlerAmount(t, "100"), handlerAmount(t, "120")
	now := time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	fake := &fakeBudgetTierHandlerService{myResult: &service.MyBudgetTiersResponse{Data: service.MyBudgetTiersData{
		SourceSummary: service.MyBudgetSourceSummary{
			Mode: service.SourceModeMixed, PrimaryScopeType: model.BudgetScopeMember,
			HasSharedPool: true, HasTemporaryOverride: true,
		},
		EffectiveTotal: []service.EffectiveTotalPeriod{{
			PeriodType: budgetdomain.PeriodMonthly, Configured: true, BaseLimitUSD: &amount,
			TemporaryTargetUSD: &temporary, EffectiveLimitUSD: &temporary, UsedUSD: &amount,
			RemainingUSD: &amount, ResetAt: now, Status: "active",
			LimitingSource: service.LimitingSourceView{ScopeType: model.BudgetScopeMember, RemainingUSD: &amount},
		}},
		BudgetChain: []service.MyBudgetChainScope{{ScopeType: model.BudgetScopeMember, Label: "我的额度", VisibleAmounts: true}},
		TierLimits: []service.MyBudgetTierView{{
			ModelTierID: tierID, ModelTierName: "高级",
			Periods: []service.MyBudgetTierPeriod{{
				PeriodType: budgetdomain.PeriodMonthly, Configured: true, BaseLimitUSD: &amount,
				TemporaryTargetUSD: &temporary, EffectiveLimitUSD: &temporary, UsedUSD: &amount,
				RemainingUSD: &amount, ResetAt: now, Status: "active",
				LimitingSource: service.LimitingSourceView{ScopeType: model.BudgetScopeMember, RemainingUSD: &amount},
			}},
		}},
		DataUpdatedAt: now, Stale: false,
	}}}
	router := gin.New()
	router.GET("/api/v1/my/budget/tiers", func(c *gin.Context) {
		rbac.SetActor(c, rbac.Actor{UserID: "member-a"})
		handler.NewBudgetTierHandler(fake).MyBudgetTiers(c)
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/my/budget/tiers", nil))
	require.Equal(t, http.StatusOK, response.Code)

	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	require.ElementsMatch(t,
		[]string{"source_summary", "effective_total", "budget_chain", "tier_limits", "data_updated_at", "stale"},
		mapKeys(envelope.Data),
	)
	require.Contains(t, response.Body.String(), `"temporary_target_usd":"120.000000"`)
	require.Contains(t, response.Body.String(), `"limiting_source":{"scope_type":"member","remaining_usd":"100.000000"}`)
}

func TestMyBudgetTiersHandlerUsesJWTUserIDWhenRBACActorMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &fakeBudgetTierHandlerService{}
	h := handler.NewBudgetTierHandler(fake)
	router := gin.New()
	router.GET("/api/v1/my/budget/tiers", func(c *gin.Context) {
		c.Set("user_id", "member-from-jwt")
		h.MyBudgetTiers(c)
	})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/my/budget/tiers", nil))

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "member-from-jwt", fake.myActor.UserID)
}

func TestMyBudgetTiersHandlerUsesPersonalActorForJWTAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &fakeBudgetTierHandlerService{}
	h := handler.NewBudgetTierHandler(fake)
	router := gin.New()
	router.GET("/api/v1/my/budget/tiers", func(c *gin.Context) {
		c.Set("user_id", "admin-user")
		c.Set("user_role", "admin")
		h.MyBudgetTiers(c)
	})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/my/budget/tiers", nil))

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "admin-user", fake.myActor.UserID)
	require.False(t, fake.myActor.IsGlobal)
}

func handlerAmount(t *testing.T, value string) budgetdomain.Amount {
	t.Helper()
	amount, err := budgetdomain.ParseAmount(value)
	require.NoError(t, err)
	return amount
}

func mapKeys(values map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func TestBudgetTierTierUsageHandlerReturns200WithData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tierID := uuid.New()
	reportedCost := handlerAmount(t, "25.500000")
	accountingCost := handlerAmount(t, "25.500000")
	fake := &fakeBudgetTierHandlerService{usageResult: &service.TierUsageResponse{Data: service.TierUsageResponseData{
		DataAsOf: time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC), Stale: false,
		Items: []repository.TierUsageAggregate{{
			ScopeType: "company", ScopeID: "global", TierID: &tierID,
			ReportedCostUSD: reportedCost, AccountingCostUSD: accountingCost, RequestCount: 42,
		}},
	}}}
	h := handler.NewBudgetTierHandler(fake)
	router := gin.New()
	router.GET("/api/v1/budgets/tier-usage", func(c *gin.Context) {
		rbac.SetActor(c, rbac.Actor{UserID: uuid.NewString(), IsGlobal: true})
		h.TierUsage(c)
	})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/budgets/tier-usage?scope_type=company&scope_id=global&period_type=monthly&model_tier_id="+tierID.String(), nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), `"model_tier_id":"`+tierID.String()+`"`)
	require.Contains(t, response.Body.String(), `"request_count":42`)
}

func TestBudgetTierTierUsageHandlerRejectsInvalidModelTierID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := handler.NewBudgetTierHandler(&fakeBudgetTierHandlerService{})
	router := gin.New()
	router.GET("/api/v1/budgets/tier-usage", func(c *gin.Context) {
		rbac.SetActor(c, rbac.Actor{UserID: uuid.NewString(), IsGlobal: true})
		h.TierUsage(c)
	})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/budgets/tier-usage?model_tier_id=not-a-uuid", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Contains(t, response.Body.String(), `"code":"bad_request"`)
	require.Contains(t, response.Body.String(), "invalid model_tier_id")
}

func TestBudgetTierTierUsageHandlerRejectsInvalidGroupID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := handler.NewBudgetTierHandler(&fakeBudgetTierHandlerService{})
	router := gin.New()
	router.GET("/api/v1/budgets/tier-usage", func(c *gin.Context) {
		rbac.SetActor(c, rbac.Actor{UserID: uuid.NewString(), IsGlobal: true})
		h.TierUsage(c)
	})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/budgets/tier-usage?group_id=not-a-number", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Contains(t, response.Body.String(), "invalid group_id")
}

func TestBudgetTierSetUsesCanonicalPendingScheduleErrorCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	allocationID := uuid.New()
	fake := &fakeBudgetTierHandlerService{
		allocation: &model.BudgetAllocation{ID: allocationID, ScopeType: model.BudgetScopeCompany},
		setErr:     service.ErrPendingScheduleExists,
	}
	h := handler.NewBudgetTierHandler(fake)
	router := gin.New()
	router.PUT("/api/v1/budgets/:allocation_id/tier-limits", func(c *gin.Context) {
		rbac.SetActor(c, rbac.Actor{UserID: uuid.NewString(), IsGlobal: true})
		h.Set(c)
	})
	body := `{"items":[{"model_tier_id":"` + uuid.NewString() + `","period_type":"monthly","limit_usd":"100.000000","enabled":true,"effective_mode":"next_period"}],"reason":"schedule"}`
	request := httptest.NewRequest(http.MethodPut, "/api/v1/budgets/"+allocationID.String()+"/tier-limits", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusConflict, response.Code)
	require.Contains(t, response.Body.String(), `"error_code":"PENDING_BUDGET_SCHEDULE_EXISTS"`)
}

func TestBatchPreviewValidationErrorsReturn422BadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := handler.NewBudgetTierHandler(service.NewBudgetTierService(nil, nil, nil, time.Now))
	router := gin.New()
	router.POST("/api/v1/budgets/tier-limits/batch/preview", func(c *gin.Context) {
		rbac.SetActor(c, rbac.Actor{UserID: uuid.NewString(), IsGlobal: true})
		h.PreviewBatch(c)
	})
	tierID, allocationID := uuid.NewString(), uuid.NewString()
	tests := map[string]string{
		"empty items":        `{"operation":"set_fixed","items":[],"reason":"batch"}`,
		"invalid operation":  `{"operation":"multiply","items":[{"item_id":"row-1","allocation_id":"` + allocationID + `","model_tier_id":"` + tierID + `","period_type":"monthly","value":"1"}],"reason":"batch"}`,
		"missing item id":    `{"operation":"set_fixed","items":[{"allocation_id":"` + allocationID + `","model_tier_id":"` + tierID + `","period_type":"monthly","value":"1"}],"reason":"batch"}`,
		"duplicate item ids": `{"operation":"set_fixed","items":[{"item_id":"row-1","allocation_id":"` + allocationID + `","model_tier_id":"` + tierID + `","period_type":"monthly","value":"1"},{"item_id":"row-1","allocation_id":"` + allocationID + `","model_tier_id":"` + tierID + `","period_type":"monthly","value":"2"}],"reason":"batch"}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/budgets/tier-limits/batch/preview", bytes.NewBufferString(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, http.StatusUnprocessableEntity, response.Code)
			require.Contains(t, response.Body.String(), `"code":"bad_request"`)
		})
	}
}
