package derive

// derive.go —— 装配派生：fact(聚合) → judgment(基线/分位/异常/效率/归因) → expression(措辞)。
//
// 三层数据流在此收口；输入为数据源无关的 ReportInput（[]DailyMetric + []CallEvent + CohortDef）。

import (
	"sort"
	"strings"

	"github.com/example/llm-gateway/internal/observability/report"
)

// CallEvent 别名：derived 包内直接以短名使用报告契约中的逐调用事件。
type CallEvent = report.CallEvent

// Derive 纯函数：输入归一化数据，产出三层 Report。
// ctx 当前保留（阶段一可空）；calls 用于 A1–A5，metrics 用于基线/分位/事实。
func Derive(meta report.Meta, in report.ReportInput, rules report.RuleSet) (*report.Report, error) {
	meta.Tool = "usage-report"

	rep := &report.Report{
		SchemaVersion: report.SchemaVersion,
		Meta:          meta,
	}

	// ---- 事实层：聚合 ----
	rep.Fact = buildFact(in)

	// ---- 判断层 ----
	judge := &report.Judgment{Anomalies: []report.AnomalyHit{}, Attribution: []report.Attribution{}, NotComputed: []report.NotComputed{}}

	// 效率派生（零分母→n/a，不参与分位）
	eff := buildEfficiency(rep.Fact, rules)
	judge.Efficiency = eff

	// 28 天基线（用周期日度量序列；当前值为周期日均）
	judge.Baseline = mapBaseline(buildBaselineSeries(in, rules))

	// 同岗分位
	peer := PeerPercentile(
		in.Cohort.Metric, in.Cohort.PoolType, in.Cohort.Direction,
		mapCohortMembers(in.Cohort),
		cohortCurrentMetric(in.Cohort, rep.Fact),
		rules.MinCohortSample,
	)
	judge.Peer = mapPeer(peer)
	if !peer.Supported {
		judge.NotComputed = append(judge.NotComputed, report.NotComputed{Metric: "同岗分位", Reason: peer.Note})
	}

	// A1–A5
	anoms := Anomalies(in.Calls, in.BaselineCalls, rules, in.ModelContextBudget)
	// 单元级语义说明：A1/A2 基于个人窗口/中位，单元级运行在「该单元成员调用流（排除 service）」上。
	scope := in.LevelMeta.Level
	for i := range anoms {
		anoms[i].Dimensions["scope"] = scope
		if anoms[i].NotComputed {
			judge.NotComputed = append(judge.NotComputed, report.NotComputed{Metric: anoms[i].Rule + " " + anoms[i].Name, Reason: evidenceBasis(anoms[i].Evidence)})
		}
		if scope != "" && scope != "personal" {
			anoms[i].Note += "（注：单元级(" + scope + ")异常基于该单元成员调用流聚合、排除 service；A1/A2 个人窗口/中位语义仅在 personal 粒度完整成立）"
		}
	}
	judge.Anomalies = anoms

	// 归因（按模型占比候选）
	judge.Attribution = buildAttribution(rep.Fact)

	rep.Judgment = *judge

	// ---- 表达层：确定性回填（只引用已存在字段，无新数字/新判定）----
	rep.Expression = buildExpression(rep, rules)

	return rep, nil
}

// ---- 事实层 ----

func buildFact(in report.ReportInput) report.Fact {
	var fact report.Fact
	switch in.LevelMeta.Level {
	case "personal":
		fact.Personal = buildPersonalFact(in)
	case "department":
		fact.Department = buildUnitFact(in, "department")
	case "studio":
		fact.Studio = buildUnitFact(in, "studio")
	}
	return fact
}

func buildPersonalFact(in report.ReportInput) *report.PersonalFact {
	p := &report.PersonalFact{
		ScopeID: in.LevelMeta.ID, ScopeName: in.LevelMeta.Name,
		HourlyDistribution: map[string]int{},
	}
	modelSet := map[string]struct{}{}
	daySet := map[string]struct{}{}
	modelAgg := map[string]*report.TopModel{}
	var durSum, durCount int64
	for _, m := range in.Metrics {
		if m.ReqCount == 0 && m.EstCostUSD == 0 && m.TotalTokens == 0 {
			continue
		}
		p.ReqCount += m.ReqCount
		p.SuccessCount += m.SuccessCount
		p.ErrorCount += m.ErrorCount
		p.PromptTokens += m.PromptTokens
		p.CachedTokens += m.CachedTokens
		p.CacheWriteTokens += m.CacheWriteTokens
		p.CompletionTokens += m.CompletionTokens
		p.ReasoningTokens += m.ReasoningTokens
		p.TotalTokens += m.TotalTokens
		p.EstCostUSD += m.EstCostUSD
		if m.Model != "" {
			modelSet[m.Model] = struct{}{}
			tm := modelAgg[m.Model]
			if tm == nil {
				tm = &report.TopModel{Model: m.Model}
				modelAgg[m.Model] = tm
			}
			tm.Calls += m.ReqCount
			tm.TotalTokens += m.TotalTokens
			tm.CostUSD += m.EstCostUSD
		}
		if m.Date != "" {
			daySet[m.Date] = struct{}{}
		}
	}
	for _, c := range in.Calls {
		h := c.TS.Hour()
		p.HourlyDistribution[itoa(h)]++
		if c.DurationMs > 0 {
			durSum += int64(c.DurationMs)
			durCount++
		}
	}
	p.ModelCount = len(modelSet)
	p.ActiveDays = len(daySet)
	if durCount > 0 {
		p.AvgDurationMs = float64(durSum) / float64(durCount)
	}
	tms := make([]report.TopModel, 0, len(modelAgg))
	for _, tm := range modelAgg {
		tms = append(tms, *tm)
	}
	sort.Slice(tms, func(i, j int) bool { return tms[i].Calls > tms[j].Calls })
	if len(tms) > 5 {
		tms = tms[:5]
	}
	p.TopModels = tms
	return p
}

func buildUnitFact(in report.ReportInput, level string) *report.UnitFact {
	u := &report.UnitFact{ScopeID: in.LevelMeta.ID, ScopeName: in.LevelMeta.Name, StudioID: in.LevelMeta.StudioID}
	modelSet := map[string]struct{}{}
	serviceDaySet := map[string]struct{}{}
	activeUsers := map[string]struct{}{}
	userDays := map[string]map[string]struct{}{}
	modelAgg := map[string]*report.TopModel{}
	// member vs service 分离（成员口径排除 service；service 块只算 service 且归属当前部门，由工具侧过滤）
	for _, m := range in.Metrics {
		if u.ScopeID != m.ScopeID {
			continue
		}
		if m.KeyType == "service" {
			s := &u.ServiceKey
			s.ReqCount += m.ReqCount
			s.SuccessCount += m.SuccessCount
			s.ErrorCount += m.ErrorCount
			s.TotalTokens += m.TotalTokens
			s.EstCostUSD += m.EstCostUSD
			if m.Date != "" {
				serviceDaySet[m.Date] = struct{}{}
			}
			continue
		}
		mb := &u.Member
		mb.ReqCount += m.ReqCount
		mb.SuccessCount += m.SuccessCount
		mb.ErrorCount += m.ErrorCount
		mb.PromptTokens += m.PromptTokens
		mb.CompletionTokens += m.CompletionTokens
		mb.ReasoningTokens += m.ReasoningTokens
		mb.TotalTokens += m.TotalTokens
		mb.EstCostUSD += m.EstCostUSD
		if m.UserID != "" {
			activeUsers[m.UserID] = struct{}{}
			if m.Date != "" {
				if userDays[m.UserID] == nil {
					userDays[m.UserID] = map[string]struct{}{}
				}
				userDays[m.UserID][m.Date] = struct{}{}
			}
		}
		if m.Model != "" {
			modelSet[m.Model] = struct{}{}
			tm := modelAgg[m.Model]
			if tm == nil {
				tm = &report.TopModel{Model: m.Model}
				modelAgg[m.Model] = tm
			}
			tm.Calls += m.ReqCount
			tm.TotalTokens += m.TotalTokens
			tm.CostUSD += m.EstCostUSD
		}
	}
	u.ServiceKey.ActiveDays = len(serviceDaySet)
	u.Member.ModelCount = len(modelSet)
	u.Member.UserCount = in.Cohort.RosterUserCount
	u.Member.ActiveUserCount = len(activeUsers)
	for _, tm := range modelAgg {
		u.Member.TopModels = append(u.Member.TopModels, *tm)
	}
	sort.Slice(u.Member.TopModels, func(i, j int) bool { return u.Member.TopModels[i].Calls > u.Member.TopModels[j].Calls })
	if len(u.Member.TopModels) > 5 {
		u.Member.TopModels = u.Member.TopModels[:5]
	}
	if u.Member.ActiveUserCount > 0 {
		activeDaySum := 0
		for _, days := range userDays {
			activeDaySum += len(days)
		}
		u.Member.PerCapita = report.PerCapita{
			ReqCount:    u.Member.ReqCount / int64(u.Member.ActiveUserCount),
			EstCostUSD:  u.Member.EstCostUSD / float64(u.Member.ActiveUserCount),
			SuccessRate: safeDiv(float64(u.Member.SuccessCount), float64(u.Member.ReqCount)),
			ActiveDays:  activeDaySum / u.Member.ActiveUserCount,
		}
	}
	return u
}

// ---- 效率/归因/基线 ----

func buildEfficiency(f report.Fact, rules report.RuleSet) report.Efficiency {
	var e report.Efficiency
	var req, success int64
	var prompt, completion, reasoning, cached, total int64
	var cost float64
	if f.Personal != nil {
		req = f.Personal.ReqCount
		success = f.Personal.SuccessCount
		prompt = f.Personal.PromptTokens
		completion = f.Personal.CompletionTokens
		reasoning = f.Personal.ReasoningTokens
		cached = f.Personal.CachedTokens
		total = f.Personal.TotalTokens
		cost = f.Personal.EstCostUSD
	} else if f.Department != nil {
		req = f.Department.Member.ReqCount
		success = f.Department.Member.SuccessCount
		prompt = f.Department.Member.PromptTokens
		completion = f.Department.Member.CompletionTokens
		reasoning = f.Department.Member.ReasoningTokens
		total = f.Department.Member.TotalTokens
		cost = f.Department.Member.EstCostUSD
	} else if f.Studio != nil {
		req = f.Studio.Member.ReqCount
		success = f.Studio.Member.SuccessCount
		prompt = f.Studio.Member.PromptTokens
		completion = f.Studio.Member.CompletionTokens
		reasoning = f.Studio.Member.ReasoningTokens
		total = f.Studio.Member.TotalTokens
		cost = f.Studio.Member.EstCostUSD
	}
	if req > 0 {
		e.SuccessRate = float64(success) / float64(req)
		e.ErrorRate = 1 - e.SuccessRate
	}
	if prompt > 0 {
		e.CacheHitRate = float64(cached) / float64(prompt)
		e.OutputRatio = float64(completion) / float64(prompt)
	}
	if total > 0 {
		e.ReasoningShare = float64(reasoning) / float64(total)
		e.CostPer1kTokens = cost / float64(total) * 1000
	}
	rate := rules.USDCNYRate
	if rate <= 0 {
		rate = 7.2
	}
	e.CostUSDCNY = cost * rate
	e.MetricDirection = "cost" // 默认 cost 主导；个人/同岗用 efficiency 时由 CohortDef 覆盖
	return e
}

func buildAttribution(f report.Fact) []report.Attribution {
	byModel := map[string]*struct {
		calls int64
		cost  float64
	}{}
	var total float64
	if f.Personal != nil {
		for _, tm := range f.Personal.TopModels {
			byModel[tm.Model] = &struct {
				calls int64
				cost  float64
			}{tm.Calls, tm.CostUSD}
			total += float64(tm.Calls)
		}
	} else {
		var tm []report.TopModel
		if f.Department != nil {
			tm = topModelsFromUnit(f.Department.Member)
		} else if f.Studio != nil {
			tm = topModelsFromUnit(f.Studio.Member)
		}
		for _, x := range tm {
			byModel[x.Model] = &struct {
				calls int64
				cost  float64
			}{x.Calls, x.CostUSD}
			total += float64(x.Calls)
		}
	}
	if total <= 0 {
		return nil
	}
	type ms struct {
		name   string
		calls  int64
		weight float64
	}
	var list []ms
	for name, v := range byModel {
		list = append(list, ms{name, v.calls, float64(v.calls) / total})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].weight > list[j].weight })
	out := make([]report.Attribution, 0, 3)
	for _, x := range list {
		if len(out) >= 3 {
			break
		}
		subject := x.name
		if strings.TrimSpace(subject) == "" {
			subject = "(未识别模型)"
		}
		out = append(out, report.Attribution{Subject: "模型 " + subject, Detail: "周期内调用占比 " + fmtPct(x.weight), Weight: x.weight})
	}
	return out
}

func topModelsFromUnit(unit report.UnitMember) []report.TopModel {
	return unit.TopModels
}

func buildBaselineSeries(in report.ReportInput, rules report.RuleSet) BaseResult {
	// 当前值 = 当前周期成员调用日均（排除 service，口径与异常/分位一致）。
	var current float64
	byDay := map[string]int64{}
	for _, m := range in.Metrics {
		if m.KeyType == "service" {
			continue
		}
		byDay[m.Date] += m.ReqCount
	}
	totalReq := int64(0)
	for _, n := range byDay {
		totalReq += n
	}
	if len(byDay) > 0 {
		current = float64(totalReq) / float64(len(byDay))
	}

	// 基线日序列：工具侧已拉「期前 28 完整自然日」并聚合到 BaselineMetrics（成员口径）。
	var vals []float64
	windowLabel := "期前28日物化"
	if len(in.BaselineMetrics) > 0 {
		baseDay := map[string]int64{}
		for _, m := range in.BaselineMetrics {
			if m.KeyType == "service" {
				continue
			}
			if m.ReqCount == 0 && m.TotalTokens == 0 {
				continue
			}
			baseDay[m.Date] += m.ReqCount
		}
		for _, n := range baseDay {
			vals = append(vals, float64(n))
		}
	} else {
		// 当前周期不是历史基线；无历史数据时明确不可计算。
		windowLabel = "期前历史窗口缺失"
	}
	return Baseline(vals, current, rules.BaselineWindowDays, rules.MinBaselineSamples, windowLabel)
}

func mapBaseline(b BaseResult) report.Baseline {
	return report.Baseline{
		Metric: "daily_calls", WindowDays: b.WindowDays, SampleDays: b.SampleDays,
		Supported: b.Supported, Current: b.Current, Median: b.Median, MAD: b.MAD,
		SigmaRob: b.SigmaRob, DevSigma: b.DevSigma, Percentile: b.Percentile,
		Trend: b.Trend, CurrentWindow: b.CurrentWindow, Note: b.Note,
	}
}

func mapPeer(p PeerResult) report.Peer {
	return report.Peer{
		Metric: p.Metric, PoolType: p.PoolType, PoolN: p.PoolN, SampleN: p.SampleN,
		Supported: p.Supported, Current: p.Current, PercentileRank: p.PercentileRank,
		Band: p.Band, P25: p.P25, P50: p.P50, P75: p.P75, P90: p.P90,
		VsPeer: p.VsPeer, Anonymized: p.Anonymized, Note: p.Note,
	}
}

func mapCohortMembers(c report.CohortDef) []PortfolioMember {
	out := make([]PortfolioMember, 0, len(c.Members))
	for _, m := range c.Members {
		out = append(out, PortfolioMember{Valid: m.MetricValid, Value: m.MetricValue})
	}
	return out
}

func cohortCurrentMetric(c report.CohortDef, f report.Fact) float64 {
	// 当前成员的效能指标值（阶段一以 cost_per_1k_tokens 为例；由 CohortDef.Metric 决定）
	switch c.Metric {
	case "cost_per_1k_tokens":
		if f.Personal != nil {
			if f.Personal.TotalTokens > 0 {
				return f.Personal.EstCostUSD / float64(f.Personal.TotalTokens) * 1000
			}
		}
	}
	return 0
}

// ---- 表达层 ----

func buildExpression(rep *report.Report, rules report.RuleSet) report.Expression {
	expr := report.Expression{Lang: "zh", Audience: "个人/单元负责人", Tone: "中性/基于证据"}
	j := &rep.Judgment
	hitCount := 0
	sev := ""
	severityRank := map[string]int{"P0": 0, "P1": 1, "P2": 2, "P3": 3}
	for _, a := range j.Anomalies {
		if !a.Hit {
			continue
		}
		hitCount++
		if sev == "" || severityRank[a.Severity] < severityRank[sev] {
			sev = a.Severity
		}
	}
	if hitCount == 0 {
		expr.Headline = "本周期用量/成本未见规则型异常"
	} else {
		expr.Headline = "本周期命中 " + itoa(hitCount) + " 条规则异常（" + sev + "）"
	}
	// insights：只引用已存在标签/规则名，不引入新数字
	for _, lbl := range j.Anomalies {
		if lbl.Hit {
			expr.Insights = append(expr.Insights, "命中规则 "+lbl.Rule+"（"+lbl.Name+"）："+lbl.Note)
		}
	}
	// suggestions：基于命中规则给措辞，不含新数字
	for _, a := range j.Anomalies {
		if a.Hit {
			expr.Suggestions = append(expr.Suggestions, suggestionsFor(a.Rule, a.Name))
		}
	}
	if len(expr.Suggestions) == 0 {
		expr.Suggestions = []string{"未命中规则型异常，保持现状并在下期继续观察。"}
	}
	return expr
}

func suggestionsFor(rule, name string) string {
	switch rule {
	case "A1":
		return "关注 A1（" + name + "）：核对对应时间窗是否有脚本/循环自动发起请求，必要时加限流或熔断。"
	case "A2":
		return "关注 A2（" + name + "）：该窗口成本显著偏高，建议下钻到具体调用点，评估降模型或调小上下文（HARD_CAP ¥200/15min 已触发则立即限流）。"
	case "A3":
		return "关注 A3（" + name + "）：段内重试循环，建议核对重试策略与上游健康度（区分上游故障 vs 客户端僵死）。"
	case "A4":
		return "关注 A4（" + name + "）：出现近上下文上限请求，建议评估精简输入或摘要替换。"
	case "A5":
		return "关注 A5（" + name + "）：存在非工作时段调用，建议确认是否为定时任务或人工操作（阶段一未接备案豁免）。"
	}
	return ""
}

// ---- Scope convenience ----

func safeDiv(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

func evidenceBasis(evidence []report.Evidence) string {
	for _, e := range evidence {
		if e.Field == "basis" {
			return e.Value
		}
	}
	return "缺少判定所需事实"
}
