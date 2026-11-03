# 调用链路记录「删除与迁移」详细设计（存量迁移 + 增量归档的状态机、校验、置空）

> 所属：docs/trace-archive-compression-design.md 的向下细化（P1 存量迁移 + P2 增量归档中的「删除/置空」部分）。
> 原则对齐：不丢数据、可回滚、后续可分析、改动最小。核心铁律：**先复制 → 校验 → 再删；校验不过绝不删**。
> 本文所有代码/SQL/命令保留英文，其余中文。

---

## 0. 事实锚点（与代码对齐）

| 事实 | 值 | 来源 |
|---|---|---|
| call_traces 行数 / 总占用 | ~271 万行 / 183 GB | 生产库 |
| request_body / response_body 两 jsonb 的 TOAST | ~179 GB | 生产库 |
| 数据跨度 / 起日 | 78 天 / 2026-06-04 | 生产库 |
| 月增长 | ~70 GB | 生产库 |
| 平均每行离行正文 | ~68 KB | 生产库 |
| call_logs 行数 / 占用 | ~297 万 / 3.1 GB（TOAST 仅 8 KB，可忽略） | 生产库 |
| call_traces 表结构 | request_id(text PK) / ts(timestamptz, idx_call_traces_ts) / stage_auth_ms…stage_total_ms / request_headers(jsonb) / request_body(jsonb) / response_body(jsonb) / error_code / error_message | internal/observability/model/call_trace.go |
| 正文消费点（全项目） | ① GET /api/v1/usage/logs/:request_id/trace（usage_handler.go 的 Trace，读整条 CallTrace）；② QueryLogs / ScopedLogs 的 LEFT JOIN 抽取 task_id、documents_count（rollup_service.go:643-645、usage_handler.go:880-882） | grep 全仓确认，仅此三处 |
| 写入链路 | TraceWriter 批量写 call_traces（OnConflict DoNothing），ts 为请求时间，异步 flush 存在滞后 | internal/gateway/observability/trace_event.go |

结论：**置空目标只有 call_traces 的 request_body / response_body（含 request_headers，量小但已归档可一并置空）**。call_logs.request_body / response_body（migration 045 加的，TOAST 合计 8 KB）不纳入本次置空范围。

---

## 1. 数据模型变更（migration，新编号 062）

新建 migrations/062_trace_archive.up.sql / 062_trace_archive.down.sql（走 atlas-db-migration 流程）：

~~~sql
-- 062_trace_archive.up.sql
-- 1) call_traces：正文解耦标量 + 归档指针 + 归档状态
ALTER TABLE call_traces
    ADD COLUMN IF NOT EXISTS task_id         TEXT,
    ADD COLUMN IF NOT EXISTS documents_count INT,
    ADD COLUMN IF NOT EXISTS archive_ref     TEXT,          -- 对象存储 key 前缀，如 dt=2026-06-04/
    ADD COLUMN IF NOT EXISTS archive_status  SMALLINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS archived_at     TIMESTAMPTZ;   -- 置空时间（archive_status=2 时写）

COMMENT ON COLUMN call_traces.archive_status IS
  '0=未归档(默认) 1=已归档已校验(正文仍热存PG) 2=已置空(正文离场,仅元数据+指针)';

-- Janitor 扫描用：已归档未置空，按 ts 过滤热窗口
CREATE INDEX IF NOT EXISTS idx_call_traces_archstatus_ts
    ON call_traces (archive_status, ts)
    WHERE archive_status = 1;

-- 2) 分片/每日归档元数据账本（状态机的持久化载体）
CREATE TABLE IF NOT EXISTS trace_archive_manifests (
    dt            DATE PRIMARY KEY,               -- 分区天（ct.ts::date）
    state         VARCHAR(16) NOT NULL DEFAULT 'pending',
    state_rank    SMALLINT    NOT NULL DEFAULT 0, -- 状态单调递增，见第3节
    part_files    JSONB       NOT NULL DEFAULT '[]',
    row_count     BIGINT      NOT NULL DEFAULT 0, -- 归档宽表行数（= 该天 call_traces 行数，见第4节）
    pg_row_count  BIGINT      NOT NULL DEFAULT 0, -- 导出瞬间该天 call_traces 行数（对账基准）
    sha256        TEXT,                           -- 逐 part 已有，此处可留空或存合成值
    min_ts        TIMESTAMPTZ,
    max_ts        TIMESTAMPTZ,
    uploaded_at   TIMESTAMPTZ,
    verified_at   TIMESTAMPTZ,
    cleaned_at    TIMESTAMPTZ,
    attempt       INT         NOT NULL DEFAULT 0,
    last_error    TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_manifest_state ON trace_archive_manifests (state);

-- 3) 归档配置落到 settings（可选，沿用 ops_advanced_settings 的读取方式）
--    推荐新增 key：trace_archive_settings
--    {"hot_window_days":30,"closure_lag_hours":24,"batch_rows":3000,
--     "part_target_bytes":157286400,"read_rate_limit_mb":60,"upload_rate_limit_mb":30,
--     "max_retry":5,"zstd_level":3,"staging_retention_hours":336}
~~~

~~~sql
-- 062_trace_archive.down.sql
DROP TABLE IF EXISTS trace_archive_manifests;
ALTER TABLE call_traces
    DROP COLUMN IF EXISTS archive_status,
    DROP COLUMN IF EXISTS archive_ref,
    DROP COLUMN IF EXISTS documents_count,
    DROP COLUMN IF EXISTS task_id;
~~~

part_files 元素 JSON 结构（每 part 一条）：

~~~json
{ "key": "dt=2026-06-04/part-00000.parquet", "rows": 17000,
  "sha256": "<hex>", "size_bytes": 152000000,
  "min_rid": "00000000-0000-...", "max_rid": "ffffffff-ffff-..." }
~~~

> 回滚性：migration 只加列/加表、不动既有列，down 可整体回退；正文数据在置空动作发生前始终原样在 PG。

---

## 2. 存量迁移完整流程（179 GB / 78 天）

### 2.1 分片策略决策

- **按天分片（dt = ts::date）为主**，与父文档 4.2 节的 dt=YYYY-MM-DD/ 目录布局、谓词下推、水位线对齐。
- **片内按 request_id 升序 keyset 游标切分多个 part 文件**（每 part 100~200 MB，约几千~1.7 万行），**不用全表 id 游标、不用 OFFSET 大翻页**。
- 不用「全表按 id 游标」的理由：request_id 是 UUID 文本 PK，无时间单调性，全表游标无法与 dt 分区/水位线/断点续传自然对齐；按天 + 片内 keyset 二者结合，既保证 dt 目录粒度，又保证单 part 大小可控、内存有界、可断点。
- **导出以 call_traces 为驱动表（ct LEFT JOIN cl）**，而不是父文档示意图的 cl LEFT JOIN ct。理由：置空的是 call_traces 正文，「该天归档行数 == 该天 call_traces 行数」这个对账等式在 ct 驱动下天然成立、逐行一一对应，杜绝 orphan trace（有 trace 无 log）漏归档。call_logs 维度列 LEFT JOIN 进来做富化，缺失时为 NULL（可分析性不受损——正文与 trace 元数据完整）。这是对父文档「宽表按 request_id 关联」的落地修正，特此标注。

### 2.2 每批大小与限速

- 日粒度：78 片，每片 ~34,743 行 / ~2.3 GB TOAST / ~5.7 GB 原始 JSON / 压缩后 ~250 MB。
- 片内 batch（keyset）：batch_rows 默认 **3000 行/批**（约 200 MB 原始 JSON / 约 20 MB 压缩后），逐批流式写 parquet，内存有界。
- part 目标：part_target_bytes 默认 **150 MB**（压缩后），达到即切下一个 part。
- 限速双闸：
  - 读侧 read_rate_limit_mb 默认 **60 MB/s**（读取侧令牌桶）；
  - 上传侧 upload_rate_limit_mb 默认 **30 MB/s**。
- 低峰期：迁移 runner 只在 **02:00–06:00** 窗口运行，用 days_per_night（默认 5）控制每晚处理天数。

**迁移排期估算**（可调，以 P0 POC 实测为准）：

| 档位 | days/night | 每夜读原始量 | 平均读速 | 总周期 |
|---|---|---|---|---|
| 保守 | 2 | ~11.5 GB | ~0.8 MB/s | ~39 晚（5.6 周） |
| **推荐** | **5** | **~28.7 GB** | **~2 MB/s** | **~16 晚（2.3 周）** |
| 激进 | 12 | ~69 GB | ~4.8 MB/s | ~7 晚（1 周） |

读速是「平均」，实际以令牌桶封顶；每批之间 sleep 100~500 ms 平滑 IO。上传总量每夜仅 ~1.25 GB（5×250 MB），不是瓶颈。

### 2.3 导出 SQL（片内 keyset 游标）

对第 d 天，从 cursor = '' 开始循环：

~~~sql
SELECT
  ct.ts, ct.request_id,
  -- call_logs 维度（LEFT JOIN，孤儿 trace 为 NULL）
  cl.key_id, cl.key_name, cl.key_type, cl.user_id, cl.user_name,
  cl.dept_id, cl.dept_name, cl.studio_id, cl.studio_name,
  cl.model, cl.routed_model, cl.group_name, cl.provider,
  cl.prompt_tokens, cl.cached_tokens, cl.cache_write_tokens,
  cl.completion_tokens, cl.reasoning_tokens, cl.total_tokens,
  cl.status, cl.upstream_status, cl.duration_ms, cl.first_token_ms,
  cl.est_cost_usd, cl.input_cost_usd, cl.output_cost_usd, cl.cache_read_cost_usd,
  cl.input_price_snapshot, cl.output_price_snapshot,
  cl.stage_auth_ms, cl.stage_route_ms, cl.stage_upstream_ms, cl.stage_ttfb_ms,
  cl.stream, cl.failover_used, cl.routed_via_group, cl.usage_missing,
  cl.reasoning_effort, cl.ip_address, cl.user_agent,
  cl.budget_tier_id::text AS budget_tier_id,
  cl.budget_tier_name_snapshot, cl.budget_block_code, cl.budget_block_dimension,
  -- call_traces 阶段/错误
  ct.stage_auth_ms AS ct_stage_auth_ms, ct.stage_route_ms AS ct_stage_route_ms,
  ct.stage_upstream_ms AS ct_stage_upstream_ms, ct.stage_ttfb_ms AS ct_stage_ttfb_ms,
  ct.stage_ttft_ms AS ct_stage_ttft_ms, ct.stage_total_ms AS ct_stage_total_ms,
  ct.error_code, ct.error_message,
  -- 正文（jsonb::text 保留原文；AES-GCM 开关见父文档第10节）
  ct.request_headers::text AS request_headers_raw,
  ct.request_body::text    AS request_body_raw,
  ct.response_body::text   AS response_body_raw,
  -- 高频标量（Phase A 抽列后此处可直接读列，避免每次解析大 JSON）
  COALESCE(NULLIF(substring(cl.inbound_endpoint FROM '/v1/contents/generations/tasks/([^/?#]+)'), ''),
           ct.response_body->>'id', ct.response_body->>'task_id') AS task_id,
  CASE WHEN jsonb_typeof(ct.request_body->'documents') = 'array'
       THEN jsonb_array_length(ct.request_body->'documents') ELSE NULL END AS documents_count
FROM call_traces ct
LEFT JOIN call_logs cl ON cl.request_id = ct.request_id
WHERE ct.ts >= :d AND ct.ts < :d + INTERVAL '1 day'
  AND ct.request_id > :cursor
ORDER BY ct.request_id
LIMIT :batch;
~~~

- 每批取回后：cursor = 最后一行 request_id；把行流式写进 parquet-go（zstd level 3），边写边累计行数、结束后算 part sha256。
- 写入器二选一：**生产与增量用 parquet-go（进程内流式）**；**存量一次性/POC 可用 DuckDB 读 PG 直转 parquet**（COPY 同 SELECT TO '...parquet' (FORMAT parquet, COMPRESSION zstd)，依赖 postgres 扩展）。SELECT 语义两者一致。

### 2.4 Phase A 前置：task_id / documents_count 回填

置空前必须先把两个标量抽成独立列（否则 QueryLogs/ScopedLogs 的 JOIN 在正文置空后失效）：

~~~sql
-- 增量回填（分批 autocommit，task_id IS NULL 作为游标推进标记）
UPDATE call_traces
SET task_id = sub.task_id, documents_count = sub.documents_count
FROM (
  SELECT request_id,
         COALESCE(response_body->>'id', response_body->>'task_id') AS task_id,
         CASE WHEN jsonb_typeof(request_body->'documents') = 'array'
              THEN jsonb_array_length(request_body->'documents') END AS documents_count
  FROM call_traces
  WHERE request_id > :cursor AND task_id IS NULL
  ORDER BY request_id
  LIMIT :batch
) sub
WHERE call_traces.request_id = sub.request_id;
~~~

- 写入侧同步：TraceWriter 在 traceEventToModel 里顺带算 TaskID / DocumentsCount（正文字节已在内存），后续新行天然带值。
- **跨任务依赖**：task_id 现由「endpoint 正则（call_logs 侧）→ response_body->>'id'/'task_id'」COALESCE 得出，endpoint 侧来源不在 call_traces 内。抽列后 QueryLogs 需改为优先读 ct.task_id（缺失再回退 endpoint 正则），属「正文解耦」子任务的改动点，本文只负责回填 + 置空前置，接口侧留待对齐。

---

## 3. 分片状态机

### 3.1 双循环模型（关键）

父文档 5.3 节的单链 exported→…→pg_cleaned 需拆成**两个独立循环**，因为「归档完成」与「热窗口置空」不是同一时刻：

1. **Archiver 循环（每日）**：推进到 verified（数据已安全落在对象存储），此时**只写 archive_ref / archive_status=1，不置空正文**。
2. **Janitor 循环（每日）**：对 archive_status=1 且 ts 早于热窗口的行做置空（archive_status=2），此时才真正释放 TOAST 空间。

### 3.2 状态集合与转换

| state | rank | 含义 | 进入条件 | 成功下一步 | 失败处理 |
|---|---|---|---|---|---|
| pending | 0 | 尚未处理 | 水位线推进后该 dt 进入可处理集 | 开始导出 → exporting | — |
| exporting | 1 | 从 PG 流式读、写本地 parquet | worker 领取该 dt | 全部 part 写完+sha256 → exported | 回 pending（attempt+1）；超 MaxRetry → dead |
| exported | 2 | staging 有 parquet + part 级 sha256 + 行数 | 导出完成、fsync | 本地重开校验 → compacted（见第4节） | 回 pending |
| compacted | 3 | parquet 已关闭、zstd 完成、本地 sha256 复核通过 | 本地复核通过 | 开始 PUT → uploading | 回 pending |
| uploading | 4 | 对象存储 PUT 中 | 触发上传 | PUT 成功（200/ETag）→ uploaded | retry_queue 指数退避；超限 → dead |
| uploaded | 5 | 对象已存在（幂等） | 上传成功或 Exists=true | 回读校验 → verified | 回 uploading |
| verified | 6 | 行数+sha256+抽样全过 | 校验通过（第4节） | 写 archive_ref / archive_status=1；**等待热窗口** → （Janitor 处置） | 校验不过 → pending/告警，绝不置空 |
| pg_cleaned | 7 | 该天全部待置空行已置空 | Janitor 置空完成 | 终态 | — |
| dead | 99 | 超过重试上限 | attempt > max_retry | 进 DLQ + 告警，人工介入 | — |

> exporting / compacted 在 parquet-go 流式直写路径下可合并（写即压缩），保留两态是为兼容「先 dump 再 compact」的 DuckDB 两段式。实现时二者可合一，但 state 字符串保留便于审计。

### 3.3 触发条件、失败重试、幂等保证

**触发**：Archiver 每轮（cron 每日，复用 Redis leader lock 单实例）：
1. 推进水位线（第7节）；
2. 取「dt < 水位线 且 state in (pending) 或 (exporting/uploading 且超时未心跳)」的 manifest，逐片驱动状态机；
3. 对 dead 片告警，不进自动循环。

**失败重试**：attempt 计数 + 指数退避（1min/5min/30min/2h/6h），last_error 记录；超过 max_retry（默认 5）→ dead + DLQ（staging 文件保留在 dlq/）+ 告警。上传/校验失败**只影响瘦身进度，不影响主链路**。

**幂等保证（重复执行零副作用）**：
- 导出：WHERE ts∈[d,d+1) ORDER BY request_id 确定性；重导覆盖 staging 同名 part，无 DB 副作用。
- 上传：对象 key 确定性（dt=YYYY-MM-DD/part-NNNNN.parquet），PUT 覆盖同内容；PUT 前 Exists 短路。
- 校验：重算 sha256/行数为纯读，幂等。
- manifest 推进：状态机**只前向**，用 rank 守卫：

~~~sql
UPDATE trace_archive_manifests
SET state = :new_state, state_rank = :new_rank, updated_at = NOW(),
    attempt = :attempt, last_error = :err
WHERE dt = :dt AND state_rank < :new_rank;   -- 重复/乱序推进被天然拒绝
~~~

- 写 archive_ref / archive_status（verified 提交）：archive_status = 0 守卫，重复执行匹配 0 行：

~~~sql
UPDATE call_traces
SET archive_ref = :day_prefix, archive_status = 1
WHERE ts >= :d AND ts < :d + INTERVAL '1 day'
  AND archive_status = 0;
~~~

- 置空（Janitor）：archive_status = 1 守卫，二次执行匹配 0 行（见第6节）。

---

## 4. 先复制 → 校验 → 再删（精确步骤）

对每一天分片，**校验全过才允许写 archive_status=1，Janitor 只会对 archive_status=1 的行置空**——这从机制上保证「校验不过绝不删」。

### 4.1 步骤清单（按序）

1. **快照基准**（导出前）：

~~~sql
SELECT count(*) AS pg_count, min(ts) AS min_ts, max(ts) AS max_ts
FROM call_traces WHERE ts >= :d AND ts < :d + INTERVAL '1 day';
~~~

   写入 manifest.pg_row_count / min_ts / max_ts。

2. **导出**（2.3 节 keyset 循环），逐 part 记录 rows、sha256、size、min_rid/max_rid，写 manifest.part_files。

3. **本地复核**（exported→compacted）：重开 parquet，count(*) 与逐 part rows 一致，重算 sha256 与写入时一致。

4. **上传**：逐 part Put，记录返回 ETag/size；全部成功后 state=uploaded。

5. **远端复核**（uploaded→verified）：
   - **行数校验**（远程）：DuckDB 对整天下所有 part：SELECT count(*), count(DISTINCT request_id) FROM read_parquet('llm-gateway-traces/dt=YYYY-MM-DD/*.parquet')。必须满足 count(*) == pg_row_count 且 count(DISTINCT request_id) == pg_row_count（后者抓重复行）。
   - **sha256 校验**：Get 回读每个 part 的字节流重算 sha256，与 manifest 中本地 sha256 一致；对象 size 与本地 size 一致。
   - **request_id 全量集合核对（强化项）**：导出 PG 侧该天 request_id 排序串接求一个 sha256，与 parquet 侧同样算法求值比对——一次性覆盖全部行（request_id 仅 ~36B，78 天全量 ~100 MB，比对代价可忽略）。
   - **抽样核对正文**：随机抽 K=100 个 request_id，对每个：

~~~sql
SELECT request_body::text, response_body::text
FROM call_traces WHERE request_id = :rid;
~~~

     与 parquet 中该行 request_body_raw / response_body_raw 逐字节比对（或 md5 比对），必须全等。

6. **verified 提交**：全部通过 → state=verified、verified_at=NOW()，执行 3.3 节的 archive_status=0→1 写 archive_ref。**此时正文仍在 PG，未动。**

7. **置空（延迟到热窗口）**：由 Janitor 在 ts < NOW()-N 后执行（第5/6节），与「校验」解耦。

### 4.2 校验失败的处置

- 任一校验失败：state 回 pending（或保持 uploaded 重试）、attempt+1、last_error 记录；**PG 正文与 archive_status 均未动**。
- 连续失败达 max_retry → dead + DLQ + 告警（企微/邮件，复用现有告警体系），人工介入。
- 「抽样正文不一致」视为**最高级**：立即告警并冻结该片，人工比对 PG 与 parquet（可能是导出路径 bug、字符集/转义问题、或 PG 数据在导出间隙被并发修改）。

---

## 5. 热窗口机制（默认 30 天）

### 5.1 判断方式：ts 判断 与 归档状态判断 **两者 AND**

- **不能只按 ts**：ts 早不代表正文已归档（归档可能失败/未跑到），只按 ts 置空 = 数据丢失。
- **不能只按归档状态**：会立刻置空昨天刚归档的数据，破坏「最近 N 天直读」体验，PG 也白留了 30 天窗口。
- 正确置空谓词（**同时满足才置空**）：

~~~sql
archive_status = 1          -- 已归档已校验（必要条件：正文已在对象存储）
AND ts < NOW() - make_interval(days => :hot_window_days)   -- 热窗口到期
AND (request_body IS NOT NULL OR response_body IS NOT NULL OR request_headers IS NOT NULL)
~~~

### 5.2 N 的取值与来源

- hot_window_days 默认 **30**，来自 settings 的 trace_archive_settings.hot_window_days（第1节），Janitor 每轮实时读取（改配置无需重启）。
- 权衡（父文档 12.6）：N=30 → PG 常驻 ~70 GB；N=7 → ~16 GB；N=1 → ~2-3 GB。**建议先 30，观察回读量与 PG 占用后再下调**。
- 回读体验：热窗口内正文直读 PG 秒开；窗口外走对象存储回读（8.3 节）。

---

## 6. PG 大字段置空 / DELETE

### 6.1 UPDATE SET NULL vs DELETE 老行：**选 UPDATE SET NULL（保留元数据行）**

理由：
- call_traces 行还承载 stage_*、error_*、task_id、documents_count、archive_ref，列表页与 trace 详情元数据直读依赖它；
- Trace 接口先读 PG 元数据再按 archive_ref 回读正文（两段式）；DELETE 会把元数据也删掉，详情页整条都走对象存储，退化为全量回读；
- DELETE 行还会引发 call_logs↔call_traces 的关联语义破坏（request_id 是关联键），损害「后续可分析」的元数据侧。

置空 SQL（Janitor，keyset 游标 + autocommit，见 6.2 节）：

~~~sql
UPDATE call_traces
SET request_body   = NULL,
    response_body  = NULL,
    request_headers = NULL,          -- 已归档，一并置空（量小但含敏感头）
    archive_status = 2,
    archived_at    = NOW()
WHERE request_id = ANY(:ids)
  AND archive_status = 1;            -- 幂等守卫：只置空已归档且未置空的行
~~~

> archive_status=2 之后，request_body/response_body 恒为 NULL；Trace 接口据此 + archive_ref 判断回读。error_code/error_message 保留（小字段、排障有用）。archived_at 已在 062 ALTER 中声明（第1节）。

### 6.2 批量策略（死元组 / 长事务 / 锁）

- **绝不单条大 UPDATE**（WHERE ts < :cutoff 一次到位）——会长事务、海量死元组、锁长时间持有、WAL 暴涨。
- Janitor 每轮 keyset 循环：

~~~sql
-- 找下一批（游标推进）
SELECT request_id
FROM call_traces
WHERE archive_status = 1
  AND ts < NOW() - make_interval(days => :hot_window_days)
  AND request_id > :cursor
ORDER BY request_id
LIMIT :batch;           -- batch 默认 1000
~~~

- 逐批执行置空 UPDATE（上面），**每批独立 autocommit**，批间 sleep 100~500 ms。
- 批大小：默认 **1000 行/批**（每行 ~68 KB TOAST → 每批回收 ~68 MB TOAST）。可按观测调 500~2000。
- 锁：按 PK（request_id）点更新，只有行锁，无表锁；不与 TraceWriter 批量 INSERT 冲突（不同行）。置空与归档读同一张表，但归档只读已关闭天、Janitor 只动 ts < 热窗口的旧行，天然错峰。
- 长事务：每批事务 < 1s；杜绝跨批持有事务。

### 6.3 VACUUM 与 pg_repack（磁盘回收时机与方式）

置空后磁盘**不会自动变小**（TOAST 死块只被标记，表/TOAST 文件高水位不降）：

1. **迁移进行中**：每批置空后不急着收空间；依赖 autovacuum（或每日一次 VACUUM (ANALYZE) call_traces）复用死元组。此时磁盘占用仍 ~183 GB，属预期。
2. **全部置空完成后（一次性回收，维护窗口）**：
   - **首选 pg_repack（在线，不长时间阻塞写入）**，处理表 + 其 TOAST：

~~~bash
pg_repack -t call_traces -d $DB_NAME --jobs 4
# 回收后统计信息刷新
psql -c "VACUUM (ANALYZE) call_traces;"
~~~

   - 无 pg_repack 时用 VACUUM FULL call_traces（**独占锁**，会阻塞 TraceWriter 插入数小时，须在深夜 + 知会 + 缩短 TraceWriter 缓冲前做），风险高，仅作兜底。
   - **磁盘余量要求**：pg_repack / VACUUM FULL 都需约等于「待回收表体积」的临时空间（~183 GB），事前 df 确认；不足则分批 repack（按 dt 做表分区改造时再议，不在本方案范围）。
3. 期望结果：call_traces 主体 + TOAST 收缩到数 GB，与父文档第7节目标一致。

---

## 7. 水位线（不漏不重、与增量归档衔接）

### 7.1 定义

- **关闭滞后 L（closure lag）**：TraceWriter 异步 flush，请求 23:59:59 的 trace 可能 00:00:05 才落库；且 call_logs 走 Redis spool 也有落库滞后。故「某天已完全落库」要等到该天结束后再过 L。默认 closure_lag_hours = 24（即一天后视为关闭）。
- **水位线**：waterline_dt = (今天 00:00) - L。含义：ts < waterline_dt 的行属于「已关闭、可安全归档的存量」，其后再无新行落入该区间。

### 7.2 不漏不重的区间划分（闭开区间）

- 存量迁移：导出 ts ∈ [min_dt, waterline_dt)，逐天闭开 [d, d+1)。
- 增量归档：第一个周期从 waterline_dt 起，之后每轮归档「上一个已关闭天」。增量归档与迁移的窗口**在 waterline_dt 处无缝拼接、互不重叠**。
- 边界日（waterline_dt 当天）**完全留给增量归档**，迁移不碰——避免「迁移导出后才写入的同日新行」被漏掉。

### 7.3 防重防漏的双保险

- **防漏**：迁移按闭区间覆盖全部 ts < waterline_dt；导出以 ct 驱动，count(*) 对账以 pg_row_count（导出瞬间快照）为准，任何漏读都会导致行数不齐 → 校验失败 → 不置空。
- **防重**：strict 闭开 [d, d+1) + archive_status=0 提交守卫 + state_rank 单调守卫，同一 dt 不会被二次导出/二次提交。
- **迁移期间的新写入**：新行 ts ≥ waterline_dt，archive_status=0，由增量归档负责；Janitor 只动 archive_status=1 且 ts < 热窗口，对未归档新行零触及。

---

## 8. 回滚与失败处理

### 8.1 归档/上传失败时 PG 原文不动

- 导出是纯读；上传/校验失败只改 manifest（state/attempt/last_error），**从不写 archive_ref、从不置空**。
- archive_status 从 0→1 的唯一入口是「verified 提交」，而 verified 以全部校验通过为前提。因此**任何归档环节失败，PG 原文与 archive_status 都纹丝不动**，可随时回退/重试，零数据风险。

### 8.2 archive_ref 与置空的配合时序（先指针后置空）

严格顺序（两阶段，中间存在安全窗口）：

1. **T1 verified 提交**：写 archive_ref（对象 key）+ archive_status=1。此刻正文仍在 PG。
2. **T2 热窗口到期**：Janitor 置空正文 + archive_status=2。此刻正文已安全在对象存储。

保证：**「正文消失」永远晚于「指针出现」**，不存在「正文没了但指针缺失」的窗口；Trace 接口只要看到 request_body IS NULL AND archive_ref IS NOT NULL 就走回读，回读必命中。

### 8.3 回读接口（两段式）

GET /api/v1/usage/logs/:request_id/trace 改造：

~~~go
// usage_handler.go Trace（伪代码，与父文档 7.1 节一致）
var trace obsmodel.CallTrace
h.db.First(&trace, "request_id = ?", rid)            // 1) PG 元数据
if (trace.RequestBody == nil || trace.ResponseBody == nil) && trace.ArchiveRef != "" {
    body := h.archiveStore.ReadBody(ctx, trace.ArchiveRef, rid)  // 2) 对象存储点查
    // DuckDB: SELECT request_body_raw, response_body_raw
    //         FROM read_parquet('llm-gateway-traces/'||archive_ref||'*.parquet')
    //         WHERE request_id = :rid
    trace.RequestBody, trace.ResponseBody = body.Req, body.Resp
}
c.JSON(200, trace)
~~~

- archive_ref 存到**天目录前缀**（dt=YYYY-MM-DD/），回读时 DuckDB 谓词下推 + 行组剪枝，单条 <1s。
- **置空后回滚**：PG 已无原文，回滚只能从对象存储/本地 staging 回灌。兜底：
  - staging 本地 parquet 保留 staging_retention_hours（默认 336h=14 天）后才删，作为置空后的第二副本；
  - 对象存储开启**版本控制/删除保护**，防止误删 object；
  - 真正「DELETE 行」只在「对象存储可读性已确认 + 本地 staging 已过保留期」之后才考虑（本方案不主动 DELETE 行，见 6.1 节）。

---

## 9. 现状 bug 修复

### 9.1 ops_cleanup_service.go 的 cron 配置 bug

现状（internal/controlplane/service/ops_cleanup_service.go:78-86）：

~~~go
s.cron = cron.New(cron.WithSeconds())          // WithSeconds → 解析器要求 6 字段
_, err := s.cron.AddFunc(s.cronSchedule, ...)  // "0 3 * * *" 只有 5 字段
if err != nil {
    slog.Error(...); return                    // AddFunc 报错 → return → Start 提前退出
}
~~~

后果：AddFunc("0 3 * * *") 在 WithSeconds 下抛「expected exactly 6 fields」→ 记录 error 后 return，**cron.Start() 与后续 slog.Info 都执行不到，清理 cron 实际从未启动**（这也能解释 78 天数据为何毫无清理）。

修复（**方案 A，推荐**：去掉 WithSeconds，用标准 5 字段）：

~~~go
func (s *OpsCleanupService) Start(ctx context.Context) {
    s.cron = cron.New()                        // 标准 5 字段：min hour dom mon dow
    _, err := s.cron.AddFunc(s.cronSchedule, func() { s.runCleanup(ctx) })
    if err != nil {
        slog.Error("ops_cleanup: failed to add cron job", "error", err)
        return
    }
    s.cron.Start()
    ...
}
~~~

- 方案 B（保留 WithSeconds，改 6 字段）：defaultCleanupSchedule = "0 0 3 * * *"（秒=0 分=0 时=3）。仅当未来确实需要秒级调度才选 B。
- **建议同时把 Start 的静默 return 改为显式失败**：func (s *OpsCleanupService) Start(ctx) error，AddFunc 失败返回 error，由调用方（router.go:569）处理/告警，避免再次「配错却静默不跑」。
- 配套：SetSchedule 目前生产无人调用（grep 仅测试调用），可在 router 中从 settings 读取 ops_advanced_settings.cleanup_schedule（默认 "0 3 * * *"）注入，使调度可配。

### 9.2 cleanupCallLogs 不清理 call_traces

现状：cleanupCallLogs 只 DELETE FROM call_logs WHERE ts < NOW()-30d，**从不碰 call_traces**——call_traces 无限增长正是 179 GB 的根因。

修复方向（**不是简单加一句 DELETE，否则直接丢正文**）：

1. **把 call_traces 生命周期纳入新归档体系**：call_traces 的「清理」= 第6节的「置空」（先归档再置空），由 Janitor 承担；cleanupCallLogs 继续只负责 call_logs（其自身 30 天保留不变）。
2. **时序耦合（重要）**：call_logs 30 天后会被 DELETE，但归档宽表要 JOIN call_logs 维度。因此：
   - 每日归档（导出「昨天」）**必须先于** cleanupCallLogs（删「30 天前」）——两者天然满足（1 天 vs 30 天的 29 天缓冲），但**修复 cron bug 后 cleanup 会真正开始删 call_logs**，须与归档上线同版本、归档 cron 先跑。
   - **存量迁移要赶在 cleanup 生效前**：当前因为 cron bug，78 天的 call_logs 恰好都还在库里。存量迁移应**在修复 cron bug 之前（或修复但暂时停用 cleanup 期间）**完成导出，把 78 天 call_logs 维度一并纳入宽表；否则修复后 >30 天的 call_logs 维度被删，宽表只能拿到孤儿 trace（正文仍完整，但分析维度缺失）。
3. 归档宽表对「call_logs 维度缺失（>30 天被删）」的容错：正文与 trace 元数据不受影响；task_id / documents_count 已抽到 call_traces 列，不依赖 call_logs 存活。

**建议上线顺序**：① P0 POC → ② 存量迁移（含 Phase A 抽列）跑完 → ③ 增量归档上线 → ④ 最后修复 cron bug 并启用 cleanupCallLogs（或 ④ 与 ③ 同版本、但确认 ③ 归档先于 cleanup 执行）。

---

## 10. 接口签名与配置（Go 落地骨架）

~~~go
// internal/archive/（control-plane 内独立 worker 包，复用 OpsCleanupService 的
// Redis leader lock 单实例模式）

type ShardState string
const (
    ShardPending   ShardState = "pending"
    ShardExporting ShardState = "exporting"
    ShardExported  ShardState = "exported"
    ShardCompacted ShardState = "compacted"
    ShardUploading ShardState = "uploading"
    ShardUploaded  ShardState = "uploaded"
    ShardVerified  ShardState = "verified"
    ShardCleaned   ShardState = "pg_cleaned"
    ShardDead      ShardState = "dead"
)

// 状态单调 rank（与 manifest.state_rank 一致，防状态回退）
func (s ShardState) Rank() int { /* 0..7, dead=99 */ }

type ArchiveConfig struct {
    HotWindowDays        int           // 默认 30
    ClosureLag           time.Duration // 默认 24h
    BatchRows            int           // 导出/置空批大小，默认 3000 / 1000
    PartTargetBytes      int64         // 默认 150 MiB
    ReadRateLimitMB      int           // 默认 60
    UploadRateLimitMB    int           // 默认 30
    MaxRetry             int           // 默认 5
    ZstdLevel            int           // 默认 3
    StagingDir           string        // /var/lib/llm-gateway/archive/staging
    StagingRetention     time.Duration // 默认 336h
    DaysPerNight         int           // 存量迁移每晚片数，默认 5
    LowPeakStart         string        // "02:00"
    LowPeakEnd           string        // "06:00"
}

type ManifestLedger interface {           // trace_archive_manifests 读写
    Upsert(ctx context.Context, m *TraceArchiveManifest) error
    Get(ctx context.Context, dt time.Time) (*TraceArchiveManifest, error)
    AdvanceState(ctx context.Context, dt time.Time, from, to ShardState) (bool, error)
    ListByState(ctx context.Context, states ...ShardState) ([]TraceArchiveManifest, error)
}

type Archiver struct { db *gorm.DB; store ArchiveStore; cfg ArchiveConfig; ledger ManifestLedger }
func (a *Archiver) RunCycle(ctx context.Context) error   // 一轮：推进水位线→逐 dt 状态机
func (a *Archiver) exportShard(ctx context.Context, dt time.Time) error  // 2.3 节 keyset 循环 → exported
func (a *Archiver) uploadShard(ctx context.Context, dt time.Time) error  // Put 各 part → uploaded
func (a *Archiver) verifyShard(ctx context.Context, dt time.Time) error  // 第4节五项校验 → verified
func (a *Archiver) commitRef(ctx context.Context, dt time.Time) error    // archive_status 0→1

type Janitor struct { db *gorm.DB; cfg ArchiveConfig }
func (j *Janitor) NullExpired(ctx context.Context) (nulled int64, err error) // 第6节 keyset 置空
~~~

ArchiveStore 接口沿用父文档 6.1 节（Put/Get/Exists/List/Delete），ReadBody 回读封装其上。

---

## 11. 风险与待确认

| 风险/依赖 | 处置 |
|---|---|
| 对象存储 endpoint/凭证/Bucket 未定 | 父文档 12.1；ArchiveStore 已隔离，不影响本文置空/校验逻辑 |
| 实际压缩比、单日行数分布 | P0 POC 取 1 天实测，校准 batch/part/days_per_night |
| 磁盘余量不足 pg_repack | 事前 df 确认 ≥183 GB 余量，否则分表 repack 或加盘 |
| 置空后回读延迟/成本 | 热窗口兜底 + DuckDB 点查 <1s；上线后观测取回流量，超 100 GB/月转标准存储 |
| task_id 的 endpoint 侧来源抽列后需改 QueryLogs | 跨任务（正文解耦）对齐点，见 2.4 节 |
| cleanup 修复后开始删 call_logs，与归档的时序 | 严格按 9.2 节上线顺序，归档先于 cleanup |

---

## 附：一键检查命令（迁移前/中/后）

~~~sql
-- 迁移前：确认目标规模与基准
SELECT count(*), pg_size_pretty(sum(pg_column_size(request_body)+pg_column_size(response_body)))
FROM call_traces;
SELECT pg_size_pretty(pg_total_relation_size('call_traces'));

-- 迁移中：看某天归档进度
SELECT dt, state, row_count, pg_row_count, attempt FROM trace_archive_manifests
WHERE dt = '2026-06-04';

-- 置空后：看已置空规模与剩余
SELECT archive_status, count(*),
       pg_size_pretty(sum(pg_column_size(request_body)+pg_column_size(response_body)))
FROM call_traces GROUP BY archive_status;

-- 迁移后：回收效果（<toast_oid> 用 \d+ call_traces 查 pg_toast 表 OID）
SELECT pg_size_pretty(pg_total_relation_size('call_traces')),
       pg_size_pretty(pg_relation_size('pg_toast.pg_toast_<toast_oid>'));
~~~
