# gateway-api 任务清单 — CHG-20260603-001

> 基线：xspec/modules/gateway-api/task.md

---

## 阶段 1：环境与基础

- [x] T001 [P] P0 定义 UsageEvent 数据结构 `internal/gateway/observability/event.go`
  - 含身份维度（key_id/user_id/dept_id/studio_id）+ 路由/模型 + Token 用量（含 cached/cache_write/reasoning）+ 状态 + 成本快照
  - AC-TE-01

- [x] T002 [P] P0 定义 Collector 配置结构 `internal/gateway/observability/config.go`
  - USAGE_COLLECT_ENABLED（默认 true）、USAGE_BUFFER_SIZE（默认 10000）、USAGE_FLUSH_N（默认 200）、USAGE_FLUSH_INTERVAL（默认 1s）

✓ 检查点：UsageEvent 结构编译通过，覆盖所有计费和身份字段

---

## 阶段 2：数据层

> gateway-api 无状态，不新增持久化。通过 GORM 写入 shared call_logs 表（表结构由 observability-hub 迁移 032 创建）。

- [x] T003 P0 定义 call_logs GORM 模型 `internal/gateway/observability/model/call_log.go`
  - 映射 call_logs 表全部字段（与 observability-hub 同定义）
  depends: T003@observability-hub

- [x] T004 P0 实现 CallLogRepository.BatchInsert `internal/gateway/observability/repository/call_log_repo.go`
  - INSERT ... ON CONFLICT (request_id) DO NOTHING
  - 批量积累 N=200 或 T=1s 后一次性写入
  depends: T003

✓ 检查点：BatchInsert 单元测试通过（幂等验证、批量积累触发）

---

## 阶段 3：功能实现

### 3.1 前置：身份解析 (P0)

> **目标**：AuthMiddleware 补全 key→user→dept→studio 解析链路
> **AC 覆盖**：AC-TE-01（含部门维度埋点）

- [x] T005 P0 AuthMiddleware 身份解析增强 `internal/gateway/middleware/auth.go`
  - 从 KeyCache 或回源 control-plane 解析 key_id/user_id/dept_id
  - 写入 model.UserIdentity（已有字段）到 gin.Context
  - Key 无效 → 401；身份缺失 → 放行，dept_id 记空
  - studio_id 预留置空

- [x] T006 P0 Orchestrator 流式 usage 回填 `internal/gateway/orchestrator/stream.go`
  - 注入/透传 stream_options.include_usage=true
  - HandleStreamResponse 收尾解析末尾 chunk 的 usage
  - 提取 prompt/completion/cached/cache_write/reasoning 写回 gin.Context
  - Anthropic：message_start/message_delta 累计并归一化
  - 上游不支持 include_usage → UsageMissing=true，token 记 0
  depends: T005

✓ 检查点：流式调用末尾能获取 usage 并正确提取 cached_tokens；Key 有效时 dept_id 非空

### 3.2 US-TRACK-EMIT-01 — 调用完成后异步埋点 (P0)

> **目标**：Logger 中间件 c.Next() 后构造 UsageEvent，锁价算成本，异步批量写入
> **独立验证**：一次模型调用完成 → 查询 call_logs 表存在对应记录
> **AC 覆盖**：AC-TE-01, AC-TE-02, AC-TE-03, AC-TE-04

- [x] T007 P0 实现 EstimateCost 成本估算 `internal/gateway/observability/cost.go`
  - 输入归一化 ExtractCostInput（OpenAI cached_tokens vs Anthropic cache_read/creation）
  - EstimateCost：缓存分桶 + 阶梯价 + 缺失回退
  - perM 单位换算（/1M tokens）
  - 缺价返回 cost=0 + missing=true
  - 异常截断：cached > prompt → uncached 截断为 0
  depends: T001

- [x] T008 P0 单元测试：EstimateCost golden 测试 `internal/gateway/observability/cost_test.go`
  - LiteLLM golden 向量（input=2.5, cache_read=0.25, output=15, 期望 cost=0.011684）
  - 边界：缺价 / cached>prompt / 无缓存 / 纯缓存 / 阶梯跨档 / 双字段归一化
  depends: T007

- [x] T009 P0 实现 Collector `internal/gateway/observability/collector.go`
  - NewUsageEvent(ctx)：从 gin.Context 提取全部字段构造 UsageEvent
  - estimateCost(event, pricing)：调用 EstimateCost 锁价
  - Emit(event)：非阻塞 send 到 channel，满则 dropped 计数
  depends: T007, T005

- [x] T010 P0 实现 batch writer `internal/gateway/observability/writer.go`
  - goroutine 消费 channel
  - 按 N=200 或 T=1s 批量累积
  - 调用 CallLogRepository.BatchInsert(ctx, batch)
  - 记录 metrics：UsageWritten / UsageWriteErrors
  depends: T004, T009

- [x] T011 P0 Logger 中间件集成埋点 `internal/gateway/middleware/logger.go`
  - c.Next() 后调用 collector.NewUsageEvent(ctx) → collector.Emit(event)
  - /health、/ready 及非 LLM 路径跳过
  - AC-TE-01, AC-TE-02
  depends: T009

✓ 检查点：一次成功调用 → call_logs 存在记录含 key_id/dept_id/cached_tokens/est_cost_usd；重复 request_id → ON CONFLICT DO NOTHING 不重复

### 3.3 配置与观测

- [x] T012 P0 注册观测指标 `internal/gateway/observability/metrics.go`
  - usage_events_emitted / dropped / written_total
  - usage_write_errors_total、usage_queue_depth
  - price_missing_total、usage_missing_total
  depends: T010

- [x] T013 [P] P0 集成到 gateway 启动链 `cmd/gateway/main.go`
  - 创建 Collector + 启动 batch writer goroutine
  - 注册到 Logger 中间件
  depends: T010, T011

✓ 检查点：gateway 启动后 /metrics 端点暴露所有观测指标

---

## 阶段 4：集成与收尾

- [ ] T014 P1 集成测试：埋点端到端 `tests/integration/usage_tracking_test.go`
  - 成功调用 → call_logs 记录正确
  - 失败调用 → token=0 仍记一行
  - 流式调用 → usage 回填正确（与非流式同口径）
  depends: T013, T003@observability-hub

- [ ] T015 [P] P1 集成测试：异步可靠性 `tests/integration/usage_async_test.go`
  - 高并发场景 channel 不阻塞热路径
  - 批量触发 FlushN 阈值
  - 幂等：重复 request_id 不重复计入
  depends: T013

- [ ] T016 P1 跨模块联调：与 observability-hub 验证完整链路
  - gateway 埋点 → call_logs 写入 → rollup → /v1/usage 查询成本
  depends: T015@observability-hub

✓ 检查点：全部 AC-TE-01~04 通过