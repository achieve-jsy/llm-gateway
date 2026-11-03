package report_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/observability/report"
)

func mkMeta(level, id string) report.Meta {
	return report.Meta{
		Level:       level,
		Scope:       report.Scope{ID: id, Name: id},
		Period:      report.Period{From: "2026-08-03", To: "2026-08-09"},
		Currency:    report.Currency{Report: "USD", USDCNYRate: 1},
		RuleVersion: "v0.6",
	}
}

// 个人层端到端：结构正确、Σattributed<=total、命中严重异常自动 pending_review。
func TestAssemble_Personal(t *testing.T) {
	meta := mkMeta(report.LevelPersonal, "u1")
	in := report.AssembleInput{
		DataQuality: report.DataQuality{RealSessionCoverage: 0.9, SceneLabelCoverage: 0.8, PriceCatalogCoverage: 1, PositionLabelCoverage: 0.7, FailedCostCoverage: 1},
		CostBreakdown: report.CostBreakdown{
			UncachedInputCostUSD: 10, CacheCostUSD: 2, OutputCostUSD: 5, CacheWriteIncluded: true,
		},
		// 3.6 严重异常命中（红线：status 初始 pending_review）。
		SafetyAnomaly: report.SafetyAnomaly{Hit: true, UnrelatedCostUSD: 5, UnrelatedCostRatio: 0.3, TopModel: "m"},
		PersonalIssues: []report.UsageIssue{
			{Type: report.IssueTypeOutputRunaway, Verdict: report.VerdictConfirmed, RequestCount: 3, RequestShare: 0.06, AttributedCostUSD: 1.0, AffectedCostUSD: 5.0, Confidence: 0.6},
			{Type: report.IssueTypePrefixPollution, Verdict: report.VerdictSuspected, SessionCount: 1, SessionShare: 0.2, AttributedCostUSD: 0.5, AffectedCostUSD: 2.0, Confidence: 0.5},
		},
	}
	r, err := report.Assemble(meta, in, report.DefaultRuleSet())
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if r.Meta.Level != report.LevelPersonal || r.MemberEvaluation != nil {
		t.Errorf("level=%s member_eval not nil", r.Meta.Level)
	}
	if !near(r.CostBreakdown.Total(), 17) {
		t.Errorf("total=%v, want 17", r.CostBreakdown.Total())
	}
	if len(r.UsageIssues) != 2 {
		t.Fatalf("usage_issues=%d, want 2", len(r.UsageIssues))
	}
	// Σattributed = 1.5 ≤ 17（Validate 内部已断言，这里再显式核对）。
	attr := 0.0
	for _, u := range r.UsageIssues {
		attr += u.AttributedCostUSD
	}
	if !near(attr, 1.5) || attr > r.CostBreakdown.Total() {
		t.Errorf("attr sum=%v (>total %v)", attr, r.CostBreakdown.Total())
	}
	// 命中严重异常 → status 初始 pending_review。
	if !r.SafetyAnomaly.Hit || r.SafetyAnomaly.Status != report.SafetyAnomalyPendingReview {
		t.Errorf("safety status=%q, want pending_review", r.SafetyAnomaly.Status)
	}
	// 主桶 uncached_input(10)：
	if r.Conclusion.CostMainBucket != report.CostBucketUncachedInput {
		t.Errorf("cost_main_bucket=%q, want uncached_input", r.Conclusion.CostMainBucket)
	}
	if !near(r.Conclusion.IssueCostRatio, 1.5/17) {
		t.Errorf("issue_ratio=%v, want %v", r.Conclusion.IssueCostRatio, 1.5/17)
	}
	if r.Conclusion.DominantIssue != report.IssueTypeOutputRunaway {
		t.Errorf("dominant=%q, want output_runaway", r.Conclusion.DominantIssue)
	}
	if r.Conclusion.Note == "" {
		t.Errorf("conclusion note empty")
	}
}

// 部门层端到端：MemberEvaluation 四象限/分群/每人 UsageIssues 落位 + 每人 Σattributed<=member.CostUSD。
func TestAssemble_Department(t *testing.T) {
	meta := mkMeta(report.LevelDepartment, "d1")
	m1 := report.MemberAssembleInput{
		Quadrant: report.MemberQuadrantInput{UserID: "m1", Name: "甲", CostUSD: 100, Calls: 10,
			SafetyAnomaly: true, SuspectedOnly: false,
			IssueAttributed: map[string]float64{report.IssueTypeOutputRunaway: 20}},
		CostBreakdown: report.CostBreakdown{UncachedInputCostUSD: 60, CacheCostUSD: 10, OutputCostUSD: 30, TokenBreakdown: report.TokenBreakdown{TotalTokens: 1000, InputTokens: 700, CachedTokens: 200, OutputTokens: 300}},
		UsageIssues: []report.UsageIssue{
			{Type: report.IssueTypeOutputRunaway, Verdict: report.VerdictConfirmed, RequestCount: 4, RequestShare: 0.1, AttributedCostUSD: 20, AffectedCostUSD: 40, Confidence: 0.7},
		},
	}
	m2 := report.MemberAssembleInput{
		Quadrant: report.MemberQuadrantInput{UserID: "m2", Name: "乙", CostUSD: 200, Calls: 20,
			SafetyAnomaly: false, SuspectedOnly: true,
			IssueAttributed: map[string]float64{report.IssueTypePrefixPollution: 10, report.IssueTypeToothpaste: 5}},
		UsageIssues: []report.UsageIssue{
			{Type: report.IssueTypePrefixPollution, Verdict: report.VerdictSuspected, SessionCount: 2, SessionShare: 0.25, AttributedCostUSD: 10, AffectedCostUSD: 30, Confidence: 0.5},
			{Type: report.IssueTypeToothpaste, Verdict: report.VerdictSuspected, SessionCount: 1, SessionShare: 0.5, AttributedCostUSD: 5, AffectedCostUSD: 10, Confidence: 0.5},
		},
	}
	// 顶层也塞入 3.6，但 dept/studio 不落位（3.6 置顶块是个人报告概念；成员级走 MemberPoint.SafetyAnomaly 描边）。
	in := report.AssembleInput{
		CostBreakdown: report.CostBreakdown{UncachedInputCostUSD: 100, OutputCostUSD: 100},
		SafetyAnomaly: report.SafetyAnomaly{Hit: true, UnrelatedCostUSD: 30, UnrelatedCostRatio: 0.3, TopModel: "m"},
		Members:       []report.MemberAssembleInput{m1, m2},
	}
	r, err := report.Assemble(meta, in, report.DefaultRuleSet())
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if r.SafetyAnomaly.Hit {
		t.Error("dept/studio must not set top-level SafetyAnomaly (3.6 is personal-report only)")
	}
	if len(r.UsageIssues) != 0 {
		t.Errorf("top-level usage_issues=%d, want 0 for dept", len(r.UsageIssues))
	}
	me := r.MemberEvaluation
	if me == nil || len(me.Members) != 2 {
		t.Fatalf("member_evaluation missing or wrong size")
	}
	if !near(me.CostMedianUSD, 150) {
		t.Errorf("cost_median=%v, want 150", me.CostMedianUSD)
	}
	// issue ratios [0.2, 0.075] → median 0.1375。
	if !near(me.IssueRatioMedian, 0.1375) {
		t.Errorf("issue_ratio_median=%v, want 0.1375", me.IssueRatioMedian)
	}
	byID := map[string]report.MemberPoint{}
	for _, p := range me.Members {
		byID[p.UserID] = p
	}
	if p := byID["m1"]; p.Quadrant != report.QuadrantCoach || !near(p.CostMultiple, 100.0/150) || !near(p.IssueCostRatio, 0.2) || !p.SafetyAnomaly {
		t.Errorf("m1 quadrant/cost/ratio/safety = %q/%v/%v/%v", p.Quadrant, p.CostMultiple, p.IssueCostRatio, p.SafetyAnomaly)
	} else if !p.IsProblemMember || !p.SafetyDetail.Hit || p.SafetyDetail.Status != report.SafetyAnomalyPendingReview {
		t.Errorf("m1 problem/safety detail not propagated: %+v", p)
	} else if p.TokenBreakdown.TotalTokens != 1000 || !near(p.CostBreakdown.Total(), 100) {
		t.Errorf("m1 token/cost breakdown not propagated: %+v / %+v", p.TokenBreakdown, p.CostBreakdown)
	}
	if p := byID["m2"]; p.Quadrant != report.QuadrantHighCostNormal || !near(p.CostMultiple, 200.0/150) {
		t.Errorf("m2 quadrant/cost = %q/%v", p.Quadrant, p.CostMultiple)
	}
	// 每人 UsageIssues 回填且 Σattributed<=member.CostUSD（Validate 已断言）。
	for _, p := range me.Members {
		attr := 0.0
		for _, u := range p.UsageIssues {
			attr += u.AttributedCostUSD
		}
		if attr > p.CostUSD {
			t.Errorf("member %s attributed=%v > cost=%v", p.UserID, attr, p.CostUSD)
		}
	}
	// 分群：人少(<30)按 dominant 分色。
	if len(me.Clusters) == 0 {
		t.Errorf("expected non-empty clusters")
	}
	// 结论：aggregate dominant = output_runaway(20) > prefix(10) > toothpaste(5)。
	if r.Conclusion.DominantIssue != report.IssueTypeOutputRunaway {
		t.Errorf("conclusion dominant=%q, want output_runaway", r.Conclusion.DominantIssue)
	}
}

func TestAssemble_GroupFiltersZeroCallsAndIncludesConfirmedBelowMedian(t *testing.T) {
	in := report.AssembleInput{
		CostBreakdown: report.CostBreakdown{UncachedInputCostUSD: 30},
		Members: []report.MemberAssembleInput{
			{Quadrant: report.MemberQuadrantInput{UserID: "confirmed", CostUSD: 10, Calls: 1}, UsageIssues: []report.UsageIssue{{Type: report.IssueTypePrefixPollution, Verdict: report.VerdictConfirmed}}},
			{Quadrant: report.MemberQuadrantInput{UserID: "baseline", CostUSD: 20, Calls: 1}},
			{Quadrant: report.MemberQuadrantInput{UserID: "inactive", CostUSD: 0, Calls: 0}},
		},
	}
	r, err := report.Assemble(mkMeta(report.LevelDepartment, "d1"), in, report.DefaultRuleSet())
	if err != nil {
		t.Fatal(err)
	}
	if len(r.MemberEvaluation.Members) != 2 {
		t.Fatalf("members=%d, want only two active callers", len(r.MemberEvaluation.Members))
	}
	for _, member := range r.MemberEvaluation.Members {
		if member.UserID == "confirmed" && !member.IsProblemMember {
			t.Fatal("confirmed 3.1-3.5 member must be classified as problem even below median")
		}
		if member.UserID == "inactive" {
			t.Fatal("zero-call member must not enter organization evaluation")
		}
	}
}

// 工作室层端到端：三成员、含正常成员（dominant=normal）。
func TestAssemble_Studio(t *testing.T) {
	meta := mkMeta(report.LevelStudio, "s1")
	in := report.AssembleInput{
		CostBreakdown: report.CostBreakdown{UncachedInputCostUSD: 50, OutputCostUSD: 50},
		Members: []report.MemberAssembleInput{
			{Quadrant: report.MemberQuadrantInput{UserID: "a", Name: "A", Position: "dev", CostUSD: 60, Calls: 6, IssueAttributed: map[string]float64{report.IssueTypeOutputRunaway: 12}},
				UsageIssues: []report.UsageIssue{{Type: report.IssueTypeOutputRunaway, Verdict: report.VerdictConfirmed, AttributedCostUSD: 12, AffectedCostUSD: 30, Confidence: 0.8}}},
			{Quadrant: report.MemberQuadrantInput{UserID: "b", Name: "B", Position: "pm", CostUSD: 90, Calls: 9, IssueAttributed: map[string]float64{report.IssueTypePrefixPollution: 9}},
				UsageIssues: []report.UsageIssue{{Type: report.IssueTypePrefixPollution, Verdict: report.VerdictSuspected, AttributedCostUSD: 9, AffectedCostUSD: 20, Confidence: 0.6}}},
			{Quadrant: report.MemberQuadrantInput{UserID: "c", Name: "C", Position: "dev", CostUSD: 30, Calls: 3, IssueAttributed: map[string]float64{}},
				UsageIssues: []report.UsageIssue{}},
		},
	}
	r, err := report.Assemble(meta, in, report.DefaultRuleSet())
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	me := r.MemberEvaluation
	if me == nil || len(me.Members) != 3 {
		t.Fatalf("member_evaluation wrong size")
	}
	// 正常成员 c：dominant=normal（无归因）。
	for _, p := range me.Members {
		if p.UserID == "c" && p.DominantIssue != report.IssueTypeNormal {
			t.Errorf("c dominant=%q, want normal", p.DominantIssue)
		}
	}
	// 每个成员 + cluster 都通过 Validate（Validate 已在 Assemble 内调用），此处显式再跑一次确保自洽。
	if err := r.Validate(); err != nil {
		t.Fatalf("re-validate failed: %v", err)
	}
	if r.Conclusion.Note == "" {
		t.Errorf("conclusion note empty")
	}
}
