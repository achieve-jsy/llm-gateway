package tracearchive_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ta "github.com/example/llm-gateway/internal/tracearchive"
)

// 本文件是 B 层（外部测试包）的封块器测试辅助。
//
// ⚠️ 与 internal/tracearchive/testhelpers_test.go（A 层）里的同名函数
// 【故意重复】：Go 的测试辅助函数不能跨包共享（两个包是不同的包，_test.go
// 之间无法互相引用）。请不要为了「去掉重复」把任何一个函数搬到生产代码里，
// 那会把测试专用代码塞进生产 API。
//
// 两个文件除限定符（ta. / 无）外应保持一致，修改一处请同步另一处。

// fakeZstdVersion 是假 zstd 应答 `--version` 时报告的版本。
//
// ⚠️ 必须等于生产默认值 defaultZstdVersion（internal/tracearchive/sealer.go）。
// 两者不一致时，所有用假 zstd 的用例都会因为「版本不符」而转红 —— 这是故意的：
// 改默认版本必须被看见，而不是悄悄放过。
const fakeZstdVersion = "1.4.8"

// requireZstd 返回 (zstd 可执行文件路径, 本机版本)；找不到就跳过。
//
// 返回版本是刻意的：真封块用例必须把本机版本传给 SealerOptions.ExpectedZstdVersion，
// 否则会被版本守卫（3a）拒绝。这样「本机 zstd 版本」在每条真封块用例里都是显式的，
// 换机器/换版本时不会被静默吞掉。
//
// ⚠️ CI 镜像必须安装 zstd（C 版，支持 --long）：否则本包所有真封块用例
// 会静默跳过，「封块产物是否正确」就没有任何自动化证据。
func requireZstd(t *testing.T) (string, string) {
	t.Helper()
	bin, err := exec.LookPath("zstd")
	if err != nil {
		t.Skip("未找到 zstd 二进制：方案 C 的封块路径需要 C 版 zstd CLI（CI 镜像必须安装）")
	}
	return bin, zstdVersion(t, bin)
}

// zstdVersion 探测本机 zstd 的 X.Y.Z（测试侧独立实现，不依赖生产代码的解析）。
// 输出形如：*** Zstandard CLI (64-bit) v1.5.7, by Yann Collet ***
func zstdVersion(t *testing.T, bin string) string {
	t.Helper()
	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		t.Fatalf("探测 zstd 版本失败（%s --version）: %v", bin, err)
	}
	s := string(out)
	i := strings.Index(s, " v")
	if i < 0 {
		t.Fatalf("无法从 zstd 版本输出解析版本号: %q", s)
	}
	rest := s[i+2:]
	j := strings.IndexByte(rest, ',')
	if j < 0 {
		t.Fatalf("无法从 zstd 版本输出解析版本号: %q", s)
	}
	return rest[:j]
}

// fakeZstd 写一个可执行的假 zstd 脚本，用于固定失败语义与产物字节布局。
// script 是「#!/bin/sh」之后的内容，例如 "exec cat" 或 "exit 3"。
// 假 zstd 会先应答 `--version`（报告 fakeZstdVersion），其余参数交给 script。
func fakeZstd(t *testing.T, script string) string {
	t.Helper()
	return fakeZstdWithVersion(t, fakeZstdVersion, script)
}

// fakeZstdWithVersion 与 fakeZstd 相同，但 `--version` 报告指定版本，
// 供版本守卫（3a）的用例使用。
func fakeZstdWithVersion(t *testing.T, version, script string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "zstd")
	if err := os.WriteFile(p, []byte(fakeZstdScript(version, script)), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// fakeZstdScript 生成假 zstd 的完整脚本体。
//
// version 为空串表示「不处理 --version」（脚本会把 --version 当普通调用执行），
// 用于制造「版本输出无法解析」的用例。
func fakeZstdScript(version, script string) string {
	body := "#!/bin/sh\n"
	if version != "" {
		body += "if [ \"$1\" = \"--version\" ]; then\n" +
			"  echo '*** Zstandard CLI (64-bit) v" + version + ", by Yann Collet ***'\n" +
			"  exit 0\n" +
			"fi\n"
	}
	return body + script
}

// listPendingSeals 是发现路径的唯一入口（裁决 2：包级函数已删除，只保留方法）。
// 用公开 API 构造 Sealer 并调用方法；minAge 必须显式传入（不留 0）。
func listPendingSeals(t *testing.T, root string, minAge time.Duration) ([]ta.SealTask, error) {
	t.Helper()
	s, err := ta.NewSealer(ta.SealerOptions{Root: root, MinAge: minAge})
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	return s.ListPendingSeals()
}

// sealLines 辅助：写 spool 并封块，供任务 7（按定位键点读）的产物路径用例使用。
//
// 方案 C：封块路径需要 C 版 zstd CLI，故先 requireZstd（缺失则整组用例 skip）。
// 只用公开 API（ta.NewSealer/ta.SealerOptions/ta.SealTask），不触达未导出符号。
//
// 【与简报 task-7-brief.md:37-50 的差异（必须在报告里指出）】简报写的是
//
//	bin := requireZstd(t)
//	s, err := ta.NewSealer(ta.SealerOptions{Root: root, ZstdBinary: bin})
//
// 而本仓库的 requireZstd 返回 (bin, 本机版本)，且 NewSealer 在
// ExpectedZstdVersion 为空时取默认值 defaultZstdVersion = "1.4.8"（生产标定版本）。
// 照抄简报会让每一次封块都被版本守卫拒绝（本机/CI 实测 zstd 为 v1.5.7）⇒
// 任务 7 的产物路径用例全部转红。因此这里必须把本机版本透传下去——Task 6 的
// 用例也都是这么做的（见 tests/tracearchive/sealer_test.go 的 `bin, ver := requireZstd(t)`）。
//
// ⚠️ 与 A 层（internal/tracearchive/testhelpers_test.go）的同名函数【故意重复】，
// 理由见文件头；两处除限定符（ta. / 无）外保持一致。
func sealLines(t *testing.T, root, dt, inst string, part int, lines [][]byte) {
	t.Helper()
	bin, ver := requireZstd(t)
	spool := writeSpool(t, root, dt, inst, part, lines)
	s, err := ta.NewSealer(ta.SealerOptions{Root: root, ZstdBinary: bin, ExpectedZstdVersion: ver})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Seal(context.Background(), ta.SealTask{
		DT: dt, Inst: inst, Part: part, SpoolPath: spool, Rows: len(lines),
	}); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------------------
// 任务 8 追加的 B 层共享 helper（端到端往返）
// ---------------------------------------------------------------------------

// sealPending 驱动一轮「发现 → 封块 → 标记」循环：
// Writer.PendingSeals 报出待封块 → Sealer.Seal 封块（C 版 zstd CLI）→
// Writer.MarkSealed 清空当前块。这正是方案 C 下生产里 sealer 的循环
// （sealer 进程的 main 与循环入口属计划 4，任务 8 的端到端用例扮演它）。
//
// 返回本轮封掉的块数。bin/ver 由 requireZstd 取得并显式透传：版本守卫
// （sealer.go 的 verifyZstdVersion）要求调用方声明本机版本，否则真封块用例会被
// 默认值 defaultZstdVersion = "1.4.8" 拒绝（理由见本文件 sealLines 的说明）。
func sealPending(t *testing.T, w *ta.Writer, root, bin, ver string) int {
	t.Helper()
	tasks, err := w.PendingSeals()
	if err != nil {
		t.Fatalf("PendingSeals: %v", err)
	}
	for _, task := range tasks {
		sealTask(t, root, bin, ver, task)
		w.MarkSealed(task.DT, task.Part)
	}
	return len(tasks)
}

// sealTask 封一个块。task.Rows 由调用方给出（写侧自己报的行数），
// 因而会经过 sealer 的行数交叉校验——这让「写出的物理行数」也被端到端断言。
func sealTask(t *testing.T, root, bin, ver string, task ta.SealTask) {
	t.Helper()
	s, err := ta.NewSealer(ta.SealerOptions{Root: root, ZstdBinary: bin, ExpectedZstdVersion: ver})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Seal(context.Background(), task); err != nil {
		t.Fatalf("封块失败 (dt=%s inst=%s part=%d rows=%d): %v",
			task.DT, task.Inst, task.Part, task.Rows, err)
	}
}

// sealExistingSpool 对「Writer 写出的」spool 封块。
//
// 刻意不复用 writeSpool + sealLines：后者会用 writeSpool 重建 spool 文件，
// 把写入器刚写的字节覆盖掉——本 helper 只封既有的那份文件。
// 顺带断言 I5：封块成功后 spool 必须已被删除（否则「产物与 spool 不会同时存在」
// 这条读取侧的前提就被破坏了）。
func sealExistingSpool(t *testing.T, root, dt, inst string, part, rows int) {
	t.Helper()
	bin, ver := requireZstd(t)
	spool := ta.NewPaths(root).SpoolFile(ta.Ref{DT: dt, Inst: inst, Part: part})
	sealTask(t, root, bin, ver, ta.SealTask{
		DT: dt, Inst: inst, Part: part, SpoolPath: spool, Rows: rows,
	})
	if _, err := os.Stat(spool); !os.IsNotExist(err) {
		t.Fatalf("封块成功后 spool 必须已被删除（I5）: %s (stat err=%v)", spool, err)
	}
}

// concatLines 把若干行按 spool 的物理布局拼成一个字节串（每行后跟一个 '\n'），
// 供「盘上字节必须逐字节等于写入内容」的断言使用。
func concatLines(lines [][]byte) []byte {
	var buf bytes.Buffer
	for _, l := range lines {
		buf.Write(l)
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

// writeSpool 辅助：把若干行写入指定块的 spool 文件，返回文件路径。
// 只用公开 API（ta.NewPaths/ta.Ref），因此不依赖实现内部字段。
//
// lines 为 nil 时写出一个 0 字节的 spool（空块是合法状态，见
// TestSealAcceptsEmptySpool）。
func writeSpool(t *testing.T, root, dt, inst string, part int, lines [][]byte) string {
	t.Helper()
	path := ta.NewPaths(root).SpoolFile(ta.Ref{DT: dt, Inst: inst, Part: part})
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
