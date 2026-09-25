package tracesealer_test

import (
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/tracesealer"
	"github.com/stretchr/testify/require"
)

// TestNewRunnerDefaultsMinAgeToThreeHours 默认 min-age 必须等于冻结包的
// defaultSealMinAge（3h）：镜像值偏小就等于缩短了与网关滚动之间的安全差。
func TestNewRunnerDefaultsMinAgeToThreeHours(t *testing.T) {
	r, err := tracesealer.NewRunner(tracesealer.Options{
		Root:      t.TempDir(),
		NewSealer: stubSealer,
	})
	require.NoError(t, err)
	require.Equal(t, 3*time.Hour, r.Options().MinAge)
}

// TestNewRunnerRejectsMinAgeAtOrBelowGatewayRollUpperBound R8：min-age 必须**严格**
// 大于网关滚动上界（1h）。min-age == 1h 也拒绝 —— 边界相等意味着「sealer 可能在网关
// 仍在追加的最后一刻动手」，这不是余量，这是赛跑。
func TestNewRunnerRejectsMinAgeAtOrBelowGatewayRollUpperBound(t *testing.T) {
	for _, d := range []time.Duration{time.Minute, 30 * time.Minute, time.Hour} {
		_, err := tracesealer.NewRunner(tracesealer.Options{
			Root: t.TempDir(), MinAge: d, NewSealer: stubSealer,
		})
		require.ErrorContains(t, err, "必须严格大于网关滚动间隔上界", "min-age=%s 必须被拒", d)
	}
}

// TestNewRunnerRejectsNonPowerOfTwoWindow 窗口要换算成 zstd 的 --long=<log2>，
// 非 2 的幂没有对应参数 ⇒ 必须在这里（启动时）拒绝，而不是几小时后封块时才炸。
func TestNewRunnerRejectsNonPowerOfTwoWindow(t *testing.T) {
	_, err := tracesealer.NewRunner(tracesealer.Options{
		Root: t.TempDir(), ZstdWindowMB: 300, NewSealer: stubSealer,
	})
	require.ErrorContains(t, err, "2 的幂")
}
