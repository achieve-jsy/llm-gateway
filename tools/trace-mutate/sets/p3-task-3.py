"""计划 3 / 任务 3 的变异集合（作者编写的产物 ⇒ 必须 git-tracked，见全局约束第 1 条）。

被变异文件是**哨兵 → HTTP 表现的纯函数映射**
（`internal/observability/handler/trace_body_error.go`）：`MapTraceBodyError` 把
服务层 / 冻结包的哨兵分类成 (status, 诊断体)。本批变异的主题是
**映射表的每一行都必须承重** —— 任意一行被摘掉 / 放宽 / 换序，对应用例必须转红成
「断言红」（编译红、panic、超时都不算通过）。

编号一经发布不再改动；匹配一律用字符串（不用行号），行号仅供人读。

## 相对计划（任务 3 步骤 5）的改动（逐条附实测理由；**没有任何一条被静默丢弃**）

计划给 7 条变异（m01–m07），本集合**逐字保留**，另**补入** 5 条（m08–m12，登记 E21）
并**改写 0 条**。补入的理由：计划任务 3 的权威判据表自己要求
「**逐行实现、逐行有用例、逐行有变异或 CHECKS**」，但 m01–m07 只覆盖
「未归档 / body_missing / 损坏 / 禁用 / 取消」五类，**没有覆盖** nil(200)、
记录缺失(404)、定位键不一致(500)、读超时(504)、兜底(500 internal_error) 五行；
计划自己的 CHECKS 只有 3 行（不新增哨兵 / 无 IO / 冻结包哨兵未变），也不含这五行。
按全局约束第 7 条「变异（行为）与静态检查（结构不变量）分开记」，这五行都是**行为**
要求 ⇒ 用变异而不是静态检查覆盖（与任务 2 的 E11 同一处置）。

m11 的形态说明：计划没有为「兜底 arm」给变异。本集合用
`case err != nil:` 把**读超时臂的条件放宽成「一切非 nil」** ⇒ 兜底 arm 再也到达不了，
于是「未知错误 ⇒ 500/internal_error」与「ErrPartNotFound ⇒ 落到兜底」两行同时转红。
选它而不是改 `Code: "internal_error",`，是因为后者依赖 gofmt 的字段对齐
（`Code:    "internal_error",` 有 4 个空格），一旦有人加一个更长的字段名就会漂移成
PATTERN-MISS；`case err != nil:` 与对齐无关，且正是「放宽某个判定」的标准形态。

## 相对计划（任务 3 步骤 1）的一处**测试向量订正**（登记 E20）

计划步骤 1 的 `TestMapTraceBodyErrorNotArchivedKeepsUnderlyingText` 要求：
传入 `fmt.Errorf("%w: %s（块内共 %d 行）", ta.ErrNotArchived, "<ref>", 3)`，
并断言 message **包含「行号越界」与「块内共 3 行」两个子串**。
但该向量产出的文本是
`tracearchive: 产物与暂存均不存在: <ref>（块内共 3 行）` —— **不含「行号越界」**
（这四个字是计划正文的诊断**说明**，不是冻结包 `reader.go` 的错误文本；
`reader.go:183` 的实际拼装是 `"%w: %s（块内共 %d 行）"`）⇒ 该断言**恒红**、不可满足。
本实现按**冻结包的真实拼装**取三个向量（`reader.go:139/183/266`：两副本都不存在 /
行号越界 / 偏移未命中行首），断言各自的**可分辨子串**原样透出
（`产物与暂存均不存在` / `块内共 3 行` / `偏移 4096 未命中行首`）。
语义不变（仍是「底层文本必须被保留、不得替换成固定文案」），只是向量可达。

## 变异测不到、但有静态检查行（CHECKS）

- 「不新增存储层哨兵」（规格 §7.1）与「纯函数不引入 IO/DB/协议类型」：**不存在性**
  断言，变异证明不了「没写」；
- 「冻结包的哨兵定义未变」（前提条件，且 `internal/tracearchive/` **冻结、不可变异**）；
- 「映射层不得把服务层结果包壳」（**本集合新增的一行**）：它是设计层不变量
  （与任务 6 的 **E19** 同形 —— 泄漏来自「加字段 / 换包壳」这类设计改动，不是某条
  可摘掉的判定），没有对应变异 —— 但由两条**可失败**的用例共同承重：
  `TestMapTraceBodyErrorSuccessCarriesNoPayload`（nil ⇒ 零值诊断体）与
  `TestTraceBodyErrorResponseKeySet`（响应键集合逐字固定，无 ref/source/body/trace 键）。
- 「`Message: err.Error(),` / `BodyMissing: true,` /
  `http.StatusServiceUnavailable, TraceBodyErrorResponse{` 各自只出现一处」：
  由 m05 / m03 / m06 定位串的**唯一性**承重 —— 工具的 `--check` 与 `apply_mutation`
  要求 `count == 1`，多写一处即 PATTERN-MISS（该集合零产出），这是比静态正则更强的判据。

## 一次实测踩坑（记以免重犯）

CHECKS 的 `pattern` 是对**整个文件文本（含注释）**做 `re.search`。本实现的第一版
在文件头注释里写了「没有 gin.Context」「……result.Body……」这类字样，于是
`纯函数不引入IO` 与 `映射层不包装正文结果` 两条**恒 FAIL** ⇒ 工具在跑变异**之前**就以
「静态检查失败」终止（整轮零证据 —— 该次中止的原始输出留档为
`p3-task-3-authoritative-aborted-checks-d0013852.txt`）。修正是**改注释措辞**，
不是放宽 CHECKS。⇒ 凡是「不存在性」CHECKS 覆盖的文件，注释里不得直接抄那些标识符。
"""

NAME = "p3-task-3"
TARGET = "internal/observability/handler/trace_body_error.go"
PACKAGES = ["./tests/observability/handler/"]
MANIFEST_FILES = ["tests/observability/handler/trace_body_error_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/observability/handler/trace_body_error_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 本集合的用例全部是纯函数表驱动：无 IO、无 DB、无网络、无时钟、无环境变量。
SKIP_WHITELIST = []
RETIRED = {}

MUTATIONS = {}

# m01 ErrNotArchived 被降级成 404 ⇒ 把存储故障伪装成「数据不存在」（v3 方案的原始教训）
#     杀手：TestMapTraceBodyError（未归档行）
MUTATIONS["m01_未归档降级为404"] = [
    ("sub", "case errors.Is(err, ta.ErrNotArchived):",
            "case errors.Is(err, ta.ErrNotArchived) && false:"),
]
# m02 用 == 比较代替 errors.Is ⇒ 包装过的哨兵全部落进兜底（500/internal_error）
#     杀手：TestMapTraceBodyErrorRequiresWrapping
MUTATIONS["m02_改用等值比较"] = [
    ("sub", "case errors.Is(err, ta.ErrNotArchived):", "case err == ta.ErrNotArchived:"),
]
# m03 body_missing 标志被摘掉 ⇒ 404 的 body_missing=true 契约破裂（规格 §8.1 明文）
#     杀手：TestMapTraceBodyError（body_missing 行的 wantMissing 断言）
MUTATIONS["m03_不再置body_missing标志"] = [
    ("sub", "BodyMissing: true,", "BodyMissing: false,"),
]
# m04 产物损坏被报成 404 ⇒ 半写/位腐被伪装成「不存在」
#     杀手：TestMapTraceBodyError（footer/trailer 两行）
MUTATIONS["m04_产物损坏降级为404"] = [
    ("sub", "case errors.Is(err, ta.ErrFooterCorrupt), errors.Is(err, ta.ErrTrailerCorrupt):",
            "case false:"),
]
# m05 底层文本被替换成固定文案 ⇒ 「三种成因」不可分辨
#     杀手：TestMapTraceBodyErrorNotArchivedKeepsUnderlyingText（三个向量）
MUTATIONS["m05_丢失底层诊断文本"] = [
    ("sub", "Message: err.Error(),", 'Message: "未归档",'),
]
# m06 读侧未配置从 503 变成 500 ⇒ 部署错误与存储错误混淆
#     杀手：TestMapTraceBodyError（trace_body_disabled 行）
MUTATIONS["m06_禁用状态码变成500"] = [
    ("sub", "http.StatusServiceUnavailable, TraceBodyErrorResponse{",
            "http.StatusInternalServerError, TraceBodyErrorResponse{"),
]
# m07 上下文取消被报成 500 ⇒ 日志里全是假存储故障（掩盖真实故障）
#     杀手：TestMapTraceBodyError（client_closed 行）
MUTATIONS["m07_取消报成500"] = [
    ("sub", "case errors.Is(err, context.Canceled):", "case false:"),
]
# ---------------------------------------------------------------------------
# 以下 5 条为**补入**（登记 E21）：覆盖计划权威判据表里「没有变异」的五行
# ---------------------------------------------------------------------------
# m08 成功路径不再返回 200（nil 落进兜底）⇒ 调用方会把「无错误」当成内部错误。
#     替换串保留 `err == nil` 的引用 ⇒ 不产生未使用变量、可编译。
#     杀手：TestMapTraceBodyError（nil 行）＋ TestMapTraceBodyErrorSuccessCarriesNoPayload
MUTATIONS["m08_成功路径不再返回200"] = [
    ("sub", "case err == nil:", "case err == nil && false:"),
]
# m09 记录缺失不再单独分类 ⇒ 「没有这条 trace」与「正文从未落盘」混为一谈
#     （前端无法区分，正是 spec §8.1 要消灭的一类错误）。
#     杀手：TestMapTraceBodyError（记录缺失行）
MUTATIONS["m09_记录缺失不再分类"] = [
    ("sub", "case errors.Is(err, gorm.ErrRecordNotFound):",
            "case errors.Is(err, gorm.ErrRecordNotFound) && false:"),
]
# m10 定位键不一致（数据写坏）不再单独分类 ⇒ 元数据自身矛盾被伪装成兜底内部错误，
#     运维无法从 code 看出「该查数据」还是「该查代码」。
#     杀手：TestMapTraceBodyError（定位键不一致行）
MUTATIONS["m10_定位键不一致不再分类"] = [
    ("sub", "case errors.Is(err, service.ErrTraceBodyInconsistent):",
            "case errors.Is(err, service.ErrTraceBodyInconsistent) && false:"),
]
# m11 兜底 arm 被提前拦截（读超时臂放宽成「一切非 nil」）⇒
#     「其它 ⇒ 500/internal_error」与「ErrPartNotFound ⇒ 落到兜底」两行不再可达。
#     杀手：TestMapTraceBodyError（未知错误兜底行 / ErrPartNotFound 行）
MUTATIONS["m11_兜底被提前拦截"] = [
    ("sub", "case errors.Is(err, context.DeadlineExceeded):", "case err != nil:"),
]
# m12 读超时不再单独分类 ⇒ 大块解压超时被报成 500（而不是 504 read_timeout），
#     客户端无从得知「等一等可能就好」。
#     杀手：TestMapTraceBodyError（读超时行）
MUTATIONS["m12_超时不再分类"] = [
    ("sub", "case errors.Is(err, context.DeadlineExceeded):",
            "case errors.Is(err, context.DeadlineExceeded) && false:"),
]

# ---------------------------------------------------------------------------
# 静态检查（变异测不到的要求）
# ---------------------------------------------------------------------------
CHECKS = [
    {
        "name": "不新增存储层哨兵",
        "file": "internal/observability/handler/trace_body_error.go",
        "pattern": r"errors\.New\(",
        "expect": "absent",
        "why": (
            "规格 §7.1「读取侧不为它新增分支或哨兵」；只有服务层两个哨兵"
            "（任务 2 的 ErrTraceBodyInconsistent / ErrTraceBodyDisabled）可以有。"
            "**不存在性**要求 ⇒ 变异无法证明「没写」。可违反：本文件出现 "
            "errors.New(...) 即 FAIL（含注释里的同名文本）。"
        ),
    },
    {
        "name": "纯函数不引入IO",
        "file": "internal/observability/handler/trace_body_error.go",
        # 【注意】不能写成 `gorm\.`：本文件必须**合法引用** gorm.ErrRecordNotFound
        # （它是哨兵，不是 IO），import 路径 "gorm.io/gorm" 也不含 "io.Read"。
        "pattern": r"os\.|io\.Read|gorm\.DB|gin\.",
        "expect": "absent",
        "why": (
            "映射表必须可在无文件系统 / 无 DB / 无 HTTP 的情况下逐行测。"
            "可违反：引入任意文件操作或 gin 上下文即 FAIL。"
        ),
    },
    {
        "name": "冻结包哨兵语义未变",
        "file": "internal/tracearchive/errors.go",
        "pattern": r'ErrBodyMissing = errors\.New\("tracearchive: 正文从未落盘"\)',
        "expect": "present",
        "why": (
            "本任务把 ta.ErrBodyMissing 映射为 404 + body_missing=true 的前提是"
            "哨兵定义不变；整目录零改动的权威判据是任务 9 的 git diff（D59）。"
        ),
    },
    {
        "name": "映射层不包装正文结果",
        "file": "internal/observability/handler/trace_body_error.go",
        "pattern": r"json\.Marshal|TraceBodyResult|result\.Body|json\.RawMessage",
        "expect": "absent",
        "why": (
            "200 的 body 必须是**正文 JSON 原文**（任务 5 的 "
            "c.Data(http.StatusOK, \"application/json\", result.Body) 直接写出）⇒ 映射层"
            "不得 json.Marshal 服务层结果、也不得引用 TraceBodyResult —— 一旦包壳，"
            "任务 2 新增的 Ref/Source 字段（无 json tag，字段名照原样）就会泄漏进响应。"
            "**该要求没有对应变异**（属设计层不变量，不是可摘掉的判定），"
            "由两条可失败用例承重：TestMapTraceBodyErrorSuccessCarriesNoPayload 与 "
            "TestTraceBodyErrorResponseKeySet。可违反：本文件引用上述任一标识符即 FAIL。"
        ),
    },
]

# 12 条全部应在原始侧转为断言红（实测见本轮交付报告 §3）。
EXPECTED_GREEN = {}

# ---------------------------------------------------------------------------
# 映射表（计划任务 3 的权威判据表逐行 + 三条形状要求 + 四条静态要求）
# ---------------------------------------------------------------------------
REQUIREMENTS = [
    {
        "plan": "映射表 nil ⇒ 200（已有正文；200 的 body 由调用方写正文原字节，不得包壳）",
        "mutations": ["m08_成功路径不再返回200"],
        "checks": ["映射层不包装正文结果"],
        "note": (
            "杀手 = TestMapTraceBodyError（nil 行）+ TestMapTraceBodyErrorSuccessCarriesNoPayload。"
            "「零值诊断体」「响应键集合固定」是**结构不变量**（无『摘掉某判定』的对应变异，"
            "与任务 6 的 E19 同形）⇒ 由两条可失败断言 + 1 行静态检查承重。"
        ),
    },
    {
        "plan": "映射表 gorm.ErrRecordNotFound ⇒ 404 trace_not_found",
        "mutations": ["m09_记录缺失不再分类"],
        "checks": [],
        "note": "杀手 = TestMapTraceBodyError（记录缺失行）。m09 为**补入**（登记 E21）。",
    },
    {
        "plan": "映射表 ta.ErrBodyMissing ⇒ 404 + code=body_missing + body_missing=true",
        "mutations": ["m03_不再置body_missing标志"],
        "checks": [],
        "note": (
            "杀手 = TestMapTraceBodyError（body_missing 行）。status/code 两面由该行断言，"
            "标志面由 m03 独立承重；「BodyMissing: true, 只出现一处」由 m03 定位串的"
            "唯一性（工具 --check 要求 count==1）承重。"
        ),
    },
    {
        "plan": "映射表 service.ErrTraceBodyInconsistent ⇒ 500 body_ref_inconsistent",
        "mutations": ["m10_定位键不一致不再分类"],
        "checks": [],
        "note": "杀手 = TestMapTraceBodyError（定位键不一致行）。m10 为**补入**（登记 E21）。",
    },
    {
        "plan": "映射表 service.ErrTraceBodyDisabled ⇒ 503 trace_body_disabled（不是 500）",
        "mutations": ["m06_禁用状态码变成500"],
        "checks": [],
        "note": (
            "杀手 = TestMapTraceBodyError（读侧未配置行）。"
            "「http.StatusServiceUnavailable, TraceBodyErrorResponse{ 只出现一处」由 m06 "
            "定位串的唯一性承重。"
        ),
    },
    {
        "plan": "映射表 ta.ErrNotArchived ⇒ 500 not_archived，且**保留底层文本**（三种成因可分辨）",
        "mutations": ["m01_未归档降级为404", "m02_改用等值比较", "m05_丢失底层诊断文本"],
        "checks": [],
        "note": (
            "杀手 = TestMapTraceBodyError（未归档行）+ TestMapTraceBodyErrorRequiresWrapping + "
            "TestMapTraceBodyErrorNotArchivedKeepsUnderlyingText。测试向量按冻结包 reader.go 的"
            "真实拼装取三格（139/183/266），计划原文的『行号越界』子串恒不可满足 ⇒ 订正登记 E20。"
        ),
    },
    {
        "plan": "映射表 ta.ErrFooterCorrupt / ta.ErrTrailerCorrupt ⇒ 500 part_corrupt",
        "mutations": ["m04_产物损坏降级为404"],
        "checks": [],
        "note": "杀手 = TestMapTraceBodyError（footer/trailer 两行）。",
    },
    {
        "plan": "映射表 ta.ErrPartNotFound ⇒ **不映射任何状态码**（410 不可达，D53）⇒ 落到兜底 500/internal_error",
        "mutations": ["m11_兜底被提前拦截"],
        "checks": [],
        "note": (
            "杀手 = TestMapTraceBodyError（ErrPartNotFound 行）。该行是 D53 的可失败形式："
            "若有人为 ErrPartNotFound 反推 410，该行立即红。m11 为**补入**（登记 E21）。"
        ),
    },
    {
        "plan": "映射表 context.Canceled ⇒ 408 client_closed（不写 500，否则日志里全是假存储故障）",
        "mutations": ["m07_取消报成500"],
        "checks": [],
        "note": "杀手 = TestMapTraceBodyError（客户端断开行）。",
    },
    {
        "plan": "映射表 context.DeadlineExceeded ⇒ 504 read_timeout",
        "mutations": ["m12_超时不再分类"],
        "checks": [],
        "note": "杀手 = TestMapTraceBodyError（读超时行）。m12 为**补入**（登记 E21）。",
    },
    {
        "plan": "映射表 其它 ⇒ 500 internal_error 兜底（message 不含内部细节）",
        "mutations": ["m11_兜底被提前拦截"],
        "checks": [],
        "note": "杀手 = TestMapTraceBodyError（未知错误兜底行）。m11 为**补入**（登记 E21）。",
    },
    {
        "plan": "形状：每一行都必须能用 %w 包装后的 errors.Is 命中（不得用 == 比较）",
        "mutations": ["m02_改用等值比较"],
        "checks": [],
        "note": "杀手 = TestMapTraceBodyErrorRequiresWrapping（6 个包装向量）。",
    },
    {
        "plan": "形状：`Message: err.Error(),` 只能出现在 ErrNotArchived 一个臂",
        "mutations": ["m05_丢失底层诊断文本"],
        "checks": [],
        "note": (
            "「只出现一处」由 m05 定位串的唯一性承重（工具 --check/apply_mutation 要求 "
            "count==1；多写一处即 PATTERN-MISS ⇒ 集合零产出）。"
        ),
    },
    {
        "plan": "形状：`BodyMissing: true,` 只能出现在 body_missing 一个臂",
        "mutations": ["m03_不再置body_missing标志"],
        "checks": [],
        "note": "同上，由 m03 定位串的唯一性承重。",
    },
    {
        "plan": "形状：`http.StatusServiceUnavailable, TraceBodyErrorResponse{` 只能出现在 disabled 一个臂",
        "mutations": ["m06_禁用状态码变成500"],
        "checks": [],
        "note": "同上，由 m06 定位串的唯一性承重。",
    },
    {
        "plan": "规格 §7.1：读侧不为隔离件新增分支或哨兵（本文件不得新增哨兵）",
        "mutations": [],
        "checks": ["不新增存储层哨兵"],
        "note": "不存在性要求 ⇒ 只有静态证据。",
    },
    {
        "plan": "纯函数：无 IO / 无 DB / 无协议类型（可逐行测、可注入替身）",
        "mutations": [],
        "checks": ["纯函数不引入IO"],
        "note": "不存在性要求 ⇒ 只有静态证据。",
    },
    {
        "plan": "前提：冻结包哨兵定义未变（本任务映射其语义）",
        "mutations": [],
        "checks": ["冻结包哨兵语义未变"],
        "note": "冻结包不可变异；整目录零改动的权威判据是任务 9 的 git diff（D59）。",
    },
]
