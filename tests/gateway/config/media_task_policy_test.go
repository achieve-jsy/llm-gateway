package config_test

import (
	"testing"

	"github.com/example/llm-gateway/internal/gateway/config"
	"github.com/stretchr/testify/require"
)

// 媒体任务平台的运行策略（C7）配置测试。
//
// 为什么这些配置值得钉死：
//   - MEDIA_TASK_TIMEOUT_MINUTES 决定「任务多久被判超时」：配成 0 或负数会让
//     每个任务在第一次查询时就被判超时（对外一律 failed），是**可用性事故**；
//   - MEDIA_POLL_MAX_FAILURES 决定「上游连续失败多少次后放弃」：配错方向会导致
//     要么过早放弃（用户任务还在跑却被判失败），要么永不放弃（持续打一个坏渠道）。
//
// 覆盖：默认值、环境变量覆盖、非法值回落（环境变量 > 代码默认值，YAML 层没有这两项）。
func TestMediaConfig_TaskPolicyDefaults(t *testing.T) {
	cfg := config.Load()
	require.Equal(t, 30, cfg.Media.TaskTimeoutMinutes, "MEDIA_TASK_TIMEOUT_MINUTES 默认 30 分钟")
	require.Equal(t, 20, cfg.Media.PollMaxFailures, "MEDIA_POLL_MAX_FAILURES 默认 20 次")
}

func TestMediaConfig_TaskPolicyEnvOverride(t *testing.T) {
	t.Setenv("MEDIA_TASK_TIMEOUT_MINUTES", "45")
	t.Setenv("MEDIA_POLL_MAX_FAILURES", "7")

	cfg := config.Load()
	require.Equal(t, 45, cfg.Media.TaskTimeoutMinutes)
	require.Equal(t, 7, cfg.Media.PollMaxFailures)
}

func TestMediaConfig_TaskPolicyInvalidValuesFallBack(t *testing.T) {
	cases := []struct {
		name     string
		timeout  string
		failures string
	}{
		{"零值", "0", "0"},
		{"负数", "-5", "-1"},
		{"非数字", "abc", "xyz"},
		{"空串", "", ""},
		{"小数", "1.5", "2.7"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MEDIA_TASK_TIMEOUT_MINUTES", tc.timeout)
			t.Setenv("MEDIA_POLL_MAX_FAILURES", tc.failures)
			cfg := config.Load()
			require.Equal(t, 30, cfg.Media.TaskTimeoutMinutes, "非法超时值必须回落到默认 30")
			require.Equal(t, 20, cfg.Media.PollMaxFailures, "非法失败上限必须回落到默认 20")
		})
	}
}
