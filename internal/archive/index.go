package archive

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// 存储格式常量（契约 docs/trace-archive-v3-implementation-contract.md §3.1）。
//
// 天目录（相对 archive.root 的 store key，形如 dt=2026-09-19/）布局：
//
//	.spool/part-00000.jsonl   导出暂存（未压缩），封块后删除
//	part-00000.jsonl.zst      每块 = 独立 zstd 帧
//	index.jsonl               每块一行（未压缩）
//	manifest.json             汇总（行数/字节/sha256/块数）
//	_SUCCESS                  最后写入；导出开始前必须先删
const (
	// IndexFileName 索引文件名：每块一行 JSON（未压缩）。
	IndexFileName = "index.jsonl"
	// ManifestFileName 汇总文件名。
	ManifestFileName = "manifest.json"
	// SuccessMarkerName 该天「完整 + 通过四步校验」标记，由调用方最后写入。
	SuccessMarkerName = "_SUCCESS"
	// SpoolDirName 未压缩暂存目录名（相对天目录）。
	SpoolDirName = ".spool"

	// DefaultChunkBytes 未压缩块目标大小，默认 256 MiB（契约 archive.chunk_mb）。
	DefaultChunkBytes int64 = 256 << 20
	// DefaultZstdWindowBytes zstd 窗口大小，默认 128 MiB。
	// 【不可省】漏配 WithWindowSize 会让压缩率静默塌 4.3 倍（契约 §3.3）。
	DefaultZstdWindowBytes = 128 << 20
	// DefaultEncoderConcurrency 编码器并发，默认 1（契约 archive.encoder_concurrency）。
	DefaultEncoderConcurrency = 1
	// DefaultMaxConcurrentReads 点读并发闸默认值（契约 archive.max_concurrent_reads）。
	DefaultMaxConcurrentReads = 4
)

// IndexEntry 是 index.jsonl 的单行结构（契约 §3.2）。
// Go 字段名与 JSON 字段名必须与契约逐字一致，WS-B/WS-D/WS-E 共用。
//
// 不变量（WS-B 与 WS-C 共同保证）：
//   - 块内记录按 request_id 字节序（PostgreSQL COLLATE "C"）连续升序
//   - FirstRID <= LastRID，且 index[i].LastRID < index[i+1].FirstRID
//   - 一行绝不跨块
type IndexEntry struct {
	Part              string `json:"part"`               // "part-00000.jsonl.zst"
	FirstRID          string `json:"first_rid"`          // 块内第一条 request_id
	LastRID           string `json:"last_rid"`           // 块内最后一条 request_id
	Rows              int64  `json:"rows"`               // 块内行数
	UncompressedBytes int64  `json:"uncompressed_bytes"` // 块内未压缩字节
	CompressedBytes   int64  `json:"compressed_bytes"`   // 该 part 压缩后字节
	SHA256            string `json:"sha256"`             // 该 part 压缩后字节的 sha256（十六进制）
}

// ManifestFile 是 {天目录}/manifest.json 的结构（契约 §3.1：汇总 行数/字节/sha256/块数）。
// 它是**文件系统侧**的汇总副本；权威状态仍在 trace_archive_manifests 表。
type ManifestFile struct {
	Dir               string       `json:"dir"`                // 天目录 key，如 dt=2026-09-19
	Parts             int          `json:"parts"`              // 块数
	RowCount          int64        `json:"row_count"`          // 总行数
	UncompressedBytes int64        `json:"uncompressed_bytes"` // 未压缩总字节
	CompressedBytes   int64        `json:"compressed_bytes"`   // 压缩后总字节
	SHA256            string       `json:"sha256"`             // 各 part 压缩后字节按块顺序串联的 sha256
	RIDsSHA256        string       `json:"rids_sha256"`        // 按导出顺序以 "\n" 串联全部 rid 的 sha256
	CreatedAt         string       `json:"created_at"`         // RFC3339（本地写盘时间）
	Files             []IndexEntry `json:"files"`              // 与 index.jsonl 逐条一致
}

// joinKey 拼接天目录（store key）与文件名/子目录名，容忍两侧多余的 "/"。
func joinKey(dir, name string) string {
	dir = strings.Trim(strings.TrimSpace(dir), "/")
	if dir == "" {
		return name
	}
	if name == "" {
		return dir
	}
	return dir + "/" + strings.Trim(name, "/")
}

// isNotFoundErr 判断错误是否为存储层 CodeNotFound。
func isNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	var se *StoreError
	return errors.As(err, &se) && se.Code == CodeNotFound
}

// WriteIndex 写入 {dir}/index.jsonl（每块一行 JSON，字段名见 IndexEntry）。
// 索引体积很小（契约 §3.1 约 10 KB），整文件覆盖写。
func WriteIndex(ctx context.Context, store ArchiveStore, dir string, parts []IndexEntry) error {
	if store == nil {
		return newStoreError(CodeInvalidArgument, "writeindex", dir, fmt.Errorf("nil store"))
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for i := range parts {
		// json.Encoder 每个对象自带 '\n'，且转义策略与 json.Marshal 一致。
		if err := enc.Encode(parts[i]); err != nil {
			return newStoreError(CodeInvalidArgument, "writeindex", joinKey(dir, IndexFileName), err)
		}
	}
	key := joinKey(dir, IndexFileName)
	if _, err := store.Put(ctx, key, bytes.NewReader(buf.Bytes()), int64(buf.Len()), PutOptions{Overwrite: true}); err != nil {
		return wrapStore(CodeRetryable, "writeindex", key, err)
	}
	return nil
}

// LoadIndex 读取 {dir}/index.jsonl。索引不存在时返回 CodeNotFound。
func LoadIndex(ctx context.Context, store ArchiveStore, dir string) ([]IndexEntry, error) {
	if store == nil {
		return nil, newStoreError(CodeInvalidArgument, "loadindex", dir, fmt.Errorf("nil store"))
	}
	key := joinKey(dir, IndexFileName)
	rc, err := store.Get(ctx, key, GetOptions{})
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	br := bufio.NewReaderSize(rc, 64*1024)
	parts := make([]IndexEntry, 0, 16)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line, rerr := br.ReadBytes('\n')
		line = bytes.TrimSpace(line)
		if len(line) > 0 {
			var e IndexEntry
			if jerr := json.Unmarshal(line, &e); jerr != nil {
				return nil, newStoreError(CodeInvalidArgument, "loadindex", key,
					fmt.Errorf("decode index line: %w", jerr))
			}
			parts = append(parts, e)
		}
		if rerr != nil {
			if rerr == io.EOF {
				break
			}
			return nil, wrapStore(CodeRetryable, "loadindex", key, rerr)
		}
	}
	return parts, nil
}

// writeManifestFile 写入 {dir}/manifest.json（整文件覆盖写）。
func writeManifestFile(ctx context.Context, store ArchiveStore, mf ManifestFile) error {
	b, err := json.Marshal(mf)
	if err != nil {
		return newStoreError(CodeInvalidArgument, "writemanifest", mf.Dir, err)
	}
	b = append(b, '\n')
	key := joinKey(mf.Dir, ManifestFileName)
	if _, err := store.Put(ctx, key, bytes.NewReader(b), int64(len(b)), PutOptions{Overwrite: true}); err != nil {
		return wrapStore(CodeRetryable, "writemanifest", key, err)
	}
	return nil
}

// LoadManifest 读取 {dir}/manifest.json（供巡检/校验④消费）。
func LoadManifest(ctx context.Context, store ArchiveStore, dir string) (ManifestFile, error) {
	key := joinKey(dir, ManifestFileName)
	rc, err := store.Get(ctx, key, GetOptions{})
	if err != nil {
		return ManifestFile{}, err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, 8<<20))
	if err != nil {
		return ManifestFile{}, wrapStore(CodeRetryable, "loadmanifest", key, err)
	}
	var mf ManifestFile
	if err := json.Unmarshal(bytes.TrimSpace(b), &mf); err != nil {
		return ManifestFile{}, newStoreError(CodeInvalidArgument, "loadmanifest", key, err)
	}
	return mf, nil
}

// FindPart 二分定位：返回第一个满足 LastRID >= rid 的条目下标；找不到返回 -1。
//
// 比较一律使用字节序（bytes.Compare），与导出侧 `ORDER BY request_id COLLATE "C"` 对齐
// （契约 §4.3：request_id 是客户端可控任意文本，不能依赖 DB 默认 collation）。
func FindPart(parts []IndexEntry, rid string) int {
	needle := []byte(rid)
	idx := sort.Search(len(parts), func(i int) bool {
		return bytes.Compare([]byte(parts[i].LastRID), needle) >= 0
	})
	if idx >= len(parts) {
		return -1
	}
	return idx
}

// —— codec 参数（契约 §3.3 硬约束）——

// parseZstdLevel 把配置字符串映射为 zstd 编码级别（archive.zstd_level，默认 "best"）。
func parseZstdLevel(name string) (zstd.EncoderLevel, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return zstd.SpeedBestCompression, nil
	}
	if ok, lvl := zstd.EncoderLevelFromString(name); ok {
		return lvl, nil
	}
	return zstd.SpeedDefault, fmt.Errorf("archive: unknown zstd level %q", name)
}

// validateWindowBytes 校验窗口大小（必须是 2 的幂且在 zstd 允许区间内）。
func validateWindowBytes(n int) error {
	if n < zstd.MinWindowSize || n > zstd.MaxWindowSize {
		return fmt.Errorf("archive: zstd window size %d out of range [%d, %d]", n, zstd.MinWindowSize, zstd.MaxWindowSize)
	}
	if n&(n-1) != 0 {
		return fmt.Errorf("archive: zstd window size %d must be a power of 2", n)
	}
	return nil
}

// buildEncoderOptions 组装写入侧编码参数。
// WithWindowSize 是硬约束：漏配会让压缩率静默塌 4.3 倍（契约 §3.3）。
func buildEncoderOptions(level zstd.EncoderLevel, windowBytes, concurrency int) []zstd.EOption {
	opts := []zstd.EOption{
		zstd.WithEncoderLevel(level),
		zstd.WithWindowSize(windowBytes),
	}
	if concurrency > 0 {
		opts = append(opts, zstd.WithEncoderConcurrency(concurrency))
	}
	return opts
}

// buildDecoderOptions 组装读取侧解码参数（契约 §3.3：WithDecoderMaxWindow）。
// 窗口 <=0 时回退默认值；非法（过小）时不加限制，避免点读直接失败。
func buildDecoderOptions(windowBytes int) []zstd.DOption {
	if windowBytes < zstd.MinWindowSize {
		return nil
	}
	return []zstd.DOption{zstd.WithDecoderMaxWindow(uint64(windowBytes))}
}

// zstdWindowReporter 由能报告解码窗口上限的 store 实现（LocalStore）。
// 用可选接口而不是扩大 ArchiveStore，避免影响其它工作流的实现。
type zstdWindowReporter interface{ ZstdWindowBytes() int }

// zstdWindowOf 取 store 上配置的解码窗口上限，未实现则用默认值。
func zstdWindowOf(store ArchiveStore) int {
	if r, ok := store.(zstdWindowReporter); ok {
		if n := r.ZstdWindowBytes(); n > 0 {
			return n
		}
	}
	return DefaultZstdWindowBytes
}
