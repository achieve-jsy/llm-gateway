package derive

// maincause.go —— 部门/工作室【本周结论】主因归因一句话（② 主因归因，derive 层）。
//
// 背景：部门周报【本周结论】此前只有「关注——命中 N 条异常（P1）」，缺主因归因。
// 本模块在三层数据流的事实层之后、渲染之前，基于**已装配事实**确定性计算出主因句：
//   「成本环比 ±X%，集中在 N 名<岗位>，其一命中<异常>」
//
// 纪律（与既有推导一致）：
//   - 只读 rep.Fact.（Department|Studio）的 WoWCostPct / TopChanged[].{JobCategory,WoWCostPct,AnomalyCause}，
//     不直连库、不新增业务口径、不访问 Judgment.Anomalies（主因已由装配层回填到 AnomalyCause）。
//   - 纯函数、幂等、可重复调用：工具侧在 backfillMemberAnomalyCause（方向 B 成员级 A1/A2/A5 归因）之后
//     重跑 FillUnitConclusion，使主因反映最终 AnomalyCause。
//   - 无事实（u==nil）或空结果时 Supported=false / MainCause=""，不产生误导性结论。

import (
	"fmt"
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
//
// ① 成本环比幅度 = u.WoWCostPct（部门本期 vs 上期同口径成员聚合成本）。
// ② 集中人群 = 成本增长（WoWCostPct>0）的 TopChanged 成员按岗位归一后取主流岗位（人数最多；
//
//	人数并列取增速之和最大者，保证可复现），N = 该岗位人数。
//
// ③ 异常命中 = 集中人群（成本增长的 TopChanged）中携带 AnomalyCause 的成员：恰 1 人→「其一命中{label}」；
//
//	多人→「其中 K 人命中{labels}」。
func buildUnitConclusion(u *report.UnitFact) *report.UnitConclusion {
	if u == nil {
		return &report.UnitConclusion{Supported: false, Note: "无单元事实可归因"}
	}
	con := &report.UnitConclusion{Supported: true, CostDeltaPct: u.WoWCostPct}

	// ---- ② 集中人群 ----
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
		con.ConcentrationJob = bestJob
	}

	// ---- ③ 异常命中 ----
	var labels []string
	seen := map[string]bool{}
	causeMembers := 0
	firstCause := ""
	for _, tc := range growth {
		if tc.AnomalyCause == "" {
			continue
		}
		causeMembers++
		if firstCause == "" {
			firstCause = tc.AnomalyCause
		}
		for _, lab := range strings.Split(tc.AnomalyCause, "; ") {
			lab = strings.TrimSpace(lab)
			if lab != "" && !seen[lab] {
				seen[lab] = true
				labels = append(labels, lab)
			}
		}
	}
	con.AnomalyLabels = labels
	switch {
	case causeMembers == 1:
		con.AnomalyWord = "其一命中" + firstCause
	case causeMembers > 1:
		con.AnomalyWord = fmt.Sprintf("其中 %d 人命中%s", causeMembers, strings.Join(labels, "、"))
	}

	// ---- 组装一句话 ----
	con.MainCause = assembleUnitMainCause(con)
	return con
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
		parts = append(parts, fmt.Sprintf("成本环比 %s%.0f%%", sign, v))
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
