package scheduler

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeModels 快速构建指定数量的 ModelRoutingEntry（ID 从 1 开始）
func makeModels(count int) []*ModelRoutingEntry {
	models := make([]*ModelRoutingEntry, count)
	for i := 0; i < count; i++ {
		models[i] = &ModelRoutingEntry{
			ModelID:   uint(i + 1),
			ModelName: fmt.Sprintf("model-%d", i+1),
		}
	}
	return models
}

func newStore() *HealthStore {
	return NewHealthStore(3, 5, 10*time.Second, time.Minute)
}

// ─── FilterHealthy 核心行为 ───────────────────────────────────────────────

func TestFilterHealthy_AllUntracked_ReturnsAll(t *testing.T) {
	hs := newStore()
	models := makeModels(3)

	result := hs.FilterHealthy(models)
	assert.Equal(t, models, result, "未追踪的模型视为健康，应全量返回")
}

func TestFilterHealthy_UnhealthyExcluded(t *testing.T) {
	hs := newStore()
	models := makeModels(3) // model-1, model-2, model-3

	// 将 model-1 打到不健康（连续失败超过阈值）
	for i := 0; i < hs.failureThreshold; i++ {
		hs.RecordFailure("1")
	}
	status := hs.Get("1")
	require.Equal(t, HealthUnhealthy, status.Status, "前提：model-1 应已变为不健康")

	result := hs.FilterHealthy(models)
	assert.Len(t, result, 2, "不健康的 model-1 应被过滤掉")
	for _, m := range result {
		assert.NotEqual(t, uint(1), m.ModelID, "model-1 不应出现在结果中")
	}
}

func TestFilterHealthy_AllUnhealthy_FallbackToAll(t *testing.T) {
	// 兜底逻辑：全部不健康时退化返回全量，避免服务中断
	hs := newStore()
	models := makeModels(2)

	// 两个模型都打到不健康
	for i := 0; i < hs.failureThreshold; i++ {
		hs.RecordFailure("1")
		hs.RecordFailure("2")
	}
	assert.Equal(t, HealthUnhealthy, hs.Get("1").Status)
	assert.Equal(t, HealthUnhealthy, hs.Get("2").Status)

	result := hs.FilterHealthy(models)
	assert.Equal(t, models, result, "全部不健康时应兜底返回全量，不能返回空列表")
}

// gateway 不再主动探测上游，不健康状态只能靠观察期过期退出。
func TestFilterHealthy_UnhealthyExcludedWithinTTL(t *testing.T) {
	hs := newStore()
	models := makeModels(3)

	for i := 0; i < hs.failureThreshold; i++ {
		hs.RecordFailure("1")
	}
	require.Equal(t, HealthUnhealthy, hs.Get("1").Status)

	result := hs.FilterHealthy(models)
	ids := make([]uint, len(result))
	for i, m := range result {
		ids[i] = m.ModelID
	}
	assert.ElementsMatch(t, []uint{2, 3}, ids, "观察期内的不健康模型应被排除")
}

func TestFilterHealthy_UnhealthyReincludedAfterTTL(t *testing.T) {
	hs := newStore()
	models := makeModels(3)

	for i := 0; i < hs.failureThreshold; i++ {
		hs.RecordFailure("1")
	}
	require.Equal(t, HealthUnhealthy, hs.Get("1").Status)

	// 把状态变更时间推到观察期之前，模拟观察期已过
	hs.mu.Lock()
	hs.data["1"].LastStateChange = time.Now().Add(-2 * hs.unhealthyTTL)
	hs.mu.Unlock()

	result := hs.FilterHealthy(models)
	assert.Len(t, result, 3, "观察期过后应放回候选池，交由真实请求验证")

	// 放回后再失败，必须立刻重新排除——不能因为「刚放回」而漏判
	for i := 0; i < hs.failureThreshold; i++ {
		hs.RecordFailure("1")
	}
	require.Equal(t, HealthUnhealthy, hs.Get("1").Status)
}

func TestFilterHealthy_MixedState(t *testing.T) {
	hs := newStore()
	models := makeModels(4) // 1,2,3,4

	// 打 2 和 4 到不健康，1 和 3 保持健康
	for i := 0; i < hs.failureThreshold; i++ {
		hs.RecordFailure("2")
		hs.RecordFailure("4")
	}

	result := hs.FilterHealthy(models)
	assert.Len(t, result, 2, "只有 model-1 和 model-3 应被保留")
	ids := make([]uint, len(result))
	for i, m := range result {
		ids[i] = m.ModelID
	}
	assert.ElementsMatch(t, []uint{1, 3}, ids)
}

func TestFilterHealthy_EmptyModels_ReturnsEmpty(t *testing.T) {
	hs := newStore()
	result := hs.FilterHealthy([]*ModelRoutingEntry{})
	assert.Empty(t, result)
}

func TestFilterHealthy_ConcurrentSafe(t *testing.T) {
	// 验证 FilterHealthy 在并发读写下不会 panic / data race
	// 需配合 -race 运行
	hs := newStore()
	models := makeModels(5)

	done := make(chan struct{})
	// 并发写（RecordFailure）
	go func() {
		for i := 0; i < 200; i++ {
			hs.RecordFailure(fmt.Sprintf("%d", (i%5)+1))
		}
		close(done)
	}()
	// 并发读（FilterHealthy）
	for i := 0; i < 100; i++ {
		_ = hs.FilterHealthy(models)
	}
	<-done
}
