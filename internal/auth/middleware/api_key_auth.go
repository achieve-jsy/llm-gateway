package middleware

import (
	"net/http"
	"strings"

	"github.com/example/llm-gateway/internal/auth/service"
	"github.com/gin-gonic/gin"
)

func APIKeyAuth(keySvc *service.KeyAuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"type": "authentication_error", "message": "missing authorization header", "code": "unauthorized"}})
			return
		}
		key := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		if !strings.HasPrefix(key, "sk-") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"type": "authentication_error", "message": "invalid api key format", "code": "unauthorized"}})
			return
		}
		resp, err := keySvc.ResolveKey(c.Request.Context(), key)
		if err != nil || resp == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"type": "authentication_error", "message": "invalid api key", "code": "unauthorized"}})
			return
		}
		switch resp.Data.Status {
		case "active":
			if resp.Data.User != nil {
				c.Set("user_id", resp.Data.User.UserID)
				c.Set("department_id", resp.Data.User.DepartmentID)
			}
			c.Set("key_id", resp.Data.KeyID)
		default:
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"type": "authentication_error", "message": "invalid api key", "code": "unauthorized"}})
			return
		}
		c.Next()
	}
}
