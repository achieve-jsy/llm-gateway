package render

// render_vicky_test.go —— 锁定 Vicky 规范渲染的门禁红线与关键结构，防止回归。

import (
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/observability/report"
)

func TestRenderVickyGateCompliance(t *testing.T) {
	r := vickyTestReport()
	html := RenderVicky(r)

	must := []string{`<section class="reveal">`, `<div class="wrap">`, `<p class="section-label">`, `<h2>`,
		`<table class="data-table">`, `<blockquote>`, `<div class="metrics">`}
	for _, s := range must {
		if !strings.Contains(html, s) {
			// stats-row 不一定每个报告都有 metrics 块；只校验通用结构
			if s == `<div class="metrics">` && strings.Contains(html, `stats-row`) {
				continue
			}
			t.Errorf("missing required vicky structure %q", s)
		}
	}

	// 门禁红线：禁止裸 <table> 与弃用类名。
	for _, bad := range []string{"<table>", "class=\"ladder-", "class=\"quote-block", "class=\"concern-box", "class=\"phase"} {
		if strings.Contains(html, bad) {
			t.Errorf("gate violation: found forbidden %q", bad)
		}
	}
	if !strings.Contains(html, `class="data-table"`) {
		t.Errorf("expected a data-table")
	}
}

func TestRenderVickyCleanNumbers(t *testing.T) {
	r := vickyTestReport()
	// 表达层幻觉校验：含裸数字的建议应被丢弃，不能进建议列表。
	r.Expression.Suggestions = []string{"为 gpt-4o-mini 设阈值（裸数字，应被丢弃）", "为高成本模型设置告警阈值"}
	html := RenderVicky(r)
	if strings.Contains(html, "为 gpt-4o-mini 设阈值") {
		t.Errorf("expression with bare digit should be dropped by hallucination guard")
	}
	if !strings.Contains(html, "为高成本模型设置告警阈值") {
		t.Errorf("clean suggestion should survive")
	}
}

func vickyTestReport() *report.Report {
	return &report.Report{
		SchemaVersion: report.SchemaVersion,
		Meta: report.Meta{
			Level:    "personal",
			Period:   report.Period{From: "2026-08-01", To: "2026-08-07", Type: "week", Days: 7, Label: "周报"},
			Scope:    report.Scope{Level: "personal", ID: "u_1", Name: "test"},
			Currency: report.Currency{Base: "USD", ReportCurrency: "CNY", USDCNYRate: 7.2},
			QA:       report.QA{Alignment: []string{"a"}, AnomalyEnabled: true},
		},
		Fact: report.Fact{
			Personal: &report.PersonalFact{ScopeID: "u_1", ScopeName: "test", ReqCount: 10, SuccessCount: 9, ErrorCount: 1, TotalTokens: 1000, EstCostUSD: 1.2, ActiveDays: 3, ModelCount: 2,
				TopModels: []report.TopModel{{Model: "m1", Calls: 5, TotalTokens: 500, CostUSD: 0.6}}},
		},
		Judgment: report.Judgment{
			Baseline: report.Baseline{Supported: true, Current: 1, Median: 0.9, MAD: 0.1, SigmaRob: 0.15, DevSigma: 0.7, Percentile: 0.5, Trend: "flat", CurrentWindow: "近似"},
			Peer:     report.Peer{Supported: true, Current: 0.12, PercentileRank: 0.6, Band: "中", P25: 0.1, P50: 0.12, P75: 0.15, P90: 0.18, PoolType: "同岗位", VsPeer: "接近中位", Note: "匿名"},
			Anomalies: []report.AnomalyHit{{Rule: "A2", Name: "成本燃烧", Severity: "P1", Hit: true, ActualValue: 1, ThresholdValue: 0.5, BaselineValue: 0.9,
				Evidence: []report.Evidence{{Field: "f", Value: "1", Ref: "fact"}}, Note: "note"}},
			NotComputed: []report.NotComputed{{Metric: "x", Reason: "sample<5"}},
		},
		Expression: report.Expression{Headline: "结论", Insights: []string{"insight"}, Suggestions: []string{"建议一"},
			Tone: "客观", Lang: "zh", Audience: "员工"},
	}
}
