# 调用链路记录归档存储 —— 实施级详细设计（单一真源 · ①c 最简版）

> 状态：实施级设计 · **已定稿**（评审→①c 收敛→拍板→代码实现 + 单测通过）。人读/审计用 vicky 版：《[调用链路记录归档存储 —— 详细设计（评审稿）](https://192.0.2.10:9093/reports/2026-08-24-trace-archive-design-review.html)》。
> **本版是评审后的收敛版（①c 最简）**：正文归档到<strong>当前服务器本地存档盘</strong>，格式为<strong>按天 jsonl.gz</strong>（仅存排障/审计回放所需的原始载荷）；<strong>不做 Parquet 列存、不用 DuckDB/parquet-go、不读归档正文做分析</strong>；状态机收敛到 3 态；校验收敛到 2 步。分析/金额走 `call_logs` + 现有 `usage_daily/model_daily_stats/user_daily_stats` + 新 `usage_settlement_daily`(063)。
> 细分文档（存储层 S3 部分已废弃，以本文件为准）：`docs/trace-archive-storage-design.md` 等。

---

## 0. 核心决策（评审后收敛，11 条）

| # | 决策 | 理由 |
|---|------|------|
| 1 | **正文归档 = 按天 `jsonl.gz`**（非 Parquet 列存） | 分析/金额已由 `call_logs`+rollup+063 承担，**不读归档正文列**；归档只做「排障/审计回放」，gzip JSON line 足以 |
| 2 | 驱动表 = `call_traces`（`ct LEFT JOIN cl`） | 正文的家在 call_traces；对账等式天然成立，孤儿 trace 不丢 |
| 3 | 双循环：Archiver → verified；Janitor → 热窗口置空 | 「正文消失」永远晚于「指针出现」；归档失败永不置空 |
| 4 | 置空 = `UPDATE SET body=NULL` 保留元数据行 | call_traces 行还承载 stage_*/error_*/task_id/archive_ref，DELETE 让列表退化为全量回读 |
| 5 | 本地存档盘（无对象存储/CK） | 单机形态链路最短、零存储费、零凭证治理；**单点用硬保障补偿**（见 §13） |
| 6 | Phase A 先抽 `task_id/documents_count` 列 | 置空后 jsonb 抽取失效，列表页仍需这些列，**刚需** |
| 7 | 状态机收敛到 **3 态**：`archiving / verified / cleaned` | 本地盘「写文件 + rename」已够原子；幂等靠「同 key 覆盖 + Exists 短路」 |
| 8 | 校验收敛到 **2 步**：`count(行)==pg_row_count` + 写 `_SUCCESS` | 终点是自家单机盘，不需逐字节抽样；SHA256 写入 manifest 供后台复核 |
| 9 | **点读用纯 Go**（`compress/gzip` + json 扫描当天文件） | 无 cgo / 无 DuckDB / 无 parquet-go，消除双引擎维护；冷数据点读几秒（已接受） |
| 10 | 金额结算表 `usage_settlement_daily`（C·明细账户粒度） | 财务/对账真源，从 call_logs 在清理前聚合（保留） |
| 11 | `archive_stat` 只存相对路径 `dt=YYYY-MM-DD/`；`dt` 时区统一 `(ts AT TIME ZONE :tz)::date` | 换盘/换挂载不写回 DB；避免 `date(ts)` 与归档 dt 两套「天」 |

---

## 1. 事实锚点（代码现状，开发勿脱离）

- `internal/observability/model/call_trace.go` — `CallTrace`（`call_traces`）：`request_id`(PK)、`ts`、`stage_*`、`request_headers/request_body/response_body`(JSONB)、`error_code/error_message`。**无** archive_status/archive_ref/task_id/documents_count。
- `internal/observability/model/call_log.go` — `CallLog`（`call_logs`）：无正文大字段；含 `input/output/cache_read/est_cost_usd`、`input_price_snapshot/output_price_snapshot`、`budget_tier_id/budget_tier_name_snapshot/budget_tier_accounting_eligible/budget_tier_constraints_snapshot`、`usage_missing`、`reasoning_effort`、`inbound_endpoint` 等。
- 采集写入：`internal/gateway/observability/`（collector / production_collector / spool / trace_event）。`trace_event.go` `TraceWriter` 异步写 call_traces，`OnConflict{DoNothing}` 幂等，`scrubNULEscapes`。
- 正文唯一在线整条消费点：`internal/observability/handler/usage_handler.go` `Trace`（L99-120）`GET /api/v1/usage/logs/:request_id/trace`，直读 call_traces。
- task_id 抽列：`ScopedLogs`（L830-930）+ `rollup_service.go QueryLogs`（L553-658）`LEFT JOIN call_traces` 从 `response_body` 用 jsonb 抽 `task_id`（三源 COALESCE）与 `documents_count`。
- 现有金额沉淀：`internal/observability/model/{usage_daily,model_daily_stats,user_daily_stats}.go` + `rollup_service.go`（`Run` L30-103 聚合 `est_cost_usd`+token）+ `RollupCron`（每 5 分钟）。
- **cron bug**：`internal/controlplane/service/ops_cleanup_service.go` L78 `cron.New(cron.WithSeconds())`（6 字段）配 L21 `"0 3 * * *"`（5 字段）→ AddFunc 报错→return→Start 未执行；`cleanupCallLogs`（L196-214）只 DELETE call_logs、不碰 call_traces。
- 依赖：`aws-sdk-go-v2` 主依赖保留（他处用），`service/s3` 依赖取消；归档点读用 Go 标准库，`parquet-go`/DuckDB 均不需要。
- 数据规模：call_traces ≈271万行/183GB；正文 TOAST ≈179GB；78 天；月增 ≈70GB（约 2~3 GB/天正文）。

---

## 2. 数据模型变更（migration 062 / 063）

> 走 atlas-db-migration 流程（当前最新 061）。**实际迁移目录是 `atlas-migrations/`**（CI/atlas.hcl/Dockerfile 均由它应用；`migrations/` 是未接线的遗留目录，勿往里写）。文件名带时间戳前缀，如 `20260824093000_trace_archive.{up,down}.sql`；`migrations/062` 仅是示意序号，开发一律落 `atlas-migrations/`。

### 2.1 migration 062：call_traces 归档列 + manifests

```sql
-- 062_trace_archive.up.sql
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

CREATE TABLE IF NOT EXISTS trace_archive_manifests (
    dt            DATE PRIMARY KEY,
    state         VARCHAR(16) NOT NULL DEFAULT 'archiving',   -- archiving / verified / cleaned(后续)
    state_rank    SMALLINT    NOT NULL DEFAULT 0,
    files         JSONB       NOT NULL DEFAULT '[]',
    row_count     BIGINT      NOT NULL DEFAULT 0,
    pg_row_count  BIGINT      NOT NULL DEFAULT 0,
    sha256        TEXT,
    min_ts        TIMESTAMPTZ,
    max_ts        TIMESTAMPTZ,
    verified_at   TIMESTAMPTZ,
    cleaned_at    TIMESTAMPTZ,
    attempt       INT         NOT NULL DEFAULT 0,
    last_error    TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_manifest_state ON trace_archive_manifests (state);
```

```sql
-- 062_trace_archive.down.sql   （修正：补 DROP archived_at）
DROP TABLE IF EXISTS trace_archive_manifests;
ALTER TABLE call_traces
    DROP COLUMN IF EXISTS archive_status,
    DROP COLUMN IF EXISTS archive_ref,
    DROP COLUMN IF EXISTS documents_count,
    DROP COLUMN IF EXISTS task_id,
    DROP COLUMN IF EXISTS archived_at;   -- ← 修复：down 必须与 up 对称，删掉 archived_at
```

> `archive_ref` 存天目录前缀 `dt=YYYY-MM-DD/`；`files[].name` 存**相对路径**（`dt=YYYY-MM-DD/body-YYYY-MM-DD.jsonl.gz`），从不存绝对路径——换盘/换挂载/未来换存储都不回写 DB。**不给 call_logs 加归档列**（正文的家在 call_traces）。

**`archive_status=0` 泄漏修复**：任何 `archive_status=0` 且 `ts < NOW()-hot_window` 的行（晚到、导出失败进 dead、漏网）永远不会被置空，正文永久留 PG。**处理**：Archiver 每轮对这种行**重新尝试归档**；仍失败则进 `last_error` 并**告警**；同时 Janitor 对「`archive_status=0` 且过热窗口且无有效 bytea/jsonb 正文字段需回放」的**孤行单独标记**，避免 PG 收敛不彻底。落地：Janitor 的筛选改为 `(archive_status=1 OR archive_status=0) AND ts<NOW()-hot_window AND (body 任一非空)`，其中 `archive_status=0` 的先行补归档流程，补不上只告警不置空。

### 2.2 migration 063：金额结算沉淀表

```sql
-- 063_usage_settlement_daily.up.sql
CREATE TABLE IF NOT EXISTS usage_settlement_daily (
    stat_date            DATE        NOT NULL,
    key_id               TEXT        NOT NULL,
    dept_id              TEXT        NOT NULL DEFAULT '',
    studio_id            TEXT        NOT NULL DEFAULT '',
    model                TEXT        NOT NULL,
    provider             TEXT        NOT NULL DEFAULT '',
    budget_tier_id       TEXT        NOT NULL DEFAULT '',   -- '' = 未绑定预算
    budget_tier_name     TEXT        NOT NULL DEFAULT '',
    accounting_eligible  BOOLEAN     NOT NULL DEFAULT false,
    req_count            BIGINT      NOT NULL DEFAULT 0,
    success_count        BIGINT      NOT NULL DEFAULT 0,
    prompt_tokens        BIGINT      NOT NULL DEFAULT 0,
    cached_tokens        BIGINT      NOT NULL DEFAULT 0,
    cache_write_tokens   BIGINT      NOT NULL DEFAULT 0,
    completion_tokens    BIGINT      NOT NULL DEFAULT 0,
    reasoning_tokens     BIGINT      NOT NULL DEFAULT 0,
    input_cost_usd       DECIMAL(18,8) NOT NULL DEFAULT 0,
    output_cost_usd      DECIMAL(18,8) NOT NULL DEFAULT 0,
    cache_read_cost_usd  DECIMAL(18,8) NOT NULL DEFAULT 0,
    est_cost_usd         DECIMAL(18,6) NOT NULL DEFAULT 0,
    diff_usd             DECIMAL(18,8) NOT NULL DEFAULT 0,
    avg_input_price      DECIMAL(14,6),
    avg_output_price     DECIMAL(14,6),
    price_varied         BOOLEAN     NOT NULL DEFAULT false,
    usage_missing_count  BIGINT      NOT NULL DEFAULT 0,
    settled_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    source               VARCHAR(16) NOT NULL DEFAULT 'live',
    batch_id             TEXT        NOT NULL DEFAULT '',
    PRIMARY KEY (stat_date, key_id, dept_id, studio_id, model, budget_tier_id, provider)
);
CREATE INDEX IF NOT EXISTS idx_settlement_statdate ON usage_settlement_daily (stat_date);
CREATE INDEX IF NOT EXISTS idx_settlement_tier    ON usage_settlement_daily (budget_tier_id, stat_date);
CREATE INDEX IF NOT EXISTS idx_settlement_model   ON usage_settlement_daily (model, stat_date);
```

```sql
-- 063_usage_settlement_daily.down.sql
DROP TABLE IF EXISTS usage_settlement_daily;
```

> **时区口径统一（修复）**：`usage_settlement_daily.stat_date`、`usage_daily.stat_date`、归档 `dt` 统一为 **`(ts AT TIME ZONE :tz)::date`（`ARCHIVE_TZ` 默认 Asia/Shanghai）**，不要用 `date(ts)`（依赖 PG server 时区）。三处（rollup / 063 / 归档）必须在**代码层**一致，不寄托「运维确认」。

---

## 3. 存储层（本地存档盘 + jsonl.gz）

### 3.1 ArchiveStore 接口（不变，实现 = 本地文件系统）

```go
type ArchiveStore interface {
    Put(ctx context.Context, key string, r io.Reader, size int64, opts PutOptions) (PutResult, error)
    Get(ctx context.Context, key string, opts GetOptions) (io.ReadCloser, error)
    Head(ctx context.Context, key string) (ObjectInfo, error)
    Exists(ctx context.Context, key string) (bool, error)
    List(ctx context.Context, prefix string, opts ListOptions) (ListResult, error)
    Delete(ctx context.Context, key string) error
}
```
- `StoreError{Code}` 保留（`CodeRetryable`/`CodeNotFound`/`CodeAlreadyExists`/`CodePermissionDenied`/`CodeInvalidArgument`）。
- 本地实现：`Put` = 写临时文件 + `fsync` + `rename` 原子提交；`Head` = `os.Stat`（size/mtime）；`Get` = `os.Open` 流式读。

### 3.2 目录布局

```
{archive.root}/
├── dt=2026-06-04/
│   ├── body-2026-06-04.jsonl.gz        # 当天正文（每行一个 JSON 对象）
│   └── _SUCCESS                          # 0 字节；该天「完整+校验通过」标记（最后写）
├── dt=2026-06-05/ ...
├── _manifest/manifest-2026-06-04.json    # 与 PG trace_archive_manifests 双写
└── _tmp/dt=2026-06-04/body-...jsonl.gz.part   # 临时文件（与目标同目录，保证 rename 原子）
```
- 每天**一个** `.jsonl.gz` 文件（3.4 万行/天 → 压缩后约 300~500 MB，无需分 part）。`_SUCCESS` 作为该天完整标记。

### 3.3 原子写（同目录临时 + fsync + rename）

```text
写 {archive.root}/dt=YYYY-MM-DD/body-YYYY-MM-DD.jsonl.gz.part（gzip 流式写）
→ 写完 fsync(part)
→ rename(part → body-YYYY-MM-DD.jsonl.gz)   # 同目录同文件系统 → 原子
→ 写 _SUCCESS（该天全部落盘且校验通过后）
```
- 断点/孤儿：中断只留 `.part`，后台清理 mtime 早于 `archive.staging_retention_hours`（默认 336h=14 天）的残留；同 key 重跑 = Exists 短路 + 覆盖写（幂等）。

### 3.4 校验（收敛为 2 步）

写入时流式算 SHA256 → 写 manifest `sha256`；写完后：
1. `count(流式读 jsonl.gz 的行数) == pg_row_count`（这天 call_traces 行数，`row_count`）。
2. 该天写 `_SUCCESS`。
- 后台可低频重算 SHA256 与 manifest 比对（可选，告警用）。不再做逐字节抽样。

---

## 4. 归档文件格式（每行一个 JSON，最小回放载荷）

每个 `.jsonl.gz` 内，**每行一个 JSON 对象**，字段=回放一条历史 Trace 所需最小集：

```json
{"request_id":"req_x","ts":"2026-06-04T23:59:10+08:00",
 "request_headers":{...},"request_body":{...},"response_body":{...},
 "error_code":"","error_message":""}
```

- `request_body/response_body/request_headers` = `jsonb::text` 反序列化后的原始 JSON（逐字保存）。
- **不存**：token/成本/维度/预算/耗时等结构化字段——这些在 `call_logs` + rollup + 063（分析/金额走这里，归档不背）。
- 抽取的 `task_id/documents_count` 等列由 062 在 **PG** 侧独立列承载（Phase A 回填），**不在归档文件里重复**。
- 写入侧仍做 `scrubNULEscapes`（NUL→U+FFFD），与 TraceWriter 一致。

---

## 5. 状态机（3 态）

| state | rank | 含义 | 成功→ | 失败→ |
|---|---|---|---|---|
| archiving | 0 | 导出→gz 写盘（.part）→校验 | verified | 回 archiving(attempt+1)；超上限→告警，保留 .part 供重试 |
| verified | 1 | `count==pg_row_count` + `_SUCCESS` → 写 archive_ref/status=1 | 等热窗口→(Janitor) cleaned | 校验不过→回 archiving |
| cleaned | 2 | Janitor 置空完成 | 终态 | — |

- Archiver：Redis leader lock 单实例，每日 cron。幂等 = 同 key 覆盖 + Exists 短路 + 天级确定性（`WHERE ts∈[d,d+1)`）。
- Janitor：每日；`archive_status IN (0,1) AND ts < NOW()-hot_window AND (body 任一非空)`；`archive_status=1` 的直接置空，`archive_status=0` 的先补归档、补不上只告警不置空。

---

## 6. 存量迁移（179GB / 78 天）

- **Phase A 前置**：抽 `task_id/documents_count` 独立列并回填（写侧 `traceEventToModel` 同步补算）。
- 按天 78 片；片内按 request_id 升序 keyset 游标；`batch_rows=3000`；读限速 `read_rate_limit_mb=60`；低峰 02:00–06:00；`days_per_night=5` → ~16 晚/2.3 周。
- 流程：①快照基准(pg_row_count) → ②流式导出每行 JSON → gzip 写 `.part`（SHA256+行数）→ ③rename 提交 + `_SUCCESS` → ④写 manifest state=verified + `archive_ref`/`status=1`（正文未动） → ⑤Janitor 延迟置空。
- **校验**：`count(jsonl.gz 行)==pg_row_count` + `_SUCCESS`（2 步）。

---

## 7. 增量归档 + 水位线

- `closure_lag_hours=24`；`waterline_dt = 今天00:00 - L`；存量覆盖 `[min_dt, waterline_dt)`；增量首周期从 waterline 起、每轮归档「上一个已关闭天」；边界日留给增量，不漏不重。
- 每轮 cron 推进水位线 → 取 `dt<waterline 且 state=archiving`（或超时未心跳）→ 逐天驱动状态机；连续失败的 `dt` 告警。

---

## 8. 回读（两段式，纯 Go 点读 jsonl.gz）

```go
var trace obsmodel.CallTrace
h.db.First(&trace, "request_id = ?", rid)                          // ① PG 元数据
if (trace.RequestBody == nil || trace.ResponseBody == nil) && trace.ArchiveRef != "" {
    body := h.archiveStore.ReadBody(ctx, trace.ArchiveRef, rid)   // ② 点读
    trace.RequestBody, trace.ResponseBody = body.Req, body.Resp
}
c.JSON(http.StatusOK, trace)
```
- `ReadBody(ctx, dtPrefix, requestID)`：`os.Open({archive.root}/dtPrefix/body-{date}.jsonl.gz)` → `gzip.NewReader` → 逐行 json.Unmarshal → 命中 `request_id` 即返回 `request_body/response_body/request_headers`（几秒内，已接受）。
- **纯 Go（`compress/gzip` 标准库），无 cgo、无 DuckDB、无 parquet-go**——点读引擎不存在选型分支。
- 优化（可选）：当冷数据点读频率上来，再加一个「按 request_id 排序的 day 级偏移索引」实现 seek；当前不做（低频阅读已够）。

---

## 9. 分析（不走归档正文）

- 分析/金额/用量看板**不读归档正文**，继续走：`call_logs` + `usage_daily/model_daily_stats/user_daily_stats` + `usage_settlement_daily`(063)。这些表在 `call_logs` 清理前由 rollup cron 沉淀，归档文件只做「排障/审计回放」。
- 因此**不需要** Parquet 列存、DuckDB、ClickHouse 或任何「正文列 SQL 分析」引擎。

---

## 10. 配置项（①c 版 · 定稿，对齐实施代码 `ArchiveConfig`）

| 配置项 | 类型 | 默认 | 环境变量 |
|---|---|---|---|
| archive.enabled | bool | false | `ARCHIVE_ENABLED` |
| **archive.root** | string | `/var/lib/llm-gateway/archive`（部署建议 `/mnt/archive`） | `ARCHIVE_ROOT` |
| archive.tz | string | `Asia/Shanghai` | `ARCHIVE_TZ` |
| archive.hot_window_days | int | 30 | `ARCHIVE_HOT_WINDOW_DAYS` |
| archive.batch_rows | int | 3000 | `ARCHIVE_BATCH_ROWS` |
| archive.max_retry | int | 5 | `ARCHIVE_MAX_RETRY` |
| archive.gzip_level | int | 6 | `ARCHIVE_GZIP_LEVEL` |
| archive.closure_lag_hours | int | 24 | `ARCHIVE_CLOSURE_LAG_HOURS` |
| archive.cron_schedule | string | `"0 2 * * *"` | `ARCHIVE_CRON_SCHEDULE` |
| archive.staging_retention_hours | int | 336 | `ARCHIVE_STAGING_RETENTION_HOURS` |
| **archive.require_offhost_copy** | bool | true | `ARCHIVE_REQUIRE_OFFHOST_COPY` |
| archive.offhost_copy_marker | string | `""` | `ARCHIVE_OFFHOST_COPY_MARKER` |
| archive.backfill_once | bool | false | `ARCHIVE_BACKFILL_ONCE` |
| **archive.disk_watermark_mb** | int | 80（使用率%） | `ARCHIVE_DISK_WATERMARK_MB` |
| **archive.max_disk_gb** | int | 0（不限，超限停写不删） | `ARCHIVE_MAX_DISK_GB` |

> ①c 移除项：`days_per_night`/`read_rate_limit_mb`/`write_rate_limit_mb`/`local_staging_dir`（纯 Go 点读 + 原子写内置，无需限速/暂存配置）、`encrypt_body`/`crypto_key_env`（AES-GCM 不启用，见 §17②）、`retention_days`（0=永久、不 TTL 删除，容量由 `disk_watermark_mb`/`max_disk_gb` 主动守卫）、`zstd_level`/`part_target_bytes`/`slice_target_bytes` 及任何 `archive.s3.*`/`archive.duckdb_*`。
> 值为百分比语义的 `disk_watermark_mb` 沿用历史键名（yaml 键与 env），避免破坏既有 env；运行时按「使用率 ≥ 该值即告警」解释。

---

## 11. 安全 / 凭证

- 无对象存储 AK/SK/IAM/KeyManager；仅**文件系统权限**（归档目录归属服务用户，`700/750`，umask 收紧）。
- 正文落盘加密 `archive.encrypt_body`（默认 false）：仅当本地盘未全盘加密且合规要求密文时才开（AES-GCM，密钥经 `archive.crypto_key_env`）。

---

## 12. 成本与磁盘容量

- 无对象存储费。压缩后：存量 179GB → **20~30 GB**（gzip）；月增 ~70GB 正文 → **~8 GB/月**；一年 ≈100GB、两年 ≈200GB。
- 规划 ≥500 GB 独立数据盘；或按 `retention_days` 计算峰值。真正收益在 PG 侧（183GB → 数GB，且止涨）。
- 容量水位：80% 告警；超 `max_disk_gb` 只停归档并告警，不删。

---

## 13. 风险（单点 → 硬保障）

| 风险 | 影响 | 缓解（硬要求，非「推荐」） |
|---|---|---|
| **单机磁盘单点** | 置空后正文唯一副本在本盘，主机故障/误删/勒索即丢 | **硬门槛**：归档盘必须每天 `rsync`/快照到**另一台主机**或异地，并每月做一次**恢复演练（restore drill）**；若无独立于该主机的副本，**不允许推进「置空」到 archive_status=2**（校验不过就不置空，正文留 PG）。RAID1 只防单盘，**不构成灾难冗余** |
| **置空后无法重建** | `archive_status=2` 行正文唯一副本是存档盘 | README 写明；归档盘与数据库同级备份；「从存量重新导出」只对未置空(≤1)成立 |
| **时序地雷** | cron 修复后 `cleanupCallLogs` 首次运行删 48 天 call_logs | **硬闸**：开启 cleanup 前，必须通过「**存量归档覆盖面 + 063 backfill 天数覆盖**」两项对账（见 §14） |
| **点读慢** | 冷数据点读需解压当天 | 已接受（低频/几秒）；频率上来再加 day 级偏移索引 |
| 容量增长 | 一年 ~100GB | 水位告警 + `max_disk_gb` + 可选 `retention_days` |

---

## 14. 阶段计划（带硬闸时序）

| 阶段 | 内容 | 交付物 | 验收 |
|---|---|---|---|
| P0 POC | 取 1 天真实数据，按 jsonl.gz 导出到本地盘，验证压缩比/点读时延/容量 | POC 报告 | gzip 压缩比量级成立；cold 点读≤几秒；容量水位可监控 |
| P1 存量迁移 | Phase A 抽列回填 → 按天 78 片分批{gz 写盘+校验+置空}；**同步 backfill 78 天 usage_settlement_daily** | 179GB 正文离场；历史金额已沉淀 | 全部分片 verified/cleaned；count 对账全过；063 覆盖 78 天且 diff 合理 |
| P2 增量归档 | Archiver cron + 水位线 + 3 态 | 正文滞留 1 周期自动离场 | 每日 count 对账过；不漏不重 |
| P3 Janitor 置空 | 热窗口到期置空 + 补归档 archive_status=0 孤行 | call_traces 收敛 | 30 天稳态正文封顶 ~70GB（**如实口径，非「数GB」**）；archive_status=0 孤行清零/告警 |
| P4 回读接线 | Trace 改两段式（纯 Go 点读）；QueryLogs/ScopedLogs 改读 ct.task_id/documents_count | 历史正文可回读 | cold 点读≤几秒；列表 task_id/documents_count 正确 |
| P5 cron bug 修复 | 去 WithSeconds 用 5/6 字段，Start 返回 error；**加硬闸：归档+063 backfill 覆盖通过才允许 cleanup 运行** | cleanup 真正执行 | cron 正常触发；call_logs 30 天保留生效；开启前对账通过 |

**上线顺序（硬闸）**：P0 → P1（存量归档 + 063 backfill 完成，且**对账通过**） → P2 → P3 → P4 → **P5 修 cron**（修复即开启 cleanup，必须以 P1 对账通过为前提）。任何一步把修好的 cron 提前带上，会触发「48 天 call_logs 一次删除」。

---

## 15. 金额结算沉淀表（保留，C·明细账户）

- 粒度：`(stat_date, key_id, dept_id, studio_id, model, budget_tier_id, provider)`（C·明细账户，已拍板）。
- Live：`RollupService.Run(day)` 追加 `aggregateUsageSettlementDaily`（RollupCron 每 5 分钟今天+昨天）；Backfill：一次性 78 天。二者都从 `call_logs` 聚合（`input/output/cache/est/diff` + 价快照 + `usage_missing`），幂等 upsert。
- **时区统一**：`stat_date = (ts AT TIME ZONE :tz)::date`（与归档 `dt`、现有 rollup 一致）。
- **覆盖对账**：开启 cleanup 前，`usage_settlement_daily` 必须覆盖 `[min_dt, today)` 且每天 `sum(req_count)` 与 `call_logs` 对得上（否则 30 天后 call_logs 没了无法重跑）。

---

## 16. 回滚与失败处理

- 归档/写盘/校验失败只改 manifest（state/attempt/last_error），从不写 archive_ref、从不置空。
- 先指针后置空（T1 verified 写指针/status=1 正文仍在 PG；T2 热窗口到期置空正文安全在本地盘，且**置空前置条件=已通过独立主机备份**，见 §13）。
- migration 062/063 down 整体可回退（062 down 已补 DROP archived_at）。
- 置空 keyset 游标 + 每批独立 autocommit（1000 行/批），批间 sleep，按 PK 点更新仅行锁，不阻塞 TraceWriter。

---

## 17. 待拍板输入（4 项 → 已定稿）

> 2026-08 定稿：以下四项均已拍板，代码按此落地；`archive.require_offhost_copy` 默认 **true** 为单点硬保障。

1. **归档落盘目录 + 磁盘容量/挂载点 + 备份策略** —— 已定稿：`ARCHIVE_ROOT=/mnt/archive`（≥500GB 独立数据盘；代码默认 `/var/lib/llm-gateway/archive`，部署以 env 覆盖）；**每天 rsync 到另一主机 B + 每月一次恢复演练**。`require_offhost_copy=true` 时，仅当 `offhost_copy_marker` 指向「另一主机副本已确认存在」的标记（运维完成 rsync+演练后落盘）才允许 Janitor 置空。
2. **正文落盘加密** —— 已定稿：**不开**（`encrypt_body=false`，AES-GCM 不启用）。仅当本地盘未全盘加密且合规明确要求密文时才评估（届时再补 `crypto_key_env` 字段）。
3. **归档保留周期 + 容量阈值** —— 已定稿：保留周期 **0=永久、不 TTL 删除**；容量由 `disk_watermark_mb=80`（使用率≥80% 仅告警、继续写）+ `max_disk_gb=0`（不限；超限只停归档并告警、绝不删数据）主动守卫（#3 守卫已实现）。
4. **热窗口天数** —— 已定稿：**30 天**（`hot_window_days=30`；窗口内正文留 PG 直读秒开，窗口外置空归档）。
