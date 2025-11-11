package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	acrypto "github.com/example/llm-gateway/internal/auth/crypto"
	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type TokenService struct {
	manager *acrypto.JWTManager
}

func NewTokenService(manager *acrypto.JWTManager) *TokenService {
	return &TokenService{manager: manager}
}

func (s *TokenService) SignTokenPair(userID, role string) (string, string, error) {
	now := time.Now()
	accessClaims := jwt.MapClaims{
		"user_id": userID,
		"role":    role,
		"iat":     now.Unix(),
		"exp":     now.Add(24 * time.Hour).Unix(),
		"jti":     uuid.NewString(),
	}
	refreshClaims := jwt.MapClaims{
		"user_id": userID,
		"role":    role,
		"iat":     now.Unix(),
		"exp":     now.Add(7 * 24 * time.Hour).Unix(),
		"jti":     uuid.NewString(),
		"typ":     "refresh",
	}
	access := jwt.NewWithClaims(s.manager.SigningMethod(), accessClaims)
	access.Header["kid"] = s.manager.KeyID()
	refresh := jwt.NewWithClaims(s.manager.SigningMethod(), refreshClaims)
	refresh.Header["kid"] = s.manager.KeyID()
	accessToken, err := access.SignedString(s.manager.PrivateKey())
	if err != nil {
		return "", "", err
	}
	refreshToken, err := refresh.SignedString(s.manager.PrivateKey())
	if err != nil {
		return "", "", err
	}
	return accessToken, refreshToken, nil
}

func (s *TokenService) VerifyAccessToken(tokenStr string) (jwt.MapClaims, error) {
	parser := jwt.NewParser(jwt.WithValidMethods([]string{"RS256"}), jwt.WithLeeway(5*time.Second))
	token, err := parser.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.manager.PublicKey(), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token claims")
	}
	if exp, ok := claims["exp"].(float64); ok {
		if time.Now().Unix() > int64(exp) {
			return nil, errors.New("token expired")
		}
	}
	return claims, nil
}

func (s *TokenService) RefreshToken(ctx context.Context, tokenStr string, rdb *redis.Client) (string, string, error) {
	parser := jwt.NewParser(jwt.WithValidMethods([]string{"RS256"}), jwt.WithLeeway(5*time.Second))
	token, err := parser.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.manager.PublicKey(), nil
	})
	if err != nil {
		return "", "", fmt.Errorf("invalid refresh token: %w", err)
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return "", "", errors.New("invalid refresh token claims")
	}
	if typ, ok := claims["typ"].(string); !ok || typ != "refresh" {
		return "", "", errors.New("token is not a refresh token")
	}
	jti, ok := claims["jti"].(string)
	if !ok {
		return "", "", errors.New("missing jti in refresh token")
	}
	blacklisted, err := rdb.SIsMember(ctx, "token:blacklist", jti).Result()
	if err != nil {
		return "", "", fmt.Errorf("failed to check blacklist: %w", err)
	}
	if blacklisted {
		return "", "", errors.New("refresh token has been revoked")
	}
	userID, _ := claims["user_id"].(string)
	role, _ := claims["role"].(string)
	if userID == "" {
		return "", "", errors.New("missing user_id in refresh token")
	}
	newAccess, newRefresh, err := s.SignTokenPair(userID, role)
	if err != nil {
		return "", "", fmt.Errorf("failed to sign new token pair: %w", err)
	}
	if err := rdb.SAdd(ctx, "token:blacklist", jti).Err(); err != nil {
		return "", "", fmt.Errorf("failed to blacklist old refresh token: %w", err)
	}
	return newAccess, newRefresh, nil
}
