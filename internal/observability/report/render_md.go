package report

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
)

// 本文件实现 docs/token-usage-analysis-report-templates-final.md 定稿结构的三份报告 MD 渲染器。
// 纯函数：RenderMD(r *Report) string，不依赖 DB/IO。
//
// 文案红线（务必遵守）：
//   - 严重异常(3.6) 只允许「待核查 / 仅供参考」，禁止出现「违规/违纪/滥用」等定责词。
//   - 存在问题的人按管理优先级（严重异常待核查 → 右上优先改善 → 左上习惯辅导）排序，**不按总成本**。
//   - 数据质量真实会话覆盖率 < 50% 时，交互类(3.2 挤牙膏 / 3.3 多任务不切会话)整类标注「降级为疑似，仅供参考」。

// 文案地图。
var mdVerdictTag = map[string]string{
	VerdictConfirmed:   "证据较充分",
	VerdictSuspected:   "疑似",
	VerdictNotHit:      "未命中",
	VerdictNotComputed: "数据不足",
}

var mdIssueDisplay = map[string]string{
	IssueTypePrefixPollution:  "前缀污染",
	IssueTypeToothpaste:       "挤牙膏",
	IssueTypeMultiTaskSession: "多任务不切会话",
	IssueTypeOutputRunaway:    "输出失控",
	IssueTypeOverModel:        "杀鸡用牛刀",
	IssueTypeNormal:           "正常",
}

var mdQuadrantDisplay = map[string]string{
	QuadrantPriorityImprove: "优先改善",
	QuadrantCoach:           "习惯辅导",
	QuadrantRoutine:         "常规使用",
	QuadrantHighCostNormal:  "高用量·正常",
}

// RenderMD 渲染一份报告为 Markdown，按 Meta.Level 分派到三种结构。
func RenderMD(r *Report) string {
	if r == nil {
		return ""
	}
	var rendered string
	switch r.Meta.Level {
	case LevelPersonal:
		rendered = renderPersonal(r)
	case LevelDepartment:
		rendered = renderDepartment(r)
	case LevelStudio:
		rendered = renderStudio(r)
	default:
		return ""
	}
	return strings.TrimRight(rendered, "\n") + "\n"
}

func renderPersonal(r *Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# 个人报告 · %s · %s\n\n", r.Meta.Scope.Name, periodLabel(r))
	fmt.Fprintf(&b, "`数据截止 %s 08:00 · 规则 %s`\n", endDate(r), r.Meta.RuleVersion)
	dataQualityLine(&b, r.DataQuality)
	b.WriteString("\n")

	b.WriteString("## 1. 本周基础数据\n\n")
	renderCostTable(&b, r)
	renderModelTier(&b, r)

	if r.SafetyAnomaly.Hit {
		b.WriteString("\n## 2. 严重异常（待核查）\n\n")
		renderPersonalSafety(&b, r)
	}

	b.WriteString("## 3. 成本构成\n\n")
	renderCostComposition(&b, r)

	b.WriteString("## 4. 效率与情况（五种情况）\n\n")
	renderIssueCards(&b, r)

	b.WriteString("## 5. 本周结论\n\n")
	fmt.Fprintf(&b, "> %s\n\n", r.Conclusion.Note)
	return b.String()
}

func renderDepartment(r *Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# 部门报告 · %s · %s\n\n", r.Meta.Scope.Name, periodLabel(r))
	dataQualityLine(&b, r.DataQuality)
	b.WriteString("\n")

	me := r.MemberEvaluation
	b.WriteString("## 0. 本周结论\n\n")
	fmt.Fprintf(&b, "> %s\n\n", r.Conclusion.Note)

	b.WriteString("## 1. 部门本周基础数据\n\n")
	renderCostTable(&b, r)
	if me != nil {
		fmt.Fprintf(&b, "\n活跃 %d 人 · 人均 %s · 调用 %d 次\n\n", len(me.Members), fmtMoney(r, r.CostBreakdown.Total()/float64(maxInt(1, len(me.Members)))), memberCalls(me.Members))
	}
	renderModelTier(&b, r)

	if len(safetyMembers(me)) > 0 {
		b.WriteString("\n## 2. 严重异常（待核查）\n\n")
		renderDeptSafety(&b, r, me)
	}

	b.WriteString("## 3. 部门成员整合评估图（四象限）\n\n")
	renderQuadrantScope(&b, r, me)

	problem, normal := splitMembers(me)
	b.WriteString("\n## 4. 存在问题的人（按管理优先级，非总成本排序）\n\n")
	renderProblemMembers(&b, r, problem)

	b.WriteString("\n## 5. 正常使用的人\n\n")
	renderNormalMembers(&b, r, normal)
	return b.String()
}

func renderStudio(r *Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# 工作室报告 · %s · %s\n\n", r.Meta.Scope.Name, periodLabel(r))
	dataQualityLine(&b, r.DataQuality)
	b.WriteString("\n")

	me := r.MemberEvaluation
	b.WriteString("## 0. 本周结论\n\n")
	fmt.Fprintf(&b, "> %s\n\n", r.Conclusion.Note)

	b.WriteString("## 1. 工作室本周基础数据\n\n")
	renderCostTable(&b, r)
	if me != nil {
		fmt.Fprintf(&b, "\n活跃 %d 人 · 调用 %d 次\n\n", len(me.Members), memberCalls(me.Members))
	}
	renderModelTier(&b, r)

	b.WriteString("\n## 2. 各部门基础数据（对照构成，不排名）\n\n")
	renderDeptBreakdown(&b, r, me)

	if len(safetyMembers(me)) > 0 {
		b.WriteString("\n## 3. 严重异常（待核查，带部门）\n\n")
		renderStudioSafety(&b, r, me)
	}

	b.WriteString("## 4. 工作室成员整合评估图（四象限）\n\n")
	renderQuadrantScope(&b, r, me)

	b.WriteString("\n## 5. 各部门存在问题的人\n\n")
	renderStudioProblemByDept(&b, r, me)
	return b.String()
}

// --- 表格与区块 ---

func renderCostTable(b *strings.Builder, r *Report) {
	c := r.CostBreakdown
	total := c.Total()
	b.WriteString("| Token 口径 | 数量 | 费用 |\n|---|--:|--:|\n")
	fmt.Fprintf(b, "| 输入（未缓存 %d） | %d | %s |\n", c.UncachedInputTokens, c.InputTokens, fmtMoney(r, c.UncachedInputCostUSD))
	fmt.Fprintf(b, "| 缓存（写入 %d） | %d | %s |\n", c.CacheWriteTokens, c.CachedTokens, fmtMoney(r, c.CacheCostUSD))
	fmt.Fprintf(b, "| 输出（推理 %d） | %d | %s |\n", c.ReasoningTokens, c.OutputTokens, fmtMoney(r, c.OutputCostUSD))
	fmt.Fprintf(b, "| **总计（缓存不重复计入）** | **%d** | **%s** |\n\n", c.TotalTokens, fmtMoney(r, total))
	calls := r.TotalCalls
	if calls == 0 && r.MemberEvaluation != nil {
		calls = int64(memberCalls(r.MemberEvaluation.Members))
	}
	fmt.Fprintf(b, "调用次数：**%d** 次。\n\n", calls)
	rows := []struct {
		name       string
		usd        float64
		prevUSD    float64
		hasPrevVal bool
	}{
		{"总计", total, prevTotal(c), c.Prev != nil},
		{"├ 非缓存输入", c.UncachedInputCostUSD, prevVal(c, func(p CostBreakdownPrev) float64 { return p.UncachedInputCostUSD }), c.Prev != nil},
		{"├ 缓存", c.CacheCostUSD, prevVal(c, func(p CostBreakdownPrev) float64 { return p.CacheCostUSD }), c.Prev != nil},
		{"└ 输出", c.OutputCostUSD, prevVal(c, func(p CostBreakdownPrev) float64 { return p.OutputCostUSD }), c.Prev != nil},
	}
	b.WriteString("| 分量 | 费用 | 占比 | 环比 |\n|---|--:|--:|--:|\n")
	for _, row := range rows {
		share := 0.0
		if total > 0 {
			share = row.usd / total
		}
		fmt.Fprintf(b, "| %s | %s | %s | %s |\n", row.name, fmtMoney(r, row.usd), fmtPct(share), fmtDelta(r, row.usd, row.prevUSD, row.hasPrevVal))
	}
	if c.FailedRequestCostUSD > 0 {
		fmt.Fprintf(b, "| 其中失败请求 | %s | 无效消耗(标记不扣减) | — |\n", fmtMoney(r, c.FailedRequestCostUSD))
	}
}

func renderModelTier(b *strings.Builder, r *Report) {
	if len(r.CostBreakdown.ByModelTier) == 0 {
		return
	}
	b.WriteString("\n各模型费用占比：\n\n")
	b.WriteString("| 模型 | 费用 | 占比 |\n|---|--:|--:|\n")
	total := r.CostBreakdown.Total()
	for _, m := range r.CostBreakdown.ByModelTier {
		share := 0.0
		if total > 0 {
			share = m.CostUSD / total
		}
		fmt.Fprintf(b, "| %s | %s | %s |\n", m.Model, fmtMoney(r, m.CostUSD), fmtPct(share))
	}
}

func renderCostComposition(b *strings.Builder, r *Report) {
	c := r.CostBreakdown
	total := c.Total()
	if c.Prev == nil {
		fmt.Fprintf(b, "> 无上周数据，仅给绝对构成：非缓存输入 %s · 缓存 %s · 输出 %s，暂不做环比判断。\n\n",
			fmtPct(safeDiv(c.UncachedInputCostUSD, total)), fmtPct(safeDiv(c.CacheCostUSD, total)), fmtPct(safeDiv(c.OutputCostUSD, total)))
		return
	}
	fmt.Fprintf(b, "> 本周 非缓存输入 %s · 缓存 %s · 输出 %s（合计 %s）\n> 上周 非缓存输入 %s · 缓存 %s · 输出 %s（合计 %s）\n",
		fmtPct(safeDiv(c.UncachedInputCostUSD, total)), fmtPct(safeDiv(c.CacheCostUSD, total)), fmtPct(safeDiv(c.OutputCostUSD, total)), fmtMoney(r, total),
		fmtPct(safeDiv(c.Prev.UncachedInputCostUSD, prevTotal(c))), fmtPct(safeDiv(c.Prev.CacheCostUSD, prevTotal(c))), fmtPct(safeDiv(c.Prev.OutputCostUSD, prevTotal(c))), fmtMoney(r, prevTotal(c)))
	b.WriteString("\n")
}

func renderIssueCards(b *strings.Builder, r *Report) {
	byType := map[string]UsageIssue{}
	for _, u := range r.UsageIssues {
		byType[u.Type] = u
	}
	// real_session_coverage<50% → 交互类(3.2/3.3)标注疑似说明。
	lowCoverage := r.DataQuality.RealSessionCoverage < 0.5
	if lowCoverage {
		b.WriteString("> ⚠ 真实会话覆盖率 < 50%，交互类（挤牙膏 3.2 / 多任务不切会话 3.3）整类降级为**疑似**，仅供参考。\n\n")
	}
	var attestedTotal float64
	var overlap float64
	for _, t := range IssueTypeOrder {
		u, ok := byType[t]
		if !ok {
			fmt.Fprintf(b, "**%s** · `[未命中]`\n\n", mdIssueDisplayName(t))
			continue
		}
		attestedTotal += u.AttributedCostUSD
		overlap += u.OverlapCostUSD
		renderIssueCard(b, r, t, u, lowCoverage)
	}
	if overlap > 0 {
		fmt.Fprintf(b, "> **归因说明**：重叠费用 %s 已归主因，未重复计；五类归因合计 %s ≤ 总成本 %s，可对账。\n\n",
			fmtMoney(r, overlap), fmtMoney(r, attestedTotal), fmtMoney(r, r.CostBreakdown.Total()))
	}
}

func renderIssueCard(b *strings.Builder, r *Report, t string, u UsageIssue, lowCoverage bool) {
	narrative := u.Narrative
	if narrative == nil {
		built := BuildIssueNarrative(u)
		narrative = &built
	}
	fmt.Fprintf(b, "**%s** · `[%s]`\n", mdIssueDisplayName(t), mdVerdictTagName(u.Verdict))
	if u.Verdict == VerdictNotHit {
		fmt.Fprintf(b, "- %s\n", narrative.Finding)
		b.WriteString("\n")
		return
	}
	if u.Verdict == VerdictNotComputed {
		fmt.Fprintf(b, "- %s %s\n\n", narrative.Finding, narrative.Reason)
		return
	}
	fmt.Fprintf(b, "- **发现**：%s\n", narrative.Finding)
	fmt.Fprintf(b, "- **理由**：%s\n", narrative.Reason)
	if u.RequestCount > 0 {
		fmt.Fprintf(b, "- **数据**：涉及请求 %d 个，占全部请求 %s；估算多花费用 %s，涉及请求总费用 %s。\n", u.RequestCount, fmtPct(u.RequestShare), fmtMoney(r, u.AttributedCostUSD), fmtMoney(r, u.AffectedCostUSD))
	}
	if u.SessionCount > 0 {
		fmt.Fprintf(b, "- **数据**：涉及会话 %d 个，占全部会话 %s。\n", u.SessionCount, fmtPct(u.SessionShare))
	}
	if narrative.SemanticReview != "" {
		fmt.Fprintf(b, "- **模型复核**：%s\n", narrative.SemanticReview)
	}
	fmt.Fprintf(b, "- **影响**：%s\n", narrative.Impact)
	if len(u.Excluded) > 0 {
		fmt.Fprintf(b, "- 已排除：%s\n", strings.Join(ClassifyExcluded(u.Excluded), "；"))
	}
	if (t == IssueTypeToothpaste || t == IssueTypeMultiTaskSession) && lowCoverage {
		b.WriteString("- ⚠ 数据质量联动：真实会话覆盖率低，本节为**疑似**仅供参考。\n")
	}
	if narrative.Advice != "" {
		fmt.Fprintf(b, "- **建议**：%s\n", narrative.Advice)
	}
	b.WriteString("\n")
}

func renderPersonalSafety(b *strings.Builder, r *Report) {
	s := r.SafetyAnomaly
	total := r.CostBreakdown.Total()
	fmt.Fprintf(b, "> ⚠ **未备案非工作时段占比 %s**（%s / %s，超过 20%% 关注线）。独立于五种情况；仅供负责人结合备案确认，不做定责判定。\n\n",
		fmtPct(s.UnrelatedCostRatio), fmtMoney(r, s.UnrelatedCostUSD), fmtMoney(r, total))
	b.WriteString("| 项 | 值 |\n|---|---|\n")
	fmt.Fprintf(b, "| 未备案非工作时段金额 | %s |\n", fmtMoney(r, s.UnrelatedCostUSD))
	fmt.Fprintf(b, "| 未备案非工作时段占比 | %s |\n", fmtPct(s.UnrelatedCostRatio))
	if len(s.TopHours) > 0 {
		fmt.Fprintf(b, "| 主要时段 | %s |\n", strings.Join(s.TopHours, "；"))
	}
	if s.TopModel != "" {
		fmt.Fprintf(b, "| 主要模型 / Key | %s / %s |\n", s.TopModel, DisplayKey(s.TopKey))
	}
	if len(s.Excluded) > 0 {
		fmt.Fprintf(b, "| 已排除 | %s |\n", strings.Join(s.Excluded, "、"))
	}
	status := "待核查"
	if s.ReviewTag != "" && s.ReviewTag != "待核查" {
		status = fmt.Sprintf("待核查（%s）", s.ReviewTag)
	}
	fmt.Fprintf(b, "| 状态 | `%s` |\n", status)
	if s.Hint != "" {
		fmt.Fprintf(b, "| 提示 | %s |\n", s.Hint)
	}
	b.WriteString("\n")
	narrative := BuildSafetyNarrative(s)
	fmt.Fprintf(b, "- **触发原因**：%s\n", narrative.Trigger)
	fmt.Fprintf(b, "- **内容复核**：%s\n", narrative.ContentReview)
	fmt.Fprintf(b, "- **如何理解**：%s\n", narrative.Interpretation)
	fmt.Fprintf(b, "- **需要核实**：%s\n\n", narrative.Action)
	if len(s.ContentSamples) > 0 {
		b.WriteString("内容样本（脱敏）：\n")
		for _, sm := range s.ContentSamples {
			fmt.Fprintf(b, "- %s\n", sm)
		}
		b.WriteString("\n")
	}
	b.WriteString("反馈：`[ 确认正常 ] [ 需核查 ] [ 误报 ]`\n\n")
}

func renderDeptSafety(b *strings.Builder, r *Report, me *MemberEvaluation) {
	hits := safetyMembers(me)
	if len(hits) == 0 {
		b.WriteString("> 无人命中，**本节整块不出现**。\n\n")
		return
	}
	b.WriteString("> 独立于五种情况，只列未备案的非工作时段用量。**“疑似加班未备案”**表示 OpenCode 判断相关内容更像工作任务，应优先核实备案，不代表与工作无关。\n\n")
	b.WriteString("| 姓名 | 未备案时段金额 | 占个人成本 | 集中时段 | 模型 | Key | 状态 |\n|---|--:|--:|---|---|---|---|\n")
	for _, p := range hits {
		status := "待核查"
		if p.SafetyReviewTag != "" && p.SafetyReviewTag != "待核查" {
			status = fmt.Sprintf("待核查（%s）", p.SafetyReviewTag)
		}
		s := p.SafetyDetail
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s | `%s` |\n", p.Name, fmtMoney(r, s.UnrelatedCostUSD), fmtPct(s.UnrelatedCostRatio), strings.Join(s.TopHours, "；"), valueOrDash(s.TopModel), DisplayKey(s.TopKey), status)
	}
	b.WriteString("\n**为什么进入待核查**\n\n")
	for _, p := range hits {
		n := BuildSafetyNarrative(p.SafetyDetail)
		fmt.Fprintf(b, "- **%s**：%s %s %s %s\n", p.Name, n.Trigger, n.ContentReview, n.Interpretation, n.Action)
	}
	b.WriteString("\n")
}

func renderStudioSafety(b *strings.Builder, r *Report, me *MemberEvaluation) {
	hits := safetyMembers(me)
	if len(hits) == 0 {
		b.WriteString("> 无人命中，**本节不出现**。\n\n")
		return
	}
	b.WriteString("> **“疑似加班未备案”**表示 OpenCode 判断相关内容更像工作任务，应优先核实备案，不代表与工作无关。\n\n")
	b.WriteString("| 姓名 | 部门 | 未备案时段金额 | 占个人成本 | 集中时段 | 模型 | Key | 状态 |\n|---|---|--:|--:|---|---|---|---|\n")
	for _, p := range hits {
		status := "待核查"
		if p.SafetyReviewTag != "" && p.SafetyReviewTag != "待核查" {
			status = fmt.Sprintf("待核查（%s）", p.SafetyReviewTag)
		}
		s := p.SafetyDetail
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s | %s | `%s` |\n", p.Name, MemberDeptLabel(p), fmtMoney(r, s.UnrelatedCostUSD), fmtPct(s.UnrelatedCostRatio), strings.Join(s.TopHours, "；"), valueOrDash(s.TopModel), DisplayKey(s.TopKey), status)
	}
	b.WriteString("\n**为什么进入待核查**\n\n")
	for _, p := range hits {
		n := BuildSafetyNarrative(p.SafetyDetail)
		fmt.Fprintf(b, "- **%s**：%s %s %s %s\n", p.Name, n.Trigger, n.ContentReview, n.Interpretation, n.Action)
	}
	b.WriteString("\n")
}

func renderQuadrantScope(b *strings.Builder, r *Report, me *MemberEvaluation) {
	if me == nil {
		b.WriteString("> 无成员数据。\n\n")
		return
	}
	fmt.Fprintf(b, "Y = 问题消耗占比（仅五种情况，**不含严重异常**）；十字线用本层级中位（X=1.0× 成本中位倍数，Y=%s）。\n\n", fmtPct(me.IssueRatioMedian))
	// 分组展示，避免大 ASCII 图难保确定性。
	for _, q := range []string{QuadrantPriorityImprove, QuadrantCoach, QuadrantRoutine, QuadrantHighCostNormal} {
		members := filterQuadrant(me.Members, q)
		if len(members) == 0 {
			continue
		}
		names := make([]string, 0, len(members))
		for _, p := range members {
			marker := ""
			if p.SafetyAnomaly {
				marker = "⊗严重异常描边"
			} else if p.DominantIssue != IssueTypeNormal {
				marker = mdIssueDisplayName(p.DominantIssue)
			} else {
				marker = "正常"
			}
			names = append(names, fmt.Sprintf("%s(%s)", p.Name, marker))
		}
		fmt.Fprintf(b, "- **%s**：%s\n", mdQuadrantDisplayName(q), strings.Join(names, "；"))
	}
	b.WriteString("\n> `已定性·实心  疑似·虚边  严重异常⊗描边(不进Y)  正常○灰  点大小=调用次数  颜色=主导问题`\n\n")
}

func renderProblemMembers(b *strings.Builder, r *Report, problem []MemberPoint) {
	if len(problem) == 0 {
		b.WriteString("**无**，全部成员用法正常。\n\n")
		return
	}
	b.WriteString("| 姓名 | 象限 | 成本 | 问题消耗占比 | 严重异常 | 主导类型 | 归因费用 |\n|---|---|--:|--:|--:|---|--:|\n")
	for _, p := range problem {
		safety := "—"
		if p.SafetyAnomaly {
			safety = "待核查"
		}
		dom := "—"
		if p.DominantIssue != IssueTypeNormal {
			dom = mdIssueDisplayName(p.DominantIssue)
		}
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s | %s |\n", p.Name, mdQuadrantDisplayName(p.Quadrant), fmtMoney(r, p.CostUSD), fmtPct(p.IssueCostRatio), safety, dom, fmtMoney(r, p.DominantIssueCostUSD))
	}
	b.WriteString("\n")
}

func renderNormalMembers(b *strings.Builder, r *Report, normal []MemberPoint) {
	if len(normal) == 0 {
		b.WriteString("**无**。\n\n")
		return
	}
	b.WriteString("| 姓名 | 输入 Token | 缓存 Token | 输出 Token | 成本 | 调用 | 结论 |\n|---|--:|--:|--:|--:|--:|---|\n")
	for _, p := range normal {
		fmt.Fprintf(b, "| %s | %d | %d | %d | %s | %d | 用法正常 |\n", p.Name, p.TokenBreakdown.InputTokens, p.TokenBreakdown.CachedTokens, p.TokenBreakdown.OutputTokens, fmtMoney(r, p.CostUSD), p.Calls)
	}
	b.WriteString("\n")
}

func renderDeptBreakdown(b *strings.Builder, r *Report, me *MemberEvaluation) {
	if me == nil {
		b.WriteString("> 无数据。\n\n")
		return
	}
	perDept := map[string][]MemberPoint{}
	deptOrder := []string{}
	for _, p := range me.Members {
		dept := MemberDeptLabel(p)
		if _, ok := perDept[dept]; !ok {
			deptOrder = append(deptOrder, dept)
		}
		perDept[dept] = append(perDept[dept], p)
	}
	b.WriteString("| 部门 | 输入 Token | 缓存 Token | 输出 Token | 总费用 | 输入费 | 缓存费 | 输出费 | 调用 | 活跃人数 |\n|---|--:|--:|--:|--:|--:|--:|--:|--:|--:|\n")
	for _, d := range deptOrder {
		ms := perDept[d]
		var cost, unc, cache, out float64
		var inputTokens, cachedTokens, outputTokens int64
		var calls int
		for _, m := range ms {
			cost += m.CostUSD
			unc += m.CostBreakdown.UncachedInputCostUSD
			cache += m.CostBreakdown.CacheCostUSD
			out += m.CostBreakdown.OutputCostUSD
			inputTokens += m.TokenBreakdown.InputTokens
			cachedTokens += m.TokenBreakdown.CachedTokens
			outputTokens += m.TokenBreakdown.OutputTokens
			calls += m.Calls
		}
		fmt.Fprintf(b, "| %s | %d | %d | %d | %s | %s | %s | %s | %d | %d |\n", d, inputTokens, cachedTokens, outputTokens, fmtMoney(r, cost), fmtMoney(r, unc), fmtMoney(r, cache), fmtMoney(r, out), calls, len(ms))
	}
	b.WriteString("\n")
}

func renderStudioProblemByDept(b *strings.Builder, r *Report, me *MemberEvaluation) {
	if me == nil {
		b.WriteString("> 无数据。\n\n")
		return
	}
	problem, _ := splitMembers(me)
	if len(problem) == 0 {
		b.WriteString("**无**，全部成员用法正常。\n\n")
		return
	}
	perDept := map[string][]MemberPoint{}
	deptOrder := []string{}
	for _, p := range problem {
		dept := MemberDeptLabel(p)
		if _, ok := perDept[dept]; !ok {
			deptOrder = append(deptOrder, dept)
		}
		perDept[dept] = append(perDept[dept], p)
	}
	for _, d := range deptOrder {
		fmt.Fprintf(b, "**【%s】**\n\n", d)
		b.WriteString("| 姓名 | 象限 | 成本 | 问题消耗占比 | 严重异常 | 主导类型 | 归因费用 |\n|---|---|--:|--:|--:|---|--:|\n")
		for _, p := range perDept[d] {
			safety := "—"
			if p.SafetyAnomaly {
				safety = "待核查"
			}
			dom := "—"
			if p.DominantIssue != IssueTypeNormal {
				dom = mdIssueDisplayName(p.DominantIssue)
			}
			fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s | %s |\n", p.Name, mdQuadrantDisplayName(p.Quadrant), fmtMoney(r, p.CostUSD), fmtPct(p.IssueCostRatio), safety, dom, fmtMoney(r, p.DominantIssueCostUSD))
		}
		b.WriteString("\n")
	}
}

func dataQualityLine(b *strings.Builder, d DataQuality) {
	fmt.Fprintf(b, "`数据质量：真实会话 %s · 场景标签 %s · 价格目录 %s · 岗位标签 %s`\n",
		fmtPct(d.RealSessionCoverage), fmtPct(d.SceneLabelCoverage), fmtPct(d.PriceCatalogCoverage), fmtPct(d.PositionLabelCoverage))
}

// --- 成员分类与排序 ---

// splitMembers 把成员分成问题成员（安全异常待核查 或 右上/左上）与正常成员。
func splitMembers(me *MemberEvaluation) (problem, normal []MemberPoint) {
	if me == nil {
		return nil, nil
	}
	for _, p := range me.Members {
		if isProblemMember(p) {
			problem = append(problem, p)
		} else {
			normal = append(normal, p)
		}
	}
	sort.SliceStable(problem, func(i, j int) bool { return problemLess(problem[i], problem[j]) })
	sort.SliceStable(normal, func(i, j int) bool { return normal[i].Name < normal[j].Name })
	return problem, normal
}

func isProblemMember(p MemberPoint) bool {
	if p.IsProblemMember || p.SafetyAnomaly || p.Quadrant == QuadrantPriorityImprove || p.Quadrant == QuadrantCoach {
		return true
	}
	for _, issue := range p.UsageIssues {
		if issue.Verdict == VerdictConfirmed {
			return true
		}
	}
	return false
}

// problemLess 按管理优先级：严重异常待核查 → 右上优先改善 → 左上习惯辅导；再按问题占比降序、姓名升序（确定性）。
func problemLess(a, b MemberPoint) bool {
	ra := problemRank(a)
	rb := problemRank(b)
	if ra != rb {
		return ra < rb
	}
	if a.IssueCostRatio != b.IssueCostRatio {
		return a.IssueCostRatio > b.IssueCostRatio
	}
	return a.Name < b.Name
}

func problemRank(p MemberPoint) int {
	if p.SafetyAnomaly {
		return 0
	}
	switch p.Quadrant {
	case QuadrantPriorityImprove:
		return 1
	case QuadrantCoach:
		return 2
	default:
		return 3
	}
}

func safetyMembers(me *MemberEvaluation) []MemberPoint {
	if me == nil {
		return nil
	}
	var out []MemberPoint
	for _, p := range me.Members {
		if p.SafetyAnomaly {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func filterQuadrant(members []MemberPoint, q string) []MemberPoint {
	var out []MemberPoint
	for _, p := range members {
		if p.Quadrant == q {
			out = append(out, p)
		}
	}
	return out
}

func memberCalls(members []MemberPoint) int {
	var total int
	for _, p := range members {
		total += p.Calls
	}
	return total
}

// --- 格式化工具 ---

func valueOrDash(v string) string {
	if v == "" {
		return "—"
	}
	return v
}

// DisplayKey 返回适合报告展示的 Key 名称；历史快照若仍保存 UUID，则只展示尾号。
func DisplayKey(value string) string {
	v := strings.TrimSpace(value)
	if v == "" {
		return "—"
	}
	if LooksLikeUUID(v) {
		return "Key ••••" + v[len(v)-4:]
	}
	return v
}

// MemberDeptLabel 优先展示部门名；只剩 UUID 时隐藏技术标识。
func MemberDeptLabel(p MemberPoint) string {
	if name := strings.TrimSpace(p.DeptName); name != "" {
		return name
	}
	if id := strings.TrimSpace(p.DeptID); id != "" && !LooksLikeUUID(id) {
		return id
	}
	return "—"
}

func LooksLikeUUID(v string) bool {
	if len(v) != 36 {
		return false
	}
	for i, ch := range v {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if ch != '-' {
				return false
			}
			continue
		}
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')) {
			return false
		}
	}
	return true
}

func fmtMoney(r *Report, usd float64) string {
	sym := "$"
	if r.Meta.Currency.Report == "CNY" {
		sym = "¥"
	}
	if r.Meta.Currency.Report == "CNY" {
		usd = usd * r.Meta.Currency.USDCNYRate
	}
	return formatThousands(sym, usd)
}

func formatThousands(sym string, v float64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	sv := float64(int64(v*100+0.5)) / 100
	intPart := int64(sv)
	frac := int64((sv-float64(intPart))*100 + 0.5)
	if frac == 100 {
		intPart++
		frac = 0
	}
	str := fmt.Sprintf("%d", intPart)
	var grouped bytes.Buffer
	for i, c := range str {
		if i > 0 && (len(str)-i)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteByte(byte(c))
	}
	if neg {
		return fmt.Sprintf("-%s%s.%02d", sym, grouped.String(), frac)
	}
	return fmt.Sprintf("%s%s.%02d", sym, grouped.String(), frac)
}

func fmtPct(v float64) string {
	return fmt.Sprintf("%.1f%%", v*100)
}

func fmtDelta(r *Report, cur, prev float64, hasPrev bool) string {
	if !hasPrev {
		return "无上期"
	}
	if prev <= 0 {
		return "新增"
	}
	d := (cur - prev) / prev
	if d > 0 {
		return fmt.Sprintf("▲ +%s", fmtPct(d))
	}
	if d < 0 {
		return fmt.Sprintf("▼ %s", fmtPct(-d))
	}
	return "持平"
}

func fmtMoneyDelta(r *Report, d float64) string {
	if d >= 0 {
		return "+" + fmtMoney(r, d)
	}
	return "-" + fmtMoney(r, -d)
}

func safeDiv(a, b float64) float64 {
	if b <= 0 {
		return 0
	}
	return a / b
}

func prevTotal(c CostBreakdown) float64 {
	if c.Prev == nil {
		return 0
	}
	return c.Prev.UncachedInputCostUSD + c.Prev.CacheCostUSD + c.Prev.OutputCostUSD
}

func prevVal(c CostBreakdown, f func(CostBreakdownPrev) float64) float64 {
	if c.Prev == nil {
		return 0
	}
	return f(*c.Prev)
}

func endDate(r *Report) string {
	return r.Meta.Period.To
}

func periodLabel(r *Report) string {
	if r.Meta.Period.IsoWeek != "" {
		return r.Meta.Period.IsoWeek
	}
	return fmt.Sprintf("%s~%s", r.Meta.Period.From, r.Meta.Period.To)
}

func evidenceSummary(m map[string]any) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, m[k]))
	}
	return strings.Join(parts, "；")
}

// EvidenceReason 从 Evidence 中取“为什么”说明，并把历史快照里的内部判定术语翻译成人话。
// Evidence 其余键值是复核用的机器细节，不在读者交付物中直接展示。
func EvidenceReason(m map[string]any) string {
	if m == nil {
		return ""
	}
	raw, ok := m["reason_suspected"].(string)
	if !ok || raw == "" {
		return ""
	}
	switch {
	case strings.Contains(raw, "reset_flag") || strings.Contains(raw, "任务意图语义标签"):
		return "系统发现长会话中输入量持续增加，部分内容像是在开始另一项任务；仅靠调用记录还不足以确认，需结合会话内容复核。"
	case strings.Contains(raw, "task_kind") || strings.Contains(raw, "无意图标签"):
		return "系统发现长会话里有多次很短的补充输入；仅靠调用记录还不足以确认这些输入是否都在处理同一件事。"
	case strings.Contains(raw, "scene") || strings.Contains(raw, "场景标签"):
		return "系统发现请求规模较小但使用了高阶模型；由于缺少明确的任务场景信息，目前只作为待确认线索。"
	case strings.Contains(raw, "P1") || strings.Contains(raw, "CSA") || strings.Contains(raw, "Evidence") || strings.Contains(raw, "suspected"):
		return "当前基础数据只能识别出异常模式，仍需结合具体任务内容进一步确认。"
	}
	return raw
}

// SemanticNote 从 Evidence 取语义复核结论的读者可读一句（无语义层时不输出）。
func SemanticNote(m map[string]any) string {
	if m == nil {
		return ""
	}
	raw, ok := m["semantic"].(map[string]any)
	if !ok || raw == nil {
		return ""
	}
	affirmed := toF(raw["affirmed"])
	refuted := toF(raw["refuted"])
	uncertain := toF(raw["uncertain"])
	if affirmed > 0 {
		return fmt.Sprintf("模型复核了相关会话：%d 个支持该判断，%d 个不支持，%d 个信息不足。报告仅保留支持或待确认的部分。", int(affirmed), int(refuted), int(uncertain))
	}
	if refuted > 0 && uncertain == 0 {
		return fmt.Sprintf("模型复核后，%d 个候选均不支持该判断，相应费用已排除。", int(refuted))
	}
	if uncertain > 0 || refuted > 0 {
		return fmt.Sprintf("模型复核后，%d 个候选不支持该判断，另有 %d 个信息不足；信息不足部分仍标为待确认。", int(refuted), int(uncertain))
	}
	return "模型复核暂未形成有效结论，当前结果仍需进一步确认。"
}

// SemanticContentSamples 从 Evidence 取语义复核读到的内容样本（负责人复核依据；截断、最多 6 条）。
func SemanticContentSamples(m map[string]any) []string {
	if m == nil {
		return nil
	}
	raw, ok := m["semantic"].(map[string]any)
	if !ok || raw == nil {
		return nil
	}
	switch v := raw["content_sample"].(type) {
	case []string:
		return v
	case []any:
		var out []string
		for _, e := range v {
			if str, ok := e.(string); ok {
				out = append(out, str)
			}
		}
		return out
	}
	return nil
}

// toF 兼容 Evidence 里 int / int64 / float64 数值（Go 直设 vs JSON 反序列化）。
func toF(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case int32:
		return float64(n)
	}
	return 0
}

// ClassifyExcluded 把 "原因前缀:sessionID" 归类为读者可读的排除原因计数，隐藏具体 ID。
func ClassifyExcluded(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	counts := map[string]int{}
	order := []string{}
	label := map[string]string{
		"batch_session":             "推断为批量/自动会话",
		"batch":                     "推断为批量/自动会话",
		"not_single_turn":           "非单轮会话",
		"single_turn":               "单轮会话",
		"reset":                     "会话重置",
		"tier_lookup_error":         "模型档位查询失败",
		"light_alt_error":           "轻量替代查询失败",
		"no_light_alt":              "无轻量替代模型",
		"position_baseline_missing": "岗位基线缺失",
		"one_off_light_long":        "单次长输入轻量调用",
		"task_kind_unavailable":     "任务类型不可用",
		"nil":                       "无判定对象",
	}
	for _, it := range items {
		kind := it
		if i := strings.IndexByte(it, ':'); i >= 0 {
			kind = it[:i]
		}
		if counts[kind] == 0 {
			order = append(order, kind)
		}
		counts[kind]++
	}
	out := make([]string, 0, len(order))
	for _, k := range order {
		l := label[k]
		if l == "" {
			l = k
		}
		out = append(out, fmt.Sprintf("%s %d 项", l, counts[k]))
	}
	return out
}

func mdVerdictTagName(v string) string {
	if s, ok := mdVerdictTag[v]; ok {
		return s
	}
	return v
}

func mdIssueDisplayName(t string) string {
	if s, ok := mdIssueDisplay[t]; ok {
		return s
	}
	return t
}

func mdQuadrantDisplayName(q string) string {
	if s, ok := mdQuadrantDisplay[q]; ok {
		return s
	}
	return q
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// SemanticAffirmed 报告该 issue 是否经语义层确认过（Evidence.semantic.affirmed > 0）。
func SemanticAffirmed(m map[string]any) bool {
	if m == nil {
		return false
	}
	raw, ok := m["semantic"].(map[string]any)
	if !ok || raw == nil {
		return false
	}
	return toF(raw["affirmed"]) > 0
}
