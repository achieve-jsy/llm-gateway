# Gateway 大请求体内存治理 — 设计与实现规范

> 分支：`fix/gateway-body-memory`（基于 `origin/main`）
> 状态：本文件是并行实现的**冻结契约**。所有实现必须严格遵循本文的接口与常量，不得自行更改签名。

## 1. 背景与根因（实测）

生产网关（`:8082`，容器内存上限 1 GiB）在 2026-09-18 出现 20 次 cgroup OOM 杀进程，每次重启后路由表冷启动约 5s，期间所有请求返回 503 `model_unavailable`。

触发源：单个 API Key（CoPaw）持续发送 20–30 MB 请求体。但**体积本身不足以打爆 1 GiB**，真正的放大器有三处：

### 1.1 trace 捕获路径的 JSON 双序列化（~65 MB / 请求）

`internal/gateway/middleware/trace_capture.go` 对**所有**请求体同步执行 `sanitizeJSONRequestBody`：
`io.ReadAll` → `json.Unmarshal` 成对象树 → 递归改写 → `json.Marshal` 回写。

30 MB 请求体实测（`/tmp/amp` 微基准）：

| 阶段 | 存活 |
|---|---|
| 读完原始 body | +30.02 MB |
| `json.Unmarshal` 对象树 | +35.08 MB |
| `json.Marshal` 输出副本 | +30.02 MB |
| **请求期间峰值** | **95.13 MB** |
| 累计分配 | 201.32 MB |

### 1.2 `io.ReadAll` 的 append 翻倍

长度未知时 `append` 按 2 倍增长：30 MB body 分配 51.31 MB（**2.18×**）。按 `Content-Length` 预分配后为 23.52 MB（**1.00×**），省 54% 分配 churn。

### 1.3 批量 INSERT 的 SQL 文本内联（最大的一项）

`cmd/gateway/main.go` 构造 `NewTraceWriter(db, 1024, 50, 2*time.Second)`：**channel 1024 槽、flushN=50**。
`internal/controlplane/infra/postgres.go:39` 以 `logger.Default.LogMode(logger.Warn)` 打开 GORM，`ParameterizedQueries` 未设置（=false）→ 慢 SQL（默认 `SlowThreshold=200ms`）会把**全部参数值内联**成日志字符串。

**更关键**：该 DB 连接配置了 `PreferSimpleProtocol: true`（`internal/controlplane/infra/postgres.go:37`），而 pgx v5 在简单协议下**由客户端把参数内联进 SQL 文本**：

```
pgx/v5@v5.6.0/conn.go:645
// QueryExecModeSimpleProtocol uses client side parameter interpolation. All values are quoted and escaped.
```

因此**即使关掉 GORM 日志，pgx 仍会构造一份内联 SQL**。50 行 × 24 MB ≈ **1.2 GB**。

运维侧留存的证据（`/usr/local/bin/docker-log-guard.sh` 头部注释）：

> GORM 以 logger.Warn 打开，会打印超过 SlowThreshold(200ms) 的慢 SQL 并带上全部参数值；call_traces 的批量 INSERT 内嵌完整 request_body/response_body，**单条日志可达 12MB，日志量 40~120GB/天，曾把系统盘（57G）写满**

（12 MB = 50 行 × 240 KB 均值，与 flushN=50 吻合。）

### 1.4 并发无上界

`/v1` 无任何请求体总量约束（全仓无 `MaxBytesReader`）。`并发数 × 体积` 乘积不受控。

### 1.5 现状汇总（单客户端重试 25 MB 请求）

```
基线                      380 MB
在途请求峰值             + 95 MB
一次 flush 的 SQL 文本   + 480 MB   ← 20 行 × 24 MB（flushN=50 时更大）
──────────────────────────────────
同时存在                  ~955 MB   → 1 GiB 下 OOM
```

## 2. 目标

| 目标 | 指标 |
|---|---|
| 单请求峰值内存 | **95 MB → ~30 MB**（= 1× body） |
| 单次 flush 的 SQL 文本 | **480 MB+ → ≤ 8 MB**（按字节限批） |
| 队列滞留 | **有上界**（按字节，不按槽数） |
| 并发 × 体积 | **有硬上界**（在途字节预算） |
| **trace 完整性** | **不降低**（大 body 存原文，比现状的 sanitize 更完整） |

## 3. 非目标（本分支不做）

- **归档外置（A-2/A-3）**：`internal/archive/` 与 `call_traces` 的 `archive_ref`/`archive_status` 列**不在 `origin/main`**（已核实），归档子系统是另一分支的进行中工作。本分支不引入 schema 变更，不依赖迁移。
- 转发路径重构（透传原始字节、字节级 patch `model`）：改动 `core.ChatRequest` 与全部 provider，风险与收益不成比例。
- 流式 `tool_calls` / `finish_reason` 丢失修复：属独立的完整性缺陷。

## 4. 冻结接口契约

### D1 — 解析阈值

```go
// internal/gateway/middleware/trace_capture.go
// TraceSanitizeLimits 新增字段（默认 1 MB）：
//   MaxParseBytesInTrace int
// 默认值放入 defaultTraceSanitizeLimits：
//   MaxParseBytesInTrace: 1 << 20
```

含义：请求体 > `MaxParseBytesInTrace` 时**完全跳过 `json.Unmarshal` / `sanitizeValue` / `json.Marshal`**，直接把原始字节作为正文交给下游。

### D2 — context 键（两个互斥）

| 键 | 类型 | 何时设置 |
|---|---|---|
| `trace_req_body` | `[]byte` | `len(body) <= MaxParseBytesInTrace`：**sanitize 后的正文（现有行为，一字不变）** |
| `trace_req_body_raw` | `[]byte` | `len(body) > MaxParseBytesInTrace`：**原始未处理正文** |

两者**互斥**，同一次请求只设置其中一个。

### D3 — TraceEvent 新增字段

```go
// internal/gateway/observability/trace_event.go
type TraceEvent struct {
	// ... 既有字段不动 ...
	// BodyRawUntrusted 为 true 表示 RequestBodyRaw 来自"大正文直存"路径（未经 sanitize/校验）。
	// 用于跳过写入侧的 json.Valid 全量校验，避免 30MB 级别的校验阻塞写入协程。
	BodyRawUntrusted bool
}
```

### D4 — TraceWriter 构造与限额

保留既有构造器（向后兼容，内部转调新构造器）：

```go
func NewTraceWriter(db *gorm.DB, bufSize, flushN int, interval time.Duration) *TraceWriter
```

新增：

```go
// TraceWriterLimits 按字节约束写入器，防止单次 flush 的 SQL 文本与队列滞留失控。
type TraceWriterLimits struct {
	MaxBatchBytes int64 // 单次批量 INSERT 的正文字节上限，默认 8 MB
	MaxQueueBytes int64 // channel 中待写正文的字节上限，默认 128 MB
}

const (
	DefaultTraceMaxBatchBytes int64 = 8 << 20   // 8 MB
	DefaultTraceMaxQueueBytes int64 = 128 << 20 // 128 MB
)

func NewTraceWriterWithLimits(db *gorm.DB, bufSize, flushN int, interval time.Duration, limits TraceWriterLimits) *TraceWriter
```

`limits` 中为 0 的字段取默认值。

### D5 — 按字节限批

`Run()` 的批量累积条件由 `len(batch) >= flushN` 改为：

```go
if len(batch) >= w.flushN || batchBytes >= w.maxBatchBytes {
```

`batchBytes` 为该批各行的 `len(RequestBodyRaw) + len(ResponseBodyRaw)` 之和。**这是防止 pgx 简单协议内联出 GB 级 SQL 文本的核心手段。**

### D6 — 按字节限队列

`Emit` 在投递前用 `atomic.Int64` 累计待写字节；若 `queued + n > maxQueueBytes` 则丢弃并记日志（保留既有的 500ms 阻塞吸收突发行为，字节上限是额外闸门）。事件被 `Run()` 取走并写完后递减计数。

### D7 — 在途字节预算中间件

新增 `internal/gateway/middleware/body_budget.go`：

```go
const DefaultBodyBudgetBytes int64 = 96 << 20 // 96 MB（端到端实测校准，见附录 A）

func BodyBudgetLimiter(budgetBytes int64) gin.HandlerFunc
```

语义：
- 只对 `POST` 且 `ContentLength != 0` 生效；其余直接 `c.Next()`。
- 以 `Content-Length` 为权重向 `golang.org/x/sync/semaphore`（`NewWeighted`）申请；`ContentLength <= 0` 时按 4 MB 保守预留。
- 申请带 10s 超时；**超时返回 503 `model_unavailable`**（与 OOM 期间客户端已会重试的错误码一致），**不是 413**。
- 预算在整个请求生命周期内持有（`defer Release`），覆盖 handler 读取与转发上游。
- 错误响应信封必须与 `internal/gateway/handler/chat.go` 的 `respondError` 一致：
  `{"error":{"type":"model_unavailable","code":"model_unavailable","message":"the requested model is temporarily unavailable"}}`

注册位置：`internal/gateway/server/router.go` 中 `v1.Use(middleware.AuthMiddleware(...))` **之后**、`v1.Use(middleware.TraceCapture())` **之前**。

### D8 — trace 写入专用 GORM logger

`cmd/gateway/main.go` 中为 trace 写入使用独立 session：

```go
traceDB := db.Session(&gorm.Session{Logger: newTraceDBLogger()})
traceWriter = gwobs.NewTraceWriterWithLimits(traceDB, 1024, 50, 2*time.Second, gwobs.TraceWriterLimits{})
```

`newTraceDBLogger()` 返回 `logger.New(...)`，配置为：
- `LogLevel: logger.Warn`
- `SlowThreshold: 2 * time.Second`
- **`ParameterizedQueries: true`**（日志只打 `?` 占位符，不内联参数值）
- `IgnoreRecordNotFoundError: true`
- `Colorful: false`

**不得**修改 `internal/controlplane/infra/postgres.go` 的全局 logger —— control-plane 共用该函数，改全局会让管理面丢失慢 SQL 日志。

### D9 — 内存上限

gateway 服务 `deploy.resources.limits.memory: 1g → 2g`，三处：
- `docker-compose.yml`（第 46 行）
- `deploy/blue-green/docker-compose.green.yml`（第 43 行）
- `deploy/blue-green/docker-compose.blue.yml`

**不改** control-plane 的 2g。

`DefaultBodyBudgetBytes` 与 2g 上限是**配套**的：256 MB × 转发路径约 2.5× = 640 MB，加基线 380 MB 约 1.0 GB，需要 2g 才有安全余量。

### D10 — 内存预算账（含端到端实测修正）

单元级建模曾给出"最坏同时存在 ~772 MB"的乐观账（假设放大倍数 2.5×）。
**端到端实测推翻了这个假设**：真实放大倍数约 **9.6×**，且请求/转发路径（不是 trace 路径）
才是内存主项。详见附录 A。

```
实测（8 并发 × 30 MB 正文，真实 gateway 二进制）：
  改前                        峰值 RSS 1548 MB
  改后 · 预算 256 MB          峰值 RSS 1456 MB   ← 预算没兜住，且丢 3 条 trace
  改后 · 预算  96 MB          峰值 RSS  923 MB   ← 8/8 落库、0 丢弃、0 超时
```

结论：**body budget 是端到端内存的决定性杠杆，而不是 trace 路径**。
按实测 9.6× 反推预算上限：

| 容器上限 | 可用预算（按 80% 余量） |
|---|---|
| 1g | (1024×0.8 − 400) / 9.6 ≈ **43 MB** |
| 2g | (2048×0.8 − 400) / 9.6 ≈ **129 MB** |

故取 **96 MB**：2g 下有安全余量；若仍留在 1g 必须先调小到 40 MB 量级。

## 5. 工作流划分（禁止跨流改文件）

| 流 | 文件 | 负责 |
|---|---|---|
| **A** | `internal/gateway/middleware/trace_capture.go`、`internal/gateway/middleware/usage_collector.go` | D1、D2、D3 的读取侧 |
| **B** | `internal/gateway/observability/trace_event.go`、`cmd/gateway/main.go` | D3、D4、D5、D6、D8 |
| **C** | `internal/gateway/middleware/body_budget.go`（新建）、`internal/gateway/server/router.go` | D7 |
| **D** | `docker-compose.yml`、`deploy/blue-green/docker-compose.{green,blue}.yml` | D9 |
| **E** | `tests/` | 单元测试（依赖 A/B/C 完成后） |

流 A 的 `usage_collector.go` 修改：读取时先取 `trace_req_body`，缺失则取 `trace_req_body_raw` 并置 `te.BodyRawUntrusted = true`。

## 6. 验收标准

1. `go build ./...` 与 `go vet ./...` 通过。
2. `go test ./tests/... -count=1` 通过。
3. 小请求体（≤ 1 MB）行为**与改动前逐字节一致**（sanitize 路径不变、落库内容不变）——必须有测试覆盖。
4. 大请求体（> 1 MB）不触发 `json.Unmarshal`/`json.Marshal`——必须有测试覆盖（可用 `testing.AllocsPerRun` 或打桩断言）。
5. 单次 flush 的构造 SQL 长度有上界（按字节限批生效）。
6. 队列字节上限生效且计数正确回收（无泄漏导致永久丢弃）。
7. `go test ./tests/... -run Trace -count=1` 通过。

## 7. 回滚

无 schema 变更、无数据迁移。蓝绿部署直接切回另一色。所有限额均为常量，可通过改常量并重新构建回退。

## 附录 A — 端到端实测（本文档所有关键数字的来源）

单元测试跑在 **SQLite 内存库**上（`tests/gateway/middleware/trace_pipeline_test.go:24`），
因此它**不可能**覆盖 pgx 简单协议内联、jsonb 落库、真实 RSS 这三件事。以下为补充的端到端实测。

### A.1 方法

- **真实驱动 + 真实 PG**：GORM `postgres.New(postgres.Config{DSN, PreferSimpleProtocol: true})`
  + 默认 `logger.LogMode(Warn)`，与生产完全一致；隔离库 `gw_e2e_scratch`。
- **真实 gateway 二进制**：`go build ./cmd/gateway` 后以真实进程运行，用极简 stub 控制面
  提供 `verify-key` / `groups`，经真实 TCP 发出真实 HTTP 请求。
- **采样**：每 20 ms 采一次 `ps -o rss`；进程内用 `runtime.ReadMemStats` 采 `HeapAlloc` 峰值。
- **请求体**：`{"model":"...","messages":[{"role":"user","content":"<30 MB>"}]}`，31457350 字节。

### A.2 写入路径：288 MB 载荷（12 × 24 MB），真实 pgx

| 配置 | GORM 日志 | 堆峰值 | 累计分配 |
|---|---|---|---|
| 改前（默认 logger + 无限批） | 264.0 MB | 2287.3 MB | 4984.7 MB |
| 只修日志（ParameterizedQueries） | 0.0 MB | 1561.6 MB | 2856.8 MB |
| 改后（再 + 8 MB 限批） | 0.0 MB | **289.4 MB** | 2304.6 MB |
| 强制触发日志·改前（SlowThreshold=1ns） | **288.0 MB** | 1765.9 MB | 5053.5 MB |
| 强制触发日志·改后 logger | **0.0 MB** | 1249.9 MB | 2545.0 MB |

结论：

1. **日志字节 ≈ 载荷字节**（288.0 MB / 288 MB），证实运维记录的"单条日志可达 12MB"
   （12 MB = 50 行 × 240 KB 均值），`ParameterizedQueries` 将其归零。
2. 但**日志是否触发取决于语句是否超过 `SlowThreshold`（默认 200ms）**：3 × 5 MB 的
   小载荷实测日志为 0 字节，12 × 24 MB 才触发。故日志修复是**条件性**收益。
3. **限批才是峰值的主刀**：1561.6 → 289.4 MB（5.4×）。
4. 改前整体 2287.3 → 改后 289.4 MB = **7.9×**。

### A.3 请求路径：单请求 30 MB，真实 gateway 二进制

| | 基线 RSS | 峰值 RSS | 落库正文 |
|---|---|---|---|
| 改前（origin/main） | 48 MB | 424.0 MB | **500,111 字节**（被截断） |
| 改后（本分支） | 47 MB | 391.7 MB | **31,457,350 字节**（完整） |

- **完整性 63× 提升**：改前 `MaxStringLenInTrace=500_000` 把 30 MB 的 `content`
  截成 500 KB；改后大正文直存，**逐字节完整**。
- **内存仅降 7.6%**：trace 路径不是请求路径的内存主项。

### A.4 并发路径：8 并发 × 30 MB（复刻事故场景）

| 配置 | 峰值 RSS | 落库 | 正文完整性 | trace 丢弃 |
|---|---|---|---|---|
| 改前 | **1548.3 MB** | 8 行 | 4 MB（250 KB/行，截断） | 0 |
| 改后 · 预算 256 MB | 1455.8 MB | 5 行 | 157 MB（完整） | **3** |
| 改后 · 预算 96 MB | **923.4 MB** | 8 行 | 252 MB（完整） | **0** |

结论（**本方案最重要的修正**）：

1. **改前的 1548 MB 就是生产 OOM 的复现**——1 GiB 上限下必然被杀，与事故现象一致。
2. **trace 路径改动几乎不影响端到端峰值**（1548 → 1456 MB，仅 −6%）。
   请求/转发路径才是主项。
3. **body budget 常量是决定性杠杆**：256 → 96 MB 使峰值降到 923 MB（−37%），
   同时把丢包从 3 条降到 0 条。
4. 实测放大倍数 **≈ 9.6×**（(923−55) MB / 90 MB 在途正文），是早期 2.5× 假设的近 4 倍。
5. 故 256 MB 预算**在两个意义上都是错的**：既兜不住内存，又因正文总量冲垮 128 MB
   trace 队列而丢数据。

### A.5 实测额外发现的两处缺陷（已修）

| 缺陷 | 性质 | 修复 |
|---|---|---|
| `Run` 关停分支用已取消的 ctx 去 flush，最后一批 trace 每次优雅关停必丢（蓝绿部署必现） | **既有** | `context.WithoutCancel` + 5s 超时（commit `ee5c6bd7`） |
| `extractModelParam` 第 2 步只认 `trace_req_body`，大正文落到第 3 步再 `io.ReadAll` 一遍 | **本方案引入** | 同时认 `trace_req_body_raw` |

### A.6 本附录对第 2 节"目标"的修正

第 2 节给出的"单请求峰值 95 MB → ~30 MB"**仅对 trace 捕获路径成立**
（已由 `TestTraceCapture_LargeBody_SkipsJSONUnmarshalAndMarshal` 实测 1.03× body 佐证）。
**端到端单请求峰值**从 424 MB 只降到 392 MB，因为转发路径（`json.Marshal(core.ChatRequest)`）
与 handler 的 `ShouldBindJSON` 才是主项。要真正把端到端压到 1× body，必须改转发为字节级透传——
那是独立且更大的改动，不在本方案范围。
