r"""计划 4 / 任务 10 的变异集合 B：跨包**镜像常量**的漂移（`internal/tracesealer/options.go`）。

主题：`tracesealer` 里有三个镜像常量 —— `MaxTrailerEntries`（冻结包未导出）、
`GatewayMaxRollInterval` 与 `DefaultMinAge`（网关侧未导出）。Go 没有「引用未导出常量」的办法，所以
唯一能自动发现漂移的手段是**现读源码、求值、比对**（`tests/tracesealer/constants_contract_test.go`）。
三条变异各改一个镜像值，产生的失效面完全不同：

  * **上界被改小**（m01：8,388,608 → 8,388,607）⇒ A8 的「行数不超上界」判断换了标尺：真实越界的块
    不再被认出来，而**没有越界**的块反而可能被预警；
  * **滚动上界被放大**（m02：1h → 2h）⇒ `--min-age` 的强制下界（必须**严格大于**网关滚动上界）跟着
    放大 ⇒ 放行一个「sealer 去封网关仍在追加的块」的配置（正文写进已 unlink 的 inode ⇒ 永久丢失，D7）；
  * **默认 min-age 被改小**（m03：3h → 2h）⇒ 三处镜像不再一致；容器里少了 1 小时安全余量。

编号一经发布不再改动；匹配一律用字符串（不用行号）。

## 与计划文本的一致性

计划任务 10 步骤 5 的 `p4-task-10-contract.py` 逐字给出三条变异与各自杀手（m01–m03），本文件逐字采用。
**没有**追加静态检查：三条要求都是「常量真正的值是多少」这类**行为/求值**要求，按 E62 只能由在册
行为测试承载（`tests/tracesealer/constants_contract_test.go`）。

## 与计划文本的一处**加强**（如实登记）

计划只要求钉住 `MaxTrailerEntries` / `GatewayMaxRollInterval` / `DefaultMinAge` 三个镜像。本集合的
在册清单里另有第四条 `TestSealerFooterSizeMatchesFrozenPackage`：`FooterSize` 是**唯一**一个冻结包
**已导出**的镜像常量，可以直接 `tracearchive.FooterSize` 比对（编译期绑定 + 运行期断言），比读源码
求值更硬。它不是新要求，而是同一条「镜像不得漂移」要求的**更强证据形态**，故一并纳入清单。

## 允许的绿

无（`EXPECTED_GREEN` 为空）。三条变异都必须红成**断言红**（E70 约定 2）。
"""

NAME = "p4-task-10-contract"
TARGET = "internal/tracesealer/options.go"
PACKAGES = ["./tests/tracesealer/"]
MANIFEST_FILES = ["tests/tracesealer/constants_contract_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/tracesealer/constants_contract_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 纯源码解析 + 常量比对：不碰磁盘产物、不需要 zstd，没有可 SKIP 的理由
SKIP_WHITELIST = []
RETIRED = {}
EXPECTED_GREEN = {}

MUTATIONS = {}

# m01 MaxTrailerEntries 8,388,608 → 8,388,607 ⇒ A8 的「行数不超上界」判断换了标尺（差 1 行就足以让
#     「恰好越界」的那一块被漏判，而预警线也跟着挪）。
#     杀手：TestSealerMaxTrailerEntriesMatchesFrozenReader
MUTATIONS["m01_上界少一行"] = [
    ("sub", "MaxTrailerEntries = 8_388_608", "MaxTrailerEntries = 8_388_607"),
]

# m02 GatewayMaxRollInterval 1h → 2h ⇒ `--min-age` 的强制下界跟着放大：放行一个可能封到
#     「网关仍在追加」的块的配置（D7：正文写进已 unlink 的 inode ⇒ 永久丢失）。
#     杀手：TestSealerGatewayRollBoundMatchesConfig
MUTATIONS["m02_滚动上界放大一倍"] = [
    ("sub", "GatewayMaxRollInterval = time.Hour", "GatewayMaxRollInterval = 2 * time.Hour"),
]

# m03 DefaultMinAge 3h → 2h ⇒ 三处镜像不再一致（冻结包 defaultSealMinAge 与网关侧 traceBodyMinAge
#     仍是 3h）。注意 2h 仍然 > MinAgeFloor(1h) ⇒ **不会**被 normalize 拦下，只能靠这条契约用例发现。
#     杀手：TestSealerDefaultMinAgeMatchesFrozenDefault
MUTATIONS["m03_默认min-age改2小时"] = [
    ("sub", "DefaultMinAge       = 3 * time.Hour", "DefaultMinAge       = 2 * time.Hour"),
]

# 变异测不到的那条要求（FooterSize 镜像）配一条**存在性**静态检查：它的镜像漂移由集合 A 的 m01 直取，
# 本集合的 TARGET（options.go）里没有 FooterSize ⇒ 本集合内造不出「只改 FooterSize」的变异。
CHECKS = [
    {
        "name": "镜像长度契约用例在册",
        "file": "tests/tracesealer/constants_contract_test.go",
        "pattern": r"func TestSealerFooterSizeMatchesFrozenPackage\(",
        "expect": "present",
        "why": ("`FooterSize` 是唯一一个可以直接引用（冻结包已导出）的镜像常量 ⇒ 它有一条不需要读源码的强证据"
                "用例。本集合的三条变异都不改它 ⇒ 无杀手，**存在性**要求 ⇒ 静态检查钉住；「它真的能失败」由"
                "跨集合的集合 A m01（FooterSize 32→24）+ 人工探针承载（见 COVERAGE_GAPS）。"),
    },
]

REQUIREMENTS = [
    {
        "plan": "tracesealer.MaxTrailerEntries == 冻结包 reader.go 真实求值出来的 maxTrailerEntries（= 8,388,608）",
        "mutations": ["m01_上界少一行"],
        "checks": [],
        "note": ("**行为（求值）**要求 ⇒ 在册用例 `TestSealerMaxTrailerEntriesMatchesFrozenReader`。"
                 "它现读 `internal/tracearchive/reader.go`、用 go/parser 解析出 `maxTrailerBytes = 64 << 20` 与 "
                 "`maxTrailerEntries = maxTrailerBytes / 8` 并**真正求值**（不是 grep 数字：`32 << 21` 这种等价改写"
                 "不会被误判），再与镜像比。"),
    },
    {
        "plan": "tracesealer.GatewayMaxRollInterval == 1h == 网关侧 trace_body.go 的 traceBodyMaxRollInterval",
        "mutations": ["m02_滚动上界放大一倍"],
        "checks": [],
        "note": "**行为（求值）**要求 ⇒ 在册用例 `TestSealerGatewayRollBoundMatchesConfig`（现读 trace_body.go 并求值时长表达式）。",
    },
    {
        "plan": "三处 3h 必须一致：tracesealer.DefaultMinAge == 冻结包 defaultSealMinAge == 网关侧 traceBodyMinAge",
        "mutations": ["m03_默认min-age改2小时"],
        "checks": [],
        "note": ("**行为（求值）**要求 ⇒ 在册用例 `TestSealerDefaultMinAgeMatchesFrozenDefault`。"
                 "【如实登记，不得夸大】网关侧那条 `roll_interval < minAge` 判据在现有校验次序下**不可达**"
                 "（trace_body.go 自己的注释已登记：更早的 `RollInterval > 1h` 守卫先拦下了所有 `>= 3h` 的取值）"
                 "⇒ 本用例钉的是**常量一致性**，**不得**声称「网关的那条检查承重」。"),
    },
    {
        "plan": "镜像的 footer 长度 == 冻结包已导出的 tracearchive.FooterSize（本集合的一处**加强**，见文件头）",
        "mutations": [],
        "checks": ["镜像长度契约用例在册"],
        "note": ("**行为**要求 ⇒ 在册用例 `TestSealerFooterSizeMatchesFrozenPackage`（直接引用冻结包常量）；"
                 "存在性由同名静态检查钉住。本集合的三条变异都不改 `FooterSize`（那是集合 A `p4-task-10-footer` "
                 "的 m01）⇒ 本集合内无杀手，按 E64② 登记进文末 `COVERAGE_GAPS`。"),
    },
]

# ---------------------------------------------------------------------------
# 零变异覆盖的**要求**登记（E64②；**只是给人看的登记，工具读不到、不参与任何判定**）
# ---------------------------------------------------------------------------
COVERAGE_GAPS = [
    {
        "id": "gap-1-FooterSize镜像常量在本集合内无杀手",
        "title": ("`TestSealerFooterSizeMatchesFrozenPackage` 在册、承重，但本集合 m01–m03 都不改 `FooterSize`"
                  "⇒ 它在本集合里没有杀手"),
        "where": [
            "tests/tracesealer/constants_contract_test.go  TestSealerFooterSizeMatchesFrozenPackage",
            "断言：ta.FooterSize == 32 且 ta.FooterSize == tracesealer.FooterSize",
        ],
        "reachable": "**可达且确定**：两次常量比较，pristine 侧每次运行都执行并 PASS。",
        "no_killer": ("`FooterSize` 的镜像漂移由集合 A `p4-task-10-footer` 的 m01（32 → 24）直取，"
                      "那条变异的**点名杀手**是集合 A 的 `TestFooterRowCountOnHandBuiltFooter`（同一份镜像的"
                      "解析面）。本集合的 TARGET 是 `options.go`，不含 `FooterSize` ⇒ 无法在本集合内造出"
                      "「只改 FooterSize」的变异。⇒ 缺口**不可由本集合闭合**，但**跨集合已闭合**："
                      "m01 施于 `footer.go` 时，本条用例在 `constants_contract_test.go` 里也会转红"
                      "（它只是不在集合 A 的清单里、因而不被集合 A 工具执行）。"),
        "falsifiability": ("**跨集合证据**：集合 A 的 m01 把 `FooterSize` 改成 24，`go test ./tests/tracesealer/ "
                           "-run TestSealerFooterSizeMatchesFrozenPackage` 转红 —— 本轮落盘于 "
                           "`p4-task-10-probe-at-*.txt`（手工探针：改坏 → 转红 → 还原）。"),
        "owner": "本轮只登记。升格路径：集合 A 的清单里纳入本条用例（但那会把「同一个镜像」的事务摊到两份集合的清单上，收益有限）——本轮明确不做。",
    },
]
