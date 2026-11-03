package archive

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// archiveRecord jsonl.gz 单行 JSON 结构（最小回放载荷，见设计 §4）。
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

// ReadBody 纯 Go 点读：从 archiveRef（形如 dt=2026-06-04/）定位当天
// body-YYYY-MM-DD.jsonl.gz，gzip 流式解压并逐行扫描，命中 request_id 即返回正文。
//
// 未命中返回 CodeNotFound；archiveRef 无法解析返回 CodeInvalidArgument。
// 实现只用 compress/gzip 标准库，无 cgo / 无 DuckDB / 无 parquet-go。
func (s *LocalStore) ReadBody(ctx context.Context, archiveRef string, requestID string) (*ArchiveObject, error) {
	if requestID == "" {
		return nil, newStoreError(CodeInvalidArgument, "readbody", archiveRef, fmt.Errorf("empty request_id"))
	}
	date, err := parseArchiveRefDate(archiveRef)
	if err != nil {
		return nil, err
	}
	// 目录前缀 + body-{date}.jsonl.gz 拼出相对 key。
	key := fmt.Sprintf("%s/body-%s.jsonl.gz", strings.TrimRight(archiveRef, "/"), date)

	rc, err := s.Get(ctx, key, GetOptions{})
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	gz, err := gzip.NewReader(rc)
	if err != nil {
		return nil, wrapStore(CodeRetryable, "readbody", key, err)
	}
	defer gz.Close()

	br := bufio.NewReaderSize(gz, 64*1024)
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		line, rerr := br.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var rec archiveRecord
			if jerr := json.Unmarshal(line, &rec); jerr == nil && rec.RequestID == requestID {
				return &ArchiveObject{
					RequestHeaders: rec.RequestHeaders,
					RequestBody:    rec.RequestBody,
					ResponseBody:   rec.ResponseBody,
				}, nil
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				break
			}
			return nil, wrapStore(CodeRetryable, "readbody", key, rerr)
		}
	}
	return nil, newStoreError(CodeNotFound, "readbody", key, fmt.Errorf("request %q not found in %s", requestID, key))
}

// parseArchiveRefDate 从 archiveRef 提取日期（YYYY-MM-DD）。
// 接受外形：dt=2026-06-04/、2026-06-04/、dt=2026-06-04。
func parseArchiveRefDate(ref string) (string, error) {
	ref = strings.TrimSpace(strings.TrimRight(ref, "/"))
	if idx := strings.Index(ref, "="); idx >= 0 {
		ref = ref[idx+1:]
	}
	if _, err := time.Parse("2006-01-02", ref); err != nil {
		return "", newStoreError(CodeInvalidArgument, "readbody", ref, fmt.Errorf("invalid archive ref date %q", ref))
	}
	return ref, nil
}
