package model

import (
	"time"

	"github.com/google/uuid"
)

// ModelTag 模型标签定义
type ModelTag struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Name        string    `gorm:"type:varchar(50);not null;uniqueIndex:idx_model_tags_name_unique" json:"name"`
	Color       string    `gorm:"type:varchar(7);not null;default:'#2BA8A2'" json:"color"`
	Description string    `gorm:"type:varchar(200);default:''" json:"description"`
	SortOrder   int       `gorm:"not null;default:0" json:"sort_order"`
	CreatedAt   time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time `gorm:"not null;autoUpdateTime" json:"updated_at"`
}

func (ModelTag) TableName() string {
	return "model_tags"
}

// ModelTagRelation 模型-标签关联（多对多）
type ModelTagRelation struct {
	ModelID   uuid.UUID `gorm:"type:uuid;primaryKey" json:"model_id"`
	TagID     uuid.UUID `gorm:"type:uuid;primaryKey" json:"tag_id"`
	CreatedAt time.Time `gorm:"not null;autoCreateTime" json:"created_at"`
}

func (ModelTagRelation) TableName() string {
	return "model_tag_relations"
}
