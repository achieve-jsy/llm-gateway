package middleware

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/example/llm-gateway/internal/gateway/infra"
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	j "github.com/golang-jwt/jwt/v5"
)

// AuthMiddleware provides unified authentication (API key via auth service + JWT).
// 同时支持两种 Header 格式：
//   - OpenAI 兼容格式：Authorization: Bearer sk-xxx
//   - Anthropic 原生格式：x-api-key: sk-xxx（Claude Code 等客户端使用）
func AuthMiddleware(authClient *infra.AuthClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		var token string
		auth := c.GetHeader("Authorization")
		if strings.HasPrefix(auth, "Bearer ") {
			// OpenAI 兼容格式：Authorization: Bearer sk-xxx
			token = strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		} else {
			// Anthropic 原生格式：x-api-key: sk-xxx（Claude Code 使用）
			token = strings.TrimSpace(c.GetHeader("x-api-key"))
		}
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gwmodel.ErrUnauthorized})
			return
		}

		// API Key path
		if strings.HasPrefix(token, "sk-") {
			// 注意：此处不再做无意义的 JWT ParseUnverified（原第 32 行已删除）
			authStart := time.Now()
			resp, err := authClient.VerifyKey(c.Request.Context(), token)
			if err != nil {
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
					"error": gin.H{"type": "service_unavailable", "message": "auth service unavailable", "code": "auth_service_unavailable"},
				})
				return
			}
			if resp.Data.Status != "active" {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"error": gin.H{"type": "authentication_error", "message": "invalid or disabled API key", "code": "invalid_api_key"},
				})
				return
			}

			// log_enabled: nil（旧缓存无此字段）时默认 true，保持原有行为
			logEnabled := resp.Data.LogEnabled == nil || *resp.Data.LogEnabled
			slog.DebugContext(c.Request.Context(), "auth: key verified",
				"key_name", resp.Data.KeyName,
				"log_enabled", logEnabled,
			)

			identity := &gwmodel.UserIdentity{
				UserID:          resp.Data.User.UserID,
				KeyID:           resp.Data.KeyID,
				DepartmentID:    resp.Data.User.DepartmentID,
				StudioID:        resp.Data.User.StudioID,
				AccessAllGroups: resp.Data.AccessAllGroups,
				LogEnabled:      logEnabled,
				// v0.7: 从 verify-key 快照直接下发，无需网关再查库
				UsableGroups: resp.Data.UsableGroups,
				KeyType:      resp.Data.KeyType,
				BudgetChain:  resp.Data.BudgetChain,
			}
			c.Set("identity", identity)
			c.Set("key_id", identity.KeyID)
			c.Set("key_name", resp.Data.KeyName)
			c.Set("log_enabled", logEnabled)
			c.Set("user_id", identity.UserID)
			c.Set("user_name", resp.Data.User.Name)
			c.Set("dept_id", identity.DepartmentID)
			c.Set("dept_name", resp.Data.User.DeptName)
			c.Set("studio_id", identity.StudioID)
			c.Set("studio_name", resp.Data.User.StudioName)

			// 阶段计时：auth 完成时间（相对于请求抵达）
			if startTime, ok := c.Get("start_time"); ok {
				if t, ok := startTime.(time.Time); ok {
					ms := int16(time.Since(t).Milliseconds())
					c.Set("t_auth_ms", ms)
				}
			} else {
				ms := int16(time.Since(authStart).Milliseconds())
				c.Set("t_auth_ms", ms)
			}
			c.Next()
			return
		}

		// JWT path for employee self-service (e.g. /v1/me/models)
		if strings.HasPrefix(token, "eyJ") {
			parser := j.NewParser()
			parsed, _, err := parser.ParseUnverified(token, j.MapClaims{})
			if err != nil {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"error": gin.H{"type": "authentication_error", "message": "invalid JWT", "code": "invalid_token"},
				})
				return
			}
			claims, _ := parsed.Claims.(j.MapClaims)
			uid, _ := claims["user_id"].(string)
			did, _ := claims["dept_id"].(string)
			sid, _ := claims["studio_id"].(string)
			uname, _ := claims["name"].(string)
			dname, _ := claims["dept_name"].(string)
			sname, _ := claims["studio_name"].(string)
			identity := &gwmodel.UserIdentity{UserID: uid, DepartmentID: did, StudioID: sid}
			c.Set("identity", identity)
			c.Set("user_id", uid)
			c.Set("user_name", uname)
			c.Set("dept_id", did)
			c.Set("dept_name", dname)
			c.Set("studio_id", sid)
			c.Set("studio_name", sname)
			c.Next()
			return
		}

		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gwmodel.ErrUnauthorized})
	}
}
