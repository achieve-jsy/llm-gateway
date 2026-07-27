// trace-calib 用生产真实样本标定 Go zstd 库与 C 版 CLI 的差异。
//
// 用法:
//
//	trace-calib -in sample.jsonl                        # 全网格：级别 9/19/22 × 窗口 128/256/512 MiB
//	trace-calib -in sample.jsonl -level 19 -window 512  # 单点（便于逐点取峰值 RSS）
//
// 输出: 每个组合的产物大小、压缩比、耗时与吞吐，用于钉死规格 §15 的 U7/U8。
//
// 峰值 RSS 从 /proc/self/status 的 VmHWM 读（Linux），这是内核记录的高水位，
// 而 runtime.MemStats 统计的是堆分配总量，不是常驻峰值，不能用于 U8。
//
// VmHWM 是单调不减的，所以它只在“一个进程只跑一个组合”时才有归属意义：
// 标定 U8 时必须把 -level 与 -window 都显式指定，一次一进程。
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

// countWriter 只累加字节数、不保留产物本体。
// 若把产物留在内存里，它会计入 RSS，污染 U8 的峰值测量。
type countWriter struct{ n int64 }

func (w *countWriter) Write(p []byte) (int, error) {
	w.n += int64(len(p))
	return len(p), nil
}

func main() {
	in := flag.String("in", "", "输入样本（未压缩 JSONL）")
	windowMB := flag.Int("window", 0, "窗口 MiB；0 表示跑 128/256/512 全档")
	level := flag.Int("level", 0, "zstd 级别；0 表示跑 9/19/22 全档")
	encodeAll := flag.Bool("encodeall", false,
		"改用 EncodeAll（整份样本读进内存，仅供小样本与流式口径对照，不可用于峰值 RSS 标定）")
	flag.Parse()
	if *in == "" {
		fmt.Fprintln(os.Stderr, "必须指定 -in")
		os.Exit(2)
	}

	fi, err := os.Stat(*in)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读样本失败: %v\n", err)
		os.Exit(1)
	}
	inBytes := fi.Size()

	mode := "流式（与生产封块路径一致）"
	if *encodeAll {
		mode = "EncodeAll（一次性，内存不具代表性）"
	}
	fmt.Printf("样本: %s  未压缩 %.1f MB\n", *in, float64(inBytes)/1048576)
	fmt.Printf("模式: %s\n", mode)
	fmt.Printf("峰值 RSS: VmHWM(%s)，另建议用 /usr/bin/time -v 或轮询 /proc/<pid>/status 交叉验证\n", vhmName())
	fmt.Println()

	levels := []int{9, 19, 22}
	if *level != 0 {
		levels = []int{*level}
	}
	windows := []int{128, 256, 512}
	if *windowMB != 0 {
		windows = []int{*windowMB}
	}

	for _, lvl := range levels {
		for _, wMB := range windows {
			w := wMB << 20
			opts := []zstd.EOption{
				zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(lvl)),
				zstd.WithWindowSize(w),
				zstd.WithEncoderConcurrency(1),
			}
			outBytes, elapsed, err := run(*in, opts, *encodeAll)
			if err != nil {
				fmt.Fprintf(os.Stderr, "级别 %d 窗口 %dMB 失败: %v\n", lvl, wMB, err)
				os.Exit(1)
			}
			secs := elapsed.Seconds()
			hwmKB, _ := vhmKB()
			fmt.Printf("级别 %-3d 窗口 %4dMB  产物 %9.1f MB  比值 %6.2fx  耗时 %7.1fs  吞吐 %6.1f MB/s  峰值RSS %6.1f MB\n",
				lvl, wMB, float64(outBytes)/1048576,
				float64(inBytes)/float64(outBytes), secs,
				float64(inBytes)/1048576/secs,
				float64(hwmKB)/1024)
			fmt.Printf("RESULT level=%d window_mb=%d in_bytes=%d out_bytes=%d seconds=%.2f peak_rss_mb=%.1f\n",
				lvl, wMB, inBytes, outBytes, secs, float64(hwmKB)/1024)
		}
	}
}

// vhmKB 读 /proc/self/status 的 VmHWM（峰值常驻集，单位 kB）。
// 非 Linux 环境下该文件不存在，返回 0，调用方按“无数据”处理。
func vhmKB() (int, error) {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "VmHWM:") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 2 {
			break
		}
		n, err := strconv.Atoi(f[1])
		if err != nil {
			return 0, err
		}
		return n, nil
	}
	return 0, errors.New("VmHWM 未找到")
}

func vhmName() string {
	if _, err := vhmKB(); err != nil {
		return "不可用：本平台无 /proc/self/status"
	}
	return "可用"
}

// run 对同一份输入跑一次编码，返回产物字节数与耗时。
// 耗时含编码器收尾（Close 才会写出帧尾），否则会低估。
func run(path string, opts []zstd.EOption, encodeAll bool) (int64, time.Duration, error) {
	if encodeAll {
		src, err := os.ReadFile(path)
		if err != nil {
			return 0, 0, err
		}
		enc, err := zstd.NewWriter(nil, opts...)
		if err != nil {
			return 0, 0, err
		}
		defer enc.Close()
		t0 := time.Now()
		out := enc.EncodeAll(src, make([]byte, 0, len(src)/8))
		return int64(len(out)), time.Since(t0), nil
	}

	cw := &countWriter{}
	enc, err := zstd.NewWriter(cw, opts...)
	if err != nil {
		return 0, 0, err
	}
	f, err := os.Open(path)
	if err != nil {
		enc.Close()
		return 0, 0, err
	}
	defer f.Close()

	t0 := time.Now()
	if _, err := io.Copy(enc, f); err != nil {
		enc.Close()
		return 0, 0, err
	}
	if err := enc.Close(); err != nil {
		return 0, 0, err
	}
	return cw.n, time.Since(t0), nil
}
