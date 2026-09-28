"""计划 4 / 任务 2 的变异集合：轮次汇总与告警分级（`internal/tracesealer/alert.go`）。

被变异文件 `internal/tracesealer/alert.go`。主题：**告警分级是对外契约**
（systemd/看板/告警规则按 `Alert.Severity` 与 `Alert.Reason` 匹配，不按错误文本），
因此它必须是纯函数、可被顶层用例逐条钉死。七个承重性质 ↔ `tests/tracesealer/
alert_test.go` 的六个顶层用例（m04 与 m05 共用同一个杀手：永久失败的「级别」与
「名字」是两件事，任一件坏了都必须响）。

编号一经发布不再改动；匹配一律用字符串（不用行号）。

## 与计划文本的一致性（一处**如实**改动，理由逐字给出）

计划 m05 写「`Reason` 改成不可匹配的串（例如拼接错误文本）」。本实现里
`Summarize(res RoundResult)` **没有 err 对象**可拼（它只吃计数），而唯一能拼出
动态文本的形态是 `fmt.Sprintf(...)` —— 那需要给 `alert.go` 新增 `fmt` import，
漏掉 import 就是**编译红**，而全局约束第 5 条明令「编译红必须改写变异字符串」。
⇒ 本集合采用**可编译**的「拼细节」形态：把 `"too_many_rows"` 换成
`"too_many_rows: 行数超 trailer 上界"`。`require.Equal(t, "too_many_rows", a.Reason)`
照样转红，语义（告警规则再也匹配不上这个 reason）与计划完全一致。

## 变异测不到、但有静态检查行（CHECKS）

- **`reason` 三个稳定串**：`too_many_rows` / `unknown_failure` / `round_failed` 是
  「取值」要求 —— 用例断言的是**具体取值**，而「这里是这三个串」这句话本身不存在
  可被删除的变异形态（删掉任一个 ⇒ 上面某条变异就红了）。静态检查按全局约束第 6 条。
- **`alert.go` 不得用 `fmt.Sprintf` 拼 reason**：**不存在性**要求（「这里没写」无法
  用变异证明）。
- **心跳每轮必打（runner.go）**：本集合的 TARGET 是 `alert.go`（工具一次只能变异
  一个文件），而心跳在 `runner.go`；更要紧的是**它没有可被杀死的用例** —— 计划已
  如实登记「这条只能靠静态检查」。两行静态检查合起来等价于「出现一次且不在任何
  `if` 内」：① 顶层锚定（行首恰好一个制表符 = 函数体最外层，不在任何 if / switch
  case / 闭包里）present；② 全文件不超过一次 absent（两条合起来 ⇒ 恰好一次）。
  承重的运行时证据是 Task 2 的人工核验（注入 `Slog` 抓输出：全绿轮 1 条心跳 0 条
  告警线、永久失败轮 1 条心跳 + 1 条 ERROR 告警线、纯可重试轮 1 条 WARN 线）——
  该核验用的是**临时**测试文件，未进提交，故不在此作为杀手登记。
- **`Options.Slog` 优先、`Logf` 只在 `Slog == nil` 时使用、都缺省回落
  `slog.Default()`（runner.go）**：接线要求，同样是「不存在性能/取值」形态 ⇒
  静态检查（本集合唯一能覆盖 runner.go 的手段）。
"""

NAME = "p4-task-2-alert"
TARGET = "internal/tracesealer/alert.go"
PACKAGES = ["./tests/tracesealer/"]
MANIFEST_FILES = ["tests/tracesealer/alert_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/tracesealer/alert_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 全部替身驱动：不依赖 zstd / 真实磁盘 / 数据库，没有可 SKIP 的理由
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# m01 全绿轮次也报 WARN ⇒ 每天 288 条恒定告警，真故障被埋掉
#     杀手：TestSummarizeCleanRoundIsSilent
#     **连带杀死** TestSummarizePartExistsAloneIsSilent（如实登记）：两条用例的输入
#     都走到同一个 `SeverityNone` 返回处，改那一个取值必然同时打中它们。这不是遮蔽：
#     PartExists 有自己的专属杀手 m02（它只杀那一条），而「全绿」这条的唯一杀手就是
#     本变异 —— 计划映射表点名的杀手与实测一致。
MUTATIONS["m01_全绿也报WARN"] = [
    ("sub", "return Alert{Severity: SeverityNone, Fields: fields}",
            "return Alert{Severity: SeverityWarn, Fields: fields}"),
]
# m02 PartExists 计入失败条件 ⇒ 幂等无害的预期状态变成天天告警
#     杀手：TestSummarizePartExistsAloneIsSilent
MUTATIONS["m02_PartExists计入告警"] = [
    ("sub", "case res.RowMismatch+res.SealFailed > 0:",
            "case res.PartExists+res.RowMismatch+res.SealFailed > 0:"),
]
# m03 可重试失败升级成 ERROR ⇒ 「等下一轮」被报成「出事了」，告警疲劳
#     杀手：TestSummarizeRetryableFailureIsWarn
MUTATIONS["m03_可重试升级成ERROR"] = [
    ("sub", 'Severity: SeverityWarn, Reason: "round_failed"',
            'Severity: SeverityError, Reason: "round_failed"'),
]
# m04 永久失败降级成 WARN ⇒ 「重试永远不会成功」被当成「等一等就好」（N5 义务被静默）
#     杀手：TestSummarizeTooManyRowsIsErrorAndNamed
MUTATIONS["m04_永久降级成WARN"] = [
    ("sub", 'Severity: SeverityError, Reason: "too_many_rows"',
            'Severity: SeverityWarn, Reason: "too_many_rows"'),
]
# m05 reason 变成告警规则匹配不上的串（拼上细节）⇒ 规则还在，但永远不响
#     杀手：TestSummarizeTooManyRowsIsErrorAndNamed（reason 的 Equal 断言）
MUTATIONS["m05_Reason拼细节"] = [
    ("sub", 'Reason: "too_many_rows"',
            'Reason: "too_many_rows: 行数超 trailer 上界"'),
]
# m06 Fields 少一个键（删掉 min_ratio 条目）⇒ 零值轮次的看板字段缺失，被读成「没问题」
#     杀手：TestSummarizeFieldsAreStable
MUTATIONS["m06_Fields漏一个键"] = [
    ("sub", "\t\t\"min_ratio\":         res.MinRatio,\n", ""),
]
# m07 未分类失败不再报 ERROR ⇒ 「我们还不知道这是什么」被静默成「没问题」
#     杀手：TestSummarizeUnknownFailureIsError
MUTATIONS["m07_未分类不再ERROR"] = [
    ("sub", 'Severity: SeverityError, Reason: "unknown_failure"',
            'Severity: SeverityNone, Reason: "unknown_failure"'),
]

CHECKS = [
    {
        "name": "reason稳定串-too_many_rows",
        "file": "internal/tracesealer/alert.go",
        "pattern": r'"too_many_rows"',
        "expect": "present",
        "why": ("reason 是告警规则/看板的匹配面：它是**取值**要求（用例断言的是具体"
                "取值），不存在能删除它的变异形态 ⇒ 静态检查按全局约束第 6 条。"),
    },
    {
        "name": "reason稳定串-unknown_failure",
        "file": "internal/tracesealer/alert.go",
        "pattern": r'"unknown_failure"',
        "expect": "present",
        "why": "同上：未分类失败的 reason 是运维据此写规则的那个串。",
    },
    {
        "name": "reason稳定串-round_failed",
        "file": "internal/tracesealer/alert.go",
        "pattern": r'"round_failed"',
        "expect": "present",
        "why": "同上：可重试失败的 reason（m03 只动级别、不动这个名字，两者必须分开钉住）。",
    },
    {
        "name": "alert不得用fmt.Sprintf拼reason",
        "file": "internal/tracesealer/alert.go",
        "pattern": r"fmt\.Sprintf",
        "expect": "absent",
        "why": ("用格式化拼 reason 就意味着名字里带上了随上游漂移的错误文本 ⇒ 告警规则"
                "会在某次升级后静默失配。这是**不存在性**要求（「这里没写」无法用变异"
                "证明）⇒ 静态检查。"),
    },
    {
        "name": "心跳每轮必打且在最外层",
        "file": "internal/tracesealer/runner.go",
        "pattern": r'(?m)^\tr\.logger\.Info\("trace-sealer round"',
        "expect": "present",
        "why": ("心跳是运维侧「>3 × poll-interval 没看到这条 ⇒ 进程挂了/被 mask 了」这条"
                "规则的输入面。行首**恰好一个制表符** = 函数体最外层 ⇒ 它不在任何 if / "
                "switch case / 闭包里。计划已如实登记：这条没有可被杀死的用例 ⇒ 静态检查。"
                "本条与下一条（全文件至多一次）合起来 ⇒ 恰好一次。"),
    },
    {
        "name": "心跳全文件至多一次",
        "file": "internal/tracesealer/runner.go",
        "pattern": r"trace-sealer round[\s\S]*trace-sealer round",
        "expect": "absent",
        "why": ("两条心跳会让「>3 × poll-interval 没看到」这类计数规则失真（也会掩盖"
                "「有人把心跳复制到了错误的分支里」）⇒ 出现第二次即红。计划要求「出现一次」。"),
    },
    {
        "name": "Slog优先于Logf",
        "file": "internal/tracesealer/runner.go",
        "pattern": r"if opts\.Slog != nil \{[\s\S]{0,80}if opts\.Logf != nil \{",
        "expect": "present",
        "why": ("接线要求：显式注入的 Slog 必须优先，Logf 只在 Slog == nil 时被使用"
                "（否则「注入了 Slog 却还是打到 Logf」这种沉默的错配无人发现）。"
                "这是「取值/次序」要求 ⇒ 静态检查。"),
    },
    {
        "name": "缺省回落slog.Default",
        "file": "internal/tracesealer/runner.go",
        "pattern": r"return slog\.Default\(\)",
        "expect": "present",
        "why": ("Slog 与 Logf 都缺省时必须落到 slog.Default()，不能是 nil logger"
                "（nil logger 会在第一轮 panic —— 比日志缺失更糟）。静态检查。"),
    },
]

EXPECTED_GREEN = {}

REQUIREMENTS = [
    {
        "plan": "全绿轮次不得产生任何告警（SeverityNone）",
        "mutations": ["m01_全绿也报WARN"],
        "checks": [],
        "note": "杀手 = TestSummarizeCleanRoundIsSilent（实测连带杀死 PartExistsAlone —— 两者共用同一个 None 返回处；PartExists 的专属杀手是 m02）。",
    },
    {
        "plan": "PartExists 幂等无害：单独出现不得告警",
        "mutations": ["m02_PartExists计入告警"],
        "checks": [],
        "note": "杀手 = TestSummarizePartExistsAloneIsSilent。",
    },
    {
        "plan": "可重试失败（行数不符 / 封块失败）= SeverityWarn，等下一轮",
        "mutations": ["m03_可重试升级成ERROR"],
        "checks": [],
        "note": "杀手 = TestSummarizeRetryableFailureIsWarn。",
    },
    {
        "plan": "永久失败 TooManyRows = SeverityError，reason = too_many_rows",
        "mutations": ["m04_永久降级成WARN", "m05_Reason拼细节"],
        "checks": [],
        "note": "杀手 = TestSummarizeTooManyRowsIsErrorAndNamed（m04 打级别断言、m05 打 reason 断言）。",
    },
    {
        "plan": "未分类失败 = SeverityError，reason = unknown_failure",
        "mutations": ["m07_未分类不再ERROR"],
        "checks": [],
        "note": "杀手 = TestSummarizeUnknownFailureIsError。",
    },
    {
        "plan": "Fields 字段集恒定：十个键恒在（零值轮次也出现）",
        "mutations": ["m06_Fields漏一个键"],
        "checks": [],
        "note": "杀手 = TestSummarizeFieldsAreStable（零值 RoundResult）。",
    },
    {
        "plan": "reason 必须是三个稳定串，禁止用错误文本/格式化拼 reason",
        "mutations": [],
        "checks": ["reason稳定串-too_many_rows", "reason稳定串-unknown_failure",
                   "reason稳定串-round_failed", "alert不得用fmt.Sprintf拼reason"],
        "note": "取值 + 不存在性要求 ⇒ 静态检查（计划原文：「正则 present」「不得出现 fmt.Sprintf 拼 reason」）。",
    },
    {
        "plan": "Round 每轮结束必须打一条 trace-sealer round 心跳（供「>3×poll 无心跳」规则使用）",
        "mutations": [],
        "checks": ["心跳每轮必打且在最外层", "心跳全文件至多一次"],
        "note": "计划已如实登记：心跳没有可被杀死的用例 ⇒ CHECKS（顶层锚定 + 至多一次 ⇒ 恰好一次）。",
    },
    {
        "plan": "保留 Logf 注入面：新增 Options.Slog，Slog 优先、Logf 仅在 Slog == nil 时使用、都缺省 ⇒ slog.Default()",
        "mutations": [],
        "checks": ["Slog优先于Logf", "缺省回落slog.Default"],
        "note": "接线要求 ⇒ 静态检查（runner.go 的 resolveLogger）。",
    },
]
