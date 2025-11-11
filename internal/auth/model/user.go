package model

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Sub       string     `gorm:"type:varchar(128);not null;uniqueIndex:uniq_users_sub" json:"sub"`
	DeptID    *uuid.UUID `gorm:"type:uuid;index:idx_users_dept_id" json:"dept_id,omitempty"`
	Name      string     `gorm:"type:varchar(128);not null" json:"name"`
	Email     string     `gorm:"type:varchar(256);not null;index:idx_users_email" json:"email"`
	Source    string     `gorm:"type:varchar(32);not null;default:oidc" json:"source"`
	Role      string     `gorm:"type:varchar(16);not null;default:user" json:"role"`
	CreatedAt time.Time  `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt time.Time  `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (User) TableName() string { return "users" }
