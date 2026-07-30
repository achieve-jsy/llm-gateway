package tracearchive

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 本文件是 A 层（包内测试）的封块器测试辅助。
//
// ⚠️ 与 tests/tracearchive/helpers_test.go（B 层）里的同名函数
// 【故意重复】：Go 的测试辅助函数不能跨包共享（一个 _test.go 属于某个具体包，
// 两个包之间无法互相引用）。请不要为了「去掉重复」把任何一个函数搬进生产代码，
// 那会把测试专用代码塞进生产 API。
//
// 两个文件除限定符（ta. / 无）外保持一致，修改一处请同步另一处。
// （A 层只用到 requireZstd/writeSpool；其余同名函数留在本文件是为了保持两边一致。）

// fakeZstdVersion 是假 zstd 应答 `--version` 时报告的版本。
//
// ⚠️ 必须等于生产默认值 defaultZstdVersion（internal/tracearchive/sealer.go）。
// 两者不一致时，所有用假 zstd 的用例都会因为「版本不符」而转红 —— 这是故意的：
// 改默认版本必须被看见，而不是悄悄放过。
const fakeZstdVersion = "1.4.8"

// requireZstd 返回 (zstd 可执行文件路径, 本机版本)；找不到就跳过。
//
// 返回版本是刻意的：真封块用例必须把本机版本传给 SealerOptions.ExpectedZstdVersion，
// 否则会被版本守卫（3a）拒绝。
//
// ⚠️ CI 镜像必须安装 zstd（C 版，支持 --long）：否则依赖真实 zstd 的用例
// 会静默跳过，方案 C 的封块路径就没有任何自动化证据。
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
// A 层没有发现类用例，本函数留在本文件是为了与 B 层保持一致。
func listPendingSeals(t *testing.T, root string, minAge time.Duration) ([]SealTask, error) {
	t.Helper()
	s, err := NewSealer(SealerOptions{Root: root, MinAge: minAge})
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	return s.ListPendingSeals()
}

// sealLines 辅助：写 spool 并封块，供任务 7（按定位键点读）的产物路径用例使用。
//
// 与 B 层 tests/tracearchive/helpers_test.go 里的同名函数【故意重复】（理由见文件头）。
// 唯一差别是不带 ta. 限定符，且这里直接调用包内的 NewSealer/SealerOptions/SealTask。
//
// 与简报的差异同 B 层：简报写 `bin := requireZstd(t)` 且不传 ExpectedZstdVersion，
// 那会走默认值 "1.4.8" 而被版本守卫拒绝（本机实测 v1.5.7）⇒ 用例全部转红。
func sealLines(t *testing.T, root, dt, inst string, part int, lines [][]byte) {
	t.Helper()
	bin, ver := requireZstd(t)
	spool := writeSpool(t, root, dt, inst, part, lines)
	s, err := NewSealer(SealerOptions{Root: root, ZstdBinary: bin, ExpectedZstdVersion: ver})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Seal(context.Background(), SealTask{
		DT: dt, Inst: inst, Part: part, SpoolPath: spool, Rows: len(lines),
	}); err != nil {
		t.Fatal(err)
	}
}

// writeSpool 辅助：把若干行写入指定块的 spool 文件，返回文件路径。
// lines 为 nil 时写出一个 0 字节的 spool（空块是合法状态）。
func writeSpool(t *testing.T, root, dt, inst string, part int, lines [][]byte) string {
	t.Helper()
	path := NewPaths(root).SpoolFile(Ref{DT: dt, Inst: inst, Part: part})
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
