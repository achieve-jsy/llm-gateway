package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// fakeModelsCache 内存版 ModelsCacheInterface，实现命中/miss 切换。
type fakeModelsCache struct {
	mu        sync.Mutex
	stored    []model.ModelInfo
	getErr    error
	setErr    error
	getCalls  int
	setCalls  int
	returnNil bool
}

func (c *fakeModelsCache) GetModelList(_ context.Context) ([]model.ModelInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.getCalls++
	if c.getErr != nil {
		return nil, c.getErr
	}
	if c.returnNil {
		return nil, nil
	}
	return c.stored, nil
}

func (c *fakeModelsCache) SetModelList(_ context.Context, models []model.ModelInfo) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setCalls++
	if c.setErr != nil {
		return c.setErr
	}
	c.stored = models
	return nil
}

// fakeRegistry 内存版 ModelsRegistryInterface
type fakeRegistry struct {
	models []string
	calls  int
}

func (r *fakeRegistry) ListAllModels() []string {
	r.calls++
	return r.models
}

// TestModelsHandler_CacheHit 缓存命中：直接返回缓存数据，跳过 Registry。
func TestModelsHandler_CacheHit(t *testing.T) {
	cached := []model.ModelInfo{
		{ID: "gpt-3.5", Object: "model", Created: 1234567890, OwnedBy: "openai"},
		{ID: "gpt-4", Object: "model", Created: 1234567891, OwnedBy: "openai"},
	}
	cache := &fakeModelsCache{stored: cached}
	registry := &fakeRegistry{models: []string{"should-not-be-used"}}

	handler := NewModelsHandler(cache, registry)
	c, w := createTestContext()
	c.Request = httptest.NewRequest("GET", "/v1/models", nil)

	handler.Handle(c)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp model.ModelsResponse
	require := assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, require)
	assert.Equal(t, "list", resp.Object)
	assert.Equal(t, 2, len(resp.Data))
	assert.Equal(t, "gpt-3.5", resp.Data[0].ID)
	assert.Equal(t, "openai", resp.Data[0].OwnedBy)
	assert.Equal(t, 0, registry.calls, "registry should not be queried on cache hit")
	assert.Equal(t, 0, cache.setCalls, "no cache write on hit")
}

// TestModelsHandler_CacheMiss 缓存 miss：回退到 Registry 构造，并写入缓存。
func TestModelsHandler_CacheMiss(t *testing.T) {
	cases := []struct {
		name   string
		models []string
	}{
		{"three models", []string{"gpt-3.5", "gpt-4", "claude-3"}},
		{"single model", []string{"gpt-3.5"}},
		{"empty registry", []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cache := &fakeModelsCache{returnNil: true}
			registry := &fakeRegistry{models: tc.models}
			handler := NewModelsHandler(cache, registry)

			c, w := createTestContext()
			c.Request = httptest.NewRequest("GET", "/v1/models", nil)

			handler.Handle(c)

			assert.Equal(t, http.StatusOK, w.Code)
			var resp model.ModelsResponse
			assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			assert.Equal(t, "list", resp.Object)
			assert.Equal(t, len(tc.models), len(resp.Data))
			for i, m := range resp.Data {
				assert.Equal(t, tc.models[i], m.ID)
				assert.Equal(t, "model", m.Object)
				assert.Equal(t, "llm-gateway", m.OwnedBy)
				assert.Greater(t, m.Created, int64(0))
			}
			assert.Equal(t, 1, registry.calls, "registry should be queried exactly once on miss")
			assert.Equal(t, 1, cache.setCalls, "cache should be written on miss")
			assert.Equal(t, len(tc.models), len(cache.stored))
		})
	}
}

// TestModelsHandler_CacheGetError 缓存读取错误：降级到 Registry，且不阻塞响应。
func TestModelsHandler_CacheGetError(t *testing.T) {
	cache := &fakeModelsCache{getErr: errors.New("redis down")}
	registry := &fakeRegistry{models: []string{"fallback-model"}}

	handler := NewModelsHandler(cache, registry)
	c, w := createTestContext()
	c.Request = httptest.NewRequest("GET", "/v1/models", nil)

	handler.Handle(c)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp model.ModelsResponse
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 1, len(resp.Data))
	assert.Equal(t, "fallback-model", resp.Data[0].ID)
}

// TestModelsHandler_CacheSetError 缓存写入错误：仍然返回 Registry 数据。
func TestModelsHandler_CacheSetError(t *testing.T) {
	cache := &fakeModelsCache{returnNil: true, setErr: errors.New("redis down")}
	registry := &fakeRegistry{models: []string{"gpt-x"}}

	handler := NewModelsHandler(cache, registry)
	c, w := createTestContext()
	c.Request = httptest.NewRequest("GET", "/v1/models", nil)

	handler.Handle(c)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp model.ModelsResponse
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 1, len(resp.Data))
}

// TestModelsHandler_ResponseStructure OpenAI 兼容响应结构。
func TestModelsHandler_ResponseStructure(t *testing.T) {
	cache := &fakeModelsCache{returnNil: true}
	registry := &fakeRegistry{models: []string{"gpt-3.5-turbo", "gpt-4", "claude-3-opus"}}
	handler := NewModelsHandler(cache, registry)

	c, w := createTestContext()
	c.Request = httptest.NewRequest("GET", "/v1/models", nil)
	handler.Handle(c)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))

	var body map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "list", body["object"])
	data := body["data"].([]interface{})
	assert.Equal(t, 3, len(data))
	first := data[0].(map[string]interface{})
	assert.Equal(t, "gpt-3.5-turbo", first["id"])
	assert.Equal(t, "model", first["object"])
	assert.NotNil(t, first["created"])
	assert.Equal(t, "llm-gateway", first["owned_by"])
}

// TestModelsHandler_ModelIDOrdering 顺序保留与 Registry 一致。
func TestModelsHandler_ModelIDOrdering(t *testing.T) {
	order := []string{"model-z", "model-a", "model-m"}
	cache := &fakeModelsCache{returnNil: true}
	registry := &fakeRegistry{models: order}
	handler := NewModelsHandler(cache, registry)

	c, w := createTestContext()
	c.Request = httptest.NewRequest("GET", "/v1/models", nil)
	handler.Handle(c)

	var resp model.ModelsResponse
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	for i, m := range resp.Data {
		assert.Equal(t, order[i], m.ID)
	}
}

// TestModelsHandler_CreatedTimestamp 检查时间戳为当前秒。
func TestModelsHandler_CreatedTimestamp(t *testing.T) {
	cache := &fakeModelsCache{returnNil: true}
	registry := &fakeRegistry{models: []string{"test-model"}}
	handler := NewModelsHandler(cache, registry)

	c, w := createTestContext()
	c.Request = httptest.NewRequest("GET", "/v1/models", nil)

	before := time.Now().Unix()
	handler.Handle(c)
	after := time.Now().Unix()

	var resp model.ModelsResponse
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Greater(t, len(resp.Data), 0)
	created := resp.Data[0].Created
	assert.GreaterOrEqual(t, created, before)
	assert.LessOrEqual(t, created, after)
}

// TestModelsHandler_LargeModelList 大量模型不应崩溃。
func TestModelsHandler_LargeModelList(t *testing.T) {
	models := make([]string, 1000)
	for i := range models {
		models[i] = fmt.Sprintf("model-%d", i)
	}
	cache := &fakeModelsCache{returnNil: true}
	registry := &fakeRegistry{models: models}
	handler := NewModelsHandler(cache, registry)

	c, w := createTestContext()
	c.Request = httptest.NewRequest("GET", "/v1/models", nil)
	handler.Handle(c)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp model.ModelsResponse
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 1000, len(resp.Data))
}

// TestModelsHandler_ContextPropagation 请求 context 应被传递到下层 cache。
func TestModelsHandler_ContextPropagation(t *testing.T) {
	cache := &fakeModelsCache{returnNil: true}
	registry := &fakeRegistry{models: []string{"a"}}
	handler := NewModelsHandler(cache, registry)

	c, w := createTestContext()
	ctx := context.WithValue(context.Background(), ctxKey("test"), "v")
	c.Request = httptest.NewRequest("GET", "/v1/models", nil).WithContext(ctx)

	handler.Handle(c)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 1, cache.getCalls)
}

// ctxKey 用于 context.WithValue 的 key 类型，避免 staticcheck 警告。
type ctxKey string

// 实现接口断言，编译期校验。
var (
	_ ModelsCacheInterface    = (*fakeModelsCache)(nil)
	_ ModelsRegistryInterface = (*fakeRegistry)(nil)
)

// helper 与 chat_test.go 中的 createTestContext 共用，定义在 chat_test.go。
// 防止 gin warnings.
func init() {
	gin.SetMode(gin.TestMode)
}
