package model

import (
	"time"

	"gorm.io/datatypes"
)

// SystemLog 系统日志
type SystemLog struct {
	ID              uint64         `gorm:"primaryKey" json:"id"`
	Ts              time.Time      `gorm:"not null;index" json:"ts"`
	Level           string         `gorm:"size:20;not null;index" json:"level"`
	Component       string         `gorm:"size:100;not null;index" json:"component"`
	Message         string         `gorm:"type:text;not null" json:"message"`
	RequestID       *string        `gorm:"size:64;index" json:"request_id"`
	ClientRequestID *string        `gorm:"size:64" json:"client_request_id"`
	UserID          *string        `gorm:"size:64" json:"user_id"`
	Provider        *string        `gorm:"size:100" json:"provider"`
	Model           *string        `gorm:"size:100" json:"model"`
	Group           *string        `gorm:"size:100" json:"group"`
	DurationMs      *int           `json:"duration_ms"`
	Status          *int           `json:"status"`
	ErrorMessage    *string        `gorm:"type:text" json:"error_message"`
	Extra           datatypes.JSON `gorm:"default:'{}'" json:"extra"`
}

func (SystemLog) TableName() string { return "system_logs" }
