package handler

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMedianOf(t *testing.T) {
	if got := medianOf(nil); got != 0 {
		t.Fatalf("medianOf(nil) = %v, want 0", got)
	}
	if got := medianOf([]float64{5}); got != 5 {
		t.Fatalf("medianOf([5]) = %v, want 5", got)
	}
	if got := medianOf([]float64{3, 1, 2}); got != 2 {
		t.Fatalf("medianOf([3,1,2]) = %v, want 2", got)
	}
	// 1,2,3,4 → (2+3)/2
	if got := medianOf([]float64{4, 1, 3, 2}); got != 2.5 {
		t.Fatalf("medianOf([4,1,3,2]) = %v, want 2.5", got)
	}
}

// TestBuildInsight_CompanyDeterministic 用确定性数据验证 company 视角三段式：
// gmed、环比主因（按 |Δcost| 而非费用头名）、三段非空、估算标 est=true。
func TestBuildInsight_CompanyDeterministic(t *testing.T) {
	m := insightMetrics{
		ScopeKind:    "company",
		ScopeLabel:   "全司",
		ScopeDisplay: "全司",
		RangeLabel:   "07-28~07-30",
		Days:         3,
		Budget:       4200,
		BudgetConfigured: true,
		Cost:         2000,
		PrevCost:     1300,
		HighCost:     500,
		InTokens:     100000,
		CachedTokens: 20000,
		Gmed:         250,
		// 费用头名是 model-a，但 |Δ| 最大是 model-b（+600 > +200）→ 主因必须选 model-b
		Models: []insightModel{
			{Model: "model-a", Tier: "L2-Advanced", Cost: 1100, PrevCost: 900},
			{Model: "model-b", Tier: "L3-Premium", Cost: 900, PrevCost: 300},
		},
		Depts: []insightDept{
			{Dept: "研发部", UserCount: 2, Cost: 1500},
			{Dept: "运营部", UserCount: 4, Cost: 500},
		},
		Users: []insightUser{
			{UserID: "u1", UserName: "张三", DeptName: "研发部", Cost: 1200},
			{UserID: "u2", UserName: "李四", DeptName: "研发部", Cost: 300},
			{UserID: "u3", UserName: "王五", DeptName: "运营部", Cost: 400},
			{UserID: "u4", UserName: "赵六", DeptName: "运营部", Cost: 100},
		},
		TopUser:         insightUser{UserID: "u1", UserName: "张三", DeptName: "研发部", Cost: 1200},
		TopUserModels:   []insightUserModel{{Model: "model-b", Tier: "L3-Premium", Cost: 800}},
		TopUserPeakHour: 15,
		TopUserPeakCost: 210.5,
	}

	resp := buildInsight(m)

	// gmed 原样透传
	if resp.Gmed != 250 {
		t.Fatalf("Gmed = %v, want 250", resp.Gmed)
	}

	// 指标与文本一致的确定性值
	if got := resp.Metrics.DeltaPct; got < 53.8 || got > 53.9 {
		t.Fatalf("DeltaPct = %v, want ~53.8", got)
	}
	if got := resp.Metrics.HighTierPct; got != 25 {
		t.Fatalf("HighTierPct = %v, want 25", got)
	}

	// 环比主因 = |Δcost| 最大的模型（model-b，即使其费用不是头名）
	if len(resp.Segments.Causes) == 0 {
		t.Fatal("causes should not be empty")
	}
	firstCause := resp.Segments.Causes[0].Text
	if !strings.Contains(firstCause, "model-b") {
		t.Fatalf("first cause should pick model-b (max |Δcost|), got: %s", firstCause)
	}
	if strings.Contains(firstCause, "model-a") {
		t.Fatalf("first cause should not mention model-a: %s", firstCause)
	}
	// 下钻锚点应内嵌 data-* 属性（前端组件契约）
	if !strings.Contains(firstCause, `<a class="ai-drill" data-model="model-b" data-scroll="overview">下钻</a>`) {
		t.Fatalf("first cause should embed drill anchor, got: %s", firstCause)
	}
	if !strings.Contains(resp.Segments.Facts[2].Text, `data-dept="研发部"`) {
		t.Fatalf("company per-capita fact should embed data-dept anchor: %s", resp.Segments.Facts[2].Text)
	}

	// 三段均非空
	if len(resp.Segments.Facts) == 0 {
		t.Fatal("facts should not be empty")
	}
	if len(resp.Segments.Causes) == 0 {
		t.Fatal("causes should not be empty")
	}
	if len(resp.Segments.Suggestions) == 0 {
		t.Fatal("suggestions should not be empty")
	}

	// 估算建议：头部用户 top 模型为 L3-Premium → est=true 且带 $240.00
	estFound := false
	for _, s := range resp.Segments.Suggestions {
		if s.Est != nil && *s.Est {
			estFound = true
			if !strings.Contains(s.Text, "$240.00") {
				t.Fatalf("est suggestion should anchor $240.00, got: %s", s.Text)
			}
			if s.Drill == nil || s.Drill.Dept != "研发部" {
				t.Fatalf("est suggestion drill should point to 研发部, got: %+v", s.Drill)
			}
		}
	}
	if !estFound {
		t.Fatal("expected an est=true suggestion segment")
	}

	// JSON 序列化形状
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m2 map[string]interface{}
	if err := json.Unmarshal(b, &m2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"scope", "gmed", "metrics", "segments"} {
		if _, ok := m2[key]; !ok {
			t.Fatalf("json missing top-level key %q", key)
		}
	}
	t.Logf("insight json:\n%s", prettyJSON(resp))
}

// TestBuildInsight_DeptScope 验证 dept 视角下不引用公司费用大头、三段仍可生成。
func TestBuildInsight_DeptScope(t *testing.T) {
	m := insightMetrics{
		ScopeKind:    "dept",
		ScopeKey:     "研发部",
		ScopeLabel:   "部门：研发部",
		ScopeDisplay: "研发部",
		RangeLabel:   "07-28~07-30",
		Days:         3,
		Budget:       4200,
		Cost:         1500,
		PrevCost:     1200,
		HighCost:     600,
		InTokens:     80000,
		CachedTokens: 40000,
		Gmed:         250,
		Models: []insightModel{
			{Model: "model-b", Tier: "L3-Premium", Cost: 900, PrevCost: 500},
			{Model: "model-a", Tier: "L2-Advanced", Cost: 600, PrevCost: 700},
		},
		Depts: []insightDept{
			{Dept: "研发部", UserCount: 2, Cost: 1500},
		},
		Users: []insightUser{
			{UserID: "u1", UserName: "张三", DeptName: "研发部", Cost: 1200},
			{UserID: "u2", UserName: "李四", DeptName: "研发部", Cost: 300},
		},
		TopUser:         insightUser{UserID: "u1", UserName: "张三", DeptName: "研发部", Cost: 1200},
		TopUserModels:   []insightUserModel{{Model: "model-b", Tier: "L3-Premium", Cost: 800}},
		TopUserPeakHour: 10,
		TopUserPeakCost: 99.9,
	}

	resp := buildInsight(m)
	if resp.Scope.Kind != "dept" || resp.Scope.Label != "部门：研发部" {
		t.Fatalf("unexpected scope: %+v", resp.Scope)
	}
	if len(resp.Segments.Facts) == 0 || len(resp.Segments.Causes) == 0 || len(resp.Segments.Suggestions) == 0 {
		t.Fatal("dept scope should produce non-empty three sections")
	}
	// 部门视角首条事实应含部门名而非全司表述
	if !strings.Contains(resp.Segments.Facts[0].Text, "研发部") {
		t.Fatalf("dept first fact should mention dept name: %s", resp.Segments.Facts[0].Text)
	}
}

// TestBuildInsight_EmptyRange 空范围应给出无数据提示而非 panic。
func TestBuildInsight_EmptyRange(t *testing.T) {
	resp := buildInsight(insightMetrics{
		ScopeKind: "company", ScopeLabel: "全司", ScopeDisplay: "全司",
		Days: 3, Budget: 4200,
	})
	if len(resp.Segments.Facts) != 1 {
		t.Fatalf("empty range should emit one no-data fact, got %d", len(resp.Segments.Facts))
	}
	if !strings.Contains(resp.Segments.Facts[0].Text, "无数据") {
		t.Fatalf("no-data text expected: %s", resp.Segments.Facts[0].Text)
	}
	if len(resp.Segments.Causes) != 0 || len(resp.Segments.Suggestions) != 0 {
		t.Fatal("empty range should have no causes/suggestions")
	}
}

// TestBuildInsight_BudgetUnconfigured 未配置预算(company 无预算分配)时:
// budget=0、budget_configured=false、budget_pct=0,事实段文案显示「未配置」。
func TestBuildInsight_BudgetUnconfigured(t *testing.T) {
	m := insightMetrics{
		ScopeKind:    "company",
		ScopeLabel:   "全司",
		ScopeDisplay: "全司",
		RangeLabel:   "07-28~07-30",
		Days:         3,
		Cost:         2000,
		PrevCost:     1300,
		HighCost:     500,
		Gmed:         250,
		Models: []insightModel{
			{Model: "model-a", Tier: "L2-Advanced", Cost: 2000, PrevCost: 1300},
		},
	}

	resp := buildInsight(m)

	if resp.Metrics.Budget != 0 || resp.Metrics.BudgetConfigured || resp.Metrics.BudgetPct != 0 {
		t.Fatalf("unconfigured budget should yield budget=0/configured=false/pct=0, got %+v", resp.Metrics)
	}
	budgetFact := resp.Segments.Facts[0].Text
	if !strings.Contains(budgetFact, "预算 未配置") {
		t.Fatalf("budget fact should say 未配置, got: %s", budgetFact)
	}
}

func prettyJSON(v interface{}) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}
