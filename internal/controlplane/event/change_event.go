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
	KeyUpdated       ChangeEventType = "key_updated"
	// v0.7: 授权 / 预算配置全局性变更（不针对特定 key，网关应清空全部 key 缓存）
	AuthConfigChanged  ChangeEventType = "auth_config_changed"
	EntitlementGranted ChangeEventType = "entitlement_granted"
	EntitlementRevoked ChangeEventType = "entitlement_revoked"
	OpenModeChanged    ChangeEventType = "open_mode_changed"
	// v0.15 四层配额体系
	QuotaConfigChanged        ChangeEventType = "quota_config_changed"
	BudgetTierSnapshotUpdated ChangeEventType = "budget_tier_snapshot_updated"
)

// ChangeEvent 配置变更事件
type ChangeEvent struct {
	Type            ChangeEventType `json:"type"`
	ResourceType    string          `json:"resource_type"`
	ResourceID      uuid.UUID       `json:"resource_id"`
	SnapshotVersion int64           `json:"snapshot_version,omitempty"`
	Timestamp       time.Time       `json:"timestamp"`
}

// NewChangeEvent 创建变更事件
func NewChangeEvent(eventType ChangeEventType, resourceType string, resourceID uuid.UUID, snapshotVersion int64) *ChangeEvent {
	return &ChangeEvent{
		Type:            eventType,
		ResourceType:    resourceType,
		ResourceID:      resourceID,
		SnapshotVersion: snapshotVersion,
		Timestamp:       time.Now().UTC(),
	}
}
