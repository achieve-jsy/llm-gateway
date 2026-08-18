package observability

// 本文件是 A 层（包内）：两处被断言的符号不是公开 API，且**无法**从
// `package observability_test` 触达 —— 这是把它们放进包内的唯一理由：
//
//  ① 未导出字段 flushInterval：周期默认值只在构造期决定（≤1m，规格 §7.1）。
//     从公开 API 观测它要么等满 1 分钟，要么让 time.NewTicker 拿到 0 而 panic ——
//     后者是崩溃红，不是断言红，不能作为证据（证据制度要求断言红）。
//  ② 未导出方法 markProbeDue：A5-4 的「到期标志恰好消费一次」需要**确定性地**
//     置一次标志；靠 Run 的 ticker 置标志无法避免竞态（无法断言「恰好一次」）。
//
// 其余全部行为都由 B 层（tests/gateway/observability/spooler_test.go）覆盖。

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestSpoolerFlushIntervalDefaultsToOneMinute 钉住构造期的周期默认值：
// 未设置（0）时必须是 1 分钟。规格 §7.1 要求 flush 周期 ≤1m，而 Run 的 ticker
// 拿到的必须是正数（time.NewTicker 对 0/负值直接 panic），因此这个默认值是
// 「≤1m 且可运行」的唯一落点。杀手：变异 m15（把默认赋值摘掉）。
func TestSpoolerFlushIntervalDefaultsToOneMinute(t *testing.T) {
	s, err := NewTraceSpooler(TraceSpoolerOptions{
		Root: t.TempDir(), Inst: "blue", BlockBytes: 1 << 20,
		RollInterval: time.Hour,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.Equal(t, time.Minute, s.flushInterval,
		"FlushInterval 未设置时必须取 1 分钟（规格 §7.1 ≤1m）")
}

// TestSpoolerDueProbeIsConsumedExactlyOnce 钉住 A5-4 的标志语义：**一个**「到期」
// 标志恰好换来**一次** gate.Tick。这里手工置标志（不启动 Run），因此判定是确定性的。
//
// 观测手段是注入的水位取数**调用次数**：未闩锁时 Tick 只评估一次水位，所以
// 「标志没被消掉」的实现会在之后每次 AppendLine 都重复 Tick（水位调用次数随
// AppendLine 次数增长）。杀手：变异 m18（把原子交换换成只读）。
func TestSpoolerDueProbeIsConsumedExactlyOnce(t *testing.T) {
	root := t.TempDir()
	var usageCalls atomic.Int32
	s, err := NewTraceSpooler(TraceSpoolerOptions{
		Root: root, Inst: "blue", BlockBytes: 1 << 20,
		RollInterval: time.Hour, FlushInterval: time.Minute,
		Gate: NewSpoolGate(SpoolGateOptions{
			Root:                 root,
			DiskWatermarkPercent: 90,
			ProbeInterval:        time.Nanosecond,
			UsagePercent: func(string) (float64, error) {
				usageCalls.Add(1)
				return 50, nil // 水位正常：Tick 只评估水位、不闩锁、不探针
			},
			ProbeWrite: func(string) error { return errors.New("未闩锁时不得探针") },
		}),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	s.markProbeDue(time.Now())
	_, err = s.AppendLine("2026-09-24", []byte(`{"a":1}`))
	require.NoError(t, err)
	require.Equal(t, int32(1), usageCalls.Load(), "到期标志必须被消费一次")

	// 没有新的到期标志：不得再 Tick（否则每次 AppendLine 都重放一次水位评估/探针）。
	_, err = s.AppendLine("2026-09-24", []byte(`{"a":2}`))
	require.NoError(t, err)
	_, err = s.AppendLine("2026-09-24", []byte(`{"a":3}`))
	require.NoError(t, err)
	require.Equal(t, int32(1), usageCalls.Load(), "到期标志必须恰好消费一次，不得重复 Tick")
}
