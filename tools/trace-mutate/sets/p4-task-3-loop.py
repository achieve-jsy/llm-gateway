"""计划 4 / 任务 3 的变异集合 B：常驻循环（`internal/tracesealer/runner.go` 的 `Loop`）。

被变异文件 `internal/tracesealer/runner.go`。主题：**常驻循环的四条承重性质**
（启动即跑第一轮 / 周期到点继续跑 / 环境级失败不退出 / 轮次严格串行）**加上**
护栏失败必须在发现之前拦下、ctx 取消后不得再开新块。它们全部由
`tests/tracesealer/loop_test.go` 的顶层用例一一对应（每个承重属性一个顶层用例，
避免表驱动遮蔽）。

编号一经发布不再改动；匹配一律用字符串（不用行号）。

## 与计划文本的一致性（如实登记：三处**加强**、一处**改写**、一处**零覆盖登记**，零处削弱）

1. **新增 m06**（跑完一轮就退出，不再等下一个周期）。计划给的 m01–m05 里没有一条能让
   `TestLoopRepeatsRoundsOnPollInterval` 转红（m01 只是「先等再跑」，5ms 周期照样跑很多轮）
   ⇒ 「循环必须继续转」这条承重性质在册没有杀手。⇒ 加一条直取它的变异（E63⑥）。
2. **新增 m07**（每轮用 goroutine 启动 ⇒ 轮次并行）。同上，
   `TestLoopNeverOverlapsRounds` 在 m01–m05 下全绿，而「轮次严格串行」是冻结包对同一
   (dt,inst,part) 的硬要求（两块共用同一个 .tmp）⇒ 加一条变异（E63⑥）。
3. **新增一条用例** `TestLoopStopsSealingAfterCancel`（在 `tests/tracesealer/loop_test.go`）。
   计划写 m04 的杀手是 `TestLoopStopsOnContextCancel`「配合 Task 1 的
   TestRoundCancellationStopsBeforeNextSeal」——后者在 `round_test.go`，**不在本集合的
   名字清单里**（本集合的清单只含 `loop_test.go` 的用例），而前者在 m04 下仍然全绿
   （取消后 Loop 照样在 select 上返回，不再有新轮次）。⇒ 让「取消后是否还会开第二块」
   在 **Loop 侧**可观察（E63⑦）。同一处**计数口径**的如实改动见下一条。
4. **轮次怎么数（如实改动）**：计划的用例体写 `f.rounds()`（把「跑了几轮」挂在
   `countingSealer` 上）。实测这**不可能成立**：根目录不可用的那一轮，Loop 根本不会调用
   `ListPendingSeals`（护栏在发现之前就拦下了）⇒ 以「封块器被问了几次」计轮，恰恰在
   需要它工作的时候恒为 0，`require.GreaterOrEqual(t, f.rounds(), 3)` 恒红。本轮把轮次
   改由**注入的身份探针**计数（`Check()` 每轮恰好调用一次 ⇒ 轮次数 = 探针次数 − 1 次
   启动基线），护栏坏没坏都成立；计数挂在 `loopHarness` 上（轮次是循环的性质，不是
   封块器的性质）。用例名与断言语义一字未改。
5. **新增一条用例** `TestLoopReportsRootUnavailable`（同文件），并据此把计划登记的
   「m05 → EXPECTED_GREEN」**消除**。计划 m05 行的原文是「无用例断言 ⇒ 如实登记，
   不假装覆盖」—— 那句话描述的是当时的测试面，不是「该字段不可断言」。`RootUnavailable`
   是「盘掉了」与「今天没活干」的分流依据（日志与看板都按它读）⇒ 它是**行为**要求；
   按 **E62**（行为要求必须由在册行为测试承载）补一条直取它的用例，本集合因此**没有任何
   允许的绿**（E63⑧）。计划全局「已知弱覆盖」清单里的同一处登记已同步订正（E63⑧）。

## 变异测不到、但有静态检查行（CHECKS）

本集合**没有**静态检查行：上表每条计划要求都有可转红的变异（含本轮新增的三条）。
`runner.go` 的「零建目录调用」由集合 A（`p4-task-3-rootguard`）与任务 1 的集合 B
（`p4-task-1-round`）各自静态检查；本集合不重复登记。

## 零变异覆盖的**用例**登记（`COVERAGE_GAPS`，E64②；**工具读不到、不参与任何判定**）

`TestLoopStopsOnContextCancel` 在册、承重，但本集合 7 条变异**没有一条**能杀它 ⇒
按「隔离探针」口径登记进下面的 `COVERAGE_GAPS`（位置 / 断言什么 / 为什么没有杀手 /
可失败性由谁承载 / 谁来补）。**为什么单列一个结构而不是写进 `EXPECTED_GREEN`**：
后者的键被工具当作**变异名**处理（`classify` 按变异名查它、`print_mapping` 列它、
计数口径还要核对「每个键都落在『允许的绿』里」）—— 把「没有变异杀手的用例」塞进去，
会让那张清单出现一个永远对不上的键（这正是 `task-5.py` / `task-6.py` 分开存放的理由）。
`REQUIREMENTS` 也不行：工具对「mutations 与 checks 都为空」的条目直接判 **GAP ⇒ 整轮失败**。
⇒ 复用**既有字段名** `COVERAGE_GAPS`（不新造字段名）。
"""

NAME = "p4-task-3-loop"
TARGET = "internal/tracesealer/runner.go"
PACKAGES = ["./tests/tracesealer/"]
MANIFEST_FILES = ["tests/tracesealer/loop_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/tracesealer/loop_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 全部替身驱动：不依赖 zstd / 真实磁盘 / 数据库，没有可 SKIP 的理由
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# m01 先等一个周期再跑第一轮 ⇒ 「根目录配错了」要等 5 分钟才被说出来，积压也白等一轮
#     杀手：TestLoopRunsFirstRoundImmediately（poll=1h：只有「立即跑」才观察得到第一轮）
MUTATIONS["m01_先等再跑"] = [
    ("sub", "\t\tif _, err := r.Round(ctx); err != nil {",
            "\t\tselect {\n\t\tcase <-ctx.Done():\n\t\t\treturn nil\n\t\tcase <-ticker.C:\n\t\t}\n"
            "\t\tif _, err := r.Round(ctx); err != nil {"),
]
# m02 **删掉「失败后继续」这道判定**：环境级失败（根目录不可用）不再「记一条日志、
#     继续等下一个周期」，而是直接返回错误 ⇒ 进程退出、被 systemd 按 RestartSec 反复
#     拉起，而重启不会把挂载变回来；告警面（每轮一条的心跳）也随之消失。
#     杀手：TestLoopKeepsRunningWhenRootIsUnavailable
#     【E64/N2 改写】旧形态是在 logger 调用**之前插入** `return err`，于是 logger 那一行
#     变成**不可达语句** —— Go 编译器放行、`go test` 的默认 vet 子集（不含 `unreachable`）
#     也放行，属侥幸写法（一旦 CI 或本地跑全量 vet 就会红）。现形态把「记日志后继续」
#     这**一条语句**整条替换成 `return err`：语义完全不变（仍是「环境级失败 ⇒ 退出，
#     而不是继续下一个周期」），且不留任何不可达代码。匹配串是被测实现里那一处
#     两行的 logger 调用（`runner.go` 的 `Loop`），唯一且逐字可解释。
MUTATIONS["m02_环境失败退出"] = [
    ("sub", '\t\t\tr.logger.Error("trace-sealer 本轮失败，下一个周期重试",\n'
            '\t\t\t\t"err", err, "poll_interval", r.opts.PollInterval.String())',
            '\t\t\treturn err'),
]
# m03 护栏失败分支不再 return ⇒ 继续 ListPendingSeals 并 Seal：挂载没了却照样封块，
#     正文被写进宿主根 FS 上那个「长得像挂载点」的目录里
#     杀手：TestLoopKeepsRunningWhenRootIsUnavailable（sealCalls 必须为 0）
MUTATIONS["m03_护栏失败继续封块"] = [
    ("sub", "\t\tr.logRootUnavailable(err)\n\t\treturn res, err\n\t}",
            "\t\tr.logRootUnavailable(err)\n\t}"),
]
# m04 Round 里的 ctx 取消检查删掉 ⇒ 关机时仍会开新块（Task 1 的杀手在 round_test.go，
#     本集合的杀手是本轮新增的 Loop 侧用例）
#     杀手：TestLoopStopsSealingAfterCancel
MUTATIONS["m04_取消后仍开新块"] = [
    ("sub", "\t\tif err := ctx.Err(); err != nil {", "\t\tif false {"),
]
# m05 Round 去掉 res.RootUnavailable 标记 ⇒ 「本轮因根目录不可用而完全没封」这件事在
#     汇总里读不出来（与「本轮没有可封块」混成同一个零值），看板把盘掉了读成正常空轮次
#     杀手：TestLoopReportsRootUnavailable（本轮新增：计划原把这行登记进 EXPECTED_GREEN，
#     理由是「无用例断言」；按 E62 补了那条用例后它有了在册杀手 ⇒ 不再登记为允许的绿）
MUTATIONS["m05_不标记根不可用"] = [
    ("sub", "\t\tres.RootUnavailable = true\n", ""),
]
# m06 【本轮新增】跑完一轮就退出 ⇒ 常驻循环退化成「启动跑一次」，积压只能等下一次重启
#     杀手：TestLoopRepeatsRoundsOnPollInterval
MUTATIONS["m06_跑一轮就退出"] = [
    ("sub", "\t\tselect {\n\t\tcase <-ctx.Done():\n\t\t\treturn nil\n\t\tcase <-ticker.C:\n\t\t}\n\t}",
            "\t\treturn nil\n\t}"),
]
# m07 【本轮新增】每轮用 goroutine 启动 ⇒ 轮次并行：并发的两个 Round 会对同一
#     (dt,inst,part) 同时 Seal，而冻结包要求同一个三元组串行（共用同一个 .tmp）
#     杀手：TestLoopNeverOverlapsRounds
MUTATIONS["m07_轮次并行"] = [
    ("sub",
     "\tfor {\n\t\tif _, err := r.Round(ctx); err != nil {\n\t\t\tif ctx.Err() != nil {\n"
     "\t\t\t\t// 收到 SIGTERM/SIGINT：干净退出，封到一半的那块由 ctx 取消路径处理。\n"
     "\t\t\t\treturn nil\n\t\t\t}\n"
     '\t\t\tr.logger.Error("trace-sealer 本轮失败，下一个周期重试",\n'
     '\t\t\t\t"err", err, "poll_interval", r.opts.PollInterval.String())\n\t\t}\n\t\tselect {',
     "\tfor {\n\t\tgo func() {\n\t\t\tif _, err := r.Round(ctx); err != nil {\n"
     "\t\t\t\tif ctx.Err() != nil {\n"
     "\t\t\t\t\t// 收到 SIGTERM/SIGINT：干净退出，封到一半的那块由 ctx 取消路径处理。\n"
     "\t\t\t\t\treturn\n\t\t\t\t}\n"
     '\t\t\t\tr.logger.Error("trace-sealer 本轮失败，下一个周期重试",\n'
     '\t\t\t\t\t"err", err, "poll_interval", r.opts.PollInterval.String())\n'
     "\t\t\t}\n\t\t}()\n\t\tselect {"),
]

CHECKS = []

# 本集合**没有任何**允许的绿：计划原本为 m05 登记的理由是「RoundResult.RootUnavailable
# 无用例断言」；本轮按 E62 补了 TestLoopReportsRootUnavailable（断言错误是身份不符 +
# 该标记为真 + 不 Seal）⇒ m05 有了在册杀手，登记随之取消（E63⑧）。
EXPECTED_GREEN = {}

# ---------------------------------------------------------------------------
# 零变异覆盖的**用例**登记（E64②；**只是给人看的登记，工具读不到、不参与任何判定**）
#
# 为什么单列一个结构、而不是写进 EXPECTED_GREEN / REQUIREMENTS（复用既有字段名
# COVERAGE_GAPS，见 tools/trace-mutate/sets/task-5.py 与 task-6.py 的 PENDING）：
#   - EXPECTED_GREEN 的键被工具当作**变异名**处理（classify 按变异名查它、
#     print_mapping 列它、计数口径核对「每个键都落在允许的绿里」）⇒ 塞进一个
#     「没有变异杀手的用例名」会让那张清单出现永远对不上的键。
#   - REQUIREMENTS 对「mutations 与 checks 都为空」的条目直接判 GAP ⇒ 整轮失败。
# 登记口径（沿用 task-5.py 的 COVERAGE_GAPS）：位置 / 断言的什么 / 为什么没有杀手 /
# 可失败性由谁承载 / 谁来补。E64② 的要求就是：**「无变异杀手」必须落在集合工件内，
# 不能只写在计划文本里**（本条自计划 E63 的「如实登记（隔离探针）」搬进集合）。
# ---------------------------------------------------------------------------
COVERAGE_GAPS = [
    {
        "id": "gap-1-TestLoopStopsOnContextCancel-无变异杀手",
        "title": ("TestLoopStopsOnContextCancel（tests/tracesealer/loop_test.go:165-175）"
                  "在册、承重，但本集合的 7 条变异**没有一条**能杀它"),
        "where": [
            "tests/tracesealer/loop_test.go:166-175  TestLoopStopsOnContextCancel",
            "断言一：取消后 Loop **干净返回**（loop_test.go:171 的 require.NoError）",
            "断言二：返回后**不再**有新轮次（loop_test.go:172-174：两次 r.rounds() 相等）",
        ],
        "reachable": (
            "**可达**：它是「关机路径」在 Loop 侧的在册行为证据（SIGTERM → ctx 取消）。"
            "但**本集合 7 条变异里没有一条能同时改变它的两半**："
            "m04（取消后仍开新块）杀的是 TestLoopStopsSealingAfterCancel —— 它钉住「取消后"
            "不再**开始**封下一块」，与「取消后循环还在不在转、返回是否干净」是两件事；"
            "m07（轮次并行）泄漏出的 goroutine **不保证**在 50ms 采样窗内留下痕迹。"
        ),
        "no_killer": (
            "**按「隔离探针」口径登记（E64②）：该用例在册、无变异杀手，可失败性由人工实测"
            "承载。** 注意本用例实际走的退出路径是 `if ctx.Err() != nil { … return nil }`"
            "这一条（`Round` 在取消后立刻返回 ctx 错误），**不是** `select` 里的 "
            "`case <-ctx.Done():` 那条 —— 只改后者不会让它转红（前者遮蔽后者，实测 PASS），"
            "因此下面的探针都打在**前者**上。"
        ),
        "falsifiability": (
            "**两半各自人工实测，均可复现、均按 `cp` 还原**（被改文件 "
            "internal/tracesealer/runner.go 还原后 sha256 = "
            "d9f6630b475aab00e3020459f96aaf289795e342b5a0c59e39efbe87dbd4c5e5，与提交逐字节一致）："
            "① 把退出分支的 `return nil` 改成 `return ctx.Err()` ⇒ 该用例在 "
            "loop_test.go:171 `require.NoError` 处 FAIL（`Received unexpected error: context canceled`）"
            "—— 断言一非恒真；"
            "② 在同一分支注入 `go func() { for { r.Round(ctx) } }()` ⇒ 该用例在 "
            "loop_test.go:174 `require.Equal(t, n, r.rounds())` 处 FAIL"
            "（连跑 3 次：expected 11 / actual 25341、25816、25735）—— 断言二非恒真。"
        ),
        "owner": (
            "本轮（E64②）**只登记、不新增变异**（发起者口径：登记落到工件内即可）。"
            "后续若要让本条升格为断言红，需要一条直取「Loop 的取消返回语义」的变异，"
            "例如把退出分支的 `return nil` 改为 `return ctx.Err()`（见上面 ① 的实测形态）。"
        ),
    },
]

REQUIREMENTS = [
    {
        "plan": "启动即跑第一轮（不是先等一个 poll 周期）",
        "mutations": ["m01_先等再跑"],
        "checks": [],
        "note": "杀手 = TestLoopRunsFirstRoundImmediately（poll=1h）。",
    },
    {
        "plan": "周期到点必须继续跑（常驻，而不是跑一轮就退出）",
        "mutations": ["m06_跑一轮就退出"],
        "checks": [],
        "note": "杀手 = TestLoopRepeatsRoundsOnPollInterval；m06 是本轮按 E63⑥ 新增的变异。",
    },
    {
        "plan": "环境级失败（根目录不可用）不得退出：继续跑轮次，且**不得**调用 Seal",
        "mutations": ["m02_环境失败退出", "m03_护栏失败继续封块", "m05_不标记根不可用"],
        "checks": [],
        "note": ("杀手 = TestLoopKeepsRunningWhenRootIsUnavailable（m02 ⇒ Loop 直接返回错误；"
                 "m03 ⇒ sealCalls > 0）；m05 的杀手 = TestLoopReportsRootUnavailable"
                 "（本轮按 E62/E63⑧ 新增，故不再登记为允许的绿）。"),
    },
    {
        "plan": "ctx 取消后不得再开新块（关机次序：少封一块无害，封到一半被杀会留下 .tmp）",
        "mutations": ["m04_取消后仍开新块"],
        "checks": [],
        "note": ("杀手 = TestLoopStopsSealingAfterCancel（本轮按 E63⑦ 新增的 Loop 侧用例）；"
                 "Round 侧的同名杀手 TestRoundCancellationStopsBeforeNextSeal 在 "
                 "p4-task-1-round 的名字清单里（本集合的 -run 清单不含它）。"
                 "**另如实登记（E64②）**：本要求相邻的另一条关机行为 —— 在册用例 "
                 "TestLoopStopsOnContextCancel（断言「取消后 Loop 干净返回」+「返回后不再有"
                 "新轮次」）**没有变异杀手**，按隔离探针口径登记在本集合的 COVERAGE_GAPS"
                 "（工具读不到该字段，故此处复述其实测结论）：① inner 退出分支的 `return nil` "
                 "改成 `return ctx.Err()` ⇒ 该用例在 loop_test.go:171 `require.NoError` 处 FAIL"
                 "（Received unexpected error: context canceled）；② 同一分支注入 "
                 "`go func() { for { r.Round(ctx) } }()` ⇒ 该用例在 loop_test.go:174 "
                 "`require.Equal(n, r.rounds())` 处 FAIL（expected 11 / actual 25341，连跑 3 次"
                 "分别为 25341 / 25816 / 25735）。两次都按 `cp` 还原，"
                 "internal/tracesealer/runner.go 还原后 sha256 = "
                 "d9f6630b475aab00e3020459f96aaf289795e342b5a0c59e39efbe87dbd4c5e5（与提交逐字节一致）。"),
    },
    {
        "plan": "轮次必须**严格串行**（同一时刻只有一轮在跑）",
        "mutations": ["m07_轮次并行"],
        "checks": [],
        "note": "杀手 = TestLoopNeverOverlapsRounds；m07 是本轮按 E63⑥ 新增的变异。",
    },
    {
        "plan": "根目录护栏必须在本轮「发现」之前拦下（失败 ⇒ 不 ListPendingSeals、不 Seal）",
        "mutations": ["m03_护栏失败继续封块"],
        "checks": [],
        "note": "杀手 = TestLoopKeepsRunningWhenRootIsUnavailable（sealCalls == 0）。",
    },
]
