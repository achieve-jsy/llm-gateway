# 调用链路记录压缩归档与对象存储上传设计方案

> 状态：草案（待评审）
> 范围：call_logs 与 call_traces 的正文数据冷热分离——压缩后上传公司对象存储，PostgreSQL 只保留元数据与索引。
> 核心原则：不丢数据、可回滚、后续可分析、改动最小。
> 详细设计（落地层）：见 [trace-archive-detailed-design.md](trace-archive-detailed-design.md)（压缩 / 删除迁移 / 存储三份）。

## 1. 背景与目标

### 1.1 问题

| 项 | call_logs | call_traces |
|---|---|---|
| 行数 | ~297 万 | ~271 万 |
| 数据跨度 | 78 天 | 78 天 |
| 总占用 | 3.1 GB | **183 GB** |
| 其中 TOAST（大字段离行存储） | 8 KB | **179 GB** |
| 月增长 | ~1.2 GB | **~70 GB** |

call_traces 的 request_body / response_body 两个 jsonb 大字段（完整 prompt/response 原文）贡献了 179 GB TOAST，是生产库空间的绝对大头，且以 ~70 GB/月 持续增长。

### 1.2 目标

1. 不丢数据：正文原文全部保留，任何历史请求可回溯。
2. 压缩降本：正文转列存 + 高压缩率格式，预期 179 GB → 20~30 GB。
3. 可分析：压缩产物可直接被分析引擎（DuckDB / ClickHouse / Pandas）查询，无需回灌数据库。
4. PG 瘦身：call_traces 正文离场，PG 只保留元数据 + 归档指针，稳定在数 GB 量级。
5. 改动最小、可回滚：现有写入链路尽量不动，归档作为独立旁路，失败可回退。

### 1.3 非目标

- 不做 PG 表分区改造（可与本方案并行，但不是本方案范围）。
- 不做正文脱敏改写（保留原文；如需脱敏，在分析侧按需做，见第 10 节）。

### 1.4 正文定位：分析对象，不是纯冷数据

- 正文（prompt/response 原文）是"用户使用分析 + 正文内容分析"的对象，不是写完就冷藏的日志。
- 因此正文必须满足三点：可过滤（按任务类型 / 长度 / 是否拒绝等特征）、可聚合（按用户 / 部门 / 模型）、可检索（关键词 / 子串）。
- 写入时把高频分析标量抽成独立列（messages_count / finish_reason / has_tool_calls / task_id / documents_count 等），正文分析靠标量列，原文保留在归档里可回放。
- 决策：先不上 ClickHouse——用户使用分析走现有 PG rollup（usage_daily / model_daily_stats / user_daily_stats），正文离线分析走 DuckDB + Parquet，成本最低。ClickHouse 留作"需要在线明细分析"时的后手：届时正文与标量已在 Parquet，灌 CK 不返工。

## 2. 现状盘点（代码事实）

- 写入链路：internal/gateway/observability/trace_event.go 的 TraceWriter 批量写 call_traces；call_logs 走 Redis spool + 批量入库（spool.go / production_collector.go）。
- 正文消费点（全项目仅两处）：
  1. 单条 trace 详情 GET /api/v1/usage/logs/:request_id/trace（usage_handler.go 的 Trace）——返回整条 trace 供排障。
  2. 日志列表的 LEFT JOIN call_traces（rollup_service.go QueryLogs、usage_handler.go）——只从正文抽取两个标量：task_id（三源 COALESCE：`NULLIF(substring(inbound_endpoint FROM '/v1/contents/generations/tasks/([^/?#]+)'), '')` → `response_body->>'id'` → `response_body->>'task_id'`）与 documents_count（`CASE WHEN jsonb_typeof(request_body->'documents')='array' THEN jsonb_array_length(...) END`）。
- 结论：正文外置后，只要把 task_id、documents_count 抽成独立列，两个 JOIN 场景即与正文解耦；trace 详情场景走懒加载回读。

## 3. 总体架构

    GW[gateway 请求链路] --> TW[TraceWriter]
    TW -->|元数据 + 小字段| PG[(PostgreSQL call_traces 瘦身表)]
    TW -->|正文| BUF[本地 Parquet 缓冲]
    BUF -->|周期 flush| ARC[归档器 Archiver]
    ARC -->|压缩 zstd| FS[(公司对象存储)]

    OLD[存量 179GB 正文] -->|一次性分批导出| ARC
    FS -->|DuckDB / ClickHouse 直查| ANA[分析 / 看板]
    PG -.->|archive_ref 指针| FS

链路分两条：

1. 存量迁移（一次性）：把 PG 中已有的 179 GB 正文分批导出 → 压缩 → 上传 → 校验 → 置空/删除 → 回收空间。
2. 增量归档（长期）：新增数据照旧近实时落 PG（正文只滞留一个归档周期，默认 1 天），归档器周期性地把已关闭批次导出 → 压缩 → 上传 → 校验 → 瘦身 PG。

> 选择"落 PG → 周期归档"而非"直写对象存储"的原因：零侵入现有写入链路，TraceWriter 不改；PG 正文最多滞留 1 个周期（约 2~3 GB），问题已解决；归档失败只影响瘦身进度，不影响主链路可用性。直写对象存储可作为后续演进。

## 4. 压缩方案选型

### 4.1 格式对比

| 格式 | 压缩率 | 分析友好度 | 写入成本 | 结论 |
|---|---|---|---|---|
| NDJSON + gzip | ~5x | 差（全量解压才能查，无列裁剪/谓词下推） | 最低 | 仅适合冷备 |
| Parquet + zstd | 10~20x | 好（列存、行组级统计、谓词下推，DuckDB/ClickHouse/Pandas 直接 SQL 查） | 中 | 推荐 |
| Avro + snappy | ~8x | 中 | 中 | 备选 |
| ORC | ~15x | 好 | 中 | 备选（Spark/Hive 生态更搭） |

推荐 Parquet + zstd，理由：

- 列存：分析只统计 token/成本/耗时时，完全不读 request_body/response_body 大列。
- 谓词下推：按 ts/model 过滤时跳过大量行组，扫描量骤降。
- 生态：DuckDB 直接 read_parquet('*.parquet')，ClickHouse 有 S3/文件表函数，Pandas/Spark 原生支持。
- zstd：对重复结构（JSON 里大量重复 key、相似模板）压缩效果显著优于 gzip/snappy。

### 4.2 分区与目录布局

按天分区（Hive 风格），对象存储根目录：

    llm-gateway-traces/
    ├── dt=2026-06-04/
    │   ├── part-00000.parquet
    │   └── _SUCCESS
    ├── dt=2026-06-05/
    │   └── ...
    ├── ...
    └── _manifest/
        └── manifest-2026-06-04.json

- 单日约 3.5 万条 trace、压缩前 ~2.3 GB JSON，压缩后约 200~300 MB，按 part-NNNNN 切分为 100~200 MB/文件。
- 分区键 dt 用 ts 的日期，与按天/按月分析的典型查询对齐，支持目录级剪枝。

### 4.3 宽表字段设计（schema）

归档文件为 call_logs ∪ call_traces 的宽表（按 request_id 关联），一张 Parquet 即完整调用记录，分析时无需二次 JOIN：

    # 分区/时间
    dt                  DATE        # 由 ts 派生，目录分区键
    ts                  TIMESTAMP

    # 维度（过滤/分组，高基数）
    request_id          STRING
    model / routed_model / group_name / provider
    key_id / key_name / key_type
    user_id / user_name / dept_id / dept_name / studio_id / studio_name
    inbound_endpoint / reasoning_effort / ip_address / user_agent
    status / upstream_status

    # 指标（聚合）
    prompt_tokens / cached_tokens / cache_write_tokens / completion_tokens / reasoning_tokens / total_tokens
    duration_ms / first_token_ms
    stage_auth_ms / stage_route_ms / stage_upstream_ms / stage_ttfb_ms / stage_ttft_ms / stage_total_ms
    input_cost_usd / output_cost_usd / cache_read_cost_usd / est_cost_usd
    input_price_snapshot / output_price_snapshot

    # 布尔
    stream / failover_used / routed_via_group / usage_missing

    # 预算
    budget_tier_id / budget_tier_name_snapshot / budget_block_code / budget_block_dimension

    # 正文（大字段，zstd 压缩；raw 保留原文）
    request_headers_raw  STRING   # 已过滤 Authorization/Cookie
    request_body_raw     STRING   # 原始 jsonb::text，保留原文
    response_body_raw    STRING   # 原始 jsonb::text，保留原文

    # 正文抽取出的标量（高频分析字段，独立成列避免每次解析大 JSON）
    task_id              STRING
    documents_count      INT

> 正文用 STRING 存原始 JSON（jsonb::text），分析时用 json_extract / DuckDB 的 ->> 按需解析。若后续某些子字段（如 request_body.model、messages 长度、max_tokens）成为高频分析维度，可再展平成顶层列（宽表加列，向后兼容）。

### 4.4 压缩比预估

- 现状 179 GB 是 PG TOAST 内 pglz 压缩后的体积，未压缩 JSON 原始量级更大（约 400~500 GB）。
- Parquet 列存 + zstd：结构化列近乎无冗余、正文 string 列 zstd 去重明显。
- 预估归档后 20~30 GB（结构化列 <1 GB，正文列占大头）。以 1 天数据做 POC 实测为准，再定 partition 大小与压缩级别（zstd level 3 起步，收益递减则不再加）。

## 5. 数据流转设计

### 5.1 存量迁移（一次性，179 GB）

按"先复制 → 校验 → 再删"铁律执行，幂等、可断点、可回滚。

    Phase A  抽出标量列（先做，后续 JOIN 解耦依赖它）
      ALTER TABLE call_traces ADD COLUMN IF NOT EXISTS task_id TEXT,
                                ADD COLUMN IF NOT EXISTS documents_count INT;
      增量回填 + 写入侧同步写这两个列

    Phase B  分批导出（只复制，不删）
      按天分片（78 片），每片：
        COPY (SELECT ... FROM call_logs cl LEFT JOIN call_traces ct
              WHERE cl.ts >= :d AND cl.ts < :d+1)
        TO 'dt=YYYY-MM-DD/part-NNNNN.parquet' (FORMAT parquet, COMPRESSION zstd)
      限速 + 低峰期执行（避免 COPY 大字段打满生产库 IO）
      每片记录行数 + 文件 sha256

    Phase C  校验（不通过绝不删）
      每片：归档行数 == PG 该片行数
      抽样比对 request_id 集合与正文 md5
      写 manifest（行数/checksum/ts 范围/压缩前后大小）

    Phase D  切换 + 回收
      校验通过的老数据：UPDATE call_traces SET request_body=NULL, response_body=NULL WHERE ts < 水位线
        （或直接 DELETE 老行，元数据已在归档中）
      VACUUM FULL / pg_repack 回收 TOAST 物理空间
      trace 详情接口对正文已外置的行走懒加载回读

水位线：迁移开始时记录 max(ts) 作为水位线，迁移期间的新数据在增量归档（5.2）中兜底，不漏迁、不重迁。

### 5.2 增量归档（长期，周期 cron）

- 新增独立归档 worker（参考现有 ops_cleanup_service.go 的 cron 骨架 + Redis leader lock 单实例）。
- 周期默认每日一次，归档 ts 在上一个周期之前、且已从 Redis spool 完全落库的行。
- 流程 = Phase B→C→D 的收敛版：导出当周期数据 → 压缩上传 → 校验 → 置空 PG 大字段。
- 归档动作 = 复制（不删）：每天把正文复制到对象存储，PG 原文不动。
- 置空动作 = 热窗口到期：正文在 PG 保留热窗口 N 天（默认 30 天，可配），N 天后 Janitor 才置空 PG 大字段。
- 效果：对象存储始终是完整备份（不丢）；PG 只留最近 30 天正文，用户查最近记录直读 PG，更早走回读（见 7.1）。
- 归档失败：文件留在本地缓冲 + 重试队列，不影响主链路；连续失败告警。

### 5.3 归档器内部设计（压缩 → 上传端到端）

> 本节是"当前服务"需要负责的完整链路，与对象存储协议解耦——上传目标协议可后定（第 6 节），本节组件与状态机不随协议变化。

组件（独立 worker，可部署为 control-plane 内 goroutine 或独立二进制）：

    Archiver
    ├── Extractor  从 PG 按周期/游标分批读正文（限速，避免打满生产库 IO）
    ├── Compactor  写 Parquet + zstd 到本地暂存目录（parquet-go，流式）
    ├── Uploader   ArchiveStore.Put 上传（带重试/断点）
    ├── Verifier   校验：行数 + sha256 回读比对
    └── Janitor    校验通过后置空/删 PG 大字段 + 更新 manifest + 清理本地暂存

每片状态机：

    exported ─► compacted ─► uploading ─► uploaded ─► verified ─► pg_cleaned
                                     │
                                     └─► retry_queue（指数退避，超 N 次进 DLQ + 告警）

本地暂存目录（上传前的落盘缓冲，也是失败重试的持久化载体）：

    /var/lib/llm-gateway/archive/
    ├── staging/dt=2026-06-04/part-00000.parquet
    ├── staging/dt=2026-06-04/part-00000.manifest.json   # 行数/sha256/ts 范围/状态
    └── dlq/

Parquet 写入实现选型：

| 方案 | 适用 | 说明 |
|---|---|---|
| Go parquet-go（segmentio） | 增量归档（服务内长期） | 纯 Go 无 cgo、流式写、zstd 支持、与 gorm 同进程；推荐 |
| DuckDB（CLI/子进程） | 存量迁移 POC/一次性 | 直接读 PG 转 Parquet，SQL 表达，但引入外部进程与部署复杂度 |

- 增量归档用 parquet-go（服务内集成，长期稳定）；存量迁移可用 DuckDB 加速 POC。
- 压缩级别：zstd level 3 起步，实测收益递减则不再加。
- 限速：Extractor 读 PG 与 Uploader 上传均限速 + 低峰期，避免影响主链路。

## 6. 归档存储接入（默认：S3 兼容对象存储）

### 6.1 存储后端抽象

定义 ArchiveStore 接口，屏蔽对象存储协议差异：

    type ArchiveStore interface {
        Put(ctx context.Context, key string, r io.Reader, size int64) error // 上传
        Get(ctx context.Context, key string) (io.ReadCloser, error)         // 回读（懒加载）
        Exists(ctx context.Context, key string) (bool, error)               // 幂等判断
        List(ctx context.Context, prefix string) ([]ObjectInfo, error)      // 清单核对
        Delete(ctx context.Context, key string) error                       // 过期清理
    }

### 6.2 协议适配（默认实现：S3 兼容对象存储）

| 存储类型 | 实现方式 | 状态 |
|---|---|---|
| S3 兼容对象存储（MinIO / 云 OSS / COS） | aws-sdk-go-v2/service/s3（项目已依赖 aws-sdk-go-v2） | **默认** |
| NFS / SMB 挂载 | 直接以本地路径读写 | 降级备选（公司无 S3 接口时） |
| SFTP / SCP | github.com/pkg/sftp | 降级备选 |

选对象存储的原因：

- 数据模型匹配：Parquet 文件"写完只读、只追加"，与对象存储的不可变对象天然匹配，传统文件服务器的随机读写能力本场景用不到。
- 容量与可靠性：容量近乎无限、多副本/纠删码不丢数据、自动生命周期管理。
- 分析生态：DuckDB / ClickHouse 原生支持 s3:// 读取（HTTP range 按 row group 拉取，回读点查 <1s）；未来上 ClickHouse 时冷分层可与归档同源复用，零迁移。
- 实现简单：归档器只需 Put / Get / List / Delete 四个动作，SDK 自带重试与断点。

> 待确认：公司对象存储的区域、Bucket 权限、内网访问域名 / S3 endpoint（见第 12 节）。ArchiveStore 已隔离，即使最终是 NFS/SMB，也不改业务逻辑。

### 6.3 对象存储申请信息（申请单填写）

按以下值填写对象存储申请单，落地时归档器与分析的 S3 配置以此为准：

| 申请单字段 | 填写值 | 说明 |
|---|---|---|
| Bucket 名称 | llm-gateway-traces（分环境加后缀 -prod / -test） | S3 命名规范：小写字母 + 数字 + 连字符 |
| 存储类型 | **低频访问** | 长期存储、较少访问；读取实时（≠归档存储，无解冻）；省约 1/3 存储费 |
| 区域 | 与网关/分析服务同区域（内网主机房） | 生产全内网（PG 192.0.2.10 / Redis 192.0.2.10 / 镜像仓库 192.0.2.10:5000），对象存储大概率同套内网自建平台，无云 region 概念：有区域下拉选主机房区域，无则填「默认」，拿不准写"与应用生产环境同区域，请运维分配" |
| 读写权限 | **私有读写（Private）** | 正文含用户 prompt/response 敏感数据，仅服务端凭证访问，可加内网 IP 白名单 |
| 绑定域名 | 不绑定；如必填则绑内网域名 | 服务端走 s3:// endpoint 即可，不对外 |
| 协议类型 | **S3 兼容协议** | ArchiveStore / DuckDB / ClickHouse 原生支持 |
| 传输方式 | HTTPS（优先）；平台仅 HTTP 时可用，配网络隔离或应用层加密（见 §10） | 正文敏感，传输加密为默认要求；红线：公网入口禁 HTTP |
| 申请说明 | 见下方模板 | — |

申请说明模板（可直接复制）：

> 【用途】LLM Gateway 调用链路记录（call_logs/call_traces）正文归档存储，用于历史排障回读与离线分析（DuckDB）。
> 【数据规模】存量约 20~30 GB（Parquet + zstd 压缩后），每月新增约 20~30 GB；按天分区（dt=YYYY-MM-DD），单文件 100~200 MB。
> 【访问模式】归档服务每日定时写入（低频）；对账 / 正文分析 / 排障按需读取，只追加、不修改。
> 【安全要求】正文含用户 prompt/response 敏感数据，需私有读写权限、仅限内网与服务端访问、传输加密（HTTPS）。
> 【协议】S3 兼容协议，供服务端 SDK 与 DuckDB/ClickHouse 直查。
> 【存储类型】低频访问（长期存储、较少访问，控制成本）。

存储类型决策要点：低频访问读取实时（不是归档存储，无解冻）；热窗口（§7.1）兜住最近的查询，对象存储只承担热窗口之外的按需回读（GB 级/月取回量，取回费可忽略）；若上线后实测取回流量异常（月 >100GB），经生命周期策略转标准存储，零成本试错。

## 7. PG 瘦身与元数据索引

归档后 call_traces 瘦身为：

    request_id (PK) / ts / stage_auth_ms / stage_route_ms / stage_upstream_ms
    stage_ttfb_ms / stage_ttft_ms / stage_total_ms
    error_code / error_message
    task_id / documents_count          # 新独立列
    archive_ref  STRING                # 对象存储 key（如 dt=2026-06-04/part-00000.parquet）
    archive_size BIGINT                # 正文压缩后大小（可选）
    # request_body / response_body 置 NULL 或删列

新增 trace_archive_manifests 表记录每日归档元数据（文件名/行数/sha256/ts 范围/状态），供核对、重放、审计。

> request_id 现为 text 主键（211 MB），可评估改 varchar(64) 缩小；属 schema 变更，走 atlas-db-migration 流程。

### 7.1 用户历史记录回读（正文外置后的在线查询）

正文从 PG 置空后，必须保证用户在界面仍能查看历史 request/response，否则就是功能回退。这是正文外置的**必做配套**，不能省。

现状：前端「调用记录」详情走 GET /api/v1/usage/logs/:request_id/trace（usage_handler.go 的 Trace），直接从 call_traces 读正文。

外置后改为两段式回读：

1. 先读 PG 元数据（request_id / stage_* / error_* / task_id / documents_count）；
2. 若 archive_ref 非空，去对象存储按 request_id 回读正文，合并返回。

回读实现：DuckDB 点查 Parquet（SELECT request_body_raw, response_body_raw FROM read_parquet('dt=...') WHERE request_id = 'xxx'），靠 request_id 排序 + 谓词下推，单条 < 1s。

分层体验（热窗口 N 天，默认 30）：

| 数据 | 位置 | 用户点开时 |
|---|---|---|
| 最近 N 天正文 | 留在 PG | 直读，秒开 |
| 更早正文 | 对象存储 Parquet | 回读，< 1s |

可选优化：列表页展示正文前 200 字符摘要（写入时截取存小列），列表浏览零回读；只有点开看完整原文才触发回读。

## 8. 后续分析方案

压缩产物的消费方式（无需回灌 PG）：

1. DuckDB 直查（推荐，最轻）：

       SELECT model, sum(total_tokens), sum(est_cost_usd)
       FROM read_parquet('llm-gateway-traces/dt=2026-06-*/part-*.parquet')
       WHERE status = 200
       GROUP BY 1;

   支持按目录通配分区剪枝，单机即可扫几十 GB 压缩数据。

2. ClickHouse 外部表 / S3 表函数：若已有 ClickHouse，直接建表映射对象存储路径，喂给看板。
3. 按需回灌：需要历史明细进现有 PG 看板时，用 DuckDB 先过滤出所需时间片再导回，避免全量回灌。
4. ETL 引擎复用：存量迁移与后续分析用同一套 DuckDB，SQL 即分析逻辑。

交付一组分析模板 SQL：token 趋势、成本趋势、错误率、耗时 p50/p99、prompt 长度分布、按 key/dept/model 聚合。

## 9. 可靠性与容错

| 机制 | 做法 |
|---|---|
| 幂等 | 以 request_id + dt 为幂等键，重复归档不产生重复行 |
| 校验 | 每文件 sha256 + 行数；上传后回读比对，通过才置空/删 PG 大字段 |
| 断点续传 | 按天分片 + manifest 记录进度，重启从断点继续 |
| 失败队列 | 上传失败的文件进本地重试队列，指数退避；连续失败告警 |
| 水位线 | 迁移/归档期间用 max(ts) 水位线保证增量不重不漏 |
| 可回滚 | 校验不通过或中途失败时，PG 原文未动，随时回退；已删数据可从归档文件恢复 |
| 资源控制 | 归档限速、低峰期执行，避免打满生产库 IO 与对象存储带宽 |

## 10. 安全

- 正文含用户 prompt/response，属敏感数据：
  - 对象存储私有读写（仅归档服务可写、分析角色只读），访问走服务端凭证，不公开。
  - 传输加密（HTTP vs HTTPS）：
    - 对象存储平台支持 HTTPS → endpoint 一律用 https://（正文含用户 prompt/response，传输加密是默认要求）。
    - 内网自建平台仅提供 HTTP → 可用 http://，但正文敏感，需二选一兜底：① 网络隔离（私有网段 + 防火墙，仅内网服务可达）；② 开启应用层加密（见下）。
    - 禁止：走公网入口且用 HTTP。
  - 应用层加密开关（可选，安全要求高时启用）：
    - 归档器写 Parquet 前对正文两列（request_body_raw / response_body_raw）做 AES-GCM 加密，密钥由 KMS / 环境变量注入。
    - 落盘即密文：无论传输层 HTTP/HTTPS、无论对象存储是否泄露，都不泄露明文正文。
    - 影响面：结构化标量列不受影响；分析侧读正文列需解密（DuckDB 解密 UDF 或解密到临时表），正文点查回读多一次解密（µs~ms 级）。
    - 默认关闭、按需开启；成本低（AES-GCM 吞吐 GB/s 级），是对"公司要求正文加密"的最终兜底。
  - 如需在分析侧脱敏（邮箱/手机号等），在 DuckDB 查询层做视图脱敏，不改原文，满足不丢数据。
- 归档文件保留周期与合规要求对齐（默认永久，或按公司数据保留政策配置过期清理）。

## 11. 分阶段实施计划

| 阶段 | 内容 | 交付物 | 风险 |
|---|---|---|---|
| P0 POC | 取 1 天数据，验证 Parquet+zstd 压缩比、DuckDB 可读性、对象存储上传 | 实测压缩比、可用性结论 | 低 |
| P1 存量迁移 | 抽出 task_id/documents_count 独立列 → 分批导出 78 天 → 校验 → 上传 → 置空 → 回收 | 179 GB 离场，PG 瘦身 | 中（IO 压力，低峰期+限速） |
| P2 增量归档 | 上线归档 cron，正文滞留 1 天自动离场 | PG 不再增长 | 低 |
| P3 分析上线 | 部署 DuckDB 查询入口 + 分析模板 SQL | 可分析历史全量 | 低 |
| P4（可选） | 演进为直写对象存储（PG 不落正文） | 进一步减 PG 写入放大 | 中 |

## 12. 待确认输入

1. 对象存储信息：区域、Bucket 读写权限、内网访问域名 / S3 endpoint、AccessKey 托管方式（默认实现已定为 S3 兼容对象存储，见 §6.2）
2. 对象存储容量与性能：能否承载 ~20~30 GB 归档 + 后续增长？
3. 分析工具栈：用 DuckDB、ClickHouse，还是已有 BI 看板？
4. 保留与加密策略：归档保留多久？是否要求传输/落盘加密、分析侧脱敏？
5. 归档周期：增量归档默认每日一次，是否需要更细（如每小时）？
6. 热窗口天数：正文在 PG 保留多久直读（默认 30 天）？这是"体验 vs 瘦身"的权衡——N 越大用户历史直读越多，但 PG 正文占用越大（月增 ~70GB：30 天 ≈ 70GB，7 天 ≈ 16GB，1 天 ≈ 2~3GB）。
7. 存储类型：默认申请**低频访问**（读取实时、单价低，用户查历史日志走热窗口直读 + 对象存储按需回读，取回量 GB 级/月，取回费可忽略）。不选归档/深度归档（解冻分钟级，日志查询等不起）。上线后若实测取回流量异常（月 >100GB），经生命周期策略转标准存储，零成本试错。

> 确认上述输入后，本方案即可进入 P0 POC 与详细排期。
