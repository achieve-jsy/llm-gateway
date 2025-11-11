package model

import (
	"time"

	"github.com/google/uuid"
)

type Department struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	DeptID    string    `gorm:"type:varchar(64);not null;uniqueIndex:uniq_departments_dept_id" json:"dept_id"`
	DeptName  string    `gorm:"type:varchar(128);not null" json:"dept_name"`
	CreatedAt time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
}

func (Department) TableName() string { return "departments" }
