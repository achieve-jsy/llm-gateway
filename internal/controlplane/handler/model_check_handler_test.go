package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func newModelRouterWithBatch(svc ModelServiceInterface) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewModelHandler(svc)
	r.POST("/api/v1/models", h.CreateModel)
	r.GET("/api/v1/models", h.ListModels)
	r.GET("/api/v1/models/check-name", h.CheckModelName)
	r.POST("/api/v1/models/from-catalog", h.CreateModelFromCatalog)
	r.POST("/api/v1/models/batch-bind-catalog", h.BatchBindCatalog)
	r.DELETE("/api/v1/models/batch", h.BatchDeleteModels)
	r.PATCH("/api/v1/models/batch-status", h.BatchUpdateModelStatus)
	r.PUT("/api/v1/models/:id", h.UpdateModel)
	r.DELETE("/api/v1/models/:id", h.DeleteModel)
	r.POST("/api/v1/models/:id/reset-override", h.ResetModelOverride)
	r.POST("/api/v1/models/:id/bind-catalog", h.BindCatalog)
	r.POST("/api/v1/models/:id/unbind-catalog", h.UnbindCatalog)
	return r
}

func TestClassifyModelCheckError(t *testing.T) {
	tests := []struct {
		errMsg string
		want   string
	}{
		{"HTTP 401: Unauthorized", "invalid_api_key"},
		{"Invalid API key provided", "invalid_api_key"},
		{"invalid key format", "invalid_api_key"},
		{"authentication fails", "invalid_api_key"},
		{"403 Forbidden", "access_denied"},
		{"access denied for this resource", "access_denied"},
		{"404 Model not found", "model_not_found"},
		{"model does not exist", "model_not_found"},
		{"429 Too many requests", "rate_limited"},
		{"rate limit exceeded", "rate_limited"},
		{"limit_requests exceeded", "rate_limited"},
		{"product is not activated", "product_not_activated"},
		{"not activated in this region", "product_not_activated"},
		{"Connection timeout after 30s", "timeout"},
		{"请求超时", "timeout"},
		{"random unknown error", "other"},
	}

	for _, tt := range tests {
		got := classifyModelCheckError(tt.errMsg)
		assert.Equal(t, tt.want, got, "classifyModelCheckError(%q)", tt.errMsg)
	}
}

func TestShouldAutoReenableModel(t *testing.T) {
	disabled := false
	enabled := true

	assert.False(t, shouldAutoReenableModel(nil, 3))
	assert.False(t, shouldAutoReenableModel(&model.Model{Enabled: &enabled, ConsecutiveFailures: 3}, 3))
	assert.False(t, shouldAutoReenableModel(&model.Model{Enabled: &disabled, ConsecutiveFailures: 2}, 3))
	assert.True(t, shouldAutoReenableModel(&model.Model{Enabled: &disabled, ConsecutiveFailures: 3}, 3))
	assert.True(t, shouldAutoReenableModel(&model.Model{Enabled: &disabled, ConsecutiveFailures: 5}, 3))
}

func TestModelHandler_BatchDeleteModels(t *testing.T) {
	ids := []uuid.UUID{uuid.New(), uuid.New()}
	body, _ := json.Marshal(map[string]interface{}{
		"model_ids": []string{ids[0].String(), ids[1].String()},
	})

	svc := &fakeModelSvc{}
	r := newModelRouterWithBatch(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/models/batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestModelHandler_BatchDeleteModels_EmptyIDs(t *testing.T) {
	body, _ := json.Marshal(map[string]interface{}{
		"model_ids": []string{},
	})

	svc := &fakeModelSvc{}
	r := newModelRouterWithBatch(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/models/batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestModelHandler_BatchDeleteModels_InvalidID(t *testing.T) {
	body, _ := json.Marshal(map[string]interface{}{
		"model_ids": []string{"not-a-uuid"},
	})

	svc := &fakeModelSvc{}
	r := newModelRouterWithBatch(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/models/batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestModelHandler_BatchUpdateModelStatus_Enable(t *testing.T) {
	id := uuid.New()
	body, _ := json.Marshal(map[string]interface{}{
		"model_ids": []string{id.String()},
		"enabled":   true,
	})

	svc := &fakeModelSvc{}
	r := newModelRouterWithBatch(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("PATCH", "/api/v1/models/batch-status", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]interface{})
	assert.Equal(t, float64(1), data["updated"])
}

func TestModelHandler_BatchUpdateModelStatus_InvalidID(t *testing.T) {
	body, _ := json.Marshal(map[string]interface{}{
		"model_ids": []string{"invalid"},
		"enabled":   true,
	})

	svc := &fakeModelSvc{}
	r := newModelRouterWithBatch(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("PATCH", "/api/v1/models/batch-status", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestModelHandler_BatchUpdateModelStatus_MissingEnabled(t *testing.T) {
	body, _ := json.Marshal(map[string]interface{}{
		"model_ids": []string{uuid.New().String()},
	})

	svc := &fakeModelSvc{}
	r := newModelRouterWithBatch(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("PATCH", "/api/v1/models/batch-status", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestModelHandler_BatchUpdateModelStatus_Disable(t *testing.T) {
	id1 := uuid.New()
	id2 := uuid.New()
	body, _ := json.Marshal(map[string]interface{}{
		"model_ids": []string{id1.String(), id2.String()},
		"enabled":   false,
	})

	svc := &fakeModelSvc{}
	r := newModelRouterWithBatch(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("PATCH", "/api/v1/models/batch-status", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]interface{})
	assert.Equal(t, float64(2), data["updated"])
}

func TestModelHandler_ListModels_NameFilter(t *testing.T) {
	svc := &fakeModelSvc{
		listFn: func(filter repository.ModelListFilter, page, pageSize int) ([]model.Model, int64, error) {
			assert.Equal(t, "gpt", filter.Name)
			return nil, 0, nil
		},
	}
	r := newModelRouterWithBatch(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/models?name=gpt", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestModelHandler_ListModels_ModelTypeFilter(t *testing.T) {
	svc := &fakeModelSvc{
		listFn: func(filter repository.ModelListFilter, page, pageSize int) ([]model.Model, int64, error) {
			assert.Equal(t, "embedding", filter.ModelType)
			return nil, 0, nil
		},
	}
	r := newModelRouterWithBatch(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/models?model_type=embedding", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestModelHandler_ListModels_Pagination(t *testing.T) {
	svc := &fakeModelSvc{
		listFn: func(filter repository.ModelListFilter, page, pageSize int) ([]model.Model, int64, error) {
			assert.Equal(t, 2, page)
			assert.Equal(t, 50, pageSize)
			return nil, 0, nil
		},
	}
	r := newModelRouterWithBatch(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/models?page=2&page_size=50", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}
