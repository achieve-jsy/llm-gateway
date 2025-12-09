package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/gateway/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// setupOrgCheckRouter 创建挂载了 OrgCheckMiddleware 的测试路由
func setupOrgCheckRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.OrgCheckMiddleware())
	r.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	return r
}

// setOrgContext 在请求上预设 dept_id / studio_id（模拟 AuthMiddleware 写入）
func setOrgContext(deptID, studioID string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("dept_id", deptID)
		c.Set("studio_id", studioID)
		c.Next()
	}
}

func TestOrgCheckMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		description string
		deptID      string
		studioID    string
		wantStatus  int
	}{
		{
			name:        "dept 和 studio 均有值",
			description: "用户已归属部门且部门已映射工作室，应放行",
			deptID:      "dept-uuid-001",
			studioID:    "studio-uuid-001",
			wantStatus:  http.StatusOK,
		},
		{
			name:        "dept 和 studio 均为空",
			description: "用户没有部门信息（dept_status=none），OrgCheck 不拦截，应放行",
			deptID:      "",
			studioID:    "",
			wantStatus:  http.StatusOK,
		},
		{
			name:        "有 dept 无 studio",
			description: "用户有部门但部门未映射工作室（pending_studio），应返回 403",
			deptID:      "dept-uuid-002",
			studioID:    "",
			wantStatus:  http.StatusForbidden,
		},
		{
			name:        "无 dept 有 studio",
			description: "异常情况：dept 为空但 studio 有值（不触发拦截），应放行",
			deptID:      "",
			studioID:    "studio-uuid-001",
			wantStatus:  http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			// 先写入 org context，再执行 OrgCheckMiddleware
			r.Use(setOrgContext(tt.deptID, tt.studioID))
			r.Use(middleware.OrgCheckMiddleware())
			r.GET("/test", func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/test", nil)
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code, tt.description)
		})
	}
}

func TestOrgCheckMiddleware_ErrorBody(t *testing.T) {
	// 测试目的：403 响应体应包含 org_pending_studio 错误码
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(setOrgContext("dept-001", ""))
	r.Use(middleware.OrgCheckMiddleware())
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "org_pending_studio")
	assert.Contains(t, body, "organization_error")
}

func TestOrgCheckMiddleware_Abort(t *testing.T) {
	// 测试目的：403 时后续 handler 不应被执行（Abort 生效）
	gin.SetMode(gin.TestMode)

	handlerCalled := false
	r := gin.New()
	r.Use(setOrgContext("dept-001", ""))
	r.Use(middleware.OrgCheckMiddleware())
	r.GET("/test", func(c *gin.Context) {
		handlerCalled = true
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.False(t, handlerCalled, "403 时后续 handler 不应被执行")
}
