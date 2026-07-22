package service

import (
	"math"
	"testing"

	"github.com/example/llm-gateway/internal/observability/report/v2"
)

func TestEvaluatePrefixPollution(t *testing.T) {
	p := DefaultPrefixPollutionParams()

	// case1: 连续 3 轮超长低命中 → 命中；strong → confirmed。
	strong := SessionFact{
		SessionID: "s-strong", SplitEvidence: SplitEvidenceStrong,
		PromptSeq:    []int64{600000, 620000, 610000, 7000},
		CachedSeq:    []int64{100000, 90000, 80000, 5000},
		InputCostSeq: []float64{1.0, 1.1, 1.0, 0.01},
		CacheCostSeq: []float64{0.1, 0.09, 0.08, 0.001},
	}
	r1 := EvaluatePrefixPollution([]SessionFact{strong}, p)
	if r1.Verdict != report.VerdictConfirmed {
		t.Fatalf("strong polluted: verdict = %q, want %q", r1.Verdict, report.VerdictConfirmed)
	}
	if r1.RequestCount != 3 {
		t.Errorf("strong polluted: request_count = %d, want 3", r1.RequestCount)
	}
	if !approx(r1.AttributedCostUSD, 3.37) {
		t.Errorf("strong polluted: attributed = %v, want 3.37", r1.AttributedCostUSD)
	}
	if !approx(r1.RequestShare, 0.75) {
		t.Errorf("strong polluted: request_share = %v, want 0.75", r1.RequestShare)
	}
	if !approx(r1.SessionShare, 1.0) {
		t.Errorf("strong polluted: session_share = %v, want 1.0", r1.SessionShare)
	}

	// case2: 同样污染但弱证据 → suspected（weak 封顶）。
	weak := SessionFact{
		SessionID: "s-weak", SplitEvidence: SplitEvidenceWeak,
		PromptSeq:    []int64{600000, 620000, 610000},
		CachedSeq:    []int64{100000, 90000, 80000},
		InputCostSeq: []float64{1.0, 1.1, 1.0},
		CacheCostSeq: []float64{0.1, 0.09, 0.08},
	}
	r2 := EvaluatePrefixPollution([]SessionFact{weak}, p)
	if r2.Verdict != report.VerdictSuspected {
		t.Fatalf("weak polluted: verdict = %q, want %q", r2.Verdict, report.VerdictSuspected)
	}
	if r2.RequestCount != 3 {
		t.Errorf("weak polluted: request_count = %d, want 3", r2.RequestCount)
	}

	// case3: 连续超长但命中稳定（>=70%）→ 排除，不判污染，not_hit。
	stable := SessionFact{
		SessionID: "s-stable", SplitEvidence: SplitEvidenceStrong,
		PromptSeq:    []int64{600000, 610000, 620000},
		CachedSeq:    []int64{450000, 460000, 470000},
		InputCostSeq: []float64{1.0, 1.1, 1.2},
		CacheCostSeq: []float64{0.5, 0.51, 0.52},
	}
	r3 := EvaluatePrefixPollution([]SessionFact{stable}, p)
	if r3.Verdict != report.VerdictNotHit {
		t.Fatalf("stable long: verdict = %q, want not_hit", r3.Verdict)
	}
	if r3.RequestCount != 0 {
		t.Errorf("stable long: request_count = %d, want 0", r3.RequestCount)
	}
	if len(r3.Excluded) == 0 {
		t.Error("stable long: expected excluded entry for stable-hit-long segment")
	}

	// case4: 只有 2 轮连续污染（< 3）→ 不构成段，not_hit。
	two := SessionFact{
		SessionID: "s-two", SplitEvidence: SplitEvidenceStrong,
		PromptSeq:    []int64{600000, 610000, 7000, 8000},
		CachedSeq:    []int64{100000, 90000, 5000, 6000},
		InputCostSeq: []float64{1.0, 1.0, 0.01, 0.01},
		CacheCostSeq: []float64{0.1, 0.09, 0.001, 0.001},
	}
	r4 := EvaluatePrefixPollution([]SessionFact{two}, p)
	if r4.Verdict != report.VerdictNotHit || r4.RequestCount != 0 {
		t.Fatalf("two polluted turns: verdict=%q count=%d, want not_hit/0", r4.Verdict, r4.RequestCount)
	}

	// case5: batch 簇 → 跳过不判。
	batch := SessionFact{SessionID: "s-batch", SplitEvidence: SplitEvidenceBatch, PromptSeq: []int64{600000, 620000, 610000}}
	r5 := EvaluatePrefixPollution([]SessionFact{batch}, p)
	if r5.Verdict != report.VerdictNotHit || r5.SessionCount != 0 {
		t.Fatalf("batch: verdict=%q session_count=%d, want not_hit/0", r5.Verdict, r5.SessionCount)
	}

	// case6: 空输入 → not_hit，不 panic。
	r6 := EvaluatePrefixPollution(nil, p)
	if r6.Verdict != report.VerdictNotHit {
		t.Fatalf("nil: verdict = %q, want not_hit", r6.Verdict)
	}
}

func approx(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}
