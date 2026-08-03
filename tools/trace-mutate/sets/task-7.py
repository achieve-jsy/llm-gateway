"""task-7 的变异集合（按定位键点读，`internal/tracearchive/reader.go`）
—— **作者编写的产物，因此必须 git-tracked**（项目缺陷 D17）。

为什么它必须进版本库：与「名字清单」不同，「哪些守卫本该被删」**无法从代码树里重推**。
本集合是私有脚本 `.superpowers/sdd/2026-09-24-tracearchive-storage-primitives/task-7-mutate.py`
（497 行）的**忠实迁移**；那份脚本所在的 `.superpowers/` 被 gitignore 覆盖（`git ls-files`
看不到它），作者可以悄悄删掉一条变异而 `git diff` 什么都看不到 —— 这正是 D17 的形态。

迁移同时关掉复审实测的另一条发现：私有脚本的 `go test` 子进程**继承环境**，
`GOFLAGS=-exec=<wrapper>` 能整体替换测试输出、把真红洗成全绿（甚至把零覆盖伪装成有覆盖），
且自证串逐字同形；共享工具 `trace_mutate.py` 用**白名单**封闭子进程环境
（`GOFLAGS`/`GOOS`/`GOCACHE`/`CC`… 一律剔除），因此本条在共享工具下不再成立。

【与私有脚本的关系（逐条可对账）】
  * 编号 `m01..m32` 与私有脚本的 `M` 字典**逐一对应**（迁移时刻 32 条 = 34 个 op：
    27 个 `sub` + 7 个 `cut`），模式字符串逐字照搬 ⇒ Task 7 报告里的表可直接对照本文件。
  * 丢弃的私有机制（共享工具已用参数取代，不再需要，**不要**在本文件里复活）：
      - `REPO=/tmp/readbase`、`PRISTINE=/tmp/reader.pristine.go`：共享工具一律
        `git archive HEAD | tar -x` 现取现解，工作树脏即拒绝运行；
      - `RAW_RUN=/tmp/task7-run.txt`（`-run` 正则从文件读）⇒ 改用 `--manifest` 名字清单；
      - 私有脚本内嵌的 `EMBEDDED_NAMES`：清单改由 MANIFEST_FILES + MANIFEST_DERIVATION
        现推，工具会实际执行推导命令并与清单逐条比对；
      - 私有脚本自带的「执行数 > 0」断言：共享工具已内置，不要重复。
  * 变异顺序也沿用私有脚本的插入顺序（含 m32 插在 m05 之后），使本文件的顺序与 `M`
    字典、与归档表逐行一致。

【权威清单是 22 条那一版】`task-7-report.md` §10 记：第一轮用的是 21 条顶层用例清单，
那一轮的变异表**整体作废**（21 条下 m31 报「绿（零覆盖）」；补齐第 22 条用例
`TestReaderHonorsContextCancelWhileWaitingForSlot` 之后 m31 转红）。因此本集合只在
22 条清单（`reader_test.go` 19 + `reader_internal_test.go` 3）下有效。

每条变异 = 一个「删掉/放宽某道防线」的操作序列：
  ("sub", a, b)  把 a 精确替换成 b（a 必须在目标文件里恰好出现一次）
  ("cut", a, b)  删除 a 到 b（含）之间的内容（a 必须在目标文件里恰好出现一次）
"""

NAME = "task-7"
# 被变异的目标文件（相对仓库根）
TARGET = "internal/tracearchive/reader.go"
# 跑变异时涉及的 Go 包（相对仓库根，`go test` 形式）
#
# ⚠️ 两个包都必须列：本任务的 22 条清单**跨包**（19 条在 tests/，3 条在 internal/）。
# 只列其中一个 ⇒ 另一包的用例一条都不会执行 ⇒ 共享工具的断言 1 判**证据无效**
#（没有按清单全部执行的运行不是通过，更不是绿）。反过来，两个包各自都有匹配的用例，
# 因此不会出现 `[no tests to run]`。
PACKAGES = ["./internal/tracearchive/", "./tests/tracearchive/"]
# 名字清单的来源（可被第三方独立重推的文件）
MANIFEST_FILES = [
    "tests/tracearchive/reader_test.go",
    "internal/tracearchive/reader_internal_test.go",
]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对（不一致 ⇒ 证据无效）
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/tracearchive/reader_test.go "
    "internal/tracearchive/reader_internal_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# SKIP 白名单：本任务为**空** —— 本组用例经 sealLines/真实 zstd CLI 走封块路径，
# 缺 zstd 时会 SKIP；SKIP 必须判无效而不是「真封块用例静默跳过」（CI/本机必须装 zstd；
# 见 tests/tracearchive/helpers_test.go 的 requireZstd 警告）。
SKIP_WHITELIST = []

MUTATIONS = {}

# ---------------------------------------------------------------------------
# brief:632 第 1 项：trailer_len 的合理性上界
# 损坏的 footer 不得让读取端按 trailer_len 分配数 GB 内存。
# ---------------------------------------------------------------------------
# 上界写成 if false ⇒ 5 字节的伪造 trailer_len 会被当成真的去分配/读取。
MUTATIONS["m01_删除trailer_len上界"] = [
    ("sub", 'if int64(ft.TrailerLen) > maxTrailerBytes {',
            'if false {'),
]


# ---------------------------------------------------------------------------
# R10 第 2 项（brief:27-2 / brief:632-7）：必须校验 footer 的 frame_sha256[:8]
# 该字段「只写不读」等于完整性检查从未执行；摘要范围必须限界在帧区
# [0, trailer_offset) —— m03 把 LimitReader 换成整体复制后 10 条用例转红。
# ---------------------------------------------------------------------------
MUTATIONS["m02_删除frame_sha比对"] = [
    ("sub", 'if sum != ft.FrameSHA {',
            'if false {'),
]

MUTATIONS["m03_摘要范围忘掉限界"] = [
    ("sub", 'io.Copy(hasher, io.LimitReader(f, ft.TrailerOffset))',
            'io.Copy(hasher, f)'),
]


# ---------------------------------------------------------------------------
# R10 第 1 项（brief:27-1 / brief:632-6）：readFromSpool 的末行终止符守卫
#
# m04 只删守卫（剩下的 `if readErr != nil` 仍会响亮报错）；
# m32 连 io.EOF 也放行，于是落到 `return trimNewline(line), nil`
# ⇒ **把没有换行收尾的残行当正文返回**（R10 第 1 项明确禁止的形态）。
# 两条都要在：它们证明的是两件不同的事 ——「删守卫」与「静默错正文」不是同一件事。
#
# ⚠️ 空串替换必须写 ("sub", A, '')，**不能**写 ("cut", A, '')：后者 b 为空串时
# `index('', i) == i` ⇒ 替换范围为空 ⇒ 变异退化为**空操作**。私有脚本第一轮正是
# 被这个形态骗过一次（m04 报「绿（零覆盖）」，实际是脚本 bug 而不是无覆盖）。
# ---------------------------------------------------------------------------
# 只删守卫：剩下的 `if readErr != nil` 仍会响亮报错；把残行当正文返回的更强形态见 m32。
MUTATIONS["m04_删除spool残行守卫"] = [
    ("sub", '\t\t\tif errors.Is(readErr, io.EOF) {\n'
            '\t\t\t\treturn nil, fmt.Errorf("%w: spool %s 第 %d 行未以换行符结尾（%d 字节残行）；"+\n'
            '\t\t\t\t\t"残行占掉一个行位，其后按行定位会整体错位", ErrNotArchived, spoolPath, row, len(line))\n'
            '\t\t\t}\n',
            ''),
]


# ---------------------------------------------------------------------------
# spool 读取路径：行号定位、越界、以及 R10 第 1 项的**更强形态**
# （m32 与上一条 m04 同属 R10 第 1 项；这里沿用私有脚本的插入顺序把它放在 m05 之后，
#   使本文件的变异顺序与私有脚本的 `M` 字典、与 Task 7 报告里的表**逐行一致**。）
# ---------------------------------------------------------------------------
MUTATIONS["m05_删除spool越界分支"] = [
    ("sub", '\t\t\t\treturn nil, fmt.Errorf("%w: spool 中不存在第 %d 行", ErrNotArchived, row)',
            '\t\t\t\treturn nil, nil'),
]

MUTATIONS["m32_残行按正文返回"] = [
    ("sub", '\t\t\tif errors.Is(readErr, io.EOF) {\n'
            '\t\t\t\treturn nil, fmt.Errorf("%w: spool %s 第 %d 行未以换行符结尾（%d 字节残行）；"+\n'
            '\t\t\t\t\t"残行占掉一个行位，其后按行定位会整体错位", ErrNotArchived, spoolPath, row, len(line))\n'
            '\t\t\t}\n'
            '\t\t\tif readErr != nil {\n'
            '\t\t\t\treturn nil, fmt.Errorf("tracearchive: 读 spool 失败: %w", readErr)\n'
            '\t\t\t}\n',
            '\t\t\tif readErr != nil && !errors.Is(readErr, io.EOF) {\n'
            '\t\t\t\treturn nil, fmt.Errorf("tracearchive: 读 spool 失败: %w", readErr)\n'
            '\t\t\t}\n'),
]

MUTATIONS["m06_spool行号错位取下一行"] = [
    ("sub", 'if len(line) > 0 && i == row {',
            'if len(line) > 0 && i == row+1 {'),
]


# ---------------------------------------------------------------------------
# R10 第 3 项（brief:27-3 / brief:632-4）：标记行识别
# 产物路径与 spool 路径**各入一条删除变异**：brief 只说「任一处」，
# 集合把两条路径都逐处验证，而不是抽样其中之一。
# m09 反向确认判据是「整行相等」而不是 Contains（否则含 "null" 子串的正文会被误判）。
# ---------------------------------------------------------------------------
MUTATIONS["m07_删除产物路径标记行识别"] = [
    ("cut", '// 标记行（正文写入时即失败，规格 §7.1/§8.2）：绝不能把 "null" 当正文返回。',
            'return nil, fmt.Errorf("%w: %s", ErrBodyMissing, ref)\n'
            '\t\t\t}'),
]

MUTATIONS["m08_删除spool路径标记行识别"] = [
    ("cut", '// 与产物路径同一条约定：标记行 ⇒ ErrBodyMissing，不返回 "null"（规格 §8.2）。',
            'return nil, fmt.Errorf("%w: spool %s 第 %d 行", ErrBodyMissing, spoolPath, row)\n'
            '\t\t\t}'),
]

MUTATIONS["m09_标记行判据放宽为Contains"] = [
    ("sub", 'return bytes.Equal(trimNewline(line), failedRowMarker)',
            'return bytes.Contains(line, failedRowMarker)'),
]


# ---------------------------------------------------------------------------
# 行号定位（物理行序 == row_seq 的读取侧）
# ---------------------------------------------------------------------------
MUTATIONS["m10_删除产物行号越界检查"] = [
    ("sub", 'if ref.Row < 0 || ref.Row >= int(ft.RowCount) {',
            'if false {'),
]

MUTATIONS["m11_只删越界下界"] = [
    ("sub", 'if ref.Row < 0 || ref.Row >= int(ft.RowCount) {',
            'if ref.Row >= int(ft.RowCount) {'),
]

MUTATIONS["m12_越界检查差一"] = [
    ("sub", 'if ref.Row < 0 || ref.Row >= int(ft.RowCount) {',
            'if ref.Row < 0 || ref.Row+1 >= int(ft.RowCount) {'),
]

MUTATIONS["m13_产物行映射错位取下一行"] = [
    ("sub", 'targetOff := offsets[ref.Row]',
            'targetOff := offsets[min(ref.Row+1, len(offsets)-1)]'),
]

MUTATIONS["m14_循环条件差一"] = [
    ("sub", 'for read <= targetOff {',
            'for read < targetOff {'),
]

MUTATIONS["m15_未命中行首兜底返回空"] = [
    ("sub", '\treturn nil, fmt.Errorf("%w: %s（偏移 %d 未命中行首）", ErrNotArchived, ref, targetOff)',
            '\treturn nil, nil'),
]


# ---------------------------------------------------------------------------
# footer / trailer 一致性
# ---------------------------------------------------------------------------
MUTATIONS["m16_删除trailer邻接校验"] = [
    ("cut", '\tif ft.TrailerOffset+int64(ft.TrailerLen) != size-FooterSize {',
            'ErrFooterCorrupt, ft.TrailerOffset, ft.TrailerLen, size)\n'
            '\t}'),
]

# 新增一个 decodeTrailerLenient：按 trailer 字节实际能解出多少条就解多少条，
# 忽略 footer 的 row_count —— 正是「被截断的 trailer 会静默少读几行」那种写法。
MUTATIONS["m17_忽略footer的row_count（宽松解码）"] = [
    ("sub", '\t"crypto/sha256"\n',
            '\t"crypto/sha256"\n'
            '\t"encoding/binary"\n'),
    ("sub", '\toffsets, err := decodeTrailer(tb, int(ft.RowCount))',
            '\toffsets, err := decodeTrailerLenient(tb)'),
    ("sub", 'func isFailedRowMarker(line []byte) bool {',
            '// decodeTrailerLenient 变异：按 trailer 字节实际能解出多少条就解多少条，\n'
            '// 忽略 footer 的 row_count（正是「被截断的 trailer 会静默少读几行」那种写法）。\n'
            'func decodeTrailerLenient(b []byte) ([]int64, error) {\n'
            '\tvar out []int64\n'
            '\tpos := 0\n'
            '\tfor pos < len(b) {\n'
            '\t\tv, adv := binary.Uvarint(b[pos:])\n'
            '\t\tif adv <= 0 {\n'
            '\t\t\treturn nil, ErrTrailerCorrupt\n'
            '\t\t}\n'
            '\t\tpos += adv\n'
            '\t\tif len(out) == 0 {\n'
            '\t\t\tout = append(out, int64(v))\n'
            '\t\t} else {\n'
            '\t\t\tout = append(out, out[len(out)-1]+int64(v))\n'
            '\t\t}\n'
            '\t}\n'
            '\treturn out, nil\n'
            '}\n'
            '\n'
            'func isFailedRowMarker(line []byte) bool {'),
]

MUTATIONS["m18_删除footer长度守卫"] = [
    ("cut", '\tif size < FooterSize {',
            'ErrFooterCorrupt, size, FooterSize)\n'
            '\t}'),
]


# ---------------------------------------------------------------------------
# 错误身份（brief:632 第 2 项：%w 改 %v 必须有用例转红；规格 §8.1）
# 三个 ErrNotArchived 包装点 + 三个 ErrFooterCorrupt 包装点，各一条。
# ---------------------------------------------------------------------------
MUTATIONS["m19_ErrNotArchived改v产物越界"] = [
    ("sub", 'fmt.Errorf("%w: %s（块内共 %d 行）", ErrNotArchived, ref, ft.RowCount)',
            'fmt.Errorf("%v: %s（块内共 %d 行）", ErrNotArchived, ref, ft.RowCount)'),
]

MUTATIONS["m20_ErrNotArchived改v spool越界"] = [
    ("sub", 'fmt.Errorf("%w: spool 中不存在第 %d 行", ErrNotArchived, row)',
            'fmt.Errorf("%v: spool 中不存在第 %d 行", ErrNotArchived, row)'),
]

MUTATIONS["m21_ErrNotArchived改v 两者都不存在"] = [
    ("sub", 'fmt.Errorf("%w: %s", ErrNotArchived, ref)',
            'fmt.Errorf("%v: %s", ErrNotArchived, ref)'),
]

MUTATIONS["m22_ErrBodyMissing改v产物路径"] = [
    ("sub", 'fmt.Errorf("%w: %s", ErrBodyMissing, ref)',
            'fmt.Errorf("%v: %s", ErrBodyMissing, ref)'),
]

MUTATIONS["m23_ErrBodyMissing改v spool路径"] = [
    ("sub", 'fmt.Errorf("%w: spool %s 第 %d 行", ErrBodyMissing, spoolPath, row)',
            'fmt.Errorf("%v: spool %s 第 %d 行", ErrBodyMissing, spoolPath, row)'),
]

MUTATIONS["m24_ErrFooterCorrupt改v邻接"] = [
    ("sub", 'fmt.Errorf("%w: trailer 未紧邻 footer（off=%d len=%d size=%d）"',
            'fmt.Errorf("%v: trailer 未紧邻 footer（off=%d len=%d size=%d）"'),
]

MUTATIONS["m25_ErrFooterCorrupt改v摘要"] = [
    ("sub", 'fmt.Errorf("%w: frame_sha256 不符（footer 记录 %x，实算 %x；帧区 %d 字节）"',
            'fmt.Errorf("%v: frame_sha256 不符（footer 记录 %x，实算 %x；帧区 %d 字节）"'),
]

MUTATIONS["m26_ErrFooterCorrupt改v上界"] = [
    ("sub", 'fmt.Errorf("%w: trailer_len=%d 超过上界 %d", ErrFooterCorrupt, ft.TrailerLen, maxTrailerBytes)',
            'fmt.Errorf("%v: trailer_len=%d 超过上界 %d", ErrFooterCorrupt, ft.TrailerLen, maxTrailerBytes)'),
]


# ---------------------------------------------------------------------------
# 并发上限与构造期守卫
# ---------------------------------------------------------------------------
# 槽位泄漏 ⇒ 后续读取永久阻塞（实测 30s 超时/挂死：响亮红，不是无效）。
MUTATIONS["m27_删除并发槽位释放"] = [
    ("sub", '\t\tdefer func() { <-r.sem }()\n',
            ''),
]

MUTATIONS["m28_删除MaxConcurrent默认值"] = [
    ("cut", '\tif opts.MaxConcurrent <= 0 {',
            '\t\topts.MaxConcurrent = 2\n'
            '\t}'),
]

MUTATIONS["m29_删除ZstdWindowBytes默认值"] = [
    ("cut", '\tif opts.ZstdWindowBytes <= 0 {',
            '\t\topts.ZstdWindowBytes = defaultZstdWindowBytes\n'
            '\t}'),
]

MUTATIONS["m30_删除Root为空守卫"] = [
    ("cut", '\tif opts.Root == "" {',
            '\t\treturn nil, fmt.Errorf("tracearchive: Root 不能为空")\n'
            '\t}'),
]

MUTATIONS["m31_删除ctx取消分支"] = [
    ("sub", '\tcase <-ctx.Done():\n'
            '\t\treturn nil, ctx.Err()\n',
            ''),
]

# ---------------------------------------------------------------------------
# 静态检查：本任务**没有**「变异测不到」的要求 —— brief:27 与 brief:632 的全部十条要求
# 都落到了上面的变异上（见下方映射表，无缺口）。因此这里是空表，而不是漏填。
# ---------------------------------------------------------------------------
CHECKS = []

# ---------------------------------------------------------------------------
# 映射表：计划写的每条变异要求 → 集合里的对应条目。缺一条即整轮失败。
#
# 来源只有两处（brief 里除此之外没有任何「删掉 X → Y 必须转红」的要求）：
#   task-7-brief.md:27   R10 三条（本任务必须额外实现的读取侧要求）
#   task-7-brief.md:632  步骤 4 的「变异自检」：七项，末两项是 R10 两条的另加
#
# 下面 10 条与这两处逐条对应（3 + 7），被杀用例一律取自 Task 7 归档的
# task-7-evidence/mutation-table.txt（22 条清单下的那一版，见其 §10 作废说明）。
# ---------------------------------------------------------------------------
REQUIREMENTS = [
    {
        "plan": "task-7-brief.md:27（R10 第 1 项）readFromSpool 必须拒绝未以换行符结尾的末行，"
                "不得 trim 掉残字节后当正文返回（规格 §8.2）",
        "mutations": ["m04_删除spool残行守卫", "m32_残行按正文返回"],
        "checks": [],
        "note": (
            "两条语义不同、缺一不可：m04 只删守卫（剩下的 `if readErr != nil` 仍会响亮报错），"
            "m32 连 io.EOF 也放行 ⇒ 落到 `return trimNewline(line), nil`，"
            "**把残行当正文返回**（正是 R10 第 1 项禁止的静默错正文路径）。"
            "实测两条都由 TestReaderRejectsUnterminatedSpoolTail 杀死。"
        ),
    },
    {
        "plan": "task-7-brief.md:27（R10 第 2 项）读取产物必须重算帧区 [0, trailer_offset) 的 "
                "SHA-256 前 8 字节并与 footer 比对，不符 ⇒ ErrFooterCorrupt",
        "mutations": ["m02_删除frame_sha比对", "m03_摘要范围忘掉限界"],
        "checks": [],
        "note": (
            "m02 由 TestReaderVerifiesFrameSHA 与 TestReaderVerifiesFrameSHABeyondDecodedRegion "
            "双双杀死 —— 后者的存在正是为了证明「摘要范围必须限界在帧区」；"
            "m03 把 io.LimitReader 换成整体复制 ⇒ 10 条用例转红（含越界段落到解码器之后的路径）。"
        ),
    },
    {
        "plan": "task-7-brief.md:27（R10 第 3 项）遇标记行必须返回 ErrBodyMissing"
                "（产物路径与 spool 路径**都**要识别），不得把字符串 \"null\" 当正文递出",
        "mutations": [
            "m07_删除产物路径标记行识别",
            "m08_删除spool路径标记行识别",
            "m09_标记行判据放宽为Contains",
            "m22_ErrBodyMissing改v产物路径",
            "m23_ErrBodyMissing改v spool路径",
        ],
        "checks": [],
        "note": (
            "两条路径各一条删除变异（brief 只说「任一」，集合逐处验证而不是抽样）："
            "m07/m08 都杀死 TestReaderReportsBodyMissingForMarkerLine，并各自另杀死本路径的专属用例。"
            "m09 反向确认判据是「整行相等」而非 Contains（由 TestReaderDoesNotTreatNullSubstringAsMarker 杀死）；"
            "m22/m23 确认该哨兵的身份（errors.Is(ErrBodyMissing)）在两条路径上都承重。"
        ),
    },
    {
        "plan": "task-7-brief.md:632 第 1 项：删掉 maxTrailerBytes 上界检查 → "
                "TestReaderRejectsOversizedTrailerLen 必须转红（且可能 OOM）",
        "mutations": ["m01_删除trailer_len上界"],
        "checks": [],
        "note": "实测由该用例单独杀死（names=22 ran=22 PASS=21 FAIL=1）。",
    },
    {
        "plan": "task-7-brief.md:632 第 2 项：把 ErrNotArchived / ErrFooterCorrupt 的 %w 改成 %v "
                "→ 对应用例必须转红",
        "mutations": [
            "m19_ErrNotArchived改v产物越界",
            "m20_ErrNotArchived改v spool越界",
            "m21_ErrNotArchived改v 两者都不存在",
            "m24_ErrFooterCorrupt改v邻接",
            "m25_ErrFooterCorrupt改v摘要",
            "m26_ErrFooterCorrupt改v上界",
        ],
        "checks": [],
        "note": (
            "两个哨兵的全部包装点各一条：ErrNotArchived 三处（产物越界 / spool 越界 / 两者都不存在），"
            "ErrFooterCorrupt 三处（邻接 / 摘要 / 上界）。任一处退回 %v 都会断掉错误链，"
            "被 errors.Is 断言抓住（m19 杀死 2 条，m25 杀死 2 条，其余各 1 条）。"
        ),
    },
    {
        "plan": "task-7-brief.md:632 第 3 项：删掉行号越界检查 → "
                "TestReaderDistinguishesMissingFromNotArchived 必须转红",
        "mutations": ["m10_删除产物行号越界检查", "m11_只删越界下界", "m12_越界检查差一"],
        "checks": [],
        "note": (
            "m10（整条删掉）与 m11（只删下界、放过负数 row）都让越界 row 落到后面的偏移读取，"
            "实测为**崩溃/中止**（m10 杀死 Distinguishes…、m11 杀死 RejectsNegativeRow）——"
            "响亮的红，绝不是绿；m12（`>=` 改 `ref.Row+1 >=`）是差一形态，转红 10 条用例。"
        ),
    },
    {
        "plan": "task-7-brief.md:632 第 4 项：删掉 readFromPart 或 readFromSpool 里的标记行识别"
                "（任一处）→ TestReaderReportsBodyMissingForMarkerLine 必须转红",
        "mutations": ["m07_删除产物路径标记行识别", "m08_删除spool路径标记行识别"],
        "checks": [],
        "note": (
            "与上面 R10 第 3 项引用同一对条目（brief 两处都要求到「任一处」）。"
            "保留两条的理由：实测**两条路径各自**都让 TestReaderReportsBodyMissingForMarkerLine 转红，"
            "即「任一处」已被逐处验证。"
        ),
    },
    {
        "plan": "task-7-brief.md:632 第 5 项：把 defer func() { <-r.sem }() 删掉 → "
                "TestReaderReleasesConcurrencySlots 必须转红",
        "mutations": ["m27_删除并发槽位释放"],
        "checks": [],
        "note": (
            "实测红(超时/挂死)：槽位泄漏后后续读取永久阻塞，30s 超时，用例拿不到终判 —— "
            "共享工具把「超时/挂死」判为响亮的红（不是无效，更不可能是绿）。"
            "brief 特别说明这条断言必须留在 A 层（B 层并发用例只断言「全部读成功」，不会红）。"
        ),
    },
    {
        "plan": "task-7-brief.md:632 第 6 项（R10 第 1 项另加）：删掉 readFromSpool 的末行终止符"
                "守卫 → 该守卫的用例必须转红",
        "mutations": ["m04_删除spool残行守卫", "m32_残行按正文返回"],
        "checks": [],
        "note": "与 R10 第 1 项同一对条目（brief:632 重申）；两条证明的是两件不同的事，故不合并。",
    },
    {
        "plan": "task-7-brief.md:632 第 7 项（R10 第 2 项另加）：删掉 frame_sha256[:8] 校验 → "
                "该校验的用例必须转红",
        "mutations": ["m02_删除frame_sha比对", "m03_摘要范围忘掉限界"],
        "checks": [],
        "note": "与 R10 第 2 项同一对条目（brief:632 重申）。",
    },
]

# ---------------------------------------------------------------------------
# 允许的绿：本任务为**空**（32 条全部转红，0 条证据无效）。
#
# ⚠️ 这一结论只在 22 条清单那一版上成立：第一轮用的是 21 条清单，那一轮的变异表
#    **整体作废** —— 21 条下 m31（删除 ctx 取消分支）报「绿（零覆盖）」，
#    补齐第 22 条用例 TestReaderHonorsContextCancelWhileWaitingForSlot 之后 m31 转红
#    （task-7-report.md §10）。
# ⇒ 本文件下任何一条判绿都必须在这里登记理由（含分类标签），未登记的绿 = 整轮失败。
# ---------------------------------------------------------------------------
EXPECTED_GREEN = {}

# ---------------------------------------------------------------------------
# 登记备查：**未被上面映射表引用**的 12 条。
#
# 它们不是缺口：brief 里只有 :27 与 :632 两处写了「删掉 X → Y 必须转红」，
# 本文件已把这两处逐条对账（10 条要求）。这 12 条是同一批守卫在**步骤 1 的用例清单**下
# 的覆盖证明，杀它们的用例同样来自 22 条清单（下表取自归档的 mutation-table.txt）：
#
#   m05_删除spool越界分支            → TestReaderDistinguishesMissingFromNotArchived
#   m06_spool行号错位取下一行        → 5 条（含 FallsBackToSpoolForUnsealedRows、
#                                      MarkerOnlySpoolIsBodyMissing、RejectsUnterminatedSpoolTail）
#   m13_产物行映射错位取下一行       → TestReaderReportsBodyMissingForMarkerLine、
#                                      TestReaderReturnsExactLine
#   m14_循环条件差一                 → 8 条（含 ReturnsExactLine、MarkerOnlyArtifactIsBodyMissing）
#   m15_未命中行首兜底返回空         → TestReaderRejectsTrailerOffsetNotAtLineStart
#   m16_删除trailer邻接校验          → TestReaderRejectsTrailerNotAdjacentToFooter
#   m17_忽略footer的row_count        → TestReaderRejectsTrailerRowCountMismatch
#   m18_删除footer长度守卫           → TestReaderRejectsTruncatedArtifact
#   m28_删除MaxConcurrent默认值      → 红(超时/挂死)，并发用例永久阻塞
#   m29_删除ZstdWindowBytes默认值    → 9 条
#   m30_删除Root为空守卫             → TestReaderRejectsEmptyRoot
#   m31_删除ctx取消分支              → TestReaderHonorsContextCancelWhileWaitingForSlot
#                                      （第 22 条用例就是为它补的，见 task-7-report.md §10）
#
# 共享工具的 --mapping 会把它们打印成「未被任何计划要求引用的变异」（非失败项）；
# 它们同样在 git-tracked 集合里，删掉会被 git diff 看见。
# ---------------------------------------------------------------------------
