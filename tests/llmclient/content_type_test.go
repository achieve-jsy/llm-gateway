package llmclient_test

// 原生旁路用 Request.RawBody 直发客户端原始报文。client 过去只在 Body != nil
// 时才设 Content-Type，导致 RawBody 请求裸奔，上游一律 415
// "Expected request with Content-Type: application/json"。
//
// 这个测试锁住三件事：
//   1. RawBody → 自动补 application/json
//   2. Body    → 行为不变（仍然自动补）
//   3. 调用方显式给出的 Content-Type 不被覆盖（multipart / passthrough 场景）

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/llmclient"
	"github.com/example/llm-gateway/internal/llmclientconfig"
)

// fakeUpstream 记录收到的 Content-Type，并返回 200 + 空 JSON。
func fakeUpstream(t *testing.T, got *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*got = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
}

func newTestClient(baseURL string) *llmclient.Client {
	cfg := llmclient.DefaultConfig("test", baseURL)
	cfg.Retry = llmclientconfig.RetryConfig{MaxRetries: 0}
	return llmclient.New(cfg, nil)
}

func TestRawBody_SetsContentTypeJSON(t *testing.T) {
	var got string
	srv := fakeUpstream(t, &got)
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.DoRaw(context.Background(), llmclient.Request{
		Method:   http.MethodPost,
		Endpoint: "/chat/completions",
		RawBody:  []byte(`{"model":"m","messages":[]}`),
	})
	if err != nil {
		t.Fatalf("DoRaw: %v", err)
	}
	if got != "application/json" {
		t.Fatalf("RawBody 请求的 Content-Type = %q, want application/json（这就是上游 415 的根因）", got)
	}
}

func TestBody_StillSetsContentTypeJSON(t *testing.T) {
	var got string
	srv := fakeUpstream(t, &got)
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.DoRaw(context.Background(), llmclient.Request{
		Method:   http.MethodPost,
		Endpoint: "/chat/completions",
		Body:     map[string]string{"model": "m"},
	})
	if err != nil {
		t.Fatalf("DoRaw: %v", err)
	}
	if got != "application/json" {
		t.Fatalf("Body 请求的 Content-Type = %q, want application/json", got)
	}
}

func TestRawBody_ExplicitContentTypeWins(t *testing.T) {
	var got string
	srv := fakeUpstream(t, &got)
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.DoRaw(context.Background(), llmclient.Request{
		Method:   http.MethodPost,
		Endpoint: "/files",
		RawBody:  []byte("--boundary--"),
		Headers: http.Header{
			"Content-Type": []string{"multipart/form-data; boundary=boundary"},
		},
	})
	if err != nil {
		t.Fatalf("DoRaw: %v", err)
	}
	if got != "multipart/form-data; boundary=boundary" {
		t.Fatalf("显式 Content-Type 被覆盖成 %q，调用方无法表达非 JSON 语义", got)
	}
}
