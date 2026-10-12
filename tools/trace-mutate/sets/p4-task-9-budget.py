r"""计划 4 / 任务 9 的变异集合 A：C5 解码内存预算的**算术单源**（`internal/traceverify/budget.go`）。

主题：`PerReadPeakBytes()`（一次并发点读的峰值内存）与 `MaxConcurrent()`（由控制面限额
反推的并发上限）是读侧（计划 3）`TRACE_BODY_READ_MAX_CONCURRENT` 的**唯一输入**
（本计划交接 **H5** / **H10**）。它的每一项与取整方向都必须被钉住：

  * **漏加一项** ⇒ 预算算小 ⇒ 并发上限算大 ⇒ 控制面被 OOM（m01）；
  * **向上取整** ⇒ 「刚好多出 0.3 次点读」时放行一个必然超限的并发数（m02）；
  * **下限被删** ⇒ 算出 0 ⇒ 读侧点读功能整体关闭（m03）；
  * **perRead ≤ 0 的兜底被放宽** ⇒ 非法分母被当成 1 ⇒ 返回一个巨大的并发数（m04）。

编号一经发布不再改动；匹配一律用字符串（不用行号）。

## 与计划文本的一致性

计划任务 9 步骤 5 的 `p4-task-9-budget.py` 逐字给出四条变异与各自杀手（m01–m04），
本文件逐字采用。**没有**追加静态检查：本集合的要求全部是「算术会返回什么」这类
**行为**要求（E62：静态检查只承载存在 / 不存在类要求，绝不承载行为要求），
因此它们全部由 `tests/traceverify/budget_test.go` 的在册用例承载。

## 零变异覆盖的**要求**登记（`COVERAGE_GAPS`，E64②；**工具读不到、不参与任何判定**）

无。四条要求与四条变异一一对应；每个变异都有确定性的断言杀手（无一条依赖偶发形态）。

## 允许的绿

无（`EXPECTED_GREEN` 为空）。本集合的每个变异都必须红成**断言红**：
任何「编译红 / panic / 超时」都要就地改写变异字符串，不得登记为允许的绿（全局约束第 5 条）。
"""

NAME = "p4-task-9-budget"
TARGET = "internal/traceverify/budget.go"
PACKAGES = ["./tests/traceverify/"]
MANIFEST_FILES = ["tests/traceverify/budget_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/traceverify/budget_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 纯算术：不碰磁盘、不需要 /proc、不需要 zstd，没有可 SKIP 的理由
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# m01 PerReadPeakBytes 少加一项（漏掉 64 MiB 的 tb 缓冲）⇒ 预算从 640 MiB 变成 576 MiB
#     ⇒ 并发上限被算大（640 而不是 576 才是一次点读的真实峰值）。
#     杀手：TestPerReadPeakMatchesSpecBudget（+ 同用例的三项取值断言）
MUTATIONS["m01_峰值少加一项"] = [
    ("sub", "return DecodeWindowBytes + MaxIndexBytes + PerReadFixedBytes",
     "return DecodeWindowBytes + MaxIndexBytes"),
]

# m02 商改成向上取整 ⇒ (2048-300-256)/640 = 2.33 被算成 **3** ⇒ 放行一个必然超限的并发数。
#     杀手：TestMaxConcurrentMatchesControlPlaneBudget（主档位 + 两条边界）
MUTATIONS["m02_商改成向上取整"] = [
    ("sub", "n := (limitBytes - baselineBytes - safetyBytes) / perReadBytes",
     "n := (limitBytes - baselineBytes - safetyBytes + perReadBytes - 1) / perReadBytes"),
]

# m03 下限 `if n < 1 { n = 1 }` 删掉 ⇒ 算出来 <1 时返回 0/负数 ⇒ 读侧点读功能整体关闭。
#     杀手：TestMaxConcurrentIsAtLeastOne
MUTATIONS["m03_删掉下限1"] = [
    ("sub", "if n < 1 {\n\t\tn = 1\n\t}", ""),
]

# m04 `perReadBytes <= 0` 的兜底从「返回 1」放宽成「把非法分母改写成 1」⇒ 返回一个
#     巨大的并发数（等于没有上限）。**刻意不选「整段删掉」**：那会让整数除法对 0
#     panic ⇒ 变成崩溃红（全局约束第 5 条：panic 红必须改写变异字符串）。
#     杀手：TestMaxConcurrentRejectsNonPositivePerRead
MUTATIONS["m04_非法分母兜底被放宽"] = [
    ("sub", "if perReadBytes <= 0 {\n\t\treturn 1\n\t}",
     "if perReadBytes <= 0 {\n\t\tperReadBytes = 1\n\t}"),
]

CHECKS = []

REQUIREMENTS = [
    {
        "plan": "一次并发点读的峰值 = 512（窗口）+ 64（索引）+ 64（tb 缓冲）= 640 MiB（加法，不是乘法）",
        "mutations": ["m01_峰值少加一项"],
        "checks": [],
        "note": ("**行为**要求 ⇒ 在册用例 `TestPerReadPeakMatchesSpecBudget`（合计 + 三项取值 + 减法形态的等价断言）。"),
    },
    {
        "plan": "2 GiB 限额、基线 300 MiB、安全垫 256 MiB ⇒ (2048−300−256)/640 = 2.33 ⇒ **2**（向下取整）",
        "mutations": ["m02_商改成向上取整"],
        "checks": [],
        "note": ("**行为**要求 ⇒ 在册用例 `TestMaxConcurrentMatchesControlPlaneBudget`；"
                 "它另用两条边界（差 1 字节不够第 3 次 / 刚好够第 3 次）钉住取整方向，"
                 "并用一条**刚好卡边**的输入（1408 MiB−1、安全垫 128 MiB）钉住「减法真的存在」："
                 "减掉安全垫 ⇒ 1，不减 ⇒ 2（两者不同 ⇒ 忘了减会转红）。"),
    },
    {
        "plan": "算出来 <1 时返回 1（下限）；「1 是下限，不是安全保证」",
        "mutations": ["m03_删掉下限1"],
        "checks": [],
        "note": ("**行为**要求 ⇒ 在册用例 `TestMaxConcurrentIsAtLeastOne`（先用不等式证明该输入"
                 "确实超限，再断言返回 1；另断言分子为负时仍是 1，绝不返回负值）。"),
    },
    {
        "plan": "perReadBytes ≤ 0 ⇒ 返回 1（不许除零 / 负值 / panic）",
        "mutations": ["m04_非法分母兜底被放宽"],
        "checks": [],
        "note": ("**行为**要求 ⇒ 在册用例 `TestMaxConcurrentRejectsNonPositivePerRead`"
                 "（0 / −1 / −640 MiB 三档，`NotPanics` + 必须等于 1）。"),
    },
]

EXPECTED_GREEN = {}
