// Package report —— 用量周报共享纯库（阶段一离线工具 / 阶段二 control-plane job 共用）。
//
// 铁律：
//   - 数据源无关：所有派生/装配/渲染只消费归一化中间表示 DailyMetric 与 CallEvent（见 metric.go），
//     不依赖 net/http / gorm，可被阶段二直接 import，派生/装配/渲染零重写。
//   - 三层数据流：fact(纯数字) → judgment(规则：基线/分位/归因/命中/标签/换算) → expression(只回填措辞，无新数字、无新判定)。
//   - 判定性分析全部确定性、可复现、可作依据；无依据不下结论。
package report

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
	TopModels          []TopModel     `json:"top_models"`
}

// UnitFact 单元（department|studio）维度事实，成员与程序两块分离。
type UnitFact struct {
	ScopeID    string      `json:"scope_id"`
	ScopeName  string      `json:"scope_name"`
	StudioID   string      `json:"studio_id,omitempty"`
	Member     UnitMember  `json:"member"`
	ServiceKey UnitService `json:"service_key"`
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
	Baseline    Baseline      `json:"baseline"`
	Peer        Peer          `json:"peer"`
	Efficiency  Efficiency    `json:"efficiency"`
	Anomalies   []AnomalyHit  `json:"anomalies"`
	Attribution []Attribution `json:"attribution"`
	NotComputed []NotComputed `json:"not_computed"`
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
	Headline    string   `json:"headline"`
	Insights    []string `json:"insights"`
	Suggestions []string `json:"suggestions"`
	Tone        string   `json:"tone"`
	Lang        string   `json:"lang"`
	Audience    string   `json:"audience"`
}
