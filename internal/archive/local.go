package archive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

// 目录布局（相对 archive.root，v3 分块格式，见契约 §3.1）：
//
//	{root}/dt=YYYY-MM-DD/.spool/part-00000.jsonl   # 未压缩导出暂存，封块后删除
//	{root}/dt=YYYY-MM-DD/part-00000.jsonl.zst      # 每块 = 独立 zstd 帧
//	{root}/dt=YYYY-MM-DD/index.jsonl               # 每块一行（未压缩）
//	{root}/dt=YYYY-MM-DD/manifest.json             # 汇总（行数/字节/sha256/块数）
//	{root}/dt=YYYY-MM-DD/_SUCCESS                  # 0 字节，该天「完整 + 校验通过」标记
//	{StagingDir}/...                               # 暂存/残留（默认 root/_tmp，按 mtime 清理）
//
// key 一律为相对路径（如 dt=2026-09-19/part-00000.jsonl.zst），与 DB 中 archive_ref /
// files[].name 一致，换盘/换挂载不回写 DB。
type LocalStore struct {
	root       string // 归档根目录
	stagingDir string // 暂存目录（残留清理用）

	// sem 点读并发闸（契约 §5.3/§7.3）：nil 表示不限制。
	sem chan struct{}
	// zstdWindow 解码窗口上限（字节），与写入侧 archive.zstd_window_mb 保持一致。
	zstdWindow int
}

// NewLocalStore 创建本地文件系统实现。空值回退到设计默认。
// 点读并发闸默认 DefaultMaxConcurrentReads；由装配层用 SetMaxConcurrentReads 覆盖。
func NewLocalStore(root, stagingDir string) *LocalStore {
	if root == "" {
		root = "/var/lib/llm-gateway/archive"
	}
	if stagingDir == "" {
		stagingDir = filepath.Join(root, "_tmp")
	}
	return &LocalStore{
		root:       root,
		stagingDir: stagingDir,
		sem:        make(chan struct{}, DefaultMaxConcurrentReads),
		zstdWindow: DefaultZstdWindowBytes,
	}
}

// SetMaxConcurrentReads 设置点读并发上限（archive.max_concurrent_reads）。<=0 表示不限制。
func (s *LocalStore) SetMaxConcurrentReads(n int) {
	if n <= 0 {
		s.sem = nil
		return
	}
	s.sem = make(chan struct{}, n)
}

// MaxConcurrentReads 返回当前点读并发上限；0 表示不限制。
func (s *LocalStore) MaxConcurrentReads() int {
	if s.sem == nil {
		return 0
	}
	return cap(s.sem)
}

// SetZstdWindowBytes 设置解码窗口上限（archive.zstd_window_mb<<20），
// 必须与写入侧一致，否则大窗口帧会解码失败。<=0 回退默认值。
func (s *LocalStore) SetZstdWindowBytes(n int) {
	if n <= 0 || n < zstd.MinWindowSize {
		s.zstdWindow = DefaultZstdWindowBytes
		return
	}
	s.zstdWindow = n
}

// ZstdWindowBytes 返回解码窗口上限（契约 §3.3：WithDecoderMaxWindow）。
func (s *LocalStore) ZstdWindowBytes() int { return s.zstdWindow }

// acquireRead 获取点读许可；ctx 取消时立即返回，避免排队点读把请求挂死。
func (s *LocalStore) acquireRead(ctx context.Context) error {
	if s.sem == nil {
		return ctx.Err()
	}
	select {
	case s.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// releaseRead 释放点读许可。
func (s *LocalStore) releaseRead() {
	if s.sem == nil {
		return
	}
	select {
	case <-s.sem:
	default:
	}
}

// Root 返回归档根目录。
func (s *LocalStore) Root() string { return s.root }

// StagingDir 返回暂存目录。
func (s *LocalStore) StagingDir() string { return s.stagingDir }

// keyPath 将相对 key 解析为文件系统路径，并做路径逃逸防护。
// key 空、绝对路径或含 .. 片段视为非法入参（CodeInvalidArgument）。
func (s *LocalStore) keyPath(key string) (string, error) {
	if key == "" {
		return "", newStoreError(CodeInvalidArgument, "key", key, fmt.Errorf("empty key"))
	}
	clean := filepath.Clean(key)
	if filepath.IsAbs(clean) {
		return "", newStoreError(CodeInvalidArgument, "key", key, fmt.Errorf("absolute key not allowed"))
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", newStoreError(CodeInvalidArgument, "key", key, fmt.Errorf("path escapes root"))
	}
	return filepath.Join(s.root, clean), nil
}

// Put 写对象：同目录临时文件 + fsync + rename 原子提交。
// Overwrite=false 且对象已存在时返回 CodeAlreadyExists。
func (s *LocalStore) Put(ctx context.Context, key string, r io.Reader, size int64, opts PutOptions) (PutResult, error) {
	if err := ctx.Err(); err != nil {
		return PutResult{}, err
	}
	target, err := s.keyPath(key)
	if err != nil {
		return PutResult{}, err
	}
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		// 目录权限不足属于权限类；其余视为可重试。
		if os.IsPermission(err) {
			return PutResult{}, wrapStore(CodePermissionDenied, "put", key, err)
		}
		return PutResult{}, wrapStore(CodeRetryable, "put", key, err)
	}

	// 已存在短路：默认拒绝覆盖，幂等重跑走 Overwrite=true + Exists 短路。
	if !opts.Overwrite {
		if _, err := os.Stat(target); err == nil {
			return PutResult{}, newStoreError(CodeAlreadyExists, "put", key, fmt.Errorf("object already exists"))
		} else if !os.IsNotExist(err) {
			return PutResult{}, wrapStore(CodePermissionDenied, "put", key, err)
		}
	}

	// 临时文件与目标同目录（同文件系统），保证 rename 原子。
	tmp := target + ".part"
	_ = os.Remove(tmp)
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return PutResult{}, wrapStore(CodePermissionDenied, "put", key, err)
	}

	// 流式写入 + 同步算 SHA256。
	hasher := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, hasher), r)
	if err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return PutResult{}, wrapStore(CodeRetryable, "put", key, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return PutResult{}, wrapStore(CodeRetryable, "put", key, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return PutResult{}, wrapStore(CodeRetryable, "put", key, err)
	}
	// rename 原子提交。
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return PutResult{}, wrapStore(CodeRetryable, "put", key, err)
	}
	return PutResult{Size: n, SHA256: hex.EncodeToString(hasher.Sum(nil))}, nil
}

// Get 打开对象用于流式读取；调用方负责 Close。
func (s *LocalStore) Get(ctx context.Context, key string, _ GetOptions) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	target, err := s.keyPath(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(target)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, newStoreError(CodeNotFound, "get", key, err)
		}
		return nil, wrapStore(CodePermissionDenied, "get", key, err)
	}
	return f, nil
}

// Head 返回对象元数据（size / mtime）。
func (s *LocalStore) Head(ctx context.Context, key string) (ObjectInfo, error) {
	if err := ctx.Err(); err != nil {
		return ObjectInfo{}, err
	}
	target, err := s.keyPath(key)
	if err != nil {
		return ObjectInfo{}, err
	}
	st, err := os.Stat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return ObjectInfo{}, newStoreError(CodeNotFound, "head", key, err)
		}
		return ObjectInfo{}, wrapStore(CodePermissionDenied, "head", key, err)
	}
	return ObjectInfo{Key: key, Size: st.Size(), ModTime: st.ModTime()}, nil
}

// Exists 判断对象是否存在。
func (s *LocalStore) Exists(ctx context.Context, key string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	target, err := s.keyPath(key)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(target)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, wrapStore(CodePermissionDenied, "exists", key, err)
}

// List 列出 prefix 下的对象（相对路径，按 key 字典序，支持分页）。
// 跳过暂存目录子树与 .part/.tmp 残留，避免把中间产物当作真实对象。
func (s *LocalStore) List(ctx context.Context, prefix string, opts ListOptions) (ListResult, error) {
	if err := ctx.Err(); err != nil {
		return ListResult{}, err
	}
	prefix = strings.Trim(prefix, "/")
	base := s.root
	if prefix != "" {
		base = filepath.Join(s.root, filepath.FromSlash(prefix))
	}
	maxKeys := opts.MaxKeys
	if maxKeys <= 0 {
		maxKeys = 1000
	}

	stagingRel, _ := filepath.Rel(s.root, s.stagingDir)

	var items []ObjectInfo
	walkErr := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			rel, rerr := filepath.Rel(s.root, path)
			if rerr == nil {
				rel = filepath.ToSlash(rel)
				// 跳过暂存目录子树。
				if rel == stagingRel || strings.HasPrefix(rel, stagingRel+"/") {
					if path == filepath.Join(s.root, stagingRel) {
						return filepath.SkipDir
					}
				}
				// 跳过 .spool 暂存子树（契约 §3.1：未压缩中间产物，不是归档对象）。
				if filepath.Base(rel) == SpoolDirName {
					return filepath.SkipDir
				}
			}
			return nil
		}
		rel, rerr := filepath.Rel(s.root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		// 跳过中间产物。
		if isTempName(filepath.Base(rel)) {
			return nil
		}
		if opts.Marker != "" && rel <= opts.Marker {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		items = append(items, ObjectInfo{Key: rel, Size: info.Size(), ModTime: info.ModTime()})
		if len(items) >= maxKeys+1 {
			return filepath.SkipAll
		}
		return nil
	})
	// prefix 目录不存在，返回空列表而非错误（与 S3-style List 一致）。
	if walkErr != nil || len(items) == 0 {
		if walkErr != nil && !os.IsNotExist(walkErr) {
			return ListResult{}, wrapStore(CodeRetryable, "list", prefix, walkErr)
		}
		if walkErr != nil {
			// 目录不存在：空结果。
			return ListResult{Objects: []ObjectInfo{}}, nil
		}
	}

	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })

	res := ListResult{}
	if len(items) > maxKeys {
		res.Objects = items[:maxKeys]
		res.IsTruncated = true
		res.NextMarker = res.Objects[len(res.Objects)-1].Key
	} else {
		res.Objects = items
	}
	if res.Objects == nil {
		res.Objects = []ObjectInfo{}
	}
	return res, nil
}

// Delete 删除对象。
func (s *LocalStore) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	target, err := s.keyPath(key)
	if err != nil {
		return err
	}
	if err := os.Remove(target); err != nil {
		if os.IsNotExist(err) {
			return newStoreError(CodeNotFound, "delete", key, err)
		}
		return wrapStore(CodePermissionDenied, "delete", key, err)
	}
	return nil
}

// CleanupStaging 清理暂存/中间产物：
//   - StagingDir 下所有 mtime 早于 olderThan 的残留文件；
//   - 各天目录下 mtime 早于 olderThan 的 .part/.tmp 中间文件。
//
// 用于接管 archive.staging_retention_hours（默认 336h=14 天）。
func (s *LocalStore) CleanupStaging(ctx context.Context, olderThan time.Duration) (CleanupResult, error) {
	if err := ctx.Err(); err != nil {
		return CleanupResult{}, err
	}
	cutoff := time.Now().Add(-olderThan)
	res := CleanupResult{}

	// 清理暂存目录。
	if _, err := os.Stat(s.stagingDir); err == nil {
		entries, err := os.ReadDir(s.stagingDir)
		if err != nil {
			res.Errors = append(res.Errors, err)
		} else {
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				info, ierr := e.Info()
				if ierr != nil {
					continue
				}
				if info.ModTime().Before(cutoff) {
					if derr := os.Remove(filepath.Join(s.stagingDir, e.Name())); derr != nil {
						res.Errors = append(res.Errors, derr)
						continue
					}
					res.Removed++
					res.Bytes += info.Size()
				}
			}
		}
	}

	// 清理天目录下的 .part/.tmp 中间产物，以及 .spool 中的陈旧未压缩暂存。
	walkErr := filepath.WalkDir(s.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			// .spool 内的文件不叫 .part/.tmp，需单独按 mtime 清理（崩溃残留）。
			if d.Name() != SpoolDirName {
				return nil
			}
			entries, rerr := os.ReadDir(path)
			if rerr != nil {
				res.Errors = append(res.Errors, rerr)
				return filepath.SkipDir
			}
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				info, ierr := e.Info()
				if ierr != nil {
					continue
				}
				if info.ModTime().Before(cutoff) {
					if rerr := os.Remove(filepath.Join(path, e.Name())); rerr != nil {
						res.Errors = append(res.Errors, rerr)
					} else {
						res.Removed++
						res.Bytes += info.Size()
					}
				}
			}
			return filepath.SkipDir
		}
		if !isTempName(d.Name()) {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		if info.ModTime().Before(cutoff) {
			if rerr := os.Remove(path); rerr != nil {
				res.Errors = append(res.Errors, rerr)
			} else {
				res.Removed++
				res.Bytes += info.Size()
			}
		}
		return nil
	})
	if walkErr != nil && !os.IsNotExist(walkErr) {
		res.Errors = append(res.Errors, walkErr)
	}
	if len(res.Errors) > 0 {
		return res, fmt.Errorf("cleanup staging: %d error(s)", len(res.Errors))
	}
	return res, nil
}

// isTempName 判断是否为临时/中间产物文件名。
func isTempName(name string) bool {
	return strings.HasSuffix(name, ".part") || strings.HasSuffix(name, ".tmp")
}
