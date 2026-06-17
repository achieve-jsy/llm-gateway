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
		// Vicky 格式标题：# 个人周报 · 周期 · 姓名（岗位 / 工作室）
		title := scopeLabel(r.Meta.Scope)
		if r.Meta.Scope.JobCategory != "" || r.Meta.Scope.JobFamily != "" {
			job := r.Meta.Scope.JobCategory
			if job == "" {
				job = r.Meta.Scope.JobFamily
			}
			extra := job
			if r.Meta.Scope.StudioID != "" {
				extra += " / " + r.Meta.Scope.StudioID
			}
			title += "（" + extra + "）"
		}
		w("# 个人周报 · %s · %s\n\n", r.Meta.Period.From+"~"+r.Meta.Period.To, title)
		w("【本周结论】%s\n\n", buildWeeklyConclusion(r))
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
	}

	w("\n## 判断\n\n")
	writeJudgment(&b, r)

	w("\n## 建议\n\n")
	for _, g := range r.Expression.Suggestions {
		w("- %s\n", g)
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
		fmt.Fprintf(b, "| 调用数 | %d |\n", p.ReqCount)
		fmt.Fprintf(b, "| 成功率 | %s |\n", pct(p.SuccessCount, p.ReqCount))
		fmt.Fprintf(b, "| 总 Token | %d |\n", p.TotalTokens)
		fmt.Fprintf(b, "| 成本 | `%s` |\n", cny(p.EstCostUSD, rate))
		fmt.Fprintf(b, "| 活跃天数 | %d |\n", p.ActiveDays)
		fmt.Fprintf(b, "| 模型数 | %d |\n", p.ModelCount)
		fmt.Fprintf(b, "| 平均耗时 | %.1fs |\n", p.AvgDurationMs/1000)
		if p.P95DurationMs > 0 {
			fmt.Fprintf(b, "| p95 耗时 | %.1fs |\n", p.P95DurationMs/1000)
		}
		if p.AvgInputCtxTokens > 0 {
			fmt.Fprintf(b, "| 平均输入上下文 | %.0f token |\n", p.AvgInputCtxTokens)
		}
		if p.CacheHitRate > 0 {
			fmt.Fprintf(b, "| 缓存 Token 占比 | %s |\n", pctP(p.CacheHitRate))
		}
		if p.CostPer1Million > 0 {
			fmt.Fprintf(b, "| 每百万 Token 成本 | `%s` |\n", cny(p.CostPer1Million, rate))
		}
		if p.TopModel != "" {
			fmt.Fprintf(b, "| 成本最高模型 | %s |\n", p.TopModel)
		}
		// 周环比
		fmt.Fprintf(b, "| 周环比·调用 | %s |\n", signedPct(p.WoWReqPct))
		fmt.Fprintf(b, "| 周环比·Token | %s |\n", signedPct(p.WoWTokenPct))
		fmt.Fprintf(b, "| 周环比·成本 | %s |\n", signedPct(p.WoWCostPct))
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
