# 运维看板分步执行计划

> 目标：让 sunny-canyon 的 Ops Dashboard 在功能和视觉上接近 sub2api  
> 总工时预估：6-9 天  
> 前置条件：已生成对比报告 `docs/ops-dashboard-comparison-report.md`

---

## 执行阶段总览

| Phase | 主题 | 工时 | 风险 |
|-------|------|------|------|
| 1 | 后端 API 补丁（缺失字段 + 聚合接口） | 1.5 天 | 低 |
| 2 | 前端 Header 交互增强 | 1 天 | 低 |
| 3 | 布局调整 + 全屏浮动工具栏 | 0.5 天 | 低 |
| 4 | 系统指标栏补全 + 并发队列深度 | 1 天 | 中 |
| 5 | WebSocket 实时 QPS 推送 | 2.5 天 | 高 |
| 6 | 数值准确性 & 端到端校准 | 1 天 | 中 |
| 7 | 测试验证 & 收尾 | 1 天 | 低 |

---

## Phase 1：后端 API 补丁

**目标**：修复前端类型声明与后端返回数据的字段不匹配问题；新增缺失的聚合接口。

### Step 1.1 — 修复 OpsDashboard 返回字段缺失

**文件**：`internal/controlplane/handler/ops_handler.go`

**问题**：前端 `OpsOverview.errors` 期望 `upstream_errors_excl_429_529` 和 `rate_limit_429_529`，但后端未返回这些字段。

**动作**：
1. 在 `GetDashboard` handler 的 errors 聚合查询中，增加：
   - `upstream_errors_excl_429_529`：统计 `status NOT IN (429, 529) AND status >= 500` 的上游错误数
   - `rate_limit_429_529`：统计 `status IN (429, 529)` 的数量
2. 在 `OpsErrors` 响应 struct 中新增这两个字段

**验证**：调用 `GET /api/v1/ops/dashboard?time_range=1h`，确认 JSON 包含新字段

### Step 1.2 — 修复 OpsConcurrencyStats 返回字段缺失

**文件**：`internal/controlplane/handler/ops_handler.go`

**问题**：前端 `OpsProviderConcurrency` 期望 `waiting_queue`、`max_capacity`，但后端只返回 `provider`、`active`、`error_rate`、`request_count`。

**动作**：
1. 在 `GetConcurrencyStats` 的 Redis 读取逻辑中，解析 `max_capacity`（从 provider config 获取）
2. 在 `concurrency:provider:*` 的 Redis value 中增加 `waiting` 字段（需同步改 gateway 写入侧）
3. 或者：从 provider 配置读取 `max_capacity`，`waiting_queue = max(0, active - max_capacity)` 估算

**验证**：调用 `GET /api/v1/ops/concurrency`，确认返回包含 `waiting_queue` 和 `max_capacity`

### Step 1.3 — Overview 中增加 jobs 字段

**文件**：`internal/controlplane/handler/ops_handler.go`

**问题**：前端 `OpsOverview` 期望 `jobs?: OpsJobHeartbeat[]`，但当前 `GetDashboard` 不返回此字段。

**动作**：
1. 在 `GetDashboard` 中复用 `ListJobs` service 获取最近 5 条 job heartbeat
2. 在 overview 响应中加入 `jobs` 字段

**验证**：dashboard API 返回 `jobs` 数组

### Step 1.4 — 修复 resolve 路由未注册

**文件**：`internal/controlplane/router/router.go`

**问题**：前端调用 `PUT /ops/request-errors/:id/resolve` 和 `PUT /ops/upstream-errors/:id/resolve`，但路由表中只有 `PUT /ops/error-logs/:id/resolve`。

**动作**：
1. 在 ops 路由组中增加两条路由：
   ```go
   ops.PUT("/request-errors/:id/resolve", h.ResolveRequestError)
   ops.PUT("/upstream-errors/:id/resolve", h.ResolveUpstreamError)
   ```
2. 在 `ops_handler.go` 中新增两个 handler 方法（可复用 `ResolveErrorLog` 逻辑，区分 error type）

**验证**：`PUT /api/v1/ops/request-errors/:id/resolve` 返回 200

### Step 1.5 — 新增 snapshot-v2 聚合接口

**文件**：`internal/controlplane/handler/ops_handler.go`

**目标**：减少首屏请求数，合并 overview + throughput_trend + error_trend 为一次请求。

**动作**：
1. 注册新路由 `GET /ops/dashboard/snapshot-v2`
2. Handler 内部并行调用：
   - `GetDashboard` 逻辑（overview）
   - `GetThroughputTrend` 逻辑
   - `GetErrorTrend` 逻辑
3. 合并为一个 JSON 响应返回
4. 前端 `ops.ts` 新增 `getSnapshotV2()` 函数

**验证**：`GET /api/v1/ops/dashboard/snapshot-v2?time_range=1h` 一次返回三个数据块

---

## Phase 2：前端 Header 交互增强

**目标**：补齐查询模式切换、暂停/恢复按钮。

### Step 2.1 — Header 添加查询模式下拉

**文件**：`web/src/views/admin/ops/components/OpsDashboardHeader.vue`

**动作**：
1. 在 `ops-filters` 区域，time-range select 后面增加一个 `el-select`：
   - 选项：`全部(auto)` / `原始数据(raw)` / `预聚合(preagg)`
2. 新增 prop `queryMode: string` 和 emit `update:queryMode`
3. 父组件 `OpsDashboardPage.vue` 接收并传递给 API 请求

**文件**：`web/src/api/ops.ts`

**动作**：
1. 所有 fetch 函数的 `time_range` 参数旁增加可选 `mode` 参数
2. 后端在 ops_handler 的 time-range 解析处读取 `mode` query 参数（即使暂不区分逻辑也先接受）

**验证**：Header 下拉切换后 URL query 参数更新、API 请求带 `?mode=raw`

### Step 2.2 — Header 添加自动刷新暂停/恢复按钮

**文件**：`web/src/views/admin/ops/components/OpsDashboardHeader.vue`

**动作**：
1. 在 auto-refresh 文字旁增加一个切换按钮（`el-switch` 或 toggle button）
2. 新增 prop `autoRefresh: boolean` 和 emit `update:autoRefresh`
3. 暂停时倒计时停止，恢复时重新从 `settings.auto_refresh_interval` 开始
4. 按钮图标使用 Pause/Play 语义

**文件**：`web/src/views/admin/ops/OpsDashboardPage.vue`

**动作**：
1. `autoRefresh` 改为双向绑定 Header
2. 暂停状态下 `countdownTimer` 不递减

**验证**：点击暂停 → 倒计时冻结；点击恢复 → 倒计时从完整值重新开始

### Step 2.3 — 首屏改用 snapshot-v2

**文件**：`web/src/views/admin/ops/OpsDashboardPage.vue`

**动作**：
1. `fetchAllData()` 中优先调用 `opsAPI.getSnapshotV2(timeRange)` 获取 overview + throughput + error trend
2. 降级：如果 snapshot-v2 失败，回退到原来的三个并行请求
3. 保留 latency histogram 和 error distribution 的独立请求

**验证**：浏览器 Network 面板，首屏请求从 6 个减少到 3-4 个

---

## Phase 3：布局调整 & 全屏浮动工具栏

**目标**：布局结构对齐 sub2api 的 4 列首行模式；全屏模式补充浮动控制栏。

### Step 3.1 — 第一行从 2 列改为 4 列

**文件**：`web/src/views/admin/ops/OpsDashboardPage.vue`

**当前布局**：
```
Row 1 (ops-row-duo, 1:1):  并发卡 | 切换率
Row 2 (全宽):               流量趋势
Row 3 (1:1:1):              延迟 | 错误分布 | 错误趋势
```

**目标布局**（对齐 sub2api `lg:grid-cols-4`）：
```
Row 1 (4-col):  并发(1) | 切换率(1) | 吞吐量趋势(2)
Row 2 (1:1:1):  延迟 | 错误分布 | 错误趋势
```

**动作**：
1. 将 `OpsTrafficTrendChart` 合并到第一行，占 `col-span-2`
2. CSS 修改 `.ops-row-main` 为 `grid-template-columns: 1fr 1fr 2fr`
3. 删除原来独立的 "Row 2" 包裹 div
4. 原 Row 3 变成新的 Row 2

**验证**：1920px 宽屏下渲染为 4 列布局；1200px 以下自动堆叠为单列

### Step 3.2 — 全屏浮动工具栏

**文件**：`web/src/views/admin/ops/OpsDashboardPage.vue`

**当前**：全屏模式只有 ESC 退出。  
**目标**（对齐 sub2api `ops-fullscreen-bar`）：右上角固定浮动条，含：
- 时间范围 select
- 刷新按钮
- 退出全屏按钮

**动作**：
1. 已有 `ops-fullscreen-bar` 代码但内容过于简单，增强为：
   - 复制 toolbar 中的 `el-select`（时间范围）
   - 刷新按钮带 loading 状态
   - "退出全屏" 文字按钮
2. 添加 backdrop-filter 和 z-index 确保覆盖图表

**验证**：进入全屏后右上角出现浮动控制栏，可操作时间切换和刷新

---

## Phase 4：系统指标栏补全

**目标**：新增并发队列深度、DB/Redis 健康布尔值到 System Metrics Bar。

### Step 4.1 — 后端采集并发队列深度

**文件**：`internal/gateway/middleware/usage_collector.go` 或 `internal/gateway/observability/collector.go`

**动作**：
1. 在 gateway 的请求处理流水线中，增加并发队列深度统计：
   - 使用 `sync/atomic` 计数器，middleware 入口 +1，出口 -1
   - 每 30s 采样写入 Redis key `ops:queue_depth:{minute}` (10min TTL)
2. 或者更简单：从 scheduler 的 provider pool 中读取每个 provider 的当前 queue 深度并取 max/sum

**文件**：`internal/controlplane/service/system_metrics_collector.go`

**动作**：
1. 在 30s 采样循环中，读取 Redis `ops:queue_depth:*` 取当前值
2. 写入 `SystemMetricsSnapshot` 新增字段 `concurrency_queue_depth`

### Step 4.2 — 后端 DB/Redis 健康布尔值

**文件**：`internal/controlplane/service/system_metrics_collector.go`

**动作**：
1. 在采样循环中增加：
   - `db_ok`：对 DB 执行 `SELECT 1`（或取连接池 Ping），布尔值
   - `redis_ok`：对 Redis 执行 `Ping()`，布尔值
2. 写入 `SystemMetricsSnapshot` 新字段

### Step 4.3 — 前端展示

**文件**：`web/src/views/admin/ops/components/OpsDashboardHeader.vue`

**动作**：
1. System Metrics Bar 新增 3 个 `.ops-system-item`：
   - **并发队列**：显示 `concurrency_queue_depth` 数字，>0 时黄色，>10 红色
   - **DB 健康**：绿色圆点 "✓" / 红色 "✗"
   - **Redis 健康**：绿色圆点 "✓" / 红色 "✗"

**文件**：`web/src/api/ops.ts`

**动作**：
1. `OpsSystemMetrics` interface 增加 `concurrency_queue_depth`、`db_ok`、`redis_ok`

**验证**：System Metrics Bar 显示 8 项指标（原有 5 + 新增 3）

---

## Phase 5：WebSocket 实时 QPS 推送

**目标**：后端广播 QPS/TPS 实时更新到前端，替代部分轮询。

### Step 5.1 — 后端 WebSocket 端点

**文件**：`internal/controlplane/handler/ops_ws_handler.go`（新建）

**动作**：
1. 新增依赖 `nhooyr.io/websocket` (或 `github.com/gorilla/websocket`)
2. 创建 `OpsWSHandler` struct，注册路由 `GET /ops/ws/qps`
3. WebSocket 升级处理：
   - 从请求头 `Sec-WebSocket-Protocol` 提取 JWT token（格式：`["ops-admin", "jwt.<token>"]`）
   - 验证 JWT
   - 升级为 WebSocket 连接
4. 启动 goroutine 每 3 秒从 Redis `ops:qps:{current_minute}` 读取最新值，推送 JSON：
   ```json
   {"qps": 12.5, "tps": 3400, "timestamp": "..."}
   ```
5. 管理连接池：支持 max 100 并发连接，idle timeout 5min，stale detection 2min

**文件**：`internal/controlplane/router/router.go`

**动作**：在 ops 路由组增加：
```go
ops.GET("/ws/qps", h.HandleQPSWebSocket)
```

### Step 5.2 — Gateway 侧 QPS Redis 写入确认

**文件**：`internal/gateway/middleware/usage_collector.go`

**动作**：
1. 确认 `ops:qps:{YYYYMMDDHHMM}` 写入逻辑已按分钟粒度持续更新
2. 确认 TTL 为 10min
3. 如有必要增加 `ops:tps:{YYYYMMDDHHMM}` 同步写入

### Step 5.3 — 前端 WebSocket 客户端

**文件**：`web/src/api/ops.ts`

**动作**：
1. 新增 `subscribeQPS()` 函数，返回 unsubscribe 函数
2. 实现：
   - 自动重连（指数退避 1s→30s，max 20 次）
   - 在线/离线感知（`navigator.onLine` + `online`/`offline` 事件）
   - Stale 检测（2 分钟无消息则关闭重连）
   - 状态回调：`onStatusChange(status: 'connecting' | 'connected' | 'reconnecting' | 'offline' | 'closed')`
3. JWT token 从 `localStorage.getItem('token')` 获取

### Step 5.4 — 前端 Header 集成 WebSocket

**文件**：`web/src/views/admin/ops/components/OpsDashboardHeader.vue`

**动作**：
1. 在 `onMounted` 中调用 `subscribeQPS()` 订阅实时更新
2. 收到消息时更新 `currentQPS` 和 `currentTPS` 的 ref
3. 在 "实时流量" 区域增加连接状态小图标：
   - 绿色闪烁 = connected
   - 黄色 = reconnecting  
   - 灰色 = offline/closed
4. `onUnmounted` 中调用 unsubscribe

**文件**：`web/src/views/admin/ops/OpsDashboardPage.vue`

**动作**：
1. WebSocket 连接成功后，将 `getRealtimeTraffic` 轮询频率降低或移除（仅保留 dashboard overview 轮询）

**验证**：
1. 有请求流量时，QPS/TPS 数字每 3 秒自动跳动（非等待 30s 刷新）
2. 断网后自动重连，恢复后数据继续更新
3. Network 面板可见 WebSocket 连接 `ws://localhost:8081/api/v1/ops/ws/qps`

---

## Phase 6：数值准确性 & 端到端校准

**目标**：确保各项数值与 sub2api 的计算口径一致。

### Step 6.1 — Health Score 算法对齐

**检查点**：
1. sub2api 的健康评分公式（推测）：
   - SLA 权重 40%
   - 内存使用率倒数权重 20%
   - CPU 使用率倒数权重 20%
   - Goroutine 健康度权重 20%
2. 确认 sunny-canyon 的 `health_score` 计算逻辑
3. 如有偏差，调整 `internal/controlplane/service/system_metrics_collector.go` 中的评分函数

**验证**：同一时间段、相同流量下，两边健康评分差距 < 5

### Step 6.2 — 延迟百分位精度

**检查点**：
1. 确认 `call_logs.duration_ms` 和 `call_logs.first_token_ms` 的写入时点
2. 确认 P50/P90/P95/P99 的计算方式（ORDER BY + percentile 近似 vs 精确分位）
3. sub2api 可能在 Redis 中维护滑动窗口，sunny-canyon 走 DB 查询——确认精度差异可接受

**动作**：如有精度问题，考虑在 gateway 的 batch writer 中增加直方图近似（HDR Histogram 或简单的 bucket 计数）

### Step 6.3 — 错误分类口径对齐

**检查点**：
1. sub2api 错误分类基于 `phase`（upstream / business / gateway）+ `error_owner` + `error_source`
2. sunny-canyon 基于 `status` 码 + `upstream_status` 做分类
3. 确认 429/529 排除逻辑在两边一致

**动作**：审查 `GetDashboard` 和 `GetErrorDistribution` 的 SQL 查询，确保分类规则一致

### Step 6.4 — QPS/TPS 峰值统计

**问题**：前端显示 `peakQPS` / `peakTPS`，但 DB 无独立 peak 存储。

**动作**：
1. 确认 Redis 中是否保留每分钟的 QPS 计数用于取 max
2. 在 `GetDashboard` 或 `GetRealtimeTraffic` 中，遍历 `ops:qps:*` 所有 key 取最大值作为 peak
3. 如果 key 已过期无法取到，则在 `SystemMetricsCollector` 中维护一个 in-memory 滚动 max

---

## Phase 7：测试验证 & 收尾

### Step 7.1 — 前端组件测试

**动作**：
1. 更新 `web/src/views/admin/ops/components/__tests__/OpsDashboardPage.spec.ts`
2. 新增 snapshot-v2 降级逻辑的测试
3. 新增 WebSocket subscribeQPS 的 mock 测试
4. 新增暂停/恢复自动刷新的交互测试

### Step 7.2 — 后端 API 测试

**动作**：
1. 确认 `make test` 通过
2. 新增 ops_handler 中 resolve route 的集成测试
3. snapshot-v2 聚合接口的性能基准测试

### Step 7.3 — 端到端验证清单

| 验证项 | 方法 |
|--------|------|
| 首屏加载 < 3s | Chrome DevTools Network |
| QPS 实时更新延迟 < 5s | 对比发请求 → WS 推送时间 |
| 暂停/恢复刷新正常 | 手动操作 + 观察倒计时 |
| 全屏浮动工具栏可用 | 全屏模式操作验证 |
| 系统指标 8 项全显示 | 目视检查 |
| 错误分类数值一致 | 同一请求对比两个系统 |
| lint / build / test 全绿 | `make lint && cd web && npx vite build && make test` |

---

## 风险与回退策略

| 风险 | 影响 | 回退 |
|------|------|------|
| WebSocket 依赖引入导致 Go build 失败 | Phase 5 全部受阻 | 先 PR 依赖引入，确认 CI 通过后再合并 handler 代码 |
| 并发队列深度采集改动 gateway 主路径 | 可能影响请求延迟 | 使用 atomic counter 无锁操作，overhead < 1μs |
| snapshot-v2 聚合查询超时 | 首屏白屏 | 前端保留 fallback 到三次独立请求 |
| resolve 路由新增与已有 `error-logs/:id/resolve` 冲突 | 路由冲突 | 两个路由共存，handler 内部复用 |
| 查询模式 preagg 后端暂不实现 | 下拉选项无实际效果 | 后端接受参数但忽略，前端先显示 "auto" 为默认 |

---

## 文件变更汇总

### 后端变更
| 文件 | 变更类型 | Phase |
|------|---------|-------|
| `internal/controlplane/handler/ops_handler.go` | 修改 | 1, 4 |
| `internal/controlplane/handler/ops_ws_handler.go` | **新建** | 5 |
| `internal/controlplane/router/router.go` | 修改 | 1, 5 |
| `internal/controlplane/service/system_metrics_collector.go` | 修改 | 4, 6 |
| `internal/gateway/middleware/usage_collector.go` | 修改 | 5 |

### 前端变更
| 文件 | 变更类型 | Phase |
|------|---------|-------|
| `web/src/api/ops.ts` | 修改 | 2, 4, 5 |
| `web/src/views/admin/ops/OpsDashboardPage.vue` | 修改 | 2, 3, 5 |
| `web/src/views/admin/ops/components/OpsDashboardHeader.vue` | 修改 | 2, 3, 4, 5 |

### 测试变更
| 文件 | 变更类型 | Phase |
|------|---------|-------|
| `web/src/views/admin/ops/components/__tests__/OpsDashboardPage.spec.ts` | 修改 | 7 |
| 后端 ops_handler_test.go | 修改 | 7 |
