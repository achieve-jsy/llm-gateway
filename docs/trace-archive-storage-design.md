# 调用链路正文归档——存储（S3 对象存储接入）详细设计

> 状态：详细设计（落地级）
> 上游：本文档是 [docs/trace-archive-compression-design.md](./trace-archive-compression-design.md) §6「归档存储接入」的向下细化，所有决策与之一致，不改写上游结论。
> 范围：ArchiveStore 接口、S3 对象布局、multipart 上传、重试/幂等/完整性校验、生命周期、权限凭证、成本、分析侧读取、配置项。
> 约束回顾（硬约束）：不丢数据、正文可回读、后续可分析、改动最小、可回滚。

## 0. 关键事实（对齐代码，避免设计与现实脱节）

| 事实 | 出处 | 对设计的影响 |
|---|---|---|
| `aws-sdk-go-v2 v1.41.7` + `config` + `credentials` 已是依赖 | `go.mod` | S3 SDK 复用同版本，无需引入新 SDK 家族 |
| `service/s3` / `feature/s3/manager` **未**在 `go.mod` 与 `vendor/` 中 | `vendor/github.com/aws/aws-sdk-go-v2/service/` 仅 bedrock/bedrockruntime/sts 等 | 需新增依赖并 `go mod vendor`（项目用 vendor 构建） |
| 配置体系：viper + `mapstructure` 标签 + `SetDefault` + `BindEnv`（`.`→`_` 的 `AutomaticEnv`） | `internal/controlplane/config/config.go` | 归档配置加 `ArchiveConfig` 结构体 + 默认值 + 环境变量名 |
| 已有加密凭证存储：`system_configs`（`Encrypted` 标志）+ `KeyManager`（KEK/DEK 版本化） | `internal/controlplane/pkg/crypto/manager.go`、`internal/controlplane/model/settings.go` | AK/SK 可作为加密项存库，运行时注入，支持免重启轮换 |
| Bedrock 已有三态凭证模式（AK/SK JSON、Bearer、SDK 默认链） | `internal/providers/bedrock/bedrock.go` | 归档凭证复用同样的「默认链兜底」思路 |
| 正文唯一在线消费点：`GET /api/v1/usage/logs/:request_id/trace`（直读 call_traces 正文） | `internal/observability/handler/usage_handler.go` 的 `Trace` | 回读点查是必做配套，S3 侧必须支持 <1s 点查 |
| 日志列表 `LEFT JOIN call_traces` 只抽 `task_id` / `documents_count` | `usage_handler.go` 的 `ScopedLogs` | 抽成独立列后 JOIN 与正文解耦（存储侧无关，但决定 Parquet 宽表列） |
| **现有清理 cron 有 bug**：`cron.New(cron.WithSeconds())`（要求 6 字段）却配 5 字段 `"0 3 * * *"` → `AddFunc` 报错、任务从未注册 | `internal/controlplane/service/ops_cleanup_service.go` | 归档 worker 复用同骨架时**不要**复制此 bug；并建议顺手修掉 |

---

## 1. ArchiveStore 接口完整定义

### 1.1 设计原则

1. **协议无关**：接口只表达「对象存储」语义（不可变对象、前缀列举、覆盖写），S3/NFS/SFTP 实现互替。
2. **错误可分类**：调用方（Archiver 的 Uploader / Verifier / Janitor）必须能区分「重试可恢复」与「永久失败」，据此进重试队列还是 DLQ。
3. **幂等优先**：归档是"复制 + 校验 + 再删"的旁路，所有写操作天然要求可安全重复执行。
4. 相对框架 §6.1 的草图，本文档是**超集**：补 `Head`（上传后廉价校验 size/ETag，避免全量回读）、把返回值从裸 `error` 细化为 `PutResult`/`ObjectInfo`/分页 `ListResult`，并给出错误类型。旧草图的方法一一保留，仅签名演进，不影响业务逻辑抽象。

### 1.2 类型定义

~~~go
// Package archive 定义调用链路正文归档的存储抽象。
package archive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

// ObjectInfo 是 List/Head 返回的对象元数据。
type ObjectInfo struct {
	Key          string    // 完整对象 key，如 dt=2026-06-04/part-00000.parquet
	Size         int64     // 字节数
	ETag         string    // 存储端返回的 ETag（已去掉引号）；multipart 对象为复合 ETag
	LastModified time.Time
	StorageClass string    // STANDARD / STANDARD_IA / ...；自建平台可能为空串
	ChecksumSHA256 string  // 仅当存储端支持并返回 x-amz-checksum-sha256 时非空
}

// PutOptions 上传参数。
type PutOptions struct {
	ContentType  string            // 默认 application/vnd.apache.parquet
	StorageClass string            // 默认 STANDARD_IA（低频）
	Metadata     map[string]string // 自定义元数据：archive.sha256 / archive.rows / archive.dt 等
	SHA256       string            // 本地上传前计算的全文件 SHA256（十六进制），供服务端校验与写入 manifest
	PartSizeBytes int64            // multipart 分片大小；0 = 实现默认（见 §2.4）
	// IfAbsent=true 时以 If-None-Match:* 实现 put-if-absent，
	// 目标已存在返回 ErrAlreadyExists，用于「已定稿对象不可被覆盖」的强保护场景。
	// 默认 false = 同 key 覆盖（last-writer-wins，即幂等重跑安全）。
	IfAbsent bool
}

// PutResult 上传成功后的存储端确认信息。
type PutResult struct {
	Key   string
	ETag  string
	Size  int64
	Parts int // multipart 上传时 >1；PutObject 单段时 =1
}

// GetOptions 回读参数。
type GetOptions struct {
	// Range 形如 "bytes=0-1023"；空串 = 全量读取。
	// 归档器自身回读用全量；分析引擎（DuckDB/ClickHouse）直连 S3 时自行发 HTTP Range。
	Range string
}

// ListOptions 列举参数（S3 ListObjectsV2 语义）。
type ListOptions struct {
	Prefix            string // 按前缀过滤，如 "dt=2026-06-04/"
	StartAfter        string // 排他起始 key，用于分页续传
	ContinuationToken string // 上一页返回的续传令牌
	MaxKeys           int32  // 单页上限，0 = 实现默认（S3 为 1000）
}

// ListResult 列举结果（分页）。
type ListResult struct {
	Objects             []ObjectInfo
	IsTruncated         bool   // 是否还有更多
	NextContinuationToken string // IsTruncated=true 时非空，作为下一页 ContinuationToken
}
~~~

### 1.3 错误模型（可重试 vs 不可重试）

~~~go
// ErrCode 对存储错误做稳定分类，调用方据此路由到重试队列或 DLQ。
type ErrCode int

const (
	CodeUnknown          ErrCode = iota // 无法归类
	CodeNotFound                        // 对象/前缀不存在（Get/Head/List 目标缺失）
	CodeAlreadyExists                   // put-if-absent 命中已存在
	CodePermissionDenied                // 403/401，凭证或策略问题，重试无意义
	CodeInvalidArgument                 // 400 等请求本身非法（key 非法、range 非法）
	CodeNotSupported                    // 501/400 且为能力缺失（如 checksum 算法不支持）
	CodeRetryable                       // 网络/超时/5xx/429/连接重置，可安全重试
)

// StoreError 是所有存储实现返回错误的统一包装。
type StoreError struct {
	Code ErrCode
	Op   string // 出错的操作名："Put"/"Get"/"Head"/"List"/"Delete"
	Key  string
	Err  error  // 底层错误（可为 nil）
}

func (e *StoreError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("archive: %s %q: %v (code=%d)", e.Op, e.Key, e.Err, e.Code)
	}
	return fmt.Sprintf("archive: %s %q failed (code=%d)", e.Op, e.Key, e.Code)
}
func (e *StoreError) Unwrap() error { return e.Err }

// IsRetryable 判断错误是否可重试（幂等 Put 下重试安全）。
func IsRetryable(err error) bool {
	var se *StoreError
	return errors.As(err, &se) && se.Code == CodeRetryable
}

// IsNotFound 判断是否「对象不存在」。
func IsNotFound(err error) bool {
	var se *StoreError
	return errors.As(err, &se) && se.Code == CodeNotFound
}

// IsAlreadyExists 判断 put-if-absent 是否命中已存在。
func IsAlreadyExists(err error) bool {
	var se *StoreError
	return errors.As(err, &se) && se.Code == CodeAlreadyExists
}
~~~

**错误分类映射表**（实现必须遵守，保证跨存储实现一致）：

| 底层现象（S3 语义） | 归类 | 重试? | 调用方动作 |
|---|---|---|---|
| 404（Get/Head/List 目标缺失） | `CodeNotFound` | 否 | 回读时返回 404；Exists 返回 false |
| 412（If-None-Match 冲突） | `CodeAlreadyExists` | 否 | 视为已归档，跳过 |
| 403 AccessDenied / 401 | `CodePermissionDenied` | 否 | 立即告警，DLQ（重试只会再失败） |
| 400 InvalidArgument / 416 RangeNotSatisfiable | `CodeInvalidArgument` | 否 | DLQ + 告警 |
| 400/501 且为「checksum/功能不支持」 | `CodeNotSupported` | 否 | 降级（关 checksum）后重试一次 |
| 500/502/503/504、429、`context.DeadlineExceeded`、`net` 超时、连接重置、`io.ErrUnexpectedEOF` | `CodeRetryable` | 是 | 进 retry_queue 指数退避 |

> 双层重试：① SDK 内置 Standard Retryer（3~5 次，指数退避 + 抖动）先吸收瞬时抖动；② 仍失败则映射为 `CodeRetryable` 抛给 Archiver 的 retry_queue（§2.5）。两层职责不同：前者覆盖单次调用的网络抖动，后者覆盖「进程重启后仍要续传」的持久化重试。

### 1.4 接口签名与各方法语义

~~~go
// ArchiveStore 对象存储抽象。所有方法必须：
//  1) 尊重 ctx 取消（ctx.Done() 后尽快返回 ctx.Err()）；
//  2) 返回的错误用 StoreError 包装（IsRetryable/IsNotFound 可判）；
//  3) 实现必须并发安全（Put/Get/List 可被多个 goroutine 并发调用）。
type ArchiveStore interface {
	// Put 上传（或覆盖）一个对象。语义：
	//   - 幂等：同 key 重复 Put 得到相同结果（last-writer-wins），重跑安全；
	//   - IfAbsent=true 时 put-if-absent，已存在返回 ErrAlreadyExists；
	//   - 成功返回存储端 ETag 与大小；r 由实现读取完毕后不再使用（调用方负责 Close）。
	Put(ctx context.Context, key string, r io.Reader, size int64, opts PutOptions) (PutResult, error)

	// Get 全量或范围回读。语义：
	//   - 返回的 io.ReadCloser 由调用方 Close；
	//   - 对象不存在返回 ErrNotFound；
	//   - 若读中途网络断开，返回的 Read 错误为 CodeRetryable（调用方可整对象重试）。
	Get(ctx context.Context, key string, opts GetOptions) (io.ReadCloser, error)

	// Head 只取元数据（不下载正文）。语义：
	//   - 用于上传后廉价校验（比对 Size/ETag/ChecksumSHA256）；
	//   - 对象不存在返回 ErrNotFound。
	Head(ctx context.Context, key string) (ObjectInfo, error)

	// Exists 判断对象是否存在。语义：
	//   - 实现为 Head 的布尔投影：存在 true；NotFound false；
	//   - **其他错误（网络/权限）必须原样返回，不得吞成 false**——false 会触发危险的上传/覆盖路径。
	Exists(ctx context.Context, key string) (bool, error)

	// List 按前缀分页列举。语义：
	//   - 结果按 key 字典序升序，稳定可续传；
	//   - 用于清单核对（归档器对比「存储端对象集合」与「manifest 文件集合」）；
	//   - Prefix 为空列举整个桶（权限允许时）。
	List(ctx context.Context, prefix string, opts ListOptions) (ListResult, error)

	// Delete 删除对象。语义：
	//   - 幂等：删除不存在的对象也返回 nil（S3 DeleteObject 对缺失 key 返回 204）；
	//   - 用于过期清理（通常由生命周期规则代劳，见 §3）与失败重跑前的清理。
	Delete(ctx context.Context, key string) error
}
~~~

---

## 2. S3 实现细节

### 2.1 依赖与客户端构造

新增依赖（复用项目已用的 aws-sdk-go-v2 家族）：

~~~bash
go get github.com/aws/aws-sdk-go-v2/service/s3@v1.41.7    # 与 go.mod 内 aws-sdk-go-v2 主版本对齐
go get github.com/aws/aws-sdk-go-v2/feature/s3/manager@v1  # 可选：仅当用 Uploader 便捷封装；本设计用 s3.Client 直驱 multipart，可不引入
go mod tidy && go mod vendor                                # 项目用 vendor 构建，必须重新 vendor
~~~

> 本设计采用 **`s3.Client` 直驱** `CreateMultipartUpload/UploadPart/CompleteMultipartUpload`，而非 `manager.Uploader`：断点续传需要在分片之间把 `uploadId` + 已完成分片持久化到本地 sidecar，`manager` 的黑盒封装不暴露这一钩子。

客户端构造（MinIO / Ceph RGW / 云 OSS 的 S3 兼容端点统一走 BaseEndpoint + path-style）：

~~~go
func NewS3Store(ctx context.Context, cfg S3Config) (*S3Store, error) {
	creds := aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(
		cfg.AccessKey, cfg.SecretKey, cfg.SessionToken,
	)) // AK/SK 为空时，改用 SDK 默认链（环境变量/共享配置/IAM 角色），见 §4.2

	awscfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(orDefault(cfg.Region, "us-east-1")),
		awsconfig.WithCredentialsProvider(creds), // 仅当显式给了 AK/SK
		awsconfig.WithRetryer(func() aws.Retryer {
			return retry.NewStandard(func(o *retry.StandardOptions) {
				o.MaxAttempts = cfg.MaxAttempts // 默认 5
			})
		}),
	)
	if err != nil { return nil, err }

	client := s3.NewFromConfig(awscfg, func(o *s3.Options) {
		o.UsePathStyle = cfg.PathStyle          // 自建/MinIO 通常 true；AWS 云 false
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint) // 如 http://minio.internal:9000
		}
	})
	return &S3Store{client: client, bucket: cfg.Bucket, partSize: cfg.PartSizeBytes}, nil
}
~~~

### 2.2 对象 key 布局（dt 分区 + part + _SUCCESS + _manifest）

以 bucket 根（或 prefix，若共享桶）为命名空间：

~~~text
llm-gateway-traces/                        # bucket（分环境加后缀 -prod / -test）
├── dt=2026-06-04/
│   ├── part-00000.parquet                 # 100~200 MB，Parquet+zstd
│   ├── part-00001.parquet
│   └── _SUCCESS                           # 空对象（0 字节），该 dt 分区完整且已校验的标记
├── dt=2026-06-05/
│   └── ...
├── ...
└── _manifest/
    └── manifest-2026-06-04.json           # 该 dt 的归档清单（行数/checksum/ts 范围/状态）
~~~

key 规则：

| 元素 | key 模板 | 说明 |
|---|---|---|
| part 文件 | `dt=YYYY-MM-DD/part-NNNNN.parquet` | 确定性命中：同一 dt 同一 NNNNN 重跑产生同一 key → 覆盖即幂等 |
| 成功标记 | `dt=YYYY-MM-DD/_SUCCESS` | **最后**写（所有 part 上传+校验通过后）；分析/回读可只认带 _SUCCESS 的分区 |
| 清单 | `_manifest/manifest-YYYY-MM-DD.json` | 与 PG 表 `trace_archive_manifests` 双写，S3 侧是自描述备份 |
| 临时/未定稿 | `_tmp/<dt>/<part>.upload.json` | multipart 断点 sidecar（§2.4），**不进** `dt=` 目录，避免被分析引擎误读 |

分区键 `dt` 由 `ts::date` 派生；`part-NNNNN` 是 5 位零填充序号，单文件 100~200 MB（`archive.part_target_bytes` 默认 128 MB 控制切分）。

**manifest JSON schema**（`_manifest/manifest-2026-06-04.json` 与 PG 表同构，这是审计/回放/对账的唯一事实源）：

~~~json
{
  "schema_version": 1,
  "dt": "2026-06-04",
  "bucket": "llm-gateway-traces-prod",
  "prefix": "dt=2026-06-04",
  "created_at": "2026-06-05T03:12:00Z",
  "status": "verified",
  "row_count": 35123,
  "compressed_bytes": 235000000,
  "uncompressed_bytes": 2300000000,
  "zstd_level": 3,
  "storage_class": "STANDARD_IA",
  "ts_min": "2026-06-04T00:00:00Z",
  "ts_max": "2026-06-04T23:59:59.999Z",
  "files": [
    {
      "key": "dt=2026-06-04/part-00000.parquet",
      "size": 128000000,
      "rows": 19120,
      "sha256": "3f8a...hex...",
      "etag": "abc123...",
      "ts_min": "2026-06-04T00:00:00Z",
      "ts_max": "2026-06-04T12:30:00Z"
    },
    { "key": "dt=2026-06-04/part-00001.parquet", "size": 107000000, "rows": 16003, "sha256": "9c1b...", "etag": "def456..." }
  ],
  "checksum_aggregate": "sha256:<所有文件 sha256 按 key 序拼接后再次 sha256>"
}
~~~

### 2.3 PutObject（单段，小文件 / IfAbsent 场景）

~~~go
func (s *S3Store) putSingle(ctx context.Context, key string, r io.Reader, size int64, opts PutOptions) (PutResult, error) {
	in := &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          r,
		ContentLength: aws.Int64(size),
		ContentType:   aws.String(orDefault(opts.ContentType, "application/vnd.apache.parquet")),
		StorageClass:  types.StorageClass(orDefault(opts.StorageClass, string(types.StorageClassStandardIa))),
		Metadata:      opts.Metadata,
	}
	if opts.IfAbsent {
		in.IfNoneMatch = aws.String("*")
	}
	out, err := s.client.PutObject(ctx, in)
	if err != nil {
		return PutResult{}, s.classify("Put", key, err, opts.IfAbsent)
	}
	return PutResult{Key: key, ETag: trimQuotes(aws.ToString(out.ETag)), Size: size, Parts: 1}, nil
}
~~~

### 2.4 Multipart 上传（大文件分片 + 断点续传）

触发条件：`size > partSize`（默认 partSize=16 MB，则 >16 MB 走 multipart；100~200 MB 的 part 文件约 7~13 片）。

分片大小选择：**16 MB**（下限 5 MB，最后一片除外）。理由：单日 part 文件 100~200 MB，16 MB 片数适中、单片重试代价小；片过小（5 MB）请求次数多、片过大重传成本高。

~~~go
type uploadState struct {
	Key      string                `json:"key"`
	UploadID string                `json:"upload_id"`
	Parts    []types.CompletedPart `json:"parts"`   // 已完成分片（含 ETag）
	Size     int64                 `json:"size"`
	SHA256   string                `json:"sha256"`
}

func (s *S3Store) putMultipart(ctx context.Context, key string, r io.Reader, size int64, opts PutOptions) (PutResult, error) {
	// 1) 断点恢复：本地 sidecar 若存在且 server 端 UploadID 仍有效，则续传
	state, err := s.loadResume(key)          // 读 /var/lib/llm-gateway/archive/staging/_tmp/<dt>/<part>.upload.json
	if err == nil && state.UploadID != "" {
		if parts, listErr := s.listParts(ctx, key, state.UploadID); listErr == nil {
			state.Parts = parts              // 以 server 端为准，合并已完成分片
		} else {
			state = nil                      // UploadID 失效（被 abort/过期）→ 从头开始
		}
	}
	if state == nil {
		create := &s3.CreateMultipartUploadInput{
			Bucket:       aws.String(s.bucket),
			Key:          aws.String(key),
			ContentType:  aws.String(orDefault(opts.ContentType, "application/vnd.apache.parquet")),
			StorageClass: types.StorageClass(orDefault(opts.StorageClass, string(types.StorageClassStandardIa))),
			Metadata:     opts.Metadata,
		}
		out, err := s.client.CreateMultipartUpload(ctx, create)
		if err != nil { return PutResult{}, s.classify("Put", key, err, false) }
		state = &uploadState{Key: key, UploadID: aws.ToString(out.UploadId), Size: size, SHA256: opts.SHA256}
	}

	// 2) 逐片上传（每片成功后持久化断点）
	done := map[int32]bool{}
	for _, p := range state.Parts { done[p.PartNumber] = true }

	var partNumber int32 = 1
	buf := make([]byte, s.partSize)
	for {
		n, readErr := io.ReadFull(r, buf)
		if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
			_ = s.abort(ctx, state) // 读源失败 → 放弃本次 multipart，避免残留
			return PutResult{}, s.classify("Put", key, readErr, false)
		}
		if n == 0 && readErr == io.EOF { break }
		if done[partNumber] { // 已上传的分片跳过
			partNumber++
			if readErr == io.EOF { break }
			continue
		}
		md5b64 := md5Base64(buf[:n])
		up := &s3.UploadPartInput{
			Bucket:        aws.String(s.bucket),
			Key:           aws.String(key),
			UploadId:      aws.String(state.UploadID),
			PartNumber:    aws.Int32(partNumber),
			Body:          bytes.NewReader(buf[:n]),
			ContentLength: aws.Int64(int64(n)),
			ContentMD5:    aws.String(md5b64),          // 单片传输完整性
		}
		out, err := s.client.UploadPart(ctx, up)
		if err != nil {
			if isAbortedUpload(err) { state = nil; return s.putMultipart(...) } // 重启
			return PutResult{}, s.classify("Put", key, err, false)
		}
		state.Parts = append(state.Parts, types.CompletedPart{PartNumber: aws.Int32(partNumber), ETag: out.ETag})
		_ = s.saveResume(state)                     // ★ 断点持久化
		partNumber++
		if readErr == io.EOF { break }
	}

	// 3) 完成
	comp := &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(s.bucket),
		Key:      aws.String(key),
		UploadId: aws.String(state.UploadID),
		MultipartUpload: &types.CompletedMultipartUpload{Parts: state.Parts},
	}
	out, err := s.client.CompleteMultipartUpload(ctx, comp)
	if err != nil { return PutResult{}, s.classify("Put", key, err, false) }
	_ = s.deleteResume(state)
	return PutResult{Key: key, ETag: trimQuotes(aws.ToString(out.ETag)), Size: size, Parts: len(state.Parts)}, nil
}
~~~

断点续传三要素：

1. **本地 sidecar**（`staging/_tmp/<dt>/<part>.upload.json`）持久化 `uploadId` + 已完成分片清单——进程崩溃/重启后据此续传，不重传已成功分片。
2. **server 端对账**：恢复时先 `ListParts` 拿 server 端真实分片，避免本地 sidecar 与实际不一致。
3. **失效回退**：`ListParts` 报 `NoSuchUpload`（UploadID 被生命周期 abort 或过期）→ 丢弃 sidecar、从头重传；`UploadPart` 期间遇 abort 同理递归重启一次。

### 2.5 重试与退避

双层重试（§1.3 已述）：

~~~go
// SDK 层：Standard Retryer（MaxAttempts=5，指数退避 + 抖动）
retry.NewStandard(func(o *retry.StandardOptions) { o.MaxAttempts = 5 })

// Archiver 层：持久化重试队列（指数退避 + DLQ）
backoff := time.Second
for attempt := 1; ; attempt++ {
	err := store.Put(ctx, key, f, size, opts)
	if err == nil { break }
	if !archive.IsRetryable(err) { moveToDLQ(key, err); alert(...); break }   // 403/400 → DLQ + 告警
	if attempt > maxAttempts { moveToDLQ(key, err); alert(...); break }        // 超 N 次进 DLQ
	sleep(backoff); backoff = min(backoff*2, 30*time.Minute)
}
~~~

- 重试只对 `CodeRetryable` 生效；`CodePermissionDenied/InvalidArgument/NotSupported` 直接 DLQ 并告警（避免无谓重试烧额度/日志）。
- 退避上限 30 分钟，默认 maxAttempts=8（约 1 小时窗口），超限进 `staging/dlq/` 并告警。

### 2.6 幂等（同 key 覆盖）

- **默认覆盖**：S3 PutObject / CompleteMultipartUpload 对同 key 是 last-writer-wins → 同一天重跑归档、同 part 重传，结果字节一致、天然幂等。这是本设计的主路径（简单、无状态）。
- **强保护（可选）**：`PutOptions.IfAbsent=true` 用 `If-None-Match:*` 实现 put-if-absent，用于「manifest 已 `verified` 的对象禁止被二次覆盖」的防呆。默认关闭，因为覆盖本身无害。
- **内容寻址（备选，不采用）**：把 sha256 前缀拼进 key（`part-00000-<sha256[:16]>.parquet`）可让重传天然去重，但破坏「archive_ref 指针稳定可预测」的框架约定（框架 §7 的 `archive_ref` 是确定性 key），故不采用；需要内容寻址时用 Metadata 里的 `archive.sha256` 兜底即可。

### 2.7 完整性校验（CRC64 / SHA256 + ETag）

校验链（从本地到存储端，逐层把关）：

~~~text
Compactor 写 Parquet 时计算全文件 SHA256 ──► 写入 manifest + PutOptions.SHA256
        │
        ▼
UploadPart 每片带 Content-MD5 ──► 服务端校验单片传输（multipart）
        │
        ▼
PutObject/Complete 成功 ──► Head 回读比对 Size + ETag（廉价，不下载正文）
        │
        ▼
Verifier（低频全量回读）──► Get 全对象重算 SHA256 与 manifest 比对，通过才置 verified
~~~

- **ETag 语义警告**：单段 PutObject 的 ETag = 内容 MD5（可作校验锚）；**multipart 的 ETag = 各分片 MD5 拼接后再 MD5 并加 `-N` 后缀，不是内容哈希**。因此 ETag 只用于「记录 + 对账标识」，**不得**作为完整性唯一依据——唯一依据是 manifest 里的 SHA256。
- **SHA256**：manifest 权威值；Verifier 阶段对每个 part 文件做一次全量回读重算（每个对象仅一次，验证后置 `verified` 并落 `_SUCCESS`）。
- **CRC64（CRC64NVME，可选）**：AWS S3（及新版 Ceph RGW）支持 `ChecksumAlgorithm=CRC64NVME`，上传后 `x-amz-checksum-crc64nvme` 可零成本回读校验；但 MinIO 老版本/部分自建平台不支持，返回 400/501 时归类 `CodeNotSupported` 并**降级**为 SHA256-only。故 CRC64 作为「支持则启用」的加速项，不作为必需项。

---

## 3. 生命周期策略

### 3.1 存储类选型（结论，对齐框架 §6.3 / §12）

- **默认：低频访问（STANDARD_IA）**。正文归档是"写一次、偶尔点查"，符合 IA 定位；读取实时（非 Glacier 解冻）。
- **热窗口 30 天内的正文留在 PG**，对象存储只承接热窗口之外的按需回读（GB 级/月取回），IA 取回费可忽略。
- 自建 MinIO/Ceph 无存储类差价与取回费时，`StorageClass` 仅作为对象元数据落库，无费用影响。

### 3.2 生命周期规则（S3 lifecycle JSON 示意）

写路径：**直接以 `STANDARD_IA` 上传**（`PutOptions.StorageClass`），不依赖 transition 规则做「standard→ia」。另加两条服务端兜底规则：

~~~json
{
  "Rules": [
    {
      "ID": "abort-incomplete-multipart",
      "Status": "Enabled",
      "Filter": { "Prefix": "dt=" },
      "AbortIncompleteMultipartUpload": { "DaysAfterInitiation": 7 }
    },
    {
      "ID": "expire-after-retention",
      "Status": "Enabled",
      "Filter": { "Prefix": "dt=" },
      "Expiration": { "Days": 3650 }
    }
  ]
}
~~~

| 规则 | 目的 | 默认值 |
|---|---|---|
| `abort-incomplete-multipart` | 清理崩溃残留的分片上传，防止分片碎片累积计费 | 7 天 |
| `expire-after-retention` | 合规/保留政策到期后自动过期（默认永久则不配此规则，`Expiration` 缺省即不删） | 按公司政策，0=永久 |
| （可选）`transition-to-ia` | 若坚持「先 STANDARD 后转 IA」，替代直写：`Transitions: [{Days: 0, StorageClass: STANDARD_IA}]` | 不启用（直写 IA 更简单） |

> 若最终是自建 MinIO：等价配置是 **ILM（生命周期）规则**，或后端 tiered storage 把 `STANDARD_IA` 映射到温/冷盘池；语义一致，配置载体从「桶级 XML/JSON」变为「MinIO ILM XML/console」，不改变对象 key 与取回方式。

### 3.3 何时转标准（升级触发）

生命周期规则**只能向冷降级**（IA 不能再自动升 STANDARD）。「转标准」是运维动作，触发条件与命令：

- **触发条件**：实测月取回量 > 100 GB（框架 §12 的红线），说明访问模式已偏离 IA 假设。
- **执行**（一次性 copy 回 STANDARD，对新数据改配置直写 STANDARD）：

~~~bash
# 把指定前缀对象复制为 STANDARD（CopyObject 同源复制，保留 key）
aws s3 cp s3://llm-gateway-traces-prod/dt=2026-06-04/ s3://llm-gateway-traces-prod/dt=2026-06-04/ \
    --recursive --storage-class STANDARD --metadata-directive COPY
~~~

> 注意：IA 有 30 天最短计费期；「转标准」的同源 copy 会产生短暂双份计费（源 IA + 新 STANDARD），copy 完成后删源对象前需 `Head` 确认新对象就绪。归档对象长期保存、不提前删除，IA 最短计费期对本场景无影响。

### 3.4 过期清理（TTL）

- 默认**永久保留**（合规要求「正文可回读」优先，且月增仅 25 GB、成本可忽略）。
- 若公司要求保留上限 N 天：启用 `expire-after-retention`（`Days = N`），对象到点由存储端自动过期删除；**归档器自身不主动 Delete**（避免误删，删除交给存储端生命周期）。
- 若需手动清理（重跑/纠错）：仅限 `Janitor` 角色（§4.1 的清理角色）使用 `Delete`，且删除前必须比对 manifest 与 PG 行——「正文已在归档且 manifest `verified`」才允许删 PG 大字段；**对象存储侧不手动删**，防止破坏「对象存储始终是完整备份」的兜底。

---

## 4. 权限与凭证模型

### 4.1 最小权限（分角色）

| 角色 | 使用者 | 动作 | 说明 |
|---|---|---|---|
| **归档写入** | Archiver（Uploader） | `PutObject`、`ListMultipartUploadParts`、`AbortMultipartUpload` | 覆盖 Create/UploadPart/Complete（`s3:PutObject` 隐式允许 multipart 子操作） |
| **归档校验** | Verifier | `GetObject`、`HeadObject`、`ListBucket` | 回读比对 SHA256 + 清单核对 |
| **分析只读** | DuckDB/ClickHouse/看板 | `GetObject`、`ListBucket`、`GetBucketLocation` | 只读，禁写禁删 |
| **清理（可选）** | 手动排障 | `DeleteObject` | 默认不给；生命周期代劳过期删除 |

S3 IAM policy（AWS 语义；MinIO 用等价的 `mc admin policy` JSON，动作名相同）：

~~~json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "ArchiverWrite",
      "Effect": "Allow",
      "Action": [
        "s3:PutObject",
        "s3:ListMultipartUploadParts",
        "s3:AbortMultipartUpload"
      ],
      "Resource": "arn:aws:s3:::llm-gateway-traces-prod/dt=*"
    },
    {
      "Sid": "ArchiverList",
      "Effect": "Allow",
      "Action": ["s3:ListBucket"],
      "Resource": "arn:aws:s3:::llm-gateway-traces-prod",
      "Condition": { "StringLike": { "s3:prefix": ["dt=*", "_manifest/*"] } }
    }
  ]
}
~~~

~~~json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "AnalyticsReadOnly",
      "Effect": "Allow",
      "Action": ["s3:GetObject", "s3:GetBucketLocation"],
      "Resource": "arn:aws:s3:::llm-gateway-traces-prod/dt=*"
    },
    {
      "Sid": "AnalyticsList",
      "Effect": "Allow",
      "Action": ["s3:ListBucket"],
      "Resource": "arn:aws:s3:::llm-gateway-traces-prod",
      "Condition": { "StringLike": { "s3:prefix": ["dt=*", "_manifest/*"] } }
    }
  ]
}
~~~

> 分桶隔离是更强选项：归档写角色与分析读角色**分属两套 AK**（而非一套万能凭证），即使分析侧凭证泄露也无法写/删归档。桶设为 **Private**，仅服务端凭证可达。

### 4.2 凭证模式与注入方式（三选一，优先级从高到低）

| 优先级 | 模式 | 配置 | 适用 |
|---|---|---|---|
| 1 | **IAM 角色**（或 K8s IRSA / ECS Task Role） | AK/SK 留空，走 SDK 默认链 | 云上/容器平台已具备角色体系时首选 |
| 2 | **静态 AK/SK**（环境变量） | `ARCHIVE_S3_ACCESS_KEY` / `ARCHIVE_S3_SECRET_KEY` | 自建 MinIO/Ceph 常规做法 |
| 3 | **加密库内凭证**（运行时加载） | `system_configs` 表 `Encrypted=true`，用 `KeyManager`（KEK/DEK）加密 | 需要免重启轮换/集中管理时 |

注入方式说明：

1. **环境变量（推荐默认）**：`ARCHIVE_S3_ACCESS_KEY` / `ARCHIVE_S3_SECRET_KEY`（独立命名，**不**占用 AWS SDK 的 `AWS_ACCESS_KEY_ID` 默认链变量，避免与 Bedrock provider 的默认链串扰）。二者都为空 → 回退 SDK 默认链（读 `~/.aws/credentials` / IMDS 角色）。
2. **KMS / KEK-DEK**：项目已有 `internal/controlplane/pkg/crypto/manager.go` 的 `KeyManager`（KEK 包裹 DEK，DEK 版本化）。AK/SK 写入 `system_configs` 时 `Encrypted=true`，启动时解密注入。云上 KMS 场景下 KEK 可再由 KMS 包裹——本设计直接复用现成 `KeyManager`，不新造轮子。
3. **会话令牌**：`ARCHIVE_S3_SESSION_TOKEN`（STS 临时凭证），用于短期授权（如存量迁移一次性任务）。

### 4.3 内网访问

- `endpoint` 一律填**内网域名/IP**（如 `http://minio.internal:9000` 或平台分配的内网 endpoint），不绑公网域名。
- 传输加密红线（对齐框架 §10）：支持 HTTPS → `use_ssl=true`；仅 HTTP → 仅限私有网段 + 防火墙白名单，或开启应用层 AES-GCM 加密正文列（`archive.encrypt_body=true`）兜底。
- 桶可加内网 IP 白名单（对象存储平台支持时），进一步缩小暴露面。

---

## 5. 成本估算

### 5.1 模型与假设

- 存量 25 GB（20~30 GB 取中值），月增 25 GB，保留期按「永久」（即第 12 个月不扣减）。
- 计费 = Σ 每月月末存量（GB）× 单价（$/GB·月）。第 1 年累计存量 = Σ_{k=0}^{11}(25 + 25k) = **1950 GB·月**；第 12 月月末 = **325 GB**。
- 参考单价（AWS us-east-1 美元价，仅作量级参考；自建平台无差价，以平台实际单价代入公式）：

| 存储类 | 单价 | 取回费 |
|---|---|---|
| STANDARD | $0.023 / GB·月 | 无 |
| STANDARD_IA | $0.0125 / GB·月 | $0.01 / GB（数据取回）+ $0.001/1000 GET |

### 5.2 一年存储费（低频 vs 标准）

| 口径 | STANDARD | STANDARD_IA | 差价 |
|---|---|---|---|
| 第 1 年累计（1950 GB·月） | **$44.9** | **$24.4** | 省 ~$20（46%） |
| 第 12 月末稳态（325 GB） | $7.5 / 月 | $4.1 / 月 | 省 ~$3.4 / 月 |

### 5.3 取回费（仅低频类）

- 假设每月取回 5 GB（历史排障点查 + 离线分析采样）→ 60 GB/年 × $0.01 = **$0.6/年**，可忽略。
- 监控触发：月取回 >100 GB 时启动 §3.3 的「转标准」评估。

### 5.4 结论

- 存储侧年成本 **$25~45（低频 vs 标准）**，量级极小，且自建 MinIO/Ceph 通常更低（只有磁盘成本）。
- **真正的收益在 PG 侧**：183 GB 瘦身到数 GB、且停止 70 GB/月的持续增长（对应的是数据库存储/备份/IO 成本的下滑）。
- 因此选低频是「无风险降本」：即便取回模式偏离假设，转标准也只是一次 copy，零成本试错。

---

## 6. 分析侧读取

### 6.1 DuckDB 读 s3://（httpfs + secret）

~~~sql
-- 一次性：装扩展 + 建 secret（path-style 内网端点 + 自定义 AK/SK）
INSTALL httpfs; LOAD httpfs;

CREATE SECRET archive_s3 (
    TYPE S3,
    KEY_ID 'ARCHIVE_READ_AK',
    SECRET 'ARCHIVE_READ_SK',
    ENDPOINT 'minio.internal:9000',          -- 内网端点；不带 http://（USE_SSL 决定协议）
    REGION 'us-east-1',
    URL_STYLE 'path',                        -- 自建/MinIO 用 path；AWS 云用默认(virtual-host)
    USE_SSL true,
    SCOPE 's3://llm-gateway-traces-prod'     -- 仅对目标桶生效，凭证不泄露到其它桶
);

-- 分区剪枝 + 谓词下推的聚合（只扫结构化列，不碰 request_body_raw/response_body_raw）
SELECT model, sum(total_tokens), sum(est_cost_usd)
FROM read_parquet('s3://llm-gateway-traces-prod/dt=2026-06-*/part-*.parquet')
WHERE status = 200
GROUP BY 1;

-- 回读点查：按 request_id 命中单个行组（<1s）
SELECT request_body_raw, response_body_raw
FROM read_parquet('s3://llm-gateway-traces-prod/dt=2026-06-04/part-*.parquet')
WHERE request_id = 'req_xxxxxxxx';
~~~

### 6.2 ClickHouse S3 表函数 / S3 磁盘

~~~sql
-- 方式一：s3 表函数（即席分析，不落盘）
SELECT model, sum(total_tokens)
FROM s3(
  'http://minio.internal:9000/llm-gateway-traces-prod/dt={dt:Date}/part-*.parquet',
  'ARCHIVE_READ_AK', 'ARCHIVE_READ_SK', 'Parquet',
  'dt Date, request_id String, model String, total_tokens Int64, est_cost_usd Float64, ...'
) GROUP BY model;

-- 方式二：S3 磁盘冷分层（后续上 ClickHouse 在线明细分析时，冷分层与归档同源，零迁移）
--   config.d/storage.xml 声明 disk type=s3（endpoint/access_key/secret），
--   建 MergeTree 时 TTL ... TO VOLUME 'cold'，或直接 disk = 's3_disk'。
~~~

### 6.3 Range 读取对回读点查的支撑

Parquet 文件物理布局决定点查效率，归档器写入时必须配合：

1. **文件结构**：Parquet = footer（元数据，含各 row group 的列 chunk 偏移 + min/max 统计） + 多个 row group。点查只需：`GET Range` 拉 footer（末尾 ~几十 KB）→ 按 min/max 统计定位目标 row group → `GET Range` 只拉该 row group 中 `request_body_raw`/`response_body_raw` 两个列 chunk（几百 KB）。**全文件 200 MB，点查只传输 <1 MB**，故 <1s。
2. **前提——排序写**：Compactor 写 Parquet 时必须 **`SORT BY request_id`**（或 `(dt, request_id)`），使每个 row group 的 `request_id` min/max 区间收窄 → 谓词下推能精确定位到单个 row group（否则 min/max 覆盖全范围，退化为全表扫）。
3. **row group 大小**：默认 128 MB / 或按 10 万行切分，与 part 文件切分（128 MB）一致，保证单个 row group 不至于过大。
4. DuckDB/ClickHouse 的 httpfs/S3 客户端原生发 HTTP Range（S3 `GetObject` 自带 `Range` 支持），无需归档器参与；归档器自身的 `Get(opts.Range)` 保留为内部校验/调试用。

---

## 7. 配置项汇总

### 7.1 配置结构体（加到 `internal/controlplane/config/config.go`）

~~~go
// Config 顶层加字段
type Config struct {
	// ...现有字段...
	Archive ArchiveConfig
}

type ArchiveConfig struct {
	Enabled          bool   `mapstructure:"enabled"`           // POC 开关，默认 false
	HotWindowDays    int    `mapstructure:"hot_window_days"`   // 正文在 PG 的直读窗口
	CronSchedule     string `mapstructure:"cron_schedule"`     // 5 字段 cron（见 §7.3 的 bug 提醒）
	LocalStagingDir  string `mapstructure:"local_staging_dir"`
	PartTargetBytes  int64  `mapstructure:"part_target_bytes"` // Parquet part 切分目标
	ZstdLevel        int    `mapstructure:"zstd_level"`
	RetentionDays    int    `mapstructure:"retention_days"`    // 0=永久
	EncryptBody      bool   `mapstructure:"encrypt_body"`      // 正文列 AES-GCM，默认 false
	CryptoKeyEnv     string `mapstructure:"crypto_key_env"`    // AES 密钥环境变量名
	S3               S3Config `mapstructure:"s3"`
}

type S3Config struct {
	Endpoint      string `mapstructure:"endpoint"`
	Region        string `mapstructure:"region"`
	Bucket        string `mapstructure:"bucket"`
	Prefix        string `mapstructure:"prefix"`        // 共享桶时的命名空间前缀，默认空
	PathStyle     bool   `mapstructure:"path_style"`    // 自建/MinIO 用 true
	UseSSL        bool   `mapstructure:"use_ssl"`
	AccessKey     string `mapstructure:"access_key"`    // 空 = 走 SDK 默认链（IAM 角色）
	SecretKey     string `mapstructure:"secret_key"`
	SessionToken  string `mapstructure:"session_token"`
	StorageClass  string `mapstructure:"storage_class"` // STANDARD_IA
	PartSizeBytes int64  `mapstructure:"part_size_bytes"` // multipart 分片，默认 16MB
	MaxAttempts   int    `mapstructure:"max_attempts"`  // SDK 重试次数，默认 5
	Timeout       string `mapstructure:"timeout"`        // 单请求超时，默认 30s
}
~~~

### 7.2 配置表（字段 / 类型 / 默认值 / 环境变量）

| 配置项 | 类型 | 默认值 | 环境变量 | 说明 |
|---|---|---|---|---|
| `archive.enabled` | bool | `false` | `ARCHIVE_ENABLED` | POC/上线开关 |
| `archive.hot_window_days` | int | `30` | `ARCHIVE_HOT_WINDOW_DAYS` | PG 正文直读窗口 |
| `archive.cron_schedule` | string | `"0 2 * * *"` | `ARCHIVE_CRON_SCHEDULE` | **5 字段** cron（凌晨 2 点） |
| `archive.local_staging_dir` | string | `/var/lib/llm-gateway/archive` | `ARCHIVE_LOCAL_STAGING_DIR` | 本地暂存 + DLQ |
| `archive.part_target_bytes` | int64 | `134217728`（128 MB） | `ARCHIVE_PART_TARGET_BYTES` | part 切分目标 |
| `archive.zstd_level` | int | `3` | `ARCHIVE_ZSTD_LEVEL` | 压缩级别 |
| `archive.retention_days` | int | `0`（永久） | `ARCHIVE_RETENTION_DAYS` | >0 时启用生命周期过期 |
| `archive.encrypt_body` | bool | `false` | `ARCHIVE_ENCRYPT_BODY` | 正文列 AES-GCM |
| `archive.crypto_key_env` | string | `""` | `ARCHIVE_CRYPTO_KEY_ENV` | AES 密钥来源环境变量名 |
| `archive.s3.endpoint` | string | `""` | `ARCHIVE_S3_ENDPOINT` | 内网端点，如 `minio.internal:9000` |
| `archive.s3.region` | string | `us-east-1` | `ARCHIVE_S3_REGION` | 自建平台任意值（SigV4 需非空） |
| `archive.s3.bucket` | string | `llm-gateway-traces` | `ARCHIVE_S3_BUCKET` | 分环境加 `-prod`/`-test` |
| `archive.s3.prefix` | string | `""` | `ARCHIVE_S3_PREFIX` | 共享桶命名空间 |
| `archive.s3.path_style` | bool | `true` | `ARCHIVE_S3_PATH_STYLE` | 自建/MinIO=true；AWS 云=false |
| `archive.s3.use_ssl` | bool | `true` | `ARCHIVE_S3_USE_SSL` | HTTPS 优先 |
| `archive.s3.access_key` | string | `""` | `ARCHIVE_S3_ACCESS_KEY` | 空=IAM 默认链 |
| `archive.s3.secret_key` | string | `""` | `ARCHIVE_S3_SECRET_KEY` | 空=IAM 默认链 |
| `archive.s3.session_token` | string | `""` | `ARCHIVE_S3_SESSION_TOKEN` | STS 临时凭证 |
| `archive.s3.storage_class` | string | `STANDARD_IA` | `ARCHIVE_S3_STORAGE_CLASS` | 低频 |
| `archive.s3.part_size_bytes` | int64 | `16777216`（16 MB） | `ARCHIVE_S3_PART_SIZE_BYTES` | multipart 分片 |
| `archive.s3.max_attempts` | int | `5` | `ARCHIVE_S3_MAX_ATTEMPTS` | SDK 重试次数 |
| `archive.s3.timeout` | duration | `30s` | `ARCHIVE_S3_TIMEOUT` | 单请求超时 |

YAML 示例（`config/prod.yaml`）：

~~~yaml
archive:
  enabled: true
  hot_window_days: 30
  cron_schedule: "0 2 * * *"       # 5 字段
  local_staging_dir: "/var/lib/llm-gateway/archive"
  part_target_bytes: 134217728
  zstd_level: 3
  retention_days: 0
  encrypt_body: false
  crypto_key_env: ""
  s3:
    endpoint: "minio.internal:9000"
    region: "us-east-1"
    bucket: "llm-gateway-traces-prod"
    prefix: ""
    path_style: true
    use_ssl: true
    access_key: ""                 # 留空走 IAM 角色 / 加密库内凭证
    secret_key: ""
    session_token: ""
    storage_class: "STANDARD_IA"
    part_size_bytes: 16777216
    max_attempts: 5
    timeout: "30s"
~~~

`.env.example` 增补（环境变量覆盖 YAML）：

~~~bash
# ─── 调用链路归档（S3 兼容对象存储）────────────────────────────────────
ARCHIVE_ENABLED=false
ARCHIVE_HOT_WINDOW_DAYS=30
ARCHIVE_CRON_SCHEDULE="0 2 * * *"
ARCHIVE_LOCAL_STAGING_DIR=/var/lib/llm-gateway/archive
ARCHIVE_S3_ENDPOINT=minio.internal:9000
ARCHIVE_S3_REGION=us-east-1
ARCHIVE_S3_BUCKET=llm-gateway-traces-prod
ARCHIVE_S3_PATH_STYLE=true
ARCHIVE_S3_USE_SSL=true
ARCHIVE_S3_ACCESS_KEY=
ARCHIVE_S3_SECRET_KEY=
ARCHIVE_S3_STORAGE_CLASS=STANDARD_IA
ARCHIVE_S3_PART_SIZE_BYTES=16777216
~~~

### 7.3 接线要点（viper）

~~~go
// Load() 内新增
v.SetDefault("archive.enabled", false)
v.SetDefault("archive.hot_window_days", 30)
v.SetDefault("archive.cron_schedule", "0 2 * * *")
v.SetDefault("archive.local_staging_dir", "/var/lib/llm-gateway/archive")
v.SetDefault("archive.part_target_bytes", int64(128<<20))
v.SetDefault("archive.zstd_level", 3)
v.SetDefault("archive.retention_days", 0)
v.SetDefault("archive.s3.region", "us-east-1")
v.SetDefault("archive.s3.bucket", "llm-gateway-traces")
v.SetDefault("archive.s3.path_style", true)
v.SetDefault("archive.s3.use_ssl", true)
v.SetDefault("archive.s3.storage_class", "STANDARD_IA")
v.SetDefault("archive.s3.part_size_bytes", int64(16<<20))
v.SetDefault("archive.s3.max_attempts", 5)
v.SetDefault("archive.s3.timeout", "30s")
~~~

> **cron 字段数 bug 提醒**：`v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))` + `AutomaticEnv()` 已自动把 `ARCHIVE_*` 映射到 `archive.*`，无需逐个 `BindEnv`（除历史别名）。同时，归档 worker 复用的 cron 骨架**必须**用 `cron.New()`（标准 5 字段）配 5 字段表达式，或 `cron.WithSeconds()` 配 6 字段——现有 `ops_cleanup_service.go` 混用了 `WithSeconds` + 5 字段导致 `AddFunc` 报错、清理任务从未运行，本设计明确规避并建议一并修复该 bug。

---

## 8. 落地检查清单与风险

### 8.1 落地检查清单

- [ ] `go get service/s3` + `go mod vendor`（vendor 构建）
- [ ] `ArchiveConfig` + 默认值 + `.env.example` 增补
- [ ] `S3Store` 实现 6 个方法 + `StoreError` 分类 + 单测（内存 mock 或 MinIO testcontainer 做契约测试）
- [ ] Compactor **排序写**（`SORT BY request_id`）——点查 <1s 的前提
- [ ] Verifier 全量回读 SHA256 比对 + `_SUCCESS` 最后写
- [ ] 生命周期规则（abort multipart 7 天 / 可选过期）在桶上配置并验证
- [ ] 归档写/分析读两套 AK 分离、桶 Private、内网端点、HTTPS
- [ ] 修复 `ops_cleanup_service.go` 的 `WithSeconds` 字段数 bug（顺手，非本任务必需）

### 8.2 风险与开放项

| 风险 | 影响 | 缓解 |
|---|---|---|
| 对象存储 checksum 算法支持不一（CRC64NVME） | 校验失败误判 | SHA256 为唯一权威，CRC64 降级为可选 |
| 内网平台仅 HTTP | 传输明文 | 私有网段 + 白名单，或 `encrypt_body` AES-GCM |
| IA 取回费异常升高 | 成本 | 监控月取回量，>100 GB 触发转标准 |
| multipart 残留分片 | 计费碎片 | 生命周期 7 天自动 abort |
| 自建平台无存储类差价 | 配置冗余 | `StorageClass` 仅作元数据落库，无副作用 |

### 8.3 待确认输入（承接框架 §12）

1. 对象存储 endpoint / region / bucket 权限 / AK 托管方式（IAM 角色还是 AK/SK）。
2. 是否要求正文落盘加密（决定 `encrypt_body` 与 `crypto_key_env`）。
3. 保留周期（`retention_days`，默认永久）。
4. 平台是否支持 `x-amz-checksum-sha256`（决定 Head 校验走 SHA256 还是仅 Size+ETag）。
