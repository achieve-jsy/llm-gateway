package middleware

import (
	"crypto/subtle"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

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

// ─── 密钥 / 内部 Token 解析（fail-closed）───────────────────────────────────
//
// JWT_SECRET 与内部服务间共享 Token 都不允许回退到已知弱默认值：任何攻击者只要
// 知道这些公开的默认字符串就能伪造 admin JWT 或内部请求。解析策略（fail-closed
// per request）：
//   - 仅从环境变量读取（JWT_SECRET / INTERNAL_TOKEN / INTERNAL_AUTH_TOKEN），
//     不复用 viper 默认值、不落到代码里的已知默认字符串；
//   - 未配置或命中已知弱默认值时返回 ""，所有调用方一律拒绝请求（JWTAuth→401、
//     InternalAuth→403、WS→4401），并打出一条启动告警；
//   - 选择 fail-closed-per-request 而非启动即 panic 的原因：deploy/blue-green
//     的 docker-compose.{blue,green}.yml 在 control-plane 上用
//     `${JWT_SECRET:?JWT_SECRET required}` 强制要求密钥（部署层已 fail-fast），
//     但 config/dev.yaml、config/prod.yaml 仍写着已知默认值、config/test.yaml
//     留空依赖 CI 注入——若启动即 panic，本地 dev/test 会被直接打挂。per-request
//     拒绝只影响受保护路由，healthz / 内部路由保持可用，运维按告警修正即可。

const (
	jwtSecretDefaultChangeMe = "change-me-in-production"
	jwtSecretDefaultDev      = "CHANGE_ME_JWT_SECRET"

	internalTokenDefaultDev = "CHANGE_ME_INTERNAL_TOKEN"
	internalTokenDefaultSh  = "internal-shared-secret"
)

var (
	warnJWTSecretOnce     sync.Once
	warnInternalTokenOnce sync.Once
)

// ResolveJWTSecret 返回用于校验/签发 admin JWT 的密钥，仅从 env JWT_SECRET 读取。
// 值为空或命中已知弱默认值时返回 ""（调用方必须 fail-closed）。
func ResolveJWTSecret() string {
	secret := strings.TrimSpace(os.Getenv("JWT_SECRET"))
	if secret == "" || isKnownJWTSecretDefault(secret) {
		warnJWTSecretOnce.Do(func() {
			log.Printf("[middleware] WARNING: JWT_SECRET is unset or set to a known default value; admin JWT authentication is DISABLED (fail-closed). Set a strong random JWT_SECRET before serving admin traffic.")
		})
		return ""
	}
	return secret
}

func isKnownJWTSecretDefault(s string) bool {
	switch s {
	case jwtSecretDefaultChangeMe, jwtSecretDefaultDev:
		return true
	}
	return false
}

// ResolveInternalToken 返回本服务用于校验内部服务间请求的共享 Token。
// 解析顺序：env INTERNAL_TOKEN → env INTERNAL_AUTH_TOKEN（历史/部署使用的变量名）。
// 均未配置或全部命中已知弱默认值时返回 ""（调用方必须 fail-closed）。
func ResolveInternalToken() string {
	for _, name := range []string{"INTERNAL_TOKEN", "INTERNAL_AUTH_TOKEN"} {
		v := strings.TrimSpace(os.Getenv(name))
		if v == "" || isKnownInternalTokenDefault(v) {
			continue
		}
		return v
	}
	warnInternalTokenOnce.Do(func() {
		log.Printf("[middleware] WARNING: internal service token (INTERNAL_TOKEN / INTERNAL_AUTH_TOKEN) is unset or set to a known default value; all /internal/v1/* routes are DISABLED (fail-closed 403). Set a strong random token before serving gateway traffic.")
	})
	return ""
}

func isKnownInternalTokenDefault(s string) bool {
	switch s {
	case internalTokenDefaultDev, internalTokenDefaultSh:
		return true
	}
	return false
}

// JWTAuth 管理员 JWT 认证中间件
func JWTAuth() gin.HandlerFunc {
	secret := ResolveJWTSecret()
	return func(c *gin.Context) {
		// fail-closed：JWT_SECRET 未配置或为已知默认值时，拒绝所有请求。
		if secret == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"type":    "authentication_error",
					"message": "JWT authentication is disabled: JWT_SECRET is not configured",
					"code":    "unauthorized",
				},
			})
			return
		}
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
		// user_id 是 users.id（UUID），passport 是 EHR 账号，两者不能互相顶替：
		// 角色表按 user_id 存，工作室负责人按 passport 存。
		if passport, ok := claims["passport"].(string); ok && passport != "" {
			c.Set("user_passport", passport)
		}
		if email, ok := claims["email"].(string); ok {
			c.Set("user_email", email)
		}
		if name, ok := claims["name"].(string); ok {
			c.Set("user_name", name)
		}
		if role, ok := claims["role"].(string); ok {
			c.Set("user_role", role)
		}
		if deptID, ok := claims["dept_id"].(string); ok {
			c.Set("user_dept_id", deptID) // 保留旧 key，兼容 budget_handler 等已有读取
			c.Set("dept_id", deptID)      // 供 AuditMiddleware 直接读取
		}
		if deptName, ok := claims["dept_name"].(string); ok {
			c.Set("dept_name", deptName) // 供 AuditMiddleware 写入审计日志
		}
		if studioID, ok := claims["studio_id"].(string); ok {
			c.Set("user_studio_id", studioID)
		}
		c.Set("admin_token", token)
		c.Next()
	}
}

// InternalAuth 内部服务间 Token 认证中间件
// 支持 X-Internal-Token Header 和 Authorization: Bearer Header 两种方式。
// 使用 crypto/subtle 常量时间比较校验展示的 token 与解析出的内部共享 Token，
// 未配置/为已知默认值/不匹配时一律 403（fail-closed）。
func InternalAuth() gin.HandlerFunc {
	expected := ResolveInternalToken()
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
		if expected == "" || subtle.ConstantTimeCompare([]byte(auth), []byte(expected)) != 1 {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": gin.H{
					"type":    "authorization_error",
					"message": "invalid internal token",
					"code":    "forbidden",
				},
			})
			return
		}
		// 内部 Token 校验通过后透传到 context（供 handler / 审计使用）
		c.Set("internal_token", auth)
		c.Next()
	}
}
