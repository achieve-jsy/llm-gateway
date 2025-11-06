package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/example/llm-gateway/internal/gateway/provider"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// HealthHandler 健康检查 Handler
type HealthHandler struct {
	rdb      *redis.Client
	registry *provider.Registry
	state    *provider.RuntimeState
}

// NewHealthHandler 创建 HealthHandler
func NewHealthHandler(rdb *redis.Client, registry *provider.Registry, state *provider.RuntimeState) *HealthHandler {
	return &HealthHandler{rdb: rdb, registry: registry, state: state}
}

// Health GET /health 存活探针
func (h *HealthHandler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Ready GET /ready 就绪探针（检查 Redis 连通性）
func (h *HealthHandler) Ready(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	// 检查 Redis
	if err := h.rdb.Ping(ctx).Err(); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "not_ready",
			"checks": gin.H{
				"redis":    "unavailable",
				"registry": "unknown",
			},
		})
		return
	}

	modelCount := len(h.registry.ListAllModels())
	loaded, source, loadedAt, lastError, stateModelCount, version := false, "", time.Time{}, "", 0, int64(0)
	if h.state != nil {
		loaded, source, loadedAt, lastError, stateModelCount, version = h.state.Snapshot()
	}
	if modelCount == 0 || !loaded {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "not_ready",
			"checks": gin.H{
				"redis":    "ok",
				"registry": "empty",
			},
			"details": gin.H{
				"source":      source,
				"loaded_at":   loadedAt,
				"last_error":  lastError,
				"model_count": maxInt(modelCount, stateModelCount),
				"version":     version,
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ready",
		"checks": gin.H{
			"redis":    "ok",
			"registry": "ok",
		},
		"details": gin.H{
			"source":      source,
			"loaded_at":   loadedAt,
			"model_count": modelCount,
			"version":     version,
		},
	})
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
