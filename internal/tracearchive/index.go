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
	// ErrTrailerCorrupt 表示 trailer 结构损坏（截断、条目数不符、偏移为负或溢出、
	// 偏移非单调、尾部有多余字节）。
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
// 任何条目数不符（含超出字节数）、偏移为负或累加溢出、非单调、
// 以及尾部有多余字节的情况都返回 ErrTrailerCorrupt。
func decodeTrailer(b []byte, n int) ([]int64, error) {
	if n < 0 {
		return nil, fmt.Errorf("%w: 非法条目数 %d", ErrTrailerCorrupt, n)
	}
	// 每条至少占 1 字节，故 n > len(b) 必为损坏。
	// 必须在 make 之前拦截：调用方的 n 来自产物 footer 的 row_count，
	// 该字段没有校验和，损坏时会给出 2^31-1 这类天文数字，
	// 直接用于定容量会造成进程级 OOM/panic，而不是返回一个错误。
	if n > len(b) {
		return nil, fmt.Errorf("%w: 期望 %d 条但仅 %d 字节", ErrTrailerCorrupt, n, len(b))
	}
	// 条目数上界（读侧内存预算的另一半，见 reader.go 的 maxTrailerEntries）。
	// n > len(b) 只挡「结构上不可能」，挡不住「结构上可能但超出预算」：len(b) 被
	// maxTrailerBytes 限制在 64 MiB，而最坏情形（每个字节都是一个 1 字节 varint）
	// 给出 n = len(b) = 64 Mi 条 ⇒ make 分配 8n = 512 MiB。实测该输入【成功解码】
	// （err=nil，不是报错），故必须在此硬拦，且同样在 make 之前。
	//
	// 超限一律 ErrTrailerCorrupt（footer 的 row_count 与 trailer 字节数不相容
	// ⇒ 产物损坏），【绝不截断】到上界条数：截断会让「损坏」伪装成一次正常读取。
	if n > maxTrailerEntries {
		return nil, fmt.Errorf("%w: 期望 %d 条超过条目数上界 %d（8 字节/条 ⇒ 索引约 %.0f MiB，超出读侧内存预算）",
			ErrTrailerCorrupt, n, maxTrailerEntries, float64(8*n)/(1<<20))
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
		} else {
			delta := int64(v)
			if delta < 0 {
				return nil, fmt.Errorf("%w: 第 %d 条偏移非单调（差分 %d）", ErrTrailerCorrupt, i, delta)
			}
			out = append(out, out[i-1]+delta)
		}
		// 首条：v ≥ 2^63 时 int64(v) 为负；其后：out[i-1]+delta 可能溢出回绕成负值。
		// 两者都绕过了上面的单调性检查，故在每条解码后统一拦截。
		if out[i] < 0 {
			return nil, fmt.Errorf("%w: 第 %d 条偏移为负（%d）", ErrTrailerCorrupt, i, out[i])
		}
	}
	if pos != len(b) {
		return nil, fmt.Errorf("%w: 解码后有 %d 字节多余", ErrTrailerCorrupt, len(b)-pos)
	}
	return out, nil
}
