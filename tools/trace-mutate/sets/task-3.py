"""task-3 的变异集合 —— **这是作者编写的产物，因此必须 git-tracked**。

为什么它必须进版本库（追加 F 的裁决）：与「名字清单」不同，「哪些守卫本该被删」
**无法从代码树里重推**。集合若留在 gitignored 的证据目录，作者可以悄悄删掉一个
变异（例如承载「布局静默错位」的那条 m03），而 `git diff` 什么都看不到。
名字清单是可推导的，因此允许留在证据目录 —— 前提是调用方打印推导命令与哈希。

本集合的对象是 **32 字节定长 footer 的编解码与校验**（`internal/tracearchive/footer.go`），
它此前只有散文记录（`task-3-report.md:288-294`）；本文件把那次手工实验落成
**可重跑、绑定提交**的集合。编号与简报里的代号对应关系：

    m01_A  删掉负值 / 负 row_count 守卫
    m02_B  五处 %w 全部退化 %v（错误身份）
    m03_C  对称调换 [14:18) trailer_len 与 [18:22) row_count 的**读写两侧**

每条变异 = 一个「删掉/放宽某道防线」的操作序列：
  ("sub", a, b)  把 a 精确替换成 b（a 必须在目标文件里恰好出现一次）
  ("cut", a, b)  删除 a 到 b（含）之间的内容（a 必须在目标文件里恰好出现一次）

【本集合做到 / 没做到的事，先写在最前面，避免读者误会】

1. **PACKAGES 与侦察给的参数不同，且这是有证据的更正**：侦察给的是
   `./internal/tracearchive/ ./tests/tracearchive/`，但本任务的 8 条用例**全部**
   只存在于 `internal/tracearchive/footer_test.go`；把 `./tests/tracearchive/` 也列进
   包列表，`go test` 会对那个包打印 `testing: warning: no tests to run` /
   `ok ... [no tests to run]`，而工具把 `no tests to run` 作为**先判 → 证据无效**
   （trace_mutate.py:188-189、:226-227）。实测（HEAD 的 pristine 树）：

       $ go test ./internal/tracearchive/ ./tests/tracearchive/ -run '<8 条用例>' -v -count=1
       ok  github.com/example/llm-gateway/internal/tracearchive  0.006s
       testing: warning: no tests to run
       ok  github.com/example/llm-gateway/tests/tracearchive  0.007s [no tests to run]

   ⇒ 照抄那个包列表，本任务**永远**只能得到「证据无效」，拿不到任何权威表。
   因此这里只列 `./internal/tracearchive/`。
   （这与 task-8.py 顶部那条 ⚠️ 是同一类陷阱：包列表必须与清单的实际所在包一致。）

2. **简报写的 golden 用例名不存在**：`task-3-brief.md:267` 要求「调换 [14:18) 与
   [18:22) … → `TestFooterGoldenLayout` 必须转红」。当前树上没有 `TestFooterGoldenLayout`
   （那是 task-6 的 `TestSealGoldenLayout`），**真实名字是 `TestFooterGoldenBytes`**
   （`internal/tracearchive/footer_test.go:35`）。本集合的 MANIFEST 里没有前者。

3. **m02_B 是「五处一起改」的并集，不证明逐处独立**：简报说「把五处 %w **任一**改成
   %v → 对应用例必须转红」，而 B 一次改全部五处 ⇒ 它证明的是「五处都退化时 5 条拒绝
   用例全红」，**没有**证明「每处各自有一条专属断言承重」。若要与 task-6 的
   m05/m08/m12（一条哨兵一条变异）同强度，下一轮应把 B 拆成 5 条。本轮按裁决保持 3 条。

4. **m03_C 是本集合唯一能验证 `[14:18)`/`[18:22)` 字段顺序的条目**，且它是**静默错误**：
   trailer_len 与 row_count 都是 4 字节整数、类型相同 ⇒ 编译通过、往返自洽。
   因此 C **必须同时改读写两侧**；只改一侧会退化成与 A/B 无关的普通变异
   （编码侧错位会被解码侧的自洽性掩盖成「往返仍绿、只有 golden 红」，而只改一侧
   则连 golden 之外的语义都被破坏，反而失去了「对称错位」这个形态本身的意义）。
"""

NAME = "task-3"
# 被变异的目标文件（相对仓库根）
TARGET = "internal/tracearchive/footer.go"
# 跑变异时涉及的 Go 包（相对仓库根，`go test` 形式）
# ⚠️ 只列 ./internal/tracearchive/：8 条用例全在该包；列上 ./tests/tracearchive/
# 会得到 `[no tests to run]` ⇒ 工具判**证据无效**（见文件头第 1 条，含实测输出）。
PACKAGES = ["./internal/tracearchive/"]
# 名字清单的来源（可被第三方独立重推的文件）
MANIFEST_FILES = ["internal/tracearchive/footer_test.go"]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对（不一致 ⇒ 证据无效）
MANIFEST_DERIVATION = (
    "grep -h '^func Test' internal/tracearchive/footer_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# SKIP 白名单：本任务为**空** —— 这 8 条用例不依赖 zstd/外部二进制，
# 任何 SKIP 都说明「没跑」，必须判无效（默认空 ⇒ 任何 SKIP 都判无效）。
SKIP_WHITELIST = []

# 已退役编号：本集合首次落地，无退役项。显式留空以便第三方核对「没有隐藏的历史编号」。
RETIRED = {}

# ---------------------------------------------------------------------------
# 单一事实源：本任务的全部变异
# ---------------------------------------------------------------------------
MUTATIONS = {}

# ---- m01_A：删掉全部负值守卫（footer.go:72-77）----
# 一次删掉两个 if 块：`TrailerOffset<0 || TrailerLen<0` 与 `RowCount<0`。
# 这两处是「被截断/损坏产物上真实可达的输入」的判据，且各自有专属断言：
#   - TestDecodeFooterRejectsNegativeOffsetOrLen（2 个子例）
#   - TestDecodeFooterRejectsNegativeRowCount
# 删掉后，负值会被当成业务事实返回给调用方（Task 7 的 Reader 靠
# errors.Is(err, ErrFooterCorrupt) 区分「产物损坏」与「数据不存在」）。
MUTATIONS["m01_A_删除负值与负row_count守卫"] = [
    ("cut", '\tif f.TrailerOffset < 0 || f.TrailerLen < 0 {',
            '\t\treturn f, fmt.Errorf("%w: 负 row_count %d", ErrFooterCorrupt, f.RowCount)\n\t}\n'),
]

# ---- m02_B：五处错误身份 `%w` → `%v` ----
# 分别对应 footer.go 的 5 处 fmt.Errorf：长度、magic、format_version、
# 负偏移/负长度、负 row_count。全部改完后 ErrFooterCorrupt 不再被 wrap ⇒
# 5 条拒绝类用例的 errors.Is 断言全红（RoundTrip/Golden/RowCountRoundTrip 不带错误断言，仍绿）。
MUTATIONS["m02_B_五处w改v错误身份退化"] = [
    ("sub", 'fmt.Errorf("%w: 长度应为', 'fmt.Errorf("%v: 长度应为'),
    ("sub", 'fmt.Errorf("%w: magic 不符', 'fmt.Errorf("%v: magic 不符'),
    ("sub", 'fmt.Errorf("%w: 未知 format_version', 'fmt.Errorf("%v: 未知 format_version'),
    ("sub", 'fmt.Errorf("%w: 负偏移或负长度', 'fmt.Errorf("%v: 负偏移或负长度'),
    ("sub", 'fmt.Errorf("%w: 负 row_count', 'fmt.Errorf("%v: 负 row_count'),
]

# ---- m03_C：对称调换 [14:18) trailer_len ↔ [18:22) row_count ----
# 两个字段都是 4 字节整数、类型相同（int32/uint32）⇒ 互换算术切片**照常编译**，
# 而且编码与解码同时改 ⇒ 往返自洽。
#
# 逐个用例推演（这是「只有 golden 能杀它」的依据，必须写下来，否则读表的人
# 无法判断这条 C 到底证明了什么）：
#   * TestFooterRoundTrip          ：编码/解码用同一张错表 ⇒ got == in ⇒ 绿
#   * TestFooterRowCountRoundTrip  ：RowCount 写 [14:18)、读 [14:18) ⇒ 仍等于 n ⇒ 绿
#   * …RejectsNegativeRowCount     ：RowCount=-1 落在 [14:18) 读到仍是 -1 ⇒ 守卫照常命中 ⇒ 绿
#   * …RejectsNegativeOffsetOrLen  ：负 trailer_len=-1 落在 [18:22) 读到仍是 -1 ⇒ 绿
#   * …RejectsWrongLength/BadMagic/UnknownVersion：不涉及这两个字段 ⇒ 绿
#   * TestFooterGoldenBytes        ：手写期望字节 [14:18)=0x0A0B0C0D（len）、
#                                    [18:22)=0x0E0F1011（row）⇒ **红**
# 因此 C 是「读写自洽、往返全绿」的布局 bug 的唯一探针，也正是
# task-3-report.md:288-294 里 C 实验的价值所在。
#
# 【杀伤范围的**准确**边界（实测，不是推断）】
#   - 在**本 MANIFEST 的 8 条**范围内：只有 TestFooterGoldenBytes 红（PASS=7/FAIL=1）。
#   - 在**全仓**范围内：还有一条**不在本清单里**的用例也红 ——
#     `tests/tracearchive/reader_test.go:597 TestReaderRejectsTrailerRowCountMismatch`。
#     它红在**自己的前置断言**上（:607 手写 `b[fo+18:fo+22] == 3` 探测 row_count 偏移），
#     即另一个「原始字节锚点」，而不是红在它想验证的 trailer 交叉校验上。
#   所以「只有 golden 能杀 C」这句话**只在 task-3 的清单范围内成立**；本集合的
#   PACKAGES 也只覆盖内部包 ⇒ 该用例不参与本表。这一点写在这里，是为了避免
#   读者把它当成「全仓唯一探针」这种过强结论。
MUTATIONS["m03_C_对称调换trailer_len与row_count"] = [
    # 写入侧：两行成对交换（放在同一个 sub 里，避免中间态被其它模式命中）
    ("sub", '\tbinary.BigEndian.PutUint32(b[14:18], uint32(f.TrailerLen))\n'
            '\tbinary.BigEndian.PutUint32(b[18:22], uint32(f.RowCount))\n',
            '\tbinary.BigEndian.PutUint32(b[14:18], uint32(f.RowCount))\n'
            '\tbinary.BigEndian.PutUint32(b[18:22], uint32(f.TrailerLen))\n'),
    # 读取侧：与写入侧**对称**地交换（少了这一段，C 就不再是「对称错位」）
    ("sub", '\tf.TrailerLen = int32(binary.BigEndian.Uint32(b[14:18]))\n'
            '\tf.RowCount = int32(binary.BigEndian.Uint32(b[18:22]))\n',
            '\tf.RowCount = int32(binary.BigEndian.Uint32(b[14:18]))\n'
            '\tf.TrailerLen = int32(binary.BigEndian.Uint32(b[18:22]))\n'),
]

# ---------------------------------------------------------------------------
# 允许的绿：本集合**没有**登记任何条目 —— 三条变异都预期转红（m01/m02 由断言红，
# m03 由 TestFooterGoldenBytes 红）。未登记的绿 ⇒ 整轮失败，这与「本任务三条防线
# 都有专属断言承重」的结论一致。若将来某条判绿，必须在这里补登记并给出分类：
#   [零覆盖]（行为存在但无用例约束 ⇒ 测试缺陷）/ [等价变异]（代码冗余 ⇒ 代码发现）。
# ---------------------------------------------------------------------------
EXPECTED_GREEN = {}

# ---------------------------------------------------------------------------
# 静态检查（追加 G）：**变异测不到**的要求必须有静态检查行。
# ---------------------------------------------------------------------------
CHECKS = [
    {
        "name": "footer-golden-bytes锚点存在",
        "file": "internal/tracearchive/footer_test.go",
        "pattern": r"0x0E, 0x0F, 0x10, 0x11",
        "expect": "present",
        "why": (
            "m03_C 的全部杀伤力都来自 TestFooterGoldenBytes 这份**不依赖被测实现**的"
            "手写期望字节（footer_test.go:42-50）。若这份期望被删掉或退化（例如照抄 "
            "encodeFooter 的输出），C 立刻变成**零覆盖的绿**，而往返类用例永远发现不了"
            "（它们只用同一张错表自证自洽）。这是一条「变异自己测不到」的元要求，"
            "因此做成静态检查行：`[18:22) row_count` 的手写期望必须还在。"
            "本行的 pattern 就是期望字节里 row_count 那 4 个字节。"
        ),
    },
    {
        "name": "reader-row_count原始偏移锚点存在",
        "file": "tests/tracearchive/reader_test.go",
        "pattern": r"fo\+18 : fo\+22",
        "expect": "present",
        "why": (
            "第二个**原始字节锚点**：TestReaderRejectsTrailerRowCountMismatch 在 "
            "reader_test.go:607 直接读 `b[fo+18:fo+22]` 断言 row_count==3，再把它改成 2。"
            "它不在本集合的 MANIFEST 里（清单只含 footer_test.go 的 8 条），但实测它是"
            "全仓范围内除 golden 之外**唯一**还会被 m03_C 打红的用例 —— 而且红在它自己的"
            "前置断言上。检查它存在 = 守住「[18:22) 是 row_count」这条布局事实在大包测试里"
            "仍有一处独立见证；若将来它被改成用 decodeFooter 构造，本条会 FAIL 并提醒复核。"
        ),
    },
]

# ---------------------------------------------------------------------------
# 映射表（追加 G）：计划写的每条变异要求 → 集合/静态检查里的对应条目。
# 来源 = task-3-brief.md:267 的「变异自检」三句（本任务全部要求就这三条）。
# 工具会校验：① 每个 requirement 至少有一个 mutation 或 check；
#             ② 引用的名字必须真实存在；③ 列出「没有任何 requirement 引用」的变异。
# ---------------------------------------------------------------------------
REQUIREMENTS = [
    {
        "plan": "task-3-brief.md:267 删掉 decodeFooter 里的负值检查 → TestDecodeFooterRejectsNegativeOffsetOrLen 必须转红",
        "mutations": ["m01_A_删除负值与负row_count守卫"],
        "checks": [],
        "note": (
            "A 一次删掉 footer.go:72-77 两个 if 块（负 offset/负 len + 负 row_count），"
            "因此预期由**两条**用例分别承重：RejectsNegativeOffsetOrLen（2 个子例）与 "
            "RejectsNegativeRowCount。这比简报的措辞更严（简报只点了前者）。"
        ),
    },
    {
        "plan": "task-3-brief.md:267 把五处 %w 任一改成 %v → 对应用例必须转红（错误身份）",
        "mutations": ["m02_B_五处w改v错误身份退化"],
        "checks": [],
        "note": (
            "B 是**并集变异**（五处一起改），预期 5 条拒绝类用例全红（含 3 个子例）。"
            "它没有证明「每一处各自有一条专属断言」—— 见文件头第 3 条；"
            "若要同 task-6 的 m05/m08/m12 同强度，下一轮拆成 5 条。"
        ),
    },
    {
        "plan": "task-3-brief.md:267 调换 [14:18) 与 [18:22) 的写入/读取顺序 → golden 布局用例必须转红（往返测试不会）",
        "mutations": ["m03_C_对称调换trailer_len与row_count"],
        "checks": ["footer-golden-bytes锚点存在", "reader-row_count原始偏移锚点存在"],
        "note": (
            "简报写的 TestFooterGoldenLayout **在本树上不存在**，真实名字是 "
            "TestFooterGoldenBytes。C 同时改读写两侧（编码 2 行 + 解码 2 行），"
            "是本集合唯一能验证字段顺序的条目；两条静态检查行分别守住它的两个原始字节锚点。"
            "实测杀伤范围：清单内只有 GoldenBytes（7 PASS/1 FAIL）；全仓另有一条清单外的 "
            "TestReaderRejectsTrailerRowCountMismatch 红在其前置断言上。"
        ),
    },
]
