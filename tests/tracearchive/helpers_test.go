package tracearchive_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	ta "github.com/example/llm-gateway/internal/tracearchive"
)

// 本文件是 B 层（外部测试包）的封块器测试辅助。
//
// ⚠️ 与 internal/tracearchive/testhelpers_test.go（A 层）里的三个同名函数
// 【故意重复】：Go 的测试辅助函数不能跨包共享（两个包是不同的包，_test.go
// 之间无法互相引用）。请不要为了「去掉重复」把任何一个函数搬到生产代码里，
// 那会把测试专用代码塞进生产 API。
//
// 两个文件除限定符（ta. / 无）外应保持一致，修改一处请同步另一处。

// requireZstd 返回 zstd 可执行文件路径；找不到就跳过。
//
// ⚠️ CI 镜像必须安装 zstd（C 版，支持 --long）：否则本包所有真封块用例
// 会静默跳过，「封块产物是否正确」就没有任何自动化证据。
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
