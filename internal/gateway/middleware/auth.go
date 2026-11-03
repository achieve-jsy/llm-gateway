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
				// Inject individual identity fields for downstream middleware
				c.Set(string(KeyIDKey), resp.Data.KeyID)
				c.Set(string(UserIDKey), resp.Data.User.UserID)
				deptID := resp.Data.User.DepartmentID
				c.Set(string(DeptIDKey), deptID)
				if deptID == "" {
					infra.UsageMetricsInstance.IdentityMissing.Inc()
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
				c.Set("identity", &gwmodel.UserIdentity{UserID: uid, DepartmentID: did})
				// Inject individual identity fields for downstream middleware
				c.Set(string(UserIDKey), uid)
				c.Set(string(DeptIDKey), did)
				if did == "" {
					infra.UsageMetricsInstance.IdentityMissing.Inc()
				}
				c.Next()
				return
			}
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gwmodel.ErrUnauthorized})
		}
}
