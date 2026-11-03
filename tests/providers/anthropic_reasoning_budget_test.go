package providers_test

// Anthropic 转换路径的思考预算保真测试。
//
// 事故背景：客户端显式给出 thinking.budget_tokens=1024 时，
//   1024 → relaykit 输出 ReasoningEffort="low" + ReasoningConversion.BudgetTokens=1024
//   1024 → bridge.go 只搬了 Effort，BudgetTokens 丢失
//   1024 → anthropic/applyReasoning 用 effort 反推档位 → 5000
//   64   → max_tokens 必须 > budget，被放大成 6024
//
// 结果：思考预算放大近 5 倍、max_tokens 放大 94 倍，直接改变计费口径。
// relaykit 的 dto.ReasoningConversionState 注释明确要求
// 「exact budgets must survive multi-step routes」。

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/stretchr/testify/require"
)

type capturedAnthropicRequest struct {
	MaxTokens int `json:"max_tokens"`
	Thinking  struct {
		Type         string `json:"type"`
		BudgetTokens int    `json:"budget_tokens"`
	} `json:"thinking"`
}

func intPtr(n int) *int { return &n }

func captureAnthropicUpstream(t *testing.T) (*httptest.Server, *capturedAnthropicRequest) {
	t.Helper()
	got := &capturedAnthropicRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant",
			"model":"claude-sonnet-4-5","content":[{"type":"text","text":"ok"}],
			"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":2}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

// 显式 budget 必须原样到达上游，不能被 effort 档位反推覆盖。
func TestAnthropicConversion_HonorsExplicitBudgetTokens(t *testing.T) {
	srv, got := captureAnthropicUpstream(t)
	p := newAnthropicForTest(t, srv.URL+"/v1")

	_, err := p.ChatCompletion(context.Background(), &core.ChatRequest{
		Model:     "claude-sonnet-4-5",
		MaxTokens: intPtr(64),
		Reasoning: &core.Reasoning{Effort: "low", BudgetTokens: intPtr(1024)},
		Messages:  []core.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)

	require.Equal(t, "enabled", got.Thinking.Type)
	require.Equal(t, 1024, got.Thinking.BudgetTokens,
		"客户端显式预算必须原样保留，不能用 effort 档位反推成 5000")
	require.Equal(t, 2048, got.MaxTokens,
		"max_tokens 只需略高于预算（budget+1024 余量），不应放大到 6024")
}

// 没给 budget 时仍按 effort 反推 —— 原有行为不能丢。
func TestAnthropicConversion_FallsBackToEffortWithoutBudget(t *testing.T) {
	srv, got := captureAnthropicUpstream(t)
	p := newAnthropicForTest(t, srv.URL+"/v1")

	_, err := p.ChatCompletion(context.Background(), &core.ChatRequest{
		Model:     "claude-sonnet-4-5",
		MaxTokens: intPtr(64),
		Reasoning: &core.Reasoning{Effort: "high"},
		Messages:  []core.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)

	require.Equal(t, 20000, got.Thinking.BudgetTokens, "high 档位对应 20000")
	require.Equal(t, 21024, got.MaxTokens, "max_tokens 抬到预算之上")
}

// 客户端 max_tokens 已经足够大时，不得缩减它。
func TestAnthropicConversion_DoesNotShrinkSufficientMaxTokens(t *testing.T) {
	srv, got := captureAnthropicUpstream(t)
	p := newAnthropicForTest(t, srv.URL+"/v1")

	_, err := p.ChatCompletion(context.Background(), &core.ChatRequest{
		Model:     "claude-sonnet-4-5",
		MaxTokens: intPtr(8000),
		Reasoning: &core.Reasoning{Effort: "low", BudgetTokens: intPtr(1024)},
		Messages:  []core.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)

	require.Equal(t, 1024, got.Thinking.BudgetTokens)
	require.Equal(t, 8000, got.MaxTokens, "客户端给的 max_tokens 已经够大，不该被改写")
}
