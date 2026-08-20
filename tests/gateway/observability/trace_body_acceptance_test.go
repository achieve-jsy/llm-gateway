package observability_test

// 【本文件为什么在 B 层（package observability_test）】
// 它只用公开 API（TraceWriter / TraceBodySink / TraceSpooler / SpoolGate /
// gwconfig.TraceBodyConfig），不需要触达未导出符号 ⇒ 按测试分层（规格 §14.1、
// docs/guardrails.md Q3）放在 tests/，与 trace_event_spool_test.go 同层。
//
// 【本文件承担哪两条验收 —— 规格 §14.2 里只有这两条属本计划】
//
//	A1（**只覆盖写入半部**）：事件 → spool 行的逐字节对应 + 元数据行的定位键正确；
//	A3（**注入式等价用例**）：正文降级为 body_missing 后网关继续服务、延迟无劣化。
//
// 其余条目**显式排除**：A2（封块产物可点读）属计划 3；A6/A7/A8（压缩比、跨进程、
// 行数分布）属计划 4；A9（冷热分层）属未来计划。
//
// 【A1 的边界：只覆盖写入半部】
// 本文件只断言「写进去的字节」与「写出来的定位键」对得上 —— 这是读写两侧唯一能在
// 写入侧独立固定的契约。**生产读侧服务**（按 (dt, inst, part, row) 取回正文的点读
// 闭环）、封块产物与索引属计划 3：本文件不实现、不断言生产读侧，也不构造读侧替身。
// A1 ② 里那次「按定位键拼路径读文件」是**测试内直接读 spool 文件**，作用只是把写入侧
// 契约钉死（写进去的字节 == 事件给的字节），不是读侧服务的实现、替身或验收。
//
// 【A3 的边界：注入式等价，不是真实盘满】
// TestAcceptanceA3DegradesWithoutDiskPressure 用**可注入的水位函数**把系统推到与
// 盘满**同一状态**（闸门闩锁 ⇒ 每一行 body_missing），等价的是「状态」，不是「成因」。
// 本用例不验证真实 ENOSPC，也不验证真实 umount / 挂载消失 —— 那属计划 4 的部署验收。
// 报告里必须逐字保留这一句，不得写成「已验证磁盘满」。
//
// 【复用 Task 6 的测试助手（增补 A8-1/A8-2），不重声明】
//   - orderRecorder / sinkWithDayOf / newSpoolTestDB / newTestSpooler / logCapture
//     定义在同包的 trace_event_spool_test.go / spooler_test.go / trace_degradation_test.go；
//   - 「一次追加严格早于对应 INSERT」的观测手法沿用 Task 6 的**共享原子计数器 +
//     require.Less**（见下面的 acceptanceSink / insertObserver），不另造一套。
//   - 水位与探针一律**注入**（newTestSpooler 的固定 1% 水位 / A3 的恒超水位），
//     落盘与解锁断言因此与宿主磁盘用量、宿主时区、分钟边界都无关。

import (
	"context"
	"encoding/json"
	"fmt"
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
	"gorm.io/gorm"
)

// appendCall 记录一次 AppendLine 的发起序号与返回值。
type appendCall struct {
	seq int64
	err error
}

// acceptanceSink 是「**真实** TraceSpooler + 次序观测」的组装物（A8-2）。
//
// 它嵌入同包的 sinkWithDayOf（A8-1：复用，不重声明），只在外面包一层：
// 进入时取一个全局序号、退出时记下返回值。日期口径仍走生产入口
// gwconfig.TraceBodyConfig（零值足够：DayStart/DayOf 只读包级 TraceBodyZone）。
//
// 【为什么「进入时取序号」也够证明次序】写入器是**唯一写协程**：它不可能在
// AppendLine 尚未返回时就去执行同一批的 INSERT，因此「追加的进入序号 < INSERT 的
// 序号」等价于「追加已返回、才开始 INSERT」。
type acceptanceSink struct {
	*sinkWithDayOf
	rec *orderRecorder

	started atomic.Int64 // 已进入 AppendLine 的次数（= 已发起的追加数）
	mu      sync.Mutex
	calls   []appendCall
}

func newAcceptanceSink(spooler *gwobs.TraceSpooler, rec *orderRecorder) *acceptanceSink {
	return &acceptanceSink{
		sinkWithDayOf: &sinkWithDayOf{spooler: spooler, day: gwconfig.TraceBodyConfig{}},
		rec:           rec,
	}
}

func (s *acceptanceSink) AppendLine(dt string, line []byte) (ta.Ref, error) {
	seq := s.rec.next()
	s.started.Add(1)
	ref, err := s.sinkWithDayOf.AppendLine(dt, line)
	s.mu.Lock()
	s.calls = append(s.calls, appendCall{seq: seq, err: err})
	s.mu.Unlock()
	return ref, err
}

func (s *acceptanceSink) callsSnapshot() []appendCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]appendCall(nil), s.calls...)
}

// insertBatch 是一次 INSERT 的观测快照。
type insertBatch struct {
	seq        int64 // 与追加共用一个原子计数器（A8-2 的手法）
	rows       int   // 这一次 INSERT 写入的行数
	appendedAt int64 // INSERT 发生时，已发起的追加数
}

// insertObserver 通过 GORM 的 Create 前回调记录每一次 INSERT。
//
// 【为什么需要批次行数】写入器按 flushN 分批（本用例 200）⇒ 一次追加与一次 INSERT
// 并非一一对应，「第 i 条追加先于承载它的那条 INSERT」必须按批次算。
// 行数从 Statement.Dest 读：flush 里的 Create(&models) 传的是 *[]obsmodel.CallTrace
// （逐条降级路径传 *obsmodel.CallTrace ⇒ 记 1 行）。
type insertObserver struct {
	rec  *orderRecorder
	sink *acceptanceSink

	mu      sync.Mutex
	batches []insertBatch
}

func (o *insertObserver) record(tx *gorm.DB) {
	rows := 1
	if dest, ok := tx.Statement.Dest.(*[]obsmodel.CallTrace); ok && len(*dest) > 0 {
		rows = len(*dest)
	}
	b := insertBatch{seq: o.rec.next(), rows: rows, appendedAt: o.sink.started.Load()}
	o.mu.Lock()
	o.batches = append(o.batches, b)
	o.mu.Unlock()
}

func (o *insertObserver) snapshot() []insertBatch {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]insertBatch(nil), o.batches...)
}

// splitLines 读出一个 spool 文件的行（Writer.Append 每条正文后补一个换行 ⇒ 直接按
// 换行切分即可；末尾换行不算最后一行）。本文件读取的 part 文件都在 FlushAndRoll
// 之后，字节已落盘。
func splitLines(t *testing.T, p string) []string {
	t.Helper()
	b, err := os.ReadFile(p)
	require.NoError(t, err)
	s := strings.TrimSuffix(string(b), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// TestAcceptanceA1WriteHalf 是规格 §14.2 A1 的**写入半部**：
// 用真实 TraceSpooler（临时目录，非替身）+ 内存 sqlite，逐条比对
// 「事件 → spool 行」的字节与「事件 → 元数据行」的定位键。
//
// **生产读侧服务**（按 (dt, inst, part, row) 取回正文的点读闭环）与封块产物/索引
// 属计划 3：本用例不实现、不断言它们。下面 ② 里那次「按定位键拼路径读文件」是
// **测试内直接读 spool 文件**，用来固定写入侧契约（写进去的字节 == 事件给的字节）。
func TestAcceptanceA1WriteHalf(t *testing.T) {
	const n = 2000
	root := t.TempDir()
	// 真实 spooler：注入固定 1% 水位的 Gate（newTestSpooler，A8-1 复用），
	// 因此「能落盘」这条断言与宿主磁盘用量无关。
	spooler := newTestSpooler(t, root, "blue")
	require.NoError(t, spooler.FlushAndRoll())

	rec := &orderRecorder{}
	db, _ := newSpoolTestDB(t, rec)
	sink := newAcceptanceSink(spooler, rec)
	obs := &insertObserver{rec: rec, sink: sink}
	require.NoError(t, db.Callback().Create().Before("gorm:create").
		Register("acceptance:insert-batches", obs.record))

	w := gwobs.NewTraceWriterWithSink(db, 4096, 200, 50*time.Millisecond, gwobs.TraceWriterLimits{}, sink)
	// 日志走注入的 Logf：本用例不断言日志，注入只为不把「闸门不可观测」的那条
	// WARN 打到真实标准错误（输出干净），与全局约束「日志断言走注入的 Logf」同源。
	capture := &logCapture{}
	w.ConfigureDegradation(gwobs.DegradationOptions{Logf: capture.logf})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	// 时间戳刻意取「北京 2026-09-25 00:00 起、每条 +1s」这一段**跨日形状**：
	// 这 2000 条事件的 UTC 日期全是 09-24，而 UTC+8 日期是 09-25 ⇒ 「dt 退回 UTC」
	// 或「dt 跟着宿主时区走」都会让下面的期望值对不上。
	// 【为什么不取白天的时刻】北京 2026-09-24 10:00 的 UTC 日期恰好也是 09-24 ⇒
	// 时区口径退回 UTC 的实现在那种取法下**全绿**（假绿）；跨日形状与宿主时区无关，
	// 又能把那类缺陷钉住。时间戳是固定字面量（不用 time.Now）⇒ 与宿主时钟、
	// 分钟/日边界也无关。
	base := time.Date(2026, 9, 25, 0, 0, 0, 0, cstTestZone)
	for i := 0; i < n; i++ {
		w.Emit(gwobs.TraceEvent{
			RequestID:         fmt.Sprintf("req-%05d", i),
			TS:                base.Add(time.Duration(i) * time.Second),
			RequestHeadersRaw: []byte(`{"x-user":"u"}`),
			RequestBodyRaw:    []byte(fmt.Sprintf(`{"i":%d}`, i)),
			ResponseBodyRaw:   []byte(`{"ok":true}`),
		})
	}
	require.Eventually(t, func() bool {
		var c int64
		db.Model(&obsmodel.CallTrace{}).Count(&c)
		return c == n
	}, 30*time.Second, 50*time.Millisecond)
	require.NoError(t, spooler.FlushAndRoll())

	// ⓪ 次序（规格 §10.1 I1 / 增补 A8-2）：每一批的追加都必须**先于**承载它的 INSERT。
	// 期望值按「批次行数」推：在第 k 次 INSERT 发生时，属于它的那 rows 条追加必须
	// 都已经发起（写入器是唯一写协程 ⇒ 已发起 == 已返回）。
	// 【为什么是 >= 而不是 ==】本用例要钉的不变量是「先追加、后 INSERT」这个次序，
	// 不是「追加恰好不多不少」这条批处理形态；写成等号会把将来合法的批处理重构
	// （例如先把一批的行全部准备好）变成假红。
	batches := obs.snapshot()
	calls := sink.callsSnapshot()
	require.NotEmpty(t, batches, "INSERT 观测为空 ⇒ 下面的次序断言会是空的（自证，不是产品行为）")
	rowsSeen := 0
	for k, b := range batches {
		require.Greater(t, b.rows, 0, "第 %d 次 INSERT 的行数必须可观测", k)
		require.GreaterOrEqual(t, b.appendedAt, int64(rowsSeen+b.rows),
			"第 %d 次 INSERT 发生时，本批 %d 行的追加必须都已发起（实际发起 %d 次）", k, b.rows, b.appendedAt)
		require.Less(t, calls[rowsSeen+b.rows-1].seq, b.seq,
			"第 %d 次 INSERT：本批最后一条追加的序号必须小于该 INSERT 的序号", k)
		rowsSeen += b.rows
	}
	require.Equal(t, n, rowsSeen, "各批次行数之和必须等于事件数")
	require.Len(t, calls, n, "每个事件都必须真的走到 sink（追加被发起）")

	var rows []obsmodel.CallTrace
	require.NoError(t, db.Order("ts asc").Find(&rows).Error)
	require.Len(t, rows, n)

	// ① 每一行都必须有完整定位键，且不得有正文列、不得是 body_missing
	for i, row := range rows {
		require.False(t, row.BodyMissing, "第 %d 行不应降级", i)
		require.Nil(t, row.RequestBody, "第 %d 行：新行不得再写正文 jsonb 列", i)
		require.Nil(t, row.RequestHeaders, "第 %d 行：新行不得再写正文 jsonb 列", i)
		require.Nil(t, row.ResponseBody, "第 %d 行：新行不得再写正文 jsonb 列", i)
		require.NotNil(t, row.BodyRefDT, "第 %d 行：定位键不得缺失", i)
		require.NotNil(t, row.BodyRefInst, "第 %d 行：定位键不得缺失", i)
		require.NotNil(t, row.BodyRefPart, "第 %d 行：定位键不得缺失", i)
		require.NotNil(t, row.BodyRefRow, "第 %d 行：定位键不得缺失", i)
		require.Equal(t, "blue", *row.BodyRefInst, "第 %d 行：inst 必须取自 spooler 的配置", i)
		require.Equal(t, "2026-09-25", row.BodyRefDT.Format("2006-01-02"),
			"第 %d 行的 dt 必须是 UTC+8 的日期（这些事件在 UTC 下仍是 09-24）", i)
		require.Equal(t, time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC), row.BodyRefDT.UTC(),
			"第 %d 行的 dt 必须是该日历日的 UTC 零点（PG date 列按会话时区折算）", i)
	}

	// ② 定位键必须能落回真实存在的 spool 行，且字节与事件一致（逐字节）
	//
	// 【这是测试内直接读文件，不是读侧服务】本用例没有构造任何读侧替身，也不验收
	// 点读闭环（属计划 3）；这里只是把写入侧的契约钉死 —— 写进去的字节必须等于
	// 事件给的字节。
	// 【为什么解码后比字节，而不是 Contains】`"i":1` 是 `"i":12` 的前缀 ⇒ 子串断言
	// 在 1..9 上会假绿。这里按 request_id 认行、按 request_body/response_body/
	// request_headers 的**字节**比对。
	fileCache := map[string][]string{}
	for i, row := range rows {
		spoolPath := filepath.Join(root, "spool",
			"dt="+row.BodyRefDT.Format("2006-01-02"), "inst="+*row.BodyRefInst,
			fmt.Sprintf("part-%06d.jsonl", *row.BodyRefPart))
		lines, ok := fileCache[spoolPath]
		if !ok {
			lines = splitLines(t, spoolPath)
			fileCache[spoolPath] = lines
		}
		require.Greater(t, len(lines), *row.BodyRefRow, "第 %d 行：row=%d 越界（块内共 %d 行）",
			i, *row.BodyRefRow, len(lines))

		var decoded struct {
			RequestID      string          `json:"request_id"`
			RequestHeaders json.RawMessage `json:"request_headers"`
			RequestBody    json.RawMessage `json:"request_body"`
			ResponseBody   json.RawMessage `json:"response_body"`
		}
		require.NoError(t, json.Unmarshal([]byte(lines[*row.BodyRefRow]), &decoded),
			"第 %d 行：定位键指到的不是一行完整 JSON", i)
		require.Equal(t, fmt.Sprintf("req-%05d", i), decoded.RequestID,
			"第 %d 行：定位键指到的行不是这一条事件", i)
		require.Equal(t, fmt.Sprintf(`{"i":%d}`, i), string(decoded.RequestBody),
			"第 %d 行：请求正文必须逐字节一致", i)
		require.Equal(t, `{"ok":true}`, string(decoded.ResponseBody),
			"第 %d 行：响应正文必须逐字节一致", i)
		require.Equal(t, `{"x-user":"u"}`, string(decoded.RequestHeaders),
			"第 %d 行：请求头必须逐字节一致", i)
	}

	// ③ body_bytes 口径不含 headers（登记 D42）
	for i, row := range rows {
		require.NotNil(t, row.BodyBytes, "第 %d 行：body_bytes 是审计口径，不得缺失", i)
		want := len(fmt.Sprintf(`{"i":%d}`, i)) + len(`{"ok":true}`)
		require.Equal(t, want, *row.BodyBytes, "第 %d 行 body_bytes 口径不符（不得含 headers）", i)
	}
}

// TestAcceptanceA3DegradesWithoutDiskPressure 是规格 §14.2 A3 的**注入式等价用例**。
//
// 【为什么不真的填满盘】真实盘满不可复现、且会把跑测试的机器写坏；
// 「填满 /datalog」这一条本身属计划 4 的生产验证。这里用可注入的水位函数
// 把系统推到与盘满**同一状态**：闸门闩锁 ⇒ 每一行 body_missing。
// 等价的是「状态」，不是「成因」—— 本用例**不验证真实磁盘满**，也不验证真实
// umount / 挂载消失（那些是计划 4 的部署验收）。
func TestAcceptanceA3DegradesWithoutDiskPressure(t *testing.T) {
	root := t.TempDir()
	// 恒超水位（99% ≥ 90%）⇒ 一次 Tick 即闩锁。探针注入为「恒可写」，
	// 用来证明闩锁**不是**因为探针失败，而是因为水位（等价的是状态，不是成因）。
	gate := gwobs.NewSpoolGate(gwobs.SpoolGateOptions{
		Root:                 root,
		DiskWatermarkPercent: 90,
		ProbeInterval:        time.Minute,
		UsagePercent:         func(string) (float64, error) { return 99, nil },
		ProbeWrite:           func(string) error { return nil },
	})
	gate.Tick(time.Now()) // 未闩锁 → 闩锁
	require.True(t, gate.Latched(), "前置条件：闸门必须已闩锁")

	spooler, err := gwobs.NewTraceSpooler(gwobs.TraceSpoolerOptions{
		Root: root, Inst: "blue", BlockBytes: 1 << 20,
		RollInterval: time.Hour, FlushInterval: time.Minute, Gate: gate,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = spooler.Close() })

	rec := &orderRecorder{}
	db, _ := newSpoolTestDB(t, rec)
	sink := newAcceptanceSink(spooler, rec)

	w := gwobs.NewTraceWriterWithSink(db, 1024, 50, 50*time.Millisecond, gwobs.TraceWriterLimits{}, sink)
	capture := &logCapture{}
	w.ConfigureDegradation(gwobs.DegradationOptions{Logf: capture.logf})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	// 时间戳固定（不用 time.Now）：dt 由生产日期入口从事件 TS 算出 ⇒ 断言与宿主
	// 时钟、分钟/日边界都无关。闩锁与「现在几点」无关。
	const n = 500
	ts := time.Date(2026, 9, 24, 10, 0, 0, 0, cstTestZone)
	start := time.Now()
	for i := 0; i < n; i++ {
		w.Emit(gwobs.TraceEvent{RequestID: fmt.Sprintf("req-%d", i), TS: ts, RequestBodyRaw: []byte(`{"a":1}`)})
	}
	emitElapsed := time.Since(start)

	require.Eventually(t, func() bool {
		var c int64
		db.Model(&obsmodel.CallTrace{}).Count(&c)
		return c == n
	}, 30*time.Second, 20*time.Millisecond)

	// ① 闩锁期间**零落盘**（R9：不重试、不目录扫描）。
	// 【为什么这条排在内容断言之前】「闩锁中仍写盘」会同时污染后面的 body_missing
	// 断言；把磁盘观测量放在最前，那条缺陷就落在**它**要证明的那一条断言上，
	// 而不是被更早的守卫遮蔽（attribution 清晰）。
	// 断言的是整个 spool 目录不存在 ⇒ 与 dt 的具体取值、宿主时钟无关。
	require.NoDirExists(t, filepath.Join(root, "spool"),
		"闩锁中不得尝试写盘：连 spool 目录都不该被建出来")

	// ② 网关继续服务：元数据行照常写入，每一行都降级为 body_missing、定位键全 NULL
	var rows []obsmodel.CallTrace
	require.NoError(t, db.Order("ts asc").Find(&rows).Error)
	require.Len(t, rows, n, "每个事件都必须有一条元数据行（降级只影响正文，不丢 trace）")
	for i, row := range rows {
		require.True(t, row.BodyMissing, "第 %d 行必须降级为 body_missing", i)
		require.Nil(t, row.BodyRefDT, "第 %d 行：降级行的定位键必须全 NULL", i)
		require.Nil(t, row.BodyRefInst, "第 %d 行：降级行的定位键必须全 NULL", i)
		require.Nil(t, row.BodyRefPart, "第 %d 行：降级行的定位键必须全 NULL", i)
		require.Nil(t, row.BodyRefRow, "第 %d 行：降级行的定位键必须全 NULL", i)
		require.Nil(t, row.RequestBody, "第 %d 行：降级行也不得回退写 jsonb 正文列", i)
		require.NotNil(t, row.BodyBytes, "第 %d 行：body_bytes 是审计口径，缺失行也应记录原正文大小", i)
	}

	// ③ 每一行都真的走到过 sink，且全部被闩锁拒绝（不是「根本没试落盘」，
	//    也不是「写盘成功」）。文件名是 ErrSpoolLatched 的包装 ⇒ 用 ErrorIs。
	calls := sink.callsSnapshot()
	require.Len(t, calls, n, "每个事件都必须尝试过一次正文落盘")
	for i, c := range calls {
		require.ErrorIs(t, c.err, gwobs.ErrSpoolLatched, "第 %d 条追加必须被闩锁拒绝", i)
	}

	// ④ 延迟无劣化：热路径 Emit 是纯内存入队（500ms 阻塞上限是既有契约），
	//    500 次 Emit 的总耗时必须在毫秒量级；真正的落盘全在写入协程里。
	require.Less(t, emitElapsed, 500*time.Millisecond,
		"Emit 不得因为落盘失败而退化：热路径必须仍是内存入队（实测 %s）", emitElapsed)

	// ⑤ 降级必须可观测（规格 §9.3）
	st := w.DegradationStats()
	require.True(t, st.Latched, "闩锁状态必须可观测")
	require.Equal(t, int64(n), st.MissingRows, "body_missing 行数必须可观测")
}
