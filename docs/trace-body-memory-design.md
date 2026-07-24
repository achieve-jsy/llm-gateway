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
const DefaultBodyBudgetBytes int64 = 256 << 20 // 256 MB

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

### D10 — 内存预算账（用于评审）

```
基线                              380 MB
在途请求体预算（D7）             ≤ 256 MB
trace 队列滞留（D6）             ≤ 128 MB
单次批量 SQL 文本（D5）          ≤   8 MB
────────────────────────────────────────
最坏同时存在                     ~772 MB   < 1 GiB（2g 有余量）
```

注意：队列中的正文是已完成请求的切片引用，**不**计入在途预算，故两者相加。

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
