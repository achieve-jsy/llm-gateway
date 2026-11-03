package orchestrator

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/gin-gonic/gin"
)

// TestChatNativeGeminiWritesUsage 钉死 Gemini 原生旁路的计费输入。
//
// Gemini 不走 usage/prompt_tokens 命名，而是顶层 usageMetadata + camelCase
// （promptTokenCount / candidatesTokenCount / totalTokenCount）。解析器不认识
// 它就是 UsageMissing，这条路径独立计费为 0——与 Anthropic 那次是同类漏洞。
func TestChatNativeGeminiWritesUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &nativeTestProvider{
		supports:   true,
		format:     core.NativeRelayGemini,
		nativeBody: `{"candidates":[{"content":{"parts":[{"text":"hi"}]}}],"usageMetadata":{"promptTokenCount":11,"candidatesTokenCount":4,"totalTokenCount":15}}`,
	}
	orch := newNativeTestOrchestrator(t, provider)

	gc, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx := WithGinContext(context.Background(), gc)

	if _, err := orch.ChatNative(ctx, &core.ChatRequest{Model: "claude-model"}, []byte(`{"model":"claude-model"}`), core.NativeRelayGemini); err != nil {
		t.Fatalf("ChatNative: %v", err)
	}
	u, ok := gc.MustGet("usage").(core.Usage)
	if !ok {
		t.Fatal("usage 未回填：Gemini 原生路径计费会漏")
	}
	if u.PromptTokens != 11 || u.CompletionTokens != 4 || u.TotalTokens != 15 {
		t.Errorf("usage = %+v, want 11/4/15", u)
	}
}

// TestStreamUsageObserverGemini 确认流式 Gemini SSE 里的 usageMetadata 被采集。
// Gemini 每个 chunk 都带累计 usageMetadata，因此"后到的非零值覆盖"就是正确语义。
func TestStreamUsageObserverGemini(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	obs := newStreamUsageObserver(c, timeNow())

	obs.OnJSONEvent(map[string]any{
		"candidates": []any{map[string]any{
			"content": map[string]any{"parts": []any{map[string]any{"text": "hi"}}},
		}},
		"usageMetadata": map[string]any{
			"promptTokenCount":     float64(11),
			"candidatesTokenCount": float64(2),
			"totalTokenCount":      float64(13),
		},
	})
	obs.OnJSONEvent(map[string]any{
		"candidates": []any{map[string]any{
			"content": map[string]any{"parts": []any{map[string]any{"text": " there"}}},
		}},
		"usageMetadata": map[string]any{
			"promptTokenCount":     float64(11),
			"candidatesTokenCount": float64(4),
			"totalTokenCount":      float64(15),
		},
	})
	obs.OnStreamClose()

	raw, ok := c.Get("usage")
	if !ok {
		t.Fatal("usage 未写入 gin context：Gemini 流式计费会漏")
	}
	u, ok := raw.(core.Usage)
	if !ok {
		t.Fatalf("usage 类型错误: %T", raw)
	}
	if u.PromptTokens != 11 {
		t.Errorf("PromptTokens = %d, want 11", u.PromptTokens)
	}
	if u.CompletionTokens != 4 {
		t.Errorf("CompletionTokens = %d, want 4（应取最后一个 chunk 的累计值）", u.CompletionTokens)
	}
	if u.TotalTokens != 15 {
		t.Errorf("TotalTokens = %d, want 15", u.TotalTokens)
	}
}
