package middleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	cpmiddleware "github.com/example/llm-gateway/internal/controlplane/middleware"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testJWTSecret = "test-jwt-secret-for-testing"

func TestJWTAuth(t *testing.T) {
	// JWT 认证中间件的完整测试集合
	gin.SetMode(gin.TestMode)

	// 生成有效 JWT token
	validToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": "u1",
		"email":   "a@b.com",
		"name":    "测试用户",
	})
	validTokenString, err := validToken.SignedString([]byte(testJWTSecret))
	require.NoError(t, err)

	// 生成缺少 user_id 的 JWT token（有 passport）
	passportToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"passport": "p001",
		"email":    "b@c.com",
	})
	passportTokenString, err := passportToken.SignedString([]byte(testJWTSecret))
	require.NoError(t, err)

	// 生成缺少任何用户标识的 JWT token
	noIdentityToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"email": "c@d.com",
	})
	noIdentityTokenString, err := noIdentityToken.SignedString([]byte(testJWTSecret))
	require.NoError(t, err)

	tests := []struct {
		name        string
		description string
		token       string
		wantStatus  int
		wantUserID  string
		wantEmail   string
		wantName    string
	}{
		{
			name:        "无 Authorization header",
			description: "请求缺少 Authorization header，中间件应返回 401",
			token:       "", // 不设 header
			wantStatus:  http.StatusUnauthorized,
		},
		{
			name:        "Authorization 不以 Bearer 开头",
			description: "Authorization header 不以 Bearer 开头时，中间件应返回 401",
			token:       "Basic abc123", // 直接设 Basic
			wantStatus:  http.StatusUnauthorized,
		},
		{
			name:        "Bearer 后 token 为空",
			description: "Authorization header 为 'Bearer '（无实际 token），中间件应返回 401",
			token:       "", // 设 Bearer 空字符串
			wantStatus:  http.StatusUnauthorized,
		},
		{
			name:        "有效的 JWT token",
			description: "使用正确密钥签名的 JWT token，应通过认证，context 中含 user_id/email/name",
			token:       validTokenString,
			wantStatus:  http.StatusOK,
			wantUserID:  "u1",
			wantEmail:   "a@b.com",
			wantName:    "测试用户",
		},
		{
			name:        "无效的 JWT token",
			description: "token 字符串非法（非 JWT 格式），jwt.Parse 应失败，返回 401",
			token:       "eyJinvaliddata.garbage.111",
			wantStatus:  http.StatusUnauthorized,
		},
		{
			name:        "JWT 中使用 passport 替代 user_id",
			description: "JWT claims 中无 user_id 但有 passport 字段，中间件应降级使用 passport 作为 user_id",
			token:       passportTokenString,
			wantStatus:  http.StatusOK,
			wantUserID:  "p001",
			wantEmail:   "b@c.com",
		},
		{
			name:        "JWT 中缺少用户标识（无 user_id 也无 passport）",
			description: "JWT 签名有效但 claims 中无 user_id 和 passport，中间件应返回 401",
			token:       noIdentityTokenString,
			wantStatus:  http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 每个子测试独立设置 JWT_SECRET，确保互不影响
			t.Setenv("JWT_SECRET", testJWTSecret)

			var capturedUserID, capturedEmail, capturedName string
			router := gin.New()
			router.Use(cpmiddleware.JWTAuth())
			router.GET("/test", func(c *gin.Context) {
capturedUserID = c.GetString("user_id")
				capturedEmail = c.GetString("user_email")
				capturedName = c.GetString("user_name")
				c.Status(http.StatusOK)
			})

			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/test", nil)

			switch tt.name {
			case "无 Authorization header":
				// 不设 header
			case "Authorization 不以 Bearer 开头":
				req.Header.Set("Authorization", tt.token)
			case "Bearer 后 token 为空":
				req.Header.Set("Authorization", "Bearer ")
			default:
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code, tt.description)
			if tt.wantUserID != "" {
				assert.Equal(t, tt.wantUserID, capturedUserID, "user_id 应匹配")
			}
			if tt.wantEmail != "" {
				assert.Equal(t, tt.wantEmail, capturedEmail, "user_email 应匹配")
			}
			if tt.wantName != "" {
				assert.Equal(t, tt.wantName, capturedName, "user_name 应匹配")
			}
		})
	}
}

func TestJWTAuth_ResponseBody(t *testing.T) {
	// 测试目的：验证 401 响应体的 JSON 结构符合规范
	gin.SetMode(gin.TestMode)
	t.Setenv("JWT_SECRET", testJWTSecret)

	router := gin.New()
	router.Use(cpmiddleware.JWTAuth())
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	// 不设 Authorization header
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var body map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &body)
	require.NoError(t, err, "响应体应为合法 JSON")

	errObj, ok := body["error"].(map[string]interface{})
	require.True(t, ok, "响应应包含 error 对象")
	assert.Equal(t, "authentication_error", errObj["type"])
	assert.Equal(t, "unauthorized", errObj["code"])
	assert.NotEmpty(t, errObj["message"])
}

func TestInternalAuth(t *testing.T) {
	// 内部服务间 Token 认证中间件的完整测试集合
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name                string
		description         string
		xInternalToken      string // X-Internal-Token header 值，空表示不设
		authorizationBearer string // Authorization Bearer 值，空表示不设
		wantStatus          int
		wantInternalToken   string // 期望 context 中的 internal_token 值
	}{
		{
			name:                "X-Internal-Token 存在",
			description:         "设置了 X-Internal-Token header，中间件应放行并通过 context 透传 token",
			xInternalToken:      "my-internal-token-123",
			authorizationBearer: "",
			wantStatus:          http.StatusOK,
			wantInternalToken:   "my-internal-token-123",
		},
		{
			name:                "Authorization Bearer 存在",
			description:         "未设 X-Internal-Token 但设置了 Authorization: Bearer，中间件应降级读取 Bearer token",
			xInternalToken:      "",
			authorizationBearer: "bearer-token-from-auth",
			wantStatus:          http.StatusOK,
			wantInternalToken:   "bearer-token-from-auth",
		},
		{
			name:                "两种 header 均不存在",
			description:         "X-Internal-Token 和 Authorization 均未设置，中间件应返回 403 Forbidden",
			xInternalToken:      "",
			authorizationBearer: "",
			wantStatus:          http.StatusForbidden,
		},
		{
			name:                "优先使用 X-Internal-Token",
			description:         "同时设置了 X-Internal-Token 和 Authorization: Bearer，中间件应优先采用 X-Internal-Token",
			xInternalToken:      "x-token-wins",
			authorizationBearer: "bearer-loses",
			wantStatus:          http.StatusOK,
			wantInternalToken:   "x-token-wins",
		},
		{
			name:                "X-Internal-Token 为空字符串",
			description:         "X-Internal-Token header 存在但值为空，中间件应降级读取 Authorization Bearer",
			xInternalToken:      "",
			authorizationBearer: "fallback-bearer",
			wantStatus:          http.StatusOK,
			wantInternalToken:   "fallback-bearer",
		},
		{
			name:                "两种 header 均存在但值为空",
			description:         "X-Internal-Token 和 Authorization Bearer 都存在但均为空字符串，中间件应返回 403",
			xInternalToken:      "",
			authorizationBearer: "",
			wantStatus:          http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedInternalToken string
			router := gin.New()
			router.Use(cpmiddleware.InternalAuth())
			router.GET("/test", func(c *gin.Context) {
				capturedInternalToken = c.GetString("internal_token")
				c.Status(http.StatusOK)
			})

			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/test", nil)

			if tt.xInternalToken != "" || tt.name == "X-Internal-Token 为空字符串" {
				req.Header.Set("X-Internal-Token", tt.xInternalToken)
			}
			if tt.authorizationBearer != "" {
				req.Header.Set("Authorization", "Bearer "+tt.authorizationBearer)
			}

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code, tt.description)
			if tt.wantInternalToken != "" {
				assert.Equal(t, tt.wantInternalToken, capturedInternalToken, "internal_token 应匹配")
			}
		})
	}
}

func TestInternalAuth_ResponseBody(t *testing.T) {
	// 测试目的：验证 403 响应体的 JSON 结构符合规范
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(cpmiddleware.InternalAuth())
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)

	var body map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &body)
	require.NoError(t, err, "响应体应为合法 JSON")

	errObj, ok := body["error"].(map[string]interface{})
	require.True(t, ok, "响应应包含 error 对象")
	assert.Equal(t, "authorization_error", errObj["type"])
	assert.Equal(t, "forbidden", errObj["code"])
	assert.NotEmpty(t, errObj["message"])
}

// TestJWTAuth_AdminTokenInContext 验证 JWT 认证后将原始 token 注入 context
func TestJWTAuth_AdminTokenInContext(t *testing.T) {
	// 测试目的：JWTAuth 应在 context 中设置 admin_token 字段，值为原始 Bearer token
	gin.SetMode(gin.TestMode)
	t.Setenv("JWT_SECRET", testJWTSecret)

	validToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": "admin-1",
	})
	tokenString, err := validToken.SignedString([]byte(testJWTSecret))
	require.NoError(t, err)

	var capturedAdminToken string
	router := gin.New()
	router.Use(cpmiddleware.JWTAuth())
	router.GET("/test", func(c *gin.Context) {
		capturedAdminToken = c.GetString("admin_token")
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, tokenString, capturedAdminToken, "admin_token 应等于原始 token 字符串")
}

// TestJWTAuth_DefaultSecret 验证未设 JWT_SECRET 环境变量时使用默认密钥
func TestJWTAuth_DefaultSecret(t *testing.T) {
	// 测试目的：JWT_SECRET 未设置时，中间件应使用默认值 "change-me-in-production"
	gin.SetMode(gin.TestMode)

	// 确保环境变量未被设置
	t.Setenv("JWT_SECRET", "")

	// 使用默认密钥签名
	validToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": "default-secret-user",
	})
	tokenString, err := validToken.SignedString([]byte("change-me-in-production"))
	require.NoError(t, err)

	router := gin.New()
	router.Use(cpmiddleware.JWTAuth())
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "使用默认密钥签名的 token 应通过认证")
}

// init 确保测试运行前设置 Gin 为 TestMode（兜底）
func init() {
	gin.SetMode(gin.TestMode)
	// 确保默认密钥可用
	if v := os.Getenv("JWT_SECRET"); v == "" {
		os.Setenv("JWT_SECRET", testJWTSecret)
	}
}