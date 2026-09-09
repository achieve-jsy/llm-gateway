"""计划 3 / 任务 5 的变异集合（作者编写的产物 ⇒ 必须 git-tracked，见全局约束第 1 条）。

`TARGET` = 本任务新建的 handler 文件 `internal/observability/handler/trace_body_handler.go`；
用例在 `tests/observability/handler/trace_body_handler_test.go`（B 层，`httptest` + 内存 sqlite，
归属 / 可见性都是注入的替身，不连 rbac / Redis / JWT）。

编号一经发布不再改动；匹配一律用字符串（不用行号），行号仅供人读。

## 相对计划（任务 5 步骤 5）的改动（逐条附理由；**没有任何一条被静默丢弃**）

计划的 m01–m05 共 5 条，本集合逐条采用；**其中三条的「替换串」被订正**（目标串、变异名与
语义一字未动），因为计划草稿的替换串在本实现上会**编译红**，而全局约束第 5 条要求
「变异必须是断言红：编译失败不算通过」（同计划 2 任务 2 的 **E10** 处置形态）：

- **m01**：草稿把 `if !found {` 换成 `if false {`。但在本实现里 `found` **只**被这个条件引用
  ⇒ 替换后 Go 报 `declared and not used: found` ⇒ 编译红。订正为 `if !found && false {`
  （`found` 仍被引用 ⇒ 可编译；分支永不执行 ⇒ 归属校验被摘掉，语义不变）。
- **m02**：同理，草稿的 `if false {` 会让 `userID` / `keyType` 只剩这一处引用 ⇒ 一并
  `declared and not used`。订正为「原条件 && false」（条件里的变量仍被引用 ⇒ 可编译；
  分支永不执行 ⇒ 403 守卫被摘掉）。
- **m04**：草稿的替换串 `c.JSON(http.StatusOK, gin.H{"body": json.RawMessage(result.Body)})`
  需要 handler 文件引入 `encoding/json`；而该文件的**设计约束**就是「不序列化、不包壳」，
  没有任何合法用点 —— 为了跑通这条变异去加一个空引用（`import _ "encoding/json"`）是
  为工具而生的代码。订正为**无需新增 import** 的包壳写法（用 `append` 拼出 `{"body":<原字节>}`），
  语义完全一致：成功响应被包一层 ⇒ 不再是正文原字节。
- **m03 / m05**：计划草稿的替换串**逐字采用**（`if false {` 时 `requestID` 仍被 `return` 引用
  ⇒ 可编译；`status, resp := 500, TraceBodyErrorResponse{}` 也直接可编译）。

计划原稿的 **m06（权限门被放宽）** 按计划自己的裁定**不在 MUTATIONS 里**：该目标在
`internal/controlplane/router/router.go`，而本集合 TARGET 是 handler 文件 ⇒ 必然 PATTERN-MISS；
且仓库不存在任何 `router.Setup` 的测试（`grep -rln "router.Setup" tests/` ⇒ 0 命中），
没有可点名的**顶层测试函数**。它降级为下面的 CHECKS 行 `点读端点权限门未放宽`，
并在 `REQUIREMENTS` 里如实登记为「只有静态证据」。

## 静态检查的自查（修复轮的教训，E21 ③）

`不新建鉴权中间件` 是**不存在性**检查、扫**整个文件文本（含注释）**：实现里任何一句
提到被禁字样（例如注释里写「不解析 Authorization」）都会让它**恒 FAIL**，而工具在跑变异
**之前**就会以「静态检查失败」终止（整轮零证据）。⇒ 本集合落地前已在干净树上单独跑过
`print_checks`，四行全 PASS（见本轮权威表 `静态检查: 全部 PASS`）。
"""

NAME = "p3-task-5"
TARGET = "internal/observability/handler/trace_body_handler.go"
PACKAGES = ["./tests/observability/handler/"]
MANIFEST_FILES = ["tests/observability/handler/trace_body_handler_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/observability/handler/trace_body_handler_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 本集合没有需要真实 zstd / 外部进程的用例（全部用替身 reader + 内存 sqlite）⇒ 白名单为空。
# 三断言的第 3 条（全层级 SKIP == 0）因此必须原样成立。
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# m01 归属校验被摘掉 ⇒ 任何人可凭 request_id 读任何人的提示词（IDOR）。
#     杀手：TestTraceBodyHandlerNotFoundForUnknownRequest
#     （该用例的行**确实存在**且服务本可 200 ⇒ 只有归属校验拦得住；摘掉即红在 200 上）
#     替换串按 E10 订正为 `if !found && false {`（草稿的 `if false {` 会让 found 未被使用 ⇒ 编译红）。
MUTATIONS["m01_摘掉归属校验"] = [
    ("sub", "if !found {", "if !found && false {"),
]
# m02 提示词不可见时不再 403，而是继续取正文并返回 200 ⇒ 「无权看」与「正文为空」不可区分。
#     杀手：TestTraceBodyHandlerForbidsWhenPromptNotVisible
#     （该用例同时断言 calls==0 ⇒ 可见性判定必须先于取正文）
#     替换串按 E10 订正为「原条件 && false」（草稿的 `if false {` 会让 userID/keyType 未被使用 ⇒ 编译红）。
MUTATIONS["m02_不可见时返回200空正文"] = [
    ("sub", "if !h.canView(c, userID, keyType == obsmodel.KeyTypeService) {",
            "if !h.canView(c, userID, keyType == obsmodel.KeyTypeService) && false {"),
]
# m03 内部端点漏掉 request_id 的 400 校验 ⇒ 空串被拿去查库，入参错误被伪装成
#     「记录不存在」（404）。杀手：TestTraceBodyHandlerQueryParamMissing
MUTATIONS["m03_缺参数不报400"] = [
    ("sub", "if requestID == \"\" {\n\t\tc.JSON(http.StatusBadRequest,",
            "if false {\n\t\tc.JSON(http.StatusBadRequest,"),
]
# m04 成功响应被包一层 {"body": ...} ⇒ 不再是正文原字节（逐字节契约破裂），
#     且会把服务层结果上的定位键 / 来源字段带进响应。
#     杀手：TestTraceBodyHandlerAdminRoute（逐字节 + Content-Length + 顶层键集合三处断言）
#     替换串按 E10 订正为**不需要新增 import** 的包壳写法（草稿用 json.RawMessage 需要
#     encoding/json，而该文件不得序列化 ⇒ 只能加空引用，编译红）。
MUTATIONS["m04_成功响应包一层"] = [
    ("sub", 'c.Data(http.StatusOK, "application/json", result.Body)',
            'c.Data(http.StatusOK, "application/json", append(append([]byte(`{"body":`), result.Body...), \'}\'))'),
]
# m05 错误映射被绕过（一律 500）⇒ 404/503/504 语义全丢，调用方无法分类。
#     杀手：TestTraceBodyHandlerMapsErrors（每格都断言 status + code + body_missing）
MUTATIONS["m05_错误一律500"] = [
    ("sub", "status, resp := MapTraceBodyError(err)", "status, resp := 500, TraceBodyErrorResponse{}"),
]

# ---------------------------------------------------------------------------
# 静态检查（变异测不到的结构不变量）
# ---------------------------------------------------------------------------
CHECKS = [
    {
        "name": "复用既有鉴权中间件",
        "file": "internal/controlplane/router/router.go",
        "pattern": r"JWTAuth|InternalAuth",
        "expect": "present",
        "why": (
            "规格 §2.2：不新建鉴权机制 —— 管理面端点挂在既有 JWTAuth 组、内部端点挂在既有 "
            "InternalAuth 组。可违反：router.go 里这两个中间件都没了即 FAIL。"
        ),
    },
    {
        "name": "不新建鉴权中间件",
        "file": "internal/observability/handler/trace_body_handler.go",
        "pattern": r"func .*Middleware|Authorization",
        "expect": "absent",
        "why": (
            "鉴权只由既有中间件负责，handler 不解析任何凭据。**不存在性**要求 ⇒ 变异无法证明"
            "「没写」；可违反：本文件出现自建中间件函数或凭据头的字样即 FAIL（含注释）。"
        ),
    },
    {
        "name": "点读端点权限门未放宽",
        "file": "internal/controlplane/router/router.go",
        "pattern": r'adminGroup\.GET\("/trace/:request_id/body", guard\(rbac\.PermUsageViewAll\), tbH\.TraceBody\)',
        "expect": "present",
        "why": (
            "既有 /usage/logs/:request_id/trace 用 guard(rbac.PermUsageViewAll)（router.go:759）；"
            "正文点读不得更宽松。**该要求当前只有静态证据**（仓库无 router.Setup 的测试，"
            "无法产出「摘掉就红」的变异，原稿 m06 因此不在 MUTATIONS 里），报告须逐条列出。"
        ),
    },
    {
        "name": "冻结包GetBody签名未变",
        "file": "internal/tracearchive/reader.go",
        "pattern": r"func \(r \*Reader\) GetBody\(ctx context\.Context, ref Ref\) \(\[\]byte, error\)",
        "expect": "present",
        "why": (
            "本任务的服务由任务 2 注入冻结包 Reader；签名若变则接线失效。整目录零改动的权威"
            "判据是任务 9 的 git diff（D59）。可违反：签名被改即 FAIL。"
        ),
    },
]

# 5 条全部应在原始侧转为断言红（实测见本轮权威表）；本集合无零覆盖条目。
EXPECTED_GREEN = {}

# ---------------------------------------------------------------------------
# 映射表（任务 5 的判据 / 要求 → 集合里的条目）
# ---------------------------------------------------------------------------
REQUIREMENTS = [
    {
        "plan": "管理面端点：JWTAuth + guard(PermUsageViewAll)；**归属判定先于取正文**，他人记录按不存在处理（404，不泄露存在性）",
        "mutations": ["m01_摘掉归属校验"],
        "checks": ["点读端点权限门未放宽"],
        "note": (
            "杀手 = TestTraceBodyHandlerNotFoundForUnknownRequest（行存在 ⇒ 只有归属校验拦得住）"
            "+ fake.calls==0（归属判定必须先于存储访问）。权限门本身只有静态证据。"
        ),
    },
    {
        "plan": "管理面端点：提示词不可见 ⇒ 403（本端点返回的就是提示词本身，无法「部分裁剪」；不得 200 + 空正文）",
        "mutations": ["m02_不可见时返回200空正文"],
        "checks": [],
        "note": (
            "杀手 = TestTraceBodyHandlerForbidsWhenPromptNotVisible（断言 403 + 响应体不含正文"
            "任何字节 + calls==0）。可见性判据本身复用既有 rbac.CanViewPrompt（不在 TARGET 文件内）。"
        ),
    },
    {
        "plan": "内部端点：InternalAuth 负责鉴权；缺 request_id ⇒ 400（入参错误不得伪装成服务故障）",
        "mutations": ["m03_缺参数不报400"],
        "checks": ["复用既有鉴权中间件", "不新建鉴权中间件"],
        "note": (
            "杀手 = TestTraceBodyHandlerQueryParamMissing（400 + calls==0）。内部端点的 token 行为"
            "由 TestTraceBodyHandlerInternalRouteNeedsToken 实测（无/错 token ⇒ 403，对 ⇒ 200），"
            "但中间件不在 TARGET 文件内 ⇒ 鉴权机制本身只有静态证据。"
        ),
    },
    {
        "plan": "200 = 正文 JSON **原字节**（Content-Length 与 body 逐字节等于服务层返回值；不得包壳，否则 Ref/Source 会泄漏进响应）",
        "mutations": ["m04_成功响应包一层"],
        "checks": [],
        "note": (
            "杀手 = TestTraceBodyHandlerAdminRoute，三处**可失败**断言：① Content-Length == "
            "len(正文)；② body 逐字节等于正文；③ 顶层键集合恰好是 request_headers / "
            "request_body / response_body 三键（无 body / ref / source）。**该要求的真正承重"
            "者是这三条断言**，不是静态检查（任务 3 的 `映射层不包装正文结果` 只证明「没抄"
            "那些字样」，见 E23 的效力边界）。"
        ),
    },
    {
        "plan": "错误语义：一律经任务 3 的映射层（MapTraceBodyError / 诊断响应），不另起一套；底层诊断文本不得被替换",
        "mutations": ["m05_错误一律500"],
        "checks": [],
        "note": (
            "杀手 = TestTraceBodyHandlerMapsErrors（5 格：记录缺失 404 / body_missing 404 且 "
            "body_missing=true / 读侧未配置 503 / 未归档 500 且保留底层文本 / 产物损坏 500）。"
            "「不另起一套」由 handler 只调用 MapTraceBodyError 保证（本行即该调用的变异）。"
        ),
    },
    {
        "plan": "冻结包复用点：任务 5 的服务由任务 2 注入的 *ta.Reader 提供正文",
        "mutations": [],
        "checks": ["冻结包GetBody签名未变"],
        "note": (
            "冻结包不可变异（本计划第一硬约束）⇒ **该要求当前只有静态证据**："
            "整目录零改动的权威判据是任务 9 的 `git diff -- internal/tracearchive/`。"
        ),
    },
]
