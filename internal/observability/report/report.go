// Package report —— 用量周报共享纯库（阶段一离线工具 / 阶段二 control-plane job 共用）。
//
// 铁律：
//   - 数据源无关：所有派生/装配/渲染只消费归一化中间表示 DailyMetric 与 CallEvent（见 metric.go），
//     不依赖 net/http / gorm，可被阶段二直接 import，派生/装配/渲染零重写。
//   - 三层数据流：fact(纯数字) → judgment(规则：基线/分位/归因/命中/标签/换算) → expression(只回填措辞，无新数字、无新判定)。
//   - 判定性分析全部确定性、可复现、可作依据；无依据不下结论。
package report

import (
	"fmt"
	"time"
)

// SchemaVersion 报告契约版本。
const SchemaVersion = "1.0"

// Report 三层报告根。
type Report struct {
	SchemaVersion string     `json:"schema_version"`
	Meta          Meta       `json:"meta"`
	Fact          Fact       `json:"fact"`
	Judgment      Judgment   `json:"judgment"`
	Expression    Expression `json:"expression"`
	// ModelAnalysis LLM 生成的模型分析章节（各类型注入）。仅在 LLM 分析启用时非 nil；
	// LLM 失败/未启用时 Levels 为空、Note 记录原因，供渲染层展示降级说明。
	ModelAnalysis *ModelAnalysis `json:"model_analysis,omitempty"`
}

// ModelAnalysis LLM 生成的模型分析章节（personal|department|studio 各类型注入）。
type ModelAnalysis struct {
	Level    string `json:"level,omitempty"`    // personal|department|studio
	Sections string `json:"sections,omitempty"` // LLM 生成的 Markdown 章节内容（含 ## 子标题）
	Note     string `json:"note,omitempty"`     // 若 LLM 失败/未启用，说明原因（渲染层可展示降级说明）
}

// Validate 校验可存档报告的最小稳定契约。
func (r Report) Validate() error {
	if r.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema_version %q", r.SchemaVersion)
	}
	if r.Meta.Level != "personal" && r.Meta.Level != "department" && r.Meta.Level != "studio" {
		return fmt.Errorf("invalid meta.level %q", r.Meta.Level)
	}
	if r.Meta.Scope.Level != r.Meta.Level {
		return fmt.Errorf("meta.level %q does not match scope.level %q", r.Meta.Level, r.Meta.Scope.Level)
	}
	facts := 0
	if r.Fact.Personal != nil {
		facts++
	}
	if r.Fact.Department != nil {
		facts++
	}
	if r.Fact.Studio != nil {
		facts++
	}
	if facts != 1 {
		return fmt.Errorf("fact must contain exactly one level, got %d", facts)
	}
	if r.Meta.Level == "personal" && r.Fact.Personal == nil || r.Meta.Level == "department" && r.Fact.Department == nil || r.Meta.Level == "studio" && r.Fact.Studio == nil {
		return fmt.Errorf("fact does not match meta.level %q", r.Meta.Level)
	}
	from, err := time.Parse("2006-01-02", r.Meta.Period.From)
	if err != nil {
		return fmt.Errorf("invalid period.from: %w", err)
	}
	to, err := time.Parse("2006-01-02", r.Meta.Period.To)
	if err != nil {
		return fmt.Errorf("invalid period.to: %w", err)
	}
	if to.Before(from) {
		return fmt.Errorf("period.to precedes period.from")
	}
	wantDays := int(to.Sub(from).Hours()/24) + 1
	if r.Meta.Period.Days != wantDays {
		return fmt.Errorf("period.days=%d, want inclusive %d", r.Meta.Period.Days, wantDays)
	}
	for name, value := range map[string]string{"generated_at": r.Meta.GeneratedAt, "data_retrieved_at": r.Meta.DataRetrievedAt} {
		if _, err := time.Parse(time.RFC3339, value); err != nil {
			return fmt.Errorf("invalid %s: %w", name, err)
		}
	}
	if r.Meta.Currency.USDCNYRate <= 0 {
		return fmt.Errorf("currency.usd_cny_rate must be positive")
	}
	return nil
}

// Meta 元信息。
type Meta struct {
	Tool            string   `json:"tool"`
	Level           string   `json:"level"` // personal | department | studio
	Period          Period   `json:"period"`
	Scope           Scope    `json:"scope"`
	GeneratedAt     string   `json:"generated_at"`
	DataRetrievedAt string   `json:"data_retrieved_at"`
	APIS            []string `json:"api_endpoints_used"`
	Currency        Currency `json:"currency"`
	QA              QA       `json:"qa"`
}

// Period 统计周期（范围、类型、天数、标签）。
type Period struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Type    string `json:"type"` // day|week|month
	Days    int    `json:"days"`
	Label   string `json:"label"`
	IsoWeek string `json:"iso_week,omitempty"` // 报告期 ISO 周数，如 "2026-W33"（按 From 起点归属周）
}

// Scope 报告范围（personal: user_id+name+岗位+studio+dept；department: dept_id+dept_name+studio；studio: studio_id+studio_name）。
type Scope struct {
	Level       string `json:"level"` // personal | department | studio
	ID          string `json:"id"`
	Name        string `json:"name"`
	StudioID    string `json:"studio_id,omitempty"`
	DeptID      string `json:"dept_id,omitempty"`
	JobCategory string `json:"job_category,omitempty"`
	JobFamily   string `json:"job_family,omitempty"`
	JobTitle    string `json:"job_title,omitempty"`
}

// Currency 币种：USD 为唯一事实源；CNY 仅展示。
type Currency struct {
	Base           string  `json:"base"`
	ReportCurrency string  `json:"report_currency"`
	USDCNYRate     float64 `json:"usd_cny_rate"`
	RateSource     string  `json:"rate_source"`
	RateAt         string  `json:"rate_at"`
}

// QA 对账/口径说明。
type QA struct {
	Alignment             []string `json:"口径_alignment"`
	AnomalyEnabled        bool     `json:"anomaly_enabled"`
	PeerPercentileEnabled bool     `json:"peer_percentile_enabled"`
	Note                  string   `json:"note,omitempty"`
}

// ===== 事实层（纯数字，只回填接口裸值，不改算）=====

type Fact struct {
	Personal   *PersonalFact `json:"personal,omitempty"`
	Department *UnitFact     `json:"department,omitempty"`
	Studio     *UnitFact     `json:"studio,omitempty"`
}

// PersonalFact 个人维度事实。
type PersonalFact struct {
	ScopeID            string         `json:"scope_id"`
	ScopeName          string         `json:"scope_name"`
	StatDate           string         `json:"stat_date"`
	ReqCount           int64          `json:"req_count"`
	SuccessCount       int64          `json:"success_count"`
	ErrorCount         int64          `json:"error_count"`
	PromptTokens       int64          `json:"prompt_tokens"`
	CachedTokens       int64          `json:"cached_tokens"`
	CacheWriteTokens   int64          `json:"cache_write_tokens"`
	CompletionTokens   int64          `json:"completion_tokens"`
	ReasoningTokens    int64          `json:"reasoning_tokens"`
	TotalTokens        int64          `json:"total_tokens"` // = prompt+completion+reasoning（不含 cached）
	EstCostUSD         float64        `json:"est_cost_usd"`
	ModelCount         int            `json:"model_count"`
	ActiveDays         int            `json:"active_days"`
	HourlyDistribution map[string]int `json:"hourly_distribution"` // "0":n ... "23":n
	AvgDurationMs      float64        `json:"avg_duration_ms"`
	P95DurationMs      float64        `json:"p95_duration_ms"`
	AvgTTFTMs          float64        `json:"avg_ttft_ms"`
	AvgInputCtxTokens  float64        `json:"avg_input_ctx_tokens"` // = prompt_total / req
	CacheHitRate       float64        `json:"cache_hit_rate"`       // = cached / prompt_total
	CostPer1Million    float64        `json:"cost_per_1m_tokens"`   // = cost / total_tokens * 1e6
	HighTierCostRatio  float64        `json:"high_tier_cost_ratio"` // = top_model_cost / total_cost
	TopModel           string         `json:"top_model"`            // 成本最高模型名
	WoWReqPct          float64        `json:"wow_req_pct"`          // 调用量周环比 %
	WoWTokenPct        float64        `json:"wow_token_pct"`        // Token 周环比 %
	WoWCostPct         float64        `json:"wow_cost_pct"`         // 成本周环比 %
	TopModels          []TopModel     `json:"top_models"`
}

// UnitFact 单元（department|studio）维度事实，成员与程序两块分离。
type UnitFact struct {
	ScopeID    string      `json:"scope_id"`
	ScopeName  string      `json:"scope_name"`
	StudioID   string      `json:"studio_id,omitempty"`
	Member     UnitMember  `json:"member"`
	ServiceKey UnitService `json:"service_key"`
	// 周环比（对比上期同口径 memberAgg；上期窗口 = 本期往前推同等长度）
	WoWReqPct   float64 `json:"wow_req_pct"`
	WoWTokenPct float64 `json:"wow_token_pct"`
	WoWCostPct  float64 `json:"wow_cost_pct"`
	// 岗位分组（第二章/第四章用）
	JobGroups []JobGroup `json:"job_groups,omitempty"`
	// 成本/调用环比变化最大的匿名成员（第四章用）
	TopChanged []TopChangedMember `json:"top_changed,omitempty"`
	// 工作室总成本（部门报告一章「工作室内占比」用；department 级别时由工具侧额外拉 studio agg 填入）
	StudioTotalCostUSD float64 `json:"studio_total_cost_usd,omitempty"`
	// DeptContributions 工作室下各部门贡献（工作室周报专用）
	DeptContributions []DeptContribution `json:"dept_contributions,omitempty"`
	// Vicky 工作室模板「一页摘要」字段（derive 层填充；tool 侧仅在 studio 级别时填入对应的 in 字段）
	CompanyCostUSD     float64        `json:"company_cost_usd,omitempty"`     // 全公司同期总成本（USD）
	CompanyRatio       float64        `json:"company_ratio,omitempty"`        // 本工作室占全公司成本比例 0~1
	AnomalySummary     []AnomalyCount `json:"anomaly_summary,omitempty"`      // 未关闭异常按 Severity 统计（P1×N）
	WeeklyTrend        []WeeklyPoint  `json:"weekly_trend,omitempty"`         // 最近 N 周工作室成本序列（4周趋势）
	OptimizableCostUSD float64        `json:"optimizable_cost_usd,omitempty"` // 预计可优化成本（USD）
	CostConcentration  float64        `json:"cost_concentration,omitempty"`   // 成本集中度：Top2 部门成本占比之和 0~1
	// BudgetExec 工作室某月预算执行（Vicky 一页摘要「预算执行」行；Supported=false 表示不可得）。
	BudgetExec *BudgetExec `json:"budget_exec,omitempty"`
	// Conclusion 部门/工作室【本周结论】主因归因一句话及其因素（derive 层计算，render 消费）。
	// 语义：点明本轮主因 ①成本环比幅度 ②集中在哪些人（岗位）③其中命中什么异常。
	Conclusion *UnitConclusion `json:"conclusion,omitempty"`
}

// UnitConclusion 单元（部门/工作室）【本周结论】主因归因。
// 由 derive 层基于已装配事实（WoW 成本环比 / TopChanged 成员[岗位+AnomalyCause]）确定性计算；
// 渲染层读 MainCause 直接展示，或用 CostDeltaPct / ConcentrationN / ConcentrationJob / AnomalyWord
// 自行组织措辞。Supported=false 表示事实不足；Note 说明原因。
type UnitConclusion struct {
	MainCause        string   `json:"main_cause"`        // 一句话，如「成本环比 +32%，集中在 3 名后端开发，其一命中A5 非工作时段」
	CostDeltaPct     float64  `json:"cost_delta_pct"`    // ① 成本周环比幅度 %（对比上期同口径成员聚合）
	ConcentrationN   int      `json:"concentration_n"`   // ② 集中人数（成本增长的主因岗位人数）
	ConcentrationJob string   `json:"concentration_job"` // ② 主因岗位（job 归一：job_title→job_category→job_family）
	AnomalyLabels    []string `json:"anomaly_labels"`    // ③ 集中人群命中的异常标签（如 "A5 非工作时段"）
	AnomalyWord      string   `json:"anomaly_word"`      // ③ 异常措辞：其一命中{label} / 其中 K 人命中{labels}
	Supported        bool     `json:"supported"`
	Note             string   `json:"note,omitempty"`
}

// AnomalyCount 未关闭异常的 Severity 级统计（Vicky 工作室一页摘要「异常 P1×N」）。
type AnomalyCount struct {
	Severity string `json:"severity"`
	Count    int    `json:"count"`
}

type UnitMember struct {
	UserCount        int        `json:"user_count"`
	ActiveUserCount  int        `json:"active_user_count"`
	ReqCount         int64      `json:"req_count"`
	SuccessCount     int64      `json:"success_count"`
	ErrorCount       int64      `json:"error_count"`
	PromptTokens     int64      `json:"prompt_tokens"`
	CompletionTokens int64      `json:"completion_tokens"`
	ReasoningTokens  int64      `json:"reasoning_tokens"`
	TotalTokens      int64      `json:"total_tokens"`
	EstCostUSD       float64    `json:"est_cost_usd"`
	ModelCount       int        `json:"model_count"`
	TopModels        []TopModel `json:"top_models"`
	PerCapita        PerCapita  `json:"per_capita"`
	// 缓存命中率 = cached_tokens / prompt_tokens（仅 agg 模式有 cached_tokens 时有效）
	CacheHitRate float64 `json:"cache_hit_rate"`
	// 每百万 Token 成本 = cost / total_tokens * 1e6（零分母时为 0）
	CostPer1Million float64 `json:"cost_per_1m_tokens"`
	// 耗时/TTFT/上下文（agg 模式由各成员服务端聚合字段按 req_count 加权得出；无逐调用分布时为 0）。
	AvgDurationMs     float64 `json:"avg_duration_ms"`      // 平均耗时 ms
	P95DurationMs     float64 `json:"p95_duration_ms"`      // P95 耗时 ms
	AvgTTFTMs         float64 `json:"avg_ttft_ms"`          // 平均首字延迟 ms
	AvgInputCtxTokens float64 `json:"avg_input_ctx_tokens"` // 平均输入上下文 token 数
}

type UnitService struct {
	ReqCount     int64   `json:"req_count"`
	SuccessCount int64   `json:"success_count"`
	ErrorCount   int64   `json:"error_count"`
	EstCostUSD   float64 `json:"est_cost_usd"`
	TotalTokens  int64   `json:"total_tokens"`
	ActiveDays   int     `json:"active_days"`
	// OwnerName 该 Service Key 块的负责人（agg 模式由服务端 service-keys 聚合带出；多 Key 时取成本最高 Key 的 owner）。
	OwnerName string `json:"owner_name"`
	// ModelCount 该 Service Key 块使用的去重模型数（agg 模式由各 Key 的 model_count 求和近似，详见 Notes）。
	ModelCount int `json:"model_count"`
	// 环比（对比前一周窗口同范围，各 Key 求和；无上一周期数据时为 0，渲染层降级「—」）。
	PrevReqCount int64   `json:"prev_req_count,omitempty"`
	PrevTokens   int64   `json:"prev_tokens,omitempty"`
	PrevCostUSD  float64 `json:"prev_cost_usd,omitempty"`
	// TopModel 该块主用模型名（成本最高 Key 的 top_model；代表本块主要服务的模型）。
	TopModel string `json:"top_model,omitempty"`
	// Keys 逐 Key 明细（agg 模式由 /usage/report/service-keys 装配，供「ServiceKey 表」逐行渲染）。
	Keys  []ServiceKeyInfo `json:"keys,omitempty"`
	Notes string           `json:"notes"`
}

// ServiceKeyInfo 单个程序 Service Key 的聚合行（ServiceKey 表逐行渲染用）。
type ServiceKeyInfo struct {
	KeyName      string  `json:"key_name"`
	OwnerName    string  `json:"owner_name"`
	ReqCount     int64   `json:"req_count"`
	TotalTokens  int64   `json:"total_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	ModelCount   int     `json:"model_count"`
	SuccessCount int64   `json:"success_count,omitempty"`
	ErrorCount   int64   `json:"error_count,omitempty"`
	TopModel     string  `json:"top_model,omitempty"`      // 该 Key 当期成本最高的主用模型名
	PrevReqCount int64   `json:"prev_req_count,omitempty"` // 环比（无上一周期数据时为 0）
	PrevTokens   int64   `json:"prev_tokens,omitempty"`
	PrevCostUSD  float64 `json:"prev_cost_usd,omitempty"`
}

type PerCapita struct {
	ReqCount    int64   `json:"req_count"`
	EstCostUSD  float64 `json:"est_cost_usd"`
	SuccessRate float64 `json:"success_rate"`
	ActiveDays  int     `json:"active_days"`
}

type TopModel struct {
	Model       string  `json:"model"`
	Calls       int64   `json:"calls"`
	TotalTokens int64   `json:"total_tokens"`
	CostUSD     float64 `json:"cost_usd"`
}

// ===== 判断层（算法可复算；命中必须附证据）=====

type Judgment struct {
	Baseline         Baseline          `json:"baseline"`
	Peer             Peer              `json:"peer"`
	PeerProfiles     []PeerMetric      `json:"peer_profiles"`      // Vicky「同岗使用效能画像」多指标分位表
	TopModelPortrait *TopModelPortrait `json:"top_model_portrait"` // Vicky「成本最高模型的同岗画像」第四章
	// ModelGuidance 个人第五章「场景与合理用模」的模型选用建议 + 省钱预估（仅个人粒度产出）。
	// 基于已算好的事实（高阶模型成本占比/单位成本差/同岗单位成本 P50）确定性计算，数字不臆造，
	// 省钱预估为估算（SaveIsEstimate=true）并给出依据 SaveBasis；建议措辞给 LLM 作为事实输入。
	ModelGuidance *ModelGuidance `json:"model_guidance,omitempty"`
	// PeerUnitCost 单元级匿名横比：本单元「单位活跃人成本」在全公司各部门中的分位（department|studio 报告专用）。
	// 池 = 全公司各部门单位活跃人成本（Σ成本/去重活跃人）；本值取自本单元 PerCapita 成本。样本不足时 Supported=false。
	PeerUnitCost Peer          `json:"peer_unit_cost"`
	Efficiency   Efficiency    `json:"efficiency"`
	Anomalies    []AnomalyHit  `json:"anomalies"`
	Attribution  []Attribution `json:"attribution"`
	NotComputed  []NotComputed `json:"not_computed"`
}

// ModelGuidance 个人第五章「场景与合理用模」：场景标注 + 模型选用建议 + 省钱预估（估算）。
// 数字一律来自既有事实（PersonalFact.TopModels / HighTierCostRatio / TopModelPortrait 单位成本），
// 本层只做确定性推算，不新增业务计算口径；SaveBasis 记录估算依据，EstWeeklySaveCNY 由渲染层乘汇率。
type ModelGuidance struct {
	Supported          bool    `json:"supported"`              // 事实充分（有同岗单位成本对比）才 true
	TopModel           string  `json:"top_model"`              // 成本最高/主用模型名
	TopModelCallShare  float64 `json:"top_model_call_share"`   // 主用模型调用占比 0..1
	TopModelCostRatio  float64 `json:"top_model_cost_ratio"`   // 高阶模型成本占比 0..1（=HighTierCostRatio）
	PersonUnitCostUSD  float64 `json:"person_unit_cost_usd"`   // 本人该模型单位成本 USD/百万Token
	PeerUnitCostP50USD float64 `json:"peer_unit_cost_p50_usd"` // 同岗采用者该模型单位成本 P50 USD/百万Token
	EstWeeklySaveUSD   float64 `json:"est_weekly_save_usd"`    // 估算周节省 USD（对齐同岗单位成本 P50）
	EstWeeklySaveCNY   float64 `json:"est_weekly_save_cny"`    // 估算周节省 CNY（渲染层乘汇率填充）
	SaveIsEstimate     bool    `json:"save_is_estimate"`       // 恒 true：省钱为估算
	ScenarioNote       string  `json:"scenario_note"`          // 场景标注（基于事实的用模结构描述，不臆造场景名）
	PeerAdoptionRate   float64 `json:"peer_adoption_rate"`     // 同岗采用率 = 同岗使用该模型人数 / 同岗总数（0..1）
	PeerAdoptionN      int     `json:"peer_adoption_n"`        // 同岗使用该模型人数
	PeerPoolN          int     `json:"peer_pool_n"`            // 同岗总数（后续可回退为池人数）
	Recommendation     string  `json:"recommendation"`         // 模型选用建议措辞（确定性；LLM 可基于本字段+数字润色）
	SaveBasis          string  `json:"save_basis"`             // 省钱预估依据
	Note               string  `json:"note,omitempty"`         // 事实不足时的说明
}

// TopModelPortrait 成本最高模型的同岗画像（Vicky 第四章）。
// 仅个人粒度产出；单元级为 nil。
type TopModelPortrait struct {
	Model              string  `json:"model"`                 // 成本最高模型名
	PersonCostUSD      float64 `json:"person_cost_usd"`       // 本人该模型成本
	PersonCostRatio    float64 `json:"person_cost_ratio"`     // 本人该模型成本占比
	AdoptionCount      int     `json:"adoption_count"`        // 同岗中使用过该模型的人数
	PoolN              int     `json:"pool_n"`                // 同岗总人数
	AdoptionRate       float64 `json:"adoption_rate"`         // 采用率 = AdoptionCount / PoolN
	FreqP50            float64 `json:"freq_p50"`              // 采用者中该模型周调用 P50
	PersonFreq         float64 `json:"person_freq"`           // 本人该模型调用数
	PersonFreqRank     float64 `json:"person_freq_rank"`      // 本人在采用者中调用分位
	CostDepP50         float64 `json:"cost_dep_p50"`          // 采用者中该模型成本占比 P50
	PersonCostDepRank  float64 `json:"person_cost_dep_rank"`  // 本人成本依赖在采用者中的分位
	UnitCostP50        float64 `json:"unit_cost_p50"`         // 采用者中该模型每百万 Token 成本 P50（USD）
	PersonUnitCost     float64 `json:"person_unit_cost"`      // 本人该模型单位成本（USD/百万 Token）
	PersonUnitCostRank float64 `json:"person_unit_cost_rank"` // 本人单位成本在采用者中的分位
	Supported          bool    `json:"supported"`
	Note               string  `json:"note,omitempty"`
}

// PeerMetric 同岗画像单个指标的分位（本人 vs 同岗 P50 + 分位 rank）。
type PeerMetric struct {
	Metric         string  `json:"metric"`          // 指标 key（如 weekly_calls / total_tokens / active_days ...）
	Label          string  `json:"label"`           // 中文标签
	Unit           string  `json:"unit,omitempty"`  // 展示单位（次 / 万token / 天 / % ...）
	Current        float64 `json:"current"`         // 本人值
	P50            float64 `json:"p50"`             // 同岗中位
	PercentileRank float64 `json:"percentile_rank"` // 0..1
	Supported      bool    `json:"supported"`
	Note           string  `json:"note,omitempty"`
}

// Baseline 28 天稳健基线（中位数 + 1.4826×MAD）。
type Baseline struct {
	Metric        string  `json:"metric"`
	WindowDays    int     `json:"window_days"`
	SampleDays    int     `json:"sample_days"`
	Supported     bool    `json:"supported"`
	Current       float64 `json:"current"`
	Median        float64 `json:"median"`
	MAD           float64 `json:"mad"`
	SigmaRob      float64 `json:"sigma_rob"`
	DevSigma      float64 `json:"dev_sigma"`
	Percentile    float64 `json:"percentile"` // 0..1
	Trend         string  `json:"trend"`      // up|flat|down
	CurrentWindow string  `json:"current_window"`
	Note          string  `json:"note,omitempty"`
}

// Peer 同岗位匿名分位。
type Peer struct {
	Metric         string  `json:"metric"`
	PoolType       string  `json:"pool_type"`
	PoolN          int     `json:"pool_n"`
	SampleN        int     `json:"sample_n"`
	Supported      bool    `json:"supported"`
	Current        float64 `json:"current"`
	PercentileRank float64 `json:"percentile_rank"` // 0..1
	Band           string  `json:"band"`
	P25            float64 `json:"p25"`
	P50            float64 `json:"p50"`
	P75            float64 `json:"p75"`
	P90            float64 `json:"p90"`
	VsPeer         string  `json:"vs_peer"`
	Anonymized     bool    `json:"anonymized"`
	Note           string  `json:"note,omitempty"`
}

// Efficiency 使用效能/效率派生指标（判断层，可复算；零分母→n/a，不参与分位）。
type Efficiency struct {
	SuccessRate     float64 `json:"success_rate,omitempty"`
	ErrorRate       float64 `json:"error_rate,omitempty"`
	DailyAvgCalls   float64 `json:"daily_avg_calls,omitempty"`
	CostPer1kTokens float64 `json:"cost_per_1k_tokens,omitempty"`
	CacheHitRate    float64 `json:"cache_hit_rate,omitempty"`
	OutputRatio     float64 `json:"output_ratio,omitempty"`
	ReasoningShare  float64 `json:"reasoning_share,omitempty"`
	CostUSDCNY      float64 `json:"cost_usd_cny,omitempty"`
	MetricDirection string  `json:"metric_direction,omitempty"` // cost（越低越好）| efficiency（越高越好）
}

// AnomalyHit 统一的 A1–A5 命中 schema（判断层四要素）。
type AnomalyHit struct {
	Rule           string            `json:"rule"` // A1..A5
	RuleVersion    string            `json:"rule_version"`
	Name           string            `json:"name"`
	Severity       string            `json:"severity"`               // P0..P3
	Window         string            `json:"window"`                 // 人类可读窗口描述（Evidence window 同源）
	WindowStart    time.Time         `json:"window_start,omitempty"` // 命中窗口起点（Asia/Shanghai），供两阶段按窗精准拉取
	WindowEnd      time.Time         `json:"window_end,omitempty"`   // 命中窗口终点（Asia/Shanghai），供两阶段按窗精准拉取
	Hit            bool              `json:"hit"`
	NotComputed    bool              `json:"not_computed,omitempty"` // 缺少判定所需事实时为 true；不得视为未命中
	ActualValue    float64           `json:"actual_value"`
	ThresholdValue float64           `json:"threshold_value"`
	BaselineValue  float64           `json:"baseline_value"`
	BaselineWindow string            `json:"baseline_window"`
	ImpactCostUSD  float64           `json:"impact_cost_usd"`
	ExcessCostUSD  float64           `json:"excess_cost_usd"`
	MatchedCount   int               `json:"matched_count"`
	Dimensions     map[string]string `json:"dimensions"`
	Evidence       []Evidence        `json:"evidence"`
	Note           string            `json:"note,omitempty"`
	// Status 异常记录人工反馈状态机（5 态）。离线报告每次现算命中后默认「待确认」；
	// 接入反馈闭环（control-plane feedback API + 持久化）后由读回的真实状态覆盖。空值视为「待确认」。
	Status string `json:"status,omitempty"`
}

// 异常状态机 5 态枚举（模板「异常处置」：待确认/已确认正常/修复中/已恢复/误报）。
const (
	AnomalyStatusPending       = "待确认"   // 异常记录刚识别，未经人工确认（默认态）
	AnomalyStatusConfirmedOK   = "已确认正常" // 人工确认非异常/正常
	AnomalyStatusFixing        = "修复中"   // 人工确认异常并在处理中
	AnomalyStatusRecovered     = "已恢复"   // 人工确认异常已恢复
	AnomalyStatusFalsePositive = "误报"    // 人工判定为误报
)

// AnomalyStatusCounts 按状态机 5 态统计异常记录数（用于「异常处置」章节 5 态计数行）。
// 未知/空状态归入「待确认」；返回顺序固定为 待确认/已确认正常/修复中/已恢复/误报。
type AnomalyStatusCounts struct {
	Pending     int `json:"pending"`        // 待确认
	ConfirmedOK int `json:"confirmed_ok"`   // 已确认正常
	Fixing      int `json:"fixing"`         // 修复中
	Recovered   int `json:"recovered"`      // 已恢复
	FalsePos    int `json:"false_positive"` // 误报
}

// CountAnomalyStatus 汇总给定异常命中的 5 态计数（不修改入参；空状态按「待确认」计）。
func CountAnomalyStatus(hits []AnomalyHit) AnomalyStatusCounts {
	var c AnomalyStatusCounts
	for _, a := range hits {
		switch a.Status {
		case AnomalyStatusConfirmedOK:
			c.ConfirmedOK++
		case AnomalyStatusFixing:
			c.Fixing++
		case AnomalyStatusRecovered:
			c.Recovered++
		case AnomalyStatusFalsePositive:
			c.FalsePos++
		default: // 空 或 AnomalyStatusPending
			c.Pending++
		}
	}
	return c
}

type Evidence struct {
	Field string `json:"field"`
	Value string `json:"value"`
	Ref   string `json:"ref,omitempty"`
	Note  string `json:"note,omitempty"`
}

type Attribution struct {
	Subject string  `json:"subject"`
	Detail  string  `json:"detail"`
	Weight  float64 `json:"weight"`
}

type NotComputed struct {
	Metric string `json:"metric"`
	Reason string `json:"reason"`
}

// ===== 表达层（只读措辞：只引用 fact/judgment 已存在字段，禁止裸数字）=====

type Expression struct {
	Headline        string   `json:"headline"`
	Insights        []string `json:"insights"`
	Suggestions     []string `json:"suggestions"`
	EfficiencyLabel string   `json:"efficiency_label,omitempty"` // 效能标签，如「高采用、待优化」
	Tone            string   `json:"tone"`
	Lang            string   `json:"lang"`
	Audience        string   `json:"audience"`
}
