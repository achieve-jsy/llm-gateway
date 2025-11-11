package model

import (
	"time"

	"github.com/google/uuid"
)

type DevUser struct {
	ID           uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Username     string    `gorm:"type:varchar(64);not null;uniqueIndex:uniq_dev_users_username" json:"username"`
	PasswordHash string    `gorm:"type:varchar(256);not null" json:"-"`
	Role         string    `gorm:"type:varchar(16);not null;default:user" json:"role"`
	CreatedAt    time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
}

func (DevUser) TableName() string { return "dev_users" }
