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
  故用静态行固定其存在。
"""

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
MUTATIONS["m07_写失败不闩锁"] = [
    ("sub", 's.gate.Trip("append_failed", err)', "_ = err"),
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
# 摘掉选取逻辑后解析器判不出所属挂载 ⇒ require.True(ok) 转红。
# 【为什么必须用合成内容】本机（macOS）没有 /proc/self/mountinfo，挂载级解析只在
# Linux 生产路径上生效 ⇒ 不测就是开发环境里的零覆盖。
MUTATIONS["m20_挂载身份不再选最长前缀"] = [
    ("sub", "if len(mp) > bestLen {", "if len(mp) > bestLen && false {"),
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

# ---------------------------------------------------------------------------
# 静态检查：**变异测不到**的要求必须有静态检查行（全局约束的证据制度第 4 条）。
# ---------------------------------------------------------------------------
CHECKS = [
    {
        "name": "空块不滚动的守卫来自 PendingSeals（计划 1 冻结行为）",
        "file": "internal/tracearchive/writer.go",
        "pattern": r"if cur\.rows == 0 \{\s*\n\s*return nil, nil",
        "expect": "present",
        "why": (
            "「空块不滚动」由计划 1 的 PendingSeals 空块守卫提供（本计划不得修改该文件），"
            "因此这里用静态检查固定它的存在；若计划 1 的守卫被删，本检查会红。"
            "行为侧的证据是 TestSpoolerDoesNotRollEmptyBlock（它断言首次写入落在 "
            "part-000000，即空块上那一轮没有 MarkSealed 过）。"
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
            "否则每分钟一次的 fsync 会阻塞写协程。"
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
]

# ---------------------------------------------------------------------------
# 允许的绿：本集合预期为空 —— 21 条变异各由一条具体顶层用例杀死。
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
        "plan": "A5-1 第 3 点：解锁探针必须先过同一道身份检查（不得为此改 SpoolGate 接口）",
        "mutations": ["m12_解锁探针不再验身份"], "checks": [],
        "note": "杀手 = TestSpoolerProbeRefusesToUnlatchWhenIdentityMismatched（探针成功 + 水位正常，只有身份维持闩锁）。",
    },
    {
        "plan": "A5-1：身份的取值本身必须可判别（覆盖 root 的最长前缀挂载项）且**稳定**（同一目录同一挂载点随时间不变）",
        "mutations": ["m20_挂载身份不再选最长前缀", "m21_身份取值不稳定"], "checks": [],
        "note": (
            "**补入条目**：挂载级解析是 Linux 生产路径上的代码，本机（macOS）没有 "
            "/proc/self/mountinfo ⇒ 用合成 mountinfo 内容覆盖（包内用例 "
            "TestMountIdentityForPicksLongestPrefixAndSurvivesEscape）。"
            "「不稳定」是比「漏检」更容易发生的失效：任何把时间掺进身份的实现都会让"
            "第一次周期复检就永久闩锁（杀手 TestDefaultRootIdentityIsStableForTheSameDirectory）。"
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
        "plan": "规格 §9.3：降级必须有计数器（Flushes / Rolls 必须真的写进状态快照）",
        "mutations": ["m16_Flushes计数不再累加", "m17_Rolls计数不再累加"], "checks": [],
        "note": "**补入条目**：简报只定义了 Stats()，没有任何断言 ⇒ 两个计数器原先零覆盖。",
    },
    {
        "plan": "计划 2 任务 5：空块不滚动（守卫来自计划 1 冻结的 PendingSeals）",
        "mutations": [],
        "checks": ["空块不滚动的守卫来自 PendingSeals（计划 1 冻结行为）"],
        "note": (
            "摘掉滚动（m09）之后 TestSpoolerDoesNotRollEmptyBlock 仍会绿（它断言的是"
            "「不滚动」）—— 正常：该守卫由计划 1 冻结的行为提供，本计划不得修改该文件，"
            "故用静态检查行固定其存在。"
        ),
    },
    {
        "plan": "计划 2 全局约束 / D49：网关侧（含 spooler）绝不得创建目录",
        "mutations": [], "checks": ["spooler 侧不出现建目录调用"],
    },
]
