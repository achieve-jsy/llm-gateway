package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/handler"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type fakeModelTierService struct {
	deleteErr     error
	createErr     error
	updateErr     error
	setEnabledErr error
	tier          *model.ModelTier
	tiers         []model.ModelTier
}

func (f *fakeModelTierService) List(context.Context, bool) ([]model.ModelTier, error) {
	if len(f.tiers) > 0 {
		return f.tiers, nil
	}
	return []model.ModelTier{*f.tier}, nil
}
func (f *fakeModelTierService) Get(context.Context, uuid.UUID) (*model.ModelTier, error) {
	return f.tier, nil
}
func (f *fakeModelTierService) Create(context.Context, service.CreateModelTierInput) (*model.ModelTier, error) {
	return f.tier, f.createErr
}
func (f *fakeModelTierService) Update(context.Context, uuid.UUID, service.UpdateModelTierInput) (*model.ModelTier, error) {
	return f.tier, f.updateErr
}
func (f *fakeModelTierService) SetEnabled(context.Context, uuid.UUID, bool, string) (*model.ModelTier, error) {
	return f.tier, f.setEnabledErr
}
func (f *fakeModelTierService) Archive(context.Context, uuid.UUID, string) (*model.ModelTier, error) {
	return f.tier, nil
}
func (f *fakeModelTierService) Delete(context.Context, uuid.UUID, string) error { return f.deleteErr }
func (f *fakeModelTierService) Reorder(context.Context, []uuid.UUID, string) error {
	return nil
}

func TestDeleteReferencedModelTierReturns409(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := uuid.New()
	h := handler.NewModelTierHandler(&fakeModelTierService{
		deleteErr: service.ErrModelTierReferenced,
		tier:      &model.ModelTier{ID: id, Name: "高级"},
	})
	router := gin.New()
	router.DELETE("/api/v1/model-tiers/:id", h.Delete)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/model-tiers/"+id.String(), bytes.NewBufferString(`{"reason":"配置仍被引用"}`))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)

	require.Equal(t, http.StatusConflict, response.Code)
	require.JSONEq(t, `{"error":{"type":"conflict","code":"MODEL_TIER_REFERENCED","message":"model tier is referenced"}}`, response.Body.String())
}

func TestModelTierPersistedButSnapshotPendingReturns202(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := uuid.New()
	h := handler.NewModelTierHandler(&fakeModelTierService{
		createErr: service.ErrBudgetConfigSyncPending,
		tier:      &model.ModelTier{ID: id, Name: "高级"},
	})
	router := gin.New()
	router.POST("/api/v1/model-tiers", h.Create)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/model-tiers", bytes.NewBufferString(
		`{"name":"高级","description":"高成本模型","color":"#7C3AED","sort_order":10,"reason":"分层"}`,
	))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)

	require.Equal(t, http.StatusAccepted, response.Code)
	require.Contains(t, response.Body.String(), `"code":"BUDGET_CONFIG_SYNC_PENDING"`)
	require.Contains(t, response.Body.String(), `"status":"persisted_sync_pending"`)
}

func TestModelTierHandlerDoesNotExposeInternalError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := "duplicate key detail: private-index"
	h := handler.NewModelTierHandler(&fakeModelTierService{
		createErr: errors.New(secret),
		tier:      &model.ModelTier{ID: uuid.New(), Name: "高级"},
	})
	router := gin.New()
	router.POST("/api/v1/model-tiers", h.Create)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/model-tiers", bytes.NewBufferString(
		`{"name":"高级","color":"#7C3AED","reason":"test"}`,
	))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.NotContains(t, response.Body.String(), secret)
}

func TestModelTierHandlerListReturns200WithData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id1, id2 := uuid.New(), uuid.New()
	h := handler.NewModelTierHandler(&fakeModelTierService{
		tiers: []model.ModelTier{
			{ID: id1, Name: "免费", Color: "#10B981", Enabled: true, GroupCount: 5, BudgetCount: 2, HistoryCount: 10, ReferenceCount: 17},
			{ID: id2, Name: "高级", Color: "#7C3AED", Enabled: true, GroupCount: 3, BudgetCount: 1, HistoryCount: 5, ReferenceCount: 9},
		},
		tier: &model.ModelTier{ID: id1, Name: "免费"},
	})
	router := gin.New()
	router.GET("/api/v1/model-tiers", h.List)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/model-tiers", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)

	require.Equal(t, http.StatusOK, response.Code)
	var result struct {
		Data []model.ModelTier `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.Len(t, result.Data, 2)
	require.Equal(t, "免费", result.Data[0].Name)
	require.Equal(t, int64(5), result.Data[0].GroupCount)
	require.Equal(t, int64(17), result.Data[0].ReferenceCount)
	require.Equal(t, "高级", result.Data[1].Name)
}

func TestModelTierHandlerGetReturns200(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := uuid.New()
	h := handler.NewModelTierHandler(&fakeModelTierService{
		tier: &model.ModelTier{ID: id, Name: "高级", Color: "#7C3AED", Enabled: true},
	})
	router := gin.New()
	router.GET("/api/v1/model-tiers/:id", h.Get)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/model-tiers/"+id.String(), nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)

	require.Equal(t, http.StatusOK, response.Code)
	var result struct {
		Data model.ModelTier `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.Equal(t, id, result.Data.ID)
	require.Equal(t, "高级", result.Data.Name)
}

func TestModelTierHandlerCreateWithValidInputReturns201(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := uuid.New()
	h := handler.NewModelTierHandler(&fakeModelTierService{
		tier: &model.ModelTier{ID: id, Name: "高级", Color: "#7C3AED", SortOrder: 10, Enabled: true},
	})
	router := gin.New()
	router.POST("/api/v1/model-tiers", h.Create)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/model-tiers", bytes.NewBufferString(
		`{"name":"高级","description":"高成本模型","color":"#7C3AED","sort_order":10,"reason":"分层"}`,
	))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)

	require.Equal(t, http.StatusCreated, response.Code)
	var result struct {
		Data model.ModelTier `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.Equal(t, "高级", result.Data.Name)
	require.Equal(t, "#7C3AED", result.Data.Color)
}

func TestModelTierHandlerCreateWithDuplicateNameReturns409(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := handler.NewModelTierHandler(&fakeModelTierService{
		createErr: service.ErrModelTierNameExists,
		tier:      &model.ModelTier{ID: uuid.New(), Name: "高级"},
	})
	router := gin.New()
	router.POST("/api/v1/model-tiers", h.Create)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/model-tiers", bytes.NewBufferString(
		`{"name":"高级","color":"#7C3AED","reason":"重复"}`,
	))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)

	require.Equal(t, http.StatusConflict, response.Code)
	require.Contains(t, response.Body.String(), `"code":"MODEL_TIER_NAME_CONFLICT"`)
	require.Contains(t, response.Body.String(), `"message":"model tier name already exists"`)
}

func TestModelTierHandlerUpdateReturns200(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := uuid.New()
	h := handler.NewModelTierHandler(&fakeModelTierService{
		tier: &model.ModelTier{ID: id, Name: "高级-更新", Color: "#EC4899", Enabled: true},
	})
	router := gin.New()
	router.PUT("/api/v1/model-tiers/:id", h.Update)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/model-tiers/"+id.String(), bytes.NewBufferString(
		`{"name":"高级-更新","color":"#EC4899","reason":"更新名称和颜色"}`,
	))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)

	require.Equal(t, http.StatusOK, response.Code)
	var result struct {
		Data model.ModelTier `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.Equal(t, "高级-更新", result.Data.Name)
	require.Equal(t, "#EC4899", result.Data.Color)
}

func TestModelTierHandlerSetStatusReturns200(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := uuid.New()
	h := handler.NewModelTierHandler(&fakeModelTierService{
		tier: &model.ModelTier{ID: id, Name: "高级", Enabled: false},
	})
	router := gin.New()
	router.PATCH("/api/v1/model-tiers/:id/status", h.SetStatus)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/model-tiers/"+id.String()+"/status", bytes.NewBufferString(
		`{"enabled":false,"reason":"禁用该分层"}`,
	))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)

	require.Equal(t, http.StatusOK, response.Code)
	var result struct {
		Data model.ModelTier `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.Equal(t, id, result.Data.ID)
	require.False(t, result.Data.Enabled)
}

func TestModelTierHandlerDisableReferencedTierReturns409(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := uuid.New()
	h := handler.NewModelTierHandler(&fakeModelTierService{
		setEnabledErr: service.ErrModelTierReferenced,
		tier:          &model.ModelTier{ID: id, Name: "高级", Enabled: true},
	})
	router := gin.New()
	router.PATCH("/api/v1/model-tiers/:id/status", h.SetStatus)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/model-tiers/"+id.String()+"/status", bytes.NewBufferString(
		`{"enabled":false,"reason":"禁用该分层"}`,
	))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)

	require.Equal(t, http.StatusConflict, response.Code)
	require.JSONEq(t, `{"error":{"type":"conflict","code":"MODEL_TIER_REFERENCED","message":"model tier is referenced"}}`, response.Body.String())
}

func TestModelTierHandlerDeleteReturns200(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := uuid.New()
	h := handler.NewModelTierHandler(&fakeModelTierService{
		tier: &model.ModelTier{ID: id, Name: "高级"},
	})
	router := gin.New()
	router.DELETE("/api/v1/model-tiers/:id", h.Delete)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/model-tiers/"+id.String(), bytes.NewBufferString(
		`{"reason":"删除未使用的分层"}`,
	))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)

	require.Equal(t, http.StatusOK, response.Code)
	var result struct {
		Data struct {
			ID uuid.UUID `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.Equal(t, id, result.Data.ID)
}

type fakeGroupBudgetTierService struct {
	setBudgetTierErr      error
	setBudgetTierBatchErr error
	group                 *model.Group
	calls                 int
}

func (f *fakeGroupBudgetTierService) SetBudgetTier(context.Context, uint, *uuid.UUID, string) error {
	f.calls++
	return f.setBudgetTierErr
}
func (f *fakeGroupBudgetTierService) SetBudgetTierBatch(context.Context, []uint, *uuid.UUID, string) error {
	f.calls++
	return f.setBudgetTierBatchErr
}
func (f *fakeGroupBudgetTierService) GetByID(uint) (*model.Group, error) {
	return f.group, nil
}

func (f *fakeGroupBudgetTierService) ListEnabledGroupsWithModels() ([]model.Group, error) {
	if f.group == nil {
		return nil, nil
	}
	return []model.Group{*f.group}, nil
}

func TestModelTierHandlerSetGroupBudgetTierReturns200(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tierID := uuid.New()
	groupSvc := &fakeGroupBudgetTierService{
		group: &model.Group{ID: 1, Name: "gpt-4"},
	}
	h := handler.NewModelTierHandler(&fakeModelTierService{
		tier: &model.ModelTier{ID: tierID, Name: "高级"},
	}).WithGroupService(groupSvc)
	router := gin.New()
	router.PUT("/api/v1/groups/:id/budget-tier", h.SetGroupBudgetTier)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/groups/1/budget-tier", bytes.NewBufferString(
		`{"model_tier_id":"`+tierID.String()+`","reason":"分配预算分层"}`,
	))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, 1, groupSvc.calls)
	var result struct {
		Data struct {
			UpdatedGroupIDs []uint     `json:"updated_group_ids"`
			ModelTierID     *uuid.UUID `json:"model_tier_id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.Equal(t, []uint{1}, result.Data.UpdatedGroupIDs)
	require.NotNil(t, result.Data.ModelTierID)
	require.Equal(t, tierID, *result.Data.ModelTierID)
}

func TestModelTierHandlerSetGroupBudgetTierBatchReturns200(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tierID := uuid.New()
	groupSvc := &fakeGroupBudgetTierService{
		group: &model.Group{ID: 1, Name: "gpt-4"},
	}
	h := handler.NewModelTierHandler(&fakeModelTierService{
		tier: &model.ModelTier{ID: tierID, Name: "高级"},
	}).WithGroupService(groupSvc)
	router := gin.New()
	router.PUT("/api/v1/groups/budget-tiers", h.SetGroupBudgetTierBatch)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/groups/budget-tiers", bytes.NewBufferString(
		`{"group_ids":[1,2,3],"model_tier_id":"`+tierID.String()+`","reason":"批量分配"}`,
	))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, 1, groupSvc.calls)
	var result struct {
		Data struct {
			UpdatedGroupIDs []uint     `json:"updated_group_ids"`
			ModelTierID     *uuid.UUID `json:"model_tier_id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.Equal(t, []uint{1, 2, 3}, result.Data.UpdatedGroupIDs)
	require.NotNil(t, result.Data.ModelTierID)
	require.Equal(t, tierID, *result.Data.ModelTierID)
}
