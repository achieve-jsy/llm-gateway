package model

import (
	"time"
)

// VideoTask 视频任务记录（用量统计用，非 task_id 映射表）
// 主键为上游原始 task_id；provider_type 用于查询时路由到正确适配器。
type VideoTask struct {
	TaskID       string           `gorm:"type:varchar(128);primaryKey" json:"task_id"`
	APIKeyID     string           `gorm:"type:varchar(64);not null;index:idx_video_tasks_api_key" json:"api_key_id"`
	APIKeyName   string           `gorm:"type:varchar(128)" json:"api_key_name,omitempty"`
	APIKeyType   string           `gorm:"type:varchar(16)" json:"api_key_type,omitempty"`
	UserID       string           `gorm:"type:text" json:"user_id,omitempty"`
	UserName     string           `gorm:"type:varchar(128)" json:"user_name,omitempty"`
	Model        string           `gorm:"type:text;not null;index:idx_video_tasks_model" json:"model"`
	ProviderName string           `gorm:"type:varchar(64);not null" json:"provider_name"`
	ProviderType string           `gorm:"type:varchar(64);not null;default:''" json:"provider_type"`
	Status       string           `gorm:"type:varchar(32);not null;default:submitted;index:idx_video_tasks_status" json:"status"`
	VideoURL     string           `gorm:"type:text" json:"video_url,omitempty"`
	RequestBody  string           `gorm:"type:text" json:"request_body,omitempty"`
	ResponseBody string           `gorm:"type:text" json:"response_body,omitempty"`
	ErrorMessage string           `gorm:"type:text" json:"error_message,omitempty"`
	CreatedAt    time.Time        `gorm:"not null;default:now();index:idx_video_tasks_ts" json:"created_at"`
	UpdatedAt    time.Time        `gorm:"not null;default:now()" json:"updated_at"`
	Events       []VideoTaskEvent `gorm:"foreignKey:TaskID;references:TaskID" json:"events,omitempty"`
}

func (VideoTask) TableName() string {
	return "video_tasks"
}

// VideoTaskEvent 记录任务创建和状态更新事件，供详情页时间线展示。
type VideoTaskEvent struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	TaskID    string    `gorm:"type:varchar(128);not null;index:idx_video_task_events_task_id" json:"task_id"`
	EventType string    `gorm:"type:varchar(64);not null" json:"event_type"`
	EventData string    `gorm:"type:text" json:"event_data,omitempty"`
	CreatedAt time.Time `gorm:"not null;default:now();index:idx_video_task_events_created_at" json:"created_at"`
}

func (VideoTaskEvent) TableName() string {
	return "video_task_events"
}
