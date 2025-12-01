package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"golang.org/x/oauth2"
	"gorm.io/gorm"

	amodel "github.com/example/llm-gateway/internal/auth/model"
	arepo "github.com/example/llm-gateway/internal/auth/repository"
)

type Handler struct {
	config       *Config
	oauthConfig  *oauth2.Config
	endpoints    *OIDCEndpoints
	db           *gorm.DB
	rdb          *redis.Client
	userRepo     *arepo.UserRepository
	deptRepo     *arepo.DeptRepository
	initAttempted bool
	initErr      error
}

type DaQunUserInfo struct {
	Avatar      string `json:"avatar"`
	CompanyName string `json:"company_name"`
	Email       string `json:"email"`
	Name        string `json:"name"`
	Passport    string `json:"passport"`
	EhrDeptID   string `json:"ehr_dept_id"`
	EhrDeptName string `json:"ehr_dept_name"`
	EhrOrgID    string `json:"ehr_org_id"`
	EhrOrgName  string `json:"ehr_org_name"`
}

type OIDCEndpoints struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
	JwksURI               string `json:"jwks_uri"`
}

func NewHandler(cfg *Config, db *gorm.DB, rdb *redis.Client) *Handler {
	h := &Handler{
		config:    cfg,
		db:        db,
		rdb:       rdb,
		userRepo:  arepo.NewUserRepository(db),
		deptRepo:  arepo.NewDeptRepository(db),
	}
	_ = h.tryInit()
	return h
}

func (h *Handler) tryInit() error {
	if h.initAttempted {
		return h.initErr
	}
	h.initAttempted = true

	endpoints, err := discoverEndpoints(h.config.IssuerURL)
	if err != nil {
		h.initErr = fmt.Errorf("discover OIDC endpoints: %w", err)
		return h.initErr
	}
	h.endpoints = endpoints

	oauthCfg := &oauth2.Config{
		ClientID:     h.config.ClientID,
		ClientSecret: h.config.ClientSecret,
		RedirectURL:  h.config.RedirectURI,
		Scopes:       h.config.Scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:   endpoints.AuthorizationEndpoint,
			TokenURL:  endpoints.TokenEndpoint,
			AuthStyle: oauth2.AuthStyleInHeader,
		},
	}
	h.oauthConfig = oauthCfg
	return nil
}

var oidcHTTPClient = &http.Client{
	Transport: &http.Transport{
		Proxy: nil,
	},
	Timeout: 10 * time.Second,
}

func discoverEndpoints(issuerURL string) (*OIDCEndpoints, error) {
	wellKnownURL := issuerURL + "/.well-known/openid-configuration"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wellKnownURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := oidcHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch openid-configuration: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openid-configuration returned status %d", resp.StatusCode)
	}

	var endpoints OIDCEndpoints
	if err := json.NewDecoder(resp.Body).Decode(&endpoints); err != nil {
		return nil, fmt.Errorf("decode openid-configuration: %w", err)
	}
	return &endpoints, nil
}

func (h *Handler) Login(c *gin.Context) {
	if err := h.tryInit(); err != nil {
		slog.Error("SSO init failed", "error", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"type": "sso_unavailable", "message": "SSO 服务暂时不可用，请稍后重试", "code": "sso_unavailable"}})
		return
	}
	state := generateState()
	if err := h.rdb.Set(c.Request.Context(), "oidc:state:"+state, "1", 5*time.Minute).Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"type": "internal_error", "message": "failed to save state", "code": "internal"}})
		return
	}
	url := h.oauthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline)
	c.Redirect(http.StatusTemporaryRedirect, url)
}

type oidcCallbackRequest struct {
	Code  string `json:"code" binding:"required"`
	State string `json:"state" binding:"required"`
}

func (h *Handler) Callback(c *gin.Context) {
	if err := h.tryInit(); err != nil {
		slog.Error("SSO init failed in callback", "error", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"type": "sso_unavailable", "message": "SSO 服务暂时不可用，请稍后重试", "code": "sso_unavailable"}})
		return
	}
	var req oidcCallbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request_error", "message": "invalid payload", "code": "invalid_request"}})
		return
	}

	exists, err := h.rdb.Exists(c.Request.Context(), "oidc:state:"+req.State).Result()
	if err != nil || exists == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "authentication_error", "message": "invalid oidc state", "code": "invalid_state"}})
		return
	}
	h.rdb.Del(c.Request.Context(), "oidc:state:"+req.State)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()

	ctx = context.WithValue(ctx, oauth2.HTTPClient, oidcHTTPClient)

	token, err := h.oauthConfig.Exchange(ctx, req.Code)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"type": "authentication_error", "message": fmt.Sprintf("failed to exchange token: %v", err), "code": "token_exchange_failed"}})
		return
	}

	userInfo, err := h.fetchUserInfo(ctx, token)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"type": "authentication_error", "message": fmt.Sprintf("failed to get user info: %v", err), "code": "userinfo_failed"}})
		return
	}

	user, deptID, deptName := h.upsertUser(userInfo)

	accessToken, err := GenerateAccessToken(h.config.JWTSecret, h.config.AccessTokenTTL, user.ID.String(), deptID, deptName, userInfo.Email, userInfo.Name, userInfo.Passport, user.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"type": "internal_error", "message": "failed to generate token", "code": "token_generation_failed"}})
		return
	}

	refreshToken, err := GenerateRefreshToken(h.config.JWTSecret, user.ID.String(), user.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"type": "internal_error", "message": "failed to generate token", "code": "token_generation_failed"}})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"accessToken":  accessToken,
		"refreshToken": refreshToken,
		"user": gin.H{
			"id":             user.ID.String(),
			"name":           userInfo.Name,
			"email":          userInfo.Email,
			"departmentId":   deptID,
			"departmentName": deptName,
		},
		"role": user.Role,
	})
}

func (h *Handler) upsertUser(info *DaQunUserInfo) (*amodel.User, string, string) {
	role := "user"
	deptID := info.EhrDeptID
	deptName := info.EhrDeptName

	// 确保部门记录存在
	var deptModel *amodel.Department
	if deptID != "" {
		dept, err := h.deptRepo.FindByDeptID(deptID)
		if err != nil {
			dept = &amodel.Department{DeptID: deptID, DeptName: deptName}
			if createErr := h.deptRepo.Create(dept); createErr != nil {
				slog.Warn("failed to create department", "error", createErr)
			}
		}
		if dept != nil {
			deptModel = dept
		}
	}

	existingUser, err := h.userRepo.FindBySub(info.Passport)
	if err == nil && existingUser != nil {
		existingUser.Name = info.Name
		existingUser.Email = info.Email

		if deptModel != nil {
			// dept_source=manual：不覆盖手动归属，改为记录冲突提醒
			if existingUser.DeptSource == "manual" {
				currentDeptID := ""
				if existingUser.DeptID != nil {
					currentDeptID = existingUser.DeptID.String()
				}
				if currentDeptID != deptModel.ID.String() {
					// 异步写冲突提醒，不阻塞登录
					go func() {
						if err := h.db.Exec(`
							INSERT INTO user_dept_conflicts(user_id, current_dept_id, sso_dept_id, sso_dept_name)
							VALUES(?, NULLIF(?,'')::uuid, NULLIF(?,'')::uuid, ?)
							ON CONFLICT DO NOTHING
						`, existingUser.ID, currentDeptID, deptModel.ID.String(), deptName).Error; err != nil {
							slog.Warn("failed to record dept conflict", "user_id", existingUser.ID, "error", err)
						}
					}()
				}
				// 只更新 name/email，不改 dept_id
				if updateErr := h.userRepo.Update(existingUser); updateErr != nil {
					slog.Warn("failed to update user", "error", updateErr)
				}
				return existingUser, deptID, deptName
			}

			// dept_source=sso（默认）：按 SSO claims 正常迁移
			existingUser.DeptID = &deptModel.ID
		}
		if updateErr := h.userRepo.Update(existingUser); updateErr != nil {
			slog.Warn("failed to update user", "error", updateErr)
		}
		return existingUser, deptID, deptName
	}

	newUser := &amodel.User{
		Sub:    info.Passport,
		Name:   info.Name,
		Email:  info.Email,
		Role:   role,
		Source: "oidc",
	}
	if deptModel != nil {
		newUser.DeptID = &deptModel.ID
	}
	if createErr := h.userRepo.Create(newUser); createErr != nil {
		slog.Warn("failed to create user", "error", createErr)
	}
	return newUser, deptID, deptName
}

func (h *Handler) fetchUserInfo(ctx context.Context, token *oauth2.Token) (*DaQunUserInfo, error) {
	client := h.oauthConfig.Client(ctx, token)
	userinfoURL := h.endpoints.UserinfoEndpoint
	if userinfoURL == "" {
		userinfoURL = h.config.IssuerURL + "/userinfo"
	}

	resp, err := client.Get(userinfoURL)
	if err != nil {
		return nil, fmt.Errorf("request userinfo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("userinfo returned %d: %s", resp.StatusCode, string(body))
	}

	var userInfo DaQunUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return nil, fmt.Errorf("decode userinfo: %w", err)
	}
	return &userInfo, nil
}

func (h *Handler) GetCurrentUser(c *gin.Context) {
	userID, _ := c.Get("user_id")
	email, _ := c.Get("user_email")
	name, _ := c.Get("user_name")
	if userID == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"type": "authentication_error", "message": "not authenticated", "code": "unauthorized"}})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id":    userID,
		"email": email,
		"name":  name,
	})
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *Handler) Refresh(c *gin.Context) {
	var req refreshRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.RefreshToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request", "message": "missing refresh_token", "code": "invalid_request"}})
		return
	}
	claims, err := ParseToken(h.config.JWTSecret, req.RefreshToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"type": "authentication_error", "message": "invalid token", "code": "unauthorized"}})
		return
	}
	if h.rdb.SIsMember(c.Request.Context(), "token:blacklist", claims.ID).Val() {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"type": "authentication_error", "message": "token revoked", "code": "unauthorized"}})
		return
	}
	accessToken, err := GenerateAccessToken(h.config.JWTSecret, h.config.AccessTokenTTL, claims.UserID, claims.DeptID, claims.DeptName, claims.Email, claims.Name, claims.Passport, claims.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"type": "internal_error", "message": "token generation failed", "code": "internal"}})
		return
	}
	refreshToken, err := GenerateRefreshToken(h.config.JWTSecret, claims.UserID, claims.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"type": "internal_error", "message": "token generation failed", "code": "internal"}})
		return
	}
	h.rdb.SAdd(c.Request.Context(), "token:blacklist", claims.ID)
	c.JSON(http.StatusOK, gin.H{
		"accessToken":  accessToken,
		"refreshToken": refreshToken,
	})
}

func (h *Handler) Logout(c *gin.Context) {
	var req refreshRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.RefreshToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request", "message": "missing refresh_token", "code": "invalid_request"}})
		return
	}
	claims, err := ParseToken(h.config.JWTSecret, req.RefreshToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"type": "authentication_error", "message": "invalid token", "code": "unauthorized"}})
		return
	}
	h.rdb.SAdd(c.Request.Context(), "token:blacklist", claims.ID)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": nil})
}

func generateState() string {
	b := make([]byte, 24)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}