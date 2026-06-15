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
	}
	w("  total_tokens_note: \"prompt+completion+reasoning（不含 cached/cache_write）\"\n")
	w("  baseline: \"GET /api/v1/usage/dashboard\"\n")
	w("  schema_version: %s\n", r.SchemaVersion)
	w("---\n\n")

	w("# 用量周报 · %s\n\n", r.Expression.Headline)
	w("> **级别**：%s · **范围**：%s\n", levelLabel(r.Meta.Level), scopeLabel(r.Meta.Scope))
	w("> **周期**：%s（%s ~ %s，%d 天）\n", r.Meta.Period.Label, r.Meta.Period.From, r.Meta.Period.To, r.Meta.Period.Days)
	w("> **币种**：%s（USD→CNY %.4f）\n\n", r.Meta.Currency.ReportCurrency, r.Meta.Currency.USDCNYRate)

	w("## 概况\n\n")
	w("| 指标 | 值 |\n|---|---|\n")
	writeFactTable(&b, r)

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
		fmt.Fprintf(b, "| 调用数 | %d |\n", p.ReqCount)
		fmt.Fprintf(b, "| 成功率 | %s |\n", pct(p.SuccessCount, p.ReqCount))
		fmt.Fprintf(b, "| 总 Token | %d |\n", p.TotalTokens)
		fmt.Fprintf(b, "| 成本 | `%.4f` USD |\n", p.EstCostUSD)
		fmt.Fprintf(b, "| 模型数 | %d |\n", p.ModelCount)
	} else if r.Fact.Department != nil {
		u := r.Fact.Department
		fmt.Fprintf(b, "| 成员调用 | %d（%d 人） |\n", u.Member.ReqCount, u.Member.ActiveUserCount)
		fmt.Fprintf(b, "| 成员成功率 | %s |\n", pct(u.Member.SuccessCount, u.Member.ReqCount))
		fmt.Fprintf(b, "| 成员成本 | `%.4f` USD |\n", u.Member.EstCostUSD)
		fmt.Fprintf(b, "| Service Key 调用 | %d |\n", u.ServiceKey.ReqCount)
	}
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
