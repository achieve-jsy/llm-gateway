package middleware

import (
	"net/http"
	"strings"
	"time"

	acrypto "github.com/example/llm-gateway/internal/auth/crypto"
	"github.com/gin-gonic/gin"
	jwt "github.com/golang-jwt/jwt/v5"
)

func JWTAuth(manager *acrypto.JWTManager) gin.HandlerFunc {
	parser := jwt.NewParser(jwt.WithValidMethods([]string{"RS256"}), jwt.WithLeeway(5*time.Second))
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"type": "authentication_error", "message": "missing authorization header", "code": "unauthorized"}})
			return
		}
		tokenStr := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		token, err := parser.ParseWithClaims(tokenStr, jwt.MapClaims{}, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return manager.PublicKey(), nil
		})
		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"type": "authentication_error", "message": "invalid jwt", "code": "unauthorized"}})
			return
		}
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"type": "authentication_error", "message": "invalid jwt claims", "code": "unauthorized"}})
			return
		}
		if exp, ok := claims["exp"].(float64); ok {
			if time.Now().Unix() > int64(exp) {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"type": "authentication_error", "message": "token expired", "code": "unauthorized"}})
				return
			}
		}
		if userID, ok := claims["user_id"].(string); ok {
			c.Set("user_id", userID)
		}
		if role, ok := claims["role"].(string); ok {
			c.Set("role", role)
		}
		if departmentID, ok := claims["department_id"].(string); ok {
			c.Set("department_id", departmentID)
		}
		c.Next()
	}
}
