package handler_test

// Gemini 原生入站端到端测试：POST /v1beta/models/{model}:generateContent。
//
// 行为与 /v1/messages 的两段式一致：orchestrator 能原生就零转换透传上游原始
// 字节；只有 ErrNativeNotSupported 才降级为 Gemini → Chat → Gemini 的
// relaykit 转换路径。fake orchestrator 同时实现两条路径，用计数器区分。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/example/llm-gateway/internal/protocol"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// geminiNativeJSONPayload 含 modelVersion / thoughtSignature 等 relaykit 转换
// 表达不了的字段，原样透传必须逐字节保留。
const geminiNativeJSONPayload = `{"candidates":[{"content":{"parts":[{"text":"native gemini hello","thoughtSignature":"sig-keep"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":3,"totalTokenCount":8},"modelVersion":"gemini-2.5-pro"}`

// geminiNativeSSEPayload 是原生上游返回的 Gemini SSE 原文（data: 帧）。
const geminiNativeSSEPayload = "" +
	`data: {"candidates":[{"content":{"parts":[{"text":"native "}],"role":"model"}}],"modelVersion":"gemini-2.5-pro"}` + sseFrameSep +
	`data: {"candidates":[{"content":{"parts":[{"text":"gemini"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":3,"totalTokenCount":8}}` + sseFrameSep +
	""

// sseFrameSep 是 SSE 帧分隔符，单独成常量避免源码里裸露空行。
const sseFrameSep = "\n\n"

// geminiGenerateBody 是客户端发来的 Gemini 原生请求体（无 model 字段）。
const geminiGenerateBody = `{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"maxOutputTokens":64}}`

type geminiFakeOrchestrator struct {
	nativeErr       error
	nativeStreamErr error

	nativeCalls       int
	nativeStreamCalls int
	chatCalls         int
	streamCalls       int

	lastFormat    core.NativeRelayFormat
	lastReqModel  string
	lastBodyModel string
}

func (f *geminiFakeOrchestrator) record(req *core.ChatRequest, body []byte, format core.NativeRelayFormat) {
	f.lastFormat = format
	if req != nil {
		f.lastReqModel = req.Model
	}
	var fields struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(body, &fields)
	f.lastBodyModel = fields.Model
}

func (f *geminiFakeOrchestrator) Chat(_ context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	f.chatCalls++
	return &core.ChatResponse{
		ID: "chatcmpl-relay", Object: "chat.completion", Model: req.Model,
		Choices: []core.Choice{{Index: 0, FinishReason: "stop",
			Message: core.ResponseMessage{Role: "assistant", Content: "relaykit hello"}}},
		Usage: core.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12},
	}, nil
}

func (f *geminiFakeOrchestrator) ChatStream(_ context.Context, _ *core.ChatRequest) (io.ReadCloser, error) {
	f.streamCalls++
	sse := "" +
		`data: {"id":"chatcmpl-relay","object":"chat.completion.chunk","model":"gemini-2.5-pro","choices":[{"index":0,"delta":{"role":"assistant","content":"relaykit hello"},"finish_reason":null}]}` + sseFrameSep +
		`data: {"id":"chatcmpl-relay","object":"chat.completion.chunk","model":"gemini-2.5-pro","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + sseFrameSep +
		`data: [DONE]` + sseFrameSep +
		""
	return io.NopCloser(strings.NewReader(sse)), nil
}

func (f *geminiFakeOrchestrator) ChatNative(_ context.Context, req *core.ChatRequest, body []byte, format core.NativeRelayFormat) ([]byte, error) {
	f.nativeCalls++
	f.record(req, body, format)
	if f.nativeErr != nil {
		return nil, f.nativeErr
	}
	return []byte(geminiNativeJSONPayload), nil
}

func (f *geminiFakeOrchestrator) ChatNativeStream(_ context.Context, req *core.ChatRequest, body []byte, format core.NativeRelayFormat) (io.ReadCloser, error) {
	f.nativeStreamCalls++
	f.record(req, body, format)
	if f.nativeStreamErr != nil {
		return nil, f.nativeStreamErr
	}
	return io.NopCloser(strings.NewReader(geminiNativeSSEPayload)), nil
}

// newGeminiRouter 复刻 router.go 的注册形态：/:modelAction 段同时承载 model 与 action。
func newGeminiRouter(t *testing.T, orch handler.ChatOrchestratorInterface) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := handler.NewGeminiHandler(orch)
	r := gin.New()
	r.POST("/v1beta/models/:modelAction", h.Handle)
	r.POST("/v1/models/:modelAction", h.Handle)
	return r
}

func doGeminiRequest(t *testing.T, r *gin.Engine, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// 用例 1：非流式原生 —— 客户端收到的字节必须与 fake 返回的完全一致，format 为 gemini。
func TestGeminiNativeE2E_NonStreamPassthrough(t *testing.T) {
	orch := &geminiFakeOrchestrator{}
	r := newGeminiRouter(t, orch)

	w := doGeminiRequest(t, r, "/v1beta/models/gemini-2.5-pro:generateContent", geminiGenerateBody)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != geminiNativeJSONPayload {
		t.Errorf("原生 JSON 被改写：\n got=%q\nwant=%q", got, geminiNativeJSONPayload)
	}
	if orch.lastFormat != core.NativeRelayGemini {
		t.Errorf("format: got %q want %q", orch.lastFormat, core.NativeRelayGemini)
	}
	if orch.nativeCalls != 1 || orch.chatCalls != 0 {
		t.Errorf("原生成功后不应走 Chat：native=%d chat=%d", orch.nativeCalls, orch.chatCalls)
	}
}

// 用例 2：流式原生 —— 字节逐字节透传，Content-Type 为 text/event-stream。
func TestGeminiNativeE2E_StreamPassthrough(t *testing.T) {
	orch := &geminiFakeOrchestrator{}
	r := newGeminiRouter(t, orch)

	w := doGeminiRequest(t, r, "/v1beta/models/gemini-2.5-pro:streamGenerateContent", geminiGenerateBody)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type: got %q want text/event-stream", ct)
	}
	if got := w.Body.String(); got != geminiNativeSSEPayload {
		t.Errorf("原生 SSE 被改写：\n got=%q\nwant=%q", got, geminiNativeSSEPayload)
	}
	if orch.lastFormat != core.NativeRelayGemini {
		t.Errorf("format: got %q want %q", orch.lastFormat, core.NativeRelayGemini)
	}
	if orch.nativeStreamCalls != 1 || orch.streamCalls != 0 {
		t.Errorf("原生成功后不应走 ChatStream：native=%d chat=%d", orch.nativeStreamCalls, orch.streamCalls)
	}
}

// 用例 3：降级 —— 只有 ErrNativeNotSupported 才降级，客户端拿到合法 Gemini 响应（不是错误）。
func TestGeminiNativeE2E_Fallback(t *testing.T) {
	t.Run("non-stream", func(t *testing.T) {
		orch := &geminiFakeOrchestrator{nativeErr: fmt.Errorf("wrap: %w", core.ErrNativeNotSupported)}
		r := newGeminiRouter(t, orch)

		w := doGeminiRequest(t, r, "/v1beta/models/gemini-2.5-pro:generateContent", geminiGenerateBody)

		if w.Code != http.StatusOK {
			t.Fatalf("降级应成功，status=%d body=%s", w.Code, w.Body.String())
		}
		var resp struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
			Error json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("降级响应不是合法 JSON: %v body=%s", err, w.Body.String())
		}
		if len(resp.Error) > 0 {
			t.Fatalf("降级不应返回错误体: %s", w.Body.String())
		}
		if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
			t.Fatalf("降级响应缺少 candidates/parts: %s", w.Body.String())
		}
		if got := resp.Candidates[0].Content.Parts[0].Text; got != "relaykit hello" {
			t.Errorf("降级内容: got %q want relaykit hello", got)
		}
		if orch.chatCalls != 1 {
			t.Errorf("降级后 Chat 调用次数: got %d want 1", orch.chatCalls)
		}
	})

	t.Run("stream", func(t *testing.T) {
		orch := &geminiFakeOrchestrator{nativeStreamErr: fmt.Errorf("wrap: %w", core.ErrNativeNotSupported)}
		r := newGeminiRouter(t, orch)

		w := doGeminiRequest(t, r, "/v1beta/models/gemini-2.5-pro:streamGenerateContent", geminiGenerateBody)

		if w.Code != http.StatusOK {
			t.Fatalf("降级应成功，status=%d body=%s", w.Code, w.Body.String())
		}
		if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
			t.Errorf("Content-Type: got %q want text/event-stream", ct)
		}
		out := w.Body.String()
		if !strings.Contains(out, "data: ") || !strings.Contains(out, "candidates") {
			t.Fatalf("降级输出不是 Gemini SSE: %.500s", out)
		}
		if !strings.Contains(out, "relaykit hello") {
			t.Errorf("降级输出缺少 relaykit 内容: %.500s", out)
		}
		if orch.streamCalls != 1 {
			t.Errorf("降级后 ChatStream 调用次数: got %d want 1", orch.streamCalls)
		}
	})
}

// 用例 4：真实错误不降级 —— 必须是 Gemini error JSON，且不二次打上游。
func TestGeminiNativeE2E_ErrorNoFallback(t *testing.T) {
	orch := &geminiFakeOrchestrator{nativeErr: errors.New("upstream native 500")}
	r := newGeminiRouter(t, orch)

	w := doGeminiRequest(t, r, "/v1beta/models/gemini-2.5-pro:generateContent", geminiGenerateBody)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d want 500, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("错误响应不是合法 JSON: %v body=%s", err, w.Body.String())
	}
	if resp.Error.Code != http.StatusInternalServerError || resp.Error.Message == "" || resp.Error.Status != "INTERNAL" {
		t.Errorf("错误响应格式不对: %s", w.Body.String())
	}
	if orch.chatCalls != 0 {
		t.Errorf("真实错误不应降级，Chat 调用次数: got %d want 0", orch.chatCalls)
	}
}

// 用例 5：路由解析 —— :modelAction 段拆出 model 与 action，两种路径前缀都支持。
func TestGeminiNativeE2E_RouteParsing(t *testing.T) {
	cases := []struct {
		name       string
		path       string
		wantModel  string
		wantStream bool
	}{
		{"v1beta generateContent", "/v1beta/models/gemini-2.5-pro:generateContent", "gemini-2.5-pro", false},
		{"v1beta streamGenerateContent", "/v1beta/models/gemini-2.5-flash:streamGenerateContent", "gemini-2.5-flash", true},
		{"v1 models 等价形态", "/v1/models/gemini-2.0-flash:generateContent", "gemini-2.0-flash", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			orch := &geminiFakeOrchestrator{}
			r := newGeminiRouter(t, orch)

			w := doGeminiRequest(t, r, tc.path, geminiGenerateBody)
			if w.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if orch.lastReqModel != tc.wantModel {
				t.Errorf("model: got %q want %q", orch.lastReqModel, tc.wantModel)
			}
			if orch.lastBodyModel != tc.wantModel {
				t.Errorf("body.model: got %q want %q（Router 需要 body 里的 model 做路由）", orch.lastBodyModel, tc.wantModel)
			}
			if tc.wantStream {
				if orch.nativeStreamCalls != 1 || orch.nativeCalls != 0 {
					t.Errorf("action 应解析为流式：native=%d nativeStream=%d", orch.nativeCalls, orch.nativeStreamCalls)
				}
				return
			}
			if orch.nativeCalls != 1 || orch.nativeStreamCalls != 0 {
				t.Errorf("action 应解析为非流式：native=%d nativeStream=%d", orch.nativeCalls, orch.nativeStreamCalls)
			}
		})
	}
}

// TestGeminiCountTokens 覆盖 :countTokens。
//
// 这是 google-genai SDK 会主动调用的方法（用于预估成本）。此前未支持时
// 直接返回 400 INVALID_ARGUMENT，等于 SDK 的 token 预估功能整段报错。
// 语义对齐 /v1/messages/count_tokens：本地估算、不产生计费、不打上游。
func TestGeminiCountTokens(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantTokens bool // true 表示期望 > 0
	}{
		{
			name:       "英文内容",
			body:       `{"contents":[{"role":"user","parts":[{"text":"Hello world, how are you today?"}]}]}`,
			wantTokens: true,
		},
		{
			name:       "中文内容",
			body:       `{"contents":[{"role":"user","parts":[{"text":"你好，请帮我总结这段文字的主要内容。"}]}]}`,
			wantTokens: true,
		},
		{
			name:       "多轮对话",
			body:       `{"contents":[{"role":"user","parts":[{"text":"hi"}]},{"role":"model","parts":[{"text":"hello"}]},{"role":"user","parts":[{"text":"bye"}]}]}`,
			wantTokens: true,
		},
		{
			name:       "空 contents",
			body:       `{"contents":[]}`,
			wantTokens: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			orch := &geminiFakeOrchestrator{}
			r := newGeminiRouter(t, orch)

			req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:countTokens", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
			}
			var resp struct {
				TotalTokens int `json:"totalTokens"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("响应不是 JSON: %v (%s)", err, w.Body.String())
			}
			if tc.wantTokens && resp.TotalTokens <= 0 {
				t.Errorf("totalTokens = %d, 期望 > 0", resp.TotalTokens)
			}
			if !tc.wantTokens && resp.TotalTokens != 0 {
				t.Errorf("空 contents 应返回 0，got %d", resp.TotalTokens)
			}
			// countTokens 是免费查询：不得打上游、不得计费。
			if orch.nativeCalls != 0 || orch.streamCalls != 0 || orch.chatCalls != 0 {
				t.Errorf("countTokens 不应调用 orchestrator: native=%d stream=%d chat=%d",
					orch.nativeCalls, orch.streamCalls, orch.chatCalls)
			}
		})
	}
}

// TestGeminiCountTokens_UnknownActionStillRejected 确认未知 action 仍然报错，
// 且错误信息里提示了 countTokens（避免回归到「只支持两个 action」的旧文案）。
func TestGeminiCountTokens_UnknownActionStillRejected(t *testing.T) {
	r := newGeminiRouter(t, &geminiFakeOrchestrator{})
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:noSuchAction",
		strings.NewReader(`{"contents":[]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "countTokens") {
		t.Errorf("错误信息应列出受支持的 action: %s", w.Body.String())
	}
}

// ─── protocol.Policy 对 Gemini 原生旁路的影响 ─────────────────────────────
//
// 背景：四条入站里 Gemini 最特殊 —— 路由是新加的，没有 legacy 实现可对照，
// 所以早期实现把 policy 只写进 gin.Context 而不参与决策，等于「记录但不用」。
// 这让运维无法按 Key 关闭某个客户端的原生直发（只能改渠道配置回退）。
// 下面三个用例把三种模式的语义钉住。

// legacyPolicy 构造把 test-key 设为 legacy 的协议策略。
func legacyPolicy() *protocol.Policy {
	p, err := protocol.LoadPolicy(protocol.PolicyConfig{
		DefaultMode: "legacy",
		PerKey:      map[string]string{"test-key": "legacy"},
	})
	if err != nil {
		panic(err)
	}
	return p
}

// geminiPolicyRouter 构造带 key_id 的 Gemini 路由（policy 按 key_id 解析）。
func geminiPolicyRouter(t *testing.T, orch handler.ChatOrchestratorInterface, p *protocol.Policy) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := handler.NewGeminiHandler(orch)
	if p != nil {
		h.SetProtocolPolicy(p)
	}
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("key_id", "test-key") })
	r.POST("/v1beta/models/:modelAction", h.Handle)
	return r
}

const geminiCountFreeBody = `{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`

// TestGeminiNative_LegacyPolicyForcesConversion：legacy 必须完全跳过原生直发。
func TestGeminiNative_LegacyPolicyForcesConversion(t *testing.T) {
	orch := &geminiFakeOrchestrator{}
	r := geminiPolicyRouter(t, orch, legacyPolicy())

	w := doGeminiRequest(t, r, "/v1beta/models/gemini-2.5-flash:generateContent", geminiCountFreeBody)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 0, orch.nativeCalls, "legacy 模式下不得调用原生直发")
	require.Equal(t, 1, orch.chatCalls, "legacy 模式应走转换路径")
}

// TestGeminiNative_ActivePolicyUsesNative：active 走原生直发。
func TestGeminiNative_ActivePolicyUsesNative(t *testing.T) {
	orch := &geminiFakeOrchestrator{}
	r := geminiPolicyRouter(t, orch, activePolicy())

	w := doGeminiRequest(t, r, "/v1beta/models/gemini-2.5-flash:generateContent", geminiCountFreeBody)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 1, orch.nativeCalls, "active 模式应走原生直发")
	require.Equal(t, 0, orch.chatCalls, "active 模式不应打转换路径")
}

// TestGeminiNative_NilPolicyKeepsNative：未配置策略时保持历史行为（Gemini 一直走原生）。
// 这是兼容性护栏：Gemini 是新增路由，没有 policy 也不该突然变成转换。
func TestGeminiNative_NilPolicyKeepsNative(t *testing.T) {
	orch := &geminiFakeOrchestrator{}
	r := geminiPolicyRouter(t, orch, nil)

	w := doGeminiRequest(t, r, "/v1beta/models/gemini-2.5-flash:generateContent", geminiCountFreeBody)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 1, orch.nativeCalls, "无 policy 时 Gemini 应保持原生直发")
}
