package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDefinitionRepo 实现 DefinitionRepoInterface
type fakeDefinitionRepo struct {
	listFn    func() ([]model.ProviderDefinition, error)
	getByIDFn func(id string) (*model.ProviderDefinition, error)
}

func (f *fakeDefinitionRepo) List() ([]model.ProviderDefinition, error) {
	if f.listFn != nil {
		return f.listFn()
	}
	return nil, nil
}
func (f *fakeDefinitionRepo) GetByID(id string) (*model.ProviderDefinition, error) {
	if f.getByIDFn != nil {
		return f.getByIDFn(id)
	}
	return nil, nil
}

var _ DefinitionRepoInterface = (*fakeDefinitionRepo)(nil)

func newDefinitionRouter(repo DefinitionRepoInterface) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewDefinitionHandler(repo)
	r.GET("/api/v1/provider-definitions", h.ListDefinitions)
	r.GET("/api/v1/provider-definitions/:id", h.GetDefinition)
	return r
}

func TestDefinitionHandler_ListDefinitions_Success(t *testing.T) {
	defs := []model.ProviderDefinition{
		{ID: "openai", DisplayName: "OpenAI", Adapter: "openai"},
		{ID: "anthropic", DisplayName: "Anthropic", Adapter: "anthropic"},
	}
	repo := &fakeDefinitionRepo{listFn: func() ([]model.ProviderDefinition, error) { return defs, nil }}
	r := newDefinitionRouter(repo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/provider-definitions", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Data []model.ProviderDefinition `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp.Data, 2)
	assert.Equal(t, "openai", resp.Data[0].ID)
}

func TestDefinitionHandler_ListDefinitions_Error(t *testing.T) {
	repo := &fakeDefinitionRepo{listFn: func() ([]model.ProviderDefinition, error) { return nil, errors.New("db down") }}
	r := newDefinitionRouter(repo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/provider-definitions", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "internal_error")
}

func TestDefinitionHandler_GetDefinition_Success(t *testing.T) {
	def := &model.ProviderDefinition{ID: "openai", DisplayName: "OpenAI", Adapter: "openai"}
	repo := &fakeDefinitionRepo{getByIDFn: func(id string) (*model.ProviderDefinition, error) {
		assert.Equal(t, "openai", id)
		return def, nil
	}}
	r := newDefinitionRouter(repo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/provider-definitions/openai", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "OpenAI")
}

func TestDefinitionHandler_GetDefinition_NotFound(t *testing.T) {
	repo := &fakeDefinitionRepo{getByIDFn: func(id string) (*model.ProviderDefinition, error) {
		return nil, errors.New("not found")
	}}
	r := newDefinitionRouter(repo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/provider-definitions/missing", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "not_found")
}
