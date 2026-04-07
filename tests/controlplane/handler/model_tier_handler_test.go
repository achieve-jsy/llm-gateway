package handler_test

import (
	"bytes"
	"context"
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
	deleteErr error
	createErr error
	tier      *model.ModelTier
}

func (f *fakeModelTierService) List(context.Context, bool) ([]model.ModelTier, error) {
	return []model.ModelTier{*f.tier}, nil
}
func (f *fakeModelTierService) Get(context.Context, uuid.UUID) (*model.ModelTier, error) {
	return f.tier, nil
}
func (f *fakeModelTierService) Create(context.Context, service.CreateModelTierInput) (*model.ModelTier, error) {
	return f.tier, f.createErr
}
func (f *fakeModelTierService) Update(context.Context, uuid.UUID, service.UpdateModelTierInput) (*model.ModelTier, error) {
	return f.tier, nil
}
func (f *fakeModelTierService) SetEnabled(context.Context, uuid.UUID, bool, string) (*model.ModelTier, error) {
	return f.tier, nil
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
