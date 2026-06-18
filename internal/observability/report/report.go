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
	From  string `json:"from"`
	To    string `json:"to"`
	Type  string `json:"type"` // day|week|month
	Days  int    `json:"days"`
	Label string `json:"label"`
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
	AvgInputCtxTokens  float64        `json:"avg_input_ctx_tokens"`  // = prompt_total / req
	CacheHitRate       float64        `json:"cache_hit_rate"`        // = cached / prompt_total
	CostPer1Million    float64        `json:"cost_per_1m_tokens"`    // = cost / total_tokens * 1e6
	HighTierCostRatio  float64        `json:"high_tier_cost_ratio"`  // = top_model_cost / total_cost
	TopModel           string         `json:"top_model"`             // 成本最高模型名
	WoWReqPct          float64        `json:"wow_req_pct"`           // 调用量周环比 %
	WoWTokenPct        float64        `json:"wow_token_pct"`         // Token 周环比 %
	WoWCostPct         float64        `json:"wow_cost_pct"`          // 成本周环比 %
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
}

type UnitService struct {
	ReqCount     int64   `json:"req_count"`
	SuccessCount int64   `json:"success_count"`
	ErrorCount   int64   `json:"error_count"`
	EstCostUSD   float64 `json:"est_cost_usd"`
	TotalTokens  int64   `json:"total_tokens"`
	ActiveDays   int     `json:"active_days"`
	Notes        string  `json:"notes"`
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
	PeerProfiles     []PeerMetric      `json:"peer_profiles"`     // Vicky「同岗使用效能画像」多指标分位表
	TopModelPortrait *TopModelPortrait `json:"top_model_portrait"` // Vicky「成本最高模型的同岗画像」第四章
	Efficiency       Efficiency        `json:"efficiency"`
	Anomalies        []AnomalyHit      `json:"anomalies"`
	Attribution      []Attribution     `json:"attribution"`
	NotComputed      []NotComputed     `json:"not_computed"`
}

// TopModelPortrait 成本最高模型的同岗画像（Vicky 第四章）。
// 仅个人粒度产出；单元级为 nil。
type TopModelPortrait struct {
	Model             string  `json:"model"`               // 成本最高模型名
	PersonCostUSD     float64 `json:"person_cost_usd"`     // 本人该模型成本
	PersonCostRatio   float64 `json:"person_cost_ratio"`   // 本人该模型成本占比
	AdoptionCount     int     `json:"adoption_count"`      // 同岗中使用过该模型的人数
	PoolN             int     `json:"pool_n"`              // 同岗总人数
	AdoptionRate      float64 `json:"adoption_rate"`       // 采用率 = AdoptionCount / PoolN
	FreqP50           float64 `json:"freq_p50"`            // 采用者中该模型周调用 P50
	PersonFreq        float64 `json:"person_freq"`         // 本人该模型调用数
	PersonFreqRank    float64 `json:"person_freq_rank"`    // 本人在采用者中调用分位
	CostDepP50        float64 `json:"cost_dep_p50"`        // 采用者中该模型成本占比 P50
	PersonCostDepRank float64 `json:"person_cost_dep_rank"` // 本人成本依赖在采用者中的分位
	UnitCostP50       float64 `json:"unit_cost_p50"`       // 采用者中该模型每百万 Token 成本 P50（USD）
	PersonUnitCost    float64 `json:"person_unit_cost"`    // 本人该模型单位成本（USD/百万 Token）
	PersonUnitCostRank float64 `json:"person_unit_cost_rank"` // 本人单位成本在采用者中的分位
	Supported         bool    `json:"supported"`
	Note              string  `json:"note,omitempty"`
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
	Severity       string            `json:"severity"` // P0..P3
	Window         string            `json:"window"`
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
