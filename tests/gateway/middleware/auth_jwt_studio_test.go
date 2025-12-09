package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/gateway/middleware"
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeJWT 生成包含指定 claims 的 JWT（用任意 secret 签名，AuthMiddleware 不验签）
func makeJWT(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	str, err := token.SignedString([]byte("test-secret"))
	require.NoError(t, err)
	return str
}

func TestAuthMiddleware_JWT_StudioID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name            string
		description     string
		claims          jwt.MapClaims
		wantStatus      int
		wantUserID      string
		wantDeptID      string
		wantStudioID    string
		wantStudioName  string
	}{
		{
			name:        "JWT 携带完整 org 信息",
			description: "JWT claims 含 dept_id/studio_id/studio_name，AuthMiddleware 应全部写入 context",
			claims: jwt.MapClaims{
				"user_id":     "user-001",
				"dept_id":     "ehr-dept-001",
				"dept_name":   "研发部",
				"studio_id":   "studio-uuid-001",
				"studio_name": "X-Lab",
				"name":        "张三",
			},
			wantStatus:     http.StatusOK,
			wantUserID:     "user-001",
			wantDeptID:     "ehr-dept-001",
			wantStudioID:   "studio-uuid-001",
			wantStudioName: "X-Lab",
		},
		{
			name:        "JWT 有 dept 无 studio",
			description: "dept_id 有值但 studio_id 缺失，studio_id 应为空字符串",
			claims: jwt.MapClaims{
				"user_id": "user-002",
				"dept_id": "ehr-dept-002",
				"name":    "李四",
			},
			wantStatus:   http.StatusOK,
			wantUserID:   "user-002",
			wantDeptID:   "ehr-dept-002",
			wantStudioID: "",
		},
		{
			name:        "JWT 无 dept 无 studio",
			description: "新用户尚未关联部门，dept_id 和 studio_id 均为空，OrgCheck 不触发",
			claims: jwt.MapClaims{
				"user_id": "user-003",
				"name":    "王五",
			},
			wantStatus:   http.StatusOK,
			wantUserID:   "user-003",
			wantDeptID:   "",
			wantStudioID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// JWT 路径不调用 auth service，用 fake server 防止误调
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Fatal("JWT 路径不应调用 auth service")
			}))
			defer srv.Close()

			var (
				capturedIdentity   *gwmodel.UserIdentity
				capturedStudioID   string
				capturedStudioName string
				capturedDeptID     string
			)

			r := gin.New()
			r.Use(middleware.AuthMiddleware(newAuthClient(srv.URL)))
			r.GET("/test", func(c *gin.Context) {
				if val, exists := c.Get("identity"); exists {
					capturedIdentity = val.(*gwmodel.UserIdentity)
				}
				capturedStudioID = c.GetString("studio_id")
				capturedStudioName = c.GetString("studio_name")
				capturedDeptID = c.GetString("dept_id")
				c.Status(http.StatusOK)
			})

			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/test", nil)
			req.Header.Set("Authorization", "Bearer "+makeJWT(t, tt.claims))
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code, tt.description)
			if tt.wantUserID != "" {
				require.NotNil(t, capturedIdentity)
				assert.Equal(t, tt.wantUserID, capturedIdentity.UserID)
				assert.Equal(t, tt.wantDeptID, capturedDeptID)
				assert.Equal(t, tt.wantStudioID, capturedStudioID)
				assert.Equal(t, tt.wantStudioID, capturedIdentity.StudioID, "identity.StudioID 应与 context 一致")
				if tt.wantStudioName != "" {
					assert.Equal(t, tt.wantStudioName, capturedStudioName)
				}
			}
		})
	}
}

func TestAuthMiddleware_JWT_OrgCheck_Integration(t *testing.T) {
	// 测试目的：JWT 携带 dept_id 但无 studio_id 时，AuthMiddleware + OrgCheckMiddleware 组合应返回 403
	gin.SetMode(gin.TestMode)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("JWT 路径不应调用 auth service")
	}))
	defer srv.Close()

	token := makeJWT(t, jwt.MapClaims{
		"user_id": "user-pending",
		"dept_id": "ehr-dept-pending",
		"name":    "赵六",
		// 无 studio_id
	})

	r := gin.New()
	r.Use(middleware.AuthMiddleware(newAuthClient(srv.URL)))
	r.Use(middleware.OrgCheckMiddleware())
	r.GET("/v1/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "org_pending_studio")
}

func TestAuthMiddleware_JWT_OrgCheck_WithStudio_Pass(t *testing.T) {
	// 测试目的：JWT 同时携带 dept_id 和 studio_id 时，OrgCheck 应放行
	gin.SetMode(gin.TestMode)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("JWT 路径不应调用 auth service")
	}))
	defer srv.Close()

	token := makeJWT(t, jwt.MapClaims{
		"user_id":   "user-active",
		"dept_id":   "ehr-dept-active",
		"studio_id": "studio-active",
		"name":      "孙七",
	})

	r := gin.New()
	r.Use(middleware.AuthMiddleware(newAuthClient(srv.URL)))
	r.Use(middleware.OrgCheckMiddleware())
	r.GET("/v1/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}
