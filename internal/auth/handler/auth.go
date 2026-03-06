package handler

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"

	"github.com/example/llm-gateway/internal/auth/dto"
	"github.com/example/llm-gateway/internal/auth/repository"
	"github.com/example/llm-gateway/internal/auth/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

type AuthHandler struct {
	oidc  *service.OIDCService
	users *service.UserService
	token *service.TokenService
	rdb   *redis.Client
	dev   *repository.DevUserRepository
}

func NewAuthHandler(oidc *service.OIDCService, users *service.UserService, token *service.TokenService, rdb *redis.Client, dev *repository.DevUserRepository) *AuthHandler {
	return &AuthHandler{oidc: oidc, users: users, token: token, rdb: rdb, dev: dev}
}

func randomState() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (h *AuthHandler) Login(c *gin.Context) {
	if h.oidc == nil || h.rdb == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"type": "service_unavailable", "message": "oidc unavailable", "code": "service_unavailable"}})
		return
	}
	state := randomState()
	_ = h.rdb.Set(c.Request.Context(), "oidc:state:"+state, "1", 5*60*1000*1000*1000).Err()
	c.Redirect(http.StatusTemporaryRedirect, h.oidc.BuildLoginURL(state))
}

func (h *AuthHandler) Callback(c *gin.Context) {
	var req dto.OIDCCallbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request_error", "message": "invalid callback payload", "code": "invalid_request"}})
		return
	}
	if h.rdb == nil || h.oidc == nil || h.users == nil || h.token == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"type": "service_unavailable", "message": "auth dependencies unavailable", "code": "service_unavailable"}})
		return
	}
	exists, err := h.rdb.Exists(c.Request.Context(), "oidc:state:"+req.State).Result()
	if err != nil || exists == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "authentication_error", "message": "invalid oidc state", "code": "invalid_state"}})
		return
	}
	_, _ = h.rdb.Del(c.Request.Context(), "oidc:state:"+req.State).Result()
	ctx, cancel := service.NewContext()
	defer cancel()
	info, err := h.oidc.HandleCallback(ctx, req.Code)
	if err != nil {
		c.JSON(service.HTTPStatusForError(err), gin.H{"error": gin.H{"type": "authentication_error", "message": err.Error(), "code": "callback_failed"}})
		return
	}
	if info.Sub == "" || info.Name == "" || info.Email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "authentication_error", "message": "authentication info incomplete", "code": "invalid_claims"}})
		return
	}
	user, err := h.users.UpsertUserAndDept(info.Sub, info.Name, info.Email, info.Role, info.DepartmentID, info.DepartmentName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"type": "internal_error", "message": "failed to persist user", "code": "persist_failed"}})
		return
	}
	access, refresh, err := h.token.SignTokenPair(user.ID.String(), user.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"type": "internal_error", "message": "failed to sign token", "code": "token_sign_failed"}})
		return
	}
	c.JSON(http.StatusOK, dto.LoginResponse{
		AccessToken:  access,
		RefreshToken: refresh,
		User: dto.UserInfo{
			ID:           user.ID.String(),
			Name:         user.Name,
			Email:        user.Email,
			DepartmentID: info.DepartmentID,
			DepartmentName: info.DepartmentName,
		},
		Role: user.Role,
	})
}

func (h *AuthHandler) DevLogin(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request_error", "message": "invalid payload", "code": "invalid_request"}})
		return
	}
	if h.dev == nil {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"type": "authentication_error", "message": "dev login not available", "code": "forbidden"}})
		return
	}
	devUser, err := h.dev.FindByUsername(req.Username)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "invalid credentials"})
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(devUser.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "invalid credentials"})
		return
	}
	// Upsert a corresponding row in the users table so that downstream
	// handlers (e.g. GetMyQuota) can look up the user by JWT user_id.
	user, err := h.users.UpsertUserAndDept(devUser.Username, devUser.Username, devUser.Username+"@dev.local", devUser.Role, "", "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"type": "internal_error", "message": "failed to create user session", "code": "internal_error"}})
		return
	}
	access, refresh, err := h.token.SignTokenPair(user.ID.String(), user.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"type": "internal_error", "message": "sign failed", "code": "internal_error"}})
		return
	}
	c.JSON(http.StatusOK, dto.LoginResponse{AccessToken: access, RefreshToken: refresh, User: dto.UserInfo{ID: user.ID.String(), Name: devUser.Username}, Role: user.Role})
}
