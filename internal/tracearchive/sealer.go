package tracearchive

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SealerOptions 是封块器配置。
type SealerOptions struct {
	Root            string
	ZstdBinary      string // zstd 可执行文件；空则回退到 "zstd"（部署项，规格 §11.1）
	ZstdLevel       int    // 0 取 defaultZstdLevel
	ZstdWindowBytes int    // 0 取 defaultZstdWindowBytes；换算为 CLI 的 --long=<log2(窗口)>
	MinAge          time.Duration
}

// Sealer 把 spool 明文封块为带内嵌索引的产物文件。
//
// 【方案 C】编码由【C 版 zstd CLI 子进程】完成，不是 Go 库（规格 §5.3.1.1）：
// 同数据、同级别、同窗口下 Go 库产物是 CLI 的 1.90 倍（384.0 MB vs 202.6 MB），
// 且 Go 库峰值 RSS ≈ 窗口 × 2.08，CLI ≈ × 1.08。由此编码器内存完全不落在
// 网关的 2 GiB cgroup 内 —— sealer 是独立进程，跑在宿主机上。
//
// Go 侧只负责：扫描 spool 采集行偏移 → 把 spool 喂给 CLI → 追加 trailer/footer
// → fsync → rename → 删 spool。
// **trailer 与 footer 永远由 Go 侧追加，CLI 只产 zstd 帧。**
type Sealer struct {
	opts  SealerOptions
	paths Paths

	// failBeforeRename 仅供测试注入故障，生产恒为 nil。
	failBeforeRename func() error
}

// NewSealer 构造封块器。
func NewSealer(opts SealerOptions) (*Sealer, error) {
	if opts.Root == "" {
		return nil, fmt.Errorf("tracearchive: Root 不能为空")
	}
	if opts.ZstdBinary == "" {
		opts.ZstdBinary = "zstd"
	}
	if opts.ZstdLevel <= 0 {
		opts.ZstdLevel = defaultZstdLevel
	}
	if opts.ZstdWindowBytes <= 0 {
		opts.ZstdWindowBytes = defaultZstdWindowBytes
	}
	if opts.MinAge <= 0 {
		opts.MinAge = defaultSealInterval
	}
	if _, err := zstdLongArg(opts.ZstdWindowBytes); err != nil {
		return nil, err
	}
	return &Sealer{opts: opts, paths: NewPaths(opts.Root)}, nil
}

// Seal 执行一次封块。任何一步失败都保留 spool 并返回错误，绝不留下半个产物。
//
// 失败语义（规格 §7.2、§9.2）：
//   - 产物已存在           → ErrPartExists（幂等，拒绝覆盖）
//   - spool 行数与记录不符  → ErrRowCountMismatch（保留 spool）
//   - 子进程非零退出/缺失   → ErrSealFailed（保留 spool、删 .tmp）
func (s *Sealer) Seal(ctx context.Context, task SealTask) error {
	partPath := s.paths.PartFile(Ref{DT: task.DT, Inst: task.Inst, Part: task.Part})

	// I4: 同一个 (dt, inst, part) 只允许封块一次。
	// 若产物已存在，宁可留下 spool 也不能覆盖——那是唯一副本。
	if _, err := os.Stat(partPath); err == nil {
		return fmt.Errorf("%w: %s", ErrPartExists, partPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("tracearchive: 检查产物失败: %w", err)
	}

	// ① 先顺序扫描 spool，采集每行首字节的未压缩偏移。
	// 方案 C 下 CLI 是独立进程，Go 侧看不到压缩过程中的行边界，
	// 因此在喂数据之前先扫一遍（一次顺序读，成本可忽略）。
	offsets, _, err := scanSpoolOffsets(task.SpoolPath)
	if err != nil {
		return err
	}
	// ② 行数交叉校验放在启动子进程之前：不匹配就不必浪费一次压缩。
	// task.Rows == 0 表示调用方（ListPendingSeals 扫目录）不知道行数。
	if task.Rows > 0 && len(offsets) != task.Rows {
		return fmt.Errorf("%w: 扫描=%d 调用方=%d（拒绝封块，保留 spool）",
			ErrRowCountMismatch, len(offsets), task.Rows)
	}

	// 【落地修正 1】产物目录必须在创建 .tmp 之前建好。
	// 简报的实现把 MkdirAll 放在 rename 之前，而 .tmp 就落在产物目录里，
	// 于是「目录还不存在」时 os.OpenFile(.tmp) 直接 ENOENT —— 所有真封块用例
	// 都会以「建临时产物失败」告终（实测输出见任务报告）。
	// .tmp 与产物同目录是刻意的（rename 必须同文件系统），因此目录只能提前建。
	if err := os.MkdirAll(filepath.Dir(partPath), 0o755); err != nil {
		return fmt.Errorf("tracearchive: 建产物目录失败: %w", err)
	}

	tmpPath := partPath + ".tmp"
	frameSHA, err := s.runZstd(ctx, task.SpoolPath, tmpPath)
	if err != nil {
		// 子进程非零退出 / 无法启动 / 压缩 I/O 失败：
		// 清理 .tmp、保留 spool、不产生产物（规格 §9.2）。
		os.Remove(tmpPath)
		return err
	}

	// ③ 帧已就绪，此刻追加 trailer 与 footer（CLI 只写了帧）
	if err := s.appendTrailerAndFooter(tmpPath, offsets, frameSHA); err != nil {
		os.Remove(tmpPath)
		return err
	}

	if s.failBeforeRename != nil {
		if err := s.failBeforeRename(); err != nil {
			os.Remove(tmpPath)
			return err
		}
	}

	// I2: 产物只能通过 rename 出现，永不出现半个文件
	if err := os.Rename(tmpPath, partPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("tracearchive: rename 失败: %w", err)
	}
	if err := fsyncDir(filepath.Dir(partPath)); err != nil {
		return fmt.Errorf("tracearchive: fsync 目录失败: %w", err)
	}

	// I5: 到此才允许删 spool
	if err := os.Remove(task.SpoolPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("tracearchive: 删除 spool 失败: %w", err)
	}
	return nil
}

// scanSpoolOffsets 顺序扫描 spool，返回每行首字节的未压缩偏移与总字节数。
//
// 索引记录的是「行首在未压缩流中的位置」。方案 C 下这份偏移必须在喂给 CLI
// 之前采集（见 Seal 注释），且必须与实际喂进去的字节流完全一致——
// 因此这里逐字节累加，不做任何行内改写。
//
// 【残行守卫：为什么必须有它】写入器把 bufio 直接写到 fd 上，盘满（ENOSPC，
// 恰是本子系统要解决的问题）时可能只写出去半行：行尾没有换行符。这种残行
// 【永不可被封块】—— 产物格式假定行稠密且以 '\n' 分隔（index.go 的
// encodeTrailer 是「每行一个差分 varint」），残行会被当成一整行烤进产物，
// 其后的行边界全部错位，点读于是静默返回错误的正文（本项目最不能接受的一类
// 缺陷）。而封块器有两条发现路径：
//
//   - SealTask.Rows > 0（写入器自己报的块）：下面的行数交叉校验会因残行
//     永久 ErrRowCountMismatch —— fail-safe，可接受；
//   - SealTask.Rows == 0（ListPendingSeals 扫目录发现，行数未知）：
//     **该交叉校验被跳过**，残行没有任何别的拦截点。
//
// 因此这里把「spool 必须以换行符结尾」作为硬性前置条件，让第二条路径也变成
// 响亮的拒绝（ErrSealFailed、保留 spool、不产生产物）。注意判据必须是
// 「EOF 时仍带回了非空字节」：0 字节的空 spool 是合法状态（row_count=0），
// 不能被误判为残行。
func scanSpoolOffsets(path string) ([]int64, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, fmt.Errorf("tracearchive: 打开 spool 失败: %w", err)
	}
	defer f.Close()

	br := bufio.NewReaderSize(f, 1<<20)
	var offsets []int64
	var off int64
	for {
		line, readErr := br.ReadBytes('\n')
		// ReadBytes 在 EOF 前找不到 '\n' 时，会同时返回已读到的字节与 io.EOF。
		// 带回了字节就说明最后一行没有换行符 —— 拒绝封块（见上方残行守卫）。
		if readErr == io.EOF && len(line) > 0 {
			return nil, 0, fmt.Errorf(
				"%w: spool 末尾缺少换行符（最后一行不完整，可能落盘时被截断，尾部 %d 字节）: %s",
				ErrSealFailed, len(line), path)
		}
		if len(line) > 0 {
			offsets = append(offsets, off)
			off += int64(len(line))
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, 0, fmt.Errorf("tracearchive: 读 spool 失败: %w", readErr)
		}
	}
	return offsets, off, nil
}

// zstdLongArg 把窗口字节数换算为 CLI 的 --long=<log2(窗口)>。
// 512 MiB = 2^29 ⇒ "29"。zstd 的 --long 语义要求窗口是 2 的幂，故此处严格校验。
func zstdLongArg(windowBytes int) (string, error) {
	if windowBytes <= 0 || windowBytes&(windowBytes-1) != 0 {
		return "", fmt.Errorf("tracearchive: zstd 窗口必须是 2 的幂（实际 %d 字节）", windowBytes)
	}
	return strconv.Itoa(bits.Len(uint(windowBytes)) - 1), nil
}

// runZstd 用 C 版 zstd CLI 把 spoolPath 压缩为 tmpPath，
// 返回 zstd 帧字节的 sha256 前 8 字节。
//
// 命令形如：zstd -19 --long=29 -T1 -c
//
//	-19        级别 19（规格 §5.3.1.1：Go 库 19 与 22 产物相同，22 只多耗 CPU）
//	--long=29  窗口 512 MiB —— 与 Go 库的唯一实质差异（长距离匹配器）
//	-T1        单线程：多并发会把流切成独立块，显著削弱压缩比
//	-c         写 stdout（不让 zstd 自己命名文件）
//
// 退出码非 0 一律视为封块失败（ErrSealFailed），绝不接受「看起来有产物」的半成品。
func (s *Sealer) runZstd(ctx context.Context, spoolPath, tmpPath string) ([8]byte, error) {
	var empty [8]byte

	long, err := zstdLongArg(s.opts.ZstdWindowBytes)
	if err != nil {
		return empty, err
	}

	src, err := os.Open(spoolPath)
	if err != nil {
		return empty, fmt.Errorf("%w: 打开 spool 失败: %w", ErrSealFailed, err)
	}
	defer src.Close()

	// O_RDWR：子进程作为 stdout 写入，随后 Go 侧还要 seek 回起点算帧的 sha256
	dst, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o644)
	if err != nil {
		return empty, fmt.Errorf("%w: 建临时产物失败: %w", ErrSealFailed, err)
	}
	defer dst.Close()

	args := []string{"-" + strconv.Itoa(s.opts.ZstdLevel), "--long=" + long, "-T1", "-c"}
	cmd := exec.CommandContext(ctx, s.opts.ZstdBinary, args...)
	cmd.Stdin = src  // 直接把文件交给子进程，不经内存中转
	cmd.Stdout = dst // 只写 zstd 帧
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return empty, fmt.Errorf("%w: 封块上下文取消: %w", ErrSealFailed, ctxErr)
		}
		// 【落地修正 2】把「子进程因二进制缺失而无法启动」归一化到 exec.ErrNotFound。
		//
		// 为什么不能只靠 %w：exec.Command 只对【不含路径分隔符】的 name 走 LookPath，
		// 而部署时 ZstdBinary 往往是一个绝对路径（/usr/bin/zstd）。实测
		//（go1.26.1/darwin）此时 cmd.Run 返回的是 *fs.PathError(ENOENT)，
		// errors.Is(err, exec.ErrNotFound) 为 false —— 调用方（sealer 循环/告警）
		// 就无法把「zstd 没装/路径配错」这类部署事故与「压缩本身失败」区分开。
		// 因此这里显式把「文件不存在」这一支同时挂上 exec.ErrNotFound 与原始错误。
		if errors.Is(err, os.ErrNotExist) {
			return empty, fmt.Errorf("%w: 找不到 zstd 可执行文件 %q（部署事故，规格 §11.1）: %w: %w",
				ErrSealFailed, s.opts.ZstdBinary, exec.ErrNotFound, err)
		}
		return empty, fmt.Errorf("%w: zstd 子进程失败（args=%v）: %w（stderr: %s）",
			ErrSealFailed, args, err, strings.TrimSpace(stderr.String()))
	}

	// 此刻 .tmp 的内容即纯 zstd 帧（trailer/footer 尚未追加），据此计算 frame_sha256
	if _, err := dst.Seek(0, io.SeekStart); err != nil {
		return empty, fmt.Errorf("%w: 回绕临时产物失败: %w", ErrSealFailed, err)
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, dst); err != nil {
		return empty, fmt.Errorf("%w: 计算帧摘要失败: %w", ErrSealFailed, err)
	}
	var sum [8]byte
	copy(sum[:], hasher.Sum(nil)[:8])
	return sum, nil
}

// appendTrailerAndFooter 在临时产物末尾追加索引与定长 footer，并 fsync。
//
// 【方案 C 的关键点】CLI 只写了 zstd 帧，索引与 footer 全部在这里由 Go 侧追加，
// 保证索引与内容出自同一次封块操作（I3）。
func (s *Sealer) appendTrailerAndFooter(tmpPath string, offsets []int64, frameSHA [8]byte) error {
	trailer := encodeTrailer(offsets)

	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("tracearchive: 打开临时产物失败: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}
	trailerOffset := info.Size()

	if _, err := f.Write(trailer); err != nil {
		return fmt.Errorf("tracearchive: 写 trailer 失败: %w", err)
	}
	footerBytes := encodeFooter(footer{
		TrailerOffset: trailerOffset,
		TrailerLen:    int32(len(trailer)),
		RowCount:      int32(len(offsets)),
		FrameSHA:      frameSHA,
	})
	if _, err := f.Write(footerBytes); err != nil {
		return fmt.Errorf("tracearchive: 写 footer 失败: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("tracearchive: fsync 临时产物失败: %w", err)
	}
	return nil
}

// fsyncDir 目录 fsync：保证 rename 本身落盘，否则崩溃后可能看不到产物。
func fsyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// ListPendingSeals 扫描 <root>/spool/dt=*/inst=*/part-*.jsonl，
// 返回 mtime 早于 now-minAge 的 spool（即「已停止追加」的块）。
//
// 为什么用 mtime + minAge 而不是信号（规格 §7.2）：sealer 是独立进程，
// 网关无法给它发信号，而目录扫描不需要任何进程间通信，并且【幂等且可重启】——
// 任何一轮崩溃后，下一轮扫描会重新发现同一个 spool 并重试
// （产物已存在则由 Seal 以 ErrPartExists 拒绝覆盖）。
//
// 纯函数：只读文件系统元信息，不修改任何状态，因此可直接单测。
//
// 【minAge 必须 > 网关的 part 滚动间隔 —— 它不是性能参数，是本进程唯一的保护】
// 网关自己周期滚动 part（到年龄即 MarkSealed 换新 part、关闭旧 fd），而 sealer
// 是独立进程、收不到任何信号：本进程无法区分「已停止追加」与「暂时空闲」，
// 两者的唯一区别就是这个时间差。若 minAge 不大于滚动间隔，就会去封一个网关
// 仍在追加的块 —— 那些行写进的是已被 unlink 的 inode：字节落盘却永久不可达，
// 且它们的 Ref 指向一个行数更少的产物（静默丢数据，见计划缺陷 D7）。
//
// 调用方还必须保证 minAge 远大于网关的 flush 周期（规格 §7.1）：刚建立的块在
// 磁盘上可能还是空文件、mtime 停留在创建时刻，只有 flush 才会推进它。
// 默认值取 defaultSealInterval（15 分钟，与规格 seal_interval 同值）。
func ListPendingSeals(root string, minAge time.Duration) ([]SealTask, error) {
	spoolRoot := filepath.Join(root, "spool")
	cutoff := time.Now().Add(-minAge)

	dts, err := os.ReadDir(spoolRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // 还没写过任何正文
		}
		return nil, fmt.Errorf("tracearchive: 读 spool 根目录失败: %w", err)
	}

	var out []SealTask
	for _, dtEnt := range dts {
		if !dtEnt.IsDir() || !strings.HasPrefix(dtEnt.Name(), "dt=") {
			continue
		}
		dt := strings.TrimPrefix(dtEnt.Name(), "dt=")
		dtDir := filepath.Join(spoolRoot, dtEnt.Name())

		insts, err := os.ReadDir(dtDir)
		if err != nil {
			return nil, fmt.Errorf("tracearchive: 读 %s 失败: %w", dtDir, err)
		}
		for _, instEnt := range insts {
			if !instEnt.IsDir() || !strings.HasPrefix(instEnt.Name(), "inst=") {
				continue
			}
			inst := strings.TrimPrefix(instEnt.Name(), "inst=")
			dir := filepath.Join(dtDir, instEnt.Name())

			ents, err := os.ReadDir(dir)
			if err != nil {
				return nil, fmt.Errorf("tracearchive: 读 %s 失败: %w", dir, err)
			}
			for _, e := range ents {
				// 只认 spool 名 part-NNNNNN.jsonl；
				// .tmp、产物名（.jsonl.zst）、非 part- 前缀一律忽略。
				if !strings.HasSuffix(e.Name(), ".jsonl") {
					continue
				}
				part, ok := parsePartSeq(e.Name())
				if !ok {
					continue
				}
				info, err := e.Info()
				if err != nil {
					return nil, fmt.Errorf("tracearchive: stat %s 失败: %w", e.Name(), err)
				}
				if info.ModTime().After(cutoff) {
					continue // 仍在追加（或刚写过），不碰
				}
				out = append(out, SealTask{
					DT:                dt,
					Inst:              inst,
					Part:              part,
					SpoolPath:         filepath.Join(dir, e.Name()),
					UncompressedBytes: info.Size(),
					// Rows 留 0：扫目录时不知道行数，交给 Seal 扫描后自证
				})
			}
		}
	}
	// 稳定顺序（dt/inst/part），便于测试断言与日志排查
	sort.Slice(out, func(i, j int) bool {
		if out[i].DT != out[j].DT {
			return out[i].DT < out[j].DT
		}
		if out[i].Inst != out[j].Inst {
			return out[i].Inst < out[j].Inst
		}
		return out[i].Part < out[j].Part
	})
	return out, nil
}

// ListPendingSeals 用本实例配置的 MinAge 发现待封块。
//
// 与包级 ListPendingSeals(root, minAge) 是同一个实现：Sealer 持有 Root 与 MinAge
// 两个配置项，sealer 循环（计划 4/8）只需构造一次 Sealer 即可发现并封块，
// 不必在两处各自维护同一组参数。
func (s *Sealer) ListPendingSeals() ([]SealTask, error) {
	return ListPendingSeals(s.opts.Root, s.opts.MinAge)
}
