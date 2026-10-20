package model

import (
	"time"

	"github.com/google/uuid"
)

// Feedback 用户反馈：用户在管理面提交的建议/问题/其他反馈，由管理员在后台处理。
type Feedback struct {
	ID         uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	UserID     uuid.UUID  `gorm:"type:uuid;not null;index:idx_feedbacks_user_id" json:"user_id"`
	UserName   string     `gorm:"type:varchar(128);not null;default:''" json:"user_name"`
	UserEmail  string     `gorm:"type:varchar(255);not null;default:''" json:"user_email"`
	Type       string     `gorm:"type:varchar(32);not null;default:suggestion" json:"type"` // suggestion | bug | usage | other
	Content    string     `gorm:"type:text;not null" json:"content"`
	Contact    string     `gorm:"type:varchar(128);not null;default:''" json:"contact"`
	Status     string     `gorm:"type:varchar(32);not null;default:pending;index:idx_feedbacks_status" json:"status"` // pending | processing | resolved | closed
	// 刻意不加 omitempty：未处理时前端需要拿到显式 null（契约与 web/src/api/feedback.ts 均按可为 null 声明）
	HandledAt  *time.Time `gorm:"type:timestamptz" json:"handled_at"`
	HandledBy  *uuid.UUID `gorm:"type:uuid" json:"handled_by"`
	HandleNote string     `gorm:"type:text;not null;default:''" json:"handle_note"`
	CreatedAt  time.Time  `gorm:"not null;default:now();index:idx_feedbacks_created_at" json:"created_at"`
	UpdatedAt  time.Time  `gorm:"not null;default:now()" json:"updated_at"`
}

func (Feedback) TableName() string { return "feedbacks" }
