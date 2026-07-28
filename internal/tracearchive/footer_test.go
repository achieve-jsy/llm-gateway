package tracearchive

import (
	"bytes"
	"errors"
	"testing"
)

func TestFooterRoundTrip(t *testing.T) {
	in := footer{
		TrailerOffset: 1234567,
		TrailerLen:    4096,
		RowCount:      2718,
		FrameSHA:      [8]byte{1, 2, 3, 4, 5, 6, 7, 8},
	}
	b := encodeFooter(in)
	if len(b) != FooterSize {
		t.Fatalf("footer 长度应为 %d，实际 %d", FooterSize, len(b))
	}
	got, err := decodeFooter(b)
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if got != in {
		t.Fatalf("往返不一致:\n want %+v\n got  %+v", in, got)
	}
}

// TestFooterGoldenBytes 把字节布局契约固化为手写期望值。
//
// 为什么需要它：往返测试只证明编解码「自对称」——若编码与解码同时错在同一个
// 偏移上（例如 row_count 与 frame_sha 都写错位置但读写一致），往返依然全绿。
// 布局契约是 Task 7 读端与冷层格式演进的依据，必须有一份不依赖被测实现的
// 期望字节来锚定，否则后续重构没有任何回归保护。
func TestFooterGoldenBytes(t *testing.T) {
	in := footer{
		TrailerOffset: 0x0102030405060708,
		TrailerLen:    0x0A0B0C0D,
		RowCount:      0x0E0F1011,
		FrameSHA:      [8]byte{0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19},
	}
	want := []byte{
		'T', 'B', 'A', '4', // [0:4) magic
		0x00, 0x01, // [4:6) format_version = 1
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, // [6:14) trailer_offset
		0x0A, 0x0B, 0x0C, 0x0D, // [14:18) trailer_len
		0x0E, 0x0F, 0x10, 0x11, // [18:22) row_count
		0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, // [22:30) frame_sha256[:8]
		0x00, 0x00, // [30:32) reserved，恒 0
	}
	if len(want) != FooterSize {
		t.Fatalf("期望值本身应为 %d 字节，实际 %d", FooterSize, len(want))
	}

	got := encodeFooter(in)
	if !bytes.Equal(got, want) {
		t.Fatalf("字节布局不符:\n want % x\n got  % x", want, got)
	}

	// 解码侧同样对这份外部期望值负责：布局改动必须两侧同时被这条测试拦住。
	back, err := decodeFooter(want)
	if err != nil {
		t.Fatalf("解码期望字节失败: %v", err)
	}
	if back != in {
		t.Fatalf("期望字节解码不一致:\n want %+v\n got  %+v", in, back)
	}
}

func TestFooterRowCountRoundTrip(t *testing.T) {
	// row_count 是读取端判断行号越界与 trailer 截断的唯一依据，必须无损往返
	for _, n := range []int32{0, 1, 255, 256, 65535, 1 << 20} {
		b := encodeFooter(footer{TrailerOffset: 8, TrailerLen: 4, RowCount: n})
		got, err := decodeFooter(b)
		if err != nil {
			t.Fatalf("row_count=%d 解码失败: %v", n, err)
		}
		if got.RowCount != n {
			t.Fatalf("row_count 往返不一致: want %d, got %d", n, got.RowCount)
		}
	}
}

// TestDecodeFooterRejectsNegativeRowCount 断言的不只是「有错」，而是错误身份：
// Task 7 的 Reader 靠 errors.Is(err, ErrFooterCorrupt) 把「产物损坏」与
// 「数据不存在」分开（规格 §8.1），若这里退化成 errors.New 之类的新错误值，
// 损坏产物就会被误报成业务事实。
func TestDecodeFooterRejectsNegativeRowCount(t *testing.T) {
	b := encodeFooter(footer{TrailerOffset: 8, TrailerLen: 4, RowCount: -1})
	if _, err := decodeFooter(b); !errors.Is(err, ErrFooterCorrupt) {
		t.Fatalf("负 row_count 应返回 ErrFooterCorrupt，实际 err=%v", err)
	}
}

// TestDecodeFooterRejectsNegativeOffsetOrLen 覆盖被截断/损坏产物上真实可达的输入：
// 检测到非法的 uint64/uint32 值后，这两处守卫各自必须独立承重，不能被其它校验顺带覆盖。
func TestDecodeFooterRejectsNegativeOffsetOrLen(t *testing.T) {
	cases := []struct {
		name string
		in   footer
	}{
		{"负 trailer_offset", footer{TrailerOffset: -1, TrailerLen: 4, RowCount: 1}},
		{"负 trailer_len", footer{TrailerOffset: 8, TrailerLen: -1, RowCount: 1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := decodeFooter(encodeFooter(c.in)); !errors.Is(err, ErrFooterCorrupt) {
				t.Fatalf("应返回 ErrFooterCorrupt，实际 err=%v", err)
			}
		})
	}
}

// TestDecodeFooterRejectsWrongLength 的长度扰动都以合法编码结果为基底，
// 使 magic 与 version 仍然合法——否则全零缓冲会先在 magic 处失败，
// 即使把长度检查放宽成「只拒绝过短」也依然全绿，长度检查本身就没被验证。
func TestDecodeFooterRejectsWrongLength(t *testing.T) {
	valid := encodeFooter(footer{TrailerOffset: 8, TrailerLen: 4, RowCount: 1})
	cases := []struct {
		name string
		b    []byte
	}{
		{"少一字节", valid[:FooterSize-1]},
		{"多一字节", append(append([]byte{}, valid...), 0x00)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := decodeFooter(c.b); !errors.Is(err, ErrFooterCorrupt) {
				t.Fatalf("长度非 %d 应返回 ErrFooterCorrupt，实际 err=%v", FooterSize, err)
			}
		})
	}
}

func TestDecodeFooterRejectsBadMagic(t *testing.T) {
	b := encodeFooter(footer{TrailerOffset: 1, TrailerLen: 2})
	b[0] ^= 0xFF // 篡改 magic
	if _, err := decodeFooter(b); !errors.Is(err, ErrFooterCorrupt) {
		t.Fatalf("magic 被篡改时应返回 ErrFooterCorrupt，实际 err=%v", err)
	}
}

func TestDecodeFooterRejectsUnknownVersion(t *testing.T) {
	b := encodeFooter(footer{TrailerOffset: 1, TrailerLen: 2})
	b[4] = 99 // 篡改 format_version
	if _, err := decodeFooter(b); !errors.Is(err, ErrFooterCorrupt) {
		t.Fatalf("未知 format_version 应返回 ErrFooterCorrupt，实际 err=%v", err)
	}
}
