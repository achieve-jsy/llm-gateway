package middleware

import (
	"net/http"
	"strings"

	"github.com/example/llm-gateway/internal/gateway/infra"
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	j "github.com/golang-jwt/jwt/v5"
	"github.com/gin-gonic/gin"
)

// AuthMiddleware provides unified authentication (API key via auth service + JWT).
// Accepts tokens from either Authorization: Bearer <token> or x-api-key: <token>
// headers. The x-api-key header is used by Anthropic-native clients (e.g. Claude Code)
// and is treated equivalently to a Bearer token.
func AuthMiddleware(authClient *infra.AuthClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			// Fall back to x-api-key header (Anthropic-native clients).
			auth = c.GetHeader("x-api-key")
			if auth == "" {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gwmodel.ErrUnauthorized})
				return
			}
		}
		token := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gwmodel.ErrUnauthorized})
			return
		}
		// API Key path
		if strings.HasPrefix(token, "sk-") {
			_, _, _ = j.NewParser().ParseUnverified(token, j.MapClaims{})
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
			c.Set("identity", &gwmodel.UserIdentity{
				UserID:          resp.Data.User.UserID,
				KeyID:           resp.Data.KeyID,
				DepartmentID:    resp.Data.User.DepartmentID,
				AccessAllGroups: resp.Data.AccessAllGroups,
			})
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
			c.Set("identity", &gwmodel.UserIdentity{UserID: uid, DepartmentID: did})
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gwmodel.ErrUnauthorized})
	}
}
