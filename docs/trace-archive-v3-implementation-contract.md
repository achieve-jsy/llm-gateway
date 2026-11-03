# trace 归档 v3 · 实现契约（并行开发唯一接口基准）

> **本文件是所有实现工作流的强制接口基准。** 与 `docs/trace-archive-solution-v3.md` 冲突时，以 v3 的**语义要求**为准、以本文件的**签名/命名**为准（签名是并行集成的契约，不得擅自改）。
> 工作基点：worktree `.worktrees/trace-archive-v3`，分支 `feat/trace-archive-v3`。

---

## 0. 工作流与文件所有权（禁止越界编辑）

| 工作流 | 独占文件 | 说明 |
|---|---|---|
| **WS-A 配置** | `internal/controlplane/config/config.go`、`config/prod.yaml`、`config/dev.yaml`、`config/test.yaml`、`deploy/blue-green/docker-compose.blue.yml`、`.green.yml` | 只加配置，不动业务逻辑 |
| **WS-B 写路径** | `internal/observability/service/archive_service.go` | 冻结期望集 / 分块 zstd / 四步校验 / 失败语义 |
| **WS-C 读路径** | `internal/archive/readbody.go`、`internal/archive/local.go`、新增 `internal/archive/partwriter.go`、`internal/archive/index.go` | 分块写+索引+点读 |
| **WS-D 清理与巡检** | `internal/observability/service/archive_janitor.go`、新增 `internal/observability/service/archive_inspect.go` | 7 项门闸 + 巡检 |
| **WS-E 装配** | `internal/controlplane/router/router.go`、`internal/observability/handler/usage_handler.go`、`internal/observability/cron/archive_cron.go` | 开关拆分接线、两段式读、cron |
| **WS-F 测试** | `internal/archive/*_test.go`、`internal/observability/service/archive_*_test.go`、`internal/observability/cron/archive_cron_test.go`、`tests/archive/**` | 含 v3 §11 的**负向测试 N3–N7** |
| **我（主线）** | `atlas-migrations/**`、`internal/controlplane/model/trace_archive_manifest.go`、`.gitlab-ci.yml` | 迁移与 CI 由主线处理（走 atlas skill） |

**禁止**：编辑他人独占文件；新增文件请放在自己名下；跨文件需求写进交接说明而非直接改。

---

## 1. 数据库 schema（主线交付，其他人只读引用）

### 1.1 `call_traces` 新增列（迁移 `20260921064216_add_trace_archive.sql`）

```sql
ALTER TABLE call_traces
    ADD COLUMN IF NOT EXISTS task_id         TEXT,
    ADD COLUMN IF NOT EXISTS documents_count INT,
    ADD COLUMN IF NOT EXISTS archive_ref     TEXT,
    ADD COLUMN IF NOT EXISTS archive_status  SMALLINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS archived_at     TIMESTAMPTZ;
COMMENT ON COLUMN call_traces.archive_status IS
  '0=未归档 1=已归档校验(正文仍热存PG) 2=已置空(正文离场,仅元数据+指针)';
CREATE INDEX IF NOT EXISTS idx_call_traces_archstatus_ts
    ON call_traces (archive_status, ts) WHERE archive_status = 1;
```

### 1.2 `trace_archive_manifests`（同上迁移，**v3 新增冻结集与巡检列**）

| 列 | 类型 | 用途 |
|---|---|---|
| `dt` | DATE PK | 归档日 |
| `state` | VARCHAR(16) | `archiving` / `verified` / `cleaned` / `empty` / `suspect` |
| `state_rank` | SMALLINT | 0/1/2，用于幂等比较 |
| `expected_total` | BIGINT | **v3 新增**：该天**全部**行数（独立基准，谓词不含 body） |
| `expected_body_count` | BIGINT | **v3 新增**：该天有正文行数 |
| `expected_body_rids_sha256` | TEXT | **v3 新增**：有正文行 rid 按 `COLLATE "C"` 串联的 sha256 |
| `files` | JSONB | index 条目数组（见 §3.2） |
| `row_count` | BIGINT | 实际归档行数 |
| `uncompressed_bytes` / `compressed_bytes` | BIGINT | **v3 新增**：体积统计 |
| `sha256` | TEXT | 归档文件整体哈希 |
| `min_ts` / `max_ts` | TIMESTAMPTZ | 该天时间范围 |
| `verified_at` / `cleaned_at` | TIMESTAMPTZ | 状态时间 |
| `verify_state` | VARCHAR(16) | **v3 新增**：`ok` / `suspect` / `unknown` |
| `last_inspected_at` | TIMESTAMPTZ | **v3 新增**：最近巡检通过时间 |
| `inspect_error` | TEXT | **v3 新增**：最近巡检失败原因 |
| `attempt` | INT | 重试次数 |
| `last_error` | TEXT | 最近错误 |

`dt` 的枚举语义由 v3 §3.1 定义：

```
expected_total > 0 且 expected_body_count > 0  → 正常归档
expected_total > 0 且 expected_body_count == 0 → state='empty'（不写指针、不置空、不计 KPI）
expected_total == 0                            → state='empty'
```

---

## 2. 配置契约（WS-A 实现，其他工作流按此读写）

### 2.1 Go 结构体（`config.go`）

```go
type ArchiveConfig struct {
    // —— 开关（v3 §5.1 拆分；旧 archive.enabled 作兼容读取，映射到 WriteEnabled）——
    WriteEnabled bool // archive.write.enabled / ARCHIVE_WRITE_ENABLED
    ReadEnabled  bool // archive.read.enabled  / ARCHIVE_READ_ENABLED

    // —— 基础 ——
    Root string // archive.root
    TZ   string // archive.tz

    // —— 热窗口与节流 ——
    HotWindowDays   int    // archive.hot_window_days    默认 3
    DaysPerNight    int    // archive.days_per_night     默认 2（仅常规归档，不含回填）
    ClosureLagHours int    // archive.closure_lag_hours  默认 24
    CronSchedule    string // archive.cron_schedule

    // —— 分块与压缩（v3 §4.1 / §E4）——
    ChunkMB            int    // archive.chunk_mb            默认 256（未压缩目标）
    ZstdLevel          string // archive.zstd_level          默认 "best"
    ZstdWindowMB       int    // archive.zstd_window_mb      默认 128（不可省！）
    EncoderConcurrency int    // archive.encoder_concurrency 默认 1
    MaxConcurrentReads int    // archive.max_concurrent_reads 默认 4

    // —— 导出 ——
    BatchRows int // archive.batch_rows 默认 1000
    MaxRetry  int // archive.max_retry  默认 5

    // —— 校验与巡检（v3 §3.2/§3.4/§3.5）——
    VerifySampleRows      int // archive.verify_sample_rows       默认 20
    InspectFreshnessHours int // archive.inspect_freshness_hours  默认 168
    InspectPerCycle       int // archive.inspect_per_cycle        默认 3

    // —— 回填（v3 §7.3，独立通道）——
    BackfillEnabled bool // archive.backfill.enabled  默认 false
    BackfillMaxDays int  // archive.backfill.max_days 默认 30

    // —— 离机副本闸 ——
    RequireOffhostCopy          bool   // archive.require_offhost_copy 默认 true
    OffhostCopyMarker           string // archive.offhost_copy_marker
    OffhostMarkerFreshnessHours int    // archive.offhost_marker_freshness_hours 默认 24

    // —— 磁盘与保留（v3 §9）——
    DiskWatermarkPercent int // archive.disk_watermark_percent 默认 80（env 键沿用 ARCHIVE_DISK_WATERMARK_MB）
    MaxDiskGB            int // archive.max_disk_gb           默认 0
    RetentionDays        int // archive.retention_days        默认 180

    // —— 暂存 ——
    StagingRetentionHours int // archive.staging_retention_hours 默认 336
}
```

**约定**：`config.Config` 上字段名仍为 `Archive ArchiveConfig`。`SetEnvKeyReplacer(".","_") + AutomaticEnv()` 已存在，**WS-A 只需 `SetDefault` + 结构体赋值**。

### 2.2 yaml 片段（`config/*.yaml` 统一）

```yaml
archive:
  write:
    enabled: false
  read:
    enabled: false
  root: /datalog/llm-gateway-archive
  tz: Asia/Shanghai
  hot_window_days: 3
  days_per_night: 2
  closure_lag_hours: 24
  cron_schedule: "0 3 * * *"
  chunk_mb: 256
  zstd_level: best
  zstd_window_mb: 128
  encoder_concurrency: 1
  max_concurrent_reads: 4
  batch_rows: 1000
  max_retry: 5
  verify_sample_rows: 20
  inspect_freshness_hours: 168
  inspect_per_cycle: 3
  backfill:
    enabled: false
    max_days: 30
  require_offhost_copy: true
  offhost_copy_marker: ""
  offhost_marker_freshness_hours: 24
  disk_watermark_percent: 80
  max_disk_gb: 0
  retention_days: 180
  staging_retention_hours: 336
```

---

## 3. 存储格式契约（WS-C 实现，WS-B 消费）

### 3.1 目录布局

```
{root}/call_traces/dt=2026-09-19/
    ├── .spool/part-00000.jsonl        导出暂存（未压缩），封块后删除
    ├── part-00000.jsonl.zst           每块 = 独立 zstd 帧
    ├── part-00001.jsonl.zst
    ├── index.jsonl                    每块一行（未压缩，~10 KB）
    ├── manifest.json                  汇总（行数/字节/sha256/块数）
    └── _SUCCESS                       最后写入；导出开始前必须先删
```

**`archive_ref` 格式不变**：`dt=2026-09-19/`（DB 里存这个）。

### 3.2 `index.jsonl` 行结构（Go 与 JSON 字段名必须完全一致）

```go
type IndexEntry struct {
    Part              string `json:"part"`               // "part-00000.jsonl.zst"
    FirstRID          string `json:"first_rid"`          // 块内第一条 request_id
    LastRID           string `json:"last_rid"`           // 块内最后一条 request_id
    Rows              int64  `json:"rows"`
    UncompressedBytes int64  `json:"uncompressed_bytes"`
    CompressedBytes   int64  `json:"compressed_bytes"`
    SHA256            string `json:"sha256"`             // 该 part 压缩后字节的 sha256
}
```

**不变量（WS-B 与 WS-C 共同保证）**：
- 块内记录按 `request_id` **字节序**（`COLLATE "C"`）连续升序
- `FirstRID <= LastRID`，且 `index[i].LastRID < index[i+1].FirstRID`
- **一行绝不跨块**：只在一整行写完后判断是否封块；单行 > `chunk_mb` 时该行独立成块

### 3.3 codec 参数（硬约束，v3 §E4）

```go
// 写：必须显式 WithWindowSize，否则压缩率静默塌 4.3 倍
zstd.NewWriter(w,
    zstd.WithEncoderLevel(zstd.SpeedBestCompression),
    zstd.WithWindowSize(cfg.ZstdWindowMB<<20),   // 默认 128<<20
    zstd.WithEncoderConcurrency(cfg.EncoderConcurrency),
)
// 读
zstd.NewReader(r, zstd.WithDecoderMaxWindow(cfg.ZstdWindowMB<<20))
```

依赖：`github.com/klauspost/compress`（纯 Go）。**由 WS-C 负责 `go get`，其他人不要改 go.mod。**

---

## 4. `internal/archive` 包 API 契约（WS-C 交付，WS-B/WS-E 消费）

```go
// —— 新增：分块写入器（partwriter.go）——
type PartWriterOptions struct {
    SpoolDir           string // 通常 {dt}/.spool
    ChunkBytes         int64  // 未压缩块目标，默认 256<<20
    ZstdWindowBytes    int    // 默认 128<<20
    ZstdLevel          string // "best"
    EncoderConcurrency int    // 默认 1
}

type PartWriter struct{ /* ... */ }

func NewPartWriter(ctx context.Context, store ArchiveStore, dir string, opts PartWriterOptions) (*PartWriter, error)

// WriteLine 追加一行 JSONL（不含换行，内部补 '\n'）。自动按整行封块。
func (w *PartWriter) WriteLine(line []byte) error

// Close 封最后一块，写 index.jsonl 与 manifest.json。不写 _SUCCESS（由调用方在校验通过后写）。
func (w *PartWriter) Close(ctx context.Context) (PartWriterResult, error)

// Abort 放弃：删除 .spool 与本次产生的 .tmp，返回聚合错误。
func (w *PartWriter) Abort(ctx context.Context) error

type PartWriterResult struct {
    Parts             []IndexEntry
    RowCount          int64
    RIDsSHA256        string // 按写入顺序串联 rid（+"\n"）的 sha256，十六进制
    UncompressedBytes int64
    CompressedBytes   int64
}

// —— 新增：回读校验（v3 §3.2 步①）——
type ReadBackResult struct {
    RowCount   int64
    RIDsSHA256 string
}

// ReadBack 重新打开 dir 下的分块，按 index 顺序流式重算行数与 rid 摘要。
// 实现必须真正解压读盘，不得复用写入侧的内存计数。
func ReadBack(ctx context.Context, store ArchiveStore, dir string, parts []IndexEntry) (ReadBackResult, error)

// —— 新增：索引读写（index.go）——
func WriteIndex(ctx context.Context, store ArchiveStore, dir string, parts []IndexEntry) error
func LoadIndex(ctx context.Context, store ArchiveStore, dir string) ([]IndexEntry, error)
// FindPart 二分定位：返回第一个满足 LastRID >= rid 的条目下标；找不到返回 -1。
// 比较一律使用字节序（bytes.Compare），与导出侧 COLLATE "C" 对齐。
func FindPart(parts []IndexEntry, rid string) int

// —— 已有（保持签名不变）——
func (s *LocalStore) ReadBody(ctx context.Context, archiveRef string, requestID string) (*ArchiveObject, error)
type ArchiveObject struct {
    RequestHeaders json.RawMessage `json:"request_headers,omitempty"`
    RequestBody    json.RawMessage `json:"request_body,omitempty"`
    ResponseBody   json.RawMessage `json:"response_body,omitempty"`
}
```

**`ReadBody` 新流程（v3 §5.2）**：`_SUCCESS` 必须存在 → `LoadIndex` → `FindPart` → `Get(part)` → zstd 解码 → **先用 `bytes.Index` 字节预筛**定位候选行，**仅对候选行 `json.Unmarshal`** → 命中返回，未命中 `CodeNotFound`。

---

## 5. 数据库访问契约（WS-B/WS-D 必须使用的 SQL 口径）

### 5.1 冻结期望行集（v3 §3.1）——**分母独立性的关键**

```sql
-- 基准 1：全量行数（【绝不】含 body 非空谓词）
SELECT count(*) FROM call_traces
 WHERE ts >= $1 AND ts < $2;

-- 基准 2：有正文行的 rid 摘要（固定字节序）
SELECT count(*),
       encode(sha256(convert_to(
         string_agg(request_id, E'\n' ORDER BY request_id COLLATE "C"), 'UTF8')), 'hex')
  FROM call_traces
 WHERE ts >= $1 AND ts < $2
   AND (request_body IS NOT NULL OR response_body IS NOT NULL);
```

> 若某天行数过大导致 `string_agg` 内存压力，可改用 `sha256` 逐行聚合的分段方案，**但结果必须与上述口径逐字节等价**，并在代码注释中说明。

### 5.2 导出（顺序即索引顺序）

```sql
SELECT request_id, ts, request_headers, request_body, response_body, error_code, error_message
  FROM call_traces
 WHERE ts >= $1 AND ts < $2
   AND (request_body IS NOT NULL OR response_body IS NOT NULL)
   AND archive_status IN (0, 1)
 ORDER BY request_id COLLATE "C"
```

**注意**：导出谓词与基准 2 谓词一致（这是设计意图），但**基准 1 才是校验的全量分母**——这正是修 BLK-02 的关键。

### 5.3 写指针（幂等）

```sql
UPDATE call_traces
   SET archive_ref = $1, archive_status = 1, archived_at = NOW()
 WHERE ts >= $2 AND ts < $3 AND archive_status IN (0, 1)
```

### 5.4 置空（幂等、keyset 1000/批）

```sql
UPDATE call_traces SET request_body = NULL, response_body = NULL, archive_status = 2
 WHERE request_id IN (...) AND archive_status = 1
```

---

## 6. 失败与错误语义契约（v3 §3.3）

| 环节 | 失败时 |
|---|---|
| manifest 落库 | **终止本轮，返回 error，不写任何归档文件** |
| 分块写盘 | Abort 清理 → manifest 回 `archiving`、`attempt++`、记 `last_error` → **正文绝不动** |
| 四步校验 | 同上（**不得**写 `_SUCCESS`、**不得**写指针） |
| `markManifestVerified` 落库 | **终止本轮并返回 error**（**禁止**仅 Warn 后继续写指针） |
| 写指针 | 记 error，次轮幂等重试（manifest 已 verified 不重复导出） |
| 置空任一批 | 记 error，次轮续做；抽样回读失败则**跳过该 dt** 并告警 |

**日志级别约定**：所有"影响数据安全"的失败用 `slog.Error`；仅可忽略的情形用 `slog.Warn`。**禁止**用 `Warn` 表达"流程继续但有数据风险"。

---

## 7. 装配契约（WS-E）

### 7.1 开关拆分（v3 §5.1）

- `WriteEnabled` 控制：归档 cron、janitor、巡检、保留期删除
- `ReadEnabled` 控制：`SetArchiveReader`
- **点读器独立构造**：只要 `ReadEnabled && Root != ""` 就构造，与 `WriteEnabled` 无关
- `router.go` 中 `archiveReader` 的构造条件必须相应修改

### 7.2 点读接口（`usage_handler.go`）

```go
// 保持既有注入方式，接口形状：
type traceArchiveReader interface {
    ReadBody(ctx context.Context, archiveRef, requestID string) (*archive.ArchiveObject, error)
}
func (h *UsageHandler) SetArchiveReader(r traceArchiveReader)
```

**三种情况必须可区分（v3 §5.1）**：

| 情况 | 行为 |
|---|---|
| 有正文 | 直接返回 |
| 无正文 + `archive_ref` 非空 + 点读**可用** | 走归档读取 |
| 无正文 + `archive_ref` 非空 + 点读**未启用** | 返回**可观测错误码/明确提示**（**不得**表现为"数据不存在"），并 `slog.Error` |
| 无正文 + `archive_ref` 为空 | 永久丢失态，告警 |

### 7.3 并发闸落点（v3 §5.3）

`MaxConcurrentReads` 信号量放在 **`LocalStore` 层**（`ReadBody` 解码前 acquire），**不是** handler 层。

---

## 8. 测试契约（WS-F）

**必须包含 v3 §11 的负向测试**（这是防回归的核心）：

| 编号 | 测试 | 断言 |
|---|---|---|
| N3 | 归档索引/文件中删 1 行 | `ReadBack` 的 rid 摘要与冻结值不等 → 校验失败 |
| N4 | 注入 manifest 落库失败 | 本轮终止；**指针未被写入** |
| N5 | 预置陈旧 `_SUCCESS`（mtime 早于导出开始） | 校验③失败 |
| N6 | `expected_body_count == 0` 的天 | 标记 `empty`；**不写指针、不置空** |
| N7 | 抽样回读注入失败 | janitor 跳过该 dt 并告警 |
| N8 | 排序一致性 | SQL `COLLATE "C"` 顺序 == Go `bytes.Compare` 顺序（含大小写/数字/连字符/非 ASCII rid） |
| N9 | 压缩率 | 真实 JSONL 样本压缩率 ≥ 9x（防漏配 `WithWindowSize`） |
| N10 | `write=false, read=true` | 点读仍可用 |

测试文件放各工作流自己名下；跨包集成测试放 `tests/archive/`。

---

## 9. 交付前自检（每个工作流必须跑）

```bash
go build ./internal/... ./cmd/...
go vet ./internal/archive/... ./internal/observability/... ./internal/controlplane/...
go test ./internal/archive/... ./internal/observability/... -count=1
```

> 基线已知问题：`test/bedrock_model_id_check.go` 在 main 上有**预先存在**的编译错误，与本特性无关，**不要修它**，也不要因它判定失败。
