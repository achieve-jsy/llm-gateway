package tracearchive

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件是 A 层（包内测试），只放**必须触达未导出符号**的写入器用例。
//
// 【为什么这几条必须破例放包内（而不是 B 层）】
//
//  1. 写失败注入：本子项目真正要防的形态是「bufio 直写 fd 时短写，留下一条
//     没有换行收尾的残行」。要精确制造它，只能替换未导出的 cur.w（*bufio.Writer）
//     并给它一个「只接受 K 字节就报错」的底层 writer。ENOSPC 无法用公开 API
//     制造：chmod 不改已打开 fd 的写权限，把 spool 路径建成目录只会在 open
//     阶段失败（那是「建块失败」，不产生残行），/dev/full 在 macOS 上不存在。
//     而盘满正是本子项目存在的那条路径，不能用「难以模拟」搪塞。
//  2. MarkSealed 的 close 回归：旧句柄是否真被关闭，只能通过持有未导出的
//     cur.f 观察（fd 不经任何公开 API 暴露）。不写这条，把那行修复退回
//     「只置 cur = nil」时 17 个 B 层用例仍全绿——即修复没有回归防线。
//
// B 层的其余用例（校验、标记行统计、MarkSealed 匹配条件等）见
// tests/tracearchive/writer_test.go。

// shortWriteFile 模拟「fd 只能再接受 budget 字节」的盘满短写。
//
// 真实 ENOSPC 的形态就是「写进去一部分，然后返回错误」，而正是这个「一部分」
// 会在 .jsonl 末尾留下没有换行收尾的残行。budget 用尽后一律直接返回错误，
// 这同时也对应「盘持续满」。
type shortWriteFile struct {
	f      *os.File
	budget int

	accepted   int   // 实际写进 fd 的字节数（用于证明短写真的发生了）
	sizeAtFail int64 // 失败瞬间的盘上长度（用于证明残行真的产生了）
}

// errInjectedWriteFailure 是注入的写失败原因。它模拟 syscall.ENOSPC，
// 但不依赖平台常量，任何平台都能跑。
var errInjectedWriteFailure = errors.New("注入的写失败：no space left on device")

func (s *shortWriteFile) Write(p []byte) (int, error) {
	if s.budget <= 0 {
		return 0, errInjectedWriteFailure
	}
	if len(p) > s.budget {
		// 短写：只接受 budget 字节，然后报错——残行由此产生。
		n, err := s.f.Write(p[:s.budget])
		s.accepted += n
		s.budget = 0
		if err != nil {
			return n, err
		}
		if info, serr := s.f.Stat(); serr == nil {
			s.sizeAtFail = info.Size()
		}
		return n, errInjectedWriteFailure
	}
	n, err := s.f.Write(p)
	s.accepted += n
	s.budget -= n
	return n, err
}

// failAuthorInjectedWrite 把当前块的写缓冲换成一个「缓冲容量 cap、底层只再
// 接受 budget 字节」的 bufio.Writer，然后追加两行把 flush 逼出来触发短写。
//
// 调用前当前块必须已经 flush 过至少一行（前置：盘上已有完整行）。
// 返回注入的底层 writer（供断言短写确实发生）与 Append 返回的错误。
func failAuthorInjectedWrite(t *testing.T, w *Writer, dt string, budget int) (*shortWriteFile, error) {
	t.Helper()

	w.mu.Lock()
	fw := &shortWriteFile{f: w.cur.f, budget: budget}
	// 缓冲容量 8 < 一次追加的 6 字节 ×2：第二次追加必然触发 flush。
	w.cur.w = bufio.NewWriterSize(fw, 8)
	w.mu.Unlock()

	// 第一次：进缓冲，不触发 flush。
	if _, err := w.Append(dt, []byte("line1\n")); err != nil {
		t.Fatalf("进缓冲不该失败: %v", err)
	}
	// 第二次：缓冲满 → flush → 底层短写 3 字节后报错。
	_, err := w.Append(dt, []byte("line2\n"))
	if err == nil {
		t.Fatal("注入短写后 Append 必须报错")
	}
	return fw, err
}

// TestWriterWriteFailureLeavesNoResidualLine 锁定「写失败不留残行」。
//
// 残行（没有换行收尾的半行）是承重不变量的唯一无守卫入口：sealer 的
// scanSpoolOffsets 对未终止的尾行也计一行（任务 6），于是 Rows==0 的目录
// 扫描路径会把残行烤进产物、其后的行全部错位（静默错正文）。
//
// 判据：删掉 poisonCurLocked 里的 truncateToLastNewline 调用，本用例转红
// （隔离件内容会变成 "line0\nlin"）。
func TestWriterWriteFailureLeavesNoResidualLine(t *testing.T) {
	root := t.TempDir()
	p := NewPaths(root)
	w, err := NewWriter(WriterOptions{Root: root, Inst: instBlue, BlockBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	const dt = "2026-09-24"
	ref0, err := w.Append(dt, []byte("line0\n"))
	if err != nil {
		t.Fatal(err)
	}
	// 先把第一行刷到盘上：制造「盘上已有一条完整行」的前置状态。
	// 阈值只有 1 字节，所以它会返回一个封块任务；本用例只借它做 flush。
	if _, err := w.PendingSeals(); err != nil {
		t.Fatal(err)
	}

	spoolPath := p.SpoolFile(ref0)
	info, err := os.Stat(spoolPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != int64(len("line0\n")) {
		t.Fatalf("前置状态应为盘上 %d 字节，实际 %d", len("line0\n"), info.Size())
	}

	fw, err := failAuthorInjectedWrite(t, w, dt, 3)

	// 前提确认：短写真的发生了、残行真的产生了——否则本用例是空转。
	if fw.accepted != 3 {
		t.Fatalf("注入的短写应只写进 3 字节，实际 %d", fw.accepted)
	}
	if want := int64(len("line0\n") + 3); fw.sizeAtFail != want {
		t.Fatalf("失败瞬间盘上应是 %d 字节（完整行 + 3 残字节），实际 %d", want, fw.sizeAtFail)
	}
	if !errors.Is(err, errInjectedWriteFailure) {
		t.Fatalf("错误链必须保留原始写失败（调用方要能 errors.Is 判断 errno）: %v", err)
	}
	if !strings.Contains(err.Error(), quarantineSuffix) {
		t.Fatalf("错误信息应指明隔离件路径，实际: %v", err)
	}

	// 核心断言：残行被抹掉——隔离件里只剩完整行，一个残字节都不留。
	qPath := quarantinePath(spoolPath, ref0.Part)
	got, err := os.ReadFile(qPath)
	if err != nil {
		t.Fatalf("隔离件应存在（%s）: %v", qPath, err)
	}
	if string(got) != "line0\n" {
		t.Fatalf("隔离件应只保留完整行 %q，实际 %q（残行没被抹掉）", "line0\n", got)
	}
	if !strings.HasSuffix(string(got), "\n") {
		t.Fatalf("隔离件必须以换行收尾（无未终止尾行），实际 %q", got)
	}
	if _, err := os.Stat(spoolPath); !os.IsNotExist(err) {
		t.Fatalf("原名下的 spool 应已被改名隔离，实际 err=%v", err)
	}
}

// TestWriterPoisonedBlockCannotBeSealed 锁定「写失败后该块明确不可封块」。
//
// 覆盖毒化块的四个可观察性质：
//  1. PendingSeals 不再报出它（即使阈值只有 1 字节）——进程内封块路径被堵死；
//  2. 隔离件名字不满足 sealer 的 ".jsonl" 后缀过滤（任务 6 的冻结规则）
//     —— 进程外目录扫描路径也被堵死；
//  3. 该名字仍被 parsePartSeq 接受 ⇒ 块号 N 被占住、不被复用
//     （复用会让 PG 里旧的 (part=N,row=r) 静默读到另一个 part 的正文）；
//  4. 后续 Append 自动换到 N+1（行号归零），Close 对毒化块只释放 fd 且不报错。
func TestWriterPoisonedBlockCannotBeSealed(t *testing.T) {
	root := t.TempDir()
	p := NewPaths(root)
	w, err := NewWriter(WriterOptions{Root: root, Inst: instBlue, BlockBytes: 1})
	if err != nil {
		t.Fatal(err)
	}

	const dt = "2026-09-24"
	ref0, err := w.Append(dt, []byte("line0\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.PendingSeals(); err != nil { // flush 前置行
		t.Fatal(err)
	}
	if _, err := failAuthorInjectedWrite(t, w, dt, 3); err == nil {
		t.Fatal("注入短写后 Append 必须报错")
	}

	spoolPath := p.SpoolFile(ref0)
	qPath := quarantinePath(spoolPath, ref0.Part)

	// 1) 不可封块。
	tasks, err := w.PendingSeals()
	if err != nil {
		t.Fatalf("毒化块的 PendingSeals 不该报错（它只是被跳过）: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("毒化块必须不可封块，实际报出 %d 个任务", len(tasks))
	}

	// 2) sealer 的发现规则看不见它（与任务 6 的 ".jsonl" 后缀过滤一致）。
	name := filepath.Base(qPath)
	if strings.HasSuffix(name, ".jsonl") {
		t.Fatalf("隔离件名字 %q 必须不满足 sealer 的 .jsonl 后缀过滤", name)
	}

	// 3) 写指针仍占着序号 N（否则 N 会被复用）。
	if seq, ok := parsePartSeq(name); !ok || seq != ref0.Part {
		t.Fatalf("隔离件名字 %q 应被 parsePartSeq 接受且序号为 %d，实际 ok=%v seq=%d",
			name, ref0.Part, ok, seq)
	}

	// 4) Close 毒化块：只释放 fd，不重复上报那次写失败。
	if err := w.Close(); err != nil {
		t.Fatalf("毒化块的 Close 应只释放 fd 并返回 nil，实际 %v", err)
	}

	// 4b) 后续 Append 自动换到新 part、行号归零——调用方无需任何重置。
	ref1, err := w.Append(dt, []byte("line3\n"))
	if err != nil {
		t.Fatalf("毒化后应能自动换到新 part: %v", err)
	}
	if ref1.Part != ref0.Part+1 || ref1.Row != 0 {
		t.Fatalf("毒化后应换到 part-%06d#row=0，实际 %s", ref0.Part+1, ref1)
	}
	if _, err := w.PendingSeals(); err != nil { // flush 新块
		t.Fatal(err)
	}
	got, err := os.ReadFile(p.SpoolFile(ref1))
	if err != nil {
		t.Fatalf("新块 spool 应存在: %v", err)
	}
	if string(got) != "line3\n" {
		t.Fatalf("新块内容应为 %q，实际 %q", "line3\n", got)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestWriterQuarantineRenameFailureEmptiesSpool 覆盖隔离的退路分支：
// 改名失败时必须清空文件，绝不允许留下一个「看起来正常、会被 sealer 烤进产物」
// 的 spool（0 字节 spool 只会封出 0 行产物，该 part 的读请求一律行号越界）。
//
// 判据：删掉 poisonCurLocked 里 os.Truncate 的退路，本用例转红。
func TestWriterQuarantineRenameFailureEmptiesSpool(t *testing.T) {
	root := t.TempDir()
	p := NewPaths(root)
	w, err := NewWriter(WriterOptions{Root: root, Inst: instBlue, BlockBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	const dt = "2026-09-24"
	ref0, err := w.Append(dt, []byte("line0\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.PendingSeals(); err != nil {
		t.Fatal(err)
	}

	// 制造改名失败：把隔离目标名建成**非空目录**（rename 到非空目录必失败）。
	qPath := quarantinePath(p.SpoolFile(ref0), ref0.Part)
	if err := os.MkdirAll(qPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(qPath, "occupied"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := failAuthorInjectedWrite(t, w, dt, 3); err == nil {
		t.Fatal("注入短写后 Append 必须报错")
	}

	// 退路生效：原名下的 spool 仍在，但已被清空（残行没了）。
	info, err := os.Stat(p.SpoolFile(ref0))
	if err != nil {
		t.Fatalf("改名失败时原名下的 spool 应保留: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("改名失败时必须清空 spool（否则残行会被 sealer 烤进产物），实际 %d 字节", info.Size())
	}

	// 该块同样被毒化：不可封块，且下一次 Append 换到新 part。
	tasks, err := w.PendingSeals()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("毒化块必须不可封块，实际报出 %d 个任务", len(tasks))
	}
	ref1, err := w.Append(dt, []byte("line3\n"))
	if err != nil {
		t.Fatal(err)
	}
	if ref1.Part != ref0.Part+1 || ref1.Row != 0 {
		t.Fatalf("毒化后应换到 part-%06d#row=0，实际 %s", ref0.Part+1, ref1)
	}
}

// TestWriterMarkSealedClosesPreviousFileHandle 是 MarkSealed「关闭旧句柄」修复的
// 回归防线：只把 cur 置 nil 会让每封一块泄漏一个 fd（长跑进程最终 EMFILE）。
//
// 【为什么放包内】fd 不经任何公开 API 暴露，只能持有未导出的 cur.f 观察它。
// 见本文件头的说明。
func TestWriterMarkSealedClosesPreviousFileHandle(t *testing.T) {
	root := t.TempDir()
	w, err := NewWriter(WriterOptions{Root: root, Inst: instBlue, BlockBytes: 1 << 30})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	const dt = "2026-09-24"
	ref, err := w.Append(dt, []byte("line0\n"))
	if err != nil {
		t.Fatal(err)
	}

	w.mu.Lock()
	old := w.cur.f
	w.mu.Unlock()

	w.MarkSealed(dt, ref.Part)

	if _, err := old.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("MarkSealed 应关闭被替换的旧句柄（否则每块泄漏一个 fd），实际 err=%v", err)
	}
}
