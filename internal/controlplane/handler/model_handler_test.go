package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

type fakeModelSvc struct {
	createFn            func(input service.CreateModelInput) (*model.Model, error)
	listFn              func(filter repository.ModelListFilter, page, pageSize int) ([]model.Model, int64, error)
	updateFn            func(id uuid.UUID, input service.UpdateModelInput) (*model.Model, error)
	deleteFn            func(id uuid.UUID) error
	checkNameFn         func(name string) (bool, error)
	createFromCatalogFn func(catalogID, providerID uuid.UUID, name string) (*model.Model, error)
	resetOverrideFn     func(modelID uuid.UUID, field string) error
	getModelFn          func(id uuid.UUID) (*model.Model, error)
	bindCatalogFn       func(modelID, catalogID uuid.UUID, resetFields bool) (*model.Model, error)
	unbindCatalogFn     func(modelID uuid.UUID) (*model.Model, error)
	batchBindCatalogFn  func(items []service.BatchBindInput) (*service.BatchBindResult, error)
}

func (f *fakeModelSvc) CreateModel(input service.CreateModelInput) (*model.Model, error) {
	if f.createFn != nil {
		return f.createFn(input)
	}
	return nil, nil
}
func (f *fakeModelSvc) ListModels(filter repository.ModelListFilter, page, pageSize int) ([]model.Model, int64, error) {
	if f.listFn != nil {
		return f.listFn(filter, page, pageSize)
	}
	return nil, 0, nil
}
func (f *fakeModelSvc) UpdateModel(id uuid.UUID, input service.UpdateModelInput) (*model.Model, error) {
	if f.updateFn != nil {
		return f.updateFn(id, input)
	}
	return nil, nil
}
func (f *fakeModelSvc) DeleteModel(id uuid.UUID) error {
	if f.deleteFn != nil {
		return f.deleteFn(id)
	}
	return nil
}
func (f *fakeModelSvc) CheckModelName(name string) (bool, error) {
	if f.checkNameFn != nil {
		return f.checkNameFn(name)
	}
	return true, nil
}

// BatchDeleteModels 默认 stub：返回 ids 长度作为 affected 数
func (f *fakeModelSvc) BatchDeleteModels(ids []uuid.UUID) (int64, error) {
	return int64(len(ids)), nil
}

// BatchUpdateModelStatus 默认 stub：返回 ids 长度作为 affected 数
func (f *fakeModelSvc) BatchUpdateModelStatus(ids []uuid.UUID, enabled bool) (int64, error) {
	return int64(len(ids)), nil
}

// CreateModelFromCatalog CAT-1 stub
func (f *fakeModelSvc) CreateModelFromCatalog(catalogID, providerID uuid.UUID, name string) (*model.Model, error) {
	if f.createFromCatalogFn != nil {
		return f.createFromCatalogFn(catalogID, providerID, name)
	}
	return nil, nil
}

// ResetOverride CAT-1 stub
func (f *fakeModelSvc) ResetOverride(modelID uuid.UUID, field string) error {
	if f.resetOverrideFn != nil {
		return f.resetOverrideFn(modelID, field)
	}
	return nil
}

// GetModel CAT-1 stub
func (f *fakeModelSvc) GetModel(id uuid.UUID) (*model.Model, error) {
	if f.getModelFn != nil {
		return f.getModelFn(id)
	}
	return nil, nil
}

// BindCatalog CAT-2 stub
func (f *fakeModelSvc) BindCatalog(modelID, catalogID uuid.UUID, resetFields bool) (*model.Model, error) {
	if f.bindCatalogFn != nil {
		return f.bindCatalogFn(modelID, catalogID, resetFields)
	}
	return nil, nil
}

// UnbindCatalog CAT-2 stub
func (f *fakeModelSvc) UnbindCatalog(modelID uuid.UUID) (*model.Model, error) {
	if f.unbindCatalogFn != nil {
		return f.unbindCatalogFn(modelID)
	}
	return nil, nil
}

// BatchBindCatalog CAT-2 stub
func (f *fakeModelSvc) BatchBindCatalog(items []service.BatchBindInput) (*service.BatchBindResult, error) {
	if f.batchBindCatalogFn != nil {
		return f.batchBindCatalogFn(items)
	}
	return &service.BatchBindResult{Applied: len(items), FailedIDs: map[string]string{}}, nil
}

var _ ModelServiceInterface = (*fakeModelSvc)(nil)

func newModelRouter(svc ModelServiceInterface) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewModelHandler(svc)
	r.POST("/api/v1/models", h.CreateModel)
	r.GET("/api/v1/models", h.ListModels)
	r.GET("/api/v1/models/check-name", h.CheckModelName)
	// CAT-1：静态路径必须在 :id 之前
	r.POST("/api/v1/models/from-catalog", h.CreateModelFromCatalog)
	r.POST("/api/v1/models/batch-bind-catalog", h.BatchBindCatalog)
	r.PUT("/api/v1/models/:id", h.UpdateModel)
	r.DELETE("/api/v1/models/:id", h.DeleteModel)
	r.POST("/api/v1/models/:id/reset-override", h.ResetModelOverride)
	r.POST("/api/v1/models/:id/bind-catalog", h.BindCatalog)
	r.POST("/api/v1/models/:id/unbind-catalog", h.UnbindCatalog)
	return r
}

func TestModelHandler_CreateModel_Success(t *testing.T) {
	providerID := uuid.New()
	created := &model.Model{ID: uuid.New(), Name: "gpt-4", ProviderID: providerID, ModelType: "chat"}
	svc := &fakeModelSvc{
		createFn: func(input service.CreateModelInput) (*model.Model, error) {
			assert.Equal(t, "gpt-4", input.Name)
			return created, nil
		},
	}
	r := newModelRouter(svc)

	body, _ := json.Marshal(map[string]string{
		"name":        "gpt-4",
		"provider_id": providerID.String(),
		"model_type":  "chat",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/models", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), "gpt-4")
}

func TestModelHandler_CreateModel_InvalidBody(t *testing.T) {
	svc := &fakeModelSvc{}
	r := newModelRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/models", bytes.NewBufferString("not-json"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid_request")
}

func TestModelHandler_CreateModel_NameExists(t *testing.T) {
	providerID := uuid.New()
	svc := &fakeModelSvc{createFn: func(input service.CreateModelInput) (*model.Model, error) {
		return nil, service.ErrModelNameExists
	}}
	r := newModelRouter(svc)

	body, _ := json.Marshal(map[string]string{
		"name": "dup", "provider_id": providerID.String(), "model_type": "chat",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/models", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	var resp map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, CodeModelNameConflict, resp["error"].(map[string]interface{})["code"])
}

func TestModelHandler_CreateModel_InvalidProvider(t *testing.T) {
	providerID := uuid.New()
	svc := &fakeModelSvc{createFn: func(input service.CreateModelInput) (*model.Model, error) {
		return nil, service.ErrModelProviderInvalid
	}}
	r := newModelRouter(svc)

	body, _ := json.Marshal(map[string]string{
		"name": "m", "provider_id": providerID.String(), "model_type": "chat",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/models", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, CodeModelProviderInvalid, resp["error"].(map[string]interface{})["code"])
}

func TestModelHandler_CreateModel_InternalError(t *testing.T) {
	providerID := uuid.New()
	svc := &fakeModelSvc{createFn: func(input service.CreateModelInput) (*model.Model, error) {
		return nil, errors.New("db error")
	}}
	r := newModelRouter(svc)

	body, _ := json.Marshal(map[string]string{
		"name": "m", "provider_id": providerID.String(), "model_type": "chat",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/models", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestModelHandler_ListModels_Success(t *testing.T) {
	providerID := uuid.New()
	list := []model.Model{{ID: uuid.New(), Name: "a", ProviderID: providerID, ModelType: "chat"}}
	svc := &fakeModelSvc{
		listFn: func(filter repository.ModelListFilter, page, pageSize int) ([]model.Model, int64, error) {
			assert.NotNil(t, filter.ProviderID)
			assert.Equal(t, providerID, *filter.ProviderID)
			assert.Equal(t, 1, page)
			return list, 1, nil
		},
	}
	r := newModelRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/models?provider_id="+providerID.String(), nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"total":1`)
}

func TestModelHandler_ListModels_InvalidProviderID(t *testing.T) {
	svc := &fakeModelSvc{}
	r := newModelRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/models?provider_id=not-a-uuid", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "bad_request")
}

func TestModelHandler_ListModels_DBError(t *testing.T) {
	svc := &fakeModelSvc{listFn: func(_ repository.ModelListFilter, page, pageSize int) ([]model.Model, int64, error) {
		return nil, 0, errors.New("boom")
	}}
	r := newModelRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/models", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestModelHandler_UpdateModel_Success(t *testing.T) {
	id := uuid.New()
	updated := &model.Model{ID: id, Name: "renamed", ModelType: "chat"}
	svc := &fakeModelSvc{updateFn: func(gotID uuid.UUID, input service.UpdateModelInput) (*model.Model, error) {
		assert.Equal(t, id, gotID)
		assert.Equal(t, "renamed", input.Name)
		return updated, nil
	}}
	r := newModelRouter(svc)

	body, _ := json.Marshal(map[string]string{"name": "renamed"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/models/"+id.String(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "renamed")
}

func TestModelHandler_UpdateModel_InvalidID(t *testing.T) {
	svc := &fakeModelSvc{}
	r := newModelRouter(svc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/models/not-a-uuid", bytes.NewBufferString("{}"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestModelHandler_UpdateModel_InvalidBody(t *testing.T) {
	svc := &fakeModelSvc{}
	r := newModelRouter(svc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/models/"+uuid.New().String(), bytes.NewBufferString("not-json"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestModelHandler_UpdateModel_NotFound(t *testing.T) {
	svc := &fakeModelSvc{updateFn: func(id uuid.UUID, input service.UpdateModelInput) (*model.Model, error) {
		return nil, service.ErrModelNotFound
	}}
	r := newModelRouter(svc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/models/"+uuid.New().String(), bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestModelHandler_UpdateModel_NameConflict(t *testing.T) {
	svc := &fakeModelSvc{updateFn: func(id uuid.UUID, input service.UpdateModelInput) (*model.Model, error) {
		return nil, service.ErrModelNameExists
	}}
	r := newModelRouter(svc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/models/"+uuid.New().String(), bytes.NewBufferString(`{"name":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestModelHandler_UpdateModel_InternalError(t *testing.T) {
	svc := &fakeModelSvc{updateFn: func(id uuid.UUID, input service.UpdateModelInput) (*model.Model, error) {
		return nil, errors.New("db")
	}}
	r := newModelRouter(svc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/models/"+uuid.New().String(), bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestModelHandler_DeleteModel_Success(t *testing.T) {
	id := uuid.New()
	called := false
	svc := &fakeModelSvc{deleteFn: func(gotID uuid.UUID) error {
		called = true
		assert.Equal(t, id, gotID)
		return nil
	}}
	r := newModelRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/models/"+id.String(), nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, called)
}

func TestModelHandler_DeleteModel_InvalidID(t *testing.T) {
	svc := &fakeModelSvc{}
	r := newModelRouter(svc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/models/invalid", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestModelHandler_DeleteModel_NotFound(t *testing.T) {
	svc := &fakeModelSvc{deleteFn: func(id uuid.UUID) error { return service.ErrModelNotFound }}
	r := newModelRouter(svc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/models/"+uuid.New().String(), nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestModelHandler_DeleteModel_InternalError(t *testing.T) {
	svc := &fakeModelSvc{deleteFn: func(id uuid.UUID) error { return errors.New("boom") }}
	r := newModelRouter(svc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/models/"+uuid.New().String(), nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// =============================================================================
// S0-2 修复回归：CreateModel/UpdateModel handler 必须把 7 个高级字段透传到 service
// =============================================================================

func TestModelHandler_CreateModel_AdvancedFieldsForwardedToService(t *testing.T) {
	providerID := uuid.New()
	var got service.CreateModelInput
	svc := &fakeModelSvc{
		createFn: func(input service.CreateModelInput) (*model.Model, error) {
			got = input
			return &model.Model{ID: uuid.New(), Name: input.Name, ProviderID: providerID, ModelType: input.ModelType}, nil
		},
	}
	r := newModelRouter(svc)

	body, _ := json.Marshal(map[string]any{
		"name":              "gpt-4o",
		"provider_id":       providerID.String(),
		"model_type":        "chat",
		"upstream_model_id": "openai/gpt-4o",
		"description":       "omni",
		"context_length":    128000,
		"max_output_tokens": 8192,
		"input_price":       0.0025,
		"output_price":      0.01,
		"weight":            7,
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/models", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "openai/gpt-4o", got.UpstreamModelID)
	assert.Equal(t, "omni", got.Description)
	if assert.NotNil(t, got.ContextLength) {
		assert.Equal(t, 128000, *got.ContextLength)
	}
	if assert.NotNil(t, got.MaxOutputTokens) {
		assert.Equal(t, 8192, *got.MaxOutputTokens)
	}
	if assert.NotNil(t, got.InputPrice) {
		assert.InDelta(t, 0.0025, *got.InputPrice, 1e-9)
	}
	if assert.NotNil(t, got.OutputPrice) {
		assert.InDelta(t, 0.01, *got.OutputPrice, 1e-9)
	}
	if assert.NotNil(t, got.Weight) {
		assert.Equal(t, 7, *got.Weight)
	}
}

func TestModelHandler_UpdateModel_AdvancedFieldsForwardedToService(t *testing.T) {
	modelID := uuid.New()
	var got service.UpdateModelInput
	svc := &fakeModelSvc{
		updateFn: func(id uuid.UUID, input service.UpdateModelInput) (*model.Model, error) {
			got = input
			enabled := true
			return &model.Model{ID: id, Name: "m", Enabled: &enabled}, nil
		},
	}
	r := newModelRouter(svc)

	body, _ := json.Marshal(map[string]any{
		"upstream_model_id": "u2",
		"description":       "d2",
		"context_length":    200000,
		"max_output_tokens": 16384,
		"input_price":       0.005,
		"output_price":      0.02,
		"weight":            9,
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/models/"+modelID.String(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	if assert.NotNil(t, got.UpstreamModelID) {
		assert.Equal(t, "u2", *got.UpstreamModelID)
	}
	if assert.NotNil(t, got.Description) {
		assert.Equal(t, "d2", *got.Description)
	}
	if assert.NotNil(t, got.ContextLength) {
		assert.Equal(t, 200000, *got.ContextLength)
	}
	if assert.NotNil(t, got.MaxOutputTokens) {
		assert.Equal(t, 16384, *got.MaxOutputTokens)
	}
	if assert.NotNil(t, got.InputPrice) {
		assert.InDelta(t, 0.005, *got.InputPrice, 1e-9)
	}
	if assert.NotNil(t, got.OutputPrice) {
		assert.InDelta(t, 0.02, *got.OutputPrice, 1e-9)
	}
	if assert.NotNil(t, got.Weight) {
		assert.Equal(t, 9, *got.Weight)
	}
}

// =============================================================================
// S0-3 修复回归：GET /api/v1/models/check-name 独立路由
// =============================================================================

func TestModelHandler_CheckModelName_Available(t *testing.T) {
	svc := &fakeModelSvc{checkNameFn: func(name string) (bool, error) {
		assert.Equal(t, "gpt-5", name)
		return true, nil
	}}
	r := newModelRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/models/check-name?name=gpt-5", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]map[string]bool
	require := assert.New(t)
	require.NoError(json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(true, resp["data"]["available"])
}

func TestModelHandler_CheckModelName_NotAvailable(t *testing.T) {
	svc := &fakeModelSvc{checkNameFn: func(string) (bool, error) { return false, nil }}
	r := newModelRouter(svc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/models/check-name?name=gpt-4o", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]map[string]bool
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.False(t, resp["data"]["available"])
}

func TestModelHandler_CheckModelName_MissingName(t *testing.T) {
	svc := &fakeModelSvc{}
	r := newModelRouter(svc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/models/check-name", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestModelHandler_CheckModelName_ServiceError(t *testing.T) {
	svc := &fakeModelSvc{checkNameFn: func(string) (bool, error) {
		return false, errors.New("db down")
	}}
	r := newModelRouter(svc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/models/check-name?name=foo", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// 防止 :id 通配路由抢占 check-name 静态路径的回归用例
// （router.go 中必须 GET /models/check-name 在 PUT/DELETE /models/:id 之前注册）
func TestModelHandler_CheckNameDoesNotCollideWithGetByID(t *testing.T) {
	svc := &fakeModelSvc{checkNameFn: func(string) (bool, error) { return true, nil }}
	r := newModelRouter(svc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/models/check-name?name=anything", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, "/models/check-name 必须命中 CheckModelName 而非被 :id 吞掉")
}

// ===== CAT-1：from-catalog 与 reset-override 路由集成测试 =====

func TestModelHandler_CreateModelFromCatalog_Success(t *testing.T) {
	catalogID := uuid.New()
	providerID := uuid.New()
	created := &model.Model{
		ID: uuid.New(), Name: "gpt-4o", ProviderID: providerID,
		ModelType: "chat", CatalogID: &catalogID,
	}
	svc := &fakeModelSvc{
		createFromCatalogFn: func(cID, pID uuid.UUID, name string) (*model.Model, error) {
			assert.Equal(t, catalogID, cID)
			assert.Equal(t, providerID, pID)
			assert.Equal(t, "gpt-4o", name)
			return created, nil
		},
	}
	r := newModelRouter(svc)

	body, _ := json.Marshal(map[string]string{
		"catalog_id":  catalogID.String(),
		"provider_id": providerID.String(),
		"name":        "gpt-4o",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/models/from-catalog", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), "gpt-4o")
}

func TestModelHandler_CreateModelFromCatalog_InvalidBody(t *testing.T) {
	svc := &fakeModelSvc{}
	r := newModelRouter(svc)

	body, _ := json.Marshal(map[string]string{"name": "x"}) // 缺 catalog_id / provider_id
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/models/from-catalog", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), CodeBadRequest)
}

func TestModelHandler_CreateModelFromCatalog_NameConflict(t *testing.T) {
	catalogID := uuid.New()
	providerID := uuid.New()
	svc := &fakeModelSvc{
		createFromCatalogFn: func(uuid.UUID, uuid.UUID, string) (*model.Model, error) {
			return nil, service.ErrModelNameExists
		},
	}
	r := newModelRouter(svc)

	body, _ := json.Marshal(map[string]string{
		"catalog_id":  catalogID.String(),
		"provider_id": providerID.String(),
		"name":        "duplicate-name",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/models/from-catalog", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), CodeModelNameConflict)
}

func TestModelHandler_ResetOverride_Success(t *testing.T) {
	modelID := uuid.New()
	updated := &model.Model{ID: modelID, Name: "gpt-4o", ContextLength: 128000, CtxOverridden: false}
	svc := &fakeModelSvc{
		resetOverrideFn: func(id uuid.UUID, field string) error {
			assert.Equal(t, modelID, id)
			assert.Equal(t, "context_length", field)
			return nil
		},
		getModelFn: func(id uuid.UUID) (*model.Model, error) {
			return updated, nil
		},
	}
	r := newModelRouter(svc)

	body, _ := json.Marshal(map[string]string{"field": "context_length"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/models/"+modelID.String()+"/reset-override", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "gpt-4o")
}

func TestModelHandler_ResetOverride_ModelNotFound(t *testing.T) {
	modelID := uuid.New()
	svc := &fakeModelSvc{
		resetOverrideFn: func(uuid.UUID, string) error { return service.ErrModelNotFound },
	}
	r := newModelRouter(svc)

	body, _ := json.Marshal(map[string]string{"field": "context_length"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/models/"+modelID.String()+"/reset-override", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), CodeModelNotFound)
}

func TestModelHandler_ResetOverride_NoCatalogLink(t *testing.T) {
	modelID := uuid.New()
	svc := &fakeModelSvc{
		resetOverrideFn: func(uuid.UUID, string) error {
			return errors.New("model " + modelID.String() + " has no catalog link")
		},
	}
	r := newModelRouter(svc)

	body, _ := json.Marshal(map[string]string{"field": "context_length"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/models/"+modelID.String()+"/reset-override", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), CodeCatalogNotBound)
}

func TestModelHandler_ResetOverride_InvalidField(t *testing.T) {
	modelID := uuid.New()
	svc := &fakeModelSvc{
		resetOverrideFn: func(uuid.UUID, string) error {
			return errors.New("unsupported override field: weight")
		},
	}
	r := newModelRouter(svc)

	body, _ := json.Marshal(map[string]string{"field": "weight"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/models/"+modelID.String()+"/reset-override", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), CodeBadRequest)
}

// 守护：from-catalog 静态路径必须在 :id 通配前注册，否则会被 :id 吞掉
func TestModelHandler_FromCatalogDoesNotCollideWithIDPath(t *testing.T) {
	catalogID := uuid.New()
	providerID := uuid.New()
	svc := &fakeModelSvc{
		createFromCatalogFn: func(uuid.UUID, uuid.UUID, string) (*model.Model, error) {
			return &model.Model{ID: uuid.New(), Name: "x"}, nil
		},
	}
	r := newModelRouter(svc)

	body, _ := json.Marshal(map[string]string{
		"catalog_id":  catalogID.String(),
		"provider_id": providerID.String(),
		"name":        "x",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/models/from-catalog", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code, "/models/from-catalog 必须命中 CreateModelFromCatalog 而非被 :id 吞掉")
}

// ========== CAT-2: BindCatalog ==========

func TestModelHandler_BindCatalog_Success(t *testing.T) {
	mid := uuid.New()
	cid := uuid.New()
	var captured struct {
		modelID, catalogID uuid.UUID
		reset              bool
	}
	svc := &fakeModelSvc{
		bindCatalogFn: func(modelID, catalogID uuid.UUID, resetFields bool) (*model.Model, error) {
			captured.modelID = modelID
			captured.catalogID = catalogID
			captured.reset = resetFields
			return &model.Model{ID: modelID, CatalogID: &catalogID, Name: "ok"}, nil
		},
	}
	r := newModelRouter(svc)

	body, _ := json.Marshal(map[string]any{"catalog_id": cid.String(), "reset_fields": true})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/models/"+mid.String()+"/bind-catalog", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, mid, captured.modelID)
	assert.Equal(t, cid, captured.catalogID)
	assert.True(t, captured.reset)
}

func TestModelHandler_BindCatalog_AlreadySame(t *testing.T) {
	svc := &fakeModelSvc{
		bindCatalogFn: func(uuid.UUID, uuid.UUID, bool) (*model.Model, error) {
			return nil, service.ErrCatalogAlreadyBoundToSame
		},
	}
	r := newModelRouter(svc)
	body, _ := json.Marshal(map[string]any{"catalog_id": uuid.New().String()})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/models/"+uuid.New().String()+"/bind-catalog", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), CodeAlreadyBoundToSame)
}

func TestModelHandler_BindCatalog_ModelNotFound(t *testing.T) {
	svc := &fakeModelSvc{
		bindCatalogFn: func(uuid.UUID, uuid.UUID, bool) (*model.Model, error) {
			return nil, service.ErrModelNotFound
		},
	}
	r := newModelRouter(svc)
	body, _ := json.Marshal(map[string]any{"catalog_id": uuid.New().String()})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/models/"+uuid.New().String()+"/bind-catalog", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), CodeModelNotFound)
}

func TestModelHandler_BindCatalog_CatalogNotFound(t *testing.T) {
	svc := &fakeModelSvc{
		bindCatalogFn: func(uuid.UUID, uuid.UUID, bool) (*model.Model, error) {
			return nil, errors.New("catalog 11111111-1111-1111-1111-111111111111 not found")
		},
	}
	r := newModelRouter(svc)
	body, _ := json.Marshal(map[string]any{"catalog_id": uuid.New().String()})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/models/"+uuid.New().String()+"/bind-catalog", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), CodeCatalogNotFound)
}

func TestModelHandler_BindCatalog_InvalidBody(t *testing.T) {
	svc := &fakeModelSvc{}
	r := newModelRouter(svc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/models/"+uuid.New().String()+"/bind-catalog", bytes.NewReader([]byte(`{"catalog_id":"not-a-uuid"}`)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ========== CAT-2: UnbindCatalog ==========

func TestModelHandler_UnbindCatalog_Success(t *testing.T) {
	mid := uuid.New()
	svc := &fakeModelSvc{
		unbindCatalogFn: func(id uuid.UUID) (*model.Model, error) {
			assert.Equal(t, mid, id)
			return &model.Model{ID: id, CatalogID: nil}, nil
		},
	}
	r := newModelRouter(svc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/models/"+mid.String()+"/unbind-catalog", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestModelHandler_UnbindCatalog_NotBound(t *testing.T) {
	svc := &fakeModelSvc{
		unbindCatalogFn: func(uuid.UUID) (*model.Model, error) {
			return nil, service.ErrCatalogNotBound
		},
	}
	r := newModelRouter(svc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/models/"+uuid.New().String()+"/unbind-catalog", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), CodeCatalogNotBound)
}

func TestModelHandler_UnbindCatalog_ModelNotFound(t *testing.T) {
	svc := &fakeModelSvc{
		unbindCatalogFn: func(uuid.UUID) (*model.Model, error) {
			return nil, service.ErrModelNotFound
		},
	}
	r := newModelRouter(svc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/models/"+uuid.New().String()+"/unbind-catalog", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), CodeModelNotFound)
}

// ========== CAT-2: BatchBindCatalog ==========

func TestModelHandler_BatchBindCatalog_Success(t *testing.T) {
	mid1 := uuid.New()
	mid2 := uuid.New()
	cid := uuid.New()

	var capturedItems []service.BatchBindInput
	svc := &fakeModelSvc{
		batchBindCatalogFn: func(items []service.BatchBindInput) (*service.BatchBindResult, error) {
			capturedItems = items
			return &service.BatchBindResult{
				Applied:   2,
				FailedIDs: map[string]string{},
			}, nil
		},
	}
	r := newModelRouter(svc)
	body, _ := json.Marshal(map[string]any{
		"bindings": []map[string]any{
			{"model_id": mid1.String(), "catalog_id": cid.String(), "reset_fields": true},
			{"model_id": mid2.String(), "catalog_id": cid.String(), "reset_fields": false},
		},
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/models/batch-bind-catalog", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"applied":2`)
	assert.Len(t, capturedItems, 2)
	assert.Equal(t, mid1, capturedItems[0].ModelID)
	assert.True(t, capturedItems[0].ResetFields)
	assert.False(t, capturedItems[1].ResetFields)
}

func TestModelHandler_BatchBindCatalog_PartialFailureBody(t *testing.T) {
	failID := uuid.New()
	svc := &fakeModelSvc{
		batchBindCatalogFn: func(items []service.BatchBindInput) (*service.BatchBindResult, error) {
			return &service.BatchBindResult{
				Applied:   1,
				FailedIDs: map[string]string{failID.String(): "model not found"},
			}, nil
		},
	}
	r := newModelRouter(svc)
	body, _ := json.Marshal(map[string]any{
		"bindings": []map[string]any{
			{"model_id": uuid.New().String(), "catalog_id": uuid.New().String()},
			{"model_id": failID.String(), "catalog_id": uuid.New().String()},
		},
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/models/batch-bind-catalog", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"applied":1`)
	assert.Contains(t, w.Body.String(), failID.String())
}

func TestModelHandler_BatchBindCatalog_EmptyRejected(t *testing.T) {
	svc := &fakeModelSvc{}
	r := newModelRouter(svc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/models/batch-bind-catalog", bytes.NewReader([]byte(`{"bindings":[]}`)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// 守护：batch-bind-catalog 静态路径必须在 :id 通配前注册
func TestModelHandler_BatchBindCatalog_DoesNotCollideWithIDPath(t *testing.T) {
	svc := &fakeModelSvc{
		batchBindCatalogFn: func(items []service.BatchBindInput) (*service.BatchBindResult, error) {
			return &service.BatchBindResult{Applied: 1, FailedIDs: map[string]string{}}, nil
		},
	}
	r := newModelRouter(svc)
	body, _ := json.Marshal(map[string]any{
		"bindings": []map[string]any{
			{"model_id": uuid.New().String(), "catalog_id": uuid.New().String()},
		},
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/models/batch-bind-catalog", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "/models/batch-bind-catalog 必须命中 BatchBindCatalog 而非被 :id 吞掉")
}
