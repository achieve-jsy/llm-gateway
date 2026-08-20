package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	obsmodel "github.com/example/llm-gateway/internal/observability/model"
	ta "github.com/example/llm-gateway/internal/tracearchive"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TraceEvent 完整请求/响应记录，异步投递到 TraceWriter
type TraceEvent struct {
	RequestID string
	TS        time.Time

	// 阶段耗时（ms，从请求抵达计）
	StageAuthMs     *int16
	StageRouteMs    *int16
	StageUpstreamMs *int16
	StageTTFBMs     *int // 上游 TTFB（仅流式）：上游请求发出→收到响应头
	StageTTFTMs     *int
	StageTotalMs    *int

	// 原始 JSON body
	RequestHeadersRaw []byte // 过滤后的请求头（JSON map）
	RequestBodyRaw    []byte // 请求体
	ResponseBodyRaw   []byte // 响应体（非流式为完整 JSON；流式为重组后的合成 JSON）

	// BodyRawUntrusted 为 true 表示 RequestBodyRaw 来自"大正文直存"路径（未经 sanitize/校验）。
	// 用于跳过写入侧的 json.Valid 全量校验，避免 30MB 级别的校验阻塞写入协程。
	BodyRawUntrusted bool

	// 错误
	ErrorCode    string
	ErrorMessage string
}

// TraceWriter 批量写入 call_traces 的异步写入器
type TraceWriter struct {
	db       *gorm.DB
	ch       chan TraceEvent
	flushN   int
	interval time.Duration

	// sink 是正文落盘目标（见 TraceBodySink）。nil ⇒ 功能关闭，
	// 走既有路径（正文仍写进本表的 jsonb 列），与本改动之前逐字段相同。
	sink TraceBodySink

	// maxBatchBytes：单次批量 INSERT 的正文字节上限（防止 pgx 简单协议内联出 GB 级 SQL 文本）
	maxBatchBytes int64
	// maxQueueBytes：channel 中待写正文的字节上限（防止大正文在队列中无界滞留）
	maxQueueBytes int64
	// queuedBytes：channel 中待写正文的字节数（各行 len(RequestBodyRaw)+len(ResponseBodyRaw) 之和）。
	// Emit 投递前累加、Run 取走事件后立即递减，用于按字节闸门丢弃过载事件。
	queuedBytes atomic.Int64

	// ---- 降级可观测性（规格 §9.3；计划 2 任务 7 / A7-1 / A7-2）----
	//
	// degMu 保护下面全部非原子字段：noteSpoolFailure / noteSpoolSuccess 在**唯一写协程**
	// 上跑，而 DegradationStats 与周期比对在别的 goroutine 上读（监控、Run 的 ticker）
	// ⇒ 必须有锁（裸 bool / time.Time 会 data race）。
	//
	// degLogf / degNow / degSummary 只在 Run 启动前由 ConfigureDegradation 写一次，
	// 之后只读；其余状态字段一律持 degMu 访问。
	degMu      sync.Mutex
	degLogf    func(format string, args ...any)
	degNow     func() time.Time
	degSummary time.Duration
	degLatched bool
	degSince   time.Time
	degLastSum time.Time
	degTrips   int64
	degReason  string
	// degGateSrc 是 sink 侧「闸门状态可观测」能力（构造期解析一次）；nil ⇒ 不可观测。
	degGateSrc spoolGateStatsSource
	// degGateSeen 是上一次观测到的闸门快照（A7-1 前后比对用的「前」）。
	degGateSeen gateSeen
	// degWarned：「闸门状态不可观测」的 Warn 只打一次。
	degWarned bool
	// missingRows / spoolFailure 是 §9.3 要求的计数器（body_missing 行数 / spool 失败数）。
	missingRows  atomic.Int64
	spoolFailure atomic.Int64

	// done 在 Run 返回**之前**关闭（见 Done）。只由 Run 关闭，且 Run 只会被启动一次。
	//
	// 【为什么必须有它（评审必修 1：关停竞态）】关停时 `cancel()` 会让写入侧的 Run 与
	// spooler 的 Run 各自响应；写入侧要用 WithoutCancel 补写最后一批，而 spooler 若先
	// Close，这一批会撞上 AppendLine 的 closed 检查 ⇒ 全部 body_missing=true + 一条
	// 「正文落盘已闩锁」ERROR（健康盘被误报成磁盘降级）。次序必须由关停编排显式串起来：
	// 先等本通道关闭（最后一批已写完），再 Close spool。
	done chan struct{}
}

// ---------------------------------------------------------------------------
// 降级可观测性（规格 §9.3；计划 2 任务 7）
// ---------------------------------------------------------------------------

const (
	// defaultDegradationSummaryInterval 是闩锁期间周期汇总的默认间隔（零值 ⇒ 5m）。
	defaultDegradationSummaryInterval = 5 * time.Minute

	// diskWatermarkReasonPrefix 是 `SpoolGate.Tick` 在**水位**闩锁分支写入的 reason 前缀
	//（spool_gate.go 的未闩锁分支：`fmt.Sprintf("disk_watermark: %s 已用 %.1f%% ≥ %d%%", …)`）。
	//
	// 【为什么需要它，以及两侧 reason 集合如何做到不相交】闩锁有三类起因，日志归属如下：
	//   - `root-identity-mismatch` / `root_unavailable`（checkRoot）与 `append_failed`
	//     （AppendLine 的写失败）：**Task 5** 已在三处 `Trip` 的返回值上按跳变恰好打一条 ERROR；
	//   - `disk_watermark: …`：由 `SpoolGate.Tick` 自行置位（spool_gate.go 未闩锁分支的
	//     `g.latched = true; g.trips++`），**没有任何调用方消费** ⇒ 归本文件（A7-1）。
	// 因此写入侧只对**水位**这一类打跳变日志，其余保持零条 —— 否则同一个跳变会被两侧
	// 各打一条，§9.3 的「跳变恰好一条」当场破功。
	//
	// 【「恰好一条」的两处已知偏离（评审必修 3.5：显式列出，不留在 ledger 里）】
	//   ① **闸门状态不可观测**（sink 未实现 GateStats）时走错误文本兜底：跳变那一刻的
	//      ERROR 由 noteSpoolFailure 的 default 分支打，**另外** observeGateTrips 还会打
	//      一条 WARN（warnGateUnobservable，只打一次）。那一条 WARN 是「接线错误」的告警，
	//      不属闩锁跳变计数 —— 因此该段的日志条数多于一条，不能声称严格「恰好一条」。
	//      当前生产接线（main.go 的 traceBodySink）实现了 GateStats，该路径只在替身 /
	//      其它接线上出现（p2-task-7 的 m09/m14 守着告警本身）。
	//   ② **水位跳变在两次写侧周期之间已自行解锁**时该跳变零条：周期比对是**快照前后比对**
	//      （observeGateTrips 只在 `st.Latched` 为真时认领），一旦闸门在两次比对之间解锁，
	//      比对看不到 latched=true ⇒ 这一次水位跳变没有任何日志。生产 ProbeInterval=1m
	//      远大于写侧 tick 粒度（main.go 的 interval=2s）时不可达；只有替身注入极小
	//      ProbeInterval 才可能出现（observeGateTrips 的注释与 adoptGateLatch 的
	//      「Trips ≤ 闸门 Stats().Trips」登记了同一边界）。
	diskWatermarkReasonPrefix = "disk_watermark:"

	// 【命名】常量名刻意避开冻结包那个「失败标记行」方法的字面量：p2-task-6 与 p2-task-7 都有
	// 一条 absent 静态检查钉住「不使用标记行方案」（D51），常量名撞上那个子串会让它假红（实测发生过）。
	// task5ReasonRootIdentity / task5ReasonRootUnavailable / task5ReasonWriteFailed 是
	// Task 5 写入的三类 reason（spooler.go 的 checkRoot 与 AppendLine）。它们出现在原因里
	// 就表示「这一条跳变已经由 Task 5 打过日志」⇒ 写入侧不得重复。
	task5ReasonRootIdentity    = "root-identity-mismatch"
	task5ReasonRootUnavailable = "root_unavailable"
	task5ReasonWriteFailed     = "append_failed"
)

// DegradationStats 是降级状态的可观测快照（规格 §9.3：降级必须有明确可观测信号）。
type DegradationStats struct {
	MissingRows   int64 // 累计被标 body_missing 的行数
	SpoolFailures int64 // 累计 spool 写入失败次数（含闩锁期间的每一行）
	Latched       bool
	// LatchedSince 是本次闩锁开始的时刻（未闩锁时为**最近一次**闩锁的开始时刻）。
	LatchedSince time.Time
	// Trips 是写入侧观测到的闩锁跳变次数；LatchedSeconds 是本次闩锁已持续的秒数
	//（未闩锁时为 0）。全局约束要求暴露「body_missing 行数 / 闩锁次数 / 闩锁累计秒数」
	// 三个计数器，后两个就是它们。
	Trips          int64
	LatchedSeconds int64
}

// DegradationOptions 让测试注入日志与时钟；零值 = log.Printf / time.Now / 5m。
type DegradationOptions struct {
	Logf            func(format string, args ...any)
	Now             func() time.Time
	SummaryInterval time.Duration
}

// spoolGateStatsSource 是 sink **可选**实现的只读能力：给出闸门状态快照。
//
// 【为什么是可选窄接口，而不是扩 TraceBodySink】A5-1 第 3 点禁止改动闸门接口；
// 而扩 TraceBodySink 会让组装物编译失败（它们只实现三个方法）。能观测的实现多一个
// 只读访问器即可被识别，其余实现（测试替身、其它接线）完全不受影响。
//
// 【生产接线】`cmd/gateway/main.go` 的 traceBodySink 已实现
// `GateStats() gwobs.GateStats { return s.spooler.Gate().Stats() }`（计划 2 任务 7），
// 只读、**不交出自闸门句柄**。若某个替身/接线不实现它，走错误文本兜底路径，
// 并由 warnGateUnobservable 打一条 Warn（不静默）。
type spoolGateStatsSource interface {
	GateStats() GateStats
}

// gateSeen 是上一次观测到的闸门快照（A7-1 的「前后比对」用的「前」）。
type gateSeen struct {
	valid   bool
	trips   int64
	latched bool
	reason  string
}

// latchCause 是闩锁起因的三分：水位（归本文件）、Task 5 已打日志（不重复）、
// 无法归因（宁可多一条可见的降级信号，也不静默）。
type latchCause int

const (
	latchCauseUnknown latchCause = iota
	latchCauseWatermark
	latchCauseTask5Logged
)

// classifyLatchReason 由 reason 文本判定起因类别。
//
// 【为什么水位用 Contains 而不是 HasPrefix】reason 有两条来源形态：
//   - 闸门快照的 `Reason` 字段：以 `disk_watermark: …` **开头**；
//   - `AppendLine` 上报的错误：`ErrSpoolLatched` 把闸门 reason 逐字裹进
//     `（原因：disk_watermark: …）`，此时它只在**中间**出现。
//
// 用 Contains 同时覆盖两条形态（水位 reason 是 `disk_watermark: ` 加冒号，不会与
// 其它 reason 撞车）。两侧 reason 的归属见 diskWatermarkReasonPrefix 的注释。
func classifyLatchReason(reason string) latchCause {
	if strings.Contains(reason, diskWatermarkReasonPrefix) {
		return latchCauseWatermark
	}
	if strings.Contains(reason, task5ReasonRootIdentity) ||
		strings.Contains(reason, task5ReasonRootUnavailable) ||
		strings.Contains(reason, task5ReasonWriteFailed) {
		return latchCauseTask5Logged
	}
	return latchCauseUnknown
}

// ConfigureDegradation 覆盖降级观测的日志与时钟（生产不调用，用零值）。
// 必须在 Run 启动前调用（字段之后只读）。
func (w *TraceWriter) ConfigureDegradation(opts DegradationOptions) {
	if opts.Logf != nil {
		w.degLogf = opts.Logf
	}
	if opts.Now != nil {
		w.degNow = opts.Now
	}
	if opts.SummaryInterval > 0 {
		w.degSummary = opts.SummaryInterval
	}
}

// DegradationStats 返回降级状态快照（规格 §9.3）。
func (w *TraceWriter) DegradationStats() DegradationStats {
	w.degMu.Lock()
	st := DegradationStats{
		Latched:      w.degLatched,
		LatchedSince: w.degSince,
		Trips:        w.degTrips,
	}
	if w.degLatched {
		st.LatchedSeconds = int64(w.now().Sub(w.degSince).Seconds())
	}
	w.degMu.Unlock()
	st.MissingRows = w.missingRows.Load()
	st.SpoolFailures = w.spoolFailure.Load()
	return st
}

// now 返回注入时钟（零值 ⇒ time.Now）。
func (w *TraceWriter) now() time.Time {
	if w.degNow != nil {
		return w.degNow()
	}
	return time.Now()
}

// summaryInterval 返回汇总间隔（零值 ⇒ 5m）。
func (w *TraceWriter) summaryInterval() time.Duration {
	if w.degSummary > 0 {
		return w.degSummary
	}
	return defaultDegradationSummaryInterval
}

// logf 输出一条降级日志（注入 Logf 优先，生产走标准日志）。
func (w *TraceWriter) logf(format string, args ...any) {
	if w.degLogf != nil {
		w.degLogf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// gateStats 读一次闸门状态快照（不可观测时 ok=false）。
func (w *TraceWriter) gateStats() (GateStats, bool) {
	if w.degGateSrc == nil {
		return GateStats{}, false
	}
	return w.degGateSrc.GateStats(), true
}

// classifyLatch 判定这次失败属于哪一类闩锁起因。闸门可观测时以闸门自己的 reason 为准
// （周期比对用的就是同一份 GateStats）；否则退化为 AppendLine 上报的错误文本。
// 第二个返回值表示判据是否来自闸门快照。
func (w *TraceWriter) classifyLatch(err error) (latchCause, bool) {
	if st, ok := w.gateStats(); ok && st.Latched {
		return classifyLatchReason(st.Reason), true
	}
	return classifyLatchReason(err.Error()), false
}

// noteSpoolFailure 记录一次 spool 失败，并按状态机输出日志：
//
//	未闩锁 → 闩锁：**恰好一条** ERROR（起因、影响、解锁条件）
//	闩锁中        ：每 SummaryInterval **恰好一条**汇总（见 maybeSummaryLog）
//
// 【为什么必须节流】2026-09-24 的事故里，失败路径对每个请求打印完整语句，一天写了
// 244 GB —— 日志本身成了把盘写满的原因。降级日志必须与降级同量级：盘满时每秒可能有
// 上千行降级，绝不能每行一条。
//
// 【日志归属：§9.3 的「恰好一条」不允许两侧同时打】Task 5 已在三处 Trip 调用点按跳变
// 打日志（root-identity-mismatch / root_unavailable / append_failed）；水位那一类
// （disk_watermark）没有任何调用方消费 ⇒ 归本文件（A7-1）。因此：
//   - 闸门可观测且是水位类：这里**不打**，交给周期分支的 observeGateTrips（它是
//     A7-1 原文要求的「Stats() 前后比对」，且闸门可观测时才有 Stats 读数）；
//   - Task 5 已打过的三类：这里**零条**（不重复）；
//   - 无法归因（测试替身 / 只实现三方法的接线）：这里打一条通用 ERROR —— 宁可多一条
//     可见的降级信号，也不要静默。
func (w *TraceWriter) noteSpoolFailure(err error) {
	w.spoolFailure.Add(1)
	w.missingRows.Add(1)
	now := w.now()
	cause, fromGate := w.classifyLatch(err)

	w.degMu.Lock()
	w.degReason = err.Error()
	first := !w.degLatched
	if first {
		w.degLatched, w.degSince, w.degLastSum = true, now, now
		w.degTrips++
	}
	w.degMu.Unlock()

	if first && cause != latchCauseTask5Logged {
		switch {
		case cause == latchCauseWatermark && fromGate:
			// 闸门可观测：水位跳变归周期分支（observeGateTrips），此处不打。
		case cause == latchCauseWatermark:
			w.logf("[trace-body] ERROR 正文落盘已闩锁，开始降级为 body_missing=true（网关继续服务）：%v；"+
				"跳变起因 = 磁盘水位（AppendLine 上报的原因含 %q，原文见上）；"+
				"解锁条件 = 水位回落 且 探针可写", err, diskWatermarkReasonPrefix)
		default:
			w.logf("[trace-body] ERROR 正文落盘已闩锁，开始降级为 body_missing=true（网关继续服务）：%v；"+
				"解锁条件 = 水位回落 且 探针可写", err)
		}
	}
	w.maybeSummaryLog(now)
}

// noteSpoolSuccess 在闩锁后的第一条成功落盘时调用（恢复信号）。
//
// 【为什么必须有这一条】Task 5 只在跳变那一刻打日志，解锁是静默的；没有恢复信号，
// 运维无法从日志判断「降级已经结束」，只能靠猜。
func (w *TraceWriter) noteSpoolSuccess() {
	w.degMu.Lock()
	if !w.degLatched {
		w.degMu.Unlock()
		return
	}
	w.degLatched = false
	since := w.degSince
	w.degMu.Unlock()
	missing := w.missingRows.Load()
	w.logf("[trace-body] INFO 正文落盘已恢复，body_missing 降级结束（本次降级持续 %s，期间累计缺失 %d 行）",
		w.now().Sub(since).Truncate(time.Second), missing)
}

// maybeSummaryLog 在闩锁期间按 SummaryInterval 输出**恰好一条**周期汇总（A7-2）。
// 由失败路径与周期分支共同调用，degLastSum 保证同一个区间里只输出一次。
func (w *TraceWriter) maybeSummaryLog(now time.Time) {
	w.degMu.Lock()
	if !w.degLatched || now.Sub(w.degLastSum) < w.summaryInterval() {
		w.degMu.Unlock()
		return
	}
	w.degLastSum = now
	since := w.degSince
	trips := w.degTrips
	reason := w.degReason
	w.degMu.Unlock()

	missing := w.missingRows.Load()
	failures := w.spoolFailure.Load()
	if st, ok := w.gateStats(); ok {
		// A7-2 的六个闸门字段（latched / latched_seconds / trips / probe_failures /
		// usage_percent / reason）+ 写入侧两个计数。
		w.logf("[trace-body] WARN 正文落盘闩锁中，周期汇总：latched=%t latched_seconds=%d trips=%d "+
			"probe_failures=%d usage_percent=%.1f reason=%q body_missing_rows=%d spool_failures=%d",
			st.Latched, st.LatchedSeconds, st.Trips, st.ProbeFailures, st.UsagePercent, st.Reason, missing, failures)
		return
	}
	// 闸门不可观测：只报写入侧确实知道的字段，**不伪造**闸门侧数值。
	//
	// 【字段名口径（评审 M2）】闸门可观测那一行里的 `trips=` 是**闸门**的 Stats().Trips；
	// 这里拿不到闸门快照，只能用写入侧自己数到的跳变次数 ⇒ 必须换名字，否则看板解析者
	// 会把两个不同来源的计数当成同一个序列。
	w.logf("[trace-body] WARN 正文落盘闩锁中，周期汇总：latched=%t latched_seconds=%d writer_latch_trips=%d "+
		"body_missing_rows=%d spool_failures=%d reason=%q；probe_failures/usage_percent 不可观测（sink 未暴露 GateStats）",
		true, int64(now.Sub(since).Seconds()), trips, missing, failures, reason)
}

// observePeriodic 是写入侧**唯一的**周期可观测性入口（A7-1 的比对 + A7-2 的汇总）。
//
// 【为什么合并成一个入口】关闭路径（`TRACE_BODY_ROOT` 未设 ⇒ `sink == nil`，正文仍写在
// PG 的 jsonb 列里）必须与本计划之前**行为一致**：不许新增任何日志、也不该多做可观测性
// 工作。分开两处调用时，`observeGateTrips` 会在启动约一个 tick 后把「闸门不可观测」的
// WARN 打出来 —— 那条文案不仅多余（接线是有意缺失的），而且**误导**：它承诺的
// 「改由 AppendLine 的错误文本判定」在 `sink == nil` 时根本不存在（buildTraceModels 在
// sink == nil 分支直接走既有路径，连 AppendLine 都不会调用）。合并后只有一个判断点，
// 关闭路径确定性零日志（用例 TestDegradationIsSilentWhenFeatureDisabled 守着它）。
func (w *TraceWriter) observePeriodic() {
	if w.sink == nil {
		return
	}
	w.observeGateTrips()
	w.maybeSummaryLog(w.now())
}

// observeGateTrips 是 A7-1 的周期检测：把**这一次**观测到的闸门快照与上一次比对，
// 发现**水位类**跳变时恰好打一条 ERROR；Task 5 已打过的那三类在本侧保持零日志。
//
// 【为什么必须有它】`SpoolGate.Tick` 的水位闩锁是唯一「自行置位、没有调用方消费」的
// 跳变（spool_gate.go 未闩锁分支的 `g.latched = true; g.trips++`）⇒ 盘满导致的闩锁
// 当前零日志。周期分支是不依赖「下一行何时到达」的观测点。
//
// 【时机口径】闸门真正跳变的时刻仍在写协程的 AppendLine 里（Tick 只从 applyDueTick
// 调用 —— A5-4 禁止 Run 直接 Tick），所以这里不是「更早发现跳变」，而是「不必等到下
// 一次请求就能把它打出来」。
//
// 【恰好一条】判据 = 快照发生变化**且**新原因是水位类：同一闩锁周期里重复观测到同一份
// 快照 ⇒ 不再打；跳变次数/原因变了（新一轮闩锁）⇒ 再打一条。
func (w *TraceWriter) observeGateTrips() {
	if w.degGateSrc == nil {
		w.warnGateUnobservable()
		return
	}
	st := w.degGateSrc.GateStats()

	w.degMu.Lock()
	prev := w.degGateSeen
	changed := !prev.valid || prev.trips != st.Trips || prev.latched != st.Latched || prev.reason != st.Reason
	w.degGateSeen = gateSeen{valid: true, trips: st.Trips, latched: st.Latched, reason: st.Reason}
	w.degMu.Unlock()

	if !changed || !st.Latched {
		return
	}
	// 【认领闸门侧的闩锁（评审 I2）】根目录类闩锁由 TraceSpooler 自己的周期滚动流程
	// （那里的 checkRoot）触发 —— 注意：此处**不能**把那个方法名字面写进来，本文件有一条
	// absent 静态检查禁止出现它（A5-4/A6-1：滚动与探针的所有权在 spooler 一侧）——
	// 它与「写入侧是否在写」无关 ⇒ 完全可能出现
	// 「闸门已闩锁、写入侧 degLatched 仍为 false」的**空闲窗口**（低流量/空闲时最容易发生，
	// D49「运行中挂载消失」正是这个形态）。不认领的话：A7-2 的汇总判据（看 degLatched）
	// 在整个闩锁期一条都不会打，`DegradationStats().Latched` 也恒为 false —— 恰在运维
	// 最需要它的时刻失效，且与「没有新行正是最需要汇总的时刻」的注释自相矛盾。
	//
	// 【为什么不在这里主动解除】解锁的权威信号仍然是「写入侧真的写成功了一次」
	// （noteSpoolSuccess）：只有真的落盘成功才能证明不再是降级状态，而「闸门未闩锁」并不
	// 等价于「现在能写」（API 缝不需要清零；空转期读到 latched=true 是保守而正确的）。
	w.adoptGateLatch(st.Reason)

	// 日志归属不变：水位类由本侧打（Task 5 没有水位这条路径），Task 5 已经按跳变打过的
	// 三类在本侧保持零条（§9.3 的「恰好一条」）。
	if !strings.HasPrefix(st.Reason, diskWatermarkReasonPrefix) {
		return
	}
	w.logf("[trace-body] ERROR 正文落盘已闩锁，开始降级为 body_missing=true（网关继续服务）："+
		"跳变起因 = 磁盘水位（reason=%q usage_percent=%.1f trips=%d）；"+
		"解锁条件 = 水位回落 且 探针可写", st.Reason, st.UsagePercent, st.Trips)
}

// adoptGateLatch 把「闸门侧观测到的闩锁」记入写入侧的降级状态（同一闩锁周期只认第一眼）。
//
// 【Trips 的语义】写入侧数的是「我观察到的闩锁跳变次数」：失败路径的首次失败算一次，
// 周期比对首次看见闸门闩锁也算一次；同一周期里重复观测不算。因此它 ≤ 闸门的
// Stats().Trips（闸门可能在下一次比对之前已经解锁，见报告登记的边界）。
func (w *TraceWriter) adoptGateLatch(reason string) {
	now := w.now()
	w.degMu.Lock()
	if !w.degLatched {
		w.degLatched, w.degSince, w.degLastSum, w.degReason = true, now, now, reason
		w.degTrips++
	}
	w.degMu.Unlock()
}

// warnGateUnobservable 在闸门状态不可观测时**响亮**说明（不得静默降级成「水位跳变
// 没有日志」这个新盲区）。只打一次，避免每周期刷屏；用注入的 Logf，因此可被断言。
func (w *TraceWriter) warnGateUnobservable() {
	w.degMu.Lock()
	if w.degWarned {
		w.degMu.Unlock()
		return
	}
	w.degWarned = true
	w.degMu.Unlock()
	w.logf("[trace-body] WARN 闸门状态不可观测（sink 未暴露 GateStats）：无法在周期循环里判定水位一类跳变，" +
		"改由 AppendLine 的错误文本判定；请检查组装接线（cmd/gateway/main.go 的 traceBodySink）")
}

// TraceWriterLimits 按字节约束写入器，防止单次 flush 的 SQL 文本与队列滞留失控。
type TraceWriterLimits struct {
	MaxBatchBytes int64 // 单次批量 INSERT 的正文字节上限，默认 8 MB
	MaxQueueBytes int64 // channel 中待写正文的字节上限，默认 128 MB
}

const (
	DefaultTraceMaxBatchBytes int64 = 8 << 20   // 8 MB
	DefaultTraceMaxQueueBytes int64 = 128 << 20 // 128 MB
)

// NewTraceWriter 创建 TraceWriter，bufSize = channel 缓冲大小
// 保留既有签名以向后兼容，等价于使用默认字节限额的新构造器。
func NewTraceWriter(db *gorm.DB, bufSize, flushN int, interval time.Duration) *TraceWriter {
	return NewTraceWriterWithLimits(db, bufSize, flushN, interval, TraceWriterLimits{})
}

// NewTraceWriterWithLimits 创建带字节限额的 TraceWriter；limits 中非正的字段取默认值。
func NewTraceWriterWithLimits(db *gorm.DB, bufSize, flushN int, interval time.Duration, limits TraceWriterLimits) *TraceWriter {
	if limits.MaxBatchBytes <= 0 {
		limits.MaxBatchBytes = DefaultTraceMaxBatchBytes
	}
	if limits.MaxQueueBytes <= 0 {
		limits.MaxQueueBytes = DefaultTraceMaxQueueBytes
	}
	return &TraceWriter{
		db:            db,
		ch:            make(chan TraceEvent, bufSize),
		flushN:        flushN,
		interval:      interval,
		maxBatchBytes: limits.MaxBatchBytes,
		maxQueueBytes: limits.MaxQueueBytes,
		done:          make(chan struct{}),
	}
}

// TraceBodySink 是正文落盘目标（生产实现由 main.go 组装：AppendLine 归
// *TraceSpooler，DayStart/DayOf 归 config.TraceBodyConfig）。
//
// 【为什么用接口】「先追加 spool、再写 PG 元数据行」是规格 §10.1 I1 的前提
// （元数据行存在 ⇒ 产物 XOR spool 存在 或 body_missing=true），必须有测试能观测到
// 两个动作的**相对次序**。接口让测试注入一个记录次序的替身
// （tests/gateway/observability/trace_event_spool_test.go 的 orderRecorder），
// 而不必依赖真实磁盘的时序。
type TraceBodySink interface {
	AppendLine(dt string, line []byte) (ta.Ref, error)

	// DayStart 是唯一的日期计算入口，返回该事件归属日历日的 **UTC 零点**，
	// 供 body_ref_dt（PG date 列）使用（理由见 config.TraceBodyConfig.DayStart）。
	DayStart(ts time.Time) time.Time

	// DayOf 拼 dt= 目录名，**必须**实现为 DayStart(ts).Format("2006-01-02")
	// —— 同源才能保证「列里的日期」与「目录名里的日期」永远一致。
	DayOf(ts time.Time) string
}

// NewTraceWriterWithSink 构造带正文落盘目标的写入器。
//
// sink 为 nil 时行为与 NewTraceWriterWithLimits 完全一致（功能关闭路径）。
func NewTraceWriterWithSink(db *gorm.DB, bufSize, flushN int, interval time.Duration,
	limits TraceWriterLimits, sink TraceBodySink) *TraceWriter {
	w := NewTraceWriterWithLimits(db, bufSize, flushN, interval, limits)
	w.sink = sink
	// A7-1 / A7-2：闸门状态是否可观测只在构造期解析一次（sink 之后不变）。
	if src, ok := sink.(spoolGateStatsSource); ok {
		w.degGateSrc = src
	}
	return w
}

// traceEventBodyBytes 返回单条 trace 的原始正文字节数。
// 必须按 TraceEvent 的原始 body 长度统计，不能用 traceEventToModel 转换后的长度
// （转换会 scrub/包装，长度与原始约定不一致）。
func traceEventBodyBytes(e TraceEvent) int64 {
	return int64(len(e.RequestBodyRaw) + len(e.ResponseBodyRaw))
}

// traceEmitBlockTimeout 当 trace channel 满时，Emit 最多阻塞等待一个写槽位的时长。
// 用于吸收瞬时突发（如大量长流式请求在同一窗口集中完成，trace 集中 emit 挤爆 channel）。
const traceEmitBlockTimeout = 500 * time.Millisecond

// traceShutdownFlushTimeout 是关停时写完最后一批 trace 的时间上限。
// 关停路径的 ctx 已取消，必须靠 WithoutCancel + 本超时才能把最后一批真正落库。
const traceShutdownFlushTimeout = 5 * time.Second

// Emit 投递 trace 事件。优先非阻塞快速路径；channel 满时短暂阻塞等待（吸收突发，
// 避免大量并发完成的长请求一起 emit 把 512 槽 channel 挤掉），仅当持续过载（超过
// traceEmitBlockTimeout 仍无空槽）才丢弃——trace 数据非关键路径，不无限阻塞请求。
// 投递前先按字节闸门预占队列额度：queued+n 超过 maxQueueBytes 时直接丢弃，
// 避免大正文在 channel 中无界滞留（字节上限是槽位数之外的额外闸门）。
func (w *TraceWriter) Emit(e TraceEvent) {
	n := traceEventBodyBytes(e)
	if w.queuedBytes.Add(n) > w.maxQueueBytes {
		// 预占失败：回滚计数后丢弃，不能把额度泄漏在计数里
		w.queuedBytes.Add(-n)
		log.Printf("[trace] queue bytes limit exceeded (limit=%d), dropping trace for request_id=%s", w.maxQueueBytes, e.RequestID)
		return
	}
	select {
	case w.ch <- e:
		return
	default:
	}
	select {
	case w.ch <- e:
	case <-time.After(traceEmitBlockTimeout):
		// 投递最终失败：必须归还预占额度，否则计数泄漏会让后续 trace 被永久丢弃
		w.queuedBytes.Add(-n)
		log.Printf("[trace] channel full after %v, dropping trace for request_id=%s", traceEmitBlockTimeout, e.RequestID)
	}
}

// Done 在 Run 返回**之后**关闭：调用方据此在关闭正文 sink（spool）之前，等待 ctx 取消后
// 补写的最后一批真正写完（评审必修 1 的关停竞态）。
//
// 【关停次序为什么必须由调用方串起来】`cancel()` 之后，本 Run 会用 WithoutCancel 补写
// 最后一批（≤flushN 条 / ≤interval），而 `TraceSpooler.Run` 也从同一个 ctx 退出。
// 两条 goroutine 各自 select、**没有任何同步**：spooler 先 Close 时，这一批会撞上
// AppendLine 的 closed 检查 ⇒ 全部 body_missing=true，并经 noteSpoolFailure 打一条
// ERROR 级「正文落盘已闩锁」——盘是健康的，却发出磁盘降级告警（每次优雅重启都可能发生）。
// 因此 `cmd/gateway/main.go` 的关停次序是：cancel → 等本通道 → `traceBodySpooler.Close()`。
//
// 【超时】调用方必须给自己的等待套一个上限（写入侧的关停 flush 自身有
// traceShutdownFlushTimeout 的上限）：超时说明写入侧卡死（DB 挂起等），此时只能照常
// 关闭 spool 并把这一批按降级处理。
func (w *TraceWriter) Done() <-chan struct{} { return w.done }

// Run 消费 channel，批量写入 DB
func (w *TraceWriter) Run(ctx context.Context) {
	// 先注册「收尾信号」：无论从哪条分支返回，Done 都在**最后一批写完之后**才关闭。
	// （defer 在函数返回时执行 ⇒ 下面的 ctx.Done 分支先 flush 完才走到这里。）
	defer func() {
		if w.done != nil {
			close(w.done)
		}
	}()
	// batch 保存**尚未组装**的事件：行构造与 spool 追加都在 flush 时、也就是这条
	// 唯一的写协程上完成（不引入第二条写路径；成本口径见 D54）。
	batch := make([]TraceEvent, 0, w.flushN)
	// batchBytes 为当前批各行的原始正文字节数之和，用于按字节限批（D5）
	var batchBytes int64
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	flush := func(flushCtx context.Context, events []TraceEvent) {
		if len(events) == 0 {
			return
		}
		models := w.buildTraceModels(events)
		if err := w.db.WithContext(flushCtx).
			Clauses(clause.OnConflict{DoNothing: true}).
			Create(&models).Error; err != nil {
			log.Printf("[trace] batch insert error, falling back to per-record: %v", err)
			// 单条坏记录（如 jsonb 含 PG 不接受的 NUL 转义）会导致整批回滚，
			// 降级为逐条插入并跳过坏记录，避免连坐丢弃正常 trace。
			for i := range models {
				if err := w.db.WithContext(flushCtx).
					Clauses(clause.OnConflict{DoNothing: true}).
					Create(&models[i]).Error; err != nil {
					log.Printf("[trace] skip trace request_id=%s: %v", models[i].RequestID, err)
				}
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			// 本分支不接收事件，故无需归还队列额度；batch 中的字节已在各事件出队时归还。
			//
			// 关停时仍要尽力写完最后一批：ctx 此刻已取消，若直接 WithContext(ctx)，
			// 这次写入必然立即失败（且逐条降级同样失败），最后一批 trace 每次关停都会丢。
			// 因此用 WithoutCancel 剥离取消信号，再套一个短超时避免关停被无限拖住。
			flushCtx, cancelFlush := context.WithTimeout(context.WithoutCancel(ctx), traceShutdownFlushTimeout)
			flush(flushCtx, batch)
			cancelFlush()
			return
		case e := <-w.ch:
			// 事件已出队：立即按原始 body 长度归还队列字节额度，
			// 保证任何后续路径（flush 失败、ctx 取消）都不会漏减。
			n := traceEventBodyBytes(e)
			w.queuedBytes.Add(-n)
			batch = append(batch, e)
			batchBytes += n
			if len(batch) >= w.flushN || batchBytes >= w.maxBatchBytes {
				flush(ctx, batch)
				batch = batch[:0]
				batchBytes = 0
			}
		case <-ticker.C:
			flush(ctx, batch)
			batch = batch[:0]
			batchBytes = 0
			// 周期动作（都不在请求路径上）：见 observePeriodic 的注释。
			w.observePeriodic()
		}
	}
}

// buildTraceModels 把一批事件组装成待写入的元数据行，并**先**把正文追加到 spool。
//
// 【次序是本方法的核心不变量（规格 §10.1 I1）】每个事件都是
// 「① AppendLine 成功 ⇒ ③ 组装带定位键的行」；①失败则组装 body_missing 行。
// 反序（先写 PG 再落 spool）会留下一条「元数据说正文在某处、而那个地方什么都没有」
// 的行 —— 读侧会报 ErrNotArchived（500），把一个正常降级伪装成存储故障。
//
// 【本次序保证的是**调用次序**，不是「字节已经落盘」（评审必修 2；计划 §10.1 同步）】
// AppendLine 只把字节写进冻结包的 1 MB bufio 缓冲（internal/tracearchive/writer.go:518），
// 字节真正进入 `write(2)` 由 `PendingSeals()` 里的 Flush 完成，而它由 spooler 的周期
// tick 驱动 ⇒ **最坏延迟 = FlushInterval（默认 1m，规格 §7.1 的上界）**。
// 因此「spool 追加严格早于 PG 元数据行」只在 API 调用次序上成立：这个窗口内进程被
// SIGKILL / OOM / 容器 kill 时，缓冲里的字节从未写出，而已提交的 PG 行仍带着指向
// 不存在正文的 body_ref_* —— 与反序同样的形态（正是计划 D47 登记的残余窗口）。
// 【窗口大小与后果】≤ FlushInterval（默认 1m）；后果 = 该窗口内崩溃的那些行在归档里
// 缺正文（读侧按 (dt, inst, part, row) 取到 ErrNotArchived），**不是**静默读到错误行。
// 收紧它需要在每批之后调用 PendingSeals()（评审已登记为计划 4 的可选项，本计划控制器
// 裁定**不做**）——不得用「已有变异全绿」推断这个窗口不存在：p2-task-6 的变异条目
// 全部只看**调用次序**，一条都覆盖不到崩溃时的耐久性。
//
// 【调用者只有 Run】行构造（含 json.Valid 与 json.Marshal 对 RawMessage 的 compact 校验）
// 与 AppendLine 都发生在唯一的写协程上，本方法不派生任何 goroutine。
func (w *TraceWriter) buildTraceModels(events []TraceEvent) []obsmodel.CallTrace {
	models := make([]obsmodel.CallTrace, 0, len(events))
	for i := range events {
		e := events[i]
		bodyBytes := int(traceEventBodyBytes(e))

		if w.sink == nil {
			// 功能关闭：既有路径，逐字段不变。
			models = append(models, traceEventToModel(e))
			continue
		}

		// ① 先落 spool。次序**承重**（规格 §10.1 I1）。
		line := traceEventLine(e)
		// day 与 dt 同源：DayOf 必须由 DayStart 派生（全局约束的单一事实源）。
		// day 进 body_ref_dt 列（*time.Time，date），dt 进 spool 路径与 ta.Ref。
		day := w.sink.DayStart(e.TS)
		dt := w.sink.DayOf(e.TS)
		ref, err := w.sink.AppendLine(dt, line)
		if err != nil {
			// ② 失败 ⇒ body_missing=true、定位键全 NULL、不重试（R9）。
			// 降级可观测性（§9.3 / A7-1）：失败与恢复各记录一次状态机转移，
			// 跳变只在这里/周期比对里打**恰好一条**（绝不每行一条）。
			w.noteSpoolFailure(err)
			models = append(models, traceEventToModelMissing(e, bodyBytes))
			continue
		}
		// ③ 成功之后才组装带定位键的元数据行。恢复（闩锁 → 可写）在这里记录一条 INFO。
		w.noteSpoolSuccess()
		models = append(models, traceEventToModelWithRef(e, day, ref, bodyBytes))
	}
	return models
}

// traceEventToModelMeta 组装与正文无关的元数据字段（两条路径共用）。
//
// 【为什么必须共用】「功能关闭」与「功能开启」两条路径给出的元数据必须逐字段一致，
// 否则同一份事件会因为开关得到两套数据；公共字段收在这里，一处改、两处生效。
func traceEventToModelMeta(e TraceEvent) obsmodel.CallTrace {
	return obsmodel.CallTrace{
		RequestID:       e.RequestID,
		TS:              e.TS,
		StageAuthMs:     e.StageAuthMs,
		StageRouteMs:    e.StageRouteMs,
		StageUpstreamMs: e.StageUpstreamMs,
		StageTTFBMs:     e.StageTTFBMs,
		StageTTFTMs:     e.StageTTFTMs,
		StageTotalMs:    e.StageTotalMs,
		ErrorCode:       e.ErrorCode,
		ErrorMessage:    e.ErrorMessage,
	}
}

// traceEventToModelWithRef 组装「正文在 spool 里」的元数据行（规格 §6）：
// 三个 jsonb 列恒为 NULL，定位键与 body_bytes 由本函数写入。
//
// day 由调用方从 `sink.DayStart(e.TS)` 取得，与定位 spool 用的
// `dt := sink.DayOf(e.TS)` 同源 ⇒ 列里的日期与目录名里的日期不可能不一致。
//
// BodyRefPart/BodyRefRow/BodyRefDT/BodyRefInst 取指针：part 0 与 row 0 都是
// 合法值，用值类型会让「定位键缺失」与「第 0 块第 0 行」不可区分。
func traceEventToModelWithRef(e TraceEvent, day time.Time, ref ta.Ref, bodyBytes int) obsmodel.CallTrace {
	m := traceEventToModelMeta(e)
	inst, part, row := ref.Inst, ref.Part, ref.Row
	m.BodyRefDT, m.BodyRefInst, m.BodyRefPart, m.BodyRefRow = &day, &inst, &part, &row
	m.BodyBytes = &bodyBytes
	return m
}

// traceEventToModelMissing 组装「正文从未落盘」的元数据行（规格 §9.1 / §15.1 R9）：
// body_missing=true、四个定位键全 NULL。
//
// 【为什么不占行号】失败的追加不消耗行号（冻结包只在两次写入都成功后才推进 rows），
// 而本行的定位键是 NULL ⇒ 读侧永远不会按行号去取它的正文，因此不需要占位行。
func traceEventToModelMissing(e TraceEvent, bodyBytes int) obsmodel.CallTrace {
	m := traceEventToModelMeta(e)
	m.BodyMissing = true
	m.BodyBytes = &bodyBytes
	return m
}

func traceEventToModel(e TraceEvent) obsmodel.CallTrace {
	ct := traceEventToModelMeta(e)
	if len(e.RequestHeadersRaw) > 0 && json.Valid(e.RequestHeadersRaw) {
		ct.RequestHeaders = scrubNULEscapes(e.RequestHeadersRaw)
	}
	// RequestBodyRaw 经过 sanitizeChatRequestBody 处理后始终是合法 JSON；
	// json.Valid 作为防御性校验保留，非法时仍写入（避免因极端兜底路径导致数据丢失）
	if len(e.RequestBodyRaw) > 0 {
		// BodyRawUntrusted（D3）：正文来自「大正文直存」路径（len(body) > MaxParseBytesInTrace），
		// 既未 sanitize 也未校验，且体积可达数十 MB。此处按契约跳过 json.Valid 的全量扫描
		// ——该扫描运行在唯一的写入协程上，会按 body 体积线性阻塞整条 trace 写入链。
		// 极端情况下（客户端发来的超大正文本身不是合法 JSON）该行会被 PG jsonb 拒绝，
		// 由 flush 的逐条降级自然跳过，不影响同批其他记录。
		if e.BodyRawUntrusted || json.Valid(e.RequestBodyRaw) {
			ct.RequestBody = scrubNULEscapes(e.RequestBodyRaw)
		} else {
			// sanitize 兜底路径：原始请求体非合法 JSON（极少发生），
			// 包装为 {"_raw_truncated": "..."} 写入，确保字段不为 null
			wrapped, err := json.Marshal(map[string]string{"_raw_truncated": string(e.RequestBodyRaw[:min(256, len(e.RequestBodyRaw))])})
			if err == nil {
				ct.RequestBody = scrubNULEscapes(wrapped)
			}
		}
	}
	if len(e.ResponseBodyRaw) > 0 && json.Valid(e.ResponseBodyRaw) {
		ct.ResponseBody = scrubNULEscapes(e.ResponseBodyRaw)
	}
	return ct
}

// scrubNULEscapes 将 JSON 字节中的 \u0000 转义替换为 \ufffd（U+FFFD 替换字符）。
// Go 的 json.Marshal 会把字符串里的 NUL 字节编码成 \u0000，
// 而 PostgreSQL jsonb 拒绝该转义（SQLSTATE 22P05），单条坏记录即可让整批写入失败。
func scrubNULEscapes(b []byte) []byte {
	if !bytes.Contains(b, []byte(`\u0000`)) {
		return b
	}
	return bytes.ReplaceAll(b, []byte(`\u0000`), []byte(`\ufffd`))
}
