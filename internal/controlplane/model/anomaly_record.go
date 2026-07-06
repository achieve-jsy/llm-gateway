package model

import (
	"time"

	"github.com/google/uuid"
)

// 异常状态机 5 态（与 report.AnomalyStatus* 常量对齐；写入前由应用层校验，DB 仅存字符串不建 CHECK，
// 避免枚举演进需要迁移）。
const (
	AnomalyStatusPending       = "待确认"   // 异常记录刚识别，未经人工确认（默认态）
	AnomalyStatusConfirmedOK   = "已确认正常" // 人工确认非异常/正常
	AnomalyStatusFixing        = "修复中"   // 人工确认异常并在处理中
	AnomalyStatusRecovered     = "已恢复"   // 人工确认异常已恢复
	AnomalyStatusFalsePositive = "误报"    // 人工判定为误报
)

// AnomalyRecord 异常反馈闭环记录（方案 X）：报告识别命中后 upsert 一条，用户可反馈状态，
// 下次报告读回真实状态覆盖 AnomalyHit.Status。
//
// 唯一 upsert 键 uniq_anomaly_record = (scope_level, scope_id, rule, window_start, member_id)：
// 同一异常窗口同一成员只一条，避免重复堆积。window_start 为 Asia/Shanghai 命中窗口起点，分钟精度；
// 上报与读回序列化统一用 +08:00 RFC3339 / 分钟精度，避免时区/秒级漂移导致匹配不上。
// 状态字段（status/feedback_at/feedback_by/reason）仅供人工反馈接口写入；报告运行时 upsert 只登记
// 默认态「待确认」，绝不回改已反馈状态。
type AnomalyRecord struct {
	ID          uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ScopeLevel  string     `gorm:"type:varchar(16);not null;uniqueIndex:uniq_anomaly_record,priority:1" json:"scope_level"`
	ScopeID     string     `gorm:"type:varchar(255);not null;uniqueIndex:uniq_anomaly_record,priority:2;index:idx_anomaly_scope_window,priority:1" json:"scope_id"`
	Rule        string     `gorm:"type:varchar(8);not null;uniqueIndex:uniq_anomaly_record,priority:3;index:idx_anomaly_scope_window,priority:2" json:"rule"`
	RuleVersion string     `gorm:"type:varchar(8);not null;default:'v1'" json:"rule_version"`
	WindowStart time.Time  `gorm:"type:timestamptz;not null;uniqueIndex:uniq_anomaly_record,priority:4;index:idx_anomaly_scope_window,priority:3" json:"window_start"`
	MemberID    string     `gorm:"type:varchar(255);not null;default:'';uniqueIndex:uniq_anomaly_record,priority:5" json:"member_id"`
	Status      string     `gorm:"type:varchar(32);not null;default:'待确认';index:idx_anomaly_status" json:"status"`
	FeedbackAt  *time.Time `json:"feedback_at,omitempty"`
	FeedbackBy  string     `gorm:"type:varchar(255)" json:"feedback_by,omitempty"`
	Reason      string     `gorm:"type:varchar(512)" json:"reason,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (AnomalyRecord) TableName() string { return "anomaly_records" }
