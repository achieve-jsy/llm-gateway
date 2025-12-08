package model

import (
	"time"

	"github.com/google/uuid"
)

// ChangeType 模型状态变更类型常量
const (
	ChangeTypeAutoDisabled   = "auto_disabled"
	ChangeTypeAutoRecovered  = "auto_recovered"
	ChangeTypeManualDisabled = "manual_disabled"
	ChangeTypeManualEnabled  = "manual_enabled"
)

// ModelStatusLog 模型状态变更日志
type ModelStatusLog struct {
	ID           uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ModelID      uuid.UUID  `gorm:"type:uuid;not null;index:idx_model_status_logs_model_id" json:"model_id"`
	ModelName    string     `gorm:"type:varchar(200);not null" json:"model_name"`
	ProviderID   uuid.UUID  `gorm:"type:uuid;not null" json:"provider_id"`
	ProviderName string     `gorm:"type:varchar(200);not null" json:"provider_name"`
	ChangeType   string     `gorm:"type:varchar(20);not null;index:idx_model_status_logs_change_type" json:"change_type"`
	Reason       string     `gorm:"type:varchar(500);not null;default:''" json:"reason"`
	ErrorMessage string     `gorm:"type:text;default:''" json:"error_message"`
	OperatorID   *uuid.UUID `gorm:"type:uuid" json:"operator_id,omitempty"`
	OperatorName string     `gorm:"type:varchar(128);default:''" json:"operator_name"`
	CreatedAt    time.Time  `gorm:"not null;autoCreateTime;index:idx_model_status_logs_created_at" json:"created_at"`
}

func (ModelStatusLog) TableName() string {
	return "model_status_logs"
}