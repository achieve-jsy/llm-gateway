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
	KeyCreated       ChangeEventType = "key_created"
	KeyEnabled       ChangeEventType = "key_enabled"
	KeyDisabled      ChangeEventType = "key_disabled"
	KeyDeleted       ChangeEventType = "key_deleted"
	KeyLogChanged    ChangeEventType = "key_log_changed"
)

// ChangeEvent 配置变更事件
type ChangeEvent struct {
	Type         ChangeEventType `json:"type"`
	ResourceType string          `json:"resource_type"`
	ResourceID   uuid.UUID       `json:"resource_id"`
	SnapshotVersion int64        `json:"snapshot_version,omitempty"`
	Timestamp    time.Time       `json:"timestamp"`
}

// NewChangeEvent 创建变更事件
func NewChangeEvent(eventType ChangeEventType, resourceType string, resourceID uuid.UUID, snapshotVersion int64) *ChangeEvent {
	return &ChangeEvent{
		Type:         eventType,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		SnapshotVersion: snapshotVersion,
		Timestamp:    time.Now().UTC(),
	}
}
