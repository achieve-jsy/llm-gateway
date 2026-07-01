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
			// 顶部数字表：个人模板首章为「一、使用概览」，部门/工作室为「概况」。
			overview := "## 概况"
			if level == "personal" {
				overview = "## 一、使用概览"
			}
			for _, want := range []string{overview, "| 指标 | 值 |", "研发 | 平台", "## 判断", "### 未能判定", "样本不足 < 5"} {
				if !strings.Contains(md, want) {
					t.Errorf("missing %q in markdown:\n%s", want, md)
				}
			}
			if level == "personal" {
				// 个人报告改为一行摘要格式，含「调用 0 次」
				if !strings.Contains(md, "调用 0 次") {
					t.Errorf("personal overview line missing: %s", md)
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

// TestRenderMDModelAnalysisSections 验证三种级别在 rep.ModelAnalysis 有 LLM Sections 时，
// 个人周报复用「场景与合理用模」标题、部门/工作室新增「## 模型分析」章节，
// 且 LLM 内容出现、Note 作为降级说明追加。
func TestRenderMDModelAnalysisSections(t *testing.T) {
	sec := "### 用模洞察\n\n存量长文本以 qwen 系列为主，建议按任务类别拆分子步。"
	for _, level := range []string{"personal", "department", "studio"} {
		t.Run(level, func(t *testing.T) {
			r := markdownReport(level)
			r.ModelAnalysis = &report.ModelAnalysis{
				Level:    level,
				Sections: sec,
				Note:     "LLM Key 未配置，使用降级说明。",
			}
			md := RenderMD(&r)
			// 标题：个人复用「场景与合理用模」，部门/工作室用「模型分析」
			wantTitle := "## 模型分析"
			if level == "personal" {
				wantTitle = "## 五、场景与合理用模"
			}
			if !strings.Contains(md, wantTitle) {
				t.Errorf("%s missing model-analysis title %q:\n%s", level, wantTitle, md)
			}
			if !strings.Contains(md, "### 用模洞察") || !strings.Contains(md, "存量长文本以 qwen 系列为主") {
				t.Errorf("%s missing LLM sections content:\n%s", level, md)
			}
			// Note 追加为降级说明
			if !strings.Contains(md, "（注：LLM Key 未配置，使用降级说明。）") {
				t.Errorf("%s missing note line:\n%s", level, md)
			}
			// 章节位置：模型分析在「## 判断」之前
			idxTitle := strings.Index(md, wantTitle)
			idxJudgment := strings.Index(md, "## 判断")
			if idxTitle == -1 || idxJudgment == -1 || idxTitle > idxJudgment {
				t.Errorf("%s model-analysis section should precede ## 判断: title=%d judgment=%d\n%s", level, idxTitle, idxJudgment, md)
			}
		})
	}
}

// TestRenderMDModelAnalysisNil 验证 rep.ModelAnalysis==nil 时不发章节（行为与现状一致）。
func TestRenderMDModelAnalysisNil(t *testing.T) {
	for _, level := range []string{"personal", "department", "studio"} {
		t.Run(level, func(t *testing.T) {
			r := markdownReport(level)
			md := RenderMD(&r)
			if strings.Contains(md, "## 模型分析") {
				t.Errorf("%s should not emit ## 模型分析 when ModelAnalysis==nil:\n%s", level, md)
			}
			if level == "personal" && strings.Contains(md, "## 五、场景与合理用模") {
				t.Errorf("personal should not emit ## 场景与合理用模 when ModelAnalysis==nil and no TopModels:\n%s", md)
			}
		})
	}
}

// TestRenderMDModelAnalysisNoteOnly 验证 LLM 失败（仅 Note、Sections 空）时的降级表现。
func TestRenderMDModelAnalysisNoteOnly(t *testing.T) {
	for _, level := range []string{"personal", "department", "studio"} {
		t.Run(level, func(t *testing.T) {
			r := markdownReport(level)
			r.ModelAnalysis = &report.ModelAnalysis{Level: level, Note: "未配置 LLM Key，跳过模型分析。"}
			md := RenderMD(&r)
			wantTitle := "## 模型分析"
			if level == "personal" {
				wantTitle = "## 五、场景与合理用模"
			}
			if !strings.Contains(md, wantTitle) {
				t.Errorf("%s missing title %q with note-only:\n%s", level, wantTitle, md)
			}
			if !strings.Contains(md, "（注：未配置 LLM Key，跳过模型分析。）") {
				t.Errorf("%s missing degradation note:\n%s", level, md)
			}
		})
	}
}

// TestRenderMDModelAnalysisPersonalDeterministicPlusLLM 验证个人周报在确定性数据与 LLM 内容并存时，
// 确定性降级行与 LLM 增强都出现在「场景与合理用模」标题下。
func TestRenderMDModelAnalysisPersonalDeterministicPlusLLM(t *testing.T) {
	r := markdownReport("personal")
	r.Fact.Personal.ReqCount = 100
	r.Fact.Personal.ModelCount = 2
	r.Fact.Personal.TopModels = []report.TopModel{{Model: "qwen-max", Calls: 60, CostUSD: 1.2}}
	r.ModelAnalysis = &report.ModelAnalysis{Level: "personal", Sections: "### 用模洞察\n\n部门内 qwen 采用集中。"}
	md := RenderMD(&r)
	if !strings.Contains(md, "## 五、场景与合理用模") {
		t.Fatalf("missing ## 场景与合理用模:\n%s", md)
	}
	if !strings.Contains(md, "主用模型：`qwen-max`（占调用 60.0%，成本 `¥8.64`）。") {
		t.Errorf("missing deterministic top-model line:\n%s", md)
	}
	if !strings.Contains(md, "### 用模洞察") {
		t.Errorf("missing LLM sections:\n%s", md)
	}
	if strings.Count(md, "## 五、场景与合理用模") != 1 {
		t.Errorf("## 场景与合理用模 should appear exactly once:\n%s", md)
	}
}

// TestRenderMDDeptTimingPeerAndServiceKeyTable 验证部门周报概览输出耗时行、匿名横比降级行，
// 且第三章在 u.ServiceKey.Keys 非空时渲染逐 Key 明细表（缺失列以占位并注明，不硬造）。
func TestRenderMDDeptTimingPeerAndServiceKeyTable(t *testing.T) {
	u := &report.UnitFact{
		Member: report.UnitMember{
			UserCount: 5, ActiveUserCount: 4, ReqCount: 100, SuccessCount: 90,
			EstCostUSD: 1.0, TotalTokens: 1000,
			AvgDurationMs: 1500, P95DurationMs: 3000, AvgTTFTMs: 200,
			PerCapita: report.PerCapita{ReqCount: 25, EstCostUSD: 50, SuccessRate: 0.9},
		},
		ServiceKey: report.UnitService{
			ReqCount: 3, TotalTokens: 1000, EstCostUSD: 0.2, ModelCount: 2,
			Keys: []report.ServiceKeyInfo{
				{KeyName: "k1", OwnerName: "app1", ReqCount: 3, TotalTokens: 1000, CostUSD: 0.2, ModelCount: 2},
			},
		},
	}
	r := markdownReport("department")
	r.Fact.Department = u
	r.Judgment.PeerUnitCost = report.Peer{
		Supported: true, Metric: "unit_active_user_cost", PoolType: "全公司部门", SampleN: 12,
		Current: 50, PercentileRank: 0.75, VsPeer: "略高",
	}
	md := RenderMD(&r)
	for _, want := range []string{
		"- 平均耗时 1.5s | p95 3.0s | 平均 TTFT 200ms",
		"- 单位活跃人成本 ¥360.00/人 · vs 同类型部门（匿名）单位活跃人成本 P75（当前 ¥360.00/人 · 池 全公司部门 12 样本 · vs 中位 略高）",
		"| Service Key | Owner/应用 | 调用 | Token | 成本 | 成本占比 | 主用模型 | 异常 |",
		"| k1 | app1 | 3 | 1.0k | ¥1.44 | 16.7% | 2 个模型 | — |",
		"- 注：「主用模型」仅给出去重模型数（t6 逐 Key 明细不含模型名）；「异常」逐 Key 明细暂缺。",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("department markdown missing %q:\n%s", want, md)
		}
	}
}

// TestRenderMDDeptAnonPeerUnitCostDegraded 验证 judge.PeerUnitCost.Supported=false 时，
// 部门概览匿名横比行输出「分位不可用（%s）」降级注明，不硬造分位。
func TestRenderMDDeptAnonPeerUnitCostDegraded(t *testing.T) {
	r := markdownReport("department")
	r.Fact.Department = &report.UnitFact{
		Member: report.UnitMember{
			UserCount: 5, ActiveUserCount: 4, ReqCount: 100, SuccessCount: 90,
			EstCostUSD: 1.0, TotalTokens: 1000,
			PerCapita: report.PerCapita{ReqCount: 25, EstCostUSD: 50, SuccessRate: 0.9},
		},
	}
	// PeerUnitCost 零值：Supported=false，Note 空 → 输出「样本不足」降级
	r.Judgment.PeerUnitCost = report.Peer{Supported: false, Note: ""}
	md := RenderMD(&r)
	if !strings.Contains(md, "- 单位活跃人成本 ¥360.00/人 · vs 同类型部门（匿名）单位活跃人成本：分位不可用（样本不足）") {
		t.Errorf("department markdown missing degraded peer line:\n%s", md)
	}
}
