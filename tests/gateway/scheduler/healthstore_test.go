package scheduler_test

import (
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/gateway/scheduler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthStore_RecordFailure(t *testing.T) {
	tests := []struct {
		description string
		name        string
		failures    int
		wantStatus  scheduler.HealthStatus
	}{
		{
			description: "连续失败未达阈值时保持Healthy（failureThreshold=5, quickFailThreshold=10）",
			name:        "阈值内保持健康",
			failures:    4,
			wantStatus:  scheduler.HealthHealthy,
		},
		{
			description: "连续失败达阈值后变Unhealthy",
			name:        "达阈值后变不健康",
			failures:    5,
			wantStatus:  scheduler.HealthUnhealthy,
		},
		{
			description: "连续失败超过阈值仍然是Unhealthy",
			name:        "超过阈值仍不健康",
			failures:    8,
			wantStatus:  scheduler.HealthUnhealthy,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// quickFailThreshold 设为 10 避免干扰 failureThreshold 测试
			hs := scheduler.NewHealthStore(5, 3, 10, 10*time.Second)

			for i := 0; i < tc.failures; i++ {
				hs.RecordFailure("model-1")
			}

			status := hs.Get("model-1")
			require.NotNil(t, status)
			assert.Equal(t, tc.wantStatus, status.Status)
			assert.Equal(t, tc.failures, status.ConsecutiveFailures)
		})
	}
}

func TestHealthStore_QuickFail(t *testing.T) {
	t.Run("短时间内多次失败达到quickFailThreshold提前变Unhealthy", func(t *testing.T) {
		hs := scheduler.NewHealthStore(10, 3, 3, 10*time.Second)

		for i := 0; i < 3; i++ {
			hs.RecordFailure("model-qf")
		}

		status := hs.Get("model-qf")
		require.NotNil(t, status)
		assert.Equal(t, scheduler.HealthUnhealthy, status.Status)
		assert.Equal(t, 3, status.ConsecutiveFailures)
	})

	t.Run("Quick-fail窗口重置后重新计数", func(t *testing.T) {
		hs := scheduler.NewHealthStore(10, 3, 3, 1*time.Millisecond)

		hs.RecordFailure("model-qf2")
		hs.RecordFailure("model-qf2")

		time.Sleep(5 * time.Millisecond)

		hs.RecordFailure("model-qf2")

		status := hs.Get("model-qf2")
		require.NotNil(t, status)
		assert.Equal(t, scheduler.HealthHealthy, status.Status)
	})
}

func TestHealthStore_Recovery(t *testing.T) {
	tests := []struct {
		description string
		name        string
		successes   int
		wantStatus  scheduler.HealthStatus
	}{
		{
			description: "Unhealthy后连续成功未达recovery threshold仍为Unhealthy",
			name:        "恢复阈值内仍不健康",
			successes:   2,
			wantStatus:  scheduler.HealthUnhealthy,
		},
		{
			description: "Unhealthy后连续成功达recovery threshold变回Healthy",
			name:        "达恢复阈值后变健康",
			successes:   3,
			wantStatus:  scheduler.HealthHealthy,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hs := scheduler.NewHealthStore(5, 3, 3, 10*time.Second)

			for i := 0; i < 5; i++ {
				hs.RecordFailure("model-r")
			}
			status := hs.Get("model-r")
			require.Equal(t, scheduler.HealthUnhealthy, status.Status)

			for i := 0; i < tc.successes; i++ {
				hs.RecordProbeSuccess("model-r")
			}

			status = hs.Get("model-r")
			assert.Equal(t, tc.wantStatus, status.Status)
		})
	}
}

func TestHealthStore_ProbeSuccess_HealthyModelUnchanged(t *testing.T) {
	t.Run("正常状态多一次成功不改变状态", func(t *testing.T) {
		hs := scheduler.NewHealthStore(5, 3, 3, 10*time.Second)

		hs.RecordProbeSuccess("model-h")

		status := hs.Get("model-h")
		require.NotNil(t, status)
		assert.Equal(t, scheduler.HealthHealthy, status.Status)
		assert.Equal(t, 1, status.ConsecutiveSuccesses)
	})
}

func TestHealthStore_Get(t *testing.T) {
	t.Run("未知模型返回nil", func(t *testing.T) {
		hs := scheduler.NewHealthStore(5, 3, 3, 10*time.Second)
		assert.Nil(t, hs.Get("unknown"))
	})

	t.Run("已知模型返回状态", func(t *testing.T) {
		hs := scheduler.NewHealthStore(5, 3, 3, 10*time.Second)
		hs.RecordFailure("known")
		status := hs.Get("known")
		assert.NotNil(t, status)
	})
}

func TestHealthStore_CleanOrphaned(t *testing.T) {
	tests := []struct {
		description string
		name        string
		models      []string
		activeIDs   []string
		wantKept    []string
		wantRemoved []string
	}{
		{
			description: "CleanOrphaned删除不在活跃列表的条目，保留活跃条目",
			name:        "正常清理孤儿条目",
			models:      []string{"m1", "m2", "m3"},
			activeIDs:   []string{"m1", "m3"},
			wantKept:    []string{"m1", "m3"},
			wantRemoved: []string{"m2"},
		},
		{
			description: "空活跃列表清空所有条目",
			name:        "空活跃列表清空",
			models:      []string{"m1", "m2"},
			activeIDs:   []string{},
			wantKept:    []string{},
			wantRemoved: []string{"m1", "m2"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hs := scheduler.NewHealthStore(5, 3, 3, 10*time.Second)

			for _, m := range tc.models {
				hs.RecordFailure(m)
			}

			hs.CleanOrphaned(tc.activeIDs)

			for _, id := range tc.wantKept {
				assert.NotNil(t, hs.Get(id), "should keep %s", id)
			}
			for _, id := range tc.wantRemoved {
				assert.Nil(t, hs.Get(id), "should remove %s", id)
			}
		})
	}
}

func TestHealthStore_Events(t *testing.T) {
	t.Run("健康事件发布：状态变更时事件正确发出", func(t *testing.T) {
		hs := scheduler.NewHealthStore(3, 2, 10, 10*time.Second)

		ch := hs.Events().Subscribe()

		for i := 0; i < 3; i++ {
			hs.RecordFailure("event-model")
		}

		select {
		case event := <-ch:
			assert.Equal(t, "event-model", event.ModelID)
			assert.Equal(t, scheduler.HealthHealthy, event.OldStatus)
			assert.Equal(t, scheduler.HealthUnhealthy, event.NewStatus)
			assert.Contains(t, event.Reason, "failure threshold")
		case <-time.After(100 * time.Millisecond):
			t.Fatal("expected health event but timed out")
		}

		for i := 0; i < 2; i++ {
			hs.RecordProbeSuccess("event-model")
		}

		select {
		case event := <-ch:
			assert.Equal(t, "event-model", event.ModelID)
			assert.Equal(t, scheduler.HealthUnhealthy, event.OldStatus)
			assert.Equal(t, scheduler.HealthHealthy, event.NewStatus)
			assert.Contains(t, event.Reason, "recovery threshold")
		case <-time.After(100 * time.Millisecond):
			t.Fatal("expected recovery event but timed out")
		}
	})
}

func TestHealthStore_RecordProbeFailure(t *testing.T) {
	t.Run("ProbeFailure达阈值后变Unhealthy", func(t *testing.T) {
		hs := scheduler.NewHealthStore(3, 2, 10, 10*time.Second)

		for i := 0; i < 3; i++ {
			hs.RecordProbeFailure("probe-model")
		}

		status := hs.Get("probe-model")
		require.NotNil(t, status)
		assert.Equal(t, scheduler.HealthUnhealthy, status.Status)
	})
}
