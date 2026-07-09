package derive

// maincause.go —— 部门/工作室【本周结论】主因归因一句话（② 主因归因，derive 层）。
//
// 背景：部门周报【本周结论】此前只有「关注——命中 N 条异常（P1）」，缺主因归因。
// 本模块在事实层之后、渲染之前，基于**已装配聚合事实**确定性计算出主因句：
//   「成本环比 ±X%，集中在 N 名<岗位>，命中 <A1/A2/A5>」
//
// 数据来源（全部来自报告聚合，不直连库）：
//   - ① 成本环比幅度 = UnitFact.WoWCostPct（部门/工作室本期 vs 上期同口径成员聚合成本）。
//   - ② 集中人群 = UnitFact.TopChanged 中成本增长（WoWCostPct>0）的成员，按岗位（JobCategory 归一）取主流
//     （人数最多，并列取增速之和最大，保证可复现），N=该岗位人数。
//   - ③ 异常命中 = 该集中人群 TopChanged[].AnomalyCause（A1/A2/A5 判定已由异常层+装配层回填），
//     去重后得规则码集合（如 A1/A2/A5）。
//
// 纪律：纯函数、幂等、可重复调用——工具侧在 backfillMemberAnomalyCause（方向 B 成员级 A1/A2/A5 归因）
// 之后重跑 FillUnitConclusion，使主因反映最终 AnomalyCause。无事实（u==nil）时 Supported=false。

import (
	"fmt"
	"sort"
	"strings"

	"github.com/example/llm-gateway/internal/observability/report"
)

// FillUnitConclusion 计算并回填单元（部门/工作室）的【本周结论】主因归因（Fact.Conclusion）。
// 幂等、可重复调用。
func FillUnitConclusion(rep *report.Report) {
	if rep == nil {
		return
	}
	for _, uf := range []*report.UnitFact{rep.Fact.Department, rep.Fact.Studio} {
		if uf == nil {
			continue
		}
		uf.Conclusion = buildUnitConclusion(uf)
	}
}

// buildUnitConclusion 从单元事实构造 UnitConclusion（纯函数，供单测）。
func buildUnitConclusion(u *report.UnitFact) *report.UnitConclusion {
	if u == nil {
		return &report.UnitConclusion{Supported: false, Note: "无单元事实可归因"}
	}
	con := &report.UnitConclusion{Supported: true, CostDeltaPct: u.WoWCostPct}

	// ---- ② 集中人群：成本增长(WoWCostPct>0)的 TopChanged 成员按岗位归一后取主流 ----
	type jobAgg struct {
		n      int
		sumPct float64
	}
	byJob := map[string]*jobAgg{}
	var growth []report.TopChangedMember
	for _, tc := range u.TopChanged {
		if tc.WoWCostPct <= 0 {
			continue // 仅成本增长成员构成「集中」；下降/持平成员不纳入主因
		}
		growth = append(growth, tc)
		job := tc.JobCategory
		if job == "" {
			job = "未标注"
		}
		a := byJob[job]
		if a == nil {
			a = &jobAgg{}
			byJob[job] = a
		}
		a.n++
		a.sumPct += tc.WoWCostPct
	}
	if len(growth) > 0 {
		bestJob, best := "", (*jobAgg)(nil)
		for j, a := range byJob {
			if best == nil || a.n > best.n || (a.n == best.n && a.sumPct > best.sumPct) {
				bestJob, best = j, a
			}
		}
		con.ConcentrationN = best.n
		// 残差/合并占位岗位名（其他/其它/未知/未分类/空）无信息量：回退到集中人群更具体的岗位名
		// （优先 job_title 原样值）；仍为占位/空则用「未归类岗位」兜底措辞，避免主因输出「集中在 N 名其他」。
		if isResidualJob(bestJob) {
			con.ConcentrationJob = "未归类岗位"
			if specific := specificJobOfSlot(growth, bestJob); specific != "" {
				con.ConcentrationJob = specific
			}
		} else {
			con.ConcentrationJob = bestJob
		}
	}

	// ---- ③ 异常命中：集中人群（成本增长的 TopChanged）的规则码并集 ----
	ruleSet := map[string]bool{}
	var labels []string
	seenLabel := map[string]bool{}
	for _, tc := range growth {
		if tc.AnomalyCause == "" {
			continue
		}
		for _, piece := range strings.Split(tc.AnomalyCause, "; ") {
			piece = strings.TrimSpace(piece)
			if piece == "" {
				continue
			}
			// AnomalyCause 形如 "A1 程序失控" / "A2 成本燃烧" / "A5 非工作时段"：首 token 为规则码。
			if code := ruleCode(piece); code != "" {
				ruleSet[code] = true
			}
			if !seenLabel[piece] {
				seenLabel[piece] = true
				labels = append(labels, piece)
			}
		}
	}
	codes := make([]string, 0, len(ruleSet))
	for c := range ruleSet {
		codes = append(codes, c)
	}
	sort.Strings(codes) // A1 < A2 < ... < A5，稳定可复现
	con.RuleCodes = codes
	con.AnomalyLabels = labels
	if len(codes) > 0 {
		con.AnomalyWord = "命中 " + strings.Join(codes, "/")
	}

	// ---- 组装一句话 ----
	con.MainCause = assembleUnitMainCause(con)
	return con
}

// ruleCode 从 "A<N> 名称" 中提取规则码（取首空格前 token）；不形如 A? 时返回 ""。
func ruleCode(label string) string {
	head := label
	if i := strings.IndexByte(label, ' '); i >= 0 {
		head = label[:i]
	}
	if len(head) == 2 && head[0] == 'A' && head[1] >= '1' && head[1] <= '9' {
		return head
	}
	return ""
}

// isResidualJob 判断岗位名是否为「残差/合并占位」值（其他/其它/未知/未分类/空等）。
// 用于主因②：占位岗位名无信息量，需回退到更具体的岗位名或用「未归类岗位」兜底。
func isResidualJob(s string) bool {
	switch strings.TrimSpace(s) {
	case "", "其他", "其它", "未知", "未分类", "未归类", "未标注", "N/A", "NA", "无", "-", "null", "none":
		return true
	}
	return false
}

// specificJobOfSlot 在被集中人群（成本增长的 TopChanged）中，取归属于 bestJob 槽位成员里
// 第一个非占位的最具体岗位名（TopChangedMember.JobTitle 原样；优先 job_title）。
// 找不到（成员无岗位名/仍占位）返回 ""，由调用方用「未归类岗位」兜底。
func specificJobOfSlot(growth []report.TopChangedMember, bestJob string) string {
	for _, tc := range growth {
		if slotKey(tc.JobCategory) != bestJob {
			continue
		}
		if !isResidualJob(tc.JobTitle) {
			return tc.JobTitle
		}
	}
	return ""
}

// slotKey 岗位槽统计键：空槽（未归一）与 buildUnitConclusion 分组用的「未标注」一致。
func slotKey(job string) string {
	if job == "" {
		return "未标注"
	}
	return job
}

// assembleUnitMainCause 按 ①②③ 拼接主因一句话；无任何可陈述部分时返回空串。
func assembleUnitMainCause(con *report.UnitConclusion) string {
	if con == nil {
		return ""
	}
	var parts []string
	if con.CostDeltaPct != 0 {
		sign := "+"
		v := con.CostDeltaPct
		if v < 0 {
			sign = "-"
			v = -v
		}
		parts = append(parts, fmt.Sprintf("成本环比 %s%.1f%%", sign, v))
	}
	if con.ConcentrationN >= 1 && con.ConcentrationJob != "" {
		if con.ConcentrationN == 1 {
			parts = append(parts, fmt.Sprintf("集中于 1 名%s", con.ConcentrationJob))
		} else {
			parts = append(parts, fmt.Sprintf("集中在 %d 名%s", con.ConcentrationN, con.ConcentrationJob))
		}
	}
	if con.AnomalyWord != "" {
		parts = append(parts, con.AnomalyWord)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "，")
}
