package tracearchive

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// 本文件是 A 层（包内测试）的封块器测试辅助。
//
// ⚠️ 与 tests/tracearchive/helpers_test.go（B 层）里的三个同名函数
// 【故意重复】：Go 的测试辅助函数不能跨包共享（一个 _test.go 属于某个具体包，
// 两个包之间无法互相引用）。请不要为了「去掉重复」把任何一个函数搬进生产代码，
// 那会把测试专用代码塞进生产 API。
//
// 两个文件除限定符（ta. / 无）外保持一致，修改一处请同步另一处。

// requireZstd 返回 zstd 可执行文件路径；找不到就跳过。
//
// ⚠️ CI 镜像必须安装 zstd（C 版，支持 --long）：否则依赖真实 zstd 的用例
// 会静默跳过，方案 C 的封块路径就没有任何自动化证据。
func requireZstd(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("zstd")
	if err != nil {
		t.Skip("未找到 zstd 二进制：方案 C 的封块路径需要 C 版 zstd CLI（CI 镜像必须安装）")
	}
	return bin
}

// fakeZstd 写一个可执行的假 zstd 脚本，用于固定失败语义与产物字节布局。
// script 是「#!/bin/sh」之后的内容，例如 "exec cat" 或 "exit 3"。
func fakeZstd(t *testing.T, script string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "zstd")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
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
