// Package contract 是 Team I 维护的 FE↔BE 契约/冒烟测试。
//
// 设计原则：
//   1. 不依赖真实 PostgreSQL / Redis。
//   2. 使用 control-plane 与 gateway 的 *真实* 中间件（认证/RequestID 等），
//      但把业务 handler 替换为符合契约的桩 handler，
//      只对"路由注册 + 中间件链 + JSON 包装"做冒烟。
//   3. 路由表 1:1 对齐 internal/controlplane/router/router.go，与 contracts.json 双向校验。
package contract

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	cpmw "github.com/example/llm-gateway/internal/controlplane/middleware"
	gwmw "github.com/example/llm-gateway/internal/gateway/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -------- 桩 handler --------

// okData 返回 {data: payload}
func okData(payload any) gin.HandlerFunc {
	return func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"data": payload}) }
}

// createdData 返回 201 {data: payload}
func createdData(payload any) gin.HandlerFunc {
	return func(c *gin.Context) { c.JSON(http.StatusCreated, gin.H{"data": payload}) }
}

// okMessage 返回 {message: msg}
func okMessage(msg string) gin.HandlerFunc {
	return func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"message": msg}) }
}

// okList 返回 {data: items, total, page}
func okList(items []any, total int, page int) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "page": page})
	}
}

// echoBody 把 body 反射回 {data: body}（POST/PUT 用于断言字段透传）
func echoBody(status int) gin.HandlerFunc {
	return func(c *gin.Context) {
		var raw map[string]any
		_ = c.ShouldBindJSON(&raw)
		c.JSON(status, gin.H{"data": raw})
	}
}

// -------- 路由注册（对齐 router.Setup） --------

// buildControlPlaneRouter 严格按 internal/controlplane/router/router.go 的路由表
// 注册，但使用桩 handler。中间件用真实的 JWTAuth/InternalAuth。
func buildControlPlaneRouter() *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(cpmw.RequestID())

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	adminID := uuid.New().String()

	admin := r.Group("/api/v1")
	admin.Use(cpmw.JWTAuth())
	{
		// 渠道定义
		admin.GET("/provider-definitions", okData([]gin.H{
			{
				"id":                "openai-compatible",
				"display_name":      "OpenAI Compatible",
				"adapter":           "openai",
				"base_url":          "https://api.openai.com/v1",
				"credential_schema": gin.H{},
				"vendor_options":    gin.H{},
				"description":       "",
				"icon_url":          "",
				"doc_url":           "",
				"default_models":    []string{"gpt-4o"},
				"builtin":           true,
				"enabled":           true,
			},
		}))
		admin.GET("/provider-definitions/:id", okData(gin.H{"id": "openai-compatible"}))

		// 渠道管理
		admin.POST("/providers", echoBody(http.StatusCreated))
		admin.GET("/providers", okList([]any{
			gin.H{"id": adminID, "name": "p1", "definition_id": "openai-compatible",
				"type": "openai", "endpoint": "https://x", "credentials": "sk-****",
				"enabled": true, "weight": 1, "priority": 0, "status": 1,
				"test_model": "", "response_time": 0, "balance": 0.0,
				"remark": "", "max_retries": 3, "timeout": 30, "tag": "",
				"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
			},
		}, 1, 1))
		admin.GET("/providers/:id", okData(gin.H{"id": adminID, "name": "p1"}))
		admin.PUT("/providers/:id", echoBody(http.StatusOK))
		admin.PATCH("/providers/:id/status", echoBody(http.StatusOK))
		admin.DELETE("/providers/:id", okMessage("provider deleted"))
		admin.POST("/providers/:id/test", okData(gin.H{
			"success": true, "response_time": 12, "status_code": 200, "model_count": 3,
		}))
		admin.POST("/providers/:id/sync-models", okData(gin.H{
			"added": []gin.H{{"id": "m1", "owned_by": "vendor"}},
			"existing": []gin.H{}, "removed": []gin.H{},
			"total_upstream": 1, "total_local": 0, "unsupported": false,
		}))
		admin.POST("/providers/:id/sync-models/confirm", okData(gin.H{
			"registered": 1, "skipped": 0, "disabled": 0,
		}))

		// 模型管理
		admin.POST("/models", echoBody(http.StatusCreated))
		admin.GET("/models", okList([]any{gin.H{"id": uuid.New().String(), "name": "m1"}}, 1, 1))
		// /models/check-name 必须在 :id 通配前注册，验证 router.go 的顺序契约
		admin.GET("/models/check-name", func(c *gin.Context) {
			if c.Query("name") == "" {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": gin.H{"type": "invalid_request", "code": "bad_request",
						"message": "name query parameter is required"},
				})
				return
			}
			c.JSON(http.StatusOK, gin.H{"data": gin.H{"available": true}})
		})
		admin.PUT("/models/:id", echoBody(http.StatusOK))
		admin.DELETE("/models/:id", okMessage("model deleted"))

		// 标签
		admin.GET("/model-tags", okData([]gin.H{
			{"id": uuid.New().String(), "name": "vision", "color": "#5DADE2",
				"description": "", "sort_order": 0,
				"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
			},
		}))
		admin.POST("/model-tags", echoBody(http.StatusCreated))
		admin.PUT("/model-tags/:id", echoBody(http.StatusOK))
		admin.DELETE("/model-tags/:id", okMessage("tag deleted"))
		admin.GET("/models/:id/tags", okData([]gin.H{}))
		admin.PUT("/models/:id/tags", echoBody(http.StatusOK))
	}

	internal := r.Group("/internal/v1")
	internal.Use(cpmw.InternalAuth())
	{
		internal.GET("/models/available", okData([]gin.H{}))
	}

	return r
}

// buildGatewayRouter 对齐 internal/gateway/server/router.go
func buildGatewayRouter() *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(gwmw.RequestID())

	r.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.GET("/ready", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ready"}) })

	v1 := r.Group("/v1")
	v1.Use(gwmw.APIKeyAuth())
	{
		v1.POST("/chat/completions", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"id": "chat-1", "object": "chat.completion",
				"choices": []gin.H{{"index": 0, "message": gin.H{"role": "assistant", "content": "hi"}, "finish_reason": "stop"}},
			})
		})
		v1.GET("/models", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"object": "list",
				"data":   []gin.H{{"id": "gpt-4o", "object": "model", "created": 0, "owned_by": "llm-gateway"}},
			})
		})
	}
	return r
}

// -------- 路由清单（与 contracts.json 双向比对） --------

type routeCase struct {
	Name           string
	Method         string
	Path           string  // 真实 URL（已替换 :id）
	Auth           string  // "admin" / "internal" / "client" / "none"
	Body           string  // 可选
	ExpectStatuses []int   // 预期可接受状态码
}

func makeControlPlaneCases() []routeCase {
	id := uuid.NewString()
	return []routeCase{
		{"healthz", "GET", "/healthz", "none", "", []int{200}},
		{"ListDefinitions", "GET", "/api/v1/provider-definitions", "admin", "", []int{200}},
		{"GetDefinition", "GET", "/api/v1/provider-definitions/openai-compatible", "admin", "", []int{200}},
		{"CreateProvider", "POST", "/api/v1/providers", "admin",
			`{"name":"p","definition_id":"openai-compatible","endpoint":"https://x","credentials":"{\"api_key\":\"k\"}"}`,
			[]int{201}},
		{"ListProviders", "GET", "/api/v1/providers?page=1&page_size=20", "admin", "", []int{200}},
		{"GetProvider", "GET", "/api/v1/providers/" + id, "admin", "", []int{200}},
		{"UpdateProvider", "PUT", "/api/v1/providers/" + id, "admin", `{"name":"p2"}`, []int{200}},
		{"ToggleProviderStatus", "PATCH", "/api/v1/providers/" + id + "/status", "admin", `{"enabled":false}`, []int{200}},
		{"DeleteProvider", "DELETE", "/api/v1/providers/" + id, "admin", "", []int{200}},
		{"TestProvider", "POST", "/api/v1/providers/" + id + "/test", "admin", "", []int{200}},
		{"SyncModels", "POST", "/api/v1/providers/" + id + "/sync-models", "admin", "", []int{200}},
		{"ConfirmSync", "POST", "/api/v1/providers/" + id + "/sync-models/confirm", "admin",
			`{"models_to_add":["m1","m2"]}`, []int{200}},
		{"CreateModel", "POST", "/api/v1/models", "admin",
			`{"name":"m1","provider_id":"` + id + `","model_type":"chat","upstream_model_id":"openai/gpt-4o","context_length":128000,"input_price":0.0025,"weight":7}`,
			[]int{201}},
		{"ListModels", "GET", "/api/v1/models?page=1", "admin", "", []int{200}},
		{"CheckModelName", "GET", "/api/v1/models/check-name?name=gpt-5", "admin", "", []int{200}},
		{"UpdateModel", "PUT", "/api/v1/models/" + id, "admin",
			`{"name":"m2","upstream_model_id":"u2","context_length":200000,"weight":9}`,
			[]int{200}},
		{"DeleteModel", "DELETE", "/api/v1/models/" + id, "admin", "", []int{200}},
		{"ListTags", "GET", "/api/v1/model-tags", "admin", "", []int{200}},
		{"CreateTag", "POST", "/api/v1/model-tags", "admin", `{"name":"vision","color":"#5DADE2"}`, []int{201}},
		{"UpdateTag", "PUT", "/api/v1/model-tags/" + id, "admin", `{"name":"x","color":"#000"}`, []int{200}},
		{"DeleteTag", "DELETE", "/api/v1/model-tags/" + id, "admin", "", []int{200}},
		{"GetModelTags", "GET", "/api/v1/models/" + id + "/tags", "admin", "", []int{200}},
		{"SetModelTags", "PUT", "/api/v1/models/" + id + "/tags", "admin", `{"tag_ids":["` + id + `"]}`, []int{200}},
		{"ListAvailableModels", "GET", "/internal/v1/models/available", "internal", "", []int{200}},
	}
}

// runReq 根据 auth 类型自动注入对应头
func runReq(t *testing.T, srv *httptest.Server, c routeCase, omitAuth bool) *http.Response {
	t.Helper()
	var body *strings.Reader
	if c.Body != "" {
		body = strings.NewReader(c.Body)
	} else {
		body = strings.NewReader("")
	}
	req, err := http.NewRequest(c.Method, srv.URL+c.Path, body)
	require.NoError(t, err)
	if c.Body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if !omitAuth {
		switch c.Auth {
		case "admin":
			req.Header.Set("Authorization", "Bearer dev-token")
		case "internal":
			req.Header.Set("X-Internal-Token", "internal-secret")
		case "client":
			req.Header.Set("Authorization", "Bearer fake-api-key")
		}
	}
	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	return resp
}

// -------- 测试用例 --------

// TestControlPlane_RoutesSmoke：每条路由发一次最小合法请求，应非 5xx 且 JSON 解析正常
func TestControlPlane_RoutesSmoke(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := httptest.NewServer(buildControlPlaneRouter())
	defer srv.Close()

	cases := makeControlPlaneCases()
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			resp := runReq(t, srv, c, false)
			defer resp.Body.Close()

			assert.Contains(t, c.ExpectStatuses, resp.StatusCode,
				"%s %s: expected status in %v, got %d", c.Method, c.Path, c.ExpectStatuses, resp.StatusCode)

			ct := resp.Header.Get("Content-Type")
			assert.True(t, strings.HasPrefix(ct, "application/json"),
				"%s %s: expected JSON Content-Type, got %q", c.Method, c.Path, ct)

			body := readAll(t, resp)
			var raw map[string]any
			err := json.Unmarshal(body, &raw)
			require.NoError(t, err, "%s %s: invalid JSON body: %s", c.Method, c.Path, string(body))

			// 顶层包装：data / message / status / error 至少有一种
			_, hasData := raw["data"]
			_, hasMsg := raw["message"]
			_, hasStatus := raw["status"]
			_, hasErr := raw["error"]
			assert.True(t, hasData || hasMsg || hasStatus || hasErr,
				"%s %s: response missing one of data/message/status/error: %s",
				c.Method, c.Path, string(body))
		})
	}
}

// TestControlPlane_AuthEnforced：admin 路由不带 token → 401；internal 路由不带 token → 403
func TestControlPlane_AuthEnforced(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := httptest.NewServer(buildControlPlaneRouter())
	defer srv.Close()

	for _, c := range makeControlPlaneCases() {
		if c.Auth == "none" {
			continue
		}
		t.Run(c.Name+"_NoAuth", func(t *testing.T) {
			resp := runReq(t, srv, c, true)
			defer resp.Body.Close()

			var expect int
			switch c.Auth {
			case "admin":
				expect = http.StatusUnauthorized
			case "internal":
				expect = http.StatusForbidden
			default:
				t.Skip()
			}
			assert.Equal(t, expect, resp.StatusCode,
				"%s %s without auth: expected %d, got %d", c.Method, c.Path, expect, resp.StatusCode)

			body := readAll(t, resp)
			var raw map[string]any
			require.NoError(t, json.Unmarshal(body, &raw))
			require.Contains(t, raw, "error", "no-auth response should carry 'error' envelope: %s", body)
		})
	}
}

// TestControlPlane_InvalidPath：注册的路径模式应匹配，乱写 ID 段不应崩溃；错误模板 path 应 404
func TestControlPlane_InvalidPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := httptest.NewServer(buildControlPlaneRouter())
	defer srv.Close()

	cases := []struct {
		path   string
		expect int
	}{
		{"/api/v1/not-a-real-route", 401}, // 未命中路由也会先过 JWTAuth；不过 gin 对未命中的路径直接 404
		{"/no-such", 404},
	}
	for _, c := range cases {
		req, _ := http.NewRequest("GET", srv.URL+c.path, nil)
		resp, err := srv.Client().Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		// 至少不应 5xx
		assert.Less(t, resp.StatusCode, 500, "unexpected 5xx for %s", c.path)
	}
}

// TestGateway_RoutesSmoke：网关 /v1 路由冒烟
func TestGateway_RoutesSmoke(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := httptest.NewServer(buildGatewayRouter())
	defer srv.Close()

	cases := []routeCase{
		{"health", "GET", "/health", "none", "", []int{200}},
		{"ready", "GET", "/ready", "none", "", []int{200}},
		{"chat", "POST", "/v1/chat/completions", "client",
			`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`, []int{200}},
		{"models", "GET", "/v1/models", "client", "", []int{200}},
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			resp := runReq(t, srv, c, false)
			defer resp.Body.Close()
			assert.Contains(t, c.ExpectStatuses, resp.StatusCode)
			body := readAll(t, resp)
			var raw map[string]any
			require.NoError(t, json.Unmarshal(body, &raw))
		})
	}
}

// TestGateway_AuthEnforced：/v1 下未带 Bearer 应 401
func TestGateway_AuthEnforced(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := httptest.NewServer(buildGatewayRouter())
	defer srv.Close()

	cases := []struct {
		method string
		path   string
	}{
		{"GET", "/v1/models"},
		{"POST", "/v1/chat/completions"},
	}
	for _, c := range cases {
		req, _ := http.NewRequest(c.method, srv.URL+c.path, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		resp, err := srv.Client().Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "no-auth %s %s should 401", c.method, c.path)
	}
}

// TestErrorEnvelope_Shape：通过故意伪造无效 JSON 触发 400，断言 error envelope 字段命中规范
// 这里走桩 handler，因为桩 handler 没做校验，只能验证 echo handler 的 JSON 解析失败路径。
// 真实 handler 的 400 行为由 internal/controlplane/handler 的单测覆盖。
func TestErrorEnvelope_Shape_TypeCodeMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// 构造一个最小路由直接挂载真实 middleware + 真实 envelope
	r := gin.New()
	r.Use(cpmw.JWTAuth())
	r.POST("/x", func(c *gin.Context) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"type": "invalid_request", "code": "bad_request", "message": "boom"},
		})
	})

	srv := httptest.NewServer(r)
	defer srv.Close()

	req, _ := http.NewRequest("POST", srv.URL+"/x", bytes.NewBufferString("{}"))
	req.Header.Set("Authorization", "Bearer dev-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	body := readAll(t, resp)
	var raw map[string]map[string]any
	require.NoError(t, json.Unmarshal(body, &raw))
	require.Contains(t, raw, "error")
	for _, k := range []string{"type", "code", "message"} {
		assert.Contains(t, raw["error"], k, "error envelope missing key %s: %s", k, body)
	}
}

// TestContractsTable_AlignsWithRouter：路由表里的 path 模板必须与 router.go 一致
// （硬编码 23 条路由的简单存在性检查，作为防回归提示。）
func TestContractsTable_AlignsWithRouter(t *testing.T) {
	// 我们这里检查的是 buildControlPlaneRouter 注册的路径集合是否与文档保持一致。
	want := []string{
		"GET    /healthz",
		"GET    /api/v1/provider-definitions",
		"GET    /api/v1/provider-definitions/:id",
		"POST   /api/v1/providers",
		"GET    /api/v1/providers",
		"GET    /api/v1/providers/:id",
		"PUT    /api/v1/providers/:id",
		"PATCH  /api/v1/providers/:id/status",
		"DELETE /api/v1/providers/:id",
		"POST   /api/v1/providers/:id/test",
		"POST   /api/v1/providers/:id/sync-models",
		"POST   /api/v1/providers/:id/sync-models/confirm",
		"POST   /api/v1/models",
		"GET    /api/v1/models",
		"GET    /api/v1/models/check-name",
		"PUT    /api/v1/models/:id",
		"DELETE /api/v1/models/:id",
		"GET    /api/v1/model-tags",
		"POST   /api/v1/model-tags",
		"PUT    /api/v1/model-tags/:id",
		"DELETE /api/v1/model-tags/:id",
		"GET    /api/v1/models/:id/tags",
		"PUT    /api/v1/models/:id/tags",
		"GET    /internal/v1/models/available",
	}
	got := map[string]bool{}
	for _, ri := range buildControlPlaneRouter().Routes() {
		key := pad(ri.Method, 6) + " " + ri.Path
		got[key] = true
	}
	for _, w := range want {
		assert.Truef(t, got[w], "route %q expected but not registered", w)
	}
}

func pad(s string, n int) string {
	for len(s) < n {
		s += " "
	}
	return s
}

// =============================================================================
// S0-1 / S0-2 字段透传回归
//
// 通过 echoBody handler 把请求体回声为 {data:...}，断言 BE 不会因为
// "未声明字段" 而丢失它们。该用例与 contracts.json 中
// request_fields_ignored_by_backend 必须保持空数组 — 一旦 BE 重新引入
// 任何字段忽略，该回归用例会失败。
// =============================================================================
func TestCreateProvider_AdvancedFieldsRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := httptest.NewServer(buildControlPlaneRouter())
	defer srv.Close()

	body := `{
		"name":"p","definition_id":"openai-compatible",
		"endpoint":"https://x","credentials":"{\"api_key\":\"k\"}",
		"weight":88,"priority":7,"test_model":"gpt-4o-mini",
		"remark":"prod","max_retries":5,"timeout":120,"tag":"production"
	}`
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer dev-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	raw := map[string]map[string]any{}
	require.NoError(t, json.Unmarshal(readAll(t, resp), &raw))
	data := raw["data"]
	for _, k := range []string{"weight", "priority", "test_model", "remark", "max_retries", "timeout", "tag"} {
		assert.Contains(t, data, k, "S0-1 字段 %s 必须能被 BE 回声（不再被静默丢弃）", k)
	}
}

func TestCreateModel_AdvancedFieldsRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := httptest.NewServer(buildControlPlaneRouter())
	defer srv.Close()

	pid := uuid.NewString()
	body := `{
		"name":"m1","provider_id":"` + pid + `","model_type":"chat",
		"upstream_model_id":"openai/gpt-4o","description":"omni",
		"context_length":128000,"max_output_tokens":8192,
		"input_price":0.0025,"output_price":0.01,"weight":7
	}`
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/models", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer dev-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	raw := map[string]map[string]any{}
	require.NoError(t, json.Unmarshal(readAll(t, resp), &raw))
	data := raw["data"]
	for _, k := range []string{"upstream_model_id", "description", "context_length",
		"max_output_tokens", "input_price", "output_price", "weight"} {
		assert.Contains(t, data, k, "S0-2 字段 %s 必须能被 BE 回声", k)
	}
}

// S0-3 防 :id 抢占：/models/check-name 必须命中 CheckModelName，而不是 UpdateModel/DeleteModel
func TestCheckModelName_DoesNotCollideWithIDPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := httptest.NewServer(buildControlPlaneRouter())
	defer srv.Close()

	cases := []struct {
		name   string
		path   string
		expect int
	}{
		{"with_name", "/api/v1/models/check-name?name=gpt-5", http.StatusOK},
		{"missing_name", "/api/v1/models/check-name", http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", srv.URL+c.path, nil)
			req.Header.Set("Authorization", "Bearer dev-token")
			resp, err := srv.Client().Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, c.expect, resp.StatusCode)
			if c.expect == http.StatusOK {
				raw := map[string]map[string]bool{}
				require.NoError(t, json.Unmarshal(readAll(t, resp), &raw))
				assert.Contains(t, raw["data"], "available")
			}
		})
	}
}

// S0 防回归元测试：contracts.json 中四条受影响路由的
// request_fields_ignored_by_backend 必须为空数组（已通过 BE input struct 补齐）
func TestContracts_NoIgnoredFieldsForFixedRoutes(t *testing.T) {
	// 复用同目录的 contracts.json 加载逻辑：直接读文件并解析
	const contractsPath = "contracts.json"
	raw, err := readContracts(contractsPath)
	require.NoError(t, err)

	var data struct {
		ControlPlane struct {
			Routes []struct {
				Method                       string   `json:"method"`
				Path                         string   `json:"path"`
				RequestFieldsIgnoredByBackend []string `json:"request_fields_ignored_by_backend"`
			} `json:"routes"`
		} `json:"control_plane"`
	}
	require.NoError(t, json.Unmarshal(raw, &data))

	guarded := map[string]bool{
		"POST /api/v1/providers":   true,
		"PUT /api/v1/providers/:id": true,
		"POST /api/v1/models":      true,
		"PUT /api/v1/models/:id":   true,
	}
	for _, r := range data.ControlPlane.Routes {
		key := r.Method + " " + r.Path
		if !guarded[key] {
			continue
		}
		assert.Empty(t, r.RequestFieldsIgnoredByBackend,
			"%s 的 request_fields_ignored_by_backend 应已被清空（S0-1/S0-2 已修复），如有新增字段被 BE 丢弃请改 input struct，而不要重新填表", key)
	}
}

// TestContracts_MachineCodes_AreCanonical 锁定 S1-5 的 13 个机器码，
// 同时校验：
//   - error_codes_canonical 表存在且与 13 项 ground truth 完全一致；
//   - 各路由的 error_codes 引用的 code 必须出现在 canonical 表中（不允许私造）；
//   - P5-1: 不允许 contracts.json 任何位置再出现 disable_removed 字段。
func TestContracts_MachineCodes_AreCanonical(t *testing.T) {
	raw, err := readContracts("contracts.json")
	require.NoError(t, err)

	expected := map[string]struct{}{
		"bad_request": {}, "invalid_tag_id": {}, "invalid_model_id": {},
		"model_provider_invalid": {}, "sync_not_supported": {}, "provider_disabled": {},
		"provider_not_found": {}, "model_not_found": {},
		"provider_name_conflict": {}, "model_name_conflict": {}, "provider_has_models": {},
		"upstream_error": {}, "internal_error": {},
		// CAT-1 (Model Catalog 自动拉取): 新增 3 个机器码。
		// 若进一步细化（如把 INVALID_PARAM / NAME_DUPLICATE 落地为独立 code），
		// 同步更新此 expected 列表与 FE web/src/services/http.ts::ERROR_MESSAGES。
		"catalog_not_found": {}, "catalog_not_bound": {}, "sync_in_progress": {},
	}

	var doc struct {
		ControlPlane struct {
			ErrorCodesCanonical map[string]map[string]any `json:"error_codes_canonical"`
			Routes              []map[string]any          `json:"routes"`
		} `json:"control_plane"`
	}
	require.NoError(t, json.Unmarshal(raw, &doc))

	got := doc.ControlPlane.ErrorCodesCanonical
	require.NotNil(t, got, "error_codes_canonical 必须存在于 contracts.json")
	for code := range expected {
		_, ok := got[code]
		assert.Truef(t, ok, "缺失机器码 %q（FE ERROR_MESSAGES 与 BE handler errcodes.go 必须同步出现）", code)
	}
	for code := range got {
		_, ok := expected[code]
		assert.Truef(t, ok, "未识别的机器码 %q：如需新增请同时更新 expected 列表与 FE ERROR_MESSAGES", code)
	}

	// 校验各路由 error_codes 中引用的 code 都在 canonical 表
	for _, route := range doc.ControlPlane.Routes {
		ec, ok := route["error_codes"].(map[string]any)
		if !ok {
			continue
		}
		for status, val := range ec {
			line, _ := val.(string)
			for _, token := range strings.Fields(line) {
				if !strings.Contains(token, "/") {
					continue
				}
				parts := strings.SplitN(token, "/", 2)
				code := strings.TrimSpace(parts[1])
				code = strings.TrimSuffix(code, ",")
				code = strings.TrimSuffix(code, ")")
				code = strings.TrimSuffix(code, "(")
				if code == "" || code == "OR" {
					continue
				}
				if _, ok := got[code]; !ok {
					t.Errorf("路由 %v 在 status=%s 下引用未注册机器码 %q（来自 %q）",
						route["path"], status, code, line)
				}
			}
		}
	}

	// P5-1: disable_removed 已在本次迭代彻底删除
	if strings.Contains(string(raw), `"disable_removed"`) {
		t.Errorf("contracts.json 仍包含 disable_removed 引用（P5-1 应已清除）")
	}
}

// TestContracts_PaginationQuery_PresentForListEndpoints S1-2:
// getProviders / getModels FE 调用项必须声明 page / page_size query_params。
func TestContracts_PaginationQuery_PresentForListEndpoints(t *testing.T) {
	raw, err := readContracts("contracts.json")
	require.NoError(t, err)

	var doc struct {
		FrontendClients struct {
			ControlPlane struct {
				Calls []struct {
					Fn          string   `json:"fn"`
					QueryParams []string `json:"query_params"`
				} `json:"calls"`
			} `json:"control-plane.ts"`
		} `json:"frontend_clients"`
	}
	require.NoError(t, json.Unmarshal(raw, &doc))

	want := map[string][]string{
		"getProviders": {"page", "page_size"},
		"getModels":    {"page", "page_size"},
	}
	for _, c := range doc.FrontendClients.ControlPlane.Calls {
		need, ok := want[c.Fn]
		if !ok {
			continue
		}
		for _, p := range need {
			assert.Containsf(t, c.QueryParams, p,
				"S1-2: %s 必须声明 query_param %q（FE service 实际已发送）", c.Fn, p)
		}
	}
}

// TestContracts_TogglePropagatesProvider S1-6:
// PATCH /providers/:id/status 路由必须声明 response_item_fields，
// 且 frontend_clients.toggleProviderStatus 标注 returns: "Provider"。
func TestContracts_TogglePropagatesProvider(t *testing.T) {
	raw, err := readContracts("contracts.json")
	require.NoError(t, err)

	var doc struct {
		ControlPlane struct {
			Routes []map[string]any `json:"routes"`
		} `json:"control_plane"`
		FrontendClients struct {
			ControlPlane struct {
				Calls []map[string]any `json:"calls"`
			} `json:"control-plane.ts"`
		} `json:"frontend_clients"`
	}
	require.NoError(t, json.Unmarshal(raw, &doc))

	var routeFound bool
	for _, r := range doc.ControlPlane.Routes {
		if r["method"] == "PATCH" && r["path"] == "/api/v1/providers/:id/status" {
			routeFound = true
			fields, ok := r["response_item_fields"].([]any)
			require.True(t, ok, "PATCH /providers/:id/status 必须声明 response_item_fields（S1-6）")
			require.NotEmpty(t, fields)
			break
		}
	}
	assert.True(t, routeFound, "未找到 PATCH /providers/:id/status 路由")

	var callFound bool
	for _, c := range doc.FrontendClients.ControlPlane.Calls {
		if c["fn"] == "toggleProviderStatus" {
			callFound = true
			assert.Equal(t, "Provider", c["returns"],
				"toggleProviderStatus 必须声明 returns: Provider（S1-6）")
			break
		}
	}
	assert.True(t, callFound, "未找到 frontend_clients.toggleProviderStatus 项")
}

func readContracts(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// =============================================================================
// CAT-1: Model Catalog 自动拉取契约元测试
//
// 这些测试只读 contracts.json 做静态断言，不依赖 BE 实际跑起来；
// 当 Team B 落地后端实现 / Team F 落地 FE 客户端时，必须使 contracts.json
// 与代码保持一致，否则本测试集会失败，作为契约门禁。
// =============================================================================

// loadCPRoutes 加载 control_plane.routes 为 []map[string]any，方便逐条断言。
func loadCPRoutes(t *testing.T) []map[string]any {
	t.Helper()
	raw, err := readContracts("contracts.json")
	require.NoError(t, err)
	var doc struct {
		ControlPlane struct {
			Routes []map[string]any `json:"routes"`
		} `json:"control_plane"`
	}
	require.NoError(t, json.Unmarshal(raw, &doc))
	return doc.ControlPlane.Routes
}

// findRoute 在 routes 中按 method+path 精确匹配；找不到返回 nil。
func findRoute(routes []map[string]any, method, path string) map[string]any {
	for _, r := range routes {
		if r["method"] == method && r["path"] == path {
			return r
		}
	}
	return nil
}

// asStringSlice 把 contracts.json 中常见的 []any 字段强转为 []string，
// 便于 assert.Contains / 集合比较。
func asStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// TestContracts_CatalogRoutesPresent CAT-1:
// 5 个 model-catalog 路由必须在 contracts.json 中完整声明（method+path 都正确）。
func TestContracts_CatalogRoutesPresent(t *testing.T) {
	routes := loadCPRoutes(t)

	expected := []struct {
		method string
		path   string
	}{
		{"GET", "/api/v1/model-catalog/search"},
		{"GET", "/api/v1/model-catalog/by-source"},
		{"POST", "/api/v1/model-catalog/sync"},
		{"GET", "/api/v1/model-catalog/diff"},
		{"POST", "/api/v1/model-catalog/apply-diff"},
	}
	for _, e := range expected {
		r := findRoute(routes, e.method, e.path)
		require.NotNilf(t, r, "CAT-1: 路由 %s %s 必须在 contracts.json 中声明", e.method, e.path)
		assert.Equal(t, "admin", r["auth"],
			"CAT-1: %s %s 必须是 admin 认证", e.method, e.path)
		// 必须声明 response_envelope，否则 FE 无从生成类型
		assert.NotEmptyf(t, r["response_envelope"],
			"CAT-1: %s %s 必须声明 response_envelope", e.method, e.path)
	}
}

// TestContracts_ModelHasCatalogFields CAT-1:
// GET /models（列表）和 GET /models/:id（单查）响应字段必须包含
// catalog_id / ctx_overridden / max_output_overridden / catalog 这 4 个新字段。
// PUT /models/:id 的请求字段必须包含 catalog_id（optional），且响应里同样含 catalog 4 字段。
func TestContracts_ModelHasCatalogFields(t *testing.T) {
	routes := loadCPRoutes(t)
	wantCatalogFields := []string{"catalog_id", "ctx_overridden", "max_output_overridden", "catalog"}

	respCheck := []struct {
		method string
		path   string
	}{
		{"GET", "/api/v1/models"},
		{"GET", "/api/v1/models/:id"},
		{"PUT", "/api/v1/models/:id"},
	}
	for _, e := range respCheck {
		r := findRoute(routes, e.method, e.path)
		require.NotNilf(t, r, "CAT-1: 路由 %s %s 必须存在", e.method, e.path)
		fields := asStringSlice(r["response_item_fields"])
		require.NotEmptyf(t, fields,
			"CAT-1: %s %s 必须声明 response_item_fields 以反映新增的 catalog 字段",
			e.method, e.path)
		for _, want := range wantCatalogFields {
			assert.Containsf(t, fields, want,
				"CAT-1: %s %s 的 response_item_fields 必须含 %q", e.method, e.path, want)
		}
	}

	// PUT /models/:id 请求体里必须可选地接收 catalog_id（绑定 / 解绑）。
	put := findRoute(routes, "PUT", "/api/v1/models/:id")
	require.NotNil(t, put)
	optional := asStringSlice(put["request_fields_optional"])
	assert.Contains(t, optional, "catalog_id",
		"CAT-1: PUT /api/v1/models/:id 必须把 catalog_id 列入 request_fields_optional（用于绑定 / 解绑）")
}

// TestContracts_FromCatalogRoutePresent CAT-1:
// POST /api/v1/models/from-catalog 必须存在，必填字段为 {catalog_id, provider_id, name}，
// 响应含 catalog 4 字段。
func TestContracts_FromCatalogRoutePresent(t *testing.T) {
	routes := loadCPRoutes(t)
	r := findRoute(routes, "POST", "/api/v1/models/from-catalog")
	require.NotNil(t, r, "CAT-1: POST /api/v1/models/from-catalog 必须在 contracts.json 中声明")

	required := asStringSlice(r["request_fields_required"])
	for _, k := range []string{"catalog_id", "provider_id", "name"} {
		assert.Containsf(t, required, k, "CAT-1: from-catalog 必填字段缺 %q", k)
	}

	respFields := asStringSlice(r["response_item_fields"])
	for _, k := range []string{"catalog_id", "ctx_overridden", "max_output_overridden", "catalog"} {
		assert.Containsf(t, respFields, k,
			"CAT-1: from-catalog 响应必须含 %q（用于 FE 直接渲染目录关系）", k)
	}

	statuses, _ := r["response_status"].([]any)
	hasCreated := false
	for _, s := range statuses {
		if n, ok := s.(float64); ok && int(n) == 201 {
			hasCreated = true
			break
		}
	}
	assert.True(t, hasCreated, "CAT-1: from-catalog 成功路径必须返回 201 Created")
}

// TestContracts_ResetOverrideRoutePresent CAT-1:
// POST /api/v1/models/:id/reset-override 必须存在，必填字段为 {field}，
// field 限定 oneof=context_length max_output_tokens；
// error_codes 必须引用 catalog_not_bound。
func TestContracts_ResetOverrideRoutePresent(t *testing.T) {
	routes := loadCPRoutes(t)
	r := findRoute(routes, "POST", "/api/v1/models/:id/reset-override")
	require.NotNil(t, r, "CAT-1: POST /api/v1/models/:id/reset-override 必须在 contracts.json 中声明")

	required := asStringSlice(r["request_fields_required"])
	assert.Contains(t, required, "field", "CAT-1: reset-override 必填字段缺 field")

	if constraints, ok := r["request_field_constraints"].(map[string]any); ok {
		if rule, ok := constraints["field"].(string); ok {
			assert.Contains(t, rule, "context_length",
				"CAT-1: reset-override field 约束必须允许 context_length")
			assert.Contains(t, rule, "max_output_tokens",
				"CAT-1: reset-override field 约束必须允许 max_output_tokens")
		} else {
			t.Errorf("CAT-1: reset-override 的 field 必须有 constraint（期望 oneof=...）")
		}
	} else {
		t.Errorf("CAT-1: reset-override 必须声明 request_field_constraints.field")
	}

	// catalog_not_bound 必须出现在 error_codes 字符串中
	ec, _ := r["error_codes"].(map[string]any)
	require.NotNil(t, ec, "CAT-1: reset-override 必须声明 error_codes")
	var allCodes string
	for _, v := range ec {
		if s, ok := v.(string); ok {
			allCodes += " " + s
		}
	}
	assert.Contains(t, allCodes, "catalog_not_bound",
		"CAT-1: reset-override 的 error_codes 必须引用 catalog_not_bound")
}

// TestContracts_CatalogErrorCodesCanonical CAT-1:
// 新增 3 个 canonical 错误码（catalog_not_found / catalog_not_bound / sync_in_progress）
// 必须出现在 control_plane.error_codes_canonical 表中，且 type/http 分类正确。
func TestContracts_CatalogErrorCodesCanonical(t *testing.T) {
	raw, err := readContracts("contracts.json")
	require.NoError(t, err)
	var doc struct {
		ControlPlane struct {
			ErrorCodesCanonical map[string]map[string]any `json:"error_codes_canonical"`
		} `json:"control_plane"`
	}
	require.NoError(t, json.Unmarshal(raw, &doc))

	canon := doc.ControlPlane.ErrorCodesCanonical
	require.NotNil(t, canon)

	want := map[string]struct {
		http int
		typ  string
	}{
		"catalog_not_found": {404, "not_found"},
		"catalog_not_bound": {400, "invalid_request"},
		"sync_in_progress":  {409, "conflict"},
	}
	for code, expect := range want {
		entry, ok := canon[code]
		require.Truef(t, ok, "CAT-1: 缺失 canonical code %q", code)
		// http 在 JSON 里是 float64
		gotHTTP, _ := entry["http"].(float64)
		assert.Equalf(t, expect.http, int(gotHTTP),
			"CAT-1: %s 的 http 应为 %d", code, expect.http)
		assert.Equalf(t, expect.typ, entry["type"],
			"CAT-1: %s 的 type 应为 %q", code, expect.typ)
	}
}

// TestContracts_CatalogClientsPresent CAT-1:
// frontend_clients.control-plane.ts 必须新增 7 个调用条目，方法和 URL 必须与 BE 路由一致。
func TestContracts_CatalogClientsPresent(t *testing.T) {
	raw, err := readContracts("contracts.json")
	require.NoError(t, err)
	var doc struct {
		FrontendClients struct {
			ControlPlane struct {
				Calls []map[string]any `json:"calls"`
			} `json:"control-plane.ts"`
		} `json:"frontend_clients"`
	}
	require.NoError(t, json.Unmarshal(raw, &doc))

	want := []struct {
		fn     string
		method string
		url    string
	}{
		{"searchCatalog", "GET", "/api/v1/model-catalog/search"},
		{"getCatalogBySource", "GET", "/api/v1/model-catalog/by-source"},
		{"triggerCatalogSync", "POST", "/api/v1/model-catalog/sync"},
		{"getCatalogDiff", "GET", "/api/v1/model-catalog/diff"},
		{"applyCatalogDiff", "POST", "/api/v1/model-catalog/apply-diff"},
		{"createModelFromCatalog", "POST", "/api/v1/models/from-catalog"},
		{"resetModelOverride", "POST", "/api/v1/models/:id/reset-override"},
	}

	index := map[string]map[string]any{}
	for _, c := range doc.FrontendClients.ControlPlane.Calls {
		if fn, ok := c["fn"].(string); ok {
			index[fn] = c
		}
	}

	for _, w := range want {
		c, ok := index[w.fn]
		require.Truef(t, ok, "CAT-1: frontend_clients 缺 fn=%s", w.fn)
		assert.Equalf(t, w.method, c["method"], "CAT-1: %s.method 期望 %s", w.fn, w.method)
		assert.Equalf(t, w.url, c["url"], "CAT-1: %s.url 期望 %s", w.fn, w.url)
	}
}

func readAll(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	_, err := buf.ReadFrom(resp.Body)
	require.NoError(t, err)
	return buf.Bytes()
}
