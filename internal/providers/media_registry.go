package providers

import (
	"sort"
	"strings"
	"sync"

	"github.com/example/llm-gateway/internal/core"
)

// MediaAdapterRegistry 是「providerType → 异步媒体任务适配器」的注册表。
//
// 定位（设计文档 §6.2「适配器注册表泛化」）：替换 video_handler 里按渠道双槽硬编码的
// VideoAdapterRegistry，让 Vidu / Kling / … 这类异步渠道的适配器可以按
// providerType 动态查表，并在 provider snapshot 热更新时**整表原子替换**。
//
// 与 ModelRegistry（registry.go）的分工：
//   - ModelRegistry 管「模型 → core.Provider」，服务聊天/嵌入等同步链路；
//   - MediaAdapterRegistry 管「providerType → core.MediaTaskProvider」，服务异步媒体任务链路。
//
// 并发安全：所有方法都用 sync.RWMutex 保护，Swap 与 Get 可安全并发
// （见 tests/providers/media_registry_test.go 的 -race 用例）。
//
// 允许 nil receiver：未接线时（MediaDeps 里注册表为 nil）所有方法都安全降级，
// 不会 panic —— Get/DisplayName 返回零值，Swap 直接返回。
type MediaAdapterRegistry struct {
	mu sync.RWMutex
	// adapters 是整张表；Swap 时整体替换指针指向的 map，绝不原地修改。
	adapters map[string]core.MediaTaskProvider
	// names 是 providerType → 展示名（前端/日志用），与 adapters 同生命周期一起替换。
	names map[string]string
}

// NewMediaAdapterRegistry 创建空注册表。
func NewMediaAdapterRegistry() *MediaAdapterRegistry {
	return &MediaAdapterRegistry{
		adapters: make(map[string]core.MediaTaskProvider),
		names:    make(map[string]string),
	}
}

// Get 按 providerType 返回媒体适配器；未注册返回 nil。
//
// 调用方必须判空（接口值为 nil 时不能直接调用方法）：
//
//	adapter := reg.Get(providerType)
//	if adapter == nil { /* 该渠道没有媒体能力 → 501 */ }
func (r *MediaAdapterRegistry) Get(providerType string) core.MediaTaskProvider {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.adapters[normalizeMediaProviderType(providerType)]
}

// DisplayName 返回 providerType 的展示名；未注册返回空串。
func (r *MediaAdapterRegistry) DisplayName(providerType string) string {
	if r == nil {
		return ""
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.names[normalizeMediaProviderType(providerType)]
}

// Swap 用 next 整表原子替换当前注册表，供热更新链路调用
// （控制面写 provider snapshot → config:change → 网关重建适配器后调用本方法）。
//
// 语义要点：
//   - **整表替换，不是增量 merge**：Next 里没有的 providerType 在替换后即为未注册
//     （渠道被删除/停用时必须真的消失，否则会继续用旧凭证打上游）；
//   - 入参 map 会被**防御性拷贝**：调用方之后复用/修改自己的 map 不会影响注册表，
//     也不会与并发的 Get 形成数据竞争；
//   - providerType 会做 TrimSpace 归一，空 key 的条目被忽略；
//   - adapter 为 nil 的条目**允许存在**（表示该 providerType 已登记但暂无适配器），Get 返回 nil；
//   - names 可与 adapters 不同步（缺名时 DisplayName 回退空串，由调用方用 providerType 兜底）；
//   - 在途请求已经拿到旧的 adapter 接口值，替换不会中断它们（旧对象由 GC 回收）。
func (r *MediaAdapterRegistry) Swap(adapters map[string]core.MediaTaskProvider, names map[string]string) {
	if r == nil {
		return
	}

	nextAdapters := make(map[string]core.MediaTaskProvider, len(adapters))
	for providerType, adapter := range adapters {
		key := normalizeMediaProviderType(providerType)
		if key == "" {
			continue
		}
		nextAdapters[key] = adapter
	}

	nextNames := make(map[string]string, len(names))
	for providerType, name := range names {
		key := normalizeMediaProviderType(providerType)
		if key == "" {
			continue
		}
		nextNames[key] = name
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters = nextAdapters
	r.names = nextNames
}

// ProviderTypes 返回已注册的 providerType，按字典序排序（输出稳定，便于日志与断言）。
func (r *MediaAdapterRegistry) ProviderTypes() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]string, 0, len(r.adapters))
	for providerType := range r.adapters {
		result = append(result, providerType)
	}
	sort.Strings(result)
	return result
}

// Len 返回已注册的 providerType 数量。
func (r *MediaAdapterRegistry) Len() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.adapters)
}

// normalizeProviderType 归一 providerType 键（与仓库其他地方一致：TrimSpace）。
func normalizeMediaProviderType(providerType string) string {
	return strings.TrimSpace(providerType)
}
