package handler

import (
	"net/http"

	"github.com/example/llm-gateway/internal/auth/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

type TokenHandler struct {
	token *service.TokenService
	rdb   *redis.Client
}

func NewTokenHandler(token *service.TokenService, rdb *redis.Client) *TokenHandler {
	return &TokenHandler{token: token, rdb: rdb}
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *TokenHandler) Refresh(c *gin.Context) {
	var req refreshRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.RefreshToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request_error", "message": "missing refresh_token", "code": "invalid_request"}})
		return
	}
	access, refresh, err := h.token.RefreshToken(c.Request.Context(), req.RefreshToken, h.rdb)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"type": "authentication_error", "message": err.Error(), "code": "unauthorized"}})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"accessToken":  access,
		"refreshToken": refresh,
	})
}

func (h *TokenHandler) Logout(c *gin.Context) {
	var req refreshRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.RefreshToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request_error", "message": "missing refresh_token", "code": "invalid_request"}})
		return
	}
	cnf, err := h.token.VerifyAccessToken(req.RefreshToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"type": "authentication_error", "message": "invalid token", "code": "unauthorized"}})
		return
	}
	jti, ok := cnf["jti"].(string)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"type": "authentication_error", "message": "invalid token", "code": "unauthorized"}})
		return
	}
	if err := h.rdb.SAdd(c.Request.Context(), "token:blacklist", jti).Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"type": "internal_error", "message": "failed to revoke token", "code": "internal_error"}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": nil})
}
