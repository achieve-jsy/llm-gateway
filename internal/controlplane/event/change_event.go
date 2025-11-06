package event

import (
	"time"

	"github.com/google/uuid"
)

// ChangeEventType 变更事件类型
type ChangeEventType string

const (
	ProviderCreated  ChangeEventType = "provider_created"
	ProviderUpdated  ChangeEventType = "provider_updated"
	ProviderDisabled ChangeEventType = "provider_disabled"
	ProviderEnabled  ChangeEventType = "provider_enabled"
	ProviderDeleted  ChangeEventType = "provider_deleted"
	ModelCreated     ChangeEventType = "model_created"
	ModelUpdated     ChangeEventType = "model_updated"
	ModelDeleted     ChangeEventType = "model_deleted"
)

// ChangeEvent 配置变更事件
type ChangeEvent struct {
	Type         ChangeEventType `json:"type"`
	ResourceType string          `json:"resource_type"`
	ResourceID   uuid.UUID       `json:"resource_id"`
	Timestamp    time.Time       `json:"timestamp"`
}

// NewChangeEvent 创建变更事件
func NewChangeEvent(eventType ChangeEventType, resourceType string, resourceID uuid.UUID) *ChangeEvent {
	return &ChangeEvent{
		Type:         eventType,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Timestamp:    time.Now().UTC(),
	}
}
