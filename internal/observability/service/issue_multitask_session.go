package service

import (
	"github.com/example/llm-gateway/internal/observability/report"
)

// 本文件实现 docs/token-usage-analysis-report-implementation.md §4.3 的 3.3 多任务不切会话判定。
// 纯数值、不读正文：消费 SessionFact 的 PromptSeq / CacheCostSeq / InputCostSeq / TaskKinds / ResetFlags。

// MultiTaskParams 3.3 多任务不切会话阈值配置。
type MultiTaskParams struct {
	// LongTokens 超长绝对阈值：末轮 prompt >= 此值即「超长」（未提供该用户 P90 时的兜底）。
	LongTokens int64 // 300_000
	// CacheHitMin 命中不差下限：CacheHitRate() >= 此值。
	CacheHitMin float64 // 0.40
	// SnowballJumpTokens 疑似切换点检测：相邻轮 prompt 增量 >= 此值视作一次「滚雪球突增」。
	SnowballJumpTokens int64 // 100_000
}

// DefaultMultiTaskParams 返回默认阈值（口径对齐规范 §3.3）。
func DefaultMultiTaskParams() MultiTaskParams {
	return MultiTaskParams{LongTokens: 300_000, CacheHitMin: 0.40, SnowballJumpTokens: 100_000}
}

// MultiTaskBaselines 3.3 读的用户级/个性化分位（未物化时为空 map，回退到 LongTokens）。
type MultiTaskBaselines struct {
	// UserPromptP90 每用户末轮 prompt 的 P90（来源 baseline_quantiles 或期内现算）；
	// 缺失 → 回退到 MultiTaskParams.LongTokens。
	UserPromptP90 map[string]float64
}

// EvaluateMultiTaskSession 对一批会话判定 3.3 多任务不切会话，聚合为一个 UsageIssue。
//   - 命中：超长(末轮 prompt >= 该用户P90 或 LongTokens) AND CacheHitRate() >= CacheHitMin
//     AND !HasReset() AND TaskSwitches() > 0。
//   - 近似态(P1 无 task_kind，TaskSwitches()==-1)：「任务在切」用数值代理替代 ——
//     firstSnowballJumpIdx>=1（会话内出现输入滚雪球突增，疑似新任务历史叠入前缀）。
//     满足 超长+命中好+无reset+有突增 → 仍命中 suspected，交语义层读正文确认 task_switch。
//   - 归因：从首次疑似切换点(SnowballRatio 突增)到会话结尾的 (InputCost + CacheCost)。
//   - verdict：命中固定 suspected —— 无 reset_flag 语义标签与任务意图证明，无法确认是否「该切没切」；
//     为 CSA 语义升级预留，命中细节写入 Evidence。
func EvaluateMultiTaskSession(sessions []SessionFact, params MultiTaskParams, baselines MultiTaskBaselines) report.UsageIssue {
	issue := report.UsageIssue{
		Type:    report.IssueTypeMultiTaskSession,
		Verdict: report.VerdictNotHit,
	}

	hitSessions := 0
	assessedSessions := 0
	attributed := 0.0
	var excluded []string
	var hitIDs []string
	attrBySession := map[string]float64{} // 语义 refute 时按会话精确扣费（不进读者渲染）

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

		if !multiTaskLong(s, params, baselines) {
			continue
		}
		if s.CacheHitRate() < params.CacheHitMin {
			continue
		}
		if s.HasReset() {
			continue
		}

		// 「任务在切」证据：P2 有 task_kind 用 TaskSwitches()>0；P1 近似态用滚雪球突增代理。
		switches := s.TaskSwitches()
		if switches == 0 {
			continue // 明确同任务（仅 P2 可达）
		}
		if switches < 0 {
			// P1：无 task_kind → 需要会话内出现输入突增才疑似「切了没开新会话」。
			if firstSnowballJumpIdx(s, params.SnowballJumpTokens) < 1 {
				continue
			}
		}

		hitSessions++
		hitIDs = append(hitIDs, s.SessionID)
		// 归因 = 从首次疑似切换点(SnowballRatio 突增)到会话结尾的 input+cache 费。
		start := firstSnowballJumpIdx(s, params.SnowballJumpTokens)
		if start < 1 {
			start = 1
		}
		var sessAttr float64
		for i := start; i < turns; i++ {
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
		"long_tokens":          params.LongTokens,
		"cache_hit_min":        params.CacheHitMin,
		"snowball_jump_tokens": params.SnowballJumpTokens,
		"hit_sessions":         hitSessions,
		"hit_session_ids":      hitIDs,        // 与元数据判定同口径的命中会话，供语义层逐会话复核
		"_attr_by_session":     attrBySession, // 内部键：语义 refute 扣费用用，渲染层不读
		"assessed_sessions":    assessedSessions,
		"reason_suspected":     "P1 无 reset_flag / 任务意图语义标签，无法确认是否该切没切会话，判定封顶 suspected；CSA 可据 Evidence 合成确认",
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

// multiTaskLong 判断会话末轮是否「超长」：末轮 prompt >= LongTokens，或 >= 该用户 P90（若提供）。
func multiTaskLong(s SessionFact, params MultiTaskParams, baselines MultiTaskBaselines) bool {
	last := s.PromptSeq[len(s.PromptSeq)-1]
	if last >= params.LongTokens {
		return true
	}
	if baselines.UserPromptP90 != nil {
		if p90, ok := baselines.UserPromptP90[s.UserID]; ok && float64(last) >= p90 {
			return true
		}
	}
	return false
}

// firstSnowballJumpIdx 返回首个「滚雪球突增」下标：相邻轮 prompt 增量 >= minJump 的首个 i（i>=1）。
// 未找到返回 -1。
func firstSnowballJumpIdx(s SessionFact, minJump int64) int {
	if len(s.PromptSeq) < 2 || minJump <= 0 {
		return -1
	}
	for i := 1; i < len(s.PromptSeq); i++ {
		if s.PromptSeq[i]-s.PromptSeq[i-1] >= minJump {
			return i
		}
	}
	return -1
}
