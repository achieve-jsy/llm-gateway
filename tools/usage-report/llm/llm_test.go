package llm

// llm_test.go —— 用 httptest 模拟 chat/completions，验证 Complete 的解析与错误处理。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompleteSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %s, want /chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization = %q, want Bearer test-key", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q, want application/json", got)
		}

		// 校验请求体：model/messages/max_tokens
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "test-model" {
			t.Fatalf("request model = %q, want test-model", req.Model)
		}
		if req.MaxTokens != maxTokens {
			t.Fatalf("request max_tokens = %d, want %d", req.MaxTokens, maxTokens)
		}
		if len(req.Messages) != 2 || req.Messages[0].Role != "system" || req.Messages[1].Role != "user" {
			t.Fatalf("unexpected messages: %+v", req.Messages)
		}
		if req.Messages[0].Content != "sys" || req.Messages[1].Content != "user" {
			t.Fatalf("unexpected message contents: %+v", req.Messages)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hello from llm"}}]}`))
	}))
	defer server.Close()

	c := New(server.URL, "test-key", "test-model")
	out, err := c.Complete(context.Background(), "sys", "user")
	if err != nil {
		t.Fatalf("Complete error: %v", err)
	}
	if out != "hello from llm" {
		t.Fatalf("out = %q, want hello from llm", out)
	}
}

func TestCompleteDefaultModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != defaultModel {
			t.Fatalf("request model = %q, want default %q", req.Model, defaultModel)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()

	c := New(server.URL, "test-key", "") // 空 model → 用默认
	if _, err := c.Complete(context.Background(), "sys", "user"); err != nil {
		t.Fatalf("Complete error: %v", err)
	}
}

func TestCompleteHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer server.Close()

	c := New(server.URL, "test-key", "test-model")
	_, err := c.Complete(context.Background(), "sys", "user")
	if err == nil {
		t.Fatal("expected error for non-2xx status")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error should contain status 500, got: %v", err)
	}
}

func TestCompleteNoChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer server.Close()

	c := New(server.URL, "test-key", "test-model")
	_, err := c.Complete(context.Background(), "sys", "user")
	if err == nil {
		t.Fatal("expected error for no choices")
	}
	if !strings.Contains(err.Error(), "no choices") {
		t.Fatalf("error should mention no choices, got: %v", err)
	}
}

func TestCompleteInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{not-json`))
	}))
	defer server.Close()

	c := New(server.URL, "test-key", "test-model")
	_, err := c.Complete(context.Background(), "sys", "user")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestCompleteEmptyBaseAndKey(t *testing.T) {
	// base 为空的错误
	if _, err := New("", "key", "m").Complete(context.Background(), "s", "u"); err == nil {
		t.Fatal("expected error for empty base URL")
	}
	// key 为空的错误
	if _, err := New("http://example.com/v1", "", "m").Complete(context.Background(), "s", "u"); err == nil {
		t.Fatal("expected error for empty api key")
	}
	// nil client 的错误
	var nilClient *Client
	if _, err := nilClient.Complete(context.Background(), "s", "u"); err == nil {
		t.Fatal("expected error for nil client")
	}
}
