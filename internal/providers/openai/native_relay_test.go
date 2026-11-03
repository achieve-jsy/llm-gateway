package openai

// 原生旁路（NativeRelay）单元测试：openai-compatible 渠道把客户端原始
// Chat Completions / Responses body 逐字节直发上游，响应也逐字节回传。
// 断言重点是「不塌缩进 core.ChatRequest」——top_p / seed / response_format /
// logit_bias 这类字段必须原样出现在上游请求里。

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/llmclient"
)

// nativeTestChatBody 含多个 Chat Completions 专有字段（core.ChatRequest 没有的也会保留）。
const nativeTestChatBody = `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],"top_p":0.9,"seed":42,"response_format":{"type":"json_object"},"logit_bias":{"50256":-100}}`

// nativeTestResponsesBody 是 Responses 请求体：有 input、无 messages。
const nativeTestResponsesBody = `{"model":"gpt-5","input":[{"role":"user","content":"hi"}],"reasoning":{"effort":"low"}}`

// newNativeTestProvider 构造指向假上游的 CompatibleProvider。
func newNativeTestProvider(t *testing.T, srv *httptest.Server, providerName string) *CompatibleProvider {
	t.Helper()
	return NewCompatibleProviderWithHTTPClient("test-key", srv.Client(), llmclient.Hooks{}, CompatibleProviderConfig{
		ProviderName: providerName,
		BaseURL:      srv.URL + "/v1",
		SetHeaders: func(req *http.Request, apiKey string) {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		},
	})
}

// TestNativeOpenAIPath 验证 Chat / Responses 两种 body 的端点判定。
func TestNativeOpenAIPath(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    string
		wantErr bool
	}{
		{"messages → chat", nativeTestChatBody, "/chat/completions", false},
		{"input 无 messages → responses", nativeTestResponsesBody, "/responses", false},
		{"同时有 messages 与 input 时 messages 优先", `{"messages":[],"input":[]}`, "/chat/completions", false},
		{"非法 JSON", `{not json`, "", true},
		{"两者都没有", `{"model":"x"}`, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := nativeOpenAIPath([]byte(tc.body))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("body=%q 应报错，实际 path=%q", tc.body, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("body=%q 不应报错: %v", tc.body, err)
			}
			if got != tc.want {
				t.Errorf("path: got %q want %q", got, tc.want)
			}
		})
	}
}

// TestNativeRelayFormatFor 验证能力声明：Chat/Responses 都吃，Claude/Gemini 不吃。
func TestNativeRelayFormatFor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	p := newNativeTestProvider(t, srv, "openai")

	tests := []struct {
		format core.NativeRelayFormat
		want   bool
	}{
		{core.NativeRelayOpenAIChat, true},
		{core.NativeRelayOpenAIResponses, true},
		{core.NativeRelayClaude, false},
		{core.NativeRelayGemini, false},
	}
	for _, tc := range tests {
		got, ok := p.NativeRelayFormatFor("gpt-4o", tc.format)
		if ok != tc.want {
			t.Errorf("format=%q ok=%v want %v", tc.format, ok, tc.want)
		}
		if ok && got != tc.format {
			t.Errorf("format=%q 返回 %q，应原样返回请求的格式", tc.format, got)
		}
	}
}

// TestRelayNativePassthrough 验证非流式：上游收到的 body 与传入 body 逐字节相同，
// URL 正确，鉴权头沿用 provider 已有配置，响应体逐字节返回。
func TestRelayNativePassthrough(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody []byte
	const upstreamResp = `{"id":"chatcmpl-native","object":"chat.completion","model":"gpt-4o","seed":42,"x-vendor-extra":{"k":"v"}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(upstreamResp))
	}))
	defer srv.Close()

	p := newNativeTestProvider(t, srv, "openai")
	out, err := p.RelayNative(context.Background(), []byte(nativeTestChatBody))
	if err != nil {
		t.Fatalf("RelayNative: %v", err)
	}
	if string(out) != upstreamResp {
		t.Errorf("响应体被改写: got=%q want=%q", out, upstreamResp)
	}
	if string(gotBody) != nativeTestChatBody {
		t.Errorf("请求体被改写:\n got=%q\nwant=%q", gotBody, nativeTestChatBody)
	}
	if gotPath != "/v1/chat/completions" {
		t.Errorf("path: got %q want /v1/chat/completions", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("鉴权头: got %q", gotAuth)
	}
}

// TestRelayNativeResponsesPath 验证 Responses 形态的 body 打到 /v1/responses。
func TestRelayNativeResponsesPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"id":"resp_native","object":"response"}`))
	}))
	defer srv.Close()

	p := newNativeTestProvider(t, srv, "openai")
	if _, err := p.RelayNative(context.Background(), []byte(nativeTestResponsesBody)); err != nil {
		t.Fatalf("RelayNative: %v", err)
	}
	if gotPath != "/v1/responses" {
		t.Errorf("path: got %q want /v1/responses", gotPath)
	}
}

// TestRelayNativeStreamPassthrough 验证流式：返回字节流与上游完全一致。
func TestRelayNativeStreamPassthrough(t *testing.T) {
	const upstreamSSE = "" +
		"data: {\"id\":\"chatcmpl-native\",\"choices\":[{\"delta\":{\"content\":\"native\"}}]}\n\n" +
		"data: [DONE]\n\n"
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(upstreamSSE))
	}))
	defer srv.Close()

	p := newNativeTestProvider(t, srv, "openai")
	stream, err := p.RelayNativeStream(context.Background(), []byte(nativeTestChatBody))
	if err != nil {
		t.Fatalf("RelayNativeStream: %v", err)
	}
	defer stream.Close()
	out, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("读取流: %v", err)
	}
	if string(out) != upstreamSSE {
		t.Errorf("SSE 被改写:\n got=%q\nwant=%q", out, upstreamSSE)
	}
	if gotBody != nativeTestChatBody {
		t.Errorf("流式请求体被改写:\n got=%q\nwant=%q", gotBody, nativeTestChatBody)
	}
	if gotPath != "/v1/chat/completions" {
		t.Errorf("path: got %q want /v1/chat/completions", gotPath)
	}
}

// TestRelayNativeUpstreamError 验证上游 500 返回错误（不吞掉，也不当成成功）。
func TestRelayNativeUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
	}))
	defer srv.Close()

	p := newNativeTestProvider(t, srv, "openai")
	if _, err := p.RelayNative(context.Background(), []byte(nativeTestChatBody)); err == nil {
		t.Fatal("上游 500 应返回错误")
	}
	stream, err := p.RelayNativeStream(context.Background(), []byte(nativeTestChatBody))
	if err == nil {
		stream.Close()
		t.Fatal("上游 500（流式）应返回错误")
	}
	if errors.Is(err, core.ErrNativeNotSupported) {
		t.Errorf("上游错误不应被标成 ErrNativeNotSupported: %v", err)
	}
}

// TestRelayNativeAzureSanitize 验证 azure 渠道的原生出站与 ChatCompletion 一致：
// max_tokens 改写为 max_completion_tokens（azure 新模型拒绝 max_tokens）。
func TestRelayNativeAzureSanitize(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody = map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"id":"chatcmpl-native"}`))
	}))
	defer srv.Close()

	p := newNativeTestProvider(t, srv, "azure")
	body := `{"model":"gpt-5","messages":[{"role":"user","content":"hi"}],"max_tokens":64,"reasoning_summary":"auto"}`
	if _, err := p.RelayNative(context.Background(), []byte(body)); err != nil {
		t.Fatalf("RelayNative: %v", err)
	}
	if _, ok := gotBody["max_tokens"]; ok {
		t.Errorf("azure 出站不应带 max_tokens: %v", gotBody)
	}
	if gotBody["max_completion_tokens"] != float64(64) {
		t.Errorf("max_tokens 应改写为 max_completion_tokens，got=%v", gotBody["max_completion_tokens"])
	}
	if _, ok := gotBody["reasoning_summary"]; ok {
		t.Errorf("azure 出站应剥离 reasoning_summary: %v", gotBody)
	}
}

// TestRelayNativeDeclinesResponsesBridge 验证「旧路径会桥到 /responses」的请求
// 在原生入口被拒绝（包装 ErrNativeNotSupported），由 handler 降级，避免回归。
func TestRelayNativeDeclinesResponsesBridge(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	// gpt-5.4 + tools + reasoning_effort：ShouldBridgeChatToResponsesForProvider 为真。
	body := `{"model":"gpt-5.4","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"f","parameters":{"type":"object"}}}],"reasoning_effort":"low"}`
	p := newNativeTestProvider(t, srv, "openai")
	_, err := p.RelayNative(context.Background(), []byte(body))
	if !errors.Is(err, core.ErrNativeNotSupported) {
		t.Fatalf("err=%v 应可被 errors.Is 命中 core.ErrNativeNotSupported", err)
	}
	if _, err := p.RelayNativeStream(context.Background(), []byte(body)); !errors.Is(err, core.ErrNativeNotSupported) {
		t.Fatalf("流式 err=%v 应命中哨兵", err)
	}
	if hits != 0 {
		t.Errorf("拒绝原生时不应打上游，实际 %d 次", hits)
	}
}
