r"""计划 4 / 任务 4 的变异集合 A：跨进程单实例锁（`internal/tracesealer/lock.go`）。

主题：**「同一个 root 只允许一个 sealer」必须由内核级排他锁保证** —— systemd 只保证
**它自己**起一个，挡不住运维手工 `--once` 与常驻循环撞车；两个进程对同一个
(dt, inst, part) 并发 `Seal` 会共用同一个 `<partPath>.tmp`（冻结包 writer.go 明文
要求同一三元组串行），而发现路径的 `os.Stat(partPath)` 也不是原子的。

编号一经发布不再改动；匹配一律用字符串（不用行号）。

## 与计划文本的一致性（如实登记：一处**拆分**、两处**加强**，零处削弱）

1. **集合拆分（计划文本缺陷的就地订正）**：计划任务 4 步骤 6 把 m02–m05（`lock.go`）
   与 m06/m07（`runner.go` 的 `NewRunner`）写进**同一个** `p4-task-4-lock.py`，并写
   `TARGET = "internal/tracesealer/lock.go"`。这与全局约束第 42 条（`TARGET` 是单值，
   **工具一次只能变异一个文件** ⇒ 一个任务按被变异文件拆成多个集合）直接冲突：把
   runner.go 的匹配串交给 TARGET=lock.go 的集合，只会在 `--check` 阶段报
   `PATTERN-MISS: count=0`。⇒ 按既有命名先例（`p2-task-8.py` / `p2-task-8-spooler.py`）
   就地拆成两个集合：本文件（lock.go 的 m02–m05）与 `p4-task-4-runner.py`
   （runner.go 的 m06–m08）。计划表格的编号**原样保留**（m02–m08 全局唯一），因此
   两份集合合起来与计划表格一一对应。
2. **m01 不实现（原样保留计划的说明）**：计划 m01「加锁改用阻塞模式（去掉 `LOCK_NB`）」
   明确写着「**不得**这样变异：阻塞会让整批挂死，红成「超时」= 无效证据」。让批次
   挂死的变异不是好变异 —— 这条以注释保留在 `MUTATIONS` 上方，防止后来者「顺手补上
   一条更强的变异」而把证据质量弄坏。
3. **名字清单扩到 concurrent_test.go（加强，E62 的要求）**：计划只写
   `MANIFEST_FILES = ["tests/tracesealer/lock_test.go"]`；但 A7 的自动化用例在
   `tests/tracesealer/concurrent_test.go`，而 **E62 规则 1** 要求「行为要求必须能在某张
   权威表的**名字清单**里找到承载它的顶层用例」。⇒ 两份 Task 4 集合的名字清单 =
   `lock_test.go` + `concurrent_test.go` 的全部顶层用例（13 条）。这样
   `TestA7RoundIsSerializedWithLock`（被 m02/m03/m06 杀）与四条次序/重启/串行用例
   都在册。
4. **新增一条静态检查**「runner 不得有第二处锁实现」（计划已有的那条）＋「锁文件默认
   落在 root 之内」（`Options.LockFile` 的默认值写在 `options.go`）＋三条 A7 用例的
   存在性检查：计划为 A7 只给了一条静态检查（`TestA7WriterNeverAppendsToAHandedOffPart`
   present），另外三条 A7 用例的**存在性**同样只能静态检查（行为由名字清单里的用例
   承载；被测对象是冻结包，本计划不得变异它）。**如实登记**：这四条用例的**可失败性**
   由人工探针承载（见 `COVERAGE_GAPS`），不是由变异承载。

## 变异测不到、但有静态检查行（CHECKS）

- **`flock` + 非阻塞**：`syscall\.Flock` 与 `LOCK_NB` 必须在 `lock.go` 里 present。
  「用 flock 而不是家酿的「文件存在即认为有实例」」与「非阻塞」都是**结构/取值**要求
  （家酿锁在崩溃后留下的陈旧文件会让 sealer 永久起不来；阻塞加锁会把运行中的
  sealer 变成一次神秘挂起）⇒ 静态检查；行为面由 m02/m03 与四个用例承载。
- **锁文件必须落在 root 之内**：`options.go` 的 `filepath.Join(o.Root, ".sealer.lock")`
  present。root 之外没有可写保证（`ProtectSystem=strict` + `ReadWritePaths=/datalog`），
  且只有落在 root 里才能被「同一个 root 只有一个写者」这条语义覆盖 ⇒ 取值要求。
- **A7 用例存在性**（四条）：见上 4。行为由名字清单里的用例承载。
- **`runner.go` 里不得有第二处锁实现**：`syscall\.Flock|os\.Mkdir` absent。互斥只能有
  一处实现；在 `NewRunner` 里再补一层「目录/文件存在即认为有实例」的家酿锁，会在
  崩溃后把 sealer **永久**挡在外面 ⇒ 不存在性要求。
- **`lock.go` 自己不得建目录**：`\bMkdir[A-Za-z]*\s*\(` absent（**E66③ 补齐**）。
  D70「整包零建目录」原先只覆盖 `runner.go`（本集合）与 `rootguard.go`
  （`p4-task-3-rootguard.py:133`），`lock.go` / `options.go` / `cmd/trace-sealer/main.go`
  **都没有该行** ⇒ 本轮三个集合各补一行，使「整包」名副其实。它**不可能**由变异承载
  （工具只在 pristine 树求值 `CHECKS`，见 `trace_mutate.py:574`），可失败性由
  **E66③ 的临时仓库探针**实测。
"""

NAME = "p4-task-4-lock"
TARGET = "internal/tracesealer/lock.go"
# 一个包（仅公开 API）：本集合的被变异文件 lock.go 只被 tests/tracesealer 的公开 API
# 用例覆盖（`AcquireLock` / `ErrAnotherSealerRunning` / `Runner.Close` 都导出）。
PACKAGES = ["./tests/tracesealer/"]
MANIFEST_FILES = [
    "tests/tracesealer/lock_test.go",
    "tests/tracesealer/concurrent_test.go",
]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/tracesealer/lock_test.go tests/tracesealer/concurrent_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 全部替身驱动：不依赖 zstd / 真实磁盘 / 真实并发，没有可 SKIP 的理由
SKIP_WHITELIST = []
RETIRED = {}
EXPECTED_GREEN = {}

MUTATIONS = {}

# 【m01 的说明必须原样保留 —— 计划任务 4 步骤 6 的表注】
# m01「加锁改用阻塞模式（去掉 LOCK_NB）」**不得**这样变异：阻塞会让整批挂死，
# 那产出的红是「超时红」（无效证据），而不是「断言红」。因此本集合从 m02 起编号
# （与计划表格的编号对齐），把 m01 永久退役在注释里，避免后来者「顺手补上一条
# 更强的变异」而把证据质量弄坏。
# m02 排他变共享 ⇒ 两个 sealer 都能进：同一 (dt,inst,part) 会被两个进程并发 Seal
#     杀手：TestLockIsExclusive（另：TestA7RoundIsSerializedWithLock）
MUTATIONS["m02_排他变共享"] = [
    ("sub", "syscall.LOCK_EX|syscall.LOCK_NB", "syscall.LOCK_SH|syscall.LOCK_NB"),
]
# m03 冲突时不再返回哨兵错误 ⇒ 调用方拿到 (nil, nil)，以为加锁成功
#     杀手：TestLockIsExclusive（另：TestNewRunnerRefusesWhenLockHeld、TestA7RoundIsSerializedWithLock）
MUTATIONS["m03_冲突不返回哨兵"] = [
    ("sub", 'return nil, fmt.Errorf("%w（锁文件 %s）", ErrAnotherSealerRunning, path)',
            "return nil, nil"),
]
# m04 Release 不再解锁/关文件 ⇒ 一次排障之后 sealer 永久起不来（要重启宿主机才能修）
#     杀手：TestLockReleasedAllowsReacquire（另：TestNewRunnerReleasesLockOnClose）
MUTATIONS["m04_释放不解锁"] = [
    ("sub", "\tf := l.f\n\tl.f = nil\n"
            "\t// 先显式解锁再关闭：即使 Close 失败，锁也已经放开，不会把下一个 sealer 挡在外面。\n"
            "\t_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)\n\treturn f.Close()",
            "\treturn nil"),
]
# m05 Release 不把 l.f 置空 ⇒ 第二次调用去 close 一个已关闭的 fd（退出路径 defer + 显式
#     会调两次）⇒ 幂等性被破坏
#     杀手：TestLockReleaseIsIdempotent
MUTATIONS["m05_释放不置空"] = [
    ("sub", "\tf := l.f\n\tl.f = nil\n", "\tf := l.f\n"),
]

CHECKS = [
    {
        "name": "锁必须用flock",
        "file": "internal/tracesealer/lock.go",
        "pattern": r"syscall\.Flock",
        "expect": "present",
        "why": ("互斥必须是**内核级**的（flock）：家酿的「文件存在即认为有实例」在崩溃后"
                "留下陈旧文件，会把 sealer **永久**挡在外面（要人工删文件才能修）。"
                "它是「用哪种机制」的取值要求 ⇒ 静态检查；行为面由 m02/m03 与 "
                "TestLockIsExclusive 承载。"),
    },
    {
        "name": "加锁必须非阻塞",
        "file": "internal/tracesealer/lock.go",
        "pattern": r"LOCK_NB",
        "expect": "present",
        "why": ("非阻塞（LOCK_NB）是**排障可用性**要求：第二个 sealer 必须**立刻**拿到"
                "ErrAnotherSealerRunning，而不是排队等 —— 等会把一次手工排障变成一次"
                "神秘挂起。阻塞形态不能作为变异（挂死 ⇒ 超时红 ⇒ 无效证据），因此这一"
                "半只能静态检查，另一半由 m02/m03 承载。"),
    },
    {
        "name": "锁文件默认落在root之内",
        "file": "internal/tracesealer/options.go",
        "pattern": r'filepath\.Join\(o\.Root, "\.sealer\.lock"\)',
        "expect": "present",
        "why": ("root 之外没有可写保证（`ProtectSystem=strict` + `ReadWritePaths=/datalog`），"
                "而且只有落在 root 里才能被「同一个 root 只有一个写者」这条语义覆盖 ⇒ "
                "它必须在 `normalize()` 里默认成 `<root>/.sealer.lock`（`--lock-file` 只能"
                "覆盖它，不能取消它）。取值要求 ⇒ 静态检查。"),
    },
    {
        "name": "A7次序不变量用例在册",
        "file": "tests/tracesealer/concurrent_test.go",
        "pattern": r"func TestA7WriterNeverAppendsToAHandedOffPart\(",
        "expect": "present",
        "why": ("计划任务 4 步骤 6 明确要求的一条：A7 的次序不变量（先 MarkSealed、后交接）"
                "必须有**可执行载体**；它的被测对象是冻结包的次序，本计划不得变异冻结包 ⇒ "
                "这一条**只能静态检查存在性**，行为由名字清单里的该用例承载（清单里有它 ⇒ "
                "权威表的执行集合里必有它）。**如实登记**：该用例没有变异杀手，可失败性由"
                "人工探针承载（见 COVERAGE_GAPS）。"),
    },
    {
        "name": "A7落点用例在册",
        "file": "tests/tracesealer/concurrent_test.go",
        "pattern": r"func TestA7NoRefPointsToNothing\(",
        "expect": "present",
        "why": ("「每一个已返回的 Ref 都必须有落点」是**行为**要求，承载它的是名字清单里的"
                "该用例（被测对象是冻结包 Writer + 替身 sealer 的次序，本计划不得变异冻结包）"
                "⇒ 静态检查只钉住它的**存在性**（防止用例被删而白单里没人发现）。"),
    },
    {
        "name": "A7重启续号用例在册",
        "file": "tests/tracesealer/concurrent_test.go",
        "pattern": r"func TestA7RestartAfterSealStartsAtNextPart\(",
        "expect": "present",
        "why": ("「产物已存在、spool 已删之后，新 Writer 必须从 part+1 继续」是行为要求，"
                "承载它的是名字清单里的该用例；静态检查只钉存在性（同上）。"),
    },
    {
        "name": "runner不得有第二处锁实现",
        "file": "internal/tracesealer/runner.go",
        "pattern": r"syscall\.Flock|os\.Mkdir",
        "expect": "absent",
        "why": ("互斥只能有**一处**实现。在 `NewRunner` 里再补一层「文件/目录存在即认为有"
                "实例」的家酿锁，会在崩溃后把 sealer 永久挡在外面（D70 同族）；在 runner 里"
                "再取一次 flock 则会让同一进程的两次装配互相拒绝。不存在性要求 ⇒ 静态检查"
                "（本行扫整个文件文本，含注释）。"),
    },
    {
        "name": "lock.go不得建目录",
        "file": "internal/tracesealer/lock.go",
        "pattern": r"\bMkdir[A-Za-z]*\s*\(",
        "expect": "absent",
        "why": ("**D70 的「整包零建目录」**（**E66③** 补齐）：本集合原先只在 `runner.go` 上有这"
                "一行，`lock.go` 缺该行；而计划全局约束第 27 条要求 `internal/tracesealer/` 与 "
                "`cmd/trace-sealer/` 的**每一个**源文件都不得出现建目录调用形态 —— 用建目录去"
                "「修复」消失的挂载点，会把挂载点重建到**宿主根文件系统**上（2026-09-24 `/` 被"
                "写满的同族事故）。**不存在性**要求 ⇒ 只能静态检查（变异无法证明「没写」）。"
                "**工具只在 pristine 树求值**（`trace_mutate.py:574` 的 `print_checks(setmod, tree)`，"
                "`tree` 由 `git archive HEAD` 现取现解）⇒ 任何变异都不可能让本行变红；它的**可失败性**"
                "由 **E66③ 的临时仓库探针**实测（在 `lock.go` 里插入 `os.MkdirAll(o.Root, 0o755)` 并"
                "提交到临时仓库 ⇒ 本行 FAIL、工具以「静态检查失败 ⇒ 整轮失败」退出 1）。"),
    },
]

# 本集合**没有任何**允许的绿：m02–m05 全部断言红。
# 四条 A7 用例（WriterNeverAppends / NoRefPointsToNothing / RestartAfterSeal）中的
# 「没有变异杀手」不是「允许的绿」—— 它们在权威表里是**绿的基线用例**（pristine 侧
# PASS），只是没有变异能杀它们（被测对象是冻结包，本计划不得变异冻结包）。按 E64②
# 把它们登记进 COVERAGE_GAPS，并把可失败性交给人工探针。
REQUIREMENTS = [
    {
        "plan": "同一个 root 同时只能有一个 sealer：第二个持有者必须**立刻**失败（装配期就要挡住）",
        "mutations": ["m02_排他变共享", "m03_冲突不返回哨兵"],
        "checks": [],
        "note": ("杀手 = TestLockIsExclusive（锁语义）；另 TestA7RoundIsSerializedWithLock "
                 "（装配期语义：常驻循环持锁期间手工 --once 必须被拒）。"),
    },
    {
        "plan": "Release 必须真正放开锁：释放之后必须能被重新获取（否则一次排障后 sealer 永久起不来）",
        "mutations": ["m04_释放不解锁"],
        "checks": [],
        "note": "杀手 = TestLockReleasedAllowsReacquire；另 TestNewRunnerReleasesLockOnClose（Close 走同一条 Release）。",
    },
    {
        "plan": "Release 幂等：退出路径（defer + 显式）会调两次，第二次必须无害",
        "mutations": ["m05_释放不置空"],
        "checks": [],
        "note": "杀手 = TestLockReleaseIsIdempotent（第二次 close 已关闭的 fd ⇒ 返回错误）。",
    },
    {
        "plan": "互斥必须是内核级 flock，且必须非阻塞（阻塞会把排障变成一次神秘挂起）",
        "mutations": ["m02_排他变共享", "m03_冲突不返回哨兵"],
        "checks": ["锁必须用flock", "加锁必须非阻塞"],
        "note": ("「用哪种机制」与「非阻塞」是结构/取值要求 ⇒ 两行静态检查；行为面由 "
                 "m02/m03 + TestLockIsExclusive 承载。阻塞形态**不作变异**（挂死 ⇒ 超时红 ⇒ "
                 "无效证据，见 MUTATIONS 上方的 m01 说明）。"),
    },
    {
        "plan": "锁文件必须落在 root 之内（root 之外没有可写保证，且只有 root 内才被「同一 root 单写者」覆盖）",
        "mutations": [],
        "checks": ["锁文件默认落在root之内"],
        "note": "`normalize()` 的默认值 ⇒ 取值要求 ⇒ 静态检查；`--lock-file` 只覆盖它。",
    },
    {
        "plan": "互斥只能有一处实现：`runner.go` 里不得再补一层家酿锁",
        "mutations": [],
        "checks": ["runner不得有第二处锁实现"],
        "note": "不存在性要求 ⇒ 静态检查（D70 同族的家酿锁会把 sealer 永久挡在外面）。",
    },
    {
        "plan": "A7 次序不变量：一旦 part 被交接（先 MarkSealed 关 fd），后续 Append 绝不允许再落到它上（D7）",
        "mutations": [],
        "checks": ["A7次序不变量用例在册"],
        "note": ("在册行为用例 = TestA7WriterNeverAppendsToAHandedOffPart（名字清单里有它，"
                 "权威表的执行集合里必有它）。**没有变异杀手**：被测对象是冻结包 Writer 的"
                 "次序，本计划不得变异冻结包 ⇒ 按 E64①/E64② 如实登记进 COVERAGE_GAPS，"
                 "并与一条静态检查（存在性）配对。"),
    },
    {
        "plan": "A7 落点：每一个已返回的 Ref 都必须有落点（产物在场，或 spool 在场且该行存在）",
        "mutations": [],
        "checks": ["A7落点用例在册"],
        "note": "在册行为用例 = TestA7NoRefPointsToNothing；无变异杀手（同上，见 COVERAGE_GAPS）。",
    },
    {
        "plan": "A7 重启：产物已存在、spool 已删之后，新 Writer 必须从 part+1 继续",
        "mutations": [],
        "checks": ["A7重启续号用例在册"],
        "note": "在册行为用例 = TestA7RestartAfterSealStartsAtNextPart；无变异杀手（同上，见 COVERAGE_GAPS）。",
    },
    {
        "plan": "D70：整包零建目录 —— `lock.go` 里不得出现任何建目录调用（含注释）",
        "mutations": [],
        "checks": ["lock.go不得建目录"],
        "note": ("不存在性要求 ⇒ 静态检查（**E66③** 补齐；同族两行在 `p4-task-1-round.py` 的 "
                 "`runner.go` 与 `p4-task-3-rootguard.py` 的 `rootguard.go`）。可失败性由 E66③ 的"
                 "临时仓库探针承载 —— 工具只在 pristine 树求值 `CHECKS`（`trace_mutate.py:574`），"
                 "变异**不可能**让它变红。"),
    },
]

# ---------------------------------------------------------------------------
# 零变异覆盖的**用例**登记（E64②；**只是给人看的登记，工具读不到、不参与任何判定**）
#
# 为什么单列一个结构、而不是写进 EXPECTED_GREEN / REQUIREMENTS（复用既有字段名
# COVERAGE_GAPS，见 tools/trace-mutate/sets/task-5.py 与 p4-task-3-loop.py）：
#   - EXPECTED_GREEN 的键被工具当作**变异名**处理 ⇒ 塞进一个「没有变异杀手的用例名」
#     会让那张清单出现永远对不上的键。
#   - REQUIREMENTS 对「mutations 与 checks 都为空」的条目直接判 GAP ⇒ 整轮失败。
# 登记口径（沿用 p4-task-3-loop.py 的 COVERAGE_GAPS）：位置 / 断言的什么 / 为什么
# 没有杀手 / 可失败性由谁承载 / 谁来补。
# ---------------------------------------------------------------------------
COVERAGE_GAPS = [
    {
        "id": "gap-1-A7-冻结包次序三条用例无变异杀手",
        "title": ("TestA7WriterNeverAppendsToAHandedOffPart / TestA7NoRefPointsToNothing / "
                  "TestA7RestartAfterSealStartsAtNextPart（tests/tracesealer/concurrent_test.go）"
                  "在册、承重，但 Task 4 两个集合的变异**没有一条**能杀它们"),
        "where": [
            "tests/tracesealer/concurrent_test.go  TestA7WriterNeverAppendsToAHandedOffPart（D7 次序不变量）",
            "tests/tracesealer/concurrent_test.go  TestA7NoRefPointsToNothing（每个 Ref 都有落点）",
            "tests/tracesealer/concurrent_test.go  TestA7RestartAfterSealStartsAtNextPart（重启续号）",
        ],
        "reachable": (
            "**可达且确定**：三者都只用公开 API（冻结包 `Writer` + 替身 sealer），不依赖 "
            "zstd / 真实并发 / 数据库，pristine 侧每次运行都执行并 PASS。"
        ),
        "no_killer": (
            "**被测对象是冻结包 `internal/tracearchive` 的次序与目录扫描**，而「冻结包零改动」"
            "是本计划的铁律（它自己另有 142 条变异证据 R11 v3，冻结提交 `ea962a70`）⇒ 任何"
            "变异都不得落在冻结包里。Task 4 两个集合的 TARGET 分别是 lock.go 与 runner.go，"
            "两者都不改变这三条用例的判定 ⇒ **没有变异杀手不是「允许的绿」，而是「本计划"
            "不可变异该文件」这条铁律的直接后果**（E64① 要求：不得用「本计划不覆盖」为"
            "从未触达的路径开脱 —— 这里给的是在册理由，并按 E64② 登记可失败性的承载者）。"
        ),
        "falsifiability": (
            "**三条各自人工探针，均可复现、均按 `cp` 还原**（被改文件 "
            "tests/tracesealer/concurrent_test.go @ `0864c519` 还原后 sha256 = "
            "6ec687db153fbdeb… 与提交逐字节一致，`git status --porcelain` 为空；"
            "**E66①：以下 `*.go:NNN` 均为「改动后行号」**）："
            "① 把 `sealOnce` 里的 `w.MarkSealed(task.DT, task.Part)` 删掉（= 交接前不关 fd，"
            "D7 的形态）⇒ TestA7WriterNeverAppendsToAHandedOffPart 在 concurrent_test.go:161 "
            "的 `require.False(t, sealed[ref.Part], ...)` 处 FAIL（Should be false / "
            "「part 0 已交接给 sealer，不得再被追加（D7）」）；"
            "② 把 `handoff` 里的 `os.WriteFile(art, ...)` 删掉（= 产物没落盘、spool 也删了）"
            "⇒ TestA7NoRefPointsToNothing 在 concurrent_test.go:141 的 "
            "`require.Len(t, hs.handed, 4)` 处 FAIL（只发生了 1 次交接 —— 产物缺失让冻结包的"
            "目录扫描把序号 0 **又分配了一次**，这本身就是同一个失效面），"
            "TestA7RestartAfterSealStartsAtNextPart 在 concurrent_test.go:188 的 "
            "`require.FileExists(t, p.PartFile(zero), ...)` 处 FAIL；"
            "③ 「Ref 丢了」这条判定单独探针：交接全部完成后把产物目录整体删掉再调 "
            "`assertRefsLand` ⇒ TestA7NoRefPointsToNothing 在 concurrent_test.go:146 的 "
            "`require.NoError(t, hs.assertRefsLand(refs))` 处 FAIL"
            "（`Ref 2026-10-08/blue/part-000000#row=0 丢了：产物与 spool 都不在`）。"
        ),
        "owner": (
            "本轮（Task 4）**只登记、不新增变异**：要让它升格为断言红，需要允许变异冻结包"
            "（本计划明确不允许），或把「次序」复制成 sealer 侧的一层并变异那一层"
            "（那会产生两份次序真相，反而更脆）。计划任务 8 的进程级剧本覆盖真实进程形态。"
        ),
    },
]
