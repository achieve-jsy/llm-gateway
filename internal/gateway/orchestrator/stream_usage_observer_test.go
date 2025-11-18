package orchestrator

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/gin-gonic/gin"
)

// TestStreamUsageObserver_WritesUsageToContext 验证旁路 observer 能从末尾 usage chunk
// 提取 token 用量并连同首 token 延迟写回 Gin context。
func TestStreamUsageObserver_WritesUsageToContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	obs := newStreamUsageObserver(c, time.Now().Add(-100*time.Millisecond))

	// 内容增量 chunk → 触发首 token 计时
	obs.OnJSONEvent(map[string]any{
		"model":   "glm-5.1",
		"choices": []any{map[string]any{"delta": map[string]any{"content": "你好"}}},
	})
	// 末尾 usage chunk（choices 为空，仅携带 usage）
	obs.OnJSONEvent(map[string]any{
		"choices": []any{},
		"usage": map[string]any{
			"prompt_tokens":     float64(9),
			"completion_tokens": float64(1087),
			"total_tokens":      float64(1096),
			"prompt_tokens_details": map[string]any{
				"cached_tokens": float64(0),
			},
		},
	})
	obs.OnStreamClose()

	v, ok := c.Get("usage")
	if !ok {
		t.Fatal("expected usage to be set in context")
	}
	u, ok := v.(core.Usage)
	if !ok {
		t.Fatalf("expected core.Usage, got %T", v)
	}
	if u.PromptTokens != 9 || u.CompletionTokens != 1087 || u.TotalTokens != 1096 {
		t.Fatalf("unexpected usage: %+v", u)
	}

	ft, ok := c.Get("first_token_ms")
	if !ok {
		t.Fatal("expected first_token_ms to be set")
	}
	if ms, ok := ft.(int); !ok || ms <= 0 {
		t.Fatalf("expected positive first_token_ms, got %v", ft)
	}
}

// TestStreamUsageObserver_NoUsageChunk 验证没有 usage chunk 时不会写入 usage（避免误记 0）。
func TestStreamUsageObserver_NoUsageChunk(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	obs := newStreamUsageObserver(c, time.Now())
	obs.OnJSONEvent(map[string]any{
		"choices": []any{map[string]any{"delta": map[string]any{"content": "hi"}}},
	})
	obs.OnStreamClose()

	if _, ok := c.Get("usage"); ok {
		t.Fatal("usage should not be set when no usage chunk present")
	}
}

// TestStreamUsageObserver_NilSafe 验证 nil receiver / nil context 安全。
func TestStreamUsageObserver_NilSafe(t *testing.T) {
	var obs *streamUsageObserver
	obs.OnJSONEvent(map[string]any{"usage": map[string]any{"total_tokens": float64(1)}})
	obs.OnStreamClose()
}
