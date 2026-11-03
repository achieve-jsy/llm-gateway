package service

import (
	"testing"

	"github.com/example/llm-gateway/internal/observability/report"
)

func TestEvaluateMultiTaskSession(t *testing.T) {
	p := DefaultMultiTaskParams()
	base := MultiTaskBaselines{UserPromptP90: map[string]float64{"u1": 300_000}}

	// 命中：超长(末轮 410000 >= 300k) + CacheHitRate(≈0.494 >= 0.4) + !HasReset + TaskSwitches=1。
	hit := SessionFact{
		SessionID: "s-hit", UserID: "u1", SplitEvidence: SplitEvidenceWeak,
		PromptSeq:    []int64{100, 200, 300, 400000, 410000},
		CachedSeq:    []int64{50, 100, 150, 200000, 200000},
		TaskKinds:    []string{"a", "a", "a", "b", "b"}, // index3 切换 → switches=1
		InputCostSeq: []float64{0.01, 0.01, 0.01, 1.0, 1.1},
		CacheCostSeq: []float64{0, 0, 0, 0.2, 0.2},
	}
	r := EvaluateMultiTaskSession([]SessionFact{hit}, p, base)
	if r.Verdict != report.VerdictSuspected {
		t.Fatalf("hit: verdict = %q, want suspected", r.Verdict)
	}
	if r.SessionCount != 1 {
		t.Errorf("hit: session_count = %d, want 1", r.SessionCount)
	}
	// 首次突增点 = index3（prompt4>prompt3 增 399700 >= 100k），归因 idx3..4 = (1.0+0.2)+(1.1+0.2)=2.5
	if !approx(r.AttributedCostUSD, 2.5) {
		t.Errorf("hit: attributed = %v, want 2.5", r.AttributedCostUSD)
	}
	if r.Evidence["reason_suspected"] == nil {
		t.Error("hit: evidence missing reason_suspected")
	}

	// 近似态：TaskKinds 为空(P1) 但 有滚雪雪球突增(100→400000) → 命中 suspected（交语义层确认 task_switch）。
	approxUnavail := SessionFact{
		SessionID: "s-approx", UserID: "u1", PromptSeq: []int64{100, 400000, 410000},
		CachedSeq:    []int64{50, 200000, 200000},
		TaskKinds:    nil, // P1 恒空 → TaskSwitches()==-1
		InputCostSeq: []float64{0.01, 1.0, 1.1}, CacheCostSeq: []float64{0, 0.2, 0.2},
	}
	r2 := EvaluateMultiTaskSession([]SessionFact{approxUnavail}, p, base)
	if r2.Verdict != report.VerdictSuspected || r2.SessionCount != 1 {
		t.Fatalf("task_kind unavailable + snowball jump: verdict=%q session_count=%d, want suspected/1", r2.Verdict, r2.SessionCount)
	}
	if !approx(r2.AttributedCostUSD, 1.0+0.2+1.1+0.2) {
		t.Errorf("approx: attributed = %v, want 2.5", r2.AttributedCostUSD)
	}

	// 近似态 + 无滚雪球突增（长输入一次到位）→ 无「任务在切」代理 → 不命中。
	approxNoJump := SessionFact{
		SessionID: "s-approxnojump", UserID: "u1", PromptSeq: []int64{400000, 410000},
		CachedSeq:    []int64{200000, 200000},
		TaskKinds:    nil,
		InputCostSeq: []float64{1.0, 1.1}, CacheCostSeq: []float64{0.2, 0.2},
	}
	r2b := EvaluateMultiTaskSession([]SessionFact{approxNoJump}, p, base)
	if r2b.Verdict != report.VerdictNotHit || r2b.SessionCount != 0 {
		t.Fatalf("task_kind unavailable + no jump: verdict=%q session_count=%d, want not_hit/0", r2b.Verdict, r2b.SessionCount)
	}

	// 排除：有 reset → 正常切换，不算不切会话。
	resetSession := SessionFact{
		SessionID: "s-reset", UserID: "u1", PromptSeq: []int64{100, 200, 400000, 410000},
		CachedSeq:    []int64{50, 100, 200000, 200000},
		TaskKinds:    []string{"a", "a", "b", "b"}, // switches=1
		ResetFlags:   []bool{false, false, true, false},
		InputCostSeq: []float64{0.01, 0.01, 1.0, 1.1}, CacheCostSeq: []float64{0, 0, 0.2, 0.2},
	}
	r3 := EvaluateMultiTaskSession([]SessionFact{resetSession}, p, base)
	if r3.Verdict != report.VerdictNotHit {
		t.Fatalf("reset: verdict = %q, want not_hit", r3.Verdict)
	}

	// 不命中：不超长。
	notLong := SessionFact{
		SessionID: "s-notlong", UserID: "u1", PromptSeq: []int64{100, 200, 300, 1000},
		CachedSeq:    []int64{50, 100, 150, 500},
		TaskKinds:    []string{"a", "a", "b", "b"},
		ResetFlags:   nil,
		InputCostSeq: []float64{0.01, 0.01, 0.01, 0.01}, CacheCostSeq: []float64{0, 0, 0, 0},
	}
	r4 := EvaluateMultiTaskSession([]SessionFact{notLong}, p, base)
	if r4.Verdict != report.VerdictNotHit {
		t.Fatalf("not long: verdict = %q, want not_hit", r4.Verdict)
	}

	// 无切换（switches==0）→ 不命中。
	noSwitch := SessionFact{
		SessionID: "s-noswitch", UserID: "u1", PromptSeq: []int64{100, 400000, 410000},
		CachedSeq:    []int64{50, 200000, 200000},
		TaskKinds:    []string{"a", "a", "a"},
		ResetFlags:   nil,
		InputCostSeq: []float64{0.01, 1.0, 1.1}, CacheCostSeq: []float64{0, 0.2, 0.2},
	}
	r5 := EvaluateMultiTaskSession([]SessionFact{noSwitch}, p, base)
	if r5.Verdict != report.VerdictNotHit {
		t.Fatalf("no switch: verdict = %q, want not_hit", r5.Verdict)
	}
}

func TestFirstSnowballJumpIdx(t *testing.T) {
	s := SessionFact{PromptSeq: []int64{100, 200, 300, 400000, 410000}}
	if got := firstSnowballJumpIdx(s, 100_000); got != 3 {
		t.Errorf("jump idx = %d, want 3", got)
	}
	noJump := SessionFact{PromptSeq: []int64{100, 110, 120, 130}}
	if got := firstSnowballJumpIdx(noJump, 100_000); got != -1 {
		t.Errorf("no-jump idx = %d, want -1", got)
	}
	short := SessionFact{PromptSeq: []int64{100}}
	if got := firstSnowballJumpIdx(short, 100_000); got != -1 {
		t.Errorf("short idx = %d, want -1", got)
	}
}
