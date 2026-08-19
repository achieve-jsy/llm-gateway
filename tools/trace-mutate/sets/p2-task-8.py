r"""计划 2 / 任务 8 的变异集合（**写入侧**；作者编写的产物 ⇒ 必须 git-tracked，见全局约束 §证据制度 1）。

Task 8 是**验收**：本任务只写测试（`tests/gateway/observability/trace_body_acceptance_test.go`），
**没有新生产代码**，因此这里的变异全部打在既有生产代码上 —— 与计划 1 的 `task-8.py`
同一形态。被变异文件是写入侧的组装与降级路径
（`internal/gateway/observability/trace_event.go`）。

【为什么有两个集合文件】变异工具一次只接受**一个 `--target`**，而 A3 的「闩锁期间不得
写盘」这条守卫住在 `spooler.go`：

    tools/trace-mutate/sets/p2-task-8.py          → TARGET = trace_event.go（本文件）
    tools/trace-mutate/sets/p2-task-8-spooler.py  → TARGET = spooler.go

两份共用同一份名字清单（同一批顶层用例），各自产出**一张**权威表，缺一不可。

【每条变异都必须能被**具体顶层测试函数**杀死，且必须是**断言红**】
本集合的两条杀手是：
  - TestAcceptanceA1WriteHalf                 （规格 §14.2 A1 的写入半部）
  - TestAcceptanceA3DegradesWithoutDiskPressure（规格 §14.2 A3 的注入式等价用例）

【相对简报（task-8-brief.md 步骤 4）的如实改动】
1. 简报的 m01/m02/m03 定位串经 `--check` 核过后保留；简报的 m04（闩锁期间仍写盘）
   的目标在 `spooler.go`，按简报自己的要求拆进 `p2-task-8-spooler.py`。
2. **补入 m04/m05/m06/m07/m08/m09**：简报只给 4 条，而本任务的全部验收断言都必须
   能红（控制者标准指令第 1 条）。补入条目的共同理由是「一条断言对应一条杀手」：
   - m04 定位键错位一行 ⇒ A1 ② 的「定位键必须落回**正确**的行」原本没有任何变异；
   - m05 真反序（先 INSERT 后落盘）⇒ 增补 A8-2 要求复用的「共享原子计数器 +
     require.Less」这条次序断言必须有杀手（简报的 4 条里没有覆盖次序）；
   - m06 降级行不记 body_bytes ⇒ A3 的审计口径断言原本没有变异；
   - m07 热路径退化（Emit 内插入 2ms 阻塞）⇒ A3 的「延迟无劣化」门槛原本没有变异；
   - m08 新行仍写 jsonb 正文列 ⇒ A1 ① 的「新行不得再写正文列」原本没有变异；
   - m09 不累计 body_missing 行数 ⇒ A3 的 DegradationStats 断言原本没有变异。
3. **m07 是「合成探针」，如实标注**：它不是一条真实可能被写出来的缺陷，而是把
   「热路径退化成阻塞」这件事**做出来**，用来证明那条延迟门槛（< 500ms）不是恒真。
   体量刻意取 2ms × 500 次 ≈ 1s：远大于门槛，又远小于变异工具固定的包级 30s 超时
   （不会变成超时红）。

编号一经发布不再改动；匹配一律用字符串（不用行号），行号仅供人读。
"""

NAME = "p2-task-8"
TARGET = "internal/gateway/observability/trace_event.go"
# 只列 B 层包：本任务的清单是 B 层用例，若把 ./internal/gateway/observability/ 也列进来，
# 那个包里一个都不匹配 ⇒ go test 打印「[no tests to run]」⇒ 工具判**证据无效**。
PACKAGES = ["./tests/gateway/observability/"]
MANIFEST_FILES = ["tests/gateway/observability/trace_body_acceptance_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/gateway/observability/trace_body_acceptance_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 两条用例不依赖数据库（内存 sqlite）/外部二进制/真实磁盘用量/真实挂载/时区数据，
# 也没有条件性跳过分支 ⇒ 没有可 SKIP 的理由。
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# ---- ① 定位键必须真的写进元数据行 ----
# 杀手 TestAcceptanceA1WriteHalf 的 ①：`require.NotNil(row.BodyRefDT)` 等四条。
# 摘掉赋值后行仍会被写入（BodyMissing=false、正文列为 NULL），只有定位键为空。
MUTATIONS["m01_不写定位键"] = [
    ("sub", "m.BodyRefDT, m.BodyRefInst, m.BodyRefPart, m.BodyRefRow = &day, &inst, &part, &row",
            "_ = day; _ = inst; _ = part; _ = row"),
]

# ---- ② body_bytes 口径不含 headers（登记 D42）----
# 杀手 TestAcceptanceA1WriteHalf 的 ③：期望值 = len(request)+len(response)，
# 事件带 13 字节的请求头 ⇒ 含 headers 的实现必然对不上。
MUTATIONS["m02_body_bytes含headers"] = [
    ("sub", "bodyBytes := int(traceEventBodyBytes(e))",
            "bodyBytes := int(traceEventBodyBytes(e)) + len(e.RequestHeadersRaw)"),
]

# ---- ③ 降级行不得带定位键（R9）----
# 杀手 TestAcceptanceA3DegradesWithoutDiskPressure 的 ②：`require.Nil(row.BodyRefDT)`
# （其后三条定位键同理）。
MUTATIONS["m03_降级行也带定位键"] = [
    ("sub", "m.BodyMissing = true",
            "m.BodyMissing = true\n\tday0, inst0, p0, r0 := time.Unix(0, 0), \"blue\", 0, 0\n"
            "\tm.BodyRefDT, m.BodyRefInst, m.BodyRefPart, m.BodyRefRow = &day0, &inst0, &p0, &r0"),
]

# ---- ④ 定位键必须落回**正确**的行（错位一行是最典型的定位键缺陷）----
# 【合成探针】把返回的 Ref 的行号整体 +1 ⇒ 每一行的定位键都指到下一条事件：
# 杀手 TestAcceptanceA1WriteHalf 的 ②（`request_id` 与 `request_body` 的逐字节比对）。
MUTATIONS["m04_定位键错位一行"] = [
    ("sub", "\t\tref, err := w.sink.AppendLine(dt, line)",
            "\t\tref, err := w.sink.AppendLine(dt, line)\n"
            "\t\tif err == nil {\n"
            "\t\t\tref.Row++\n"
            "\t\t}"),
]

# ---- ⑤ 次序：先 spool 后 PG（规格 §10.1 I1 / 增补 A8-2）----
# 真反序：把追加推到 flush 返回时（即 PG 批次插入**之后**）执行，于是
# 「INSERT 发生时本批追加数」为 0、且追加序号大于 INSERT 序号。
# 杀手 TestAcceptanceA1WriteHalf 的 ⓪：`require.Equal(appendsAt, rowsBefore+rows)`
# 与 `require.Less(calls[...].seq, b.seq)`。
# 【为什么延迟体里要判 sink == nil】与计划 2 Task 6 的 m06 同一理由：A1/A3 都用真实
# sink，但关闭路径的用例也在同一个包里跑（-run 只选本任务两条，此处仍保留判空以免
# 将来清单变宽时对 nil 接口调方法 ⇒ panic ⇒ 崩溃红而不是断言红）。
MUTATIONS["m05_真反序_先写PG后落spool"] = [
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

# ---- ⑥ 降级行仍记 body_bytes（审计口径）----
# 杀手 TestAcceptanceA3DegradesWithoutDiskPressure 的 ②：
# `require.NotNil(row.BodyBytes)`（body_bytes 是「原正文多大」的唯一记录，降级行也必须留）。
# 参数 bodyBytes 变成未使用**形参** ⇒ 不影响编译。
MUTATIONS["m06_降级行不记body_bytes"] = [
    ("sub", "\tm.BodyMissing = true\n\tm.BodyBytes = &bodyBytes",
            "\tm.BodyMissing = true"),
]

# ---- ⑦ 热路径不得退化（A3 的「延迟无劣化」）----
# 【合成探针】在 Emit 入口插入一次 2ms 阻塞：500 次 Emit ≈ 1s ≫ 用例的 500ms 门槛。
# 杀手 TestAcceptanceA3DegradesWithoutDiskPressure 的 ④（`require.Less(emitElapsed, 500ms)`）。
MUTATIONS["m07_热路径退化_Emit内阻塞2毫秒"] = [
    ("sub", "func (w *TraceWriter) Emit(e TraceEvent) {",
            "func (w *TraceWriter) Emit(e TraceEvent) {\n\ttime.Sleep(2 * time.Millisecond)"),
]

# ---- ⑧ 新行不再写正文 jsonb 列（规格 §6 / 目标 G1）----
# 杀手 TestAcceptanceA1WriteHalf 的 ①：`require.Nil(row.RequestBody)`（headers/response 同理）。
MUTATIONS["m08_新行仍写正文列"] = [
    ("sub", "\tm := traceEventToModelMeta(e)\n\tinst, part, row := ref.Inst, ref.Part, ref.Row",
            "\tm := traceEventToModel(e)\n\tinst, part, row := ref.Inst, ref.Part, ref.Row"),
]

# ---- ⑨ 降级计数必须累计（规格 §9.3）----
# 杀手 TestAcceptanceA3DegradesWithoutDiskPressure 的 ⑤：
# `require.Equal(int64(n), st.MissingRows)`（摘掉累加后为 0）。
MUTATIONS["m09_不累计body_missing行数"] = [
    ("sub", "\tw.missingRows.Add(1)\n", ""),
]

# ---------------------------------------------------------------------------
# 静态检查：**变异测不到**的要求必须有静态检查行（全局约束的证据制度第 4 条）。
#
# 本集合的三条都打在**新测试文件**上，钉的是控制者两条硬性语义约束里的
# 「必须写明」部分与 A1 的形态要求。如实标注效力边界：它们匹配的是源码文本
# （注释也算），因此是**启发式**——照抄一句注释就能满足；它们的价值是
# 「删掉声明/换掉真实 spooler 会被看见」，不是「不可能绕过」。
# ---------------------------------------------------------------------------
CHECKS = [
    {
        "name": "A1-用真实 spooler（不是替身）",
        "file": "tests/gateway/observability/trace_body_acceptance_test.go",
        "pattern": r'spooler := newTestSpooler\(t, root, "blue"\)',
        "expect": "present",
        "why": (
            "规格 §14.2 A1 要的是「事件 → spool 行的逐字节对应」⇒ 必须用**真实** "
            "TraceSpooler 落盘（临时目录），不能换成只记次序的替身：替身能测次序，"
            "但测不到「定位键能不能落回真实存在的 spool 行」。"
            "行为侧的另一半（定位键正确性）由 m01/m04 覆盖。"
        ),
    },
    {
        "name": "A3-注入式等价的显式声明",
        "file": "tests/gateway/observability/trace_body_acceptance_test.go",
        "pattern": r"(?s)注入式等价，不是真实盘满.*不验证真实 ENOSPC",
        "expect": "present",
        "why": (
            "控制者硬性语义约束 1：A3 是**注入式等价**验收，不得声称验证了真实磁盘满 —— "
            "真实 ENOSPC / umount 属计划 4。这句话是给人看的交付约束，行为变异测不到"
            "（删掉它测试照样全绿）⇒ 只能静态钉住它确实写在用例里。"
        ),
    },
    {
        "name": "A1-只覆盖写入半部的边界声明",
        "file": "tests/gateway/observability/trace_body_acceptance_test.go",
        "pattern": r"(?s)只覆盖写入半部.*读回半部.*属计划 3",
        "expect": "present",
        "why": (
            "控制者硬性语义约束 2：A1 只做写入半部，读回半部（点读闭环）属计划 3 —— "
            "本任务不得实现或断言读侧。与上一条同理，这是交付边界声明，静态钉住。"
        ),
    },
]

# ---------------------------------------------------------------------------
# 允许的绿：本集合预期**没有**条目（每条变异都应由断言杀死）。
# 若某条变异转绿 ⇒ 整轮失败，必须回来登记理由（[零覆盖] 或 [等价变异]）。
# ---------------------------------------------------------------------------
EXPECTED_GREEN = {}

# ---------------------------------------------------------------------------
# 映射表：计划/控制者写的每条要求 → 集合里的对应条目（工具会校验引用真实存在）。
# ---------------------------------------------------------------------------
REQUIREMENTS = [
    {
        "plan": "规格 §14.2 A1（写入半部）：事件 → spool 行的逐字节对应 + 元数据行的定位键正确",
        "mutations": [
            "m01_不写定位键",
            "m02_body_bytes含headers",
            "m04_定位键错位一行",
            "m08_新行仍写正文列",
        ],
        "checks": ["A1-用真实 spooler（不是替身）", "A1-只覆盖写入半部的边界声明"],
        "note": (
            "m01→① 定位键非空；m04→② 定位键落回**正确**的行（request_id/正文逐字节）；"
            "m02→③ body_bytes 口径（不含 headers，D42）；m08→① 新行不写 jsonb 正文列（G1）。"
            "dt 的口径断言（UTC+8 的日期 + 该日 UTC 零点）由 Task 2 的集合（p2-task-2 的 "
            "m06/m10/m11）与 Task 6 的集合（p2-task-6 的 m05/m07）覆盖；本任务的报告另附"
            "一条**临时时区漂移探针**（TraceBodyZone 偏移 8h→0）实测 A1 的 dt 断言转红。"
        ),
    },
    {
        "plan": "规格 §10.1 I1 / 增补 A8-2：一次 Append 必须严格早于对应元数据行的 INSERT（复用 Task 6 的共享原子计数器 + require.Less）",
        "mutations": ["m05_真反序_先写PG后落spool"],
        "checks": [],
        "note": (
            "杀手 TestAcceptanceA1WriteHalf 的 ⓪：`appendedAt` 相等断言（INSERT 发生时本批"
            "追加必须都已发起）与 `require.Less(追加序号, INSERT 序号)`。"
            "与 Task 6 的 m06 同源（同一手法、同一形态），但本集合独立产出权威表。"
        ),
    },
    {
        "plan": "规格 §14.2 A3（注入式等价）：闩锁 ⇒ 每一行 body_missing、定位键全 NULL、降级仍可观测、延迟无劣化、网关继续服务",
        "mutations": [
            "m03_降级行也带定位键",
            "m06_降级行不记body_bytes",
            "m07_热路径退化_Emit内阻塞2毫秒",
            "m09_不累计body_missing行数",
        ],
        "checks": ["A3-注入式等价的显式声明"],
        "note": (
            "m03→② 降级行定位键必须全 NULL；m06→② body_bytes 审计口径；"
            "m07→④ Emit 延迟门槛（合成探针，如实标注）；m09→⑤ DegradationStats.MissingRows。"
            "「闩锁期间零落盘」与「每一行都被 ErrSpoolLatched 拒绝」的变异打在 spooler.go ⇒ "
            "见 p2-task-8-spooler.py（工具一次只接受一个 --target）。"
        ),
    },
]
