// partwriter.go —— 分块写入器：未压缩 spool 先落盘，满块后再流式压缩。
//
// 【内存约束设计（重要）】
// 契约 §3.3 要求 chunk 默认 256 MiB，但生产容器内存紧张。本实现因此**不把未压缩块
// 放在内存里**，而是分两段落盘：
//
//	① 写入阶段：每一整行经 io.Pipe 流式写入 store 上的 .spool/part-NNNNN.jsonl
//	   （未压缩 JSONL）。内存占用只有 io.Pipe 的固定缓冲（几十 KB），与块大小无关。
//	② 封块阶段：重新 Get 该 spool 文件，流式喂给 zstd 编码器，编码结果再经 io.Pipe
//	   流式 Put 成 part-NNNNN.jsonl.zst，最后删除 spool。
//
// 于是进程峰值内存只与「压缩器自身的窗口/并发」有关（约等于 ZstdWindowBytes ×
// encoder 内部缓冲），而与 chunk_mb 无关；chunk 调大到 512 MiB 也不会抬高 RSS。
//
// 其它约定：
//   - 只在一整行写完后判断是否封块（一行绝不跨块）；单行 >= chunk 时该行独立成块。
//   - 封块与 spool 的原子提交交给 ArchiveStore.Put（LocalStore 用临时文件 + fsync +
//     rename 实现，语义等价于契约 §4.1 的 .tmp → rename）。
//   - Close 只写 index.jsonl 与 manifest.json，**不写 _SUCCESS**（由调用方在校验通过后写）。
//   - Abort 清理 .spool 与本次产生的 .part/.tmp 残留，返回聚合错误。
package archive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

// 分块文件命名（契约 §3.1）。
const (
	spoolFilePattern = "part-%05d.jsonl"     // .spool/part-00000.jsonl（未压缩）
	partFilePattern  = "part-%05d.jsonl.zst" // part-00000.jsonl.zst（独立 zstd 帧）
)

// newlineBytes 复用的换行字节，供摘要串联与 JSONL 写入使用。
var newlineBytes = []byte("\n")

var (
	// errPartWriterClosed 写入器已 Close。
	errPartWriterClosed = errors.New("partwriter already closed")
	// errPartWriterAborted 写入器已 Abort。
	errPartWriterAborted = errors.New("partwriter aborted")
)

// PartWriterOptions 分块写入器的可调参数（契约 §4）。
type PartWriterOptions struct {
	SpoolDir           string // 未压缩暂存目录（store key），通常 {dt}/.spool；空则自动推导
	ChunkBytes         int64  // 未压缩块目标，默认 256<<20
	ZstdWindowBytes    int    // zstd 窗口，默认 128<<20（不可省）
	ZstdLevel          string // "best"
	EncoderConcurrency int    // 默认 1
}

// PartWriterResult Close 的返回值（契约 §4）。
type PartWriterResult struct {
	Parts             []IndexEntry // 与 index.jsonl 逐条一致
	RowCount          int64        // 写入总行数
	RIDsSHA256        string       // 按写入顺序以 "\n" 串联 rid 的 sha256（十六进制）
	UncompressedBytes int64        // 未压缩总字节
	CompressedBytes   int64        // 压缩后总字节
}

// PartWriter 分块写入器。非并发安全：一行一行顺序调用 WriteLine。
type PartWriter struct {
	ctx   context.Context
	store ArchiveStore
	dir   string // 天目录 store key，如 dt=2026-09-19
	spool string // 暂存目录 store key

	chunkBytes  int64
	windowBytes int
	level       zstd.EncoderLevel
	concurrency int

	// 当前块的 spool（懒打开；封块后置空，下一行再打开新的）。
	spoolKey   string
	spoolWrite *io.PipeWriter
	spoolDone  chan error

	// 已封块 / 累计统计。
	partIdx      int
	parts        []IndexEntry
	rows         int64
	uncompressed int64
	compressed   int64
	ridsSHA      hash.Hash

	// 当前块累加器。
	curRows         int64
	curUncompressed int64
	curFirstRID     string
	curLastRID      string

	spoolKeys []string // 本次打开过的全部 spool key（Abort 用）
	touched   []string // 本次 Put 过的 part key（Abort 清理 .part/.tmp 用）

	failed  error // 首次失败原因：失败后写入器「毒性化」，拒绝继续写
	closed  bool
	aborted bool
}

// fail 记录首次失败原因并返回该错误。
// 封块/落盘一旦失败，继续 WriteLine 会把「部分块」拼成看似完整的 part，
// 因此失败后一律拒绝后续写入（fail-closed），由调用方 Abort 清理后重导。
func (w *PartWriter) fail(err error) error {
	if err == nil {
		return nil
	}
	if w.failed == nil {
		w.failed = err
	}
	return err
}

// NewPartWriter 创建分块写入器。ctx 用于内部 spool/part 的读写（WriteLine 无 ctx 入参）。
func NewPartWriter(ctx context.Context, store ArchiveStore, dir string, opts PartWriterOptions) (*PartWriter, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if store == nil {
		return nil, newStoreError(CodeInvalidArgument, "partwriter", dir, fmt.Errorf("nil store"))
	}
	dir = strings.Trim(strings.TrimSpace(dir), "/")
	if dir == "" {
		return nil, newStoreError(CodeInvalidArgument, "partwriter", dir, fmt.Errorf("empty dir"))
	}

	chunk := opts.ChunkBytes
	if chunk <= 0 {
		chunk = DefaultChunkBytes
	}
	window := opts.ZstdWindowBytes
	if window <= 0 {
		window = DefaultZstdWindowBytes
	}
	if err := validateWindowBytes(window); err != nil {
		return nil, newStoreError(CodeInvalidArgument, "partwriter", dir, err)
	}
	conc := opts.EncoderConcurrency
	if conc <= 0 {
		conc = DefaultEncoderConcurrency
	}
	level, err := parseZstdLevel(opts.ZstdLevel)
	if err != nil {
		return nil, newStoreError(CodeInvalidArgument, "partwriter", dir, err)
	}
	spoolDir := strings.Trim(strings.TrimSpace(opts.SpoolDir), "/")
	if spoolDir == "" {
		spoolDir = joinKey(dir, SpoolDirName)
	}

	return &PartWriter{
		ctx:         ctx,
		store:       store,
		dir:         dir,
		spool:       spoolDir,
		chunkBytes:  chunk,
		windowBytes: window,
		level:       level,
		concurrency: conc,
		ridsSHA:     sha256.New(),
		parts:       make([]IndexEntry, 0, 8),
	}, nil
}

// WriteLine 追加一行 JSONL（不含换行，内部补 '\n'）。自动按整行封块。
func (w *PartWriter) WriteLine(line []byte) error {
	if w.aborted {
		return newStoreError(CodeInvalidArgument, "partwriter", w.dir, errPartWriterAborted)
	}
	if w.closed {
		return newStoreError(CodeInvalidArgument, "partwriter", w.dir, errPartWriterClosed)
	}
	if w.failed != nil {
		return w.failed
	}
	if err := w.ctx.Err(); err != nil {
		return err
	}
	if len(line) == 0 {
		return nil
	}
	rid, err := extractRequestID(line)
	if err != nil {
		return w.fail(newStoreError(CodeInvalidArgument, "partwriter", w.dir, err))
	}

	// ① 整行（含换行）流式写入未压缩 spool。
	if err := w.ensureSpool(); err != nil {
		return w.fail(err)
	}
	if err := writeAll(w.spoolWrite, line); err != nil {
		return w.fail(wrapStore(CodeRetryable, "partwriter", w.spoolKey, err))
	}
	if err := writeAll(w.spoolWrite, newlineBytes); err != nil {
		return w.fail(wrapStore(CodeRetryable, "partwriter", w.spoolKey, err))
	}

	// ② 整行写完后才更新计数与块内区间。
	if w.curRows == 0 {
		w.curFirstRID = rid
	}
	w.curLastRID = rid
	w.curRows++
	n := int64(len(line)) + 1
	w.curUncompressed += n
	w.rows++
	w.uncompressed += n
	// 契约 §5.1 的 string_agg 口径：以 "\n" 作为分隔符串联（**无**尾随 "\n"）。
	if w.rows > 1 {
		_, _ = w.ridsSHA.Write(newlineBytes)
	}
	_, _ = w.ridsSHA.Write([]byte(rid))

	// ③ 一整行写完后判断封块：单行 >= chunk 时该行独立成块，绝不跨行切块。
	if w.curUncompressed >= w.chunkBytes {
		return w.sealBlock()
	}
	return nil
}

// Close 封最后一块，写 index.jsonl 与 manifest.json。
// **不写 _SUCCESS**：由调用方在校验通过后写（契约 §4.1 第 8 步）。
func (w *PartWriter) Close(ctx context.Context) (PartWriterResult, error) {
	if w.aborted {
		return PartWriterResult{}, newStoreError(CodeInvalidArgument, "partwriter", w.dir, errPartWriterAborted)
	}
	if w.closed {
		return PartWriterResult{}, newStoreError(CodeInvalidArgument, "partwriter", w.dir, errPartWriterClosed)
	}
	if w.failed != nil {
		return PartWriterResult{}, w.failed
	}
	w.closed = true
	if ctx == nil {
		ctx = context.Background()
	}

	if w.curRows > 0 {
		if err := w.sealBlock(); err != nil {
			return PartWriterResult{}, err
		}
	}

	res := PartWriterResult{
		Parts:             w.parts,
		RowCount:          w.rows,
		RIDsSHA256:        hex.EncodeToString(w.ridsSHA.Sum(nil)),
		UncompressedBytes: w.uncompressed,
		CompressedBytes:   w.compressed,
	}
	if err := WriteIndex(ctx, w.store, w.dir, w.parts); err != nil {
		return PartWriterResult{}, err
	}
	mf := ManifestFile{
		Dir:               w.dir,
		Parts:             len(w.parts),
		RowCount:          w.rows,
		UncompressedBytes: w.uncompressed,
		CompressedBytes:   w.compressed,
		SHA256:            partsSHA256(w.parts),
		RIDsSHA256:        res.RIDsSHA256,
		CreatedAt:         time.Now().Format(time.RFC3339),
		Files:             w.parts,
	}
	if err := writeManifestFile(ctx, w.store, mf); err != nil {
		return PartWriterResult{}, err
	}
	return res, nil
}

// Abort 放弃本次写入：删除 .spool 与本次产生的 .part/.tmp 残留，返回聚合错误。
// Abort 幂等，且在 Close 失败后仍可调用（用 ctx 做删除，故不受导出 ctx 取消影响）。
//
// 注意（交接说明）：**不删除已封块的 part-*.jsonl.zst**（契约规定只清理 .spool 与 .tmp）。
// 这些孤儿块会在次轮重导时被同名覆盖（同一天数据冻结、导出顺序固定，块划分可复现）；
// 崩溃残留的 .spool 由 LocalStore.CleanupStaging 按 staging_retention_hours 清理。
// index.jsonl 只有在 Close 成功后才存在，故孤儿块不会被索引引用。
func (w *PartWriter) Abort(ctx context.Context) error {
	if w.aborted {
		return nil
	}
	w.aborted = true
	if ctx == nil {
		ctx = context.Background()
	}

	var errs []error

	// 1) 关闭未封块的 spool 流（失败也无所谓：对象随后会被删除）。
	if w.spoolWrite != nil {
		_ = w.spoolWrite.Close()
		<-w.spoolDone
		w.spoolWrite = nil
	}

	// 2) 删除全部 spool 对象；part key 只删临时残留（**.zst 本身保留**）。
	cands := make([]string, 0, len(w.spoolKeys)*3+len(w.touched)*2)
	for _, k := range w.spoolKeys {
		if k == "" {
			continue
		}
		cands = append(cands, k, k+".part", k+".tmp")
	}
	for _, k := range w.touched {
		if k == "" {
			continue
		}
		cands = append(cands, k+".part", k+".tmp")
	}
	seen := make(map[string]struct{}, len(cands))
	for _, cand := range cands {
		if _, dup := seen[cand]; dup {
			continue
		}
		seen[cand] = struct{}{}
		if err := w.store.Delete(ctx, cand); err != nil && !isNotFoundErr(err) {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("archive: partwriter abort %s: %w", w.dir, errors.Join(errs...))
	}
	return nil
}

// ensureSpool 懒打开当前块的未压缩暂存流（spool → store.Put 的流式管道）。
func (w *PartWriter) ensureSpool() error {
	if w.spoolWrite != nil {
		return nil
	}
	key := joinKey(w.spool, fmt.Sprintf(spoolFilePattern, w.partIdx))
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		_, err := w.store.Put(w.ctx, key, pr, -1, PutOptions{Overwrite: true})
		if err != nil {
			// Put 提前返回时解除写侧阻塞，让 WriteLine 拿到真实错误。
			_ = pr.CloseWithError(err)
		}
		done <- err
	}()
	w.spoolKey = key
	w.spoolWrite = pw
	w.spoolDone = done
	w.spoolKeys = append(w.spoolKeys, key)
	return nil
}

// sealBlock 封当前块：flush spool → 流式 zstd 压缩 → Put part → 删 spool → 记 index。
// 全程不在内存里保留整块未压缩数据。
func (w *PartWriter) sealBlock() error {
	if w.curRows == 0 {
		return nil
	}
	if w.spoolWrite == nil {
		return w.fail(newStoreError(CodeRetryable, "partwriter", w.dir, fmt.Errorf("seal without spool")))
	}
	// 1) 关闭 spool 写侧，等待未压缩暂存落盘。
	_ = w.spoolWrite.Close()
	if err := <-w.spoolDone; err != nil {
		w.spoolWrite = nil
		return w.fail(wrapStore(CodeRetryable, "partwriter", w.spoolKey, fmt.Errorf("spool flush: %w", err)))
	}
	w.spoolWrite = nil
	spoolKey := w.spoolKey

	partName := fmt.Sprintf(partFilePattern, w.partIdx)
	partKey := joinKey(w.dir, partName)
	w.touched = append(w.touched, partKey)

	// 2) 流式重读 spool → zstd → Put(part)。
	rc, err := w.store.Get(w.ctx, spoolKey, GetOptions{})
	if err != nil {
		return w.fail(wrapStore(CodeRetryable, "partwriter", spoolKey, err))
	}
	defer rc.Close()

	pr, pw := io.Pipe()
	type putOutcome struct {
		res PutResult
		err error
	}
	putDone := make(chan putOutcome, 1)
	go func() {
		res, perr := w.store.Put(w.ctx, partKey, pr, -1, PutOptions{Overwrite: true})
		if perr != nil {
			_ = pr.CloseWithError(perr)
		}
		putDone <- putOutcome{res: res, err: perr}
	}()

	zw, err := zstd.NewWriter(pw, buildEncoderOptions(w.level, w.windowBytes, w.concurrency)...)
	if err != nil {
		_ = pw.CloseWithError(err)
		<-putDone
		return w.fail(newStoreError(CodeInvalidArgument, "partwriter", partKey, err))
	}
	_, copyErr := io.Copy(zw, rc)
	closeErr := zw.Close()
	if copyErr == nil {
		copyErr = closeErr
	}
	// zstd 编码器的 Close 不会关闭底层 writer，必须显式关管道让 Put 看到 EOF。
	_ = pw.CloseWithError(copyErr)
	out := <-putDone
	if copyErr == nil {
		copyErr = out.err
	}
	if copyErr != nil {
		return w.fail(wrapStore(CodeRetryable, "partwriter", partKey, copyErr))
	}

	// 3) 记录索引条目并清理 spool（best-effort）。
	w.parts = append(w.parts, IndexEntry{
		Part:              partName,
		FirstRID:          w.curFirstRID,
		LastRID:           w.curLastRID,
		Rows:              w.curRows,
		UncompressedBytes: w.curUncompressed,
		CompressedBytes:   out.res.Size,
		SHA256:            out.res.SHA256,
	})
	w.compressed += out.res.Size
	w.partIdx++
	w.curRows = 0
	w.curUncompressed = 0
	w.curFirstRID = ""
	w.curLastRID = ""
	w.spoolKey = ""

	_ = w.store.Delete(w.ctx, spoolKey)
	return nil
}

// extractRequestID 从一行 JSONL 中取出 request_id。
// 用 json.Unmarshal 到最小结构：encoding/json 对未声明的字段只做 skip 扫描、不复制
// 正文，因此在导入侧它是正确且低分配的（不依赖字段出现位置，避免正文里出现
// "request_id" 字样时取错）。
func extractRequestID(line []byte) (string, error) {
	var probe struct {
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(line, &probe); err != nil {
		return "", fmt.Errorf("decode request_id: %w", err)
	}
	if probe.RequestID == "" {
		return "", fmt.Errorf("line has empty request_id")
	}
	return probe.RequestID, nil
}

// partsSHA256 计算「各 part 压缩后字节按块顺序串联」的 sha256，作为归档整体哈希
// （对应 manifest 的 sha256 列）。
func partsSHA256(parts []IndexEntry) string {
	h := sha256.New()
	for i := range parts {
		_, _ = h.Write([]byte(parts[i].SHA256))
		_, _ = h.Write(newlineBytes)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// writeAll 写满整段字节（io.Writer 允许短写）。
func writeAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if n > 0 {
			b = b[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
