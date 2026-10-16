package tracesealer_test

// 本文件是 B 层测试（tests/tracesealer，package tracesealer_test）：在**已提交的真实产物**
// fixture（`testdata/footer_roundtrip_real.jsonl.zst`）上钉住「**冻结包真的按这个布局写**」
// 这条行为要求 —— 手工拼 footer 的用例只能证明「我们按自己以为的布局解析」，证明不了
// 「冻结包就是按这个布局写的」。
//
// 【为什么要有这份 fixture（E73① 的修法）】原来承载这条要求的只有
// `footer_roundtrip_test.go` 的 `TestFooterRoundTripOnRealArtifact`：它要**当场用 zstd 封一块**，
// 于是缺 zstd 的机器上会 Skip —— 而清单里的用例一旦 Skip，整张权威表就判「证据无效」，
// 所以它**故意不进任何 MANIFEST_FILES** ⇒ 这条行为要求落到了自动门禁之外。
// **「需要 zstd」不是正当理由**：解析产物**不需要** zstd（冻结读侧用 Go 库
// `klauspost/compress/zstd`），只有**产出**产物需要。修法 = 把产物**预先产好并提交**，
// 再用一条**不依赖 zstd** 的用例解析它。
//
// 【fixture 的来源（可复算）】由**冻结包的真 Sealer** 在 `4a9ffbf4` 上产出：
// `ta.NewSealer(ta.SealerOptions{Root: <tmp>, ZstdBinary: <zstd>}).Seal(ctx, ta.SealTask{
// DT: "2026-10-08", Inst: "blue", Part: 7, SpoolPath: <spool>, Rows: 3})`，
// 其中 spool 的 3 行正文就是下面的 `committedRows`（逐字节）；产出机 `zstd` = **v1.5.7**
// （生产是 1.4.8 —— 产物字节已冻结在仓库里，**不需要**在测试机上复现封块，故这个版本差不影响本用例）。
// 产物 = zstd 帧 + Go 侧 trailer + 32 字节 footer，共 **104** 字节。
//
// 本文件的断言由变异集合 `tools/trace-mutate/sets/p4-task-10-footer.py` 的 m01 / m02 承载
// （改坏镜像的 `footer.go` ⇒ `RowCount` 读不出真实产物 ⇒ 本条转红）。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	ta "github.com/example/llm-gateway/internal/tracearchive"
	"github.com/example/llm-gateway/internal/tracesealer"
	"github.com/stretchr/testify/require"
)

const (
	// committedArtifact 是**已提交**的真实产物（冻结 Sealer 预产）。
	committedArtifact = "testdata/footer_roundtrip_real.jsonl.zst"
	// committedArtifactSHA256 把「本用例钉的是哪一份字节」写成可复核的常量：
	// fixture 被换掉而没人发现，这条断言会先红。
	committedArtifactSHA256 = "7f4291c592b71182a90bfa25953c648b3db5fd64383fc85af14752430e2af75e"
	// committedArtifactSize 是 fixture 的字节数（104 = zstd 帧 + trailer + 32 字节 footer）。
	committedArtifactSize = 104
	// committedDT / committedInst / committedPart 是封块时的定位键。
	committedDT   = "2026-10-08"
	committedInst = "blue"
	committedPart = 7
)

// committedRows 是封块时逐字节写进 spool 的 3 行正文（封块时的输入）。
var committedRows = [][]byte{
	[]byte(`{"ts":"2026-10-08T01:02:03+08:00","n":0}`),
	[]byte(`{"ts":"2026-10-08T01:02:04+08:00","n":1}`),
	[]byte(`{"ts":"2026-10-08T01:02:05+08:00","n":2}`),
}

// TestFooterParsesCommittedRealArtifact 在**已提交的真实产物**上钉住两件事：
//
//	① 镜像实现 `tracesealer.RowCount` 解析真实产物得到封块时的行数（3）；
//	② **冻结读侧**按 (dt, inst, part, row) 能取回逐字节相同的正文，且越界行号报
//	   `ErrNotArchived`（不许返回错行）—— 后半句才是「冻结包真按此布局写」的实证：
//	   footer 偏移/trailer 任一错位，取回的就不是那三行原文。
//
// 全程**不需要 zstd**：解析用冻结包的 Go zstd 解码器。
func TestFooterParsesCommittedRealArtifact(t *testing.T) {
	raw, err := os.ReadFile(committedArtifact)
	require.NoError(t, err,
		"读不到已提交的真实产物 fixture %s：它必须存在（缺失 ⇒ FAIL，不是跳过）", committedArtifact)

	// ① 先钉「是哪一份字节」：fixture 被替换/截断都必须在这里响。
	sum := sha256.Sum256(raw)
	require.Equal(t, committedArtifactSHA256, hex.EncodeToString(sum[:]),
		"%s 的内容必须与提交时的字节一致（sha256）", committedArtifact)
	require.Len(t, raw, committedArtifactSize, "%s 的字节数", committedArtifact)

	// ② 镜像实现解析真实产物：行数必须等于封块时的行数（偏移错一个字节就会红）。
	got, err := tracesealer.RowCount(committedArtifact)
	require.NoError(t, err, "镜像 RowCount 必须能解析**已提交的真实产物** %s", committedArtifact)
	require.Equal(t, len(committedRows), got,
		"镜像实现解析真实产物的 row_count 必须等于封块时的行数")

	// ③ 冻结读侧按定位键取回正文：逐字节相同。
	//    【为什么不能只断言行数】行数只是一个 footer 字段；「冻结包真按此布局写」要求
	//    trailer 的每个行首偏移也真的指向那一行。
	root := t.TempDir()
	ref := ta.Ref{DT: committedDT, Inst: committedInst, Part: committedPart}
	partPath := ta.NewPaths(root).PartFile(ref)
	require.NoError(t, os.MkdirAll(filepath.Dir(partPath), 0o755))
	require.NoError(t, os.WriteFile(partPath, raw, 0o644))

	r, err := ta.NewReader(ta.ReaderOptions{Root: root})
	require.NoError(t, err)
	defer func() { require.NoError(t, r.Close()) }()

	for i, want := range committedRows {
		rowRef := ref
		rowRef.Row = i
		gotLine, gerr := r.GetBody(context.Background(), rowRef)
		require.NoError(t, gerr, "冻结读侧必须能取回真实产物的第 %d 行", i)
		require.Equal(t, want, gotLine,
			"真实产物第 %d 行必须与封块时的输入逐字节相同（trailer 偏移错位会让这条红）", i)
	}

	// ④ 越界行号不是「读到别的东西」：冻结读侧必须报 ErrNotArchived（元数据与存储不一致）。
	over := ref
	over.Row = len(committedRows)
	_, oerr := r.GetBody(context.Background(), over)
	require.Error(t, oerr, "越界行号必须报错，不得返回错行")
	require.True(t, errors.Is(oerr, ta.ErrNotArchived),
		"越界行号必须报 ErrNotArchived（实际 %v）", oerr)
}
