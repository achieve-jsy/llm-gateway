package traceverify

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

// defaultProcRoot 是 procfs 的默认根（Linux）。darwin 上没有它 ⇒ 默认根下的读取
// 必然失败，而失败必须被当成失败向上传播（不许静默返回 0）。
const defaultProcRoot = "/proc"

// bytesPerKB 是 `/proc/<pid>/status` 里 VmHWM 的单位换算（kB → bytes）。
const bytesPerKB int64 = 1024

// ErrVmHWMMissing 表示 status 文本里没有 `VmHWM:` 行。
var ErrVmHWMMissing = errors.New("traceverify: status 文本里没有 VmHWM 行")

// ProcStatusPath 返回 pid 在 root（procfs 根；空 ⇒ `/proc`）下的 status 路径。
func ProcStatusPath(root string, pid int) string {
	if root == "" {
		root = defaultProcRoot
	}
	return filepath.Join(root, strconv.Itoa(pid), "status")
}

// ParseVmHWM 从 `/proc/<pid>/status` 的文本里取 `VmHWM`（峰值常驻内存）并换算成 bytes。
//
// 行形态（真实文件逐字同形）：`VmHWM:\t  100000 kB`。
// 取**第一条** `VmHWM:` 行；缺行 / 字段数不对 / 值不是纯十进制数字（含带符号）/ 单位不是
// `kB` / 乘 1024 溢出 int64 ⇒ 报错。**绝不返回 0 冒充读数**：0 会被下游当成一个合法
// 的峰值，把「没有证据」洗成「内存很小」。
func ParseVmHWM(statusText string) (int64, error) {
	for _, line := range strings.Split(statusText, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "VmHWM:" {
			continue
		}
		if len(fields) != 3 {
			return 0, fmt.Errorf("traceverify: VmHWM 行字段数=%d（期望 3：`VmHWM: <值> kB`）: %q",
				len(fields), line)
		}
		// 只接受纯十进制数字（逐位校验，拒掉 `strconv` 会吞下的前导 `+`）——
		// 与冻结包 `parsePartSeq` 同一口径：procfs 不会产出带符号的读数，把 `+100`
		// 当成 100 等于让「读不懂的输入」被静默接受。
		val := fields[1]
		for i := 0; i < len(val); i++ {
			if val[i] < '0' || val[i] > '9' {
				return 0, fmt.Errorf("traceverify: VmHWM 值必须是纯十进制数字（不接受符号）: %q", val)
			}
		}
		kb, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("traceverify: VmHWM 值超出 int64: %q", val)
		}
		if fields[2] != "kB" {
			return 0, fmt.Errorf("traceverify: VmHWM 单位不是 kB: %q", fields[2])
		}
		const maxInt64 = int64(^uint64(0) >> 1)
		if kb > maxInt64/bytesPerKB {
			return 0, fmt.Errorf("traceverify: VmHWM 值 %d kB 换算成 bytes 会溢出 int64", kb)
		}
		return kb * bytesPerKB, nil
	}
	return 0, fmt.Errorf("%w: 没有任何以 `VmHWM:` 起头的行", ErrVmHWMMissing)
}

// VmHWMProbe 描述一次峰值常驻内存采样。
//
// Root/Samples/Interval 与 `PeakVmHWM` 的三个参数一一对应；Read 可注入：
// 用例用它造出「跨采样递增」的读数与「读不到」的形态，从而在 darwin 上也能真跑
// 采样循环（不需要 /proc）。Read 为 nil 时按 Root 读 `<root>/<pid>/status`。
type VmHWMProbe struct {
	Root     string
	Samples  int
	Interval time.Duration
	Read     func(pid int) (string, error)
}

// Peak 对每个 pid 采样 Samples 次（间隔 Interval）并返回**全部采样**的峰值（bytes）。
//
// 参数非法（pids 为空 / Samples ≤ 0 / Interval < 0）⇒ 在**任何读取之前**报错。
// 任一次读取失败或解析失败 ⇒ 报错（不返回部分结果：半个峰值不是证据）。
// 最后一次采样后不再等待间隔。
func (p VmHWMProbe) Peak(pids []int) (int64, error) {
	if len(pids) == 0 {
		return 0, errors.New("traceverify: 峰值采样需要至少一个 pid")
	}
	if p.Samples <= 0 {
		return 0, fmt.Errorf("traceverify: 采样次数必须 > 0（得到 %d）", p.Samples)
	}
	if p.Interval < 0 {
		return 0, fmt.Errorf("traceverify: 采样间隔必须 ≥ 0（得到 %s）", p.Interval)
	}
	read := p.Read
	if read == nil {
		root := p.Root
		if root == "" {
			root = defaultProcRoot
		}
		read = func(pid int) (string, error) {
			path := ProcStatusPath(root, pid)
			b, err := os.ReadFile(path)
			if err != nil {
				return "", fmt.Errorf("读 %s: %w", path, err)
			}
			return string(b), nil
		}
	}
	var peak int64
	for i := 0; i < p.Samples; i++ {
		if i > 0 && p.Interval > 0 {
			time.Sleep(p.Interval)
		}
		for _, pid := range pids {
			text, err := read(pid)
			if err != nil {
				return 0, fmt.Errorf("traceverify: pid %d 的 status 读不到: %w", pid, err)
			}
			kb, err := ParseVmHWM(text)
			if err != nil {
				return 0, fmt.Errorf("traceverify: pid %d: %w", pid, err)
			}
			if kb > peak {
				peak = kb
			}
		}
	}
	return peak, nil
}

// PeakVmHWM 轮询若干进程（默认 procfs 根 `/proc`）的 VmHWM 并返回峰值（bytes）。
// 采样间隔由调用方决定（`cmd/trace-verify decode-bench` 用 50ms）。
func PeakVmHWM(pids []int, samples int, interval time.Duration) (int64, error) {
	return VmHWMProbe{Samples: samples, Interval: interval}.Peak(pids)
}

// ─────────────────────────────────────────────────────────────────────────────
// decode-bench 的承重逻辑（A6/C5 的标定入口）
//
// 为什么它与 VmHWM 采样同文件：这两块代码是同一个动作的两半 —— 峰值常驻内存的
// **采样对象**就是解码工作负载；把它们放在一起，「测的是同一个解码路径」这件事
// 在文件层面就看得见（被测产物用读侧同一个解码库与同一个窗口）。
// ─────────────────────────────────────────────────────────────────────────────

// DefaultDecodeWindowBytes 是解码窗口的默认值（512 MiB）。
//
// 它必须与写入侧一致：生产封块用 C 版 `zstd -19 --long=29`，读侧
// （`internal/tracearchive`）用 Go 库解码且 `WithDecoderMaxWindow` 取同一个 512 MiB
// —— 解码器的内存占用 ≈ 窗口大小，这正是规格 §8.4 C5 要标定的量。
const DefaultDecodeWindowBytes = 512 << 20

// ParseConcurrencyLevels 解析 `--concurrency` 的取值（形如 `1,2,3`）：只接受正整数，
// 去重后按升序返回（档位本身就是「从小到大各测一次」，顺序无关却必须确定）。
// 空串 / 非数字 / 非正数 / 重复 / 超过 maxDecodeConcurrency 一律报错。
const maxDecodeConcurrency = 64

func ParseConcurrencyLevels(s string) ([]int, error) {
	if strings.TrimSpace(s) == "" {
		return nil, errors.New("traceverify: --concurrency 不能为空（形如 `1,2,3`）")
	}
	seen := map[int]bool{}
	var out []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("traceverify: --concurrency %q 里有空档位", s)
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("traceverify: --concurrency %q 里的 %q 不是整数", s, part)
		}
		if n <= 0 || n > maxDecodeConcurrency {
			return nil, fmt.Errorf("traceverify: 并发档位 %d 越界（必须 1..%d）", n, maxDecodeConcurrency)
		}
		if seen[n] {
			return nil, fmt.Errorf("traceverify: 并发档位 %d 重复出现（%q）", n, s)
		}
		seen[n] = true
		out = append(out, n)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out, nil
}

// DecodeArtifact 用读侧同一个解码库（klauspost/compress/zstd）**流式**解码一个产物，
// 返回解压后的字节数（不落任何临时文件、不缓存解压结果 —— 与 C1/C2/C3 同形）。
//
// windowBytes ≤ 0 ⇒ DefaultDecodeWindowBytes。产物不存在 / 解码失败 ⇒ 报错。
func DecodeArtifact(path string, windowBytes int) (int64, error) {
	if path == "" {
		return 0, errors.New("traceverify: DecodeArtifact 需要产物路径")
	}
	if windowBytes <= 0 {
		windowBytes = DefaultDecodeWindowBytes
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("traceverify: 打开产物 %s: %w", path, err)
	}
	defer f.Close()

	dec, err := zstd.NewReader(f, zstd.WithDecoderMaxWindow(uint64(windowBytes)))
	if err != nil {
		return 0, fmt.Errorf("traceverify: 构造解码器（窗口 %d）：%w", windowBytes, err)
	}
	defer dec.Close()

	n, err := io.Copy(io.Discard, dec)
	if err != nil {
		return 0, fmt.Errorf("traceverify: 解码 %s: %w", path, err)
	}
	return n, nil
}
