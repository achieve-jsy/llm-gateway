package render

import (
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/observability/report"
)

func TestRenderMDLevelMatrix(t *testing.T) {
	for _, level := range []string{"personal", "department", "studio"} {
		t.Run(level, func(t *testing.T) {
			r := markdownReport(level)
			md := RenderMD(&r)
			for _, want := range []string{"## 概况", "| 指标 | 值 |", "研发 | 平台", "## 判断", "### 未能判定", "样本不足 < 5"} {
				if !strings.Contains(md, want) {
					t.Errorf("missing %q in markdown:\n%s", want, md)
				}
			}
			if level == "personal" {
				if !strings.Contains(md, "| 调用数 | 0 |") || !strings.Contains(md, "| 成功率 | n/a |") {
					t.Errorf("personal empty state is not explicit:\n%s", md)
				}
			} else {
				for _, want := range []string{"| 成员调用 | 0（花名册 3 人，活跃 0 人） |", "| 成员成功率 | n/a |", "| Service Key 调用 | 2 |"} {
					if !strings.Contains(md, want) {
						t.Errorf("%s unit summary missing %q:\n%s", level, want, md)
					}
				}
			}
		})
	}
}

func markdownReport(level string) report.Report {
	r := report.Report{
		SchemaVersion: report.SchemaVersion,
		Meta: report.Meta{
			Level:    level,
			Period:   report.Period{From: "2026-08-01", To: "2026-08-07", Type: "week", Days: 7, Label: "周 | 特殊"},
			Scope:    report.Scope{Level: level, ID: "scope", Name: "研发 | 平台"},
			Currency: report.Currency{ReportCurrency: "CNY", USDCNYRate: 7.2},
		},
		Judgment: report.Judgment{
			Baseline:    report.Baseline{Metric: "daily_calls", Note: "无历史"},
			Peer:        report.Peer{Note: "不适用"},
			NotComputed: []report.NotComputed{{Metric: "同岗 | 分位", Reason: "样本不足 < 5"}},
		},
		Expression: report.Expression{Headline: "空态 | 可读"},
	}
	if level == "personal" {
		r.Fact.Personal = &report.PersonalFact{}
	} else {
		u := &report.UnitFact{Member: report.UnitMember{UserCount: 3}, ServiceKey: report.UnitService{ReqCount: 2, TotalTokens: 10, EstCostUSD: 0.1}}
		if level == "department" {
			r.Fact.Department = u
		} else {
			r.Fact.Studio = u
		}
	}
	return r
}

func TestRenderHTMLStudioSummary(t *testing.T) {
	r := markdownReport("studio")
	r.Fact.Studio.Member.ReqCount = 7
	r.Fact.Studio.Member.TotalTokens = 99
	html := RenderHTML(&r)
	if !strings.Contains(html, `<div class="v">7</div>`) || !strings.Contains(html, `<div class="v">99</div>`) {
		t.Fatalf("studio digest is empty: %s", html)
	}
}
