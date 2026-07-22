package service

import (
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/observability/report/v2"
)

func TestEvaluateOutputRunaway(t *testing.T) {
	base := OutputRunawayBaselines{
		ModelPromptP25:    map[string]float64{"gpt-4o": 1000, "gpt-4o-pro": 800},
		ModelOutputP75:    map[string]float64{"gpt-4o": 2000, "gpt-4o-pro": 3000},
		PositionPromptP25: map[string]float64{"engineer": 900},
	}
	params := DefaultOutputRunawayParams()
	params.HighTierModels = map[string]bool{"gpt-4o-pro": true}

	// 混合场景（v0.7 护栏）：
	//   row1: u1 首条 finish_reason=="length"（撞上限截断）→ 命中
	//   row2: u1 第二条 finish_reason=="length" → 同 user 重复（>=RepeatMin=2）→ confidence 提升
	//   row3: u2 空 finish_reason 且 completion=4000（<8000）→ 未达失控量级 → 免检
	//   row4: u3 空 finish_reason 且 completion=100（<8000）→ 未命中
	rows := []OutputRunawayRow{
		{RequestID: "r1", UserID: "u1", SessionID: "s1", Position: "engineer", Model: "gpt-4o", PromptTokens: 500, CachedTokens: 0, CompletionTokens: 5000, OutputCostUSD: 0.50, FinishReason: "length"},
		{RequestID: "r2", UserID: "u1", SessionID: "s1", Position: "engineer", Model: "gpt-4o", PromptTokens: 100, CachedTokens: 50, CompletionTokens: 3000, OutputCostUSD: 0.30, FinishReason: "length"},
		{RequestID: "r3", UserID: "u2", SessionID: "s2", Position: "data", Model: "gpt-4o", PromptTokens: 500, CachedTokens: 0, CompletionTokens: 4000, OutputCostUSD: 0.40},
		{RequestID: "r4", UserID: "u3", SessionID: "s3", Position: "engineer", Model: "gpt-4o", PromptTokens: 5000, CachedTokens: 0, CompletionTokens: 100, OutputCostUSD: 0.01},
	}

	issue := EvaluateOutputRunaway(rows, base, params)
	if issue.Verdict != report.VerdictConfirmed {
		t.Fatalf("verdict = %q, want confirmed", issue.Verdict)
	}
	if issue.RequestCount != 2 {
		t.Errorf("request_count = %d, want 2 (r3/r4 exempt)", issue.RequestCount)
	}
	if issue.SessionCount != 1 {
		t.Errorf("session_count = %d, want 1 (only s1)", issue.SessionCount)
	}
	// 归因 = r1: 0.50*(5000-2000)/5000=0.30; r2: 0.30*(3000-2000)/3000=0.10 → 0.40
	if !approx(issue.AttributedCostUSD, 0.40) {
		t.Errorf("attributed_cost_usd = %v, want 0.40", issue.AttributedCostUSD)
	}
	if !approx(issue.RequestShare, 0.50) {
		t.Errorf("request_share = %v, want 0.50", issue.RequestShare)
	}
	// confidence: base 0.6 + repeated(u1,2>=2)=0.15 → 0.75；无高阶命中。
	if !approx(issue.Confidence, 0.75) {
		t.Errorf("confidence = %v, want 0.75", issue.Confidence)
	}

	// 高阶模型命中 → 提升 confidence（叠加叠高阶模型）。
	rowsHigh := []OutputRunawayRow{
		{RequestID: "h1", UserID: "u9", SessionID: "s9", Position: "engineer", Model: "gpt-4o-pro", PromptTokens: 100, CachedTokens: 0, CompletionTokens: 5000, OutputCostUSD: 1.0, FinishReason: "length"},
	}
	issueHigh := EvaluateOutputRunaway(rowsHigh, base, params)
	if issueHigh.Verdict != report.VerdictConfirmed {
		t.Fatalf("high-tier verdict = %q, want confirmed", issueHigh.Verdict)
	}
	// base 0.6 + high-tier 0.15（单次，无 repeated）= 0.75
	if !approx(issueHigh.Confidence, 0.75) {
		t.Errorf("high-tier confidence = %v, want 0.75", issueHigh.Confidence)
	}

	// 无命中 → not_hit。
	rowsNone := []OutputRunawayRow{
		{RequestID: "n1", UserID: "u5", SessionID: "s5", Position: "engineer", Model: "gpt-4o", PromptTokens: 5000, CachedTokens: 0, CompletionTokens: 100, OutputCostUSD: 0.01},
	}
	issueNone := EvaluateOutputRunaway(rowsNone, base, params)
	if issueNone.Verdict != report.VerdictNotHit || issueNone.RequestCount != 0 {
		t.Fatalf("no-hit verdict=%q count=%d, want not_hit/0", issueNone.Verdict, issueNone.RequestCount)
	}
	if issueNone.Confidence != 0 {
		t.Errorf("no-hit confidence = %v, want 0", issueNone.Confidence)
	}
}

func TestRunawayMagnitude_NoBaseline(t *testing.T) {
	// 分位基线缺失 + finish_reason 缺失时，仅当达到绝对失控量级（completion>=MinRunawayTokens 且费用>=MinRunawayCostUSD）才命中。
	base := OutputRunawayBaselines{} // 无任何基线
	params := DefaultOutputRunawayParams()
	rows := []OutputRunawayRow{
		{RequestID: "m1", UserID: "u1", SessionID: "s1", Model: "unknown-model", PromptTokens: 10, CachedTokens: 1, CompletionTokens: 1000, OutputCostUSD: 0.10},
	}
	// 未达量级（1000<8000）→ 免检。
	issue := EvaluateOutputRunaway(rows, base, params)
	if issue.Verdict != report.VerdictNotHit || issue.RequestCount != 0 {
		t.Fatalf("below-magnitude verdict=%q count=%d, want not_hit/0", issue.Verdict, issue.RequestCount)
	}

	// 达到量级（9000>=8000 且 0.20>=0.10）→ 命中。
	rowsBig := []OutputRunawayRow{
		{RequestID: "m2", UserID: "u1", SessionID: "s2", Model: "unknown-model", PromptTokens: 10, CachedTokens: 1, CompletionTokens: 9000, OutputCostUSD: 0.20},
	}
	issueBig := EvaluateOutputRunaway(rowsBig, base, params)
	if issueBig.Verdict != report.VerdictConfirmed || issueBig.RequestCount != 1 {
		t.Fatalf("magnitude verdict=%q count=%d, want confirmed/1", issueBig.Verdict, issueBig.RequestCount)
	}
	// 无 P75 可用 → 归因 0。
	if !approx(issueBig.AttributedCostUSD, 0) {
		t.Errorf("magnitude attributed = %v, want 0", issueBig.AttributedCostUSD)
	}
}

// TestRunawayFinishReason_StopExemption verifies v0.7 physical causality defense:
// finish_reason=="stop" without extreme loops is exempt from runaway output,
// eliminating false positives for normal long code output (e.g. "杨士俊").
func TestRunawayFinishReason_StopExemption(t *testing.T) {
	base := OutputRunawayBaselines{}
	params := DefaultOutputRunawayParams()

	// Yang Shijun scenario: short prompt (10 tokens), large completion (4000 tokens), finish_reason = "stop"
	rows := []OutputRunawayRow{
		{
			RequestID:        "yang_1",
			UserID:           "yangshijun",
			SessionID:        "s_yang",
			Position:         "engineer",
			Model:            "gpt-4o",
			PromptTokens:     10,
			CachedTokens:     0,
			CompletionTokens: 4000,
			OutputCostUSD:    0.04,
			FinishReason:     "stop",
			OutputText:       "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Normal long code\")\n}",
		},
	}

	issue := EvaluateOutputRunaway(rows, base, params)
	if issue.Verdict != report.VerdictNotHit || issue.RequestCount != 0 {
		t.Fatalf("expected finish_reason='stop' to be exempt (not_hit), got verdict=%q count=%d", issue.Verdict, issue.RequestCount)
	}
}

// TestRunawayFinishReason_ExtremeLoop verifies that even with finish_reason=="stop",
// an output with extreme looping/repeating patterns is still caught.
func TestRunawayFinishReason_ExtremeLoop(t *testing.T) {
	base := OutputRunawayBaselines{}
	params := DefaultOutputRunawayParams()

	repeatedLines := strings.Repeat("Error: connection refused retry\n", 20)
	rows := []OutputRunawayRow{
		{
			RequestID:        "loop_1",
			UserID:           "user_loop",
			SessionID:        "s_loop",
			Position:         "engineer",
			Model:            "gpt-4o",
			PromptTokens:     10,
			CachedTokens:     0,
			CompletionTokens: 2000,
			OutputCostUSD:    0.02,
			FinishReason:     "stop",
			OutputText:       repeatedLines,
		},
	}

	issue := EvaluateOutputRunaway(rows, base, params)
	if issue.Verdict != report.VerdictConfirmed || issue.RequestCount != 1 {
		t.Fatalf("expected extreme loop to be confirmed runaway, got verdict=%q count=%d", issue.Verdict, issue.RequestCount)
	}
}

// TestComputeHealthGrade verifies personal health checkup grade calculation and highlights.
func TestComputeHealthGrade(t *testing.T) {
	// 1. Yang Shijun profile: zero runaway, zero multitask waste, high cache hit rate -> Grade A & 高复用标杆
	yangProfile := report.UserHealthProfile{
		UserID:              "yangshijun",
		Position:            "engineer",
		TotalCostUSD:        2.50,
		TotalCalls:          25,
		TotalTokens:         30000,
		RunawayWastedCost:   0,
		MultitaskWastedCost: 0,
		CacheHitRate:        0.65,
		DeepDevSessions:     3,
		RollbackCount:       0,
	}

	grade, highlights := ComputeHealthGrade(yangProfile)
	if grade != string(report.GradeA) {
		t.Fatalf("expected Grade A for Yang Shijun, got %s", grade)
	}
	hasReuseCard := false
	for _, h := range highlights {
		if h == "高复用标杆" {
			hasReuseCard = true
			break
		}
	}
	if !hasReuseCard {
		t.Fatalf("expected '高复用标杆' in highlights, got %+v", highlights)
	}

	// 2. Risk profile: severe runaway output -> Grade C
	badProfile := report.UserHealthProfile{
		UserID:            "user_bad",
		Position:          "analyst",
		TotalCostUSD:      5.00,
		TotalCalls:        10,
		RunawayWastedCost: 2.50, // 50% waste
	}
	gradeBad, _ := ComputeHealthGrade(badProfile)
	if gradeBad != string(report.GradeC) {
		t.Fatalf("expected Grade C for severe runaway user, got %s", gradeBad)
	}
}
