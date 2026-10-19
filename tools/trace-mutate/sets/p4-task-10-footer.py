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

## 与计划文本的一致性（如实登记：两处**新增**（m04–m08 ＋ E76② 的 m09），零处削弱）

计划任务 10 步骤 5 的 `p4-task-10-footer.py` 逐字给出三条变异与各自杀手（m01–m03），本文件逐字采用，
并**新增** m04–m08：把 footer 的五条字段自洽判据各自改成空操作；**E76② 再新增** m09：
把 row_count 的字段偏移 `b[18:22]` 错写成 trailer_len 的 `b[14:18]`（两者都是 4 字节
`uint32`、类型相同 ⇒ **编译通过**，只能红在断言上）。

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

## 与冻结包的边界（如实登记；**E73① 已修**）

`internal/tracearchive/**` 是本计划的**冻结面**：本集合只变异**镜像侧**（`internal/tracesealer/footer.go`），
冻结包零改动。因此「镜像 == 冻结包的编码」这条**跨包**要求由三处承载：

1. `tests/tracesealer/constants_contract_test.go`（已导出常量直接比对；在 `p4-task-10-contract` 的清单里）；
2. **`tests/tracesealer/footer_artifact_test.go` 的 `TestFooterParsesCommittedRealArtifact`（在册）** ——
   在**已提交的真实产物 fixture**（`tests/tracesealer/testdata/footer_roundtrip_real.jsonl.zst`，冻结
   Sealer 预产、104 字节）上，既用镜像 `RowCount` 读行数，又用**冻结读侧** `GetBody` 逐字节取回三行
   原文 ⇒ 「冻结包真按此布局写」这条**行为**要求第一次进了自动门禁（m01/m02 直取它）。
   **E73① 的修法**：原来承载它的 `TestFooterRoundTripOnRealArtifact` 要**当场用 zstd 封一块**
   ⇒ 缺 zstd 的机器上会 Skip ⇒ 进不了任何清单；**「需要 zstd」不是正当理由**（解析产物不需要 zstd，
   只有产出需要），故改成「预先产好并提交产物 + 不依赖 zstd 的解析用例」；
3. `tests/tracesealer/footer_roundtrip_test.go` 的 `TestFooterRoundTripOnRealArtifact`（**仍未进任何清单**）
   —— 它是**产出方向**的人工复核项：在本机有 zstd 时验证「冻结 Sealer 真能封出可解析的产物」。
   它不承载「镜像解析真实产物」这条要求（那条已由 2 承接），故不再构成 E62 缺口。

## 允许的绿

无（`EXPECTED_GREEN` 为空）。九条变异都必须红成**断言红**；任何编译红 / panic / 超时都要就地改写变异
字符串（E70 约定 2），不得登记为允许的绿。
"""

NAME = "p4-task-10-footer"
TARGET = "internal/tracesealer/footer.go"
PACKAGES = ["./tests/tracesealer/"]
# E76② 核对（**清单无需变更**）：m09 的点名杀手 `TestFooterRowCountOnHandBuiltFooter`
# 在 `footer_test.go` 里 ⇒ 它**已经**在本清单中；本清单与 names 清单的**内容**在 E76②
# 一字未动（工具仍按 MANIFEST_DERIVATION 现推并逐条比对）。E76② 改的是 MUTATIONS（8 → 9 条）。
MANIFEST_FILES = [
    "tests/tracesealer/footer_test.go",
    "tests/tracesealer/footer_artifact_test.go",
]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对
MANIFEST_DERIVATION = (
    "grep -h '^func Test' tests/tracesealer/footer_test.go tests/tracesealer/footer_artifact_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# 全部是纯文件读写（手工拼 32 字节 footer + 解析**已提交的**真实产物 fixture）：
# 不依赖 zstd、不依赖现场封块，没有可 SKIP 的理由
SKIP_WHITELIST = []
RETIRED = {}
EXPECTED_GREEN = {}

MUTATIONS = {}

# m01 footer 长度 32 → 24 ⇒ 末尾 24 字节被当成定长 footer：magic 落在 trailer_offset 字段里，
#     解析出的「行数」与真实行数无关（本用例里直接报错，因为 magic 不符）。
#     杀手：TestFooterRowCountOnHandBuiltFooter；另 TestFooterParsesCommittedRealArtifact
#     （E73① 新增的在册用例：同一偏移错位让它在解析真实产物处转红）
MUTATIONS["m01_footer长度32改24"] = [
    ("sub", "FooterSize = 32", "FooterSize = 24"),
]

# m02 读取偏移差一个字节（size-32 → size-33）⇒ magic 不再落在 footer 首位 ⇒ 每块都读不出来。
#     杀手：TestFooterRowCountOnHandBuiltFooter；另 TestFooterParsesCommittedRealArtifact
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

# m09（**E76② 新增**）row_count 的字段偏移写成 trailer_len 的偏移（`b[18:22]` → `b[14:18]`）。
#     `trailer_len` 与 `row_count` 都是 4 字节 `uint32`、类型相同 ⇒ **编译通过**；但读出来的数
#     就是**另一个字段**。手工拼的 footer 里 trailer_len=0、row_count=4242 ⇒ 解析结果 0 ≠ 4242。
#     杀手：TestFooterRowCountOnHandBuiltFooter（在册：`buildFooter(4242, 4096-32, 0)`，即
#     trailer_len=0 ≠ row_count=4242）。这就是探针 P3 曾登记为「覆盖缺口」的那条判据 ——
#     E76② 之后它**正式在册**（见 REQUIREMENTS 与 COVERAGE_GAPS gap-2、报告 §5.5 的 E76③ 收窄）。
MUTATIONS["m09_row_count偏移改成trailer_len偏移"] = [
    ("sub", "b[18:22]", "b[14:18]"),
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
        "why": ("**产出方向**的人工复核项（需要本机 zstd：当场用冻结 Sealer 封一块）。它**仍未进任何 "
                "MANIFEST_FILES**（缺 zstd 的机器上会 Skip ⇒ 塞进清单会让整张表判「证据无效」）⇒ 存在性由静态"
                "检查钉住。**E73① 之后**：它**不再**是「冻结包真按此布局写」这条行为要求的唯一承载者 —— "
                "该要求由在册用例 `TestFooterParsesCommittedRealArtifact`（`footer_artifact_test.go`，"
                "已提交的真实产物 fixture、不依赖 zstd）承载，故本条不再构成 E62 缺口。"),
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
        "plan": "row_count 必须读 footer 的 [18:22) —— 不得错读成同为 4 字节的 trailer_len [14:18)（E76② 新增）",
        "mutations": ["m09_row_count偏移改成trailer_len偏移"],
        "checks": [],
        "note": ("**行为**要求 ⇒ 在册用例 `TestFooterRowCountOnHandBuiltFooter`"
                 "（`buildFooter(4242, 4096-32, 0)`：trailer_len = 0 ≠ row_count = 4242；"
                 "换偏移后读出 0 ⇒ 与 4242 不等 ⇒ 断言红）。"
                 "**它正是探针 P3 / 报告 §5.5.5 曾登记为「覆盖缺口」的那条判据**：已提交的真实产物 "
                 "fixture 的 trailer_len == row_count == 3 ⇒ 在 `-run TestFooterParsesCommittedRealArtifact` "
                 "的单测口径下区分不了；**包级**由本变异 + 本用例承重 ⇒ E76② 之后该判据正式在册"
                 "（见 COVERAGE_GAPS gap-2 与报告 §5.5 的 E76③ 收窄）。"),
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
                 "另有**产出方向**的人工复核项 `TestFooterRoundTripOnRealArtifact`（**仍未进任何 MANIFEST_FILES**，"
                 "需 zstd）。两条用例的**存在性**分别由同名静态检查钉住。"),
    },
    {
        "plan": "**冻结包真按此布局写**（E62 / E73① 的修法）：在**已提交的真实产物**上，"
                "镜像 `RowCount` 读出封块时的行数，且冻结读侧 `GetBody` 逐字节取回每一行原文",
        "mutations": ["m01_footer长度32改24", "m02_读取偏移差一个字节"],
        "checks": [],
        "note": ("**行为**要求 ⇒ 在册用例 `TestFooterParsesCommittedRealArtifact`（`footer_artifact_test.go`，"
                 "**在名字清单里**，每次运行都真的执行、不依赖 zstd）。它同时钉住 fixture 的 sha256（换掉字节会"
                 "先红）与越界行号报 `ErrNotArchived`。m01/m02 改坏镜像的偏移/长度 ⇒ 该用例的 `RowCount` 断言转红"
                 "（实测见权威表的「杀死的用例」栏：两条变异都把本用例列进 FAIL 集）。"),
    },
]

# ---------------------------------------------------------------------------
# 零变异覆盖的**要求/用例**登记（E64②；**只是给人看的登记，工具读不到、不参与任何判定**）
# ---------------------------------------------------------------------------
COVERAGE_GAPS = [
    {
        "id": "gap-1-真实产物往返测（产出方向）不在任何清单里",
        "title": ("`TestFooterRoundTripOnRealArtifact`（tests/tracesealer/footer_roundtrip_test.go）是**产出方向**"
                  "的人工复核项，它**仍未进任何 MANIFEST_FILES**（E73① 之后它不再唯一承载任何行为要求）"),
        "where": [
            "tests/tracesealer/footer_roundtrip_test.go  TestFooterRoundTripOnRealArtifact",
            "断言：用冻结包 Sealer **当场封一块**后，RowCount(真实产物) == 封块时的行数",
        ],
        "reachable": ("本机**可达**（zstd v1.5.7 在场），本轮实跑 PASS（见 build/vet/测试证据）；"
                      "但缺 zstd 的机器上它会 **Skip** ⇒ 放进任何清单都会让整张表在那类机器上判「证据无效」。"),
        "no_killer": (
            "**E73① 之前的缺口**：它是「冻结包真的按这个布局写」的唯一证据，而它在任何清单之外 ⇒ 违反 E62。"
            "**E73① 的修法**：把产物**预先产好并提交**（`testdata/footer_roundtrip_real.jsonl.zst`），"
            "新增在册用例 `TestFooterParsesCommittedRealArtifact` 解析它（**不需要 zstd**），并登记进本集合的"
            "`MANIFEST_FILES` ⇒ 「镜像解析真实产物」这条行为要求已进自动门禁。**仍然留在清单外的是产出方向**："
            "「冻结 Sealer 当场封块」这一步只有装 zstd 的机器能跑，它不承载镜像侧的任何判定 ⇒ 作为人工复核项保留。"
        ),
        "falsifiability": ("**本机实跑**：pristine 侧 PASS；把 m01（FooterSize→24）或 m02（偏移 −1）应用到 "
                          "`footer.go` 后重跑本条用例 ⇒ FAIL（落盘见 `p4-task-10-probe-at-*.txt`）。"
                          "**在册的那一条**（`TestFooterParsesCommittedRealArtifact`）的红由本集合的权威表"
                          "「杀死的用例」栏直接给出（m01/m02 都点名它）。"),
        "owner": ("本轮只登记：产出方向保留为人工复核项。升格路径 = CI 镜像保证装有 C 版 zstd 且断言"
                  "「zstd 缺失 ⇒ FAIL 而不是 Skip」，或把产出步骤移到一个单独的、带 zstd 的 job。"),
    },
    {
        "id": "gap-2-row_count偏移与trailer_len偏移互串（**E76② 后已关闭**）",
        "title": ("**改写此前的「覆盖缺口」登记**（原登记不在本文件里，而在交付报告 §5.5 第 5 条第二点"
                  "与探针记录 `p4-task-12-probe-at-5650d28c.txt` 的 P3）："
                  "「把 `footer.go` 的 row_count 偏移 `b[18:22]` 错写成 trailer_len 的 `b[14:18]`」"
                  "这条判据**现已正式在册** —— E76② 新增变异 `m09_row_count偏移改成trailer_len偏移`，"
                  "点名杀手 = 在册用例 `TestFooterRowCountOnHandBuiltFooter`（名字清单里、每次运行都真的执行）。"
                  "⇒ 该判据**不再是**覆盖缺口。"),
        "where": [
            "tools/trace-mutate/sets/p4-task-10-footer.py  MUTATIONS[\"m09_row_count偏移改成trailer_len偏移\"]",
            "tests/tracesealer/footer_test.go:79  TestFooterRowCountOnHandBuiltFooter"
            "（:81 buildFooter(4242, 4096-32, 0) ⇒ trailer_len=0；:85 断言解析值 == 4242）",
            "internal/tracesealer/footer.go:107  rowCount := int32(binary.BigEndian.Uint32(b[18:22]))",
        ],
        "reachable": ("本机**可达**：`m09` 的权威表判定 = 红（**断言红**，非编译红）；"
                      "names=8 ran=8 diff=0 PASS=6 SKIP=0 FAIL=2。"
                      "**杀死的用例** = `TestFooterRowCountOnHandBuiltFooter`（点名杀手）"
                      "＋ `TestFooterRejectsNegativeRowCount`（字段互串后负 row_count 读不出来）。"),
        "no_killer": (
            "**改写前（原登记）**：探针 P3 把第 107 行 `b[18:22]` 改成 `b[14:18]` 后在**已提交的真实产物 fixture** 上"
            "「读不出差别、用例仍绿」⇒ 登记为覆盖缺口。**改写后（E76②）**：该负结果**只在 "
            "`-run TestFooterParsesCommittedRealArtifact` 的单测口径下**成立（该 fixture 的 "
            "trailer_len == row_count == 3 ⇒ 换偏移读出同一个数）；**包级**由 "
            "`TestFooterRowCountOnHandBuiltFooter` 承重（手工拼的 footer：trailer_len = 0 ≠ row_count = 4242）"
            "⇒ 该判据已在册、有断言红变异。**不再**称其为「不可证伪的覆盖缺口」。"
        ),
        "falsifiability": (
            "**E76② 实跑（`go test -overlay` 打补丁，不动工作树）**：\n"
            "  · 包级：`names=8 ran=8 PASS=6 SKIP=0 FAIL=2` ⇒ exit=1，`--- FAIL: TestFooterRowCountOnHandBuiltFooter`\n"
            "    ＋ `--- FAIL: TestFooterRejectsNegativeRowCount`（无编译错误行）；\n"
            "  · 单测口径：`-run '^TestFooterRowCountOnHandBuiltFooter$'` ⇒ exit=1、"
            "`footer_test.go:85` 报 `expected: 4242 / actual: 0` ⇒ **断言红**。\n"
            "  权威表（整集合）见 `p4-task-10-footer-authoritative-at-<sha>.txt` 的 m09 行；\n"
            "  E62 探针（改坏 → 转红 → 还原 + 还原后 sha256）见 `e76-e62-probe-at-<sha>.txt`。"
        ),
        "owner": ("E76② 关闭本缺口。**残留边界**（明确、不夸大）：已提交 fixture 的 "
                  "trailer_len == row_count == 3 ⇒ **该 fixture 的单测**区分不了两字段；"
                  "这不影响包级判定（由手工拼 footer 的用例承重），也不再是「无承载者」的缺口。"),
    },
]
