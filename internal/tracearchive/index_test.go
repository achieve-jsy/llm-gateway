package tracearchive

import (
	"reflect"
	"testing"
)

func TestTrailerRoundTrip(t *testing.T) {
	cases := []struct {
		name    string
		offsets []int64
	}{
		{"单行", []int64{0}},
		{"两行", []int64{0, 100}},
		{"等长多行", []int64{0, 10, 20, 30, 40}},
		{"长度悬殊", []int64{0, 5, 5000, 5001, 900000}},
		{"零长度行", []int64{0, 0, 0}},
		{"大偏移", []int64{0, 1 << 40, 1<<40 + 7}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := encodeTrailer(c.offsets)
			got, err := decodeTrailer(b, len(c.offsets))
			if err != nil {
				t.Fatalf("解码失败: %v", err)
			}
			if !reflect.DeepEqual(got, c.offsets) {
				t.Fatalf("往返不一致:\n want %v\n got  %v", c.offsets, got)
			}
		})
	}
}

func TestDecodeTrailerRejectsWrongCount(t *testing.T) {
	b := encodeTrailer([]int64{0, 10, 20})
	if _, err := decodeTrailer(b, 4); err == nil {
		t.Fatal("条目数不符时应报错，实际未报错")
	}
}

func TestDecodeTrailerRejectsNonMonotonic(t *testing.T) {
	// 手工构造一个偏移递减的 trailer：差分编码下即为负增量
	b := []byte{}
	b = appendVarint(b, 0)
	b = appendVarint(b, ^uint64(5-1)) // 负增量（补码表示）
	if _, err := decodeTrailer(b, 2); err == nil {
		t.Fatal("偏移非单调时应报错，实际未报错")
	}
}

func TestDecodeTrailerRejectsTruncated(t *testing.T) {
	b := encodeTrailer([]int64{0, 100, 200, 300})
	if _, err := decodeTrailer(b[:len(b)-1], 4); err == nil {
		t.Fatal("截断的 trailer 应报错，实际未报错")
	}
}
