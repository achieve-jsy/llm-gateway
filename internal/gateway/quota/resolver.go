package quota

import (
	"sync"
	"sync/atomic"

	"github.com/example/llm-gateway/internal/core/quota"
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
)

// SnapshotHolder 持有最新的 QuotaSnapshot，支持原子热替换。
type SnapshotHolder struct {
	current atomic.Value // *quota.QuotaSnapshot
	mu      sync.Mutex
}

// NewSnapshotHolder 创建快照持有者。
func NewSnapshotHolder() *SnapshotHolder {
	return &SnapshotHolder{}
}

// Set 原子替换快照。
func (h *SnapshotHolder) Set(s *quota.QuotaSnapshot) {
	h.current.Store(s)
}

// Get 返回当前快照（可能为 nil）。
func (h *SnapshotHolder) Get() *quota.QuotaSnapshot {
	v := h.current.Load()
	if v == nil {
		return nil
	}
	return v.(*quota.QuotaSnapshot)
}

// ResolveSubject 解析调用主体类型与 ID。
func ResolveSubject(id *gwmodel.UserIdentity) (subjectType, subjectID string) {
	if id.KeyType == "service" {
		return "service_key", id.KeyID
	}
	return "user", id.UserID
}

// EffectiveLimits 解析指定层的有效配额。
// 返回 (rpm, maxConcurrency, source)。
// source: "default_user" | "default_service_key" | "override" | "unset"
type EffectiveResult struct {
	RPM            int
	MaxConcurrency int
	Source         string
}

// ResolveSubjectLimits 解析主体层（user/service_key）的有效配额。
func ResolveSubjectLimits(snap *quota.QuotaSnapshot, subjectType, subjectID string) EffectiveResult {
	if snap == nil {
		if subjectType == "service_key" {
			return EffectiveResult{RPM: quota.FallbackDefaultServiceKey.RPM, MaxConcurrency: quota.FallbackDefaultServiceKey.MaxConcurrency, Source: "default_service_key"}
		}
		return EffectiveResult{RPM: quota.FallbackDefaultUser.RPM, MaxConcurrency: quota.FallbackDefaultUser.MaxConcurrency, Source: "default_user"}
	}

	switch subjectType {
	case "service_key":
		if lim, ok := snap.ServiceKeys[subjectID]; ok {
			return EffectiveResult{RPM: lim.RPM, MaxConcurrency: lim.MaxConcurrency, Source: "override"}
		}
		return EffectiveResult{RPM: snap.DefaultServiceKey.RPM, MaxConcurrency: snap.DefaultServiceKey.MaxConcurrency, Source: "default_service_key"}
	default: // user
		if lim, ok := snap.Users[subjectID]; ok {
			return EffectiveResult{RPM: lim.RPM, MaxConcurrency: lim.MaxConcurrency, Source: "override"}
		}
		return EffectiveResult{RPM: snap.DefaultUser.RPM, MaxConcurrency: snap.DefaultUser.MaxConcurrency, Source: "default_user"}
	}
}

// ResolveGroupLimits 解析组层的有效配额。
// 未配置（nil/缺失）视为 0（不限）。
func ResolveGroupLimits(snap *quota.QuotaSnapshot, groupName string) EffectiveResult {
	if snap == nil {
		return EffectiveResult{RPM: 0, MaxConcurrency: 0, Source: "unset"}
	}
	if lim, ok := snap.Groups[groupName]; ok {
		return EffectiveResult{RPM: lim.RPM, MaxConcurrency: lim.MaxConcurrency, Source: "override"}
	}
	return EffectiveResult{RPM: 0, MaxConcurrency: 0, Source: "unset"}
}

// ResolveModelLimits 解析模型层的有效配额。
// 未配置（nil/缺失）视为 0（不限）。
func ResolveModelLimits(snap *quota.QuotaSnapshot, modelUUID string) EffectiveResult {
	if snap == nil {
		return EffectiveResult{RPM: 0, MaxConcurrency: 0, Source: "unset"}
	}
	if lim, ok := snap.Models[modelUUID]; ok {
		return EffectiveResult{RPM: lim.RPM, MaxConcurrency: lim.MaxConcurrency, Source: "override"}
	}
	return EffectiveResult{RPM: 0, MaxConcurrency: 0, Source: "unset"}
}
