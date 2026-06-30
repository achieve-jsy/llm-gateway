package derive

// derive.go —— 装配派生：fact(聚合) → judgment(基线/分位/异常/效率/归因) → expression(措辞)。
//
// 三层数据流在此收口；输入为数据源无关的 ReportInput（[]DailyMetric + []CallEvent + CohortDef）。

import (
	"fmt"
	"math"
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

	// 个人报告补充：同岗 Target 的周环比与成本最高模型（来自服务端聚合，不经明细；agg/明细两模式共用）。
	// 注意：必须在 buildTopModelPortrait 之前执行，portrait 依赖 PersonalFact.TopModels。
	if rep.Fact.Personal != nil && in.Cohort.Target.WoWCostPct != 0 {
		rep.Fact.Personal.WoWReqPct = in.Cohort.Target.WoWReqPct
		rep.Fact.Personal.WoWTokenPct = in.Cohort.Target.WoWTokenPct
		rep.Fact.Personal.WoWCostPct = in.Cohort.Target.WoWCostPct
		if in.Cohort.Target.TopModel != "" {
			rep.Fact.Personal.TopModel = in.Cohort.Target.TopModel
		}
		if m := in.Cohort.Target.Metrics; m.AvgInputCtxTokens > 0 {
			rep.Fact.Personal.AvgInputCtxTokens = m.AvgInputCtxTokens
		}
		if m := in.Cohort.Target.Metrics; m.CacheHitRate > 0 {
			rep.Fact.Personal.CacheHitRate = m.CacheHitRate
		}
		if m := in.Cohort.Target.Metrics; m.CostPer1kTokens > 0 {
			rep.Fact.Personal.CostPer1Million = m.CostPer1kTokens
		}
		if m := in.Cohort.Target.Metrics; m.HighTierCostRatio > 0 {
			rep.Fact.Personal.HighTierCostRatio = m.HighTierCostRatio
		}
		// agg 模式无明细 TopModels；从 CohortTarget.Metrics.TopModels 补入（dashboard/users 已有）
		if len(rep.Fact.Personal.TopModels) == 0 {
			for _, tm := range in.Cohort.Target.Metrics.TopModels {
				rep.Fact.Personal.TopModels = append(rep.Fact.Personal.TopModels, report.TopModel{
					Model:       tm.Model,
					CostUSD:     tm.CostUSD,
					Calls:       tm.CallCount,
					TotalTokens: tm.TotalTokens,
				})
			}
		}
	}

	// 同岗使用效能画像（Vicky 多指标分位表）；样本不足时指标级降级，不整体判失败。
	judge.PeerProfiles = buildPeerProfiles(in, rep.Fact, rules.MinCohortSample)

	// 第四章：成本最高模型的同岗画像（依赖 PersonalFact.TopModels，须在上方注入之后调用）。
	judge.TopModelPortrait = buildTopModelPortrait(in, rep.Fact, rules.MinCohortSample)

	// A1–A5
	anoms := Anomalies(in.Calls, in.BaselineCalls, in.BaselineBuckets, in.CurrentBuckets, rules, in.ModelContextBudget)
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

	// ---- Vicky 工作室模板（仅 studio 级别）：一页摘要 / 部门环比 / 成本集中度 / 变化归因 ----
	if rep.Fact.Studio != nil {
		dcs, conc := buildDeptContributionWoW(in, rep.Fact.Studio.DeptContributions)
		rep.Fact.Studio.DeptContributions = dcs
		rep.Fact.Studio.CostConcentration = conc
		buildStudioSummary(rep.Fact.Studio, in, judge)
		if attrs := buildStudioAttribution(in); len(attrs) > 0 {
			judge.Attribution = attrs
		}
	}

	// ---- 单元级匿名横比（department|studio）：本单元「单位活跃人成本」在全公司各部门中的分位 ----
	// 本值取本单元 PerCapita 成本（Σ成本/活跃人数）；池 = 全公司各部门单位活跃人成本（按 DeptName 归并）。
	// 无 CompanyUserMetrics 或池样本不足时 supported=false，不产生误导性横比。
	if rep.Fact.Department != nil {
		judge.PeerUnitCost = buildDeptUnitCostPeer(in, unitCostPerActiveUser(rep.Fact.Department), rules.MinCohortSample)
	} else if rep.Fact.Studio != nil {
		judge.PeerUnitCost = buildDeptUnitCostPeer(in, unitCostPerActiveUser(rep.Fact.Studio), rules.MinCohortSample)
	}

	rep.Judgment = *judge

	// ---- 表达层：确定性回填（只引用已存在字段，无新数字/新判定）----
	rep.Expression = buildExpression(rep, rules)

	return rep, nil
}

// ---- 事实层 ----

func buildFact(in report.ReportInput) report.Fact {
	var fact report.Fact
	// 成员聚合接入：无明细派生(Metrics 空)但有成员聚合(MemberAgg)时，事实层走聚合口径
	//（每人一行、排除 service；TopModels/ServiceKey/日模型分布等明细依赖项降级/空，见 builder）。
	switch in.LevelMeta.Level {
	case "personal":
		if len(in.Metrics) == 0 && len(in.MemberAgg) > 0 {
			fact.Personal = buildPersonalFactFromAgg(in)
		} else {
			fact.Personal = buildPersonalFact(in)
		}
	case "department":
		if len(in.Metrics) == 0 && len(in.MemberAgg) > 0 {
			fact.Department = buildUnitFactFromAgg(in, "department")
		} else {
			fact.Department = buildUnitFact(in, "department")
		}
	case "studio":
		if len(in.Metrics) == 0 && len(in.MemberAgg) > 0 {
			fact.Studio = buildUnitFactFromAgg(in, "studio")
		} else {
			fact.Studio = buildUnitFact(in, "studio")
		}
	}
	return fact
}

// ---- 成员聚合事实层（阶段二：不拉全量明细，用 FetchMemberAgg 每人一行构造加法核心）----

// buildPersonalFactFromAgg 用单条成员聚合构造 PersonalFact。缺明细依赖项：TopModels(空)、
// HourlyDistribution(空)、CacheWriteTokens(0)、ModelCount/ActiveDays 用其计数。
// 耗时/TTFT/上下文取服务端聚合字段（个人即单成员，直接装配；无字段时保持 0，AvgInputCtxTokens 回退 prompt/req 推算）。
func buildPersonalFactFromAgg(in report.ReportInput) *report.PersonalFact {
	p := &report.PersonalFact{ScopeID: in.LevelMeta.ID, ScopeName: in.LevelMeta.Name, HourlyDistribution: map[string]int{}}
	for _, m := range in.MemberAgg {
		if m.UserID == "" || (m.ReqCount == 0 && m.CostUSD == 0 && m.TotalTokens == 0) {
			continue
		}
		// personal 只认与 scope 匹配的一行；若调用方已按 user 过滤(常见)，匹配 ID 即可。
		if in.LevelMeta.ID != "" && m.UserID != in.LevelMeta.ID {
			continue
		}
		p.ReqCount += m.ReqCount
		p.SuccessCount += m.SuccessCount
		p.ErrorCount += m.ErrorCount
		p.PromptTokens += m.PromptTokens
		p.CachedTokens += m.CachedTokens
		p.CompletionTokens += m.CompletionTokens
		p.ReasoningTokens += m.ReasoningTokens
		p.TotalTokens += m.TotalTokens
		p.EstCostUSD += m.CostUSD
		p.ModelCount += m.ModelCount
		p.ActiveDays += int(m.ActiveDays)
		// 耗时/TTFT/上下文：服务端聚合已算好，单成员直接装配（个人无加权，即该成员值）。
		p.AvgDurationMs = m.AvgDurationMs
		p.P95DurationMs = m.P95DurationMs
		p.AvgTTFTMs = m.AvgFirstTokenMs
		if m.AvgInputCtxTokens > 0 {
			p.AvgInputCtxTokens = m.AvgInputCtxTokens
		}
		break
	}
	// 派生指标（同 buildPersonalFact，基于 agg 字段推算）
	// AvgInputCtxTokens：优先取服务端字段（口径更精确），否则回退 prompt/req 推算。
	if p.ReqCount > 0 && p.PromptTokens > 0 && p.AvgInputCtxTokens == 0 {
		p.AvgInputCtxTokens = float64(p.PromptTokens) / float64(p.ReqCount)
	}
	if p.PromptTokens > 0 {
		p.CacheHitRate = float64(p.CachedTokens) / float64(p.PromptTokens)
	}
	if p.TotalTokens > 0 {
		p.CostPer1Million = p.EstCostUSD / float64(p.TotalTokens) * 1e6
	}
	return p
}

// buildUnitFactFromAgg 用成员聚合行(每人一行、排除 service)构造 UnitFact 的成员加法核心。
// 明细依赖项降级：TopModels/ModelCount 空(无模型粒度，u.Member.ModelCount=0)、PerCapita 用 Σreq/Σcost/count 与 Σactive_days/count。
// ServiceKey 块：成员聚合接口恒排除 service，改用 in.ServiceKeys（/usage/report/service-keys）装配 owner/model_count 等；该通道为空时降级并标注 Notes。
func buildUnitFactFromAgg(in report.ReportInput, level string) *report.UnitFact {
	u := &report.UnitFact{ScopeID: in.LevelMeta.ID, ScopeName: in.LevelMeta.Name, StudioID: in.LevelMeta.StudioID}
	if len(in.MemberAgg) == 0 {
		assembleServiceKey(&u.ServiceKey, in.ServiceKeys)
		if len(in.ServiceKeys) == 0 {
			u.ServiceKey.Notes = "成员聚合口径：未拉明细，Service Key 块不可得。"
		}
		return u
	}
	active := 0
	var activeDaySum int64
	var totalCached, totalPrompt int64
	// 耗时/TTFT/上下文：各成员是周期口径的均值/分位，聚合到部门/工作室需按调用量(req_count)加权。
	// 权重分母 = Σ活跃成员 req_count；分子 = Σ(成员值 × 成员 req_count)。
	var totalReqWeight int64
	var durWeighted, p95Weighted, ttftWeighted, ctxWeighted float64
	for _, m := range in.MemberAgg {
		if m.ReqCount == 0 && m.CostUSD == 0 && m.TotalTokens == 0 {
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
		mb.EstCostUSD += m.CostUSD
		totalCached += m.CachedTokens
		totalPrompt += m.PromptTokens
		active++
		activeDaySum += m.ActiveDays
		// 加权累加耗时字段（仅对产生调用的成员计权；无调用成员值为 0，自然不影响分子）
		totalReqWeight += m.ReqCount
		durWeighted += m.AvgDurationMs * float64(m.ReqCount)
		p95Weighted += m.P95DurationMs * float64(m.ReqCount)
		ttftWeighted += m.AvgFirstTokenMs * float64(m.ReqCount)
		ctxWeighted += m.AvgInputCtxTokens * float64(m.ReqCount)
	}
	u.Member.ActiveUserCount = active
	u.Member.UserCount = in.Cohort.RosterUserCount
	assembleServiceKey(&u.ServiceKey, in.ServiceKeys)
	if len(in.ServiceKeys) == 0 {
		u.ServiceKey.Notes = "成员聚合口径：未拉明细，Service Key 块与 TopModels/模型并集不可得(排除 service)。"
	}
	if active > 0 {
		u.Member.PerCapita = report.PerCapita{
			ReqCount:    u.Member.ReqCount / int64(active),
			EstCostUSD:  u.Member.EstCostUSD / float64(active),
			SuccessRate: safeDiv(float64(u.Member.SuccessCount), float64(u.Member.ReqCount)),
			ActiveDays:  int(activeDaySum / int64(active)),
		}
	}

	// 效能字段（agg 模式）
	// CacheHitRate = Σcached / Σprompt（prompt>0 才算）
	if totalPrompt > 0 {
		u.Member.CacheHitRate = float64(totalCached) / float64(totalPrompt)
	}
	// CostPer1Million = Σcost / Σtotal_tokens * 1e6
	if u.Member.TotalTokens > 0 {
		u.Member.CostPer1Million = u.Member.EstCostUSD / float64(u.Member.TotalTokens) * 1e6
	}
	// 耗时/TTFT/上下文：按 req_count 加权平均（求和口径与成员口径一致）。
	// p95 本身为分位，无法在无逐调用分布时精确复算，这里以 req_count 加权近似（略偏温和；
	// 若要偏保守取该单元最差成员 p95 可改为 max），注明为近似值以供渲染层口径说明。
	if totalReqWeight > 0 {
		u.Member.AvgDurationMs = durWeighted / float64(totalReqWeight)
		u.Member.P95DurationMs = p95Weighted / float64(totalReqWeight)
		u.Member.AvgTTFTMs = ttftWeighted / float64(totalReqWeight)
		u.Member.AvgInputCtxTokens = ctxWeighted / float64(totalReqWeight)
	}

	// 岗位分组：构建 userID→TopModels map（优先 Cohort.Members，补 ReportInput.UserTopModels）
	userTopModels := make(map[string][]report.CohortTopModel, len(in.Cohort.Members)+len(in.UserTopModels))
	// 先从 ReportInput.UserTopModels（部门/工作室级别由工具侧拉 dashboard/users 填入）
	for uid, tms := range in.UserTopModels {
		userTopModels[uid] = tms
	}
	// Cohort.Members 覆盖（personal 级别更精确）
	for _, m := range in.Cohort.Members {
		if len(m.Metrics.TopModels) > 0 {
			userTopModels[m.UserID] = m.Metrics.TopModels
		}
	}
	u.JobGroups = buildJobGroups(in.MemberAgg, userTopModels)

	// 周环比（上期 MemberAgg 非空时）
	if len(in.PrevMemberAgg) > 0 {
		var prevReq, prevTokens int64
		var prevCost float64
		for _, m := range in.PrevMemberAgg {
			prevReq += m.ReqCount
			prevTokens += m.TotalTokens
			prevCost += m.CostUSD
		}
		curReq := u.Member.ReqCount
		curTokens := u.Member.TotalTokens
		curCost := u.Member.EstCostUSD
		if prevReq != 0 {
			u.WoWReqPct = float64(curReq-prevReq) / float64(prevReq) * 100
		}
		if prevTokens != 0 {
			u.WoWTokenPct = float64(curTokens-prevTokens) / float64(prevTokens) * 100
		}
		if prevCost != 0 {
			u.WoWCostPct = (curCost - prevCost) / prevCost * 100
		}

		// Top 变化人员（|WoWCostPct| 最大的前 5 人，匿名）
		u.TopChanged = buildTopChanged(in.MemberAgg, in.PrevMemberAgg, userTopModels)
	}

	// 工作室总成本（department 级别：从 StudioMemberAgg 聚合，用于「工作室内占比」）
	if len(in.StudioMemberAgg) > 0 {
		var studioCost float64
		for _, m := range in.StudioMemberAgg {
			studioCost += m.CostUSD
		}
		u.StudioTotalCostUSD = studioCost
	}

	// 部门贡献（studio 级别专用：按 DeptID 聚合 MemberAgg，算各部门成本/人数/人均）
	if level == "studio" {
		u.DeptContributions = buildDeptContributions(in)
	}

	return u
}

// assembleServiceKey 用 ServiceKey 聚合行（每 Key 一行）装配 UnitFact.ServiceKey 块（agg 模式专用）。
//   - 数值字段（req/success/error/total_tokens/cost）跨 Key 求和；
//   - ActiveDays 取各 Key 的最大值（按天去重近似，避免多 Key 同日重复计）；
//   - OwnerName 取成本最高 Key 的 owner（代表本块主要服务）；
//   - ModelCount 为各 Key 之和（无逐 Key 模型并集，属上限近似；详见 Notes）。
//
// 传空 keys 时保持原值（调用方据此标注「不可得」）。
func assembleServiceKey(sk *report.UnitService, keys []report.ServiceKeyAgg) {
	if len(keys) == 0 {
		return
	}
	var req, succ, errCnt, tokens int64
	var cost float64
	var modelCount int
	topOwner := ""
	topCost := 0.0
	for _, k := range keys {
		req += k.ReqCount
		succ += k.SuccessCount
		errCnt += k.ErrorCount
		tokens += k.TotalTokens
		cost += k.CostUSD
		modelCount += k.ModelCount
		if int(k.ActiveDays) > sk.ActiveDays {
			sk.ActiveDays = int(k.ActiveDays)
		}
		if k.CostUSD > topCost {
			topCost = k.CostUSD
			topOwner = k.OwnerName
		}
	}
	sk.ReqCount = req
	sk.SuccessCount = succ
	sk.ErrorCount = errCnt
	sk.TotalTokens = tokens
	sk.EstCostUSD = cost
	sk.OwnerName = topOwner
	sk.ModelCount = modelCount
	// 逐 Key 明细：供「ServiceKey 表」逐行渲染（KeyName/OwnerName/ReqCount/TotalTokens/CostUSD/ModelCount）。
	sk.Keys = make([]report.ServiceKeyInfo, 0, len(keys))
	for _, k := range keys {
		sk.Keys = append(sk.Keys, report.ServiceKeyInfo{
			KeyName:     k.KeyName,
			OwnerName:   k.OwnerName,
			ReqCount:    k.ReqCount,
			TotalTokens: k.TotalTokens,
			CostUSD:     k.CostUSD,
			ModelCount:  k.ModelCount,
		})
	}
	sk.Notes = fmt.Sprintf("%d 个 Service Key（由服务端 service-keys 聚合装配；OwnerName=成本最高 Key 的 owner；ModelCount=各 Key 模型数之和的近似；逐 Key 明细见下表）", len(keys))
}

// buildDeptContributions 按 DeptID 聚合 MemberAgg，计算工作室下各部门的成本/人数/人均贡献。
// 仅在 studio 级别调用。
// - DeptName 从 in.DeptNames 映射查取，无映射时保持 DeptID 原值作为名称。
// - CostRatio = 该部门成本 / 工作室总成本（Σ全部活跃成员 CostUSD）；总成本为 0 时比例置 0。
// - AvgCostUSD = 该部门成本 / 活跃人数（活跃 = ReqCount>0 || CostUSD>0 || TotalTokens>0）；无活跃时为 0。
// - 结果按 CostUSD 降序排列。
func buildDeptContributions(in report.ReportInput) []report.DeptContribution {
	type deptEntry struct {
		activeUsers int
		costUSD     float64
		reqCount    int64
	}
	groups := map[string]*deptEntry{}

	// 按 DeptID 聚合
	for _, m := range in.MemberAgg {
		if m.ReqCount == 0 && m.CostUSD == 0 && m.TotalTokens == 0 {
			continue
		}
		deptID := m.DeptID
		if deptID == "" {
			deptID = "(未分配部门)"
		}
		e := groups[deptID]
		if e == nil {
			e = &deptEntry{}
			groups[deptID] = e
		}
		e.activeUsers++
		e.costUSD += m.CostUSD
		e.reqCount += m.ReqCount
	}

	if len(groups) == 0 {
		return nil
	}

	// 计算工作室总成本（用于 CostRatio 分母）
	var totalCost float64
	for _, e := range groups {
		totalCost += e.costUSD
	}

	result := make([]report.DeptContribution, 0, len(groups))
	for deptID, e := range groups {
		deptName := deptID
		if n, ok := in.DeptNames[deptID]; ok && n != "" {
			deptName = n
		}
		dc := report.DeptContribution{
			DeptID:      deptID,
			DeptName:    deptName,
			ActiveUsers: e.activeUsers,
			CostUSD:     e.costUSD,
			ReqCount:    e.reqCount,
		}
		if totalCost > 0 {
			dc.CostRatio = e.costUSD / totalCost
		}
		if e.activeUsers > 0 {
			dc.AvgCostUSD = e.costUSD / float64(e.activeUsers)
		}
		result = append(result, dc)
	}

	// 按 CostUSD 降序排列
	sort.Slice(result, func(i, j int) bool {
		return result[i].CostUSD > result[j].CostUSD
	})
	return result
}

// buildJobGroups 按岗位分组，计算每组使用概况。
// userTopModels: userID → 该用户 Top 模型列表（来自 dashboard/users，用于统计每组主用模型）。
// 分组键优先取 JobCategory，空时回退 JobFamily，再空回退 JobTitle，都空归 "其他"。
// 按 AvgCostUSD 降序，最多 10 组。
func buildJobGroups(aggs []report.MemberAgg, userTopModels map[string][]report.CohortTopModel) []report.JobGroup {
	type groupEntry struct {
		activeUsers int
		totalCost   float64
		perReqCosts []float64          // 每人 cost/req（活跃用户，用于中位数）
		modelCost   map[string]float64 // 该组内各模型累计成本（用于选 TopModel）
	}
	groups := map[string]*groupEntry{}
	for _, m := range aggs {
		cat := jobSlotDerive(m.JobCategory, m.JobFamily, m.JobTitle)
		if m.ReqCount == 0 && m.CostUSD == 0 && m.TotalTokens == 0 {
			continue
		}
		e := groups[cat]
		if e == nil {
			e = &groupEntry{modelCost: map[string]float64{}}
			groups[cat] = e
		}
		if m.ReqCount > 0 {
			e.activeUsers++
			e.totalCost += m.CostUSD
			e.perReqCosts = append(e.perReqCosts, m.CostUSD/float64(m.ReqCount))
		}
		// 从 userTopModels 累加该组内各模型成本
		for _, tm := range userTopModels[m.UserID] {
			if tm.Model != "" {
				e.modelCost[tm.Model] += tm.CostUSD
			}
		}
	}
	result := make([]report.JobGroup, 0, len(groups))
	for cat, e := range groups {
		// 选该组成本最高的模型作为 TopModel
		topModel := ""
		maxCost := 0.0
		for model, cost := range e.modelCost {
			if cost > maxCost {
				maxCost = cost
				topModel = model
			}
		}
		jg := report.JobGroup{
			JobCategory: cat,
			ActiveUsers: e.activeUsers,
			TopModel:    topModel,
		}
		if e.activeUsers > 0 {
			jg.AvgCostUSD = e.totalCost / float64(e.activeUsers)
		}
		if len(e.perReqCosts) > 0 {
			sort.Float64s(e.perReqCosts)
			jg.MedianCostUSD = Percentile(e.perReqCosts, 50)
		}
		result = append(result, jg)
	}
	// 按 AvgCostUSD 降序
	sort.Slice(result, func(i, j int) bool {
		return result[i].AvgCostUSD > result[j].AvgCostUSD
	})
	if len(result) > 10 {
		result = result[:10]
	}
	return result
}

// buildTopChanged 计算环比变化最大的前 5 名匿名成员。
// userTopModels: userID → Top 模型列表（可空），用于填充 TopModel 字段。
func buildTopChanged(cur, prev []report.MemberAgg, userTopModels map[string][]report.CohortTopModel) []report.TopChangedMember {
	// 构建上期 cost 与 req 索引（以 UserID 为键）
	prevCostMap := make(map[string]float64, len(prev))
	prevReqMap := make(map[string]int64, len(prev))
	for _, m := range prev {
		if m.UserID == "" {
			continue
		}
		prevCostMap[m.UserID] = m.CostUSD
		prevReqMap[m.UserID] = m.ReqCount
	}

	type candidate struct {
		userID     string
		wowCostPct float64
		wowReqPct  float64
		jobCat     string
		absWoW     float64
	}
	var candidates []candidate

	for _, m := range cur {
		if m.UserID == "" {
			continue
		}
		if m.ReqCount == 0 && m.CostUSD == 0 && m.TotalTokens == 0 {
			continue
		}
		pCost, ok := prevCostMap[m.UserID]
		if !ok {
			continue // 仅计算两期都有的成员
		}
		var wowCost float64
		if pCost != 0 {
			wowCost = (m.CostUSD - pCost) / pCost * 100
		}
		var wowReq float64
		pReq := prevReqMap[m.UserID]
		if pReq != 0 {
			wowReq = float64(m.ReqCount-pReq) / float64(pReq) * 100
		}
		cat := jobSlotDerive(m.JobCategory, m.JobFamily, m.JobTitle)
		abs := wowCost
		if abs < 0 {
			abs = -abs
		}
		candidates = append(candidates, candidate{
			userID:     m.UserID,
			wowCostPct: wowCost,
			wowReqPct:  wowReq,
			jobCat:     cat,
			absWoW:     abs,
		})
	}

	// 按 |WoWCostPct| 降序
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].absWoW > candidates[j].absWoW
	})
	if len(candidates) > 5 {
		candidates = candidates[:5]
	}

	result := make([]report.TopChangedMember, len(candidates))
	for i, c := range candidates {
		// 从 userTopModels 取该成员成本最高的模型名
		topModel := ""
		if tms, ok := userTopModels[c.userID]; ok && len(tms) > 0 {
			topModel = tms[0].Model // top_models 已按 cost DESC 排序
		}
		result[i] = report.TopChangedMember{
			Idx:         i + 1,
			JobCategory: c.jobCat,
			WoWCostPct:  c.wowCostPct,
			WoWReqPct:   c.wowReqPct,
			TopModel:    topModel,
			AnomalyRule: "", // derive 层不访问 Anomalies，置空
		}
	}
	return result
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
	durSlice := make([]float64, 0, 512)
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
			durSlice = append(durSlice, float64(c.DurationMs))
		}
	}
	p.ModelCount = len(modelSet)
	p.ActiveDays = len(daySet)
	if durCount > 0 {
		p.AvgDurationMs = float64(durSum) / float64(durCount)
	}
	if len(durSlice) > 0 {
		sort.Float64s(durSlice)
		p.P95DurationMs = Percentile(durSlice, 95)
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
	// 派生指标：同岗画像所需（输入ctx / 缓存占比 / 每百万成本 / 成本最高模型占比）。
	if p.ReqCount > 0 && p.PromptTokens > 0 {
		p.AvgInputCtxTokens = float64(p.PromptTokens) / float64(p.ReqCount)
	}
	if p.PromptTokens > 0 {
		p.CacheHitRate = float64(p.CachedTokens) / float64(p.PromptTokens)
	}
	if p.TotalTokens > 0 {
		p.CostPer1Million = p.EstCostUSD / float64(p.TotalTokens) * 1e6
	}
	if p.TopModels != nil && p.EstCostUSD > 0 {
		for _, tm := range p.TopModels {
			if tm.Model != "" && tm.CostUSD > 0 {
				p.TopModel = tm.Model
				p.HighTierCostRatio = tm.CostUSD / p.EstCostUSD
				break
			}
		}
	}
	return p
}

func buildUnitFact(in report.ReportInput, level string) *report.UnitFact {
	u := &report.UnitFact{ScopeID: in.LevelMeta.ID, ScopeName: in.LevelMeta.Name, StudioID: in.LevelMeta.StudioID}
	modelSet := map[string]struct{}{}
	serviceDaySet := map[string]struct{}{}
	activeUsers := map[string]struct{}{}
	userDays := map[string]map[string]struct{}{}
	modelAgg := map[string]*report.TopModel{}
	var memberCachedTokens, memberPromptTokens int64
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
		memberCachedTokens += m.CachedTokens
		memberPromptTokens += m.PromptTokens
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
	// 效能字段（明细模式）
	// CacheHitRate = Σcached / Σprompt
	if memberPromptTokens > 0 {
		u.Member.CacheHitRate = float64(memberCachedTokens) / float64(memberPromptTokens)
	}
	// CostPer1Million = Σcost / Σtotal_tokens * 1e6
	if u.Member.TotalTokens > 0 {
		u.Member.CostPer1Million = u.Member.EstCostUSD / float64(u.Member.TotalTokens) * 1e6
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

// ===== Vicky 工作室模板派生（t2：一页摘要 / 部门环比 / 成本集中度 / 变化归因）=====

// buildDeptContributionWoW 计算各部门成本周环比（WoWCostPct）与工作室成本集中度（CostConcentration）。
//   - WoWCostPct：用 in.PrevMemberAgg 按 DeptID（与 buildDeptContributions 相同的归并口径）分组对比；
//     无上期成本时为 0。
//   - CostConcentration：Top2 部门 CostRatio 之和（dcs 已按 CostUSD 降序，[0]/[1] 即 Top2；不足 2 部门时收敛 1）。
//
// 返回更新后的 dcs 与 CostConcentration。仅在 studio 级别调用；不改动部门/个人路径。
func buildDeptContributionWoW(in report.ReportInput, dcs []report.DeptContribution) ([]report.DeptContribution, float64) {
	if len(dcs) == 0 {
		return dcs, 0
	}
	prevCost := map[string]float64{}
	for _, m := range in.PrevMemberAgg {
		if m.ReqCount == 0 && m.CostUSD == 0 && m.TotalTokens == 0 {
			continue
		}
		deptID := m.DeptID
		if deptID == "" {
			deptID = "(未分配部门)"
		}
		prevCost[deptID] += m.CostUSD
	}
	for i := range dcs {
		if p, ok := prevCost[dcs[i].DeptID]; ok && p != 0 {
			dcs[i].WoWCostPct = (dcs[i].CostUSD - p) / p * 100
		}
	}
	var conc float64
	for i := 0; i < len(dcs) && i < 2; i++ {
		conc += dcs[i].CostRatio
	}
	if conc > 1 {
		conc = 1
	}
	return dcs, conc
}

// buildStudioSummary 填充 Vicky 工作室「一页摘要」的派生字段（确定性、可复算）：
// CompanyCostUSD / CompanyRatio / AnomalySummary / WeeklyTrend / OptimizableCostUSD。
// 数据不足时安全降级（对应字段保持 0 / nil），不 panic。
func buildStudioSummary(unitFact *report.UnitFact, in report.ReportInput, judge *report.Judgment) {
	if unitFact == nil {
		return
	}
	// 全公司占比（CompanyCostUSD 由 tool 侧填入；为 0 时不下发占比）
	unitFact.CompanyCostUSD = in.CompanyCostUSD
	if in.CompanyCostUSD > 0 && unitFact.Member.EstCostUSD > 0 {
		unitFact.CompanyRatio = unitFact.Member.EstCostUSD / in.CompanyCostUSD
	}
	// 4 周趋势（原始序列；升/降/平与周均由呈现层据此序列降级呈现）
	if len(in.WeeklyTrend) > 0 {
		unitFact.WeeklyTrend = in.WeeklyTrend
	}
	// 未关闭异常按 Severity 统计（Hit 且 !NotComputed）
	if judge != nil {
		sevCount := map[string]int{}
		var sevs []string
		for _, a := range judge.Anomalies {
			if !a.Hit || a.NotComputed {
				continue
			}
			if sevCount[a.Severity] == 0 {
				sevs = append(sevs, a.Severity)
			}
			sevCount[a.Severity]++
		}
		if len(sevs) > 0 {
			sort.Slice(sevs, func(i, j int) bool { return severityRank(sevs[i]) < severityRank(sevs[j]) })
			for _, s := range sevs {
				unitFact.AnomalySummary = append(unitFact.AnomalySummary, report.AnomalyCount{Severity: s, Count: sevCount[s]})
			}
		}
	}
	// 预计可优化成本（近值估算）
	unitFact.OptimizableCostUSD = estimateOptimizable(unitFact, in)
}

// severityRank 返回异常等级的中文排序权重（P0<P1<P2<P3），未知等级靠后（用于 AnomalySummary 稳定排序）。
func severityRank(s string) int {
	switch s {
	case "P0":
		return 0
	case "P1":
		return 1
	case "P2":
		return 2
	case "P3":
		return 3
	default:
		return 99
	}
}

// estimateOptimizable 近值估算可优化成本（USD），全部确定性可复算：
//   - 缓存命中率 < 30%：约省总成本 * 0.1；
//   - 高价值模型成本占比偏高（>=50%）：约省成本最高模型 * 0.2；
//   - 以上均未触发时给一个保守近值（成本最高模型 * 0.1，或总成本 * 5%），保证不返回 0。
func estimateOptimizable(u *report.UnitFact, in report.ReportInput) float64 {
	total := u.Member.EstCostUSD
	if total <= 0 {
		return 0
	}
	// 成本最高模型成本（agg 模式优先 UserTopModels；明细模式 Member.TopModels 已按 Calls 排序，取其成本）
	topModelCost := 0.0
	if len(u.Member.TopModels) > 0 {
		topModelCost = u.Member.TopModels[0].CostUSD
	}
	if topModelCost <= 0 {
		topModelCost = topModelCostOf(in)
	}
	var opt float64
	if u.Member.CacheHitRate > 0 && u.Member.CacheHitRate < 0.3 {
		opt += total * 0.1
	}
	// 高价值模型成本占比：agg 模式无 HighTierCostRatio 字段，用成本最高模型 / 总成本近似。
	highRatio := 0.0
	if topModelCost > 0 {
		highRatio = topModelCost / total
	}
	if highRatio >= 0.5 && topModelCost > 0 {
		opt += topModelCost * 0.2
	}
	if opt <= 0 {
		if topModelCost > 0 {
			opt = topModelCost * 0.1
		} else {
			opt = total * 0.05
		}
	}
	return opt
}

// topModelCostOf 从 in.UserTopModels 取全部用户 Top 模型中的单模型最大成本（agg 模式无 Member.TopModels）。
func topModelCostOf(in report.ReportInput) float64 {
	var maxCost float64
	for _, tms := range in.UserTopModels {
		for _, tm := range tms {
			if tm.CostUSD > maxCost {
				maxCost = tm.CostUSD
			}
		}
	}
	return maxCost
}

// studioModelAgg 模型维度聚合（跨用户累加），用于变化归因的中间表示。
type studioModelAgg struct {
	calls  int64
	tokens int64
	cost   float64
}

// buildStudioAttribution 用本期 vs 上期每用户 Top 模型列表对比，把工作室成本增量拆为 5 类来源：
// 业务调用量增长、模型切换(含单价/档位)、上下文变长、错误重试、新项目(新增用户/模型组合)。
//
// 每条 Attribution{Subject, Detail, Weight}。约定 Σd_i = totalDelta（总增量），Weight = d_i/totalDelta，
// 因此 ΣWeight = 1，render 可用「成本值 = 总增量 × Weight」还原各来源增量；totalDelta 恰为 0 时按 Σ|Δ| 归一。
// 无上期数据（in.PrevUserTopModels 为空）时返回 nil，由 render 降级。
// 数据不足的来源 Weight=0（Detail 注明）；所有推导确定性、可复算。
func buildStudioAttribution(in report.ReportInput) []report.Attribution {
	if len(in.PrevUserTopModels) == 0 {
		return nil
	}
	cur := aggregateStudioModels(in.UserTopModels)
	prev := aggregateStudioModels(in.PrevUserTopModels)
	var curCalls, prevCalls int64
	var curCost, prevCost, curTokens, prevTokens float64
	for _, a := range cur {
		curCalls += a.calls
		curTokens += float64(a.tokens)
		curCost += a.cost
	}
	for _, a := range prev {
		prevCalls += a.calls
		prevTokens += float64(a.tokens)
		prevCost += a.cost
	}
	if curCalls == 0 || prevCalls == 0 {
		return nil
	}
	prevUnitCost := prevCost / float64(prevCalls)
	prevUnitTokenCost := safeDiv(prevCost, prevTokens)
	prevTokensPerCall := float64(prevTokens) / float64(prevCalls)
	curTokensPerCall := float64(curTokens) / float64(curCalls)

	// ① 业务调用量增长：调用差 × 上期单位成本
	d1 := float64(curCalls-prevCalls) * prevUnitCost
	// ③ 上下文变长：token/调用 比值变化 × 上期单位 token 成本 × 本期调用
	d3 := (curTokensPerCall - prevTokensPerCall) * float64(curCalls) * prevUnitTokenCost
	// ④ 错误重试：error/req 比值变化 × 上期单位成本 × 本期调用（成员口径；无数据时为 0）
	d4 := errorRateDelta(in) * float64(curCalls) * prevUnitCost
	// ⑤ 新项目：本期新增 (user,model) 组合的成本（上期不存在）
	newKeys := map[string]bool{}
	for user, tms := range in.PrevUserTopModels {
		for _, tm := range tms {
			newKeys[user+"\x00"+tm.Model] = true
		}
	}
	var d5 float64
	for user, tms := range in.UserTopModels {
		for _, tm := range tms {
			if !newKeys[user+"\x00"+tm.Model] {
				d5 += tm.CostUSD
			}
		}
	}
	// ② 模型切换：剩余增量 = 总模型成本增量 - 其他四类（可正可负，代表模型/单价/档位变化）
	totalDelta := curCost - prevCost
	d2 := totalDelta - (d1 + d3 + d4 + d5)

	sources := []struct {
		subject string
		detail  string
		delta   float64
	}{
		{"业务调用量增长", fmt.Sprintf("调用量由 %d -> %d，按上期单位成本估算", prevCalls, curCalls), d1},
		{"模型切换", fmt.Sprintf("剔除调用量/上下文/错误/新项目影响后的模型与单价变化，估算 %s", fmtUSD(d2)), d2},
		{"上下文变长", fmt.Sprintf("单位调用 Token 由 %.0f -> %.0f，按上期单位 token 成本估算", prevTokensPerCall, curTokensPerCall), d3},
		{"错误重试", fmt.Sprintf("错误率变化 %s -> %s，导致的估算额外成本", errorRatePct(in.PrevMemberAgg), errorRatePct(in.MemberAgg)), d4},
		{"新项目", fmt.Sprintf("本期新增用户/模型组合成本合计 %s", fmtUSD(d5)), d5},
	}

	// Weight = 该来源成本增量 / 总增量（Σd_i = totalDelta，故 ΣWeight = 1）。
	// 约定 render 用「成本值 = 总增量 × Weight」还原该来源增量。
	// 当总增量恰为 0 时（净变化为 0，无法除），退化为 Σ|Δ| 归一以保留相对量级。
	var absSum float64
	for _, s := range sources {
		absSum += math.Abs(s.delta)
	}
	out := make([]report.Attribution, 0, len(sources))
	for _, s := range sources {
		w := 0.0
		if totalDelta != 0 {
			w = s.delta / totalDelta
		} else if absSum > 0 {
			w = s.delta / absSum
		}
		out = append(out, report.Attribution{Subject: s.subject, Detail: s.detail, Weight: w})
	}
	return out
}

// aggregateStudioModels 把 per-user Top 模型累加为模型维度聚合（跨用户）。
func aggregateStudioModels(utm map[string][]report.CohortTopModel) map[string]*studioModelAgg {
	m := map[string]*studioModelAgg{}
	for _, tms := range utm {
		for _, tm := range tms {
			a := m[tm.Model]
			if a == nil {
				a = &studioModelAgg{}
				m[tm.Model] = a
			}
			a.calls += tm.CallCount
			a.tokens += tm.TotalTokens
			a.cost += tm.CostUSD
		}
	}
	return m
}

// memberErrorRate 返回成员口径的 error/req（0..1；分母为 0 时为 0）。
func memberErrorRate(aggs []report.MemberAgg) float64 {
	var req, err int64
	for _, m := range aggs {
		req += m.ReqCount
		err += m.ErrorCount
	}
	return safeDiv(float64(err), float64(req))
}

// errorRateDelta 返回本期与上期 error/req 比值之差（无错误数据时为 0）。
func errorRateDelta(in report.ReportInput) float64 {
	return memberErrorRate(in.MemberAgg) - memberErrorRate(in.PrevMemberAgg)
}

// errorRatePct 返回百分比字符串（如 "5.0%"）。
func errorRatePct(aggs []report.MemberAgg) string {
	return fmt.Sprintf("%.1f%%", memberErrorRate(aggs)*100)
}

func topModelsFromUnit(unit report.UnitMember) []report.TopModel {
	return unit.TopModels
}

func buildBaselineSeries(in report.ReportInput, rules report.RuleSet) BaseResult {
	// 当前值 = 当前周期成员调用日均（排除 service，口径与异常/分位一致）。
	// agg 模式：用 BaselineCurrent（当期按天序列，工具侧从 /usage/report/baseline 拉取，
	// 与历史基线同接口同过滤）；非 agg：Metrics 已含逐日明细。两者都只取 member 非 service。
	var current float64
	byDay := map[string]int64{}
	currentMetrics := in.Metrics
	if len(currentMetrics) == 0 {
		currentMetrics = in.BaselineCurrent
	}
	for _, m := range currentMetrics {
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

	// 口径：当期无逐日调用数据（如 -agg 成员聚合模式且 baseline 接口当期也无按天数据）→
	// 无法计算当期日均，禁用「当前=0 去套历史分位」的误导性判定，明确降级不做基线判定。
	if len(byDay) == 0 {
		return BaseResult{
			Supported:     false,
			WindowDays:    rules.BaselineWindowDays,
			SampleDays:    0,
			Current:       0,
			CurrentWindow: "期前28日物化",
			Trend:         "flat",
			Note:          "当前周期无逐日调用数据（-agg 成员聚合模式不拉逐日明细），无法计算当期日均，不做基线判定。",
		}
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

// unitCostPerActiveUser 返回本单元「单位活跃人成本」= ΣEstCostUSD / 活跃人数（PerCapita.EstCostUSD 口径）。
// 活跃人数为 0 时返回 0（避免除零；此时不产横比）。
func unitCostPerActiveUser(u *report.UnitFact) float64 {
	if u == nil || u.Member.ActiveUserCount <= 0 {
		return 0
	}
	return u.Member.EstCostUSD / float64(u.Member.ActiveUserCount)
}

// buildDeptUnitCostPeer 计算单元级匿名横比（judge.PeerUnitCost）：用全公司每用户指标（in.CompanyUserMetrics）
// 按 DeptName 分组，各部门「单位活跃人成本 = ΣEstCostUSD / 去重UserName 活跃人数」，得出各部门单位活跃人成本分布；
// 再对 current（本单元单位活跃人成本）用 PeerPercentile 求在全公司各部门中的分位。
// 池含当前部门自身（跨公司横比口径）；DeptName 为空、无活跃人的部门不计入池。
// CompanyUserMetrics 为空或池有效样本 < minSample 时返回 supported=false，不产误导性横比。
func buildDeptUnitCostPeer(in report.ReportInput, current float64, minSample int) report.Peer {
	byDept := map[string]*struct {
		cost  float64
		users map[string]struct{}
	}{}
	for _, um := range in.CompanyUserMetrics {
		dept := um.DeptName
		if dept == "" {
			continue
		}
		e := byDept[dept]
		if e == nil {
			e = &struct {
				cost  float64
				users map[string]struct{}
			}{users: map[string]struct{}{}}
			byDept[dept] = e
		}
		e.cost += um.EstCostUSD
		if um.UserName != "" {
			e.users[um.UserName] = struct{}{}
		}
	}
	members := make([]PortfolioMember, 0, len(byDept))
	for _, e := range byDept {
		n := len(e.users)
		if n == 0 {
			continue
		}
		members = append(members, PortfolioMember{Valid: true, Value: e.cost / float64(n)})
	}
	if len(in.CompanyUserMetrics) == 0 {
		return report.Peer{
			Metric: "unit_cost_per_active_user", PoolType: "company_departments", Anonymized: true, Current: current,
			Note: "无全公司每用户数据（CompanyUserMetrics 为空），不做匿名横比。",
		}
	}
	res := PeerPercentile("unit_cost_per_active_user", "company_departments", "cost", members, current, minSample)
	return mapPeer(res)
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

// peerMetricDef 描述同岗画像的一个指标：key / 中文标签 / 单位 / 从本人取当前值 / 从池成员取池值。
type peerMetricDef struct {
	Key   string
	Label string
	Unit  string
	cur   func(p *report.PersonalFact) float64
	pool  func(m report.CohortMetrics) float64
}

// buildPeerProfiles 计算 Vicky「同岗使用效能画像」多指标分位表。
// 每个指标独立判采样；池样本 < minSample 时该指标 supported=false（不整体失败）。
// direction：成本类/占比越高越需关注，这里只给分位不额外定好坏（呈现层自行解释），
// 但所有指标统一用 PercentRank 表示"本人处于同岗的什么位置"。
func buildPeerProfiles(in report.ReportInput, f report.Fact, minSample int) []report.PeerMetric {
	defs := []peerMetricDef{
		{Key: "weekly_calls", Label: "周调用次数", Unit: "次", cur: func(p *report.PersonalFact) float64 { return float64(p.ReqCount) }, pool: func(m report.CohortMetrics) float64 { return float64(m.ReqCount) }},
		{Key: "weekly_tokens", Label: "周 Token 总量", Unit: "", cur: func(p *report.PersonalFact) float64 { return float64(p.TotalTokens) }, pool: func(m report.CohortMetrics) float64 { return float64(m.TotalTokens) }},
		{Key: "active_days", Label: "活跃天数", Unit: "天", cur: func(p *report.PersonalFact) float64 { return float64(p.ActiveDays) }, pool: func(m report.CohortMetrics) float64 { return float64(m.ActiveDays) }},
		{Key: "avg_input_ctx", Label: "平均输入上下文", Unit: "token", cur: func(p *report.PersonalFact) float64 { return p.AvgInputCtxTokens }, pool: func(m report.CohortMetrics) float64 { return m.AvgInputCtxTokens }},
		{Key: "cache_hit_rate", Label: "缓存 Token 占比", Unit: "%", cur: func(p *report.PersonalFact) float64 { return p.CacheHitRate }, pool: func(m report.CohortMetrics) float64 { return m.CacheHitRate }},
		{Key: "cost_per_1m", Label: "每百万 Token 成本", Unit: "USD", cur: func(p *report.PersonalFact) float64 { return p.CostPer1Million }, pool: func(m report.CohortMetrics) float64 { return m.CostPer1kTokens }},
		{Key: "high_tier_cost_ratio", Label: "成本最高模型成本占比", Unit: "%", cur: func(p *report.PersonalFact) float64 { return p.HighTierCostRatio }, pool: func(m report.CohortMetrics) float64 { return m.HighTierCostRatio }},
	}
	// 仅个人粒度有同岗画像；其他层级直接返回空表。
	if f.Personal == nil {
		return nil
	}
	// 本人各指标当前值
	cur := func(d peerMetricDef) float64 { return d.cur(f.Personal) }

	out := make([]report.PeerMetric, 0, len(defs))
	for _, d := range defs {
		vals := make([]float64, 0, len(in.Cohort.Members))
		for _, m := range in.Cohort.Members {
			v := d.pool(m.Metrics)
			// 该成员此项有值（非零）才入池；由工具侧在算 Metrics 时置 0 表示缺失
			if v != 0 {
				vals = append(vals, v)
			}
		}
		pm := report.PeerMetric{Metric: d.Key, Label: d.Label, Unit: d.Unit}
		if len(vals) == 0 {
			pm.Supported = false
			pm.Note = "同岗样本为 0，该指标不产出分位。"
			out = append(out, pm)
			continue
		}
		sort.Float64s(vals)
		pm.Supported = true
		pm.P50 = Percentile(vals, 50)
		pm.Current = cur(d)
		pm.PercentileRank = PercentRank(vals, pm.Current)
		if len(vals) < minSample {
			pm.Note = "该指标同岗有效样本仅 " + itoa(len(vals)) + " 人，分位仅供参考。"
		}
		out = append(out, pm)
	}
	return out
}

// buildTopModelPortrait 计算 Vicky 第四章「成本最高模型的同岗画像」。
// 从本人 TopModels[0] 取成本最高模型，遍历同岗池成员找出也用过该模型的采用者，
// 算采用率 / 频率 P50 / 成本依赖 P50 / 单位成本 P50（全部在采用者子集内）。
func buildTopModelPortrait(in report.ReportInput, f report.Fact, minSample int) *report.TopModelPortrait {
	if f.Personal == nil || len(f.Personal.TopModels) == 0 || f.Personal.TopModels[0].CostUSD == 0 {
		return nil
	}
	topModel := f.Personal.TopModels[0].Model
	personCostUSD := f.Personal.TopModels[0].CostUSD
	personCalls := float64(f.Personal.TopModels[0].Calls)

	portrait := &report.TopModelPortrait{
		Model:         topModel,
		PersonCostUSD: personCostUSD,
		PersonFreq:    personCalls,
		PoolN:         len(in.Cohort.Members),
	}
	if f.Personal.EstCostUSD > 0 {
		portrait.PersonCostRatio = personCostUSD / f.Personal.EstCostUSD
	}
	if f.Personal.TotalTokens > 0 && personCostUSD > 0 {
		personTokens := f.Personal.TopModels[0].TotalTokens
		if personTokens > 0 {
			portrait.PersonUnitCost = personCostUSD / float64(personTokens) * 1e6
		}
	}

	// 遍历同岗池，找采用了该模型的成员
	var freqVals, costDepVals, unitCostVals []float64
	for _, m := range in.Cohort.Members {
		for _, tm := range m.Metrics.TopModels {
			if tm.Model != topModel {
				continue
			}
			portrait.AdoptionCount++
			freqVals = append(freqVals, float64(tm.CallCount))
			if m.Metrics.EstCostUSD > 0 {
				costDepVals = append(costDepVals, tm.CostUSD/m.Metrics.EstCostUSD)
			}
			if tm.TotalTokens > 0 {
				unitCostVals = append(unitCostVals, tm.CostUSD/float64(tm.TotalTokens)*1e6)
			}
			break
		}
	}
	if portrait.PoolN > 0 {
		portrait.AdoptionRate = float64(portrait.AdoptionCount) / float64(portrait.PoolN)
	}
	if len(freqVals) < minSample {
		// 采用者样本不足 MinCohortSample(5)：仍按实际人数计算并展示分位，仅标注样本少、供参考。
		// 不再整体降级为「仅展示采用率」——分位数对任意非空样本都有定义（n=1 时 P50=该值）。
		// 数值仍来自真实数据，不造假；但需提示读者样本小、统计意义有限。
		portrait.Supported = len(freqVals) > 0
		if portrait.Supported {
			portrait.Note = fmt.Sprintf("采用者样本仅 %d 人（不足 %d），分位数仅供参考。", len(freqVals), minSample)
		} else {
			portrait.Note = "本周同岗无人使用该模型，无法计算采用者分位。"
		}
		sort.Float64s(freqVals)
		portrait.FreqP50 = Percentile(freqVals, 50)
		portrait.PersonFreqRank = PercentRank(freqVals, personCalls)
		if len(costDepVals) > 0 {
			sort.Float64s(costDepVals)
			portrait.CostDepP50 = Percentile(costDepVals, 50)
			portrait.PersonCostDepRank = PercentRank(costDepVals, portrait.PersonCostRatio)
		}
		if len(unitCostVals) > 0 {
			sort.Float64s(unitCostVals)
			portrait.UnitCostP50 = Percentile(unitCostVals, 50)
			portrait.PersonUnitCostRank = PercentRank(unitCostVals, portrait.PersonUnitCost)
		}
		return portrait
	}
	portrait.Supported = true
	sort.Float64s(freqVals)
	portrait.FreqP50 = Percentile(freqVals, 50)
	portrait.PersonFreqRank = PercentRank(freqVals, personCalls)
	if len(costDepVals) > 0 {
		sort.Float64s(costDepVals)
		portrait.CostDepP50 = Percentile(costDepVals, 50)
		portrait.PersonCostDepRank = PercentRank(costDepVals, portrait.PersonCostRatio)
	}
	if len(unitCostVals) > 0 {
		sort.Float64s(unitCostVals)
		portrait.UnitCostP50 = Percentile(unitCostVals, 50)
		portrait.PersonUnitCostRank = PercentRank(unitCostVals, portrait.PersonUnitCost)
	}
	return portrait
}

// ---- 表达层 ----

func buildExpression(rep *report.Report, rules report.RuleSet) report.Expression {
	expr := report.Expression{Lang: "zh", Audience: "个人/单元负责人", Tone: "中性/基于证据"}
	j := &rep.Judgment
	p := rep.Fact.Personal

	// ── 异常摘要 ──────────────────────────────────────────────────
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

	// ── 效能标签（个人粒度 + 同岗画像有数据才产出）────────────────
	if p != nil && len(j.PeerProfiles) > 0 {
		expr.EfficiencyLabel = buildEfficiencyLabel(p, j.PeerProfiles)
	}

	// ── insights ─────────────────────────────────────────────────
	for _, lbl := range j.Anomalies {
		if lbl.Hit {
			expr.Insights = append(expr.Insights, "命中规则 "+lbl.Rule+"（"+lbl.Name+"）："+lbl.Note)
		}
	}

	// ── suggestions：异常规则建议 ─────────────────────────────────
	for _, a := range j.Anomalies {
		if a.Hit {
			expr.Suggestions = append(expr.Suggestions, suggestionsFor(a.Rule, a.Name))
		}
	}

	// ── suggestions：数据驱动建议（基于已算指标，无 LLM）─────────
	if p != nil {
		expr.Suggestions = append(expr.Suggestions, dataBasedSuggestions(p, j)...)
	}

	if len(expr.Suggestions) == 0 {
		expr.Suggestions = []string{"未命中规则型异常，保持现状并在下期继续观察。"}
	}
	return expr
}

// buildEfficiencyLabel 从同岗 7 指标分位综合判断效能标签（确定性，参考 Vicky 模板）。
// 规则：采用量（调用/Token/活跃）高于 P50 → 高采用；低于 P25 → 低采用；其余 → 中等采用。
// 效能（成本/上下文/缓存）处于 P75 以上偏高 → 待优化；低于 P50 → 良好；否则 → 正常。
func buildEfficiencyLabel(p *report.PersonalFact, profiles []report.PeerMetric) string {
	usageRank := -1.0 // 调用量维度在同岗的平均分位（calls/tokens/active_days 均值）
	effRank := -1.0   // 效能维度（cost/ctx/cache）中最差的分位

	usageCnt := 0
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
			// 成本越高分位越高 → 越"待优化"
			if effRank < pm.PercentileRank {
				effRank = pm.PercentileRank
			}
		case "avg_input_ctx":
			if effRank < pm.PercentileRank {
				effRank = pm.PercentileRank
			}
		case "cache_hit_rate":
			// 缓存占比越高越好；低分位 = 待优化
			inv := 1 - pm.PercentileRank
			if effRank < inv {
				effRank = inv
			}
		}
	}
	if usageCnt == 0 && effRank < 0 {
		return ""
	}

	usagePart := "中等采用"
	if usageRank >= 0 {
		switch {
		case usageRank >= 0.6:
			usagePart = "高采用"
		case usageRank < 0.3:
			usagePart = "低采用"
		}
	}
	effPart := "效能正常"
	if effRank >= 0 {
		switch {
		case effRank >= 0.75:
			effPart = "待优化"
		case effRank < 0.4:
			effPart = "效能良好"
		}
	}
	return usagePart + "、" + effPart
}

// dataBasedSuggestions 基于已算 PersonalFact 指标生成具体建议（确定性，无 LLM）。
func dataBasedSuggestions(p *report.PersonalFact, j *report.Judgment) []string {
	var sugs []string

	// 建议：输入上下文偏高
	if p.AvgInputCtxTokens > 0 {
		for _, pm := range j.PeerProfiles {
			if pm.Metric == "avg_input_ctx" && pm.Supported && pm.PercentileRank >= 0.75 {
				sugs = append(sugs, "上下文：单次平均输入（"+fmtK(p.AvgInputCtxTokens)+"k token）高于同岗 P75，建议压缩历史会话或用检索召回替代长上下文。")
				break
			}
		}
	}

	// 建议：缓存占比偏低
	if p.CacheHitRate >= 0 {
		for _, pm := range j.PeerProfiles {
			if pm.Metric == "cache_hit_rate" && pm.Supported && pm.PercentileRank < 0.25 {
				sugs = append(sugs, "缓存：缓存 Token 占比（"+pctStr(p.CacheHitRate)+"）低于同岗 P25，检查是否反复传相同前缀，可启用 prompt caching 节省成本。")
				break
			}
		}
	}

	// 建议：每百万 Token 成本偏高
	if p.CostPer1Million > 0 {
		for _, pm := range j.PeerProfiles {
			if pm.Metric == "cost_per_1m" && pm.Supported && pm.PercentileRank >= 0.75 {
				sugs = append(sugs, "合理用模：每百万 Token 成本（"+fmtUSD(p.CostPer1Million)+"/M）高于同岗 P75，可评估对部分场景降级至更经济模型。")
				break
			}
		}
	}

	// 建议：成本下降（正面反馈）
	if p.WoWCostPct < -20 {
		sugs = append(sugs, "成本：本周成本环比下降 "+pctStr(-p.WoWCostPct)+"，趋势良好，持续保持。")
	}
	// 建议：成本上升且无异常命中
	if p.WoWCostPct > 50 && j.TopModelPortrait != nil && !j.TopModelPortrait.Supported {
		sugs = append(sugs, "成本：本周成本环比上升 "+pctStr(p.WoWCostPct)+"，建议关注调用量或模型选择变化。")
	}

	return sugs
}

func fmtK(v float64) string { return fmt.Sprintf("%.0f", v/1000) }
func pctStr(v float64) string {
	if v < 1 {
		return fmt.Sprintf("%.1f%%", v*100)
	}
	return fmt.Sprintf("%.1f%%", v)
}
func fmtUSD(v float64) string { return fmt.Sprintf("$%.2f", v) }

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

// jobSlotDerive 岗位回退：title → category → family → "其他"。
// 优先展示职位名（如"WEB前端开发"），无则退回岗位类别，再退族，都空归"其他"。
// 与 tools/usage-report/cohort.go 的 jobSlot 语义一致，但在 derive 包内独立实现（无工具层依赖）。
func jobSlotDerive(category, family, title string) string {
	if title != "" {
		return title
	}
	if category != "" {
		return category
	}
	if family != "" {
		return family
	}
	return "其他"
}
