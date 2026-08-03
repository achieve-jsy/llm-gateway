"""task-4 的变异集合（`internal/tracearchive/errors.go` 部分）——
**这是作者编写的产物，因此必须 git-tracked**（理由同 `task-4.py`：哪些防线本该被删
无法从代码树重推，集合留在 gitignored 空间时作者可以悄悄删掉一条而 `git diff` 看不见）。

## 为什么单独一个文件

工具一次只接受一个 `--target`，而 Task 4 的被变异面横跨两个文件：
`ref.go` 的 `parsePartSeq` 拒绝分支（见 `task-4.py`）与 `errors.go` 的哨兵互异。
两个集合**共用同一份名字清单**
（`.superpowers/sdd/<feature>/task-4-evidence/names.txt`，8 条 = A 层 2 条 + B 层 5 条
+ `-run` 无锚匹配额外选中 1 条），因为本集合唯一变异的杀手就在 B 层那份清单里。

## 变异的出处

`task-4-report.md:380-400`（修复轮 1 的 F3【要修 3】「6 个哨兵无互异断言」）：
B 层 `tests/tracearchive/ref_test.go` 的 `TestSentinelErrorsAreDistinct` 对 6 个哨兵
做两两 `errors.Is` 断言，报告的变异验证把 `ErrNotArchived` 指向 `ErrBodyMissing`
（哨兵别名 = 复制粘贴的典型形态），该用例立刻转红（:393-399 是当时的原始输出），
恢复后 PASS。本集合把那次一次性验证固化成可重跑的条目。

注意：`errors.go` 本身在修复轮 1 **无改动**（报告 :493），因此本集合只有 1 条变异，
不存在「首轮 8 条 → 最终 6 条」那种编号收缩，也就没有 RETIRED 条目。
"""

NAME = "task-4-errors"
# 被变异的目标文件（相对仓库根）
TARGET = "internal/tracearchive/errors.go"
# 与 task-4.py 相同的两个包：杀手在 B 层 tests/tracearchive，
# 但同轮也必须跑通 A 层（同一 `go test` 调用，证据才覆盖同一次运行）。
PACKAGES = ["./internal/tracearchive/", "./tests/tracearchive/"]
# 名字清单的来源：**与 task-4.py 完全相同的三个文件**（两个集合共用一份清单；
# 第三个文件为什么在列：Go 的 `-run` 无锚匹配会把 TestParsePartSeqUsedBySealer
# 一并选中，而三断言要求「执行集合 == 清单」——详见 task-4.py 里 MANIFEST_DERIVATION 的说明）
MANIFEST_FILES = [
    "internal/tracearchive/ref_internal_test.go",
    "tests/tracearchive/ref_test.go",
    "internal/tracearchive/sealer_internal_test.go",
]
# 推导命令（与 task-4.py 逐字相同）：工具会实际执行并与 --manifest 比对
MANIFEST_DERIVATION = (
    "{ grep -h '^func Test' internal/tracearchive/ref_internal_test.go "
    "tests/tracearchive/ref_test.go; "
    "grep -h '^func TestParsePartSeqUsedBySealer(' internal/tracearchive/sealer_internal_test.go; "
    "} | sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# SKIP 白名单：空（理由同 task-4.py：任何 SKIP 都判证据无效）
SKIP_WHITELIST = []

# 本集合没有编号收缩，故没有退役编号
RETIRED = {}

# 单一事实源：本集合的全部变异
MUTATIONS = {}

# ---- F3：哨兵别名 ----
# 把 ErrNotArchived 改成 ErrBodyMissing（而不是新建一个 errors.New）⇒
# 「正文从未落盘（404）」与「元数据与存储不一致（500）」被 errors.Is 归为同一类，
# 读取侧的分类静默合并。`%w` 包装、导入、声明都仍然合法 ⇒ **编译通过**，
# 只有 TestSentinelErrorsAreDistinct 能发现它（这正是「复制粘贴哨兵」的形态）。
MUTATIONS["m01_ErrNotArchived指向ErrBodyMissing"] = [
    ("sub", '\tErrNotArchived = errors.New("tracearchive: 产物与暂存均不存在")',
            '\tErrNotArchived = ErrBodyMissing'),
]

# ---------------------------------------------------------------------------
# 允许的绿：**预期为空**。把哨兵改成别名后，6 个哨兵的两两互异断言必然命中两处
# （ErrBodyMissing↔ErrNotArchived 两个方向），因此本条只能是「红」；
# 若判绿，说明互异断言没有真正跑起来（例如清单缺了 B 层文件），必须整轮失败。
# ---------------------------------------------------------------------------
EXPECTED_GREEN = {}

# ---------------------------------------------------------------------------
# 静态检查：**故意为空**。本集合的唯一要求由唯一变异完整承载，
# 没有「变异测不到」的部分，不编造检查行（理由同 task-4.py）。
# ---------------------------------------------------------------------------
CHECKS = []

# ---------------------------------------------------------------------------
# 映射表：要求 → 集合条目。
# ---------------------------------------------------------------------------
REQUIREMENTS = [
    {
        "plan": "task-4-report.md:380-400 F3【要修 3】6 个哨兵必须两两 errors.Is 互异（复制粘贴的哨兵别名须被发现）",
        "mutations": ["m01_ErrNotArchived指向ErrBodyMissing"],
        "checks": [],
        "note": (
            "**来源如实标注**：这条要求来自修复轮 1 的评审要修项（报告 F3），"
            "**不是** task-4-brief.md:96 的 parsePartSeq 泛判据 —— 后者只管 ref.go 的分支守卫，"
            "套到 errors.go 上就是错配。简报里也没有任何关于哨兵互异的要求，"
            "因此这里引用真正提出该要求的行，而不是为了让映射表好看而编造 plan 行。"
            "报告的原始验证只用 `-run TestSentinelErrorsAreDistinct` 单条跑；"
            "本集合要求在**全清单 8 条同轮执行**下转红，证据强度高于当时的一行命令。"
        ),
    },
]
