package vertex

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers"
)

// newNativeRelayTestProvider 构造一个指向假上游的 vertex provider。
// BaseURL 不带 /endpoints/openapi 后缀时，nativeBaseURL 就是它本身，
// 因此 Gemini 原生端点会直接落在 httptest server 上。
func newNativeRelayTestProvider(t *testing.T, handler http.HandlerFunc) *Provider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	p, ok := New(providers.ProviderConfig{
		BaseURL:  srv.URL,
		APIKey:   "fake-key",
		AuthType: "api_key",
		APIMode:  "native",
	}, providers.ProviderOptions{}).(*Provider)
	if !ok {
		t.Fatal("New 未返回 *vertex.Provider")
	}
	return p
}

// TestVertexNativeRelayFormatFor 钉死：vertex 必须真的把能力判定转发给内部 gemini。
// 少了这层转发，生产上所有 Gemini 流量（全部由 vertex 承载）都拿不到零转换。
func TestVertexNativeRelayFormatFor(t *testing.T) {
	p := newNativeRelayTestProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for _, model := range []string{"gemini-2.5-pro", "gemini-2.5-flash", "models/gemini-2.5-pro", "publishers/google/models/gemini-2.5-pro"} {
		if _, ok := p.NativeRelayFormatFor(model, core.NativeRelayGemini); !ok {
			t.Errorf("model %q 应支持 Gemini 原生格式", model)
		}
	}
	for _, model := range []string{"claude-3-5-sonnet", "gpt-5.6-sol", ""} {
		if _, ok := p.NativeRelayFormatFor(model, core.NativeRelayGemini); ok {
			t.Errorf("model %q 不应被判为支持 Gemini 原生格式", model)
		}
	}
	if _, ok := p.NativeRelayFormatFor("gemini-2.5-pro", core.NativeRelayClaude); ok {
		t.Error("Gemini 模型不应声明支持 Claude 原生格式")
	}
}

// TestVertexRelayNative_ForwardsThroughGemini 验证转发真的到达上游，
// 且 Gemini 原生字段（topK/topP）没有被网关改写。
func TestVertexRelayNative_ForwardsThroughGemini(t *testing.T) {
	var gotPath string
	var gotBody []byte
	upstreamResp := `{"candidates":[{"content":{"parts":[{"text":"hi"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":11,"candidatesTokenCount":4,"totalTokenCount":15}}`
	p := newNativeRelayTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(upstreamResp))
	})

	reqBody := `{"model":"gemini-2.5-pro","contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{"topK":40,"topP":0.95,"temperature":0.7}}`
	out, err := p.RelayNative(context.Background(), []byte(reqBody))
	if err != nil {
		t.Fatalf("RelayNative: %v", err)
	}

	if !strings.HasSuffix(gotPath, "/models/gemini-2.5-pro:generateContent") {
		t.Errorf("path = %q, want 以 /models/gemini-2.5-pro:generateContent 结尾", gotPath)
	}

	var up map[string]any
	if err := json.Unmarshal(gotBody, &up); err != nil {
		t.Fatalf("上游 body 不是 JSON: %v", err)
	}
	if _, exists := up["model"]; exists {
		t.Error("model 字段应被摘除（Gemini 协议把模型放在 URL 里，多余字段会被拒绝）")
	}
	gc, _ := up["generationConfig"].(map[string]any)
	if gc == nil {
		t.Fatal("generationConfig 丢失")
	}
	if v, ok := gc["topK"].(float64); !ok || v != 40 {
		t.Errorf("topK = %v, want 40（零转换的核心证据）", gc["topK"])
	}
	if v, ok := gc["topP"].(float64); !ok || v != 0.95 {
		t.Errorf("topP = %v, want 0.95", gc["topP"])
	}

	if string(out) != upstreamResp {
		t.Errorf("响应未原样透传:\n got=%.200s\nwant=%.200s", out, upstreamResp)
	}
}

// TestVertexRelayNativeStream_ForwardsSSE 验证流式转发与 URL 形态。
func TestVertexRelayNativeStream_ForwardsSSE(t *testing.T) {
	frames := strings.Join([]string{
		`data: {"candidates":[{"content":{"parts":[{"text":"hi"}]}}]}`,
		`data: {"candidates":[{"content":{"parts":[{"text":" there"}]}}]}`,
	}, "\n\n") + "\n\n"
	var gotPath, gotQuery bool
	p := newNativeRelayTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = strings.HasSuffix(r.URL.Path, ":streamGenerateContent")
		gotQuery = r.URL.Query().Get("alt") == "sse"
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(frames))
	})

	rc, err := p.RelayNativeStream(context.Background(), []byte(`{"model":"gemini-2.5-pro","contents":[{"role":"user","parts":[{"text":"hello"}]}]}`))
	if err != nil {
		t.Fatalf("RelayNativeStream: %v", err)
	}
	defer rc.Close()
	out, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("读取流: %v", err)
	}
	if !gotPath {
		t.Error("URL 应形如 :streamGenerateContent")
	}
	if !gotQuery {
		t.Error("流式 URL 必须带 alt=sse")
	}
	if string(out) != frames {
		t.Errorf("SSE 未逐字节透传:\n got=%q\nwant=%q", out, frames)
	}
}

// TestVertexRelayNative_UpstreamError 确认上游失败不会被包装成降级信号。
func TestVertexRelayNative_UpstreamError(t *testing.T) {
	p := newNativeRelayTestProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
	})

	_, err := p.RelayNative(context.Background(), []byte(`{"model":"gemini-2.5-pro"}`))
	if err == nil {
		t.Fatal("上游 500 应返回错误")
	}
	if errorsIs(err, core.ErrNativeNotSupported) {
		t.Error("上游失败不得包装成 ErrNativeNotSupported，否则会被误降级成第二次上游调用")
	}

	_, serr := p.RelayNativeStream(context.Background(), []byte(`{"model":"gemini-2.5-pro"}`))
	if serr == nil {
		t.Fatal("流式上游 500 应返回错误")
	}
	if errorsIs(serr, core.ErrNativeNotSupported) {
		t.Error("流式上游失败不得包装成 ErrNativeNotSupported")
	}
}

func errorsIs(err, target error) bool {
	for e := err; e != nil; {
		if e == target {
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}
