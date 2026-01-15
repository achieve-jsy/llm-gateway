package model

import (
	"time"

	"github.com/google/uuid"
)

// AuditLog 操作审计日志（管理面写操作留痕）
type AuditLog struct {
	ID           uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	UserID       uuid.UUID `gorm:"type:uuid;not null;index:idx_audit_logs_user_id" json:"user_id"`
	UserName     string    `gorm:"type:varchar(128);not null;default:''" json:"user_name"`
	Action       string    `gorm:"type:varchar(64);not null;index:idx_audit_logs_action" json:"action"`
	ResourceType string    `gorm:"type:varchar(32);not null;index:idx_audit_logs_resource_type" json:"resource_type"`
	ResourceID   string    `gorm:"type:text;not null;default:''" json:"resource_id"`
	ResourceName string    `gorm:"type:varchar(255);not null;default:''" json:"resource_name"`
	DeptID       string    `gorm:"type:text;not null;default:'';index:idx_audit_logs_dept_id" json:"dept_id"`
	DeptName     string    `gorm:"type:varchar(128);not null;default:''" json:"dept_name"`
	StatusCode   int       `gorm:"not null;default:200" json:"status_code"`
	ClientIP     string    `gorm:"type:varchar(64);not null;default:''" json:"client_ip"`
	UserAgent    string    `gorm:"type:text;not null;default:''" json:"user_agent"`
	Details      string    `gorm:"type:text;not null;default:''" json:"details"`
	CreatedAt    time.Time `gorm:"not null;autoCreateTime;index:idx_audit_logs_created_at" json:"created_at"`
}

func (AuditLog) TableName() string { return "audit_logs" }
