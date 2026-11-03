package vertex

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

// 生产形态的 base URL（.../publishers/google）单独钉一遍：
// 既有的转发测试用 base=srv.URL 起假上游，覆盖不到 VertexBaseURLs 推导出
// publishers/google 之后端点会不会拼重/拼漏。这里用真实形状的 base URL，
// 断言最终路径是 /publishers/google/models/<model>:generateContent。
func TestVertexRelayNative_PublishersGoogleBaseURL(t *testing.T) {
	var gotPath, gotQuery, gotKey string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotKey = r.Header.Get("x-goog-api-key")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"hi"}]}}]}`)
	}))
	defer srv.Close()

	p, ok := New(providers.ProviderConfig{
		Type:     "vertex",
		BaseURL:  srv.URL + "/publishers/google",
		APIKey:   "fake-key",
		AuthType: "api_key",
		APIMode:  "native",
	}, providers.ProviderOptions{}).(*Provider)
	if !ok {
		t.Fatal("New 未返回 *vertex.Provider")
	}

	if _, ok := p.NativeRelayFormatFor("gemini-2.5-pro", core.NativeRelayGemini); !ok {
		t.Fatal("gemini-2.5-pro 应支持 Gemini 原生格式")
	}

	body := []byte(`{"model":"gemini-2.5-pro","contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{"topK":40,"topP":0.95}}`)
	if _, err := p.RelayNative(context.Background(), body); err != nil {
		t.Fatalf("RelayNative: %v", err)
	}
	if want := "/publishers/google/models/gemini-2.5-pro:generateContent"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotKey != "fake-key" {
		t.Errorf("x-goog-api-key = %q, want fake-key（Vertex api_key 认证必须到上游）", gotKey)
	}
	if strings.Contains(string(gotBody), `"model"`) {
		t.Errorf("上游 body 不应含 model 字段: %s", gotBody)
	}
	if !strings.Contains(string(gotBody), `"topK":40`) {
		t.Errorf("上游 body 丢失原生 generationConfig: %s", gotBody)
	}

	rc, err := p.RelayNativeStream(context.Background(), body)
	if err != nil {
		t.Fatalf("RelayNativeStream: %v", err)
	}
	_, _ = io.ReadAll(rc)
	_ = rc.Close()
	if want := "/publishers/google/models/gemini-2.5-pro:streamGenerateContent"; gotPath != want {
		t.Errorf("stream path = %q, want %q", gotPath, want)
	}
	if gotQuery != "alt=sse" {
		t.Errorf("stream query = %q, want alt=sse", gotQuery)
	}
}
