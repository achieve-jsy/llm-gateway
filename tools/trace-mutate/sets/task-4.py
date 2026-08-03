"""task-4 的变异集合（`internal/tracearchive/ref.go` 部分）——
**这是作者编写的产物，因此必须 git-tracked**。

为什么它必须进版本库（追加 F 的裁决）：与「名字清单」不同，「哪些守卫本该被删」
**无法从代码树里重推**。集合若留在 gitignored 的证据目录，作者可以悄悄删掉一个
变异（例如承载「前缀检查真的被约束」的那条），而 `git diff` 什么都看不到。
名字清单是可推导的，因此允许留在证据目录 —— 前提是调用方打印推导命令与哈希。

## 为什么 Task 4 有两个集合文件

工具一次只接受一个 `--target`（`trace_mutate.py:472`），而 Task 4 的被变异面横跨
**两个文件**：`ref.go` 的 `parsePartSeq` 拒绝分支，与 `errors.go` 的哨兵互异。
因此拆成 `task-4.py`（本文件，TARGET=ref.go）与 `task-4-errors.py`
（TARGET=errors.go）；两者**共用同一份名字清单**
（`.superpowers/sdd/<feature>/task-4-evidence/names.txt`，**8 条** =
A 层 `ref_internal_test.go` 2 条 + B 层 `ref_test.go` 5 条 + `-run` 无锚匹配
额外选中的 `TestParsePartSeqUsedBySealer` 1 条；详见下面 MANIFEST_DERIVATION 的说明），
因为 B 层 `ref_test.go` 里的 `TestSentinelErrorsAreDistinct` 是 errors.go 那条变异的杀手。

集合里每条变异 = 一个「删掉/放宽某道防线」的操作序列：
  ("sub", a, b)  把 a 精确替换成 b（a 必须在目标文件里恰好出现一次）
  ("cut", a, b)  删除 a 到 b（含）之间的内容（a 必须在目标文件里恰好出现一次）

## 本集合 6 条的来源与编号对照

以 `task-4-report.md:457-468` 的**最终矩阵**（6 条）为准，本集合命名为 m01..m06：

    本集合  最终矩阵  首轮矩阵（task-4-report.md:195-203）        守卫
    m01     M1        M1  删除 part- 前缀检查                     if !strings.HasPrefix(name, "part-")
    m02     M2        M2  删除 .jsonl.zst 扩展名分支              case strings.HasSuffix(rest, ".jsonl.zst")
    m03     M3        M3  删除 .jsonl 扩展名分支                  case strings.HasSuffix(rest, ".jsonl")
    m04     M4        M8  删除扩展名 default 分支                 default: return 0, false
    m05     M5        M4  删除 6 位长度检查                       if len(digits) != 6
    m06     M6        ——  删除逐位数字校验（首轮后**新增**的守卫） for i := ...; c < '0' || c > '9'

⇒ 首轮 8 条中有 **3 条**不再落成本集合的变异（M5/M6/M7），理由逐条登记在
下面的 `RETIRED` 里。注意首轮 M5/M6 与最终 M5/M6 **编号撞名但含义不同**
（报告作者在 :468 只解释了 2 条消失，实际是 3 条；见 RETIRED 说明）。
"""

NAME = "task-4"
# 被变异的目标文件（相对仓库根）
TARGET = "internal/tracearchive/ref.go"
# 跑变异时涉及的 Go 包（相对仓库根，`go test` 形式）：
# A 层（包内）与 B 层（外部）都必须跑，因为杀死 m01/m04/m05/m06 的子用例
# （000042.jsonl、part-000042、part-+00042.jsonl、part-00004a.jsonl）
# 在 A 层 internal/tracearchive/ref_internal_test.go 里。
PACKAGES = ["./internal/tracearchive/", "./tests/tracearchive/"]
# 名字清单的来源（可被第三方独立重推的文件）：A 层 + B 层两个文件，
# **外加** sealer_internal_test.go —— 原因见下方 MANIFEST_DERIVATION 的说明。
MANIFEST_FILES = [
    "internal/tracearchive/ref_internal_test.go",
    "tests/tracearchive/ref_test.go",
    "internal/tracearchive/sealer_internal_test.go",
]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对（不一致 ⇒ 证据无效）。
#
# 【为什么清单是 8 条而不是「ref 两个文件里的 7 条」】工具把清单直接拼成 go test 的
# `-run` 正则（trace_mutate.py:267），而 Go 的 -run 是**无锚匹配**：
# 清单里的 `TestParsePartSeq` 会连 `TestParsePartSeqUsedBySealer`
#（internal/tracearchive/sealer_internal_test.go:54，同为 package tracearchive 的
#  顶层用例）一起选中。工具的三断言 1 要求「执行集合 == 清单」，因此该条必须显式进清单，
# 否则**原始侧就直接判证据无效**（实测：多=[TestParsePartSeqUsedBySealer]）。
# 这一条也确实与 Task 4 的守卫同源（它断言 parsePartSeq 拒绝 `.jsonl.zst.tmp`），
# 故并进清单不是凑数。下面的推导命令把「为什么多这一条」写成了可执行形式：
# 前一段从两个 ref 测试文件取全部顶层用例，后一段只补被 -run 无锚匹配捎带进来的那一条。
MANIFEST_DERIVATION = (
    "{ grep -h '^func Test' internal/tracearchive/ref_internal_test.go "
    "tests/tracearchive/ref_test.go; "
    "grep -h '^func TestParsePartSeqUsedBySealer(' internal/tracearchive/sealer_internal_test.go; "
    "} | sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# SKIP 白名单：本任务为**空** —— 未装 zstd 时其他用例会 SKIP（见
# tests/tracearchive/helpers_test.go），而 SKIP 会让「有执行、有终判」不成立，
# 因此必须判无效，不能靠白名单掩盖。
SKIP_WHITELIST = []

# ---------------------------------------------------------------------------
# 已退役编号（登记备查，读者不需要问「首轮 M5/M6/M7 去哪了」）
#
# 注意：首轮矩阵与最终矩阵**都用 M 编号且含义不同**。下表左侧一律写成
# 「首轮 Mn」以消歧，并注明它现在由哪条取代（或为何不再存在）。
# ---------------------------------------------------------------------------
RETIRED = {
    "首轮 M5「删除 Atoi 错误检查」": (
        "**已不再适用（守卫不存在）**：ref.go 的 `strconv.Atoi` 已在修复轮 1（提交 7f5eda3c）"
        "被就地逐位校验 + 累加取代（task-4-report.md:291-316），文件里既没有 Atoi 也没有 "
        "`if err != nil` 分支 ⇒ 该操作连模式都定位不到（硬塞会 PATTERN-MISS）。"
        "这条守卫的职责（拒绝长度合法但含非数字的数字段）现由本集合 m06 承担，"
        "首轮用来杀它的子用例 `part-00004a.jsonl` 仍留在 A 层清单里，m06 的杀手也包括它。"
    ),
    "首轮 M6「删除 n<0 检查」": (
        "**已不再适用（守卫不存在）**：同上，Atoi 的整数结果与 `n < 0` 分支随 Atoi 一起消失"
        "（报告 :316 明确「逐位循环同时承担校验与求值」是为了不留下不可达守卫）。"
        "负号现在由逐位数字校验拒绝 ⇒ 由 m06 承担；"
        "首轮用来杀它的子用例 `part--00042.jsonl`（以及 `part-+00042.jsonl`）仍在清单里。"
        "**若日后实现退回 Atoi/ParseInt，本集合必须重新登记这两条守卫** —— "
        "那时 m06 就不再是同一道防线了。"
    ),
    "首轮 M7「扩展名改为取首个点前数字」": (
        "**登记为退役，但理由与前两条不同，必须说清**：这不是「守卫已不存在」——"
        "在最终实现上「放宽扩展名解析」仍然是可表达的、且仍会被 "
        "`part-000042` 杀掉。退役的真实原因是**它没有被记录到可逐字重跑的程度**："
        "首轮报告只给出散文式描述（task-4-report.md:201），没有记下它当时的精确操作与"
        "恢复步骤，仓内也没有 task-4 的首轮变异脚本（只有 task-6/task-7 留下了 "
        "*mutate.py）。按散文重新拟一个操作，等于替首轮补造一份它没做过的证据；"
        "而最终矩阵(:457-468)已把同一区域（扩展名严格性）收敛为 m04（删除 default 分支），"
        "杀手同样是 `part-000042`。因此如实登记退役，而不是硬塞一条无出处的变异。"
        "**这是一处判断，不是事实**：若终审要求保留 M7 语义，应作为**新增**条目"
        "（例如 m07）重新设计并双侧验证，而不是声称它在首轮被做过。"
    ),
}

# 单一事实源：本任务的全部变异（分支守卫，与最终矩阵一一对应）
MUTATIONS = {}

# ---- 首轮 M1 / 最终 M1：part- 前缀 ----
# 杀死者：TestParsePartSeqRejectsIllegalNames/000042.jsonl
# （`index.jsonl` 挡不住：TrimPrefix 无操作后 digits="index" 长度 5≠6，被长度检查兜住
#   —— 这正是首轮判定的「假覆盖」，补充子用例 000042.jsonl 后才真正被约束。）
MUTATIONS["m01_删除part前缀检查"] = [
    ("sub", '\tif !strings.HasPrefix(name, "part-") {\n\t\treturn 0, false\n\t}\n', ''),
]

# ---- 首轮 M2 / 最终 M2：.jsonl.zst 扩展名分支 ----
# 杀死者：TestParsePartSeq（正例 part-000042.jsonl.zst / part-000000.jsonl.zst）
MUTATIONS["m02_删除jsonlzst扩展名分支"] = [
    ("sub", '\tcase strings.HasSuffix(rest, ".jsonl.zst"):\n'
            '\t\tdigits = strings.TrimSuffix(rest, ".jsonl.zst")\n', ''),
]

# ---- 首轮 M3 / 最终 M3：.jsonl 扩展名分支 ----
# 杀死者：TestParsePartSeq（正例 part-000007.jsonl）
MUTATIONS["m03_删除jsonl扩展名分支"] = [
    ("sub", '\tcase strings.HasSuffix(rest, ".jsonl"):\n'
            '\t\tdigits = strings.TrimSuffix(rest, ".jsonl")\n', ''),
]

# ---- 首轮 M8 / 最终 M4：扩展名 default 分支 ----
# 删除后 digits 仍是整段 rest；对无扩展名的 part-000042 恰好是 6 位数字 ⇒ 被误当合法块名。
# 杀死者：TestParsePartSeqRejectsIllegalNames/part-000042
MUTATIONS["m04_删除扩展名default分支"] = [
    ("sub", '\tdefault:\n\t\treturn 0, false\n', ''),
]

# ---- 首轮 M4 / 最终 M5：恰好 6 位长度检查 ----
# 杀死者：TestParsePartSeqRejectsIllegalNames/part-1234.jsonl、/part-1234567.jsonl
#（报告 :464 还列了 /part-.jsonl.zst：去掉长度检查后空数字段会累加出 seq=0 并被判合法）
MUTATIONS["m05_删除6位长度检查"] = [
    ("sub", '\tif len(digits) != 6 {\n\t\treturn 0, false\n\t}\n', ''),
]

# ---- 最终 M6（首轮之后新增的守卫）：逐位数字校验 ----
# 删掉 `c < '0' || c > '9'` 之后，非数字字符会被 `int(c-'0')` 当成数字参与累加
# （报告 :354 实测 part-+00042.jsonl → seq=25100042），于是与真实块名撞号。
# 杀死者：TestParsePartSeqRejectsIllegalNames/part-00004a.jsonl、/part-+00042.jsonl、
#         /part--00042.jsonl
MUTATIONS["m06_删除逐位数字校验"] = [
    ("sub", '\t\tif c < \'0\' || c > \'9\' {\n\t\t\treturn 0, false\n\t\t}\n', ''),
]

# ---------------------------------------------------------------------------
# 允许的绿：预期**不转红**的条目，每条都必须带分类标签与依据；
# 未登记的绿 ⇒ 整轮失败。本集合**预期为空**：最终矩阵(:457-468)声称
# 「没有留下任何不可达/等价变异守卫」，因此若某条判绿，那是一个必须解释的发现，
# 而不是事先豁免的对象。保持空 = 让零覆盖无处躲藏。
# ---------------------------------------------------------------------------
EXPECTED_GREEN = {}

# ---------------------------------------------------------------------------
# 静态检查（追加 G）：**变异测不到**的要求才需要静态检查行。
# 本集合**故意为空**：本任务唯一的计划要求（task-4-brief.md:96 的泛判据）完全由
# 6 条变异承载，没有「变异测不到」的部分；为了凑表而加检查行等于编造要求
# （task-4 简报没有 task-6-brief.md:961 那种「必须另有人工检查」的条目）。
# ---------------------------------------------------------------------------
CHECKS = []

# ---------------------------------------------------------------------------
# 映射表（追加 G）：计划要求 → 集合条目。
# 工具会校验：① 每个 requirement 至少有一个 mutation 或 check；
#             ② 引用的名字必须真实存在；③ 列出「没有任何 requirement 引用」的变异。
# ---------------------------------------------------------------------------
REQUIREMENTS = [
    {
        "plan": "task-4-brief.md:96 判据：删掉 parsePartSeq 的任一分支后，必须至少有一个子用例转红",
        "mutations": [
            "m01_删除part前缀检查",
            "m02_删除jsonlzst扩展名分支",
            "m03_删除jsonl扩展名分支",
            "m04_删除扩展名default分支",
            "m05_删除6位长度检查",
            "m06_删除逐位数字校验",
        ],
        "checks": [],
        "note": (
            "**如实登记**：简报只有这一条泛判据，没有逐守卫清单（没有「必须删除 part- 前缀检查」"
            "这类逐条要求），因此这里把一条要求一次性映射到全部 6 条分支守卫，"
            "不拆成 6 条假 requirement，也不编造简报里没有的行号。"
            "修复轮 1 的评审要修项（报告 F1）新增了第 6 条守卫（逐位数字校验）与"
            "对应的拒绝子用例，该条同样落在这条泛判据下。"
        ),
    },
]
