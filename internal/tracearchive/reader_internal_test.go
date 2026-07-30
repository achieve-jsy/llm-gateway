package tracearchive

import (
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
//  1. TestReaderRejectsOversizedTrailerLen：边界值就是未导出的 maxTrailerBytes 本身
//     （64 << 20）。B 层若要表达，只能内联一个自己抄来的字面量——那样它就不再是
//     「上界的边界用例」，改常量时用例不会跟着动，等于把守卫与用例解耦。
//     此外它要用未导出的 footer/encodeFooter 构造「帧摘要与邻接都自洽、唯独 trailer_len
//     越界」的产物，这种自洽只能靠包内的编码器表达。
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
