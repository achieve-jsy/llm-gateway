package model

import "time"

// AlertJobHeartbeat 告警任务心跳记录
type AlertJobHeartbeat struct {
	ID             uint       `gorm:"primaryKey" json:"id"`
	Name           string     `gorm:"size:255;not null;uniqueIndex" json:"name"`
	Status         string     `gorm:"size:30;not null;default:'running'" json:"status"`
	LastDurationMs int        `gorm:"not null;default:0" json:"last_duration_ms"`
	LastSuccessAt  *time.Time `json:"last_success_at"`
	LastErrorAt    *time.Time `json:"last_error_at"`
	LastResult     string     `gorm:"type:text;not null;default:''" json:"last_result"`
	LastError      string     `gorm:"type:text;not null;default:''" json:"last_error"`
	UpdatedAt      time.Time  `gorm:"not null" json:"updated_at"`
}

func (AlertJobHeartbeat) TableName() string { return "alert_job_heartbeats" }
