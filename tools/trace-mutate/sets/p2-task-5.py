r"""计划 2 / 任务 5 的变异集合（作者编写的产物 ⇒ 必须 git-tracked，见全局约束 §证据制度 1）。

被变异文件是周期 flush 与滚动（`internal/gateway/observability/spooler.go`）。
这一批变异的共同主题：**「挂载消失/盘满时必须响亮地停止写盘」「周期 flush 必须无条件执行」
「滚动必须先落盘、且 Tick 必须在写协程上」这三组守卫都必须能被测出来。**

编号一经发布不再改动；匹配一律用字符串（不用行号），行号仅供人读。

## 相对简报（task-5-brief.md）的如实改动

1. **m01 的杀手用例必须新增**：简报没有给 m01 指定杀手（只在注释里说
   TestSpoolerRefusesMissingRoot 必须红）。实测：**它杀不死 m01** —— 根目录不存在时
   A5-1 的身份基线同样取不到（`defaultRootIdentity` 要先 stat），两条检查互相遮蔽，
   摘掉存在性检查后构造**仍然**失败 ⇒ 简报的 m01 在本实现上是「绿（零覆盖）」。
   ⇒ 新增隔离探针用例
   `TestSpoolerRefusesMissingRootEvenWhenIdentityIsObtainable`（注入恒成功的
   RootIdentity），使「启动期存在性检查」成为唯一的失败来源。

2. **m06/m07 的杀手用例名在简报里不存在**：简报写的是
   `TestSpoolerLatchedAppendDoesNotTouchDisk`，而简报 11 条用例里没有这个名字
   （"11 个顶层用例" 的清单里没有它）。按控制者硬性指令第 2 条（杀手必须是具体顶层
   测试函数名），本集合改为：
   - m06（闩锁时仍然尝试写入）⇒ `TestSpoolerLatchesWhenRootDisappears`
     （闩锁后 AppendLine 必须返回 ErrSpoolLatched）；
   - m07（写失败不闩锁）⇒ `TestSpoolerLatchesOnAppendFailureWhileRootHealthy`（**新增**）。

3. **m07 在简报的用例集上零覆盖 ⇒ 新增用例**：简报的 m07 注释说
   TestSpoolerChecksRootAfterAppendFailure 会红。实测：该用例里 `gone = true`，
   于是 `AppendLine` 失败后先走 `checkRoot()` 并**提前 return**（root_unavailable 已经
   闩锁），永远到不了 `append_failed` 那一支 ⇒ 摘掉那条 Trip 照样绿。
   ⇒ 新增 `TestSpoolerLatchesOnAppendFailureWhileRootHealthy`（根目录保持健康、
   只让建块失败），使 append_failed 有独立观察者。

4. **m04 的变异串按实测修正**：简报给的 `if !rootOK {\n\t\treturn nil\n\t}` →
   `if false {...}` 会让 `rootOK` 变成「声明未使用」⇒ **编译失败红**（一条用例都不
   执行，证明不了守卫承重）。改为 `if !rootOK && false {`，保住对 rootOK 的引用
   ⇒ 断言红，杀手 TestSpoolerDoesNotRollWhileRootUnavailable。

5. **简报 m01..m09 之外补 m10..m19**（依据：增补条目 A5-1/A5-4 与控制者
   「每条断言都必须能红」的要求）：
   - A5-1 是本任务最承重的验收项之一（D49/D55 的归属）：冻结包建块时的建目录调用
     会在容器可写层把 root 重建出来 ⇒ 存在性、IsDir、**写探针**全部成功。
     只验存在/可写的实现**不是** A5-1。⇒ m10（不比对身份）、m11（取不到身份也不闩锁）、
     m12（解锁探针不先验身份）、m13（构造期取不到基线仍启动）、m19（两类闩锁原因不分流）；
   - A5-4（Tick 持锁做探针 fsync，会堵住热路径 Allow）：⇒ m14（Run 的 goroutine 上直接
     Tick）、m18（到期标志不再恰好消费一次）；
   - §9.3 计数器与周期默认值原先零覆盖 ⇒ m15（周期默认值失效）、m16（Flushes 不计）、
     m17（Rolls 不计）；
   - A5-1 的**身份取值本身**也要有观察者 ⇒ m20（挂载级身份不再选最长前缀）、
     m21（身份取值不稳定 ⇒ 第一次复检就永久闩锁）。

6. **用例分两层**：A5-4 的两条守卫里，「Run 不得在自己的 goroutine 上探针」可在
   B 层观测（`TestSpoolerRunDefersProbeToWriteGoroutine`）；「到期标志恰消费一次」必须
   确定性地置一次标志 ⇒ 用未导出方法 `markProbeDue`，放包内
   （`internal/gateway/observability/spooler_internal_test.go`，理由写在文件头）。
   周期的默认值（未导出字段 `flushInterval`）同理放包内。因此本集合的
   `PACKAGES`/`MANIFEST_FILES` 有两个包/两个测试文件。

## 变异测不到、但有静态检查行（CHECKS）

- 「Run 不得以任何形式调用 gate.Tick」（A5-4）额外用静态行钉住 Run 的 ticker 分支
  形态；它是**结构辅助信号**，承重证据是 m14 + 行为用例。
- 「网关侧不得创建目录」（D49）：行为用例无法表达「本文件里没有建目录调用」
  （删除一个本就不存在的调用不改变任何行为）⇒ 只能静态钉住源码形态。
  注意：该正则也会命中**注释**里「标识符紧跟 ASCII 左括号」的提及 ⇒ 它反向约束了
  本任务自己的注释写法（spooler.go 的注释一律不写该形态，已登记在报告里）。
- 「空块不滚动的守卫来自计划 1 冻结的 PendingSeals」：本计划不得修改该文件，
  故用静态行固定它（拆成承重的 `cur == nil` 与防御性的 `rows == 0` 两行，见修复轮 M1）。
"""

# ---------------------------------------------------------------------------
# 合并前广度评审修复轮（必修 1：关停竞态）之后的改动
#
# 新增 1 条变异（m29）与 1 条静态检查行（main.go 的关停次序）。
#
# - **必修 1（重要）关停竞态**：`cancel()` 之后 spooler 的 Run 与写入侧的 Run 各自
#   select、**无同步**。原实现（`case <-ctx.Done(): _ = s.Close()`，来自计划草稿
#   plan:2481 骨架）会在写入侧补写最后一批（≤flushN=50 条 / ≤2s）之前关掉 spool ⇒
#   该批全部 body_missing=true + 一条 ERROR 级「正文落盘已闩锁」= **健康盘上的假
#   磁盘降级告警**，每次优雅重启都可能发生（违反计划 plan:66「main.go 关闭时保持
#   既有行为」）。修复：Run 的 ctx.Done 只 `return`（spool 的生命周期归调用方），
#   main 在 `cancel()` 后先等 `(*TraceWriter).Done()`（外层 6s 上限防死锁）再 `Close()`。
#   ⇒ m29 恢复「Run 自行 Close」的形态；杀手 =
#   TestSpoolerRunDoesNotCloseOnContextCancel（Run 退出后 spool 仍可写）与
#   TestShutdownFlushIsNotLatchedBySpoolerRun（端到端 PG 行不得 body_missing）。
#   main.go 的语句次序没有用例覆盖 ⇒ 用静态检查行钉住「先等 Done、再 Close」。
# - 语义不得变坏：真实闩锁/真实故障时最后一批仍必须降级（body_missing）—— 修复只消除
#   「健康盘被误判」这一段。m27（Close 后仍允许写入）与 p2-task-8-spooler 的 m01
#   （闩锁期间仍写盘）仍在守这条边界；m29 本身的杀手断言也依赖「真的拒绝写入」。
#
# ---------------------------------------------------------------------------
# 评审修复轮（task-5 评审：I1/I2/I3/M1/M3/M5/M6）之后的改动
#
# 新增 7 条变异（m22–m28），并修正两条已有变异的定位串（m07/m20 —— 实现形态变了，
# 原串不再唯一/合法）。全部改动：
#
# - **I1（重要）§9.3 闩锁日志**：三处 `Trip` 的返回值原先被丢弃 ⇒ 跳变那一刻**一条日志
#   都没有**（Run 只在 `FlushAndRoll` 返错时打日志，而根目录不可用那一轮返回 nil）。
#   改为 `if s.gate.Trip(...) { log.Printf("... ERROR ...") }` ⇒ 跳变恰好一条、
#   闩锁期间不刷屏（Trip 的契约把「打日志」交给调用方）。m25（每次 tick 都打）与
#   m26（去掉 ERROR 标记）分别钉住「不刷屏」与「级别可见」；
#   杀手 = TestSpoolerLatchLogsExactlyOncePerTransition。
#   【归属】闩锁**期间**的周期汇总日志判给 Task 7（§9.3 的降级可观测性），
#   本任务只负责跳变那一刻这一条。
# - **I2（重要）A5-1 第 3 点在注入 Gate 时失效**（两条都做）：
#   ① 新增 `TraceSpoolerOptions.DiskWatermarkPercent`（0 ⇒ 90）传给**内置** Gate ⇒
#      Task 6 不必为「尊重配置水位」而注入 Gate（m23 忽略选项、m24 去掉 0⇒90 默认值；
#      杀手 = TestSpoolerDiskWatermarkOptionReachesBuiltGate）；
#   ② 身份包装**无条件**套在 `Gate.ProbeWrite` 外层（`guardProbe`，放在两个分支之后）⇒
#      注入的 Gate 的探针也先验身份（m22；
#      杀手 = TestSpoolerInjectedGateProbeStillRequiresIdentity）。
# - **I3（裁定，无需改行为）A5-4 条件④的顺序**：先消费到期标志 → 再 `Allow()`。
#   控制者已正式批准；实现注释（AppendLine）与报告都写明这是**已批准的偏离**，
#   防止 Task 6 照字面重写而把「超水位放弃正文」变成死代码。
# - **M1（轻微）静态检查归因不实**：原 CHECKS 把「空块不滚动」归给 `rows == 0` 守卫，
#   而冻结包自己写明该守卫经公开 API **不可达**（writer.go:369-376）；真正承重的是
#   `cur == nil`（writer.go:358-360：没有 Append 就没有块）。现拆成两行 ——
#   一行钉 `cur == nil`（承重），一行钉 `rows == 0`（防御性守卫；简报点名要钉，
#   但明确声明它**不是**本用例的证据）。
# - **M3（轻微）Close 后可静默重建 part**：冻结包的 `Writer.Close` 只是关句柄、没有
#   closed 标志 ⇒ 之后 AppendLine 会被当成「正常的新块」重新建 part。加
#   `closed atomic.Bool`（Close 用 CAS 幂等，AppendLine 在 Close 后返回 ErrSpoolLatched）
#   ⇒ m27；杀手 = TestSpoolerAppendAfterCloseDoesNotRecreatePart。
# - **M5（轻微）两条用例押在「宿主真实盘用量 < 90%」上** ⇒ 盘用量高的机器假红：
#   TestSpoolerDoesNotRollWhileRootUnavailable 与
#   TestSpoolerProbeRefusesToUnlatchWhenIdentityMismatched 改为注入水位恒 10% 的 Gate
#   （测试内的 lowUsageGate）。这是可移植性修复，不产生新变异（它本身不是守卫）。
# - **M6（轻微）并列挂载点「先到者胜」⇒ 身份随行序漂移**：`mountKey.beats` 提供确定性
#   tie-break（dev、再 fstype，字典序最大），使身份与 mountinfo 行序无关 ——
#   「行序变化 ⇒ 身份变化 ⇒ 永久闩锁」与 m21（不稳定身份）是同一类失效。
#   ⇒ m28；杀手 = TestMountIdentityForPicksLongestPrefixAndSurvivesEscape 的两种行序断言。
#
# 【评审「登记不修」的三条，不在本集合内（报告里登记）】
#   1. `MarkSealed` 无返回值 ⇒ close/Flush 错误只能丢弃：`FlushAndRoll` 在
#      `PendingSeals` 与 `MarkSealed` 之间释放 `w.mu`，该窗口内写协程缓冲进同一 part 的
#      行若遇 close-flush 失败（ENOSPC）会被丢弃，而那一行的 `Ref` 已作为成功返回
#      （点读表现为「行号越界」，不是响亮缺失）。冻结契约无返回值 ⇒ 超出本任务可修范围。
#   2. 解锁探针在 Gate 锁内做 4 类文件操作（CreateTemp/write/fsync/unlink）⇒
#      Task 4 锁设计的延续。
#   3. 构造期只查 `Clean(root) == "/"`、不查 `filepath.IsAbs`（与全局约束一致，
#      配置层已校验）；但提交信息里写了「绝对路径」⇒ 提交信息与代码不符（历史不改，报告登记）。

NAME = "p2-task-5"
TARGET = "internal/gateway/observability/spooler.go"
# 两个包：B 层（仅公开 API）+ 包内（两处必须触达未导出符号，见文件头第 6 条）
PACKAGES = ["./tests/gateway/observability/", "./internal/gateway/observability/"]
MANIFEST_FILES = [
    "tests/gateway/observability/spooler_test.go",
    "internal/gateway/observability/spooler_internal_test.go",
]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/gateway/observability/spooler_test.go "
    "internal/gateway/observability/spooler_internal_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 本集合的用例不依赖数据库/外部二进制/真实磁盘/真实挂载/时区数据，没有可 SKIP 的理由。
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# ---- ① 启动期的根目录存在性检查（只覆盖「启动时未挂载」，见 D49）----
# 杀手 = TestSpoolerRefusesMissingRootEvenWhenIdentityIsObtainable（**本任务新增**：
# 简报的 TestSpoolerRefusesMissingRoot 杀不死本条 —— 身份基线在缺失路径上同样失败，
# 两条检查互相遮蔽）。摘掉存在性检查后构造成功 ⇒ require.Error 转红。
MUTATIONS["m01_删除启动期根目录存在性检查"] = [
    ("sub", "if err := rootExists(opts.Root); err != nil {",
            "if err := rootExists(opts.Root); err != nil && false {"),
]
# ---- ② root 不得是文件系统根（第二道防线，第一道在 config.LoadTraceBodyConfig）----
# 杀手 = TestSpoolerRefusesFilesystemRoot："/"、"//"、"/./" 都 Clean 成 "/"，
# 放行后构造成功 ⇒ require.Error 转红。
MUTATIONS["m02_允许文件系统根"] = [
    ("sub", 'if filepath.Clean(opts.Root) == "/" {', "if false {"),
]
# ---- ③ 周期 tick 与滚动前必须复检根目录（D49）----
# 杀手 = TestSpoolerLatchesWhenRootDisappears（以及两条身份用例）：
# FlushAndRoll 不再复检 ⇒ 根目录消失后不闩锁 ⇒ require.False(Allow()) 转红。
MUTATIONS["m03_滚动前不复检根目录"] = [
    ("sub", "rootOK := s.checkRoot()", "rootOK := true"),
]
# ---- ④ 根目录不可用的那一轮不得滚动（避免随后的 Append 触发容器可写层重建 root）----
# 杀手 = TestSpoolerDoesNotRollWhileRootUnavailable（可观测量 = part 序号）。
# 【相对简报的修正】简报的 `if false {` 形态会让 rootOK 声明未使用 ⇒ 编译失败红。
MUTATIONS["m04_根目录不可用时仍然滚动"] = [
    ("sub", "if !rootOK {\n\t\treturn nil\n\t}", "if !rootOK && false {\n\t\treturn nil\n\t}"),
]
# ---- ⑤ 任何 Append 失败都要复检根目录（把「盘满」与「挂载消失」分开）----
# 杀手 = TestSpoolerChecksRootAfterAppendFailure：Append 失败后不再复检 ⇒ 原因变成
# append_failed ⇒ require.Contains(Reason(), "root_unavailable") 转红。
MUTATIONS["m05_Append失败后不复检根目录"] = [
    ("sub", "if !s.checkRoot() {", "if false {"),
]
# ---- ⑥ 闩锁时不得触碰文件系统（R9）----
# 杀手 = TestSpoolerLatchesWhenRootDisappears：闩锁后 AppendLine 必须返回
# ErrSpoolLatched；摘掉 Allow 判定后那一行会被真的写进 spool ⇒ require.ErrorIs 转红。
MUTATIONS["m06_闩锁时仍然尝试写入"] = [
    ("sub", "if !s.gate.Allow() {", "if false {"),
]
# ---- ⑦ 写失败必须闩锁 ----
# 杀手 = TestSpoolerLatchesOnAppendFailureWhileRootHealthy（**本任务新增**：
# 简报指定的 TestSpoolerChecksRootAfterAppendFailure 会先被 checkRoot 闩锁并提前
# return，到不了这一支 ⇒ 零覆盖）。摘掉 Trip 后 Allow() 仍为真 ⇒ 转红。
# 【修复轮】I1 之后这一支变成 `if s.gate.Trip("append_failed", err) { log… }`，
# 因此定位串随之更新（旧串 `s.gate.Trip(...)` 仍在，但把它换成 `_ = err` 会得到
# 非法的 `if _ = err {` ⇒ 编译红，不是断言红）。
MUTATIONS["m07_写失败不闩锁"] = [
    ("sub", 'if s.gate.Trip("append_failed", err) {', "if false {"),
]
# ---- ⑧ 周期 flush 必须真的调 PendingSeals（R8/§7.1）----
# 杀手 = TestSpoolerFlushMakesBufferedLinesVisible：不再 flush ⇒ 缓冲里的两行永远
# 不落盘 ⇒ require.Equal("{\"a\":1}\n{\"a\":2}\n", ...) 转红。
MUTATIONS["m08_FlushAndRoll不再flush"] = [
    ("sub", "tasks, err := s.w.PendingSeals()", "var tasks []ta.SealTask; var err error"),
]
# ---- ⑨ 达阈值必须滚动（MarkSealed）----
# 杀手 = TestSpoolerRollsOnBlockBytes（以及 OnRollInterval / DTChange）：
# 不滚动 ⇒ 后续 Append 复用 part-000000 ⇒ require.FileExists(part-000001) 转红。
MUTATIONS["m09_不再滚动"] = [
    ("sub", "s.w.MarkSealed(task.DT, task.Part)", "_ = task"),
]

# ==== A5-1：根目录身份基线（D49/D55 的归属，本任务最承重的验收项之一）====
# 冻结包建块时的建目录调用会在**容器可写层**把 root 重建出来 ⇒ 存在性、IsDir、
# 写探针**全部成功**。所以「挂载消失」的真判据只能是身份。
# ---- ⑩ 默认检查不再比对身份 ----
# 杀手 = TestSpoolerLatchesWhenRootIdentityChanges：身份变了但不比对 ⇒ 不闩锁
# ⇒ require.False(Allow()) / require.Contains(Reason(), "root-identity-mismatch") 转红。
MUTATIONS["m10_默认检查不再比对根目录身份"] = [
    ("sub", "if cur != baseline {", "if cur != baseline && false {"),
]
# ---- ⑪ 取不到身份不算不一致（放行）----
# 杀手 = TestSpoolerLatchesWhenRootIdentityUnobtainable：注入的身份函数在**值不变**时
# 返回 error ⇒ 忽略 err 的实现看到 cur == baseline，不闩锁 ⇒ 转红。
MUTATIONS["m11_取身份失败被忽略"] = [
    ("sub", "if err != nil {\n\t\t\t// 【为什么 err 也算不一致】",
            "if err != nil && false {\n\t\t\t// 【为什么 err 也算不一致】"),
]
# ---- ⑫ 解锁探针必须先过身份检查 ----
# 杀手 = TestSpoolerProbeRefusesToUnlatchWhenIdentityMismatched：探针不验身份时，
# 真实的默认探针在 t.TempDir() 上会成功、水位也正常 ⇒ 网关解锁 ⇒ 转红
#（这正是 A5-1 要堵的静默错位：挂载缺失而容器层可写）。
MUTATIONS["m12_解锁探针不再验身份"] = [
    ("sub", "if err := identityErr(root); err != nil {",
            "if err := identityErr(root); err != nil && false {"),
]
# ---- ⑬ 构造期取不到身份基线仍启动 ----
# 杀手 = TestSpoolerRefusesRootWithoutIdentityBaseline：忽略基线错误后构造成功
# ⇒ require.Error 转红；且后续无法检测身份变化。
MUTATIONS["m13_构造期忽略身份基线失败"] = [
    ("sub", "if identErr != nil {", "if identErr != nil && false {"),
]
# ---- ⑭ 两类闩锁原因必须分流 ----
# 杀手 = TestSpoolerLatchesWhenRootIdentityChanges / ...Unobtainable：
# 身份失败被归到 root_unavailable ⇒ require.Contains(Reason(), "root-identity-mismatch") 转红
#（运维动作不同：重新挂载 vs 其它可用性问题）。
MUTATIONS["m19_身份失败不再单独报原因"] = [
    ("sub", "if errors.Is(err, errRootIdentityMismatch) {", "if false {"),
]

# ==== A5-4：Tick（持锁 + 探针 fsync）必须在写协程上 ====
# ---- ⑮ Run 在自己的 goroutine 上直接 Tick ----
# 杀手 = TestSpoolerRunDefersProbeToWriteGoroutine：Run 侧跑探针后，
# 第一次断言 `require.Equal(0, calls)` 在 50ms 内必然转红（探针持锁 fsync 会堵住 Allow）。
MUTATIONS["m14_Run在自己的goroutine上直接Tick"] = [
    ("sub", "s.markProbeDue(s.now())", "s.gate.Tick(s.now())"),
]
# ---- ⑯ 到期标志必须恰好消费一次 ----
# 杀手 = TestSpoolerDueProbeIsConsumedExactlyOnce（包内）：标志不被消掉时之后每次
# AppendLine 都重复 Tick ⇒ 水位取数次数从 1 涨到 3 ⇒ 转红。
MUTATIONS["m18_到期标志不再恰好消费一次"] = [
    ("sub", "if !s.probeDue.Swap(false) {", "if !s.probeDue.Load() {"),
]

# ==== §9.3 快照与周期默认值（原先零覆盖）====
# ---- ⑰ 周期 flush 的默认值失效 ----
# 杀手 = TestSpoolerFlushIntervalDefaultsToOneMinute（包内）：默认值失效后
# flushInterval == 0 ⇒ 断言转红（**不是**编译红/panic 红 —— 那两种都不能作为证据）。
MUTATIONS["m15_周期默认值失效"] = [
    ("sub", "if opts.FlushInterval <= 0 {\n\t\topts.FlushInterval = time.Minute\n\t}",
            "if false {\n\t\topts.FlushInterval = time.Minute\n\t}"),
]
# ---- ⑱ Flushes 计数不再累加 ----
# 杀手 = TestSpoolerFlushMakesBufferedLinesVisible：require.Equal(1, Stats().Flushes) 转红。
MUTATIONS["m16_Flushes计数不再累加"] = [
    ("sub", "s.flushes++", "_ = s.flushes"),
]
# ---- ⑲ Rolls 计数不再累加 ----
# 杀手 = TestSpoolerRollsOnBlockBytes：require.Equal(1, Stats().Rolls) 转红。
MUTATIONS["m17_Rolls计数不再累加"] = [
    ("sub", "s.rolls++", "_ = s.rolls"),
]

# ==== A5-1 的身份取值本身（判别逻辑与稳定性）====
# ---- ⑳ 挂载级身份不再选取「覆盖 root 的最长前缀挂载项」----
# 杀手 = TestMountIdentityForPicksLongestPrefixAndSurvivesEscape（包内）：
# 合成 mountinfo 里 /datalog 与 / 都覆盖 root，只有最长前缀是对的；
# 摘掉选取逻辑后解析器只认「首个覆盖项」（= /）⇒ require.Equal(挂载点 …) 转红。
# 【修复轮】M6 之后该条件多了并列 tie-break，定位串随之更新（见 m28）。
# 【为什么必须用合成内容】本机（macOS）没有 /proc/self/mountinfo，挂载级解析只在
# Linux 生产路径上生效 ⇒ 不测就是开发环境里的零覆盖。
MUTATIONS["m20_挂载身份不再选最长前缀"] = [
    ("sub", "if len(mp) > bestLen || (len(mp) == bestLen && cand.beats(best)) {",
            "if bestLen < 0 {"),
]
# ---- ㉑ 身份取值不稳定（把时间掺进身份）----
# 杀手 = TestDefaultRootIdentityIsStableForTheSameDirectory（包内）：同一目录两次取身份
# 必须相同。不稳定 ⇒ 第一次周期复检就与基线不一致 ⇒ **永久闩锁**、全部正文变 body_missing。
# 两个分支都注入（挂载级 / 设备级），使这条变异在 Linux 与 macOS 上都能被杀死。
MUTATIONS["m21_身份取值不稳定"] = [
    ("sub", 'return fmt.Sprintf("mount=%s|dev=%s|fstype=%s", key.mountPoint, key.dev, key.fsType), nil',
            'return fmt.Sprintf("mount=%s|dev=%s|fstype=%s|t=%d", key.mountPoint, key.dev, key.fsType, time.Now().UnixNano()), nil'),
    ("sub", 'return fmt.Sprintf("dev=%d|fstype=%d", uint64(st.Dev), fsType), nil',
            'return fmt.Sprintf("dev=%d|fstype=%d|t=%d", uint64(st.Dev), fsType, time.Now().UnixNano()), nil'),
]

# ==== 评审修复轮（I1/I2/M3/M6）====
# ---- I2-② 探针身份包装被条件化（= 原来那句「只对内建 Gate 包装」的直觉写法）----
# 杀手 = TestSpoolerInjectedGateProbeStillRequiresIdentity（**本任务新增**）与
# TestSpoolerProbeRefusesToUnlatchWhenIdentityMismatched（两者都注入 Gate）。
#
# 【杀伤面的如实说明】`if ProbeWrite == nil` 是「把包装挪回内建分支」最自然的写法，但
# NewSpoolGate 已把 nil 探针换成 defaultProbeWrite ⇒ 该条件**实际恒假** ⇒ 两种 Gate 的
# 探针都不再验身份。因此本变异比「只对注入 Gate 生效」更宽（它同时打断了内建路径）。
# 「注入的 Gate 也必须先验身份」这一具体形态由上面两条行为用例直接把关：
# 去掉无条件包装后，注入的成功探针 + 正常水位会让网关解锁 ⇒ 两条用例转红。
# 之所以仍需要本变异：m12 改的是「身份检查本身」，而本变异改的是「包装是否生效」，
# 修的人可能只动其中一处。
MUTATIONS["m22_探针身份包装被条件化（两种Gate的探针都不再验身份）"] = [
    ("sub", "opts.Gate.opts.ProbeWrite = guardProbe(identityErr, opts.Gate.opts.ProbeWrite)",
            "if opts.Gate.opts.ProbeWrite == nil {\n\t\topts.Gate.opts.ProbeWrite = guardProbe(identityErr, nil)\n\t}"),
]
# ---- I2-① 内置 Gate 忽略配置的水位 ----
# 杀手 = TestSpoolerDiskWatermarkOptionReachesBuiltGate（包内）：选项 42 应传到内置
# Gate；硬编码 90 后断言 42 转红（Task 6 于是只能靠注入 Gate 来「尊重配置」，而那条路
# 正是 I2-② 要补的绕路）。
MUTATIONS["m23_内置Gate忽略配置水位"] = [
    ("sub", "DiskWatermarkPercent: opts.DiskWatermarkPercent,", "DiskWatermarkPercent: 90,"),
]
# ---- I2-① 去掉「0 ⇒ 90」的默认值 ----
# 杀手 = TestSpoolerDiskWatermarkOptionReachesBuiltGate 的第二个断言。
# 【为什么它是承重的】SpoolGate **自己不为水位设默认值**，而 Tick 的未闩锁判据是
# `pct >= DiskWatermarkPercent` ⇒ 传 0 会让第一次 Tick 就闩锁（0% 水位）。
MUTATIONS["m24_去掉水位默认值"] = [
    ("sub", "if opts.DiskWatermarkPercent <= 0 {\n\t\topts.DiskWatermarkPercent = 90\n\t}",
            "if false {\n\t\topts.DiskWatermarkPercent = 90\n\t}"),
]
# ---- I1 §9.3 闩锁日志：不再按「跳变」判定，每次都打（噪音）----
# 杀手 = TestSpoolerLatchLogsExactlyOncePerTransition：跳变一条、闩锁期间不再打。
# `|| true` 保留 Trip 调用（跳变语义不变），只让日志无条件打印 ⇒ 闩锁期间的每个 tick
# 都打一条 ⇒ require.Len(lines, 1) 转红。
# 【为什么不能写成 `if true {`】那样会**连 Trip 调用一起删掉**（它就在 if 条件里）
# ⇒ 变成「根目录失败不再闩锁」这条完全不同的变异（杀伤面 8 条用例，不再是「日志噪音」），
# 归因就不实了 —— 本集合第一版正是这么写的，权威表一跑就暴露出来，故修正。
MUTATIONS["m25_闩锁日志不再按跳变判定（每次都打）"] = [
    ("sub", "if s.gate.Trip(reason, err) {", "if s.gate.Trip(reason, err) || true {"),
]
# ---- I1 §9.3 闩锁日志：去掉 ERROR 级别标记 ----
# 杀手 = TestSpoolerLatchLogsExactlyOncePerTransition 的 require.Contains(..., "ERROR")。
# 标准 log 包没有级别，本项目用消息里的 ERROR 标记表达级别 ⇒ 删掉它，日志里就再也
# 分不出「闩锁跳变」这件需要人工介入的事。这里只改**根目录**那一处（AppendLine 的
# append_failed 那一处有自己的消息，行为侧由 m07 覆盖）。
MUTATIONS["m26_根目录闩锁日志不再标ERROR"] = [
    ("sub", 'log.Printf("[trace-spool] ERROR 正文落盘闩锁（跳变）：reason=%s root=%s cause=%v"',
            'log.Printf("[trace-spool] 正文落盘闩锁（跳变）：reason=%s root=%s cause=%v"'),
]
# ---- M3 Close 之后不再拒绝写入 ----
# 杀手 = TestSpoolerAppendAfterCloseDoesNotRecreatePart：摘掉 closed 判定后，
# 冻结包的 Writer 会把这次 Append 当成「正常的新块」重建 part-000001 ⇒ 转红。
MUTATIONS["m27_Close后仍允许写入"] = [
    ("sub", "if s.closed.Load() {", "if false {"),
]
# ---- M6 并列挂载点回到「先到者胜」 ----
# 杀手 = TestMountIdentityForPicksLongestPrefixAndSurvivesEscape 的行序断言：
# 同一挂载点的两条记录换序后身份必须相同；「先到者胜」会随行序漂移 ⇒ 转红
# （「行序一变身份就变」= 与 m21 同一类的永久闩锁）。
MUTATIONS["m28_并列挂载点先到者胜"] = [
    ("sub", "if len(mp) > bestLen || (len(mp) == bestLen && cand.beats(best)) {",
            "if len(mp) > bestLen {"),
]
# ---- 合并前广度评审必修 1：关停竞态（Run 自行 Close → 健康盘假降级） ----
# 把 Run 的 ctx.Done 分支改回「自己 Close」（= 修复前的实现，也正是计划草稿
# plan:2481 骨架里的 `_ = s.Close()`）：关停时它与写入侧的排空**没有任何同步**，
# 写入侧用 WithoutCancel 补写的最后一批（≤flushN=50 条 / ≤2s）会撞上 AppendLine 的
# closed 检查 ⇒ 全部 body_missing=true，并经 noteSpoolFailure 打一条 ERROR 级
# 「正文落盘已闩锁」—— 盘是健康的，却发出磁盘降级告警，每次优雅重启都可能发生。
# 杀手：
#   - TestSpoolerRunDoesNotCloseOnContextCancel（Run 退出后 spool 必须仍可写）；
#   - TestShutdownFlushIsNotLatchedBySpoolerRun（端到端：最后一批的追加不得是
#     ErrSpoolLatched，且对应 PG 行不得 body_missing=true）。
# 两条都是**确定性**断言（前者先等 Run 返回；后者把追加押在「spooler 的 Run 已返回」
# 之后），不靠「写入侧跑得快就绿」的调度概率。
MUTATIONS["m29_Run在ctx取消时自行关闭spool"] = [
    ("sub", "\t\tcase <-ctx.Done():\n\t\t\treturn",
            "\t\tcase <-ctx.Done():\n\t\t\t_ = s.Close()\n\t\t\treturn"),
]

# ---------------------------------------------------------------------------
# 静态检查：**变异测不到**的要求必须有静态检查行（全局约束的证据制度第 4 条）。
# ---------------------------------------------------------------------------
CHECKS = [
    {
        "name": "空块不滚动：真正承重的守卫是 PendingSeals 的 `cur == nil` 提前返回（计划 1 冻结行为）",
        "file": "internal/tracearchive/writer.go",
        # 【评审 M1 的修正】原以为承重的是 `rows == 0` 守卫；实测读源码后确认：
        # 没有 Append 就没有块（cur == nil），这才是 TestSpoolerDoesNotRollEmptyBlock
        # 在空块那一轮走到的那条提前返回（writer.go:358-360）。因此这一行的 pattern
        # 钉的是它，而不是 rows == 0。
        "pattern": r"cur := w\.cur\s*\n\s*if cur == nil \{\s*\n\s*return nil, nil",
        "expect": "present",
        "why": (
            "「空块不滚动」在 Spooler 这一侧的实际机理是：那一轮从未 Append 过 ⇒ 冻结包的 "
            "Writer 根本没有当前块 ⇒ PendingSeals 的 `cur == nil` 提前返回 nil（无 SealTask）"
            "⇒ Spooler 无可滚动对象。行为侧证据 = TestSpoolerDoesNotRollEmptyBlock"
            "（断言空块那一轮后 spool 文件仍不存在、首次写入落在 part-000000）。"
            "本计划不得修改冻结包，故用静态行固定该守卫存在。"
        ),
    },
    {
        "name": "空块不滚动：`rows == 0` 防御性守卫仍在（经公开 API 不可达；简报点名要钉）",
        "file": "internal/tracearchive/writer.go",
        "pattern": r"if cur\.rows == 0 \{\s*\n\s*return nil, nil",
        "expect": "present",
        # 【评审 M1 的修正：归因必须说实话】冻结包在这条守卫上方自己写明：经公开 API
        # 它**不可达**（唯一能造出空块的路径是写失败，而写失败会先毒化该块，
        # 上面那条 poisoned 分支已提前返回，writer.go:369-376）。
        # 因此本行只钉「防御性守卫仍在」，**不是** TestSpoolerDoesNotRollEmptyBlock 的证据。
        "why": (
            "简报点名要钉这条冻结守卫，故保留本行；但归因必须说实话："
            "冻结包自己标注该守卫**经公开 API 不可达**（writer.go:369-376，写失败先毒化块），"
            "保留它是防御性的（不让封块器拿到 0 行 spool ⇒ 否则该 part 的所有点读会从"
            "「响亮缺失」退化成「行号越界」）。因此本行只证明「守卫没被删」，"
            "**不能**用它宣称「Spooler 的空块不滚动由它保证」——后者见上一行与 "
            "TestSpoolerDoesNotRollEmptyBlock。"
        ),
    },
    {
        "name": "Run 的 ticker 分支只置「探针到期」标志，不调用 gate.Tick",
        "file": "internal/gateway/observability/spooler.go",
        # 结构辅助信号：钉住 Run 的 ticker 分支形态（置标志 → FlushAndRoll）。
        # 承重证据是行为用例 TestSpoolerRunDefersProbeToWriteGoroutine + 变异 m14。
        "pattern": (r"case <-ticker\.C:\s*\n\s*s\.markProbeDue\(s\.now\(\)\)\s*\n"
                    r"\s*if err := s\.FlushAndRoll\(\); err != nil \{"),
        "expect": "present",
        "why": (
            "A5-4：SpoolGate.Tick 持同一把锁执行探针（CreateTemp + write + fsync + unlink），"
            "而热路径 Allow() 取同一把锁 ⇒ Tick 不得在 Run 的 goroutine 上执行，"
            "否则每分钟一次的 fsync 的抖动会进共享的闸门锁（【论据订正，评审必修 3.3】"
            "**不要**说「阻塞 Allow() 直接影响请求 p99」：Allow() 在生产上唯一的调用者是"
            "TraceSpooler.AppendLine，而它跑在 main.go 的后台写协程上，不在请求路径上）。"
            "【效力边界】这是源码形态的启发式，不是结构不变量（把置标志挪到别处仍可绕过）；"
            "承重证据是行为用例与变异 m14/m18。"
        ),
    },
    {
        "name": "spooler 侧不出现建目录调用",
        "file": "internal/gateway/observability/spooler.go",
        # 与 Task 4 的 spool_gate.go 同一判据：网关自己一个目录都不建。
        # `\b` + `\s*\(` 只命中**直接调用形态**（不依赖包名与别名），但**不是**结构不变量：
        # 取出赋值再调、或 exec 调 mkdir(1) 都能绕过；反向它会命中注释里
        # 「标识符紧跟 ASCII 左括号」的提及 ⇒ 本任务的 spooler.go 注释一律不写该形态
        #（这条检查反向约束了自己的注释，已如实登记在报告里）。
        "pattern": r"\bMkdir[A-Za-z]*\s*\(",
        "expect": "absent",
        "why": (
            "网关侧唯一的建目录动作在冻结包里（writer.go:489）。网关侧若出现建目录调用"
            "（最可能出现在恢复探针或滚动路径里），会把消失的挂载点在容器可写层重新建回来，"
            "使运行中挂载消失**无法被检测**（D49）。"
        ),
    },
    {
        "name": "关停次序：先等写入侧排空（Done），再关 spool（评审必修 1）",
        "file": "cmd/gateway/main.go",
        # 次序检查：要求 `traceWriter.Done()` 的等待出现在 `traceBodySpooler.Close()` 之前。
        # 它是**启发式**（正则要求两条语句按此顺序出现），但真能红：把 Close 提到等待之前
        # 就不再命中。为什么只能静态钉：main() 不被任何用例执行，行为侧覆盖不到这段编排。
        #
        # 【收尾订正（2026-09 非行为性整理提交）】原正则以 `case <-traceWriter\.Done\(\):`
        # 直接起头、不带行首锚 ⇒ 把该行**注释掉**（`// case <-traceWriter.Done():`）仍然命中，
        # 于是「main.go 关停次序的唯一证据」可被一个注释满足（换序会红，注释不会）。
        # 现收紧为先匹配行首（`\n` + 只允许空白）再匹配 `case`，注释前缀 `//` 不再命中。
        # 就地实测（三种 main.go 变体，re.search 与 trace_mutate.py:373 同一调用）：
        #   旧正则：真实文件=命中 / 注释掉该 case=命中（洞） / Close 提到等 Done 之前=未命中
        #   新正则：真实文件=命中 / 注释掉该 case=未命中     / Close 提到等 Done 之前=未命中
        "pattern": r"\n[ \t]*case <-traceWriter\.Done\(\):[\s\S]*?traceBodySpooler\.Close\(\)",
        "expect": "present",
        "why": (
            "关停竞态的修复有两半：① spooler 的 Run 不再自行 Close（杀手 "
            "TestSpoolerRunDoesNotCloseOnContextCancel / TestShutdownFlushIsNotLatchedBySpoolerRun，"
            "变异 m29）；② 关停编排必须**先等写入侧排空、再 Close**（本行）。"
            "反序会让写入侧用 WithoutCancel 补写的最后一批撞上 AppendLine 的 closed 检查 ⇒ "
            "健康盘上该批全部 body_missing=true + 一条假的「磁盘降级」ERROR。"
            "【为什么不是变异】本段是 main() 里的语句次序，没有被任何用例执行到，"
            "删掉或换序都不会让测试红 ⇒ 只能静态钉住。"
            "【效力边界】带行首锚后，注释掉 `case` 行不再算命中；但把整段编排挪进另一个"
            "函数、或把 `Close()` 改成经别名调用，仍可绕过 —— 它依然是启发式，不是结构不变量。"
        ),
    },
]

# ---------------------------------------------------------------------------
# 允许的绿：本集合预期为空 —— 29 条变异各由一条具体顶层用例杀死。
# ---------------------------------------------------------------------------
EXPECTED_GREEN = {}

# ---------------------------------------------------------------------------
# 映射表：计划/规格/增补条目写的每条要求 → 集合里的对应条目。
# ---------------------------------------------------------------------------
REQUIREMENTS = [
    {
        "plan": "计划 2 任务 5：启动期根目录必须存在（只覆盖「启动时未挂载」，见 D49）",
        "mutations": ["m01_删除启动期根目录存在性检查"], "checks": [],
        "note": (
            "**补入隔离探针**：简报的 TestSpoolerRefusesMissingRoot 杀不死本条（身份基线在"
            "缺失路径上同样失败，两条检查互相遮蔽）⇒ 新增 "
            "TestSpoolerRefusesMissingRootEvenWhenIdentityIsObtainable。"
        ),
    },
    {
        "plan": "计划 2 全局约束：root 不得是文件系统根（构造层第二道防线）",
        "mutations": ["m02_允许文件系统根"], "checks": [],
    },
    {
        "plan": "计划 2 全局约束 / D49：运行中挂载消失必须在一个 tick 内闩锁并停止写盘",
        "mutations": ["m03_滚动前不复检根目录", "m05_Append失败后不复检根目录",
                      "m19_身份失败不再单独报原因"], "checks": [],
    },
    {
        "plan": "A5-1 第 1/2 点：启动期记录根目录身份基线；每 tick/滚动前比对，不一致或取不到即闩锁",
        "mutations": ["m10_默认检查不再比对根目录身份", "m11_取身份失败被忽略",
                      "m13_构造期忽略身份基线失败", "m19_身份失败不再单独报原因"], "checks": [],
        "note": (
            "**补入条目（简报没有）**：简报的 RootCheck 默认实现只做 stat + IsDir，"
            "而冻结包 writer.go:489 会在容器可写层把 root 重建出来 ⇒ 存在性、IsDir、"
            "写探针全部成功，A5-1（D49/D55 的归属）实际上没有被落实。"
            "杀手 = TestSpoolerLatchesWhenRootIdentityChanges / "
            "TestSpoolerLatchesWhenRootIdentityUnobtainable / "
            "TestSpoolerRefusesRootWithoutIdentityBaseline。"
        ),
    },
    {
        "plan": "A5-1 第 3 点：解锁探针必须先过同一道身份检查，且**对注入的 Gate 同样生效**（不得为此改 SpoolGate 接口）",
        "mutations": ["m12_解锁探针不再验身份",
                      "m22_探针身份包装被条件化（两种Gate的探针都不再验身份）"], "checks": [],
        "note": (
            "杀手 = TestSpoolerProbeRefusesToUnlatchWhenIdentityMismatched（探针成功 + 水位正常，"
            "只有身份维持闩锁）与 TestSpoolerInjectedGateProbeStillRequiresIdentity。"
            "【评审 I2-②】原实现只在 `opts.Gate == nil` 分支里做包装 ⇒ Task 6 为尊重配置水位"
            "而注入 Gate 时探针没有身份检查，identityErr 又是构造期闭包、Task 6 自建不出等价探针 "
            "⇒ 「挂载缺失 + 容器层可写」会解锁并按 ProbeInterval 反复发生。现改为无条件包装"
            "（guardProbe，放在两个分支之后）⇒ m22 专盯「只对内建 Gate 生效」这一形态。"
        ),
    },
    {
        "plan": "A5-1：身份的取值本身必须可判别（覆盖 root 的最长前缀挂载项）、**稳定**（同一目录同一挂载点随时间不变）、且与 mountinfo 行序无关",
        "mutations": ["m20_挂载身份不再选最长前缀", "m21_身份取值不稳定",
                      "m28_并列挂载点先到者胜"], "checks": [],
        "note": (
            "**补入条目**：挂载级解析是 Linux 生产路径上的代码，本机（macOS）没有 "
            "/proc/self/mountinfo ⇒ 用合成 mountinfo 内容覆盖（包内用例 "
            "TestMountIdentityForPicksLongestPrefixAndSurvivesEscape）。"
            "「不稳定」是比「漏检」更容易发生的失效：任何把时间掺进身份的实现都会让"
            "第一次周期复检就永久闩锁（杀手 TestDefaultRootIdentityIsStableForTheSameDirectory）。"
            "【评审 M6】并列挂载点（同一路径被多次挂载）原先「先到者胜」⇒ 身份随行序漂移，"
            "与「不稳定」同类；现由 mountKey.beats 提供确定性 tie-break（m28 专盯它）。"
        ),
    },
    {
        "plan": "计划 2 任务 5：根目录不可用的那一轮不得滚动（不做结构性写入）",
        "mutations": ["m04_根目录不可用时仍然滚动"], "checks": [],
    },
    {
        "plan": "规格 §15.1 R9：闩锁期间不得触碰文件系统；写失败必须闩锁",
        "mutations": ["m06_闩锁时仍然尝试写入", "m07_写失败不闩锁"], "checks": [],
        "note": (
            "简报指定的杀手 TestSpoolerLatchedAppendDoesNotTouchDisk 在简报的 11 条用例里"
            "**不存在** ⇒ 改用具体顶层用例：m06 = TestSpoolerLatchesWhenRootDisappears，"
            "m07 = TestSpoolerLatchesOnAppendFailureWhileRootHealthy（**新增**："
            "简报指定的那条用例会让 checkRoot 先闩锁并提前 return ⇒ m07 零覆盖）。"
        ),
    },
    {
        "plan": "规格 §7.1：周期 flush 必须无条件执行（入口 PendingSeals），周期 ≤1m 有默认值",
        "mutations": ["m08_FlushAndRoll不再flush", "m15_周期默认值失效"], "checks": [],
        "note": "**补入条目**：周期默认值原先零覆盖（未导出字段，包内用例 TestSpoolerFlushIntervalDefaultsToOneMinute）。",
    },
    {
        "plan": "规格 §15.1 R8：达阈值必须滚动（MarkSealed），且滚动前必须已 flush",
        "mutations": ["m09_不再滚动"], "checks": [],
        "note": (
            "「先 flush、后滚动」的次序由 FlushAndRoll 的语句次序保证，且 MarkSealed 自身"
            "会 flush 当前块 ⇒ 交换次序**不产生可观测差异**（造不出能红的隔离变异），"
            "故不为它登记变异；次序的后果由 "
            "TestSpoolerRollLeavesCompleteSpoolAndNeverAppendsAgain 固定（滚动后老 part "
            "逐字节不变 + 以完整行结尾）。"
        ),
    },
    {
        "plan": "A5-4：Tick（探针 fsync）必须在写协程上执行，且到期标志恰好消费一次",
        "mutations": ["m14_Run在自己的goroutine上直接Tick", "m18_到期标志不再恰好消费一次"],
        "checks": ["Run 的 ticker 分支只置「探针到期」标志，不调用 gate.Tick"],
        "note": (
            "**补入条目**：杀手 = TestSpoolerRunDefersProbeToWriteGoroutine（B 层）与 "
            "TestSpoolerDueProbeIsConsumedExactlyOnce（包内，需确定性置标志）。"
            "静态行只是结构辅助信号。"
        ),
    },
    {
        "plan": "规格 §9.3：降级必须有计数器（Flushes / Rolls 必须真的写进状态快照）；闩锁**跳变**必须恰好一条 ERROR 日志",
        "mutations": ["m16_Flushes计数不再累加", "m17_Rolls计数不再累加",
                      "m25_闩锁日志不再按跳变判定（每次都打）", "m26_根目录闩锁日志不再标ERROR"], "checks": [],
        "note": (
            "**补入条目**：简报只定义了 Stats()，没有任何断言 ⇒ 两个计数器原先零覆盖。"
            "【评审 I1】闩锁日志原先**完全没有**：三处 `Trip` 的返回值被丢弃，"
            "而 Run 只在 FlushAndRoll 返错时打日志（根目录不可用那一轮返回 nil）"
            "⇒ 跳变时刻一条日志都没有。现改为 `if Trip(...) { log.Printf(... ERROR ...) }`"
            "（Trip 契约要求调用方据此恰好打一条）：杀手 = "
            "TestSpoolerLatchLogsExactlyOncePerTransition（m25 钉「不刷屏」、m26 钉「级别可见」）。"
            "【归属】闩锁**期间**的周期汇总日志属 Task 7，本任务只负责跳变那一刻。"
        ),
    },
    {
        "plan": "规格 §9.3 / Task 4 契约：内置 Gate 的水位必须来自配置（TraceSpoolerOptions.DiskWatermarkPercent，0 ⇒ 90）",
        "mutations": ["m23_内置Gate忽略配置水位", "m24_去掉水位默认值"], "checks": [],
        "note": (
            "**补入条目（评审 I2-①）**：原先水位硬编码 90，配置层的 DiskWatermarkPercent "
            "唯一消费者就是那个硬编码值 ⇒ Task 6 只能靠注入 Gate 来「尊重配置」，"
            "而那条路会让身份探针失效（见 A5-1 第 3 点那条要求）。"
            "杀手 = TestSpoolerDiskWatermarkOptionReachesBuiltGate（包内，断言接线而不依赖真实盘用量）。"
        ),
    },
    {
        "plan": "Task 5 契约：Close 之后不得再写（冻结包 Writer.Close 没有 closed 标志）",
        "mutations": ["m27_Close后仍允许写入"], "checks": [],
        "note": (
            "**补入条目（评审 M3）**：`Writer.Close` 只关句柄并把 cur 置 nil ⇒ 之后 AppendLine "
            "会被当成「正常的新块」重建 part，一个已封口的 spool 被静默续写。"
            "杀手 = TestSpoolerAppendAfterCloseDoesNotRecreatePart。"
        ),
    },
    {
        "plan": "计划 2 任务 5：空块不滚动（守卫来自计划 1 冻结的 PendingSeals）",
        "mutations": [],
        "checks": ["空块不滚动：真正承重的守卫是 PendingSeals 的 `cur == nil` 提前返回（计划 1 冻结行为）",
                   "空块不滚动：`rows == 0` 防御性守卫仍在（经公开 API 不可达；简报点名要钉）"],
        "note": (
            "摘掉滚动（m09）之后 TestSpoolerDoesNotRollEmptyBlock 仍会绿（它断言的是"
            "「不滚动」）—— 正常：该守卫由计划 1 冻结的行为提供，本计划不得修改该文件，"
            "故用静态检查行固定它。**评审 M1 的修正**：原先把归因写成 `rows == 0` 守卫，"
            "而冻结包自己标注该守卫经公开 API **不可达**（writer.go:369-376）；"
            "实际承重的是 `cur == nil`（writer.go:358-360）⇒ 现拆成两行，归因如实。"
        ),
    },
    {
        "plan": "计划 2 全局约束 / D49：网关侧（含 spooler）绝不得创建目录",
        "mutations": [], "checks": ["spooler 侧不出现建目录调用"],
    },
    {
        "plan": "合并前广度评审必修 1：关停竞态 —— Run 不得自行 Close；关停次序 = cancel → 等 Done → Close",
        "mutations": ["m29_Run在ctx取消时自行关闭spool"],
        "checks": ["关停次序：先等写入侧排空（Done），再关 spool（评审必修 1）"],
        "note": (
            "缺陷：`cancel()` 后 spooler 的 Run 与写入侧的 Run 各自 select、**无同步**；"
            "spooler 先 Close ⇒ 写入侧补写的最后一批（≤flushN=50 条 / ≤2s）全部 "
            "body_missing=true，并经 noteSpoolFailure 打一条 ERROR 级「正文落盘已闩锁」"
            "—— 盘健康却发出磁盘降级告警，每次优雅重启都可能发生（违反计划 plan:66"
            "「main.go 关闭时保持既有行为」）。根因是计划草稿 plan:2481 骨架的 `_ = s.Close()`。"
            "修复：Run 的 ctx.Done 只 return（生命周期归调用方），main 在 cancel 后先等 "
            "(*TraceWriter).Done()（外层 6s 上限防死锁）再 Close。"
            "杀手（确定性，不靠调度概率）："
            "TestSpoolerRunDoesNotCloseOnContextCancel —— 先等 Run 的包装 goroutine 返回"
            "（旧实现此刻必然已 Close），再断言 AppendLine 仍然成功；"
            "TestShutdownFlushIsNotLatchedBySpoolerRun —— 写入器用无缓冲 channel（Emit 返回"
            "即代表事件已进 batch），追加被押在「spooler 的 Run 已返回」之后，断言最后一批的"
            "追加不是 ErrSpoolLatched 且 PG 行没有 body_missing=true。"
            "②（main.go 的语句次序）不被任何用例执行 ⇒ 由静态检查行钉住次序。"
        ),
    },
]
