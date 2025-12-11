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

// BootstrapGranter 由 *bootstrap.Checker 实现，通过接口注入避免循环依赖
type BootstrapGranter interface {
	CheckAndGrant(ctx context.Context, email, userID string) bool
}

// OrgSyncer 抽象 KeyOrgSyncer 依赖，避免循环引用
type OrgSyncer interface {
	OnUserDeptChanged(ctx context.Context, userID string)
}

type Handler struct {
	config           *Config
	oauthConfig      *oauth2.Config
	endpoints        *OIDCEndpoints
	db               *gorm.DB
	rdb              *redis.Client
	userRepo         *arepo.UserRepository
	deptRepo         *arepo.DeptRepository
	bootstrapGranter BootstrapGranter
	orgSyncer        OrgSyncer
	initAttempted    bool
	initErr          error
}

// SetBootstrapChecker 在 router 初始化后注入 bootstrap 检查器
func (h *Handler) SetBootstrapChecker(g BootstrapGranter) {
	h.bootstrapGranter = g
}

// SetOrgSyncer 注入 KeyOrgSyncer
func (h *Handler) SetOrgSyncer(s OrgSyncer) {
	h.orgSyncer = s
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

	// SSO 登录可能引起部门迁移，异步同步该用户的 api_keys org 字段
	if h.orgSyncer != nil {
		h.orgSyncer.OnUserDeptChanged(c.Request.Context(), user.ID.String())
	}

	// ── Bootstrap：若系统无管理员且当前账号在白名单，自动授予 admin ──────────
	isBootstrapAdmin := false
	if h.bootstrapGranter != nil {
		if h.bootstrapGranter.CheckAndGrant(c.Request.Context(), userInfo.Email, user.ID.String()) {
			user.Role = "admin"
			isBootstrapAdmin = true
		}
	}

	// ── 部门映射状态（供前端展示引导提示） ─────────────────────────────────
	deptStatus := h.deptMappingStatus(userInfo.EhrDeptID)

	// 查询 studio_id/studio_name 写入 JWT，供 gateway OrgCheckMiddleware 使用
	studioID, studioName := h.deptStudioInfo(userInfo.EhrDeptID)

	accessToken, err := GenerateAccessToken(h.config.JWTSecret, h.config.AccessTokenTTL, user.ID.String(), deptID, deptName, studioID, studioName, userInfo.Email, userInfo.Name, userInfo.Passport, user.Role)
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
		"role":               user.Role,
		"dept_status":        deptStatus,
		"is_bootstrap_admin": isBootstrapAdmin,
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
			// SSO dept_id 未命中 → 检查是否有导入时按名称创建的部门，把 dept_id 改为 SSO 的
			dept = h.mergeImportDept(deptID, deptName)
			if dept == nil {
				dept = &amodel.Department{DeptID: deptID, DeptName: deptName}
				if createErr := h.deptRepo.Create(dept); createErr != nil {
					slog.Warn("failed to create department", "error", createErr)
				}
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
		// 确保登录状态为 active（可能之前是 pending）
		existingUser.LoginStatus = "active"
		if updateErr := h.userRepo.Update(existingUser); updateErr != nil {
			slog.Warn("failed to update user", "error", updateErr)
		}
		return existingUser, deptID, deptName
	}

	// sub 未命中 → 检查是否有按邮箱导入的 pending 用户，激活之
	pendingUser, pendingErr := h.userRepo.FindByEmail(info.Email)
	if pendingErr == nil && pendingUser != nil && pendingUser.LoginStatus == "pending" {
		pendingUser.Sub = info.Passport
		pendingUser.Name = info.Name
		pendingUser.LoginStatus = "active"
		if deptModel != nil {
			pendingUser.DeptID = &deptModel.ID
		}
		if updateErr := h.userRepo.Update(pendingUser); updateErr != nil {
			slog.Warn("failed to activate pending user", "error", updateErr)
		}
		return pendingUser, deptID, deptName
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

// mergeImportDept 将导入创建的部门（dept_id 为 import: 前缀或名称）合并到 SSO 的 dept_id
// 返回合并后的 department，失败或无匹配时返回 nil
func (h *Handler) mergeImportDept(ssDeptID, deptName string) *amodel.Department {
	var importDept amodel.Department
	err := h.db.Where("LOWER(dept_name) = LOWER(?) AND dept_id != ?", deptName, ssDeptID).
		First(&importDept).Error
	if err != nil {
		return nil
	}

	oldDeptID := importDept.DeptID
	// 更新 dept_id 为 SSO 的
	if err := h.db.Exec(`UPDATE departments SET dept_id = ? WHERE dept_id = ?`, ssDeptID, oldDeptID).Error; err != nil {
		slog.Warn("failed to merge department dept_id", "old", oldDeptID, "new", ssDeptID, "error", err)
		return nil
	}
	// 同步 dept_studio_map 的 dept_id
	if err := h.db.Exec(`UPDATE dept_studio_map SET dept_id = ? WHERE dept_id = ?`, ssDeptID, oldDeptID).Error; err != nil {
		slog.Warn("failed to update dept_studio_map dept_id", "old", oldDeptID, "new", ssDeptID, "error", err)
	}

	dept, err := h.deptRepo.FindByDeptID(ssDeptID)
	if err != nil {
		return nil
	}
	return dept
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
	// JWT dept_id 存的是 SSO 原始字符串（EhrDeptID），可直接用于 dept_studio_map 查询
	deptIDRaw, _ := c.Get("user_dept_id")
	if userID == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"type": "authentication_error", "message": "not authenticated", "code": "unauthorized"}})
		return
	}

	// 实时计算部门映射状态
	ssoDeptID, _ := deptIDRaw.(string)
	deptStatus := h.deptMappingStatus(ssoDeptID)

	c.JSON(http.StatusOK, gin.H{
		"id":          userID,
		"email":       email,
		"name":        name,
		"dept_status": deptStatus,
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

	// Refresh 时重新查库获取最新的 dept/studio 信息，
	// 避免 org 变动后 token 里的 studio_id 仍为旧值导致 OrgCheck 403。
	deptID, deptName, studioID, studioName := claims.DeptID, claims.DeptName, claims.StudioID, claims.StudioName
	if claims.UserID != "" {
		if freshStudioID, freshStudioName := h.deptStudioInfo(claims.DeptID); freshStudioID != "" {
			studioID = freshStudioID
			studioName = freshStudioName
		}
		// dept 信息也从 user 表刷新，防止部门迁移后 dept_id/dept_name 过期
		if freshDeptID, freshDeptName := h.userDeptInfo(c.Request.Context(), claims.UserID); freshDeptID != "" {
			deptID = freshDeptID
			deptName = freshDeptName
			// 用新 deptID 重新查 studio
			if sid, sname := h.deptStudioInfo(freshDeptID); sid != "" {
				studioID = sid
				studioName = sname
			}
		}
	}

	accessToken, err := GenerateAccessToken(h.config.JWTSecret, h.config.AccessTokenTTL, claims.UserID, deptID, deptName, studioID, studioName, claims.Email, claims.Name, claims.Passport, claims.Role)
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

// deptMappingStatus 返回部门的工作室映射状态：
//   - "none"           → SSO 未携带部门信息
//   - "pending_studio" → 部门存在但未映射工作室
//   - "active"         → 已映射工作室
//
// 内部复用 deptStudioInfo，避免两次查库。
func (h *Handler) deptMappingStatus(ehrDeptID string) string {
	if ehrDeptID == "" {
		return "none"
	}
	studioID, _ := h.deptStudioInfo(ehrDeptID)
	if studioID == "" {
		return "pending_studio"
	}
	return "active"
}

// deptStudioInfo 根据 SSO dept_id 查询对应的 studio_id 和 studio_name。
// 供生成 JWT 时写入 claims，让 gateway OrgCheckMiddleware 能正确判断。
func (h *Handler) deptStudioInfo(ehrDeptID string) (studioID, studioName string) {
	if ehrDeptID == "" {
		return "", ""
	}
	var row struct {
		StudioID   string `gorm:"column:studio_id"`
		StudioName string `gorm:"column:studio_name"`
	}
	h.db.Raw(`
		SELECT dsm.studio_id::text AS studio_id, COALESCE(s.name, '') AS studio_name
		FROM dept_studio_map dsm
		LEFT JOIN studios s ON s.id = dsm.studio_id
		WHERE dsm.dept_id = ?
		LIMIT 1
	`, ehrDeptID).Scan(&row)
	return row.StudioID, row.StudioName
}

// userDeptInfo 通过 user_id 查询用户最新的 SSO dept_id 和 dept_name。
// 用于 Refresh Token 时刷新 JWT 里的部门信息。
func (h *Handler) userDeptInfo(ctx context.Context, userID string) (deptID, deptName string) {
	var row struct {
		DeptID   string `gorm:"column:dept_id"`
		DeptName string `gorm:"column:dept_name"`
	}
	h.db.WithContext(ctx).Raw(`
		SELECT COALESCE(d.dept_id, '') AS dept_id, COALESCE(d.dept_name, '') AS dept_name
		FROM users u
		LEFT JOIN departments d ON d.id = u.dept_id
		WHERE u.id::text = ?
		LIMIT 1
	`, userID).Scan(&row)
	return row.DeptID, row.DeptName
}

func generateState() string {
	b := make([]byte, 24)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}