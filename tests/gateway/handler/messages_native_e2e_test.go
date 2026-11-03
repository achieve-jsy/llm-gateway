package handler_test

// 原生旁路（Native Chat）端到端测试：/v1/messages 在 orchestrator 支持
// NativeChatOrchestratorInterface 时，把客户端原始 Anthropic body 零转换直发上游，
// 响应原样回传；只有 ErrNativeNotSupported 才降级到 relaykit 转换路径。
// fake orchestrator 同时实现转换路径与原生路径，并用计数器区分谁被调用。

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
	"github.com/example/llm-gateway/internal/gateway/cache"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/example/llm-gateway/internal/gateway/orchestrator"
	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/example/llm-gateway/internal/protocol"
	"github.com/gin-gonic/gin"
)

// nativeSSEPayload 是原生上游返回的 Anthropic SSE 原文，测试断言逐字节透传。
const nativeSSEPayload = "" +
	"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_native\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"rk-model\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":7,\"output_tokens\":1}}}\n\n" +
	"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
	"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"native hello\"}}\n\n" +
	"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
	"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":3}}\n\n" +
	"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n" +
	""

// nativeJSONPayload 含 top_k —— relaykit 转换会丢掉它，原样透传必须保留。
const nativeJSONPayload = "{\"id\":\"msg_native\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"rk-model\",\"content\":[{\"type\":\"text\",\"text\":\"native hello\"}],\"stop_reason\":\"end_turn\",\"stop_sequence\":null,\"top_k\":40,\"usage\":{\"input_tokens\":7,\"output_tokens\":3}}"

// nativeFakeOrchestrator 同时实现 ChatOrchestratorInterface 与
// NativeChatOrchestratorInterface，用计数器验证走了哪条路径。
type nativeFakeOrchestrator struct {
	nativeStreamErr error // ChatNativeStream 返回的错误
	nativeBodyErr   error // ChatNative 返回的错误

	nativeStreamCalls int
	nativeCalls       int
	streamCalls       int
	chatCalls         int
}

func (f *nativeFakeOrchestrator) Chat(_ context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	f.chatCalls++
	return &core.ChatResponse{
		ID: "chatcmpl-relay", Object: "chat.completion", Model: req.Model,
		Choices: []core.Choice{{Index: 0, FinishReason: "stop",
			Message: core.ResponseMessage{Role: "assistant", Content: "relaykit hello"}}},
		Usage: core.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12},
	}, nil
}

func (f *nativeFakeOrchestrator) ChatStream(_ context.Context, _ *core.ChatRequest) (io.ReadCloser, error) {
	f.streamCalls++
	sse := "" +
		"data: {\"id\":\"chatcmpl-relay\",\"object\":\"chat.completion.chunk\",\"model\":\"rk-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"relaykit hello\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl-relay\",\"object\":\"chat.completion.chunk\",\"model\":\"rk-model\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"total_tokens\":12}}\n\n" +
		"data: [DONE]\n\n"
	return io.NopCloser(strings.NewReader(sse)), nil
}

func (f *nativeFakeOrchestrator) ChatNative(_ context.Context, _ *core.ChatRequest, _ []byte, _ core.NativeRelayFormat) ([]byte, error) {
	f.nativeCalls++
	if f.nativeBodyErr != nil {
		return nil, f.nativeBodyErr
	}
	return []byte(nativeJSONPayload), nil
}

func (f *nativeFakeOrchestrator) ChatNativeStream(_ context.Context, _ *core.ChatRequest, _ []byte, _ core.NativeRelayFormat) (io.ReadCloser, error) {
	f.nativeStreamCalls++
	if f.nativeStreamErr != nil {
		return nil, f.nativeStreamErr
	}
	return io.NopCloser(strings.NewReader(nativeSSEPayload)), nil
}

// newNativeRouter 构造走 relaykit active 路径的 /v1/messages 路由。
func newNativeRouter(t *testing.T, orch handler.ChatOrchestratorInterface) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	nc := cache.NewGroupNameCache("")
	nc.SetNames([]string{"rk-model"})
	sch := &fakeMessagesScheduler{decision: &scheduler.RouteDecision{
		GroupName: "rk-model", ModelID: "1", ModelName: "rk-model", ProviderName: "p1",
	}}
	gd := orchestrator.NewGroupAwareDispatcher(sch, nc)

	h := handler.NewMessagesHandlerWithGroup(orch, gd)
	policy, err := protocol.LoadPolicy(protocol.PolicyConfig{
		DefaultMode: "active",
		PerKey:      map[string]string{"test-key": "active"},
	})
	if err != nil {
		t.Fatalf("LoadPolicy: %v", err)
	}
	h.SetProtocolPolicy(policy)

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("key_id", "test-key") })
	r.POST("/v1/messages", h.HandleMessages)
	return r
}

func doNativeRequest(t *testing.T, r *gin.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// 用例 1：流式原生 —— 客户端收到的字节必须与上游 fake 返回的完全一致（零改写）。
func TestMessagesNativeE2E_StreamPassthrough(t *testing.T) {
	orch := &nativeFakeOrchestrator{}
	r := newNativeRouter(t, orch)

	w := doNativeRequest(t, r, "{\"model\":\"rk-model\",\"max_tokens\":64,\"stream\":true,\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}")

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type: got %q want text/event-stream", ct)
	}
	if got := w.Body.String(); got != nativeSSEPayload {
		t.Errorf("原生 SSE 被改写：\n got=%q\nwant=%q", got, nativeSSEPayload)
	}
	if orch.streamCalls != 0 {
		t.Errorf("原生成功后不应走 relaykit ChatStream，实际调用 %d 次", orch.streamCalls)
	}
	if orch.nativeStreamCalls != 1 {
		t.Errorf("ChatNativeStream 调用次数: got %d want 1", orch.nativeStreamCalls)
	}
}

// 用例 2：流式降级 —— ChatNativeStream 返回 ErrNativeNotSupported 时
// 必须落到 relaykit 转换路径，客户端拿到合法的 Anthropic SSE 而不是错误响应。
func TestMessagesNativeE2E_StreamFallback(t *testing.T) {
	orch := &nativeFakeOrchestrator{nativeStreamErr: fmt.Errorf("wrap: %w", core.ErrNativeNotSupported)}
	r := newNativeRouter(t, orch)

	w := doNativeRequest(t, r, "{\"model\":\"rk-model\",\"max_tokens\":64,\"stream\":true,\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}")

	if w.Code != http.StatusOK {
		t.Fatalf("降级应成功，status=%d body=%s", w.Code, w.Body.String())
	}
	out := w.Body.String()
	assertAnthropicSSE(t, out)
	if !strings.Contains(out, "event: message_start") {
		t.Errorf("降级输出缺少 message_start: %.500s", out)
	}
	if !strings.Contains(out, "event: message_stop") {
		t.Errorf("降级输出缺少 message_stop: %.500s", out)
	}
	if !strings.Contains(out, "relaykit hello") {
		t.Errorf("降级输出缺少 relaykit 内容: %.500s", out)
	}
	if orch.streamCalls != 1 {
		t.Errorf("降级后 ChatStream 调用次数: got %d want 1", orch.streamCalls)
	}
}

// 用例 3：非流式原生 —— 响应体逐字节透传，top_k 不得被清洗掉。
func TestMessagesNativeE2E_NonStreamPassthrough(t *testing.T) {
	orch := &nativeFakeOrchestrator{}
	r := newNativeRouter(t, orch)

	w := doNativeRequest(t, r, "{\"model\":\"rk-model\",\"max_tokens\":64,\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}")

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if got := strings.TrimSpace(w.Body.String()); got != nativeJSONPayload {
		t.Errorf("原生 JSON 被改写：\n got=%q\nwant=%q", got, nativeJSONPayload)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := resp["top_k"]; !ok {
		t.Errorf("top_k 被删除：%s", w.Body.String())
	}
	if orch.chatCalls != 0 {
		t.Errorf("原生成功后不应走 relaykit Chat，实际调用 %d 次", orch.chatCalls)
	}
	if orch.nativeCalls != 1 {
		t.Errorf("ChatNative 调用次数: got %d want 1", orch.nativeCalls)
	}
}

// 用例 4：原生返回真实错误（非 ErrNativeNotSupported）——
// 必须直接回错误，不能静默降级二次打上游。
func TestMessagesNativeE2E_ErrorNoFallback(t *testing.T) {
	orch := &nativeFakeOrchestrator{nativeStreamErr: errors.New("upstream native 500")}
	r := newNativeRouter(t, orch)

	w := doNativeRequest(t, r, "{\"model\":\"rk-model\",\"max_tokens\":64,\"stream\":true,\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}")

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d want 500, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("错误响应不是 Anthropic JSON: %v body=%s", err, w.Body.String())
	}
	if resp.Type != "error" || resp.Error.Message == "" {
		t.Errorf("错误响应格式不对: %s", w.Body.String())
	}
	if orch.streamCalls != 0 {
		t.Errorf("真实错误不应降级，ChatStream 调用次数: got %d want 0", orch.streamCalls)
	}
}
