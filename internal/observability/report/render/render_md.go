package render

// render_md.go —— GFM Markdown 输出（只读 Report，不在模板内二次计算）。
//
// 版式：front-matter `facts_digest` 供对账；结论用 `> `；事实用 ``inline``；严重度 [P0..P3]；附录(口径/对账)兜底。

import (
	"fmt"
	"strings"

	"github.com/example/llm-gateway/internal/observability/report"
)

// RenderMD 渲染 Markdown。
func RenderMD(r *report.Report) string {
	var b strings.Builder
	w := func(format string, args ...interface{}) { fmt.Fprintf(&b, format, args...) }

	// front-matter facts_digest（对账用：只列关键数字+口径）
	w("---\n")
	w("facts_digest:\n")
	w("  level: %s\n", r.Meta.Level)
	w("  period: \"%s ~ %s (%s, %dd)\"\n", r.Meta.Period.From, r.Meta.Period.To, r.Meta.Period.Type, r.Meta.Period.Days)
	if r.Fact.Personal != nil {
		w("  req: %d\n", r.Fact.Personal.ReqCount)
		w("  success: %d\n", r.Fact.Personal.SuccessCount)
		w("  total_tokens: %d\n", r.Fact.Personal.TotalTokens) // = prompt+completion+reasoning（不含 cached）
		w("  cost_usd: %.4f\n", r.Fact.Personal.EstCostUSD)
	} else if u := unitFact(r); u != nil {
		w("  req: %d\n", u.Member.ReqCount)
		w("  success: %d\n", u.Member.SuccessCount)
		w("  total_tokens: %d\n", u.Member.TotalTokens)
		w("  cost_usd: %.4f\n", u.Member.EstCostUSD)
		w("  service_req: %d\n", u.ServiceKey.ReqCount)
	}
	w("  total_tokens_note: \"prompt+completion+reasoning（不含 cached/cache_write）\"\n")
	w("  baseline: \"GET /api/v1/usage/dashboard\"\n")
	w("  schema_version: %s\n", r.SchemaVersion)
	w("---\n\n")

	if r.Fact.Personal != nil {
		// Vicky 格式标题：# 个人周报 · 周期 · 姓名（岗位）
		// 岗位：jobSlot 回退 category→family→title；工作室因只有 ID 不展示（避免 UUID）
		title := scopeLabel(r.Meta.Scope)
		s := r.Meta.Scope
		job := jobSlotRender(s.JobCategory, s.JobFamily, s.JobTitle)
		if job != "" {
			title += "（" + job + "）"
		}
		w("# 个人周报 · %s · %s\n\n", r.Meta.Period.From+"~"+r.Meta.Period.To, title)
		w("【本周结论】%s\n\n", buildWeeklyConclusion(r))
	} else if r.Fact.Department != nil {
		// Vicky 部门周报标题：只用部门名，不附 StudioID（UUID 无可读性）
		w("# 部门周报 · %s · %s\n\n", r.Meta.Period.From+"~"+r.Meta.Period.To, scopeLabel(r.Meta.Scope))
		w("【本周结论】%s\n\n", buildDeptConclusion(r))
	} else if r.Fact.Studio != nil {
		w("# 工作室周报 · %s · %s\n\n", r.Meta.Period.From+"~"+r.Meta.Period.To, scopeLabel(r.Meta.Scope))
		w("【本周结论】%s\n\n", buildDeptConclusion(r))
	} else {
		w("# 用量周报 · %s\n\n", r.Expression.Headline)
	}
	w("> **级别**：%s · **范围**：%s\n", levelLabel(r.Meta.Level), scopeLabel(r.Meta.Scope))
	w("> **周期**：%s（%s ~ %s，%d 天）\n", r.Meta.Period.Label, r.Meta.Period.From, r.Meta.Period.To, r.Meta.Period.Days)
	w("> **币种**：%s（USD→CNY %.4f）\n\n", r.Meta.Currency.ReportCurrency, r.Meta.Currency.USDCNYRate)

	w("## 概况\n\n")
	w("| 指标 | 值 |\n|---|---|\n")
	writeFactTable(&b, r)

	if r.Fact.Personal != nil {
		// Vicky 个人周报：纵向基线 / 同岗使用效能画像 / 成本最高模型 / 场景与合理用模
		writePersonalSections(&b, r)
	} else if r.Fact.Department != nil {
		// Vicky 部门周报：6 章模板
		writeDeptSections(&b, r)
	}

	w("\n## 判断\n\n")
	writeJudgment(&b, r)

	// 部门/工作室周报：建议已在第六章输出，跳过通用建议块避免重复
	if r.Fact.Personal != nil {
		w("\n## 建议\n\n")
		for _, g := range r.Expression.Suggestions {
			w("- %s\n", g)
		}
	}

	w("\n## 口径与对账\n\n")
	// 只列一次：打印 QA.Alignment（对账/口径单一来源），去重；缺失才补默认；最后补一条判定性说明。
	seen := map[string]bool{}
	add := func(line string) {
		if strings.TrimSpace(line) == "" || seen[line] {
			return
		}
		seen[line] = true
		w("- %s\n", line)
	}
	for _, al := range r.Meta.QA.Alignment {
		add(al)
	}
	defaults := []string{
		"成功=`status=200`；个人画像/岗位人均排除 `key_type='service'`；部门 Service Key 块仅 `=service` 且归属当前部门。",
		"`total_tokens=prompt+completion+reasoning`（不含 cached/cache_write）；对账基准 `GET /api/v1/usage/dashboard`。",
	}
	for _, d := range defaults {
		covered := false
		pfx := strings.SplitN(d, "；", 2)[0]
		for k := range seen {
			if strings.HasPrefix(k, pfx) {
				covered = true
				break
			}
		}
		if !covered {
			add(d)
		}
	}
	add("判定性分析确定性可复现；A1–A5 阈值为阶段一默认，T4 用真实数据校准。")

	return b.String()
}

func writeFactTable(b *strings.Builder, r *report.Report) {
	if r.Fact.Personal != nil {
		p := r.Fact.Personal
		rate := r.Meta.Currency.USDCNYRate
		// Vicky 一行摘要格式
		fmt.Fprintf(b, "- 调用 %d 次 | 成本 %s | Token %s | 活跃 %d 天 | 用了 %d 个模型\n",
			p.ReqCount, cny(p.EstCostUSD, rate), fmtTokens(p.TotalTokens), p.ActiveDays, p.ModelCount)
		// 耗时行（agg 模式无明细时省略）
		if p.AvgDurationMs > 0 || p.P95DurationMs > 0 || p.AvgTTFTMs > 0 {
			parts := []string{}
			if p.AvgDurationMs > 0 {
				parts = append(parts, fmt.Sprintf("平均耗时 %.1fs", p.AvgDurationMs/1000))
			}
			if p.P95DurationMs > 0 {
				parts = append(parts, fmt.Sprintf("p95 %.1fs", p.P95DurationMs/1000))
			}
			if p.AvgTTFTMs > 0 {
				parts = append(parts, fmt.Sprintf("平均 TTFT %.0fms", p.AvgTTFTMs))
			}
			fmt.Fprintf(b, "- %s\n", strings.Join(parts, " | "))
		}
		// 缓存/输入上下文/每百万成本
		if p.CacheHitRate > 0 || p.AvgInputCtxTokens > 0 || p.CostPer1Million > 0 {
			parts := []string{}
			if p.CacheHitRate > 0 {
				parts = append(parts, "缓存占比 "+pctP(p.CacheHitRate))
			}
			if p.AvgInputCtxTokens > 0 {
				parts = append(parts, fmt.Sprintf("平均输入 %.0fk token", p.AvgInputCtxTokens/1000))
			}
			if p.CostPer1Million > 0 {
				parts = append(parts, "每百万 Token "+cny(p.CostPer1Million, rate))
			}
			fmt.Fprintf(b, "- %s\n", strings.Join(parts, " | "))
		}
		// 周环比
		fmt.Fprintf(b, "- 周环比：调用 %s / 成本 %s / Token %s\n",
			signedPct(p.WoWReqPct), signedPct(p.WoWCostPct), signedPct(p.WoWTokenPct))
	} else if u := unitFact(r); u != nil {
		fmt.Fprintf(b, "| 成员调用 | %d（花名册 %d 人，活跃 %d 人） |\n", u.Member.ReqCount, u.Member.UserCount, u.Member.ActiveUserCount)
		fmt.Fprintf(b, "| 成员成功率 | %s |\n", pct(u.Member.SuccessCount, u.Member.ReqCount))
		fmt.Fprintf(b, "| 成员 Token | %d |\n", u.Member.TotalTokens)
		fmt.Fprintf(b, "| 成员成本 | `%.4f` USD |\n", u.Member.EstCostUSD)
		fmt.Fprintf(b, "| Service Key 调用 | %d |\n", u.ServiceKey.ReqCount)
		fmt.Fprintf(b, "| Service Key Token | %d |\n", u.ServiceKey.TotalTokens)
		fmt.Fprintf(b, "| Service Key 成本 | `%.4f` USD |\n", u.ServiceKey.EstCostUSD)
	}
}

// cny 将 USD 金额按汇率换算并以人民币 ¥ 显示（保留 2 位）。
func cny(usd float64, rate float64) string {
	return fmt.Sprintf("¥%.2f", usd*rate)
}

// signedPct 显示带符号的百分比（周环比用），0 显示为 0.0%。
func signedPct(v float64) string {
	if v == 0 {
		return "0.0%"
	}
	return fmt.Sprintf("%+.1f%%", v)
}

func unitFact(r *report.Report) *report.UnitFact {
	if r.Fact.Department != nil {
		return r.Fact.Department
	}
	return r.Fact.Studio
}

func writeJudgment(b *strings.Builder, r *report.Report) {
	j := &r.Judgment
	// 基线
	fmt.Fprintf(b, "### 28 天基线 `%s`", j.Baseline.Metric)
	if j.Baseline.CurrentWindow != "" {
		fmt.Fprintf(b, "（%s）", j.Baseline.CurrentWindow)
	}
	fmt.Fprintf(b, "\n\n")
	if j.Baseline.Supported {
		fmt.Fprintf(b, "- 样本 %d 天 · 中位数 `%.0f` · MAD `%.0f` · 稳健上界 `%.0f`\n", j.Baseline.SampleDays, j.Baseline.Median, j.Baseline.MAD, j.Baseline.Median+1.4826*j.Baseline.MAD)
		fmt.Fprintf(b, "- 当前 `%.0f` · 处于基线 P%.0f · 偏离σ `%.2f`（趋势 %s）\n", j.Baseline.Current, j.Baseline.Percentile*100, j.Baseline.DevSigma, j.Baseline.Trend)
	} else {
		fmt.Fprintf(b, "- %s\n", j.Baseline.Note)
	}

	// 同岗分位
	fmt.Fprintf(b, "\n### 同岗分位\n\n")
	if j.Peer.Supported {
		fmt.Fprintf(b, "- 指标 `%s` · 池 `%s`（%d 样本）\n", j.Peer.Metric, j.Peer.PoolType, j.Peer.SampleN)
		fmt.Fprintf(b, "- 当前 P%.0f（%s）· P25/P50/P75/P90 `%.0f/%.0f/%.0f/%.0f` · vs 中位 `%s`\n", j.Peer.PercentileRank*100, j.Peer.Band, j.Peer.P25, j.Peer.P50, j.Peer.P75, j.Peer.P90, j.Peer.VsPeer)
	} else {
		fmt.Fprintf(b, "- %s\n", j.Peer.Note)
	}

	// 异常
	fmt.Fprintf(b, "\n### 异常（A1–A5）\n\n")
	for _, a := range j.Anomalies {
		fmt.Fprintf(b, "- [%s] **%s**（%s）\n", a.Rule, a.Name, a.Severity)
		if a.Hit {
			fmt.Fprintf(b, "  - `actual=%.2f` · `threshold=%.2f` · `baseline=%.2f`（%s）\n", a.ActualValue, a.ThresholdValue, a.BaselineValue, a.BaselineWindow)
			if a.ImpactCostUSD > 0 {
				fmt.Fprintf(b, "  - 影响成本 `%.2f` USD · 超支 `%.2f` USD\n", a.ImpactCostUSD, a.ExcessCostUSD)
			}
			for _, e := range a.Evidence {
				fmt.Fprintf(b, "  - %s: `%s`\n", e.Field, e.Value)
			}
		} else {
			for _, e := range a.Evidence {
				if e.Field == "basis" {
					fmt.Fprintf(b, "  - 未命中：%s\n", e.Value)
				}
			}
		}
	}

	// 归因 + 未判定
	if len(j.Attribution) > 0 {
		fmt.Fprintf(b, "\n### 归因候选（由人终裁）\n\n")
		for _, at := range j.Attribution {
			fmt.Fprintf(b, "- %s：%s（占比 %s）\n", at.Subject, at.Detail, pctP(at.Weight))
		}
	}
	if len(j.NotComputed) > 0 {
		fmt.Fprintf(b, "\n### 未能判定\n\n")
		for _, nc := range j.NotComputed {
			fmt.Fprintf(b, "- **%s**：%s\n", nc.Metric, nc.Reason)
		}
	}
}

func levelLabel(l string) string {
	switch l {
	case "personal":
		return "个人"
	case "department":
		return "部门"
	case "studio":
		return "工作室"
	}
	return l
}

func scopeLabel(s report.Scope) string {
	if s.Name != "" {
		return s.Name
	}
	return s.ID
}

func pct(a, b int64) string {
	if b == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", float64(a)/float64(b)*100)
}

func pctP(v float64) string { return fmt.Sprintf("%.1f%%", v*100) }

// writePersonalSections 输出 Vicky 个人周报的纵向基线 / 同岗使用效能画像 / 成本最高模型 / 场景与合理用模。
func writePersonalSections(b *strings.Builder, r *report.Report) {
	p := r.Fact.Personal
	j := &r.Judgment
	rate := r.Meta.Currency.USDCNYRate

	// 二、纵向基线
	fmt.Fprintf(b, "\n## 纵向基线\n\n")
	if j.Baseline.Supported {
		fmt.Fprintf(b, "- vs 本人过去 %d 天：成本处于 P%.0f（MAD 偏离 %.2fσ，%s）\n",
			j.Baseline.WindowDays, j.Baseline.Percentile*100, j.Baseline.DevSigma, j.Baseline.Trend)
	} else {
		fmt.Fprintf(b, "- %s\n", j.Baseline.Note)
	}

	// 三、同岗使用效能画像
	fmt.Fprintf(b, "\n## 同岗使用效能画像\n\n")
	if len(j.PeerProfiles) > 0 {
		poolDesc := j.Peer.PoolType
		if j.Peer.SampleN > 0 {
			poolDesc = fmt.Sprintf("%s（N=%d；不足 5 人则扩展至全公司同岗）", j.Peer.PoolType, j.Peer.SampleN)
		}
		fmt.Fprintf(b, "对标范围：%s。\n\n", poolDesc)
		fmt.Fprintf(b, "| 指标 | 本人 | 同岗 P50 | 本人分位 |\n|---|---:|---:|---:|\n")
		for _, pm := range j.PeerProfiles {
			if !pm.Supported {
				fmt.Fprintf(b, "| %s | %s | — | 样本不足 |\n", pm.Label, fmtVal(pm.Current))
				continue
			}
			fmt.Fprintf(b, "| %s | %s | %s | P%.0f |\n",
				pm.Label, fmtVal(pm.Current), fmtVal(pm.P50), pm.PercentileRank*100)
		}
		// 效能标签
		if r.Expression.EfficiencyLabel != "" {
			fmt.Fprintf(b, "\n- 使用效能标签：**%s**\n", r.Expression.EfficiencyLabel)
		}
		fmt.Fprintf(b, "- 本画像用于资源治理、优秀实践沉淀和使用辅导；不单独作为能力或绩效结论。\n")
	} else {
		fmt.Fprintf(b, "- 无同岗样本，不产出分位。\n")
	}

	// 四、成本最高模型的同岗画像
	if mp := j.TopModelPortrait; mp != nil {
		fmt.Fprintf(b, "\n## 成本最高模型的同岗画像\n\n")
		fmt.Fprintf(b, "- 成本最高模型：`%s`；本人该模型成本 `%s`，占本周总成本 %s。\n",
			mp.Model, cny(mp.PersonCostUSD, rate), pctP(mp.PersonCostRatio))
		if mp.PoolN > 0 {
			fmt.Fprintf(b, "- 同岗采用率：同岗 %d 人中 %d 人本周使用过该模型，占 %s。\n",
				mp.PoolN, mp.AdoptionCount, pctP(mp.AdoptionRate))
		}
		if mp.Supported {
			fmt.Fprintf(b, "- 同岗使用频率：仅采用者中比较，周调用 P50 为 %.0f 次；本人 %.0f 次，P%.0f。\n",
				mp.FreqP50, mp.PersonFreq, mp.PersonFreqRank*100)
			if mp.CostDepP50 > 0 {
				fmt.Fprintf(b, "- 同岗成本依赖：采用者该模型成本占比 P50 为 %s；本人 %s，P%.0f。\n",
					pctP(mp.CostDepP50), pctP(mp.PersonCostRatio), mp.PersonCostDepRank*100)
			}
			if mp.UnitCostP50 > 0 {
				fmt.Fprintf(b, "- 同岗单位成本：采用者 P50 为 `%s / 百万 Token`；本人 `%s`，P%.0f。\n",
					cny(mp.UnitCostP50, rate), cny(mp.PersonUnitCost, rate), mp.PersonUnitCostRank*100)
			}
		} else if mp.Note != "" {
			fmt.Fprintf(b, "- %s\n", mp.Note)
		}
	} else if p.TopModel != "" {
		// 有成本最高模型但无同岗池时简要输出
		fmt.Fprintf(b, "\n## 成本最高模型\n\n")
		fmt.Fprintf(b, "- 成本最高模型：`%s`，本周成本 `%s`，占比 %s。\n",
			p.TopModel, cny(topModelCostUSD(p), rate), pctP(p.HighTierCostRatio))
	}

	// 五、场景与合理用模
	if len(p.TopModels) > 0 {
		fmt.Fprintf(b, "\n## 场景与合理用模\n\n")
		top := p.TopModels[0]
		fmt.Fprintf(b, "- 主用模型：`%s`（占调用 %s，成本 `%s`）。\n",
			top.Model, pct(top.Calls, p.ReqCount), cny(top.CostUSD, rate))
		if p.ModelCount > 1 {
			fmt.Fprintf(b, "- 本周共使用 %d 个模型；成本最高模型占比见上方第四章。\n", p.ModelCount)
		}
	}

	// 七、优化建议（data-based，写入 ## 建议 之前由 RenderMD 统一输出，这里不重复）
}

// buildWeeklyConclusion 生成「本周结论」一行（确定性，基于已算字段）。
func buildWeeklyConclusion(r *report.Report) string {
	p := r.Fact.Personal
	j := &r.Judgment
	// 最高优先级：异常命中
	for _, a := range j.Anomalies {
		if a.Hit {
			return "关注 —— 命中 " + a.Rule + "（" + a.Name + "），" + a.Severity
		}
	}
	// 次优先：成本大幅波动
	if p != nil {
		if p.WoWCostPct >= 50 {
			return fmt.Sprintf("关注 —— 成本环比 +%.0f%%，建议关注用量变化", p.WoWCostPct)
		}
		if p.WoWCostPct <= -30 {
			return fmt.Sprintf("良好 —— 成本环比下降 %.0f%%，持续保持", -p.WoWCostPct)
		}
	}
	// 默认
	return "正常 —— " + r.Expression.Headline
}

// fmtVal 精简数值展示：整数无小数，小数保留 2 位。
func fmtVal(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%.2f", v)
}

// topModelCostUSD 取成本最高模型的成本（USD）。
func topModelCostUSD(p *report.PersonalFact) float64 {
	if len(p.TopModels) > 0 {
		return p.TopModels[0].CostUSD
	}
	return 0
}

// writeDeptSections 输出 Vicky 部门周报 6 章内容。
// 依赖 r.Fact.Department != nil。
func writeDeptSections(b *strings.Builder, r *report.Report) {
	u := r.Fact.Department
	rate := r.Meta.Currency.USDCNYRate
	j := &r.Judgment
	w := func(format string, args ...interface{}) { fmt.Fprintf(b, format, args...) }

	// 一、部门概览
	w("\n## 一、部门概览\n\n")
	// 第一行：成本 / 调用 / 成功率 / 活跃人数
	successRate := pct(u.Member.SuccessCount, u.Member.ReqCount)
	w("- 成本 %s | 调用 %d 次 | 成功率 %s | 活跃 %d 人\n",
		cny(u.Member.EstCostUSD, rate), u.Member.ReqCount, successRate, u.Member.ActiveUserCount)
	// 工作室内占比（有工作室总成本时输出）
	if u.StudioTotalCostUSD > 0 && u.Member.EstCostUSD > 0 {
		studioRatio := u.Member.EstCostUSD / u.StudioTotalCostUSD * 100
		w("- 工作室内占比 %.1f%%\n", studioRatio)
	}
	// 第二行：周环比（仅当有上期数据时输出，WoW 字段全零视为无上期）
	hasWoW := u.WoWCostPct != 0 || u.WoWReqPct != 0 || u.WoWTokenPct != 0
	if hasWoW {
		w("- 周环比：成本 %s / 调用 %s / Token %s\n",
			signedPct(u.WoWCostPct), signedPct(u.WoWReqPct), signedPct(u.WoWTokenPct))
	}
	// 基线行
	if j.Baseline.Supported {
		w("- vs 本部门过去 %d 天：成本处于 P%.0f（MAD 偏离 %.2fσ，%s）\n",
			j.Baseline.WindowDays, j.Baseline.Percentile*100, j.Baseline.DevSigma, j.Baseline.Trend)
	}

	// 二、岗位分布
	w("\n## 二、岗位分布（同岗位分位，不做跨岗位裸排名）\n\n")
	if len(u.JobGroups) > 0 {
		w("| 岗位类别 | 活跃人数 | 人均成本 | 单次成本中位 | 主用模型 |\n")
		w("|---|---|---|---|---|\n")
		for _, jg := range u.JobGroups {
			topModel := jg.TopModel
			if topModel == "" {
				topModel = "—"
			}
			w("| %s | %d | %s | %s | %s |\n",
				jg.JobCategory, jg.ActiveUsers,
				cny(jg.AvgCostUSD, rate),
				cny(jg.MedianCostUSD, rate),
				topModel)
		}
	} else {
		w("- 岗位分布数据不可得（-agg 模式无 JobCategory 明细）\n")
	}

	// 三、Service Key 用量与风险
	w("\n## 三、Service Key 用量与风险（仅当前归属部门）\n\n")
	sk := u.ServiceKey
	if sk.ReqCount > 0 {
		totalCost := u.Member.EstCostUSD + sk.EstCostUSD
		var skRatio string
		if totalCost > 0 {
			skRatio = fmt.Sprintf("，占部门总成本 %s", pctP(sk.EstCostUSD/totalCost))
		}
		w("- Service Key 总成本 %s%s；调用 %d 次；Token %d\n",
			cny(sk.EstCostUSD, rate), skRatio, sk.ReqCount, sk.TotalTokens)
	} else {
		w("- 本周期无 Service Key 调用（或 -agg 模式未拉取明细）\n")
	}

	// 四、重点变化（Top 变化人员）
	w("\n## 四、重点变化（Top 变化人员）\n\n")
	if len(u.TopChanged) > 0 {
		w("| 人员（匿名） | 岗位 | 成本环比 | 主用模型 | 是否有解释 |\n")
		w("|---|---|---|---|---|\n")
		for _, tc := range u.TopChanged {
			explain := "待确认"
			if tc.AnomalyRule != "" {
				explain = "命中 " + tc.AnomalyRule
			}
			w("| 员工%d | %s | %s | %s | %s |\n",
				tc.Idx, tc.JobCategory, signedPct(tc.WoWCostPct), tc.TopModel, explain)
		}
	} else {
		w("- 无上期数据，不产出环比变化（需 -agg 模式 + 有上期记录）\n")
	}

	// 五、异常处置
	w("\n## 五、异常处置\n\n")
	hitCount, missCount := 0, 0
	for _, a := range j.Anomalies {
		if a.Hit {
			hitCount++
		} else if !a.NotComputed {
			missCount++
		}
	}
	w("- 命中 %d 条 | 未命中 %d 条\n", hitCount, missCount)
	for _, a := range j.Anomalies {
		if a.Hit {
			detail := a.Name
			if len(a.Evidence) > 0 {
				detail += "（" + a.Evidence[0].Value + "）"
			}
			w("- [%s] %s —— %s\n", a.Severity, a.Rule, detail)
		}
	}

	// 六、建议
	w("\n## 六、建议\n\n")
	hasSuggestion := false
	if len(u.TopChanged) > 0 {
		w("- 关注成本环比变化最大的成员（见第四章），建议核查原因\n")
		hasSuggestion = true
	}
	// 若部门整体缓存命中偏低，给出优化提示
	if u.Member.CacheHitRate > 0 && u.Member.CacheHitRate < 0.2 {
		w("- 部门整体缓存命中率偏低（%s），建议各岗位关注 Prompt 缓存策略\n", pctP(u.Member.CacheHitRate))
		hasSuggestion = true
	}
	if hitCount == 0 {
		w("- 本周期无规则型异常，各岗位用量正常，保持现状\n")
		hasSuggestion = true
	}
	if !hasSuggestion {
		// 兜底
		w("- 本周期无特别建议\n")
	}
}

// buildDeptConclusion 生成部门周报「本周结论」一行。
// 优先级：异常命中 > 成本大幅上涨 > 成本下降 > 正常。
func buildDeptConclusion(r *report.Report) string {
	u := r.Fact.Department
	j := &r.Judgment
	// 最高优先：异常命中
	hitCount := 0
	maxSeverity := ""
	for _, a := range j.Anomalies {
		if a.Hit {
			hitCount++
			if maxSeverity == "" || a.Severity < maxSeverity {
				maxSeverity = a.Severity // P0 < P1 < P2 < P3，字典序越小越严重
			}
		}
	}
	if hitCount > 0 {
		return fmt.Sprintf("关注 —— 命中 %d 条异常（%s）", hitCount, maxSeverity)
	}
	// 次优先：成本大幅上涨
	if u != nil && u.WoWCostPct >= 30 {
		return fmt.Sprintf("关注 —— 成本环比 +%.0f%%", u.WoWCostPct)
	}
	// 次优先：成本明显下降
	if u != nil && u.WoWCostPct <= -20 {
		return fmt.Sprintf("良好 —— 成本环比下降 %.0f%%", -u.WoWCostPct)
	}
	// 默认
	return "正常 —— 本周期未见异常"
}

// jobSlotRender 岗位回退（render 包内独立）：category→family→title→""。
func jobSlotRender(category, family, title string) string {
	if category != "" {
		return category
	}
	if family != "" {
		return family
	}
	return title
}

// fmtTokens 把 token 数量格式化为易读形式（3.2M / 354M / 1.2B）。
func fmtTokens(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1_000_000_000)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
