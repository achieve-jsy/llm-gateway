package ehrsync

import (
	"encoding/json"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
)

// DiffEntry 表示一次 upsert 的差异记录（用于写 change_log / skip_log）
type DiffEntry struct {
	Entity          string
	EntityID        string
	OrgID           string
	LocalOverrideAt *time.Time
	LocalOverrideBy string
	LocalValue      map[string]any
	EhrValue        map[string]any
	DifferingFields []string
}

// MarshalLocalValue JSON 序列化
func (d *DiffEntry) MarshalLocalValue() string {
	if len(d.LocalValue) == 0 {
		return "{}"
	}
	b, _ := json.Marshal(d.LocalValue)
	return string(b)
}

func (d *DiffEntry) MarshalEhrValue() string {
	if len(d.EhrValue) == 0 {
		return "{}"
	}
	b, _ := json.Marshal(d.EhrValue)
	return string(b)
}

func (d *DiffEntry) MarshalDifferingFields() string {
	if len(d.DifferingFields) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(d.DifferingFields)
	return string(b)
}

// toChangeLog 转成 sync_change_log 写库模型
func (d *DiffEntry) toChangeLog(batchID string, source string, changeType string) model.EhrSyncChangeLog {
	return model.EhrSyncChangeLog{
		ID:         uuid.New(),
		BatchID:    batchID,
		Entity:     d.Entity,
		EntityID:   d.EntityID,
		OrgID:      d.OrgID,
		ChangeType: changeType,
		OldValue:   d.MarshalLocalValue(),
		NewValue:   d.MarshalEhrValue(),
		Source:     source,
		OccurredAt: time.Now(),
	}
}

// toSkipLog 转成 sync_skip_log
func (d *DiffEntry) toSkipLog(batchID string, overrideAt *time.Time, overrideBy string) model.EhrSyncSkipLog {
	oa := time.Now()
	if overrideAt != nil {
		oa = *overrideAt
	}
	return model.EhrSyncSkipLog{
		ID:              uuid.New(),
		BatchID:         batchID,
		Entity:          d.Entity,
		EntityID:        d.EntityID,
		OrgID:           d.OrgID,
		OverrideAt:      oa,
		OverrideBy:      overrideBy,
		LocalValue:      d.MarshalLocalValue(),
		EhrValue:        d.MarshalEhrValue(),
		DifferingFields: d.MarshalDifferingFields(),
		CreatedAt:       time.Now(),
	}
}

func makeChangeEntry(dst, src any, changes map[string]any) DiffEntry {
	// 这里由调用方根据实体构造 LocalValue / EhrValue
	// 简化：直接把 changes 当作 EhrValue；LocalValue 也复用 changes
	return DiffEntry{
		LocalValue:      changes,
		EhrValue:        changes,
		DifferingFields: fieldKeys(changes),
	}
}

func makeSkipEntry(dst, src any, changes map[string]any) DiffEntry {
	entry := makeChangeEntry(dst, src, changes)
	if override, ok := dst.(HasOverride); ok {
		entry.LocalOverrideAt = override.GetOverride()
	}
	return entry
}

func fieldKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
