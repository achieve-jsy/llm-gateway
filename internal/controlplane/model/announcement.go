package model

import (
	"time"

	"github.com/google/uuid"
)

type Announcement struct {
	ID          uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Title       string     `gorm:"type:varchar(200);not null" json:"title"`
	Content     string     `gorm:"type:text;not null" json:"content"`
	Category    string     `gorm:"type:varchar(20);not null;default:'system';check:category IN ('system','feature','alert')" json:"category"`
	Level       string     `gorm:"type:varchar(20);not null;default:'info';check:level IN ('info','warning','critical')" json:"level"`
	DisplayMode string     `gorm:"type:varchar(20);not null;default:'banner';check:display_mode IN ('banner','popup')" json:"display_mode"`
	Status      string     `gorm:"type:varchar(20);not null;default:'draft';check:status IN ('draft','published','archived')" json:"status"`
	SortOrder   int        `gorm:"not null;default:0" json:"sort_order"`
	CreatedBy   string     `gorm:"type:varchar(128);not null;default:''" json:"created_by"`
	PublishedAt *time.Time `gorm:"type:timestamp with time zone" json:"published_at,omitempty"`
	CreatedAt   time.Time  `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time  `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (Announcement) TableName() string { return "announcements" }

type AnnouncementRead struct {
	UserID         uuid.UUID  `gorm:"type:uuid;primaryKey" json:"user_id"`
	AnnouncementID uuid.UUID  `gorm:"type:uuid;primaryKey" json:"announcement_id"`
	IsRead         bool       `gorm:"not null;default:false" json:"is_read"`
	IsDismissed    bool       `gorm:"not null;default:false" json:"is_dismissed"`
	ReadAt         *time.Time `gorm:"type:timestamp with time zone" json:"read_at,omitempty"`
	CreatedAt      time.Time  `gorm:"not null;autoCreateTime" json:"created_at"`
}

func (AnnouncementRead) TableName() string { return "announcement_reads" }