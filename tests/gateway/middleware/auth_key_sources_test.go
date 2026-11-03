package middleware_test

// 入站客户端的 Key 传递方式有三种，缺一种就等于该协议的官方 SDK 不能用：
//   - OpenAI：Authorization: Bearer
//   - Anthropic：x-api-key
//   - Gemini：x-goog-api-key 或 ?key=（google-genai SDK 只会用这两种）
//
// 修复前 /v1beta 对 google-genai 完全不可用：SDK 发 x-goog-api-key，
// 而中间件只认前两种，一律 401。

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/gateway/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestAuthMiddleware_KeySources(t *testing.T) {
	const okJSON = `{"code":0,"message":"ok","data":{"status":"active","key_id":"k1","access_all_groups":true,"user":{"user_id":"u1","name":"n","department_id":"d"}}}`

	cases := []struct {
		name   string
		apply  func(*http.Request)
		want   int
		reason string
	}{
		{
			name:  "Authorization Bearer（OpenAI SDK）",
			apply: func(r *http.Request) { r.Header.Set("Authorization", "Bearer sk-openai") },
			want:  http.StatusOK,
		},
		{
			name:  "x-api-key（Anthropic SDK）",
			apply: func(r *http.Request) { r.Header.Set("x-api-key", "sk-anthropic") },
			want:  http.StatusOK,
		},
		{
			name:  "x-goog-api-key（google-genai SDK 默认）",
			apply: func(r *http.Request) { r.Header.Set("x-goog-api-key", "sk-gemini") },
			want:  http.StatusOK,
		},
		{
			name:  "?key= 查询参数（Gemini 另一种写法）",
			apply: func(r *http.Request) { r.URL.RawQuery = "key=sk-gemini-q" },
			want:  http.StatusOK,
		},
		{
			name: "Bearer 优先于 x-goog-api-key",
			apply: func(r *http.Request) {
				r.Header.Set("Authorization", "Bearer sk-openai")
				r.Header.Set("x-goog-api-key", "sk-gemini")
			},
			want: http.StatusOK,
		},
		{
			name:  "完全没有 Key → 401",
			apply: func(r *http.Request) {},
			want:  http.StatusUnauthorized,
		},
		{
			name:  "非 Bearer 的 Authorization 且无其他来源 → 401",
			apply: func(r *http.Request) { r.Header.Set("Authorization", "Basic abc") },
			want:  http.StatusUnauthorized,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)

			srv := fakeAuthServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(okJSON))
			})
			defer srv.Close()

			router := gin.New()
			router.Use(middleware.AuthMiddleware(newAuthClient(srv.URL), testJWTManager()))
			router.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/test", nil)
			tc.apply(req)
			router.ServeHTTP(w, req)

			assert.Equal(t, tc.want, w.Code, tc.name)
		})
	}
}
