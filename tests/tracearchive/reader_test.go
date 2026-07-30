package tracearchive_test

// 本文件是 B 层测试：按定位键点读（任务 7）的公开行为断言。
//
// 分层：全部断言只用公开 API（ta.NewReader/ReaderOptions/GetBody/Close/FooterSize/
// ta.NewPaths/ta.Ref/Err*），产物字节布局的解析沿用 Task 6 在 B 层既有的做法
//（按规格 §5.2 的契约读 footer 字段，见 tests/tracearchive/sealer_test.go）。
//
// 只有两条断言语义上无法用公开 API 表达，按全局约束放在 A 层
//（internal/tracearchive/reader_internal_test.go）并在那里写明理由：
//   - trailer_len 上界用的未导出常量 maxTrailerBytes（边界值就是它本身，不能内联字面量）；
//   - 「并发槽位完全释放」要读未导出的 r.inFlight()。
//
// 辅助函数（sealLines/writeSpool/requireZstd）在 helpers_test.go。

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	ta "github.com/example/llm-gateway/internal/tracearchive"
)

func TestReaderReturnsExactLine(t *testing.T) {
	root := t.TempDir()
	lines := [][]byte{
		[]byte(`{"request_id":"r0","body":"短"}`),
		[]byte(`{"request_id":"r1","body":"` + string(bytes.Repeat([]byte("x"), 100000)) + `"}`),
		[]byte(`{"request_id":"r2","body":"结尾"}`),
	}
	sealLines(t, root, "2026-09-24", "blue", 0, lines)

	r, err := ta.NewReader(ta.ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	for i, want := range lines {
		got, err := r.GetBody(context.Background(), ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: i})
		if err != nil {
			t.Fatalf("第 %d 行读取失败: %v", i, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("第 %d 行不一致:\n want %d 字节 %q\n got  %d 字节 %q",
				i, len(want), trunc([]byte(want)), len(got), trunc(got))
		}
	}
}

// trunc 只用于失败信息：把超过 80 字节的行截断显示，避免一条 100 KB 的正文
// 把测试输出刷满（本用例第一条断言就是逐字节比对，失败信息只需够判断）。
func trunc(b []byte) []byte {
	const n = 80
	if len(b) <= n {
		return b
	}
	return append(append([]byte{}, b[:n]...), "…(截断)"...)
}

func TestReaderFallsBackToSpoolForUnsealedRows(t *testing.T) {
	root := t.TempDir()
	// 只写 spool，不封块：模拟「近期正文尚未封块」
	lines := [][]byte{[]byte(`{"request_id":"hot0"}`), []byte(`{"request_id":"hot1"}`)}
	writeSpool(t, root, "2026-09-24", "blue", 0, lines)

	r, err := ta.NewReader(ta.ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	got, err := r.GetBody(context.Background(), ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: 1})
	if err != nil {
		t.Fatalf("未封块的行应从 spool 读到: %v", err)
	}
	if !bytes.Equal(got, lines[1]) {
		t.Fatalf("spool 兜底读到错误内容: %q", got)
	}
}

// TestReaderReportsBodyMissingForMarkerLine 锁定标记行的读取语义（规格 §8.2）：
// Task 5 的 AppendFailed 会写一行 null（正文从未落盘），两条读取路径
// **都必须**把它映射成 ErrBodyMissing，而不是把字符串 "null" 当正文返回 ——
// 后者会把「正文从未落盘」伪装成「正文就是 null」，与 body_missing 的 404 语义冲突。
//
// ⚠️ 本用例是**环用例**（part 0/1 两圈）：按全局约束 D15，环在第一个失败处停止，
// 因此它只能证明「环里有东西失败了」，不能证明「新增的那一圈承重」。两条路径各自的
// 隔离探针见下面的 TestReaderMarkerOnlyArtifactIsBodyMissing /
// TestReaderMarkerOnlySpoolIsBodyMissing；本用例保留为两条路径的端到端回归网
// （含「按行序取行没有被标记行带偏」的反向断言）。
func TestReaderReportsBodyMissingForMarkerLine(t *testing.T) {
	root := t.TempDir()
	// 标记行两侧都有真实正文，用来同时证明「按行序取行」没有被标记行带偏
	lines := [][]byte{[]byte(`{"request_id":"a"}`), []byte("null"), []byte(`{"request_id":"c"}`)}

	// ① 产物路径：spool 封块后，标记行进了产物
	sealLines(t, root, "2026-09-24", "blue", 0, lines)
	// ② spool 路径：同日同实例的另一个块不封块，直接兜底读
	writeSpool(t, root, "2026-09-24", "blue", 1, lines)

	r, err := ta.NewReader(ta.ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	for _, part := range []int{0, 1} {
		_, err := r.GetBody(context.Background(), ta.Ref{DT: "2026-09-24", Inst: "blue", Part: part, Row: 1})
		if !errors.Is(err, ta.ErrBodyMissing) {
			t.Fatalf("part=%d 的标记行必须返回 ErrBodyMissing，实际 %v", part, err)
		}
		// 反向断言：标记行前后的真实正文必须照常读到，否则「一律报 missing」也会全绿
		for _, tc := range []struct {
			row  int
			want []byte
		}{{0, lines[0]}, {2, lines[2]}} {
			got, err := r.GetBody(context.Background(), ta.Ref{DT: "2026-09-24", Inst: "blue", Part: part, Row: tc.row})
			if err != nil {
				t.Fatalf("part=%d row=%d 应正常读到: %v", part, tc.row, err)
			}
			if !bytes.Equal(got, tc.want) {
				t.Fatalf("part=%d row=%d 内容不符: want %q, got %q", part, tc.row, tc.want, got)
			}
		}
	}
}

// TestReaderMarkerOnlyArtifactIsBodyMissing 是「产物路径识别标记行」的**隔离探针**。
//
// 存在的理由（全局约束 D15）上半条用例是环用例：环在第一个失败处停止，摘掉 spool 路径的
// 识别时它会 FAIL 于 part=1，而摘掉产物路径的识别时 FAIL 于 part=0 —— 但两条断言的
// 正确性互相遮蔽（环里更早的条目会抢走判决）。本探针只含一条条目：产物里只有一行，
// 且那一行就是标记行。
func TestReaderMarkerOnlyArtifactIsBodyMissing(t *testing.T) {
	root := t.TempDir()
	sealLines(t, root, "2026-09-24", "blue", 0, [][]byte{[]byte("null")})

	r, err := ta.NewReader(ta.ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	got, err := r.GetBody(context.Background(), ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: 0})
	if err == nil {
		t.Fatalf("产物里的标记行必须报 ErrBodyMissing，却把 %q 当正文返回了", got)
	}
	if !errors.Is(err, ta.ErrBodyMissing) {
		t.Fatalf("产物里的标记行应返回 ErrBodyMissing（404），实际 %v", err)
	}
	// 反向：不得归入「存储状态异常」那类 500 身份——两者对调用方的语义不同
	if errors.Is(err, ta.ErrNotArchived) {
		t.Fatalf("标记行是 body_missing（404），不得同时携带 ErrNotArchived（500）身份: %v", err)
	}
}

// TestReaderMarkerOnlySpoolIsBodyMissing 是「spool 路径识别标记行」的隔离探针，
// 与上一条同源（理由见该用例与 D15）。
func TestReaderMarkerOnlySpoolIsBodyMissing(t *testing.T) {
	root := t.TempDir()
	writeSpool(t, root, "2026-09-24", "green", 0, [][]byte{[]byte("null")})

	r, err := ta.NewReader(ta.ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	got, err := r.GetBody(context.Background(), ta.Ref{DT: "2026-09-24", Inst: "green", Part: 0, Row: 0})
	if err == nil {
		t.Fatalf("spool 里的标记行必须报 ErrBodyMissing，却把 %q 当正文返回了", got)
	}
	if !errors.Is(err, ta.ErrBodyMissing) {
		t.Fatalf("spool 里的标记行应返回 ErrBodyMissing（404），实际 %v", err)
	}
	if errors.Is(err, ta.ErrNotArchived) {
		t.Fatalf("标记行是 body_missing（404），不得同时携带 ErrNotArchived（500）身份: %v", err)
	}
}

// TestReaderDoesNotTreatNullSubstringAsMarker 锁住标记行判据的**下界**：
// 真实正文里出现 "null" 子串（合法 JSON，例如 {"body":"values are null"}）必须原样返回。
//
// 为什么必须有这条：判据只能比 trim 掉换行后的**整行**是否等于 "null"（Task 5 的
// failedRowMarker）。若为了「更稳」把它放宽成 bytes.Contains / 前缀匹配，
// 就会把真实正文误报成 body_missing（404）——那是与 R10 第 3 项方向相反的一类静默错误。
func TestReaderDoesNotTreatNullSubstringAsMarker(t *testing.T) {
	root := t.TempDir()
	body := []byte(`{"request_id":"n1","body":"values are null"}`)
	// 两条路径都要验：产物路径（part 0）与 spool 路径（part 1）
	sealLines(t, root, "2026-09-24", "blue", 0, [][]byte{body})
	writeSpool(t, root, "2026-09-24", "blue", 1, [][]byte{body})

	r, err := ta.NewReader(ta.ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	for _, part := range []int{0, 1} {
		got, err := r.GetBody(context.Background(), ta.Ref{DT: "2026-09-24", Inst: "blue", Part: part, Row: 0})
		if err != nil {
			t.Fatalf("part=%d 的正文只是含 null 子串，不该报错: %v", part, err)
		}
		if !bytes.Equal(got, body) {
			t.Fatalf("part=%d 正文被改写: want %q, got %q", part, body, got)
		}
	}
}

// TestReaderRejectsUnterminatedSpoolTail 覆盖 R10 第 1 项（规格 §8.2 第 4 步）：
// spool 末行未以换行符结尾 ⇒ **拒绝读取**，不得 trim 掉残字节后当正文返回。
//
// 【隔离探针】只含这一条守卫的条目（简报里 readFromSpool 的代码块没有它）。
// 为什么这是真正的静默错正文路径：残行占掉一个行位，其后的行整体错位
// ⇒ row=N 读到第 N+1 行的正文；而且短写留下的残行可能只是真实正文的**前缀**，
// 返回它就等于静默返回被截断的正文。
//
// 【拒绝范围】只拒绝「被请求的那一行恰是残行」的情况，不拒绝整份 spool：
// 残行恒是文件的最后一条物理行（写失败会毒化并换 part，重启也一定开新 part），
// 它之前的完整行与 row_seq 一一对应；若整份拒绝，活跃 spool（1 MiB bufio 在两次
// flush 之间天然停在行中间）的正常读取会被误伤。②就是这条正向对照。
func TestReaderRejectsUnterminatedSpoolTail(t *testing.T) {
	root := t.TempDir()
	spool := ta.NewPaths(root).SpoolFile(ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0})
	if err := os.MkdirAll(filepath.Dir(spool), 0o755); err != nil {
		t.Fatal(err)
	}
	// 两个完整行 + 一个没有换行收尾的残行（写入侧短写/崩溃后的真实形态）。
	// 直接写文件而不经 writeSpool：writeSpool 会给每一行补 '\n'，表达不出残行。
	raw := []byte("{\"row\":0}\n{\"row\":1}\n{\"row\":2") // 末行故意不带 '\n'
	if err := os.WriteFile(spool, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := ta.NewReader(ta.ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	// ① 直接命中残行：没有守卫时会返回 trim 后的 `{"row":2`（静默错正文）
	got, err := r.GetBody(context.Background(), ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: 2})
	if err == nil {
		t.Fatalf("末行未以换行结尾时必须拒绝读取，却把残行当正文返回了: %q", got)
	}
	if !errors.Is(err, ta.ErrNotArchived) {
		t.Fatalf("残行属「存储状态异常」（ErrNotArchived/500 类），实际 %v", err)
	}
	if errors.Is(err, ta.ErrBodyMissing) {
		t.Fatalf("残行不是 body_missing（正文并非写入时即失败），不得归入 404 语义: %v", err)
	}

	// ② 正向对照：残行之前的完整行必须**照常读到**——守卫不得退化成「整份 spool 一律拒绝」，
	// 否则会成为「一律拒绝」也会全绿的假证据，而且会误伤活跃 spool 的正常读取。
	got0, err := r.GetBody(context.Background(), ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: 0})
	if err != nil {
		t.Fatalf("残行之前的完整行（row=0）应照常读到，实际 %v", err)
	}
	if want := []byte(`{"row":0}`); !bytes.Equal(got0, want) {
		t.Fatalf("row=0 内容不符: want %q, got %q", want, got0)
	}
}

// TestReaderVerifiesFrameSHA 覆盖 R10 第 2 项（规格 §8.2 第 3 步）：
// 读取产物时必须重算 zstd 帧字节（[0, trailer_offset)）的 sha256[:8] 并与 footer 比对；
// 不符 ⇒ ErrFooterCorrupt（500 + 诊断），不得静默返回「不存在」，更不能把位腐后的
// 字节当正文解出来。
//
// 【隔离探针】footer 的 frame_sha256 字段此前只写不读（Task 6 只负责写），
// 因此这是唯一能把「位腐/半写」变成响亮失败的守卫。
func TestReaderVerifiesFrameSHA(t *testing.T) {
	root := t.TempDir()
	lines := [][]byte{
		[]byte(`{"request_id":"s0","body":"` + string(bytes.Repeat([]byte("a"), 4096)) + `"}`),
		[]byte(`{"request_id":"s1","body":"tail"}`),
	}
	sealLines(t, root, "2026-09-24", "blue", 0, lines)

	partPath := ta.NewPaths(root).PartFile(ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0})
	b, err := os.ReadFile(partPath)
	if err != nil {
		t.Fatal(err)
	}
	fo := len(b) - ta.FooterSize
	trailerOff := int64(binary.BigEndian.Uint64(b[fo+6 : fo+14]))
	if trailerOff < 8 {
		t.Fatalf("自检失败：帧区仅 %d 字节，改不出位腐", trailerOff)
	}
	// 只改帧区的**一个字节**（不动 trailer/footer）：footer 里的 frame_sha256 于是与
	// 真实帧字节不再一致。这正是「半写 / 位腐」的形态。
	b[trailerOff-1] ^= 0x01
	if err := os.WriteFile(partPath, b, 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := ta.NewReader(ta.ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	_, err = r.GetBody(context.Background(), ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: 0})
	if !errors.Is(err, ta.ErrFooterCorrupt) {
		t.Fatalf("帧字节被改而 footer 的 frame_sha256 未更新时必须返回 ErrFooterCorrupt（产物损坏），实际 %v", err)
	}
}

// TestReaderVerifiesFrameSHABeyondDecodedRegion 是 R10 第 2 项的**判别性**探针：
// 它证明 frame_sha256 校验的独有价值——能发现「点读这次根本不会解到」的帧内位腐。
//
// 【为什么上一条用例还不够】上一条改的是**帧的最后一个字节**、而帧区只有几 KB，
// 于是 Reader 的 1 MiB bufio 会向解码器一次要满 1 MiB，解码器顺势把整帧解完，
// zstd 自身的内容校验和（CRC）也就报错了。此时删掉 frame_sha256 校验，读取仍然失败
// （只是错误身份变成「解压读取失败: CRC check failed」）——守卫的独有价值没被证明。
//
// 本用例把帧区做到远大于 1 MiB 读缓冲（3 MiB 不可压缩数据 ⇒ 压缩后仍 ~3 MiB，
// 约 24 个 zstd 块），并只改**最后一个字节**：row=0 的点读只会解到前 ~1 MiB，
// 最后一个块永远不会被解码，CRC 也就永远不触发。因此：
//   - 有 frame_sha256 校验 ⇒ ErrFooterCorrupt（覆盖整个帧区，与解到哪无关）；
//   - 删掉校验 ⇒ 读取**成功**（静默接受位腐产物）。
//
// 这正是「该字段只写不读 = 完整性检查从未执行」要消灭的形态。
func TestReaderVerifiesFrameSHABeyondDecodedRegion(t *testing.T) {
	root := t.TempDir()
	const rowBytes = 64 << 10
	const rows = 48 // 3 MiB
	blob := incompressibleBlob(rows * rowBytes)
	lines := make([][]byte, 0, rows)
	for i := 0; i < rows; i++ {
		lines = append(lines, blob[i*rowBytes:(i+1)*rowBytes])
	}
	sealLines(t, root, "2026-09-24", "blue", 0, lines)

	partPath := ta.NewPaths(root).PartFile(ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0})
	b, err := os.ReadFile(partPath)
	if err != nil {
		t.Fatal(err)
	}
	fo := len(b) - ta.FooterSize
	trailerOff := int64(binary.BigEndian.Uint64(b[fo+6 : fo+14]))
	// 用例自检：帧区必须显著大于 Reader 的 1 MiB 读缓冲，否则「解码器走不到位腐处」
	// 这个前提不成立，本用例就退化成了上一条（自证失败比假绿好）。
	if trailerOff < 2<<20 {
		t.Fatalf("用例自检失败：帧区只有 %d 字节，未超过 Reader 的 1 MiB 读缓冲（需 ≥ 2 MiB）", trailerOff)
	}
	b[trailerOff-1] ^= 0x01
	if err := os.WriteFile(partPath, b, 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := ta.NewReader(ta.ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	_, err = r.GetBody(context.Background(), ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: 0})
	if err == nil {
		t.Fatal("帧尾位腐被静默接受了：读 row=0 竟然成功——frame_sha256 校验没有生效")
	}
	if !errors.Is(err, ta.ErrFooterCorrupt) {
		t.Fatalf("帧尾位腐必须由 frame_sha256 校验报成 ErrFooterCorrupt（解码器走不到那里，不会报 CRC），实际 %v", err)
	}
}

// incompressibleBlob 返回 n 字节的确定性伪随机字节，且**不含 '\n'/'\r'**
// （spool 的每一行都以 '\n' 分隔，正文里出现裸换行会改变行数）。
// 用 SHA-256 链产生：熵接近 8 bit/字节，压缩后几乎不缩水，因此帧区大小可控。
func incompressibleBlob(n int) []byte {
	out := make([]byte, 0, n+32)
	sum := sha256.Sum256([]byte("task-7 frame_sha probe"))
	for len(out) < n {
		sum = sha256.Sum256(sum[:])
		out = append(out, sum[:]...)
	}
	out = out[:n]
	for i, c := range out {
		switch c {
		case '\n':
			out[i] = 0x0b
		case '\r':
			out[i] = 0x0c
		}
	}
	return out
}

func TestReaderDistinguishesMissingFromNotArchived(t *testing.T) {
	root := t.TempDir()
	sealLines(t, root, "2026-09-24", "blue", 0, [][]byte{[]byte("only")})

	r, _ := ta.NewReader(ta.ReaderOptions{Root: root})
	defer r.Close()

	// 块完全不存在（既无产物也无 spool）→ ErrNotArchived
	_, err := r.GetBody(context.Background(), ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 99, Row: 0})
	if !errors.Is(err, ta.ErrNotArchived) {
		t.Fatalf("两者都不存在时应返回 ErrNotArchived，实际 %v", err)
	}

	// 行号越界 → 明确报错，不得静默返回空
	_, err = r.GetBody(context.Background(), ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: 5})
	if !errors.Is(err, ta.ErrNotArchived) {
		t.Fatalf("产物内行号越界应返回 ErrNotArchived，实际 %v", err)
	}

	// spool 内行号越界 → 同样必须是 ErrNotArchived（readFromSpool 的守卫）
	spoolOnly := writeSpool(t, root, "2026-09-24", "green", 0, [][]byte{[]byte("only-one-row")})
	if spoolOnly == "" {
		t.Fatal("前置失败")
	}
	_, err = r.GetBody(context.Background(), ta.Ref{DT: "2026-09-24", Inst: "green", Part: 0, Row: 9})
	if !errors.Is(err, ta.ErrNotArchived) {
		t.Fatalf("spool 内行号越界应返回 ErrNotArchived，实际 %v", err)
	}
}

// TestReaderRejectsNegativeRow 覆盖行号越界检查的**下界**（ref.Row < 0）。
//
// 为什么单独一条：负的 row 只可能来自元数据损坏，而检查一旦缺失，产物路径会直接
// offsets[-1] 越界 panic（读取进程崩溃 = 比错误更糟的失败形态）。这半条守卫在
// TestReaderDistinguishesMissingFromNotArchived 里没有任何条目覆盖。
// 【隔离探针】只含这一条断言。
func TestReaderRejectsNegativeRow(t *testing.T) {
	root := t.TempDir()
	sealLines(t, root, "2026-09-24", "blue", 0, [][]byte{[]byte("only")})

	r, err := ta.NewReader(ta.ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	_, err = r.GetBody(context.Background(), ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: -1})
	if !errors.Is(err, ta.ErrNotArchived) {
		t.Fatalf("负行号必须被拒绝（ErrNotArchived），实际 %v", err)
	}
}

func TestReaderRejectsCorruptFooter(t *testing.T) {
	root := t.TempDir()
	sealLines(t, root, "2026-09-24", "blue", 0, [][]byte{[]byte("data")})

	partPath := ta.NewPaths(root).PartFile(ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0})
	b, err := os.ReadFile(partPath)
	if err != nil {
		t.Fatal(err)
	}
	b[len(b)-ta.FooterSize] ^= 0xFF // 篡改 magic
	if err := os.WriteFile(partPath, b, 0o644); err != nil {
		t.Fatal(err)
	}

	r, _ := ta.NewReader(ta.ReaderOptions{Root: root})
	defer r.Close()
	_, err = r.GetBody(context.Background(), ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: 0})
	if !errors.Is(err, ta.ErrFooterCorrupt) {
		t.Fatalf("footer 被篡改时应返回 ErrFooterCorrupt 而非「不存在」，实际 %v", err)
	}
}

// TestReaderRejectsTruncatedArtifact 覆盖「文件装不下 footer」这条前置校验。
//
// 为什么单独一条：产物被截断到 32 字节以下时，读取端必须先于任何 ReadAt(负偏移)
// 拒绝，并把身份定为「产物损坏」（ErrFooterCorrupt）而不是「不存在」——
// 后者会把存储故障伪装成业务事实。
func TestReaderRejectsTruncatedArtifact(t *testing.T) {
	root := t.TempDir()
	partPath := ta.NewPaths(root).PartFile(ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0})
	if err := os.MkdirAll(filepath.Dir(partPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(partPath, []byte("太短"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := ta.NewReader(ta.ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	_, err = r.GetBody(context.Background(), ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: 0})
	if !errors.Is(err, ta.ErrFooterCorrupt) {
		t.Fatalf("产物装不下 footer 时必须返回 ErrFooterCorrupt，实际 %v", err)
	}
}

// TestReaderRejectsTrailerOffsetNotAtLineStart 覆盖「trailer 记录的偏移必须落在行首」
// 这条兜底：把 trailer 的第 2 个差分 varint 改成一个小值，使 row=1 的偏移指向
// 第 0 行的**行中间**。此时流式解压永远累计不到该偏移 ⇒ 必须响亮报错，
// 不得把「上一次读到的行」当成结果返回（那正是静默错正文）。
//
// 【本用例覆盖不到什么（如实登记）】footer/trailer 区域**没有校验和**
// （footer 的 frame_sha256 只覆盖 [0, trailer_offset) 的帧字节）。因此若把 trailer
// 改成「指向另一个**行首**」的偏移，读取端无法察觉，会静默返回错误的行——
// 这是本格式已知的残余风险，已写进任务 7 报告。
func TestReaderRejectsTrailerOffsetNotAtLineStart(t *testing.T) {
	root := t.TempDir()
	// 第 0 行的长度取在 8 < len < 120：这样 trailer 的两个差分 varint（偏移 0 与
	// len(line0)+1 < 128）各占 1 字节，trailer 区恰好 2 字节，可以直接改第 2 个 varint。
	prefix := `{"request_id":"off0","pad":"`
	line0 := []byte(prefix + string(bytes.Repeat([]byte("p"), 60)) + `"}`)
	if len(line0) <= 8 || len(line0) >= 120 {
		t.Fatalf("用例自检失败：line0 长度 %d 不满足 8 < len < 120", len(line0))
	}
	lines := [][]byte{line0, []byte(`{"request_id":"off1"}`)}
	sealLines(t, root, "2026-09-24", "blue", 0, lines)

	partPath := ta.NewPaths(root).PartFile(ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0})
	b, err := os.ReadFile(partPath)
	if err != nil {
		t.Fatal(err)
	}
	fo := len(b) - ta.FooterSize
	trailerOff := int64(binary.BigEndian.Uint64(b[fo+6 : fo+14]))
	if got := int32(binary.BigEndian.Uint32(b[fo+14 : fo+18])); got != 2 {
		t.Fatalf("用例自检失败：trailer 应为 2 字节（两个 1 字节 varint），实际 %d", got)
	}
	if b[trailerOff] != 0x00 {
		t.Fatalf("用例自检失败：trailer 首字节应为偏移 0，实际 0x%02x", b[trailerOff])
	}
	b[trailerOff+1] = 0x05 // 差分 5 ⇒ row=1 的偏移 = 5，落在第 0 行中间
	if err := os.WriteFile(partPath, b, 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := ta.NewReader(ta.ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	_, err = r.GetBody(context.Background(), ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: 1})
	if !errors.Is(err, ta.ErrNotArchived) {
		t.Fatalf("trailer 偏移未命中行首时必须报 ErrNotArchived（不得返回别的行），实际 %v", err)
	}
}

// TestReaderRejectsTrailerNotAdjacentToFooter 覆盖「trailer 必须紧邻 footer」的一致性校验。
//
// 【构造方式（为什么这不算「测试造假」）】把 footer 的 trailer_offset 改小 1 字节，
// 并**同步重算** frame_sha256[:8] 覆盖新的 [0, trailer_offset) —— 于是产物在
// 「帧摘要」「trailer_len」「row_count」三方面都自洽，唯一不成立的就是
// trailer_offset + trailer_len == size - FooterSize。
// 这样断言 ErrFooterCorrupt 才真的落在邻接校验上：不重算摘要的话，删掉邻接校验后
// 摘要校验会抢先报同一个错误，用例照样绿、变异杀不死（断言被别的守卫遮蔽）。
func TestReaderRejectsTrailerNotAdjacentToFooter(t *testing.T) {
	root := t.TempDir()
	sealLines(t, root, "2026-09-24", "blue", 0, [][]byte{[]byte("adj-0"), []byte("adj-1")})

	partPath := ta.NewPaths(root).PartFile(ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0})
	b, err := os.ReadFile(partPath)
	if err != nil {
		t.Fatal(err)
	}
	fo := len(b) - ta.FooterSize
	trailerOff := int64(binary.BigEndian.Uint64(b[fo+6 : fo+14]))
	if trailerOff < 8 {
		t.Fatalf("自检失败：帧区仅 %d 字节，改不出「自洽但错位」的 footer", trailerOff)
	}
	newOff := trailerOff - 1
	binary.BigEndian.PutUint64(b[fo+6:fo+14], uint64(newOff))
	sum := sha256.Sum256(b[:newOff])
	copy(b[fo+22:fo+30], sum[:8])
	if err := os.WriteFile(partPath, b, 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := ta.NewReader(ta.ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	_, err = r.GetBody(context.Background(), ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: 0})
	if !errors.Is(err, ta.ErrFooterCorrupt) {
		t.Fatalf("trailer_offset 与文件大小不再自洽时必须返回 ErrFooterCorrupt，实际 %v", err)
	}
}

// TestReaderRejectsTrailerRowCountMismatch 覆盖「footer 的 row_count 与 trailer 实际条目数
// 必须一致」这条交叉校验（规格 §8.2：trailer 是「每行一个差分 varint」，行数是它的唯一
// 长度契约）。
//
// 为什么承重：trailer 被截断时，若读取端按 trailer 字节自行推断条目数（宽松解码），
// 更少的条目会静默通过 —— 于是行号越界读取会被伪装成正常读取。这里把 row_count 改小 1，
// 断言损坏类错误。
func TestReaderRejectsTrailerRowCountMismatch(t *testing.T) {
	root := t.TempDir()
	sealLines(t, root, "2026-09-24", "blue", 0, [][]byte{[]byte("rc-0"), []byte("rc-1"), []byte("rc-2")})

	partPath := ta.NewPaths(root).PartFile(ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0})
	b, err := os.ReadFile(partPath)
	if err != nil {
		t.Fatal(err)
	}
	fo := len(b) - ta.FooterSize
	if got := int32(binary.BigEndian.Uint32(b[fo+18 : fo+22])); got != 3 {
		t.Fatalf("前置失败：footer 的 row_count 应为 3，实际 %d", got)
	}
	binary.BigEndian.PutUint32(b[fo+18:fo+22], 2) // 改成 2：footer 与 trailer 的说法不一致
	if err := os.WriteFile(partPath, b, 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := ta.NewReader(ta.ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	// row=1 在改小后的 row_count 里仍然「合法」，因此走不到越界分支，
	// 只有 trailer 条目数交叉校验能拦下它。
	_, err = r.GetBody(context.Background(), ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: 1})
	if !errors.Is(err, ta.ErrTrailerCorrupt) {
		t.Fatalf("footer 的 row_count 与 trailer 条目数不符时必须报 trailer 损坏，实际 %v", err)
	}
}

func TestReaderConcurrencyIsBounded(t *testing.T) {
	root := t.TempDir()
	sealLines(t, root, "2026-09-24", "blue", 0, [][]byte{[]byte("x"), []byte("y"), []byte("z")})

	r, err := ta.NewReader(ta.ReaderOptions{Root: root, MaxConcurrent: 1})
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
			_, errs[i] = r.GetBody(context.Background(), ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: i % 3})
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("并发读 %d 失败: %v", i, err)
		}
	}
	// 并发槽位是否**完全释放**要读内部状态（r.inFlight），外部包看不到，
	// 因此那条断言单独放在 A 层：internal/tracearchive/reader_internal_test.go
	// 的 TestReaderReleasesConcurrencySlots。
}

func TestReaderDoesNotLeaveTempFiles(t *testing.T) {
	// 规格 §8.3 C2：不落任何临时文件。此测试断言读取前后目录条目集合不变。
	root := t.TempDir()
	sealLines(t, root, "2026-09-24", "blue", 0, [][]byte{[]byte("aaa"), []byte("bbb")})

	dir := ta.NewPaths(root).PartDir("2026-09-24", "blue")
	before := dirEntries(t, dir)

	r, _ := ta.NewReader(ta.ReaderOptions{Root: root})
	defer r.Close()
	for i := 0; i < 50; i++ {
		if _, err := r.GetBody(context.Background(), ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: i % 2}); err != nil {
			t.Fatal(err)
		}
	}
	after := dirEntries(t, dir)
	if len(before) != len(after) {
		t.Fatalf("读取不应改变目录内容: before=%v after=%v", before, after)
	}
}

// TestReaderRejectsEmptyRoot 覆盖 NewReader 的 Root 校验。
// 与 Task 6 的 TestNewSealerValidatesOptions 对称：Root 为空时若不拒绝，
// 所有读取都会静默落到进程当前目录下（一个不存在的根被当成空根目录 = 全部 not archived）。
func TestReaderRejectsEmptyRoot(t *testing.T) {
	if r, err := ta.NewReader(ta.ReaderOptions{}); err == nil {
		_ = r.Close()
		t.Fatal("Root 为空必须报错")
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
