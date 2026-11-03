package service

import (
	"strings"

	"github.com/example/llm-gateway/internal/observability/report"
)

// 本文件实现 docs/token-usage-analysis-report-implementation.md §4.4 的 3.4 输出失控判定。
// 纯数值、不读正文：基于 baseline_quantiles 的同模型/同岗位分位基线 + 物理因果量级护栏，逐请求判定。
//
// 口径（v0.6 文本 + v0.7 物理因果防线 的合并口径）：
//   - 具备 finish_reason 时走物理因果防线（v0.7）；
//   - finish_reason 缺失/tool_calls/content_filter 时，需同时达到绝对量级护栏
//     （completion>=MinRunawayTokens 且 费用>=MinRunawayCostUSD）并命中至少一个
//     v0.6「输出失控」信号（输出/输入倍率>OutRunawayRatio，或 同模型 P25 输入偏低 且 输出>P75）。

// OutputRunawayRow 是输出失控判定的最小输入行（由调用方从 call_logs + users 归因 投射；应只含 status=200 的成功请求）。
type OutputRunawayRow struct {
	RequestID string
	UserID    string
	SessionID string
	Position  string // 岗位归因（COALESCE job_category/job_family/job_title），供同岗 P25。
	Model     string

	PromptTokens     int64
	CachedTokens     int64
	CompletionTokens int64

	OutputCostUSD float64 // 该请求实际输出费（用于折算超基准输出量）。

	// v0.7 物理因果防线字段：
	FinishReason string // "stop", "length", "tool_calls", "content_filter" 等
	OutputText   string // 用于死循环检测
}

// OutputRunawayBaselines 是输出失控读取的分位基线（来源 baseline_quantiles，scope=model/position × metric=prompt/completion）。
type OutputRunawayBaselines struct {
	// ModelPromptP25 分模型 prompt P25（输入偏短基准）。
	ModelPromptP25 map[string]float64
	// ModelOutputP75 分模型 completion P75（输出失控基准）。
	ModelOutputP75 map[string]float64
	// PositionPromptP25 同岗位 prompt P25（输入偏短基准，§6.4 岗位归因）。
	PositionPromptP25 map[string]float64
}

// modelPromptP25 读取某模型的 prompt P25。
func (b OutputRunawayBaselines) modelPromptP25(model string) (float64, bool) {
	v, ok := b.ModelPromptP25[model]
	return v, ok
}

// modelOutputP75 读取某模型的 completion P75。
func (b OutputRunawayBaselines) modelOutputP75(model string) (float64, bool) {
	v, ok := b.ModelOutputP75[model]
	return v, ok
}

// positionPromptP25 读取某岗位的 prompt P25。
func (b OutputRunawayBaselines) positionPromptP25(position string) (float64, bool) {
	v, ok := b.PositionPromptP25[position]
	return v, ok
}

// OutputRunawayParams 3.4 输出失控阈值配置。
type OutputRunawayParams struct {
	// OutRunawayRatio 输出/输入倍率：completion / (prompt - cached) > 此值即命中 v0.6「输出/输入过高」信号
	//（在无 finish_reason 分支，需同时达到绝对量级护栏）。
	OutRunawayRatio float64 // 20
	// RepeatMin 同 user 重复命中次数达到此值 → 叠加「反复出现」提升 confidence。
	RepeatMin int // 2
	// HighTierModels 高阶模型集（档位 high/ultra）；命中该集合模型 → 叠加「叠高阶模型」提升 confidence，
	// 且不适用「一次成功长输出+轻量模型」排除。
	HighTierModels map[string]bool
	// BaseConfidence 单次命中基线置信度。
	BaseConfidence float64 // 0.6
	// MinRunawayTokens 无 finish_reason 数据时的绝对输出量护栏：completion 低于此值不判失控（避免误伤正常代码生成）。
	// 仅当 finish_reason 采集缺失（== ""）时生效；具备 finish_reason 时走物理因果防线。
	MinRunawayTokens int64 // 8000
	// MinRunawayCostUSD 无 finish_reason 数据时的绝对费用护栏：单次输出费用低于此值不判失控（微小浪费不入罪）。
	MinRunawayCostUSD float64 // 0.10
}

// DefaultOutputRunawayParams 返回默认阈值（口径对齐规范 §3.4）。
func DefaultOutputRunawayParams() OutputRunawayParams {
	return OutputRunawayParams{
		OutRunawayRatio:  20,
		RepeatMin:        2,
		HighTierModels:   map[string]bool{},
		BaseConfidence:   0.6,
		MinRunawayTokens: 8000,
		MinRunawayCostUSD: 0.10,
	}
}

// EvaluateOutputRunaway 对一批成功请求判定 3.4 输出失控，聚合为一个 UsageIssue。
//   - 命中（v0.7 护栏为主）：
//   - finish_reason=="length"（撞上限截断）或「stop 且含极端死循环」→ 命中；
//   - 其余情形（fR 缺失/tool_calls/content_filter）→ 需达绝对量级护栏（completion>=MinRunawayTokens
//     且 费用>=MinRunawayCostUSD）并命中 v0.6 输出失控信号（输出/输入倍率>OutRunawayRatio，
//     或 同模型 P25 输入偏低 且 输出>P75）；
//   - finish_reason=="stop" 且无死循环 → 免检（消除杨士俊型正常长代码误判）。
//   - 归因：超出同模型 P75 的输出量费用 = OutputCostUSD × (completion - modelOutP75) / completion；
//     P75 不可用时归因 0。
//   - confidence：BaseConfidence；叠加「反复出现」（同 user 命中 >= RepeatMin）或「叠高阶模型」再提升。
//   - verdict：存在有效命中即 confirmed；无命中 not_hit。
func EvaluateOutputRunaway(rows []OutputRunawayRow, baselines OutputRunawayBaselines, params OutputRunawayParams) report.UsageIssue {
	issue := report.UsageIssue{
		Type:    report.IssueTypeOutputRunaway,
		Verdict: report.VerdictNotHit,
	}

	total := len(rows)
	// 第一遍：命中检测。
	type hitRow struct {
		row        OutputRunawayRow
		modelP75   float64
		modelP75ok bool
	}
	var hits []hitRow
	for _, r := range rows {
		h, p75, p75ok := runawayHit(r, baselines, params)
		if !h {
			continue
		}
		hits = append(hits, hitRow{row: r, modelP75: p75, modelP75ok: p75ok})
	}

	// 第二遍：按 user 统计命中数（用于「一次/反复」判断）。
	hitsByUser := map[string]int{}
	for _, h := range hits {
		hitsByUser[h.row.UserID]++
	}

	var excluded []string
	counted := 0
	attributed := 0.0
	affected := 0.0
	sessionsHit := map[string]bool{}
	anyHighTier := false
	anyRepeated := false

	for _, h := range hits {
		userCount := hitsByUser[h.row.UserID]
		isHighTier := params.HighTierModels[h.row.Model]

		counted++
		if h.row.SessionID != "" {
			sessionsHit[h.row.SessionID] = true
		}
		// 影响面 = 命中请求的整笔输出费；归因 = 超基准输出量的输出费（P75 可用时）。
		affected += h.row.OutputCostUSD
		if h.modelP75ok && float64(h.row.CompletionTokens) > h.modelP75 {
			excess := float64(h.row.CompletionTokens) - h.modelP75
			attributed += h.row.OutputCostUSD * excess / float64(h.row.CompletionTokens)
		}
		if isHighTier {
			anyHighTier = true
		}
		if userCount >= params.RepeatMin {
			anyRepeated = true
		}
	}

	issue.RequestCount = int(counted)
	issue.SessionCount = len(sessionsHit)
	if total > 0 {
		issue.RequestShare = float64(counted) / float64(total)
	}
	// 会话占比：命中会话数 / 评估总会话数（按 SessionID 去重），与 前缀污染/挤牙膏/多任务 的
	// hitSessions/assessedSessions 口径一致。缺省会话数=所有行去重数（provider 每请求投射 SessionID）。
	allSessions := map[string]bool{}
	for _, r := range rows {
		if r.SessionID != "" {
			allSessions[r.SessionID] = true
		}
	}
	if len(allSessions) > 0 {
		issue.SessionShare = float64(len(sessionsHit)) / float64(len(allSessions))
	}
	issue.AttributedCostUSD = attributed
	issue.AffectedCostUSD = affected
	issue.Excluded = excluded
	issue.Confidence = params.BaseConfidence
	if anyHighTier {
		issue.Confidence += 0.15
	}
	if anyRepeated {
		issue.Confidence += 0.15
	}
	if issue.Confidence > 1 {
		issue.Confidence = 1
	}
	issue.Evidence = map[string]any{
		"out_runaway_ratio": params.OutRunawayRatio,
		"repeat_min":        params.RepeatMin,
		"hit_requests":      counted,
		"total_requests":    total,
		"high_tier_hits":    anyHighTier,
		"repeated_hits":     anyRepeated,
	}

	if counted > 0 {
		issue.Verdict = report.VerdictConfirmed
	} else {
		issue.Verdict = report.VerdictNotHit
		issue.Confidence = 0
	}
	return issue
}

// runawayHit 判断单个请求是否命中 3.4 输出失控；返回是否命中、其模型的 completion P75、P75 是否可用。
//   - v0.7 物理因果防线（具备 finish_reason 时）：
//   - finish_reason == "length"（撞系统上限被强制掐断）→ 最高置信失控信号，命中；
//   - finish_reason == "stop"（自然完结）且含极端死循环 → 命中；
//   - finish_reason == "stop"（自然完结）且无死循环 → 免检，一律不判失控（消除杨士俊型长代码误判）。
//   - finish_reason 缺失（当前 call_logs 无此列的现实）或 tool_calls/content_filter 等其他情形：
//   - 以「真实失控量级 + 费用」护栏核定：仅当 completion >= MinRunawayTokens 且输出费 >= MinRunawayCostUSD
//     才判定命中。彻底去除对全员被污染分位基线（P25≈54k/P75≈945）的依赖，
//     正常代码生成（体量/费用远低于护栏）直接免检。
func runawayHit(r OutputRunawayRow, baselines OutputRunawayBaselines, params OutputRunawayParams) (bool, float64, bool) {
	modelP75, p75ok := baselines.modelOutputP75(r.Model)
	normReason := strings.ToLower(strings.TrimSpace(r.FinishReason))

	switch normReason {
	case "length":
		// 系统级强制截断：最高置信失控信号。
		return true, modelP75, p75ok
	case "stop":
		if hasExtremeLoop(r.OutputText) {
			// 自然完结但内容退化为极端死循环 → 命中。
			return true, modelP75, p75ok
		}
		// 自然完结且内容正常 → 免检。
		return false, modelP75, p75ok
	}

	// 其余情形（finish_reason 缺失 / tool_calls / content_filter 等）。
	// 命中 = 「物理因果量级」护栏：completion>=MinRunawayTokens 且 单次输出费>=MinRunawayCostUSD；
	//        且满足至少一个 v0.6「输出失控」信号：
	//          - 输出/输入倍率 > OutRunawayRatio（如 20）——真正的「短输入、爆长输出」；
	//          - 或 输出高于同模型 P75 且 输入低于同模型 P25（分位信号，仅当基线可用）。
	//  这样既保留 v0.7 防杨士俊型误判的绝对量级门槛，又让 OutRunawayRatio 真正参与判定；
	//  避免「单看倍率/单看分位」把短输入但体量可控的正常输出误判为失控。
	effInput := r.PromptTokens - r.CachedTokens
	if effInput < 1 {
		effInput = 1
	}
	ratio := float64(r.CompletionTokens) / float64(effInput)

	if float64(r.CompletionTokens) < float64(params.MinRunawayTokens) || r.OutputCostUSD < params.MinRunawayCostUSD {
		return false, modelP75, p75ok
	}

	// 达到量级后，需有 v0.6 输出失控主信号兜底，避免仅凭量级一刀切。
	modelP25, p25ok := baselines.modelPromptP25(r.Model)
	overP75 := p75ok && float64(r.CompletionTokens) > modelP75
	underP25 := p25ok && float64(effInput) < modelP25
	ratioHigh := ratio > params.OutRunawayRatio
	if !(ratioHigh || (overP75 && underP25)) {
		return false, modelP75, p75ok
	}
	return true, modelP75, p75ok
}

// hasExtremeLoop 辅助检测输出文本是否包含极端死循环特征（连续重复多行或高频重复短语）
func hasExtremeLoop(text string) bool {
	if len(text) < 50 {
		return false
	}
	// 1. 检查重复行：同一非空行连续出现多次
	lines := strings.Split(text, "\n")
	var lastLine string
	lineRepeatCount := 0
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if len(trimmed) < 4 {
			continue
		}
		if trimmed == lastLine {
			lineRepeatCount++
			if lineRepeatCount >= 10 {
				return true
			}
		} else {
			lastLine = trimmed
			lineRepeatCount = 1
		}
	}

	// 2. 检查重复短语
	clean := strings.ReplaceAll(text, "\r", "")
	runes := []rune(clean)
	n := len(runes)
	for patLen := 8; patLen <= 40; patLen++ {
		if patLen*15 > n {
			break
		}
		startIdx := n - (patLen * 15)
		if startIdx < 0 {
			startIdx = 0
		}
		for i := startIdx; i+patLen*3 <= n; i += patLen {
			pat := string(runes[i : i+patLen])
			if strings.TrimSpace(pat) == "" {
				continue
			}
			matches := 1
			for j := i + patLen; j+patLen <= n; j += patLen {
				if string(runes[j:j+patLen]) == pat {
					matches++
					if matches >= 15 {
						return true
					}
				} else {
					break
				}
			}
		}
	}
	return false
}
