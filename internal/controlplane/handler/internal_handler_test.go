package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeInternalSvc struct {
	listFn func() ([]service.AvailableModel, error)
}

func (f *fakeInternalSvc) ListAvailableModels() ([]service.AvailableModel, error) {
	if f.listFn != nil {
		return f.listFn()
	}
	return nil, nil
}

func (f *fakeInternalSvc) GetModelHealth(names []string) (map[string]service.ModelHealthInfo, error) {
	return nil, nil
}

var _ InternalServiceInterface = (*fakeInternalSvc)(nil)

func newInternalRouter(svc InternalServiceInterface) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewInternalHandler(svc, nil, nil)
	r.GET("/internal/v1/models/available", h.ListAvailableModels)
	return r
}

func TestInternalHandler_ListAvailableModels_Success(t *testing.T) {
	want := []service.AvailableModel{
		{
			ID:                  uuid.New(),
			Name:                "gpt-4",
			ModelType:           "chat",
			ProviderID:          uuid.New(),
			ProviderName:        "openai",
			ProviderType:        "openai",
			ProviderEndpoint:    "https://api.openai.com/v1",
			ProviderCredentials: "sk-test",
		},
	}
	svc := &fakeInternalSvc{listFn: func() ([]service.AvailableModel, error) { return want, nil }}

	r := newInternalRouter(svc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/internal/v1/models/available", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Data []service.AvailableModel `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "gpt-4", resp.Data[0].Name)
	assert.Equal(t, "sk-test", resp.Data[0].ProviderCredentials)
}

func TestInternalHandler_ListAvailableModels_Error(t *testing.T) {
	svc := &fakeInternalSvc{listFn: func() ([]service.AvailableModel, error) { return nil, errors.New("boom") }}
	r := newInternalRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/internal/v1/models/available", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "internal_error")
}
