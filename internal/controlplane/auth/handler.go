package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
)

// Handler handles SSO authentication endpoints
type Handler struct {
	config      *Config
	oauthConfig *oauth2.Config
	endpoints   *OIDCEndpoints
}

// DaQunUserInfo represents the user info returned by DaQun OAuth
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

// OIDCEndpoints holds discovered OIDC endpoints
type OIDCEndpoints struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
	JwksURI               string `json:"jwks_uri"`
}

// NewHandler creates a new auth handler
func NewHandler(cfg *Config) (*Handler, error) {
	// Discover OIDC endpoints from well-known configuration
	endpoints, err := discoverEndpoints(cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC endpoints: %w", err)
	}

	oauthCfg := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURI,
		Scopes:       cfg.Scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:   endpoints.AuthorizationEndpoint,
			TokenURL:  endpoints.TokenEndpoint,
			AuthStyle: oauth2.AuthStyleInHeader, // client_secret_basic (大群默认)
		},
	}

	return &Handler{
		config:      cfg,
		oauthConfig: oauthCfg,
		endpoints:   endpoints,
	}, nil
}

// discoverEndpoints fetches OIDC configuration from well-known endpoint
func discoverEndpoints(issuerURL string) (*OIDCEndpoints, error) {
	wellKnownURL := issuerURL + "/.well-known/openid-configuration"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wellKnownURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
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

// Login redirects the user to the OAuth2 authorization endpoint
func (h *Handler) Login(c *gin.Context) {
	state := generateState()

	// Store state in cookie for CSRF protection
	c.SetCookie("oauth_state", state, 600, "/", "", false, true)

	url := h.oauthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline)
	c.Redirect(http.StatusTemporaryRedirect, url)
}

// Callback handles the OAuth2 callback after user authorization
func (h *Handler) Callback(c *gin.Context) {
	// Verify state for CSRF protection
	state := c.Query("state")
	savedState, err := c.Cookie("oauth_state")
	if err != nil || state != savedState {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "authentication_error",
				"message": "invalid oauth state",
				"code":    "invalid_state",
			},
		})
		return
	}

	// Clear state cookie
	c.SetCookie("oauth_state", "", -1, "/", "", false, true)

	// Exchange code for token
	code := c.Query("code")
	if code == "" {
		errMsg := c.Query("error_description")
		if errMsg == "" {
			errMsg = c.Query("error")
		}
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"type":    "authentication_error",
				"message": fmt.Sprintf("oauth callback error: %s", errMsg),
				"code":    "oauth_error",
			},
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()

	token, err := h.oauthConfig.Exchange(ctx, code)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"type":    "authentication_error",
				"message": fmt.Sprintf("failed to exchange token: %v", err),
				"code":    "token_exchange_failed",
			},
		})
		return
	}

	// Fetch user info from DaQun
	userInfo, err := h.fetchUserInfo(ctx, token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"type":    "authentication_error",
				"message": fmt.Sprintf("failed to get user info: %v", err),
				"code":    "userinfo_failed",
			},
		})
		return
	}

	// Generate our own JWT for the session
	claims := &UserClaims{
		Email:       userInfo.Email,
		Name:        userInfo.Name,
		Passport:    userInfo.Passport,
		Avatar:      userInfo.Avatar,
		CompanyName: userInfo.CompanyName,
		DeptName:    userInfo.EhrDeptName,
	}

	jwtToken, err := GenerateToken(h.config.JWTSecret, claims)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"type":    "authentication_error",
				"message": "failed to generate session token",
				"code":    "token_generation_failed",
			},
		})
		return
	}

	// Redirect to frontend with token
	redirectURL := fmt.Sprintf("%s/oauth/callback?token=%s", h.config.FrontendURL, jwtToken)
	c.Redirect(http.StatusTemporaryRedirect, redirectURL)
}

// GetCurrentUser returns the authenticated user's info
func (h *Handler) GetCurrentUser(c *gin.Context) {
	claims, exists := c.Get("user_claims")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{
				"type":    "authentication_error",
				"message": "not authenticated",
				"code":    "unauthorized",
			},
		})
		return
	}

	userClaims := claims.(*UserClaims)
	c.JSON(http.StatusOK, gin.H{
		"email":        userClaims.Email,
		"name":         userClaims.Name,
		"passport":     userClaims.Passport,
		"avatar":       userClaims.Avatar,
		"company_name": userClaims.CompanyName,
		"dept_name":    userClaims.DeptName,
	})
}

// fetchUserInfo fetches user information from DaQun's userinfo endpoint
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

// generateState generates a random state string for CSRF protection
func generateState() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}
