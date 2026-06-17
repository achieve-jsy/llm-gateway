package derive_test

import (
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/observability/report"
	"github.com/example/llm-gateway/internal/observability/report/derive"
)

// TestDeriveUnitFact_ServiceSplit 验证明细路径 buildUnitFact 的 member/service 拆分：
// Metrics 中 KeyType=="service" 的行进 ServiceKey 块，其余进 Member 块（成员口径）。
func TestDeriveUnitFact_ServiceSplit(t *testing.T) {
	rules := report.DefaultRuleSet()
	rules.USDCNYRate = 7.2
	in := report.ReportInput{
		Metrics: []report.DailyMetric{
			// 成员(member)行：2 用户
			{Date: "2026-08-01", Level: "department", ScopeID: "d1", UserID: "u1", KeyType: "personal", Model: "m1", ReqCount: 100, SuccessCount: 95, ErrorCount: 5, PromptTokens: 1000, CompletionTokens: 500, TotalTokens: 1500, EstCostUSD: 5},
			{Date: "2026-08-01", Level: "department", ScopeID: "d1", UserID: "u2", KeyType: "personal", Model: "m2", ReqCount: 50, SuccessCount: 48, ErrorCount: 2, PromptTokens: 500, CompletionTokens: 200, TotalTokens: 700, EstCostUSD: 2},
			// 程序(service)行：归属当前部门
			{Date: "2026-08-01", Level: "department", ScopeID: "d1", UserID: "svc", KeyType: "service", Model: "m3", ReqCount: 200, SuccessCount: 198, ErrorCount: 2, PromptTokens: 2000, CompletionTokens: 800, TotalTokens: 2800, EstCostUSD: 10},
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
		t.Fatal("expected department fact")
	}
	// Member 块 = 2 个 personal 行累加
	if u.Member.ReqCount != 150 || u.Member.SuccessCount != 143 || u.Member.ErrorCount != 7 {
		t.Errorf("Member 加法错: req=%d succ=%d err=%d, want 150/143/7", u.Member.ReqCount, u.Member.SuccessCount, u.Member.ErrorCount)
	}
	if u.Member.TotalTokens != 2200 || u.Member.EstCostUSD != 7 {
		t.Errorf("Member token/cost 错: total=%d cost=%v, want 2200/7", u.Member.TotalTokens, u.Member.EstCostUSD)
	}
	if u.Member.ActiveUserCount != 2 {
		t.Errorf("ActiveUserCount=%d, want 2", u.Member.ActiveUserCount)
	}
	if u.Member.ModelCount != 2 {
		t.Errorf("Member.ModelCount=%d, want 2 (m1,m2)", u.Member.ModelCount)
	}
	// ServiceKey 块 = m3 行
	if u.ServiceKey.ReqCount != 200 || u.ServiceKey.SuccessCount != 198 || u.ServiceKey.ErrorCount != 2 {
		t.Errorf("ServiceKey 加法错: req=%d succ=%d err=%d, want 200/198/2", u.ServiceKey.ReqCount, u.ServiceKey.SuccessCount, u.ServiceKey.ErrorCount)
	}
	if u.ServiceKey.TotalTokens != 2800 || u.ServiceKey.EstCostUSD != 10 {
		t.Errorf("ServiceKey token/cost 错: total=%d cost=%v, want 2800/10", u.ServiceKey.TotalTokens, u.ServiceKey.EstCostUSD)
	}
	// 人均 = 总量 / 活跃用户数
	if u.Member.PerCapita.ReqCount != 75 { // 150/2
		t.Errorf("PerCapita.ReqCount=%d, want 75", u.Member.PerCapita.ReqCount)
	}
}
