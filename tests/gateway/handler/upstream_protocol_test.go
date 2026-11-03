package handler_test

// 原生旁路可观测标记：只有「原生直发成功」才写 gin key `upstream_protocol`；
// 降级（包装了 core.ErrNativeNotSupported）与真实错误一律不写 —— 空串即代表走了转换路径。
// 这是运维看板区分 native_* 与 conversion 的唯一信号源。
//
// 捕获方式：在注册路由前挂一个 middleware，c.Next() 之后读 gin context，
// 确保读到的是 handler 全部执行完（含流式转发）后的最终值。

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/example/llm-gateway/internal/protocol"
	"github.com/gin-gonic/gin"
)

// upstreamProbe 记录 handler 结束后 gin context 里的 upstream_protocol。
type upstreamProbe struct {
	value string
	set   bool
}

// newProbedEngine 返回挂了探针的引擎；探针必须先于路由注册 Use。
func newProbedEngine() (*gin.Engine, *upstreamProbe) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	probe := &upstreamProbe{}
	r.Use(func(c *gin.Context) {
		c.Next()
		probe.value = c.GetString("upstream_protocol")
		_, probe.set = c.Get("upstream_protocol")
	})
	return r, probe
}

func probedChatRouter(t *testing.T, nativeErr error, stream bool) (*gin.Engine, *upstreamProbe) {
	t.Helper()
	fake := &chatNativeFakeOrchestrator{}
	if stream {
		fake.nativeStreamErr = nativeErr
	} else {
		fake.nativeBodyErr = nativeErr
	}
	h := handler.NewChatHandler(fake)
	h.SetProtocolPolicy(activePolicy())

	r, probe := newProbedEngine()
	r.Use(func(c *gin.Context) { c.Set("key_id", "test-key") })
	r.POST("/v1/chat/completions", h.Handle)
	return r, probe
}

func probedMessagesRouter(t *testing.T, nativeErr error, stream bool) (*gin.Engine, *upstreamProbe) {
	t.Helper()
	fake := &nativeFakeOrchestrator{}
	if stream {
		fake.nativeStreamErr = nativeErr
	} else {
		fake.nativeBodyErr = nativeErr
	}
	nc := cache.NewGroupNameCache("")
	nc.SetNames([]string{"rk-model"})
	sch := &fakeMessagesScheduler{decision: &scheduler.RouteDecision{
		GroupName: "rk-model", ModelID: "1", ModelName: "rk-model", ProviderName: "p1",
	}}
	gd := orchestrator.NewGroupAwareDispatcher(sch, nc)

	h := handler.NewMessagesHandlerWithGroup(fake, gd)
	policy, err := protocol.LoadPolicy(protocol.PolicyConfig{
		DefaultMode: "active",
		PerKey:      map[string]string{"test-key": "active"},
	})
	if err != nil {
		t.Fatalf("LoadPolicy: %v", err)
	}
	h.SetProtocolPolicy(policy)

	r, probe := newProbedEngine()
	r.Use(func(c *gin.Context) { c.Set("key_id", "test-key") })
	r.POST("/v1/messages", h.HandleMessages)
	return r, probe
}

func probedResponsesRouter(t *testing.T, nativeErr error, stream bool) (*gin.Engine, *upstreamProbe) {
	t.Helper()
	fake := &responsesNativeFake{
		nativeErr:  nativeErr,
		nativeBody: responsesNativeJSON,
		streamBody: "data: {\"type\":\"response.completed\"}\n\n",
	}
	h := handler.NewResponsesHandler(legacyResponsesFake{})
	h.SetChatOrchestrator(fake)
	policy, err := protocol.LoadPolicy(protocol.PolicyConfig{
		DefaultMode: "active",
		PerKey:      map[string]string{"test-key": "active"},
	})
	if err != nil {
		t.Fatalf("LoadPolicy: %v", err)
	}
	h.SetProtocolPolicy(policy)

	r, probe := newProbedEngine()
	r.Use(func(c *gin.Context) { c.Set("key_id", "test-key") })
	r.POST("/v1/responses", h.Handle)
	return r, probe
}

func probedGeminiRouter(t *testing.T, nativeErr error, stream bool) (*gin.Engine, *upstreamProbe) {
	t.Helper()
	fake := &geminiFakeOrchestrator{}
	if stream {
		fake.nativeStreamErr = nativeErr
	} else {
		fake.nativeErr = nativeErr
	}
	h := handler.NewGeminiHandler(fake)

	r, probe := newProbedEngine()
	r.POST("/v1beta/models/:modelAction", h.Handle)
	return r, probe
}

// upstreamCase 一条入站协议的原生成功样本。
type upstreamCase struct {
	name   string
	want   string
	stream bool
	path   string
	body   string
	router func(t *testing.T, nativeErr error, stream bool) (*gin.Engine, *upstreamProbe)
}

func upstreamCases() []upstreamCase {
	return []upstreamCase{
		{
			name: "native_chat/非流式", want: "native_chat", path: "/v1/chat/completions",
			body:   `{"model":"native-model","messages":[{"role":"user","content":"hi"}]}`,
			router: probedChatRouter,
		},
		{
			name: "native_chat/流式", want: "native_chat", stream: true, path: "/v1/chat/completions",
			body:   `{"model":"native-model","messages":[{"role":"user","content":"hi"}],"stream":true}`,
			router: probedChatRouter,
		},
		{
			name: "native_responses/非流式", want: "native_responses", path: "/v1/responses",
			body:   `{"model":"gpt-5.6-sol","input":"hello"}`,
			router: probedResponsesRouter,
		},
		{
			name: "native_responses/流式", want: "native_responses", stream: true, path: "/v1/responses",
			body:   `{"model":"gpt-5.6-sol","input":"hello","stream":true}`,
			router: probedResponsesRouter,
		},
		{
			name: "native_claude/非流式", want: "native_claude", path: "/v1/messages",
			body:   `{"model":"rk-model","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`,
			router: probedMessagesRouter,
		},
		{
			name: "native_claude/流式", want: "native_claude", stream: true, path: "/v1/messages",
			body:   `{"model":"rk-model","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"hi"}]}`,
			router: probedMessagesRouter,
		},
		{
			name: "native_gemini/非流式", want: "native_gemini", path: "/v1beta/models/gemini-2.5-pro:generateContent",
			body:   geminiGenerateBody,
			router: probedGeminiRouter,
		},
		{
			name: "native_gemini/流式", want: "native_gemini", stream: true, path: "/v1beta/models/gemini-2.5-pro:streamGenerateContent",
			body:   geminiGenerateBody,
			router: probedGeminiRouter,
		},
	}
}

func doUpstreamRequest(t *testing.T, r *gin.Engine, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// 原生成功：四条入站协议（流式/非流式）都必须写入对应取值。
func TestUpstreamProtocol_NativeSuccess(t *testing.T) {
	for _, tc := range upstreamCases() {
		t.Run(tc.name, func(t *testing.T) {
			r, probe := tc.router(t, nil, tc.stream)

			w := doUpstreamRequest(t, r, tc.path, tc.body)

			if w.Code != http.StatusOK {
				t.Fatalf("原生成功应 200，got %d: %s", w.Code, w.Body.String())
			}
			if probe.value != tc.want {
				t.Errorf("upstream_protocol = %q, want %q", probe.value, tc.want)
			}
		})
	}
}

// 降级：包装了哨兵的错误必须留空（空串 = 转换路径），且请求仍然成功。
func TestUpstreamProtocol_FallbackNotSet(t *testing.T) {
	for _, tc := range upstreamCases() {
		t.Run(tc.name, func(t *testing.T) {
			fallbackErr := fmt.Errorf("provider lacks native capability: %w", core.ErrNativeNotSupported)
			r, probe := tc.router(t, fallbackErr, tc.stream)

			w := doUpstreamRequest(t, r, tc.path, tc.body)

			if w.Code != http.StatusOK {
				t.Fatalf("降级路径应成功，got %d: %s", w.Code, w.Body.String())
			}
			if probe.set {
				t.Errorf("降级不得设置 upstream_protocol，实际 %q", probe.value)
			}
		})
	}
}

// 真实错误（非哨兵）：不降级、也不得打标。
func TestUpstreamProtocol_RealErrorNotSet(t *testing.T) {
	for _, tc := range upstreamCases() {
		t.Run(tc.name, func(t *testing.T) {
			r, probe := tc.router(t, errors.New("upstream exploded"), tc.stream)

			w := doUpstreamRequest(t, r, tc.path, tc.body)

			if w.Code == http.StatusOK {
				t.Fatalf("真实错误不应返回 200: %s", w.Body.String())
			}
			if probe.set {
				t.Errorf("真实错误不得设置 upstream_protocol，实际 %q", probe.value)
			}
		})
	}
}
