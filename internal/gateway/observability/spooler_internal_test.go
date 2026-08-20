package observability

// 本文件是 A 层（包内）：两处被断言的符号不是公开 API，且**无法**从
// `package observability_test` 触达 —— 这是把它们放进包内的唯一理由：
//
//  ① 未导出字段 flushInterval：周期默认值只在构造期决定（≤1m，规格 §7.1）。
//     从公开 API 观测它要么等满 1 分钟，要么让 time.NewTicker 拿到 0 而 panic ——
//     后者是崩溃红，不是断言红，不能作为证据（证据制度要求断言红）。
//  ② 未导出方法 markProbeDue：A5-4 的「到期标志恰好消费一次」需要**确定性地**
//     置一次标志；靠 Run 的 ticker 置标志无法避免竞态（无法断言「恰好一次」）。
//  ③ 未导出纯函数 mountIdentityFor：挂载级身份解析是 **Linux 生产路径**上的代码，
//     而本机（macOS）没有 /proc/self/mountinfo ⇒ 若不在这里用合成内容测，
//     这条 A5-1 的判别逻辑在开发环境中完全没有覆盖。
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

// TestSpoolerDiskWatermarkOptionReachesBuiltGate 钉住内置 Gate 的水位来自**本包的选项**
// （评审 I2 第 ① 条）：Task 6 只需把 `trace_body.disk_watermark_percent` 传进
// `TraceSpoolerOptions.DiskWatermarkPercent`，不必自己注入 Gate 来「尊重配置水位」。
//
// 【为什么断言 Gate 的配置字段而不是行为】水位的**行为**（pct ≥ 水位 ⇒ 闩锁）由 Task 4
// 的用例覆盖；在这一层要验证的是**接线**（选项真的传到了那个被使用的 Gate）。
// 用真实 statfs 断言行为会依赖宿主盘用量 ⇒ 机器相关（正是评审 M5 的教训）。
// 杀手：变异 m23（忽略选项、硬编码 90）与 m24（去掉 0 ⇒ 90 的默认值）。
func TestSpoolerDiskWatermarkOptionReachesBuiltGate(t *testing.T) {
	newSpooler := func(t *testing.T, watermark int) *TraceSpooler {
		t.Helper()
		s, err := NewTraceSpooler(TraceSpoolerOptions{
			Root: t.TempDir(), Inst: "blue", BlockBytes: 1 << 20,
			RollInterval: time.Hour, FlushInterval: time.Minute,
			DiskWatermarkPercent: watermark,
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = s.Close() })
		return s
	}

	require.Equal(t, 42, newSpooler(t, 42).gate.opts.DiskWatermarkPercent,
		"配置的水位必须传到内置 Gate（Task 6 不必自己注入 Gate）")

	// 0 ⇒ 90 不是「锦上添花」：SpoolGate **自己不为水位设默认值**，而 Tick 的未闩锁判据
	// 是 `pct >= DiskWatermarkPercent` ⇒ 传 0 会让**每一次** Tick 都闩锁（0% 水位）。
	require.Equal(t, 90, newSpooler(t, 0).gate.opts.DiskWatermarkPercent,
		"DiskWatermarkPercent=0 必须取默认 90（否则第一次 Tick 就因 0% 水位闩锁）")
}

// TestMountIdentityForPicksLongestPrefixAndSurvivesEscape 钉住挂载级身份的判别逻辑
// （A5-1 的核心）：身份取「覆盖 root 的**最长前缀**挂载项」的 挂载点 + 设备 + 类型。
//
// 【为什么必须有这条】本机（macOS）没有 /proc/self/mountinfo，`defaultRootIdentity`
// 走的是设备级退化分支 ⇒ 挂载级解析在生产（Linux）路径上，只有合成内容能覆盖它。
// 合成内容按 mountinfo 的真实字段顺序写：挂载 ID / 父 ID / major:minor / 根 /
// 挂载点 / 挂载选项 [可选字段…] "-" fstype 源 super-options。
// 杀手：变异 m20（不再选取最长前缀 ⇒ 解析器直接判不出所属挂载）。
func TestMountIdentityForPicksLongestPrefixAndSurvivesEscape(t *testing.T) {
	const mounted = `25 30 0:23 / / rw,relatime - overlay overlay rw,lowerdir=/l
36 25 8:1 / /datalog rw,relatime shared:1 - ext4 /dev/sda1 rw
40 36 8:1 /sub /datalog/trace-archive-v4 rw,relatime - ext4 /dev/sda1 rw
garbage line
`
	key, ok := mountIdentityFor([]byte(mounted), "/datalog/trace-archive-v4/")
	require.True(t, ok, "覆盖 root 的挂载项必须被找到（root 末尾的 / 由 Clean 归一）")
	require.Equal(t, mountKey{mountPoint: "/datalog/trace-archive-v4", dev: "8:1", fsType: "ext4"}, key)

	// 「挂载消失」= 覆盖 root 的挂载项退化成容器根文件系统（overlay）⇒ 最长前缀变了。
	const unmounted = `25 30 0:23 / / rw,relatime - overlay overlay rw,lowerdir=/l
`
	gone, ok := mountIdentityFor([]byte(unmounted), "/datalog/trace-archive-v4")
	require.True(t, ok)
	require.Equal(t, mountKey{mountPoint: "/", dev: "0:23", fsType: "overlay"}, gone)
	require.NotEqual(t, key, gone, "挂载消失必须被判为不同身份，否则 A5-1 无从检测")

	// mountinfo 的八进制转义（空格 = \040）必须还原，否则含空格的挂载点永远匹配不上。
	esc, ok := mountIdentityFor([]byte(`7 1 8:2 / /data\040vol rw - ext4 /dev/sdb1 rw`), "/data vol/x")
	require.True(t, ok)
	require.Equal(t, "/data vol", esc.mountPoint)

	// 【并列挂载点必须与行序无关（评审 M6）】同一路径被多次挂载（overmount）时
	// mountinfo 里会有多条同挂载点的记录；若「先到者胜」，身份就随**行序**变化 ——
	// 而行序不是稳定契约 ⇒ 行序一变身份就变 ⇒ 与启动期基线比较必然不等 ⇒ 永久闩锁
	//（与 m21「不稳定身份」同一类失效）。这里把两条记录**两种顺序**各喂一次，
	// 结果必须相同，且取到的是 tie-break 选出的那条（dev 字典序最大 = 8:2）。
	const dupA = `25 30 0:23 / / rw,relatime - overlay overlay rw
36 25 8:1 / /datalog rw,relatime - ext4 /dev/sda1 rw
41 36 8:2 / /datalog rw,relatime - ext4 /dev/sdb1 rw
`
	const dupB = `25 30 0:23 / / rw,relatime - overlay overlay rw
41 36 8:2 / /datalog rw,relatime - ext4 /dev/sdb1 rw
36 25 8:1 / /datalog rw,relatime - ext4 /dev/sda1 rw
`
	first, ok := mountIdentityFor([]byte(dupA), "/datalog/trace-archive-v4")
	require.True(t, ok)
	second, ok := mountIdentityFor([]byte(dupB), "/datalog/trace-archive-v4")
	require.True(t, ok)
	require.Equal(t, mountKey{mountPoint: "/datalog", dev: "8:2", fsType: "ext4"}, first)
	require.Equal(t, first, second, "并列挂载点的身份必须与 mountinfo 的行序无关（否则永久闩锁）")
}

// TestDefaultRootIdentityIsStableForTheSameDirectory 钉住身份取值的**稳定性**：
// 同一目录、同一挂载点，在时间上必须返回同一个身份。
//
// 【为什么这是承重守卫】身份是与**启动期基线**比较的：只要取值本身会随时间变化，
// 第一次周期复检就会「身份不一致」⇒ 网关被永久闩锁、所有正文变成 body_missing。
// 这是一条比「漏检」更容易发生的失效（任何把时间/随机量/进程状态掺进身份的实现
// 都会命中），故必须有观察者。杀手：变异 m21（把时间掺进身份）。
func TestDefaultRootIdentityIsStableForTheSameDirectory(t *testing.T) {
	root := t.TempDir()
	a, err := defaultRootIdentity(root)
	require.NoError(t, err)
	require.NotEmpty(t, a)
	time.Sleep(2 * time.Millisecond)
	b, err := defaultRootIdentity(root)
	require.NoError(t, err)
	require.Equal(t, a, b, "同一目录的身份必须稳定：否则每次 tick 都会与基线不一致而被闩锁")
}
