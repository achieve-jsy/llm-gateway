package orchestrator

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/core"
	"github.com/gin-gonic/gin"
)

// TestStreamUsageObserver_AnthropicFormat 钉死原生旁路的计费输入。
//
// Bedrock InvokeModel 返回的是 Anthropic SSE，usage 分散在两处：
//   - message_start: {"message":{"usage":{"input_tokens":25,"output_tokens":1}}}
//   - message_delta: {"usage":{"output_tokens":15}}
//
// 旧的 extractUsageMap 只认 payload["usage"] / payload["response"]["usage"]，
// 会漏掉 input_tokens；而且 message_delta 会用 prompt=0 覆盖掉前一次的 25。
// 这个用例保证两处都被合并，不被覆盖。
func TestStreamUsageObserver_AnthropicFormat(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	obs := newStreamUsageObserver(c, timeNow())

	events := []map[string]any{
		{
			"type": "message_start",
			"message": map[string]any{
				"id":    "msg_1",
				"model": "claude-sonnet-4-5",
				"usage": map[string]any{"input_tokens": float64(25), "output_tokens": float64(1)},
			},
		},
		{
			"type":  "content_block_delta",
			"index": float64(0),
			"delta": map[string]any{"type": "text_delta", "text": "Hello"},
		},
		{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "end_turn"},
			"usage": map[string]any{"output_tokens": float64(15)},
		},
		{"type": "message_stop"},
	}
	for _, e := range events {
		obs.OnJSONEvent(e)
	}
	obs.OnStreamClose()

	raw, ok := c.Get("usage")
	if !ok {
		t.Fatal("usage 未写入 gin context：原生流式路径计费会漏")
	}
	u, ok := raw.(core.Usage)
	if !ok {
		t.Fatalf("usage 类型错误: %T", raw)
	}
	if u.PromptTokens != 25 {
		t.Errorf("PromptTokens = %d, want 25（input_tokens 在 message.usage 里，不能被 message_delta 覆盖）", u.PromptTokens)
	}
	if u.CompletionTokens != 15 {
		t.Errorf("CompletionTokens = %d, want 15", u.CompletionTokens)
	}
}

// TestStreamUsageObserver_AnthropicFormatNoRegression 确认 OpenAI 格式仍然工作。
func TestStreamUsageObserver_AnthropicFormatNoRegression(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	obs := newStreamUsageObserver(c, timeNow())
	obs.OnJSONEvent(map[string]any{
		"usage": map[string]any{
			"prompt_tokens":     float64(7),
			"completion_tokens": float64(3),
			"total_tokens":      float64(10),
		},
	})
	obs.OnStreamClose()

	raw, _ := c.Get("usage")
	u := raw.(core.Usage)
	if u.PromptTokens != 7 || u.CompletionTokens != 3 || u.TotalTokens != 10 {
		t.Errorf("OpenAI 格式回归失败: %+v", u)
	}
}

func timeNow() time.Time { return time.Now() }
