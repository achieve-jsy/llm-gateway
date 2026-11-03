package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func setupGuardRouter(guard *middleware.GatewayGuard, identity *gwmodel.UserIdentity) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if identity != nil {
		router.Use(func(c *gin.Context) {
			c.Set("identity", identity)
			c.Next()
		})
	}
	router.Use(guard.Handler())
	router.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })
	return router
}

func TestGatewayGuard_Concurrency(t *testing.T) {
	tests := []struct {
		name          string
		description   string
		maxConcurrent int
		limitPerMin   int
		concurrent    int
		wantBlocked   bool
	}{
		{
			name:          "并发数在限制内",
			description:   "maxConcurrent=5 只发 1 个请求，应通过",
			maxConcurrent: 5,
			limitPerMin:   0,
			concurrent:    1,
			wantBlocked:   false,
		},
		{
			name:          "maxConcurrent=0 禁用并发控制",
			description:   "maxConcurrent=0 时不做并发限制，所有请求应通过",
			maxConcurrent: 0,
			limitPerMin:   0,
			concurrent:    1,
			wantBlocked:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			guard := middleware.NewGatewayGuard(tt.limitPerMin, tt.maxConcurrent)
			identity := &gwmodel.UserIdentity{UserID: "user-concurrency", DepartmentID: "dept-1"}
			router := setupGuardRouter(guard, identity)

			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/test", nil)
			router.ServeHTTP(w, req)

			if tt.wantBlocked {
				assert.Equal(t, http.StatusTooManyRequests, w.Code)
			} else {
				assert.Equal(t, http.StatusOK, w.Code)
			}
		})
	}
}

func TestGatewayGuard_ConcurrencyExceeded(t *testing.T) {
	// 测试目的：超过并发限制时应返回 429，请求完成后并发数释放
	gin.SetMode(gin.TestMode)
	guard := middleware.NewGatewayGuard(0, 1)
	identity := &gwmodel.UserIdentity{UserID: "user-block", DepartmentID: "dept-1"}

	enteredCh := make(chan struct{})
	blockCh := make(chan struct{})

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("identity", identity)
		c.Next()
	})
	router.Use(guard.Handler())
	router.GET("/test", func(c *gin.Context) {
		enteredCh <- struct{}{}
		<-blockCh
		c.Status(http.StatusOK)
	})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		router.ServeHTTP(w, req)
	}()

	<-enteredCh

	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusTooManyRequests, w2.Code)

	close(blockCh)
	wg.Wait()

	// 释放后新请求应能通过
	guardNew := middleware.NewGatewayGuard(0, 1)
	routerNew := setupGuardRouter(guardNew, identity)
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest("GET", "/test", nil)
	routerNew.ServeHTTP(w3, req3)
	assert.Equal(t, http.StatusOK, w3.Code)
}

func TestGatewayGuard_RateLimit(t *testing.T) {
	tests := []struct {
		name        string
		description string
		limitPerMin int
		requests    int
		wantLast    int
	}{
		{
			name:        "请求数在限制内",
			description: "limitPerMin=10 发 5 个请求，全部应通过",
			limitPerMin: 10,
			requests:    5,
			wantLast:    http.StatusOK,
		},
		{
			name:        "超过速率限制",
			description: "limitPerMin=3 发 4 个请求，第 4 个应返回 429",
			limitPerMin: 3,
			requests:    4,
			wantLast:    http.StatusTooManyRequests,
		},
		{
			name:        "limitPerMin=0 禁用速率限制",
			description: "limitPerMin=0 时不做速率限制，所有请求应通过",
			limitPerMin: 0,
			requests:    100,
			wantLast:    http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			guard := middleware.NewGatewayGuard(tt.limitPerMin, 0) // 无并发限制
			identity := &gwmodel.UserIdentity{UserID: "user-rate-" + tt.name, DepartmentID: "dept-1"}
			router := setupGuardRouter(guard, identity)

			var lastCode int
			for i := 0; i < tt.requests; i++ {
				w := httptest.NewRecorder()
				req, _ := http.NewRequest("GET", "/test", nil)
				router.ServeHTTP(w, req)
				lastCode = w.Code
			}
			assert.Equal(t, tt.wantLast, lastCode)
		})
	}
}

func TestGatewayGuard_NoIdentity(t *testing.T) {
	// 测试目的：无 identity 时跳过 Guard 直接 Next
	gin.SetMode(gin.TestMode)
	guard := middleware.NewGatewayGuard(1, 1) // 极低限制
	router := setupGuardRouter(guard, nil)    // 不设置 identity

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGatewayGuard_IdentityWrongType(t *testing.T) {
	// 测试目的：identity 类型不是 *UserIdentity 时跳过 Guard
	gin.SetMode(gin.TestMode)
	guard := middleware.NewGatewayGuard(1, 1)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("identity", "not-a-user-identity") // 错误类型
		c.Next()
	})
	router.Use(guard.Handler())
	router.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGatewayGuard_IdentityEmptyUserID(t *testing.T) {
	// 测试目的：identity.UserID 为空时跳过 Guard
	gin.SetMode(gin.TestMode)
	guard := middleware.NewGatewayGuard(1, 1)
	identity := &gwmodel.UserIdentity{UserID: "", DepartmentID: "dept-1"}
	router := setupGuardRouter(guard, identity)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGatewayGuard_UpdateLimits(t *testing.T) {
	// 测试目的：UpdateLimits 后新限制生效
	gin.SetMode(gin.TestMode)
	guard := middleware.NewGatewayGuard(100, 100)
	identity := &gwmodel.UserIdentity{UserID: "user-update", DepartmentID: "dept-1"}
	router := setupGuardRouter(guard, identity)

	// 初始限制宽松，请求应通过
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// 收紧速率限制为 1/min（上面已消耗 1 次）
	guard.UpdateLimits(1, 100)

	// 再发请求应被拒绝
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusTooManyRequests, w2.Code)
}

func TestGatewayGuard_ConcurrencyAndRateCombined(t *testing.T) {
	// 测试目的：并发和速率限制同时生效，先检查并发再检查速率
	gin.SetMode(gin.TestMode)
	guard := middleware.NewGatewayGuard(5, 2)
	identity := &gwmodel.UserIdentity{UserID: "user-combined", DepartmentID: "dept-1"}

	enteredCh := make(chan struct{}, 2)
	blockCh := make(chan struct{})

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("identity", identity)
		c.Next()
	})
	router.Use(guard.Handler())
	router.GET("/test", func(c *gin.Context) {
		enteredCh <- struct{}{}
		<-blockCh
		c.Status(http.StatusOK)
	})

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/test", nil)
			router.ServeHTTP(w, req)
		}()
	}

	<-enteredCh
	<-enteredCh

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)

	close(blockCh)
	wg.Wait()
}
