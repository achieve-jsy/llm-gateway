package service

import (
	"testing"
	"time"
)

func mkRow(reqID, userID, keyID string, ts time.Time, prompt, cached int64, opts ...func(*CallLogRow)) CallLogRow {
	r := CallLogRow{
		RequestID:     reqID,
		UserID:        userID,
		KeyID:         keyID,
		TS:            ts,
		Model:         "gpt-4",
		PromptTokens:  prompt,
		CachedTokens:  cached,
		InputCostUSD:  float64(prompt) * 0.000001,
		OutputCostUSD: 0.0001,
	}
	for _, o := range opts {
		o(&r)
	}
	return r
}

func withUA(ua string) func(*CallLogRow) {
	return func(r *CallLogRow) { r.UserAgent = ua }
}

func withIP(ip string) func(*CallLogRow) {
	return func(r *CallLogRow) { r.IPAddress = ip }
}

var base = time.Date(2025, 1, 1, 9, 0, 0, 0, time.UTC)

// ---------------------------------------------------------------------------
// 派生判据纯函数单测（设计文档 §1.4）
// ---------------------------------------------------------------------------

func TestInputMonotonicRatio(t *testing.T) {
	cases := []struct {
		name string
		seq  []int64
		want float64
	}{
		{"empty", nil, 0},
		{"single", []int64{100}, 0},
		{"all_nondecrease", []int64{100, 100, 200, 300}, 1},
		{"all_decrease", []int64{300, 200, 100}, 0},
		{"mixed", []int64{100, 200, 150, 300}, 2.0 / 3.0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := SessionFact{PromptSeq: c.seq}
			got := f.InputMonotonicRatio()
			if diff := got - c.want; diff > 1e-9 || diff < -1e-9 {
				t.Errorf("InputMonotonicRatio() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestCacheHitRate(t *testing.T) {
	f := SessionFact{PromptSeq: []int64{100, 200}, CachedSeq: []int64{20, 100}}
	got := f.CacheHitRate()
	want := 120.0 / 300.0
	if diff := got - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("CacheHitRate() = %v, want %v", got, want)
	}

	zero := SessionFact{PromptSeq: []int64{0, 0}, CachedSeq: []int64{0, 0}}
	if got := zero.CacheHitRate(); got != 0 {
		t.Errorf("CacheHitRate() with zero prompt sum = %v, want 0 (no div-by-zero)", got)
	}
}

func TestShortInputRatio(t *testing.T) {
	f := SessionFact{PromptSeq: []int64{10, 40, 20, 100}}
	got := f.ShortInputRatio(30)
	want := 0.5 // 10, 20 < 30
	if got != want {
		t.Errorf("ShortInputRatio(30) = %v, want %v", got, want)
	}
	empty := SessionFact{}
	if got := empty.ShortInputRatio(30); got != 0 {
		t.Errorf("ShortInputRatio on empty = %v, want 0", got)
	}
}

func TestSnowballRatio(t *testing.T) {
	f := SessionFact{PromptSeq: []int64{100, 200, 500}}
	if got := f.SnowballRatio(); got != 5 {
		t.Errorf("SnowballRatio() = %v, want 5", got)
	}
	single := SessionFact{PromptSeq: []int64{100}}
	if got := single.SnowballRatio(); got != 0 {
		t.Errorf("SnowballRatio() single turn = %v, want 0", got)
	}
	zeroFirst := SessionFact{PromptSeq: []int64{0, 100}}
	if got := zeroFirst.SnowballRatio(); got != 0 {
		t.Errorf("SnowballRatio() zero first = %v, want 0", got)
	}
}

func TestTaskSwitches(t *testing.T) {
	empty := SessionFact{}
	if got := empty.TaskSwitches(); got != -1 {
		t.Errorf("TaskSwitches() empty TaskKinds = %v, want -1 (不可判定)", got)
	}
	same := SessionFact{TaskKinds: []string{"chat", "chat", "chat"}}
	if got := same.TaskSwitches(); got != 0 {
		t.Errorf("TaskSwitches() same = %v, want 0", got)
	}
	switched := SessionFact{TaskKinds: []string{"chat", "embed", "chat", "chat", "rerank"}}
	if got := switched.TaskSwitches(); got != 3 {
		t.Errorf("TaskSwitches() switched = %v, want 3", got)
	}
}

func TestHasReset(t *testing.T) {
	empty := SessionFact{}
	if empty.HasReset() {
		t.Error("HasReset() on empty ResetFlags (P1 近似态) should be false (保守判无 reset)")
	}
	withReset := SessionFact{ResetFlags: []bool{false, false, true}}
	if !withReset.HasReset() {
		t.Error("HasReset() should be true when any flag is true")
	}
}

// ---------------------------------------------------------------------------
// 会话切分 / 前缀链聚类：R1 错切场景
// ---------------------------------------------------------------------------

// TestR1_LongPauseWithStrongPrefixChain_NotSplit 验证：长任务中途停顿 > 30min，
// 但后续请求的 cached_tokens 证明其携带了前一轮的完整历史前缀（强证据）——
// 纯 30min 超时切分会错误地把它切开，本实现必须判同会话且 evidence=strong。
func TestR1_LongPauseWithStrongPrefixChain_NotSplit(t *testing.T) {
	params := DefaultSessionSplitParams()
	rows := []CallLogRow{
		mkRow("r1", "u1", "k1", base, 1000, 0),
		// 间隔 45min（超过 30min 基线阈值），但 cached=950 ≈ 上一轮 prompt(1000) 的 95%，
		// 证明这一轮携带了上一轮的完整历史前缀 → 应判同会话。
		mkRow("r2", "u1", "k1", base.Add(45*time.Minute), 1200, 950),
		mkRow("r3", "u1", "k1", base.Add(46*time.Minute), 1400, 1150),
	}
	facts := AssembleSessionFacts(rows, params)
	if len(facts) != 1 {
		t.Fatalf("R1: want 1 merged session despite >30min gap, got %d sessions: %+v", len(facts), facts)
	}
	f := facts[0]
	if f.Turns != 3 {
		t.Errorf("R1: want 3 turns merged, got %d", f.Turns)
	}
	if f.SplitEvidence != SplitEvidenceStrong {
		t.Errorf("R1: want split_evidence=strong (前缀链完整衔接), got %s", f.SplitEvidence)
	}
	if !f.Inferred {
		t.Errorf("R1: inferred session should be marked Inferred=true")
	}
}

// TestR1_LongPauseWithoutPrefixEvidence_SplitsWeak 对照组：同样的 45min 停顿，
// 但没有 cache 命中证据（纯超时）——此时不应硬并，弱证据窗口内(<=30min)才会合并；
// 超过窗口且无强证据应拆成两条会话，evidence=weak。
func TestR1_LongPauseWithoutPrefixEvidence_SplitsWeak(t *testing.T) {
	params := DefaultSessionSplitParams()
	rows := []CallLogRow{
		mkRow("r1", "u1", "k1", base, 1000, 0),
		mkRow("r2", "u1", "k1", base.Add(45*time.Minute), 1200, 0), // 无缓存证据 + 超过30min
	}
	facts := AssembleSessionFacts(rows, params)
	if len(facts) != 2 {
		t.Fatalf("want 2 sessions (无前缀证据的长间隔应拆分), got %d: %+v", len(facts), facts)
	}
}

// TestBaselineWeakMerge_WithinTimeout 纯超时弱证据合并的正常场景：间隔 <=30min 且无强证据，
// 仍应合并为一个会话，但 evidence=weak（对齐设计文档「纯 30min 超时推断」）。
func TestBaselineWeakMerge_WithinTimeout(t *testing.T) {
	params := DefaultSessionSplitParams()
	rows := []CallLogRow{
		mkRow("r1", "u1", "k1", base, 1000, 0),
		mkRow("r2", "u1", "k1", base.Add(10*time.Minute), 900, 0),
	}
	facts := AssembleSessionFacts(rows, params)
	if len(facts) != 1 {
		t.Fatalf("want 1 session within timeout, got %d", len(facts))
	}
	if facts[0].SplitEvidence != SplitEvidenceWeak {
		t.Errorf("want split_evidence=weak (纯超时推断), got %s", facts[0].SplitEvidence)
	}
}

// ---------------------------------------------------------------------------
// R2 错并场景：同一 (user_id, key_id) 上的并发/多设备互不包含累积链
// ---------------------------------------------------------------------------

// TestR2_ConcurrentInterleavedChains_NotMerged 验证：同一时间窗内交替出现两条互不包含的
// 累积链（模拟同 key 多设备并发使用）——纯时间排序会把它们错并成一条混乱的会话；
// 本实现必须按前缀链证据拆成两条不按时间穿插的独立会话。
func TestR2_ConcurrentInterleavedChains_NotMerged(t *testing.T) {
	params := DefaultSessionSplitParams()
	// 链 A：设备1，任务从 1000 起步累积到 3000（每轮都包含上一轮的完整前缀）。
	// 链 B：设备2，任务从 500 起步累积到 1500，与 A 完全独立。
	// 两条链的请求在时间线上交替出现（A1 B1 A2 B2 A3 B3），间隔都在 5min 内。
	rows := []CallLogRow{
		mkRow("a1", "u1", "k1", base, 1000, 0, withIP("10.0.0.1")),
		mkRow("b1", "u1", "k1", base.Add(1*time.Minute), 500, 0, withIP("10.0.0.2")),
		mkRow("a2", "u1", "k1", base.Add(2*time.Minute), 2000, 950, withIP("10.0.0.1")),  // 包含a1前缀(950/1000=95%)
		mkRow("b2", "u1", "k1", base.Add(3*time.Minute), 1000, 480, withIP("10.0.0.2")),  // 包含b1前缀(480/500=96%)
		mkRow("a3", "u1", "k1", base.Add(4*time.Minute), 3000, 1950, withIP("10.0.0.1")), // 包含a2前缀(1950/2000=97.5%)
		mkRow("b3", "u1", "k1", base.Add(5*time.Minute), 1500, 980, withIP("10.0.0.2")),  // 包含b2前缀(980/1000=98%)
	}
	facts := AssembleSessionFacts(rows, params)
	if len(facts) != 2 {
		t.Fatalf("R2: want 2 independent concurrent sessions, got %d: %+v", len(facts), facts)
	}

	// 每条链应各自完整衔接（strong），且互不混入对方的请求。
	byFirstReq := map[string]SessionFact{}
	for _, f := range facts {
		if len(f.ReqIDs) == 0 {
			continue
		}
		byFirstReq[f.ReqIDs[0]] = f
	}
	chainA, okA := byFirstReq["a1"]
	chainB, okB := byFirstReq["b1"]
	if !okA || !okB {
		t.Fatalf("R2: expected chains starting at a1 and b1, got facts=%+v", facts)
	}
	if chainA.Turns != 3 || chainB.Turns != 3 {
		t.Errorf("R2: want each chain to have 3 turns (not interleaved/merged), got chainA=%d chainB=%d", chainA.Turns, chainB.Turns)
	}
	for _, id := range chainA.ReqIDs {
		if id == "b1" || id == "b2" || id == "b3" {
			t.Errorf("R2: chain A must not contain chain B's requests, got %v", chainA.ReqIDs)
		}
	}
	if chainA.SplitEvidence != SplitEvidenceStrong || chainB.SplitEvidence != SplitEvidenceStrong {
		t.Errorf("R2: both concurrent chains should each have strong evidence, got A=%s B=%s", chainA.SplitEvidence, chainB.SplitEvidence)
	}
}

// ---------------------------------------------------------------------------
// R3 误并场景：批处理式请求
// ---------------------------------------------------------------------------

// TestR3_BatchRequests_MarkedBatchNotMerged 验证：一批 prompt 大小相近、互不累积、
// 间隔很短的请求（典型批处理/脚本调用）——纯超时切分会把它们误并成一条长会话；
// 本实现必须把每条标记为 batch，且不参与交互类语义判定（即不产生多轮 SessionFact）。
func TestR3_BatchRequests_MarkedBatchNotMerged(t *testing.T) {
	params := DefaultSessionSplitParams()
	rows := []CallLogRow{
		mkRow("p1", "u1", "k1", base, 500, 0),
		mkRow("p2", "u1", "k1", base.Add(10*time.Second), 520, 5),
		mkRow("p3", "u1", "k1", base.Add(20*time.Second), 480, 3),
		mkRow("p4", "u1", "k1", base.Add(30*time.Second), 510, 8),
	}
	facts := AssembleSessionFacts(rows, params)
	if len(facts) != 4 {
		t.Fatalf("R3: want 4 independent batch facts (not merged into one session), got %d: %+v", len(facts), facts)
	}
	for _, f := range facts {
		if f.SplitEvidence != SplitEvidenceBatch {
			t.Errorf("R3: want split_evidence=batch for each request, got %s (fact=%+v)", f.SplitEvidence, f)
		}
		if f.Turns != 1 {
			t.Errorf("R3: batch facts must be single-turn (标不并会话), got turns=%d", f.Turns)
		}
		if !f.IsBatch() {
			t.Errorf("R3: IsBatch() should be true for batch evidence")
		}
	}
}

// TestR3_ShortRunNotBatch 验证批处理判定需要连续满足 run 长度（BatchMinRun），
// 只有 2 条相近-不累积的请求不足以判定为批处理（避免把偶发的两次短问误判）。
func TestR3_ShortRunNotBatch(t *testing.T) {
	params := DefaultSessionSplitParams()
	rows := []CallLogRow{
		mkRow("p1", "u1", "k1", base, 500, 0),
		mkRow("p2", "u1", "k1", base.Add(10*time.Second), 520, 5),
	}
	facts := AssembleSessionFacts(rows, params)
	for _, f := range facts {
		if f.SplitEvidence == SplitEvidenceBatch {
			t.Errorf("run of length 2 (< BatchMinRun=%d) should not be classified as batch, got %+v", params.BatchMinRun, f)
		}
	}
}

// TestR3_AccumulatingChain_NotBatch 对照组：即使间隔很短、大小相近，只要存在真实累积
// （cache 命中体现前缀包含），也不应被误判为批处理，必须走前缀链聚类判同会话。
func TestR3_AccumulatingChain_NotBatch(t *testing.T) {
	params := DefaultSessionSplitParams()
	rows := []CallLogRow{
		mkRow("c1", "u1", "k1", base, 1000, 0),
		mkRow("c2", "u1", "k1", base.Add(5*time.Second), 1050, 960), // 累积：包含c1前缀
		mkRow("c3", "u1", "k1", base.Add(10*time.Second), 1100, 1010),
	}
	facts := AssembleSessionFacts(rows, params)
	if len(facts) != 1 {
		t.Fatalf("want 1 accumulating session (not misclassified as batch), got %d: %+v", len(facts), facts)
	}
	if facts[0].IsBatch() {
		t.Errorf("accumulating chain must not be classified as batch")
	}
	if facts[0].Turns != 3 {
		t.Errorf("want 3 turns merged, got %d", facts[0].Turns)
	}
}

// ---------------------------------------------------------------------------
// 综合场景：不同 (user_id, key_id) 分组互不干扰；多个用户/key 混合输入。
// ---------------------------------------------------------------------------

func TestAssembleSessionFacts_GroupIsolation(t *testing.T) {
	params := DefaultSessionSplitParams()
	rows := []CallLogRow{
		mkRow("u1r1", "u1", "k1", base, 1000, 0),
		mkRow("u2r1", "u2", "k2", base, 1000, 0),
		mkRow("u1r2", "u1", "k1", base.Add(5*time.Minute), 900, 0),
		mkRow("u2r2", "u2", "k2", base.Add(5*time.Minute), 900, 0),
	}
	facts := AssembleSessionFacts(rows, params)
	if len(facts) != 2 {
		t.Fatalf("want 2 sessions (one per user/key group), got %d: %+v", len(facts), facts)
	}
	for _, f := range facts {
		if f.Turns != 2 {
			t.Errorf("want each group's 2 requests merged within timeout, got turns=%d for user=%s", f.Turns, f.UserID)
		}
	}
}

func TestAssembleSessionFacts_Empty(t *testing.T) {
	facts := AssembleSessionFacts(nil, DefaultSessionSplitParams())
	if len(facts) != 0 {
		t.Errorf("want 0 facts for empty input, got %d", len(facts))
	}
}

// TestSessionIDDeterministic 验证同一输入多次装配得到相同的 SessionID 序列（确定性排序）。
func TestSessionIDDeterministic(t *testing.T) {
	params := DefaultSessionSplitParams()
	rows := []CallLogRow{
		mkRow("r1", "u1", "k1", base, 1000, 0),
		mkRow("r2", "u1", "k1", base.Add(45*time.Minute), 900, 0), // weak split, gap>30min无证据 -> 新会话
	}
	f1 := AssembleSessionFacts(rows, params)
	f2 := AssembleSessionFacts(rows, params)
	if len(f1) != len(f2) {
		t.Fatalf("nondeterministic fact count: %d vs %d", len(f1), len(f2))
	}
	for i := range f1 {
		if f1[i].SessionID != f2[i].SessionID {
			t.Errorf("SessionID not deterministic: %s vs %s", f1[i].SessionID, f2[i].SessionID)
		}
	}
}
