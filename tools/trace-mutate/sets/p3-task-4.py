"""计划 3 / 任务 4 的变异集合（作者编写的产物 ⇒ 必须 git-tracked，见全局约束第 1 条）。

本任务是**只写测试**（隔离件 / spool 兜底 / 越界 / 跨日跨 part 边界），因此
`TARGET` 指向**任务 2 的服务文件**（同计划 2 任务 8 的形态：变异打在既有生产代码上）：
`internal/observability/service/trace_body_service.go`。用例在
`tests/observability/service/trace_body_quarantine_test.go`，用**真实文件系统**
（`t.TempDir()`）与**真实 `ta.Reader`**（不是任务 2 的替身）—— 补的正是
「服务 ↔ 冻结包」这道接缝。

编号一经发布不再改动；匹配一律用字符串（不用行号），行号仅供人读。

## 相对计划（任务 4 步骤 3）的改动（逐条附理由；**没有任何一条被静默丢弃**）

计划的 m01–m05 共 5 条，本集合**逐字采用**（目标串已逐条核对唯一性），只补两样：
① 集合必备的 `REQUIREMENTS` 映射表（全局约束第 2 条要求 10 个字段）；
② `EXPECTED_GREEN` 为空（本集合**无零覆盖条目**）。

原稿的 m01–m08 里有若干条目标串落在**冻结包**（`partPath := r.paths.PartFile(ref)`、
`SpoolFile`、`isFailedRowMarker`、越界文案 …）或**网关侧**（`t := ts.In(zone)`，
该设计已按 E2 删除），而 `TARGET` 是服务文件 ⇒ 工具会 `PATTERN-MISS`（一条都跑不到）、
「8 条断言红」不可达。本版**全部改为服务文件里由任务 2 逐字钉住的构造**；冻结包/网关侧
的那些要求**降级为 CHECKS 行**（见下），并在 `REQUIREMENTS` 里如实登记为「只有静态证据」。

## 已降级为 CHECKS 的原稿变异（逐条说明，不得静默丢弃）

- 原 m01（隔离件回退读明文）⇒ 服务层**本来就不允许有**该分支 ⇒ `读侧无隔离件分支`
  这条 CHECKS 直接钉住「隔离件处理不得出现在读侧生产代码里」（规格 §7.1 明文）；
- 原 m02（隔离件路径拼成 double suffix）⇒ 该守卫在冻结包的命名/路径精确性里
  （`partName(part, ".jsonl.zst")` 与 `SpoolFile` 的 `.jsonl`）⇒ 由
  `冻结包spool兜底未被移除` 钉住 + 用例里对 `PartFile`/`SpoolFile` 基名的逐字断言；
- 原 m03（摘掉 spool 兜底）、原 m06（摘掉标记行识别）、原 m07（`Paths.SpoolFile` 的
  dt/inst 调换）⇒ 目标在 `internal/tracearchive/` 内，那是**冻结包**（本计划的第一硬约束），
  且工具一次只变异 `TARGET` 一个文件 ⇒ 降级为 `冻结包spool兜底未被移除` 与
  `冻结包标记行识别未被移除` 两条 CHECKS。

**不得**为了让这些变异可跑而改冻结包，也**不得**声称它们有变异证据——`REQUIREMENTS`
里逐条写明「只有静态证据」，本轮交付报告必须原样列出。

## 需要真实 zstd 的用例（SKIP_WHITELIST）

`TestTraceBodyServiceRowOutOfRangeIsNotArchivedNotMissing` 用**真实 Sealer** 封 3 行产物
（`footer.row_count=3`）来考「行号越界 ≠ 正文从未落盘」。本机没有 C 版 zstd CLI 时该用例
整条 `t.Skip` ⇒ 必须登记在 `SKIP_WHITELIST` 里，否则三断言的第 3 条（全层级 SKIP==0）失败。
**⚠️ 本机 zstd 版本与生产标定版本（冻结包 `defaultZstdVersion`）可能不同**：本用例证明的是
「服务对越界产物的分类正确」，**不得**把它当成生产产物形态的验证；权威表须注明本机版本。
m01 / m03 的杀手另有**不需要 zstd** 的 `TestTraceBodyServiceMissingProductAndSpoolIsNotArchived`
与 `TestTraceBodyServiceQuarantineYieldsNotArchived` ⇒ 无 zstd 的机器上 5 条仍必须断言红。
"""

NAME = "p3-task-4"
TARGET = "internal/observability/service/trace_body_service.go"
PACKAGES = ["./tests/observability/service/"]
MANIFEST_FILES = ["tests/observability/service/trace_body_quarantine_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/observability/service/trace_body_quarantine_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 需要真实 zstd（C 版 CLI）的用例；缺 zstd 时整条 SKIP，必须在此登记否则三断言失败。
# 本机（开发机）有 zstd 时它**真跑**（不进 SKIP）。
SKIP_WHITELIST = ["TestTraceBodyServiceRowOutOfRangeIsNotArchivedNotMissing"]
RETIRED = {}

MUTATIONS = {}

# m01 冻结包哨兵被服务层换成 ErrBodyMissing ⇒ 隔离件 / 越界 / 缺文件三类「元数据与存储
#     不一致」被伪装成「正文从未落盘」（规格 §8.1 反复强调要消灭的一类错误）。
#     替换串保留 requestID 的引用 ⇒ 不产生未使用变量、可编译。
#     目标串 `\t\treturn nil, err\n` 在服务文件里唯一（实测 count==1；构造期两条
#     `return nil, errors.New(...)` 不以它为整行起手）。
#     杀手：TestTraceBodyServiceQuarantineYieldsNotArchived
#           + TestTraceBodyServiceMissingProductAndSpoolIsNotArchived（**都不需要 zstd**）
MUTATIONS["m01_哨兵被换成body_missing"] = [
    ("sub", "\t\treturn nil, err\n",
            '\t\treturn nil, fmt.Errorf("%w: %s", ta.ErrBodyMissing, requestID)\n'),
]
# m02 Source 判定被写死成 archive ⇒ spool 兜底路径（未封块的近期正文）的来源诊断失真，
#     排障与压缩比核算会指向错误对象。
#     杀手：TestTraceBodyServiceReadsUnsealedSpool（Source==SourceSpool 断言）
#           + TestTraceBodyServiceCrossDayAndCrossPart（同一断言）
MUTATIONS["m02_source恒为archive"] = [
    ("sub", "return SourceSpool", "return SourceArchive"),
]
# m03 归档错误被降级成 jsonb 回退 ⇒ 越界 / 隔离件被伪装成「正文在 jsonb 列」（返回空正文
#     而不是响亮报错），正是 D55 明文禁止的静默回退。
#     杀手：TestTraceBodyServiceMissingProductAndSpoolIsNotArchived（**不需要 zstd**）
MUTATIONS["m03_归档错误回退jsonb"] = [
    ("sub", "\t\treturn nil, err\n", "\t\treturn s.fromJSONB(trace)\n"),
]
# m04 定位键的 dt 分量被换成 inst 分量 ⇒ 拼出的目录名指向别的目录/不存在，跨日串行。
#     目标串含 3 个空格（与实现行逐字一致，见计划 E15）。
#     杀手：TestTraceBodyServiceCrossDayAndCrossPart
MUTATIONS["m04_dt分量换成inst"] = [
    ("sub", 'DT:   trace.BodyRefDT.Format("2006-01-02"),', "DT:   *trace.BodyRefInst,"),
]
# m05 标记行（AppendFailed 留下的字面量 null 行）的 ErrBodyMissing 被换掉 ⇒ 把「从未落盘」
#     伪装成「存储不一致」；同时证明服务层原样透传该哨兵、没有自己吞掉。
#     杀手：TestTraceBodyServiceMarkerLineIsBodyMissingNotText
MUTATIONS["m05_标记行哨兵被换掉"] = [
    ("sub", "\t\treturn nil, err\n",
            '\t\treturn nil, fmt.Errorf("%w: %s", ta.ErrNotArchived, requestID)\n'),
]

# ---------------------------------------------------------------------------
# 静态检查（本任务的核心：规格 §7.1「不新增分支」与冻结包复用点的静态形式）
# ---------------------------------------------------------------------------
CHECKS = [
    {
        "name": "读侧无隔离件分支",
        "file": "internal/observability/service/trace_body_service.go",
        "pattern": r"quarantine|\.jsonl\.jsonl\.zst",
        "expect": "absent",
        "why": (
            "规格 §7.1：隔离件在 spool 目录、名字是 .jsonl.zst，两条精确路径都不命中它 "
            "⇒ ErrNotArchived；不得为它加分支、也不得回退读明文。"
            "本行同时兜住原稿 m01/m02（目标在冻结包或不存在于服务层）。"
            "**不存在性**要求 ⇒ 变异无法证明「没写」；可违反：服务文件出现上述任一写法即 FAIL（含注释）。"
        ),
    },
    {
        "name": "读侧不自算日期",
        "file": "internal/observability/service/trace_body_service.go",
        "pattern": r"FixedZone|LoadLocation|time\.Date\(|time\.Now\(",
        "expect": "absent",
        "why": (
            "读侧零日期计算（全局约束 + E2）：dt 的唯一事实源是行内 body_ref_dt 列值，"
            "只做 Format(\"2006-01-02\") 布局格式化；自算日期会出现第二套口径。"
            "可违反：服务文件出现任一日期构造/时区定位调用即 FAIL。"
        ),
    },
    {
        "name": "冻结包spool兜底未被移除",
        "file": "internal/tracearchive/reader.go",
        "pattern": r"return readFromSpool\(spoolPath, ref\.Row\)",
        "expect": "present",
        "why": (
            "任务 4 的 spool 兜底/隔离件形态依赖冻结包的这条精确路径（产物不命中 ⇒ 读 spool；"
            "隔离件名 .jsonl.zst 也不命中 ⇒ ErrNotArchived）。**该要求当前只有静态证据**"
            "（服务层无法为冻结包内部行为产出变异，冻结包也不可变异），报告必须逐条列出。"
        ),
    },
    {
        "name": "冻结包标记行识别未被移除",
        "file": "internal/tracearchive/reader.go",
        "pattern": r"if isFailedRowMarker\(line\) \{",
        "expect": "present",
        "why": (
            "标记行 ⇒ ErrBodyMissing 的识别在冻结包内（reader.go 的产物与 spool 两条路径共用）；"
            "服务层只透传该哨兵。**同样只有静态证据**（原因同上），报告须逐条列出。"
        ),
    },
]

# 5 条全部应在原始侧转为断言红（实测见本轮交付报告 §3）；本集合无零覆盖条目。
EXPECTED_GREEN = {}

# ---------------------------------------------------------------------------
# 映射表（任务 4 的四项范围 + 冻结包侧要求的静态形式）
# ---------------------------------------------------------------------------
REQUIREMENTS = [
    {
        "plan": "① 隔离件形态：spool 下的 part-NNNNNN.jsonl.zst（内容是明文）⇒ ErrNotArchived，不是 ErrFooterCorrupt，且不返回明文",
        "mutations": ["m01_哨兵被换成body_missing"],
        "checks": ["读侧无隔离件分支"],
        "note": (
            "杀手 = TestTraceBodyServiceQuarantineYieldsNotArchived（不需要 zstd）。"
            "「隔离件不得有专门分支/回退」是**不存在性**要求 ⇒ 只有静态证据（CHECKS 第 1 行）。"
        ),
    },
    {
        "plan": "① 隔离件命名精确性：名字只由 partName(part, \".jsonl.zst\") 生成（单后缀），双后缀文件不被 spool 兜底读到",
        "mutations": ["m01_哨兵被换成body_missing"],
        "checks": ["冻结包spool兜底未被移除"],
        "note": (
            "杀手 = TestTraceBodyServiceQuarantineNameIsNotDoubleSuffixed（不含变异；"
            "该守卫在冻结包的路径精确性里）+ TestTraceBodyServiceQuarantineYieldsNotArchived。"
            "**冻结包内部行为只有静态证据**。"
        ),
    },
    {
        "plan": "② spool 兜底：未封块的近期正文直接读明文（产物缺失不是错误），逐字节取回且 Source 诊断为 spool",
        "mutations": ["m02_source恒为archive", "m03_归档错误回退jsonb"],
        "checks": ["冻结包spool兜底未被移除"],
        "note": (
            "杀手 = TestTraceBodyServiceReadsUnsealedSpool、TestTraceBodyServiceSpoolFallbackWhenProductAbsent、"
            "TestTraceBodyServiceCrossDayAndCrossPart。「冻结包有没有实现 spool 兜底」不可变异 ⇒ 静态证据。"
        ),
    },
    {
        "plan": "③ 行号越界 vs 真实缺失的语义不同：两者都 ⇒ ErrNotArchived（不是 ErrBodyMissing、不是 404、不是服务层不一致）",
        "mutations": ["m01_哨兵被换成body_missing", "m03_归档错误回退jsonb"],
        "checks": [],
        "note": (
            "杀手 = TestTraceBodyServiceRowOutOfRangeIsNotArchivedNotMissing（需要真实 zstd，"
            "SKIP_WHITELIST 已登记）+ TestTraceBodyServiceMissingProductAndSpoolIsNotArchived"
            "（不需要 zstd ⇒ 无 zstd 的机器上 m01/m03 仍必须断言红）。"
        ),
    },
    {
        "plan": "③ 标记行（AppendFailed 的字面量 null）⇒ ErrBodyMissing，绝不返回字符串 \"null\"；不影响同一 spool 的后续行",
        "mutations": ["m05_标记行哨兵被换掉"],
        "checks": ["冻结包标记行识别未被移除"],
        "note": (
            "杀手 = TestTraceBodyServiceMarkerLineIsBodyMissingNotText。"
            "「冻结包是否识别标记行」不可变异 ⇒ 静态证据。"
        ),
    },
    {
        "plan": "④ 跨 dt / 跨 inst / 跨 part：dt 分量只能取自行内 body_ref_dt 列（读侧零日期计算），三分量任一拼错都会读错文件",
        "mutations": ["m04_dt分量换成inst"],
        "checks": ["读侧不自算日期"],
        "note": (
            "杀手 = TestTraceBodyServiceCrossDayAndCrossPart（8 格：2 dt × 2 inst × 2 part，"
            "每格内容自带唯一指纹并逐字节断言）。"
        ),
    },
    {
        "plan": "规格 §7.1：读侧不为隔离件新增分支或哨兵",
        "mutations": [],
        "checks": ["读侧无隔离件分支"],
        "note": "不存在性要求 ⇒ 只有静态证据。",
    },
    {
        "plan": "冻结包侧的隔离件发现规则 / spool 兜底 / 标记行识别不在服务文件内（工具只变异 TARGET 一个文件）",
        "mutations": [],
        "checks": ["冻结包spool兜底未被移除", "冻结包标记行识别未被移除"],
        "note": (
            "**该要求当前只有静态证据**：冻结包不可变异（本计划第一硬约束），"
            "报告必须逐条列出「哪几条只有静态证据」，不得声称它们有变异证据。"
        ),
    },
]
