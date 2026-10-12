package tracesealer_test

// ⛔ 本文件**故意不进任何 MANIFEST_FILES**（计划任务 10 步骤 1 的明文规定）。
//
// 它需要本机有 C 版 zstd（支持 --long）才能真封一块。有 ⇒ 它就是「镜像实现对**真实
// 产物**解析正确」的人工复核项；没有 ⇒ 跳过，运行时证据改由任务 8 的 A3 环境（那里
// 有真实产物）补，并在宿主执行登记表里如实标注「待宿主执行」。
//
// 【为什么跳到别处补也算数，而不是把这条用例塞进清单】清单里的用例必须每次运行都
// **真的执行**（工具的 verify() 会把 SKIP 判成证据无效）；把一条「本机可能跑不了」
// 的用例塞进去，只会让整张表在缺 zstd 的机器上失效。这正是计划把它列为人工复核项
// 的原因。
//
// 【为什么不用 `-short` 跳过】本仓库既有约定是「找不到二进制就 Skip 并说明缺什么」
// （见 tests/tracearchive/helpers_test.go 的 requireZstd）—— 与之一致。

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	ta "github.com/example/llm-gateway/internal/tracearchive"
	"github.com/example/llm-gateway/internal/tracesealer"
	"github.com/stretchr/testify/require"
)

// TestFooterRoundTripOnRealArtifact 用**冻结包的真 Sealer** 封一块，再用镜像实现
// `tracesealer.RowCount` 解析它的 footer，断言行数等于封块时的行数。
//
// 这条往返测是「字节偏移对不对」的**唯一**真实产物证据：手工拼 footer 的用例只能
// 证明「我们按自己以为的布局解析」，证明不了「冻结包就是按这个布局写的」。
func TestFooterRoundTripOnRealArtifact(t *testing.T) {
	bin, err := exec.LookPath("zstd")
	if err != nil {
		t.Skip("未找到 zstd 二进制：真实产物往返测在本机不可执行；按计划任务 10，运行时证据改由任务 8 的 A3 环境补（登记见 .superpowers/sdd/2026-10-08-trace-sealer-deployment/p4-task-10-host-deferral-*.txt）")
	}
	out, err := exec.Command(bin, "--version").CombinedOutput()
	require.NoError(t, err)
	// 输出形如：*** Zstandard CLI (64-bit) v1.5.7, by Yann Collet ***
	i := strings.Index(string(out), " v")
	require.GreaterOrEqual(t, i, 0, "无法从 zstd 版本输出解析版本号: %q", string(out))
	rest := string(out)[i+2:]
	j := strings.IndexByte(rest, ',')
	require.GreaterOrEqual(t, j, 0, "无法从 zstd 版本输出解析版本号: %q", string(out))
	ver := rest[:j]

	root := t.TempDir()
	const dt, inst = "2026-10-08", "blue"
	lines := [][]byte{[]byte(`{"ts":"2026-10-08T01:02:03+08:00"}`), []byte(`{"ts":"2026-10-08T01:02:04+08:00"}`), []byte(`{"ts":"2026-10-08T01:02:05+08:00"}`)}

	spool := ta.NewPaths(root).SpoolFile(ta.Ref{DT: dt, Inst: inst, Part: 0})
	require.NoError(t, os.MkdirAll(ta.NewPaths(root).SpoolDir(dt, inst), 0o755))
	var buf bytes.Buffer
	for _, l := range lines {
		buf.Write(l)
		buf.WriteByte('\n')
	}
	require.NoError(t, os.WriteFile(spool, buf.Bytes(), 0o644))

	s, err := ta.NewSealer(ta.SealerOptions{Root: root, ZstdBinary: bin, ExpectedZstdVersion: ver})
	require.NoError(t, err)
	require.NoError(t, s.Seal(context.Background(), ta.SealTask{
		DT: dt, Inst: inst, Part: 0, SpoolPath: spool, Rows: len(lines),
	}))

	partPath := ta.NewPaths(root).PartFile(ta.Ref{DT: dt, Inst: inst, Part: 0})
	got, err := tracesealer.RowCount(partPath)
	require.NoError(t, err)
	require.Equal(t, len(lines), got,
		"镜像实现解析真实产物的 row_count 必须等于封块时的行数（偏移错一个字节就会红）")
}
