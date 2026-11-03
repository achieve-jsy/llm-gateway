package archive

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

// archiveRecord 分块 JSONL 单行结构（最小回放载荷，见设计 §4）。
// request_body/response_body/request_headers 为 jsonb::text 反序列化后的原始 JSON，逐字保存。
type archiveRecord struct {
	RequestID      string          `json:"request_id"`
	TS             string          `json:"ts"`
	RequestHeaders json.RawMessage `json:"request_headers"`
	RequestBody    json.RawMessage `json:"request_body"`
	ResponseBody   json.RawMessage `json:"response_body"`
	ErrorCode      string          `json:"error_code"`
	ErrorMessage   string          `json:"error_message"`
}

// ArchiveObject 点读命中后返回的最小回放载荷（原始 JSON 字节，可直接回填 jsonb 字段）。
type ArchiveObject struct {
	RequestHeaders json.RawMessage `json:"request_headers,omitempty"`
	RequestBody    json.RawMessage `json:"request_body,omitempty"`
	ResponseBody   json.RawMessage `json:"response_body,omitempty"`
}

// readGate 点读并发闸（契约 §5.3/§7.3：信号量落在 LocalStore 层，解码前 acquire）。
type readGate interface {
	acquireRead(ctx context.Context) error
	releaseRead()
}

// ReadBackResult 回读校验结果（契约 §4）。
type ReadBackResult struct {
	RowCount   int64
	RIDsSHA256 string
}

// ReadBack 重新打开 dir 下的分块，按 index 顺序流式解压读盘，重算行数与 rid 摘要。
//
// 实现**真正解压读盘**，不复用写入侧的内存计数（契约 §3.2 步①：防漏行/截断/覆盖丢行）。
// 摘要口径与 SQL 冻结基准一致：`string_agg(request_id, E'\n' ORDER BY request_id COLLATE "C")`，
// 即**以 "\n" 作为分隔符串联、无尾随换行**。
func ReadBack(ctx context.Context, store ArchiveStore, dir string, parts []IndexEntry) (ReadBackResult, error) {
	if store == nil {
		return ReadBackResult{}, newStoreError(CodeInvalidArgument, "readback", dir, fmt.Errorf("nil store"))
	}
	if g, ok := store.(readGate); ok {
		if err := g.acquireRead(ctx); err != nil {
			return ReadBackResult{}, err
		}
		defer g.releaseRead()
	}

	window := zstdWindowOf(store)
	h := sha256.New()
	var n int64

	for _, e := range parts {
		if strings.TrimSpace(e.Part) == "" {
			return ReadBackResult{}, newStoreError(CodeInvalidArgument, "readback", dir, fmt.Errorf("empty part name in index"))
		}
		key := joinKey(dir, e.Part)
		pr, err := openPartReader(ctx, store, key, window)
		if err != nil {
			return ReadBackResult{}, err
		}
		for {
			if err := ctx.Err(); err != nil {
				pr.Close()
				return ReadBackResult{}, err
			}
			line, rerr := pr.readLine()
			if len(line) > 0 {
				var probe struct {
					RequestID string `json:"request_id"`
				}
				if jerr := json.Unmarshal(line, &probe); jerr != nil {
					pr.Close()
					return ReadBackResult{}, newStoreError(CodeInvalidArgument, "readback", key,
						fmt.Errorf("decode line %d: %w", n+1, jerr))
				}
				if n > 0 {
					_, _ = h.Write(newlineBytes)
				}
				_, _ = h.Write([]byte(probe.RequestID))
				n++
			}
			if rerr != nil {
				if rerr == io.EOF {
					break
				}
				pr.Close()
				return ReadBackResult{}, wrapStore(CodeRetryable, "readback", key, rerr)
			}
		}
		pr.Close()
	}

	return ReadBackResult{RowCount: n, RIDsSHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

// ReadBody 纯 Go 点读（契约 §4 / 设计 §5.2）：
//
//	① _SUCCESS 必须存在（缺标记 = 该天不可信，直接 NotFound）
//	② LoadIndex 读 index.jsonl
//	③ FindPart 按字节序二分定位目标块
//	④ Get(part) → zstd 流式解码 → **先用 bytes.Index 做字节预筛**，仅对候选行
//	   json.Unmarshal（不对整块全量反序列化）
//	⑤ 命中返回正文；未命中返回 CodeNotFound
//
// 并发闸在解码前 acquire（契约 §5.3：落在 LocalStore 层，handler 无法绕过）。
func (s *LocalStore) ReadBody(ctx context.Context, archiveRef string, requestID string) (*ArchiveObject, error) {
	if requestID == "" {
		return nil, newStoreError(CodeInvalidArgument, "readbody", archiveRef, fmt.Errorf("empty request_id"))
	}
	dir, err := normalizeArchiveRef(archiveRef)
	if err != nil {
		return nil, err
	}

	// 并发闸：从索引读取到解码全程受限，避免多路点读把 RSS 顶爆。
	if err := s.acquireRead(ctx); err != nil {
		return nil, err
	}
	defer s.releaseRead()

	// ① _SUCCESS 必须存在。
	ok, err := s.Exists(ctx, joinKey(dir, SuccessMarkerName))
	if err != nil {
		return nil, wrapStore(CodeRetryable, "readbody", dir, err)
	}
	if !ok {
		return nil, newStoreError(CodeNotFound, "readbody", dir,
			fmt.Errorf("day %s not archived (%s missing)", dir, SuccessMarkerName))
	}

	// ② 索引 → ③ 二分定位。
	parts, err := LoadIndex(ctx, s, dir)
	if err != nil {
		if isNotFoundErr(err) {
			return nil, newStoreError(CodeNotFound, "readbody", dir, fmt.Errorf("%s missing", IndexFileName))
		}
		return nil, err
	}
	idx := FindPart(parts, requestID)
	if idx < 0 {
		return nil, newStoreError(CodeNotFound, "readbody", dir,
			fmt.Errorf("request %q is beyond the last part (%d parts)", requestID, len(parts)))
	}
	partKey := joinKey(dir, parts[idx].Part)

	// ④ 快路径：字节预筛。
	needle := requestIDNeedle(requestID)
	obj, found, err := scanPartForNeedle(ctx, s, partKey, needle, requestID)
	if err != nil {
		return nil, err
	}
	if found {
		return obj, nil
	}

	// ⑤ 兜底慢路径：仅当预筛完全未命中时启用。若写入侧 JSON 转义策略与本地
	// json.Marshal 有差异（例如 SetEscapeHTML(false) + rid 含 <>&），预筛会漏，
	// 全量反序列化仍能命中，避免把「存在的数据」误报成丢失。
	obj, found, err = scanPartFull(ctx, s, partKey, requestID)
	if err != nil {
		return nil, err
	}
	if found {
		return obj, nil
	}
	return nil, newStoreError(CodeNotFound, "readbody", partKey,
		fmt.Errorf("request %q not found in %s", requestID, partKey))
}

// scanPartForNeedle 字节预筛：只有含 needle 的行才 json.Unmarshal（契约 §5.2）。
// needle 形如 `"request_id":"req-xxx"`，用 json.Marshal 生成，转义与写入侧 encoding/json 一致；
// 命中后仍用完整反序列化确认 request_id 相等（防 needle 出现在别的记录正文里）。
func scanPartForNeedle(ctx context.Context, store ArchiveStore, key string, needle []byte, requestID string) (*ArchiveObject, bool, error) {
	pr, err := openPartReader(ctx, store, key, zstdWindowOf(store))
	if err != nil {
		return nil, false, err
	}
	defer pr.Close()

	for {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		line, rerr := pr.readLine()
		if len(line) > 0 && bytes.Contains(line, needle) {
			var rec archiveRecord
			if jerr := json.Unmarshal(line, &rec); jerr == nil && rec.RequestID == requestID {
				return &ArchiveObject{
					RequestHeaders: rec.RequestHeaders,
					RequestBody:    rec.RequestBody,
					ResponseBody:   rec.ResponseBody,
				}, true, nil
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				return nil, false, nil
			}
			return nil, false, wrapStore(CodeRetryable, "readbody", key, rerr)
		}
	}
}

// scanPartFull 兜底全量扫描：每行先只解析 request_id（encoding/json 对未声明字段只 skip，
// 不复制正文），确认相等后才完整反序列化该行。
func scanPartFull(ctx context.Context, store ArchiveStore, key string, requestID string) (*ArchiveObject, bool, error) {
	pr, err := openPartReader(ctx, store, key, zstdWindowOf(store))
	if err != nil {
		return nil, false, err
	}
	defer pr.Close()

	for {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		line, rerr := pr.readLine()
		if len(line) > 0 {
			var probe struct {
				RequestID string `json:"request_id"`
			}
			if jerr := json.Unmarshal(line, &probe); jerr == nil && probe.RequestID == requestID {
				var rec archiveRecord
				if jerr := json.Unmarshal(line, &rec); jerr == nil {
					return &ArchiveObject{
						RequestHeaders: rec.RequestHeaders,
						RequestBody:    rec.RequestBody,
						ResponseBody:   rec.ResponseBody,
					}, true, nil
				}
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				return nil, false, nil
			}
			return nil, false, wrapStore(CodeRetryable, "readbody", key, rerr)
		}
	}
}

// requestIDNeedle 构造字节预筛 needle：`"request_id":` + json.Marshal(rid)。
// 用 json.Marshal（而不是手工拼引号）保证与写入侧 encoding/json 的字符串转义逐字节一致。
func requestIDNeedle(rid string) []byte {
	quoted, err := json.Marshal(rid)
	if err != nil {
		quoted = []byte(`"` + rid + `"`)
	}
	needle := make([]byte, 0, len(`"request_id":`)+len(quoted))
	needle = append(needle, `"request_id":`...)
	return append(needle, quoted...)
}

// partReader 是一个 part 的「文件 → zstd → 按行」读取器。
type partReader struct {
	rc  io.ReadCloser
	dec *zstd.Decoder
	br  *bufio.Reader
}

// openPartReader 打开 part 并建立 zstd 解码流。
//
// 注意：这里按行读取会把**单行**放进内存（行本身可能很大），但绝不把整块未压缩数据
// 一次性读入内存——与 partwriter 的 spool 设计配套。
func openPartReader(ctx context.Context, store ArchiveStore, key string, windowBytes int) (*partReader, error) {
	rc, err := store.Get(ctx, key, GetOptions{})
	if err != nil {
		return nil, err
	}
	dec, err := zstd.NewReader(rc, buildDecoderOptions(windowBytes)...)
	if err != nil {
		_ = rc.Close()
		return nil, newStoreError(CodeInvalidArgument, "readbody", key, err)
	}
	return &partReader{rc: rc, dec: dec, br: bufio.NewReaderSize(dec, 256*1024)}, nil
}

// readLine 读一行并去掉行尾 CR/LF。
// 返回的 error 可能是 io.EOF 且同时带回了最后一行（无尾随换行），调用方需先处理 line。
func (p *partReader) readLine() ([]byte, error) {
	line, err := p.br.ReadBytes('\n')
	return bytes.TrimRight(line, "\r\n"), err
}

// Close 释放解码器与底层文件句柄。
func (p *partReader) Close() {
	if p.dec != nil {
		p.dec.Close()
		p.dec = nil
	}
	if p.rc != nil {
		_ = p.rc.Close()
		p.rc = nil
	}
}

// parseArchiveRefDate 从 archiveRef 提取日期（YYYY-MM-DD）。
// 接受外形：dt=2026-06-04/、2026-06-04/、call_traces/dt=2026-06-04/。
func parseArchiveRefDate(ref string) (string, error) {
	last := strings.Trim(strings.TrimSpace(ref), "/")
	if i := strings.LastIndex(last, "/"); i >= 0 {
		last = last[i+1:]
	}
	if i := strings.Index(last, "="); i >= 0 {
		last = last[i+1:]
	}
	if _, err := time.Parse("2006-01-02", last); err != nil {
		return "", newStoreError(CodeInvalidArgument, "readbody", ref, fmt.Errorf("invalid archive ref date %q", ref))
	}
	return last, nil
}

// normalizeArchiveRef 把 archive_ref（契约 §3.1：`dt=2026-09-19/`）规范成天目录 store key。
// 保留调用方给出的中间前缀，只校验末段日期可解析。
func normalizeArchiveRef(ref string) (string, error) {
	dir := strings.Trim(strings.TrimSpace(ref), "/")
	if dir == "" {
		return "", newStoreError(CodeInvalidArgument, "readbody", ref, fmt.Errorf("empty archive ref"))
	}
	if _, err := parseArchiveRefDate(dir); err != nil {
		return "", err
	}
	return dir, nil
}
