"""计划 3 / 任务 7 的变异集合（作者编写的产物 ⇒ 必须 git-tracked，见全局约束第 1 条）。

`TARGET` = `internal/observability/service/trace_body_cli.go`（点读 CLI 的**可导入**部分：
退出码映射 / 参数解析 / 读侧配置装配 / 三种输出形态的写出）。
用例在 `tests/tools/trace_body_cli/trace_body_cli_test.go`（B 层；不 exec 进程、不连库、
不碰文件系统）。

编号一经发布不再改动；匹配一律用字符串（不用行号），行号仅供人读。

## 相对计划（任务 7 步骤 5）的改动（逐条附理由；**没有任何一条被静默丢弃**）

计划草稿有 m01–m05 共 5 条。语义**逐条采用**，其中一条必须订正**机制**（否则不是断言红），
另有一处**命名**随机制一并订正：

- **m01 / m02 / m04 / m05**：草稿的目标串与替换串在实现上**逐字存在**（目标串唯一命中），
  逐字保留，不做任何订正。
- **m03**：草稿用 `panic("missing")` 制造红 ⇒ 那是**崩溃红**，全局约束第 5 条明文
  「编译失败 / panic / 30s 超时都不算通过」，工具的 `classify()` 也把它归入
  「红(崩溃/中止)」而不是「红」⇒ 步骤 6 的「预期 5 条断言红」在字面实现下不可能成立
  （会得到 4 断言红 + 1 崩溃红）。**订正为等价的「摘掉必填项校验」**：
  `if *requestID == "" {` → `if false {`（语句体留在原地，`requestID` 仍在返回结构里被引用
  ⇒ 可编译）。语义与草稿注释一字不变（缺 `--request-id` 不再返回用法错误 ⇒ 退出码不再是 1），
  但红来自用例**断言**：`TestTraceBodyCLIParsesFlags` 的「缺 request-id ⇒ 期望 error」那一格
  得到 `nil` error。变异名同步订正为 `m03_缺requestid不再报错`（原名带 `panic`，会把读者
  引向已被订正的机制）。本条的登记见计划文末 **E40**。**变异条数不变（仍 5 条）。**

## 镜像的静态检查（E41 的落点裁定）

任务的 5 处 CHECKS 目标在 `cmd/trace-body/main.go`。但 m05 的目标串（那句 `fmt.Fprintf`）
必须落在集合的 `TARGET` 文件里（否则工具在跑变异之前就以 `PATTERN-MISS` 终止，整轮零证据）
⇒ **ref 格式化必然被抽进 `internal/observability/service/trace_body_cli.go`**，从而触发计划
Task 7 的镜像要求（`--ref` 条逐字：「若执行者把 ref 格式化抽进 …，必须把下面两条 CHECKS 行
镜像到该文件」；E18 更强：「本页前三行都必须镜像」）。**实际镜像第 1、3 条**（禁用标识符 /
`.Ref`），**第 2 条（`NewTraceBodyService(...)` 的服务构造调用）留在 `main.go`**：它的落点是**服务构造点**，
而任务 7 的正向钉住要求该构造点出现在 `cmd/trace-body/main.go`；把它镜像到 service 文件
只能靠在那里再写一个**无调用者**的构造调用 ⇒ 死代码 / 重复构造。裁定与理由见计划文末
**E41**。⇒ 产物里的镜像行是 `CHECKS` 的第 7、8 条（名字带「CLI逻辑文件」前缀），
其余 6 条是草稿的逐字采用。

## 覆盖边界（诚实登记，不是缺口掩盖）

- **`--json` 无对应变异**：草稿的 5 条不含它，本集合**不新增**（保持计划的 5 条与步骤 6 的
  「5 条断言红」口径）。它的行为证据是 `TestTraceBodyCLIJSONSummaryMatchesServiceResult`
  （source / bytes / sha256 全部由服务返回的正文算出 + 不得夹带正文）。⇒ 该用例的守卫
  **只有行为证据、没有变异承重**，不得读成「已由变异覆盖」。
- **退出码 2 的语义**：计划只写「2 = 其它错误」，**未定义**「`request_id` 查不到记录」该落在
  哪一码。本实现的裁定是 **2**（记录不存在不是「正文缺失」——那是 `body_missing` 的专属语义，
  也不是「需要人工介入的存储故障」），由 `TestTraceBodyCLIExitCodes` 的「记录不存在」一格
  钉住。这是本计划的一处歧义，登记在任务 7 报告里。

## 静态检查的自查（修复轮的教训，E21 ③ / E24 / E19）

第 1、7 条是**不存在性**检查，对**整个文件文本（含注释）**做 `re.search`：`main.go` /
`trace_body_cli.go` 里任何一句（哪怕是解释「为什么不用某写法」的注释）提到被禁标识符都会
恒 FAIL，而工具在跑变异**之前**就以「静态检查失败」终止（整轮零证据）。⇒ 两个文件的注释
一律写成「trace 表」「定位键」「冻结包」等描述性说法，不写被禁的列名 / 模型字段 / 包限定符；
本集合落地前已在干净树上单独跑过 `print_checks`，8 行全 PASS（见本轮权威表「静态检查」段）。
"""

NAME = "p3-task-7"
TARGET = "internal/observability/service/trace_body_cli.go"
PACKAGES = ["./tests/tools/trace_body_cli/"]
MANIFEST_FILES = ["tests/tools/trace_body_cli/trace_body_cli_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/tools/trace_body_cli/trace_body_cli_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 本集合的用例不依赖真实 zstd / 外部进程 / 真实库（不 exec 进程、不连库、不碰文件系统）
# ⇒ 白名单为空：三断言的第 3 条（全层级 SKIP == 0）必须原样成立。
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# m01 正文缺失（3）与未归档（4）的退出码被合并 ⇒ 排障无法区分「写入时即失败、从未落盘」
#     与「元数据指向归档但存储取不回来」，两者的处置完全不同。
#     杀手：TestTraceBodyCLIExitCodes
#     （该用例断言 ErrBodyMissing ⇒ 3；m01 后得到 4 ⇒ 红）
MUTATIONS["m01_合并缺失与未归档退出码"] = [
    ("sub", "if errors.Is(err, ta.ErrBodyMissing) {\n\t\treturn 3\n\t}", "if false {\n\t\treturn 3\n\t}"),
]
# m02 读侧未配置不再有独立退出码 ⇒ 部署错误（TRACE_BODY_ROOT 没配）被报成存储错误，
#     把运维引向查 spool / 产物目录的死路。
#     杀手：TestTraceBodyCLIExitCodes
#     （该用例断言 ErrTraceBodyDisabled（含被包装形态）⇒ 5；m02 后得到 2 ⇒ 红）
MUTATIONS["m02_禁用退出码合并"] = [
    ("sub", "if errors.Is(err, ErrTraceBodyDisabled) {\n\t\treturn 5\n\t}", "if false {\n\t\treturn 5\n\t}"),
]
# m03 必填项校验被摘掉 ⇒ 缺 --request-id 时不再返回用法错误（退出码不再是 1），
#     CLI 会带着空 request_id 去查库（把「参数错」伪装成「查不到记录」）。
#     杀手：TestTraceBodyCLIParsesFlags
#     （该用例的「缺request-id」一格期望 error；m03 后得到 nil error ⇒ 红）
#     【相对计划的订正（E40）】草稿用 panic 制造红 ⇒ 崩溃红不算断言红；机制改为
#     「摘掉必填项校验」（`if *requestID == "" {` → `if false {`），语义不变、红仍是断言红。
MUTATIONS["m03_缺requestid不再报错"] = [
    ("sub", 'if *requestID == "" {', "if false {"),
]
# m04 --raw 多写一个换行 ⇒ 管道给 jq / sha256sum 的下游全部偏一位（逐字节契约破裂）。
#     杀手：TestTraceBodyCLIRawWritesExactBytes
#     （该用例断言 stdout 逐字节等于 Body，并断言字节数 == len(Body)；m04 后多 1 字节 ⇒ 红）
MUTATIONS["m04_raw多写换行"] = [
    ("sub", "w.Write(body)", "w.Write(append(body, '\\n'))"),
]
# m05 --ref 少输出一个分量（行号 row）⇒ 运维拿着 dt/inst/part 也定位不到具体那一行，
#     而排障恰恰要靠行号在有内嵌索引的产物里定位。
#     杀手：TestTraceBodyCLIRefModePrintsAllFourComponents
#     （该用例断言输出逐字等于 `<dt>/<inst>/part-%06d#row=%d`；m05 后缺 `#row=42` ⇒ 红）
MUTATIONS["m05_ref少输出分量"] = [
    ("sub", 'fmt.Fprintf(w, "%s/%s/part-%06d#row=%d\\n", ref.DT, ref.Inst, ref.Part, ref.Row)',
            'fmt.Fprintf(w, "%s/%s/part-%06d\\n", ref.DT, ref.Inst, ref.Part)'),
]

# ---------------------------------------------------------------------------
# 静态检查（变异测不到的结构不变量）
# ---------------------------------------------------------------------------
CHECKS = [
    {
        "name": "CLI复用服务不自己拼路径也不直查定位键列",
        "file": "cmd/trace-body/main.go",
        "pattern": r"tracearchive\.|body_ref_dt|body_ref_inst|body_ref_part|body_ref_row|call_traces|obsmodel\.|BodyRefDT|BodyRefInst|BodyRefPart|BodyRefRow|ta\.Ref\{",
        "expect": "absent",
        "why": (
            "CLI 必须经 TraceBodyService 取正文与定位键（`TraceBodyResult.Ref`）；直接引用冻结包、"
            "自己查那四个定位键列、**用 `obsmodel.CallTrace` 的 `BodyRefDT/BodyRefInst/BodyRefPart/"
            "BodyRefRow` 字段取四分量、或在 CLI 里直接构造 `ta.Ref{…}`**，都会绕过服务层的优先级"
            "判定/ValidateRef ⇒ 出现第二份判据（D59 的形态）。可违反：正文出现上述任一标识符即 FAIL。"
            "**残余（如实登记）**：若有人把模型字段再改名（如 `tr.RefDT`）或用字符串拼接隐藏表名，"
            "本行仍拦不住——那已属刻意规避，由下面的正向检查与镜像行兜底。"
            "**本行的正向一侧**：`main.go` 只经 `service.OpenArchiveReader` / `service.NewTraceBodyService` 接触归档。"
        ),
    },
    {
        "name": "CLI必须调用服务构造",
        "file": "cmd/trace-body/main.go",
        "pattern": r"NewTraceBodyService\(",
        "expect": "present",
        "why": (
            "正向钉住「取正文与 `--ref` 真的走了服务层」（任务 7 步骤 3 的硬要求：复用 "
            "TraceBodyService，不得自己查库 + 自己拼路径）。**这一行是 E18 的承重项**：禁用项只能"
            "证明「没用这些绕行写法」，只有它要求服务被真正构造出来；两者合起来才排除「CLI 自造 "
            "定位键」。可违反：删掉服务构造、改为直接查库即 FAIL。**本行不镜像到 "
            "internal/observability/service/trace_body_cli.go**（理由见集合头部与计划文末 E41）。"
        ),
    },
    {
        "name": "CLI的ref取自服务导出",
        "file": "cmd/trace-body/main.go",
        "pattern": r"\.Ref\b",
        "expect": "present",
        "why": (
            "钉住「`--ref` 打印的是服务导出的四分量」（任务 2 的 `TraceBodyResult.Ref`）。"
            "**本行的效力边界（订正 E18）**：`\\.Ref\\b` 会被自造的 `ta.Ref{…}` 满足（`ta.Ref{` 里就含 "
            "`.Ref`）⇒ 它单独**不能**证明「ref 取自服务」；真正承重的是上面两行（加强后的禁用项 + "
            "`NewTraceBodyService(`）。在本实现里它是 `ref := res.Ref`（取值语句）。"
        ),
    },
    {
        "name": "CLI不把DSN写进日志",
        "file": "cmd/trace-body/main.go",
        "pattern": r'"dsn"\s*,\s*\*?dsn|dsn\s*=\s*%[svq]',
        "expect": "absent",
        "why": (
            "DSN 含凭据，不得进日志（命中形态：`slog.Info(..., \"dsn\", dsn)` 或 `Printf(\"dsn=%s\", dsn)`）。"
            "**注**：`--help`/usage 文本里出现 `--dsn` 字样是允许的（原稿的正则 `Print.*(dsn|DSN)` 会把它"
            "误判为违规 ⇒ 恒 FAIL）；usage 文本用 `--dsn <postgres-url>` 形式，不写 `--dsn=%s`。"
        ),
    },
    {
        "name": "CLI写明仅限运维",
        "file": "cmd/trace-body/main.go",
        "pattern": r"仅限运维|不得暴露为服务",
        "expect": "present",
        "why": "CLI 不做归属/权限判定（用 DSN 直连）⇒ 必须自带这条使用告示（可被违反：删掉即 FAIL）。",
    },
    {
        "name": "冻结包GetBody签名未变",
        "file": "internal/tracearchive/reader.go",
        "pattern": r"func \(r \*Reader\) GetBody\(ctx context\.Context, ref Ref\) \(\[\]byte, error\)",
        "expect": "present",
        "why": (
            "CLI 经 TraceBodyService 调用冻结包 Reader；签名若变则接线失效。整目录零改动的权威判据是"
            "任务 9 的 git diff（D59）。"
        ),
    },
    # ---- 以下两行是第 1、3 条在 TARGET 文件上的镜像（E41 的落点裁定）----
    {
        "name": "CLI逻辑文件复用服务不自己拼路径也不直查定位键列",
        "file": "internal/observability/service/trace_body_cli.go",
        "pattern": r"tracearchive\.|body_ref_dt|body_ref_inst|body_ref_part|body_ref_row|call_traces|obsmodel\.|BodyRefDT|BodyRefInst|BodyRefPart|BodyRefRow|ta\.Ref\{",
        "expect": "absent",
        "why": (
            "**镜像行（E41）**：m05 的目标串必须落在本集合的 TARGET 文件里 ⇒ ref 格式化必然被抽进 "
            "`internal/observability/service/trace_body_cli.go`，故把 `main.go` 的禁用项镜像到这里，"
            "否则该文件里的绕行（自己查定位键列 / 引用冻结包拼路径 / 自造定位键）无人检查。"
            "本文件唯一允许接触冻结包的地方是 `OpenArchiveReader`（把配置变成服务需要的 Reader，"
            "不参与「什么算合法定位键」的判定）。可违反：正文出现被禁标识符即 FAIL。"
        ),
    },
    {
        "name": "CLI逻辑文件的ref取自服务导出",
        "file": "internal/observability/service/trace_body_cli.go",
        "pattern": r"\.Ref\b",
        "expect": "present",
        "why": (
            "**镜像行（E41）**：钉住「四分量输出的参数就是服务导出的定位键类型」"
            "（`WriteRef(w io.Writer, ref ta.Ref)`，由 `main.go` 传 `res.Ref`）。效力边界同第 3 条："
            "`\\.Ref\\b` 会被自造的类型引用满足，真正承重的是禁用项与 `main.go` 的服务构造行。"
        ),
    },
]

# 5 条全部应在原始侧转为断言红（实测见本轮权威表）；本集合无零覆盖条目。
EXPECTED_GREEN = {}

# ---------------------------------------------------------------------------
# 映射表（任务 7 的判据 / 要求 → 集合里的条目）
# ---------------------------------------------------------------------------
REQUIREMENTS = [
    {
        "plan": "退出码 3（body_missing）/ 4（未归档·需人工介入）/ 5（读侧未配置）必须互不混淆",
        "mutations": ["m01_合并缺失与未归档退出码", "m02_禁用退出码合并"],
        "checks": [],
        "note": (
            "杀手 = TestTraceBodyCLIExitCodes（表驱动：裸哨兵 + `%w` 包装两种形态）。"
            "m01 摘掉 3、m02 摘掉 5，各自让一格断言失败。"
        ),
    },
    {
        "plan": "缺必填项（--root/--dsn/--request-id）⇒ 退出码 1（用法错误），**不是 panic**",
        "mutations": ["m03_缺requestid不再报错"],
        "checks": [],
        "note": (
            "杀手 = TestTraceBodyCLIParsesFlags（合法参数逐字保留 + 5 格失败形态：缺 root / 缺 dsn / "
            "缺 request-id / 未知开关 / --help，全部断言「返回 error 且 options 为 nil」）。"
            "**相对计划订正了制造红的机制**（草稿的 panic ⇒ 崩溃红），见集合头部与计划文末 E40。"
        ),
    },
    {
        "plan": "`--raw` 逐字节把正文写 stdout（不额外加换行，可管道给 jq）",
        "mutations": ["m04_raw多写换行"],
        "checks": [],
        "note": (
            "杀手 = TestTraceBodyCLIRawWritesExactBytes（逐字节相等 + 字节数 + 「正文自带换行时不得再补」"
            "的阴性对照）。"
        ),
    },
    {
        "plan": "`--ref` 输出 dt/inst/part/row 四分量，且**逐字等于服务导出的 `TraceBodyResult.Ref`**",
        "mutations": ["m05_ref少输出分量"],
        "checks": ["CLI的ref取自服务导出", "CLI逻辑文件的ref取自服务导出"],
        "note": (
            "杀手 = TestTraceBodyCLIRefModePrintsAllFourComponents（设一份与正文无关的定位键，断言输出"
            "逐字等于它 + 逐分量差分定位）。静态两行钉住「打印/取值确实引用服务导出的字段」。"
        ),
    },
    {
        "plan": "CLI 必须复用 TraceBodyService：不得自己查定位键列、不得引用冻结包拼路径、不得自造定位键",
        "mutations": [],
        "checks": [
            "CLI复用服务不自己拼路径也不直查定位键列",
            "CLI必须调用服务构造",
            "CLI逻辑文件复用服务不自己拼路径也不直查定位键列",
        ],
        "note": (
            "**无对应变异**（这是**结构性**要求：要做反向变异就得先让实现漂移成一个「第二个读实现」，"
            "那本身就是缺陷）。承重的是两处禁用项（`main.go` + TARGET 文件的镜像行）与正向的服务构造行；"
            "`--ref` 的取值来源另由 `TestTraceBodyCLIRefModePrintsAllFourComponents` 从行为侧证明"
            "（它拿到的是服务返回的 `Ref`，而不是重新查库拼出来的）。**残余已如实登记**见两处 `why`。"
        ),
    },
    {
        "plan": "CLI 不做归属/权限判定（DSN 直连）⇒ 必须自带「仅限运维/不得暴露为服务」的使用告示，且 DSN 不进日志",
        "mutations": [],
        "checks": ["CLI写明仅限运维", "CLI不把DSN写进日志"],
        "note": (
            "两条都是**不存在性 / 文档性**要求，变异无法制造（删掉告示不是「放宽判定」，而 DSN 泄漏"
            "不是可测的行为）⇒ 由静态检查承重。"
        ),
    },
    {
        "plan": "冻结包 `internal/tracearchive/` 一个字节都不改（GetBody 签名是接线的锚点）",
        "mutations": [],
        "checks": ["冻结包GetBody签名未变"],
        "note": "整目录零改动的权威判据是任务 9 的 `git diff`（D59）；本行只钉住签名这一处接线锚点。",
    },
    {
        "plan": "`--json` 输出 `{\"source\":…,\"bytes\":…,\"sha256\":…}`（不输出正文）",
        "mutations": [],
        "checks": [],
        "note": (
            "**无对应变异**（覆盖边界，已登记）：计划草稿的 5 条不含它，本集合保持 5 条 ⇒ 该守卫只有"
            "行为证据 `TestTraceBodyCLIJSONSummaryMatchesServiceResult`（source/bytes/sha256 全部由服务"
            "返回的正文算出 + 不得夹带正文）。**不得**读成「已由变异覆盖」。"
        ),
    },
]
