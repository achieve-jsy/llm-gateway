# observability-hub 后端任务清单

## 文档信息

| 属性 | 内容 |
| :--- | :--- |
| **所属变更** | CHG-20260603-001 |
| **模块名称** | observability-hub |
| **模块类型** | web-backend |
| **创建时间** | 2026-06-04 |

---

## 任务格式

```
- [ ] T{NNN} [P?] {优先级} 任务描述（含具体文件路径）
  depends: T{NNN}, T{NNN}@{other-module}
```

| 标记 | 含义 |
| :--- | :--- |
| `T{NNN}` | 模块内唯一编号，从 T001 起编 |
| `[P]` | 可并行：操作不同文件、无依赖未完成任务 |
| 优先级 | P0（必须）/ P1（重要）/ P2（可选） |
| `depends:` | 依赖标注（缩进换行）。模块内：`T010`；跨模块：`T020@gateway-core`。无依赖则不写此行 |

---

## 阶段 1：环境与基础

**目标**：模块目录结构、DB 迁移、领域模型、Repository 就绪

- [ ] T001 [P] P0 模块目录初始化（新建 `internal/controlplane/observability/` 及其子目录 `service/`、`repository/`）
- [ ] T002 [P] P0 数据库迁移脚本 `migrations/NNN_usage_daily.sql`（新建 usage_daily 聚合表：stat_date/key_id/dept_id/studio_id/model/provider_type/is_stream 组合 PK，含 request_count/prompt_tokens/completion_tokens/reasoning_tokens/cached_tokens/avg_ttft_ms/p95_ttft_ms/avg_duration_ms/p95_duration_ms/prompt_cost/completion_cost/reasoning_cost/cached_cost/total_cost；新建 call_payloads 表对齐 data-model.md；新建对应索引：idx_usage_daily_date / idx_usage_daily_user_date / idx_usage_daily_dept_date / idx_usage_daily_model_date / uniq_usage_daily）
- [ ] T003 [P] P0 GORM 领域模型定义 `internal/controlplane/observability/model/`（新建：UsageDaily struct / CallPayload struct / AggregatedRow struct，对齐 data-model.md §实体定义）
  depends: T002

**✓ 检查点**：迁移可执行，GORM AutoMigrate 后 usage_daily / call_payloads 表就绪

---

## 阶段 2：数据层

**目标**：Repository 层封装，Service 层可依赖

- [ ] T010 [P] P0 UsageDailyRepository `internal/controlplane/observability/repository/usage_daily_repo.go`（新建：UpsertBatch 批量 ON CONFLICT DO UPDATE 覆盖语义；QueryByScope 按 scope + filters 条件查询含 GROUP BY；GetReconcileData 对账 SUM 查询）
  depends: T003
- [ ] T011 [P] P0 CallLogsRepository `internal/controlplane/observability/repository/call_logs_repo.go`（新建：AggregateByWindow 聚合查询含 percentile_cont(0.95) P95 计算和分桶 SUM；GetReconcileData 对账 SUM 查询；只读）
  depends: T003
- [ ] T012 [P] P0 CallPayloadsRepository `internal/controlplane/observability/repository/call_payloads_repo.go`（新建：FindByRequestID 按 request_id 查找；FindByRequestIDAndUser JOIN call_logs 校验 user_id）
  depends: T003

**✓ 检查点**：Repository 方法可编译，单元测试 mock 可用

---

## 阶段 3：功能实现

**目标**：按用户故事实现——成本引擎 → Rollup 聚合 → 查询 API → 内容检索 → 对账

### US-08 — 多桶成本估算引擎 (P0)

**目标**：CostEstimator 接收分桶 Token + 四桶单价，输出分桶成本，含 reasoning 降级逻辑
**独立验证**：输入 prompt=2192, completion=736, reasoning=3412 及对应单价，验证四桶 cost 输出正确
**对应验收标准**：AC-09, AC-10, AC-13

- [ ] T020 P0 CostEstimator 实现 `internal/controlplane/observability/cost_estimator.go`（新建；func EstimateCost(tokens TokenBuckets, pricing ModelPricing) CostBuckets；prompt_cost = tokens × pricing.PromptPrice / 1M；completion_cost = tokens × pricing.CompletionPrice / 1M；reasoning_cost：pricing.ReasoningPrice 非 nil 用独立价，否则降级 CompletionPrice + 标记 fallback；cached_cost：pricing.CachedPrice 非 nil 计算，否则 0）
  depends: T001@control-plane（依赖 control-plane ModelPricing struct）
- [ ] T021 [P] P0 单元测试 `tests/controlplane/observability/cost_estimator_test.go`（Cover: 四桶正常计算、reasoning 降级、cached null 计 0、token=0 时 cost=0）
  depends: T020

**✓ 检查点**：CostEstimator 四桶输出正确，降级逻辑覆盖

---

### US-01 — 按天聚合 Rollup (P0)

**目标**：每小时从 call_logs 聚合到 usage_daily，幂等 upsert，含分桶 Token/延迟 P95/成本分桶
**独立验证**：在 call_logs 中插入当日 100 条模拟数据，手动触发 Rollup，验证 usage_daily 聚合行正确且 SUM 对账一致
**对应验收标准**：AC-01, AC-02, AC-11

- [ ] T030 P0 RollupCron 实现 `internal/controlplane/observability/rollup_cron.go`（新建；ticker 每小时触发；聚合窗口今天+昨天；从 CallLogsRepository.AggregateByWindow 获取聚合结果；统计 rollup_duration_seconds + rollup_rows_upserted 指标；启动时注册到 control-plane main goroutine）
  depends: T010, T011
- [ ] T031 [P] P0 RollupService `internal/controlplane/observability/service/rollup_service.go`（新建；Execute 方法：调用 Repository 聚合 + 批量 Upsert；返回统计信息行数+耗时）
  depends: T030
- [ ] T032 P0 Rollup 幂等验证（集成测试：同区间两次执行结果一致，ON CONFLICT DO UPDATE 覆盖语义正确）
  depends: T031

**✓ 检查点**：Rollup 每小时自动执行，usage_daily 聚合结果与 call_logs SUM 一致，重复执行安全

---

### US-02/03/04 — 只读用量查询 API (P0/P1)

**目标**：提供 GET /v1/usage 接口，支持多维度过滤（from/to/model/group_by），三级 RBAC 视角强制注入
**独立验证**：管理员查询返回全量；普通员工仅返回自身数据；部门负责人仅返回本部门
**对应验收标准**：AC-03, AC-04, AC-05, AC-12

- [ ] T040 P0 ViewInjector 中间件 `internal/controlplane/middleware/view_injector.go`（新建；从 JWT 提取 role/user_id/dept_id；admin 无过滤；dept_manager 注入 scope{dept_id}；user 注入 scope{user_id}；将 scope 写入 Gin Context 供 Handler/Service 使用）
- [ ] T041 P0 UsageService 实现 `internal/controlplane/observability/service/usage_service.go`（新建；Query(ctx, scope, filters) 方法：scope 条件二次校验注入 SQL WHERE；按 group_by 聚合查询 UsageDailyRepository；计算 total UsageSummary；GetTrend 按日返回趋势点）
  depends: T010, T040
- [ ] T042 P0 UsageQueryHandler `internal/controlplane/handler/usage_handler.go`（新建；QueryUsage 方法：解析 query params from/to/group_by；校验 from ≤ to 且 ≤ 90 天；调用 UsageService.Query；序列化 UsageResponse JSON）
  depends: T041
- [ ] T043 P0 路由注册 `internal/controlplane/router/router.go`（在 api := r.Group("/api/v1") 下注册 `api.GET("/usage", usageHandler.QueryUsage)`，JWT 中间件 + ViewInjector）
  depends: T042
- [ ] T044 [P] P1 单元测试 `tests/controlplane/handler/usage_handler_test.go`（Cover: 合法参数返回 200、from > to 返回 400、范围 > 90 天返回 400、无效 group_by 默认 day）
  depends: T042
- [ ] T045 [P] P1 单元测试 `tests/controlplane/middleware/view_injector_test.go`（Cover: admin 无过滤、dept_manager 注入 dept_id、user 注入 user_id）
  depends: T040

**✓ 检查点**：GET /v1/usage 可查询，三级 RBAC 视角正确，参数校验生效

---

### US-05/06 — 调用内容记录与检索 (P1)

**目标**：提供 GET /v1/usage/payload/:request_id 接口，管理员可查任意，普通员工仅查自身
**独立验证**：管理员查他人 request_id 返回内容；普通员工查他人返回 403
**对应验收标准**：AC-06, AC-07

- [ ] T050 P1 PayloadService `internal/controlplane/observability/service/payload_service.go`（新建；GetByRequestID(ctx, requestID, userID, role) 方法：admin 直接 FindByRequestID；其他角色 FindByRequestIDAndUser JOIN call_logs 校验 user_id；不存在返回 NotFoundError；无权限返回 ForbiddenError）
  depends: T012, T040
- [ ] T051 P1 PayloadHandler `internal/controlplane/handler/payload_handler.go`（新建；GetPayload 方法：提取 path param request_id → 调用 PayloadService.GetByRequestID；200 返回 PayloadData JSON；404 返回 E4041；403 返回 E4031；审计日志记录访问）
  depends: T050
- [ ] T052 P1 路由注册 `internal/controlplane/router/router.go`（在 api Group 下注册 `api.GET("/usage/payload/:request_id", payloadHandler.GetPayload)`，JWT + ViewInjector）
  depends: T051

**✓ 检查点**：GET /v1/usage/payload/:id 权限校验正确，审计日志记录

---

### US-07 — 数据对账 (P1)

**目标**：每日凌晨对账 usage_daily ↔ call_logs，偏差 > $0.01 触发告警
**独立验证**：修改 call_logs 某行 cost 后触发对账，验证偏差告警
**对应验收标准**：AC-08

- [ ] T060 P1 Reconciler 实现 `internal/controlplane/observability/reconciler.go`（新建；每日凌晨触发：SELECT SUM(total_cost) FROM usage_daily WHERE stat_date = yesterday vs SELECT SUM(total_est_cost) FROM call_logs WHERE stat_date = yesterday；分桶对账 reasoning_cost / cached_cost；偏差 > $0.01 触发 Metrics Alert；记录对账结果到日志）
  depends: T010, T011

**✓ 检查点**：对账触发正确，偏差 > 阈值时告警

---

## 阶段 4：集成与收尾

**目标**：编译验证、测试、宪章合规

- [ ] T070 P0 编译验证 `go build ./cmd/control-plane` + `go test ./internal/controlplane/observability/... -count=1`
  depends: T043, T052
- [ ] T071 [P] P0 集成测试 `tests/controlplane/observability/rollup_integration_test.go`（Cover: Rollup 端到端聚合 + 对账一致）
  depends: T032
- [ ] T072 [P] P1 AI Slop 清理（检查新增文件无 AI 代码异味）
  depends: T070
- [ ] T073 [P] P1 性能验证（GET /v1/usage P95 响应 < 200ms，usage_daily 单日 10 万行；Rollup 执行 < 60s）
  depends: T070

**✓ 检查点**：所有测试通过，编译无警告，性能达标

---

## 交付策略

### MVP 优先（仅 P0）
1. 完成阶段 1（T001-T003）+ 阶段 2（T010-T012）
2. 完成 US-08 CostEstimator（T020）→ 成本引擎就绪
3. 完成 US-01 Rollup（T030-T032）→ 聚合管道就绪
4. 完成 US-02/03/04 查询 API（T040-T043）→ 用量可查
5. MVP 可验证：Rollup 正确聚合 + GET /v1/usage 返回分桶成本+延迟

### 场景间依赖
- US-08 (CostEstimator)：无场景间依赖（先于 Rollup 完成）
- US-01 (Rollup)：依赖 Phase 2 Repository + US-08 CostEstimator
- US-02/03/04 (查询 API)：依赖 US-01 Rollup 的 usage_daily 表
- US-05/06 (内容检索)：依赖 Phase 2 CallPayloadsRepository
- US-07 (对账)：依赖 US-01 Rollup 稳定运行后

---

## 任务统计

| 指标 | 数值 |
| :--- | :--- |
| 总任务数 | 24 |
| P0（必须） | 16 |
| P1（重要） | 8 |
| P2（可选） | 0 |
| 可并行任务 | 12 |
| 预计工时 | 36h |
