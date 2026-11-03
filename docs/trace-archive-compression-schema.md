# 调用链路记录压缩归档 —— 压缩侧详细设计（Parquet 宽表 schema / 分区 / 写入批处理 / 实现选型）

> 状态：详细设计（向下细化 docs/trace-archive-compression-design.md，不替代框架文档）
> 范围：call_logs ∪ call_traces 按 request_id 关联成的 Parquet 宽表，以及分区、写入批处理、Go/DuckDB 选型、zstd 级别、schema 演进、P0 POC。
> 读者：实现压缩归档（Archiver / Compactor）与存量迁移的工程师。
> 事实对齐文件：internal/observability/model/call_log.go、internal/observability/model/call_trace.go、internal/gateway/observability/trace_event.go、internal/observability/handler/usage_handler.go（ScopedLogs 的 task_id / documents_count 抽取）、internal/gateway/middleware/trace_capture.go（正文 sanitize）。

---

## 0. 关键事实与前置判断（实现前必须对齐）

1. “不丢数据”的精确含义 = 不丢 PG 里现存的数据。call_traces.request_body / response_body 里的 JSON 不是线上原始字节：
   - 请求体经 sanitizeJSONRequestBody 两层改写：base64 data-URI 超过 64 字符被替换为大小占位符；每个 string 字段超过 2048 字符被截断为「首 35% + 尾 65% + 截断标记」（见 trace_capture.go）。
   - jsonb 里的 NUL 转义（U+0000）被 scrubNULEscapes 替换为 U+FFFD（见 trace_event.go）。
   - 结论：归档只承诺对 PG 现存的 jsonb 内容逐字无损（jsonb::text 原样落盘）；被 sanitize 截断前的原始字节在 PG 中已不可恢复。文档与排障口径要写清楚这一点，避免「归档能还原原始 prompt」的误期。

2. task_id 的真实抽取是三源 COALESCE，不是框架文档 §2 写的两源（以 usage_handler.go ScopedLogs / rollup_service.go QueryLogs 为准）：

   COALESCE(
     NULLIF(substring(call_logs.inbound_endpoint FROM '/v1/contents/generations/tasks/([^/?#]+)'), ''),
     ct.response_body->>'id',
     ct.response_body->>'task_id'
   ) AS task_id

   优先级：① 视频/Seedance 的 task id（从 URL 正则抠）→ ② response_body.id → ③ response_body.task_id。

3. 正文消费点全项目仅两处（框架文档已核实）：单条 trace 详情 GET /api/v1/usage/logs/:request_id/trace（整条正文回读）与日志列表 LEFT JOIN（只抽 task_id + documents_count）。本文新增的 messages_count / finish_reason / has_tool_calls / prompt_chars / response_chars 是新分析列，不是现状已有消费，抽取表达式由本设计给出。

4. JOIN 语义：以 call_traces 为驱动表做 LEFT JOIN（`ct LEFT JOIN cl`；框架文档写的「call_logs ∪ call_traces」在此收敛为以 trace 为主）。call_traces 由 TraceWriter 独立写入（OnConflict{DoNothing}），call_logs 走 Redis spool 批量落库，二者存在极小概率不对称（有 trace 无 log / 有 log 无 trace）。为保证正文不丢：孤儿 trace 的 cl 维度为 NULL 也照常归档；孤儿 log 本无正文，留在 PG call_logs 表、不归档。request_id / ts 以 ct 为准，cl 维度按需 COALESCE 或留 NULL。

5. Parquet 侧时间戳自控：ts 不用库默认编码，显式存 int64 微秒（Unix epoch, UTC）并标注 TIMESTAMP(isAdjustedToUTC=true, unit=MICROS)。dt 存 STRING 'YYYY-MM-DD'。理由见 §1.1 与 §2（避免 parquet-go 各版本对 time.Time 的 INT96 / NANOS 分歧，读写两端都无歧义）。

---

## 1. 宽表完整 schema

### 1.1 命名与类型约定

| 约定 | 取值 | 说明 |
|---|---|---|
| 列名 | 与 PG 字段同名（snake_case） | 读侧 SQL 直觉一致 |
| 物理类型 | BYTE_ARRAY / INT32 / INT64 / BOOLEAN / DOUBLE | 时间见下 |
| 时间列 ts | INT64 物理 + TIMESTAMP(MICROS,UTC) logical | 自控，见上 |
| 分区列 dt | STRING（BYTE_ARRAY），值 YYYY-MM-DD | 与 Hive 目录键同名同值 |
| 编码 | 低基数串=PLAIN_DICTIONARY；高基数串/raw=PLAIN；布尔=RLE；整数=PLAIN | 见每列 |
| 压缩 codec | 全列 ZSTD（默认 level 3，可配） | 小整数/布尔列可选 SNAPPY，默认不折腾 |
| 可空 | 与 Go model 的指针 / not null 对齐 | NULL 用 Parquet optional 定义级 |

字典编码阈值：字典只用于「去重后唯一值占比低」的列；若某列字典页超过约 1MB（唯一值过多），writer 自动退回 PLAIN。request_id / user_id / key_id / dept_id / studio_id / 正文 raw 等强制 PLAIN（唯一值近行数，字典反而膨胀内存）。

### 1.2 逐列清单

来源列标注：cl. = call_logs，ct. = call_traces，COALESCE = 两表同名/同义字段取 trace 优先，— = 抽取派生。

#### (A) 分区 / 时间

| 列名 | Parquet 类型(logical) | 编码 | codec | 可空 | 来源 / 表达式 |
|---|---|---|---|---|---|
| dt | STRING YYYY-MM-DD | PLAIN | ZSTD | 否 | (ts AT TIME ZONE '<tz>')::date，见 §2 |
| ts | INT64 / TIMESTAMP(MICROS,UTC) | PLAIN | ZSTD | 否 | COALESCE(cl.ts, ct.ts) |

#### (B) 主键 / 维度（过滤、分组，高基数为主）

| 列名 | Parquet 类型(logical) | 编码 | codec | 可空 | 来源 / 表达式 |
|---|---|---|---|---|---|
| request_id | STRING | PLAIN | ZSTD | 否 | COALESCE(cl.request_id, ct.request_id)（排序键，见 §2） |
| key_id | STRING | PLAIN | ZSTD | 是 | cl.key_id |
| key_name | STRING | DICT | ZSTD | 是 | cl.key_name |
| key_type | STRING | DICT | ZSTD | 否 | cl.key_type（default 'personal'） |
| user_id | STRING | PLAIN | ZSTD | 是 | cl.user_id |
| user_name | STRING | DICT | ZSTD | 是 | cl.user_name |
| dept_id | STRING | PLAIN | ZSTD | 是 | cl.dept_id |
| dept_name | STRING | DICT | ZSTD | 是 | cl.dept_name |
| studio_id | STRING | PLAIN | ZSTD | 是 | cl.studio_id |
| studio_name | STRING | DICT | ZSTD | 是 | cl.studio_name |
| model | STRING | DICT | ZSTD | 否 | cl.model（LENGTH(model)<=128 过滤脏数据后仍落盘原始值） |
| routed_model | STRING | DICT | ZSTD | 是 | cl.routed_model |
| group_name | STRING | DICT | ZSTD | 是 | cl.group_name |
| provider | STRING | DICT | ZSTD | 是 | cl.provider |
| inbound_endpoint | STRING | DICT | ZSTD | 是 | cl.inbound_endpoint（task_id 抽取的 URL 来源） |
| reasoning_effort | STRING | DICT | ZSTD | 是 | cl.reasoning_effort |
| ip_address | STRING | PLAIN | ZSTD | 是 | cl.ip_address |
| user_agent | STRING | PLAIN | ZSTD | 是 | cl.user_agent |
| budget_tier_id | STRING（uuid 文本） | DICT | ZSTD | 是 | cl.budget_tier_id::text（NULL 保持 NULL） |
| budget_tier_name_snapshot | STRING | DICT | ZSTD | 是 | cl.budget_tier_name_snapshot |
| budget_block_code | STRING | DICT | ZSTD | 是 | cl.budget_block_code |
| budget_block_dimension | STRING | DICT | ZSTD | 是 | cl.budget_block_dimension |
| budget_block_scope_type | STRING | DICT | ZSTD | 是 | cl.budget_block_scope_type |
| budget_block_scope_id | STRING | PLAIN | ZSTD | 是 | cl.budget_block_scope_id |
| budget_block_period_type | STRING | DICT | ZSTD | 是 | cl.budget_block_period_type |

#### (C) 指标（聚合）

| 列名 | Parquet 类型(logical) | 编码 | codec | 可空 | 来源 / 表达式 |
|---|---|---|---|---|---|
| status | INT32 | PLAIN | ZSTD | 是 | cl.status |
| upstream_status | INT32 | PLAIN | ZSTD | 是 | cl.upstream_status |
| prompt_tokens | INT64 | PLAIN | ZSTD | 否 | cl.prompt_tokens |
| cached_tokens | INT64 | PLAIN | ZSTD | 否 | cl.cached_tokens |
| cache_write_tokens | INT64 | PLAIN | ZSTD | 否 | cl.cache_write_tokens |
| completion_tokens | INT64 | PLAIN | ZSTD | 否 | cl.completion_tokens |
| reasoning_tokens | INT64 | PLAIN | ZSTD | 否 | cl.reasoning_tokens |
| total_tokens | INT64 | PLAIN | ZSTD | 否 | cl.total_tokens |
| duration_ms | INT64 | PLAIN | ZSTD | 是 | cl.duration_ms |
| first_token_ms | INT64 | PLAIN | ZSTD | 是 | cl.first_token_ms |
| stage_auth_ms | INT32 | PLAIN | ZSTD | 是 | COALESCE(ct.stage_auth_ms, cl.stage_auth_ms) |
| stage_route_ms | INT32 | PLAIN | ZSTD | 是 | COALESCE(ct.stage_route_ms, cl.stage_route_ms) |
| stage_upstream_ms | INT32 | PLAIN | ZSTD | 是 | COALESCE(ct.stage_upstream_ms, cl.stage_upstream_ms) |
| stage_ttfb_ms | INT64 | PLAIN | ZSTD | 是 | COALESCE(ct.stage_ttfb_ms, cl.stage_ttfb_ms) |
| stage_ttft_ms | INT64 | PLAIN | ZSTD | 是 | ct.stage_ttft_ms（仅 call_traces 有） |
| stage_total_ms | INT64 | PLAIN | ZSTD | 是 | ct.stage_total_ms（仅 call_traces 有） |
| input_cost_usd | DOUBLE | PLAIN | ZSTD | 否 | cl.input_cost_usd |
| output_cost_usd | DOUBLE | PLAIN | ZSTD | 否 | cl.output_cost_usd |
| cache_read_cost_usd | DOUBLE | PLAIN | ZSTD | 否 | cl.cache_read_cost_usd |
| est_cost_usd | DOUBLE | PLAIN | ZSTD | 否 | cl.est_cost_usd |
| input_price_snapshot | DOUBLE | PLAIN | ZSTD | 是 | cl.input_price_snapshot |
| output_price_snapshot | DOUBLE | PLAIN | ZSTD | 是 | cl.output_price_snapshot |

> 金额精度说明：PG 侧是 decimal(18,8)/(18,6)，Go model 已是 float64。Parquet 用 DOUBLE（分析够用、无 DECIMAL 逻辑类型兼容负担）。若未来要求「金额分毫不差」，演进路径见 §6（加 est_cost_usd_dec DECIMAL 列），不与现列冲突。

#### (D) 布尔

| 列名 | Parquet 类型(logical) | 编码 | codec | 可空 | 来源 / 表达式 |
|---|---|---|---|---|---|
| stream | BOOLEAN | RLE | ZSTD | 否 | cl.stream |
| failover_used | BOOLEAN | RLE | ZSTD | 否 | cl.failover_used |
| routed_via_group | BOOLEAN | RLE | ZSTD | 否 | cl.routed_via_group |
| usage_missing | BOOLEAN | RLE | ZSTD | 否 | cl.usage_missing |
| budget_tier_accounting_eligible | BOOLEAN | RLE | ZSTD | 否 | cl.budget_tier_accounting_eligible |

#### (E) 预算约束快照（jsonb 原样）

| 列名 | Parquet 类型(logical) | 编码 | codec | 可空 | 来源 / 表达式 |
|---|---|---|---|---|---|
| budget_tier_constraints_snapshot | STRING | PLAIN | ZSTD | 否 | cl.budget_tier_constraints_snapshot::text（jsonb 原样，默认 '[]'） |

#### (F) 错误信息（仅 call_traces）

| 列名 | Parquet 类型(logical) | 编码 | codec | 可空 | 来源 / 表达式 |
|---|---|---|---|---|---|
| error_code | STRING | DICT | ZSTD | 是 | ct.error_code |
| error_message | STRING | PLAIN | ZSTD | 是 | ct.error_message |

#### (G) 正文 raw（大字段，zstd 压缩；“原文”= PG 现存 jsonb 逐字，见 §0.1）

| 列名 | Parquet 类型(logical) | 编码 | codec | 可空 | 来源 / 表达式 |
|---|---|---|---|---|---|
| request_headers_raw | STRING | PLAIN | ZSTD | 是 | ct.request_headers::text（已过滤 Authorization/Cookie/Set-Cookie/X-Api-Key） |
| request_body_raw | STRING | PLAIN | ZSTD | 是 | ct.request_body::text |
| response_body_raw | STRING | PLAIN | ZSTD | 是 | ct.response_body::text |

> 这三个 raw 列用 PLAIN（不打字典）：正文内容几乎逐行唯一，字典只会浪费内存且无收益。zstd 在页级仍能压缩重复 key / 模板片段，这正是压缩大头（见 §5）。

#### (H) 正文抽取标量（高频分析列，独立成列避免每次解析大 JSON）

| 列名 | Parquet 类型(logical) | 编码 | codec | 可空 | 来源 / 表达式 |
|---|---|---|---|---|---|
| task_id | STRING | DICT | ZSTD | 是 | 见 §1.3 表达式 ① |
| documents_count | INT32 | PLAIN | ZSTD | 是 | 见 §1.3 表达式 ② |
| messages_count | INT32 | PLAIN | ZSTD | 是 | 见 §1.3 表达式 ③ |
| finish_reason | STRING | DICT | ZSTD | 是 | 见 §1.3 表达式 ④ |
| has_tool_calls | BOOLEAN | RLE | ZSTD | 否 | 见 §1.3 表达式 ⑤ |
| prompt_chars | INT64 | PLAIN | ZSTD | 是 | 见 §1.3 表达式 ⑥ |
| response_chars | INT64 | PLAIN | ZSTD | 是 | 见 §1.3 表达式 ⑦ |

### 1.3 正文抽取标量——精确表达式（PG jsonb 语法，抽取在 SQL 侧完成）

① task_id（三源 COALESCE，与 usage_handler 一致）：

    COALESCE(
      NULLIF(substring(cl.inbound_endpoint FROM '/v1/contents/generations/tasks/([^/?#]+)'), ''),
      ct.response_body->>'id',
      ct.response_body->>'task_id'
    ) AS task_id

② documents_count（仅当 request_body.documents 是数组）：

    CASE WHEN jsonb_typeof(ct.request_body->'documents') = 'array'
         THEN jsonb_array_length(ct.request_body->'documents')
         ELSE NULL END AS documents_count

③ messages_count（仅 /v1/chat/completions 有顶层 messages；Anthropic /v1/messages、/v1/responses、/v1/embeddings、/v1/rerank、视频无此字段 → NULL）：

    CASE WHEN jsonb_typeof(ct.request_body->'messages') = 'array'
         THEN jsonb_array_length(ct.request_body->'messages')
         ELSE NULL END AS messages_count

④ finish_reason（响应侧 OpenAI chat.completion 结构；流式响应由 stream_usage_observer 重组为同构合成 JSON）：

    CASE WHEN jsonb_typeof(ct.response_body->'choices') = 'array'
         AND jsonb_array_length(ct.response_body->'choices') > 0
         THEN ct.response_body->'choices'->0->>'finish_reason'
         ELSE NULL END AS finish_reason

⑤ has_tool_calls（请求侧任一 message 带非空 tool_calls 或 role='tool'；或响应侧 choices[0].message.tool_calls 非空数组）：

    CASE WHEN
      (jsonb_typeof(ct.request_body->'messages') = 'array' AND EXISTS (
         SELECT 1 FROM jsonb_array_elements(ct.request_body->'messages') AS m
         WHERE (m ? 'tool_calls' AND jsonb_typeof(m->'tool_calls') = 'array'
                AND jsonb_array_length(m->'tool_calls') > 0)
            OR m->>'role' = 'tool'
      ))
      OR (jsonb_typeof(ct.response_body->'choices'->0->'message'->'tool_calls') = 'array'
          AND jsonb_array_length(ct.response_body->'choices'->0->'message'->'tool_calls') > 0)
    THEN true ELSE false END AS has_tool_calls

⑥ prompt_chars / ⑦ response_chars（sanitize 后 JSON 文本字符数，见 §0.1 口径）：

    char_length(ct.request_body::text)  AS prompt_chars,   -- 或 octet_length()，按字节则更贴近物理大小
    char_length(ct.response_body::text) AS response_chars

> 这俩长度列属于冗余降本列：读侧其实可 char_length(request_body_raw) 现算，但那样必须拉取大列；独立成列后「prompt 长度分布」查询完全不碰 body 列。DuckDB 侧等价写法：char_length(request_body_raw)（raw 已落成 STRING，无需再 ::text）。

### 1.4 关联与导出 SELECT（骨架）

    SELECT
      (COALESCE(ct.ts, cl.ts) AT TIME ZONE 'Asia/Shanghai')::date::text AS dt,
      COALESCE(ct.ts, cl.ts) AS ts,
      COALESCE(ct.request_id, cl.request_id) AS request_id,  -- ct 为驱动表，等价于 ct.request_id
      -- ... §1.2 (B)(C)(D)(E) 各列：cl.* / ct.* / COALESCE(...) ...
      ct.request_headers::text AS request_headers_raw,
      ct.request_body::text     AS request_body_raw,
      ct.response_body::text    AS response_body_raw,
      -- 抽取标量：§1.3 ①~⑦
    FROM call_traces ct
    LEFT JOIN call_logs cl ON cl.request_id = ct.request_id
    WHERE ct.ts >= :d AND ct.ts < :d_next
    ORDER BY ct.request_id ASC

> **驱动表裁决（captain 对齐）**：导出以 call_traces 为驱动表、call_logs 仅 LEFT JOIN 补充维度（孤儿 trace 的 cl 维度为 NULL 也照常归档）。理由：归档对象是 call_traces 正文，对账等式「归档行数 == 当天 call_traces 行数」天然成立；孤儿 log（有 log 无 trace）本无正文可归档，留在 PG call_logs 表即可。故不用 FULL OUTER JOIN。

---

## 2. 分区与文件切分

### 2.1 目录布局（Hive 风格）

    s3://llm-gateway-traces/
    ├── dt=2026-06-04/
    │   ├── part-00000.parquet      # 150MB（压缩后）
    │   ├── part-00001.parquet
    │   └── _SUCCESS
    ├── dt=2026-06-05/ ...
    └── _manifest/
        ├── manifest-2026-06-04.json
        └── manifest-2026-06-05.json

### 2.2 分区键 dt 的时区口径（必须定死）

- dt 用固定业务时区的日期，默认 Asia/Shanghai，配置常量 ARCHIVE_TZ。
- 现有看板/列表用 ts::date（依赖 PG 服务器时区），为对齐口径，ARCHIVE_TZ 必须与生产 PG 的 timezone 设置一致（待运维确认；不一致则「按天」统计错一天）。
- SQL 侧统一 (ts AT TIME ZONE 'Asia/Shanghai')::date，不用 ts::date（后者受连接时区影响，DuckDB 读 PG 时尤其容易漂）。

### 2.3 part 文件与 row group 切分

| 项 | 建议值 | 依据 |
|---|---|---|
| part 文件（压缩后） | 100~200MB，目标 150MB 触发切新 part | 多 part 并行下载友好；DuckDB 点查只下需要的 row group，文件大小不影响点查延迟；太小文件数爆炸（78 天 × 多 part），太大单文件上传/校验粒度粗 |
| row group（未压缩） | 128MB（约 1000 行，66KB/行）或 20,000 行，先到先切 | 128MB 是 Parquet/Hadoop 社区经验值；本场景每行 ~66KB（body 主导），128MB≈1000 行；row group 是谓词下推与回读的最小 IO 单位 |
| 排序键 | request_id ASC（全文排序） | 见 §2.4 |
| 行内列序 | 按 §1.2 分组顺序（时间/主键 → 维度 → 指标 → raw → 抽取标量） | 读「只统计指标」的查询不会碰到 raw 列所在的列块（列裁剪） |
| 单日 part 数 | 约 2 个（35k 行 / 2.3GB 原始 → 250MB 压缩 → 2×125MB） | 与框架文档 §4.2 一致 |

### 2.4 按 request_id 排序的理由（回读点查 + 谓词下推）

1. 点查回读是唯一热路径：用户点开历史 trace → WHERE request_id = 'xxx'。Parquet 每 row group 写 request_id 的 min/max 统计，按 request_id 排序后，谓词下推把点查收敛到唯一 1 个 row group，只读该 row group 的 request_body_raw / response_body_raw 两列（HTTP range 拉取 <1MB），<1s。
2. 幂等去重：同一 request_id 相邻，归档/重放时按 request_id 去重只需在相邻行内判断。
3. 代价与取舍：UUID 类 request_id 排序后 ts 在文件内近似随机。单日数据量小（35k 行），「日内 ts 范围扫描」本来就要扫全 partition 的所有 row group，排序键不影响它；而 request_id 点查收益是决定性的。若未来「日内 ts 顺序读」成为主查询，可另建按 ts 排序的副本（见 §6），不改本 schema。

---

## 3. 写入批处理设计

### 3.1 抽取在哪做：SQL 侧预解析，Go 写层不碰 JSON

结论：标量抽取在 SQL 侧做（存量用 DuckDB、增量用 PG 查询），Go Compactor 只做「SQL 结果列 → Parquet 列」的纯映射，不解析 JSON。

理由：
- 现有 JOIN 抽取逻辑已在 SQL（usage_handler 一致），复用同一条表达式，语义不会漂。
- 避免 Go 侧对 68KB body 做 json.Unmarshal（内存翻倍 + CPU），写层零 JSON 依赖。
- 存量/增量共用同一 SELECT，仅数据源不同（DuckDB 读 PG vs 服务内 gorm 查 PG）。

### 3.2 内存与批大小（落地参数，默认值可配）

| 参数 | 默认值 | 说明 |
|---|---|---|
| RowGroupTargetBytes | 128MB（未压缩累计） | 触发 row group flush 的字节阈值（估算：各行 raw 列长度之和） |
| RowGroupTargetRows | 20,000 行 | 触发 row group flush 的行数阈值（先到先切） |
| RowGroupFlushEvery | 5 分钟 | 兜底定时 flush（凌晨低流量时不长时间滞留缓冲） |
| PartTargetBytes | 150MB（压缩后） | 写完一个 row group 后检查文件压缩后大小，≥150MB 即 CloseFile 开新 part |
| MaxConcurrentParts | 2（可 4） | 天级并行度（semaphore），控制总内存 |
| 单 part 写缓冲内存 | ≤256MB | row group buffer（128MB）+ 列页压缩/编码临时区，写满即 flush 释放 |

总内存上界 ≈ MaxConcurrentParts × 256MB（默认 2 → 512MB），不随数据量增长（流式，边读边写边 flush）。

### 3.3 flush 并发模型

- 同一 dt 内顺序写：单 writer goroutine 按 request_id 排序流式消费，顺序保证排序键不变；row group 写满即 flush 到本地 staging 文件。
- 不同 dt 之间并行：每个 dt 一个 goroutine，由 MaxConcurrentParts semaphore 限流；某天归档失败只重试该天，不影响其他天。
- Extractor 限速：读 PG 限速（默认 ≤50MB/s，低峰期执行），避免大字段查询把生产库 IO 打满；Uploader 同样限速（见框架文档 §5.3）。

### 3.4 写循环伪代码（Compactor）

    func (c *Compactor) CompactPart(ctx context.Context, dt string, rows <-chan ArchiveRow) ([]PartManifest, error) {
        var manifests []PartManifest
        w, partIdx, partBytes := c.newPartWriter(dt, 0), 0, int64(0)
        var buffered int64 // 当前 row group 未压缩字节
        var bufferedRows int
        ticker := time.NewTicker(c.cfg.RowGroupFlushEvery)
        defer ticker.Stop()

        flushRG := func() error {
            if bufferedRows == 0 { return nil }
            w.FlushRowGroup()               // 编码 + zstd + 落 staging
            partBytes = w.CompressedBytes() // 文件当前压缩后大小
            buffered, bufferedRows = 0, 0
            if partBytes >= c.cfg.PartTargetBytes {
                m := w.Close()              // 收尾：footer、_SUCCESS、sha256、行数
                manifests = append(manifests, m)
                partIdx++
                w = c.newPartWriter(dt, partIdx) // part-00001.parquet
                partBytes = 0
            }
            return nil
        }

        for {
            select {
            case <-ctx.Done():
                _ = flushRG()
                if m := w.Close(); m.Rows > 0 { manifests = append(manifests, m) }
                return manifests, nil
            case r, ok := <-rows:
                if !ok { _ = flushRG(); if m := w.Close(); m.Rows > 0 { manifests = append(manifests, m) }; return manifests, nil }
                w.Write(r)
                buffered += r.estimateRawBytes()
                bufferedRows++
                if buffered >= c.cfg.RowGroupTargetBytes || bufferedRows >= c.cfg.RowGroupTargetRows {
                    if err := flushRG(); err != nil { return manifests, err }
                }
            case <-ticker.C:
                _ = flushRG()
            }
        }
    }

### 3.5 part 状态机与 manifest（与框架文档 §5.3 对齐，补字段）

    exported → compacted → uploading → uploaded → verified → pg_cleaned
                                        └→ retry_queue（指数退避，>N 次 → DLQ + 告警）

每 part 一个 manifest（本地 staging 与 _manifest/ 各一份）：

    {
      "dt": "2026-06-04",
      "part": "part-00000.parquet",
      "state": "verified",
      "rows": 17823,
      "ts_min": "2026-06-04T00:00:00+08:00",
      "ts_max": "2026-06-04T23:59:59+08:00",
      "uncompressed_bytes": 1234567890,
      "compressed_bytes": 150000000,
      "sha256": "…",
      "zstd_level": 3,
      "schema_version": 1,
      "created_at": "…", "uploaded_at": "…", "verified_at": "…", "pg_cleaned_at": "…"
    }

---

## 4. 实现选型：Go 库直写 vs DuckDB 导出

### 4.1 三个候选

| 方案 | 内存 | 复杂度 | schema 演进 | 部署 | 适用 |
|---|---|---|---|---|---|
| A. github.com/parquet-go/parquet-go（segmentio 的社区维护续作） | 流式、受控 | 中（Go 写 struct→writer） | struct 加字段即可，footer 自描述 | 纯 Go，进程内 | 增量归档（长期） |
| B. github.com/segmentio/parquet-go | 流式、受控 | 中 | 同上 | 纯 Go，进程内 | 与 A 二选一，API 近同 |
| C. DuckDB CLI / 子进程（COPY） | 受控（spill 到 temp） | 低（纯 SQL） | SQL 加列即可 | 引入外部二进制 | 存量迁移 + POC |

### 4.2 结论

- 增量归档（服务内长期运行）→ 用 Go 库直写，选 parquet-go/parquet-go（续作），segmentio/parquet-go 作为 API 近同的兜底。理由：纯 Go 无 cgo；流式 writer 显式控制 row group（MaxRowsPerRowGroup / 手动 FlushRowGroup）；zstd 支持（走 klauspost/compress）；与 Archiver 状态机（Extractor/Uploader/Verifier/Janitor）同进程，不引入外部进程与进程管理；schema 演进靠 Go struct 加字段。
- 存量迁移（一次性 78 天）+ POC → 用 DuckDB。理由：JOIN + 全量标量抽取 + 分区 + 压缩 + row group 一行 SQL 表达；DuckDB 自带 postgres_scanner 直接读 PG；列存引擎天然流式 + 落盘 spill，内存有界；省去为一次性任务写 Go 代码。引入的外部二进制只用于迁移机/POC，不进生产服务镜像。
- 为何不用 PG 侧 COPY ... (FORMAT parquet)：需装 pg_parquet 扩展（CrunchyData），引入 PG 扩展与 COPY 大字段的锁/IO 风险；且抽标量表达式嵌在 COPY 里不如 DuckDB 好调。列为非首选，仅在「不能引入 DuckDB、但 PG 允许装扩展」时备用。

> 落地前用 go list -m -versions github.com/parquet-go/parquet-go 与 github.com/segmentio/parquet-go 确认当前维护状态与 Go 1.26 兼容，再钉版本；两库均纯 Go、均可无 cgo 交叉编译，符合项目风格（项目已有 aws-sdk-go-v2，上传侧零新增依赖争议）。

### 4.3 Go 写入侧结构体（schema 的单一事实来源）

    type ArchiveRow struct {
        Dt          string  `parquet:"dt"`           // STRING 'YYYY-MM-DD'
        TsMicros    int64   `parquet:"ts,timestamp(microsecond)"` // INT64 微秒, UTC
        RequestID   string  `parquet:"request_id"`   // PLAIN，排序键
        // … §1.2 各列；可空列用指针 *string / *int32 / *int64 / *bool / *float64
        RequestBodyRaw  *string `parquet:"request_body_raw"`
        ResponseBodyRaw *string `parquet:"response_body_raw"`
    }

- 时间列写 int64 微秒自控（避免 time.Time 默认编码歧义），读侧 DuckDB 直接识别 TIMESTAMP(MICROS)。
- 写入时把 schema_version、zstd_level、writer 写入 Parquet file key-value metadata（parquet-go 支持 KeyValueMetadata），供 §6 兼容与审计。
- 排序：Extractor SQL ORDER BY request_id 保证流已有序，writer 无需内存排序；若数据源无序，用 writer 的排序缓冲或外部排序（磁盘）兜底，默认不启用。

---

## 5. zstd 压缩级别选择与依据

| level | klauspost 常量 | 相对吞吐 | 相对压缩比 | 结论 |
|---|---|---|---|---|
| 1 | SpeedFastest | 最快 | 基准 | 不选（浪费存量降本空间） |
| 3 | SpeedDefault | 快 | ~+2~4% vs L1 | 默认 |
| 6 | — | 中 | ~+4~8% vs L3 | POC 若收益 ≥5% 且吞吐 ≥30MB/s → 升到 6 |
| 9~11 | SpeedBetterCompression / SpeedBestCompression | 慢（3~5x L3） | ~+8~12% vs L3 | 归档一次性写入可接受，但收益递减，默认不选 |

依据：
1. 本场景压缩大头来自列存 + 字典编码 + 结构化列去冗余，zstd 级别只作用于「raw 正文列」等低冗余数据，级别越高收益越边际。
2. 写入是一次性、低峰期、可重试，CPU 不是瓶颈，理论可上高级别；但 level 3→9 的额外 CPU 换 ~8% 大小，对「179GB→20~30GB」量级只差 2~3GB，性价比低。
3. POC 实测 level 1/3/6/9 的「压缩比 × 导出耗时」，取拐点（默认 3，6 为上限备选）；配置项 ARCHIVE_ZSTD_LEVEL（默认 3）+ manifest 记录 zstd_level（同文件回读无需猜测）。

---

## 6. schema 演进与向后兼容

Parquet schema 写在每个文件 footer 里、自描述，没有全局 catalog，这是演进的最大便利：加列不影响旧文件。

| 场景 | 规则 | 读旧文件 |
|---|---|---|
| 加列 | 只追加列（放 schema 末尾），新列默认 NULL；新文件带新列，旧文件无 | DuckDB read_parquet('**/*.parquet', union_by_name=true, hive_partitioning=true) 自动把缺列填 NULL |
| 改列类型 | 禁止原地改。新类型用新列名（如 prompt_chars 需换语义 → 加 prompt_chars_v2），读侧 COALESCE(prompt_chars_v2, prompt_chars) | 旧文件只有旧列，COALESCE 兜底 |
| 删列 | 物理不删（历史文件还有），读侧 SELECT 不引用即可 | 无影响 |
| 加分区/换排序键 | 新目录或新副本（如 sorted_by_ts=1/），不改原 dt= 目录 | 旧查询路径不变 |
| schema 版本 | 每文件 footer key-value 写 schema_version（int 递增）+ _manifest/schema_version；重大变更 bump | 读侧按版本分支（一般无需，union_by_name 已兜住） |
| 回滚 | schema 只增不改 + 文件不可变（对象存储只追加），回滚=读侧回退到旧 SELECT/旧 struct | 天然可回滚 |

兼容性验证（每次加列后跑一次 smoke）：

    SELECT count(*) FROM read_parquet('s3://llm-gateway-traces/**/*.parquet',
                                      hive_partitioning=true, union_by_name=true);

---

## 7. P0 POC 验证方案（取 1 天真实数据）

目标：实测压缩比 / 可读性 / 回读延迟 / zstd 级别拐点，为 §1~§6 的参数定值。

### 7.1 选天与基线

    -- 1) 选代表性一天（行数最多的一天）作为主样本；另选最早一天测 schema 漂移
    SELECT ct.ts::date AS d,
           count(*) AS trace_rows,
           count(cl.request_id) AS log_rows,
           pg_size_pretty(sum(pg_column_size(ct.request_body) + pg_column_size(ct.response_body))) AS body_size
    FROM call_traces ct
    LEFT JOIN call_logs cl ON cl.request_id = ct.request_id
    GROUP BY 1 ORDER BY 1;

    # 2) 导出前抓取基线：该日正文未压缩字节（用于算压缩比）
    psql "$DATABASE_URL" -c "
    SELECT sum(pg_column_size(ct.request_body)+pg_column_size(ct.response_body)) AS body_bytes
    FROM call_traces ct LEFT JOIN call_logs cl ON cl.request_id = ct.request_id
    WHERE ct.ts >= '2026-06-04' AND ct.ts < '2026-06-05';"

### 7.2 导出（DuckDB 直读 PG）

    duckdb :memory: <<'SQL'
    INSTALL postgres; LOAD postgres;
    ATTACH 'host=192.0.2.10 port=5432 dbname=llm_gateway user=ro password=***'
           AS pg (TYPE postgres, READ_ONLY);
    COPY (
      SELECT
        (COALESCE(cl.ts, ct.ts) AT TIME ZONE 'Asia/Shanghai')::date::text AS dt,
        COALESCE(cl.ts, ct.ts) AS ts,
        COALESCE(cl.request_id, ct.request_id) AS request_id,
        -- ... §1.2 各列 + §1.3 抽取标量 ...
        ct.request_body::text  AS request_body_raw,
        ct.response_body::text AS response_body_raw
      FROM pg.call_traces ct
      LEFT JOIN pg.call_logs cl ON cl.request_id = ct.request_id
      WHERE ct.ts >= '2026-06-04' AND ct.ts < '2026-06-05'
      ORDER BY ct.request_id
    ) TO 'staging/dt=2026-06-04/part-00000.parquet'
    (FORMAT parquet, COMPRESSION zstd, COMPRESSION_LEVEL 3, ROW_GROUP_SIZE 20000);
    SQL

（DuckDB 参数名以所装版本为准：COMPRESSION_LEVEL 为 zstd 级别、ROW_GROUP_SIZE 为行数；另可用 PER_THREAD_OUTPUT 并行分 part。）

### 7.3 压缩比

    du -sh staging/dt=2026-06-04/*.parquet   # 压缩后总字节
    # 压缩比 = 7.1 的 body_bytes（+结构化列近似值）/ 压缩后总字节

### 7.4 可读性 / 正确性

    -- 行数一致（== 7.1 该日 FULL JOIN 行数）
    SELECT count(*) FROM read_parquet('staging/dt=2026-06-04/*.parquet');

    -- request_id 集合一致性（双向差集，均应为 0）
    SELECT count(*) FROM (
      SELECT request_id FROM read_parquet('staging/dt=2026-06-04/*.parquet')
      EXCEPT
      SELECT request_id FROM ( /* 同 7.2 的 PG 端 FULL JOIN 查询 */ ) p
    );

    -- 抽样正文 md5 比对（抽 100 条，PG jsonb::text vs Parquet raw）
    -- PG:      SELECT md5(ct.request_body::text) FROM call_traces ct WHERE request_id=...;
    -- Parquet: SELECT md5(request_body_raw) FROM read_parquet(...) WHERE request_id=...;

    -- 标量抽取抽样校验：task_id/documents_count 与 usage_handler 现网结果对齐
    SELECT request_id, task_id, documents_count, messages_count, finish_reason, has_tool_calls
    FROM read_parquet('staging/dt=2026-06-04/*.parquet') LIMIT 20;

### 7.5 回读延迟（点查）

    # 随机取 100 个 request_id，逐个点查，测 p50/p95
    time duckdb :memory: "SELECT request_body_raw, response_body_raw
      FROM read_parquet('staging/dt=2026-06-04/*.parquet')
      WHERE request_id='<id>'"

验收：p95 < 1s（本机；生产再加对象存储 HTTP range 一次往返）。

谓词下推确认（应只扫 1 个 row group，不扫全文件）：

    EXPLAIN ANALYZE SELECT request_body_raw FROM read_parquet('staging/dt=2026-06-04/*.parquet')
    WHERE request_id='<id>';

### 7.6 zstd 级别拐点（§5 定值依据）

    for lvl in 1 3 6 9; do
      /usr/bin/time -v duckdb :memory: "
        COPY (/* 同 7.2 的 SELECT */)
        TO 'staging/lvl$lvl.parquet'
        (FORMAT parquet, COMPRESSION zstd, COMPRESSION_LEVEL $lvl, ROW_GROUP_SIZE 20000);"
      du -sh staging/lvl$lvl.parquet
    done

### 7.7 POC 通过判据（写进 POC 报告）

1. 压缩比 ≥ 8x（支撑 179GB 量级 → 20~30GB）；
2. FULL JOIN 行数、request_id 集合、抽样 md5 三者 100% 一致（不丢数据铁证）；
3. 点查 p95 < 1s，EXPLAIN 证明谓词下推命中唯一 row group；
4. zstd 级别拐点明确（默认取 3 或 6）；
5. 最早一天样本通过（验证历史 schema 漂移不炸导出）。