package tracearchive

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	// FooterSize 是产物文件末尾定长 footer 的字节数。
	// 读取端只需 pread 末尾 32 字节即可得知索引位置，无需解压任何数据。
	FooterSize = 32

	// footerMagic 标识本格式。冷层合并若引入扩展索引键，应提升 formatVersion。
	footerMagic = "TBA4"

	// formatVersion 当前格式版本。冷层超级文件（规格 §5.5）将提升此值。
	formatVersion uint16 = 1
)

// ErrFooterCorrupt 表示产物的定长 footer 与其内容不相容。实际覆盖：
//   - footer 缺失（文件短于 FooterSize）、长度不符、magic 不符、版本未知；
//   - footer 字段自相矛盾：负的 trailer_offset/trailer_len、负 row_count；
//   - trailer 未紧邻 footer（trailer_offset + trailer_len ≠ size - FooterSize）；
//   - trailer_len 超过合理性上界 maxTrailerBytes；
//   - frame_sha256[:8] 与帧区实算不符（半写/位腐，规格 §8.2 第 3 步）；
//   - 行号越界之外的「footer 与 trailer 不相容」由 ErrTrailerCorrupt 承担
//     （条目数不符、条目数超过 maxTrailerEntries、偏移非单调等，见 index.go）。
//
// 全是「产物损坏」，对应 HTTP 500 + 诊断，绝不能静默返回「不存在」。
var ErrFooterCorrupt = errors.New("tracearchive: footer 损坏")

// footer 是产物文件末尾的定长元信息。
//
// 字段布局（共 32 字节）：
//
//	[0:4)   magic
//	[4:6)   format_version
//	[6:14)  trailer_offset
//	[14:18) trailer_len
//	[18:22) row_count
//	[22:30) frame_sha256[:8]
//	[30:32) reserved
type footer struct {
	TrailerOffset int64   // trailer 相对文件起始的偏移
	TrailerLen    int32   // trailer 字节长度
	RowCount      int32   // 块内行数。读取端据此校验 trailer 完整性并判断行号越界
	FrameSHA      [8]byte // zstd 帧字节（不含 trailer/footer）的 sha256 前 8 字节
}

// encodeFooter 编码为定长 32 字节。
func encodeFooter(f footer) []byte {
	b := make([]byte, FooterSize)
	copy(b[0:4], footerMagic)
	binary.BigEndian.PutUint16(b[4:6], formatVersion)
	binary.BigEndian.PutUint64(b[6:14], uint64(f.TrailerOffset))
	binary.BigEndian.PutUint32(b[14:18], uint32(f.TrailerLen))
	binary.BigEndian.PutUint32(b[18:22], uint32(f.RowCount))
	copy(b[22:30], f.FrameSHA[:])
	// b[30:32] 保留，恒为 0
	return b
}

// decodeFooter 解析并校验定长 footer。
func decodeFooter(b []byte) (footer, error) {
	var f footer
	if len(b) != FooterSize {
		return f, fmt.Errorf("%w: 长度应为 %d，实际 %d", ErrFooterCorrupt, FooterSize, len(b))
	}
	if string(b[0:4]) != footerMagic {
		return f, fmt.Errorf("%w: magic 不符（期望 %q）", ErrFooterCorrupt, footerMagic)
	}
	if v := binary.BigEndian.Uint16(b[4:6]); v != formatVersion {
		return f, fmt.Errorf("%w: 未知 format_version %d", ErrFooterCorrupt, v)
	}
	f.TrailerOffset = int64(binary.BigEndian.Uint64(b[6:14]))
	f.TrailerLen = int32(binary.BigEndian.Uint32(b[14:18]))
	f.RowCount = int32(binary.BigEndian.Uint32(b[18:22]))
	copy(f.FrameSHA[:], b[22:30])

	if f.TrailerOffset < 0 || f.TrailerLen < 0 {
		return f, fmt.Errorf("%w: 负偏移或负长度（off=%d len=%d）", ErrFooterCorrupt, f.TrailerOffset, f.TrailerLen)
	}
	if f.RowCount < 0 {
		return f, fmt.Errorf("%w: 负 row_count %d", ErrFooterCorrupt, f.RowCount)
	}
	return f, nil
}
