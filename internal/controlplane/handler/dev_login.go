package handler

import (
	"net/http"

	"github.com/example/llm-gateway/internal/controlplane/auth"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type DevLoginHandler struct {
	db        *gorm.DB
	jwtSecret string
}

func NewDevLoginHandler(db *gorm.DB, jwtSecret string) *DevLoginHandler {
	return &DevLoginHandler{db: db, jwtSecret: jwtSecret}
}

func (h *DevLoginHandler) Login(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request", "message": "invalid payload", "code": "invalid_request"}})
		return
	}

	var user struct {
		ID           string
		Username     string
		PasswordHash string
		Role         string
	}
	if err := h.db.Table("dev_users").Where("username = ?", req.Username).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	token, err := auth.GenerateAccessToken(h.jwtSecret, user.ID, "", "", user.Username+"@admin.local", user.Username, user.ID, user.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "token generation failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"accessToken":  token,
		"refreshToken": "",
		"user": gin.H{
			"id":   user.ID,
			"name": user.Username,
			"role": user.Role,
		},
	})
}
