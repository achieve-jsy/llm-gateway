"""计划 4 / 任务 1 的变异集合 D：选项默认值与启动期校验（`Options.normalize`）。

被变异文件 `internal/tracesealer/options.go`。主题：**启动期必须响亮拒绝**
（配置错误静默接受只会把现场推到几小时后的封块失败上）——尤其是 R8 的 min-age
强制下界：它是 sealer 与网关之间**唯一**的保护（两者没有信号通道，min-age 不大于
网关滚动间隔就会去封一个仍在追加的块 ⇒ 字节写进已 unlink 的 inode、永久丢失）。

编号一经发布不再改动；匹配一律用字符串（不用行号）。

## 与计划文本的一致性（一处**如实**改动，理由逐字给出）

计划集合 D 的 m02 匹配串写成 `DefaultMinAge       = 3 * time.Hour`（含 gofmt 的
对齐空格）。本实现里该行**逐字相同**（`DefaultMinAge       = 3 * time.Hour`，
因为 `DefaultExpectedZstd`/`DefaultZstdWindowMB` 的宽度与该对齐一致），因此**未改**。
这里如实记录一次核对：既然匹配串与实现逐字一致，就不存在「改实现去迁就变异」的动作。

## 变异测不到、但有静态检查行（CHECKS）

`min-age=%s 必须严格大于网关滚动间隔上界` 这段错误文案本身是**运维可见面**：运维
看到这句话才知道是哪个契约（R8，sealer↔网关的次序不变量）被违反了，而不是看到一个
「配置非法」的泛泛提示。它是「文案必须是某个字面量」的**取值**要求，不存在能删除
它的变异形态（用例只断言了 `ErrorContains` 的片段）⇒ 按全局约束第 6/7 条静态检查。

**`options.go` 自己不得建目录**：`\bMkdir[A-Za-z]*\s*\(` absent（**E66③ 补齐**）。D70
「整包零建目录」原先只有 `runner.go`（`p4-task-1-round.py:93`）与 `rootguard.go`
（`p4-task-3-rootguard.py:133`）两行；`options.go` 的 `normalize()` 恰是最容易被顺手加上
「root 不存在就先建出来」的地方 ⇒ 补这一行。它**不可能**由变异承载（工具只在 pristine 树
求值 `CHECKS`，`trace_mutate.py:574`），可失败性由 **E66③ 的临时仓库探针**实测。
"""

NAME = "p4-task-1-options"
TARGET = "internal/tracesealer/options.go"
PACKAGES = ["./tests/tracesealer/"]
MANIFEST_FILES = ["tests/tracesealer/options_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/tracesealer/options_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 用例只调 NewRunner（注入替身封块器），不碰 zstd/真实盘/数据库，没有可 SKIP 的理由
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# m01 R8 强制下界消失 ⇒ min-age 可以配成 1m、sealer 会去封网关仍在追加的块
#     杀手：TestNewRunnerRejectsMinAgeAtOrBelowGatewayRollUpperBound
MUTATIONS["m01_去掉min-age下界"] = [
    ("sub", "if o.MinAge <= MinAgeFloor {", "if false {"),
]
# m02 默认 min-age 从 3h 缩到 30m ⇒ 缩短了与网关滚动之间的安全差（默认值即生产值）
#     杀手：TestNewRunnerDefaultsMinAgeToThreeHours
MUTATIONS["m02_默认min-age缩到30m"] = [
    ("sub", "DefaultMinAge       = 3 * time.Hour", "DefaultMinAge       = 30 * time.Minute"),
]
# m03 窗口 2 的幂检查被摘掉 ⇒ 非 2 的幂被接受，几小时后在封块时才炸（且错误指向 zstd）
#     杀手：TestNewRunnerRejectsNonPowerOfTwoWindow
MUTATIONS["m03_不校验2的幂"] = [
    ("sub", "if o.ZstdWindowMB&(o.ZstdWindowMB-1) != 0 {",
            "if false && o.ZstdWindowMB&(o.ZstdWindowMB-1) != 0 {"),
]

CHECKS = [
    {
        "name": "min-age错误文案指向R8契约",
        "file": "internal/tracesealer/options.go",
        "pattern": r"min-age=%s 必须严格大于网关滚动间隔上界",
        "expect": "present",
        "why": ("错误文案是运维可见面：运维看到这句话才知道被违反的是 R8"
                "（sealer 与网关之间唯一的次序保护），而不是一个泛泛的「配置非法」。"
                "用例只断言 ErrorContains 的片段 ⇒ 不存在能删除整句的变异形态，"
                "按全局约束第 6/7 条静态检查。"),
    },
    {
        "name": "options.go不得建目录",
        "file": "internal/tracesealer/options.go",
        "pattern": r"\bMkdir[A-Za-z]*\s*\(",
        "expect": "absent",
        "why": ("**D70 的「整包零建目录」**（**E66③** 补齐）：原先只有 `runner.go`"
                "（`p4-task-1-round.py:93`）与 `rootguard.go`（`p4-task-3-rootguard.py:133`）"
                "各有该行，`options.go` 缺；而计划全局约束第 27 条要求 `internal/tracesealer/` 的"
                "**每一个**源文件都不得出现建目录调用形态（`normalize()` 是最容易被顺手加上"
                "「root 不存在就先建出来」的地方 —— 那正是把消失的挂载点重建到宿主根 FS 上的"
                "形态，2026-09-24 事故同族）。**不存在性**要求 ⇒ 静态检查；**工具只在 pristine 树"
                "求值**（`trace_mutate.py:574`）⇒ 变异不可能让它变红，可失败性由 **E66③ 的临时"
                "仓库探针**实测（插入 `os.MkdirAll(o.Root, 0o755)` 并提交 ⇒ 本行 FAIL、退出 1）。"),
    },
]

EXPECTED_GREEN = {}

REQUIREMENTS = [
    {
        "plan": "R8：min-age 必须严格大于网关滚动间隔上界（1h），违反即启动失败",
        "mutations": ["m01_去掉min-age下界"],
        "checks": [],
        "note": "杀手 = TestNewRunnerRejectsMinAgeAtOrBelowGatewayRollUpperBound（1m/30m/1h 三例；边界相等也必须被拒）。",
    },
    {
        "plan": "默认 min-age = 3h（= 冻结包 defaultSealMinAge 的镜像值）",
        "mutations": ["m02_默认min-age缩到30m"],
        "checks": [],
        "note": "杀手 = TestNewRunnerDefaultsMinAgeToThreeHours。",
    },
    {
        "plan": "zstd --long=<log2> 的前提：窗口必须是 2 的幂（启动期拒绝，而不是封块时才炸）",
        "mutations": ["m03_不校验2的幂"],
        "checks": [],
        "note": "杀手 = TestNewRunnerRejectsNonPowerOfTwoWindow。",
    },
    {
        "plan": "配置错误一律响亮失败，错误文案点明被违反的契约（运维可见面）",
        "mutations": [],
        "checks": ["min-age错误文案指向R8契约"],
        "note": "文案取值要求 ⇒ 静态检查。",
    },
    {
        "plan": "D70：整包零建目录 —— `options.go`（含 `normalize()`）不得出现任何建目录调用",
        "mutations": [],
        "checks": ["options.go不得建目录"],
        "note": ("不存在性要求 ⇒ 静态检查（**E66③** 补齐）。可失败性由 E66③ 的临时仓库探针承载 —— "
                 "工具只在 pristine 树求值 `CHECKS`（`trace_mutate.py:574`）。"),
    },
]
