package derive_test

import (
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/observability/report"
	"github.com/example/llm-gateway/internal/observability/report/derive"
)

// TestDeriveMemberAgg_Personal 验证 `-agg` 模式：ReportInput 仅有 MemberAgg（无 Metrics/Calls）时，
// PersonalFact 由成员聚合构造加法核心；TopModels/耗时分布等明细依赖项降级为空/0。
func TestDeriveMemberAgg_Personal(t *testing.T) {
	rules := report.DefaultRuleSet()
	rules.USDCNYRate = 7.2
	in := report.ReportInput{
		MemberAgg: []report.MemberAgg{
			{UserID: "u1", UserName: "张三", ReqCount: 100, SuccessCount: 95, ErrorCount: 5,
				PromptTokens: 1000, CachedTokens: 200, CompletionTokens: 500, ReasoningTokens: 100,
				TotalTokens: 1600, CostUSD: 5, ActiveDays: 6, ModelCount: 3},
		},
		LevelMeta: report.Scope{Level: "personal", ID: "u1", Name: "张三"},
	}
	meta := report.Meta{
		Level:    "personal",
		Period:   report.Period{From: "2026-08-01", To: "2026-08-07", Type: "week", Label: "week 2026-08-01~2026-08-07"},
		Scope:    report.Scope{Level: "personal", ID: "u1", Name: "张三"},
		Currency: report.Currency{Base: "USD", ReportCurrency: "CNY", USDCNYRate: 7.2, RateSource: "test", RateAt: time.Now().Format(time.RFC3339)},
		QA:       report.QA{Alignment: []string{""}, AnomalyEnabled: false},
	}
	rep, err := derive.Derive(meta, in, rules)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	f := rep.Fact.Personal
	if f == nil {
		t.Fatal("expected personal fact from agg")
	}
	if f.ReqCount != 100 || f.SuccessCount != 95 || f.ErrorCount != 5 {
		t.Errorf("加法核心错: req=%d succ=%d err=%d, want 100/95/5", f.ReqCount, f.SuccessCount, f.ErrorCount)
	}
	if f.TotalTokens != 1600 {
		t.Errorf("total_tokens=%d, want 1600 (不含 cached)", f.TotalTokens)
	}
	if f.CachedTokens != 200 {
		t.Errorf("cached=%d, want 200", f.CachedTokens)
	}
	if f.EstCostUSD != 5 {
		t.Errorf("cost=%v, want 5", f.EstCostUSD)
	}
	if f.ModelCount != 3 || f.ActiveDays != 6 {
		t.Errorf("model_count=%d active_days=%d, want 3/6", f.ModelCount, f.ActiveDays)
	}
	// 明细依赖项降级：无 TopModels、无耗时分布、CacheWrite=0。
	if len(f.TopModels) != 0 {
		t.Errorf("agg 模式不应有 TopModels, got %d", len(f.TopModels))
	}
	if len(f.HourlyDistribution) != 0 {
		t.Errorf("agg 模式不应有 HourlyDistribution, got %d", len(f.HourlyDistribution))
	}
	if f.AvgDurationMs != 0 {
		t.Errorf("agg 模式 AvgDurationMs 应为 0, got %v", f.AvgDurationMs)
	}
	if f.CacheWriteTokens != 0 {
		t.Errorf("agg 模式 CacheWriteTokens 应为 0, got %d", f.CacheWriteTokens)
	}
}

// TestDeriveMemberAgg_Department 验证 `-agg` 模式单元级：UnitFact.Member 由成员聚合逐行累加，
// ServiceKey(程序)块降级为空并注记、TopModels 空（无模型粒度）。
func TestDeriveMemberAgg_Department(t *testing.T) {
	rules := report.DefaultRuleSet()
	rules.USDCNYRate = 7.2
	in := report.ReportInput{
		MemberAgg: []report.MemberAgg{
			{UserID: "u1", UserName: "张三", DeptID: "d1", ReqCount: 100, SuccessCount: 95, ErrorCount: 5,
				PromptTokens: 1000, CachedTokens: 200, CompletionTokens: 500, ReasoningTokens: 100,
				TotalTokens: 1600, CostUSD: 5, ActiveDays: 6, ModelCount: 3},
			{UserID: "u2", UserName: "李四", DeptID: "d1", ReqCount: 50, SuccessCount: 48, ErrorCount: 2,
				PromptTokens: 500, CompletionTokens: 200, ReasoningTokens: 0,
				TotalTokens: 700, CostUSD: 2, ActiveDays: 4, ModelCount: 1},
		},
		LevelMeta: report.Scope{Level: "department", ID: "d1", Name: "部门一", DeptID: "d1"},
	}
	meta := report.Meta{
		Level:    "department",
		Period:   report.Period{From: "2026-08-01", To: "2026-08-07", Type: "week", Label: "week 2026-08-01~2026-08-07"},
		Scope:    report.Scope{Level: "department", ID: "d1", Name: "部门一", DeptID: "d1"},
		Currency: report.Currency{Base: "USD", ReportCurrency: "CNY", USDCNYRate: 7.2, RateSource: "test", RateAt: time.Now().Format(time.RFC3339)},
		QA:       report.QA{Alignment: []string{""}, AnomalyEnabled: false},
	}
	rep, err := derive.Derive(meta, in, rules)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	u := rep.Fact.Department
	if u == nil {
		t.Fatal("expected department fact from agg")
	}
	if u.Member.ReqCount != 150 || u.Member.SuccessCount != 143 || u.Member.ErrorCount != 7 {
		t.Errorf("Member 加法错: req=%d succ=%d err=%d, want 150/143/7", u.Member.ReqCount, u.Member.SuccessCount, u.Member.ErrorCount)
	}
	if u.Member.TotalTokens != 2300 {
		t.Errorf("Member total_tokens=%d, want 2300", u.Member.TotalTokens)
	}
	if u.Member.EstCostUSD != 7 {
		t.Errorf("Member cost=%v, want 7", u.Member.EstCostUSD)
	}
	// 降级项：ServiceKey 块不可得（聚合恒排除 service）、TopModels/ModelCount 空。
	if u.ServiceKey.ReqCount != 0 {
		t.Errorf("agg 模式 ServiceKey 应降级为空, got req=%d", u.ServiceKey.ReqCount)
	}
	if u.Member.ModelCount != 0 {
		t.Errorf("agg 模式 Member.ModelCount 应为 0, got %d", u.Member.ModelCount)
	}
}
