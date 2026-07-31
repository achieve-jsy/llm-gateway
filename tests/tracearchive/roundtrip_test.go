// tests/tracearchive/roundtrip_test.go
//
// B 层（package tracearchive_test）：只用公开 API，走一遍
// 「Writer 追加 → Sealer 封块（C 版 zstd CLI）→ Reader 按定位键点读」的完整往返。
//
// 【为什么没有 A 层文件】本文件与 constraints_test.go 的每一条断言都只需
// Writer/Sealer/Reader 的导出方法 + 文件系统可观测结果，没有一条需要未导出符号
// ——正是判据里「优先选择 B 层」的标准情形。
//
// 【依赖】真封块路径需要本机 zstd（C 版，支持 --long），缺失即 SKIP
// （requireZstd）。SKIP 会让变异工具把整轮证据判为无效，因此 CI 镜像必须安装 zstd。
package tracearchive_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	ta "github.com/example/llm-gateway/internal/tracearchive"
)

// TestEndToEndRoundTripByteExact 是验收用例 A1 的缩小版：
// 写入 N 行 → 达到阈值即封块 → 收尾封块 → 随机抽 K 行点读 → 逐字节比对。
//
// 这里用 Writer.PendingSeals 直接驱动封块循环，等价于生产里 sealer 的
// 「ListPendingSeals 发现 → Seal 封块 → MarkSealed」循环（方案 C：封块在网关外）。
//
// 【为什么是「缩小版」】原验收是 2 万行级别；本用例固定 2 万行、1 MiB 块阈值，
// 因此必然跨多个块（末段 `sealed < 2` 的断言把这一点变成显式的护栏）。
func TestEndToEndRoundTripByteExact(t *testing.T) {
	if testing.Short() {
		t.Skip("端到端测试，-short 下跳过")
	}
	const (
		totalRows = 20000
		sampleK   = 200
	)
	root := t.TempDir()
	// 方案 C：封块走真实 zstd CLI（缺失则 skip，绝不静默假绿）
	bin, ver := requireZstd(t)

	w, err := ta.NewWriter(ta.WriterOptions{Root: root, Inst: "blue", BlockBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}

	rnd := rand.New(rand.NewSource(20260924))
	makeLine := func(i int) []byte {
		n := rnd.Intn(4096)
		return []byte(fmt.Sprintf(`{"request_id":"rid-%06d","ts":"2026-09-24T00:00:00+08:00","body":"%s"}`,
			i, string(bytes.Repeat([]byte("x"), n))))
	}

	want := make(map[ta.Ref][]byte, totalRows)
	sealed := 0
	for i := 0; i < totalRows; i++ {
		line := makeLine(i)
		ref, err := w.Append("2026-09-24", line)
		if err != nil {
			t.Fatalf("第 %d 行追加失败: %v", i, err)
		}
		want[ref] = line
		sealed += sealPending(t, w, root, bin, ver)
	}
	// 收尾：最后那个未达阈值的块也必须封掉，否则它的行只能从 spool 读，
	// 这条往返就不再覆盖「产物路径」。
	sealed += sealPending(t, w, root, bin, ver)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if sealed < 2 {
		t.Fatalf("端到端往返必须跨多个块，实际只封了 %d 个块（参数退化 ⇒ 这条用例会变成"+
			"「单块往返」的假覆盖）", sealed)
	}

	r, err := ta.NewReader(ta.ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	// 随机抽样，逐字节比对
	refs := make([]ta.Ref, 0, len(want))
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
	if checked != sampleK {
		t.Fatalf("抽样比对只完成 %d/%d 条（抽样逻辑退化 ⇒ 覆盖不足）", checked, sampleK)
	}
	t.Logf("逐字节比对通过: %d/%d 行，共写入 %d 行，共封 %d 个块", checked, sampleK, totalRows, sealed)
}

// TestRoundTripSurvivesWriterReopen 是补充一的**前半段**（可经公开 API 直接到达的形态）：
// 写入若干完整行 → Close → 用同一 (dt, inst) 重新打开 → 继续追加 → 封块 → 逐行点读比对。
//
// 它把两条事实固化下来：
//
//  1. 干净 Close 之后，spool 的最后一字节是 '\n'：每次 Append 都写「正文 + '\n'」，
//     Close 会 flush 整段缓冲。因此「关闭态留下残行」在正常路径上**不出现**，
//     任务 7 残行守卫所面对的残行只可能来自崩溃/短写。
//  2. 重新打开**不会**在既有的同一个 part 上续写：写指针以目录内容为权威
//     （规格 §7.3），存在 part-N.jsonl 时重开得到 N+1。
//
// 事实 2 是 reader.go:266-273 所依赖的前提「残行恒为该文件的最后一条物理行」的一半
// （另一半是写失败后的毒化 + 改名隔离，由 A 层与 Task 5 的用例覆盖）。
func TestRoundTripSurvivesWriterReopen(t *testing.T) {
	if testing.Short() {
		t.Skip("端到端测试，-short 下跳过")
	}
	root := t.TempDir()
	const dt, inst = "2026-09-24", "blue"

	first := [][]byte{
		[]byte(`{"row":0,"half":"first"}`),
		[]byte(`{"row":1,"half":"first"}`),
		[]byte(`{"row":2,"half":"first"}`),
	}
	second := [][]byte{
		[]byte(`{"row":3,"half":"second"}`),
		[]byte(`{"row":4,"half":"second"}`),
	}

	// ---- 第一段：写入 → Close ----
	w1, err := ta.NewWriter(ta.WriterOptions{Root: root, Inst: inst, BlockBytes: 1 << 30})
	if err != nil {
		t.Fatal(err)
	}
	refs1 := make([]ta.Ref, 0, len(first))
	for i, line := range first {
		ref, err := w1.Append(dt, line)
		if err != nil {
			t.Fatalf("第一段第 %d 行追加失败: %v", i, err)
		}
		if ref.Row != i {
			t.Fatalf("行号必须稠密: 期望 row=%d 实际 row=%d", i, ref.Row)
		}
		refs1 = append(refs1, ref)
	}
	if err := w1.Close(); err != nil {
		t.Fatalf("关闭第一段失败: %v", err)
	}

	// 事实 1：干净关闭后的 spool 以 '\n' 收尾，且字节逐字节等于写入内容
	part0Spool := ta.NewPaths(root).SpoolFile(refs1[0])
	raw0, err := os.ReadFile(part0Spool)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw0) == 0 || raw0[len(raw0)-1] != '\n' {
		t.Fatalf("干净关闭后 spool 必须以 '\\n' 收尾（否则 reader.go 对「关闭态」的假设就不成立）: "+
			"%d 字节，末字节=%q", len(raw0), lastByte(raw0))
	}
	if want0 := concatLines(first); !bytes.Equal(raw0, want0) {
		t.Fatalf("第一段 spool 字节不符: want %d 字节 got %d 字节", len(want0), len(raw0))
	}

	// ---- 第二段：重新打开同一 (dt, inst) → 继续追加 ----
	w2, err := ta.NewWriter(ta.WriterOptions{Root: root, Inst: inst, BlockBytes: 1 << 30})
	if err != nil {
		t.Fatal(err)
	}
	refs2 := make([]ta.Ref, 0, len(second))
	for i, line := range second {
		ref, err := w2.Append(dt, line)
		if err != nil {
			t.Fatalf("第二段第 %d 行追加失败: %v", i, err)
		}
		refs2 = append(refs2, ref)
	}

	// 事实 2：重开的块序号必须严格大于第一段（裸的「同 part 续写」不可达）
	if refs2[0].Part <= refs1[0].Part {
		t.Fatalf("重开后仍落在 part %d（第一段是 part %d）⇒ 在同一文件上续写，\n"+
			"「残行恒为该文件的最后一条物理行」这一前提不再由构造保证",
			refs2[0].Part, refs1[0].Part)
	}
	if refs2[0].Row != 0 {
		t.Fatalf("新块的行号必须从 0 开始，实际 %d", refs2[0].Row)
	}
	if err := w2.Close(); err != nil {
		t.Fatalf("关闭第二段失败: %v", err)
	}

	// 前提的直接观测：第一段那个 spool 在第二段写入之后逐字节未变
	raw0Again, err := os.ReadFile(part0Spool)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw0, raw0Again) {
		t.Fatalf("重开续写改动了既有 part 的字节: before=%d 字节 after=%d 字节",
			len(raw0), len(raw0Again))
	}

	// ---- 封块两块，逐行点读比对 ----
	sealExistingSpool(t, root, dt, inst, refs1[0].Part, len(first))
	sealExistingSpool(t, root, dt, inst, refs2[0].Part, len(second))

	r, err := ta.NewReader(ta.ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	refs := append(append([]ta.Ref{}, refs1...), refs2...)
	lines := append(append([][]byte{}, first...), second...)
	for i, ref := range refs {
		got, err := r.GetBody(context.Background(), ref)
		if err != nil {
			t.Fatalf("%s 点读失败: %v", ref, err)
		}
		if !bytes.Equal(got, lines[i]) {
			t.Fatalf("%s 内容不符:\n want %q\n got  %q", ref, lines[i], got)
		}
	}
	t.Logf("跨两次打开（part %d → part %d）逐行点读通过: %d 行",
		refs1[0].Part, refs2[0].Part, len(refs))
}

// TestReopenNeverContinuesResidualSpoolTail 是补充一的**后半段**，覆盖危险形态：
// 最后一字节不是 '\n'（残行）时重新打开写入器会发生什么。
//
// 【为什么必须直接构造文件】残行经公开 API 造不出来：正常路径每次 Append 都写
// 「正文 + '\n'」；写失败会毒化该块并把它改名隔离。只有进程在 bufio 的一次 flush
// 之后、写完整行之前被 kill，盘上才会留下「完整行… + 未收尾的残行」。
// 因此这里按该形态直接写文件（与 reader_test.go 的
// TestReaderRejectsUnterminatedSpoolTail 同一构造手法），再考察**重开写入器**之后的行为。
//
// 断言的正是那条前提本身：重开不会在已带残行的 part 上续写。
// 若在此续写，残行会与后续字节**粘连**成一条物理行，其后所有行整体错位
// （后果由 TestResidualTailGluesWhenPremiseBroken 固化）——那是静默错正文。
func TestReopenNeverContinuesResidualSpoolTail(t *testing.T) {
	root := t.TempDir()
	const dt, inst = "2026-09-24", "blue"
	p := ta.NewPaths(root)
	crashedSpool := p.SpoolFile(ta.Ref{DT: dt, Inst: inst, Part: 0})
	if err := os.MkdirAll(filepath.Dir(crashedSpool), 0o755); err != nil {
		t.Fatal(err)
	}
	// 两条完整行 + 一条没有换行收尾的残行（模拟 bufio 的 1 MiB flush 停在行中间后被 kill）
	crashed := []byte(`{"row":0}` + "\n" + `{"row":1}` + "\n" + `{"row":2}`)
	if err := os.WriteFile(crashedSpool, crashed, 0o644); err != nil {
		t.Fatal(err)
	}

	// 读侧正反两面：残行之前的完整行照常可读，残行本身必须被拒绝
	r, err := ta.NewReader(ta.ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	got0, err := r.GetBody(context.Background(), ta.Ref{DT: dt, Inst: inst, Part: 0, Row: 0})
	if err != nil {
		t.Fatalf("残行之前的完整行必须照常读到: %v", err)
	}
	if want := []byte(`{"row":0}`); !bytes.Equal(got0, want) {
		t.Fatalf("row=0 内容不符: want %q got %q", want, got0)
	}
	_, err = r.GetBody(context.Background(), ta.Ref{DT: dt, Inst: inst, Part: 0, Row: 2})
	if err == nil {
		t.Fatal("残行必须被拒绝，不能把未收尾的字节当正文返回")
	}
	if !errors.Is(err, ta.ErrNotArchived) {
		t.Fatalf("残行属「存储状态异常」（ErrNotArchived/500 类），实际 %v", err)
	}
	if errors.Is(err, ta.ErrBodyMissing) {
		t.Fatalf("残行不是 body_missing（正文并非写入时即失败），不得归入 404 语义: %v", err)
	}

	// ---- 重开写入器：必须落到新的块序号，且不动那个残行文件 ----
	w, err := ta.NewWriter(ta.WriterOptions{Root: root, Inst: inst, BlockBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := w.Append(dt, []byte(`{"row":0,"part":"next"}`))
	if err != nil {
		t.Fatalf("重开追加失败: %v", err)
	}
	if ref.Part == 0 {
		t.Fatalf("重开后在带残行的 part 0 上续写 ⇒ 残行会与后续字节粘连，" +
			"「残行恒为该文件的最后一条物理行」这一前提失效")
	}
	if err := w.Close(); err != nil {
		t.Fatalf("关闭重开的写入器失败: %v", err)
	}

	// 前提的核心观测：带残行的文件逐字节未变（既没被续写，也没被回退改写）
	after, err := os.ReadFile(crashedSpool)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(crashed, after) {
		t.Fatalf("重开续写改动了带残行的 spool:\n before=%q\n after =%q", crashed, after)
	}

	// 正向对照：新块的内容可从 spool 读回（新块是干净起点，行号从 0 开始）
	r2, err := ta.NewReader(ta.ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Close()
	got2, err := r2.GetBody(context.Background(), ref)
	if err != nil {
		t.Fatalf("新块的 row=0 必须可读: %v", err)
	}
	if want := []byte(`{"row":0,"part":"next"}`); !bytes.Equal(got2, want) {
		t.Fatalf("新块内容不符: want %q got %q", want, got2)
	}
}

// TestResidualTailGluesWhenPremiseBroken 是**反事实探针**，不是对实现的要求：
// 它固化「前提失效之后世界长什么样」，用来证明「残行恒为该文件的最后一条物理行」
// （reader.go:266-273 对 Task 7 残行守卫的成立性说明所依赖的前提）是**承重**的。
//
// 构造（假设写入器真的在残行之后往**同一个文件**继续追加）：
//
//	逻辑行  row0="A"、row1="BBBB"（短写只落盘 "BB"）、row2="CCCC"、row3="DDDD"
//	盘上字节 "A\n" + "BB" + "CCCC\n" + "DDDD\n"
//
// ⇒ 物理行序与 row_seq 整体错位一位，且残行与下一行粘连。
// 本用例断言的就是 Reader 在这种文件上的可观测行为：它返回**格式正确但属于相邻行**
// 的正文（row=2 读到的是 row3 的内容），而残行守卫对此无能为力——因为被请求的那一条
// 物理行是**以换行收尾**的，守卫的判据（EOF 前仍带回非空字节）根本不触发。
//
// 【这条用例不保护任何守卫】它描述的是守卫失效后的世界。它的价值是：任何让
// 「残行之后还能续写」重新成立（例如写指针不再 +1）的改动，都会被
// TestReopenNeverContinuesResidualSpoolTail 抓住，而本用例给出「必须抓住」的理由。
func TestResidualTailGluesWhenPremiseBroken(t *testing.T) {
	root := t.TempDir()
	const dt, inst = "2026-09-24", "blue"
	spool := ta.NewPaths(root).SpoolFile(ta.Ref{DT: dt, Inst: inst, Part: 0})
	if err := os.MkdirAll(filepath.Dir(spool), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := []byte("A\n" + "BB" + "CCCC\n" + "DDDD\n")
	if err := os.WriteFile(spool, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := ta.NewReader(ta.ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	read := func(row int) ([]byte, error) {
		return r.GetBody(context.Background(), ta.Ref{DT: dt, Inst: inst, Part: 0, Row: row})
	}

	// row0 在粘连点之前：照常正确
	got0, err := read(0)
	if err != nil {
		t.Fatalf("row=0 应可读: %v", err)
	}
	if !bytes.Equal(got0, []byte("A")) {
		t.Fatalf("row=0 内容不符: %q", got0)
	}

	// row1：残行的前缀与后续字节粘连成一条物理行 ⇒ 返回的既不是 row1 的完整正文，
	// 也不是任何逻辑行的内容，而是一个「格式正确」的拼接串（无错误、无告警）
	got1, err := read(1)
	if err != nil {
		t.Fatalf("row=1 在粘连形态下不会报错（这正是该失效的静默性）: %v", err)
	}
	if !bytes.Equal(got1, []byte("BBCCCC")) {
		t.Fatalf("粘连形态下 row=1 期望读到拼接串 %q，实际 %q", "BBCCCC", got1)
	}

	// row2：错位一位 ⇒ **返回的是 row3 的正文**，格式完全正确
	got2, err := read(2)
	if err != nil {
		t.Fatalf("row=2 在错位形态下不会报错（静默错正文）: %v", err)
	}
	if !bytes.Equal(got2, []byte("DDDD")) {
		t.Fatalf("错位形态下 row=2 期望读到相邻行（row3）的正文 %q，实际 %q", "DDDD", got2)
	}

	// row3：物理行已用完 ⇒ 响亮报错（错位只对「落在文件内的行号」静默）
	if _, err := read(3); !errors.Is(err, ta.ErrNotArchived) {
		t.Fatalf("超出物理行数的行号必须响亮报错（ErrNotArchived），实际 %v", err)
	}
}

// lastByte 返回最后一个字节，空切片返回 0（只用于错误信息）。
func lastByte(b []byte) byte {
	if len(b) == 0 {
		return 0
	}
	return b[len(b)-1]
}
