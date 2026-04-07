package budget

import (
	"sync/atomic"

	"github.com/example/llm-gateway/internal/configsync"
)

type SnapshotHolder struct {
	current atomic.Pointer[configsync.BudgetTierSnapshot]
}

func NewSnapshotHolder() *SnapshotHolder {
	return &SnapshotHolder{}
}

func (h *SnapshotHolder) Get() *configsync.BudgetTierSnapshot {
	return h.current.Load()
}

func (h *SnapshotHolder) Set(next *configsync.BudgetTierSnapshot) bool {
	if next == nil || next.SchemaVersion != 1 {
		return false
	}
	for {
		old := h.current.Load()
		if old != nil && next.Revision < old.Revision {
			return false
		}
		if h.current.CompareAndSwap(old, next) {
			return true
		}
	}
}
