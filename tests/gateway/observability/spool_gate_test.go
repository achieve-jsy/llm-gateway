package observability_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
	"github.com/stretchr/testify/require"
)

// fakeClock 让闩锁的时间语义（探针周期、累计秒数）可确定性断言。
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// countingUsage 记录 statfs 被调用的次数：闩锁期间**不得**再有文件系统调用。
type countingUsage struct {
	calls int
	pct   float64
	err   error
}

func (u *countingUsage) usage(root string) (float64, error) {
	u.calls++
	return u.pct, u.err
}

type countingProbe struct {
	calls int
	fail  error
}

func (p *countingProbe) probe(root string) error {
	p.calls++
	return p.fail
}

// newGate 用注入的水位取数、探针与时钟构造 SpoolGate：
// 水位与「能不能写」都不来自真实文件系统（测试不依赖真实盘满）。
func newGate(t *testing.T, u *countingUsage, p *countingProbe, clk *fakeClock) *gwobs.SpoolGate {
	t.Helper()
	return gwobs.NewSpoolGate(gwobs.SpoolGateOptions{
		Root:                 "/datalog/trace-archive-v4",
		DiskWatermarkPercent: 90,
		ProbeInterval:        time.Minute,
		Now:                  clk.now,
		UsagePercent:         u.usage,
		ProbeWrite:           p.probe,
	})
}

// TestGateTripsOnDiskWatermark 水位超限 ⇒ 闩锁，且水位与闩锁状态进入可观测快照。
func TestGateTripsOnDiskWatermark(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	u := &countingUsage{pct: 91}
	p := &countingProbe{}
	g := newGate(t, u, p, clk)
	require.True(t, g.Allow())
	g.Tick(clk.now())
	require.False(t, g.Allow(), "水位 91%% > 90%% ⇒ 必须闩锁")
	require.True(t, g.Latched())
	require.Contains(t, g.Reason(), "watermark")
	require.Equal(t, int64(1), g.Stats().Trips)

	// §9.3：降级必须有明确可观测信号 ⇒ 状态快照里的字段也必须反映本次跳变。
	stats := g.Stats()
	require.True(t, stats.Latched)
	require.Equal(t, 91.0, stats.UsagePercent, "最近一次观测到的水位必须写入快照")
	require.Contains(t, stats.Reason, "watermark")
}

// TestGateWatermarkBoundaryIsInclusive 钉住阈值的**闭区间**语义（规格 R9
// 「达到或超过水位线」）：恰好等于 `DiskWatermarkPercent` ⇒ 必须闩锁；
// 低于 ⇒ 必须不闩锁。此前只用 91%%/95%% 与 10%% 这类远离阈值的数，
// 「>= 写成 >」这类边界错法测不出来。
func TestGateWatermarkBoundaryIsInclusive(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	u := &countingUsage{pct: 89.9}
	g := newGate(t, u, &countingProbe{}, clk)

	g.Tick(clk.now())
	require.True(t, g.Allow(), "水位 89.9%% < 90%% ⇒ 不得闩锁")
	require.Equal(t, int64(0), g.Stats().Trips)

	u.pct = 90 // 恰好等于阈值
	g.Tick(clk.now())
	require.False(t, g.Allow(), "恰好等于阈值必须闩锁（阈值是闭区间，不是严格大于）")
	require.Equal(t, int64(1), g.Stats().Trips)
	require.Contains(t, g.Reason(), "watermark")
}

// TestGateTripIsIdempotentAndReportedOnce 跳变只报一次：Trip 的返回值是
// 「本次是否跳变」，日志据此只打一条（否则每行一次 ERROR 会淹没日志）。
func TestGateTripIsIdempotentAndReportedOnce(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	g := newGate(t, &countingUsage{pct: 0}, &countingProbe{}, clk)
	require.True(t, g.Trip("append_failed", errors.New("no space")))
	require.False(t, g.Trip("append_failed", errors.New("no space")), "第二次不得再报跳变")
	require.Equal(t, int64(1), g.Stats().Trips)
	// §9.3 的唯一条 ERROR 日志要能说出「为什么」⇒ reason 必须带上底层 cause。
	require.Contains(t, g.Reason(), "append_failed")
	require.Contains(t, g.Reason(), "no space")
}

// TestGateLatchedDoesNoFilesystemWork 闩锁期间：Allow() 为假、statfs 与探针
// 都**只在到点那一刻**调用一次。这一条就是 R9：「不要每个请求都重试」。
func TestGateLatchedDoesNoFilesystemWork(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	u := &countingUsage{pct: 95}
	p := &countingProbe{}
	g := newGate(t, u, p, clk)
	g.Tick(clk.now())
	require.False(t, g.Allow())
	usageAfterTrip, probeAfterTrip := u.calls, p.calls

	// 闩锁期间大量请求：只读 Allow()，不得触发任何文件系统调用
	for i := 0; i < 1000; i++ {
		require.False(t, g.Allow())
	}
	require.Equal(t, usageAfterTrip, u.calls, "Allow() 不得触发 statfs")
	require.Equal(t, probeAfterTrip, p.calls, "Allow() 不得触发探针")

	// 未到周期：Tick 也不得探针
	clk.advance(30 * time.Second)
	g.Tick(clk.now())
	require.Equal(t, probeAfterTrip, p.calls, "未到探针周期不得探针")
}

// TestGateProbeRespectsInterval 是本任务**唯一**专门盯探针周期的用例
// （集合里的 m04「探针周期改为 0」必须杀死它）。
// 它与 TestGateLatchedDoesNoFilesystemWork 的区别：后者断言的主语是「Allow() 不做 IO」，
// 周期只是顺带检查；这条把「未到周期不得探针 / 到周期恰好探一次 / 距上次不足周期不得重复」
// 做成显式三段，使「摘掉周期判断」这一类变异有唯一归属的用例，
// 而不是靠一条复合用例转红来推断（复合用例的遮蔽效应见全局约束的证据制度第 3 条）。
func TestGateProbeRespectsInterval(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	u := &countingUsage{pct: 95}
	p := &countingProbe{}
	g := newGate(t, u, p, clk)
	g.Tick(clk.now()) // 未闩锁 → 水位超限 ⇒ 闩锁，并记下 lastProbe
	require.False(t, g.Allow())
	afterTrip := p.calls

	clk.advance(59 * time.Second)
	g.Tick(clk.now())
	require.Equal(t, afterTrip, p.calls, "未到 ProbeInterval（59s < 60s）不得探针")

	clk.advance(2 * time.Second) // 累计 61s > 60s
	g.Tick(clk.now())
	require.Equal(t, afterTrip+1, p.calls, "到 ProbeInterval 必须恰好探针一次")

	clk.advance(1 * time.Second) // 距上次探针仅 1s
	g.Tick(clk.now())
	require.Equal(t, afterTrip+1, p.calls, "距上次探针不足周期不得重复探针")
}

// TestGateTripStartsProbeInterval 通过 Trip 进入闩锁后，第一次探针同样要等满一个
// ProbeInterval：写入**刚刚**失败就立刻再探一次没有意义，闩锁的意义正是把
// 「每请求重试」变成「每周期一次」。⇒ Trip 必须同时记下 latchedAt 与 lastProbe。
func TestGateTripStartsProbeInterval(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	p := &countingProbe{fail: errors.New("read-only file system")}
	g := newGate(t, &countingUsage{pct: 0}, p, clk) // 水位 0 ⇒ 不会由水位闩锁

	require.True(t, g.Trip("append_failed", errors.New("no space")))
	require.False(t, g.Allow())

	clk.advance(30 * time.Second)
	g.Tick(clk.now())
	require.Equal(t, 0, p.calls, "Trip 之后未满一个 ProbeInterval 不得探针")

	clk.advance(31 * time.Second) // 距 Trip 累计 61s
	g.Tick(clk.now())
	require.Equal(t, 1, p.calls, "Trip 之后满一个 ProbeInterval 必须探针一次")

	clk.advance(1 * time.Second)
	g.Tick(clk.now())
	require.Equal(t, 1, p.calls, "距上次探针不足周期不得重复探针")
}

// TestGateProbeFailureKeepsLatched 探针失败 ⇒ 保持闩锁，并计入 ProbeFailures
// 与本次闩锁已持续秒数（§9.3 的降级计数器）。
func TestGateProbeFailureKeepsLatched(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	p := &countingProbe{fail: errors.New("read-only file system")}
	u := &countingUsage{pct: 95}
	g := newGate(t, u, p, clk)
	g.Tick(clk.now())
	clk.advance(2 * time.Minute)
	g.Tick(clk.now())
	require.False(t, g.Allow())
	require.Equal(t, 1, p.calls)
	require.Equal(t, int64(1), g.Stats().ProbeFailures)
	require.Equal(t, int64(120), g.Stats().LatchedSeconds, "本次闩锁已持续 120s")
}

// TestGateProbeSuccessWithHighWatermarkKeepsLatched 恢复必须**同时**满足
// 「探针可写」与「水位已回落」：探针成功但水位仍超限 ⇒ 不得解锁。
// （只满足一个条件就解锁会让闩锁在盘满时反复抖动，且把恢复条件从显式退化成隐式。）
func TestGateProbeSuccessWithHighWatermarkKeepsLatched(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	u := &countingUsage{pct: 95}
	p := &countingProbe{} // 探针成功
	g := newGate(t, u, p, clk)
	g.Tick(clk.now())
	require.False(t, g.Allow())

	clk.advance(2 * time.Minute)
	g.Tick(clk.now()) // 探针成功，但水位仍是 95% ≥ 90%
	require.False(t, g.Allow(), "探针成功不能单独解锁：水位未回落 ⇒ 必须保持闩锁")
	require.Equal(t, 1, p.calls)
	// 这一条出口（探针成功、水位未回落）自己也要被观察：它同样计入 ProbeFailures，
	// 否则该出口的计数恒错也全绿（对应变异 m19）。
	require.Equal(t, int64(1), g.Stats().ProbeFailures, "水位未回落同样计入探针失败")
}

// TestGateProbeSuccessUnlatches 恢复条件必须显式：探针成功（且水位低于阈值）⇒ 解锁，
// 且解锁后状态清零（reason 不得残留、本次闩锁秒数归零）。
func TestGateProbeSuccessUnlatches(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	u := &countingUsage{pct: 95}
	p := &countingProbe{}
	g := newGate(t, u, p, clk)
	g.Tick(clk.now())
	require.False(t, g.Allow())

	u.pct = 50 // 空间已释放
	clk.advance(2 * time.Minute)
	g.Tick(clk.now())
	require.True(t, g.Allow(), "探针成功且水位已回落 ⇒ 必须解锁")
	require.False(t, g.Latched())
	require.Empty(t, g.Reason(), "解锁后不得残留上一次闩锁的原因")
	require.Equal(t, int64(0), g.Stats().LatchedSeconds, "解锁后本次闩锁秒数归零")
}

// TestGateWatermarkAloneDoesNotUnlatch：水位落回但探针仍失败 ⇒ 保持闩锁。
// 「水位」与「可写」是两件事：水位可能因别的进程写入而回落，但本进程仍无权限。
func TestGateWatermarkAloneDoesNotUnlatch(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	u := &countingUsage{pct: 95}
	p := &countingProbe{fail: errors.New("permission denied")}
	g := newGate(t, u, p, clk)
	g.Tick(clk.now())
	u.pct = 10
	clk.advance(2 * time.Minute)
	g.Tick(clk.now())
	require.False(t, g.Allow())
}

// TestGateUsageErrorDoesNotLatch：statfs 失败本身**不**闩锁。
// 「读不到水位」与「盘满」是两件事：把权限/路径问题伪装成磁盘问题会让运维追错方向；
// 真正的判据是「能不能写」，由 Append 失败或探针来定（见 Tick 的注释）。
func TestGateUsageErrorDoesNotLatch(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	u := &countingUsage{err: errors.New("statfs: no such file or directory")}
	g := newGate(t, u, &countingProbe{}, clk)
	require.True(t, g.Allow())
	g.Tick(clk.now())
	require.True(t, g.Allow(), "statfs 失败不得闩锁")
	require.Equal(t, int64(0), g.Stats().Trips)
	require.Empty(t, g.Reason())
}

// TestGateProbeIntervalDefaultsToOneMinute：ProbeInterval 未设置（0）时必须回落到 1m。
// 否则闩锁的节流作用消失：「每请求重试」退化成「每 flush tick 探针一次」。
func TestGateProbeIntervalDefaultsToOneMinute(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	u := &countingUsage{pct: 95}
	p := &countingProbe{}
	g := gwobs.NewSpoolGate(gwobs.SpoolGateOptions{
		Root:                 "/datalog/trace-archive-v4",
		DiskWatermarkPercent: 90,
		// ProbeInterval 故意留零值：必须由 NewSpoolGate 回落到 1m
		Now:          clk.now,
		UsagePercent: u.usage,
		ProbeWrite:   p.probe,
	})
	g.Tick(clk.now())
	require.False(t, g.Allow())
	afterTrip := p.calls

	clk.advance(30 * time.Second)
	g.Tick(clk.now())
	require.Equal(t, afterTrip, p.calls, "默认周期 1m：30s 时不得探针")

	clk.advance(31 * time.Second) // 累计 61s ≥ 60s
	g.Tick(clk.now())
	require.Equal(t, afterTrip+1, p.calls, "默认周期 1m：61s 时必须探针一次")
}

// TestGateDefaultProbeDoesNotCreateRoot 走**真实**默认实现（defaultUsagePercent /
// defaultProbeWrite，不用注入），钉住 D49 的硬约束：根目录不存在 ⇒ 探针必须失败并
// 保持闩锁，且**绝不得把根目录建出来**。
//
// 【为什么这条必须走真实文件系统】「探针不得创建目录」这条性质只有在根目录真的
// 不存在时才可观察；注入的假探针永远观察不到它。用 t.TempDir() 下的**不存在**路径
// （父目录存在），结果与宿主盘剩多少空间无关 ⇒ 不会假红。
func TestGateDefaultProbeDoesNotCreateRoot(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	missing := filepath.Join(t.TempDir(), "datalog-not-mounted")
	g := gwobs.NewSpoolGate(gwobs.SpoolGateOptions{
		Root:                 missing,
		DiskWatermarkPercent: 90,
		Now:                  clk.now, // UsagePercent / ProbeWrite 留空 ⇒ 用真实默认实现
	})

	// 未闩锁：真实 statfs 对不存在的目录报错 ⇒ 不闩锁（判据是「能不能写」而不是「读得到水位」）。
	g.Tick(clk.now())
	require.True(t, g.Allow())

	require.True(t, g.Trip("root_missing", errors.New("statfs failed")))
	clk.advance(2 * time.Minute)
	g.Tick(clk.now()) // 探针：<root> 不存在 ⇒ 必须失败 ⇒ 保持闩锁
	require.False(t, g.Allow(), "探针失败 ⇒ 保持闩锁")
	require.Equal(t, int64(1), g.Stats().ProbeFailures)

	_, statErr := os.Stat(missing)
	require.True(t, os.IsNotExist(statErr),
		"探针绝不得创建根目录：建出来会把「运行中挂载消失」在容器可写层掩盖掉（D49）")
}
