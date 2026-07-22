package report

// RatingGrade 表示个人健康评级等级 (A/B/C)。
type RatingGrade string

const (
	GradeA RatingGrade = "A" // 优秀/标杆：无输出失控，多任务浪费极低，高复用
	GradeB RatingGrade = "B" // 健康/良好：偶发轻微漂移，整体可控
	GradeC RatingGrade = "C" // 需关注/风险：存在严重失控或频繁跨任务浪费
)

// UserHealthProfile 包含用户评级所需的消费、浪费与会话特征（v0.7 规范）。
type UserHealthProfile struct {
	UserID              string      `json:"user_id"`
	Position            string      `json:"position"`
	TotalCostUSD        float64     `json:"total_cost_usd"`
	TotalCalls          int64       `json:"total_calls"`
	TotalTokens         int64       `json:"total_tokens"`
	RunawayWastedCost   float64     `json:"runaway_wasted_cost"`
	MultitaskWastedCost float64     `json:"multitask_wasted_cost"`
	CacheHitRate        float64     `json:"cache_hit_rate"`
	DeepDevSessions     int         `json:"deep_dev_sessions"`
	RollbackCount       int         `json:"rollback_count"`
	HealthGrade         RatingGrade `json:"health_grade,omitempty"`
	Highlights          []string    `json:"highlights,omitempty"`
}

// HighlightCard 亮点卡片明细（v0.7 规范）。
type HighlightCard struct {
	CardType      string `json:"card_type"` // high_reuse_benchmark, precise_prompting, frugal_efficiency
	Title         string `json:"title"`
	Description   string `json:"description"`
	MetricSummary string `json:"metric_summary"`
}
