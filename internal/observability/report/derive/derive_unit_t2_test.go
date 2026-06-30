package derive_test

// derive_unit_t2_test.go —— t2 新增字段的单元测试：
//   - UnitMember.CacheHitRate / CostPer1Million（agg 模式 + 明细模式）
//   - UnitFact.JobGroups（岗位分组）
//   - UnitFact.WoWReqPct / WoWTokenPct / WoWCostPct（周环比）
//   - UnitFact.TopChanged（Top 变化人员，匿名）

import (
	"math"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/observability/report"
	"github.com/example/llm-gateway/internal/observability/report/derive"
)

// ---- 公共测试夹具 ----

func makeAggMeta(level, id, name string) report.Meta {
	return report.Meta{
		Level:    level,
		Period:   report.Period{From: "2026-08-01", To: "2026-08-07", Type: "week", Label: "w", Days: 7},
		Scope:    report.Scope{Level: level, ID: id, Name: name},
		Currency: report.Currency{Base: "USD", ReportCurrency: "CNY", USDCNYRate: 7.2, RateSource: "test", RateAt: time.Now().Format(time.RFC3339)},
		QA:       report.QA{Alignment: []string{""}, AnomalyEnabled: false},
	}
}

// ============================================================
// 1. UnitMember.CacheHitRate / CostPer1Million（agg 模式）
// ============================================================

func TestUnitMemberEfficiency_AggMode(t *testing.T) {
	in := report.ReportInput{
		MemberAgg: []report.MemberAgg{
			{UserID: "u1", ReqCount: 100, PromptTokens: 1000, CachedTokens: 200, TotalTokens: 1500, CostUSD: 3},
			{UserID: "u2", ReqCount: 50, PromptTokens: 500, CachedTokens: 100, TotalTokens: 700, CostUSD: 1.4},
		},
		LevelMeta: report.Scope{Level: "department", ID: "d1", Name: "部门一"},
	}
	rep, err := derive.Derive(makeAggMeta("department", "d1", "部门一"), in, report.DefaultRuleSet())
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	u := rep.Fact.Department
	if u == nil {
		t.Fatal("expected department fact")
	}

	// CacheHitRate = (200+100)/(1000+500) = 300/1500 = 0.2
	wantHitRate := 300.0 / 1500.0
	if math.Abs(u.Member.CacheHitRate-wantHitRate) > 1e-9 {
		t.Errorf("CacheHitRate=%v, want %v", u.Member.CacheHitRate, wantHitRate)
	}

	// CostPer1Million = (3+1.4)/(1500+700)*1e6 = 4.4/2200*1e6 ≈ 2000
	wantCPM := 4.4 / 2200.0 * 1e6
	if math.Abs(u.Member.CostPer1Million-wantCPM) > 1e-6 {
		t.Errorf("CostPer1Million=%v, want %v", u.Member.CostPer1Million, wantCPM)
	}
}

// CacheHitRate=0 when prompt=0
func TestUnitMemberEfficiency_AggMode_ZeroPrompt(t *testing.T) {
	in := report.ReportInput{
		MemberAgg: []report.MemberAgg{
			{UserID: "u1", ReqCount: 10, PromptTokens: 0, CachedTokens: 0, TotalTokens: 100, CostUSD: 0.5},
		},
		LevelMeta: report.Scope{Level: "department", ID: "d1", Name: "部门一"},
	}
	rep, err := derive.Derive(makeAggMeta("department", "d1", "部门一"), in, report.DefaultRuleSet())
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	u := rep.Fact.Department
	if u.Member.CacheHitRate != 0 {
		t.Errorf("CacheHitRate should be 0 when prompt=0, got %v", u.Member.CacheHitRate)
	}
}

// ============================================================
// 2. UnitMember.CacheHitRate / CostPer1Million（明细模式）
// ============================================================

func TestUnitMemberEfficiency_DetailMode(t *testing.T) {
	in := report.ReportInput{
		Metrics: []report.DailyMetric{
			{Date: "2026-08-01", Level: "department", ScopeID: "d1", UserID: "u1", KeyType: "personal",
				PromptTokens: 800, CachedTokens: 160, TotalTokens: 1200, EstCostUSD: 2.4},
			{Date: "2026-08-02", Level: "department", ScopeID: "d1", UserID: "u2", KeyType: "personal",
				PromptTokens: 400, CachedTokens: 40, TotalTokens: 600, EstCostUSD: 1.2},
		},
		LevelMeta: report.Scope{Level: "department", ID: "d1", Name: "部门一"},
	}
	rep, err := derive.Derive(makeAggMeta("department", "d1", "部门一"), in, report.DefaultRuleSet())
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	u := rep.Fact.Department
	if u == nil {
		t.Fatal("expected department fact")
	}

	// CacheHitRate = (160+40)/(800+400) = 200/1200 ≈ 0.1667
	wantHitRate := 200.0 / 1200.0
	if math.Abs(u.Member.CacheHitRate-wantHitRate) > 1e-9 {
		t.Errorf("CacheHitRate=%v, want %v", u.Member.CacheHitRate, wantHitRate)
	}

	// CostPer1Million = (2.4+1.2)/(1200+600)*1e6 = 3.6/1800*1e6 = 2000
	wantCPM := 3.6 / 1800.0 * 1e6
	if math.Abs(u.Member.CostPer1Million-wantCPM) > 1e-6 {
		t.Errorf("CostPer1Million=%v, want %v", u.Member.CostPer1Million, wantCPM)
	}
}

// ============================================================
// 3. JobGroups（岗位分组）
// ============================================================

func TestJobGroups_BasicGrouping(t *testing.T) {
	in := report.ReportInput{
		MemberAgg: []report.MemberAgg{
			{UserID: "u1", JobCategory: "工程师", ReqCount: 100, TotalTokens: 1000, CostUSD: 4.0},
			{UserID: "u2", JobCategory: "工程师", ReqCount: 50, TotalTokens: 500, CostUSD: 2.0},
			{UserID: "u3", JobCategory: "产品", ReqCount: 30, TotalTokens: 300, CostUSD: 1.5},
			{UserID: "u4", JobCategory: "", ReqCount: 20, TotalTokens: 200, CostUSD: 0.8}, // 归入"其他"
		},
		LevelMeta: report.Scope{Level: "department", ID: "d1", Name: "部门一"},
	}
	rep, err := derive.Derive(makeAggMeta("department", "d1", "部门一"), in, report.DefaultRuleSet())
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	u := rep.Fact.Department
	if u == nil {
		t.Fatal("expected department fact")
	}

	// 应有 3 组：工程师、产品、其他
	if len(u.JobGroups) != 3 {
		t.Fatalf("expected 3 job groups, got %d: %+v", len(u.JobGroups), u.JobGroups)
	}

	// 按 AvgCostUSD 降序：工程师 AvgCost=(4+2)/2=3, 产品=1.5, 其他=0.8
	if u.JobGroups[0].JobCategory != "工程师" {
		t.Errorf("first group should be 工程师 (highest avg cost), got %q", u.JobGroups[0].JobCategory)
	}
	if u.JobGroups[0].ActiveUsers != 2 {
		t.Errorf("工程师 active users=%d, want 2", u.JobGroups[0].ActiveUsers)
	}
	wantAvg := 6.0 / 2.0 // (4+2)/2
	if math.Abs(u.JobGroups[0].AvgCostUSD-wantAvg) > 1e-9 {
		t.Errorf("工程师 AvgCostUSD=%v, want %v", u.JobGroups[0].AvgCostUSD, wantAvg)
	}

	// 第 2 组：产品
	if u.JobGroups[1].JobCategory != "产品" {
		t.Errorf("second group should be 产品, got %q", u.JobGroups[1].JobCategory)
	}

	// 第 3 组：其他
	if u.JobGroups[2].JobCategory != "其他" {
		t.Errorf("third group should be 其他, got %q", u.JobGroups[2].JobCategory)
	}
}

func TestJobGroups_MedianCostPerReq(t *testing.T) {
	// 同岗 3 人：per-req cost 分别为 0.04, 0.05, 0.06 → 中位数应为 0.05
	in := report.ReportInput{
		MemberAgg: []report.MemberAgg{
			{UserID: "u1", JobCategory: "工程师", ReqCount: 100, TotalTokens: 1000, CostUSD: 4.0}, // 0.04/req
			{UserID: "u2", JobCategory: "工程师", ReqCount: 100, TotalTokens: 1000, CostUSD: 5.0}, // 0.05/req
			{UserID: "u3", JobCategory: "工程师", ReqCount: 100, TotalTokens: 1000, CostUSD: 6.0}, // 0.06/req
		},
		LevelMeta: report.Scope{Level: "department", ID: "d1", Name: "部门一"},
	}
	rep, err := derive.Derive(makeAggMeta("department", "d1", "部门一"), in, report.DefaultRuleSet())
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	u := rep.Fact.Department
	jgs := u.JobGroups
	if len(jgs) != 1 {
		t.Fatalf("expected 1 group, got %d", len(jgs))
	}
	wantMedian := 0.05
	if math.Abs(jgs[0].MedianCostUSD-wantMedian) > 1e-9 {
		t.Errorf("MedianCostUSD=%v, want %v", jgs[0].MedianCostUSD, wantMedian)
	}
}

func TestJobGroups_InactiveExcluded(t *testing.T) {
	// req=0 的成员不计入活跃，不贡献 ActiveUsers 或 perReqCost
	in := report.ReportInput{
		MemberAgg: []report.MemberAgg{
			{UserID: "u1", JobCategory: "工程师", ReqCount: 100, TotalTokens: 1000, CostUSD: 5.0},
			{UserID: "u2", JobCategory: "工程师", ReqCount: 0, TotalTokens: 0, CostUSD: 0},
		},
		LevelMeta: report.Scope{Level: "department", ID: "d1", Name: "部门一"},
	}
	rep, err := derive.Derive(makeAggMeta("department", "d1", "部门一"), in, report.DefaultRuleSet())
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	u := rep.Fact.Department
	jgs := u.JobGroups
	// u2 应被跳过（req=0 和 cost=0 和 token=0）
	if len(jgs) != 1 {
		t.Fatalf("expected 1 group, got %d", len(jgs))
	}
	if jgs[0].ActiveUsers != 1 {
		t.Errorf("active users=%d, want 1", jgs[0].ActiveUsers)
	}
}

func TestJobGroups_Max10Groups(t *testing.T) {
	// 12 个岗位 → 截断到 10 组
	aggs := make([]report.MemberAgg, 12)
	for i := range aggs {
		cat := "岗位" + itoa(i)
		aggs[i] = report.MemberAgg{
			UserID:      "u" + itoa(i),
			JobCategory: cat,
			ReqCount:    10,
			TotalTokens: 100,
			CostUSD:     float64(i) * 0.1,
		}
	}
	in := report.ReportInput{
		MemberAgg: aggs,
		LevelMeta: report.Scope{Level: "department", ID: "d1", Name: "部门一"},
	}
	rep, err := derive.Derive(makeAggMeta("department", "d1", "部门一"), in, report.DefaultRuleSet())
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	u := rep.Fact.Department
	if len(u.JobGroups) != 10 {
		t.Errorf("expected max 10 job groups, got %d", len(u.JobGroups))
	}
}

// itoa helper in test (reuse the one from derive package via string conversion, or reimplement)
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		p--
		b[p] = '-'
	}
	return string(b[p:])
}

// ============================================================
// 4. 周环比 WoWReqPct / WoWTokenPct / WoWCostPct
// ============================================================

func TestWoW_BasicCalculation(t *testing.T) {
	in := report.ReportInput{
		MemberAgg: []report.MemberAgg{
			{UserID: "u1", ReqCount: 150, TotalTokens: 1500, CostUSD: 6.0},
			{UserID: "u2", ReqCount: 50, TotalTokens: 500, CostUSD: 2.0},
		},
		PrevMemberAgg: []report.MemberAgg{
			{UserID: "u1", ReqCount: 100, TotalTokens: 1000, CostUSD: 4.0},
			{UserID: "u2", ReqCount: 50, TotalTokens: 500, CostUSD: 2.0},
		},
		LevelMeta: report.Scope{Level: "department", ID: "d1", Name: "部门一"},
	}
	rep, err := derive.Derive(makeAggMeta("department", "d1", "部门一"), in, report.DefaultRuleSet())
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	u := rep.Fact.Department

	// curReq=200, prevReq=150 → WoWReqPct = (200-150)/150*100 ≈ 33.33
	wantReqPct := (200.0 - 150.0) / 150.0 * 100
	if math.Abs(u.WoWReqPct-wantReqPct) > 1e-6 {
		t.Errorf("WoWReqPct=%v, want %v", u.WoWReqPct, wantReqPct)
	}

	// curTokens=2000, prevTokens=1500 → WoWTokenPct = (2000-1500)/1500*100 ≈ 33.33
	wantTokenPct := (2000.0 - 1500.0) / 1500.0 * 100
	if math.Abs(u.WoWTokenPct-wantTokenPct) > 1e-6 {
		t.Errorf("WoWTokenPct=%v, want %v", u.WoWTokenPct, wantTokenPct)
	}

	// curCost=8.0, prevCost=6.0 → WoWCostPct = (8-6)/6*100 ≈ 33.33
	wantCostPct := (8.0 - 6.0) / 6.0 * 100
	if math.Abs(u.WoWCostPct-wantCostPct) > 1e-6 {
		t.Errorf("WoWCostPct=%v, want %v", u.WoWCostPct, wantCostPct)
	}
}

func TestWoW_ZeroWhenPrevEmpty(t *testing.T) {
	in := report.ReportInput{
		MemberAgg: []report.MemberAgg{
			{UserID: "u1", ReqCount: 100, TotalTokens: 1000, CostUSD: 5.0},
		},
		// PrevMemberAgg 为空 → WoW 字段全为 0
		LevelMeta: report.Scope{Level: "department", ID: "d1", Name: "部门一"},
	}
	rep, err := derive.Derive(makeAggMeta("department", "d1", "部门一"), in, report.DefaultRuleSet())
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	u := rep.Fact.Department
	if u.WoWReqPct != 0 || u.WoWTokenPct != 0 || u.WoWCostPct != 0 {
		t.Errorf("WoW should be 0 when PrevMemberAgg empty, got req=%v tok=%v cost=%v", u.WoWReqPct, u.WoWTokenPct, u.WoWCostPct)
	}
}

func TestWoW_ZeroPrevHandled(t *testing.T) {
	// prevReq=0 → WoWReqPct 置 0；prevCost>0 → WoWCostPct 正常计算
	in := report.ReportInput{
		MemberAgg: []report.MemberAgg{
			{UserID: "u1", ReqCount: 100, TotalTokens: 1000, CostUSD: 6.0},
		},
		PrevMemberAgg: []report.MemberAgg{
			{UserID: "u1", ReqCount: 0, TotalTokens: 0, CostUSD: 3.0},
		},
		LevelMeta: report.Scope{Level: "department", ID: "d1", Name: "部门一"},
	}
	rep, err := derive.Derive(makeAggMeta("department", "d1", "部门一"), in, report.DefaultRuleSet())
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	u := rep.Fact.Department
	// prevReq=0 → WoWReqPct=0
	if u.WoWReqPct != 0 {
		t.Errorf("WoWReqPct should be 0 when prevReq=0, got %v", u.WoWReqPct)
	}
	// prevCost=3, curCost=6 → WoWCostPct = (6-3)/3*100 = 100
	if math.Abs(u.WoWCostPct-100) > 1e-6 {
		t.Errorf("WoWCostPct=%v, want 100", u.WoWCostPct)
	}
}

// ============================================================
// 5. TopChanged（Top 变化人员，匿名）
// ============================================================

func TestTopChanged_BasicAnonymized(t *testing.T) {
	in := report.ReportInput{
		MemberAgg: []report.MemberAgg{
			{UserID: "u1", JobCategory: "工程师", ReqCount: 100, TotalTokens: 1000, CostUSD: 10.0},
			{UserID: "u2", JobCategory: "产品", ReqCount: 50, TotalTokens: 500, CostUSD: 2.0},
			{UserID: "u3", JobCategory: "工程师", ReqCount: 30, TotalTokens: 300, CostUSD: 3.0},
		},
		PrevMemberAgg: []report.MemberAgg{
			// u1: cost 从 5 增到 10 → +100%
			{UserID: "u1", JobCategory: "工程师", ReqCount: 80, TotalTokens: 800, CostUSD: 5.0},
			// u2: cost 从 4 降到 2 → -50%
			{UserID: "u2", JobCategory: "产品", ReqCount: 50, TotalTokens: 500, CostUSD: 4.0},
			// u3: cost 从 3 不变 → 0%
			{UserID: "u3", JobCategory: "工程师", ReqCount: 30, TotalTokens: 300, CostUSD: 3.0},
		},
		LevelMeta: report.Scope{Level: "department", ID: "d1", Name: "部门一"},
	}
	rep, err := derive.Derive(makeAggMeta("department", "d1", "部门一"), in, report.DefaultRuleSet())
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	u := rep.Fact.Department

	// 应有 3 条（3 人都在两期都出现）
	if len(u.TopChanged) != 3 {
		t.Fatalf("expected 3 top changed, got %d: %+v", len(u.TopChanged), u.TopChanged)
	}

	// 第一条 |WoWCostPct| 最大（u1 +100% 或 u2 -50%，100>50 → u1 第一）
	first := u.TopChanged[0]
	if first.Idx != 1 {
		t.Errorf("first Idx=%d, want 1", first.Idx)
	}
	// 匿名化：没有 UserName/UserID 字段
	if math.Abs(first.WoWCostPct-100.0) > 1e-6 {
		t.Errorf("first WoWCostPct=%v, want 100", first.WoWCostPct)
	}
	if first.JobCategory != "工程师" {
		t.Errorf("first JobCategory=%q, want 工程师", first.JobCategory)
	}

	// 第二条 |WoWCostPct| 第二（u2 -50%）
	second := u.TopChanged[1]
	if second.Idx != 2 {
		t.Errorf("second Idx=%d, want 2", second.Idx)
	}
	if math.Abs(second.WoWCostPct-(-50.0)) > 1e-6 {
		t.Errorf("second WoWCostPct=%v, want -50", second.WoWCostPct)
	}

	// 第三条 |WoWCostPct|=0（u3 成本不变）
	third := u.TopChanged[2]
	if third.WoWCostPct != 0 {
		t.Errorf("third WoWCostPct=%v, want 0", third.WoWCostPct)
	}
}

func TestTopChanged_Max5(t *testing.T) {
	// 8 个人都在两期存在，应只取前 5
	cur := make([]report.MemberAgg, 8)
	prev := make([]report.MemberAgg, 8)
	for i := range cur {
		uid := "u" + itoa(i)
		cur[i] = report.MemberAgg{UserID: uid, JobCategory: "工程师", ReqCount: 100, TotalTokens: 1000, CostUSD: float64(i+1) * 2}
		prev[i] = report.MemberAgg{UserID: uid, JobCategory: "工程师", ReqCount: 80, TotalTokens: 800, CostUSD: float64(i + 1)}
	}
	in := report.ReportInput{
		MemberAgg:     cur,
		PrevMemberAgg: prev,
		LevelMeta:     report.Scope{Level: "department", ID: "d1", Name: "部门一"},
	}
	rep, err := derive.Derive(makeAggMeta("department", "d1", "部门一"), in, report.DefaultRuleSet())
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	u := rep.Fact.Department
	if len(u.TopChanged) != 5 {
		t.Errorf("expected max 5 top changed, got %d", len(u.TopChanged))
	}
	// Idx 应从 1 连续编号
	for i, tc := range u.TopChanged {
		if tc.Idx != i+1 {
			t.Errorf("TopChanged[%d].Idx=%d, want %d", i, tc.Idx, i+1)
		}
	}
}

func TestTopChanged_OnlyBothPeriods(t *testing.T) {
	// u3 只在本期出现（无上期数据），不应出现在 TopChanged
	in := report.ReportInput{
		MemberAgg: []report.MemberAgg{
			{UserID: "u1", JobCategory: "工程师", ReqCount: 100, TotalTokens: 1000, CostUSD: 10.0},
			{UserID: "u3", JobCategory: "产品", ReqCount: 50, TotalTokens: 500, CostUSD: 5.0},
		},
		PrevMemberAgg: []report.MemberAgg{
			{UserID: "u1", JobCategory: "工程师", ReqCount: 50, TotalTokens: 500, CostUSD: 5.0},
			// u3 在上期不存在
		},
		LevelMeta: report.Scope{Level: "department", ID: "d1", Name: "部门一"},
	}
	rep, err := derive.Derive(makeAggMeta("department", "d1", "部门一"), in, report.DefaultRuleSet())
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	u := rep.Fact.Department
	// 仅 u1 同时出现在两期
	if len(u.TopChanged) != 1 {
		t.Fatalf("expected 1 top changed, got %d: %+v", len(u.TopChanged), u.TopChanged)
	}
	if u.TopChanged[0].WoWCostPct != 100.0 { // (10-5)/5*100 = 100
		t.Errorf("WoWCostPct=%v, want 100", u.TopChanged[0].WoWCostPct)
	}
}

func TestTopChanged_AnomalyRuleEmpty(t *testing.T) {
	// AnomalyRule 应恒为空（derive 层不访问 Anomalies）
	in := report.ReportInput{
		MemberAgg: []report.MemberAgg{
			{UserID: "u1", JobCategory: "工程师", ReqCount: 100, TotalTokens: 1000, CostUSD: 10.0},
		},
		PrevMemberAgg: []report.MemberAgg{
			{UserID: "u1", JobCategory: "工程师", ReqCount: 50, TotalTokens: 500, CostUSD: 5.0},
		},
		LevelMeta: report.Scope{Level: "department", ID: "d1", Name: "部门一"},
	}
	rep, err := derive.Derive(makeAggMeta("department", "d1", "部门一"), in, report.DefaultRuleSet())
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	u := rep.Fact.Department
	for _, tc := range u.TopChanged {
		if tc.AnomalyRule != "" {
			t.Errorf("TopChanged[%d].AnomalyRule=%q, want empty", tc.Idx, tc.AnomalyRule)
		}
	}
}
