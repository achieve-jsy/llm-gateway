package model

import (
	"time"

	"github.com/google/uuid"
)

// AnomalyReviewStatus anomaly_review.status 枚举
// 参考 docs/token-usage-anomaly-work-unrelated-design.md §5 待核查状态机
const (
	AnomalyReviewStatusPendingReview  = "pending_review"
	AnomalyReviewStatusConfirmedOK    = "confirmed_ok"
	AnomalyReviewStatusNeedCheck      = "need_check"
	AnomalyReviewStatusFalsePositive  = "false_positive"
)

// AnomalyReview 严重异常（非工作时段无关消耗）待核查记录
// 参考 docs/token-usage-analysis-report-implementation.md §10、docs/token-usage-anomaly-work-unrelated-design.md §5
// 本轮只做层1元数据判定（不读正文、不依赖大模型）；content_signal 等 CSA 语义线索字段留给后续独立阶段扩展。
// 状态机：pending_review → confirmed_ok / need_check / false_positive（负责人处置，被指向人可申诉见 appeal_note）。
type AnomalyReview struct {
	ID              uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	UserID          string     `gorm:"type:text;not null;index:idx_anomaly_review_user" json:"user_id"`
	DeptID          string     `gorm:"type:text;not null;default:'';index:idx_anomaly_review_dept" json:"dept_id"`
	PeriodStart     time.Time  `gorm:"type:date;not null;index:idx_anomaly_review_period" json:"period_start"`
	PeriodEnd       time.Time  `gorm:"type:date;not null" json:"period_end"`
	UnrelatedCostUSD float64   `gorm:"column:unrelated_cost_usd;type:decimal(18,6);not null;default:0" json:"unrelated_cost_usd"`
	UnrelatedRatio  float64    `gorm:"type:decimal(9,6);not null;default:0" json:"unrelated_ratio"`
	TopHours        string     `gorm:"type:text;not null;default:''" json:"top_hours"`
	TopModel        string     `gorm:"type:text;not null;default:''" json:"top_model"`
	TopKey          string     `gorm:"type:text;not null;default:''" json:"top_key"`
	Status          string     `gorm:"type:varchar(16);not null;default:'pending_review';index:idx_anomaly_review_status;check:status IN ('pending_review','confirmed_ok','need_check','false_positive')" json:"status"`
	Reviewer        string     `gorm:"type:text;not null;default:''" json:"reviewer,omitempty"`
	ReviewedAt      *time.Time `gorm:"type:timestamptz" json:"reviewed_at,omitempty"`
	AppealNote      string     `gorm:"column:appeal_note;type:text;not null;default:''" json:"appeal_note,omitempty"`
	CreatedAt       time.Time  `gorm:"type:timestamptz;not null;default:now()" json:"created_at"`
}

func (AnomalyReview) TableName() string { return "anomaly_review" }
