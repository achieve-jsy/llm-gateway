package analysis

// analysis.go —— 模型分析章节注入层（t2）。
//
// 职责：按 level 从 report 抽取关键数据摘要（MiniJSON，绝不序列化全量 Report）→ 构造中文 prompt
// → 调 llm.Client.Complete → 回填 report.ModelAnalysis{Level, Sections, Note}。
//
// 铁律：
//   - 绝不 panic、绝不中断下游报告流程；LLM 失败/未启用一律安全降级。
//   - 绝不把 API key、token 等敏感字段写入 prompt 或错误日志。
//   - 只消费 report 已派生字段，不在本包内做二次业务计算。

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/example/llm-gateway/internal/observability/report"
	"github.com/example/llm-gateway/tools/usage-report/llm"
)

// Generate 为指定 level 构造一次 LLM 模型分析并回填 ModelAnalysis。
//
// 返回契约：本函数保证绝不返回 (nil, nil)；三种情形都返回非 nil 的 *report.ModelAnalysis：
//   - 成功：Sections 为 LLM 生成的 Markdown，Note 为空，err 为 nil；
//   - 未启用（cli 为 nil）：Sections 空、Note 说明，err 为 nil；
//   - 调用失败/空输出：Sections 空、Note 说明，err 非 nil（供调用方按需记录）。
//
// 因此调用方可直接 `rep.ModelAnalysis = ma`（只要 ma != nil），并在 err != nil 时按需 log，
// 保证降级说明能到达渲染层（t3 依赖 Note 展示降级原因）。
func Generate(ctx context.Context, cli *llm.Client, level string, rep *report.Report, fxRate float64) (*report.ModelAnalysis, error) {
	if cli == nil {
		return &report.ModelAnalysis{Level: level, Note: "LLM 分析未启用，跳过（确定性输出保留）"}, nil
	}
	if rep == nil {
		return &report.ModelAnalysis{Level: level, Note: "报告数据不可得，跳过"}, fmt.Errorf("model analysis: report is nil")
	}

	summary := buildSummary(level, rep, fxRate)
	system := systemPrompt(level)
	user := userPrompt(level, summary)

	text, err := cli.Complete(ctx, system, user)
	if err != nil {
		return &report.ModelAnalysis{Level: level, Note: "LLM 分析失败，回退到确定性输出"}, fmt.Errorf("model analysis: %w", err)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return &report.ModelAnalysis{Level: level, Note: "LLM 返回为空，回退到确定性输出"}, fmt.Errorf("model analysis: empty llm output")
	}
	return &report.ModelAnalysis{Level: level, Sections: text}, nil
}

// ---- prompt 构造 ----

// systemPrompt 返回固定中文数据分析师系统提示：输出结构化的 Markdown 小节。
func systemPrompt(level string) string {
	return "你是一名资深的大模型用量数据分析师，负责为用量周报撰写「模型分析」章节。" +
		"请仅依据给定的数据摘要（JSON），输出一段结构清晰、结论先行、可执行的中文 Markdown 小节，" +
		"包含若干二级/三级子标题与要点。" +
		"要求：1) 基于数据、不编造数字；2) 先给结论再给依据与建议；3) 语言精炼务实，避免空话套话；" +
		"4) 直接输出 Markdown 正文，不要任何前后缀说明或代码块标记。"
}

// userPrompt 生成 level 特定的分析要求 + 数据摘要 JSON。
func userPrompt(level string, summary map[string]interface{}) string {
	data, _ := json.MarshalIndent(summary, "", "  ")
	req := levelRequirement(level)
	if strings.TrimSpace(req) == "" {
		req = "请根据数据给出模型分析建议。"
	}
	return "数据摘要（JSON）：\n" + string(data) + "\n\n分析要求：" + req + "\n\n请输出「模型分析」Markdown 章节。"
}

// levelRequirement 各 level 的分析视角模板。
func levelRequirement(level string) string {
	switch level {
	case "personal":
		return "请从「个人模型使用」视角分析：主用/常用模型与模型组合是否合理、成本与缓存命中、" +
			"是否有模型替换/降本空间，并给出「场景与合理用模」建议（结合同岗画像，若数据可得）。"
	case "department":
		return "请从「部门模型组合效率」视角分析：成员成本与岗位分布、Service Key 使用、模型组合效率与成本优化建议、" +
			"重点变化成员与风险异常，给出部门层面的模型治理建议。"
	case "studio":
		return "请从「工作室模型策略」全局视角分析：一页摘要（成本/占比/趋势）、部门贡献集中度、变化归因、风险清单，" +
			"给出工作室级别的模型策略与治理建议。"
	default:
		return ""
	}
}

// ---- 数据摘要 ----

// buildSummary 从 report 抽取各 level 关键字段，构造 MiniJSON 摘要（只取叶子字段，不序列化全量 Report）。
func buildSummary(level string, rep *report.Report, fxRate float64) map[string]interface{} {
	s := map[string]interface{}{
		"level":           rep.Meta.Level,
		"period":          rep.Meta.Period.Label,
		"scope":           rep.Meta.Scope.Name,
		"scope_id":        rep.Meta.Scope.ID,
		"fx_rate_usd_cny": round4(fxRate),
		"schema_version":  rep.SchemaVersion,
	}

	if hits := anomalyHits(rep.Judgment); len(hits) > 0 {
		s["anomalies"] = hits
	}
	if e := rep.Judgment.Efficiency; e.SuccessRate != 0 {
		s["success_rate"] = round4(e.SuccessRate)
		s["cost_per_1k_tokens_usd"] = round4(e.CostPer1kTokens)
		if e.CacheHitRate != 0 {
			s["cache_hit_rate"] = round4(e.CacheHitRate)
		}
	}

	switch level {
	case "personal":
		if p := rep.Fact.Personal; p != nil {
			s["req_count"] = p.ReqCount
			s["top_model"] = p.TopModel // 成本最高模型
			s["model_count"] = p.ModelCount
			s["est_cost_usd"] = round4(p.EstCostUSD)
			s["high_tier_cost_ratio"] = round4(p.HighTierCostRatio)
			s["cache_hit_rate"] = round4(p.CacheHitRate)
			s["wow_cost_pct"] = round4(p.WoWCostPct)
			s["wow_req_pct"] = round4(p.WoWReqPct)
			if len(p.TopModels) > 0 {
				s["top_models"] = summarizeTopModels(p.TopModels)
			}
		}
		if mp := rep.Judgment.TopModelPortrait; mp != nil {
			s["top_model_portrait"] = map[string]interface{}{
				"model":                mp.Model,
				"person_cost_usd":      round4(mp.PersonCostUSD),
				"person_cost_ratio":    round4(mp.PersonCostRatio),
				"adoption_count":       mp.AdoptionCount,
				"pool_n":               mp.PoolN,
				"adoption_rate":        round4(mp.AdoptionRate),
				"person_unit_cost_usd": round4(mp.PersonUnitCost),
				"unit_cost_p50_usd":    round4(mp.UnitCostP50),
			}
		}
	case "department":
		if u := rep.Fact.Department; u != nil {
			fillUnitSummary(s, u)
		}
	case "studio":
		if u := rep.Fact.Studio; u != nil {
			fillUnitSummary(s, u)
			s["company_ratio"] = round4(u.CompanyRatio)
			s["optimizable_cost_usd"] = round4(u.OptimizableCostUSD)
			s["cost_concentration"] = round4(u.CostConcentration)
			if len(u.DeptContributions) > 0 {
				s["dept_contributions"] = summarizeDeptContributions(u.DeptContributions)
			}
			if len(u.AnomalySummary) > 0 {
				s["anomaly_summary"] = u.AnomalySummary
			}
			if len(u.WeeklyTrend) > 0 {
				s["weekly_trend"] = summarizeWeeklyTrend(u.WeeklyTrend)
			}
		}
		if len(rep.Judgment.Attribution) > 0 {
			s["attribution"] = summarizeAttribution(rep.Judgment.Attribution)
		}
	}
	return s
}

// fillUnitSummary 填充 department/studio 共用的单元级字段。
func fillUnitSummary(s map[string]interface{}, u *report.UnitFact) {
	m := u.Member
	s["member_cost_usd"] = round4(m.EstCostUSD)
	s["member_req"] = m.ReqCount
	s["member_model_count"] = m.ModelCount
	if m.CacheHitRate != 0 {
		s["member_cache_hit_rate"] = round4(m.CacheHitRate)
	}
	s["service_key_req"] = u.ServiceKey.ReqCount
	if u.ServiceKey.EstCostUSD != 0 {
		s["service_key_cost_usd"] = round4(u.ServiceKey.EstCostUSD)
	}
	s["wow_cost_pct"] = round4(u.WoWCostPct)
	if len(m.TopModels) > 0 {
		s["member_top_models"] = summarizeTopModels(m.TopModels)
	}
	if len(u.JobGroups) > 0 {
		s["job_groups"] = summarizeJobGroups(u.JobGroups)
	}
	if len(u.TopChanged) > 0 {
		s["top_changed"] = summarizeTopChanged(u.TopChanged)
	}
}

// ---- 小结构序列化 ----

func summarizeTopModels(tms []report.TopModel) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(tms))
	for _, t := range tms {
		out = append(out, map[string]interface{}{
			"model":        t.Model,
			"calls":        t.Calls,
			"total_tokens": t.TotalTokens,
			"cost_usd":     round4(t.CostUSD),
		})
	}
	return out
}

func summarizeJobGroups(jgs []report.JobGroup) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(jgs))
	for _, jg := range jgs {
		out = append(out, map[string]interface{}{
			"job_category":    jg.JobCategory,
			"active_users":    jg.ActiveUsers,
			"avg_cost_usd":    round4(jg.AvgCostUSD),
			"median_cost_usd": round4(jg.MedianCostUSD),
			"top_model":       jg.TopModel,
		})
	}
	return out
}

func summarizeTopChanged(tcs []report.TopChangedMember) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(tcs))
	for _, tc := range tcs {
		out = append(out, map[string]interface{}{
			"idx":          tc.Idx,
			"job_category": tc.JobCategory,
			"wow_cost_pct": round4(tc.WoWCostPct),
			"top_model":    tc.TopModel,
			"anomaly_rule": tc.AnomalyRule,
		})
	}
	return out
}

func summarizeDeptContributions(dcs []report.DeptContribution) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(dcs))
	for _, dc := range dcs {
		out = append(out, map[string]interface{}{
			"dept_name":    dc.DeptName,
			"active_users": dc.ActiveUsers,
			"cost_usd":     round4(dc.CostUSD),
			"cost_ratio":   round4(dc.CostRatio),
			"avg_cost_usd": round4(dc.AvgCostUSD),
			"wow_cost_pct": round4(dc.WoWCostPct),
		})
	}
	return out
}

func summarizeWeeklyTrend(pts []report.WeeklyPoint) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(pts))
	for _, p := range pts {
		out = append(out, map[string]interface{}{
			"week_start": p.WeekStart,
			"week_end":   p.WeekEnd,
			"cost_usd":   round4(p.CostUSD),
			"req_count":  p.ReqCount,
		})
	}
	return out
}

func summarizeAttribution(attrs []report.Attribution) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(attrs))
	for _, a := range attrs {
		out = append(out, map[string]interface{}{
			"subject": a.Subject,
			"detail":  a.Detail,
			"weight":  round4(a.Weight),
		})
	}
	return out
}

// anomalyHits 只保留命中（Hit）的异常，压缩为 prompt 友好的紧凑结构。
func anomalyHits(j report.Judgment) []map[string]interface{} {
	var out []map[string]interface{}
	for _, a := range j.Anomalies {
		if !a.Hit {
			continue
		}
		out = append(out, map[string]interface{}{
			"rule":            a.Rule,
			"name":            a.Name,
			"severity":        a.Severity,
			"actual":          round4(a.ActualValue),
			"threshold":       round4(a.ThresholdValue),
			"impact_cost_usd": round4(a.ImpactCostUSD),
		})
	}
	return out
}

// ---- 工具 ----

// round4 数值四舍五入到 4 位小数，用于压缩 prompt 体积、避免浮点噪音。
func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}
