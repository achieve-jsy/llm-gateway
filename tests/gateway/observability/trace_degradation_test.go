package observability_test

// 【本文件为什么在 B 层（package observability_test）】
// 它只用 observability / gateway-config 的**公开 API**（TraceWriter / TraceBodySink /
// TraceSpooler / SpoolGate），不需要触达未导出符号 ⇒ 按测试分层（规格 §14.1、
// docs/guardrails.md Q3）放在 tests/（与 trace_event_spool_test.go 同层）。
//
// 【本文件覆盖的要求】
//   - 规格 §9.3：闩锁跳变**恰好一条** ERROR、闩锁期间**零条**、恢复一条 INFO、
//     闩锁期间按 SummaryInterval 一条汇总（绝不每行一条 —— 2026-09-24 的 / 盘写满
//     正是失败路径全量打日志造成的，244 GB/天）；
//   - 增补 A7-1：`SpoolGate.Tick` 自行置位的**水位**闩锁（`disk_watermark`，
//     spool_gate.go 的未闩锁分支）原先没有任何调用方消费 ⇒ 必须被检测并打日志，
//     且**不得**与 Task 5 已在 `Trip` 返回值上打过的那三类（root-identity-mismatch /
//     root_unavailable / append_failed）重复；
//   - 增补 A7-2：周期汇总的六个闸门字段（Latched / LatchedSeconds / Trips /
//     ProbeFailures / UsagePercent / reason）；
//   - 增补 A7-3 / D51：被拒绝/闩锁的行 ⇒ body_missing=true、定位键全 NULL、
//     不触盘、不使用冻结包的 AppendFailed。
//
// 【两条互补的水位路径（互不遮蔽）】闸门可观测时走「GateStats().Trips（+ Latched/Reason）
// 前后比对」的周期检测；闸门不可观测时（例如只实现 TraceBodySink 三方法的接线）走
// `AppendLine` 上报的错误文本。两条路径打印的详情形态刻意不同
//（前者带 Stats 读数 `usage_percent=`，后者带错误原文），因此各自有**隔离**的杀手。

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gwconfig "github.com/example/llm-gateway/internal/gateway/config"
	gwobs "github.com/example/llm-gateway/internal/gateway/observability"
	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	ta "github.com/example/llm-gateway/internal/tracearchive"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// 本文件私有的测试替身（同包的 trace_event_spool_test.go 已定义 orderRecorder /
// fakeSink / sinkWithDayOf / newSpoolTestDB / cstCrossDayTS，这里一律复用，
// 不重复定义 —— 同包重名会编译失败）。
// ---------------------------------------------------------------------------

// logCapture 收集写入的降级日志行。所有日志断言都逐条打在**注入的 Logf** 上，
// 不看真实标准错误输出：§9.3 要的是「恰好一条」，而 grep 真实日志既拿不到
// 「闩锁期间零条」的确定性，也会把别处的日志混进来。
type logCapture struct {
	mu    sync.Mutex
	lines []string
}

func (c *logCapture) logf(format string, args ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, fmt.Sprintf(format, args...))
}

func (c *logCapture) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.lines...)
}

// countContaining 返回**含该子串的行数**（不是出现次数）：跳变日志「恰好一条」的
// 判据是行数。
func (c *logCapture) countContaining(sub string) int {
	n := 0
	for _, l := range c.snapshot() {
		if strings.Contains(l, sub) {
			n++
		}
	}
	return n
}

// firstContaining 返回第一条含该子串的日志（没有则失败）。
func (c *logCapture) firstContaining(t *testing.T, sub string) string {
	t.Helper()
	for _, l := range c.snapshot() {
		if strings.Contains(l, sub) {
			return l
		}
	}
	require.Failf(t, "没有找到含该子串的日志", "子串=%q 全部日志=%v", sub, c.snapshot())
	return ""
}

// testClock 是可在**运行中**推进的注入时钟。
//
// 【为什么不能照简报那样捕获一个普通变量】简报的 `now := time.Unix(0,0)` 由测试协程
// 在运行中赋值、写协程并发读 ⇒ data race（`go test -race` 会直接报，且推进时机不可控）。
// 这里用原子纳秒，读写都无锁。
type testClock struct{ nanos atomic.Int64 }

func newTestClock(t time.Time) *testClock {
	c := &testClock{}
	c.nanos.Store(t.UnixNano())
	return c
}

func (c *testClock) Now() time.Time { return time.Unix(0, c.nanos.Load()) }

func (c *testClock) Advance(d time.Duration) { c.nanos.Add(int64(d)) }

// switchableSink 是「失败状态可在运行中切换」的落盘替身：恢复用例要表达
// 「空间已释放」。fakeSink 的 err 字段是无锁读的、且只在 Run 之前设置，
// 用它做运行中切换会产生数据竞争。
type switchableSink struct {
	mu  sync.Mutex
	err error
	day gwconfig.TraceBodyConfig
}

func (s *switchableSink) AppendLine(dt string, line []byte) (ta.Ref, error) {
	s.mu.Lock()
	err := s.err
	s.mu.Unlock()
	if err != nil {
		return ta.Ref{}, err
	}
	return ta.Ref{DT: dt, Inst: "blue", Part: 0, Row: 0}, nil
}

func (s *switchableSink) setErr(err error) {
	s.mu.Lock()
	s.err = err
	s.mu.Unlock()
}

func (s *switchableSink) DayStart(ts time.Time) time.Time { return s.day.DayStart(ts) }

func (s *switchableSink) DayOf(ts time.Time) string { return s.day.DayOf(ts) }

// sinkWithGateStats 是「闸门可观测」的组装物：在 Task 6 的 sinkWithDayOf（真实
// *TraceSpooler 的 AppendLine + 生产日期入口）之上多暴露一个只读 GateStats()。
//
// 【为什么与生产同形】cmd/gateway/main.go 的 traceBodySink 已实现同一个访问器
// （`GateStats() gwobs.GateStats { return s.spooler.Gate().Stats() }`，计划 2 任务 7），
// 因此本类型就是「生产接线 + 真实 spooler」。
type sinkWithGateStats struct{ *sinkWithDayOf }

func (s *sinkWithGateStats) GateStats() gwobs.GateStats { return s.spooler.Gate().Stats() }

// plainTraceBodySink 与只实现 TraceBodySink 三方法的接线**同形**（不暴露 GateStats）：
// 用来证明「闸门不可观测」时水位闩锁的跳变依然被检测并打出一条 ERROR —— 这是
// 错误文本兜底路径的生产级证据，也覆盖访问器接线缺失时的行为。
type plainTraceBodySink struct{ spooler *gwobs.TraceSpooler }

func (s plainTraceBodySink) AppendLine(dt string, line []byte) (ta.Ref, error) {
	return s.spooler.AppendLine(dt, line)
}

func (s plainTraceBodySink) DayStart(ts time.Time) time.Time {
	return gwconfig.TraceBodyConfig{}.DayStart(ts)
}

func (s plainTraceBodySink) DayOf(ts time.Time) string {
	return gwconfig.TraceBodyConfig{}.DayOf(ts)
}

// newWatermarkTripSpooler 构造一个**必然在水位分支闩锁**的真实 spooler：
// 注入 UsagePercent = 99（水位线 90）与极短的 ProbeInterval。
//
// 【为什么不依赖宿主】断言「水位超限 ⇒ 闩锁」绝不能读真实 statfs：宿主磁盘用量
// <90% 时用例假红、>90% 时别的「能解锁」用例假绿。水位与探针一律注入。
func newWatermarkTripSpooler(t *testing.T, root string) *gwobs.TraceSpooler {
	t.Helper()
	gate := gwobs.NewSpoolGate(gwobs.SpoolGateOptions{
		Root:                 root,
		DiskWatermarkPercent: 90,
		ProbeInterval:        time.Millisecond,
		UsagePercent:         func(string) (float64, error) { return 99, nil },
	})
	spooler, err := gwobs.NewTraceSpooler(gwobs.TraceSpoolerOptions{
		Root: root, Inst: "blue", BlockBytes: 1 << 20,
		RollInterval: time.Hour, FlushInterval: 5 * time.Millisecond, Gate: gate,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = spooler.Close() })
	return spooler
}

// listSpoolParts 列出 root 下所有 spool part 文件（冻结包的命名 part-NNNNNN.jsonl）。
// 用于断言「闩锁在建块之前就返回」——闩锁中不得产出任何 part。
func listSpoolParts(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasPrefix(d.Name(), "part-") {
			out = append(out, path)
		}
		return nil
	})
	require.NoError(t, err)
	return out
}

// ---------------------------------------------------------------------------
// 简报的三条用例（规格 §9.3 的基本信号）
// ---------------------------------------------------------------------------

// TestDegradationLogsLatchExactlyOnce：闩锁跳变必须**恰好一条** ERROR，而不是每一行
// 一条 —— 否则盘满时日志自己的写入会把盘写得更满（2026-09-24 那次 `/` 盘被写满
// 正是「失败路径打全量日志」造成的，事后用 log_min_error_statement='panic' 才止住）。
func TestDegradationLogsLatchExactlyOnce(t *testing.T) {
	rec := &orderRecorder{}
	db, _ := newSpoolTestDB(t, rec)
	capture := &logCapture{}
	sink := &fakeSink{rec: rec, day: gwconfig.TraceBodyConfig{}, err: fmt.Errorf("%w", gwobs.ErrSpoolLatched)}

	w := gwobs.NewTraceWriterWithSink(db, 1024, 1, time.Hour, gwobs.TraceWriterLimits{}, sink)
	w.ConfigureDegradation(gwobs.DegradationOptions{
		Logf: capture.logf, Now: newTestClock(time.Unix(0, 0)).Now, SummaryInterval: 5 * time.Minute,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	for i := 0; i < 20; i++ {
		w.Emit(gwobs.TraceEvent{RequestID: fmt.Sprintf("req-%d", i), TS: time.Now(), RequestBodyRaw: []byte(`{"a":1}`)})
	}
	require.Eventually(t, func() bool {
		return w.DegradationStats().MissingRows == 20
	}, 5*time.Second, 10*time.Millisecond)

	require.Equal(t, 1, capture.countContaining("闩锁"), "跳变日志必须恰好一条：%v", capture.snapshot())
	require.Equal(t, 1, capture.countContaining("降级为 body_missing"), "只允许一条进入降级的 ERROR")
	st := w.DegradationStats()
	require.True(t, st.Latched)
	require.Equal(t, int64(20), st.MissingRows)
	require.Equal(t, int64(20), st.SpoolFailures)
	require.Equal(t, int64(1), st.Trips, "闩锁跳变计数器必须恰好为 1（全局约束要求暴露闩锁次数）")
}

// TestDegradationLogsRecovery：恢复（重新可写）也必须有一条明确信号
// （Task 5 没有恢复日志，这一条是 §9.3「降级有明确信号」的另一半）。
func TestDegradationLogsRecovery(t *testing.T) {
	rec := &orderRecorder{}
	db, _ := newSpoolTestDB(t, rec)
	capture := &logCapture{}
	sink := &switchableSink{err: fmt.Errorf("%w", gwobs.ErrSpoolLatched), day: gwconfig.TraceBodyConfig{}}

	w := gwobs.NewTraceWriterWithSink(db, 1024, 1, time.Hour, gwobs.TraceWriterLimits{}, sink)
	w.ConfigureDegradation(gwobs.DegradationOptions{Logf: capture.logf})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	w.Emit(gwobs.TraceEvent{RequestID: "req-1", TS: time.Now()})
	require.Eventually(t, func() bool { return w.DegradationStats().Latched }, 5*time.Second, 10*time.Millisecond)

	sink.setErr(nil) // 空间已释放
	w.Emit(gwobs.TraceEvent{RequestID: "req-2", TS: time.Now()})
	require.Eventually(t, func() bool { return !w.DegradationStats().Latched }, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, 1, capture.countContaining("恢复"), "恢复必须有且只有一条信号：%v", capture.snapshot())
}

// TestDegradationSummaryIsThrottled：闩锁期间按 SummaryInterval 输出汇总，
// 而不是每个 flush 一条（汇总由周期性分支驱动，时间由注入时钟推进）。
func TestDegradationSummaryIsThrottled(t *testing.T) {
	rec := &orderRecorder{}
	db, _ := newSpoolTestDB(t, rec)
	capture := &logCapture{}
	clock := newTestClock(time.Unix(0, 0))
	sink := &fakeSink{rec: rec, day: gwconfig.TraceBodyConfig{}, err: fmt.Errorf("%w", gwobs.ErrSpoolLatched)}

	w := gwobs.NewTraceWriterWithSink(db, 1024, 1, 20*time.Millisecond, gwobs.TraceWriterLimits{}, sink)
	w.ConfigureDegradation(gwobs.DegradationOptions{
		Logf: capture.logf, Now: clock.Now, SummaryInterval: time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	for i := 0; i < 5; i++ {
		w.Emit(gwobs.TraceEvent{RequestID: fmt.Sprintf("req-%d", i), TS: time.Now()})
	}
	require.Eventually(t, func() bool { return w.DegradationStats().MissingRows == 5 }, 5*time.Second, 10*time.Millisecond)
	time.Sleep(200 * time.Millisecond) // 期间会跑很多个 flush 周期
	require.LessOrEqual(t, capture.countContaining("汇总"), 1, "未到 SummaryInterval 不得重复输出汇总")

	clock.Advance(2 * time.Hour)
	require.Eventually(t, func() bool { return capture.countContaining("汇总") >= 1 }, 5*time.Second, 10*time.Millisecond)
}

// ---------------------------------------------------------------------------
// A7-1：水位闩锁的跳变日志（SpoolGate.Tick 自行置位、原先零日志的那一条路径）
// ---------------------------------------------------------------------------

// TestDegradationLogsWatermarkLatchExactlyOnce 是 A7-1 的**主证据**：走
// 真实 `TraceSpooler` + 真实 `Run`（它只置「探针到期」标志）+ 真实 `applyDueTick`
// + **真实 `SpoolGate.Tick` 的水位分支**（注入 UsagePercent=99），因此闸门确实是由
// `g.latched = true; g.trips++` 那两行闩锁的 —— 不是用 `Trip` 手工构造的前置状态。
//
// 断言：① 水位跳变恰好一条 ERROR（含 Stats 读数与水位读数）；② 其后闩锁的每一行
// 零条日志；③ 闩锁行 body_missing=true、定位键全 NULL、不产出任何 spool part（A7-3/D51）。
func TestDegradationLogsWatermarkLatchExactlyOnce(t *testing.T) {
	root := t.TempDir()
	spooler := newWatermarkTripSpooler(t, root)
	require.False(t, spooler.Gate().Latched(), "前置条件：闸门初始未闩锁")

	rec := &orderRecorder{}
	db, _ := newSpoolTestDB(t, rec)
	sink := &sinkWithGateStats{&sinkWithDayOf{spooler: spooler, day: gwconfig.TraceBodyConfig{}}}
	capture := &logCapture{}

	w := gwobs.NewTraceWriterWithSink(db, 1024, 1, 20*time.Millisecond, gwobs.TraceWriterLimits{}, sink)
	w.ConfigureDegradation(gwobs.DegradationOptions{Logf: capture.logf, SummaryInterval: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)
	go spooler.Run(ctx)

	// 等到周期 tick 真的跑过一次：它置「探针到期」标志，而水位评估在写协程的
	// AppendLine 入口执行（A5-4）⇒ 第一条事件的追加必然先触发水位分支，而不是先落盘。
	require.Eventually(t, func() bool { return spooler.Stats().Flushes >= 1 }, 5*time.Second, time.Millisecond)

	for i := 0; i < 5; i++ {
		w.Emit(gwobs.TraceEvent{RequestID: fmt.Sprintf("wm-%d", i), TS: cstCrossDayTS, RequestBodyRaw: []byte(`{"a":1}`)})
	}
	require.Eventually(t, func() bool { return w.DegradationStats().MissingRows == 5 }, 5*time.Second, 10*time.Millisecond)

	st := spooler.Gate().Stats()
	require.True(t, st.Latched, "前置条件：闸门必须因水位闩锁（走 SpoolGate.Tick 的未闩锁分支）")
	require.Contains(t, st.Reason, "disk_watermark", "前置条件：闩锁原因必须是水位那一类")
	require.Equal(t, int64(1), st.Trips, "水位跳变必须恰好一次")

	// 周期检测在 ticker 上跑 ⇒ 那条 ERROR 最多一个 tick 之后出现。
	require.Eventually(t, func() bool { return capture.countContaining("跳变起因 = 磁盘水位") == 1 },
		5*time.Second, 10*time.Millisecond, "水位跳变必须恰好在写入侧打出一条日志：%v", capture.snapshot())

	require.Equal(t, 1, capture.countContaining("闩锁"), "水位闩锁跳变必须恰好一条 ERROR：%v", capture.snapshot())
	require.Equal(t, 1, capture.countContaining("降级为 body_missing"), "降级信号恰好一条")
	line := capture.firstContaining(t, "跳变起因 = 磁盘水位")
	require.Contains(t, line, "usage_percent=99.0",
		"闸门可观测时必须给出 Stats 的水位读数（这是周期比对路径独有的形态）：%s", line)

	// 闩锁期间零条：再发 10 行，全部走 body_missing，但不得再有日志。
	for i := 5; i < 15; i++ {
		w.Emit(gwobs.TraceEvent{RequestID: fmt.Sprintf("wm-%d", i), TS: cstCrossDayTS, RequestBodyRaw: []byte(`{"a":1}`)})
	}
	require.Eventually(t, func() bool { return w.DegradationStats().MissingRows == 15 }, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, 1, capture.countContaining("闩锁"), "闩锁期间不得重复打跳变日志")
	require.Equal(t, 0, capture.countContaining("恢复"), "尚未恢复时不得出现恢复信号")
	require.Equal(t, int64(15), w.DegradationStats().SpoolFailures)

	// A7-3 / D51：闩锁行的定位键全 NULL + body_missing=true；正文不触盘。
	var row obsmodel.CallTrace
	require.NoError(t, db.First(&row, "request_id = ?", "wm-0").Error)
	require.True(t, row.BodyMissing)
	require.Nil(t, row.BodyRefDT)
	require.Nil(t, row.BodyRefInst)
	require.Nil(t, row.BodyRefPart)
	require.Nil(t, row.BodyRefRow)
	require.Nil(t, row.RequestBody, "闩锁行不得再写 jsonb 正文列")
	require.Empty(t, listSpoolParts(t, root), "水位闩锁必须在建块之前返回：不得产出任何 spool part")
}

// TestDegradationLogsWatermarkLatchWithoutGateStats 是 A7-1 的**兜底路径**证据：
// sink 不暴露 GateStats（与只实现三方法的接线同形）时，水位起因由
// `AppendLine` 上报的 `ErrSpoolLatched（原因：disk_watermark: … 已用 99.0% ≥ 90%）`
// 判定 —— 生产里不依赖任何额外访问器也能看到水位闩锁这条信号。
//
// 【与上一条用例互不遮蔽】两条路径的详情形态刻意不同：这里带错误原文
// （`disk_watermark` + `已用 99.0%`），上一条带 `Stats()` 读数（`usage_percent=`）。
func TestDegradationLogsWatermarkLatchWithoutGateStats(t *testing.T) {
	root := t.TempDir()
	spooler := newWatermarkTripSpooler(t, root)

	rec := &orderRecorder{}
	db, _ := newSpoolTestDB(t, rec)
	sink := plainTraceBodySink{spooler: spooler}
	capture := &logCapture{}
	clock := newTestClock(time.Unix(0, 0))

	w := gwobs.NewTraceWriterWithSink(db, 1024, 1, 20*time.Millisecond, gwobs.TraceWriterLimits{}, sink)
	w.ConfigureDegradation(gwobs.DegradationOptions{Logf: capture.logf, Now: clock.Now, SummaryInterval: time.Minute})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)
	go spooler.Run(ctx)

	require.Eventually(t, func() bool { return spooler.Stats().Flushes >= 1 }, 5*time.Second, time.Millisecond)

	for i := 0; i < 3; i++ {
		w.Emit(gwobs.TraceEvent{RequestID: fmt.Sprintf("plain-%d", i), TS: cstCrossDayTS, RequestBodyRaw: []byte(`{"a":1}`)})
	}
	require.Eventually(t, func() bool { return w.DegradationStats().MissingRows == 3 }, 5*time.Second, 10*time.Millisecond)
	require.True(t, spooler.Gate().Latched(), "前置条件：水位闩锁")

	require.Equal(t, 1, capture.countContaining("跳变起因 = 磁盘水位"), "兜底路径也必须打出水位起因：%v", capture.snapshot())
	require.Equal(t, 1, capture.countContaining("闩锁"), "恰好一条跳变 ERROR")
	line := capture.firstContaining(t, "跳变起因 = 磁盘水位")
	require.Contains(t, line, "disk_watermark", "必须带上 AppendLine 上报的原因原文")
	require.Contains(t, line, "已用 99.0%", "必须带上水位读数（原文里就有）")
	require.NotContains(t, line, "usage_percent=", "兜底路径不带 Stats 读数 ⇒ 与周期路径互不遮蔽")

	// 闸门不可观测必须**响亮**说明，不得静默降级。Warn 在**周期分支**上打（首次周期），
	// 因此这里必须等到第一个 tick，而不是立即断言。
	require.Eventually(t, func() bool { return capture.countContaining("闸门状态不可观测") == 1 },
		5*time.Second, 10*time.Millisecond, "必须恰好一条 Warn：%v", capture.snapshot())
	time.Sleep(80 * time.Millisecond) // 再跑几个周期，确认不刷屏
	require.Equal(t, 1, capture.countContaining("闸门状态不可观测"), "Warn 只允许一条：%v", capture.snapshot())

	// A7-2 的汇总在不可观测时也不得伪造闸门字段：显式说明哪些字段拿不到。
	clock.Advance(2 * time.Minute)
	require.Eventually(t, func() bool { return capture.countContaining("汇总") == 1 }, 5*time.Second, 10*time.Millisecond)
	summary := capture.firstContaining(t, "汇总")
	require.Contains(t, summary, "body_missing_rows=3")
	require.NotContains(t, summary, "usage_percent=", "不可观测时不得伪造 usage_percent")
	require.Contains(t, summary, "不可观测")
}

// TestDegradationSuppressesWriterLogsForRootClassLatch 是 A7-1 的「不重复打日志」证据：
// 根目录类闩锁（root_unavailable / root-identity-mismatch，由 Task 5 的 checkRoot 在
// Trip 返回值上**已经**按跳变打过恰好一条 ERROR）在写入侧必须**零条 ERROR** ——
// 否则 §9.3 的「跳变恰好一条」会被两侧同时打日志破坏。
func TestDegradationSuppressesWriterLogsForRootClassLatch(t *testing.T) {
	root := t.TempDir()
	gate := gwobs.NewSpoolGate(gwobs.SpoolGateOptions{
		Root: root, DiskWatermarkPercent: 90, ProbeInterval: time.Millisecond,
		UsagePercent: func(string) (float64, error) { return 1, nil },
	})
	spooler, err := gwobs.NewTraceSpooler(gwobs.TraceSpoolerOptions{
		Root: root, Inst: "blue", BlockBytes: 1 << 20,
		RollInterval: time.Hour, FlushInterval: 5 * time.Millisecond, Gate: gate,
		// 注入根目录检查：表达式「挂载点/根目录不可用」；身份基线仍需可取。
		RootIdentity: func(string) (string, error) { return "test-identity", nil },
		RootCheck:    func(string) error { return errors.New("测试构造：根目录不可用") },
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = spooler.Close() })

	rec := &orderRecorder{}
	db, _ := newSpoolTestDB(t, rec)
	sink := &sinkWithGateStats{&sinkWithDayOf{spooler: spooler, day: gwconfig.TraceBodyConfig{}}}
	capture := &logCapture{}

	w := gwobs.NewTraceWriterWithSink(db, 1024, 1, 20*time.Millisecond, gwobs.TraceWriterLimits{}, sink)
	w.ConfigureDegradation(gwobs.DegradationOptions{Logf: capture.logf, SummaryInterval: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)
	go spooler.Run(ctx)

	// 周期复检（FlushAndRoll → checkRoot）会以 root_unavailable 跳变（Task 5 的 Trip 调用点）。
	require.Eventually(t, func() bool { return spooler.Gate().Latched() }, 5*time.Second, time.Millisecond)
	st := spooler.Gate().Stats()
	require.Contains(t, st.Reason, "root_unavailable", "前置条件：根目录类闩锁")
	require.Equal(t, int64(1), st.Trips)

	for i := 0; i < 5; i++ {
		w.Emit(gwobs.TraceEvent{RequestID: fmt.Sprintf("root-%d", i), TS: cstCrossDayTS, RequestBodyRaw: []byte(`{"a":1}`)})
	}
	require.Eventually(t, func() bool { return w.DegradationStats().MissingRows == 5 }, 5*time.Second, 10*time.Millisecond)
	time.Sleep(100 * time.Millisecond) // 让周期比对再跑几轮

	require.Equal(t, 0, capture.countContaining("ERROR"),
		"根目录类跳变的日志归 Task 5 的三处 Trip 调用点，写入侧必须零条 ERROR：%v", capture.snapshot())
	require.Equal(t, 0, capture.countContaining("body_missing"),
		"写入侧不得为根目录类闩锁重复打印降级行")
	// 降级本身仍然生效：行还是 body_missing，只是日志不重复打。
	var row obsmodel.CallTrace
	require.NoError(t, db.First(&row, "request_id = ?", "root-0").Error)
	require.True(t, row.BodyMissing)
}

// TestDegradationWarnsWhenGateStatsUnobservable：闸门状态不可观测时必须**响亮**说明
// （不得静默地变成「水位跳变没有日志」这个新盲区），且只打一次、不刷屏。
func TestDegradationWarnsWhenGateStatsUnobservable(t *testing.T) {
	rec := &orderRecorder{}
	db, _ := newSpoolTestDB(t, rec)
	capture := &logCapture{}
	sink := &fakeSink{rec: rec, day: gwconfig.TraceBodyConfig{}}

	w := gwobs.NewTraceWriterWithSink(db, 1024, 1, 20*time.Millisecond, gwobs.TraceWriterLimits{}, sink)
	w.ConfigureDegradation(gwobs.DegradationOptions{Logf: capture.logf})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	require.Eventually(t, func() bool { return capture.countContaining("闸门状态不可观测") == 1 },
		5*time.Second, 10*time.Millisecond, "闸门不可观测必须打一条 Warn：%v", capture.snapshot())
	time.Sleep(100 * time.Millisecond) // 期间会跑多个周期
	require.Equal(t, 1, capture.countContaining("闸门状态不可观测"), "Warn 只允许一条，不得每周期刷屏")
}

// ---------------------------------------------------------------------------
// A7-2：闩锁期间的周期汇总（六个闸门字段）
// ---------------------------------------------------------------------------

// TestDegradationSummaryCarriesGateFields：周期汇总必须按 SummaryInterval 输出
// 一条，且带齐 Latched / LatchedSeconds / Trips / ProbeFailures / UsagePercent / reason
// 六个闸门字段（外加 body_missing / spool 失败两个写入侧计数）。
func TestDegradationSummaryCarriesGateFields(t *testing.T) {
	root := t.TempDir()
	spooler := newWatermarkTripSpooler(t, root)

	rec := &orderRecorder{}
	db, _ := newSpoolTestDB(t, rec)
	sink := &sinkWithGateStats{&sinkWithDayOf{spooler: spooler, day: gwconfig.TraceBodyConfig{}}}
	capture := &logCapture{}
	clock := newTestClock(time.Unix(0, 0))

	w := gwobs.NewTraceWriterWithSink(db, 1024, 1, 20*time.Millisecond, gwobs.TraceWriterLimits{}, sink)
	w.ConfigureDegradation(gwobs.DegradationOptions{Logf: capture.logf, Now: clock.Now, SummaryInterval: time.Minute})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)
	go spooler.Run(ctx)

	require.Eventually(t, func() bool { return spooler.Stats().Flushes >= 1 }, 5*time.Second, time.Millisecond)

	for i := 0; i < 3; i++ {
		w.Emit(gwobs.TraceEvent{RequestID: fmt.Sprintf("sum-%d", i), TS: cstCrossDayTS, RequestBodyRaw: []byte(`{"a":1}`)})
	}
	require.Eventually(t, func() bool { return w.DegradationStats().MissingRows == 3 }, 5*time.Second, 10*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	require.Equal(t, 0, capture.countContaining("汇总"), "未到 SummaryInterval 不得输出汇总")

	clock.Advance(2 * time.Minute)
	require.Eventually(t, func() bool { return capture.countContaining("汇总") == 1 }, 5*time.Second, 10*time.Millisecond,
		"到点必须恰好输出一条汇总：%v", capture.snapshot())

	line := capture.firstContaining(t, "汇总")
	for _, key := range []string{
		"latched=true", "latched_seconds=", "trips=", "probe_failures=", "usage_percent=", "reason=",
		"body_missing_rows=3", "spool_failures=3",
	} {
		require.Contains(t, line, key, "A7-2 汇总必须含 %s", key)
	}
	require.Contains(t, line, "usage_percent=99.0", "水位读数必须来自注入的 UsagePercent")
	require.Contains(t, line, "disk_watermark", "reason 必须是闸门侧的水位原因")

	time.Sleep(150 * time.Millisecond) // 时钟未推进 ⇒ 再来多个 tick 也不得再输出
	require.Equal(t, 1, capture.countContaining("汇总"), "同一周期内不得重复输出汇总：%v", capture.snapshot())
}
