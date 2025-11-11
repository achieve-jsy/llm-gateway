package middleware

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RequestID 注入请求唯一标识（T004）
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if id == "" {
			id = uuid.New().String()
		}
		c.Set("request_id", id)
		c.Header("X-Request-ID", id)
		c.Next()
	}
}

// Logger 结构化日志中间件（T006）
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path

		c.Next()

		if path == "/health" || path == "/ready" {
			return
		}

		duration := time.Since(start)
		requestID, _ := c.Get("request_id")

		attrs := []any{
			"request_id", requestID,
			"method", c.Request.Method,
			"path", path,
			"status", c.Writer.Status(),
			"duration_ms", duration.Milliseconds(),
			"model", c.GetString("model"),
		}
		if gn := c.GetString("group_name"); gn != "" {
			attrs = append(attrs, "group_name", gn)
		}
		if rm := c.GetString("routed_model_id"); rm != "" {
			attrs = append(attrs, "routed_model_id", rm)
		}
		slog.Info("request", attrs...)
	}
}

// APIKeyAuth API Key 校验中间件（T005）
func APIKeyAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"type":    "authentication_error",
					"message": "missing or invalid Authorization header",
					"code":    "invalid_api_key",
				},
			})
			return
		}

		apiKey := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		if apiKey == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"type":    "authentication_error",
					"message": "empty API key",
					"code":    "invalid_api_key",
				},
			})
			return
		}

		// TODO: Redis Set 校验 Key 有效性（当前先放行非空 Key，后续对接 control-plane Key 管理）
		c.Set("api_key", apiKey)
		c.Next()
	}
}

// ErrorHandler 统一错误处理与 panic 恢复（T009）
func ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				requestID, _ := c.Get("request_id")
				slog.Error("panic recovered",
					"request_id", requestID,
					"error", r,
				)
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error": gin.H{
						"type":    "internal_error",
						"message": "internal server error",
						"code":    "internal",
					},
				})
			}
		}()
		c.Next()
	}
}
