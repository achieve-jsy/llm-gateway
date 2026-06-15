package derive

// anomalies.go —— A1–A5 分钟窗口判定（判断层，规则型，命中必须附证据）。
//
// 输入为归一化逐调用事件 CallEvent；模型上下文预算来自 model_catalog（A4）。
// 全部规则确定性、可复现；阈值来自 RuleSet（阶段一默认，T4 校准）。

import (
	"math"
	"sort"
	"time"

	"github.com/example/llm-gateway/internal/observability/report"
)

// Anomalies 返回 A1–A5 判定结果（命中/未命中都返回，未命中附 basis）。
func Anomalies(calls, baselineCalls []CallEvent, rules report.RuleSet, modelBudget map[string]int64) []report.AnomalyHit {
	return []report.AnomalyHit{
		A1(calls, baselineCalls, rules),
		A2(calls, baselineCalls, rules),
		A3(calls, rules),
		A4(calls, rules, modelBudget),
		A5(calls, rules),
	}
}

// ---- 时间桶工具 ----

type ev struct {
	c      CallEvent
	minute int64 // unix 分钟桶
}

type windowStat struct {
	start, end int64
	lo, hi     int
	count      int
	cost       float64
}

func bucket(calls []CallEvent) []ev {
	out := make([]ev, 0, len(calls))
	for _, c := range calls {
		out = append(out, ev{c: c, minute: c.TS.Unix() / 60})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].c.TS.Before(out[j].c.TS) })
	return out
}

// windows 用双指针一次生成逐分钟滑窗，避免每个分钟重新扫描全部调用。
func windows(b []ev, width int64) []windowStat {
	if len(b) == 0 || width <= 0 {
		return nil
	}
	minB, maxB := maxBucket(b)
	out := make([]windowStat, 0, maxB-minB+1)
	lo, hi := 0, 0
	var cost float64
	for s := minB; s <= maxB; s++ {
		for lo < len(b) && b[lo].minute < s {
			if lo < hi {
				cost -= b[lo].c.EstCostUSD
			}
			lo++
			if hi < lo {
				hi = lo
			}
		}
		for hi < len(b) && b[hi].minute < s+width {
			cost += b[hi].c.EstCostUSD
			hi++
		}
		out = append(out, windowStat{start: s, end: s + width, lo: lo, hi: hi, count: hi - lo, cost: cost})
	}
	return out
}

func medianIntervalSec(b []ev) float64 {
	if len(b) < 2 {
		return math.Inf(1)
	}
	ivs := make([]float64, 0, len(b)-1)
	for i := 1; i < len(b); i++ {
		ivs = append(ivs, b[i].c.TS.Sub(b[i-1].c.TS).Seconds())
	}
	return Median(ivs)
}

func maxBucket(b []ev) (int64, int64) {
	if len(b) == 0 {
		return 0, 0
	}
	return b[0].minute, b[len(b)-1].minute
}

func newHit(rule, version, name, severity, window string) report.AnomalyHit {
	return report.AnomalyHit{
		Rule: rule, RuleVersion: version, Name: name, Severity: severity,
		Window: window, Dimensions: map[string]string{}, Evidence: []report.Evidence{},
	}
}

func evg(f string, v float64) report.Evidence { return report.Evidence{Field: f, Value: fmtFloat(v)} }
func evi(f string, v int) report.Evidence     { return report.Evidence{Field: f, Value: itoa(v)} }
func evs(f, v string) report.Evidence         { return report.Evidence{Field: f, Value: v} }

// ---- A1 程序失控 ----

// A1：10 分钟调用 > max(60, 个人P99_10min) 且中位间隔 <10s 且连续 ≥15 分钟（1 分钟粒度滑动窗，勿 tumbling）。
func A1(calls, baselineCalls []CallEvent, rules report.RuleSet) report.AnomalyHit {
	a := newHit("A1", "v1", "程序失控", "P1", itoa(rules.A1WindowMinutes)+"min/连续"+itoa(rules.A1SustainMinutes)+"min")
	if len(calls) < 2 {
		a.Evidence = append(a.Evidence, evs("basis", "调用样本不足，无法判定程序失控。"))
		return a
	}
	b := bucket(calls)
	w := int64(rules.A1WindowMinutes)
	var p99 float64
	if historical := windows(bucket(baselineCalls), w); len(historical) > 0 {
		cnts := make([]float64, 0, len(historical))
		for _, x := range historical {
			cnts = append(cnts, float64(x.count))
		}
		sort.Float64s(cnts)
		p99 = Percentile(cnts, 99)
		a.BaselineWindow = "历史窗口P99_" + itoa(rules.A1WindowMinutes) + "min"
	} else {
		a.BaselineWindow = "无历史窗口，仅绝对阈值"
	}
	thr := math.Max(float64(rules.A1MinCallsAbsolute), math.Ceil(p99))

	bestStart, bestEnd := int64(-1), int64(-1)
	bestCount := 0
	bestInterval := 0.0
	bestCost := 0.0
	streak := 0
	for _, x := range windows(b, w) {
		iv := medianIntervalSec(b[x.lo:x.hi])
		if x.count <= int(thr) || iv >= rules.A1MaxMedianIntervalS {
			streak = 0
			continue
		}
		streak++
		if streak >= rules.A1SustainMinutes && x.count > bestCount {
			bestStart, bestEnd = x.start, x.end
			bestCount, bestInterval, bestCost = x.count, iv, x.cost
		}
	}
	if bestStart < 0 {
		a.Hit = false
		a.Evidence = append(a.Evidence,
			evs("basis", "未发现满足阈值的高调用窗口（窗口调用>max("+itoa(rules.A1MinCallsAbsolute)+",历史P99="+fmtFloat(math.Ceil(p99))+")，且中位间隔<"+fmtFloat(rules.A1MaxMedianIntervalS)+"s，连续≥"+itoa(rules.A1SustainMinutes)+"min）。"))
		return a
	}
	a.Hit = true
	a.ActualValue = float64(bestCount)
	a.ThresholdValue = thr
	a.BaselineValue = p99
	a.MatchedCount = bestCount
	a.ImpactCostUSD = bestCost
	a.ExcessCostUSD = bestCost
	a.Dimensions["rule_version"] = "v1"
	a.Dimensions["key_type"] = "personal"
	a.Evidence = append(a.Evidence,
		evi("window_calls_10min", bestCount),
		evs("median_interval_s", fmtFloat(bestInterval)),
		evs("threshold", "max(60,P99="+fmtFloat(math.Ceil(p99))+")"),
		evs("window", fmtTime(bestStart)+"~"+fmtTime(bestEnd)),
		evs("impact_cost_usd", fmtFloat(bestCost)),
	)
	a.Note = "豁免：key_type=service 白名单/__system_/备案（阶段一未接入备案表）。"
	return a
}

// ---- A2 成本燃烧 ----

// A2：15 分钟成本 > max(¥50, 个人28天同窗中位×5)；另加 HARD_CAP_A2=¥200/15min 独立触发。
func A2(calls, baselineCalls []CallEvent, rules report.RuleSet) report.AnomalyHit {
	a := newHit("A2", "v1", "成本燃烧", "P1", "15min")
	if len(calls) < 2 {
		a.Hit = false
		a.Evidence = append(a.Evidence, evs("basis", "调用样本不足，无法判定成本燃烧。"))
		return a
	}
	rate := rules.USDCNYRate
	if rate <= 0 {
		rate = 7.2
	}
	w := int64(rules.A2WindowMinutes)
	b := bucket(calls)

	var med float64
	if historical := windows(bucket(baselineCalls), w); len(historical) > 0 {
		costVals := make([]float64, 0, len(historical))
		for _, x := range historical {
			costVals = append(costVals, x.cost)
		}
		sort.Float64s(costVals)
		med = Median(costVals)
		a.BaselineWindow = "历史" + itoa(rules.A2WindowMinutes) + "min窗口中位"
	} else {
		a.BaselineWindow = "无历史窗口，仅绝对阈值"
	}
	baseThr := math.Max(med*rules.A2BaselineMedianMultiple, rules.A2AbsMinUSD)

	bestCost := 0.0
	bestStart, bestEnd := int64(-1), int64(-1)
	bestCount := 0
	for _, x := range windows(b, w) {
		if x.cost > bestCost {
			bestCost, bestStart, bestEnd, bestCount = x.cost, x.start, x.end, x.count
		}
	}
	bestCNY := bestCost * rate
	hitBaseline := bestCost > baseThr
	hitHardCap := bestCNY > rules.A2HardCapCNY
	if !hitBaseline && !hitHardCap {
		a.Hit = false
		a.Evidence = append(a.Evidence,
			evs("basis", "未发现 15min 窗口成本超基线(>"+fmtFloat(baseThr)+" USD)或 HARD_CAP(¥"+fmtFloat(rules.A2HardCapCNY)+")。"))
		return a
	}
	a.Hit = true
	a.ActualValue = bestCost
	if hitHardCap {
		a.ThresholdValue = rules.A2HardCapCNY / rate
		a.Note = "触发 HARD_CAP_A2 = ¥" + fmtFloat(rules.A2HardCapCNY) + "/15min（绝对值封顶）。"
		a.ExcessCostUSD = bestCost - rules.A2HardCapCNY/rate
	} else {
		a.ThresholdValue = baseThr
		a.ExcessCostUSD = bestCost - baseThr
	}
	if a.ExcessCostUSD < 0 {
		a.ExcessCostUSD = 0
	}
	a.BaselineValue = med
	a.MatchedCount = bestCount
	a.ImpactCostUSD = bestCost
	a.Dimensions["rule_version"] = "v1"
	a.Dimensions["usd_cny_rate"] = fmtFloat(rate)
	a.Dimensions["total_cost_cny"] = fmtFloat(bestCNY)
	a.Evidence = append(a.Evidence,
		evs("baseline_median_usd", fmtFloat(med)),
		evs("threshold_usd", fmtFloat(baseThr)),
		evs("hard_cap_cny", fmtFloat(rules.A2HardCapCNY)),
		evs("actual_cny", fmtFloat(bestCNY)),
		evs("window", fmtTime(bestStart)+"~"+fmtTime(bestEnd)),
		evs("impact_cost_usd", fmtFloat(bestCost)),
	)
	return a
}

// ---- A3 重试循环 ----

// A3：段=(key_id,model)，段内 req≥20 ∧ fail_rate≥30% ∧ 单一 status_code 占失败≥80% ∧ failed≥6。
// 失败定义 status>=400；区分上游故障(5xx→归上游)与客户端僵死(429/400)。
func A3(calls []CallEvent, rules report.RuleSet) report.AnomalyHit {
	a := newHit("A3", "v1", "重试循环", "P2", itoa(rules.A3WindowMinutes)+"min滑窗")
	if len(calls) < 2 {
		a.Evidence = append(a.Evidence, evs("basis", "调用样本不足，无法判定重试循环。"))
		return a
	}
	type seg struct{ keyID, model string }
	groups := map[seg][]ev{}
	for _, c := range calls {
		k := seg{keyID: c.KeyID, model: c.Model}
		groups[k] = append(groups[k], ev{c: c, minute: c.TS.Unix() / 60})
	}
	best := report.AnomalyHit{}
	bestFailed := 0
	width := int64(rules.A3WindowMinutes)
	for k, arr := range groups {
		sort.Slice(arr, func(i, j int) bool { return arr[i].c.TS.Before(arr[j].c.TS) })
		if len(arr) == 0 {
			continue
		}
		lo, hi, failed := 0, 0, 0
		var failCost float64
		statusCnt := map[int]int{}
		minB, maxB := maxBucket(arr)
		for start := minB; start <= maxB; start++ {
			for lo < hi && arr[lo].minute < start {
				if arr[lo].c.Status != 200 {
					failed--
					failCost -= arr[lo].c.EstCostUSD
					statusCnt[arr[lo].c.Status]--
				}
				lo++
			}
			for hi < len(arr) && arr[hi].minute < start+width {
				if arr[hi].c.Status != 200 {
					failed++
					failCost += arr[hi].c.EstCostUSD
					statusCnt[arr[hi].c.Status]++
				}
				hi++
			}
			req := hi - lo
			if req < rules.A3MinSegmentReq || failed < rules.A3MinFailed {
				continue
			}
			failRate := float64(failed) / float64(req)
			domStatus, domShare := dominantStatus(statusCnt, failed)
			if failRate < rules.A3FailRateThresh || domShare < rules.A3DominantFailShare || failed <= bestFailed {
				continue
			}
			best = newHit("A3", "v1", "重试循环", "P2", itoa(rules.A3WindowMinutes)+"min滑窗")
			best.Hit = true
			best.ActualValue, best.ThresholdValue = failRate, rules.A3FailRateThresh
			best.MatchedCount, best.ImpactCostUSD, best.ExcessCostUSD = failed, failCost, failCost
			best.BaselineWindow = "无（规则型）"
			best.Dimensions["key_id"], best.Dimensions["model"] = k.keyID, k.model
			best.Dimensions["segment"] = k.keyID + "|" + k.model
			best.Dimensions["fail_rate"] = fmtFloat(failRate)
			best.Dimensions["dominant_status"] = itoa(domStatus)
			best.Dimensions["dominant_fail_share"] = fmtFloat(domShare)
			if domStatus >= 500 {
				best.Dimensions["kind"] = "上游故障(5xx)"
			} else {
				best.Dimensions["kind"] = "客户端僵死(非200)"
			}
			best.Evidence = append(best.Evidence,
				evi("segment_req", req), evi("failed", failed),
				evs("fail_rate", fmtFloat(failRate)),
				evs("dominant_status", itoa(domStatus)+" 占比 "+fmtFloat(domShare)),
				evs("window", fmtTime(start)+"~"+fmtTime(start+width)),
				evs("impact_cost_usd", fmtFloat(failCost)))
			bestFailed = failed
		}
	}
	if bestFailed == 0 {
		a.Evidence = append(a.Evidence, evs("basis", "未发现配置滑窗内 req/fail_rate/单一status/failed 同时达到阈值的重复失败（失败=status!=200）。"))
		return a
	}
	return best
}

func dominantStatus(cnt map[int]int, failures int) (int, float64) {
	dom := 0
	max := 0
	for s, n := range cnt {
		if n > max {
			max = n
			dom = s
		}
	}
	if failures == 0 {
		return dom, 0
	}
	return dom, float64(max) / float64(failures)
}

// ---- A4 超长上下文 ----

// A4：模型上下文上限来自 model_catalog 的有效输入预算（context_length - max_output_tokens）。
// 触发：窗口内「prompt 近上限（>80% 有效预算 或 >绝对下限）」请求 ≥A4MinNearLimit，且存在单条 >80% 偏噪。
func A4(calls []CallEvent, rules report.RuleSet, budget map[string]int64) report.AnomalyHit {
	a := newHit("A4", "v1", "超长上下文", "P2", itoa(rules.A4WindowMinutes)+"min")
	if len(calls) == 0 {
		a.Evidence = append(a.Evidence, evs("basis", "无调用数据，无法判定超长上下文。"))
		return a
	}
	known := make([]CallEvent, 0, len(calls))
	unknownModels := map[string]struct{}{}
	for _, c := range calls {
		if bd := budget[c.Model]; bd > 0 {
			known = append(known, c)
		} else {
			unknownModels[c.Model] = struct{}{}
		}
	}
	if len(known) == 0 {
		a.NotComputed = true
		a.Evidence = append(a.Evidence, evs("basis", "全部调用缺少 model_catalog 有效输入预算，A4 不可判定。"))
		return a
	}
	b := bucket(known)
	bestNear := 0
	for _, win := range windows(b, int64(rules.A4WindowMinutes)) {
		near, maxPrompt := 0, 0
		var cost float64
		maxModel := ""
		for _, x := range b[win.lo:win.hi] {
			bd := budget[x.c.Model]
			// Catalog 预算是必要事实；绝对下限仅提高阈值，不能替代缺失预算。
			threshold := math.Min(float64(bd)*rules.A4NearLimitRatio, float64(rules.A4MinPromptAbsolute))
			if float64(x.c.PromptTokens) > threshold {
				near++
				cost += x.c.EstCostUSD
			}
			if x.c.PromptTokens > maxPrompt {
				maxPrompt, maxModel = x.c.PromptTokens, x.c.Model
			}
		}
		if near < rules.A4MinNearLimit || near <= bestNear {
			continue
		}
		bd := budget[maxModel]
		best := newHit("A4", "v1", "超长上下文", "P2", itoa(rules.A4WindowMinutes)+"min")
		best.Hit, best.ActualValue = true, float64(near)
		best.ThresholdValue, best.MatchedCount = float64(rules.A4MinNearLimit), near
		best.BaselineValue, best.BaselineWindow = float64(bd), maxModel+" Catalog有效输入预算"
		best.ImpactCostUSD, best.ExcessCostUSD = cost, cost
		best.Dimensions["model"] = maxModel
		best.Evidence = append(best.Evidence,
			evi("near_limit_req_in_window", near), evi("max_prompt", maxPrompt),
			evs("model", maxModel), evs("budget", fmtFloat(float64(bd))),
			evs("ratio", fmtFloat(float64(maxPrompt)/float64(bd))),
			evs("window", fmtTime(win.start)+"~"+fmtTime(win.end)),
			evs("impact_cost_usd", fmtFloat(cost)))
		if len(unknownModels) > 0 {
			best.Note = "部分模型缺少 Catalog 预算，已排除这些调用；缺失预算不会用绝对下限兜底命中。"
		}
		bestNear = near
		a = best
	}
	if bestNear == 0 {
		a.Evidence = append(a.Evidence, evs("basis", "已知 Catalog 预算的调用中，未发现窗口内近上下文上限请求达到阈值。"))
	}
	return a
}

// ---- A5 非工作时段 ----

// A5：00:00–07:00 或周末的「非工作时段成本」> max(同岗位P99, ¥100) 且非备案 → 触发。
// 阶段一：同岗位P99 未接入（t8），降解链落到「仅floor」；HARD_CAP_A5 作为独立封顶（升 P1）。
// 阶段一无备案治理表 → 纯时间窗口触发并标注"阶段二补备案豁免"。
func A5(calls []CallEvent, rules report.RuleSet) report.AnomalyHit {
	a := newHit("A5", "v1", "非工作时段", "P2", "00-08时/周末")
	if len(calls) < rules.A5MinCallCount {
		a.Hit = false
		a.Evidence = append(a.Evidence, evs("basis", "调用量 <"+itoa(rules.A5MinCallCount)+"，样本不足。"))
		return a
	}
	rate := rules.USDCNYRate
	if rate <= 0 {
		rate = 7.2
	}
	// 非工作时段成本（USD 事实源）与调用占比。
	var total, off int
	var offCost float64
	for _, c := range calls {
		total++
		h := c.TS.Hour()
		wk := c.TS.Weekday()
		if h < rules.A5OffHourStart || (rules.A5OffHourEnd != rules.A5OffHourStart && h >= rules.A5OffHourEnd) || wk == time.Saturday || wk == time.Sunday {
			off++
			offCost += c.EstCostUSD
		}
	}
	offShare := float64(off) / float64(total)
	offCNY := offCost * rate
	// floor：max(同岗位P99, ¥100)。阶段一未接入同岗P99（需每用户非工作成本，t8），按降解链回退到「仅floor」。
	floorUSD := rules.A5FloorCNY / rate
	thrUSD := floorUSD
	peerLevel := "仅floor(¥" + fmtFloat(rules.A5FloorCNY) + ")"
	hardcapUSD := rules.A5HardCapCNY / rate
	hitHardCap := offCNY > rules.A5HardCapCNY

	if offCNY <= rules.A5FloorCNY && !hitHardCap {
		a.Hit = false
		a.Evidence = append(a.Evidence,
			evs("basis", "非工作时段成本 "+fmtFloat(offCost)+" USD（¥"+fmtFloat(offCNY)+"）≤ floor ¥"+fmtFloat(rules.A5FloorCNY)+" 且未触 HARD_CAP ¥"+fmtFloat(rules.A5HardCapCNY)+"（占比 "+fmtFloat(offShare)+"）。"))
		return a
	}
	a.Hit = true
	a.Severity = "P2"
	if hitHardCap {
		a.Severity = "P1" // 触硬顶升 P1
		thrUSD = hardcapUSD
	}
	a.ActualValue = offCost // 统一以 USD 为单位（同阈值）
	a.ThresholdValue = thrUSD
	a.BaselineValue = floorUSD
	a.BaselineWindow = "max(同岗位P99,¥100)；阶段一降解链→仅floor"
	a.MatchedCount = off
	a.ImpactCostUSD = offCost
	a.ExcessCostUSD = offCost - thrUSD
	if a.ExcessCostUSD < 0 {
		a.ExcessCostUSD = 0
	}
	a.Dimensions["off_share"] = fmtFloat(offShare)
	a.Dimensions["off_cost_cny"] = fmtFloat(offCNY)
	a.Dimensions["floor_cny"] = fmtFloat(rules.A5FloorCNY)
	a.Dimensions["hard_cap_cny"] = fmtFloat(rules.A5HardCapCNY)
	a.Dimensions["peer_basis_level"] = peerLevel
	a.Dimensions["exempt_filing"] = "none(阶段一无备案表)"
	a.Evidence = append(a.Evidence,
		evi("total_calls", total),
		evi("off_calls", off),
		evs("off_share", fmtFloat(offShare)),
		evs("off_cost_usd", fmtFloat(offCost)),
		evs("off_cost_cny", fmtFloat(offCNY)),
		evs("threshold_usd", fmtFloat(thrUSD)),
		evs("hard_cap_cny", fmtFloat(rules.A5HardCapCNY)),
		evs("impact_cost_usd", fmtFloat(offCost)),
	)
	a.Note = "阶段一未接入备案治理表，按纯时间窗口（00:00–07:00/周末）触发；阶段二补备案豁免（带 until 过期，防永久豁免）。同岗位P99 与岗位降解链(job_title→category→family→dept→global→仅floor)阶段一未接入，回退仅floor。"
	return a
}

// ---- helpers ----

func fmtFloat(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e12 {
		return itoa(int(v))
	}
	return strconvFormatFloat(v)
}

func strconvFormatFloat(v float64) string {
	if v == 0 {
		return "0"
	}
	return formatFloat(v, 2)
}

func fmtTime(minute int64) string {
	return time.Unix(minute*60, 0).Format("2006-01-02 15:04")
}
