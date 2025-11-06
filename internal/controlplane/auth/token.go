package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// UserClaims represents the JWT claims for authenticated users
type UserClaims struct {
	jwt.RegisteredClaims
	Email       string `json:"email"`
	Name        string `json:"name"`
	Passport    string `json:"passport"`
	Avatar      string `json:"avatar,omitempty"`
	CompanyName string `json:"company_name,omitempty"`
	DeptName    string `json:"dept_name,omitempty"`
}

// GenerateToken creates a signed JWT for the authenticated user
func GenerateToken(secret string, claims *UserClaims) (string, error) {
	if claims.ExpiresAt == nil {
		claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(24 * time.Hour))
	}
	if claims.IssuedAt == nil {
		claims.IssuedAt = jwt.NewNumericDate(time.Now())
	}
	claims.Issuer = "llm-gateway"

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ParseToken validates and parses a JWT token string
func ParseToken(secret, tokenStr string) (*UserClaims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &UserClaims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}

	claims, ok := token.Claims.(*UserClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	return claims, nil
}
