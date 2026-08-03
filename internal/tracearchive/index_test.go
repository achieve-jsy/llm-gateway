package tracearchive

import (
	"errors"
	"math"
	"reflect"
	"runtime"
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

// 下面各条拒绝断言都用 errors.Is 而非 err != nil：损坏必须被报成 ErrTrailerCorrupt 本身，
// 否则把 index.go 里的 %w 改成 %v（或换成别的错误）时测试仍然全绿，
// 而读取端依赖这个身份来区分「产物损坏」与「数据不存在」。
func TestDecodeTrailerRejectsWrongCount(t *testing.T) {
	b := encodeTrailer([]int64{0, 10, 20})
	if _, err := decodeTrailer(b, 4); !errors.Is(err, ErrTrailerCorrupt) {
		t.Fatalf("条目数不符时应报 ErrTrailerCorrupt，实际 err=%v", err)
	}
}

func TestDecodeTrailerRejectsOversizedCount(t *testing.T) {
	// 这里的 n 模拟真实损坏：调用方（Task 7 的 Reader）用产物 footer 的 row_count
	// 作 n，该字段是 int32 且没有校验和，损坏时可给出 2^31-1。每条至少占 1 字节，
	// 故 n > len(b) 必为损坏，且必须在 make 之前拒绝——否则 make([]int64, 0, n)
	// 会按 8n 字节定容量，2^31-1 条即约 16 GiB，是进程级 OOM/panic 而非可返回的错误。
	//
	// 断言的是「分配量」而非「错误身份」：缺这条守卫时，varint 耗尽路径仍会报同一个
	// ErrTrailerCorrupt，只查 errors.Is 锁不住它。n 取 2^22（未守卫时约 32 MiB）
	// 足以观测到 8n 的分配，又不会在守卫被误删时打死测试机。
	b := encodeTrailer([]int64{0, 10})
	const n = 1 << 22
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err := decodeTrailer(b, n)
	runtime.ReadMemStats(&after)
	if !errors.Is(err, ErrTrailerCorrupt) {
		t.Fatalf("条目数远超字节数时应报 ErrTrailerCorrupt，实际 err=%v", err)
	}
	if alloc := after.TotalAlloc - before.TotalAlloc; alloc > 1<<20 {
		t.Fatalf("解码 %d 字节的 trailer 却为 %d 条定容量，分配了 %d 字节：条目数未在 make 之前校验",
			len(b), n, alloc)
	}
}

func TestDecodeTrailerRejectsNonMonotonic(t *testing.T) {
	// 两条子用例分别锁住**两道不同的守卫**，缺一不可（变异验证 m02/m03）：
	//   「差分为负且偏移变负」：out[1] = 0 + (-5) = -5 ⇒ 由 `out[i] < 0` 兜住；
	//     单独删掉 `delta < 0` 时它**仍然 PASS**（不是它不承重，而是这个输入凑巧
	//     让结果变负，被下一条守卫接住了）。
	//   「差分为负但偏移仍非负」：out[1] = 10 + (-5) = 5 ≥ 0 ⇒ **只有** `delta < 0`
	//     能拦（`out[i] < 0` 对它完全无感）。缺这道检查时解码会**成功返回一个
	//     非单调、因而错误的偏移序列**（静默错读，不是无害差异）——这条专杀 m02。
	cases := []struct {
		name string
		b    []byte
		n    int
	}{
		{
			// 手工构造一个偏移递减的 trailer：差分编码下即为负增量（补码表示）
			name: "差分为负且偏移变负",
			b:    appendVarint(appendVarint(nil, 0), ^uint64(5-1)),
			n:    2,
		},
		{
			// 差分 -5 相加后仍非负：非单调性与「偏移为负」在此**分离**
			name: "差分为负但偏移仍非负",
			b:    encodeTrailer([]int64{10, 5}),
			n:    2,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := decodeTrailer(c.b, c.n); !errors.Is(err, ErrTrailerCorrupt) {
				t.Fatalf("偏移非单调时应报 ErrTrailerCorrupt，实际 err=%v", err)
			}
		})
	}
}

func TestDecodeTrailerRejectsTruncated(t *testing.T) {
	b := encodeTrailer([]int64{0, 100, 200, 300})
	if _, err := decodeTrailer(b[:len(b)-1], 4); !errors.Is(err, ErrTrailerCorrupt) {
		t.Fatalf("截断的 trailer 应报 ErrTrailerCorrupt，实际 err=%v", err)
	}
}

func TestDecodeTrailerRejectsTrailingGarbage(t *testing.T) {
	// 走 index.go 的 pos != len(b) 守卫：两个合法 varint 之后多一个字节，
	// 声明 n=2，解码器必须在解完 2 条后因剩余字节而报错。
	// 注意这条与 RejectsTruncated 不是同一条路径——后者走 varint 耗尽。
	b := append(encodeTrailer([]int64{0, 10}), 0xFF)
	if _, err := decodeTrailer(b, 2); !errors.Is(err, ErrTrailerCorrupt) {
		t.Fatalf("trailer 尾部有多余字节时应报 ErrTrailerCorrupt，实际 err=%v", err)
	}
}

func TestDecodeTrailerRejectsNegativeOrWrappedOffset(t *testing.T) {
	cases := []struct {
		name string
		b    []byte
		n    int
	}{
		// 首条是绝对偏移。v ≥ 2^63 时 int64(v) 为负，这不是合法偏移而是损坏。
		{"首条偏移溢出为负", appendVarint(nil, 1<<63), 1},
		// MaxInt64 + 1 回绕成负值：两处输入（绝对偏移、差分）分别都合法，
		// 只有累加后校验才能拦住，单调性检查看不到它。
		{"累加回绕为负", appendVarint(appendVarint(nil, math.MaxInt64), 1), 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := decodeTrailer(c.b, c.n); !errors.Is(err, ErrTrailerCorrupt) {
				t.Fatalf("偏移为负或溢出时应报 ErrTrailerCorrupt，实际 err=%v", err)
			}
		})
	}
}
