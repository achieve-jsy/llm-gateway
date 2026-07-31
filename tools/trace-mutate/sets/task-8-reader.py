"""task-8 的变异集合（读取侧）—— **作者编写的产物，必须 git-tracked**（见 D17）。

拆成两份的理由见 task-8.py 的文件头：变异工具一次只接受一个 `--target`。
本文件针对 `internal/tracearchive/reader.go`，用同一份名字清单产出第二张权威表。

每条变异 = 一个「删掉/放宽某道防线」的操作序列：
  ("sub", a, b)  把 a 精确替换成 b（a 必须在目标文件里恰好出现一次）
  ("cut", a, b)  删除 a 到 b（含）之间的内容（a 必须在目标文件里恰好出现一次）
"""

NAME = "task-8-reader"
TARGET = "internal/tracearchive/reader.go"
# ⚠️ 同 task-8.py：只列 B 层包，否则 go test 会出现 [no tests to run] ⇒ 证据无效。
PACKAGES = ["./tests/tracearchive/"]
MANIFEST_FILES = [
    "tests/tracearchive/roundtrip_test.go",
    "tests/tracearchive/constraints_test.go",
]
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/tracearchive/roundtrip_test.go "
    "tests/tracearchive/constraints_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
SKIP_WHITELIST = []

MUTATIONS = {}

# ---------------------------------------------------------------------------
# 残行守卫（spool 路径，规格 §8.2 第 4 步）
#
# 与 Task 7 的集合条目（.superpowers/.../task-7-mutate.py 的 m04）同源，但那份集合
# **在 gitignored 空间**（可被悄悄删掉、git diff 看不见 ⇒ D17 的形态）。本集合是
# 该守卫在版本库里的对应条目，并且由**端到端**用例（重开写入器之后）来杀死它。
# ---------------------------------------------------------------------------
MUTATIONS["m01_删除spool残行守卫"] = [
    ("cut", "\t\t\t// 残行守卫：ReadBytes 在 EOF 前找不到",
            "ErrNotArchived, spoolPath, row, len(line))\n\t\t\t}"),
]

# ---------------------------------------------------------------------------
# §8.3 C1~C4 空间硬约束：本任务用 A4「读取千次后磁盘占用增量必须为 0」断言它。
#
# task-8-brief.md:172 的注释声称「若实现被改成『解压到 /tmp 再读』或『缓存已解压块』，
# 此测试会立刻转红」。这句**只对「落在 root 下的临时文件」成立**（原简报只观测 root）；
# 因此 constraints_test.go 额外把进程临时目录（TMPDIR）也纳入观测，本集合用
# m02/m03 两条分别验证**两个观测量各自承重**。
#   - 内存形态的 C3（缓存已解压块）**没有**对应用例 —— 已登记为覆盖边界，不是静默遗漏。
# ---------------------------------------------------------------------------
_TMPFILE_AT_ROOT = (
    "\ttmpCache, err := os.CreateTemp(r.opts.Root, \"trace-read-cache-*\")\n"
    "\tif err != nil {\n\t\treturn nil, err\n\t}\n"
    "\tif _, err := io.Copy(tmpCache, f); err != nil {\n\t\treturn nil, err\n\t}\n"
    "\t_ = tmpCache.Close()\n"
)
_TMPFILE_AT_TMPDIR = (
    "\ttmpCache, err := os.CreateTemp(\"\", \"trace-read-cache-*\")\n"
    "\tif err != nil {\n\t\treturn nil, err\n\t}\n"
    "\tif _, err := io.Copy(tmpCache, f); err != nil {\n\t\treturn nil, err\n\t}\n"
    "\t_ = tmpCache.Close()\n"
)
MUTATIONS["m02_产物读取落临时文件到root"] = [
    ("sub", '\tf, err := os.Open(partPath)\n'
            '\tif err != nil {\n'
            '\t\treturn nil, fmt.Errorf("tracearchive: 打开产物失败: %w", err)\n'
            '\t}\n'
            '\tdefer f.Close()\n',
            '\tf, err := os.Open(partPath)\n'
            '\tif err != nil {\n'
            '\t\treturn nil, fmt.Errorf("tracearchive: 打开产物失败: %w", err)\n'
            '\t}\n'
            '\tdefer f.Close()\n' + _TMPFILE_AT_ROOT),
]
MUTATIONS["m03_产物读取落临时文件到TMPDIR"] = [
    ("sub", '\tf, err := os.Open(partPath)\n'
            '\tif err != nil {\n'
            '\t\treturn nil, fmt.Errorf("tracearchive: 打开产物失败: %w", err)\n'
            '\t}\n'
            '\tdefer f.Close()\n',
            '\tf, err := os.Open(partPath)\n'
            '\tif err != nil {\n'
            '\t\treturn nil, fmt.Errorf("tracearchive: 打开产物失败: %w", err)\n'
            '\t}\n'
            '\tdefer f.Close()\n' + _TMPFILE_AT_TMPDIR),
]
# spool 兜底路径的空间约束（TestUnsealedReadsAlsoDoNotGrow）：readFromSpool 是包级
# 函数、拿不到 ReaderOptions/Root，因此就地落在 spool 文件所在目录（必在 root 下）。
MUTATIONS["m04_spool读取落临时缓存"] = [
    ("sub", '\t"io"\n', '\t"io"\n\t"path/filepath"\n'),
    ("sub", '\tbr := bufio.NewReaderSize(f, 1<<20)\n\tfor i := 0; ; i++ {',
            '\ttmpCache, err := os.CreateTemp(filepath.Dir(spoolPath), "trace-spool-cache-*")\n'
            '\tif err != nil {\n\t\treturn nil, err\n\t}\n'
            '\t_ = tmpCache.Close()\n\n'
            '\tbr := bufio.NewReaderSize(f, 1<<20)\n\tfor i := 0; ; i++ {'),
]

# ---------------------------------------------------------------------------
# A1 的读侧对称项：「点读返回的正文不含行分隔符」。
# 摘掉它之后往返比对必然不等（返回 body+"\n"）——这正是逐字节往返用例存在的理由。
# ---------------------------------------------------------------------------
MUTATIONS["m05_产物路径不去掉行分隔符"] = [
    ("sub", '\t\t\tif isFailedRowMarker(line) {\n'
            '\t\t\t\treturn nil, fmt.Errorf("%w: %s", ErrBodyMissing, ref)\n'
            '\t\t\t}\n'
            '\t\t\treturn trimNewline(line), nil\n',
            '\t\t\tif isFailedRowMarker(line) {\n'
            '\t\t\t\treturn nil, fmt.Errorf("%w: %s", ErrBodyMissing, ref)\n'
            '\t\t\t}\n'
            '\t\t\treturn line, nil\n'),
]

# ---------------------------------------------------------------------------
# reader.go:116-118（readFromPart 的 os.Open 失败包装）——本轮新增覆盖的分支。
# 该分支此前**零覆盖**：本任务用「mode 0o000 的产物文件」构造出它（Stat 成功、
# Open 失败），因此这里必须有一条能杀死该用例的变异，证明新用例是承重的。
# 只把 %w 改成 %v：错误文本仍在，**错误链断掉** ⇒ 用例的 errors.Is(fs.ErrPermission) 转红。
# ---------------------------------------------------------------------------
MUTATIONS["m06_打开产物失败不包装底层错误"] = [
    ("sub", '\t\treturn nil, fmt.Errorf("tracearchive: 打开产物失败: %w", err)',
            '\t\treturn nil, fmt.Errorf("tracearchive: 打开产物失败: %v", err)'),
]

# 变异测不到的读取侧要求：本集合没有 CHECKS（读取侧的承重要求都落到了上面 6 条变异上）。
CHECKS = []

# ---------------------------------------------------------------------------
# 映射表（追加 G）
# ---------------------------------------------------------------------------
REQUIREMENTS = [
    {
        "plan": "task-8-brief.md:130-228 步骤 2 / §14.2 A4：读取千次后磁盘占用增量必须为 0（§8.3 C1~C4）",
        "mutations": [
            "m02_产物读取落临时文件到root",
            "m03_产物读取落临时文件到TMPDIR",
            "m04_spool读取落临时缓存",
        ],
        "checks": [],
        "note": (
            "brief:172 把「解压到 /tmp 再读」列为本用例应当抓住的反模式，但**只观测 root 的"
            "断言对它是盲的**；因此 constraints_test.go 额外观测进程临时目录，并用 m03 证明该"
            "观测量承重（若删掉 TMPDIR 观测，m03 就会判绿 ⇒ 未登记的绿让整轮失败）。"
            "m02/m04 分别证明产物路径与 spool 路径的 root 观测量承重（两条路径实现不同）。"
            "【覆盖边界】C3 的内存形态（缓存已解压块）没有对应用例，已登记在报告里。"
        ),
    },
    {
        "plan": "task-8-brief.md:253-273 步骤 5 与 §8.2 第 4 步：残行必须被拒绝（端到端路径）",
        "mutations": ["m01_删除spool残行守卫"],
        "checks": [],
        "note": (
            "TestReopenNeverContinuesResidualSpoolTail 在**重开写入器**之后断言残行以 "
            "ErrNotArchived 被拒（不是 ErrBodyMissing、更不是把残字节当正文返回）。"
            "该守卫的单元级对应在 Task 7（TestReaderRejectsUnterminatedSpoolTail）；"
            "本条把它放进本任务的集合，是因为 Task 7 的集合仍在 gitignored 目录（D17 形态）。"
        ),
    },
    {
        "plan": "task-8-brief.md:36-127 步骤 1 / §14.2 A1：逐字节往返（读侧必须返回不含行分隔符的正文）",
        "mutations": ["m05_产物路径不去掉行分隔符"],
        "checks": [],
        "note": "TestEndToEndRoundTripByteExact 与 TestRoundTripSurvivesWriterReopen 双双转红。",
    },
    {
        "plan": "本轮新增（复审登记的疑似零覆盖）：reader.go:116-118 readFromPart 的 os.Open 失败包装",
        "mutations": ["m06_打开产物失败不包装底层错误"],
        "checks": [],
        "note": (
            "由 TestMissingArtifactAfterStatIsReportedAsOpenError 单条杀死（隔离探针："
            "本变异只影响这一条用例，因此「转红」精确地证明了新用例承重）。"
        ),
    },
]

EXPECTED_GREEN = {}
