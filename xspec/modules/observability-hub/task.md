# observability-hub 任务清单 — CHG-20260603-001

> 基线：xspec/changes/observability-hub/spec.md + design.md + data-model.md + contracts.md

---

## 阶段 1：环境与基础

- [x] T001 [P] P0 创建 observability-hub 进程入口 `cmd/observability-hub/main.go`
  - 加载 Config（GATEWAY_PORT、DATABASE_URL、REDIS_ADDR）
  - 初始化 GORM + Gin + cron 调度器
  - 注册 /health /ready 端点

- [x] T002 [P] P0 定义配置结构 `internal/observability/config/config.go`
  - USAGE_RETENTION_DAYS（默认 90）、ROLLUP_CRON（默认 `0 * * * *`）

✓ 检查点：服务启动成功，/health 返回 200，PG 连接正常

---

## 阶段 2：数据层

- [x] T003 P0 数据库迁移：创建 call_logs 表 `migrations/032_create_call_logs.sql`
  - DD 完整 DDL（含 4 个索引：key_ts / dept_ts / model_ts / ts）
  - request_id PRIMARY KEY
  - AC-TRACK-01

- [x] T004 [P] P0 数据库迁移：创建 usage_daily 表 `migrations/033_create_usage_daily.sql`
  - 复合主键 (stat_date, key_id, dept_id, studio_id, model)
  - 索引 idx_usage_daily_dept
  - AC-AGGR-01

- [x] T005 [P] P0 数据库迁移：call_logs 保留策略 `migrations/034_call_logs_retention.sql`（可选）
  - 明细 90 天清理策略

- [x] T006 P0 定义 call_logs GORM 模型 `internal/observability/model/call_log.go`
  - 映射 call_logs 表全部字段
  depends: T003

- [x] T007 [P] P0 定义 usage_daily GORM 模型 `internal/observability/model/usage_daily.go`
  depends: T004

- [x] T008 P0 实现 CallLogRepository.BatchInsert `internal/observability/repository/call_log_repo.go`
  - INSERT ... ON CONFLICT (request_id) DO NOTHING
  depends: T006

- [x] T009 [P] P0 实现 UsageDailyRepository `internal/observability/repository/usage_daily_repo.go`
  - UpsertDaily（整日重算覆盖）
  - Query（三级视角注入 WHERE + 可选过滤器 + GROUP BY）
  depends: T007

✓ 检查点：迁移执行成功，表结构 + 索引验证通过；Repository 单元测试通过

---

## 阶段 3：功能实现

### 3.1 US-AGGR-01 — 按天汇总用量 (P0)

> **目标**：每小时从 call_logs 聚合到 usage_daily，整日重算覆盖
> **独立验证**：插入一批 call_logs → 触发 rollup → 查询 usage_daily 验证聚合正确
> **AC 覆盖**：AC-AGGR-01, AC-AGGR-02

- [x] T010 P0 实现 RollupService 聚合逻辑 `internal/observability/service/rollup_service.go`
  - 整日重算覆盖语义：INSERT INTO usage_daily SELECT ... ON CONFLICT DO UPDATE
  - 聚合维度：date(ts) + key_id + dept_id + studio_id + model
  - 聚合指标：req_count / success_count / prompt/completion/cached/cache_write/reasoning tokens / est_cost_usd
  - 今日 + 昨日覆盖（跨零点延迟到达）
  depends: T008, T009

- [x] T011 P0 实现 RollupCron 调度 `internal/observability/cron/rollup_cron.go`
  - cron 表达式每小时触发
  - 调用 RollupService.Run(ctx)
  - 记录 rollup_duration_seconds、rollup_rows_upserted 指标
  depends: T010

✓ 检查点：手动插入测试 call_logs → 触发 rollup → usage_daily 聚合正确，重复执行结果一致（幂等）

### 3.2 US-COST-05 — 模型定价存储（内存价表） (P1)

- [x] T012 P1 实现 PricingTable 内存价表 `internal/observability/pricing/table.go`
  - map[modelKey]core.ModelPricing
  - Refresh(ctx) 从 control-plane 内部接口拉取
  - 5min cron 全量替换 atomic swap
  - 缺失模型返回 cost=0 + price_missing 计数
  depends: T007

✓ 检查点：启动后价表加载成功；查询价表能返回含缓存/阶梯价的完整定价

### 3.3 US-COST-01~04 — 成本查询 API (P0)

> **目标**：提供 GET /v1/usage 只读查询，三级数据视角
> **独立验证**：预置 usage_daily 数据 → 不同角色调用 → 验证返回范围正确
> **AC 覆盖**：AC-COST-03, AC-COST-04, AC-COST-05

- [x] T013 P0 实现 UsageQueryService `internal/observability/service/query_service.go`
  - 三级视角注入 WHERE（管理员/部门负责人/员工）
  - 参数校验：from/to 日期格式、范围 ≤ 90 天
  - 构建查询 SQL（拼接 scope_filter + optional_filters + GROUP BY + ORDER BY）
  depends: T009

- [x] T014 P0 实现 UsageHandler `internal/observability/handler/usage_handler.go`
  - GET /v1/usage 参数解析与校验
  - 调用 QueryService.Query(identity, params)
  - 返回 JSON 响应（含 total 汇总行）
  depends: T013

- [x] T015 P0 注册路由 `internal/observability/router/router.go`
  - GET /v1/usage → UsageHandler
  - 复用网关 AuthMiddleware 鉴权
  depends: T014

✓ 检查点：管理员查全量成功；员工仅返回自己的数据；部门负责人仅返回本部门数据

---

## 阶段 4：集成与收尾

- [x] T016 P1 单元测试：RollupService 聚合正确性 `observability/service/rollup_service_test.go`
  - 多维度聚合验证、整日重算幂等
  depends: T010

- [x] T017 [P] P1 单元测试：UsageQueryService 三级视角 `observability/service/query_service_test.go`
  - 越权拦截、空结果、大范围查询
  depends: T013

- [ ] T018 [P] P1 集成测试：端到端 rollup 链路 `tests/integration/rollup_test.go`
  - mock call_logs → rollup → 查询 usage_daily 验证
  depends: T011, T015

- [ ] T019 [P] P1 集成测试：三级视角越权防护 `tests/integration/usage_auth_test.go`
  - 员工/部门负责人/管理员三级视角验证
  depends: T015

- [ ] T020 P1 对账告警：定期校验 sum(usage_daily.est_cost) ≈ sum(call_logs.est_cost)
  - `internal/observability/service/reconciliation.go`
  depends: T010

- [x] T021 [P] P1 配置文档：observability-hub 部署配置 `config/observability-hub.yaml`
  depends: T001

- [ ] T022 P1 验收：对照 AC 清单逐项验证全部 14 条 AC
  depends: T019

✓ 检查点：全部 AC 通过；单元测试 + 集成测试覆盖率达标