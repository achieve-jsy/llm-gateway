package tracearchive_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	ta "github.com/example/llm-gateway/internal/tracearchive"
)

// 本文件是 B 层测试：全部断言只用公开 API 表达（任务简报已确认本任务
// 没有任何必须触达未导出符号的断言，故无需拆 A 层）。

func TestWriterAppendAssignsDenseRowSeq(t *testing.T) {
	root := t.TempDir()
	w, err := ta.NewWriter(ta.WriterOptions{Root: root, Inst: "blue", BlockBytes: 1 << 30})
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

// TestWriterAppendFailedWritesMarkerLineAndAdvancesRow 锁定「正文落盘失败」的语义：
// 行号照常推进（与 PG 的 row_seq 对齐），并且必须落一个标记行 ——
// 产物格式（footer 的 row_count + trailer 的逐行差分 varint）要求行稠密，
// 只推进行号不写字节会让该行之后的所有行「row=N 读成第 N+1 行」，静默返回错误正文。
//
// 原用例名为 ...RowSeqWithoutBytes；控制者裁决后「不写字节」变成「写 null 标记行」，
// 因此改名（断言意图不变：行号推进 + 该行在文件里真实可见）。
func TestWriterAppendFailedWritesMarkerLineAndAdvancesRow(t *testing.T) {
	root := t.TempDir()
	w, err := ta.NewWriter(ta.WriterOptions{Root: root, Inst: "blue", BlockBytes: 1 << 30})
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

	// 【对简报的一处改写，理由】读文件前必须先让写缓冲落盘：Writer 用 bufio
	// 缓冲（这正是 D3 缺陷的成因），而 os.ReadFile 只看磁盘。直接读会读到空文件
	// ——这不是实现缺陷，而是简报此处漏了一次 flush。PendingSeals 是本包唯一的
	// 「周期性 flush 入口」（方案 C 下 gateway 的定时器就按 flush_interval 调它，
	// 规格 §7.1），故用它把缓冲刷下去；此刻两个阈值都远未达到，它返回 0 个任务，
	// 只承担 flush 职责。
	if _, err := w.PendingSeals(); err != nil {
		t.Fatalf("PendingSeals（flush 入口）失败: %v", err)
	}

	// 文件里应有 3 行：line0 / null（标记行） / line2 —— 行序与 row_seq 一一对应
	b, err := os.ReadFile(ta.NewPaths(root).SpoolFile(ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0}))
	if err != nil {
		t.Fatalf("读 spool 失败: %v", err)
	}
	if got := countLines(b); got != 3 {
		t.Fatalf("spool 应有 3 行（含失败行的 null 标记），实际 %d（内容 %q）", got, b)
	}
	if string(b) != "line0\nnull\nline2\n" {
		t.Fatalf("spool 内容应为 %q，实际 %q", "line0\nnull\nline2\n", b)
	}
}

func TestWriterPendingSealsBySize(t *testing.T) {
	root := t.TempDir()
	w, err := ta.NewWriter(ta.WriterOptions{Root: root, Inst: "blue", BlockBytes: 32})
	if err != nil {
		t.Fatalf("建写入器失败: %v", err)
	}
	defer w.Close()

	for i := 0; i < 4; i++ {
		if _, err := w.Append("2026-09-24", []byte("0123456789\n")); err != nil {
			t.Fatal(err)
		}
	}
	tasks, err := w.PendingSeals()
	if err != nil {
		t.Fatalf("PendingSeals 失败: %v", err)
	}
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

// TestWriterPendingSealsFlushesBuffer 是 D3 缺陷的回归测试：
// 封块器直接读 spool 文件，若 Writer 未 flush 写缓冲，它会读到不完整的块。
func TestWriterPendingSealsFlushesBuffer(t *testing.T) {
	root := t.TempDir()
	w, err := ta.NewWriter(ta.WriterOptions{Root: root, Inst: "blue", BlockBytes: 1 << 30, SealInterval: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// 写入量远小于 bufio 缓冲（1 MiB），若未 flush 则文件为空
	line := []byte("0123456789")
	if _, err := w.Append("2026-09-24", line); err != nil {
		t.Fatal(err)
	}
	time.Sleep(15 * time.Millisecond)
	tasks, err := w.PendingSeals()
	if err != nil {
		t.Fatalf("PendingSeals 失败: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("应产生 1 个封块任务，实际 %d", len(tasks))
	}
	b, err := os.ReadFile(tasks[0].SpoolPath)
	if err != nil {
		t.Fatal(err)
	}
	if countLines(b) != 1 {
		t.Fatalf("封块任务返回时 spool 必须已 flush，实际读到 %d 行（内容 %q）", countLines(b), b)
	}
}

func TestWriterPendingSealsByInterval(t *testing.T) {
	root := t.TempDir()
	w, err := ta.NewWriter(ta.WriterOptions{
		Root: root, Inst: "blue",
		BlockBytes:   1 << 30, // 永不因大小触发
		SealInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("建写入器失败: %v", err)
	}
	defer w.Close()

	if _, err := w.Append("2026-09-24", []byte("x\n")); err != nil {
		t.Fatal(err)
	}
	if tasks, err := w.PendingSeals(); err != nil {
		t.Fatal(err)
	} else if len(tasks) != 0 {
		t.Fatal("未到时间阈值不应产生封块任务")
	}
	time.Sleep(15 * time.Millisecond)
	tasks, err := w.PendingSeals()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("超过时间阈值应产生 1 个封块任务（否则低峰天正文写了永远读不到），实际 %d", len(tasks))
	}
}

func TestWriterNextSeqResumesFromDirectory(t *testing.T) {
	root := t.TempDir()
	p := ta.NewPaths(root)

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

	w, err := ta.NewWriter(ta.WriterOptions{Root: root, Inst: "blue", BlockBytes: 1 << 30})
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

// TestNewWriterRejectsEmptyRootAndInst 覆盖 NewWriter 的参数校验分支。
// 这两个 error 不是分类错误（没有定义哨兵，任务 8 的一致性表也已登记），
// 因此只断言「必须报错且不产生文件」。
func TestNewWriterRejectsEmptyRootAndInst(t *testing.T) {
	if w, err := ta.NewWriter(ta.WriterOptions{Root: "", Inst: "blue"}); err == nil {
		w.Close()
		t.Fatal("Root 为空应报错")
	}
	if w, err := ta.NewWriter(ta.WriterOptions{Root: t.TempDir(), Inst: ""}); err == nil {
		w.Close()
		t.Fatal("Inst 为空应报错")
	}
}

// TestNewWriterRejectsIllegalInst 锁定 Inst 白名单（"blue" / "green"）。
//
// 为什么必须有这道校验：inst 直接拼进目录名，而 filepath.Join 会做路径清理 ——
// Inst="../green" 会写进 <root>/dt=<dt>/green（越过 inst= 子树，把两个实例的
// 正文混进同一目录）；Inst="blue/" 会被清成 blue，让「实例」标识与实际目录
// 不再一一对应。只查非空挡不住这两种，必须白名单。
func TestNewWriterRejectsIllegalInst(t *testing.T) {
	root := t.TempDir()
	for _, inst := range []string{"red", "../green", "blue/", "blue/../green", "BLUE", "blue "} {
		w, err := ta.NewWriter(ta.WriterOptions{Root: root, Inst: inst})
		if err == nil {
			w.Close()
			t.Fatalf("Inst=%q 应被拒绝（越过 inst= 子树或与标识不符）", inst)
		}
	}
	// 反向断言：白名单内的实例必须仍然可用。否则「一律拒绝」也会让上面的
	// 用例全绿，而那样的守卫是坏的。
	for _, inst := range []string{"blue", "green"} {
		w, err := ta.NewWriter(ta.WriterOptions{Root: root, Inst: inst})
		if err != nil {
			t.Fatalf("Inst=%q 应被接受: %v", inst, err)
		}
		if err := w.Close(); err != nil {
			t.Fatalf("Inst=%q Close 失败: %v", inst, err)
		}
	}
}

// TestWriterPendingSealsNilBeforeAnyAppend 覆盖「当前块不存在」守卫。
// 删掉该守卫会立刻 nil 解引用 panic —— 这个用例就是那道防线的回归保护。
func TestWriterPendingSealsNilBeforeAnyAppend(t *testing.T) {
	w, err := ta.NewWriter(ta.WriterOptions{Root: t.TempDir(), Inst: "blue", BlockBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	tasks, err := w.PendingSeals()
	if err != nil {
		t.Fatalf("无当前块时不应报错: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("尚未追加任何行时不应产生封块任务，实际 %d", len(tasks))
	}
}

// TestWriterPendingSealsAppliesDefaultThresholds 覆盖两个阈值的「0 取默认值」分支。
//
// 判据：删掉任一条默认值赋值，本用例立即转红 ——
// BlockBytes=0 会让 bySize 的 `bytes >= 0` 恒真，SealInterval=0 会让 byTime 的
// `since >= 0` 恒真，于是「刚写下的一行」会被当成「块已满」交给封块器，
// 1.5 GiB 的块大小与 15 分钟的封块间隔形同虚设。
func TestWriterPendingSealsAppliesDefaultThresholds(t *testing.T) {
	w, err := ta.NewWriter(ta.WriterOptions{Root: t.TempDir(), Inst: "blue"}) // 不传任何阈值
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	if _, err := w.Append("2026-09-24", []byte("0123456789\n")); err != nil {
		t.Fatal(err)
	}
	tasks, err := w.PendingSeals()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("默认阈值（1.5 GiB / 15 min）下刚写入一行不应封块，实际 %d 个任务", len(tasks))
	}
}

// TestWriterPendingSealsReportsTaskFields 锁定 SealTask 的每个分量。
// 封块器（任务 6）只认这个结构：SpoolPath 必须等于公开路径推导的结果，
// UncompressedBytes 必须等于磁盘上 spool 的实际字节数（含每行结尾的换行）。
func TestWriterPendingSealsReportsTaskFields(t *testing.T) {
	root := t.TempDir()
	const line = "0123456789" // 10 字节
	w, err := ta.NewWriter(ta.WriterOptions{Root: root, Inst: "green", BlockBytes: 32})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	for i := 0; i < 4; i++ {
		if _, err := w.Append("2026-09-24", []byte(line+"\n")); err != nil {
			t.Fatal(err)
		}
	}
	tasks, err := w.PendingSeals()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("应产生 1 个封块任务，实际 %d", len(tasks))
	}
	got := tasks[0]
	wantPath := ta.NewPaths(root).SpoolFile(ta.Ref{DT: "2026-09-24", Inst: "green", Part: 0})
	if got.DT != "2026-09-24" || got.Inst != "green" || got.Part != 0 || got.SpoolPath != wantPath {
		t.Fatalf("SealTask 分量不符: %+v（want spool=%s）", got, wantPath)
	}
	if got.Rows != 4 {
		t.Fatalf("Rows 应为 4，实际 %d", got.Rows)
	}
	want := int64(4 * (len(line) + 1))
	if got.UncompressedBytes != want {
		t.Fatalf("UncompressedBytes 应为 spool 字节数 %d（含换行），实际 %d", want, got.UncompressedBytes)
	}
	info, err := os.Stat(got.SpoolPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != got.UncompressedBytes {
		t.Fatalf("UncompressedBytes(%d) 必须与磁盘上的 spool 大小(%d)一致", got.UncompressedBytes, info.Size())
	}
}

// TestWriterAppendTrimsTrailingNewline 锁定「末尾换行可有可无」的接口约定：
// 两种调用在 spool 里必须产生完全相同的字节，否则同一条正文会有两种形态，
// 「行号即行序」的对应关系随之破裂。
func TestWriterAppendTrimsTrailingNewline(t *testing.T) {
	root := t.TempDir()
	w, err := ta.NewWriter(ta.WriterOptions{Root: root, Inst: "blue", BlockBytes: 1 << 30})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	if _, err := w.Append("2026-09-24", []byte("a\r\n")); err != nil {
		t.Fatal(err)
	}
	ref, err := w.Append("2026-09-24", []byte("a")) // 不带换行
	if err != nil {
		t.Fatal(err)
	}
	if ref.Row != 1 {
		t.Fatalf("第二行行号应为 1，实际 %d", ref.Row)
	}
	if _, err := w.PendingSeals(); err != nil {
		t.Fatalf("PendingSeals（flush 入口）失败: %v", err)
	}
	b, err := os.ReadFile(ta.NewPaths(root).SpoolFile(ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0}))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "a\na\n" {
		t.Fatalf("spool 内容应为 %q，实际 %q", "a\na\n", b)
	}
}

// TestWriterAppendSwitchesBlockOnDayChange 锁定块以天为界。
//
// 跨天必须换块（换文件、序号归零）：若复用旧块，9 月 25 日的正文会写进
// dt=2026-09-24 的文件 —— 点读按 (dt, inst, part, row) 永远取不到它，
// sealer 也会把它封进错误的那一天。
func TestWriterAppendSwitchesBlockOnDayChange(t *testing.T) {
	root := t.TempDir()
	p := ta.NewPaths(root)
	w, err := ta.NewWriter(ta.WriterOptions{Root: root, Inst: "blue", BlockBytes: 1 << 30})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	ref1, err := w.Append("2026-09-24", []byte("d1\n"))
	if err != nil {
		t.Fatal(err)
	}
	ref2, err := w.Append("2026-09-25", []byte("d2\n"))
	if err != nil {
		t.Fatal(err)
	}
	if ref1.DT != "2026-09-24" || ref1.Part != 0 || ref1.Row != 0 {
		t.Fatalf("第一天的定位键不符: %s", ref1)
	}
	if ref2.DT != "2026-09-25" || ref2.Part != 0 || ref2.Row != 0 {
		t.Fatalf("跨天后应是新的一天、新块、行号归零: %s", ref2)
	}
	if _, err := w.PendingSeals(); err != nil {
		t.Fatalf("PendingSeals（flush 入口）失败: %v", err)
	}
	for dt, want := range map[string]string{"2026-09-24": "d1\n", "2026-09-25": "d2\n"} {
		b, err := os.ReadFile(p.SpoolFile(ta.Ref{DT: dt, Inst: "blue", Part: 0}))
		if err != nil {
			t.Fatalf("%s 的 spool 应存在: %v", dt, err)
		}
		if string(b) != want {
			t.Fatalf("%s 的 spool 内容应为 %q，实际 %q", dt, want, b)
		}
	}
}

// TestWriterAppendReportsUnusableSpoolDir 覆盖 spool 目录不可用时的 I/O 失败路径。
//
// 做法：在 <root>/spool/dt=.../inst=blue 这个位置放一个普通文件，使目录树
// 无法建立。Append 与 AppendFailed 都必须报错，绝不能默默把行号推进了
// 却什么都没落盘 —— 那会让 PG 的 row_seq 指向空气（且无人发现）。
// 这里断言错误保留了底层 *fs.PathError：实现若把 err 吞掉或不包装，本用例转红。
func TestWriterAppendReportsUnusableSpoolDir(t *testing.T) {
	root := t.TempDir()
	p := ta.NewPaths(root)
	spoolDir := p.SpoolDir("2026-09-24", "blue")
	if err := os.MkdirAll(filepath.Dir(spoolDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spoolDir, []byte("占位：这不是目录"), 0o644); err != nil {
		t.Fatal(err)
	}

	w, err := ta.NewWriter(ta.WriterOptions{Root: root, Inst: "blue", BlockBytes: 1 << 30})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	if _, err := w.Append("2026-09-24", []byte("x\n")); err == nil {
		t.Fatal("spool 目录不可用时 Append 必须报错")
	} else {
		var pathErr *fs.PathError
		if !errors.As(err, &pathErr) {
			t.Fatalf("错误必须保留底层文件系统原因（*fs.PathError），实际 %v", err)
		}
	}
	if err := w.AppendFailed("2026-09-24"); err == nil {
		t.Fatal("spool 目录不可用时 AppendFailed 必须报错（不能只推进行号）")
	}
}

// TestWriterCloseIsIdempotentWithoutBlock 覆盖 Close 在「没有当前块」时的路径：
// 没写过任何行就 Close、以及重复 Close，都必须返回 nil 而不是解引用空的 cur。
func TestWriterCloseIsIdempotentWithoutBlock(t *testing.T) {
	w, err := ta.NewWriter(ta.WriterOptions{Root: t.TempDir(), Inst: "blue"})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("空写入器 Close 应返回 nil，实际 %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("重复 Close 应返回 nil，实际 %v", err)
	}
	if _, err := w.Append("2026-09-24", []byte("x\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("有当前块时 Close 应成功，实际 %v", err)
	}
}

// TestWriterMarkSealedStartsNextPart 锁定封块后的写指针行为。
//
// 模拟 sealer（方案 C：封块在网关外）：产物就位、spool 删除，然后 MarkSealed。
// 写入器必须换到 part-000001 且行号归零；若 MarkSealed 不生效，新正文会被
// 追加进一个已被封块、随时会被删除的 spool（那些行永远进不了产物）。
func TestWriterMarkSealedStartsNextPart(t *testing.T) {
	root := t.TempDir()
	p := ta.NewPaths(root)
	w, err := ta.NewWriter(ta.WriterOptions{Root: root, Inst: "blue", BlockBytes: 1 << 30})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	ref0, err := w.Append("2026-09-24", []byte("a\n"))
	if err != nil {
		t.Fatal(err)
	}
	if ref0.Part != 0 || ref0.Row != 0 {
		t.Fatalf("首行的定位键应为 part-000000#row=0，实际 %s", ref0)
	}

	ref0Full := ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0}
	partPath := p.PartFile(ref0Full)
	if err := os.MkdirAll(filepath.Dir(partPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(partPath, []byte("zst"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(p.SpoolFile(ref0Full)); err != nil {
		t.Fatal(err)
	}
	w.MarkSealed("2026-09-24", 0)

	ref1, err := w.Append("2026-09-24", []byte("b\n"))
	if err != nil {
		t.Fatal(err)
	}
	if ref1.Part != 1 || ref1.Row != 0 {
		t.Fatalf("封块后应换到 part-000001 且行号归零，实际 %s", ref1)
	}
	if _, err := w.PendingSeals(); err != nil {
		t.Fatalf("PendingSeals（flush 入口）失败: %v", err)
	}
	b, err := os.ReadFile(p.SpoolFile(ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 1}))
	if err != nil {
		t.Fatalf("新块的 spool 应存在: %v", err)
	}
	if string(b) != "b\n" {
		t.Fatalf("新块 spool 内容应为 %q，实际 %q", "b\n", b)
	}
}

// TestWriterListPartsFiltersIllegalNames 覆盖 ListParts 的两条分支：
// 产物目录不存在时返回空而不是错误；目录里的非法名（半成品 .tmp、索引文件、
// 位数不符）一律不得进入结果 —— 调用方靠它判断「哪些块已经存在」。
func TestWriterListPartsFiltersIllegalNames(t *testing.T) {
	root := t.TempDir()
	p := ta.NewPaths(root)
	w, err := ta.NewWriter(ta.WriterOptions{Root: root, Inst: "blue"})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	parts, err := w.ListParts("2026-09-24")
	if err != nil {
		t.Fatalf("产物目录不存在时不应报错: %v", err)
	}
	if len(parts) != 0 {
		t.Fatalf("产物目录不存在时应返回空，实际 %v", parts)
	}

	dir := p.PartDir("2026-09-24", "blue")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"part-000003.jsonl.zst",
		"part-000001.jsonl.zst",
		"part-000002.jsonl.zst.tmp", // rename 前的半成品
		"index.jsonl",               // 非 part- 前缀
		"part-00004.jsonl.zst",      // 位数不符
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	parts, err = w.ListParts("2026-09-24")
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{1, 3}; !reflect.DeepEqual(parts, want) {
		t.Fatalf("ListParts 应升序返回 %v（忽略半成品与非法名），实际 %v", want, parts)
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
