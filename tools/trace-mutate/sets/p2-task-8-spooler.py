r"""计划 2 / 任务 8 的变异集合（**spooler 侧**）—— 作者编写的产物，必须 git-tracked。

拆成三份的理由见 `p2-task-8.py` 的文件头：变异工具一次只接受一个 `--target`
（本文件是第二张表，第三张是 `p2-task-8-dt-zone.py`）。
本文件针对 `internal/gateway/observability/spooler.go`，用**同一份名字清单**
（TestAcceptanceA1WriteHalf / TestAcceptanceA3DegradesWithoutDiskPressure）
产出第二张权威表，缺一不可。

这一批变异的主题：**A3 的退化路径本身** —— 闩锁必须真的挡住落盘（R9）、
闩锁错误必须可被 `errors.Is` 识别（否则调用方无法区分「被闩锁拒绝」与「落盘真失败」），
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

# ---- ③ 闩锁错误必须包裹 ErrSpoolLatched（评审 I2；`:377` 的唯一归因点）----
# 闩锁仍然挡住落盘（不动 `if !s.gate.Allow()` 判定），只把**错误值**换成一条不包裹
# ErrSpoolLatched 的普通错误：调用方（写入器/上层告警）就无法用 `errors.Is` 区分
# 「被闩锁拒绝、正文降级、应告警降级」与「落盘真失败」，A3 ③ 的契约随之失效。
# 杀手 TestAcceptanceA3DegradesWithoutDiskPressure 的 ③：
# `require.ErrorIs(t, c.err, gwobs.ErrSpoolLatched)`。
# 【为什么不会先红在别处】闩锁判定保留 ⇒ ① 零落盘绿；错误非 nil ⇒ 写入器照旧走
# body_missing 降级 ⇒ ② 绿；每一次追加都被调用过 ⇒ `require.Len(calls, n)` 绿；
# 归因精确落在 ErrorIs 这一条。
# 【为什么锚点要带上一行】该 `fmt.Errorf` 在 spooler.go 里出现两次（闩锁分支与
# 「建块后仍不可写」分支），单独一行定位不唯一；带 `if !s.gate.Allow() {` 才唯一。
MUTATIONS["m03_闩锁错误不包裹ErrSpoolLatched"] = [
    ("sub", "\tif !s.gate.Allow() {\n\t\treturn ta.Ref{}, fmt.Errorf(\"%w（原因：%s）\", ErrSpoolLatched, s.gate.Reason())",
            "\tif !s.gate.Allow() {\n\t\treturn ta.Ref{}, fmt.Errorf(\"spooler 已闩锁（原因：%s）\", s.gate.Reason())"),
]

# ---------------------------------------------------------------------------
# 静态检查：本集合没有「变异测不到」的额外要求（A3 的两条边界声明由
# p2-task-8.py 的 CHECKS 钉住，避免两份集合重复声明同一件事）。
# ---------------------------------------------------------------------------
CHECKS = []

# 预期没有条目：三条变异都应由**断言**杀死。
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
        "plan": "规格 §15.1 R9 / A3 ③：闩锁拒绝必须是**可识别**的错误（`errors.Is(err, ErrSpoolLatched)`），调用方据此区分闩锁与真实落盘失败",
        "mutations": ["m03_闩锁错误不包裹ErrSpoolLatched"],
        "checks": [],
        "note": (
            "杀手 TestAcceptanceA3DegradesWithoutDiskPressure 的 ③："
            "`require.ErrorIs(c.err, gwobs.ErrSpoolLatched)`。"
            "m01 虽然也让 ③ 红，但它更早红在 ①（磁盘上真的建出了目录）⇒ ③ 从未成为归因点；"
            "m03 保持闩锁判定与零落盘不变，只破坏错误值的可识别性，归因精确落在 ③。"
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
