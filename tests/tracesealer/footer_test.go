package tracesealer_test

// 本文件是 B 层测试（tests/tracesealer，package tracesealer_test）：只用公开 API
// （tracesealer.RowCount / tracesealer.FooterSize / tracesealer.ErrFooterCorrupt）。
//
// 「产物即契约」：这些用例钉住的是**镜像实现**的字节偏移与失败语义 —— 冻结包的
// footer 编码（magic / 版本 / 字段次序 / 32 字节长度）全部未导出，本包只能复制一份；
// 复制品一旦漂移，A8 的「行数分布」读到的就是**别的字节**，而它不会报错。
// 因此下面所有手工拼出的 footer 都用**字面量 32 与字面量偏移**，不从任何导出常量取值
// —— 断言两边一起错就同时绿，那等于没钉。

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/example/llm-gateway/internal/tracesealer"
	"github.com/stretchr/testify/require"
)

// buildFooter 按冻结包 footer.go 的字段布局手工拼一个 32 字节 footer：
//
//	[0:4)   magic "TBA4"
//	[4:6)   format_version 1（大端 uint16）
//	[6:14)  trailer_offset（大端 uint64 ⇒ int64）
//	[14:18) trailer_len（大端 uint32 ⇒ int32）
//	[18:22) row_count（大端 uint32 ⇒ int32）
//	[22:30) frame_sha256[:8]
//	[30:32) reserved
func buildFooter(rowCount int32, trailerOffset int64, trailerLen int32) []byte {
	b := make([]byte, 32)
	copy(b[0:4], "TBA4")
	binary.BigEndian.PutUint16(b[4:6], 1)
	binary.BigEndian.PutUint64(b[6:14], uint64(trailerOffset))
	binary.BigEndian.PutUint32(b[14:18], uint32(trailerLen))
	binary.BigEndian.PutUint32(b[18:22], uint32(rowCount))
	copy(b[22:30], []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88})
	return b
}

// writeArtifactWithRawFooter 写一个 totalBytes 字节的产物：末尾 len(footer) 字节原样用
// 给定 footer，其余是确定性的填充字节。父目录会被创建。
func writeArtifactWithRawFooter(t *testing.T, path string, totalBytes int, footer []byte) {
	t.Helper()
	require.GreaterOrEqual(t, totalBytes, len(footer))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	body := make([]byte, totalBytes-len(footer))
	for i := range body {
		body[i] = byte(i % 251)
	}
	require.NoError(t, os.WriteFile(path, append(body, footer...), 0o644))
}

// writeArtifactWithFooter 写一个 trailer 长度为 0（即 trailer 紧邻 footer）的**自洽**产物：
//   - 文件总长 = totalBytes；
//   - trailer_offset = totalBytes - 32、trailer_len = 0 ⇒ 满足冻结包的
//     「trailer 必须紧邻 footer」判据；
//   - row_count = rows。
func writeArtifactWithFooter(t *testing.T, path string, totalBytes int, rows int32) {
	t.Helper()
	require.GreaterOrEqual(t, totalBytes, 32)
	writeArtifactWithRawFooter(t, path, totalBytes, buildFooter(rows, int64(totalBytes-32), 0))
}

// TestFooterRowCountOnHandBuiltFooter 纯函数证据（不需要 zstd）：按冻结布局手工拼出的
// 32 字节，解析结果必须等于写进去的 row_count。
//
// 【它是 m01/m02 的在册杀手】
//   - m01 把 FooterSize 从 32 改成 24 ⇒ 本用例在第一条 require.Equal 处转红（显式常量
//     断言），即使那条被绕过，解析也会因 magic 落在错位置而报错 ⇒ 第二条断言红；
//   - m02 把读取偏移减一个字节 ⇒ magic 不再落在 footer 首位 ⇒ RowCount 报错 ⇒ 红。
func TestFooterRowCountOnHandBuiltFooter(t *testing.T) {
	require.Equal(t, 32, tracesealer.FooterSize,
		"FooterSize 必须严格等于冻结包 footer.go 的 footerSize（32）")

	dir := t.TempDir()
	path := filepath.Join(dir, "part-000007.jsonl.zst")
	writeArtifactWithFooter(t, path, 4096, 4242)

	got, err := tracesealer.RowCount(path)
	require.NoError(t, err)
	require.Equal(t, 4242, got, "解析出的行数必须就是 footer 里写进去的 row_count")
}

// TestFooterRejectsShortFile 文件不足 32 字节 ⇒ **报错**，绝不返回 0 冒充「0 行」。
//
// 【为什么这条比看上去重要】0 行是一个**合法读数**（空块）。把「读不到」折成 0，
// A8 的分布会在产物损坏时画成「块都很小」，而没有任何东西报警 —— 这正是本计划
// 反复登记的那一类「观测面自己失真」。
//
// 【它是 m03 的在册杀手】m03 把短文件分支改成 `return 0, nil`。
func TestFooterRejectsShortFile(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []int{0, 1, 31} {
		path := filepath.Join(dir, fmt.Sprintf("short-%d.jsonl.zst", n))
		require.NoError(t, os.WriteFile(path, bytes.Repeat([]byte{0xAB}, n), 0o644))

		got, err := tracesealer.RowCount(path)
		require.Errorf(t, err, "%d 字节的文件必须报错（不是返回 0）", n)
		require.Zero(t, got, "出错时返回的必须是零值，且调用方必须看 error 而不是这个 0")
		require.ErrorIs(t, err, tracesealer.ErrFooterCorrupt, "必须是「产物损坏」这一类，不是「文件不存在」")
	}
}

// TestFooterRejectsBadMagic 末尾 32 字节不是本格式的 footer ⇒ 报错。
// 覆盖「文件长度够、但那段字节是别的东西」这一形态。
//
// 【产物只违反**一条**判据】trailer 仍然紧邻 footer（offset=96、len=0、size=128）：
// 负例产物若同时违反两条判据，就**证不出**这一条判据单独承重 —— 把它删掉，另一条
// 判据仍会把同一个产物拦下，用例照样绿。下面的版本 / 负 row_count / 未紧邻三个负例
// 同理，各自只违反一条。
func TestFooterRejectsBadMagic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "part-000001.jsonl.zst")
	f := buildFooter(7, 96, 0)
	copy(f[0:4], "TBA5")
	writeArtifactWithRawFooter(t, path, 128, f)

	got, err := tracesealer.RowCount(path)
	require.ErrorIs(t, err, tracesealer.ErrFooterCorrupt)
	require.Zero(t, got)
}

// TestFooterRejectsUnknownFormatVersion 版本未知 ⇒ 报错（未来格式的 footer 不得被
// 当成当前格式解析）。同样只违反「版本」这一条判据。
func TestFooterRejectsUnknownFormatVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "part-000002.jsonl.zst")
	f := buildFooter(7, 96, 0)
	binary.BigEndian.PutUint16(f[4:6], 2)
	writeArtifactWithRawFooter(t, path, 128, f)

	got, err := tracesealer.RowCount(path)
	require.ErrorIs(t, err, tracesealer.ErrFooterCorrupt)
	require.Zero(t, got)
}

// TestFooterRejectsTrailerNotAdjacentFooter 钉住「trailer 必须紧邻 footer」
// （trailer_offset + trailer_len == size − FooterSize）。footer 自身的字段自洽，
// 但与文件长度不相容 ⇒ 仍是产物损坏。
func TestFooterRejectsTrailerNotAdjacentFooter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "part-000003.jsonl.zst")
	// 文件 128 字节、footer 32 字节 ⇒ 紧邻时 trailer_offset 必须是 96；这里写成 95。
	writeArtifactWithRawFooter(t, path, 128, buildFooter(7, 95, 0))

	got, err := tracesealer.RowCount(path)
	require.ErrorIs(t, err, tracesealer.ErrFooterCorrupt)
	require.Zero(t, got)
}

// TestFooterRejectsNegativeRowCount 负 row_count 是自相矛盾的 footer ⇒ 报错
// （不得被折成一个负数读数、更不得被当成 0）。
func TestFooterRejectsNegativeRowCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "part-000004.jsonl.zst")
	writeArtifactWithRawFooter(t, path, 128, buildFooter(-1, 96, 0))

	got, err := tracesealer.RowCount(path)
	require.ErrorIs(t, err, tracesealer.ErrFooterCorrupt)
	require.Zero(t, got)
}

// TestFooterRejectsMissingFile 产物不存在 ⇒ 报错，且错误身份是「文件不存在」
// （与「产物损坏」分开：前者是「这个 part 还没封」，后者要按事故看）。
func TestFooterRejectsMissingFile(t *testing.T) {
	got, err := tracesealer.RowCount(filepath.Join(t.TempDir(), "nope.jsonl.zst"))
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Zero(t, got)
	require.NotErrorIs(t, err, tracesealer.ErrFooterCorrupt)
}
