"""task-2 的变异集合 —— **这是作者编写的产物，因此必须 git-tracked**。

与「名字清单」不同，「哪些守卫本该被删」**无法从代码树里重推**：集合若留在
gitignored 的证据目录（`.superpowers/sdd/` 被 `.gitignore` 的 `*` 整体忽略），
作者可以悄悄删掉一条变异而 `git diff` 什么都看不到。本任务此前只有散文形式的
变异记录（task-2-report.md:276-283 的矩阵、:115-135 的 overlay 探针），
本文件把它落成**可重跑、绑定提交**的集合。

每条变异 = 一个「删掉/放宽某道防线」的操作序列：
  ("sub", a, b)  把 a 精确替换成 b（a 必须在目标文件里恰好出现一次）
  ("cut", a, b)  删除 a 到 b（含）之间的内容（a 必须在目标文件里恰好出现一次）

本集合的 4 条守卫变异对应 task-2-brief.md:255-259 的四条计划要求；第 5 条
`%w → %v` 是简报第 4 条「任意一处 %w 改 %v」的**全量**形态（见 m05 的说明）。

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
`tools/trace-mutate/sets/task-8-reader.py:13` 的同一警告。收窄不损失覆盖：
`index.go` 的三个编解码函数只由 `index_test.go` 这 7 条用例直接约束。
"""

NAME = "task-2"
# 被变异的目标文件（相对仓库根）
TARGET = "internal/tracearchive/index.go"
# 跑变异时涉及的 Go 包 —— 只列 B 层包，理由见文件头 ⚠️
PACKAGES = ["./internal/tracearchive/"]
# 名字清单的来源（可被第三方独立重推的文件）
MANIFEST_FILES = ["internal/tracearchive/index_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对（不一致 ⇒ 证据无效）
MANIFEST_DERIVATION = (
    "grep -h '^func Test' internal/tracearchive/index_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
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
# ⚠️ 这条守卫**在当前用例集下判绿**（已登记在 EXPECTED_GREEN）：唯一的非单调用例
# TestDecodeTrailerRejectsNonMonotonic 构造的是「上一条偏移 = 0、差分 = -5」，
# 于是 out[i] = -5 < 0，被紧随其后的 `out[i] < 0` 守卫拦下并**仍**返回
# ErrTrailerCorrupt ⇒ errors.Is 断言通过。
# 注意 task-2-report.md:130-133 的探针**主张**这条变异会转红（"RejectsNonMonotonic
# 应 FAIL"）；在 HEAD 的实现（负值检查后置到 append 之后）下该主张**不成立**，
# 本集合把它变成可重跑的判定而不是散文结论（实测判绿，见 EXPECTED_GREEN）。
# 要真正杀死它，需要一条「递减但结果仍非负」的用例，例如 offsets {10, 5}：
# 差分 -5 相加得 5 ≥ 0，只有 delta < 0 这道检查能拦。
# 该闭合方式已用**隔离探针**实测（/tmp 树，不进仓库）：在 pristine 树上注入
# TestProbeNonMonotonicStaysNonNegative（断言 decodeTrailer(encodeTrailer({10,5}), 2)
# 报 ErrTrailerCorrupt）后，pristine PASS / m02 FAIL / m03 PASS ⇒ 该用例专杀 m02，
# 且对 m03 不敏感 ⇒ 两条守卫语义独立（不是「等价变异」）。
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

# ---------------------------------------------------------------------------
# 静态检查（追加 G）：**变异测不到**的要求必须有静态检查行。
# 本任务的 4 条计划要求全部由上面的变异覆盖（都能被 7 条用例杀死或已登记绿），
# 因此没有静态检查行 —— 这是「为空」，不是「漏写」：`--mapping` 会核对每条
# requirement 都有 mutation 或 check。
# ---------------------------------------------------------------------------
CHECKS = []

# ---------------------------------------------------------------------------
# 允许的绿：预期**不转红**的条目，每条必须带分类标签并写明依据；
# 未登记的绿 ⇒ 整轮失败。分类（task-6 集合沿用）：
#   [零覆盖]   行为**存在**但**没有用例约束**它 ⇒ **测试缺陷**，必须有保留理由
#   [等价变异] 该行在框架/标准库契约下**冗余** ⇒ 关于代码本身的发现
# ---------------------------------------------------------------------------
EXPECTED_GREEN = {
    "m02_删除非单调守卫": (
        "[零覆盖] `delta < 0`（偏移非单调）这道检查**没有任何用例单独约束它**："
        "唯一相关的 TestDecodeTrailerRejectsNonMonotonic 构造的是"
        "「上一条偏移 = 0、差分 = -5」，于是 out[i] = 0 + (-5) = -5 < 0，"
        "被紧随其后的 `out[i] < 0` 守卫拦下，**仍**返回 ErrTrailerCorrupt，"
        "而该用例只断言 errors.Is(err, ErrTrailerCorrupt) ⇒ 变异后照旧 PASS。"
        "这不是等价变异：`delta < 0` 拒绝的是「递减但结果非负」的偏移表"
        "（例如 {10, 5}），`out[i] < 0` 对它完全无感 —— 两条守卫的语义不同，"
        "缺的是用例，不是冗余代码。"
        "【缺口闭合方式（未在本轮实施，属控制者裁决范围）】在"
        " TestDecodeTrailerRejectsNonMonotonic 之外补一条 offsets = {10, 5} 的用例"
        "（期望 decodeTrailer 报 ErrTrailerCorrupt）；**该闭合方式已实测**："
        "在 pristine 树上注入等价探针 TestProbeNonMonotonicStaysNonNegative 后，"
        "pristine PASS / 本变异 FAIL / m03(删 out[i] < 0) PASS ⇒ 它专杀 m02，"
        "且 m03 仍由现有的两个负值子用例杀死 ⇒ 两条守卫各自独立承重。"
        "【对照】task-2-report.md:130-133 曾把这条变异记为「应 FAIL」；"
        "在本集合上重跑判定为**绿**，即那份散文证据与 HEAD 实现不符。"
    ),
}

# ---------------------------------------------------------------------------
# 映射表（追加 G）：计划写的每条变异要求 → 集合里的对应条目。
# 工具会校验：① 每个 requirement 至少有一个 mutation 或 check；
#             ② 引用的名字必须真实存在；③ 列出「没有被任何 requirement 引用」的变异。
# 「悄悄丢掉一个变异」在这里会表现为一行 GAP。
# ---------------------------------------------------------------------------
REQUIREMENTS = [
    {
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
            "本集合把这条**散文主张**变成可重跑判定，结论是**不成立**：该变异判绿，"
            "依据见 EXPECTED_GREEN[m02]（用例的差分 -5 被 `out[i] < 0` 兜住，"
            "唯一缺失的是「递减但结果非负」的用例）。登记为覆盖缺口，不是缺口掩盖。"
        ),
    },
]
