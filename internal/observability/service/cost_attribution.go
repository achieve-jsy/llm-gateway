package service

import (
	"fmt"
	"sort"
)

// 本文件实现 docs/token-usage-analysis-report-implementation.md §5 的费用桶归因清算（防重复计算）。
// 这是四象限 Y 轴正确性的关键：同一请求/会话可能被 2+ 规则命中，必须「主因择一」，避免 Σ归因 > 总成本。

// 成本载体粒度。
const (
	EntityKindRequest = "request"
	EntityKindSession = "session"
)

// costEpsilon 费用归因清算允许的浮点误差（1 分钱，与 report 包保持一致）。
const costEpsilonUSD = 0.01

// CostClaim 是某条规则对某个「成本载体」（请求或会话）的一条费用主张。
// 同一载体被多条规则命中时，每条规则各产生一条 claim，它们的 BucketCost 指向该载体同一份费用（去重前）。
type CostClaim struct {
	// IssueType 五种情况之一（report.IssueType*）。
	IssueType string
	// EntityKind request|session。
	EntityKind string
	// EntityKey 载体的唯一标识（request_id 或 session_id）。两个 claim 同 (kind,key) 即视为落在同一载体上。
	EntityKey string
	// BucketCost 该规则在该载体上主张的归因费用（去重前，也是「主因择一」的排序依据）。
	BucketCost float64
	// AffectedCost 该载体命中该规则的整笔费用（仅展示用途；>= BucketCost）。
	AffectedCost float64
}

// IssueAttribution 是单个情况在归因清算后的三项费用。
type IssueAttribution struct {
	IssueType string
	// AttributedCostUSD 该情况作为「主因」的净额（去重后实际计入合计）。
	AttributedCostUSD float64
	// OverlapCostUSD 该情况作为「非主因」被扣减的重叠费用。
	OverlapCostUSD float64
	// AffectedCostUSD 命中该情况的请求/会话整笔费用（仅展示；>= AttributedCostUSD）。
	AffectedCostUSD float64
}

// CostAttributionResult 是归因清算结果，含对账三量。
type CostAttributionResult struct {
	// TotalCost 输入的总成本（调用方口径，报告范围的 est_cost_usd 合计）。
	TotalCost float64
	// AttributedTotal Σ各类 attributed_cost_usd（去重后实际计入合计的净额）。
	AttributedTotal float64
	// OverlapTotal 被扣减的重复部分汇总。
	OverlapTotal float64
	// Residual 未归因残差 = TotalCost - AttributedTotal - OverlapTotal。
	Residual float64
	// PerIssue 每个情况的归因净额/重叠/影响面。
	PerIssue []IssueAttribution
}

// ReconcileCostAttribution 对一批费用主张做「主因择一」归因清算。
//
//   - 聚合键 = (EntityKind, EntityKey)：同一载体上的多条命中视为「重叠」。
//   - 每个载体选 BucketCost 最大者为主因，计入其 attributed；其余记 overlap（不重复计入合计）。
//   - pickPrimary 规则（可配置语义，默认金额最大）：同一载体上按 BucketCost 降序，金额相同则按 IssueType 字典序保证确定性。
//   - 对账等式：TotalCost = AttributedTotal + OverlapTotal + Residual；Residual 即未被任何情况认领的剩余成本（健康请求等）。
//   - 铁律：本函数只重排费用桶，不涉及 cost_per_1m / token 占比分母；total 为 est_cost_usd 口径，与 report.Validate() 一致。
//
// 违反「Σ归因 ≤ 总成本」时返回 error（对齐 report.Validate() 归因防重复校验）。
func ReconcileCostAttribution(claims []CostClaim, totalCost float64) (CostAttributionResult, error) {
	res := CostAttributionResult{TotalCost: totalCost}

	// 校验 claim 合法性。
	for i, c := range claims {
		if c.IssueType == "" || c.EntityKind == "" || c.EntityKey == "" {
			return res, fmt.Errorf("claims[%d]: issue_type/entity_kind/entity_key must not be empty", i)
		}
		if c.BucketCost < 0 {
			return res, fmt.Errorf("claims[%d]: bucket_cost %v < 0", i, c.BucketCost)
		}
		if c.AffectedCost < 0 {
			return res, fmt.Errorf("claims[%d]: affected_cost %v < 0", i, c.AffectedCost)
		}
		if c.BucketCost > c.AffectedCost+costEpsilonUSD {
			return res, fmt.Errorf("claims[%d]: bucket_cost %v exceeds affected_cost %v", i, c.BucketCost, c.AffectedCost)
		}
	}
	if totalCost < 0 {
		return res, fmt.Errorf("total_cost %v < 0", totalCost)
	}

	// 按 (kind,key) 分组。
	groups := map[string][]CostClaim{}
	for _, c := range claims {
		gk := c.EntityKind + "\x00" + c.EntityKey
		groups[gk] = append(groups[gk], c)
	}

	// 每组择一主因。
	type perIssue struct {
		attr, over, aff float64
	}
	agg := map[string]*perIssue{}
	ensure := func(it string) *perIssue {
		if agg[it] == nil {
			agg[it] = &perIssue{}
		}
		return agg[it]
	}

	res.TotalCost = totalCost
	for _, groupClaims := range groups {
		if len(groupClaims) == 0 {
			continue
		}
		primary := pickPrimary(groupClaims)
		for _, c := range groupClaims {
			pi := ensure(c.IssueType)
			pi.aff += c.AffectedCost // 影响面：命中该情况即计入整笔，不论主因与否。
			if c == primary {
				pi.attr += c.BucketCost
				res.AttributedTotal += c.BucketCost
			} else {
				pi.over += c.BucketCost
				res.OverlapTotal += c.BucketCost
			}
		}
	}

	// 汇总 per-issue（字典序输出，保证确定性）。
	issueTypes := make([]string, 0, len(agg))
	for it := range agg {
		issueTypes = append(issueTypes, it)
	}
	sort.Strings(issueTypes)
	for _, it := range issueTypes {
		pi := agg[it]
		res.PerIssue = append(res.PerIssue, IssueAttribution{
			IssueType:         it,
			AttributedCostUSD: pi.attr,
			OverlapCostUSD:    pi.over,
			AffectedCostUSD:   pi.aff,
		})
	}

	res.Residual = totalCost - res.AttributedTotal - res.OverlapTotal

	// 对账校验：Σ归因 ≤ 总成本（对齐 report.Validate()）。
	if res.AttributedTotal > totalCost+costEpsilonUSD {
		return res, fmt.Errorf("cost attribution over-run: ΣAttributedTotal=%v > totalCost=%v", res.AttributedTotal, totalCost)
	}
	if res.Residual < -costEpsilonUSD {
		return res, fmt.Errorf("cost attribution residual %v < 0: ΣAttributed+Overlap exceed totalCost", res.Residual)
	}
	return res, nil
}

// pickPrimary 在同一个载体的一组 claim 中择一主因：BucketCost 最大者优先；
// 金额相同按 IssueType 字典序（确定性），与 docs §5 的 pickPrimary 一致。
func pickPrimary(claims []CostClaim) CostClaim {
	sorted := make([]CostClaim, len(claims))
	copy(sorted, claims)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].BucketCost != sorted[j].BucketCost {
			return sorted[i].BucketCost > sorted[j].BucketCost
		}
		return sorted[i].IssueType < sorted[j].IssueType
	})
	return sorted[0]
}
