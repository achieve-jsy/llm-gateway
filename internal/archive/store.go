// Package archive 实现「调用链路记录归档存储」的存储层。
//
// 本包是 docs/trace-archive-final-design.md（①c 最简版）的存储层实现：
//   - 正文按天 jsonl.gz 落到本地存档盘，不做 Parquet 列存、不用 DuckDB / parquet-go；
//   - 点读用纯 Go（compress/gzip + 逐行 json 扫描），无 cgo / 无对象存储 / 无 S3；
//   - key 一律为相对路径（如 dt=2026-06-04/body-2026-06-04.jsonl.gz），与 DB 中
//     archive_ref / files[].name 一致，换盘/换挂载不回写 DB。
package archive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

// ErrorCode 归档存储失败分类码，供业务层通过 errors.As 做确定性分支。
type ErrorCode int

const (
	// CodeRetryable 可重试的临时故障（磁盘 IO 抖动、写一半中断等）。
	CodeRetryable ErrorCode = iota + 1
	// CodeNotFound 对象不存在。
	CodeNotFound
	// CodeAlreadyExists 对象已存在（仅 Overwrite=false 时返回）。
	CodeAlreadyExists
	// CodePermissionDenied 权限/路径非法（无写权限、键逃逸等）。
	CodePermissionDenied
	// CodeInvalidArgument 入参非法（空 key、无法解析的 archive ref 等）。
	CodeInvalidArgument
)

// String 返回稳定的错误码字符串，便于日志与错误模型输出。
func (c ErrorCode) String() string {
	switch c {
	case CodeRetryable:
		return "Retryable"
	case CodeNotFound:
		return "NotFound"
	case CodeAlreadyExists:
		return "AlreadyExists"
	case CodePermissionDenied:
		return "PermissionDenied"
	case CodeInvalidArgument:
		return "InvalidArgument"
	default:
		return "Unknown"
	}
}

// StoreError 归档存储统一错误类型。
// 通过 errors.As(err, &se) 拿到分类码，避免业务层依赖字符串匹配。
type StoreError struct {
	Code ErrorCode // 分类码
	Op   string    // 操作名，如 put/get/head/exists/list/delete/readbody
	Key  string    // 关联对象 key（相对路径）
	Err  error     // 底层错误（可为空）
}

// Error 实现 error 接口。
func (e *StoreError) Error() string {
	msg := fmt.Sprintf("archive: %s %s %q", e.Code, e.Op, e.Key)
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Unwrap 支持 errors.Unwrap / errors.As 透出底层错误。
func (e *StoreError) Unwrap() error { return e.Err }

// newStoreError 构造一个 StoreError。
func newStoreError(code ErrorCode, op, key string, err error) *StoreError {
	return &StoreError{Code: code, Op: op, Key: key, Err: err}
}

// wrapStore 包装底层错误为 StoreError；若底层已经是 StoreError 则原样保留分类。
func wrapStore(code ErrorCode, op, key string, err error) error {
	if err == nil {
		return nil
	}
	var se *StoreError
	if errors.As(err, &se) {
		return err
	}
	return newStoreError(code, op, key, err)
}

// PutOptions Put 的扩展选项。
type PutOptions struct {
	// Overwrite 为 true 时允许覆盖已存在的 key（幂等重跑）；默认 false，已存在则返回 CodeAlreadyExists。
	Overwrite bool
	// ContentType 预留的 MIME 标记，本地实现暂不落盘。
	ContentType string
	// Metadata 预留的对象元数据，本地实现暂不落盘。
	Metadata map[string]string
}

// GetOptions Get 的扩展选项（当前预留）。
type GetOptions struct{}

// PutResult Put 成功后的结果。
type PutResult struct {
	Size   int64  // 实际写入字节数
	SHA256 string // 内容哈希（十六进制），供 manifest 双写
}

// ObjectInfo 对象元数据（Head / List 返回）。
type ObjectInfo struct {
	Key         string    // 相对路径
	Size        int64     // 字节数
	ModTime     time.Time // 最后修改时间
	ContentType string    // 预留，本地实现为空
}

// ListOptions List 的扩展选项。
type ListOptions struct {
	MaxKeys int    // 单页最大返回数，<=0 用默认（1000）
	Marker  string // 游标：只返回严格大于该 key 的对象（按相对路径字典序）
}

// ListResult List 的结果，支持分页。
type ListResult struct {
	Objects     []ObjectInfo
	IsTruncated bool   // 是否还有更多
	NextMarker  string // 当 IsTruncated 时，下一次调用的 marker
}

// CleanupResult CleanupStaging 的结果。
type CleanupResult struct {
	Removed int   // 清理掉的残留文件数
	Bytes   int64 // 释放的字节数
	Errors  []error
}

// ArchiveStore 归档存储统一接口。实现=本地文件系统（见 LocalStore）。
//
// 语义约定：
//   - key 为相对路径（相对 archive.root），禁止绝对路径或 ../ 逃逸；
//   - Put 语义为原子提交（临时文件 + fsync + rename），Overwrite=false 且已存在时返回 CodeAlreadyExists；
//   - Head/Delete 对不存在对象返回 CodeNotFound；Exists 返回 (false, nil)；
//   - List(prefix) 返回 prefix 下所有对象，含分页。
type ArchiveStore interface {
	// Put 写入对象（原子提交）。
	Put(ctx context.Context, key string, r io.Reader, size int64, opts PutOptions) (PutResult, error)
	// Get 打开对象用于流式读取，调用方负责 Close 返回的 ReadCloser。
	Get(ctx context.Context, key string, opts GetOptions) (io.ReadCloser, error)
	// Head 返回对象元数据。
	Head(ctx context.Context, key string) (ObjectInfo, error)
	// Exists 判断对象是否存在。
	Exists(ctx context.Context, key string) (bool, error)
	// List 列出指定前缀下的对象。
	List(ctx context.Context, prefix string, opts ListOptions) (ListResult, error)
	// Delete 删除对象。
	Delete(ctx context.Context, key string) error
}
