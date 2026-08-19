r"""计划 2 / 任务 8 的变异集合（**spooler 侧**）—— 作者编写的产物，必须 git-tracked。

拆成两份的理由见 `p2-task-8.py` 的文件头：变异工具一次只接受一个 `--target`。
本文件针对 `internal/gateway/observability/spooler.go`，用**同一份名字清单**
（TestAcceptanceA1WriteHalf / TestAcceptanceA3DegradesWithoutDiskPressure）
产出第二张权威表，缺一不可。

这一批变异的主题：**A3 的退化路径本身** —— 闩锁必须真的挡住落盘（R9），
以及定位键里的实例标识必须来自 spooler 的配置（蓝绿隔离的目录分量）。
"""

NAME = "p2-task-8-spooler"
TARGET = "internal/gateway/observability/spooler.go"
# 同 p2-task-8.py：只列 B 层包，否则 go test 会出现 [no tests to run] ⇒ 证据无效。
PACKAGES = ["./tests/gateway/observability/"]
MANIFEST_FILES = ["tests/gateway/observability/trace_body_acceptance_test.go"]
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/gateway/observability/trace_body_acceptance_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# ---- ① 闩锁期间不得写盘（规格 §15.1 R9：不重试、不目录扫描）----
# 把热路径的闩锁判定短路掉 ⇒ 闩锁中照旧调 Writer.Append：冻结包会在**每次建块**时
# 建出 spool 目录链（writer.go:489）⇒ dt= 目录与 part 文件当场出现。
# 杀手 TestAcceptanceA3DegradesWithoutDiskPressure 的 ①（`require.NoDirExists(root/spool)`）
# 与 ③（每一条追加都必须返回 ErrSpoolLatched）。
# 括号保留：`if false { ... }` 仍是合法 Go（不可达分支），不会变成编译红。
MUTATIONS["m01_闩锁期间仍写盘"] = [
    ("sub", "if !s.gate.Allow() {", "if false {"),
]

# ---- ② 定位键的 inst 必须取自 spooler 配置（蓝绿隔离）----
# 【合成探针，但对应一条真实缺陷】把 Ref.Inst 抹成空串：目录名会变成 `inst=`，
# blue 与 green 两实例的正文落进**同一目录**（arch 布局的隔离分量失效），
# 定位键也不再指向本该指向的文件。
# 杀手 TestAcceptanceA1WriteHalf 的 ①（`require.Equal("blue", *row.BodyRefInst)`）
# 与 ②（拼出的 spool 路径必须真的存在 ⇒ 读文件失败）。
MUTATIONS["m02_ref丢失inst"] = [
    ("sub", "\treturn ref, nil", "\tref.Inst = \"\"\n\treturn ref, nil"),
]

# ---------------------------------------------------------------------------
# 静态检查：本集合没有「变异测不到」的额外要求（A3 的两条边界声明由
# p2-task-8.py 的 CHECKS 钉住，避免两份集合重复声明同一件事）。
# ---------------------------------------------------------------------------
CHECKS = []

# 预期没有条目：两条变异都应由**断言**杀死。
EXPECTED_GREEN = {}

REQUIREMENTS = [
    {
        "plan": "规格 §15.1 R9 / A3：闩锁期间不得写盘 —— 不重试、不目录扫描（网关继续服务，只有正文降级）",
        "mutations": ["m01_闩锁期间仍写盘"],
        "checks": [],
        "note": (
            "杀手 TestAcceptanceA3DegradesWithoutDiskPressure 的 ①（整个 spool 目录不得存在）"
            "与 ③（每一行都必须被 ErrSpoolLatched 拒绝）。该断言排在内容断言之前是刻意的："
            "这条缺陷会同时污染 body_missing 断言，排在最前才能让归因落在它要证明的那一条上。"
        ),
    },
    {
        "plan": "规格 §5.1 目录布局与蓝绿隔离：定位键的 inst 必须来自 spooler 配置（不得为空/错值）",
        "mutations": ["m02_ref丢失inst"],
        "checks": [],
        "note": (
            "杀手 TestAcceptanceA1WriteHalf 的 ①（inst 必须逐字为 \"blue\"）与 ②"
            "（按定位键拼出的 spool 路径必须真的存在并可读）。"
        ),
    },
]
