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
//	decode-bench     对产物做**真实的 K 路并发点读**（tracearchive.NewReader + GetBody，
//	                 与读侧同形），并采样本进程的 VmHWM 峰值（任务 9 的 A6/C5 用）。
//	                 默认采样对象 = **本进程** + `/proc`；`--pid/--proc-root` 只用于
//	                 非 Linux 机器上的冒烟与排障（在那台机器上 /proc 不存在）。
//	                 它为每个并发档位**连续跑 ≥5 轮**，并分别报告「有 GC」与
//	                 禁掉 GC（GOGC=off）两种形态的峰值（计划任务 9 步骤 4.2）。
//
// 每一步都打印它真正执行的命令与输出；所有破坏性动作都要 `--confirm`。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/example/llm-gateway/internal/tracearchive"
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
  trace-verify decode-bench    --artifact <path> [--concurrency 1,2,3] [--rounds 5] [--points 50] [--json]

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

// benchResult 是 decode-bench 对**一个（并发档位 × GC 模式）**的读数。
//
// 字段口径：
//   - `points` 是每个 goroutine **每轮**的点读数（计划步骤 3 的 50）；`total_points` 是
//     本次实际完成的点读总数（= concurrency × rounds × points）—— 两者都报告，前者对齐
//     计划给出的输出形状，后者是「负载真的跑满了」的自证。
//   - `max_in_flight` 是**实测**同时在飞点数（配置了 K 却实际串行 ⇒ 它小于 K，读数不可用）；
//   - `gc_cycles` 是该档位期间发生的 GC 次数（`runtime.MemStats.NumGC` 差值）：
//     `gc_mode=off` 时它必须为 0（否则说明 GC 没被真正禁掉，不能当 GOGC=off 的证据）；
//   - `peak_vmhwm_bytes` 是 VmHWM（进程生命周期高水位，**保守**读数）；
//     `peak_vmhwm_delta_bytes` = 本档峰值 − 本档开始前的高水位（区分「这一档自己涨了多少」）。
type benchResult struct {
	Concurrency          int    `json:"concurrency"`
	GCMode               string `json:"gc_mode"`
	Rounds               int    `json:"rounds"`
	Points               int    `json:"points"`
	TotalPoints          int    `json:"total_points"`
	ExpectedPoints       int    `json:"expected_points"`
	Aborted              bool   `json:"aborted"`
	MaxInFlight          int    `json:"max_in_flight"`
	DistinctRows         int    `json:"distinct_rows"`
	Rows                 int    `json:"rows"`
	Artifact             string `json:"artifact"`
	WindowBytes          int    `json:"window_bytes"`
	Bytes                int64  `json:"bytes"`
	ElapsedMillis        int64  `json:"elapsed_ms"`
	GCCycles             uint32 `json:"gc_cycles"`
	PeakVmHWMBytes       int64  `json:"peak_vmhwm_bytes"`
	PeakVmHWMDeltasBytes int64  `json:"peak_vmhwm_delta_bytes"`
	PID                  int    `json:"pid"`
	ProcRoot             string `json:"proc_root"`
}

// parseArtifactRef 从产物路径反解出读侧需要的定位键与 root：
// `<root>/dt=<dt>/inst=<inst>/part-NNNNNN.jsonl.zst`（冻结包 Paths 的布局）。
// 布局不符 ⇒ 报错（绝不猜：猜错的 root 会让点读落到另一个目录，读数就失去意义）。
func parseArtifactRef(artifact string) (string, tracearchive.Ref, error) {
	var ref tracearchive.Ref
	base := filepath.Base(artifact)
	if !strings.HasPrefix(base, "part-") || !strings.HasSuffix(base, ".jsonl.zst") {
		return "", ref, fmt.Errorf("decode-bench: 产物名 %q 不是 part-NNNNNN.jsonl.zst", base)
	}
	seq := strings.TrimSuffix(strings.TrimPrefix(base, "part-"), ".jsonl.zst")
	if len(seq) != 6 {
		return "", ref, fmt.Errorf("decode-bench: 块序号 %q 不是恰好 6 位（冻结包 partName 的约定）", seq)
	}
	part, err := strconv.Atoi(seq)
	if err != nil {
		return "", ref, fmt.Errorf("decode-bench: 块序号 %q 不是整数: %w", seq, err)
	}
	instDir := filepath.Base(filepath.Dir(artifact))
	dtDir := filepath.Base(filepath.Dir(filepath.Dir(artifact)))
	if !strings.HasPrefix(instDir, "inst=") || !strings.HasPrefix(dtDir, "dt=") {
		return "", ref, fmt.Errorf("decode-bench: 产物父目录必须是 inst=<inst>/dt=<dt>（得到 %q/%q）", dtDir, instDir)
	}
	ref.Part = part
	ref.Inst = strings.TrimPrefix(instDir, "inst=")
	ref.DT = strings.TrimPrefix(dtDir, "dt=")
	if ref.Inst == "" || ref.DT == "" {
		return "", ref, fmt.Errorf("decode-bench: dt/inst 不能为空（得到 dt=%q inst=%q）", ref.DT, ref.Inst)
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(artifact))), ref, nil
}

// parseGCModes 解析 `--gc-modes`：只接受 on / off，至少一个、不重复。
func parseGCModes(s string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if p != "on" && p != "off" {
			return nil, fmt.Errorf("decode-bench: --gc-modes 里的 %q 非法（只接受 on / off）", p)
		}
		if seen[p] {
			return nil, fmt.Errorf("decode-bench: --gc-modes 里的 %q 重复", p)
		}
		seen[p] = true
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, errors.New("decode-bench: --gc-modes 不能为空（形如 `on,off`）")
	}
	return out, nil
}

func runDecodeBench(args []string) error {
	fs := flag.NewFlagSet("decode-bench", flag.ContinueOnError)
	artifact := fs.String("artifact", "", "产物路径（.jsonl.zst，必填；必须在 <root>/dt=<dt>/inst=<inst>/ 下）")
	levels := fs.String("concurrency", "1,2,3", "并发档位（逗号分隔）")
	rounds := fs.Int("rounds", 5, "每档每 GC 模式的连续轮数（计划步骤 4.2 要求 ≥5）")
	points := fs.Int("points", traceverify.DefaultPointsPerGoroutine, "每个 goroutine 每轮的点读数（计划步骤 3 的 50）")
	rows := fs.Int("rows", 0, "块内行数（0 ⇒ 用 GetBody 的错误身份探测；探测本身会推高 VmHWM）")
	window := fs.Int("window-bytes", 0, "解码窗口（0 ⇒ 512 MiB；必须与写入侧一致）")
	gcModes := fs.String("gc-modes", "on,off", "GC 形态（on / off；off ⇒ 真正禁掉 GC 以复测「连续多轮不做 GC」）")
	samples := fs.Int("samples", 0, "VmHWM 采样次数上限（0 ⇒ 采到负载结束）")
	interval := fs.Duration("interval", traceverify.DefaultSampleInterval, "VmHWM 采样间隔")
	abortAt := fs.Int64("abort-at-bytes", traceverify.ControlPlaneLimitBytes,
		"观测峰值达到该字节数 ⇒ 中止这一档（0 ⇒ 不中止）。GOGC=off 下必须设它，否则进程会被 OOM 杀掉、什么都报不出来")
	jsonOut := fs.Bool("json", false, "以 JSON 输出")
	pidFlag := fs.Int("pid", 0, "被采样的 pid（0 ⇒ 本进程；非 Linux 机器排障用）")
	procRoot := fs.String("proc-root", "/proc", "procfs 根（默认 /proc；排障用）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *artifact == "" {
		return errors.New("decode-bench 需要 --artifact")
	}
	if *rounds < 5 {
		return fmt.Errorf("decode-bench 的 --rounds 必须 ≥5（计划任务 9 步骤 4.2：要复测「连续多轮不做 GC」的形态），得到 %d", *rounds)
	}
	if *points <= 0 {
		return fmt.Errorf("decode-bench 的 --points 必须 > 0（得到 %d）", *points)
	}
	if *rows < 0 {
		return fmt.Errorf("decode-bench 的 --rows 不能为负（得到 %d）", *rows)
	}
	cons, err := traceverify.ParseConcurrencyLevels(*levels)
	if err != nil {
		return err
	}
	modes, err := parseGCModes(*gcModes)
	if err != nil {
		return err
	}
	windowBytes := *window
	if windowBytes <= 0 {
		windowBytes = traceverify.DefaultDecodeWindowBytes
	}

	root, ref, err := parseArtifactRef(*artifact)
	if err != nil {
		return err
	}
	// Reader 的 MaxConcurrent 取最大档位：否则 sem 会把实际并发串行化，
	// 「实测同时在飞点数」就永远到不了档位值（那会让 A6 的结论失真）。
	maxK := cons[len(cons)-1]
	reader, err := tracearchive.NewReader(tracearchive.ReaderOptions{
		Root:            root,
		ZstdWindowBytes: windowBytes,
		MaxConcurrent:   maxK,
	})
	if err != nil {
		return err
	}
	defer reader.Close()
	read := func(ctx context.Context, r tracearchive.Ref) ([]byte, error) {
		return reader.GetBody(ctx, r)
	}

	rowCount := *rows
	if rowCount == 0 {
		rowCount, err = traceverify.ArtifactRows(context.Background(), read, ref, 0)
		if err != nil {
			return fmt.Errorf("decode-bench: 探测 %s 的行数失败（可用 --rows 直接给定）：%w", ref, err)
		}
		fmt.Fprintf(os.Stderr, "decode-bench: 探测到 %s 共 %d 行（探测过程本身会推高 VmHWM —— 保守方向）\n", ref, rowCount)
	}

	pid := *pidFlag
	if pid == 0 {
		pid = os.Getpid()
	}

	params := benchParams{
		artifact:    *artifact,
		ref:         ref,
		rows:        rowCount,
		windowBytes: windowBytes,
		points:      *points,
		rounds:      *rounds,
		samples:     *samples,
		interval:    *interval,
		abortAt:     *abortAt,
		pid:         pid,
		procRoot:    *procRoot,
		read:        read,
	}
	results := make([]benchResult, 0, len(cons)*len(modes))
	for _, c := range cons {
		for mi, mode := range modes {
			r, err := benchLevel(params, c, mode, int64(mi))
			if err != nil {
				return err
			}
			results = append(results, r)
		}
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(results)
	}
	fmt.Printf("采样对象 pid=%d proc_root=%s rows=%d 中止阈值=%d\n", pid, *procRoot, rowCount, *abortAt)
	fmt.Printf("%-8s %-5s %-10s %-10s %-9s %-18s %s\n",
		"并发", "GC", "点数/期望", "在飞峰值", "GC次数", "peak_vmhwm_bytes", "delta_bytes")
	for _, r := range results {
		note := ""
		if r.Aborted {
			note = "（到限中止 ⇒ 峰值 ≥ 阈值）"
		}
		fmt.Printf("%-8d %-5s %-10s %-10d %-9d %-18d %d%s\n",
			r.Concurrency, r.GCMode, fmt.Sprintf("%d/%d", r.TotalPoints, r.ExpectedPoints),
			r.MaxInFlight, r.GCCycles, r.PeakVmHWMBytes, r.PeakVmHWMDeltasBytes, note)
	}
	return nil
}

// benchParams 是一次 decode-bench 的公共参数（每个（并发档 × GC 模式）共用）。
type benchParams struct {
	artifact    string
	ref         tracearchive.Ref
	rows        int
	windowBytes int
	points      int
	rounds      int
	samples     int
	interval    time.Duration
	abortAt     int64
	pid         int
	procRoot    string
	read        traceverify.PointReadFunc
}

// benchLevel 对**一个（并发档位 × GC 模式）**做 rounds 轮真实点读，同时采样本进程的
// VmHWM 峰值；`AbortAtBytes > 0` 时峰值一到阈值就中止这一档（避免 GOGC=off 把进程跑到
// 被 OOM 杀掉、什么都报不出来）。
//
// 口径：① `peak_vmhwm_bytes` 是采样窗内观测到的进程峰值（VmHWM 是进程生命周期高水位，
// 因此它是**保守**读数）；② `peak_vmhwm_delta_bytes` = 峰值 − 本档开始前的高水位
// （区分「这一档自己涨了多少」与「之前档位留下的高水位」）。
//
// `gcMode=off` 用 `debug.SetGCPercent(-1)` **真正**禁掉 GC（并在返回前恢复原值）；
// `gcMode=on` 显式设成 100（Go 默认值）—— 于是「两种形态」不依赖调用者的 GOGC 环境变量。
func benchLevel(p benchParams, concurrency int, gcMode string, seedBase int64) (benchResult, error) {
	probe := traceverify.VmHWMProbe{Root: p.procRoot, Samples: 1}
	peakFn := func() (int64, error) { return probe.Peak([]int{p.pid}) }
	before, err := peakFn()
	if err != nil {
		return benchResult{}, err
	}

	// GC 形态：显式设置、返回前恢复（不把状态留给调用者）。
	prevGC := debug.SetGCPercent(100)
	if gcMode == "off" {
		prevGC = debug.SetGCPercent(-1)
	}
	defer debug.SetGCPercent(prevGC)

	var beforeStats, afterStats runtime.MemStats
	runtime.ReadMemStats(&beforeStats)

	load := traceverify.PointReadLoad{
		Ref:                p.ref,
		Rows:               p.rows,
		Concurrency:        concurrency,
		Rounds:             p.rounds,
		PointsPerGoroutine: p.points,
		Seed:               seedBase*1_000_003 + int64(concurrency),
		Read:               p.read,
		Peak:               peakFn,
		AbortAtBytes:       p.abortAt,
		SampleInterval:     p.interval,
		MaxSamples:         p.samples,
	}
	res, err := load.Run(context.Background())
	if err != nil {
		return benchResult{}, err
	}
	runtime.ReadMemStats(&afterStats)

	// VmHWM 单调不减 ⇒ 负载之后再读一次，取三者最大，避免采样窗错过峰值。
	after, err := peakFn()
	if err != nil {
		return benchResult{}, err
	}
	peak := res.PeakBytes
	if after > peak {
		peak = after
	}
	if before > peak {
		peak = before
	}

	expected := concurrency * p.rounds * p.points
	// 读数自证：没跑满 / 没有真正并发 ⇒ 这一档的峰值不能用来回答「K 是否安全」。
	// 到限中止是**预期结局**（正是它要证明的东西），因此不适用这两条。
	if !res.Aborted {
		if res.Points != expected {
			return benchResult{}, fmt.Errorf(
				"decode-bench: 实际点读数 %d != 期望 %d（K × rounds × points）⇒ 负载没跑满，读数不可用",
				res.Points, expected)
		}
		if res.MaxInFlight != concurrency {
			return benchResult{}, fmt.Errorf(
				"decode-bench: 实测同时在飞峰值 %d != 档位 %d ⇒ 该档位没有真正并发（峰值会偏低），读数不可用",
				res.MaxInFlight, concurrency)
		}
	}
	delta := peak - before
	if delta < 0 {
		delta = 0
	}
	return benchResult{
		PID:                  p.pid,
		ProcRoot:             p.procRoot,
		Concurrency:          concurrency,
		GCMode:               gcMode,
		Rounds:               p.rounds,
		Points:               p.points,
		TotalPoints:          res.Points,
		ExpectedPoints:       expected,
		Aborted:              res.Aborted,
		MaxInFlight:          res.MaxInFlight,
		DistinctRows:         res.DistinctRows,
		Rows:                 p.rows,
		Artifact:             p.artifact,
		WindowBytes:          p.windowBytes,
		Bytes:                res.Bytes,
		ElapsedMillis:        res.Elapsed.Milliseconds(),
		GCCycles:             afterStats.NumGC - beforeStats.NumGC,
		PeakVmHWMBytes:       peak,
		PeakVmHWMDeltasBytes: delta,
	}, nil
}
