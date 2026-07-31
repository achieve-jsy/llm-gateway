"""task-8 的变异集合（**作者编写的产物，因此必须 git-tracked**，见 D17）。

Task 8 是**端到端往返与约束验证**：本任务只写测试、没有新生产代码，因此这里的变异
全部针对**既有生产代码**——写入侧 `internal/tracearchive/writer.go`。

【为什么有两个集合文件】变异工具一次只接受**一个 `--target`**（集合里的 MUTATIONS
只作用于那一份文件文本）。Task 8 的端到端用例同时压住写入侧的构造性质与读取侧的守卫，
因此拆成两份、都进版本库：

    tools/trace-mutate/sets/task-8.py          → TARGET = internal/tracearchive/writer.go（本文件）
    tools/trace-mutate/sets/task-8-reader.py   → TARGET = internal/tracearchive/reader.go

两份集合共用同一份名字清单（同一批顶层用例），各自产出**一张**权威表，缺一不可。

每条变异 = 一个「删掉/放宽某道防线」的操作序列：
  ("sub", a, b)  把 a 精确替换成 b（a 必须在目标文件里恰好出现一次）
  ("cut", a, b)  删除 a 到 b（含）之间的内容（a 必须在目标文件里恰好出现一次）
"""

NAME = "task-8"
# 被变异的目标文件（相对仓库根）
TARGET = "internal/tracearchive/writer.go"
# 跑变异时涉及的 Go 包（相对仓库根，`go test` 形式）
#
# ⚠️ 只列 ./tests/tracearchive/：本任务的 manifest 是 B 层用例，若把
# ./internal/tracearchive/ 也列进来，那个包里一个都不匹配 ⇒ go test 会打印
# 「[no tests to run]」⇒ 工具判**证据无效**（这是对的：没有用例执行的运行不是通过）。
PACKAGES = ["./tests/tracearchive/"]
# 名字清单的来源（可被第三方独立重推的文件）
MANIFEST_FILES = [
    "tests/tracearchive/roundtrip_test.go",
    "tests/tracearchive/constraints_test.go",
]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对（不一致 ⇒ 证据无效）
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/tracearchive/roundtrip_test.go "
    "tests/tracearchive/constraints_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# SKIP 白名单：本任务为**空** —— 缺 zstd 时 requireZstd 会 SKIP，必须判无效
#（CI/本机必须装 zstd；见 tests/tracearchive/helpers_test.go:40-47 的警告）。
# 另：TestMissingArtifactAfterStatIsReportedAsOpenError 在 **root** 身份下会 SKIP
#（0o000 不阻止 root 打开）—— 那同样会让整轮判无效，即「以 root 重跑证据」是响亮的失败，
# 而不是静默丢一条覆盖。
SKIP_WHITELIST = []

# ---------------------------------------------------------------------------
# 单一事实源：本任务的全部变异（写入侧）
# ---------------------------------------------------------------------------

MUTATIONS = {}

# 【补充一的核心】写指针以目录内容为权威（规格 §7.3）时必须 +1：
# 只有「重开一定得到更高的块序号」这一个构造性质，才保证
# 「残行恒为该文件的最后一条物理行」（reader.go:266-273 的成立性依赖它）。
# 本变异把它放宽为「复用目录里最大的已用序号」——正是「同 part 跨重开续写」。
MUTATIONS["m01_写指针不加一（重开复用已用序号）"] = [
    ("sub", "\treturn maxSeq + 1, nil",
            "\tif maxSeq < 0 {\n\t\treturn 0, nil\n\t}\n\treturn maxSeq, nil"),
]

# Close 必须把缓冲落到盘上：否则「关闭后重开继续追加」时第一段的字节根本没落盘
#（pendingSeals 的 flush 不属于关闭路径），点读会以行号越界失败。
MUTATIONS["m02_Close不flush"] = [
    ("cut", "\tif err := w.cur.w.Flush(); err != nil {",
            "\t\tfirstErr = err\n\t}\n"),
]

# 「一次 Append == 一条物理行」的落盘侧：行尾换行是被端到端往返依赖的格式不变量
#（specs 的产物是 .jsonl）。摘掉它之后 spool 不再有行边界，sealer 的残行守卫
#（以 EOF 前仍带回非空字节为判据）会拒绝封块。
MUTATIONS["m03_删除行尾换行"] = [
    ("cut", "\tif err := cur.w.WriteByte('\\n'); err != nil {",
            'tracearchive: 追加行尾换行失败（%s）: %w", ref, err))\n\t}'),
]

# ---------------------------------------------------------------------------
# 静态检查（追加 G）：**变异测不到**的要求必须有静态检查行。
# ---------------------------------------------------------------------------
CHECKS = [
    {
        "name": "B层-roundtrip",
        "file": "tests/tracearchive/roundtrip_test.go",
        "pattern": r"(?m)^package tracearchive_test$",
        "expect": "present",
        "why": (
            "task-8-brief.md:4-6 要求两条用例都是 B 层（package tracearchive_test，"
            "仅用公开 API），且本任务**不应有 A 层文件**。分层是「测试摆在哪」的要求，"
            "变异测试测不到 ⇒ 落成静态检查行。"
        ),
    },
    {
        "name": "B层-constraints",
        "file": "tests/tracearchive/constraints_test.go",
        "pattern": r"(?m)^package tracearchive_test$",
        "expect": "present",
        "why": "同 B层-roundtrip（brief:4-6）。",
    },
    {
        "name": "真封块路径走本机zstd",
        "file": "tests/tracearchive/roundtrip_test.go",
        "pattern": r"bin, ver := requireZstd\(t\)",
        "expect": "present",
        "why": (
            "task-8-brief.md:36-50 与 helpers_test.go:40-47：方案 C 的往返必须经**真实** "
            "zstd CLI 封块。用假 zstd（cat）产出的不是合法 zstd 帧；只走 spool 兜底也不覆盖"
            "产物路径。requireZstd 缺失时 SKIP ⇒ 工具的全层级 SKIP==0 会把「静默跳过」判为"
            "无效（brief:251 的「假绿」警告）。这条检查同时挡住「把 requireZstd 换成 fakeZstd」。"
        ),
    },
]

# ---------------------------------------------------------------------------
# 映射表（追加 G）：计划/补充里写的每条变异要求 → 集合里的对应条目。
# ---------------------------------------------------------------------------
REQUIREMENTS = [
    {
        "plan": "task-8-brief.md:4-6 两条用例都必须是 B 层（package tracearchive_test，仅公开 API）",
        "mutations": [],
        "checks": ["B层-roundtrip", "B层-constraints"],
        "note": "测试摆放要求，变异测不到 ⇒ 静态检查行。",
    },
    {
        "plan": "task-8-brief.md:36-127 步骤 1 / §14.2 A1：2 万行写入 → 封块 → 抽样点读，逐字节相等",
        "mutations": ["m03_删除行尾换行"],
        "checks": ["真封块路径走本机zstd"],
        "note": (
            "m03 摘掉「一次 Append == 一条物理行」后，spool 不再是可封块的 .jsonl"
            "（残行守卫 ErrSealFailed）⇒ 本任务的封块循环立刻以 t.Fatalf 转红。"
            "读侧对称项（返回时不 trim 行分隔符）在 task-8-reader.py 的 m05。"
        ),
    },
    {
        "plan": "task-8-brief.md:231-236 步骤 3 / 补充一：Close 后重开必须能读到第一段的全部行（缓冲必须落盘）",
        "mutations": ["m02_Close不flush"],
        "checks": [],
        "note": "TestRoundTripSurvivesWriterReopen 必须转红（spool 字节为空/缺行）。",
    },
    {
        "plan": "补充一（本轮新增要求，不在 plan 里）：重开同一 (dt, inst) 不得在既有 part 上续写 ⇒「残行恒为该文件的最后一条物理行」",
        "mutations": ["m01_写指针不加一（重开复用已用序号）"],
        "checks": [],
        "note": (
            "m01 是本任务最关键的一条：它把「重开一定换新块」改成「复用最大已用序号」，"
            "于是 TestRoundTripSurvivesWriterReopen（part 序号断言）与 "
            "TestReopenNeverContinuesResidualSpoolTail（残行文件逐字节不变 + 新块序号）双双转红。"
            "这两条用例就是判定「计划 2 是否需要写入侧保证」的实测依据。"
        ),
    },
]

# 允许的绿：本集合预期**没有**条目转绿（任何未登记的绿都会让整轮失败）。
EXPECTED_GREEN = {}
