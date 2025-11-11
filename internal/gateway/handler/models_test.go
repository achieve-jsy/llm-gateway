package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/provider"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// fakeModelsCache is an in-memory ModelsCacheInterface implementation.
type fakeModelsCache struct {
	mu        sync.Mutex
	stored    []core.Model
	version   int64
	getErr    error
	setErr    error
	getCalls  int
	setCalls  int
	returnNil bool
}

func (c *fakeModelsCache) GetModelList(_ context.Context) ([]core.Model, error) {
	models, _, err := c.GetModelListWithVersion(context.Background())
	return models, err
}

func (c *fakeModelsCache) GetModelListWithVersion(_ context.Context) ([]core.Model, int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.getCalls++
	if c.getErr != nil {
		return nil, 0, c.getErr
	}
	if c.returnNil {
		return nil, 0, nil
	}
	return c.stored, c.version, nil
}

func (c *fakeModelsCache) SetModelList(_ context.Context, models []core.Model) error {
	return c.SetModelListWithVersion(context.Background(), models, 0)
}

func (c *fakeModelsCache) SetModelListWithVersion(_ context.Context, models []core.Model, version int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setCalls++
	if c.setErr != nil {
		return c.setErr
	}
	c.stored = models
	c.version = version
	return nil
}

// fakeRouter is an in-memory ModelsRouterInterface implementation.
type fakeRouter struct {
	models []core.Model
	err    error
	calls  int
}

func (r *fakeRouter) ListModels(_ context.Context) (*core.ModelsResponse, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	return &core.ModelsResponse{Object: "list", Data: r.models}, nil
}

func newFakeRouter(ids ...string) *fakeRouter {
	models := make([]core.Model, len(ids))
	for i, id := range ids {
		models[i] = core.Model{ID: id, Object: "model", OwnedBy: "llm-gateway", Created: 1234567890}
	}
	return &fakeRouter{models: models}
}

// TestModelsHandler_Handle_Success tests that Handle returns models from the router.
func TestModelsHandler_Handle_Success(t *testing.T) {
	router := newFakeRouter("gpt-3.5", "gpt-4", "claude-3")
	handler := NewModelsHandler(&fakeModelsCache{}, router, nil)

	c, w := createTestContext()
	c.Request = httptest.NewRequest("GET", "/v1/models", nil)

	handler.Handle(c)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp core.ModelsResponse
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "list", resp.Object)
	assert.Equal(t, 3, len(resp.Data))
	assert.Equal(t, 1, router.calls)
}

// TestModelsHandler_Handle_EmptyRouter returns empty list when router is nil.
func TestModelsHandler_Handle_NilRouter(t *testing.T) {
	handler := NewModelsHandler(&fakeModelsCache{}, nil, nil)

	c, w := createTestContext()
	c.Request = httptest.NewRequest("GET", "/v1/models", nil)

	handler.Handle(c)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp core.ModelsResponse
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "list", resp.Object)
	assert.Equal(t, 0, len(resp.Data))
}

// TestModelsHandler_HandleLegacy_CacheHit tests HandleLegacy cache hit path.
func TestModelsHandler_HandleLegacy_CacheHit(t *testing.T) {
	cached := []core.Model{
		{ID: "gpt-3.5", Object: "model", Created: 1234567890, OwnedBy: "openai"},
		{ID: "gpt-4", Object: "model", Created: 1234567891, OwnedBy: "openai"},
	}
	cache := &fakeModelsCache{stored: cached}
	router := newFakeRouter("should-not-be-used")

	handler := NewModelsHandler(cache, router, nil)
	c, w := createTestContext()
	c.Request = httptest.NewRequest("GET", "/v1/health/models", nil)

	handler.HandleLegacy(c)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp core.ModelsResponse
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "list", resp.Object)
	assert.Equal(t, 2, len(resp.Data))
	assert.Equal(t, "gpt-3.5", resp.Data[0].ID)
	// Router should NOT be queried on cache hit.
	assert.Equal(t, 0, router.calls)
}

// TestModelsHandler_HandleLegacy_CacheMiss tests HandleLegacy falls back to router.
func TestModelsHandler_HandleLegacy_CacheMiss(t *testing.T) {
	cache := &fakeModelsCache{returnNil: true}
	router := newFakeRouter("gpt-3.5", "gpt-4", "claude-3")

	handler := NewModelsHandler(cache, router, nil)
	c, w := createTestContext()
	c.Request = httptest.NewRequest("GET", "/v1/health/models", nil)

	handler.HandleLegacy(c)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp core.ModelsResponse
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "list", resp.Object)
	assert.Equal(t, 3, len(resp.Data))
	assert.Equal(t, 1, router.calls)
}

// TestModelsHandler_StaleVersionFallsBackToRouter tests version-based cache invalidation.
func TestModelsHandler_StaleVersionFallsBackToRouter(t *testing.T) {
	cache := &fakeModelsCache{stored: []core.Model{{ID: "stale-model"}}, version: 1}
	router := newFakeRouter("fresh-model")
	state := &provider.RuntimeState{}
	state.MarkLoadSuccess("redis-snapshot", 1, 2)

	handler := NewModelsHandler(cache, router, state)
	c, w := createTestContext()
	c.Request = httptest.NewRequest("GET", "/v1/health/models", nil)

	handler.HandleLegacy(c)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp core.ModelsResponse
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 1, len(resp.Data))
	assert.Equal(t, "fresh-model", resp.Data[0].ID)
}

// Compile-time interface checks.
var (
	_ ModelsCacheInterface  = (*fakeModelsCache)(nil)
	_ ModelsRouterInterface = (*fakeRouter)(nil)
)

// init sets gin to test mode.
func init() {
	gin.SetMode(gin.TestMode)
}

// ctxKey is used as context.WithValue key.
type ctxKey string
