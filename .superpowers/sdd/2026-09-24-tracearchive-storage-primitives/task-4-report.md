# Task 4 报告：定位键与目录布局

**状态：** DONE（含 1 处简报笔误修正 + 1 处一行偏离，均见 §5，报告末尾附原始输出）
**提交：** `b56be60f` feat(tracearchive): 定位键、目录布局与错误分类
**基线：** `f6f4765f`（方案 C 文档修订，仅动文档）

---

## 1. 实现了什么

本任务是后续 Task 5/6/7 的**公共类型基座**：写入侧（Task 5 spool、Task 6 sealer）与读取侧（Task 7 点读）都依赖这里的 `Ref` / `Paths` / 错误哨兵。接口严格按简报签名实现，未做任何增删。

| 文件 | 内容 |
|------|------|
| `internal/tracearchive/ref.go`（新，87 行） | `Ref{DT,Inst,Part,Row}` + `Ref.String()`、`Paths{Root}`、`NewPaths`、`PartDir`、`SpoolDir`、`PartFile`、`SpoolFile`、`partName`、`parsePartSeq` |
| `internal/tracearchive/errors.go`（新，40 行） | 读取侧 `ErrPartNotFound` / `ErrBodyMissing` / `ErrNotArchived`；封块侧 `ErrPartExists` / `ErrRowCountMismatch` / `ErrSealFailed` |
| `tests/tracearchive/ref_test.go`（新，68 行） | **B 层** `package tracearchive_test`，仅经公开 API：`TestPathsLayout`、`TestPartNameZeroPadsSixDigits`、`TestPartDirAndSpoolDir`、`TestRefString` |
| `internal/tracearchive/ref_internal_test.go`（新，51 行） | **A 层** `package tracearchive`，唯一理由是 `parsePartSeq` 未导出：`TestParsePartSeq`、`TestParsePartSeqRejectsIllegalNames`（12 个子用例） |

目录布局（规格 §5.1）：

```
<root>/dt=<DT>/inst=<Inst>/part-<NNNNNN>.jsonl.zst
<root>/spool/dt=<DT>/inst=<Inst>/part-<NNNNNN>.jsonl
```

### 错误变量不重复声明（简报要求先确认）

已读 `index.go` / `footer.go` 确认：`ErrTrailerCorrupt`（index.go:17）、`ErrFooterCorrupt`（footer.go:22）已存在。`errors.go` **只新增**本任务的 6 个哨兵，未触碰既有两个；编译通过即为证据（重复声明会直接编译失败）。`errors.go` 顶部注释显式交叉引用了这两个既有哨兵，便于后续任务在一处找到全包错误集合。

---

## 2. 测试内容

### B 层 `tests/tracearchive/ref_test.go`（公开 API，后续任务 B 层文件的书写范例）

- `TestPathsLayout` — 目录布局全路径：`PartFile` 与 `SpoolFile` 各自的期望绝对路径（含 `spool/` 子树）。
- `TestPartNameZeroPadsSixDigits` — 块名补零位数：0→`part-000000`、1→`part-000001`、99999→`part-099999`。
- `TestPartDirAndSpoolDir`（自检补充）— 单独锁定两个目录方法，并断言 `PartDir != SpoolDir`（防止 spool 与产物混同导致 sealer 把 `.jsonl` 当产物）。
- `TestRefString`（自检补充）— 锁定日志/错误信息里的定位键可读形式 `2026-09-24/blue/part-000042#row=7`。

### A 层 `internal/tracearchive/ref_internal_test.go`（未导出 `parsePartSeq`）

- `TestParsePartSeq` — 3 个正例（`.jsonl.zst` 与 `.jsonl` 两种扩展名）。
- `TestParsePartSeqRejectsIllegalNames` — **12 个拒绝子用例**，每个标注它守住的分支。

---

## 3. TDD 证据

### 3.1 RED（先写测试，未实现）

命令（逐字照抄简报步骤 2）：

```
go test ./internal/tracearchive/ ./tests/tracearchive/ -run 'TestPathsLayout|TestPartNameZeroPadsSixDigits|TestParsePartSeq|TestParsePartSeqRejectsIllegalNames' -v
```

原始输出（tail）：

```
# github.com/example/llm-gateway/internal/tracearchive [github.com/example/llm-gateway/internal/tracearchive.test]
internal/tracearchive/ref_internal_test.go:15:14: undefined: parsePartSeq
internal/tracearchive/ref_internal_test.go:44:18: undefined: parsePartSeq
FAIL	github.com/example/llm-gateway/internal/tracearchive [build failed]
# github.com/example/llm-gateway/tests/tracearchive_test [github.com/example/llm-gateway/tests/tracearchive.test]
tests/tracearchive/ref_test.go:11:10: undefined: ta.NewPaths
tests/tracearchive/ref_test.go:12:12: undefined: ta.Ref
tests/tracearchive/ref_test.go:25:10: undefined: ta.NewPaths
tests/tracearchive/ref_test.go:31:38: undefined: ta.Ref
tests/tracearchive/ref_test.go:42:10: undefined: ta.NewPaths
tests/tracearchive/ref_test.go:61:12: undefined: ta.Ref
FAIL	github.com/example/llm-gateway/tests/tracearchive [build failed]
FAIL
EXIT=1
```

**与简报预期一致**：两个包都编译失败，B 层 `undefined: ta.NewPaths` / `ta.Ref`，A 层 `undefined: parsePartSeq`。这正是「符号尚不存在」的真实失败，不是断言写错。

### 3.2 GREEN（实现之后）

同一条命令，原始输出：

```
=== RUN   TestParsePartSeq
--- PASS: TestParsePartSeq (0.00s)
=== RUN   TestParsePartSeqRejectsIllegalNames
=== RUN   TestParsePartSeqRejectsIllegalNames/index.jsonl
=== RUN   TestParsePartSeqRejectsIllegalNames/000042.jsonl
=== RUN   TestParsePartSeqRejectsIllegalNames/part-.jsonl.zst
=== RUN   TestParsePartSeqRejectsIllegalNames/part-abc.jsonl.zst
=== RUN   TestParsePartSeqRejectsIllegalNames/part-00004a.jsonl
=== RUN   TestParsePartSeqRejectsIllegalNames/part-000042
=== RUN   TestParsePartSeqRejectsIllegalNames/part-1234567.jsonl
=== RUN   TestParsePartSeqRejectsIllegalNames/part-1234.jsonl
=== RUN   TestParsePartSeqRejectsIllegalNames/part-000042.jsonl.zst.tmp
=== RUN   TestParsePartSeqRejectsIllegalNames/part-000042.ZST
=== RUN   TestParsePartSeqRejectsIllegalNames/part--00042.jsonl
=== RUN   TestParsePartSeqRejectsIllegalNames/part-000042.jsonl.gz
--- PASS: TestParsePartSeqRejectsIllegalNames (0.00s)
    --- PASS: TestParsePartSeqRejectsIllegalNames/index.jsonl (0.00s)
    --- PASS: TestParsePartSeqRejectsIllegalNames/000042.jsonl (0.00s)
    --- PASS: TestParsePartSeqRejectsIllegalNames/part-.jsonl.zst (0.00s)
    --- PASS: TestParsePartSeqRejectsIllegalNames/part-abc.jsonl.zst (0.00s)
    --- PASS: TestParsePartSeqRejectsIllegalNames/part-00004a.jsonl (0.00s)
    --- PASS: TestParsePartSeqRejectsIllegalNames/part-000042 (0.00s)
    --- PASS: TestParsePartSeqRejectsIllegalNames/part-1234567.jsonl (0.00s)
    --- PASS: TestParsePartSeqRejectsIllegalNames/part-1234.jsonl (0.00s)
    --- PASS: TestParsePartSeqRejectsIllegalNames/part-000042.jsonl.zst.tmp (0.00s)
    --- PASS: TestParsePartSeqRejectsIllegalNames/part-000042.ZST (0.00s)
    --- PASS: TestParsePartSeqRejectsIllegalNames/part--00042.jsonl (0.00s)
    --- PASS: TestParsePartSeqRejectsIllegalNames/part-000042.jsonl.gz (0.00s)
PASS
ok  	github.com/example/llm-gateway/internal/tracearchive	0.005s
=== RUN   TestPathsLayout
--- PASS: TestPathsLayout (0.00s)
=== RUN   TestPartNameZeroPadsSixDigits
--- PASS: TestPartNameZeroPadsSixDigits (0.00s)
PASS
ok  	github.com/example/llm-gateway/tests/tracearchive	0.005s
EXIT=0
```

注：RED→GREEN 之间有过一次**真实失败**，见 §5.1（简报测试期望值笔误），因此上面的 GREEN 是修正测试期望后的结果。

### 3.3 全量相关测试 + 构建

```
$ go test ./internal/tracearchive/ ./tests/tracearchive/ -count=1
ok  	github.com/example/llm-gateway/internal/tracearchive	3.479s
ok  	github.com/example/llm-gateway/tests/tracearchive	0.005s
EXIT=0

$ go test ./tests/tracearchive/ -run 'TestPathsLayout|TestPartNameZeroPadsSixDigits|TestPartDirAndSpoolDir|TestRefString' -count=1 -v
=== RUN   TestPathsLayout
--- PASS: TestPathsLayout (0.00s)
=== RUN   TestPartNameZeroPadsSixDigits
--- PASS: TestPartNameZeroPadsSixDigits (0.00s)
=== RUN   TestPartDirAndSpoolDir
--- PASS: TestPartDirAndSpoolDir (0.00s)
=== RUN   TestRefString
--- PASS: TestRefString (0.00s)
PASS
ok  	github.com/example/llm-gateway/tests/tracearchive	0.005s
EXIT=0

$ go build ./internal/... ./cmd/...
EXIT=0

$ gofmt -l internal/tracearchive/ tests/tracearchive/
(空输出 = 全部已格式化)

$ go vet ./internal/tracearchive/ ./tests/tracearchive/
vet OK
```

### 3.4 `make test` 全量（提交前一次）—— 3 个**预存在**失败，与本次改动无关

```
$ make test          # = go test ./tests/... -count=1
...
--- FAIL: TestPutPendingScheduleRequiresExplicitReplaceAndPreservesAuditRow (0.00s)
    budget_tier_repo_test.go:108: table budget_limit_schedules has no column named schedule_operation
FAIL	github.com/example/llm-gateway/tests/controlplane/repository	0.047s
FAIL	github.com/example/llm-gateway/tests/gateway/handler [build failed]
FAIL	github.com/example/llm-gateway/tests/gateway/orchestrator [build failed]
...
ok  	github.com/example/llm-gateway/tests/tracearchive	0.019s
FAIL
make: *** [test] Error 1
EXIT=2
```

**已用「把我的 4 个文件移开后重跑」证明是预存在失败**（我的文件全是新增，与这些包无依赖）：

```
$ mv tests/tracearchive /tmp/t4hide/tests_tracearchive
$ mv internal/tracearchive/{ref.go,errors.go,ref_internal_test.go} /tmp/t4hide/
$ go test ./tests/gateway/handler/ ./tests/gateway/orchestrator/ ./tests/controlplane/repository/ -count=1
FAIL	github.com/example/llm-gateway/tests/gateway/handler [build failed]
FAIL	github.com/example/llm-gateway/tests/gateway/orchestrator [build failed]
--- FAIL: TestPutPendingScheduleRequiresExplicitReplaceAndPreservesAuditRow (0.00s)
FAIL	github.com/example/llm-gateway/tests/controlplane/repository	0.026s
```

失败根因均与本任务无关：`tests/gateway/{handler,orchestrator}` 是 `*fakeScheduler does not implement orchestrator.Scheduler (wrong type for method UpdateSticky)` 的接口漂移；`controlplane/repository` 是缺 `schedule_operation` 列。`tests/tracearchive` 在该全量运行中 **ok**。

---

## 4. 自检：逐个守卫的「删掉即转红」验证（变异测试）

本项目前两个任务栽过「写了守卫但零覆盖，删掉后测试全绿」。本次对 `parsePartSeq` 的每条拒绝分支做**变异删除**并重跑两个包的全部测试，判据是「变异体是否被杀死」：

```
M1 删除 part- 前缀检查     killed by TestParsePartSeqRejectsIllegalNames
M2 删除 .jsonl.zst 扩展名分支 killed by TestParsePartSeq
M3 删除 .jsonl 扩展名分支   killed by TestParsePartSeq
M4 删除 6 位长度检查        killed by TestParsePartSeqRejectsIllegalNames
M5 删除 Atoi 错误检查       killed by TestParsePartSeqRejectsIllegalNames
M6 删除 n<0 检查            killed by TestParsePartSeqRejectsIllegalNames
M7 扩展名改为取首个点前数字  killed by TestParsePartSeqRejectsIllegalNames
M8 删除扩展名 default 分支  killed by TestParsePartSeqRejectsIllegalNames
```

8/8 全部被杀死。其中 **M1/M5/M8 是靠自检补充的 3 个子用例才被杀死的**，简报原有的 9 个子用例对它们是**等价变异体（删除后测试仍全绿）**：

| 缺口 | 简报原有用例为何挡不住 | 补充的用例 |
|------|------------------------|-----------|
| 前缀检查（M1） | `index.jsonl` 去掉前缀检查后 `TrimPrefix` 无操作，`digits="index"` 长度 5≠6，被长度检查兜住 → 仍绿 | `000042.jsonl`（无前缀但数字段合法）：去掉前缀检查后会被解析成 42，**只有前缀检查能挡住** |
| Atoi 错误检查（M5） | `part-abc.jsonl.zst` 的数字段长度 3≠6，长度检查先挡下，Atoi 分支根本没被走到 | `part-00004a.jsonl`（恰好 6 位但含非数字）：Atoi 失败，**只有 err 检查能挡住** |
| 扩展名 default（M8） | `.tmp`/`.ZST`/`.gz` 的 remainder 长度都≠6，长度检查兜住 | `part-000042`（无扩展名）**+ 一行实现调整**（见 §5.2）：删掉 default 后 `digits` 为整段 rest，会被误当合法块名 |
| `part-abc.jsonl.zst` 的注释 | 原注释写它覆盖「Atoi 失败」，实际被长度检查挡下 —— 属**假覆盖**（注释与事实不符），已改写为「长度检查就挡下了」 | — |

补充用例后，12 个拒绝子用例各自标注「它守住哪个分支 / 哪些分支由别的用例守」，避免再出现「凑一个非法名」式假覆盖。

---

## 5. 与简报的两处偏离（均已修正/说明）

### 5.1 简报测试期望值笔误：`99999` 的补零结果

简报 `TestPartNameZeroPadsSixDigits` 给出：

```go
99999: "part-009999.jsonl.zst",
```

`99999` 是 5 位，`%06d` 补零到 6 位应为 `099999`（`009999` 是 9999 的补零结果）。**简报自相矛盾**：其「布局」段写的是 `<NNNNNN>`（6 位），`partName` 用 `%06d`，`TestPathsLayout` 期望 `part-000042`，且 `parsePartSeq` 要求数字段**恰好 6 位**（否则无法解析自产文件名）。RED→GREEN 之间该用例确实**真实失败**：

```
=== RUN   TestPartNameZeroPadsSixDigits
    ref_test.go:33: part=99999: want part-009999.jsonl.zst, got part-099999.jsonl.zst
--- FAIL: TestPartNameZeroPadsSixDigits (0.00s)
FAIL	github.com/example/llm-gateway/tests/tracearchive	0.005s
EXIT=1
```

**处置：修正测试期望为 `part-099999.jsonl.zst`**（实现不动，因其满足其余 3 处一致性约束），并在测试内加注释说明 99999 与 009999 的位数关系。请后续任务/评审知悉这条简报笔误。

### 5.2 一行实现偏离：`var digits string` → `digits := rest`

原因见 §4 的 M8：简报原写法下 `default: return 0, false` 是**等价变异体**（删掉后 `digits` 保持空串，仍被长度检查拒绝）——即一条「无法被任何测试区分」的守卫。为让该守卫真正可被区分，将其改为：

```go
// digits 以 rest 起手（而非空串）：没被认可扩展名裁剪掉的 remainder
// 一定不是数字段，default 是它唯一的护栏。
digits := rest
```

**行为完全不变**：default 命中时直接 return，`digits` 初值不被使用；对**任意**输入，两种写法的返回值逐一相同（default 未命中时 `digits` 一律被某个 case 重新赋值）。仅改变「删除 default 是否可被测试发现」。签名与语义零变化，不影响 Task 5/6/7。

### 5.3 超出简报的 3 个测试（均为公开 API/边界行为，非过度构建）

- `TestPartDirAndSpoolDir`：简报只在文件全路径层面隐含目录布局；单独断言可让失败信息直接指向出错的目录层级，并锁定 `PartDir != SpoolDir`。
- `TestRefString`：`Ref.String()` 是简报实现中的导出方法但无任何用例，锁定其格式避免日志格式静默漂移。
- 拒绝表中补 3 个子用例：见 §4（覆盖缺口修复，非新增功能）。

生产代码除 §5.2 一行外**未做任何增减**。

---

## 6. 变更文件

```
b56be60f feat(tracearchive): 定位键、目录布局与错误分类
 internal/tracearchive/errors.go            | 40 ++++++++++++++
 internal/tracearchive/ref.go               | 87 ++++++++++++++++++++++++++++++
 internal/tracearchive/ref_internal_test.go | 51 ++++++++++++++++++
 tests/tracearchive/ref_test.go             | 68 ++++++++++++++++++++++++++++++
 4 files changed, 246 insertions(+)
```

纯新增，未修改任何既有文件（`git show --stat` 4 个 A 状态）。

## 7. 问题与顾虑

1. **§5.1 简报笔误**已修正并在此显式记录；若评审认为应由控制器改简报，请以本报告为准同步。
2. **§5.2 一行偏离**是为满足「守卫必须可被测试区分」的硬要求，行为等价性已在报告中论证。
3. `parsePartSeq` 目前**只被测试调用**（Task 6 才会在生产路径使用）；`go vet`/`build` 无告警（Go 不对未使用的函数报错），符合计划预期。
4. 环境提示的 `test/bedrock_model_id_check.go:98` 预存在失败**未触碰**；`make test` 的另 3 个预存在失败亦未触碰（§3.4 已证明与我无关）。

---
---

# 修复轮次 1/5 报告

**状态：** 已修复 3 条要修项 + 1 条可选项（采取加注释方案）
**新提交：** `7f5eda3c` fix(tracearchive): 逐位校验数字段，拒绝 Atoi 会吞下的前导符号（未 amend，`b56be60f` 保留）
**基线：** 评审时的 `3709bec8`（控制器另行提交 `fix(plan)`，把规划文档里 `99999 → part-009999` 修正为 `part-099999`，与我 §5.1 的判断一致）

## F1【要修 1】数字段未完全拒绝非数字：`Atoi` 吞下前导 `+`

**问题确认：** 成立且可复现。`strconv.Atoi("+00042")` 返回 42，故 `part-+00042.jsonl` 与真实的 `part-000042.jsonl` 撞同一块序号——写指针以目录内容为权威来源（规格 §7.3）时，这种撞号会让「下一块序号」判定错误。

**修法（`internal/tracearchive/ref.go`）：** 把 `strconv.Atoi` 换成**就地逐位校验 + 累加**：

```go
	if len(digits) != 6 {
		return 0, false
	}
	// 逐位解析并校验，而不用 strconv.Atoi：Atoi 接受前导 '+'（"+00042" → 42），
	// 于是 part-+00042.jsonl 会与真实的 part-000042 撞上同一序号，违背
	// 「写指针以目录内容为权威来源」所要求的严格性（规格 §7.3）。
	// 逐位校验同时覆盖 Atoi 的错误分支与负号分支，不留不可达的守卫。
	n := 0
	for i := 0; i < len(digits); i++ {
		c := digits[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
```

**为什么不是「保留 Atoi，前面加一道逐位校验」：** 逐位校验一旦存在，`Atoi` 的错误分支与 `n < 0` 分支就**永远不可达**（6 位纯数字不可能解析失败也不可能为负），那两条守卫会退化成我在 §4 里刚论证过的「等价变异体 = 无法被任何测试区分」。因此让逐位循环**同时承担校验与求值**：分支数更少、无死代码、`strconv` 依赖一并移除。行为对合法输入完全不变（`part-000042.jsonl.zst` → 42 等正例全部保持）。

**新增用例（`ref_internal_test.go` 拒绝表）：**

```go
{"part-+00042.jsonl", "前导加号 —— strconv.Atoi 会接受（+00042 → 42）并与 part-000042 撞序号，只有逐位数字校验能挡住"},
```

同时更新两条旧注释（它们原本指向已不存在的 Atoi/n<0 分支）：
- `part-00004a.jsonl`：「只有 Atoi 错误检查能挡住」→「长度检查会放行，只有逐位数字校验能挡住它」
- `part--00042.jsonl`：「长度恰好 6 位且 Atoi 成功，只有 n<0 检查能挡住」→「长度恰好 6 位，只有逐位数字校验能挡住它」

### 要求的删除变异验证（两侧原始输出）

**变异侧：删掉逐位数字校验分支（确认 diff 已生效）**

```
$ perl -0pi -e 's/\t\tif c < .0. \|\| c > .9. \{\n\t\t\treturn 0, false\n\t\t\}\n//' internal/tracearchive/ref.go
$ diff /tmp/r1_ref.go internal/tracearchive/ref.go
93,95d92
< 		if c < '0' || c > '9' {
< 			return 0, false
< 		}

$ go test ./internal/tracearchive/ ./tests/tracearchive/ -run 'TestParsePartSeq|TestParsePartSeqRejectsIllegalNames' -count=1 -v
--- PASS: TestParsePartSeq (0.00s)
    ref_internal_test.go:52: part-00004a.jsonl 不应被解析为合法块名（数字段 6 位但含非数字 —— 长度检查会放行，只有逐位数字校验能挡住它），实际得到 seq=89
=== RUN   TestParsePartSeqRejectsIllegalNames/part-+00042.jsonl
    ref_internal_test.go:52: part-+00042.jsonl 不应被解析为合法块名（前导加号 —— strconv.Atoi 会接受（+00042 → 42）并与 part-000042 撞序号，只有逐位数字校验能挡住），实际得到 seq=25100042
    ref_internal_test.go:52: part--00042.jsonl 不应被解析为合法块名（负号混在数字段里 —— 长度恰好 6 位，只有逐位数字校验能挡住它），实际得到 seq=25300042
--- FAIL: TestParsePartSeqRejectsIllegalNames (0.00s)
    --- FAIL: TestParsePartSeqRejectsIllegalNames/part-00004a.jsonl (0.00s)
    --- FAIL: TestParsePartSeqRejectsIllegalNames/part-+00042.jsonl (0.00s)
    --- FAIL: TestParsePartSeqRejectsIllegalNames/part--00042.jsonl (0.00s)
FAIL
EXIT=1
```

**`part-+00042.jsonl` 在变异体下得到 `seq=25100042`**——这正是「前导 `+` 被当成数字参与累加」的实证：没有逐位校验时它不是被 Atoi 语义接受，而是被算术悄悄吸收，撞号风险比评审描述的还直接。

**恢复侧（同一命令）：**

```
$ cp /tmp/r1_ref.go internal/tracearchive/ref.go
$ go test ./internal/tracearchive/ ./tests/tracearchive/ -run 'TestParsePartSeq|TestParsePartSeqRejectsIllegalNames|TestSentinelErrorsAreDistinct' -count=1
ok  	github.com/example/llm-gateway/internal/tracearchive	0.005s
ok  	github.com/example/llm-gateway/tests/tracearchive	0.005s
EXIT=0
```

## F2【要修 2】A 层「为什么破例放包内」逐处说明

`TestParsePartSeqRejectsIllegalNames` 的函数注释补上理由（原文「【为什么破例放包内】」只挂在 `TestParsePartSeq` 上）：

```go
// TestParsePartSeqRejectsIllegalNames 逐个分支覆盖 parsePartSeq 的拒绝逻辑。
//
// 【为什么破例放包内】与上面的正例同因：被断言的是未导出的 parsePartSeq，
// 在 B 层（tests/tracearchive，package tracearchive_test）不可见，
// 无法从外部测试包触达这条严格解析路径。
```

`ref_internal_test.go` 现在**两个**测试函数各自都写明了破例理由，满足「每处须在测试函数注释里说明为何必须触达未导出符号」的全局约束。

## F3【要修 3】6 个哨兵无互异断言

B 层（`tests/tracearchive/ref_test.go`）新增 `TestSentinelErrorsAreDistinct`：对 6 个哨兵做两两 `errors.Is` 断言必须为 false（含 `errors` 包导入）。注释写明判据是「分类不会静默合并」而非「哨兵非空」，并指出该断言顺带覆盖「哨兵为 nil」——`errors.Is(nil, nil)` 为 true，会被判失败。

**变异验证（证明这条断言真的能抓到复制粘贴）**：把 `errors.go` 的 `ErrNotArchived` 改成 `ErrBodyMissing`：

```
$ diff /tmp/r1_errors.go internal/tracearchive/errors.go
20c20
< 	ErrNotArchived = errors.New("tracearchive: 产物与暂存均不存在")
---
> 	ErrNotArchived = ErrBodyMissing

$ go test ./tests/tracearchive/ -run 'TestSentinelErrorsAreDistinct' -count=1 -v
=== RUN   TestSentinelErrorsAreDistinct
    ref_test.go:96: ErrBodyMissing 与 ErrNotArchived 被 errors.Is 判为同一错误，分类会静默合并两类语义不同的失败
    ref_test.go:96: ErrNotArchived 与 ErrBodyMissing 被 errors.Is 判为同一错误，分类会静默合并两类语义不同的失败
--- FAIL: TestSentinelErrorsAreDistinct (0.00s)
FAIL
EXIT=1
```

恢复后 `PASS`（见 F1 恢复侧输出）。

## F4【可选】`partName` 恒 6 位的隐含不变式 —— 采取**加注释**方案

未加测试（`Part >= 1_000_000` 按设计不可达，加测试等于测一个不可能状态），改为在 `partName` 上写明前置约束：

```go
// partName 生成块文件名。
//
// 前置约束：seq ∈ [0, 999999]。块序号按天独立编号，单日块数远达不到 10^6，
// 因此 %06d 恒为 6 位——这正是 parsePartSeq「恰好 6 位」约定所依赖的前提。
// 若某天真的越过 999999，产出的是 7 位文件名，会被 parsePartSeq 拒绝而非静默截断。
```

关键点写明：越界时是**被拒绝**（可观测的失败）而非静默截断（危险的静默行为）。

## 转交项：inst 白名单校验 —— 未做（遵守裁决）

按要求**未在 `ref.go` 加任何校验**，`Paths` 层保持不变（`PartDir("2026-09-24", "../green")` 的清理语义问题留给 Task 5 的 `NewWriter`），也未新增任何导出错误哨兵。本轮的 6 个哨兵集合与首轮完全一致。

## 修复轮次 1 的验证输出（原始）

**评审指定的聚焦命令：**

```
$ go test ./internal/tracearchive/ ./tests/tracearchive/ -run 'TestPathsLayout|TestPartNameZeroPadsSixDigits|TestParsePartSeq|TestParsePartSeqRejectsIllegalNames' -count=1 -v
--- PASS: TestParsePartSeq (0.00s)
--- PASS: TestParsePartSeqRejectsIllegalNames (0.00s)   # 12 个子用例全 PASS（含 part-+00042.jsonl）
=== RUN   TestPathsLayout
--- PASS: TestPathsLayout (0.00s)
--- PASS: TestPartNameZeroPadsSixDigits (0.00s)
ok  	github.com/example/llm-gateway/internal/tracearchive	0.005s
ok  	github.com/example/llm-gateway/tests/tracearchive	0.005s
EXIT=0
```

**两包全量：**

```
$ go test ./internal/tracearchive/ ./tests/tracearchive/ -count=1
ok  	github.com/example/llm-gateway/internal/tracearchive	2.778s
ok  	github.com/example/llm-gateway/tests/tracearchive	0.005s
EXIT=0

$ go test ./tests/tracearchive/ -count=1 -v
--- PASS: TestPathsLayout (0.00s)
--- PASS: TestPartNameZeroPadsSixDigits (0.00s)
--- PASS: TestPartDirAndSpoolDir (0.00s)
--- PASS: TestRefString (0.00s)
--- PASS: TestSentinelErrorsAreDistinct (0.00s)
PASS
ok  	github.com/example/llm-gateway/tests/tracearchive	0.005s
EXIT=0
```

**守卫变异矩阵（新实现，6 条全被杀死）：**

```
M1 删除 part- 前缀检查       killed by TestParsePartSeq, TestParsePartSeqRejectsIllegalNames/000042.jsonl
M2 删除 .jsonl.zst 扩展名分支 killed by TestParsePartSeq
M3 删除 .jsonl 扩展名分支     killed by TestParsePartSeq
M4 删除扩展名 default 分支    killed by TestParsePartSeq, TestParsePartSeqRejectsIllegalNames/part-000042
M5 删除 6 位长度检查          killed by TestParsePartSeq, .../part-.jsonl.zst, .../part-1234.jsonl
M6 删除逐位数字校验（新）      killed by TestParsePartSeq, .../part-+00042.jsonl, .../part--00042.jsonl
```

（首轮矩阵的 M5/M6 是「Atoi 错误检查」「n<0 检查」；由于本轮用逐位校验取代了 Atoi，这两条守卫已不存在，矩阵相应收缩为 6 条，且**没有留下任何不可达/等价变异守卫**。）

**格式 / 静态检查 / 构建：**

```
$ gofmt -l internal/tracearchive/ tests/tracearchive/
(空输出 = 干净)

$ go vet ./internal/tracearchive/ ./tests/tracearchive/
vet OK

$ go build ./internal/... ./cmd/...
build EXIT=0
```

## 修复轮次的变更文件

```
7f5eda3c fix(tracearchive): 逐位校验数字段，拒绝 Atoi 会吞下的前导符号
 internal/tracearchive/ref.go               | 20 ++++++++++++++----
 internal/tracearchive/ref_internal_test.go |  9 ++++++--
 tests/tracearchive/ref_test.go             | 33 ++++++++++++++++++++++++++++++
 3 files changed, 56 insertions(+), 6 deletions(-)
```

（`errors.go` 本轮**无改动**——哨兵集合与首轮一致；F3 只新增断言。）

## 修复轮次的技术异议

无。三条要修项均成立并已按建议修复；F1 的具体修法（用循环取代 Atoi 而非在其前面叠加一道校验）与评审建议的差别已在 F1 中说明理由：叠加会让 Atoi 的两条分支变成不可达守卫，正是首轮刚论证过的「不可被测试区分」问题。
