"""计划 2 / 任务 3 的变异集合（作者编写的产物 ⇒ 必须 git-tracked，见全局约束 §证据制度 1）。

被变异文件是 spool 行的序列化（`internal/gateway/observability/trace_line.go`）。
这一批变异的共同主题：**每一道「不让一个坏字段带走整条记录」的防线都必须能被测出来**。

编号一经发布不再改动；匹配一律用字符串（不用行号），行号仅供人读。

## 相对简报（task-3-brief.md:368-438）的四处**如实**改动

1. **m03 的杀手用例是新增的**。简报声称 `TestTraceEventLineScrubsNUL` 同时盯住
   「请求头」与「请求体」两条清洗路径，但该用例把 NUL 只放进 `RequestBodyRaw` ——
   删掉请求头清洗后它**仍然全绿** ⇒ 照抄简报会得到一条零覆盖的假绿。
   因此新增顶部用例 `TestTraceEventLineScrubsNULInRequestHeaders`（NUL 只放请求头），
   由它作 m03 的杀手。**临时把请求头清洗改坏后该用例确实变红**（见任务报告 §变异证据）。

2. **m02 的变异串按本实现的守卫形态改写，且实现侧做了等价重构。**
   简报给的实现里，`rawOrQuoted` 的**全部**调用点都已由 `json.Valid` 守卫过
   （`scrubNULEscapes` 只把 `\\u0000` 换成 `\\ufffd`，不改变合法性）⇒ 它的降级分支
   在当前实现下**不可达**，m02 会是零覆盖的假绿。本实现把「未校验正文」也导入
   `rawOrQuoted`（`case e.BodyRawUntrusted, json.Valid(...)`），使「非法 ⇒ 降级成字符串」
   只有这一个出口 ⇒ m02 由 `TestTraceEventLineInvalidUntrustedBodyFallsBackToString` 杀死。
   变异串因此是「删掉 rawOrQuoted 里的 json.Valid 判断，直接 `return json.RawMessage(b)`」。
   连带一处：降级/包装的 256 字节截断移入 `rawOrQuoted`（原实现在未校验分支另有截断），
   否则该分支会不再受 256 字节契约约束。

3. **m06 / m07 / m08 / m09 / m10 是补入条目**（简报没有）。简报的 5 条只覆盖了
   ts/降级/两条 NUL/响应体省略，而它自己列出的规则里还有：
   「非法请求体最多 256 字节」、承重的「一次 Append == 一条物理行（行内不得有裸换行）」、
   「请求头仅当 json.Valid 时写入」、「未校验标记决定降级形态」、「键序」。
   R1/R4/R5/R7 因此各补一条能红的条目（每条都有具体顶层杀手用例）。

## 变异测不到、但有静态检查行（CHECKS）

- 「observability 不得依赖 internal/gateway/config」：把 `traceLineZone` 换成
  `config.TraceBodyZone` 后**行为完全一致**（两者都是固定 +08:00）⇒ 任何行为用例都
  无法区分 ⇒ 只能静态钉住 import 形态（简报明写这条约束）。
- 「键序」同时有 A 层用例（m10 可杀）与静态检查：静态行钉住**源码结构**，
  变异行证明该结构在行为上确实被断言。
"""

NAME = "p2-task-3"
TARGET = "internal/gateway/observability/trace_line.go"
# 本任务的用例是 A 层（package observability），必须在本包内跑
PACKAGES = ["./internal/gateway/observability/"]
MANIFEST_FILES = ["internal/gateway/observability/trace_line_internal_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' internal/gateway/observability/trace_line_internal_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 本集合的用例不依赖数据库/外部二进制/时区数据，没有可 SKIP 的理由。
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# ---- 行内 ts 的 +08:00 契约 ----
# 杀手用例 TestTraceEventLineKeyOrderAndTSFormat：UTC 下 ts 变成 "2026-09-24T04:00:00Z"，
# HasSuffix("+08:00") 与逐字断言都会失败 ⇒ 转红。
# 【为什么不是简报的 time.Local 形态】本机 /etc/localtime = Asia/Shanghai ⇒ time.Local 恰为
# +08:00，那种变异是**假绿**；time.UTC 与宿主时区无关，恒可检出（控制者在指令 3 里明确禁用
# time.Local 形态）。
MUTATIONS["m01_ts改用UTC"] = [
    ("sub", "e.TS.In(traceLineZone).Format(traceArchiveTSLayout)",
            "e.TS.In(time.UTC).Format(traceArchiveTSLayout)"),
]

# ---- 非法字段必须降级，绝不让整行失效 ----
# 杀手用例 TestTraceEventLineInvalidUntrustedBodyFallsBackToString：
# rawOrQuoted 不再降级后，非法 RawMessage 会让 json.Marshal 整体失败 ⇒
# 走 `_line_error` 兜底行（没有 request_body 键）⇒ 该用例的「键必须存在」断言转红。
MUTATIONS["m02_非法字段不再降级为字符串"] = [
    ("sub", "if json.Valid(b) {\n\t\treturn json.RawMessage(b)\n\t}\n\treturn quotedJSON(truncateForRaw(b))",
            "return json.RawMessage(b)"),
]

# ---- NUL 清洗 ⇒ 两条路径各一条变异、各有独立杀手 ----
# m03 杀手 = TestTraceEventLineScrubsNULInRequestHeaders（NUL 只在请求头里）。
MUTATIONS["m03_请求头不清洗NUL"] = [
    ("sub", "out.RequestHeaders = rawOrQuoted(scrubNULEscapes(e.RequestHeadersRaw))",
            "out.RequestHeaders = rawOrQuoted(e.RequestHeadersRaw)"),
]
# m04 杀手 = TestTraceEventLineScrubsNUL（NUL 只在请求体里）。
MUTATIONS["m04_请求体不清洗NUL"] = [
    ("sub", "out.RequestBody = rawOrQuoted(scrubNULEscapes(e.RequestBodyRaw))",
            "out.RequestBody = rawOrQuoted(e.RequestBodyRaw)"),
]

# ---- 响应体非法时必须整体省略（与请求体的包装规则不同）----
# 杀手用例 TestTraceEventLineOmitsInvalidResponseBody：去掉 json.Valid 守卫后，
# 非法响应体被 rawOrQuoted 降级成 JSON **字符串** ⇒ 键存在且非 nil ⇒ 转红。
MUTATIONS["m05_非法响应体改为包装"] = [
    ("sub", "if len(e.ResponseBodyRaw) > 0 && json.Valid(e.ResponseBodyRaw) {",
            "if len(e.ResponseBodyRaw) > 0 {"),
]

# ---- 非法请求体包装后的 256 字节上界 ----
# 杀手用例 TestTraceEventLineWrapsInvalidRequestBody：截断失效后 300 字节的输入
# 原样进入 _raw_truncated ⇒ len != 256 ⇒ 转红。
MUTATIONS["m06_非法请求体不再截断"] = [
    ("sub", "if len(b) > traceRawTruncateLen {", "if false {"),
]

# ---- 承重不变量：一次 Append == 一条物理行（行内不得有裸换行）----
# 杀手用例 TestTraceEventLineKeyOrderAndTSFormat 的 `bytes.ContainsAny(line, "\\n\\r")`。
MUTATIONS["m07_行尾补裸换行"] = [
    ("sub", "return buf", "return append(buf, '\\n')"),
]

# ---- 请求头仅当 json.Valid 时写入 ----
# 杀手用例 TestTraceEventLineOmitsInvalidRequestHeaders：去掉守卫后非法请求头
# 以 JSON 字符串形式出现 ⇒ 键存在且非 nil ⇒ 转红。
MUTATIONS["m08_请求头不再校验合法性"] = [
    ("sub", "if len(e.RequestHeadersRaw) > 0 && json.Valid(e.RequestHeadersRaw) {",
            "if len(e.RequestHeadersRaw) > 0 {"),
]

# ---- BodyRawUntrusted 决定「未校验且非法」的降级形态 ----
# 杀手用例 TestTraceEventLineInvalidUntrustedBodyFallsBackToString：
# 摘掉未校验入口后，非法未校验正文落到 default 分支被包成 {"_raw_truncated": ...}
# ⇒ 该用例的「必须降级成字符串（以 " 开头）」断言转红。
MUTATIONS["m09_未校验标记不再区分降级形态"] = [
    ("sub", "case e.BodyRawUntrusted, json.Valid(e.RequestBodyRaw):",
            "case json.Valid(e.RequestBodyRaw):"),
]

# ---- 键序与归档格式一致（对外格式契约）----
# 杀手用例 TestTraceEventLineKeyOrderAndTSFormat：交换两个字段声明后
# JSON 顶层键序变成 ...request_body, request_headers... ⇒ 键序断言转红。
MUTATIONS["m10_键序交换"] = [
    ("sub", "\t\tRequestHeaders json.RawMessage `json:\"request_headers,omitempty\"`\n"
            "\t\tRequestBody    json.RawMessage `json:\"request_body,omitempty\"`",
            "\t\tRequestBody    json.RawMessage `json:\"request_body,omitempty\"`\n"
            "\t\tRequestHeaders json.RawMessage `json:\"request_headers,omitempty\"`"),
]

# ---------------------------------------------------------------------------
# 静态检查：**变异测不到**的要求必须有静态检查行（全局约束的证据制度第 4 条）。
# ---------------------------------------------------------------------------
CHECKS = [
    {
        "name": "键序-与归档格式一致",
        "file": "internal/gateway/observability/trace_line.go",
        "pattern": (r'json:"request_id"[\s\S]*json:"ts"[\s\S]*json:"request_headers"'
                    r'[\s\S]*json:"request_body"[\s\S]*json:"response_body"'
                    r'[\s\S]*json:"error_code"[\s\S]*json:"error_message"'),
        "expect": "present",
        "why": (
            "规格 §4.1 步骤① 的「字段与顺序对齐现有归档格式」是**源码结构**要求；"
            "变异 m10 已证明该结构在行为上被 A 层用例断言（交换即红），"
            "本静态行是第二道：它不依赖任何运行时输入，直接钉住声明顺序。"
        ),
    },
    {
        "name": "impl-不依赖config包",
        "file": "internal/gateway/observability/trace_line.go",
        "pattern": r"internal/gateway/config",
        "expect": "absent",
        "why": (
            "简报明写：observability 不该依赖 internal/gateway/config（依赖方向），"
            "时区由调用方注入。把 traceLineZone 换成 config.TraceBodyZone 后行为完全一致"
            "（都是固定 +08:00）⇒ 任何行为用例都无法区分 ⇒ 只能静态钉住 import 形态。"
        ),
    },
    {
        "name": "test-不依赖config包",
        "file": "internal/gateway/observability/trace_line_internal_test.go",
        "pattern": r"internal/gateway/config",
        "expect": "absent",
        "why": (
            "同上：测试若 import config 就等于把该依赖引进包内（config 是装配层，"
            "本包是纯序列化逻辑）。测试自带的 TraceBodyZoneForTest 是固定 +08:00。"
        ),
    },
]

# ---------------------------------------------------------------------------
# 允许的绿：预期**不转红**的条目，每条必须带分类标签并写明依据；未登记的绿 ⇒ 整轮失败。
# 本集合预期为空 —— 10 条变异各由一条具体顶层用例杀死。
# ---------------------------------------------------------------------------
EXPECTED_GREEN = {}

# ---------------------------------------------------------------------------
# 映射表：计划/简报写的每条要求 → 集合里的对应条目。
# ---------------------------------------------------------------------------
REQUIREMENTS = [
    {
        "plan": "规格 §4.1 步骤① + 简报步骤 3：一整行 JSON、键序与归档格式一致、行内不得有裸换行",
        "mutations": ["m10_键序交换", "m07_行尾补裸换行"],
        "checks": ["键序-与归档格式一致"],
        "note": (
            "m10 杀键序（A 层 Token 流断言是唯一能测键序的手段）；m07 杀「无裸换行」这条"
            "承重不变量（结尾换行形态）。**行内部**的裸换行在 encoding/json 下不可能产生"
            "（字符串里的换行一律转义），属结构保证，已如实登记。"
        ),
    },
    {
        "plan": "简报步骤 3：行内 ts 固定 +08:00（与 /datalog 存量归档格式一致）",
        "mutations": ["m01_ts改用UTC"],
        "checks": [],
        "note": "简报给的 time.Local 形态在本机是假绿（/etc/localtime=Asia/Shanghai），改成固定 UTC 形态。",
    },
    {
        "plan": "简报步骤 3：NUL 清洗规则从写 jsonb 列平移到写 spool 行（PG 拒绝 \\u0000，22P05）",
        "mutations": ["m03_请求头不清洗NUL", "m04_请求体不清洗NUL"],
        "checks": [],
        "note": (
            "两处清洗是两行独立代码 ⇒ 各需一条杀手用例：m03→"
            "TestTraceEventLineScrubsNULInRequestHeaders（**简报没有，为本条新增**："
            "简报指定的 TestTraceEventLineScrubsNUL 只把 NUL 放进请求体，"
            "删掉请求头清洗时它仍全绿）；m04→TestTraceEventLineScrubsNUL。"
        ),
    },
    {
        "plan": "简报步骤 3：非法请求体包装成 {\"_raw_truncated\": ...} 且最多 256 字节，字段不为 null",
        "mutations": ["m06_非法请求体不再截断"],
        "checks": [],
        "note": "杀手用例 TestTraceEventLineWrapsInvalidRequestBody（300 字节 → 必须只剩 256）。",
    },
    {
        "plan": "简报步骤 3：BodyRawUntrusted 未校验路径 —— 合法原样嵌入、非法降级，绝不让整行失效",
        "mutations": ["m02_非法字段不再降级为字符串", "m09_未校验标记不再区分降级形态"],
        "checks": [],
        "note": (
            "两条杀手用例都是 TestTraceEventLineInvalidUntrustedBodyFallsBackToString："
            "m02 让整行退化为 `_line_error` 兜底（request_body 键消失）；"
            "m09 让非法未校验正文被包成 _raw_truncated 对象（不再是字符串）。"
            "**简报给的实现在此处是零覆盖**：它的全部 rawOrQuoted 调用点都已被 json.Valid 守卫，"
            "降级分支不可达（见文件头第 2 条），故实现侧做了等价重构。"
        ),
    },
    {
        "plan": "简报步骤 3：非法响应体整体省略（沿用 trace_event.go:231-233 既有行为，不得改成包装）",
        "mutations": ["m05_非法响应体改为包装"],
        "checks": [],
        "note": "杀手用例 TestTraceEventLineOmitsInvalidResponseBody。",
    },
    {
        "plan": "简报步骤 3：request_headers 仅当 json.Valid 时写入并清洗（沿用 trace_event.go:209-211）",
        "mutations": ["m08_请求头不再校验合法性"],
        "checks": [],
        "note": "杀手用例 TestTraceEventLineOmitsInvalidRequestHeaders（**补入用例**：简报的 7 条只覆盖响应体的省略规则）。",
    },
    {
        "plan": "简报步骤 1 与步骤 3：observability 不得依赖 internal/gateway/config（时区由调用方注入）",
        "mutations": [],
        "checks": ["impl-不依赖config包", "test-不依赖config包"],
        "note": "行为上不可区分（两份时区都是固定 +08:00）⇒ 只能是静态检查。",
    },
]
