package handler

import (
	"net/http"
	"os"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/auth"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type DevLoginHandler struct {
	db             *gorm.DB
	jwtSecret      string
	accessTokenTTL time.Duration
}

func NewDevLoginHandler(db *gorm.DB, jwtSecret string, accessTokenTTL time.Duration) *DevLoginHandler {
	return &DevLoginHandler{db: db, jwtSecret: jwtSecret, accessTokenTTL: accessTokenTTL}
}

// DevLoginEnabled reports whether the pre-auth dev-login backdoor is allowed.
// It is an explicit opt-in that defaults OFF everywhere: the route is only
// registered (and the handler only proceeds) when DEV_LOGIN_ENABLED == "true".
// In production (APP_ENV=prod) a second gate DEV_LOGIN_PROD_ALLOWED == "true"
// is required to prevent accidental enablement — this path bypasses OIDC
// (MFA / offboarding / audit), so it must be enabled deliberately and removed
// once the SSO bootstrap is complete.
func DevLoginEnabled() bool {
	if os.Getenv("DEV_LOGIN_ENABLED") != "true" {
		return false
	}
	if os.Getenv("APP_ENV") == "prod" {
		return os.Getenv("DEV_LOGIN_PROD_ALLOWED") == "true"
	}
	return true
}

func (h *DevLoginHandler) Login(c *gin.Context) {
	// Defense in depth: even if the route were reachable, refuse when disabled.
	if !DevLoginEnabled() {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}

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

	var profile struct {
		Email      string `gorm:"column:email"`
		Name       string `gorm:"column:name"`
		DeptID     string `gorm:"column:dept_id"`
		DeptName   string `gorm:"column:dept_name"`
		StudioID   string `gorm:"column:studio_id"`
		StudioName string `gorm:"column:studio_name"`
	}
	_ = h.db.Raw(`
		SELECT
			COALESCE(u.email, '') AS email,
			COALESCE(u.name, '') AS name,
			COALESCE(d.dept_id, '') AS dept_id,
			COALESCE(d.dept_name, '') AS dept_name,
			COALESCE(dsm.studio_id::text, d.studio_id::text, '') AS studio_id,
			COALESCE(s.name, '') AS studio_name
		FROM users u
		LEFT JOIN departments d ON d.id = u.dept_id
		LEFT JOIN dept_studio_map dsm ON dsm.dept_id = d.dept_id
		LEFT JOIN studios s ON s.id = COALESCE(dsm.studio_id, d.studio_id)
		WHERE u.id::text = ?
		LIMIT 1
	`, user.ID).Scan(&profile).Error

	email := profile.Email
	if email == "" {
		email = user.Username + "@admin.local"
	}
	name := profile.Name
	if name == "" {
		name = user.Username
	}

	accessToken, err := auth.GenerateAccessToken(h.jwtSecret, h.accessTokenTTL, user.ID, profile.DeptID, profile.DeptName, profile.StudioID, profile.StudioName, email, name, user.ID, user.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "token generation failed"})
		return
	}

	refreshToken, err := auth.GenerateRefreshToken(h.jwtSecret, user.ID, user.Role, name, email, user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "token generation failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"accessToken":  accessToken,
		"refreshToken": refreshToken,
		"user": gin.H{
			"id":             user.ID,
			"name":           name,
			"email":          email,
			"role":           user.Role,
			"departmentId":   profile.DeptID,
			"departmentName": profile.DeptName,
			"studioId":       profile.StudioID,
			"studioName":     profile.StudioName,
		},
	})
}
