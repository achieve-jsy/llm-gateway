package model

import "testing"

// 默认值集中在 ApplyDefaults，所有调用方都依赖它拿到可用的配置。
// 这条测试锁住「零值补齐」和「显式配置不被覆盖」两个语义。
func TestHealthCheckConfig_ApplyDefaults(t *testing.T) {
	t.Run("零值全部补齐", func(t *testing.T) {
		var c HealthCheckConfig
		c.ApplyDefaults()

		if c.IntervalSeconds != DefaultCheckIntervalSeconds {
			t.Fatalf("IntervalSeconds 应补齐为 %d，实际 %d", DefaultCheckIntervalSeconds, c.IntervalSeconds)
		}
		if c.FailureThreshold != DefaultFailureThreshold {
			t.Fatalf("FailureThreshold 应补齐为 %d，实际 %d", DefaultFailureThreshold, c.FailureThreshold)
		}
		if c.ModelCheckIntervalSeconds != DefaultModelCheckIntervalSeconds {
			t.Fatalf("ModelCheckIntervalSeconds 应补齐，实际 %d", c.ModelCheckIntervalSeconds)
		}
		if c.ModelCheckFailureThreshold != DefaultModelCheckFailureThreshold {
			t.Fatalf("ModelCheckFailureThreshold 应补齐，实际 %d", c.ModelCheckFailureThreshold)
		}
		if c.GroupCheckIntervalSeconds != DefaultGroupCheckIntervalSeconds {
			t.Fatalf("GroupCheckIntervalSeconds 应补齐，实际 %d", c.GroupCheckIntervalSeconds)
		}
		if c.ModelRecoverySuccessThreshold != DefaultModelRecoverySuccessThreshold {
			t.Fatalf("ModelRecoverySuccessThreshold 应补齐，实际 %d", c.ModelRecoverySuccessThreshold)
		}
		if c.TimeoutSeconds != DefaultTimeoutSeconds {
			t.Fatalf("TimeoutSeconds 应补齐，实际 %d", c.TimeoutSeconds)
		}
	})

	t.Run("显式配置不被覆盖", func(t *testing.T) {
		c := HealthCheckConfig{
			IntervalSeconds:               60,
			FailureThreshold:              7,
			ModelCheckIntervalSeconds:     120,
			ModelCheckFailureThreshold:    9,
			GroupCheckIntervalSeconds:     240,
			ModelRecoverySuccessThreshold: 5,
			TimeoutSeconds:                30,
		}
		c.ApplyDefaults()

		if c.IntervalSeconds != 60 || c.FailureThreshold != 7 ||
			c.ModelCheckIntervalSeconds != 120 || c.ModelCheckFailureThreshold != 9 ||
			c.GroupCheckIntervalSeconds != 240 || c.ModelRecoverySuccessThreshold != 5 ||
			c.TimeoutSeconds != 30 {
			t.Fatalf("显式配置被覆盖了：%+v", c)
		}
	})
}
