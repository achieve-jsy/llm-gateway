package service

// 原生协议健康探针测试。
//
// 背景：健康检查原先统一用 OpenAI /chat/completions 探测，anthropic / gemini
// 渠道必然 404，进而在「连续 N 次失败」后被自动禁用 —— 即使渠道完全正常。
// 这组用例锁住「探针走原生路径 + 用 adapter 真正使用的鉴权头」。

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeAnthropicMessages_UsesNativePathAndHeaders(t *testing.T) {
	var gotPath, gotKey, gotVersion string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"type":"message","content":[{"type":"text","text":"hi"}]}`))
	}))
	defer srv.Close()

	// 线上 endpoint 普遍带路径前缀（如百炼 /apps/anthropic/v1）。
	// 探针必须像 adapter 一样原样保留前缀，不能自己 TrimRight 后再拼 ——
	// 那样会比生产更宽容，把「转发必然 404」的渠道判成健康。
	ok, _, msg := ProbeAnthropicMessages(context.Background(), srv.URL+"/apps/anthropic/v1", "sk-test", "claude-x")
	if !ok {
		t.Fatalf("探针应成功，得到错误: %s", msg)
	}
	if gotPath != "/apps/anthropic/v1/messages" {
		t.Fatalf("必须走原生 /messages 并保留 endpoint 前缀，实际 %q", gotPath)
	}
	if gotKey != "sk-test" {
		t.Fatalf("必须用 x-api-key 鉴权，实际 %q", gotKey)
	}
	if gotVersion == "" {
		t.Fatal("Anthropic 请求必须带 anthropic-version")
	}
	// 请求体同样由 adapter 改写而来，不能只验证 URL 就宣布等价：
	// Anthropic 要求 max_tokens 必填，探针若漏了它会永远 400。
	if gotBody["max_tokens"] != float64(16) {
		t.Fatalf("max_tokens 必须为 16，实际 %v", gotBody["max_tokens"])
	}
	if gotBody["model"] != "claude-x" {
		t.Fatalf("model 必须原样透传，实际 %v", gotBody["model"])
	}
	if msgs, ok := gotBody["messages"].([]any); !ok || len(msgs) == 0 {
		t.Fatalf("messages 不能为空，实际 %v", gotBody["messages"])
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

	// 同时锁两件事：endpoint 前缀保留，以及模型名归一化。
	// 模型名前缀（models/ 、google/）在配置里很常见，手写拼接会产生
	// /models/models/xxx:generateContent 这种永久 404。
	ok, _, msg := ProbeGeminiGenerate(context.Background(), srv.URL+"/v1beta", "gk-test", "models/gemini-2.5-pro")
	if !ok {
		t.Fatalf("探针应成功，得到错误: %s", msg)
	}
	if gotPath != "/v1beta/models/gemini-2.5-pro:generateContent" {
		t.Fatalf("必须走原生 generateContent，且前缀与模型名都已归一化，实际 %q", gotPath)
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

func TestProbeVertexGenerate_UsesNativePathAndHeaders(t *testing.T) {
	var gotPath, gotKey string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-goog-api-key")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"hi"}]}}]}`))
	}))
	defer srv.Close()

	// 线上 vertex 渠道的 endpoint 是原生面（/publishers/google 结尾）。
	// adapter 用 VertexBaseURLs 把它识别成 native base，再拼 /models/{model}:generateContent。
	res := ProbeVertexGenerate(context.Background(), srv.URL+"/publishers/google", "sk-vertex", "gemini-1.5-flash")
	if !res.Success {
		t.Fatalf("探针应成功，得到错误: %s", res.Message)
	}
	if gotPath != "/publishers/google/models/gemini-1.5-flash:generateContent" {
		t.Fatalf("必须走原生 generateContent 并保留 endpoint 前缀，实际 %q", gotPath)
	}
	if gotKey != "sk-vertex" {
		t.Fatalf("必须用 x-goog-api-key 鉴权，实际 %q", gotKey)
	}
	// generateContent 用 contents 而非 messages；探针若还按 OpenAI 体发送，
	// 上游会 400，而这里断言的正是「请求体来自 adapter 而非手拼」。
	if _, ok := gotBody["contents"]; !ok {
		t.Fatalf("请求体必须是 generateContent 格式（contents），实际 %v", gotBody)
	}
}

func TestProbeAzureChat_UsesAdapterPathAndHeaders(t *testing.T) {
	var gotPath, gotKey string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("api-key")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hi"}}]}`))
	}))
	defer srv.Close()

	res := ProbeAzureChat(context.Background(), srv.URL, "sk-azure", "gpt-4o")
	if !res.Success {
		t.Fatalf("探针应成功，得到错误: %s", res.Message)
	}
	if gotPath != "/openai/v1/chat/completions" {
		t.Fatalf("必须走 azure 的 /openai/v1/chat/completions，实际 %q", gotPath)
	}
	if gotKey != "sk-azure" {
		t.Fatalf("azure 必须用 api-key 头鉴权，实际 %q", gotKey)
	}
	if gotBody["model"] != "gpt-4o" {
		t.Fatalf("model 必须原样透传，实际 %v", gotBody["model"])
	}
}
