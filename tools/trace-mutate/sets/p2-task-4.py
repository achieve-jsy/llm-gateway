r"""计划 2 / 任务 4 的变异集合（作者编写的产物 ⇒ 必须 git-tracked，见全局约束 §证据制度 1）。

被变异文件是磁盘水位与闩锁（`internal/gateway/observability/spool_gate.go`）。
这一批变异的共同主题：**每一道「盘满/写不了时不要每请求重试、恢复要显式」的防线都必须能被测出来**。

编号一经发布不再改动；匹配一律用字符串（不用行号），行号仅供人读。

## 相对简报（task-4-brief.md:447-509）的如实改动

1. **简报的 7 条用例杀不死 m05 ⇒ 新增第 8 个顶层用例**
   `TestGateProbeSuccessWithHighWatermarkKeepsLatched`。简报把
   「恢复 = 探针成功 且 水位回落」的两半分别挂在 `TestGateProbeSuccessUnlatches`
   （水位 95→50，探针成功）与 `TestGateWatermarkAloneDoesNotUnlatch`
   （水位 95→10，探针失败）上，**没有任何用例覆盖「探针成功 + 水位仍高」**。
   **实测**（用本工具自己的 `apply_mutation` + `classify`，只跑简报那 7 条名字）：
   m05 的判定是「**绿（零覆盖！）**」；补上第 8 条后为
   「红 / TestGateProbeSuccessWithHighWatermarkKeepsLatched」。详见报告 §变异集合证据。

2. **m07..m17 是补入条目**（简报只有 m01..m06）。依据是控制者硬性指令第 1 条
   「**每一条断言都必须能红**」以及自检发现的零覆盖：
   - 简报没有断言 `Stats()` 快照里的 `LatchedSeconds` / `UsagePercent` / `Latched`，
     也没断言 `Latched()` 与 `Trip` 的 reason 拼接 —— 这些字段**全都可以恒错而全部用例仍绿**；
   - `NewSpoolGate` 的 `ProbeInterval <= 0 ⇒ 1m` 默认值无人守（Task 2 评审 F1 同类：
     默认值漂移无守卫）；
   - D49 的硬约束「探针不得创建根目录」原本只有静态检查 ⇒ 补一条**行为**用例
     （用真实默认探针 + 不存在的 `<root>`），使注入 MkdirAll 能被行为断言抓住；
   - `statfs 失败本身不闩锁` 是简报明确写下的设计决定，却没有任何用例覆盖。

3. **m07/m13 的语义按本实现登记**：`LatchedSeconds` 是「**本次**闩锁已持续秒数」
   （每次 Tick 重算、解锁归零），不是跨闩锁周期的历史累计 —— 后者需要额外字段与
   额外记账，超出本任务接口。测试同时钉住「闩锁中 == 120s」（m07）与「解锁后 == 0」（m13）。

4. **CHECKS 的正则按实测加严**：简报给的 `os\.MkdirAll` 只钉**无别名的调用形态**，
   `o "os"` + `o.MkdirAll(...)` 即可绕过（假绿）⇒ 改为 `\bMkdir[A-Za-z]*\s*\(`，
   覆盖 `os.MkdirAll` / `syscall.Mkdir` / `os.MkdirTemp` 与一切别名形态。
   实测：把别名调用临时写进实现副本，简报正则【PASS（漏判）】、本正则【FAIL（命中）】。

5. **m06 的变异串按实测修正**：简报给的 `if probeErr != nil {` → `if false {` 会让
   `probeErr` 变成「声明未使用」⇒ **编译失败红**（一条用例都不执行；权威表判定
   「红(编译失败)」），证明不了守卫承重，也不满足简报「6 条断言红」的预期。
   改为 `if probeErr != nil && false {`（保住对 probeErr 的引用）⇒ 断言红，
   杀手用例 TestGateWatermarkAloneDoesNotUnlatch。

## 变异测不到、但有静态检查行（CHECKS）

- 「网关侧不得创建目录」（D49）：行为用例无法表达「本文件里没有建目录调用」
  （删除一个本就不存在的调用不改变任何行为）⇒ 只能静态钉住源码形态。
"""

NAME = "p2-task-4"
TARGET = "internal/gateway/observability/spool_gate.go"
# 本任务的用例是 B 层（package observability_test），只依赖公开 API
PACKAGES = ["./tests/gateway/observability/"]
MANIFEST_FILES = ["tests/gateway/observability/spool_gate_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/gateway/observability/spool_gate_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 本集合的用例不依赖数据库/外部二进制/真实磁盘/时区数据，没有可 SKIP 的理由。
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# ---- 水位闩锁 ----
# 杀手用例 TestGateTripsOnDiskWatermark：水位比较永不成立 ⇒ Tick 不闩锁 ⇒
# Allow() 仍为真（以及 Latched/Reason/Trips/UsagePercent 全部不符）⇒ 转红。
MUTATIONS["m01_水位比较改为永不触发"] = [
    ("sub", "if pct >= float64(g.opts.DiskWatermarkPercent) {", "if false {"),
]

# ---- 跳变幂等 ----
# 杀手用例 TestGateTripIsIdempotentAndReportedOnce：第二次 Trip 再报一次跳变
# ⇒ `require.False` 与 `Stats().Trips == 1` 同时转红（日志会每行一条 ERROR）。
MUTATIONS["m02_Trip不再幂等"] = [
    ("sub", "if g.latched {\n\t\treturn false\n\t}", "if false {\n\t\treturn false\n\t}"),
]

# ---- Allow 零 IO（R9 的核心：不要每请求重试）----
# 杀手用例 TestGateLatchedDoesNoFilesystemWork：Allow() 每次查水位 ⇒
# 1000 次 Allow 后 `u.calls` 从 1 涨到 1001 ⇒ 转红。
MUTATIONS["m03_Allow每次查水位"] = [
    ("sub", "return !g.latched", "_, _ = g.opts.UsagePercent(g.opts.Root); return !g.latched"),
]

# ---- 探针周期 ----
# 杀手用例 TestGateProbeRespectsInterval：摘掉周期判断后 59s 的 Tick 也探针
# ⇒ 第一段「未到 ProbeInterval 不得探针」转红。
MUTATIONS["m04_探针周期改为0"] = [
    ("sub", "if now.Sub(g.lastProbe) < g.opts.ProbeInterval {", "if false {"),
]

# ---- 恢复条件必须是「探针成功 + 水位回落」两个条件 ----
# m05 杀手 = TestGateProbeSuccessWithHighWatermarkKeepsLatched（**本任务新增**：
# 简报的 7 条用例全部覆盖不到「探针成功但水位仍高」⇒ 简报的 m05 是假绿）。
# 摘掉水位判据后，探针成功即解锁 ⇒ 该用例的 `require.False(Allow())` 转红。
MUTATIONS["m05_解锁只看探针"] = [
    ("sub", "if usageErr == nil && pct >= float64(g.opts.DiskWatermarkPercent) {", "if false {"),
]
# m06 杀手 = TestGateWatermarkAloneDoesNotUnlatch：水位回落到 10% 但探针仍失败
# ⇒ 若忽略探针失败就会解锁，该用例的 `require.False(Allow())` 转红。
# （注意 TestGateProbeFailureKeepsLatched **杀不死** m06：它的水位仍是 95%，
# 于是「水位未回落」那一支照样保持闩锁、ProbeFailures 也照样 +1。）
# 【相对简报的修正】简报写的是把 `if probeErr != nil {` 换成 `if false {`，
# 实测那是**编译失败红**（probeErr 变成「声明未使用」）⇒ 一条用例都不执行，
# 证明不了守卫承重（也不满足「6 条断言红」的预期）。改成 `&& false` 保住了对
# probeErr 的引用，编译通过 ⇒ 真正的断言红。
MUTATIONS["m06_解锁只看水位"] = [
    ("sub", "if probeErr != nil {", "if probeErr != nil && false {"),
]

# ---- §9.3 降级计数器：LatchedSeconds（本次闩锁已持续秒数）----
# 杀手用例 TestGateProbeFailureKeepsLatched：不再重算 ⇒ Stats().LatchedSeconds
# 恒为 0，而该用例断言闩锁 120s 后为 120 ⇒ 转红。
MUTATIONS["m07_闩锁秒数不再累计"] = [
    ("sub", "g.latchedSeconds = int64(now.Sub(g.latchedAt).Seconds())", "_ = g.latchedSeconds"),
]

# ---- §9.3 跳变日志必须能说出「为什么」----
# m08 杀手 = TestGateTripIsIdempotentAndReportedOnce：Trip 不再拼接底层 cause
# ⇒ `require.Contains(Reason(), "no space")` 转红（只有 "append_failed" 说不清根因）。
MUTATIONS["m08_Trip丢弃底层cause"] = [
    ("sub", "g.reason = reason\n\tif cause != nil {\n\t\tg.reason = fmt.Sprintf(\"%s: %v\", reason, cause)\n\t}",
            "g.reason = reason"),
]
# m14 杀手 = TestGateTripsOnDiskWatermark：水位闩锁的原因只剩 "disk_full"
# ⇒ `require.Contains(Reason(), "watermark")`（以及快照里的同一条）转红。
MUTATIONS["m14_水位原因文案丢失"] = [
    ("sub", "g.reason = fmt.Sprintf(\"disk_watermark: %s 已用 %.1f%% ≥ %d%%\",\n\t\t\t\t\tg.opts.Root, pct, g.opts.DiskWatermarkPercent)",
            "g.reason = \"disk_full\""),
]

# ---- §9.3 状态快照的字段必须真的被写入 ----
# m09 杀手 = TestGateTripsOnDiskWatermark：未闩锁分支不再记录最近水位
# ⇒ `Stats().UsagePercent == 91` 转红（快照里的水位恒为 0，运维看不到真实水位）。
MUTATIONS["m09_最近水位不写入快照"] = [
    ("sub", "if pct, err := g.opts.UsagePercent(g.opts.Root); err == nil {\n\t\t\tg.usagePercent = pct",
            "if pct, err := g.opts.UsagePercent(g.opts.Root); err == nil {\n\t\t\t_ = pct"),
]
# m10 杀手 = TestGateTripsOnDiskWatermark：Latched() 读数取反
# ⇒ 闩锁期间 `require.True(g.Latched())` 转红（Allow() 仍对，所以只有这一条能测出来）。
MUTATIONS["m10_Latched读数取反"] = [
    ("sub", "func (g *SpoolGate) Latched() bool { g.mu.Lock(); defer g.mu.Unlock(); return g.latched }",
            "func (g *SpoolGate) Latched() bool { g.mu.Lock(); defer g.mu.Unlock(); return !g.latched }"),
]
# m11 杀手 = TestGateTripsOnDiskWatermark：快照里的 Latched 恒 false
# ⇒ `require.True(stats.Latched)` 转红（漏赋值是这类快照最典型的错法）。
MUTATIONS["m11_快照Latched恒false"] = [
    ("sub", "Latched: g.latched, Reason: g.reason,", "Latched: false, Reason: g.reason,"),
]

# ---- 解锁后状态必须清零 ----
# m12 杀手 = TestGateProbeSuccessUnlatches：解锁后 reason 残留
# ⇒ `require.Empty(g.Reason())` 转红（「原因」还在但盘已经能写，日志/看板会误导）。
MUTATIONS["m12_解锁不清空reason"] = [
    ("sub", "g.latched = false\n\tg.reason = \"\"", "g.latched = false"),
]
# m13 杀手 = TestGateProbeSuccessUnlatches：解锁后本次闩锁秒数残留
# ⇒ `require.Equal(int64(0), Stats().LatchedSeconds)` 转红。
MUTATIONS["m13_解锁不归零闩锁秒数"] = [
    ("sub", "g.reason = \"\"\n\tg.latchedSeconds = 0", "g.reason = \"\""),
]

# ---- 构造期的默认值：ProbeInterval 未设置 ⇒ 1m ----
# 杀手用例 TestGateProbeIntervalDefaultsToOneMinute：默认值失效后 ProbeInterval=0，
# 每次 Tick 都探针 ⇒ 该用例第二段（30s 时不得探针）转红。
# 闩锁的节流靠这个默认值兜底：没有它，「每请求重试」就退化成「每 flush tick 探针」。
MUTATIONS["m15_探针周期默认值失效"] = [
    ("sub", "if opts.ProbeInterval <= 0 {\n\t\topts.ProbeInterval = time.Minute\n\t}",
            "if false {\n\t\topts.ProbeInterval = time.Minute\n\t}"),
]

# ---- D49：探针绝不得创建根目录 ----
# 杀手用例 TestGateDefaultProbeDoesNotCreateRoot（真实默认探针 + 不存在的 `<root>`）：
# 注入 MkdirAll 后根目录被建出来 ⇒ 探针「成功」、闩锁被解开，且
# `os.IsNotExist(missing)` 断言命中 ⇒ 转红。
# 注意这条**同时**会被 CHECKS 的静态行命中：行为与结构两条独立证据。
MUTATIONS["m16_探针创建根目录"] = [
    ("sub", "f, err := os.CreateTemp(root, \".probe-*\")",
            "_ = os.MkdirAll(root, 0o755)\n\tf, err := os.CreateTemp(root, \".probe-*\")"),
]

# ---- statfs 失败不得伪装成「盘满」----
# 杀手用例 TestGateUsageErrorDoesNotLatch：把「读不到水位」也当闩锁后
# `Allow()` / `Trips == 0` / `Reason() == ""` 三条断言同时转红。
# 【这是注入反向行为的变异】它正是「顺手修 D49」时最可能被引入的改动，
# 也正是简报明确禁止的（权限/路径问题会伪装成磁盘问题）。
MUTATIONS["m17_statfs失败也闩锁"] = [
    ("sub", "if pct, err := g.opts.UsagePercent(g.opts.Root); err == nil {",
            "pct, err := g.opts.UsagePercent(g.opts.Root)\n"
            "\t\tif err != nil {\n"
            "\t\t\tg.latched = true\n"
            "\t\t\tg.reason = \"statfs_failed\"\n"
            "\t\t\tg.latchedAt = now\n"
            "\t\t\tg.lastProbe = now\n"
            "\t\t\tg.trips++\n"
            "\t\t}\n"
            "\t\tif err == nil {"),
]

# ---------------------------------------------------------------------------
# 静态检查：**变异测不到**的要求必须有静态检查行（全局约束的证据制度第 4 条）。
# ---------------------------------------------------------------------------
CHECKS = [
    {
        "name": "网关侧不出现建目录调用",
        "file": "internal/gateway/observability/spool_gate.go",
        # 【相对简报加严，实测依据见文件头第 4 条】简报的 `os\.MkdirAll` 只钉无别名的
        # 调用形态（`o "os"` + `o.MkdirAll(...)` 即可绕过 ⇒ 假绿）；本正则钉「任何
        # Mkdir* 调用」，不依赖包名与别名形态。`\b` + `\s*\(` 排除注释里不带括号的
        # 提及（本文件注释正当地提到了 MkdirAll）。
        "pattern": r"\bMkdir[A-Za-z]*\s*\(",
        "expect": "absent",
        "why": (
            "网关自己一个目录都不建：唯一的 MkdirAll 在冻结包里（writer.go:489，见 D49）。"
            "网关侧若出现建目录调用（最可能出现在恢复探针里），会把消失的挂载点在容器可写层"
            "重新建回来，使运行中挂载消失**无法被检测**。"
            "【为什么必须静态检查】把探针里本就不存在的 MkdirAll 删掉不改变任何行为，"
            "没有任何行为用例能表达这条结构不变量；它同时被 defaultProbeWrite 的注释"
            "（探针用 os.CreateTemp，父目录必须已存在）在实现上约束。"
        ),
    },
]

# ---------------------------------------------------------------------------
# 允许的绿：本集合预期为空 —— 14 条变异各由一条具体顶层用例杀死。
# ---------------------------------------------------------------------------
EXPECTED_GREEN = {}

# ---------------------------------------------------------------------------
# 映射表：计划/规格写的每条要求 → 集合里的对应条目。
# ---------------------------------------------------------------------------
REQUIREMENTS = [
    {
        "plan": "规格 §15.1 R9：水位超限必须放弃正文（本计划用注入水位表达）",
        "mutations": ["m01_水位比较改为永不触发"], "checks": [],
    },
    {
        "plan": "计划 2 任务 4：闩锁跳变必须幂等（日志只打一条的前提）",
        "mutations": ["m02_Trip不再幂等"], "checks": [],
    },
    {
        "plan": "规格 §15.1 R9：闩锁期间不得有任何文件系统调用（Allow 零 IO）",
        "mutations": ["m03_Allow每次查水位"], "checks": [],
    },
    {
        "plan": "计划 2 任务 4：探针周期（未到不探、到点探一次、不足周期不重复）",
        "mutations": ["m04_探针周期改为0"], "checks": [],
    },
    {
        "plan": "计划 2 任务 4 / 规格 §15.1 R9：恢复必须是「探针成功 且 水位回落」两个条件",
        "mutations": ["m05_解锁只看探针", "m06_解锁只看水位"], "checks": [],
        "note": (
            "m05 的杀手 TestGateProbeSuccessWithHighWatermarkKeepsLatched 是本任务**新增**用例："
            "简报的 7 条用例里没有一条覆盖「探针成功但水位仍高」⇒ 实测 m05 在简报用例集上"
            "判定为「绿（零覆盖！）」；m06 的杀手是 TestGateWatermarkAloneDoesNotUnlatch。"
        ),
    },
    {
        "plan": "规格 §9.3：闩锁的降级信号必须能说出「为什么」（reason 带水位数值与底层 cause）",
        "mutations": ["m08_Trip丢弃底层cause", "m14_水位原因文案丢失"], "checks": [],
    },
    {
        "plan": ("规格 §9.3：降级必须暴露计数器与状态快照"
                 "（Trips / ProbeFailures / LatchedSeconds / UsagePercent / Latched / Reason）"),
        "mutations": ["m07_闩锁秒数不再累计", "m09_最近水位不写入快照",
                      "m10_Latched读数取反", "m11_快照Latched恒false"], "checks": [],
        "note": (
            "**补入条目（简报没有）**：简报只断言了 Trips 与 ProbeFailures，"
            "`LatchedSeconds` / `UsagePercent` / `Latched` 三个 §9.3 字段可以恒错而全绿。"
            "LatchedSeconds 的口径按本实现登记为「本次闩锁已持续秒数」（解锁归零）。"
        ),
    },
    {
        "plan": "计划 2 任务 4：解锁必须把上一次闩锁的状态清零（reason / 本次闩锁秒数）",
        "mutations": ["m12_解锁不清空reason", "m13_解锁不归零闩锁秒数"], "checks": [],
    },
    {
        "plan": "计划 2 任务 4：ProbeInterval 默认 1m（未设置时不得退化成「每 tick 探针」）",
        "mutations": ["m15_探针周期默认值失效"], "checks": [],
        "note": (
            "**补入条目**：`NewSpoolGate` 的默认值原先零覆盖（与 Task 2 评审 F1「默认值漂移"
            "无守卫」同类）。杀手用例 TestGateProbeIntervalDefaultsToOneMinute。"
        ),
    },
    {
        "plan": "规格 §15.1 R9 + 全局约束 D49：探针不得创建根目录（根目录不存在 ⇒ 探针失败并保持闩锁）",
        "mutations": ["m16_探针创建根目录"], "checks": ["网关侧不出现建目录调用"],
        "note": (
            "**补入条目**：D49 的这条硬约束原先只有静态检查 ⇒ 补行为用例 "
            "TestGateDefaultProbeDoesNotCreateRoot（真实默认探针 + 不存在的 `<root>`），"
            "注入 MkdirAll 即转红；静态行与行为断言是两条独立证据。"
        ),
    },
    {
        "plan": "计划 2 任务 4：statfs 失败本身不闩锁（「读不到水位」≠「盘满」）",
        "mutations": ["m17_statfs失败也闩锁"], "checks": [],
        "note": (
            "**补入条目**：简报在 Tick 里明确写下这条设计决定却无用例覆盖；"
            "杀手用例 TestGateUsageErrorDoesNotLatch。该分支是 D49「运行中挂载消失」"
            "交接给任务 5 的关键上下文（见交付报告 §顾虑）。"
        ),
    },
    {
        "plan": "计划 2 全局约束 / D49：网关侧（含恢复探针）绝不得创建目录",
        "mutations": [], "checks": ["网关侧不出现建目录调用"],
    },
]
