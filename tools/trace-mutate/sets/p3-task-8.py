"""计划 3 / 任务 8 的变异集合（作者编写的产物 ⇒ 必须 git-tracked，见全局约束第 1 条）。

本任务是**只写测试**（A2 生产形态验收），因此 `TARGET` 指向**任务 2 的服务文件**
（与计划 2 任务 8、计划 3 任务 4 同一形态：变异打在既有生产代码上）：
`internal/observability/service/trace_body_service.go`。
用例在 `tests/observability/service/trace_body_acceptance_test.go`，用**真实 `ta.Sealer`**
封块（C 版 zstd CLI）+ **真实 `ta.Reader`** + 内存 sqlite 上的 `call_traces` 行。

编号一经发布不再改动；匹配一律用字符串（不用行号），行号仅供人读。

## 覆盖边界（诚实登记，不是缺口掩盖）

本集合只覆盖**任务 8 的 A2 生产形态缺口 1–3**（完整链路逐字节 / 四态互不混淆 /
跨 part·跨 dt·行号越界的对外表现），以及三条静态不变量。A2 缺口清单里的另外两条
**不在本集合**，**由既有集合承重，不得读成本集合的缺口**：

- 缺口 4（**隔离件形态**）：`p3-task-4` 的
  `TestTraceBodyServiceQuarantineYieldsNotArchived` / `...NameIsNotDoubleSuffixed`；
- 缺口 5（**HTTP 表现与哨兵的对应**）：`p3-task-3`（映射层）+ `p3-task-5`（端点）；
- 库层的「封块产物可点读 + 索引正确」：**计划 1 已覆盖**，本计划不重验
  （`TestEndToEndRoundTripByteExact` / `TestFooterGoldenBytes` / R11 v3 的
  `task-7.py` 32 条 + `task-8-reader.py` 6 条）。

## 相对计划（任务 8 步骤 3）的改动（逐条附理由；没有任何一条被静默丢弃）

计划的 m01–m03 与 3 行 `CHECKS` **逐字采用**（目标串已逐条核对唯一性：三者在本文件里
`count == 1`，实测）。只补两样：

① 集合必备的 `REQUIREMENTS` 映射表（全局约束第 2 条要求 10 个字段；计划草稿没给）；
② `EXPECTED_GREEN` 为空（本集合**无零覆盖条目**）。

**⚠️ 一处与计划正文不同的地方（必须登记，不得静默）**：`SKIP_WHITELIST` 从计划写的
**2 条**扩到 **3 条**（新增 `TestAcceptanceA2StateMatrixMutuallyExclusive`）。理由：计划
原文那 2 条是「依赖真实封块」的用例，而**四态互不混淆**那条的 `archive` 格同样必须用
**真实封块产物**（它的验收点逐字是 `archive ⇒ (nil, SourceArchive)`，用 spool 替身会
把这一格测成 `SourceSpool`）⇒ 无 zstd 的机器上它也会整条 SKIP。**计划原文把
「所有可能 SKIP 的用例」列全了这一点不成立** —— 不补这一条，三断言的第 3 条
（全层级 `SKIP == 0`）在无 zstd 的机器上会失败、整轮判无效。本机有 zstd（v1.5.7）⇒
三条**都不进 SKIP**，白名单在本轮不生效（它是给无 zstd 机器的护栏）。

## 效力边界（写进报告，不得升级）

- 本机 zstd = **v1.5.7**，生产标定版 = 冻结包 `defaultZstdVersion = "1.4.8"`
  ⇒ 本集合证明的是「服务 ↔ 冻结包」在**合法产物**上的正确性，
  **不是**生产产物形态、也不是压缩比档位的验收；
- `CallTrace` 落**内存 sqlite**，不是真实 PG ⇒ 真实 PG + 二进制级路径属**任务 8 步骤 6**
  （CLI A/B/C 组），不属本集合。
"""

NAME = "p3-task-8"
TARGET = "internal/observability/service/trace_body_service.go"
PACKAGES = ["./tests/observability/service/"]
MANIFEST_FILES = ["tests/observability/service/trace_body_acceptance_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/observability/service/trace_body_acceptance_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 需要真实封块（C 版 zstd CLI）的用例；缺 zstd 时整条 SKIP，必须在此登记否则三断言失败。
# 本机（开发机）有 zstd v1.5.7 时它们**真跑**（不进 SKIP）。
SKIP_WHITELIST = [
    "TestAcceptanceA2ProductionRoundTrip",
    "TestAcceptanceA2IndexBoundaries",
    "TestAcceptanceA2StateMatrixMutuallyExclusive",  # 计划原文遗漏，见文件头「改动 ② 前的 ⚠️」
]
RETIRED = {}

MUTATIONS = {}

# m01 定位键的 part 分量被写死为 0 ⇒ 跨 part 串文件（索引正确性破裂）。
#     替换串保留 `trace.BodyRefPart` 的**类型**信息之外的一切：`Part: 0,` 是 int 字面量，
#     与字段类型一致 ⇒ 可编译（不会变成编译红）。`trace` 变量仍被其它三行引用 ⇒ 无未使用变量。
#     【与任务 2 / 任务 4 的同类变异的关系】语义同族，但**杀手不同**：这里由 A2 的
#     跨 part 抽样（partsSeen >= 2 + 逐字节）固定，而不是由 spool 兜底的 Source 断言固定。
#     杀手：TestAcceptanceA2ProductionRoundTrip（partsSeen 断言与逐字节断言）
MUTATIONS["m01_part分量写死为0"] = [
    ("sub", "Part: *trace.BodyRefPart,", "Part: 0,"),
]
# m02 定位键的 dt 分量被换成 inst 分量 ⇒ 跨日串目录 / 直接校验失败。
#     `*trace.BodyRefInst` 是 *string 而 DT 字段是 string ⇒ 替换串必须**解引用**，
#     否则是编译红（全局约束第 5 条：编译红不算通过）。这里写成 `*(trace.BodyRefInst)`。
#     杀手：TestAcceptanceA2IndexBoundaries（跨日那两条 cell 的逐字节断言）
MUTATIONS["m02_dt分量换成inst"] = [
    ("sub", 'DT:   trace.BodyRefDT.Format("2006-01-02"),', "DT:   *(trace.BodyRefInst),"),
]
# m03 行号越界（冻结包返回 ErrNotArchived）被换成 ErrBodyMissing
#     ⇒「元数据与存储不一致」被伪装成「正文从未落盘」（A2 最承重的一条）。
#     替换串保留 requestID 的引用 ⇒ 不产生未使用变量、可编译。
#     目标串 `\t\treturn nil, err\n` 在本文件里唯一（实测 count==1；构造期两条
#     `return nil, errors.New(...)` 不以它为整行起手）。
#     杀手：TestAcceptanceA2IndexBoundaries（越界两格）
#           + TestAcceptanceA2StateMatrixMutuallyExclusive（互异性断言）
MUTATIONS["m03_越界报成body_missing"] = [
    ("sub", "\t\treturn nil, err\n",
            '\t\treturn nil, fmt.Errorf("%w: %s", ta.ErrBodyMissing, requestID)\n'),
]

CHECKS = [
    {
        "name": "读侧不自算日期",
        "file": "internal/observability/service/trace_body_service.go",
        "pattern": r"FixedZone|LoadLocation|time\.Date\(|time\.Now\(",
        "expect": "absent",
        "why": (
            "A2 的跨日索引正确性以「dt 取自行内 body_ref_dt 列」为前提（登记 E2）：读侧自算"
            "日期会让跨日断言失去意义（两个 dt 目录的取值会随宿主机时区/当前时刻漂移）。"
            "可违反：本文件出现四种日期计算形态之一即 FAIL（含注释里的同名文本）。"
        ),
    },
    {
        "name": "冻结包GetBody签名未变",
        "file": "internal/tracearchive/reader.go",
        "pattern": r"func \(r \*Reader\) GetBody\(ctx context\.Context, ref Ref\) \(\[\]byte, error\)",
        "expect": "present",
        "why": (
            "A2 的全链路逐字节以这个复用点为前提；整目录零改动的权威判据是任务 9 的 git diff（D59）。"
            "冻结包不可变异（第一硬约束）⇒ 只能静态钉住。"
        ),
    },
    # 【E8 / H18】读侧窗口上限是冻结包 defaultZstdWindowBytes 的手抄副本，而 sealer 的窗口
    # 由计划 4 独立声明 ⇒ 读侧唯一能做的「同源」证据是钉住**冻结包侧的值**：一旦写入侧窗口
    # 被提升（受控变更 + R11 v4），这一行必须转红，强迫同一次改动回看
    # trace_body_read.go 的镜像常量与读侧配置（否则会出现「sealer 写得出、读侧解不开」
    # 且被报成 500 internal_error 的静默不兼容）。这是**绊线**，不是「窗口一致」的证明 ——
    # 报告不得把它写成端到端验收（见任务 8「跨计划耦合」块的残余缺口）。
    {
        "name": "冻结包默认窗口未被静默改动",
        "file": "internal/tracearchive/defaults.go",
        "pattern": r"defaultZstdWindowBytes = 512 << 20",
        "expect": "present",
        "why": (
            "读侧 traceBodyWriteSideWindowMB=512（internal/controlplane/config/trace_body_read.go:29）"
            "是它唯一的手抄副本；改这个值而不动读侧 = 每一次归档点读都变成未分类的 500（E8）。"
        ),
    },
]

# 3 条全部应在原始侧转为断言红（实测见本轮交付报告 §3）。
EXPECTED_GREEN = {}

# ---------------------------------------------------------------------------
# 映射表（任务 8 步骤 1 的三条用例 + 三条静态不变量）
# ---------------------------------------------------------------------------
REQUIREMENTS = [
    {
        "plan": "A2 缺口 1：`request_id` → PG 行 → 定位键 → `GetBody` 的**完整链路**逐字节"
                  "（含「抽样覆盖 ≥ 2 个 part」这条索引正确性的承重断言）",
        "mutations": ["m01_part分量写死为0"],
        "checks": [],
        "note": (
            "杀手 = TestAcceptanceA2ProductionRoundTrip。"
            "m01 让 part 分量恒为 0 ⇒ 跨 part 的行读到错正文（**逐字节**断言红）。"
            "⚠️ **不得**把「抽样覆盖 >= 2 个 part」那一行记成独立杀手：本轮隔离探针 R7"
            "（只把服务导出的 `Ref.Part` 写死为 0、正文路径不动）实测是**上一条 "
            "`require.Equal(…, res.Ref)` 转红**，覆盖行**未被触到** —— 它读的就是那个已被"
            "逐字钉住的 `res.Ref.Part`，逻辑上被蕴含（遮蔽效应）。该行的真实身份是"
            "**测试夹具的守卫**（防后人把 parts 改成 1 而让「跨 part」命题悄悄退化），"
            "已在用例注释与交付报告 §3 R7 里如实标注。"
        ),
    },
    {
        "plan": "A2 缺口 3（跨日）：两个 dt 目录各取回自己的行，不串目录（dt 的唯一事实源"
                  "是行内 body_ref_dt 列）",
        "mutations": ["m02_dt分量换成inst"],
        "checks": ["读侧不自算日期"],
        "note": (
            "杀手 = TestAcceptanceA2IndexBoundaries（跨日两格）。"
            "m02 把 DT 换成 inst 分量 ⇒ 路径落到 inst=<blue>（不是日期）⇒ 该格读不到自己的行。"
            "「读侧不自算日期」是**静态**要求（变异杀不掉「没写 time.Now」），由 CHECKS 承重。"
        ),
    },
    {
        "plan": "A2 缺口 3（跨 part）：同一 dt 的 part-000000 与 part-000001 抽样不串文件",
        "mutations": ["m01_part分量写死为0"],
        "checks": [],
        "note": (
            "杀手 = TestAcceptanceA2IndexBoundaries（跨 part 两格）+ "
            "TestAcceptanceA2ProductionRoundTrip。m01 使 part-000001 的行也去读 part-000000。"
        ),
    },
    {
        "plan": "A2 缺口 3（行号越界）：row == row_count 与 row == math.MaxInt32 ⇒ "
                  "ErrNotArchived，且**不是** ErrBodyMissing（存储不一致不得伪装成业务事实）",
        "mutations": ["m03_越界报成body_missing"],
        "checks": [],
        "note": (
            "杀手 = TestAcceptanceA2IndexBoundaries（越界两格，逐条断言 ErrorIs/NotErrorIs）"
            "+ TestAcceptanceA2StateMatrixMutuallyExclusive（互异性断言）。"
            "m03 直接把透传改成包 ErrBodyMissing ⇒ 两处各自独立转红。"
        ),
    },
    {
        "plan": "A2 缺口 2：四种状态（jsonb / archive / body_missing / 未归档）的 (err, source) "
                  "组合**两两不同** —— 任何「把所有错误都归到同一哨兵」的实现必须转红",
        "mutations": ["m03_越界报成body_missing"],
        "checks": [],
        "note": (
            "杀手 = TestAcceptanceA2StateMatrixMutuallyExclusive。它是**互异性**断言："
            "四条独立用例各自绿不能证明互不混淆（全局约束第 6 条的遮蔽效应），"
            "故必须在同一用例里做两两比较。"
        ),
    },
    {
        "plan": "前提：冻结包 `GetBody` 签名未变（A2 全链路以它为唯一复用点）",
        "mutations": [],
        "checks": ["冻结包GetBody签名未变"],
        "note": (
            "冻结包为第一硬约束（不可变异，改名/改签名属编译红）⇒ 只有静态证据；"
            "整目录零改动的权威判据是任务 9 的 git diff（D59）。"
        ),
    },
    {
        "plan": "前提：冻结包默认解码窗口（`defaultZstdWindowBytes`）未被静默改动"
                  "（读侧上限是它的手抄副本，E8 / H18）",
        "mutations": [],
        "checks": ["冻结包默认窗口未被静默改动"],
        "note": (
            "**绊线**，不是「窗口一致」的证明：真实的窗口不一致端到端行为（>512 MiB 帧头）"
            "不在本计划（任务 8「跨计划耦合」块的三条理由）。不得把它写成端到端验收。"
        ),
    },
]
