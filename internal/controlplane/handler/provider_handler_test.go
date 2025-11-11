package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// MockProviderService 实现 ProviderServiceInterface
type MockProviderService struct {
	createProviderFn       func(input service.CreateProviderInput) (*model.Provider, error)
	listProvidersFn        func(filter repository.ProviderListFilter, page, pageSize int) ([]service.ProviderResponse, int64, error)
	getProviderFn          func(id uuid.UUID) (*service.ProviderResponse, error)
	updateProviderFn       func(id uuid.UUID, input service.UpdateProviderInput) (*service.ProviderResponse, error)
	toggleProviderStatusFn func(id uuid.UUID, enabled bool) (*service.ProviderResponse, error)
	deleteProviderFn       func(id uuid.UUID) error
}

func (m *MockProviderService) CreateProvider(input service.CreateProviderInput) (*model.Provider, error) {
	if m.createProviderFn != nil {
		return m.createProviderFn(input)
	}
	return nil, nil
}
func (m *MockProviderService) ListProviders(filter repository.ProviderListFilter, page, pageSize int) ([]service.ProviderResponse, int64, error) {
	if m.listProvidersFn != nil {
		return m.listProvidersFn(filter, page, pageSize)
	}
	return nil, 0, nil
}
func (m *MockProviderService) GetProvider(id uuid.UUID) (*service.ProviderResponse, error) {
	if m.getProviderFn != nil {
		return m.getProviderFn(id)
	}
	return nil, nil
}
func (m *MockProviderService) UpdateProvider(id uuid.UUID, input service.UpdateProviderInput) (*service.ProviderResponse, error) {
	if m.updateProviderFn != nil {
		return m.updateProviderFn(id, input)
	}
	return nil, nil
}
func (m *MockProviderService) ToggleProviderStatus(id uuid.UUID, enabled bool) (*service.ProviderResponse, error) {
	if m.toggleProviderStatusFn != nil {
		return m.toggleProviderStatusFn(id, enabled)
	}
	return nil, nil
}
func (m *MockProviderService) DeleteProvider(id uuid.UUID) error {
	if m.deleteProviderFn != nil {
		return m.deleteProviderFn(id)
	}
	return nil
}

var _ ProviderServiceInterface = (*MockProviderService)(nil)

func newProviderRouter(svc ProviderServiceInterface) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewProviderHandler(svc)
	r := gin.New()
	r.POST("/api/v1/providers", h.CreateProvider)
	r.GET("/api/v1/providers", h.ListProviders)
	r.GET("/api/v1/providers/:id", h.GetProvider)
	r.PUT("/api/v1/providers/:id", h.UpdateProvider)
	r.PATCH("/api/v1/providers/:id/status", h.ToggleProviderStatus)
	r.DELETE("/api/v1/providers/:id", h.DeleteProvider)
	return r
}

func TestCreateProvider_Success(t *testing.T) {
	pid := uuid.New()
	svc := &MockProviderService{
		createProviderFn: func(input service.CreateProviderInput) (*model.Provider, error) {
			return &model.Provider{ID: pid, Name: input.Name, Enabled: true, CreatedAt: time.Now()}, nil
		},
	}
	r := newProviderRouter(svc)

	body := `{"name":"p1","definition_id":"openai","endpoint":"https://x","credentials":"sk-x"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/providers", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var resp map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotNil(t, resp["data"])
}

func TestCreateProvider_NameConflict(t *testing.T) {
	svc := &MockProviderService{
		createProviderFn: func(input service.CreateProviderInput) (*model.Provider, error) {
			return nil, service.ErrProviderNameExists
		},
	}
	r := newProviderRouter(svc)
	body := `{"name":"dup","definition_id":"openai","endpoint":"https://x","credentials":"sk-x"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/providers", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusConflict, w.Code)
	var resp map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, CodeProviderNameConflict, resp["error"].(map[string]interface{})["code"])
}

func TestCreateProvider_InternalError(t *testing.T) {
	svc := &MockProviderService{
		createProviderFn: func(service.CreateProviderInput) (*model.Provider, error) {
			return nil, errors.New("db err")
		},
	}
	r := newProviderRouter(svc)
	body := `{"name":"p1","definition_id":"openai","endpoint":"https://x","credentials":"sk-x"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/providers", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCreateProvider_InvalidBody(t *testing.T) {
	svc := &MockProviderService{}
	r := newProviderRouter(svc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/providers", bytes.NewBufferString(`{"name":""}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListProviders_Success(t *testing.T) {
	svc := &MockProviderService{
		listProvidersFn: func(_ repository.ProviderListFilter, page, pageSize int) ([]service.ProviderResponse, int64, error) {
			return []service.ProviderResponse{{ID: uuid.New(), Name: "p1"}}, 1, nil
		},
	}
	r := newProviderRouter(svc)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/providers?page=1&page_size=20", nil))
	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(1), resp["total"])
}

func TestListProviders_Error(t *testing.T) {
	svc := &MockProviderService{
		listProvidersFn: func(_ repository.ProviderListFilter, _, _ int) ([]service.ProviderResponse, int64, error) {
			return nil, 0, errors.New("db err")
		},
	}
	r := newProviderRouter(svc)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/providers", nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestGetProvider_Success(t *testing.T) {
	id := uuid.New()
	svc := &MockProviderService{
		getProviderFn: func(qid uuid.UUID) (*service.ProviderResponse, error) {
			assert.Equal(t, id, qid)
			return &service.ProviderResponse{ID: qid, Name: "p1"}, nil
		},
	}
	r := newProviderRouter(svc)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", fmt.Sprintf("/api/v1/providers/%s", id), nil))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGetProvider_InvalidID(t *testing.T) {
	r := newProviderRouter(&MockProviderService{})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/providers/bad-id", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetProvider_NotFound(t *testing.T) {
	svc := &MockProviderService{
		getProviderFn: func(uuid.UUID) (*service.ProviderResponse, error) {
			return nil, service.ErrProviderNotFound
		},
	}
	r := newProviderRouter(svc)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", fmt.Sprintf("/api/v1/providers/%s", uuid.New()), nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestGetProvider_InternalError(t *testing.T) {
	svc := &MockProviderService{
		getProviderFn: func(uuid.UUID) (*service.ProviderResponse, error) { return nil, errors.New("xx") },
	}
	r := newProviderRouter(svc)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", fmt.Sprintf("/api/v1/providers/%s", uuid.New()), nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestUpdateProvider_Success(t *testing.T) {
	id := uuid.New()
	svc := &MockProviderService{
		updateProviderFn: func(qid uuid.UUID, input service.UpdateProviderInput) (*service.ProviderResponse, error) {
			return &service.ProviderResponse{ID: qid, Name: input.Name}, nil
		},
	}
	r := newProviderRouter(svc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", fmt.Sprintf("/api/v1/providers/%s", id),
		bytes.NewBufferString(`{"name":"new"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestUpdateProvider_InvalidID(t *testing.T) {
	r := newProviderRouter(&MockProviderService{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/providers/bad", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdateProvider_InvalidBody(t *testing.T) {
	r := newProviderRouter(&MockProviderService{})
	id := uuid.New()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", fmt.Sprintf("/api/v1/providers/%s", id), bytes.NewBufferString(`bad json`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdateProvider_NotFound(t *testing.T) {
	svc := &MockProviderService{
		updateProviderFn: func(uuid.UUID, service.UpdateProviderInput) (*service.ProviderResponse, error) {
			return nil, service.ErrProviderNotFound
		},
	}
	r := newProviderRouter(svc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", fmt.Sprintf("/api/v1/providers/%s", uuid.New()), bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUpdateProvider_NameConflict(t *testing.T) {
	svc := &MockProviderService{
		updateProviderFn: func(uuid.UUID, service.UpdateProviderInput) (*service.ProviderResponse, error) {
			return nil, service.ErrProviderNameExists
		},
	}
	r := newProviderRouter(svc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", fmt.Sprintf("/api/v1/providers/%s", uuid.New()),
		bytes.NewBufferString(`{"name":"dup"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestUpdateProvider_InternalError(t *testing.T) {
	svc := &MockProviderService{
		updateProviderFn: func(uuid.UUID, service.UpdateProviderInput) (*service.ProviderResponse, error) {
			return nil, errors.New("x")
		},
	}
	r := newProviderRouter(svc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", fmt.Sprintf("/api/v1/providers/%s", uuid.New()),
		bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestToggleProviderStatus_Success(t *testing.T) {
	svc := &MockProviderService{
		toggleProviderStatusFn: func(id uuid.UUID, enabled bool) (*service.ProviderResponse, error) {
			return &service.ProviderResponse{ID: id, Enabled: enabled}, nil
		},
	}
	r := newProviderRouter(svc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PATCH", fmt.Sprintf("/api/v1/providers/%s/status", uuid.New()),
		bytes.NewBufferString(`{"enabled":true}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestToggleProviderStatus_InvalidID(t *testing.T) {
	r := newProviderRouter(&MockProviderService{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PATCH", "/api/v1/providers/bad/status", bytes.NewBufferString(`{"enabled":true}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestToggleProviderStatus_NotFound(t *testing.T) {
	svc := &MockProviderService{
		toggleProviderStatusFn: func(uuid.UUID, bool) (*service.ProviderResponse, error) {
			return nil, service.ErrProviderNotFound
		},
	}
	r := newProviderRouter(svc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PATCH", fmt.Sprintf("/api/v1/providers/%s/status", uuid.New()),
		bytes.NewBufferString(`{"enabled":true}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestToggleProviderStatus_InvalidBody(t *testing.T) {
	r := newProviderRouter(&MockProviderService{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PATCH", fmt.Sprintf("/api/v1/providers/%s/status", uuid.New()),
		bytes.NewBufferString(`bad`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestToggleProviderStatus_InternalError(t *testing.T) {
	svc := &MockProviderService{
		toggleProviderStatusFn: func(uuid.UUID, bool) (*service.ProviderResponse, error) { return nil, errors.New("x") },
	}
	r := newProviderRouter(svc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PATCH", fmt.Sprintf("/api/v1/providers/%s/status", uuid.New()),
		bytes.NewBufferString(`{"enabled":false}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestDeleteProvider_Success(t *testing.T) {
	svc := &MockProviderService{deleteProviderFn: func(uuid.UUID) error { return nil }}
	r := newProviderRouter(svc)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("DELETE", fmt.Sprintf("/api/v1/providers/%s", uuid.New()), nil))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestDeleteProvider_InvalidID(t *testing.T) {
	r := newProviderRouter(&MockProviderService{})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("DELETE", "/api/v1/providers/bad", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDeleteProvider_NotFound(t *testing.T) {
	svc := &MockProviderService{deleteProviderFn: func(uuid.UUID) error { return service.ErrProviderNotFound }}
	r := newProviderRouter(svc)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("DELETE", fmt.Sprintf("/api/v1/providers/%s", uuid.New()), nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestDeleteProvider_HasModels(t *testing.T) {
	svc := &MockProviderService{deleteProviderFn: func(uuid.UUID) error { return service.ErrProviderHasModels }}
	r := newProviderRouter(svc)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("DELETE", fmt.Sprintf("/api/v1/providers/%s", uuid.New()), nil))
	assert.Equal(t, http.StatusConflict, w.Code)
	var resp map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, CodeProviderHasModels, resp["error"].(map[string]interface{})["code"])
}

func TestDeleteProvider_NotFound_Code(t *testing.T) {
	svc := &MockProviderService{deleteProviderFn: func(uuid.UUID) error { return service.ErrProviderNotFound }}
	r := newProviderRouter(svc)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("DELETE", fmt.Sprintf("/api/v1/providers/%s", uuid.New()), nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
	var resp map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, CodeProviderNotFound, resp["error"].(map[string]interface{})["code"])
}

func TestDeleteProvider_InternalError(t *testing.T) {
	svc := &MockProviderService{deleteProviderFn: func(uuid.UUID) error { return errors.New("x") }}
	r := newProviderRouter(svc)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("DELETE", fmt.Sprintf("/api/v1/providers/%s", uuid.New()), nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
