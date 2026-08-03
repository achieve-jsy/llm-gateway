# Task 6 报告：封块器（spool → C 版 zstd CLI → trailer/footer → fsync → rename）

**状态**：完成，已提交（含第二轮评审裁决落实，见 §10）。
**提交**：`76042bcd`（首轮）+ `e0b0084f`（裁决 1/2/3）+ `37f95c36`（裁决 4/5/6）+ `eaccd92b`（评审 ①–⑬）+ `69d9c317`（复审轮 2：④ 的 SKIP/作者意图缺口、⑬ 的失实论据、轻微 1/2/3）（分支 `feat/trace-body-direct-write`，工作目录 `/Users/jsy/Public/self_code/llm-gateway/.worktrees/trace-body-direct-write`）
**交付内容**：5 个文件、1513 行

> 📌 **勘误（控制者，`3ff5c0d0` 时补记）**：本报告 §10.3-3 / §10.6.2 仍称版本探测结果被记忆化（`s.versionOK`），
> 与实际代码矛盾——`37f95c36` 已**整段删除**记忆化，`grep -c versionOK = 0`。
> **以代码为准**：不做记忆化。§10.3-3 的 **m44 已不再是删除型变异**（分支不存在，无变异可做），
> 该节后被本报告 §11.1 取代。同理 §7.1/§8.1 称「minAge 默认 = `defaultSealInterval`」亦已过时，
> 代码为 `defaultSealMinAge = 3h`（`e0b0084f` 取代）。**凡早于 §11 的结论，以 §11 与代码为准。**
>
> 📌 **勘误补充（控制者，`f33d1091` 时补记）**：本报告 **:557** 与 **:896**（变更汇总表第 12 行）
> 仍称「`dt=..`/`inst=..` 会被 `filepath.Join` 清理语义带出 spool 子树」——**该前提错误**（不折叠；
> 列举侧的 `dt`/`inst` 取自目录名，目录名不含 `/`，故此处不存在穿越）。**以 §12.10（:2371 起的更正表）为准。**
> 再强调口径：**一般情形**下穿越**成立**，但向量**须含 `/`**（`dt="2026-09-24/../.."`、`inst="../../green"`）；
> **写入侧**取值来自调用方参数、**可含 `/`** ⇒ 写入侧结论保留；**列举侧**按本节更正。
>
> 📌 **已知不精确（控制者裁定不改，`writer.go:44`）**：`validateDT` 的文档把 `"../x"` 归在「路径穿越与空串」下。
> 实测：`"../x"` **确实被拒**（因**不是日期**，且它本身**不是**穿越向量——无折叠），真向量
> `"2026-09-24/../.."` 同样被拒 ⇒ **该句的实践结论正确**（任何穿越向量都会被 `validateDT` 或 Inst 白名单拒），
> 只是分类标签不够精确。**修它需要 `.go` → 计划逐字副本的又一轮往返，收益不成比例，故保留。**
> 记录在此以说明「已考虑，非遗漏」。

| 文件 | 层 | 行数 |
|---|---|---|
| `internal/tracearchive/sealer.go` | 实现 | 435 |
| `tests/tracearchive/sealer_test.go` | B 层（`package tracearchive_test`） | 900 |
| `tests/tracearchive/helpers_test.go` | B 层辅助 | 66 |
| `internal/tracearchive/sealer_internal_test.go` | A 层（`package tracearchive`） | 51 |
| `internal/tracearchive/testhelpers_test.go` | A 层辅助 | 51 |

**结论摘要**

- 三条不可退让的约束全部落实，且各有**自动化**证据（不是只有注释）：§5 的 `m01`/`m02`/`m17`、§5.5 的 grep 检查。
- **简报给的实现照抄会红**：实测发现 2 处真实缺陷（§3.1 目录创建顺序、§3.2 `exec.ErrNotFound` 不可达）+ 1 处测试常量错误（§3.3 golden 自检越界）；按你的要求「最小偏离 + 单列说明」，逐条给了证据。
- 变异自检共 34 条：**31 条删除后有用例转红**；3 条零覆盖（`m15`/`m24`/`m34`），逐条给出原因与保留理由（§5.3）；另有 16 处防御性分支未做删除变异并说明为何不可覆盖（§5.4）。
- 真 zstd 用例**确实执行**（GREEN 输出 `SKIP` 计数 = 0，`--- PASS` 齐全）。本机 `/opt/homebrew/bin/zstd` = **v1.5.7**，生产是 **v1.4.8**——压缩比验收线**没有**在本任务重测（§9）。

---

## 1. 步骤 1/2：先写测试并确认失败

`-run` 用完整合并正则（每条测试函数名都完整出现；因补充了守卫用例，正则在简报基础上扩充）：

```bash
go test ./internal/tracearchive/ ./tests/tracearchive/ -run 'TestSealProducesArtifactAndRemovesSpool|…|TestSealerListPendingSealsUsesConfiguredMinAge' -v
```

原始输出（编译失败，与简报预期一致）：

```text
# github.com/example/llm-gateway/internal/tracearchive [github.com/example/llm-gateway/internal/tracearchive.test]
internal/tracearchive/sealer_internal_test.go:25:10: undefined: NewSealer
internal/tracearchive/sealer_internal_test.go:25:20: undefined: SealerOptions
FAIL	github.com/example/llm-gateway/internal/tracearchive [build failed]
# github.com/example/llm-gateway/tests/tracearchive_test [github.com/example/llm-gateway/tests/tracearchive.test]
tests/tracearchive/sealer_test.go:34:15: undefined: ta.NewSealer
tests/tracearchive/sealer_test.go:34:28: undefined: ta.SealerOptions
tests/tracearchive/sealer_test.go:74:13: undefined: ta.NewSealer
tests/tracearchive/sealer_test.go:74:26: undefined: ta.SealerOptions
tests/tracearchive/sealer_test.go:103:13: undefined: ta.NewSealer
tests/tracearchive/sealer_test.go:103:26: undefined: ta.SealerOptions
tests/tracearchive/sealer_test.go:167:13: undefined: ta.NewSealer
tests/tracearchive/sealer_test.go:167:26: undefined: ta.SealerOptions
tests/tracearchive/sealer_test.go:227:13: undefined: ta.NewSealer
tests/tracearchive/sealer_test.go:227:26: undefined: ta.SealerOptions
tests/tracearchive/sealer_test.go:264:13: undefined: ta.NewSealer
tests/tracearchive/sealer_test.go:264:26: undefined: ta.SealerOptions
tests/tracearchive/sealer_test.go:294:15: undefined: ta.NewSealer
tests/tracearchive/sealer_test.go:294:28: undefined: ta.SealerOptions
tests/tracearchive/sealer_test.go:349:19: undefined: ta.ListPendingSeals
tests/tracearchive/sealer_test.go:367:19: undefined: ta.ListPendingSeals
tests/tracearchive/sealer_test.go:386:19: undefined: ta.ListPendingSeals
tests/tracearchive/sealer_test.go:419:19: undefined: ta.ListPendingSeals
tests/tracearchive/sealer_test.go:460:17: undefined: ta.NewSealer
tests/tracearchive/sealer_test.go:460:30: undefined: ta.SealerOptions
tests/tracearchive/sealer_test.go:503:15: undefined: ta.NewSealer
tests/tracearchive/sealer_test.go:503:28: undefined: ta.SealerOptions
tests/tracearchive/sealer_test.go:522:19: undefined: ta.NewSealer
tests/tracearchive/sealer_test.go:522:32: undefined: ta.SealerOptions
tests/tracearchive/sealer_test.go:529:19: undefined: ta.NewSealer
tests/tracearchive/sealer_test.go:529:32: undefined: ta.SealerOptions
tests/tracearchive/sealer_test.go:555:15: undefined: ta.NewSealer
tests/tracearchive/sealer_test.go:555:28: undefined: ta.SealerOptions
tests/tracearchive/sealer_test.go:585:15: undefined: ta.NewSealer
tests/tracearchive/sealer_test.go:585:28: undefined: ta.SealerOptions
tests/tracearchive/sealer_test.go:620:15: undefined: ta.NewSealer
tests/tracearchive/sealer_test.go:620:28: undefined: ta.SealerOptions
tests/tracearchive/sealer_test.go:651:19: undefined: ta.ListPendingSeals
tests/tracearchive/sealer_test.go:662:15: undefined: ta.NewSealer
tests/tracearchive/sealer_test.go:662:28: undefined: ta.SealerOptions
tests/tracearchive/sealer_test.go:687:20: undefined: ta.ListPendingSeals
tests/tracearchive/sealer_test.go:700:19: undefined: ta.ListPendingSeals
tests/tracearchive/sealer_test.go:746:19: undefined: ta.ListPendingSeals
tests/tracearchive/sealer_test.go:786:17: undefined: ta.NewSealer
tests/tracearchive/sealer_test.go:786:30: undefined: ta.SealerOptions
tests/tracearchive/sealer_test.go:827:15: undefined: ta.NewSealer
tests/tracearchive/sealer_test.go:827:28: undefined: ta.SealerOptions
tests/tracearchive/sealer_test.go:877:15: undefined: ta.NewSealer
tests/tracearchive/sealer_test.go:877:28: undefined: ta.SealerOptions
tests/tracearchive/sealer_test.go:889:16: undefined: ta.NewSealer
tests/tracearchive/sealer_test.go:889:29: undefined: ta.SealerOptions
FAIL	github.com/example/llm-gateway/tests/tracearchive [build failed]
FAIL
```

> 说明：简报预期还包含 `s.failBeforeRename undefined`。实际编译在更早的 `SealerOptions` 处即失败（`s` 类型非法），Go 的类型检查不再报后续字段错误；`-gcflags=-e` 也未列出该条。这不是偏差，是编译器的报错顺序。

---

## 2. 测试分层与「故意重复」

- **B 层**（`tests/tracearchive/sealer_test.go`，`package tracearchive_test`）：23 条用例，**全部**只用公开 API + 规格 §5.2 的产物字节格式契约。连 trailer 的差分 varint 解码都在测试里**独立实现一遍**（`decodeVarints`），避免「自己验自己」。
- **A 层例外**（`internal/tracearchive/sealer_internal_test.go`，`package tracearchive`）：仅 `TestSealIsAtomicOnCrash` 与 `TestParsePartSeqUsedBySealer`。前者必须在包内的理由已写在测试函数注释里：它靠注入**未导出**的 `s.failBeforeRename` 制造「rename 之前一步失败」，从而在不真的崩溃的前提下验证 I2；该钩子刻意不导出（规格不要求它成为生产 API），B 层无法表达。后者是 `parsePartSeq` 的交叉验证。
- 三个辅助函数（`writeSpool`/`requireZstd`/`fakeZstd`）在 A/B 两份文件里**故意重复**，两个文件顶部都写明了「Go 的测试辅助不能跨包共享，不要去重、更不要搬进生产代码」。

---

## 3. 步骤 3：实现——每一步「红 → 绿」的实测证据

为让简报的缺陷以**证据**而非断言呈现，先按简报原文实现（仅额外加上 B 层编译所需的 `MinAge` 字段/方法，见 §6），跑测试；再逐条修正。

### 3.1 缺陷 A（真实缺陷）：`MkdirAll` 在创建 `.tmp` 之后 → 所有真封块用例 ENOENT

简报把 `os.MkdirAll(filepath.Dir(partPath))` 放在 `runZstd` **之后**（rename 之前），而 `.tmp` 路径是 `partPath + ".tmp"`——它就落在产物目录里。目录还不存在时 `os.OpenFile(.tmp)` 直接 ENOENT。10 条用例因此红，典型原始输出：

```text
    sealer_internal_test.go:32: 注入的失败应被原样传播（errors.Is 必须可识别），实际 tracearchive: 封块失败: 建临时产物失败: open /var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestSealIsAtomicOnCrash3491381113/001/dt=2026-09-24/inst=blue/part-000000.jsonl.zst.tmp: no such file or directory
--- FAIL: TestSealIsAtomicOnCrash (0.00s)
    sealer_test.go:43: 封块失败: tracearchive: 封块失败: 建临时产物失败: open /var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestSealProducesArtifactAndRemovesSpool2989911347/001/dt=2026-09-24/inst=blue/part-000000.jsonl.zst.tmp: no such file or directory
--- FAIL: TestSealProducesArtifactAndRemovesSpool (0.00s)
    sealer_test.go:171: tracearchive: 封块失败: 建临时产物失败: open /var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestSealFooterPointsAtValidTrailer403601275/001/dt=2026-09-24/inst=blue/part-000000.jsonl.zst.tmp: no such file or directory
--- FAIL: TestSealFooterPointsAtValidTrailer (0.00s)
    sealer_test.go:236: 错误链里应能取到退出码 3（这要求用 %w 包装），实际 tracearchive: 封块失败: 建临时产物失败: open /var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestSealFailsOnNonZeroExit1620711935/001/dt=2026-09-24/inst=blue/part-000000.jsonl.zst.tmp: no such file or directory
--- FAIL: TestSealFailsOnNonZeroExit (0.00s)
    sealer_test.go:272: 错误链里应保留 exec.ErrNotFound（这要求用 %w 包装），实际 tracearchive: 封块失败: 建临时产物失败: open /var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestSealFailsWhenZstdMissing1900299842/001/dt=2026-09-24/inst=blue/part-000000.jsonl.zst.tmp: no such file or directory
--- FAIL: TestSealFailsWhenZstdMissing (0.00s)
    sealer_test.go:301: tracearchive: 封块失败: 建临时产物失败: open /var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestSealGoldenLayout785325860/001/dt=2026-09-24/inst=blue/part-000000.jsonl.zst.tmp: no such file or directory
--- FAIL: TestSealGoldenLayout (0.00s)
    sealer_test.go:470: 封块失败: tracearchive: 封块失败: 建临时产物失败: open /var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestSealInvokesZstdCLIWithArgs默认级别与默认窗口1509484642/001/dt=2026-09-24/inst=blue/part-000000.jsonl.zst.tmp: no such file or directory
    sealer_test.go:470: 封块失败: tracearchive: 封块失败: 建临时产物失败: open /var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestSealInvokesZstdCLIWithArgs显式级别与显式窗口605729379/001/dt=2026-09-24/inst=blue/part-000000.jsonl.zst.tmp: no such file or directory
--- FAIL: TestSealInvokesZstdCLIWithArgs (0.00s)
    sealer_test.go:510: ZstdBinary 留空时必须回退到 PATH 里的 zstd: tracearchive: 封块失败: 建临时产物失败: open /var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestSealDefaultsZstdBinaryFromPath300644257/003/dt=2026-09-24/inst=blue/part-000000.jsonl.zst.tmp: no such file or directory
--- FAIL: TestSealDefaultsZstdBinaryFromPath (0.00s)
    sealer_test.go:667: Rows=0（行数未知）时不应被行数交叉校验拒绝: tracearchive: 封块失败: 建临时产物失败: open /var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestSealAcceptsRowsZeroFromDiscoveryPath27534969/001/dt=2026-09-24/inst=blue/part-000000.jsonl.zst.tmp: no such file or directory
--- FAIL: TestSealAcceptsRowsZeroFromDiscoveryPath (0.00s)
    sealer_test.go:834: 空 spool 合法（row_count=0），不应被残行守卫拒绝: tracearchive: 封块失败: 建临时产物失败: open /var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestSealAcceptsEmptySpool1355685627/001/dt=2026-09-24/inst=blue/part-000000.jsonl.zst.tmp: no such file or directory
--- FAIL: TestSealAcceptsEmptySpool (0.00s)
```

**修正**：把建目录提到创建 `.tmp` 之前（`.tmp` 必须与产物同目录才能 rename，所以目录只能提前建）。修正后仅剩 2 条红（见 3.2/3.3）：

```text
    sealer_test.go:272: 错误链里应保留 exec.ErrNotFound（这要求用 %w 包装），实际 tracearchive: 封块失败: zstd 子进程失败（args=[-19 --long=29 -T1 -c]）: fork/exec /var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestSealFailsWhenZstdMissing3907937190/002/definitely-not-zstd: no such file or directory（stderr: ）
--- FAIL: TestSealFailsWhenZstdMissing (0.00s)
    sealer_test.go:794: 末尾不是换行符的 spool 必须拒绝封块（ErrSealFailed），实际 <nil>
    sealer_test.go:794: 末尾不是换行符的 spool 必须拒绝封块（ErrSealFailed），实际 <nil>
--- FAIL: TestSealRejectsTruncatedSpool (0.02s)
```

### 3.2 缺陷 B（真实缺陷）：`exec.ErrNotFound` 只在裸文件名时出现

简报的注释写「错误链里应保留 `exec.ErrNotFound`（这要求用 `%w` 包装）」，但实测（go1.26.1/darwin）：

```text
err=fork/exec /tmp/.../definitely-not-zstd: no such file or directory (*fs.PathError)
errors.Is(err, exec.ErrNotFound) = false
lookPathErr for bare name: exec: "definitely-not-zstd-xyz": executable file not found in $PATH | Is ErrNotFound=true
```

`exec.Command` **只对不含路径分隔符**的 name 走 `LookPath`；测试与生产部署都常用绝对路径，此时 `cmd.Run()` 返回 `*fs.PathError(ENOENT)`，`errors.Is(err, exec.ErrNotFound)` 恒为 false——**`%w` 换不来这个身份**。

**修正**（最小偏离）：在 `runZstd` 里把「文件不存在」这一支显式挂上 `exec.ErrNotFound`（用两个 `%w` 同时保留原始错误），调用方据此区分「zstd 没装/路径配错」的部署事故与「压缩本身失败」：

```go
if errors.Is(err, os.ErrNotExist) {
    return empty, fmt.Errorf("%w: 找不到 zstd 可执行文件 %q（部署事故，规格 §11.1）: %w: %w",
        ErrSealFailed, s.opts.ZstdBinary, exec.ErrNotFound, err)
}
```

我不改测试断言：你给的测试断言「错误链里能取到 `exec.ErrNotFound`」是**需求**，括号里的「只要 `%w`」才是（错误的）实现猜测；改实现满足了需求本身。

### 3.3 测试常量错误（与实现无关）：golden 自检索引越界

简报 golden 用例的自检写作 `bytes.Equal(sum[:8], want[32:40])`。但 footer 从 `len(want)-FooterSize = 13` 起算，帧摘要是 footer 内 `[22:30)` 字段，即整个产物的 `[35,43)`。`want[32:40]` 恰好偏移了 `len(trailer)=3` 字节，读到 `trailer_len`/`row_count` 与摘要前 5 字节的混合，**该自检恒不成立**。实测摘要无误：

```text
$ printf 'aa\nbbbb\nc\n' | shasum -a 256
e376d8f8ff1f11e2c98c823f3a3a4dffb7110601dcc04d48d86fcdbb439426b2
（want[35:43] = e3 76 d8 f8 ff 1f 11 e2 = 简报写死的字面量 ✓；want[32:40] = 00 00 03 e3 76 d8 f8 ff ✗）
```

**修正**：改用格式契约表达，不再手写魔数——`footerStart := len(want) - ta.FooterSize`，比较 `want[footerStart+22:footerStart+30]`。

### 3.4 按控制者指令新增的残行守卫（步骤 3 追加）

`scanSpoolOffsets` 增加「spool 必须以换行符结尾」的硬前置条件，判据是**「EOF 时仍带回了非空字节」**（0 字节空 spool 合法，不误判）。加守卫前，残行会被**静默烤进产物**：

```text
sealer_test.go:794: 末尾不是换行符的 spool 必须拒绝封块（ErrSealFailed），实际 <nil>
```

守卫的完整理由（含「目录扫描路径 Rows==0 无交叉校验可用」）写在 `scanSpoolOffsets` 的函数注释里，防止后人误删。位置选择：放在 `scanSpoolOffsets` 里意味着**在创建任何文件之前**就拒绝——「保留 spool、删除 `.tmp`、不产生产物」由构造保证，而不依赖后续清理。

---

## 4. 步骤 4：GREEN 原始输出

```bash
go test ./internal/tracearchive/ ./tests/tracearchive/ -run '<完整合并正则>' -v
```

```text
=== RUN   TestSealIsAtomicOnCrash
--- PASS: TestSealIsAtomicOnCrash (0.05s)
=== RUN   TestParsePartSeqUsedBySealer
--- PASS: TestParsePartSeqUsedBySealer (0.00s)
PASS
ok  	github.com/example/llm-gateway/internal/tracearchive	0.061s
=== RUN   TestSealProducesArtifactAndRemovesSpool
--- PASS: TestSealProducesArtifactAndRemovesSpool (0.06s)
=== RUN   TestSealRefusesToOverwriteExistingArtifact
--- PASS: TestSealRefusesToOverwriteExistingArtifact (0.01s)
=== RUN   TestSealRejectsRowCountMismatch
--- PASS: TestSealRejectsRowCountMismatch (0.00s)
=== RUN   TestSealFooterPointsAtValidTrailer
--- PASS: TestSealFooterPointsAtValidTrailer (0.04s)
=== RUN   TestSealFailsOnNonZeroExit
--- PASS: TestSealFailsOnNonZeroExit (0.01s)
=== RUN   TestSealFailsWhenZstdMissing
--- PASS: TestSealFailsWhenZstdMissing (0.00s)
=== RUN   TestSealGoldenLayout
--- PASS: TestSealGoldenLayout (0.01s)
=== RUN   TestListPendingSealsSkipsFreshFiles
--- PASS: TestListPendingSealsSkipsFreshFiles (0.00s)
=== RUN   TestListPendingSealsReturnsStaleFiles
--- PASS: TestListPendingSealsReturnsStaleFiles (0.00s)
=== RUN   TestListPendingSealsIgnoresIllegalNames
--- PASS: TestListPendingSealsIgnoresIllegalNames (0.00s)
=== RUN   TestSealInvokesZstdCLIWithArgs
=== RUN   TestSealInvokesZstdCLIWithArgs/默认级别与默认窗口
=== RUN   TestSealInvokesZstdCLIWithArgs/显式级别与显式窗口
--- PASS: TestSealInvokesZstdCLIWithArgs (0.02s)
    --- PASS: TestSealInvokesZstdCLIWithArgs/默认级别与默认窗口 (0.01s)
    --- PASS: TestSealInvokesZstdCLIWithArgs/显式级别与显式窗口 (0.01s)
=== RUN   TestSealDefaultsZstdBinaryFromPath
--- PASS: TestSealDefaultsZstdBinaryFromPath (0.01s)
=== RUN   TestNewSealerValidatesOptions
=== RUN   TestNewSealerValidatesOptions/空_Root_被拒绝
=== RUN   TestNewSealerValidatesOptions/非_2_的幂窗口被拒绝
--- PASS: TestNewSealerValidatesOptions (0.00s)
    --- PASS: TestNewSealerValidatesOptions/空_Root_被拒绝 (0.00s)
    --- PASS: TestNewSealerValidatesOptions/非_2_的幂窗口被拒绝 (0.00s)
=== RUN   TestSealRejectsUnknownArtifactState
--- PASS: TestSealRejectsUnknownArtifactState (0.00s)
=== RUN   TestSealFailsWhenSpoolMissing
--- PASS: TestSealFailsWhenSpoolMissing (0.00s)
=== RUN   TestSealFailsWhenSpoolIsDirectory
--- PASS: TestSealFailsWhenSpoolIsDirectory (0.00s)
=== RUN   TestSealAcceptsRowsZeroFromDiscoveryPath
--- PASS: TestSealAcceptsRowsZeroFromDiscoveryPath (0.04s)
=== RUN   TestListPendingSealsHandlesSpoolRootErrors
=== RUN   TestListPendingSealsHandlesSpoolRootErrors/spool_根目录不存在返回空而非报错
=== RUN   TestListPendingSealsHandlesSpoolRootErrors/spool_根目录读失败必须报错
--- PASS: TestListPendingSealsHandlesSpoolRootErrors (0.00s)
    --- PASS: TestListPendingSealsHandlesSpoolRootErrors/spool_根目录不存在返回空而非报错 (0.00s)
    --- PASS: TestListPendingSealsHandlesSpoolRootErrors/spool_根目录读失败必须报错 (0.00s)
=== RUN   TestListPendingSealsIgnoresNonLayoutEntries
--- PASS: TestListPendingSealsIgnoresNonLayoutEntries (0.00s)
=== RUN   TestSealRejectsTruncatedSpool
=== RUN   TestSealRejectsTruncatedSpool/目录扫描路径（Rows_未知，无交叉校验可用）
=== RUN   TestSealRejectsTruncatedSpool/行数已知路径（纵深防御，交叉校验本身也拦不住残行）
--- PASS: TestSealRejectsTruncatedSpool (0.00s)
    --- PASS: TestSealRejectsTruncatedSpool/目录扫描路径（Rows_未知，无交叉校验可用） (0.00s)
    --- PASS: TestSealRejectsTruncatedSpool/行数已知路径（纵深防御，交叉校验本身也拦不住残行） (0.00s)
=== RUN   TestSealAcceptsEmptySpool
--- PASS: TestSealAcceptsEmptySpool (0.01s)
=== RUN   TestSealerListPendingSealsUsesConfiguredMinAge
--- PASS: TestSealerListPendingSealsUsesConfiguredMinAge (0.00s)
PASS
ok  	github.com/example/llm-gateway/tests/tracearchive	0.228s
```

关键点：`SKIP` 计数 = 0，4 条依赖**真实 zstd CLI** 的用例（`TestSealProducesArtifactAndRemovesSpool`、`TestSealFooterPointsAtValidTrailer`、`TestSealIsAtomicOnCrash`、`TestSealAcceptsRowsZeroFromDiscoveryPath`）都是 `--- PASS` 而非跳过。

提交前的硬约束命令（在 `HEAD + 本任务 5 个文件` 的隔离基线上跑，避免被同工作树里别人的在制品干扰）：

```text
$ gofmt -l internal/tracearchive/sealer.go internal/tracearchive/sealer_internal_test.go \
          internal/tracearchive/testhelpers_test.go tests/tracearchive/sealer_test.go tests/tracearchive/helpers_test.go
（空）
$ go vet ./internal/... ./cmd/...      → OK
$ go build ./internal/... ./cmd/...    → OK
$ go test ./internal/tracearchive/ ./tests/tracearchive/ -count=1   （提交内容 git archive 后）
ok  github.com/example/llm-gateway/internal/tracearchive
ok  github.com/example/llm-gateway/tests/tracearchive
$ grep -n "zstd.NewWriter" internal/tracearchive/sealer.go
（无输出）
```

> `gofmt -l internal/ cmd/` 全仓库有 **154** 个既有违规文件（与本任务无关），`internal/tracearchive` 一个都不在里面。

### 4.1 额外验证（不在测试里，一次性）

真 CLI 产出的帧能否被**读侧**（`klauspost/compress`，即本包解码路径）解回来——这是任务 7 的职责，但成本极低，故顺手验证（3 MiB 输入、窗口 1 MiB 以让帧头真的声明窗口）：

```text
输入 2224000 字节 → 帧 242 字节（9190.1x），trailer 7999 字节，row_count=4000
Go 解码器解回字节数: 2224000 与输入一致: true
trailer 首字节（第 0 行偏移的 varint）= 0
```

**局限**：这只证明「`--long` 帧的解码机制可用」，**没有**验证生产档 `--long=29`/512 MiB 窗口与读侧 max-window 的匹配（需要 >512 MiB 输入才能让帧头声明该窗口），见 §9。

---

## 5. 变异自检

方法：对实现逐条做**删除/弱化变异**，跑**同一套合并正则**（两个包），看是否有用例转红。基线 = 提交内容（`HEAD + 5 个文件`，隔离于同工作树的在制品）。

### 5.1 完整变异表

| 变异 | 结果 | 杀死的用例 |
|---|---|---|
| m01_不调用配置的二进制 | 红 | TestSealDefaultsZstdBinaryFromPath, TestSealFailsOnNonZeroExit, TestSealFailsWhenZstdMissing, TestSealGoldenLayout, TestSealInvokesZstdCLIWithArgs |
| m02_退回Go库编码器 | 红 | TestSealAcceptsEmptySpool, TestSealDefaultsZstdBinaryFromPath, TestSealFailsOnNonZeroExit, TestSealFailsWhenZstdMissing, TestSealGoldenLayout, TestSealInvokesZstdCLIWithArgs |
| m03_删除产物存在检查 | 红 | TestSealRefusesToOverwriteExistingArtifact, TestSealRejectsUnknownArtifactState |
| m04_删除无法判断分支 | 红 | TestSealRejectsUnknownArtifactState |
| m05_ErrPartExists改v | 红 | TestSealRefusesToOverwriteExistingArtifact |
| m06_删除Rows等于0的豁免 | 红 | TestSealAcceptsRowsZeroFromDiscoveryPath |
| m07_放宽为只拦多于 | 红 | TestSealRejectsRowCountMismatch |
| m08_ErrRowCountMismatch改v | 红 | TestSealRejectsRowCountMismatch |
| m09_删除残行守卫 | 红 | TestSealRejectsTruncatedSpool |
| m10_删除zstd失败时的tmp清理 | 红 | TestSealFailsOnNonZeroExit |
| m11_删除建目录 | 红 | TestSealAcceptsEmptySpool, TestSealAcceptsRowsZeroFromDiscoveryPath, TestSealDefaultsZstdBinaryFromPath, TestSealFailsOnNonZeroExit, TestSealFailsWhenZstdMissing, TestSealFooterPointsAtValidTrailer, TestSealGoldenLayout, TestSealInvokesZstdCLIWithArgs, TestSealIsAtomicOnCrash, TestSealProducesArtifactAndRemovesSpool |
| m12_ErrSealFailed改v | 红 | TestSealFailsOnNonZeroExit |
| m13_删除ErrNotFound归一化 | 红 | TestSealFailsWhenZstdMissing |
| m14_删除failBeforeRename钩子 | 红 | TestSealIsAtomicOnCrash |
| m15_删除打开spool失败守卫 | **绿（零覆盖！）** | — |
| m16_删除读spool失败守卫 | 红(超时/挂死) | 超时 |
| m17_trailer先于帧 | 红 | TestSealAcceptsRowsZeroFromDiscoveryPath, TestSealFooterPointsAtValidTrailer, TestSealGoldenLayout |
| m18_删除Root为空守卫 | 红 | TestNewSealerValidatesOptions |
| m19_删除ZstdBinary默认值 | 红 | TestSealDefaultsZstdBinaryFromPath |
| m20_删除ZstdLevel默认值 | 红 | TestSealInvokesZstdCLIWithArgs |
| m21_删除ZstdWindowBytes默认值 | 红 | TestSealIsAtomicOnCrash, TestSealProducesArtifactAndRemovesSpool, TestSealRefusesToOverwriteExistingArtifact |
| m22_删除MinAge默认值 | 红 | TestSealerListPendingSealsUsesConfiguredMinAge |
| m23_删除窗口2的幂校验 | 红 | TestNewSealerValidatesOptions |
| m24_删除ctx取消分支 | **绿（零覆盖！）** | — |
| m25_删除根目录不存在分支 | 红 | TestListPendingSealsHandlesSpoolRootErrors |
| m26_删除根目录读失败上抛 | 红 | TestListPendingSealsHandlesSpoolRootErrors |
| m27_删除dt前缀过滤 | 红 | TestListPendingSealsIgnoresNonLayoutEntries |
| m28_删除dt的IsDir过滤 | 红 | TestListPendingSealsIgnoresNonLayoutEntries |
| m29_删除inst前缀过滤 | 红 | TestListPendingSealsIgnoresNonLayoutEntries |
| m30_删除inst的IsDir过滤 | 红 | TestListPendingSealsIgnoresNonLayoutEntries |
| m31_删除jsonl后缀过滤 | 红 | TestListPendingSealsIgnoresIllegalNames |
| m32_删除parsePartSeq过滤 | 红 | TestListPendingSealsIgnoresIllegalNames |
| m33_删除mtime门槛 | 红 | TestListPendingSealsSkipsFreshFiles, TestSealerListPendingSealsUsesConfiguredMinAge |
| m34_删除sort顺序 | **绿（零覆盖！）** | — |

（脚本自检：还原校验: 一致 —— 每条变异后文件都被完整还原）

### 5.2 每条变异的执行方式（可复现）

```bash
# 变异脚本（临时，未入库）：/tmp/seal_mutate.py —— 精确子串替换/区间删除，
# 每条变异后跑 go test … -run '<合并正则>' -count=1 -timeout 30s，try/finally 还原
python3 /tmp/seal_mutate.py            # 全量
python3 /tmp/seal_mutate.py --check    # 只校验每条变异能唯一定位
```

两侧命令与输出均为上表的直接来源；`m16` 的「红」来自 `-timeout`（删掉守卫后非 EOF 的读错误被当成 EOF 反复重试 → 无限循环 → 挂死），其余为断言失败。`m02` 不是删除而是**完整改写**（把 `os/exec` 真的换成 `klauspost/compress/zstd.NewWriter`），它是约束 1（「不得退回 Go 库」）的直接反证。脚本已留档：`.superpowers/sdd/2026-09-24-tracearchive-storage-primitives/task-6-mutate.py`（与报告同为未跟踪的流程产物）。

### 5.3 三条零覆盖守卫（如实报告）

| 守卫 | 位置 | 删除后 | 为什么无法被区分 | 保留理由 |
|---|---|---|---|---|
| `runZstd` 的「打开 spool 失败」守卫 | `sealer.go:232-235` | **全绿** | **公开 API 不可达**：`ScanSpoolOffsets` 已在同一路径上成功打开过该文件，随后立即关闭；只有在「扫描成功之后、`runZstd` 再次打开之前」spool 被删除的 **TOCTOU 窗口**内才可达。唯一的测试钩子是 `failBeforeRename`，位置在更后面，无法制造这个窗口。 | 该窗口内若没有这条守卫，`cmd.Stdin` 会拿到 nil 文件句柄，CLI 读到 EOF，于是**产出一个空帧配上正确索引的产物**（静默损坏）；有它则变成响亮报错。删除守卫是负收益。 |
| `Seal` 的 `ctx.Err()` 分支 | `sealer.go:253-255` | **全绿** | **仅文案不同**：`ctx` 已取消时 `cmd.Run()` 本身就返回 `context.Canceled`，所以有无这个分支，错误链里都同时有 `ErrSealFailed` 与 `context.Canceled`（`errors.Is` 断言两者都成立）。唯一区分点是消息文本（「封块上下文取消」vs「zstd 子进程失败」），而按消息文本断言属于断言日志字符串，我没有加。 | 运维需要区分「我们主动取消（停机/超时）」与「zstd 自身失败」——两者都要告警，但处置不同。 |
| `ListPendingSeals` 的 `sort.Slice` | `sealer.go:415-424` | **全绿** | **与 `os.ReadDir` 的排序完全等价**：`os.ReadDir` 文档保证按文件名排序，而目录名 `dt=`/`inst=` 的字典序就是 `DT`/`Inst` 的字典序、`part-%06d` 补零后字典序就是数值序。因此显式排序不改变任何可观测输出。 | 让「稳定顺序」成为**本函数的契约**，而不是依赖 `ReadDir` 的实现细节（若将来块名不再补零、或目录名引入非 ASCII，等价性即失效）。 |

### 5.4 未做删除变异的守卫（防御性分支，说明为何不可覆盖）

这些分支的删除不会让任何用例转红，我**没有**为它们编造用例；逐条给出不可覆盖的原因：

| 位置 | 分支 | 为何不可覆盖 |
|---|---|---|
| `sealer.go:227-230` | `runZstd` 内第二次 `zstdLongArg` 的错误上抛 | 不可达：同一字段已在 `NewSealer` 校验过且此后不再变更（包外无法构造 `Sealer`）。保留为构造函数之外的第二道保险。 |
| `sealer.go:239-242` | 建 `.tmp` 失败 | 需要 ENOSPC/EMFILE 或只读产物目录（root 下不成立）。ENOSPC 恰是本子系统存在的理由，值得补一条注入式测试（见 §8 缺口 4）。 |
| `sealer.go:273-275` | `dst.Seek` 失败 | 常规文件上不可达。 |
| `sealer.go:277-279` | `io.Copy(hasher, dst)` 失败 | 需要常规文件读错误。 |
| `sealer.go:292-295` / `298-301` | `appendTrailerAndFooter` 打开/`Stat` 失败 | `.tmp` 刚由本函数上游创建，不可达。 |
| `sealer.go:304-306` / `313-315` / `316-318` | 写 trailer / 写 footer / `Sync` 失败 | 需要 ENOSPC（同样是「本子系统要解决的问题」本身）。见 §8 缺口 4。 |
| `sealer.go:121-124` | 追加 trailer/footer 失败后的 `.tmp` 清理 | 需要上面那组 ENOSPC 之一。 |
| `sealer.go:134-137` | `os.Rename` 失败 | 有存在性守卫在，`partPath` 存在即返回 `ErrPartExists`；不存在则同文件系统 rename 成功。只剩竞态可达。 |
| `sealer.go:138-140` / `fsyncDir:324-329` | `fsyncDir` 打开/`Sync` 失败 | 需要异常文件系统或不支持 fsync 的挂载点。 |
| `sealer.go:143-145` | 删 spool 失败 | 需要「不可 unlink 的文件」：macOS 可用 `chflags uchg`（连 root 也拦），但 Linux 需要 `chattr +i`（容器内常无 `CAP_LINUX_IMMUTABLE`），做出来会变成平台相关的静默跳过。见 §8 缺口 4。 |
| `sealer.go:373-375` / `384-386` | `ListPendingSeals` 读 dt 目录 / inst 目录失败 | 不可达：非目录条目已被 `IsDir()` 过滤，是目录则可读（除权限，root 下不成立）。 |
| `sealer.go:397-400` | `e.Info()` 失败 | TOCTOU：条目在 `ReadDir` 与 `Info` 之间被删。 |

### 5.5 简报要求的 grep 检查

```bash
$ grep -n "zstd.NewWriter" internal/tracearchive/sealer.go
（无输出 = sealer 未使用 Go 库编码器）
```

（`internal/tracearchive/calib_test.go` 里仍有 `zstd.NewWriter`，那是任务 1 的标定测试，属既有产物，未改动；grep 必须限定在 `sealer.go`。）

---

## 6. 对简报的偏离

| # | 偏离 | 类型 | 理由与证据 |
|---|---|---|---|
| 1 | B 层测试补 `"bytes"` import | 机械修正 | 简报的 import 块漏了 `bytes`，而 golden 用例用了 `bytes.Equal`（`writeSpool` 的 `bytes` 在另一个文件里）。照抄会编译失败。 |
| 2 | golden 自检索引 `want[32:40]` → `want[footerStart+22:footerStart+30]` | 修正简报的**测试常量错误** | §3.3（越界 3 字节 = trailer 长度，自检恒不成立）。 |
| 3 | `MkdirAll` 提到创建 `.tmp` 之前 | 修正简报的**实现缺陷** | §3.1（否则 10 条真封块用例全部 ENOENT）。 |
| 4 | 子进程「文件不存在」归一化到 `exec.ErrNotFound` | 修正简报的**实现缺陷** | §3.2。测试断言保持原样。 |
| 5 | 新增 `scanSpoolOffsets` 的「末尾必须换行」守卫 | 按控制者追加要求 | §3.4；错误用既有哨兵 `ErrSealFailed`（未新增哨兵）。 |
| 6 | `SealerOptions` 新增 `MinAge time.Duration`（默认 `defaultSealInterval`）+ `func (s *Sealer) ListPendingSeals()` | 按控制者追加要求 | 见 §6.1。 |
| 7 | B 层新增 12 条「守卫覆盖」用例 | 变异自检要求 | 简报给了用于守卫的用例集后，逐条删除变异发现多条守卫零覆盖，故补：`TestSealInvokesZstdCLIWithArgs`、`TestSealDefaultsZstdBinaryFromPath`、`TestNewSealerValidatesOptions`、`TestSealRejectsUnknownArtifactState`、`TestSealFailsWhenSpoolMissing`、`TestSealFailsWhenSpoolIsDirectory`、`TestSealAcceptsRowsZeroFromDiscoveryPath`、`TestListPendingSealsHandlesSpoolRootErrors`、`TestListPendingSealsIgnoresNonLayoutEntries`、`TestSealRejectsTruncatedSpool`、`TestSealAcceptsEmptySpool`、`TestSealerListPendingSealsUsesConfiguredMinAge`。每条都在注释里写明「它区分哪条守卫」；新增段在文件里有分隔注释标明系简报之外的补充。 |
| 8 | `ListPendingSeals` 文档补 minAge 依据 | 按控制者追加要求 | 写明「minAge 必须 > 网关的 part 滚动间隔；本进程无法区分『已停止追加』与『暂时空闲』，这个时间差是唯一的保护」，并保留原「必须远大于 flush 周期」的说明。 |

### 6.1 关于 `MinAge` 的落地方式（需要你知道的设计选择）

你要求「minAge 作为显式配置项（`SealerOptions` 里可设），不要写死成常量，也不要默认等于 0」。包级 `ListPendingSeals(root, minAge)` 本来就是显式参数，但只有「配置项 + 有个消费者」才算真的落地，否则 `SealerOptions.MinAge` 会是一个**零覆盖的死字段**（删除它不会有任何用例转红，违反本任务的判据）。因此我：
- 在 `SealerOptions` 加 `MinAge`，`NewSealer` 里 `<= 0 → defaultSealInterval`（15 分钟，既有常量，非我发明）；
- 加 `func (s *Sealer) ListPendingSeals() ([]SealTask, error)` 委托给包级函数，用 `s.opts.MinAge`。

这样 sealer 循环（计划 4/8）构造一次 `Sealer` 就能「发现 + 封块」，不必在两处各自维护同一组参数。删除 `MinAge` 默认值或断掉委托都有用例转红（`m22`、`TestSealerListPendingSealsUsesConfiguredMinAge`）。

**这属于新增导出 API**（简报的接口清单里没有这个方法），如果你认为接口必须冻结，我可以改为「sealer 循环自己调包级 `ListPendingSeals(root, opts.MinAge)`」并把字段标注为待消费者——那样字段会变成死代码，需要你裁决。

---

## 7. 待裁决项（均已被 §10 的裁决解决：1 → §10.1，2 → §10.2）

1. **minAge 默认值与「必须严格大于滚动间隔」的张力**（你要求登记，我没有自行发明数值）：规格取 `minAge = seal_interval`（默认 15 分钟），我照此用 `defaultSealInterval`；但 D7 的表述是「minAge 必须**严格大于**网关的 part 滚动间隔」。若网关滚动间隔也取 `seal_interval`，则默认值是**等于**而非严格大于，边界上有竞态（网关在 `T+15m` 滚动、sealer 在 `T+15m` 以 `cutoff=T` 判定，可能同时发生）。请裁决：滚动间隔取更小值（如 10 分钟）、还是 minAge 取更大值（如 20 分钟）。在裁决前我保持规格值不动，并在代码注释里写清了依据与约束。
2. **`SealerOptions.MinAge` + 方法是否可留在公开接口**（§6.1）。

---

## 8. 超出本任务范围、供记账的缺口

1. **网关侧 part 滚动（D7）尚未实现**：`Writer.MarkSealed` 已存在，但没有任何代码按年龄/大小周期调用它。minAge 保护成立的前提是「网关会自己停止往旧块追加」；该前提未落地前，minAge 对「网关仍持 fd 的块」保护是空的。
2. **独立 sealer 进程与循环（计划 4/8）不存在**：本任务只交付封块原语。`ListPendingSeals → Seal` 的循环、`MarkSealed` 回调、systemd 单元、失败告警、退避重试都还没有。注意 `Seal` 成功后**没有**回调写入器，所以「网关不知道自己写的块已被封掉并删除」这个竞态在集成层仍然存在。
3. **zstd 版本锁定只有注释**：没有运行时校验（例如启动时 `zstd --version` 必须等于生产标定版本 v1.4.8）。本机是 v1.5.7，生产是 v1.4.8；换版本必须重新标定压缩比（规格 §11.1），但没有任何代码阻止版本漂移。
4. **ENOSPC 路径没有任何测试**：`.tmp` 写一半、`fsync` 失败、删 spool 失败都属于「本子系统要解决的问题本身」，却因为需要注入磁盘错误而全部零覆盖。建议后续给 `Sealer` 加一个 `io/fs` 故障注入点（比照 `failBeforeRename`），专门覆盖这一组分支。
5. **产物帧从未被读侧解压**：本任务的真 zstd 用例只验证「布局/trailer 正确」，不解压帧；golden 用 `cat` 直通也不校验帧。真正「产物能被 klauspost 解码 + 窗口不超过 `WithDecoderMaxWindow`」是任务 7 的事，建议在任务 7 里补一条端到端（`Seal` → `Reader` 读回正文逐字节相等）。
6. **footer 的 `frame_sha256[:8]` 只写不读**：没有任何自检在封块后重读产物校验该字段（可考虑在 sealer 循环里做一次廉价校验，用于发现半写/位腐）。
7. **`ListPendingSeals` 不校验 `dt`/`inst` 的取值**：只按目录名前缀收集，`dt=` 不校验是否合法日期、`inst=` 不校验是否 ∈ {blue, green}——与写入器侧严格的 `validateDT` + 白名单**不对称**。任何被塞进 spool 树的目录都会被当作待封块（会把 `SealTask.SpoolPath` 指向目录外的任意路径）。当前风险低（spool 树由写入器独占），但冷层工具或人工操作可能造出这种目录。
8. **目录 fsync 不完整**：rename 后只 fsync 了产物目录本身；若该 `dt=`/`inst=` 目录是本轮新建的，其父目录未 fsync，崩溃后可能丢目录项。严格做法是逐级 fsync（或对新建链路都做）。低风险，记录在案。
9. **CI 镜像必须装 C 版 zstd**：否则 4 条真封块用例静默 `t.Skip`，「产物是否正确」就没有任何自动化证据（测试里已用醒目注释警告）。
10. **同工作树里的在制品与我的提交无关，但会影响整包运行**：`internal/tracearchive/writer.go` + 新增的 `internal/tracearchive/writer_internal_test.go`（Task 5 评审的在制品）在我提交时处于**未完成**状态，`go test ./internal/tracearchive/` 整包会因 `TestWriterWriteFailureLeavesNoResidualLine`、`TestWriterPoisonedBlockCannotBeSealed`、`TestWriterQuarantineRenameFailureEmptiesSpool` 三条红。我的 24 条用例全绿；`go vet ./internal/... ./cmd/...` 与 `go build ./internal/... ./cmd/...` 在我提交前后均已通过（期间该文件一度不编译，我因此把「提交前硬约束命令」在隔离基线上执行，见 §4）。

---

## 9. 我不确定 / 没能验证的地方

1. **压缩比验收线未重测**：本机 zstd v1.5.7，生产 v1.4.8；本次标定（10.26x）来自 v1.4.8。我**没有**测量 CLI 与 Go 库的产物比（需要真实大样本）。本任务能保证的是「走的是 CLI、参数就是 `-19 --long=29 -T1 -c`」（`m01`/`m02`/参数用例），**不能**保证 v1.5.7 与 v1.4.8 压缩比一致。
2. **`--long=29`/512 MiB 窗口与读侧 max-window 的匹配未验证**（§4.1）：需要 >512 MiB 的输入才能让帧头声明该窗口，成本过高；我只验证了 1 MiB 窗口档的解码机制可用。
3. **`-3` 级别只在假 zstd 下被断言过**：参数用例验证「级别值被正确拼进命令」，但不证明真 zstd 接受 `-3`（默认档 `-19` 由其他真 zstd 用例覆盖）。
4. **`m16` 的「红」是超时而非断言失败**：删掉「读 spool 失败」守卫后用例挂死（无限循环），我用 `-timeout 30s` 判定为红。这在语义上确实是缺陷，但比断言失败弱一档，特此标注。
5. **三条零覆盖守卫**（`m15`/`m24`/`m34`）：我认为 5.3 的理由成立，但它们确实是「没被测到的代码」，请按你的判据复核。
6. **生产部署形态未验证**：`ZstdBinary` 的实际路径、systemd 单元里的工作目录与权限、spool/产物是否同一文件系统（rename 需要）都没有在真实宿主机上跑过。我的测试全部在 `t.TempDir()` 上。
7. **跨平台**：`TestSealRejectsUnknownArtifactState` 依赖自引用符号链接（不支持则 `t.Skip`）；`TestSealFailsWhenSpoolIsDirectory` 依赖「读目录返回 EISDIR」，macOS/Linux 成立但未在其他 unix 上验证。本机开发环境是 macOS/arm64，CI 的 Linux 行为我没有实机跑过。

---

# 10. 评审裁决落实（第二轮，提交 `e0b0084f`）

> 本轮按控制者三条裁决改动，**未 amend `76042bcd`**，是独立提交。
> 新增/改动用例后共 **29 条**（原 24 条 + 5 条），变异自检扩到 **44 条**。

## 10.1 裁决 1：`minAge` 默认值 = 3 小时

**落实**

- 新增常量 `defaultSealMinAge = 3 * time.Hour`（`internal/tracearchive/sealer.go`），
  注释里写全了推导链：37 GiB/天 ≈ 1.54 GiB/h → `defaultBlockBytes`(1536 MiB) 满流量
  ≈ 1 小时填满 → 网关滚动间隔应取 ~1h（让**大小**阈值先触发以保住已标定的 10.26x
  压缩比，过早按时间截断会掉到未标定的 ~400 MB 档）→ minAge 必须**严格大于**滚动间隔
  ⇒ 3 × 1h，留 3 倍余量。成本核算也写进注释：未封块 spool ≈ 3h × 1.54 GiB/h ≈ 4.6 GiB，
  相对 `/datalog` 240 GiB 可忽略。
- `SealerOptions.MinAge` 保留且**确实被消费**（`(*Sealer).ListPendingSeals()` 用的就是
  它），默认值不再等于 0、也不再等于 `defaultSealInterval`。
- **登记对端约束**：网关滚动间隔必须 ≤ 1h，属**计划 2（网关接入）**落地，本任务只负责
  minAge 侧。现有 `defaultSealInterval = 15m` 已满足该约束（3h 对它有 12 倍余量），
  因此本任务没有改动 `defaults.go`（那是任务 1 的产物，未触碰）。

**用例**：`TestSealerListPendingSealsUsesConfiguredMinAge` 增加第三条断言——
2 小时前的 spool 必须被跳过、4 小时前的必须被发现。它同时钉住「默认值不是 15 分钟」
和「默认值没有大到不可用」。

**双侧原始输出**（`m36_minAge默认值退回15分钟`：把 `defaultSealMinAge` 改回
`defaultSealInterval`）：

```text
[变异侧]
--- FAIL: TestSealerListPendingSealsUsesConfiguredMinAge (0.00s)
    sealer_test.go:940: 默认 minAge=3h 时应且仅应发现 4 小时前的块（2 小时前的仍可能在被追加），实际 [.../part-000001.jsonl .../part-000002.jsonl]
[原始侧]
ok  github.com/example/llm-gateway/internal/tracearchive
ok  github.com/example/llm-gateway/tests/tracearchive
```

## 10.2 裁决 2：只保留一个发现 API —— 选「只保留方法」

**落实**：删除了包级 `ListPendingSeals(root, minAge)`，只留
`(*Sealer).ListPendingSeals()`（原方法体内的实现，直接用 `s.opts.Root` 与
`s.opts.MinAge`）。B 层测试全部改为经方法调用（`helpers_test.go` 里新增
`listPendingSeals(t, root, minAge)` 辅助，构造 Sealer 后调方法）。

**选择理由**（与你的「优先」一致，补充一点实测理由）：包级签名允许 root 与 minAge
来自不同来源；方法形式只可能拿到同一份 `SealerOptions`。另外方法形式天然带上
`ZstdBinary`/`ExpectedZstdVersion`，为计划 4 的 sealer 循环省掉「两处各自维护同一组
参数」的隐患——循环构造一次 Sealer 即可发现并封块。

**登记为简报偏离**：计划/简报里登记的是包级 `ListPendingSeals(root, minAge)`，
现在只有方法形式。A 层辅助文件里也放了同名 `listPendingSeals`（当前 A 层无发现类
用例，属未使用函数，与既有的 `fakeZstd` 情况相同，两边保持一致是刻意的）。

## 10.3 裁决 3a：zstd 版本校验

**设计选择：探测放在「首次封块」而不是构造期**，依据三条：

1. 发现路径 `ListPendingSeals` 是**纯文件系统**操作，不该要求一个可用的 zstd 二进制。
   构造期探测会让「只做发现」的进程也依赖编码器版本，且我现有的发现类用例（用
   `SealerOptions{Root: root}` 构造、不传 zstd）会全部因版本不符而红。
2. 版本校验失效的对象就是**封块产物**，放在 `Seal` 入口正好覆盖它；同时「二进制缺失」
   仍与压缩失败同处 `ErrSealFailed` + `exec.ErrNotFound` 一类，不产生第二个报错点。
3. 结果记忆在实例上（`s.versionOK`），是真正的「一次」。

**失败语义**：一律用既有哨兵 `ErrSealFailed`（未新增哨兵），并且都**响亮失败**、
不静默通过：① 二进制缺失 → 归一化到 `exec.ErrNotFound`（部署事故可被调用方区分）；
② 探测进程非 0 退出 → 原样包装（保留退出码）；③ 输出无法解析 → 拒绝（「没能确认版本」
与「版本不符」同等对待）；④ 版本不符 → 拒绝，错误里带上期望值与实际值。
期望版本可配置：`SealerOptions.ExpectedZstdVersion`（空则取
`defaultZstdVersion = "1.4.8"`，即标定所用的生产版本）；注释写明**标定是版本特定的，
换版本必须重新标定**，并点名本机 v1.5.7 与生产 v1.4.8 的差异。

**实现过程中的一次真实红→绿（值得记录）**：我最初把探测放在 `Seal` 的最前面，
结果简报的 `TestSealRefusesToOverwriteExistingArtifact` 与
`TestSealRejectsRowCountMismatch` 转红——这两条用例明确写着「不需要 zstd：检查发生在
压缩之前」。也就是说**不依赖编码器的判定必须先于编码器校验**，否则一个配错的编码器
会掩盖「产物已存在」「行数不符」这两类真实数据问题，运维看到的根因就是错的。
最终位置：产物存在性检查 + 行数交叉校验**之后**、创建任何文件（含 `.tmp`）**之前**，
代码里写了这段理由。

**新增用例（5 条）**：`TestSealRejectsMismatchedZstdVersion`（版本不符）、
`TestSealRejectsUnparsableZstdVersion`（输出无法解析）、
`TestSealRejectsFailedVersionProbe`（探测进程非 0 退出，并断言退出码被保留）、
`TestSealAcceptsConfiguredZstdVersion`（期望版本可配置：显式传一致版本必须放行——
它专杀「比对写死成默认常量」）、以及既有的 `TestSealFailsWhenZstdMissing`（二进制缺失）
现在实际覆盖的是探针的归一化分支。

## 10.4 裁决 3b：`ListPendingSeals` 校验 `dt`/`inst`

**落实**：`dt` 用既有的 `validateDT`（拒绝未补零、非 YYYY-MM-DD、非法日期如 2026-02-30），
`inst` 用既有的白名单 `instBlue`/`instGreen`；两者都从**目录名**取值，非法即
**`continue` 跳过该目录**（不报错、不中断整轮扫描），与写入侧 `Writer` 的严格校验对称。
未新增哨兵。

**用例**：`TestListPendingSealsRejectsMalformedDirNames`，7 个畸形目录
（`dt=2026-9-24`、`dt=20260924`、`dt=..`、`dt=2026-02-30`、`inst=bluex`、`inst=..`）
+ 1 个合法目录，断言「只返回合法那一个」且「不报错」。`dt=..` / `inst=..` 正是会被
`filepath.Join` 清理语义带出 spool 子树的输入（与任务 4 评审同一个洞）。

**双侧原始输出**（`m42_删除dt目录名校验`）：

```text
[变异侧]
--- FAIL: TestListPendingSealsRejectsMalformedDirNames (0.00s)
    sealer_test.go:1072: 只应返回合法目录下的 spool，实际 7: [{DT:.. Inst:blue Part:3 ...} {DT:2026-02-30 ...} ...]
[原始侧]
ok  github.com/example/llm-gateway/internal/tracearchive
ok  github.com/example/llm-gateway/tests/tracearchive
```

## 10.5 本轮 RED → GREEN 原始输出

**RED（第一轮：只加了字段/默认值，守卫未实现）**——两个包都是编译失败
（`unknown field ExpectedZstdVersion`），与首轮的 RED 同性质：

```text
# github.com/example/llm-gateway/internal/tracearchive [github.com/example/llm-gateway/internal/tracearchive.test]
internal/tracearchive/sealer_internal_test.go:25:63: unknown field ExpectedZstdVersion in struct literal of type SealerOptions
# github.com/example/llm-gateway/tests/tracearchive_test [github.com/example/llm-gateway/tests/tracearchive.test]
tests/tracearchive/sealer_test.go:34:71: unknown field ExpectedZstdVersion in struct literal of type tracearchive.SealerOptions
tests/tracearchive/sealer_test.go:167:69: unknown field ExpectedZstdVersion in struct literal of type tracearchive.SealerOptions
tests/tracearchive/sealer_test.go:662:71: unknown field ExpectedZstdVersion in struct literal of type tracearchive.SealerOptions
tests/tracearchive/sealer_test.go:1017:32: unknown field ExpectedZstdVersion in struct literal of type tracearchive.SealerOptions
FAIL	github.com/example/llm-gateway/internal/tracearchive [build failed]
FAIL	github.com/example/llm-gateway/tests/tracearchive [build failed]
FAIL
```

**RED（第二轮：字段已接线，守卫仍未实现）**——行为级失败，正是四条新守卫缺席的证据：

```text
    sealer_test.go:931: 默认 minAge=3h 时应且仅应发现 4 小时前的块（2 小时前的仍可能在被追加），实际 [/var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestSealerListPendingSealsUsesConfiguredMinAge4266568091/002/spool/dt=2026-09-24/inst=blue/part-000001.jsonl /var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestSealerListPendingSealsUsesConfiguredMinAge4266568091/002/spool/dt=2026-09-24/inst=blue/part-000002.jsonl]
--- FAIL: TestSealerListPendingSealsUsesConfiguredMinAge (0.00s)
    sealer_test.go:964: zstd 版本不符必须失败（ErrSealFailed），实际 <nil>
--- FAIL: TestSealRejectsMismatchedZstdVersion (0.01s)
    sealer_test.go:1000: 无法解析 zstd 版本时必须失败（ErrSealFailed），实际 <nil>
--- FAIL: TestSealRejectsUnparsableZstdVersion (0.01s)
    sealer_test.go:1072: 只应返回合法目录下的 spool，实际 7: [{DT:.. Inst:blue Part:3 SpoolPath:/var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestListPendingSealsRejectsMalformedDirNames1828152286/001/spool/dt=../inst=blue/part-000003.jsonl Rows:0 UncompressedBytes:2} {DT:2026-02-30 Inst:blue Part:4 SpoolPath:/var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestListPendingSealsRejectsMalformedDirNames1828152286/001/spool/dt=2026-02-30/inst=blue/part-000004.jsonl Rows:0 UncompressedBytes:2} {DT:2026-09-24 Inst:.. Part:6 SpoolPath:/var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestListPendingSealsRejectsMalformedDirNames1828152286/001/spool/dt=2026-09-24/inst=../part-000006.jsonl Rows:0 UncompressedBytes:2} {DT:2026-09-24 Inst:blue Part:7 SpoolPath:/var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestListPendingSealsRejectsMalformedDirNames1828152286/001/spool/dt=2026-09-24/inst=blue/part-000007.jsonl Rows:0 UncompressedBytes:2} {DT:2026-09-24 Inst:bluex Part:5 SpoolPath:/var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestListPendingSealsRejectsMalformedDirNames1828152286/001/spool/dt=2026-09-24/inst=bluex/part-000005.jsonl Rows:0 UncompressedBytes:2} {DT:2026-9-24 Inst:blue Part:1 SpoolPath:/var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestListPendingSealsRejectsMalformedDirNames1828152286/001/spool/dt=2026-9-24/inst=blue/part-000001.jsonl Rows:0 UncompressedBytes:2} {DT:20260924 Inst:blue Part:2 SpoolPath:/var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestListPendingSealsRejectsMalformedDirNames1828152286/001/spool/dt=20260924/inst=blue/part-000002.jsonl Rows:0 UncompressedBytes:2}]
--- FAIL: TestListPendingSealsRejectsMalformedDirNames (0.00s)
```

**GREEN（实现三条守卫后，29 条，`SKIP=0`）**：

```text
=== RUN   TestSealIsAtomicOnCrash
--- PASS: TestSealIsAtomicOnCrash (0.11s)
=== RUN   TestParsePartSeqUsedBySealer
--- PASS: TestParsePartSeqUsedBySealer (0.00s)
PASS
ok  	github.com/example/llm-gateway/internal/tracearchive	0.115s
=== RUN   TestSealProducesArtifactAndRemovesSpool
--- PASS: TestSealProducesArtifactAndRemovesSpool (0.10s)
=== RUN   TestSealRefusesToOverwriteExistingArtifact
--- PASS: TestSealRefusesToOverwriteExistingArtifact (0.00s)
=== RUN   TestSealRejectsRowCountMismatch
--- PASS: TestSealRejectsRowCountMismatch (0.00s)
=== RUN   TestSealFooterPointsAtValidTrailer
--- PASS: TestSealFooterPointsAtValidTrailer (0.05s)
=== RUN   TestSealFailsOnNonZeroExit
--- PASS: TestSealFailsOnNonZeroExit (0.01s)
=== RUN   TestSealFailsWhenZstdMissing
--- PASS: TestSealFailsWhenZstdMissing (0.00s)
=== RUN   TestSealGoldenLayout
--- PASS: TestSealGoldenLayout (0.01s)
=== RUN   TestListPendingSealsSkipsFreshFiles
--- PASS: TestListPendingSealsSkipsFreshFiles (0.00s)
=== RUN   TestListPendingSealsReturnsStaleFiles
--- PASS: TestListPendingSealsReturnsStaleFiles (0.00s)
=== RUN   TestListPendingSealsIgnoresIllegalNames
--- PASS: TestListPendingSealsIgnoresIllegalNames (0.00s)
=== RUN   TestSealInvokesZstdCLIWithArgs
=== RUN   TestSealInvokesZstdCLIWithArgs/默认级别与默认窗口
=== RUN   TestSealInvokesZstdCLIWithArgs/显式级别与显式窗口
--- PASS: TestSealInvokesZstdCLIWithArgs (0.02s)
    --- PASS: TestSealInvokesZstdCLIWithArgs/默认级别与默认窗口 (0.01s)
    --- PASS: TestSealInvokesZstdCLIWithArgs/显式级别与显式窗口 (0.01s)
=== RUN   TestSealDefaultsZstdBinaryFromPath
--- PASS: TestSealDefaultsZstdBinaryFromPath (0.01s)
=== RUN   TestNewSealerValidatesOptions
=== RUN   TestNewSealerValidatesOptions/空_Root_被拒绝
=== RUN   TestNewSealerValidatesOptions/非_2_的幂窗口被拒绝
--- PASS: TestNewSealerValidatesOptions (0.00s)
    --- PASS: TestNewSealerValidatesOptions/空_Root_被拒绝 (0.00s)
    --- PASS: TestNewSealerValidatesOptions/非_2_的幂窗口被拒绝 (0.00s)
=== RUN   TestSealRejectsUnknownArtifactState
--- PASS: TestSealRejectsUnknownArtifactState (0.00s)
=== RUN   TestSealFailsWhenSpoolMissing
--- PASS: TestSealFailsWhenSpoolMissing (0.00s)
=== RUN   TestSealFailsWhenSpoolIsDirectory
--- PASS: TestSealFailsWhenSpoolIsDirectory (0.00s)
=== RUN   TestSealAcceptsRowsZeroFromDiscoveryPath
--- PASS: TestSealAcceptsRowsZeroFromDiscoveryPath (0.04s)
=== RUN   TestListPendingSealsHandlesSpoolRootErrors
=== RUN   TestListPendingSealsHandlesSpoolRootErrors/spool_根目录不存在返回空而非报错
=== RUN   TestListPendingSealsHandlesSpoolRootErrors/spool_根目录读失败必须报错
--- PASS: TestListPendingSealsHandlesSpoolRootErrors (0.00s)
    --- PASS: TestListPendingSealsHandlesSpoolRootErrors/spool_根目录不存在返回空而非报错 (0.00s)
    --- PASS: TestListPendingSealsHandlesSpoolRootErrors/spool_根目录读失败必须报错 (0.00s)
=== RUN   TestListPendingSealsIgnoresNonLayoutEntries
--- PASS: TestListPendingSealsIgnoresNonLayoutEntries (0.00s)
=== RUN   TestSealRejectsTruncatedSpool
=== RUN   TestSealRejectsTruncatedSpool/目录扫描路径（Rows_未知，无交叉校验可用）
=== RUN   TestSealRejectsTruncatedSpool/行数已知路径（纵深防御，交叉校验本身也拦不住残行）
--- PASS: TestSealRejectsTruncatedSpool (0.00s)
    --- PASS: TestSealRejectsTruncatedSpool/目录扫描路径（Rows_未知，无交叉校验可用） (0.00s)
    --- PASS: TestSealRejectsTruncatedSpool/行数已知路径（纵深防御，交叉校验本身也拦不住残行） (0.00s)
=== RUN   TestSealAcceptsEmptySpool
--- PASS: TestSealAcceptsEmptySpool (0.01s)
=== RUN   TestSealerListPendingSealsUsesConfiguredMinAge
--- PASS: TestSealerListPendingSealsUsesConfiguredMinAge (0.00s)
=== RUN   TestSealRejectsMismatchedZstdVersion
--- PASS: TestSealRejectsMismatchedZstdVersion (0.00s)
=== RUN   TestSealRejectsUnparsableZstdVersion
--- PASS: TestSealRejectsUnparsableZstdVersion (0.00s)
=== RUN   TestSealAcceptsConfiguredZstdVersion
--- PASS: TestSealAcceptsConfiguredZstdVersion (0.01s)
=== RUN   TestListPendingSealsRejectsMalformedDirNames
--- PASS: TestListPendingSealsRejectsMalformedDirNames (0.00s)
=== RUN   TestSealRejectsFailedVersionProbe
--- PASS: TestSealRejectsFailedVersionProbe (0.00s)
PASS
ok  	github.com/example/llm-gateway/tests/tracearchive	0.305s
```

## 10.6 本轮变异表（44 条）与新增守卫的双侧输出

方法同 §5.1（同一合并正则、两个包、`-count=1 -timeout 30s`，每条变异后校验文件还原）：

| 变异 | 结果 | 杀死的用例 |
|---|---|---|
| m01_不调用配置的二进制 | 红 | TestSealDefaultsZstdBinaryFromPath, TestSealFailsOnNonZeroExit, TestSealGoldenLayout, TestSealInvokesZstdCLIWithArgs |
| m02_退回Go库编码器 | 红 | TestSealAcceptsEmptySpool, TestSealDefaultsZstdBinaryFromPath, TestSealFailsOnNonZeroExit, TestSealGoldenLayout, TestSealInvokesZstdCLIWithArgs |
| m03_删除产物存在检查 | 红 | TestSealRefusesToOverwriteExistingArtifact, TestSealRejectsUnknownArtifactState |
| m04_删除无法判断分支 | 红 | TestSealRejectsUnknownArtifactState |
| m05_ErrPartExists改v | 红 | TestSealRefusesToOverwriteExistingArtifact |
| m06_删除Rows等于0的豁免 | 红 | TestSealAcceptsRowsZeroFromDiscoveryPath |
| m07_放宽为只拦多于 | 红 | TestSealRejectsRowCountMismatch |
| m08_ErrRowCountMismatch改v | 红 | TestSealRejectsRowCountMismatch |
| m09_删除残行守卫 | 红 | TestSealRejectsTruncatedSpool |
| m10_删除zstd失败时的tmp清理 | 红 | TestSealFailsOnNonZeroExit |
| m11_删除建目录 | 红 | TestSealAcceptsConfiguredZstdVersion, TestSealAcceptsEmptySpool, TestSealAcceptsRowsZeroFromDiscoveryPath, TestSealDefaultsZstdBinaryFromPath, TestSealFailsOnNonZeroExit, TestSealFooterPointsAtValidTrailer, TestSealGoldenLayout, TestSealInvokesZstdCLIWithArgs, TestSealIsAtomicOnCrash, TestSealProducesArtifactAndRemovesSpool |
| m12_ErrSealFailed改v | 红 | TestSealFailsOnNonZeroExit |
| m13_删除ErrNotFound归一化 | 红 | TestSealFailsWhenZstdMissing |
| m14_删除failBeforeRename钩子 | 红 | TestSealIsAtomicOnCrash |
| m15_删除打开spool失败守卫 | **绿（零覆盖！）** | — |
| m16_删除读spool失败守卫 | 红(超时/挂死) | 超时 |
| m17_trailer先于帧 | 红 | TestSealAcceptsRowsZeroFromDiscoveryPath, TestSealFooterPointsAtValidTrailer, TestSealGoldenLayout |
| m18_删除Root为空守卫 | 红 | TestNewSealerValidatesOptions |
| m19_删除ZstdBinary默认值 | 红 | TestSealDefaultsZstdBinaryFromPath |
| m20_删除ZstdLevel默认值 | 红 | TestSealInvokesZstdCLIWithArgs |
| m21_删除ZstdWindowBytes默认值 | 红 | TestSealIsAtomicOnCrash, TestSealProducesArtifactAndRemovesSpool, TestSealRefusesToOverwriteExistingArtifact |
| m23_删除窗口2的幂校验 | 红 | TestNewSealerValidatesOptions |
| m24_删除ctx取消分支 | **绿（零覆盖！）** | — |
| m25_删除根目录不存在分支 | 红 | TestListPendingSealsHandlesSpoolRootErrors |
| m26_删除根目录读失败上抛 | 红 | TestListPendingSealsHandlesSpoolRootErrors |
| m27_删除dt前缀过滤 | 红 | TestListPendingSealsIgnoresNonLayoutEntries |
| m28_删除dt的IsDir过滤 | 红 | TestListPendingSealsIgnoresNonLayoutEntries |
| m29_删除inst前缀过滤 | 红 | TestListPendingSealsIgnoresNonLayoutEntries |
| m30_删除inst的IsDir过滤 | 红 | TestListPendingSealsIgnoresNonLayoutEntries |
| m31_删除jsonl后缀过滤 | 红 | TestListPendingSealsIgnoresIllegalNames |
| m32_删除parsePartSeq过滤 | 红 | TestListPendingSealsIgnoresIllegalNames |
| m33_删除mtime门槛 | 红 | TestListPendingSealsSkipsFreshFiles, TestSealerListPendingSealsUsesConfiguredMinAge |
| m34_删除sort顺序 | **绿（零覆盖！）** | — |
| m35_删除minAge默认值 | 红 | TestSealerListPendingSealsUsesConfiguredMinAge |
| m36_minAge默认值退回15分钟 | 红 | TestSealerListPendingSealsUsesConfiguredMinAge |
| m37_删除版本探测调用 | 红 | TestSealFailsWhenZstdMissing, TestSealRejectsMismatchedZstdVersion, TestSealRejectsUnparsableZstdVersion |
| m38_版本比对恒通过 | 红 | TestSealRejectsMismatchedZstdVersion |
| m39_无法解析时放行 | 红 | TestSealRejectsUnparsableZstdVersion |
| m40_期望版本写死默认值 | 红 | TestSealAcceptsConfiguredZstdVersion, TestSealAcceptsRowsZeroFromDiscoveryPath, TestSealFooterPointsAtValidTrailer, TestSealIsAtomicOnCrash, TestSealProducesArtifactAndRemovesSpool |
| m41_忽略探测失败 | 红 | TestSealFailsWhenZstdMissing, TestSealRejectsFailedVersionProbe |
| m42_删除dt目录名校验 | 红 | TestListPendingSealsRejectsMalformedDirNames |
| m43_删除inst目录名校验 | 红 | TestListPendingSealsRejectsMalformedDirNames |
| m44_版本探测结果不记忆 | **绿（零覆盖！）** | — |
| m45_解析放宽为不校验三段 | **绿（零覆盖！）** | — |

（脚本自检：还原校验: 一致）

### 10.6.1 三条新守卫的双侧原始输出

```text
##### m35_删除minAge默认值 #####
[变异侧] python3 seal_apply2.py m35_删除minAge默认值 && go test ... -count=1
ok  	github.com/example/llm-gateway/internal/tracearchive	0.056s
--- FAIL: TestSealerListPendingSealsUsesConfiguredMinAge (0.00s)
    sealer_test.go:899: 默认 minAge 不得为 0：1 分钟前写过的 spool 不应被判为「已停止追加」，实际 [{DT:2026-09-24 Inst:blue Part:0 SpoolPath:/var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestSealerListPendingSealsUsesConfiguredMinAge3022492930/001/spool/dt=2026-09-24/inst=blue/part-000000.jsonl Rows:0 UncompressedBytes:2}]
FAIL
FAIL	github.com/example/llm-gateway/tests/tracearchive	0.247s
FAIL
[原始侧] cp /tmp/sealer.pristine.go → sealer.go && go test ... -count=1
ok  	github.com/example/llm-gateway/internal/tracearchive	0.054s
ok  	github.com/example/llm-gateway/tests/tracearchive	0.258s
##### m36_minAge默认值退回15分钟 #####
[变异侧] python3 seal_apply2.py m36_minAge默认值退回15分钟 && go test ... -count=1
ok  	github.com/example/llm-gateway/internal/tracearchive	0.061s
--- FAIL: TestSealerListPendingSealsUsesConfiguredMinAge (0.00s)
    sealer_test.go:940: 默认 minAge=3h 时应且仅应发现 4 小时前的块（2 小时前的仍可能在被追加），实际 [/var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestSealerListPendingSealsUsesConfiguredMinAge473862990/002/spool/dt=2026-09-24/inst=blue/part-000001.jsonl /var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestSealerListPendingSealsUsesConfiguredMinAge473862990/002/spool/dt=2026-09-24/inst=blue/part-000002.jsonl]
FAIL
FAIL	github.com/example/llm-gateway/tests/tracearchive	0.272s
FAIL
[原始侧] cp /tmp/sealer.pristine.go → sealer.go && go test ... -count=1
ok  	github.com/example/llm-gateway/internal/tracearchive	0.059s
ok  	github.com/example/llm-gateway/tests/tracearchive	0.275s
##### m37_删除版本探测调用 #####
[变异侧] python3 seal_apply2.py m37_删除版本探测调用 && go test ... -count=1
ok  	github.com/example/llm-gateway/internal/tracearchive	0.051s
--- FAIL: TestSealFailsWhenZstdMissing (0.00s)
    sealer_test.go:272: 错误链里应保留 exec.ErrNotFound（这要求用 %w 包装），实际 tracearchive: 封块失败: zstd 子进程失败（args=[-19 --long=29 -T1 -c]）: fork/exec /var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestSealFailsWhenZstdMissing4187555721/002/definitely-not-zstd: no such file or directory（stderr: ）
--- FAIL: TestSealRejectsMismatchedZstdVersion (0.01s)
    sealer_test.go:973: zstd 版本不符必须失败（ErrSealFailed），实际 <nil>
--- FAIL: TestSealRejectsUnparsableZstdVersion (0.01s)
    sealer_test.go:1009: 无法解析 zstd 版本时必须失败（ErrSealFailed），实际 <nil>
FAIL
FAIL	github.com/example/llm-gateway/tests/tracearchive	0.244s
FAIL
[原始侧] cp /tmp/sealer.pristine.go → sealer.go && go test ... -count=1
ok  	github.com/example/llm-gateway/internal/tracearchive	0.059s
ok  	github.com/example/llm-gateway/tests/tracearchive	0.263s
##### m38_版本比对恒通过 #####
[变异侧] python3 seal_apply2.py m38_版本比对恒通过 && go test ... -count=1
ok  	github.com/example/llm-gateway/internal/tracearchive	0.066s
--- FAIL: TestSealRejectsMismatchedZstdVersion (0.01s)
    sealer_test.go:973: zstd 版本不符必须失败（ErrSealFailed），实际 <nil>
FAIL
FAIL	github.com/example/llm-gateway/tests/tracearchive	0.284s
FAIL
[原始侧] cp /tmp/sealer.pristine.go → sealer.go && go test ... -count=1
ok  	github.com/example/llm-gateway/internal/tracearchive	0.056s
ok  	github.com/example/llm-gateway/tests/tracearchive	0.266s
##### m39_无法解析时放行 #####
[变异侧] python3 seal_apply2.py m39_无法解析时放行 && go test ... -count=1
ok  	github.com/example/llm-gateway/internal/tracearchive	0.053s
--- FAIL: TestSealRejectsUnparsableZstdVersion (0.01s)
    sealer_test.go:1009: 无法解析 zstd 版本时必须失败（ErrSealFailed），实际 <nil>
FAIL
FAIL	github.com/example/llm-gateway/tests/tracearchive	0.252s
FAIL
[原始侧] cp /tmp/sealer.pristine.go → sealer.go && go test ... -count=1
ok  	github.com/example/llm-gateway/internal/tracearchive	0.057s
ok  	github.com/example/llm-gateway/tests/tracearchive	0.260s
##### m40_期望版本写死默认值 #####
[变异侧] python3 seal_apply2.py m40_期望版本写死默认值 && go test ... -count=1
--- FAIL: TestSealIsAtomicOnCrash (0.01s)
    sealer_internal_test.go:32: 注入的失败应被原样传播（errors.Is 必须可识别），实际 tracearchive: 封块失败: zstd 版本不符：期望 1.5.7，实际 1.5.7。压缩比标定是版本特定的（规格 §5.3.1.1/§11.1），换版本必须重新标定后才能放开
FAIL
FAIL	github.com/example/llm-gateway/internal/tracearchive	0.013s
--- FAIL: TestSealProducesArtifactAndRemovesSpool (0.00s)
    sealer_test.go:43: 封块失败: tracearchive: 封块失败: zstd 版本不符：期望 1.5.7，实际 1.5.7。压缩比标定是版本特定的（规格 §5.3.1.1/§11.1），换版本必须重新标定后才能放开
--- FAIL: TestSealFooterPointsAtValidTrailer (0.00s)
    sealer_test.go:171: tracearchive: 封块失败: zstd 版本不符：期望 1.5.7，实际 1.5.7。压缩比标定是版本特定的（规格 §5.3.1.1/§11.1），换版本必须重新标定后才能放开
--- FAIL: TestSealAcceptsRowsZeroFromDiscoveryPath (0.01s)
    sealer_test.go:667: Rows=0（行数未知）时不应被行数交叉校验拒绝: tracearchive: 封块失败: zstd 版本不符：期望 1.5.7，实际 1.5.7。压缩比标定是版本特定的（规格 §5.3.1.1/§11.1），换版本必须重新标定后才能放开
--- FAIL: TestSealAcceptsConfiguredZstdVersion (0.00s)
    sealer_test.go:1034: 显式期望版本与二进制一致时应放行（重新标定后的升级路径）: tracearchive: 封块失败: zstd 版本不符：期望 9.9.9，实际 9.9.9。压缩比标定是版本特定的（规格 §5.3.1.1/§11.1），换版本必须重新标定后才能放开
FAIL
FAIL	github.com/example/llm-gateway/tests/tracearchive	0.126s
FAIL
[原始侧] cp /tmp/sealer.pristine.go → sealer.go && go test ... -count=1
ok  	github.com/example/llm-gateway/internal/tracearchive	0.062s
ok  	github.com/example/llm-gateway/tests/tracearchive	0.262s
##### m41_忽略探测失败 #####
[变异侧] python3 seal_apply2.py m41_忽略探测失败 && go test ... -count=1
ok  	github.com/example/llm-gateway/internal/tracearchive	0.059s
--- FAIL: TestSealFailsWhenZstdMissing (0.00s)
    sealer_test.go:272: 错误链里应保留 exec.ErrNotFound（这要求用 %w 包装），实际 tracearchive: 封块失败: 无法解析 zstd 版本输出 ""，无法确认编码器版本（规格 §11.1 要求锁定版本，拒绝在未标定的编码器上封块）
--- FAIL: TestSealRejectsFailedVersionProbe (0.00s)
    sealer_test.go:1116: 错误链里应保留探测进程的退出码 7（这要求用 %w 包装探测错误），实际 tracearchive: 封块失败: 无法解析 zstd 版本输出 ""，无法确认编码器版本（规格 §11.1 要求锁定版本，拒绝在未标定的编码器上封块）
FAIL
FAIL	github.com/example/llm-gateway/tests/tracearchive	0.270s
FAIL
[原始侧] cp /tmp/sealer.pristine.go → sealer.go && go test ... -count=1
ok  	github.com/example/llm-gateway/internal/tracearchive	0.059s
ok  	github.com/example/llm-gateway/tests/tracearchive	0.275s
##### m42_删除dt目录名校验 #####
[变异侧] python3 seal_apply2.py m42_删除dt目录名校验 && go test ... -count=1
ok  	github.com/example/llm-gateway/internal/tracearchive	0.057s
--- FAIL: TestListPendingSealsRejectsMalformedDirNames (0.00s)
    sealer_test.go:1081: 只应返回合法目录下的 spool，实际 5: [{DT:.. Inst:blue Part:3 SpoolPath:/var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestListPendingSealsRejectsMalformedDirNames2544342750/001/spool/dt=../inst=blue/part-000003.jsonl Rows:0 UncompressedBytes:2} {DT:2026-02-30 Inst:blue Part:4 SpoolPath:/var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestListPendingSealsRejectsMalformedDirNames2544342750/001/spool/dt=2026-02-30/inst=blue/part-000004.jsonl Rows:0 UncompressedBytes:2} {DT:2026-09-24 Inst:blue Part:7 SpoolPath:/var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestListPendingSealsRejectsMalformedDirNames2544342750/001/spool/dt=2026-09-24/inst=blue/part-000007.jsonl Rows:0 UncompressedBytes:2} {DT:2026-9-24 Inst:blue Part:1 SpoolPath:/var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestListPendingSealsRejectsMalformedDirNames2544342750/001/spool/dt=2026-9-24/inst=blue/part-000001.jsonl Rows:0 UncompressedBytes:2} {DT:20260924 Inst:blue Part:2 SpoolPath:/var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestListPendingSealsRejectsMalformedDirNames2544342750/001/spool/dt=20260924/inst=blue/part-000002.jsonl Rows:0 UncompressedBytes:2}]
FAIL
FAIL	github.com/example/llm-gateway/tests/tracearchive	0.251s
FAIL
[原始侧] cp /tmp/sealer.pristine.go → sealer.go && go test ... -count=1
ok  	github.com/example/llm-gateway/internal/tracearchive	0.088s
ok  	github.com/example/llm-gateway/tests/tracearchive	0.295s
##### m43_删除inst目录名校验 #####
[变异侧] python3 seal_apply2.py m43_删除inst目录名校验 && go test ... -count=1
ok  	github.com/example/llm-gateway/internal/tracearchive	0.062s
--- FAIL: TestListPendingSealsRejectsMalformedDirNames (0.00s)
    sealer_test.go:1081: 只应返回合法目录下的 spool，实际 3: [{DT:2026-09-24 Inst:.. Part:6 SpoolPath:/var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestListPendingSealsRejectsMalformedDirNames956102014/001/spool/dt=2026-09-24/inst=../part-000006.jsonl Rows:0 UncompressedBytes:2} {DT:2026-09-24 Inst:blue Part:7 SpoolPath:/var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestListPendingSealsRejectsMalformedDirNames956102014/001/spool/dt=2026-09-24/inst=blue/part-000007.jsonl Rows:0 UncompressedBytes:2} {DT:2026-09-24 Inst:bluex Part:5 SpoolPath:/var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestListPendingSealsRejectsMalformedDirNames956102014/001/spool/dt=2026-09-24/inst=bluex/part-000005.jsonl Rows:0 UncompressedBytes:2}]
FAIL
FAIL	github.com/example/llm-gateway/tests/tracearchive	0.263s
FAIL
[原始侧] cp /tmp/sealer.pristine.go → sealer.go && go test ... -count=1
ok  	github.com/example/llm-gateway/internal/tracearchive	0.058s
ok  	github.com/example/llm-gateway/tests/tracearchive	0.272s
```

### 10.6.2 本轮零覆盖条目（如实报告）

| 变异 | 删除后 | 为什么无法区分 | 保留理由 |
|---|---|---|---|
| `m15_删除打开spool失败守卫` | 全绿 | 同 §5.3（TOCTOU，公开 API 不可达） | 同 §5.3 |
| `m24_删除ctx取消分支` | 全绿 | 同 §5.3（仅文案不同） | 同 §5.3 |
| `m34_删除sort顺序` | 全绿 | 同 §5.3（与 `os.ReadDir` 排序等价） | 同 §5.3 |
| `m44_版本探测结果不记忆` | 全绿 | 删掉 `s.versionOK = true` 只是**每轮多探一次版本**，除「探测次数」外行为完全一致；要区分只能断言调用次数（断言调用次数而非行为，且会引入额外的「计数文件」测试复杂度） | 每轮封块都 spawn 一次子进程是纯浪费；保留并用注释说明它只影响成本 |
| `m45_解析放宽为不校验三段` | 全绿 | 三段校验只改变**拒绝理由的文案**，不改变是否拒绝：`"1.5"` 这种输出即使放过解析，仍会因与期望版本不符而被拒 | 让「解析出来的东西确实是一个版本号」这件事在类型上成立，避免把 `"1.5-beta,"` 之类送上比对 |

### 10.6.3 本轮修复的两处「变异本身不合法」

- `m02_退回Go库编码器` 原本靠「把 `os/exec` 导入替换成 klauspost」实现。新增版本探针后
  `os/exec` 仍被探针使用，该替换会让文件**编译失败**（变异质量下降为「编译不过」）。
  已改为**追加** klauspost 导入、且不动 `"os/exec"`。现在它依旧被 6 条用例杀死，
  是约束 1 的直接反证。
- `m13_删除ErrNotFound归一化` 原本删的是 `runZstd` 里的归一化。探针上线后
  `runZstd` 的归一化**只可能被 TOCTOU 命中**（二进制在探测与压缩之间被删），
  测试不可达；我据此**删掉了 `runZstd` 里的重复归一化**（归一化只保留在探针一处，
  避免第二个零覆盖分支），并把 `m13` 指向探针的归一化——它现在被
  `TestSealFailsWhenZstdMissing` 杀死。

### 10.6.4 `m27`/`m29` 由红转绿引发的用例补强

加入 3b 的目录名校验后，`m27_删除dt前缀过滤` 与 `m29_删除inst前缀过滤` 变成**绿**：
原先用来区分它们的畸形目录（`notdt`/`notinst`）现在被更严格的日期校验/白名单顺带拦住
（`"notdt"` 不是合法日期）。这说明前缀过滤当时已成「删除后无人察觉」的冗余守卫。

我没有把它们当作「防御性保留」，而是补了两条**只有前缀过滤能拦**的 fixture：
`spool/2026-09-24/…`（合法日期、缺 `dt=` 前缀——`validateDT` 会放行它）与
`spool/dt=…/blue/…`（合法 inst、缺 `inst=` 前缀——白名单会放行它）。补强后
`m27`/`m29` 重新转红，两条守卫恢复可被区分。

## 10.7 本轮简报偏离清单（含 3a/3b 属新增要求）

| # | 偏离 | 类型 | 依据 |
|---|---|---|---|
| 9 | `minAge` 默认值 15m → **3h**（新增 `defaultSealMinAge`） | 裁决 1（覆盖简报默认值） | §10.1；简报/计划的默认值与「严格大于滚动间隔」冲突，已按控制者给定值与推导落地 |
| 10 | **删除包级** `ListPendingSeals(root, minAge)`，只留方法 | 裁决 2 | §10.2；计划里登记的是包级函数，需据此更新计划 |
| 11 | 新增 `SealerOptions.ExpectedZstdVersion` + `defaultZstdVersion` + 探测/解析 | 裁决 3a（**新增要求**，简报没有） | §10.3；规格 §11.1「版本必须锁定」此前无任何代码强制 |
| 12 | `ListPendingSeals` 新增 `dt`/`inst` 目录名校验 | 裁决 3b（**新增要求**，简报没有） | §10.4；列举侧与写入侧校验不对称，`dt=..` 可越出 spool 子树 |
| 13 | 删除 `runZstd` 里的 `ErrNotFound` 归一化（首轮【落地修正 2】的代码） | 本轮重构 | §10.6.3；探针上线后该分支测试不可达，重复归一化属第二个零覆盖分支 |
| 14 | 新增 5 条用例（版本 4 条 + 畸形目录 1 条）、`TestSealerListPendingSealsUsesConfiguredMinAge` 加一条断言、`TestListPendingSealsIgnoresNonLayoutEntries` 加两条 fixture | 裁决要求 + 变异自检 | §10.3/§10.4/§10.6.4 |
| 15 | 测试辅助 `requireZstd` 改为返回 `(路径, 版本)`；`fakeZstd` 增加 `--version` 应答；新增 `fakeZstdWithVersion`/`fakeZstdScript`/`listPendingSeals`/`zstdVersion` | 机械适配 | 期望版本必须显式（本机 v1.5.7 ≠ 生产 v1.4.8），否则每条真封块用例都会被版本守卫拦住；假 zstd 也必须能应答 `--version` |
| 16 | `fakeZstdVersion` 常量 = `"1.4.8"`，与生产默认值**故意重复** | 测试设计 | 注释已说明：两者不一致时所有假 zstd 用例转红——改默认版本必须被看见 |

## 10.8 本轮暴露的一个方法论缺陷（我的验证流程，不是产品代码）

补用例时我用 `printf ... >> /tmp/sealrun.txt` 把新用例名接到合并正则后面，而该文件
原本**以换行结尾**（此前一直无害，因为 `-run "$(cat …)"` 会剥掉尾部换行）。新名字被写到
换行**之后**，于是正则里出现一个内嵌换行，把最后一个分支变成
`TestSealerListPendingSealsUsesConfiguredMinAge\n` —— **该用例从此静默地不再被执行**，
而 `ok` 依旧打印、我差点把它当成「28 条全绿」。

这类缺陷的危害是「绿得看不出来」，所以我把验证流程固定为两条断言，并将在本轮之后
继续执行：

1. 正则文件**去空白**后按 `|` 切分成名字集合，与 `go test -v` 的 `=== RUN` 集合
   **做集合 diff**，不一致即失败；
2. 断言 `--- PASS` 条数 == 名字个数、`--- SKIP` 计数 == 0。

（`/tmp/sealrun.txt` 已重建；本轮所有 GREEN/RED 证据都满足上述两条。）

## 10.9 缺口更新

- **3c（footer 的 `frame_sha256` 只写不读）→ 转交任务 7**：按裁决只作登记，本任务未做。
- ① 网关 part 滚动（D7）→ 计划 2（**并新增对端约束：滚动间隔 ≤ 1h**）；② sealer
  循环/systemd/告警/回写 → 计划 4；④ ENOSPC 零覆盖 → 记账；⑤ 真 zstd 用例不解压帧
  → 任务 7 端到端。
- 其余缺口同 §8（**新**：`SealerOptions.ExpectedZstdVersion` 只在 sealer 侧生效，
  写入侧/解码侧没有任何版本约束；生产宿主机上 `zstd` 的实际版本仍未被任何部署脚本钉住）。

## 10.10 本轮我不确定的地方

1. **`defaultZstdVersion = "1.4.8"` 这个字符串我无法在本机验证**：本机是 v1.5.7，
   生产是 v1.4.8（来自标定记录）。因此「默认值在生产上探测通过」这件事**没有实测**，
   只能在部署时首次封块时验证。所有真封块用例都是显式传本机版本。
2. **网关滚动间隔 ≤ 1h 是「已登记但未实现」的对端约束**：在计划 2 落地前，3h 的安全性
   依赖 `defaultSealInterval=15m` 这个现有常量；若计划 2 把它改到 > 1h，3h 就不再
   「严格大于」，需要同步调整（这条已写进常量注释，但没有代码强制）。
3. `m44`（探测记忆化）与 `m45`（三段解析）两条零覆盖**我选择保留而非删除**，理由见
   §10.6.2；如果你认为「不能被杀死的守卫就该删」，请裁决——那两条都可以安全删除，
   代价分别是「每轮多一次 `--version` 子进程」与「解析更宽松」。
4. 版本探测**只在校验可执行文件的版本**，不校验它是否与「标定用的那份二进制」是同一个
   文件（例如同版本号但被打了不同补丁/不同编译选项的 zstd 构建）。规格 §11.1 只要求
   版本锁定，我按版本号实现；要不要加 `sha256(binary)` 白名单属另一个决定。

---

# 11. 第三轮裁决落实（提交 `37f95c36`）

三条裁决的落实结果，以及一条升级为**计划级全局约束**的方法论要求。

## 11.1 裁决 4：删除版本探测的记忆化

**落实**：删除 `Sealer.versionOK` 字段以及 `verifyZstdVersion` 里的「已核对则跳过」与
`versionOK = true` 两处。**每次 `Seal` 都探测一次**；探测位置不变（产物存在性检查与
行数校验之后、任何文件创建之前）。

**代码现状**：`grep -c versionOK internal/tracearchive/sealer.go` ⇒ `0`。该分支**不存在了**，
所以没有「删除它的变异」可做（`m44` 已从变异集中移除，脚本里留了一行注释说明原因）。
删除的收益不止「省一次子进程」——`Sealer` 现在**没有运行期可变状态**（唯一字段是测试
注入的 `failBeforeRename`，只在单测里、并发调用之前设置），因此它在并发调用 `Seal` 下
不再有数据竞争。注释就写在这个位置上。

**残余缺口（如实登记，控制者已裁决「接受现状，不加测试」）**：我没有加「断言每次 Seal
都探测」的用例，因为这要断言**调用次数**而不是行为（数子进程被 spawn 了几次），会把
测试绑到实现细节上。代价是：如果有人把记忆化加回来，现有用例不会发现它。缺口可接受的
理由是记忆化的删除依据是**并发安全**（不可测的设计属性）而非行为差异。
**并发属性改由任务 8 的端到端验收用 `go test -race` 覆盖一次**（比数 spawn 次数诚实）。

**字段注释已写明的「动机」**（评审要求确认：只写「无状态」不够，必须让后人看见加缓存的
代价）——`37f95c36` 上 `type Sealer struct` 的注释原文：

```go
	// 本结构体【没有运行期可变状态】（评审裁决 1）：版本探测刻意不做记忆化，
	// 因此 Sealer 可以在多个 goroutine 间共享调用 Seal 而无需同步
	//（唯一的例外是下面的测试注入钩子，它只在单测里、并发调用之前设置）。
	//
	// 为什么不做记忆化：省下的只是一天几十次（回溯追赶时最多上百次）
	// `zstd --version`，合计秒级；而代价是引入一个未同步的可变字段，
	// 让这个看起来可共享的值类型在并发下产生数据竞争。不划算。
```

动机三要素齐备：① 本类型**可被并发共享调用**；② 记忆化的代价是**未同步的可变字段
⇒ 数据竞争**；③ 划算与否的结论（收益秒级、代价是并发隐患）。

## 11.2 裁决 5：让解析校验变成可杀死的（诊断保真度）

你的判断是对的：m45 原本「只改变文案」并不是死代码，而是**诊断保真度**。三段式/数字
校验真正的价值在于把两类完全不同的处置分开：

| 错误文案 | 运维的解读与动作 |
|---|---|
| `无法解析 zstd 版本输出 "…v1.5, …"` | 这份二进制/包装脚本不规范 → **换构建** |
| `zstd 版本不符：期望 1.4.8，实际 1.5` | 版本真的漂移了 → **重走压缩比标定** |

如果把两段/带后缀的输出放过去，运维看到的就是「版本不符：期望 1.4.8，实际 1.5」——
会去重走标定流程，而正确的动作是修那份二进制。因此**文案本身就是接口**，
断言它是有意义的，不是「断言日志字符串」。

**用例改动**

1. `TestSealRejectsUnparsableZstdVersion` 扩为 **4 个子用例**，每个都断言错误文案含
   「无法解析」：`输出里没有版本号` / `版本号只有两段`(`v1.5`) /
   `版本号带后缀`(`v1.5.7-beta`) / `版本号段内非数字`(`v1.x.7`)。
2. `TestSealRejectsMismatchedZstdVersion` 补一句断言：错误信息必须**同时**含期望值
   `1.4.8` 与实际值 `1.9.9`。

**删除变异（三个都在守卫位、都转红）**

```text
##### m45_解析放宽为不校验三段 #####
[变异侧]
ok  	github.com/example/llm-gateway/internal/tracearchive	0.056s
--- FAIL: TestSealRejectsUnparsableZstdVersion (0.02s)
    --- FAIL: TestSealRejectsUnparsableZstdVersion/版本号只有两段 (0.00s)
FAIL
FAIL	github.com/example/llm-gateway/tests/tracearchive	0.300s
FAIL
[原始侧]
ok  	github.com/example/llm-gateway/internal/tracearchive	0.051s
ok  	github.com/example/llm-gateway/tests/tracearchive	0.259s
##### m46_删除版本号数字校验 #####
[变异侧]
ok  	github.com/example/llm-gateway/internal/tracearchive	0.054s
--- FAIL: TestSealRejectsUnparsableZstdVersion (0.02s)
    --- FAIL: TestSealRejectsUnparsableZstdVersion/版本号带后缀 (0.00s)
    --- FAIL: TestSealRejectsUnparsableZstdVersion/版本号段内非数字 (0.00s)
FAIL
FAIL	github.com/example/llm-gateway/tests/tracearchive	0.260s
FAIL
[原始侧]
ok  	github.com/example/llm-gateway/internal/tracearchive	0.055s
ok  	github.com/example/llm-gateway/tests/tracearchive	0.266s
##### m47_版本不符信息省略期望值 #####
[变异侧]
ok  	github.com/example/llm-gateway/internal/tracearchive	0.056s
--- FAIL: TestSealRejectsMismatchedZstdVersion (0.00s)
    sealer_test.go:976: 错误信息必须同时带上【实际】与【期望】版本号（运维要据此决定换构建还是重新标定），实际: tracearchive: 封块失败: zstd 版本不符：实际 1.9.9。压缩比标定是版本特定的（规格 §5.3.1.1/§11.1），换版本必须重新标定后才能放开
FAIL
FAIL	github.com/example/llm-gateway/tests/tracearchive	0.261s
FAIL
[原始侧]
ok  	github.com/example/llm-gateway/internal/tracearchive	0.054s
ok  	github.com/example/llm-gateway/tests/tracearchive	0.264s
```

对应关系：`m45`（放宽三段校验）→ 被「版本号只有两段」杀死；`m46`（删除数字校验）→
被「版本号带后缀」+「版本号段内非数字」杀死；`m47`（错误信息省掉期望值）→ 被
`TestSealRejectsMismatchedZstdVersion` 的新断言杀死。三条零覆盖由此归零。

**我判断「无法被有意义断言」而选择删除的情况**：这一轮里没有出现——三段校验与数字
校验的文案差异正好可以被有意义地区分（见上表），所以我保留了两段校验并补了断言，
没有删任何一段。

## 11.3 裁决 6：合并正则的三条断言（升级为计划级全局约束）

### 11.3.1 缺陷为什么危险

我上一轮踩的坑：用 `printf ... >> regex.txt` 追加用例名，而该文件原本以换行结尾，
于是正则内容变成 `…|TestA|TestB\nTestC`——**内嵌换行把最后一个分支变成
`TestC` 之前的分支之一带上了 `\n`**，实际效果是最后那个用例静默地不再被执行。
最危险的地方在于**所有信号都是绿的**：`ok  	github.com/…/tests/tracearchive	0.3s`
照常打印，退出码 0，`--- FAIL` 一条没有。我如果只看「测试通过」，就会把一条从未运行过
的用例当成通过。这与「用例真的跑了且通过」在输出上**不可区分**。

这条缺陷不属于我的任务：计划里每个任务的 RED/GREEN 命令都是拼接出来的合并正则，
同样的写法在任何任务上都会复现，而证据会看起来完整。

### 11.3.2 请写进计划全局约束的三条断言

任何任务的 RED/GREEN 证据都必须同时满足（命令按原样可复制；口径见 §11.3.2.1）：

```bash
# 0) 准备两个集合：正则里的名字 vs 实际执行的名字
tr -d '[:space:]' < regex.txt | tr '|' '\n' | sed '/^$/d' | sort > names.txt
go test ./<pkgs> -run "$(cat regex.txt)" -v -count=1 > g.txt
grep -E '^=== RUN   Test[A-Za-z]+$' g.txt | sed 's/=== RUN   //' | sort > ran.txt

# 1) 集合相等（diff 必须无输出）
diff names.txt ran.txt

# 2) 通过数 == 名字数（注意匹配「顶层」行，见 11.3.2.1）
grep -c '^--- PASS: Test[A-Za-z]* ' g.txt    # 必须等于 wc -l < names.txt

# 3) 无跳过（除简报明确允许的例外）
grep -c '^--- SKIP' g.txt          # 必须是 0
```

#### 11.3.2.1 两条必须写进口径的细则（控制者正式定下，后续所有任务统一）

1. **断言 1 的关键是 `tr -d '[:space:]'`**：它同时消掉尾部换行与**内嵌空白**，所以
   「拼出来带换行的正则」这类错误证据**过不了**断言 1（`names.txt` 会与 `ran.txt` 不等）。
   若只在拼接时 `cat` 而不去空白，缺陷会继续静默存在。
2. **子用例不计入顶层 PASS 数**：`    --- PASS: Test…/…` 是缩进行，原写法
   `grep -c '^--- PASS'` 虽因 `^` 锚定而天然排除缩进行，但**断言 2 的正式形式**是
   带**尾随空格**匹配顶层行——`grep -c '^--- PASS: Test[A-Za-z]* '`——以显式排除
   `--- PASS: TestName/sub` 这类「无缩进也可能出现」的形式。若不排除，含子用例的测试上
   「PASS 数 == 名字数」会**失败**（子用例行里 `TestName/sub` 不含尾随空格）。

**计数口径（正式）**

- 「用例数」= **顶层测试函数数**，与 `-run` 正则里的名字集合一致；
- 子用例**单独计数**，汇报格式为「**N 个顶层用例 + M 个子用例**」；
- 本轮证据按此口径为 **29 个顶层用例 + 12 个子用例**（`PASS=29` / 名字数 29 / `SKIP=0` /
  `diff` 空），控制者已确认符合口径，无需重出。

### 11.3.3 本轮范例的原始输出（提交 `37f95c36`，干净导出目录）

```text
### 提交 37f95c36，在干净导出目录 /tmp/sealcommit3 上执行
$ tr -d "[:space:]" < /tmp/sealrun.txt | tr "|" "\n" | sed "/^\$/d" | sort > /tmp/namesF.txt
$ go test ./internal/tracearchive/ ./tests/tracearchive/ -run "$(cat /tmp/sealrun.txt)" -v -count=1
$ grep -E "^=== RUN   Test[A-Za-z]+\$" ... | sed "s/=== RUN   //" | sort > /tmp/ranF.txt
$ diff /tmp/namesF.txt /tmp/ranF.txt
  ↑ 无输出 ⇒ 断言1 成立：正则集合 == 实际执行集合（29 条）

断言2：PASS=29  名字数=29  ⇒ 相等
断言3：SKIP=0

$ tail -3 /tmp/gF.txt
--- PASS: TestSealRejectsFailedVersionProbe (0.00s)
PASS
ok  	github.com/example/llm-gateway/tests/tracearchive	0.371s
```

> 说明：断言 1 的 `tr -d '[:space:]'` 是关键——它同时消掉尾部换行与任何内嵌空白，
> 因此**拼接错误的证据无法通过断言 1**（`names.txt` 会多出/少掉条目，与 `ran.txt` 不等）。
> 子用例（`    --- PASS: Test…/…`）不计入顶层 PASS 数，需单独 `grep '^    --- PASS'`
> 观察；按正式口径复核本轮：**29 个顶层用例 + 12 个子用例**。
>
> 计数口径的反例（在同一份 `g.txt` 上实测）：`grep -c '^--- PASS'` ⇒ 29 ✓，
> `grep -c '^--- PASS: Test[A-Za-z]* '` ⇒ 29 ✓（正式形式），
> `grep -c '^    --- PASS'` ⇒ 12（子用例），
> 而**不锚定行首**的 `grep -c -- '--- PASS'` ⇒ **41 ≠ 29**，正是「把子用例算进顶层」
> 导致的误报。因此断言 2 必须用锚定 + 尾随空格的形式。

## 11.4 最终变异表（45 条：42 红 / 3 零覆盖）

| 变异 | 结果 | 杀死的用例 |
|---|---|---|
| m01_不调用配置的二进制 | 红 | TestSealDefaultsZstdBinaryFromPath, TestSealFailsOnNonZeroExit, TestSealGoldenLayout, TestSealInvokesZstdCLIWithArgs |
| m02_退回Go库编码器 | 红 | TestSealAcceptsEmptySpool, TestSealDefaultsZstdBinaryFromPath, TestSealFailsOnNonZeroExit, TestSealGoldenLayout, TestSealInvokesZstdCLIWithArgs |
| m03_删除产物存在检查 | 红 | TestSealRefusesToOverwriteExistingArtifact, TestSealRejectsUnknownArtifactState |
| m04_删除无法判断分支 | 红 | TestSealRejectsUnknownArtifactState |
| m05_ErrPartExists改v | 红 | TestSealRefusesToOverwriteExistingArtifact |
| m06_删除Rows等于0的豁免 | 红 | TestSealAcceptsRowsZeroFromDiscoveryPath |
| m07_放宽为只拦多于 | 红 | TestSealRejectsRowCountMismatch |
| m08_ErrRowCountMismatch改v | 红 | TestSealRejectsRowCountMismatch |
| m09_删除残行守卫 | 红 | TestSealRejectsTruncatedSpool |
| m10_删除zstd失败时的tmp清理 | 红 | TestSealFailsOnNonZeroExit |
| m11_删除建目录 | 红 | TestSealAcceptsConfiguredZstdVersion, TestSealAcceptsEmptySpool, TestSealAcceptsRowsZeroFromDiscoveryPath, TestSealDefaultsZstdBinaryFromPath, TestSealFailsOnNonZeroExit, TestSealFooterPointsAtValidTrailer, TestSealGoldenLayout, TestSealInvokesZstdCLIWithArgs, TestSealIsAtomicOnCrash, TestSealProducesArtifactAndRemovesSpool |
| m12_ErrSealFailed改v | 红 | TestSealFailsOnNonZeroExit |
| m13_删除ErrNotFound归一化 | 红 | TestSealFailsWhenZstdMissing |
| m14_删除failBeforeRename钩子 | 红 | TestSealIsAtomicOnCrash |
| m15_删除打开spool失败守卫 | **绿（零覆盖！）** | — |
| m16_删除读spool失败守卫 | 红(超时/挂死) | 超时 |
| m17_trailer先于帧 | 红 | TestSealAcceptsRowsZeroFromDiscoveryPath, TestSealFooterPointsAtValidTrailer, TestSealGoldenLayout |
| m18_删除Root为空守卫 | 红 | TestNewSealerValidatesOptions |
| m19_删除ZstdBinary默认值 | 红 | TestSealDefaultsZstdBinaryFromPath |
| m20_删除ZstdLevel默认值 | 红 | TestSealInvokesZstdCLIWithArgs |
| m21_删除ZstdWindowBytes默认值 | 红 | TestSealIsAtomicOnCrash, TestSealProducesArtifactAndRemovesSpool, TestSealRefusesToOverwriteExistingArtifact |
| m23_删除窗口2的幂校验 | 红 | TestNewSealerValidatesOptions |
| m24_删除ctx取消分支 | **绿（零覆盖！）** | — |
| m25_删除根目录不存在分支 | 红 | TestListPendingSealsHandlesSpoolRootErrors |
| m26_删除根目录读失败上抛 | 红 | TestListPendingSealsHandlesSpoolRootErrors |
| m27_删除dt前缀过滤 | 红 | TestListPendingSealsIgnoresNonLayoutEntries |
| m28_删除dt的IsDir过滤 | 红 | TestListPendingSealsIgnoresNonLayoutEntries |
| m29_删除inst前缀过滤 | 红 | TestListPendingSealsIgnoresNonLayoutEntries |
| m30_删除inst的IsDir过滤 | 红 | TestListPendingSealsIgnoresNonLayoutEntries |
| m31_删除jsonl后缀过滤 | 红 | TestListPendingSealsIgnoresIllegalNames |
| m32_删除parsePartSeq过滤 | 红 | TestListPendingSealsIgnoresIllegalNames |
| m33_删除mtime门槛 | 红 | TestListPendingSealsSkipsFreshFiles, TestSealerListPendingSealsUsesConfiguredMinAge |
| m34_删除sort顺序 | **绿（零覆盖！）** | — |
| m35_删除minAge默认值 | 红 | TestSealerListPendingSealsUsesConfiguredMinAge |
| m36_minAge默认值退回15分钟 | 红 | TestSealerListPendingSealsUsesConfiguredMinAge |
| m37_删除版本探测调用 | 红 | TestSealFailsWhenZstdMissing, TestSealRejectsMismatchedZstdVersion, TestSealRejectsUnparsableZstdVersion |
| m38_版本比对恒通过 | 红 | TestSealRejectsMismatchedZstdVersion |
| m39_无法解析时放行 | 红 | TestSealRejectsUnparsableZstdVersion |
| m40_期望版本写死默认值 | 红 | TestSealAcceptsConfiguredZstdVersion, TestSealAcceptsRowsZeroFromDiscoveryPath, TestSealFooterPointsAtValidTrailer, TestSealIsAtomicOnCrash, TestSealProducesArtifactAndRemovesSpool |
| m41_忽略探测失败 | 红 | TestSealFailsWhenZstdMissing, TestSealRejectsFailedVersionProbe |
| m42_删除dt目录名校验 | 红 | TestListPendingSealsRejectsMalformedDirNames |
| m43_删除inst目录名校验 | 红 | TestListPendingSealsRejectsMalformedDirNames |
| m45_解析放宽为不校验三段 | 红 | TestSealRejectsUnparsableZstdVersion |
| m46_删除版本号数字校验 | 红 | TestSealRejectsUnparsableZstdVersion |
| m47_版本不符信息省略期望值 | 红 | TestSealRejectsMismatchedZstdVersion |

（脚本自检：还原校验: 一致；共 45 条）

三处零覆盖全是首轮既已说明的条目（`m15` TOCTOU 打开 spool 失败守卫、`m24` ctx 取消仅
文案不同、`m34` sort 与 `os.ReadDir` 等价）；本轮之前的两条（探测记忆化、三段解析）
已随裁决消散：前者连分支一起删掉，后者被文案断言杀死。

## 11.5 本轮登记与残余不确定

**登记（供你更新计划）**

| # | 项 | 状态 |
|---|---|---|
| 17 | 版本探测**不做记忆化**：`Seal` 每次探测 | 裁决 4 落实；`Sealer` 因此无运行期可变状态 |
| 18 | 「解析校验」的价值是**诊断保真度**，用文案断言守护（4 个子用例 + 期望/实际值断言） | 裁决 5 落实 |
| 19 | **合并正则三断言**（集合 diff / PASS==名字数 / SKIP==0）作为每个任务证据的强制项 | 裁决 6：请写进计划全局约束 |
| 20 | `ListPendingSeals` 方法形式（原 §10.7 #10）、`defaultSealMinAge`(3h)、`ExpectedZstdVersion`(默认 `1.4.8`)、探针位置、dt/inst 目录名校验 | 与 §10.7 重复登记，等你并入计划 |
| 21 | `defaultZstdVersion="1.4.8"` 在生产探测通过 + 压缩比 10.26x 复核 | 本机无法验证 → 你记入**生产侧验收清单** |

**残余不确定：无**（控制者第四轮逐条裁决后全部关闭）

1. §11.1 的残余缺口：无人钉住「每次 Seal 都探测」；重新引入记忆化不会被测试发现。
   **控制者裁决：接受现状、不加测试**（数 spawn 次数会绑到实现细节）；并发属性改由
   **任务 8 的端到端验收用 `go test -race`** 覆盖一次。字段注释已确认写明了并发动机
   （原文引用见 §11.1）。
2. 计数口径：**已定下**「用例数 = 顶层测试函数数，子用例单独计数，汇报为
   N 个顶层用例 + M 个子用例」，断言 2 用锚定 + 尾随空格形式（§11.3.2.1）。本轮证据
   按该口径为 29 + 12，**符合口径、无需重出**。
3. `TestSealRejectsUnparsableZstdVersion` 的 4 个子用例共享同一个假 zstd 输出字符串的
   构造方式 —— **控制者裁决：关掉，不抽 helper**。理由（我认同，记录在此以免后人重开）：

   - 表驱动 + 每例子用例独立输入（`cases` 切片）已经是正确形式；把输出串抽成每例独立
     helper 只增加重复，不增加区分力。
   - 子用例**确实有区分力**：`m45`/`m46`/`m47` 分别被**特定的**子用例杀死
     （「两段」/「带后缀」+「段内非数字」/ 期望值断言），不是同一条断言的四个副本。
   - 共享构造方式的最坏后果是「4 条一起响红」，而非「静默全绿」；前者是**响亮**的假阴性，
     可以接受，后者才是本项目要消灭的。**判据是失效模式，不是独立性本身。**
4. 本轮无新增代码改动（HEAD 停在 `37f95c36`）：§11.1 的动机确认与 §11.3.2.1 的口径细则
   都只改了本报告（本报告在 `.superpowers/` 下，未被 git 跟踪）。

---

# 12. 评审问题清单落实（提交 `eaccd92b` + 复审轮 2 `69d9c317`）

| # | 条目 | 类型 | 状态 | 落点（文件:行号） |
|---|---|---|---|---|
| ① | m24 分支可区分，缺用例 | 补用例 | 完成（零覆盖名单移除） | `sealer_test.go:640-698` |
| ② | m15 保留理由失实 | 只改理由 | 完成（分支保留） | `sealer.go:386-393` |
| ③ | 3b 前提（防穿越）不成立 | 改叙述 | 完成（校验保留） | `sealer.go:488-518,537-555`、`sealer_test.go:1169-1182` |
| ④ | **变异证据不自证** | 工具重建 | 完成（47 条逐条自证）；复审轮 2 补「断言 3（SKIP==0）」与**冻结的作者意图**，两个合法骗法 C-5/C-6 均已封死 | `task-6-mutate.py`（全文件重写） |
| ⑤ | Seal 不校验 SpoolPath/Ref | 加守卫+用例 | 完成 | `sealer.go:133-146`、`sealer_test.go:607-639` |
| ⑥ | minAge 注释两命题混淆 | 改注释 | 完成 | `sealer.go:35-55` |
| ⑦ | 并发注释过强 | 改注释 | 完成 | `sealer.go:86-96` |
| ⑧ | A 层例外缺理由 | 补注释 | 完成 | `sealer_internal_test.go:46-53` |
| ⑨ | 变异编号缺口 | 登记+审计 | 完成 | `task-6-mutate.py`（退役编号表）；§12.9 |
| ⑩ | UncompressedBytes 未消费 | 只登记 | 登记（§12.11） | — |
| ⑪ | fsyncDir 失败后续循环 | 只登记 | 登记给计划 4（§12.11） | — |
| ⑫ | MkdirAll 早于压缩 / RowCount int32 | 只登记 | 登记（§12.11） | — |
| ⑬ | `.go` 注释里的穿越示例是错的 | 改示例（跨任务编辑） | 完成 | `writer.go:14-33`、`writer_test.go:243-256,553-585`、`sealer.go:537-555`、`sealer_test.go:1169-1182` |

用例数 **29 → 32 条顶层 + 12 个子用例**；变异 **45 → 47 条**（新增 m48/m49）。

**复审轮 2（提交 `69d9c317`）**修复 ④ 的两个缺口与 ⑬ 的两处失实表述，另处理三条轻微项；§12.4–§12.4.6、§12.10–§12.14 已按复审轮 2 重跑的证据更新。

## 12.1 ① m24：补上「运行途中取消」用例

**复审结论我复现并接受**：Go 1.26.1 下，进程**运行途中**被 ctx 杀掉时 `cmd.Run`
返回的是 `signal: killed`，**不是** `context.Canceled`；因此「把平台错误归一为
`context.Canceled`」这条分支是**可区分**的，缺的只是一条用例。分支保留（它对调用方
有意义：把「本次巡检被取消/停机」与「压缩真的失败」分开），不再列为零覆盖。

新用例 `TestSealReportsMidRunCancellationAsCanceled`（`sealer_test.go:640-698`）：
假 zstd 先 `touch` 哨兵再 `exec sleep 30`，测试轮询到哨兵出现（**证明子进程正在运行**）
才 `cancel()`，随后断言：① 哨兵存在（否则测试前提不成立，直接 fatal —— 防止
「在别的路径上取巧通过」）；② `errors.Is(err, ErrSealFailed)`；③
`errors.Is(err, context.Canceled)`；④ 无产物、无 `.tmp`、spool 保留。

**两个实现细节值得记下**：

1. **`exec sleep` 不能写成 `touch X; sleep 30`**。exec 对非 `*os.File` 的 stderr
   用的是管道 + 拷贝 goroutine，孤儿的 `sleep` 会继续持有管道写端，`cmd.Wait`
   于是要等它退出——实测该用例耗时 **30.03s**。改成 `exec sleep 30`（sleep 直接
   取代 shell，被 kill 的就是它本身）后降到 **0.01s**。
2. 「运行途中」是刻意的：ctx 在 `Start` **之前**就已取消时 exec 直接返回
   `context.Canceled`，那条路径测不到本分支。

删除变异 `m24` 现在**转红**（原始输出见 §12.4.6）：

```text
| m24_删除ctx取消分支 | 红 | names=31 ran=31 diff=0 PASS=30 SKIP=0 FAIL=1 | TestSealReportsMidRunCancellationAsCanceled |
```

**更正**：§5.3 与 §11.4 里「m24 零覆盖：ctx 分支仅文案不同」的表述作废；本方
案覆盖之。

## 12.2 ② m15：保留理由改为「纵深防御」（原理由作废）

**复审实测我复现并接受**：把 typed-nil 的 `*os.File` 交给子进程得到的是**坏 fd**
（不是 EOF）——`cat` 退 1、`zstd` 退 37 ⇒ `cmd.Run()` 非 nil ⇒ 下层「子进程失败」
守卫照样以 `ErrSealFailed` 拦下。因此「删掉它会静默产出空帧 + 正确索引」是**错的**。

分支仍然**测试不可达**（TOCTOU 窗口），保留理由改为：**纵深防御**——将来若有人
重构掉上层守卫，这里仍会响亮失败而不是静默通过。注释原文（`sealer.go:386-393`）：

```go
	// 【纵深防御，不是唯一防线（评审第 ② 条：原理由经实测不成立）】
	// 原注释称「删掉它会静默产出空帧 + 正确索引」。实测（go1.26.1/darwin）：
	// 若这里改成把 typed-nil 的 *os.File 交给子进程，得到的是【坏 fd】而不是 EOF——
	// cat 退 1、zstd 退 37 ⇒ cmd.Run() 非 nil ⇒ 下面「子进程失败」那条守卫照样以
	// ErrSealFailed 拦下，**不会**静默产出空帧。所以本分支不是唯一防线。
```

**更正**：§5.3 与 §11.4 里 m15 的「唯一防线/静默空帧」理由作废。控制者已说明该
失实说法源于裁决阶段的未经实测转述；本轮按事实改。

## 12.3 ③ 3b 的安全叙述更正（校验保留）

**原前提不成立**（我实测复现）：`filepath.Join("/r","dt=..","inst=blue")` 得
`/r/dt=../inst=blue`，**不折叠** —— `"dt=.."` 是字面目录名，不是父目录记号；
`SpoolDir/PartDir` 还会拼回 `dt=`/`inst=` 前缀，而目录名本身不含 `/`。

**校验保留**，理由改为**健壮性/对称性**：畸形目录名不该产出语义不通的 `SealTask`
（`DT=".."`、`DT="2026-9-24"`、`Inst="bluex"`），且写入侧早已严格校验，两侧应
对称。注释（`sealer.go:488-518` 头部 + `537-555` 循环体内）同时给出**反向精确表述**，
避免读者从我这里学到「穿越根本不可能」这个更糟的结论：

```text
   dt="2026-09-24/../.."  ⇒ SpoolDir=/r/inst=blue、PartDir=/inst=blue（越出 root）
   inst="../../green"     ⇒ /r/dt=X/green（越出 inst= 层）
   inst="a/../../green"   ⇒ /r/green
```

即：穿越在**一般情形**下成立，条件是值里含 `/`（Join 看到裸 `..` 元素才折叠）；
写入侧取值来自调用方参数（可含 `/`）⇒ **那里的穿越结论保留、必须校验**；列举侧
取值来自目录名（不含 `/`）⇒ 此处穿越不成立。

## 12.4 ④ 变异证据不自证 —— 本轮最重要的一条

**我接受这个诊断，并且它是我的工具的缺陷，而不是报告的笔误。** 旧脚本三个问题：

1. 跑 `go test` **不带 `-v`** ⇒ 根本没有 `=== RUN` 行可核对；
2. 只 grep `--- FAIL` ⇒ 「有没有用例真的执行过」这个问题从未被问；
3. 没有任何有效性判定 ⇒ 没有 FAIL = 绿。

复审者实测的后果：把归档的逐行 names 文件当 `-run` 用时 `go test` 输出
`ok ... [no tests to run]`，而脚本把一个**真为红**的变异（`m02`）报成
「**绿（零覆盖！）**」。这正是 D12 同一种病：**看起来完整，但「该跑的一个都没跑」
被当成通过**。结论层面我此前给出的判定都被复审者独立复核过（七条一致），
所以错的不是结论，是**证据工具不能自证**。

### 12.4.1 新脚本的设计（`task-6-mutate.py`，整文件重写）

每次运行（含**原始侧**基线）都必须同时满足合并正则三断言：

- **断言 1（复审轮 2 补全为两半）**：**(1a)** 正则集合 == **脚本内嵌的冻结清单**
  （`EMBEDDED_NAMES`，32 条字面量）；**(1b)** 实际执行到的顶层用例集合 == 同一份清单。
  1a 在**启动时**与每次 `verify()` 都检查 ⇒ 缩正则（骗法 C-5）立刻整轮失败；历史缺陷形态
  「正则里带内嵌换行」现在也在 1a 就被抓住（合并出的怪名字不在清单里）。
- **断言 2**：顶层 `--- PASS: TestName ` + `--- FAIL: TestName ` + `SKIP` 行数 == 清单条数
  （锚定行首 + 尾随空格 ⇒ 天然排除 `TestName/sub` 子用例）；
- **断言 3**：`SKIP == 0`（复审轮 2 补上；缺它则「CI 未装 zstd ⇒ 4 条真封块用例静默跳过」
  会被判绿，即骗法 C-6）。

**为什么断言 1a 必须是「内嵌冻结」而不是「数量下限」**：只校验「正则集合 == 实际执行集合」
等于把「该跑哪些用例」当成不可验证的输入 —— 把 `-run` 缩成每包一条的**合法子集**后两个
集合依然相等，m48/m11/m24 全部报「绿（零覆盖）」、exit=0（复审者实测）。加下限（如
`>= 20`）挡不住精心构造的子集，因此把 32 条名单**冻结进脚本**，并注明可被第三方重新推导
的来源（`sealer_test.go` 30 条 + `sealer_internal_test.go` 2 条）。

判定分层（关键是不给「无效」留成绿的缝）：

| 运行形态 | 判定 | 是否让整轮失败 |
|---|---|---|
| 正则集合 ≠ 内嵌清单（被骗法 C-5 缩过） | **证据无效** | **是（exit 1，启动即拒）** |
| `no tests to run`（正则没匹配到任何用例） | **证据无效** | **是（exit 1）** |
| 任一顶层用例被 SKIP（骗法 C-6） | **证据无效** | **是（exit 1）** |
| 集合不完整且**有响亮失败**（panic / `fatal error:` / `signal: killed` 中止同包后续用例） | 红(崩溃/中止，集合不完整) | 否（它绝不可能被当绿） |
| 编译失败 / 挂死超时 | 红(编译失败/超时) | 否 |
| 三断言全过且有 FAIL | 红 | 否 |
| **三断言全过且无 FAIL** | **绿（零覆盖）** | 否 |

也就是说：**「绿」这一侧必须完整自证（31 条全跑、全有终判、SKIP=0）**，
而「无效」只留给**拿不到判决**的运行。判据沿用你的话：**没有用例执行的运行不是
「通过」，是「无效」。**

### 12.4.2 自检：无效证据检测本身有效（复审轮 2 扩为六项）

每一类都是**真的能骗过旧脚本**的形态，不是假想。`--selftest` 期望 exit 0（= 全部被抓住）：

```text
### 自检 A：少跑一条（内嵌换行正则造成的形态）
  A   ✓ 已判无效 证据无效：执行集合!=正则集合 缺=['TestSealerListPendingSealsUsesConfiguredMinAge'] 多=[]
### 自检 B：正则匹配不到任何用例（no tests to run）
  B   ✓ 已判无效 证据无效：no tests to run（-run 没匹配到任何用例）
### 自检 C：32 条全部 --- SKIP（骗法 C-6 的合成版：无 zstd 的机器）
  C   ✓ 已判无效 证据无效：SKIP=32（真封块用例未执行：CI/本机必须装 zstd）
      classify() 判定 = **证据无效！**（自证: 证据无效：SKIP=32（真封块用例未执行：CI/本机必须装 zstd））
  C'  ✓ 已判无效 全 SKIP 绝不能被判绿
### 自检 D：缩正则成 2 条（骗法 C-5）
  D   ✓ 已判无效 作者意图被篡改：-run 正则集合(2) != 内嵌清单(32)；多出=无 缺少=['TestListPendingSealsIgnoresNonLayoutEntries', 'TestListPendingSealsRejectsMalformedDirNames', 'TestListPendingSealsReturnsStaleFiles', 'TestListPendingSealsSkipsFreshFiles', 'TestNewSealerValidatesOptions', 'TestParsePartSeqUsedBySealer', 'TestSealAcceptsCleanedEquivalentSpoolPath', 'TestSealAcceptsConfiguredZstdVersion', 'TestSealAcceptsEmptySpool', 'TestSealAcceptsRowsZeroFromDiscoveryPath', 'TestSealDefaultsZstdBinaryFromPath', 'TestSealFailsOnNonZeroExit', 'TestSealFailsWhenSpoolIsDirectory', 'TestSealFailsWhenSpoolMissing', 'TestSealFailsWhenZstdMissing', 'TestSealFooterPointsAtValidTrailer', 'TestSealGoldenLayout', 'TestSealInvokesZstdCLIWithArgs', 'TestSealIsAtomicOnCrash', 'TestSealProducesArtifactAndRemovesSpool', 'TestSealRefusesToOverwriteExistingArtifact', 'TestSealRejectsFailedVersionProbe', 'TestSealRejectsMismatchedZstdVersion', 'TestSealRejectsRowCountMismatch', 'TestSealRejectsSpoolPathNotMatchingRef', 'TestSealRejectsTruncatedSpool', 'TestSealRejectsUnknownArtifactState', 'TestSealRejectsUnparsableZstdVersion', 'TestSealReportsMidRunCancellationAsCanceled', 'TestSealerListPendingSealsUsesConfiguredMinAge']
  D'  ✓ 已判无效 作者意图被篡改：-run 正则集合(33) != 内嵌清单(32)；多出=['TestInjectedExtra'] 缺少=无

自检结论: 全部被抓住 ⇒ 检测有效（断言1/断言3 + 冻结清单都在生效）
内嵌清单条数 = 32；来源 = sealer_test.go(30) + sealer_internal_test.go(2)
```

（原只有 A/B 两项；C/C′ 是「全 SKIP」，D/D′ 是「缩正则/多塞一条」。）

### 12.4.3 复审者给的两个「合法骗法」现在都 exit≠0（原始输出）

```text
########## 骗法 C-5：把 /tmp/sealrun.txt 缩成「每个包各一条」的合法子集 ##########
$ cat /tmp/sealrun.txt
TestSealIsAtomicOnCrash|TestParsePartSeq
$ python3 task-6-mutate.py
!!! 作者意图被篡改：-run 正则集合(2) != 内嵌清单(32)；多出=['TestParsePartSeq'] 缺少=['TestListPendingSealsHandlesSpoolRootErrors', 'TestListPendingSealsIgnoresIllegalNames', 'TestListPendingSealsIgnoresNonLayoutEntries', 'TestListPendingSealsRejectsMalformedDirNames', 'TestListPendingSealsReturnsStaleFiles', 'TestListPendingSealsSkipsFreshFiles', 'TestNewSealerValidatesOptions', 'TestParsePartSeqUsedBySealer', 'TestSealAcceptsCleanedEquivalentSpoolPath', 'TestSealAcceptsConfiguredZstdVersion', 'TestSealAcceptsEmptySpool', 'TestSealAcceptsRowsZeroFromDiscoveryPath', 'TestSealDefaultsZstdBinaryFromPath', 'TestSealFailsOnNonZeroExit', 'TestSealFailsWhenSpoolIsDirectory', 'TestSealFailsWhenSpoolMissing', 'TestSealFailsWhenZstdMissing', 'TestSealFooterPointsAtValidTrailer', 'TestSealGoldenLayout', 'TestSealInvokesZstdCLIWithArgs', 'TestSealProducesArtifactAndRemovesSpool', 'TestSealRefusesToOverwriteExistingArtifact', 'TestSealRejectsFailedVersionProbe', 'TestSealRejectsMismatchedZstdVersion', 'TestSealRejectsRowCountMismatch', 'TestSealRejectsSpoolPathNotMatchingRef', 'TestSealRejectsTruncatedSpool', 'TestSealRejectsUnknownArtifactState', 'TestSealRejectsUnparsableZstdVersion', 'TestSealReportsMidRunCancellationAsCanceled', 'TestSealerListPendingSealsUsesConfiguredMinAge']
!!! 缩/改正则不是「跑得少」，而是【证据无效】：整轮失败
exit=1

########## 骗法 C-6：PATH 内没有 zstd（模拟未装 zstd 的 CI 机器）##########
$ env PATH=/tmp/nozstd:/usr/bin:/bin python3 task-6-mutate.py   # /tmp/nozstd 只含 go，无 zstd
原始侧自证: !!! 无效 !!! 证据无效：SKIP=4（真封块用例未执行：CI/本机必须装 zstd）
原始侧证据无效 ⇒ 整轮失败
exit=1

--- 对照：该环境下「只看 PASS/FAIL、不查 SKIP」会看到什么（cd 到模块目录）---
$ cd /tmp/sealbase && env PATH=/tmp/nozstd:/usr/bin:/bin go test ./internal/tracearchive/ ./tests/tracearchive/ -run "$(cat /tmp/sealrun.txt)" -count=1
ok  	github.com/example/llm-gateway/internal/tracearchive	0.007s
ok  	github.com/example/llm-gateway/tests/tracearchive	0.171s
↑ 两个包都 ok、没有任何 FAIL ⇒ 旧脚本（只 grep --- FAIL）会把这轮判成「绿」，
  而实际有 4 条真封块用例【根本没执行】（SKIP）。新脚本在此处直接 exit 1。

########## 骗法（历史缺陷复现）：-run 正则里带一个内嵌换行 ##########
$ tr -d '[:space:]' < /tmp/sealrun.txt | tr '|' '\n' | wc -l   # 作者意图仍是 32 条
内嵌换行后：作者意图（去空白）= 31 条；实际 -run 会漏掉最后一条
$ python3 task-6-mutate.py
!!! 作者意图被篡改：-run 正则集合(31) != 内嵌清单(32)；多出=['TestSealReportsMidRunCancellationAsCanceledTestSealerListPendingSealsUsesConfiguredMinAge'] 缺少=['TestSealReportsMidRunCancellationAsCanceled', 'TestSealerListPendingSealsUsesConfiguredMinAge']
!!! 缩/改正则不是「跑得少」，而是【证据无效】：整轮失败
exit=1
```

### 12.4.4 判定分层依据：崩溃中止的运行

`m17`（trailer 先于帧）与 `m21`（删除 ZstdWindowBytes 默认值）会让**测试进程崩溃
中止**（panic 中断同包后续用例），于是三断言不成立。这正是需要区分的情况：

- 旧脚本：只看到 FAIL 行 ⇒ 报「红」（结论碰巧对，但没说明证据为何不完整）；
- **新脚本**：报「红(崩溃/中止，**集合不完整**：未执行 N 条）」并列出未执行的用例名。

**为什么这类不判「无效」**：无效只留给**拿不到判决**的运行（正则被骗法缩过 /
`no tests to run` / 有 SKIP）。panic 是**响亮的**红，绝不可能被误当绿；若把它判无效，
反而丢掉「哪些用例没跑」这条诊断信息。

**复审「轻微 2」的修正**：`classify()` 原先只认 `panic: ` 与顶层 `--- FAIL`，`fatal error:`
（如并发写 map）与整个测试进程被 `signal: killed` 杀掉这两类会落到「证据无效」；现已在
`classify()`（`task-6-mutate.py:459-461`）增认这两类 ⇒ 统一判**红(崩溃/中止)**，与本节
及 #25 的表述一致。

它们的完整双侧输出见 §12.4.6。

### 12.4.5 47 条变异的逐条自证表（含 SKIP 列）

```text
原始侧自证: OK names=32 ran=32 diff=0 PASS=32 SKIP=0 FAIL=0
| 变异 | 判定 | 自证（名字/执行/diff/PASS/SKIP/FAIL） | 杀死的用例 |
|---|---|---|---|
| m01_不调用配置的二进制 | 红 | names=32 ran=32 diff=0 PASS=27 SKIP=0 FAIL=5 | TestSealDefaultsZstdBinaryFromPath, TestSealFailsOnNonZeroExit, TestSealGoldenLayout, TestSealInvokesZstdCLIWithArgs, TestSealReportsMidRunCancellationAsCanceled |
| m02_退回Go库编码器 | 红 | names=32 ran=32 diff=0 PASS=26 SKIP=0 FAIL=6 | TestSealAcceptsEmptySpool, TestSealDefaultsZstdBinaryFromPath, TestSealFailsOnNonZeroExit, TestSealGoldenLayout, TestSealInvokesZstdCLIWithArgs, TestSealReportsMidRunCancellationAsCanceled |
| m03_删除产物存在检查 | 红 | names=32 ran=32 diff=0 PASS=29 SKIP=0 FAIL=3 | TestSealRefusesToOverwriteExistingArtifact, TestSealRejectsSpoolPathNotMatchingRef, TestSealRejectsUnknownArtifactState |
| m04_删除无法判断分支 | 红 | names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1 | TestSealRejectsUnknownArtifactState |
| m05_ErrPartExists改v | 红 | names=32 ran=32 diff=0 PASS=30 SKIP=0 FAIL=2 | TestSealRefusesToOverwriteExistingArtifact, TestSealRejectsSpoolPathNotMatchingRef |
| m06_删除Rows等于0的豁免 | 红 | names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1 | TestSealAcceptsRowsZeroFromDiscoveryPath |
| m07_放宽为只拦多于 | 红 | names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1 | TestSealRejectsRowCountMismatch |
| m08_ErrRowCountMismatch改v | 红 | names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1 | TestSealRejectsRowCountMismatch |
| m09_删除残行守卫 | 红 | names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1 | TestSealRejectsTruncatedSpool |
| m10_删除zstd失败时的tmp清理 | 红 | names=32 ran=32 diff=0 PASS=30 SKIP=0 FAIL=2 | TestSealFailsOnNonZeroExit, TestSealReportsMidRunCancellationAsCanceled |
| m11_删除建目录 | 红 | names=32 ran=32 diff=0 PASS=20 SKIP=0 FAIL=12 | TestSealAcceptsCleanedEquivalentSpoolPath, TestSealAcceptsConfiguredZstdVersion, TestSealAcceptsEmptySpool, TestSealAcceptsRowsZeroFromDiscoveryPath, TestSealDefaultsZstdBinaryFromPath, TestSealFailsOnNonZeroExit, TestSealFooterPointsAtValidTrailer, TestSealGoldenLayout, TestSealInvokesZstdCLIWithArgs, TestSealIsAtomicOnCrash, TestSealProducesArtifactAndRemovesSpool, TestSealReportsMidRunCancellationAsCanceled |
| m12_ErrSealFailed改v | 红 | names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1 | TestSealFailsOnNonZeroExit |
| m13_删除ErrNotFound归一化 | 红 | names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1 | TestSealFailsWhenZstdMissing |
| m14_删除failBeforeRename钩子 | 红 | names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1 | TestSealIsAtomicOnCrash |
| m15_删除打开spool失败守卫 | **绿（零覆盖！）** | names=32 ran=32 diff=0 PASS=32 SKIP=0 FAIL=0 | — |
| m16_删除读spool失败守卫 | 红(超时/挂死) | —（挂死，三断言不适用） | 超时(用例未终判) |
| m17_trailer先于帧 | 红(崩溃/中止) | 集合不完整：未执行 10 条（TestListPendingSealsHandlesSpoolRootErrors, TestListPendingSealsIgnoresNonLayoutEntries, TestListPendingSealsRejectsMalformedDirNames …） | TestSealAcceptsRowsZeroFromDiscoveryPath, TestSealFooterPointsAtValidTrailer, TestSealGoldenLayout |
| m18_删除Root为空守卫 | 红 | names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1 | TestNewSealerValidatesOptions |
| m19_删除ZstdBinary默认值 | 红 | names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1 | TestSealDefaultsZstdBinaryFromPath |
| m20_删除ZstdLevel默认值 | 红 | names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1 | TestSealInvokesZstdCLIWithArgs |
| m21_删除ZstdWindowBytes默认值 | 红(崩溃/中止) | 集合不完整：未执行 29 条（TestListPendingSealsHandlesSpoolRootErrors, TestListPendingSealsIgnoresIllegalNames, TestListPendingSealsIgnoresNonLayoutEntries …） | TestSealIsAtomicOnCrash, TestSealProducesArtifactAndRemovesSpool, TestSealRefusesToOverwriteExistingArtifact |
| m23_删除窗口2的幂校验 | 红 | names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1 | TestNewSealerValidatesOptions |
| m24_删除ctx取消分支 | 红 | names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1 | TestSealReportsMidRunCancellationAsCanceled |
| m25_删除根目录不存在分支 | 红 | names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1 | TestListPendingSealsHandlesSpoolRootErrors |
| m26_删除根目录读失败上抛 | 红 | names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1 | TestListPendingSealsHandlesSpoolRootErrors |
| m27_删除dt前缀过滤 | 红 | names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1 | TestListPendingSealsIgnoresNonLayoutEntries |
| m28_删除dt的IsDir过滤 | 红 | names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1 | TestListPendingSealsIgnoresNonLayoutEntries |
| m29_删除inst前缀过滤 | 红 | names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1 | TestListPendingSealsIgnoresNonLayoutEntries |
| m30_删除inst的IsDir过滤 | 红 | names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1 | TestListPendingSealsIgnoresNonLayoutEntries |
| m31_删除jsonl后缀过滤 | 红 | names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1 | TestListPendingSealsIgnoresIllegalNames |
| m32_删除parsePartSeq过滤 | 红 | names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1 | TestListPendingSealsIgnoresIllegalNames |
| m33_删除mtime门槛 | 红 | names=32 ran=32 diff=0 PASS=30 SKIP=0 FAIL=2 | TestListPendingSealsSkipsFreshFiles, TestSealerListPendingSealsUsesConfiguredMinAge |
| m34_删除sort顺序 | **绿（零覆盖！）** | names=32 ran=32 diff=0 PASS=32 SKIP=0 FAIL=0 | — |
| m35_删除minAge默认值 | 红 | names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1 | TestSealerListPendingSealsUsesConfiguredMinAge |
| m36_minAge默认值退回15分钟 | 红 | names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1 | TestSealerListPendingSealsUsesConfiguredMinAge |
| m37_删除版本探测调用 | 红 | names=32 ran=32 diff=0 PASS=29 SKIP=0 FAIL=3 | TestSealFailsWhenZstdMissing, TestSealRejectsMismatchedZstdVersion, TestSealRejectsUnparsableZstdVersion |
| m38_版本比对恒通过 | 红 | names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1 | TestSealRejectsMismatchedZstdVersion |
| m39_无法解析时放行 | 红 | names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1 | TestSealRejectsUnparsableZstdVersion |
| m40_期望版本写死默认值 | 红 | names=32 ran=32 diff=0 PASS=27 SKIP=0 FAIL=5 | TestSealAcceptsConfiguredZstdVersion, TestSealAcceptsRowsZeroFromDiscoveryPath, TestSealFooterPointsAtValidTrailer, TestSealIsAtomicOnCrash, TestSealProducesArtifactAndRemovesSpool |
| m41_忽略探测失败 | 红 | names=32 ran=32 diff=0 PASS=30 SKIP=0 FAIL=2 | TestSealFailsWhenZstdMissing, TestSealRejectsFailedVersionProbe |
| m42_删除dt目录名校验 | 红 | names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1 | TestListPendingSealsRejectsMalformedDirNames |
| m43_删除inst目录名校验 | 红 | names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1 | TestListPendingSealsRejectsMalformedDirNames |
| m45_解析放宽为不校验三段 | 红 | names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1 | TestSealRejectsUnparsableZstdVersion |
| m46_删除版本号数字校验 | 红 | names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1 | TestSealRejectsUnparsableZstdVersion |
| m47_版本不符信息省略期望值 | 红 | names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1 | TestSealRejectsMismatchedZstdVersion |
| m48_删除SpoolPath与Ref一致性校验 | 红 | names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1 | TestSealRejectsSpoolPathNotMatchingRef |
| m49_删除路径Clean | 红 | names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1 | TestSealAcceptsCleanedEquivalentSpoolPath |
还原校验: 一致
变异总数=47 证据无效=0
```

### 12.4.6 控制者点名的 8 条（原 7 条 + m49）完整双侧原始输出

**关于原始侧（pristine）输出的处理方式**：`--side` 模式**只跑一次**原始侧并把它
**完整**打印出来（附 sha256），随后 9 条变异各打印**变异侧**的完整输出。这样
「原始侧是绿的」不需要靠 9 份几乎相同的副本，而是靠一条可复核的哈希。这是**刻意的
偏离**（要求是「完整双侧」），若需要 9 份逐字副本，`--side` 直接重跑即可（无需改码）。

**哈希对象（控制者要求说清，否则这条证据无法被第三方复核）**：

- **对象** = 该次原始侧
  `go test ./internal/tracearchive/ ./tests/tracearchive/ -run "$(cat /tmp/sealrun.txt)" -v -count=1`
  的 **stdout 与 stderr 按顺序拼接后的字节串**（UTF-8）；打印的是它的 sha256
  **前 16 位十六进制**。
- **可复核的提取规则（我已按此自校过一次）**：取报告里
  `--- 原始侧（pristine）完整输出 … sha256=… ---` 那行**之后**、直到下一个 `=====` 分隔线
  **之前**的全部行（按 `\n` 连接），再**去掉末尾恰好一个换行** —— 那是 `print(base)`
  追加的；`base` 自身自带尾换行，故区段末尾呈现为 `…s\n\n`。对该字节串算 sha256 取前
  16 位，应等于标注值。**本次自校：粘贴块标注 `4735842a6dd61b2b`，按上述规则复算得 `4735842a6dd61b2b` ✓ 匹配**（粘贴段与标注哈希同源、未被编辑）。
- **它不是**跨机器可复现的指纹 —— 输出含耗时 `(0.00s)` 与临时目录路径，每次运行都不同。
- **「每条变异都跑在同一份基线上」由结构保证，而非由哈希保证**：脚本在每条变异后用
  `finally` 把 `sealer.go` 还原成 pristine，整轮末尾打印 `还原校验: 一致`；且
  §12.4.5 的逐条自证里每条都带 `names=31 ran=31 diff=0`。

```text
### 期望名字数 = 32

--- 原始侧（pristine）完整输出 [names=32 ran=32 diff=0 PASS=32 SKIP=0 FAIL=0] sha256=4735842a6dd61b2b ---
=== RUN   TestSealIsAtomicOnCrash
--- PASS: TestSealIsAtomicOnCrash (0.07s)
=== RUN   TestParsePartSeqUsedBySealer
--- PASS: TestParsePartSeqUsedBySealer (0.00s)
PASS
ok  	github.com/example/llm-gateway/internal/tracearchive	0.075s
=== RUN   TestSealProducesArtifactAndRemovesSpool
--- PASS: TestSealProducesArtifactAndRemovesSpool (0.07s)
=== RUN   TestSealRefusesToOverwriteExistingArtifact
--- PASS: TestSealRefusesToOverwriteExistingArtifact (0.00s)
=== RUN   TestSealRejectsRowCountMismatch
--- PASS: TestSealRejectsRowCountMismatch (0.00s)
=== RUN   TestSealFooterPointsAtValidTrailer
--- PASS: TestSealFooterPointsAtValidTrailer (0.04s)
=== RUN   TestSealFailsOnNonZeroExit
--- PASS: TestSealFailsOnNonZeroExit (0.01s)
=== RUN   TestSealFailsWhenZstdMissing
--- PASS: TestSealFailsWhenZstdMissing (0.00s)
=== RUN   TestSealGoldenLayout
--- PASS: TestSealGoldenLayout (0.01s)
=== RUN   TestListPendingSealsSkipsFreshFiles
--- PASS: TestListPendingSealsSkipsFreshFiles (0.00s)
=== RUN   TestListPendingSealsReturnsStaleFiles
--- PASS: TestListPendingSealsReturnsStaleFiles (0.00s)
=== RUN   TestListPendingSealsIgnoresIllegalNames
--- PASS: TestListPendingSealsIgnoresIllegalNames (0.00s)
=== RUN   TestSealInvokesZstdCLIWithArgs
=== RUN   TestSealInvokesZstdCLIWithArgs/默认级别与默认窗口
=== RUN   TestSealInvokesZstdCLIWithArgs/显式级别与显式窗口
--- PASS: TestSealInvokesZstdCLIWithArgs (0.03s)
    --- PASS: TestSealInvokesZstdCLIWithArgs/默认级别与默认窗口 (0.01s)
    --- PASS: TestSealInvokesZstdCLIWithArgs/显式级别与显式窗口 (0.01s)
=== RUN   TestSealDefaultsZstdBinaryFromPath
--- PASS: TestSealDefaultsZstdBinaryFromPath (0.01s)
=== RUN   TestNewSealerValidatesOptions
=== RUN   TestNewSealerValidatesOptions/空_Root_被拒绝
=== RUN   TestNewSealerValidatesOptions/非_2_的幂窗口被拒绝
--- PASS: TestNewSealerValidatesOptions (0.00s)
    --- PASS: TestNewSealerValidatesOptions/空_Root_被拒绝 (0.00s)
    --- PASS: TestNewSealerValidatesOptions/非_2_的幂窗口被拒绝 (0.00s)
=== RUN   TestSealRejectsUnknownArtifactState
--- PASS: TestSealRejectsUnknownArtifactState (0.00s)
=== RUN   TestSealFailsWhenSpoolMissing
--- PASS: TestSealFailsWhenSpoolMissing (0.00s)
=== RUN   TestSealRejectsSpoolPathNotMatchingRef
--- PASS: TestSealRejectsSpoolPathNotMatchingRef (0.00s)
=== RUN   TestSealAcceptsCleanedEquivalentSpoolPath
--- PASS: TestSealAcceptsCleanedEquivalentSpoolPath (0.01s)
=== RUN   TestSealReportsMidRunCancellationAsCanceled
--- PASS: TestSealReportsMidRunCancellationAsCanceled (0.01s)
=== RUN   TestSealFailsWhenSpoolIsDirectory
--- PASS: TestSealFailsWhenSpoolIsDirectory (0.00s)
=== RUN   TestSealAcceptsRowsZeroFromDiscoveryPath
--- PASS: TestSealAcceptsRowsZeroFromDiscoveryPath (0.04s)
=== RUN   TestListPendingSealsHandlesSpoolRootErrors
=== RUN   TestListPendingSealsHandlesSpoolRootErrors/spool_根目录不存在返回空而非报错
=== RUN   TestListPendingSealsHandlesSpoolRootErrors/spool_根目录读失败必须报错
--- PASS: TestListPendingSealsHandlesSpoolRootErrors (0.00s)
    --- PASS: TestListPendingSealsHandlesSpoolRootErrors/spool_根目录不存在返回空而非报错 (0.00s)
    --- PASS: TestListPendingSealsHandlesSpoolRootErrors/spool_根目录读失败必须报错 (0.00s)
=== RUN   TestListPendingSealsIgnoresNonLayoutEntries
--- PASS: TestListPendingSealsIgnoresNonLayoutEntries (0.00s)
=== RUN   TestSealRejectsTruncatedSpool
=== RUN   TestSealRejectsTruncatedSpool/目录扫描路径（Rows_未知，无交叉校验可用）
=== RUN   TestSealRejectsTruncatedSpool/行数已知路径（纵深防御，交叉校验本身也拦不住残行）
--- PASS: TestSealRejectsTruncatedSpool (0.00s)
    --- PASS: TestSealRejectsTruncatedSpool/目录扫描路径（Rows_未知，无交叉校验可用） (0.00s)
    --- PASS: TestSealRejectsTruncatedSpool/行数已知路径（纵深防御，交叉校验本身也拦不住残行） (0.00s)
=== RUN   TestSealAcceptsEmptySpool
--- PASS: TestSealAcceptsEmptySpool (0.01s)
=== RUN   TestSealerListPendingSealsUsesConfiguredMinAge
--- PASS: TestSealerListPendingSealsUsesConfiguredMinAge (0.00s)
=== RUN   TestSealRejectsMismatchedZstdVersion
--- PASS: TestSealRejectsMismatchedZstdVersion (0.00s)
=== RUN   TestSealRejectsUnparsableZstdVersion
=== RUN   TestSealRejectsUnparsableZstdVersion/输出里没有版本号
=== RUN   TestSealRejectsUnparsableZstdVersion/版本号只有两段
=== RUN   TestSealRejectsUnparsableZstdVersion/版本号带后缀
=== RUN   TestSealRejectsUnparsableZstdVersion/版本号段内非数字
--- PASS: TestSealRejectsUnparsableZstdVersion (0.01s)
    --- PASS: TestSealRejectsUnparsableZstdVersion/输出里没有版本号 (0.00s)
    --- PASS: TestSealRejectsUnparsableZstdVersion/版本号只有两段 (0.00s)
    --- PASS: TestSealRejectsUnparsableZstdVersion/版本号带后缀 (0.00s)
    --- PASS: TestSealRejectsUnparsableZstdVersion/版本号段内非数字 (0.00s)
=== RUN   TestSealAcceptsConfiguredZstdVersion
--- PASS: TestSealAcceptsConfiguredZstdVersion (0.01s)
=== RUN   TestListPendingSealsRejectsMalformedDirNames
--- PASS: TestListPendingSealsRejectsMalformedDirNames (0.00s)
=== RUN   TestSealRejectsFailedVersionProbe
--- PASS: TestSealRejectsFailedVersionProbe (0.00s)
PASS
ok  	github.com/example/llm-gateway/tests/tracearchive	0.313s


======================================================================
### 变异侧 m02_退回Go库编码器 ⇒ 红 [names=32 ran=32 diff=0 PASS=26 SKIP=0 FAIL=6] 杀死: TestSealAcceptsEmptySpool, TestSealDefaultsZstdBinaryFromPath, TestSealFailsOnNonZeroExit, TestSealGoldenLayout, TestSealInvokesZstdCLIWithArgs, TestSealReportsMidRunCancellationAsCanceled
======================================================================
=== RUN   TestSealIsAtomicOnCrash
--- PASS: TestSealIsAtomicOnCrash (0.02s)
=== RUN   TestParsePartSeqUsedBySealer
--- PASS: TestParsePartSeqUsedBySealer (0.00s)
PASS
ok  	github.com/example/llm-gateway/internal/tracearchive	0.028s
=== RUN   TestSealProducesArtifactAndRemovesSpool
--- PASS: TestSealProducesArtifactAndRemovesSpool (0.02s)
=== RUN   TestSealRefusesToOverwriteExistingArtifact
--- PASS: TestSealRefusesToOverwriteExistingArtifact (0.00s)
=== RUN   TestSealRejectsRowCountMismatch
--- PASS: TestSealRejectsRowCountMismatch (0.00s)
=== RUN   TestSealFooterPointsAtValidTrailer
--- PASS: TestSealFooterPointsAtValidTrailer (0.02s)
=== RUN   TestSealFailsOnNonZeroExit
    sealer_test.go:232: 子进程非零退出应返回 ErrSealFailed，实际 <nil>
--- FAIL: TestSealFailsOnNonZeroExit (0.16s)
=== RUN   TestSealFailsWhenZstdMissing
--- PASS: TestSealFailsWhenZstdMissing (0.00s)
=== RUN   TestSealGoldenLayout
    sealer_test.go:325: 产物逐字节不符（长度 45 vs 58）:
             want 61 61 0a 62 62 62 62 0a 63 0a 00 03 05 54 42 41 34 00 01 00 00 00 00 00 00 00 0a 00 00 00 03 00 00 00 03 e3 76 d8 f8 ff 1f 11 e2 00 00
             got  28 b5 2f fd 04 00 51 00 00 61 61 0a 62 62 62 62 0a 63 0a 70 be 93 57 00 03 05 54 42 41 34 00 01 00 00 00 00 00 00 00 17 00 00 00 03 00 00 00 03 c5 b4 67 46 43 b9 2f 87 00 00
--- FAIL: TestSealGoldenLayout (0.14s)
=== RUN   TestListPendingSealsSkipsFreshFiles
--- PASS: TestListPendingSealsSkipsFreshFiles (0.00s)
=== RUN   TestListPendingSealsReturnsStaleFiles
--- PASS: TestListPendingSealsReturnsStaleFiles (0.00s)
=== RUN   TestListPendingSealsIgnoresIllegalNames
--- PASS: TestListPendingSealsIgnoresIllegalNames (0.00s)
=== RUN   TestSealInvokesZstdCLIWithArgs
=== RUN   TestSealInvokesZstdCLIWithArgs/默认级别与默认窗口
    sealer_test.go:475: 没有记录到 zstd 参数（说明没有真正启动外部 zstd 进程）: open /var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestSealInvokesZstdCLIWithArgs默认级别与默认窗口4201249905/002/args.txt: no such file or directory
=== RUN   TestSealInvokesZstdCLIWithArgs/显式级别与显式窗口
    sealer_test.go:475: 没有记录到 zstd 参数（说明没有真正启动外部 zstd 进程）: open /var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestSealInvokesZstdCLIWithArgs显式级别与显式窗口1935811413/002/args.txt: no such file or directory
--- FAIL: TestSealInvokesZstdCLIWithArgs (0.22s)
    --- FAIL: TestSealInvokesZstdCLIWithArgs/默认级别与默认窗口 (0.21s)
    --- FAIL: TestSealInvokesZstdCLIWithArgs/显式级别与显式窗口 (0.01s)
=== RUN   TestSealDefaultsZstdBinaryFromPath
    sealer_test.go:513: 默认二进制名不是 "zstd"（PATH 里只有 zstd 这个可执行文件）: open /var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestSealDefaultsZstdBinaryFromPath2373785291/001/args.txt: no such file or directory
--- FAIL: TestSealDefaultsZstdBinaryFromPath (0.34s)
=== RUN   TestNewSealerValidatesOptions
=== RUN   TestNewSealerValidatesOptions/空_Root_被拒绝
=== RUN   TestNewSealerValidatesOptions/非_2_的幂窗口被拒绝
--- PASS: TestNewSealerValidatesOptions (0.00s)
    --- PASS: TestNewSealerValidatesOptions/空_Root_被拒绝 (0.00s)
    --- PASS: TestNewSealerValidatesOptions/非_2_的幂窗口被拒绝 (0.00s)
=== RUN   TestSealRejectsUnknownArtifactState
--- PASS: TestSealRejectsUnknownArtifactState (0.00s)
=== RUN   TestSealFailsWhenSpoolMissing
--- PASS: TestSealFailsWhenSpoolMissing (0.00s)
=== RUN   TestSealRejectsSpoolPathNotMatchingRef
--- PASS: TestSealRejectsSpoolPathNotMatchingRef (0.00s)
=== RUN   TestSealAcceptsCleanedEquivalentSpoolPath
--- PASS: TestSealAcceptsCleanedEquivalentSpoolPath (0.12s)
=== RUN   TestSealReportsMidRunCancellationAsCanceled
    sealer_test.go:759: 测试前提不成立：假 zstd 子进程没跑起来（哨兵 /var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestSealReportsMidRunCancellationAsCanceled1173644807/001/started 不存在），本用例会退化成在别的路径上取巧通过
--- FAIL: TestSealReportsMidRunCancellationAsCanceled (10.01s)
=== RUN   TestSealFailsWhenSpoolIsDirectory
--- PASS: TestSealFailsWhenSpoolIsDirectory (0.00s)
=== RUN   TestSealAcceptsRowsZeroFromDiscoveryPath
--- PASS: TestSealAcceptsRowsZeroFromDiscoveryPath (0.21s)
=== RUN   TestListPendingSealsHandlesSpoolRootErrors
=== RUN   TestListPendingSealsHandlesSpoolRootErrors/spool_根目录不存在返回空而非报错
=== RUN   TestListPendingSealsHandlesSpoolRootErrors/spool_根目录读失败必须报错
--- PASS: TestListPendingSealsHandlesSpoolRootErrors (0.00s)
    --- PASS: TestListPendingSealsHandlesSpoolRootErrors/spool_根目录不存在返回空而非报错 (0.00s)
    --- PASS: TestListPendingSealsHandlesSpoolRootErrors/spool_根目录读失败必须报错 (0.00s)
=== RUN   TestListPendingSealsIgnoresNonLayoutEntries
--- PASS: TestListPendingSealsIgnoresNonLayoutEntries (0.00s)
=== RUN   TestSealRejectsTruncatedSpool
=== RUN   TestSealRejectsTruncatedSpool/目录扫描路径（Rows_未知，无交叉校验可用）
=== RUN   TestSealRejectsTruncatedSpool/行数已知路径（纵深防御，交叉校验本身也拦不住残行）
--- PASS: TestSealRejectsTruncatedSpool (0.00s)
    --- PASS: TestSealRejectsTruncatedSpool/目录扫描路径（Rows_未知，无交叉校验可用） (0.00s)
    --- PASS: TestSealRejectsTruncatedSpool/行数已知路径（纵深防御，交叉校验本身也拦不住残行） (0.00s)
=== RUN   TestSealAcceptsEmptySpool
    sealer_test.go:1026: 空 spool 的产物应只有 32 字节 footer，实际 45 字节
--- FAIL: TestSealAcceptsEmptySpool (0.02s)
=== RUN   TestSealerListPendingSealsUsesConfiguredMinAge
--- PASS: TestSealerListPendingSealsUsesConfiguredMinAge (0.00s)
=== RUN   TestSealRejectsMismatchedZstdVersion
--- PASS: TestSealRejectsMismatchedZstdVersion (0.00s)
=== RUN   TestSealRejectsUnparsableZstdVersion
=== RUN   TestSealRejectsUnparsableZstdVersion/输出里没有版本号
=== RUN   TestSealRejectsUnparsableZstdVersion/版本号只有两段
=== RUN   TestSealRejectsUnparsableZstdVersion/版本号带后缀
=== RUN   TestSealRejectsUnparsableZstdVersion/版本号段内非数字
--- PASS: TestSealRejectsUnparsableZstdVersion (0.02s)
    --- PASS: TestSealRejectsUnparsableZstdVersion/输出里没有版本号 (0.00s)
    --- PASS: TestSealRejectsUnparsableZstdVersion/版本号只有两段 (0.00s)
    --- PASS: TestSealRejectsUnparsableZstdVersion/版本号带后缀 (0.00s)
    --- PASS: TestSealRejectsUnparsableZstdVersion/版本号段内非数字 (0.00s)
=== RUN   TestSealAcceptsConfiguredZstdVersion
--- PASS: TestSealAcceptsConfiguredZstdVersion (0.18s)
=== RUN   TestListPendingSealsRejectsMalformedDirNames
--- PASS: TestListPendingSealsRejectsMalformedDirNames (0.00s)
=== RUN   TestSealRejectsFailedVersionProbe
--- PASS: TestSealRejectsFailedVersionProbe (0.01s)
FAIL
FAIL	github.com/example/llm-gateway/tests/tracearchive	11.506s
FAIL


======================================================================
### 变异侧 m15_删除打开spool失败守卫 ⇒ **绿（零覆盖！）** [names=32 ran=32 diff=0 PASS=32 SKIP=0 FAIL=0] 杀死: —
======================================================================
=== RUN   TestSealIsAtomicOnCrash
--- PASS: TestSealIsAtomicOnCrash (0.08s)
=== RUN   TestParsePartSeqUsedBySealer
--- PASS: TestParsePartSeqUsedBySealer (0.00s)
PASS
ok  	github.com/example/llm-gateway/internal/tracearchive	0.093s
=== RUN   TestSealProducesArtifactAndRemovesSpool
--- PASS: TestSealProducesArtifactAndRemovesSpool (0.11s)
=== RUN   TestSealRefusesToOverwriteExistingArtifact
--- PASS: TestSealRefusesToOverwriteExistingArtifact (0.00s)
=== RUN   TestSealRejectsRowCountMismatch
--- PASS: TestSealRejectsRowCountMismatch (0.00s)
=== RUN   TestSealFooterPointsAtValidTrailer
--- PASS: TestSealFooterPointsAtValidTrailer (0.06s)
=== RUN   TestSealFailsOnNonZeroExit
--- PASS: TestSealFailsOnNonZeroExit (0.01s)
=== RUN   TestSealFailsWhenZstdMissing
--- PASS: TestSealFailsWhenZstdMissing (0.00s)
=== RUN   TestSealGoldenLayout
--- PASS: TestSealGoldenLayout (0.03s)
=== RUN   TestListPendingSealsSkipsFreshFiles
--- PASS: TestListPendingSealsSkipsFreshFiles (0.00s)
=== RUN   TestListPendingSealsReturnsStaleFiles
--- PASS: TestListPendingSealsReturnsStaleFiles (0.00s)
=== RUN   TestListPendingSealsIgnoresIllegalNames
--- PASS: TestListPendingSealsIgnoresIllegalNames (0.00s)
=== RUN   TestSealInvokesZstdCLIWithArgs
=== RUN   TestSealInvokesZstdCLIWithArgs/默认级别与默认窗口
=== RUN   TestSealInvokesZstdCLIWithArgs/显式级别与显式窗口
--- PASS: TestSealInvokesZstdCLIWithArgs (0.07s)
    --- PASS: TestSealInvokesZstdCLIWithArgs/默认级别与默认窗口 (0.04s)
    --- PASS: TestSealInvokesZstdCLIWithArgs/显式级别与显式窗口 (0.03s)
=== RUN   TestSealDefaultsZstdBinaryFromPath
--- PASS: TestSealDefaultsZstdBinaryFromPath (0.04s)
=== RUN   TestNewSealerValidatesOptions
=== RUN   TestNewSealerValidatesOptions/空_Root_被拒绝
=== RUN   TestNewSealerValidatesOptions/非_2_的幂窗口被拒绝
--- PASS: TestNewSealerValidatesOptions (0.00s)
    --- PASS: TestNewSealerValidatesOptions/空_Root_被拒绝 (0.00s)
    --- PASS: TestNewSealerValidatesOptions/非_2_的幂窗口被拒绝 (0.00s)
=== RUN   TestSealRejectsUnknownArtifactState
--- PASS: TestSealRejectsUnknownArtifactState (0.00s)
=== RUN   TestSealFailsWhenSpoolMissing
--- PASS: TestSealFailsWhenSpoolMissing (0.00s)
=== RUN   TestSealRejectsSpoolPathNotMatchingRef
--- PASS: TestSealRejectsSpoolPathNotMatchingRef (0.00s)
=== RUN   TestSealAcceptsCleanedEquivalentSpoolPath
--- PASS: TestSealAcceptsCleanedEquivalentSpoolPath (0.03s)
=== RUN   TestSealReportsMidRunCancellationAsCanceled
--- PASS: TestSealReportsMidRunCancellationAsCanceled (0.02s)
=== RUN   TestSealFailsWhenSpoolIsDirectory
--- PASS: TestSealFailsWhenSpoolIsDirectory (0.00s)
=== RUN   TestSealAcceptsRowsZeroFromDiscoveryPath
--- PASS: TestSealAcceptsRowsZeroFromDiscoveryPath (0.07s)
=== RUN   TestListPendingSealsHandlesSpoolRootErrors
=== RUN   TestListPendingSealsHandlesSpoolRootErrors/spool_根目录不存在返回空而非报错
=== RUN   TestListPendingSealsHandlesSpoolRootErrors/spool_根目录读失败必须报错
--- PASS: TestListPendingSealsHandlesSpoolRootErrors (0.00s)
    --- PASS: TestListPendingSealsHandlesSpoolRootErrors/spool_根目录不存在返回空而非报错 (0.00s)
    --- PASS: TestListPendingSealsHandlesSpoolRootErrors/spool_根目录读失败必须报错 (0.00s)
=== RUN   TestListPendingSealsIgnoresNonLayoutEntries
--- PASS: TestListPendingSealsIgnoresNonLayoutEntries (0.00s)
=== RUN   TestSealRejectsTruncatedSpool
=== RUN   TestSealRejectsTruncatedSpool/目录扫描路径（Rows_未知，无交叉校验可用）
=== RUN   TestSealRejectsTruncatedSpool/行数已知路径（纵深防御，交叉校验本身也拦不住残行）
--- PASS: TestSealRejectsTruncatedSpool (0.00s)
    --- PASS: TestSealRejectsTruncatedSpool/目录扫描路径（Rows_未知，无交叉校验可用） (0.00s)
    --- PASS: TestSealRejectsTruncatedSpool/行数已知路径（纵深防御，交叉校验本身也拦不住残行） (0.00s)
=== RUN   TestSealAcceptsEmptySpool
--- PASS: TestSealAcceptsEmptySpool (0.03s)
=== RUN   TestSealerListPendingSealsUsesConfiguredMinAge
--- PASS: TestSealerListPendingSealsUsesConfiguredMinAge (0.00s)
=== RUN   TestSealRejectsMismatchedZstdVersion
--- PASS: TestSealRejectsMismatchedZstdVersion (0.00s)
=== RUN   TestSealRejectsUnparsableZstdVersion
=== RUN   TestSealRejectsUnparsableZstdVersion/输出里没有版本号
=== RUN   TestSealRejectsUnparsableZstdVersion/版本号只有两段
=== RUN   TestSealRejectsUnparsableZstdVersion/版本号带后缀
=== RUN   TestSealRejectsUnparsableZstdVersion/版本号段内非数字
--- PASS: TestSealRejectsUnparsableZstdVersion (0.02s)
    --- PASS: TestSealRejectsUnparsableZstdVersion/输出里没有版本号 (0.00s)
    --- PASS: TestSealRejectsUnparsableZstdVersion/版本号只有两段 (0.00s)
    --- PASS: TestSealRejectsUnparsableZstdVersion/版本号带后缀 (0.00s)
    --- PASS: TestSealRejectsUnparsableZstdVersion/版本号段内非数字 (0.00s)
=== RUN   TestSealAcceptsConfiguredZstdVersion
--- PASS: TestSealAcceptsConfiguredZstdVersion (0.03s)
=== RUN   TestListPendingSealsRejectsMalformedDirNames
--- PASS: TestListPendingSealsRejectsMalformedDirNames (0.00s)
=== RUN   TestSealRejectsFailedVersionProbe
--- PASS: TestSealRejectsFailedVersionProbe (0.01s)
PASS
ok  	github.com/example/llm-gateway/tests/tracearchive	0.576s


======================================================================
### 变异侧 m17_trailer先于帧 ⇒ 红(崩溃/中止) [集合不完整：未执行 10 条（TestListPendingSealsHandlesSpoolRootErrors, TestListPendingSealsIgnoresNonLayoutEntries, TestListPendingSealsRejectsMalformedDirNames …）] 杀死: TestSealAcceptsRowsZeroFromDiscoveryPath, TestSealFooterPointsAtValidTrailer, TestSealGoldenLayout
======================================================================
=== RUN   TestSealIsAtomicOnCrash
--- PASS: TestSealIsAtomicOnCrash (0.06s)
=== RUN   TestParsePartSeqUsedBySealer
--- PASS: TestParsePartSeqUsedBySealer (0.00s)
PASS
ok  	github.com/example/llm-gateway/internal/tracearchive	0.064s
=== RUN   TestSealProducesArtifactAndRemovesSpool
--- PASS: TestSealProducesArtifactAndRemovesSpool (0.07s)
=== RUN   TestSealRefusesToOverwriteExistingArtifact
--- PASS: TestSealRefusesToOverwriteExistingArtifact (0.00s)
=== RUN   TestSealRejectsRowCountMismatch
--- PASS: TestSealRejectsRowCountMismatch (0.00s)
=== RUN   TestSealFooterPointsAtValidTrailer
    sealer_test.go:180: 产物只有 25 字节，装不下 32 字节 footer
--- FAIL: TestSealFooterPointsAtValidTrailer (0.05s)
=== RUN   TestSealFailsOnNonZeroExit
--- PASS: TestSealFailsOnNonZeroExit (0.01s)
=== RUN   TestSealFailsWhenZstdMissing
--- PASS: TestSealFailsWhenZstdMissing (0.00s)
=== RUN   TestSealGoldenLayout
    sealer_test.go:325: 产物逐字节不符（长度 45 vs 10）:
             want 61 61 0a 62 62 62 62 0a 63 0a 00 03 05 54 42 41 34 00 01 00 00 00 00 00 00 00 0a 00 00 00 03 00 00 00 03 e3 76 d8 f8 ff 1f 11 e2 00 00
             got  61 61 0a 62 62 62 62 0a 63 0a
--- FAIL: TestSealGoldenLayout (0.02s)
=== RUN   TestListPendingSealsSkipsFreshFiles
--- PASS: TestListPendingSealsSkipsFreshFiles (0.00s)
=== RUN   TestListPendingSealsReturnsStaleFiles
--- PASS: TestListPendingSealsReturnsStaleFiles (0.00s)
=== RUN   TestListPendingSealsIgnoresIllegalNames
--- PASS: TestListPendingSealsIgnoresIllegalNames (0.00s)
=== RUN   TestSealInvokesZstdCLIWithArgs
=== RUN   TestSealInvokesZstdCLIWithArgs/默认级别与默认窗口
=== RUN   TestSealInvokesZstdCLIWithArgs/显式级别与显式窗口
--- PASS: TestSealInvokesZstdCLIWithArgs (0.04s)
    --- PASS: TestSealInvokesZstdCLIWithArgs/默认级别与默认窗口 (0.03s)
    --- PASS: TestSealInvokesZstdCLIWithArgs/显式级别与显式窗口 (0.01s)
=== RUN   TestSealDefaultsZstdBinaryFromPath
--- PASS: TestSealDefaultsZstdBinaryFromPath (0.01s)
=== RUN   TestNewSealerValidatesOptions
=== RUN   TestNewSealerValidatesOptions/空_Root_被拒绝
=== RUN   TestNewSealerValidatesOptions/非_2_的幂窗口被拒绝
--- PASS: TestNewSealerValidatesOptions (0.00s)
    --- PASS: TestNewSealerValidatesOptions/空_Root_被拒绝 (0.00s)
    --- PASS: TestNewSealerValidatesOptions/非_2_的幂窗口被拒绝 (0.00s)
=== RUN   TestSealRejectsUnknownArtifactState
--- PASS: TestSealRejectsUnknownArtifactState (0.00s)
=== RUN   TestSealFailsWhenSpoolMissing
--- PASS: TestSealFailsWhenSpoolMissing (0.00s)
=== RUN   TestSealRejectsSpoolPathNotMatchingRef
--- PASS: TestSealRejectsSpoolPathNotMatchingRef (0.00s)
=== RUN   TestSealAcceptsCleanedEquivalentSpoolPath
--- PASS: TestSealAcceptsCleanedEquivalentSpoolPath (0.01s)
=== RUN   TestSealReportsMidRunCancellationAsCanceled
--- PASS: TestSealReportsMidRunCancellationAsCanceled (0.01s)
=== RUN   TestSealFailsWhenSpoolIsDirectory
--- PASS: TestSealFailsWhenSpoolIsDirectory (0.00s)
=== RUN   TestSealAcceptsRowsZeroFromDiscoveryPath
--- FAIL: TestSealAcceptsRowsZeroFromDiscoveryPath (0.04s)
panic: runtime error: slice bounds out of range [-11:] [recovered, repanicked]

goroutine 101 [running]:
testing.tRunner.func1.2({0x102ff0240, 0x3b46b34f41b0})
	/opt/homebrew/Cellar/go/1.26.1/libexec/src/testing/testing.go:1974 +0x1a0
testing.tRunner.func1()
	/opt/homebrew/Cellar/go/1.26.1/libexec/src/testing/testing.go:1977 +0x318
panic({0x102ff0240?, 0x3b46b34f41b0?})
	/opt/homebrew/Cellar/go/1.26.1/libexec/src/runtime/panic.go:860 +0x12c
github.com/example/llm-gateway/tests/tracearchive_test.TestSealAcceptsRowsZeroFromDiscoveryPath(0x3b46b39a2248)
	/private/tmp/sealbase/tests/tracearchive/sealer_test.go:850 +0x488
testing.tRunner(0x3b46b39a2248, 0x103001918)
	/opt/homebrew/Cellar/go/1.26.1/libexec/src/testing/testing.go:2036 +0xc4
created by testing.(*T).Run in goroutine 1
	/opt/homebrew/Cellar/go/1.26.1/libexec/src/testing/testing.go:2101 +0x3a8
FAIL	github.com/example/llm-gateway/tests/tracearchive	0.286s
FAIL


======================================================================
### 变异侧 m20_删除ZstdLevel默认值 ⇒ 红 [names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1] 杀死: TestSealInvokesZstdCLIWithArgs
======================================================================
=== RUN   TestSealIsAtomicOnCrash
--- PASS: TestSealIsAtomicOnCrash (0.02s)
=== RUN   TestParsePartSeqUsedBySealer
--- PASS: TestParsePartSeqUsedBySealer (0.00s)
PASS
ok  	github.com/example/llm-gateway/internal/tracearchive	0.025s
=== RUN   TestSealProducesArtifactAndRemovesSpool
--- PASS: TestSealProducesArtifactAndRemovesSpool (0.02s)
=== RUN   TestSealRefusesToOverwriteExistingArtifact
--- PASS: TestSealRefusesToOverwriteExistingArtifact (0.00s)
=== RUN   TestSealRejectsRowCountMismatch
--- PASS: TestSealRejectsRowCountMismatch (0.00s)
=== RUN   TestSealFooterPointsAtValidTrailer
--- PASS: TestSealFooterPointsAtValidTrailer (0.02s)
=== RUN   TestSealFailsOnNonZeroExit
--- PASS: TestSealFailsOnNonZeroExit (0.01s)
=== RUN   TestSealFailsWhenZstdMissing
--- PASS: TestSealFailsWhenZstdMissing (0.00s)
=== RUN   TestSealGoldenLayout
--- PASS: TestSealGoldenLayout (0.01s)
=== RUN   TestListPendingSealsSkipsFreshFiles
--- PASS: TestListPendingSealsSkipsFreshFiles (0.00s)
=== RUN   TestListPendingSealsReturnsStaleFiles
--- PASS: TestListPendingSealsReturnsStaleFiles (0.00s)
=== RUN   TestListPendingSealsIgnoresIllegalNames
--- PASS: TestListPendingSealsIgnoresIllegalNames (0.00s)
=== RUN   TestSealInvokesZstdCLIWithArgs
=== RUN   TestSealInvokesZstdCLIWithArgs/默认级别与默认窗口
    sealer_test.go:479: 传给 zstd 的参数不符:
         want ["-19" "--long=29" "-T1" "-c"]
         got  ["-0" "--long=29" "-T1" "-c"]
=== RUN   TestSealInvokesZstdCLIWithArgs/显式级别与显式窗口
--- FAIL: TestSealInvokesZstdCLIWithArgs (0.03s)
    --- FAIL: TestSealInvokesZstdCLIWithArgs/默认级别与默认窗口 (0.01s)
    --- PASS: TestSealInvokesZstdCLIWithArgs/显式级别与显式窗口 (0.01s)
=== RUN   TestSealDefaultsZstdBinaryFromPath
--- PASS: TestSealDefaultsZstdBinaryFromPath (0.01s)
=== RUN   TestNewSealerValidatesOptions
=== RUN   TestNewSealerValidatesOptions/空_Root_被拒绝
=== RUN   TestNewSealerValidatesOptions/非_2_的幂窗口被拒绝
--- PASS: TestNewSealerValidatesOptions (0.00s)
    --- PASS: TestNewSealerValidatesOptions/空_Root_被拒绝 (0.00s)
    --- PASS: TestNewSealerValidatesOptions/非_2_的幂窗口被拒绝 (0.00s)
=== RUN   TestSealRejectsUnknownArtifactState
--- PASS: TestSealRejectsUnknownArtifactState (0.00s)
=== RUN   TestSealFailsWhenSpoolMissing
--- PASS: TestSealFailsWhenSpoolMissing (0.00s)
=== RUN   TestSealRejectsSpoolPathNotMatchingRef
--- PASS: TestSealRejectsSpoolPathNotMatchingRef (0.00s)
=== RUN   TestSealAcceptsCleanedEquivalentSpoolPath
--- PASS: TestSealAcceptsCleanedEquivalentSpoolPath (0.01s)
=== RUN   TestSealReportsMidRunCancellationAsCanceled
--- PASS: TestSealReportsMidRunCancellationAsCanceled (0.01s)
=== RUN   TestSealFailsWhenSpoolIsDirectory
--- PASS: TestSealFailsWhenSpoolIsDirectory (0.00s)
=== RUN   TestSealAcceptsRowsZeroFromDiscoveryPath
--- PASS: TestSealAcceptsRowsZeroFromDiscoveryPath (0.02s)
=== RUN   TestListPendingSealsHandlesSpoolRootErrors
=== RUN   TestListPendingSealsHandlesSpoolRootErrors/spool_根目录不存在返回空而非报错
=== RUN   TestListPendingSealsHandlesSpoolRootErrors/spool_根目录读失败必须报错
--- PASS: TestListPendingSealsHandlesSpoolRootErrors (0.00s)
    --- PASS: TestListPendingSealsHandlesSpoolRootErrors/spool_根目录不存在返回空而非报错 (0.00s)
    --- PASS: TestListPendingSealsHandlesSpoolRootErrors/spool_根目录读失败必须报错 (0.00s)
=== RUN   TestListPendingSealsIgnoresNonLayoutEntries
--- PASS: TestListPendingSealsIgnoresNonLayoutEntries (0.00s)
=== RUN   TestSealRejectsTruncatedSpool
=== RUN   TestSealRejectsTruncatedSpool/目录扫描路径（Rows_未知，无交叉校验可用）
=== RUN   TestSealRejectsTruncatedSpool/行数已知路径（纵深防御，交叉校验本身也拦不住残行）
--- PASS: TestSealRejectsTruncatedSpool (0.00s)
    --- PASS: TestSealRejectsTruncatedSpool/目录扫描路径（Rows_未知，无交叉校验可用） (0.00s)
    --- PASS: TestSealRejectsTruncatedSpool/行数已知路径（纵深防御，交叉校验本身也拦不住残行） (0.00s)
=== RUN   TestSealAcceptsEmptySpool
--- PASS: TestSealAcceptsEmptySpool (0.01s)
=== RUN   TestSealerListPendingSealsUsesConfiguredMinAge
--- PASS: TestSealerListPendingSealsUsesConfiguredMinAge (0.00s)
=== RUN   TestSealRejectsMismatchedZstdVersion
--- PASS: TestSealRejectsMismatchedZstdVersion (0.00s)
=== RUN   TestSealRejectsUnparsableZstdVersion
=== RUN   TestSealRejectsUnparsableZstdVersion/输出里没有版本号
=== RUN   TestSealRejectsUnparsableZstdVersion/版本号只有两段
=== RUN   TestSealRejectsUnparsableZstdVersion/版本号带后缀
=== RUN   TestSealRejectsUnparsableZstdVersion/版本号段内非数字
--- PASS: TestSealRejectsUnparsableZstdVersion (0.01s)
    --- PASS: TestSealRejectsUnparsableZstdVersion/输出里没有版本号 (0.00s)
    --- PASS: TestSealRejectsUnparsableZstdVersion/版本号只有两段 (0.00s)
    --- PASS: TestSealRejectsUnparsableZstdVersion/版本号带后缀 (0.00s)
    --- PASS: TestSealRejectsUnparsableZstdVersion/版本号段内非数字 (0.00s)
=== RUN   TestSealAcceptsConfiguredZstdVersion
--- PASS: TestSealAcceptsConfiguredZstdVersion (0.01s)
=== RUN   TestListPendingSealsRejectsMalformedDirNames
--- PASS: TestListPendingSealsRejectsMalformedDirNames (0.00s)
=== RUN   TestSealRejectsFailedVersionProbe
--- PASS: TestSealRejectsFailedVersionProbe (0.00s)
FAIL
FAIL	github.com/example/llm-gateway/tests/tracearchive	0.208s
FAIL


======================================================================
### 变异侧 m21_删除ZstdWindowBytes默认值 ⇒ 红(崩溃/中止) [集合不完整：未执行 29 条（TestListPendingSealsHandlesSpoolRootErrors, TestListPendingSealsIgnoresIllegalNames, TestListPendingSealsIgnoresNonLayoutEntries …）] 杀死: TestSealIsAtomicOnCrash, TestSealProducesArtifactAndRemovesSpool, TestSealRefusesToOverwriteExistingArtifact
======================================================================
=== RUN   TestSealIsAtomicOnCrash
--- FAIL: TestSealIsAtomicOnCrash (0.00s)
panic: runtime error: invalid memory address or nil pointer dereference [recovered, repanicked]
[signal SIGSEGV: segmentation violation code=0x2 addr=0x0 pc=0x10047dbfc]

goroutine 33 [running]:
testing.tRunner.func1.2({0x1005f7380, 0x100649290})
	/opt/homebrew/Cellar/go/1.26.1/libexec/src/testing/testing.go:1974 +0x1a0
testing.tRunner.func1()
	/opt/homebrew/Cellar/go/1.26.1/libexec/src/testing/testing.go:1977 +0x318
panic({0x1005f7380?, 0x100649290?})
	/opt/homebrew/Cellar/go/1.26.1/libexec/src/runtime/panic.go:860 +0x12c
github.com/example/llm-gateway/internal/tracearchive.TestSealIsAtomicOnCrash(0x1d1a4b696248)
	/private/tmp/sealbase/internal/tracearchive/sealer_internal_test.go:27 +0xfc
testing.tRunner(0x1d1a4b696248, 0x100628bf0)
	/opt/homebrew/Cellar/go/1.26.1/libexec/src/testing/testing.go:2036 +0xc4
created by testing.(*T).Run in goroutine 1
	/opt/homebrew/Cellar/go/1.26.1/libexec/src/testing/testing.go:2101 +0x3a8
FAIL	github.com/example/llm-gateway/internal/tracearchive	0.013s
=== RUN   TestSealProducesArtifactAndRemovesSpool
    sealer_test.go:36: 建封块器失败: tracearchive: zstd 窗口必须是 2 的幂（实际 0 字节）
--- FAIL: TestSealProducesArtifactAndRemovesSpool (0.00s)
=== RUN   TestSealRefusesToOverwriteExistingArtifact
--- FAIL: TestSealRefusesToOverwriteExistingArtifact (0.00s)
panic: runtime error: invalid memory address or nil pointer dereference [recovered, repanicked]
[signal SIGSEGV: segmentation violation code=0x2 addr=0x48 pc=0x10098bad4]

goroutine 10 [running]:
testing.tRunner.func1.2({0x100b00d20, 0x100b50e50})
	/opt/homebrew/Cellar/go/1.26.1/libexec/src/testing/testing.go:1974 +0x1a0
testing.tRunner.func1()
	/opt/homebrew/Cellar/go/1.26.1/libexec/src/testing/testing.go:1977 +0x318
panic({0x100b00d20?, 0x100b50e50?})
	/opt/homebrew/Cellar/go/1.26.1/libexec/src/runtime/panic.go:860 +0x12c
github.com/example/llm-gateway/internal/tracearchive.(*Sealer).Seal(0x0, {0x100b30458, 0x100b824a0}, {{0x1009aaeb9, 0xa}, {0x1009a9b7f, 0x4}, 0x3, {0x318c655f4280, 0x99}, ...})
	/private/tmp/sealbase/internal/tracearchive/sealer.go:144 +0x84
github.com/example/llm-gateway/tests/tracearchive_test.TestSealRefusesToOverwriteExistingArtifact(0x318c654d8488)
	/private/tmp/sealbase/tests/tracearchive/sealer_test.go:75 +0x1f8
testing.tRunner(0x318c654d8488, 0x100b2d968)
	/opt/homebrew/Cellar/go/1.26.1/libexec/src/testing/testing.go:2036 +0xc4
created by testing.(*T).Run in goroutine 1
	/opt/homebrew/Cellar/go/1.26.1/libexec/src/testing/testing.go:2101 +0x3a8
FAIL	github.com/example/llm-gateway/tests/tracearchive	0.013s
FAIL


======================================================================
### 变异侧 m24_删除ctx取消分支 ⇒ 红 [names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1] 杀死: TestSealReportsMidRunCancellationAsCanceled
======================================================================
=== RUN   TestSealIsAtomicOnCrash
--- PASS: TestSealIsAtomicOnCrash (0.05s)
=== RUN   TestParsePartSeqUsedBySealer
--- PASS: TestParsePartSeqUsedBySealer (0.00s)
PASS
ok  	github.com/example/llm-gateway/internal/tracearchive	0.057s
=== RUN   TestSealProducesArtifactAndRemovesSpool
--- PASS: TestSealProducesArtifactAndRemovesSpool (0.05s)
=== RUN   TestSealRefusesToOverwriteExistingArtifact
--- PASS: TestSealRefusesToOverwriteExistingArtifact (0.00s)
=== RUN   TestSealRejectsRowCountMismatch
--- PASS: TestSealRejectsRowCountMismatch (0.00s)
=== RUN   TestSealFooterPointsAtValidTrailer
--- PASS: TestSealFooterPointsAtValidTrailer (0.04s)
=== RUN   TestSealFailsOnNonZeroExit
--- PASS: TestSealFailsOnNonZeroExit (0.01s)
=== RUN   TestSealFailsWhenZstdMissing
--- PASS: TestSealFailsWhenZstdMissing (0.00s)
=== RUN   TestSealGoldenLayout
--- PASS: TestSealGoldenLayout (0.01s)
=== RUN   TestListPendingSealsSkipsFreshFiles
--- PASS: TestListPendingSealsSkipsFreshFiles (0.00s)
=== RUN   TestListPendingSealsReturnsStaleFiles
--- PASS: TestListPendingSealsReturnsStaleFiles (0.00s)
=== RUN   TestListPendingSealsIgnoresIllegalNames
--- PASS: TestListPendingSealsIgnoresIllegalNames (0.00s)
=== RUN   TestSealInvokesZstdCLIWithArgs
=== RUN   TestSealInvokesZstdCLIWithArgs/默认级别与默认窗口
=== RUN   TestSealInvokesZstdCLIWithArgs/显式级别与显式窗口
--- PASS: TestSealInvokesZstdCLIWithArgs (0.03s)
    --- PASS: TestSealInvokesZstdCLIWithArgs/默认级别与默认窗口 (0.01s)
    --- PASS: TestSealInvokesZstdCLIWithArgs/显式级别与显式窗口 (0.01s)
=== RUN   TestSealDefaultsZstdBinaryFromPath
--- PASS: TestSealDefaultsZstdBinaryFromPath (0.01s)
=== RUN   TestNewSealerValidatesOptions
=== RUN   TestNewSealerValidatesOptions/空_Root_被拒绝
=== RUN   TestNewSealerValidatesOptions/非_2_的幂窗口被拒绝
--- PASS: TestNewSealerValidatesOptions (0.00s)
    --- PASS: TestNewSealerValidatesOptions/空_Root_被拒绝 (0.00s)
    --- PASS: TestNewSealerValidatesOptions/非_2_的幂窗口被拒绝 (0.00s)
=== RUN   TestSealRejectsUnknownArtifactState
--- PASS: TestSealRejectsUnknownArtifactState (0.00s)
=== RUN   TestSealFailsWhenSpoolMissing
--- PASS: TestSealFailsWhenSpoolMissing (0.00s)
=== RUN   TestSealRejectsSpoolPathNotMatchingRef
--- PASS: TestSealRejectsSpoolPathNotMatchingRef (0.00s)
=== RUN   TestSealAcceptsCleanedEquivalentSpoolPath
--- PASS: TestSealAcceptsCleanedEquivalentSpoolPath (0.01s)
=== RUN   TestSealReportsMidRunCancellationAsCanceled
    sealer_test.go:765: 运行途中取消必须归一化为 context.Canceled（子进程被杀只给出 signal: killed，调用方无法区分「取消」与「压缩失败」），实际 tracearchive: 封块失败: zstd 子进程失败（args=[-19 --long=29 -T1 -c]）: signal: killed（stderr: ）
--- FAIL: TestSealReportsMidRunCancellationAsCanceled (0.01s)
=== RUN   TestSealFailsWhenSpoolIsDirectory
--- PASS: TestSealFailsWhenSpoolIsDirectory (0.00s)
=== RUN   TestSealAcceptsRowsZeroFromDiscoveryPath
--- PASS: TestSealAcceptsRowsZeroFromDiscoveryPath (0.05s)
=== RUN   TestListPendingSealsHandlesSpoolRootErrors
=== RUN   TestListPendingSealsHandlesSpoolRootErrors/spool_根目录不存在返回空而非报错
=== RUN   TestListPendingSealsHandlesSpoolRootErrors/spool_根目录读失败必须报错
--- PASS: TestListPendingSealsHandlesSpoolRootErrors (0.00s)
    --- PASS: TestListPendingSealsHandlesSpoolRootErrors/spool_根目录不存在返回空而非报错 (0.00s)
    --- PASS: TestListPendingSealsHandlesSpoolRootErrors/spool_根目录读失败必须报错 (0.00s)
=== RUN   TestListPendingSealsIgnoresNonLayoutEntries
--- PASS: TestListPendingSealsIgnoresNonLayoutEntries (0.00s)
=== RUN   TestSealRejectsTruncatedSpool
=== RUN   TestSealRejectsTruncatedSpool/目录扫描路径（Rows_未知，无交叉校验可用）
=== RUN   TestSealRejectsTruncatedSpool/行数已知路径（纵深防御，交叉校验本身也拦不住残行）
--- PASS: TestSealRejectsTruncatedSpool (0.00s)
    --- PASS: TestSealRejectsTruncatedSpool/目录扫描路径（Rows_未知，无交叉校验可用） (0.00s)
    --- PASS: TestSealRejectsTruncatedSpool/行数已知路径（纵深防御，交叉校验本身也拦不住残行） (0.00s)
=== RUN   TestSealAcceptsEmptySpool
--- PASS: TestSealAcceptsEmptySpool (0.02s)
=== RUN   TestSealerListPendingSealsUsesConfiguredMinAge
--- PASS: TestSealerListPendingSealsUsesConfiguredMinAge (0.00s)
=== RUN   TestSealRejectsMismatchedZstdVersion
--- PASS: TestSealRejectsMismatchedZstdVersion (0.00s)
=== RUN   TestSealRejectsUnparsableZstdVersion
=== RUN   TestSealRejectsUnparsableZstdVersion/输出里没有版本号
=== RUN   TestSealRejectsUnparsableZstdVersion/版本号只有两段
=== RUN   TestSealRejectsUnparsableZstdVersion/版本号带后缀
=== RUN   TestSealRejectsUnparsableZstdVersion/版本号段内非数字
--- PASS: TestSealRejectsUnparsableZstdVersion (0.01s)
    --- PASS: TestSealRejectsUnparsableZstdVersion/输出里没有版本号 (0.00s)
    --- PASS: TestSealRejectsUnparsableZstdVersion/版本号只有两段 (0.00s)
    --- PASS: TestSealRejectsUnparsableZstdVersion/版本号带后缀 (0.00s)
    --- PASS: TestSealRejectsUnparsableZstdVersion/版本号段内非数字 (0.00s)
=== RUN   TestSealAcceptsConfiguredZstdVersion
--- PASS: TestSealAcceptsConfiguredZstdVersion (0.01s)
=== RUN   TestListPendingSealsRejectsMalformedDirNames
--- PASS: TestListPendingSealsRejectsMalformedDirNames (0.00s)
=== RUN   TestSealRejectsFailedVersionProbe
--- PASS: TestSealRejectsFailedVersionProbe (0.00s)
FAIL
FAIL	github.com/example/llm-gateway/tests/tracearchive	0.296s
FAIL


======================================================================
### 变异侧 m34_删除sort顺序 ⇒ **绿（零覆盖！）** [names=32 ran=32 diff=0 PASS=32 SKIP=0 FAIL=0] 杀死: —
======================================================================
=== RUN   TestSealIsAtomicOnCrash
--- PASS: TestSealIsAtomicOnCrash (0.05s)
=== RUN   TestParsePartSeqUsedBySealer
--- PASS: TestParsePartSeqUsedBySealer (0.00s)
PASS
ok  	github.com/example/llm-gateway/internal/tracearchive	0.056s
=== RUN   TestSealProducesArtifactAndRemovesSpool
--- PASS: TestSealProducesArtifactAndRemovesSpool (0.05s)
=== RUN   TestSealRefusesToOverwriteExistingArtifact
--- PASS: TestSealRefusesToOverwriteExistingArtifact (0.00s)
=== RUN   TestSealRejectsRowCountMismatch
--- PASS: TestSealRejectsRowCountMismatch (0.00s)
=== RUN   TestSealFooterPointsAtValidTrailer
--- PASS: TestSealFooterPointsAtValidTrailer (0.04s)
=== RUN   TestSealFailsOnNonZeroExit
--- PASS: TestSealFailsOnNonZeroExit (0.01s)
=== RUN   TestSealFailsWhenZstdMissing
--- PASS: TestSealFailsWhenZstdMissing (0.00s)
=== RUN   TestSealGoldenLayout
--- PASS: TestSealGoldenLayout (0.01s)
=== RUN   TestListPendingSealsSkipsFreshFiles
--- PASS: TestListPendingSealsSkipsFreshFiles (0.00s)
=== RUN   TestListPendingSealsReturnsStaleFiles
--- PASS: TestListPendingSealsReturnsStaleFiles (0.00s)
=== RUN   TestListPendingSealsIgnoresIllegalNames
--- PASS: TestListPendingSealsIgnoresIllegalNames (0.00s)
=== RUN   TestSealInvokesZstdCLIWithArgs
=== RUN   TestSealInvokesZstdCLIWithArgs/默认级别与默认窗口
=== RUN   TestSealInvokesZstdCLIWithArgs/显式级别与显式窗口
--- PASS: TestSealInvokesZstdCLIWithArgs (0.03s)
    --- PASS: TestSealInvokesZstdCLIWithArgs/默认级别与默认窗口 (0.01s)
    --- PASS: TestSealInvokesZstdCLIWithArgs/显式级别与显式窗口 (0.01s)
=== RUN   TestSealDefaultsZstdBinaryFromPath
--- PASS: TestSealDefaultsZstdBinaryFromPath (0.01s)
=== RUN   TestNewSealerValidatesOptions
=== RUN   TestNewSealerValidatesOptions/空_Root_被拒绝
=== RUN   TestNewSealerValidatesOptions/非_2_的幂窗口被拒绝
--- PASS: TestNewSealerValidatesOptions (0.00s)
    --- PASS: TestNewSealerValidatesOptions/空_Root_被拒绝 (0.00s)
    --- PASS: TestNewSealerValidatesOptions/非_2_的幂窗口被拒绝 (0.00s)
=== RUN   TestSealRejectsUnknownArtifactState
--- PASS: TestSealRejectsUnknownArtifactState (0.00s)
=== RUN   TestSealFailsWhenSpoolMissing
--- PASS: TestSealFailsWhenSpoolMissing (0.00s)
=== RUN   TestSealRejectsSpoolPathNotMatchingRef
--- PASS: TestSealRejectsSpoolPathNotMatchingRef (0.00s)
=== RUN   TestSealAcceptsCleanedEquivalentSpoolPath
--- PASS: TestSealAcceptsCleanedEquivalentSpoolPath (0.01s)
=== RUN   TestSealReportsMidRunCancellationAsCanceled
--- PASS: TestSealReportsMidRunCancellationAsCanceled (0.01s)
=== RUN   TestSealFailsWhenSpoolIsDirectory
--- PASS: TestSealFailsWhenSpoolIsDirectory (0.00s)
=== RUN   TestSealAcceptsRowsZeroFromDiscoveryPath
--- PASS: TestSealAcceptsRowsZeroFromDiscoveryPath (0.04s)
=== RUN   TestListPendingSealsHandlesSpoolRootErrors
=== RUN   TestListPendingSealsHandlesSpoolRootErrors/spool_根目录不存在返回空而非报错
=== RUN   TestListPendingSealsHandlesSpoolRootErrors/spool_根目录读失败必须报错
--- PASS: TestListPendingSealsHandlesSpoolRootErrors (0.00s)
    --- PASS: TestListPendingSealsHandlesSpoolRootErrors/spool_根目录不存在返回空而非报错 (0.00s)
    --- PASS: TestListPendingSealsHandlesSpoolRootErrors/spool_根目录读失败必须报错 (0.00s)
=== RUN   TestListPendingSealsIgnoresNonLayoutEntries
--- PASS: TestListPendingSealsIgnoresNonLayoutEntries (0.00s)
=== RUN   TestSealRejectsTruncatedSpool
=== RUN   TestSealRejectsTruncatedSpool/目录扫描路径（Rows_未知，无交叉校验可用）
=== RUN   TestSealRejectsTruncatedSpool/行数已知路径（纵深防御，交叉校验本身也拦不住残行）
--- PASS: TestSealRejectsTruncatedSpool (0.00s)
    --- PASS: TestSealRejectsTruncatedSpool/目录扫描路径（Rows_未知，无交叉校验可用） (0.00s)
    --- PASS: TestSealRejectsTruncatedSpool/行数已知路径（纵深防御，交叉校验本身也拦不住残行） (0.00s)
=== RUN   TestSealAcceptsEmptySpool
--- PASS: TestSealAcceptsEmptySpool (0.01s)
=== RUN   TestSealerListPendingSealsUsesConfiguredMinAge
--- PASS: TestSealerListPendingSealsUsesConfiguredMinAge (0.00s)
=== RUN   TestSealRejectsMismatchedZstdVersion
--- PASS: TestSealRejectsMismatchedZstdVersion (0.00s)
=== RUN   TestSealRejectsUnparsableZstdVersion
=== RUN   TestSealRejectsUnparsableZstdVersion/输出里没有版本号
=== RUN   TestSealRejectsUnparsableZstdVersion/版本号只有两段
=== RUN   TestSealRejectsUnparsableZstdVersion/版本号带后缀
=== RUN   TestSealRejectsUnparsableZstdVersion/版本号段内非数字
--- PASS: TestSealRejectsUnparsableZstdVersion (0.01s)
    --- PASS: TestSealRejectsUnparsableZstdVersion/输出里没有版本号 (0.00s)
    --- PASS: TestSealRejectsUnparsableZstdVersion/版本号只有两段 (0.00s)
    --- PASS: TestSealRejectsUnparsableZstdVersion/版本号带后缀 (0.00s)
    --- PASS: TestSealRejectsUnparsableZstdVersion/版本号段内非数字 (0.00s)
=== RUN   TestSealAcceptsConfiguredZstdVersion
--- PASS: TestSealAcceptsConfiguredZstdVersion (0.01s)
=== RUN   TestListPendingSealsRejectsMalformedDirNames
--- PASS: TestListPendingSealsRejectsMalformedDirNames (0.00s)
=== RUN   TestSealRejectsFailedVersionProbe
--- PASS: TestSealRejectsFailedVersionProbe (0.00s)
PASS
ok  	github.com/example/llm-gateway/tests/tracearchive	0.292s


======================================================================
### 变异侧 m36_minAge默认值退回15分钟 ⇒ 红 [names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1] 杀死: TestSealerListPendingSealsUsesConfiguredMinAge
======================================================================
=== RUN   TestSealIsAtomicOnCrash
--- PASS: TestSealIsAtomicOnCrash (0.05s)
=== RUN   TestParsePartSeqUsedBySealer
--- PASS: TestParsePartSeqUsedBySealer (0.00s)
PASS
ok  	github.com/example/llm-gateway/internal/tracearchive	0.060s
=== RUN   TestSealProducesArtifactAndRemovesSpool
--- PASS: TestSealProducesArtifactAndRemovesSpool (0.05s)
=== RUN   TestSealRefusesToOverwriteExistingArtifact
--- PASS: TestSealRefusesToOverwriteExistingArtifact (0.00s)
=== RUN   TestSealRejectsRowCountMismatch
--- PASS: TestSealRejectsRowCountMismatch (0.00s)
=== RUN   TestSealFooterPointsAtValidTrailer
--- PASS: TestSealFooterPointsAtValidTrailer (0.04s)
=== RUN   TestSealFailsOnNonZeroExit
--- PASS: TestSealFailsOnNonZeroExit (0.01s)
=== RUN   TestSealFailsWhenZstdMissing
--- PASS: TestSealFailsWhenZstdMissing (0.00s)
=== RUN   TestSealGoldenLayout
--- PASS: TestSealGoldenLayout (0.01s)
=== RUN   TestListPendingSealsSkipsFreshFiles
--- PASS: TestListPendingSealsSkipsFreshFiles (0.00s)
=== RUN   TestListPendingSealsReturnsStaleFiles
--- PASS: TestListPendingSealsReturnsStaleFiles (0.00s)
=== RUN   TestListPendingSealsIgnoresIllegalNames
--- PASS: TestListPendingSealsIgnoresIllegalNames (0.00s)
=== RUN   TestSealInvokesZstdCLIWithArgs
=== RUN   TestSealInvokesZstdCLIWithArgs/默认级别与默认窗口
=== RUN   TestSealInvokesZstdCLIWithArgs/显式级别与显式窗口
--- PASS: TestSealInvokesZstdCLIWithArgs (0.03s)
    --- PASS: TestSealInvokesZstdCLIWithArgs/默认级别与默认窗口 (0.01s)
    --- PASS: TestSealInvokesZstdCLIWithArgs/显式级别与显式窗口 (0.01s)
=== RUN   TestSealDefaultsZstdBinaryFromPath
--- PASS: TestSealDefaultsZstdBinaryFromPath (0.01s)
=== RUN   TestNewSealerValidatesOptions
=== RUN   TestNewSealerValidatesOptions/空_Root_被拒绝
=== RUN   TestNewSealerValidatesOptions/非_2_的幂窗口被拒绝
--- PASS: TestNewSealerValidatesOptions (0.00s)
    --- PASS: TestNewSealerValidatesOptions/空_Root_被拒绝 (0.00s)
    --- PASS: TestNewSealerValidatesOptions/非_2_的幂窗口被拒绝 (0.00s)
=== RUN   TestSealRejectsUnknownArtifactState
--- PASS: TestSealRejectsUnknownArtifactState (0.00s)
=== RUN   TestSealFailsWhenSpoolMissing
--- PASS: TestSealFailsWhenSpoolMissing (0.00s)
=== RUN   TestSealRejectsSpoolPathNotMatchingRef
--- PASS: TestSealRejectsSpoolPathNotMatchingRef (0.00s)
=== RUN   TestSealAcceptsCleanedEquivalentSpoolPath
--- PASS: TestSealAcceptsCleanedEquivalentSpoolPath (0.01s)
=== RUN   TestSealReportsMidRunCancellationAsCanceled
--- PASS: TestSealReportsMidRunCancellationAsCanceled (0.01s)
=== RUN   TestSealFailsWhenSpoolIsDirectory
--- PASS: TestSealFailsWhenSpoolIsDirectory (0.00s)
=== RUN   TestSealAcceptsRowsZeroFromDiscoveryPath
--- PASS: TestSealAcceptsRowsZeroFromDiscoveryPath (0.04s)
=== RUN   TestListPendingSealsHandlesSpoolRootErrors
=== RUN   TestListPendingSealsHandlesSpoolRootErrors/spool_根目录不存在返回空而非报错
=== RUN   TestListPendingSealsHandlesSpoolRootErrors/spool_根目录读失败必须报错
--- PASS: TestListPendingSealsHandlesSpoolRootErrors (0.00s)
    --- PASS: TestListPendingSealsHandlesSpoolRootErrors/spool_根目录不存在返回空而非报错 (0.00s)
    --- PASS: TestListPendingSealsHandlesSpoolRootErrors/spool_根目录读失败必须报错 (0.00s)
=== RUN   TestListPendingSealsIgnoresNonLayoutEntries
--- PASS: TestListPendingSealsIgnoresNonLayoutEntries (0.00s)
=== RUN   TestSealRejectsTruncatedSpool
=== RUN   TestSealRejectsTruncatedSpool/目录扫描路径（Rows_未知，无交叉校验可用）
=== RUN   TestSealRejectsTruncatedSpool/行数已知路径（纵深防御，交叉校验本身也拦不住残行）
--- PASS: TestSealRejectsTruncatedSpool (0.00s)
    --- PASS: TestSealRejectsTruncatedSpool/目录扫描路径（Rows_未知，无交叉校验可用） (0.00s)
    --- PASS: TestSealRejectsTruncatedSpool/行数已知路径（纵深防御，交叉校验本身也拦不住残行） (0.00s)
=== RUN   TestSealAcceptsEmptySpool
--- PASS: TestSealAcceptsEmptySpool (0.01s)
=== RUN   TestSealerListPendingSealsUsesConfiguredMinAge
    sealer_test.go:1113: 默认 minAge=3h 时应且仅应发现 4 小时前的块（2 小时前的仍可能在被追加），实际 [/var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestSealerListPendingSealsUsesConfiguredMinAge350667274/002/spool/dt=2026-09-24/inst=blue/part-000001.jsonl /var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestSealerListPendingSealsUsesConfiguredMinAge350667274/002/spool/dt=2026-09-24/inst=blue/part-000002.jsonl]
--- FAIL: TestSealerListPendingSealsUsesConfiguredMinAge (0.00s)
=== RUN   TestSealRejectsMismatchedZstdVersion
--- PASS: TestSealRejectsMismatchedZstdVersion (0.00s)
=== RUN   TestSealRejectsUnparsableZstdVersion
=== RUN   TestSealRejectsUnparsableZstdVersion/输出里没有版本号
=== RUN   TestSealRejectsUnparsableZstdVersion/版本号只有两段
=== RUN   TestSealRejectsUnparsableZstdVersion/版本号带后缀
=== RUN   TestSealRejectsUnparsableZstdVersion/版本号段内非数字
--- PASS: TestSealRejectsUnparsableZstdVersion (0.01s)
    --- PASS: TestSealRejectsUnparsableZstdVersion/输出里没有版本号 (0.00s)
    --- PASS: TestSealRejectsUnparsableZstdVersion/版本号只有两段 (0.00s)
    --- PASS: TestSealRejectsUnparsableZstdVersion/版本号带后缀 (0.00s)
    --- PASS: TestSealRejectsUnparsableZstdVersion/版本号段内非数字 (0.00s)
=== RUN   TestSealAcceptsConfiguredZstdVersion
--- PASS: TestSealAcceptsConfiguredZstdVersion (0.01s)
=== RUN   TestListPendingSealsRejectsMalformedDirNames
--- PASS: TestListPendingSealsRejectsMalformedDirNames (0.00s)
=== RUN   TestSealRejectsFailedVersionProbe
--- PASS: TestSealRejectsFailedVersionProbe (0.00s)
FAIL
FAIL	github.com/example/llm-gateway/tests/tracearchive	0.298s
FAIL


======================================================================
### 变异侧 m49_删除路径Clean ⇒ 红 [names=32 ran=32 diff=0 PASS=31 SKIP=0 FAIL=1] 杀死: TestSealAcceptsCleanedEquivalentSpoolPath
======================================================================
=== RUN   TestSealIsAtomicOnCrash
--- PASS: TestSealIsAtomicOnCrash (0.04s)
=== RUN   TestParsePartSeqUsedBySealer
--- PASS: TestParsePartSeqUsedBySealer (0.00s)
PASS
ok  	github.com/example/llm-gateway/internal/tracearchive	0.052s
=== RUN   TestSealProducesArtifactAndRemovesSpool
--- PASS: TestSealProducesArtifactAndRemovesSpool (0.04s)
=== RUN   TestSealRefusesToOverwriteExistingArtifact
--- PASS: TestSealRefusesToOverwriteExistingArtifact (0.00s)
=== RUN   TestSealRejectsRowCountMismatch
--- PASS: TestSealRejectsRowCountMismatch (0.00s)
=== RUN   TestSealFooterPointsAtValidTrailer
--- PASS: TestSealFooterPointsAtValidTrailer (0.04s)
=== RUN   TestSealFailsOnNonZeroExit
--- PASS: TestSealFailsOnNonZeroExit (0.01s)
=== RUN   TestSealFailsWhenZstdMissing
--- PASS: TestSealFailsWhenZstdMissing (0.00s)
=== RUN   TestSealGoldenLayout
--- PASS: TestSealGoldenLayout (0.01s)
=== RUN   TestListPendingSealsSkipsFreshFiles
--- PASS: TestListPendingSealsSkipsFreshFiles (0.00s)
=== RUN   TestListPendingSealsReturnsStaleFiles
--- PASS: TestListPendingSealsReturnsStaleFiles (0.00s)
=== RUN   TestListPendingSealsIgnoresIllegalNames
--- PASS: TestListPendingSealsIgnoresIllegalNames (0.00s)
=== RUN   TestSealInvokesZstdCLIWithArgs
=== RUN   TestSealInvokesZstdCLIWithArgs/默认级别与默认窗口
=== RUN   TestSealInvokesZstdCLIWithArgs/显式级别与显式窗口
--- PASS: TestSealInvokesZstdCLIWithArgs (0.03s)
    --- PASS: TestSealInvokesZstdCLIWithArgs/默认级别与默认窗口 (0.01s)
    --- PASS: TestSealInvokesZstdCLIWithArgs/显式级别与显式窗口 (0.01s)
=== RUN   TestSealDefaultsZstdBinaryFromPath
--- PASS: TestSealDefaultsZstdBinaryFromPath (0.01s)
=== RUN   TestNewSealerValidatesOptions
=== RUN   TestNewSealerValidatesOptions/空_Root_被拒绝
=== RUN   TestNewSealerValidatesOptions/非_2_的幂窗口被拒绝
--- PASS: TestNewSealerValidatesOptions (0.00s)
    --- PASS: TestNewSealerValidatesOptions/空_Root_被拒绝 (0.00s)
    --- PASS: TestNewSealerValidatesOptions/非_2_的幂窗口被拒绝 (0.00s)
=== RUN   TestSealRejectsUnknownArtifactState
--- PASS: TestSealRejectsUnknownArtifactState (0.00s)
=== RUN   TestSealFailsWhenSpoolMissing
--- PASS: TestSealFailsWhenSpoolMissing (0.00s)
=== RUN   TestSealRejectsSpoolPathNotMatchingRef
--- PASS: TestSealRejectsSpoolPathNotMatchingRef (0.00s)
=== RUN   TestSealAcceptsCleanedEquivalentSpoolPath
    sealer_test.go:700: 与规范路径等价的 SpoolPath（含 ./）不该被拒: tracearchive: 封块失败: task.SpoolPath="/var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestSealAcceptsCleanedEquivalentSpoolPath690928276/001/./spool/dt=2026-09-24/inst=blue/part-000000.jsonl" 与 Ref{2026-09-24, blue, 0} 对应的 "/var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestSealAcceptsCleanedEquivalentSpoolPath690928276/001/spool/dt=2026-09-24/inst=blue/part-000000.jsonl" 不一致（拒绝封块：产物会以 Ref 寻址却装进另一文件的内容）
--- FAIL: TestSealAcceptsCleanedEquivalentSpoolPath (0.00s)
=== RUN   TestSealReportsMidRunCancellationAsCanceled
--- PASS: TestSealReportsMidRunCancellationAsCanceled (0.01s)
=== RUN   TestSealFailsWhenSpoolIsDirectory
--- PASS: TestSealFailsWhenSpoolIsDirectory (0.00s)
=== RUN   TestSealAcceptsRowsZeroFromDiscoveryPath
--- PASS: TestSealAcceptsRowsZeroFromDiscoveryPath (0.04s)
=== RUN   TestListPendingSealsHandlesSpoolRootErrors
=== RUN   TestListPendingSealsHandlesSpoolRootErrors/spool_根目录不存在返回空而非报错
=== RUN   TestListPendingSealsHandlesSpoolRootErrors/spool_根目录读失败必须报错
--- PASS: TestListPendingSealsHandlesSpoolRootErrors (0.00s)
    --- PASS: TestListPendingSealsHandlesSpoolRootErrors/spool_根目录不存在返回空而非报错 (0.00s)
    --- PASS: TestListPendingSealsHandlesSpoolRootErrors/spool_根目录读失败必须报错 (0.00s)
=== RUN   TestListPendingSealsIgnoresNonLayoutEntries
--- PASS: TestListPendingSealsIgnoresNonLayoutEntries (0.00s)
=== RUN   TestSealRejectsTruncatedSpool
=== RUN   TestSealRejectsTruncatedSpool/目录扫描路径（Rows_未知，无交叉校验可用）
=== RUN   TestSealRejectsTruncatedSpool/行数已知路径（纵深防御，交叉校验本身也拦不住残行）
--- PASS: TestSealRejectsTruncatedSpool (0.00s)
    --- PASS: TestSealRejectsTruncatedSpool/目录扫描路径（Rows_未知，无交叉校验可用） (0.00s)
    --- PASS: TestSealRejectsTruncatedSpool/行数已知路径（纵深防御，交叉校验本身也拦不住残行） (0.00s)
=== RUN   TestSealAcceptsEmptySpool
--- PASS: TestSealAcceptsEmptySpool (0.01s)
=== RUN   TestSealerListPendingSealsUsesConfiguredMinAge
--- PASS: TestSealerListPendingSealsUsesConfiguredMinAge (0.00s)
=== RUN   TestSealRejectsMismatchedZstdVersion
--- PASS: TestSealRejectsMismatchedZstdVersion (0.00s)
=== RUN   TestSealRejectsUnparsableZstdVersion
=== RUN   TestSealRejectsUnparsableZstdVersion/输出里没有版本号
=== RUN   TestSealRejectsUnparsableZstdVersion/版本号只有两段
=== RUN   TestSealRejectsUnparsableZstdVersion/版本号带后缀
=== RUN   TestSealRejectsUnparsableZstdVersion/版本号段内非数字
--- PASS: TestSealRejectsUnparsableZstdVersion (0.01s)
    --- PASS: TestSealRejectsUnparsableZstdVersion/输出里没有版本号 (0.00s)
    --- PASS: TestSealRejectsUnparsableZstdVersion/版本号只有两段 (0.00s)
    --- PASS: TestSealRejectsUnparsableZstdVersion/版本号带后缀 (0.00s)
    --- PASS: TestSealRejectsUnparsableZstdVersion/版本号段内非数字 (0.00s)
=== RUN   TestSealAcceptsConfiguredZstdVersion
--- PASS: TestSealAcceptsConfiguredZstdVersion (0.01s)
=== RUN   TestListPendingSealsRejectsMalformedDirNames
--- PASS: TestListPendingSealsRejectsMalformedDirNames (0.00s)
=== RUN   TestSealRejectsFailedVersionProbe
--- PASS: TestSealRejectsFailedVersionProbe (0.00s)
FAIL
FAIL	github.com/example/llm-gateway/tests/tracearchive	0.281s
FAIL


还原校验: 一致
EXIT=0
```

## 12.5 ⑤ Seal 校验 `SpoolPath` 与 `(DT,Inst,Part)` 一致

**落实**：新增守卫（`sealer.go:133-146`），位于 `Seal` 的**最前面** —— 比产物存在性
检查还早。理由是它与「不依赖编码器的判定必须先报」（§10.3 那条原则）同一逻辑的更
前一步：它是**纯内存的调用契约判定**（一次字符串比较，连 `stat` 都不做），而畸形调用
不该先去看世界状态。不一致一律以既有哨兵 `ErrSealFailed` 拒绝，错误信息带上两侧路径。

```go
	wantSpool := s.paths.SpoolFile(Ref{DT: task.DT, Inst: task.Inst, Part: task.Part})
	if task.SpoolPath != wantSpool {
		return fmt.Errorf("%w: task.SpoolPath=%q 与 Ref{%s, %s, %d} 对应的 %q 不一致（拒绝封块：产物会以 Ref 寻址却装进另一文件的内容）", ...)
	}
```

用例 `TestSealRejectsSpoolPathNotMatchingRef`（`sealer_test.go:607-639`）：写一份属于
`part 1` 的 spool，却让任务声称它是 `part 0`；断言 `ErrSealFailed`、错误文案含
「不一致」、不产出 `part 0` 产物、不残留 `.tmp`。**该用例刻意不提供 zstd**，因此它
同时钉住了「该判定先于版本探测与一切 I/O」。

删除变异：`m48_删除SpoolPath与Ref一致性校验` ⇒ 红，杀死
`TestSealRejectsSpoolPathNotMatchingRef`（§12.4.5）。

**连带修正（必须记录）**：`TestSealFailsWhenSpoolIsDirectory` 原来的 fixture 把
`SpoolPath` 设成 **spool 目录**（`SpoolDir(...)`），而新守卫要求 `SpoolPath` 等于规范
文件路径 —— 若不改，该用例会被新守卫先拦下，`m16`（删除「读 spool 失败」守卫）将
**不再挂死、覆盖丢失**。已把 fixture 改为「在**规范 spool 文件路径**上放一个目录」
（`os.MkdirAll(SpoolFile(...))`），路径自洽、`os.Open` 成功、读时 EISDIR，覆盖面不变。

## 12.6 ⑥ `defaultSealMinAge` 注释拆成两个命题

原注释把「压缩比档位目标」与「正确性约束」混在一句里，容易被读成「15m 已验证了压缩比
档位」。现已拆开（`sealer.go:35-55`）：

```
【两个不同的命题，不要混为一谈（评审第 ⑥ 条）】
 1. 正确性约束：minAge 必须严格大于网关滚动间隔。**现有 defaultSealInterval=15m
    满足这一条**（3h 对它有 12 倍余量），因此当前默认值在正确性上是安全的。
 2. 压缩比档位目标（**15m 不满足这一条**）：计划 2 应把网关滚动间隔调到 ~1h，好让
    1536 MiB 的【大小】阈值先触发，从而落在已标定 10.26x 的档位上；当前 15m 会让
    【时间】阈值先触发，块被截到 ~400 MB，压缩比落进**未标定区间**。也就是说，
    10.26x 这个验收线只有在第 2 条落地后才真正被覆盖。
```

## 12.7 ⑦ 并发注释降为准确表述

`sealer.go:86-96`：删除「可以在多个 goroutine 间共享调用 `Seal` 而无需同步」这种
过强表述，改为「不同 `Ref` 之间可并发；**同一个 `(dt, inst, part)` 必须串行**（共用
同一个 `partPath+".tmp"`）」。同时显式说明：**这不动摇裁决 4 的依据** —— 被删掉的
`versionOK` 是**任意**并发调用（含不同 Ref）都会写的未同步共享状态，竞态依然存在，
所以删记忆化仍然正确。

## 12.8 ⑧ A 层例外理由

`sealer_internal_test.go:46-53`：为 `TestParsePartSeqUsedBySealer` 补上「为什么必须
触达未导出符号」的说明，并指明 B 层已有的同类覆盖（`TestListPendingSealsIgnoresIllegalNames`
覆盖同一约定在真实目录扫描下的行为，本条只做 `.tmp` 不被当合法块名的定点确认）。

## 12.9 ⑨ 变异编号审计

编号一经发布不再改动（历史报告的表可直接对照），因此采用「显式列出退役编号」方案，
写进脚本头部注释：

| 编号 | 退役原因 |
|---|---|
| `m22_删除MinAge默认值` | 默认值常量由 `defaultSealInterval` 改为 `defaultSealMinAge`（裁决 1）后，它与新 `m35` 是同一守卫，重复项删除，由 `m35` 取代 |
| `m44_版本探测结果不记忆` | 裁决 4 直接删除了记忆化分支（字段 `s.versionOK`），**无分支可删** |

除此之外编号连续：`m01–m47` + 评审轮新增 `m48` + 复审轮 2 新增 `m49`（共 **47** 条，`m22`/`m44` 为登记缺口）。
**判据达成**：读者不需要问「22 去哪了」。

## 12.10 ⑬ `.go` 注释里的穿越示例更正（跨任务编辑，已获授权）

先说清**权属**：`writer.go` 与 `writer_test.go` 属任务 4/5 的工件，我此前被要求不得
修改；本轮控制者明确授权（「现在没有其他人动它们，可安全编辑」）后，我只改**注释示例**
与两条用例的**输入列表**，不动任何实现逻辑。计划文档里的代码块是这些注释的逐字副本，
**文档同步由文档代理下一轮做，我未改计划文档**。

**我实测的数据（root="/r"，自己跑 `filepath.Join` 复现，不转述）**：

| 输入 | 实测结果 | 旧注释的说法 |
|---|---|---|
| `inst="../green"` | `/r/dt=X/inst=../green`（**不折叠**） | ❌「清成 /r/dt=X/green」 |
| `dt="../x"` | `/r/dt=../x/inst=blue`（**不折叠**） | ❌「清成 /r/x」 |
| `inst="blue/"` | `/r/dt=X/inst=blue`（只去掉尾部分隔符） | ⚠️「清成 blue」表述不准确 |
| `inst="../../green"` | `/r/dt=X/green`（越出 inst= 层） | ✔️（真向量） |
| `inst="a/../../green"` | `/r/green` | ✔️（真向量） |
| `dt="2026-09-24/../.."` | `PartDir=/inst=blue`、`SpoolDir=/r/inst=blue`（越出 root） | ✔️（真向量） |

**更正规则**（四处统一）：结论保留（需要校验；一般情形下穿越可能），**示例改为含 `/`
的真实向量**，并补一句原因：`PartDir/SpoolDir` 会拼回 `dt=`/`inst=` 前缀，紧贴前缀的
`..` 被吸收成字面名，只有**裸 `..` 元素**才被 `Clean` 折叠。

落点：`writer.go:14-33`、`writer_test.go:243-256`（inst 用例）、`writer_test.go:553-585`
（dt 用例，含两处 inline 注释修正）、`sealer.go:537-555`、`sealer_test.go:1169-1182`。

**额外（超出字面要求，请评审裁定）**：我顺手把真实向量加进了两条用例的输入列表
（inst 加 `../../green`、`a/../../green`；dt 加 `2026-09-24/../../x`），好让「注释举的
例子」与「用例实际覆盖的输入」一致 —— 否则注释里的真向量没有任何用例对着它。两条用例
仍然全绿（白名单/日期校验本就拒绝它们）。

### 12.10.1 前置修正（复审轮 2）：B′ 的论据作废并按实测重做

**先认账。** §12.10.1 原稿写「B′ 实测真的在 `<root>` 之外创建了 `…/T/x/inst=blue`」，
而同一轮的归档证据 `added-vectors-isolated-probe.txt` 写的是「**（未找到）**」——
**报告与归档互相矛盾，且归档是对的**（归档是当时的真实输出）。复审者复现并给了更准的
结论，我用探针重测确认：

| 向量 | 实测落点（root = 临时目录） | 越出范围 |
|---|---|---|
| `dt="2026-09-24/../../x"` | `<root>/x/inst=blue` | 越出 **spool 子树**，但**仍在 root 之内** |
| `dt="2026-09-24/../../.."` | `<parent-of-root>/inst=blue` | 越出 **root 本身** |

原稿把「越出 spool 子树」写成了「越出 root」，且与自己的归档相反 —— 这是**证据与结论
不一致**，比单纯写错更严重。修正动作：① 按实测改准本节与 §12.10 的措辞；② **重做实验**
并把三类证据（隔离探针 A/B/B′、落点测量、守卫搬家、m49）合成一份新的归档
`added-vectors-isolated-probe.txt`，使归档与报告一致。

**方法论教训（写给后来者）**：报告里任何「我实测过 X」都必须能在当前归档里逐字找到；
一旦报告与归档冲突，**以归档为准**，然后**重做实验**而不是改口径去迁就结论。

### 12.10.2 裁决 (a) 核实：新增的两个真向量确实是「必须被拒绝」（有断言、可杀死）

**结论：是。** 两处都在 `err == nil ⇒ t.Fatalf` 的断言环里，不是「能跑通」而已：

| 落点 | 断言行 | 语义 |
|---|---|---|
| `tests/tracearchive/writer_test.go:259` | `263` `t.Fatalf("Inst=%q 应被拒绝…")` | `NewWriter` 返回 nil error 即红 |
| `tests/tracearchive/writer_test.go:582`（`"2026-09-24/../../x"`） | `587`/`590` 拒绝断言、`599` 「非法 dt 不得产出任何文件/目录」 | 任一处的 nil error 即红 |

**方法论陷阱**：两条用例是**共享断言环**，环内更早的条目（inst 的 `"red"`、dt 的 `""`）
会**遮蔽**新增条目 —— 「把守卫摘掉后整条用例变红」**不能**证明新增条目本身承重。
为此写了只含新增向量的**隔离探针**（临时文件，未提交，跑完即删）：

| 侧 | 改动 | 探针结果 |
|---|---|---|
| A | 守卫在位（= 提交 `69d9c317`） | **PASS**：`../../green`、`a/../../green` 各被白名单拒绝；`Append("2026-09-24/../../x")` 被 `validateDT` 拒绝 |
| B | 摘除 `NewWriter` 的 inst 白名单 | **FAIL** 于第一条新增向量（`inst="../../green" 必须被拒绝`） |
| B′ | 只摘除 `validateDT` | **FAIL** 于新增的 dt 向量；且**真的**按实测落点写到了 `<root>/x/inst=blue`（root 内、spool 外，见 §12.10.1 表） |

### 12.10.3 `sealer_test.go` 的两条执行顺序判据（复审「轻微 1」）

原判据「先于版本探测与**一切 I/O**」超出用例能钉住的范围（复审者把守卫搬到 `os.Stat`
之后，用例**仍然 PASS**）。已收窄为两条各自可被实测钉住的命题，并**真的钉住**：

| 命题 | 如何钉住 | 守卫搬家后的实测 |
|---|---|---|
| (a) 先于**版本探测** | `ZstdBinary` 指向**不存在**的文件（原「不给 ZstdBinary」钉不住：默认值 `"zstd"` 在本机存在，本机版本恰等于默认版本时探测会通过） | 搬到探测之后 ⇒ **红**（错误变成 ErrPartExists / 探测失败，文案不含「不一致」） |
| (b) 先于**产物存在性检查** | 先放一份 part 0 的**既有产物哨兵**，断言收到的不是 `ErrPartExists` 且哨兵内容未被碰；并反向断言「`SpoolPath` 正确时确实会撞 `ErrPartExists`」以免 (b) 成为空断言 | 搬到 `os.Stat` 之后 ⇒ **红** |

顺带更正措辞：原注释说「本用例不提供 zstd」不准确 —— `ZstdBinary` 走默认值 `"zstd"`，
本机存在；不准的是「不给**假** zstd」。已改写。

### 12.10.4 三条轻微项

| 轻微项 | 处理 | 落点 / 证据 |
|---|---|---|
| 1 判据过宽 + zstd 措辞 | 收窄为 (a)(b) 两条并实测钉住（§12.10.3） | `sealer_test.go:614-627,628-675` |
| 2 `fatal error:` / `signal: killed` 被误判「证据无效」 | `classify()` 增认这两类 ⇒ 判**红(崩溃/中止)**，与 #25 一致 | `task-6-mutate.py:459-461` |
| 3 字符串相等比较可能误拒等价拼写 | 比较前 `filepath.Clean(task.SpoolPath)`；**不**做 `EvalSymlinks`（要读文件系统，会破坏「纯内存判定」）；符号链接造成的误拒**登记为已知限制**；新增用例 `TestSealAcceptsCleanedEquivalentSpoolPath` 锁定，删掉 Clean 即红（变异 `m49`） | `sealer.go:142-148`、`sealer_test.go:676-706`、变异 `m49` |

### 12.10.5 本轮落点（`文件:行号`）

| 文件 | 行 | 内容 |
|---|---|---|
| `internal/tracearchive/sealer.go` | 142-148 | `filepath.Clean` 后再比较 + 不做 EvalSymlinks 的理由 + 已知限制登记 |
| `tests/tracearchive/sealer_test.go` | 607-627 | ⑤ 用例判据收窄说明（(a) 先于版本探测 / (b) 先于产物存在性检查） |
| | 628-675 | ⑤ 用例本体：不存在的 zstd 路径 + 既有产物哨兵 + 反向断言 |
| | 677-706 | 新增 `TestSealAcceptsCleanedEquivalentSpoolPath` |
| | 1271 / 1274 | 畸形目录 fixture 内联注释按精确口径改写 |
| `.superpowers/.../task-6-mutate.py` | 38-101 | `EMBEDDED_NAMES` 冻结清单（32 条）+ `intent_error()` / `validate_intent()` + 启动自检 |
| | 357-372 | m48 模式跟随新代码（Clean 版）+ 新增 m49 |
| | 425-441 | `verify()`：断言 1a/1b/2/3（含 `SKIP != 0` ⇒ 无效） |
| | 459-461 | `classify()` 增认 `fatal error:` / `signal: killed` |
| | 544-600 | `--selftest` 六项（A/B/C/C′/D/D′） |

## 12.11 只登记（⑩⑪⑫ + 复审范围外 ⑭）

**⑩ `SealTask.UncompressedBytes` 未被消费；`scanSpoolOffsets` 的总字节数被 `_` 丢弃。**

分析（控制者给出，我复核后认同）：在**行边界**截断得到的是一个**一致的前缀** ⇒
`row_count` 也随之变小 ⇒ `row < count` 的点读仍读到**正确**正文，`row >= count` 是
响亮的越界错误。**不是静默错正文路径**，所以只值一条登记（潜在产量偏小），不值一次
补偿逻辑（简报也明确禁止跳号补偿）。

**⑪ `fsyncDir` 失败后产物已 rename 但不删 spool ⇒ spool 永久滞留 + 持续告警。**

登记给**计划 4**。⚠️ **不要**采用「fsync 失败时也删 spool」的解法：目录 fsync 失败
意味着 **rename 的持久性未确认**，删 spool 是拿**数据丢失**换告警停止。正确修法是
**重投递幂等** —— `Seal` 遇到 `ErrPartExists` 时应能确认既有产物完整（行数/帧哈希）
后删除 spool，而不是每轮重复拒绝。这属于 sealer 循环（计划 4）的职责。

**⑫ `MkdirAll` 早于压缩 ⇒ 子进程失败会留下空 `dt=/inst=` 目录（会在产物树累积）；
`RowCount` 为 int32、`int32(len(offsets))` 无上界校验。**

两条都只登记：前者是空间/整洁问题（不影响正确性），后者需 >2^31 行才触发、属既有
格式约束。

**⑭（复审范围外，登记）`ListPendingSeals` 对文件级条目不查 `IsDir`。**

`sealer.go` 列举 spool 时只按「目录 → 目录 → 文件」的形状取路径，不校验最后一级是不是
普通文件。因此一个**名为 `part-000000.jsonl` 的目录**会被当成待封块交给 `Seal`；`Seal`
随后以「读 spool 失败：is a directory」**响亮失败** ⇒ **不静默**（`TestSealFailsWhenSpoolIsDirectory`
的 fixture 恰好就是这个状态，属既有行为，本任务未改动）。登记理由：不构成静默错正文路径；
若要收紧，应在列举侧加 `IsDir` 检查并配一条「目录形状条目被跳过」的用例（属任务 4/5 或计划 4 范围）。

## 12.12 全部落点（文件:行号）—— 供文档代理同步

| 文件 | 行 | 内容 |
|---|---|---|
| `internal/tracearchive/sealer.go` | 35-55 | ⑥ minAge 推导拆两命题 |
| | 86-96 | ⑦ 无状态 + 并发边界（不同 Ref 可并发 / 同一 Ref 必须串行） |
| | 133-148 | ⑤ `SpoolPath` ⇄ `Ref` 一致性守卫（Seal 最前）；复审轮 2 起比较前 `filepath.Clean`（142-148） |
| | 386-393 | ② m15 保留理由＝纵深防御 |
| | 411-418 | ① ctx 归一化分支（未改代码；新增用例覆盖它） |
| | 488-518 | ③ 列举侧 3b 说明（健壮性/对称性，非防穿越） |
| | 537-555 | ③ 循环体内 dt 校验理由 + 真实穿越向量 |
| | 573-576 | ③ inst 白名单（未改，措辞已准确） |
| `internal/tracearchive/writer.go` | 14-33 | ⑬ 路径分量校验：示例改为含 `/` 的真向量 |
| `internal/tracearchive/sealer_internal_test.go` | 46-53 | ⑧ A 层例外理由 |
| `tests/tracearchive/sealer_test.go` | 607-675 | ⑤ 新用例 `TestSealRejectsSpoolPathNotMatchingRef`（复审轮 2 起含 (a)(b) 两条顺序判据，见 §12.10.3） |
| | 708-777 | ① 新用例 `TestSealReportsMidRunCancellationAsCanceled` |
| | 713-728 | ① `exec sleep` 的说明注释 |
| | 677-706 | 复审轮 2 新增 `TestSealAcceptsCleanedEquivalentSpoolPath`（`filepath.Clean` 的锁定用例） |
| | 779-807 | ⑤ 连带修正：`TestSealFailsWhenSpoolIsDirectory` 的 fixture |
| | 1237-1291 | ③ 畸形目录用例理由更正（含 1271/1274 两处内联注释） |
| `tests/tracearchive/writer_test.go` | 243-256 | ⑬ inst 用例注释 + 两个真向量输入 |
| | 553-585 | ⑬ dt 用例注释 + inline 修正 + 一个真向量输入 |
| `.superpowers/.../task-6-mutate.py` | 全文 | ④ 证据工具重建（三断言 + 无效判定 + `--side` + `--selftest`）、⑨ 退役编号表；复审轮 2 起：38-101 冻结清单与启动自检、425-441 断言 1a/1b/2/3、459-461 中止分类、544-600 六项自检；复审轮 2 的其余落点见 §12.10.5 |

## 12.13 本轮 GREEN 与三断言原始输出

```text
### 提交 69d9c317，干净导出目录 /tmp/sealcommit5
$ tr -d "[:space:]" < /tmp/sealrun.txt | tr "|" "\n" | sed "/^\$/d" | sort > /tmp/n32.txt
$ go test ./internal/tracearchive/ ./tests/tracearchive/ -run "$(cat /tmp/sealrun.txt)" -v -count=1
$ diff /tmp/n32.txt /tmp/r32.txt
  ↑ 无输出 ⇒ 断言1 成立（正则集合 == 实际执行集合，32 条）
$ python3 -c "检查「断言1 的另一半」：正则集合 == 脚本内嵌清单"
  内嵌清单=32 正则集合=32 相等=True
  内嵌清单与源文件一致（重新推导）=True

断言2：PASS=32  名字数=32  ⇒ 相等
断言3：SKIP=0   ← 新增判定，非 0 即整轮失败
子用例：12 条（不计入顶层）

$ tail -3 /tmp/g32.txt
--- PASS: TestSealRejectsFailedVersionProbe (0.00s)
PASS
ok  	github.com/example/llm-gateway/tests/tracearchive	0.303s
```

## 12.14 偏离与残余不确定

**本轮新增偏离**

| # | 偏离 | 依据 |
|---|---|---|
| 22 | `Seal` 新增「`SpoolPath` ⇄ `Ref` 一致」前置校验（导出 API 的调用契约） | 评审第 ⑤ 条；判据是消除「点读静默返回错正文」 |
| 23 | 修改了任务 4/5 的 `writer.go` / `writer_test.go`（注释示例 + 两条用例的输入列表） | 评审第 ⑬ 条明确授权；只动注释与测试输入，不动实现 |
| 24 | `TestSealFailsWhenSpoolIsDirectory` 的 fixture 由「指向 spool 目录」改为「规范路径上是目录」 | ⑤ 的连带必要性：否则新守卫先拦下，`m16` 的挂死覆盖丢失 |
| 25 | 变异自证脚本对「panic 中止」判**红(集合不完整)**而非「无效」 | 无效只留给拿不到判决的运行；红的判定不需要完整性，绿的判定才需要 |
| 26 | 双侧输出：原始侧完整打印一次 + 附其输出字节的 sha256（而非 9 份逐字副本）；哈希对象与提取规则在 §12.4.6 写明并已自校 | 控制者裁决 (b) 接受，理由：内容寻址强于逐字副本；逐字副本仍可 `--side` 重跑 |
| 27 | 用例数 29 → 32（评审轮新增 ⑤、① 两条；复审轮 2 新增 Clean 等价路径一条） | 评审第 ⑤ 条、第 ① 条；复审「轻微 3」 |
| 28 | 变异 45 → 47（`m48`、`m49`；`m22`/`m44` 登记退役） | 评审第 ⑤ 条、第 ⑨ 条；复审「轻微 3」 |

| 29 | 脚本内**冻结** 32 条期望名单（`EMBEDDED_NAMES`），并把「正则集合 == 清单」作为启动前置条件 | 复审骗法 C-5：作者意图必须可验证；只加数量下限挡不住合法子集 |
| 30 | `verify()` 增「`SKIP != 0` ⇒ 证据无效」 | 复审骗法 C-6；与脚本 docstring/§12.4.1 早先声明的「断言 3」对齐 |
| 31 | ⑤ 守卫比较前 `filepath.Clean`；不做 `EvalSymlinks`（符号链接误拒登记为已知限制） | 复审「轻微 3」：避免等价拼写被误拒，同时不破坏「纯内存判定」 |

**残余不确定 / 待你裁决**

1. §12.10 里我**额外**给两条别人任务的用例加了真向量输入（超出字面要求）——若你希望
   保持「只改注释」的最小改动面，我可以撤回这一项（撤回后注释里的真向量没有对应用例）。
2. §12.4.6 的原始侧「一份 + 哈希」处理方式是否接受（见上表 #26）。
3. m15 现在是**唯一**的真零覆盖条目（`m34` 是等价变换，见下）；`m15` 的 TOCTOU 窗口
   在单元测试里确实不可达 —— 控制者已接受该缺口，本轮仅把理由改准。
4. `m34_删除sort顺序`：`sort.Slice` 与 `os.ReadDir` 的返回顺序在当前 fs 实现下等价，
   仍是零覆盖（首轮已说明）。本轮它通过三断言自证（31 条全跑、全绿）——**这是
   「证据有效」意义上的绿**，而不是「没跑」意义上的绿。
# 13 修复轮 3：**分支级验证基础设施**（第三次攻击复审 + 追加 A–G）

> 本轮交付物的性质由追加 A/F/G 改变：产出**不是**「Task 6 的工具」，而是**分支级的
> 验证基础设施**。Task 8、R11 与分支收尾的「全任务证据重跑」都用它；工具若写成
> Task 6 专用，会让 R11 变贵。

**阅读约定**：本节每条论断都带 `文件:行号` 或**归档指针**（`task-6-evidence/<文件名>`）。
**归档与本节冲突时以归档为准**，并重做该实验 —— 归档是工具当场产出的原始输出，
本节只是它的摘要。

## 13.0 前置（追加 C：非空即停）与最终绑定

开工前三项前置（全部通过后才动手）：

- `git rev-parse HEAD` = `0c42054753fffc9be4b7459c0c28fc56892ab818`
- `git status --porcelain` = **空**（干净）
- `pgrep -f 'mutat[e]\.py|go tes[t]|go bui[l]d'` = **空**（无兄弟 agent 在同一工作树写入）

本轮结束时的绑定（原始输出 `task-6-evidence/round3-mutation-table.txt` 头部）：

```text
## 证据绑定（这些是事实，不是声明）
git rev-parse HEAD       = 7bf7d112d53690a0942abafe5f9d80ee240851f5
git status --porcelain   = (空)
工具自身 sha256          = 10dea750fec073af9fd5b3af262ad6ec6f020aefa223da6b079e1c5a9cf708f8  (/Users/jsy/Public/self_code/llm-gateway/.worktrees/trace-body-direct-write/tools/trace-mutate/trace_mutate.py)
变异集合 sha256          = 6d20f5d699fc7519d2fc69fdcefee7530ab696c497c18e68847b4b123817c4cf  (/Users/jsy/Public/self_code/llm-gateway/.worktrees/trace-body-direct-write/tools/trace-mutate/sets/task-6.py)
名字清单 sha256          = c27020f20e00916b50a276e808e9382583209bf84fc2dc6b2a1bd6c6ae53b893  (/Users/jsy/Public/self_code/llm-gateway/.worktrees/trace-body-direct-write/.superpowers/sdd/2026-09-24-tracearchive-storage-primitives/task-6-evidence/names.txt)
名字清单条数             = 34
清单推导命令             = grep -h '^func Test' tests/tracearchive/sealer_test.go internal/tracearchive/sealer_internal_test.go | sed -E 's/^func (Test[A-Za-z0-9_]*).*/\1/' | sort
推导结果与清单比对       = 一致（推导出 34 条）
pristine 来源            = git archive HEAD | tar -x  →  /var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/trace-mutate-q2v1nykq
被测文件 sha256          = ff4efc76a1b728aca9d6c50ce1d464d6cf8d983a44d4810dcd293c6d5a5ea437  internal/tracearchive/sealer.go
测试文件 sha256          = bf9283cbe2370e03f9f3031f5c182a7f3958038a9ba45d1b0173bc647d8584c8  internal/tracearchive/calib_test.go
测试文件 sha256          = d8665dfc1c666f05fde260813a732014db5ea07906fced8dada8edf8cd92fb53  internal/tracearchive/footer_test.go
测试文件 sha256          = 48586664e2b599f81624d816abee6f25329b9f8b62bdc801b7d3594853fd919e  internal/tracearchive/index_test.go
测试文件 sha256          = 03fe2aa81b077ae87e051fe6a8c1705784b7539f30db49ed4d1e5b02dc9a5299  internal/tracearchive/reader_internal_test.go
测试文件 sha256          = a482bb48538c16b000faecad96580a6fa91699e94954dd0e3d9a67f2f948f025  internal/tracearchive/ref_internal_test.go
测试文件 sha256          = dd55a0b7fb7c64bd802f849c7e63b2790fb14206aba9798210c40d22e9824600  internal/tracearchive/sealer_internal_test.go
测试文件 sha256          = 862de96fbf7f6a1f43f7cefd28f332a41aebd8ac2eb9d33acf7002609ea185ea  internal/tracearchive/testhelpers_test.go
测试文件 sha256          = adc19f6428686cfd442389c4c88e0072221baf17ceb7746ff67cdc9ffb731588  internal/tracearchive/writer_internal_test.go
测试文件 sha256          = b458652e42c8ad4ce35b082d4a81e2532a57b25fb313dc380ed6dd659d2ca9c8  tests/tracearchive/helpers_test.go
测试文件 sha256          = caa45152ebadc6926b660cc2a94b55c9e063bcc2b5a69b0c5bc44c627cc875aa  tests/tracearchive/reader_test.go
测试文件 sha256          = bdafe97489a2a1255e28f0dd4b10bcfeda8e781f74cb5bbb3aca20646762f826  tests/tracearchive/ref_test.go
测试文件 sha256          = 66f8033e68ea372fcb96b4836d566eb2a0e092ed58bb90d4f2612d5918e305bb  tests/tracearchive/sealer_test.go
测试文件 sha256          = 7ba83e027f9e21bfb18901f5b3feacbd80cff854afa17b62890178c7416f7ea1  tests/tracearchive/writer_test.go
子进程环境（白名单）     = HOME LANG LOGNAME PATH SHELL TMPDIR USER
父进程中被剔除的敏感变量 = （无）
```

⇒ 权威表绑定在提交 `7bf7d112`、工具 `10dea750…`、集合 `6d20f5d6…`、清单 `c27020f2…` 上。
三者任一被改动，上述哈希都会变，「这张表是哪个工具/集合产出的」因此是**事实**。

## 13.1 严重① 封闭子进程环境 —— 而且它不是「洗白」，是**伪造任意判定**

修法：`go test` 子进程只拿到**白名单**环境（`tools/trace-mutate/trace_mutate.py:73-95` 的
`ENV_ALLOW` / `ENV_DENY_PREFIX` / `ENV_DENY_EXACT`）。父进程里的 `GOFLAGS`/`GOOS`/`GOARCH`/`GOTOOLCHAIN`/
`GOCACHE`/`GOMODCACHE`/`GOPATH`/`GOENV`/`CC`/`CGO_*` 一类变量一律不传；
白名单而非黑名单 —— 新增变量必须**显式批准**，而不是「默认继承、再删几个」，
否则默认值会随 Go 新版本悄悄变宽。工具每次运行都打印白名单与「被剔除的敏感变量」。

### 13.1.1 实测：这个机制能伪造**任意**判定（四份归档）

| # | 实验 | 归档 | 结果 |
|---|---|---|---|
| 1 | `m15`（**真实零覆盖**：测试不可达的纵深防御）诚实运行 | `goflags-demo-1-honest-m15-green.txt` | rc=0，PASS=34 / FAIL=0 ⇒ **绿（零覆盖）** |
| 2 | 同一棵树，只多加 `GOFLAGS=-exec=goflags-exec-forge-red.sh` | `goflags-demo-2-forged-red-m15.txt` | rc=1，PASS=**0** / FAIL=**34** ⇒ **伪造成红**；wrapper 被调用 2 次 |
| 3 | `m49`（真红）加 `GOFLAGS=-exec=goflags-exec-wash-red-green.sh` | `goflags-demo-3-washed-green-m49.txt` | rc=0，PASS=34 / FAIL=0 ⇒ **洗成绿** |
| 4 | **新工具** + `GOFLAGS=-exec=goflags-exec-forge-red.sh` | `goflags-demo-4-tool-closed-env.txt` | 见 13.1.2 |

第 2 步是把结论说重的地方：它**不是**「把失败洗成通过」，而是**能伪造任意判定** ——
包括把**零覆盖伪装成有覆盖**。`m15` 本来是测试不可达的纵深防御（缺口真实存在、
已在集合里登记），伪造之后它会显示为「红」，于是缺口**永远不会被发现**。
这比假绿更危险：假绿骗的是「这次改动的判定」，伪造红骗的是「**测试覆盖率的结论**」。

第 3 步是复审者最初给出的方向（4 条真红变异全部判绿、`exit 0`、自证串逐字同形）。

### 13.1.2 反证：新工具下同一条 `GOFLAGS` 完全无效

`task-6-evidence/goflags-demo-4-tool-closed-env.txt`（HEAD `7bf7d112`，`TOOL_EXIT=0`）：

```text
子进程环境（白名单）     = HOME LANG LOGNAME PATH SHELL TMPDIR USER
父进程中被剔除的敏感变量 = GOFLAGS
wrapper 调用次数 = 0（task-6-evidence/goflags-exec-wrapper-called.log 根本不存在）
| m15_删除打开spool失败守卫 | **绿（零覆盖！）** | names=34 ran=34 diff=0 PASS=34 SKIP=0 FAIL=0 | — |
| m49_删除路径Clean | 红 | names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1 | TestSealAcceptsCleanedEquivalentSpoolPath |
```

⇒ 同一条 `GOFLAGS=-exec=…` 在诚实工具下一次也没有生效：`GOFLAGS` 出现在
「被剔除的敏感变量」里、wrapper 调用次数为 **0**、判定回到诚实值
（`m15` = 绿且带登记理由，`m49` = 红）。

## 13.2 严重② 证据绑定提交

三条修法都落地，原始输出见 `task-6-evidence/round3-mutation-table.txt:1-26`：

1. **打印 HEAD 与树状态**；**脏树拒绝运行、非零退出**。实测归档
   `task-6-evidence/round3-refuse-dirty-tree.txt`：制造一个未跟踪文件后运行 ⇒
   `!!! 工作树不干净，拒绝产出证据（严重项 ②）` 且 `REFUSE_EXIT=1`，**没有产出任何表**。
2. **pristine 由 git 派生**：一律 `git archive HEAD | tar -x` **现取现解**，
   不再复用任何 `/tmp` 旧副本（`trace_mutate.py:251-261`）。解包出的目标文件还会与
   `git show HEAD:<target>` 逐字节比对，不一致即判证据无效（`trace_mutate.py:583-584`）。
   题面指出的活证据（工具测的 /tmp 树与工作区漂移 34 行）在本轮结构上不可能复发。
3. **记录被读测试文件的哈希**：打印被变异文件与**全部** `*_test.go` 的 sha256
   （`round3-mutation-table.txt:14-26`），其中
   `tests/tracearchive/sealer_test.go` = `66f8033e…`、
   `internal/tracearchive/sealer_internal_test.go` = `dd55a0b7…`、
   `internal/tracearchive/sealer.go` = `ff4efc76…`。

## 13.3 严重③ + 追加 A/F：通用工具 + 集合进版本库

### 13.3.1 追加 A：工具是**通用**的，不含任何 Task 6 常量

`tools/trace-mutate/`（**git-tracked**）：

| 文件 | 行数 | 作用 |
|---|---|---|
| `tools/trace-mutate/trace_mutate.py` | 707 | 通用引擎 |
| `tools/trace-mutate/sets/task-6.py` | 448 | **本任务的变异集合**（信任根） |
| `tools/trace-mutate/README.md` | 93 | 一分钟上手：怎么跑、产出什么、为什么可信 |

四个参数全部外置：`--manifest` / `--mutation-set` / `--packages` / `--target`；
工具本体只认集合声明的 `TARGET`/`PACKAGES` 作为缺省。**一个任务一次调用、无交互** ⇒
追加 B 要求的「分支收尾把所有任务重跑一遍」是廉价的（每个任务一条命令）。

### 13.3.2 追加 F：作者编写的产物进版本库，可推导的产物随地方便

| 产物 | 可推导？ | 位置 | 为什么可接受 |
|---|---|---|---|
| 名字清单（34 条） | **是** | 证据目录（gitignored）`task-6-evidence/names.txt` | 工具打印**推导命令**并**实际执行**它、与清单逐条比对；不符即证据无效 |
| 变异集合（51 条） | **否** | **git-tracked** `tools/trace-mutate/sets/task-6.py` | 树里没有任何东西能让第三方重算；留在 gitignored 空间时作者可悄悄删掉一条变异而 `git diff` 什么都看不到 |

「清单可推导」这一条不是声明而是**检查**：工具在 pristine 树里跑
`grep -h '^func Test' … | sed … | sort`，与清单比对，结果 `一致（推导出 34 条）`
（`round3-mutation-table.txt:8-9`）。清单被改小 ⇒ 比对不一致 ⇒ 整轮失败。

### 13.3.3 三哈希全部打印

工具 `10dea750fec073af9fd5b3af262ad6ec6f020aefa223da6b079e1c5a9cf708f8`、
集合 `6d20f5d699fc7519d2fc69fdcefee7530ab696c497c18e68847b4b123817c4cf`、
清单 `c27020f20e00916b50a276e808e9382583209bf84fc2dc6b2a1bd6c6ae53b893`
（`round3-mutation-table.txt:3-6`）。

上一版脚本（594 行，住在 gitignored 的 `.superpowers/`）全文存档在
`task-6-evidence/task-6-mutate.py.prior-to-round3`（sha256 `df78eaac…`，可逐行 diff）；
原路径 `task-6-mutate.py` 改成 20 行**转发 shim**，**不再是证据链的一环** ——
包/目标/集合/清单全部转由被跟踪的集合读取，shim 不重复声明任何配置（否则它就成了
第二个事实源）。

### 13.3.4 信任根边界（严重项 ③ 尾段要求写进报告）

**没有任何仓内工具能阻止一个不诚实的作者。** 上面三条的目标不是防作弊，而是让
篡改在**事后可被发现**：提交哈希 + 文件哈希 + 工具哈希 + 可重推的清单。
本节**不宣称「已不可骗」**。

## 13.4 追加 G：映射表 + 静态检查行（`:961`）+ `m50`（`:962`）

追加 G 要求「`:961` 那类**不可变异测的**要求必须有静态检查行」——计划自己就写着
「压缩比断言抓不到它」。落地形式是集合里的 `CHECKS`，**可执行**而非人工 grep：

```text
：变异测不到的要求必须有静态检查行）

- [PASS] sealer-不得使用-Go-库编码器
      文件=internal/tracearchive/sealer.go  正则=klauspost/compress/zstd|zstd\.NewWriter  期望=absent  实际=未命中
      理由=简报 task-6-brief.md:961：把 exec.CommandContext 换回 zstd.NewWriter 时，压缩比类断言**不会红**（本任务不测比值），因此计划自己就写明「必须另有一条人工检查」。这里把它做成可执行的静态检查行，而不是靠人工 grep。
```

`:962`（忽略 `cmd.Run()` 的 error）经映射表暴露为**唯一一条「计划明确要求、集合里
却没有对应条目」**的要求。它**不是覆盖缺口**（既有用例能杀死它），缺口在**集合**：
补 `m50_忽略cmd.Run错误` 后转红，杀死用例 `TestSealFailsOnNonZeroExit` +
`TestSealReportsMidRunCancellationAsCanceled`（`round3-mutation-table.txt` 表行 m50），
与简报 `task-6-brief.md:962` 点名的两个用例一致。

**完整映射表**（工具 `--mapping`，有缺口即整轮失败）：

| 计划要求 | 变异集合条目 | 静态检查 | 说明 |
|---|---|---|---|
| task-6-brief.md:961 不得把 exec.CommandContext 换回 zstd.NewWriter（压缩比断言抓不到） | — | `sealer-不得使用-Go-库编码器` | 计划自己要求「另有一条人工检查」⇒ 本工具把它落地为静态检查行。 |
| task-6-brief.md:962 忽略 cmd.Run() 的 error（当成成功） | `m50_忽略cmd.Run错误` | — | 映射表首次生成时这条**没有**对应条目（集合缺口）；本轮补 m50 关闭。 |
| task-6-brief.md:963 把 %w 改成 %v（ErrSealFailed / ErrPartExists / ErrRowCountMismatch） | `m05_ErrPartExists改v`, `m08_ErrRowCountMismatch改v`, `m12_ErrSealFailed改v` | — | 三条哨兵各一条；errors.Is/As 断言必须转红。 |
| task-6-brief.md:964 颠倒「先跑 CLI 再写 trailer/footer」的顺序 | `m17_trailer先于帧` | — | 把 trailer/footer 写到帧之前 ⇒ 被 CLI 的 O_TRUNC 冲掉。 |
| task-6-brief.md:965 删掉 info.ModTime().After(cutoff) 判断 | `m33_删除mtime门槛` | — | TestListPendingSealsSkipsFreshFiles 必须转红。 |
| task-6-brief.md:966 去掉 .jsonl 后缀过滤 | `m31_删除jsonl后缀过滤` | — | TestListPendingSealsIgnoresIllegalNames 必须转红。 |
| task-6-fix3-items.md 重要 ⑥ 行数交叉校验缺「实际多于声明」方向（复审自设计变异 x1） | `m51_行数校验放宽为只拦少于` | — | 修复前该变异**判绿**（fixture 是「声明 3、实际 2」，2 < 3 仍成立）；新增「声明 1、实际 2」的用例后转红 ⇒ 缺口已闭合且可复核。 |
| task-6-fix3-items.md 重要 ④ 把 (a)/(b) 做成两次**独立**的搬家验证 | `m52_守卫搬到版本探测之后`, `m53_守卫搬到产物存在检查之后` | — | relocA ⇒ (a)(b) 两条用例各死在自己的判别断言；relocB ⇒ **只有 (b) 红、(a) 绿**。「两次搬家验证不是两次独立验证」由此可复核（旧用例两处都红在同一行）。 |

映射表另外新增两条（重要 ④/⑥），把「本轮修法」也纳入可审计范围：

- `task-6-fix3-items.md` 重要 ⑥ → `m51_行数校验放宽为只拦少于`
- `task-6-fix3-items.md` 重要 ④ → `m52_守卫搬到版本探测之后`、`m53_守卫搬到产物存在检查之后`

## 13.5 重要④ 两次**独立**的搬家验证（`m52`/`m53`）

复审者的实测：旧用例把 (a)(b) 塞在同一条里，为钉 (b) 先放了哨兵产物，于是**任何**
「把守卫往后搬」的变异都更早撞上哨兵 ⇒ 报 `ErrPartExists`（它不包裹 `ErrSealFailed`）
⇒ 用例先死在不包裹的那条断言上 ⇒ **钉 (a) 的「文案必须含『不一致』」断言从未执行**。

修法：拆成两条独立用例 —— (a) **不放**哨兵 + `ZstdBinary` 指向不存在的文件；
(b) **放**哨兵 + `ZstdBinary` 指向**可用**的假 zstd（版本探测通过，唯一变量就是守卫
相对 `os.Stat` 的位置）。两条的**判别断言都放在最前**，避免被后面的通用断言遮蔽。

落点（`文件:行号`，绑定提交 `7bf7d112`）：

- (a) `tests/tracearchive/sealer_test.go:675`，判别断言 `:698`
- (b) `tests/tracearchive/sealer_test.go:713`，判别断言 `:745`
- 被钉的守卫 `internal/tracearchive/sealer.go:147-151`；`os.Stat` `:157`；
  `verifyZstdVersion` 调用 `:183`

**实测**（原始输出 `task-6-evidence/round3-two-relocations.txt`，`SIDE_EXIT=0`）：

```text
### 变异侧 m52_守卫搬到版本探测之后 (relocA) ⇒ 红 [names=34 ran=34 diff=0 PASS=32 SKIP=0 FAIL=2]
    杀死: TestSealRejectsSpoolPathNotMatchingRefBeforePartExistsCheck, TestSealRejectsSpoolPathNotMatchingRefBeforeVersionProbe
### 变异侧 m53_守卫搬到产物存在检查之后 (relocB) ⇒ 红 [names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1]
    杀死: TestSealRejectsSpoolPathNotMatchingRefBeforePartExistsCheck
```

两条用例各自的失败点（取自同一归档）：

```text
sealer_test.go:698: (a) 路径一致性判定必须先于 zstd 版本探测：错误文案必须点明「不一致」，实际: tracearchive: 封块失败: 探测 zstd 版本失败，找不到可执行文件 "/var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestSealRejectsSpoolPath
sealer_test.go:745: (b) 该判定必须先于产物存在性检查（否则报的是产物已存在），实际 tracearchive: 产物已存在，拒绝覆盖: /var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestSealRejectsSpoolPathNotMatchingRefBeforePartExi
sealer_test.go:745: (b) 该判定必须先于产物存在性检查（否则报的是产物已存在），实际 tracearchive: 产物已存在，拒绝覆盖: /var/folders/vm/m1785wd52_182p2q1z7yl77c0000gn/T/TestSealRejectsSpoolPathNotMatchingRefBeforePartExi
```

⇒ `relocA` 让 (a) 死在 `:698`、(b) 死在 `:745`；
`relocB` **只让 (b) 死**，(a) **绿**。两次搬家因此是**两次独立验证** ——
这正是复审者说旧实现做不到的事（旧实现两次都红在同一行）。

## 13.6 重要⑤ 全层级 SKIP + 「判定，不是打印」

两处都修，且都在**判定路径**上（不是只打印）：

1. **全层级 SKIP**：断言 3 统计 `^\s*--- SKIP` **与** `^--- SKIP`（`trace_mutate.py:206-209`），
   白名单在集合里声明（Task 6 为空）。子测试 SKIP 因此可判无效。
   自检 D 专门合成「34 条顶层全 PASS + 一条缩进的 `--- SKIP: TestX/子用例`」，
   旧正则漏判、新实现判无效（`task-6-evidence/round3-selftest.txt`）。
2. **判定与打印分离**：`verify()` 是**唯一**判据，其返回值直接决定 `classify()` 的判定
   与整轮退出码；「名字清单为空（执行数==0）」与「`no tests to run`」是 `verify()` 开头的
   **先判**（`trace_mutate.py:175-189`），不是打印。自检 F 专门合成「清单为空」。

> 追加要求点名的 `three-assertions.sh` 属 **Task 7 的证据目录**，`git ls-files` = 0，
> 不在本任务的交付面（本轮明确不碰 Task 7 的任何文件）。这个缺陷的性质已由**共享工具**
> 覆盖：同一个 `verify()` 同时服务 Task 6 与后续任务；`--side` 下证据无效也非零退出
> （轻微⑦，`trace_mutate.py:619-620`）。

自检原始输出（7 组反例 + 2 组环境反例，`SELFTEST_EXIT=0`）见
`task-6-evidence/round3-selftest.txt`；结论行：

```text
自检结论: 全部被抓住 ⇒ 检测有效（三断言 + 冻结清单 + 封闭环境都在生效）
名字清单条数 = 34
SELFTEST_EXIT=0
```

## 13.7 重要⑥ 「实际多于声明」fixture（`m51` 前后对照）

复审者自设计的 `x1`（`!=` → `<`）此前**判绿**：原 fixture 是「声明 3、实际 2」，
`2 < 3` 仍成立 ⇒ 只拦「少于声明」的放宽变异观察不到差异。

新增 `TestSealRejectsRowCountMismatchWhenSpoolHasMoreRows`
（`tests/tracearchive/sealer_test.go:130`，判别断言 `:141`）—— 「声明 1、实际 2」。

`task-6-evidence/m51-before-after.txt`（同一变异、同一实现，只换测试文件；
修复前测试文件取 `0c420547:tests/tracearchive/sealer_test.go`）：

```text
### m51 在修复前测试文件下 ⇒ 绿（零覆盖）  (PASS=32 FAIL=0)  杀死: —
### m51 在修复后测试文件下 ⇒ 红  (PASS=33 FAIL=1)  杀死: TestSealRejectsRowCountMismatchWhenSpoolHasMoreRows
```

⇒ 「修复前零覆盖 / 修复后转红」是**实测**，且这条变异已经进 git-tracked 集合，
第三方可原样重跑。

## 13.8 轻微⑦⑧⑨

- **⑦ `--side` 传播退出码**：`trace_mutate.py` 的 `--side` 分支统计证据无效的条数，
  非零即 `raise SystemExit(...)`（`trace_mutate.py:619-620`）。旧实现打印
  「证据无效！」却 `exit 0`，而 §12.4.6 的双侧证据正是该模式产出。
- **⑧ 摘要口径**：计数口径区分「**断言支撑的红**」与「**崩溃/超时/编译红**」
  （后者**没有**「用例执行 + 终判」证据），见 13.9 的原始输出。
  本轮口径为：**46 条断言支撑 + 3 条崩溃/超时 + 1 条零覆盖 + 1 条等价变异**。
- **⑨ 行号自指**：`sealer_test.go` 的两条搬运用例**不再写行号**，改为按「断言什么」自指；
  行号只在**报告**里给出并绑定提交哈希（13.5）。旧注释写「断言 628 行即红」，
  实测 `:628` 是 `func` 声明、`:625` 是注释 —— 把行号写进代码注释就是这一类失效的根源。
  另：追加 E 指出的陈旧「31 条」随旧脚本文档字符串一起消失（旧脚本已存档）；
  新工具**不再内嵌任何计数**，名字数来自清单、变异数来自集合，因此这类数字不会再陈旧。

## 13.9 全量变异表（51 条，`--only`/`--skip` 未使用 ⇒ 全集）

原始输出：`task-6-evidence/round3-mutation-table.txt`（sha256 `685200611f988ccd7ca3bb6ede0962aefdfe9dd9cfb4beee931364e76576140b`）。

```text
原始侧自证: OK names=34 ran=34 diff=0 PASS=34 SKIP=0 FAIL=0
```

| 变异 | 判定 | 自证（名字/执行/diff/PASS/SKIP/FAIL） | 杀死的用例 |
|---|---|---|---|
| m01_不调用配置的二进制 | 红 | names=34 ran=34 diff=0 PASS=29 SKIP=0 FAIL=5 | TestSealDefaultsZstdBinaryFromPath, TestSealFailsOnNonZeroExit, TestSealGoldenLayout, TestSealInvokesZstdCLIWithArgs, TestSealReportsMidRunCancellationAsCanceled |
| m02_退回Go库编码器 | 红 | names=34 ran=34 diff=0 PASS=28 SKIP=0 FAIL=6 | TestSealAcceptsEmptySpool, TestSealDefaultsZstdBinaryFromPath, TestSealFailsOnNonZeroExit, TestSealGoldenLayout, TestSealInvokesZstdCLIWithArgs, TestSealReportsMidRunCancellationAsCanceled |
| m03_删除产物存在检查 | 红 | names=34 ran=34 diff=0 PASS=31 SKIP=0 FAIL=3 | TestSealRefusesToOverwriteExistingArtifact, TestSealRejectsSpoolPathNotMatchingRefBeforePartExistsCheck, TestSealRejectsUnknownArtifactState |
| m04_删除无法判断分支 | 红 | names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1 | TestSealRejectsUnknownArtifactState |
| m05_ErrPartExists改v | 红 | names=34 ran=34 diff=0 PASS=32 SKIP=0 FAIL=2 | TestSealRefusesToOverwriteExistingArtifact, TestSealRejectsSpoolPathNotMatchingRefBeforePartExistsCheck |
| m06_删除Rows等于0的豁免 | 红 | names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1 | TestSealAcceptsRowsZeroFromDiscoveryPath |
| m07_放宽为只拦多于 | 红 | names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1 | TestSealRejectsRowCountMismatch |
| m08_ErrRowCountMismatch改v | 红 | names=34 ran=34 diff=0 PASS=32 SKIP=0 FAIL=2 | TestSealRejectsRowCountMismatch, TestSealRejectsRowCountMismatchWhenSpoolHasMoreRows |
| m09_删除残行守卫 | 红 | names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1 | TestSealRejectsTruncatedSpool |
| m10_删除zstd失败时的tmp清理 | 红 | names=34 ran=34 diff=0 PASS=32 SKIP=0 FAIL=2 | TestSealFailsOnNonZeroExit, TestSealReportsMidRunCancellationAsCanceled |
| m11_删除建目录 | 红 | names=34 ran=34 diff=0 PASS=22 SKIP=0 FAIL=12 | TestSealAcceptsCleanedEquivalentSpoolPath, TestSealAcceptsConfiguredZstdVersion, TestSealAcceptsEmptySpool, TestSealAcceptsRowsZeroFromDiscoveryPath, TestSealDefaultsZstdBinaryFromPath, TestSealFailsOnNonZeroExit, TestSealFooterPointsAtValidTrailer, TestSealGoldenLayout, TestSealInvokesZstdCLIWithArgs, TestSealIsAtomicOnCrash, TestSealProducesArtifactAndRemovesSpool, TestSealReportsMidRunCancellationAsCanceled |
| m12_ErrSealFailed改v | 红 | names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1 | TestSealFailsOnNonZeroExit |
| m13_删除ErrNotFound归一化 | 红 | names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1 | TestSealFailsWhenZstdMissing |
| m14_删除failBeforeRename钩子 | 红 | names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1 | TestSealIsAtomicOnCrash |
| m15_删除打开spool失败守卫 | **绿（零覆盖！）** | names=34 ran=34 diff=0 PASS=34 SKIP=0 FAIL=0 | — |
| m16_删除读spool失败守卫 | 红(超时/挂死) | —（挂死，三断言不适用） | 超时(用例未终判) |
| m17_trailer先于帧 | 红(崩溃/中止) | 集合不完整：未执行 10 条（TestListPendingSealsHandlesSpoolRootErrors, TestListPendingSealsIgnoresNonLayoutEntries, TestListPendingSealsRejectsMalformedDirNames …） | TestSealAcceptsRowsZeroFromDiscoveryPath, TestSealFooterPointsAtValidTrailer, TestSealGoldenLayout |
| m18_删除Root为空守卫 | 红 | names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1 | TestNewSealerValidatesOptions |
| m19_删除ZstdBinary默认值 | 红 | names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1 | TestSealDefaultsZstdBinaryFromPath |
| m20_删除ZstdLevel默认值 | 红 | names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1 | TestSealInvokesZstdCLIWithArgs |
| m21_删除ZstdWindowBytes默认值 | 红(崩溃/中止) | 集合不完整：未执行 31 条（TestListPendingSealsHandlesSpoolRootErrors, TestListPendingSealsIgnoresIllegalNames, TestListPendingSealsIgnoresNonLayoutEntries …） | TestSealIsAtomicOnCrash, TestSealProducesArtifactAndRemovesSpool, TestSealRefusesToOverwriteExistingArtifact |
| m23_删除窗口2的幂校验 | 红 | names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1 | TestNewSealerValidatesOptions |
| m24_删除ctx取消分支 | 红 | names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1 | TestSealReportsMidRunCancellationAsCanceled |
| m25_删除根目录不存在分支 | 红 | names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1 | TestListPendingSealsHandlesSpoolRootErrors |
| m26_删除根目录读失败上抛 | 红 | names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1 | TestListPendingSealsHandlesSpoolRootErrors |
| m27_删除dt前缀过滤 | 红 | names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1 | TestListPendingSealsIgnoresNonLayoutEntries |
| m28_删除dt的IsDir过滤 | 红 | names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1 | TestListPendingSealsIgnoresNonLayoutEntries |
| m29_删除inst前缀过滤 | 红 | names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1 | TestListPendingSealsIgnoresNonLayoutEntries |
| m30_删除inst的IsDir过滤 | 红 | names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1 | TestListPendingSealsIgnoresNonLayoutEntries |
| m31_删除jsonl后缀过滤 | 红 | names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1 | TestListPendingSealsIgnoresIllegalNames |
| m32_删除parsePartSeq过滤 | 红 | names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1 | TestListPendingSealsIgnoresIllegalNames |
| m33_删除mtime门槛 | 红 | names=34 ran=34 diff=0 PASS=32 SKIP=0 FAIL=2 | TestListPendingSealsSkipsFreshFiles, TestSealerListPendingSealsUsesConfiguredMinAge |
| m34_删除sort顺序 | **绿（零覆盖！）** | names=34 ran=34 diff=0 PASS=34 SKIP=0 FAIL=0 | — |
| m35_删除minAge默认值 | 红 | names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1 | TestSealerListPendingSealsUsesConfiguredMinAge |
| m36_minAge默认值退回15分钟 | 红 | names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1 | TestSealerListPendingSealsUsesConfiguredMinAge |
| m37_删除版本探测调用 | 红 | names=34 ran=34 diff=0 PASS=31 SKIP=0 FAIL=3 | TestSealFailsWhenZstdMissing, TestSealRejectsMismatchedZstdVersion, TestSealRejectsUnparsableZstdVersion |
| m38_版本比对恒通过 | 红 | names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1 | TestSealRejectsMismatchedZstdVersion |
| m39_无法解析时放行 | 红 | names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1 | TestSealRejectsUnparsableZstdVersion |
| m40_期望版本写死默认值 | 红 | names=34 ran=34 diff=0 PASS=29 SKIP=0 FAIL=5 | TestSealAcceptsConfiguredZstdVersion, TestSealAcceptsRowsZeroFromDiscoveryPath, TestSealFooterPointsAtValidTrailer, TestSealIsAtomicOnCrash, TestSealProducesArtifactAndRemovesSpool |
| m41_忽略探测失败 | 红 | names=34 ran=34 diff=0 PASS=32 SKIP=0 FAIL=2 | TestSealFailsWhenZstdMissing, TestSealRejectsFailedVersionProbe |
| m42_删除dt目录名校验 | 红 | names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1 | TestListPendingSealsRejectsMalformedDirNames |
| m43_删除inst目录名校验 | 红 | names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1 | TestListPendingSealsRejectsMalformedDirNames |
| m45_解析放宽为不校验三段 | 红 | names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1 | TestSealRejectsUnparsableZstdVersion |
| m46_删除版本号数字校验 | 红 | names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1 | TestSealRejectsUnparsableZstdVersion |
| m47_版本不符信息省略期望值 | 红 | names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1 | TestSealRejectsMismatchedZstdVersion |
| m48_删除SpoolPath与Ref一致性校验 | 红 | names=34 ran=34 diff=0 PASS=32 SKIP=0 FAIL=2 | TestSealRejectsSpoolPathNotMatchingRefBeforePartExistsCheck, TestSealRejectsSpoolPathNotMatchingRefBeforeVersionProbe |
| m49_删除路径Clean | 红 | names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1 | TestSealAcceptsCleanedEquivalentSpoolPath |
| m50_忽略cmd.Run错误 | 红 | names=34 ran=34 diff=0 PASS=32 SKIP=0 FAIL=2 | TestSealFailsOnNonZeroExit, TestSealReportsMidRunCancellationAsCanceled |
| m51_行数校验放宽为只拦少于 | 红 | names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1 | TestSealRejectsRowCountMismatchWhenSpoolHasMoreRows |
| m52_守卫搬到版本探测之后 | 红 | names=34 ran=34 diff=0 PASS=32 SKIP=0 FAIL=2 | TestSealRejectsSpoolPathNotMatchingRefBeforePartExistsCheck, TestSealRejectsSpoolPathNotMatchingRefBeforeVersionProbe |
| m53_守卫搬到产物存在检查之后 | 红 | names=34 ran=34 diff=0 PASS=33 SKIP=0 FAIL=1 | TestSealRejectsSpoolPathNotMatchingRefBeforePartExistsCheck |

### 计数口径（原始输出）

```text
变异总数                 = 51
集合里的变异总数          = 51（本次运行 == 全集 ✓）
断言支撑的红             = 46 条（清单全部执行 + 全部终判 + SKIP=0）
崩溃/超时/编译红         = 3 条（**没有**「用例执行+终判」证据，但绝不可能是绿）：m16_删除读spool失败守卫(红(超时/挂死)), m17_trailer先于帧(红(崩溃/中止)), m21_删除ZstdWindowBytes默认值(红(崩溃/中止))
允许的绿（集合里登记了理由） = 2 条
未登记的绿（必须为 0）     = 0 条
证据无效（必须为 0）       = 0 条

合计: 51 = 46 断言支撑 + 3 崩溃/超时/编译 + 2 允许的绿
EXIT=0
```

### 允许的绿：**1 条零覆盖 + 1 条等价变异**（分类各自给依据，未登记的绿 ⇒ 整轮失败）

> **口径更正（控制者修复轮 3 终审裁决）**：这两条此前都写「零覆盖」，**分类是错的**。
> 等价变异与覆盖缺口的含义完全不同，混为一谈会污染这份集合（它正是信任根）：
>
> - **零覆盖** = 某行为**存在**但**没有用例约束**它 ⇒ **这是缺陷**（需补用例或登记理由）
> - **等价变异** = 该行代码在框架/标准库**契约**下**冗余** ⇒ **这是关于代码本身的发现**，不是测试缺陷
>
> 因此集合里两条绿现在都带 `[零覆盖]` / `[等价变异]` **分类标签**
> （`tools/trace-mutate/sets/task-6.py` 的 `EXPECTED_GREEN`），工具会把标签逐字打印出来。
> **判定本身没有变**（两条都是绿），所以**不需要重跑 51 条**。
>
> **这是一次「退化后的更正」**：修复轮 2 的报告 §12.14 第 3 条本来就写着
> 「`m15` 现在是**唯一**的真零覆盖条目（`m34` 是等价变换）」—— 也就是说
> **R2 的分类是对的**，是修复轮 3 的集合文件把 `m34` 退化成「零覆盖」并写进了信任根。
> 这也说明「分类必须进集合、不能只在报告里」这条要求是对的：只写在报告里的
> 分类无法约束下一次编辑。

#### `m15_删除打开spool失败守卫` —— **[零覆盖]（真零覆盖）**，逐字理由

> `[零覆盖] 测试不可达的纵深防御：把 os.Open 的 error 丢掉后，空句柄在读时返回 ErrInvalid，而 TestSealFailsWhenSpoolMissing 在 scanSpoolOffsets 的打开阶段就以 os.ErrNotExist 失败 —— 早于 runZstd 的这次打开。守卫只在「扫描与打开之间 spool 被删」的 TOCTOU 窗口内可达，现有用例无法构造该窗口。sealer.go:392-399 已声明它「测试不可达」并说明保留理由。`

**依据（为什么是真零覆盖，而非等价变异）**：丢掉 `os.Open` 的 error 之后，
**行为确实变了**（空句柄读时返回 `ErrInvalid`，而不是可归因的 `os.ErrNotExist`），
只是**现有用例走不到那里** —— 这正是「行为存在但无用例约束」的定义，是**测试缺陷**。
落点：`internal/tracearchive/sealer.go:400`（被丢掉 error 的 `os.Open`）、
`:392-399`（「测试不可达」的声明与保留理由）。**保留**：纵深防御，删掉它会让未来重构
失去一道响亮失败。

#### `m34_删除sort顺序` —— **[等价变异]（等价，**不是**零覆盖）**，逐字理由

> `[等价变异] 【依据：标准库契约，可逐字复核】os.ReadDir 的文档写着「ReadDir reads the named directory, returning all its directory entries sorted by filename.」（$GOROOT/src/os/dir.go:109-110），其实现就是 slices.SortFunc(dirs, func(a,b DirEntry) int { return bytealg.CompareString(a.Name(), b.Name()) })（同文件 :114-126）⇒ 本函数三层嵌套读目录（dt → inst → part-NNNNNN.jsonl）拿到的顺序已经是 sort.Slice 的比较键序：dt=/inst= 按文件名即字典序，part 名由 ref.go:83-84 强制「恰好 6 位」⇒ 零填充 ⇒ 文件名字典序 == 数值序。因此显式 sort.Slice 冗余，删掉它不存在可观测差异。【不是覆盖缺口】「杀不死」的原因不是没测，而是无可观测差异 —— 这是关于代码本身的发现，与 m15 那种「行为存在但无用例约束」的测试缺陷不同类。登记为等价变异；保留显式排序是【有意把顺序从 ReadDir 的实现细节升格为本函数的契约】的选择（建议保留）。待决见本文件末尾 PENDING。`

**依据为什么不必靠实验**：`os.ReadDir` 的排序是**标准库文档明文契约**，
并且可直接在 `$GOROOT/src/os/dir.go` 读到实现（:114-126 的 `slices.SortFunc`）。
零填充那一半则在 `internal/tracearchive/ref.go:83-84`（`len(digits) != 6` ⇒ 拒绝）——
两处都是**可逐字复核的代码/文档**，不是我的实测断言。

#### 顺带登记的发现（**待决项，本轮不补用例**）

**发现**：`(*Sealer).ListPendingSeals` 末尾的 `sort.Slice(dt,inst,part)` 在 `os.ReadDir`
的契约下是**冗余的**。

- 保留它（控制者倾向、我也倾向）是**有意显式化不变量**：一旦将来数据源换成
  `(*os.File).ReadDir`（**不排序**）或注入排序器，显式排序就是唯一的保证。
- 但既然是冗余的，**一个断言返回顺序的用例会让它不再冗余**（`m34` 就会变成真能杀死）。
- **登记位置**：`tools/trace-mutate/sets/task-6.py` 的 `PENDING`
  （id `finding-1-显式排序在-os.ReadDir-契约下冗余`），owner = **R11 / 终审**，
  `this_round = 不补用例`。放在**被跟踪的集合**里而不是只写在 gitignored 的报告里 ——
  这是关于代码本身的发现，删掉它会留下 `git diff` 痕迹。

> **归档口径说明（必须明说，否则会被当成归档/报告冲突）**：
> `task-6-evidence/round3-mutation-table.txt` 生成于**分类更正之前**，
> 它里面 `m34` 的理由文本仍写着「登记为已知的零覆盖」、`m15` 也还没有 `[零覆盖]` 标签。
> **判定（两条都是绿）完全一致**，变的只是分类口径与理由文本。
> 分类与理由的权威是 **git-tracked 的 `sets/task-6.py`**（信任根）；归档是上一版口径的证据。
> 本节保留**逐字**引用，就是为了让两者可以并排比对。
>
> 现行集合（分类更正后）sha256 = `81070df4068a96b7dfa11fccbaaea0a45cd704f7e8cfd5b4a6e49a3d4a35809d`
> ＝ `6f652f3f`；归档里打印的是 `6d20f5d6`（更正前）。
> **判定未变**，所以权威表无需重跑；变的只是 `EXPECTED_GREEN` 的理由文本与分类标签
> （以及新增的 `PENDING`）。

## 13.10 三断言 + SKIP==0 + 执行数>0（原始输出）

```text
### HEAD = 7bf7d112d53690a0942abafe5f9d80ee240851f5
### git status --porcelain = []

### gofmt -l internal/tracearchive tests/tracearchive
(以上无输出 = 干净)

### go vet ./internal/tracearchive/ ./tests/tracearchive/
vet: OK

### go build ./internal/... ./cmd/...
build: OK

### go test ./internal/tracearchive/ ./tests/tracearchive/ -count=1
ok  	github.com/example/llm-gateway/internal/tracearchive	3.533s
ok  	github.com/example/llm-gateway/tests/tracearchive	2.547s
exit=0
```

- 断言 1：`names=34 ran=34 diff=0` ⇒ 执行集合 == 清单
- 断言 2：`PASS=34` == 34
- 断言 3：`SKIP=0`
- 执行数 34 > 0（清单为空会先判无效）

## 13.11 偏离与残余不确定（诚实清单）

**本轮新增偏离**

| # | 偏离 | 依据 |
|---|---|---|
| 32 | 变异 47 → 51（新增 `m50`–`m53`） | 追加 G（映射表暴露 `:962` 无对应条目）+ 让 ④/⑥ 的修法可重跑 |
| 33 | 顶层用例 32 → 34（①拆成 ①a/①b 两条 + ⑥新增一条） | 重要 ④、⑥；清单随之重推并写入 `names.txt` |
| 34 | 变异工具从 `.superpowers/` 迁到 `tools/trace-mutate/`，原路径改为转发 shim | 严重 ③；追加 A/F（工具与集合必须 git-tracked） |
| 35 | 工具引入了旧脚本没有的东西：静态检查行、映射表、登记绿、集合/清单推导校验、子集自标识 | 追加 G；以及「判定不是打印」 |
| 36 | `sealer_test.go` 的搬运用例不再在注释里写行号 | 轻微 ⑨ 的根因就是注释里的行号会陈旧 |

**残余不确定 / 需要你裁决**

1. **工具仍不能防一个不诚实的作者**（13.3.4）。它能做的是让篡改事后可见。
   具体残余：作者仍可以**改集合**（会 `git diff` 可见）、**删清单里的名字**
   （推导比对会抓住）、**用 `--only` 只跑子集**（现在会在计数里自标识
   「!!! 这是子集，不能作为权威表」）。**新的残余**：推导命令本身写在集合里，
   一个有权限改集合的人也能改推导命令；不过那不是「悄悄丢变异」，
   而是「显式改判据」，会在 `git diff` 里露出。
2. `m16`/`m17`/`m21` 三条仍是「崩溃/超时」意义上的红，**没有**「用例执行 + 终判」证据
   （旧摘要把它们混在「45 红」里，轻微⑧已修）：`m16` 挂死触发 `-timeout 30s`；
   `m17`/`m21` 让测试进程 `panic` 中止，同包后续用例不再执行。它们**绝不可能**被误判为绿，
   但它们杀死的不是「某条断言」，而是「整轮运行」。
3. `m15` 是**真零覆盖**（测试不可达的纵深防御，控制者已接受）；`m34` **不是零覆盖**，
   而是**等价变异**（分类已按终审裁决更正，见 13.9）—— 它是「关于代码本身的发现」：
   显式 `sort.Slice` 在 `os.ReadDir` 契约下冗余。已登记为待决项交 R11 / 终审。
4. 本轮的 `--side` 只对 `m52`/`m53` 做了完整双侧输出（因为 ④ 需要的是这两次搬家的原始对照）。其余 49 条只在权威表里给自证串；需要时可用 `--side --only <名字>` 重放。
   这是**证据粒度的选择**，不是缺口，但复审者若要求「全部 51 条双侧」，成本是一次
   `--side`（输出量约 51×(约 6KB)）。
5. `tools/trace-mutate/__pycache__/` 已加 `.gitignore`；shim 与证据目录仍在 gitignored
   的 `.superpowers/` 下，符合追加 F 的裁决，但**必须明说**：它们不在信任根内。
6. **顺带记录（非本轮，明确不静默丢弃）**：`-exec` 一类环境变量能整体替换测试输出，
   也说明 CI 里若有人放了 `GOFLAGS`，`go build` 之类的检查同样不可信。本分支的
   `.gitlab-ci.yml` 只跑 `go build`、**完全不跑 `go test`**（我未核实这句以外的 CI 内容，
   按题面记录）⇒ 变异证据是目前唯一的验证。**建议计划 4 至少补一次**
   `go test ./tests/tracearchive/ -count=1`，否则 8 个任务的变异证据没有任何独立复算。
   本轮**未**修改 `.gitlab-ci.yml`（不在交付面）。

## 13.12 归档索引（本节引用的全部原始输出）

本节所有归档都在 `.superpowers/.../task-6-evidence/` 下（**gitignored**，按追加 F 的裁决）——
即：它们**不在信任根内**，是「随手可得的方便产物」；信任根是 git-tracked 的
`tools/trace-mutate/sets/task-6.py` 与产生这些输出的工具哈希。

| 归档 | sha256（前 12） | 内容 |
|---|---|---|
| `task-6-evidence/round3-mutation-table.txt` | `685200611f98` | 权威变异表（51 条）+ 证据绑定头 + 静态检查 + 映射表 + 计数口径（HEAD 7bf7d112） |
| `task-6-evidence/round3-two-relocations.txt` | `9f2fdbe4d79a` | --side m52 m53：两侧完整原始输出（含 pristine 侧全量 verbose） |
| `task-6-evidence/round3-verify.txt` | `d297bc26b5ec` | gofmt / go vet / go build ./internal/... ./cmd/... / 两包 go test -count=1 全绿 |
| `task-6-evidence/round3-selftest.txt` | `d3a88d763d40` | --selftest：7 组反例（少跑/无匹配/全SKIP/子测试SKIP/缩清单/塞多余/空清单）+ 2 组环境反例 |
| `task-6-evidence/round3-refuse-dirty-tree.txt` | `0b026d4bbd90` | 脏树拒绝运行（未跟踪文件 ⇒ REFUSE_EXIT=1，未产出任何表） |
| `task-6-evidence/goflags-demo-1-honest-m15-green.txt` | `cdf69ce2c0f0` | 攻击第1步：m15（真实零覆盖）诚实运行 ⇒ 绿 |
| `task-6-evidence/goflags-demo-2-forged-red-m15.txt` | `2c960f09080c` | 攻击第2步：同一棵树 + GOFLAGS=-exec=forge-red ⇒ **伪造成红**（掩盖零覆盖缺口） |
| `task-6-evidence/goflags-demo-3-washed-green-m49.txt` | `ddcc401d1a81` | 攻击第3步：m49（真红）+ GOFLAGS=-exec=wash ⇒ 洗成绿 |
| `task-6-evidence/goflags-demo-4-tool-closed-env.txt` | `6476f8050694` | 反证：新工具下 GOFLAGS 被剔除、wrapper 调用 0 次、判定回到诚实值（TOOL_EXIT=0） |
| `task-6-evidence/m51-before-after.txt` | `5a0aefccab5b` | m51 前后对照：修复前测试文件 ⇒ 绿（零覆盖）；修复后 ⇒ 红（杀死新用例） |
| `task-6-evidence/names.txt` | `c27020f20e00` | 名字清单（34 条，可推导；sha256 c27020f2…） |
| `task-6-evidence/goflags-exec-forge-red.sh` | `c373de89ac99` | -exec 攻击 wrapper：把 PASS 伪造成 FAIL（用于实测「伪造任意判定」） |
| `task-6-evidence/goflags-exec-wash-red-green.sh` | `970b923c7ff1` | -exec 攻击 wrapper：把 FAIL 改写成 PASS 并 exit 0（复审者原始骗法） |
| `task-6-evidence/task-6-mutate.py.prior-to-round3` | `df78eaac4452` | 修复轮 3 之前的整份变异脚本（594 行）存档，供逐行 diff |

（sha256 由本节脚本当场计算，可用 `shasum -a 256` 复核。）

## 13.13 状态契约（≤15 行）

1. 交付文件（行数；`git diff --stat 0c420547..HEAD`）：`tools/trace-mutate/trace_mutate.py` 707、
   `tools/trace-mutate/sets/task-6.py` 482、`tools/trace-mutate/README.md` 93、`tools/trace-mutate/.gitignore` 2、
   `tests/tracearchive/sealer_test.go` 1420（+118/−24）。共 5 文件 +1368/−24。
2. 提交（无 amend、无 reset）：`d2670961` 工具+集合 → `1ca0a27a` 拆 (a)/(b) + ⑥ 用例 →
   `f9e12bca` m51–m53 + 映射 → `7bf7d112` 子集自标识 → `6f652f3f` 允许的绿分类更正。
   权威表绑定 `7bf7d112`（判定未变，分类更正见 13.9，`judge-exit=0` 未重跑）。
3. 变异计数（全集 51/51，`EXIT=0`）：**红 46**（断言支撑）+ **红 3**（崩溃/超时，无「执行+终判」证据）
   + **绿 2** = **1 零覆盖 + 1 等价变异**（分类与依据都在集合里）+ 无效 0 + 未登记绿 0。
4. 三断言：`names=34 ran=34 diff=0 PASS=34 SKIP=0 FAIL=0`（执行数 34 > 0）；两包 `go test -count=1` 全绿；
   `gofmt` / `go vet` / `go build ./internal/... ./cmd/...` 干净（**未**跑 `go build ./...`）。工作树结束时干净。
5. **本轮没做到 / 做得不好的地方（如实说）**：
   - `m15` 是**真零覆盖**（控制者已接受）。`m34` 的分类**此前被我写错了**：它是**等价变异**，
     不是零覆盖 —— 而且修复轮 2 的报告 §12.14 本来就写对了，是 R3 的集合文件把它退化了。
     两处分类已按终审裁决更正并带标签入集合；`sort.Slice` 冗余这件事已登记为待决项（R11 / 终审）。
   - `m16`/`m17`/`m21` 只有「崩溃/超时」证据：它们杀死的不是某条断言，而是整轮运行。口径已分开，
     但**这三条防线的「哪条用例会红」仍然说不出来**。
   - `--side` 双侧完整输出只给了 `m52`/`m53`（④ 需要那一对），其余 49 条只有自证串 —— 是证据粒度选择，
     不是缺口，但「全部 51 条双侧」需要我再跑一次 `--side`。
   - 工具**不能防一个不诚实的作者**，只能让篡改事后可见；推导命令本身也写在可改的集合里
     （改它会在 `git diff` 露出，但那是「显式改判据」而非「悄悄丢条目」）。
   - 证据、清单与 shim 都在 gitignored 的 `.superpowers/` 下（追加 F 允许），13.12 已明说它们**不在信任根内**。
