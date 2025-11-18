package observability

import "time"

// UsageEvent 埋点事件数据契约
// gateway-api 在每次调用完成后构造，异步投递到 Collector
type UsageEvent struct {
	RequestID string
	Timestamp time.Time

	// 身份维度（快照）
	KeyID      string
	KeyName    string
	UserID     string
	UserName   string
	DeptID     string
	DeptName   string
	StudioID   string
	StudioName string

	// 路由/模型
	Model       string
	RoutedModel string
	GroupName   string
	Provider    string

	// Token 用量
	PromptTokens     int
	CachedTokens     int
	CacheWriteTokens int
	CompletionTokens int
	ReasoningTokens  int
	TotalTokens      int
	UsageMissing     bool

	// 状态
	Status         int
	UpstreamStatus int
	DurationMs     int64
	FirstTokenMs   *int  // 首 token 延迟（毫秒），流式时才有
	Stream         bool
	FailoverUsed   bool

	// 请求元信息
	ReasoningEffort string // low / medium / high
	UserAgent       string
	IPAddress       string

	// 成本（采集时算好并锁价）
	InputPriceSnapshot  float64
	OutputPriceSnapshot float64
	InputCostUSD        float64
	OutputCostUSD       float64
	CacheReadCostUSD    float64
	EstCostUSD          float64
}