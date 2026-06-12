package derive_test

import (
	"strings"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/observability/report"
	"github.com/example/llm-gateway/internal/observability/report/derive"
	"github.com/example/llm-gateway/internal/observability/report/render"
)

func mkCalls() []report.CallEvent {
	// 周末凌晨非工作时段大额成本（A5）＋ 单模型近上限 prompt（A4）＋ 段内大量失败（A3）。
	weekend := time.Date(2026, 8, 8, 2, 30, 0, 0, time.UTC) // 周六
	offModel := time.Date(2026, 8, 7, 23, 55, 0, 0, time.UTC)
	var calls []report.CallEvent
	for i := 0; i < 25; i++ {
		calls = append(calls, report.CallEvent{TS: weekend.Add(time.Duration(i) * time.Minute), Model: "x", KeyType: "personal", UserID: "u1", Status: 200, EstCostUSD: 10, PromptTokens: 100, CompletionTokens: 50})
	}
	// A4：模型 limit=10 万，prompt=9.9 万（近上限），≥2 条
	for i := 0; i < 3; i++ {
		calls = append(calls, report.CallEvent{TS: offModel.Add(time.Duration(i) * time.Minute), Model: "duck", KeyType: "personal", UserID: "u1", Status: 200, EstCostUSD: 1, PromptTokens: 99000, CompletionTokens: 100})
	}
	// A3：段=(k,model)，20+ req，失败 ≥30%，单一 500 占失败 ≥80%
	for i := 0; i < 30; i++ {
		st := 200
		if i%2 == 0 {
			st = 500
		}
		calls = append(calls, report.CallEvent{TS: weekend.Add(time.Duration(i) * time.Second), KeyID: "k", Model: "chat", KeyType: "service", UserID: "svc", Status: st, EstCostUSD: 0.1})
	}
	return calls
}

func TestDeriveAndRender(t *testing.T) {
	rules := report.DefaultRuleSet()
	rules.USDCNYRate = 7.2
	in := report.ReportInput{
		Calls: mkCalls(),
		Metrics: []report.DailyMetric{
			{Date: "2026-08-01", Level: "personal", ScopeID: "u1", UserID: "u1", ReqCount: 100, SuccessCount: 95, PromptTokens: 1000, CompletionTokens: 500, ReasoningTokens: 100, TotalTokens: 1600, EstCostUSD: 5},
			{Date: "2026-08-02", Level: "personal", ScopeID: "u1", UserID: "u1", ReqCount: 120, SuccessCount: 118, PromptTokens: 1200, CompletionTokens: 600, ReasoningTokens: 80, TotalTokens: 1880, EstCostUSD: 6},
		},
		ModelContextBudget: map[string]int64{"duck": 100000, "chat": 32000},
		LevelMeta:          report.Scope{Level: "personal", ID: "u1", Name: "张三"},
	}
	meta := report.Meta{
		Level:    "personal",
		Period:   report.Period{From: "2026-08-01", To: "2026-08-07", Type: "week", Label: "week 2026-08-01~2026-08-07"},
		Scope:    report.Scope{Level: "personal", ID: "u1", Name: "张三"},
		Currency: report.Currency{Base: "USD", ReportCurrency: "CNY", USDCNYRate: 7.2, RateSource: "test", RateAt: time.Now().Format(time.RFC3339)},
		QA:       report.QA{Alignment: []string{"成功=status=200"}, AnomalyEnabled: true},
	}
	rep, err := derive.Derive(meta, in, rules)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if rep.Fact.Personal == nil {
		t.Fatal("expected personal fact")
	}
	// A5 命中、A4 命中（周末非工作成本 > HARD_CAP_A5）
	var hitA5, hitA4 bool
	for _, a := range rep.Judgment.Anomalies {
		if a.Rule == "A5" && a.Hit {
			hitA5 = true
		}
		if a.Rule == "A4" && a.Hit {
			hitA4 = true
		}
	}
	if !hitA5 {
		t.Errorf("expected A5 hit (weekend off-hour cost); got %+v", rep.Judgment.Anomalies)
	}
	if !hitA4 {
		t.Errorf("expected A4 hit (near-limit prompt); got %+v", rep.Judgment.Anomalies)
	}
	md := render.RenderMD(rep)
	if !strings.Contains(md, "用量周报") {
		t.Errorf("MD missing title")
	}
	html := render.RenderHTML(rep)
	if !strings.Contains(html, "用量周报") {
		t.Errorf("HTML missing title")
	}
	if len(rep.Expression.Suggestions) == 0 {
		t.Errorf("expected suggestions")
	}
	// A3 命中（段=(k,chat)：30 req，15 个 500 → failRate 50%≥30%、单一 500 占 100%≥80%、failed=15≥6）
	var hitA3 bool
	for _, a := range rep.Judgment.Anomalies {
		if a.Rule == "A3" && a.Hit {
			hitA3 = true
		}
	}
	if !hitA3 {
		t.Errorf("expected A3 hit; got %+v", rep.Judgment.Anomalies)
	}
}

func findHit(hits []report.AnomalyHit, rule string) *report.AnomalyHit {
	for i := range hits {
		if hits[i].Rule == rule {
			return &hits[i]
		}
	}
	return nil
}

// A1 命中：30 分钟 × 每分钟 100 次同步调用 → 每 10min 窗 1000 次、中位间隔 <10s、连续 ≥15min。
func TestA1ProgramLoadHit(t *testing.T) {
	calls := []report.CallEvent{}
	start := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	for m := 0; m < 30; m++ {
		for i := 0; i < 100; i++ {
			calls = append(calls, report.CallEvent{TS: start.Add(time.Duration(m)*time.Minute + time.Duration(i)*600*time.Millisecond), Model: "a", KeyType: "personal", UserID: "u1", Status: 200, EstCostUSD: 0.001})
		}
	}
	rules := report.DefaultRuleSet()
	rules.USDCNYRate = 7.2
	hits := derive.Anomalies(calls, rules, nil)
	a1 := findHit(hits, "A1")
	if a1 == nil || !a1.Hit {
		t.Fatalf("expected A1 hit; got %+v", hits)
	}
}

// A2 命中（基线分支）：背景低成本垫高中位数 → 单窗 15USD 突刺 > max(¥50, 中位×5)，且不触 HARD_CAP ¥200。
func TestA2CostAnomalyHit(t *testing.T) {
	calls := []report.CallEvent{}
	base := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	for m := 0; m < 30; m++ {
		for i := 0; i < 3; i++ {
			calls = append(calls, report.CallEvent{TS: base.Add(time.Duration(m)*time.Minute + time.Duration(i)*time.Second), Model: "bg", KeyType: "personal", UserID: "u1", Status: 200, EstCostUSD: 0.01})
		}
	}
	spike := base.Add(12 * time.Minute)
	for i := 0; i < 3; i++ {
		calls = append(calls, report.CallEvent{TS: spike.Add(time.Duration(i) * time.Second), Model: "spike", KeyType: "personal", UserID: "u1", Status: 200, EstCostUSD: 5})
	}
	rules := report.DefaultRuleSet()
	rules.USDCNYRate = 7.2
	hits := derive.Anomalies(calls, rules, nil)
	a2 := findHit(hits, "A2")
	if a2 == nil || !a2.Hit {
		t.Fatalf("expected A2 hit; got %+v", hits)
	}
}

// 部门级事实非空：member 与 service 按 key_type 分离；ScopeID 由 aggregateDaily 设 dept_id 后 buildUnitFact 匹配。
func TestUnitDepartmentFact(t *testing.T) {
	rules := report.DefaultRuleSet()
	rules.USDCNYRate = 7.2
	in := report.ReportInput{
		Metrics: []report.DailyMetric{
			{Date: "2026-08-01", Level: "department", ScopeID: "dept1", UserID: "u1", KeyType: "personal", ReqCount: 100, SuccessCount: 95, PromptTokens: 1000, CompletionTokens: 500, TotalTokens: 1500, EstCostUSD: 5},
			{Date: "2026-08-01", Level: "department", ScopeID: "dept1", UserID: "u2", KeyType: "personal", ReqCount: 50, SuccessCount: 48, PromptTokens: 500, CompletionTokens: 200, TotalTokens: 700, EstCostUSD: 2},
			{Date: "2026-08-01", Level: "department", ScopeID: "dept1", UserID: "", KeyType: "service", ReqCount: 80, SuccessCount: 80, TotalTokens: 3000, EstCostUSD: 8},
		},
		LevelMeta: report.Scope{Level: "department", ID: "dept1", Name: "研发部", DeptID: "dept1"},
	}
	meta := report.Meta{Level: "department", Period: report.Period{From: "2026-08-01", To: "2026-08-01", Type: "week", Label: "w"}, Scope: report.Scope{Level: "department", ID: "dept1", Name: "研发部"}, QA: report.QA{AnomalyEnabled: true}}
	rep, err := derive.Derive(meta, in, rules)
	if err != nil {
		t.Fatalf("derive %v", err)
	}
	d := rep.Fact.Department
	if d == nil {
		t.Fatal("expected department fact")
	}
	if d.Member.ReqCount != 150 {
		t.Fatalf("member req=%d want 150", d.Member.ReqCount)
	}
	if d.ServiceKey.ReqCount != 80 {
		t.Fatalf("service req=%d want 80", d.ServiceKey.ReqCount)
	}
	if d.Member.UserCount != 2 {
		t.Fatalf("user count=%d want 2", d.Member.UserCount)
	}
}

// 28 天基线物化：提供 BaselineMetrics → supported，CurrentWindow 反映「物化」。
func TestBaselineMaterialized(t *testing.T) {
	rules := report.DefaultRuleSet()
	in := report.ReportInput{
		Metrics: []report.DailyMetric{{Date: "2026-08-01", ScopeID: "u1", ReqCount: 100}},
		BaselineMetrics: []report.DailyMetric{
			{Date: "2026-07-01", ScopeID: "u1", ReqCount: 80},
			{Date: "2026-07-02", ScopeID: "u1", ReqCount: 90},
			{Date: "2026-07-03", ScopeID: "u1", ReqCount: 70},
			{Date: "2026-07-04", ScopeID: "u1", ReqCount: 85},
		},
		LevelMeta: report.Scope{Level: "personal", ID: "u1"},
	}
	meta := report.Meta{Level: "personal", Scope: report.Scope{Level: "personal", ID: "u1"}, QA: report.QA{AnomalyEnabled: true}}
	rep, err := derive.Derive(meta, in, rules)
	if err != nil {
		t.Fatalf("derive %v", err)
	}
	if !rep.Judgment.Baseline.Supported {
		t.Fatalf("baseline unsupported: %s", rep.Judgment.Baseline.Note)
	}
	if !strings.Contains(rep.Judgment.Baseline.CurrentWindow, "物化") {
		t.Fatalf("baseline window not materialized: %s", rep.Judgment.Baseline.CurrentWindow)
	}
}

// 28 天基线降级：无 BaselineMetrics → CurrentWindow 注明「近似」，避免误读为真实历史基线。
func TestBaselineDegraded(t *testing.T) {
	rules := report.DefaultRuleSet()
	in := report.ReportInput{
		Metrics:   []report.DailyMetric{{Date: "2026-08-01", ScopeID: "u1", ReqCount: 100}, {Date: "2026-08-02", ScopeID: "u1", ReqCount: 120}},
		LevelMeta: report.Scope{Level: "personal", ID: "u1"},
	}
	meta := report.Meta{Level: "personal", Scope: report.Scope{Level: "personal", ID: "u1"}, QA: report.QA{AnomalyEnabled: true}}
	rep, err := derive.Derive(meta, in, rules)
	if err != nil {
		t.Fatalf("derive %v", err)
	}
	if !strings.Contains(rep.Judgment.Baseline.CurrentWindow, "近似") {
		t.Fatalf("expected degraded window, got %s", rep.Judgment.Baseline.CurrentWindow)
	}
}
