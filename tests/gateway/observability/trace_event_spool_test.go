package observability_test

// 【本文件为什么在 B 层（package observability_test）】
// 它只用 observability 的公开 API（TraceWriter / TraceBodySink / TraceSpooler /
// SpoolGate），不需要触达未导出符号 ⇒ 按测试分层（规格 §14.1、docs/guardrails.md Q3）
// 放在 tests/，与既有 trace_writer_limits_test.go / spooler_test.go 同层。

import (
	"context"
	"encoding/json"
	"errors"
	"os"
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
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// cstTestZone 只用于**构造时间戳**（把事件的瞬间摆在北京时间的某个点上），
// **不参与**任何 dt / body_ref_dt 的计算 —— 日期口径一律走生产入口
// `gwconfig.TraceBodyConfig`（见 fakeSink / sinkWithDayOf）。
//
// 【为什么两边刻意用不同的来源】时间戳固定在测试自带的 +08:00、期望值由生产入口算，
// 二者一旦不再一致就会错位：例如 `config.TraceBodyZone` 被改成 UTC，
// 「北京 01:00」这个瞬间的归属日就变成 09-23，而期望值仍是 09-24 ⇒ 用例立刻红。
// 若时间戳也用 `config.TraceBodyZone` 构造，时区一改两边同时平移，反而测不出来。
//
// 【为什么时间戳一律用「北京 01:00」这种跨日形状】北京 01:00 的 UTC 日期是
// **前一天**。若时间戳取在中午（如 12:00），UTC 日期与 UTC+8 日期恰好相同，
// 「dt 退回 UTC」这类缺陷会全绿地溜过去（假绿）——本文件的几条标点用例因此
// 刻意选在跨日边界附近。
var cstTestZone = time.FixedZone("CST", 8*60*60)

// cstCrossDayTS 返回「北京 2026-09-25 00:30」，其 UTC 日期是 2026-09-24。
var cstCrossDayTS = time.Date(2026, 9, 25, 0, 30, 0, 0, cstTestZone)

// orderRecorder 用一个原子计数器把「两个动作发生的相对次序」变成可断言的事实。
// 这是本文件存在的首要理由：规格 §10.1 I1 要求「元数据行存在 ⇒ 产物 XOR spool 存在」，
// 它只在「先追加 spool、再写 PG」的次序下成立；反序会留下一条指向不存在正文的元数据行。
type orderRecorder struct {
	seq     atomic.Int64
	appends []string // 记录 dt，供 dt 口径断言
	mu      sync.Mutex
}

func (r *orderRecorder) next() int64 { return r.seq.Add(1) }

// recordAppend 记录一次 AppendLine 的 dt（受锁保护，测试侧并发读）。
func (r *orderRecorder) recordAppend(dt string) {
	r.mu.Lock()
	r.appends = append(r.appends, dt)
	r.mu.Unlock()
}

// appendedDTs 返回已记录的 dt 快照（copy，避免与写协程共享底层数组）。
func (r *orderRecorder) appendedDTs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.appends...)
}

// fakeSink 实现 gwobs.TraceBodySink：**生产日期入口**（gwconfig.TraceBodyConfig）
// + 假落盘（记录追加次序，不碰磁盘）。
//
// 【为什么日期也走 config 而不是测试自算】写入器的 dt/body_ref_dt 断言必须链到
// 生产唯一的日期入口上，否则 config.TraceBodyZone 被改动时用例发现不了。
type fakeSink struct {
	rec *orderRecorder

	mu      sync.Mutex
	appendI []int64
	err     error
	day     gwconfig.TraceBodyConfig
}

func (s *fakeSink) AppendLine(dt string, line []byte) (ta.Ref, error) {
	seq := s.rec.next()
	s.mu.Lock()
	s.appendI = append(s.appendI, seq)
	n := len(s.appendI)
	s.mu.Unlock()
	s.rec.recordAppend(dt)
	if s.err != nil {
		return ta.Ref{}, s.err
	}
	return ta.Ref{DT: dt, Inst: "blue", Part: 0, Row: n - 1}, nil
}

// appendSeqs 返回追加序号快照（受锁保护）。
func (s *fakeSink) appendSeqs() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int64(nil), s.appendI...)
}

// DayStart 与 DayOf 同源：DayOf 由 DayStart 派生（全局约束的单一事实源）。
// 零值 TraceBodyConfig 足够：这两个方法只读包级 TraceBodyZone，不读结构体字段
// （与生产 main.go 的 traceBodySink 同形，只是那里的 cfg 来自 viper）。
func (s *fakeSink) DayStart(ts time.Time) time.Time { return s.day.DayStart(ts) }

func (s *fakeSink) DayOf(ts time.Time) string { return s.day.DayOf(ts) }

// insertRecorder 记录每次 INSERT 的序号（受锁保护：回调在写协程上跑，断言在主测试协程上读）。
type insertRecorder struct {
	mu  sync.Mutex
	seq []int64
}

func (r *insertRecorder) add(v int64) {
	r.mu.Lock()
	r.seq = append(r.seq, v)
	r.mu.Unlock()
}

func (r *insertRecorder) snapshot() []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]int64(nil), r.seq...)
}

// newSpoolTestDB 建内存 sqlite + 记录每次 INSERT 的序号。
func newSpoolTestDB(t *testing.T, rec *orderRecorder) (*gorm.DB, *insertRecorder) {
	t.Helper()
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{SkipDefaultTransaction: true})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&obsmodel.CallTrace{}))
	inserts := &insertRecorder{}
	db.Callback().Create().Before("gorm:create").Register("test:order", func(tx *gorm.DB) {
		inserts.add(rec.next())
	})
	return db, inserts
}

// newTestSpooler 构造一个**不依赖宿主磁盘**的真实 spooler。
//
// 【为什么注入一个新建的 Gate（而不是用 spooler 内置的那个）】解锁判据是
// 「探针可写 **且** 水位未超限」，内置 Gate 的水位来自真实 statfs ⇒ 把用例的
// 「能解锁」押在宿主磁盘用量上（>90% 已用即假红）。全局约束要求这类断言注入水位与探针。
// 注入的 Gate 只被**这一个** spooler 使用一次：A6-9 第 2 条禁止的是「同一个 Gate 被
// 两个 spooler 复用/二次包装」，一次性注入不构成那条失效，且根目录身份包装由
// NewTraceSpooler 无条件套上（A6-9 第 1 条），不受影响。
func newTestSpooler(t *testing.T, root, inst string) *gwobs.TraceSpooler {
	t.Helper()
	gate := gwobs.NewSpoolGate(gwobs.SpoolGateOptions{
		Root:                 root,
		DiskWatermarkPercent: 90,
		ProbeInterval:        time.Millisecond,
		// 固定水位：本文件的「能解锁」断言不得依赖宿主磁盘用量。
		UsagePercent: func(string) (float64, error) { return 1, nil },
	})
	spooler, err := gwobs.NewTraceSpooler(gwobs.TraceSpoolerOptions{
		Root: root, Inst: inst, BlockBytes: 1 << 20,
		RollInterval: time.Hour, FlushInterval: time.Minute, Gate: gate,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = spooler.Close() })
	return spooler
}

// sinkWithDayOf 把真实 spooler 与**生产日期入口**装在一起，是 main.go 里
// traceBodySink 的测试侧同形物（TraceSpooler 提供 AppendLine，
// gwconfig.TraceBodyConfig 提供 DayStart/DayOf）⇒ 生产日期胶水在这里被真正执行，
// 而不是由测试自算一遍。
type sinkWithDayOf struct {
	spooler *gwobs.TraceSpooler
	day     gwconfig.TraceBodyConfig
}

func (s *sinkWithDayOf) AppendLine(dt string, line []byte) (ta.Ref, error) {
	return s.spooler.AppendLine(dt, line)
}

// DayStart 与 DayOf 同源：DayOf 由 DayStart 派生（全局约束的单一事实源）。
func (s *sinkWithDayOf) DayStart(ts time.Time) time.Time { return s.day.DayStart(ts) }

func (s *sinkWithDayOf) DayOf(ts time.Time) string { return s.day.DayOf(ts) }

// TestTraceWriterAppendsSpoolBeforeMetadataRow 是**承重**用例（规格 §10.1 I1）：
// 每一次 flush 里，所有 AppendLine 必须先于任何一次 INSERT 发生。
// 若实现把次序写反，appendI 里会出现比它自己的 insert 更大的序号。
//
// 【两个计数都要等】只等「INSERT 可见」不足以保证追加已经发生（反序实现里，
// 追加可能晚于插入可见）⇒ Eventually 同时要求三个计数到齐，次序断言才落在
// require.Less 这一行上，而不是落在长度断言上。
func TestTraceWriterAppendsSpoolBeforeMetadataRow(t *testing.T) {
	rec := &orderRecorder{}
	db, inserts := newSpoolTestDB(t, rec)
	sink := &fakeSink{rec: rec, day: gwconfig.TraceBodyConfig{}}

	w := gwobs.NewTraceWriterWithSink(db, 1024, 1, time.Hour, gwobs.TraceWriterLimits{}, sink)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	for i := 0; i < 3; i++ {
		w.Emit(gwobs.TraceEvent{
			RequestID: "req-" + string(rune('a'+i)),
			// 北京 01:00 ⇒ UTC 日期是 09-23（前一天）：dt 口径退回 UTC 会被抓住。
			TS:             time.Date(2026, 9, 24, 1, 0, i, 0, cstTestZone),
			RequestBodyRaw: []byte(`{"a":1}`),
		})
	}
	require.Eventually(t, func() bool {
		var n int64
		db.Model(&obsmodel.CallTrace{}).Count(&n)
		return n == 3 && len(sink.appendSeqs()) == 3 && len(inserts.snapshot()) == 3
	}, 5*time.Second, 10*time.Millisecond)

	appends := sink.appendSeqs()
	ins := inserts.snapshot()
	require.Len(t, appends, 3)
	require.Len(t, ins, 3)
	for i, app := range appends {
		require.Less(t, app, ins[i], "第 %d 行：spool 追加（seq=%d）必须先于 PG 插入（seq=%d）", i, app, ins[i])
	}

	// dt（目录名分量）必须按生产日期入口（TraceBodyConfig → TraceBodyZone，UTC+8）口径，
	// 且与 body_ref_dt 列同源。期望值是字面量，而时间戳由测试自带的 +08:00 构造 ⇒
	// 日期入口的时区一旦改变，这里就会错位（评审 F1 要求的耦合）。
	require.Equal(t, []string{"2026-09-24", "2026-09-24", "2026-09-24"}, rec.appendedDTs(),
		"dt 必须是 TS 在 UTC+8 下的日期；退回 UTC 会得到 2026-09-23")

	var rows []obsmodel.CallTrace
	require.NoError(t, db.Order("request_id").Find(&rows).Error)
	require.Len(t, rows, 3)
	for _, row := range rows {
		require.Nil(t, row.RequestBody, "新行不得再写正文 jsonb 列")
		require.Nil(t, row.RequestHeaders)
		require.Nil(t, row.ResponseBody)
		require.False(t, row.BodyMissing)
		require.NotNil(t, row.BodyRefDT)
		require.NotNil(t, row.BodyRefPart)
		require.NotNil(t, row.BodyRefRow)
		require.NotNil(t, row.BodyBytes)
		require.Equal(t, len(`{"a":1}`), *row.BodyBytes, "body_bytes 口径 = len(request_body)+len(response_body)")
		require.Equal(t, "2026-09-24", row.BodyRefDT.Format("2006-01-02"),
			"dt 必须按 UTC+8 口径（date 列用 *time.Time，不能用 *string）")
		// 列值必须是该日历日的 **UTC 零点**：PG date 列按会话时区（生产 = UTC）折算，
		// 传 +08:00 的零点会落成前一天。只比 Format 抓不住这一条（同一个瞬间在不同
		// Location 下 Format 出的日期可能相同），因此这里比 UTC 瞬间。
		require.Equal(t, time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC), row.BodyRefDT.UTC(),
			"body_ref_dt 必须是 UTC 零点（DayStart 的口径），不是事件的原始时刻")
		require.Equal(t, "blue", *row.BodyRefInst)
	}
}

// TestTraceWriterMarksBodyMissingWhenSpoolFails 固定 R9 的降级列：
// spool 失败 ⇒ body_missing=true、定位键全 NULL、PG 行**照常写入**。
func TestTraceWriterMarksBodyMissingWhenSpoolFails(t *testing.T) {
	rec := &orderRecorder{}
	db, _ := newSpoolTestDB(t, rec)
	sink := &fakeSink{rec: rec, err: errors.New("no space left on device"), day: gwconfig.TraceBodyConfig{}}

	w := gwobs.NewTraceWriterWithSink(db, 1024, 1, time.Hour, gwobs.TraceWriterLimits{}, sink)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	w.Emit(gwobs.TraceEvent{RequestID: "req-missing", TS: time.Now(), RequestBodyRaw: []byte(`{"a":1}`)})

	require.Eventually(t, func() bool {
		var n int64
		db.Model(&obsmodel.CallTrace{}).Count(&n)
		return n == 1
	}, 5*time.Second, 10*time.Millisecond)

	var row obsmodel.CallTrace
	require.NoError(t, db.First(&row, "request_id = ?", "req-missing").Error)
	require.True(t, row.BodyMissing)
	require.Nil(t, row.BodyRefDT)
	require.Nil(t, row.BodyRefInst)
	require.Nil(t, row.BodyRefPart)
	require.Nil(t, row.BodyRefRow)
	require.NotNil(t, row.BodyBytes, "body_bytes 是审计口径，缺失行也应记录原正文大小")
	require.Equal(t, len(`{"a":1}`), *row.BodyBytes)
	require.Nil(t, row.RequestBody)
}

// TestTraceWriterLatchedSpoolMarksBodyMissing 覆盖 A6-2 的**闩锁中**这一半
// （上一个用例覆盖的是「被拒绝/失败」这一半）：
// 闸门已闩锁时 AppendLine 返回 ErrSpoolLatched，写入器必须把该行标成
// body_missing=true、定位键全 NULL，**且不去触碰磁盘**（闩锁的意义就是把
// 「每请求重试一次」变成「每周期探一次」，闩锁中不得再尝试建块/建目录）。
//
// 【为什么用 Trip 构造前置状态而不是真盘满】真实水位/盘满不可复现，
// 且闸门本身的状态机由 Task 4 的证据覆盖；本用例只依赖「已闩锁」这一状态。
func TestTraceWriterLatchedSpoolMarksBodyMissing(t *testing.T) {
	root := t.TempDir()
	spooler := newTestSpooler(t, root, "blue")
	spooler.Gate().Trip("disk_watermark", errors.New("测试构造的闩锁"))
	require.False(t, spooler.Gate().Allow(), "前置条件：闸门必须已闩锁")

	rec := &orderRecorder{}
	db, inserts := newSpoolTestDB(t, rec)
	sink := &sinkWithDayOf{spooler: spooler, day: gwconfig.TraceBodyConfig{}}
	w := gwobs.NewTraceWriterWithSink(db, 1024, 1, time.Minute, gwobs.TraceWriterLimits{}, sink)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	w.Emit(gwobs.TraceEvent{RequestID: "req-latched", TS: cstCrossDayTS, RequestBodyRaw: []byte(`{"a":1}`)})
	require.Eventually(t, func() bool {
		var n int64
		db.Model(&obsmodel.CallTrace{}).Count(&n)
		return n == 1
	}, 5*time.Second, 10*time.Millisecond)

	var row obsmodel.CallTrace
	require.NoError(t, db.First(&row, "request_id = ?", "req-latched").Error)
	require.True(t, row.BodyMissing, "闩锁中的行必须标 body_missing=true")
	require.Nil(t, row.BodyRefDT)
	require.Nil(t, row.BodyRefInst)
	require.Nil(t, row.BodyRefPart)
	require.Nil(t, row.BodyRefRow)
	require.NotNil(t, row.BodyBytes)
	require.Nil(t, row.RequestBody)

	// 闩锁中不得触盘：连 spool 目录都不该被建出来（网关侧不建目录，
	// 冻结包只在真的建块时才建 —— 闩锁必须在建块之前就返回）。
	_, statErr := os.Stat(filepath.Join(root, "spool"))
	require.True(t, os.IsNotExist(statErr), "闩锁中不得尝试写盘（不得留下 spool 目录）")
	require.Len(t, inserts.snapshot(), 1, "PG 元数据行照常写入（降级只影响正文）")
}

// TestTraceWriterLegacyPathKeepsBodiesInPG 是**回归**用例：
// sink == nil（功能关闭）时必须与本计划之前的行为逐字段相同 —— 正文仍进 jsonb 列。
func TestTraceWriterLegacyPathKeepsBodiesInPG(t *testing.T) {
	rec := &orderRecorder{}
	db, _ := newSpoolTestDB(t, rec)

	w := gwobs.NewTraceWriterWithSink(db, 1024, 1, time.Hour, gwobs.TraceWriterLimits{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	w.Emit(gwobs.TraceEvent{
		RequestID: "req-legacy", TS: time.Now(),
		RequestHeadersRaw: []byte(`{"h":"1"}`),
		RequestBodyRaw:    []byte(`{"a":1}`),
		ResponseBodyRaw:   []byte(`{"ok":true}`),
	})
	require.Eventually(t, func() bool {
		var n int64
		db.Model(&obsmodel.CallTrace{}).Count(&n)
		return n == 1
	}, 5*time.Second, 10*time.Millisecond)

	var row obsmodel.CallTrace
	require.NoError(t, db.First(&row, "request_id = ?", "req-legacy").Error)
	require.NotNil(t, row.RequestHeaders)
	require.NotNil(t, row.RequestBody)
	require.NotNil(t, row.ResponseBody)
	require.False(t, row.BodyMissing)
	require.Nil(t, row.BodyRefDT)
	require.Nil(t, row.BodyRefInst)
	require.Nil(t, row.BodyRefPart)
	require.Nil(t, row.BodyRefRow)
	// 关闭路径必须与改动前**逐字段相同**：body_bytes 原先也不写 ⇒ 仍为 NULL。
	require.Nil(t, row.BodyBytes)
}

// TestTraceWriterRetryAfterFailureStartsAtRowZero 是**偏离登记（规格 §7.1）**所依赖前提的实证：
// **失败的请求不占用行号**。
//
// 规格 §7.1 要求网关用失败标记行维持「物理行序 == row_seq」。本计划**不写标记行**
// （理由见简报步骤 3 的偏离登记）：失败行的四个定位键写成 NULL，读侧永远不会按行号
// 去取它的正文，因此不需要占位。本用例把那个推理变成可判定的事实：
//
//	① 第一条事件（dt=A）成功 ⇒ part 0 / row 0；
//	② 在 spool 目录下用**普通文件**占住 dt=B 的目录名 ⇒ 第二条（dt=B）追加失败：
//	   nextSeqLocked 的 os.ReadDir 以 ENOTDIR 失败（writer.go:527-544）⇒
//	   元数据行 body_missing=true、定位键全 NULL；
//	③ 解除闩锁、移除障碍 ⇒ 第三条（dt=B）成功，必须是 part 0 / **row 0**：
//	   第二步没有消耗任何行号（否则会看到 row=1）。
//
// 【为什么用普通文件占位】真实 ENOSPC 不可复现；ENOTDIR 是**确定性**地让 Append 失败、
// 且不毒化任何已存在块的唯一办法（若块被毒化则会走隔离+换 part 契约，那是冻结行为、
// 由计划 1 的证据覆盖）。等价的是状态，不是成因。
func TestTraceWriterRetryAfterFailureStartsAtRowZero(t *testing.T) {
	root := t.TempDir()
	spooler := newTestSpooler(t, root, "blue")

	rec := &orderRecorder{}
	db, _ := newSpoolTestDB(t, rec)
	sink := &sinkWithDayOf{spooler: spooler, day: gwconfig.TraceBodyConfig{}}
	w := gwobs.NewTraceWriterWithSink(db, 1024, 1, 50*time.Millisecond, gwobs.TraceWriterLimits{}, sink)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	dayA := time.Date(2026, 9, 24, 10, 0, 0, 0, cstTestZone)
	dayB := dayA.Add(24 * time.Hour)

	// ① 第一条成功：part 0 / row 0
	w.Emit(gwobs.TraceEvent{RequestID: "req-1", TS: dayA, RequestBodyRaw: []byte(`{"i":1}`)})
	require.Eventually(t, func() bool {
		var n int64
		db.Model(&obsmodel.CallTrace{}).Count(&n)
		return n == 1
	}, 5*time.Second, 10*time.Millisecond)

	// ② 占住 dt=B 的目录名 ⇒ 第二条必然追加失败
	require.NoError(t, os.MkdirAll(filepath.Join(root, "spool"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "spool", "dt=2026-09-25"), []byte("x"), 0o644))
	w.Emit(gwobs.TraceEvent{RequestID: "req-2", TS: dayB, RequestBodyRaw: []byte(`{"i":2}`)})
	require.Eventually(t, func() bool {
		var n int64
		db.Model(&obsmodel.CallTrace{}).Count(&n)
		return n == 2
	}, 5*time.Second, 10*time.Millisecond)

	var failed obsmodel.CallTrace
	require.NoError(t, db.First(&failed, "request_id = ?", "req-2").Error)
	require.True(t, failed.BodyMissing, "追加失败必须标 body_missing=true")
	require.Nil(t, failed.BodyRefDT)
	require.Nil(t, failed.BodyRefInst)
	require.Nil(t, failed.BodyRefPart)
	require.Nil(t, failed.BodyRefRow)

	// 写失败会让闸门闩锁（R9），必须显式恢复才能验证重试（注入的探针 + 固定水位会成功）
	spooler.Gate().Tick(time.Now().Add(2 * time.Minute))
	require.True(t, spooler.Gate().Allow(), "前置条件：闸门必须已恢复")

	// ③ 移除障碍 ⇒ 第三条成功，且必须是 part 0 / row 0
	require.NoError(t, os.Remove(filepath.Join(root, "spool", "dt=2026-09-25")))
	w.Emit(gwobs.TraceEvent{RequestID: "req-3", TS: dayB, RequestBodyRaw: []byte(`{"i":3}`)})
	require.Eventually(t, func() bool {
		var n int64
		db.Model(&obsmodel.CallTrace{}).Count(&n)
		return n == 3
	}, 5*time.Second, 10*time.Millisecond)

	var retried obsmodel.CallTrace
	require.NoError(t, db.First(&retried, "request_id = ?", "req-3").Error)
	require.False(t, retried.BodyMissing)
	require.NotNil(t, retried.BodyRefDT)
	require.NotNil(t, retried.BodyRefPart)
	require.NotNil(t, retried.BodyRefRow)
	require.Equal(t, "2026-09-25", retried.BodyRefDT.Format("2006-01-02"))
	require.Equal(t, 0, *retried.BodyRefPart, "dt=B 是全新的一天，必须是 part 0")
	require.Equal(t, 0, *retried.BodyRefRow, "失败的请求不得占用行号：重试必须落在 row 0")
}

// TestTraceWriterDtCoversUTCPlus8DayBoundary 是 A6-4 的交接项①：
// 行内 ts 的时区常量（observability 包内的 traceLineZone）与 dt 口径的时区常量
// （config.TraceBodyZone）是两份，Task 3 在包内无法钉住二者相等 ⇒ 这里用
// **生产日期入口（gwconfig.TraceBodyConfig）+ 真实 spooler + 真实目录名 + 真实行内容**
// 把「同一天」这件事一并钉住：
//
//	① 目录名：北京 2026-09-25 00:30（UTC 仍是 09-24 16:30）的事件必须落进 dt=2026-09-25；
//	② 列值：body_ref_dt 必须是 2026-09-25 的 UTC 零点；
//	③ 行内 ts：必须写成 +08:00 的 2026-09-25T00:30:00+08:00（与目录同一天）。
//
// **本用例真正钉住的范围**（评审 F1 后按实际改写）：① 与② 走的是生产日期入口
// （sinkWithDayOf 的 DayStart/DayOf 委托给 TraceBodyConfig，不再由测试自算），
// 因此 `config.TraceBodyZone` 一旦被改成会改变归属日的口径（例如 UTC），
// 前置断言与①都会立刻红；③ 钉的是 observability 包内行序列化器的 +08:00。
// 两份常量「恰好都是 +08:00」只在「归属日相同」这个可观测后果上被覆盖，
// 本用例不声称做了常量相等性检查（那由 p2-task-2 的静态检查负责）。
// dt 退回 UTC 会让①变成 dt=2026-09-24，行内 ts 退回 UTC 会让③变成 UTC 时刻。
func TestTraceWriterDtCoversUTCPlus8DayBoundary(t *testing.T) {
	// 前置条件：生产日期入口本身必须把「北京 09-25 00:30」归到 09-25。
	// 这一条把期望值与 config.TraceBodyZone 显式绑在一起 —— 否则期望值只是本地字面量，
	// 时区常量被改也看不出来。
	require.Equal(t, "2026-09-25", gwconfig.TraceBodyConfig{}.DayOf(cstCrossDayTS),
		"前置条件：生产日期入口（TraceBodyZone）必须把北京 09-25 00:30 归到 09-25")

	root := t.TempDir()
	spooler := newTestSpooler(t, root, "blue")

	rec := &orderRecorder{}
	db, _ := newSpoolTestDB(t, rec)
	sink := &sinkWithDayOf{spooler: spooler, day: gwconfig.TraceBodyConfig{}}
	w := gwobs.NewTraceWriterWithSink(db, 1024, 1, time.Minute, gwobs.TraceWriterLimits{}, sink)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)
	go spooler.Run(ctx)

	w.Emit(gwobs.TraceEvent{RequestID: "req-cross-day", TS: cstCrossDayTS, RequestBodyRaw: []byte(`{"a":1}`)})
	require.Eventually(t, func() bool {
		var n int64
		db.Model(&obsmodel.CallTrace{}).Count(&n)
		return n == 1
	}, 5*time.Second, 10*time.Millisecond)

	var row obsmodel.CallTrace
	require.NoError(t, db.First(&row, "request_id = ?", "req-cross-day").Error)
	require.False(t, row.BodyMissing)
	require.NotNil(t, row.BodyRefDT)
	require.Equal(t, "2026-09-25", row.BodyRefDT.Format("2006-01-02"),
		"列值必须是北京 09-25 那一日（其 UTC 日期是 09-24）")
	require.Equal(t, time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC), row.BodyRefDT.UTC(),
		"列值必须是北京 09-25 那一日的 UTC 零点（事件的瞬间在 UTC 下仍是 09-24）")
	require.NotNil(t, row.BodyRefPart)
	require.NotNil(t, row.BodyRefRow)

	spoolPath := filepath.Join(root, "spool", "dt=2026-09-25", "inst=blue", "part-000000.jsonl")
	require.NoError(t, spooler.FlushAndRoll())
	require.FileExists(t, spoolPath,
		"正文必须落在 dt=2026-09-25（UTC+8 口径）；落进 dt=2026-09-24 说明 dt 退回了 UTC")
	_, statErr := os.Stat(filepath.Join(root, "spool", "dt=2026-09-24"))
	require.True(t, os.IsNotExist(statErr), "不得出现 dt=2026-09-24 目录（那是 UTC 日期）")

	raw, err := os.ReadFile(spoolPath)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	require.Len(t, lines, 1)
	var decoded struct {
		TS string `json:"ts"`
	}
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &decoded))
	require.Equal(t, "2026-09-25T00:30:00+08:00", decoded.TS,
		"行内 ts 必须是 +08:00 口径（与 dt 同一天，不得退回 UTC）")
}

// TestTraceWriterIndentedBodyStaysOneLine 是 A6-4 的交接项②：
// Task 3 的「行内无裸换行」原先只是结构性推理（json.Marshal 会把正文里的换行
// 编码成 \n）。这里用**带缩进的合法正文**（正文自带裸换行）走完整写路径，
// 断言「一次 Append == 一行」，把那个推理变成运行时证据 —— 裸换行会当场把
// 一次追加劈成两行，从而破坏「物理行序 == row_seq」。
func TestTraceWriterIndentedBodyStaysOneLine(t *testing.T) {
	root := t.TempDir()
	spooler := newTestSpooler(t, root, "blue")

	rec := &orderRecorder{}
	db, _ := newSpoolTestDB(t, rec)
	sink := &sinkWithDayOf{spooler: spooler, day: gwconfig.TraceBodyConfig{}}
	w := gwobs.NewTraceWriterWithSink(db, 1024, 1, time.Minute, gwobs.TraceWriterLimits{}, sink)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)
	go spooler.Run(ctx)

	// 缩进 + 字符串里带转义换行：两者都不得变成物理裸换行。
	indented := "{\n  \"a\": 1,\n  \"b\": \"x\\ny\"\n}"
	ts := time.Date(2026, 9, 24, 1, 0, 0, 0, cstTestZone)
	w.Emit(gwobs.TraceEvent{RequestID: "req-indented", TS: ts, RequestBodyRaw: []byte(indented)})
	w.Emit(gwobs.TraceEvent{RequestID: "req-second", TS: ts, RequestBodyRaw: []byte(`{"c":2}`)})
	require.Eventually(t, func() bool {
		var n int64
		db.Model(&obsmodel.CallTrace{}).Count(&n)
		return n == 2
	}, 5*time.Second, 10*time.Millisecond)

	spoolPath := filepath.Join(root, "spool", "dt=2026-09-24", "inst=blue", "part-000000.jsonl")
	require.NoError(t, spooler.FlushAndRoll())
	raw, err := os.ReadFile(spoolPath)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	require.Len(t, lines, 2, "两次 Append 必须恰好是两行：正文里的裸换行必须被 JSON 编码")

	for i, line := range lines {
		require.True(t, json.Valid([]byte(line)), "第 %d 行必须是合法 JSON（裸换行会把它劈开）", i)
	}
	var decoded struct {
		RequestID   string          `json:"request_id"`
		RequestBody json.RawMessage `json:"request_body"`
	}
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &decoded))
	require.Equal(t, "req-indented", decoded.RequestID)
	// 正文往返无损：缩进被 compact 掉，但字符串里的换行语义必须保留。
	require.JSONEq(t, indented, string(decoded.RequestBody),
		"正文内容必须无损（只允许去掉缩进空白）")
}
