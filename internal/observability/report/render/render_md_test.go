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
		"| Service Key | Owner/应用 | 调用 | 调用环比 | Token | 成本 | 成本环比 | 成本占比 | 主用模型 | 异常 |",
		"| k1 | app1 | 3 | — | 1.0k | ¥1.44 | — | 16.7% | — | — |",
		"- 注：「主用模型」为当期成本最高的模型名；「环比」为对比前一周同范围（无上期显示「—」）；「异常」为当期失败调用数（0 显示「—」）。",
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

// TestRenderMDDeptTopChangedAnomalyCause 验证部门第四章「重点变化」表：列序=人员（匿名）|岗位|成本环比|主因|是否有解释，
// 移除「主用模型」列；主因列与是否有解释列均读 AnomalyCause（有值→主因显示命中规则、是否有解释=已解释；空→均「待确认」）。
func TestRenderMDDeptTopChangedAnomalyCause(t *testing.T) {
	r := markdownReport("department")
	r.Fact.Department.TopChanged = []report.TopChangedMember{
		{Idx: 1, JobCategory: "工程师", WoWCostPct: 120.0, AnomalyCause: "A2 成本燃烧"},
		{Idx: 2, JobCategory: "设计", WoWCostPct: -30.0, AnomalyCause: ""},
	}
	md := RenderMD(&r)
	for _, want := range []string{
		"| 人员（匿名） | 岗位 | 成本环比 | 主因 | 是否有解释 |",
		"| 员工1 | 工程师 | +120.0% | A2 成本燃烧 | 已解释 |",
		"| 员工2 | 设计 | -30.0% | 待确认 | 待确认 |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("department TopChanged section missing %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "主用模型 | 是否有解释") {
		t.Errorf("第四章仍含「主用模型」列，应按模板移除:\n%s", md)
	}
}

// TestRenderMDDeptServiceKeyWoWAndTopModel 验证部门第三章 ServiceKey 逐 Key 表：
// 新增「调用环比/成本环比」列（读 ServiceKeyInfo.PrevReqCount/PrevCostUSD；无上期显示「—」），
// 「主用模型」列读 ServiceKeyInfo.TopModel（模型名，非去重计数）；汇总行带主用模型与成本环比。
func TestRenderMDDeptServiceKeyWoWAndTopModel(t *testing.T) {
	u := &report.UnitFact{
		Member: report.UnitMember{EstCostUSD: 10.0, ReqCount: 100},
		ServiceKey: report.UnitService{
			ReqCount: 200, TotalTokens: 10000, EstCostUSD: 20.0, TopModel: "gpt-4o",
			PrevReqCount: 100, PrevTokens: 5000, PrevCostUSD: 10.0,
			Keys: []report.ServiceKeyInfo{
				{KeyName: "k1", OwnerName: "app1", ReqCount: 200, TotalTokens: 10000, CostUSD: 20.0,
					SuccessCount: 195, ErrorCount: 5, TopModel: "gpt-4o",
					PrevReqCount: 100, PrevTokens: 5000, PrevCostUSD: 10.0},
			},
		},
	}
	r := markdownReport("department")
	r.Fact.Department = u
	md := RenderMD(&r)
	for _, want := range []string{
		"主用模型 gpt-4o；成本环比 +100.0%",
		"| k1 | app1 | 200 | +100.0% | 10.0k | ¥144.00 | +100.0% | 66.7% | gpt-4o | 5 |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("department ServiceKey WoW/TopModel missing %q:\n%s", want, md)
		}
	}
}

// TestRenderMDDeptServiceKeyConcentration 验证部门第三章 ServiceKey 块补「TopN 成本集中度」：
// 多 Key 时渲染 derive 的 ConcentrationText（如「活跃 Key 6 个，Top 2 成本占比 78%」）；为空（单 Key/不足 2）时降级不输出。
func TestRenderMDDeptServiceKeyConcentration(t *testing.T) {
	u := &report.UnitFact{
		Member: report.UnitMember{EstCostUSD: 10.0, ReqCount: 100},
		ServiceKey: report.UnitService{
			ReqCount: 200, TotalTokens: 10000, EstCostUSD: 20.0, TopModel: "gpt-4o",
			ActiveKeyCount: 6, TopN: 2, TopNCostRatio: 0.78,
			ConcentrationText: "活跃 Key 6 个，Top 2 成本占比 78%",
			Keys: []report.ServiceKeyInfo{
				{KeyName: "k1", OwnerName: "app1", ReqCount: 200, TotalTokens: 10000, CostUSD: 20.0, TopModel: "gpt-4o"},
				{KeyName: "k2", OwnerName: "app2", ReqCount: 100, TotalTokens: 5000, CostUSD: 10.0, TopModel: "gpt-4o"},
			},
		},
	}
	r := markdownReport("department")
	r.Fact.Department = u
	md := RenderMD(&r)
	if !strings.Contains(md, "- 成本集中度：活跃 Key 6 个，Top 2 成本占比 78%") {
		t.Errorf("部门 ServiceKey 块缺少成本集中度行:\n%s", md)
	}
	// 为空（单 Key 或不足 2）时不输出集中度行
	r2 := markdownReport("department")
	r2.Fact.Department = &report.UnitFact{
		Member: report.UnitMember{EstCostUSD: 10.0, ReqCount: 100},
		ServiceKey: report.UnitService{
			ReqCount: 200, TotalTokens: 10000, EstCostUSD: 20.0, TopModel: "gpt-4o",
			ConcentrationText: "",
			Keys: []report.ServiceKeyInfo{
				{KeyName: "k1", OwnerName: "app1", ReqCount: 200, TotalTokens: 10000, CostUSD: 20.0, TopModel: "gpt-4o"},
			},
		},
	}
	if md2 := RenderMD(&r2); strings.Contains(md2, "成本集中度：") {
		t.Errorf("ConcentrationText 为空时不应输出成本集中度行:\n%s", md2)
	}
}

// TestRenderMDStudioBudgetExec 验证工作室「一页摘要」预算执行行：Supported 时输出「预算执行：X%（实际/额度）」，
// Supported=false（不可得）时输出「预算执行：不可得（note）」降级，不硬造数值。
func TestRenderMDStudioBudgetExec(t *testing.T) {
	r := markdownReport("studio")
	r.Fact.Studio.BudgetExec = &report.BudgetExec{
		Supported: true, ScopeType: "studio", ScopeID: "s1", Month: "2026-08",
		MonthLimitUSD: 1000, ActualCostUSD: 450, Percent: 45.0,
	}
	md := RenderMD(&r)
	if !strings.Contains(md, "- 预算执行：实际 450.0000 USD / 限额 1000.0000 USD（45.0%）") {
		t.Errorf("studio budget exec line missing:\n%s", md)
	}

	r2 := markdownReport("studio")
	r2.Fact.Studio.BudgetExec = &report.BudgetExec{Supported: false, Note: "无预算执行数据"}
	md2 := RenderMD(&r2)
	if !strings.Contains(md2, "- 预算执行：不可得（无预算执行数据）") {
		t.Errorf("studio budget exec degraded missing:\n%s", md2)
	}
}

// TestRenderMDDeptHighTierLabel 验证个人周报「同岗使用效能画像」指标行透出 derive 侧改名后的
// 「高阶模型成本占比」label（Part2 把 high_tier_cost_ratio 指标 label 由「成本最高模型成本占比」改为「高阶模型成本占比」）。
func TestRenderMDDeptHighTierLabel(t *testing.T) {
	r := markdownReport("personal")
	r.Judgment.PeerProfiles = []report.PeerMetric{
		{Metric: "high_tier_cost_ratio", Label: "高阶模型成本占比", Unit: "%",
			Current: 0.45, P50: 0.30, PercentileRank: 0.8, Supported: true},
	}
	md := RenderMD(&r)
	if !strings.Contains(md, "| 高阶模型成本占比 | 45.0% | 30.0% | P80 |") {
		t.Errorf("peer profile high-tier label missing:\n%s", md)
	}
}

// TestRenderMDDeptAnomalyStateMachine 验证部门五章「异常处置」消费 t14 的 AnomalyHit.Status：
// 输出 5 态计数（CountAnomalyStatus，仅命中记录参与）+ 每条命中异常的状态；未命中不参与 5 态计数。
func TestRenderMDDeptAnomalyStateMachine(t *testing.T) {
	r := markdownReport("department")
	r.Judgment.Anomalies = []report.AnomalyHit{
		{Rule: "A1", Name: "程序失控", Severity: "P1", Hit: true},
		{Rule: "A2", Name: "成本燃烧", Severity: "P2", Hit: true, Status: report.AnomalyStatusFixing},
		{Rule: "A3", Name: "重试循环", Severity: "P3", Hit: false, Status: report.AnomalyStatusConfirmedOK},
	}
	md := RenderMD(&r)
	for _, want := range []string{
		"- 命中 2 条 | 未命中 1 条",
		"- 异常状态机：待确认=1 | 已确认正常=0 | 修复中=1 | 已恢复=0 | 误报=0",
		"- [P1] A1 —— 程序失控（状态：待确认）",
		"- [P2] A2 —— 成本燃烧（状态：修复中）",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("dept anomaly state machine missing %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "已确认正常=1") {
		t.Errorf("未命中 A3 不应计入异常状态机 5 态计数（仅命中记录参与）:\n%s", md)
	}
}

// TestRenderMDStudioRiskStatus 验证工作室第四章「风险与行动清单」状态列读 AnomalyHit.Status（t14 5 态），
// 而非硬编码「待确认」；空状态降级为「待确认」。
func TestRenderMDStudioRiskStatus(t *testing.T) {
	r := markdownReport("studio")
	r.Judgment.Anomalies = []report.AnomalyHit{
		{Rule: "A2", Name: "成本燃烧", Severity: "P2", Hit: true, Status: report.AnomalyStatusRecovered},
		{Rule: "A1", Name: "程序失控", Severity: "P1", Hit: true, Status: report.AnomalyStatusPending},
	}
	md := RenderMD(&r)
	for _, want := range []string{
		"| 已恢复 | 应用Owner |",
		"| 待确认 | 应用Owner |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("studio 风险清单状态列未读 AnomalyHit.Status: missing %q:\n%s", want, md)
		}
	}
}

// TestRenderMDDeptTopChangedAdvice 验证部门第六章「建议」精简：①仅对命中异常的重点变化成员给精简建议；
// ②补一条岗位级通用建议（人均成本高于部门人均衡的岗位）；③不再对未命中异常成员逐条重复建议。
func TestRenderMDDeptTopChangedAdvice(t *testing.T) {
	u := &report.UnitFact{
		Member: report.UnitMember{
			UserCount: 20, ActiveUserCount: 10, ReqCount: 1000, SuccessCount: 950, EstCostUSD: 10.0, TotalTokens: 10000,
			PerCapita: report.PerCapita{EstCostUSD: 50},
		},
		JobGroups: []report.JobGroup{
			{JobCategory: "工程师", ActiveUsers: 3, AvgCostUSD: 100, MedianCostUSD: 2, TopModel: "gpt-4o"}, // 人均 100 > 部门人均 50 → 触发岗位级通用建议
			{JobCategory: "设计", ActiveUsers: 3, AvgCostUSD: 20, MedianCostUSD: 1, TopModel: "gpt-4o"},
		},
		TopChanged: []report.TopChangedMember{
			{Idx: 1, JobCategory: "工程师", WoWCostPct: 120.0, AnomalyCause: "A2 成本燃烧"},
			{Idx: 2, JobCategory: "设计", WoWCostPct: -30.0, AnomalyCause: ""}, // 未命中异常 → 不给建议
		},
	}
	r := markdownReport("department")
	r.Fact.Department = u
	// 命中 A2 异常，使 五、异常处置 计数非空（避免「无异常」兜底行干扰断言）。
	r.Judgment.Anomalies = []report.AnomalyHit{{Rule: "A2", Name: "成本燃烧", Severity: "P1", Hit: true}}
	md := RenderMD(&r)
	for _, want := range []string{
		"- 员工1（工程师，成本环比 +120.0%）：确认是否存在后台脚本/循环调用或异常流量，必要时限流",
		"- 岗位级通用建议：工程师 岗人均成本最高（¥720.00），建议核对是否存在后台脚本/批量调用，必要时限流或降档，并统一可缓存的提示词模板以提升缓存复用。",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("department 六、建议 missing %q:\n%s", want, md)
		}
	}
	// 未命中异常的员工2不再逐条重复建议（不会出现「- 员工2（设计…）」形式的建议行）；
	// 也不再输出旧的「重点变化成员建议：」子列表头。
	if strings.Contains(md, "- 员工2（设计，成本环比 -30.0%）") {
		t.Errorf("六、建议 不应再对未命中异常成员重复建议:\n%s", md)
	}
	if strings.Contains(md, "- 重点变化成员建议：") {
		t.Errorf("六、建议 仍使用逐成员子列表头，应改为精简建议:\n%s", md)
	}
}

// TestMemberJobDisplay 验证成员岗位展示名的残差回退口径：
// 优先 derive 已算好的 JobDisplay；否则 JobTitle 非残差→取 JobTitle；其次 JobCategory 非残差→取 JobCategory；
// 仍残差/空→「未归类」。与主因「未归类岗位」一致，避免「其他/未分类」等无信息量占位。
func TestMemberJobDisplay(t *testing.T) {
	cases := []struct {
		name string
		tc   report.TopChangedMember
		want string
	}{
		{"JobDisplay 已算好则直接用", report.TopChangedMember{JobCategory: "其他", JobTitle: "", JobDisplay: "后端开发工程师"}, "后端开发工程师"},
		{"JobCategory 残差→JobTitle", report.TopChangedMember{JobCategory: "其他", JobTitle: "后端开发工程师"}, "后端开发工程师"},
		{"JobCategory 残差且 JobTitle 残差→未归类", report.TopChangedMember{JobCategory: "未知", JobTitle: "未分类"}, "未归类"},
		{"JobCategory 残差且 JobTitle 空→未归类", report.TopChangedMember{JobCategory: "其他", JobTitle: ""}, "未归类"},
		{"JobCategory 非残差→直接用", report.TopChangedMember{JobCategory: "工程师", JobTitle: ""}, "工程师"},
	}
	for _, c := range cases {
		if got := memberJobDisplay(c.tc); got != c.want {
			t.Errorf("%s: memberJobDisplay(%+v)=%q, want %q", c.name, c.tc, got, c.want)
		}
	}
}

// TestRenderMDDeptTopChangedJobResidualFallback 验证四章重点变化表岗位列做残差回退：
// 岗位列从无信息量的「其他/未知」回退到具体 JobTitle；仍残差/空则显示「未归类」。并确建议章节岗位列同步一致。
func TestRenderMDDeptTopChangedJobResidualFallback(t *testing.T) {
	u := &report.UnitFact{
		Member: report.UnitMember{UserCount: 20, ActiveUserCount: 10, ReqCount: 1000, SuccessCount: 950, EstCostUSD: 10.0, TotalTokens: 10000},
		TopChanged: []report.TopChangedMember{
			{Idx: 1, JobCategory: "其他", JobTitle: "后端开发工程师", WoWCostPct: 120.0, AnomalyCause: "A2 成本燃烧"},
			{Idx: 2, JobCategory: "未知", JobTitle: "", WoWCostPct: 60.0, AnomalyCause: "A5 非工作时段"},
		},
	}
	r := markdownReport("department")
	r.Fact.Department = u
	r.Judgment.Anomalies = []report.AnomalyHit{{Rule: "A2", Name: "成本燃烧", Severity: "P1", Hit: true}}
	md := RenderMD(&r)
	for _, want := range []string{
		// 四章重点变化表岗位列：残差「其他」→具体 job_title；残差「未知」且无 title →「未归类」。
		"| 员工1 | 后端开发工程师 | +120.0% | A2 成本燃烧 | 已解释 |",
		"| 员工2 | 未归类 | +60.0% | A5 非工作时段 | 已解释 |",
		// 六、建议岗位列与四章一致，不再出现「其他/未知」。
		"- 员工1（后端开发工程师，成本环比 +120.0%）：",
		"- 员工2（未归类，成本环比 +60.0%）：",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("department 四章岗位列残差回退 missing %q:\n%s", want, md)
		}
	}
	// 不应再出现无信息量的「其他/未知」作为岗位展示。
	if strings.Contains(md, "员工1（其他，成本环比") || strings.Contains(md, "员工2（未知，成本环比") {
		t.Errorf("四章/建议岗位列不应再显示「其他/未知」占位:\n%s", md)
	}
}

// TestPositionLevelAdvice 直接验证 positionLevelAdvice 边界：
// 高于部门人均衡的岗位给通用建议；低于/等于部门人均、空列表、人均成本为 0、残差/低活跃岗位均不给建议。
func TestPositionLevelAdvice(t *testing.T) {
	rate := 7.2
	// ① 高于部门人均 → 给建议
	if s := positionLevelAdvice([]report.JobGroup{{JobCategory: "后端开发", ActiveUsers: 5, AvgCostUSD: 100}}, 50, rate); !strings.Contains(s, "后端开发 岗人均成本最高") {
		t.Errorf("p.l.a 应给岗位级建议，got %q", s)
	}
	// ② 低于部门人均 → 不输出（避免噪音）
	if s := positionLevelAdvice([]report.JobGroup{{JobCategory: "后端开发", ActiveUsers: 5, AvgCostUSD: 30}}, 50, rate); s != "" {
		t.Errorf("p.l.a 人均低于部门人均时应为空，got %q", s)
	}
	// ③ 空列表 → 空
	if s := positionLevelAdvice(nil, 50, rate); s != "" {
		t.Errorf("p.l.a 空列表应为空，got %q", s)
	}
	// ④ 人均成本为 0 → 空
	if s := positionLevelAdvice([]report.JobGroup{{JobCategory: "后端开发", ActiveUsers: 5, AvgCostUSD: 0}}, 50, rate); s != "" {
		t.Errorf("p.l.a 人均成本为 0 应为空，got %q", s)
	}
	// ⑤ 低活跃/残差岗被忽略（即使成本高）
	groups := []report.JobGroup{
		{JobCategory: "其他", ActiveUsers: 1, AvgCostUSD: 999},
		{JobCategory: "交互设计", ActiveUsers: 1, AvgCostUSD: 999},
	}
	if s := positionLevelAdvice(groups, 50, rate); s != "" {
		t.Errorf("p.l.a 残差/低活跃岗应被忽略，got %q", s)
	}
}

// TestRenderMDPersonalAnomalyHitsOnly 验证个人第六章「异常提示」对齐模板：只列【命中】异常
// （带阈值/实际/基线/影响成本/完整证据链），未命中的在末尾简要注记、不再作为主体异常清单条目。
func TestRenderMDPersonalAnomalyHitsOnly(t *testing.T) {
	r := markdownReport("personal")
	r.Judgment.Anomalies = []report.AnomalyHit{
		{Rule: "A2", Name: "成本燃烧", Severity: "P1", Hit: true,
			ActualValue: 120, ThresholdValue: 100, BaselineValue: 80, BaselineWindow: "上周",
			ImpactCostUSD: 500, ExcessCostUSD: 200,
			Evidence: []report.Evidence{{Field: "cause", Value: "后台脚本循环调用"}}},
		{Rule: "A1", Name: "程序失控", Severity: "P2", Hit: false},
	}
	md := RenderMD(&r)
	for _, want := range []string{
		"- [A2] **成本燃烧**（P1）",
		"  - `actual=120.00` · `threshold=100.00` · `baseline=80.00`（上周）",
		"  - 影响成本 `500.00` USD · 超支 `200.00` USD",
		"  - cause: `后台脚本循环调用`",
		"- 另有 1 条规则未命中（判定未触发，不列入异常清单）：A1",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("personal 六、异常提示 hits-only missing %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "- [A1]") {
		t.Errorf("未命中 A1 仍作为主体异常条目列出（模板只列命中）:\n%s", md)
	}
}

// TestRenderMDPersonalFeedbackOptions 验证个人第六章「异常提示」反馈入口选项由 3 项（□属实/□误报/□已恢复）
// 对齐为 5 项（□正常任务/□已停止/□归属错误/□误报/□需协助）。
func TestRenderMDPersonalFeedbackOptions(t *testing.T) {
	r := markdownReport("personal")
	md := RenderMD(&r)
	if !strings.Contains(md, "- 反馈入口：请确认该异常（□正常任务 □已停止 □归属错误 □误报 □需协助）——") {
		t.Errorf("personal 反馈入口未更新为 5 选项:\n%s", md)
	}
	if strings.Contains(md, "□属实") {
		t.Errorf("反馈入口仍含旧选项「□属实」，应改为 5 选项:\n%s", md)
	}
}

// TestRenderMDPersonalEfficiencyLabel 验证个人第三章「同岗使用效能画像」效能标签追加一句动态解释
// （本人 vs 同岗 P50 分位）：高采用→使用量高于同岗；待优化→单位 Token 成本偏高、缓存比偏低等。
func TestRenderMDPersonalEfficiencyLabel(t *testing.T) {
	r := markdownReport("personal")
	r.Judgment.PeerProfiles = []report.PeerMetric{
		{Metric: "weekly_calls", Label: "周调用量", Unit: "次", Current: 50, P50: 30, PercentileRank: 0.7, Supported: true},
		{Metric: "cost_per_1m", Label: "单位 Token 成本", Unit: "USD", Current: 3.0, P50: 2.0, PercentileRank: 0.8, Supported: true},
		{Metric: "cache_hit_rate", Label: "缓存命中率", Unit: "%", Current: 0.2, P50: 0.4, PercentileRank: 0.1, Supported: true},
	}
	r.Expression.EfficiencyLabel = "高采用、待优化"
	md := RenderMD(&r)
	if !strings.Contains(md, "- 使用效能标签：**高采用、待优化**——使用量高于同岗中位数，但单位 Token 成本偏高、缓存比偏低") {
		t.Errorf("效能标签未追加动态解释:\n%s", md)
	}
}

// TestBuildWeeklyConclusionRootCause 结论主因一句话（③）：命中异常时把成本环比 + 主因写入结论行。
func TestBuildWeeklyConclusionRootCause(t *testing.T) {
	r := &report.Report{
		Fact: report.Fact{Personal: &report.PersonalFact{WoWCostPct: 180}},
		Judgment: report.Judgment{Anomalies: []report.AnomalyHit{
			{Rule: "A2", Name: "成本燃烧", Severity: "P1", Hit: true, Window: "15:00~15:15", Note: "触发封顶 ¥300/15min"},
		}},
	}
	got := buildWeeklyConclusion(r)
	if !strings.Contains(got, "成本环比 +180%") || !strings.Contains(got, "主因 A2 成本燃烧") {
		t.Errorf("结论未含成本环比+主因: %q", got)
	}
	if !strings.Contains(got, "15:00~15:15") || !strings.Contains(got, "触发封顶") {
		t.Errorf("主因未含窗口/说明: %q", got)
	}
}

// TestBuildWeeklyConclusionNoAnomaly 无异常命中时回退到成本环比；异常存在而未命中用无异常措辞。
func TestBuildWeeklyConclusionNoAnomaly(t *testing.T) {
	r := &report.Report{
		Fact: report.Fact{Personal: &report.PersonalFact{WoWCostPct: 180}},
	}
	r.Judgment.Anomalies = []report.AnomalyHit{{Rule: "A2", Name: "成本燃烧", Hit: false}}
	got := buildWeeklyConclusion(r)
	if !strings.Contains(got, "成本环比 +180%") || !strings.Contains(got, "无异常命中") {
		t.Errorf("无异常命中结论未含成本环比+无异常措辞: %q", got)
	}
}

// TestBuildDeptConclusionMainCause 验证部门/工作室【本周结论】读取 derive 层 MainCause（Supported && 非空）：
// 直接展示主因句，替换旧的「关注 —— 命中 N 条异常（P1）」。
func TestBuildDeptConclusionMainCause(t *testing.T) {
	r := markdownReport("department")
	r.Fact.Department.Conclusion = &report.UnitConclusion{
		Supported: true,
		MainCause: "成本环比 +32%，集中在 3 名后端开发，其一命中A5 非工作时段",
	}
	if got := buildDeptConclusion(&r); got != "成本环比 +32%，集中在 3 名后端开发，其一命中A5 非工作时段" {
		t.Errorf("buildDeptConclusion 应直接展示 MainCause，got %q", got)
	}
	// 端到端：渲染后【本周结论】行含主因句，不含旧的「命中 %d 条异常」。
	md := RenderMD(&r)
	if !strings.Contains(md, "【本周结论】成本环比 +32%，集中在 3 名后端开发，其一命中A5 非工作时段") {
		t.Errorf("渲染后【本周结论】未含主因句:\n%s", md)
	}
}

// TestBuildDeptConclusionDegraded 验证 MainCause 不可用（Supported=false 或 MainCause 为空串）时降级到现有逻辑：
// 有命中异常 →「关注 —— 命中 N 条异常（P1）」；无异常且成本上涨 →「关注 —— 成本环比 +X%」。
func TestBuildDeptConclusionDegraded(t *testing.T) {
	// ① Conclusion 为 nil → 降级到命中异常计数
	r := markdownReport("department")
	r.Judgment.Anomalies = []report.AnomalyHit{{Rule: "A2", Name: "成本燃烧", Severity: "P1", Hit: true}}
	if got := buildDeptConclusion(&r); !strings.Contains(got, "关注 —— 命中 1 条异常（P1）") {
		t.Errorf("Conclusion 为 nil 时应降级到命中异常，got %q", got)
	}
	// ② Supported=true 但 MainCause 为空串 → 降级到成本上涨
	r2 := markdownReport("department")
	r2.Fact.Department.Conclusion = &report.UnitConclusion{Supported: true, MainCause: "", CostDeltaPct: 32}
	r2.Fact.Department.WoWCostPct = 32
	if got := buildDeptConclusion(&r2); got != "关注 —— 成本环比 +32%" {
		t.Errorf("MainCause 为空串时应降级到成本上涨，got %q", got)
	}
	// ③ Supported=false → 降级到成本上涨（MainCause 即使有内容也不展示）
	r3 := markdownReport("department")
	r3.Fact.Department.Conclusion = &report.UnitConclusion{Supported: false, MainCause: "不应展示"}
	r3.Fact.Department.WoWCostPct = 32
	if got := buildDeptConclusion(&r3); got != "关注 —— 成本环比 +32%" {
		t.Errorf("Supported=false 时应降级到成本上涨，got %q", got)
	}
}

// TestScopeLabelWithStudio 验证部门标题「部门名（工作室名）」：有父工作室名→追加；空/同名→不追加。
func TestScopeLabelWithStudio(t *testing.T) {
	cases := []struct {
		name string
		s    report.Scope
		want string
	}{
		{"有父工作室名", report.Scope{Name: "后端研发部", StudioName: "技术研究工作室"}, "后端研发部（技术研究工作室）"},
		{"StudioName 为空", report.Scope{Name: "后端研发部", StudioName: ""}, "后端研发部"},
		{"StudioName 与部门名相同（工作室级自身名）", report.Scope{Name: "技术研究工作室", StudioName: "技术研究工作室"}, "技术研究工作室"},
		{"Name 为空回退 ID", report.Scope{ID: "dept-1", StudioName: "技术研究工作室"}, "dept-1（技术研究工作室）"},
	}
	for _, c := range cases {
		if got := scopeLabelWithStudio(c.s); got != c.want {
			t.Errorf("%s: scopeLabelWithStudio(%+v)=%q, want %q", c.name, c.s, got, c.want)
		}
	}
}

// TestRenderMDDeptTitleWithStudio 验证部门周报标题带父工作室归属（端到端）；StudioName 未解析时不追加。
func TestRenderMDDeptTitleWithStudio(t *testing.T) {
	r := markdownReport("department")
	r.Meta.Period.IsoWeek = "2026-W33"
	r.Meta.Scope = report.Scope{Level: "department", ID: "dept-1", Name: "后端研发部", StudioName: "技术研究工作室"}
	md := RenderMD(&r)
	if !strings.Contains(md, "# 部门周报 · 2026-W33 · 后端研发部（技术研究工作室）") {
		t.Errorf("部门标题未带父工作室归属:\n%s", md)
	}
	// StudioName 未解析时不追加
	r2 := markdownReport("department")
	r2.Meta.Scope = report.Scope{Level: "department", ID: "dept-1", Name: "后端研发部", StudioName: ""}
	if md2 := RenderMD(&r2); strings.Contains(md2, "后端研发部（技术研究工作室）") {
		t.Errorf("StudioName 为空时不应追加工作室名:\n%s", md2)
	}
}

// TestRenderMDDeptJobGroupsCompact 验证部门「二、岗位分布」表精简：
// 去除「其他」等合并占位行、仅保留活跃人数≥2、人均成本降序只取前 6 行，且表格行数收敛。
func TestRenderMDDeptJobGroupsCompact(t *testing.T) {
	u := &report.UnitFact{
		Member: report.UnitMember{UserCount: 20, ActiveUserCount: 12, ReqCount: 1000, SuccessCount: 950, EstCostUSD: 10.0, TotalTokens: 10000},
		// 10 组：含「其他」占位与多个活跃人数为 1 的噪音岗，验证被过滤；人均成本非严格递减以验证只做截断不重排。
		JobGroups: []report.JobGroup{
			{JobCategory: "WEB前端开发", ActiveUsers: 3, AvgCostUSD: 40, MedianCostUSD: 2, TopModel: "gpt-4o"},
			{JobCategory: "AI工程师", ActiveUsers: 4, AvgCostUSD: 60, MedianCostUSD: 3, TopModel: "claude"},
			{JobCategory: "交互设计", ActiveUsers: 1, AvgCostUSD: 25, MedianCostUSD: 1, TopModel: "gpt-4o"}, // 活跃<2 → 过滤
			{JobCategory: "其他", ActiveUsers: 1, AvgCostUSD: 5, MedianCostUSD: 0, TopModel: ""},          // 合并占位 → 过滤
			{JobCategory: "服务端开发", ActiveUsers: 5, AvgCostUSD: 30, MedianCostUSD: 1, TopModel: "qwen"},
			{JobCategory: "软件测试", ActiveUsers: 4, AvgCostUSD: 20, MedianCostUSD: 1, TopModel: "gpt-4o"},
			{JobCategory: "UI设计", ActiveUsers: 2, AvgCostUSD: 18, MedianCostUSD: 1, TopModel: "gpt-4o"},
			{JobCategory: "PMO", ActiveUsers: 1, AvgCostUSD: 15, MedianCostUSD: 1, TopModel: "gpt-4o"}, // 活跃<2 → 过滤
			{JobCategory: "项目管理", ActiveUsers: 5, AvgCostUSD: 10, MedianCostUSD: 1, TopModel: "qwen"},
			{JobCategory: "后端开发", ActiveUsers: 6, AvgCostUSD: 12, MedianCostUSD: 1, TopModel: "qwen"},
		},
	}
	r := markdownReport("department")
	r.Fact.Department = u
	md := RenderMD(&r)

	// 保留 7 组按活跃人数降序（并列按人均成本降序），Top6 截断：后端开发(6)、服务端开发(5)、项目管理(5)、AI工程师(4)、软件测试(4)、WEB前端开发(3)。
	for _, want := range []string{
		"| 后端开发 | 6 |",
		"| 服务端开发 | 5 |",
		"| 项目管理 | 5 |",
		"| AI工程师 | 4 |",
		"| 软件测试 | 4 |",
		"| WEB前端开发 | 3 |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("岗位分布表缺少保留行 %q:\n%s", want, md)
		}
	}
	// 被过滤的残差/低活跃行不得出现；UI设计因 Top6 截断也不得出现。
	for _, banned := range []string{
		"| 其他 |",
		"| 交互设计 |",
		"| PMO |",
		"| UI设计 |",
	} {
		if strings.Contains(md, banned) {
			t.Errorf("岗位分布表仍含应过滤行 %q:\n%s", banned, md)
		}
	}
	// 若过滤发生，应输出说明规则的行
	if !strings.Contains(md, "- 注：仅展示活跃人数≥2 且非「其他」等合并占位的岗位 Top6（按活跃人数降序）。") {
		t.Errorf("岗位分布表未输出精简规则说明:\n%s", md)
	}
}

// TestCompactJobGroups 直接验证 compactJobGroups：残差行/低活跃过滤 + 按活跃人数降序 + Top6 截断 + 不修改输入切片。
func TestCompactJobGroups(t *testing.T) {
	in := []report.JobGroup{
		{JobCategory: "A", ActiveUsers: 2, AvgCostUSD: 5},
		{JobCategory: "其他", ActiveUsers: 3, AvgCostUSD: 999}, // 残差，即使活跃高也过滤
		{JobCategory: "B", ActiveUsers: 1, AvgCostUSD: 4},    // 活跃<2，过滤
		{JobCategory: "C", ActiveUsers: 8, AvgCostUSD: 1},
		{JobCategory: "D", ActiveUsers: 7, AvgCostUSD: 2},
		{JobCategory: "E", ActiveUsers: 6, AvgCostUSD: 3},
		{JobCategory: "F", ActiveUsers: 5, AvgCostUSD: 4},
		{JobCategory: "G", ActiveUsers: 3, AvgCostUSD: 5},
		{JobCategory: "H", ActiveUsers: 4, AvgCostUSD: 6},
		{JobCategory: "I", ActiveUsers: 9, AvgCostUSD: 0}, // 活跃最高，排第一
	}
	got := compactJobGroups(in)
	if len(got) != 6 {
		t.Fatalf("compactJobGroups should cap to 6 rows, got %d: %+v", len(got), got)
	}
	for _, g := range got {
		if g.JobCategory == "其他" || g.JobCategory == "B" {
			t.Errorf("compactJobGroups 仍含应过滤行 %q", g.JobCategory)
		}
	}
	// 按活跃人数降序：I(9) C(8) D(7) E(6) F(5) H(4)；A(2)/G(3) 因截断被排除（H 与 G 皆在 cap 前）。
	wantOrder := []string{"I", "C", "D", "E", "F", "H"}
	for i, w := range wantOrder {
		if got[i].JobCategory != w {
			t.Errorf("compactJobGroups 排序错误 got[%d]=%s want=%s (full %+v)", i, got[i].JobCategory, w, got)
		}
	}
	// 未修改输入
	if len(in) != 10 || in[1].JobCategory != "其他" {
		t.Errorf("compactJobGroups 修改了输入切片")
	}
}
