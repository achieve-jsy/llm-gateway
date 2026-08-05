package tracearchive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 本文件是任务 7（按定位键点读）的 A 层（包内）测试。
//
// 【为什么必须触达未导出符号（全局约束要求写明）】两条断言都无法用公开 API 表达：
//
//  1. TestReaderRejectsOversizedTrailerLen 与 TestReaderRejectsTrailerEntryCountOverBound：
//     两者的边界值分别是未导出的 maxTrailerBytes（64 << 20）与 maxTrailerEntries
//     （64 << 20 / 8）。B 层若要表达，只能内联一个自己抄来的字面量——那样它就不再是
//     「上界的边界用例」，改常量时用例不会跟着动，等于把守卫与用例解耦。
//     此外它们要用未导出的 footer/encodeFooter 构造「帧摘要与邻接都自洽、唯独越界」
//     的产物，这种自洽只能靠包内的编码器表达。
//  2. TestReaderReleasesConcurrencySlots 与 TestReaderHonorsContextCancelWhileWaitingForSlot：
//     前者的断言要读 Reader 的内部状态 r.inFlight()
//     （= len(r.sem)）。外部包只能观察到「读成功了」，观察不到「槽位还回来了」——
//     而槽位泄漏正是并发上限实现最容易出的 bug（跑满 MaxConcurrent 后永久卡死）。
//     对应的公开行为断言留在 B 层：tests/tracearchive/reader_test.go 的
//     TestReaderConcurrencyIsBounded（8 个并发读必须全部成功）。
//
// 其余全部断言都在 B 层（tests/tracearchive/reader_test.go），本文件不重复表达。

// TestReaderRejectsOversizedTrailerLen 覆盖 Reader 对 footer 的 trailer_len 上界校验。
//
// 为什么必须有这道闸：decodeTrailer 的容量上限是 trailer 字节数的 8 倍
// （len(b) 字节的 varint 流确实可能编码出 len(b) 个 int64 条目，这是格式固有的）。
// 当 trailer_len 可以接近文件大小时，一次损坏的 footer 就能让 control-plane
// （2 GiB 限额）尝试分配数 GB 并 OOM。trailer 紧邻 footer 的一致性校验只把
// trailer_len 绑在文件大小上，拦不住「1.5 GiB 的块 + 1.5 GiB 的 trailer」。
//
// 【为什么帧摘要必须自洽（与简报代码块的差异，报告里指出）】简报此处把
// trailer_offset 留 0、FrameSHA 留零值（encodeFooter 的零值）。但本任务 R10 第 2 项
// 给读取路径加了 frame_sha256 校验：零值与「空帧区的摘要」并不相等，于是删除上界守卫后
// 摘要校验会抢先把读取拦成 ErrFooterCorrupt —— 用例照样绿，变异杀不死（断言被别的
// 守卫遮蔽，正是全局约束 D15 的反面写法）。因此这里构造一个**帧区非空且摘要自洽**的
// 产物：邻接与摘要都通过，唯一不成立的只有 trailer_len 上界。
//
// 构造方式：稀疏文件，帧区 16 字节假数据（本用例不解压），trailer 区总长
// maxTrailerBytes+1，footer 自洽（trailer_offset=16，trailer_offset+trailer_len == size-FooterSize）。
func TestReaderRejectsOversizedTrailerLen(t *testing.T) {
	root := t.TempDir()
	partPath := NewPaths(root).PartFile(Ref{DT: "2026-09-24", Inst: "blue", Part: 0})
	if err := os.MkdirAll(filepath.Dir(partPath), 0o755); err != nil {
		t.Fatal(err)
	}

	frame := []byte("fake-zstd-frame!") // 16 字节：只为让帧区非空且摘要可比对
	sum := sha256.Sum256(frame)
	var sha8 [8]byte
	copy(sha8[:], sum[:8])

	trailerLen := int64(maxTrailerBytes) + 1
	size := int64(len(frame)) + trailerLen + FooterSize

	// 稀疏文件：Truncate 不写实际数据，因此不会真的占用 64 MiB
	f, err := os.Create(partPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(frame); err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	fb := encodeFooter(footer{
		TrailerOffset: int64(len(frame)),
		TrailerLen:    int32(trailerLen),
		RowCount:      1,
		FrameSHA:      sha8,
	})
	if _, err := f.WriteAt(fb, size-FooterSize); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	r, err := NewReader(ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	_, err = r.GetBody(context.Background(), Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: 0})
	if !errors.Is(err, ErrFooterCorrupt) {
		t.Fatalf("trailer_len 超过上界应返回 ErrFooterCorrupt（产物损坏），实际 %v", err)
	}
}

// TestReaderRejectsTrailerEntryCountOverBound 覆盖 Reader 对 trailer【条目数】上界的校验。
//
// 【为什么必须有（与上面那条字节上界是两半）】maxTrailerBytes 只压 trailer 的字节数，
// 而 decodeTrailer 的分配是 8 × 条目数：trailer 的每个字节都可以是一个恰好 1 字节的
// varint（全 0x00 ⇒ 差分 0、偏移单调不减），于是 64 MiB 的 trailer 能【合法地】声称
// 67,108,864 条。F1 实测：这份输入让 decodeTrailer 成功返回（err=nil）并分配
// 512.0 MiB（536,876,232 字节，213 ms）；装进产物后单次点读峰值堆 1090 MiB
// （索引 512 + tb 64 + 解码窗口 512），MaxConcurrent=2 时超出规格 §8.4 C5 的预算。
// 上界因此必须同时施加在条目数上（见 reader.go 的 maxTrailerEntries），
// 超限返回 ErrTrailerCorrupt，【不截断】。
//
// 【为什么放 A 层】边界值是未导出的 maxTrailerEntries（同上面那条 maxTrailerBytes：
// 内联抄来的字面量会让用例与常量解耦），且要用未导出的 footer/encodeFooter 构造
// 「真实 zstd 帧 + 帧摘要自洽 + trailer 紧邻 footer + trailer_len 未越界，唯独条目数越界」
// 的产物。
//
// 【杀伤力（F1 变异条目）】删掉 decodeTrailer 里 `n > maxTrailerEntries` 分支后，
// 这份产物的 trailer 会被【完整解码】（n == len(b)，尾部无多余字节）：全 0 偏移 ⇒
// 目标偏移 0 ⇒ 真实帧正常解压 ⇒ GetBody **成功返回第一行正文且 err == nil**。
// 断言错误身份（errors.Is ErrTrailerCorrupt）而非「有没有报错」，正是为了这一点：
// 变异后不是「换个错误」而是「静默读出一行」，用例在 err 上转红。
func TestReaderRejectsTrailerEntryCountOverBound(t *testing.T) {
	root := t.TempDir()
	sealLines(t, root, "2026-09-24", "blue", 0, [][]byte{[]byte(`{"a":1}`), []byte(`{"b":2}`)})
	ref := Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: 0}
	partPath := NewPaths(root).PartFile(ref)

	// 以真实封块的产物为底：只换掉 trailer 与 footer，帧字节与其摘要保持自洽。
	raw, err := os.ReadFile(partPath)
	if err != nil {
		t.Fatal(err)
	}
	old, err := decodeFooter(raw[len(raw)-FooterSize:])
	if err != nil {
		t.Fatal(err)
	}
	frame := raw[:old.TrailerOffset]
	sum := sha256.Sum256(frame)
	var sha8 [8]byte
	copy(sha8[:], sum[:8])

	// 条目数与 trailer 长度都取上界 +1，且长度【恰好等于】条目数：
	// 每个字节都是一个合法单字节 varint（0x00），故除了条目数上界之外全部成立。
	n := int64(maxTrailerEntries) + 1
	var buf bytes.Buffer
	buf.Write(frame)
	buf.Write(make([]byte, int(n)))
	if _, err := buf.Write(encodeFooter(footer{
		TrailerOffset: int64(len(frame)),
		TrailerLen:    int32(n),
		RowCount:      int32(n),
		FrameSHA:      sha8,
	})); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(partPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewReader(ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	line, err := r.GetBody(context.Background(), ref)
	if !errors.Is(err, ErrTrailerCorrupt) {
		t.Fatalf("条目数超过上界 %d 应返回 ErrTrailerCorrupt（产物损坏），实际 err=%v line=%q",
			maxTrailerEntries, err, line)
	}
}

// TestReaderReleasesConcurrencySlots 覆盖「并发槽位用后必须完全释放」。
//
// 【为什么破例放包内】见本文件头部说明 2：断言要读 Reader 的内部状态 r.inFlight()
// （= len(r.sem)），这是未导出的测试辅助方法；槽位泄漏（跑满 MaxConcurrent 后永久卡死）
// 是并发上限实现最容易出的 bug，而外部包观察不到它。
func TestReaderReleasesConcurrencySlots(t *testing.T) {
	root := t.TempDir()
	sealLines(t, root, "2026-09-24", "blue", 0, [][]byte{[]byte("x"), []byte("y")})

	r, err := NewReader(ReaderOptions{Root: root, MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	for i := 0; i < 3; i++ {
		if _, err := r.GetBody(context.Background(), Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: i % 2}); err != nil {
			t.Fatalf("第 %d 次读取失败: %v", i, err)
		}
		if got := r.inFlight(); got != 0 {
			t.Fatalf("第 %d 次读取后并发槽位未释放，残留 %d", i, got)
		}
	}
}

// TestReaderHonorsContextCancelWhileWaitingForSlot 覆盖「等待并发槽位期间的取消」
// （reader.go:109-114 的 select 的 case <-ctx.Done() 那一支）。
//
// 【为什么破例放包内】要**确定性地**走到该分支，必须先把槽位占满；而占满的唯一办法
// 是直接向未导出的 r.sem 投一个令牌——外部包既观察不到 r.sem，也造不出「槽位已满」
// 这个状态。占满之后 select 的两个分支里只有 ctx.Done() 就绪（向已满的带缓冲 channel
// 发送不可就绪），所以本用例不依赖调度时序、不 sleep、没有竞态。
//
// 【它守的是什么】槽位被占满时，调用方的取消必须立刻返回 context.Canceled，而不是
// 无限期排队等槽位。没有这一支，被取消的请求会一直占着调用方 goroutine（以及 HTTP
// 连接）直到前面那次 decode 结束——而 decode 可能持续数秒。
//
// 【为什么用带界的 select 而不是直接调用】直接调用在「该分支被删」时会永久阻塞，
// 把整个测试包拖成超时挂死（其他用例的判决一起丢掉）。这里给 10 s 上界：正常路径
// 微秒级返回，上界只在守卫缺失时才起作用，失败信息因此是**干净且定位到本条用例**的。
func TestReaderHonorsContextCancelWhileWaitingForSlot(t *testing.T) {
	root := t.TempDir()
	sealLines(t, root, "2026-09-24", "blue", 0, [][]byte{[]byte("x")})

	r, err := NewReader(ReaderOptions{Root: root, MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	ref := Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: 0}

	// ① 正向对照：槽位空闲时同一请求必须成功——保证下面的失败确实来自取消。
	if _, err := r.GetBody(context.Background(), ref); err != nil {
		t.Fatalf("槽位空闲时读取应成功: %v", err)
	}

	// ② 占满唯一槽位（等价于「另一次点读正在解压」）。
	r.sem <- struct{}{}
	if got := r.inFlight(); got != 1 {
		t.Fatalf("用例自检失败：槽位未按预期占满，残留 %d", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	type result struct{ err error }
	done := make(chan result, 1)
	go func() {
		_, err := r.GetBody(ctx, ref)
		done <- result{err}
	}()
	select {
	case got := <-done:
		if !errors.Is(got.err, context.Canceled) {
			t.Fatalf("等待槽位期间的取消必须立刻返回 context.Canceled，实际 %v", got.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("等待槽位期间的取消没有返回：ctx.Done() 分支缺失（该请求会一直占着调用方 goroutine）")
	}

	// ③ 取消路径不得动到别人占着的槽位。
	if got := r.inFlight(); got != 1 {
		t.Fatalf("取消路径不得释放别人的槽位，实际残留 %d", got)
	}

	// ④ 取消只影响那一次调用：归还槽位后同一 Reader 必须照常工作。
	<-r.sem
	if _, err := r.GetBody(context.Background(), ref); err != nil {
		t.Fatalf("取消不应损坏 Reader，后续读取失败: %v", err)
	}
	if got := r.inFlight(); got != 0 {
		t.Fatalf("后续读取后槽位未完全归还，残留 %d", got)
	}
}
