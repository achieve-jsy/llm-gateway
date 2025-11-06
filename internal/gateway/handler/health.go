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
}

// NewHealthHandler 创建 HealthHandler
func NewHealthHandler(rdb *redis.Client, registry *provider.Registry) *HealthHandler {
	return &HealthHandler{rdb: rdb, registry: registry}
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
				"redis": "unavailable",
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ready",
		"checks": gin.H{
			"redis": "ok",
		},
	})
}
