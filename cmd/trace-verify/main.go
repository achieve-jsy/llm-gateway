// Command trace-verify 是**验收工具**（计划 4 / 任务 8）：A3（真实 ENOSPC）的隔离
// 回环文件系统准备/清理，以及 A6（C5 解码内存预算）的标定入口。
//
// 【它不参与生产上线】`make build`（build-gateway / build-cp / build-sealer）**不含**
// 本命令；它只由 `make build-trace-verify` 编译，只在测试环境 / A3 宿主上人工执行。
// 三个子命令：
//
//	scratch-prepare  容量预检（R2）→ fallocate 建镜像 → mkfs.ext4 -F -m 0 → install -d 挂载点
//	                 → mount -o loop → **挂载成功之后**才 GuardScratchTarget 复核设备号
//	                 → 在回环 FS 内 install -d -m 0755 归档根 → 打印 df -h 与 findmnt
//	scratch-cleanup  umount → losetup -d（若镜像仍被占用）→ rm -f 镜像 → rmdir 挂载点
//	                 （**刻意不用 rm -rf**：rmdir 对非空目录会失败，破坏面因此有界）
//	decode-bench     对产物做真实解码，并采样本进程的 VmHWM 峰值（任务 9 的 A6 用）
//	                 默认采样对象 = **本进程** + `/proc`；`--pid/--proc-root` 只用于
//	                 非 Linux 机器上的冒烟与排障（在那台机器上 /proc 不存在）。
//
// 每一步都打印它真正执行的命令与输出；所有破坏性动作都要 `--confirm`。
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/example/llm-gateway/internal/traceverify"
)

const (
	exitOK    = 0
	exitFail  = 1
	exitUsage = 2

	// defaultForbidden 是两个**绝对不能**与目标同设备的路径（D66）：
	// `/` 是 2026-09-24 被写满的那个文件系统，`/datalog` 是生产调用链正文的盘。
	defaultForbidden = "/,/datalog"
)

const usageText = `trace-verify —— A3/A6 的验收工具（不参与生产上线）

用法：
  trace-verify scratch-prepare --image <img> --mount <dir> [--size 1G] --confirm
  trace-verify scratch-cleanup --image <img> --mount <dir> --confirm
  trace-verify decode-bench    --artifact <path> [--concurrency 1,2,3] [--json]

子命令的详细参数：trace-verify <子命令> -h
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(exitUsage)
	}
	var err error
	switch os.Args[1] {
	case "scratch-prepare":
		err = runScratchPrepare(os.Args[2:])
	case "scratch-cleanup":
		err = runScratchCleanup(os.Args[2:])
	case "decode-bench":
		err = runDecodeBench(os.Args[2:])
	case "-h", "--help", "help":
		fmt.Print(usageText)
		return
	default:
		fmt.Fprintf(os.Stderr, "trace-verify: 未知子命令 %q\n%s", os.Args[1], usageText)
		os.Exit(exitUsage)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "trace-verify: %v\n", err)
		os.Exit(exitFail)
	}
	os.Exit(exitOK)
}

// runCmd 打印它要执行的命令（`$ name args…`），再把 stdio 直接接到本进程，
// 最后把失败原样包成 error（`run.sh` 因此能看到真实输出与真实退出码）。
func runCmd(name string, args ...string) error {
	fmt.Printf("$ %s %s\n", name, strings.Join(args, " "))
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("命令失败：%s %s：%w", name, strings.Join(args, " "), err)
	}
	return nil
}

// deviceOf 返回路径所在文件系统的设备号（护栏的判据）。
func deviceOf(path string) (uint64, error) {
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		return 0, err
	}
	return uint64(st.Dev), nil
}

// filesystemAvailableBytes 返回 path 所在文件系统对**非特权进程**可用的字节数。
func filesystemAvailableBytes(path string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, fmt.Errorf("statfs %s：%w", path, err)
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}

// splitCSV 把 `a,b` 拆成 ["a" "b"]（丢掉空白项）。
func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func runScratchPrepare(args []string) error {
	fs := flag.NewFlagSet("scratch-prepare", flag.ContinueOnError)
	image := fs.String("image", "", "回环镜像文件路径（必填）")
	mnt := fs.String("mount", "", "回环挂载点（宿主根 FS 上的普通目录，不承载数据）")
	size := fs.String("size", "1G", "镜像大小（K/M/G/T，1024 进制）")
	confirm := fs.Bool("confirm", false, "显式确认：确认目标是与 / 和 /datalog 都不同设备的专用回环文件系统")
	forbidden := fs.String("forbidden", defaultForbidden, "禁止同设备的路径（逗号分隔）")
	archiveRoot := fs.String("archive-root", "trace-archive-v4", "挂载点内要建的归档根目录名")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *image == "" || *mnt == "" {
		return errors.New("scratch-prepare 需要 --image 与 --mount")
	}
	if filepath.Clean(*image) == "/" || filepath.Clean(*mnt) == "/" {
		return fmt.Errorf("%w: --image / --mount 不得是文件系统根", traceverify.ErrScratchTargetUnsafe)
	}
	if !*confirm {
		return fmt.Errorf("%w: scratch-prepare 会 mkfs 并挂载一块盘 ⇒ 必须显式 --confirm",
			traceverify.ErrScratchTargetUnsafe)
	}
	imageBytes, err := traceverify.ParseSize(*size)
	if err != nil {
		return err
	}
	forb := splitCSV(*forbidden)

	// ① 容量预检（R2）：镜像本身不受设备护栏保护 ⇒ 这是它唯一的前置防线。
	avail, err := filesystemAvailableBytes(filepath.Dir(*image))
	if err != nil {
		return err
	}
	fmt.Printf("[容量预检] 镜像所在文件系统可用 %d bytes；需要 镜像 %d + 余量 %d = %d\n",
		avail, imageBytes, traceverify.ScratchCapacityMarginBytes, imageBytes+traceverify.ScratchCapacityMarginBytes)
	if !traceverify.ScratchCapacitySufficient(avail, imageBytes) {
		return fmt.Errorf("traceverify: 可用空间 %d bytes 不足（需要 %d + 1 GiB 余量，见 R2）⇒ 拒绝建镜像",
			avail, imageBytes)
	}

	// ② 建镜像（fallocate；不支持时由操作者改用 dd 并在报告里写明）。
	if err := runCmd("fallocate", "-l", *size, *image); err != nil {
		return err
	}
	// ③ 专用文件系统（回环 ext4；-m 0 不预留 root 空间，让 ENOSPC 真的发生）。
	if err := runCmd("mkfs.ext4", "-F", "-m", "0", *image); err != nil {
		return err
	}
	// ④ 挂载点：宿主根 FS 上的普通目录（它不承载数据 ⇒ 允许落在根 FS）。
	if err := runCmd("install", "-d", *mnt); err != nil {
		return err
	}
	// ⑤ 挂载回环设备。
	if err := runCmd("mount", "-o", "loop", *image, *mnt); err != nil {
		return err
	}

	// ⑥ ⛔ 设备号护栏必须在**挂载成功之后**：挂载前 `*mnt` 只是宿主根 FS 上的普通
	// 目录（与 `/` 同设备）⇒ 校验必然误拒；挂载后护栏看到的才是回环 ext4 的设备号。
	if err := traceverify.GuardScratchTarget(*mnt, *confirm, deviceOf, forb...); err != nil {
		return err
	}
	fmt.Printf("[护栏] %q 与 %v 都不同设备 ⇒ 允许在其上做真实 ENOSPC\n", *mnt, forb)

	// ⑦ 归档根只能在**挂载之后**建：挂载前建会在宿主根 FS 上造出一个同名空目录
	// （D70 的同一类事故），容器里的 TRACE_BODY_ROOT 就会指到根盘上。
	if err := runCmd("install", "-d", "-m", "0755", filepath.Join(*mnt, *archiveRoot)); err != nil {
		return err
	}

	// ⑧ 留证：df -h 与 findmnt（A3 证据要抄录这两条的真实输出）。
	if err := runCmd("df", "-h", *mnt); err != nil {
		return err
	}
	return runCmd("findmnt", "-T", filepath.Join(*mnt, *archiveRoot), "--output", "TARGET,SOURCE,FSTYPE")
}

func runScratchCleanup(args []string) error {
	fs := flag.NewFlagSet("scratch-cleanup", flag.ContinueOnError)
	image := fs.String("image", "", "回环镜像文件路径（必填）")
	mnt := fs.String("mount", "", "回环挂载点（必填）")
	confirm := fs.Bool("confirm", false, "显式确认：确认要卸载并删除这个回环镜像与挂载点")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *image == "" || *mnt == "" {
		return errors.New("scratch-cleanup 需要 --image 与 --mount")
	}
	if !*confirm {
		return fmt.Errorf("%w: scratch-cleanup 会卸载并删除镜像/目录 ⇒ 必须显式 --confirm",
			traceverify.ErrScratchTargetUnsafe)
	}
	if filepath.Clean(*mnt) == "/" || filepath.Clean(*image) == "/" {
		return fmt.Errorf("%w: --image / --mount 不得是文件系统根", traceverify.ErrScratchTargetUnsafe)
	}

	// ① 卸载（本来就没挂上 ⇒ 非致命，但必须打印出来）。
	if err := runCmd("umount", *mnt); err != nil {
		fmt.Printf("[警告] umount %s 失败（可能本来就没挂载）：%v\n", *mnt, err)
	}
	// ② 若镜像仍被某个 loop 设备占用 ⇒ 解除它（losetup 不存在时只警告）。
	if err := runCmd("losetup", "-j", *image); err != nil {
		fmt.Printf("[警告] losetup -j %s 失败（未安装/util-linux 缺失或本机非 Linux）：%v\n", *image, err)
	}
	// ③ 删镜像：只删命令行给出的**这一个**文件。
	if err := runCmd("rm", "-f", *image); err != nil {
		return err
	}
	// ④ 删目录：只用 rmdir（非空即拒绝 ⇒ 破坏面有界；刻意不用 rm -rf）。
	return runCmd("rmdir", *mnt)
}

// benchResult 是 decode-bench 对**一个并发档位**的读数。
type benchResult struct {
	Concurrency          int    `json:"concurrency"`
	Rounds               int    `json:"rounds"`
	Artifact             string `json:"artifact"`
	WindowBytes          int    `json:"window_bytes"`
	DecodedBytes         int64  `json:"decoded_bytes"`
	ElapsedMillis        int64  `json:"elapsed_ms"`
	PeakVmHWMBytes       int64  `json:"peak_vmhwm_bytes"`
	PeakVmHWMDeltasBytes int64  `json:"peak_vmhwm_delta_bytes"`
	PID                  int    `json:"pid"`
	ProcRoot             string `json:"proc_root"`
}

func runDecodeBench(args []string) error {
	fs := flag.NewFlagSet("decode-bench", flag.ContinueOnError)
	artifact := fs.String("artifact", "", "产物路径（.jsonl.zst，必填）")
	levels := fs.String("concurrency", "1,2,3", "并发档位（逗号分隔）")
	window := fs.Int("window-bytes", 0, "解码窗口（0 ⇒ 512 MiB；必须与写入侧一致）")
	rounds := fs.Int("rounds", 3, "每档每 goroutine 的解码轮数")
	samples := fs.Int("samples", 20, "VmHWM 采样次数")
	interval := fs.Duration("interval", 50*time.Millisecond, "VmHWM 采样间隔")
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	pidFlag := fs.Int("pid", 0, "被采样的 pid（0 ⇒ 本进程；非 Linux 机器排障用）")
	procRoot := fs.String("proc-root", "/proc", "procfs 根（默认 /proc；排障用）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *artifact == "" {
		return errors.New("decode-bench 需要 --artifact")
	}
	if *rounds <= 0 {
		return errors.New("decode-bench 的 --rounds 必须 > 0")
	}
	cons, err := traceverify.ParseConcurrencyLevels(*levels)
	if err != nil {
		return err
	}
	windowBytes := *window
	if windowBytes <= 0 {
		windowBytes = traceverify.DefaultDecodeWindowBytes
	}

	pid := *pidFlag
	if pid == 0 {
		pid = os.Getpid()
	}
	results := make([]benchResult, 0, len(cons))
	for _, c := range cons {
		r, err := benchLevel(*artifact, c, *rounds, windowBytes, *samples, *interval, pid, *procRoot)
		if err != nil {
			return err
		}
		results = append(results, r)
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(results)
	}
	fmt.Printf("采样对象 pid=%d proc_root=%s\n", pid, procRoot)
	fmt.Printf("%-8s %-12s %-10s %-18s %s\n", "并发", "解码字节", "耗时ms", "peak_vmhwm_bytes", "delta_bytes")
	for _, r := range results {
		fmt.Printf("%-8d %-12d %-10d %-18d %d\n",
			r.Concurrency, r.DecodedBytes, r.ElapsedMillis, r.PeakVmHWMBytes, r.PeakVmHWMDeltasBytes)
	}
	return nil
}

// benchLevel 对**一个**并发档位做 rounds 轮真实解码，同时采样本进程的 VmHWM 峰值。
//
// 口径：① `peak_vmhwm_bytes` 是采样窗内观测到的进程峰值（VmHWM 是进程生命周期高水位，
// 因此它是**保守**读数）；② `peak_vmhwm_delta_bytes` = 峰值 − 本档开始前的高水位
// （用于区分「这一档自己涨了多少」与「之前档位留下的高水位」）。采样窗必须覆盖工作负载
// （默认 20 × 50ms = 1s）；工作负载更久时加大 `--samples`。
func benchLevel(artifact string, concurrency, rounds, windowBytes, samples int, interval time.Duration,
	pid int, procRoot string) (benchResult, error) {
	self := []int{pid}
	one := traceverify.VmHWMProbe{Root: procRoot, Samples: 1}
	before, err := one.Peak(self)
	if err != nil {
		return benchResult{}, err
	}

	probe := traceverify.VmHWMProbe{Root: procRoot, Samples: samples, Interval: interval}
	peakCh := make(chan int64, 1)
	errCh := make(chan error, 1)
	go func() {
		peak, err := probe.Peak(self)
		if err != nil {
			errCh <- err
			return
		}
		peakCh <- peak
	}()

	start := time.Now()
	totals := make([]int64, concurrency)
	errs := make([]error, concurrency)
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				n, err := traceverify.DecodeArtifact(artifact, windowBytes)
				if err != nil {
					errs[slot] = err
					return
				}
				totals[slot] += n
			}
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(start)

	var decoded int64
	for i, n := range totals {
		if errs[i] != nil {
			return benchResult{}, errs[i]
		}
		decoded += n
	}

	var peak int64
	select {
	case peak = <-peakCh:
	case err := <-errCh:
		return benchResult{}, err
	}
	delta := peak - before
	if delta < 0 {
		delta = 0
	}
	return benchResult{
		PID:                  pid,
		ProcRoot:             procRoot,
		Concurrency:          concurrency,
		Rounds:               rounds,
		Artifact:             artifact,
		WindowBytes:          windowBytes,
		DecodedBytes:         decoded,
		ElapsedMillis:        elapsed.Milliseconds(),
		PeakVmHWMBytes:       peak,
		PeakVmHWMDeltasBytes: delta,
	}, nil
}
