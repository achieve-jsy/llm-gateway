package derive

// derive_studio_vicky_test.go —— t2 Vicky 工作室模板派生层单元测试：
//   - buildDeptContributionWoW：部门成本周环比（WoWCostPct）+ 成本集中度（Top2 部门占比）
//   - buildStudioSummary：全公司占比（CompanyRatio）/ 异常 Severity 统计（AnomalySummary）/ 4 周趋势 / 可优化成本估值
//   - buildStudioAttribution：5 类变化归因增量拆解（两个场景：模型切换+调用量、新项目），及无上期数据降级

import (
	"math"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/observability/report"
)

// ============================================================
// 1. buildDeptContributionWoW：部门环比 + 成本集中度
// ============================================================

func TestStudioVicky_DeptWoWAndConcentration(t *testing.T) {
	in := report.ReportInput{
		MemberAgg: []report.MemberAgg{
			{UserID: "u1", DeptID: "d1", ReqCount: 100, TotalTokens: 1000, CostUSD: 8},
			{UserID: "u2", DeptID: "d1", ReqCount: 50, TotalTokens: 500, CostUSD: 0}, // 活跃（req/tok>0），无成本
			{UserID: "u3", DeptID: "d2", ReqCount: 80, TotalTokens: 800, CostUSD: 4},
			{UserID: "u4", DeptID: "d3", ReqCount: 40, TotalTokens: 400, CostUSD: 2},
		},
		PrevMemberAgg: []report.MemberAgg{
			{UserID: "p1", DeptID: "d1", ReqCount: 100, TotalTokens: 1000, CostUSD: 5},
			{UserID: "p2", DeptID: "d2", ReqCount: 80, TotalTokens: 800, CostUSD: 10},
			{UserID: "p3", DeptID: "d3", ReqCount: 40, TotalTokens: 400, CostUSD: 2},
		},
		DeptNames: map[string]string{"d1": "研发部", "d2": "产品部", "d3": "运营部"},
	}

	dcs := buildDeptContributions(in)
	if len(dcs) != 3 {
		t.Fatalf("expected 3 depts, got %d: %+v", len(dcs), dcs)
	}
	dcs, conc := buildDeptContributionWoW(in, dcs)

	// 成本集中度 = Top2 部门占比之和 = (8 + 4) / 14
	wantConc := 12.0 / 14.0
	if math.Abs(conc-wantConc) > 1e-9 {
		t.Errorf("CostConcentration=%v, want %v", conc, wantConc)
	}

	// 各部门环比（按 DeptID 断言，避免依赖排序）
	for _, dc := range dcs {
		switch dc.DeptID {
		case "d1":
			if math.Abs(dc.WoWCostPct-60.0) > 1e-9 { // (8-5)/5*100
				t.Errorf("d1 WoWCostPct=%v, want 60", dc.WoWCostPct)
			}
		case "d2":
			if math.Abs(dc.WoWCostPct-(-60.0)) > 1e-9 { // (4-10)/10*100
				t.Errorf("d2 WoWCostPct=%v, want -60", dc.WoWCostPct)
			}
		case "d3":
			if math.Abs(dc.WoWCostPct-0.0) > 1e-9 { // (2-2)/2*100
				t.Errorf("d3 WoWCostPct=%v, want 0", dc.WoWCostPct)
			}
		}
	}
}

// ============================================================
// 2. buildStudioSummary：全公司占比 / 异常统计 / 4 周趋势 / 可优化成本
// ============================================================

func TestStudioVicky_BuildStudioSummary(t *testing.T) {
	u := &report.UnitFact{}
	u.Member.EstCostUSD = 10
	u.Member.CacheHitRate = 0.2 // 命中缓存优化

	judge := &report.Judgment{Anomalies: []report.AnomalyHit{
		{Rule: "A2", Severity: "P1", Hit: true},
		{Rule: "A5", Severity: "P2", Hit: true},
		{Rule: "A3", Severity: "P2", Hit: true, NotComputed: true}, // 不计入
		{Rule: "A4", Severity: "P2", Hit: false},                   // 未命中，不计入
	}}

	in := report.ReportInput{
		CompanyCostUSD: 100,
		WeeklyTrend: []report.WeeklyPoint{
			{WeekStart: "2026-08-01", WeekEnd: "2026-08-07", CostUSD: 10, ReqCount: 100},
			{WeekStart: "2026-07-25", WeekEnd: "2026-07-31", CostUSD: 8, ReqCount: 90},
		},
		UserTopModels: map[string][]report.CohortTopModel{
			"u1": {{Model: "m1", CostUSD: 6, CallCount: 100, TotalTokens: 1000}},
		},
	}

	buildStudioSummary(u, in, judge)

	// 全公司占比 = 10 / 100
	if math.Abs(u.CompanyRatio-0.1) > 1e-9 {
		t.Errorf("CompanyRatio=%v, want 0.1", u.CompanyRatio)
	}
	// 全公司成本注入
	if u.CompanyCostUSD != 100 {
		t.Errorf("CompanyCostUSD=%v, want 100", u.CompanyCostUSD)
	}
	// 4 周趋势拷贝
	if len(u.WeeklyTrend) != 2 || u.WeeklyTrend[0].CostUSD != 10 {
		t.Errorf("WeeklyTrend not copied: %+v", u.WeeklyTrend)
	}
	// 异常统计：P1×1, P2×1（A3 未计算排除，A4 未命中排除），按 P0<P1<P2<P3 排序
	if len(u.AnomalySummary) != 2 {
		t.Fatalf("AnomalySummary len=%d, want 2: %+v", len(u.AnomalySummary), u.AnomalySummary)
	}
	if u.AnomalySummary[0].Severity != "P1" || u.AnomalySummary[0].Count != 1 {
		t.Errorf("AnomalySummary[0]=%+v, want P1×1", u.AnomalySummary[0])
	}
	if u.AnomalySummary[1].Severity != "P2" || u.AnomalySummary[1].Count != 1 {
		t.Errorf("AnomalySummary[1]=%+v, want P2×1", u.AnomalySummary[1])
	}
	// 可优化成本：缓存 10*0.1=1 + 高价值模型 6*0.2=1.2 = 2.2
	if math.Abs(u.OptimizableCostUSD-2.2) > 1e-9 {
		t.Errorf("OptimizableCostUSD=%v, want 2.2", u.OptimizableCostUSD)
	}
}

func TestStudioVicky_BuildStudioSummary_NoCompanyCost(t *testing.T) {
	u := &report.UnitFact{}
	u.Member.EstCostUSD = 10
	in := report.ReportInput{CompanyCostUSD: 0} // 0 时不下发占比
	buildStudioSummary(u, in, nil)
	if u.CompanyRatio != 0 {
		t.Errorf("CompanyRatio=%v, want 0 when CompanyCostUSD=0", u.CompanyRatio)
	}
	if u.OptimizableCostUSD <= 0 {
		t.Errorf("OptimizableCostUSD should be nonzero estimate, got %v", u.OptimizableCostUSD)
	}
}

// ============================================================
// 3. buildStudioAttribution：5 类增量拆解
// ============================================================

func TestStudioVicky_Attribution_VolumeAndSwitch(t *testing.T) {
	in := report.ReportInput{
		UserTopModels: map[string][]report.CohortTopModel{
			"u1": {{Model: "m1", CostUSD: 10, CallCount: 100, TotalTokens: 1000}},
			"u2": {{Model: "m2", CostUSD: 5, CallCount: 50, TotalTokens: 500}},
		},
		PrevUserTopModels: map[string][]report.CohortTopModel{
			"u1": {{Model: "m1", CostUSD: 8, CallCount: 80, TotalTokens: 800}},
			"u2": {{Model: "m2", CostUSD: 3, CallCount: 40, TotalTokens: 400}},
		},
	}
	attrs := buildStudioAttribution(in)
	if len(attrs) != 5 {
		t.Fatalf("expected 5 attributions, got %d: %+v", len(attrs), attrs)
	}
	if attrs[0].Subject != "业务调用量增长" {
		t.Errorf("attrs[0].Subject=%q, want 业务调用量增长", attrs[0].Subject)
	}
	if attrs[0].Weight <= 0 {
		t.Errorf("业务调用量增长 Weight=%v, want >0", attrs[0].Weight)
	}
	if attrs[1].Subject != "模型切换" {
		t.Errorf("attrs[1].Subject=%q, want 模型切换", attrs[1].Subject)
	}
	// 本场景无新项目/错误/上下文变化，前两项 Weight 归一 ≈1
	if math.Abs(attrs[0].Weight+attrs[1].Weight-1.0) > 1e-9 {
		t.Errorf("weight sum of top two=%v, want 1.0 (got %+v)", attrs[0].Weight+attrs[1].Weight, attrs)
	}
}

func TestStudioVicky_Attribution_NewProject(t *testing.T) {
	in := report.ReportInput{
		UserTopModels: map[string][]report.CohortTopModel{
			"u1":    {{Model: "m1", CostUSD: 10, CallCount: 100, TotalTokens: 1000}},
			"u_new": {{Model: "m3", CostUSD: 20, CallCount: 100, TotalTokens: 2000}}, // 本期新增用户/项目
		},
		PrevUserTopModels: map[string][]report.CohortTopModel{
			"u1": {{Model: "m1", CostUSD: 8, CallCount: 80, TotalTokens: 800}},
		},
	}
	attrs := buildStudioAttribution(in)
	if len(attrs) != 5 {
		t.Fatalf("expected 5 attributions, got %d: %+v", len(attrs), attrs)
	}
	var newProject *report.Attribution
	for i := range attrs {
		if attrs[i].Subject == "新项目" {
			newProject = &attrs[i]
		}
	}
	if newProject == nil {
		t.Fatalf("expected 新项目 attribution, got %+v", attrs)
	}
	if newProject.Weight <= 0 {
		t.Errorf("新项目 Weight=%v, want >0 (新增用户/模型成本)", newProject.Weight)
	}
	if math.Abs(newProject.Weight-20.0/22.0) > 1e-9 { // Weight=d5/totalDelta：d5=20, totalDelta=30-8=22
		t.Errorf("新项目 Weight=%v, want %v", newProject.Weight, 20.0/22.0)
	}
}

func TestStudioVicky_Attribution_NoPrevDataReturnsNil(t *testing.T) {
	in := report.ReportInput{
		UserTopModels: map[string][]report.CohortTopModel{
			"u1": {{Model: "m1", CostUSD: 10, CallCount: 100, TotalTokens: 1000}},
		},
		// 无 PrevUserTopModels -> 降级 nil
	}
	if attrs := buildStudioAttribution(in); attrs != nil {
		t.Errorf("expected nil when no PrevUserTopModels, got %+v", attrs)
	}
}

// ============================================================
// 4. 集成：Derive 的 studio 分支正确调用三个派生函数并写入 UnitFact / judge.Attribution
// ============================================================

func TestStudioVicky_DeriveWiresStudioFields(t *testing.T) {
	in := report.ReportInput{
		MemberAgg: []report.MemberAgg{
			{UserID: "u1", DeptID: "d1", ReqCount: 100, TotalTokens: 1000, CostUSD: 8},
			{UserID: "u2", DeptID: "d2", ReqCount: 50, TotalTokens: 500, CostUSD: 4},
		},
		PrevMemberAgg: []report.MemberAgg{
			{UserID: "p1", DeptID: "d1", ReqCount: 100, TotalTokens: 1000, CostUSD: 5},
			{UserID: "p2", DeptID: "d2", ReqCount: 50, TotalTokens: 500, CostUSD: 10},
		},
		CompanyCostUSD: 100,
		LevelMeta:      report.Scope{Level: "studio", ID: "s1", Name: "工作室一"},
		UserTopModels: map[string][]report.CohortTopModel{
			"u1": {{Model: "m1", CostUSD: 8, CallCount: 100, TotalTokens: 1000}},
			"u2": {{Model: "m2", CostUSD: 4, CallCount: 50, TotalTokens: 500}},
		},
		PrevUserTopModels: map[string][]report.CohortTopModel{
			"u1": {{Model: "m1", CostUSD: 5, CallCount: 80, TotalTokens: 800}},
			"u2": {{Model: "m2", CostUSD: 10, CallCount: 40, TotalTokens: 400}},
		},
	}
	meta := report.Meta{
		Level:    "studio",
		Period:   report.Period{From: "2026-08-01", To: "2026-08-07", Type: "week", Label: "w", Days: 7},
		Scope:    report.Scope{Level: "studio", ID: "s1", Name: "工作室一"},
		Currency: report.Currency{Base: "USD", ReportCurrency: "CNY", USDCNYRate: 7.2, RateSource: "test", RateAt: time.Now().Format(time.RFC3339)},
		QA:       report.QA{Alignment: []string{""}, AnomalyEnabled: false},
	}
	rep, err := Derive(meta, in, report.DefaultRuleSet())
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	u := rep.Fact.Studio
	if u == nil {
		t.Fatal("expected studio fact")
	}
	// 全公司占比 = (8+4)/100 = 0.12
	if math.Abs(u.CompanyRatio-0.12) > 1e-9 {
		t.Errorf("CompanyRatio=%v, want 0.12", u.CompanyRatio)
	}
	// 成本集中度：仅 2 部门，Top2 = 1.0
	if math.Abs(u.CostConcentration-1.0) > 1e-9 {
		t.Errorf("CostConcentration=%v, want 1.0", u.CostConcentration)
	}
	// 部门环比：d1 (8-5)/5=60%；d2 (4-10)/10=-60%
	for _, dc := range u.DeptContributions {
		switch dc.DeptID {
		case "d1":
			if math.Abs(dc.WoWCostPct-60) > 1e-9 {
				t.Errorf("d1 WoWCostPct=%v, want 60", dc.WoWCostPct)
			}
		case "d2":
			if math.Abs(dc.WoWCostPct-(-60)) > 1e-9 {
				t.Errorf("d2 WoWCostPct=%v, want -60", dc.WoWCostPct)
			}
		}
	}
	// 变化归因：有上期模型数据 -> 5 条 studio attribution
	if len(rep.Judgment.Attribution) != 5 {
		t.Fatalf("expected 5 studio attributions, got %d: %+v", len(rep.Judgment.Attribution), rep.Judgment.Attribution)
	}
	if rep.Judgment.Attribution[0].Subject != "业务调用量增长" {
		t.Errorf("attribution[0].Subject=%q, want 业务调用量增长", rep.Judgment.Attribution[0].Subject)
	}
}
