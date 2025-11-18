package middleware

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RequestID 注入请求唯一标识（T004）
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 请求到达时间：作为所有阶段耗时的统一基准点
		c.Set("start_time", time.Now())

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
// 在请求完成后记录完整的请求摘要，包含身份信息、模型、provider、耗时等。
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

		// 身份信息（认证后可用）
		if identity, ok := c.Get("identity"); ok {
			if id, ok := identity.(*model.UserIdentity); ok {
				if id.UserID != "" {
					attrs = append(attrs, "user_id", id.UserID)
				}
				if id.KeyID != "" {
					attrs = append(attrs, "key_id", id.KeyID)
				}
				if id.DepartmentID != "" {
					attrs = append(attrs, "dept_id", id.DepartmentID)
				}
			}
		}

		// 分组路由信息（GroupAwareDispatcher 填充）
		if gn := c.GetString("group_name"); gn != "" {
			attrs = append(attrs, "group_name", gn)
		}
		if rm := c.GetString("routed_model_id"); rm != "" {
			attrs = append(attrs, "routed_model_id", rm)
		}

		// 上游 provider 名称（orchestrator 填充）
		if prov := c.GetString("provider"); prov != "" {
			attrs = append(attrs, "provider", prov)
		}

		// 上游状态码（orchestrator 填充，用于区分网关错误与上游错误）
		if upstreamStatus, exists := c.Get("upstream_status"); exists {
			attrs = append(attrs, "upstream_status", upstreamStatus)
		}

		// Token 用量（非流式请求由 orchestrator 填充）
		if pt, exists := c.Get("prompt_tokens"); exists {
			attrs = append(attrs, "prompt_tokens", pt)
		}
		if ct, exists := c.Get("completion_tokens"); exists {
			attrs = append(attrs, "completion_tokens", ct)
		}

		// 是否为流式请求
		if stream, exists := c.Get("stream"); exists {
			attrs = append(attrs, "stream", stream)
		}

		// failover 标记
		if fv, exists := c.Get("failover_used"); exists {
			attrs = append(attrs, "failover_used", fv)
		}

		slog.Info("request", attrs...)
	}
}

// RequestBodyLogger 在 DEBUG 级别记录请求体原始内容。
// 仅在 LOG_LEVEL=debug 时有实际输出，生产环境 INFO 级别下该中间件为无操作。
// 注意：此中间件会将请求体读入内存后重置，对大 payload 有一定内存压力。
func RequestBodyLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !slog.Default().Enabled(c.Request.Context(), slog.LevelDebug) {
			c.Next()
			return
		}

		// 仅对有 body 的方法记录
		if c.Request.Method != http.MethodPost && c.Request.Method != http.MethodPut && c.Request.Method != http.MethodPatch {
			c.Next()
			return
		}

		// 跳过健康检查
		path := c.Request.URL.Path
		if path == "/health" || path == "/ready" {
			c.Next()
			return
		}

		// 读取并重置请求体（让下游 handler 仍能读取）
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, 64*1024)) // 最多 64KB
		if err == nil {
			c.Request.Body = io.NopCloser(bytes.NewBuffer(body))
			requestID, _ := c.Get("request_id")
			slog.Debug("request_body",
				"request_id", requestID,
				"path", path,
				"body", string(body),
			)
		}

		c.Next()
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
