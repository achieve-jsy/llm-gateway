"""计划 3 / 任务 1 的变异集合（作者编写的产物 ⇒ 必须 git-tracked，见全局约束第 1 条）。

被变异文件是**读侧**（control-plane）的点读配置与定位键入口校验
（`internal/controlplane/config/trace_body_read.go`）。本批变异的主题与计划 2 的
`p2-task-2.py` 相同：**每一条校验都必须能被测出来** —— 删掉/放宽它之后，对应用例
必须转红成「断言红」。

编号一经发布不再改动；匹配一律用字符串（不用行号），行号仅供人读。

## 相对计划（任务 1 步骤 5）的一处**如实**改动

**m05 的替换串改了**。计划给的是

    ("sub", "if err := validateReadDT(ref.DT); err != nil {", "if false {")

但 `ValidateRef` 的该分支体是 `return err` ⇒ 替换成 `if false {` 之后 `err` **未声明**，
产出的是 `undefined: err` —— **编译红**，而全局约束第 5 条明文规定「编译失败不算通过，
变异必须是断言红」（本机实测：`internal/controlplane/config/trace_body_read.go:159:10:
undefined: err` + `[build failed]`）。因此改成只摘掉**判据**、保留 if-init 调用：

    ("sub", "if err := validateReadDT(ref.DT); err != nil {",
            "if err := validateReadDT(ref.DT); false {")

后者可编译、分支永不进入（`err` 在分支体内被引用，仍算已使用），实测为**断言红**：
`TestValidateRefRejectsIllegalComponents` 的 7 条 dt 子例全红，而 inst/part/row 的
8 条子例与 2 条反向断言仍**全绿**（隔离探针成立，见任务 1 报告）。

## 实现与计划文本的一处歧义（在报告里逐字说明，这里只记事实）

计划的任务 1 接口块写 `LoadTraceBodyReadConfig(v *viper.Viper) TraceBodyReadConfig`
（无 error），而步骤 1/3/5 的用例与变异都要求它「返回 error」。本实现取
`(TraceBodyReadConfig, error)` —— 否则 m02/m03/m09 的杀手用例（要求 `require.Error`）
无法成立。并发一列同理：步骤 3 的伪代码写 `<=0 ⇒ 2`，而步骤 1 的用例要求
「0 / -1 / 17 都必须被拒」，本实现取「键未出现 ⇒ 默认 2；显式配置的 0/-1 ⇒ 报错」。

## 变异测不到、但有静态检查行（CHECKS）

- 「读侧不读写入侧参数」住在**本集合的单文件 TARGET** 里，但它是**不存在性**断言
  （要求某个字符串**不出现**）：变异无法证明「没写」，只能静态检查。
- 「默认值在 config.go」：默认值住在 `config.go`，不在 TARGET 里 ⇒ 变异覆盖不到。
- 「冻结包 GetBody 签名未变」：结构不变量（任务 2 的窄接口依赖它），且
  `internal/tracearchive/` **冻结、不可变异**（改了会让 R11 证据断绑定）⇒ 静态检查。
"""

NAME = "p3-task-1"
TARGET = "internal/controlplane/config/trace_body_read.go"
PACKAGES = ["./tests/controlplane/config/"]
MANIFEST_FILES = ["tests/controlplane/config/trace_body_read_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/controlplane/config/trace_body_read_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 本集合的用例不依赖 zstd/外部二进制/数据库/网络，没有可 SKIP 的理由。
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# m01 禁用语义丢失：Root 留空也当成「启用了一个空路径」⇒ 不再 Disabled
#     杀手：TestTraceBodyReadConfigDefaults（断言 Disabled()==true 那一条；
#           Root 为空会继续走绝对路径校验并报错 ⇒ require.NoError 红）
MUTATIONS["m01_空白root不再视为禁用"] = [
    ("sub", 'if root == "" {', "if false {"),
]
# m02 root 绝对路径校验被摘掉 ⇒ 相对路径被接受
#     杀手：TestTraceBodyReadConfigRootMustBeAbsoluteAndNotFilesystemRoot（相对路径子例）
MUTATIONS["m02_不校验root必须绝对"] = [
    ("sub", "if !filepath.IsAbs(root) {", "if false {"),
]
# m03 root == "/" 的第二道判据被摘掉
#     杀手：同上（该用例的 "/" 子例）
MUTATIONS["m03_不拒绝文件系统根"] = [
    ("sub", 'if filepath.Clean(root) == "/" {', "if false {"),
]
# m04 并发上界被放宽到不设限 ⇒ 17 被接受（会撑爆 2 GiB）
#     杀手：TestTraceBodyReadConfigMaxConcurrentBounds（17/64 子例；
#           0/-1 由更早的 c < 1 守卫拦下，不受本条影响）
MUTATIONS["m04_并发上界放宽"] = [
    ("sub", "if c > traceBodyReadMaxConcurrent {", "if false {"),
]
# m05 【隔离探针载体】ValidateRef 的 dt 校验被摘掉（替换串已按文件头第 1 条改动，
#     否则是编译红）。杀手：TestValidateRefRejectsIllegalComponents 的 dt 子例
#     （7 条）与 TestValidateRefDoesNoFilesystemWork；inst/part/row 子例仍绿。
MUTATIONS["m05_不校验dt"] = [
    ("sub", "if err := validateReadDT(ref.DT); err != nil {",
            "if err := validateReadDT(ref.DT); false {"),
]
# m06 inst 白名单被放宽成「非空即可」
#     杀手：TestValidateRefRejectsIllegalComponents 的 inst 子例（含 "../../green"）
MUTATIONS["m06_inst白名单放宽为非空"] = [
    ("sub", 'if ref.Inst != instBlue && ref.Inst != instGreen {', 'if ref.Inst == "" {'),
]
# m07 part 上界 999999 被摘掉（只摘上界：`ref.Part < 0` 那一半仍在）
#     杀手：TestValidateRefRejectsIllegalComponents 的 part=1000000 子例
MUTATIONS["m07_不校验part上界"] = [
    ("sub", "ref.Part > tracePartSeqMax", "false"),
]
# m08 row 非负校验被摘掉
#     杀手：TestValidateRefRejectsIllegalComponents 的 row=-1 子例
MUTATIONS["m08_不校验row非负"] = [
    ("sub", "if ref.Row < 0 {", "if false {"),
]
# m09 窗口下限被摘掉 ⇒ 256 MiB 被接受（解码会失败，把配置错伪装成产物损坏）
#     杀手：TestTraceBodyReadConfigWindowMustNotBeSmallerThanWriteSide
MUTATIONS["m09_不校验窗口下限"] = [
    ("sub", "if mb < traceBodyWriteSideWindowMB {", "if false {"),
]

# ---------------------------------------------------------------------------
# 静态检查（变异测不到的要求）
# ---------------------------------------------------------------------------
CHECKS = [
    # 读侧不得读写入侧参数（规格 §11.1 把这些参数划给网关；读侧无消费者）
    {
        "name": "读侧不读写入侧参数",
        "file": "internal/controlplane/config/trace_body_read.go",
        "pattern": (
            r"TRACE_BODY_(BLOCK_MB|FLUSH_INTERVAL|ROLL_INTERVAL|DISK_WATERMARK_PERCENT|INST)"
            r"|trace_body\.(block_mb|flush_interval|roll_interval|disk_watermark_percent|inst)"
        ),
        "expect": "absent",
        "why": (
            "读侧只读 root + 解码参数；写入侧参数对读侧没有消费者，读了会漂移出「同名两个数」。"
            "这是**不存在性**要求 ⇒ 变异无法证明「没写」，只能静态钉住。"
            "注意本行只覆盖 TARGET 单文件，写入侧参数的消费者仍在网关侧配置里。"
        ),
    },
    # 默认值必须留在 config.go（与仓库既有默认值同一处，便于审计）
    {
        "name": "默认值在config.go",
        "file": "internal/controlplane/config/config.go",
        "pattern": r'v\.SetDefault\("trace_body\.root", ""\)',
        "expect": "present",
        "why": (
            "「留空 = 禁用」是**默认值**，而默认值住在 config.go（不是本集合的单文件 TARGET）"
            "⇒ 变异覆盖不到，只能静态钉住。默认值漂移成非空会让「挂载未到位」的部署"
            "静默走进归档路径。"
        ),
    },
    # 冻结包复用点未被改（**必须指向一个真实存在的具体文件**：工具 print_checks 会直接
    # open(path)，`internal/tracearchive` 是目录 ⇒ IsADirectoryError，整轮零产出）
    {
        "name": "冻结包GetBody签名未变",
        "file": "internal/tracearchive/reader.go",
        "pattern": r"func \(r \*Reader\) GetBody\(ctx context\.Context, ref Ref\) \(\[\]byte, error\)",
        "expect": "present",
        "why": (
            "任务 2 的窄接口 tracearchiveReader 依赖这一方法集；签名若变则接线失效。"
            "**「整目录零改动」的权威判据是任务 9 的 git diff** —— 本条只能钉住这个复用点"
            "（另见全局约束与 D59）。本行不参与任务 1 的 8 条映射（它是任务 2 的结构前提）。"
        ),
    },
]

# 没有登记的「允许的绿」：9 条全部应在原始侧转为断言红（实测见任务 1 报告）。
EXPECTED_GREEN = {}

# ---------------------------------------------------------------------------
# 映射表（计划任务 1 步骤 5 的 8 条要求，逐条对应）
# ---------------------------------------------------------------------------
REQUIREMENTS = [
    {
        "plan": "禁用默认值（Root 留空 ⇒ Disabled，默认并发 2 / 窗口 512 MiB）",
        "mutations": ["m01_空白root不再视为禁用"],
        "checks": ["默认值在config.go"],
        "note": "杀手 = TestTraceBodyReadConfigDefaults（+ 空白 root 用例）。默认值本身住在 config.go，由静态检查钉住。",
    },
    {
        "plan": "root 取值收窄（IsAbs 且 Clean != \"/\"）",
        "mutations": ["m02_不校验root必须绝对", "m03_不拒绝文件系统根"],
        "checks": [],
        "note": "两条都由 TestTraceBodyReadConfigRootMustBeAbsoluteAndNotFilesystemRoot 的对应子例杀死。",
    },
    {
        "plan": "并发上界 1..16（0/-1 显式配置也必须被拒）",
        "mutations": ["m04_并发上界放宽"],
        "checks": [],
        "note": "杀手 = TestTraceBodyReadConfigMaxConcurrentBounds（17/64 子例）。0/-1 由 c < 1 守卫覆盖，本集合无对应变异（不在计划的 9 条内）。",
    },
    {
        "plan": "dt 校验（计划 1 的 N3，调用侧收口）",
        "mutations": ["m05_不校验dt"],
        "checks": [],
        "note": "隔离探针：TestValidateRefRejectsIllegalComponents 的 dt 子例（7 条）与 TestValidateRefDoesNoFilesystemWork；inst/part/row 子例仍绿。替换串按文件头第 1 条改动（否则编译红）。",
    },
    {
        "plan": "inst 白名单（计划 1 的 N3）",
        "mutations": ["m06_inst白名单放宽为非空"],
        "checks": [],
        "note": "杀手 = TestValidateRefRejectsIllegalComponents 的 inst 子例（\"../../green\" 是关键条目：非空但不是白名单）。",
    },
    {
        "plan": "part / row 界（计划 1 的 N3）",
        "mutations": ["m07_不校验part上界", "m08_不校验row非负"],
        "checks": [],
        "note": "杀手 = TestValidateRefRejectsIllegalComponents 的 part=1000000 与 row=-1 子例。",
    },
    {
        "plan": "窗口下限（>= 写入侧 512 MiB，错误文本带下限）",
        "mutations": ["m09_不校验窗口下限"],
        "checks": [],
        "note": "杀手 = TestTraceBodyReadConfigWindowMustNotBeSmallerThanWriteSide（256/511 子例）。",
    },
    {
        "plan": "读侧不读写入侧参数（不存在性要求）",
        "mutations": [],
        "checks": ["读侧不读写入侧参数"],
        "note": "不存在性无法用变异证明 ⇒ 只能静态检查（计划全局约束第 7 条：两类证据分开记）。",
    },
]
