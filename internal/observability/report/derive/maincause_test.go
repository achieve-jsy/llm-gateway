package derive

// maincause_test.go —— 单元【本周结论】主因归因（②）白盒单测。
//
// 验证 buildUnitConclusion / assembleUnitMainCause / FillUnitConclusion 三段式主因句：
//   「成本环比 ±X%，集中在 N 名<岗位>，其一命中<异常>」
// 数据只来自 UnitFact（WoWCostPct + TopChanged[].{JobCategory,WoWCostPct,AnomalyCause}），
// 不依赖 Judgment / 不直连库；纯函数、可复现。

import (
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/observability/report"
)

func TestBuildUnitConclusion_ConcentratedWithCause(t *testing.T) {
	u := &report.UnitFact{
		WoWCostPct: 32,
		TopChanged: []report.TopChangedMember{
			{Idx: 1, JobCategory: "后端开发", WoWCostPct: 90, AnomalyCause: "A5 非工作时段"},
			{Idx: 2, JobCategory: "后端开发", WoWCostPct: 45},
			{Idx: 3, JobCategory: "后端开发", WoWCostPct: 20},
			{Idx: 4, JobCategory: "前端开发", WoWCostPct: 15},
		},
	}
	con := buildUnitConclusion(u)
	if con.MainCause != "成本环比 +32%，集中在 3 名后端开发，其一命中A5 非工作时段" {
		t.Fatalf("MainCause=%q, want %q", con.MainCause, "成本环比 +32%，集中在 3 名后端开发，其一命中A5 非工作时段")
	}
	if con.CostDeltaPct != 32 {
		t.Errorf("CostDeltaPct=%v, want 32", con.CostDeltaPct)
	}
	if con.ConcentrationN != 3 || con.ConcentrationJob != "后端开发" {
		t.Errorf("concentration=%d,%q, want 3,后端开发", con.ConcentrationN, con.ConcentrationJob)
	}
	if con.AnomalyWord != "其一命中A5 非工作时段" {
		t.Errorf("AnomalyWord=%q", con.AnomalyWord)
	}
	if len(con.AnomalyLabels) != 1 || con.AnomalyLabels[0] != "A5 非工作时段" {
		t.Errorf("AnomalyLabels=%v", con.AnomalyLabels)
	}
}

// 多成员命中异常 → 「其中 K 人命中」。
func TestBuildUnitConclusion_MultipleCauseMembers(t *testing.T) {
	u := &report.UnitFact{
		WoWCostPct: 50,
		TopChanged: []report.TopChangedMember{
			{Idx: 1, JobCategory: "后端开发", WoWCostPct: 80, AnomalyCause: "A2 成本燃烧"},
			{Idx: 2, JobCategory: "后端开发", WoWCostPct: 40, AnomalyCause: "A5 非工作时段"},
			{Idx: 3, JobCategory: "后端开发", WoWCostPct: 10},
		},
	}
	con := buildUnitConclusion(u)
	if con.AnomalyWord != "其中 2 人命中A2 成本燃烧、A5 非工作时段" {
		t.Errorf("AnomalyWord=%q", con.AnomalyWord)
	}
	if !strings.Contains(con.MainCause, "集中在 3 名后端开发") && !strings.Contains(con.MainCause, "其中 2 人命中") {
		t.Errorf("MainCause=%q", con.MainCause)
	}
}

// 成本下降且无增长成员 → 只有成本环比前缀，无集中/异常。
func TestBuildUnitConclusion_DecreaseOnly(t *testing.T) {
	u := &report.UnitFact{
		WoWCostPct: -15,
		TopChanged: []report.TopChangedMember{
			{Idx: 1, JobCategory: "后端开发", WoWCostPct: -60},
		},
	}
	con := buildUnitConclusion(u)
	if con.MainCause != "成本环比 -15%" {
		t.Errorf("MainCause=%q, want %q", con.MainCause, "成本环比 -15%")
	}
	if con.ConcentrationN != 0 || len(con.AnomalyLabels) != 0 {
		t.Errorf("decrease should have no concentration/anomaly, got n=%d labels=%v", con.ConcentrationN, con.AnomalyLabels)
	}
}

// 无事实（nil）→ Supported=false。
func TestBuildUnitConclusion_Nil(t *testing.T) {
	con := buildUnitConclusion(nil)
	if con.Supported {
		t.Error("nil should be unsupported")
	}
	if con.MainCause != "" {
		t.Errorf("nil MainCause=%q, want empty", con.MainCause)
	}
}

// FillUnitConclusion：nil 不 panic，且对 department/studio 同时回填；再次调用幂等覆盖。
func TestFillUnitConclusion_Idempotent(t *testing.T) {
	rep := &report.Report{
		Fact: report.Fact{
			Department: &report.UnitFact{
				WoWCostPct: 32,
				TopChanged: []report.TopChangedMember{
					{Idx: 1, JobCategory: "后端开发", WoWCostPct: 90, AnomalyCause: "A5 非工作时段"},
					{Idx: 2, JobCategory: "后端开发", WoWCostPct: 45},
				},
			},
			Studio: &report.UnitFact{
				WoWCostPct: 10,
				TopChanged: []report.TopChangedMember{
					{Idx: 1, JobCategory: "算法", WoWCostPct: 30},
				},
			},
		},
	}
	FillUnitConclusion(rep)
	if rep.Fact.Department.Conclusion == nil || rep.Fact.Department.Conclusion.MainCause != "成本环比 +32%，集中在 2 名后端开发，其一命中A5 非工作时段" {
		t.Fatalf("department conclusion=%+v", rep.Fact.Department.Conclusion)
	}
	if rep.Fact.Studio.Conclusion == nil || rep.Fact.Studio.Conclusion.MainCause != "成本环比 +10%，集中于 1 名算法" {
		t.Fatalf("studio conclusion=%+v", rep.Fact.Studio.Conclusion)
	}
	// 幂等：再次调用不改变结果（覆盖为同值）。
	first := rep.Fact.Department.Conclusion.MainCause
	FillUnitConclusion(rep)
	if rep.Fact.Department.Conclusion.MainCause != first {
		t.Errorf("idempotency broken: %q -> %q", first, rep.Fact.Department.Conclusion.MainCause)
	}
	// nil 安全
	FillUnitConclusion(nil)
}

// 无增长成员、无异常、无环比 → MainCause 为空（supported 仍 true，避免误导）。
func TestBuildUnitConclusion_NoNarrative(t *testing.T) {
	u := &report.UnitFact{WoWCostPct: 0, TopChanged: nil}
	con := buildUnitConclusion(u)
	if !con.Supported {
		t.Error("unit fact present → supported")
	}
	if con.MainCause != "" {
		t.Errorf("MainCause=%q, want empty（无环比/集中/异常时不产出主因句）", con.MainCause)
	}
}
