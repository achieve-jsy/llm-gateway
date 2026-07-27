package tracearchive

import (
	"bytes"
	"runtime"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// TestGoEncoderPeakRSS 是 U8 的常驻回归测试。
// 它断言：在本包选定的窗口参数下，Go zstd 编码器的峰值堆占用不超过预算。
//
// 该测试的意义：规格 §8.4 的 C5 约束（点读并发上限）与网关 2 GiB 内存限额
// 都直接依赖这个数字。若换 zstd 版本或改窗口，此测试会立刻转红。
func TestGoEncoderPeakRSS(t *testing.T) {
	if testing.Short() {
		t.Skip("标定测试，-short 下跳过")
	}
	const budget = 1200 << 20 // 1.2 GB，规格 §5.3.3 的结论边界

	// 构造可压缩样本，大小略大于窗口以确保窗口被填满
	sample := bytes.Repeat([]byte(`{"request_id":"rid-0000","body":"`), 1)
	sample = append(sample, bytes.Repeat([]byte("abcdefgh"), (defaultZstdWindowBytes+64<<20)/8)...)
	sample = append(sample, []byte(`"}`)...)

	enc, err := zstd.NewWriter(nil,
		zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(defaultZstdLevel)),
		zstd.WithWindowSize(defaultZstdWindowBytes),
		zstd.WithEncoderConcurrency(1),
	)
	if err != nil {
		t.Fatalf("建编码器失败: %v", err)
	}
	defer enc.Close()

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_ = enc.EncodeAll(sample, nil)
	runtime.ReadMemStats(&after)

	// HeapInuse 差值近似编码器常驻占用
	used := int64(after.HeapInuse) - int64(before.HeapInuse)
	if used > budget {
		t.Fatalf("编码器堆占用 %d MB 超出预算 %d MB（U8 结论失效，需下调 defaultZstdWindowBytes）",
			used>>20, budget>>20)
	}
	t.Logf("编码器堆占用 %d MB（预算 %d MB）", used>>20, budget>>20)
}

// TestGoEncoderRatioRegression 是 U7 的常驻回归测试。
// 它断言：对本包选定的参数，Go 库产物不超过给定上界，防止换版本后压缩比悄悄劣化。
func TestGoEncoderRatioRegression(t *testing.T) {
	// 用高重复度文本，保证压缩比稳定可断言
	line := []byte(`{"request_id":"` + strings.Repeat("a", 32) + `","ts":"2026-09-24T00:00:00+08:00","body":"` + strings.Repeat("x", 4096) + `"}` + "\n")
	var raw bytes.Buffer
	for i := 0; i < 4096; i++ {
		raw.Write(line)
	}

	enc, err := zstd.NewWriter(nil,
		zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(defaultZstdLevel)),
		zstd.WithWindowSize(defaultZstdWindowBytes),
		zstd.WithEncoderConcurrency(1),
	)
	if err != nil {
		t.Fatalf("建编码器失败: %v", err)
	}
	defer enc.Close()

	out := enc.EncodeAll(raw.Bytes(), nil)
	ratio := float64(raw.Len()) / float64(len(out))

	// 该样本重复度极高，Go 库实测应远超 50x；上界取保守值
	const minRatio = 50.0
	if ratio < minRatio {
		t.Fatalf("压缩比 %.1fx 低于下界 %.1fx（Go 库或参数发生变化，见规格 U7）", ratio, minRatio)
	}
	t.Logf("压缩比 %.1fx（下界 %.1fx），%d 字节 → %d 字节", ratio, minRatio, raw.Len(), len(out))
}
