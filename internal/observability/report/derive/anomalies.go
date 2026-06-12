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
func Anomalies(calls []CallEvent, rules report.RuleSet, modelBudget map[string]int64) []report.AnomalyHit {
	return []report.AnomalyHit{
		A1(calls, rules),
		A2(calls, rules),
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

func bucket(calls []CallEvent) []ev {
	out := make([]ev, 0, len(calls))
	for _, c := range calls {
		out = append(out, ev{c: c, minute: c.TS.Unix() / 60})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].minute < out[j].minute })
	return out
}

func countIn(b []ev, s, e int64) int {
	n := 0
	for _, x := range b {
		if x.minute >= s && x.minute < e {
			n++
		}
	}
	return n
}

func sumCostIn(b []ev, s, e int64) float64 {
	var sum float64
	for _, x := range b {
		if x.minute >= s && x.minute < e {
			sum += x.c.EstCostUSD
		}
	}
	return sum
}

func within(b []ev, s, e int64) []ev {
	out := make([]ev, 0)
	for _, x := range b {
		if x.minute >= s && x.minute < e {
			out = append(out, x)
		}
	}
	return out
}

func medianIntervalSec(b []ev) float64 {
	if len(b) < 2 {
		return math.Inf(1)
	}
	ivs := make([]float64, 0, len(b)-1)
	for i := 1; i < len(b); i++ {
		ivs = append(ivs, float64(b[i].minute-b[i-1].minute)*60)
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
func A1(calls []CallEvent, rules report.RuleSet) report.AnomalyHit {
	a := newHit("A1", "v1", "程序失控", "P1", "10min/连续15min")
	if len(calls) < 2 {
		a.Hit = false
		a.Evidence = append(a.Evidence, evs("basis", "调用样本不足，无法判定程序失控。"))
		return a
	}
	b := bucket(calls)
	// 个人 P99_10min：按 1 分钟粒度统计每个 10 分钟窗口调用数，取 P99
	w := int64(rules.A1WindowMinutes)
	byWindow := map[int64]int{}
	for _, x := range b {
		byWindow[x.minute] = countIn(b, x.minute, x.minute+w)
	}
	cnts := make([]float64, 0, len(byWindow))
	for _, n := range byWindow {
		cnts = append(cnts, float64(n))
	}
	sort.Float64s(cnts)
	p99 := Percentile(cnts, 99)
	thr := math.Max(float64(rules.A1MinCallsAbsolute), math.Ceil(p99))

	minB, maxB := maxBucket(b)
	bestStart, bestEnd := int64(-1), int64(-1)
	bestCount := 0
	bestInterval := 0.0
	bestCost := 0.0
	streak := 0
	for s := minB; s <= maxB; s++ {
		e := s + w
		n := countIn(b, s, e)
		if n < int(thr) {
			streak = 0
			continue
		}
		streak++
		iv := medianIntervalSec(within(b, s, e))
		// 1 分钟粒度滑动窗口：连续 hot 起点数即持续时间（分钟），需 ≥ A1SustainMinutes（勿除以窗口分钟数）。
		if int64(streak) >= int64(rules.A1SustainMinutes) && iv < rules.A1MaxMedianIntervalS {
			if n > bestCount {
				bestStart, bestEnd = s, e
				bestCount = n
				bestInterval = iv
				bestCost = sumCostIn(b, s, e)
			}
		}
	}
	if bestStart < 0 {
		a.Hit = false
		a.Evidence = append(a.Evidence,
			evs("basis", "未发现满足阈值的高调用窗口（每分钟>max(60,P99="+fmtFloat(math.Ceil(p99))+")，且中位间隔<"+fmtFloat(rules.A1MaxMedianIntervalS)+"s，连续≥"+itoa(rules.A1SustainMinutes)+"min）。"))
		return a
	}
	a.Hit = true
	a.ActualValue = float64(bestCount)
	a.ThresholdValue = thr
	a.BaselineValue = p99
	a.BaselineWindow = "P99_10min"
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
func A2(calls []CallEvent, rules report.RuleSet) report.AnomalyHit {
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

	// 基线：单个 15 分钟窗口成本的中位数
	byMin := map[int64][]ev{}
	for _, x := range b {
		byMin[x.minute] = append(byMin[x.minute], x)
	}
	costVals := make([]float64, 0, len(byMin))
	for _, arr := range byMin {
		costVals = append(costVals, sumCostIn(arr, arr[0].minute, arr[0].minute+w))
	}
	sort.Float64s(costVals)
	med := Median(costVals)
	absMinUSD := rules.A2AbsMinUSD
	baseThr := math.Max(med*rules.A2BaselineMedianMultiple, absMinUSD)

	minB, maxB := maxBucket(b)
	bestCost := 0.0
	bestStart, bestEnd := int64(-1), int64(-1)
	for s := minB; s <= maxB; s++ {
		e := s + w
		c := sumCostIn(b, s, e)
		if c > bestCost {
			bestCost = c
			bestStart, bestEnd = s, e
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
	a.BaselineWindow = "个人28天同窗中位"
	a.MatchedCount = countIn(b, bestStart, bestEnd)
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
	a := newHit("A3", "v1", "重试循环", "P2", itoa(rules.A3WindowMinutes)+"min段")
	if len(calls) < 2 {
		a.Hit = false
		a.Evidence = append(a.Evidence, evs("basis", "调用样本不足，无法判定重试循环。"))
		return a
	}
	type seg struct{ keyID, model string }
	groups := map[seg][]CallEvent{}
	for _, c := range calls {
		k := seg{keyID: c.KeyID, model: c.Model}
		groups[k] = append(groups[k], c)
	}
	best := report.AnomalyHit{}
	found := false
	bestFailed := 0
	for k, arr := range groups {
		req := len(arr)
		if req < rules.A3MinSegmentReq {
			continue
		}
		failCnt := 0
		statusCnt := map[int]int{}
		for _, c := range arr {
			if c.Status >= 400 {
				failCnt++
				statusCnt[c.Status]++
			}
		}
		if failCnt < rules.A3MinFailed {
			continue
		}
		failRate := float64(failCnt) / float64(req)
		if failRate < rules.A3FailRateThresh {
			continue
		}
		domStatus, domShare := dominantStatus(statusCnt, failCnt)
		if domShare < rules.A3DominantFailShare {
			continue
		}
		if failCnt > bestFailed {
			best = report.AnomalyHit{
				Rule: "A3", RuleVersion: "v1", Name: "重试循环", Severity: "P2",
				Window: itoa(rules.A3WindowMinutes) + "min段", Hit: true,
				ActualValue: float64(failRate), ThresholdValue: rules.A3FailRateThresh,
				MatchedCount: failCnt, Dimensions: map[string]string{}, Evidence: []report.Evidence{},
			}
			best.Dimensions["key_id"] = k.keyID
			best.Dimensions["model"] = k.model
			best.Dimensions["segment"] = k.keyID + "|" + k.model
			best.Dimensions["fail_rate"] = fmtFloat(failRate)
			best.Dimensions["dominant_status"] = itoa(domStatus)
			best.Dimensions["dominant_fail_share"] = fmtFloat(domShare)
			// 区分上游 vs 客户端
			if domStatus >= 500 {
				best.Dimensions["kind"] = "上游故障(5xx)"
			} else {
				best.Dimensions["kind"] = "客户端僵死(4xx)"
			}
			best.BaselineValue = 0
			best.BaselineWindow = "无（规则型）"
			for _, c := range arr {
				if c.Status >= 400 {
					best.ImpactCostUSD += c.EstCostUSD
				}
			}
			best.ExcessCostUSD = best.ImpactCostUSD
			best.Evidence = append(best.Evidence,
				evi("segment_req", req),
				evi("failed", failCnt),
				evs("fail_rate", fmtFloat(failRate)),
				evs("dominant_status", itoa(domStatus)+" 占比 "+fmtFloat(domShare)),
				evs("impact_cost_usd", fmtFloat(best.ImpactCostUSD)),
			)
			bestFailed = failCnt
			found = true
		}
	}
	if !found {
		a.Hit = false
		a.Evidence = append(a.Evidence, evs("basis", "未发现段内 req>=20/fail_rate>=30%/单一status>=80%/failed>=6 的重复失败。"))
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
		a.Hit = false
		a.Evidence = append(a.Evidence, evs("basis", "无调用数据，无法判定超长上下文。"))
		return a
	}
	b := bucket(calls)
	minB, maxB := maxBucket(b)
	w := int64(rules.A4WindowMinutes)
	best := report.AnomalyHit{}
	found := false
	bestNear := 0
	for s := minB; s <= maxB; s++ {
		e := s + w
		arr := within(b, s, e)
		near := 0
		var cost float64
		var maxPrompt int
		var maxModel string
		for _, x := range arr {
			c := x.c
			budg := int64(rules.A4MinPromptAbsolute)
			if bd, ok := budget[c.Model]; ok && bd > 0 {
				budg = bd
			}
			// prompt_total（含 cached 都算作输入）——近上限才计入
			if float64(int64(c.PromptTokens)) >= float64(budg)*rules.A4NearLimitRatio {
				near++
				cost += c.EstCostUSD
			}
			if c.PromptTokens > maxPrompt {
				maxPrompt = c.PromptTokens
				maxModel = c.Model
			}
		}
		if near < rules.A4MinNearLimit || near <= bestNear {
			continue
		}
		best = report.AnomalyHit{
			Rule: "A4", RuleVersion: "v1", Name: "超长上下文", Severity: "P2",
			Window: itoa(rules.A4WindowMinutes) + "min", Hit: true,
			ActualValue: float64(near), ThresholdValue: float64(rules.A4MinNearLimit),
			MatchedCount: near, Dimensions: map[string]string{}, Evidence: []report.Evidence{},
		}
		best.Dimensions["model"] = maxModel
		best.BaselineValue = float64(rules.A4MinPromptAbsolute)
		best.BaselineWindow = "保守下限(未录上下文)"
		if bd, ok := budget[maxModel]; ok && bd > 0 {
			best.BaselineValue = float64(bd)
			best.BaselineWindow = maxModel + " 有效输入预算"
		}
		best.ImpactCostUSD = cost
		best.ExcessCostUSD = cost
		best.Evidence = append(best.Evidence,
			evi("near_limit_req_in_window", near),
			evi("max_prompt", maxPrompt),
			evs("model", maxModel),
			evs("budget", fmtFloat(best.BaselineValue)),
			evs("ratio", fmtFloat(float64(maxPrompt)/best.BaselineValue)),
			evs("impact_cost_usd", fmtFloat(cost)),
		)
		best.Note = "注：分支2(IO比同场景P99)依赖 scene 埋点，未埋点前默认禁用/灰度（阶段一不启用）。"
		bestNear = near
		found = true
	}
	if !found {
		a.Hit = false
		a.Evidence = append(a.Evidence, evs("basis", "未发现窗口内近上下文上限请求（>80% 有效预算）≥"+itoa(rules.A4MinNearLimit)+" 条。"))
		return a
	}
	return best
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
