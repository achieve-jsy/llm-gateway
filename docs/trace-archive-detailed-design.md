# 调用链路记录归档 —— 详细设计总览与索引

> 状态：详细设计（落地层）。方案框架（示意层，讲"做什么 / 为什么"）见 [trace-archive-compression-design.md](trace-archive-compression-design.md)。
> 本文件是三份详细设计的入口索引 + 关键决策汇总 + 跨任务依赖 + 待确认输入。

## 三份详细设计

| 文档 | 覆盖 | 关键产出 |
|------|------|----------|
| [trace-archive-compression-schema.md](trace-archive-compression-schema.md) | 压缩 | Parquet 宽表完整 schema（逐列类型/编码/来源）、分区切分、写入批处理、parquet-go vs DuckDB 选型、zstd 级别、schema 演进、P0 POC |
| [trace-archive-deletion-migration-design.md](trace-archive-deletion-migration-design.md) | 删除/迁移 | 存量 179GB + 增量归档的分片状态机、校验五步、热窗口、UPDATE SET NULL 置空、水位线、cron bug 修复 |
| [trace-archive-storage-design.md](trace-archive-storage-design.md) | 存储 | ArchiveStore 接口、S3 对象布局、multipart/断点续传、生命周期、最小权限、成本估算、20+ 配置项 |

## 关键决策汇总（captain 裁决后的统一口径）

1. **压缩格式**：Parquet + zstd（level 3 默认、6 上限），宽表 = call_logs ∪ call_traces。
2. **写入选型**：增量归档用 Go 库 `parquet-go/parquet-go` 直写；存量迁移 + POC 用 DuckDB 导出。
3. **驱动表（裁决）**：以 call_traces 为驱动表 `ct LEFT JOIN cl`——孤儿 trace 不丢、对账等式「归档行数 == 当天 call_traces 行数」成立。compression 初稿的 FULL OUTER JOIN 已由 captain 修正为 LEFT JOIN。
4. **状态机双循环**：Archiver 推进到 `verified`（写 archive_ref / archive_status=1，不动正文）；Janitor 在热窗口（默认 30 天）到期后置空（archive_status=2）。
5. **置空方式**：`UPDATE SET request_body=NULL, response_body=NULL`（保留元数据行），不用 DELETE；keyset 1000 行/批 autocommit；pg_repack 在线回收。
6. **存储**：S3 兼容对象存储，低频访问；16MB multipart 直驱 + sidecar 断点续传；MD5→ETag→SHA256→CRC64 校验链。
7. **回读**：按 request_id 排序写 Parquet，DuckDB range 拉取单条 < 1s。

## 跨任务依赖（落地必须处理）

1. **task_id 抽列后的消费改造**：endpoint 正则来源在 call_logs 侧，抽列到 call_traces 后，`QueryLogs` 需改为**优先读 ct.task_id、缺失回退 endpoint 正则**（否则抽列白做，JOIN 仍会碰大字段）。
2. **存量迁移的时序**：当前 cleanup cron 因 bug 从未运行，78 天 call_logs 都还在库。**存量迁移须赶在修复 cron bug 之前跑**（或修复后暂时停用 cleanup），否则 cleanup 会删掉 30 天+ 的 call_logs 维度行，迁移 JOIN 不到维度。
3. **依赖补齐**：aws-sdk-go-v2 v1.41.7 已依赖，但 `service/s3` 未 vendor，落地需 `go get github.com/aws/aws-sdk-go-v2/service/s3` + `go mod vendor`。

## 待确认输入（落地前必须拍板）

1. 对象存储 endpoint / region / bucket 权限 + AccessKey 托管方式（IAM 角色 / env / system_configs 加密三选一）。
2. 是否启用正文落盘加密（AES-GCM 应用层加密开关，见方案 §10）。
3. 归档保留周期 + TTL（默认永久，或按公司数据保留政策）。
4. 平台是否支持 `x-amz-checksum-sha256`（影响校验链强度）。
5. 热窗口天数（默认 30 天；30 天 ≈ 70GB、7 天 ≈ 16GB、1 天 ≈ 2~3GB）。

## 环节解耦性分析

解耦原则：**环节之间解耦（可替换、可独立演进、故障隔离），但每个环节都锚定在少量"事实源"上。**

### 已解耦（通过接口/抽象，可独立替换演进）

| 环节边界 | 解耦手段 | 替换什么不影响其他 |
|---|---|---|
| 主写入 vs 归档 | 归档是旁路，TraceWriter 不动 | 归档挂了主链路照常；反之亦然 |
| 归档 vs 置空 | 双循环：Archiver 到 `verified`（只写指针不动正文），Janitor 热窗口到期才置空 | 归档失败永不置空；置空晚不影响归档 |
| 存储协议 | ArchiveStore 接口（Put/Get/Exists/List/Delete + Head） | S3 ↔ NFS ↔ SFTP 只换一个实现 |
| 压缩 vs 存储 | Parquet 自描述对象 | 换对象存储 / 分区粒度，文件本身不动 |
| 正文 vs 分析 | 标量列抽取（task_id / messages_count / …） | 统计 token / 成本不读正文大列 |
| 产物 vs 分析引擎 | Parquet + Hive 分区是开放格式 | DuckDB ↔ ClickHouse ↔ Pandas 随便换 |

### 刻意保留的锚点（耦合，但有清晰边界）

| 锚点 | 耦合什么 | 为什么不能全解耦 |
|---|---|---|
| call_traces schema | Extractor / Janitor 的 SQL 直接读写 `request_body/response_body/archive_status/archive_ref/ts` | 正文的"家"在 PG，唯一事实源；改表结构要同步改这两个环节 |
| archive_ref ↔ 对象 key | PG 指针存 `dt=YYYY-MM-DD/` 前缀，回读按它拼路径 | 指针格式 = 存储布局契约 |
| 回读性能 ↔ 写入排序 | 单条 <1s 依赖 Parquet 按 `request_id` 排序写 | 写入约定喂给读性能 |
| manifest 双写 | S3 `_manifest/*.json` ↔ PG `trace_archive_manifests` | 对账 / 审计要一个权威清单 |
| 两个改造依赖 | ①存量迁移先于 cleanup 修复 ②task_id 抽列后 QueryLogs 改读 `ct.task_id` | 部署时序 + 一次性接线 |

### 结论

环节之间解耦、故障隔离，但每个环节锚定在少量事实源上（PG schema、对象 key 约定、manifest 格式、request_id 排序）——这是"耦合点少、且每个耦合点有明确 owner 和改动半径"的形态，不是零耦合。

可选收敛：实现期加一层 `call_traces_archive_v` 视图，把 Extractor / Janitor 对 call_traces 字段的直接依赖收拢到一处，进一步缩小 schema 改动半径。
