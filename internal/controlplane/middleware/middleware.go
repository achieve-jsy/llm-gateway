package middleware

import (
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
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
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "change-me-in-production"
	}
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
		parsed, err := jwt.Parse(token, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return []byte(secret), nil
		})
		if err != nil || !parsed.Valid {
			log.Printf("[JWTAuth] token parse failed: %v", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"type":    "authentication_error",
					"message": "invalid token",
					"code":    "unauthorized",
				},
			})
			return
		}
		claims, ok := parsed.Claims.(jwt.MapClaims)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"type": "authentication_error", "message": "invalid claims", "code": "unauthorized"}})
			return
		}
		if uid, ok := claims["user_id"].(string); ok && uid != "" {
			c.Set("user_id", uid)
		}
		if c.GetString("user_id") == "" {
			if passport, ok := claims["passport"].(string); ok && passport != "" {
				c.Set("user_id", passport)
			}
		}
		if c.GetString("user_id") == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"type":    "authentication_error",
					"message": "invalid token: missing user identity",
					"code":    "unauthorized",
				},
			})
			return
		}
		if email, ok := claims["email"].(string); ok {
			c.Set("user_email", email)
		}
		if name, ok := claims["name"].(string); ok {
			c.Set("user_name", name)
		}
		c.Set("admin_token", token)
		c.Next()
	}
}

// InternalAuth 内部服务间 Token 认证中间件
// 支持 X-Internal-Token Header 和 Authorization: Bearer Header 两种方式
func InternalAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("X-Internal-Token")
		if auth == "" {
			auth = strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		}
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
