package middleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/gateway/middleware"
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────────────
// Test runner helpers
// ─────────────────────────────────────────────────────────────────────────────

// setupRateLimiterRouter 构造一个跑 RateLimiter 中间件的 gin router。
// 成功走到底时返回 HTTP 200 + JSON body: {"reached": "handler"}
func setupRateLimiterRouter(rdb *redis.Client, staticCfg middleware.RateLimitConfig) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		// 注入默认 identity（可被测试覆盖）
		if _, ok := c.Get("identity"); !ok {
			c.Set("identity", &gwmodel.UserIdentity{
				KeyID:        "key-default",
				UserID:       "user-default",
				DepartmentID: "dept-d1",
				StudioID:     "studio-s1",
				KeyType:      "personal",
			})
		}
		c.Next()
	})
	router.Use(middleware.LayeredRateLimiter(rdb, func(c *gin.Context) middleware.RateLimitConfig {
		return staticCfg
	}))
	router.GET("/api", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"reached": "handler"})
	})
	return router
}

func callRouter(t *testing.T, router *gin.Engine) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// parseErrorResponse 解析 rate_limited 响应中的 error 字段
type rateLimitErrResp struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
		Param   string `json:"param"`
		Gate    string `json:"gate"`
	} `json:"error"`
}

func parseErrResp(w *httptest.ResponseRecorder) rateLimitErrResp {
	var resp rateLimitErrResp
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return resp
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: 跳过路径
// ─────────────────────────────────────────────────────────────────────────────

// TestRateLimiter_NilRDB 验证 rdb=nil 时直接放行。
func TestRateLimiter_NilRDB(t *testing.T) {
	router := setupRateLimiterRouter(nil, middleware.RateLimitConfig{GlobalRPM: 1})

	w := callRouter(t, router)

	assert.Equal(t, http.StatusOK, w.Code, "rdb=nil 应直接放行")
}

// TestRateLimiter_AllZeros 验证所有 RPM 配置为 0 时全部跳过限速检查。
func TestRateLimiter_AllZeros(t *testing.T) {
	rdb, mr := startMiniredis(t)
	router := setupRateLimiterRouter(rdb, middleware.RateLimitConfig{})

	// 多次请求应都不被拦
	for i := 0; i < 20; i++ {
		_ = callRouter(t, router)
	}

	assert.Equal(t, 0, len(mr.Keys()), "不跑 Lua 时 Redis 不应有写入")
}

// TestRateLimiter_RedisFailure_FailOpen 验证 Redis 不可用时 fail-open：返回 200 放行。
func TestRateLimiter_RedisFailure_FailOpen(t *testing.T) {
	rdb, mr := startMiniredis(t)
	router := setupRateLimiterRouter(rdb, middleware.RateLimitConfig{GlobalRPM: 1})
	// 关闭 Redis
	mr.Close()

	w := callRouter(t, router)

	assert.Equal(t, http.StatusOK, w.Code, "Redis fail 应 fail-open")
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: 单层限速 — 通过/拦截
// ─────────────────────────────────────────────────────────────────────────────

// TestRateLimiter_GlobalRPM_UnderLimit 验证未超限时放行。
func TestRateLimiter_GlobalRPM_UnderLimit(t *testing.T) {
	rdb, _ := startMiniredis(t)
	router := setupRateLimiterRouter(rdb, middleware.RateLimitConfig{GlobalRPM: 3})

	for i := 0; i < 3; i++ {
		w := callRouter(t, router)
		assert.Equal(t, http.StatusOK, w.Code, "第 %d 次请求应在限额内", i+1)
	}
}

// TestRateLimiter_GlobalRPM_AtLimit 验证最后一次配额刚好通过。
func TestRateLimiter_GlobalRPM_AtLimit(t *testing.T) {
	rdb, _ := startMiniredis(t)
	router := setupRateLimiterRouter(rdb, middleware.RateLimitConfig{GlobalRPM: 2})

	// 第 1、2 次通过
	for i := 0; i < 2; i++ {
		w := callRouter(t, router)
		assert.Equal(t, http.StatusOK, w.Code)
	}
	// 第 3 次应被拦
	w := callRouter(t, router)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)

	errResp := parseErrResp(w)
	assert.Equal(t, "rate_limited", errResp.Error.Type)
	assert.Equal(t, "rate_limited", errResp.Error.Code)
	assert.Equal(t, "全局", errResp.Error.Param)
	assert.Equal(t, "rate", errResp.Error.Gate)
	assert.Contains(t, errResp.Error.Message, "全局")
	assert.NotEmpty(t, w.Header().Get("Retry-After"))
}

// TestRateLimiter_StaleRequestsExpire 验证滑动窗口过期：60s 前的请求应不再计入限额。
func TestRateLimiter_StaleRequestsExpire(t *testing.T) {
	rdb, mr := startMiniredis(t)
	router := setupRateLimiterRouter(rdb, middleware.RateLimitConfig{GlobalRPM: 2})

	// 填满 2 次配额
	callRouter(t, router)
	callRouter(t, router)
	// 第 3 次被拦
	w1 := callRouter(t, router)
	assert.Equal(t, http.StatusTooManyRequests, w1.Code)

	// 时间快进 61 秒（窗口 60s）
	mr.FastForward(61 * time.Second)

	// 配额恢复
	w2 := callRouter(t, router)
	assert.Equal(t, http.StatusOK, w2.Code, "60s 后应恢复配额")
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: 多层限速 — 最严胜出
// ─────────────────────────────────────────────────────────────────────────────

// TestRateLimiter_StudioStricterThanGlobal 验证 Studio 层限额更严时，由 Studio 触发拦截。
func TestRateLimiter_StudioStricterThanGlobal(t *testing.T) {
	rdb, _ := startMiniredis(t)
	router := setupRateLimiterRouter(rdb, middleware.RateLimitConfig{
		GlobalRPM: 100, // 宽松
		StudioRPM: 2,   // 严格
	})

	// 填 2 次 studio 配额
	callRouter(t, router)
	callRouter(t, router)
	// 第 3 次触发 studio 拦截
	w := callRouter(t, router)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)

	errResp := parseErrResp(w)
	assert.Equal(t, "工作室", errResp.Error.Param, "应由 studio 层拦截，不是 global 层")
}

// TestRateLimiter_GlobalStricterThanStudio 验证 Global 更严时由 Global 触发。
func TestRateLimiter_GlobalStricterThanStudio(t *testing.T) {
	rdb, _ := startMiniredis(t)
	router := setupRateLimiterRouter(rdb, middleware.RateLimitConfig{
		GlobalRPM: 1,  // 严格
		StudioRPM: 50, // 宽松
	})

	callRouter(t, router)
	w := callRouter(t, router)

	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	errResp := parseErrResp(w)
	assert.Equal(t, "全局", errResp.Error.Param)
}

// TestRateLimiter_KeyLayer_PersonalKey_UsesUserID 验证个人 Key 按 UserID 计。
func TestRateLimiter_KeyLayer_PersonalKey_UsesUserID(t *testing.T) {
	rdb, _ := startMiniredis(t)
	router := setupRateLimiterRouter(rdb, middleware.RateLimitConfig{KeyRPM: 1})

	// identity user1 用第 1 次配额
	callRouter(t, router)
	// identity user1 第 2 次被拦
	w := callRouter(t, router)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	errResp := parseErrResp(w)
	assert.Equal(t, "Key", errResp.Error.Param)

	// 切换到 user2（同 KeyRPM 但独立桶）—— 关键：必须从头构造新 router
	// 不能用 setupRateLimiterRouter 后再 Use（那会在 rate limiter 之后覆盖 identity）
	gin.SetMode(gin.TestMode)
	router2 := gin.New()
	router2.Use(func(c *gin.Context) {
		c.Set("identity", &gwmodel.UserIdentity{
			UserID:  "user-2",
			KeyID:   "key-2",
			KeyType: "personal",
			StudioID:     "studio-s1",
			DepartmentID: "dept-d1",
		})
		c.Next()
	})
	router2.Use(middleware.LayeredRateLimiter(rdb, func(c *gin.Context) middleware.RateLimitConfig {
		return middleware.RateLimitConfig{KeyRPM: 1}
	}))
	router2.GET("/api", func(c *gin.Context) { c.JSON(200, gin.H{"reached": "handler"}) })

	w2 := callRouter(t, router2)
	assert.Equal(t, http.StatusOK, w2.Code, "不同 user 应有独立桶")
}

// TestRateLimiter_KeyLayer_ServiceKey_UsesKeyID 验证 Service Key 按 key_id 独立计，
// 不并入创建者 UserID 桶。
func TestRateLimiter_KeyLayer_ServiceKey_UsesKeyID(t *testing.T) {
	rdb, _ := startMiniredis(t)

	// 构造 Service Key identity
	setupServiceIdentity := func(router *gin.Engine, keyID, userID string) {
		router.Use(func(c *gin.Context) {
			c.Set("identity", &gwmodel.UserIdentity{
				KeyID:   keyID,
				UserID:  userID,
				KeyType: "service",
			})
			c.Next()
		})
	}

	// svc-key-1 用 2 次，KeyRPM=2
	router1 := gin.New()
	setupServiceIdentity(router1, "svc-key-1", "creator-user-1")
	router1.Use(middleware.LayeredRateLimiter(rdb, func(c *gin.Context) middleware.RateLimitConfig {
		return middleware.RateLimitConfig{KeyRPM: 2}
	}))
	router1.GET("/api", func(c *gin.Context) { c.JSON(200, nil) })

	callRouter(t, router1)
	callRouter(t, router1)
	w1 := callRouter(t, router1)
	assert.Equal(t, http.StatusTooManyRequests, w1.Code, "svc-key-1 已满配额应被拦")

	// 同一 creator 的另一个 service key (svc-key-2) — 独立桶
	router2 := gin.New()
	setupServiceIdentity(router2, "svc-key-2", "creator-user-1")
	router2.Use(middleware.LayeredRateLimiter(rdb, func(c *gin.Context) middleware.RateLimitConfig {
		return middleware.RateLimitConfig{KeyRPM: 2}
	}))
	router2.GET("/api", func(c *gin.Context) { c.JSON(200, nil) })

	w2 := callRouter(t, router2)
	assert.Equal(t, http.StatusOK, w2.Code, "svc-key-2 应有独立桶，不应受 svc-key-1 影响")
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: 配置项 0 = 不限
// ─────────────────────────────────────────────────────────────────────────────

// TestRateLimiter_ZeroConfig_SkipsLayer 验证 config 中某 RPM=0 表示跳过该层检查。
func TestRateLimiter_ZeroConfig_SkipsLayer(t *testing.T) {
	rdb, mr := startMiniredis(t)
	router := setupRateLimiterRouter(rdb, middleware.RateLimitConfig{
		GlobalRPM: 0, // 禁
		StudioRPM: 0, // 禁
		DeptRPM:   1, // 唯一启用
		KeyRPM:    0, // 禁
	})

	callRouter(t, router)
	w := callRouter(t, router)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	errResp := parseErrResp(w)
	assert.Equal(t, "部门", errResp.Error.Param, "应只有 dept 层生效")

	// 验证 Redis 只有 1 个 key（对应 dept 层）
	keys := mr.Keys()
	deptKeys := 0
	for _, k := range keys {
		if len(k) > 7 && k[:7] == "rl:dept" {
			deptKeys++
		}
	}
	require.Equal(t, 1, deptKeys, "应只有 rl:dept:* 1 个 key，不应有其他层")
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: JSON 错误响应格式
// ─────────────────────────────────────────────────────────────────────────────

// TestRateLimiter_ResponseBody 验证 429 响应体符合约定格式。
func TestRateLimiter_ResponseBody(t *testing.T) {
	rdb, _ := startMiniredis(t)
	router := setupRateLimiterRouter(rdb, middleware.RateLimitConfig{DeptRPM: 1})
	callRouter(t, router)
	w := callRouter(t, router)

	errResp := parseErrResp(w)
	assert.Equal(t, "rate_limited", errResp.Error.Type)
	assert.Equal(t, "rate_limited", errResp.Error.Code)
	assert.Equal(t, "rate", errResp.Error.Gate)
	assert.Contains(t, errResp.Error.Message, "RPM")
}
