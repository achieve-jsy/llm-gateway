package derive

// maincause_test.go —— 单元【本周结论】主因归因（②）白盒单测。
//
// 验证 buildUnitConclusion / assembleUnitMainCause / FillUnitConclusion 三段式主因句：
//   「成本环比 ±X%，集中在 N 名<岗位>，命中 <A1/A2/A5>」
// 数据只来自 UnitFact（WoWCostPct + TopChanged[].{JobCategory,WoWCostPct,AnomalyCause}），
// 不依赖 Judgment / 不直连库；纯函数、可复现。
//
// 第 1 个用例用队长提供的 X-Lab 真实读数（成本环比 +16.4%；
// 员工1 +501.7% 主因A1 / 员工2 +204.3% A2 / 员工3 +203.3% A1A2A5）锁定期望输出，
// 验证「真实读数」而非样例数值。

import (
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/observability/report"
)

// X-Lab 真实读数（队长提供）：三段式主因句。
// 该部门成员岗位归一后落「其他」（无具体 job_title/category/family），主因回退为「未归类岗位」。
func TestBuildUnitConclusion_XLabReal(t *testing.T) {
	u := &report.UnitFact{
		WoWCostPct: 16.4,
		TopChanged: []report.TopChangedMember{
			{Idx: 1, JobCategory: "其他", WoWCostPct: 501.7, AnomalyCause: "A1 程序失控"},
			{Idx: 2, JobCategory: "其他", WoWCostPct: 204.3, AnomalyCause: "A2 成本燃烧"},
			{Idx: 3, JobCategory: "其他", WoWCostPct: 203.3, AnomalyCause: "A5 非工作时段; A1 程序失控; A2 成本燃烧"},
		},
	}
	con := buildUnitConclusion(u)
	want := "成本环比 +16.4%，集中在 3 名未归类岗位，命中 A1/A2/A5"
	if con.MainCause != want {
		t.Fatalf("MainCause=%q, want %q", con.MainCause, want)
	}
	if con.CostDeltaPct != 16.4 {
		t.Errorf("CostDeltaPct=%v, want 16.4", con.CostDeltaPct)
	}
	if con.ConcentrationN != 3 || con.ConcentrationJob != "未归类岗位" {
		t.Errorf("concentration=%d,%q, want 3,未归类岗位", con.ConcentrationN, con.ConcentrationJob)
	}
	if con.AnomalyWord != "命中 A1/A2/A5" {
		t.Errorf("AnomalyWord=%q", con.AnomalyWord)
	}
	if strings.Join(con.RuleCodes, "/") != "A1/A2/A5" {
		t.Errorf("RuleCodes=%v, want [A1 A2 A5]", con.RuleCodes)
	}
	if len(con.AnomalyLabels) == 0 {
		t.Error("AnomalyLabels should carry full rule+name labels")
	}
}

// 残差岗位名但成员有更具体 job_title：回退到更具体岗位名（优先 job_title 非占位值）。
func TestBuildUnitConclusion_ResidualJobFallsBackToSpecific(t *testing.T) {
	u := &report.UnitFact{
		WoWCostPct: 20,
		TopChanged: []report.TopChangedMember{
			{Idx: 1, JobCategory: "其他", JobTitle: "后端开发工程师", WoWCostPct: 80, AnomalyCause: "A1 程序失控"},
			{Idx: 2, JobCategory: "其他", JobTitle: "后端开发工程师", WoWCostPct: 50, AnomalyCause: "A2 成本燃烧"},
			{Idx: 3, JobCategory: "其他", JobTitle: "后端开发工程师", WoWCostPct: 40, AnomalyCause: "A5 非工作时段"},
		},
	}
	con := buildUnitConclusion(u)
	if con.ConcentrationJob != "后端开发工程师" {
		t.Errorf("ConcentrationJob=%q, want 后端开发工程师（回退 job_title）", con.ConcentrationJob)
	}
	if con.MainCause != "成本环比 +20.0%，集中在 3 名后端开发工程师，命中 A1/A2/A5" {
		t.Errorf("MainCause=%q", con.MainCause)
	}
}

// 残差岗位名变体（其他/其它/未知/未分类/空）：无具体 job_title → 一律「未归类岗位」。
func TestBuildUnitConclusion_ResidualJobVariants(t *testing.T) {
	for _, residual := range []string{"其他", "其它", "未知", "未分类", "", "未标注"} {
		u := &report.UnitFact{
			WoWCostPct: 10,
			TopChanged: []report.TopChangedMember{
				{Idx: 1, JobCategory: residual, WoWCostPct: 30},
				{Idx: 2, JobCategory: residual, WoWCostPct: 20},
			},
		}
		con := buildUnitConclusion(u)
		if con.ConcentrationJob != "未归类岗位" {
			t.Errorf("residual=%q → ConcentrationJob=%q, want 未归类岗位", residual, con.ConcentrationJob)
		}
		if con.ConcentrationN != 2 {
			t.Errorf("residual=%q → ConcentrationN=%d, want 2", residual, con.ConcentrationN)
		}
	}
}

// 只有 1 名增长成员且岗位为「其他」→ 未归类岗位（N=1）。
func TestBuildUnitConclusion_ResidualJobSingle(t *testing.T) {
	u := &report.UnitFact{
		WoWCostPct: 5,
		TopChanged: []report.TopChangedMember{
			{Idx: 1, JobCategory: "其他", WoWCostPct: 15},
		},
	}
	con := buildUnitConclusion(u)
	if con.ConcentrationN != 1 || con.ConcentrationJob != "未归类岗位" {
		t.Errorf("concentration=%d,%q, want 1,未归类岗位", con.ConcentrationN, con.ConcentrationJob)
	}
}

// isResidualJob 判定各占位/残差值与具体岗位名。
func TestIsResidualJob(t *testing.T) {
	for _, s := range []string{"", "其他", "其它", "未知", "未分类", "未归类", "未标注", "N/A", "  其他  "} {
		if !isResidualJob(s) {
			t.Errorf("isResidualJob(%q)=false, want true", s)
		}
	}
	for _, s := range []string{"后端开发", "算法工程师", "WEB前端开发", "后端开发工程师"} {
		if isResidualJob(s) {
			t.Errorf("isResidualJob(%q)=true, want false", s)
		}
	}
}

// 无成本环比且无异常 → MainCause 为空（不产出误导性主因句）。
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

// 成本下降且无增长成员 → 只有成本环比前缀。
func TestBuildUnitConclusion_DecreaseOnly(t *testing.T) {
	u := &report.UnitFact{
		WoWCostPct: -15,
		TopChanged: []report.TopChangedMember{
			{Idx: 1, JobCategory: "后端开发", WoWCostPct: -60},
		},
	}
	con := buildUnitConclusion(u)
	if con.MainCause != "成本环比 -15.0%" {
		t.Errorf("MainCause=%q, want %q", con.MainCause, "成本环比 -15.0%")
	}
	if con.ConcentrationN != 0 || len(con.RuleCodes) != 0 {
		t.Errorf("decrease should have no concentration/anomaly, got n=%d rules=%v", con.ConcentrationN, con.RuleCodes)
	}
}

// 多岗位增长成员：取主流岗位，规则码取并集。
func TestBuildUnitConclusion_MixedJobs(t *testing.T) {
	u := &report.UnitFact{
		WoWCostPct: 40,
		TopChanged: []report.TopChangedMember{
			{Idx: 1, JobCategory: "后端开发", WoWCostPct: 80, AnomalyCause: "A2 成本燃烧"},
			{Idx: 2, JobCategory: "后端开发", WoWCostPct: 40, AnomalyCause: "A5 非工作时段"},
			{Idx: 3, JobCategory: "算法", WoWCostPct: 30, AnomalyCause: "A1 程序失控"},
		},
	}
	con := buildUnitConclusion(u)
	if con.ConcentrationN != 2 || con.ConcentrationJob != "后端开发" {
		t.Errorf("concentration=%d,%q, want 2,后端开发", con.ConcentrationN, con.ConcentrationJob)
	}
	if con.AnomalyWord != "命中 A1/A2/A5" {
		t.Errorf("AnomalyWord=%q, want 命中 A1/A2/A5（并集）", con.AnomalyWord)
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

// FillUnitConclusion：department/studio 同时回填，nil 不 panic，幂等覆盖。
func TestFillUnitConclusion_Idempotent(t *testing.T) {
	rep := &report.Report{
		Fact: report.Fact{
			Department: &report.UnitFact{
				WoWCostPct: 16.4,
				TopChanged: []report.TopChangedMember{
					{Idx: 1, JobCategory: "后端开发", WoWCostPct: 501.7, AnomalyCause: "A1 程序失控"},
					{Idx: 2, JobCategory: "后端开发", WoWCostPct: 204.3, AnomalyCause: "A2 成本燃烧"},
				},
			},
			Studio: &report.UnitFact{
				WoWCostPct: 10,
				TopChanged: []report.TopChangedMember{{Idx: 1, JobCategory: "算法", WoWCostPct: 30}},
			},
		},
	}
	FillUnitConclusion(rep)
	if rep.Fact.Department.Conclusion == nil || rep.Fact.Department.Conclusion.MainCause != "成本环比 +16.4%，集中在 2 名后端开发，命中 A1/A2" {
		t.Fatalf("department conclusion=%+v", rep.Fact.Department.Conclusion)
	}
	if rep.Fact.Studio.Conclusion == nil || rep.Fact.Studio.Conclusion.MainCause != "成本环比 +10.0%，集中于 1 名算法" {
		t.Fatalf("studio conclusion=%+v", rep.Fact.Studio.Conclusion)
	}
	// 幂等：再次调用不改变结果（覆盖为同值）。
	first := rep.Fact.Department.Conclusion.MainCause
	FillUnitConclusion(rep)
	if rep.Fact.Department.Conclusion.MainCause != first {
		t.Errorf("idempotency broken: %q -> %q", first, rep.Fact.Department.Conclusion.MainCause)
	}
	FillUnitConclusion(nil) // 不应 panic
}
