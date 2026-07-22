// report-gen 的 HTML 可视化输出：把 *report.Report 渲染为自包含 HTML（内嵌 CSS 与 SVG，无外部依赖）。
package main

import (
	"fmt"
	"html"
	"math"
	"sort"
	"strings"

	"github.com/example/llm-gateway/internal/observability/report/v2"
)

// renderHTML 生成自包含 HTML 报告页面（完整适配个人、部门、工作室三级报告定稿模板）。
func renderHTML(r *report.Report) string {
	if r == nil {
		return ""
	}
	switch r.Meta.Level {
	case report.LevelDepartment:
		return renderDepartmentHTML(r)
	case report.LevelStudio:
		return renderStudioHTML(r)
	case report.LevelPersonal:
		return renderPersonalHTML(r)
	default:
		return renderPersonalHTML(r)
	}
}

// -----------------------------------------------------------------------------
// 1. 个人报告 HTML 渲染
// -----------------------------------------------------------------------------

func renderPersonalHTML(r *report.Report) string {
	var b strings.Builder
	writeHTMLDocumentHead(&b, r)

	period := r.Meta.Period
	title := fmt.Sprintf("个人报告 · %s · %s", r.Meta.Scope.Name, period.IsoWeek)
	if period.IsoWeek == "" {
		title = fmt.Sprintf("个人报告 · %s · %s~%s", r.Meta.Scope.Name, period.From, period.To)
	}
	writePageHeader(&b, r, title)
	writePageNav(&b)

	// 首屏只保留领导最关心的结论、需核查、核心数据和下一步行动。
	writeConclusion(&b, r.Conclusion)
	writePersonalSafety(&b, r)
	writeCost(&b, r)
	writePersonalIssues(&b, r)
	writePersonalActionCard(&b, personalActions(r))

	// 数据口径默认折叠；面向人的效率解释保持直接可见。
	writeHTML(&b, "<details class=\"technical-section\"><summary>查看数据质量与环比明细</summary>")
	writeDataQuality(&b, r.DataQuality)
	writePrev(&b, r.CostBreakdown)
	writeHTML(&b, "</details>")

	writePageFooter(&b)
	return b.String()
}

func personalActions(r *report.Report) []string {
	var actions []string
	totalCost := r.CostBreakdown.Total()

	runawayCount := 0
	wastedCost := 0.0
	multiTaskWasted := 0.0
	toothpasteCount := 0
	hasRunawayIssue := false

	for _, u := range r.UsageIssues {
		if u.Verdict != report.VerdictConfirmed && u.Verdict != report.VerdictSuspected {
			continue
		}
		wastedCost += u.AttributedCostUSD
		switch u.Type {
		case report.IssueTypeMultiTaskSession, "multi_task_single_session":
			multiTaskWasted += u.AttributedCostUSD
		case report.IssueTypeToothpaste:
			toothpasteCount++
		case report.IssueTypeOutputRunaway:
			hasRunawayIssue = true
			if u.Verdict == report.VerdictConfirmed {
				runawayCount++
			}
		}
	}

	wastedRatio := 0.0
	if totalCost > 0 {
		wastedRatio = (wastedCost / totalCost) * 100.0
	} else if r.Conclusion.IssueCostRatio > 0 {
		wastedRatio = r.Conclusion.IssueCostRatio * 100.0
	}

	if multiTaskWasted > 0 || r.Conclusion.DominantIssue == "multi_task_session" || r.Conclusion.DominantIssue == "multi_task_single_session" {
		actions = append(actions, "在新功能开发或编写独立测试用例时，顺手点击一次 New Chat 新开窗口，避免跨任务历史上下文冗余累积。")
	}
	if runawayCount > 0 || hasRunawayIssue {
		actions = append(actions, "在提示词中明确输出边界（如仅返回核心 diff 代码或目标函数），避免单次返回超长全量冗余文件。")
	}
	if toothpasteCount > 0 {
		actions = append(actions, "提问时尽量一次性附带完整需求背景与错误堆栈，减少多轮追加与来回试错成本。")
	}
	if wastedRatio <= 10.0 {
		actions = append(actions, "继续保持前缀缓存习惯；下周在新任务开启时顺手点击一次 New Chat。")
	}
	if len(actions) == 0 {
		actions = append(actions, "继续保持当前稳健的使用习惯，多轮长会话切换任务时及时切分 session 即可。")
	}

	return actions
}

func writePersonalActionCard(b *strings.Builder, actions []string) {
	if len(actions) == 0 {
		return
	}
	writeHTML(b, "<section id=\"next-actions\" class=\"card action-card\">")
	writeHTML(b, "<div class=\"card-title-row\" style=\"display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px;\">")
	writeHTML(b, "<h2 style=\"margin: 0; font-size: 16px;\">🎯 次周专属可执行行动卡</h2>")
	writeHTML(b, "<span style=\"font-size: 11px; padding: 2px 8px; border-radius: 9999px; background: #e0f2fe; color: #0369a1; font-weight: 600;\">Action Checklist</span>")
	writeHTML(b, "</div>")
	writeHTML(b, "<div class=\"action-item-list\">")
	for i, act := range actions {
		writeHTML(b, "<div class=\"action-item-row\">")
		writeHTML(b, "<span class=\"action-badge\">行动 %d</span>", i+1)
		writeHTML(b, "<span class=\"action-text\">%s</span>", html.EscapeString(act))
		writeHTML(b, "</div>")
	}
	writeHTML(b, "</div>")
	writeHTML(b, "</section>")
}

// -----------------------------------------------------------------------------
// 2. 部门报告 HTML 渲染
// -----------------------------------------------------------------------------

func renderDepartmentHTML(r *report.Report) string {
	var b strings.Builder
	writeHTMLDocumentHead(&b, r)

	period := r.Meta.Period
	title := fmt.Sprintf("部门报告 · %s · %s", r.Meta.Scope.Name, period.IsoWeek)
	if period.IsoWeek == "" {
		title = fmt.Sprintf("部门报告 · %s · %s~%s", r.Meta.Scope.Name, period.From, period.To)
	}
	writePageHeader(&b, r, title)
	writePageNav(&b)

	// 首屏领导摘要。
	writeConclusion(&b, r.Conclusion)
	writeDepartmentSafety(&b, r)
	writeDepartmentBaseData(&b, r)
	writeManagementActions(&b, r)

	// 技术说明默认折叠。
	writeHTML(&b, "<details class=\"technical-section\"><summary>展开四象限、分群和数据质量</summary>")
	writeDataQuality(&b, r.DataQuality)
	writeDepartmentQuadrantAndClusters(&b, r)
	writeHTML(&b, "</details>")

	// 4. 存在问题的人（按管理优先级排序，展开卡）。
	writeDepartmentProblemMembers(&b, r)

	// 5. 正常使用的人（汇总表）。
	writeDepartmentNormalMembers(&b, r)

	writePageFooter(&b)
	return b.String()
}

// -----------------------------------------------------------------------------
// 3. 工作室报告 HTML 渲染
// -----------------------------------------------------------------------------

func renderStudioHTML(r *report.Report) string {
	var b strings.Builder
	writeHTMLDocumentHead(&b, r)

	period := r.Meta.Period
	title := fmt.Sprintf("工作室报告 · %s · %s", r.Meta.Scope.Name, period.IsoWeek)
	if period.IsoWeek == "" {
		title = fmt.Sprintf("工作室报告 · %s · %s~%s", r.Meta.Scope.Name, period.From, period.To)
	}
	writePageHeader(&b, r, title)
	writePageNav(&b)

	// 首屏领导摘要。
	writeConclusion(&b, r.Conclusion)
	writeStudioSafety(&b, r)
	writeStudioBaseData(&b, r)
	writeManagementActions(&b, r)

	// 技术说明默认折叠。
	writeHTML(&b, "<details class=\"technical-section\"><summary>展开四象限、分群和数据质量</summary>")
	writeDataQuality(&b, r.DataQuality)
	writeStudioQuadrantAndClusters(&b, r)
	writeHTML(&b, "</details>")

	// 各部门基础数据（对照构成，不排名）。
	writeStudioDeptBreakdown(&b, r)

	// 5. 各部门存在问题的人（分组卡片与展开明细）。
	writeStudioProblemByDept(&b, r)

	// §5.3：正常使用的人不上工作室报告（已在各部门基础数据和全员图里能看见位置），
	// 故此处不再单独渲染「正常使用的人」一节。

	writePageFooter(&b)
	return b.String()
}

// -----------------------------------------------------------------------------
// 通用头部、数据质量、结论与基础数据组件
// -----------------------------------------------------------------------------

func writeHTMLDocumentHead(b *strings.Builder, r *report.Report) {
	b.WriteString("<!DOCTYPE html>\n<html lang=\"zh-CN\">\n<head>\n<meta charset=\"utf-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	b.WriteString("<title>")
	b.WriteString(html.EscapeString(r.Meta.Scope.Name))
	b.WriteString(" · 用量报告")
	b.WriteString("</title>\n<style>\n")
	b.WriteString(cssStyles)
	b.WriteString("</style>\n</head>\n<body id=\"top\">\n<div class=\"report-wrapper\">\n")
}

func writePageHeader(b *strings.Builder, r *report.Report, title string) {
	esc := html.EscapeString
	scopeName := r.Meta.Scope.Name
	if scopeName == "" {
		scopeName = r.Meta.Scope.ID
	}
	subPeriod := r.Meta.Period.IsoWeek
	if subPeriod == "" {
		subPeriod = r.Meta.Period.From + " ~ " + r.Meta.Period.To
	}

	writeHTML(b, "<header class=\"page-header\">")
	writeHTML(b, "<div class=\"header-breadcrumb\">用量分析 &nbsp;/&nbsp; %s &nbsp;/&nbsp; %s</div>", esc(scopeName), esc(subPeriod))
	writeHTML(b, "<div class=\"header-title-row\">")
	writeHTML(b, "<h1>%s</h1>", esc(title))
	writeHTML(b, "</div>")
	writeHTML(b, "<div class=\"header-badges\">")
	writeHTML(b, "<span class=\"badge-pill\">📋 规则版本 %s</span>", esc(r.Meta.RuleVersion))
	writeHTML(b, "<span class=\"badge-pill\">📅 周期 %s 至 %s</span>", esc(r.Meta.Period.From), esc(r.Meta.Period.To))
	writeHTML(b, "<span class=\"badge-pill\">💵 结算币种 %s</span>", esc(r.Meta.Currency.Report))
	if r.MemberEvaluation != nil && len(r.MemberEvaluation.Members) > 0 {
		writeHTML(b, "<span class=\"badge-pill\">👥 活跃成员 %d 人</span>", len(r.MemberEvaluation.Members))
		writeHTML(b, "<span class=\"badge-pill\">⚡ 总调用 %s 次</span>", formatInt(memberCalls(r.MemberEvaluation.Members)))
	}
	writeHTML(b, "</div>")
	writeHTML(b, "</header>\n")
}

func writeManagementActions(b *strings.Builder, r *report.Report) {
	problem, _ := splitMembers(r.MemberEvaluation)
	writeHTML(b, "<section id=\"next-actions\" class=\"card action-card\"><h2>下一步行动</h2>")
	if len(problem) == 0 {
		writeHTML(b, "<p>保持当前使用方式，下周继续观察趋势。</p>")
	} else {
		writeHTML(b, "<p>先核查待确认事项，再针对 %d 名问题成员安排用法辅导；技术证据见下方折叠区。</p>", len(problem))
	}
	writeHTML(b, "</section>")
}

func writePageNav(b *strings.Builder) {
	writeHTML(b, "<nav class=\"page-nav\" aria-label=\"页内导航\"><a href=\"#conclusion\">本周结论</a><a href=\"#safety-review\">需核查</a><a href=\"#core-data\">核心数据</a><a href=\"#next-actions\">下一步行动</a><button type=\"button\" id=\"toggle-details\">展开全部明细</button></nav>")
}

func writePageFooter(b *strings.Builder) {
	writeHTML(b, "<a class=\"back-top\" href=\"#top\">返回顶部</a>")
	writeHTML(b, "<footer>说明：失败请求费用已包含在三类费用中，仅作为无效消耗标记；异常只用于提醒核查，不能直接定责。</footer>\n")
	b.WriteString(`<script>(function(){
var x=false,b=document.getElementById('toggle-details');
if(b)b.addEventListener('click',function(){x=!x;document.querySelectorAll('details').forEach(function(d){d.open=x});b.textContent=x?'收起全部明细':'展开全部明细'});
document.querySelectorAll('.quadrant-directory-row').forEach(function(row){
  var point=document.getElementById(row.dataset.memberRef||'');if(!point)return;
  function on(){row.classList.add('is-linked');point.classList.add('is-linked')}
  function off(){row.classList.remove('is-linked');point.classList.remove('is-linked')}
  row.addEventListener('mouseenter',on);row.addEventListener('mouseleave',off);row.addEventListener('focusin',on);row.addEventListener('focusout',off);
  point.addEventListener('mouseenter',on);point.addEventListener('mouseleave',off);point.addEventListener('focus',on);point.addEventListener('blur',off);
});
})();</script>`)
	b.WriteString("</div>\n</body>\n</html>\n")
}

func writeDataQuality(b *strings.Builder, dq report.DataQuality) {
	writeHTML(b, "<section class=\"dq-section\"><h2>数据质量</h2><div class=\"dqgrid\">")
	writeDQCell(b, "真实会话", dq.RealSessionCoverage)
	writeDQCell(b, "场景标签", dq.SceneLabelCoverage)
	writeDQCell(b, "价格目录", dq.PriceCatalogCoverage)
	writeDQCell(b, "岗位标签", dq.PositionLabelCoverage)
	writeHTML(b, "</div>")
	if dq.SceneLabelCoverage == 0 && dq.PriceCatalogCoverage == 0 && dq.PositionLabelCoverage == 0 {
		writeHTML(b, "<p class=\"muted\" style=\"margin-top:10px;\">部分标签还没接入。本报告先按已有数据分析，缺失项不会强行下结论。</p>")
	}
	writeHTML(b, "</section>\n")
}

func writeDQCell(b *strings.Builder, name string, v float64) {
	pct := v * 100
	color := "ok"
	if v < 0.5 {
		color = "warn"
	}
	if v == 0 {
		color = "bad"
	}
	writeHTML(b, "<div class=dqcell><div class=dqname>%s</div><div class=bar><div class=\"fill %s\" style=\"width:%.1f%%\"></div></div><div class=dqval>%.0f%%</div></div>",
		html.EscapeString(name), color, pct, pct)
}

func writeConclusion(b *strings.Builder, c report.Conclusion) {
	writeHTML(b, "<section id=\"conclusion\" class=\"conclusion\"><h2>0. 本周结论</h2><p class=\"conclusion-text\">%s</p>", html.EscapeString(c.Note))
	if c.IssueCostRatio > 0 {
		writeHTML(b, "<div class=\"meta\">五类问题消耗占比：<strong>%.1f%%</strong> &nbsp;·&nbsp; 主导问题：<span class=\"pill-tag\">%s</span></div>",
			c.IssueCostRatio*100, issueDisplayName(c.DominantIssue))
	}
	writeHTML(b, "</section>\n")
}

func writeTokenSummary(b *strings.Builder, r *report.Report) {
	cb := r.CostBreakdown
	writeHTML(b, "<div class=\"table-scroll\"><table class=\"num compact token-table\">")
	writeHTML(b, "<tr><th>口径</th><th>Token 数量</th><th>费用</th></tr>")
	writeHTML(b, "<tr><td>输入 Token（其中未缓存 %s）</td><td>%s</td><td class=money>%s</td></tr>", formatInt64(cb.UncachedInputTokens), formatInt64(cb.InputTokens), moneyWithReport(r, cb.UncachedInputCostUSD))
	writeHTML(b, "<tr><td>缓存 Token（写入 %s）</td><td>%s</td><td class=money>%s</td></tr>", formatInt64(cb.CacheWriteTokens), formatInt64(cb.CachedTokens), moneyWithReport(r, cb.CacheCostUSD))
	writeHTML(b, "<tr><td>输出 Token（其中推理 %s）</td><td>%s</td><td class=money>%s</td></tr>", formatInt64(cb.ReasoningTokens), formatInt64(cb.OutputTokens), moneyWithReport(r, cb.OutputCostUSD))
	writeHTML(b, "<tr><td><strong>总计（缓存不重复计入）</strong></td><td><strong>%s</strong></td><td class=money><strong>%s</strong></td></tr>", formatInt64(cb.TotalTokens), moneyWithReport(r, cb.Total()))
	writeHTML(b, "</table></div>")
	calls := r.TotalCalls
	if calls == 0 && r.MemberEvaluation != nil {
		calls = int64(memberCalls(r.MemberEvaluation.Members))
	}
	writeHTML(b, "<p class=muted>调用次数：<strong>%s</strong> 次。缓存 Token 是输入 Token 的组成，不重复计入总量。</p>", formatInt64(calls))
}

func writeCost(b *strings.Builder, r *report.Report) {
	cb := r.CostBreakdown
	total := cb.Total()
	delta := deltaPct(prevTotal(cb), total)
	deltaStr := "—"
	deltaClass := "muted"
	if delta < 0 {
		deltaStr = fmt.Sprintf("▼ %.1f%% 环比", -delta*100)
		deltaClass = "down"
	} else if delta > 0 {
		deltaStr = fmt.Sprintf("▲ +%.1f%% 环比", delta*100)
		deltaClass = "up"
	}

	writeHTML(b, "<section id=\"core-data\"><h2>1. 本周基础数据</h2>")
	writeTokenSummary(b, r)

	// 4-Card KPI Grid
	writeHTML(b, "<div class=\"kpi-grid\">")
	writeHTML(b, "<div class=\"kpi-card highlight\"><div class=\"kpi-label\">个人总成本</div><div class=\"kpi-val\">%s</div><div class=\"kpi-sub\"><span class=\"kpi-trend %s\">%s</span> 本周综合总费用</div></div>",
		moneyWithReport(r, total), deltaClass, deltaStr)
	writeHTML(b, "<div class=\"kpi-card\"><div class=\"kpi-label\">缓存读取 (Cache)</div><div class=\"kpi-val\">%s</div><div class=\"kpi-sub\"><span class=\"kpi-pill-badge\">占比 %.1f%%</span> 复用前缀，节约开支</div></div>",
		moneyWithReport(r, cb.CacheCostUSD), share(cb.CacheCostUSD, total)*100)
	writeHTML(b, "<div class=\"kpi-card\"><div class=\"kpi-label\">非缓存输入 (Input)</div><div class=\"kpi-val\">%s</div><div class=\"kpi-sub\"><span class=\"kpi-pill-badge\">占比 %.1f%%</span> 新会话与首轮提示词</div></div>",
		moneyWithReport(r, cb.UncachedInputCostUSD), share(cb.UncachedInputCostUSD, total)*100)
	writeHTML(b, "<div class=\"kpi-card\"><div class=\"kpi-label\">生成输出 (Output)</div><div class=\"kpi-val\">%s</div><div class=\"kpi-sub\"><span class=\"kpi-pill-badge\">占比 %.1f%%</span> 模型答复与思考生成</div></div>",
		moneyWithReport(r, cb.OutputCostUSD), share(cb.OutputCostUSD, total)*100)
	writeHTML(b, "</div>")

	writeHTML(b, "<table class=\"num\">")
	writeHTML(b, "<tr><th>分量</th><th>费用</th><th>占比</th><th>环比</th></tr>")
	writeCostRow(b, r, "总计", total, 1.0, delta)
	writeCostRow(b, r, "├ 非缓存输入", cb.UncachedInputCostUSD, share(cb.UncachedInputCostUSD, total), deltaPct(prevOf(cb).UncachedInputCostUSD, cb.UncachedInputCostUSD))
	writeCostRow(b, r, "├ 缓存", cb.CacheCostUSD, share(cb.CacheCostUSD, total), deltaPct(prevOf(cb).CacheCostUSD, cb.CacheCostUSD))
	writeCostRow(b, r, "└ 输出", cb.OutputCostUSD, share(cb.OutputCostUSD, total), deltaPct(prevOf(cb).OutputCostUSD, cb.OutputCostUSD))
	if cb.FailedRequestCostUSD > 0 {
		writeHTML(b, "<tr><td>无效消耗（失败请求）</td><td class=\"money\">%s</td><td>标记不扣减</td><td>—</td></tr>", moneyWithReport(r, cb.FailedRequestCostUSD))
	}
	writeHTML(b, "</table>")
	writeModelTier(b, r)
	writeHTML(b, "</section>\n")
}

func writeCostRow(b *strings.Builder, r *report.Report, name string, val, pct, delta float64) {
	up := delta > 0
	down := delta < 0
	arrow := "—"
	cls := ""
	if pct > 0.15 {
		cls = " warm"
	}
	if up {
		arrow = fmt.Sprintf("▲ +%.1f%%", delta*100)
		cls = " up"
	} else if down {
		arrow = fmt.Sprintf("▼ %.1f%%", -delta*100)
		cls = " down"
	}
	writeHTML(b, "<tr><td>%s</td><td class=money>%s</td><td%s>%.1f%%</td><td class=%s>%s</td></tr>",
		html.EscapeString(name), moneyWithReport(r, val), cls, pct*100, cls, arrow)
}

func writeModelTier(b *strings.Builder, r *report.Report) {
	cb := r.CostBreakdown
	if len(cb.ByModelTier) == 0 {
		return
	}
	total := cb.Total()
	writeHTML(b, "<div class=\"model-tier-block\"><h3>各模型费用占比</h3>")
	models := append([]report.ModelTierCost(nil), cb.ByModelTier...)
	sort.Slice(models, func(i, j int) bool { return models[i].CostUSD > models[j].CostUSD })
	for _, m := range models {
		if m.CostUSD <= 0 {
			continue
		}
		pct := 0.0
		if total > 0 {
			pct = m.CostUSD / total
		}
		tierMarkup := ""
		if m.Tier != "" && m.Tier != "unknown" {
			tierMarkup = " <span class=tier>" + html.EscapeString(m.Tier) + "</span>"
		}
		writeHTML(b, "<div class=modelrow><div class=mname>%s%s</div><div class=bar><div class=\"fill model\" style=\"width:%.1f%%\"></div></div><div class=mval>%s · %.1f%%</div></div>",
			escOrDash(m.Model), tierMarkup, pct*100, moneyWithReport(r, m.CostUSD), pct*100)
	}
	writeHTML(b, "</div>\n")
}

// -----------------------------------------------------------------------------
// 部门报告专属区块：基础数据、严重异常、四象限/分群、问题成员、正常成员
// -----------------------------------------------------------------------------

func writeDepartmentBaseData(b *strings.Builder, r *report.Report) {
	me := r.MemberEvaluation
	cb := r.CostBreakdown
	total := cb.Total()

	writeHTML(b, "<section id=\"core-data\"><h2>1. 部门本周基础数据</h2>")
	writeTokenSummary(b, r)
	if me != nil {
		totalCalls := memberCalls(me.Members)
		delta := deltaPct(prevTotal(cb), total)
		deltaStr := "—"
		deltaClass := "muted"
		if delta < 0 {
			deltaStr = fmt.Sprintf("▼ %.1f%% 环比", -delta*100)
			deltaClass = "down"
		} else if delta > 0 {
			deltaStr = fmt.Sprintf("▲ +%.1f%% 环比", delta*100)
			deltaClass = "up"
		}
		avgCost := 0.0
		if len(me.Members) > 0 {
			avgCost = total / float64(len(me.Members))
		}

		writeHTML(b, "<div class=\"kpi-grid\">")
		writeHTML(b, "<div class=\"kpi-card highlight\"><div class=\"kpi-label\">部门总成本</div><div class=\"kpi-val\">%s</div><div class=\"kpi-sub\"><span class=\"kpi-trend %s\">%s</span> 人均 %s · %d 人活跃</div></div>",
			moneyWithReport(r, total), deltaClass, deltaStr, moneyWithReport(r, avgCost), len(me.Members))
		writeHTML(b, "<div class=\"kpi-card\"><div class=\"kpi-label\">缓存读取 (Cache)</div><div class=\"kpi-val\">%s</div><div class=\"kpi-sub\"><span class=\"kpi-pill-badge\">占比 %.1f%%</span> 节省主力，命中良好</div></div>",
			moneyWithReport(r, cb.CacheCostUSD), share(cb.CacheCostUSD, total)*100)
		writeHTML(b, "<div class=\"kpi-card\"><div class=\"kpi-label\">非缓存输入 (Input)</div><div class=\"kpi-val\">%s</div><div class=\"kpi-sub\"><span class=\"kpi-pill-badge\">占比 %.1f%%</span> 历史前缀与新输入</div></div>",
			moneyWithReport(r, cb.UncachedInputCostUSD), share(cb.UncachedInputCostUSD, total)*100)
		writeHTML(b, "<div class=\"kpi-card\"><div class=\"kpi-label\">生成输出 (Output)</div><div class=\"kpi-val\">%s</div><div class=\"kpi-sub\"><span class=\"kpi-pill-badge\">占比 %.1f%%</span> 模型答复与思考生成</div></div>",
			moneyWithReport(r, cb.OutputCostUSD), share(cb.OutputCostUSD, total)*100)
		writeHTML(b, "</div>")

		writeHTML(b, "<div class=\"stat-summary-bar\">部门活跃 <strong>%d</strong> 人 &nbsp;·&nbsp; 总调用 <strong>%s</strong> 次 &nbsp;·&nbsp; 总成本 <strong>%s</strong></div>",
			len(me.Members), formatInt(totalCalls), moneyWithReport(r, total))
	}

	writeHTML(b, "<table class=\"num\">")
	writeHTML(b, "<tr><th>分量</th><th>费用</th><th>占比</th><th>环比</th></tr>")
	writeCostRow(b, r, "总计", total, 1.0, deltaPct(prevTotal(cb), total))
	writeCostRow(b, r, "├ 非缓存输入", cb.UncachedInputCostUSD, share(cb.UncachedInputCostUSD, total), deltaPct(prevOf(cb).UncachedInputCostUSD, cb.UncachedInputCostUSD))
	writeCostRow(b, r, "├ 缓存", cb.CacheCostUSD, share(cb.CacheCostUSD, total), deltaPct(prevOf(cb).CacheCostUSD, cb.CacheCostUSD))
	writeCostRow(b, r, "└ 输出", cb.OutputCostUSD, share(cb.OutputCostUSD, total), deltaPct(prevOf(cb).OutputCostUSD, cb.OutputCostUSD))
	if cb.FailedRequestCostUSD > 0 {
		writeHTML(b, "<tr><td>无效消耗（失败请求）</td><td class=\"money\">%s</td><td>标记不扣减</td><td>—</td></tr>", moneyWithReport(r, cb.FailedRequestCostUSD))
	}
	writeHTML(b, "</table>")

	writeModelTier(b, r)
	writeHTML(b, "</section>\n")
}

func writeDepartmentSafety(b *strings.Builder, r *report.Report) {
	me := r.MemberEvaluation
	hits := safetyMembers(me)
	if len(hits) == 0 {
		return
	}
	writeHTML(b, "<section id=\"safety-review\" class=safety><h2>2. 严重异常（待核查）</h2>")
	{
		writeHTML(b, "<p class=warm><strong>本周有 %d 人需要进一步核查。</strong>这里只是提醒，请先确认实际情况，不能直接定责。</p>", len(hits))
		writeHTML(b, "<p class=\"safety-definition\"><strong>“疑似加班未备案”是什么意思：</strong>该成员的未备案非工作时段费用超过个人成本的 20%%，同时 OpenCode 抽查内容后认为更像工作任务。因此应优先核实加班、值班或批处理备案，不代表与工作无关。</p>")
		writeHTML(b, "<div class=\"table-scroll\"><table class=\"num compact\">")
		writeHTML(b, "<tr><th>姓名</th><th>岗位</th><th>未备案时段金额</th><th>占个人成本</th><th>集中时段</th><th>模型</th><th>Key</th><th>状态</th></tr>")
		for _, m := range hits {
			status := "待核查"
			if m.SafetyReviewTag != "" && m.SafetyReviewTag != "待核查" {
				status = fmt.Sprintf("待核查（%s）", m.SafetyReviewTag)
			}
			d := m.SafetyDetail
			if d.ReviewTag == "" {
				d.ReviewTag = m.SafetyReviewTag
			}
			if d.Hint == "" {
				d.Hint = m.SafetyHint
			}
			writeHTML(b, "<tr><td><strong>%s</strong></td><td>%s</td><td class=money>%s</td><td>%.1f%%</td><td>%s</td><td>%s</td><td>%s</td><td><span class=\"vbadge bad\">%s</span></td></tr>",
				html.EscapeString(m.Name), html.EscapeString(m.Position), moneyWithReport(r, d.UnrelatedCostUSD), d.UnrelatedCostRatio*100, html.EscapeString(strings.Join(d.TopHours, "；")), escOrDash(d.TopModel), html.EscapeString(report.DisplayKey(d.TopKey)), html.EscapeString(status))
			writeSafetyReasonRow(b, d, 8)
		}
		writeHTML(b, "</table></div>")
	}
	writeHTML(b, "</section>\n")
}

func writeDepartmentQuadrantAndClusters(b *strings.Builder, r *report.Report) {
	me := r.MemberEvaluation
	if me == nil || len(me.Members) == 0 {
		return
	}

	writeHTML(b, "<section id=\"dept-member-evaluation\">")
	writeHTML(b, "<h2>3. 部门成员整合评估图（四象限散点图与分群）</h2>")
	writeHTML(b, "<h3 style=\"font-size:14px;color:#1e3a5f;margin-bottom:6px;\">4.1 组织成员整合评估图（四象限散点图）</h3>")
	writeHTML(b, "<div class=\"quadrant-meta\"><strong>怎么看：</strong>越往右花费越高，越往上问题消耗占比越高。右上角最需要关注；十字线表示部门中位水平。</div>")

	// 4.1 主图：四象限散点图 (SVG)。
	writeQuadrantSVG(b, r, me)
	writeQuadrantMemberDirectory(b, r, me)
	writeQuadrantLegend(b, r, me)
	writeQuadrantGuide(b)

	// §4.1「岗位差异大时按岗位分面」：岗位间量级悬殊时拆分为岗位小图，共享同一轴刻度与中位十字线。
	writePositionFacets(b, r, me)

	// 4.2 补充图：K-Means 聚类分群图。
	writeClusterSection(b, r, me)

	writeHTML(b, "</section>\n")
}

func writeDepartmentProblemMembers(b *strings.Builder, r *report.Report) {
	me := r.MemberEvaluation
	problem, _ := splitMembers(me)

	writeHTML(b, "<section id=\"dept-problem-members\">")
	writeHTML(b, "<h2>4. 存在问题的人（按管理优先级，非总成本排序）</h2>")
	if len(problem) == 0 {
		writeHTML(b, "<p style=\"color:#166534;font-size:14px;font-weight:600;\">无，部门全部成员用法正常。</p>")
		writeHTML(b, "</section>\n")
		return
	}

	writeHTML(b, "<p class=muted>需要核查的人排在最前面。点击姓名可看原因和建议。</p>")

	// 汇总表格。
	writeHTML(b, "<table class=\"num compact\">")
	writeHTML(b, "<tr><th>姓名</th><th>当前情况</th><th>总成本</th><th>主要问题</th><th>浪费金额</th><th>建议</th></tr>")
	for _, p := range problem {
		status := "需要辅导"
		if p.Quadrant == report.QuadrantPriorityImprove {
			status = "优先关注"
		}
		if p.SafetyAnomaly {
			status = "<span class=\"vbadge bad\">待核查</span>"
		}
		advice := defaultAdvice(p.DominantIssue, p.Quadrant, p.SafetyAnomaly)
		attrCost := p.DominantIssueCostUSD
		if attrCost <= 0 {
			attrCost = p.IssueAttributedCostUSD
		}
		writeHTML(b, "<tr><td><strong>%s</strong></td><td>%s</td><td class=money>%s</td><td>%s</td><td class=money>%s</td><td style=\"text-align:left;font-size:12px;\">%s</td></tr>",
			html.EscapeString(p.Name),
			status,
			moneyWithReport(r, p.CostUSD),
			issueDisplayName(p.DominantIssue),
			moneyWithReport(r, attrCost),
			html.EscapeString(advice),
		)
	}
	writeHTML(b, "</table>")

	// 展开卡片（与个人报告 2–4 结构对齐）。
	writeHTML(b, "<div class=\"problem-expand-list\" style=\"margin-top:16px;\">")
	for _, p := range problem {
		writeMemberProblemExpandCard(b, r, p)
	}
	writeHTML(b, "</div>")

	writeHTML(b, "</section>\n")
}

func writeDepartmentNormalMembers(b *strings.Builder, r *report.Report) {
	me := r.MemberEvaluation
	_, normal := splitMembers(me)

	writeHTML(b, "<section id=\"dept-normal-members\">")
	writeHTML(b, "<h2>5. 正常使用的人（大概展示，不展开五类）</h2>")
	if len(normal) == 0 {
		writeHTML(b, "<p class=muted>无。</p>")
		writeHTML(b, "</section>\n")
		return
	}

	writeHTML(b, "<p class=muted>共 %d 名成员行为高效健康，无需技术限制或人工干预。</p>", len(normal))
	writeHTML(b, "<table class=\"num compact\">")
	writeHTML(b, "<tr><th>姓名</th><th>岗位</th><th>输入 Token</th><th>缓存 Token</th><th>输出 Token</th><th>成本</th><th>调用次数</th><th>结论</th></tr>")
	for _, p := range normal {
		verdict := "用法正常"
		if p.Quadrant == report.QuadrantHighCostNormal {
			verdict = "高用量·用法正常"
		}
		writeHTML(b, "<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td class=money>%s</td><td>%d</td><td><span class=\"vbadge ok\">%s</span></td></tr>",
			html.EscapeString(p.Name), html.EscapeString(p.Position), formatInt64(p.TokenBreakdown.InputTokens), formatInt64(p.TokenBreakdown.CachedTokens), formatInt64(p.TokenBreakdown.OutputTokens), moneyWithReport(r, p.CostUSD), p.Calls, verdict)
	}
	writeHTML(b, "</table>")
	writeHTML(b, "</section>\n")
}

// -----------------------------------------------------------------------------
// 工作室报告专属区块：基础数据、各部门对照表、严重异常、全员散点图、各部门问题人、全员正常表
// -----------------------------------------------------------------------------

func writeStudioBaseData(b *strings.Builder, r *report.Report) {
	cb := r.CostBreakdown
	total := cb.Total()
	me := r.MemberEvaluation

	writeHTML(b, "<section id=\"core-data\"><h2>1. 工作室本周基础数据</h2>")
	writeTokenSummary(b, r)
	if me != nil {
		totalCalls := memberCalls(me.Members)
		delta := deltaPct(prevTotal(cb), total)
		deltaStr := "—"
		deltaClass := "muted"
		if delta < 0 {
			deltaStr = fmt.Sprintf("▼ %.1f%% 环比", -delta*100)
			deltaClass = "down"
		} else if delta > 0 {
			deltaStr = fmt.Sprintf("▲ +%.1f%% 环比", delta*100)
			deltaClass = "up"
		}

		writeHTML(b, "<div class=\"kpi-grid\">")
		writeHTML(b, "<div class=\"kpi-card highlight\"><div class=\"kpi-label\">工作室总成本</div><div class=\"kpi-val\">%s</div><div class=\"kpi-sub\"><span class=\"kpi-trend %s\">%s</span> 覆盖 %d 活跃成员</div></div>",
			moneyWithReport(r, total), deltaClass, deltaStr, len(me.Members))
		writeHTML(b, "<div class=\"kpi-card\"><div class=\"kpi-label\">缓存读取 (Cache)</div><div class=\"kpi-val\">%s</div><div class=\"kpi-sub\"><span class=\"kpi-pill-badge\">占比 %.1f%%</span></div></div>",
			moneyWithReport(r, cb.CacheCostUSD), share(cb.CacheCostUSD, total)*100)
		writeHTML(b, "<div class=\"kpi-card\"><div class=\"kpi-label\">非缓存输入 (Input)</div><div class=\"kpi-val\">%s</div><div class=\"kpi-sub\"><span class=\"kpi-pill-badge\">占比 %.1f%%</span></div></div>",
			moneyWithReport(r, cb.UncachedInputCostUSD), share(cb.UncachedInputCostUSD, total)*100)
		writeHTML(b, "<div class=\"kpi-card\"><div class=\"kpi-label\">生成输出 (Output)</div><div class=\"kpi-val\">%s</div><div class=\"kpi-sub\"><span class=\"kpi-pill-badge\">占比 %.1f%%</span></div></div>",
			moneyWithReport(r, cb.OutputCostUSD), share(cb.OutputCostUSD, total)*100)
		writeHTML(b, "</div>")

		writeHTML(b, "<div class=\"stat-summary-bar\">工作室活跃 <strong>%d</strong> 人 &nbsp;·&nbsp; 总调用 <strong>%s</strong> 次 &nbsp;·&nbsp; 总成本 <strong>%s</strong></div>",
			len(me.Members), formatInt(totalCalls), moneyWithReport(r, total))
	}

	writeHTML(b, "<table class=\"num\">")
	writeHTML(b, "<tr><th>分量</th><th>费用</th><th>占比</th><th>环比</th></tr>")
	writeCostRow(b, r, "总计", total, 1.0, deltaPct(prevTotal(cb), total))
	writeCostRow(b, r, "├ 非缓存输入", cb.UncachedInputCostUSD, share(cb.UncachedInputCostUSD, total), deltaPct(prevOf(cb).UncachedInputCostUSD, cb.UncachedInputCostUSD))
	writeCostRow(b, r, "├ 缓存", cb.CacheCostUSD, share(cb.CacheCostUSD, total), deltaPct(prevOf(cb).CacheCostUSD, cb.CacheCostUSD))
	writeCostRow(b, r, "└ 输出", cb.OutputCostUSD, share(cb.OutputCostUSD, total), deltaPct(prevOf(cb).OutputCostUSD, cb.OutputCostUSD))
	if cb.FailedRequestCostUSD > 0 {
		writeHTML(b, "<tr><td>无效消耗（失败请求）</td><td class=\"money\">%s</td><td>标记不扣减</td><td>—</td></tr>", moneyWithReport(r, cb.FailedRequestCostUSD))
	}
	writeHTML(b, "</table>")

	writeModelTier(b, r)
	writeHTML(b, "</section>\n")
}

func writeStudioDeptBreakdown(b *strings.Builder, r *report.Report) {
	me := r.MemberEvaluation
	if me == nil || len(me.Members) == 0 {
		return
	}

	// 聚合各部门数据。
	deptMap := make(map[string][]report.MemberPoint)
	var deptOrder []string
	for _, m := range me.Members {
		d := report.MemberDeptLabel(m)
		if d == "" || d == "—" {
			d = "未分配部门"
		}
		if _, ok := deptMap[d]; !ok {
			deptOrder = append(deptOrder, d)
		}
		deptMap[d] = append(deptMap[d], m)
	}

	writeHTML(b, "<section><h2>2. 各部门基础数据（对照构成，不排名）</h2>")
	writeHTML(b, "<p class=muted>对照各部门成本与人员体量构成，看结构与异常分布，不做部门问责排名。</p>")
	writeHTML(b, "<table class=\"num compact\">")
	writeHTML(b, "<tr><th>部门</th><th>输入 Token</th><th>缓存 Token</th><th>输出 Token</th><th>输入费</th><th>缓存费</th><th>输出费</th><th>总费用</th><th>调用</th><th>活跃</th><th>待核查</th><th>问题人数</th></tr>")

	for _, d := range deptOrder {
		ms := deptMap[d]
		var deptCost, uncCost, cacheCost, outputCost float64
		var inputTokens, cachedTokens, outputTokens int64
		var deptCalls int
		var safetyCount int
		var problemCount int
		for _, m := range ms {
			deptCost += m.CostUSD
			uncCost += m.CostBreakdown.UncachedInputCostUSD
			cacheCost += m.CostBreakdown.CacheCostUSD
			outputCost += m.CostBreakdown.OutputCostUSD
			inputTokens += m.TokenBreakdown.InputTokens
			cachedTokens += m.TokenBreakdown.CachedTokens
			outputTokens += m.TokenBreakdown.OutputTokens
			deptCalls += m.Calls
			if m.SafetyAnomaly {
				safetyCount++
			}
			if isProblemMember(m) {
				problemCount++
			}
		}

		safetyStr := "—"
		if safetyCount > 0 {
			safetyStr = fmt.Sprintf("<span class=\"vbadge bad\">%d 人</span>", safetyCount)
		}
		probStr := "0"
		if problemCount > 0 {
			probStr = fmt.Sprintf("<span class=\"vbadge warn\">%d 人</span>", problemCount)
		}

		writeHTML(b, "<tr><td><strong>%s</strong></td><td>%s</td><td>%s</td><td>%s</td><td class=money>%s</td><td class=money>%s</td><td class=money>%s</td><td class=money>%s</td><td>%s</td><td>%d</td><td>%s</td><td>%s</td></tr>",
			html.EscapeString(d), formatInt64(inputTokens), formatInt64(cachedTokens), formatInt64(outputTokens), moneyWithReport(r, uncCost), moneyWithReport(r, cacheCost), moneyWithReport(r, outputCost), moneyWithReport(r, deptCost), formatInt(deptCalls), len(ms), safetyStr, probStr)
	}

	writeHTML(b, "</table></section>\n")
}

func writeStudioSafety(b *strings.Builder, r *report.Report) {
	me := r.MemberEvaluation
	hits := safetyMembers(me)
	if len(hits) == 0 {
		return
	}

	writeHTML(b, "<section id=\"safety-review\" class=safety><h2>3. 严重异常（待核查，带部门）</h2>")
	{
		writeHTML(b, "<p class=warm><strong>全工作室共 %d 名成员命中严重异常待核查</strong>（已带所属部门，独立于五种情况，绝不计入四象限 Y 轴；红线：仅供管理核查，严禁直接定责）：</p>", len(hits))
		writeHTML(b, "<p class=\"safety-definition\"><strong>“疑似加班未备案”是什么意思：</strong>该成员的未备案非工作时段费用超过个人成本的 20%%，同时 OpenCode 抽查内容后认为更像工作任务。因此应优先核实加班、值班或批处理备案，不代表与工作无关。</p>")
		writeHTML(b, "<div class=\"table-scroll\"><table class=\"num compact\">")
		writeHTML(b, "<tr><th>姓名</th><th>部门</th><th>未备案时段金额</th><th>占个人成本</th><th>集中时段</th><th>模型</th><th>Key</th><th>状态</th></tr>")
		for _, m := range hits {
			d := report.MemberDeptLabel(m)
			if d == "" || d == "—" {
				d = "—"
			}
			status := "待核查"
			if m.SafetyReviewTag != "" && m.SafetyReviewTag != "待核查" {
				status = fmt.Sprintf("待核查（%s）", m.SafetyReviewTag)
			}
			sd := m.SafetyDetail
			if sd.ReviewTag == "" {
				sd.ReviewTag = m.SafetyReviewTag
			}
			if sd.Hint == "" {
				sd.Hint = m.SafetyHint
			}
			writeHTML(b, "<tr><td><strong>%s</strong></td><td>%s</td><td class=money>%s</td><td>%.1f%%</td><td>%s</td><td>%s</td><td>%s</td><td><span class=\"vbadge bad\">%s</span></td></tr>",
				html.EscapeString(m.Name), html.EscapeString(d), moneyWithReport(r, sd.UnrelatedCostUSD), sd.UnrelatedCostRatio*100, html.EscapeString(strings.Join(sd.TopHours, "；")), escOrDash(sd.TopModel), html.EscapeString(report.DisplayKey(sd.TopKey)), html.EscapeString(status))
			writeSafetyReasonRow(b, sd, 8)
		}
		writeHTML(b, "</table></div>")
	}
	writeHTML(b, "</section>\n")
}

func writeStudioQuadrantAndClusters(b *strings.Builder, r *report.Report) {
	me := r.MemberEvaluation
	if me == nil || len(me.Members) == 0 {
		return
	}

	writeHTML(b, "<section id=\"studio-member-evaluation\">")
	writeHTML(b, "<h2>4. 工作室成员整合评估图（全员四象限散点图与分群）</h2>")
	writeHTML(b, "<h3 style=\"font-size:14px;color:#1e3a5f;margin-bottom:6px;\">4.1 组织成员整合评估图（四象限散点图）</h3>")
	writeHTML(b, "<div class=\"quadrant-meta\">Y 轴 = 五类问题消耗占比（不含严重异常） · X 轴 = 个人总成本 ÷ 工作室成本中位数（%.2f %s） · 点形区分不同部门 · 十字线标定工作室中位数</div>",
		me.CostMedianUSD, r.Meta.Currency.Report)

	// 4.1 全员工作室散点主图（点形区分部门）。
	writeQuadrantSVG(b, r, me)
	writeQuadrantMemberDirectory(b, r, me)
	writeQuadrantLegend(b, r, me)
	writeQuadrantGuide(b)

	// 4.2 K-Means 分群图。
	writeClusterSection(b, r, me)

	writeHTML(b, "</section>\n")
}

func writeStudioProblemByDept(b *strings.Builder, r *report.Report) {
	me := r.MemberEvaluation
	problem, _ := splitMembers(me)

	writeHTML(b, "<section id=\"studio-problem-by-dept\">")
	writeHTML(b, "<h2>5. 各部门存在问题的人</h2>")

	if len(problem) == 0 {
		writeHTML(b, "<p style=\"color:#166534;font-size:14px;font-weight:600;\">无，全工作室全部成员用法正常。</p>")
		writeHTML(b, "</section>\n")
		return
	}

	writeHTML(b, "<p class=muted>按部门分组呈现存在问题人员，部门内部按管理优先级排序（严重异常待核查 → 右上优先改善 → 左上习惯辅导）。</p>")

	// 按部门分组。
	deptMap := make(map[string][]report.MemberPoint)
	var deptOrder []string
	for _, p := range problem {
		d := report.MemberDeptLabel(p)
		if d == "" || d == "—" {
			d = "未分配部门"
		}
		if _, ok := deptMap[d]; !ok {
			deptOrder = append(deptOrder, d)
		}
		deptMap[d] = append(deptMap[d], p)
	}

	for _, d := range deptOrder {
		ms := deptMap[d]
		writeHTML(b, "<div class=\"dept-card\" style=\"margin-top:16px;\">")
		writeHTML(b, "<h3 style=\"color:#1e3a5f;\">【%s】共 %d 人存在待辅导或改善项</h3>", html.EscapeString(d), len(ms))

		writeHTML(b, "<table class=\"num compact\">")
		writeHTML(b, "<tr><th>姓名</th><th>象限</th><th>总成本</th><th>相对中位</th><th>问题消耗占比</th><th>严重异常</th><th>主导类型</th><th>归因费用</th><th>管理建议</th></tr>")
		for _, p := range ms {
			safetyBadge := "—"
			if p.SafetyAnomaly {
				safetyBadge = "<span class=\"vbadge bad\">待核查</span>"
			}
			advice := defaultAdvice(p.DominantIssue, p.Quadrant, p.SafetyAnomaly)
			writeHTML(b, "<tr><td><strong>%s</strong></td><td>%s</td><td class=money>%s</td><td>%.2f×</td><td>%.1f%%</td><td>%s</td><td>%s</td><td class=money>%s</td><td style=\"text-align:left;font-size:12px;\">%s</td></tr>",
				html.EscapeString(p.Name),
				quadrantDisplayName(p.Quadrant),
				moneyWithReport(r, p.CostUSD),
				p.CostMultiple,
				p.IssueCostRatio*100,
				safetyBadge,
				issueDisplayName(p.DominantIssue),
				moneyWithReport(r, p.DominantIssueCostUSD),
				html.EscapeString(advice),
			)
		}
		writeHTML(b, "</table>")

		// 展开卡。
		writeHTML(b, "<div class=\"problem-expand-list\" style=\"margin-top:12px;\">")
		for _, p := range ms {
			writeMemberProblemExpandCard(b, r, p)
		}
		writeHTML(b, "</div>")

		writeHTML(b, "</div>")
	}

	writeHTML(b, "</section>\n")
}

// -----------------------------------------------------------------------------
// 四象限散点图 SVG 绘制与图例（支持点形区分部门）
// -----------------------------------------------------------------------------

// quadrantXPos 为成本中位线两侧分配稳定空间：左侧 [0,1] 占 45%，右侧
// [1,xMax] 占 55%。这样既能看见接近零成本成员，也不会让极小值把中位线推到最右侧。
func quadrantXPos(mult, xMax float64) float64 {
	const leftShare = 0.45
	if mult <= 0 {
		return 0
	}
	if mult <= 1 {
		return leftShare * math.Pow(mult, 0.55)
	}
	if xMax <= 1 {
		return leftShare
	}
	if mult > xMax {
		mult = xMax
	}
	return leftShare + (1-leftShare)*math.Log(mult)/math.Log(xMax)
}

// multipleTick 格式化 X 轴倍数刻度（<1 保留两位去尾零，>=1 取整）。
func multipleTick(v float64) string {
	if v >= 1 {
		return fmt.Sprintf("%.0f×", v)
	}
	return fmt.Sprintf("%g×", v)
}

// quadrantYAxis 返回能覆盖本期数据的易读纵轴。象限仍按原始占比判定，
// 这里只调整展示尺度，避免常见的 0%~15% 数据被固定 100% 轴压在底部。
func quadrantYAxis(members []report.MemberPoint, median float64) (maxY, step float64) {
	target := math.Max(0.10, median*1.6)
	for _, m := range members {
		if v := m.IssueCostRatio * 1.15; v > target {
			target = v
		}
	}
	switch {
	case target <= 0.10:
		return 0.10, 0.02
	case target <= 0.20:
		return 0.20, 0.05
	case target <= 0.30:
		return 0.30, 0.05
	case target <= 0.40:
		return 0.40, 0.10
	case target <= 0.50:
		return 0.50, 0.10
	case target <= 0.60:
		return 0.60, 0.10
	case target <= 0.80:
		return 0.80, 0.20
	default:
		return 1.0, 0.20
	}
}

// quadrantYPos 让中位线以下占 30% 高度、以上占 70% 高度，返回自底向上的归一化坐标。
// 这只展开密集区域，不改变成员的原始问题占比和象限归属。
func quadrantYPos(value, median, maxY float64) float64 {
	const lowerShare = 0.30
	value = clampf(value, 0, maxY)
	if median <= 0 {
		if maxY <= 0 {
			return 0
		}
		return value / maxY
	}
	if value <= median {
		return lowerShare * value / median
	}
	if maxY <= median {
		return lowerShare
	}
	return lowerShare + (1-lowerShare)*(value-median)/(maxY-median)
}

// clampf 将 v 限制在 [lo,hi] 区间。
func clampf(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// dotLayout 单个散点的布局（点心 + 半径）。
type dotLayout struct{ cx, cy, rr float64 }

// deOverlap 对一组散点做轻量防重叠松弛：互相重叠/过近的点沿连线反向推开，
// 迭代若干次收敛；并把每个点相对原始位置的总位移限制在 maxDx/maxDy 内，
// 避免点被推得远离真实数据位置而误导读数。
func deOverlap(dots []dotLayout, plotLeft, plotRight, plotTop, plotBottom, maxDx, maxDy float64) {
	const minGap = 4.5
	type orig struct{ x, y float64 }
	origs := make([]orig, len(dots))
	for i, d := range dots {
		origs[i] = orig{d.cx, d.cy}
	}
	for iter := 0; iter < 48; iter++ {
		moved := false
		for i := 0; i < len(dots); i++ {
			for j := i + 1; j < len(dots); j++ {
				dx := dots[j].cx - dots[i].cx
				dy := dots[j].cy - dots[i].cy
				d := math.Hypot(dx, dy)
				need := dots[i].rr + dots[j].rr + minGap
				if d >= need {
					continue
				}
				if d < 0.0001 {
					dx, dy, d = 0.0001, 0.0001, 0.0001
				}
				ux, uy := dx/d, dy/d
				push := (need - d) / 2
				dots[i].cx -= ux * push
				dots[i].cy -= uy * push
				dots[j].cx += ux * push
				dots[j].cy += uy * push
				moved = true
			}
		}
		// 限制在绘图区内，并约束相对原始位置的位移上限。
		for k := range dots {
			dots[k].cx = clampf(dots[k].cx, plotLeft+2, plotRight-2)
			dots[k].cy = clampf(dots[k].cy, plotTop+2, plotBottom-2)
			dots[k].cx = clampf(dots[k].cx, origs[k].x-maxDx, origs[k].x+maxDx)
			dots[k].cy = clampf(dots[k].cy, origs[k].y-maxDy, origs[k].y+maxDy)
		}
		if !moved {
			break
		}
	}
}

func writeQuadrantSVG(b *strings.Builder, r *report.Report, me *report.MemberEvaluation) {
	const (
		svgWidth   = 1040.0
		svgHeight  = 640.0
		plotLeft   = 78.0
		plotRight  = 980.0
		plotTop    = 55.0
		plotBottom = 565.0
		plotWidth  = plotRight - plotLeft
		plotHeight = plotBottom - plotTop
	)

	// 左侧固定覆盖 0~1，中位线不会被极小值挤到画布边缘。
	maxCostMult := 1.5
	for _, m := range me.Members {
		if m.CostMultiple > maxCostMult {
			maxCostMult = m.CostMultiple
		}
	}
	xMax := 2.0
	for xMax < maxCostMult*1.08 {
		xMax *= 2
	}

	// Y 轴按本期数据自动放大；四象限分类仍使用原始占比。
	yMax, yStep := quadrantYAxis(me.Members, me.IssueRatioMedian)
	yMed := me.IssueRatioMedian
	if yMed < 0 {
		yMed = 0
	}
	if yMed > yMax {
		yMed = yMax
	}

	// 十字线位置。
	crossX := plotLeft + quadrantXPos(1.0, xMax)*plotWidth
	crossY := plotBottom - quadrantYPos(yMed, yMed, yMax)*plotHeight

	// 调用次数极值（用于半径缩放）。
	minCalls := math.MaxInt32
	maxCalls := 0
	for _, m := range me.Members {
		if m.Calls < minCalls {
			minCalls = m.Calls
		}
		if m.Calls > maxCalls {
			maxCalls = m.Calls
		}
	}
	if minCalls > maxCalls {
		minCalls = 0
	}

	// 工作室报告：分配部门点形。
	deptShapes := make(map[string]string)
	if r.Meta.Level == report.LevelStudio {
		shapes := []string{"circle", "triangle", "square", "diamond"}
		var dOrder []string
		for _, m := range me.Members {
			d := report.MemberDeptLabel(m)
			if d == "" || d == "—" {
				d = "未分配"
			}
			if _, ok := deptShapes[d]; !ok {
				deptShapes[d] = shapes[len(dOrder)%len(shapes)]
				dOrder = append(dOrder, d)
			}
		}
	}

	writeHTML(b, "<div class=\"quadrant-svg-wrap\">")
	writeHTML(b, "<svg viewBox=\"0 0 %.0f %.0f\" width=\"100%%\" height=\"auto\" class=\"quadrant-svg\" data-y-max=\"%.2f\" xmlns=\"http://www.w3.org/2000/svg\">", svgWidth, svgHeight, yMax)

	// SVG 滤镜定义（点阴影）
	writeHTML(b, "<defs>")
	writeHTML(b, "<filter id=\"dot-shadow\" x=\"-40%%\" y=\"-40%%\" width=\"180%%\" height=\"180%%\">")
	writeHTML(b, "<feDropShadow dx=\"0\" dy=\"1.5\" stdDeviation=\"1.8\" flood-color=\"#0f172a\" flood-opacity=\"0.22\"/>")
	writeHTML(b, "</filter>")
	writeHTML(b, "</defs>")

	// 1. 象限背景色块（轻柔半透明水印，仅表示区域范围；角标与中位药丸在点之后统一叠加绘制）。
	// 左上 · 习惯辅导。
	writeHTML(b, "<rect x=\"%.1f\" y=\"%.1f\" width=\"%.1f\" height=\"%.1f\" fill=\"#fef3c7\" fill-opacity=\"0.14\"/>",
		plotLeft, plotTop, crossX-plotLeft, crossY-plotTop)
	// 右上 · 优先改善。
	writeHTML(b, "<rect x=\"%.1f\" y=\"%.1f\" width=\"%.1f\" height=\"%.1f\" fill=\"#fee2e2\" fill-opacity=\"0.16\"/>",
		crossX, plotTop, plotRight-crossX, crossY-plotTop)
	// 左下 · 常规使用。
	writeHTML(b, "<rect x=\"%.1f\" y=\"%.1f\" width=\"%.1f\" height=\"%.1f\" fill=\"#f0fdf4\" fill-opacity=\"0.10\"/>",
		plotLeft, crossY, crossX-plotLeft, plotBottom-crossY)
	// 右下 · 高用量·正常。
	writeHTML(b, "<rect x=\"%.1f\" y=\"%.1f\" width=\"%.1f\" height=\"%.1f\" fill=\"#eff6ff\" fill-opacity=\"0.12\"/>",
		crossX, crossY, plotRight-crossX, plotBottom-crossY)

	// 象限角标卡片 + 中位药丸（下沉为闭包，在散点之后绘制，保证始终可读、不被点覆盖）。
	type quadrantCard struct {
		x, y, w    float64
		bg, bd, fg string
		label      string
	}
	cards := []quadrantCard{
		{plotLeft + 10, plotTop + 10, 150, "#fef3c7", "#fde68a", "#92400e", "左上 · 习惯辅导 (教用法)"},
		{plotRight - 215, plotTop + 10, 205, "#fee2e2", "#fecaca", "#991b1b", "右上 · 优先改善 (重点关注 / 优先看)"},
		{plotLeft + 10, plotBottom - 32, 140, "#f0fdf4", "#bbf7d0", "#166534", "左下 · 常规使用 (例行)"},
		{plotRight - 190, plotBottom - 32, 180, "#eff6ff", "#bfdbfe", "#1e40af", "右下 · 高用量·正常 (正常不干预)"},
	}
	drawOverlays := func() {
		for _, c := range cards {
			writeHTML(b, "<rect x=\"%.1f\" y=\"%.1f\" width=\"%.1f\" height=\"22\" rx=\"4\" fill=\"%s\" fill-opacity=\"0.94\" stroke=\"%s\" stroke-width=\"1\"/>",
				c.x, c.y, c.w, c.bg, c.bd)
			writeHTML(b, "<text x=\"%.1f\" y=\"%.1f\" fill=\"%s\" font-size=\"11\" font-weight=\"600\">%s</text>",
				c.x+8, c.y+16, c.fg, c.label)
		}
		// X=1.0 与 Y=中位 药丸。
		writeHTML(b, "<rect x=\"%.1f\" y=\"%.1f\" width=\"104\" height=\"18\" rx=\"9\" fill=\"#0f172a\" fill-opacity=\"0.92\"/>",
			crossX-52, plotTop-12)
		writeHTML(b, "<text x=\"%.1f\" y=\"%.1f\" text-anchor=\"middle\" font-size=\"10\" font-weight=\"600\" fill=\"#f8fafc\">X=1.0 (成本中位)</text>",
			crossX, plotTop+1)
		writeHTML(b, "<rect x=\"%.1f\" y=\"%.1f\" width=\"96\" height=\"18\" rx=\"9\" fill=\"#0f172a\" fill-opacity=\"0.92\"/>",
			plotRight-100, crossY-27)
		writeHTML(b, "<text x=\"%.1f\" y=\"%.1f\" text-anchor=\"middle\" font-size=\"10\" font-weight=\"600\" fill=\"#f8fafc\">Y=%.1f%% (中位)</text>",
			plotRight-52, crossY-14, yMed*100)
	}

	// 2. 坐标网格与刻度。左半区固定展示 0~1，右半区按 2 的幂展开。
	xTicks := []float64{0, 0.25, 0.5, 1}
	for tick := 2.0; tick <= xMax; tick *= 2 {
		xTicks = append(xTicks, tick)
	}
	for _, tick := range xTicks {
		tx := plotLeft + quadrantXPos(tick, xMax)*plotWidth
		writeHTML(b, "<line x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\" stroke=\"#e2e8f0\" stroke-width=\"1\" stroke-dasharray=\"2,2\"/>",
			tx, plotTop, tx, plotBottom)
		writeHTML(b, "<text x=\"%.1f\" y=\"%.1f\" text-anchor=\"middle\" font-size=\"10\" fill=\"#94a3b8\">%s</text>",
			tx, plotBottom+16, multipleTick(tick))
	}

	for yt := 0.0; yt <= yMax+1e-9; yt += yStep {
		ty := plotBottom - quadrantYPos(yt, yMed, yMax)*plotHeight
		writeHTML(b, "<line x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\" stroke=\"#e2e8f0\" stroke-width=\"1\" stroke-dasharray=\"2,2\"/>",
			plotLeft, ty, plotRight, ty)
		writeHTML(b, "<text x=\"%.1f\" y=\"%.1f\" text-anchor=\"end\" font-size=\"10\" fill=\"#94a3b8\">%.0f%%</text>",
			plotLeft-8, ty+3, yt*100)
	}

	// 3. 组织中位基准十字线（高对比深灰虚线；药丸徽章在散点之后由 drawOverlays 绘制）。
	writeHTML(b, "<line x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\" stroke=\"#475569\" stroke-width=\"1.6\" stroke-dasharray=\"6,4\"/>",
		crossX, plotTop, crossX, plotBottom)

	writeHTML(b, "<line x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\" stroke=\"#475569\" stroke-width=\"1.6\" stroke-dasharray=\"6,4\"/>",
		plotLeft, crossY, plotRight, crossY)

	// 外边框。
	writeHTML(b, "<line x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\" stroke=\"#cbd5e1\" stroke-width=\"1.2\"/>",
		plotLeft, plotBottom, plotRight, plotBottom)
	writeHTML(b, "<line x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\" stroke=\"#cbd5e1\" stroke-width=\"1.2\"/>",
		plotLeft, plotTop, plotLeft, plotBottom)

	// 轴标题。
	writeHTML(b, "<text x=\"%.1f\" y=\"%.1f\" text-anchor=\"middle\" font-size=\"12\" font-weight=\"600\" fill=\"#475569\">个人总成本 ÷ 组织成本中位数（分段对数刻度，X=1.0 为组织中位线）</text>",
		plotLeft+plotWidth/2, svgHeight-10)
	writeHTML(b, "<text transform=\"rotate(-90)\" x=\"%.1f\" y=\"20\" text-anchor=\"middle\" font-size=\"12\" font-weight=\"600\" fill=\"#475569\">五类问题消耗占比 (0%% ~ %.0f%%)</text>",
		-(plotTop + plotHeight/2), yMax*100)

	// 找出花费最高的前2名正常人员（用于高用量正常标注）
	var topSpenders []string
	sortedByCost := make([]report.MemberPoint, len(me.Members))
	copy(sortedByCost, me.Members)
	sort.Slice(sortedByCost, func(i, j int) bool {
		return sortedByCost[i].CostUSD > sortedByCost[j].CostUSD
	})
	for i := 0; i < len(sortedByCost) && i < 2; i++ {
		topSpenders = append(topSpenders, sortedByCost[i].Name)
	}
	isTopSpender := func(name string) bool {
		for _, s := range topSpenders {
			if s == name {
				return true
			}
		}
		return false
	}

	// 4. 成员散点绘制与智能防重叠文本标注。
	//   - 标签按优先级取「关键关注人员」：右上优先 / 左上辅导 / 严重异常 / 头部用量。
	//   - 每个点尝试多个候选锚点（上/下/左/右/斜向），取首个不与已放置标签、象限水印
	//     卡片冲突的位置；若全部冲突则后移一档再试；仍冲突则不显示（保留在浮窗与表格）。
	//   - 命中红色「⊗待核查」单独从其锚点沿点外圈画引线，避免压住点本身。

	// 已放置标签（中心点 + 半宽/半高，用于矩形碰撞检测）。
	type placedLabel struct {
		cx, cy, hw, hh float64
	}
	var placed []placedLabel

	// 象限水印卡片占位（避免标签压住「左下 · 常规使用」等说明框）。
	type zoneBox struct {
		x0, y0, x1, y1 float64
	}
	var zones []zoneBox
	addZone := func(bx, by, bw, bh float64) {
		zones = append(zones, zoneBox{bx - 6, by - 6, bx + bw + 6, by + bh + 6})
	}
	addZone(plotLeft+10, plotTop+10, 150, 22)      // 左上
	addZone(plotRight-215, plotTop+10, 205, 22)    // 右上
	addZone(plotLeft+10, plotBottom-32, 140, 22)   // 左下
	addZone(plotRight-190, plotBottom-32, 180, 22) // 右下
	addZone(crossX-52, plotTop-12, 104, 18)        // X=1.0 中位药丸
	addZone(plotRight-100, crossY-27, 96, 18)      // Y=中位 药丸

	// 散点本体避让区（与区间水印分开，便于按当前成员跳过自身）。
	var dotZones []zoneBox

	labelHalf := func(name string, fontSize float64) (hw, hh float64) {
		w := float64(len([]rune(name)))*fontSize*0.95 + 8
		return w / 2, fontSize*0.62 + 3
	}

	// collides 返回锚点 (lx,ly) 处放置该名标签是否与已放置标签/水印区/其它散点冲突。
	// ownDot 为当前成员在 dotZones 中的下标，命中自身散点不计为冲突。
	collides := func(lx, ly, hw, hh float64, ownDot int) bool {
		if lx-hw < plotLeft+2 || lx+hw > plotRight-2 || ly-hh < plotTop+2 || ly+hh > plotBottom-2 {
			return true
		}
		for _, p := range placed {
			if math.Abs(p.cx-lx) < (p.hw+hw+4) && math.Abs(p.cy-ly) < (p.hh+hh+3) {
				return true
			}
		}
		for _, z := range zones {
			if lx+hw > z.x0 && lx-hw < z.x1 && ly+hh > z.y0 && ly-hh < z.y1 {
				return true
			}
		}
		for di, z := range dotZones {
			if di == ownDot {
				continue
			}
			if lx+hw > z.x0 && lx-hw < z.x1 && ly+hh > z.y0 && ly-hh < z.y1 {
				return true
			}
		}
		return false
	}

	// anchorOffsets 按优先级给出相对点心的候选锚点（δx, δy）。radial=1 为紧贴点，
	// 随 radial 增大向外扩散，用于把标签推离密集点簇。
	anchorOffsets := func(rr float64, radial float64) [][2]float64 {
		R := rr
		if radial > 1 {
			R = rr + (radial-1)*22
		}
		return [][2]float64{
			{0, -(R + 8)},         // 上
			{R + 8, 0},            // 右
			{-(R + 8), 0},         // 左
			{0, R + 14},           // 下
			{R + 12, -(R + 6)},    // 右上斜
			{-(R + 12), -(R + 6)}, // 左上斜
			{R + 12, R + 6},       // 右下斜
			{-(R + 12), R + 6},    // 左下斜
			{0, -(R + 22)},        // 更上
			{0, R + 28},           // 更下
			{R + 20, 0},           // 更右
			{-(R + 20), 0},        // 更左
		}
	}

	// 记录标签引线（从点心到标签锚点），统一延后绘制，避免压住散点。
	type leader struct{ x0, y0, x1, y1 float64 }
	var leaders []leader
	// 严重异常成员（红色描边环）的下标，延后为其放置「⊗待核查」标签，避免互相/与姓名重叠。
	var safetyTags []int

	// 预计算全部散点布局（点心 + 半径），并注册为标签避让区，避免文字压住其它成员的点。
	dots := make([]dotLayout, 0, len(me.Members))
	for _, m := range me.Members {
		cx := plotLeft + quadrantXPos(m.CostMultiple, xMax)*plotWidth
		if cx < plotLeft {
			cx = plotLeft
		}
		if cx > plotRight {
			cx = plotRight
		}
		cy := plotBottom - quadrantYPos(m.IssueCostRatio, yMed, yMax)*plotHeight
		if cy < plotTop {
			cy = plotTop
		}
		if cy > plotBottom {
			cy = plotBottom
		}
		rr := 7.5
		if maxCalls > minCalls {
			factor := math.Sqrt(float64(m.Calls-minCalls) / float64(maxCalls-minCalls))
			rr = 6.0 + factor*6.0
		}
		dots = append(dots, dotLayout{cx: cx, cy: cy, rr: rr})
	}

	// 防重叠松弛：把同一象限内几乎完全重叠的点沿连线推开，避免糊成一坨。
	// 限制位移上限，确保点仍落在真实数据位置附近、不跨象限误导。
	deOverlap(dots, plotLeft, plotRight, plotTop, plotBottom, plotWidth*0.035, plotHeight*0.04)
	// 避让后仍限制在原象限内，避免视觉位移改变象限含义。
	for i, m := range me.Members {
		if m.CostMultiple > 1 {
			dots[i].cx = clampf(dots[i].cx, crossX+3, plotRight-2)
		} else {
			dots[i].cx = clampf(dots[i].cx, plotLeft+2, crossX-3)
		}
		if m.IssueCostRatio > me.IssueRatioMedian {
			dots[i].cy = clampf(dots[i].cy, plotTop+2, crossY-3)
		} else {
			dots[i].cy = clampf(dots[i].cy, crossY+3, plotBottom-2)
		}
	}

	// 依据松弛后的最终位置注册散点避让区（略留空隙），供标签避让判定。
	for _, d := range dots {
		dotZones = append(dotZones, zoneBox{d.cx - d.rr - 5, d.cy - d.rr - 5, d.cx + d.rr + 5, d.cy + d.rr + 5})
	}

	for i, m := range me.Members {
		cx := dots[i].cx
		cy := dots[i].cy
		rRadius := dots[i].rr
		color := dominantIssueColor(m.DominantIssue)
		shape := "circle"
		if r.Meta.Level == report.LevelStudio {
			d := report.MemberDeptLabel(m)
			if d == "" || d == "—" {
				d = "未分配"
			}
			if s, ok := deptShapes[d]; ok {
				shape = s
			}
		}

		pointID := fmt.Sprintf("quadrant-member-%02d", i+1)
		writeHTML(b, "<g id=\"%s\" class=\"point-group\" data-member-index=\"%02d\" tabindex=\"0\" aria-label=\"%02d %s\">",
			pointID, i+1, i+1, html.EscapeString(m.Name))

		// 严重异常描边（红色虚线外环，不进四象限 Y 轴）。「⊗待核查」标签延后放置，
		// 避免与姓名标签、其它安全标签及右下水印互相重叠。
		if m.SafetyAnomaly {
			writeHTML(b, "<circle cx=\"%.1f\" cy=\"%.1f\" r=\"%.1f\" fill=\"none\" stroke=\"#dc2626\" stroke-width=\"2.6\" stroke-dasharray=\"4,2\"/>",
				cx, cy, rRadius+4.5)
			safetyTags = append(safetyTags, i)
		}

		// 根据形状绘制主点（实心 vs 虚边，带 drop-shadow 滤镜与白边）。
		strokeAttr := "stroke=\"#ffffff\" stroke-width=\"2.0\" fill-opacity=\"0.95\" filter=\"url(#dot-shadow)\""
		if m.SuspectedOnly {
			strokeAttr = "stroke=\"#334155\" stroke-width=\"2\" stroke-dasharray=\"3,2\" fill-opacity=\"0.65\" filter=\"url(#dot-shadow)\""
		}

		switch shape {
		case "triangle":
			writeHTML(b, "<polygon points=\"%.1f,%.1f %.1f,%.1f %.1f,%.1f\" fill=\"%s\" %s/>",
				cx, cy-rRadius*1.2, cx-rRadius*1.1, cy+rRadius*0.8, cx+rRadius*1.1, cy+rRadius*0.8, color, strokeAttr)
		case "square":
			s := rRadius * 1.6
			writeHTML(b, "<rect x=\"%.1f\" y=\"%.1f\" width=\"%.1f\" height=\"%.1f\" rx=\"3\" fill=\"%s\" %s/>",
				cx-s/2, cy-s/2, s, s, color, strokeAttr)
		case "diamond":
			writeHTML(b, "<polygon points=\"%.1f,%.1f %.1f,%.1f %.1f,%.1f %.1f,%.1f\" fill=\"%s\" %s/>",
				cx, cy-rRadius*1.25, cx+rRadius*1.25, cy, cx, cy+rRadius*1.25, cx-rRadius*1.25, cy, color, strokeAttr)
		default: // circle
			writeHTML(b, "<circle cx=\"%.1f\" cy=\"%.1f\" r=\"%.1f\" fill=\"%s\" %s/>",
				cx, cy, rRadius, color, strokeAttr)
		}
		// 每个成员都固定显示编号；完整姓名和精确数值在图下方全员定位表中一一对应。
		writeHTML(b, "<text class=\"point-index\" x=\"%.1f\" y=\"%.1f\" text-anchor=\"middle\" dominant-baseline=\"central\">%02d</text>", cx, cy, i+1)

		// 智能标注成员姓名（仅关键关注人员显示文字，避免密集重叠；带白光晕提高辨识度）。
		shouldLabel := m.Quadrant == report.QuadrantPriorityImprove ||
			m.Quadrant == report.QuadrantCoach ||
			m.SafetyAnomaly ||
			isTopSpender(m.Name)

		nameRendered := false
		if shouldLabel {
			hw, hh := labelHalf(m.Name, 11)
			lx, ly := cx, cy-rRadius-10
			placedOK := false
			// 逐档向外扩散尝试锚点（最多 6 档），找到首个不与已放置标签/水印区冲突的位置。
			for radial := 1.0; radial <= 6 && !placedOK; radial++ {
				for _, off := range anchorOffsets(rRadius, radial) {
					tx, ty := cx+off[0], cy+off[1]
					if collides(tx, ty, hw, hh, i) {
						continue
					}
					lx, ly, placedOK = tx, ty, true
					break
				}
			}
			if placedOK {
				// 记录引线：当标签锚点明显偏离点心时补一条细引线，保证可读。
				if math.Abs(lx-cx) > rRadius+6 || math.Abs(ly-cy) > rRadius+14 {
					leaders = append(leaders, leader{cx, cy, lx, ly})
				}
				placed = append(placed, placedLabel{lx, ly, hw, hh})
				nameRendered = true
				writeHTML(b, "<text class=\"point-name\" x=\"%.1f\" y=\"%.1f\" text-anchor=\"middle\">%s</text>",
					lx, ly+4, html.EscapeString(m.Name))
			}
		}
		if !nameRendered {
			writeHTML(b, "<text class=\"point-hover-name\" x=\"%.1f\" y=\"%.1f\" text-anchor=\"middle\">%02d %s</text>",
				cx, cy-rRadius-9, i+1, html.EscapeString(m.Name))
		}

		// 交互浮窗提示 (<title>)。
		safetyText := "未命中"
		if m.SafetyAnomaly {
			safetyText = "命中严重异常（待核查·仅描边不计入Y轴）"
		}
		verdictText := "已定性"
		if m.SuspectedOnly {
			verdictText = "疑似"
		}
		deptInfo := ""
		if dept := report.MemberDeptLabel(m); dept != "" && dept != "—" {
			deptInfo = fmt.Sprintf("，部门：%s", dept)
		}
		writeHTML(b, "<title>姓名：%s（岗位：%s%s）&#10;象限：%s&#10;个人总成本：%s（组织中位 %.2f×）&#10;问题消耗占比：%.1f%%（进四象限Y）&#10;主导问题：%s（归因 %s，状态：%s）&#10;严重异常：%s&#10;调用次数：%d 次</title>",
			html.EscapeString(m.Name),
			html.EscapeString(m.Position),
			html.EscapeString(deptInfo),
			quadrantDisplayName(m.Quadrant),
			moneyWithReport(r, m.CostUSD),
			m.CostMultiple,
			m.IssueCostRatio*100,
			issueDisplayName(m.DominantIssue),
			moneyWithReport(r, m.DominantIssueCostUSD),
			verdictText,
			safetyText,
			m.Calls,
		)

		writeHTML(b, "</g>")
	}

	// 5. 绘制标签引线（置于点后方，避免压住散点；细且低对比，指向对应成员）。
	for _, ld := range leaders {
		writeHTML(b, "<line x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\" stroke=\"#cbd5e1\" stroke-width=\"1\" stroke-dasharray=\"2,2\"/>",
			ld.x0, ld.y0, ld.x1, ld.y1)
	}

	// 5b. 放置严重异常「⊗待核查」标签：在环外多方向尝试，避开姓名标签 / 其它散点 /
	//     象限水印卡片。若与姓名标签重叠则优先让位。标签记录为红线描边的红色小徽章。
	type safetyTagPlaced struct{ x0, y0, x1, y1, cx, cy float64 }
	var placedSafety []safetyTagPlaced
	for _, idx := range safetyTags {
		cx, cy, rr := dots[idx].cx, dots[idx].cy, dots[idx].rr
		hw, hh := labelHalf("⊗待核查", 10)
		lx, ly := cx, cy
		placedOK := false
		// 安全标签优先放在点的「下方/右下/左下」，与姓名标签（优先上/左/右）错开。
		safetyAnchors := [][2]float64{
			{0, rr + 14},            // 下
			{rr + 5, rr + 10},       // 右下
			{-(rr + 5), rr + 10},    // 左下
			{-(rr + 12), -(rr + 6)}, // 左上斜
			{rr + 12, -(rr + 6)},    // 右上斜
			{-(rr + 8), 0},          // 左
			{rr + 8, 0},             // 右
		}
		for radial := 0.0; radial <= 5 && !placedOK; radial++ {
			for _, off := range safetyAnchors {
				tx, ty := cx+off[0], cy+off[1]+radial*8
				if tx+hw > plotRight-2 || tx-hw < plotLeft+2 || ty+hh > plotBottom-2 || ty-hh < plotTop+2 {
					continue
				}
				// 不与姓名标签冲突；不与其它散点冲突（跳过自身）；不与水印卡片冲突。
				if collides(tx, ty, hw, hh, idx) {
					continue
				}
				// 不与已放置的安全标签冲突。
				bad := false
				for _, p := range placedSafety {
					if math.Abs(p.cx-tx) < (p.x1-p.x0)/2+hw+3 && math.Abs(p.cy-ty) < (p.y1-p.y0)/2+hh+2 {
						bad = true
						break
					}
				}
				if bad {
					continue
				}
				lx, ly, placedOK = tx, ty, true
				break
			}
		}
		if placedOK {
			placedSafety = append(placedSafety, safetyTagPlaced{lx - hw, ly - hh, lx + hw, ly + hh, lx, ly})
			writeHTML(b, "<text x=\"%.1f\" y=\"%.1f\" font-size=\"10\" font-weight=\"bold\" fill=\"#dc2626\">⊗待核查</text>",
				lx, ly+hh*0.34)
		}
	}

	// 6. 叠加绘制象限角标卡片与中位药丸（置于散点之上，保证不被点遮挡、始终可读）。
	drawOverlays()

	writeHTML(b, "</svg>")
	writeHTML(b, "<p class=\"quadrant-scale-note\">横纵轴按组织中位线分段展开，纵轴覆盖到 %.0f%%；散点仅做轻微避让且不跨象限，精确数值请对照下方全员定位表。</p>", yMax*100)
	writeHTML(b, "</div>")
}

// writeQuadrantMemberDirectory 把图中每一个编号映射到姓名和精确数值，保证密集点、
// 零成本点和未显示常驻姓名的成员也能被完整定位。
func writeQuadrantMemberDirectory(b *strings.Builder, r *report.Report, me *report.MemberEvaluation) {
	type indexedMember struct {
		index  int
		member report.MemberPoint
	}
	rows := make([]indexedMember, 0, len(me.Members))
	for i, m := range me.Members {
		rows = append(rows, indexedMember{index: i + 1, member: m})
	}
	priority := map[string]int{
		report.QuadrantPriorityImprove: 0,
		report.QuadrantCoach:           1,
		report.QuadrantHighCostNormal:  2,
		report.QuadrantRoutine:         3,
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, c := rows[i].member, rows[j].member
		if priority[a.Quadrant] != priority[c.Quadrant] {
			return priority[a.Quadrant] < priority[c.Quadrant]
		}
		if a.SafetyAnomaly != c.SafetyAnomaly {
			return a.SafetyAnomaly
		}
		if a.IssueCostRatio != c.IssueCostRatio {
			return a.IssueCostRatio > c.IssueCostRatio
		}
		return a.CostUSD > c.CostUSD
	})

	writeHTML(b, "<div class=\"quadrant-directory\">")
	writeHTML(b, "<div class=\"quadrant-directory-head\"><strong>全员定位表</strong><span>共 %d 人，编号与散点一一对应；按管理优先级分组，不按总成本排名。</span></div>", len(rows))
	writeHTML(b, "<div class=\"table-scroll\"><table class=\"num compact quadrant-directory-table\">")
	writeHTML(b, "<tr><th>图中</th><th>姓名</th><th>象限</th><th>总成本</th><th>相对中位</th><th>问题占比</th><th>主导情况</th><th>状态</th></tr>")
	for _, row := range rows {
		m := row.member
		status := "已定性"
		if m.SuspectedOnly {
			status = "疑似"
		}
		if m.SafetyAnomaly {
			status += " · 待核查"
		}
		writeHTML(b, "<tr class=\"quadrant-directory-row q-%s\" data-member-ref=\"quadrant-member-%02d\" tabindex=\"0\"><td><a class=\"member-index-link\" href=\"#quadrant-member-%02d\">%02d</a></td><td><strong>%s</strong></td><td>%s</td><td class=money>%s</td><td>%.2f×</td><td>%.1f%%</td><td><span class=\"legend-color-dot\" style=\"background:%s;\"></span>%s</td><td>%s</td></tr>",
			m.Quadrant, row.index, row.index, row.index, html.EscapeString(m.Name), quadrantDisplayName(m.Quadrant), moneyWithReport(r, m.CostUSD), m.CostMultiple, m.IssueCostRatio*100, dominantIssueColor(m.DominantIssue), issueDisplayName(m.DominantIssue), html.EscapeString(status))
	}
	writeHTML(b, "</table></div></div>")
}

func writeQuadrantLegend(b *strings.Builder, r *report.Report, me *report.MemberEvaluation) {
	writeHTML(b, "<details class=\"legend-box\"><summary>看不懂颜色和圆点？点这里查看说明</summary>")

	// 主导问题分色图例。
	writeHTML(b, "<div class=\"legend-row\"><span class=\"legend-title\">主导问题分色：</span>")
	writeHTML(b, "<span class=\"legend-item\"><span class=\"legend-color-dot\" style=\"background:#2563eb;\"></span>前缀污染 (蓝)</span>")
	writeHTML(b, "<span class=\"legend-item\"><span class=\"legend-color-dot\" style=\"background:#0d9488;\"></span>挤牙膏 (青)</span>")
	writeHTML(b, "<span class=\"legend-item\"><span class=\"legend-color-dot\" style=\"background:#7c3aed;\"></span>多任务不切会话 (紫)</span>")
	writeHTML(b, "<span class=\"legend-item\"><span class=\"legend-color-dot\" style=\"background:#ea580c;\"></span>输出失控 (橙)</span>")
	writeHTML(b, "<span class=\"legend-item\"><span class=\"legend-color-dot\" style=\"background:#dc2626;\"></span>杀鸡用牛刀 (红)</span>")
	writeHTML(b, "<span class=\"legend-item\"><span class=\"legend-color-dot\" style=\"background:#94a3b8;\"></span>正常使用 (灰)</span>")
	writeHTML(b, "</div>")

	// 散点样式标识图例。
	writeHTML(b, "<div class=\"legend-row\"><span class=\"legend-title\">散点样式标识：</span>")
	writeHTML(b, "<span class=\"legend-item\">● 实心：已定性</span>")
	writeHTML(b, "<span class=\"legend-item\">◉ 虚边：全疑似</span>")
	writeHTML(b, "<span class=\"legend-item\"><span style=\"color:#dc2626;font-weight:bold;\">⊗ 红色外环</span>：需要人工核查</span>")
	writeHTML(b, "<span class=\"legend-item\">⭕ 点越大：使用次数越多</span>")
	writeHTML(b, "</div>")

	// 工作室点形区分图例。
	if r.Meta.Level == report.LevelStudio {
		shapes := []string{"circle", "triangle", "square", "diamond"}
		shapeSymbols := map[string]string{
			"circle":   "●",
			"triangle": "▲",
			"square":   "■",
			"diamond":  "◆",
		}
		deptMap := make(map[string]string)
		var dOrder []string
		for _, m := range me.Members {
			d := report.MemberDeptLabel(m)
			if d == "" || d == "—" {
				d = "未分配"
			}
			if _, ok := deptMap[d]; !ok {
				deptMap[d] = shapes[len(dOrder)%len(shapes)]
				dOrder = append(dOrder, d)
			}
		}
		if len(dOrder) > 0 {
			writeHTML(b, "<div class=\"legend-row\"><span class=\"legend-title\">部门点形标识：</span>")
			for _, d := range dOrder {
				sh := deptMap[d]
				sym := shapeSymbols[sh]
				writeHTML(b, "<span class=\"legend-item\"><strong style=\"font-size:14px;\">%s</strong> %s</span>", sym, html.EscapeString(d))
			}
			writeHTML(b, "</div>")
		}
	}

	// 组织基准线说明。
	writeHTML(b, "<div class=\"legend-row\"><span class=\"legend-title\">十字线：</span>")
	writeHTML(b, "<span class=\"legend-item\">竖线表示部门一般花费（%.2f %s）</span>", me.CostMedianUSD, r.Meta.Currency.Report)
	writeHTML(b, "<span class=\"legend-item\">横线表示部门一般浪费水平（%.1f%%）</span>", me.IssueRatioMedian*100)
	writeHTML(b, "</div>")

	writeHTML(b, "</details>")
}

func writeQuadrantGuide(b *strings.Builder) {
	writeHTML(b, "<div class=\"guide-grid\">")

	writeHTML(b, "<div class=\"guide-card priority\">")
	writeHTML(b, "<h4>右上 · 优先改善（优先看 / 重点关注）</h4>")
	writeHTML(b, "<p><strong>怎么看：</strong>花得多，浪费也多。</p>")
	writeHTML(b, "<p><strong>怎么做：</strong>先看这些人，找出浪费最大的地方并及时调整。</p>")
	writeHTML(b, "</div>")

	writeHTML(b, "<div class=\"guide-card coach\">")
	writeHTML(b, "<h4>左上 · 习惯辅导（教用法）</h4>")
	writeHTML(b, "<p><strong>怎么看：</strong>花得不多，但使用方式比较浪费。</p>")
	writeHTML(b, "<p><strong>怎么做：</strong>教会一次说清需求，新任务及时开新会话。</p>")
	writeHTML(b, "</div>")

	writeHTML(b, "<div class=\"guide-card high-normal\">")
	writeHTML(b, "<h4>右下 · 高用量·正常（正常不干预）</h4>")
	writeHTML(b, "<p><strong>怎么看：</strong>花得多，但大部分都用在正常工作上。</p>")
	writeHTML(b, "<p><strong>怎么做：</strong>不用限制，保持观察即可。</p>")
	writeHTML(b, "</div>")

	writeHTML(b, "<div class=\"guide-card routine\">")
	writeHTML(b, "<h4>左下 · 常规使用（例行）</h4>")
	writeHTML(b, "<p><strong>怎么看：</strong>花得少，浪费也少。</p>")
	writeHTML(b, "<p><strong>怎么做：</strong>继续保持。</p>")
	writeHTML(b, "</div>")

	writeHTML(b, "</div>")
}

// -----------------------------------------------------------------------------
// 4.1 岗位分面（按岗位汇总问题，采用表格 + 影响面，便于领导快速读懂）
// -----------------------------------------------------------------------------

// writePositionFacets §4.1「岗位差异大时按岗位分面」的面向领导版本。
// 不再是多张拥挤的小散点图，而是把成员按岗位分组，给出每类岗位的：
//
//	岗位 · 人数 · 需关注人数 · 主要问题 · 问题消耗占比 · 归因浪费 · 一句话建议，
//
// 并在下方列出每类岗位的典型成员（姓名 + 主导问题 + 归因），让领导一眼看清
// 「问题集中在哪个岗位 / 哪几个人 / 浪费了多少钱 / 该怎么管」。
func writePositionFacets(b *strings.Builder, r *report.Report, me *report.MemberEvaluation) {
	if me == nil || len(me.Members) == 0 {
		return
	}
	if r.Meta.Level != report.LevelDepartment {
		return // 岗位分面只对部门报告有意义；工作室已用部门维度呈现，不再按岗位分面。
	}

	// 1. 按岗位分组（空岗位归入「未标注」）。
	type facetGroup struct {
		position string
		members  []report.MemberPoint
	}
	var order []string
	byPos := make(map[string][]report.MemberPoint)
	for _, m := range me.Members {
		pos := strings.TrimSpace(m.Position)
		if pos == "" {
			pos = "未标注"
		}
		if _, ok := byPos[pos]; !ok {
			order = append(order, pos)
		}
		byPos[pos] = append(byPos[pos], m)
	}
	if len(order) < 2 {
		return // 只有单一岗位，无需分面。
	}

	// 2. 汇总每个岗位：人数、需关注人数、主导问题、归因金额、建议。
	//    需关注 = 严重异常待核查 / 右上优先改善 / 左上习惯辅导。
	type facetSummary struct {
		position       string
		total          int
		problem        int
		problemMembers []report.MemberPoint
		dominant       string // 该岗位问题归因最高的主导类型
		dominantCost   float64
		attrCost       float64 // 该岗位五人归因合计（不含严重异常）
	}
	var summary []facetSummary
	groupSort := func(a, b facetSummary) bool { return a.position < b.position }

	for _, pos := range order {
		grp := byPos[pos]
		var sum facetSummary
		sum.position = pos
		sum.total = len(grp)

		// 主导问题与归因只统计「需关注」成员（严重异常/右上/左上），因为他们才是真正的问题人。
		// 归因金额优先取 DominantIssueCostUSD（各成员五类归因中最高的一项），缺失时回退 IssueAttributedCostUSD。
		issueCost := map[string]float64{}
		for _, m := range grp {
			if !isProblemMember(m) {
				continue
			}
			sum.problem++
			sum.problemMembers = append(sum.problemMembers, m)

			cost := m.DominantIssueCostUSD
			if cost <= 0 {
				cost = m.IssueAttributedCostUSD
			}
			if cost > 0 {
				sum.attrCost += cost
			}
			if m.DominantIssue != "" && m.DominantIssue != report.IssueTypeNormal {
				issueCost[m.DominantIssue] += cost
			}
		}

		// 主导问题 = 该岗位归因金额最高的类型；金额全为 0 时取出现次数最多的类型。
		best := ""
		var bestCost float64
		for iss, c := range issueCost {
			if c > bestCost {
				best, bestCost = iss, c
			}
		}
		if best == "" {
			// 所有问题人金额都为 0：按主导类型出现次数兜底。
			count := map[string]int{}
			for _, m := range sum.problemMembers {
				if m.DominantIssue != "" && m.DominantIssue != report.IssueTypeNormal {
					count[m.DominantIssue]++
				}
			}
			topCount := 0
			for iss, n := range count {
				if n > topCount {
					best, topCount = iss, n
				}
			}
		}
		sum.dominant = best
		sum.dominantCost = bestCost
		summary = append(summary, sum)
	}
	sort.SliceStable(summary, func(i, j int) bool { return groupSort(summary[i], summary[j]) })

	// 3. 渲染「按岗位看问题」汇总表。
	writeHTML(b, "<details class=\"facet-block\">")
	writeHTML(b, "<summary class=\"facet-heading\">4.1 补充 · 按岗位看问题（展开明细）</summary>")
	writeHTML(b, "<div class=\"quadrant-meta\" style=\"margin-bottom:8px;\">把部门成员按岗位分组，一眼看出问题集中在哪类岗位、多少人、浪费了多少，以及该怎么管理。</div>")

	totalProblem := 0
	for _, s := range summary {
		totalProblem += s.problem
	}
	if totalProblem == 0 {
		writeHTML(b, "<p class=muted>各岗位成员使用均正常，无需按岗位专项干预。</p>")
		writeHTML(b, "</details>\n")
		return
	}

	writeHTML(b, "<table class=\"num compact position-table\">")
	writeHTML(b, "<tr><th>岗位</th><th>人数</th><th>需关注</th><th>主要问题</th><th>问题归因</th><th>一句话建议</th></tr>")
	for _, s := range summary {
		needClass := "ok"
		if s.problem > 0 {
			needClass = "warn"
		}
		issueName := "—"
		if s.dominant != "" {
			issueName = issueDisplayName(s.dominant)
		}
		advice := clusterAdvice(s.dominant)
		writeHTML(b, "<tr>")
		writeHTML(b, "<td><strong>%s</strong></td>", html.EscapeString(s.position))
		writeHTML(b, "<td>%d</td>", s.total)
		writeHTML(b, "<td><span class=\"vbadge %s\">%d</span></td>", needClass, s.problem)
		writeHTML(b, "<td>%s</td>", html.EscapeString(issueName))
		writeHTML(b, "<td class=money>%s</td>", moneyWithReport(r, s.attrCost))
		writeHTML(b, "<td style=\"text-align:left;font-size:12px;\">%s</td>", html.EscapeString(advice))
		writeHTML(b, "</tr>")
	}
	writeHTML(b, "</table>")

	// 4. 仅对「有问题」的岗位，展开列出具体成员，便于直达责任人。
	writeHTML(b, "<div class=\"facet-members-block\">")
	for _, s := range summary {
		if s.problem == 0 {
			continue
		}
		writeHTML(b, "<details class=\"facet-members-card\">")
		writeHTML(b, "<summary>%s · %d 人中 %d 人需关注 · 主要是「%s」</summary>",
			html.EscapeString(s.position), s.total, s.problem, html.EscapeString(issueNameOf(s.dominant)))
		writeHTML(b, "<table class=\"num compact\">")
		writeHTML(b, "<tr><th>姓名</th><th>象限</th><th>总成本</th><th>主导问题</th><th>归因费用</th><th>管理建议</th></tr>")
		for _, m := range s.problemMembers {
			// 归因费用优先取 DominantIssueCostUSD，缺失时回退 IssueAttributedCostUSD，与汇总表口径一致。
			memberAttr := m.DominantIssueCostUSD
			if memberAttr <= 0 {
				memberAttr = m.IssueAttributedCostUSD
			}
			advice := defaultAdvice(m.DominantIssue, m.Quadrant, m.SafetyAnomaly)
			safetyBadge := ""
			if m.SafetyAnomaly {
				safetyBadge = " <span class=\"vbadge bad\">待核查</span>"
			}
			writeHTML(b, "<tr><td><strong>%s</strong>%s</td><td>%s</td><td class=money>%s</td><td>%s</td><td class=money>%s</td><td style=\"text-align:left;font-size:12px;\">%s</td></tr>",
				html.EscapeString(m.Name), safetyBadge,
				quadrantDisplayName(m.Quadrant),
				moneyWithReport(r, m.CostUSD),
				issueDisplayName(m.DominantIssue),
				moneyWithReport(r, memberAttr),
				html.EscapeString(advice),
			)
		}
		writeHTML(b, "</table>")
		writeHTML(b, "</details>")
	}
	writeHTML(b, "</div>")

	writeHTML(b, "</details>\n")
}

// issueNameOf 返回规范的名称（同 issueDisplayName 的兜底）。
func issueNameOf(issue string) string {
	if issue == "" {
		return "用法正常"
	}
	return issueDisplayName(issue)
}

// -----------------------------------------------------------------------------
// K-Means 分群可视化 (Section 4.2)
// -----------------------------------------------------------------------------

func writeClusterSection(b *strings.Builder, r *report.Report, me *report.MemberEvaluation) {
	clusters := me.Clusters
	if len(clusters) == 0 && len(me.Members) > 0 {
		inputs := make([]report.MemberQuadrantInput, 0, len(me.Members))
		for _, m := range me.Members {
			in := report.MemberQuadrantInput{
				UserID:          m.UserID,
				Name:            m.Name,
				Position:        m.Position,
				DeptID:          m.DeptID,
				DeptName:        m.DeptName,
				CostUSD:         m.CostUSD,
				Calls:           m.Calls,
				SafetyAnomaly:   m.SafetyAnomaly,
				SuspectedOnly:   m.SuspectedOnly,
				IssueAttributed: make(map[string]float64),
			}
			for _, u := range m.UsageIssues {
				in.IssueAttributed[u.Type] = u.AttributedCostUSD
			}
			if len(in.IssueAttributed) == 0 && m.DominantIssue != "" && m.DominantIssue != report.IssueTypeNormal {
				in.IssueAttributed[m.DominantIssue] = m.DominantIssueCostUSD
			}
			inputs = append(inputs, in)
		}
		clusters = report.ClusterMembers(inputs, report.DefaultClusterParams())
	}

	if len(clusters) == 0 {
		writeHTML(b, "<p class=muted>暂无分群数据。</p>")
		return
	}

	memberMap := make(map[string]report.MemberPoint, len(me.Members))
	for _, m := range me.Members {
		memberMap[m.UserID] = m
	}

	writeHTML(b, "<details class=\"cluster-container\" style=\"margin-top:20px;\">")
	writeHTML(b, "<summary style=\"font-size:14px;color:#1e3a5f;font-weight:600;margin-bottom:8px;cursor:pointer;\">4.2 成员问题相似度聚类分群图（需要时展开）</summary>")
	writeHTML(b, "<div class=muted style=\"margin-bottom:10px;\">把问题相似的人放在一起，方便统一治理策略。</div>")

	// 分群占比条。
	totalMembers := len(me.Members)
	writeHTML(b, "<div class=\"cluster-dist-bar\">")
	for _, cl := range clusters {
		pct := 0.0
		if totalMembers > 0 {
			pct = float64(len(cl.MemberIDs)) / float64(totalMembers) * 100.0
		}
		color := dominantIssueColor(cl.Label)
		writeHTML(b, "<div class=\"dist-seg\" style=\"width: %.1f%%; background: %s;\" title=\"%s: %d人 (%.1f%%)\"></div>",
			pct, color, issueDisplayName(cl.Label), len(cl.MemberIDs), pct)
	}
	writeHTML(b, "</div>")

	// 聚类卡片。
	for i, cl := range clusters {
		color := dominantIssueColor(cl.Label)
		title := fmt.Sprintf("簇 %c · %s（%d 人）", 'A'+i, issueDisplayName(cl.Label), len(cl.MemberIDs))
		advice := clusterAdvice(cl.Label)

		var clusterCost, clusterAttrCost float64
		for _, uid := range cl.MemberIDs {
			if m, ok := memberMap[uid]; ok {
				clusterCost += m.CostUSD
				clusterAttrCost += m.DominantIssueCostUSD
			}
		}

		writeHTML(b, "<div class=\"cluster-card\" style=\"border-left: 4px solid %s;\">", color)
		writeHTML(b, "<div class=\"cluster-header\">")
		writeHTML(b, "<div class=\"cluster-title\"><span class=\"legend-color-dot\" style=\"background:%s;\"></span>%s</div>",
			color, html.EscapeString(title))
		writeHTML(b, "<div class=\"cluster-stat\">该簇总成本: %s · 问题归因: %s</div>",
			moneyWithReport(r, clusterCost), moneyWithReport(r, clusterAttrCost))
		writeHTML(b, "</div>")

		writeHTML(b, "<div class=\"cluster-advice\"><strong>统一治理策略</strong>：%s</div>", html.EscapeString(advice))

		writeHTML(b, "<div class=\"cluster-members\">")
		for _, uid := range cl.MemberIDs {
			m, ok := memberMap[uid]
			if !ok {
				continue
			}
			cls := "member-chip"
			sym := "●"
			if m.SuspectedOnly {
				cls += " suspected"
				sym = "◉"
			} else {
				cls += " confirmed"
			}
			if m.SafetyAnomaly {
				cls += " safety"
				sym = "⊗"
			}

			writeHTML(b, "<span class=\"%s\" title=\"成本: %s · 归因: %s\">%s %s <span style=\"color:#64748b;\">%s</span></span>",
				cls,
				moneyWithReport(r, m.CostUSD),
				moneyWithReport(r, m.DominantIssueCostUSD),
				sym,
				html.EscapeString(m.Name),
				moneyWithReport(r, m.DominantIssueCostUSD),
			)
		}
		writeHTML(b, "</div>")

		writeHTML(b, "</div>")
	}
	writeHTML(b, "</details>")
}

// -----------------------------------------------------------------------------
// 展开卡片：问题成员详细剖析卡
// -----------------------------------------------------------------------------

func writeMemberProblemExpandCard(b *strings.Builder, r *report.Report, p report.MemberPoint) {
	rankClass := "priority"
	quadBadgeClass := "priority"
	if p.Quadrant == report.QuadrantCoach {
		rankClass = "coach"
		quadBadgeClass = "coach"
	}
	if p.SafetyAnomaly {
		rankClass = "safety"
	}

	deptText := ""
	if dept := report.MemberDeptLabel(p); dept != "" && dept != "—" {
		deptText = fmt.Sprintf(" · 部门: %s", html.EscapeString(dept))
	}
	safetyBadge := ""
	if p.SafetyAnomaly {
		safetyBadge = "<span class=\"vbadge bad\">严重异常·待核查</span>"
	}

	advice := defaultAdvice(p.DominantIssue, p.Quadrant, p.SafetyAnomaly)

	writeHTML(b, "<details class=\"member-detail-card %s\">", rankClass)
	writeHTML(b, "<summary class=\"member-summary-bar\">")
	writeHTML(b, "<div class=\"summary-left\"><span class=\"member-name-tag\">%s</span> <span class=\"pos-tag\">%s</span>%s %s</div>",
		html.EscapeString(p.Name), html.EscapeString(p.Position), deptText, safetyBadge)
	writeHTML(b, "<div class=\"summary-right\"><span class=\"badge-quadrant %s\">%s</span> 花费 <strong>%s</strong> &nbsp;·&nbsp; 主要问题：%s <span class=\"expand-arrow\">▼</span></div>",
		quadBadgeClass,
		quadrantDisplayName(p.Quadrant),
		moneyWithReport(r, p.CostUSD),
		issueDisplayName(p.DominantIssue),
	)
	writeHTML(b, "</summary>")

	writeHTML(b, "<div class=\"member-detail-body\">")
	writeHTML(b, "<div class=\"detail-grid\">")
	writeHTML(b, "<div class=\"kpi-sub-item\"><strong>总成本</strong>：%s（相对组织中位 %.2f×）</div>", moneyWithReport(r, p.CostUSD), p.CostMultiple)
	writeHTML(b, "<div class=\"kpi-sub-item\"><strong>象限归属</strong>：%s</div>", quadrantDisplayName(p.Quadrant))
	writeHTML(b, "<div class=\"kpi-sub-item\"><strong>五类问题消耗占比</strong>：%.1f%%（进四象限 Y 轴）</div>", p.IssueCostRatio*100)
	writeHTML(b, "<div class=\"kpi-sub-item\"><strong>主导问题</strong>：%s（归因 %s）</div>", issueDisplayName(p.DominantIssue), moneyWithReport(r, p.DominantIssueCostUSD))
	if p.SafetyAnomaly {
		writeHTML(b, "<div class=\"kpi-sub-item\" style=\"color:#b91c1c;\"><strong>严重异常</strong>：待核查（红线：仅供管理核查，不计入 Y 轴）</div>")
	}
	writeHTML(b, "<div class=\"advice-box\" style=\"grid-column: 1 / -1;\"><strong>针对性管理建议</strong>：%s</div>", html.EscapeString(advice))
	writeHTML(b, "</div>")

	// 若包含具体的使用问题，展示明细。
	if len(p.UsageIssues) > 0 {
		writeHTML(b, "<div class=\"inner-issues-block\">")
		writeHTML(b, "<h4 style=\"font-size:12.5px;color:var(--text-muted);margin-bottom:8px;\">五类情况命中明细：</h4>")
		for _, u := range p.UsageIssues {
			if u.Verdict == report.VerdictNotHit || u.Verdict == report.VerdictNotComputed {
				continue
			}
			vText := "证据较充分"
			if u.Verdict == report.VerdictSuspected {
				vText = "需进一步确认"
			}
			narrative := u.Narrative
			if narrative == nil {
				built := report.BuildIssueNarrative(u)
				narrative = &built
			}
			stats := make([]string, 0, 2)
			if u.RequestCount > 0 {
				stats = append(stats, fmt.Sprintf("涉及请求 %d 个", u.RequestCount))
			}
			if u.SessionCount > 0 {
				stats = append(stats, fmt.Sprintf("涉及会话 %d 个", u.SessionCount))
			}
			writeHTML(b, "<div class=\"inner-issue-row\">")
			writeHTML(b, "<div><strong>%s</strong> <span class=\"vbadge\">%s</span> &nbsp;·&nbsp; 估算问题费用: <strong>%s</strong> &nbsp;·&nbsp; 涉及调用总费用: %s%s</div>",
				issueDisplayName(u.Type), vText, moneyWithReport(r, u.AttributedCostUSD), moneyWithReport(r, u.AffectedCostUSD), readableStats(stats))
			writeHTML(b, "<div class=\"inner-why\"><strong>为什么这样判断：</strong>%s</div>", html.EscapeString(narrative.Reason))
			if narrative.SemanticReview != "" {
				writeHTML(b, "<div class=\"inner-why semantic-review\"><strong>模型复核：</strong>%s</div>", html.EscapeString(narrative.SemanticReview))
			}
			writeHTML(b, "<div class=\"inner-why\"><strong>可能影响：</strong>%s</div>", html.EscapeString(narrative.Impact))
			writeHTML(b, "</div>")
		}
		writeHTML(b, "</div>")
	}

	writeHTML(b, "</div>")
	writeHTML(b, "</details>\n")
}

func readableStats(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	return " &nbsp;·&nbsp; " + strings.Join(parts, "，")
}

func writePersonalSafety(b *strings.Builder, r *report.Report) {
	sa := r.SafetyAnomaly
	if !sa.Hit {
		return
	}
	writeHTML(b, "<section id=\"safety-review\" class=safety><h2>2. 严重异常（待核查）</h2>")
	{
		status := "待核查"
		if sa.ReviewTag != "" && sa.ReviewTag != "待核查" {
			status = fmt.Sprintf("待核查（%s）", sa.ReviewTag)
		}
		writeHTML(b, "<p>命中：未备案非工作时段占比 %.1f%% · 金额 %s · 模型 %s · Key %s · 状态 <span class=\"vbadge bad\">%s</span></p>",
			sa.UnrelatedCostRatio*100, moneyWithReport(r, sa.UnrelatedCostUSD), escOrDash(sa.TopModel), html.EscapeString(report.DisplayKey(sa.TopKey)), html.EscapeString(status))
		narrative := report.BuildSafetyNarrative(sa)
		writeHTML(b, "<div class=\"safety-explanation\"><p><strong>触发原因：</strong>%s</p><p><strong>内容复核：</strong>%s</p><p><strong>如何理解：</strong>%s</p><p><strong>需要核实：</strong>%s</p></div>",
			html.EscapeString(narrative.Trigger), html.EscapeString(narrative.ContentReview), html.EscapeString(narrative.Interpretation), html.EscapeString(narrative.Action))
		if len(sa.ContentSamples) > 0 {
			var sb strings.Builder
			sb.WriteString("内容样本（脱敏）：<br>")
			for i, smp := range sa.ContentSamples {
				fmt.Fprintf(&sb, "<span class=sample>%d. %s</span><br>", i+1, html.EscapeString(smp))
			}
			writeHTML(b, "<div class=samples style=\"margin-top:6px;font-size:12px;color:#555;\">%s</div>", sb.String())
		}
		if len(sa.Excluded) > 0 {
			writeHTML(b, "<div class=muted style=\"margin-top:4px;\">排除：%s</div>", html.EscapeString(strings.Join(sa.Excluded, "；")))
		}
	}
	writeHTML(b, "</section>\n")
}

func writeSafetyReasonRow(b *strings.Builder, safety report.SafetyAnomaly, colspan int) {
	narrative := report.BuildSafetyNarrative(safety)
	writeHTML(b, "<tr class=\"safety-reason-row\"><td colspan=\"%d\"><div class=\"safety-reason-grid\"><p><strong>触发原因：</strong>%s</p><p><strong>内容复核：</strong>%s</p><p><strong>如何理解：</strong>%s</p><p><strong>需要核实：</strong>%s</p></div></td></tr>",
		colspan, html.EscapeString(narrative.Trigger), html.EscapeString(narrative.ContentReview), html.EscapeString(narrative.Interpretation), html.EscapeString(narrative.Action))
}

func writePersonalIssues(b *strings.Builder, r *report.Report) {
	writeHTML(b, "<section><h2>4. 效率与情况（五种情况）</h2>")
	for _, it := range report.IssueTypeOrder {
		var u *report.UsageIssue
		for i := range r.UsageIssues {
			if r.UsageIssues[i].Type == it {
				u = &r.UsageIssues[i]
				break
			}
		}
		writeIssueCard(b, r, u)
	}
	writeHTML(b, "</section>\n")
}

func writeIssueCard(b *strings.Builder, r *report.Report, u *report.UsageIssue) {
	if u == nil {
		return
	}
	name := issueDisplayName(u.Type)
	verdict := map[string]string{
		report.VerdictConfirmed: "证据较充分", report.VerdictSuspected: "疑似",
		report.VerdictNotHit: "未命中", report.VerdictNotComputed: "无法计算",
	}[u.Verdict]
	if verdict == "" {
		verdict = u.Verdict
	}
	cls := "card " + u.Verdict
	writeHTML(b, "<div class=\"%s\"><h3>%s <span class=vbadge>%s</span></h3>", cls, escOrDash(name), escOrDash(verdict))
	narrative := u.Narrative
	if narrative == nil {
		built := report.BuildIssueNarrative(*u)
		narrative = &built
	}
	if u.Verdict == report.VerdictNotHit || u.Verdict == report.VerdictNotComputed {
		writeHTML(b, "<p class=why>%s %s</p>", html.EscapeString(narrative.Finding), html.EscapeString(narrative.Reason))
		writeHTML(b, "</div>\n")
		return
	}
	writeHTML(b, "<div class=plain-explanation><p><strong>发现：</strong>%s</p><p><strong>为什么：</strong>%s</p></div>", html.EscapeString(narrative.Finding), html.EscapeString(narrative.Reason))
	if u.RequestCount > 0 || u.SessionCount > 0 {
		parts := []string{}
		if u.RequestCount > 0 {
			parts = append(parts, fmt.Sprintf("命中请求 %d · 占比 %.1f%%", u.RequestCount, u.RequestShare*100))
		}
		if u.SessionCount > 0 {
			parts = append(parts, fmt.Sprintf("命中会话 %d · 占比 %.1f%%", u.SessionCount, u.SessionShare*100))
		}
		writeHTML(b, "<div class=hit>%s</div>", html.EscapeString(strings.Join(parts, " · ")))
	}
	if u.AttributedCostUSD > 0 || u.AffectedCostUSD > 0 {
		writeHTML(b, "<div class=hit>估算多花费用 %s · 涉及请求总费用 %s</div>", moneyWithReport(r, u.AttributedCostUSD), moneyWithReport(r, u.AffectedCostUSD))
	}
	if narrative.SemanticReview != "" {
		writeHTML(b, "<div class=\"evidence-box semantic-note\"><div class=\"evidence-title\">模型复核</div><div class=\"evidence-body\">%s</div></div>", html.EscapeString(narrative.SemanticReview))
	}
	writeHTML(b, "<div class=impact><strong>这意味着：</strong>%s</div>", html.EscapeString(narrative.Impact))
	if len(u.Excluded) > 0 {
		writeHTML(b, "<div class=muted>已排除：%s</div>", html.EscapeString(strings.Join(report.ClassifyExcluded(u.Excluded), "；")))
	}
	if narrative.Advice != "" {
		writeHTML(b, "<div class=\"action-item-row\" style=\"margin-top: 10px;\"><span class=\"action-badge\">建议</span><span class=\"action-text\">%s</span></div>", html.EscapeString(narrative.Advice))
	}
	writeHTML(b, "</div>\n")
}

func writePrev(b *strings.Builder, cb report.CostBreakdown) {
	if cb.Prev == nil {
		return
	}
	cur := cb.Total()
	prev := cb.Prev.UncachedInputCostUSD + cb.Prev.CacheCostUSD + cb.Prev.OutputCostUSD
	writeHTML(b, "<section><h2>3. 成本构成对比</h2>")
	if prev > 0 {
		writeHTML(b, "<p>本周 %s · 上周 %s</p>", money(cur), money(prev))
	} else {
		writeHTML(b, "<p>本周 %s（上周无数据）</p>", money(cur))
	}
	writeHTML(b, "</section>\n")
}

// -----------------------------------------------------------------------------
// 辅助计算与通用格式化
// -----------------------------------------------------------------------------

func dominantIssueColor(issue string) string {
	switch issue {
	case report.IssueTypePrefixPollution:
		return "#2563eb" // 蓝上下文
	case report.IssueTypeToothpaste:
		return "#0d9488" // 青挤牙膏
	case report.IssueTypeMultiTaskSession:
		return "#7c3aed" // 紫混会话
	case report.IssueTypeOutputRunaway:
		return "#ea580c" // 橙输出失控
	case report.IssueTypeOverModel:
		return "#dc2626" // 红选型
	case report.IssueTypeNormal, "":
		return "#94a3b8" // 灰正常
	default:
		return "#94a3b8"
	}
}

func issueDisplayName(issue string) string {
	switch issue {
	case report.IssueTypePrefixPollution:
		return "前缀污染"
	case report.IssueTypeToothpaste:
		return "挤牙膏"
	case report.IssueTypeMultiTaskSession:
		return "多任务不切会话"
	case report.IssueTypeOutputRunaway:
		return "输出失控"
	case report.IssueTypeOverModel:
		return "杀鸡用牛刀"
	case report.IssueTypeNormal, "":
		return "正常"
	default:
		return issue
	}
}

func quadrantDisplayName(q string) string {
	switch q {
	case report.QuadrantPriorityImprove:
		return "右上·优先改善"
	case report.QuadrantCoach:
		return "左上·习惯辅导"
	case report.QuadrantHighCostNormal:
		return "右下·高用量·正常"
	case report.QuadrantRoutine:
		return "左下·常规使用"
	default:
		return q
	}
}

func clusterAdvice(label string) string {
	switch label {
	case report.IssueTypePrefixPollution:
		return "固定复用系统前缀，别每次重复发送大段内容。"
	case report.IssueTypeToothpaste:
		return "第一次尽量把需求说完整，少来回补充。"
	case report.IssueTypeMultiTaskSession:
		return "新任务开启新会话，不要把不同工作混在一起。"
	case report.IssueTypeOutputRunaway:
		return "提前说明输出格式和字数，避免回答过长。"
	case report.IssueTypeOverModel:
		return "简单任务改用便宜一些的模型。"
	case report.IssueTypeNormal, "":
		return "使用正常，继续保持。"
	default:
		return "先确认具体原因，再针对性调整。"
	}
}

func defaultAdvice(issue, quadrant string, safety bool) string {
	parts := []string{}
	if safety {
		parts = append(parts, "负责人核查备案与凭据排查")
	}
	switch issue {
	case report.IssueTypePrefixPollution:
		parts = append(parts, "固定复用系统前缀")
	case report.IssueTypeToothpaste:
		parts = append(parts, "首轮写清完整需求")
	case report.IssueTypeMultiTaskSession:
		parts = append(parts, "新任务开启新会话")
	case report.IssueTypeOutputRunaway:
		parts = append(parts, "限定输出格式与字数")
	case report.IssueTypeOverModel:
		parts = append(parts, "降级至中轻量模型")
	default:
		if quadrant == report.QuadrantPriorityImprove {
			parts = append(parts, "评估高用量业务匹配度")
		} else {
			parts = append(parts, "保持良好用法")
		}
	}
	return strings.Join(parts, " + ")
}

func splitMembers(me *report.MemberEvaluation) (problem, normal []report.MemberPoint) {
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
	sort.SliceStable(problem, func(i, j int) bool {
		ra := problemRank(problem[i])
		rb := problemRank(problem[j])
		if ra != rb {
			return ra < rb
		}
		if problem[i].IssueCostRatio != problem[j].IssueCostRatio {
			return problem[i].IssueCostRatio > problem[j].IssueCostRatio
		}
		return problem[i].Name < problem[j].Name
	})
	sort.SliceStable(normal, func(i, j int) bool { return normal[i].Name < normal[j].Name })
	return problem, normal
}

func isProblemMember(p report.MemberPoint) bool {
	if p.IsProblemMember || p.SafetyAnomaly || p.Quadrant == report.QuadrantPriorityImprove || p.Quadrant == report.QuadrantCoach {
		return true
	}
	for _, issue := range p.UsageIssues {
		if issue.Verdict == report.VerdictConfirmed {
			return true
		}
	}
	return false
}

func problemRank(p report.MemberPoint) int {
	if p.SafetyAnomaly {
		return 0
	}
	if p.Quadrant == report.QuadrantPriorityImprove {
		return 1
	}
	if p.Quadrant == report.QuadrantCoach {
		return 2
	}
	return 3
}

func safetyMembers(me *report.MemberEvaluation) []report.MemberPoint {
	if me == nil {
		return nil
	}
	var out []report.MemberPoint
	for _, m := range me.Members {
		if m.SafetyAnomaly {
			out = append(out, m)
		}
	}
	return out
}

func memberCalls(members []report.MemberPoint) int {
	total := 0
	for _, m := range members {
		total += m.Calls
	}
	return total
}

func formatInt(n int) string { return formatInt64(int64(n)) }

func formatInt64(n int64) string {
	if n < 10000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000.0)
}

func prevOf(cb report.CostBreakdown) report.CostBreakdownPrev {
	if cb.Prev != nil {
		return *cb.Prev
	}
	return report.CostBreakdownPrev{}
}

func prevTotal(cb report.CostBreakdown) float64 {
	if cb.Prev == nil {
		return 0
	}
	return cb.Prev.UncachedInputCostUSD + cb.Prev.CacheCostUSD + cb.Prev.OutputCostUSD
}

func share(val, total float64) float64 {
	if total <= 0 {
		return 0
	}
	return val / total
}

func deltaPct(prev, cur float64) float64 {
	if prev <= 0 {
		return 0
	}
	return (cur - prev) / prev
}

func money(v float64) string {
	return fmt.Sprintf("$%.2f", v)
}

func moneyWithReport(r *report.Report, v float64) string {
	curr := "USD"
	if r != nil && r.Meta.Currency.Report != "" {
		curr = r.Meta.Currency.Report
	}
	sym := "$"
	if curr == "CNY" || curr == "RMB" {
		sym = "¥"
	}
	return fmt.Sprintf("%s%.2f", sym, v)
}

func escOrDash(s string) string {
	if s == "" {
		return "—"
	}
	return html.EscapeString(s)
}

func writeHTML(b *strings.Builder, format string, a ...any) {
	fmt.Fprintf(b, format, a...)
	b.WriteString("\n")
}

// -----------------------------------------------------------------------------
// CSS 样式定义（自包含、响应式、暗色/亮色自适应）
// -----------------------------------------------------------------------------

const cssStyles = `
:root {
  --bg-page: #f8fafc;
  --bg-card: #ffffff;
  --bg-subtle: #f1f5f9;
  --text-main: #0f172a;
  --text-muted: #64748b;
  --border-color: #e2e8f0;
  --table-border: #f1f5f9;
  --primary-blue: #2563eb;
  --radius-lg: 16px;
  --radius-md: 10px;
  --radius-sm: 6px;
  --shadow-sm: 0 1px 3px 0 rgb(0 0 0 / 0.05), 0 1px 2px -1px rgb(0 0 0 / 0.05);
  --shadow-md: 0 4px 6px -1px rgb(0 0 0 / 0.07), 0 2px 4px -2px rgb(0 0 0 / 0.07);
}

@media (prefers-color-scheme: dark) {
  :root {
    --bg-page: #0b0f19;
    --bg-card: #141b2d;
    --bg-subtle: #1e293b;
    --text-main: #f8fafc;
    --text-muted: #94a3b8;
    --border-color: #243048;
    --table-border: #1e293b;
    --primary-blue: #3b82f6;
    --shadow-sm: 0 1px 3px 0 rgb(0 0 0 / 0.3);
    --shadow-md: 0 4px 6px -1px rgb(0 0 0 / 0.4);
  }
}

* { box-sizing: border-box; margin: 0; padding: 0; }
html { scroll-behavior: smooth; }
.page-nav { position: sticky; top: 0; z-index: 20; display:flex; flex-wrap:wrap; gap:8px; align-items:center; padding:10px 12px; margin-bottom:16px; background:var(--bg-card); border:1px solid var(--border-color); border-radius:var(--radius-md); }
.page-nav a, .page-nav button, .back-top { color:var(--primary-blue); background:transparent; border:1px solid var(--border-color); border-radius:6px; padding:5px 9px; font:inherit; font-size:12px; cursor:pointer; text-decoration:none; }
.technical-section { margin-bottom:22px; }
.technical-section > summary, .facet-block > summary { cursor:pointer; font-weight:600; padding:14px; }
.table-scroll { overflow-x:auto; -webkit-overflow-scrolling:touch; }
.back-top { display:inline-block; margin:8px 0; }
body {
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", sans-serif;
  background: var(--bg-page);
  color: var(--text-main);
  line-height: 1.6;
  -webkit-font-smoothing: antialiased;
}
.report-wrapper {
  max-width: 1040px;
  margin: 0 auto;
  padding: 32px 20px 80px;
}

/* 顶部头部 */
.page-header {
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-lg);
  padding: 24px 28px;
  margin-bottom: 22px;
  box-shadow: var(--shadow-sm);
}
.header-breadcrumb {
  font-size: 13px;
  color: var(--text-muted);
  font-weight: 500;
  margin-bottom: 8px;
}
.header-title-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
}
.header-title, .page-header h1 {
  font-size: 26px;
  font-weight: 700;
  letter-spacing: -0.02em;
  color: var(--text-main);
}
.header-badges {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 14px;
}
.badge-pill {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 12px;
  padding: 4px 11px;
  border-radius: 20px;
  background: var(--bg-subtle);
  color: var(--text-muted);
  font-weight: 500;
  border: 1px solid var(--border-color);
}

/* 卡片区域 */
section {
  background: var(--bg-card);
  border-radius: var(--radius-lg);
  padding: 24px 28px;
  margin-bottom: 22px;
  box-shadow: var(--shadow-sm);
  border: 1px solid var(--border-color);
}
h2 {
  font-size: 17px;
  font-weight: 600;
  letter-spacing: -0.01em;
  color: var(--text-main);
  margin-bottom: 16px;
}
h3 { font-size: 15px; font-weight: 600; margin-bottom: 8px; color: var(--text-main); }
h4 { font-size: 13px; font-weight: 600; }
.muted { color: var(--text-muted); font-size: 13px; margin-top: 6px; }

/* KPI Grid */
.kpi-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(210px, 1fr));
  gap: 12px;
  margin-bottom: 18px;
}
.kpi-card {
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  padding: 16px 18px;
  box-shadow: var(--shadow-sm);
  transition: transform 0.15s ease, box-shadow 0.15s ease;
}
.kpi-card:hover {
  transform: translateY(-2px);
  box-shadow: var(--shadow-md);
}
.kpi-card.highlight {
  background: linear-gradient(135deg, rgba(59, 130, 246, 0.05), rgba(16, 185, 129, 0.05));
  border-color: rgba(59, 130, 246, 0.3);
}
.kpi-label {
  font-size: 12px;
  font-weight: 600;
  color: var(--text-muted);
  letter-spacing: 0.3px;
}
.kpi-val {
  font-size: 24px;
  font-weight: 700;
  color: var(--text-main);
  margin: 6px 0;
  font-variant-numeric: tabular-nums;
}
.kpi-sub {
  font-size: 12px;
  color: var(--text-muted);
  display: flex;
  align-items: center;
  gap: 6px;
}
.kpi-trend {
  font-weight: 600;
  padding: 1px 6px;
  border-radius: 4px;
}
.kpi-trend.down {
  background: #dcfce7;
  color: #15803d;
}
.kpi-trend.up {
  background: #fee2e2;
  color: #b91c1c;
}
.kpi-pill-badge {
  background: var(--bg-subtle);
  padding: 1px 6px;
  border-radius: 4px;
  font-weight: 500;
}

.stat-summary-bar {
  background: var(--bg-subtle);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  padding: 10px 14px;
  margin-bottom: 14px;
  font-size: 13px;
  color: var(--text-main);
}

/* 数据质量 */
.dqgrid { display: grid; grid-template-columns: repeat(auto-fit, minmax(190px, 1fr)); gap: 12px; }
.dqcell { display: flex; align-items: center; gap: 8px; }
.dqname { font-size: 13px; font-weight: 500; white-space: nowrap; width: 62px; color: var(--text-muted); }
.bar { flex: 1; height: 7px; background: var(--bg-subtle); border-radius: 4px; overflow: hidden; border: 1px solid var(--border-color); }
.fill { height: 100%; background: #10b981; border-radius: 4px; }
.fill.warn { background: #f59e0b; }
.fill.bad { background: #ef4444; }
.fill.model { background: #3b82f6; }
.dqval { font-size: 12px; font-weight: 600; color: var(--text-main); width: 44px; text-align: right; }

/* 结论区域 */
.conclusion {
  background: linear-gradient(to right, rgba(16, 185, 129, 0.06), rgba(59, 130, 246, 0.03));
  border: 1px solid rgba(16, 185, 129, 0.3);
  border-left: 4px solid #10b981;
}
.conclusion p.conclusion-text { font-size: 15px; font-weight: 500; line-height: 1.65; color: var(--text-main); }
.meta { font-size: 13px; color: #059669; margin-top: 10px; font-weight: 500; }
.pill-tag { background: rgba(16, 185, 129, 0.15); padding: 2px 8px; border-radius: 12px; }

/* 严重异常置顶卡 */
.safety {
  background: linear-gradient(to right, rgba(239, 68, 68, 0.06), transparent);
  border: 1px solid rgba(239, 68, 68, 0.3);
  border-left: 4px solid #ef4444;
}

/* 表格 */
table.num {
  width: 100%;
  border-collapse: separate;
  border-spacing: 0;
  margin: 12px 0;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  overflow: hidden;
}
table.num th {
  background: var(--bg-subtle);
  color: var(--text-muted);
  font-size: 12px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border-color);
  text-align: right;
}
table.num td {
  padding: 10px 14px;
  border-bottom: 1px solid var(--table-border);
  font-size: 13.5px;
  font-variant-numeric: tabular-nums;
  text-align: right;
}
table.num th:first-child, table.num td:first-child { text-align: left; }
table.num tr:last-child td { border-bottom: none; }
table.num tr:first-child td { font-weight: 600; }
table.num tr:hover td { background: var(--bg-subtle); }
table.compact th, table.compact td { padding: 8px 10px; font-size: 12.5px; }

.warm { color: #b45309; }
.up { color: #dc2626; }
.down { color: #16a34a; }
.money { font-variant-numeric: tabular-nums; }

/* 模型档位 */
.model-tier-block { margin-top: 16px; padding-top: 14px; border-top: 1px dashed var(--border-color); }
.modelrow { display: flex; align-items: center; gap: 10px; margin: 7px 0; }
.mname { width: 220px; font-size: 13px; font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.tier { font-size: 11px; color: #94a3b8; margin-left: 4px; }
.mval { font-size: 12px; color: var(--text-muted); width: 160px; text-align: right; }

/* 个人情况卡片 */
.card {
  border-radius: var(--radius-md);
  padding: 16px 20px;
  margin-bottom: 14px;
  border: 1px solid var(--border-color);
  background: var(--bg-card);
  box-shadow: var(--shadow-sm);
}
.card h3 { display: flex; align-items: center; gap: 8px; }
.card .vbadge { font-size: 12px; font-weight: 600; padding: 2px 8px; border-radius: 12px; background: var(--bg-subtle); color: var(--text-muted); }
.card.confirmed { background: rgba(239, 68, 68, 0.03); border-color: rgba(239, 68, 68, 0.3); }
.card.confirmed .vbadge { background: #ef4444; color: #fff; }
.card.suspected { background: rgba(245, 158, 11, 0.03); border-color: rgba(245, 158, 11, 0.3); }
.card.suspected .vbadge { background: #f59e0b; color: #fff; }

.hit { font-size: 13.5px; margin: 6px 0; }
.plain-explanation { display:grid; gap:6px; margin-top:10px; padding:10px 12px; background:var(--bg-subtle); border-radius:var(--radius-sm); }
.plain-explanation p { margin:0; font-size:13px; line-height:1.6; color:var(--text-main); }
.impact { margin-top:8px; font-size:13px; color:var(--text-main); }
.why { font-size: 13px; color: var(--text-main); background: var(--bg-subtle); border-radius: 6px; padding: 10px 12px; margin-top: 8px; line-height: 1.6; border-left: 3px solid #cbd5e1; }
.semantic-note { font-size: 12.5px; color: #15803d; background: #f0fdf4; border-radius: 6px; padding: 8px 12px; margin-top: 8px; border-left: 3px solid #22c55e; }
.samples { font-size: 12px; color: #92400e; background: #fffbeb; border-left: 3px solid #f59e0b; padding: 8px 12px; margin-top: 8px; border-radius: 0 6px 6px 0; }
.safety-explanation, .safety-reason-grid { display:grid; gap:5px; color:var(--text-main); font-size:12.5px; line-height:1.55; }
.safety-explanation { margin-top:10px; padding:10px 12px; background:rgba(255,255,255,.72); border:1px solid rgba(220,38,38,.16); border-radius:6px; }
.safety-explanation p, .safety-reason-grid p { margin:0; }
.safety-definition { margin:8px 0 12px; padding:9px 11px; background:rgba(255,255,255,.72); border-left:3px solid #f59e0b; color:var(--text-main); font-size:12.5px; line-height:1.55; }
.safety-reason-row td { padding:0 12px 12px !important; text-align:left !important; border-top:0 !important; background:rgba(254,242,242,.42); white-space:normal !important; }
.safety-reason-grid { padding:9px 11px; border-left:3px solid #f59e0b; background:rgba(255,255,255,.68); }

/* 四象限散点图与聚类图 */
.quadrant-meta, .cluster-meta { font-size: 13px; color: var(--text-muted); margin-bottom: 14px; line-height: 1.5; }
.quadrant-svg-wrap {
  background: #ffffff;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  overflow: hidden;
  padding: 16px;
  margin-bottom: 16px;
  box-shadow: var(--shadow-sm);
}
@media (prefers-color-scheme: dark) {
  .quadrant-svg-wrap { background: #111827; }
}
.quadrant-svg { display: block; width: 100%; height: auto; user-select: none; }
.point-group { cursor: pointer; transition: transform 0.15s ease; transform-box: fill-box; transform-origin: center; }
.point-group:hover, .point-group:focus, .point-group.is-linked { transform: scale(1.18); outline: none; filter:drop-shadow(0 0 5px rgba(37, 99, 235, .75)); }
.point-group:target { filter: drop-shadow(0 0 5px rgba(37, 99, 235, .75)); }
.point-index { fill:#fff; stroke:#0f172a; stroke-width:2px; paint-order:stroke fill; font-size:8px; font-weight:800; pointer-events:none; }
.point-name { fill:#0f172a; stroke:#fff; stroke-width:3.5px; stroke-linejoin:round; paint-order:stroke fill; font-size:11px; font-weight:650; pointer-events:none; }
.point-hover-name { opacity:0; fill:#0f172a; stroke:#fff; stroke-width:4px; stroke-linejoin:round; paint-order:stroke fill; font-size:12px; font-weight:700; pointer-events:none; transition:opacity .12s ease; }
.point-group:hover .point-hover-name, .point-group:focus .point-hover-name, .point-group:target .point-hover-name, .point-group.is-linked .point-hover-name { opacity:1; }
.quadrant-scale-note { margin:8px 4px 0; color:var(--text-muted); font-size:12px; line-height:1.5; }
.quadrant-directory { margin:-4px 0 16px; border:1px solid var(--border-color); border-radius:var(--radius-md); overflow:hidden; background:var(--bg-card); }
.quadrant-directory-head { display:flex; align-items:baseline; gap:10px; flex-wrap:wrap; padding:10px 14px; border-bottom:1px solid var(--border-color); background:var(--bg-subtle); }
.quadrant-directory-head strong { font-size:13px; color:var(--text-main); }
.quadrant-directory-head span { font-size:12px; color:var(--text-muted); }
.quadrant-directory-table { margin:0; }
.quadrant-directory-table td, .quadrant-directory-table th { white-space:nowrap; }
.quadrant-directory-table td:nth-child(2), .quadrant-directory-table td:nth-child(7) { text-align:left; }
.quadrant-directory-table .legend-color-dot { margin-right:6px; vertical-align:-1px; }
.member-index-link { display:inline-flex; width:26px; height:24px; align-items:center; justify-content:center; border-radius:4px; background:#e2e8f0; color:#0f172a; font-size:11px; font-weight:800; text-decoration:none; }
.quadrant-directory-row:focus, .quadrant-directory-row:hover, .quadrant-directory-row.is-linked { background:#eff6ff; outline:none; }

/* 4.1 岗位分面（按岗位看问题） */
.facet-block { margin-bottom: 18px; }
.facet-heading { font-size: 13px; font-weight: 600; color: #1e3a5f; margin-bottom: 2px; }
.position-table { margin-top: 4px; }
.position-table td:nth-child(4), .position-table td:nth-child(5) { white-space: nowrap; }
.facet-members-block { margin-top: 14px; display: flex; flex-direction: column; gap: 10px; }
.facet-members-card {
  background: #ffffff;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  box-shadow: var(--shadow-sm);
  overflow: hidden;
}
@media (prefers-color-scheme: dark) {
  .facet-members-card { background: #111827; }
}
.facet-members-card > summary {
  font-size: 13px; font-weight: 600; color: #1e3a5f;
  padding: 10px 14px; cursor: pointer; list-style: none;
  border-bottom: 1px solid var(--border-color);
  background: var(--bg-subtle);
}
.facet-members-card > summary::-webkit-details-marker { display: none; }
.facet-members-card > table { margin: 8px 12px 12px; }

.legend-box {
  background: var(--bg-subtle);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  padding: 14px 18px;
  margin-bottom: 18px;
}
.legend-row { display: flex; flex-wrap: wrap; align-items: center; gap: 14px; font-size: 12px; color: var(--text-main); margin-bottom: 8px; }
.legend-row:last-child { margin-bottom: 0; }
.legend-title { font-weight: 600; color: var(--text-main); margin-right: 4px; }
.legend-item { display: inline-flex; align-items: center; gap: 5px; }
.legend-color-dot { width: 10px; height: 10px; border-radius: 50%; display: inline-block; }

.guide-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(210px, 1fr)); gap: 12px; margin-bottom: 18px; }
.guide-card { border-radius: var(--radius-md); padding: 12px 14px; font-size: 12px; line-height: 1.5; border: 1px solid transparent; }
.guide-card.priority { background: #fef2f2; border-left: 4px solid #ef4444; color: #991b1b; }
.guide-card.coach { background: #fffbeb; border-left: 4px solid #f59e0b; color: #92400e; }
.guide-card.high-normal { background: #eff6ff; border-left: 4px solid #3b82f6; color: #1e40af; }
.guide-card.routine { background: #f0fdf4; border-left: 4px solid #22c55e; color: #166534; }
@media (prefers-color-scheme: dark) {
  .guide-card.priority { background: #2d1515; color: #fca5a5; }
  .guide-card.coach { background: #2e200c; color: #fde68a; }
  .guide-card.high-normal { background: #172554; color: #93c5fd; }
  .guide-card.routine { background: #132a1c; color: #86efac; }
}

.cluster-dist-bar { display: flex; height: 16px; border-radius: 8px; overflow: hidden; background: #e2e8f0; margin: 12px 0 18px; box-shadow: inset 0 1px 2px rgba(0,0,0,0.1); }
.dist-seg { height: 100%; transition: opacity 0.2s; }
.dist-seg:hover { opacity: 0.85; }

.cluster-card {
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  padding: 14px 16px;
  margin-bottom: 12px;
  box-shadow: var(--shadow-sm);
}
.cluster-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; }
.cluster-title { font-size: 13.5px; font-weight: 600; display: flex; align-items: center; gap: 6px; }
.cluster-stat { font-size: 12px; color: var(--text-muted); font-weight: 500; }
.cluster-advice { font-size: 12px; color: var(--text-main); background: var(--bg-subtle); border-radius: 6px; padding: 8px 12px; margin-bottom: 10px; border-left: 3px solid #3b82f6; }
.cluster-members { display: flex; flex-wrap: wrap; gap: 6px; }
.member-chip {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 3px 10px;
  border-radius: 14px;
  font-size: 12px;
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  font-weight: 500;
}
.member-chip.confirmed { border-color: #94a3b8; }
.member-chip.suspected { border-style: dashed; border-color: #94a3b8; }
.member-chip.safety { border-color: #ef4444; background: #fef2f2; color: #991b1b; }
@media (prefers-color-scheme: dark) { .member-chip.safety { background: #2d1515; color: #fca5a5; } }

/* 问题成员展开卡 */
.member-detail-card {
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  margin-bottom: 12px;
  overflow: hidden;
  background: var(--bg-card);
  box-shadow: var(--shadow-sm);
  transition: all 0.2s ease;
}
.member-detail-card:hover { border-color: #cbd5e1; }
.member-detail-card.priority { border-left: 4px solid #ef4444; }
.member-detail-card.coach { border-left: 4px solid #f59e0b; }
.member-detail-card.safety { border-left: 4px solid #dc2626; }

.member-summary-bar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 18px;
  cursor: pointer;
  user-select: none;
  background: var(--bg-card);
  font-size: 13.5px;
  transition: background 0.15s ease;
}
.member-summary-bar:hover { background: var(--bg-subtle); }
.summary-left { display: flex; align-items: center; gap: 8px; }
.summary-right { display: flex; align-items: center; gap: 12px; font-size: 12.5px; color: var(--text-muted); }
.badge-quadrant { font-weight: 600; padding: 2px 8px; border-radius: 4px; font-size: 12px; }
.badge-quadrant.priority { background: #fee2e2; color: #b91c1c; }
.badge-quadrant.coach { background: #fef3c7; color: #b45309; }
.pos-tag { font-size: 11.5px; color: var(--text-muted); background: var(--bg-subtle); padding: 1px 6px; border-radius: 4px; }
.expand-arrow { font-size: 10px; color: #94a3b8; transition: transform 0.2s; }
details[open] .expand-arrow { transform: rotate(180deg); }

.member-detail-body { padding: 16px 18px; border-top: 1px solid var(--border-color); font-size: 13px; background: var(--bg-card); }
.detail-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 10px; line-height: 1.6; }
.kpi-sub-item { background: var(--bg-subtle); border-radius: 6px; padding: 8px 10px; }
.advice-box { background: rgba(59, 130, 246, 0.06); border-radius: 6px; padding: 10px 12px; border-left: 3px solid #3b82f6; }
.inner-issues-block { border-top: 1px dashed var(--border-color); padding-top: 12px; margin-top: 12px; }
.inner-issue-row { background: var(--bg-subtle); border-radius: 6px; padding: 10px 12px; margin-bottom: 8px; }
.inner-why { font-size: 12px; color: var(--text-muted); margin-top: 4px; line-height: 1.5; }

.vbadge { font-size: 11px; font-weight: 600; padding: 2px 7px; border-radius: 10px; }
.vbadge.bad { background: #cf5b5b; color: #fff; }
.vbadge.warn { background: #d9a13c; color: #fff; }
.vbadge.ok { background: #52a06e; color: #fff; }

.summary-toggle { font-size: 13px; font-weight: 600; cursor: pointer; color: #2c5f8a; padding: 6px 0; }
@media (prefers-color-scheme: dark) { .summary-toggle { color: #38bdf8; } }

/* --- v0.7 Personal Health Dashboard Styles --- */
.personal-health-header {
  border-bottom: 2px solid var(--border-color);
  padding-bottom: 16px;
  margin-bottom: 20px;
}
.header-breadcrumb {
  font-size: 12px;
  color: var(--text-muted);
  margin-bottom: 6px;
  font-weight: 500;
}
.badge-grade {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 14px;
  border-radius: 9999px;
  font-weight: 700;
  font-size: 13px;
  line-height: 1.2;
}
.badge-grade.grade-a {
  background: #dcfce7;
  color: #15803d;
  border: 1px solid #86efac;
}
.badge-grade.grade-b {
  background: #fef3c7;
  color: #b45309;
  border: 1px solid #fcd34d;
}
.badge-grade.grade-c {
  background: #fee2e2;
  color: #b91c1c;
  border: 1px solid #fca5a5;
}
.badge-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background-color: currentColor;
  display: inline-block;
}
.highlights-card {
  border-top: 4px solid #10b981;
}
.highlight-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  gap: 12px;
  margin-top: 10px;
}
.hl-item {
  background: var(--bg-subtle);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  padding: 12px 14px;
  transition: border-color 0.15s ease;
}
.hl-item:hover {
  border-color: #10b981;
}
.hl-title {
  font-weight: 600;
  font-size: 13.5px;
  color: var(--text-main);
  margin-bottom: 4px;
}
.hl-desc {
  font-size: 12px;
  color: var(--text-muted);
  line-height: 1.5;
}
.evidence-box {
  margin-top: 10px;
  padding: 10px 14px;
  background: rgba(59, 130, 246, 0.05);
  border: 1px solid rgba(59, 130, 246, 0.2);
  border-left: 3px solid #3b82f6;
  border-radius: var(--radius-sm);
  font-size: 12px;
  line-height: 1.5;
}
.evidence-title {
  font-weight: 600;
  color: #2563eb;
  margin-bottom: 4px;
  font-size: 12px;
}
.evidence-body {
  color: var(--text-main);
  font-size: 12px;
}
.action-card {
  border-left: 4px solid #3b82f6;
}
.action-item-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 10px;
}
.action-item-row {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  background: var(--bg-subtle);
  padding: 10px 14px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border-color);
}
.action-badge {
  padding: 2px 8px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 600;
  background: #dbeafe;
  color: #1e40af;
  flex-shrink: 0;
  line-height: 1.4;
}
.action-text {
  font-size: 12.5px;
  color: var(--text-main);
  line-height: 1.5;
}
@media (prefers-color-scheme: dark) {
  .badge-grade.grade-a {
    background: #064e3b;
    color: #6ee7b7;
    border-color: #047857;
  }
  .badge-grade.grade-b {
    background: #451a03;
    color: #fcd34d;
    border-color: #b45309;
  }
  .badge-grade.grade-c {
    background: #450a0a;
    color: #fca5a5;
    border-color: #b91c1c;
  }
  .evidence-box {
    background: rgba(59, 130, 246, 0.1);
    border-color: rgba(59, 130, 246, 0.3);
  }
  .evidence-title {
    color: #93c5fd;
  }
  .action-badge {
    background: #1e3a8a;
    color: #bfdbfe;
  }
}

@media (max-width: 760px) {
  .report-wrapper { padding: 12px 8px 48px; }
  section, .page-header { padding: 16px 12px; }
  .kpi-grid { grid-template-columns: 1fr; }
  .quadrant-svg { min-width: 720px; }
  .quadrant-svg-wrap { overflow-x:auto; }
	.quadrant-directory-head { align-items:flex-start; flex-direction:column; gap:2px; }
}
@media print {
  .page-nav, .back-top { display:none !important; }
  details > * { display:block !important; }
  details > summary { display:none !important; }
  section { break-inside:avoid; box-shadow:none; }
}

footer {
  text-align: center;
  font-size: 12px;
  color: var(--text-muted);
  margin-top: 40px;
  padding-top: 20px;
  border-top: 1px solid var(--border-color);
}
`
