package service

import (
	"testing"

	"github.com/example/llm-gateway/internal/observability/report"
)

func TestEvaluateToothpaste(t *testing.T) {
	p := DefaultToothpasteParams()

	// 命中：turns=9 >= 8，短问占比 6/9=0.667 >= 0.5。
	hit := SessionFact{
		SessionID: "s-hit", SplitEvidence: SplitEvidenceStrong,
		PromptSeq:    []int64{10, 10, 10, 10, 10, 10, 5000, 5000, 5000},
		InputCostSeq: []float64{1.00, 0.05, 0.05, 0.05, 0.05, 0.05, 0.50, 0.50, 0.50},
		CacheCostSeq: []float64{0, 0.01, 0.01, 0.01, 0.01, 0.01, 0.10, 0.10, 0.10},
	}
	r := EvaluateToothpaste([]SessionFact{hit}, p)
	if r.Verdict != report.VerdictSuspected {
		t.Fatalf("hit: verdict = %q, want suspected", r.Verdict)
	}
	if r.SessionCount != 1 {
		t.Errorf("hit: session_count = %d, want 1", r.SessionCount)
	}
	if !approx(r.SessionShare, 1.0) {
		t.Errorf("hit: session_share = %v, want 1.0", r.SessionShare)
	}
	// 归因 = 除首轮外额外轮次 input+cache = (0.05*5+0.5*3)+(0.01*5+0.1*3) = 1.75+0.35 = 2.10
	if !approx(r.AttributedCostUSD, 2.10) {
		t.Errorf("hit: attributed = %v, want 2.10", r.AttributedCostUSD)
	}
	// 命中事件证据应指明 why suspected（为 CSA 预留）。
	if r.Evidence["reason_suspected"] == nil {
		t.Error("hit: evidence missing reason_suspected")
	}

	// 排除：turns==1 → 一轮结束的正常短问。
	single := SessionFact{SessionID: "s-one", PromptSeq: []int64{5}, InputCostSeq: []float64{0.01}, CacheCostSeq: []float64{0}}
	r2 := EvaluateToothpaste([]SessionFact{single}, p)
	if r2.Verdict != report.VerdictNotHit || r2.SessionCount != 0 {
		t.Fatalf("single-turn: verdict=%q session_count=%d, want not_hit/0", r2.Verdict, r2.SessionCount)
	}
	if len(r2.Excluded) == 0 {
		t.Error("single-turn: expected excluded entry")
	}

	// 排除：batch 簇跳过。
	batch := SessionFact{SessionID: "s-batch", SplitEvidence: SplitEvidenceBatch, PromptSeq: []int64{5, 5, 5, 5, 5, 5, 5, 5, 5}}
	r3 := EvaluateToothpaste([]SessionFact{batch}, p)
	if r3.Verdict != report.VerdictNotHit || r3.SessionCount != 0 {
		t.Fatalf("batch: verdict=%q session_count=%d, want not_hit/0", r3.Verdict, r3.SessionCount)
	}

	// 不命中：turns < 8。
	short := SessionFact{SessionID: "s-short", PromptSeq: []int64{5, 5, 5}}
	r4 := EvaluateToothpaste([]SessionFact{short}, p)
	if r4.Verdict != report.VerdictNotHit {
		t.Fatalf("turns<8: verdict = %q, want not_hit", r4.Verdict)
	}

	// 不命中：短问占比不足（有长问）。
	notEnough := SessionFact{SessionID: "s-le", PromptSeq: []int64{10, 20000, 20000, 20000, 20000, 20000, 20000, 20000}}
	r5 := EvaluateToothpaste([]SessionFact{notEnough}, p) // 1/8 = 0.125 < 0.5
	if r5.Verdict != report.VerdictNotHit {
		t.Fatalf("short-ratio insufficient: verdict = %q, want not_hit", r5.Verdict)
	}
}
