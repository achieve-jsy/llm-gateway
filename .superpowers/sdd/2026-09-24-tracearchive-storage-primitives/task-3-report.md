# Task 3 报告：32 字节 footer 的编解码与校验

**状态：** DONE
**提交：** `04647e9a` feat(tracearchive): 实现 32 字节定长 footer 的编解码与校验
**基线：** `d847ef57`（控制器在本任务进行期间并发提交了 defaults.go 的编码器裁决文档；本次提交在其之上，只含本人两个文件）

---

## 1. 实现了什么

产物文件末尾的**定长 32 字节 footer**，是读取端定位索引的唯一入口——只需 `pread` 末尾 32 字节即可得知 trailer 位置，不必解压任何数据。

新增文件（仅这两个）：

| 文件 | 内容 |
|------|------|
| `internal/tracearchive/footer.go` | `FooterSize=32`、`footer` 结构体、`encodeFooter`、`decodeFooter`、`ErrFooterCorrupt`、`footerMagic="TBA4"`、`formatVersion=1` |
| `internal/tracearchive/footer_test.go` | 6 个用例（见 §2） |

字节布局（与规格 §5.2 / 简报逐字节一致，已用独立手段核对，见 §3.3）：

```
[0:4)   magic "TBA4"
[4:6)   format_version (2)
[6:14)  trailer_offset (8)
[14:18) trailer_len    (4)
[18:22) row_count      (4)
[22:30) frame_sha256[:8](8)
[30:32) reserved       (2, 恒 0)
= 32 字节
```

**`row_count` 已实现且不可省**：类型为 `int32`，编码在 `[18:22)`。读取端（Task 7）会把它作为 `decodeTrailer(b, n)` 的期望条目数——缺少它时被截断的 trailer 会被静默解码成更少的条目，读取端也无法判断 `ref.Row` 越界，这正是规格 §8.1 要求避免的「把产物损坏误判成数据不存在」。解码端另拒绝负 `row_count`、负 `trailer_offset`、负 `trailer_len`。

**校验路径**（任一不符即返回包装了 `ErrFooterCorrupt` 的错误，便于 `errors.Is` 分类）：
长度 ≠ 32 → magic 不符 → 未知 `format_version` → 字段为负。

`magic` 与 `format_version` 是冷层格式演进（规格 §5.5 超级文件）的锚点：后续若要扩展索引键，提升 `formatVersion` 即可让老读取端明确拒绝，而不是误读。

## 2. 测试内容与结果

简报给出的 6 个用例原样落地（未增删）：

1. `TestFooterRoundTrip` — 四个字段（含非零 `FrameSHA`）编码后长度恰为 32，解码后与输入**结构体全等**（同时隐含校验了字段边界：各字段取值互不相同，错位即失败）。
2. `TestFooterRowCountRoundTrip` — `0, 1, 255, 256, 65535, 1<<20` 六档 `row_count` 无损往返，覆盖 varint 非相关的定长 4 字节全域边界。
3. `TestDecodeFooterRejectsNegativeRowCount` — 负 `row_count` 必须报错。
4. `TestDecodeFooterRejectsWrongLength` — `32-1` 与 `32+1` 都必须报错。
5. `TestDecodeFooterRejectsBadMagic` — 篡改 `b[0]` 后必须报错。
6. `TestDecodeFooterRejectsUnknownVersion` — `b[4]=99` 后必须报错。

**结果：6/6 通过，输出干净**（无 SKIP、无警告，`ok ... 0.006s`）。

## 3. TDD 证据

### 3.1 RED（实现之前）

命令（先写 `footer_test.go`，此时 `footer.go` 尚不存在）：

```bash
go test ./internal/tracearchive/ -run 'TestFooter|TestDecodeFooter' -v
```

实际输出：

```
# github.com/example/llm-gateway/internal/tracearchive [github.com/example/llm-gateway/internal/tracearchive.test]
internal/tracearchive/footer_test.go:6:8: undefined: footer
internal/tracearchive/footer_test.go:12:7: undefined: encodeFooter
internal/tracearchive/footer_test.go:13:15: undefined: FooterSize
internal/tracearchive/footer_test.go:14:50: undefined: FooterSize
internal/tracearchive/footer_test.go:16:14: undefined: decodeFooter
internal/tracearchive/footer_test.go:28:8: undefined: encodeFooter
internal/tracearchive/footer_test.go:28:21: undefined: footer
internal/tracearchive/footer_test.go:29:15: undefined: decodeFooter
internal/tracearchive/footer_test.go:40:7: undefined: encodeFooter
internal/tracearchive/footer_test.go:40:20: undefined: footer
internal/tracearchive/footer_test.go:40:20: too many errors
FAIL	github.com/example/llm-gateway/internal/tracearchive [build failed]
FAIL
[exit: 1]
```

与简报预期的差异（如实记录）：简报预计会看到 `undefined: footer`、`undefined: FooterSize`、`unknown field RowCount`。实际编译在 `undefined: footer` 处即达 "too many errors" 上限并终止，因此 `unknown field RowCount` 这条**未出现**——原因是类型本身未定义时，字段解析尚未进行。这不影响 RED 的有效性：6 个用例全部因缺少实现而无法编译。

### 3.2 GREEN（实现之后）

命令：

```bash
go test ./internal/tracearchive/ -run 'TestFooter|TestDecodeFooter' -count=1 -v
```

实际输出：

```
=== RUN   TestFooterRoundTrip
--- PASS: TestFooterRoundTrip (0.00s)
=== RUN   TestFooterRowCountRoundTrip
--- PASS: TestFooterRowCountRoundTrip (0.00s)
=== RUN   TestDecodeFooterRejectsNegativeRowCount
--- PASS: TestDecodeFooterRejectsNegativeRowCount (0.00s)
=== RUN   TestDecodeFooterRejectsWrongLength
--- PASS: TestDecodeFooterRejectsWrongLength (0.00s)
=== RUN   TestDecodeFooterRejectsBadMagic
--- PASS: TestDecodeFooterRejectsBadMagic (0.00s)
=== RUN   TestDecodeFooterRejectsUnknownVersion
--- PASS: TestDecodeFooterRejectsUnknownVersion (0.00s)
PASS
ok  	github.com/example/llm-gateway/internal/tracearchive	0.006s
[exit: 0]
```

**该命令能选中的测试清单（6/6，全部）：**

| 用例 | 是否被 `-run 'TestFooter\|TestDecodeFooter'` 选中 |
|------|------|
| `TestFooterRoundTrip` | ✅ |
| `TestFooterRowCountRoundTrip` | ✅ |
| `TestDecodeFooterRejectsNegativeRowCount` | ✅ |
| `TestDecodeFooterRejectsWrongLength` | ✅ |
| `TestDecodeFooterRejectsBadMagic` | ✅ |
| `TestDecodeFooterRejectsUnknownVersion` | ✅ |

**为什么必须用组合模式（Task 2 踩过的坑，已实测确认）：** Go 的 `-run` 是**非锚定子串/正则匹配**。实测 `go test ./internal/tracearchive/ -run TestFooter -v` 只选中 **2 个**用例：

```
=== RUN   TestFooterRoundTrip
--- PASS: TestFooterRoundTrip (0.00s)
=== RUN   TestFooterRowCountRoundTrip
--- PASS: TestFooterRowCountRoundTrip (0.00s)
ok  	github.com/example/llm-gateway/internal/tracearchive	0.005s
```

4 个 `TestDecodeFooterRejects*` 的名字里没有子串 `TestFooter`（只有 `Footer`），因此被漏掉——若只交这条命令作 GREEN 证据，**「四类拒绝逻辑全部坏掉」也依然会绿**。故本任务全程使用 `-run 'TestFooter|TestDecodeFooter'`。建议计划里 Task 4/5/6/7 的 STEP 4 命令一并改为组合模式（各任务的 GREEN 命令原文都写的是 `-run TestXxx` 形式）。

### 3.3 附加的独立验证（超出简报，未新增仓库文件）

简报说「务必逐字节核对」。往返测试能抓住字段错位，但不是对偏移量的直接断言。为此我用 `go test -overlay` 在 **/tmp** 中挂载了一个临时测试（仅存在于本机 /tmp，运行后已删除，**仓库内没有新增任何文件**），按规格偏移逐字节断言：

- `b[0:4] == "TBA4"`、`b[4:6] == 1`
- `b[6:14] == 0x0102030405060708`（BigEndian）、`b[14:18] == 0x0A0B0C0D`、`b[18:22] == 0x11223344`
- `b[22:30] == A1..A8`、`b[30:32] == {0,0}`（reserved 恒 0）
- 同一字节序列解码后三字段值完全还原

结果：`--- PASS: TestOverlayFooterByteLayout (0.00s)`。确认偏移与规格 §5.2 逐字节一致，`reserved` 未被误用于任何字段。

### 3.4 回归与静态检查

```bash
go test ./internal/tracearchive/ -short -v
```

11 个用例：10 PASS，`TestGoEncoderPeakRSS` 按设计在 `-short` 下 SKIP（Task 1 的标定测试，非本任务范围）。Task 2 的 4 个 trailer 用例全部仍 PASS，无回归。

```bash
go vet ./internal/tracearchive/     # 无输出，通过
gofmt -l internal/tracearchive/footer.go internal/tracearchive/footer_test.go   # 无输出
go build ./internal/... ./cmd/...   # 通过
```

## 4. 变更的文件

```
internal/tracearchive/footer.go      | 79 ++++++++++++++++++++++++++
internal/tracearchive/footer_test.go | 69 ++++++++++++++++++++++++++
2 files changed, 148 insertions(+)
```

仅这两个文件（`git diff --cached --name-only` 已在提交前确认）。`defaults.go` / `calib_test.go` / `index.go` / `index_test.go` 未被本人改动。

## 5. 自检发现

| 检查项 | 结论 |
|--------|------|
| 5 个步骤是否都做了 | ✅ 写测试 → RED（编译失败证据）→ 写实现 → GREEN（6/6）→ 提交 |
| 6 个测试是否都在 | ✅ 往返、row_count 往返、负 row_count、长度、magic、版本，一个不少 |
| footer 恰为 32 字节 | ✅ `len(encodeFooter(...)) != 32` 断言在 `TestFooterRoundTrip` 中；另 §3.3 独立断言 |
| 命名与注释 | ✅ 导出 `FooterSize`/`ErrFooterCorrupt` 有文档注释；注释解释「为什么」（读取端只需 pread、row_count 是越界与截断判断依据、reserved 恒 0、magic/version 为冷层演进锚点）而非复述代码 |
| YAGNI | ✅ 只实现被要求的内容：未加 `Marshal`/`io` 封装、未加 CRC、未加 reserved 语义、未提前实现 Sealer/Reader 相关逻辑 |
| TDD 证据真实性 | ✅ RED 为真实编译失败（exit 1）；GREEN 为真实 6 用例 PASS（exit 0）；两条命令与输出均已原文记录于 §3 |
| GREEN 命令能选全测试 | ✅ 已实测确认 `-run TestFooter` 只选 2/6，故改用 `-run 'TestFooter\|TestDecodeFooter'`（2+4=6/6），§3.2 给出对照证据 |
| 输出是否干净 | ✅ 无 SKIP、无 vet 告警、无 gofmt 差异、无多余日志 |
| 自读一遍 diff | ✅ 代码与简报逐字符一致（除下方 §6.1 的记录性说明），提交内容与预期一致 |

## 6. 问题与顾虑

### 6.1 计划文档的接口清单漏了 `RowCount`（文档不一致，未修改）

简报第 11 行与计划原文（`docs/superpowers/plans/...md:499`）的「对外产出」写成：

```go
type footer struct { TrailerOffset int64; TrailerLen int32; FrameSHA [8]byte }
```

而同一份简报的布局表、任务说明「`row_count` 不可省」、以及全部 6 个测试都要求 `RowCount int32`。**以测试与布局为准**，实现里 `RowCount` 是结构体成员（`int32`，编在 `[18:22)`），否则 Task 6 无法写入、Task 7 无法取期望条目数。这属于计划文本的漏字，不是代码缺陷；因硬约束限定只新建两个文件，我未改计划文档，**建议控制者修正该行**，以免 Task 6/7 的实现者按接口清单漏掉该字段。

### 6.2 `go build ./...` 存在与本次无关的预存在失败

```
# github.com/example/llm-gateway/test
test/bedrock_model_id_check.go:98:23: undefined: bedrock.NewListFoundationModelsPaginator
```

该文件在 `test/`（package `test`），与 `internal/tracearchive` 无依赖关系，且 `go build ./internal/... ./cmd/...` 全绿。确认与本次改动无关，未做处理（也不在授权范围内）。

### 6.3 `internal/tracearchive/defaults.go` 不符合 gofmt（他人文件）

`gofmt -l internal/tracearchive/` 仍报 `defaults.go`（我的两个文件无输出）。原因是 Go 1.19+ 的注释重排规则（列表项 `//\t1.` 需改为 `//  1.`，且缩进列表后需空注释行）。该文件属 Task 1/控制器的范围且已入库，本次未触碰，**建议控制者单独 `gofmt -w` 一次**，否则 `make lint` 或 CI 的 gofmt 检查可能连带失败。

### 6.4 并发写入提示

本任务执行期间，控制器在工作区内并发提交了 `d847ef57`（defaults.go 文档裁决为方案 C）。该提交为纯注释改动，不影响本包常量取值与测试结果；我在其之上重新跑过 GREEN，并只暂存/提交了自己的两个文件，工作树提交后干净。

### 6.5 无其它顾虑

未发现简报代码的真实缺陷：长度/magic/版本/负值四类校验齐备，错误均包装 `ErrFooterCorrupt` 可供 `errors.Is` 分类；`int32`↔`uint32` 与 `int64`↔`uint64` 的双向转换对负值有显式拒绝，不存在静默回绕。无需偏离简报。

---

# 修复轮次 1/5 报告

**状态：** 已修复
**提交：** `532746ca` test(tracearchive): 补 footer 的字节布局 golden 与错误身份断言（新提交，未 amend）
**评审裁决：** 通过，附 1 项「重要」测试覆盖缺口 + 2 项「轻微」

**技术异议：无。** 评审的 4 条意见我全部认同，理由如下——「编解码自对称」确实不能证明布局契约（编码器是测试字节的唯一来源，读写同错即全绿）；`err == nil` 也确实不能阻止错误身份退化（`%w`→`%v` 后 6/6 依旧绿），而 Task 7 的 Reader 依赖 `errors.Is` 区分「产物损坏 / 数据不存在」（规格 §8.1）。无一条需要争辩或降级处理，全部照办。

## 1. 改了什么

**只改了 `internal/tracearchive/footer_test.go`（+91/−11）。`footer.go` 未改动**——三条修复都在测试侧（评审亦未要求改实现）。

| 评审项 | 修复内容 |
|--------|----------|
| 【重要 1】负 `TrailerOffset` / 负 `TrailerLen` 守卫零覆盖 | 新增 `TestDecodeFooterRejectsNegativeOffsetOrLen`，两个子例（`负 trailer_offset` = `{TrailerOffset:-1, TrailerLen:4, RowCount:1}`、`负 trailer_len` = `{TrailerOffset:8, TrailerLen:-1, RowCount:1}`），各断言 `errors.Is(err, ErrFooterCorrupt)` |
| 【重要 2】六条断言只查 `err == nil`，未断言错误身份 | 全部 5 个拒绝用例（共 7 处断言）统一改为 `errors.Is(err, ErrFooterCorrupt)`，失败信息打印实际 `err`；新增 `errors` import。分支↔断言映射现已齐备：长度 ← `TestDecodeFooterRejectsWrongLength` 两子例；magic ← `…BadMagic`；version ← `…UnknownVersion`；**负 offset/len ← `…NegativeOffsetOrLen` 两子例（本次补齐）**；负 row_count ← `…NegativeRowCount` |
| 【轻微 1】字节布局缺显式 golden 断言 | 新增 `TestFooterGoldenBytes`：固定输入 `TrailerOffset:0x0102030405060708`、`TrailerLen:0x0A0B0C0D`、`RowCount:0x0E0F1011`、`FrameSHA:0x12..0x19`，断言 `encodeFooter` 输出**逐字节等于**手写 32 字节期望（`"TBA4"`、`[4:6]=0x00,0x01`、各字段 BigEndian、`[30:32]=0x00,0x00`）；并反向断言 `decodeFooter(want) == in`。期望值长度另由 `len(want) != FooterSize` 自检，避免黄金值本身写错长度 |
| 【轻微 2】`32+1` 子例区分力弱 | 长度扰动改为以**合法编码结果为基底**：`valid[:FooterSize-1]`（少一字节）与 `append(append([]byte{}, valid...), 0x00)`（多一字节）。两者 magic 与 version 均合法，**只有长度检查能拒绝**，长度检查因此真正承重（原先全零缓冲在 magic 处即失败） |

## 2. 修复后的聚焦测试

命令：

```bash
go test ./internal/tracearchive/ -run 'TestFooter|TestDecodeFooter' -count=1 -v
```

结果：**12 处 PASS，0 处 FAIL，输出干净（无 SKIP/警告），exit 0**

```
--- PASS: TestFooterRoundTrip (0.00s)
--- PASS: TestFooterGoldenBytes (0.00s)
--- PASS: TestFooterRowCountRoundTrip (0.00s)
--- PASS: TestDecodeFooterRejectsNegativeRowCount (0.00s)
--- PASS: TestDecodeFooterRejectsNegativeOffsetOrLen (0.00s)
    --- PASS: TestDecodeFooterRejectsNegativeOffsetOrLen/负_trailer_offset (0.00s)
    --- PASS: TestDecodeFooterRejectsNegativeOffsetOrLen/负_trailer_len (0.00s)
--- PASS: TestDecodeFooterRejectsWrongLength (0.00s)
    --- PASS: TestDecodeFooterRejectsWrongLength/少一字节 (0.00s)
    --- PASS: TestDecodeFooterRejectsWrongLength/多一字节 (0.00s)
--- PASS: TestDecodeFooterRejectsBadMagic (0.00s)
--- PASS: TestDecodeFooterRejectsUnknownVersion (0.00s)
PASS
ok  	github.com/example/llm-gateway/internal/tracearchive	0.005s
```

**该命令能选中的测试清单（7 个测试函数 / 9 个用例，全部选中）：**

| # | 测试函数 | 子例 |
|---|----------|------|
| 1 | `TestFooterRoundTrip` | — |
| 2 | `TestFooterGoldenBytes`（新增） | — |
| 3 | `TestFooterRowCountRoundTrip` | — |
| 4 | `TestDecodeFooterRejectsNegativeRowCount` | — |
| 5 | `TestDecodeFooterRejectsNegativeOffsetOrLen`（新增） | `负_trailer_offset`、`负_trailer_len` |
| 6 | `TestDecodeFooterRejectsWrongLength` | `少一字节`、`多一字节` |
| 7 | `TestDecodeFooterRejectsBadMagic` | — |
| 8 | `TestDecodeFooterRejectsUnknownVersion` | — |

（`-run TestFooter` 单模式仍然只选 2/7，理由同 §3.2，本命令继续使用组合模式。）

## 3. 变异验证：证明新增断言承重

方法：`go test -overlay` 把 `internal/tracearchive/footer.go` 替换为 **/tmp 下的变异副本**（不落仓库文件），再跑同一聚焦命令。三个变异各跑一次：

```bash
go test -overlay /tmp/tbamut/overlay<A|B|C>.json ./internal/tracearchive/ -run 'TestFooter|TestDecodeFooter' -count=1 -v
```

| 变异 | 具体改动 | 预期转红 | 实测结果 | 退出码 |
|------|----------|----------|----------|--------|
| **A** 移除守卫 | 删掉 `if f.TrailerOffset < 0 \|\| f.TrailerLen < 0 { … }` 整块 | 只有负 offset/len 用例 | **`TestDecodeFooterRejectsNegativeOffsetOrLen` FAIL（`负_trailer_offset`、`负_trailer_len` 两个子例均 FAIL）**，其余 6 个函数全 PASS | 1 |
| **B** 错误身份退化 | `sed 's/%w/%v/g'`（5 处全部替换） | 全部拒绝用例 | **5 个拒绝用例 FAIL（合计 7 处断言）**：`…NegativeRowCount`、`…NegativeOffsetOrLen`（2 子例）、`…WrongLength`（2 子例）、`…BadMagic`、`…UnknownVersion`；`RoundTrip`/`GoldenBytes`/`RowCountRoundTrip` 仍 PASS | 1 |
| **C** 对称错位（评审所指的盲区） | `[18:22) row_count` 与 `[22:30) frame_sha` 互换，**编码与解码同时改**（`copy(b[18:26], FrameSHA)` + `PutUint32(b[26:30], RowCount)`；解码对称） | 只有 golden 转红 | **`TestFooterGoldenBytes` FAIL，而 `TestFooterRoundTrip` 与其余全部 PASS** | 1 |

变异 C 是对【轻微 1】价值的直接证明：这是一个**读写自洽、往返全绿**的布局 bug，加固前 6/6 通过，加固后仅 golden 一条即可拦住它。变异 A 与变异 B 分别证明负值守卫与错误身份现在都各有专属断言承重，不再「删掉也全绿」。

变异文件全部位于 `/tmp/tbamut/`，运行后已 `rm -rf` 删除；`git diff --stat -- internal/tracearchive/footer.go` 为空，确认仓库内 `footer.go` 未被 overlay 触碰。

## 4. gofmt / vet / 回归

```bash
gofmt -l internal/tracearchive/footer.go internal/tracearchive/footer_test.go   # 无输出，合规
go vet ./internal/tracearchive/                                                  # 无输出，通过
go test ./internal/tracearchive/ -short -count=1                                  # ok，exit 0
```

全包 `-short` 回归通过：Task 1 的 `TestGoEncoderPeakRSS` 按设计 SKIP，Task 2 的 4 个 trailer 用例与本次 8 个 footer 用例全绿。

## 5. 遗留与状态更新

- **原 §6.3（`defaults.go` 不符合 gofmt）已由控制者在 `8ab755b8` style(tracearchive): gofmt defaults.go（仅注释列表缩进）解决**，`gofmt -l internal/tracearchive/` 现已全部干净。
- **原 §6.1（计划/简报接口清单漏 `RowCount`）仍未处理**：`docs/.../2026-09-24-tracearchive-storage-primitives.md:499` 的「对外产出」仍写作 `{TrailerOffset; TrailerLen; FrameSHA}`。实现与测试均以布局表为准（`RowCount int32`，编在 `[18:22)`），建议在进入 Task 6 前修正该行，否则 Task 6 写入侧会漏字段。
- **原 §6.2（`go build ./...` 中 `test/bedrock_model_id_check.go:98` 失败）与本任务无关**，仍未处理，不在授权范围。
- 本轮期间工作区出现他人并发改动（`internal/tracearchive/index.go`、`index_test.go`、`docs/superpowers/specs/2026-09-23-...md`）。我提交前用 `git diff --cached --name-only` 核对，**只暂存了 `footer_test.go`**，未夹带他人改动。
