"""计划 2 / 任务 2 的变异集合（作者编写的产物 ⇒ 必须 git-tracked，见 D17）。

被变异文件是网关侧配置校验（`internal/gateway/config/trace_body.go`）。这一批变异的
共同主题：**每一条校验都必须能被测出来** —— 删掉它之后，对应用例必须转红。

编号一经发布不再改动；匹配一律用字符串（不用行号），行号仅供人读。

## 相对简报（task-2-brief.md:397-508）的四处**如实**改动

1. **m06 的变异串换掉了**。简报写的是
   `return ts.In(TraceBodyZone).Format("2006-01-02")` → `ts.In(time.Local)...`，
   但真实实现里 `DayOf` 是 `return c.DayStart(ts).Format("2006-01-02")` ——
   该串**在 HEAD 上定位到 0 处**（`--check` 会直接失败）。简报自己规定
   「若某条报定位到 0 处或多处 ⇒ 修正变异字符串（不要改测试）」，因此这里改成
   等价且可定位的形态：把 `TraceBodyZone` 的偏移从 `8*60*60` 改成 `0`（口径不再是 UTC+8）。
   **另一条理由（本机实测）**：`time.Local` 形态的变异在**本机不可检出** ——
   开发机 `/etc/localtime → Asia/Shanghai`（`date` 输出 CST），
   `time.Local` 恰好就是 +08:00，`ts.In(time.Local)` 与 `ts.In(TraceBodyZone)`
   逐字同解 ⇒ 该变异是**假绿**。用固定偏移 0 则与宿主时区无关，恒可检出。
   额外的 `t := ts.In(TraceBodyZone)` → `t := ts.UTC()`（m11）覆盖**调用点**。

2. **m03 / m16 是登记的「允许的绿」[等价变异]**（详见 EXPECTED_GREEN）。
   简报的 `EXPECTED_GREEN = {}` 与它自己的期望「10 条全部转红」在这一处与代码事实不符：
   这两条守卫在**当前校验次序**下不可达。

3. **m11/m12 是补入的条目**：m11 覆盖 `DayStart` 的调用点（简报没覆盖）；
   m12 覆盖控制者硬性口径第 2 条的后半句「开启而未设 inst ⇒ 启动失败」——
   这条守卫在简报的 10 条里没有任何变异或静态检查对应。

4. **m14/m15/m16 是补入的条目**：文件头写明的主题是「**每一条**校验都必须能被测出来」，
   而简报的 10 条漏掉了 `block_mb <= 0` / `flush_interval <= 0` / `roll_interval <= 0`
   三条守卫。前两条可被隔离探针杀死（承重），第三条不可达（见 m16）。

5. **m17/m18 是评审 F2 补入的条目（同时改了实现与用例）**：评审指出规格 §7.1 与全局约束
   声明了「周期 flush ≤ 1m」，而修复前的实现只校验 `flush > 0` 与 `flush < roll` ⇒
   `TRACE_BODY_FLUSH_INTERVAL=5m` + `roll=1h` 被接受，**声明的不变量是空的**。
   修复在 `trace_body.go` 新增 `traceBodyMaxFlushInterval = time.Minute` 与对应检查，
   并补用例 `TestTraceBodyConfigFlushIntervalMustBeAtMostOneMinute`。
   m17 = 删检查；m18 = 把阈值从 1m 放宽到 1h（两条并存：只测一条时另一条可以静默失效）。
   ⚠️ **连带改动**：`flush ≤ 1m` 上界是**更早**的守卫 ⇒ 原 m04 探针（flush=1h）会被它遮蔽成假绿，
   已把该探针的输入改成 flush=1m（见 m04 注释）。

## 变异测不到、但有静态检查行（CHECKS）

- 「`TRACE_BODY_ROOT` 留空 = 关闭」是**默认值**，变异测不到默认值 ⇒ 静态检查 `.env.example`。
- 「Viper 键必须在顶层 `trace_body.*`、不得进 `observability.*`」住在 **config.go**，
  而本集合的 TARGET 是单文件 `trace_body.go` ⇒ 静态检查 config.go 的两条正则。
- 「`DayOf` 必须由 `DayStart` 派生（单一事实源）」：任何「自己算一遍」的**正确**实现
  都得到同一个字符串 ⇒ 用例与变异在行为上都无法区分 ⇒ 只能静态钉住实现形态。
"""

NAME = "p2-task-2"
TARGET = "internal/gateway/config/trace_body.go"
PACKAGES = ["./tests/gateway/config/"]
MANIFEST_FILES = ["tests/gateway/config/trace_body_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/gateway/config/trace_body_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 本集合的用例不依赖 zstd/外部二进制/数据库，没有可 SKIP 的理由。
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# ---- inst 白名单（blue/green）：放宽成「非空即可」----
# 杀手用例 TestTraceBodyConfigInstIsWhitelisted：它的 bad 列表里有 red/Blue/blue/../green，
# 放宽后这些值都被接受 ⇒ 用例转红。
MUTATIONS["m01_inst白名单放宽为非空"] = [
    ("sub", 'if cfg.Inst != "" && cfg.Inst != "blue" && cfg.Inst != "green" {', 'if false {'),
]

# ---- 滚动间隔上界（≤1h，规格 §15.1 R8）----
# 杀手用例 TestTraceBodyConfigRollIntervalMustBeUnderOneHour：roll=2h 会被接受 ⇒ 转红。
MUTATIONS["m02_删除滚动间隔1h上界"] = [
    ("sub", "if cfg.RollInterval > traceBodyMaxRollInterval {", "if false {"),
]

# ---- 「滚动间隔必须严格小于 minAge=3h」----
# ⚠️ **登记的允许的绿（[等价变异]）**：见 EXPECTED_GREEN 的 m03 条目。
MUTATIONS["m03_删除minAge严格小于校验"] = [
    ("sub", "if cfg.RollInterval >= traceBodyMinAge {", "if false {"),
]

# ---- flush 必须远小于滚动间隔（规格 §7.1）----
# 杀手用例 = **隔离探针** TestTraceBodyConfigFlushMustBeFarBelowRoll
#（flush=1m、roll=1m，其余字段全合法 ⇒ 只有这条校验会报错）。
#
# ⚠️ 探针的 flush 必须取 **1m**（评审 F2 后改，原为 1h）：`flush ≤ 1m` 的上界
# （m17/m18 守的那条）现在是承重的，若探针写 flush=1h，上界会**先**报错 ⇒
# 删掉本守卫后探针照样红，m04 被遮蔽成假绿（同「更早的守卫遮蔽」现象）。
# 1m 正好落在上界内，只有 `flush >= roll` 能拦它。
MUTATIONS["m04_删除flush与滚动间隔的关系校验"] = [
    ("sub", "if cfg.FlushInterval >= cfg.RollInterval {", "if false {"),
]

# ---- 水位范围 1..99（登记 D45：0 会让每个请求都降级）----
# 杀手用例 = **隔离探针** TestTraceBodyConfigWatermarkRange（-1/0/100/101 全须报错）。
MUTATIONS["m05_删除水位范围校验"] = [
    ("sub", "if cfg.DiskWatermarkPercent < 1 || cfg.DiskWatermarkPercent > 99 {", "if false {"),
]

# ---- dt 口径：固定 +08:00 的偏移被改掉 ----
# 杀手用例 TestDayOfUsesUTCPlus8 的第三个子用例「UTC 16:00 已是北京次日」——
# 偏移为 0 时它得到 2026-09-24，期望 2026-09-25 ⇒ 红。
# 【为什么不是简报的 time.Local 形态】见文件头第 1 条：本机 time.Local == +08:00，
# 那种变异是假绿；固定偏移 0 与宿主时区无关。
MUTATIONS["m06_dt口径偏移改为UTC0"] = [
    ("sub", 'var TraceBodyZone = time.FixedZone("CST", 8*60*60)',
            'var TraceBodyZone = time.FixedZone("CST", 0)'),
]

# ---- root 必须是绝对路径 ----
# 杀手用例 TestTraceBodyConfigRootMustBeAbsolute（datalog/… / ./datalog / ../datalog / datalog/）。
MUTATIONS["m07_root允许相对路径"] = [
    ("sub", "if cfg.Enabled && !filepath.IsAbs(cfg.Root) {", "if false {"),
]

# ---- root 不得是文件系统根 ----
# 杀手用例 TestTraceBodyConfigRootRejectsFilesystemRoot（/ 与 Clean 后为 / 的三种形态）。
MUTATIONS["m08_root允许文件系统根"] = [
    ("sub", 'if cfg.Enabled && filepath.Clean(cfg.Root) == "/" {', "if false {"),
]

# ---- trim 后为空必须等同于未设置 ----
# 杀手用例 TestTraceBodyConfigWhitespaceRootIsDisabled：不做 TrimSpace 时
# root="   " 变成「已开启」且 IsAbs 失败 ⇒ 期待 NoError 的用例转红。
MUTATIONS["m09_root不做TrimSpace"] = [
    ("sub", 'Root:                 strings.TrimSpace(v.GetString("trace_body.root")),',
            'Root:                 v.GetString("trace_body.root"),'),
]

# ---- DayStart 必须是该日历日的 UTC 零点（不是 +08:00 的零点）----
# 杀手用例 TestDayStartIsUTCMidnightOfTheCSTDay：直接返回 t 后
# `start.Location() == time.UTC` 与 `start.Hour()==0/Minute()==0` 都会失败 ⇒ 断言红。
MUTATIONS["m10_DayStart用事件时间而非零点"] = [
    ("sub", "\treturn time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)",
            "\treturn t"),
]

# ---- DayStart 的**调用点**真的用了 TraceBodyZone（而不是 UTC / time.Local）----
# 【补入条目，简报没有】m06 只改常量，覆盖不到「调用点是否用它」。
# 杀手用例 TestDayStartIsUTCMidnightOfTheCSTDay：ts.UTC() 下北京 9-25 的那一天
# 变成 9-24 ⇒ 「DayStart 必须是北京 9-25 那一日」失败 ⇒ 红。
MUTATIONS["m11_DayStart忽略时区"] = [
    ("sub", "t := ts.In(TraceBodyZone)", "t := ts.UTC()"),
]

# ---- 开启（root 非空）而未设 inst ⇒ 必须启动失败 ----
# 【补入条目，简报没有】控制者硬性口径第 2 条的后半句。
# 杀手用例 TestTraceBodyConfigInstIsWhitelisted 的 bad 列表首项 ""（此时 root 已设）：
# 摘掉这条守卫后空 inst 被接受 ⇒ 用例转红。
MUTATIONS["m12_允许开启但inst为空"] = [
    ("sub", 'if cfg.Enabled && cfg.Inst == "" {', "if false {"),
]

# ---- Enabled 必须由 Root 推导（功能默认关闭是安全前提）----
# 【补入条目，简报没有】强开 Enabled 后，未设 root 的输入会在 IsAbs("") 上失败 ⇒
# 杀手用例 TestTraceBodyConfigDisabledByDefault 在 require.NoError 处转红
#（TestLoadWiresTraceBodyFromEnvVerbatim 的 NotPanics 探针是第二道）。
MUTATIONS["m13_Enabled强制为true"] = [
    ("sub", 'cfg.Enabled = cfg.Root != ""', "cfg.Enabled = true"),
]

# ---- block_mb 必须为正 ----
# 【补入条目，简报没有】杀手用例 = 隔离探针 TestTraceBodyConfigBlockMBMustBePositive。
MUTATIONS["m14_删除block_mb正数校验"] = [
    ("sub", "if cfg.BlockMB <= 0 {", "if false {"),
]

# ---- flush_interval 必须为正 ----
# 【补入条目，简报没有】杀手用例 = 隔离探针 TestTraceBodyConfigFlushIntervalMustBePositive
#（flush=0 会被接受 ⇒ 周期 flush 永不发生而无人察觉）。
MUTATIONS["m15_删除flush_interval正数校验"] = [
    ("sub", "if cfg.FlushInterval <= 0 {", "if false {"),
]

# ---- roll_interval 必须为正 ----
# ⚠️ **登记的允许的绿（[等价变异]）**：见 EXPECTED_GREEN 的 m16 条目。
MUTATIONS["m16_删除roll_interval正数校验"] = [
    ("sub", "if cfg.RollInterval <= 0 {", "if false {"),
]

# ---- flush 周期 ≤ 1m 的上界（评审 F2 补入：声明过、原先没强制）----
# 规格 §7.1 与全局约束都写「周期 flush ≤ 1m 且必须无条件执行」，而修复前实现只校验
# `flush > 0` 与 `flush < roll` ⇒ `TRACE_BODY_FLUSH_INTERVAL=5m` + `roll=1h` 会被接受，
# 声明的不变量是空的。修复后由上界强制。
# 杀手用例 = TestTraceBodyConfigFlushIntervalMustBeAtMostOneMinute（61s/5m 须报错，1m 须接受）。
MUTATIONS["m17_删除flush1m上界"] = [
    ("sub", "if cfg.FlushInterval > traceBodyMaxFlushInterval {", "if false {"),
]

# ---- 同一个上界的**常量**被放宽（删检查之外的另一条路：改大阈值）----
# 杀手用例同上：上界从 1m 放宽到 1h 后，flush=61s/5m + roll=1h 会被接受 ⇒ 转红。
# 两条并存是有意的：m17 证明「检查在位」，m18 证明「阈值确实是 1m」，
# 只测其中一条时另一条可以静默失效（例如把检查留着但把常量改成 2h）。
MUTATIONS["m18_flush1m上界放宽为1h"] = [
    ("sub", "const traceBodyMaxFlushInterval = time.Minute",
            "const traceBodyMaxFlushInterval = time.Hour"),
]

# ---------------------------------------------------------------------------
# 静态检查：**变异测不到**的要求必须有静态检查行（全局约束的证据制度第 4 条）。
# ---------------------------------------------------------------------------
CHECKS = [
    {
        "name": "doc-默认关闭",
        "file": ".env.example",
        "pattern": r"(?m)^# TRACE_BODY_ROOT=",
        "expect": "present",
        "why": (
            "规格 §11.1 与计划全局约束要求「留空 = 关闭」是**默认值**；"
            "变异测不到「默认值是什么」，只能静态检查模板里确实写成了注释态。"
        ),
    },
    {
        "name": "cfg-键在顶层trace_body",
        "file": "internal/gateway/config/config.go",
        "pattern": r'(?m)^\tv\.SetDefault\("trace_body\.root", ""\)$',
        "expect": "present",
        "why": (
            "环境变量名必须逐字等于规格 §11.1 的 TRACE_BODY_ROOT（viper 的 '.' → '_'）。"
            "该键住在 config.go，不在本集合的单文件 TARGET 里 ⇒ 变异覆盖不到，只能静态钉住。"
            "行为侧由 TestLoadWiresTraceBodyFromEnvVerbatim（真 Load + 真 env）覆盖。"
        ),
    },
    {
        "name": "cfg-没有observability-trace_body",
        "file": "internal/gateway/config/config.go",
        "pattern": r"observability\.trace_body",
        "expect": "absent",
        "why": (
            "把键放进 observability.* 会得到 OBSERVABILITY_TRACE_BODY_ROOT，"
            "与规格 §11.1 的部署变量名不符（全局约束明写禁止）。"
        ),
    },
    {
        "name": "DayOf-由DayStart派生",
        "file": "internal/gateway/config/trace_body.go",
        "pattern": r'return c\.DayStart\(ts\)\.Format\("2006-01-02"\)',
        "expect": "present",
        "why": (
            "「唯一日期入口」是**结构**不变量：DayOf 若不是由 DayStart 派生，"
            "只要它自己算对了，行为与用例都看不出差别（两者恒等）⇒ 变异必然假绿。"
            "因此只能静态钉住实现形态。"
        ),
    },
    {
        "name": "test-产品默认值被真实Load断言",
        "file": "tests/gateway/config/trace_body_test.go",
        "pattern": (
            r"(?s)tbDefault := mustLoadTraceBody\(t\)"
            r".*require\.Equal\(t, int64\(1536\), tbDefault\.BlockMB"
            r".*require\.Equal\(t, time\.Minute, tbDefault\.FlushInterval"
            r".*require\.Equal\(t, time\.Hour, tbDefault\.RollInterval"
            r".*require\.Equal\(t, 90, tbDefault\.DiskWatermarkPercent"
        ),
        "expect": "present",
        "why": (
            "评审 F1：产品默认值住在 config.go 的 SetDefault（**不在本集合的单文件 TARGET 里**）"
            "⇒ 变异覆盖不到；而 TestTraceBodyConfigDisabledByDefault 断言的默认值取自测试自己的 "
            "helper newTraceViper()，对「产品默认值漂移」没有约束力（block_mb 改成 1024、"
            "水位改成 80 都仍全绿）。因此必须有**打在真实 Load() 结果上**的四条断言；"
            "本行静态钉住它们的存在与位置，行为侧的红由修复报告的 F1 临时漂移探针给出。"
        ),
    },
]

# ---------------------------------------------------------------------------
# 允许的绿：预期**不转红**的条目，每条必须带分类标签并写明依据；未登记的绿 ⇒ 整轮失败。
# 分类（沿用 task-6 集合）：
#   [零覆盖]   行为**存在**但**没有用例约束**它 ⇒ **测试缺陷**
#   [等价变异] 该行在当前校验次序下**冗余/不可达** ⇒ 关于代码本身的发现
#
# 本集合有 2 条 [等价变异]，都来自「更早的守卫已经完全遮蔽它」这一结构事实。
# ---------------------------------------------------------------------------
EXPECTED_GREEN = {
    "m03_删除minAge严格小于校验": (
        "[等价变异] 不可达守卫：紧随其前的 `RollInterval > 1h` 上界（1h）严格强于"
        "`RollInterval >= 3h`（minAge）—— 任何 `>= 3h` 的取值都先被上界拦下，"
        "因此删掉这条检查后**没有任何输入**能改变可观测行为（错误存在性不变）。"
        "保留它是防御性的：上界常量若将来被放宽，它仍是最后一道。"
        "注意 TestTraceBodyConfigRollIntervalMustBeUnderOneHour 里的 3h 断言因此是"
        "**空洞**的（它靠 1h 上界通过），报告已如实登记。"
    ),
    "m16_删除roll_interval正数校验": (
        "[等价变异] 不可达守卫：在 `FlushInterval > 0`（更早的守卫已强制）的前提下，"
        "`RollInterval <= 0` 必然同时满足 `FlushInterval >= RollInterval`"
        "（`flush > 0` 与 `flush < roll <= 0` 矛盾），因此删掉这条检查后"
        "取值 `<= 0` 仍会报错 —— 只有**错误信息**不同，行为与用例判决不变。"
        "隔离探针 TestTraceBodyConfigRollIntervalMustBePositive 删它之前/之后都 PASS"
        "（这正是它被登记为绿的原因，不是测试缺陷）。"
    ),
}

# ---------------------------------------------------------------------------
# 映射表：计划/控制者写的每条要求 → 集合里的对应条目。
# 工具会校验：① 每个 requirement 至少有一个 mutation 或 check；② 引用的名字真实存在；
# ③ 列出没有被任何 requirement 引用的变异（orphan，同样受 git diff 保护）。
# ---------------------------------------------------------------------------
REQUIREMENTS = [
    {
        "plan": "控制者硬性口径 2 / 简报步骤 1：inst ∈ {blue,green}，非法值启动即失败；开启而未设 inst 也失败",
        "mutations": ["m01_inst白名单放宽为非空", "m12_允许开启但inst为空"],
        "checks": [],
        "note": (
            "m12 是补入条目：简报的 10 条里没有覆盖「开启而未设 inst」这条守卫，"
            "而它是控制者硬性口径第 2 条的后半句。"
        ),
    },
    {
        "plan": "简报步骤 1：滚动间隔 ≤ 1h，且严格小于 minAge 3h（规格 §15.1 R8 / §7.2）",
        "mutations": ["m02_删除滚动间隔1h上界", "m03_删除minAge严格小于校验", "m16_删除roll_interval正数校验"],
        "checks": [],
        "note": (
            "m02 断言红；m03 与 m16 是 [等价变异]（不可达守卫，已在 EXPECTED_GREEN 登记理由）。"
            "测试里 3h 的断言在 1h 上界在位时不可能失败，属于意图记录。"
        ),
    },
    {
        "plan": "简报步骤 1 + 规格 §7.1 + 全局约束：flush 周期 ≤ 1m 且远小于滚动间隔",
        "mutations": [
            "m04_删除flush与滚动间隔的关系校验",
            "m15_删除flush_interval正数校验",
            "m17_删除flush1m上界",
            "m18_flush1m上界放宽为1h",
        ],
        "checks": [],
        "note": (
            "四条各由一条**隔离探针**杀死：m04→TestTraceBodyConfigFlushMustBeFarBelowRoll（探针 flush 必须是 1m，"
            "否则会被 m17/m18 守的上界遮蔽）；m15→…MustBePositive；m17/m18→"
            "TestTraceBodyConfigFlushIntervalMustBeAtMostOneMinute（61s/5m 须报错、1m 须接受）。"
            "**评审 F2 的修复**：`≤1m` 原先只写在文档与 .env.example 里、实现没强制，已补上检查+常量+用例。"
        ),
    },
    {
        "plan": "简报步骤 1：水位 1..99（登记 D45）",
        "mutations": ["m05_删除水位范围校验"],
        "checks": [],
        "note": "杀手 = 隔离探针 TestTraceBodyConfigWatermarkRange（-1/0/100/101 全须报错，1/99 须接受）。",
    },
    {
        "plan": "简报步骤 1：block_mb 必须为正（隔离探针补入）",
        "mutations": ["m14_删除block_mb正数校验"],
        "checks": [],
        "note": "【补入】简报的 10 条漏了这条守卫，而文件头的主题是「每一条校验都必须能被测出来」。",
    },
    {
        "plan": "全局约束：dt = 事件 TS 在 UTC+8 下的日期（固定偏移，不用 LoadLocation / time.Local）",
        "mutations": ["m06_dt口径偏移改为UTC0", "m11_DayStart忽略时区"],
        "checks": [],
        "note": (
            "简报给的 m06 变异串在实现里定位到 0 处（DayOf 由 DayStart 派生），"
            "且 time.Local 形态在本机（Asia/Shanghai）是假绿 ⇒ 改成固定偏移 0；"
            "m11 补覆盖调用点。"
        ),
    },
    {
        "plan": "全局约束：DayStart 是唯一日期入口，返回该 UTC+8 日历日的 **UTC 零点**；DayOf 由它派生",
        "mutations": ["m10_DayStart用事件时间而非零点"],
        "checks": ["DayOf-由DayStart派生"],
        "note": (
            "「DayOf 由 DayStart 派生」是结构不变量，行为上不可区分 ⇒ 静态检查行；"
            "m10 只覆盖「必须是 UTC 零点」这一半。"
        ),
    },
    {
        "plan": "全局约束：root 必须绝对路径且 Clean != \"/\"，TrimSpace 后为空 = 关闭；默认关闭",
        "mutations": [
            "m07_root允许相对路径",
            "m08_root允许文件系统根",
            "m09_root不做TrimSpace",
            "m13_Enabled强制为true",
        ],
        "checks": ["doc-默认关闭"],
        "note": (
            "m13 是补入条目：默认关闭是**安全前提**（挂载未到位前不得静默丢正文），"
            "简报只有静态检查、没有行为变异。"
        ),
    },
    {
        "plan": "全局约束：配置读取惯例 —— Viper 键放顶层 trace_body.*（env 名逐字 TRACE_BODY_*），不得进 observability.*",
        "mutations": [],
        "checks": ["cfg-键在顶层trace_body", "cfg-没有observability-trace_body", "test-产品默认值被真实Load断言"],
        "note": (
            "键名与默认值都住在 config.go（不是本集合的单文件 TARGET）⇒ 变异覆盖不到；"
            "行为侧由 TestLoadWiresTraceBodyFromEnvVerbatim 用真实 env 覆盖。"
            "**评审 F1 的修复**：该用例原先只断言「默认关闭」，读不到产品默认值 ⇒"
            "已补 4 条打在 Load() 结果上的默认值断言（block_mb/flush/roll/watermark），"
            "并由本表的静态检查行钉住它们的存在与位置。"
        ),
    },
]
