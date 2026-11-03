package deepseek

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers"
)

// TestDeepSeekRelayNativePassthrough 验证原始 body 零转换直达上游。
// deepseek 用私有 llmclient，走 providers 包的公共 helper，这条用例守住接线正确。
func TestDeepSeekRelayNativePassthrough(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody []byte
	upstreamResp := `{"id":"cmpl-1","choices":[{"message":{"role":"assistant","content":"hi"}}],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, upstreamResp)
	}))
	defer srv.Close()

	p := New(providers.ProviderConfig{BaseURL: srv.URL, APIKey: "sk-test"}, providers.ProviderOptions{}).(*Provider)

	// 带 Chat 专有字段，验证零转换（结构体往返会丢它们）
	body := `{"model":"deepseek-chat","messages":[{"role":"user","content":"hi"}],"seed":42,"logit_bias":{"50256":-100},"response_format":{"type":"json_object"}}`
	out, err := p.RelayNative(context.Background(), []byte(body))
	if err != nil {
		t.Fatalf("RelayNative: %v", err)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("path = %q, want /chat/completions", gotPath)
	}
	if !strings.Contains(gotAuth, "sk-test") {
		t.Errorf("Authorization = %q, 应带 API Key", gotAuth)
	}
	if string(gotBody) != body {
		t.Errorf("body 未逐字节透传: got=%s want=%s", gotBody, body)
	}
	if string(out) != upstreamResp {
		t.Errorf("响应未原样返回: %s", out)
	}
}

// TestDeepSeekRelayNativeStreamPassthrough 验证流式 SSE 原样透传。
func TestDeepSeekRelayNativeStreamPassthrough(t *testing.T) {
	frames := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"hi"}}]}`,
		`data: {"choices":[{"delta":{"content":" there"}}]}`,
	}, "\n\n") + "\n\n"
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, frames)
	}))
	defer srv.Close()

	p := New(providers.ProviderConfig{BaseURL: srv.URL, APIKey: "sk-test"}, providers.ProviderOptions{}).(*Provider)
	rc, err := p.RelayNativeStream(context.Background(), []byte(`{"model":"deepseek-chat","messages":[],"stream":true}`))
	if err != nil {
		t.Fatalf("RelayNativeStream: %v", err)
	}
	defer rc.Close()
	out, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("读取流: %v", err)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("path = %q", gotPath)
	}
	if string(out) != frames {
		t.Errorf("SSE 未逐字节透传: got=%q want=%q", out, frames)
	}
}

// TestDeepSeekNativeRelayFormatFor 确认能力判定不会误接其他协议。
func TestDeepSeekNativeRelayFormatFor(t *testing.T) {
	p := &Provider{}
	if _, ok := p.NativeRelayFormatFor("deepseek-chat", core.NativeRelayOpenAIChat); !ok {
		t.Error("应支持 Chat 原生格式")
	}
	if _, ok := p.NativeRelayFormatFor("deepseek-chat", core.NativeRelayClaude); ok {
		t.Error("不应声明支持 Claude 格式")
	}
	if _, ok := p.NativeRelayFormatFor("deepseek-chat", core.NativeRelayGemini); ok {
		t.Error("不应声明支持 Gemini 格式")
	}
}
