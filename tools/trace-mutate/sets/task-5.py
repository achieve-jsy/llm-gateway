"""task-5 的变异集合 —— **这是作者编写的产物，因此必须 git-tracked**。

为什么它必须进版本库（与 task-6 同一条裁定）：与「名字清单」不同，「哪些守卫本该被删」
**无法从代码树里重推**。集合若留在 gitignored 的证据目录（`.superpowers/sdd/**` 被
`.superpowers/sdd/.gitignore` 整体忽略），作者可以悄悄删掉一个变异 —— 例如承载
「写失败必须回退残行」的那几条 —— 而 `git diff` 什么都看不到。
名字清单是可推导的，因此允许留在证据目录 `task-5-evidence/names.txt`；工具会打印
推导命令、三者的 sha256，并**实际重推**后逐条比对。

## 本轮集合是怎么来的（可审计性，D17）

task-5 的变异证据此前**只存在于报告散文里**（`task-5-report.md` 30 条），部分旧脚本在
`/tmp`（正在被清理）。本轮把它落成这个集合，并遵守三条纪律：

1. **在 HEAD 上重新推导**，不照抄旧数字：目标文件是当前 HEAD 的 `writer.go`
   （首轮报告之后它还经过 `771d7985` 修复轮与 `eaccd92b` 评审轮改动）。
2. **每一处被丢弃的旧编号都在 `RETIRED` 里登记理由**，绝不静默减少。
3. **只合并「同一守卫」的条目**；不同守卫/不同分支的条目一律保留，不为凑数而删。

编号沿用报告（M1–M32）。`RETIRED` 里是 7 个不再单独成条的旧编号
（m11/m12 随 `ListParts` 退役；m02/m17/m19/m23/m31 与保留条目同守卫，被合并）。
新增一条 **m33**：它是简报 `task-5-brief.md:253-254` 唯一一条变异级要求
（「删掉『当前块不存在』守卫会 nil 解引用 panic」）—— 报告散文里**没有**这一条，
即老集合在这条要求上是缺口的证据；m33 把它补上并登记进 `REQUIREMENTS`。

## 收敛到什么程度（对侦察估计 15–20 条的一处偏离，必须说明）

去重后**保留 25 条**，高于侦察给的 15–20。原因：侦察的估计按「编号」去重，
而这个文件里可变异的位置是**按守卫/分支**计的。逐条核对 HEAD 后，同一守卫的重复只有
5 处（已全部合并进 RETIRED），其余 25 条对应 25 个**不同的**代码位置/分支，
且每条都有各自独立的杀死用例（见每条的 why 注释）。
再往下压只能删掉「不同守卫」的条目 —— 正是给定优先级里明确禁止的那件事
（「不同守卫的条目不要为了凑数而删」）。因此这里如实交 25 条，并把分歧写在集合里。

## 证据级别（本轮的限制，别夸大）

本轮**只做 `--check` 定位校验 + `--selftest`**：权威表要求 `git status --porcelain`
为空，而当前有多个 agent 并发写同一 worktree ⇒ 权威重跑由控制者下一阶段统一安排。
因此下表里**每条的「转红」判定都还没有在本集合上实跑过**：它是从报告散文
（§6 全量表 / §10.4 修正表 / §11.6 十四项表）逐条映射到 HEAD 的代码位置得来的，
其中带 `[散文]` 标记的条目**照抄报告结论**，带 `[推断]` 标记的是本轮新写的等价变异
（与原编号同一守卫，但操作序列按 HEAD 重写 —— HEAD 之后的结构变了，旧文本已不可定位）。

本轮另跑过一次**非权威的辅助自检**（临时 pristine 树 + 逐条 go build/go test，
无提交绑定、不具备工具的三条判据）：基线 26 PASS/SKIP=0；25 条里
**红 24 + 绿（已登记 m13）1 + 未登记的绿 0 + 编译失败 0 + 证据无效 0**。
这一步抓到一个真实缺陷并已修：m22 的初稿把 `notes` 声明一起 cut 掉 ⇒
`undefined: notes`，那会以「红(编译失败)」的形态混进证据（编译红是弱一档的证据：
没有「用例执行 + 终判」），而工具**不会**因此判整轮失败（它落在 crash 桶）。
辅助输出见 `task-5-evidence/aux-NOT-authoritative-verdict-prediction.txt`。

另有 `COVERAGE_GAPS`（**人类可读登记，工具读不到**）：登记本轮**没有**变异覆盖的
守卫（`NewWriter` 的两个阈值默认值分支）—— 位置、是否可达、补法、该由谁补。
它与 `EXPECTED_GREEN` 分开存放，因为后者是**变异名**表，塞进无变异体的守卫会让
阶段② 的「每个 EXPECTED_GREEN 键都要落在允许的绿计数里」核对出现永远对不上的键。

集合里每条变异 = 一个「删掉/放宽某道防线」的操作序列：
  ("sub", a, b)  把 a 精确替换成 b（a 必须在目标文件里恰好出现一次）
  ("cut", a, b)  删除 a 到 b（含）之间的内容（a 必须在目标文件里恰好出现一次）

## F2a 的一处**重新定点**（唯一改动，必须可见）

F1（终审 A 缺陷 ②，`cb3253f5`）把 Inst 白名单抽成 `writer.go:74-79` 的唯一一份
`validateInst`，`NewWriter` 改为调用它。于是 m03 的原模式
`if opts.Inst != instBlue && opts.Inst != instGreen {` 在 HEAD 上 count=0 ⇒
`--check` 报 PATTERN-MISS ⇒ **task-5 整轮无法产出权威表**。F2a 把 m03 的定点
改到 NewWriter 的**调用点**（`if err := validateInst(opts.Inst); err != nil {`），
**语义与 blast radius 逐字不变**（仍只停用 NewWriter 这一处；不动 `validateInst`
本身，那会连带停用 `Seal` 的入口校验 = 另一个更大范围的变异，由 task-6 的 m55 承担）。
这是「守卫被搬家后重新定点」，**不是**「为了让表好看而放宽变异」：条目的数量、
名称、判别力与 EXPECTED_GREEN 均未变。详见 m03 条目内的注释。
"""

NAME = "task-5"
# 被变异的目标文件（相对仓库根）
TARGET = "internal/tracearchive/writer.go"
# 跑变异时涉及的 Go 包（相对仓库根，`go test` 形式）
PACKAGES = ["./internal/tracearchive/", "./tests/tracearchive/"]
# 名字清单的来源（可被第三方独立重推的文件）
MANIFEST_FILES = [
    "internal/tracearchive/writer_internal_test.go",
    "tests/tracearchive/writer_test.go",
]
# 推导命令：工具会在 pristine 树里实际执行它，并与 --manifest 逐条比对（不一致 ⇒ 证据无效）
MANIFEST_DERIVATION = (
    "grep -h '^func Test' internal/tracearchive/writer_internal_test.go "
    "tests/tracearchive/writer_test.go "
    "| sed -E 's/^func (Test[A-Za-z0-9_]*).*/\\1/' | sort"
)
# SKIP 白名单：本任务为**空**。写入器用例不依赖 zstd 二进制（那 4 条真封块的
# sealer 用例不在本清单里，-run 也不会选到它们），因此任何 SKIP 都判无效。
SKIP_WHITELIST = []

# ---------------------------------------------------------------------------
# 已退役编号（登记备查，读者不需要问「m02 / m17 / m19 / m23 / m31 去哪了」）
# ---------------------------------------------------------------------------
RETIRED = {
    "m02": (
        "与 m01_删除PendingSeals的无条件flush 同一守卫（PendingSeals 里那次 Flush 调用）。"
        "M2 的形态是「把 flush 挪到阈值判断之后、只在产出封块任务时执行」，M1 的形态是"
        "「整段删掉」。二者被同一批用例杀死（TestWriterPendingSealsFlushesBuffer、"
        "AppendFailedWritesMarkerLine…、AppendTrimsTrailingNewline、"
        "AppendSwitchesBlockOnDayChange、MarkSealedStartsNextPart）——"
        "凡是能杀死「条件 flush」的用例必然也能杀死「没有 flush」。保留更强的 M1。"
        "「flush 必须无条件」这条语义仍写在 writer.go:325-334 的方法注释里，"
        "并由 M1 转红证明它有用例撑着。"
    ),
    "m11": "随 ListParts 退役（评审裁决 2 整段删除该 API，见报告 §10.2/§7.B）：目标代码已不存在，无守卫可变异。",
    "m12": "随 ListParts 退役（同上）：目标代码已不存在，无守卫可变异。",
    "m17": (
        "与 m18_validateDT放宽为只查长度 同一守卫（validateDT 的格式校验）。"
        "M17 的形态是「删掉 Append 里的 validateDT 调用」，M18 的形态是「把 time.Parse "
        "放宽为长度判断」。**M18 的断言严格强于 M17**：M18 的变异体位于 validateDT 内部，"
        "它转红本身就证明该函数确实在 Append/AppendFailed 的调用路径上（否则改它不会有"
        "任何可观测差异）；同时 M18 还覆盖「必须是合法日期而不仅是长度」这一更强要求"
        "（用例里的 \"2026-13-01\" 长度合法、日期非法）。保留 M18。"
    ),
    "m19": (
        "与 m20 + m21 同一守卫（validateLine 的两条分支）：删掉 Append 里的 validateLine "
        "调用 == 同时删掉两条分支（该函数只被这一个入口调用），因此 M19 = M20 ∪ M21，"
        "没有自己的独立断言。保留两条更细的分支条目（各自被一条用例杀死，"
        "见 TestWriterAppendRejectsInteriorNewline / TestWriterAppendRejectsEmptyLine）。"
    ),
    "m23": (
        "与 m26_隔离名直接拼接 同一守卫（poisonCurLocked 的改名隔离）。"
        "M23 的形态是「原地改名（等于不隔离）」，M26 的形态是「隔离名不经 partName 生成」。"
        "在 HEAD 上两者被同一组用例杀死：隔离件路径读不到（"
        "TestWriterWriteFailureLeavesNoResidualLine）＋ 序号 N 被复用"
        "（TestWriterPoisonedBlockCannotBeSealed）。M26 还额外证明「文件真的被搬走了」，"
        "故保留 M26。"
    ),
    "m31": "与 m04_字节计数漏掉换行字节 是同一条变异（同一行 `cur.bytes += int64(n) + 1`），重复项删除。",
}

# 单一事实源：本任务的全部变异（25 条）
MUTATIONS = {}

# ---- PendingSeals：无条件 flush（缺陷 A / D3，报告缺陷 A）----
MUTATIONS["m01_删除PendingSeals的无条件flush"] = [
    # [散文] 报告 §6 M1：PendingSeals 是方案 C 下网关唯一的周期性 flush 入口，
    # 删掉它 ⇒ sealer 读到不完整/空的 spool。杀死 TestWriterPendingSealsFlushesBuffer
    # 及 5 条依赖 flush 才看得到盘上内容的用例。
    ("sub", '\tif err := cur.w.Flush(); err != nil {\n'
            '\t\treturn nil, fmt.Errorf("tracearchive: flush 写缓冲失败: %w", err)\n'
            '\t}\n', ''),
]

# ---- 路径分量校验：inst / dt / root（三处独立守卫）----
MUTATIONS["m03_停用Inst白名单"] = [
    # [散文] M3：Inst 直接拼进目录名（<root>/dt=D/inst=I），只查非空挡不住
    # "../../green" 这类含斜杠的值。杀死 TestNewWriterRejectsIllegalInst
    # （末尾的反向断言保证「一律拒绝」不算通过）。
    #
    # 【F2a 重新定点：守卫被搬家，不是被删除】F1（终审 A 缺陷 ②，提交 cb3253f5）
    # 把 Inst 白名单抽成 writer.go:74-79 的唯一一份 `validateInst`，NewWriter 改为
    # **调用**它（writer.go:211 `if err := validateInst(opts.Inst); err != nil`）。
    # 于是原位模式 `if opts.Inst != instBlue && opts.Inst != instGreen {` 在 HEAD 上
    # count=0 ⇒ PATTERN-MISS（F2a 的 `--check` 实测）⇒ task-5 **整轮无法运行**。
    # 本条目因此**只换定点、不换语义**：仍然只停用 **NewWriter 这一处**的判定
    #（把分支条件与 false 相与），blast radius 与 F1 之前逐字一致 —— 刻意**不**改
    # `validateInst` 本身，那会连带停用 (*Sealer).Seal 的入口校验（更大范围的变异，
    # 已由 task-6 的 m55 单独承担）。判别力不变：仍只有
    # TestNewWriterRejectsIllegalInst 被杀死。
    ("sub", 'if err := validateInst(opts.Inst); err != nil {',
            'if err := validateInst(opts.Inst); err != nil && false {'),
]
MUTATIONS["m14_停用Root非空校验"] = [
    # [散文] M14：Root 为空时 paths 全是相对路径，会把正文写进进程 cwd。
    # 杀死 TestNewWriterRejectsEmptyRootAndInst 的第一段。
    ("sub", 'if opts.Root == "" {', 'if false {'),
]
MUTATIONS["m18_validateDT放宽为只查长度"] = [
    # [散文] M18：把 time.Parse("2006-01-02", dt) 换成长度判断。
    # 杀死 TestWriterAppendRejectsIllegalDT：用例里有 "2026-13-01"（10 字节、
    # 非法月份）与 "../x"/"2026-9-24"（长度不符）两侧输入，长度判断只挡得住后者。
    # 注意：**不要**改成 `if false`（那就是被退役的 M17），本条的判别力恰恰在于
    # 「放行了长度合法但日期非法的输入」。
    ("sub", '\tif _, err := time.Parse(dateLayout, dt); err != nil {\n'
            '\t\treturn fmt.Errorf("tracearchive: dt 必须是 %s 形式的合法日期，实际 %q", dateLayout, dt)\n'
            '\t}\n',
            '\tif len(dt) != len(dateLayout) {\n'
            '\t\treturn fmt.Errorf("tracearchive: dt 必须是 %s 形式的合法日期，实际 %q", dateLayout, dt)\n'
            '\t}\n'),
]

# ---- validateLine：两条分支各一条（调用被删 = 两条都删，已退役为 m19）----
MUTATIONS["m20_删除validateLine的行内换行分支"] = [
    # [散文] M20：删掉 IndexAny("\r\n") 分支。承重不变量「一次 Append == 一条物理行」
    # 由此失守 ⇒ 杀死 TestWriterAppendRejectsInteriorNewline。
    # 只删这一条分支不动空行分支，是为了让「哪条规则失守」在表里可分辨。
    ("cut", '\tif i := bytes.IndexAny(body, "\\r\\n"); i >= 0 {',
            '\t\t\t"换行只能由调用方序列化为 \\\\n", i, body[i])\n\t}\n'),
]
MUTATIONS["m21_删除validateLine的空行分支"] = [
    # [散文] M21：空行不是合法 JSON，按行解析 .jsonl 的工具会在它上面炸掉
    # （这也是 failedRowMarker 不取空行的理由）。杀死 TestWriterAppendRejectsEmptyLine。
    ("sub", '\tif len(body) == 0 {\n'
            '\t\treturn fmt.Errorf("tracearchive: 正文不能是空行（去掉末尾换行后长度为 0）")\n'
            '\t}\n', ''),
]

# ---- trimNewline：末尾换行是分隔符不是内容 ----
MUTATIONS["m05_trimNewline不再去CR"] = [
    # [散文] M5：只去 '\n' 不去 '\r' ⇒ "a\r\n" 变成 "a\r"，随后被行内换行守卫拒绝
    # （契约是「带尾换行的调用与不带等价」）。杀死 TestWriterAppendTrimsTrailingNewline。
    ("sub", "\tfor len(b) > 0 && (b[len(b)-1] == '\\n' || b[len(b)-1] == '\\r') {",
            "\tfor len(b) > 0 && b[len(b)-1] == '\\n' {"),
]

# ---- 行号/字节计数与块切换 ----
MUTATIONS["m04_字节计数漏掉换行字节"] = [
    # [散文] M4（= 报告 M31，同一条变异）：UncompressedBytes 必须等于盘上 spool
    # 字节数（含每行结尾的换行），否则任务 6 的压缩比标定与行数交叉校验都错。
    # 杀死 TestWriterPendingSealsReportsTaskFields、TestWriterMarkerLineCountsRowsAndBytes。
    ("sub", '\tcur.bytes += int64(n) + 1\n', '\tcur.bytes += int64(n)\n'),
]
MUTATIONS["m15_SealTaskRows报错一位"] = [
    # [散文] M15：封块任务的 Rows 报错一位。杀死 TestWriterPendingSealsBySize（want 4）
    # 与 TestWriterPendingSealsReportsTaskFields。
    ("sub", '\t\tRows:              cur.rows,\n', '\t\tRows:              cur.rows + 1,\n'),
]
MUTATIONS["m06_换块不再比较dt"] = [
    # [散文] M6：块以 (dt, inst) 为界。不比较 dt ⇒ 25 日的正文追加进 24 日的文件，
    # 点读按 (dt,inst,part,row) 永远取不到它，sealer 也会把它封进错误的那一天。
    # 杀死 TestWriterAppendSwitchesBlockOnDayChange（row 不归零即红）。
    ("sub", 'if w.cur != nil && w.cur.dt == dt && !w.cur.poisoned {',
            'if w.cur != nil && !w.cur.poisoned {'),
]

# ---- 标记行（缺陷 C：AppendFailed 必须落盘且占行号）----
MUTATIONS["m07_AppendFailed只推进行号不落盘"] = [
    # [散文] M7：退回首轮实现（缺陷 C）—— 只推进行号、不写字节 ⇒ 其后每一行
    # 「row=N 读成第 N+1 行」，静默错正文；残余矛盾最终由任务 6 的行数交叉校验兜。
    # 这里**保留** validateDT 调用：本条的判别力要落在「标记行没落盘」这一点上，
    # 不要与 m18 的 dt 校验混在一处（旧 M7 顺带让非法 dt 也被放过，那是副作用）。
    # 杀死 AppendFailedWritesMarkerLine…（spool 行数/内容）、MarkerLineCountsRowsAndBytes
    # （字节数）、AppendReportsUnusableSpoolDir（cur==nil 时静默返回 nil）。
    ("sub", '\tif _, err := w.Append(dt, failedRowMarker); err != nil {\n'
            '\t\treturn fmt.Errorf("tracearchive: 写正文缺失标记行失败: %w", err)\n'
            '\t}\n\treturn nil',
            '\tif err := validateDT(dt); err != nil {\n'
            '\t\treturn err\n\t}\n'
            '\tw.mu.Lock()\n\tdefer w.mu.Unlock()\n'
            '\tif w.cur == nil {\n\t\treturn nil\n\t}\n'
            '\tw.cur.rows++\n\treturn nil'),
]
MUTATIONS["m08_标记行不推进行号"] = [
    # [散文] M8：写了标记行但不占行号 —— 与 m07 是同一契约的另一半
    # （m07 是「不落盘」，本条是「不占行号」），两条各自被
    # TestWriterAppendFailedWritesMarkerLineAndAdvancesRow 的**不同断言**杀死，
    # 因此不合并。HEAD 的 AppendFailed 复用 appendLineLocked，没有独立的行号推进
    # 语句，故用「追加后回退一次 rows」表达这个语义（等价于首轮的只写不推）。
    ("sub", '\tif _, err := w.Append(dt, failedRowMarker); err != nil {\n'
            '\t\treturn fmt.Errorf("tracearchive: 写正文缺失标记行失败: %w", err)\n'
            '\t}\n\treturn nil',
            '\tif _, err := w.Append(dt, failedRowMarker); err != nil {\n'
            '\t\treturn fmt.Errorf("tracearchive: 写正文缺失标记行失败: %w", err)\n'
            '\t}\n'
            '\tw.mu.Lock()\n\tw.cur.rows--\n\tw.mu.Unlock()\n\treturn nil'),
]
MUTATIONS["m16_标记行改成空行"] = [
    # [散文] M16：标记行写成空行 ⇒ 空行不是合法 JSON，且会被 validateLine 自己拒绝
    # （AppendFailed 直接返回错误）。杀死 AppendFailedWritesMarkerLine… 与
    # AppendRejectsEmptyLine 的反向断言（「标记行应被接受」）。
    ("sub", 'var failedRowMarker = []byte("null")', 'var failedRowMarker = []byte("")'),
]

# ---- MarkSealed：三个子守卫（生效 / 匹配条件 / 关闭旧句柄）----
MUTATIONS["m09_停用MarkSealed的清除"] = [
    # [散文] M9：整段停用 ⇒ 封块后的新正文会被追加进一个已被封块、随时会被删除的
    # spool（那些行永远进不了产物）。杀死 TestWriterMarkSealedStartsNextPart。
    ("cut", '\tif w.cur != nil && w.cur.dt == dt && w.cur.part == part {',
            '\t\t_ = w.closeCurLocked()\n\t}\n'),
]
MUTATIONS["m29_MarkSealed不再比较dt与part"] = [
    # [散文] M29：无条件清除 ⇒ 一个错配的封块回调（另一个 part / 另一个 dt）会让写入器
    # 丢掉当前块状态。杀死 TestWriterMarkSealedIgnoresNonMatchingPart。
    # 与 m09 同一行但**交错覆盖不同用例**（m09 杀 StartsNextPart，本条杀
    # IgnoresNonMatchingPart；把 m09 的变异体套在 m29 的用例上反而全绿），故不合并。
    ("sub", 'if w.cur != nil && w.cur.dt == dt && w.cur.part == part {', 'if w.cur != nil {'),
]
MUTATIONS["m27_MarkSealed只置nil不关句柄"] = [
    # [散文] M27（= 报告缺陷 B）：只把 cur 置 nil ⇒ 每封一块泄漏一个 fd，长跑进程终
    # EMFILE。fd 不经公开 API 暴露，只有包内用例能观察。杀死
    # TestWriterMarkSealedClosesPreviousFileHandle。
    ("sub", '\tif w.cur != nil && w.cur.dt == dt && w.cur.part == part {\n'
            '\t\t_ = w.closeCurLocked()\n\t}\n',
            '\tif w.cur != nil && w.cur.dt == dt && w.cur.part == part {\n'
            '\t\tw.cur = nil\n\t}\n'),
]

# ---- 写失败：回退残行 → 隔离改名 → 失败退路 → 毒化 ----
MUTATIONS["m22_删除残行回退调用"] = [
    # [散文] M22：不调 truncateToLastNewline ⇒ 短写留下的残行被留在 spool 里，
    # 封块侧永远封不掉、读取侧整体错位。杀死 TestWriterWriteFailureLeavesNoResidualLine
    # （隔离件内容应为 "line0\n"，实际 "line0\nlin"）。
    # 只删那一次调用与它的两行处理，**保留** notes 的声明：声明后面还有
    # `if len(notes) > 0` 在用（把声明一起删掉会让变异体不编译 —— 辅助自检实测
    # `writer.go:567:32: undefined: notes`，那会把「编译红」混进证据）。
    ("cut", '\tif err := truncateToLastNewline(spoolPath); err != nil {',
            '\t\tnotes = append(notes, fmt.Sprintf("回退残行失败: %v", err))\n\t}\n'),
]
MUTATIONS["m24_删除隔离失败的清空退路"] = [
    # [散文] M24：改名失败时不清空 spool ⇒ 一个「带着残行、看起来正常」的 .jsonl
    # 留在 sealer 的发现范围里。杀死 TestWriterQuarantineRenameFailureEmptiesSpool。
    # 注意本变异**不动** truncateToLastNewline：改名前 spool 已被回退到 6 字节
    # （"line0\n"），清空退路才是这条用例唯一能分辨的那一步。
    ("cut", '\t\t// 退路：清空文件。0 字节 spool 里没有任何残行，只会被 sealer 封成 0 行',
            '\t\t}\n'),
]
MUTATIONS["m25_停用毒化块的复用判断"] = [
    # [散文] M25：ensureBlockLocked 的第一条判断里去掉 `!w.cur.poisoned` ⇒ 毒化块被
    # 复用：后续 Append 继续往被隔离的（bufio 已粘性出错的）块里写，既不换 part 也
    # 不放弃。杀死 TestWriterQuarantineRenameFailureEmptiesSpool 与
    # TestWriterPoisonedBlockCannotBeSealed 的「自动换到 N+1」断言。
    # 与 m06 同一行的**另一个合取项**（dt 身份 vs 毒化），两者互不覆盖，故都保留。
    ("sub", 'if w.cur != nil && w.cur.dt == dt && !w.cur.poisoned {',
            'if w.cur != nil && w.cur.dt == dt {'),
]
MUTATIONS["m26_隔离名直接拼接"] = [
    # [散文] M26：把 quarantineSuffix 直接拼到 spool 名后面 ⇒ part-000000.jsonl.jsonl.zst，
    # parsePartSeq 拒绝它 ⇒ 序号 N 不再被占住、会被下一个块复用（最坏的一类错：
    # PG 里旧的 (part=N,row=r) 静默读到另一个 part 的正文）。
    # 【为什么变异点在**调用处**而不是 quarantinePath 内部】包内用例用
    # quarantinePath() 自己算出期望路径；若把后缀拼在 quarantinePath 内部，用例的期望
    # 会跟着变异体一起漂移（自遮蔽），只有 parsePartSeq 那一条断言还抓得住。
    # 变异在调用处则保留了「改名目标必须是 partName(part, quarantineSuffix)」的完整判别力：
    # 隔离件在期望路径下不存在 + 序号被复用，两侧都红。
    ("sub", '\tisolated := quarantinePath(spoolPath, ref.Part)\n',
            '\tisolated := spoolPath + quarantineSuffix\n'),
]
MUTATIONS["m28_PendingSeals不再跳过毒化块"] = [
    # [散文] M28：毒化块的 rows 与盘上完整行数已不可信（短写可能吃掉一条已成功行的
    # 尾部），且它的文件已被隔离成 .jsonl.zst；不再跳过它 ⇒ PendingSeals 会对粘性出错的
    # bufio 做 Flush 并报错（或把不可信的行数交给 sealer）。杀死
    # TestWriterPoisonedBlockCannotBeSealed 的「毒化块的 PendingSeals 不该报错」断言。
    ("cut", '\t// 毒化块不可封块：它的 rows 与盘上完整行数已不可信（短写可能吃掉一条',
            '\tif cur.poisoned {\n\t\treturn nil, nil\n\t}\n'),
]

# ---- truncateToLastNewline：没有行边界时的终局退路 ----
MUTATIONS["m32_删除无行边界时的整文件清空"] = [
    # [散文] M32（复审第 5 项新增分支）：整个文件一个 '\n' 都没有（首次追加即失败）
    # 时必须清空为 0 字节，否则那条半行会被当成第 0 行返回。
    # 杀死 TestWriterTruncatesWholeFileWhenNoLineBoundaryExists。
    # 与 m22 是**不同代码位置的两条不同退路**（调用 vs 无边界分支），故都保留。
    ("sub", '\treturn os.Truncate(path, 0)\n}', '\treturn nil\n}'),
]

# ---- Close / closeCurLocked 的 nil 安全 ----
MUTATIONS["m10_删除closeCurLocked的nil守卫"] = [
    # [散文] M10：没有当前块时 Close（以及 closeCurLocked 的任何调用点）会解引用空 cur。
    # 报告记的是 panic。杀死 TestWriterCloseIsIdempotentWithoutBlock
    # （也顺带打死所有 `defer w.Close()` 时 cur 仍为 nil 的用例）。
    ("sub", '\tif w.cur == nil {\n\t\treturn nil\n\t}\n'
            '\tif w.cur.poisoned {\n\t\treturn w.dropCurLocked()\n\t}\n',
            '\tif w.cur.poisoned {\n\t\treturn w.dropCurLocked()\n\t}\n'),
]

# ---- PendingSeals 的空块守卫：**期望绿（零覆盖）**，见 EXPECTED_GREEN ----
MUTATIONS["m13_删除PendingSeals的空块守卫"] = [
    # [散文] M13：报告 §6/§10.4 记的判定是**仍全绿（无区分）**，理由见 §7.A。
    # 保留这条变异（而不是「知道会绿就不写」）本身就是证据：
    # 「这个守卫没有被任何用例约束」这句话由此可复核，而不是一句声明；
    # 不是「空块守卫删了也没事」——它只是**经公开 API 不可达**（写失败会先毒化）。
    ("cut", '\t// 空块（建了文件但一行都没写进去）没有可封的内容。',
            '\tif cur.rows == 0 {\n\t\treturn nil, nil\n\t}\n'),
]

# ---- PendingSeals 的「当前块不存在」守卫（简报唯一的变异级要求）----
MUTATIONS["m33_删除PendingSeals的当前块nil守卫"] = [
    # [推断] 简报 task-5-brief.md:253-254：TestWriterPendingSealsNilBeforeAnyAppend
    # 覆盖「当前块不存在」守卫，「删掉该守卫会立刻 nil 解引用 panic」。
    # 这是本任务简报里**唯一**一条变异级要求（全文 grep「变异/守卫/人工检查」只命中
    # 253-254 两行），报告散文的 30 条里**没有**它 —— 也就是说老集合在这条要求上
    # 是缺口的证据，故本轮补为 m33 并登记进 REQUIREMENTS。
    # 预期判定：红（崩溃/中止）—— cur 为 nil 时 `cur.poisoned` 立即 panic。
    ("sub", '\tcur := w.cur\n\tif cur == nil {\n\t\treturn nil, nil\n\t}\n', '\tcur := w.cur\n'),
]

# ---------------------------------------------------------------------------
# 允许的绿：预期**不转红**的条目。每条都必须写明依据；未登记的绿 ⇒ 整轮失败。
# 分类标签（与 task-6 同一套口径）：
#   [零覆盖]   某行为存在但没有用例约束它 ⇒ **测试缺陷**，必须有保留理由
#   [等价变异] 该行在框架/标准库契约下冗余 ⇒ 关于代码本身的发现，不是测试缺陷
# ---------------------------------------------------------------------------
EXPECTED_GREEN = {
    "m13_删除PendingSeals的空块守卫": (
        "[零覆盖] 测试不可达的纵深防御，**有意保留**（不是缺陷，是已登记的裁决）。"
        "经公开 API，「rows == 0 且未毒化」这个状态不可达：唯一能造出「空块」的路径是"
        "「建块成功但 Append 写失败」，而写失败会先把该块毒化，PendingSeals 在更早的"
        " `cur.poisoned` 分支就返回了（writer.go:347-349）——删掉 rows==0 守卫后，"
        "先到的那条 poisoned 分支仍然拦住所有既有用例，因此本变异**仍全绿**。"
        "保留它的理由（不让封块器拿到 0 行 spool：0 行产物会让该 part 的所有点读变成"
        "行号越界）已经写在**代码注释本身**（writer.go:353-358，含「本条不可达」的"
        "声明），不再只存在于报告散文里；报告侧见 §7.A 与 §11.7.3。"
        "【不是等价变异】该行在语义上并非冗余（若能造出空块，它确实改变行为），"
        "杀不死的原因纯粹是**没有用例构造该状态**，故归类为 [零覆盖]。"
    ),
}

# ---------------------------------------------------------------------------
# 零变异覆盖的守卫登记（**只是给人看的登记，工具读不到、不参与任何判定**）
#
# 为什么单独一个结构、而不是写进 EXPECTED_GREEN：EXPECTED_GREEN 的键被工具当作
# **变异名**处理，阶段② 还要核对「每个键都确实落在『允许的绿』计数里」——
# 把「没有变异体的守卫」塞进去，会让那张核对表出现一个永远对不上的键。因此本结构
# 与 task-6.py 的 PENDING 同一用途：进版本库、可被 git diff 审计的人类可读登记。
#
# 登记口径与 m13 的 EXPECTED_GREEN 条目对齐：位置 / 是否可达 / 缺的是什么 / 谁来补。
# 裁决（控制者，本轮）：**不新增条目**，但必须登记 —— 一个守卫没有变异覆盖
# 不自动等于缺陷（m13 就是「不可达 + 有意保留」的那类）。
# ---------------------------------------------------------------------------
COVERAGE_GAPS = [
    {
        "id": "gap-1-NewWriter两个阈值默认值分支零变异覆盖",
        "title": ("NewWriter 的「BlockBytes/SealInterval 为 0 取默认值」两处赋值"
                  "没有任何变异条目（旧 30 条里也没有）"),
        "where": [
            "internal/tracearchive/writer.go:196-198  if opts.BlockBytes <= 0 { opts.BlockBytes = defaultBlockBytes }",
            "internal/tracearchive/writer.go:199-201  if opts.SealInterval <= 0 { opts.SealInterval = defaultSealInterval }",
        ],
        "reachable": (
            "**可达**，且两处都已被用例约束：TestWriterPendingSealsAppliesDefaultThresholds "
            "（不传任何阈值）断言「刚写入一行不应封块」。删掉任一条赋值会让该阈值变成 0："
            "bySize 的 `cur.bytes >= 0` / byTime 的 `time.Since(...) >= 0` 恒真 ⇒ 第一次 "
            "PendingSeals 就报出 1 个任务 ⇒ 该用例的 `len(tasks) != 0` 断言转红。"
            "因此缺的**不是覆盖**，而是「变异证据」——即没有一条变异证明这个用例真的在"
            "约束这两行（若这两行将来被删，只有这条用例会红，而表里没有对应条目）。"
        ),
        "how_to_close": (
            "补两条变异即可（编号建议 m34/m35，实际由补条者定；两条各自独立、不要合并，"
            "因为两个阈值是两处独立守卫）：\n"
            "  (\"cut\", '\\tif opts.BlockBytes <= 0 {', '\\t\\topts.BlockBytes = defaultBlockBytes\\n\\t}\\n')\n"
            "  (\"cut\", '\\tif opts.SealInterval <= 0 {', '\\t\\topts.SealInterval = defaultSealInterval\\n\\t}\\n')\n"
            "补条时请把本字段的依据写进那两条的注释，并跑一次 --check。"
        ),
        "aux_verified": (
            "两条建议的 pattern 本轮已**实测**（非权威、无提交绑定：临时 pristine 树；"
            "writer.go 在 a2bbfdf8..aceb59ee 之间未变）：各自在 HEAD 上唯一定位、编译通过、"
            "判定为「红」且杀死用例恰为 TestWriterPendingSealsAppliesDefaultThresholds"
            "（PASS=25 SKIP=0 FAIL=1）。⇒ 补这两条只需照抄 pattern，判定不需要猜。"
            "（权威表仍须由工具在干净树上产出。）"
        ),
        "why_not_this_round": (
            "控制者裁决：本阶段只在「旧 30 条 + 简报要求」范围内收敛，不扩范围 "
            "（我此前按「不编造、不扩范围」把它作为待决项上报，未自行补）。"
            "注意它**不属于** m13 那种「不可达的纵深防御」：它是可达的、有用例的，"
            "缺的是变异证据本身 —— 归类上更接近「旧证据的强度被高估」"
            "（与 brief:253-254 那条缺口同类，后者已由 m33 关闭）。"
        ),
        "owner": "R11 / 阶段② 控制者（登记进 R11 报告与终审输入）",
        "this_round": "仅登记，不补条目（裁决：不新增）",
    },
]

# ---------------------------------------------------------------------------
# 静态检查（追加 G）：**变异测不到**的要求必须有静态检查行。
# 本任务为空，理由如下（而不是「忘了写」）：
#   - task-5 简报里唯一一条变异级要求（:253-254）已被 m33 用变异覆盖，
#     没有 task-6 那种「压缩比断言抓不到、计划自己要求另有人工检查」的要求；
#   - 报告 §7.A 登记的那些不可达/不可观测分支（Append 的两个 bufio 写错误分支、
#     PendingSeals 的 flush 错误分支、ensureBlockLocked 的 f.Stat 错误分支、
#     MarkSealed 的 flush+close 在公开 API 下不可观测）属于「零覆盖的防御性守卫」，
#     它们的处置是**保留 + 在代码注释里写明理由**，不是「有要求却无人执行」，
#     因此没有对应的静态检查行可写。
# ---------------------------------------------------------------------------
CHECKS = []

# ---------------------------------------------------------------------------
# 映射表（追加 G）：计划写的每条变异要求 → 集合里的对应条目。
# 工具会校验：① 每个 requirement 至少有一个 mutation 或 check；
#             ② 引用的名字必须真实存在；③ 列出「没有任何 requirement 引用」的变异。
# ---------------------------------------------------------------------------
REQUIREMENTS = [
    {
        "plan": ("task-5-brief.md:253-254 —— 写入器必须对「当前块不存在」的守卫有回归保护："
                 "「删掉该守卫会立刻 nil 解引用 panic」，TestWriterPendingSealsNilBeforeAnyAppend "
                 "就是那道防线的回归保护"),
        "mutations": ["m33_删除PendingSeals的当前块nil守卫"],
        "checks": [],
        "note": ("这是简报里**唯一**一条变异级要求（grep 全文「变异/守卫/人工检查」只命中这两行），"
                 "简报**没有**逐条变异清单 ⇒ 如实登记这一条泛判据，不编造其余条目。"
                 "其余 24 条来自 task-5-report.md 的散文（§6 全量表 16 条、§10.4 修正表 M17/M18、"
                 "§11.6 十四项表，去重后 30 条）与其中登记的评审裁决；"
                 "工具会把它们列为「未被任何计划要求引用」——那是散文来源，不是缺口。"
                 "反过来，本表也暴露了旧集合的一处真实缺口：简报这条要求原先把 30 条里「一条都没有」覆盖"
                 "（旧 M10 删的是 closeCurLocked 的 nil 守卫，不是 PendingSeals 的），"
                 "m33 是本轮补上的证据。"),
    },
]
