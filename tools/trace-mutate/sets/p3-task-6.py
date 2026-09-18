"""计划 3 / 任务 6 的变异集合（作者编写的产物 ⇒ 必须 git-tracked，见全局约束第 1 条）。

`TARGET` = `internal/observability/handler/usage_handler.go`（既有 trace 端点的正文来源替换）。
用例在 `tests/observability/handler/usage_trace_archive_test.go`（B 层，`httptest` 风格 +
内存 sqlite；点读服务与归档原语都是替身；不连 Redis / PG / 真实 rbac 后端）。

编号一经发布不再改动；匹配一律用字符串（不用行号），行号仅供人读。

## 相对计划（任务 6 步骤 5）的改动（逐条附理由；**没有任何一条被静默丢弃**）

计划草稿有 m01–m06 共 6 条。本集合**逐条采用其语义**，但有四条需要订正**机制**（否则会
PATTERN-MISS / 编译红 / 恒绿），理由与 **E10**、任务 5 集合同形（全局约束第 5 条：变异必须是
**断言红**）；另**补入 m07**（E32 的覆盖缺口，见下）。

- **m01**：草稿的替换串是 `("sub", "if err := h.hydrateTraceBody(c.Request.Context(), &trace); err != nil {", ...)`。
  本实现把「回填 + 裁剪」抽成**唯一出口** `hydrateThenMaskTrace`（Trace / MyTrace / ScopedTrace
  共用），其中的 hydrate 调用是 `h.hydrateTraceBody(c.Request.Context(), trace)`（`trace` 已是
  `*obsmodel.CallTrace` ⇒ 没有 `&`）。**目标串按实现订正**（仍是「摘掉回填调用」这一语义；
  替换串 `if err := error(nil); err != nil {` 保留 `err` 被引用 ⇒ 可编译）。
  为什么必须是**唯一出口**：草稿的两处内联调用会让同一条目标串在文件里出现 **2 次**
  ⇒ `count=2` ⇒ PATTERN-MISS（整轮零产出）。（对应 E10 ① 的形态。）
- **m02**：草稿写 `if !traceHasArchiveRef(&trace) {`；实现里参数已是 `*obsmodel.CallTrace`
  ⇒ 实际目标是 `if !traceHasArchiveRef(trace) {`。语义不变（无条件回填）。
- **m03**：草稿写 `trace.BodyMissing = true` ⇒ `trace.BodyMissing = trace.BodyMissing`，目标串逐字存在，
  **逐字保留**。
- **m04**：草稿写 `slog.Warn("trace body hydrate failed"` ⇒ `slog.Debug(...)`，**逐字保留**。
- **m05**：草稿用 **`cut`**（从 hydrate 的 `if err := ...` 删到 `MaskTracePrompt(...)` 结尾）。
  实测该 cut 会把**两条语句一起删掉**（回填与裁剪都没了）⇒ 归档正文根本不会被取回，
  RBAC 用例的「不得出现」断言**仍绿**（恒绿，不是断言红）。**订正为等价的 `sub` 交换**：
  把「回填 → 裁剪」两行换成「裁剪 → 回填」（正是草稿注释里写的语义「回填被挪到裁剪之后」）
  ⇒ 归档提示词越过裁剪，`TestUsageTracePreservesRBACOnArchivedBody` 断言红。
- **m06**：草稿的目标是 `if !inScope {`。**该行在既有 `ScopedTrace` 里不存在** —— 范围校验
  不是一条独立 `if`，而是 GORM 的 `Scopes(usagePol.QueryScope(actor))`（越权行在 SQL 层就
  不命中 ⇒ `First(&lg)` 报 not found ⇒ 404）。因此把目标订正为**范围判定本身**：
  `Scopes(usagePol.QueryScope(actor)).` 换成 `Scopes(usagePol.QueryScope(rbac.Actor{IsGlobal: true})).`
  ⇒ 范围外的行被当成范围内（语义 =「范围外也取正文」），
  `TestScopedTracePreservesScopeOnArchivedBody` 断言红。
- **m07（本集合补入，计划草稿没有；登记 E32）**：`LookupTraceSubject` 的 fail-closed 判据
  `if row.UserID == "" {` ⇒ `if false {`。任务 5 只把该判据抽成共用函数，**零用例零变异**
  （`grep -rn "traceSubject|LookupTraceSubject|\\.Trace(|ScopedTrace" tests/` ⇒ 0 命中）；
  E32 要求任务 6 至少补一条能失败的行为用例、并在集合纪律允许时补一条变异。该目标就在
  本集合的 `TARGET` 文件内 ⇒ 落在目标文件里，不构成新集合。步骤 6 的预期由 **6 条改为 7 条**
  断言红。

## 静态检查的自查（修复轮的教训，E21 ③ / E24）

`回填在裁剪之前` 与 `模型未新增定位键泄漏字段` 都是对**整个文件文本（含注释）**做
`re.search`：前者 `present`（注释里出现同形文本会误判为通过 ⇒ 已把锚点写成**真实代码
语句**而非描述），后者 `absent` ⇒ `call_trace.go` 里任何一句提到被禁字样都会恒 FAIL，
而工具在跑变异**之前**就会以「静态检查失败」终止（整轮零证据）。⇒ 本集合落地前已在
干净树上单独跑过 `print_checks`，五行全 PASS（见本轮权威表「静态检查: 全部 PASS」）。
"""

NAME = "p3-task-6"
TARGET = "internal/observability/handler/usage_handler.go"
PACKAGES = ["./tests/observability/handler/"]
MANIFEST_FILES = ["tests/observability/handler/usage_trace_archive_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/observability/handler/usage_trace_archive_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 本集合没有需要真实 zstd / 外部进程的用例（点读服务与归档原语都是替身，sqlite 是内存库）
# ⇒ 白名单为空：三断言的第 3 条（全层级 SKIP == 0）必须原样成立。
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# m01 归档回填被摘掉 ⇒ 写侧功能开启后（jsonb 恒 NULL）既有端点对所有新 trace 显示空正文。
#     杀手：TestUsageTraceHydratesBodyFromArchive
#     （该用例的行是归档形态，且替身服务**恰被调用一次**；摘掉回填 ⇒ 正文三键全缺 ⇒ 红）
#     目标串按实现订正：hydrate 调用在唯一出口 hydrateThenMaskTrace 里，参数是 `trace`（无 &）。
MUTATIONS["m01_不回填归档正文"] = [
    ("sub", "if err := h.hydrateTraceBody(c.Request.Context(), trace); err != nil {",
            "if err := error(nil); err != nil {"),
]
# m02 无条件回填（摘掉「3 列全 NULL + 4 键全非 NULL」的形态判据）⇒ 功能关闭 / 历史行也去调服务，
#     把 jsonb 里的正文用归档结果覆盖（两路内容不同 ⇒ 内容比对可判），并多打一次服务调用。
#     杀手：TestUsageTraceKeepsJSONBWhenNoRefs
#     （该用例断言替身服务 calls==0 且正文三键仍是 jsonb 原值 —— 隔离探针形式）
MUTATIONS["m02_无条件回填"] = [
    ("sub", "if !traceHasArchiveRef(trace) {", "if false {"),
]
# m03 失败时不再下发既有的 body_missing 标志 ⇒ 归档形态下「正文从未落盘」与「正文为空」不可区分。
#     杀手：TestUsageTraceMarksBodyMissing
#     （行本身 body_missing=false + 替身服务报 ErrBodyMissing ⇒ 只有该赋值能让响应出现 true；
#      若种子行本身就是 true，断言会退化成恒真 —— 用例注释已写明这一点）
MUTATIONS["m03_不下发body_missing标志"] = [
    ("sub", "trace.BodyMissing = true", "trace.BodyMissing = trace.BodyMissing"),
]
# m04 回填失败不再留下响亮日志（Warn → Debug）⇒ 唯一的可观测信号被默认级别过滤掉。
#     杀手：TestUsageTraceArchiveFailureKeeps200
#     （该用例捕获 slog 到内存 buffer 并断言 level=WARN + request_id + 定位键；
#      默认级别为 Info ⇒ Debug 记录被丢弃 ⇒ 红。用例注释写明**不得**把捕获级别写成 Debug。）
MUTATIONS["m04_失败不记日志"] = [
    ("sub", 'slog.Warn("trace body hydrate failed"', 'slog.Debug("trace body hydrate failed"'),
]
# m05 回填被挪到裁剪之后 ⇒ 归档提示词在「先裁后填」的顺序下越过 rbac.CanViewPrompt 的可见性判定。
#     杀手：TestUsageTracePreservesRBACOnArchivedBody
#     【相对计划的订正】草稿用 cut（会把回填与裁剪两条语句一起删掉 ⇒ 归档正文压根没取回、
#     「不得出现」断言恒绿）；这里改为等价的**交换**（sub），语义与草稿注释逐字一致。
MUTATIONS["m05_回填绕过裁剪"] = [
    ("sub",
     "\tif err := h.hydrateTraceBody(c.Request.Context(), trace); err != nil {\n"
     "\t\twarnHydrateTraceBodyFailed(trace, err)\n"
     "\t}\n"
     "\tMaskTracePrompt(trace, canViewPrompt)",
     "\tMaskTracePrompt(trace, canViewPrompt)\n"
     "\tif err := h.hydrateTraceBody(c.Request.Context(), trace); err != nil {\n"
     "\t\twarnHydrateTraceBodyFailed(trace, err)\n"
     "\t}"),
]
# m06 范围判定被放宽成「全局 actor」⇒ 范围外的行也被当成范围内并去取正文（越权 + 无谓 IO）。
#     杀手：TestScopedTracePreservesScopeOnArchivedBody
#     【相对计划的订正】草稿目标 `if !inScope {` 在既有 ScopedTrace 里**不存在**：范围校验是
#     GORM 的 Scopes(usagePol.QueryScope(actor))（越权行在 SQL 层不命中 ⇒ First(&lg) 报
#     not found ⇒ 404）⇒ 改为把该 Scope 的 actor 换成 IsGlobal（= 摘掉范围限制）。
#     替换串**必须继续引用 `actor`**：该变量在 ScopedTrace 里只有这一处引用，写成
#     `rbac.Actor{IsGlobal: true}` 会得到 `declared and not used: actor` ⇒ 编译红
#     （E10 ① 的同形陷阱）。因此带上 `UserID: actor.UserID` —— 引用保留、语义不变（IsGlobal
#     下 QueryScope 直接返回 db，UserID 不参与过滤）。
MUTATIONS["m06_范围外也取正文"] = [
    ("sub", "Scopes(usagePol.QueryScope(actor)).\n\t\tFirst(&lg).Error",
            "Scopes(usagePol.QueryScope(rbac.Actor{IsGlobal: true, UserID: actor.UserID})).\n\t\tFirst(&lg).Error"),
]
# m07 【E32 补入】归属 fail-closed 的「user_id 为空 ⇒ 按查不到处理」判据被摘掉 ⇒ 归属未知的
#     历史行 / 无 call_logs 行都被当成「归属为空串的合法记录」继续走下去（既有端点在有全局
#     actor 时转为 200，点读端点也会去取正文）。杀手：TestLookupTraceSubjectRejectsUnknownOrEmptyOwner
MUTATIONS["m07_归属为空不再fail_closed"] = [
    ("sub", 'if row.UserID == "" {', "if false {"),
]

# ---------------------------------------------------------------------------
# 静态检查（变异测不到的结构不变量）
# ---------------------------------------------------------------------------
CHECKS = [
    {
        "name": "回填在裁剪之前",
        "file": "internal/observability/handler/usage_handler.go",
        # 【相对计划的订正】草稿写 `&trace`；实现里唯一出口的参数已是 *obsmodel.CallTrace
        # ⇒ 锚点改为真实代码语句（同时避开「注释里出现同形描述」的误判）。
        "pattern": r"if err := h\.hydrateTraceBody\(c\.Request\.Context\(\), trace\); err != nil \{[\s\S]{0,200}?\n\tMaskTracePrompt\(trace, canViewPrompt\)",
        "expect": "present",
        "why": (
            "hydrate 必须在 MaskTracePrompt 之前，否则归档正文绕过提示词裁剪（越权）。"
            "**该要求当前只有静态证据**（顺序是结构事实，m05 只证明「换序会红」，"
            "不能证明「顺序未被换」——除非再跑一次 m05）。可违反：两条语句被交换"
            "（去掉 hydrate 那条 `if` 或把它挪到 MaskTracePrompt 之后）即 FAIL。"
        ),
    },
    {
        "name": "不改NewUsageHandler签名",
        "file": "internal/observability/handler/usage_handler.go",
        "pattern": r"func NewUsageHandler\(svc \*service\.RollupService, db \.\.\.\*gorm\.DB\) \*UsageHandler",
        "expect": "present",
        "why": (
            "既有 2 处调用点（router.go 的管理面用量组与 rollup 组）不得被波及；"
            "新增依赖一律走 SetTraceBodyService setter。可违反：签名被改（加参数 / 换返回）即 FAIL。"
        ),
    },
    {
        "name": "冻结包GetBody签名未变",
        "file": "internal/tracearchive/reader.go",
        "pattern": r"func \(r \*Reader\) GetBody\(ctx context\.Context, ref Ref\) \(\[\]byte, error\)",
        "expect": "present",
        "why": (
            "回填经 TraceBodyService 调用冻结包 Reader；签名若变则接线失效。"
            "整目录零改动的权威判据是任务 9 的 git diff（D59）。"
        ),
    },
    {
        "name": "trace响应仍是模型整体",
        "file": "internal/observability/handler/usage_handler.go",
        "pattern": r"c\.JSON\(http\.StatusOK, trace\)",
        "expect": "present",
        "why": (
            "E19：200 的响应体是 `obsmodel.CallTrace` 本身（既有形态），**不得**换成包壳、"
            "也不得把 res.Ref / res.Source 并进去。可违反：改成 `gin.H{\"trace\": trace}` "
            "或任何包装结构即 FAIL（两处返回点共用本行，命中一次即算通过）。"
        ),
    },
    {
        "name": "模型未新增定位键泄漏字段",
        "file": "internal/observability/model/call_trace.go",
        "pattern": r"tracearchive|json:\"(ref|source|body|trace)\"",
        "expect": "absent",
        "why": (
            "E19：定位键的唯一合法来源是四个**既有**的 `body_ref_*` 列（json tag 早已存在"
            "且带 omitempty）；模型**不得**引用冻结包定位键类型，也不得新增 `ref`/`source` "
            "之类的 json 键 —— 否则 `TraceBodyResult.Ref` 会经模型泄漏进既有 trace 端点的响应。"
            "可违反：加 `Ref *ta.Ref `json:\"ref,omitempty\"`` 或任何新 json 键即 FAIL。"
            "**不存在性**要求 ⇒ 变异无法证明「没写」；承重的是 "
            "TestUsageTraceResponseKeySetUnchanged 的完整键列表断言。"
        ),
    },
]

# 7 条全部应在原始侧转为断言红（实测见本轮权威表）；本集合无零覆盖条目。
EXPECTED_GREEN = {}

# ---------------------------------------------------------------------------
# 映射表（任务 6 的判据 / 要求 → 集合里的条目）
# ---------------------------------------------------------------------------
REQUIREMENTS = [
    {
        "plan": "归档形态（三个 jsonb 列全 NULL + 四个定位键全非 NULL）⇒ 正文改由点读服务提供，回填进返回模型的 RequestBody / ResponseBody / RequestHeaders",
        "mutations": ["m01_不回填归档正文"],
        "checks": [],
        "note": (
            "杀手 = TestUsageTraceHydratesBodyFromArchive（替身服务返回固定三键 ⇒ 响应三字段"
            "逐字等于归档三键 + calls==1）。回填是**取值替换**，不引入任何新响应键。"
        ),
    },
    {
        "plan": "功能关闭 / 历史行（定位键全 NULL）⇒ 正文仍来自 jsonb，**不得**调用点读服务",
        "mutations": ["m02_无条件回填"],
        "checks": [],
        "note": (
            "杀手 = TestUsageTraceKeepsJSONBWhenNoRefs（断言替身服务 calls==0 + 三键仍是 jsonb 原值）。"
            "**调用次数是隔离探针**：只做内容比对时两路内容等价会让断言退化成恒真。"
        ),
    },
    {
        "plan": "ErrBodyMissing ⇒ 正文留空 + 把**既有** `body_missing` 字段置真（不是新增字段）",
        "mutations": ["m03_不下发body_missing标志"],
        "checks": [],
        "note": (
            "杀手 = TestUsageTraceMarksBodyMissing（行本身 body_missing=false，只有该赋值能让"
            "响应出现 true）。**覆盖边界（诚实登记）**：真实 `TraceBodyService.GetBody` 只在"
            "「body_missing=true 且四个定位键全 NULL」时返回 ErrBodyMissing，而本任务的回填"
            "触发条件恰是「定位键全非 NULL」⇒ 该分支在**真实服务下不可达**，是用例用替身"
            "服务驱动的一条**防御性**分支；它仍是可失败的（不是恒真断言），但不得被读成"
            "「生产路径已覆盖」。"
        ),
    },
    {
        "plan": "ErrNotArchived / 产物损坏 / 读侧禁用 ⇒ 正文留空 + 200 + **一条响亮的服务端日志**（唯一可观测信号）",
        "mutations": ["m04_失败不记日志"],
        "checks": [],
        "note": (
            "杀手 = TestUsageTraceArchiveFailureKeeps200（200 + 正文空 + 捕获到 level=WARN 且含 "
            "request_id 与定位键的日志）。端点**不区分**「正文缺失」与「存储故障」的取舍已在"
            "计划正文写明；需要精确语义的调用方走任务 5 的 /api/v1/trace/:request_id/body。"
        ),
    },
    {
        "plan": "裁剪（rbac.CanViewPrompt）必须作用于**新来源**：归档提示词不得越过可见性判定",
        "mutations": ["m05_回填绕过裁剪"],
        "checks": ["回填在裁剪之前"],
        "note": (
            "杀手 = TestUsageTracePreservesRBACOnArchivedBody（canViewPrompt=false ⇒ 响应不含"
            "归档提示词，但 calls==1 证明正文确实被取回后裁掉）。安全承重；顺序的结构事实由"
            "静态检查「回填在裁剪之前」再次钉住。"
        ),
    },
    {
        "plan": "ScopedTrace 的 QueryScope 校验保持：范围外记录 404（不泄露存在性），且**不发生**取正文",
        "mutations": ["m06_范围外也取正文"],
        "checks": [],
        "note": (
            "杀手 = TestScopedTracePreservesScopeOnArchivedBody（范围外 404 + calls==0；"
            "阳性对照范围内 200 + calls==1，否则 404 断言恒真）。"
        ),
    },
    {
        "plan": "归属判据与既有端点**同一套**（复用任务 5 抽出的 LookupTraceSubject），且 fail-closed（查不到 / user_id 为空 ⇒ 按不存在处理）",
        "mutations": ["m07_归属为空不再fail_closed"],
        "checks": [],
        "note": (
            "**E32 的覆盖缺口承接**：杀手 = TestLookupTraceSubjectRejectsUnknownOrEmptyOwner"
            "（两格归属不满足 ⇒ 既有 MyTrace 与点读端点都 404，且两侧都不取正文；"
            "阳性对照归属完整 ⇒ 200 + calls==1）。点读一侧的 subject 注入**生产实现**"
            "（与 router.go 逐字同形），因此这条同时证明「同一套判据」不是两份实现。"
        ),
    },
    {
        "plan": "响应字段集合与改造前逐字相同：不新增定位键泄漏字段、不把 200 换成包壳",
        "mutations": [],
        "checks": ["trace响应仍是模型整体", "模型未新增定位键泄漏字段", "冻结包GetBody签名未变"],
        "note": (
            "**无对应变异**（E19 已写明的理由：泄漏来自设计层改动，且 `c.JSON(http.StatusOK, trace)` "
            "在同一文件出现 2 次 ⇒ 无法做唯一目标的 sub）。真正承重的是可失败用例 "
            "TestUsageTraceResponseKeySetUnchanged（两格固定输入的完整顶层键列表 + 断言不存在 "
            "ref/source/body/trace）；静态检查只证明「没写这些字样」（效力边界）。"
        ),
    },
]
