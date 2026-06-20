package derive_test

// derive_studio_t2_test.go —— t2 DeptContributions 单元测试：
//   - buildDeptContributions 按 DeptID 聚合（成本/人数/人均/占比）
//   - studio 级别调用后 UnitFact.DeptContributions 填充正确
//   - department 级别不填充 DeptContributions（保持 nil）
//   - DeptName 映射：有 DeptNames → 显示名称；无映射 → 回退 DeptID
//   - 空 DeptID 归入 "(未分配部门)"
//   - 全部非活跃成员 → 返回 nil
//   - CostRatio 总成本为 0 时为 0
//   - 按 CostUSD 降序排列

import (
	"math"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/observability/report"
	"github.com/example/llm-gateway/internal/observability/report/derive"
)

// makeStudioMeta 构造 studio 级别的测试 Meta。
func makeStudioMeta(id, name string) report.Meta {
	return report.Meta{
		Level:    "studio",
		Period:   report.Period{From: "2026-08-01", To: "2026-08-07", Type: "week", Label: "w", Days: 7},
		Scope:    report.Scope{Level: "studio", ID: id, Name: name},
		Currency: report.Currency{Base: "USD", ReportCurrency: "CNY", USDCNYRate: 7.2, RateSource: "test", RateAt: time.Now().Format(time.RFC3339)},
		QA:       report.QA{Alignment: []string{""}, AnomalyEnabled: false},
	}
}

// ============================================================
// 1. 基本聚合：CostUSD / ActiveUsers / ReqCount / AvgCostUSD / CostRatio
// ============================================================

func TestDeptContributions_BasicAggregation(t *testing.T) {
	in := report.ReportInput{
		MemberAgg: []report.MemberAgg{
			{UserID: "u1", DeptID: "d1", ReqCount: 100, TotalTokens: 1000, CostUSD: 6.0},
			{UserID: "u2", DeptID: "d1", ReqCount: 50, TotalTokens: 500, CostUSD: 2.0},
			{UserID: "u3", DeptID: "d2", ReqCount: 80, TotalTokens: 800, CostUSD: 4.0},
		},
		DeptNames: map[string]string{
			"d1": "研发部",
			"d2": "产品部",
		},
		LevelMeta: report.Scope{Level: "studio", ID: "s1", Name: "工作室一"},
	}
	rep, err := derive.Derive(makeStudioMeta("s1", "工作室一"), in, report.DefaultRuleSet())
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	u := rep.Fact.Studio
	if u == nil {
		t.Fatal("expected studio fact")
	}

	dcs := u.DeptContributions
	if len(dcs) != 2 {
		t.Fatalf("expected 2 DeptContributions, got %d: %+v", len(dcs), dcs)
	}

	// 按 CostUSD 降序：d1(8.0) > d2(4.0)
	if dcs[0].DeptID != "d1" {
		t.Errorf("first dept should be d1 (highest cost), got %q", dcs[0].DeptID)
	}
	if dcs[0].DeptName != "研发部" {
		t.Errorf("d1 DeptName=%q, want 研发部", dcs[0].DeptName)
	}
	if dcs[0].ActiveUsers != 2 {
		t.Errorf("d1 ActiveUsers=%d, want 2", dcs[0].ActiveUsers)
	}
	wantCostD1 := 8.0
	if math.Abs(dcs[0].CostUSD-wantCostD1) > 1e-9 {
		t.Errorf("d1 CostUSD=%v, want %v", dcs[0].CostUSD, wantCostD1)
	}
	if dcs[0].ReqCount != 150 {
		t.Errorf("d1 ReqCount=%d, want 150", dcs[0].ReqCount)
	}
	// AvgCostUSD = 8.0 / 2 = 4.0
	if math.Abs(dcs[0].AvgCostUSD-4.0) > 1e-9 {
		t.Errorf("d1 AvgCostUSD=%v, want 4.0", dcs[0].AvgCostUSD)
	}
	// CostRatio = 8.0 / 12.0 ≈ 0.6667
	wantRatioD1 := 8.0 / 12.0
	if math.Abs(dcs[0].CostRatio-wantRatioD1) > 1e-9 {
		t.Errorf("d1 CostRatio=%v, want %v", dcs[0].CostRatio, wantRatioD1)
	}

	// d2
	if dcs[1].DeptID != "d2" {
		t.Errorf("second dept should be d2, got %q", dcs[1].DeptID)
	}
	if dcs[1].DeptName != "产品部" {
		t.Errorf("d2 DeptName=%q, want 产品部", dcs[1].DeptName)
	}
	if dcs[1].ActiveUsers != 1 {
		t.Errorf("d2 ActiveUsers=%d, want 1", dcs[1].ActiveUsers)
	}
	wantRatioD2 := 4.0 / 12.0
	if math.Abs(dcs[1].CostRatio-wantRatioD2) > 1e-9 {
		t.Errorf("d2 CostRatio=%v, want %v", dcs[1].CostRatio, wantRatioD2)
	}
}

// ============================================================
// 2. CostRatio 总和应为 1（浮点精度内）
// ============================================================

func TestDeptContributions_CostRatioSumsToOne(t *testing.T) {
	in := report.ReportInput{
		MemberAgg: []report.MemberAgg{
			{UserID: "u1", DeptID: "d1", ReqCount: 100, TotalTokens: 1000, CostUSD: 3.0},
			{UserID: "u2", DeptID: "d2", ReqCount: 50, TotalTokens: 500, CostUSD: 5.0},
			{UserID: "u3", DeptID: "d3", ReqCount: 80, TotalTokens: 800, CostUSD: 2.0},
		},
		LevelMeta: report.Scope{Level: "studio", ID: "s1", Name: "工作室一"},
	}
	rep, err := derive.Derive(makeStudioMeta("s1", "工作室一"), in, report.DefaultRuleSet())
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	dcs := rep.Fact.Studio.DeptContributions
	var totalRatio float64
	for _, dc := range dcs {
		totalRatio += dc.CostRatio
	}
	if math.Abs(totalRatio-1.0) > 1e-9 {
		t.Errorf("CostRatio sum=%v, want 1.0", totalRatio)
	}
}

// ============================================================
// 3. DeptName 映射：无 DeptNames 时回退 DeptID
// ============================================================

func TestDeptContributions_FallbackDeptName(t *testing.T) {
	in := report.ReportInput{
		MemberAgg: []report.MemberAgg{
			{UserID: "u1", DeptID: "dept-abc", ReqCount: 100, TotalTokens: 1000, CostUSD: 5.0},
		},
		// DeptNames 为空，DeptName 应回退 DeptID
		LevelMeta: report.Scope{Level: "studio", ID: "s1", Name: "工作室一"},
	}
	rep, err := derive.Derive(makeStudioMeta("s1", "工作室一"), in, report.DefaultRuleSet())
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	dcs := rep.Fact.Studio.DeptContributions
	if len(dcs) != 1 {
		t.Fatalf("expected 1, got %d", len(dcs))
	}
	if dcs[0].DeptName != "dept-abc" {
		t.Errorf("DeptName=%q, want %q (fallback to DeptID)", dcs[0].DeptName, "dept-abc")
	}
}

// ============================================================
// 4. 空 DeptID 归入 "(未分配部门)"
// ============================================================

func TestDeptContributions_EmptyDeptID_FallsToUnassigned(t *testing.T) {
	in := report.ReportInput{
		MemberAgg: []report.MemberAgg{
			{UserID: "u1", DeptID: "", ReqCount: 50, TotalTokens: 500, CostUSD: 3.0},
			{UserID: "u2", DeptID: "d1", ReqCount: 80, TotalTokens: 800, CostUSD: 5.0},
		},
		DeptNames: map[string]string{"d1": "研发部"},
		LevelMeta: report.Scope{Level: "studio", ID: "s1", Name: "工作室一"},
	}
	rep, err := derive.Derive(makeStudioMeta("s1", "工作室一"), in, report.DefaultRuleSet())
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	dcs := rep.Fact.Studio.DeptContributions
	if len(dcs) != 2 {
		t.Fatalf("expected 2, got %d: %+v", len(dcs), dcs)
	}
	// 验证存在 "(未分配部门)"
	found := false
	for _, dc := range dcs {
		if dc.DeptID == "(未分配部门)" {
			found = true
			if dc.DeptName != "(未分配部门)" {
				t.Errorf("unassigned DeptName=%q, want (未分配部门)", dc.DeptName)
			}
		}
	}
	if !found {
		t.Errorf("expected dept (未分配部门) to be present in %+v", dcs)
	}
}

// ============================================================
// 5. 非活跃成员（全零行）不计入
// ============================================================

func TestDeptContributions_InactiveMembersExcluded(t *testing.T) {
	in := report.ReportInput{
		MemberAgg: []report.MemberAgg{
			{UserID: "u1", DeptID: "d1", ReqCount: 100, TotalTokens: 1000, CostUSD: 5.0},
			// 全零行：跳过
			{UserID: "u2", DeptID: "d1", ReqCount: 0, TotalTokens: 0, CostUSD: 0},
		},
		LevelMeta: report.Scope{Level: "studio", ID: "s1", Name: "工作室一"},
	}
	rep, err := derive.Derive(makeStudioMeta("s1", "工作室一"), in, report.DefaultRuleSet())
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	dcs := rep.Fact.Studio.DeptContributions
	if len(dcs) != 1 {
		t.Fatalf("expected 1, got %d: %+v", len(dcs), dcs)
	}
	if dcs[0].ActiveUsers != 1 {
		t.Errorf("ActiveUsers=%d, want 1 (inactive excluded)", dcs[0].ActiveUsers)
	}
}

// ============================================================
// 6. 全部成员非活跃 → DeptContributions 为 nil
// ============================================================

func TestDeptContributions_AllInactive_ReturnsNil(t *testing.T) {
	in := report.ReportInput{
		MemberAgg: []report.MemberAgg{
			{UserID: "u1", DeptID: "d1", ReqCount: 0, TotalTokens: 0, CostUSD: 0},
		},
		LevelMeta: report.Scope{Level: "studio", ID: "s1", Name: "工作室一"},
	}
	rep, err := derive.Derive(makeStudioMeta("s1", "工作室一"), in, report.DefaultRuleSet())
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if rep.Fact.Studio.DeptContributions != nil {
		t.Errorf("expected nil DeptContributions when all inactive, got %+v", rep.Fact.Studio.DeptContributions)
	}
}

// ============================================================
// 7. department 级别不产出 DeptContributions（保持 nil）
// ============================================================

func TestDeptContributions_NotFilledForDepartmentLevel(t *testing.T) {
	in := report.ReportInput{
		MemberAgg: []report.MemberAgg{
			{UserID: "u1", DeptID: "d1", ReqCount: 100, TotalTokens: 1000, CostUSD: 5.0},
		},
		LevelMeta: report.Scope{Level: "department", ID: "d1", Name: "部门一"},
	}
	meta := report.Meta{
		Level:    "department",
		Period:   report.Period{From: "2026-08-01", To: "2026-08-07", Type: "week", Label: "w", Days: 7},
		Scope:    report.Scope{Level: "department", ID: "d1", Name: "部门一"},
		Currency: report.Currency{Base: "USD", ReportCurrency: "CNY", USDCNYRate: 7.2, RateSource: "test", RateAt: time.Now().Format(time.RFC3339)},
		QA:       report.QA{Alignment: []string{""}, AnomalyEnabled: false},
	}
	rep, err := derive.Derive(meta, in, report.DefaultRuleSet())
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	u := rep.Fact.Department
	if u == nil {
		t.Fatal("expected department fact")
	}
	if u.DeptContributions != nil {
		t.Errorf("DeptContributions should be nil for department level, got %+v", u.DeptContributions)
	}
}

// ============================================================
// 8. CostRatio = 0 when totalCost = 0（成员只有 ReqCount，无 CostUSD）
// ============================================================

func TestDeptContributions_ZeroCostRatio_WhenNoCost(t *testing.T) {
	in := report.ReportInput{
		MemberAgg: []report.MemberAgg{
			// 仅有 tokens，无 cost（CostUSD=0 但 TotalTokens>0 → 活跃）
			{UserID: "u1", DeptID: "d1", ReqCount: 100, TotalTokens: 1000, CostUSD: 0},
		},
		LevelMeta: report.Scope{Level: "studio", ID: "s1", Name: "工作室一"},
	}
	rep, err := derive.Derive(makeStudioMeta("s1", "工作室一"), in, report.DefaultRuleSet())
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	dcs := rep.Fact.Studio.DeptContributions
	if len(dcs) != 1 {
		t.Fatalf("expected 1, got %d", len(dcs))
	}
	if dcs[0].CostRatio != 0 {
		t.Errorf("CostRatio=%v, want 0 when totalCost=0", dcs[0].CostRatio)
	}
	if dcs[0].AvgCostUSD != 0 {
		t.Errorf("AvgCostUSD=%v, want 0 when CostUSD=0", dcs[0].AvgCostUSD)
	}
}

// ============================================================
// 9. 排序：按 CostUSD 降序
// ============================================================

func TestDeptContributions_SortedByCostDesc(t *testing.T) {
	in := report.ReportInput{
		MemberAgg: []report.MemberAgg{
			{UserID: "u1", DeptID: "d_low", ReqCount: 10, TotalTokens: 100, CostUSD: 1.0},
			{UserID: "u2", DeptID: "d_high", ReqCount: 200, TotalTokens: 2000, CostUSD: 10.0},
			{UserID: "u3", DeptID: "d_mid", ReqCount: 50, TotalTokens: 500, CostUSD: 5.0},
		},
		LevelMeta: report.Scope{Level: "studio", ID: "s1", Name: "工作室一"},
	}
	rep, err := derive.Derive(makeStudioMeta("s1", "工作室一"), in, report.DefaultRuleSet())
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	dcs := rep.Fact.Studio.DeptContributions
	if len(dcs) != 3 {
		t.Fatalf("expected 3, got %d", len(dcs))
	}
	if dcs[0].DeptID != "d_high" {
		t.Errorf("first dept should be d_high, got %q", dcs[0].DeptID)
	}
	if dcs[1].DeptID != "d_mid" {
		t.Errorf("second dept should be d_mid, got %q", dcs[1].DeptID)
	}
	if dcs[2].DeptID != "d_low" {
		t.Errorf("third dept should be d_low, got %q", dcs[2].DeptID)
	}
}
