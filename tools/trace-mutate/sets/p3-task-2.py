"""计划 3 / 任务 2 的变异集合（作者编写的产物 ⇒ 必须 git-tracked，见全局约束第 1 条）。

被变异文件是**读侧点读服务**（`internal/observability/service/trace_body_service.go`）：
PG 查询 → 优先级判定 → 归档取回 / jsonb 回退 → 错误分类。本批变异的主题是
**判定顺序的每一条都必须承重** —— 步骤 0..4 中任意一条被摘掉/放宽/换序，
对应用例必须转红成「断言红」。

编号一经发布不再改动；匹配一律用字符串（不用行号），行号仅供人读。

## 相对计划（任务 2 步骤 5）的三处**如实**改动（逐条附实测理由）

计划给的 MUTATIONS 里有三条不能直接用；三条都按下表改写。**没有任何一条被静默丢弃**：
改写后的每一条仍落在「断言红」，杀手仍是**具体顶层测试函数名**。
（计划另外四条 m01/m03/m05/m06 **逐字保留**；m06 的 `if false {` 实测**可编译**
——它把整个 if-init 一起去掉，因此 `err` 并未留下，见下第 4 条的反例说明。）

1) **m07 的定位串不唯一**。计划给的是

       ("sub", "return nil, err", 'return nil, fmt.Errorf("%w: %s", ErrTraceBodyInconsistent, requestID)')

   但服务文件里裸串 `return nil, err` 出现 **3** 次 —— 构造期两条
   `return nil, errors.New(...)` 也以它起手（实测：`grep -c` 计数 3）⇒ 工具的
   `apply_mutation` 会以 `count=3` 抛 **PATTERN-MISS**（该集合零产出）。
   改为带缩进的精确行（与任务 4 的 m01/m03/m05 同一写法）：

       ("sub", "\t\treturn nil, err\n", ...)

   实测唯一（`\t\treturn nil, err\n` 计数 1）。

2) **m02 的 `cut` 括号失衡 ⇒ 编译红**。计划给的是

       ("cut", "\tif trace.BodyMissing {", "\t}\n\tif len(refs) > 0 {")

   `cut` 删掉 `if trace.BodyMissing {` … 其闭合 `}` … 以及 **`if len(refs) > 0 {` 这个开括号**，
   却把该 if 的**闭合 `}` 留在文件里** ⇒ 括号净少一个开括号，是**语法/编译红**，
   违反全局约束第 5 条（「编译失败不算通过，必须是断言红」）。本实现里该形状的
   两个端点也确实无法同时存在（判定顺序 1 的 `body_missing` 与「部分 NULL 入口」
   在实现上是**合并成一次形态判定**的，见登记 E9/E10）。
   改为「摘掉『body_missing 与定位键矛盾』这一条判据」——正是 m02 要证明的语义
   （实测串是本实现里的 `!allRefs || trace.BodyMissing`）：

       ("sub", "!allRefs || trace.BodyMissing", "!allRefs")

   摘掉之后「body_missing=true 且带定位键」会落到 `ErrBodyMissing`（404 类），
   而用例要求 `ErrTraceBodyInconsistent`（服务层 500）⇒ **断言红**（实测，见报告）。

3) **m04 的替换串让 `src` / `body` 变成未使用变量 ⇒ 编译红**。计划给的是
   `("sub", "return &TraceBodyResult{Body: body, Source: src}, nil", "return s.fromJSONB(trace)")`；
   而生产代码里 `src`（`src := sourceOf(...)`）与 `body` 的**唯一使用**就在这条返回语句里
   ⇒ 替换后是 `declared and not used`（本机实测原始替换串：`go build` 退出 1，
   `trace_body_service.go:199:2: declared and not used: body` +
   `:206:2: declared and not used: src`）。改写为保留两个变量被引用：

       ("sub", "return &TraceBodyResult{Body: body, Source: src}, nil",
               "_, _ = body, src\n\treturn s.fromJSONB(trace)")

   语义不变（仍把「归档原字节」换成「jsonb 重新序列化」）⇒ 杀
   `TestTraceBodyServiceReadsArchiveWhenRefsPresent` 的逐字节/来源断言 ⇒ **断言红**。

4) 【**保留原样**，此处登记一次核对】m06 的原始替换串
   `("sub", "if err := cpconfig.ValidateRef(ref); err != nil {", "if false {")`
   **本集合逐字采用**。它可编译：`if false {` 把整个 if-init 一起去掉，`err` 根本不留下
   （实测 `go build` 退出 0、`go test` 退出 1 且只有
   `TestTraceBodyServiceRejectsIllegalRefBeforeIO` 转红 ⇒ **断言红**）。
   ⚠️ 只有「**保留 init** 而让判据恒假」的形态（例如计划任务 1 的 m05 若写成
   `if err := validateReadDT(ref.DT); false {`）才会踩 `declared and not used: err`
   —— 本机实测该形态报 `./main.go:9:5: declared and not used: err`。两者别混为一谈。

## 相对计划的一处**补入**（不是改写）

计划的任务 2 写明「判定顺序 0..4 逐条都是承重的，**任何一步**被摘掉或换序都必须有变异红」，
但步骤 5 的 7 条变异里**没有步骤 0（记录缺失 ⇒ 透传 `gorm.ErrRecordNotFound`）**这条。
本集合补 **m08**（登记 E11）覆盖它——用静态检查代替不当（该行为是可测的行为，不是结构不变量）。

## 相对计划的一处**用例改写**（不在本集合里，但与本集合的杀手对应）

计划步骤 1 的 `TestTraceBodyServiceRejectsIllegalRefBeforeIO` 要求「行的 dt 值是
`2026-09-24/../..`」，但该列是 `*time.Time`（`type:date`，计划 2 为避免字符串往返有损
而刻意选定的类型）⇒ `.Format("2006-01-02")` **恒产出合法日期**，服务层不可能构造出非法 dt。
该用例改用**可达**的非法向量（inst / part / row），dt 的守卫由任务 1 的配置层用例承担
（登记 E12）。

## 变异测不到、但有静态检查行（CHECKS）

- jsonb 回退的 `omitempty` 三键（JSON tag 是结构不变量，变异测不到）；
- 「服务层不引入协议/HTTP 类型」（**不存在性**断言，变异证明不了「没写」）；
- 「冻结包的哨兵定义未变」（前提条件，且 `internal/tracearchive/` **冻结、不可变异**）；
- 记录缺失的 `%w` 包装（**本集合新增的一行**：计划的 CHECKS 只有 3 行，而「判定顺序 0」
  没有任何变异或静态检查 ⇒ 映射表会出现 GAP。用静态行钉住包装形态是**可违反的**
  —— 去掉 `%w` 或换成固定文案都会命中失败）。
"""

NAME = "p3-task-2"
TARGET = "internal/observability/service/trace_body_service.go"
PACKAGES = ["./tests/observability/service/"]
MANIFEST_FILES = ["tests/observability/service/trace_body_service_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/observability/service/trace_body_service_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 本集合的用例全部用内存 sqlite + 替身 reader：不依赖 zstd/外部二进制/网络/宿主机状态。
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# m01 jsonb 回退被摘掉 ⇒ 「定位键全 NULL + 未标 missing」被当成缺失
#     （最严重：功能关闭 / sink 构造失败 = 静默丢正文）
#     杀手：TestTraceBodyServicePrefersJSONBWhenRefsAreNULLAndNotMissing
MUTATIONS["m01_全NULL不再回退jsonb"] = [
    ("sub", "return s.fromJSONB(trace)",
            'return nil, fmt.Errorf("%w: %s", ta.ErrNotArchived, requestID)'),
]
# m02 「body_missing=true 却带定位键」这条矛盾判据被摘掉 ⇒ 数据写坏被伪装成
#     「正文从未落盘」（404）。替换串按本文件头第 2 条改写（计划原文是编译红）。
#     杀手：TestTraceBodyServiceBodyMissingWinsOverEverything（第二格）
MUTATIONS["m02_body_missing与定位键的矛盾未判"] = [
    ("sub", "!allRefs || trace.BodyMissing", "!allRefs"),
]
# m03 部分 NULL 被静默回退成 jsonb（最阴的一类：数据写坏被伪装成「正文就是空的」）
#     杀手：TestTraceBodyServicePartialRefsAreInconsistent
MUTATIONS["m03_部分NULL静默回退jsonb"] = [
    ("sub", "return nil, fmt.Errorf(\"%w: %s\", ErrTraceBodyInconsistent, requestID)",
            "return s.fromJSONB(trace)"),
]
# m04 归档字节被换成 jsonb 序列化结果 ⇒ 逐字节保证丢失。
#     替换串按本文件头第 3 条改写（计划原文留下未使用变量 ⇒ 编译红）。
#     杀手：TestTraceBodyServiceReadsArchiveWhenRefsPresent（逐字节 + Source 断言）
MUTATIONS["m04_归档结果被重新序列化"] = [
    ("sub", "return &TraceBodyResult{Body: body, Source: src}, nil",
            "_, _ = body, src\n\treturn s.fromJSONB(trace)"),
]
# m05 读侧禁用不再报 503，而是回落 jsonb ⇒ 部署错误被伪装成「正文在 jsonb 列」
#     杀手：TestTraceBodyServiceDisabledWithRefsIs503
MUTATIONS["m05_禁用时回落jsonb"] = [
    ("sub", "return nil, fmt.Errorf(\"%w: %s\", ErrTraceBodyDisabled, requestID)",
            "return s.fromJSONB(trace)"),
]
# m06 ValidateRef 的判据被摘掉 ⇒ 畸形定位键会真的去访问存储（拼出半截/越界路径）。
#     【逐字采用计划原文】`if false {` 连 if-init 一起去掉 ⇒ 可编译（本文件头第 4 条已实测）。
#     杀手：TestTraceBodyServiceRejectsIllegalRefBeforeIO（calls==0 断言）
MUTATIONS["m06_不校验定位键"] = [
    ("sub", "if err := cpconfig.ValidateRef(ref); err != nil {", "if false {"),
]
# m07 哨兵被吞掉/替换 ⇒ 上层无法分类（把存储故障伪装成数据写坏）。
#     定位串按本文件头第 1 条改写（裸串在文件里出现 3 次 ⇒ 计划原文是 PATTERN-MISS）。
#     杀手：TestTraceBodyServicePassthroughSentinels
MUTATIONS["m07_吞掉reader哨兵"] = [
    ("sub", "\t\treturn nil, err\n",
            '\t\treturn nil, fmt.Errorf("%w: %s", ErrTraceBodyInconsistent, requestID)\n'),
]
# m08 记录缺失不再透传 gorm.ErrRecordNotFound（换成「正文从未落盘」）⇒ handler 会把
#     「记录不存在」映射成 404 body_missing，前端无法区分「没有这条 trace」与
#     「有这条 trace 但正文写入时失败」。**本集合补入的条目**（登记 E11）：
#     计划把判定顺序 0 也声明为承重，却没给它任何变异。
#     杀手：TestTraceBodyServiceRecordNotFound
MUTATIONS["m08_记录缺失被吞掉"] = [
    ("sub", "return nil, fmt.Errorf(\"tracebody: %s 无对应 call_traces 记录: %w\", requestID, gorm.ErrRecordNotFound)",
            "return nil, ta.ErrBodyMissing"),
]

# ---------------------------------------------------------------------------
# 静态检查（变异测不到的要求）
# ---------------------------------------------------------------------------
CHECKS = [
    # jsonb 回退必须重建计划 2 的三键形状（omitempty 让 NULL 列不出现键）
    {
        "name": "jsonb回退用omitempty三键",
        "file": "internal/observability/service/trace_body_service.go",
        "pattern": r'json:"(request_headers|request_body|response_body),omitempty"',
        "expect": "present",
        "why": (
            "形状必须与计划 2 的 trace_line.go 一致（调用方要用同一套解析代码处理两个来源）；"
            "变异测不到 JSON tag。可违反：去掉 omitempty 就会让 NULL 列输出 "
            "\"request_headers\":null（用例里的 NotContains 同时会红）。"
        ),
    },
    # 服务层必须与协议解耦（否则 B 层无法在无 HTTP 的情况下测判定顺序）
    {
        "name": "服务层不引入协议类型",
        "file": "internal/observability/service/trace_body_service.go",
        "pattern": r"gin\.|net/http",
        "expect": "absent",
        "why": (
            "服务只做「查库 + 判定 + 取正文」，不碰状态码与请求上下文；"
            "这是**不存在性**要求 ⇒ 变异无法证明「没写」，只能静态钉住。"
            "注意本行会命中注释里的 `gin.` 字样 ⇒ 注释里不得出现该标识符。"
        ),
    },
    # 冻结包复用点未被改（**必须指向一个真实存在的具体文件**：工具 print_checks 会直接
    # open(path)，`internal/tracearchive` 是目录 ⇒ IsADirectoryError，整轮零产出）
    {
        "name": "冻结包哨兵语义未变",
        "file": "internal/tracearchive/errors.go",
        "pattern": r'ErrNotArchived = errors\.New\("tracearchive: 产物与暂存均不存在"\)',
        "expect": "present",
        "why": (
            "服务层「透传冻结包哨兵、不新增哨兵」的前提是哨兵定义不变；"
            "整目录零改动的权威判据是任务 9 的 git diff（D59）。"
        ),
    },
    # 记录缺失必须保留 gorm 哨兵的分类能力（%w 包装）—— 本集合新增的一行，见文件头
    {
        "name": "记录缺失保留gorm哨兵",
        "file": "internal/observability/service/trace_body_service.go",
        "pattern": r'fmt\.Errorf\("tracebody: %s 无对应 call_traces 记录: %w", requestID, gorm\.ErrRecordNotFound\)',
        "expect": "present",
        "why": (
            "判定顺序 0 的可验证形式：用 %w 包装 gorm.ErrRecordNotFound，handler 才能用 "
            "errors.Is 把它映射成 404 trace_not_found（而不是 body_missing）。"
            "可违反：去掉 %w 或换成固定文案都会命中失败。"
        ),
    },
]

# 没有登记的「允许的绿」：8 条全部应在原始侧转为断言红（实测见任务 2 报告）。
EXPECTED_GREEN = {}

# ---------------------------------------------------------------------------
# 映射表（计划任务 2 的判定顺序 0..4 + 形状/解耦/哨兵三条静态要求，逐条对应）
# ---------------------------------------------------------------------------
REQUIREMENTS = [
    {
        "plan": "判定顺序 0：记录缺失 ⇒ 透传 gorm.ErrRecordNotFound（handler 映射 404）",
        "mutations": ["m08_记录缺失被吞掉"],
        "checks": ["记录缺失保留gorm哨兵"],
        "note": "杀手 = TestTraceBodyServiceRecordNotFound。m08 为本集合补入（计划的 7 条里没有步骤 0，登记 E11）。",
    },
    {
        "plan": "判定顺序 1：body_missing=true ⇒ ErrBodyMissing（先于任何存储访问）",
        "mutations": ["m02_body_missing与定位键的矛盾未判"],
        "checks": [],
        "note": "杀手 = TestTraceBodyServiceBodyMissingWinsOverEverything 第一格。",
    },
    {
        "plan": "判定顺序 1：body_missing=true 却带定位键 ⇒ 服务层 500，绝不报 404",
        "mutations": ["m02_body_missing与定位键的矛盾未判"],
        "checks": [],
        "note": "杀手 = 同用例第二格（全局约束「四者之外的形态 ⇒ 500 + 明确诊断，绝不报 404」）。",
    },
    {
        "plan": "判定顺序 2：四个定位键全 NULL ⇒ 200 + jsonb 回退（Source=jsonb）",
        "mutations": ["m01_全NULL不再回退jsonb"],
        "checks": [],
        "note": "杀手 = TestTraceBodyServicePrefersJSONBWhenRefsAreNULLAndNotMissing（禁用/启用两格）。",
    },
    {
        "plan": "判定顺序 3a：ValidateRef 先于任何存储访问（非法列值 ⇒ 服务层不一致，calls==0）",
        "mutations": ["m06_不校验定位键"],
        "checks": [],
        "note": "杀手 = TestTraceBodyServiceRejectsIllegalRefBeforeIO；隔离探针：摘掉后部分 NULL 用例仍绿。",
    },
    {
        "plan": "判定顺序 3b：读侧禁用 + 带定位键 ⇒ ErrTraceBodyDisabled（503，不 jsonb 不回退）",
        "mutations": ["m05_禁用时回落jsonb"],
        "checks": [],
        "note": "杀手 = TestTraceBodyServiceDisabledWithRefsIs503。",
    },
    {
        "plan": "判定顺序 3c：归档路径逐字节返回 + 冻结包哨兵原样透传",
        "mutations": ["m04_归档结果被重新序列化", "m07_吞掉reader哨兵"],
        "checks": ["冻结包哨兵语义未变"],
        "note": "杀手 = TestTraceBodyServiceReadsArchiveWhenRefsPresent（逐字节/Source）与 TestTraceBodyServicePassthroughSentinels（四类哨兵）。",
    },
    {
        "plan": "判定顺序 4：定位键部分 NULL（14 种非空真子集）⇒ 服务层不一致，绝不 jsonb 不回退",
        "mutations": ["m03_部分NULL静默回退jsonb"],
        "checks": [],
        "note": "杀手 = TestTraceBodyServicePartialRefsAreInconsistent（14 格 + calls==0）。",
    },
    {
        "plan": "jsonb 回退的响应形状（三键 + omitempty，与计划 2 的 trace_line.go 一致）",
        "mutations": [],
        "checks": ["jsonb回退用omitempty三键"],
        "note": "JSON tag 是结构不变量 ⇒ 只有静态证据；用例另用 NotContains 钉住「NULL 列不出现键」。",
    },
    {
        "plan": "服务层与协议解耦（不引入状态码/请求上下文）",
        "mutations": [],
        "checks": ["服务层不引入协议类型"],
        "note": "不存在性要求 ⇒ 只有静态证据。",
    },
]
