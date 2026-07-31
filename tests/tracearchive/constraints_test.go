// tests/tracearchive/constraints_test.go
//
// B 层（package tracearchive_test）：只用公开 API，断言规格 §8.3 的空间硬约束
// 在**端到端**路径上成立，外加读取侧最后一条未被覆盖的错误分支。
//
// 本文件的断言全部是「文件系统可观测结果」，没有一条需要未导出符号。
package tracearchive_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ta "github.com/example/llm-gateway/internal/tracearchive"
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

// dirEntryCount 统计目录的**直接**子项数（不递归）。
func dirEntryCount(t *testing.T, dir string) int {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return len(ents)
}

// scratchTempDir 把本进程的临时目录指向一个专属目录并返回它。
//
// 【为什么需要第二个观测量】规格 §8.3 C2 是「不落任何临时文件」，它不止约束归档
// 根目录。简报把「解压到 /tmp 再读」点名为本用例应当抓住的反模式，而**只盯
// root 的 dirUsage 对它是盲的**（/tmp 不在 root 下）。因此这里额外观测进程临时目录：
// 实现若用 os.CreateTemp("")/os.TempDir() 落临时文件，就会在此处被发现。
//
// 残余限制（如实登记）：写死在代码里的绝对路径（如 "/tmp/x"）仍观测不到——
// 本用例覆盖的是「通过 TMPDIR 解析临时目录」这一主流形态。
func scratchTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	if got := os.TempDir(); got != dir {
		t.Fatalf("TMPDIR 未生效: os.TempDir()=%q 期望 %q（否则本用例的第二个观测量会静默失效）",
			got, dir)
	}
	return dir
}

// TestReadsDoNotGrowDiskUsage 是验收用例 A4：
// 规格 §8.3 的 C1~C4 是强制性约束，读取千次后磁盘占用增量必须为 0。
//
// 若实现被改成「解压到 /tmp 再读」或「把解压结果落盘缓存」，此测试会立刻转红。
//
// 【本用例抓得到什么、抓不到什么（如实登记）】
//   - 抓得到：读取期间在 root 下新增/增长的文件，以及通过 TMPDIR 落下的临时文件；
//   - 抓不到：① 写死在代码里的绝对路径（如 "/tmp/x"）；
//     ② C3「不缓存**已解压块**」的内存形态 —— 那是堆内存约束，磁盘占用观测不到。
//     本计划没有任何用例约束 C3 的内存形态（已登记在任务报告里，不是静默遗漏）。
func TestReadsDoNotGrowDiskUsage(t *testing.T) {
	root := t.TempDir()
	scratch := scratchTempDir(t)
	lines := [][]byte{
		[]byte(`{"request_id":"a"}`),
		[]byte(`{"request_id":"b"}`),
		[]byte(`{"request_id":"c"}`),
	}
	sealLines(t, root, "2026-09-24", "blue", 0, lines)

	r, err := ta.NewReader(ta.ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	// 预热一次，排除首次读取可能的一次性分配
	if _, err := r.GetBody(context.Background(), ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: 0}); err != nil {
		t.Fatal(err)
	}
	before := dirUsage(t, root)
	beforeScratch := dirEntryCount(t, scratch)

	for i := 0; i < 1000; i++ {
		if _, err := r.GetBody(context.Background(), ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: i % 3}); err != nil {
			t.Fatalf("第 %d 次读取失败: %v", i, err)
		}
	}
	if after := dirUsage(t, root); after != before {
		t.Fatalf("读取 1000 次后磁盘占用从 %d 变为 %d（违反 §8.3 C1~C4）", before, after)
	}
	if after := dirEntryCount(t, scratch); after != beforeScratch {
		t.Fatalf("读取 1000 次后进程临时目录（TMPDIR=%s）从 %d 项变为 %d 项"+
			"（C2「不落任何临时文件」）", scratch, beforeScratch, after)
	}
}

// TestUnsealedReadsAlsoDoNotGrow 覆盖 spool 兜底路径的空间约束。
//
// 与上一条的分工：上一条走**产物路径**（解压 + trailer 定位），这一条走
// **未封块的 spool 路径**（readFromSpool 顺序读）。两条路径的空间约束都是规格
// §8.3 的要求，且实现互不相同（一个是 zstd 流式解压，一个是文件顺序读）。
func TestUnsealedReadsAlsoDoNotGrow(t *testing.T) {
	root := t.TempDir()
	lines := [][]byte{[]byte(`{"request_id":"h0"}`), []byte(`{"request_id":"h1"}`)}
	writeSpool(t, root, "2026-09-24", "blue", 0, lines)

	r, err := ta.NewReader(ta.ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if _, err := r.GetBody(context.Background(), ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: 0}); err != nil {
		t.Fatal(err)
	}
	before := dirUsage(t, root)
	for i := 0; i < 500; i++ {
		if _, err := r.GetBody(context.Background(), ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: i % 2}); err != nil {
			t.Fatalf("第 %d 次读取失败: %v", i, err)
		}
	}
	if after := dirUsage(t, root); after != before {
		t.Fatalf("spool 读取后磁盘占用从 %d 变为 %d", before, after)
	}
}

// TestMissingArtifactAfterStatIsReportedAsOpenError 覆盖 reader.go:116-118
// （readFromPart 里 os.Open 失败的包装分支）。该分支此前**零覆盖**（复审登记为疑点，
// 本任务实测确认：`go test -cover` 之外，全仓没有任何用例构造出「Stat 成功、Open 失败」）。
//
// 构造：产物路径上放一个 mode 0o000 的普通文件 —— os.Stat 不需要读权限因而成功
// （GetBody 据此进入 readFromPart 的产物分支），os.Open 因缺读权限返回 EACCES。
// 这正是「索引查找与读取之间产物被删/被改权限」这一类 TOCTOU 窗口的可复现形态，
// 且**不需要改动生产代码去注入钩子**。
//
// 【以 root 身份运行时例外】root 不受文件权限位约束 ⇒ EACCES 不出现，本用例的
// 前置条件不成立，因此显式 SKIP（而不是让它变成一条「看起来通过但什么都没测」的用例）。
// SKIP 会让变异工具把整轮证据判为**无效**（全层级 SKIP 必须为 0），因此这条限制
// 是响亮的：以 root 重跑证据时会整轮失败，而不是静默丢一条覆盖。
func TestMissingArtifactAfterStatIsReportedAsOpenError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("以 root 运行时文件权限位不生效（EACCES 不会出现），无法构造「Stat 成功、Open 失败」")
	}
	root := t.TempDir()
	ref := ta.Ref{DT: "2026-09-24", Inst: "blue", Part: 0, Row: 0}
	partPath := ta.NewPaths(root).PartFile(ref)
	if err := os.MkdirAll(filepath.Dir(partPath), 0o755); err != nil {
		t.Fatal(err)
	}
	// 内容非空：确保即使权限位意外失效（例如以 root 跑）也不会落到「文件过短」分支
	if err := os.WriteFile(partPath, []byte("本分支不应被命中"), 0o000); err != nil {
		t.Fatal(err)
	}
	// 前置复核：Stat 必须成功，且 Open 必须失败 —— 否则本用例测的是别的分支
	if _, err := os.Stat(partPath); err != nil {
		t.Fatalf("前置条件不成立：os.Stat 应当成功（Stat 不需要读权限），实际 %v", err)
	}
	if _, err := os.Open(partPath); err == nil {
		t.Fatalf("前置条件不成立：os.Open 应当因权限失败，实际成功（以 root 运行？）")
	}

	r, err := ta.NewReader(ta.ReaderOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	_, err = r.GetBody(context.Background(), ref)
	if err == nil {
		t.Fatal("产物不可读时必须报错，不得静默返回")
	}
	if !strings.Contains(err.Error(), "打开产物失败") {
		t.Fatalf("应命中 readFromPart 的「打开产物失败」包装，实际 %v", err)
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("错误链必须保留底层权限错误（EACCES），实际 %v", err)
	}
	// 不得被归入哨兵语义：既不是「正文缺失」（404），也不是「未归档」（500 类的元数据不一致）
	if errors.Is(err, ta.ErrBodyMissing) || errors.Is(err, ta.ErrNotArchived) {
		t.Fatalf("「打开产物失败」不得被归入 ErrBodyMissing/ErrNotArchived，实际 %v", err)
	}
}
