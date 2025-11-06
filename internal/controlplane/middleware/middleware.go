package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RequestID 为每个请求注入唯一 ID
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

// Logger 结构化日志中间件
func Logger() gin.HandlerFunc {
	return gin.LoggerWithConfig(gin.LoggerConfig{
		SkipPaths: []string{"/healthz"},
	})
}

// JWTAuth 管理员 JWT 认证中间件
func JWTAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"type":    "authentication_error",
					"message": "missing or invalid authorization header",
					"code":    "unauthorized",
				},
			})
			return
		}
		token := strings.TrimPrefix(auth, "Bearer ")
		// TODO: 实际 JWT 验证逻辑在后续版本完善（当前 SSO 未对接，先做 token 非空校验）
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"type":    "authentication_error",
					"message": "empty token",
					"code":    "unauthorized",
				},
			})
			return
		}
		c.Set("admin_token", token)
		c.Next()
	}
}

// InternalAuth 内部服务间 Token 认证中间件
func InternalAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("X-Internal-Token")
		if auth == "" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": gin.H{
					"type":    "authorization_error",
					"message": "missing internal token",
					"code":    "forbidden",
				},
			})
			return
		}
		// 内部 Token 校验：简单共享密钥（后续可改为更安全方案）
		c.Set("internal_token", auth)
		c.Next()
	}
}
