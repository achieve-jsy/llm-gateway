package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/handler"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// fakeModelSvcForBatch 仅覆盖批量注册 handler 所需方法。
type fakeModelSvcForBatch struct {
	batchRes       *service.BatchCreateResult
	batchErr       error
	batchCatalogRes *service.BatchCreateResult
	batchCatalogErr error
	lastBatchInput  *service.BatchCreateModelsInput
	lastCatalogInput *service.BatchCreateFromCatalogInput
}

func (f *fakeModelSvcForBatch) CreateModel(service.CreateModelInput) (*model.Model, error) {
	return nil, nil
}
func (f *fakeModelSvcForBatch) ListModels(repository.ModelListFilter, int, int) ([]model.Model, int64, error) {
	return nil, 0, nil
}
func (f *fakeModelSvcForBatch) UpdateModel(uuid.UUID, service.UpdateModelInput) (*model.Model, error) {
	return nil, nil
}
func (f *fakeModelSvcForBatch) DeleteModel(uuid.UUID) error { return nil }
func (f *fakeModelSvcForBatch) CheckModelName(string, uuid.UUID) (bool, error) {
	return true, nil
}
func (f *fakeModelSvcForBatch) BatchDeleteModels([]uuid.UUID) (int64, error) { return 0, nil }
func (f *fakeModelSvcForBatch) BatchUpdateModelStatus([]uuid.UUID, bool) (int64, error) {
	return 0, nil
}
func (f *fakeModelSvcForBatch) BatchUpdateModelStatusWithOperator([]uuid.UUID, bool, *uuid.UUID, string) (int64, error) {
	return 0, nil
}
func (f *fakeModelSvcForBatch) CreateModelFromCatalog(uuid.UUID, uuid.UUID, string) (*model.Model, error) {
	return nil, nil
}
func (f *fakeModelSvcForBatch) ResetOverride(uuid.UUID, string) error { return nil }
func (f *fakeModelSvcForBatch) GetModel(uuid.UUID) (*model.Model, error) {
	return nil, nil
}
func (f *fakeModelSvcForBatch) BindCatalog(uuid.UUID, uuid.UUID, bool) (*model.Model, error) {
	return nil, nil
}
func (f *fakeModelSvcForBatch) UnbindCatalog(uuid.UUID) (*model.Model, error) { return nil, nil }
func (f *fakeModelSvcForBatch) BatchBindCatalog([]service.BatchBindInput) (*service.BatchBindResult, error) {
	return nil, nil
}

func (f *fakeModelSvcForBatch) BatchCreateModels(input service.BatchCreateModelsInput) (*service.BatchCreateResult, error) {
	cp := input
	f.lastBatchInput = &cp
	return f.batchRes, f.batchErr
}

func (f *fakeModelSvcForBatch) BatchCreateModelsFromCatalog(input service.BatchCreateFromCatalogInput) (*service.BatchCreateResult, error) {
	cp := input
	f.lastCatalogInput = &cp
	return f.batchCatalogRes, f.batchCatalogErr
}

func TestBatchCreateModelsHandler_OK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	modelID := uuid.New().String()
	fake := &fakeModelSvcForBatch{
		batchRes: &service.BatchCreateResult{
			SuccessCount: 1,
			SkipCount:    1,
			FailCount:    0,
			Results: []service.BatchCreateResultItem{
				{ProviderID: uuid.New().String(), Status: service.BatchCreateStatusCreated, ModelID: &modelID},
				{ProviderID: uuid.New().String(), Status: service.BatchCreateStatusSkipped, Reason: "model already exists"},
			},
		},
	}
	h := handler.NewModelHandler(fake)
	router := gin.New()
	router.POST("/api/v1/models/batch", h.BatchCreateModels)

	p1, p2 := uuid.New().String(), uuid.New().String()
	body := map[string]any{
		"name":         "gpt-4o",
		"provider_ids": []string{p1, p2},
		"model_type":   "chat",
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/models/batch", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"success_count":1`)
	require.Contains(t, w.Body.String(), `"skip_count":1`)
	require.NotNil(t, fake.lastBatchInput)
	require.Equal(t, "gpt-4o", fake.lastBatchInput.Name)
	require.Equal(t, []string{p1, p2}, fake.lastBatchInput.ProviderIDs)
}

func TestBatchCreateModelsHandler_EmptyProviderIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &fakeModelSvcForBatch{batchErr: service.ErrEmptyProviderIDs}
	h := handler.NewModelHandler(fake)
	router := gin.New()
	router.POST("/api/v1/models/batch", h.BatchCreateModels)

	// binding 要求 min=1，空数组应在 bind 阶段 400；这里用合法结构但 service 返回 ErrEmptyProviderIDs
	// 模拟去重后为空的路径：直接让 fake 返回错误，并跳过 bind——用含空串的 ids 触发 service。
	// 更直接：构造绑定通过但 service 报错。
	body := `{"name":"gpt-4o","provider_ids":["` + uuid.New().String() + `"],"model_type":"chat"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/models/batch", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "provider_ids is required")
}

func TestBatchCreateModelsHandler_InvalidBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := handler.NewModelHandler(&fakeModelSvcForBatch{})
	router := gin.New()
	router.POST("/api/v1/models/batch", h.BatchCreateModels)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/models/batch", bytes.NewBufferString(`{"name":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestBatchCreateModelsFromCatalogHandler_CatalogNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	catID := uuid.New()
	fake := &fakeModelSvcForBatch{
		batchCatalogErr: catNotFoundErr(catID),
	}
	h := handler.NewModelHandler(fake)
	router := gin.New()
	router.POST("/api/v1/models/batch-from-catalog", h.BatchCreateModelsFromCatalog)

	body := map[string]any{
		"catalog_id":   catID.String(),
		"provider_ids": []string{uuid.New().String()},
		"name":         "gpt-4o",
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/models/batch-from-catalog", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
	require.Contains(t, w.Body.String(), "not found")
}

func TestBatchCreateModelsFromCatalogHandler_OK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	modelID := uuid.New().String()
	catID := uuid.New()
	p1 := uuid.New().String()
	fake := &fakeModelSvcForBatch{
		batchCatalogRes: &service.BatchCreateResult{
			SuccessCount: 1,
			Results: []service.BatchCreateResultItem{
				{ProviderID: p1, Status: service.BatchCreateStatusCreated, ModelID: &modelID},
			},
		},
	}
	h := handler.NewModelHandler(fake)
	router := gin.New()
	router.POST("/api/v1/models/batch-from-catalog", h.BatchCreateModelsFromCatalog)

	body := map[string]any{
		"catalog_id":   catID.String(),
		"provider_ids": []string{p1},
		"name":         "gpt-4o",
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/models/batch-from-catalog", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"success_count":1`)
	require.NotNil(t, fake.lastCatalogInput)
	require.Equal(t, catID, fake.lastCatalogInput.CatalogID)
	require.Equal(t, "gpt-4o", fake.lastCatalogInput.Name)
}

type catalogMissingError struct{ id uuid.UUID }

func (e catalogMissingError) Error() string {
	return "catalog " + e.id.String() + " not found"
}

func catNotFoundErr(id uuid.UUID) error { return catalogMissingError{id: id} }
