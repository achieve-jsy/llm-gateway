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

// MinuteBucket 期前基线的最小时间粒度聚合：某分钟非 service 调用的 count 与总成本。
// 用于 A1/A2 历史窗口分位的加权滑窗重建，避免拉取/构建全量逐调用事件（大数据部门为数十万行）。
type MinuteBucket struct {
	Minute time.Time // 该分钟（Asia/Shanghai）
	Count  int       // 该分钟非 service 调用数
	Cost   float64   // 该分钟非 service 调用成本合计（USD）
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
	FirstTokenMs       int // 首字延迟 ms（TTFT；日志 first_token_ms，nil→0）
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

// MemberAgg 归一化的成员级聚合行（每人一行、周期聚合、排除 service）。
// 数据源无关：阶段二 main.go 用 FetchMemberAgg(/usage/report/members) 填充，供「按人统计 + 逐层横向对比」
// 人工层地基与 UnitFact/PersonalFact 的成员加法核心。与 DailyMetric 粒度不同（无日期/模型/key 维度），
// 只能复现加法/计数子集，无法还原 TopModels/ServiceKey/日模型分布。
type MemberAgg struct {
	UserID           string
	UserName         string
	JobCategory      string
	JobFamily        string
	JobTitle         string
	DeptID           string
	StudioID         string
	ReqCount         int64
	SuccessCount     int64
	ErrorCount       int64
	PromptTokens     int64
	CompletionTokens int64
	ReasoningTokens  int64
	CachedTokens     int64
	TotalTokens      int64
	CostUSD          float64
	ActiveDays       int64
	ModelCount       int
	// 耗时/TTFT/上下文 派生字段（服务端已算好，供报告「耗时行」装配；口径同服务端 SQL 聚合结果）。
	// 均用 float64 承载：AVG/PERCENTILE_CONT 产出为浮点，与 /usage/report/members 响应 json 精确对齐。
	AvgDurationMs     float64 // 平均耗时 ms
	P95DurationMs     float64 // P95 耗时 ms
	AvgFirstTokenMs   float64 // 平均首字延迟 ms
	AvgInputCtxTokens float64 // 平均输入上下文 token 数
}

// ServiceKeyAgg 归一化的程序 Service Key 聚合行（每 Key 一行、排除 personal、无日期/模型维度）。
// 供 derive 装配 UnitFact.ServiceKey 块（agg 模式恒排除 service，成员聚合接口不可得，需此通道）；
// 数据源无关：工具侧用 client.FetchServiceKeys（/usage/report/service-keys）填充。
// KeyName/OwnerName 取服务端冗余快照；SuccessCount/ErrorCount 为 UnitService 装配所需（客户端已带出）。
type ServiceKeyAgg struct {
	KeyName      string
	OwnerName    string
	ReqCount     int64
	SuccessCount int64
	ErrorCount   int64
	TotalTokens  int64
	CostUSD      float64
	ModelCount   int
	ActiveDays   int64
	// 环比（对比前一周窗口同范围）：来自 /usage/report/service-keys 响应的 prev_* 字段。
	// 无上一周期数据时工具侧置 0（derive 按 0 视为无环比，输出 0%/「—」）。
	PrevReqCount    int64
	PrevTotalTokens int64
	PrevCostUSD     float64
	// TopModel 该 Key 当期成本最高的主用模型名（/usage/report/service-keys 响应的 top_model）。
	TopModel string
}

// BudgetExec 工作室某月预算执行结果（Vicky 工作室一页摘要「预算执行」行）。
// 数据源无关：工具侧在 studio 级别调用 budget-exec 接口后填入 ReportInput.BudgetExec；derive 层原样装配。
// Supported=false 表示该维度未配置预算/数据不可用（NotAvailable），渲染层应显示「预算执行(不可得)」。
type BudgetExec struct {
	Supported     bool    `json:"supported"`
	ScopeType     string  `json:"scope_type"`
	ScopeID       string  `json:"scope_id"`
	Month         string  `json:"month"`
	MonthLimitUSD float64 `json:"month_limit_usd"`
	ActualCostUSD float64 `json:"actual_cost_usd"`
	Percent       float64 `json:"percent"`
	Note          string  `json:"note,omitempty"`
}

// CompanyUserMetric 全公司每用户的口径指标行（匿名横比用）。数据源无关；
// 工具侧用 client.FetchUserMetricsAll（/usage/dashboard/users，dept/studio 传空）填充。
// 注意：用户维度只有 DeptName（部门名，无 DeptID）；供 derive 按 DeptName 分组算「部门单位活跃人成本」。
type CompanyUserMetric struct {
	UserName   string
	DeptName   string
	EstCostUSD float64
	CallCount  int64
	ActiveDays int64
}

// JobGroup 岗位分组统计（部门周报二章）。
type JobGroup struct {
	JobCategory   string  `json:"job_category"`
	ActiveUsers   int     `json:"active_users"`
	AvgCostUSD    float64 `json:"avg_cost_usd"`
	MedianCostUSD float64 `json:"median_cost_usd"` // 单次成本中位 = per-req cost 中位数
	TopModel      string  `json:"top_model"`
}

// DeptContribution 工作室下单个部门的贡献统计（工作室周报第二章）。
type DeptContribution struct {
	DeptID      string  `json:"dept_id"`
	DeptName    string  `json:"dept_name"`
	ActiveUsers int     `json:"active_users"`
	CostUSD     float64 `json:"cost_usd"`
	ReqCount    int64   `json:"req_count"`
	// 以下由 derive 层算出
	CostRatio  float64 `json:"cost_ratio"`   // 占工作室总成本比例 0~1
	AvgCostUSD float64 `json:"avg_cost_usd"` // 人均成本
	// WoWCostPct 该部门成本周环比 %（对比上期同部门成员聚合；无上期数据时为 0）
	WoWCostPct float64 `json:"wow_cost_pct"`
}

// TopChangedMember 成本变化最大成员（匿名，部门周报四章）。
type TopChangedMember struct {
	Idx         int     `json:"idx"` // 1,2,3...
	JobCategory string  `json:"job_category"`
	WoWCostPct  float64 `json:"wow_cost_pct"`
	WoWReqPct   float64 `json:"wow_req_pct"`
	TopModel    string  `json:"top_model"`
	AnomalyRule string  `json:"anomaly_rule"` // 空=无命中
	// AnomalyCause 主因归因：由装配层 correlateTopChangedAnomaly 在异常判定之后按 user 关联回填。
	// derive 层不访问 Anomalies。值形如 "A2 成本燃烧"（rule+name）；空=无命中/无归属。
	AnomalyCause string `json:"anomaly_cause,omitempty"` // 空=无命中
	// UserID 仅供装配层内部按 user 关联异常命中用（json:"-" 不序列化，维持输出匿名）。
	UserID string `json:"-"`
}

// ReportInput 派生输入：基线/分位用日聚合，A1–A5 用逐调用事件。
type ReportInput struct {
	Metrics []DailyMetric
	Calls   []CallEvent
	// MemberAgg 成员级聚合行（排除 service）。非空且 Metrics 为空时，事实层成员加法核心由它构造
	// （用于不拉全量明细的大部门/工作室）；若同时存在 Metrics(明细派生)，以 Metrics 为准（保真 TopModels/ServiceKey/日模型分布）。
	MemberAgg []MemberAgg
	// ServiceKeys 程序 Service Key 聚合行（每 Key 一行、排除 personal；走 /usage/report/service-keys）。
	// 供 derive 装配 UnitFact.ServiceKey 块（agg 模式成员聚合接口恒排除 service，需此通道补 owner/model_count 等）。
	// 空时 ServiceKey 块降级为 Notes 标注「不可得」。
	ServiceKeys []ServiceKeyAgg
	// CompanyUserMetrics 全公司每用户口径指标（无 dept/studio 过滤；走 client.FetchUserMetricsAll）。
	// 供 derive 按 DeptName 分组算「部门单位活跃人成本 = ΣEstCostUSD / 去重UserName」，做单元级匿名横比
	// （judge.PeerUnitCost）。空时该横比 supported=false。
	CompanyUserMetrics []CompanyUserMetric
	// PrevMemberAgg 上期成员聚合行（与 MemberAgg 同口径，用于 derive 计算周环比）。
	// 上期窗口 = 本期 [from,to] 往前推同等长度（prevFrom = from-periodDays, prevTo = from-1d）。
	// 仅 agg 模式（-agg）填充；明细模式暂不处理。
	PrevMemberAgg []MemberAgg
	// StudioMemberAgg 工作室级成员聚合（department 级别报告时额外拉取，用于计算部门在工作室的占比）。
	// 空时不产出占比字段。
	StudioMemberAgg []MemberAgg
	// UserTopModels 每用户的 Top 模型列表（来自 /usage/dashboard/users，部门/工作室级别用于岗位分布主用模型）。
	// key=UserID，value=Top 模型列表（已按 cost DESC 排序）。
	UserTopModels map[string][]CohortTopModel
	// BaselineCalls 期前历史窗口逐调用事件，供 A1/A2 计算真实历史窗口分布。
	// 为空时 A1 仅使用绝对阈值，A2 仅保留绝对阈值/HARD_CAP，且不得标记为 28 天基线。
	BaselineCalls []CallEvent
	// BaselineBuckets 期前历史窗口的分钟桶聚合（非 service），供 A1/A2 做加权滑窗分位重建。
	// 非空时优先于 BaselineCalls 使用：基于分钟桶聚合直接滑动，避免展开数十万逐调用事件（大数据部门内存关键）。
	BaselineBuckets []MinuteBucket
	// BaselineMetrics 期前 28 完整自然日（事件日前 28d ~ 前1d）的日聚合序列，用于 28 天稳健基线物化。
	// 若为空，基线明确标记 unsupported；当前周期不得替代真实历史基线。
	BaselineMetrics []DailyMetric
	// BaselineCurrent 当期 [from,to] 的日聚合序列（同 level/scope 过滤，走 /usage/report/baseline）。
	// 仅 -agg 成员聚合模式下填充（此时 Metrics 保持空以走 buildUnitFactFromAgg，避免成本/Token 被
	// 明细路径清空）；仅供 28 天基线计算当期日均(current)，不参与事实层装配。
	BaselineCurrent []DailyMetric
	// CurrentBuckets 当期 [from,to] 的分钟桶聚合（非 service，走 /usage/report/baseline minute_buckets）。
	// -agg 成员聚合模式无逐调用明细（Calls 空）时，A1/A2 用它在分钟窗口上直接滑窗判定，避免降级
	// 「调用样本不足」；空时 A1/A2 回退 Calls 逐调用路径。
	CurrentBuckets []MinuteBucket
	// ModelContextBudget 模型→有效输入预算（context_length - max_output_tokens），来自 model_catalog（A4 用）。
	// 缺省/未知模型的调用不参与 A4；若全部缺失则 A4 NotComputed。
	ModelContextBudget map[string]int64
	// Cohort 为同岗分位对标池（适配器拉 /api/v1/org/* 后在工具侧拼装；库内不负责拉数）。
	Cohort CohortDef
	// LevelMeta 携带本次报告范围的岗位/工作室/部门上下文（填写 Report.Meta.Scope）。
	LevelMeta Scope
	// DeptNames deptID→deptName 映射（工作室级别时由工具侧填入，用于 DeptContribution.DeptName）
	DeptNames map[string]string `json:"dept_names,omitempty"`
	// CompanyCostUSD 全公司同期总成本（USD），一页摘要「全公司占比」用。0 时不下发占比。
	CompanyCostUSD float64 `json:"company_cost_usd,omitempty"`
	// PrevUserTopModels 上期（同长度窗口）每用户 Top 模型列表，变化归因用。
	// key=UserID，value=该用户上期 Top 模型列表（与 UserTopModels 同结构）。
	PrevUserTopModels map[string][]CohortTopModel `json:"prev_user_top_models,omitempty"`
	// WeeklyTrend 最近 N 周工作室成本序列（4周趋势用）。降序，最新在前。
	WeeklyTrend []WeeklyPoint `json:"weekly_trend,omitempty"`
	// BudgetExec 工作室某月预算执行结果（studio 级别时由工具侧调用 budget-exec 接口填入）。
	// derive 层原样装配到 Studio.BudgetExec；nil（或无预算数据）时 Supported=false 降级。
	BudgetExec *BudgetExec `json:"budget_exec,omitempty"`
}

// WeeklyPoint 单个周窗口的成本/请求序列点（Vicky 四章模板「4 周趋势」用）。
type WeeklyPoint struct {
	WeekStart string  `json:"week_start"`
	WeekEnd   string  `json:"week_end"`
	CostUSD   float64 `json:"cost_usd"`
	ReqCount  int64   `json:"req_count"`
}

// CohortDef 同岗位分位对标池定义（供 derive 计算分位，pool 内每元素为匿名成员）。
type CohortDef struct {
	Enabled   bool
	Option    string // 对标池选择说明（由工具侧决定，如 "工作室+岗位"）
	PoolType  string // 工作室+岗位 | 全公司+岗位 | 工作室 | 全公司
	Members   []CohortMember
	Metric    string // 效能指标（cost_per_1k_tokens | cache_hit_rate | calls_per_active_day | unique_models）
	Direction string // cost（越低越好）| efficiency（越高越好）
	// RosterUserCount 是当前部门/工作室的花名册总人数；与报告期活跃人数分开。
	RosterUserCount int
	// Target 本人在报告期的画像指标 + 周环比（个人粒度；来自服务端聚合，供 derive 填充 PersonalFact）
	Target CohortTarget
}

// CohortTarget 本人画像指标与周环比（个人报告用）。
type CohortTarget struct {
	Metrics       CohortMetrics
	WoWReqPct     float64 // 调用量周环比 %
	WoWTokenPct   float64 // Token 周环比 %
	WoWCostPct    float64 // 成本周环比 %
	TopModel      string  // 成本最高模型名
	TopModelCalls int64
	// 目标用户岗位/组织信息（从 roster 取，用于回填 Scope 标题）
	JobCategory string
	JobFamily   string
	JobTitle    string
	StudioID    string
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
	// Metrics 该成员的完整同岗画像指标集（Vicky 「同岗使用效能画像」7 指标；valid 由各指标非零判定）。
	Metrics CohortMetrics
}

// CohortMetrics 同岗画像成员的多指标集。每个指标字段非零即有效；derive 端各自独立判分位，
// 不做整体 valid 一票否决（允许某成员仅在部分指标参与）。
type CohortMetrics struct {
	ReqCount          int64            // 周调用次数
	TotalTokens       int64            // 周 Token 总量
	EstCostUSD        float64          // 周成本 USD（第四章分母）
	ActiveDays        int64            // 活跃天数
	AvgInputCtxTokens float64          // 平均输入上下文长度 = prompt_total / req
	CacheHitRate      float64          // 缓存 Token 占比 = cached / prompt_total
	CostPer1kTokens   float64          // 每百万 Token 成本 = cost / total_tokens * 1e6
	HighTierCostRatio float64          // 成本最高模型成本占比 = top_model_cost / total_cost
	TopModels         []CohortTopModel // 该成员 Top3 模型（供第四章同岗模型采用率/P50 计算）
}

// CohortTopModel 同岗池成员的单个 Top 模型（来自 dashboard/users top_models）。
type CohortTopModel struct {
	Model       string
	CostUSD     float64
	CallCount   int64
	TotalTokens int64
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
