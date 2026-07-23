package openai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/llmclient"
)

// azureToolsRejectionBody 复刻 Azure OpenAI 推理部署在 /chat/completions 上
// 拒绝 function tools 时的真实响应体（取自 gpt-6-astra 生产日志）。
const azureToolsRejectionBody = `{"error":{"message":"Function tools with reasoning_effort are not supported for 4476-gpt-6-astra-xlab in /v1/chat/completions. To use function tools, use /v1/responses or set reasoning_effort to 'none'.","type":"invalid_request_error","param":"reasoning_effort","code":null}}`

const responsesCompletedBody = `{"id":"resp_1","object":"response","status":"completed","model":"4476-gpt-6-astra-xlab","output":[{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"正常"}]}]}`

func TestIsResponsesRequiredForToolsError(t *testing.T) {
	gatewayErr := func(status int, message string) error {
		return &core.GatewayError{Type: core.ErrorTypeInvalidRequest, StatusCode: status, Message: message, Provider: "azure"}
	}

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil 错误", err: nil, want: false},
		{name: "Azure 真实拒绝文案", err: gatewayErr(http.StatusBadRequest, "Function tools with reasoning_effort are not supported for 4476-gpt-6-astra-xlab in /v1/chat/completions. To use function tools, use /v1/responses or set reasoning_effort to 'none'."), want: true},
		{name: "大小写不敏感", err: gatewayErr(http.StatusBadRequest, "TO USE FUNCTION TOOLS, USE /V1/RESPONSES"), want: true},
		{name: "普通参数错误不重试", err: gatewayErr(http.StatusBadRequest, "Unsupported value: 'reasoning_effort' does not support 'minimal' with this model."), want: false},
		{name: "提到 responses 但非工具场景不重试", err: gatewayErr(http.StatusBadRequest, "invalid request: /v1/responses payload malformed"), want: false},
		{name: "提到工具但未指路 responses 不重试", err: gatewayErr(http.StatusBadRequest, "tools are not supported for this deployment"), want: false},
		{name: "非 400 不重试", err: gatewayErr(http.StatusInternalServerError, "Function tools ... use /v1/responses"), want: false},
		{name: "非 GatewayError 不重试", err: errors.New("Function tools ... use /v1/responses"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsResponsesRequiredForToolsError(tt.err); got != tt.want {
				t.Fatalf("IsResponsesRequiredForToolsError() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Message 为空时回退读 Body，确保上游 body 未解析出 message 的情况也能识别。
func TestIsResponsesRequiredForToolsErrorFallsBackToBody(t *testing.T) {
	err := &core.GatewayError{
		StatusCode: http.StatusBadRequest,
		Body:       []byte(azureToolsRejectionBody),
	}
	if !IsResponsesRequiredForToolsError(err) {
		t.Fatal("Message 为空时应回退读 Body 并被识别")
	}
}

func TestToolsRequireResponsesMemo(t *testing.T) {
	p := &CompatibleProvider{providerName: "azure"}
	if p.toolsRequireResponses("m1") {
		t.Fatal("初始状态不应命中记忆")
	}
	p.markToolsRequireResponses("m1")
	if !p.toolsRequireResponses("m1") {
		t.Fatal("标记后应命中记忆")
	}
	if p.toolsRequireResponses("m2") {
		t.Fatal("记忆应按模型隔离")
	}
	// 空模型名不应写入，避免把结论错误地套用到所有请求上。
	p.markToolsRequireResponses("")
	if p.toolsRequireResponses("") {
		t.Fatal("空模型名不应被记录")
	}
}

func chatRequestWithTools(model string, withTools bool) *core.ChatRequest {
	req := &core.ChatRequest{
		Model:    model,
		Messages: []core.Message{{Role: "user", Content: "北京天气？"}},
	}
	if withTools {
		req.Tools = []map[string]any{{
			"type":     "function",
			"function": map[string]any{"name": "get_weather"},
		}}
	}
	return req
}

func newMockProvider(t *testing.T, chatStatus int, chatBody string) (*CompatibleProvider, *int32, *int32) {
	t.Helper()
	var chatCalls, responsesCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/chat/completions":
			atomic.AddInt32(&chatCalls, 1)
			w.WriteHeader(chatStatus)
			_, _ = w.Write([]byte(chatBody))
		case "/responses":
			atomic.AddInt32(&responsesCalls, 1)
			_, _ = w.Write([]byte(responsesCompletedBody))
		default:
			t.Errorf("意外的请求路径: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	p := NewCompatibleProviderWithHTTPClient("test-key", srv.Client(), llmclient.Hooks{}, CompatibleProviderConfig{
		ProviderName: "azure",
		BaseURL:      srv.URL,
	})
	return p, &chatCalls, &responsesCalls
}

// 带工具的请求被上游 400 拒绝后，应自动改走 /responses 并对客户端返回正常结果。
func TestChatCompletionSelfHealsToResponsesWhenToolsRejected(t *testing.T) {
	p, chatCalls, responsesCalls := newMockProvider(t, http.StatusBadRequest, azureToolsRejectionBody)

	resp, err := p.ChatCompletion(context.Background(), chatRequestWithTools("4476-gpt-6-astra-xlab", true))
	if err != nil {
		t.Fatalf("应自愈成功，实际报错: %v", err)
	}
	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content != "正常" {
		t.Fatalf("自愈后的响应内容不正确: %+v", resp.Choices)
	}
	if got := atomic.LoadInt32(chatCalls); got != 1 {
		t.Fatalf("chat/completions 调用次数 = %d, want 1", got)
	}
	if got := atomic.LoadInt32(responsesCalls); got != 1 {
		t.Fatalf("responses 调用次数 = %d, want 1", got)
	}
	if !p.toolsRequireResponses("4476-gpt-6-astra-xlab") {
		t.Fatal("自愈成功后应记住该部署需要 /responses")
	}
}

// 记忆生效后，后续带工具的请求应直接走 /responses，不再重复撞 400。
func TestChatCompletionSkipsRetryOnSecondToolCall(t *testing.T) {
	p, chatCalls, responsesCalls := newMockProvider(t, http.StatusBadRequest, azureToolsRejectionBody)

	for i := 0; i < 2; i++ {
		if _, err := p.ChatCompletion(context.Background(), chatRequestWithTools("4476-gpt-6-astra-xlab", true)); err != nil {
			t.Fatalf("第 %d 次调用应成功: %v", i+1, err)
		}
	}
	if got := atomic.LoadInt32(chatCalls); got != 1 {
		t.Fatalf("chat/completions 只应被撞一次，实际 %d 次", got)
	}
	if got := atomic.LoadInt32(responsesCalls); got != 2 {
		t.Fatalf("responses 调用次数 = %d, want 2", got)
	}
}

// 不带工具的请求即使命中了记忆，也不应改走 /responses（保持原有链路）。
func TestChatCompletionWithoutToolsKeepsChatPath(t *testing.T) {
	p, chatCalls, responsesCalls := newMockProvider(t, http.StatusBadRequest, azureToolsRejectionBody)
	p.markToolsRequireResponses("4476-gpt-6-astra-xlab")

	if _, err := p.ChatCompletion(context.Background(), chatRequestWithTools("4476-gpt-6-astra-xlab", false)); err == nil {
		t.Fatal("不带工具时不应自愈，应透传上游 400")
	}
	if got := atomic.LoadInt32(chatCalls); got != 1 {
		t.Fatalf("chat/completions 调用次数 = %d, want 1", got)
	}
	if got := atomic.LoadInt32(responsesCalls); got != 0 {
		t.Fatalf("不带工具时不应调用 responses，实际 %d 次", got)
	}
}

// 与工具无关的 400 必须原样透传，不能触发自愈重试。
func TestChatCompletionDoesNotRetryUnrelatedBadRequest(t *testing.T) {
	p, chatCalls, responsesCalls := newMockProvider(t, http.StatusBadRequest,
		`{"error":{"message":"Unsupported value: 'reasoning_effort' does not support 'minimal' with this model.","type":"invalid_request_error"}}`)

	_, err := p.ChatCompletion(context.Background(), chatRequestWithTools("4476-gpt-6-astra-xlab", true))
	if err == nil {
		t.Fatal("无关 400 应返回错误")
	}
	if !strings.Contains(err.Error(), "minimal") {
		t.Fatalf("应透传上游原始错误，实际: %v", err)
	}
	if got := atomic.LoadInt32(chatCalls); got != 1 {
		t.Fatalf("chat/completions 调用次数 = %d, want 1", got)
	}
	if got := atomic.LoadInt32(responsesCalls); got != 0 {
		t.Fatalf("无关 400 不应触发 responses 调用，实际 %d 次", got)
	}
}

// 流式：400 发生在响应头阶段，此时还没发出任何 SSE，改走 Responses 重发是安全的。
func TestStreamChatCompletionSelfHealsToResponses(t *testing.T) {
	const streamBody = "event: response.output_text.delta\n" +
		`data: {"type":"response.output_text.delta","delta":"正常"}` + "\n\n" +
		"event: response.completed\n" +
		`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","status":"completed","model":"4476-gpt-6-astra-xlab","output":[]}}` + "\n\n"

	var chatCalls, responsesCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/completions":
			atomic.AddInt32(&chatCalls, 1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(azureToolsRejectionBody))
		case "/responses":
			atomic.AddInt32(&responsesCalls, 1)
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(streamBody))
		default:
			t.Errorf("意外的请求路径: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	p := NewCompatibleProviderWithHTTPClient("test-key", srv.Client(), llmclient.Hooks{}, CompatibleProviderConfig{
		ProviderName: "azure",
		BaseURL:      srv.URL,
	})

	stream, err := p.StreamChatCompletion(context.Background(), chatRequestWithTools("4476-gpt-6-astra-xlab", true))
	if err != nil {
		t.Fatalf("流式自愈应成功，实际报错: %v", err)
	}
	defer stream.Close()

	raw := make([]byte, 4096)
	n, _ := stream.Read(raw)
	if got := atomic.LoadInt32(&chatCalls); got != 1 {
		t.Fatalf("chat/completions 调用次数 = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&responsesCalls); got != 1 {
		t.Fatalf("responses 调用次数 = %d, want 1", got)
	}
	if !p.toolsRequireResponses("4476-gpt-6-astra-xlab") {
		t.Fatal("流式自愈后同样应记住该部署")
	}
	if n == 0 {
		t.Fatal("自愈后的流不应为空")
	}
	_ = json.Valid(raw[:n]) // 仅确保读出的是数据而非空流
}
