"""计划 4 / 任务 1 的变异集合 C：退出码契约（纯函数 `ExitCode`）。

被变异文件 `internal/tracesealer/exit.go`。主题：**退出码是对外契约**（systemd unit、
CI、将来若改 oneshot/timer 形态的运维脚本都依赖它），因此它是纯函数、可被顶层用例
钉死 —— 而不是埋在 `cmd/trace-sealer/main.go` 的 switch 里（那种形态只能靠
「编译出二进制再跑」端到端验证，而变异批次有 30s 上限，编译一次就吃掉了大半预算）。

编号一经发布不再改动；匹配一律用字符串（不用行号）。

## 与计划文本的一致性

计划任务 1 步骤 9 的集合 C 逐字采用（2 条变异 + 1 行静态检查）。

## 变异测不到、但有静态检查行（CHECKS）

「`main` 不自己决定退出码」是**不存在性**要求：退出码只能来自 `ExitCode`，`main`
里出现字面量就意味着契约被复制成了两份（改一处漏一处）。没有任何变异能让
「这里没有字面量」变成假 ⇒ 只能静态检查。

**`main.go` 不得建目录**：`\bMkdir[A-Za-z]*\s*\(` absent（**E66③ 补齐**）。D70 的
全局约束第 27 条点名 `internal/tracesealer/` **与** `cmd/trace-sealer/`；本集合的
`PACKAGES` 已经含薄壳包 `./cmd/trace-sealer/`，把这一行挂在同一处最省。它**不可能**由
变异承载（工具只在 pristine 树求值 `CHECKS`，`trace_mutate.py:574`），可失败性由
**E66③ 的临时仓库探针**实测。该行同时不改变本集合的「测试文件 sha256」段（`main.go`
不是 `*_test.go`，工具不会为它印哈希 —— 见上面的「工具边界」栏）。
"""

NAME = "p4-task-1-exit"
TARGET = "internal/tracesealer/exit.go"
# PACKAGES 的第 2 项是**薄壳包** `./cmd/trace-sealer/`（该包没有 `*_test.go`）。它把
# 「退出码契约的静态检查对象」拉进**同一个 `go test` 批次**：薄壳一旦破编译，整轮 `go test`
# 失败 ⇒ 三断言不成立 ⇒ 本表拿不到「全部转红」的结论。
#
# **工具边界（如实登记，不是本行的疏漏）**：`trace_mutate.py` 的证据头只印两种文件的 sha256 ——
# `print_binding()` 对 `TARGET` 印「被测文件 sha256」（`trace_mutate.py:308`），
# `test_files_of()` 只收 `*_test.go`（`trace_mutate.py:283` 的 `f.endswith("_test.go")`，
# 对文件路径 `os.walk` 返回空 ⇒ 把文件写进 PACKAGES 不会多出任何行）。因此 `main.go` 自身的
# sha256 **不可能**由工具印出，只能与 `table_exit=` 一样在表末附录行给出（见本轮
# `p4-task-1-exit-authoritative-at-*.txt` 末尾的 `main_go_sha256=` 行）。
PACKAGES = ["./tests/tracesealer/", "./cmd/trace-sealer/"]
MANIFEST_FILES = ["tests/tracesealer/exit_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/tracesealer/exit_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 纯函数用例：不编译任何二进制、不碰磁盘，没有可 SKIP 的理由
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# m01 有失败也退 0 ⇒ 「定时跑一次、失败就告警」的脚本永远沉默
#     杀手：TestExitCodeTwoWhenOnceHasFailures
MUTATIONS["m01_有失败也退0"] = [
    ("sub", "if res.Failed() > 0 {", "if false {"),
]
# m02 致命错误退 0 ⇒ 启动失败被伪装成正常结束（systemd/CI 都看不见）
#     杀手：TestExitCodeOneOnFatalError
MUTATIONS["m02_致命错误退0"] = [
    ("sub", "if err != nil {", "if false {"),
]

CHECKS = [
    {
        "name": "main不自己决定退出码",
        "file": "cmd/trace-sealer/main.go",
        "pattern": r"os\.Exit\([0-9]",
        "expect": "absent",
        "why": ("退出码契约只允许有一份（internal/tracesealer/exit.go 的 ExitOK/ExitFatal/"
                "ExitFailed）。main 里出现字面量就等于把契约复制成两份，改一处漏一处。"
                "这是**不存在性**要求 ⇒ 只能静态检查（变异无法证明「没写」）。"),
    },
    {
        "name": "main不得建目录",
        "file": "cmd/trace-sealer/main.go",
        "pattern": r"\bMkdir[A-Za-z]*\s*\(",
        "expect": "absent",
        "why": ("**D70 的「整包零建目录」**（**E66③** 补齐）：全局约束第 27 条明确点名 "
                "`internal/tracesealer/` **与** `cmd/trace-sealer/` 两个目录都不得出现建目录"
                "调用形态；原先只有 `runner.go`（`p4-task-1-round.py:93`）与 `rootguard.go`"
                "（`p4-task-3-rootguard.py:133`）两行，薄壳 `main.go` 缺 —— 而「启动时 root"
                "不在就先建出来」正是这类事故最自然的入口。**不存在性**要求 ⇒ 静态检查；"
                "**工具只在 pristine 树求值**（`trace_mutate.py:574`）⇒ 变异不可能让它变红，"
                "可失败性由 **E66③ 的临时仓库探针**实测（插入 `os.MkdirAll(*root, 0o755)` 并提交"
                "⇒ 本行 FAIL、工具以「静态检查失败 ⇒ 整轮失败」退出 1）。"),
    },
    {
        "name": "exit.go不得建目录",
        "file": "internal/tracesealer/exit.go",
        "pattern": r"\bMkdir[A-Za-z]*\s*\(",
        "expect": "absent",
        "why": ("**D70 的「整包零建目录」**（**E67** 补齐）：`E66③` 声称覆盖两个目录的每一个"
                "源文件，实测 `internal/tracesealer/exit.go` 缺该行 ⇒ 本集合（其 `TARGET` 正是 "
                "`exit.go`，且 `PACKAGES` 已含 `internal/tracesealer/` 所属的两个包）补上，"
                "使「整包」对 9 个 `.go` 文件逐一行成立。**不存在性**要求 ⇒ 静态检查；"
                "可失败性由**E67 的临时仓库探针**实测（在 `exit.go` 里插入 "
                "`os.MkdirAll(root, 0o755)` 并提交 ⇒ 本行 FAIL、退出 1）。"),
    },
]

EXPECTED_GREEN = {}

REQUIREMENTS = [
    {
        "plan": "退出码：--once 跑完但本轮有失败（可重试或永久）⇒ 2；PartExists 不算失败 ⇒ 0",
        "mutations": ["m01_有失败也退0"],
        "checks": [],
        "note": "杀手 = TestExitCodeTwoWhenOnceHasFailures（含 PartExists:3 ⇒ ExitOK 的反向断言）；TestExitCodeZeroOnCleanRound 在被变异下仍绿，是隔离探针。",
    },
    {
        "plan": "退出码：启动期/环境级致命错误 ⇒ 1（与「某一轮有失败」区分）",
        "mutations": ["m02_致命错误退0"],
        "checks": [],
        "note": "杀手 = TestExitCodeOneOnFatalError。",
    },
    {
        "plan": "main 是薄壳：退出码只由 ExitCode 决定（不存在性要求）",
        "mutations": [],
        "checks": ["main不自己决定退出码", "main不得建目录"],
        "note": ("不存在性无法用变异证明 ⇒ 静态检查。「不得建目录」是 D70 在薄壳上的落点"
                 "（**E66③** 补齐），可失败性由 E66③ 的临时仓库探针承载。"),
    },
    {
        "plan": "计划 4 全局约束第 27 条（D70 整包零建目录）：internal/tracesealer/exit.go",
        "mutations": [],
        "checks": ["exit.go不得建目录"],
        "note": ("**E67** 补齐：`E66③` 的「覆盖每一个源文件」名不副实（实测缺 "
                 "`classify.go` / `alert.go` / `exit.go` / `covers_test.go`）⇒ 本集合补 "
                 "`exit.go` 这一行。不存在性要求 ⇒ 静态检查；可失败性由 E67 的临时仓库探针实测。"),
    },
]
