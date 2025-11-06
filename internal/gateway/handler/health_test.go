package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/gateway/provider"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newHealthRouter(rdb *redis.Client, reg *provider.Registry) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewHealthHandler(rdb, reg)
	r.GET("/health", h.Health)
	r.GET("/ready", h.Ready)
	return r
}

func TestHealthHandler_Health(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	r := newHealthRouter(rdb, provider.NewRegistry())

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/health", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"status":"ok"`)
}

func TestHealthHandler_Ready_OK(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	r := newHealthRouter(rdb, provider.NewRegistry())

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ready", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"redis":"ok"`)
}

func TestHealthHandler_Ready_RedisDown(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	addr := mr.Addr()
	mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close()

	r := newHealthRouter(rdb, provider.NewRegistry())

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ready", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), `"redis":"unavailable"`)
}
