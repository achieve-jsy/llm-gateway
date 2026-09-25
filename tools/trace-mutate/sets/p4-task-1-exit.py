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
"""

NAME = "p4-task-1-exit"
TARGET = "internal/tracesealer/exit.go"
PACKAGES = ["./tests/tracesealer/"]
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
        "checks": ["main不自己决定退出码"],
        "note": "不存在性无法用变异证明 ⇒ 静态检查。",
    },
]
