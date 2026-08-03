# Task 8 报告：端到端往返与约束验证

> 归档优先：本报告与 `.superpowers/sdd/2026-09-24-tracearchive-storage-primitives/task-8-evidence/`
> 冲突时**以归档为准**。归档里每一份产物的 sha256 见 §10。

---

## 0. 结论摘要（先给答案）

| 问题 | 结论 |
|---|---|
| 简报六步 | 全部完成（步骤 6 = 提交，见 §3） |
| 端到端逐字节往返（A1） | 通过：2 万行 / 40 个块 / 抽样 200 行逐字节相等（`task-8-evidence/green-run-7-verbose.txt`） |
| 读取千次磁盘零增量（A4） | 通过：**文件数 + 总字节数**两个口径都不变（见 §9，本任务发现并修掉了只比字节数的盲区）。**可观测域 = 落盘形态（root + TMPDIR）；内存形态（C3）按构造不可观测，属 A4 固有限制而非遗漏**（§13.2 第 1 条） |
| **补充一（同 part 跨重开续写）** | **前提成立（实测）**：重开一定换新块，残行恒为该文件的最后一条物理行。**当前不需要计划 2 的「只有完整行落盘」保证**——它是构造函数性质；一旦计划 2 把写指针改成「同一 part 续写」，前提立刻失效（§5） |
| 变异证据 | 9 条变异 **9 红**（断言支撑 9 / 崩溃超时 0 / 允许的绿 0 / 未登记的绿 0 / 证据无效 0），两张权威表 RC=0（§7） |
| `-race` | `./internal/tracearchive/` 79.8s、`./tests/tracearchive/` 41.8s，**均 ok，无竞态报告**（§12） |
| 可选覆盖（reader.go:116-118） | **已覆盖**并证明承重（变异 m06 单条杀死它，§11） |

---

## 1. 前置检查（事实）

| 项 | 值 |
|---|---|
| 起始 HEAD | `c967ecf7d384dcef502fcd1a00a84042a32c8353` |
| `git status --porcelain` | 空（干净） |
| `pgrep -f 'mutat[e]\.py|go tes[t]|go buil[d]'` | 空（`exit=1`） |

两项均满足，未停手。

---

## 2. 交付物

| 文件 | 行数 | 说明 |
|---|---|---|
| `tests/tracearchive/roundtrip_test.go` | 408（新） | B 层：A1 往返 + 补充一三项实验 |
| `tests/tracearchive/constraints_test.go` | 230（新） | B 层：A4 空间约束 + reader.go:116-118 覆盖 |
| `tests/tracearchive/helpers_test.go` | +68 | 追加 B 层共享 helper：`sealPending`(:160)、`sealTask`(:175)、`sealExistingSpool`(:193)、`concatLines`(:207) |
| `tools/trace-mutate/sets/task-8.py` | 153（新） | 写入侧变异集合（**git-tracked**） |
| `tools/trace-mutate/sets/task-8-reader.py` | 173（新） | 读取侧变异集合（**git-tracked**） |
| `.superpowers/.../task-8-report.md` | 本文件 | 进版本库（`git add -f`） |

**无 A 层文件、无新生产代码**（符合 brief:4-6）；唯一对既有文件的改动是 `helpers_test.go` 的**追加**。

## 3. 提交

| 提交 | 内容 |
|---|---|
| `0476da1d` | 两个测试文件 + helpers 追加 + 两个变异集合 |
| `0243b1df` | A4 空间约束补「文件数」观测量（§9 的修复） |
| 本报告提交 | `git add -f .superpowers/.../task-8-report.md`；哈希用 `git log -1 --format=%H -- .superpowers/sdd/2026-09-24-tracearchive-storage-primitives/task-8-report.md` 取（提交无法含自身哈希，故不写死） |

未 amend、未 reset、未 rebase。变异证据绑定在 `0243b1df`（见 §7 的证据头部）。

**证据在最终 HEAD 上依然有效（可复核）**：`git diff --name-only 0243b1df..HEAD` 只列出本报告文件
⇒ 被测文件与集合的 sha256 与两张表的证据头部逐字一致（实测：`writer.go` `3a9829071de9f7d7…`、
`reader.go` `6bb58fdd33c1407c…`、`roundtrip_test.go` `2f72a85fa7e5d519…`、
`constraints_test.go` `2ba8f24ecd381b0e…`、`task-8.py` `5c9511bd5424b7cf…`）。
第三方（含 R11 分支收尾）可在最终 HEAD 上直接重跑两张表，结果应逐字相同。

**关于「集合必须先提交」**：变异工具用 `git archive HEAD` 现取现解 pristine 树，且拒绝脏树
⇒ **测试与集合都必须先提交**，证据才能在 HEAD 上产出。因此提交顺序是「测试+集合 → 跑证据 → 报告」。

---

## 4. 简报六步完成情况

| 步骤 | 状态 | 证据 |
|---|---|---|
| 1 端到端往返测试 | ✔ | `roundtrip_test.go:35` `TestEndToEndRoundTripByteExact`；2 万行 → 40 块 → 抽样 200 行逐字节相等（`green-run-7-verbose.txt`） |
| 2 空间约束测试 | ✔ | `constraints_test.go:97`、`:144`（brief 版 + 本任务加强：文件数/TMPDIR 观测） |
| 3 跑三条具名用例 | ✔ | 7 条用例全绿：`green-run-7-verbose.txt` + `green-run-three-assertions.txt` |
| 4 `-race` | ✔ | `race-two-packages.txt`（两包均 ok） |
| 5 全量 + 构建 | ✔ | `fullsuite-summary.txt`（顶层 113 = A 层 29 + B 层 84，PASS 113、FAIL 0、**全层级 SKIP 0**）；`build-vet-gofmt.txt`（`go build ./internal/... ./cmd/...`、`go vet`、`gofmt -l` 全干净） |
| 6 提交 | ✔ | `0476da1d`、`0243b1df`、报告提交 |

**简报与实现的差异（必须指出）**：brief:48/78/90 写 `bin := requireZstd(t)` 后
`NewSealer(SealerOptions{Root, ZstdBinary: bin})`；本仓的 `requireZstd` 返回 `(bin, ver)`
且 `NewSealer` 在 `ExpectedZstdVersion` 为空时用标定值 `1.4.8`（本机 zstd 为 v1.5.7）
⇒ 照抄 brief 会让**每一次封块**被版本守卫拒绝。本任务按 Task 6/7 的既有做法显式透传本机版本
（`helpers_test.go:133-146` 已有同样的说明），并把它落成静态检查行 `真封块路径走本机zstd`。

---

## 5. 补充一：同 part 跨重开续写端到端实验（本轮最重要）

### 5.1 实验设计

| # | 实验 | 用例 |
|---|---|---|
| 1 | 写若干完整行 → Close → 查看盘上字节 | `roundtrip_test.go:126` `TestRoundTripSurvivesWriterReopen` |
| 2 | 重开同一 `(dt, inst)` → 继续追加 → 封块 → 逐行点读比对 | 同上（`:192`、`:209`、封块 `:215-216`、点读 `:226-236`） |
| 3 | **关闭时最后一字节不是 `\n`** 的形态（残行）→ 重开 | `roundtrip_test.go:251` `TestReopenNeverContinuesResidualSpoolTail` |
| 4 | 反事实：**若前提失效**会怎样 | `roundtrip_test.go:347` `TestResidualTailGluesWhenPremiseBroken` |

### 5.2 实测结果（逐项，均为事实）

**① 干净 Close 后，最后一字节是 `\n`**（`roundtrip_test.go:169` 断言通过）。
每次 `Append` 写「正文 + `'\n'`」（`writer.go:247-270`，换行在 `writer.go:261`），
`Close` 走 `closeCurLocked` 并 `Flush`（`writer.go:414`）⇒ 缓冲全量落盘。
**因此「关闭时最后一字节不是 `\n`」在正常关闭路径上不出现**；该形态只能由崩溃或写失败产生。

**② 重开得到的是新块，不是同一 part**：第一段 part 0，第二段 part 1（`:192` 断言
`refs2[0].Part > refs1[0].Part`），且**第一段 spool 的字节在第二段写入后逐字节未变**（`:209`）。
逐行点读 5 行全部与写入内容相等（`:226-236`）（`green-run-7-verbose.txt`: `跨两次打开（part 0 → part 1）逐行点读通过: 5 行`）。
机制：写指针以目录内容为权威——`nextSeqLocked`（`writer.go:483`）取
`max(part dir, spool dir)` 的序号并 **+1**（`writer.go:499`），`parsePartSeq` 同时接受
`.jsonl` 与 `.jsonl.zst`（`ref.go:76-79`），因此已存在的 spool 一定被计入。
`ensureBlockLocked`（`writer.go:436-476`）随后以 `O_CREATE|O_WRONLY|O_APPEND` 打开**新文件名**。

**③ 残行形态（崩溃态）也不会被续写**：构造「两条完整行 + 一条没有换行收尾的残行」
（`:255-264`，与 `reader_test.go:236-241` 同一手法，因为残行经公开 API 造不出来），然后：
- 残行之前的完整行照常读到（`:271-280`）；
- **残行本身以 `ErrNotArchived` 被拒**（`:282`），且**不是** `ErrBodyMissing`（`:285`）——
  这是 Task 7 残行守卫（`reader.go:285-292`）在**端到端**路径上的正向证据；
- 重开写入器后新的 `Ref.Part == 1`（`:298`），残行文件**逐字节未变**（`:311`）；
- 新块内容可读回（`:321`）。

**④ 反事实（前提承重）**：直接构造「残行之后还有字节」的文件
`"A\n" + "BB" + "CCCC\n" + "DDDD\n"`，实测 Reader 的行为（`:347`）：

| 请求 | 返回 | 性质 |
|---|---|---|
| row 0 | `A` | 正确 |
| row 1 | `BBCCCC` | **残行与下一行粘连**，不是任何逻辑行的内容，且**不报错** |
| row 2 | `DDDD` | **格式正确但属于相邻行（row3）的正文** —— 静默错正文（本项目最不能接受的失效） |
| row 3 | `ErrNotArchived` | 响亮报错（错位只对「落在文件内的行号」静默） |

守卫在此**不触发**：被请求的那条物理行是以换行收尾的，判据（EOF 前仍带回非空字节）用不上。

### 5.3 结论

> **该前提成立**：`残行恒为该文件的最后一条物理行` 不是运行期巧合，而是**构造函数性质**——
> 由「写指针 = max(目录内容)+1」（`writer.go:499`）与「写失败则毒化 + 改名隔离」两条共同保证。

**对计划 2（写入侧）的判定**：
- **在当前写指针策略下，不需要**额外引入「只有完整行落盘」的保证；读者侧的残行守卫
  （`reader.go:285-292`）与 sealer 的残行守卫（`sealer.go:270-274`）不会因为续写而失效。
- **但前提是可以被写侧改掉的**：只要计划 2 让写入器**在已存在字节的同一 part 上继续追加**
  （例如按「逻辑块号」重开续写、或复用最大已用序号），前提立即失效，后果就是 §5.2 ④的
  静默错正文。本任务的变异 `task-8.py` 的 m01（把 `maxSeq+1` 改成 `maxSeq`）**就是该回归的探针**：
  它一改，`TestRoundTripSurvivesWriterReopen`（`:192`）、`TestReopenNeverContinuesResidualSpoolTail`
  （`:298`、`:311`）与 `TestEndToEndRoundTripByteExact` 立刻转红（三侧原始输出见
  `two-sided-writer-m01-premise.txt`）。
  ⇒ 建议：计划 2 若改动写指针策略，**必须同时**引入「只有完整行落盘」的保证，并保留本任务的两条用例。

### 5.4 附带发现（文档级，未改代码）

`writer.go:457` 的注释写「**O_APPEND：进程重启后继续往同一个 spool 追加**，不覆盖已写内容」。
本实验实测：重启/重开**一定开新 part**，**不会**继续同一个 spool（§5.2 ②③）。
O_APPEND 在当前实现里是纵深防御（真正起作用的是 `nextSeqLocked` 的 +1），
注释描述的场景与行为不符。行为无缺陷、无用例可区分，因此**按边界要求不改生产代码**，
只在此登记；建议计划 2 落地时同步修正注释（或让实现真正支持「续写同一 part」——但那样就回到 §5.3 的红色条件）。

---

## 6. 补充二 / 补充三：共享工具与 git-tracked 集合

**补充二（用共享工具，不新写脚本）**：全部证据由 `tools/trace-mutate/trace_mutate.py` 产出，
调用的参数形式与 README 一致（`--manifest` / `--mutation-set` / `--packages` / `--target` /
`--side` / `--only` / `--selftest`），**没有**任何任务专用变异脚本。命令逐字见
`writer-mutation-table.txt`、`reader-mutation-table.txt` 的头部与 §7。

**补充三（集合进版本库）**：`task-8.py`（sha256 `5c9511bd5424b7cf…`）与
`task-8-reader.py`（sha256 见 `reader-mutation-table.txt` 头部）都在 `tools/trace-mutate/sets/` 下，
随 `0476da1d` 进版本库。名字清单留在证据目录（可推导；工具打印推导命令并**实际重推比对**，
本轮结果「一致（推导出 7 条）」，`names.txt` sha256 `eb0cfda7dbf9ac99…`）。

**为什么是两份集合**：工具的 `--target` 一次只接受一个文件，而 Task 8 的端到端用例同时压住
写入侧（构造性质）与读取侧（守卫/空间约束）。两份集合共用同一份名字清单，**各自产出一张权威表，缺一不可**。
如果分支收尾（R11）希望「一个任务一个集合文件」，需要工具先支持多 target；本任务不改工具引擎。

---

## 7. 变异证据（两张权威表，9/9 红）

证据绑定（两张表头部逐字一致，此处摘录）：

```
git rev-parse HEAD       = 0243b1df1c107d0ad3b5540cf40b17d093b12f44
git status --porcelain   = (空)
工具自身 sha256          = 10dea750fec073af9fd5b3af262ad6ec6f020aefa223da6b079e1c5a9cf708f8
变异集合 sha256          = 5c9511bd5424b7cf…  (task-8.py)
名字清单条数             = 7   推导结果与清单比对 = 一致（推导出 7 条）
被测文件 sha256          = 3a9829071de9f7d7…  internal/tracearchive/writer.go
                       = 6bb58fdd33c1407c…  internal/tracearchive/reader.go
子进程环境（白名单）     = HOME LANG LOGNAME PATH SHELL TMPDIR USER
```

### 7.1 原始侧自证（pristine）

两张表各自 `原始侧自证: OK names=7 ran=7 diff=0 PASS=7 SKIP=0 FAIL=0`
⇒ 7 条用例全部执行、全部有终判、**全层级 SKIP = 0**。

### 7.2 写入侧（`task-8.py`，RC=0）

| 变异 | 判定 | 自证串 | 杀死的用例 |
|---|---|---|---|
| m01_写指针不加一（重开复用已用序号） | 红 | names=7 ran=7 diff=0 PASS=4 SKIP=0 FAIL=3 | TestEndToEndRoundTripByteExact, TestReopenNeverContinuesResidualSpoolTail, TestRoundTripSurvivesWriterReopen |
| m02_Close不flush | 红 | names=7 ran=7 diff=0 PASS=5 SKIP=0 FAIL=2 | TestReopenNeverContinuesResidualSpoolTail, TestRoundTripSurvivesWriterReopen |
| m03_删除行尾换行 | 红 | names=7 ran=7 diff=0 PASS=4 SKIP=0 FAIL=3 | TestEndToEndRoundTripByteExact, TestReopenNeverContinuesResidualSpoolTail, TestRoundTripSurvivesWriterReopen |

### 7.3 读取侧（`task-8-reader.py`，RC=0）

| 变异 | 判定 | 自证串 | 杀死的用例 |
|---|---|---|---|
| m01_删除spool残行守卫 | 红 | names=7 ran=7 diff=0 PASS=6 SKIP=0 FAIL=1 | TestReopenNeverContinuesResidualSpoolTail |
| m02_产物读取落临时文件到root | 红 | names=7 ran=7 diff=0 PASS=6 SKIP=0 FAIL=1 | TestReadsDoNotGrowDiskUsage |
| m03_产物读取落临时文件到TMPDIR | 红 | names=7 ran=7 diff=0 PASS=6 SKIP=0 FAIL=1 | TestReadsDoNotGrowDiskUsage |
| m04_spool读取落临时缓存 | 红 | names=7 ran=7 diff=0 PASS=6 SKIP=0 FAIL=1 | TestUnsealedReadsAlsoDoNotGrow |
| m05_产物路径不去掉行分隔符 | 红 | names=7 ran=7 diff=0 PASS=5 SKIP=0 FAIL=2 | TestEndToEndRoundTripByteExact, TestRoundTripSurvivesWriterReopen |
| m06_打开产物失败不包装底层错误 | 红 | names=7 ran=7 diff=0 PASS=6 SKIP=0 FAIL=1 | TestMissingArtifactAfterStatIsReportedAsOpenError |

### 7.4 分层计数（工具有效性 + 判定必须改变退出码）

```
写入侧：变异总数 3 = 3 断言支撑 + 0 崩溃/超时/编译 + 0 允许的绿；未登记的绿 0；证据无效 0
读取侧：变异总数 6 = 6 断言支撑 + 0 崩溃/超时/编译 + 0 允许的绿；未登记的绿 0；证据无效 0
```

- 判定**改变退出码**：两张表 RC 均为 0；`0243b1df` 之前的读取侧一跑 RC=1（§9）。
- **空清单**与 **`[no tests to run]`** 都先判无效：`PACKAGES` 里刻意**只放** `./tests/tracearchive/`
  （若把 `./internal/tracearchive/` 也放进去，那个包里一条都不匹配 ⇒ `[no tests to run]` ⇒ 整轮无效）。
  `--selftest`（7 条名字）把 7 组合成反例全部抓住，退出码 0：
  `自检结论: 全部被抓住 ⇒ 检测有效（三断言 + 冻结清单 + 封闭环境都在生效）`（`selftest-7names.txt`）。
- **计数式判据**：每行的 `diff` 必须为 0、`ran` 必须等于 7、`PASS+FAIL+SKIP` 必须等于 7、SKIP 必须为 0；
  任一不等即判无效（§9 的 m04 就是靠「未登记的绿必须为 0」把整轮判失败的）。

### 7.5 D15 遮蔽效应：新条目的单条目隔离探针（两侧都展示）

7 条用例彼此**没有共享断言环**（无表驱动、无公共断言函数），因此每条变异杀死的用例集合
就是「哪条用例承重」的直接读数：

| 新条目 | 隔离探针（只杀死它的变异） | 变异侧的失败断言（逐字） |
|---|---|---|
| `TestReopenNeverContinuesResidualSpoolTail` | 读取侧 m01 | `reader.go…: 残行属「存储状态异常」（ErrNotArchived/500 类），实际 tracearchive: 读 spool 失败: EOF` |
| `TestUnsealedReadsAlsoDoNotGrow` | 读取侧 m04 | `constraints_test.go:171: spool 读取后归档根目录下的文件数从 2 变为 502` |
| `TestMissingArtifactAfterStatIsReportedAsOpenError` | 读取侧 m06 | `constraints_test.go:224: 错误链必须保留底层权限错误（EACCES），实际 …permission denied` |
| `TestReadsDoNotGrowDiskUsage` | 读取侧 m02 / m03（各 1 条） | root 字节数增长（`:129`）/ TMPDIR 文件数增长（`:133`） |
| `TestRoundTripSurvivesWriterReopen` | 写入侧 m01 的 part 断言 | `roundtrip_test.go:193: 重开后仍落在 part 0（第一段是 part 0）⇒ 在同一文件上续写…` |

**两侧**（pristine 绿 + 变异红）的完整原始输出：
`two-sided-writer-m01-premise.txt`（94 行；`### 原始侧…` 在第 47 行、`### 变异侧 m01…` 在第 69 行）、
`two-sided-reader-isolated-probes.txt`（134 行；原始侧第 38 行，三条隔离探针分别在第 60 / 85 / 110 行）。

唯一一次「一条变异杀死三条用例」是写入侧 m01 —— 这不是遮蔽，而是同一个构造性质被三条用例
**独立**断言（各自死在自己的判别断言上，见 §7.2 的杀伤列与 `two-sided-writer-m01-premise.txt` 的三行 FAIL）。

---

## 8. 映射表对账（如实：计划 Task 8 小节**没有**这类清单）

**核对方法（可复现）**：对简报全文扫「删/转红/会红/必须红/变红/变异」，逐条判归属：

```
grep -n '删\|转红\|会红\|必须红\|变红\|变异' task-8-brief.md
```

**核对结果：`task-8-brief.md` 的 Task 8 小节（第 1–269 行，步骤 1–6）里没有任何
「删掉 X ⇒ 某用例必须转红」式条目。** 上述模式全文命中 **16 行**，其中落在 Task 8 小节
（1–269 行）内的**只有 1 行**（`sed -n '1,269p' … | grep -n …` 输出仅 `172:` 一条）：

| 简报位置 | 内容 | 归属判定 |
|---|---|---|
| **brief:172** | 「若实现被改成『解压到 /tmp 再读』或『缓存已解压块』，此测试会立刻转红」 | **Task 8 小节内唯一的「转红」声明**（且是测试内注释，不是任务要求）。见 §9：只对一半成立，已按实测修正 |
| brief:278 / 286 / 287 | D4「封出空产物并删掉 spool」承重行、Inst/dt 白名单「删掉它没有任何用例转红」、移除 `ListParts`（连同其用例一并删除） | Task 5/6 的计划缺陷登记（**不是** Task 8 的要求） |
| brief:293 / 297 / 302 / 305 / 306 / 307 | 编号续说明、D7、D12、D15、D16、D17 | 方法论缺陷登记（非任务要求） |
| brief:319 / 331 / 333 / 337 / 339 / 373 / 404 | R8、R11 的理由/做法/必须满足项、CI 建议、§14.1 自检行、`ListPendingSeals` 方法形态（包级函数已删除） | 计划 2 对端要求 / 分支收尾 / **自检表** |

**自检表（brief:343-425）里与 Task 8 直接相关的行共 6 条，逐条映射如下**（其中 brief:373 一行的判定是「不适用」）：

| 自检表行 | 要求 | 本任务的承重证据 |
|---|---|---|
| brief:373 | §14.1 变异测试「**每道防线摘除必须转红**」 | 该行**只列任务 2/3/5/6/7**，**不含任务 8** ⇒ Task 8 没有这道清单式要求 |
| brief:374 | §14.2 A1 逐字节往返 | `m03_删除行尾换行` + `m05_产物路径不去掉行分隔符`（+ 静态检查 `真封块路径走本机zstd`） |
| brief:375 | §14.2 A4 零空间增量 | `m02`/`m03`/`m04` 三条（各证明一个观测量承重） |
| brief:362 | §8.3 C1~C4 空间硬约束 → 任务 8（断言） | 同上（A4 两条用例） |
| brief:418 | Task 8 分层 = **无 A 层**，B 层 `roundtrip_test.go` + `constraints_test.go` | 静态检查 `B层-roundtrip`、`B层-constraints` |
| brief:392 | 任务 6/7/8 的封块类用例需要本机 zstd | 静态检查 `真封块路径走本机zstd`（`requireZstd` 缺失即 SKIP ⇒ 全层级 SKIP==0 判整轮无效） |

⇒ **没有「要求未覆盖」的缺口，也没有需要补的变异条目**；`未被任何计划要求引用的变异 = 0`，
反向的 `每条计划要求都有对应条目 ✓` 由工具 `--mapping` 逐条打印（两张表的映射表段）。

除 brief:172 这一条外，没有再为凑表造条目。

集合里的映射表（`--mapping` 输出，逐字见两张表）：
写入侧 4 条要求（B 层分层 ×1、A1 ×1、Close 落盘 ×1、补充一前提 ×1）、读取侧 4 条要求
（A4 ×1、残行守卫 ×1、A1 读侧 ×1、reader.go:116-118 ×1）；
`映射表核对：每条计划要求都有对应条目 ✓`、`未被任何计划要求引用的变异 = 0`。
「补充一前提」这条**不在 plan 里**，已在集合的 `plan` 字段里显式标注来源是本轮新增要求。

---

## 9. 过程中的重要发现：A4 口径曾判绿（已修，未登记成「允许的绿」）

第一次跑读取侧集合（HEAD=`0476da1d`）时：

```
| m04_spool读取落临时缓存 | **绿（零覆盖！）** | names=7 ran=7 diff=0 PASS=7 SKIP=0 FAIL=0 | — |
未登记的绿（必须为 0）     = 1 条：m04_spool读取落临时缓存
!!! 有 0 条证据无效 / 1 条未登记的绿 ⇒ 整轮失败（绝不能算绿）      READER RC=1
```

**这次运行的原始表已归档**（可复核，不必信本报告的转述）：`task-8-evidence/reader-mutation-table-at-0476da1d-m04-GREEN.txt`（sha256 `a4087dbe0204b923…`，RC=1）。复现方式：在提交 `0476da1d` 上重跑读取侧集合——
`git worktree add --detach /tmp/t8-old 0476da1d` 后执行 §7 的同一条读取侧命令
（`--repo /tmp/t8-old`）；该提交的 `constraints_test.go` 里 `treeFileCount` 出现 **0** 次，这正是差异所在。

**根因**：brief 的 A4 断言只比较**目录内文件的总字节数**（`dirUsage`）。
m04 让每次读 spool 都落一个 `os.CreateTemp` 出来的**0 字节**临时文件 —— 文件确实出现了，
但总字节数**恰好不变**。也就是说：**原口径对「落空文件」这类实现完全不可见**，
而 brief 的注释把「落临时文件」列为该测试应当抓住的反模式。

**处理（不是把它登记成「允许的绿」）**：给两条 A4 用例补上**文件数**观测量
（`constraints_test.go:48` `treeFileCount`，`:129`、`:170` 断言文件数不变），产物路径另比较 TMPDIR 的文件数
（`:75` `scratchTempDir`、`:133` 与其配对断言）。修复后 m04 转红，且**只**杀死 `TestUnsealedReadsAlsoDoNotGrow`
（§7.5 的隔离探针）⇒ 新观测量被证明承重，而不是装饰。修复提交 `0243b1df`，两张表在同一 HEAD 重跑。

**顺带登记两个"自算坑"**（都在我**工具之外**的复算脚本里，工具的自证串不受影响，
已写在 `green-run-three-assertions.txt` 里）：① `tr -d '[:space:]'` 若在 `sort` **之前**，
会把名字压成一行使排序失效 ⇒ 两侧 diff 永远非空（假红）；② 顶层计数必须用
`^=== RUN   Test[A-Za-z0-9_]*$`，缺 `$` 会把子测试行（`=== RUN   TestFoo/bar`）算进顶层
⇒ 全量 A+B 会从 113 抬到 150。**权威判定用工具，不用临时 grep** —— 这正是本项目的判据。

---

## 10. 归档清单（sha256；归档不进版本库）

目录：`.superpowers/sdd/2026-09-24-tracearchive-storage-primitives/task-8-evidence/`

| 文件 | 内容 | sha256 |
|---|---|---|
| `names.txt` | 名字清单（7 条，可推导） | `eb0cfda7dbf9ac99…` |
| `writer-mutation-table.txt` | 写入侧权威表（含证据头部、静态检查、映射表、计数口径） | `eba45bb09a7c42fd…` |
| `reader-mutation-table.txt` | 读取侧权威表（同上） | `136d2441b59ba726…` |
| `two-sided-writer-m01-premise.txt` | 写入侧 m01 双侧完整原始输出 | `d9d64fef7fecf671…` |
| `two-sided-reader-isolated-probes.txt` | 读取侧 m01/m04/m06 双侧完整原始输出 | `81f357ed102d712d…` |
| `green-run-7-verbose.txt` | 7 条用例绿跑原始输出 | `0b61e35df8617b46…` |
| `green-run-three-assertions.txt` | 三断言独立复算（含两个自算坑的登记） | `bbcc3c402b62661a…` |
| `fullsuite-two-packages-verbose.txt` | A+B 全量详细输出 | `e4c0c1fa90dc3a36…` |
| `fullsuite-summary.txt` | 全量计数（顶层 113 = 29 + 84） | `ce98fb9f8dabd531…` |
| `race-two-packages.txt` | `-race` 两包结果 | `23e669d1e84860a3…` |
| `build-vet-gofmt.txt` | 构建 / vet / gofmt | `ad010465a4fef519…` |
| `selftest-7names.txt` | 工具自证（7 组反例全被抓住） | `7e5b906aa50ed967…` |
| `reader-mutation-table-at-0476da1d-m04-GREEN.txt` | §9 的复现证据：**旧口径下 m04 判绿、整轮 RC=1**（在提交 `0476da1d` 上重跑） | `a4087dbe0204b923…` |

（完整 64 位哈希由 `shasum -a 256 task-8-evidence/*` 复现；本表为便于阅读截断到 16 位。）

---

## 11. 可选覆盖：`reader.go:116-118`（复审登记的疑似零覆盖）

**已覆盖，且证明不是"看起来测到了"**：`constraints_test.go:188`
`TestMissingArtifactAfterStatIsReportedAsOpenError` 用 **mode 0o000 的产物文件**构造出
「`os.Stat` 成功、`os.Open` 失败（EACCES）」的窗口（`:203` 前置复核 `Stat` 必成功、`:206` 复核
`Open` 必失败），断言错误文本命中「打开产物失败」、`:223` 断言错误链保留
`fs.ErrPermission`、且**没有被归入** `ErrBodyMissing`/`ErrNotArchived` 两个哨兵。
承重证据：变异 m06 把该分支的 `%w` 改成 `%v`（文本仍在、错误链断掉），
**单条杀死此用例**（§7.5）。

**代价与限制（如实登记）**：以 **root** 运行时权限位不生效 ⇒ `t.Skip`。由于集合的
`SKIP_WHITELIST = []`，一旦以 root 重跑，**整轮证据会判无效并非零退出**（响亮，而不是静默丢覆盖）。
本机 euid=501，本轮无 SKIP（全层级 SKIP=0）。

---

## 12. `-race` 与构建（逐字）

```
$ go test -race ./internal/tracearchive/ ./tests/tracearchive/ -count=1
ok  github.com/example/llm-gateway/internal/tracearchive  79.838s
ok  github.com/example/llm-gateway/tests/tracearchive     41.835s     （无 DATA RACE 报告）
$ go build ./internal/... ./cmd/...     → rc=0（无输出）
$ go vet ./internal/tracearchive/ ./tests/tracearchive/   → rc=0（无输出）
$ gofmt -l internal/tracearchive/ tests/tracearchive/     → 无输出
```
未跑 `go build ./...`（`test/bedrock_model_id_check.go` 有既存编译错误，按边界要求）。

---

## 13. 覆盖边界与留白（不是静默遗漏）

### 13.1 证据分级（本项目发生过「把推理写成实测」的事故，故逐条标注）

**A. 实测**（有归档原始输出，第三方可重跑）：

| 断言 | 位置 | 归档 |
|---|---|---|
| 2 万行 → 40 块 → 抽样 200 行逐字节相等 | `roundtrip_test.go:35` | `green-run-7-verbose.txt` |
| 干净 Close 后末字节 == `'\n'` | `:169` | 同上 |
| 重开换新块（part0 → part1）、旧 spool 逐字节不变、5 行点读相等 | `:192`/`:209`/`:226-236` | 同上 |
| 残行之前的完整行可读、残行以 `ErrNotArchived` 被拒（非 `ErrBodyMissing`）、重开新 part、残行文件逐字节不变、新块可读 | `:271-285`/`:298`/`:311`/`:321` | 同上 |
| 反事实：粘连 `BBCCCC` + 相邻行错正文 `DDDD` + row3 `ErrNotArchived` | `:347` 起 | 同上 |
| A4：字节数 **与** 文件数在两处（spool / 产物）读取后均不变 | `:129`/`:133`/`:170` | 同上 |
| `reader.go:116-118`：`Stat` 成功但 `Open` EACCES ⇒ 包装文本 + `fs.ErrPermission` 错误链 | `constraints_test.go:203`/`:206`/`:223` | `green-run-7-verbose.txt` |
| 9 条变异逐条转红 | — | 两张权威表 + 两份双侧输出 |
| `-race` / 全量 / 构建 | — | `race-*.txt`、`fullsuite-*.txt`、`build-vet-gofmt.txt` |
| `writer.go:457` 注释所述「重启续写同一 spool」与实测行为不符 | §5.4 | §5.2 ②③ 的实测 |

**B. 构造输入 + 实测行为**（输入无法由公开 API 产生，测试直接写盘构造；其上的观测全部是实测）：

- 崩溃态残行文件（`roundtrip_test.go:255-264`，与 `reader_test.go:236-241` 同一手法）；
- 「残行之后还有字节」的反事实文件（`:347` 起）；
- EACCES 权限窗口（`constraints_test.go:203-206`，mode `0o000`）；
- 旧口径下 m04 判绿（在提交 `0476da1d` 上**实跑**，表已归档，非事后推断）。

**C. 推理（未跑，只能算推理，不得当证据用）**：

- 关闭时 `Flush` 失败（ENOSPC）留下的残行不破坏前提（§13.2 第 4 条）；
- `bufio` 在 `Write(body)` 与 `WriteByte('\n')` 之间的活跃 spool 中间态对并发读者的表现（§13.2 第 5 条）；
- 真实写失败（ENOSPC/短写）→ 毒化 → 隔离 → 换 part 的完整链路（本任务未做端到端，由 Task 5 的 A 层用例覆盖）；
- C3 内存形态的可观测性（按构造不可观测，见下）。

### 13.2 逐条边界

1. **A4 的可观测域 = 落盘形态（`<root>` 下 + 进程 TMPDIR 下）**，以「**总字节数 + 文件数**」双口径覆盖。
   **内存形态（C3「不缓存已解压块」）按构造不可观测**——读取在堆里留一份已解压块不产生任何文件系统
   可观测差异 ⇒ 这是 **A4 的固有限制，不是遗漏**（brief:172 把「缓存已解压块」列为 A4 能抓住的反模式，
   属**简报的过度承诺**，已按实测更正注释）。
   **为什么收窄后依然达成目的**：A4 要守的是 `/data` 与 `/` 的**磁盘增长**（生产事故正是这两处 ENOSPC）；
   内存中的解压副本消耗 RAM，**不属于 A4 要守的资源**。
   **已登记**：若将来要给内存形态加约束，需要一个**显式的指标/钩子**（而非文件系统观测量）——**本轮不做**。
2. **TMPDIR 观测只管「通过 `os.TempDir()`/TMPDIR 解析临时目录」的形态**；写死的绝对路径（如 `/tmp/x`）
   无法从环境观测 ⇒ 属同一类固有限制，已接受。
3. **残行是构造出来的，不是真实写失败产生的**（见 §13.1 B）。真实写失败链路不是本任务的端到端证据。
4. **关闭时 `Flush` 失败（ENOSPC）留下的残行未做端到端实验**（测试里无法安全制造 flush 失败）。
   推理上它同样不破坏前提：`closeCurLocked` 之后 `cur` 置空（`writer.go:406-422`），
   后续 `Append` 一定开新 part ⇒ 残行仍留在原文件末尾，只会让该行被响亮拒绝（`ErrNotArchived`）。
   **这是推理，不是实测。**
5. 写指针的「活跃 spool」中间态：`bufio` 的 1 MiB 缓冲可能在 `Write(body)` 与
   `WriteByte('\n')` 之间停在行中间，此时该行**尚未**由 `Append` 返回给调用方
   ⇒ 对并发读者表现为残行守卫触发（响亮），不会返回截断正文。**同为推理，未做并发实验。**

---

## 14. 本轮没做到 / 做得不好的地方

1. **两个集合文件而不是一个**：受工具 `--target` 单文件限制。若 R11 的「一个任务一次调用」
   要严格成立，需要工具支持多 target（本任务按边界未改工具引擎）。
2. **A4 的第一版口径有真实缺陷**（只比字节数 ⇒ 0 字节临时文件不可见）。它被工具判绿逼出来
   （§9），但**这个缺陷是我先写进测试的**，说明我在写 A4 时把 brief 的注释当成了已验证的事实。
3. **Task 7 的变异集合仍在 gitignored 目录**（`.superpowers/.../task-7-mutate.py`），
   属 D17 形态（可被悄悄删掉、`git diff` 看不见）。不在本任务范围，但
   **R11 若要按共享工具重跑 task 7 的证据，必须先把它迁到 `tools/trace-mutate/sets/`**。
   我为此在本任务集合里保留了一条同源条目（读取侧 m01），但那只覆盖 reader.go 的一条守卫。
4. 我未能把 §13 的 3/4/5 三条推理做成实验（真实写失败注入需要 ENOSPC 或受控 fd 失效，
   当前环境没有安全做法）；它们以「推理」而非「证据」出现在报告里。
5. 工具外的复算脚本一开始有两处 D12 形态的坑（§9 末），我是在读到 diff 非空时才发现并修正的
   —— 这不是面子问题，而是提醒：**手写 grep 断言不可作为判据**。

---

## 15. 状态契约（≤15 行）

```
1 交付：tests/tracearchive/roundtrip_test.go(408) constraints_test.go(230) 新；helpers_test.go +68（4 个 helper）
2 变异集合（git-tracked）：tools/trace-mutate/sets/task-8.py(153) + task-8-reader.py(173)（工具单 target ⇒ 拆两份）
3 前置：起始 HEAD c967ecf7…、工作树干净、无竞争进程 ⇒ 通过
4 提交：0476da1d（测试+集合）、0243b1df（A4 补文件数观测量）；报告提交见 §3 的取值命令；未 amend/reset
5 变异计数（证据绑定 0243b1df）：9 条 → 9 红 = 9 断言支撑 + 0 崩溃/超时/编译 + 0 允许的绿；未登记的绿 0、证据无效 0
6            两张表 RC 均为 0；原始侧自证 names=7 ran=7 diff=0 SKIP=0（diff 0243b1df..HEAD 只含报告 ⇒ 哈希仍一致）
7 分层/隔离：单条目探针 3 条（reader m01/m04/m06 各杀 1 条用例），两侧原始输出已归档
8 曾判绿并已修：reader m04（0 字节临时文件在「只比字节数」口径下不可见）→ 补 treeFileCount 后转红
9 测试：7 条绿跑 diff=0/PASS=7/全层级 SKIP=0；全量 113 顶层（A29+B84）PASS=113 FAIL=0 全层级 SKIP=0
10 -race/构建：两包 79.8s+41.8s ok 无竞态；go build ./internal/... ./cmd/...、go vet、gofmt -l 全干净（未跑 go build ./...，既存错误）
11 补充一结论：前提【成立】（实测）——重开一定换新块(+1)、旧文件逐字节不变、残行被 ErrNotArchived 拒
12             ⇒ 当前【不需要】计划 2 的写入侧保证；但写指针若改为「同 part 续写」则立刻失效（m01 即探针）
13 可选覆盖：reader.go:116-118 已覆盖并由 m06 单条杀死证明承重（root 身份下 SKIP ⇒ 整轮无效，响亮）
14 审计：Task 8 小节无「删掉 X⇒转红」清单（全文 16 处命中仅 brief:172 落在 1–269 行内）；自检表 6 行已逐条映射，无缺口
15 不足（控制者裁决口径）：C3 内存形态 = A4 固有限制（非缺陷，需显式指标/钩子，本轮不做）；TMPDIR 不含写死路径；残行/真实写失败/关闭失败为构造或推理非实测（§13.1 三级分列）
```
