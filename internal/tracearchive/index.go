package tracearchive

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// trailer 是产物文件尾部的「行号 → 未压缩偏移」索引表。
//
// 编码方式：第一条存绝对偏移，其后每条存与上一条的差值（差分），
// 全部用 unsigned LEB128 varint 编码。理由：同一块内各行长度相近，
// 差分的数值分布集中在小整数区间，varint 比定长 8 字节省 4~6 倍。
var (
	// ErrTrailerCorrupt 表示 trailer 结构损坏（截断、条目数不符、偏移非单调）。
	ErrTrailerCorrupt = errors.New("tracearchive: trailer 损坏")
)

// appendVarint 追加一个 LEB128 varint。
// 负差分会以二补码形式编码，decodeTrailer 会据此识别非单调。
func appendVarint(dst []byte, v uint64) []byte {
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(buf[:], v)
	return append(dst, buf[:n]...)
}

// encodeTrailer 把「行号 → 未压缩偏移」表编码为差分 varint。
// offsets 必须按行号升序且单调不减；空表返回空切片。
func encodeTrailer(offsets []int64) []byte {
	if len(offsets) == 0 {
		return nil
	}
	out := make([]byte, 0, len(offsets)*3)
	out = appendVarint(out, uint64(offsets[0]))
	for i := 1; i < len(offsets); i++ {
		delta := offsets[i] - offsets[i-1]
		// 显式转为 uint64 以便负差分以二补码编码，decodeTrailer 依据高位判定
		out = appendVarint(out, uint64(delta))
	}
	return out
}

// decodeTrailer 解码 trailer，期望恰好 n 条。
// 任何长度不符、非单调、或尾部有多余字节的情况都返回 ErrTrailerCorrupt。
func decodeTrailer(b []byte, n int) ([]int64, error) {
	if n < 0 {
		return nil, fmt.Errorf("%w: 非法条目数 %d", ErrTrailerCorrupt, n)
	}
	if n == 0 {
		if len(b) != 0 {
			return nil, fmt.Errorf("%w: 期望 0 条但剩余 %d 字节", ErrTrailerCorrupt, len(b))
		}
		return nil, nil
	}

	out := make([]int64, 0, n)
	pos := 0
	for i := 0; i < n; i++ {
		v, adv := binary.Uvarint(b[pos:])
		if adv <= 0 {
			return nil, fmt.Errorf("%w: 第 %d 条 varint 解不出（剩余 %d 字节）", ErrTrailerCorrupt, i, len(b)-pos)
		}
		pos += adv
		if i == 0 {
			out = append(out, int64(v))
			continue
		}
		delta := int64(v)
		if delta < 0 {
			return nil, fmt.Errorf("%w: 第 %d 条偏移非单调（差分 %d）", ErrTrailerCorrupt, i, delta)
		}
		out = append(out, out[i-1]+delta)
	}
	if pos != len(b) {
		return nil, fmt.Errorf("%w: 解码后有 %d 字节多余", ErrTrailerCorrupt, len(b)-pos)
	}
	return out, nil
}
