r"""计划 2 / 任务 6 的变异集合（作者编写的产物 ⇒ 必须 git-tracked，见全局约束 §证据制度 1）。

被变异文件是 `TraceWriter` 的写入路径（`internal/gateway/observability/trace_event.go`）。
这一批变异的共同主题：**「先落 spool、再写 PG 元数据行」这条次序、以及围绕它的
降级列 / 关闭路径 / 日期口径，都必须能被测出来。**

编号一经发布不再改动；匹配一律用字符串（不用行号），行号仅供人读。

## 相对简报（task-6-brief.md 步骤 7）的如实改动 —— 每条都是实测发现

1. **m01 的简报原串产生「编译红」而不是「断言红」**：简报写
   `("sub", "ref, err := tw.sink.AppendLine(dt, line)", "var ref ta.Ref; var err error\n\t\t\t_ = dt")`，
   替换后 `line` 成为**声明未使用** ⇒ `go build` 失败。控制者硬性指令第 1 条把
   「编译失败红冒充断言红」列为必须避免的失效 ⇒ 本集合的 m01 补上 `_ = line`，
   使用例真正跑起来、并由断言（`require.Eventually` 的追加计数）判红。
   同时按控制者指令第 8 条的提示，**另立 m06** 做真反序（把 PG 插入搬到追加之前），
   使「次序守卫本身」也有不依赖控制流变化的隔离证据。

2. **m03 的简报定位串在实现里 0 命中**：简报写
   `"m := traceEventToModelMeta(e)\n\tdt, inst, part, row := ref.DT, ref.Inst, ref.Part, ref.Row"`，
   而简报自己给出的实现是 `inst, part, row := ref.Inst, ref.Part, ref.Row`（没有 `dt` 分量，
   DT 走 `day` 参数）。按实际形态改为定位 `traceEventToModelWithRef` 的首两行，
   并把 `traceEventToModelMeta(e)` 换成 `traceEventToModel(e)`（即新路径也把三个 jsonb
   正文列写回去）——这正是本条要求要钉的行为。

3. **m04 的简报原串（`if tw.sink == nil {` → `if false {`）产生崩溃红**：sink == nil 时走新
   路径会对 nil 接口调方法 ⇒ panic ⇒ 整个测试进程中止，没有任何用例得到终判。
   控制者指令第 1 条要求区分断言红与崩溃红 ⇒ 本集合改为把**关闭分支的组装**换成新路径的
   组装（`traceEventToModel` → `traceEventToModelWithRef`），用例照常跑完并由
   `TestTraceWriterLegacyPathKeepsBodiesInPG` 的 `require.NotNil(row.RequestHeaders)` 判红。

4. **m05 的简报杀手是假绿 —— 必须补跨日时间戳**：简报的
   `TestTraceWriterAppendsSpoolBeforeMetadataRow` 用「北京 09-24 12:00」，
   其 UTC 日期**也是** 09-24 ⇒ `dt` 退回 UTC 时该用例全绿；而简报该用例里收集的
   `rec.appends` 又**没有任何断言**。⇒ 本任务的用例改成北京 **01:00**（UTC 日期是前一天
   09-23）、并对 `rec.appends` 与 `body_ref_dt` 的 UTC 瞬间都加断言；
   A6-4 的跨日用例（真实目录名 `dt=2026-09-25`）是 m05 的第二个杀手。

5. **补 m07（`body_ref_dt` 不经 DayStart 归一）**：`day := e.TS` 会让列里存事件原始瞬间
   （+08:00 的 01:00），而 PG `date` 列按会话时区（生产 = UTC）折算 ⇒ 落成前一天。
   只比 `Format("2006-01-02")` 抓不住它（同一瞬间在不同 Location 下 Format 出的日期可能
   相同）⇒ 用例额外断言 `.UTC()` 等于该日历日的 UTC 零点。

6. **补 m08（交给 Append 的字节被缩进美化）**：A6-4 要求把「行内无裸换行 / 一次 Append ==
   一行」从结构性推理变成运行时证据。冻结包的 `validateLine` 也会拒绝裸换行，所以本集合里
   唯一能杀死该用例的形态是「行构造侧先把行美化」（任何人在写路径上加 `json.Indent`
   都会造成这个后果）⇒ m08 就是它。**如实标注**：该不变量同时由冻结包的结构校验兜底，
   m08 证明的是「用例真的能看出一次追加被劈成两行」，不是「网关侧是唯一守卫」。

7. **补 CHECKS C2–C5（A6-1 / A6-7）**：循环、周期 flush、滚动、探针调度的所有权
   （「写入侧与启动接线都**不得**驱动闸门或滚动」）和「水位经 `DiskWatermarkPercent`
   传入而不自建闸门」都是**不存在的东西**，变异（删除守卫）表达不了 ⇒ 只能静态钉住。

## 命名的坑（A6-5）

`internal/gateway/observability/spool.go` 是 `call_logs` 的 **Redis Stream** spool，
与本任务的文件 spool（`internal/tracearchive`）只是重名、机制完全不同；本集合的每一条
都只谈后者。
"""

NAME = "p2-task-6"
TARGET = "internal/gateway/observability/trace_event.go"
# 只用 B 层（tests/，仅公开 API）；本任务不需要触达未导出符号。
PACKAGES = ["./tests/gateway/observability/"]
MANIFEST_FILES = ["tests/gateway/observability/trace_event_spool_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/gateway/observability/trace_event_spool_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 用例不依赖数据库（内存 sqlite）/外部二进制/真实磁盘用量/真实挂载/时区数据，没有可 SKIP 的理由。
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# ---- ① 次序：先 spool 后 PG（规格 §10.1 I1）----
# 简报 m01 的修正版：追加调用整段移除，但补齐 `_ = line` 以免变成编译红。
# 杀伤面是「追加根本没发生」：appendI 永远到不了 3 ⇒ 用例的 Eventually（同时等
# 三个计数）先失败。它确实改变了控制流，故不能拿它单独声称「次序守卫承重」——
# 那一条由 m06 提供。
MUTATIONS["m01_落盘调用被整段移除"] = [
    ("sub", "\t\tref, err := w.sink.AppendLine(dt, line)",
            "\t\tvar ref ta.Ref\n\t\tvar err error\n\t\t_ = line\n\t\t_ = dt"),
]

# 真反序（隔离次序守卫本身）：把 spool 追加推到 flush 返回时（即 PG 批次插入之后）执行，
# 于是「插入序号 < 追加序号」，`require.Less` 那一行直接失败。
#
# 【为什么延迟体里要判 sink == nil】第一版漏了这个判断，`TestTraceWriterLegacyPathKeepsBodiesInPG`
# （sink == nil）里对 nil 接口调方法 ⇒ panic ⇒ 测试进程中止、后 3 条用例没有终判，
# 整条被工具判成「崩溃红」。控制者指令第 1 条要求每条变异都是**断言红** ⇒ 补上判空。
MUTATIONS["m06_真反序_先写PG后落spool"] = [
    ("sub", "\t\tmodels := w.buildTraceModels(events)",
            "\t\tmodels := w.buildTraceModels(events)\n"
            "\t\tdefer func() {\n"
            "\t\t\tif w.sink == nil {\n"
            "\t\t\t\treturn\n"
            "\t\t\t}\n"
            "\t\t\tfor j := range events {\n"
            "\t\t\t\t_, _ = w.sink.AppendLine(w.sink.DayOf(events[j].TS), traceEventLine(events[j]))\n"
            "\t\t\t}\n"
            "\t\t}()"),
    ("sub", "\t\tref, err := w.sink.AppendLine(dt, line)",
            "\t\tvar ref ta.Ref\n\t\tvar err error\n\t\t_ = line\n\t\t_ = dt"),
]

# ---- ② 降级列（规格 §9.1 / §15.1 R9）----
# 落盘失败时仍然按「成功」组装定位键：用例的 body_missing / 定位键全 NULL 断言必须红。
MUTATIONS["m02_落盘失败不标body_missing"] = [
    ("sub", "\t\t\tmodels = append(models, traceEventToModelMissing(e, bodyBytes))",
            "\t\t\tmodels = append(models, traceEventToModelWithRef(e, day, ta.Ref{DT: dt, Inst: \"blue\"}, bodyBytes))"),
]

# ---- ③ 新行不再写正文列（规格 §6 / 目标 G1）----
# 新路径的第一行改回旧组装（连 jsonb 正文列一起写）⇒ 三列恒 NULL 的断言必须红。
MUTATIONS["m03_新行仍然写正文列"] = [
    ("sub", "\tm := traceEventToModelMeta(e)\n\tinst, part, row := ref.Inst, ref.Part, ref.Row",
            "\tm := traceEventToModel(e)\n\tinst, part, row := ref.Inst, ref.Part, ref.Row"),
]

# ---- ④ 关闭路径（全局约束：TRACE_BODY_ROOT 为空 ⇒ 走既有路径）----
# 关闭分支的组装换成新路径 ⇒ 正文不再进 jsonb 列，回归用例必须红。
MUTATIONS["m04_关闭时也走新路径"] = [
    ("sub", "\t\t\tmodels = append(models, traceEventToModel(e))",
            "\t\t\tmodels = append(models, traceEventToModelWithRef(e, time.Time{}, ta.Ref{}, bodyBytes))"),
]

# ---- ⑤ 日期口径（全局约束：dt = TS 在 UTC+8 下的日期）----
# 目录名分量退回 UTC 日期：跨日事件会落进 `dt=` 前一天，且列/目录不再同源。
MUTATIONS["m05_dt改用UTC"] = [
    ("sub", "\t\tdt := w.sink.DayOf(e.TS)", "\t\tdt := e.TS.UTC().Format(\"2006-01-02\")"),
]

# 列值不经唯一日期入口归一（直接用事件瞬间）：PG date 列按会话时区折算 ⇒ 跨日错位。
MUTATIONS["m07_body_ref_dt不经DayStart归一"] = [
    ("sub", "\t\tday := w.sink.DayStart(e.TS)", "\t\tday := e.TS"),
]

# ---- ⑥ 一次 Append == 一行（A6-4 的运行时证据）----
# 行构造侧把行美化（任何人在写路径上加 json.Indent 都会这样）⇒ 行内出现裸换行，
# 追加被拒 ⇒ spool 文件不存在、用例在读取处失败。
MUTATIONS["m08_交给Append的行被缩进美化"] = [
    ("sub", "\t\tline := traceEventLine(e)",
            "\t\tvar pretty bytes.Buffer\n"
            "\t\t_ = json.Indent(&pretty, traceEventLine(e), \"\", \"  \")\n"
            "\t\tline := pretty.Bytes()"),
]

CHECKS = [
    {
        "name": "写入侧不调用冻结包的失败标记行接口（D51 的偏离声明）",
        "file": "internal/gateway/observability/trace_event.go",
        "pattern": r"AppendFailed",
        "expect": "absent",
        "why": (
            "本计划把失败行的定位键写成 NULL + body_missing=true，读侧永不按行号取它的正文，"
            "因此不需要规格 §7.1 的 null 标记行（D51）。若有人加了该调用，"
            "要么是回到了标记行方案（需要同步改读侧与 D51），要么是误把两种方案混用 —— "
            "两种都必须被看见。这是「不存在的东西」，变异表达不了，只能静态钉住。"
        ),
    },
    {
        "name": "写入侧不驱动闸门/滚动（A6-1：循环归 TraceSpooler）",
        "file": "internal/gateway/observability/trace_event.go",
        "pattern": r"FlushAndRoll|Gate\(\)\.(Tick|Trip)",
        "expect": "absent",
        "why": (
            "A6-1：周期循环、flush、滚动、根目录复检与探针调度**全部**由 TraceSpooler 拥有，"
            "Task 6 只做 `go spooler.Run(ctx)` 与在唯一写协程上调 AppendLine。"
            "一旦写入器自己驱动闸门/滚动，探针的 fsync 就会跑回非写协程（A5-4 失效），"
            "或与 Run 的周期动作重复。删除一个不存在的调用不改变任何行为 ⇒ 只能静态钉住。"
        ),
    },
    {
        "name": "启动接线不驱动闸门/滚动（A6-1）",
        "file": "cmd/gateway/main.go",
        "pattern": r"FlushAndRoll|Gate\(\)\.(Tick|Trip)",
        "expect": "absent",
        "why": (
            "同 A6-1，但落点是启动接线：main 只 `go spooler.Run(ctx)`，不替它 flush/滚动/探针。"
        ),
    },
    {
        "name": "水位经配置层传入 Spooler（A6-7）",
        "file": "cmd/gateway/main.go",
        "pattern": r"DiskWatermarkPercent:\s*cfg\.TraceBody\.DiskWatermarkPercent",
        "expect": "present",
        "why": (
            "A6-7：把 trace_body.disk_watermark_percent 通过 TraceSpoolerOptions 传进去，"
            "让 Spooler 自己构造闸门（身份包装无条件生效）。漏传会让水位静默退回默认 90，"
            "而配置里写的值看上去完全生效 —— 这是「配置与实际不符」的静默失效。"
        ),
    },
    {
        "name": "启动接线不自建闸门（A6-7 / A6-9 第 2 条）",
        "file": "cmd/gateway/main.go",
        "pattern": r"NewSpoolGate",
        "expect": "absent",
        "why": (
            "A6-9 第 2 条：解锁探针的根目录身份包装是构造期闭包，且是**就地改写**调用方 Gate 的 "
            "ProbeWrite；在 main 里自建 Gate 再注入会多一层绕路，而一旦同一个 Gate 被复用/"
            "构造重试，就会二次包装、叠加身份错误 ⇒ 闸门可能永远解不开（归档全停）。"
            "正确做法是把水位交给 Spooler。同样是「不存在的东西」⇒ 静态钉住。"
        ),
    },
]

# 计划要求 → 集合条目的映射表：有缺口（有要求无条目）整轮失败。
REQUIREMENTS = [
    {
        "plan": "规格 §10.1 I1：必须先落 spool、再写元数据行（次序承重）",
        "mutations": ["m01_落盘调用被整段移除", "m06_真反序_先写PG后落spool"],
        "checks": [],
        "note": (
            "m01 杀「追加根本没发生」（Eventually 的计数等不到 3），同时改变了控制流；"
            "m06 把 PG 插入搬到追加之前，杀的正是 `require.Less` 那一行（真反序）。"
            "两条合起来才同时覆盖「必须追加」与「追加必须在插入之前」。"
        ),
    },
    {
        "plan": "规格 §9.1 / §15.1 R9 + A6-2：落盘失败/被拒绝/闩锁 ⇒ body_missing=true、定位键 NULL",
        "mutations": ["m02_落盘失败不标body_missing"],
        "checks": [],
        "note": (
            "杀手 TestTraceWriterMarksBodyMissingWhenSpoolFails（被拒绝）与 "
            "TestTraceWriterLatchedSpoolMarksBodyMissing（闩锁中，另断言不触盘），"
            "以及重试用例里的失败行断言。"
        ),
    },
    {
        "plan": "规格 §6 / 目标 G1：新行的三个正文列恒为 NULL，改写 body_ref_* + body_bytes",
        "mutations": ["m03_新行仍然写正文列"],
        "checks": [],
        "note": "杀手 TestTraceWriterAppendsSpoolBeforeMetadataRow 的 require.Nil(row.RequestBody/RequestHeaders/ResponseBody)。",
    },
    {
        "plan": "计划 2 全局约束：TRACE_BODY_ROOT 为空 ⇒ 功能关闭、走既有路径（逐字段相同）",
        "mutations": ["m04_关闭时也走新路径"],
        "checks": [],
        "note": (
            "简报原串（把关闭分支的守卫整个去掉）会 panic（nil 接口）⇒ 崩溃红、无用例终判，"
            "故改为「关闭分支的组装换成新路径」：杀手 TestTraceWriterLegacyPathKeepsBodiesInPG。"
        ),
    },
    {
        "plan": "计划 2 全局约束：dt = TS 在 UTC+8 下的日期；body_ref_dt 与 dt 同源（唯一日期入口）",
        "mutations": ["m05_dt改用UTC", "m07_body_ref_dt不经DayStart归一"],
        "checks": [],
        "note": (
            "m05 杀目录名口径（杀手 TestTraceWriterAppendsSpoolBeforeMetadataRow 的 dt 列表断言"
            " + TestTraceWriterDtCoversUTCPlus8DayBoundary 的真实目录名）；"
            "m07 杀列值口径（杀手 TestTraceWriterAppendsSpoolBeforeMetadataRow 的 UTC 零点断言）。"
        ),
    },
    {
        "plan": "A6-4① 交接：用落盘证据钉住「行内 ts 的 +08:00」与「dt 的 UTC+8」是同一天",
        "mutations": ["m05_dt改用UTC"],
        "checks": [],
        "note": (
            "杀手 TestTraceWriterDtCoversUTCPlus8DayBoundary：真实 spooler + 真实目录名 "
            "`spool/dt=2026-09-25/…` + 行内 ts `2026-09-25T00:30:00+08:00` 三条一起钉。"
        ),
    },
    {
        "plan": "A6-4② 交接：带缩进的合法正文 ⇒ 一次 Append == 一行（行内无裸换行）",
        "mutations": ["m08_交给Append的行被缩进美化"],
        "checks": [],
        "note": (
            "杀手 TestTraceWriterIndentedBodyStaysOneLine。如实标注：冻结包的 validateLine "
            "也拒绝裸换行，m08 证明的是「用例真的能看出一次追加被劈成两行」，"
            "不声称网关侧是唯一守卫（p2-task-3 的 m07 覆盖序列化侧的结尾换行形态）。"
        ),
    },
    {
        "plan": "规格 §7.1 的失败标记行方案被有意偏离（D51）",
        "mutations": [],
        "checks": ["写入侧不调用冻结包的失败标记行接口（D51 的偏离声明）"],
        "note": "零引用的「不存在的东西」只能静态钉住。",
    },
    {
        "plan": "A6-1 / A6-7：循环与闸门所有权；水位经配置层传入、不自建闸门",
        "mutations": [],
        "checks": [
            "写入侧不驱动闸门/滚动（A6-1：循环归 TraceSpooler）",
            "启动接线不驱动闸门/滚动（A6-1）",
            "水位经配置层传入 Spooler（A6-7）",
            "启动接线不自建闸门（A6-7 / A6-9 第 2 条）",
        ],
        "note": "四条都是「不该存在的调用 / 不该出现的构造」⇒ 静态检查，行为证据在用例与 Task 5 侧。",
    },
]

EXPECTED_GREEN = {}
