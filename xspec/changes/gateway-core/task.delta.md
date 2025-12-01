# gateway-core 后端任务清单

## 文档信息

| 属性 | 内容 |
| :--- | :--- |
| **所属变更** | CHG-20260603-001 |
| **模块名称** | gateway-core |
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
| `depends:` | 依赖标注（缩进换行）。模块内：`T010`；跨模块：`T020@control-plane`。无依赖则不写此行 |

---

## 阶段 1：环境与基础

**目标**：Context 注入 Key 常量、UsageEvent 结构体、Metrics 指标、DB 迁移就绪

- [x] T001 [P] P0 Context Key 常量定义，新建 `internal/gateway/middleware/context_keys.go`（定义 KeyIDKey / UserIDKey / DeptIDKey / TTFTKey / DurationKey / ProviderDurationKey / PromptTokensKey / CompletionTokensKey / ReasoningTokensKey / CachedTokensKey / ProviderTypeKey / ProviderModelIDKey / IsStreamKey / SessionIDKey 等）
- [x] T002 [P] P0 UsageEvent 结构体定义，新建 `internal/gateway/infra/usage_event.go`（request_id / key_id / user_id / dept_id / model / provider_type / ttft_ms / duration_ms / 分桶 token / 锁价成本 / messages / response_text 等字段，对齐 design.delta.md §UsageEvent 结构体）
- [x] T003 [P] P0 Metrics 指标注册，新建 `internal/gateway/infra/usage_metrics.go`（usage_events_emitted / dropped / written / batch_write_errors / batch_write_duration / queue_depth / price_missing / usage_missing / identity_missing / pricing_cache_refresh_errors）
  depends: T001

**✓ 检查点**：Context Key 常量可用，UsageEvent 结构体编译通过，Metrics 可注册

---

## 阶段 2：数据层

**目标**：call_logs 新增 16 列 + call_payloads 建表迁移脚本就绪

- [x] T004 [P] P0 call_logs 新增 16 列迁移脚本 `migrations/032_add_usage_fields.sql`（reasoning_tokens / cached_tokens / ttft_ms / duration_ms / provider_duration_ms / provider_type / provider_model_id / is_stream / session_id / prompt_est_cost / completion_est_cost / reasoning_est_cost / cached_est_cost / total_est_cost / usage_missing / pricing_missing，全部 ADD COLUMN IF NOT EXISTS + DEFAULT）
- [x] T005 [P] P0 call_payloads 建表迁移脚本 `migrations/032_add_usage_fields.sql`（request_id PK / messages JSONB / response_text TEXT / system_fingerprint / tools / byte_size / truncated / storage_backend / created_at，同 migration 文件追加）

**✓ 检查点**：迁移脚本可执行，call_logs 列和 call_payloads 表就绪

---

## 阶段 3：功能实现

**目标**：按用户故事实现采集管道——BatchWriter 基础设施 → 身份解析 → 埋点采集 → 流式回填 → 内容采集

### US-NEW-05 — Batch Writer 批量写入 (P0)

**目标**：从 channel 批量消费 UsageEvent，离线计算多桶成本后幂等写入 call_logs / call_payloads
**独立验证**：向 channel 投递 100 条模拟事件，验证 call_logs 写入行数 = 100，成本分桶正确
**对应验收标准**：AC-NEW-01, AC-NEW-04, AC-NEW-05

- [x] T010 P0 BatchWriter 实现 `internal/gateway/infra/batch_writer.go`（从 chan UsageEvent 批量消费，200 条 / 1s 触发写入；批量 INSERT ON CONFLICT(request_id) DO NOTHING；写入失败记录指标丢弃当批不重试）
  depends: T002, T003
- [x] T011 P0 MemoryPricingCache 实现 `internal/gateway/infra/pricing_cache.go`（每 5min 从 control-plane `GET /internal/v1/pricing` 拉取全量多桶价表，本地 sync.Map 缓存；启动时立即拉取；拉取失败保留上一份缓存记录指标）
  depends: T002, T003, T031@control-plane
- [x] T012 P0 CostEstimator 多桶成本估算 `internal/gateway/infra/cost_estimator.go`（prompt_cost = prompt_tokens × price / 1M；completion_cost = completion_tokens × price / 1M；reasoning_cost：有独立价用 reasoning_price，否则降级 completion_price 标记 fallback；cached_cost：有独立价计算，否则 0；四桶 cost 汇总 total_cost）
  depends: T002

**✓ 检查点**：BatchWriter 可批量消费 channel 事件并幂等写入 call_logs，成本估算四桶输出正确

---

### US-NEW-03 — 身份解析补全 (P0)

**目标**：Key 验证后从 AuthMiddleware 提取完整身份链（key_id → user_id → dept_id），注入 Gin Context
**独立验证**：发起一次携带有效 Key 的请求，检查 Context 中 key_id / user_id / dept_id 均已正确注入
**对应验收标准**：AC-NEW-06

- [x] T020 P0 AuthMiddleware 身份链注入 `internal/gateway/middleware/auth.go`（在 Key 验证通过后，从 AuthClient.VerifyKey 返回结果中提取 key_id / user_id / dept_id，通过 cors 新增的 Context Key 常量写入 Gin Context；Key 有效但身份缺失时放行请求 dept_id 记空 + identity_missing 指标 +1）
  depends: T001

**✓ 检查点**：请求携带有效 Key 后 Context 中可读取完整身份链，缺失身份时请求仍然放行

---

### US-NEW-01 — 调用埋点采集 (P0)

**目标**：每次 LLM 请求完成后异步构造 UsageEvent 并投递 collector channel，采集 reasoning/cached 分桶、TTFT/延迟、provider 路由、stream/session 标记
**独立验证**：进行一次非流式 LLM 调用后，验证 call_logs 中出现正确 request_id，所有 11 维字段非空
**对应验收标准**：AC-NEW-01, AC-NEW-09, AC-NEW-11, AC-NEW-12, AC-NEW-13, AC-NEW-14

- [x] T030 P0 UsageCollector 中间件 `internal/gateway/middleware/usage_collector.go`（c.Next() 之后从 Context 提取 11 维字段构造 UsageEvent；select default 非阻塞 send 到 channel；队列满时丢弃 + usage_events_dropped 指标 +1）
  depends: T001, T002, T003, T010, T020
- [x] T031 P0 路由注册 `internal/gateway/server/router.go`（将 UsageCollector 中间件注册到认证路由组，置于 AuthMiddleware 之后、路由 handler 之前）
  depends: T030
- [x] T032 P0 Orchestrator Context 注入 `internal/gateway/orchestrator/`（在转发上下游过程中，将 model / provider_type / provider_model_id / is_stream / ttft_ms / duration_ms / provider_duration_ms 写入 Gin Context；从响应 Header 提取 x-request-duration 作为 provider_duration_ms）
  depends: T001

**✓ 检查点**：一次非流式调用完成后 call_logs 中 token 分桶、TTFT/延迟、provider、stream 字段正确

---

### US-NEW-02 — 流式 usage 回填 (P0)

**目标**：对流式请求注入 include_usage 解析末尾 chunk usage，回填 reasoning_tokens / cached_tokens 等分桶字段
**独立验证**：进行一次含 reasoning 的流式调用，验证 call_logs 中 reasoning_tokens 非零
**对应验收标准**：AC-NEW-02, AC-NEW-03, AC-NEW-10

- [x] T040 P0 StreamUsageParser 实现 `internal/gateway/orchestrator/stream_usage.go`（对 OpenAI 兼容上游注入 stream_options.include_usage=true，解析最后 SSE chunk 的 usage.prompt_tokens / completion_tokens / prompt_tokens_details.cached_tokens / completion_tokens_details.reasoning_tokens；对 Anthropic 累计 message_start / message_delta 事件中的 input_tokens / output_tokens / cache_read_input_tokens / cache_creation_input_tokens / thinking_tokens；上游不支持 include_usage 时 token 记 0 + usage_missing=true 指标 +1）
  depends: T001, T003
- [x] T041 P0 Orchestrator 流式收尾集成（在流式转发收尾点调用 StreamUsageParser，将解析结果回填 Gin Context 的 token 字段）
  depends: T040, T032

**✓ 检查点**：流式调用后 call_logs 中 reasoning_tokens / cached_tokens 与非流式同口径

---

### US-NEW-04 — 调用内容采集 (P1)

**目标**：将每次请求的 messages 和响应正文异步写入 call_payloads 表
**独立验证**：进行一次调用后，通过 request_id 查询 call_payloads 可获取完整请求 messages 和响应正文
**对应验收标准**：AC-NEW-07, AC-NEW-08

- [ ] T050 P1 调用内容采集逻辑 `internal/gateway/middleware/usage_collector.go`（在 UsageCollector 中扩展：从已解析的请求体中提取 messages/params；非流式取最终响应 choices[].message；流式在收尾点重组各 delta.content 为完整 response_text；走独立 writer 批量写入 call_payloads ON CONFLICT(request_id) DO NOTHING；body > 256KB 截断 + truncated=true）
  depends: T030, T041

**✓ 检查点**：call_payloads 中可检索完整请求 messages 和响应正文，超大 body 截断标记正确

---

## 阶段 4：集成与收尾

**目标**：测试、性能验证、宪章合规检查

- [ ] T060 P0 单元测试 `tests/gateway/middleware/usage_collector_test.go`（Cover: UsageCollector 正常采集、channel 满丢弃、空 context 字段容忍）
  depends: T030
- [ ] T061 [P] P0 单元测试 `tests/gateway/infra/batch_writer_test.go`（Cover: 批量写入正常、ON CONFLICT 幂等、写入失败丢弃当批）
  depends: T010
- [x] T062 [P] P0 单元测试 `tests/gateway/orchestrator/stream_usage_test.go`（Cover: OpenAI usage 解析正确、Anthropic 累计正确、不支持 include_usage 时 usage_missing=true）
  depends: T040
- [x] T063 [P] P0 单元测试 `tests/gateway/infra/cost_estimator_test.go`（Cover: 四桶多桶计算正确、reasoning 降级逻辑、cached=null 计 0、缺价 pricing_missing）
  depends: T012
- [ ] T064 [P] P1 单元测试 `tests/gateway/infra/pricing_cache_test.go`（Cover: 成功拉取缓存、拉取失败保留旧缓存、空价表兜底）
  depends: T011
- [x] T065 P0 编译验证 `go build ./cmd/gateway` + `go test ./internal/gateway/... -count=1`
  depends: T031, T041, T050
- [ ] T066 [P] P1 AI Slop 清理（检查新增文件无 AI 代码异味，无 `as any`/`@ts-ignore` 等价模式）
  depends: T065
- [ ] T067 [P] P1 性能验证（P99 热路径延迟增量 < 100μs；验证 select default 非阻塞投递；验证 BatchWriter 不阻塞主 goroutine）
  depends: T065

**✓ 检查点**：所有测试通过，编译无警告，热路径延迟不增加

---

## 交付策略

### MVP 优先（仅 P0）
1. 完成阶段 1（T001-T003）+ 阶段 2（T004-T005）
2. 完成 US-NEW-05 BatchWriter（T010-T012）→ 批量写入管道就绪
3. 完成 US-NEW-03 身份解析（T020）→ 身份链完整
4. 完成 US-NEW-01 埋点采集（T030-T032）→ 请求收尾可采集明细
5. 完成 US-NEW-02 流式回填（T040-T041）→ 流式 Token 采集正确
6. MVP 可验证：call_logs 中可看到完整采集字段

### 场景间依赖
- US-NEW-05 (BatchWriter)：无场景间依赖（基础设施，最先完成）
- US-NEW-03 (身份解析)：无场景间依赖（独立于采集管道）
- US-NEW-01 (埋点采集)：依赖 US-NEW-05 BatchWriter + US-NEW-03 身份解析
- US-NEW-02 (流式回填)：依赖 US-NEW-01 采集管道
- US-NEW-04 (内容采集)：依赖 US-NEW-01 + US-NEW-02，P1 可延后

---

## 任务统计

| 指标 | 数值 |
| :--- | :--- |
| 总任务数 | 24 |
| P0（必须） | 18 |
| P1（重要） | 6 |
| P2（可选） | 0 |
| 可并行任务 | 10 |
| 预计工时 | 36h |
