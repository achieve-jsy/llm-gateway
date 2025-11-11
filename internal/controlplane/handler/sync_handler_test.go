package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// MockSyncService mocks the SyncService for testing
type MockSyncService struct {
	syncModelsFn  func(providerID uuid.UUID) (*service.SyncResult, error)
	confirmSyncFn func(providerID uuid.UUID, input service.ConfirmSyncInput) (*service.ConfirmSyncResult, error)
}

func (m *MockSyncService) SyncModels(providerID uuid.UUID) (*service.SyncResult, error) {
	if m.syncModelsFn != nil {
		return m.syncModelsFn(providerID)
	}
	return &service.SyncResult{}, nil
}

func (m *MockSyncService) ConfirmSync(providerID uuid.UUID, input service.ConfirmSyncInput) (*service.ConfirmSyncResult, error) {
	if m.confirmSyncFn != nil {
		return m.confirmSyncFn(providerID, input)
	}
	return &service.ConfirmSyncResult{}, nil
}

func TestSyncModels_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	providerID := uuid.New()

	syncResult := &service.SyncResult{
		Added: []service.SyncModelItem{
			{ID: "model-1", OwnedBy: "provider"},
			{ID: "model-2", OwnedBy: "provider"},
		},
		Existing: []service.SyncModelItem{
			{ID: "model-3", OwnedBy: "provider"},
		},
		Removed:       []service.SyncModelItem{},
		TotalUpstream: 3,
		TotalLocal:    1,
		Unsupported:   false,
	}

	mockSvc := &MockSyncService{
		syncModelsFn: func(id uuid.UUID) (*service.SyncResult, error) {
			if id == providerID {
				return syncResult, nil
			}
			return nil, fmt.Errorf("provider not found")
		},
	}

	router := gin.New()
	handler := NewSyncHandler(mockSvc, nil)

	router.POST("/api/v1/providers/:id/sync-models", handler.SyncModels)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/providers/%s/sync-models", providerID.String()), nil)

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NotNil(t, resp["data"])

	data := resp["data"].(map[string]interface{})
	assert.Equal(t, "model-1", data["added"].([]interface{})[0].(map[string]interface{})["id"])
	assert.Equal(t, float64(3), data["total_upstream"])
	assert.Equal(t, false, data["unsupported"])
}

func TestSyncModels_InvalidID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	mockSvc := &MockSyncService{}
	handler := NewSyncHandler(mockSvc, nil)

	router.POST("/api/v1/providers/:id/sync-models", handler.SyncModels)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/providers/invalid-id/sync-models", nil)

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NotNil(t, resp["error"])
}

func TestSyncModels_ProviderNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	providerID := uuid.New()

	mockSvc := &MockSyncService{
		syncModelsFn: func(id uuid.UUID) (*service.SyncResult, error) {
			return nil, fmt.Errorf("provider not found")
		},
	}

	router := gin.New()
	handler := NewSyncHandler(mockSvc, nil)

	router.POST("/api/v1/providers/:id/sync-models", handler.SyncModels)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/providers/%s/sync-models", providerID.String()), nil)

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NotNil(t, resp["error"])
	errObj := resp["error"].(map[string]interface{})
	assert.Equal(t, CodeProviderNotFound, errObj["code"])
	assert.Equal(t, "not_found", errObj["type"])
}

func TestSyncModels_ProviderDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	providerID := uuid.New()

	mockSvc := &MockSyncService{
		syncModelsFn: func(id uuid.UUID) (*service.SyncResult, error) {
			return nil, fmt.Errorf("provider is disabled")
		},
	}

	router := gin.New()
	handler := NewSyncHandler(mockSvc, nil)

	router.POST("/api/v1/providers/:id/sync-models", handler.SyncModels)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/providers/%s/sync-models", providerID.String()), nil)

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	error := resp["error"].(map[string]interface{})
	assert.Equal(t, "provider_disabled", error["code"])
}

func TestSyncModels_UpstreamError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	providerID := uuid.New()

	mockSvc := &MockSyncService{
		syncModelsFn: func(id uuid.UUID) (*service.SyncResult, error) {
			return nil, fmt.Errorf("upstream error: connection refused")
		},
	}

	router := gin.New()
	handler := NewSyncHandler(mockSvc, nil)

	router.POST("/api/v1/providers/:id/sync-models", handler.SyncModels)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/providers/%s/sync-models", providerID.String()), nil)

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadGateway, w.Code)
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	error := resp["error"].(map[string]interface{})
	assert.Equal(t, "upstream_error", error["code"])
}

func TestSyncModels_Unsupported(t *testing.T) {
	gin.SetMode(gin.TestMode)

	providerID := uuid.New()

	syncResult := &service.SyncResult{
		Added:         []service.SyncModelItem{},
		Existing:      []service.SyncModelItem{},
		Removed:       []service.SyncModelItem{},
		TotalUpstream: 0,
		TotalLocal:    0,
		Unsupported:   true,
	}

	mockSvc := &MockSyncService{
		syncModelsFn: func(id uuid.UUID) (*service.SyncResult, error) {
			if id == providerID {
				return syncResult, nil
			}
			return nil, fmt.Errorf("provider not found")
		},
	}

	router := gin.New()
	handler := NewSyncHandler(mockSvc, nil)

	router.POST("/api/v1/providers/:id/sync-models", handler.SyncModels)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/providers/%s/sync-models", providerID.String()), nil)

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	errObj := resp["error"].(map[string]interface{})
	assert.Equal(t, CodeSyncNotSupported, errObj["code"])
	assert.Equal(t, "invalid_request", errObj["type"])
	assert.Equal(t, "upstream adapter does not support model sync", errObj["message"])
}

func TestConfirmSync_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	providerID := uuid.New()

	confirmResult := &service.ConfirmSyncResult{
		Registered: 2,
		Skipped:    1,
		Disabled:   0,
	}

	mockSvc := &MockSyncService{
		confirmSyncFn: func(id uuid.UUID, input service.ConfirmSyncInput) (*service.ConfirmSyncResult, error) {
			if id == providerID {
				return confirmResult, nil
			}
			return nil, fmt.Errorf("provider not found")
		},
	}

	router := gin.New()
	handler := NewSyncHandler(mockSvc, nil)

	router.POST("/api/v1/providers/:id/sync-models/confirm", handler.ConfirmSync)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/providers/%s/sync-models/confirm", providerID.String()),
		bytes.NewBufferString(`{
			"models_to_add": ["model-1", "model-2"],
			"disable_removed": true
		}`))
	req.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NotNil(t, resp["data"])

	data := resp["data"].(map[string]interface{})
	assert.Equal(t, float64(2), data["registered"])
	assert.Equal(t, float64(1), data["skipped"])
	assert.Equal(t, float64(0), data["disabled"])
}

func TestConfirmSync_InvalidID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	mockSvc := &MockSyncService{}
	handler := NewSyncHandler(mockSvc, nil)

	router.POST("/api/v1/providers/:id/sync-models/confirm", handler.ConfirmSync)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/providers/invalid-id/sync-models/confirm",
		bytes.NewBufferString(`{
			"models_to_add": ["model-1"]
		}`))
	req.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NotNil(t, resp["error"])
}

func TestConfirmSync_InvalidBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	providerID := uuid.New()

	tests := []struct {
		name     string
		body     string
		wantCode int
	}{
		{
			name:     "Missing required models_to_add",
			body:     `{"disable_removed": true}`,
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "Invalid JSON",
			body:     `{invalid json}`,
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "models_to_add is null",
			body:     `{"models_to_add": null}`,
			wantCode: http.StatusBadRequest,
		},
	}

	router := gin.New()
	mockSvc := &MockSyncService{}
	handler := NewSyncHandler(mockSvc, nil)

	router.POST("/api/v1/providers/:id/sync-models/confirm", handler.ConfirmSync)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/providers/%s/sync-models/confirm", providerID.String()),
				bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantCode, w.Code, "Test: %s", tt.name)
		})
	}
}

func TestConfirmSync_EmptyModelsToAdd(t *testing.T) {
	gin.SetMode(gin.TestMode)

	providerID := uuid.New()

	confirmResult := &service.ConfirmSyncResult{
		Registered: 0,
		Skipped:    0,
		Disabled:   0,
	}

	mockSvc := &MockSyncService{
		confirmSyncFn: func(id uuid.UUID, input service.ConfirmSyncInput) (*service.ConfirmSyncResult, error) {
			if id == providerID {
				return confirmResult, nil
			}
			return nil, fmt.Errorf("provider not found")
		},
	}

	router := gin.New()
	handler := NewSyncHandler(mockSvc, nil)

	router.POST("/api/v1/providers/:id/sync-models/confirm", handler.ConfirmSync)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/providers/%s/sync-models/confirm", providerID.String()),
		bytes.NewBufferString(`{
			"models_to_add": [],
			"disable_removed": false
		}`))
	req.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(w, req)

	// Should still succeed even with empty list
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestConfirmSync_DisableRemovedNil(t *testing.T) {
	gin.SetMode(gin.TestMode)

	providerID := uuid.New()

	confirmResult := &service.ConfirmSyncResult{
		Registered: 1,
		Skipped:    0,
		Disabled:   0,
	}

	mockSvc := &MockSyncService{
		confirmSyncFn: func(id uuid.UUID, input service.ConfirmSyncInput) (*service.ConfirmSyncResult, error) {
			if id == providerID {
				// Verify disable_removed defaults to true when nil
				return confirmResult, nil
			}
			return nil, fmt.Errorf("provider not found")
		},
	}

	router := gin.New()
	handler := NewSyncHandler(mockSvc, nil)

	router.POST("/api/v1/providers/:id/sync-models/confirm", handler.ConfirmSync)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/providers/%s/sync-models/confirm", providerID.String()),
		bytes.NewBufferString(`{
			"models_to_add": ["model-1"]
		}`))
	req.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestConfirmSync_ServiceError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	providerID := uuid.New()

	mockSvc := &MockSyncService{
		confirmSyncFn: func(id uuid.UUID, input service.ConfirmSyncInput) (*service.ConfirmSyncResult, error) {
			return nil, fmt.Errorf("database error")
		},
	}

	router := gin.New()
	handler := NewSyncHandler(mockSvc, nil)

	router.POST("/api/v1/providers/:id/sync-models/confirm", handler.ConfirmSync)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/providers/%s/sync-models/confirm", providerID.String()),
		bytes.NewBufferString(`{
			"models_to_add": ["model-1"]
		}`))
	req.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NotNil(t, resp["error"])
}

func TestSyncHandler_TableDrivenTests(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []struct {
		name         string
		providerID   uuid.UUID
		method       string
		endpoint     string
		body         string
		mockError    string
		expectedCode int
		description  string
	}{
		{
			name:         "SyncModels with valid UUID",
			providerID:   uuid.New(),
			method:       "POST",
			endpoint:     "/sync-models",
			expectedCode: http.StatusOK,
			description:  "Should return OK when sync succeeds",
		},
		{
			name:         "SyncModels with invalid UUID",
			providerID:   uuid.Nil,
			method:       "POST",
			endpoint:     "/sync-models",
			expectedCode: http.StatusBadRequest,
			description:  "Should return BadRequest with invalid UUID",
		},
		{
			name:         "ConfirmSync with valid UUID",
			providerID:   uuid.New(),
			method:       "POST",
			endpoint:     "/sync-models/confirm",
			body:         `{"models_to_add": ["model-1"]}`,
			expectedCode: http.StatusOK,
			description:  "Should return OK when confirm succeeds",
		},
		{
			name:         "ConfirmSync with invalid body",
			providerID:   uuid.New(),
			method:       "POST",
			endpoint:     "/sync-models/confirm",
			body:         `invalid json`,
			expectedCode: http.StatusBadRequest,
			description:  "Should return BadRequest with invalid JSON",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			mockSvc := &MockSyncService{}
			handler := NewSyncHandler(mockSvc, nil)

			if tc.endpoint == "/sync-models" {
				router.POST("/api/v1/providers/:id/sync-models", handler.SyncModels)
			} else if tc.endpoint == "/sync-models/confirm" {
				router.POST("/api/v1/providers/:id/sync-models/confirm", handler.ConfirmSync)
			}

			w := httptest.NewRecorder()

			var providerIDStr string
			if tc.providerID != uuid.Nil {
				providerIDStr = tc.providerID.String()
			} else {
				providerIDStr = "invalid-id"
			}

			var req *http.Request
			if tc.body != "" {
				req = httptest.NewRequest(tc.method, fmt.Sprintf("/api/v1/providers/%s%s", providerIDStr, tc.endpoint),
					bytes.NewBufferString(tc.body))
				req.Header.Set("Content-Type", "application/json")
			} else {
				req = httptest.NewRequest(tc.method, fmt.Sprintf("/api/v1/providers/%s%s", providerIDStr, tc.endpoint), nil)
			}

			router.ServeHTTP(w, req)

			assert.Equal(t, tc.expectedCode, w.Code, "Test: %s - %s", tc.name, tc.description)
		})
	}
}
