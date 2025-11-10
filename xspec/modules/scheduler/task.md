# scheduler — 任务清单

## 阶段 1：环境与基础

- [x] T001 [P] P0 创建 package 骨架目录 `internal/gateway/scheduler/`，建立空 Go 文件占位（`types.go`、`errors.go`、`data.go`、`config.go`、`routetable.go`、`healthstore.go`、`failure_ring.go`、`event.go`、`router.go`、`config_syncer.go`、`health_prober.go`、`failover.go`、`scheduler.go`）、`go.mod` 依赖声明及 package-level doc 注释

- [x] T002 [P] P0 定义枚举与哨兵错误：在 `internal/gateway/scheduler/types.go` 定义 `RoutingStrategy`（`round_robin` / `priority` / `weighted`）及 `Valid()` 方法、`HealthStatus`（`healthy` / `unhealthy`）、`RouteDecision` 结构体；在 `internal/gateway/scheduler/errors.go` 定义 `ErrGroupNotFound`、`ErrNoAvailableModel`、`ErrNoOtherModel`、`ErrConfigFetchFailed`、`ErrInvalidConfig`

- [x] T003 [P] P0 定义全量数据模型：在 `internal/gateway/scheduler/data.go` 定义 `GroupRoutingConfig`、`ModelRoutingEntry`、`ChannelEndpoint`、`ModelHealthStatus`（含 `FailureWindow` 指针、`QuickFailCount`、`QuickFailWindowStart`、`LastProbeTime`、`LastFailTime`、`LastStateChange`、`CreatedAt`）、`GroupConfig`、`GroupModel`、`ControlPlaneConfig` 结构体，字段完全对齐 `data-model.md`

- [x] T004 [P] P0 定义配置项：在 `internal/gateway/scheduler/config.go` 定义 `SchedulerConfig` 结构体（`ControlPlaneURL`、`ConfigSyncInterval` 默认 5s、`HealthProbeInterval` 默认 30s、`MaxProbeConcurrency` 默认 50、`FailureThreshold` 默认 3、`RecoverySuccessThreshold` 默认 3、`QuickFailWindow` 默认 60s、`QuickFailThreshold` 默认 5、`ChannelProbeTimeout` 默认 5s）及 `DefaultConfig(controlPlaneURL)`

> ✅ 阶段 1 检查点：所有枚举、错误类型、数据结构及配置定义完备，Go build 通过

---

## 阶段 2：数据层

- [x] T005 P0 实现滑动窗口环形缓冲：在 `internal/gateway/scheduler/failure_ring.go` 实现 `FailureRingBuffer` 结构体及 `NewFailureRingBuffer(capacity int)`、`Record(t time.Time)`、`RecentFailures(window time.Duration) int` 方法，容量默认 100，按插入序从最新向最早遍历，遇到首个超出窗口即停止
  depends: T003

- [x] T006 P0 实现内存路由表：在 `internal/gateway/scheduler/routetable.go` 实现 `RouteTable` 结构体（`sync.RWMutex` + `map[string]*GroupRoutingConfig`）、`NewRouteTable()`、`Get(groupName string) *GroupRoutingConfig`（RLock）、`GetAllModels() []*ModelRoutingEntry`（RLock）、`ReplaceAll(config *ControlPlaneConfig)`（Lock 全量替换，过滤 `Enabled=false` 组，转换 `GroupConfig` → `GroupRoutingConfig`）
  depends: T003

- [x] T007 P0 实现健康状态存储：在 `internal/gateway/scheduler/healthstore.go` 实现 `HealthStore` 结构体（`sync.RWMutex` + `map[string]*ModelHealthStatus`）、`NewHealthStore()`、`Get(modelID string) *ModelHealthStatus`（RLock）、`FilterHealthy(models []*ModelRoutingEntry) []*ModelRoutingEntry`（RLock 过滤 Status==Healthy）、`RecordFailure(modelID string)`（Lock → 写入 FailureRingBuffer → ConsecutiveFailures++ → ConsecutiveSuccesses=0 → 滑动窗口 ≥3 或 60s 快速失败 ≥5 则标记 Unhealthy + 发布事件）、`RecordProbeSuccess(modelID string)`（Lock → ConsecutiveSuccesses++ → ConsecutiveFailures=0 → ≥3 且当前 Unhealthy 则标记 Healthy + 发布事件）、`RecordProbeFailure(modelID string)`（Lock → 写入 FailureRingBuffer → ConsecutiveFailures++ → ConsecutiveSuccesses=0 → ≥3 且当前 Healthy 则标记 Unhealthy + 发布事件）、`CleanOrphaned(activeModelIDs []string)`（Lock 清理不在活跃列表中的状态）
  depends: T003, T005

- [x] T008 [P] P0 实现事件发布器：在 `internal/gateway/scheduler/event.go` 定义 `HealthEvent` 结构体（`ModelID`、`GroupID`、`ModelName`、`OldStatus`、`NewStatus`、`Reason`、`Timestamp`）及 `EventPublisher` 结构体（异步 channel + 后台 goroutine 消费，`Publish(event HealthEvent)`、`Subscribe() <-chan HealthEvent`），新模型首次注册默认 Healthy
  depends: T002

- [x] T009 P0 实现配置同步器：在 `internal/gateway/scheduler/config_syncer.go` 实现 `ConfigSyncer` 结构体（`httpClient`、`cpBaseURL`、`routeTable *RouteTable`、`healthStore *HealthStore`、`pollInterval`、`stopCh`、`lastSyncAt`）、`NewConfigSyncer(cpBaseURL, routeTable, healthStore, interval)`、`Run()`（ticker 驱动 5s 循环）、`doSync()`（GET `{cpBaseURL}/api/v1/config/routing`，请求头 `X-Service: scheduler` + `X-Auth-Token: pre-shared`，超时 5s，解析 → 校验 → 调用 `RouteTable.ReplaceAll` → 调用 `HealthStore.CleanOrphaned`，失败不覆盖现有路由表并记录 warn，配置校验失败跳过并记录 error），含单元测试（mock HTTP server：合法 JSON 正确替换、HTTP 500 保留旧配置、非法 JSON 跳过）
  depends: T006, T007, T004

> ✅ 阶段 2 检查点：RouteTable、HealthStore（含状态转换逻辑）、FailureRingBuffer、EventPublisher、ConfigSyncer 全部实现并通过各自单元测试

---

## 阶段 3：功能实现

### 3.1 Router 核心

**目标**：实现路由决策引擎的基础调度框架，根据组策略类型分发到具体算法。

**独立验证方式**：对合法组名调用 `Route()`，验证返回的 `RouteDecision` 中 `Strategy` 字段与组配置一致；对不存在组名返回 `ErrGroupNotFound`；对全不健康组返回 `ErrNoAvailableModel`。

- [x] T010 P0 在 `internal/gateway/scheduler/router.go` 实现 `Router` 结构体、`NewRouter(routeTable *RouteTable, healthStore *HealthStore)`、`Route(groupName string) (*RouteDecision, error)`（RouteTable.Get → nil 返回 ErrGroupNotFound → HealthStore.FilterHealthy → 空返回 ErrNoAvailableModel → switch strategy 分发至 routeRoundRobin / routePriority / routeWeighted）、`SelectByStrategy(strategy RoutingStrategy, healthyModels []*ModelRoutingEntry) (*RouteDecision, error)`（供 FailoverHandler 调用），决策耗时写入 `DecisionTimeUs`
  depends: T006, T007

> ✅ 阶段 3 检查点：路由决策（轮询/优先级/加权）、故障切换、被动即时标记、定时健康探测、故障恢复、Scheduler 聚合全部实现
> ✅ US-RT-01/02/03 检查点：三种路由策略均可通过独立组配置验证 AC
> ✅ US-HP-03 检查点：被动失败评分和快速失败机制通过滑动窗口测试
> ✅ US-FO-01 检查点：排除失败模型重路由通过 AC-FO-01/02/03
> ✅ US-HP-01 检查点：主动探测 worker pool 限流 + 429 不计失败
> ✅ US-HP-02 检查点：连续成功恢复健康通过 AC-HP-03

---

## 阶段 4：集成与收尾

- [x] T019 P1 竞态检测测试：运行 `go test -race ./internal/gateway/scheduler/...`，覆盖 10 goroutine 并发 Route + 1 goroutine 定时写 RouteTable、10 goroutine 并发 NotifyFailure + 1 goroutine 定时 HealthProber.probeAll、Route 与 Failover 与 ConfigSyncer 并发执行，确保零 data race
  depends: T011, T012, T013, T014, T015, T016, T017

- [x] T020 [P] P1 安全加固：ConfigSyncer 添加预共享 Token 验证，HTTP 请求头 `X-Auth-Token` 值通过 `SchedulerConfig.CPAuthToken` 配置注入，control-plane 侧需校验匹配；添加 `internal/gateway/scheduler/config_syncer_test.go` 中 mock server 校验 Token 的测试用例
  depends: T009

- [x] T021 [P] P1 性能验证：编写 `internal/gateway/scheduler/bench_test.go` benchmark 用例（BenchmarkRouteRoundRobin、BenchmarkRoutePriority、BenchmarkRouteWeighted、BenchmarkFailover），确认路由决策 P99 < 10ms；验证 worker pool 并发上限 50 goroutine（启动 100 模型探测，确认同时执行 ≤ 50）；验证 ConfigSyncer 写锁持有时间 < 1ms
  depends: T018

- [x] T022 P1 与 gateway-api 集成联调：在 gateway-api 的 `main` 或初始化代码中创建 `Scheduler` 实例（注入 `SchedulerConfig`），在模型调用 Handler 中以 `scheduler.Route(groupName)` 替代硬编码模型选择，在调用失败时调用 `scheduler.NotifyFailure(modelID)` + `scheduler.Failover(groupName, failedModelID)`，验证正常路由 → 失败切换 → 健康恢复全链路闭环
  depends: T018

- [x] T023 P2 Code Review：检查全部代码（23 个任务覆盖的 13 个文件），重点审视并发安全（锁范围、atomic 使用）、错误处理完整性、哨兵错误语义一致性、配置校验健壮性、goroutine 泄漏风险（stopCh 关闭后是否全部退出）、日志级别合理性（warn/error 区分）
  depends: T022

> ✅ 阶段 4 检查点：性能通过、竞态检测通过、安全加固完成、与 gateway-api 集成联调通过、Code Review 完成
