// Package providers_test 测试 internal/providers 的 MediaAdapterRegistry（异步媒体适配器注册表）。
// 使用手写 fakeMediaAdapter 模拟 core.MediaTaskProvider，不依赖真实上游。
package providers_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// fakeMediaAdapter — 模拟 core.MediaTaskProvider
// ---------------------------------------------------------------------------

// fakeMediaAdapter 只用于断言「注册表里拿到的确实是这一个适配器实例」，
// 所有业务方法都返回固定值，不模拟任何上游行为。
type fakeMediaAdapter struct {
	name string
}

func (f *fakeMediaAdapter) Submit(_ context.Context, _ *core.MediaSubmitRequest) (*core.MediaSubmitResult, error) {
	return &core.MediaSubmitResult{VendorTaskID: f.name + "-vendor-task", Status: core.MediaStatusQueued}, nil
}

func (f *fakeMediaAdapter) Query(_ context.Context, _ string) (*core.MediaTaskStatus, error) {
	return &core.MediaTaskStatus{Status: core.MediaStatusInProgress}, nil
}

// ---------------------------------------------------------------------------
// 基础行为
// ---------------------------------------------------------------------------

// 测试目的：Get 只按 providerType 精确查表，未注册返回 nil，key 前后空白被归一。
func TestMediaAdapterRegistry_Get(t *testing.T) {
	kling := &fakeMediaAdapter{name: "kling"}
	vidu := &fakeMediaAdapter{name: "vidu"}

	registry := providers.NewMediaAdapterRegistry()
	registry.Swap(
		map[string]core.MediaTaskProvider{"kling": kling, "vidu": vidu},
		map[string]string{"kling": "可灵", "vidu": "Vidu"},
	)

	tests := []struct {
		name         string
		description  string
		providerType string
		want         core.MediaTaskProvider
	}{
		{
			name:         "命中已注册渠道",
			description:  "Swap 注册了 kling/vidu，按 providerType 查询应返回对应适配器实例",
			providerType: "kling",
			want:         kling,
		},
		{
			name:         "未注册渠道返回 nil",
			description:  "runway 未在注册表中登记，Get 必须返回 nil（调用方据此返回 501）",
			providerType: "runway",
			want:         nil,
		},
		{
			name:         "空字符串不命中",
			description:  "providerType 为空时不得意外命中任何条目，返回 nil",
			providerType: "",
			want:         nil,
		},
		{
			name:         "前后空白被归一",
			description:  "查询键 \" kling \" 应等价于 \"kling\"（避免上游配置带空格导致查不到适配器）",
			providerType: " kling ",
			want:         kling,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, registry.Get(tt.providerType), tt.description)
		})
	}
}

// 测试目的：Swap 是「整表替换」而不是增量合并——新表里没有的渠道必须真的消失。
func TestMediaAdapterRegistry_Swap_ReplacesWholeTable(t *testing.T) {
	registry := providers.NewMediaAdapterRegistry()
	oldAdapter := &fakeMediaAdapter{name: "old-kling"}

	registry.Swap(
		map[string]core.MediaTaskProvider{"kling": oldAdapter},
		map[string]string{"kling": "旧渠道"},
	)
	require.Equal(t, oldAdapter, registry.Get("kling"))
	require.Equal(t, 1, registry.Len())

	newAdapter := &fakeMediaAdapter{name: "new-vidu"}
	registry.Swap(
		map[string]core.MediaTaskProvider{"vidu": newAdapter},
		map[string]string{"vidu": "新渠道"},
	)

	// 渠道被删除/停用后必须查不到旧适配器，否则会继续用旧凭证打上游。
	assert.Nil(t, registry.Get("kling"), "整表替换后旧渠道必须消失")
	assert.Equal(t, newAdapter, registry.Get("vidu"))
	assert.Empty(t, registry.DisplayName("kling"), "旧渠道的展示名也必须一起消失")
	assert.Equal(t, "新渠道", registry.DisplayName("vidu"))
	assert.Equal(t, []string{"vidu"}, registry.ProviderTypes())
}

// 测试目的：Swap 会防御性拷贝入参 map——调用方之后修改自己的 map 不影响注册表。
func TestMediaAdapterRegistry_Swap_DefensiveCopy(t *testing.T) {
	adapters := map[string]core.MediaTaskProvider{"kling": &fakeMediaAdapter{name: "kling"}}
	names := map[string]string{"kling": "可灵"}

	registry := providers.NewMediaAdapterRegistry()
	registry.Swap(adapters, names)

	// 模拟调用方复用同一个 map 构建下一轮快照：清空 + 改写。
	for key := range adapters {
		delete(adapters, key)
	}
	adapters["vidu"] = &fakeMediaAdapter{name: "vidu"}
	names["kling"] = "被篡改"

	assert.NotNil(t, registry.Get("kling"), "注册表必须持有自己的 map，不受调用方修改影响")
	assert.Nil(t, registry.Get("vidu"), "调用方新增的条目不得凭空出现在注册表里")
	assert.Equal(t, "可灵", registry.DisplayName("kling"), "展示名同样必须是拷贝")
}

// 测试目的：DisplayName / ProviderTypes / Len 的结果稳定可预期（排序 + 缺名回退）。
func TestMediaAdapterRegistry_Introspection(t *testing.T) {
	claude := &fakeMediaAdapter{name: "claude"}
	registry := providers.NewMediaAdapterRegistry()
	registry.Swap(
		map[string]core.MediaTaskProvider{"vidu": nil, "kling": claude, "  ": claude},
		// kling 故意不给展示名，用于断言「缺名回退空串」。
		map[string]string{"vidu": "Vidu", "": "空 key 应被忽略"},
	)

	tests := []struct {
		name        string
		description string
		got         any
		want        any
	}{
		{
			name:        "nil 适配器也允许登记",
			description: "vidu 登记了 nil 适配器（渠道已登记但暂无媒体能力），Get 返回 nil 且不 panic",
			got:         registry.Get("vidu"),
			want:        nil,
		},
		{
			name:        "空 key 条目被忽略",
			description: "Swap 应忽略 providerType 为空的条目，Len 只统计有效渠道",
			got:         registry.Len(),
			want:        2,
		},
		{
			name:        "ProviderTypes 字典序",
			description: "ProviderTypes 输出按字典序排序，保证日志与看板稳定",
			got:         registry.ProviderTypes(),
			want:        []string{"kling", "vidu"},
		},
		{
			name:        "缺展示名返回空串",
			description: "labels 里没有 kling 时应返回空串，由调用方用 providerType 兜底",
			got:         registry.DisplayName("kling"),
			want:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.got, tt.description)
		})
	}
}

// 测试目的：nil 注册表（尚未接线）上所有方法都必须安全降级，不 panic。
func TestMediaAdapterRegistry_NilReceiver(t *testing.T) {
	var registry *providers.MediaAdapterRegistry

	tests := []struct {
		name        string
		description string
		call        func()
	}{
		{
			name:        "Get",
			description: "未接线时 Get 返回 nil，而不是 panic",
			call:        func() { assert.Nil(t, registry.Get("kling")) },
		},
		{
			name:        "DisplayName",
			description: "未接线时 DisplayName 返回空串",
			call:        func() { assert.Empty(t, registry.DisplayName("kling")) },
		},
		{
			name:        "Swap",
			description: "未接线时 Swap 静默返回（启动早期 snapshot 回调可能先于接线）",
			call: func() {
				registry.Swap(map[string]core.MediaTaskProvider{"kling": &fakeMediaAdapter{name: "kling"}}, nil)
			},
		},
		{
			name:        "ProviderTypes",
			description: "未接线时 ProviderTypes 返回 nil 切片",
			call:        func() { assert.Nil(t, registry.ProviderTypes()) },
		},
		{
			name:        "Len",
			description: "未接线时 Len 返回 0",
			call:        func() { assert.Equal(t, 0, registry.Len()) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NotPanics(t, tt.call, tt.description)
		})
	}
}

// ---------------------------------------------------------------------------
// 并发（必须用 -race 跑）
// ---------------------------------------------------------------------------

// 测试目的：Swap 与 Get/DisplayName/ProviderTypes 并发调用时无数据竞争，且读侧永远看不到
// 「半替换」状态——每一轮 Swap 放入的都是自洽的整表，读者要么看到上一版要么看到新版。
func TestMediaAdapterRegistry_ConcurrentSwapAndGet(t *testing.T) {
	const (
		writers          = 4
		readers          = 8
		iterationsPerRun = 200
	)

	// 两版整表轮换：版本号编码在适配器实例上，读者据此断言拿到的一定是「完整的一版」。
	versionA := &fakeMediaAdapter{name: "kling-A"}
	versionB := &fakeMediaAdapter{name: "kling-B"}
	tableA := map[string]core.MediaTaskProvider{"kling": versionA, "vidu": versionA}
	tableB := map[string]core.MediaTaskProvider{"kling": versionB, "vidu": versionB}
	namesA := map[string]string{"kling": "可灵-A", "vidu": "Vidu-A"}
	namesB := map[string]string{"kling": "可灵-B", "vidu": "Vidu-B"}

	registry := providers.NewMediaAdapterRegistry()
	registry.Swap(tableA, namesA)

	var wg sync.WaitGroup

	// 写者：不停地整表 Swap，交替 A/B 两版。
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			for i := 0; i < iterationsPerRun; i++ {
				if (writerID+i)%2 == 0 {
					registry.Swap(tableA, namesA)
				} else {
					registry.Swap(tableB, namesB)
				}
			}
		}(w)
	}

	// 读者：并发读取，断言拿到的永远是 A 或 B 其中之一（不是 nil、也不是混杂视图）。
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterationsPerRun; i++ {
				adapter := registry.Get("kling")
				if adapter == nil {
					t.Errorf("并发 Swap 期间 Get(kling) 不应返回 nil（每一版整表都包含 kling）")
					return
				}
				got, ok := adapter.(*fakeMediaAdapter)
				if !ok {
					t.Errorf("Get(kling) 返回了非预期类型 %T", adapter)
					return
				}
				if got.name != "kling-A" && got.name != "kling-B" {
					t.Errorf("Get(kling) 返回了撕裂视图：%q", got.name)
					return
				}
				// 读侧其它方法同样必须安全（不 panic、不返回半截）。
				_ = registry.DisplayName(got.name)
				_ = registry.ProviderTypes()
				_ = registry.Len()
			}
		}()
	}

	wg.Wait()

	// 所有写者结束后做一次收口 Swap，终态必须完全等于最后一次写入的表。
	registry.Swap(tableB, namesB)
	assert.Equal(t, versionB, registry.Get("kling"))
	assert.Equal(t, versionB, registry.Get("vidu"))
	assert.Equal(t, "可灵-B", registry.DisplayName("kling"))
	assert.Equal(t, []string{"kling", "vidu"}, registry.ProviderTypes())
	assert.Equal(t, 2, registry.Len())
}

// 测试目的：并发 Swap 不同 providerType 组合时，Get 的返回值必须始终来自某一个完整快照。
// 这覆盖「热更新期间渠道增删」这一真实场景（旧渠道消失与新渠道出现不得交错可见）。
func TestMediaAdapterRegistry_ConcurrentSwap_ChannelChurn(t *testing.T) {
	const iterations = 150

	registry := providers.NewMediaAdapterRegistry()
	// 预置一版只含 kling 的快照：读者可能在写者完成第一次 Swap 之前就开始读，
	// 因此初始状态必须已经是自洽的（否则读到 nil 属于测试自身构造不当）。
	registry.Swap(map[string]core.MediaTaskProvider{"kling": &fakeMediaAdapter{name: "kling"}}, nil)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// 写者 1：只注册 kling（模拟只启用一个渠道的快照）。
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			registry.Swap(map[string]core.MediaTaskProvider{"kling": &fakeMediaAdapter{name: "kling"}}, nil)
		}
		close(stop)
	}()

	// 写者 2：注册 kling + vidu（模拟两渠道快照）。
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			registry.Swap(map[string]core.MediaTaskProvider{
				"kling": &fakeMediaAdapter{name: "kling"},
				"vidu":  &fakeMediaAdapter{name: "vidu"},
			}, nil)
		}
	}()

	// 读者：kling 在两版快照里都存在，因此永远查得到；vidu 可有可无，但查到就必须能调用。
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if registry.Get("kling") == nil {
					t.Errorf("kling 在每一版快照中都存在，Get 不应返回 nil")
					return
				}
				if viduAdapter := registry.Get("vidu"); viduAdapter != nil {
					if _, err := viduAdapter.Query(context.Background(), "vendor-task-1"); err != nil {
						t.Errorf("从注册表拿到的适配器必须可直接调用，err=%v", err)
						return
					}
				}
			}
		}()
	}

	wg.Wait()
}

// 测试目的：确保注册表可以存放多个不同 providerType 且互相不串（回归「渠道→适配器」映射）。
func TestMediaAdapterRegistry_MultipleProviderTypes(t *testing.T) {
	registry := providers.NewMediaAdapterRegistry()
	adapters := make(map[string]core.MediaTaskProvider, 16)
	names := make(map[string]string, 16)
	for i := 0; i < 16; i++ {
		providerType := fmt.Sprintf("provider-%02d", i)
		adapters[providerType] = &fakeMediaAdapter{name: providerType}
		names[providerType] = providerType
	}
	registry.Swap(adapters, names)

	for i := 0; i < 16; i++ {
		providerType := fmt.Sprintf("provider-%02d", i)
		adapter, ok := registry.Get(providerType).(*fakeMediaAdapter)
		require.True(t, ok, "providerType=%s 应命中已注册适配器", providerType)
		assert.Equal(t, providerType, adapter.name, "不同 providerType 之间不得串适配器")
	}
	assert.Len(t, registry.ProviderTypes(), 16)
}
