package render

// render_md.go —— GFM Markdown 输出（只读 Report，不在模板内二次计算）。
//
// 版式：front-matter `facts_digest` 供对账；结论用 `> `；事实用 ``inline``；严重度 [P0..P3]；附录(口径/对账)兜底。

import (
	"fmt"
	"math"
	"strings"

	"github.com/example/llm-gateway/internal/observability/report"
)

// periodTitle 标题用周期标记：有 ISO 周则优先展示（如 "2026-W33"），否则回退日期区间。
// 目标形态是对照样例的「2026-W33」；日期区间保留在该行后半作为辅助。
func periodTitle(p report.Period) string {
	if p.IsoWeek != "" {
		return p.IsoWeek
	}
	return p.From + "~" + p.To
}

// RenderMD 渲染 Markdown。
func RenderMD(r *report.Report) string {
	var b strings.Builder
	w := func(format string, args ...interface{}) { fmt.Fprintf(&b, format, args...) }

	// front-matter facts_digest（对账用：只列关键数字+口径）
	w("---\n")
	w("facts_digest:\n")
	w("  level: %s\n", r.Meta.Level)
	w("  period: \"%s ~ %s (%s, %dd)\"\n", r.Meta.Period.From, r.Meta.Period.To, r.Meta.Period.Type, r.Meta.Period.Days)
	if r.Meta.Period.IsoWeek != "" {
		w("  iso_week: %s\n", r.Meta.Period.IsoWeek)
	}
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
		w("# 个人周报 · %s · %s\n\n", periodTitle(r.Meta.Period), title)
		w("【本周结论】%s\n\n", buildWeeklyConclusion(r))
	} else if r.Fact.Department != nil {
		// Vicky 部门周报标题：只用部门名，不附 StudioID（UUID 无可读性）
		w("# 部门周报 · %s · %s\n\n", periodTitle(r.Meta.Period), scopeLabel(r.Meta.Scope))
		w("【本周结论】%s\n\n", buildDeptConclusion(r))
	} else if r.Fact.Studio != nil {
		w("# 工作室周报 · %s · %s\n\n", periodTitle(r.Meta.Period), scopeLabel(r.Meta.Scope))
		w("【本周结论】%s\n\n", buildDeptConclusion(r))
	} else {
		w("# 用量周报 · %s\n\n", r.Expression.Headline)
	}
	w("> **级别**：%s · **范围**：%s\n", levelLabel(r.Meta.Level), scopeLabel(r.Meta.Scope))
	w("> **周期**：%s（%s ~ %s，%d 天）\n", r.Meta.Period.Label, r.Meta.Period.From, r.Meta.Period.To, r.Meta.Period.Days)
	if r.Meta.Period.IsoWeek != "" {
		w("> **ISO 周**：%s\n", r.Meta.Period.IsoWeek)
	}
	w("> **币种**：%s（USD→CNY %.4f）\n\n", r.Meta.Currency.ReportCurrency, r.Meta.Currency.USDCNYRate)

	// Vicky 模板章节命名：个人首章为「一、使用概览」，部门/工作室保持「概况」数字表（其编号章节随后输出）。
	if r.Fact.Personal != nil {
		w("## 一、使用概览\n\n")
	} else {
		w("## 概况\n\n")
	}
	w("| 指标 | 值 |\n|---|---|\n")
	writeFactTable(&b, r)

	if r.Fact.Personal != nil {
		// Vicky 个人周报：纵向基线 / 同岗使用效能画像 / 成本最高模型 / 场景与合理用模
		writePersonalSections(&b, r)
	} else if r.Fact.Department != nil {
		// Vicky 部门周报：6 章模板
		writeDeptSections(&b, r)
	} else if r.Fact.Studio != nil {
		// Vicky 工作室周报：真实 4 章模板（一页摘要 / 部门贡献 / 变化归因 / 风险与行动清单）
		writeStudioSections(&b, r)
	}

	w("\n## 判断\n\n")
	writeJudgment(&b, r)

	// 部门/工作室周报：建议已在第六章输出，跳过通用建议块避免重复
	if r.Fact.Personal != nil {
		w("\n## 七、优化建议\n\n")
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

// orDash 空字符串显示为「—」（表格占位，避免留下空单元格）。
func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// memberAdvice 依据 TopChanged 成员的事实（成本环比/承担模型/命中主因）规则生成具体建议（具体到人+动作）。
// 数字来自 facts（WoWCostPct/AnomalyCause/TopModel），措辞为规则生成，不硬编码具体值。
func memberAdvice(tc report.TopChangedMember) string {
	var parts []string
	switch {
	case tc.WoWCostPct >= 30:
		parts = append(parts, "确认是否存在后台脚本/循环调用或异常流量，必要时限流")
	case tc.WoWCostPct <= -30:
		parts = append(parts, "成本环比下降明显，确认是否用量减少或合理优化")
	case tc.WoWCostPct > 0:
		parts = append(parts, "成本小幅上升，关注用量趋势")
	case tc.WoWCostPct < 0:
		parts = append(parts, "成本小幅下降，保持现状")
	default:
		parts = append(parts, "成本持平，保持现状")
	}
	if tc.AnomalyCause != "" {
		parts = append(parts, "命中异常"+tc.AnomalyCause+"，建议优先处理")
	} else if tc.TopModel != "" {
		parts = append(parts, "主用模型"+tc.TopModel)
	}
	return strings.Join(parts, "；")
}

// efficiencyLabelExplain 依据同岗指标（本人 vs 同岗 P50 分位）为「使用效能标签」追加一句解释（确定性，数字来自 profiles）。
// 与 derive.buildEfficiencyLabel 同一口径：高采用=使用量高于同岗；待优化=单位成本偏高/缓存比偏低/上下文偏长等。
func efficiencyLabelExplain(profiles []report.PeerMetric) string {
	usageRank := -1.0 // 调用量维度平均分位（calls/tokens/active_days）
	effRank := -1.0   // 效能维度最差分位（cost/ctx/cache）
	usageCnt := 0
	var effNotes []string
	for _, pm := range profiles {
		if !pm.Supported {
			continue
		}
		switch pm.Metric {
		case "weekly_calls", "weekly_tokens", "active_days":
			if usageRank < 0 {
				usageRank = pm.PercentileRank
			} else {
				usageRank = (usageRank + pm.PercentileRank) / 2
			}
			usageCnt++
		case "cost_per_1m":
			if effRank < pm.PercentileRank {
				effRank = pm.PercentileRank
			}
			if pm.PercentileRank >= 0.75 {
				effNotes = append(effNotes, "单位 Token 成本偏高")
			}
		case "avg_input_ctx":
			if effRank < pm.PercentileRank {
				effRank = pm.PercentileRank
			}
			if pm.PercentileRank >= 0.75 {
				effNotes = append(effNotes, "输入上下文偏长")
			}
		case "cache_hit_rate":
			inv := 1 - pm.PercentileRank
			if effRank < inv {
				effRank = inv
			}
			if inv >= 0.75 {
				effNotes = append(effNotes, "缓存比偏低")
			}
		}
	}
	if usageCnt == 0 && effRank < 0 {
		return ""
	}

	usageClause := ""
	if usageCnt > 0 {
		switch {
		case usageRank >= 0.6:
			usageClause = "使用量高于同岗中位数"
		case usageRank < 0.3:
			usageClause = "使用量低于同岗中位数"
		default:
			usageClause = "使用量接近同岗中位数"
		}
	}

	effBad := effRank >= 0.75
	var effClause string
	switch {
	case effBad:
		if len(effNotes) == 0 {
			effNotes = append(effNotes, "单位成本/缓存表现偏弱")
		}
		effClause = strings.Join(effNotes, "、")
	case effRank >= 0:
		effClause = "单位成本/缓存优于同岗"
	default:
		effClause = ""
	}

	switch {
	case usageClause != "" && effClause != "":
		conn := "，且"
		if effBad {
			conn = "，但"
		}
		return usageClause + conn + effClause
	case usageClause != "":
		return usageClause
	default:
		return effClause
	}
}

// signedPct 显示带符号的百分比（周环比用），0 显示为 0.0%。
func signedPct(v float64) string {
	if v == 0 {
		return "0.0%"
	}
	return fmt.Sprintf("%+.1f%%", v)
}

// wowPctFromPrev 计算相对上期的环比 %；无上期（prev==0）时输出「—」，避免硬造 0%。
func wowPctFromPrev(cur, prev float64) string {
	if prev == 0 {
		return "—"
	}
	return signedPct((cur - prev) / prev * 100)
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

	// 异常（A1–A5）
	// Studio 报告已在新第四章「风险与行动清单」完整呈现异常与风险；个人报告在独立「六、异常提示」章节呈现。
	// 此处抑制，避免与各自独立章节重复。
	isStudio := r.Meta.Level == "studio"
	isPersonal := r.Meta.Level == "personal"
	if !isStudio && !isPersonal {
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
	}

	// 归因候选 + 未判定
	// Studio 报告已在新第三章「变化归因」完整呈现归因，此处抑制，避免与三章重复。
	if !isStudio && len(j.Attribution) > 0 {
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
	fmt.Fprintf(b, "\n## 二、纵向基线\n\n")
	if j.Baseline.Supported {
		fmt.Fprintf(b, "- vs 本人过去 %d 天：成本处于 P%.0f（MAD 偏离 %.2fσ，%s）\n",
			j.Baseline.WindowDays, j.Baseline.Percentile*100, j.Baseline.DevSigma, j.Baseline.Trend)
	} else {
		fmt.Fprintf(b, "- %s\n", j.Baseline.Note)
	}

	// 三、同岗使用效能画像
	fmt.Fprintf(b, "\n## 三、同岗使用效能画像\n\n")
	if len(j.PeerProfiles) > 0 {
		poolDesc := j.Peer.PoolType
		if j.Peer.SampleN > 0 {
			poolDesc = fmt.Sprintf("%s（N=%d；不足 5 人则扩展至全公司同岗）", j.Peer.PoolType, j.Peer.SampleN)
		}
		fmt.Fprintf(b, "对标范围：%s。\n\n", poolDesc)
		fmt.Fprintf(b, "| 指标 | 本人 | 同岗 P50 | 本人分位 |\n|---|---:|---:|---:|\n")
		for _, pm := range j.PeerProfiles {
			if !pm.Supported {
				fmt.Fprintf(b, "| %s | %s | — | 样本不足 |\n", pm.Label, fmtPeerVal(pm.Current, pm.Unit, rate))
				continue
			}
			fmt.Fprintf(b, "| %s | %s | %s | P%.0f |\n",
				pm.Label, fmtPeerVal(pm.Current, pm.Unit, rate), fmtPeerVal(pm.P50, pm.Unit, rate), pm.PercentileRank*100)
		}
		// 效能标签（附一句动态解释：本人 vs 同岗 P50）
		if r.Expression.EfficiencyLabel != "" {
			explain := efficiencyLabelExplain(j.PeerProfiles)
			if explain != "" {
				fmt.Fprintf(b, "\n- 使用效能标签：**%s**——%s\n", r.Expression.EfficiencyLabel, explain)
			} else {
				fmt.Fprintf(b, "\n- 使用效能标签：**%s**\n", r.Expression.EfficiencyLabel)
			}
		}
		fmt.Fprintf(b, "- 本画像用于资源治理、优秀实践沉淀和使用辅导；不单独作为能力或绩效结论。\n")
	} else {
		fmt.Fprintf(b, "- 无同岗样本，不产出分位。\n")
	}

	// 四、成本最高模型的同岗画像
	if mp := j.TopModelPortrait; mp != nil {
		fmt.Fprintf(b, "\n## 四、成本最高模型的同岗画像\n\n")
		fmt.Fprintf(b, "- 成本最高模型：`%s`；本人该模型成本 `%s`，占本周总成本 %s。\n",
			mp.Model, cny(mp.PersonCostUSD, rate), pctP(mp.PersonCostRatio))
		if mp.PoolN > 0 {
			fmt.Fprintf(b, "- 同岗采用率：同岗 %d 人中 %d 人本周使用过该模型，占 %s。\n",
				mp.PoolN, mp.AdoptionCount, pctP(mp.AdoptionRate))
		}
		if mp.Supported {
			if mp.Note != "" {
				fmt.Fprintf(b, "- %s\n", mp.Note)
			}
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

	// 五、场景与合理用模（确定性降级输出 + LLM 模型分析增强）
	// 截面：确定性 TopModels 数据（LLM 未启用时的降级）+ LLM 生成内容（增强）。
	// 头部仅在确定性数据或 LLM 内容任一存在时输出；ma==nil 时行为与现状完全一致。
	ma := r.ModelAnalysis
	hasDet := len(p.TopModels) > 0
	if hasDet || ma != nil {
		fmt.Fprintf(b, "\n## 五、场景与合理用模\n\n")
		if hasDet {
			top := p.TopModels[0]
			fmt.Fprintf(b, "- 主用模型：`%s`（占调用 %s，成本 `%s`）。\n",
				top.Model, pct(top.Calls, p.ReqCount), cny(top.CostUSD, rate))
			if p.ModelCount > 1 {
				fmt.Fprintf(b, "- 本周共使用 %d 个模型；成本最高模型占比见上方第四章。\n", p.ModelCount)
			}
		}
		// 模型选用建议 + 省钱预估（确定性，数字来自既定事实；SaveIsEstimate 标注为估算）。
		if mg := j.ModelGuidance; mg != nil {
			if mg.ScenarioNote != "" {
				fmt.Fprintf(b, "- 场景：%s\n", mg.ScenarioNote)
			}
			if mg.Recommendation != "" {
				fmt.Fprintf(b, "- 建议：%s\n", mg.Recommendation)
			}
			if mg.EstWeeklySaveUSD > 0 {
				// cny() 内部乘汇率，故此处直接传入 USD 估算值（derive 无汇率）。
				fmt.Fprintf(b, "- 省钱预估（估算）：预计每周约省 `%s`。\n", cny(mg.EstWeeklySaveUSD, rate))
				if mg.SaveBasis != "" {
					fmt.Fprintf(b, "  - 估算依据：%s\n", mg.SaveBasis)
				}
			}
			if mg.Note != "" && mg.EstWeeklySaveUSD <= 0 {
				fmt.Fprintf(b, "- %s\n", mg.Note)
			}
			fmt.Fprintf(b, "\n")
		}
		// LLM 增强：Sections 展开 + Note 降级说明（复用统一 helper 的章节体；确定性内容后补空行区隔）
		if ma != nil {
			if strings.TrimSpace(ma.Sections) != "" && hasDet {
				fmt.Fprintf(b, "\n")
			}
			writeModelAnalysisBody(b, ma)
		}
	}

	// 六、异常提示（Vicky 模板含反馈入口□；A1–A5 命中/未命中判定在此独立呈现）
	// 模板要求「异常提示含反馈入口」。此处输出 A1–A5，尾部给出反馈入口占位。
	fmt.Fprintf(b, "\n## 六、异常提示\n\n")
	if len(j.Anomalies) > 0 {
		// 模板只列【命中】异常（带阈值/实际/基线/影响成本/可能原因/反馈入口）；未命中的在末尾简要注记，不占主清单。
		var hits, misses []report.AnomalyHit
		for _, a := range j.Anomalies {
			if a.Hit {
				hits = append(hits, a)
			} else if !a.NotComputed {
				misses = append(misses, a)
			}
		}
		for _, a := range hits {
			fmt.Fprintf(b, "- [%s] **%s**（%s）\n", a.Rule, a.Name, a.Severity)
			fmt.Fprintf(b, "  - `actual=%.2f` · `threshold=%.2f` · `baseline=%.2f`（%s）\n", a.ActualValue, a.ThresholdValue, a.BaselineValue, a.BaselineWindow)
			if a.ImpactCostUSD > 0 {
				fmt.Fprintf(b, "  - 影响成本 `%.2f` USD · 超支 `%.2f` USD\n", a.ImpactCostUSD, a.ExcessCostUSD)
			}
			// 保留完整证据链（含可能原因/判定依据）
			for _, e := range a.Evidence {
				fmt.Fprintf(b, "  - %s: `%s`\n", e.Field, e.Value)
			}
		}
		if len(hits) == 0 {
			fmt.Fprintf(b, "- 无异常命中。\n")
		}
		if len(misses) > 0 {
			missNames := make([]string, 0, len(misses))
			for _, m := range misses {
				missNames = append(missNames, m.Rule)
			}
			fmt.Fprintf(b, "- 另有 %d 条规则未命中（判定未触发，不列入异常清单）：%s\n", len(misses), strings.Join(missNames, "、"))
		}
	} else {
		fmt.Fprintf(b, "- 无异常命中。\n")
	}
	// 反馈入口（异常状态机需人工确认，离线仅占位；阶段一可让用户标注确认/误报/已恢复）
	fmt.Fprintf(b, "- 反馈入口：请确认该异常（□正常任务 □已停止 □归属错误 □误报 □需协助）——用于后续「异常状态机（待确认/已确认/修复中/已恢复/误报）」演进。\n")

	// 七、优化建议（data-based，写入 ## 建议 之前由 RenderMD 统一输出，这里不重复）
}

// buildWeeklyConclusion 生成「本周结论」一行（确定性，基于已算字段）。
func buildWeeklyConclusion(r *report.Report) string {
	p := r.Fact.Personal
	j := &r.Judgment
	// 成本环比前缀（若有个人事实）：支持「成本环比 +180%，主因 …」的结论形态。
	costPrefix := ""
	if p != nil && p.WoWCostPct >= 50 {
		costPrefix = fmt.Sprintf("成本环比 +%.0f%%，", p.WoWCostPct)
	} else if p != nil && p.WoWCostPct <= -30 {
		costPrefix = fmt.Sprintf("成本环比 -%.0f%%，", -p.WoWCostPct)
	}
	// 最高优先级：异常命中（主因归因写到结论行；规则+名称+命中窗口/说明，来自已算字段，非臆造）。
	for _, a := range j.Anomalies {
		if a.Hit {
			lead := "关注 —— "
			if costPrefix != "" {
				lead += costPrefix
			}
			return lead + "主因 " + anomalyRootCause(a)
		}
	}
	// 次优先：成本大幅波动
	if p != nil {
		if p.WoWCostPct >= 50 {
			return fmt.Sprintf("关注 —— 成本环比 +%.0f%%（无异常命中），建议关注用量变化", p.WoWCostPct)
		}
		if p.WoWCostPct <= -30 {
			return fmt.Sprintf("良好 —— 成本环比下降 %.0f%%，持续保持", -p.WoWCostPct)
		}
	}
	// 默认
	return "正常 —— " + r.Expression.Headline
}

// anomalyRootCause 提取异常的主因措辞（确定性，来自已算字段）：规则+名称，补命中窗口/说明。
// 形如 "A2 成本燃烧（15:00~15:15 窗口，触发封顶 ¥300/15min）"；证据缺失时回退为规则+名称+级别。
func anomalyRootCause(a report.AnomalyHit) string {
	base := a.Rule + " " + a.Name
	// 命中窗口（人类可读）是最能说明「何时何因」的主因线索。
	if a.Window != "" {
		base += "（" + a.Window
		if a.Note != "" {
			base += "；" + a.Note
		}
		base += "）"
	} else if a.Note != "" {
		base += "（" + a.Note + "）"
	} else {
		base += "（" + a.Severity + "）"
	}
	return base
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
	// 耗时行：成员聚合耗时字段 >0 时输出（与「概况」顶部个人耗时行同口径）
	if u.Member.AvgDurationMs > 0 || u.Member.P95DurationMs > 0 || u.Member.AvgTTFTMs > 0 {
		parts := []string{}
		if u.Member.AvgDurationMs > 0 {
			parts = append(parts, fmt.Sprintf("平均耗时 %.1fs", u.Member.AvgDurationMs/1000))
		}
		if u.Member.P95DurationMs > 0 {
			parts = append(parts, fmt.Sprintf("p95 %.1fs", u.Member.P95DurationMs/1000))
		}
		if u.Member.AvgTTFTMs > 0 {
			parts = append(parts, fmt.Sprintf("平均 TTFT %.0fms", u.Member.AvgTTFTMs))
		}
		w("- %s\n", strings.Join(parts, " | "))
	}
	// 匿名横比行：消费 judge.PeerUnitCost（derive 单元级匿名横比字段），输出同类型部门横比。
	// 行首保留事实层本单元「单位活跃人成本」（有值时），再附 peer 分位；peer 数据未就绪时降级注明。
	pu := j.PeerUnitCost
	perCapita := u.Member.PerCapita
	lead := ""
	if perCapita.EstCostUSD > 0 {
		lead = fmt.Sprintf("单位活跃人成本 %s/人 · ", cny(perCapita.EstCostUSD, rate))
	}
	if pu.Supported {
		w("- %svs 同类型部门（匿名）单位活跃人成本 P%.0f（当前 %s/人 · 池 %s %d 样本 · vs 中位 %s）\n",
			lead, pu.PercentileRank*100, cny(pu.Current, rate), pu.PoolType, pu.SampleN, orDash(pu.VsPeer))
	} else {
		note := pu.Note
		if note == "" {
			note = "样本不足"
		}
		w("- %svs 同类型部门（匿名）单位活跃人成本：分位不可用（%s）\n", lead, note)
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
		w("- Service Key 总成本 %s%s；调用 %d 次；Token %d；主用模型 %s；成本环比 %s\n",
			cny(sk.EstCostUSD, rate), skRatio, sk.ReqCount, sk.TotalTokens,
			orDash(sk.TopModel), wowPctFromPrev(sk.EstCostUSD, sk.PrevCostUSD))
		// 逐 Key 明细表（t6 装配 u.ServiceKey.Keys；环比/主用模型读 ServiceKeyInfo.Prev*/TopModel，缺失列占位不硬造）
		if len(sk.Keys) > 0 {
			w("\n| Service Key | Owner/应用 | 调用 | 调用环比 | Token | 成本 | 成本环比 | 成本占比 | 主用模型 | 异常 |\n")
			w("|---|---|---:|---:|---:|---:|---:|---:|---|---|\n")
			for _, k := range sk.Keys {
				keyRatio := "—"
				if totalCost > 0 {
					keyRatio = pctP(k.CostUSD / totalCost)
				}
				errCell := "—"
				if k.ErrorCount > 0 {
					errCell = fmt.Sprintf("%d", k.ErrorCount)
				}
				w("| %s | %s | %d | %s | %s | %s | %s | %s | %s | %s |\n",
					orDash(k.KeyName), orDash(k.OwnerName), k.ReqCount,
					wowPctFromPrev(float64(k.ReqCount), float64(k.PrevReqCount)),
					fmtTokens(k.TotalTokens), cny(k.CostUSD, rate),
					wowPctFromPrev(k.CostUSD, k.PrevCostUSD), keyRatio, orDash(k.TopModel), errCell)
			}
			w("\n- 注：「主用模型」为当期成本最高的模型名；「环比」为对比前一周同范围（无上期显示「—」）；「异常」为当期失败调用数（0 显示「—」）。\n")
		}
	} else {
		w("- 本周期无 Service Key 调用（或 -agg 模式未拉取明细）\n")
	}

	// 四、重点变化（Top 变化人员）
	w("\n## 四、重点变化（Top 变化人员）\n\n")
	if len(u.TopChanged) > 0 {
		w("| 人员（匿名） | 岗位 | 成本环比 | 主因 | 是否有解释 |\n")
		w("|---|---|---|---|---|\n")
		for _, tc := range u.TopChanged {
			// 主因列与是否有解释列均读 AnomalyCause（模板口径；不再用旧的 AnomalyRule）。
			// 有值即已解释，主因直接显示命中规则（rule+name，如 "A2 成本燃烧"）；空则均降级「待确认」。
			cause := tc.AnomalyCause
			causeDisplay := "待确认"
			explained := "待确认"
			if cause != "" {
				causeDisplay = cause
				explained = "已解释"
			}
			w("| 员工%d | %s | %s | %s | %s |\n",
				tc.Idx, tc.JobCategory, signedPct(tc.WoWCostPct), causeDisplay, explained)
		}
	} else {
		w("- 无上期数据，不产出环比变化（需 -agg 模式 + 有上期记录）\n")
	}

	// 五、异常处置
	w("\n## 五、异常处置\n\n")
	var hitAnoms, missAnoms []report.AnomalyHit
	for _, a := range j.Anomalies {
		if a.Hit {
			hitAnoms = append(hitAnoms, a)
		} else if !a.NotComputed {
			missAnoms = append(missAnoms, a)
		}
	}
	w("- 命中 %d 条 | 未命中 %d 条\n", len(hitAnoms), len(missAnoms))
	// 异常状态机 5 态计数（t14 report.CountAnomalyStatus；离线默认「待确认」，反馈闭环读回真实状态后自动归桶）
	sc := report.CountAnomalyStatus(hitAnoms)
	w("- 异常状态机：待确认=%d | 已确认正常=%d | 修复中=%d | 已恢复=%d | 误报=%d\n",
		sc.Pending, sc.ConfirmedOK, sc.Fixing, sc.Recovered, sc.FalsePos)
	for _, a := range hitAnoms {
		detail := a.Name
		if len(a.Evidence) > 0 {
			detail += "（" + a.Evidence[0].Value + "）"
		}
		status := a.Status
		if status == "" {
			status = report.AnomalyStatusPending
		}
		w("- [%s] %s —— %s（状态：%s）\n", a.Severity, a.Rule, detail, status)
	}

	// 六、建议
	w("\n## 六、建议\n\n")
	hasSuggestion := false
	if len(u.TopChanged) > 0 {
		// 具体到人+动作：对重点变化成员逐条给建议（结合其岗位/成本环比/承担模型/命中主因）。
		w("- 重点变化成员建议：\n")
		for _, tc := range u.TopChanged {
			w("  - 员工%d（%s，成本环比 %s）：%s\n",
				tc.Idx, orDash(tc.JobCategory), signedPct(tc.WoWCostPct), memberAdvice(tc))
		}
		hasSuggestion = true
	}
	// 若部门整体缓存命中偏低，给出优化提示
	if u.Member.CacheHitRate > 0 && u.Member.CacheHitRate < 0.2 {
		w("- 部门整体缓存命中率偏低（%s），建议统一可缓存的提示词模板、提升缓存复用\n", pctP(u.Member.CacheHitRate))
		hasSuggestion = true
	}
	if len(hitAnoms) == 0 {
		w("- 本周期无规则型异常，各岗位用量正常，保持现状\n")
		hasSuggestion = true
	}
	if !hasSuggestion {
		// 兜底
		w("- 本周期无特别建议\n")
	}

	// 模型分析（LLM 生成；rep.ModelAnalysis==nil 时跳过，保持现状）—— 六、建议之后
	writeModelAnalysisSection(b, r, "模型分析")
}

// writeStudioSections 输出 Vicky 工作室周报真实 4 章模板：
// 一、一页摘要 / 二、部门贡献 / 三、变化归因 / 四、风险与行动清单。
// 数据缺失时逐条安全降级（对应行跳过/输出占位），不 panic。依赖 r.Fact.Studio != nil。
func writeStudioSections(b *strings.Builder, r *report.Report) {
	u := r.Fact.Studio
	rate := r.Meta.Currency.USDCNYRate
	j := &r.Judgment
	w := func(format string, args ...interface{}) { fmt.Fprintf(b, format, args...) }
	m := u.Member

	// ===== 一、一页摘要 =====
	w("\n## 一、一页摘要\n\n")
	// 第一行：成本 / 调用 / 活跃 / Token
	w("- 成本 %s | 调用 %d 次 | 活跃 %d 人 | Token %s\n",
		cny(m.EstCostUSD, rate), m.ReqCount, m.ActiveUserCount, fmtTokens(m.TotalTokens))
	// 第二行：全公司占比 + 周环比（占比仅在公司成本>0 时下发）
	var line2 []string
	if u.CompanyRatio > 0 {
		line2 = append(line2, fmt.Sprintf("全公司占比 %s", pctP(u.CompanyRatio)))
	}
	if u.WoWCostPct != 0 || u.WoWReqPct != 0 {
		line2 = append(line2, fmt.Sprintf("周环比：成本 %s / 调用 %s", signedPct(u.WoWCostPct), signedPct(u.WoWReqPct)))
	}
	if len(line2) > 0 {
		w("- %s\n", strings.Join(line2, " | "))
	}
	// 第三行：4 周趋势 + 预计可优化成本
	var line3 []string
	if len(u.WeeklyTrend) >= 2 {
		line3 = append(line3, fmt.Sprintf("4 周趋势：%s", studioTrendLabel(u.WeeklyTrend)))
	}
	if u.OptimizableCostUSD > 0 {
		line3 = append(line3, fmt.Sprintf("预计可优化成本：约 %s/周（模型降级 + 缓存）", cny(u.OptimizableCostUSD, rate)))
	}
	if len(line3) > 0 {
		w("- %s\n", strings.Join(line3, " | "))
	}
	// 第四行：新增未关闭异常统计（P1 ×N，P2 ×N）
	if len(u.AnomalySummary) > 0 {
		var sevParts []string
		for _, ac := range u.AnomalySummary {
			sevParts = append(sevParts, fmt.Sprintf("%s ×%d", ac.Severity, ac.Count))
		}
		w("- 新增未关闭异常：%s\n", strings.Join(sevParts, "，"))
	}
	// 预算执行行（Vicky 工作室「一页摘要」；Supported=false 表示不可得，安全降级注明，不硬造数值）
	if be := u.BudgetExec; be != nil {
		if be.Supported {
			if be.MonthLimitUSD > 0 {
				w("- 预算执行：实际 %.4f USD / 限额 %.4f USD（%.1f%%）\n",
					be.ActualCostUSD, be.MonthLimitUSD, be.Percent)
			} else {
				w("- 预算执行：实际 %.4f USD / 限额不可用（%.1f%%）\n", be.ActualCostUSD, be.Percent)
			}
		} else {
			note := be.Note
			if note == "" {
				note = "未配置预算"
			}
			w("- 预算执行：不可得（%s）\n", note)
		}
	}

	// ===== 二、部门贡献 =====
	w("\n## 二、部门贡献\n\n")
	if len(u.DeptContributions) > 0 {
		w("| 部门 | 成本 | 占比 | 环比 | 单位活跃人成本 |\n")
		w("|---|---:|---:|---:|---:|\n")
		for _, dc := range u.DeptContributions {
			wow := "—"
			if dc.WoWCostPct != 0 { // 无上期成本时 WoW=0，显示占位
				wow = signedPct(dc.WoWCostPct)
			}
			w("| %s | %s | %s | %s | %s |\n",
				dc.DeptName, cny(dc.CostUSD, rate), pctP(dc.CostRatio), wow, cny(dc.AvgCostUSD, rate))
		}
		if u.CostConcentration > 0 {
			w("\n- 成本集中度：Top2 部门占 %s\n", pctP(u.CostConcentration))
		}
	} else {
		w("- 本周期无部门贡献数据\n")
	}

	// ===== 三、变化归因 =====
	if len(j.Attribution) == 0 {
		w("\n## 三、变化归因\n\n")
		w("- 无上期数据，不产出变化归因\n")
	} else {
		netDelta := studioNetDeltaUSD(u)
		if deltas, ok := attributionDeltas(j.Attribution, netDelta); ok {
			w("\n## 三、变化归因（成本 %s 拆解）\n\n", signedCNY(netDelta, rate))
			w("| 来源 | 贡献 |\n")
			w("|---|---:|\n")
			for i, at := range j.Attribution {
				w("| %s | %s |\n", at.Subject, signedCNY(deltas[i], rate))
			}
		} else {
			w("\n## 三、变化归因\n\n")
			w("- 本周期成本变化有限，无需归因\n")
		}
	}

	// ===== 四、风险与行动清单 =====
	w("\n## 四、风险与行动清单\n\n")
	var hits []report.AnomalyHit
	for _, a := range j.Anomalies {
		if a.Hit && !a.NotComputed {
			hits = append(hits, a)
		}
	}
	if len(hits) == 0 {
		w("- 本周期无风险项，各成员用量正常\n")
	} else {
		w("| 对象 | 时间 | 命中规则 | 影响成本 | 状态 | Owner | 建议动作 | 截止 |\n")
		w("|---|---|---|---|---|---|---|---|\n")
		for _, a := range hits {
			win := a.Window
			if win == "" {
				win = r.Meta.Period.From + "~" + r.Meta.Period.To
			}
			impact := "—"
			if a.ImpactCostUSD > 0 {
				impact = cny(a.ImpactCostUSD, rate)
			}
			status := a.Status
			if status == "" {
				status = report.AnomalyStatusPending
			}
			w("| %s | %s | %s | %s | %s | 应用Owner | %s | %s |\n",
				riskObject(a), win, a.Rule+" "+a.Name, impact, status, riskAction(a.Rule), shortDate(r.Meta.Period.To))
		}
		// 状态列读 AnomalyHit.Status（t14 状态机 5 态；离线默认「待确认」，反馈闭环读回真实状态回填）。
		w("\n- 注：「状态」为异常状态机 5 态（待确认/已确认正常/修复中/已恢复/误报），离线默认「待确认」，接入反馈闭环后按真实状态回填。\n")
	}

	// 模型分析（LLM 生成；rep.ModelAnalysis==nil 时跳过，保持现状）—— 四、风险与行动清单之后
	writeModelAnalysisSection(b, r, "模型分析")
}

// writeModelAnalysisBody 输出 LLM 模型分析的章节体（不含标题）：Sections 非空优先展开；
// 有 Note 时追加一行降级说明；都为空时输出兜底文案，绝不 panic。ma==nil 时跳过。
func writeModelAnalysisBody(b *strings.Builder, ma *report.ModelAnalysis) {
	if ma == nil {
		return
	}
	if strings.TrimSpace(ma.Sections) != "" {
		fmt.Fprintf(b, "%s\n", ma.Sections)
	}
	if ma.Note != "" {
		fmt.Fprintf(b, "- （注：%s）\n", ma.Note)
	}
	if strings.TrimSpace(ma.Sections) == "" && strings.TrimSpace(ma.Note) == "" {
		fmt.Fprintf(b, "- 本周期模型分析数据不可得\n")
	}
}

// writeModelAnalysisSection 输出 LLM 模型分析章节（含标题）。
// title 由调用方传入：个人周报复用「场景与合理用模」标题，部门/工作室周报用「模型分析」。
// 契约：rep.ModelAnalysis==nil 时完全跳过（保持现状不发章节），绝不 panic。
func writeModelAnalysisSection(b *strings.Builder, r *report.Report, title string) {
	if r.ModelAnalysis == nil {
		return
	}
	fmt.Fprintf(b, "\n## %s\n\n", title)
	writeModelAnalysisBody(b, r.ModelAnalysis)
}

// signedCNY 带符号的 CNY 展示：+¥12.34 / -¥56.78（入参为 USD，按汇率换算）。
func signedCNY(usd float64, rate float64) string {
	sign := "+"
	v := usd * rate
	if v < 0 {
		sign = "-"
		v = -v
	}
	return fmt.Sprintf("%s¥%.2f", sign, v)
}

// studioNetDeltaUSD 计算工作室成本净增量（USD）：本期成本 - 上期成本（由 WoWCostPct 反推上期）。
// WoWCostPct==0（无上期/无变化）或无法反推时返回 0。
func studioNetDeltaUSD(u *report.UnitFact) float64 {
	cur := u.Member.EstCostUSD
	if cur <= 0 {
		return 0
	}
	denom := 1 + u.WoWCostPct/100
	if denom <= 0 {
		return 0
	}
	return cur - cur/denom
}

// attributionDeltas 由 judge.Attribution 的 Weight（契约：Weight=该来源增量/总增量，ΣWeight=1，可为负表示降本）
// 直接还原各来源成本增量（USD）：Δi = 总增量 × Weight。
// 无上期（len==0）或总增量≈0（ΣWeight→0，netDelta 不可作为拆解基准）时返回 ok=false，由调用方降级。
func attributionDeltas(attrs []report.Attribution, netDelta float64) ([]float64, bool) {
	if len(attrs) == 0 {
		return nil, false
	}
	if math.Abs(netDelta) < 0.005 {
		return nil, false
	}
	out := make([]float64, len(attrs))
	for i, a := range attrs {
		out[i] = netDelta * a.Weight
	}
	return out, true
}

// studioTrendLabel 依据 4 周成本序列（降序、最新在前）给出确定性趋势标签。
func studioTrendLabel(pts []report.WeeklyPoint) string {
	if len(pts) < 2 {
		return "数据不足"
	}
	first := pts[len(pts)-1].CostUSD // 最旧
	last := pts[0].CostUSD           // 最新
	if first <= 0 {
		return "数据不足"
	}
	chg := (last - first) / first
	switch {
	case chg > 0.05:
		return "上升"
	case chg > 0.01:
		return "稳中有升"
	case chg < -0.05:
		return "下降"
	case chg < -0.01:
		return "稳中有降"
	default:
		return "平稳"
	}
}

// riskObject 从异常命中的维度信息提取风险对象；无组织维度时用默认「成员/部门」。
func riskObject(a report.AnomalyHit) string {
	if a.Dimensions != nil {
		if v := a.Dimensions["user_id"]; v != "" {
			return "成员 " + v
		}
		if v := a.Dimensions["dept_name"]; v != "" {
			return v
		}
		if v := a.Dimensions["dept_id"]; v != "" {
			return "部门 " + v
		}
	}
	return "成员/部门"
}

// riskAction 按命中规则给出一条默认建议动作（无组织数据时使用）。
func riskAction(rule string) string {
	switch rule {
	case "A1":
		return "确认是否存在失控脚本/任务，核查运行时长与调用频率"
	case "A2":
		return "核查批处理/循环/异常重试，定位成本激增来源"
	case "A3":
		return "检查重试策略与失败率，定位高失败重试段"
	case "A4":
		return "优化 Prompt / 精简上下文，核查超长调用"
	case "A5":
		return "确认是否备案；未备案建议加强非工作时段管控"
	default:
		return "核查原因"
	}
}

// shortDate 将 "YYYY-MM-DD" 截断为 "MM-DD"；非日期字符串原样返回。
func shortDate(s string) string {
	if len(s) >= 10 && s[4] == '-' {
		return s[5:10]
	}
	return s
}

// countSeverity 统计未关闭异常中指定等级的条数（AnomalySummary 专用）。
func countSeverity(summary []report.AnomalyCount, sev string) int {
	for _, ac := range summary {
		if ac.Severity == sev {
			return ac.Count
		}
	}
	return 0
}

// buildDeptConclusion 生成部门/工作室周报「本周结论」一行。
// 优先级：异常命中 > 成本大幅上涨 > 成本下降 > 正常。
// 工作室级别：数据存在时追加「占全公司 %；新增 N 起 P1 待处理」。
func buildDeptConclusion(r *report.Report) string {
	u := unitFact(r)
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
	base := ""
	switch {
	case hitCount > 0:
		base = fmt.Sprintf("关注 —— 命中 %d 条异常（%s）", hitCount, maxSeverity)
	case u != nil && u.WoWCostPct >= 30:
		base = fmt.Sprintf("关注 —— 成本环比 +%.0f%%", u.WoWCostPct)
	case u != nil && u.WoWCostPct <= -20:
		base = fmt.Sprintf("良好 —— 成本环比下降 %.0f%%", -u.WoWCostPct)
	default:
		base = "正常 —— 本周期未见异常"
	}
	// 工作室专属追加：占全公司占比 + 新增未关闭 P1（仅 studio 级别有这些字段）
	if r.Fact.Studio != nil && u != nil {
		var suffix []string
		if u.CompanyRatio > 0 {
			suffix = append(suffix, fmt.Sprintf("占全公司 %.1f%%", u.CompanyRatio*100))
		}
		if p1 := countSeverity(u.AnomalySummary, "P1"); p1 > 0 {
			suffix = append(suffix, fmt.Sprintf("%d 起 P1 待处理", p1))
		}
		if len(suffix) > 0 {
			base += " ｜ " + strings.Join(suffix, "；")
		}
	}
	return base
}

// jobSlotRender 岗位回退（render 包内独立）：title→category→family→""。
// 优先展示职位名（job_title，如"WEB前端开发"），无则退回岗位类别，再退族。
func jobSlotRender(category, family, title string) string {
	if title != "" {
		return title
	}
	if category != "" {
		return category
	}
	return family
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

// fmtPeerVal 按指标单位格式化同岗画像数值。
// unit="%" → 小数×100 显示为百分比；unit="USD" → CNY；unit="次"/"天" → 整数；
// unit="token" → 万 token；空 unit（total_tokens）→ fmtTokens。
func fmtPeerVal(v float64, unit string, rate float64) string {
	switch unit {
	case "%":
		return fmt.Sprintf("%.1f%%", v*100)
	case "USD":
		return cny(v, rate)
	case "次", "天":
		return fmt.Sprintf("%.0f %s", v, unit)
	case "token":
		// 平均输入上下文：显示为 k token
		return fmt.Sprintf("%.0fk token", v/1000)
	default:
		// total_tokens 等大数字用 fmtTokens
		return fmtTokens(int64(v))
	}
}
