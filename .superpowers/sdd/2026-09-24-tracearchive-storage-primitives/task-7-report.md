# Task 7 报告：按定位键点读（含 spool 兜底）

**分支**：`feat/trace-body-direct-write`
**提交**：`f7033774`（`feat(tracearchive): 按定位键点读（含 spool 兜底与错误分类）`，5 files, +1197）
       + `b691ab11`（`test(tracearchive): 补 A 层用例覆盖「等待并发槽位期间被取消」`，1 file, +73/-1）
**本报告对应的最终状态**：Task 7 的文件止于 `b691ab11`；`git status --porcelain` 为空。
（本报告写完后，分支上的 HEAD 变成 `0c420547`——那是**另一个 agent** 的计划文档提交，只改了
`.superpowers/` 下的 `.md`，**未触碰** Task 7 的五个被测文件：`git diff --stat b691ab11..HEAD -- <这五个文件>` 输出 0 行，
故在 `b691ab11` 上跑出的全部运行对当时 HEAD 的被测树依然成立。）
证据与提交的绑定见 `task-7-evidence/PROVENANCE.txt`（含 `HEAD`、`git diff --stat b691ab11..HEAD` 的核对结果、被测文件 blob 哈希、各归档 sha256）。**注意：该文件是人手写的、无生成脚本，其内容是声明而非工具产出的事实——它能证明「有人核对过」，不能证明「归档确由那份脚本、在那棵树、按那份输入产出」，详见 §11 第 5 条。**
**工作树**：`/Users/jsy/Public/self_code/llm-gateway/.worktrees/trace-body-direct-write`
**规格**：`docs/superpowers/specs/2026-09-23-trace-body-direct-write-design.md` §8（8.1~8.4）、§7.1（标记行）
**证据目录**：`.superpowers/sdd/2026-09-24-tracearchive-storage-primitives/task-7-evidence/`

> 本报告里每条事实性论断都带 `文件:行号` 或归档原始输出指针。归档与报告冲突时以归档为准。
> 归档原始输出（本目录内，均为未经编辑的 `go test` 输出）：
>
> | 文件 | 内容 | sha256 前 16 |
> |---|---|---|
> | `red-step2-build-failed.txt` | 步骤 2 红灯（编译失败）原始输出（**历史：实现前的状态，不是最终状态的运行**） | `2766af2ef742adcc` |
> | `green-run-22-verbose.txt` | **权威**绿灯：`-run <22 名单> -v -count=1`（最终提交 `b691ab11` 之后跑的） | `c0b7d7bb82e04bc9` |
> | `VOID-green-run-21-verbose.txt` | **已作废**：清单为 21 条那一轮的绿灯输出（不含 A 层第 3 条用例） | `1dddafb5189b8e2b` |
> | `three-assertions-green.txt` | 对绿灯输出的四断言计算 | — |
> | `mutation-table.txt` | 32 条变异逐条自证表 | `5664c8580ce599bd` |
> | `r10-two-sided.txt` | R10 三条守卫的双侧完整原始输出（**已被 22 条清单重跑覆盖**，报告内旧行号已按现文件更正） | `026fbfeb38cf1ccd` |
> | `raw-mNN.txt` | 每条变异的完整原始输出（32 个文件） | — |
> | `fullsuite-104-top-level-verbose.txt` | **权威**两包**全量**（无 `-run`）原始输出（顶层 104 + 子 37） | — |
> | `VOID-fullsuite-103-top-level-verbose.txt` | **已作废**：顶层为 103 的那一轮全量输出 | `f1df8f7e8000aea6` |

---

## 1. 状态：改了/建了哪些文件

| 文件 | 行数 | 说明 |
|---|---|---|
| `internal/tracearchive/reader.go` | 320（新建） | `ReaderOptions`/`Reader`/`NewReader`/`GetBody`/`Close`/`maxTrailerBytes` + `readFromPart`/`readFromSpool`/`verifyFrameSHA`/`isFailedRowMarker` |
| `tests/tracearchive/reader_test.go` | 701（新建） | B 层（`package tracearchive_test`，仅公开 API）19 条顶层用例 + `trunc`/`dirEntries`/`incompressibleBlob` 辅助 |
| `internal/tracearchive/reader_internal_test.go` | 191（新建 119 + 补 73） | A 层 3 条顶层用例（第 3 条见 §4.2 与 §6.4） |
| `tests/tracearchive/helpers_test.go` | 168（+34） | 追加 B 层 `sealLines`（含 `context` 导入） |
| `internal/tracearchive/testhelpers_test.go` | 152（+23） | 追加 A 层 `sealLines`（同上） |

提交：`f7033774`（5 files, +1197）+ `b691ab11`（1 file, +73/-1）。两个提交后 `git status --porcelain` 均为空；此后未再改动受版本控制的文件（`.superpowers/` 被该目录内 `.gitignore` 忽略，见 `.superpowers/sdd/.gitignore:1`）。（**本轮更正例外**：为把这份报告纳入版本控制，报告文件本身被 `git add -f` 加入跟踪，见提交 `62572d42`；证据工具 `task-7-mutate.py` 与全部 `raw-*` 归档仍被忽略。上面这句「此后未再改动受版本控制的文件」指 `b691ab11` 之后、本轮更正之前的那段时间。）
**提交 2 的来由**：协调者在裁决里邀请补一条 A 层用例覆盖此前唯一的零覆盖分支（`reader.go:112-113` 的 `ctx.Done()`）。它**超出简报对 A 层「两条」的限定**，故单独成一个提交、不动 `f7033774`（该提交此前被我 amend 过一次，发现违反「不改既有提交」后已 `git reset --hard f7033774` 复原，最终历史里它是独立提交）。

---

## 2. 测试：计数与四断言原始输出

**两条命令、两套计数（顶层与子用例分开报）**

| 运行 | 顶层用例 | 子用例 | SKIP | FAIL | 结论 |
|---|---|---|---|---|---|
| `-run <22 名单> -count=1 -v`（两包合并） | 22 执行 / 22 PASS | 0 | 0 | 0 | 绿 |
| 两包**全量**（无 `-run`）`-count=1 -v` | 104 执行 / 104 PASS | 37 执行 / 37 PASS | 0 | 0 | 绿 |
| 步骤 2 红灯（实现前） | 0 执行 | 0 | 0 | `[build failed]` ×2 | **无效**（计数为 0 ⇒ 绝不算绿） |

顶层与子用例的计数命令（原始输出见 `three-assertions-green.txt` / `fullsuite-104-top-level-verbose.txt`）：

```
grep -cE '^=== RUN   Test[A-Za-z0-9_]+$'      # 顶层
grep -cE '^=== RUN   Test[A-Za-z0-9_]+/'      # 子
grep -cE '^--- PASS: Test[A-Za-z0-9_]+ '      # 顶层 PASS（带尾随空格，排除子用例）
grep -cE '^[[:space:]]+--- PASS: Test[A-Za-z0-9_]+/'   # 子用例 PASS（缩进）
```

**四断言的原始输出**（`three-assertions-green.txt`，`bash three-assertions.sh` 生成）：

```
== 断言 1：正则集合 == 清单，且实际执行集合 == 清单 ==
正则集合 vs 清单：diff 为空 ✓
实际执行集合条数 = 22
执行集合 vs 清单：diff 为空 ✓

== 断言 2：顶层 PASS（带尾随空格）行数 == 清单大小 ==
顶层 --- PASS 行数 = 22，清单 = 22，相等=成立✓
顶层 --- FAIL 行数 = 0
子用例（顶层/子）终判行数 = 0

== 断言 3：SKIP == 0 ==
SKIP = 0

== 断言 4：执行到的顶层用例总数 > 0 ==
=== RUN 顶层行数 = 22

== [no tests to run] 检查 ==
0
```

* 断言 1 的两侧都做了：`printf '%s' "$RUN" | tr -d '[:space:]' | tr '|' '\n' | sort` 与内嵌清单 `diff` 为空；`=== RUN` 实测集合与内嵌清单 `diff` 也为空。
* `SKIP == 0` 关键（`tests/tracearchive/helpers_test.go:38-44` 自己写明「未找到 zstd 则 skip」）：本机 `/opt/homebrew/bin/zstd` 为 C 版 v1.5.7，真封块用例全部实际执行。变异脚本的自检 C 用合成输出验证了「全 SKIP 会被判为**证据无效**而不是绿」。
* 步骤 2 红灯的原始输出 `red-step2-build-failed.txt`：`undefined: ta.NewReader`、`undefined: ta.ReaderOptions`、`undefined: maxTrailerBytes`，`=== RUN` 行数 = 0，退出码 1。**0 个用例执行 ⇒ 无效，而不是绿**。

**分包的顶层用例数**：`internal/tracearchive`（A 层）3 条（`reader_internal_test.go:48`、`:103`、`:138`——均指函数声明行）；`tests/tracearchive`（B 层）19 条（`reader_test.go`）。两包都无子用例（`t.Run`）。

---

## 3. R10 三项：落地位置 + 删掉后哪条用例转红

### 3.1 R10 第 1 项：`readFromSpool` 拒绝未以换行符结尾的末行

| 项 | 位置 |
|---|---|
| 守卫代码 | `internal/tracearchive/reader.go:289-292`（在 `readFromSpool`，`reader.go:274`） |
| 判据 | `errors.Is(readErr, io.EOF)` ⟺ 封块侧 `scanSpoolOffsets` 的「EOF 时仍带回了非空字节」（`internal/tracearchive/sealer.go:266-274`） |
| 归入身份 | `ErrNotArchived`（500 类「元数据与存储不一致」），并在注释里说明为什么不是 `ErrBodyMissing`（404） |
| 用例 | `tests/tracearchive/reader_test.go:230`（`TestReaderRejectsUnterminatedSpoolTail`） |
| 形态 | spool 内容 `{"row":0}\n{"row":1}\n{"row":2`（末行故意不带 `\n`，`reader_test.go:238`） |

**删掉该守卫：两条变异都只杀这一条用例，且失败信息直接暴露两种坏法**

* `m04_删除spool残行守卫`（只删守卫，保留下面的 `if readErr != nil` 报错）：

```
    reader_test.go:255: 残行属「存储状态异常」（ErrNotArchived/500 类），实际 tracearchive: 读 spool 失败: EOF
--- FAIL: TestReaderRejectsUnterminatedSpoolTail (0.00s)
```

* `m32_残行按正文返回`（连 `io.EOF` 也放行，落到 `return trimNewline(line), nil`——**简报点名禁止的形态**）：

```
    reader_test.go:252: 末行未以换行结尾时必须拒绝读取，却把残行当正文返回了: "{\"row\":2"
--- FAIL: TestReaderRejectsUnterminatedSpoolTail (0.00s)
```

原始输出：`task-7-evidence/r10-two-sided.txt:168`（m04）、`:223`（m32）；完整输出 `raw-m04.txt`、`raw-m32.txt`。

**本用例如何区分「拒绝」与「trim 后当正文返回」**：三条断言按顺序落在不同坏法上——

1. `reader_test.go:251-253`：`err == nil` 且 `got` 非空 ⇒ 打印**被误当正文返回的残行字节**。这就是「trim 后当正文返回」；m32 命中的正是这一条（`却把残行当正文返回了: "{\"row":2"`）。
2. `reader_test.go:254-256`：有错误但身份不是 `ErrNotArchived`（`errors.Is`）⇒ 拒绝是拒了，但把「存储状态异常」混成了别的 500/404 身份；m04 命中这一条（错误变成 `读 spool 失败: EOF`）。
3. `reader_test.go:257-259`：反向断言，错误**不得**携带 `ErrBodyMissing` 身份（残行不是「正文写入时即失败」，不能混进 404/`body_missing` 语义）。
4. `reader_test.go:261-269`：正向对照——残行**之前**的完整行（row=0）必须照常读到。没有这一条，「凡有残行就一律报错」也能全绿，而且会把「活跃 spool 的正常读取」（见下方 §3.1.1）误伤成 500。

#### 3.1.1 与规格文字的一处差异（已在代码注释与本节登记）

规格 §8.2 第 4 步写的是「末行若未以换行符结尾 ⇒ **拒绝读取**」。我把它落实为**拒绝「被请求的那一行恰是残行」**，而不是「整份 spool 一律拒绝」（`reader.go:266-273` 的注释）。理由：

* 残行**恒是文件的最后一条物理行**——写失败会毒化该块并换到新 part（`internal/tracearchive/writer.go:540-574`），进程重启后 `nextSeqLocked` 也一定开新 part（`writer.go:483-500`，目录内容即写指针）。所以残行之前每一行的物理序与 `row_seq` 仍然一一对应。
* 反过来，「整份拒绝」会**误伤活跃块**：Writer 用 1 MiB 的 `bufio`（`writer.go:474`），一条正文大于缓冲余量时一次 flush 会停在**行中间**，于是文件最后一字节在两次 flush 之间天然不是 `\n`——那是正常写入的中间态，不是事故。整份拒绝会让这种窗口里的任何点读变成 500。
* 若上游要求严格按规格文字「整份拒绝」，这是**一行改动**（把守卫从「目标行分支内」移到顺序扫描的每个分支之前）；但那样必须同时接受上面那个误伤窗口。**这条差异请评审裁决。**

### 3.2 R10 第 2 项：读取产物时校验 footer 的 `frame_sha256[:8]`

| 项 | 位置 |
|---|---|
| 校验实现 | `internal/tracearchive/reader.go:234-251`（`verifyFrameSHA`） |
| 调用点 | `internal/tracearchive/reader.go:171-173`（在 `trailer_len` 上界之后、任何大分配与解压之前） |
| 范围 | 流式 `io.Copy(hasher, io.LimitReader(f, ft.TrailerOffset))`（`reader.go:241`），峰值内存为常数、不落临时文件（§8.3 C1/C2/C3） |
| 身份 | 不符 ⇒ `ErrFooterCorrupt`（`reader.go:246-249`） |
| 用例 | `tests/tracearchive/reader_test.go:279`（`TestReaderVerifiesFrameSHA`）、`reader_test.go:331`（`TestReaderVerifiesFrameSHABeyondDecodedRegion`） |

**删掉该校验（`m02_删除frame_sha比对`）⇒ 两条用例都转红**：

```
    reader_test.go:312: 帧字节被改而 footer 的 frame_sha256 未更新时必须返回 ErrFooterCorrupt（产物损坏），实际 tracearchive: 解压读取失败: CRC check failed
--- FAIL: TestReaderVerifiesFrameSHA (0.10s)
    reader_test.go:367: 帧尾位腐被静默接受了：读 row=0 竟然成功——frame_sha256 校验没有生效
--- FAIL: TestReaderVerifiesFrameSHABeyondDecodedRegion (0.33s)
```

原始输出：`task-7-evidence/r10-two-sided.txt:112`、`raw-m02.txt`。

**为什么要第二条用例（这条是本任务里最值得看的一处实验）**：第一轮变异跑完时我发现，`TestReaderVerifiesFrameSHA` 改的是**帧的最后一个字节**、而帧区只有几 KB ⇒ Reader 的 1 MiB `bufio` 一次向解码器要满 1 MiB，解码器顺势把整帧解完，**zstd 自身的内容校验和（CRC）先报了错**（错误身份是「解压读取失败」，不是 `ErrFooterCorrupt`）。也就是说：删掉 `frame_sha256` 校验后读取**仍然失败**，只是错误身份变了——那条用例并不能证明「这个字段非读不可」。

第二条用例把帧区做到 **3 MiB**（48 × 64 KiB 的不可压缩字节，`reader_test.go:333-335`；生成器 `incompressibleBlob` 见 `reader_test.go:377-393`，用 SHA-256 链生成、并避开 `\n`/`\r`），只改**帧区最后一个字节**，然后读 `row=0`：点读只会解到前约 1 MiB，**最后一个 zstd 块永远不会被解码**，CRC 也永远不触发。于是：

* 有校验 ⇒ `ErrFooterCorrupt`（覆盖整个帧区，与「这次解到哪」无关）；
* 删掉校验 ⇒ **读取成功**（`m02` 的原始输出：「帧尾位腐被静默接受了」，`reader_test.go:367`）——静默接受位腐产物，正是 R10 第 2 项要消灭的形态。

用例内自带前提自检（`reader_test.go:352-355`：帧区 < 2 MiB 就 `t.Fatalf` 自证失败），避免将来块变小后这条用例悄悄退化成前一条。

### 3.3 R10 第 3 项：标记行 ⇒ `ErrBodyMissing`（两条路径都识别）

| 路径 | 位置 | 用例 |
|---|---|---|
| 产物 | `internal/tracearchive/reader.go:211-213` | `reader_test.go:141`（隔离探针）、`reader_test.go:98`（环用例，含反向断言） |
| spool | `internal/tracearchive/reader.go:297-299` | `reader_test.go:166`（隔离探针）、`reader_test.go:98` |
| 共用判据 | `internal/tracearchive/reader.go:318-320`（`isFailedRowMarker` = `bytes.Equal(trimNewline(line), failedRowMarker)`，与 Task 5 的 `failedRowMarker`（`writer.go:280`）同包共用一份定义） | `reader_test.go:194`（判据下界） |

**删除任一处识别 ⇒ 都转红（且能定位到是哪一条路径）**：

```
m07_删除产物路径标记行识别:
    reader_test.go:153: 产物里的标记行必须报 ErrBodyMissing，却把 "null" 当正文返回了
    reader_test.go:117: part=0 的标记行必须返回 ErrBodyMissing，实际 <nil>
--- FAIL: TestReaderReportsBodyMissingForMarkerLine (0.09s)

m08_删除spool路径标记行识别:
    reader_test.go:178: spool 里的标记行必须报 ErrBodyMissing，却把 "null" 当正文返回了
    reader_test.go:117: part=1 的标记行必须返回 ErrBodyMissing，实际 <nil>
--- FAIL: TestReaderReportsBodyMissingForMarkerLine (0.18s)
```

原始输出：`r10-two-sided.txt:278`（m07）、`:334`（m08）、`raw-m07.txt`、`raw-m08.txt`。

**D15（遮蔽效应）处理**：简报登记的 `TestReaderReportsBodyMissingForMarkerLine` 是**环用例**（`for _, part := range []int{0,1}`，`reader_test.go:98`），环在第一个失败处停止 ⇒ 它只能证明「环里有东西失败了」。因此我另加两条**只含该条目的隔离探针**（产物只有一行且那行就是标记行 / spool 只有一行且那行就是标记行）：

* `reader_test.go:141`：断言 `errors.Is(err, ErrBodyMissing)` **且** `!errors.Is(err, ErrNotArchived)`（404 与 500 身份不得混淆）；
* `reader_test.go:166`：同上，spool 路径。

**判据的下界**（简报未要求，我加的）：`reader_test.go:194`（`TestReaderDoesNotTreatNullSubstringAsMarker`）要求正文 `{"request_id":"n1","body":"values are null"}` 在两条路径上都**原样返回**；`m09_标记行判据放宽为Contains`（把 `bytes.Equal(trimNewline(line), failedRowMarker)` 改成 `bytes.Contains(line, failedRowMarker)`）只杀它一条（`raw-m09.txt`）。理由：放宽判据会把真实正文误报成 `body_missing`（404），那是与 R10 第 3 项方向相反的一类静默错误。

---

## 4. 变异自检

* 脚本：`.superpowers/sdd/2026-09-24-tracearchive-storage-primitives/task-7-mutate.py`（未入库：`.superpowers/sdd/.gitignore:1` 忽略整个目录，与 Task 6 的 `task-6-mutate.py` 同例）
* 变异对象：`/tmp/readbase/internal/tracearchive/reader.go`（提交 `f7033774` 的**完整副本**，`rsync -a` 排除 `.git`；原始侧 sha256 = `6bb58fdd33c1407c…`，每轮结束都做「还原校验: 一致」）
* 命令：`python3 task-7-mutate.py`（表）、`--check`、`--selftest`、`--side m01 m02 …`
* 每条运行（原始侧与变异侧）都先过**内嵌清单 + 四断言**；`--selftest` 用四个合成反例证明检测本身有效：A 少跑一条 ⇒ 判无效；B `no tests to run` ⇒ 判无效；C 全 SKIP ⇒ 判无效（绝不算绿）；D 缩正则/多塞一条 ⇒ 启动即失败。

```
原始侧自证: OK names=22 ran=22 diff=0 PASS=22 SKIP=0 FAIL=0
```

**内嵌名单清单与来源**（`task-7-mutate.py` 的 `EMBEDDED_NAMES`，22 条，与 `-run` 正则逐条 `diff` 为空；与最终测试文件的重推导结果也逐条 `diff` 为空）：

```
TestReaderConcurrencyIsBounded
TestReaderDistinguishesMissingFromNotArchived
TestReaderDoesNotLeaveTempFiles
TestReaderDoesNotTreatNullSubstringAsMarker
TestReaderFallsBackToSpoolForUnsealedRows
TestReaderHonorsContextCancelWhileWaitingForSlot
TestReaderMarkerOnlyArtifactIsBodyMissing
TestReaderMarkerOnlySpoolIsBodyMissing
TestReaderRejectsCorruptFooter
TestReaderRejectsEmptyRoot
TestReaderRejectsNegativeRow
TestReaderRejectsOversizedTrailerLen
TestReaderRejectsTrailerNotAdjacentToFooter
TestReaderRejectsTrailerOffsetNotAtLineStart
TestReaderRejectsTrailerRowCountMismatch
TestReaderRejectsTruncatedArtifact
TestReaderRejectsUnterminatedSpoolTail
TestReaderReleasesConcurrencySlots
TestReaderReportsBodyMissingForMarkerLine
TestReaderReturnsExactLine
TestReaderVerifiesFrameSHA
TestReaderVerifiesFrameSHABeyondDecodedRegion
```

**来源（可被第三方重新推导）**：`tests/tracearchive/reader_test.go` 与 `internal/tracearchive/reader_internal_test.go`（本任务全部顶层用例），命令内嵌在脚本 docstring 与 `three-assertions.sh` 里：

```
grep -h '^func Test' tests/tracearchive/reader_test.go \
    internal/tracearchive/reader_internal_test.go | sed 's/^func \([A-Za-z0-9_]*\).*/\1/' | sort
```

### 4.1 变异表（归档：`task-7-evidence/mutation-table.txt`）

判据：**红 = 该轮的某条用例失败/崩溃/超时；「绿（零覆盖！）」= 该守卫无覆盖，必须给保留理由；「证据无效」= 该轮根本没按清单跑（整轮失败）**。

| 变异 | 判定 | 自证（名字/执行/diff/PASS/SKIP/FAIL） | 杀死的用例 |
|---|---|---|---|
| m01_删除trailer_len上界 | 红 | names=22 ran=22 diff=0 PASS=21 SKIP=0 FAIL=1 | TestReaderRejectsOversizedTrailerLen |
| m02_删除frame_sha比对 | 红 | names=22 ran=22 diff=0 PASS=20 SKIP=0 FAIL=2 | TestReaderVerifiesFrameSHA, TestReaderVerifiesFrameSHABeyondDecodedRegion |
| m03_摘要范围忘掉限界 | 红 | names=22 ran=22 diff=0 PASS=12 SKIP=0 FAIL=10 | TestReaderConcurrencyIsBounded, TestReaderDoesNotLeaveTempFiles, TestReaderDoesNotTreatNullSubstringAsMarker, TestReaderHonorsContextCancelWhileWaitingForSlot, TestReaderMarkerOnlyArtifactIsBodyMissing, TestReaderRejectsTrailerOffsetNotAtLineStart, TestReaderRejectsTrailerRowCountMismatch, TestReaderReleasesConcurrencySlots, TestReaderReportsBodyMissingForMarkerLine, TestReaderReturnsExactLine |
| m04_删除spool残行守卫 | 红 | names=22 ran=22 diff=0 PASS=21 SKIP=0 FAIL=1 | TestReaderRejectsUnterminatedSpoolTail |
| m05_删除spool越界分支 | 红 | names=22 ran=22 diff=0 PASS=21 SKIP=0 FAIL=1 | TestReaderDistinguishesMissingFromNotArchived |
| m32_残行按正文返回 | 红 | names=22 ran=22 diff=0 PASS=21 SKIP=0 FAIL=1 | TestReaderRejectsUnterminatedSpoolTail |
| m06_spool行号错位取下一行 | 红 | names=22 ran=22 diff=0 PASS=17 SKIP=0 FAIL=5 | TestReaderDoesNotTreatNullSubstringAsMarker, TestReaderFallsBackToSpoolForUnsealedRows, TestReaderMarkerOnlySpoolIsBodyMissing, TestReaderRejectsUnterminatedSpoolTail, TestReaderReportsBodyMissingForMarkerLine |
| m07_删除产物路径标记行识别 | 红 | names=22 ran=22 diff=0 PASS=20 SKIP=0 FAIL=2 | TestReaderMarkerOnlyArtifactIsBodyMissing, TestReaderReportsBodyMissingForMarkerLine |
| m08_删除spool路径标记行识别 | 红 | names=22 ran=22 diff=0 PASS=20 SKIP=0 FAIL=2 | TestReaderMarkerOnlySpoolIsBodyMissing, TestReaderReportsBodyMissingForMarkerLine |
| m09_标记行判据放宽为Contains | 红 | names=22 ran=22 diff=0 PASS=21 SKIP=0 FAIL=1 | TestReaderDoesNotTreatNullSubstringAsMarker |
| m10_删除产物行号越界检查 | 红(崩溃/中止) | 集合不完整：未执行 9 条（TestReaderConcurrencyIsBounded, TestReaderDoesNotLeaveTempFiles, TestReaderRejectsCorruptFooter …） | TestReaderDistinguishesMissingFromNotArchived（`index out of range [5] with length 1` → panic 中止同包） |
| m11_只删越界下界 | 红(崩溃/中止) | 集合不完整：未执行 8 条（TestReaderConcurrencyIsBounded, TestReaderDoesNotLeaveTempFiles, TestReaderRejectsCorruptFooter …） | TestReaderRejectsNegativeRow（`index out of range [-1]` → panic 中止同包） |
| m12_越界检查差一 | 红 | names=22 ran=22 diff=0 PASS=12 SKIP=0 FAIL=10 | TestReaderConcurrencyIsBounded, TestReaderDoesNotLeaveTempFiles, TestReaderDoesNotTreatNullSubstringAsMarker, TestReaderHonorsContextCancelWhileWaitingForSlot, TestReaderMarkerOnlyArtifactIsBodyMissing, TestReaderRejectsOversizedTrailerLen, TestReaderRejectsTrailerRowCountMismatch, TestReaderReleasesConcurrencySlots, TestReaderReportsBodyMissingForMarkerLine, TestReaderReturnsExactLine |
| m13_产物行映射错位取下一行 | 红 | names=22 ran=22 diff=0 PASS=20 SKIP=0 FAIL=2 | TestReaderReportsBodyMissingForMarkerLine, TestReaderReturnsExactLine |
| m14_循环条件差一 | 红 | names=22 ran=22 diff=0 PASS=14 SKIP=0 FAIL=8 | TestReaderConcurrencyIsBounded, TestReaderDoesNotLeaveTempFiles, TestReaderDoesNotTreatNullSubstringAsMarker, TestReaderHonorsContextCancelWhileWaitingForSlot, TestReaderMarkerOnlyArtifactIsBodyMissing, TestReaderReleasesConcurrencySlots, TestReaderReportsBodyMissingForMarkerLine, TestReaderReturnsExactLine |
| m15_未命中行首兜底返回空 | 红 | names=22 ran=22 diff=0 PASS=21 SKIP=0 FAIL=1 | TestReaderRejectsTrailerOffsetNotAtLineStart |
| m16_删除trailer邻接校验 | 红 | names=22 ran=22 diff=0 PASS=21 SKIP=0 FAIL=1 | TestReaderRejectsTrailerNotAdjacentToFooter |
| m17_忽略footer的row_count（宽松解码） | 红 | names=22 ran=22 diff=0 PASS=21 SKIP=0 FAIL=1 | TestReaderRejectsTrailerRowCountMismatch |
| m18_删除footer长度守卫 | 红 | names=22 ran=22 diff=0 PASS=21 SKIP=0 FAIL=1 | TestReaderRejectsTruncatedArtifact |
| m19_ErrNotArchived改v产物越界 | 红 | names=22 ran=22 diff=0 PASS=20 SKIP=0 FAIL=2 | TestReaderDistinguishesMissingFromNotArchived, TestReaderRejectsNegativeRow |
| m20_ErrNotArchived改v spool越界 | 红 | names=22 ran=22 diff=0 PASS=21 SKIP=0 FAIL=1 | TestReaderDistinguishesMissingFromNotArchived |
| m21_ErrNotArchived改v 两者都不存在 | 红 | names=22 ran=22 diff=0 PASS=21 SKIP=0 FAIL=1 | TestReaderDistinguishesMissingFromNotArchived |
| m22_ErrBodyMissing改v产物路径 | 红 | names=22 ran=22 diff=0 PASS=20 SKIP=0 FAIL=2 | TestReaderMarkerOnlyArtifactIsBodyMissing, TestReaderReportsBodyMissingForMarkerLine |
| m23_ErrBodyMissing改v spool路径 | 红 | names=22 ran=22 diff=0 PASS=20 SKIP=0 FAIL=2 | TestReaderMarkerOnlySpoolIsBodyMissing, TestReaderReportsBodyMissingForMarkerLine |
| m24_ErrFooterCorrupt改v邻接 | 红 | names=22 ran=22 diff=0 PASS=21 SKIP=0 FAIL=1 | TestReaderRejectsTrailerNotAdjacentToFooter |
| m25_ErrFooterCorrupt改v摘要 | 红 | names=22 ran=22 diff=0 PASS=20 SKIP=0 FAIL=2 | TestReaderVerifiesFrameSHA, TestReaderVerifiesFrameSHABeyondDecodedRegion |
| m26_ErrFooterCorrupt改v上界 | 红 | names=22 ran=22 diff=0 PASS=21 SKIP=0 FAIL=1 | TestReaderRejectsOversizedTrailerLen |
| m27_删除并发槽位释放 | 红(超时/挂死) | —（挂死，三断言不适用） | TestReaderReleasesConcurrencySlots（`reader_internal_test.go:118: 第 0 次读取后并发槽位未释放，残留 1`，**先于挂死**干净失败）；随后槽位永不归还，B 包点读在 `reader.go:109` 的槽位 `select` 上永久阻塞 ⇒ `panic: test timed out` + goroutine dump（`raw-m27.txt`） |
| m28_删除MaxConcurrent默认值 | 红(超时/挂死) | —（挂死，三断言不适用） | 挂死：A 包 `TestReaderRejectsOversizedTrailerLen`、B 包 `TestReaderReturnsExactLine`（`sem` 无缓冲 ⇒ 首次 `GetBody` 永久阻塞；`panic: test timed out`） |
| m29_删除ZstdWindowBytes默认值 | 红 | names=22 ran=22 diff=0 PASS=13 SKIP=0 FAIL=9 | TestReaderConcurrencyIsBounded, …, TestReaderReturnsExactLine（`WithDecoderMaxWindow(0)` 直接报错） |
| m30_删除Root为空守卫 | 红 | names=22 ran=22 diff=0 PASS=21 SKIP=0 FAIL=1 | TestReaderRejectsEmptyRoot |
| m31_删除ctx取消分支 | 红 | names=22 ran=22 diff=0 PASS=21 SKIP=0 FAIL=1 | TestReaderHonorsContextCancelWhileWaitingForSlot（`reader_internal_test.go:175: 等待槽位期间的取消没有返回：ctx.Done() 分支缺失`） |

（表中省略号处与 `mutation-table.txt` 同源，完整列举见该文件；本表只是把「自证串」原样搬过来。）

汇总（**分层口径**，与 Task 6 修复轮 3 对齐）：**变异 32 条 = 28 条断言支撑 + 4 条崩溃/超时 + 0 零覆盖（绿）+ 0 证据无效**。

* **28 条断言支撑**：执行集合 == 内嵌清单（22 条全部 `=== RUN`）且全部有终判，失败由用例断言报出（自证串形如 `names=22 ran=22 diff=0 …`）。
* **4 条崩溃/超时**（`m10`/`m11`/`m27`/`m28`）：这 4 条**缺「执行 + 终判」证据**，不是干净的断言失败——`m10`/`m11` 因 panic 中止同包，清单 22 条分别只跑了 13/14 条（缺 9/8 条 `=== RUN`）；`m27` 在 A 包一条干净 FAIL 后超时；`m28` 纯挂死（两包各仅 1 条 `=== RUN`，合计 2 条、0 条终判）。它们仍然是红（绝不可能被算绿），但**不能与上面 28 条同列**，故单列。

（判据与杀死的用例逐条来自 `task-7-evidence/mutation-table.txt` 的原始输出；清单由 21 条变 22 条后**整表重跑**，22 条之前的旧表已作废，见 §10。）

> 过程记录（诚实留痕）：第一轮跑出 `m04` 报「绿（零覆盖）」——查下来是**脚本 bug**：我把它写成 `("cut", A, '')`，而 `apply` 的 cut 分支用 `text.index('', i)`，替换长度为 0 ⇒ 变异是空操作。改成三元的 `("sub", A, '')` 后 m04 正常转红，并在脚本里留了注释（`task-7-mutate.py` 的 m04 定义处）。这条也说明「绿色」必须先怀疑证据本身。

### 4.2 零覆盖条目与保留理由

清单变为 22 条后，**零覆盖条目为三条纯 IO 错误包装分支**（复审实测：把这三处的 `%w` 都改成 `%v` 后 22 条全绿 ⇒ 没有任何用例约束它们的包装语义）：

| 零覆盖 | 保留理由 |
|---|---|
| `GetBody` 对**产物路径**的 `os.Stat` 非 `IsNotExist` 分支（`reader.go:89-91`，包装行 `:90`） | 需要权限/IO 注入才能到达，B 层无法表达；其身份是「包装原始错误上抛」，不含静默通过路径。 |
| `GetBody` 对 **spool 路径**的 `os.Stat` 非 `IsNotExist` 分支（`reader.go:95-97`，包装行 `:97`） | 同上。**这一条此前被漏报**：它与 `:89-91` 不是同一处，而是 `GetBody` 同一个函数里两条并列的 `os.Stat … else if !os.IsNotExist(err)` 分支（先产物、后 spool），各自独立可达。 |
| `readFromSpool` 的 `os.Open` 失败分支（`reader.go:275-277`，包装行 `:277`） | 同上；需要打开 spool 文件失败（权限/IO 注入）才能到达。 |

（原报告把 `:89-91` 记作 `readFromPart` 的分支；它实为 `GetBody` 的分支。三处零覆盖是复审把 `%w` 逐个改成 `%v` 实测出来的，而 `:89-91` 与 `:95-97` 是同一个函数里的两处不同包装，故必须分列。）

**此前的第三条零覆盖（`reader.go:112-113` 的 `ctx.Done()` 取消分支）已由 `b691ab11` 覆盖**，不再是零覆盖：

* 用例 `internal/tracearchive/reader_internal_test.go:138`（`TestReaderHonorsContextCancelWhileWaitingForSlot`），A 层（第 3 条，超出简报的「两条」限定，披露见 §6.4）；
* 构造：`MaxConcurrent: 1` → 直接向未导出的 `r.sem` 投一个令牌占满槽位 → 用**已取消**的 ctx 调 `GetBody`。占满后 `select` 里只有 `<-ctx.Done()` 就绪（向已满缓冲 channel 发送不可就绪），因此断言确定、不 sleep、无竞态（`-race` 通过）；
* 三条伴随断言：取消路径**不得**释放别人占着的槽位（`r.inFlight()==1`）；归还槽位后同一 Reader 必须照常读；读完槽位完全归还；
* 用「带 10 s 上界的 `select`」而非直接调用：该分支被删时得到**定位到本条用例的干净失败**（`reader_internal_test.go:175: 等待槽位期间的取消没有返回：ctx.Done() 分支缺失`），而不是把整个测试包拖成挂死超时（后者会让同包其他用例的判决一起丢失）。
* 变异证据：`m31_删除ctx取消分支` ⇒ 红，只杀这一条（`names=22 ran=22 diff=0 PASS=21 SKIP=0 FAIL=1`，原始输出 `task-7-evidence/raw-m31.txt`）。

### 4.3 「物理行序 == row_seq」的读取侧对照实验（我额外做的）

简要 4 只要求三个 R10 守卫各有区分用例；变异表里的下列三条是**直接针对承重不变量的静默错正文形态**，我认为值得单列：

* `m13_产物行映射错位取下一行`（`targetOff := offsets[ref.Row]` → 取 `ref.Row+1`）：杀 `TestReaderReturnsExactLine`（逐字节比对 100 KB 大行）与环用例 ⇒ **一行错位立刻被抓住**；
* `m14_循环条件差一`（`for read <= targetOff` → `read < targetOff`）：杀 7 条；
* `m06_spool行号错位取下一行`：杀 5 条。

原始输出：`r10-two-sided.txt:509`（m13）、`raw-m13.txt`、`raw-m14.txt`、`raw-m06.txt`。

---

## 5. 工具链

```
gofmt -l internal/tracearchive/ tests/tracearchive/     # 空 ⇒ 通过
go vet ./internal/tracearchive/ ./tests/tracearchive/   # 通过
go build ./internal/... ./cmd/...                       # 通过
```

（未跑 `go build ./...`：`test/bedrock_model_id_check.go` 有既存编译错误，简报要求规避。）

---

## 6. 与简报的差异 / 冲突（逐条）

1. **`sealLines` 的版本透传（简报代码块有误）**：`task-7-brief.md:37-39` 写 `bin := requireZstd(t)` 且不传 `ExpectedZstdVersion`。但本仓库 `requireZstd` 返回 `(bin, 本机版本)`（`tests/tracearchive/helpers_test.go:40`），而 `NewSealer` 在空值时取默认 `defaultZstdVersion = "1.4.8"`（`internal/tracearchive/sealer.go:33,117-118`；版本守卫在 `sealer.go:333-335`）；本机 zstd 是 v1.5.7 ⇒ 照抄会让**每一次封块都被版本守卫拒绝**，产物路径用例全红。落地为 `bin, ver := requireZstd(t)` + `ExpectedZstdVersion: ver`（`tests/tracearchive/helpers_test.go:133-137`、`internal/tracearchive/testhelpers_test.go:120-124`），与 Task 6 既有用例一致。**以简报为准会得到全红，故按实现修正，理由留在两处 `sealLines` 的注释里。**
2. **A 层 `TestReaderRejectsOversizedTrailerLen` 的构造**（`reader_internal_test.go:48-96`）：简报的 `trailer_offset=0` + `FrameSHA` 零值版**杀不死「删除上界」这条变异**——R10 第 2 项加了摘要校验后，零值与「空帧区摘要」不相等，删掉上界会有摘要校验抢先报同一个 `ErrFooterCorrupt`，用例照样绿。落地版把帧区做成 16 字节真数据、并把 `FrameSHA` 设成它的真实摘要，使「邻接 + 摘要」都自洽，唯一不成立的只有 `trailer_len > maxTrailerBytes`；删掉上界后错误变成 `ErrTrailerCorrupt`（trailer 区是稀疏零字节）⇒ 用例转红（`m01`，`raw-m01.txt`）。A 层仍按要求引用未导出 `maxTrailerBytes`（`reader_internal_test.go:60`）、用未导出 `footer`/`encodeFooter` 构造（`:74-82`）。
3. **R10 第 1 项的拒绝范围**：见 §3.1.1（简报/规格只写「拒绝读取」，落地为「拒绝未终止的那一行」并给出理由；如要求整份拒绝，一行可改）。
4. **新增了简报未登记的用例**（B 层 11 条，均只用公开 API，理由逐条写在用例注释里）：两条标记行隔离探针（D15）、标记行判据下界、R10 第 1 项的隔离探针、R10 第 2 项的两条、负行号下界、产物截断、trailer 非邻接、trailer `row_count` 不一致、空 Root。**简报的 9 条用例原样保留**（其中 `TestReaderReportsBodyMissingForMarkerLine` 我加了反向断言，见 §3.3）。
   另有 **A 层第 3 条用例**（`reader_internal_test.go:138`，提交 `b691ab11`）——**这一条超出了简报对 A 层「两条」的限定**，如实披露：协调者在裁决里邀请补它，覆盖此前唯一的零覆盖分支 `reader.go:112-113`；它只触达未导出的 `r.sem`/`r.inFlight()`，不重构任何并发结构。
5. **`readFromPart` 的两处顺序选择**（简报代码块未规定）：`trailer_len` 上界校验放在摘要校验**之前**（`reader.go:148-155` → `:171`），目的是让「上界」这条闸的身份单独可见（否则越界产物会被摘要校验抢先拦下，`m01` 就杀不死）。这条顺序在 `reader.go:164-167` 有注释。
6. **`decodeTrailer` 的错误原样上抛**（`reader.go:181-185`）：简报如此，我没有把它归一化成 `ErrFooterCorrupt`。因此「footer 的 `row_count` 与 trailer 条目数不符」返回的是 `ErrTrailerCorrupt`（同样是 500 类哨兵）。若服务层只想处理一个「产物损坏」身份，需要在上层用 `errors.Is(err, ErrTrailerCorrupt) || errors.Is(err, ErrFooterCorrupt)`——**这条请计划 3 的服务层注意**。

---

## 7. 顾虑（如实说，不粉饰）

1. **trailer 与 footer 区域没有校验和（格式级残余风险，本任务无法在读取侧消除）**。`frame_sha256` 只覆盖 `[0, trailer_offset)` 的帧字节（`reader.go:229-251`）。因此**把 trailer 改成「指向另一个行首」的偏移时，读取端无法察觉，会静默返回错误的行**。我加了 `TestReaderRejectsTrailerOffsetNotAtLineStart`（`reader_test.go:506`）挡住「指向行中间」这一半（`m15` 杀得死），并在该用例注释里**明确登记了它覆盖不到的那一半**——避免用例名称造成「trailer 损坏都被处理了」的假象。要真正消除，需要给 trailer 加校验和（Task 3 的 `footer` 里 `[30:32)` 是 2 字节 reserved，容量不够 8 字节摘要；可行的是在 `formatVersion` 升级时新增字段，或把 trailer 纳入 `frame_sha` 的范围——两种都会改产物格式，超出本任务）。
2. **`frame_sha256` 校验的 I/O 代价**：每次点读会先把 `[0, trailer_offset)` 顺序读一遍做哈希（`reader.go:234-243`）。读一个后段行时，文件被读了两遍（哈希一遍、解压一遍）。这是 R10 硬要求带来的代价，我按 §8.3 只保证了内存/临时文件约束（C1/C2/C3），**没有做「摘要校验只在必要时才做」之类的优化**（那会引入跳过校验的路径）。若规格 §8.3 还想约束 I/O 放大，需要另行裁决。
3. **`TestReaderConcurrencyIsBounded`（B 层）只断言「8 个并发读全部成功」**，它**不能干净地**发现槽位不释放——**正确表述是「表现为挂死，而不是干净的断言失败」**：`m27`（删除并发槽位释放）下，A 包 `TestReaderReleasesConcurrencySlots` 先在 `reader_internal_test.go:118` 干净 FAIL（第 0 次读取后并发槽位未释放，残留 1），随后槽位永不归还，B 包的**点读在 `reader.go:109` 的槽位 `select` 上永久阻塞**，`panic: test timed out` + goroutine dump（`task-7-evidence/raw-m27.txt`）。也就是说，只跑 B 层的并发用例时这个缺失**会挂死**（超时 + goroutine dump），既不是「全绿」，也不是一条能指明原因的断言失败——这正是「B 层无法发现槽位不释放」的正确表述。我按要求把「槽位完全释放」的断言留在 A 层（`reader_internal_test.go:117-118`），并在 B 层用例注释里写明这个分工。这条依赖仍然脆弱：删掉 A 层那条断言，B 层只会以挂死（超时）暴露它。
4. **`m27`/`m28` 的判定是「超时/挂死」而非干净的 FAIL**（`m27` 在 A 包先有一条干净 FAIL，`m28` 纯挂死）。它们仍然是红（绝不可能被算绿），但「杀死了哪些用例」只能从 A 包的 FAIL 行/panic 栈里读，不比行为型变异干净。
5. **`m10`/`m11` 的杀掉方式是 panic**（越界检查缺失 ⇒ `offsets[-1]`/`offsets[5]` 越界 panic，测试进程中止，`=== RUN` 集合不完整）。这是**响亮**的失败、且正是「不能把损坏行号喂给数组索引」的证明，但它会中断同包后续用例的执行（我用 `classify()` 把这种形态标成「红(崩溃/中止)」并列出未执行的用例，避免把它误判成「证据无效」或「绿」）。
6. **Task 5 的进程重启语义**：写失败会毒化并换 part、重启后 `nextSeqLocked` 也一定开新 part（所以残行恒是文件末尾），这两条是我在 §3.1.1 里论证「只拒绝残行本身」的依据。我**只做了代码阅读、没有写端到端实验**去验证「重启后不会在同 part 续写」（那属于 Task 5 的用例范围，不在本任务文件里）。如果那个前提不成立，「残行之后又被追加」的形态会绕过我的守卫（残行与后续字节粘成一条**已终止**的行，读取端无法识别）——**建议在 Task 8 的端到端用例里覆盖一次**。
7. **`GetBody` 对 `Inst`/`dt` 不做白名单校验**（只按 `Ref` 拼路径）。恶意 `dt`（如 `../..`）能否穿越取决于上游服务层；`Writer` 有 `validateDT` 白名单（`writer.go:55`）而读取端没有。本任务简报没要求，我按「不做简报没要求的事」没有加——但这是**服务层调用前必须校验**的一点，登记在此。

---

## 8. 未做的事 / 超出简报的改动

**未做**：
* 没有改 Task 1~6 的任何生产文件与测试文件（只追加了两处 `sealLines` 与一次 import 追加：`tests/tracearchive/helpers_test.go`、`internal/tracearchive/testhelpers_test.go`）；
* 没有动计划/规格文档（`docs/superpowers/**` 未改）；
* 没有跑 `go build ./...`（规避既存编译错误）；没有改任何 HTTP 层/服务层；
* 没有加 trailer 校验和（属格式变更，见顾虑 1）；
* （「ctx 取消分支」A 层用例原列为「未做」，后经裁决邀请已补，见 §4.2 与 §6.4。）
* 没有为「同 part 续写」写端到端实验（见顾虑 6）。

**超出简报的改动**（逐条理由已在上文对应小节）：
1. 新增 B 层 11 条 + A 层 1 条用例（§6.4；D15 隔离探针 ×4、判据下界 ×1、R10 判别性用例 ×1、其余为护栏的边界用例 ×5，A 层 1 条为零覆盖分支）；
2. `TestReaderReportsBodyMissingForMarkerLine` 内加了两条反向断言（标记行前后的真实正文必须照常读到），防止「一律报 missing」也能全绿；
3. `sealLines` 透传本机 zstd 版本（§6.1，简报代码块有误）；
4. A 层越界用例的产物构造改为「帧摘要自洽」（§6.2）；
5. `readFromPart` 的校验顺序（§6.5）；
6. 变异脚本 `task-7-mutate.py` 与 32 条变异（简报步骤 4 要求做变异自检，脚本属证据工具，按 Task 6 惯例放在被忽略的 `.superpowers/` 下）。

---

## 9. 复现命令

```bash
cd <worktree>
# 绿：-run 证据（内嵌清单的来源）
grep -h '^func Test' tests/tracearchive/reader_test.go \
    internal/tracearchive/reader_internal_test.go | sed 's/^func \([A-Za-z0-9_]*\).*/\1/' | sort
python3 -c "names=[l.strip() for l in open('/tmp/task7-names.txt') if l.strip()]; print('|'.join(sorted(names)))" > /tmp/task7-run.txt
bash .superpowers/sdd/2026-09-24-tracearchive-storage-primitives/task-7-evidence/three-assertions.sh

# 全量两包
go test ./internal/tracearchive/ ./tests/tracearchive/ -count=1 -v -timeout 300s

# 变异（先建隔离副本，再逐条删守卫）
rsync -a --exclude .git --exclude web/node_modules ./ /tmp/readbase/
cp /tmp/readbase/internal/tracearchive/reader.go /tmp/reader.pristine.go
cd .superpowers/sdd/2026-09-24-tracearchive-storage-primitives
python3 task-7-mutate.py --selftest     # 证明「无效证据」检测本身有效
python3 task-7-mutate.py                # 32 条变异表（含每条的自证串）
python3 task-7-mutate.py --side m01 m02 m04 m32 m07 m08   # R10 双侧原始输出
```

---

## 10. 本轮出现过两次状态：作废归档清单

本任务在交付前经历了**两次状态**，归档里因此同时存在两轮产物。**权威状态是清单 22 条的 `b691ab11`**；所有与它不一致的归档一律以 `VOID-` 前缀标注作废，并在 `task-7-evidence/PROVENANCE.txt` 里登记。

| 状态 | 清单 | 提交 | 权威归档 |
|---|---|---|---|
| ① 第一轮（21 条顶层用例；A 层 2 条） | 21 | `f7033774` | **已作废** |
| ② 最终（22 条顶层用例；A 层 3 条，+`TestReaderHonorsContextCancelWhileWaitingForSlot`） | 22 | `b691ab11` | `green-run-22-verbose.txt`、`three-assertions-green.txt`、`fullsuite-104-top-level-verbose.txt`、`mutation-table.txt`、`raw-pristine-green-22.txt`、`raw-m01..m32.txt`、`r10-two-sided.txt` |

**作废的归档（保留但改名，供追溯）**：

* `VOID-green-run-21-verbose.txt`（sha256 `1dddafb5189b8e2b`）——第一轮的 `-run` 绿灯输出，只有 21 条 `--- PASS`，**不含** `TestReaderHonorsContextCancelWhileWaitingForSlot`；与最终提交不一致，故作废。
* `VOID-fullsuite-103-top-level-verbose.txt`（sha256 `f1df8f7e8000aea6`）——第一轮的两包全量输出，顶层 103；最终为 104。
* 第一轮的**变异表整体作废**（同路径 `mutation-table.txt` 已被 22 条清单的重跑结果覆盖，无法同时保留两轮）：第一轮 32 条里 `m31` 报「绿（零覆盖）」；22 条清单下 `m31` 转红。**旧表的内容仅在 §4 的过程记录里被引用，不再作为判据。**
* **`r10-two-sided.txt` 同样被 22 条清单的重跑结果覆盖**（原始侧自证 `names=22`），但本报告正文原先引用的行号来自 21 条那一轮 ⇒ 那些行号已失效。**本轮已按现文件逐条更正**（m04→`:168`、m32→`:223`、m02→`:112`、m07→`:278`、m08→`:334`、m13→`:509`），并同时更正表头里 `mutation-table.txt` / `r10-two-sided.txt` 的 sha256 前缀（旧前缀是 21 条那一轮的残值）。
* `/tmp/task7-green-run.txt`、`/tmp/task7-run.txt`、`/tmp/readbase/` 是第一轮的临时文件（`/tmp` 不进归档）；`/tmp/readbase` 已用最终状态重建（`diff -r` 与提交后的工作树一致）。

**这次复发的问题形态与教训（我自己犯的）**：我在收到「保留该用例」的裁决前就先写了用例、改了脚本清单并跑了变异，随后**只 amend 了提交**——于是短暂出现了「脚本清单 22 条 / 一份归档 21 条 / 报告还写着『未加该用例』」的三方漂移。**消解方式**：先定状态（保留），再让文件、清单、变异表、归档、报告五者对齐；提交改为**新增提交而非 amend**（`b691ab11`，父 `f7033774`），并补 `PROVENANCE.txt` 把证据绑到 `HEAD` 与被测文件的 blob 哈希上。教训与本项目既有结论一致：**证据工具测的树必须与提交一致，且这份一致性要被机器记录，而不是靠人记得。**

---

## 11. 证据工具的已知局限与将被取代的方式

本任务的证据工具是 `.superpowers/sdd/.../task-7-mutate.py`（与 Task 6 的 `task-6-mutate.py` 同源）。它在 gitignore 之外，存在四项已知局限，另有一项必须明说的边界（第 5 条）——**这些是各任务证据工具共有的问题，不是本任务独有**；修复轮 3 已产出共享工具（`tools/trace-mutate/`，本分支已落地）取代它。本任务**没有**自行实现这些修复（按协调者指令），只在这里登记：

1. **没有版本谱系**：证据工具与被 `.gitignore` 忽略的归档都在 `.superpowers/` 下（`git ls-files .superpowers` 在本轮更正前输出为空；本轮为把这份报告落进版本控制而 `git add -f` 了它本身，故该命令现在只输出这一个报告文件，`task-7-mutate.py` 与全部 `raw-*` 归档仍不可见）⇒ 谁把内嵌清单改小、或把归档换成一次「更好看」的运行，`git diff` 看不见。
   *本任务的部分缓解*：内嵌清单可从受版本控制的测试文件逐条重推导（推导命令写在 §4 与脚本 docstring 里，并同时断言「正则集合 == 内嵌清单」与「执行集合 == 内嵌清单」）；`PROVENANCE.txt` 记下被测文件的 blob 哈希与各归档 sha256。
2. **证据不绑定提交**：脚本本身不做 `git rev-parse` / 树状态校验。
   *本任务的部分缓解*：`task-7-evidence/PROVENANCE.txt` 记录 `HEAD`、父提交、`git status --porcelain` 行数、被测文件 blob 哈希、全部归档 sha256。但这仍**不是强制**的——它是人写的一步，不是工具的一步，而且这份文件本身也是人手写的**声明**（见第 5 条）。
3. **子进程继承环境**：`go test` 子进程继承 `GOFLAGS` 等变量，`GOFLAGS=-exec=<wrapper>` 可整体替换测试输出，而脚本察觉不到。
   *本任务未做任何缓解*。
4. **断言 3 只数顶层 SKIP**：`^--- SKIP` 不含缩进的子测试 SKIP ⇒ 若本任务将来引入 `t.Run` 子用例并让它们 skip，`SKIP==0` 会失效。
   *本任务的部分缓解*：本任务两包**没有任何子用例**（本报告独立给出「子用例 0 条」的计数），因此当前不存在子用例 SKIP。**注意（复审更正）**：原报告称「脚本另有『子用例终判行数』一栏」，该栏**只存在于 `three-assertions.sh:48`**，**变异工具 `task-7-mutate.py` 里没有这一栏**（它的三/四断言只匹配顶层 `^--- PASS/FAIL/SKIP: Test…`，见该脚本 `verify()`）⇒ 该缓解**只对 `three-assertions.sh` 成立，对变异工具不成立**：一旦出现子用例，变异工具既不会显式打印子用例终判，也不会因「子用例被 skip 而顶层仍绿」而判无效。
5. **`PROVENANCE.txt` 是人手写的，不是工具产出**（本任务缓解的边界，必须明说）：复审核实其内容**全部正确**（5 个被测文件 blob 哈希、43 条归档 sha256 全部匹配、`git diff --stat b691ab11..HEAD` 为 0 行、工作树干净），但它是**手工声明**——**没有生成脚本**，且**不记工具自身哈希、不记变异输入（内嵌清单/变异定义）、也没有任何机制把 `raw-m*.txt` 绑到被测 blob**。因此它给出的是**声明，不是事实**：它能证明「有人核对过」，**不能**证明「这些归档确实由那份脚本、在那棵树、按那份输入产出」。这正是共享工具 `tools/trace-mutate/`（Task 6 修复轮 3 已落地）要在 **R11 重跑**时取代的东西——届时版本谱系、提交绑定、封闭子进程环境、子测试 SKIP 四项由工具强制，而非人手。

**结论**：本任务的证据在修复轮 3 之后**应使用共享工具重跑**（清单、提交绑定、封闭子进程环境、子测试 SKIP 四项都会被覆盖，`PROVENANCE.txt` 这类人手声明也将被工具产出取代）。在此之前，请把本报告的全部结论视为「在 §10 所述权威归档与 `PROVENANCE.txt` 的**人手声明**（见第 5 条，是声明不是工具事实）成立的前提下有效」。
