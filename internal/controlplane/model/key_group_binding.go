package model

import (
	"time"

	"github.com/google/uuid"
)

type KeyGroupBinding struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	KeyID     uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:uniq_key_group" json:"key_id"`
	GroupID   uint      `gorm:"not null;uniqueIndex:uniq_key_group;index:idx_kbg_group_id" json:"group_id"`
	CreatedAt time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
}

func (KeyGroupBinding) TableName() string {
	return "key_group_bindings"
}