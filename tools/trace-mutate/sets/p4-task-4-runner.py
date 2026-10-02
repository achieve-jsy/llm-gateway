r"""计划 4 / 任务 4 的变异集合 B：装配期的锁接线与释放（`internal/tracesealer/runner.go`）。

主题：**单实例锁必须在装配期取得、在失败路径与关闭路径上释放干净** ——
`NewRunner` 的装配次序（`normalize()` → `NewRootGuard()` → `AcquireLock()`）本身是契约：
「先建目录再取锁」会把消失的挂载点在**宿主根文件系统**上重建（D70），而「构造封块器
失败却握着锁不放」会把一次配错变成下一次启动的「已有另一个 sealer 持有锁」。

编号一经发布不再改动；匹配一律用字符串（不用行号）。

## 与计划文本的一致性（如实登记：一处**拆分**、两处**加强**，零处削弱）

1. **集合拆分**：计划任务 4 步骤 6 把 m02–m05（lock.go）与 m06/m07（runner.go）塞进
   同一个 `p4-task-4-lock.py`（`TARGET = lock.go`），与全局约束第 42 条冲突
   （`TARGET` 单值，工具一次只能变异一个文件）。⇒ 就地拆出本集合承载 runner.go 的
   m06/m07，并**新增 m08/m09**（见下一节的 2/3）。计划表格的编号原样保留。
2. **新增 m08**（`Close` 不释放锁）：计划只写了 m06/m07，但 `Runner.Close()` 的接口
   契约是「释放锁」⇒ 「Close 接线」这条**行为**要求必须有一条直取它的变异，否则
   `TestNewRunnerReleasesLockOnClose` 的断言无在册杀手（只有 set A 的 m04 会**顺带**
   杀它，而 m04 的被测文件是 lock.go、点名的是 TestLockReleasedAllowsReacquire）。
3. **新增 m09**（装配失败不释放已取得的锁）：计划步骤 3 明文要求「`NewRunner` 失败时
   必须把已取得的资源释放干净」⇒ 同上，补一条直取该错误分支的变异与一条在册行为用例
   `TestNewRunnerReleasesLockWhenSealerFails`（E62：行为要求必须由在册用例承载）。
4. **新增 m07 的实现形态（如实订正计划文本）**：计划 m07 写「插入 `os.MkdirAll`」，
   而 `runner.go` **没有** `os` 导入 ⇒ 直接插入会红成**编译红**（无效证据，违反全局
   约束第 46 条）。因此 m07 由**两个操作**组成：先把 `"os"` 插进导入块，再插
   `os.MkdirAll(opts.Root, 0o755)`；语义与计划逐字相同（「先建 root 再取锁」）。
5. **名字清单** = `lock_test.go` + `concurrent_test.go` 的全部顶层用例（与
   `p4-task-4-lock.py` 相同，理由见该集合文件头 3）。

## 变异测不到、但有静态检查行（CHECKS）

- **`runner.go` 里不得有第二处锁实现**：`syscall\.Flock|os\.Mkdir` absent（同一处
  互斥只能有一个实现；家酿锁在崩溃后会把 sealer 永久挡在外面）。
- **`cmd/trace-sealer/main.go` 必须 `defer r.Close()`**：进程**退出前必须调用** Close
  （`Runner.Close` 的接口契约）。它是接线要求 ⇒ 静态检查；「Close 真的释放锁」由
  m08 + `TestNewRunnerReleasesLockOnClose` 承载。
- **A7 轮内不重复封块用例在册**：`TestA7NoPartIsSealedTwiceConcurrently` present
  （被测对象是冻结包对同一三元组「必须串行」的要求，本计划不得变异冻结包 ⇒ 只钉
  存在性；行为由名字清单里的用例承载）。
- **Close 幂等用例在册**：`TestNewRunnerCloseIsIdempotent` present（幂等由两层保证，
  runner.go 侧单点变异杀不掉，见 COVERAGE_GAPS）。
"""

NAME = "p4-task-4-runner"
TARGET = "internal/tracesealer/runner.go"
# 一个包（仅公开 API）：`NewRunner` / `Runner.Close` / `ErrAnotherSealerRunning` 都导出。
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

# m06 装配期不再取锁 ⇒ systemd 起的常驻循环与运维手工 --once 可以同时封同一
#     (dt,inst,part)（共用同一个 .tmp）
#     杀手：TestNewRunnerRefusesWhenLockHeld（另：TestA7RoundIsSerializedWithLock）
MUTATIONS["m06_装配不取锁"] = [
    ("sub", "\tlock, err := AcquireLock(opts.LockFile)\n\tif err != nil {\n\t\treturn nil, err\n\t}",
            "\tvar lock *Lock"),
]
# m07 先建 root 再取锁（顺序颠倒）⇒ 根目录不存在时它被**创建**出来：消失的挂载点被
#     重建到宿主根文件系统上，正文随后写进那片没人会去看的空间（D70 / 2026-09-24 事故）
#     杀手：TestNewRunnerNeverCreatesRoot
#     【两个操作】计划只写「插入 os.MkdirAll」，但 runner.go 没有 os 导入 ⇒ 直接插入
#     是编译红（无效证据）⇒ 先补导入再插调用，语义与计划逐字相同。
MUTATIONS["m07_先建根目录再取锁"] = [
    ("sub", '\t"fmt"\n\t"log/slog"\n', '\t"fmt"\n\t"log/slog"\n\t"os"\n'),
    ("sub", "\tguard, err := NewRootGuard(opts.Root, opts.Identity)",
            "\t_ = os.MkdirAll(opts.Root, 0o755)\n\tguard, err := NewRootGuard(opts.Root, opts.Identity)"),
]
# m08 Close 不再释放锁 ⇒ 进程可控退出后锁仍然被自己握着（下一次启动被自己的残留挡住）
#     杀手：TestNewRunnerReleasesLockOnClose
MUTATIONS["m08_Close不释放锁"] = [
    ("sub", "\tl := r.lock\n\tr.lock = nil\n\treturn l.Release()", "\treturn nil"),
]
# m09 构造封块器失败时不释放已取得的锁 ⇒ 一次配错（例如 zstd 路径写错）会把锁一直
#     握到进程退出，把下一次启动误导成「已有另一个 sealer 持有锁」
#     杀手：TestNewRunnerReleasesLockWhenSealerFails
MUTATIONS["m09_装配失败不释放锁"] = [
    ("sub", "\t\t_ = lock.Release()\n", ""),
]

CHECKS = [
    {
        "name": "runner不得有第二处锁实现",
        "file": "internal/tracesealer/runner.go",
        "pattern": r"syscall\.Flock|os\.Mkdir",
        "expect": "absent",
        "why": ("互斥只能有**一处**实现。在 `NewRunner` 里再补一层「文件/目录存在即认为有"
                "实例」的家酿锁，会在崩溃后把 sealer 永久挡在外面（D70 同族）。不存在性"
                "要求 ⇒ 静态检查（本行扫整个文件文本，含注释）。"),
    },
    {
        "name": "main必须释放锁",
        "file": "cmd/trace-sealer/main.go",
        "pattern": r"defer r\.Close\(\)",
        "expect": "present",
        "why": ("`Runner.Close()` 的接口契约是「**进程退出前必须调用**」（释放单实例锁）。"
                "它是接线要求 ⇒ 静态检查；「Close 真的释放锁」由 m08 + "
                "TestNewRunnerReleasesLockOnClose 承载。"),
    },
    {
        "name": "A7轮内不重复封块用例在册",
        "file": "tests/tracesealer/concurrent_test.go",
        "pattern": r"func TestA7NoPartIsSealedTwiceConcurrently\(",
        "expect": "present",
        "why": ("冻结包要求同一个 (dt,inst,part) 串行（两块共用同一个 <partPath>.tmp）。"
                "本用例的被测对象是冻结包的这一要求 + Round 的串行编排 ⇒ 本计划不得变异"
                "冻结包 ⇒ 只钉**存在性**（防止用例被删而没人发现）；行为由名字清单里的该"
                "用例承载，可失败性由人工探针承载（见 COVERAGE_GAPS）。"),
    },
    {
        "name": "Close幂等用例在册",
        "file": "tests/tracesealer/lock_test.go",
        "pattern": r"func TestNewRunnerCloseIsIdempotent\(",
        "expect": "present",
        "why": ("`Runner.Close()` 的接口契约写明「幂等」（defer + 显式两条退出路径）。"
                "runner.go 侧的单点变异杀不掉它（幂等由 `r.lock = nil` 与 `Lock.Release` "
                "自己的幂等两层保证）⇒ 静态检查只钉存在性，行为由名字清单里的用例承载。"),
    },
]

# 本集合**没有任何**允许的绿：m06–m09 全部断言红。
# `TestNewRunnerCloseIsIdempotent` 与 `TestA7NoPartIsSealedTwiceConcurrently` 是
# **绿的基线用例**（pristine 侧 PASS）而**没有变异杀手**；按 E64② 登记进
# COVERAGE_GAPS（它们不是「允许的绿」—— 允许的绿指的是**变异**判绿的登记）。
REQUIREMENTS = [
    {
        "plan": "装配期就必须取锁：同一个 root 的第二个实例（含运维手工 --once）必须被拒",
        "mutations": ["m06_装配不取锁"],
        "checks": [],
        "note": ("杀手 = TestNewRunnerRefusesWhenLockHeld；另 TestA7RoundIsSerializedWithLock"
                 "（常驻循环持锁期间手工 --once 被装配期挡住）。跨集合：集合 A 的 m02/m03 "
                 "也杀这两条。"),
    },
    {
        "plan": "装配次序：normalize() → NewRootGuard()（stat 根目录、不建目录）→ AcquireLock()；失败不得创建根目录",
        "mutations": ["m07_先建根目录再取锁"],
        "checks": [],
        "note": "杀手 = TestNewRunnerNeverCreatesRoot（它断言缺失的根目录事后仍然不存在）。",
    },
    {
        "plan": "装配失败必须把**已取得的**资源释放干净（构造封块器失败 ⇒ 释放锁）",
        "mutations": ["m09_装配失败不释放锁"],
        "checks": [],
        "note": "杀手 = TestNewRunnerReleasesLockWhenSealerFails（本轮按 E62 新增的在册行为用例）。",
    },
    {
        "plan": "Runner.Close() 必须释放锁（进程退出前必须调用）",
        "mutations": ["m08_Close不释放锁"],
        "checks": ["main必须释放锁"],
        "note": "杀手 = TestNewRunnerReleasesLockOnClose（Close 后同一 root 必须能再起一个）；`cmd/trace-sealer/main.go` 的接线另有静态检查。",
    },
    {
        "plan": "Runner.Close() 幂等（defer + 显式两条退出路径）",
        "mutations": [],
        "checks": ["Close幂等用例在册"],
        "note": ("在册行为用例 = TestNewRunnerCloseIsIdempotent；下层的 `Lock.Release` 幂等由 "
                 "TestLockReleaseIsIdempotent 承载（被集合 A 的 m05 断言红杀死）。runner.go 侧"
                 "无单点变异可杀 ⇒ 按 E64② 登记进 COVERAGE_GAPS。"),
    },
    {
        "plan": "互斥只能有一处实现：`runner.go` 里不得再补一层家酿锁",
        "mutations": [],
        "checks": ["runner不得有第二处锁实现"],
        "note": "不存在性要求 ⇒ 静态检查（与集合 A 的同名检查互补：那边守 lock.go 的机制，这边守 runner.go 不重复实现）。",
    },
    {
        "plan": "A7：一个轮次内对同一 part 只能有一次 Seal 调用，且同一时刻只能有一块在封（冻结包要求同一三元组串行）",
        "mutations": [],
        "checks": ["A7轮内不重复封块用例在册"],
        "note": ("在册行为用例 = TestA7NoPartIsSealedTwiceConcurrently（名字清单里有它）。"
                 "**没有变异杀手**：TARGET（runner.go）的 m06–m09 都不改变 Round 的串行性，"
                 "而改 Round 的变异属于 p4-task-1-round / p4-task-3-loop 的职责（那两份集合的"
                 "清单里没有本用例）⇒ 按 E64①/E64② 如实登记进 COVERAGE_GAPS，可失败性由"
                 "人工探针承载。"),
    },
]

# ---------------------------------------------------------------------------
# 零变异覆盖的**用例**登记（E64②；**只是给人看的登记，工具读不到、不参与任何判定**）
# 字段口径与 p4-task-4-lock.py / p4-task-3-loop.py 一致：位置 / 断言的什么 / 为什么
# 没有杀手 / 可失败性由谁承载 / 谁来补。
# ---------------------------------------------------------------------------
COVERAGE_GAPS = [
    {
        "id": "gap-1-TestNewRunnerCloseIsIdempotent-无变异杀手",
        "title": ("TestNewRunnerCloseIsIdempotent（tests/tracesealer/lock_test.go）在册、承重，"
                  "但本集合 m06–m09 没有一条能杀它（m08 是「Close 不释放锁」，对**第二次** "
                  "Close 的错误无影响）"),
        "where": [
            "tests/tracesealer/lock_test.go  TestNewRunnerCloseIsIdempotent",
            "断言：两次 `r.Close()` 都返回 nil（第二次必须无害）",
        ],
        "reachable": "**可达且确定**：纯替身驱动（stubSealer），pristine 侧每次运行都执行并 PASS。",
        "no_killer": (
            "幂等由**两层**保证：`Close` 自己把 `r.lock` 置 nil，以及 `Lock.Release` 对"
            "`l.f == nil` 的幂等返回。runner.go 侧任何**单点**变异都杀不掉它 —— 去掉 "
            "`r.lock = nil` 之后第二次 Close 仍会走到 `Release`，而 `Release` 幂等返回 nil"
            "（它的幂等由集合 A 的 m05 直取并断言红杀死 TestLockReleaseIsIdempotent）。"
            "⇒ **缺口不可由本集合闭合**（要在 runner.go 里同时破坏两层，那就不是「一处"
            "守卫被删」的变异形态）。"
        ),
        "falsifiability": (
            "**下层已由变异断言红承载**：集合 A 的 m05（`Release` 不把 `l.f` 置空）让 "
            "`TestLockReleaseIsIdempotent` 在第二次 `require.NoError(t, l.Release())` 处 FAIL "
            "（去 close 一个已关闭的 fd）⇒ 「幂等」这条性质整体不是恒真的空断言。"
            "本轮**未**为 `Close` 的第二次调用单独造人工探针（造出来的也不是被测实现的"
            "真实失效形态）—— 如实登记。"
        ),
        "owner": "本轮只登记。若将来要让本条升格，需要给 `Runner` 加一个可注入的锁替身（当前 `lock` 是具体类型 ⇒ 不可注入），本轮明确不做。",
    },
    {
        "id": "gap-2-TestA7NoPartIsSealedTwiceConcurrently-无变异杀手",
        "title": ("TestA7NoPartIsSealedTwiceConcurrently（tests/tracesealer/concurrent_test.go）"
                  "在册、承重，但本集合 m06–m09 没有一条能杀它（它们都不改变 Round 的串行编排）"),
        "where": [
            "tests/tracesealer/concurrent_test.go  TestA7NoPartIsSealedTwiceConcurrently",
            "断言一：每个 part 在一个轮次内只被 Seal 一次（perPart[part] == 1）",
            "断言二：同一时刻最多一块在封（maxInFlight == 1）",
        ],
        "reachable": "**可达且确定**：替身 sealer 记录 perPart 与 inFlight，无真实并发/无 zstd。",
        "no_killer": (
            "它的被测对象是**冻结包对同一 (dt,inst,part) 串行的要求**与 `Round` 的串行编排。"
            "本集合 TARGET 是 runner.go，但 m06–m09 只动 `NewRunner` 的锁接线与 `Close` ⇒ "
            "不动 `Round`；而「把 Round 的逐块循环改成并行」属于 p4-task-1-round / "
            "p4-task-3-loop 的变异范围（它们的名字清单里**没有**本用例，`-run` 不会执行它）"
            "⇒ 本集合没有杀手。"
        ),
        "falsifiability": (
            "**两半各自人工探针，均可复现、均按 `cp` 还原**（被改文件 internal/tracesealer/"
            "runner.go 与 tests/tracesealer/concurrent_test.go @ `0864c519` 还原后 sha256 分别 = "
            "dd80de132255fafb… / 6ec687db153fbdeb… 与提交逐字节一致，`git status --porcelain` 为空）："
            "① 在 `Round` 的循环里把 `err := r.sealer.Seal(sealCtx, task)` 之后**再重复调用一次** "
            "⇒ 该用例在 concurrent_test.go:277 的 `require.Equal(t, 1, n, ...)` 处 FAIL"
            "（perPart=2：「part 0 在一个轮次内被封了 2 次」）—— 断言一非恒真；"
            "② 在用例里把单次 `r.Round(...)` 换成两个 goroutine 并发调用 ⇒ 该用例在 "
            "concurrent_test.go:281 的 `require.Equal(t, 1, f.maxInFlight, ...)` 处 FAIL"
            "（Not equal: expected 1）—— 断言二非恒真。"
        ),
        "owner": (
            "本轮只登记、不加变异（发起者口径：登记落到工件内即可）。升格路径：在 "
            "p4-task-1-round / p4-task-3-loop 的名字清单里纳入本用例并加一条「Round 逐块"
            "并行」的变异 —— 本轮明确不做（会改动那两份集合的绑定面，且与 Task 4 的边界"
            "不符）。"
        ),
    },
]
