package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/gateway/server"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// rejectingBillingReadiness 模拟计费事实存储到达高水位（CanAcceptBillableRequest=false），
// 用于观察哪些请求会被 BillingFactGuard 以 503 背压拒绝。
type rejectingBillingReadiness struct{}

func (rejectingBillingReadiness) CanAcceptBillableRequest() bool { return false }

// acceptingBillingReadiness 模拟存储健康，用于验证未到高水位时一律放行。
type acceptingBillingReadiness struct{}

func (acceptingBillingReadiness) CanAcceptBillableRequest() bool { return true }

// newBillingGuardRouter 构造只挂 BillingFactGuard + 兜底 handler 的路由：
// 请求能走到兜底 handler（204）说明"放行"，被中间件拦截（503）说明"计入计费路径白名单"。
//
// 参数用匿名接口（方法集与 server 包内未导出的 readiness 接口一致）以便外部测试包可赋值；
// 传 nil 表示 readiness 缺失（未注入计费事实存储）。
func newBillingGuardRouter(readiness interface{ CanAcceptBillableRequest() bool }) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(server.BillingFactGuard(readiness))
	router.Any("/*path", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	return router
}

// TestBillingFactGuard_BillablePathWhitelist 逐条断言 isBillableRequest 的路径/方法白名单。
//
// 测什么：chat/embeddings/rerank/responses/messages 等既有计费路径，以及本次 A4 新增的媒体
// 提交路径（images/generations、images/edits、videos）与既有 Seedance 创建路径；
// 同时覆盖"查询类请求不背压""方法不对不背压""未登记路径不背压"。
// 怎么测：真 Gin 路由 + httptest，高水位 readiness 下断言 503（背压）或 204（放行）。
// 期望：白名单内的 POST 提交类请求被 503 拒绝，其余一律放行——避免「chat 被拒流、媒体全量放行」。
func TestBillingFactGuard_BillablePathWhitelist(t *testing.T) {
	tests := []struct {
		name         string
		description  string
		method       string
		path         string
		wantBillable bool
	}{
		// ── 既有计费路径（回归，防止本次改动误删） ─────────────────────────────
		{
			name: "chat 计费路径", description: "POST /v1/chat/completions 属于计费路径，高水位时必须背压",
			method: http.MethodPost, path: "/v1/chat/completions", wantBillable: true,
		},
		{
			name: "embeddings 计费路径", description: "POST /v1/embeddings 属于计费路径",
			method: http.MethodPost, path: "/v1/embeddings", wantBillable: true,
		},
		{
			name: "rerank 计费路径", description: "POST /v1/rerank 属于计费路径",
			method: http.MethodPost, path: "/v1/rerank", wantBillable: true,
		},
		{
			name: "responses 计费路径", description: "POST /v1/responses 属于计费路径",
			method: http.MethodPost, path: "/v1/responses", wantBillable: true,
		},
		{
			name: "messages 计费路径", description: "POST /v1/messages 属于计费路径",
			method: http.MethodPost, path: "/v1/messages", wantBillable: true,
		},

		// ── 本次 A4 纳入的媒体提交路径 ───────────────────────────────────────
		{
			name: "图像生成路径", description: "POST /v1/images/generations 是新纳入的计费路径",
			method: http.MethodPost, path: "/v1/images/generations", wantBillable: true,
		},
		{
			name: "图像编辑路径", description: "POST /v1/images/edits 是新纳入的计费路径",
			method: http.MethodPost, path: "/v1/images/edits", wantBillable: true,
		},
		{
			name: "视频提交路径", description: "POST /v1/videos 是新纳入的计费路径",
			method: http.MethodPost, path: "/v1/videos", wantBillable: true,
		},
		{
			name: "Seedance 创建路径", description: "POST /v1/contents/generations/tasks 是既有视频创建端点，一并纳入白名单（否则视频仍可绕过背压）",
			method: http.MethodPost, path: "/v1/contents/generations/tasks", wantBillable: true,
		},

		// ── 查询类请求不背压 ────────────────────────────────────────────────
		{
			name: "视频查询不背压", description: "GET /v1/videos/{task_id} 是查询，不产生新的计费事实，不背压",
			method: http.MethodGet, path: "/v1/videos/video_abc123", wantBillable: false,
		},
		{
			name: "视频产物下载不背压", description: "GET /v1/videos/{task_id}/content 是下载，不背压",
			method: http.MethodGet, path: "/v1/videos/video_abc123/content", wantBillable: false,
		},
		{
			name: "Seedance 查询不背压", description: "GET /v1/contents/generations/tasks/{task_id} 是查询，不背压",
			method: http.MethodGet, path: "/v1/contents/generations/tasks/cgt-1", wantBillable: false,
		},
		{
			name: "models 不背压", description: "GET /v1/models 是只读端点，不背压",
			method: http.MethodGet, path: "/v1/models", wantBillable: false,
		},
		{
			name: "model_info 不背压", description: "GET /v1/model/info 是只读端点，不背压",
			method: http.MethodGet, path: "/v1/model/info", wantBillable: false,
		},
		{
			name: "count_tokens 不背压", description: "POST /v1/messages/count_tokens 只做计量、不产生生成计费事实，不在白名单内",
			method: http.MethodPost, path: "/v1/messages/count_tokens", wantBillable: false,
		},

		// ── 方法不对 / 未登记路径不背压 ─────────────────────────────────────
		{
			name: "chat 的 GET 不背压", description: "GET /v1/chat/completions 不是提交请求，isBillableRequest 只认 POST",
			method: http.MethodGet, path: "/v1/chat/completions", wantBillable: false,
		},
		{
			name: "视频提交的 GET 不背压", description: "GET /v1/videos 是列表（本次未实现），不背压",
			method: http.MethodGet, path: "/v1/videos", wantBillable: false,
		},
		{
			name: "素材端点本次不背压", description: "POST /v1/assets/{Action} 未纳入 A4 白名单（超出本次范围，属姊妹文档 §5 P0#3 的其余条目）；若后续纳入需同步改本断言",
			method: http.MethodPost, path: "/v1/assets/GetAsset", wantBillable: false,
		},
		{
			name: "未登记路径不背压", description: "POST /v1/unknown 未登记，不背压",
			method: http.MethodPost, path: "/v1/unknown", wantBillable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newBillingGuardRouter(rejectingBillingReadiness{})

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(tt.method, tt.path, nil))

			if tt.wantBillable {
				require.Equal(t, http.StatusServiceUnavailable, recorder.Code, tt.description)
				require.Contains(t, recorder.Body.String(), "billing_fact_store_backpressure", tt.description)
				return
			}
			// 非计费路径：中间件放行，落到兜底 handler（204），不能出现背压 503
			require.Equal(t, http.StatusNoContent, recorder.Code, tt.description)
		})
	}
}

// TestBillingFactGuard_StorageHealthyPassesAllRequests 断言存储未到高水位时，即使是计费路径也一律放行。
//
// 测什么：readiness=true（存储健康）时，isBillableRequest 命中的路径不应被拦截。
// 期望：全部 204（放行），证明背压只在存储高水位时生效。
func TestBillingFactGuard_StorageHealthyPassesAllRequests(t *testing.T) {
	router := newBillingGuardRouter(acceptingBillingReadiness{})

	for _, path := range []string{
		"/v1/chat/completions",
		"/v1/images/generations",
		"/v1/images/edits",
		"/v1/videos",
		"/v1/contents/generations/tasks",
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, nil))
		require.Equal(t, http.StatusNoContent, recorder.Code, "存储健康时 POST %s 不应被背压拒绝", path)
	}
}

// TestBillingFactGuard_NilReadinessPassesAllRequests 断言 readiness 为 nil（未注入存储）时不拦截。
//
// 测什么：BillingFactGuard(nil) 的降级行为——存储组件缺失时不能把全部请求拒掉。
// 期望：计费路径也返回 204（放行）。
func TestBillingFactGuard_NilReadinessPassesAllRequests(t *testing.T) {
	router := newBillingGuardRouter(nil)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	require.Equal(t, http.StatusNoContent, recorder.Code)
}
