"""task-2 的变异集合 —— **这是作者编写的产物，因此必须 git-tracked**。

与「名字清单」不同，「哪些守卫本该被删」**无法从代码树里重推**：集合若留在
gitignored 的证据目录（`.superpowers/sdd/` 被 `.gitignore` 的 `*` 整体忽略），
作者可以悄悄删掉一条变异而 `git diff` 什么都看不到。本任务此前只有散文形式的
变异记录（task-2-report.md:276-283 的矩阵、:115-135 的 overlay 探针），
本文件把它落成**可重跑、绑定提交**的集合。

每条变异 = 一个「删掉/放宽某道防线」的操作序列：
  ("sub", a, b)  把 a 精确替换成 b（a 必须在目标文件里恰好出现一次）
  ("cut", a, b)  删除 a 到 b（含）之间的内容（a 必须在目标文件里恰好出现一次）

本集合的 6 条变异：m01/m03/m04 逐一对应 task-2-brief.md:255-259 的计划要求，m05 是
简报第 4 条「任意一处 `%w` 改 `%v`」的**全量**形态（见 m05 说明），m02 则来自作者
报告里的一处**失实主张** —— task-2-report.md:130-133 称「把 `if delta < 0` 改成
`if false && delta < 0` ⇒ TestDecodeTrailerRejectsNonMonotonic 应 FAIL」，
而本集合重跑判定为**绿**（该用例的差分 -5 让 out[1] 变成负数，被紧随其后的
`out[i] < 0` 接住）。**这是真实的测试缺口，不是等价变异**：守卫承重、只是没有
用例约束它。本轮已按裁决在 `index_test.go` 的既有表驱动用例里补入
「差分为负但偏移仍非负」子用例，m02 因此是**普通变异**（不再是允许的绿）。

m06 是 F2a 补入的条目（终审 A 缺陷 ③ ⇒ 控制者裁定 ③「上界施加在 n 上」⇒ F1 落成
`index.go:66-69` 的 `if n > maxTrailerEntries`）。它的杀手用例在**另一个测试文件**
（`internal/tracearchive/reader_internal_test.go`），因此本集合的清单与推导命令都
多了一条 —— 理由见下方 ⚠️ 与 MANIFEST_DERIVATION 的说明（同 task-4 对
`TestParsePartSeqUsedBySealer` 的先例）。

⚠️ **PACKAGES 只列 B 层包 `./internal/tracearchive/`，不列 `./tests/tracearchive/`**：
名字清单只覆盖 `internal/tracearchive/index_test.go` 的 7 条顶层用例，而工具的
`-run` 是**按用例名**过滤的 —— 一旦把 `./tests/tracearchive/` 也列进来，那个包里
没有任何用例名命中 `-run`，go test 就会打印 `[no tests to run]`，而
`trace_mutate.py:188-189` 明确把 `no tests to run` 判为**证据无效**（不是绿）。
在 HEAD 上实测：

    $ go test ./internal/tracearchive/ ./tests/tracearchive/ -run '<本集合 7 条名字>' -v -count=1
    ok   .../internal/tracearchive 0.006s
    testing: warning: no tests to run
    ok   .../tests/tracearchive 0.006s [no tests to run]

⇒ 两包并列会让这一轮**永远**判「证据无效」，因此这里必须收窄。同前例见
`tools/trace-mutate/sets/task-8-reader.py:13` 的同一警告。

⚠️⚠️ **收窄包是一条可被误用的自由，判据只有一条（写下来供下一个编辑者核对）**：
**收窄只有在「被省略的包里没有任何一条能杀死本集合变异」时才成立。**
本集合的该条件是**逐条实测**过的（pristine HEAD 树 + 工具自身的 apply_mutation:
m01 由 `RejectsOversizedCount`、m03 由 `RejectsNegativeOrWrappedOffset`、
m04 由 `RejectsTrailingGarbage`、m05 由 6 条拒绝断言各自的 errors.Is 杀死，
四者都在 `./internal/tracearchive/` 内；m02 由本轮补进
`TestDecodeTrailerRejectsNonMonotonic` 的子用例杀死 —— 同样在 `internal/` 内）。
即 6/6 条变异都不需要 `tests/` 包；反过来，把 `tests/` 包列上**只会**让运行判无效
（它在这个 `-run` 清单下一条用例也匹配不到）。删掉或弱化上述任一用例前，
请先重跑本判据，不要假定「收窄无代价」。

⚠️ **m06 的杀手也在 A 层（`internal/tracearchive/reader_internal_test.go`），因此不
改变上面「只列 B 层包」的结论** —— 该文件与 `index_test.go` 同属
`./internal/tracearchive/`，只是**不同测试文件**。工具的 `TARGET` 是**单文件**
（`trace_mutate.py:472`，本集合 = `index.go`），而 `-run` 是**用例名**过滤、
与文件无关 ⇒ 守卫在 `index.go`、杀手在 `reader_internal_test.go` 时，唯一正确的
归属是**把杀手名字并进本集合的清单**，而不是新建一个 TARGET 相同的集合
（那会把同一个被变异文件切成两份证据面）。清单因此由「index_test.go 的全部顶层
用例」+「reader_internal_test.go 里被点名的这一条」构成，推导命令照此写成可执行
形式（先例：`task-4.py` 对 `TestParsePartSeqUsedBySealer` 的同一手法）。
"""

NAME = "task-2"
# 被变异的目标文件（相对仓库根）
TARGET = "internal/tracearchive/index.go"
# 跑变异时涉及的 Go 包 —— 只列 B 层包，理由见文件头 ⚠️
PACKAGES = ["./internal/tracearchive/"]
# 名字清单的来源（可被第三方独立重推的文件）
MANIFEST_FILES = [
    "internal/tracearchive/index_test.go",
    "internal/tracearchive/reader_internal_test.go",
]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对（不一致 ⇒ 证据无效）
#
# 【为什么是「一个文件全量 + 另一个文件点名一条」】m06 的守卫在 index.go，杀手用例
# `TestReaderRejectsTrailerEntryCountOverBound` 却住在 reader_internal_test.go
#（A 层，因为要用未导出的 maxTrailerEntries 常量）。推导因此分两段：前者取
# index_test.go 的全部顶层用例，后者只在 reader_internal_test.go 里点出这一条。
# 不整段 grep reader_internal_test.go 的理由：那会把 15 条与 index.go 无关的用例
# 一并拉进 `-run`，清单越界即「多=[…]」判证据无效 —— 且它们对 index.go 的变异
# 没有判别力（同 task-4.py 的 `TestParsePartSeqUsedBySealer` 先例）。
MANIFEST_DERIVATION = (
    "{ grep -h '^func Test' internal/tracearchive/index_test.go; "
    "grep -h '^func TestReaderRejectsTrailerEntryCountOverBound(' "
    "internal/tracearchive/reader_internal_test.go; "
    "} | sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# SKIP 白名单：本任务为**空** —— 这 7 条用例不依赖 zstd/外部二进制，没有可 SKIP 的理由。
SKIP_WHITELIST = []
# 本集合首次落库（Task 6 修复轮 3 之后），没有已退役编号。
RETIRED = {}

# ---------------------------------------------------------------------------
# 单一事实源：本任务的全部变异
#
# 编号一经发布不再改动（历史报告表可直接对照）。行号按 HEAD:
# internal/tracearchive/index.go 标注，仅供人读；匹配一律用字符串，不用行号。
# ---------------------------------------------------------------------------
MUTATIONS = {}

# ---- index.go:51-57 的 `n > len(b)` 定容量上界守卫（必须在 make 之前拦截）----
# 简报要求「删掉它 ⇒ RejectsHugeCount 必须转红（且可能 panic/OOM）」。
# HEAD 上用例名是 TestDecodeTrailerRejectsOversizedCount（简报里的 RejectsHugeCount 是旧名）；
# 该用例断言的是**分配量**（alloc > 1 MiB 即失败），因为缺这条守卫时 varint 耗尽路径
# 仍会报同一个 ErrTrailerCorrupt —— 只查 errors.Is 锁不住它。
MUTATIONS["m01_删除上界守卫"] = [
    ("sub", 'if n > len(b) {', 'if false && n > len(b) {'),
]

# ---- index.go:77 的 `delta < 0` 非单调（负差分）守卫 ----
# ⚠️ 这条守卫**曾经判绿**（零覆盖）：唯一的非单调用例 TestDecodeTrailerRejectsNonMonotonic
# 构造的是「上一条偏移 = 0、差分 = -5」，于是 out[i] = 0 + (-5) = -5 < 0，
# 被紧随其后的 `out[i] < 0` 守卫拦下并**仍**返回 ErrTrailerCorrupt ⇒ errors.Is 断言
# 通过 ⇒ 删掉 delta < 0 也照旧全绿。
# 注意 task-2-report.md:130-133 的探针**主张**这条变异会转红（"RejectsNonMonotonic
# 应 FAIL"）；在 HEAD 的实现（负值检查后置到 append 之后）下该主张**不成立**，
# 那是一条**失实主张**，而该变异之所以逃过用例只是**输入凑巧**（差分为 -5 使结果变负）。
# 【本轮处置】守卫是承重的：offsets {10, 5} ⇒ out[1] = 10 + (-5) = 5 ≥ 0，
# 只有 delta < 0 能拦；缺它时解码会**成功**返回一个非单调（错误）的偏移序列，
# 属静默错读 ⇒ 这是**真实的测试缺口**，正确处置是补用例而不是登记为允许的绿。
# 已在 index_test.go 的 TestDecodeTrailerRejectsNonMonotonic（本轮改为表驱动，
# 顶层用例数仍为 7）补入子用例「差分为负但偏移仍非负」= encodeTrailer([]int64{10, 5})，
# 断言 errors.Is(err, ErrTrailerCorrupt)。该子用例**专杀本变异**：
# 删 delta < 0 时它转红，而删 out[i] < 0（m03）时它仍 PASS（两守卫语义独立，已实测）。
MUTATIONS["m02_删除非单调守卫"] = [
    ("sub", 'if delta < 0 {', 'if false && delta < 0 {'),
]

# ---- index.go:82-86 的 `out[i] < 0` 负偏移/累加回绕守卫 ----
# 简报写作「删掉 off < 0 检查」（草案版本在 append 之前检查 off）；HEAD 的形态是
# append 之后检查 out[i]，语义相同。杀死它的用例是
# TestDecodeTrailerRejectsNegativeOrWrappedOffset 的两个子用例：
#   「首条偏移溢出为负」走 i==0 分支；「累加回绕为负」走 out[i-1]+delta 回绕。
MUTATIONS["m03_删除负偏移守卫"] = [
    ("sub", 'if out[i] < 0 {', 'if false && out[i] < 0 {'),
]

# ---- index.go:88-90 的 `pos != len(b)` 尾部多余字节守卫 ----
# 简报写作「删掉 pos != len(b) 检查 ⇒ RejectsTrailingBytes 必须转红」；
# HEAD 上用例名是 TestDecodeTrailerRejectsTrailingGarbage（评审轮新增，见
# task-2-report.md:267），它与 RejectsTruncated 走的**不是**同一条路径：
# 后者走 varint 耗尽，本条走「解完 n 条后仍有剩余字节」。
MUTATIONS["m04_删除尾部多余字节守卫"] = [
    ("sub", 'if pos != len(b) {', 'if false && pos != len(b) {'),
]

# ---- 错误身份：**全量** %w → %v（简报第 4 条「任意一处」的严格形态）----
# index.go 里 7 处错误全部用 %w 包装 ErrTrailerCorrupt；读取端依赖这个身份区分
# 「产物损坏」与「数据不存在」，因此每一处都必须被 errors.Is 锁住。
# 只在**一处**改成 %v 只能证明那一处的身份被锁；全量改则逐处验证 7 条错误路径
# （index_test.go:37-39 的注释正是这条判据的来源）。
# 7 处分别在 HEAD 的行 49 / 56 / 60 / 70 / 78 / 85 / 89。
MUTATIONS["m05_错误身份改v"] = [
    ("sub", 'fmt.Errorf("%w: 非法条目数', 'fmt.Errorf("%v: 非法条目数'),
    ("sub", 'fmt.Errorf("%w: 期望 %d 条但仅 %d 字节', 'fmt.Errorf("%v: 期望 %d 条但仅 %d 字节'),
    ("sub", 'fmt.Errorf("%w: 期望 0 条但剩余 %d 字节', 'fmt.Errorf("%v: 期望 0 条但剩余 %d 字节'),
    ("sub", 'fmt.Errorf("%w: 第 %d 条 varint 解不出', 'fmt.Errorf("%v: 第 %d 条 varint 解不出'),
    ("sub", 'fmt.Errorf("%w: 第 %d 条偏移非单调', 'fmt.Errorf("%v: 第 %d 条偏移非单调'),
    ("sub", 'fmt.Errorf("%w: 第 %d 条偏移为负', 'fmt.Errorf("%v: 第 %d 条偏移为负'),
    ("sub", 'fmt.Errorf("%w: 解码后有 %d 字节多余', 'fmt.Errorf("%v: 解码后有 %d 字节多余'),
]

# ---- index.go:66-69 的 `n > maxTrailerEntries` 条目数硬上限（F2a 补，守卫由 F1 新增）----
# 【出处】终审 A 缺陷 ③（progress.md:3721-3723）：`maxTrailerBytes` 只压 trailer 的
# **字节数**，而 `decodeTrailer` 的分配量是 8 × **条目数**；每个字节都可以是合法的
# 1 字节 varint（0x00），于是 64 MiB 的 trailer 能合法地声称 67,108,864 条
# ⇒ `make([]int64, n)` 分配 512 MiB。控制者裁定 ③（progress.md:3771）：
# **上界施加在 n 上、超限返回明确错误、绝不截断**；F1 落成
# `index.go:66-69`（常量 `reader.go:55` `maxTrailerEntries = maxTrailerBytes/8`）。
#
# 【变异形态 = cut 整块】F1 实测（progress.md:3797）：cut 掉这一处守卫后，
# `TestReaderRejectsTrailerEntryCountOverBound` 的产物（真封块帧 + trailer 与 footer
# 自洽、唯独条目数 = 长度 = max+1）会被**完整解码**（全 0 偏移 ⇒ 目标偏移 0 ⇒ 真实帧
# 正常解压），`GetBody` **成功返回第一行正文且 err == nil**（`err=<nil> line="{\"a\":1}"`）
# —— 不是「换个错误」而是**静默成功读出**，破坏规格 §8.4 C5 的内存预算。
# 用例断言的是**错误身份**（`errors.Is(err, ErrTrailerCorrupt)`），因此专门锁住它。
MUTATIONS["m06_删除条目数上界守卫"] = [
    ("cut", '\tif n > maxTrailerEntries {',
            'ErrTrailerCorrupt, n, maxTrailerEntries, float64(8*n)/(1<<20))\n\t}\n'),
]

# ---------------------------------------------------------------------------
# 静态检查（追加 G）：**变异测不到**的要求必须有静态检查行。
# 本任务的计划要求（简报 4 条 + F2a 补入的 F1 守卫 1 条）全部由上面的变异覆盖
# （6 条变异都能被清单里的用例杀死），因此没有静态检查行 —— 这是「为空」，
# 不是「漏写」：`--mapping` 会核对每条 requirement 都有 mutation 或 check。
# ---------------------------------------------------------------------------
CHECKS = []

# ---------------------------------------------------------------------------
# 允许的绿：预期**不转红**的条目，每条必须带分类标签并写明依据；
# 未登记的绿 ⇒ 整轮失败。分类（task-6 集合沿用）：
#   [零覆盖]   行为**存在**但**没有用例约束**它 ⇒ **测试缺陷**，必须有保留理由
#   [等价变异] 该行在框架/标准库契约下**冗余** ⇒ 关于代码本身的发现
#
# 本集合为**空**（5/5 条变异都必须转红）—— 这里曾经登记过 m02（[零覆盖]），
# 但那个「零覆盖」是**真实测试缺口**（守卫承重、无用例约束 ⇒ 静默错读风险），
# 正确处置是补用例而非登记例外：本轮已在 index_test.go 的
# TestDecodeTrailerRejectsNonMonotonic 里补入「差分为负但偏移仍非负」子用例，
# m02 转为普通变异。发现记录保留在 m02 的注释与 REQUIREMENTS 的 note 里。
# ---------------------------------------------------------------------------
EXPECTED_GREEN = {}

# ---------------------------------------------------------------------------
# 映射表（追加 G）：计划写的每条变异要求 → 集合里的对应条目。
# 工具会校验：① 每个 requirement 至少有一个 mutation 或 check；
#             ② 引用的名字必须真实存在；③ 列出「没有被任何 requirement 引用」的变异。
# 「悄悄丢掉一个变异」在这里会表现为一行 GAP。
# ---------------------------------------------------------------------------
REQUIREMENTS = [    {
        "plan": "task-2-brief.md:256 删掉 `n > len(b)` 守卫 → 转红（且可能 panic/OOM）",
        "mutations": ["m01_删除上界守卫"],
        "checks": [],
        "note": (
            "简报里的用例名是 TestDecodeTrailerRejectsHugeCount（旧名）；HEAD 上是 "
            "TestDecodeTrailerRejectsOversizedCount，且它断言的是**分配量**而非错误身份"
            "（缺守卫时 varint 耗尽仍报同一个 ErrTrailerCorrupt，errors.Is 锁不住）。"
        ),
    },
    {
        "plan": "task-2-brief.md:257 删掉 `off < 0` 检查 → RejectsNegativeOffset 必须转红",
        "mutations": ["m03_删除负偏移守卫"],
        "checks": [],
        "note": (
            "HEAD 的实现把检查放在 append 之后（`out[i] < 0`），语义同草案的 `off < 0`；"
            "用例名是 TestDecodeTrailerRejectsNegativeOrWrappedOffset（含两个子用例："
            "首条溢出为负 / 累加回绕为负）。"
        ),
    },
    {
        "plan": "task-2-brief.md:258 删掉 `pos != len(b)` 检查 → RejectsTrailingBytes 必须转红",
        "mutations": ["m04_删除尾部多余字节守卫"],
        "checks": [],
        "note": (
            "用例名是 TestDecodeTrailerRejectsTrailingGarbage（评审轮要求新增，见 "
            "task-2-report.md:267）；它与 RejectsTruncated 不是同一条路径"
            "（后者的用例名同样与简报的 RejectsTruncated 一致，但走 varint 耗尽）。"
        ),
    },
    {
        "plan": "task-2-brief.md:259 把任意一处 `%w` 改成 `%v` → 相应用例（errors.Is）必须转红",
        "mutations": ["m05_错误身份改v"],
        "checks": [],
        "note": (
            "本集合取**全量**形态（7 处 %w 一次全改），比简报的「任意一处」更严："
            "它一次性验证 7 条错误路径的身份都被 errors.Is 锁住，而「只改一处」"
            "按定义只能覆盖那一处。"
        ),
    },
    {
        "plan": (
            "task-2-report.md:130-133 作者探针主张：把 `if delta < 0` 改成 "
            "`if false && delta < 0` ⇒ TestDecodeTrailerRejectsNonMonotonic 应 FAIL"
        ),
        "mutations": ["m02_删除非单调守卫"],
        "checks": [],
        "note": (
            "【发现的记录】本集合把这条**散文主张**变成可重跑判定，结论是**不成立**："
            "判绿。原因是用例的输入凑巧 —— 差分 -5 使 out[1] = -5 变负，被紧随其后的 "
            "`out[i] < 0` 守卫接住，仍返回 ErrTrailerCorrupt，而它只断言 errors.Is ⇒ "
            "删掉 `delta < 0` 也全绿。守卫本身是承重的（offsets {10, 5} 时 "
            "out[1] = 5 ≥ 0，只有 `delta < 0` 能拦；缺它即静默接受非单调偏移序列），"
            "所以这是**真实测试缺口**而非等价变异。"
            "【本轮处置（控制者裁决 1）】补用例而非登记例外：index_test.go 的 "
            "TestDecodeTrailerRejectsNonMonotonic 已改为表驱动（顶层用例数仍为 7），"
            "新增子用例「差分为负但偏移仍非负」= encodeTrailer([]int64{10, 5})，"
            "断言 errors.Is(err, ErrTrailerCorrupt)；该子用例专杀 m02（删 `out[i] < 0` "
            "时它仍 PASS ⇒ 两守卫语义独立，已实测）⇒ m02 现为**普通变异**，"
            "EXPECTED_GREEN 已清空。报告 :130-133 的正式更正由 R11 阶段③统一处理，"
            "本集合不改报告。"
        ),
    },
    {
        "plan": (
            "【如实登记：**不是** task-2-brief.md 里的要求】终审 A 缺陷 ③ ⇒ "
            "控制者裁定 ③（progress.md:3721-3723 / :3771）⇒ F1 落成 "
            "index.go:66-69 的 `if n > maxTrailerEntries`"
        ),
        "mutations": ["m06_删除条目数上界守卫"],
        "checks": [],
        "note": (
            "**依据不在本任务的简报里**：`grep -n 'maxTrailerEntries\\|条目数上界' "
            "task-2-brief.md` **零命中** —— 这条守卫是修复轮 F1 才新增的"
            "（提交 cb3253f5），task-2 的简报写于它存在之前。因此这里**不编造要求编号**，"
            "而是把真实出处写在 plan 字段里：终审 A 缺陷 ③（`maxTrailerBytes` 只压字节数、"
            "未压条目数 ⇒ 64 MiB 全 0x00 的 trailer 合法声称 67,108,864 条 ⇒ `make` 分配 "
            "512 MiB，叠加解码窗口 ≈1.09 GiB/请求，2 并发超规格 §8.4 C5 的 2 GiB 预算）"
            "⇒ 控制者裁定「上界施加在 n 上、超限返回明确错误、不截断」⇒ F1 实现。"
            "F1 自测形态（progress.md:3797）与本条目一致：cut 守卫 ⇒ "
            "`err=<nil> line=\"{\\\"a\\\":1}\"`（静默成功读出）⇒ 红。"
            "杀手用例 `TestReaderRejectsTrailerEntryCountOverBound` 在 "
            "reader_internal_test.go（A 层），已由本集合的 MANIFEST_DERIVATION "
            "定点并入清单。"
        ),
    },
]
