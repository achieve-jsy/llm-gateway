package traceverify

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/example/llm-gateway/internal/tracearchive"
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

// ─────────────────────────────────────────────────────────────────────────────
// decode-bench 的点读负载（计划任务 9 步骤 3-4）
//
// 【为什么是点读而不是整流解码】规格 §14.2 的 A6/C5 要标定的是「**K 路并发点读**的
// 峰值常驻内存」——读侧（计划 3）的实际动作是 `tracearchive.NewReader` + `GetBody`
// 按定位键取一行，它必须把整个解码窗口载入。整流解码（把整块解压一遍再丢弃）测的是
// 另一个动作：它共享解码器，却少了 trailer/索引那条路径（那正是 `maxTrailerEntries`
// 上界要压的东西）⇒ 只能当参考读数，不能当 C5 的判据。故本节与真实读侧同形。
//
// 【可测性】真实点读需要一个真实产物（要 zstd 子进程）与 `/proc`（要 Linux）；
// 负载因此把「取一行」与「采一次峰值」都抽象成可注入的函数 ⇒ 并发度、随机性、
// 错误传播、**到限中止**这些行为断言可以用替身在 darwin 上真跑。
// ─────────────────────────────────────────────────────────────────────────────

// DefaultPointsPerGoroutine 是每个 goroutine 每轮的点读数（计划任务 9 步骤 3 的 50 次）。
const DefaultPointsPerGoroutine = 50

// DefaultSampleInterval 是峰值采样的默认间隔。
const DefaultSampleInterval = 50 * time.Millisecond

// PointReadFunc 按定位键取回一行正文。真实实现 = `tracearchive.Reader.GetBody`；
// 用例注入替身即可在没有任何产物 / 没有 procfs 的机器上驱动整个负载。
type PointReadFunc func(ctx context.Context, ref tracearchive.Ref) ([]byte, error)

// PeakFunc 采样一次峰值常驻内存（VmHWM，bytes）。decode-bench 注入真实 procfs 读取
// （本进程 + 自身子进程）；用例注入替身即可驱动「到限中止」与「读不到峰值」两条路径。
//
// **读不到必须报错，绝不返回 0**：0 会被当成「内存很小」的合法读数。
type PeakFunc func() (int64, error)

// PointReadResult 是一次点读负载的读数。
//
// MaxInFlight 是**实测**到的同时在飞点数（不是配置值）：它是「K 路并发」这句话的
// 唯一可核验证据 —— 配置了 K 但实际串行，峰值内存自然上不去，A6 的结论就会失真。
type PointReadResult struct {
	Points       int           // 实际完成的点读数
	Bytes        int64         // 取回的正文总字节数
	MaxInFlight  int           // 同时在飞点读数的峰值
	DistinctRows int           // 命中过的不同行号个数（随机性证据）
	Rows         int           // 采样上界（= l.Rows）
	Elapsed      time.Duration // 整个负载的墙钟耗时
	PeakBytes    int64         // 采样到的峰值常驻内存（Peak 为 nil ⇒ 0）
	PeakSamples  int           // 实际采样次数
	Aborted      bool          // 是否因峰值达到 AbortAtBytes 而中止（中止 ≠ 失败）
}

// PointReadLoad 描述一次「K 个 goroutine 各做 Rounds × PointsPerGoroutine 次随机点读」。
//
// Rounds 是**连续轮数**（≥5，计划任务 9 步骤 4.2 要复测「连续多轮不做 GC」的形态）：
// 轮内不重建进程、不重建 Reader，因此分配压力跨轮累积 —— 这正是要暴露的形态。
//
// 【到限中止（承重）】`GOGC=off` 的形态会让内存单调增长，直跑到 OOM 为止；
// 被 OOM 杀掉的进程**什么都报不出来**。因此 `AbortAtBytes > 0` 时，监控协程一旦
// 采样到峰值 ≥ 该阈值就取消负载并置 `Aborted`：于是「峰值至少到了阈值」是一条
// **已证明**的读数（而不是一个被杀的进程）。这正是计划任务 9 停止点要的判据形态。
type PointReadLoad struct {
	Ref                tracearchive.Ref // DT/Inst/Part；Row 由负载逐次随机生成
	Rows               int              // 块内行数（采样上界，来自 ArtifactRows 或 --rows）
	Concurrency        int              // K
	Rounds             int              // 连续轮数
	PointsPerGoroutine int              // 0 ⇒ DefaultPointsPerGoroutine
	Seed               int64            // 随机种子（可复现）；每个 goroutine 用它 + slot
	Read               PointReadFunc    // 必填

	Peak           PeakFunc      // nil ⇒ 不采样（峰值与中止都不生效）
	AbortAtBytes   int64         // >0 且实测峰值 ≥ 它 ⇒ 取消负载并置 Aborted
	SampleInterval time.Duration // 0 ⇒ DefaultSampleInterval
	MaxSamples     int           // 0 ⇒ 采到负载结束
}

// Run 执行负载并返回读数。参数非法 ⇒ 在**任何点读之前**报错。
// 任一次点读 / 峰值采样失败 ⇒ 取消其余 goroutine 并把该错误原样上抛（不返回半个读数）。
// 到限中止（Aborted）**不是**错误：它返回部分读数，由调用方（报告）据此判定。
func (l PointReadLoad) Run(ctx context.Context) (PointReadResult, error) {
	if l.Read == nil {
		return PointReadResult{}, errors.New("traceverify: 点读负载需要一个 Read（真实实现 = tracearchive.Reader.GetBody）")
	}
	if l.Rows <= 0 {
		return PointReadResult{}, fmt.Errorf("traceverify: 点读负载需要 Rows > 0（得到 %d）", l.Rows)
	}
	if l.Concurrency <= 0 {
		return PointReadResult{}, fmt.Errorf("traceverify: 点读负载需要 Concurrency > 0（得到 %d）", l.Concurrency)
	}
	if l.Rounds <= 0 {
		return PointReadResult{}, fmt.Errorf("traceverify: 点读负载需要 Rounds > 0（得到 %d）", l.Rounds)
	}
	points := l.PointsPerGoroutine
	if points <= 0 {
		points = DefaultPointsPerGoroutine
	}
	interval := l.SampleInterval
	if interval <= 0 {
		interval = DefaultSampleInterval
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg          sync.WaitGroup
		mu          sync.Mutex
		firstErr    error
		inFlight    int64
		maxInFlight int64
		pointsDone  int64
		bytesRead   int64
		rowsSeen    = map[int]struct{}{}
		peakBytes   int64
		peakSamples int
		aborted     bool
	)
	// abortRequested 让「到限中止」与「真错误」区分开：中止会把 ctx 取消掉，
	// 正在飞的点读随即返回 context.Canceled —— 那是**预期的收尾**，不是失败。
	var abortRequested int32
	recordErr := func(err error) {
		mu.Lock()
		if firstErr == nil {
			firstErr = err
			cancel()
		}
		mu.Unlock()
	}
	noteRow := func(row int) {
		mu.Lock()
		rowsSeen[row] = struct{}{}
		mu.Unlock()
	}
	notePeak := func(v int64) {
		mu.Lock()
		if v > peakBytes {
			peakBytes = v
		}
		peakSamples++
		mu.Unlock()
	}

	start := time.Now()
	done := make(chan struct{})
	monitorDone := make(chan struct{})
	if l.Peak != nil {
		go func() {
			defer close(monitorDone)
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			sample := func() bool { // 返回 false ⇒ 监控结束
				v, err := l.Peak()
				if err != nil {
					recordErr(fmt.Errorf("traceverify: 采样峰值常驻内存失败: %w", err))
					return false
				}
				notePeak(v)
				mu.Lock()
				n := peakSamples
				mu.Unlock()
				if l.AbortAtBytes > 0 && v >= l.AbortAtBytes {
					mu.Lock()
					aborted = true
					mu.Unlock()
					atomic.StoreInt32(&abortRequested, 1)
					cancel()
					return false
				}
				return l.MaxSamples <= 0 || n < l.MaxSamples
			}
			if !sample() {
				return
			}
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
					if !sample() {
						return
					}
				}
			}
		}()
	} else {
		close(monitorDone)
	}

	for slot := 0; slot < l.Concurrency; slot++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(l.Seed + int64(slot)))
			for i := 0; i < l.Rounds*points; i++ {
				if ctx.Err() != nil {
					return
				}
				ref := l.Ref
				ref.Row = int(rng.Int63n(int64(l.Rows)))
				cur := atomic.AddInt64(&inFlight, 1)
				for {
					old := atomic.LoadInt64(&maxInFlight)
					if cur <= old || atomic.CompareAndSwapInt64(&maxInFlight, old, cur) {
						break
					}
				}
				body, err := l.Read(ctx, ref)
				atomic.AddInt64(&inFlight, -1)
				if err != nil {
					if atomic.LoadInt32(&abortRequested) == 1 && errors.Is(err, context.Canceled) {
						return // 到限中止的预期收尾，不是失败
					}
					recordErr(fmt.Errorf("traceverify: 点读 %s: %w", ref, err))
					return
				}
				atomic.AddInt64(&pointsDone, 1)
				atomic.AddInt64(&bytesRead, int64(len(body)))
				noteRow(ref.Row)
			}
		}(slot)
	}
	wg.Wait()
	elapsed := time.Since(start)
	close(done)
	<-monitorDone

	mu.Lock()
	err := firstErr
	res := PointReadResult{
		Points:       int(pointsDone),
		Bytes:        bytesRead,
		MaxInFlight:  int(maxInFlight),
		DistinctRows: len(rowsSeen),
		Rows:         l.Rows,
		Elapsed:      elapsed,
		PeakBytes:    peakBytes,
		PeakSamples:  peakSamples,
		Aborted:      aborted,
	}
	mu.Unlock()
	if err != nil {
		return PointReadResult{}, err
	}
	return res, nil
}

// ArtifactRows 用**公开 API 的错误身份**探测一个产物块内的行数（decode-bench 的
// `--rows 0` 默认路径）。它只读、不写、不落临时文件。
//
// 判据来自冻结包 `reader.go` 的显式越界拒绝：`ref.Row < 0 || ref.Row >= RowCount`
// ⇒ `ErrNotArchived`（在**解码之前**返回）；块内行 ⇒ 正文或 `ErrBodyMissing`（标记行）。
// 因此「第一个越界的行号」就等于行数 —— 不需要复刻 footer 布局（那是任务 10 的
// `tracesealer.RowCount` 的活），也就不存在第三份 footer 镜像。
//
// 代价（如实登记）：倍增 + 二分共约 2×log2(rows) 次点读；真实块上每次都要解码到目标行
// ⇒ 这一步本身会推高 VmHWM（保守方向）。`--rows` 可以直接给定行数跳过它。
func ArtifactRows(ctx context.Context, read PointReadFunc, ref tracearchive.Ref, maxRows int) (int, error) {
	if read == nil {
		return 0, errors.New("traceverify: 行数探测需要一个 Read")
	}
	if maxRows <= 0 {
		maxRows = 1 << 31
	}
	inRange := func(row int) (bool, error) {
		r := ref
		r.Row = row
		_, err := read(ctx, r)
		switch {
		case err == nil:
			return true, nil
		case errors.Is(err, tracearchive.ErrBodyMissing):
			return true, nil
		case errors.Is(err, tracearchive.ErrNotArchived):
			return false, nil
		default:
			return false, err
		}
	}

	ok, err := inRange(0)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, fmt.Errorf("traceverify: %s 的 row=0 都不在范围内 ⇒ 无法探测行数", ref)
	}
	lo, hi := 0, 1
	for {
		ok, err := inRange(hi)
		if err != nil {
			return 0, err
		}
		if !ok {
			break
		}
		lo = hi
		if hi >= maxRows {
			return 0, fmt.Errorf("traceverify: %s 的行数超过 maxRows=%d（探测上界）", ref, maxRows)
		}
		hi *= 2
		if hi > maxRows {
			hi = maxRows
		}
	}
	for lo+1 < hi {
		mid := lo + (hi-lo)/2
		ok, err := inRange(mid)
		if err != nil {
			return 0, err
		}
		if ok {
			lo = mid
		} else {
			hi = mid
		}
	}
	return lo + 1, nil
}
