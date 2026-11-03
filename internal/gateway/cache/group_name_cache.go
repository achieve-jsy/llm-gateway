package cache

import (
	"sync"
	"sync/atomic"
)

// GroupNameCache 保存「哪些名字是模型组」的注册表快照。
//
// 数据由调度器的配置同步（scheduler.ConfigSyncer）注入：组名清单与路由表来自同一个
// 控制面接口 /api/v1/internal/groups，所以这里不再自己拉 HTTP。历史实现为组名单独
// 维护了一份 30s 刷新的 HTTP 缓存，于是重启时出现「路由表已就绪、组名判定仍为空」的
// 窗口，把每个合法组名都判成直连模型（2026-10-30 17:17 blue 切流 403 事故）。
//
// IsInitialized 用于区分「还不知道」和「不是组」：未就绪时调用方必须返回可重试的
// 503 config_not_ready，而不是 403 direct_model_not_allowed。
type GroupNameCache struct {
	mu          sync.RWMutex
	names       map[string]struct{}
	initialized atomic.Bool
}

// NewGroupNameCache 创建空的组名缓存（未就绪，等 SetNames 注入）。
func NewGroupNameCache() *GroupNameCache {
	return &GroupNameCache{names: make(map[string]struct{})}
}

func (gc *GroupNameCache) IsGroupName(name string) bool {
	gc.mu.RLock()
	_, ok := gc.names[name]
	gc.mu.RUnlock()
	return ok
}

// IsInitialized 表示注册表至少成功同步过一次。
// 未初始化时 IsGroupName 的 false 只代表「不知道」，不代表「不是组」。
func (gc *GroupNameCache) IsInitialized() bool {
	return gc.initialized.Load()
}

// SetNames 用一次完整同步的结果整体替换注册表。
func (gc *GroupNameCache) SetNames(groupNames []string) {
	newNames := make(map[string]struct{}, len(groupNames))
	for _, n := range groupNames {
		if n != "" {
			newNames[n] = struct{}{}
		}
	}
	gc.mu.Lock()
	gc.names = newNames
	gc.mu.Unlock()
	gc.initialized.Store(true)
}
