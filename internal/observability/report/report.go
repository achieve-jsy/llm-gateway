// Package report 定义 Token 使用分析报告的统一 Report JSON 契约（schema_version 2.0）。
//
// 铁律（对齐 docs/token-usage-analysis-report-design.md §11、
// docs/token-usage-analysis-report-implementation.md §10）：
//   - 页面 / Markdown / HTML 三份报告同源，都只消费本包定义的 Report 结构，逐字一致。
//   - 本包是纯库：不依赖 DB/HTTP，只做契约定义与结构校验，不做任何数据装配或查询。
//   - 判定/解读分界：后端只出结构化判定（type/verdict/evidence），措辞由渲染层按
//     type/verdict/suggestion_key 模板回填，本包不掺入任何文案生成逻辑。
//   - 费用桶归因防重复计算（§3.6/§5）：Σ UsageIssues[].AttributedCostUSD 必须 <= 总成本，
//     由 Validate() 强制校验，避免四象限 Y 轴失真。
//   - 3.6 严重异常永远只产出 "待核查"（pending_review 起点），Validate() 不允许自动认定违纪。
package report

import (
	"fmt"
	"time"
)

// SchemaVersion 是当前 Report JSON 契约版本。只增不改（语义化）。
const SchemaVersion = "2.0"

// costEpsilonUSD 是费用桶归因校验允许的浮点误差（1 分钱），避免因四舍五入误报。
const costEpsilonUSD = 0.01

// ratioEpsilon 是占比/系数类字段允许的浮点误差。
const ratioEpsilon = 1e-9

// Level 枚举：报告层级。
const (
	LevelPersonal   = "personal"
	LevelDepartment = "department"
	LevelStudio     = "studio"
)

// IssueType 枚举：五种情况类型（对齐规范 §3.1-§3.5）。
const (
	IssueTypePrefixPollution  = "prefix_pollution"   // §3.1 前缀污染（上下文）
	IssueTypeToothpaste       = "toothpaste"         // §3.2 挤牙膏（交互）
	IssueTypeMultiTaskSession = "multi_task_session" // §3.3 多任务不切会话（交互）
	IssueTypeOutputRunaway    = "output_runaway"     // §3.4 输出失控（生成）
	IssueTypeOverModel        = "over_model"         // §3.5 杀鸡用牛刀（选型）
	IssueTypeNormal           = "normal"             // 全部命中 < 阈值时的 dominant_issue 兜底值
)

// Verdict 枚举：情况判定结论。
const (
	VerdictConfirmed   = "confirmed"
	VerdictSuspected   = "suspected"
	VerdictNotHit      = "not_hit"
	VerdictNotComputed = "not_computed"
)

// SafetyAnomalyStatus 枚举：严重异常待核查状态机（§4/§8.3 红线：永远不自动认定违纪）。
const (
	SafetyAnomalyPendingReview = "pending_review"
	SafetyAnomalyConfirmedOK   = "confirmed_ok"
	SafetyAnomalyNeedCheck     = "need_check"
	SafetyAnomalyFalsePositive = "false_positive"
)

// Quadrant 枚举：四象限（对齐规范 §6/§4.1）。
const (
	QuadrantPriorityImprove = "priority_improve" // 右上：成本高 + 问题占比高
	QuadrantCoach           = "coach"            // 左上：成本不高但问题占比高
	QuadrantRoutine         = "routine"          // 左下：成本不高且问题占比低
	QuadrantHighCostNormal  = "high_cost_normal" // 右下：成本高但问题占比低
)

// CostBucket 枚举：成本三分法计费桶（用于 Conclusion.CostMainBucket）。
const (
	CostBucketUncachedInput = "uncached_input"
	CostBucketCache         = "cache"
	CostBucketOutput        = "output"
)

var validLevels = map[string]bool{
	LevelPersonal:   true,
	LevelDepartment: true,
	LevelStudio:     true,
}

var validIssueTypes = map[string]bool{
	IssueTypePrefixPollution:  true,
	IssueTypeToothpaste:       true,
	IssueTypeMultiTaskSession: true,
	IssueTypeOutputRunaway:    true,
	IssueTypeOverModel:        true,
}

// validDominantIssues 允许五种情况类型，或 "normal"（全部 < 阈值时的兜底）。
var validDominantIssues = map[string]bool{
	IssueTypePrefixPollution:  true,
	IssueTypeToothpaste:       true,
	IssueTypeMultiTaskSession: true,
	IssueTypeOutputRunaway:    true,
	IssueTypeOverModel:        true,
	IssueTypeNormal:           true,
	"":                        true, // 未判定/未展示时允许为空
}

var validVerdicts = map[string]bool{
	VerdictConfirmed:   true,
	VerdictSuspected:   true,
	VerdictNotHit:      true,
	VerdictNotComputed: true,
}

var validSafetyAnomalyStatuses = map[string]bool{
	SafetyAnomalyPendingReview: true,
	SafetyAnomalyConfirmedOK:   true,
	SafetyAnomalyNeedCheck:     true,
	SafetyAnomalyFalsePositive: true,
}

var validQuadrants = map[string]bool{
	QuadrantPriorityImprove: true,
	QuadrantCoach:           true,
	QuadrantRoutine:         true,
	QuadrantHighCostNormal:  true,
}

var validCostBuckets = map[string]bool{
	CostBucketUncachedInput: true,
	CostBucketCache:         true,
	CostBucketOutput:        true,
	"":                      true,
}

// Report 是统一 Report JSON 契约根节点（页面/Markdown/HTML 同源，§11）。
type Report struct {
	SchemaVersion string `json:"schema_version"`

	Meta          Meta          `json:"meta"`
	DataQuality   DataQuality   `json:"data_quality"`
	CostBreakdown CostBreakdown `json:"cost_breakdown"`
	SafetyAnomaly SafetyAnomaly `json:"safety_anomaly"`

	// UsageIssues 仅 level=personal 使用；department/studio 走 MemberEvaluation.Members[].UsageIssues。
	UsageIssues []UsageIssue `json:"usage_issues,omitempty"`

	// MemberEvaluation 仅 level=department|studio 使用（§9/§6/§7）。
	MemberEvaluation *MemberEvaluation `json:"member_evaluation,omitempty"`

	Conclusion Conclusion `json:"conclusion"`

	// TotalCalls 该范围统计周期内的成功调用总次数（level=personal 归一；dept 由汇总）。
	TotalCalls int64 `json:"total_calls,omitempty"`
	// HealthProfile 个人健康体检评级与亮点（level=personal 归一；v0.7 规范 §4）。
	HealthProfile *UserHealthProfile `json:"health_profile,omitempty"`
}

// Meta 是报告元信息（§2.5 报告头）。
type Meta struct {
	Level       string   `json:"level"` // personal | department | studio
	Scope       Scope    `json:"scope"`
	Period      Period   `json:"period"`
	Currency    Currency `json:"currency"`
	RuleVersion string   `json:"rule_version"` // 如 "v0.6"
}

// Scope 是报告范围（personal: user_id；department: dept_id + 父工作室；studio: studio_id）。
type Scope struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	StudioID   string `json:"studio_id,omitempty"`
	StudioName string `json:"studio_name,omitempty"`
}

// Period 是统计周期（周报默认，支持自定义区间）。
type Period struct {
	From    string `json:"from"`               // "2006-01-02"
	To      string `json:"to"`                 // "2006-01-02"，闭区间
	IsoWeek string `json:"iso_week,omitempty"` // 如 "2026-W33"（按 From 归属周）
}

// Currency 是报告币种与汇率（est_cost 内部为 USD，报告展示按 Report 币种换算）。
type Currency struct {
	Report     string  `json:"report"` // CNY | USD
	USDCNYRate float64 `json:"usd_cny_rate"`
}

// DataQuality 是数据质量覆盖率（§2.5 报告头必带）。
type DataQuality struct {
	RealSessionCoverage   float64 `json:"real_session_coverage"`
	SceneLabelCoverage    float64 `json:"scene_label_coverage"`
	PriceCatalogCoverage  float64 `json:"price_catalog_coverage"`
	PositionLabelCoverage float64 `json:"position_label_coverage"`
	FailedCostCoverage    float64 `json:"failed_cost_coverage"`
}

// TokenBreakdown 是 v0.6 统一 Token 数量契约。
type TokenBreakdown struct {
	TotalTokens         int64 `json:"total_tokens"`
	InputTokens         int64 `json:"input_tokens"`
	UncachedInputTokens int64 `json:"uncached_input_tokens"`
	CachedTokens        int64 `json:"cached_tokens"`
	CacheWriteTokens    int64 `json:"cache_write_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	ReasoningTokens     int64 `json:"reasoning_tokens"`
}

// CostBreakdown 是成本三分法（§2.2）：非缓存输入 + 缓存 + 输出。
type CostBreakdown struct {
	UncachedInputCostUSD float64 `json:"uncached_input_cost_usd"`
	CacheCostUSD         float64 `json:"cache_cost_usd"`
	CacheWriteIncluded   bool    `json:"cache_write_included"`
	OutputCostUSD        float64 `json:"output_cost_usd"`

	// Token 数量与费用桶同源聚合。TotalTokens 按 v0.6 口径为 prompt+completion+reasoning，
	// cached/cache_write 是 prompt 的组成，不得再次计入总量。
	TokenBreakdown

	// FailedRequestCostUSD 是无效消耗（失败请求，已计入上面三块，仅打标注记，不新增计费块）。
	FailedRequestCostUSD float64 `json:"failed_request_cost_usd"`

	Prev *CostBreakdownPrev `json:"prev,omitempty"`

	ByModelTier []ModelTierCost `json:"by_model_tier,omitempty"`
}

// Total 返回成本三分法总成本（非缓存输入 + 缓存 + 输出），FailedRequestCostUSD 已含于其中，不叠加。
func (c CostBreakdown) Total() float64 {
	return c.UncachedInputCostUSD + c.CacheCostUSD + c.OutputCostUSD
}

// CostBreakdownPrev 是上一周期的成本三分法（用于本周 vs 上周堆叠条对比）。
type CostBreakdownPrev struct {
	UncachedInputCostUSD float64 `json:"uncached_input_cost_usd"`
	CacheCostUSD         float64 `json:"cache_cost_usd"`
	OutputCostUSD        float64 `json:"output_cost_usd"`
}

// ModelTierCost 是「成本构成 × 模型档位」切片（§7）。
type ModelTierCost struct {
	Model                string  `json:"model"`
	Tier                 string  `json:"tier"` // 高阶/中/轻量，来自 model_catalog + 组等级
	CostUSD              float64 `json:"cost_usd"`
	UncachedInputCostUSD float64 `json:"uncached_input_cost_usd"`
	CacheCostUSD         float64 `json:"cache_cost_usd"`
	OutputCostUSD        float64 `json:"output_cost_usd"`
}

// SafetyAnomaly 是 3.6 严重异常（§4，独立置顶块，永远待核查，不认定违纪）。
type SafetyAnomaly struct {
	Hit                bool     `json:"hit"`
	UnrelatedCostRatio float64  `json:"unrelated_cost_ratio,omitempty"`
	UnrelatedCostUSD   float64  `json:"unrelated_cost_usd,omitempty"`
	TopHours           []string `json:"top_hours,omitempty"`
	TopModel           string   `json:"top_model,omitempty"`
	TopKey             string   `json:"top_key,omitempty"`
	Basis              string   `json:"basis,omitempty"`
	Excluded           []string `json:"excluded,omitempty"`

	// Status 状态机：pending_review | confirmed_ok | need_check | false_positive。
	// 命中时初始值必须是 pending_review，只能由负责人回填其他状态，Report 层不自动认定。
	Status string `json:"status,omitempty"`

	// ReviewTag 标记：例如达标 work_related 标记为「疑似加班未备案」，未达标项保留为「待核查」。
	ReviewTag string `json:"review_tag,omitempty"`
	// Hint 在报告中提示核实备案，降低虚警风险。
	Hint string `json:"hint,omitempty"`
	// Priority 核查优先级：low（低 - 疑似加班误报）/ medium（中 - 仅时间线索）/ high（高 - 明显私人事务）。
	Priority string `json:"priority,omitempty"`
	// ContentSignal 语义信号：work_related | not_work_related | uncertain。
	ContentSignal string `json:"content_signal,omitempty"`
	// ContentConfidence 语义判定置信度。
	ContentConfidence float64 `json:"content_confidence,omitempty"`
	// ContentSamples 正文内容样本（用于复核与审计）。
	ContentSamples []string `json:"content_samples,omitempty"`
	// Evidence 语义增强层调试与审计证据。
	Evidence map[string]any `json:"evidence,omitempty"`
}

// UsageIssue 是五种情况（§3）统一判定结构，个人层在 Report.UsageIssues，
// 成员层在 MemberEvaluation.Members[].UsageIssues。
type UsageIssue struct {
	Type    string `json:"type"`    // prefix_pollution|toothpaste|multi_task_session|output_runaway|over_model
	Verdict string `json:"verdict"` // confirmed|suspected|not_hit|not_computed

	RequestCount int     `json:"request_count"`
	SessionCount int     `json:"session_count"`
	RequestShare float64 `json:"request_share"`
	SessionShare float64 `json:"session_share"`

	// AttributedCostUSD 进合计、进 Y 轴（费用桶归因清算后的主因费用，§5/§3.6）。
	AttributedCostUSD float64 `json:"attributed_cost_usd"`
	// OverlapCostUSD 共同命中、不重复计。
	OverlapCostUSD float64 `json:"overlap_cost_usd"`
	// AffectedCostUSD 涉及请求的整笔费用，仅展示影响面，不进 Y 轴。
	AffectedCostUSD float64 `json:"affected_cost_usd"`

	Confidence float64        `json:"confidence"`
	Evidence   map[string]any `json:"evidence,omitempty"`
	Excluded   []string       `json:"excluded,omitempty"`

	SuggestionKey string `json:"suggestion_key,omitempty"`

	// Narrative 是面向读者的稳定解释；Evidence 仅供技术复核，不应默认直接展示。
	Narrative *IssueNarrative `json:"narrative,omitempty"`
}

// MemberEvaluation 是成员整合评估（§6/§7，仅 department|studio 使用）。
type MemberEvaluation struct {
	CostMultipleBaseline string  `json:"cost_multiple_baseline"` // 目前固定 "median"
	CostMedianUSD        float64 `json:"cost_median_usd"`
	IssueRatioMedian     float64 `json:"issue_ratio_median"` // Y 轴十字线

	Members  []MemberPoint   `json:"members"`
	Clusters []MemberCluster `json:"clusters,omitempty"`
}

// MemberPoint 是四象限散点的单个成员评估结果。
type MemberPoint struct {
	UserID   string `json:"user_id"`
	Name     string `json:"name"`
	Position string `json:"position"`
	DeptID   string `json:"dept_id,omitempty"`
	DeptName string `json:"dept_name,omitempty"`

	CostUSD      float64 `json:"cost_usd"`
	CostMultiple float64 `json:"cost_multiple"` // cost_usd / median(members.cost_usd)
	// TokenBreakdown 与 CostBreakdown 均来自该成员真实调用，供部门/工作室做精确加总。
	TokenBreakdown TokenBreakdown `json:"token_breakdown"`
	CostBreakdown  CostBreakdown  `json:"cost_breakdown"`

	IssueAttributedCostUSD float64 `json:"issue_attributed_cost_usd"`
	IssueCostRatio         float64 `json:"issue_cost_ratio"`

	Quadrant             string  `json:"quadrant"` // priority_improve|coach|routine|high_cost_normal
	DominantIssue        string  `json:"dominant_issue"`
	DominantIssueCostUSD float64 `json:"dominant_issue_cost_usd"`

	SafetyAnomaly   bool   `json:"safety_anomaly"`
	SafetyReviewTag string `json:"safety_review_tag,omitempty"`
	SafetyHint      string `json:"safety_hint,omitempty"`
	// SafetyDetail 保存组织成员的完整 3.6 待核查证据；仅用于独立清单/描边，绝不参与 Y 轴、主导问题或分群。
	SafetyDetail  SafetyAnomaly `json:"safety_detail,omitempty"`
	SuspectedOnly bool          `json:"suspected_only"` // 命中情况全为 suspected → 前端画虚边
	// IsProblemMember 明确报告分类：高于组织问题占比中位、任一 3.1–3.5 confirmed，或命中 3.6。
	IsProblemMember bool `json:"is_problem_member"`

	Calls int `json:"calls"`

	UsageIssues []UsageIssue `json:"usage_issues,omitempty"`
}

// MemberCluster 是分群结果（§7，可空）。
type MemberCluster struct {
	Label     string   `json:"label"`
	MemberIDs []string `json:"member_ids"`
}

// Conclusion 是机器模板两句话结论（§8.5，只回填变量，不新算）。
type Conclusion struct {
	CostMainBucket string  `json:"cost_main_bucket"` // uncached_input|cache|output
	IssueCostRatio float64 `json:"issue_cost_ratio"`
	DominantIssue  string  `json:"dominant_issue"` // 五种情况之一 或 "normal"
	Note           string  `json:"note"`
}

// Validate 校验 Report 的最小稳定契约与核心业务约束。
//
// 核心校验（§3.6/§5，费用桶归因防重复计算，Y 轴正确性关键）：
//
//	level=personal:   Σ UsageIssues[].AttributedCostUSD <= CostBreakdown.Total()
//	level=department|studio: 对每个 member，Σ member.UsageIssues[].AttributedCostUSD <= member.CostUSD
func (r Report) Validate() error {
	if r.SchemaVersion != SchemaVersion {
		return fmt.Errorf("report: unsupported schema_version %q, want %q", r.SchemaVersion, SchemaVersion)
	}
	if err := r.Meta.validate(); err != nil {
		return fmt.Errorf("report: meta: %w", err)
	}
	if err := r.DataQuality.validate(); err != nil {
		return fmt.Errorf("report: data_quality: %w", err)
	}
	if err := r.CostBreakdown.validate(); err != nil {
		return fmt.Errorf("report: cost_breakdown: %w", err)
	}
	if err := r.SafetyAnomaly.validate(); err != nil {
		return fmt.Errorf("report: safety_anomaly: %w", err)
	}

	switch r.Meta.Level {
	case LevelPersonal:
		if r.MemberEvaluation != nil {
			return fmt.Errorf("report: level=personal must not set member_evaluation")
		}
		total := r.CostBreakdown.Total()
		if err := validateUsageIssues(r.UsageIssues, total, "usage_issues"); err != nil {
			return fmt.Errorf("report: %w", err)
		}
	case LevelDepartment, LevelStudio:
		if len(r.UsageIssues) > 0 {
			return fmt.Errorf("report: level=%s must not set top-level usage_issues (use member_evaluation.members[].usage_issues)", r.Meta.Level)
		}
		if r.MemberEvaluation == nil {
			return fmt.Errorf("report: level=%s requires member_evaluation", r.Meta.Level)
		}
		if err := r.MemberEvaluation.validate(); err != nil {
			return fmt.Errorf("report: member_evaluation: %w", err)
		}
	default:
		// Meta.validate 已校验 level 合法性，理论不会到达这里。
		return fmt.Errorf("report: unhandled level %q", r.Meta.Level)
	}

	if err := r.Conclusion.validate(); err != nil {
		return fmt.Errorf("report: conclusion: %w", err)
	}
	return nil
}

func (m Meta) validate() error {
	if !validLevels[m.Level] {
		return fmt.Errorf("invalid level %q", m.Level)
	}
	if m.Scope.ID == "" {
		return fmt.Errorf("scope.id must not be empty")
	}
	from, err := time.Parse("2006-01-02", m.Period.From)
	if err != nil {
		return fmt.Errorf("invalid period.from: %w", err)
	}
	to, err := time.Parse("2006-01-02", m.Period.To)
	if err != nil {
		return fmt.Errorf("invalid period.to: %w", err)
	}
	if to.Before(from) {
		return fmt.Errorf("period.to precedes period.from")
	}
	if m.Currency.Report == "" {
		return fmt.Errorf("currency.report must not be empty")
	}
	if m.Currency.USDCNYRate <= 0 {
		return fmt.Errorf("currency.usd_cny_rate must be positive")
	}
	if m.RuleVersion == "" {
		return fmt.Errorf("rule_version must not be empty")
	}
	return nil
}

func (d DataQuality) validate() error {
	fields := map[string]float64{
		"real_session_coverage":   d.RealSessionCoverage,
		"scene_label_coverage":    d.SceneLabelCoverage,
		"price_catalog_coverage":  d.PriceCatalogCoverage,
		"position_label_coverage": d.PositionLabelCoverage,
		"failed_cost_coverage":    d.FailedCostCoverage,
	}
	for name, v := range fields {
		if v < -ratioEpsilon || v > 1+ratioEpsilon {
			return fmt.Errorf("%s=%v out of range [0,1]", name, v)
		}
	}
	return nil
}

func (c CostBreakdown) validate() error {
	if c.TotalTokens < 0 || c.InputTokens < 0 || c.UncachedInputTokens < 0 || c.CachedTokens < 0 || c.CacheWriteTokens < 0 || c.OutputTokens < 0 || c.ReasoningTokens < 0 {
		return fmt.Errorf("token counts must not be negative")
	}
	if c.UncachedInputCostUSD < 0 {
		return fmt.Errorf("uncached_input_cost_usd must not be negative")
	}
	if c.CacheCostUSD < 0 {
		return fmt.Errorf("cache_cost_usd must not be negative")
	}
	if c.OutputCostUSD < 0 {
		return fmt.Errorf("output_cost_usd must not be negative")
	}
	if c.FailedRequestCostUSD < 0 {
		return fmt.Errorf("failed_request_cost_usd must not be negative")
	}
	total := c.Total()
	if c.FailedRequestCostUSD > total+costEpsilonUSD {
		return fmt.Errorf("failed_request_cost_usd=%v exceeds total cost=%v (failed cost must already be included in the three buckets)", c.FailedRequestCostUSD, total)
	}
	if c.Prev != nil {
		if c.Prev.UncachedInputCostUSD < 0 || c.Prev.CacheCostUSD < 0 || c.Prev.OutputCostUSD < 0 {
			return fmt.Errorf("prev costs must not be negative")
		}
	}
	for i, mt := range c.ByModelTier {
		if mt.Model == "" {
			return fmt.Errorf("by_model_tier[%d].model must not be empty", i)
		}
		if mt.CostUSD < 0 || mt.UncachedInputCostUSD < 0 || mt.CacheCostUSD < 0 || mt.OutputCostUSD < 0 {
			return fmt.Errorf("by_model_tier[%d] costs must not be negative", i)
		}
	}
	return nil
}

func (s SafetyAnomaly) validate() error {
	if s.UnrelatedCostRatio < -ratioEpsilon || s.UnrelatedCostRatio > 1+ratioEpsilon {
		return fmt.Errorf("unrelated_cost_ratio=%v out of range [0,1]", s.UnrelatedCostRatio)
	}
	if s.UnrelatedCostUSD < 0 {
		return fmt.Errorf("unrelated_cost_usd must not be negative")
	}
	if s.Status != "" && !validSafetyAnomalyStatuses[s.Status] {
		return fmt.Errorf("invalid status %q", s.Status)
	}
	if s.Hit {
		// 红线：命中即待核查，不能一上来就是已确认/已排除；只有负责人回填才能改状态，
		// 这里只保证状态字段非空且合法，真正的"初始值必须是 pending_review"由装配器保证，
		// Validate 只做契约层校验（允许下游把已回填状态再写回来）。
		if s.Status == "" {
			return fmt.Errorf("status must not be empty when hit=true")
		}
	}
	return nil
}

func validateUsageIssues(issues []UsageIssue, totalCostUSD float64, field string) error {
	var attributedSum float64
	for i, issue := range issues {
		if !validIssueTypes[issue.Type] {
			return fmt.Errorf("%s[%d]: invalid type %q", field, i, issue.Type)
		}
		if !validVerdicts[issue.Verdict] {
			return fmt.Errorf("%s[%d]: invalid verdict %q", field, i, issue.Verdict)
		}
		if issue.RequestCount < 0 || issue.SessionCount < 0 {
			return fmt.Errorf("%s[%d]: request_count/session_count must not be negative", field, i)
		}
		if issue.RequestShare < -ratioEpsilon || issue.RequestShare > 1+ratioEpsilon {
			return fmt.Errorf("%s[%d]: request_share=%v out of range [0,1]", field, i, issue.RequestShare)
		}
		if issue.SessionShare < -ratioEpsilon || issue.SessionShare > 1+ratioEpsilon {
			return fmt.Errorf("%s[%d]: session_share=%v out of range [0,1]", field, i, issue.SessionShare)
		}
		if issue.AttributedCostUSD < 0 {
			return fmt.Errorf("%s[%d]: attributed_cost_usd must not be negative", field, i)
		}
		if issue.OverlapCostUSD < 0 {
			return fmt.Errorf("%s[%d]: overlap_cost_usd must not be negative", field, i)
		}
		if issue.AffectedCostUSD < 0 {
			return fmt.Errorf("%s[%d]: affected_cost_usd must not be negative", field, i)
		}
		if issue.AttributedCostUSD > issue.AffectedCostUSD+costEpsilonUSD && issue.AffectedCostUSD > 0 {
			return fmt.Errorf("%s[%d]: attributed_cost_usd=%v exceeds affected_cost_usd=%v", field, i, issue.AttributedCostUSD, issue.AffectedCostUSD)
		}
		if issue.Confidence < -ratioEpsilon || issue.Confidence > 1+ratioEpsilon {
			return fmt.Errorf("%s[%d]: confidence=%v out of range [0,1]", field, i, issue.Confidence)
		}
		attributedSum += issue.AttributedCostUSD
	}
	// 核心校验：费用桶归因防重复计算，Σ attributed_cost_usd <= 总成本。
	if attributedSum > totalCostUSD+costEpsilonUSD {
		return fmt.Errorf("%s: sum(attributed_cost_usd)=%v exceeds total cost=%v (cost bucket attribution must not double count)", field, attributedSum, totalCostUSD)
	}
	return nil
}

func (m MemberEvaluation) validate() error {
	if m.IssueRatioMedian < -ratioEpsilon || m.IssueRatioMedian > 1+ratioEpsilon {
		return fmt.Errorf("issue_ratio_median=%v out of range [0,1]", m.IssueRatioMedian)
	}
	if m.CostMedianUSD < 0 {
		return fmt.Errorf("cost_median_usd must not be negative")
	}
	seen := make(map[string]bool, len(m.Members))
	for i, member := range m.Members {
		if member.UserID == "" {
			return fmt.Errorf("members[%d]: user_id must not be empty", i)
		}
		if seen[member.UserID] {
			return fmt.Errorf("members[%d]: duplicate user_id %q", i, member.UserID)
		}
		seen[member.UserID] = true
		if member.CostUSD < 0 {
			return fmt.Errorf("members[%d]: cost_usd must not be negative", i)
		}
		if err := member.CostBreakdown.validate(); err != nil {
			return fmt.Errorf("members[%d].cost_breakdown: %w", i, err)
		}
		if member.CostBreakdown.Total() > 0 && member.CostBreakdown.Total() > member.CostUSD+costEpsilonUSD {
			return fmt.Errorf("members[%d]: cost_breakdown total exceeds cost_usd", i)
		}
		if member.IssueAttributedCostUSD < 0 {
			return fmt.Errorf("members[%d]: issue_attributed_cost_usd must not be negative", i)
		}
		if member.IssueCostRatio < -ratioEpsilon || member.IssueCostRatio > 1+ratioEpsilon {
			return fmt.Errorf("members[%d]: issue_cost_ratio=%v out of range [0,1]", i, member.IssueCostRatio)
		}
		if !validQuadrants[member.Quadrant] {
			return fmt.Errorf("members[%d]: invalid quadrant %q", i, member.Quadrant)
		}
		if !validDominantIssues[member.DominantIssue] {
			return fmt.Errorf("members[%d]: invalid dominant_issue %q", i, member.DominantIssue)
		}
		if member.DominantIssueCostUSD < 0 {
			return fmt.Errorf("members[%d]: dominant_issue_cost_usd must not be negative", i)
		}
		if member.Calls < 0 {
			return fmt.Errorf("members[%d]: calls must not be negative", i)
		}
		if member.SafetyAnomaly != member.SafetyDetail.Hit {
			return fmt.Errorf("members[%d]: safety_anomaly must match safety_detail.hit", i)
		}
		if err := member.SafetyDetail.validate(); err != nil {
			return fmt.Errorf("members[%d].safety_detail: %w", i, err)
		}
		// 核心校验：每个成员各自的费用桶归因防重复计算。
		field := fmt.Sprintf("members[%d].usage_issues", i)
		if err := validateUsageIssues(member.UsageIssues, member.CostUSD, field); err != nil {
			return err
		}
	}
	for i, cluster := range m.Clusters {
		if cluster.Label == "" {
			return fmt.Errorf("clusters[%d]: label must not be empty", i)
		}
		for _, id := range cluster.MemberIDs {
			if !seen[id] {
				return fmt.Errorf("clusters[%d]: member_id %q not found in members", i, id)
			}
		}
	}
	return nil
}

func (c Conclusion) validate() error {
	if !validCostBuckets[c.CostMainBucket] {
		return fmt.Errorf("invalid cost_main_bucket %q", c.CostMainBucket)
	}
	if c.IssueCostRatio < -ratioEpsilon || c.IssueCostRatio > 1+ratioEpsilon {
		return fmt.Errorf("issue_cost_ratio=%v out of range [0,1]", c.IssueCostRatio)
	}
	if !validDominantIssues[c.DominantIssue] {
		return fmt.Errorf("invalid dominant_issue %q", c.DominantIssue)
	}
	return nil
}
