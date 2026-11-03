package gemini

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/llmclient"
)

// newRelayTestProvider 构造一个把 nativeClient 指向测试服务器的 provider。
// SetModelsURL 同时改 nativeClient 的 BaseURL，正好把 Gemini 原生端点接到 httptest。
func newRelayTestProvider(t *testing.T, srv *httptest.Server) *Provider {
	t.Helper()
	p := NewWithHTTPClient("test-key", srv.Client(), llmclient.Hooks{})
	p.SetModelsURL(srv.URL)
	return p
}

func TestNativeRelayFormatFor(t *testing.T) {
	p := &Provider{}
	cases := []struct {
		name   string
		model  string
		format core.NativeRelayFormat
		want   bool
	}{
		{"aistudio gemini", "gemini-2.5-pro", core.NativeRelayGemini, true},
		{"models prefix", "models/gemini-2.5-flash", core.NativeRelayGemini, true},
		{"google prefix", "google/gemini-2.5-pro", core.NativeRelayGemini, true},
		{"vertex publisher path", "projects/p/locations/us-central1/publishers/google/models/gemini-2.5-pro", core.NativeRelayGemini, true},
		{"claude", "claude-sonnet-4-5", core.NativeRelayGemini, false},
		{"vertex anthropic path", "publishers/anthropic/models/claude-sonnet-4-5", core.NativeRelayGemini, false},
		{"gpt", "gpt-4o", core.NativeRelayGemini, false},
		{"empty", "  ", core.NativeRelayGemini, false},
		{"other format", "gemini-2.5-pro", core.NativeRelayClaude, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotFormat, ok := p.NativeRelayFormatFor(tc.model, tc.format)
			if ok != tc.want {
				t.Fatalf("NativeRelayFormatFor(%q, %q) ok = %v, want %v", tc.model, tc.format, ok, tc.want)
			}
			if ok && gotFormat != core.NativeRelayGemini {
				t.Fatalf("format = %q, want %q", gotFormat, core.NativeRelayGemini)
			}
		})
	}
}

// 原生旁路的核心断言：URL 走 :generateContent、body 里 model 被摘掉、
// generationConfig 的 topK/topP 等 Gemini 原生字段原样到达上游。
func TestRelayNative(t *testing.T) {
	upstreamResp := []byte("{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hi\"}],\"role\":\"model\"}}],\"usageMetadata\":{\"promptTokenCount\":3}}")

	var gotPath string
	var gotBody string
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(upstreamResp)
	}))
	defer srv.Close()

	p := newRelayTestProvider(t, srv)
	reqBody := []byte("{\"contents\":[{\"role\":\"user\",\"parts\":[{\"text\":\"hi\"}]}]," +
		"\"generationConfig\":{\"topK\":40,\"topP\":0.95,\"temperature\":0.3}," +
		"\"model\":\"gemini-2.5-pro\"}")

	got, err := p.RelayNative(context.Background(), reqBody)
	if err != nil {
		t.Fatalf("RelayNative error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if want := "/models/gemini-2.5-pro:generateContent"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	// model 是 Router 为路由写进去的，Gemini 协议不认这个顶层字段，必须删掉。
	if strings.Contains(gotBody, "\"model\"") {
		t.Errorf("upstream body must not contain model field: %s", gotBody)
	}
	// 原生字段必须逐字节保留（不能被 core.ChatRequest 中转掉）。
	for _, want := range []string{`"topK":40`, `"topP":0.95`, `"contents"`} {
		if !strings.Contains(gotBody, want) {
			t.Errorf("upstream body missing %s: %s", want, gotBody)
		}
	}
	if string(got) != string(upstreamResp) {
		t.Errorf("response not passed through:\n got %s\nwant %s", got, upstreamResp)
	}
}

func TestRelayNativeStream(t *testing.T) {
	frames := "" +
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"he\"}]}}]}\n\n" +
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"llo\"}]}}]}\n\n"

	var gotPath, gotQuery, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, frames)
		w.(http.Flusher).Flush()
	}))
	defer srv.Close()

	p := newRelayTestProvider(t, srv)
	reqBody := []byte("{\"contents\":[{\"parts\":[{\"text\":\"hi\"}]}],\"model\":\"gemini-2.5-pro\"}")

	stream, err := p.RelayNativeStream(context.Background(), reqBody)
	if err != nil {
		t.Fatalf("RelayNativeStream error: %v", err)
	}
	got, err := io.ReadAll(stream)
	closeErr := stream.Close()
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	if closeErr != nil {
		t.Fatalf("close stream: %v", closeErr)
	}
	if want := "/models/gemini-2.5-pro:streamGenerateContent"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotQuery != "alt=sse" {
		t.Errorf("query = %q, want alt=sse", gotQuery)
	}
	if strings.Contains(gotBody, "\"model\"") {
		t.Errorf("upstream body must not contain model field: %s", gotBody)
	}
	if string(got) != frames {
		t.Errorf("stream bytes not passed through:\n got %q\nwant %q", got, frames)
	}
}

func TestRelayNativeUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"code":500,"message":"boom","status":"INTERNAL"}}`)
	}))
	defer srv.Close()

	p := newRelayTestProvider(t, srv)
	body := []byte(`{"contents":[{"parts":[{"text":"hi"}]}],"model":"gemini-2.5-pro"}`)

	if _, err := p.RelayNative(context.Background(), body); err == nil {
		t.Fatal("RelayNative: want error on upstream 500, got nil")
	}
	stream, err := p.RelayNativeStream(context.Background(), body)
	if err == nil {
		got, _ := io.ReadAll(stream)
		_ = stream.Close()
		t.Fatalf("RelayNativeStream: want error on upstream 500, got stream with %q", got)
	}
	// 上游错误不能被误判成"不支持原生"从而触发降级。
	if errors.Is(err, core.ErrNativeNotSupported) {
		t.Errorf("upstream error must not wrap ErrNativeNotSupported: %v", err)
	}
}
