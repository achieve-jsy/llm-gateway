package service

import (
	"fmt"

	"github.com/example/llm-gateway/internal/observability/report"
)

// 本文件实现 docs/token-usage-analysis-report-implementation.md §4.1 的 3.1 前缀污染判定。
// 纯数值、不读正文：只消费 SessionFact 的 PromptSeq/CachedSeq/InputCostSeq/CacheCostSeq 与 SplitEvidence。

// PrefixPollutionParams 3.1 前缀污染阈值配置（集中、可配置、不写死）。
type PrefixPollutionParams struct {
	// LongInputTokens 超长输入阈值：单轮 prompt_tokens > 此值才算「超长」。
	LongInputTokens int64 // 500_000
	// LowHitRate 低命中率阈值：单轮 cached/prompt < 此值且超长 → 该轮具备污染特征。
	LowHitRate float64 // 0.30
	// Consecutive 连续轮次：连续 >= 此值的低命中超长轮才构成一个污染段。
	Consecutive int // 3
	// StableHitRate 排除阈值：连续超长轮中段内命中率 >= 此值 → 「超长但命中稳定」→ 转 3.3 候选，不计污染。
	StableHitRate float64 // 0.70
}

// DefaultPrefixPollutionParams 返回默认阈值（口径对齐规范 §3.1）。
func DefaultPrefixPollutionParams() PrefixPollutionParams {
	return PrefixPollutionParams{
		LongInputTokens: 500_000,
		LowHitRate:      0.30,
		Consecutive:     3,
		StableHitRate:   0.70,
	}
}

// EvaluatePrefixPollution 对一批会话判定 3.1 前缀污染，聚合为一个 UsageIssue。
//   - 污染段 = 连续 >= Consecutive 轮满足「prompt > LongInputTokens 且 cached/prompt < LowHitRate」。
//   - 段级归因 = Σ(段内 InputCost + CacheCost)。
//   - 排除：连续超长轮但段内命中率 >= StableHitRate → 记入 Excluded（转 3.3 候选），不计污染。
//   - verdict：命中即 confirmed；若命中会话的 SplitEvidence 为 weak（纯超时推断、无前缀佐证）→ 封顶 suspected。
//   - batch 簇为并发互不累积请求，不属顺序上下文复用，跳过（不判污染）。
func EvaluatePrefixPollution(sessions []SessionFact, params PrefixPollutionParams) report.UsageIssue {
	issue := report.UsageIssue{
		Type:    report.IssueTypePrefixPollution,
		Verdict: report.VerdictNotHit,
	}

	totalTurns := 0
	assessedSessions := 0
	hitTurns := 0
	hitSessions := 0
	attributed := 0.0
	var excluded []string
	hasConfirmedHit := false

	for _, s := range sessions {
		turns := len(s.PromptSeq)
		if turns == 0 {
			continue
		}
		if s.IsBatch() {
			excluded = append(excluded, "batch_session:"+s.SessionID)
			continue
		}
		assessedSessions++
		totalTurns += turns

		// 逐轮标记：超长+低命中=污染；超长+稳定高命中=排除候选；其余中性。
		polluted := make([]bool, turns)
		stableLong := make([]bool, turns)
		for i := 0; i < turns; i++ {
			if s.PromptSeq[i] <= params.LongInputTokens {
				continue
			}
			ratio := cacheHitRatio(s.CachedSeq[i], s.PromptSeq[i])
			if ratio < params.LowHitRate {
				polluted[i] = true
			} else if ratio >= params.StableHitRate {
				stableLong[i] = true
			}
		}

		// 排除：超长但命中稳定（> StableHitRate）的连续段 → 转 3.3 候选。
		for _, seg := range boolRuns(stableLong, 1) {
			cost := seqSegmentCost(s, seg)
			excluded = append(excluded, fmt.Sprintf("stable_hit_long[%d-%d]:$%.4f", seg[0], seg[1], cost))
		}

		// 污染段：连续 >= Consecutive 的低命中超长轮。
		pollutedSegments := boolRuns(polluted, params.Consecutive)
		if len(pollutedSegments) > 0 {
			for _, seg := range pollutedSegments {
				segLen := seg[1] - seg[0]
				hitTurns += segLen
				attributed += seqSegmentCost(s, seg)
				excluded = append(excluded, fmt.Sprintf("polluted[%d-%d]:%d turns", seg[0], seg[1], segLen))
			}
			hitSessions++
			if s.SplitEvidence != SplitEvidenceWeak {
				hasConfirmedHit = true
			}
		}
	}

	issue.RequestCount = hitTurns
	issue.SessionCount = hitSessions
	if totalTurns > 0 {
		issue.RequestShare = float64(hitTurns) / float64(totalTurns)
	}
	if assessedSessions > 0 {
		issue.SessionShare = float64(hitSessions) / float64(assessedSessions)
	}
	issue.AttributedCostUSD = attributed
	issue.AffectedCostUSD = attributed
	issue.Excluded = excluded
	issue.Evidence = map[string]any{
		"long_input_threshold": params.LongInputTokens,
		"low_hit_rate":         params.LowHitRate,
		"consecutive":          params.Consecutive,
		"stable_hit_rate":      params.StableHitRate,
		"polluted_turns":       hitTurns,
		"assessed_sessions":    assessedSessions,
	}

	switch {
	case hitTurns == 0:
		issue.Verdict = report.VerdictNotHit
		issue.Confidence = 0
	case hasConfirmedHit:
		issue.Verdict = report.VerdictConfirmed
		issue.Confidence = 0.9
	default:
		issue.Verdict = report.VerdictSuspected
		issue.Confidence = 0.5
	}
	return issue
}

// cacheHitRatio 计算单轮缓存命中率 cached/prompt；prompt<=0 时不构成超长，返回 1（视为未命中低命中特征）。
func cacheHitRatio(cached, prompt int64) float64 {
	if prompt <= 0 {
		return 1
	}
	return float64(cached) / float64(prompt)
}

// boolRuns 返回 bool 序列中连续 true 段 [start, end)（end 为开区间），仅保留长度 >= minLen 的段。
func boolRuns(flags []bool, minLen int) [][2]int {
	var runs [][2]int
	i, n := 0, len(flags)
	for i < n {
		if !flags[i] {
			i++
			continue
		}
		j := i
		for j < n && flags[j] {
			j++
		}
		if j-i >= minLen {
			runs = append(runs, [2]int{i, j})
		}
		i = j
	}
	return runs
}

// seqSegmentCost 计算会话一个下标段 [start, end) 的 input+cache 费用（越界下标安全跳过）。
func seqSegmentCost(s SessionFact, seg [2]int) float64 {
	var c float64
	for i := seg[0]; i < seg[1]; i++ {
		if i < len(s.InputCostSeq) {
			c += s.InputCostSeq[i]
		}
		if i < len(s.CacheCostSeq) {
			c += s.CacheCostSeq[i]
		}
	}
	return c
}
