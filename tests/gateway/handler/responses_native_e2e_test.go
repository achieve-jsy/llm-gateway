package handler_test

// Responses 原生旁路端到端：只伪造 orchestrator，其余走真实 handler。
//
// 这条路径的意义是 Responses → Responses 零转换：provider 声明
// NativeRelayOpenAIResponses 时，客户端原始 body 直发上游，响应原样回传；
// 否则回落到 relaykit 的 Responses→Chat→Responses 转换。

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/example/llm-gateway/internal/protocol"
	"github.com/gin-gonic/gin"
)

// responsesNativeFake 同时实现 Chat 与原生两套接口，用来观察走了哪条路。
type responsesNativeFake struct {
	nativeBody   string
	nativeErr    error
	streamBody   string
	chatCalls    int
	streamCalls  int
	nativeCalls  int
	gotFormat    core.NativeRelayFormat
	gotNativeRaw []byte
}

func (f *responsesNativeFake) Chat(_ context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	f.chatCalls++
	return &core.ChatResponse{ID: "chatcmpl-relay", Model: req.Model, Choices: []core.Choice{{Message: core.ResponseMessage{Role: "assistant", Content: "relaykit hello"}}}}, nil
}

func (f *responsesNativeFake) ChatStream(_ context.Context, _ *core.ChatRequest) (io.ReadCloser, error) {
	f.streamCalls++
	return io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\"}\n\n")), nil
}

func (f *responsesNativeFake) ChatNative(_ context.Context, _ *core.ChatRequest, body []byte, format core.NativeRelayFormat) ([]byte, error) {
	f.nativeCalls++
	f.gotFormat = format
	f.gotNativeRaw = append([]byte(nil), body...)
	if f.nativeErr != nil {
		return nil, f.nativeErr
	}
	return []byte(f.nativeBody), nil
}

func (f *responsesNativeFake) ChatNativeStream(_ context.Context, _ *core.ChatRequest, body []byte, format core.NativeRelayFormat) (io.ReadCloser, error) {
	f.nativeCalls++
	f.gotFormat = format
	f.gotNativeRaw = append([]byte(nil), body...)
	if f.nativeErr != nil {
		return nil, f.nativeErr
	}
	return io.NopCloser(strings.NewReader(f.streamBody)), nil
}

// legacyResponsesFake 只实现 ResponsesOrchestratorInterface（legacy 路径要求）。
type legacyResponsesFake struct{}

func (legacyResponsesFake) Responses(_ context.Context, req *core.ResponsesRequest) (*core.ResponsesResponse, error) {
	return &core.ResponsesResponse{ID: "resp_legacy", Model: req.Model}, nil
}

func (legacyResponsesFake) ResponsesStream(_ context.Context, _ *core.ResponsesRequest) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("data: {}\n\n")), nil
}

func newResponsesNativeRouter(t *testing.T, fake *responsesNativeFake) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

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

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("key_id", "test-key") })
	r.POST("/v1/responses", h.Handle)
	return r
}

const responsesNativeJSON = `{"id":"resp_1","object":"response","status":"completed","model":"gpt-5.6-sol","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"native hi"}]}],"usage":{"input_tokens":12,"output_tokens":5,"total_tokens":17}}`

// TestResponsesNativeE2E_NonStreamPassthrough 原生成功时逐字节透传，且不碰 relaykit 转换。
func TestResponsesNativeE2E_NonStreamPassthrough(t *testing.T) {
	fake := &responsesNativeFake{nativeBody: responsesNativeJSON}
	r := newResponsesNativeRouter(t, fake)

	reqBody := `{"model":"gpt-5.6-sol","input":"hello","reasoning":{"effort":"high"},"text":{"verbosity":"low"}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != responsesNativeJSON {
		t.Errorf("响应被改写:\n got=%s\nwant=%s", got, responsesNativeJSON)
	}
	if fake.nativeCalls != 1 {
		t.Errorf("ChatNative 调用次数 = %d, want 1", fake.nativeCalls)
	}
	if fake.gotFormat != core.NativeRelayOpenAIResponses {
		t.Errorf("format = %q, want %q", fake.gotFormat, core.NativeRelayOpenAIResponses)
	}
	if string(fake.gotNativeRaw) != reqBody {
		t.Errorf("原生 body 被改写（应零转换）:\n got=%s\nwant=%s", fake.gotNativeRaw, reqBody)
	}
	if fake.chatCalls != 0 || fake.streamCalls != 0 {
		t.Errorf("原生成功后不应走 relaykit 转换: chat=%d stream=%d", fake.chatCalls, fake.streamCalls)
	}
}

// TestResponsesNativeE2E_FallbackToRelaykit 哨兵错误时回落到 relaykit 转换。
func TestResponsesNativeE2E_FallbackToRelaykit(t *testing.T) {
	fake := &responsesNativeFake{
		nativeErr: fmtWrap(core.ErrNativeNotSupported, "provider does not support responses"),
	}
	r := newResponsesNativeRouter(t, fake)

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.6-sol","input":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("降级后应返回 200，got %d: %s", w.Code, w.Body.String())
	}
	if fake.chatCalls != 1 {
		t.Errorf("降级路径应调用 Chat 一次，got %d", fake.chatCalls)
	}
	if !strings.Contains(w.Body.String(), "relaykit hello") {
		t.Errorf("降级响应缺少 relaykit 内容: %s", w.Body.String())
	}
}

// TestResponsesNativeE2E_ErrorNoFallback 真实错误不降级（避免二次打上游）。
func TestResponsesNativeE2E_ErrorNoFallback(t *testing.T) {
	fake := &responsesNativeFake{nativeErr: errors.New("upstream exploded")}
	r := newResponsesNativeRouter(t, fake)

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.6-sol","input":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Errorf("真实错误不应返回 200: %s", w.Body.String())
	}
	if fake.chatCalls != 0 || fake.streamCalls != 0 {
		t.Errorf("真实错误不得降级: chat=%d stream=%d", fake.chatCalls, fake.streamCalls)
	}
}

// TestResponsesNativeE2E_StreamPassthrough 流式原生透传。
func TestResponsesNativeE2E_StreamPassthrough(t *testing.T) {
	frames := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"native"}`,
		`data: {"type":"response.completed"}`,
	}, "\n\n") + "\n\n"
	fake := &responsesNativeFake{streamBody: frames}
	r := newResponsesNativeRouter(t, fake)

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.6-sol","input":"hello","stream":true}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != frames {
		t.Errorf("SSE 被改写:\n got=%q\nwant=%q", got, frames)
	}
	if !strings.Contains(w.Header().Get("Content-Type"), "text/event-stream") {
		t.Errorf("Content-Type = %q", w.Header().Get("Content-Type"))
	}
	if fake.streamCalls != 0 {
		t.Errorf("原生成功后不应走 relaykit ChatStream, got %d", fake.streamCalls)
	}
}

// fmtWrap 构造一个包装了哨兵的错误，模拟 providers 包的返回形态。
func fmtWrap(target error, msg string) error {
	return &wrapped{msg: msg, err: target}
}

type wrapped struct {
	msg string
	err error
}

func (w *wrapped) Error() string { return w.msg + ": " + w.err.Error() }
func (w *wrapped) Unwrap() error { return w.err }
