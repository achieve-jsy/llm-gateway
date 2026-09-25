"""计划 4 / 任务 1 的变异集合 B：一轮的编排（`Runner.Round`）。

被变异文件 `internal/tracesealer/runner.go`。主题：**一轮的五个承重性质**
（一个坏块不中断整轮 / PartExists 不计失败 / 永久与可重试分开 / 未分类计入失败 /
ctx 取消后不再开新块 / 发现阶段错误必须上抛）。这些性质全部由 `tests/tracesealer/
round_test.go` 的 6 个顶层用例一一对应（每个承重属性一个顶层用例，避免表驱动遮蔽）。

编号一经发布不再改动；匹配一律用字符串（不用行号）。

## 与计划文本的一致性（两处**如实**改动，理由逐字给出）

1. **m02 的插入点**：计划只写「`KindSealFailed` 分支后补一句 `return res, err`」。
   本集合把插入点定在**该分支的自有语句之后、日志之前**（匹配串 `res.SealFailed++`
   在 runner.go 里唯一），语义与计划完全一致：一个坏块立刻中断整轮。
2. **m06 的匹配串**：计划的 `Failed()` 求和式在本实现里跨两行，因此匹配串写成
   `r.RowMismatch + r.SealFailed + r.TooManyRows + r.UnknownFailed`（单行、唯一），
   替换为去掉 `+ r.UnknownFailed` 的形态。语义与计划一致。

## 变异测不到、但有静态检查行（CHECKS）

- **sealer 侧零建目录调用（D70）**：`internal/tracesealer/` 与 `cmd/trace-sealer/`
  不得出现任何建目录调用形态。这是**不存在性**要求（没有任何一条变异能让
  「这里没有建目录调用」这句话变成假）⇒ 只能静态检查。承重证据是计划 4/4 任务 3 的
  `TestRootGuardNeverCreatesRoot`，本行只是同任务范围内的启发式旁证（本 TARGET 单文件）。
- 「`RoundResult.Failed()` 不含 PartExists」的**另一半**（含 PartExists 会怎样）由
  m01 覆盖；但「`Round` 只累加 `Sealed`、把 A8/A6 观测量留成零值」是任务 10 的接口
  前提（本任务不实现观测）⇒ 不属于本集合的变异面。
"""

NAME = "p4-task-1-round"
TARGET = "internal/tracesealer/runner.go"
PACKAGES = ["./tests/tracesealer/"]
MANIFEST_FILES = ["tests/tracesealer/round_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/tracesealer/round_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 全部替身驱动：不依赖 zstd/真实磁盘/数据库，没有可 SKIP 的理由
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# m01 PartExists 被当成封块失败 ⇒ 幂等无害变成天天告警
#     杀手：TestRoundTreatsPartExistsAsIdempotentNoop（res.Failed() 必须为 0）
MUTATIONS["m01_PartExists计入失败"] = [
    ("sub", "res.PartExists++", "res.SealFailed++"),
]
# m02 一个坏块中断整轮（后面所有健康块被无限期拖延）
#     杀手：TestRoundSealsEveryEligiblePart（断言 calls == [0,1,2,3]）
MUTATIONS["m02_坏块中断整轮"] = [
    ("sub", "res.SealFailed++\n\t\t\tr.opts.Logf(",
            "res.SealFailed++\n\t\t\treturn res, err\n\t\t\tr.opts.Logf("),
]
# m03 ctx 取消被忽略 ⇒ 关机时会「封到一半被杀」，留下 .tmp（并可能落在被删 spool 之后）
#     杀手：TestRoundCancellationStopsBeforeNextSeal（断言 calls == [0]）
MUTATIONS["m03_忽略ctx取消"] = [
    ("sub", "if err := ctx.Err(); err != nil {", "if false {"),
]
# m04 发现阶段错误被吞 ⇒ 「根目录读不了」被伪装成「今天没有可封块」
#     杀手：TestRoundDoesNotSwallowListError
MUTATIONS["m04_吞掉发现错误"] = [
    ("sub", 'return res, fmt.Errorf("列举待封块失败: %w", err)', "return res, nil"),
]
# m05 永久失败被计成可重试失败 ⇒ 运维无法从汇总里区分「等一等」与「必须有人拆块」
#     杀手：TestRoundSeparatesPermanentFromRetryable
MUTATIONS["m05_永久混入可重试"] = [
    ("sub", "res.TooManyRows++", "res.RowMismatch++"),
]
# m06 未分类失败不计入 Failed() ⇒ 退出码与告警一起沉默
#     杀手：TestRoundCountsUnknownFailuresAsFailures
MUTATIONS["m06_漏计未分类失败"] = [
    ("sub", "r.RowMismatch + r.SealFailed + r.TooManyRows + r.UnknownFailed",
            "r.RowMismatch + r.SealFailed + r.TooManyRows"),
]

CHECKS = [
    {
        "name": "sealer侧零建目录调用",
        "file": "internal/tracesealer/runner.go",
        "pattern": r"\bMkdir[A-Za-z]*\s*\(",
        "expect": "absent",
        "why": ("D70 / 2026-09-24 事故同类：冻结包在**每次建块**时自己会建产物目录，"
                "因此 sealer 侧任何「用建目录来修复缺失 root」的写法都会把消失的挂载点"
                "在**宿主根文件系统**上重建出来（把 / 写满的那一类事故）。这是**不存在性**"
                "要求 ⇒ 只能静态检查；承重证据是任务 3 的 TestRootGuardNeverCreatesRoot，"
                "本行是同 TARGET 范围内的启发式旁证。注意本行扫**整个文件文本（含注释）**："
                "runner.go 的注释里也不得出现该形态。"),
    },
]

EXPECTED_GREEN = {}

REQUIREMENTS = [
    {
        "plan": "PartExists 幂等无害：不计失败、不告警",
        "mutations": ["m01_PartExists计入失败"],
        "checks": [],
        "note": "杀手 = TestRoundTreatsPartExistsAsIdempotentNoop。",
    },
    {
        "plan": "一个坏块不得中断整轮（发现的健康块必须全部走到）",
        "mutations": ["m02_坏块中断整轮"],
        "checks": [],
        "note": "杀手 = TestRoundSealsEveryEligiblePart（calls == [0,1,2,3]）。",
    },
    {
        "plan": "永久（TooManyRows）与可重试必须分开计数",
        "mutations": ["m05_永久混入可重试"],
        "checks": [],
        "note": "杀手 = TestRoundSeparatesPermanentFromRetryable。",
    },
    {
        "plan": "ctx 取消后不得再开新块（关停机次序）",
        "mutations": ["m03_忽略ctx取消"],
        "checks": [],
        "note": "杀手 = TestRoundCancellationStopsBeforeNextSeal（calls == [0]；语句替换后 err 仍解析到外层 tasks,err，编译通过、断言红）。",
    },
    {
        "plan": "发现阶段错误必须上抛（不得伪装成空轮次）",
        "mutations": ["m04_吞掉发现错误"],
        "checks": [],
        "note": "杀手 = TestRoundDoesNotSwallowListError。",
    },
    {
        "plan": "未分类失败也计入 Failed()（退出码与告警不许沉默）",
        "mutations": ["m06_漏计未分类失败"],
        "checks": [],
        "note": "杀手 = TestRoundCountsUnknownFailuresAsFailures。",
    },
    {
        "plan": "sealer 侧零建目录调用（D70，不存在性要求）",
        "mutations": [],
        "checks": ["sealer侧零建目录调用"],
        "note": "不存在性无法用变异证明 ⇒ 静态检查；承重证据在任务 3。",
    },
]
