package model

import (
	"time"
)

// VideoTask 视频任务记录
type VideoTask struct {
	TaskID       string    `gorm:"type:varchar(128);primaryKey" json:"task_id"`
	APIKeyID     int64     `gorm:"not null" json:"api_key_id"`
	APIKeyName   string    `gorm:"type:varchar(128)" json:"api_key_name,omitempty"`
	APIKeyType   string    `gorm:"type:varchar(16)" json:"api_key_type,omitempty"`
	UserID       string    `gorm:"type:text" json:"user_id,omitempty"`
	UserName     string    `gorm:"type:varchar(128)" json:"user_name,omitempty"`
	Model        string    `gorm:"type:text;not null" json:"model"`
	ProviderName string    `gorm:"type:varchar(64);not null" json:"provider_name"`
	Status       string    `gorm:"type:varchar(32);not null;default:submitted" json:"status"`
	CreatedAt    time.Time `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt    time.Time `gorm:"not null;default:now()" json:"updated_at"`
}

func (VideoTask) TableName() string {
	return "video_tasks"
}
