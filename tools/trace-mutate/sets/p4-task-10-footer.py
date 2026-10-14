r"""计划 4 / 任务 10 的变异集合 A：产物 footer 的**只读镜像解析**（`internal/tracesealer/footer.go`）。

主题：`RowCount` 是冻结包 footer 编码的一份**镜像实现**（字段次序 / 字节序 / magic / 版本 / 长度全部
未导出，只能复制）。它承重的是 A8 的行数观测：镜像一旦漂移，观测到的「块内有多少行」就是**别的
字节**，而调用方不会收到任何信号 —— 观测面自己失真，恰是本计划反复登记的那一类失效。

  * **长度算错**（m01：FooterSize 32 → 24）⇒ 末尾 24 字节被当成 footer，magic 落在 trailer_offset
    里 ⇒ 解析出的行数与真实行数无关（或直接报错）；
  * **偏移差一个字节**（m02）⇒ magic 不再落在 footer 首位 ⇒ 每一块都读不出来（或读到错的行数）；
  * **短文件被折成 0 行**（m03）⇒ 「读不到」与「读到 0 行」从此不可区分 ⇒ A8 的分布会在产物损坏时
    画成「块都很小」，且没有任何东西报警；
  * **五条字段自洽判据各自被改成空操作**（m04 magic / m05 版本 / m06 负 row_count / m07 trailer 未
    紧邻 footer / m08 文件不存在 ⇒ 返回 `0, nil`）⇒ 每一类坏产物都会被**当真**解析出一个行数，
    或者把「文件不存在」与「产物损坏」折成同一件事。

编号一经发布不再改动；匹配一律用字符串（不用行号）。

## 与计划文本的一致性（如实登记：一处**新增**，零处削弱）

计划任务 10 步骤 5 的 `p4-task-10-footer.py` 逐字给出三条变异与各自杀手（m01–m03），本文件逐字采用，
并**新增** m04–m08：把 footer 的五条字段自洽判据各自改成空操作。

> **为什么加**：这五条判据都由在册用例承载（`TestFooterRejectsBadMagic` / `...UnknownFormatVersion` /
> `...NegativeRowCount` / `...TrailerNotAdjacentFooter` / `...MissingFile`）。按 E62「行为要求必须由
> 在册行为测试承载、且必须有能直取它的变异」，只登记「无杀手」是不够的 —— 加五条变异后每个负例
> 都有**点名杀手**，且全部红成断言红（不是编译红 / 崩溃红，E70 约定 2）。
>
> **前置的用例侧修正（同一次提交里）**：bad-magic 与未知版本这两个负例原来的产物**同时**违反了
> 「trailer 紧邻 footer」判据（`buildFooter(7, 64, 0)` 配 128 字节 ⇒ 64+0 ≠ 96）⇒ 单独删掉 magic /
> 版本判据时，另一条判据仍会把同一个产物拦下，用例照样绿 —— 那样加的变异红不起来，只能登记成
> 「允许的绿」。因此把这两个负例的 trailer 改成紧邻（offset=96），让每个负例**只违反一条**判据；
> m04/m05 这才真的能红。

## 与冻结包的边界（如实登记）

`internal/tracearchive/**` 是本计划的**冻结面**：本集合只变异**镜像侧**（`internal/tracesealer/footer.go`），
冻结包零改动。因此「镜像 == 冻结包的编码」这条**跨包**要求只能由 `tests/tracesealer/constants_contract_test.go`
（已导出常量直接比对）与 `tests/tracesealer/footer_roundtrip_test.go`（真实产物往返，人工复核项）承载，
不在本集合的变异范围内。

## 允许的绿

无（`EXPECTED_GREEN` 为空）。八条变异都必须红成**断言红**；任何编译红 / panic / 超时都要就地改写变异
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

# m04 magic 判据改成空操作 ⇒ 一段「不是本格式」的字节会被继续解析并当成行数返回。
#     杀手：TestFooterRejectsBadMagic
MUTATIONS["m04_magic判据空操作"] = [
    ("sub", "if string(b[0:4]) != footerMagic {", "if false && string(b[0:4]) != footerMagic {"),
]

# m05 版本判据改成空操作 ⇒ **未来格式**的 footer 会被按当前格式解析。
#     杀手：TestFooterRejectsUnknownFormatVersion
MUTATIONS["m05_版本判据空操作"] = [
    ("sub", "if v := binary.BigEndian.Uint16(b[4:6]); v != footerFormatVersion {",
            "if v := binary.BigEndian.Uint16(b[4:6]); false && v != footerFormatVersion {"),
]

# m06 负 row_count 判据改成空操作 ⇒ 一个自相矛盾的 footer 会被折成一个**负的行数读数**混进 A8 的分布。
#     杀手：TestFooterRejectsNegativeRowCount
MUTATIONS["m06_负行数判据空操作"] = [
    ("sub", "if rowCount < 0 {", "if false && rowCount < 0 {"),
]

# m07 「trailer 必须紧邻 footer」判据改成空操作 ⇒ footer 字段本身自洽、但与文件长度不相容的产物
#     会被当成合法（行数照读），A8 会拿一个**错位**的 footer 当样本。
#     杀手：TestFooterRejectsTrailerNotAdjacentFooter
MUTATIONS["m07_trailer紧邻判据空操作"] = [
    ("sub", "if trailerOffset+int64(trailerLen) != fileSize-int64(FooterSize) {",
            "if false && trailerOffset+int64(trailerLen) != fileSize-int64(FooterSize) {"),
]

# m08 打开文件失败时返回 (0, nil) ⇒ 「这个 part 还没封」（os.ErrNotExist）与「产物损坏」
#     （ErrFooterCorrupt）被折成同一件事：调用方再也分不出「还没好」与「坏了」，而 0 会被当成
#     「0 行」这个**合法读数**。
#     杀手：TestFooterRejectsMissingFile
MUTATIONS["m08_文件不存在返回0而不是错误"] = [
    ("sub", 'return 0, fmt.Errorf("tracesealer: 打开产物 %s 失败: %w", artifactPath, err)', "return 0, nil"),
]

# 变异测不到的那条要求（「镜像长度 == 冻结包已导出常量」）配一条**存在性**静态检查（E62：静态检查
# 只承载存在/不存在类要求；行为由在册用例承载，见 COVERAGE_GAPS）。
CHECKS = [
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
        "plan": "footer 与文件不相容（坏 magic / 未知版本 / 负 row_count / trailer 未紧邻 footer）⇒ 报错",
        "mutations": ["m04_magic判据空操作", "m05_版本判据空操作", "m06_负行数判据空操作",
                      "m07_trailer紧邻判据空操作"],
        "checks": [],
        "note": ("**行为**要求 ⇒ 在册用例 `TestFooterRejectsBadMagic` / `TestFooterRejectsUnknownFormatVersion` / "
                 "`TestFooterRejectsNegativeRowCount` / `TestFooterRejectsTrailerNotAdjacentFooter`，**每条判据各有一个"
                 "点名杀手**（各负例产物只违反一条判据，见文件头「前置的用例侧修正」）。"),
    },
    {
        "plan": "「这个 part 还没封」（文件不存在）与「产物损坏」必须分开：前者不得被折成 0 行的读数",
        "mutations": ["m08_文件不存在返回0而不是错误"],
        "checks": [],
        "note": ("**行为**要求 ⇒ 在册用例 `TestFooterRejectsMissingFile`（断言不是 ErrFooterCorrupt、且返回零值）。"),
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
        "id": "gap-1-真实产物往返测不在任何清单里",
        "title": ("`TestFooterRoundTripOnRealArtifact`（tests/tracesealer/footer_roundtrip_test.go）是本任务"
                  "**唯一**的真实产物证据，但它**故意不进任何 MANIFEST_FILES**"),
        "where": [
            "tests/tracesealer/footer_roundtrip_test.go  TestFooterRoundTripOnRealArtifact",
            "断言：用冻结包 Sealer 真封一块后，RowCount(真实产物) == 封块时的行数",
        ],
        "reachable": ("本机**可达**（zstd v1.5.7 在场），本轮实跑 PASS（见 build/vet/测试证据）；"
                      "但缺 zstd 的机器上它会 **Skip** ⇒ 放进任何清单都会让整张表在那类机器上判「证据无效」。"),
        "no_killer": (
            "它是「冻结包真的按这个布局写」的证据，不是「镜像侧某行被改坏」的证据 ⇒ 本集合的八条变异"
            "都会让它一起转红，但它**不点名**任何一条 —— 点名杀手是手工拼 footer 的用例。按 E62「行为要求"
            "必须能在某张权威表的名字清单里找到承载它的顶层用例」：这条要求的承载者是"
            "`constants_contract_test.go` 的四个常量断言（在册）+ 本条真实产物往返（**人工复核项**，"
            "与计划任务 10 步骤 1 的明文规定一致）。"
        ),
        "falsifiability": ("**本机实跑**：pristine 侧 PASS；把 m01（FooterSize→24）或 m02（偏移 −1）应用到 "
                           "`footer.go` 后重跑本条用例 ⇒ FAIL（本轮已落盘，见 `p4-task-10-probe-at-*.txt`）。"),
        "owner": "本轮只登记。若将来 CI 镜像保证装有 C 版 zstd，可把它升格进 `MANIFEST_FILES` 并纳入本集合的推导命令。",
    },
]
