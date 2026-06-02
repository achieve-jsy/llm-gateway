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
	// 内部服务间 Token 认证中间件的完整测试集合。
	// 安全语义（fail-closed）：仅当请求 token 与 env INTERNAL_TOKEN 常量时间比对一致才放行，
	// 任意非空 token 不再被视为合法（修复前的旧行为）。
	gin.SetMode(gin.TestMode)

	const expectedToken = "test-internal-token-abc"

	tests := []struct {
		name                string
		description         string
		xInternalToken      string // X-Internal-Token header 值，空表示不设
		authorizationBearer string // Authorization Bearer 值，空表示不设
		wantStatus          int
		wantInternalToken   string // 期望 context 中的 internal_token 值
	}{
		{
			name:                "X-Internal-Token 匹配",
			description:         "X-Internal-Token 与 INTERNAL_TOKEN 一致，中间件应放行并通过 context 透传 token",
			xInternalToken:      expectedToken,
			authorizationBearer: "",
			wantStatus:          http.StatusOK,
			wantInternalToken:   expectedToken,
		},
		{
			name:                "Authorization Bearer 匹配",
			description:         "未设 X-Internal-Token 但 Authorization: Bearer 与 INTERNAL_TOKEN 一致，中间件应降级读取 Bearer 并放行",
			xInternalToken:      "",
			authorizationBearer: expectedToken,
			wantStatus:          http.StatusOK,
			wantInternalToken:   expectedToken,
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
			description:         "X-Internal-Token 匹配但 Authorization Bearer 不匹配，中间件应优先采用 X-Internal-Token 并放行",
			xInternalToken:      expectedToken,
			authorizationBearer: "wrong-bearer",
			wantStatus:          http.StatusOK,
			wantInternalToken:   expectedToken,
		},
		{
			name:                "token 不匹配",
			description:         "提供的 X-Internal-Token 与 INTERNAL_TOKEN 不一致，中间件应返回 403（安全核心：不再信任任意非空 token）",
			xInternalToken:      "wrong-token",
			authorizationBearer: "",
			wantStatus:          http.StatusForbidden,
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
			t.Setenv("INTERNAL_TOKEN", expectedToken)

			var capturedInternalToken string
			router := gin.New()
			router.Use(cpmiddleware.InternalAuth())
			router.GET("/test", func(c *gin.Context) {
				capturedInternalToken = c.GetString("internal_token")
				c.Status(http.StatusOK)
			})

			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/test", nil)

			if tt.xInternalToken != "" {
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

// TestJWTAuth_DefaultSecretFailClosed 验证 fail-closed：JWT_SECRET 未配置或命中已知弱默认值时，
// 即使攻击者用公开默认值伪造 token，JWTAuth 也必须拒绝（401）。
func TestJWTAuth_DefaultSecretFailClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 用已知弱默认密钥签名，模拟攻击者伪造的 admin token
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"user_id": "forged-admin"})
	tokenString, err := tok.SignedString([]byte("change-me-in-production"))
	require.NoError(t, err)

	for _, tc := range []struct {
		name   string
		secret string
	}{
		{name: "JWT_SECRET 未设置", secret: ""},
		{name: "JWT_SECRET 命中生产默认值", secret: "change-me-in-production"},
		{name: "JWT_SECRET 命中 dev 默认值", secret: "CHANGE_ME_JWT_SECRET"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("JWT_SECRET", tc.secret)

			router := gin.New()
			router.Use(cpmiddleware.JWTAuth())
			router.GET("/test", func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/test", nil)
			req.Header.Set("Authorization", "Bearer "+tokenString)
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code, "弱默认密钥签发的 token 必须被 fail-closed 拒绝")
		})
	}
}

// init 确保测试运行前设置 Gin 为 TestMode（兜底）
func init() {
	gin.SetMode(gin.TestMode)
	// 确保默认密钥可用
	if v := os.Getenv("JWT_SECRET"); v == "" {
		os.Setenv("JWT_SECRET", testJWTSecret)
	}
}