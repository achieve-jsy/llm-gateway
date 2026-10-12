r"""计划 4 / 任务 10 的变异集合 A：产物 footer 的**只读镜像解析**（`internal/tracesealer/footer.go`）。

主题：`RowCount` 是冻结包 footer 编码的一份**镜像实现**（字段次序 / 字节序 / magic / 版本 / 长度全部
未导出，只能复制）。它承重的是 A8 的行数观测：镜像一旦漂移，观测到的「块内有多少行」就是**别的
字节**，而调用方不会收到任何信号 —— 观测面自己失真，恰是本计划反复登记的那一类失效。

  * **长度算错**（m01：FooterSize 32 → 24）⇒ 末尾 24 字节被当成 footer，magic 落在 trailer_offset
    里 ⇒ 解析出的行数与真实行数无关（或直接报错）；
  * **偏移差一个字节**（m02）⇒ magic 不再落在 footer 首位 ⇒ 每一块都读不出来（或读到错的行数）；
  * **短文件被折成 0 行**（m03）⇒ 「读不到」与「读到 0 行」从此不可区分 ⇒ A8 的分布会在产物损坏时
    画成「块都很小」，且没有任何东西报警。

编号一经发布不再改动；匹配一律用字符串（不用行号）。

## 与计划文本的一致性

计划任务 10 步骤 5 的 `p4-task-10-footer.py` 逐字给出三条变异与各自杀手（m01–m03），本文件逐字采用，
**没有**追加；也**没有**静态检查 —— 三条要求全部是「解析会返回什么」这类**行为**要求，按 E62 只能由
在册行为测试承载（`tests/tracesealer/footer_test.go`）。

## 与冻结包的边界（如实登记）

`internal/tracearchive/**` 是本计划的**冻结面**：本集合只变异**镜像侧**（`internal/tracesealer/footer.go`），
冻结包零改动。因此「镜像 == 冻结包的编码」这条**跨包**要求只能由 `tests/tracesealer/constants_contract_test.go`
（已导出常量直接比对）与 `tests/tracesealer/footer_roundtrip_test.go`（真实产物往返，人工复核项）承载，
不在本集合的变异范围内。

## 允许的绿

无（`EXPECTED_GREEN` 为空）。三条变异都必须红成**断言红**；任何编译红 / panic / 超时都要就地改写变异
字符串（E70 约定 2），不得登记为允许的绿。
"""

NAME = "p4-task-10-footer"
TARGET = "internal/tracesealer/footer.go"
PACKAGES = ["./tests/tracesealer/"]
MANIFEST_FILES = ["tests/tracesealer/footer_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/tracesealer/footer_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 全部是纯文件读写（手工拼 32 字节 footer）：不依赖 zstd、不依赖真实产物，没有可 SKIP 的理由
SKIP_WHITELIST = []
RETIRED = {}
EXPECTED_GREEN = {}

MUTATIONS = {}

# m01 footer 长度 32 → 24 ⇒ 末尾 24 字节被当成定长 footer：magic 落在 trailer_offset 字段里，
#     解析出的「行数」与真实行数无关（本用例里直接报错，因为 magic 不符）。
#     杀手：TestFooterRowCountOnHandBuiltFooter
MUTATIONS["m01_footer长度32改24"] = [
    ("sub", "FooterSize = 32", "FooterSize = 24"),
]

# m02 读取偏移差一个字节（size-32 → size-33）⇒ magic 不再落在 footer 首位 ⇒ 每块都读不出来。
#     杀手：TestFooterRowCountOnHandBuiltFooter
MUTATIONS["m02_读取偏移差一个字节"] = [
    ("sub", "off := size - int64(FooterSize)", "off := size - int64(FooterSize) - 1"),
]

# m03 短文件返回 (0, nil) 而不是错误 ⇒ 「读不到」与「读到 0 行」不可区分：A8 的分布会在产物损坏时
#     静默画成「块都很小」。0 行本身是**合法读数**（空块），所以这条不是「报错太严」而是「语义塌陷」。
#     杀手：TestFooterRejectsShortFile
MUTATIONS["m03_短文件返回0而不是错误"] = [
    ("sub", 'return 0, fmt.Errorf("%w: %s 仅 %d 字节（不足 footer %d）", ErrFooterCorrupt, artifactPath, size, FooterSize)',
            "return 0, nil"),
]

# 变异测不到的两条要求（「五个字段自洽校验分支」与「镜像长度 == 冻结包已导出常量」）各配一条
# **存在性**静态检查（E62：静态检查只承载存在/不存在类要求；行为由在册用例承载，见 COVERAGE_GAPS）。
CHECKS = [
    {
        "name": "footer不相容五条判据的在册用例",
        "file": "tests/tracesealer/footer_test.go",
        "pattern": (r"(?s)^.*func TestFooterRejectsBadMagic\(.*func TestFooterRejectsUnknownFormatVersion\("
                    r".*func TestFooterRejectsNegativeRowCount\(.*func TestFooterRejectsTrailerNotAdjacentFooter\("
                    r".*func TestFooterRejectsMissingFile\("),
        "expect": "present",
        "why": ("五条判据（坏 magic / 未知版本 / 负 row_count / trailer 未紧邻 footer / 文件不存在）各有在册用例，"
                "但本集合的 m01–m03 只改「长度 / 偏移 / 短文件返回值」⇒ 没有杀手。**存在性**要求 ⇒ 静态检查钉住"
                "这五条用例都在册（删掉任何一条就会 FAIL，整轮失败）；「它们真的能失败」由人工探针承载"
                "（见 COVERAGE_GAPS 的 falsifiability）。"),
    },
    {
        "name": "镜像长度契约用例在册",
        "file": "tests/tracesealer/constants_contract_test.go",
        "pattern": r"func TestSealerFooterSizeMatchesFrozenPackage\(",
        "expect": "present",
        "why": ("`FooterSize` 是唯一一个冻结包**已导出**的镜像常量 ⇒ 「镜像 == 冻结包」这条要求有一条不需要"
                "读源码的强证据用例。它不在本集合的清单里（在 p4-task-10-contract 的清单里）⇒ 存在性由静态检查钉住。"),
    },
    {
        "name": "真实产物往返测在册",
        "file": "tests/tracesealer/footer_roundtrip_test.go",
        "pattern": r"func TestFooterRoundTripOnRealArtifact\(",
        "expect": "present",
        "why": ("「冻结包真的按这个布局写」的唯一真实产物证据（需要 zstd）。它**故意不进任何 MANIFEST_FILES**"
                "（缺 zstd 的机器上会 Skip ⇒ 塞进清单会让整张表判「证据无效」）⇒ 存在性由静态检查钉住，"
                "行为由人工复核承载。"),
    },
]

REQUIREMENTS = [
    {
        "plan": "RowCount 只读产物末尾的固定长度 footer，返回其 row_count（偏移必须精确到字节）",
        "mutations": ["m01_footer长度32改24", "m02_读取偏移差一个字节"],
        "checks": [],
        "note": ("**行为**要求 ⇒ 在册用例 `TestFooterRowCountOnHandBuiltFooter`（按冻结布局手工拼 32 字节 + "
                 "显式断言 FooterSize == 32）。m01 会让常量断言与解析断言至少一条转红；m02 让 magic 检查红。"),
    },
    {
        "plan": "文件不足 32 字节 ⇒ 报错（不是返回 0）",
        "mutations": ["m03_短文件返回0而不是错误"],
        "checks": [],
        "note": ("**行为**要求 ⇒ 在册用例 `TestFooterRejectsShortFile`（0 / 1 / 31 三档，断言 Error + "
                 "ErrorIs(ErrFooterCorrupt) + 返回零值）。"),
    },
    {
        "plan": "footer 与文件不相容（坏 magic / 未知版本 / 负 row_count / trailer 未紧邻 footer）⇒ 报错，且与「文件不存在」分开",
        "mutations": [],
        "checks": ["footer不相容五条判据的在册用例"],
        "note": ("**行为**要求（无专属变异，理由见 COVERAGE_GAPS gap-1）⇒ 在册用例 `TestFooterRejectsBadMagic` / "
                 "`TestFooterRejectsUnknownFormatVersion` / `TestFooterRejectsNegativeRowCount` / "
                 "`TestFooterRejectsTrailerNotAdjacentFooter` / `TestFooterRejectsMissingFile`；"
                 "存在性由静态检查「footer不相容五条判据的在册用例」钉住。"),
    },
    {
        "plan": "镜像的 footer 长度必须等于冻结包已导出的 tracearchive.FooterSize（跨包镜像不漂移）",
        "mutations": [],
        "checks": ["镜像长度契约用例在册", "真实产物往返测在册"],
        "note": ("**行为**要求（跨包常量一致性）⇒ 在册用例 `TestSealerFooterSizeMatchesFrozenPackage`"
                 "（在 `tests/tracesealer/constants_contract_test.go`，由集合 `p4-task-10-contract` 的清单承载）；"
                 "另有真实产物往返测 `TestFooterRoundTripOnRealArtifact`（**故意不进任何 MANIFEST_FILES**，需 zstd）。"
                 "两条用例的**存在性**分别由同名静态检查钉住。"),
    },
]

# ---------------------------------------------------------------------------
# 零变异覆盖的**要求/用例**登记（E64②；**只是给人看的登记，工具读不到、不参与任何判定**）
# ---------------------------------------------------------------------------
COVERAGE_GAPS = [
    {
        "id": "gap-1-footer五个字段自洽校验分支无专属变异",
        "title": ("`footer.go` 的五个字段自洽校验分支（坏 magic / 未知版本 / 负 trailer_offset 或 trailer_len / "
                  "负 row_count / trailer 未紧邻 footer）各自都有在册用例，但本集合 m01–m03 没有一条能杀它们"),
        "where": [
            "tests/tracesealer/footer_test.go  TestFooterRejectsBadMagic",
            "tests/tracesealer/footer_test.go  TestFooterRejectsUnknownFormatVersion",
            "tests/tracesealer/footer_test.go  TestFooterRejectsNegativeRowCount",
            "tests/tracesealer/footer_test.go  TestFooterRejectsTrailerNotAdjacentFooter",
        ],
        "reachable": "**可达且确定**：全部是「写一个 128 字节的文件 + 手工拼的 footer」的纯文件读写，pristine 侧每次运行都执行并 PASS。",
        "no_killer": (
            "本集合 TARGET 的三条变异改的是**长度、偏移、短文件返回值**，不触碰这五条判据的**条件**。"
            "把其中任一条判据整体删掉（例如不再检查 magic）不会让任何在册用例转红 —— 因为其余四条判据仍然"
            "会拦下同一批坏产物（本文件里的坏 magic 形态同时不满足 trailer 紧邻判据）。"
            "⇒ 这是**判据之间的冗余**造成的覆盖缺口，不是「判据没被触达」。"
        ),
        "falsifiability": (
            "本轮按 E70② 补了**人工探针**（改坏 → 转红 → 还原，逐条落盘，见 "
            "`p4-task-10-probe-at-*.txt`）：把每条判据各自的**专属**坏产物喂进去，确认它能单独把对应用例"
            "打红 —— 说明断言不是恒真。**没有**为它们造变异：造出来的形态（删掉一条判据）在本文件里"
            "必然被另一条判据兜住，红不起来，那只能登记成「允许的绿」，而按 E64② 那比如实登记缺口更糟。"
        ),
        "owner": "本轮只登记 + 人工探针。升格路径：把「判据 A 与判据 B 必须各自独立可证伪」做成一条按判据拆分的变异集合（本轮明确不做，会超出任务 10 的边界）。",
    },
    {
        "id": "gap-2-真实产物往返测不在任何清单里",
        "title": ("`TestFooterRoundTripOnRealArtifact`（tests/tracesealer/footer_roundtrip_test.go）是本任务"
                  "**唯一**的真实产物证据，但它**故意不进任何 MANIFEST_FILES**"),
        "where": [
            "tests/tracesealer/footer_roundtrip_test.go  TestFooterRoundTripOnRealArtifact",
            "断言：用冻结包 Sealer 真封一块后，RowCount(真实产物) == 封块时的行数",
        ],
        "reachable": ("本机**可达**（zstd v1.5.7 在场），本轮实跑 PASS（见 build/vet/测试证据）；"
                      "但缺 zstd 的机器上它会 **Skip** ⇒ 放进任何清单都会让整张表在那类机器上判「证据无效」。"),
        "no_killer": (
            "它是「冻结包真的按这个布局写」的证据，不是「镜像侧某行被改坏」的证据 ⇒ 本集合的三条变异"
            "（长度 / 偏移 / 短文件）都会让它一起转红，但它**不点名**任何一条 —— 点名杀手是手工拼 footer 的"
            "用例。按 E62「行为要求必须能在某张权威表的名字清单里找到承载它的顶层用例」：这条要求的承载者是"
            "`constants_contract_test.go` 的四个常量断言（在册）+ 本条真实产物往返（**人工复核项**，"
            "与计划任务 10 步骤 1 的明文规定一致）。"
        ),
        "falsifiability": ("**本机实跑**：pristine 侧 PASS；把 m01（FooterSize→24）或 m02（偏移 −1）应用到 "
                           "`footer.go` 后重跑本条用例 ⇒ FAIL（本轮已落盘，见 `p4-task-10-probe-at-*.txt`）。"),
        "owner": "本轮只登记。若将来 CI 镜像保证装有 C 版 zstd，可把它升格进 `MANIFEST_FILES` 并纳入本集合的推导命令。",
    },
]
