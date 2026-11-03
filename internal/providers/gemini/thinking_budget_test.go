package gemini

// 思考预算保真：与 anthropic 路径同源的问题。
//
// 客户端显式给出 budget_tokens=3000 时，若只按 effort 档位反推，
// medium 会给出 8192 —— 预算被放大 2.7 倍。Gemini 2.5 的
// thinkingBudget 是数值，本该直接承接。

import (
	"testing"

	"github.com/example/llm-gateway/internal/core"
)

func intPtr(n int) *int { return &n }

func TestGeminiGenerationConfig_HonorsExplicitBudget(t *testing.T) {
	cfg := geminiGenerationConfig(&core.ChatRequest{
		Model:     "gemini-2.5-pro",
		Reasoning: &core.Reasoning{Effort: "medium", BudgetTokens: intPtr(3000)},
	})

	tc, ok := cfg["thinkingConfig"].(map[string]any)
	if !ok {
		t.Fatalf("thinkingConfig 缺失: %#v", cfg)
	}
	if got := tc["thinkingBudget"]; got != 3000 {
		t.Fatalf("thinkingBudget = %v, 期望 3000（客户端显式预算）", got)
	}
}

// 没给预算时仍按档位映射 —— 原有行为不能丢。
func TestGeminiGenerationConfig_FallsBackToEffort(t *testing.T) {
	cfg := geminiGenerationConfig(&core.ChatRequest{
		Model:     "gemini-2.5-pro",
		Reasoning: &core.Reasoning{Effort: "medium"},
	})

	tc, ok := cfg["thinkingConfig"].(map[string]any)
	if !ok {
		t.Fatalf("thinkingConfig 缺失: %#v", cfg)
	}
	if got := tc["thinkingBudget"]; got != 8192 {
		t.Fatalf("thinkingBudget = %v, 期望 8192（medium 档位）", got)
	}
}

// 非 2.5 模型用 thinkingLevel 字符串，没有数值预算可承接，退回档位映射。
func TestGeminiGenerationConfig_NonBudgetModelIgnoresBudget(t *testing.T) {
	cfg := geminiGenerationConfig(&core.ChatRequest{
		Model:     "gemini-3.0-pro",
		Reasoning: &core.Reasoning{Effort: "high", BudgetTokens: intPtr(3000)},
	})

	tc, ok := cfg["thinkingConfig"].(map[string]any)
	if !ok {
		t.Fatalf("thinkingConfig 缺失: %#v", cfg)
	}
	if _, has := tc["thinkingBudget"]; has {
		t.Fatalf("非 2.5 模型不该出现 thinkingBudget: %#v", tc)
	}
	if got := tc["thinkingLevel"]; got != "high" {
		t.Fatalf("thinkingLevel = %v, 期望 high", got)
	}
}

// budget_tokens: 0 是合法值（关闭思考），不能被当成「没给」。
func TestGeminiGenerationConfig_ZeroBudgetIsHonored(t *testing.T) {
	cfg := geminiGenerationConfig(&core.ChatRequest{
		Model:     "gemini-2.5-flash",
		Reasoning: &core.Reasoning{Effort: "high", BudgetTokens: intPtr(0)},
	})

	tc, ok := cfg["thinkingConfig"].(map[string]any)
	if !ok {
		t.Fatalf("thinkingConfig 缺失: %#v", cfg)
	}
	if got := tc["thinkingBudget"]; got != 0 {
		t.Fatalf("thinkingBudget = %v, 期望 0（显式关闭）", got)
	}
}
