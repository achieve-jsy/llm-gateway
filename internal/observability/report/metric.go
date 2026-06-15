package report

// metric.go —— 数据源无关的归一化中间表示。
//
// adapter/from_http.go（现在）把 /api/v1/usage/*、/api/v1/org/* 的响应归一化为 []DailyMetric + []CallEvent；
// 阶段二新增 from_db.go / from_engine.go，归一化产物一致，derive/装配/渲染零重写。

import (
	"time"
)

// DailyMetric 归一化的单日聚合行（用于 28 天基线 / 同群分位 / 事实层回填）。
type DailyMetric struct {
	Date             string // YYYY-MM-DD（Asia/Shanghai 日切）
	Level            string // personal | department | studio
	ScopeID          string // personal=user_id / department=dept_id / studio=studio_id
	UserID           string // personal 维度
	KeyID            string // 程序/API Key
	KeyType          string // personal | service | ""（空=personal）
	Model            string
	GroupName        string
	ReqCount         int64
	SuccessCount     int64
	ErrorCount       int64
	PromptTokens     int64
	CachedTokens     int64 // prompt 子集，不并入 total
	CacheWriteTokens int64 // prompt 子集，不并入 total
	CompletionTokens int64
	ReasoningTokens  int64
	EstCostUSD       float64
	TotalTokens      int64 // = prompt+completion+reasoning（不含 cached/cache_write）
}

// CallEvent 归一化的逐调用事件（A1–A5 分钟窗判定用）。
type CallEvent struct {
	TS                 time.Time
	KeyID              string
	KeyType            string // personal | service
	UserID             string
	Model              string
	GroupName          string
	Status             int // 200=成功；>=400 失败
	DurationMs         int
	PromptTokens       int
	CompletionTokens   int
	ReasoningTokens    int
	CachedTokens       int // prompt 子集，不并入 total
	CacheWriteTokens   int // prompt 子集，不并入 total
	PromptBeforeCached int // 用于 A4 有效上下文判断（=prompt_total - cached）
	EstCostUSD         float64
	FailoverUsed       bool
	InboundEndpoint    string
}

// ReportInput 派生输入：基线/分位用日聚合，A1–A5 用逐调用事件。
type ReportInput struct {
	Metrics []DailyMetric
	Calls   []CallEvent
	// BaselineCalls 期前历史窗口逐调用事件，供 A1/A2 计算真实历史窗口分布。
	// 为空时 A1 仅使用绝对阈值，A2 仅保留绝对阈值/HARD_CAP，且不得标记为 28 天基线。
	BaselineCalls []CallEvent
	// BaselineMetrics 期前 28 完整自然日（事件日前 28d ~ 前1d）的日聚合序列，用于 28 天稳健基线物化。
	// 若为空，基线明确标记 unsupported；当前周期不得替代真实历史基线。
	BaselineMetrics []DailyMetric
	// ModelContextBudget 模型→有效输入预算（context_length - max_output_tokens），来自 model_catalog（A4 用）。
	// 缺省/未知模型的调用不参与 A4；若全部缺失则 A4 NotComputed。
	ModelContextBudget map[string]int64
	// Cohort 为同岗分位对标池（适配器拉 /api/v1/org/* 后在工具侧拼装；库内不负责拉数）。
	Cohort CohortDef
	// LevelMeta 携带本次报告范围的岗位/工作室/部门上下文（填写 Report.Meta.Scope）。
	LevelMeta Scope
}

// CohortDef 同岗位分位对标池定义（供 derive 计算分位，pool 内每元素为匿名成员）。
type CohortDef struct {
	Enabled   bool
	Option    string // 对标池选择说明（由工具侧决定，如 "studio+job_category"）
	PoolType  string // studio+job_category | company+job_category | studio | company
	Members   []CohortMember
	Metric    string // 效能指标（cost_per_1k_tokens | cache_hit_rate | calls_per_active_day | unique_models）
	Direction string // cost（越低越好）| efficiency（越高越好）
	// RosterUserCount 是当前部门/工作室的花名册总人数；与报告期活跃人数分开。
	RosterUserCount int
}

// CohortMember 标池内一个匿名成员（不指名）。
type CohortMember struct {
	UserID      string
	StudioID    string
	DeptID      string
	JobCategory string
	JobFamily   string
	JobTitle    string
	MetricValue float64 // 该成员在报告期的效能指标值（已预先算好；无效为 0 且 Valid=false）
	MetricValid bool
}

// RuleSet 阈值与参数集中地（阶段一默认值；均来自文档/任务描述，T4 用真实数据校准）。
type RuleSet struct {
	BaselineWindowDays int
	MinBaselineSamples int // 基线有效样本下限（< 即 supported=false，不产出判断）
	MinCohortSample    int // 同岗样本下限（默认 5；不足回退）

	// A1 程序失控
	A1WindowMinutes      int
	A1SustainMinutes     int
	A1MinCallsAbsolute   int
	A1MaxMedianIntervalS float64

	// A2 成本燃烧（USD 判定；HARD_CAP 用 CNY 独立触发）
	A2WindowMinutes          int
	A2BaselineMedianMultiple float64
	A2AbsMinUSD              float64 // ¥50 对应 USD 下限
	A2HardCapCNY             float64 // HARD_CAP_A2 = ¥200/15min（独立触发）
	A2SustainMinutes         int

	// A3 重试循环 —— 段=(key_id,model)，单一 status_code 主导
	A3WindowMinutes     int
	A3MinSegmentReq     int     // 段内 ≥20
	A3FailRateThresh    float64 // >=30%
	A3DominantFailShare float64 // 单状态码占失败 ≥80%
	A3MinFailed         int     // failed ≥6

	// A4 超长上下文 —— 模型上下文上限来自 model_catalog（有效输入预算=context_length-max_output_tokens）
	A4WindowMinutes     int
	A4NearLimitRatio    float64 // >80%（近上限）考虑偏噪 → 用「窗口内近上限请求≥2」或降 P2
	A4MinNearLimit      int     // 窗口内近上限请求 ≥2
	A4MinPromptAbsolute int     // 绝对下限（防低上下文模型误报）

	// A5 非工作时段 —— 阶段一无备案表，纯时间窗口触发并标注
	A5OffHourStart int     // 0..23；表示「早于该小时」为深夜段（00:00–07:00 用 start=8，则 h<8 为 0..7 点）
	A5OffHourEnd   int     // 若 != start，表示第二时段起点；==start 则仅用 start 段 + 周末
	A5MinCallCount int     // 非工作时段样本下限（默认 20）
	A5FloorCNY     float64 // floor：max(同岗位P99, ¥100) 的 ¥100 下限（USD 判定折算）
	A5HardCapCNY   float64 // HARD_CAP_A5：独立触发（升 P1）

	USDCNYRate float64
}

// DefaultRuleSet 返回阶段一默认阈值。
func DefaultRuleSet() RuleSet {
	return RuleSet{
		BaselineWindowDays: 28,
		MinBaselineSamples: 3,
		MinCohortSample:    5,

		A1WindowMinutes:      10,
		A1SustainMinutes:     15,
		A1MinCallsAbsolute:   60,
		A1MaxMedianIntervalS: 10,

		A2WindowMinutes:          15,
		A2BaselineMedianMultiple: 5,
		A2AbsMinUSD:              8, // ≈ ¥50/7.2
		A2HardCapCNY:             200,
		A2SustainMinutes:         60,

		A3WindowMinutes:     5,
		A3MinSegmentReq:     20,
		A3FailRateThresh:    0.30,
		A3DominantFailShare: 0.80,
		A3MinFailed:         6,

		A4WindowMinutes:     15,
		A4NearLimitRatio:    0.80,
		A4MinNearLimit:      2,
		A4MinPromptAbsolute: 100000,

		A5OffHourStart: 8, // 00:00–07:00（h<8 为深夜）+ 周末
		A5OffHourEnd:   8, // ==start 表示仅 start 段
		A5MinCallCount: 20,
		A5FloorCNY:     100,  // max(同岗位P99, ¥100) 的 floor
		A5HardCapCNY:   1000, // HARD_CAP_A5（独立触发，升 P1）

		USDCNYRate: 7.2,
	}
}
