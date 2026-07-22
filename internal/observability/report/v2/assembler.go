package report

import (
	"fmt"
)

// 本文件实现 docs §10.1 的 Report 装配器（L4，纯函数）。
//
// 分层口径（务必理解）：
//   - `report` 是纯库，**不能 import `service`**（service 依赖 report，import 会造成循环依赖）。
//   - 因此「判定五类」(§4, service 层) 与「费用归因清算」(§5, service 层) 与「严重异常 3.6」(§8, service 层)
//     **不在此处重跑**；而是由 service 层先算好，以 report 级结果填入 `AssembleInput`（CostBreakdown /
//     PersonalIssues / Members[].UsageIssues / SafetyAnomaly / DataQuality），再交给本装配器做最终组装。
//   - 本装配器负责：字段落位、personal/department/studio 分支、调用四象限+分群(§9, 同包 report)、
//     生成两句话结论(§8.5)、最后 `Report.Validate()` 做 Σattributed≤total 归因防重复校验。
//   - `RuleSet` 是注入的 report 层规则配置（四象限/分群参数、可扩展的结论模板版本），由 service 层传入。

// AssembleInput 是装配器所需的全部输入（report 级，勿含 service 类型）。
type AssembleInput struct {
	// DataQuality 数据质量覆盖率（§1），由上层聚合或默认。
	DataQuality DataQuality
	// CostBreakdown 成本三分法（§3.2），由上层聚合。
	CostBreakdown CostBreakdown
	// SafetyAnomaly 严重异常 3.6（§8），由上层计算；命中时装配器保证 status=pending_review。
	SafetyAnomaly SafetyAnomaly

	// PersonalIssues level=personal 的、已归因清算的 UsageIssues（§4 + §5）。
	PersonalIssues []UsageIssue

	// Members level=department|studio 的成员输入（每人一个，含四象限原始数据 + 各自已归因的 UsageIssues）。
	Members []MemberAssembleInput
}

// MemberAssembleInput 是构建 MemberEvaluation 所需的一个成员输入。
type MemberAssembleInput struct {
	// Quadrant 该成员的四象限原始输入（费用 + 五种归因净额 + safety/suspected）。
	Quadrant MemberQuadrantInput
	// CostBreakdown 是该成员真实调用的成本与 Token 聚合，渲染层不得反推。
	CostBreakdown CostBreakdown
	// UsageIssues 该成员已归因清算的 UsageIssues（回填到 MemberPoint.UsageIssues）。
	UsageIssues []UsageIssue
}

// RuleSet 是装配器用到的 report 层规则配置（§9 四象限/分群参数）。service 层可注入自定义值或留默认。
type RuleSet struct {
	// Quadrant 四象限参数（默认 DefaultQuadrantParams）。
	Quadrant QuadrantParams
	// Cluster 分群参数（默认 DefaultClusterParams）。
	Cluster ClusterParams
}

// DefaultRuleSet 返回默认装配规则配置。
func DefaultRuleSet() RuleSet {
	return RuleSet{Quadrant: DefaultQuadrantParams(), Cluster: DefaultClusterParams()}
}

// Assemble 装配一份 Report 并做最终一致性校验。
//
//   - level=personal：把 PersonalIssues 落位到 Report.UsageIssues，SafetyAnomaly/DataQuality/CostBreakdown 照搬。
//   - level=department|studio：由 Members 构建 MemberEvaluation（四象限 + 分群 + 回填每人 UsageIssues），
//     顶层不设 UsageIssues（契约要求成员级）。
//   - 命中严重异常时把 status 初始化为 pending_review（红线：绝不自动认定违纪）。
//   - 最后 `Report.Validate()`：level=personal 校验 Σattributed≤CostBreakdown.Total；
//     level=department|studio 校验每个成员 Σattributed≤member.CostUSD。
func Assemble(meta Meta, in AssembleInput, rs RuleSet) (*Report, error) {
	r := &Report{SchemaVersion: SchemaVersion, Meta: meta}
	r.DataQuality = in.DataQuality
	r.CostBreakdown = in.CostBreakdown

	switch meta.Level {
	case LevelPersonal:
		// 3.6 严重异常「置顶块」是个人报告概念；命中时 status 恒为 pending_review（红线：绝不自动认定违纪）。
		r.SafetyAnomaly = in.SafetyAnomaly
		if r.SafetyAnomaly.Hit && r.SafetyAnomaly.Status == "" {
			r.SafetyAnomaly.Status = SafetyAnomalyPendingReview
		}
		r.UsageIssues = attachIssueNarratives(in.PersonalIssues)
	case LevelDepartment, LevelStudio:
		// 3.6 在 department|studio 层不走顶层 SafetyAnomaly（保持零值 Hit=false），
		// 而是经 MemberPoint.SafetyAnomaly 描边 + 渲染层从 members 聚合「严重异常置顶」清单。
		me, err := buildMemberEvaluation(in, rs)
		if err != nil {
			return nil, err
		}
		r.MemberEvaluation = me
	default:
		return nil, fmt.Errorf("assemble: unsupported level %q", meta.Level)
	}

	r.Conclusion = buildConclusion(r)
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return r, nil
}

// buildMemberEvaluation 由成员输入构建 MemberEvaluation（§9）：四象限 + 分群 + 回填每人 UsageIssues。
func buildMemberEvaluation(in AssembleInput, rs RuleSet) (*MemberEvaluation, error) {
	quadrantInputs := make([]MemberQuadrantInput, 0, len(in.Members))
	usageByUser := make(map[string][]UsageIssue, len(in.Members))
	costByUser := make(map[string]CostBreakdown, len(in.Members))
	for _, m := range in.Members {
		// 组织图只评同期真实有调用的人；身份目录中的零调用成员不进入中位线、象限或分群。
		if m.Quadrant.Calls <= 0 {
			continue
		}
		if m.Quadrant.SafetyAnomaly && !m.Quadrant.SafetyDetail.Hit {
			m.Quadrant.SafetyDetail.Hit = true
			m.Quadrant.SafetyDetail.Status = SafetyAnomalyPendingReview
			m.Quadrant.SafetyDetail.ReviewTag = m.Quadrant.SafetyReviewTag
			m.Quadrant.SafetyDetail.Hint = m.Quadrant.SafetyHint
		}
		quadrantInputs = append(quadrantInputs, m.Quadrant)
		usageByUser[m.Quadrant.UserID] = attachIssueNarratives(m.UsageIssues)
		costByUser[m.Quadrant.UserID] = m.CostBreakdown
	}

	if rs.Quadrant.XMedian == 0 {
		rs.Quadrant.XMedian = DefaultQuadrantParams().XMedian
	}
	points, eval := EvaluateMemberQuadrants(quadrantInputs, rs.Quadrant)
	for i := range points {
		points[i].UsageIssues = usageByUser[points[i].UserID]
		points[i].CostBreakdown = costByUser[points[i].UserID]
		points[i].TokenBreakdown = points[i].CostBreakdown.TokenBreakdown
		points[i].IsProblemMember = points[i].SafetyAnomaly || points[i].IssueCostRatio > eval.IssueRatioMedian || hasConfirmedIssue(points[i].UsageIssues)
	}
	eval.Members = points

	if rs.Cluster.MinForKMeans == 0 {
		rs.Cluster = DefaultClusterParams()
	}
	eval.Clusters = ClusterMembers(quadrantInputs, rs.Cluster)
	return &eval, nil
}

func hasConfirmedIssue(issues []UsageIssue) bool {
	for _, issue := range issues {
		if issue.Verdict == VerdictConfirmed {
			return true
		}
	}
	return false
}

// buildConclusion 生成两句话结论（§8.5，只回填变量，不新算）。
func buildConclusion(r *Report) Conclusion {
	c := Conclusion{
		CostMainBucket: mainBucket(r.CostBreakdown),
	}
	switch r.Meta.Level {
	case LevelPersonal:
		attr := 0.0
		byIssue := map[string]float64{}
		for _, u := range r.UsageIssues {
			attr += u.AttributedCostUSD
			byIssue[u.Type] += u.AttributedCostUSD
		}
		c.IssueCostRatio = IssueCostRatio(attr, r.CostBreakdown.Total())
		// 主导问题按归因金额最大者选；但最大归因 < 总成本 1% 时视为无实质主导（对齐 §9.2「全<阈值→normal」），
		// 避免「主导问题 X、问题消耗占比 0%」的自相矛盾结论（微末归因不上结论）。
		c.DominantIssue, _ = DominantIssue(byIssue, r.CostBreakdown.Total()*0.01)
	case LevelDepartment, LevelStudio:
		if r.MemberEvaluation != nil {
			attr := 0.0
			cost := 0.0
			byIssue := map[string]float64{}
			for _, p := range r.MemberEvaluation.Members {
				attr += p.IssueAttributedCostUSD
				cost += p.CostUSD
				for _, u := range p.UsageIssues {
					byIssue[u.Type] += u.AttributedCostUSD
				}
			}
			c.IssueCostRatio = IssueCostRatio(attr, cost)
			c.DominantIssue, _ = DominantIssue(byIssue, cost*0.01)
		}
	}
	c.Note = conclusionNote(c, r.CostBreakdown)
	return c
}

// mainBucket 返回成本三分法中金额最大的桶（tie-break 按 uncached_input>cache>output 顺序取首个，确定性）。
func mainBucket(c CostBreakdown) string {
	main := CostBucketUncachedInput
	max := c.UncachedInputCostUSD
	if c.CacheCostUSD > max {
		max = c.CacheCostUSD
		main = CostBucketCache
	}
	if c.OutputCostUSD > max {
		main = CostBucketOutput
	}
	return main
}

// conclusionNote 生成两句话说明：第一句成本主桶占比，第二句主导问题占比（或「用法正常」）。
func conclusionNote(c Conclusion, cb CostBreakdown) string {
	bucket := costBucketDisplayName(c.CostMainBucket)
	total := cb.Total()
	share := 0.0
	if total > 0 {
		switch c.CostMainBucket {
		case CostBucketCache:
			share = cb.CacheCostUSD / total
		case CostBucketOutput:
			share = cb.OutputCostUSD / total
		default:
			share = cb.UncachedInputCostUSD / total
		}
	}
	sentence1 := fmt.Sprintf("成本主要来自%s（占比%.0f%%）。", bucket, share*100)
	if c.DominantIssue == IssueTypeNormal || c.DominantIssue == "" {
		return sentence1 + "本期未发现明显异常用法。"
	}
	return sentence1 + fmt.Sprintf("主导问题为%s，问题消耗占比%.0f%%。",
		issueDisplayName(c.DominantIssue), c.IssueCostRatio*100)
}

var costBucketDisplayNames = map[string]string{
	CostBucketUncachedInput: "非缓存输入",
	CostBucketCache:         "缓存",
	CostBucketOutput:        "输出",
}

var issueDisplayNames = map[string]string{
	IssueTypePrefixPollution:  "前缀污染",
	IssueTypeToothpaste:       "挤牙膏",
	IssueTypeMultiTaskSession: "多任务不切会话",
	IssueTypeOutputRunaway:    "输出失控",
	IssueTypeOverModel:        "杀鸡用牛刀",
}

func costBucketDisplayName(b string) string {
	if n, ok := costBucketDisplayNames[b]; ok {
		return n
	}
	return b
}

func issueDisplayName(t string) string {
	if n, ok := issueDisplayNames[t]; ok {
		return n
	}
	return t
}
