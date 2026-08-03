# Task 5 报告：spool 顺序追加写入器

**状态：** DONE（含 3 处计划缺陷登记、5 处已批准的偏离、§10 两条评审修正；控制者裁决 1 已落实为标记行）
**提交：** `8cd873e7` feat(tracearchive): spool 顺序追加写入器与写指针 → **`52ccc9b2` fix(tracearchive): 校验 dt 路径分量并删除无消费者的 ListParts**（父提交 `7f5eda3c`，控制者在我工作期间合入的 Task 4 修正）
**评审提示：** §4/§6/§7 记录的是首轮（`8cd873e7`）的状态，**以 §10 的最终用例清单与变异表为准**（§10 说明了被它取代的三处：ListParts 已删、dt 校验已加、用例数与行数已变）。

> 📌 **勘误（控制者，`3ff5c0d0` 时补记）**：本报告早段的「文件」表称**本任务无 A 层文件**，与代码矛盾——
> `internal/tracearchive/writer_internal_test.go` 实际存在（**5 个用例**），且本报告 §11.4 自己也列了这 5 条。
> **以代码为准**：本任务**有** A 层文件。修正轮 1（`771d7985`）新增行校验与写失败隔离后，
> A 层用例增至 5 条（含「为什么必须触达未导出符号」的理由注释）。
> 该矛盾由文档代理在 `3ff5c0d0` 对齐规格与计划时**通过读代码**发现（而非读本报告）。
>
> 📌 **勘误补充（控制者，`f33d1091` 时补记）**：本报告 **§7.C（约 :261）** 与 **§10.1（约 :298）**
> 仍写「`dt="../x"` 会被 `filepath.Join` 清成 `<root>/x`」——**该示例不成立**（`PartDir` 会拼回 `dt=` 前缀，
> 紧贴前缀的 `..` 被吸收为字面名，不折叠）。**结论（`dt`/`inst` 必须校验）仍然正确**，只是示例错。
> **真穿越向量须含 `/`**：`dt="2026-09-24/../.."` ⇒ `PartDir` = `<base>/T/inst=blue`（**已实测在 root 之外创建文件**）；
> `Inst="../../green"` ⇒ `<root>/dt=X/green`。详见 task-6-report §12.10 更正表与本报告上方的勘误指针。
**工作目录：** `/Users/jsy/Public/self_code/llm-gateway/.worktrees/trace-body-direct-write`

---

## 1. 实现了什么

| 文件 | 内容 |
|------|------|
| `internal/tracearchive/writer.go`（新建，首轮 323 行 → §10 后 340 行） | `WriterOptions` / `Writer` / `NewWriter` / `Append` / `AppendFailed` / `PendingSeals` / `MarkSealed` / `Close` / `SealTask`，以及私有 `curBlock`、`closeCurLocked`、`ensureBlockLocked`、`nextSeqLocked`、`trimNewline`、`failedRowMarker`、`validateDT`、`instBlue`/`instGreen`/`dateLayout`。（首轮还含 `ListParts`，已按 §10 裁决 2 整段移除） |
| `tests/tracearchive/writer_test.go`（新建，首轮 592 行 → §10 后 602 行，B 层 `package tracearchive_test`） | 17 个用例（§10 后：移除 `ListParts` 用例、新增 `dt` 校验用例），全部经公开 API；无 A 层文件（本任务没有必须触达未导出符号的断言） |

关键行为：

- **请求路径只做顺序追加**：`Append` 经 `bufio`（1 MiB）写入 `O_APPEND` 打开的 spool，不做任何压缩（方案 C）。
- **双触发封块**：`bytes ≥ BlockBytes` 或 `time.Since(startedAt) ≥ SealInterval`；`0` 值取 `defaultBlockBytes`(1.5 GiB) / `defaultSealInterval`(15 min)。
- **写指针以目录内容为权威**（规格 §7.3）：`max(产物序号, spool 序号) + 1`，产物与 spool 两个目录都扫；半成品 `.tmp`、非 `part-` 前缀、位数不符一律由 `parsePartSeq` 拒绝。
- **`PendingSeals` 兼作周期性 flush 入口**：**无条件** flush 写缓冲，flush 失败以 error 上抛（见缺陷 A）。
- **`AppendFailed` 推进行号 + 落 `null` 标记行**（见缺陷 C，控制者裁决 1）。
- **`Inst` 白名单**（控制者裁决 4）：只接受 `"blue"`/`"green"`。

## 2. 三处计划缺陷（供规格与计划更新）

### 缺陷 A（承重）：`PendingSeals` 的 flush 语义 —— 简报原文会静默丢数据（另一个 D3）

简报给出的实现把 `cur.w.Flush()` 放在**阈值判断之后**，即只有「本轮要产出封块任务」时才 flush。这与方案 C 的生产职责直接冲突：`PendingSeals()` 是网关唯一的周期性 flush 入口（gateway 定时器按 `flush_interval` 调用，规格 §7.1）。

后果（低峰/正常流量下每天都会发生）：一个块在建立后的**前 `SealInterval`（15 min）内**磁盘上始终是**空文件**，且它的 mtime 停留在**创建时刻**。sealer 的 `ListPendingSeals` 用 `mtime ≤ now-minAge` 判定「已停止追加」（任务 6），于是它会把这个**仍在追加**的块判为已停止，封出一个 0 行产物、删掉 spool，缓冲里的正文全部丢失且**无任何报错**。这与 D3（忘记 flush）是同一类静默丢数据，只是触发条件从「忘了 flush」变成「没到阈值」。

证据（变异 M2 直接复现简报原文的位置，见 §5）：把 flush 移回阈值判断之后，4 个用例转红，其中包括「正文已 flush」的直接断言。

**已实现**：flush 提到 `cur == nil` 判断之后、所有早退之前，即**无条件**执行；代码注释写的是生产理由（mtime 必须反映仍在追加），不是测试理由。

### 缺陷 B（资源泄漏）：`MarkSealed` 只把 `cur` 置 nil ⇒ 每封一块泄漏一个 `*os.File`

简报原文 `w.cur = nil` 丢弃了对 `*os.File` 的唯一引用，既不 `Flush` 也不 `Close`：`ensureBlockLocked` 下一次换块时看到的是 `cur == nil`，旧句柄永远不会被关闭。按 `BlockBytes=1.5 GiB` + 15 min 时间触发估算，每实例每天约上百个 fd，长跑进程最终 EMFILE。任务 8 的端到端流程正是在 `Seal` 之后调 `MarkSealed`，因此这条路径是可达的。

**已实现**：`_ = w.closeCurLocked()`（flush + close），签名不变，任务 8 的调用点与运行时序（`PendingSeals` 已先 flush，此刻缓冲为空）不受影响。调用点冻结为无返回值，close 错误只能丢弃——代码注释里说明了为什么这与「`PendingSeals` 必须上抛 flush 错误」不矛盾（后者是为了不把不完整的块交给 sealer；此处已晚于 sealer 读完文件）。

### 缺陷 C（承重）：`AppendFailed` 只推进行号不写字节 ⇒ 产物行序与 `row_seq` 错位，点读静默返回**错误的正文**

简报原文用「只推进行号不写字节」来表示正文从未落盘。这与**冻结的产物格式**不相容：

- footer 的 `row_count` 是产物内**行数**（`footer.go:38`）；
- trailer 是「每行一个差分 varint」的偏移表（`index.go` 的 `encodeTrailer`），varint 差分编码**没有表达「本行没有偏移」的能力**，它假定行稠密；
- 读取侧两条路径都是「第 row 行」：`Reader.readFromSpool` 按物理行序取第 `row` 行（规格 §8.2「行号即顺序」），产物路径取 `offsets[row]`。

因此跳过一行会让其后的所有行整体前移一位：`row=N` 会取到第 `N+1` 行的正文——**静默返回错误的正文**，正是本方案最不能接受的一类缺陷（v3 的教训）。任务 6 的行数交叉校验以 `ErrRowCountMismatch` 拒绝封块，是同一个矛盾的另一表征，不是独立问题。

**已实现**（控制者裁决 1）：`AppendFailed(dt)` 复用 `Append(dt, failedRowMarker)` 写一个 `null` 标记行，行号推进语义不变。复用的理由是让标记行与真实正文共用同一段写入代码（trim/换行/行号/字节计数），「物理行序 == row_seq」由构造保证。标记取 `null`：合法 JSON、`spool` 每行都是正文对象故不可能与其混淆；空行会让按行解析 `.jsonl` 的工具炸掉，`{}` 与合法空正文无法区分。

**下游约定（请转告任务 6/7）：**
- 任务 6 的行数交叉校验现在天然对齐（物理行数 == `Rows`）。
- 任务 7 的 `Reader` 遇到标记行应返回 `ErrBodyMissing`，**不得**把 `"null"` 当作正文返回。该约定已写入 `AppendFailed` 的代码注释。

## 3. 与简报的偏离清单（5 处，均在上文或控制者裁决中说明）

| # | 偏离 | 依据 |
|---|------|------|
| 1 | `PendingSeals` 的 flush 改为**无条件**（简报：仅在产出任务时 flush） | 缺陷 A；控制者裁决 2 |
| 2 | `AppendFailed` 写 `null` 标记行（简报：只推进行号不写字节） | 缺陷 C；控制者裁决 1 |
| 3 | `MarkSealed` 改为 `_ = w.closeCurLocked()`（简报：`w.cur = nil`） | 缺陷 B；控制者裁决 3 |
| 4 | `NewWriter` 增加 `Inst` 白名单，用**一条**分支同时涵盖空值与非法值（不再单列「非空 Inst」） | 控制者裁决 4；若保留独立的非空分支，删掉它没有任何用例转红（零覆盖守卫） |
| 5 | 用例改名 `...RowSeqWithoutBytes` → `TestWriterAppendFailedWritesMarkerLineAndAdvancesRow`，并在其读文件前补一次 `w.PendingSeals()`（唯一公开 flush 入口）+ 注释 | 裁决 1（语义变了）+ 缺陷 D（见 §6 RED/GREEN 的说明） |

**未新增哨兵错误**：`NewWriter` 的参数校验沿用本包约定（任务 8 一致性表已登记「空 Root/Inst 属无哨兵参数校验，只断言必须报错」），新增 `ErrInvalidInst` 会扩冻结接口，且启动期致命错误无从分支处理。

## 4. 测试内容与结果

聚焦命令（**全部 17 个用例名逐一列出**；Go 的 `-run` 是子串匹配，`TestNewWriter...` 系列不以 `TestWriter` 开头，必须显式带上）：

```bash
go test ./internal/tracearchive/ ./tests/tracearchive/ -count=1 -v -run 'TestWriterAppendAssignsDenseRowSeq|TestWriterAppendFailedWritesMarkerLineAndAdvancesRow|TestWriterPendingSealsBySize|TestWriterPendingSealsFlushesBuffer|TestWriterPendingSealsByInterval|TestWriterNextSeqResumesFromDirectory|TestNewWriterRejectsEmptyRootAndInst|TestNewWriterRejectsIllegalInst|TestWriterPendingSealsNilBeforeAnyAppend|TestWriterPendingSealsAppliesDefaultThresholds|TestWriterPendingSealsReportsTaskFields|TestWriterAppendTrimsTrailingNewline|TestWriterAppendSwitchesBlockOnDayChange|TestWriterAppendReportsUnusableSpoolDir|TestWriterCloseIsIdempotentWithoutBlock|TestWriterMarkSealedStartsNextPart|TestWriterListPartsFiltersIllegalNames'
```

原始输出（最终提交态）：

```
testing: warning: no tests to run
PASS
ok  	github.com/example/llm-gateway/internal/tracearchive	0.006s [no tests to run]
--- PASS: TestWriterAppendAssignsDenseRowSeq (0.00s)
--- PASS: TestWriterAppendFailedWritesMarkerLineAndAdvancesRow (0.00s)
--- PASS: TestWriterPendingSealsBySize (0.00s)
--- PASS: TestWriterPendingSealsFlushesBuffer (0.02s)
--- PASS: TestWriterPendingSealsByInterval (0.02s)
--- PASS: TestWriterNextSeqResumesFromDirectory (0.00s)
--- PASS: TestNewWriterRejectsEmptyRootAndInst (0.00s)
--- PASS: TestNewWriterRejectsIllegalInst (0.00s)
--- PASS: TestWriterPendingSealsNilBeforeAnyAppend (0.00s)
--- PASS: TestWriterPendingSealsAppliesDefaultThresholds (0.00s)
--- PASS: TestWriterPendingSealsReportsTaskFields (0.00s)
--- PASS: TestWriterAppendTrimsTrailingNewline (0.00s)
--- PASS: TestWriterAppendSwitchesBlockOnDayChange (0.00s)
--- PASS: TestWriterAppendReportsUnusableSpoolDir (0.00s)
--- PASS: TestWriterCloseIsIdempotentWithoutBlock (0.00s)
--- PASS: TestWriterMarkSealedStartsNextPart (0.00s)
--- PASS: TestWriterListPartsFiltersIllegalNames (0.00s)
PASS
ok  	github.com/example/llm-gateway/tests/tracearchive	0.052s
```

`./internal/tracearchive/` 的 `no tests to run` 是预期的：本任务全部用例在 B 层，A 层没有用例匹配该 `-run`。

用例与断言（★ = 简报给定，☆ = 我补的守卫覆盖）：

| 用例 | 断言的守卫/行为 |
|------|-----------------|
| ★ `...AppendAssignsDenseRowSeq` | 行号稠密递增 0..4；`DT`/`Inst`/`Part` 正确 |
| ★ `...AppendFailedWritesMarkerLineAndAdvancesRow` | 失败行占行号（`Row==2`）且落 `null` 标记 ⇒ spool 恰为 `"line0\nnull\nline2\n"` |
| ★ `...PendingSealsBySize` | 大小阈值触发；`Rows` 与 `SpoolPath` 存在 |
| ★ `...PendingSealsFlushesBuffer` | **D3 回归**：返回任务时缓冲必须已落盘（1 MiB 缓冲，10 字节写入） |
| ★ `...PendingSealsByInterval` | 时间阈值触发；未到阈值不触发 |
| ★ `...NextSeqResumesFromDirectory` | 写指针以目录为权威：`max(0,5,7)+1=8`，新块 `Row=0` |
| ★ `TestNewWriterRejectsEmptyRootAndInst` | 空 Root / 空 Inst 必须报错 |
| ★ `...PendingSealsNilBeforeAnyAppend` | 无当前块时不 panic、不产任务 |
| ☆ `TestNewWriterRejectsIllegalInst` | 白名单拒绝 `red`/`../green`/`blue/`/`blue/../green`/`BLUE`/`blue `，并反向断言 `blue`/`green` 仍被接受 |
| ☆ `...PendingSealsAppliesDefaultThresholds` | `BlockBytes=0`/`SealInterval=0` 取默认值（删任一条赋值即转红） |
| ☆ `...PendingSealsReportsTaskFields` | `SealTask` 全字段；`UncompressedBytes` == 磁盘 spool 大小（含每行换行字节） |
| ☆ `...AppendTrimsTrailingNewline` | 末尾 `\n`/`\r\n` 可有可无，落盘字节一致 |
| ☆ `...AppendSwitchesBlockOnDayChange` | 跨天换块：新的一天、`Part=0`、两天的 spool 各一行 |
| ☆ `...AppendReportsUnusableSpoolDir` | spool 目录不可用时 `Append`/`AppendFailed` 必须报错且保留 `*fs.PathError`（`errors.As`） |
| ☆ `...CloseIsIdempotentWithoutBlock` | 无当前块 / 重复 `Close` 返回 nil |
| ☆ `...MarkSealedStartsNextPart` | 封块后换到 `part-000001`、`Row=0`、新 spool 内容正确 |
| ☆ `...ListPartsFiltersIllegalNames` | 无产物目录返回空；忽略 `.tmp`/非 `part-`/位数不符；升序返回 `[1,3]` |

## 5. TDD 证据

### RED（实现前）

命令：同 §4 的聚焦命令 + `-v`（简报原文命令，当时用例名还是 `...RowSeqWithoutBytes`）。

原始输出：

```
# github.com/example/llm-gateway/tests/tracearchive_test [github.com/example/llm-gateway/tests/tracearchive.test]
tests/tracearchive/writer_test.go:20:15: undefined: ta.NewWriter
tests/tracearchive/writer_test.go:20:28: undefined: ta.WriterOptions
tests/tracearchive/writer_test.go:42:15: undefined: ta.NewWriter
tests/tracearchive/writer_test.go:42:28: undefined: ta.WriterOptions
tests/tracearchive/writer_test.go:85:15: undefined: ta.NewWriter
tests/tracearchive/writer_test.go:85:28: undefined: ta.WriterOptions
tests/tracearchive/writer_test.go:115:15: undefined: ta.NewWriter
tests/tracearchive/writer_test.go:115:28: undefined: ta.WriterOptions
tests/tracearchive/writer_test.go:145:15: undefined: ta.NewWriter
tests/tracearchive/writer_test.go:145:28: undefined: ta.WriterOptions
tests/tracearchive/writer_test.go:145:28: too many errors
testing: warning: no tests to run
PASS
ok  	github.com/example/llm-gateway/internal/tracearchive	0.005s [no tests to run]
FAIL	github.com/example/llm-gateway/tests/tracearchive [build failed]
FAIL
```

为何符合预期：`NewWriter`/`WriterOptions` 尚未实现，B 层测试包无法编译（`./internal/tracearchive/` 无此任务的用例，`-run` 过滤后 no tests to run，与简报预测一致）。

### GREEN

见 §4 的原始输出（17/17 PASS）。在交付该输出之前，我用简报**原文**实现跑过一次，发现第 3 节的缺陷 D：

### 缺陷 D（简报内部矛盾，已向控制者报备并获裁决 2 接受）

简报的 `TestWriterAppendFailed...` **直接 `os.ReadFile` 读 spool**，而简报自己的实现是 `bufio`（1 MiB）+ 只在 `PendingSeals` 里 flush；`TestWriterPendingSealsFlushesBuffer` 又要求「不 flush 则文件为空」。两者不可同时满足。实测（简报原文的用例，去掉我补的 flush 调用）：

```
=== RUN   TestWriterAppendFailedAdvancesRowSeqWithoutBytes
    writer_test.go:75: spool 应有 2 行，实际 0
--- FAIL: TestWriterAppendFailedAdvancesRowSeqWithoutBytes (0.00s)
FAIL	github.com/example/llm-gateway/tests/tracearchive	0.005s
```

处理：用例读文件前调用 `w.PendingSeals()`（唯一公开 flush 入口，此刻返回 0 个任务，只承担 flush 职责）并写明理由；控制者裁决 1 之后 spool 期望值由 2 行改为 3 行（`null` 标记行）。

## 6. 变异验证

方法：对 `writer.go` 逐个施加单点变异，跑 `go test ./internal/tracearchive/ ./tests/tracearchive/ -count=1 -v`，记录转红的用例名；每次变异后从备份还原（已校验 `diff` 为空）。基线（未变异）= 全部 PASS。

| 变异 | 结果 | 转红的用例 |
|------|------|-----------|
| M1 删掉 `PendingSeals` 的 `Flush` | 转红 | FlushesBuffer, AppendFailedWritesMarkerLine…, TrimsTrailingNewline, SwitchesBlockOnDayChange, MarkSealedStartsNextPart, PendingSealsReportsTaskFields |
| M2 flush 只在产出封块任务时执行（**简报原文位置**） | 转红 | AppendFailedWritesMarkerLine…, TrimsTrailingNewline, SwitchesBlockOnDayChange, MarkSealedStartsNextPart |
| M3 停用 `Inst` 白名单 | 转红 | RejectsIllegalInst, RejectsEmptyRootAndInst |
| M4 字节计数丢掉换行字节（`+1` → `+0`） | 转红 | PendingSealsReportsTaskFields |
| M5 `trimNewline` 不再去 `\r` | 转红 | AppendTrimsTrailingNewline |
| M6 换块时不再比较 `dt` | 转红 | AppendSwitchesBlockOnDayChange |
| M7 `AppendFailed` 退回「只推进行号，不写字节」 | 转红 | AppendFailedWritesMarkerLine… |
| M8 `AppendFailed` 写了标记行但不推进行号 | 转红 | AppendFailedWritesMarkerLine… |
| M9 停用 `MarkSealed` | 转红 | MarkSealedStartsNextPart |
| M10 删掉 `closeCurLocked` 的 `cur == nil` 守卫 | 转红（panic） | AppendAssignsDenseRowSeq |
| M11 `ListParts` 不再过滤非法名 | 转红 | ListPartsFiltersIllegalNames |
| M12 `ListParts` 目录不存在时改报错 | 转红 | ListPartsFiltersIllegalNames |
| M13 删掉 `PendingSeals` 的 `cur.rows == 0` 守卫 | **仍全绿（无区分）** | ——（见 §7.C） |
| M14 停用 Root 非空校验 | 转红 | RejectsEmptyRootAndInst |
| M15 `SealTask.Rows` 报错一位 | 转红 | PendingSealsBySize, PendingSealsReportsTaskFields |
| M16 标记行改成空行 | 转红 | AppendFailedWritesMarkerLine… |

### 承重约束 1（D3）指定的双侧原始输出

命令：`go test ./tests/tracearchive/ -run 'TestWriterPendingSealsFlushesBuffer|TestWriterAppendFailedWritesMarkerLineAndAdvancesRow' -v -count=1`

基线（未变异）：

```
=== RUN   TestWriterAppendFailedWritesMarkerLineAndAdvancesRow
--- PASS: TestWriterAppendFailedWritesMarkerLineAndAdvancesRow (0.00s)
=== RUN   TestWriterPendingSealsFlushesBuffer
--- PASS: TestWriterPendingSealsFlushesBuffer (0.02s)
PASS
ok  	github.com/example/llm-gateway/tests/tracearchive	0.022s
```

变异 M1（删掉 flush）：

```
=== RUN   TestWriterAppendFailedWritesMarkerLineAndAdvancesRow
    writer_test.go:86: spool 应有 3 行（含失败行的 null 标记），实际 0（内容 ""）
--- FAIL: TestWriterAppendFailedWritesMarkerLineAndAdvancesRow (0.00s)
=== RUN   TestWriterPendingSealsFlushesBuffer
    writer_test.go:149: 封块任务返回时 spool 必须已 flush，实际读到 0 行（内容 ""）
--- FAIL: TestWriterPendingSealsFlushesBuffer (0.02s)
FAIL
FAIL	github.com/example/llm-gateway/tests/tracearchive	0.022s
FAIL
```

### 裁决 1 指定的标记行删除变异（双侧原始输出）

变异 M7（`AppendFailed` 退回只推进行号）：基线见上一节（PASS），变异后：

```
=== RUN   TestWriterAppendFailedWritesMarkerLineAndAdvancesRow
    writer_test.go:86: spool 应有 3 行（含失败行的 null 标记），实际 2（内容 "line0\nline2\n"）
--- FAIL: TestWriterAppendFailedWritesMarkerLineAndAdvancesRow (0.00s)
=== RUN   TestWriterPendingSealsFlushesBuffer
--- PASS: TestWriterPendingSealsFlushesBuffer (0.02s)
FAIL
FAIL	github.com/example/llm-gateway/tests/tracearchive	0.022s
FAIL
```

## 7. 自检发现

**A. 零覆盖的不可达分支（已登记，未删）** —— 全部只能经「公开 API 无法注入的写失败」抵达，删除它们不会让任何用例转红，但保留是有意的防御：

- `PendingSeals` 的 `cur.rows == 0` 守卫（M13 证实无区分）：只可能由「建了块但 `Append` 写失败」产生，用于避免把 0 行 spool 交给 sealer；
- `Append` 的两个 bufio 写错误分支、`PendingSeals` 的 flush 错误分支、`ensureBlockLocked` 的 `f.Stat()` 错误分支；
- `MarkSealed` 的 flush+close 没有「可观察」的公开断言（fd 是否关闭在公开 API 下不可观测）。它是资源安全修复而非拒绝分支；删掉这一行不会有用例转红，已在代码注释中说明。

**B. `ListParts` 没有消费者**（首轮登记）——*已按 §10 裁决 2 整段移除*。当时保留的理由是「简报代码块含它」；控制者裁决 YAGNI 删除，连带其两条分支用例与唯一用到的 `sort` 导入一起删除。另注（记录当时的判断）：`sort.Ints` 在 6 位补零命名下本就冗余（`os.ReadDir` 已按文件名升序返回且与数值序一致），现在它随 `ListParts` 一并消失。

**C. `dt` 未做校验**（首轮登记）——*已按 §10 裁决 1 修复*。首轮只在报告里登记的顾虑：`filepath.Join(root, "dt="+dt, ...)` 会清理路径，`dt="../x"` 会把文件写进 `<root>/x`（与 `Inst` 是同一个洞的另一半）。现已在 `Append` 用 `time.Parse("2006-01-02", dt)` 校验，`AppendFailed` 复用同一入口，详见 §10.1。

**D. 未闭合的分层争议（不改）**：`Paths.PartDir/SpoolDir` 本身不校验 `inst`，越权风险只在 `NewWriter` 白名单处被挡住；任务 4 已 CLOSED，我按裁决未改 `ref.go`，仅在此登记。

**E. 一处注释瑕疵已在提交前修正**：`failedRowMarker` 的文档注释曾残留一行 `AppendFailed` 的标题（amend 修正，见提交 `8cd873e7`）。

## 8. 验证命令汇总（全部在最终提交态执行）

```bash
gofmt -l internal/tracearchive/writer.go tests/tracearchive/writer_test.go   # 无输出
go vet ./internal/tracearchive/ ./tests/tracearchive/                        # OK
go build ./internal/... ./cmd/...                                            # build OK
go test ./internal/tracearchive/ ./tests/tracearchive/ -count=1              # 两个包都 ok
go test ./tests/... -count=1                                                 # 见下
```

`go test ./tests/... -count=1` 有 **3 个预存在失败**，与本次改动无关（已用 `git worktree add /tmp/base-check HEAD` 在未含我改动的 `3709bec8` 上复现同样三处）：

- `tests/gateway/handler`、`tests/gateway/orchestrator`：`[build failed]`，测试替身 `fakeScheduler.UpdateSticky` 与 `orchestrator.Scheduler` 的新签名不符；
- `tests/controlplane/repository`：`table budget_limit_schedules has no column named schedule_operation`（内存 SQLite 与 model 漂移）。

这三处对 `tracearchive` 无任何引用。`tests/tracearchive` 与 `internal/tracearchive` 在整套运行中 ok。

## 9. 变更的文件

- `internal/tracearchive/writer.go`（新建，340 行 @ `52ccc9b2`）
- `tests/tracearchive/writer_test.go`（新建，602 行 @ `52ccc9b2`）

---

# 10. 修复报告（收到评审修正后追加）

**提交：** `52ccc9b2` fix(tracearchive): 校验 dt 路径分量并删除无消费者的 ListParts（不动 `8cd873e7`）
**本节取代**：§4 的用例清单/命令/输出、§6 的变异表（M11/M12 已随 `ListParts` 退役）、§7.B/§7.C（两条自检发现均已按裁决处理）。

## 10.1 修正 1：`dt` 路径分量校验（裁决：要修）

**为什么**：`dt` 与 `inst` 是同一个洞的两半——两者都直接拼进目录名（`<root>/dt=<dt>/inst=<inst>`），而 `filepath.Join` 会做路径清理。首轮只堵了 `inst`，`dt="../x"` 仍会被清成 `<root>/x`，越过 `dt=` 子树；`dt="2026-9-24"` 还会让同一天出现两个目录名，点读按 `Ref.DT` 只会命中其中一个。

**改了什么**（`internal/tracearchive/writer.go`）：

- 新增 `dateLayout = "2006-01-02"` 常量与 `validateDT(dt)` 辅助：`time.Parse(dateLayout, dt)` 一次挡住路径穿越/空串、长度不符、未补零、非法日期。**不设哨兵**（与 `Inst` 同理，已写入注释）。
- `Append` 在加锁前调用 `validateDT`——这是**唯一**的 dt 校验入口；`AppendFailed` 复用 `Append`，两个写入入口共用同一道校验，避免出现「第二处不可被测试区分的重复守卫」。
- 校验先于任何文件系统副作用：非法 dt 连 spool 目录都不会创建。
- 两处校验的注释互相点明（文件头「路径分量校验」常量块 + `NewWriter` 的 Inst 分支 + `validateDT`），便于后人一次看全两个路径分量。
- 新增用例 `TestWriterAppendRejectsIllegalDT`：拒绝 `""`、`"../x"`、`"2026-9-24"`、`"2026-13-01"`、`"2026-02-30"`、`"2026-09-24T00:00:00Z"`、`"2026-09-24/../.."`、`"20260924"`（`Append` 与 `AppendFailed` 两个入口都断言必须报错）；断言**非法输入后归档根目录为空**；**反向断言**合法日期 `"2026-09-24"`/`"2026-09-25"` 仍被接受（并检查 `Ref` 分量），防「一律拒绝」的坏守卫。

**变异验证（裁决要求的双侧原始输出）**

命令：`go test ./tests/tracearchive/ -run 'TestWriterAppendRejectsIllegalDT|TestNewWriterRejectsIllegalInst' -v -count=1`

基线（未变异）：

```
=== RUN   TestNewWriterRejectsIllegalInst
--- PASS: TestNewWriterRejectsIllegalInst (0.00s)
=== RUN   TestWriterAppendRejectsIllegalDT
--- PASS: TestWriterAppendRejectsIllegalDT (0.00s)
PASS
ok  	github.com/example/llm-gateway/tests/tracearchive	0.007s
```

变异 M17（删掉 `Append` 里的 `validateDT` 调用，退回首轮的无校验状态）：

```
=== RUN   TestNewWriterRejectsIllegalInst
--- PASS: TestNewWriterRejectsIllegalInst (0.00s)
=== RUN   TestWriterAppendRejectsIllegalDT
    writer_test.go:566: Append 的 dt="" 应被拒绝
--- FAIL: TestWriterAppendRejectsIllegalDT (0.00s)
FAIL
FAIL	github.com/example/llm-gateway/tests/tracearchive	0.006s
FAIL
```

另加一条强化变异 **M18**（把 `time.Parse` 换成只看长度的非锚定判断 `len(dt) != len(dateLayout)`）：`TestWriterAppendRejectsIllegalDT` 转红——即「形如 10 字符但非法」的输入（`../x` 补位类、`2026-13-01`、`2026-02-30`）确实被锚定解析挡住，而不只是靠长度。

## 10.2 修正 2：删除 `ListParts`（YAGNI）

**为什么**：计划 Task 5 的代码块包含 `ListParts`，但它**没有消费者**——Task 6 的发现逻辑是 `ListPendingSeals` 扫 **spool**，Task 7 从 `Ref` 直接推导路径、不做列举，后续计划 3 也未用到。

**改了什么**：整段删除 `Writer.ListParts` 及其唯一分支用例 `TestWriterListPartsFiltersIllegalNames`，并删除因此不再被使用的 `sort` 导入（测试侧同时删除 `reflect` 导入）。未触碰任何其它导出 API（`PendingSeals` / `SealTask` / `AppendFailed` / `Close` / `MarkSealed` 均保留且有消费者或计划用途）。

**变异表更新**：首轮的 M11（`ListParts` 不再过滤非法名）、M12（`ListParts` 目录不存在时改报错）随该 API 一起退役；新增 M17/M18（见上）。

## 10.3 修正后的最终用例清单与原始输出

命令（17 个用例名逐一列出，`ListParts` 用例已不含，新增 `dt` 用例）：

```bash
go test ./internal/tracearchive/ ./tests/tracearchive/ -count=1 -v -run 'TestWriterAppendAssignsDenseRowSeq|TestWriterAppendFailedWritesMarkerLineAndAdvancesRow|TestWriterPendingSealsBySize|TestWriterPendingSealsFlushesBuffer|TestWriterPendingSealsByInterval|TestWriterNextSeqResumesFromDirectory|TestNewWriterRejectsEmptyRootAndInst|TestNewWriterRejectsIllegalInst|TestWriterPendingSealsNilBeforeAnyAppend|TestWriterPendingSealsAppliesDefaultThresholds|TestWriterPendingSealsReportsTaskFields|TestWriterAppendTrimsTrailingNewline|TestWriterAppendSwitchesBlockOnDayChange|TestWriterAppendReportsUnusableSpoolDir|TestWriterCloseIsIdempotentWithoutBlock|TestWriterMarkSealedStartsNextPart|TestWriterAppendRejectsIllegalDT'
```

原始输出（`52ccc9b2`）：

```
testing: warning: no tests to run
PASS
ok  	github.com/example/llm-gateway/internal/tracearchive	0.006s [no tests to run]
--- PASS: TestWriterAppendAssignsDenseRowSeq (0.00s)
--- PASS: TestWriterAppendFailedWritesMarkerLineAndAdvancesRow (0.00s)
--- PASS: TestWriterPendingSealsBySize (0.00s)
--- PASS: TestWriterPendingSealsFlushesBuffer (0.02s)
--- PASS: TestWriterPendingSealsByInterval (0.02s)
--- PASS: TestWriterNextSeqResumesFromDirectory (0.00s)
--- PASS: TestNewWriterRejectsEmptyRootAndInst (0.00s)
--- PASS: TestNewWriterRejectsIllegalInst (0.00s)
--- PASS: TestWriterPendingSealsNilBeforeAnyAppend (0.00s)
--- PASS: TestWriterPendingSealsAppliesDefaultThresholds (0.00s)
--- PASS: TestWriterPendingSealsReportsTaskFields (0.00s)
--- PASS: TestWriterAppendTrimsTrailingNewline (0.00s)
--- PASS: TestWriterAppendSwitchesBlockOnDayChange (0.00s)
--- PASS: TestWriterAppendReportsUnusableSpoolDir (0.00s)
--- PASS: TestWriterCloseIsIdempotentWithoutBlock (0.00s)
--- PASS: TestWriterMarkSealedStartsNextPart (0.00s)
--- PASS: TestWriterAppendRejectsIllegalDT (0.00s)
PASS
ok  	github.com/example/llm-gateway/tests/tracearchive	0.051s
```

包级命令与静态检查（`52ccc9b2`）：

```
$ go test ./internal/tracearchive/ ./tests/tracearchive/ -count=1
ok  	github.com/example/llm-gateway/internal/tracearchive	3.077s
ok  	github.com/example/llm-gateway/tests/tracearchive	0.050s

$ gofmt -l internal/tracearchive/ tests/tracearchive/    # 无输出
$ go vet ./internal/tracearchive/ ./tests/tracearchive/  # OK
$ go build ./internal/... ./cmd/...                      # build OK
```

## 10.4 修正后的完整变异表（16 项，15 项转红）

方法同上（逐点变异 → `go test ./internal/tracearchive/ ./tests/tracearchive/ -count=1 -v` → 从备份还原并 `diff` 校验）。基线 = 全绿。

| 变异 | 结果 | 转红用例 |
|------|------|---------|
| M17 删掉 `Append` 的 `validateDT` 调用 | 转红 | AppendRejectsIllegalDT |
| M18 `validateDT` 改成只查长度（非锚定） | 转红 | AppendRejectsIllegalDT |
| M1 删掉 `PendingSeals` 的 Flush | 转红 | FlushesBuffer + 5 个依赖 flush 的用例 |
| M2 flush 只在产出封块任务时执行（简报原文位置） | 转红 | AppendFailedWritesMarkerLine…, TrimsTrailingNewline, SwitchesBlockOnDayChange, MarkSealedStartsNextPart |
| M3 停用 `Inst` 白名单 | 转红 | RejectsIllegalInst, RejectsEmptyRootAndInst |
| M4 字节计数丢掉换行字节 | 转红 | PendingSealsReportsTaskFields |
| M5 `trimNewline` 不再去 `\r` | 转红 | AppendTrimsTrailingNewline |
| M6 换块时不再比较 `dt` | 转红 | AppendSwitchesBlockOnDayChange |
| M7 `AppendFailed` 退回「只推进行号，不写字节」 | 转红 | AppendFailedWritesMarkerLine…, AppendRejectsIllegalDT |
| M8 `AppendFailed` 写了标记行但不推进行号 | 转红 | AppendFailedWritesMarkerLine…, AppendRejectsIllegalDT |
| M9 停用 `MarkSealed` | 转红 | MarkSealedStartsNextPart |
| M10 删掉 `closeCurLocked` 的 `nil` 守卫 | 转红（panic） | AppendAssignsDenseRowSeq |
| M13 删掉 `PendingSeals` 的 `rows == 0` 守卫 | **仍全绿（无区分）** | ——（理由见 §7.A） |
| M14 停用 Root 非空校验 | 转红 | RejectsEmptyRootAndInst |
| M15 `SealTask.Rows` 报错一位 | 转红 | PendingSealsBySize, PendingSealsReportsTaskFields |
| M16 标记行改成空行 | 转红 | AppendFailedWritesMarkerLine… |
| ~~M11/M12 `ListParts` 两条分支~~ | 随 API 退役 | —— |

（M7/M8 现在额外让 `AppendRejectsIllegalDT` 转红：这两个变异把 `AppendFailed` 改成不再经过 `Append`，于是 `AppendFailed` 的非法 dt 也被放过——恰好额外证明了「dt 校验只有一处、两个入口共用」这条设计。）

---

# 11. 评审修复轮二报告（行校验 / 写失败路径 / 覆盖补齐）

**提交：**
- `771d7985` fix(tracearchive): 拒绝行内换行/空行，并把写失败的块隔离以杜绝残行封块
- `8359f4bc` docs(tracearchive): 修正写失败注释对任务 6 残行行为的描述（仅注释与用例头部说明，机制不变；见 §11.3.1）
- `49475a78` fix(tracearchive): 修正三处失实注释，并补 `truncateToLastNewline` 的清空分支用例（复审轮六项未关闭项的处置；见 §11.8 的逐条 `文件:行号`）

**本节取代**：§1 里对 `AppendFailed` 的「调用方只能走 AppendFailed」旧说法、§5/§7 的相关自检条目，以及 §10.1 里「两个写入入口共用同一道 dt 校验」的表述（本轮把落盘逻辑抽成内部 `appendLineLocked`，dt 校验仍在公开 `Append` 上）。

## 11.1 本轮处置对照

| 评审意见 | 处置 | 新增/修订用例 | 变异项 |
|---|---|---|---|
| 要修 1：`Append` 对行内换行无守卫 | 新增 `validateLine`：行内裸 `\n`/`\r` + trim 后为空都拒绝；校验只放公开 `Append` | B 层 `AppendRejectsInteriorNewline`、`AppendRejectsEmptyLine` | M19 / M20 / M21 |
| 要修 1 细化：守卫不能放共享路径 | 落盘逻辑抽成 `appendLineLocked`（不校验），`AppendFailed` 复用 | 两个新用例里的「标记行不得被行校验拦住」反向断言 | M30 |
| 要修 1 细化：空行纳入校验 | 已纳入（`len(body) == 0`） | 同上 | M21 |
| 要修 1 细化：`null` 只写文档不禁止 | 写进 `Append` 的前置条件；登记为未成文义务（§11.7） | —— | —— |
| 要修 2：写失败后残行不可封块 | ①回退到行边界 ②改名隔离 ③毒化 ④退路清空（§11.3） | A 层 3 个用例 | M22~M28 |
| 要修 3：标记行字节/行数统计 | 新增 B 层用例（含「全标记行的块也要能封块」） | `MarkerLineCountsRowsAndBytes` | M30 / M31 |
| 要修 3：`MarkSealed` close 零回归覆盖 | A 层用例持有旧句柄断言已关闭 | `MarkSealedClosesPreviousFileHandle` | M27 |
| 要修 3：`MarkSealed` 的 dt/part 匹配零覆盖 | B 层用例：错配时当前块不被清除 | `MarkSealedIgnoresNonMatchingPart` | M29 |
| 补充 1：隔离件 `.jsonl.zst` 的副作用要写明 | 写进 `quarantineSuffix` 注释 + §11.3.7（并**更正**一处预判） | —— | —— |
| 补充 2：反向扫描要有界、无 `\n` 回退清空 | 界写成「本次失败那一行的长度」 | 退路分支用例 | M24 |
| 补充 3：A 层理由按全局约束写明 | 文件头 + 每个用例都写了「为什么必须触达未导出符号」 | —— | —— |

## 11.2 「要修 1」：行校验，以及它为什么不能放在共享路径上

### 11.2.1 规则与位置

`validateLine(body)`（`writer.go`，紧挨 `validateDT`，同属写入前置校验）：

1. `bytes.IndexAny(body, "\r\n") >= 0` ⇒ 报错。这是承重不变量「一次 `Append` == 一条物理行」的唯一无守卫入口：正文自带换行时一次调用产出多条物理行而行号只 +1，此后 `row=N` 会读到第 N+1 行的正文。
2. `len(body) == 0` ⇒ 报错。空行不是合法 JSON（与 `failedRowMarker` 不取空行同一个理由），且首轮实现里 `Append(dt, nil)` 确实会写出一条空行。

**为什么拒绝而不是转义**（落实裁决）：产物是逐字节原样 JSONL；写入侧若转义，读取侧就必须反转义，等于把一个变换强加给读取路径，并让「按行解析 JSONL 的任何工具」读到非原字节。而 JSON 序列化本身不会产生裸换行（`\n` 会写成 `\\n`），所以含裸换行的输入只可能是调用方 bug，快速失败才是对的。**不新增哨兵**。

**为什么校验只在公开 `Append` 上**：`AppendFailed` 复用落盘路径，守卫若放进去就会打死失败标记行。故拆成两层：

- 公开 `Append`：`validateDT` → `trimNewline` → `validateLine` → `appendLineLocked`；
- 内部 `appendLineLocked`（必须持锁）：只做确保块、缓冲写入、`rows++`、`bytes += n+1`，**不校验**，由 `Append` 与 `AppendFailed` 共享——标记行与真实正文因此由构造共享同一套不变量。

**校验先于任何文件系统副作用**：两个新用例都断言「全部输入被拒后归档根目录为空」，与 `AppendRejectsIllegalDT` 同一判据。

### 11.2.2 换行语义的确认结果（裁决要求写清）

确认结果：**末尾换行是分隔符，不是内容**；`trimNewline` 去掉末尾**整段** `\r`/`\n`（任意个），因此在 `trim` 之后校验等价于「只拒绝行内换行」：

- `"line0\n"`、`"line0"` 等价，都接受（首轮 17 个用例大量传带尾换行的行，本轮**没有一个被打红**，这就是该语义的实证）；
- `"line2\n\n"` 等价于 `"line2"`（只产出 1 条物理行，不变量仍成立）——本轮把这条也固定进用例（`AppendRejectsInteriorNewline` 的反向断言二），此前它没有被任何文档或用例承认；
- `"line0\nline1\n"`、`"li\nne"`、`"line0\n\nx"` 一律拒绝（行内仍有换行）。

### 11.2.3 变异证据

见 §11.6 的 M19（删掉 `validateLine` 调用 → 两用例转红）、M20（只删行内换行分支 → 只有 `AppendRejectsInteriorNewline` 转红）、M21（只删空行分支 → 只有 `AppendRejectsEmptyLine` 转红）。三条分支各自可区分。

**关于「把同样的校验挪进共享路径」**：如实记录——只把同一份校验复制进 `appendLineLocked` 会**仍然全绿**，因为标记行 `"null"` 既不空也无换行，今天恰好能通过这两条规则。所以「拆两层」在当前规则集下的价值是**结构性**的：它是「将来把守卫加严（例如禁止字面量 `null`、或要求 JSON 合法性）时不会连标记行一起打死」的前提。两个新用例里的「标记行不得被行校验拦住」反向断言把今天这条生存性固定住了；M30（`AppendFailed` 不再落盘）证明该断言确实有区分力。

## 11.3 「要修 2」：写失败路径的完整机制（①~④）

### 11.3.1 不变量与三条发现路径

spool 的每一行就是一条物理行，`row` 由「第 row 行」定位（产物 trailer 与 spool 兜底读取都按行取），因此「**物理行数 == 已分配行号数**」是承重不变量。一次写失败可能在盘上留下**没有换行收尾的残行**。

**残行的后果（已按两种实现对过账，此处为落地版事实）**：任务 6 的简报把未终止尾行「计一行」，但**落地版更严**——`sealer.go` 的 `scanSpoolOffsets` 现在直接拒绝（`ErrSealFailed`，注释写明「残行守卫」），本轮据此修正了代码注释与用例头部说明（提交 `8359f4bc`）。于是：

1. **封块侧：响亮但持久**。`scanSpoolOffsets` 遇未终止尾行 ⇒ `ErrSealFailed`，该块**永远封不掉**，告警与重试一直持续；而首轮写入器（写失败只「不推进行号」）会**继续往同一个 spool 追加**，于是这个永不封块的 spool 还会**无限增长**。
2. **读取侧：静默**。任务 7 的 spool 兜底读取（简报里冻结的 `readFromSpool`，`task-7-brief.md:530-547`）是「顺序取第 row 行」，`if i == row && len(line) > 0 { return trimNewline(line), nil }`，**不校验行尾终止符**——残行照样被当成一行返回。所以写入器一旦在残行之后继续追加，其后每一行都会整体错位 ⇒ `row=N` 读到第 N+1 行的正文。**这才是本机制真正要防的那条静默路径**（`Rows==0` 的目录扫描路径因为第 1 条已经封不成了，不再是静默路径）。

⇒ 硬要求：**残行永远不可被封块，也不可再有后续行追加进同一个文件**。机制如下（`poisonCurLocked`）。

### 11.3.2 ① 回退到最后一个完整行边界

`truncateToLastNewline(path)`：用**只读句柄**（spool 的写句柄是 `O_WRONLY`，读不了）从文件尾按 64 KiB 反向分块扫描，找到最后一个 `'\n'` 后 `os.Truncate` 到它之后。

**界**：残行长度 ≤ **本次失败那一行的长度**。正文内不含裸换行（`validateLine`），所以文件里最后一个 `'\n'` 之后只可能是一条写了一半的行，反向扫描最多覆盖「该行长度 + 一个块」，不会无限倒退。**整个文件一个 `'\n'` 都没有**（首次追加即失败）时回退为 `Truncate(0)`——与裁决的 ④ 一致（`truncateToLastNewline` 末尾的 `return os.Truncate(path, 0)`）。

### 11.3.3 ② 改名隔离（`part-NNNNNN.jsonl.zst`）

隔离路径必须同时满足两条，且都由**既有冻结规则**给出（不新增契约）：

- `ListPendingSeals` 只收 `.jsonl` 结尾的名字（**落地代码** `sealer.go:388-392`，注释明写「.tmp、产物名（.jsonl.zst）、非 part- 前缀一律忽略」）⇒ sealer 的进程外目录扫描永远看不到它，读取侧那条静默路径的源头（继续追加入同一个 spool）也就无从发生；
- `parsePartSeq` 接受 `.jsonl.zst`（`ref.go:76`）⇒ 写指针仍把序号 N 算作已用，**N 不被复用**。复用会让 PG 里旧的 `(part=N, row=r)` 静默解析到另一个 part 的正文——最坏的一类错，必须由命名规则而不是流程纪律来挡。

**这里抓到一个真实 bug**：首版实现写的是 `spoolPath + quarantineSuffix`，而 `spoolPath` 已以 `.jsonl` 结尾 ⇒ 得到 `part-000000.jsonl.jsonl.zst`，`parsePartSeq` **拒绝**它 ⇒ 序号 N 被复用（正是上面那条最坏情形）。新用例的名字断言（`parsePartSeq(name)` 必须接受且序号 == N）把它抓住了；现改为 `quarantinePath()` 用 `partName(part, quarantineSuffix)` 生成，生产代码与测试共用同一函数，避免再次分叉。M26 就是这个 bug 的变异复现（两个用例转红）。

### 11.3.4 ③ 毒化

`cur.poisoned = true`，四个可观察后果：

- `Append`/`AppendFailed`：经 `ensureBlockLocked` 检测到毒化 ⇒ 丢弃该块（`dropCurLocked`，**不 flush**）并**自动换到 N+1**，调用方无需重置任何东西；
- `PendingSeals`：跳过毒化块（返回 `nil, nil`）——不 flush（错误粘性且文件已隔离）、不产出任务；
- `Close`：只释放 fd 并返回 `nil`（那次写失败已由 `Append` 上报，不重复）；
- 行号：失败的这一行不推进行号、不返回 Ref ⇒ 不存在「行号已推进而字节没写」的窗口。

### 11.3.5 ④ 退路：改名失败则清空

改名失败（例如目标名被占）时 `os.Truncate(spoolPath, 0)`：0 字节 spool 里没有任何残行，只会被 sealer 封成 0 行产物，该 part 的读请求一律行号越界 ⇒ 响亮报错而非错正文。清空也失败时，把失败原因如实拼进返回错误（`隔离改名失败: …；清空失败: …`），并仍然毒化该块。

### 11.3.6 调用方契约（已写进导出方法注释）

- `Append` 写失败：本行不写入、不推进行号，返回错误（`%w` 保留原始 errno）；调用方把这一行记 `body_missing=true`（方案 C 的降级约定），然后**照常继续**——下一次 `Append`/`AppendFailed` 自动换 part。
- 毒化前已成功写入的行：其 Ref 仍指向 part N，而 part N 已不可被正常路径读取（见 11.3.7）；这些行的正文**没有被错位**，只是读不到，属「需人工介入」。
- `Close`：毒化块只释放 fd。

### 11.3.7 【更正评审的一处预判】隔离件的读取结果不是 `ErrFooterCorrupt`，而是 `ErrNotArchived`

裁决补充 1 让我把「点读会解析到 `part-N.jsonl.zst` 并发现它是原始 JSONL 而非 zstd 帧 ⇒ 返回 `ErrFooterCorrupt`」写进注释与报告。核对了冻结的任务 7 简报后，这个链路**不成立**，实际行为更简单也更不误导：

任务 7 的 `GetBody`（`task-7-brief.md:404-425`）查找顺序是「产物 → spool」，且两者都是**精确路径**：

```go
partPath := r.paths.PartFile(ref)   // <root>/dt=D/inst=I/part-N.jsonl.zst（产物目录）
if _, err := os.Stat(partPath); err == nil { return r.readFromPart(...) }
spoolPath := r.paths.SpoolFile(ref) // <root>/spool/dt=D/inst=I/part-N.jsonl（没有 .zst）
if _, err := os.Stat(spoolPath); err == nil { return readFromSpool(...) }
return nil, fmt.Errorf("%w: %s", ErrNotArchived, ref)
```

而隔离件在 **spool 目录**下、名字是 `part-N.jsonl.zst`：既不是 `PartFile`（产物目录 + 同名），也不是 `SpoolFile`（spool 目录 + `.jsonl`）。所以不会有任何读取路径去解析它 ⇒ 读 part N 得到 **`ErrNotArchived`**（"产物与暂存均不存在"，500 类，需人工介入），**不会**出现 `ErrFooterCorrupt`。`ErrFooterCorrupt` 只会在**产物目录**里放一个非 zstd 文件时出现，本机制不产生这种文件。

结论：裁决要求的性质（响亮、绝不静默错正文）成立；但**错误语义的误导性比预期更小**，不需要额外说明「产物损坏」的歧义。

> **【更正在 `8359f4bc` 里没有落到代码，本轮补上】** 上一轮我在本报告的这段里写了「注释已按实际行为改写」，但 `8359f4bc` 只改了 `writer.go` 的「写失败契约」那一段（`@@ -125,12 +125,17 @@`），**没有碰 `quarantineSuffix` 这段注释**；直到 `49475a78` 才改。复审逐行核对源码发现了这一点，是有效的纠错：**报告里的每一条「已改」都必须能在代码里指出行号**，此后本报告改成给出 `文件:行号`（见 §11.8）。
>
> 现在的实际状态：`writer.go:499-508`（`quarantineSuffix` 的【已知副作用】段）已按 `ErrNotArchived` 改写；`writer.go:509-514`（运维判据段）保持原样，那句是对的。

运维判据仍然成立并已写进注释：

> spool 目录下若出现「内容是明文 JSONL 的 part-*.jsonl.zst」，即表示该块因写入失败被写入器隔离，其中的正文已不能通过正常路径读取；释放空间后把它改回 part-NNNNNN.jsonl，sealer 会把它当作 Rows==0 的 spool 正常封块，因文件已回退到行边界，产物行数 = 真实完整行数（尾部丢失的行读出来是行号越界，仍是响亮报错）。

读取侧**没有**为此新增分支或哨兵，与裁决「保持读取路径简单」一致。

### 11.3.8 如何注入写失败（以及为什么只能用 A 层）

注入手法：把当前块的 `cur.w` 换成一个容量 8 字节的 `bufio.Writer`，其底层是 `shortWriteFile`——一个写到 `budget` 字节后返回错误的 `io.Writer`，且**短写路径先把 `budget` 字节真的写进 fd 再报错**（真实 ENOSPC 的形态），并记录 `accepted` 与 `sizeAtFail` 供断言。第一次 `Append`（6 字节）进缓冲不触发 flush，第二次触发 flush ⇒ 短写 ⇒ 残行。用例先断言 `accepted == 3` 且 `sizeAtFail == 9`，确保「残行真的产生过」，避免空转。

为什么不用公开 API 制造：`chmod` 不改**已打开** fd 的写权限；把 spool 路径建成目录只会让 `open` 失败（那是「建块失败」，不产生残行）；`/dev/full` 在 macOS 上不存在；填充文件系统不可行。故只能替换未导出的 `cur.w`。这正是 A 层用例文件头与 `failAuthorInjectedWrite` 注释里写明的理由。

### 11.3.9 各分支的变异证据（详见 §11.6）

| 分支 | 变异 | 转红用例 |
|---|---|---|
| ① 回退残行 | M22 删掉 `truncateToLastNewline` 调用 | `WriteFailureLeavesNoResidualLine`（隔离件内容 `"line0\nlin"`） |
| ② 改名隔离 | M23 退化成原地改名 | `WriteFailureLeavesNoResidualLine`、`QuarantineRenameFailureEmptiesSpool` |
| ② 名字占序号 | M26 退回首轮的真 bug（直接拼接） | `PoisonedBlockCannotBeSealed`、`WriteFailureLeavesNoResidualLine` |
| ③ 毒化不可写 | M25 停用 `ensureBlockLocked` 的毒化判断 | `QuarantineRenameFailureEmptiesSpool` |
| ③ 毒化不可封 | M28 `PendingSeals` 不再跳过毒化块 | `PoisonedBlockCannotBeSealed`、`QuarantineRenameFailureEmptiesSpool` |
| ④ 退路清空 | M24 删掉 `Truncate(0)` 退路 | `QuarantineRenameFailureEmptiesSpool`（spool 仍是 6 字节） |

## 11.4 「要修 3」：补齐的覆盖

1. **标记行的字节/行数统计**（B 层 `MarkerLineCountsRowsAndBytes`）：4 条 `AppendFailed` 的块必须能被封块，`Rows == 4`、`UncompressedBytes == 4×5 == 20`；随后接一条真实正文，断言 `Rows == 5`、字节数如实累加、文件内容为 `"null\n"×4 + "line0\n"`（行号 0..4 连续）。
2. **`MarkSealed` 的 close 回归**（A 层 `MarkSealedClosesPreviousFileHandle`）：先持有 `cur.f`，`MarkSealed` 后断言该句柄已关闭（`errors.Is(err, os.ErrClosed)`）。把修复退回 `w.cur = nil` 时此用例是唯一转红者（M27）——此前没有任何回归防线。
3. **`MarkSealed` 的 dt/part 匹配条件**（B 层 `MarkSealedIgnoresNonMatchingPart`）：错配 part 与错配 dt 各调一次后，第三条 `Append` 仍落在 `part-000000#row=2`，spool 内容 `"a\nb\nc\n"` 完整。

## 11.5 本轮最终用例清单与原始输出

基线（`771d7985`）：

```
$ go test ./internal/tracearchive/ ./tests/tracearchive/ -count=1
ok  	github.com/example/llm-gateway/internal/tracearchive	2.879s
ok  	github.com/example/llm-gateway/tests/tracearchive	0.386s

$ gofmt -l internal/tracearchive/ tests/tracearchive/    # 无输出
$ go vet ./internal/tracearchive/ ./tests/tracearchive/  # OK
$ go build ./internal/... ./cmd/...                      # build OK
```

聚焦命令（B 层 21 个用例；`-run` 为子串匹配，用例名以 `|` 连接，`grep -c '^--- PASS'` 计通过数）：

```
$ go test ./tests/tracearchive/ -count=1 -v -run '<21 个用例名>'   # 21
$ go test ./internal/tracearchive/ -count=1 -v \
    -run 'TestWriterWriteFailureLeavesNoResidualLine|TestWriterPoisonedBlockCannotBeSealed|TestWriterQuarantineRenameFailureEmptiesSpool|TestWriterMarkSealedClosesPreviousFileHandle'
--- PASS: TestWriterWriteFailureLeavesNoResidualLine (0.00s)
--- PASS: TestWriterPoisonedBlockCannotBeSealed (0.00s)
--- PASS: TestWriterQuarantineRenameFailureEmptiesSpool (0.00s)
--- PASS: TestWriterMarkSealedClosesPreviousFileHandle (0.00s)
ok  	github.com/example/llm-gateway/internal/tracearchive	0.009s
```

B 层 21 个用例 = §10.3 的 17 个 + 追加的 4 个（`TestWriterAppendRejectsInteriorNewline`、`TestWriterAppendRejectsEmptyLine`、`TestWriterMarkerLineCountsRowsAndBytes`、`TestWriterMarkSealedIgnoresNonMatchingPart`）；A 层新增 `internal/tracearchive/writer_internal_test.go`，含 4 个写入器用例（该文件与既有 A 层文件同包，`package tracearchive`）。

全量 `go test ./tests/... -count=1`：仍有**与本任务无关的既有编译失败**（`tests/contract`、`tests/controlplane/handler`、`tests/gateway/handler`——都是别的子系统的测试 fake 与接口漂移，例如 `UpdateSticky` 签名、`QueryScopedTierUsage` 缺失）。已在 `HEAD`（`76042bcd`，**不含**我的改动）的临时 worktree 上复现同样报错，确认是既有失败、非本轮引入，按约定未修。

## 11.6 本轮变异表（14 项，14 项转红）

方法：逐项改 `writer.go` → `go test ./internal/tracearchive/ ./tests/tracearchive/ -count=1 -v`（**全量两包**）→ 从备份还原并 `diff` 校验（每项都校验还原成功）。基线全绿。下表是**全量跑**下的判定与转红用例；每项的**完整双侧原始输出**（聚焦命令的完整 `go test` 输出块、禁止摘录）见 §11.6.1。

> 为什么下面这段是「摘要」而 §11.6.1 是证据：上一轮的块是从全量输出里摘出的断言行，复审指出这不算原始输出。§11.6.1 是 `mutate5.py` 直接落盘的完整输出（含 `=== RUN` / `--- PASS|FAIL` / `ok|FAIL` 与命令行），不再手工摘录。

```
基线: ok internal/tracearchive 2.879s / ok tests/tracearchive 0.386s

M19 删掉 Append 里的 validateLine 调用: 转红
    writer_test.go:631: 含行内换行的正文应被拒绝，实际被接受: "li\nne"
    --- FAIL: TestWriterAppendRejectsInteriorNewline (0.00s)
    writer_test.go:709: 空行应被拒绝，实际被接受: ""
    --- FAIL: TestWriterAppendRejectsEmptyLine (0.00s)
M20 删掉 validateLine 的行内换行分支: 转红
    writer_test.go:631: 含行内换行的正文应被拒绝，实际被接受: "li\nne"
    --- FAIL: TestWriterAppendRejectsInteriorNewline (0.00s)
M21 删掉 validateLine 的空行分支: 转红
    writer_test.go:709: 空行应被拒绝，实际被接受: ""
    --- FAIL: TestWriterAppendRejectsEmptyLine (0.00s)
M22 删掉 poisonCurLocked 的 truncateToLastNewline 调用: 转红
    writer_internal_test.go:155: 隔离件应只保留完整行 "line0\n"，实际 "line0\nlin"（残行没被抹掉）
    --- FAIL: TestWriterWriteFailureLeavesNoResidualLine (0.00s)
M23 隔离改名退化成原地改名（等于不隔离）: 转红
    writer_internal_test.go:152: 隔离件应存在（…/part-000000.jsonl.zst）: no such file or directory
    --- FAIL: TestWriterWriteFailureLeavesNoResidualLine (0.00s)
    writer_internal_test.go:288: 改名失败时必须清空 spool（否则残行会被 sealer 烤进产物），实际 6 字节
    --- FAIL: TestWriterQuarantineRenameFailureEmptiesSpool (0.00s)
M24 删掉隔离失败时的 Truncate(0) 退路: 转红
    writer_internal_test.go:288: 改名失败时必须清空 spool（…），实际 6 字节
    --- FAIL: TestWriterQuarantineRenameFailureEmptiesSpool (0.00s)
M25 停用 ensureBlockLocked 的毒化判断（毒化块被复用）: 转红
    writer_internal_test.go:301: tracearchive: 追加正文失败（…part-000000#row=2）: 注入的写失败…；隔离改名失败…
    --- FAIL: TestWriterQuarantineRenameFailureEmptiesSpool (0.00s)
M26 隔离名直接拼接（退回首轮那个真 bug：.jsonl.jsonl.zst）: 转红
    writer_internal_test.go:152: 隔离件应存在（…/part-000000.jsonl.zst）: no such file or directory
    --- FAIL: TestWriterWriteFailureLeavesNoResidualLine (0.00s)
    writer_internal_test.go:229: 毒化后应换到 part-000001#row=0，实际 2026-09-24/blue/part-000000#row=0
    --- FAIL: TestWriterPoisonedBlockCannotBeSealed (0.00s)
M27 MarkSealed 只置 nil、不关闭旧句柄: 转红
    writer_internal_test.go:334: MarkSealed 应关闭被替换的旧句柄（否则每块泄漏一个 fd），实际 err=<nil>
    --- FAIL: TestWriterMarkSealedClosesPreviousFileHandle (0.00s)
M28 PendingSeals 不再跳过毒化块: 转红
    writer_internal_test.go:200: 毒化块的 PendingSeals 不该报错（它只是被跳过）: … flush 写缓冲失败 …
    --- FAIL: TestWriterPoisonedBlockCannotBeSealed (0.00s)
M29 MarkSealed 不再比较 dt/part（无条件清除）: 转红
    writer_test.go:840: 错配的 MarkSealed 不得清除当前块（want part-000000#row=2），实际 2026-09-24/blue/part-000001#row=0
    --- FAIL: TestWriterMarkSealedIgnoresNonMatchingPart (0.00s)
M30 AppendFailed 不再落盘（直接返回 nil）: 转红
    writer_test.go:72: 落盘失败的行仍应占用行号: want row 2, got 1
    --- FAIL: TestWriterAppendFailedWritesMarkerLineAndAdvancesRow (0.00s)
    writer_test.go:466: spool 目录不可用时 AppendFailed 必须报错（不能只推进行号）
    --- FAIL: TestWriterAppendReportsUnusableSpoolDir (0.00s)
    writer_test.go:575: AppendFailed 的 dt="" 应被拒绝
    --- FAIL: TestWriterAppendRejectsIllegalDT (0.00s)
    writer_test.go:765: 全标记行的块也必须能被封块，实际报出 0 个任务
    --- FAIL: TestWriterMarkerLineCountsRowsAndBytes (0.00s)   ← 该项单跑复核（批量打印器截断）
M31 字节计数漏掉换行字节（承重约束 2）: 转红
    writer_test.go:348: UncompressedBytes 应为 spool 字节数 44（含换行），实际 40
    --- FAIL: TestWriterPendingSealsReportsTaskFields (0.00s)
    writer_test.go:772: 封块任务字节数应含标记行（want 20），实际 16
    --- FAIL: TestWriterMarkerLineCountsRowsAndBytes (0.00s)

合计 13 项，转红 13 项

M32 删掉 truncateToLastNewline 的 Truncate(0) 退路（复审第 5 项新增分支）: 转红   ← 聚焦跑：该分支只被新用例覆盖
    writer_internal_test.go:313: 无行边界可用时必须清空文件（否则残行会被当成第 0 行返回），实际 2 字节: "li"
    --- FAIL: TestWriterTruncatesWholeFileWhenNoLineBoundaryExists (0.00s)

本轮合计 14 项，转红 14 项
```

## 11.6.1 完整双侧原始输出（每项的完整 `go test` 输出块）

生成方式：`python3 /tmp/mutate5.py`——对每一项，先用**未变异的代码**跑一次聚焦命令（基线），再变异跑同一条命令，逐项还原并 `diff -q` 校验；两侧输出**原样落盘**（含命令行、`=== RUN`、`--- PASS|FAIL`、`ok|FAIL`），本文件由脚本直接拼接，没有手工摘录。聚焦命令的 `-run` 只选取「该项应当打红的用例」，因此块很短；全量两包的判定与转红集合见 §11.6 上表。

### M19 删掉 Append 里的 validateLine 调用

命令（两侧完全相同）：`go test ./tests/tracearchive/ -count=1 -v -run 'TestWriterAppendRejectsInteriorNewline|TestWriterAppendRejectsEmptyLine'`

基线（未变异）：

```
=== RUN   TestWriterAppendRejectsInteriorNewline
--- PASS: TestWriterAppendRejectsInteriorNewline (0.00s)
=== RUN   TestWriterAppendRejectsEmptyLine
--- PASS: TestWriterAppendRejectsEmptyLine (0.00s)
PASS
ok  	github.com/example/llm-gateway/tests/tracearchive	0.007s
```

变异后（转红）：

```
=== RUN   TestWriterAppendRejectsInteriorNewline
    writer_test.go:631: 含行内换行的正文应被拒绝，实际被接受: "li\nne"
--- FAIL: TestWriterAppendRejectsInteriorNewline (0.00s)
=== RUN   TestWriterAppendRejectsEmptyLine
    writer_test.go:709: 空行应被拒绝，实际被接受: ""
--- FAIL: TestWriterAppendRejectsEmptyLine (0.00s)
FAIL
FAIL	github.com/example/llm-gateway/tests/tracearchive	0.006s
FAIL
```

### M20 删掉 validateLine 的行内换行分支

命令（两侧完全相同）：`go test ./tests/tracearchive/ -count=1 -v -run 'TestWriterAppendRejectsInteriorNewline|TestWriterAppendRejectsEmptyLine'`

基线（未变异）：

```
=== RUN   TestWriterAppendRejectsInteriorNewline
--- PASS: TestWriterAppendRejectsInteriorNewline (0.00s)
=== RUN   TestWriterAppendRejectsEmptyLine
--- PASS: TestWriterAppendRejectsEmptyLine (0.00s)
PASS
ok  	github.com/example/llm-gateway/tests/tracearchive	0.006s
```

变异后（转红）：

```
=== RUN   TestWriterAppendRejectsInteriorNewline
    writer_test.go:631: 含行内换行的正文应被拒绝，实际被接受: "li\nne"
--- FAIL: TestWriterAppendRejectsInteriorNewline (0.00s)
=== RUN   TestWriterAppendRejectsEmptyLine
--- PASS: TestWriterAppendRejectsEmptyLine (0.00s)
FAIL
FAIL	github.com/example/llm-gateway/tests/tracearchive	0.007s
FAIL
```

### M21 删掉 validateLine 的空行分支

命令（两侧完全相同）：`go test ./tests/tracearchive/ -count=1 -v -run 'TestWriterAppendRejectsInteriorNewline|TestWriterAppendRejectsEmptyLine'`

基线（未变异）：

```
=== RUN   TestWriterAppendRejectsInteriorNewline
--- PASS: TestWriterAppendRejectsInteriorNewline (0.00s)
=== RUN   TestWriterAppendRejectsEmptyLine
--- PASS: TestWriterAppendRejectsEmptyLine (0.00s)
PASS
ok  	github.com/example/llm-gateway/tests/tracearchive	0.006s
```

变异后（转红）：

```
=== RUN   TestWriterAppendRejectsInteriorNewline
--- PASS: TestWriterAppendRejectsInteriorNewline (0.00s)
=== RUN   TestWriterAppendRejectsEmptyLine
    writer_test.go:709: 空行应被拒绝，实际被接受: ""
--- FAIL: TestWriterAppendRejectsEmptyLine (0.00s)
FAIL
FAIL	github.com/example/llm-gateway/tests/tracearchive	0.006s
FAIL
```

### M22 删掉 poisonCurLocked 的 truncateToLastNewline 调用

命令（两侧完全相同）：`go test ./internal/tracearchive/ -count=1 -v -run 'TestWriterWriteFailureLeavesNoResidualLine'`

基线（未变异）：

```
=== RUN   TestWriterWriteFailureLeavesNoResidualLine
--- PASS: TestWriterWriteFailureLeavesNoResidualLine (0.00s)
PASS
ok  	github.com/example/llm-gateway/internal/tracearchive	0.006s
```

变异后（转红）：

```
=== RUN   TestWriterWriteFailureLeavesNoResidualLine
    writer_internal_test.go:156: 隔离件应只保留完整行 "line0\n"，实际 "line0\nlin"（残行没被抹掉）
--- FAIL: TestWriterWriteFailureLeavesNoResidualLine (0.00s)
FAIL
FAIL	github.com/example/llm-gateway/internal/tracearchive	0.006s
FAIL
```

### M23 隔离改名退化成原地改名（等于不隔离）

命令（两侧完全相同）：`go test ./internal/tracearchive/ -count=1 -v -run 'TestWriterWriteFailureLeavesNoResidualLine|TestWriterQuarantineRenameFailureEmptiesSpool'`

基线（未变异）：

```
=== RUN   TestWriterWriteFailureLeavesNoResidualLine
--- PASS: TestWriterWriteFailureLeavesNoResidualLine (0.00s)
=== RUN   TestWriterQuarantineRenameFailureEmptiesSpool
--- PASS: TestWriterQuarantineRenameFailureEmptiesSpool (0.00s)
PASS
ok  	github.com/example/llm-gateway/internal/tracearchive	0.007s
```

变异后（转红）：

```
=== RUN   TestWriterWriteFailureLeavesNoResidualLine
    writer_internal_test.go:153: 隔离件应存在（/var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestWriterWriteFailureLeavesNoResidualLine4268729233/001/spool/dt=2026-09-24/inst=blue/part-000000.jsonl.zst）: open /var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestWriterWriteFailureLeavesNoResidualLine4268729233/001/spool/dt=2026-09-24/inst=blue/part-000000.jsonl.zst: no such file or directory
--- FAIL: TestWriterWriteFailureLeavesNoResidualLine (0.00s)
=== RUN   TestWriterQuarantineRenameFailureEmptiesSpool
    writer_internal_test.go:375: 改名失败时必须清空 spool（否则残行会留在 sealer 的发现范围里），实际 6 字节
--- FAIL: TestWriterQuarantineRenameFailureEmptiesSpool (0.00s)
FAIL
FAIL	github.com/example/llm-gateway/internal/tracearchive	0.007s
FAIL
```

### M24 删掉隔离失败时的 Truncate(0) 退路

命令（两侧完全相同）：`go test ./internal/tracearchive/ -count=1 -v -run 'TestWriterQuarantineRenameFailureEmptiesSpool'`

基线（未变异）：

```
=== RUN   TestWriterQuarantineRenameFailureEmptiesSpool
--- PASS: TestWriterQuarantineRenameFailureEmptiesSpool (0.00s)
PASS
ok  	github.com/example/llm-gateway/internal/tracearchive	0.006s
```

变异后（转红）：

```
=== RUN   TestWriterQuarantineRenameFailureEmptiesSpool
    writer_internal_test.go:375: 改名失败时必须清空 spool（否则残行会留在 sealer 的发现范围里），实际 6 字节
--- FAIL: TestWriterQuarantineRenameFailureEmptiesSpool (0.00s)
FAIL
FAIL	github.com/example/llm-gateway/internal/tracearchive	0.007s
FAIL
```

### M25 停用 ensureBlockLocked 的毒化判断（毒化块被复用）

命令（两侧完全相同）：`go test ./internal/tracearchive/ -count=1 -v -run 'TestWriterQuarantineRenameFailureEmptiesSpool'`

基线（未变异）：

```
=== RUN   TestWriterQuarantineRenameFailureEmptiesSpool
--- PASS: TestWriterQuarantineRenameFailureEmptiesSpool (0.00s)
PASS
ok  	github.com/example/llm-gateway/internal/tracearchive	0.006s
```

变异后（转红）：

```
=== RUN   TestWriterQuarantineRenameFailureEmptiesSpool
    writer_internal_test.go:388: tracearchive: 追加正文失败（2026-09-24/blue/part-000000#row=2）: 注入的写失败：no space left on device；该块已毒化并停止写入（隔离件 /var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestWriterQuarantineRenameFailureEmptiesSpool2485811457/001/spool/dt=2026-09-24/inst=blue/part-000000.jsonl.zst），后续 Append/AppendFailed 会自动换到新 part，本行请记 body_missing=true；隔离改名失败: rename /var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestWriterQuarantineRenameFailureEmptiesSpool2485811457/001/spool/dt=2026-09-24/inst=blue/part-000000.jsonl /var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestWriterQuarantineRenameFailureEmptiesSpool2485811457/001/spool/dt=2026-09-24/inst=blue/part-000000.jsonl.zst: file exists
--- FAIL: TestWriterQuarantineRenameFailureEmptiesSpool (0.00s)
FAIL
FAIL	github.com/example/llm-gateway/internal/tracearchive	0.006s
FAIL
```

### M26 隔离名直接拼接（回退到 .jsonl.jsonl.zst 那个真 bug）

命令（两侧完全相同）：`go test ./internal/tracearchive/ -count=1 -v -run 'TestWriterWriteFailureLeavesNoResidualLine|TestWriterPoisonedBlockCannotBeSealed'`

基线（未变异）：

```
=== RUN   TestWriterWriteFailureLeavesNoResidualLine
--- PASS: TestWriterWriteFailureLeavesNoResidualLine (0.00s)
=== RUN   TestWriterPoisonedBlockCannotBeSealed
--- PASS: TestWriterPoisonedBlockCannotBeSealed (0.00s)
PASS
ok  	github.com/example/llm-gateway/internal/tracearchive	0.007s
```

变异后（转红）：

```
=== RUN   TestWriterWriteFailureLeavesNoResidualLine
    writer_internal_test.go:153: 隔离件应存在（/var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestWriterWriteFailureLeavesNoResidualLine2578257523/001/spool/dt=2026-09-24/inst=blue/part-000000.jsonl.zst）: open /var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestWriterWriteFailureLeavesNoResidualLine2578257523/001/spool/dt=2026-09-24/inst=blue/part-000000.jsonl.zst: no such file or directory
--- FAIL: TestWriterWriteFailureLeavesNoResidualLine (0.00s)
=== RUN   TestWriterPoisonedBlockCannotBeSealed
    writer_internal_test.go:230: 毒化后应换到 part-000001#row=0，实际 2026-09-24/blue/part-000000#row=0
--- FAIL: TestWriterPoisonedBlockCannotBeSealed (0.00s)
FAIL
FAIL	github.com/example/llm-gateway/internal/tracearchive	0.007s
FAIL
```

### M27 MarkSealed 只置 nil、不关闭旧句柄

命令（两侧完全相同）：`go test ./internal/tracearchive/ -count=1 -v -run 'TestWriterMarkSealedClosesPreviousFileHandle'`

基线（未变异）：

```
=== RUN   TestWriterMarkSealedClosesPreviousFileHandle
--- PASS: TestWriterMarkSealedClosesPreviousFileHandle (0.00s)
PASS
ok  	github.com/example/llm-gateway/internal/tracearchive	0.007s
```

变异后（转红）：

```
=== RUN   TestWriterMarkSealedClosesPreviousFileHandle
    writer_internal_test.go:421: MarkSealed 应关闭被替换的旧句柄（否则每块泄漏一个 fd），实际 err=<nil>
--- FAIL: TestWriterMarkSealedClosesPreviousFileHandle (0.00s)
FAIL
FAIL	github.com/example/llm-gateway/internal/tracearchive	0.005s
FAIL
```

### M28 PendingSeals 不再跳过毒化块

命令（两侧完全相同）：`go test ./internal/tracearchive/ -count=1 -v -run 'TestWriterPoisonedBlockCannotBeSealed|TestWriterQuarantineRenameFailureEmptiesSpool'`

基线（未变异）：

```
=== RUN   TestWriterPoisonedBlockCannotBeSealed
--- PASS: TestWriterPoisonedBlockCannotBeSealed (0.00s)
=== RUN   TestWriterQuarantineRenameFailureEmptiesSpool
--- PASS: TestWriterQuarantineRenameFailureEmptiesSpool (0.00s)
PASS
ok  	github.com/example/llm-gateway/internal/tracearchive	0.007s
```

变异后（转红）：

```
=== RUN   TestWriterPoisonedBlockCannotBeSealed
    writer_internal_test.go:201: 毒化块的 PendingSeals 不该报错（它只是被跳过）: tracearchive: flush 写缓冲失败: 注入的写失败：no space left on device
--- FAIL: TestWriterPoisonedBlockCannotBeSealed (0.00s)
=== RUN   TestWriterQuarantineRenameFailureEmptiesSpool
    writer_internal_test.go:381: tracearchive: flush 写缓冲失败: 注入的写失败：no space left on device
--- FAIL: TestWriterQuarantineRenameFailureEmptiesSpool (0.00s)
FAIL
FAIL	github.com/example/llm-gateway/internal/tracearchive	0.007s
FAIL
```

### M29 MarkSealed 不再比较 dt/part（无条件清除）

命令（两侧完全相同）：`go test ./tests/tracearchive/ -count=1 -v -run 'TestWriterMarkSealedIgnoresNonMatchingPart'`

基线（未变异）：

```
=== RUN   TestWriterMarkSealedIgnoresNonMatchingPart
--- PASS: TestWriterMarkSealedIgnoresNonMatchingPart (0.00s)
PASS
ok  	github.com/example/llm-gateway/tests/tracearchive	0.005s
```

变异后（转红）：

```
=== RUN   TestWriterMarkSealedIgnoresNonMatchingPart
    writer_test.go:840: 错配的 MarkSealed 不得清除当前块（want part-000000#row=2），实际 2026-09-24/blue/part-000001#row=0
--- FAIL: TestWriterMarkSealedIgnoresNonMatchingPart (0.00s)
FAIL
FAIL	github.com/example/llm-gateway/tests/tracearchive	0.005s
FAIL
```

### M30 AppendFailed 不再落盘（直接返回 nil）

命令（两侧完全相同）：`go test ./tests/tracearchive/ -count=1 -v -run 'TestWriterAppendFailedWritesMarkerLineAndAdvancesRow|TestWriterMarkerLineCountsRowsAndBytes'`

基线（未变异）：

```
=== RUN   TestWriterAppendFailedWritesMarkerLineAndAdvancesRow
--- PASS: TestWriterAppendFailedWritesMarkerLineAndAdvancesRow (0.00s)
=== RUN   TestWriterMarkerLineCountsRowsAndBytes
--- PASS: TestWriterMarkerLineCountsRowsAndBytes (0.00s)
PASS
ok  	github.com/example/llm-gateway/tests/tracearchive	0.006s
```

变异后（转红）：

```
=== RUN   TestWriterAppendFailedWritesMarkerLineAndAdvancesRow
    writer_test.go:72: 落盘失败的行仍应占用行号: want row 2, got 1
--- FAIL: TestWriterAppendFailedWritesMarkerLineAndAdvancesRow (0.00s)
=== RUN   TestWriterMarkerLineCountsRowsAndBytes
    writer_test.go:765: 全标记行的块也必须能被封块，实际报出 0 个任务
--- FAIL: TestWriterMarkerLineCountsRowsAndBytes (0.00s)
FAIL
FAIL	github.com/example/llm-gateway/tests/tracearchive	0.007s
FAIL
```

### M31 字节计数漏掉换行字节（承重约束 2）

命令（两侧完全相同）：`go test ./tests/tracearchive/ -count=1 -v -run 'TestWriterPendingSealsReportsTaskFields|TestWriterMarkerLineCountsRowsAndBytes'`

基线（未变异）：

```
=== RUN   TestWriterPendingSealsReportsTaskFields
--- PASS: TestWriterPendingSealsReportsTaskFields (0.00s)
=== RUN   TestWriterMarkerLineCountsRowsAndBytes
--- PASS: TestWriterMarkerLineCountsRowsAndBytes (0.00s)
PASS
ok  	github.com/example/llm-gateway/tests/tracearchive	0.006s
```

变异后（转红）：

```
=== RUN   TestWriterPendingSealsReportsTaskFields
    writer_test.go:348: UncompressedBytes 应为 spool 字节数 44（含换行），实际 40
--- FAIL: TestWriterPendingSealsReportsTaskFields (0.00s)
=== RUN   TestWriterMarkerLineCountsRowsAndBytes
    writer_test.go:772: 封块任务字节数应含标记行（want 20），实际 16
--- FAIL: TestWriterMarkerLineCountsRowsAndBytes (0.00s)
FAIL
FAIL	github.com/example/llm-gateway/tests/tracearchive	0.006s
FAIL
```

### M32 删掉 truncateToLastNewline 的 Truncate(0) 退路（新增分支的变异）

命令（两侧完全相同）：`go test ./internal/tracearchive/ -count=1 -v -run 'TestWriterTruncatesWholeFileWhenNoLineBoundaryExists'`

基线（未变异）：

```
=== RUN   TestWriterTruncatesWholeFileWhenNoLineBoundaryExists
--- PASS: TestWriterTruncatesWholeFileWhenNoLineBoundaryExists (0.00s)
PASS
ok  	github.com/example/llm-gateway/internal/tracearchive	0.006s
```

变异后（转红）：

```
=== RUN   TestWriterTruncatesWholeFileWhenNoLineBoundaryExists
    writer_internal_test.go:313: 无行边界可用时必须清空文件（否则残行会被当成第 0 行返回），实际 2 字节: "li"
--- FAIL: TestWriterTruncatesWholeFileWhenNoLineBoundaryExists (0.00s)
FAIL
FAIL	github.com/example/llm-gateway/internal/tracearchive	0.006s
FAIL
```

## 11.7 遗留义务与未决项

1. **未成文义务（已登记，未校验）**：`Append` 的正文不得是字面量 `null`——它与 `AppendFailed` 的标记行逐字节相同，任务 7 按整行相等识别标记行时会把真实正文误报成「正文从未落盘」。生产侧正文恒为 `{...}`（struct 序列化）故安全；禁止 `null` 会连标记行一起禁掉，所以只写进 `Append` 的前置条件。**若将来允许把裸 JSON 标量写进正文，这条必须重新评估。**
2. **毒化块会固定占住一份盘上空间**（≤ 一个块），直到人工按 11.3.7 的判据处理。这不是无限增长（该块已停止写入、编号也不会复用），但**需要一个清理/巡检手段**：建议后续在运维侧扫 spool 目录下「内容是明文 JSONL 的 `part-*.jsonl.zst`」并告警——本轮不做（不在任务 5 的接口范围内）。
3. **`PendingSeals` 的 `rows == 0` 守卫不可达**（与本轮无关，§7.A 的结论不变）：写失败路径现在会先毒化，`PendingSeals` 在 `cur.poisoned` 处就返回了，故「rows==0 且未毒化」经公开 API 不可达。`49475a78` 已把这条**写进代码注释本身**（`writer.go:346-352`，含保留理由），不再只存在于报告里。
4. **影子缓冲版本（能保住已成功行、块仍可封）本轮不做**，按裁决留作「已知的更彻底方案」；本轮机制以安全为先，代价是毒化块的已写正文不自动归档。
5. **长期答案是背压**（裁决认定）：盘满时网关应放弃正文并标 `body_missing=true`，而不是持续重试写盘——属计划 2 职责。
6. **`Paths` 仍未做参数校验**（任务 4 已 CLOSED，本轮未动）。
7. **持续 ENOSPC 下块序号与目录项本地无界增长（复审第 4 项，裁决为「记账、不在本轮修」）**：每次写失败都会换到下一个 part，而 `nextSeqLocked` 每次重扫产物目录与 spool 目录，因此磁盘长期满且调用方持续 Append 时，块序号与目录项无界增长，且每次 Append 付一次 O(n) 目录扫描（旧实现有界）。**按裁决只记账、不加计时器或计数器**：它不产生静默错正文，属背压问题（控制者记为 R9），修法在计划 2。已写进代码注释 `writer.go:150-156`（【已知代价：持续 ENOSPC 下块序号与目录项无界增长】），并在其中写明「本地无界，依赖网关侧背压；背压落地前由运维磁盘告警兜住」。

## 11.8 本轮逐条改动清单（供逐行核对，不再只看「已改」字样）

复审的六项未关闭项，逐条给出**最终状态 + `文件:行号`**（行号取自 `49475a78` 提交后的工作区）。凡「已改」都可直接 `sed -n 'N,Mp' <file>` 核对。

| # | 复审项 | 状态 | 位置（`sed -n 'N,Mp'` 可直接核对） |
|---|---|---|---|
| 1 | 报告称改了、代码未改（`ErrFooterCorrupt`） | **已改**：注释改为「读取侧查不到它 ⇒ `ErrNotArchived`，不是 `ErrFooterCorrupt`」 | `internal/tracearchive/writer.go:499-508`（运维判据段 `509-513` 未动，那句是对的） |
| 2 | `writer.go` 注释回归：`valiateLine` 拼写 + 虚构的「第二个 dt 入口」 | **已改**：写清 dt 与正文是两条独立前置校验、`validateDT` 是唯一 dt 入口、`appendLineLocked` 两样都不做（否则标记行被自己的守卫拒绝） | `internal/tracearchive/writer.go:207-209` |
| 3 | `PendingSeals` 的 `rows == 0` 守卫注释已失效 | **已改**：写明「经公开 API 不可达」+ 为什么保留（不让封块器拿到 0 行 spool）+ 指向报告 §11.7.3 | `internal/tracearchive/writer.go:346-349`（守卫本体 `350-352`） |
| 4 | 持续 ENOSPC 的块序号/目录项无界增长 | **按裁决只记账**：写进写失败契约，注明本地无界、依赖网关侧背压（R9）、不加计时器/计数器 | `internal/tracearchive/writer.go:150-156`；报告 §11.7.7 |
| 5 | `truncateToLastNewline` 的「全文件无 `\n` ⇒ `Truncate(0)`」零覆盖 | **已补用例**：`ensureBlockLocked` 建 0 字节空块 → 注入短写让首次 flush 就短写 → 断言隔离开的隔离件被清空、块被毒化、下次 Append 换 part；删除该退路时转红 | 用例 `internal/tracearchive/writer_internal_test.go:258-335`（判据写在 `248-257`）；退路本体 `internal/tracearchive/writer.go:601`；变异 M32，完整双侧输出见 §11.6.1 |
| 6 | 变异两侧原始输出不齐全 | **已补**：14 项逐项给出聚焦命令的**完整**输出块（基线 + 变异后，脚本落盘、非摘录） | 报告 §11.6.1（`/tmp/mutate5.py` 生成，原文 `/tmp/round2-evidence.md`） |

本轮（`771d7985` / `8359f4bc` / `49475a78`）触及的生产代码与用例落点，供交叉核对（行号取自 `49475a78` 之后的工作区，逐个用 `sed` 核对过）：

| 改动 | 位置（声明起止行） |
|---|---|
| `validateLine`（行内换行 + 空行） | `internal/tracearchive/writer.go:73-84` |
| 公开 `Append`（校验顺序：dt → trim → 行校验 → 共享落盘） | `internal/tracearchive/writer.go:216-237` |
| `appendLineLocked`（共享落盘路径，**不校验**；`AppendFailed` 复用） | `internal/tracearchive/writer.go:238-270` |
| `AppendFailed`（复用 `Append`；粘性错误说明） | `internal/tracearchive/writer.go:299-325` |
| `PendingSeals`（无条件 flush、跳过毒化块、`rows==0` 守卫） | `internal/tracearchive/writer.go:326-375` |
| `MarkSealed`（关闭旧句柄 + dt/part 匹配） | `internal/tracearchive/writer.go:376-387` |
| `curBlock.poisoned` 与 `ensureBlockLocked`（毒化块不复用） | `internal/tracearchive/writer.go:103-167`（结构体）、`427-473`（确保块） |
| `nextSeqLocked`（每次重扫两个目录——§11.7.7 的代价来源） | `internal/tracearchive/writer.go:474-514` |
| `quarantineSuffix` 常量与副作用注释 | `internal/tracearchive/writer.go:515`（常量）、`490-514`（注释） |
| `quarantinePath`（必须经 `partName` 生成） | `internal/tracearchive/writer.go:523-530` |
| `poisonCurLocked`（①回退 ②隔离 ③毒化 ④退路） | `internal/tracearchive/writer.go:531-571` |
| `truncateToLastNewline`（反向扫描 + 无 `\n` 清空） | `internal/tracearchive/writer.go:572-603` |
| B 层 4 个新用例 | `tests/tracearchive/writer_test.go:614`（行内换行）、`:692`（空行）、`:744`（标记行统计）、`:814`（MarkSealed 匹配） |
| A 层 5 个用例 | `internal/tracearchive/writer_internal_test.go:104`（不留残行）、`:175`（毒化不可封）、`:258`（无行边界清空，本轮新增）、`:338`（改名失败清空）、`:400`（MarkSealed 关句柄） |

用例总数：B 层 writer 用例 21 个（`tests/tracearchive/writer_test.go` 里 19 个 `TestWriter*` + 2 个 `TestNewWriter*`），A 层 5 个。



