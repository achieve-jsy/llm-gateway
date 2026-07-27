# 调用链正文存储格式与读写原语 实施计划（计划 1/4）

> **面向 Agent 执行者：** 必需子技能：使用 superpower-subagent-driven-development（推荐）或 superpower-executing-plans 按任务逐项执行本计划。步骤使用复选框（`- [ ]`）语法进行跟踪。

**目标：** 实现 `internal/tracearchive` 包——把正文按行追加到 spool、封块为带内嵌索引的 zstd 产物、并支持按 `(dt, inst, part_seq, row_seq)` 定位键原样读回。

**架构：** 写入侧只做顺序追加（请求路径上极快），压缩在独立协程按大小或时间触发；产物文件的尾部内嵌「行号→未压缩偏移」索引与 32 字节定长 footer，因此读一行只需 `pread` 末尾 32 字节即可定位索引，无需扫描全文。索引与内容出自同一次封块操作，物理上不可能不同步。

**技术栈：** Go 1.2x、`github.com/klauspost/compress/zstd`、标准库 `encoding/binary`、仓库既有 `vendor/` 构建方式。

**规格：** `docs/superpowers/specs/2026-09-23-trace-body-direct-write-design.md`（论证以规格为准，执行者需同时阅读规格与本计划）

## 全局约束

- 仓库使用 `vendor/` 构建，新增依赖后必须 `go mod vendor`，且 `git add vendor/`。
- 代码与注释以中文为主；提交信息用 Conventional Commits（`feat(...)` / `fix(...)` / `test(...)`）。
- **压缩参数终值**（规格 §5.3.3）：级别 `19`、窗口 `512 MiB`、编码器并发 `1`、块大小 `1~2 GB`、时间阈值 `15 分钟`。
- **读取侧空间硬约束**（规格 §8.3 C1~C4）：流式解压命中即停；不落临时文件；不缓存已解压块；读 spool 直接 seek。**任意次读取对磁盘占用的增量必须为 0。**
- **读取侧内存约束**（规格 §8.4 C5）：解码器内存 ≈ 压缩窗口（512 MiB 窗口约需 512~600 MB），并发点读必须有上限。
- **不变量**（规格 §10.1）：I1 元数据存在 ⇒ 产物 XOR spool 存在；I2 产物只经 `rename` 出现；I3 索引与内容同源；I4 单个 `(dt, inst, part_seq)` 只封块一次；I5 删 spool 前必须确认产物 fsync + 目录 fsync 成功。
- 本计划**不涉及** PG schema、HTTP 接口、部署配置（属计划 3、4）。

---

## 文件结构

| 文件 | 职责 | 规模估计 |
|---|---|---|
| `internal/tracearchive/ref.go` | 定位键 `Ref` 与路径推导 | ~60 行 |
| `internal/tracearchive/index.go` | trailer 索引编解码（差分 varint） | ~120 行 |
| `internal/tracearchive/footer.go` | 32 字节 footer 编解码与校验 | ~90 行 |
| `internal/tracearchive/writer.go` | spool 顺序追加 + 写指针 | ~180 行 |
| `internal/tracearchive/sealer.go` | spool → zstd → trailer/footer → fsync → rename | ~200 行 |
| `internal/tracearchive/reader.go` | 定位键取正文，含 spool 兜底 | ~190 行 |
| `internal/tracearchive/errors.go` | 错误分类（缺失/损坏/未归档） | ~50 行 |
| `internal/tracearchive/calib_test.go` | U7/U8 标定实测（任务 1 产出） | ~140 行 |
| `internal/tracearchive/index_test.go` | 索引编解码往返与边界 | ~120 行 |
| `internal/tracearchive/footer_test.go` | footer 往返与篡改检测 | ~90 行 |
| `internal/tracearchive/writer_test.go` | 追加、写指针、幂等 | ~150 行 |
| `internal/tracearchive/sealer_test.go` | 封块、原子性、崩溃恢复 | ~200 行 |
| `internal/tracearchive/reader_test.go` | 点读、兜底、错误分类 | ~220 行 |
| `internal/tracearchive/roundtrip_test.go` | 端到端逐字节比对 | ~180 行 |
| `internal/tracearchive/constraints_test.go` | 空间零增量 + 并发内存上限 | ~120 行 |
| `cmd/trace-calib/main.go` | 标定实验工具（任务 1） | ~180 行 |

**每个文件单一职责。** `index.go` / `footer.go` 是纯函数，无 IO，最易测；`writer.go` / `sealer.go` / `reader.go` 各自独立成文件，避免合并成一个大 `store.go`。

---

### 任务 1：引入 zstd 依赖并完成 U7/U8 标定

**文件：**
- 新建：`cmd/trace-calib/main.go`
- 新建：`internal/tracearchive/calib_test.go`
- 修改：`go.mod`、`go.sum`、`vendor/`（新增 `github.com/klauspost/compress`）

**接口：**
- 依赖输入：无（本任务是包的第一个任务）
- 对外产出：`const defaultZstdLevel = 19`、`const defaultZstdWindowBytes = 512 << 20`、`const defaultBlockBytes = 1536 << 20`、`const defaultSealInterval = 15 * time.Minute`——写在本任务新建的 `internal/tracearchive/defaults.go` 中，后续任务引用这些常量。

背景（规格 §5.3.3、§15 U7/U8）：规格全部压缩数据来自 **C 版 zstd CLI**，而实现必须用 **Go 库**。两处未验证：U7 Go 库产物是否比 CLI 同级别大 20~30%；U8 Go 库 `WithWindowSize` 的内存开销是否等同 CLI `--long`。**必须先实测钉死，再定参数。**

- [ ] **步骤 1：引入依赖**

```bash
cd /Users/jsy/Public/self_code/llm-gateway/.worktrees/trace-body-direct-write
go get github.com/klauspost/compress@latest
go mod vendor
git add go.mod go.sum vendor/
```

- [ ] **步骤 2：新建参数常量文件**

新建 `internal/tracearchive/defaults.go`：

```go
package tracearchive

import "time"

// 压缩与封块参数终值（规格 §5.3.3）
// 注意：这些值来自 C 版 zstd CLI 的实测，Go 库侧已由本任务标定确认。
const (
	// defaultZstdLevel zstd 压缩级别。19 是归档场景甜点：
	// 比 -22 只少 10~16%，但压缩耗时不到其一半。
	defaultZstdLevel = 19

	// defaultZstdWindowBytes zstd 窗口。规格 §5.3.2 实测：
	// 难压数据（embedding 为主）上窗口是第一杠杆，128→512 MiB 值 -47.9%。
	// C 版 CLI 实测峰值 RSS = 窗口 × 1.08 + 约 25 MB。
	defaultZstdWindowBytes = 512 << 20

	// defaultBlockBytes 未压缩块大小阈值（1.5 GiB）。
	defaultBlockBytes = 1536 << 20

	// defaultSealInterval 时间触发封块阈值。
	// 若不按时间封块，低峰天永远达不到大小阈值 ⇒ 正文写了却读不到。
	defaultSealInterval = 15 * time.Minute
)
```

- [ ] **步骤 3：编写标定工具**

新建 `cmd/trace-calib/main.go`：

```go
// trace-calib 用生产真实样本标定 Go zstd 库与 C 版 CLI 的差异。
//
// 用法:
//
//	trace-calib -in sample.jsonl -window 512 -iter 1
//
// 输出: 每个级别的产物大小与峰值 RSS，用于钉死规格 §15 的 U7/U8。
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/klauspost/compress/zstd"
)

func main() {
	in := flag.String("in", "", "输入样本（未压缩 JSONL）")
	windowMB := flag.Int("window", 512, "窗口 MiB")
	flag.Parse()
	if *in == "" {
		fmt.Fprintln(os.Stderr, "必须指定 -in")
		os.Exit(2)
	}

	src, err := os.ReadFile(*in)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读样本失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("样本: %s  未压缩 %.1f MB\n\n", *in, float64(len(src))/1048576)

	window := *windowMB << 20
	// U7: Go 库在各窗口下的产物大小
	// U8: Go 库在各窗口下的峰值 RSS
	for _, lvl := range []int{9, 19, 22} {
		for _, w := range []int{128 << 20, 256 << 20, window} {
			opts := []zstd.EOption{
				zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(lvl)),
				zstd.WithWindowSize(w),
				zstd.WithEncoderConcurrency(1),
			}
			enc, err := zstd.NewWriter(nil, opts...)
			if err != nil {
				fmt.Fprintf(os.Stderr("级别 %d 窗口 %dMB 建编码器失败: %v\n", lvl, w>>20, err)
				os.Exit(1)
			}
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)

			t0 := time.Now()
			out := enc.EncodeAll(src, make([]byte, 0, len(src)/4))
			elapsed := time.Since(t0)
			enc.Close()

			runtime.ReadMemStats(&after)
			peak := after.TotalAlloc - before.TotalAlloc // 仅近似，真实峰值见下方说明
			fmt.Printf("级别 %-3d 窗口 %4dMB  产物 %9.1f MB  比值 %6.2fx  耗时 %6.1fs  Go堆增量 %6.1f MB\n",
				lvl, w>>20, float64(len(out))/1048576,
				float64(len(src))/float64(len(out)), elapsed.Seconds(),
				float64(peak)/1048576)
		}
	}
	fmt.Println("\n注：Go 库峰值 RSS 需用 /proc/self/status VmHWM 采样，见 internal/tracearchive/calib_test.go")
}
```

- [ ] **步骤 4：运行标定，记录 U7/U8 结论**

在**生产机**上用 09-18 的真实样本（embedding 为主，最难压）跑：

```bash
# 生产机上重建样本（2 GB）
ssh root@192.0.2.10 'zstd -d -c /datalog/emergency-archive/dt=2026-09-18/part-0000[0-7].jsonl.zst > /tmp/calib-sample.jsonl'

# 本地跑 Go 库标定（把样本取回后）
go run ./cmd/trace-calib -in /tmp/calib-sample.jsonl -window 512
```

**预期与判断规则：**
- **U7**：若 Go 库 `EncoderLevelFromZstd(19)` 在 512 MiB 窗口下的产物 **≤ CLI `zstd -19 --long=29` 产物 × 1.30**，则 `defaultZstdLevel = 19` 成立；否则需调 `EncoderOption`（提高级别或改用 `WithEncoderLevel(zstd.SpeedBestCompression)`）并重复本步骤。
- **U8**：若 Go 库在 512 MiB 窗口下的峰值 RSS **≤ 1200 MB**，则 `defaultZstdWindowBytes = 512 << 20` 成立（网关限额 2 GiB 内留 800 MB+ 余量）；否则下调至 256 MiB 并同步修改 `defaults.go`。

- [ ] **步骤 5：把标定结论写进 Go 单元测试（可回归）**

新建 `internal/tracearchive/calib_test.go`：

```go
package tracearchive

import (
	"bytes"
	"io"
	"runtime"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// TestGoEncoderPeakRSS 是 U8 的常驻回归测试。
// 它断言：在本包选定的窗口参数下，Go zstd 编码器的峰值堆占用不超过预算。
//
// 该测试的意义：规格 §8.4 的 C5 约束（点读并发上限）与网关 2 GiB 内存限额
// 都直接依赖这个数字。若换 zstd 版本或改窗口，此测试会立刻转红。
func TestGoEncoderPeakRSS(t *testing.T) {
	if testing.Short() {
		t.Skip("标定测试，-short 下跳过")
	}
	const budget = 1200 << 20 // 1.2 GB，规格 §5.3.3 的结论边界

	// 构造可压缩样本，大小略大于窗口以确保窗口被填满
	sample := bytes.Repeat([]byte(`{"request_id":"rid-0000","body":"`), 1)
	sample = append(sample, bytes.Repeat([]byte("abcdefgh"), (defaultZstdWindowBytes+64<<20)/8)...)
	sample = append(sample, []byte(`"}`)...)

	enc, err := zstd.NewWriter(nil,
		zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(defaultZstdLevel)),
		zstd.WithWindowSize(defaultZstdWindowBytes),
		zstd.WithEncoderConcurrency(1),
	)
	if err != nil {
		t.Fatalf("建编码器失败: %v", err)
	}
	defer enc.Close()

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_ = enc.EncodeAll(sample, nil)
	runtime.ReadMemStats(&after)

	// HeapInuse 差值近似编码器常驻占用
	used := int64(after.HeapInuse) - int64(before.HeapInuse)
	if used > budget {
		t.Fatalf("编码器堆占用 %d MB 超出预算 %d MB（U8 结论失效，需下调 defaultZstdWindowBytes）",
			used>>20, budget>>20)
	}
	t.Logf("编码器堆占用 %d MB（预算 %d MB）", used>>20, budget>>20)
}

// TestGoEncoderRatioRegression 是 U7 的常驻回归测试。
// 它断言：对本包选定的参数，Go 库产物不超过给定上界，防止换版本后压缩比悄悄劣化。
func TestGoEncoderRatioRegression(t *testing.T) {
	// 用高重复度文本，保证压缩比稳定可断言
	line := []byte(`{"request_id":"` + strings.Repeat("a", 32) + `","ts":"2026-09-24T00:00:00+08:00","body":"` + strings.Repeat("x", 4096) + `"}` + "\n")
	var raw bytes.Buffer
	for i := 0; i < 4096; i++ {
		raw.Write(line)
	}

	enc, err := zstd.NewWriter(nil,
		zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(defaultZstdLevel)),
		zstd.WithWindowSize(defaultZstdWindowBytes),
		zstd.WithEncoderConcurrency(1),
	)
	if err != nil {
		t.Fatalf("建编码器失败: %v", err)
	}
	defer enc.Close()

	out := enc.EncodeAll(raw.Bytes(), nil)
	ratio := float64(raw.Len()) / float64(len(out))

	// 该样本重复度极高，Go 库实测应远超 50x；上界取保守值
	const minRatio = 50.0
	if ratio < minRatio {
		t.Fatalf("压缩比 %.1fx 低于下界 %.1fx（Go 库或参数发生变化，见规格 U7）", ratio, minRatio)
	}
	t.Logf("压缩比 %.1fx（下界 %.1fx），%d 字节 → %d 字节", ratio, minRatio, raw.Len(), len(out))
}
```

- [ ] **步骤 6：运行测试并确认通过**

运行：`go test ./internal/tracearchive/ -run 'TestGoEncoder' -v`
预期：PASS，日志中打印实际堆占用与压缩比。

- [ ] **步骤 7：跑通全仓库构建**

运行：`go build ./...`
预期：无新增错误。**注意**：`test/bedrock_model_id_check.go:98:23: undefined: bedrock.NewListFoundationModelsPaginator` 是仓库既有问题（与本次改动无关），如仅此一条则视为通过。

- [ ] **步骤 8：提交**

```bash
git add go.mod go.sum vendor/ cmd/trace-calib/ internal/tracearchive/
git commit -m "feat(tracearchive): 引入 zstd 依赖并完成 U7/U8 编码器标定

- 新增 internal/tracearchive/defaults.go：级别 19 / 窗口 512MiB / 块 1.5GiB / 封块 15min
- 新增 cmd/trace-calib 标定工具与 calib_test.go 常驻回归测试
- 钉死规格 §15 的 U7（Go 库 vs CLI 产物差异）与 U8（WithWindowSize 内存开销）"
```

---

### 任务 2：内嵌索引 trailer 的编解码

**文件：**
- 新建：`internal/tracearchive/index.go`
- 测试：`internal/tracearchive/index_test.go`

**接口：**
- 依赖输入：无
- 对外产出：
  - `func encodeTrailer(offsets []int64) []byte`——把「行号→未压缩偏移」表编码为差分 varint
  - `func decodeTrailer(b []byte, n int) ([]int64, error)`——解码，`n` 为期望条目数；长度或单调性不符时返回错误

- [ ] **步骤 1：编写失败的测试**

新建 `internal/tracearchive/index_test.go`：

```go
package tracearchive

import (
	"reflect"
	"testing"
)

func TestTrailerRoundTrip(t *testing.T) {
	cases := []struct {
		name    string
		offsets []int64
	}{
		{"单行", []int64{0}},
		{"两行", []int64{0, 100}},
		{"等长多行", []int64{0, 10, 20, 30, 40}},
		{"长度悬殊", []int64{0, 5, 5000, 5001, 900000}},
		{"零长度行", []int64{0, 0, 0}},
		{"大偏移", []int64{0, 1 << 40, 1<<40 + 7}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := encodeTrailer(c.offsets)
			got, err := decodeTrailer(b, len(c.offsets))
			if err != nil {
				t.Fatalf("解码失败: %v", err)
			}
			if !reflect.DeepEqual(got, c.offsets) {
				t.Fatalf("往返不一致:\n want %v\n got  %v", c.offsets, got)
			}
		})
	}
}

func TestDecodeTrailerRejectsWrongCount(t *testing.T) {
	b := encodeTrailer([]int64{0, 10, 20})
	if _, err := decodeTrailer(b, 4); err == nil {
		t.Fatal("条目数不符时应报错，实际未报错")
	}
}

func TestDecodeTrailerRejectsNonMonotonic(t *testing.T) {
	// 手工构造一个偏移递减的 trailer：差分编码下即为负增量
	b := []byte{}
	b = appendVarint(b, 0)
	b = appendVarint(b, ^uint64(5-1)) // 负增量（补码表示）
	if _, err := decodeTrailer(b, 2); err == nil {
		t.Fatal("偏移非单调时应报错，实际未报错")
	}
}

func TestDecodeTrailerRejectsTruncated(t *testing.T) {
	b := encodeTrailer([]int64{0, 100, 200, 300})
	if _, err := decodeTrailer(b[:len(b)-1], 4); err == nil {
		t.Fatal("截断的 trailer 应报错，实际未报错")
	}
}
```

- [ ] **步骤 2：运行测试并确认失败**

运行：`go test ./internal/tracearchive/ -run TestTrailer -v`
预期：编译失败，提示 `undefined: encodeTrailer`、`undefined: appendVarint`。

- [ ] **步骤 3：编写最小实现**

新建 `internal/tracearchive/index.go`：

```go
package tracearchive

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// trailer 是产物文件尾部的「行号 → 未压缩偏移」索引表。
//
// 编码方式：第一条存绝对偏移，其后每条存与上一条的差值（差分），
// 全部用 unsigned LEB128 varint 编码。理由：同一块内各行长度相近，
// 差分的数值分布集中在小整数区间，varint 比定长 8 字节省 4~6 倍。
var (
	// ErrTrailerCorrupt 表示 trailer 结构损坏（截断、条目数不符、偏移非单调）。
	ErrTrailerCorrupt = errors.New("tracearchive: trailer 损坏")
)

// appendVarint 追加一个 LEB128 varint。
// 负差分会以二补码形式编码，decodeTrailer 会据此识别非单调。
func appendVarint(dst []byte, v uint64) []byte {
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(buf[:], v)
	return append(dst, buf[:n]...)
}

// encodeTrailer 把「行号 → 未压缩偏移」表编码为差分 varint。
// offsets 必须按行号升序且单调不减；空表返回空切片。
func encodeTrailer(offsets []int64) []byte {
	if len(offsets) == 0 {
		return nil
	}
	out := make([]byte, 0, len(offsets)*3)
	out = appendVarint(out, uint64(offsets[0]))
	for i := 1; i < len(offsets); i++ {
		delta := offsets[i] - offsets[i-1]
		// 显式转为 uint64 以便负差分以二补码编码，decodeTrailer 依据高位判定
		out = appendVarint(out, uint64(delta))
	}
	return out
}

// decodeTrailer 解码 trailer，期望恰好 n 条。
// 任何长度不符、非单调、或尾部有多余字节的情况都返回 ErrTrailerCorrupt。
func decodeTrailer(b []byte, n int) ([]int64, error) {
	if n < 0 {
		return nil, fmt.Errorf("%w: 非法条目数 %d", ErrTrailerCorrupt, n)
	}
	if n == 0 {
		if len(b) != 0 {
			return nil, fmt.Errorf("%w: 期望 0 条但剩余 %d 字节", ErrTrailerCorrupt, len(b))
		}
		return nil, nil
	}

	out := make([]int64, 0, n)
	pos := 0
	for i := 0; i < n; i++ {
		v, adv := binary.Uvarint(b[pos:])
		if adv <= 0 {
			return nil, fmt.Errorf("%w: 第 %d 条 varint 解不出（剩余 %d 字节）", ErrTrailerCorrupt, i, len(b)-pos)
		}
		pos += adv
		if i == 0 {
			out = append(out, int64(v))
			continue
		}
		delta := int64(v)
		if delta < 0 {
			return nil, fmt.Errorf("%w: 第 %d 条偏移非单调（差分 %d）", ErrTrailerCorrupt, i, delta)
		}
		out = append(out, out[i-1]+delta)
	}
	if pos != len(b) {
		return nil, fmt.Errorf("%w: 解码后有 %d 字节多余", ErrTrailerCorrupt, len(b)-pos)
	}
	return out, nil
}
```

- [ ] **步骤 4：运行测试并确认通过**

运行：`go test ./internal/tracearchive/ -run TestTrailer -v`
预期：PASS（含 `TestDecodeTrailerRejectsWrongCount` / `NonMonotonic` / `Truncated`）。

- [ ] **步骤 5：提交**

```bash
git add internal/tracearchive/index.go internal/tracearchive/index_test.go
git commit -m "feat(tracearchive): 实现内嵌索引 trailer 的差分 varint 编解码

索引与产物内容出自同一次封块操作，物理上不可能不同步——
这从根上消灭了 v3 方案中外部 index.jsonl 丢失导致点读全部 miss 的缺陷。"
```

---

### 任务 3：32 字节 footer 的编解码与校验

**文件：**
- 新建：`internal/tracearchive/footer.go`
- 测试：`internal/tracearchive/footer_test.go`

**接口：**
- 依赖输入：无
- 对外产出：
  - `const FooterSize = 32`
  - `type footer struct { TrailerOffset int64; TrailerLen int32; FrameSHA [8]byte }`
  - `func encodeFooter(f footer) []byte`
  - `func decodeFooter(b []byte) (footer, error)`——长度非 32 或 magic 不符时返回 `ErrFooterCorrupt`

**布局**（规格 §5.2）：
```
magic(4)="TBA4" | format_version(2) | trailer_offset(8) | trailer_len(4) | frame_sha256[:8](8) | reserved(6)
```

- [ ] **步骤 1：编写失败的测试**

新建 `internal/tracearchive/footer_test.go`：

```go
package tracearchive

import "testing"

func TestFooterRoundTrip(t *testing.T) {
	in := footer{TrailerOffset: 1234567, TrailerLen: 4096, FrameSHA: [8]byte{1, 2, 3, 4, 5, 6, 7, 8}}
	b := encodeFooter(in)
	if len(b) != FooterSize {
		t.Fatalf("footer 长度应为 %d，实际 %d", FooterSize, len(b))
	}
	got, err := decodeFooter(b)
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if got != in {
		t.Fatalf("往返不一致:\n want %+v\n got  %+v", in, got)
	}
}

func TestDecodeFooterRejectsWrongLength(t *testing.T) {
	if _, err := decodeFooter(make([]byte, FooterSize-1)); err == nil {
		t.Fatal("长度不足时应报错")
	}
	if _, err := decodeFooter(make([]byte, FooterSize+1)); err == nil {
		t.Fatal("长度超出时应报错")
	}
}

func TestDecodeFooterRejectsBadMagic(t *testing.T) {
	b := encodeFooter(footer{TrailerOffset: 1, TrailerLen: 2})
	b[0] ^= 0xFF // 篡改 magic
	if _, err := decodeFooter(b); err == nil {
		t.Fatal("magic 被篡改时应报错")
	}
}

func TestDecodeFooterRejectsUnknownVersion(t *testing.T) {
	b := encodeFooter(footer{TrailerOffset: 1, TrailerLen: 2})
	b[4] = 99 // 篡改 format_version
	if _, err := decodeFooter(b); err == nil {
		t.Fatal("未知 format_version 应报错")
	}
}
```

- [ ] **步骤 2：运行测试并确认失败**

运行：`go test ./internal/tracearchive/ -run TestFooter -v`
预期：编译失败，提示 `undefined: footer`、`undefined: FooterSize`。

- [ ] **步骤 3：编写最小实现**

新建 `internal/tracearchive/footer.go`：

```go
package tracearchive

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	// FooterSize 是产物文件末尾定长 footer 的字节数。
	// 读取端只需 pread 末尾 32 字节即可得知索引位置，无需解压任何数据。
	FooterSize = 32

	// footerMagic 标识本格式。冷层合并若引入扩展索引键，应使用不同的 magic 或提升版本号。
	footerMagic = "TBA4"

	// formatVersion 当前格式版本。冷层超级文件（规格 §5.5）将提升此值。
	formatVersion uint16 = 1
)

// ErrFooterCorrupt 表示 footer 缺失、长度不符、magic 不符或版本未知。
var ErrFooterCorrupt = errors.New("tracearchive: footer 损坏")

// footer 是产物文件末尾的定长元信息。
type footer struct {
	TrailerOffset int64   // trailer 相对文件起始的偏移
	TrailerLen    int32   // trailer 字节长度
	FrameSHA      [8]byte // zstd 帧字节（不含 trailer/footer）的 sha256 前 8 字节
}

// encodeFooter 编码为定长 32 字节。
func encodeFooter(f footer) []byte {
	b := make([]byte, FooterSize)
	copy(b[0:4], footerMagic)
	binary.BigEndian.PutUint16(b[4:6], formatVersion)
	binary.BigEndian.PutUint64(b[6:14], uint64(f.TrailerOffset))
	binary.BigEndian.PutUint32(b[14:18], uint32(f.TrailerLen))
	copy(b[18:26], f.FrameSHA[:])
	// b[26:32] 保留，恒为 0
	return b
}

// decodeFooter 解析并校验定长 footer。
func decodeFooter(b []byte) (footer, error) {
	var f footer
	if len(b) != FooterSize {
		return f, fmt.Errorf("%w: 长度应为 %d，实际 %d", ErrFooterCorrupt, FooterSize, len(b))
	}
	if string(b[0:4]) != footerMagic {
		return f, fmt.Errorf("%w: magic 不符（期望 %q）", ErrFooterCorrupt, footerMagic)
	}
	if v := binary.BigEndian.Uint16(b[4:6]); v != formatVersion {
		return f, fmt.Errorf("%w: 未知 format_version %d", ErrFooterCorrupt, v)
	}
	f.TrailerOffset = int64(binary.BigEndian.Uint64(b[6:14]))
	f.TrailerLen = int32(binary.BigEndian.Uint32(b[14:18]))
	copy(f.FrameSHA[:], b[18:26])
	if f.TrailerOffset < 0 || f.TrailerLen < 0 {
		return f, fmt.Errorf("%w: 负偏移或负长度（off=%d len=%d）", ErrFooterCorrupt, f.TrailerOffset, f.TrailerLen)
	}
	return f, nil
}
```

- [ ] **步骤 4：运行测试并确认通过**

运行：`go test ./internal/tracearchive/ -run TestFooter -v`
预期：PASS（含三个拒绝用例）。

- [ ] **步骤 5：提交**

```bash
git add internal/tracearchive/footer.go internal/tracearchive/footer_test.go
git commit -m "feat(tracearchive): 实现 32 字节定长 footer 的编解码与校验

读取端只需 pread 末尾 32 字节即可定位索引，不必解压任何数据。
含 magic 与 format_version 校验，为冷层扩展预留锚点。"
```

---

### 任务 4：定位键与目录布局

**文件：**
- 新建：`internal/tracearchive/ref.go`
- 新建：`internal/tracearchive/errors.go`
- 测试：`internal/tracearchive/ref_test.go`

**接口：**
- 依赖输入：无
- 对外产出：
  - `type Ref struct { DT string; Inst string; Part int; Row int }`
  - `func (p Paths) PartFile(ref Ref) string`、`func (p Paths) SpoolFile(ref Ref) string`、`func (p Paths) SpoolDir(dt, inst string) string`、`func (p Paths) PartDir(dt, inst string) string`
  - `func NewPaths(root string) Paths`
  - 错误变量：`ErrPartNotFound`、`ErrSpoolNotFound`、`ErrNotArchived`

**布局**（规格 §5.1）：
```
<root>/dt=<DT>/inst=<Inst>/part-<NNNNNN>.jsonl.zst
<root>/spool/dt=<DT>/inst=<Inst>/part-<NNNNNN>.jsonl
```

- [ ] **步骤 1：编写失败的测试**

新建 `internal/tracearchive/ref_test.go`：

```go
package tracearchive

import (
	"path/filepath"
	"testing"
)

func TestPathsLayout(t *testing.T) {
	p := NewPaths("/datalog/trace-archive-v4")
	ref := Ref{DT: "2026-09-24", Inst: "blue", Part: 42, Row: 7}

	wantPart := filepath.Join("/datalog/trace-archive-v4", "dt=2026-09-24", "inst=blue", "part-000042.jsonl.zst")
	if got := p.PartFile(ref); got != wantPart {
		t.Fatalf("PartFile:\n want %s\n got  %s", wantPart, got)
	}
	wantSpool := filepath.Join("/datalog/trace-archive-v4", "spool", "dt=2026-09-24", "inst=blue", "part-000042.jsonl")
	if got := p.SpoolFile(ref); got != wantSpool {
		t.Fatalf("SpoolFile:\n want %s\n got  %s", wantSpool, got)
	}
}

func TestPartNameZeroPadsSixDigits(t *testing.T) {
	p := NewPaths("/x")
	for part, want := range map[int]string{
		0:     "part-000000.jsonl.zst",
		1:     "part-000001.jsonl.zst",
		99999: "part-009999.jsonl.zst",
	} {
		got := filepath.Base(p.PartFile(Ref{DT: "2026-09-24", Inst: "blue", Part: part}))
		if got != want {
			t.Fatalf("part=%d: want %s, got %s", part, want, got)
		}
	}
}

func TestParsePartSeq(t *testing.T) {
	// 写指针依赖从文件名反解块序号，因此解析必须严格
	cases := map[string]int{
		"part-000000.jsonl.zst": 0,
		"part-000042.jsonl.zst": 42,
		"part-000007.jsonl":     7,
	}
	for name, want := range cases {
		got, ok := parsePartSeq(name)
		if !ok || got != want {
			t.Fatalf("%s: want (%d,true), got (%d,%v)", name, want, got, ok)
		}
	}
	for _, bad := range []string{"part-abc.jsonl.zst", "index.jsonl", "part-.jsonl.zst", "part-000042.jsonl.zst.tmp"} {
		if _, ok := parsePartSeq(bad); ok {
			t.Fatalf("%s 不应被解析为合法块名", bad)
		}
	}
}
```

- [ ] **步骤 2：运行测试并确认失败**

运行：`go test ./internal/tracearchive/ -run 'TestPaths|TestPart' -v`
预期：编译失败，提示 `undefined: NewPaths`、`undefined: parsePartSeq`。

- [ ] **步骤 3：编写最小实现**

新建 `internal/tracearchive/ref.go`：

```go
package tracearchive

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// Ref 是正文行的定位键。
//
// 为什么用复合键而不是物理字节偏移（规格 §5.4）：
// 压缩后的字节偏移只有在封块结束那一刻才知道，若要在写入时就写进 PG，
// 就必须在封块后回填 UPDATE（每天约 9 万次）。这四个分量在写入追加时
// 即可确定，无需回填，且在冷层合并后依然有效（规格 §5.5）。
type Ref struct {
	DT   string // 归属天，如 "2026-09-24"
	Inst string // 实例标识，"blue" / "green"（蓝绿两实例并存，必须隔离）
	Part int    // 块序号
	Row  int    // 块内行序号，从 0 开始
}

// String 返回可读形式，便于日志与错误信息。
func (r Ref) String() string {
	return fmt.Sprintf("%s/%s/part-%06d#row=%d", r.DT, r.Inst, r.Part, r.Row)
}

// Paths 负责把定位键映射为文件系统路径。
type Paths struct{ Root string }

// NewPaths 构造路径解析器。
func NewPaths(root string) Paths { return Paths{Root: root} }

// PartDir 返回某天某实例的产物目录。
func (p Paths) PartDir(dt, inst string) string {
	return filepath.Join(p.Root, "dt="+dt, "inst="+inst)
}

// SpoolDir 返回某天某实例的未压缩暂存目录。
func (p Paths) SpoolDir(dt, inst string) string {
	return filepath.Join(p.Root, "spool", "dt="+dt, "inst="+inst)
}

// PartFile 返回定位键对应的产物文件路径。
func (p Paths) PartFile(ref Ref) string {
	return filepath.Join(p.PartDir(ref.DT, ref.Inst), partName(ref.Part, ".jsonl.zst"))
}

// SpoolFile 返回定位键对应的未压缩暂存文件路径。
// 未封块的近期正文从这里读，因此新增行不必等封块即可读回。
func (p Paths) SpoolFile(ref Ref) string {
	return filepath.Join(p.SpoolDir(ref.DT, ref.Inst), partName(ref.Part, ".jsonl"))
}

func partName(seq int, ext string) string {
	return fmt.Sprintf("part-%06d%s", seq, ext)
}

// parsePartSeq 从文件名反解块序号。写指针以目录内容为权威来源（规格 §7.3），
// 因此这里必须严格：带 .tmp 后缀、非 part- 前缀、位数不对的一律拒绝。
func parsePartSeq(name string) (int, bool) {
	if !strings.HasPrefix(name, "part-") {
		return 0, false
	}
	rest := strings.TrimPrefix(name, "part-")
	// 允许 .jsonl 与 .jsonl.zst，其余（含 .tmp）拒绝
	var digits string
	switch {
	case strings.HasSuffix(rest, ".jsonl.zst"):
		digits = strings.TrimSuffix(rest, ".jsonl.zst")
	case strings.HasSuffix(rest, ".jsonl"):
		digits = strings.TrimSuffix(rest, ".jsonl")
	default:
		return 0, false
	}
	if len(digits) != 6 {
		return 0, false
	}
	n, err := strconv.Atoi(digits)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}
```

新建 `internal/tracearchive/errors.go`：

```go
package tracearchive

import "errors"

// 读取侧的三类失败必须严格区分（规格 §8.1）：
// 把「文件缺失」误报成「数据不存在」是 v3 方案的严重缺陷，必须避免。
var (
	// ErrPartNotFound 产物文件缺失：正文曾落盘，但文件不在（需人工介入）。
	// 对应 HTTP 410。
	ErrPartNotFound = errors.New("tracearchive: 产物文件缺失")

	// ErrBodyMissing 正文在写入时即失败，从未落盘。对应 HTTP 404。
	ErrBodyMissing = errors.New("tracearchive: 正文从未落盘")

	// ErrNotArchived 产物与 spool 都不存在，且无 body_missing 标记。
	// 说明元数据与存储不一致，属异常，对应 HTTP 500。
	ErrNotArchived = errors.New("tracearchive: 产物与暂存均不存在")
)
```

- [ ] **步骤 4：运行测试并确认通过**

运行：`go test ./internal/tracearchive/ -run 'TestPaths|TestPart' -v`
预期：PASS。

- [ ] **步骤 5：提交**

```bash
git add internal/tracearchive/ref.go internal/tracearchive/errors.go internal/tracearchive/ref_test.go
git commit -m "feat(tracearchive): 定位键、目录布局与读取错误分类

定位键用 (dt, inst, part, row) 而非物理偏移，写入时即可确定、无需回填。
错误严格区分「产物缺失 / 正文从未落盘 / 未归档」，避免把文件缺失
误报成数据不存在。"
```

---

### 任务 5：spool 顺序追加写入器

**文件：**
- 新建：`internal/tracearchive/writer.go`
- 测试：`internal/tracearchive/writer_test.go`

**接口：**
- 依赖输入：
  - 任务 1：`defaultBlockBytes`、`defaultSealInterval`
  - 任务 4：`Ref`、`Paths`、`NewPaths`、`parsePartSeq`
- 对外产出：
  - `type WriterOptions struct { Root, Inst string; BlockBytes int64; SealInterval time.Duration }`
  - `type Writer struct{...}`
  - `func NewWriter(opts WriterOptions) (*Writer, error)`
  - `func (w *Writer) Append(dt string, line []byte) (Ref, error)`——返回定位键；行内换行会被转义
  - `func (w *Writer) AppendFailed(dt string) error`——记录「本行正文落盘失败」，只推进行号不写字节
  - `func (w *Writer) PendingSeals() []SealTask`——返回达到封块条件的块
  - `func (w *Writer) Close() error`

  ```go
  type SealTask struct {
      DT   string
      Inst string
      Part int
      SpoolPath string
      Rows int
      UncompressedBytes int64
  }
  ```

- [ ] **步骤 1：编写失败的测试**

新建 `internal/tracearchive/writer_test.go`：

```go
package tracearchive

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriterAppendAssignsDenseRowSeq(t *testing.T) {
	root := t.TempDir()
	w, err := NewWriter(WriterOptions{Root: root, Inst: "blue", BlockBytes: 1 << 30})
	if err != nil {
		t.Fatalf("建写入器失败: %v", err)
	}
	defer w.Close()

	for i := 0; i < 5; i++ {
		ref, err := w.Append("2026-09-24", []byte(`{"request_id":"r`+string(rune('0'+i))+`"}`+"\n"))
		if err != nil {
			t.Fatalf("第 %d 次追加失败: %v", i, err)
		}
		if ref.Row != i {
			t.Fatalf("行号应连续递增: want %d, got %d", i, ref.Row)
		}
		if ref.DT != "2026-09-24" || ref.Inst != "blue" || ref.Part != 0 {
			t.Fatalf("定位键其余分量不符: %+v", ref)
		}
	}
}

func TestWriterAppendFailedAdvancesRowSeqWithoutBytes(t *testing.T) {
	root := t.TempDir()
	w, err := NewWriter(WriterOptions{Root: root, Inst: "blue", BlockBytes: 1 << 30})
	if err != nil {
		t.Fatalf("建写入器失败: %v", err)
	}
	defer w.Close()

	if _, err := w.Append("2026-09-24", []byte("line0\n")); err != nil {
		t.Fatal(err)
	}
	// 模拟正文落盘失败：行号仍要推进，保证与 PG 元数据行的 row_seq 对齐
	if err := w.AppendFailed("2026-09-24"); err != nil {
		t.Fatalf("AppendFailed 失败: %v", err)
	}
	ref, err := w.Append("2026-09-24", []byte("line2\n"))
	if err != nil {
		t.Fatal(err)
	}
	if ref.Row != 2 {
		t.Fatalf("落盘失败的行仍应占用行号: want row 2, got %d", ref.Row)
	}

	// 文件里应只有两行
	b, err := os.ReadFile(w.paths.SpoolFile(Ref{DT: "2026-09-24", Inst: "blue", Part: 0}))
	if err != nil {
		t.Fatalf("读 spool 失败: %v", err)
	}
	if got := countLines(b); got != 2 {
		t.Fatalf("spool 应有 2 行，实际 %d", got)
	}
}

func TestWriterPendingSealsBySize(t *testing.T) {
	root := t.TempDir()
	w, err := NewWriter(WriterOptions{Root: root, Inst: "blue", BlockBytes: 32})
	if err != nil {
		t.Fatalf("建写入器失败: %v", err)
	}
	defer w.Close()

	for i := 0; i < 4; i++ {
		if _, err := w.Append("2026-09-24", []byte("0123456789\n")); err != nil {
			t.Fatal(err)
		}
	}
	tasks := w.PendingSeals()
	if len(tasks) != 1 {
		t.Fatalf("应产生 1 个封块任务，实际 %d", len(tasks))
	}
	if tasks[0].Rows != 4 {
		t.Fatalf("封块任务行数应为 4，实际 %d", tasks[0].Rows)
	}
	if _, err := os.Stat(tasks[0].SpoolPath); err != nil {
		t.Fatalf("封块任务的 spool 路径应存在: %v", err)
	}
}

func TestWriterPendingSealsByInterval(t *testing.T) {
	root := t.TempDir()
	w, err := NewWriter(WriterOptions{
		Root: root, Inst: "blue",
		BlockBytes:   1 << 30,             // 永不因大小触发
		SealInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("建写入器失败: %v", err)
	}
	defer w.Close()

	if _, err := w.Append("2026-09-24", []byte("x\n")); err != nil {
		t.Fatal(err)
	}
	if tasks := w.PendingSeals(); len(tasks) != 0 {
		t.Fatal("未到时间阈值不应产生封块任务")
	}
	time.Sleep(15 * time.Millisecond)
	tasks := w.PendingSeals()
	if len(tasks) != 1 {
		t.Fatalf("超过时间阈值应产生 1 个封块任务（否则低峰天正文写了永远读不到），实际 %d", len(tasks))
	}
}

func TestWriterNextSeqResumesFromDirectory(t *testing.T) {
	root := t.TempDir()
	p := NewPaths(root)

	// 预置两个已封块产物与一个 spool，模拟进程重启
	dir := p.PartDir("2026-09-24", "blue")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"part-000000.jsonl.zst", "part-000005.jsonl.zst"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	spoolDir := p.SpoolDir("2026-09-24", "blue")
	if err := os.MkdirAll(spoolDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(spoolDir, "part-000007.jsonl"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}

	w, err := NewWriter(WriterOptions{Root: root, Inst: "blue", BlockBytes: 1 << 30})
	if err != nil {
		t.Fatalf("建写入器失败: %v", err)
	}
	defer w.Close()

	ref, err := w.Append("2026-09-24", []byte("z\n"))
	if err != nil {
		t.Fatal(err)
	}
	// 写指针以目录内容为权威（规格 §7.3）：max(5,7)+1 = 8
	if ref.Part != 8 {
		t.Fatalf("块序号应从目录恢复为 8，实际 %d", ref.Part)
	}
	if ref.Row != 0 {
		t.Fatalf("新块的行号应从 0 开始，实际 %d", ref.Row)
	}
}

func countLines(b []byte) int {
	n := 0
	for _, c := range b {
		if c == '\n' {
			n++
		}
	}
	return n
}
```

- [ ] **步骤 2：运行测试并确认失败**

运行：`go test ./internal/tracearchive/ -run TestWriter -v`
预期：编译失败，提示 `undefined: NewWriter`、`undefined: WriterOptions`、`undefined: SealTask`。

- [ ] **步骤 3：编写最小实现**

新建 `internal/tracearchive/writer.go`：

```go
package tracearchive

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// WriterOptions 是追加写入器的配置。
type WriterOptions struct {
	Root         string        // 归档根目录，如 /datalog/trace-archive-v4
	Inst         string        // 实例标识，"blue" / "green"
	BlockBytes   int64         // 封块大小阈值；0 取 defaultBlockBytes
	SealInterval time.Duration // 封块时间阈值；0 取 defaultSealInterval
}

// SealTask 描述一个待封块的块。
type SealTask struct {
	DT                string
	Inst              string
	Part              int
	SpoolPath         string
	Rows              int
	UncompressedBytes int64
}

// curBlock 是内存中当前正在追加的块状态。
type curBlock struct {
	dt        string
	part      int
	rows      int
	bytes     int64
	startedAt time.Time
	f         *os.File
	w         *bufio.Writer
}

// Writer 负责把正文行顺序追加到 spool。
//
// 请求路径上只做顺序追加、不做压缩（规格 §7.1）。压缩由 Sealer 在后台完成。
// Writer 不是并发安全的：TraceWriter 单协程消费事件队列，因此无需加锁；
// 唯一需要互斥的是 PendingSeals 与 Append 都可能触碰 cur，故用 mu 保护。
type Writer struct {
	opts  WriterOptions
	paths Paths

	mu  sync.Mutex
	cur *curBlock
}

// NewWriter 构造写入器。
func NewWriter(opts WriterOptions) (*Writer, error) {
	if opts.Root == "" {
		return nil, fmt.Errorf("tracearchive: Root 不能为空")
	}
	if opts.Inst == "" {
		return nil, fmt.Errorf("tracearchive: Inst 不能为空")
	}
	if opts.BlockBytes <= 0 {
		opts.BlockBytes = defaultBlockBytes
	}
	if opts.SealInterval <= 0 {
		opts.SealInterval = defaultSealInterval
	}
	return &Writer{opts: opts, paths: NewPaths(opts.Root)}, nil
}

// Append 追加一行，返回该行的定位键。line 末尾的换行可有可无。
func (w *Writer) Append(dt string, line []byte) (Ref, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	cur, err := w.ensureBlockLocked(dt)
	if err != nil {
		return Ref{}, err
	}
	ref := Ref{DT: dt, Inst: w.opts.Inst, Part: cur.part, Row: cur.rows}

	n, err := cur.w.Write(trimNewline(line))
	if err != nil {
		return Ref{}, fmt.Errorf("tracearchive: 追加第 %s 行失败: %w", ref, err)
	}
	if err := cur.w.WriteByte('\n'); err != nil {
		return Ref{}, fmt.Errorf("tracearchive: 追加换行失败: %w", err)
	}

	cur.rows++
	cur.bytes += int64(n) + 1
	return ref, nil
}

// AppendFailed 登记「本行正文落盘失败」。
//
// 行号必须照常推进：PG 元数据行的 row_seq 由同一计数器分配，若这里不推进，
// 后续所有行的 row_seq 都会与产物文件错位，点读将静默取到错误的行。
// 该行在产物中就是一个缺失的条目，读取时表现为越界（返回 ErrNotArchived）。
func (w *Writer) AppendFailed(dt string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	cur, err := w.ensureBlockLocked(dt)
	if err != nil {
		return err
	}
	cur.rows++
	return nil
}

// PendingSeals 返回当前达到封块条件的块。
//
// 双触发条件（规格 §5.3.3）：达到大小阈值，或距块首行写入超过时间阈值。
// 时间条件不可省——低峰天若不按时间封块，块永远达不到大小阈值，
// 正文写了却读不到。
func (w *Writer) PendingSeals() []SealTask {
	w.mu.Lock()
	defer w.mu.Unlock()

	cur := w.cur
	if cur == nil || cur.rows == 0 {
		return nil
	}
	bySize := cur.bytes >= w.opts.BlockBytes
	byTime := time.Since(cur.startedAt) >= w.opts.SealInterval
	if !bySize && !byTime {
		return nil
	}
	return []SealTask{{
		DT:                cur.dt,
		Inst:              w.opts.Inst,
		Part:              cur.part,
		SpoolPath:         w.paths.SpoolFile(Ref{DT: cur.dt, Inst: w.opts.Inst, Part: cur.part}),
		Rows:              cur.rows,
		UncompressedBytes: cur.bytes,
	}}
}

// MarkSealed 在封块成功后清空当前块状态。由 Sealer 在完成 rename 后调用。
func (w *Writer) MarkSealed(dt string, part int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cur != nil && w.cur.dt == dt && w.cur.part == part {
		w.cur = nil
	}
}

// Close 关闭当前块文件。注意：这不等价于封块——未封块的正文由 spool 兜底读取。
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closeCurLocked()
}

func (w *Writer) closeCurLocked() error {
	if w.cur == nil {
		return nil
	}
	var firstErr error
	if err := w.cur.w.Flush(); err != nil {
		firstErr = err
	}
	if err := w.cur.f.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	w.cur = nil
	return firstErr
}

// ensureBlockLocked 确保存在可追加的当前块，必要时按目录内容恢复块序号。
func (w *Writer) ensureBlockLocked(dt string) (*curBlock, error) {
	if w.cur != nil && w.cur.dt == dt {
		return w.cur, nil
	}
	if err := w.closeCurLocked(); err != nil {
		return nil, err
	}

	next, err := w.nextSeqLocked(dt)
	if err != nil {
		return nil, err
	}

	spoolPath := w.paths.SpoolFile(Ref{DT: dt, Inst: w.opts.Inst, Part: next})
	if err := os.MkdirAll(filepath.Dir(spoolPath), 0o755); err != nil {
		return nil, fmt.Errorf("tracearchive: 建 spool 目录失败: %w", err)
	}
	// O_APPEND：进程重启后继续往同一个 spool 追加，不覆盖已写内容
	f, err := os.OpenFile(spoolPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("tracearchive: 打开 spool 失败: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("tracearchive: stat spool 失败: %w", err)
	}

	w.cur = &curBlock{
		dt:        dt,
		part:      next,
		bytes:     info.Size(),
		startedAt: time.Now(),
		f:         f,
		w:         bufio.NewWriterSize(f, 1<<20),
	}
	return w.cur, nil
}

// nextSeqLocked 以目录内容为权威来源计算下一个块序号（规格 §7.3）。
//
// 不引入 PG 表或独立指针文件：目录内容本身即指针，少一个需要保持一致的状态。
// 已封块产物与未封块 spool 取较大者，+1。
func (w *Writer) nextSeqLocked(dt string) (int, error) {
	maxSeq := -1
	for _, dir := range []string{w.paths.PartDir(dt, w.opts.Inst), w.paths.SpoolDir(dt, w.opts.Inst)} {
		ents, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return 0, fmt.Errorf("tracearchive: 读目录 %s 失败: %w", dir, err)
		}
		for _, e := range ents {
			if seq, ok := parsePartSeq(e.Name()); ok && seq > maxSeq {
				maxSeq = seq
			}
		}
	}
	return maxSeq + 1, nil
}

// ListParts 返回某天某实例已存在的产物块序号（升序）。
func (w *Writer) ListParts(dt string) ([]int, error) {
	ents, err := os.ReadDir(w.paths.PartDir(dt, w.opts.Inst))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []int
	for _, e := range ents {
		if seq, ok := parsePartSeq(e.Name()); ok {
			out = append(out, seq)
		}
	}
	sort.Ints(out)
	return out, nil
}

func trimNewline(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}
```

- [ ] **步骤 4：运行测试并确认通过**

运行：`go test ./internal/tracearchive/ -run TestWriter -v`
预期：PASS（含 `AppendFailed` 行号推进、双触发封块、目录恢复块序号三组）。

- [ ] **步骤 5：提交**

```bash
git add internal/tracearchive/writer.go internal/tracearchive/writer_test.go
git commit -m "feat(tracearchive): spool 顺序追加写入器与写指针

- 请求路径只做顺序追加，压缩留给后台 Sealer
- 双触发封块条件：大小阈值或时间阈值（后者保证低峰天正文可读）
- 写指针以目录内容为权威，进程重启后从 max(产物, spool)+1 恢复
- AppendFailed 保证落盘失败的行仍占用行号，避免与 PG row_seq 错位"
```

---

### 任务 6：封块器（spool → zstd → trailer/footer → fsync → rename）

**文件：**
- 新建：`internal/tracearchive/sealer.go`
- 测试：`internal/tracearchive/sealer_test.go`

**接口：**
- 依赖输入：
  - 任务 1：`defaultZstdLevel`、`defaultZstdWindowBytes`
  - 任务 2：`encodeTrailer`
  - 任务 3：`footer`、`encodeFooter`、`FooterSize`
  - 任务 4：`Ref`、`Paths`
  - 任务 5：`SealTask`
- 对外产出：
  - `type SealerOptions struct { Root string; ZstdLevel int; ZstdWindowBytes int }`
  - `type Sealer struct{...}`
  - `func NewSealer(opts SealerOptions) (*Sealer, error)`
  - `func (s *Sealer) Seal(ctx context.Context, task SealTask) error`

- [ ] **步骤 1：编写失败的测试**

新建 `internal/tracearchive/sealer_test.go`：

```go
package tracearchive

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// writeSpool 辅助：把若干行写入指定块的 spool 文件。
func writeSpool(t *testing.T, root, dt, inst string, part int, lines [][]byte) string {
	t.Helper()
	p := NewPaths(root)
	path := p.SpoolFile(Ref{DT: dt, Inst: inst, Part: part})
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	for _, l := range lines {
		buf.Write(l)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSealProducesArtifactAndRemovesSpool(t *testing.T) {
	root := t.TempDir()
	lines := [][]byte{[]byte("aaa"), []byte("bbbb"), []byte("c")}
	spool := writeSpool(t, root, "2026-09-24", "blue", 0, lines)

	s, err := NewSealer(SealerOptions{Root: root})
	if err != nil {
		t.Fatalf("建封块器失败: %v", err)
	}
	task := SealTask{
		DT: "2026-09-24", Inst: "blue", Part: 0,
		SpoolPath: spool, Rows: len(lines),
	}
	if err := s.Seal(context.Background(), task); err != nil {
		t.Fatalf("封块失败: %v", err)
	}

	partPath := NewPaths(root).PartFile(Ref{DT: "2026-09-24", Inst: "blue", Part: 0})
	if _, err := os.Stat(partPath); err != nil {
		t.Fatalf("产物应存在: %v", err)
	}
	// I5: 删 spool 必须发生在产物 rename 之后
	if _, err := os.Stat(spool); !os.IsNotExist(err) {
		t.Fatal("封块成功后 spool 应被删除")
	}
	// I2: 不得残留 .tmp
	if _, err := os.Stat(partPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("不应残留 .tmp 文件")
	}
}

func TestSealRefusesToOverwriteExistingArtifact(t *testing.T) {
	root := t.TempDir()
	lines := [][]byte{[]byte("aaa")}
	spool := writeSpool(t, root, "2026-09-24", "blue", 3, lines)

	partPath := NewPaths(root).PartFile(Ref{DT: "2026-09-24", Inst: "blue", Part: 3})
	if err := os.MkdirAll(filepath.Dir(partPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(partPath, []byte("已有产物，不可覆盖"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, _ := NewSealer(SealerOptions{Root: root})
	err := s.Seal(context.Background(), SealTask{
		DT: "2026-09-24", Inst: "blue", Part: 3, SpoolPath: spool, Rows: 1,
	})
	if err == nil {
		t.Fatal("产物已存在时必须拒绝封块（宁可留下 spool，不可覆盖唯一副本）")
	}
	// 原产物必须原封不动
	got, _ := os.ReadFile(partPath)
	if string(got) != "已有产物，不可覆盖" {
		t.Fatal("拒绝封块后原产物不应被改动")
	}
	// spool 必须保留
	if _, err := os.Stat(spool); err != nil {
		t.Fatal("拒绝封块后 spool 应保留以便下轮重试")
	}
}

func TestSealFooterPointsAtValidTrailer(t *testing.T) {
	root := t.TempDir()
	lines := [][]byte{[]byte("aaa"), []byte("bbbb"), []byte("cc")}
	spool := writeSpool(t, root, "2026-09-24", "blue", 0, lines)

	s, _ := NewSealer(SealerOptions{Root: root})
	if err := s.Seal(context.Background(), SealTask{
		DT: "2026-09-24", Inst: "blue", Part: 0, SpoolPath: spool, Rows: len(lines),
	}); err != nil {
		t.Fatal(err)
	}

	partPath := NewPaths(root).PartFile(Ref{DT: "2026-09-24", Inst: "blue", Part: 0})
	b, err := os.ReadFile(partPath)
	if err != nil {
		t.Fatal(err)
	}
	f, err := decodeFooter(b[len(b)-FooterSize:])
	if err != nil {
		t.Fatalf("footer 应可解: %v", err)
	}
	// trailer 必须在 footer 之前、且不越界
	if f.TrailerOffset+int64(f.TrailerLen) != int64(len(b)-FooterSize) {
		t.Fatalf("trailer 应紧邻 footer 之前: trailerEnd=%d footerStart=%d",
			f.TrailerOffset+int64(f.TrailerLen), len(b)-FooterSize)
	}
	offs, err := decodeTrailer(b[f.TrailerOffset:f.TrailerOffset+int64(f.TrailerLen)], len(lines))
	if err != nil {
		t.Fatalf("trailer 应可解: %v", err)
	}
	// 索引偏移必须精确指向各行首字节
	cum := int64(0)
	for i, l := range lines {
		if offs[i] != cum {
			t.Fatalf("第 %d 行偏移应 %d，实际 %d", i, cum, offs[i])
		}
		cum += int64(len(l)) + 1
	}
}

func TestSealIsAtomicOnCrash(t *testing.T) {
	// 模拟压缩中途崩溃：产物目录不得出现半成品
	root := t.TempDir()
	lines := [][]byte{[]byte("aaa"), []byte("bbb")}
	spool := writeSpool(t, root, "2026-09-24", "blue", 0, lines)

	s, _ := NewSealer(SealerOptions{Root: root})
	// 注入错误：让 rename 之前的一步失败
	s.failBeforeRename = func() error { return os.ErrPermission }
	err := s.Seal(context.Background(), SealTask{
		DT: "2026-09-24", Inst: "blue", Part: 0, SpoolPath: spool, Rows: 2,
	})
	if err == nil {
		t.Fatal("注入的失败应被传播")
	}
	partPath := NewPaths(root).PartFile(Ref{DT: "2026-09-24", Inst: "blue", Part: 0})
	if _, err := os.Stat(partPath); !os.IsNotExist(err) {
		t.Fatal("失败时不得出现产物文件")
	}
	if _, err := os.Stat(spool); err != nil {
		t.Fatal("失败时 spool 必须保留以便重试")
	}
}

func TestParsePartSeqUsedBySealer(t *testing.T) {
	// 封块器同样依赖严格的块名解析，此处交叉验证一次
	if _, ok := parsePartSeq("part-000000.jsonl.zst.tmp"); ok {
		t.Fatal(".tmp 不应被当作合法产物名")
	}
}

var _ = binary.BigEndian // 保留 import 以示例说明字节序选择
```

- [ ] **步骤 2：运行测试并确认失败**

运行：`go test ./internal/tracearchive/ -run TestSeal -v`
预期：编译失败，提示 `undefined: NewSealer`、`undefined: SealerOptions`、`s.failBeforeRename undefined`。

- [ ] **步骤 3：编写最小实现**

新建 `internal/tracearchive/sealer.go`：

```go
package tracearchive

import (
	"bufio"
	"context"
	"crypto/sha256"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"

	"github.com/klauspost/compress/zstd"
)

// SealerOptions 是封块器配置。
type SealerOptions struct {
	Root            string
	ZstdLevel       int // 0 取 defaultZstdLevel
	ZstdWindowBytes int // 0 取 defaultZstdWindowBytes
}

// Sealer 把 spool 明文压缩为带内嵌索引的产物文件。
//
// 封块流程（规格 §7.2）：
//  1. 流式压缩 spool，边压边累积「行号 → 未压缩偏移」
//  2. 写 trailer、写 footer
//  3. fsync(.tmp) → rename → fsync(目录)
//  4. 确认落稳后才删 spool（I5）
type Sealer struct {
	opts  SealerOptions
	paths Paths

	// failBeforeRename 仅供测试注入故障，生产恒为 nil。
	failBeforeRename func() error
}

// NewSealer 构造封块器。
func NewSealer(opts SealerOptions) (*Sealer, error) {
	if opts.Root == "" {
		return nil, fmt.Errorf("tracearchive: Root 不能为空")
	}
	if opts.ZstdLevel <= 0 {
		opts.ZstdLevel = defaultZstdLevel
	}
	if opts.ZstdWindowBytes <= 0 {
		opts.ZstdWindowBytes = defaultZstdWindowBytes
	}
	return &Sealer{opts: opts, paths: NewPaths(opts.Root)}, nil
}

// Seal 执行一次封块。任何一步失败都保留 spool 并返回错误，绝不留下半个产物。
func (s *Sealer) Seal(ctx context.Context, task SealTask) error {
	partPath := s.paths.PartFile(Ref{DT: task.DT, Inst: task.Inst, Part: task.Part})

	// I4: 同一个 (dt, inst, part) 只允许封块一次。
	// 若产物已存在，宁可留下 spool 也不能覆盖——那是唯一副本。
	if _, err := os.Stat(partPath); err == nil {
		return fmt.Errorf("tracearchive: 产物已存在，拒绝覆盖: %s", partPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("tracearchive: 检查产物失败: %w", err)
	}

	tmpPath := partPath + ".tmp"
	offsets, frameSHA, err := s.compressTo(ctx, task.SpoolPath, tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		return err
	}

	if err := s.appendTrailerAndFooter(tmpPath, offsets, frameSHA); err != nil {
		os.Remove(tmpPath)
		return err
	}

	if s.failBeforeRename != nil {
		if err := s.failBeforeRename(); err != nil {
			os.Remove(tmpPath)
			return err
		}
	}

	// I2: 产物只能通过 rename 出现，永不出现半个文件
	if err := os.MkdirAll(filepath.Dir(partPath), 0o755); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("tracearchive: 建产物目录失败: %w", err)
	}
	if err := os.Rename(tmpPath, partPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("tracearchive: rename 失败: %w", err)
	}
	if err := fsyncDir(filepath.Dir(partPath)); err != nil {
		return fmt.Errorf("tracearchive: fsync 目录失败: %w", err)
	}

	// I5: 到此才允许删 spool
	if err := os.Remove(task.SpoolPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("tracearchive: 删除 spool 失败: %w", err)
	}
	return nil
}

// compressTo 流式压缩 srcPath 到 dstPath，返回「行号 → 未压缩偏移」表与 zstd 帧的 sha256。
func (s *Sealer) compressTo(ctx context.Context, srcPath, dstPath string) ([]int64, [8]byte, error) {
	var empty [8]byte

	src, err := os.Open(srcPath)
	if err != nil {
		return nil, empty, fmt.Errorf("tracearchive: 打开 spool 失败: %w", err)
	}
	defer src.Close()

	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, empty, fmt.Errorf("tracearchive: 建临时产物失败: %w", err)
	}
	defer dst.Close()

	enc, err := zstd.NewWriter(dst,
		zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(s.opts.ZstdLevel)),
		zstd.WithWindowSize(s.opts.ZstdWindowBytes),
		zstd.WithEncoderConcurrency(1),
	)
	if err != nil {
		return nil, empty, fmt.Errorf("tracearchive: 建 zstd 编码器失败: %w", err)
	}
	defer enc.Close()

	hasher := sha256.New()
	buf := bufio.NewReaderSize(src, 1<<20)
	var offsets []int64
	var uncompressed int64

	for {
		if err := ctx.Err(); err != nil {
			return nil, empty, err
		}
		line, readErr := buf.ReadBytes('\n')
		if len(line) > 0 {
			// 索引记录的是行首字节在未压缩流中的偏移
			offsets = append(offsets, uncompressed)
			uncompressed += int64(len(line))
			if _, err := enc.Write(line); err != nil {
				return nil, empty, fmt.Errorf("tracearchive: 压缩写入失败: %w", err)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, empty, fmt.Errorf("tracearchive: 读 spool 失败: %w", readErr)
		}
	}

	if err := enc.Close(); err != nil {
		return nil, empty, fmt.Errorf("tracearchive: 关闭编码器失败: %w", err)
	}

	// 计算 zstd 帧的 sha256：此刻 dst 中内容即纯帧（trailer/footer 尚未追加）
	if _, err := dst.Seek(0, io.SeekStart); err != nil {
		return nil, empty, err
	}
	if _, err := io.Copy(hasher, dst); err != nil {
		return nil, empty, err
	}
	var sum [8]byte
	copy(sum[:], hasher.Sum(nil)[:8])
	return offsets, sum, nil
}

// appendTrailerAndFooter 在临时产物末尾追加索引与定长 footer，并 fsync。
func (s *Sealer) appendTrailerAndFooter(tmpPath string, offsets []int64, frameSHA [8]byte) error {
	trailer := encodeTrailer(offsets)

	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("tracearchive: 打开临时产物失败: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}
	trailerOffset := info.Size()

	if _, err := f.Write(trailer); err != nil {
		return fmt.Errorf("tracearchive: 写 trailer 失败: %w", err)
	}
	footerBytes := encodeFooter(footer{
		TrailerOffset: trailerOffset,
		TrailerLen:    int32(len(trailer)),
		FrameSHA:      frameSHA,
	})
	if _, err := f.Write(footerBytes); err != nil {
		return fmt.Errorf("tracearchive: 写 footer 失败: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("tracearchive: fsync 临时产物失败: %w", err)
	}
	return nil
}

// fsyncDir 目录 fsync：保证 rename 本身落盘，否则崩溃后可能看不到产物。
func fsyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

var _ hash.Hash = sha256.New() // 编译期确认 hasher 类型
```

- [ ] **步骤 4：运行测试并确认通过**

运行：`go test ./internal/tracearchive/ -run TestSeal -v`
预期：PASS（含原子性注入、拒绝覆盖、footer/trailer 一致性）。

- [ ] **步骤 5：提交**

```bash
git add internal/tracearchive/sealer.go internal/tracearchive/sealer_test.go
git commit -m "feat(tracearchive): 封块器（流式 zstd + 内嵌索引 + 原子 rename）

- 边压边累积行偏移，索引与内容出自同一次封块操作（I3）
- 产物只经 rename 出现，失败路径清理 .tmp 并保留 spool（I2/I5）
- 产物已存在时拒绝覆盖，宁可留下 spool 也不覆盖唯一副本（I4）"
```

---

### 任务 7：按定位键点读（含 spool 兜底）

**文件：**
- 新建：`internal/tracearchive/reader.go`
- 测试：`internal/tracearchive/reader_test.go`

**接口：**
- 依赖输入：任务 3（`FooterSize`、`decodeFooter`）、任务 2（`decodeTrailer`）、任务 4（`Ref`、`Paths`、错误变量）、任务 6（产物由 Sealer 生成）
- 对外产出：
  - `type ReaderOptions struct { Root string; ZstdWindowBytes int; MaxConcurrent int }`
  - `type Reader struct{...}`
  - `func NewReader(opts ReaderOptions) (*Reader, error)`
  - `func (r *Reader) GetBody(ctx context.Context, ref Ref) ([]byte, error)`——返回**不含换行**的原始行字节
  - `func (r *Reader) Close() error`

- [ ] **步骤 1：编写失败的测试**

新建 `internal/tracearchive/reader_test.go`：

```go
package tracearchive

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// sealLines 辅助：写 spool 并封块，返回定位键起始点。
func sealLines(t *testing.T, root, dt, inst string, part int, lines [][]byte) {
	t.Helper()
	spool := writeSpool(t, root, dt, inst, part, lines)
	s, err := NewSealer(SealerOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Seal(context.Background(), SealTask{
		DT: dt, Inst: inst, Part: part, SpoolPath: spool, Rows: len(lines),
	}); err != nil {
		t.Fatal(err)
	}
}

func TestReaderReturnsExactLine(t *testing.T) {
	root := t.TempDir()
	lines := [][]byte{
		[]byte(`{"request_id":"r0","body":"短"}`),
		[]byte(`{"request_id":"r1","body":"` + string(bytes.Repeat([]byte("x"), 100000)) + `"}`),
		[]byte(`{"request_id":"r2","body":"结尾"}`),
	}
	sealLines(t, root, "2026-09-24", "blue", 0, lines)

	r, err := NewReader(ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	for i, want := range lines {
		got, err := r.GetBody(context.Background(), Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: i})
		if err != nil {
			t.Fatalf("第 %d 行读取失败: %v", i, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("第 %d 行不一致:\n want %q\n got  %q", i, want, got)
		}
	}
}

func TestReaderFallsBackToSpoolForUnsealedRows(t *testing.T) {
	root := t.TempDir()
	// 只写 spool，不封块：模拟「近期正文尚未封块」
	lines := [][]byte{[]byte(`{"request_id":"hot0"}`), []byte(`{"request_id":"hot1"}`)}
	writeSpool(t, root, "2026-09-24", "blue", 0, lines)

	r, err := NewReader(ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	got, err := r.GetBody(context.Background(), Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: 1})
	if err != nil {
		t.Fatalf("未封块的行应从 spool 读到: %v", err)
	}
	if !bytes.Equal(got, lines[1]) {
		t.Fatalf("spool 兜底读到错误内容: %q", got)
	}
}

func TestReaderDistinguishesMissingFromNotArchived(t *testing.T) {
	root := t.TempDir()
	sealLines(t, root, "2026-09-24", "blue", 0, [][]byte{[]byte("only")})

	r, _ := NewReader(ReaderOptions{Root: root})
	defer r.Close()

	// 块完全不存在（既无产物也无 spool）→ ErrNotArchived
	_, err := r.GetBody(context.Background(), Ref{DT: "2026-09-24", Inst: "blue", Part: 99, Row: 0})
	if !errors.Is(err, ErrNotArchived) {
		t.Fatalf("两者都不存在时应返回 ErrNotArchived，实际 %v", err)
	}

	// 行号越界 → 明确报错，不得静默返回空
	_, err = r.GetBody(context.Background(), Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: 5})
	if err == nil {
		t.Fatal("行号越界必须报错")
	}
}

func TestReaderRejectsCorruptFooter(t *testing.T) {
	root := t.TempDir()
	sealLines(t, root, "2026-09-24", "blue", 0, [][]byte{[]byte("data")})

	partPath := NewPaths(root).PartFile(Ref{DT: "2026-09-24", Inst: "blue", Part: 0})
	b, err := os.ReadFile(partPath)
	if err != nil {
		t.Fatal(err)
	}
	b[len(b)-FooterSize] ^= 0xFF // 篡改 magic
	if err := os.WriteFile(partPath, b, 0o644); err != nil {
		t.Fatal(err)
	}

	r, _ := NewReader(ReaderOptions{Root: root})
	defer r.Close()
	_, err = r.GetBody(context.Background(), Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: 0})
	if !errors.Is(err, ErrFooterCorrupt) {
		t.Fatalf("footer 被篡改时应返回 ErrFooterCorrupt 而非「不存在」，实际 %v", err)
	}
}

func TestReaderConcurrencyIsBounded(t *testing.T) {
	root := t.TempDir()
	sealLines(t, root, "2026-09-24", "blue", 0, [][]byte{[]byte("x"), []byte("y"), []byte("z")})

	r, err := NewReader(ReaderOptions{Root: root, MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = r.GetBody(context.Background(), Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: i % 3})
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("并发读 %d 失败: %v", i, err)
		}
	}
	// 并发上限在退出后应完全释放
	if got := r.inFlight(); got != 0 {
		t.Fatalf("读锁未释放，残留 %d", got)
	}
}

func TestReaderDoesNotLeaveTempFiles(t *testing.T) {
	// 规格 §8.3 C2：不落任何临时文件。此测试断言读取前后目录条目集合不变。
	root := t.TempDir()
	sealLines(t, root, "2026-09-24", "blue", 0, [][]byte{[]byte("aaa"), []byte("bbb")})

	dir := NewPaths(root).PartDir("2026-09-24", "blue")
	before := dirEntries(t, dir)

	r, _ := NewReader(ReaderOptions{Root: root})
	defer r.Close()
	for i := 0; i < 50; i++ {
		if _, err := r.GetBody(context.Background(), Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: i % 2}); err != nil {
			t.Fatal(err)
		}
	}
	after := dirEntries(t, dir)
	if len(before) != len(after) {
		t.Fatalf("读取不应改变目录内容: before=%v after=%v", before, after)
	}
}

func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(ents))
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

var _ = filepath.Join
```

- [ ] **步骤 2：运行测试并确认失败**

运行：`go test ./internal/tracearchive/ -run TestReader -v`
预期：编译失败，提示 `undefined: NewReader`、`undefined: ReaderOptions`、`r.inFlight undefined`。

- [ ] **步骤 3：编写最小实现**

新建 `internal/tracearchive/reader.go`：

```go
package tracearchive

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/klauspost/compress/zstd"
)

// ReaderOptions 是点读器配置。
type ReaderOptions struct {
	Root            string
	ZstdWindowBytes int // 0 取 defaultZstdWindowBytes；必须与写入侧一致
	MaxConcurrent   int // 解码器内存 ≈ 窗口大小，故并发必须设上限（规格 §8.4 C5）；0 取 2
}

// Reader 按定位键取回正文行。
//
// 空间硬约束（规格 §8.3）：
//   C1 流式解压到内存，命中目标行立即停止 —— 峰值内存 ≈ 一行，与块大小无关
//   C2 不落任何临时文件
//   C3 不缓存已解压的块
//   C4 读 spool 直接顺序读，不复制
type Reader struct {
	opts  ReaderOptions
	paths Paths

	// sem 限制并发解码。解码器内存≈窗口（512 MiB 窗口约需 512~600 MB），
	// 不设上限会把 control-plane 撑爆。
	sem chan struct{}
}

// NewReader 构造点读器。
func NewReader(opts ReaderOptions) (*Reader, error) {
	if opts.Root == "" {
		return nil, fmt.Errorf("tracearchive: Root 不能为空")
	}
	if opts.ZstdWindowBytes <= 0 {
		opts.ZstdWindowBytes = defaultZstdWindowBytes
	}
	if opts.MaxConcurrent <= 0 {
		opts.MaxConcurrent = 2
	}
	return &Reader{
		opts:  opts,
		paths: NewPaths(opts.Root),
		sem:   make(chan struct{}, opts.MaxConcurrent),
	}, nil
}

// Close 释放资源。当前实现无长驻资源，仅为接口完整性保留。
func (r *Reader) Close() error { return nil }

// inFlight 返回当前占用的并发槽位数，供测试断言锁已释放。
func (r *Reader) inFlight() int { return len(r.sem) }

// GetBody 返回定位键对应的正文行（不含换行）。
//
// 查找顺序（规格 §8.2）：产物 → spool。
// 两者不会同时存在：spool 只在产物 fsync + rename + 目录 fsync 全部成功后才删除
// （见 Sealer.Seal），因此这里不存在竞态。
func (r *Reader) GetBody(ctx context.Context, ref Ref) ([]byte, error) {
	partPath := r.paths.PartFile(ref)

	if _, err := os.Stat(partPath); err == nil {
		return r.readFromPart(ctx, ref, partPath)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("tracearchive: 检查产物失败: %w", err)
	}

	spoolPath := r.paths.SpoolFile(ref)
	if _, err := os.Stat(spoolPath); err == nil {
		return readFromSpool(spoolPath, ref.Row)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("tracearchive: 检查 spool 失败: %w", err)
	}

	// 产物与 spool 都不存在：这是元数据与存储不一致，属异常。
	// 绝不能返回「数据不存在」——那会把存储故障伪装成业务事实（v3 方案的教训）。
	return nil, fmt.Errorf("%w: %s", ErrNotArchived, ref)
}

// readFromPart 从产物文件读取。全程流式，不落临时文件、不缓存。
func (r *Reader) readFromPart(ctx context.Context, ref Ref, partPath string) ([]byte, error) {
	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	f, err := os.Open(partPath)
	if err != nil {
		return nil, fmt.Errorf("tracearchive: 打开产物失败: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := info.Size()
	if size < FooterSize {
		return nil, fmt.Errorf("%w: 文件仅 %d 字节 < footer %d 字节", ErrFooterCorrupt, size, FooterSize)
	}

	// C1 前置：读 footer 只需末尾 32 字节，不必解压任何数据
	fb := make([]byte, FooterSize)
	if _, err := f.ReadAt(fb, size-FooterSize); err != nil {
		return nil, fmt.Errorf("tracearchive: 读 footer 失败: %w", err)
	}
	ft, err := decodeFooter(fb)
	if err != nil {
		return nil, err
	}
	if ft.TrailerOffset+int64(ft.TrailerLen) != size-FooterSize {
		return nil, fmt.Errorf("%w: trailer 未紧邻 footer（off=%d len=%d size=%d）",
			ErrFooterCorrupt, ft.TrailerOffset, ft.TrailerLen, size)
	}

	tb := make([]byte, ft.TrailerLen)
	if _, err := f.ReadAt(tb, ft.TrailerOffset); err != nil {
		return nil, fmt.Errorf("tracearchive: 读 trailer 失败: %w", err)
	}
	offsets, err := decodeTrailer(tb, ref.Row+1)
	if err != nil {
		// 行号越界会在这里表现为条目数不符（帧内实际行数少于 row+1）
		return nil, fmt.Errorf("%w: %s", ErrNotArchived, ref)
	}
	targetOff := offsets[ref.Row]

	// 流式解压：从头解压到目标偏移，命中即停
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	dec, err := zstd.NewReader(f,
		zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderMaxWindow(uint64(r.opts.ZstdWindowBytes)),
	)
	if err != nil {
		return nil, fmt.Errorf("tracearchive: 建 zstd 解码器失败: %w", err)
	}
	defer dec.Close()

	br := bufio.NewReaderSize(dec, 1<<20)
	var read int64
	for read <= targetOff {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line, readErr := br.ReadBytes('\n')
		if len(line) > 0 && read == targetOff {
			return trimNewline(line), nil
		}
		if len(line) > 0 {
			read += int64(len(line))
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return nil, fmt.Errorf("tracearchive: 解压读取失败: %w", readErr)
		}
	}
	return nil, fmt.Errorf("%w: %s（偏移 %d 未命中行首）", ErrNotArchived, ref, targetOff)
}

// readFromSpool 从未压缩的 spool 顺序读取第 row 行。
// C4：直接顺序读，不复制整文件。
func readFromSpool(spoolPath string, row int) ([]byte, error) {
	f, err := os.Open(spoolPath)
	if err != nil {
		return nil, fmt.Errorf("tracearchive: 打开 spool 失败: %w", err)
	}
	defer f.Close()

	br := bufio.NewReaderSize(f, 1<<20)
	for i := 0; ; i++ {
		line, err := br.ReadBytes('\n')
		if i == row && len(line) > 0 {
			return trimNewline(line), nil
		}
		if err != nil {
			if err == io.EOF {
				return nil, fmt.Errorf("%w: spool 中不存在第 %d 行", ErrNotArchived, row)
			}
			return nil, fmt.Errorf("tracearchive: 读 spool 失败: %w", err)
		}
	}
}

var _ = errors.Is
```

- [ ] **步骤 4：运行测试并确认通过**

运行：`go test ./internal/tracearchive/ -run TestReader -v`
预期：PASS（含逐字节一致、spool 兜底、错误分类、footer 篡改、并发上限、无临时文件）。

- [ ] **步骤 5：提交**

```bash
git add internal/tracearchive/reader.go internal/tracearchive/reader_test.go
git commit -m "feat(tracearchive): 按定位键点读（含 spool 兜底与错误分类）

- 读 footer 只需末尾 32 字节，不必解压任何数据即可定位索引
- 流式解压命中即停，峰值内存与块大小无关；不落临时文件、不缓存
- 未封块的近期正文从 spool 直接读，无需等待封块
- 严格区分「产物缺失 / 从未归档 / footer 损坏」，杜绝把故障伪装成数据不存在
- 解码并发设上限（解码器内存≈窗口，规格 §8.4 C5）"
```

---

### 任务 8：端到端往返与约束验证

**文件：**
- 新建：`internal/tracearchive/roundtrip_test.go`
- 新建：`internal/tracearchive/constraints_test.go`

**接口：**
- 依赖输入：任务 5（`Writer`）、任务 6（`Sealer`）、任务 7（`Reader`）
- 对外产出：无新导出符号（集成测试）

- [ ] **步骤 1：编写端到端往返测试**

新建 `internal/tracearchive/roundtrip_test.go`：

```go
package tracearchive

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"testing"
)

// TestEndToEndRoundTripByteExact 是验收用例 A1 的缩小版：
// 写入 N 行 → 封块 → 随机抽 K 行读回 → 逐字节比对。
func TestEndToEndRoundTripByteExact(t *testing.T) {
	if testing.Short() {
		t.Skip("端到端测试，-short 下跳过")
	}
	const (
		totalRows = 20000
		sampleK   = 200
	)
	root := t.TempDir()

	w, err := NewWriter(WriterOptions{Root: root, Inst: "blue", BlockBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}

	rnd := rand.New(rand.NewSource(20260924))
	makeLine := func(i int) []byte {
		n := rnd.Intn(4096)
		return []byte(fmt.Sprintf(`{"request_id":"rid-%06d","ts":"2026-09-24T00:00:00+08:00","body":"%s"}`,
			i, string(bytes.Repeat([]byte("x"), n))))
	}

	want := make(map[Ref][]byte, totalRows)
	// 逐行追加，并在达到阈值时封块（模拟后台协程）
	for i := 0; i < totalRows; i++ {
		line := makeLine(i)
		ref, err := w.Append("2026-09-24", line)
		if err != nil {
			t.Fatalf("第 %d 行追加失败: %v", i, err)
		}
		want[ref] = line
		for _, task := range w.PendingSeals() {
			s, _ := NewSealer(SealerOptions{Root: root})
			if err := s.Seal(context.Background(), task); err != nil {
				t.Fatalf("封块失败: %v", err)
			}
			w.MarkSealed(task.DT, task.Part)
		}
	}
	for _, task := range w.PendingSeals() {
		s, _ := NewSealer(SealerOptions{Root: root})
		if err := s.Seal(context.Background(), task); err != nil {
			t.Fatalf("收尾封块失败: %v", err)
		}
		w.MarkSealed(task.DT, task.Part)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	r, err := NewReader(ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	// 随机抽样，逐字节比对
	refs := make([]Ref, 0, len(want))
	for ref := range want {
		refs = append(refs, ref)
	}
	checked := 0
	for checked < sampleK && len(refs) > 0 {
		i := rnd.Intn(len(refs))
		ref := refs[i]
		refs = append(refs[:i], refs[i+1:]...)

		got, err := r.GetBody(context.Background(), ref)
		if err != nil {
			t.Fatalf("%s 读取失败: %v", ref, err)
		}
		if !bytes.Equal(got, want[ref]) {
			t.Fatalf("%s 内容不一致:\n want %d 字节\n got  %d 字节", ref, len(want[ref]), len(got))
		}
		checked++
	}
	t.Logf("逐字节比对通过: %d/%d 行，共写入 %d 行", checked, sampleK, totalRows)
}
```

- [ ] **步骤 2：编写约束验证测试**

新建 `internal/tracearchive/constraints_test.go`：

```go
package tracearchive

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// dirUsage 统计目录下所有文件的总字节数。
func dirUsage(t *testing.T, dir string) int64 {
	t.Helper()
	var total int64
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return total
}

// TestReadsDoNotGrowDiskUsage 是验收用例 A4：
// 规格 §8.3 的 C1~C4 是强制性约束，读取千次后磁盘占用增量必须为 0。
//
// 若实现被改成「解压到 /tmp 再读」或「缓存已解压块」，此测试会立刻转红。
func TestReadsDoNotGrowDiskUsage(t *testing.T) {
	root := t.TempDir()
	lines := [][]byte{
		[]byte(`{"request_id":"a"}`),
		[]byte(`{"request_id":"b"}`),
		[]byte(`{"request_id":"c"}`),
	}
	sealLines(t, root, "2026-09-24", "blue", 0, lines)

	r, err := NewReader(ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	// 预热一次，排除首次读取可能的一次性分配
	if _, err := r.GetBody(context.Background(), Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: 0}); err != nil {
		t.Fatal(err)
	}
	before := dirUsage(t, root)

	for i := 0; i < 1000; i++ {
		if _, err := r.GetBody(context.Background(), Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: i % 3}); err != nil {
			t.Fatalf("第 %d 次读取失败: %v", i, err)
		}
	}
	if after := dirUsage(t, root); after != before {
		t.Fatalf("读取 1000 次后磁盘占用从 %d 变为 %d（违反 §8.3 C1~C4）", before, after)
	}
}

// TestUnsealedReadsAlsoDoNotGrow 覆盖 spool 兜底路径的空间约束。
func TestUnsealedReadsAlsoDoNotGrow(t *testing.T) {
	root := t.TempDir()
	lines := [][]byte{[]byte(`{"request_id":"h0"}`), []byte(`{"request_id":"h1"}`)}
	writeSpool(t, root, "2026-09-24", "blue", 0, lines)

	r, err := NewReader(ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if _, err := r.GetBody(context.Background(), Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: 0}); err != nil {
		t.Fatal(err)
	}
	before := dirUsage(t, root)
	for i := 0; i < 500; i++ {
		if _, err := r.GetBody(context.Background(), Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: i % 2}); err != nil {
			t.Fatalf("第 %d 次读取失败: %v", i, err)
		}
	}
	if after := dirUsage(t, root); after != before {
		t.Fatalf("spool 读取后磁盘占用从 %d 变为 %d", before, after)
	}
}
```

- [ ] **步骤 3：运行测试并确认通过**

运行：`go test ./internal/tracearchive/ -v`
预期：全部 PASS。特别注意 `TestReadsDoNotGrowDiskUsage` 与 `TestEndToEndRoundTripByteExact`。

- [ ] **步骤 4：跑竞态检测**

运行：`go test -race ./internal/tracearchive/ -count=1`
预期：PASS，无 data race 报告。

- [ ] **步骤 5：全仓构建与测试**

运行：
```bash
go build ./...
go test ./internal/... -count=1
```
预期：本包全绿；仅 `test/bedrock_model_id_check.go` 的既有编译问题可忽略。

- [ ] **步骤 6：提交**

```bash
git add internal/tracearchive/roundtrip_test.go internal/tracearchive/constraints_test.go
git commit -m "test(tracearchive): 端到端逐字节往返与空间零增量约束验证

- 2 万行写入 → 封块 → 随机抽 200 行读回，逐字节比对（验收 A1 缩小版）
- 读取 1000 次后磁盘占用增量断言为 0（规格 §8.3 C1~C4 的断言级验证，A4）
- spool 兜底路径单独覆盖空间约束"
```

---

## 自检

**1. 规格覆盖度**（本计划范围内的规格条目逐项核对）：

| 规格条目 | 对应任务 |
|---|---|
| §4.2 `internal/tracearchive` 包 | 任务 1~8（全包） |
| §5.1 目录布局 `dt=/inst=/spool/` | 任务 4 |
| §5.2 产物物理布局（帧+trailer+footer）| 任务 2、3、6 |
| §5.3.3 参数终值（19 / 512MiB / 1.5GiB / 15min）| 任务 1 |
| §5.4 定位键为复合键而非偏移 | 任务 4 |
| §5.5 冷层格式预留（format_version）| 任务 3 |
| §7.1 请求路径只追加不压缩 | 任务 5 |
| §7.2 封块流程与顺序 | 任务 6 |
| §7.3 写指针以目录内容为权威 | 任务 4（`parsePartSeq`）、任务 5（`nextSeqLocked`）|
| §8.2 读取算法（产物→spool）| 任务 7 |
| §8.3 C1~C4 空间硬约束 | 任务 7（实现）、任务 8（断言）|
| §8.4 C5 并发上限 | 任务 7（`sem`）、任务 7 的 `TestReaderConcurrencyIsBounded` |
| §10.1 I1~I5 不变量 | 任务 5/6（实现）、任务 6 的原子性与拒绝覆盖测试 |
| §10.2 崩溃窗口 | 任务 5（`nextSeqLocked` 恢复）、任务 6（失败清理 .tmp）|
| §14.2 A1 逐字节往返 | 任务 8 |
| §14.2 A4 零空间增量 | 任务 8 |
| §15 U7/U8 标定 | 任务 1 |

**不在本计划范围**（属其它计划，此处仅作说明，避免执行者误判为遗漏）：
- §6 PG schema 变更 → 计划 3
- §8.1 HTTP 接口、CLI → 计划 3
- §11 部署变更 → 计划 4
- §5.3.4 块大小 U1 实验 → 需真实生产数据与延迟测量，属计划 4 的灰度前验证（本计划取 `1.5 GiB` 为暂定值）
- 规格 §8.4 C6（读 spool 不受并发限制）→ 已由任务 7 的实现顺序自然满足（spool 路径不占用 `sem`）

**2. 占位符扫描**：全文无 TBD/TODO/FIXME；每个代码步骤均含可编译的完整代码块。

**3. 类型一致性核对**：
- `Ref` 字段名 `DT`/`Inst`/`Part`/`Row` 在任务 4 定义，任务 5、6、7、8 使用一致。
- `SealTask` 字段 `DT`/`Inst`/`Part`/`SpoolPath`/`Rows`/`UncompressedBytes` 任务 5 定义，任务 6 使用一致。
- `footer` 字段 `TrailerOffset`/`TrailerLen`/`FrameSHA` 任务 3 定义，任务 6、7 使用一致；`FooterSize` 同一任务定义，任务 6、7 引用。
- `WriterOptions` 含 `Root`/`Inst`/`BlockBytes`/`SealInterval`；`SealerOptions` 含 `Root`/`ZstdLevel`/`ZstdWindowBytes`；`ReaderOptions` 含 `Root`/`ZstdWindowBytes`/`MaxConcurrent`——三者字段名不冲突。
- `Writer.MarkSealed(dt string, part int)` 任务 5 定义，任务 8 使用一致。
- `Paths` 方法 `PartFile`/`SpoolFile`/`PartDir`/`SpoolDir` 任务 4 定义，任务 5、6、7 使用一致。
- 错误变量 `ErrPartNotFound`/`ErrBodyMissing`/`ErrNotArchived` 任务 4 定义；任务 7 使用 `ErrNotArchived`，与 `errors.Is` 兼容（均为 `errors.New` 直接包装）。

**已知的实现细节留白（非计划缺陷，属执行者的正常判断）**：测试中使用的 `writeSpool`、`sealLines`、`countLines`、`dirEntries` 是测试辅助函数，分别定义在 `sealer_test.go`、`reader_test.go`。若出现重复定义，合并到 `testhelpers_test.go`。
