package service

import (
	"context"
	"fmt"

	"github.com/example/llm-gateway/internal/observability/report"
)

// 本文件实现 docs/token-usage-analysis-report-implementation.md §4.5 的 3.5 杀鸡用牛刀判定。
// 纯数值、不读正文：用同岗分位 + turns==1 作「简单任务」代理；「走高阶」与「轻量替代溢价」由
// ModelTierLookupService 的 GetModelTier / GetLightestAlternative 解析。P1 无 scene → verdict 固定 suspected。

// OverModelBaselines 3.5 读取的同岗位分位（来源 baseline_quantiles，scope=position × metric=prompt/completion）。
type OverModelBaselines struct {
	// PositionPromptP25 同岗位 prompt P25（输入偏短基准）。
	PositionPromptP25 map[string]float64
	// PositionCompletionP50 同岗位 completion P50（输出不长基准）。
	PositionCompletionP50 map[string]float64
}

// OverModelRow 是杀鸡用牛刀判定最小输入行（由调用方从 call_logs + users 归因投射，且只投 turns==1 的会话）。
type OverModelRow struct {
	RequestID        string
	UserID           string
	SessionID        string
	Position         string // 岗位归因，供同岗 P25/P50。
	Model            string
	SessionTurns     int // 仅投 turns==1 的会话。
	PromptTokens     int64
	CachedTokens     int64
	CompletionTokens int64
	// ActualCostUSD 该请求实际费用（影响面）；溢价另算。
	ActualCostUSD float64
}

// OverModelResolver 抽象「档位/轻量替代」查询，便于测试打桩；*ModelTierLookupService 天然满足该接口。
type OverModelResolver interface {
	GetModelTier(ctx context.Context, modelName string) (ModelTierInfo, error)
	GetLightestAlternative(ctx context.Context, groupID uint, excludeModelName string) (*LightestAlternative, error)
}

// OverModelParams 3.5 杀鸡用牛刀阈值配置。
type OverModelParams struct {
	// HighTiers 视为「高阶档位」的集合（默认仅 high）。
	HighTiers map[TierLevel]bool
}

// DefaultOverModelParams 返回默认配置（仅 high 为高阶档位）。
func DefaultOverModelParams() OverModelParams {
	return OverModelParams{HighTiers: map[TierLevel]bool{TierHigh: true}}
}

// EvaluateOverModel 对一批单轮会话请求判定 3.5 杀鸡用牛刀，聚合为一个 UsageIssue。
//   - 命中候选：turns==1 且 输入 < 同岗P25 且 输出 < 同岗P50 且 走高阶档位(GetModelTier)。
//   - 溢价 = GetLightestAlternative 差额：按「同 token 结构」用轻量模型单价重算：
//     (实际InputPrice - 轻量InputPrice) × 未缓存输入/1M + (实际OutputPrice - 轻量OutputPrice) × 输出/1M。
//     （铁律：total_tokens = prompt + completion + reasoning，不含 cached，故 input 用 prompt-cached。）
//   - 无轻量替代（同组内无更低档）→ verdict=not_computed（无法量化溢价）。
//   - verdict：命中可算溢价的候选 → suspected（P1 无 scene，无法确认是否「简单任务」）；无候选 → not_hit。
func EvaluateOverModel(ctx context.Context, rows []OverModelRow, resolver OverModelResolver, baselines OverModelBaselines, params OverModelParams) report.UsageIssue {
	issue := report.UsageIssue{
		Type:    report.IssueTypeOverModel,
		Verdict: report.VerdictNotHit,
	}

	total := len(rows)
	hitCount := 0
	notComputed := 0
	attributed := 0.0 // Σ 溢价
	affected := 0.0   // Σ 命中候选实际费用
	var excluded []string
	highTierRows := 0
	var hitReqIDs []string                // 命中候选 RequestID，供语义层读正文复核（只存 ID，不落正文）
	attrByRequest := map[string]float64{} // 语义 refute 扣费用（内部键，渲染层不读）

	for _, r := range rows {
		// 简单任务代理：turns==1 且 输入<同岗P25 且 输出<同岗P50。
		if r.SessionTurns != 1 {
			excluded = append(excluded, fmt.Sprintf("not_single_turn:%s#%s", r.UserID, r.RequestID))
			continue
		}
		pp25, pp25ok := baselines.PositionPromptP25[r.Position]
		pc50, pc50ok := baselines.PositionCompletionP50[r.Position]
		if !pp25ok || !pc50ok {
			excluded = append(excluded, fmt.Sprintf("position_baseline_missing:%s", r.Position))
			continue
		}
		simple := float64(r.PromptTokens) < pp25 && float64(r.CompletionTokens) < pc50
		if !simple {
			continue
		}

		// 走高阶档位。
		info, err := resolver.GetModelTier(ctx, r.Model)
		if err != nil {
			excluded = append(excluded, fmt.Sprintf("tier_lookup_error:%s", r.Model))
			continue
		}
		if !params.HighTiers[info.Tier] {
			continue
		}
		highTierRows++

		// 轻量替代。
		alt, err := resolver.GetLightestAlternative(ctx, info.GroupID, info.ModelName)
		if err != nil {
			excluded = append(excluded, fmt.Sprintf("light_alt_error:%s", r.Model))
			continue
		}
		if alt == nil || alt.Tier == TierUnknown {
			excluded = append(excluded, fmt.Sprintf("no_light_alt:%s", r.Model))
			notComputed++
			continue
		}

		// 溢价 = 同 token 结构下用轻量模型重算的差额。
		inputTokens := r.PromptTokens - r.CachedTokens
		if inputTokens < 0 {
			inputTokens = 0
		}
		premium := (info.InputPrice-alt.InputPrice)*float64(inputTokens)/1e6 +
			(info.OutputPrice-alt.OutputPrice)*float64(r.CompletionTokens)/1e6
		if premium < 0 {
			premium = 0
		}

		hitCount++
		attributed += premium
		affected += r.ActualCostUSD
		hitReqIDs = append(hitReqIDs, r.RequestID)
		attrByRequest[r.RequestID] = premium
	}

	issue.RequestCount = hitCount
	if total > 0 {
		issue.RequestShare = float64(hitCount) / float64(total)
	}
	issue.AttributedCostUSD = attributed
	issue.AffectedCostUSD = affected
	issue.Excluded = excluded
	issue.Evidence = map[string]any{
		"high_tier_rows":   highTierRows,
		"not_computed":     notComputed,
		"hit_requests":     hitCount,
		"total_requests":   total,
		"hit_request_ids":  hitReqIDs,     // 语义复核用（只存 ID）
		"_attr_by_request": attrByRequest, // 内部键：语义 refute 扣费用，渲染层不读
		"reason_suspected": "P1 无 scene 场景标签，无法确认是否「简单任务」，判定封顶 suspected；CSA 可据 Evidence 合成确认",
	}

	switch {
	case hitCount > 0:
		issue.Verdict = report.VerdictSuspected
		issue.Confidence = 0.5
	case notComputed > 0:
		issue.Verdict = report.VerdictNotComputed
		issue.Confidence = 0
	default:
		issue.Verdict = report.VerdictNotHit
		issue.Confidence = 0
	}
	return issue
}
