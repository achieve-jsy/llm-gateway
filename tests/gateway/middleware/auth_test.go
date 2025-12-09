package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/gateway/infra"
	"github.com/example/llm-gateway/internal/gateway/middleware"
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeAuthServer 模拟控制面 auth verify-key 端点，用于测试 AuthMiddleware 的 API Key 路径
func fakeAuthServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(handler)
}

// newAuthClient 创建指向 fake server 的 AuthClient
func newAuthClient(baseURL string) *infra.AuthClient {
	return infra.NewAuthClient(baseURL, "test-internal-token")
}

func TestAuthMiddleware_MissingAuthorizationHeader(t *testing.T) {
	// 测试目的：缺少 Authorization header 时应返回 401
	gin.SetMode(gin.TestMode)

	srv := fakeAuthServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("不应调用 auth service")
	})
	defer srv.Close()

	router := gin.New()
	router.Use(middleware.AuthMiddleware(newAuthClient(srv.URL)))
	router.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_InvalidAuthorizationFormat(t *testing.T) {
	// 测试目的：Authorization header 不以 Bearer 开头时应返回 401
	gin.SetMode(gin.TestMode)

	srv := fakeAuthServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("不应调用 auth service")
	})
	defer srv.Close()

	router := gin.New()
	router.Use(middleware.AuthMiddleware(newAuthClient(srv.URL)))
	router.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Basic abc123")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_EmptyBearerToken(t *testing.T) {
	// 测试目的：Bearer 后 token 为空时应返回 401
	gin.SetMode(gin.TestMode)

	srv := fakeAuthServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("不应调用 auth service")
	})
	defer srv.Close()

	router := gin.New()
	router.Use(middleware.AuthMiddleware(newAuthClient(srv.URL)))
	router.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer ")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_APIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		description string
		apiKey      string
		// serverJSON 直接用 JSON 字符串，避免内嵌匿名结构体字面量随字段增减而失效
		serverJSON string
		serverErr  bool
		wantStatus int
		wantUserID string
	}{
		{
			name:        "有效的 sk- token",
			description: "VerifyKey 返回 active 状态，请求应通过且 context 包含正确的 identity",
			apiKey:      "sk-valid-key-123",
			serverJSON: `{"code":0,"message":"ok","data":{` +
				`"status":"active","key_id":"key-001","access_all_groups":true,` +
				`"user":{"user_id":"user-123","name":"测试用户","department_id":"dept-456"}}}`,
			wantStatus: http.StatusOK,
			wantUserID: "user-123",
		},
		{
			name:        "无效的 sk- token（inactive）",
			description: "VerifyKey 返回 inactive 状态，应返回 401 认证失败",
			apiKey:      "sk-disabled-key",
			serverJSON:  `{"code":0,"message":"ok","data":{"status":"inactive"}}`,
			wantStatus:  http.StatusUnauthorized,
		},
		{
			name:        "auth service 不可用",
			description: "VerifyKey 返回错误（auth service down），应返回 503",
			apiKey:      "sk-service-down",
			serverErr:   true,
			wantStatus:  http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := fakeAuthServer(t, func(w http.ResponseWriter, r *http.Request) {
				if tt.serverErr {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(tt.serverJSON))
			})
			defer srv.Close()

			var capturedIdentity *gwmodel.UserIdentity
			router := gin.New()
			router.Use(middleware.AuthMiddleware(newAuthClient(srv.URL)))
			router.GET("/test", func(c *gin.Context) {
				if val, exists := c.Get("identity"); exists {
					capturedIdentity = val.(*gwmodel.UserIdentity)
				}
				c.Status(http.StatusOK)
			})

			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/test", nil)
			req.Header.Set("Authorization", "Bearer "+tt.apiKey)
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
			if tt.wantUserID != "" {
				require.NotNil(t, capturedIdentity)
				assert.Equal(t, tt.wantUserID, capturedIdentity.UserID)
			}
		})
	}
}

func TestAuthMiddleware_JWT(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		description string
		token       string
		wantStatus  int
		wantUserID  string
		wantDeptID  string
	}{
		{
			name:        "有效的 JWT token",
			description: "JWT 以 eyJ 开头且 ParseUnverified 能解析，应提取 user_id 和 dept_id 设入 context",
			token:       "", // 动态生成
			wantStatus:  http.StatusOK,
			wantUserID:  "jwt-user-789",
			wantDeptID:  "jwt-dept-012",
		},
		{
			name:        "无效的 JWT token",
			description: "token 以 eyJ 开头但格式不合法导致 ParseUnverified 失败，应返回 401",
			token:       "eyJinvalidtoken.notreal.garbage",
			wantStatus:  http.StatusUnauthorized,
		},
	}

	// 为第一个用例生成有效 JWT
	validToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": "jwt-user-789",
		"dept_id": "jwt-dept-012",
	})
	validTokenString, err := validToken.SignedString([]byte("test-secret"))
	require.NoError(t, err)
	tests[0].token = validTokenString

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// JWT 路径不需要 auth service
			srv := fakeAuthServer(t, func(w http.ResponseWriter, r *http.Request) {
				t.Fatal("JWT 路径不应调用 auth service")
			})
			defer srv.Close()

			var capturedIdentity *gwmodel.UserIdentity
			router := gin.New()
			router.Use(middleware.AuthMiddleware(newAuthClient(srv.URL)))
			router.GET("/test", func(c *gin.Context) {
				if val, exists := c.Get("identity"); exists {
					capturedIdentity = val.(*gwmodel.UserIdentity)
				}
				c.Status(http.StatusOK)
			})

			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/test", nil)
			req.Header.Set("Authorization", "Bearer "+tt.token)
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
			if tt.wantUserID != "" {
				require.NotNil(t, capturedIdentity)
				assert.Equal(t, tt.wantUserID, capturedIdentity.UserID)
				assert.Equal(t, tt.wantDeptID, capturedIdentity.DepartmentID)
			}
		})
	}
}

func TestAuthMiddleware_UnknownTokenFormat(t *testing.T) {
	// 测试目的：token 不以 sk- 或 eyJ 开头时应返回 401
	gin.SetMode(gin.TestMode)

	srv := fakeAuthServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("未知格式 token 不应调用 auth service")
	})
	defer srv.Close()

	router := gin.New()
	router.Use(middleware.AuthMiddleware(newAuthClient(srv.URL)))
	router.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer unknown-format-token")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
