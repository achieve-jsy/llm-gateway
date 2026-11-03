package handler_test

// Chat 原生旁路（Native Chat）端到端测试：/v1/chat/completions 在 orchestrator
// 实现 NativeChatOrchestratorInterface 时，把客户端原始 OpenAI body 零转换直发上游，
// 响应原样回传；只有 ErrNativeNotSupported 才降级到现有 Chat 路径。
// 最后一个用例专门守住「不实现原生接口的 orchestrator 行为与改动前完全一致」。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/llmclient"
	"github.com/example/llm-gateway/internal/protocol"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/example/llm-gateway/internal/providers/openai"
	"github.com/gin-gonic/gin"
)

// chatNativeJSONPayload 含 seed / top_p 等 core.ChatResponse 结构里没有的字段：
// 原样透传必须一个字节都不丢。
const chatNativeJSONPayload = `{"id":"chatcmpl-native","object":"chat.completion","created":1726000000,"model":"native-model","choices":[{"index":0,"message":{"role":"assistant","content":"native hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10},"seed":42,"top_p":0.9}`

// chatNativeSSEPayload 是上游返回的 OpenAI SSE 原文。
const chatNativeSSEPayload = "" +
	"data: {\"id\":\"chatcmpl-native\",\"object\":\"chat.completion.chunk\",\"model\":\"native-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"native hello\"},\"finish_reason\":null}],\"seed\":42}\n\n" +
	"data: [DONE]\n\n"

// chatNativeFakeOrchestrator 同时实现两条路径，用计数器区分走了哪条。
type chatNativeFakeOrchestrator struct {
	nativeBodyErr   error
	nativeStreamErr error

	nativeCalls       int
	nativeStreamCalls int
	chatCalls         int
	streamCalls       int

	lastChatReq *core.ChatRequest // 记录降级路径实际收到的请求（验证 body 已还原）
	lastFormat  core.NativeRelayFormat
}

func (f *chatNativeFakeOrchestrator) Chat(_ context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	f.chatCalls++
	f.lastChatReq = req
	return &core.ChatResponse{
		ID: "chatcmpl-relay", Object: "chat.completion", Model: req.Model,
		Choices: []core.Choice{{Index: 0, FinishReason: "stop",
			Message: core.ResponseMessage{Role: "assistant", Content: "relay hello"}}},
		Usage: core.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12},
	}, nil
}

func (f *chatNativeFakeOrchestrator) ChatStream(_ context.Context, _ *core.ChatRequest) (io.ReadCloser, error) {
	f.streamCalls++
	sse := "" +
		"data: {\"id\":\"chatcmpl-relay\",\"object\":\"chat.completion.chunk\",\"model\":\"relay-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"relay hello\"},\"finish_reason\":null}]}\n\n" +
		"data: [DONE]\n\n"
	return io.NopCloser(strings.NewReader(sse)), nil
}

func (f *chatNativeFakeOrchestrator) ChatNative(_ context.Context, _ *core.ChatRequest, _ []byte, format core.NativeRelayFormat) ([]byte, error) {
	f.nativeCalls++
	f.lastFormat = format
	if f.nativeBodyErr != nil {
		return nil, f.nativeBodyErr
	}
	return []byte(chatNativeJSONPayload), nil
}

func (f *chatNativeFakeOrchestrator) ChatNativeStream(_ context.Context, _ *core.ChatRequest, _ []byte, format core.NativeRelayFormat) (io.ReadCloser, error) {
	f.nativeStreamCalls++
	f.lastFormat = format
	if f.nativeStreamErr != nil {
		return nil, f.nativeStreamErr
	}
	return io.NopCloser(strings.NewReader(chatNativeSSEPayload)), nil
}

// newChatNativeRouter 构造 /v1/chat/completions 路由。
func newChatNativeRouter(orch handler.ChatOrchestratorInterface) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := handler.NewChatHandler(orch)
	// 原生旁路默认关闭（legacy），这里显式开启 active。
	h.SetProtocolPolicy(activePolicy())
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("key_id", "test-key") })
	r.POST("/v1/chat/completions", h.Handle)
	return r
}

func doChatNativeRequest(t *testing.T, r *gin.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// 用例 1：非流式原生透传 —— 响应体逐字节相同，seed/top_p 不得被清洗掉。
func TestChatNativeE2E_NonStreamPassthrough(t *testing.T) {
	orch := &chatNativeFakeOrchestrator{}
	r := newChatNativeRouter(orch)

	w := doChatNativeRequest(t, r, `{"model":"native-model","messages":[{"role":"user","content":"hi"}],"top_p":0.9,"seed":42}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if got := string(w.Body.Bytes()); got != chatNativeJSONPayload {
		t.Errorf("原生 JSON 被改写：\n got=%q\nwant=%q", got, chatNativeJSONPayload)
	}
	if orch.chatCalls != 0 {
		t.Errorf("原生成功后不应走 Chat，实际调用 %d 次", orch.chatCalls)
	}
	if orch.nativeCalls != 1 {
		t.Errorf("ChatNative 调用次数: got %d want 1", orch.nativeCalls)
	}
	if orch.lastFormat != core.NativeRelayOpenAIChat {
		t.Errorf("原生格式: got %q want %q", orch.lastFormat, core.NativeRelayOpenAIChat)
	}
}

// 用例 2：流式原生透传 —— SSE 逐字节相同。
func TestChatNativeE2E_StreamPassthrough(t *testing.T) {
	orch := &chatNativeFakeOrchestrator{}
	r := newChatNativeRouter(orch)

	w := doChatNativeRequest(t, r, `{"model":"native-model","messages":[{"role":"user","content":"hi"}],"stream":true}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type: got %q want text/event-stream", ct)
	}
	if got := string(w.Body.Bytes()); got != chatNativeSSEPayload {
		t.Errorf("原生 SSE 被改写：\n got=%q\nwant=%q", got, chatNativeSSEPayload)
	}
	if orch.streamCalls != 0 {
		t.Errorf("原生成功后不应走 ChatStream，实际调用 %d 次", orch.streamCalls)
	}
	if orch.nativeStreamCalls != 1 {
		t.Errorf("ChatNativeStream 调用次数: got %d want 1", orch.nativeStreamCalls)
	}
}

// 用例 3：降级 —— ChatNative 返回包装了哨兵的错误时，必须落到原 Chat 路径，
// 且原有逻辑读到的 body 与改动前完全一致（否则 ShouldBindJSON 会 400）。
func TestChatNativeE2E_FallbackOnNotSupported(t *testing.T) {
	t.Run("非流式", func(t *testing.T) {
		orch := &chatNativeFakeOrchestrator{nativeBodyErr: fmt.Errorf("wrap: %w", core.ErrNativeNotSupported)}
		r := newChatNativeRouter(orch)

		w := doChatNativeRequest(t, r, `{"model":"native-model","messages":[{"role":"user","content":"hi"}]}`)

		if w.Code != http.StatusOK {
			t.Fatalf("降级应成功，status=%d body=%s", w.Code, w.Body.String())
		}
		var resp core.ChatResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("降级响应不是 Chat JSON: %v body=%s", err, w.Body.String())
		}
		if resp.ID != "chatcmpl-relay" {
			t.Errorf("降级响应 id: got %q want chatcmpl-relay", resp.ID)
		}
		if orch.nativeCalls != 1 {
			t.Errorf("ChatNative 调用次数: got %d want 1", orch.nativeCalls)
		}
		if orch.chatCalls != 1 {
			t.Errorf("降级后 Chat 调用次数: got %d want 1", orch.chatCalls)
		}
		if orch.lastChatReq == nil || len(orch.lastChatReq.Messages) != 1 {
			t.Fatalf("降级路径没拿到还原后的 body: %+v", orch.lastChatReq)
		}
	})

	t.Run("流式", func(t *testing.T) {
		orch := &chatNativeFakeOrchestrator{nativeStreamErr: fmt.Errorf("wrap: %w", core.ErrNativeNotSupported)}
		r := newChatNativeRouter(orch)

		w := doChatNativeRequest(t, r, `{"model":"native-model","messages":[{"role":"user","content":"hi"}],"stream":true}`)

		if w.Code != http.StatusOK {
			t.Fatalf("降级应成功，status=%d body=%s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "relay hello") {
			t.Errorf("降级输出缺少原路径内容: %.300s", w.Body.String())
		}
		if orch.nativeStreamCalls != 1 {
			t.Errorf("ChatNativeStream 调用次数: got %d want 1", orch.nativeStreamCalls)
		}
		if orch.streamCalls != 1 {
			t.Errorf("降级后 ChatStream 调用次数: got %d want 1", orch.streamCalls)
		}
	})
}

// 用例 4：真实错误不降级 —— 必须是 OpenAI 错误 JSON，且原 Chat 一次都不许调。
func TestChatNativeE2E_ErrorNoFallback(t *testing.T) {
	orch := &chatNativeFakeOrchestrator{nativeBodyErr: errors.New("upstream native 500")}
	r := newChatNativeRouter(orch)

	w := doChatNativeRequest(t, r, `{"model":"native-model","messages":[{"role":"user","content":"hi"}]}`)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d want 500, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("错误响应不是 OpenAI JSON: %v body=%s", err, w.Body.String())
	}
	if resp.Error.Message == "" {
		t.Errorf("错误响应缺少 error.message: %s", w.Body.String())
	}
	if orch.chatCalls != 0 {
		t.Errorf("真实错误不应降级，Chat 调用次数: got %d want 0", orch.chatCalls)
	}
}

// 用例 5（回归保护）：orchestrator 不实现 NativeChatOrchestratorInterface 时，
// 输出必须与改动前逐字节一致 —— fakeChatOrchestrator（chat_test.go）正是这种实现。
func TestChatNativeE2E_RegressionNonNativeOrchestrator(t *testing.T) {
	chatResp := &core.ChatResponse{
		ID: "chatcmpl-123", Object: "chat.completion", Model: "gpt-4",
		Choices: []core.Choice{{Index: 0, FinishReason: "stop",
			Message: core.ResponseMessage{Role: "assistant", Content: "Hi there!"}}},
		Usage: core.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}
	orch := &fakeChatOrchestrator{chatResp: chatResp}
	r := newChatNativeRouter(orch)

	w := doChatNativeRequest(t, r, `{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	want, err := json.Marshal(chatResp)
	if err != nil {
		t.Fatalf("marshal want: %v", err)
	}
	if string(w.Body.Bytes()) != string(want) {
		t.Errorf("无原生能力时输出变了：\n got=%s\nwant=%s", w.Body.Bytes(), want)
	}

	// 流式同样不受影响。
	streamOrch := &fakeChatOrchestrator{streamBody: io.NopCloser(strings.NewReader("data: {\"id\":\"chatcmpl-1\"}\n\ndata: [DONE]\n\n"))}
	ws := doChatNativeRequest(t, newChatNativeRouter(streamOrch), `{"model":"gpt-4","messages":[{"role":"user","content":"hello"}],"stream":true}`)
	if ws.Code != http.StatusOK {
		t.Fatalf("stream status=%d body=%s", ws.Code, ws.Body.String())
	}
	if !strings.Contains(ws.Header().Get("Content-Type"), "text/event-stream") {
		t.Errorf("Content-Type: %q", ws.Header().Get("Content-Type"))
	}
	if !strings.Contains(ws.Body.String(), "chatcmpl-1") {
		t.Errorf("流式输出异常: %s", ws.Body.String())
	}
}

// 用例 6：错误降级条件互不串门 —— UnifiedError 原样映射，不当成「不支持」。
func TestChatNativeE2E_UnifiedErrorNoFallback(t *testing.T) {
	orch := &chatNativeFakeOrchestrator{nativeBodyErr: model.ErrModelNotFound}
	r := newChatNativeRouter(orch)

	w := doChatNativeRequest(t, r, `{"model":"bad-model","messages":[{"role":"user","content":"hi"}]}`)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d want 404, body=%s", w.Code, w.Body.String())
	}
	if orch.chatCalls != 0 {
		t.Errorf("UnifiedError 不应降级，Chat 调用次数: got %d want 0", orch.chatCalls)
	}
}

// 用例 7（真链路）：handler → 真 ChatOrchestrator → 真 providers.Router →
// 真 openai.Provider → httptest 假上游。前面用例用 fake orchestrator 验证 handler
// 分支，这里验证 26 个 openai-compatible 渠道是否真的自动继承了原生能力。
func TestChatNativeE2E_FullChainOpenAIProvider(t *testing.T) {
	var gotPath string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(chatNativeJSONPayload))
	}))
	defer srv.Close()

	p := openai.NewWithHTTPClient("test-key", srv.Client(), llmclient.Hooks{})
	p.SetBaseURL(srv.URL + "/v1")

	lookup := &nativeChainLookup{model: "gpt-4o", provider: p}
	router, err := providers.NewRouter(lookup)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	orch := orchestrator.NewChatOrchestrator(router)

	gin.SetMode(gin.TestMode)
	h := handler.NewChatHandler(orch)
	h.SetProtocolPolicy(activePolicy())
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("key_id", "test-key") })
	r.POST("/v1/chat/completions", h.Handle)

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],"top_p":0.9,"seed":42}`
	w := doChatNativeRequest(t, r, body)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if got := string(w.Body.Bytes()); got != chatNativeJSONPayload {
		t.Errorf("客户端收到的不是上游原始字节：\n got=%q\nwant=%q", got, chatNativeJSONPayload)
	}
	// Router 会把 body.model 改写为解析后的模型名（现有行为），因此这里是
	// 语义相同而非逐字节相同；字节级不变由 provider 单测守住。
	var gotFields, wantFields map[string]any
	if err := json.Unmarshal(gotBody, &gotFields); err != nil {
		t.Fatalf("上游 body 不是 JSON: %v body=%s", err, gotBody)
	}
	if err := json.Unmarshal([]byte(body), &wantFields); err != nil {
		t.Fatalf("测试 body 不是 JSON: %v", err)
	}
	if !reflect.DeepEqual(gotFields, wantFields) {
		t.Errorf("上游收到的 body 被改写：\n got=%v\nwant=%v", gotFields, wantFields)
	}
	if gotPath != "/v1/chat/completions" {
		t.Errorf("上游 path: got %q want /v1/chat/completions", gotPath)
	}
}

// activePolicy 构造把 test-key 设为 active 的协议策略（原生旁路需显式开启）。
func activePolicy() *protocol.Policy {
	p, err := protocol.LoadPolicy(protocol.PolicyConfig{
		DefaultMode: "active",
		PerKey:      map[string]string{"test-key": "active"},
	})
	if err != nil {
		panic(err)
	}
	return p
}

// TestChatNativeE2E_DisabledByDefault 是这条链路的**上线护栏**：
// 即使 orchestrator 具备原生能力，只要没有显式开启 active，就必须完全走原有
// 转换路径。Chat 是流量最大的入口，这个默认值决定了上线是否改变既有行为。
func TestChatNativeE2E_DisabledByDefault(t *testing.T) {
	orch := &chatNativeFakeOrchestrator{}
	gin.SetMode(gin.TestMode)
	h := handler.NewChatHandler(orch)
	// 刻意不调用 SetProtocolPolicy —— 模拟生产默认配置。
	r := gin.New()
	r.POST("/v1/chat/completions", h.Handle)

	w := doChatNativeRequest(t, r, `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if orch.chatCalls != 1 {
		t.Errorf("默认配置下必须走原有转换路径，Chat 调用 = %d, want 1", orch.chatCalls)
	}
	if orch.nativeCalls != 0 {
		t.Errorf("默认配置下不得走原生旁路，ChatNative 调用 = %d, want 0", orch.nativeCalls)
	}
}

// TestChatNativeE2E_LegacyModeAlsoDisabled 显式 legacy 模式同样不走原生。
func TestChatNativeE2E_LegacyModeAlsoDisabled(t *testing.T) {
	orch := &chatNativeFakeOrchestrator{}
	gin.SetMode(gin.TestMode)
	h := handler.NewChatHandler(orch)
	legacy, err := protocol.LoadPolicy(protocol.PolicyConfig{DefaultMode: "legacy"})
	if err != nil {
		t.Fatalf("LoadPolicy: %v", err)
	}
	h.SetProtocolPolicy(legacy)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("key_id", "test-key") })
	r.POST("/v1/chat/completions", h.Handle)

	w := doChatNativeRequest(t, r, `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if orch.nativeCalls != 0 {
		t.Errorf("legacy 模式不得走原生旁路，ChatNative = %d", orch.nativeCalls)
	}
	if orch.chatCalls != 1 {
		t.Errorf("legacy 模式应走原有路径，Chat = %d", orch.chatCalls)
	}
}
