package service

import (
	"fmt"

	"github.com/example/llm-gateway/internal/observability/report/v2"
)

// 本文件实现 docs/token-usage-analysis-report-implementation.md §4.2 的 3.2 挤牙膏判定。
// 纯数值、不读正文：消费 SessionFact 的 PromptSeq / InputCostSeq / CacheCostSeq，P1 无 task_kind → verdict 固定 suspected。

// ToothpasteParams 3.2 挤牙膏阈值配置。
type ToothpasteParams struct {
	// ShortInputThreshold 短输入阈值：单轮 prompt < 此值视作「挤牙膏式短问」。
	ShortInputThreshold int64 // 30
	// ShortInputRatio 短问轮占比：ShortInputRatio(ShortInputThreshold) >= 此值才命中。
	ShortInputRatio float64 // 0.5
	// MinTurns 会话语义要求：turns >= 此值才考虑（否则视为正常短对话）。
	MinTurns int // 8
}

// DefaultToothpasteParams 返回默认阈值（口径对齐规范 §3.2）。
func DefaultToothpasteParams() ToothpasteParams {
	return ToothpasteParams{ShortInputThreshold: 30, ShortInputRatio: 0.5, MinTurns: 8}
}

// EvaluateToothpaste 对一批会话判定 3.2 挤牙膏，聚合为一个 UsageIssue。
//   - 命中：turns >= MinTurns 且 ShortInputRatio(ShortInputThreshold) >= ShortInputRatio。
//   - 排除：turns == 1 直接结束的正常短问；batch 簇（非交互）跳过。
//   - 归因：除首轮外的额外轮次 (InputCost + CacheCost)。
//   - verdict：命中固定 suspected —— P1 无 task_kind / 无意图标签，无法确认「同一件事」，故不 down 到 confirmed。
//     为 CSA 语义升级预留：命中细节写入 Evidence，供后续合成确认。
func EvaluateToothpaste(sessions []SessionFact, params ToothpasteParams) report.UsageIssue {
	issue := report.UsageIssue{
		Type:          report.IssueTypeToothpaste,
		Verdict:       report.VerdictNotHit,
		SuggestionKey: "",
	}

	hitSessions := 0
	assessedSessions := 0
	attributed := 0.0
	var excluded []string
	attrBySession := map[string]float64{} // 语义 refute 时按会话精确扣费（不进读者渲染）

	for _, s := range sessions {
		if len(s.PromptSeq) == 0 {
			continue
		}
		if s.IsBatch() {
			excluded = append(excluded, "batch_session:"+s.SessionID)
			continue
		}
		assessedSessions++
		turns := len(s.PromptSeq)
		if turns == 1 {
			excluded = append(excluded, fmt.Sprintf("single_turn:%s", s.SessionID))
			continue
		}
		if turns < params.MinTurns {
			continue
		}
		if s.ShortInputRatio(params.ShortInputThreshold) < params.ShortInputRatio {
			continue
		}

		hitSessions++
		// 归因 = 除首轮外的额外轮次 input+cache 费。
		var sessAttr float64
		for i := 1; i < turns; i++ {
			if i < len(s.InputCostSeq) {
				sessAttr += s.InputCostSeq[i]
			}
			if i < len(s.CacheCostSeq) {
				sessAttr += s.CacheCostSeq[i]
			}
		}
		attributed += sessAttr
		attrBySession[s.SessionID] = sessAttr
	}

	issue.SessionCount = hitSessions
	if assessedSessions > 0 {
		issue.SessionShare = float64(hitSessions) / float64(assessedSessions)
	}
	issue.AttributedCostUSD = attributed
	issue.AffectedCostUSD = attributed
	issue.Excluded = excluded
	issue.Evidence = map[string]any{
		"short_input_threshold": params.ShortInputThreshold,
		"short_input_ratio":     params.ShortInputRatio,
		"min_turns":             params.MinTurns,
		"hit_sessions":          hitSessions,
		"assessed_sessions":     assessedSessions,
		"_attr_by_session":      attrBySession, // 内部键：语义 refute 扣费用用，渲染层不读
		"reason_suspected":      "P1 无 task_kind / 无意图标签，无法确认同一件事，判定封顶 suspected；CSA 可据 Evidence 合成确认",
	}

	if hitSessions > 0 {
		issue.Verdict = report.VerdictSuspected
		issue.Confidence = 0.5
	} else {
		issue.Verdict = report.VerdictNotHit
		issue.Confidence = 0
	}
	return issue
}
