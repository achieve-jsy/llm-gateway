package report

import (
	"sort"
)

// 本文件实现 docs §9.2/§9.3 的四象限评估（L4）。
// 纯函数：不依赖 DB/HTTP/IO，输入成员原始费用与各情况归因净额，输出 MemberPoint[] 与组织十字线。
//
// 关键口径（铁律，务必遵守）：
//   - issue_cost_ratio 只含「五种情况 Σ attributed_cost」÷ 个人 total_cost；
//     **3.6 严重异常的 unrelated_cost 绝不计入此占比**，两者在报告中永久分列（unrelated 只走 safety_anomaly 描边块）。

// IssueTypeOrder 五种情况的稳定顺序（用于 dominant_issue 确定性 tie-break）。
var IssueTypeOrder = []string{
	IssueTypePrefixPollution,
	IssueTypeToothpaste,
	IssueTypeMultiTaskSession,
	IssueTypeOutputRunaway,
	IssueTypeOverModel,
}

// QuadrantParams 四象限评估阈值/十字线配置。
type QuadrantParams struct {
	// DominantMinCostUSD 判定 dominant_issue 是否有效的费用下限：五种情况最大归因 < 此值 → dominant=normal。
	// 默认 0：任意正的归因即视为「存在主导问题」；全 0 则 normal。后续可用 baseline 校准。
	DominantMinCostUSD float64
	// XMedian 十字线 X 轴 = 成本中位倍数（固定 1.0）。
	XMedian float64
}

// DefaultQuadrantParams 返回默认四象限配置。
func DefaultQuadrantParams() QuadrantParams {
	return QuadrantParams{DominantMinCostUSD: 0, XMedian: 1.0}
}

// MemberQuadrantInput 是单个成员四象限评估的输入（原始数据，不含推导字段）。
// IssueAttributed 为该成员五种情况的归因净额（issue type → attributed_cost_usd，来自 §5 费用归因清算）。
type MemberQuadrantInput struct {
	UserID   string
	Name     string
	Position string
	DeptID   string
	DeptName string
	CostUSD  float64
	Calls    int

	// SafetyAnomaly 是否命中 3.6 严重异常（只描边，**不进 Y 轴 / 不进 issue_cost_ratio**）。
	SafetyAnomaly bool
	// SafetyDetail 保存 3.6 完整待核查证据，不参与四象限计算。
	SafetyDetail SafetyAnomaly
	// SafetyReviewTag 严重异常标记（兼容直接消费字段）。
	SafetyReviewTag string
	// SafetyHint 严重异常提示（兼容直接消费字段）。
	SafetyHint string
	// SuspectedOnly 该成员命中情况是否全部为 suspected（虚边）。
	SuspectedOnly bool

	// IssueAttributed 五种情况归因净额（issue type → attributed_cost_usd）。
	IssueAttributed map[string]float64
}

// CostMultiple 个人 total_cost ÷ 组织成员成本中位数（X 轴；避免极端值撑开）。
// median<=0 时返回 0（不可比，避免除零）。
func CostMultiple(costUSD, medianUSD float64) float64 {
	if medianUSD <= 0 {
		return 0
	}
	return costUSD / medianUSD
}

// IssueCostRatio 五种情况 Σ attributed_cost ÷ 个人 total_cost（Y 轴）。
// 与 3.6 unrelated_cost_ratio 严格分列：本函数只收五种情况的归因合计，绝不混入 unrelated_cost。
// total_cost<=0 返回 0；结果夹在 [0,1]。
func IssueCostRatio(attributedTotal, totalCost float64) float64 {
	if totalCost <= 0 {
		return 0
	}
	r := attributedTotal / totalCost
	if r < 0 {
		return 0
	}
	if r > 1 {
		return 1
	}
	return r
}

// DominantIssue 返回五种情况中 attributed_cost 最大者及其费用；若最大归因 < minCost → normal。
// 按 IssueTypeOrder 顺序取首个最大值（确定性 tie-break）。
func DominantIssue(issueAttributed map[string]float64, minCost float64) (string, float64) {
	dominant := IssueTypeNormal
	maxCost := 0.0
	for _, it := range IssueTypeOrder {
		c := issueAttributed[it]
		if c > maxCost {
			maxCost = c
			dominant = it
		}
	}
	if maxCost < minCost {
		return IssueTypeNormal, 0
	}
	return dominant, maxCost
}

// QuadrantOf 根据 cost_multiple 与 issue_cost_ratio 相对十字线判定象限。
//   - X 十字线 = xMedian（成本中位倍数，默认 1.0）；cost_multiple > xMedian → 右侧(成本高)。
//   - Y 十字线 = yMedian（组织问题占比中位数，非固定 0.5）；issue_cost_ratio > yMedian → 上侧(问题占比高)。
//   - 边界（等于十字线）归入「不高/不高」一侧（左/下），保证四个象限无重叠且覆盖全平面。
func QuadrantOf(costMultiple, issueCostRatio, yMedian, xMedian float64) string {
	costHigh := costMultiple > xMedian
	issueHigh := issueCostRatio > yMedian
	switch {
	case costHigh && issueHigh:
		return QuadrantPriorityImprove // 右上：成本高 + 问题占比高
	case !costHigh && issueHigh:
		return QuadrantCoach // 左上：成本不高但问题占比高
	case costHigh && !issueHigh:
		return QuadrantHighCostNormal // 右下：成本高但问题占比低
	default:
		return QuadrantRoutine // 左下：成本不高且问题占比低
	}
}

// EvaluateMemberQuadrants 计算组织内每个成员的四象限字段与组织十字线，产出 MemberPoint[] + MemberEvaluation。
//   - CostMedianUSD = 成员 total_cost 中位数；IssueRatioMedian = 成员 issue_cost_ratio 中位数（Y 十字线）。
//   - 每个成员：CostMultiple、IssueCostRatio、DominantIssue、DominantIssueCostUSD、Quadrant、
//     SafetyAnomaly、SuspectedOnly、Calls 由本函数填充；UsageIssues 由调用方（Assemble）回填。
func EvaluateMemberQuadrants(inputs []MemberQuadrantInput, params QuadrantParams) ([]MemberPoint, MemberEvaluation) {
	med := medianOf(costValues(inputs))
	eval := MemberEvaluation{
		CostMultipleBaseline: "median",
		CostMedianUSD:        med,
	}

	points := make([]MemberPoint, 0, len(inputs))
	ratios := make([]float64, 0, len(inputs))
	for _, in := range inputs {
		attrTotal := issueTotal(in.IssueAttributed)
		cm := CostMultiple(in.CostUSD, med)
		ir := IssueCostRatio(attrTotal, in.CostUSD)
		ratios = append(ratios, ir)
		dominant, domCost := DominantIssue(in.IssueAttributed, params.DominantMinCostUSD)
		points = append(points, MemberPoint{
			UserID:                 in.UserID,
			Name:                   in.Name,
			Position:               in.Position,
			DeptID:                 in.DeptID,
			DeptName:               in.DeptName,
			CostUSD:                in.CostUSD,
			CostMultiple:           cm,
			IssueAttributedCostUSD: attrTotal,
			IssueCostRatio:         ir,
			DominantIssue:          dominant,
			DominantIssueCostUSD:   domCost,
			SafetyAnomaly:          in.SafetyAnomaly,
			SafetyReviewTag:        in.SafetyReviewTag,
			SafetyHint:             in.SafetyHint,
			SafetyDetail:           in.SafetyDetail,
			SuspectedOnly:          in.SuspectedOnly,
			Calls:                  in.Calls,
		})
	}

	yMed := medianOf(ratios)
	eval.IssueRatioMedian = yMed
	for i := range points {
		points[i].Quadrant = QuadrantOf(points[i].CostMultiple, points[i].IssueCostRatio, yMed, params.XMedian)
	}
	eval.Members = points
	return points, eval
}

// issueTotal 汇总 members 五种情况归因合计（不含 3.6 unrelated）。
func issueTotal(m map[string]float64) float64 {
	var s float64
	for _, it := range IssueTypeOrder {
		s += m[it]
	}
	return s
}

func costValues(inputs []MemberQuadrantInput) []float64 {
	out := make([]float64, 0, len(inputs))
	for _, in := range inputs {
		out = append(out, in.CostUSD)
	}
	return out
}

// medianOf 返回中位数（偶数取中间两数平均）；空 → 0。
func medianOf(vals []float64) float64 {
	n := len(vals)
	if n == 0 {
		return 0
	}
	sorted := append([]float64(nil), vals...)
	sort.Float64s(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}
