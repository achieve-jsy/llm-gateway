package tracearchive

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/klauspost/compress/zstd"
)

// maxTrailerBytes 是 trailer 大小的合理性上界。
//
// 真实 trailer 是「每行一个差分 varint」：1.5 GiB 的块按每行约 400 KB 估算只有
// 约 4000 行，编码后约 12 KB；即便每行只有 1 字节，也需要上千万行才能撑到这个量级。
//
// 不设上界时，一个损坏的 footer 可以让 trailer_len 接近文件大小，
// 进而让 decodeTrailer 试图分配其 8 倍的 int64 容量，在 control-plane 的
// 2 GiB 限额内 OOM。这是「把产物损坏挡成错误」的最后一道闸，
// 与规格 §8.1 的意图一致（损坏必须报成损坏，不能伪装成「数据不存在」）。
const maxTrailerBytes = 64 << 20

// ReaderOptions 是点读器配置。
type ReaderOptions struct {
	Root            string
	ZstdWindowBytes int // 0 取 defaultZstdWindowBytes；必须与写入侧一致
	MaxConcurrent   int // 解码器内存 ≈ 窗口大小，故并发必须设上限（规格 §8.4 C5）；0 取 2
}

// Reader 按定位键取回正文行。
//
// 空间硬约束（规格 §8.3）：
//
//	C1 流式解压到内存，命中目标行立即停止 —— 峰值内存 ≈ 一行，与块大小无关
//	C2 不落任何临时文件
//	C3 不缓存已解压的块
//	C4 读 spool 直接顺序读，不复制
type Reader struct {
	opts  ReaderOptions
	paths Paths

	// sem 限制并发解码。解码器内存≈窗口（512 MiB 窗口约需 512~600 MB），
	// 不设上限会把 control-plane 撑爆。
	sem chan struct{}
}

// NewReader 构造点读器。
func NewReader(opts ReaderOptions) (*Reader, error) {
	if opts.Root == "" {
		return nil, fmt.Errorf("tracearchive: Root 不能为空")
	}
	if opts.ZstdWindowBytes <= 0 {
		opts.ZstdWindowBytes = defaultZstdWindowBytes
	}
	if opts.MaxConcurrent <= 0 {
		opts.MaxConcurrent = 2
	}
	return &Reader{
		opts:  opts,
		paths: NewPaths(opts.Root),
		sem:   make(chan struct{}, opts.MaxConcurrent),
	}, nil
}

// Close 释放资源。当前实现无长驻资源，仅为接口完整性保留。
func (r *Reader) Close() error { return nil }

// inFlight 返回当前占用的并发槽位数，供测试断言锁已释放。
func (r *Reader) inFlight() int { return len(r.sem) }

// GetBody 返回定位键对应的正文行（不含换行）。
//
// 查找顺序（规格 §8.2）：产物 → spool。
// 两者不会同时存在：spool 只在产物 fsync + rename + 目录 fsync 全部成功后才删除
// （见 Sealer.Seal），因此这里不存在竞态。
//
// 错误身份是契约（规格 §8.1）：ErrBodyMissing（标记行，404）、
// ErrNotArchived（元数据与存储不一致，500）、ErrFooterCorrupt / ErrTrailerCorrupt
// （产物损坏，500）。全部用 %w 包装，调用方一律用 errors.Is 分类。
func (r *Reader) GetBody(ctx context.Context, ref Ref) ([]byte, error) {
	partPath := r.paths.PartFile(ref)

	if _, err := os.Stat(partPath); err == nil {
		return r.readFromPart(ctx, ref, partPath)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("tracearchive: 检查产物失败: %w", err)
	}

	spoolPath := r.paths.SpoolFile(ref)
	if _, err := os.Stat(spoolPath); err == nil {
		return readFromSpool(spoolPath, ref.Row)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("tracearchive: 检查 spool 失败: %w", err)
	}

	// 产物与 spool 都不存在：这是元数据与存储不一致，属异常。
	// 绝不能返回「数据不存在」——那会把存储故障伪装成业务事实（v3 方案的教训）。
	return nil, fmt.Errorf("%w: %s", ErrNotArchived, ref)
}

// readFromPart 从产物文件读取。全程流式，不落临时文件、不缓存。
func (r *Reader) readFromPart(ctx context.Context, ref Ref, partPath string) ([]byte, error) {
	// 并发上限（规格 §8.4 C5）：解码器内存 ≈ 窗口（512 MiB 量级），
	// 不设上限会把 control-plane（2 GiB 限额）撑爆。spool 路径不占槽位（C6）。
	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	f, err := os.Open(partPath)
	if err != nil {
		return nil, fmt.Errorf("tracearchive: 打开产物失败: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := info.Size()
	if size < FooterSize {
		return nil, fmt.Errorf("%w: 文件仅 %d 字节 < footer %d 字节", ErrFooterCorrupt, size, FooterSize)
	}

	// C1 前置：读 footer 只需末尾 32 字节，不必解压任何数据
	fb := make([]byte, FooterSize)
	if _, err := f.ReadAt(fb, size-FooterSize); err != nil {
		return nil, fmt.Errorf("tracearchive: 读 footer 失败: %w", err)
	}
	ft, err := decodeFooter(fb)
	if err != nil {
		return nil, err
	}
	if ft.TrailerOffset+int64(ft.TrailerLen) != size-FooterSize {
		return nil, fmt.Errorf("%w: trailer 未紧邻 footer（off=%d len=%d size=%d）",
			ErrFooterCorrupt, ft.TrailerOffset, ft.TrailerLen, size)
	}
	// 行号越界必须在此明确拒绝，不能指望后续解码偶然失败
	if ref.Row < 0 || ref.Row >= int(ft.RowCount) {
		return nil, fmt.Errorf("%w: %s（块内共 %d 行）", ErrNotArchived, ref, ft.RowCount)
	}
	// trailer 大小的合理性上界。decodeTrailer 的容量上限是 trailer 字节数的 8 倍
	//（这是格式固有的：len(b) 字节的 varint 流可能编码出 len(b) 个 int64 条目），
	// 而 trailer_len 只被「紧邻 footer」这一条绑在文件大小上 ⇒ 一个 1.5 GiB 的块
	// 仍可给出接近 1.5 GiB 的 trailer，让 control-plane（2 GiB 限额）尝试分配数 GB。
	// 归入「产物损坏」，与 §8.1 的意图一致。
	if int64(ft.TrailerLen) > maxTrailerBytes {
		return nil, fmt.Errorf("%w: trailer_len=%d 超过上界 %d", ErrFooterCorrupt, ft.TrailerLen, maxTrailerBytes)
	}

	// 【R10 第 2 项】校验 footer 的 frame_sha256[:8]：重算 zstd 帧字节
	//（[0, trailer_offset)）的 SHA-256 前 8 字节并与 footer 比对（规格 §8.2 第 3 步）。
	//
	// 为什么必须有：该字段由 Task 6 写入，但在此之前**只写不读**，等于完整性检查
	// 从未执行——而「发现半写 / 位腐」正是它存在的全部理由。不符 ⇒ ErrFooterCorrupt
	// （500 + 诊断），绝不能静默返回「不存在」，更不能把位腐后的字节当正文解出来。
	//
	// 【位置】放在所有便宜的元信息校验之后、任何大分配与解压之前：
	//  - 放在 trailer_len 上界之后，是为了让「上界」这条闸单独可见（越界产物在
	//    这里就返回了，不会先被摘要校验抢走身份）；
	//  - 放在解码之前，是因为摘要校验的目的就是「先证明帧字节可信，再把它们喂给解码器」。
	//
	// 【成本与空间约束】一次前向顺序读 + 流式哈希（io.Copy/SHA-256 的
	// 固定大小缓冲区），峰值内存为常数、不落临时文件 ⇒ 不违反 §8.3 C1/C2/C3。
	if err := verifyFrameSHA(f, ft); err != nil {
		return nil, err
	}

	tb := make([]byte, ft.TrailerLen)
	if _, err := f.ReadAt(tb, ft.TrailerOffset); err != nil {
		return nil, fmt.Errorf("tracearchive: 读 trailer 失败: %w", err)
	}
	// 传入 footer 记录的期望条目数：这样被截断的 trailer 会被识别为损坏，
	// 而不是静默解码出更少的条目（那会让越界读取伪装成正常读取）。
	offsets, err := decodeTrailer(tb, int(ft.RowCount))
	if err != nil {
		// footer 与 trailer 不一致 ⇒ 产物损坏，绝不能报成「数据不存在」
		return nil, err
	}
	targetOff := offsets[ref.Row]

	// 流式解压：从头解压到目标偏移，命中即停
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	dec, err := zstd.NewReader(f,
		zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderMaxWindow(uint64(r.opts.ZstdWindowBytes)),
	)
	if err != nil {
		return nil, fmt.Errorf("tracearchive: 建 zstd 解码器失败: %w", err)
	}
	defer dec.Close()

	br := bufio.NewReaderSize(dec, 1<<20)
	var read int64
	for read <= targetOff {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line, readErr := br.ReadBytes('\n')
		if len(line) > 0 && read == targetOff {
			// 标记行（正文写入时即失败，规格 §7.1/§8.2）：绝不能把 "null" 当正文返回。
			// 复用 Task 5 的 failedRowMarker：同包共用一份定义。
			if isFailedRowMarker(line) {
				return nil, fmt.Errorf("%w: %s", ErrBodyMissing, ref)
			}
			return trimNewline(line), nil
		}
		if len(line) > 0 {
			read += int64(len(line))
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return nil, fmt.Errorf("tracearchive: 解压读取失败: %w", readErr)
		}
	}
	return nil, fmt.Errorf("%w: %s（偏移 %d 未命中行首）", ErrNotArchived, ref, targetOff)
}

// verifyFrameSHA 重算 [0, ft.TrailerOffset) 的 SHA-256 前 8 字节并与 footer 比对
// （R10 第 2 项，规格 §8.2 第 3 步）。不符返回 ErrFooterCorrupt。
//
// 必须在 header/footer 的元信息校验通过之后调用：TrailerOffset 非负且
// TrailerOffset+TrailerLen == size-FooterSize 已经成立，因此这里的读范围一定在文件内。
func verifyFrameSHA(f *os.File, ft footer) error {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("tracearchive: 回绕产物失败: %w", err)
	}
	hasher := sha256.New()
	// 必须把范围**限死在 [0, TrailerOffset)**：把 trailer/footer 也算进摘要，
	// 摘要就恒不匹配 ⇒ 每一次点读都报「产物损坏」（比不做校验更糟）。
	if _, err := io.Copy(hasher, io.LimitReader(f, ft.TrailerOffset)); err != nil {
		return fmt.Errorf("tracearchive: 计算帧摘要失败: %w", err)
	}
	var sum [8]byte
	copy(sum[:], hasher.Sum(nil)[:8])
	if sum != ft.FrameSHA {
		return fmt.Errorf("%w: frame_sha256 不符（footer 记录 %x，实算 %x；帧区 %d 字节）",
			ErrFooterCorrupt, ft.FrameSHA, sum, ft.TrailerOffset)
	}
	return nil
}

// readFromSpool 从未压缩的 spool 顺序读取第 row 行。
// C4：直接顺序读，不复制整文件。
//
// 【R10 第 1 项 · 残行守卫（规格 §8.2 第 4 步）】读到目标行时，若这一行在 EOF 前
// 仍带回了非空字节（即没有以 '\n' 结尾），必须**拒绝读取**，不得 trim 掉残字节后
// 当正文返回。判据与封块侧的 scanSpoolOffsets 同源（「EOF 时仍带回了非空字节」；
// 0 字节 spool 是合法状态，本分支不会被它触发）。
//
// 为什么这是真正的静默错正文路径：
//   - 残行占掉一个行位，其后的行整体错位 ⇒ row=N 读到第 N+1 行的正文；
//   - 更直接的一种：短写留下的残行可能只是真实正文的**前缀**，返回 trim 后的它
//     就等于静默返回被截断的正文。
//
// 【拒绝范围 = 被请求的那一行，不是整份 spool（与规格文字的差异，报告里已登记）】
// 生产者（Writer）在同一次进程生命周期内绝不会在残行之后继续追加：写失败会毒化该块
// 并换到新 part（writer.go 的 poisonCurLocked），重启后 nextSeqLocked 也一定开新
// part（目录内容即写指针）。因此残行**恒是文件的最后一条物理行**，它之前的完整行
// 与 row_seq 一一对应、内容可信。反过来，若把整份 spool 一律拒绝，则「活跃块」的
// 正常读取会被误伤：Writer 用 1 MiB 的 bufio，正文超过缓冲余量时一次 flush 会停在
// **行中间**，文件的最后一字节在两次 flush 之间天然不是 '\n'——那是正常写入的中间态，
// 不是事故。规格 §8.2 第 4 步的「拒绝读取」在这里落实为「拒绝把未终止的行当正文」。
func readFromSpool(spoolPath string, row int) ([]byte, error) {
	f, err := os.Open(spoolPath)
	if err != nil {
		return nil, fmt.Errorf("tracearchive: 打开 spool 失败: %w", err)
	}
	defer f.Close()

	br := bufio.NewReaderSize(f, 1<<20)
	for i := 0; ; i++ {
		line, readErr := br.ReadBytes('\n')
		if len(line) > 0 && i == row {
			// 残行守卫：ReadBytes 在 EOF 前找不到 '\n' 时会同时返回已读字节与 io.EOF。
			// 归入 ErrNotArchived（500 类「元数据与存储不一致」）而不是 404：
			// 这不是「正文写入时即失败」（那是 ErrBodyMissing），而是存储处于
			// 不可按行定位的状态，需要人工介入。
			if errors.Is(readErr, io.EOF) {
				return nil, fmt.Errorf("%w: spool %s 第 %d 行未以换行符结尾（%d 字节残行）；"+
					"残行占掉一个行位，其后按行定位会整体错位", ErrNotArchived, spoolPath, row, len(line))
			}
			if readErr != nil {
				return nil, fmt.Errorf("tracearchive: 读 spool 失败: %w", readErr)
			}
			// 与产物路径同一条约定：标记行 ⇒ ErrBodyMissing，不返回 "null"（规格 §8.2）。
			if isFailedRowMarker(line) {
				return nil, fmt.Errorf("%w: spool %s 第 %d 行", ErrBodyMissing, spoolPath, row)
			}
			return trimNewline(line), nil
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil, fmt.Errorf("%w: spool 中不存在第 %d 行", ErrNotArchived, row)
			}
			return nil, fmt.Errorf("tracearchive: 读 spool 失败: %w", readErr)
		}
	}
}

// isFailedRowMarker 判断一行是否是 Task 5 的 AppendFailed 留下的标记行
// （内容 "null"：该行正文从未落盘）。产物路径与 spool 路径共用它，
// 保证两条路径的判据只有一份（规格 §7.1/§8.2）。
//
// 只比较 trim 掉换行后的字节与 failedRowMarker：不解析 JSON、不做逐字节 JSON
// 等价（例如 "null " 或 "\u006eull"）——写入侧只可能产出这一种形态，
// 放宽判据只会把真实正文误报成缺失（例如正文里含 "null" 子串的合法 JSON）。
func isFailedRowMarker(line []byte) bool {
	return bytes.Equal(trimNewline(line), failedRowMarker)
}
