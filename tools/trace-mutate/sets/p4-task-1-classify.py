"""计划 4 / 任务 1 的变异集合 A：错误分类（纯函数）。

被变异文件 `internal/tracesealer/classify.go`。主题：**三类哨兵的语义必须被区分** ——
冻结包 errors.go 把 ErrPartExists（幂等无害）/ ErrRowCountMismatch + ErrSealFailed
（保留 spool、可重试）/ ErrTooManyRows（永久、需人工介入）的处置写死，sealer 若把
它们揉成一个「失败就重试」，运维看到的是一堆永不消失的告警，而真正需要人拆块的
那一块被淹没。

编号一经发布不再改动；匹配一律用字符串（不用行号）。

## 与计划文本的一致性

计划任务 1 步骤 9 的集合 A 逐字采用，**未做任何改动**（7 条变异 + 1 行静态检查）。

## 变异测不到、但有静态检查行（CHECKS）

`FailureKind` 的五个字符串（`part_exists` / `row_count_mismatch` / `seal_failed` /
`too_many_rows` / `unknown`）会被写进轮次汇总日志与告警规则（`kind=…`），是**跨进程
契约**。它是「取值必须是某个字面量」的**取值**要求，不是「删掉某个分支」的**结构**
要求 ⇒ 不存在能让用例转红的变异形态（没有任何用例读字面量以外的形态），按全局约束
第 6/7 条用静态检查钉住。

## E67：D70「整包零建目录」的覆盖补齐（本行）

**E66③** 声称该静态检查覆盖两个目录的**每一个**源文件，实测只覆盖
`runner.go` / `rootguard.go` / `lock.go` / `options.go` / `main.go`，而
`classify.go` / `alert.go` / `exit.go`（以及包内测试文件 `covers_test.go`）**没有该行**
⇒ 名不副实。**E67** 把该行按「一个源文件一行」补齐到 `internal/tracesealer/` 与
`cmd/trace-sealer/` 下的**全部 9 个 `.go` 文件**；本集合负责 `classify.go` 这一行。
完整的「覆盖到的文件清单」与可失败性探测证据见本目录的
`e67-mkdir-coverage-at-*.txt` 与 `e67-mkdir-probe-at-*.txt`（探测中该行 FAIL ⇒ 工具以
「静态检查失败 ⇒ 整轮失败」退出 1）。它**不可能**由变异承载（工具只在 pristine 树
求值 `CHECKS`，`trace_mutate.py:574`）。
"""

NAME = "p4-task-1-classify"
TARGET = "internal/tracesealer/classify.go"
PACKAGES = ["./tests/tracesealer/"]
MANIFEST_FILES = ["tests/tracesealer/classify_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/tracesealer/classify_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 纯函数用例：不依赖数据库/zstd/真实盘，没有可 SKIP 的理由
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {
    # 超限块必须被认成「永久」⇒ 杀手 TestClassifyTooManyRowsIsPermanent
    "m01_不识别TooManyRows": [("sub", "case errors.Is(err, ta.ErrTooManyRows):", "case false:")],
    # 三类哨兵一律用 %w 包装，== 比较会全部落进 unknown ⇒ 杀手 TestClassifyUsesErrorsIsNotEquality
    "m02_改用相等比较": [("sub", "case errors.Is(err, ta.ErrSealFailed):", "case err == ta.ErrSealFailed:")],
    # 未分类不得被吞成「幂等无害」（那等于静默丢数据）⇒ 杀手 TestClassifyUnknownIsLoudAndRetryable
    "m03_未分类吞成PartExists": [("sub", "default:\n\t\treturn KindUnknown", "default:\n\t\treturn KindPartExists")],
    # 行数不符必须自成一类（否则运维分不清「等一下」和「要人工拆块」）⇒ 杀手 TestClassifyRowMismatchIsRetryable
    "m04_不识别RowMismatch": [("sub", "case errors.Is(err, ta.ErrRowCountMismatch):", "case false:")],
    # 幂等无害必须被认出来 ⇒ 杀手 TestClassifyPartExistsIsIdempotent
    "m05_不识别PartExists": [("sub", "case errors.Is(err, ta.ErrPartExists):", "case false:")],
    # Permanent 恒 false ⇒ 永久失败会被当成可重试 ⇒ 杀手 TestClassifyTooManyRowsIsPermanent
    "m06_Permanent恒false": [
        ("sub", "func (k FailureKind) Permanent() bool { return k == KindTooManyRows }",
                "func (k FailureKind) Permanent() bool { return false }")],
    # Retryable 漏掉 RowMismatch ⇒ 行数不符不再重试 ⇒ 杀手 TestClassifyRowMismatchIsRetryable
    "m07_Retryable漏掉RowMismatch": [
        ("sub", "return k == KindRowMismatch || k == KindSealFailed || k == KindUnknown",
                "return k == KindSealFailed || k == KindUnknown")],
}

CHECKS = [
    {
        "name": "分类只认哨兵常量，不认裸字面量",
        "file": "internal/tracesealer/classify.go",
        "pattern": r"KindTooManyRows\s*FailureKind\s*=\s*\"too_many_rows\"",
        "expect": "present",
        "why": ("这些串会被写进日志与看板/告警规则（轮次汇总里的 kind=…），"
                "是**跨进程契约**；改成任意串就等于把告警规则悄悄打断。"
                "它是「取值」要求（不存在能让用例转红的删除形态）⇒ 只能静态检查。"),
    },
    {
        "name": "classify.go不得建目录",
        "file": "internal/tracesealer/classify.go",
        "pattern": r"\bMkdir[A-Za-z]*\s*\(",
        "expect": "absent",
        "why": ("**D70 的「整包零建目录」**（**E67** 补齐）：全局约束第 27 条要求 "
                "`internal/tracesealer/` 与 `cmd/trace-sealer/` 的**每一个**源文件都不得"
                "出现建目录调用形态 —— 用建目录去「修复」消失的挂载点，会把挂载点重建到"
                "**宿主根文件系统**上（2026-09-24 `/` 被写满的同族事故）。**不存在性**要求 "
                "⇒ 只能静态检查；**工具只在 pristine 树求值**（`trace_mutate.py:574`）⇒ "
                "变异不可能让它变红，可失败性由 **E67 的临时仓库探针**实测（在 `classify.go` "
                "里插入 `os.MkdirAll(root, 0o755)` 并提交到临时仓库 ⇒ 本行 FAIL、工具以"
                "「静态检查失败 ⇒ 整轮失败」退出 1）。本行扫整个文件文本（含注释）。"),
    },
]

REQUIREMENTS = [
    {"plan": "冻结包 errors.go：ErrPartExists 幂等无害（不告警）", "mutations": ["m05_不识别PartExists"], "checks": []},
    {"plan": "冻结包 errors.go：ErrRowCountMismatch 可重试、需告警", "mutations": ["m04_不识别RowMismatch", "m07_Retryable漏掉RowMismatch"], "checks": []},
    {"plan": "冻结包 errors.go：ErrSealFailed 可重试、需告警", "mutations": ["m02_改用相等比较"], "checks": []},
    {"plan": "冻结包 errors.go + N5：ErrTooManyRows 永久拒绝、需人工介入", "mutations": ["m01_不识别TooManyRows", "m06_Permanent恒false"], "checks": []},
    {"plan": "计划 4 全局约束：未分类错误不得被吞", "mutations": ["m03_未分类吞成PartExists"], "checks": []},
    {"plan": "计划 4 全局约束：FailureKind 字符串是日志/告警的稳定面", "mutations": [], "checks": ["分类只认哨兵常量，不认裸字面量"]},
    {"plan": "计划 4 全局约束第 27 条（D70 整包零建目录）：internal/tracesealer/classify.go", "mutations": [], "checks": ["classify.go不得建目录"],
     "note": ("**E67** 补齐：原先只有 runner.go / rootguard.go / lock.go / options.go / main.go "
              "五行，`classify.go` / `alert.go` / `exit.go` / `covers_test.go` 缺 ⇒「整包」名不副实。"
              "不存在性要求 ⇒ 静态检查；可失败性由临时仓库探针实测（见集合 docstring）。")},
]

EXPECTED_GREEN = {}
