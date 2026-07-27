package tracearchive

import "testing"

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

func TestDecodeFooterRejectsNegativeRowCount(t *testing.T) {
	b := encodeFooter(footer{TrailerOffset: 8, TrailerLen: 4, RowCount: -1})
	if _, err := decodeFooter(b); err == nil {
		t.Fatal("负 row_count 应报错")
	}
}

func TestDecodeFooterRejectsWrongLength(t *testing.T) {
	if _, err := decodeFooter(make([]byte, FooterSize-1)); err == nil {
		t.Fatal("长度不足时应报错")
	}
	if _, err := decodeFooter(make([]byte, FooterSize+1)); err == nil {
		t.Fatal("长度超出时应报错")
	}
}

func TestDecodeFooterRejectsBadMagic(t *testing.T) {
	b := encodeFooter(footer{TrailerOffset: 1, TrailerLen: 2})
	b[0] ^= 0xFF // 篡改 magic
	if _, err := decodeFooter(b); err == nil {
		t.Fatal("magic 被篡改时应报错")
	}
}

func TestDecodeFooterRejectsUnknownVersion(t *testing.T) {
	b := encodeFooter(footer{TrailerOffset: 1, TrailerLen: 2})
	b[4] = 99 // 篡改 format_version
	if _, err := decodeFooter(b); err == nil {
		t.Fatal("未知 format_version 应报错")
	}
}
