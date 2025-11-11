package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type UserClaims struct {
	jwt.RegisteredClaims
	UserID       string `json:"user_id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	Passport     string `json:"passport"`
	DeptID       string `json:"dept_id"`
	DeptName     string `json:"dept_name,omitempty"`
	Role         string `json:"role"`
	CompanyName  string `json:"company_name,omitempty"`
}

func GenerateAccessToken(secret, userID, deptID, deptName, email, name, passport, role string) (string, error) {
	now := time.Now()
	claims := &UserClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "llm-gateway",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(30 * time.Minute)),
		},
		UserID:   userID,
		DeptID:   deptID,
		DeptName: deptName,
		Email:    email,
		Name:     name,
		Passport: passport,
		Role:     role,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func GenerateRefreshToken(secret, userID, role string) (string, error) {
	now := time.Now()
	claims := &UserClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "llm-gateway",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(7 * 24 * time.Hour)),
		},
		UserID: userID,
		Role:   role,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

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