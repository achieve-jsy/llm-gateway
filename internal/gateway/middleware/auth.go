package middleware

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/example/llm-gateway/internal/gateway/infra"
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	j "github.com/golang-jwt/jwt/v5"
)


// AuthMiddleware provides unified authentication (API key via auth service + JWT).
// Replaces the old APIKeyAuth middleware.
func AuthMiddleware(authClient *infra.AuthClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gwmodel.ErrUnauthorized})
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gwmodel.ErrUnauthorized})
			return
		}
		// API Key path
		if strings.HasPrefix(token, "sk-") {
			_, _, _ = j.NewParser().ParseUnverified(token, j.MapClaims{})
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
		var logEnabledPtr interface{} = nil
		if resp.Data.LogEnabled != nil {
			logEnabledPtr = *resp.Data.LogEnabled
		}
		log.Printf("[auth-debug] key=%s log_enabled_ptr=%v", resp.Data.KeyName, logEnabledPtr)
		logEnabled := resp.Data.LogEnabled == nil || *resp.Data.LogEnabled
		identity := &gwmodel.UserIdentity{
			UserID:          resp.Data.User.UserID,
			KeyID:           resp.Data.KeyID,
			DepartmentID:    resp.Data.User.DepartmentID,
			StudioID:        resp.Data.User.StudioID,
			AccessAllGroups: resp.Data.AccessAllGroups,
			LogEnabled:      logEnabled,
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
				_ = authStart // suppress unused warning if start_time not set
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
			uname, _ := claims["name"].(string)
			dname, _ := claims["dept_name"].(string)
			identity := &gwmodel.UserIdentity{UserID: uid, DepartmentID: did}
			c.Set("identity", identity)
			c.Set("user_id", uid)
			c.Set("user_name", uname)
			c.Set("dept_id", did)
			c.Set("dept_name", dname)
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gwmodel.ErrUnauthorized})
	}
}
