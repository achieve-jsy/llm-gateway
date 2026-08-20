r"""计划 2 / 任务 7 的变异集合（作者编写的产物 ⇒ 必须 git-tracked，见全局约束 §证据制度 1）。

被变异文件是 `TraceWriter` 的**降级可观测性**（`internal/gateway/observability/trace_event.go`；
A7-1 / A7-2 / A7-3）。这一批变异的共同主题：**「盘满/写失败导致正文降级」这件事必须
恰好被看见一次 —— 跳变一条、闩锁期汇总按周期一条、恢复一条，绝不每行一条**，
而且水位那一条路径（SpoolGate.Tick 自行置位）必须真的被覆盖。

编号一经发布不再改动；匹配一律用字符串（不用行号），行号仅供人读。

## 评审修复轮（控制者裁定「需修复」）新增的条目

- `m14_关闭路径也打警告`（I1）：关闭路径（`TRACE_BODY_ROOT` 未设 ⇒ `sink == nil`）必须零新增
  日志；原先的实现会在启动约一个 tick 后打一条**误导性** WARN（接线是有意缺失的，且它承诺的
  AppendLine 兜底在 sink == nil 时不存在）。
- `m15_不认领闸门侧闩锁`（I2）：根目录类闩锁由 spooler 自己的 FlushAndRoll 触发 ⇒ 有
  「闸门已闩锁、写入侧还没失败过」的空闲窗口；不认领则整段闩锁期没有汇总、看板读不到闩锁。
- 两条静态检查补齐「两侧 reason 集合不相交」的**另一半**（Task 5 的 reason 字面量）。

## 相对简报（task-7-brief.md 步骤 5）的如实改动 —— 每条都是实测发现

1. **简报的 m01/m02 定位串在实现里不存在**：简报的字符串按「失败路径自己维护
   degLatched/degLastSum」的草稿写成（`if !w.degLatched {\n\t\tw.degLatched, ... = true, now, now`），
   而控制器裁定 A7-1 的检测必须放在**周期分支**（对 `GateStats().Trips` 做前后比对），
   于是跳变日志有两个来源（失败路径兜底 / 周期比对），字段与分支形态都变了。
   本集合的定位串一律取自最终实现（`--check` 唯一可证）。

2. **简报的实现草稿与它自己的用例互相矛盾（实测红）**：
   - `if !latched && now.Sub(w.degLastSum) >= w.summary()` —— 只在本行**不是**
     `ErrSpoolLatched` 时才打汇总，而 `TestDegradationSummaryIsThrottled` 的 sink
     恒返回 `ErrSpoolLatched` ⇒ 汇总永远不会输出，用例必红；
   - 跳变文案含「恢复条件」，而 `TestDegradationLogsRecovery` 断言
     `countContaining("恢复") == 1` ⇒ 跳变那一行也算一条，必为 2。
   本实现把汇总判据改为 `!w.degLatched || now.Sub(degLastSum) < summaryInterval()`，
   并把跳变文案的解锁条件写成「解锁条件 = 水位回落 且 探针可写」（不含「恢复」）。
   这两条已用**实现前**的 RED 证据记录在报告里（简报草稿版本实测两条用例失败）。

3. **A7-1 的「类型断言窄接口」在生产恒假（我在发货前实测定稿）**：生产的 sink 不是
   `*TraceSpooler`，而是 `cmd/gateway/main.go` 的 `traceBodySink` 组装物（只实现
   AppendLine/DayStart/DayOf）⇒ `sink.(interface{ Gate() *SpoolGate })` 永远失败。
   控制器据此**授权**在 main.go 上只加一个只读访问器
   `GateStats() gwobs.GateStats`（不交出闸门句柄），本集合的 C5 静态行钉住它。
   因此本任务有**两条**水位路径，且各有隔离杀手：
   - 闸门可观测（周期比对，带 Stats 读数）⇒ m05 / m07 / m12；
   - 闸门不可观测（`AppendLine` 上报的 `（原因：disk_watermark: …）` 错误文本）⇒ m06。

4. **m03 的判据形态**：简报写 `if !latched && now.Sub(...) >= w.summary()`；本实现的
   节流条件是 `if !w.degLatched || now.Sub(w.degLastSum) < w.summaryInterval() { return }`，
   故 m03 定位这一条。

## 已知不覆盖（如实登记）

- **闸门解锁（水位回落 + 探针可写）本身**是 Task 4 的状态机，由 Task 4 的证据覆盖；
  本任务只覆盖「解锁后写入侧必须打一条恢复 INFO」（m02 的杀手）。
- **「Task 5 侧确实打了那一条」**不在本文件（`spooler.go` / `spool_gate.go` 是 Task 5 的
  产物，有已绑定的变异证据）。本任务只证明**写入侧零条**（m10）与两侧 reason 集合不相交
  （C1 + C2）。
"""

NAME = "p2-task-7"
TARGET = "internal/gateway/observability/trace_event.go"
# 只用 B 层（tests/，仅公开 API）；本任务不需要触达未导出符号。
PACKAGES = ["./tests/gateway/observability/"]
MANIFEST_FILES = [
    "tests/gateway/observability/trace_event_spool_test.go",
    "tests/gateway/observability/trace_degradation_test.go",
]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/gateway/observability/trace_event_spool_test.go "
    "tests/gateway/observability/trace_degradation_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 用例不依赖数据库（内存 sqlite）/外部二进制/真实磁盘用量/真实挂载/时区数据，没有可 SKIP 的理由。
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# ---- ① 跳变恰好一条（§9.3 的核心：不是「至少一条」）----
# 每一次失败都当成「首次闩锁」⇒ 20 行失败打 20 条跳变日志（正是 2026-09-24 把 / 盘写满
# 的形态）。杀手 TestDegradationLogsLatchExactlyOnce 的 countContaining("闩锁")==1 断言红。
MUTATIONS["m01_每次失败都重打跳变日志"] = [
    ("sub", "\tfirst := !w.degLatched", "\tfirst := true"),
]

# 闩锁期**每个 tick** 都打一条跳变日志（前后比对恒为「变化」）⇒ 闩锁期间不再零条。
# 杀手 TestDegradationLogsWatermarkLatchExactlyOnce 的「闩锁期间 1 条」与
# 「再发 10 行后仍恰好 1 条」断言红。
MUTATIONS["m07_闸门比对恒为变化每tick重打"] = [
    ("sub", "\tprev := w.degGateSeen\n\tchanged := !prev.valid || prev.trips != st.Trips || "
            "prev.latched != st.Latched || prev.reason != st.Reason",
            "\tchanged := true"),
]

# 根目录类（root_unavailable / root-identity-mismatch / append_failed）已由 Task 5 的
# 三处 Trip 调用点按跳变打过；写入侧必须零条。去掉这条抑制 ⇒ 同一个跳变被两侧各打一条。
# 杀手 TestDegradationSuppressesWriterLogsForRootClassLatch 的 countContaining("ERROR")==0 断言红。
MUTATIONS["m10_根目录类不抑制写入侧日志"] = [
    ("sub", "\tif first && cause != latchCauseTask5Logged {", "\tif first {"),
]

# ---- ② 恢复信号（§9.3 的另一半：Task 5 没有恢复日志）----
# 恢复分支的守卫反过来（已闩锁时直接返回）⇒ 解锁后那一条 INFO 永远不会出现。
# 杀手 TestDegradationLogsRecovery 的 countContaining("恢复")==1 断言红（实际 0）。
MUTATIONS["m02_恢复不记录"] = [
    ("sub", "\tw.degMu.Lock()\n\tif !w.degLatched {\n\t\tw.degMu.Unlock()\n\t\treturn\n\t}\n\tw.degLatched = false",
            "\tw.degMu.Lock()\n\tif true {\n\t\tw.degMu.Unlock()\n\t\treturn\n\t}\n\tw.degLatched = false"),
]

# ---- ③ A7-2 周期汇总：按 SummaryInterval 节流、且带齐闸门六字段 ----
# 去掉节流 ⇒ 每个 tick 一条汇总（20ms 一次）⇒ 「未到 SummaryInterval 不得重复输出」断言红。
MUTATIONS["m03_汇总不节流"] = [
    ("sub", "\tif !w.degLatched || now.Sub(w.degLastSum) < w.summaryInterval() {",
            "\tif !w.degLatched {"),
]

# 汇总退回「只在失败路径打」（简报草稿的形态）：闩锁后若没有新行就永远没有汇总 ——
# 而盘满后长时间没有正文可写，正是运维最需要汇总的时刻。
# 杀手 TestDegradationSummaryIsThrottled（推进时钟后 Eventually 等不到汇总）。
# 【评审修复轮更新定位串】I2 把周期动作合并成 observePeriodic 一个入口，这条变异改为
# 「去掉该入口里的汇总调用」—— 语义与原先完全一致（汇总退回只在失败路径打）。
MUTATIONS["m11_汇总只在失败路径打"] = [
    ("sub", "\tw.observeGateTrips()\n\tw.maybeSummaryLog(w.now())",
            "\tw.observeGateTrips()"),
]

# 汇总丢掉闸门字段（只留写入侧计数）⇒ A7-2 的六字段要求破功。
# 杀手 TestDegradationSummaryCarriesGateFields 的逐字段断言红。
MUTATIONS["m08_汇总丢掉闸门字段"] = [
    ("sub", "\t\tw.logf(\"[trace-body] WARN 正文落盘闩锁中，周期汇总：latched=%t latched_seconds=%d trips=%d \"+\n"
            "\t\t\t\"probe_failures=%d usage_percent=%.1f reason=%q body_missing_rows=%d spool_failures=%d\",\n"
            "\t\t\tst.Latched, st.LatchedSeconds, st.Trips, st.ProbeFailures, st.UsagePercent, st.Reason, missing, failures)",
            "\t\t_ = failures\n"
            "\t\tw.logf(\"[trace-body] WARN 正文落盘闩锁中，周期汇总：latched=%t latched_seconds=%d trips=%d body_missing_rows=%d\",\n"
            "\t\t\tst.Latched, st.LatchedSeconds, st.Trips, missing)"),
]

# ---- ④ A7-1 水位路径（闸门可观测：周期比对，带 Stats 读数）----
# 周期检测不打日志 ⇒ 水位闩锁（SpoolGate.Tick 自行置位、没有别的调用方消费的那一类）
# 在写入侧零日志 —— 正是 A7-1 要闭合的静默盲区。
# 杀手 TestDegradationLogsWatermarkLatchExactlyOnce（Eventually 等不到「跳变起因 = 磁盘水位」）。
MUTATIONS["m05_水位跳变周期检测不打日志"] = [
    ("sub", "\tw.logf(\"[trace-body] ERROR 正文落盘已闩锁，开始降级为 body_missing=true（网关继续服务）：\"+\n"
            "\t\t\"跳变起因 = 磁盘水位（reason=%q usage_percent=%.1f trips=%d）；\"+\n"
            "\t\t\"解锁条件 = 水位回落 且 探针可写\", st.Reason, st.UsagePercent, st.Trips)\n",
            ""),
]

# ---- ⑤ A7-1 水位路径（闸门不可观测：错误文本兜底）----
# 兜底分类恒为「无法归因」⇒ 水位闩锁只报一条通用文案，起因与水位读数都丢了。
# 杀手 TestDegradationLogsWatermarkLatchWithoutGateStats 的「跳变起因 = 磁盘水位」断言红。
MUTATIONS["m06_水位起因不按错误文本归因"] = [
    ("sub", "\treturn classifyLatchReason(err.Error()), false", "\treturn latchCauseUnknown, false"),
]

# ---- ⑥ 不静默降级：闸门不可观测必须有一条 Warn ----
# 杀手 TestDegradationWarnsWhenGateStatsUnobservable（等不到那一条 Warn），
# 以及 TestDegradationLogsWatermarkLatchWithoutGateStats 的 Warn 断言。
MUTATIONS["m09_闸门不可观测不告警"] = [
    ("sub", "\tif w.degWarned {", "\tif true {"),
]

# ---- ⑦ 计数器必须真的累加（全局约束：暴露 body_missing 行数 / 闩锁次数 / 闩锁累计秒数）----
# 杀手 TestDegradationLogsLatchExactlyOnce（MissingRows==20 等不到 / Trips==1 断言红）。
MUTATIONS["m04_不累加missingRows"] = [
    ("sub", "\tw.missingRows.Add(1)", "\t_ = w.missingRows"),
]
# 【评审修复轮更新定位串】I2 的 adoptGateLatch 里出现了第二处 `w.degTrips++` ⇒ 带上上下文
# 保证唯一；本条只针对**失败路径**的计数（认领路径的计数由 m15 覆盖）。
MUTATIONS["m12_不累加闩锁跳变次数"] = [
    ("sub", "\t\tw.degLatched, w.degSince, w.degLastSum = true, now, now\n\t\tw.degTrips++",
            "\t\tw.degLatched, w.degSince, w.degLastSum = true, now, now\n\t\t_ = w.degTrips"),
]

# 闩锁起始时刻不记录（degSince 置零值）⇒ LatchedSince 为零值、LatchedSeconds 变成
# 「距公元 1 年」的巨数、恢复日志的时长也失真。
# 杀手 TestDegradationLogsRecovery 的三条确定性断言（LatchedSince 非零 / LatchedSeconds==30 /
# 恢复行含 "30s"）都会红。
MUTATIONS["m13_不记录闩锁起始时刻"] = [
    ("sub", "w.degLatched, w.degSince, w.degLastSum = true, now, now",
            "w.degLatched, w.degSince, w.degLastSum = true, time.Time{}, now"),
]

# ---- 评审 I1：关闭路径（sink == nil）必须零新增日志 ----
# 去掉 observePeriodic 的关闭判断 ⇒ 关闭路径每个进程都会打一条「闸门状态不可观测」的
# 误导性 WARN（它承诺的 AppendLine 兜底在 sink == nil 时并不存在）。
# 杀手 TestDegradationIsSilentWhenFeatureDisabled 的「零行日志」断言红。
MUTATIONS["m14_关闭路径也打警告"] = [
    ("sub", "\tif w.sink == nil {\n\t\treturn\n\t}\n\tw.observeGateTrips()",
            "\tw.observeGateTrips()"),
]

# ---- 评审 I2：必须认领闸门侧自行置位的闩锁（否则空闲窗口里汇总与看板都看不到）----
# 去掉认领 ⇒ 根目录类闩锁 + 无流量时：degLatched 恒 false ⇒ 汇总整段闩锁期一条不打、
# DegradationStats().Latched 恒 false。
# 杀手 TestDegradationSummarizesRootLatchWithoutTraffic。
MUTATIONS["m15_不认领闸门侧闩锁"] = [
    ("sub", "\tif !w.degLatched {\n\t\tw.degLatched, w.degSince, w.degLastSum, w.degReason = true, now, now, reason\n\t\tw.degTrips++\n\t}",
            "\t_, _ = now, reason"),
]

CHECKS = [
    {
        "name": "闸门侧水位 reason 的实际取值（跨文件耦合的「另一半」）",
        "file": "internal/gateway/observability/spool_gate.go",
        "pattern": r"disk_watermark: %s 已用 %.1f%% ≥ %d%%",
        "expect": "present",
        "why": (
            "A7-1 的判据是「reason 属于水位那一类」，而 reason 文本由 SpoolGate.Tick 的未闩锁"
            "分支产生（Task 4 的文件，本任务不得修改）。这条静态行把「闸门侧实际写出的前缀」"
            "钉住：一旦有人改了那边的文案，写入侧的判据就静默失效（水位闩锁又回到零日志），"
            "而任何行为用例都看不见 —— 文件间耦合只能静态钉住。"
        ),
    },
    {
        "name": "写入侧判据常量与闸门侧的取值一致",
        "file": "internal/gateway/observability/trace_event.go",
        "pattern": r'diskWatermarkReasonPrefix = "disk_watermark:"',
        "expect": "present",
        "why": (
            "同上的「这一半」。两条合起来才等于「两侧 reason 集合不相交且判据有效」："
            "水位 = `disk_watermark: `（本文件判据）；Task 5 的三类 = root-identity-mismatch / "
            "root_unavailable / append_failed（Task 5 已按跳变打过日志）。"
        ),
    },
    {
        "name": "Task 5 侧根目录类 reason 字面量（root_unavailable / root-identity-mismatch）",
        "file": "internal/gateway/observability/spooler.go",
        "pattern": r'"root_unavailable"[\s\S]{0,200}"root-identity-mismatch"',
        "expect": "present",
        "why": (
            "评审 M1：「两侧 reason 集合不相交」原先只有水位那一对有静态检查，Task 5 这三类"
            "（spooler.go 的 checkRoot 与写失败分支）没人钉。若那边的文案改了，写入侧的"
            "task5Reason* 判据就失效 ⇒ 同一个跳变会在两侧各打一条 ERROR（破坏 §9.3 的「恰好一条」），"
            "而没有任何用例或检查会变红（Task 5 的日志来自 log.Printf，测试不解析真实 stderr）。"
            "文件间耦合只能静态钉住。"
        ),
    },
    {
        "name": "Task 5 侧写失败 reason 字面量（append_failed）",
        "file": "internal/gateway/observability/spooler.go",
        "pattern": r'Trip\("append_failed"',
        "expect": "present",
        "why": "同上，第三类 reason；写入侧 task5ReasonWriteFailed 的取值必须与它逐字一致。",
    },
    {
        "name": "生产接线暴露只读 GateStats（A7-1 在生产的落点）",
        "file": "cmd/gateway/main.go",
        "pattern": r"func \(s traceBodySink\) GateStats\(\) gwobs\.GateStats",
        "expect": "present",
        "why": (
            "生产的 sink 是 main.go 的 traceBodySink 组装物（不是 *TraceSpooler）⇒ 没有这条只读"
            "访问器，写入侧的周期比对在生产里恒不生效（类型断言恒假），"
            "水位闩锁就只剩兜底路径。返回**值** GWobs.GateStats 而不是 *SpoolGate："
            "正则也钉住「只读、不交出闸门句柄」这一形态。"
        ),
    },
    {
        "name": "A7-1：写入侧对闸门做的是「前后比对」而不是无条件打日志",
        "file": "internal/gateway/observability/trace_event.go",
        "pattern": r"prev\.trips != st\.Trips",
        "expect": "present",
        "why": (
            "A7-1 原文要求的形态：对 Stats().Trips（配合 Latched/Reason）做前后比对。"
            "「比较一个不存在的字段」这种要求行为用例测不到全部语义，故静态钉住比较形态。"
        ),
    },
    {
        "name": "A7-2：汇总格式串含闸门六字段",
        "file": "internal/gateway/observability/trace_event.go",
        "pattern": r"probe_failures=%d usage_percent=%.1f reason=%q",
        "expect": "present",
        "why": (
            "A7-2 的字段清单（Latched / LatchedSeconds / Trips / ProbeFailures / UsagePercent / "
            "reason）里，后三个的格式名只在这里出现一次；m08 已覆盖「整段丢掉」，"
            "这条静态行额外钉住字段名本身（改名同样让看板/告警的解析失效）。"
        ),
    },
    {
        "name": "A7-3 / D51：写入侧不使用冻结包的失败标记行接口",
        "file": "internal/gateway/observability/trace_event.go",
        "pattern": r"AppendFailed",
        "expect": "absent",
        "why": (
            "被拒绝/闩锁的行不消费行号（定位键全 NULL + body_missing=true），因此不需要规格 §7.1 "
            "的 null 标记行（D51）。这是「不存在的东西」，变异表达不了，只能静态钉住；"
            "p2-task-6 也有一条同名检查（两处同时盯着同一个偏离声明）。"
        ),
    },
    {
        "name": "写入侧不驱动闸门/滚动（A5-4 / A6-1：周期动作归 TraceSpooler）",
        "file": "internal/gateway/observability/trace_event.go",
        "pattern": r"FlushAndRoll|Gate\(\)\.(Tick|Trip)",
        "expect": "absent",
        "why": (
            "A5-4：探针（CreateTemp + fsync）只能在**写协程**上跑，而本任务新增的周期动作"
            "（比对 GateStats、打汇总）必须留在只读观测一侧。一旦写入器自己调用 "
            "Gate().Tick/Gate().Trip/FlushAndRoll，探针的 fsync 就跑回 Run 的 goroutine 上，"
            "直接阻塞热路径的 Allow()。删除一个不存在的调用不改变行为 ⇒ 只能静态钉住。"
        ),
    },
]

# 计划要求 → 集合条目的映射表：有缺口（有要求无条目）整轮失败。
REQUIREMENTS = [
    {
        "plan": "规格 §9.3：闩锁跳变时**恰好一条** ERROR 级日志（不是「至少一条」）",
        "mutations": [
            "m01_每次失败都重打跳变日志",
            "m07_闸门比对恒为变化每tick重打",
            "m10_根目录类不抑制写入侧日志",
        ],
        "checks": [],
        "note": (
            "m01 杀「每行一条」（TestDegradationLogsLatchExactlyOnce）；m07 杀「每 tick 一条」"
            "（TestDegradationLogsWatermarkLatchExactlyOnce 的闩锁期零条断言）；m10 杀「两侧各一条」"
            "（TestDegradationSuppressesWriterLogsForRootClassLatch）。三者合起来才等于 §9.3 的"
            "「恰好一条」。"
        ),
    },
    {
        "plan": "增补 A7-1：SpoolGate.Tick 自行置位的水位闩锁（原先零日志）必须被检测并打出起因",
        "mutations": [
            "m05_水位跳变周期检测不打日志",
            "m06_水位起因不按错误文本归因",
            "m07_闸门比对恒为变化每tick重打",
        ],
        "checks": [
            "闸门侧水位 reason 的实际取值（跨文件耦合的「另一半」）",
            "写入侧判据常量与闸门侧的取值一致",
            "生产接线暴露只读 GateStats（A7-1 在生产的落点）",
            "A7-1：写入侧对闸门做的是「前后比对」而不是无条件打日志",
        ],
        "note": (
            "两条水位路径各有**隔离**杀手：闸门可观测的周期比对（m05/m07，杀手 "
            "TestDegradationLogsWatermarkLatchExactlyOnce，断言带 usage_percent=99.0 的 Stats 读数）、"
            "闸门不可观测的错误文本兜底（m06，杀手 "
            "TestDegradationLogsWatermarkLatchWithoutGateStats，断言带错误原文的形态）。"
            "两条用例都走**真实 TraceSpooler + 真实 Run/applyDueTick + 真实 SpoolGate.Tick 水位分支**"
            "（注入 UsagePercent=99，不依赖宿主磁盘）。"
        ),
    },
    {
        "plan": "增补 A7-1：不得与 Task 5 的三处 Trip 调用点重复打日志（§9.3 的「恰好一条」）",
        "mutations": ["m10_根目录类不抑制写入侧日志"],
        "checks": [
            "闸门侧水位 reason 的实际取值（跨文件耦合的「另一半」）",
            "写入侧判据常量与闸门侧的取值一致",
        ],
        "note": (
            "杀手 TestDegradationSuppressesWriterLogsForRootClassLatch：注入必然失败的 RootCheck ⇒ "
            "闸门以 root_unavailable 跳变（Task 5 的 Trip 调用点打日志），写入侧必须零条 ERROR。"
            "两条静态检查一起钉住「两侧 reason 集合不相交」。"
        ),
    },
    {
        "plan": "规格 §9.3：恢复（重新可写）必须有一条明确信号",
        "mutations": ["m02_恢复不记录"],
        "checks": [],
        "note": "杀手 TestDegradationLogsRecovery（闩锁后 sink 转为可写 ⇒ 恰好一条「恢复」）。",
    },
    {
        "plan": "增补 A7-2：闩锁期间按 SummaryInterval 一条汇总，且含 Latched / LatchedSeconds / Trips / ProbeFailures / UsagePercent / reason",
        "mutations": [
            "m03_汇总不节流",
            "m08_汇总丢掉闸门字段",
            "m11_汇总只在失败路径打",
        ],
        "checks": ["A7-2：汇总格式串含闸门六字段"],
        "note": (
            "m03 杀节流（TestDegradationSummaryIsThrottled）；m08 杀字段齐全"
            "（TestDegradationSummaryCarriesGateFields）；m11 杀「只在失败路径打」"
            "（同两条用例：闩锁后没有新行时也必须按周期输出）。"
        ),
    },
    {
        "plan": "规格 §9.3 + 计划 2 全局约束：暴露计数器（body_missing 行数 / 闩锁次数 / 闩锁累计秒数）",
        "mutations": [
            "m04_不累加missingRows",
            "m12_不累加闩锁跳变次数",
            "m13_不记录闩锁起始时刻",
        ],
        "checks": [],
        "note": (
            "杀手都是 TestDegradationLogsLatchExactlyOnce：m04 让 MissingRows 永远为 0"
            "（Eventually 等不到 20），m12 让 Trips 永远为 0（require.Equal(int64(1)) 断言红）。"
            "LatchedSeconds 由**注入时钟**做确定性断言（测试 2：推进 30s ⇒ LatchedSeconds==30、"
            "解锁后归零、恢复行含时长），m13 即它的杀手。"
        ),
    },
    {
        "plan": "控制器裁定：闸门状态不可观测时不得静默降级，必须有一条 Warn",
        "mutations": ["m09_闸门不可观测不告警"],
        "checks": [],
        "note": (
            "杀手 TestDegradationWarnsWhenGateStatsUnobservable（只实现 TraceBodySink 三方法的替身 ⇒ "
            "必须恰好一条、且不每周期刷屏），以及 TestDegradationLogsWatermarkLatchWithoutGateStats。"
        ),
    },
    {
        "plan": "增补 A7-3 / D51：被拒绝/闩锁时不消费行 ⇒ body_missing=true + 定位键全 NULL，且不用 AppendFailed",
        "mutations": [],
        "checks": ["A7-3 / D51：写入侧不使用冻结包的失败标记行接口"],
        "note": (
            "行为面由 TestTraceWriterMarksBodyMissingWhenSpoolFails / "
            "TestTraceWriterLatchedSpoolMarksBodyMissing（Task 6，本集合的清单包含它们）与 "
            "TestDegradationLogsWatermarkLatchExactlyOnce 的 A7-3 断言段覆盖："
            "水位闩锁下的行 BodyMissing=true、四个定位键全 NULL、且不产出任何 spool part"
            "（闩锁在建块之前返回）。「不使用 AppendFailed」是零引用，只能静态钉住。"
        ),
    },
    {
        "plan": "评审 I1：关闭路径（TRACE_BODY_ROOT 未设 ⇒ sink == nil）必须零新增日志（global-constraints 的关闭路径不变量）",
        "mutations": ["m14_关闭路径也打警告"],
        "checks": [],
        "note": (
            "杀手 TestDegradationIsSilentWhenFeatureDisabled：注入日志出口 + 不发 spool + 跑满"
            "十余个写入侧 tick ⇒ 必须零行日志、DegradationStats 全零、正文仍在 jsonb 列。"
        ),
    },
    {
        "plan": "评审 I2：闸门自行置位的根目录类闩锁在空闲窗口也必须可见（周期汇总 + Latched）",
        "mutations": ["m15_不认领闸门侧闩锁"],
        "checks": [],
        "note": (
            "杀手 TestDegradationSummarizesRootLatchWithoutTraffic：零流量 + root_unavailable 闩锁"
            "+ 推进 SummaryInterval ⇒ 仍有汇总（latched=true、reason 与闸门一致），且"
            "DegradationStats().Latched 为 true、LatchedSeconds 按认领时刻起算。"
        ),
    },
    {
        "plan": "评审 M1：两侧 reason 集合不相交（Task 5 那一半也要静态钉住）",
        "mutations": [],
        "checks": [
            "Task 5 侧根目录类 reason 字面量（root_unavailable / root-identity-mismatch）",
            "Task 5 侧写失败 reason 字面量（append_failed）",
        ],
        "note": "水位那一半由本文件前两条静态检查钉住；两条合起来才是完整的不相交声明。",
    },
    {
        "plan": "A5-4 / A6-1：周期动作的所有权（探针与 flush 仍归 TraceSpooler，写入侧只读观测）",
        "mutations": [],
        "checks": ["写入侧不驱动闸门/滚动（A5-4 / A6-1：周期动作归 TraceSpooler）"],
        "note": "「不该出现的调用」是零引用 ⇒ 静态钉住；行为证据在 Task 5 的 spooler 用例与 p2-task-6 集合。",
    },
]

EXPECTED_GREEN = {}
