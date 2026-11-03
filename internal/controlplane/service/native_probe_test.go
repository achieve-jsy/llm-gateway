package service

// 原生协议健康探针测试。
//
// 背景：健康检查原先统一用 OpenAI /chat/completions 探测，anthropic / gemini
// 渠道必然 404，进而在「连续 N 次失败」后被自动禁用 —— 即使渠道完全正常。
// 这组用例锁住「探针走原生路径 + 用 adapter 真正使用的鉴权头」。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeAnthropicMessages_UsesNativePathAndHeaders(t *testing.T) {
	var gotPath, gotKey, gotVersion string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"type":"message","content":[{"type":"text","text":"hi"}]}`))
	}))
	defer srv.Close()

	ok, _, msg := ProbeAnthropicMessages(context.Background(), srv.URL, "sk-test", "claude-x")
	if !ok {
		t.Fatalf("探针应成功，得到错误: %s", msg)
	}
	if gotPath != "/messages" {
		t.Fatalf("必须走原生 /messages，实际 %q", gotPath)
	}
	if gotKey != "sk-test" {
		t.Fatalf("必须用 x-api-key 鉴权，实际 %q", gotKey)
	}
	if gotVersion == "" {
		t.Fatal("Anthropic 请求必须带 anthropic-version")
	}
}

func TestProbeGeminiGenerate_UsesNativePathAndHeaders(t *testing.T) {
	var gotPath, gotKey string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-goog-api-key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"hi"}]}}]}`))
	}))
	defer srv.Close()

	ok, _, msg := ProbeGeminiGenerate(context.Background(), srv.URL, "gk-test", "gemini-2.5-pro")
	if !ok {
		t.Fatalf("探针应成功，得到错误: %s", msg)
	}
	if gotPath != "/models/gemini-2.5-pro:generateContent" {
		t.Fatalf("必须走原生 generateContent，实际 %q", gotPath)
	}
	if gotKey != "gk-test" {
		t.Fatalf("必须用 x-goog-api-key 鉴权，实际 %q", gotKey)
	}
}

// 真实故障仍必须被判定为失败，否则健康检查就失去意义。
func TestProbeNativeProtocols_RealFailureIsNotSwallowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid key"}`))
	}))
	defer srv.Close()

	if ok, _, msg := ProbeAnthropicMessages(context.Background(), srv.URL, "bad", "m"); ok {
		t.Fatalf("401 必须判失败，实际成功（msg=%s）", msg)
	}
	if ok, _, msg := ProbeGeminiGenerate(context.Background(), srv.URL, "bad", "m"); ok {
		t.Fatalf("401 必须判失败，实际成功（msg=%s）", msg)
	}
}

// 探针分发开关：anthropic / gemini 不能再被当成 OpenAI 兼容渠道走通用探针。
func TestIsNonOpenAICompatible_NativeProtocols(t *testing.T) {
	for _, id := range []string{"anthropic", "gemini", "bedrock", "azure", "vertex"} {
		if !isNonOpenAICompatible(id) {
			t.Fatalf("%q 必须走专属探针，不能被当成 OpenAI 兼容渠道", id)
		}
	}
	for _, id := range []string{"openai-compatible", "dashscope", "deepseek", ""} {
		if isNonOpenAICompatible(id) {
			t.Fatalf("%q 是 OpenAI 兼容渠道，不应走专属探针", id)
		}
	}
}
