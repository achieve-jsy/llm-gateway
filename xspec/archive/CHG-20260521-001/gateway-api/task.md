# gateway-api 后端任务清单

## 文档信息

| 属性 | 内容 |
| :--- | :--- |
| **所属变更** | CHG-20260521-001 |
| **模块名称** | gateway-api |
| **模块类型** | web-backend (added) |
| **创建时间** | 2026-05-21 |

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

**目标**：项目结构、依赖、路由、中间件、公共连接就绪

- [x] T001 [P] P0 项目骨架初始化：go.mod、cmd/gateway/main.go 入口、internal/ 目录结构
- [x] T002 [P] P0 配置加载模块：internal/gateway/config/config.go（环境变量读取，定义 ServerConfig、RedisConfig、OTLPConfig、ProviderConfig 结构体）
- [x] T003 P0 Gin 路由注册与中间件挂载：internal/gateway/server/router.go（注册 /v1/chat/completions、/v1/models、/health、/ready）
  depends: T001
- [x] T004 P0 中间件-请求ID注入：internal/gateway/middleware/middleware.go（X-Request-ID header 或生成 UUID）
  depends: T003
- [x] T005 P0 中间件-API Key 校验：internal/gateway/middleware/middleware.go（从 Authorization Bearer 校验）
  depends: T003, T007
- [x] T006 P0 中间件-结构化日志：internal/gateway/middleware/middleware.go（slog JSON 格式，注入 request_id）
  depends: T004
- [x] T007 [P] P0 Redis 连接封装：internal/gateway/infra/redis.go（go-redis/v9 连接池，健康探针）
  depends: T002
- [x] T008 [P] P0 OTLP exporter 骨架：internal/gateway/infra/trace.go（OpenTelemetry SDK 初始化）
  depends: T002
- [x] T009 P0 中间件-统一错误处理：internal/gateway/middleware/middleware.go（panic 恢复 + 统一 JSON 错误格式）
  depends: T003

**✓ 检查点**：`go build ./cmd/gateway` 通过；服务启动后 GET /health 返回 200；中间件链路日志可见

---

## 阶段 2：数据层

**目标**：运行时数据结构、Provider 接口、Registry、Redis 消费封装就绪

- [x] T010 [P] P0 请求/响应数据结构定义：internal/gateway/model/chat.go（ChatCompletionRequest、ChatCompletionResponse、ChatCompletionChunk、StreamChunk、Message、Choice）
- [x] T011 [P] P0 统一错误结构定义：internal/gateway/model/error.go（UnifiedError 含 Type/Code/Message/HTTPStatus、三类错误常量）
- [x] T012 [P] P0 运行时上下文结构：internal/gateway/model/context.go（RequestContext 含 RequestID/APIKey/Model/Provider/StartTime）
- [x] T013 [P] P0 Provider 元数据结构：internal/gateway/model/provider.go（ProviderEntry 含 Name/Endpoint/Priority/Enabled/Models）
- [x] T014 P0 Provider Interface 定义：internal/gateway/provider/interface.go（Provider 接口：Chat/ChatStream/ListModels/HealthCheck）
  depends: T010, T011
- [x] T015 P0 Provider Registry 实现：internal/gateway/provider/registry.go（注册/查询/按模型匹配、优先级排序、可用性过滤）
  depends: T014, T013
- [x] T016 P0 Redis 缓存消费封装：internal/gateway/infra/cache.go（模型列表缓存 Get/Set TTL=60s、Key集合校验、模型可用性标记读写 TTL=300s）
  depends: T007

**✓ 检查点**：所有结构体和接口编译通过；Registry 单元测试覆盖注册/查询/过滤逻辑

---

## 阶段 3：功能实现

**目标**：按业务场景实现核心逻辑和 API，每个场景独立可测

### US-01 — 普通调用兼容 (P0)

**目标**：POST /v1/chat/completions 非流式请求返回完整 OpenAI 兼容响应
**独立验证**：curl POST /v1/chat/completions（stream=false）→ 返回 200 + 完整 JSON
**对应验收标准**：AC-01

- [x] T020 P0 Chat Handler 实现：internal/gateway/handler/chat.go（参数绑定、校验、调用 Orchestrator、返回 JSON）
  depends: T003, T010, T012
- [x] T021 P0 Orchestrator 编排逻辑：internal/gateway/orchestrator/chat.go（模型路由、Provider 选择、调用执行、超时控制）
  depends: T015, T016, T014
- [x] T022 P0 百炼适配器实现：internal/gateway/provider/bailian/adapter.go（实现 Provider 接口的 Chat 方法，HTTP 调用 + 响应解析）
  depends: T014, T010
- [x] T023 P0 结果归一层-普通响应：internal/gateway/normalizer/response.go（将厂商响应转换为 OpenAI 标准格式，补全 usage/id/created 字段）
  depends: T010

**✓ 检查点**：普通调用端到端通过，AC-01 验收

---

### US-02 — 流式调用兼容 (P0)

**目标**：POST /v1/chat/completions（stream=true）返回 SSE 流式响应
**独立验证**：curl --no-buffer POST（stream=true）→ 逐块接收 data: {...} + data: [DONE]
**对应验收标准**：AC-02, AC-06

- [x] T030 P0 流式 channel 管道：internal/gateway/orchestrator/stream.go（Go channel 流水线，接收 Provider 流式输出，转发到 Handler）
  depends: T021, T014
- [x] T031 P0 SSE flush 实现：internal/gateway/handler/chat_stream.go（Gin c.Stream + Flusher，逐块写入 data: JSON\n\n 格式）
  depends: T020, T030
- [x] T032 P0 百炼适配器流式方法：internal/gateway/provider/bailian/adapter.go（实现 ChatStream，SSE 读取 → channel 输出）
  depends: T022, T030
- [x] T033 P0 结果归一层-流式 chunk：internal/gateway/normalizer/stream.go（将厂商 chunk 转换为 OpenAI delta 格式）
  depends: T023, T030
- [x] T034 P0 流式中断处理：internal/gateway/handler/chat_stream.go 追加（客户端断开检测 context.Done、channel 清理、资源释放）
  depends: T031

**✓ 检查点**：流式调用端到端通过，AC-02 + AC-06 验收

---

### US-03 — 模型选择与可用性 (P0)

**目标**：GET /v1/models 返回当前可用模型列表；不可用模型请求返回 404/503
**独立验证**：GET /v1/models → 200 + OpenAI 兼容列表；请求不可用模型 → 503
**对应验收标准**：AC-03, AC-04

- [x] T040 P0 Models Handler 实现：internal/gateway/handler/models.go（查询 Redis 缓存 → 回源 Registry → 返回 OpenAI 格式列表）
  depends: T003, T016, T015
- [x] T041 P0 模型可用性校验逻辑：internal/gateway/orchestrator/chat.go 追加（请求前检查模型可用性标记，不可用返回 UnifiedError）
  depends: T021, T016, T011

**✓ 检查点**：模型列表接口正常，不可用模型正确拒绝，AC-03 + AC-04 验收

---

### US-04 — 错误语义映射 (P0)

**目标**：全链路错误按三类分治（400 输入错误 / 404+503 不可用 / 502+504 临时失败）
**独立验证**：模拟各类错误 → 返回正确 HTTP 状态码 + 统一错误 JSON
**对应验收标准**：AC-05

- [x] T050 P0 错误映射规则引擎：internal/gateway/errormap/mapper.go（定义三类错误判定规则，Provider 原始错误 → UnifiedError 转换）
  depends: T011
- [x] T051 P0 百炼错误转换：internal/gateway/provider/bailian/error.go（百炼 HTTP 状态码/错误码 → 三类映射）
  depends: T050, T022
- [x] T052 P0 Orchestrator 错误集成：internal/gateway/orchestrator/chat.go 追加（捕获 Provider 错误，经 mapper 转换后返回）
  depends: T050, T021

**✓ 检查点**：输入参数错误返回 400，模型不可用返回 503，上游超时返回 504，AC-05 验收

---

### US-05 — 多厂商适配扩展 (P1)

**目标**：扩展 DeepSeek、Bedrock、voocel/litellm 适配器，Provider Registry 统一管理
**独立验证**：切换 provider 配置后调用同一接口返回正确结果

- [x] T060 [P] P1 DeepSeek 适配器：internal/gateway/provider/deepseek/adapter.go（实现 Provider 全部方法）
  depends: T014, T050
- [x] T061 [P] P1 Bedrock 适配器：internal/gateway/provider/bedrock/adapter.go（骨架实现，方法存在返回未实现错误）
  depends: T014, T050
- [x] T062 [P] P1 voocel/litellm 兜底适配器：internal/gateway/provider/litellm/adapter.go（通用 OpenAI 兼容转发）
  depends: T014, T050
- [x] T063 P1 各厂商错误转换补齐：internal/gateway/provider/deepseek/error.go、internal/gateway/provider/bedrock/error.go、internal/gateway/provider/litellm/error.go
  depends: T060, T061, T062, T050

**✓ 检查点**：多厂商切换验证通过，Registry 热切换无异常

---

### US-06 — 配置变更监听 (P1)

**目标**：监听 control-plane 通过 Redis pub/sub 发布的变更通知，热刷新 Registry 与缓存
**独立验证**：Redis 发布变更消息 → Registry 自动刷新 → 后续请求使用新配置

- [x] T070 P1 Redis pub/sub 订阅：internal/gateway/infra/subscriber.go（订阅 config:change channel，解析变更事件）
  depends: T007
- [x] T071 P1 Registry 热刷新：internal/gateway/provider/registry.go 追加（接收变更事件 → 重新加载 Provider 配置 → 原子替换）
  depends: T070, T015

**✓ 检查点**：pub/sub 消息触发配置热更新，无需重启服务

---

## 阶段 4：集成与收尾

**目标**：集成测试、安全加固、性能验证，交付就绪

- [x] T080 P0 健康检查完善：internal/gateway/handler/health.go（/health 返回服务状态；/ready 检查 Redis 连通性 + Provider 可达性）
  depends: T003, T007, T015
- [x] T081 P1 集成测试-核心链路：tests/integration/chat_test.go（普通调用、流式调用、模型列表端到端，mock Provider）
  depends: T020, T031, T040
- [x] T082 P1 集成测试-错误场景：tests/integration/error_test.go（三类错误覆盖、流式中断、Key 无效）
  depends: T052, T034, T005
- [x] T083 [P] P1 安全加固：internal/gateway/middleware/middleware.go 追加（请求体大小限制、Header 安全校验）
  depends: T005
- [x] T084 [P] P1 OTLP trace 埋点完善：internal/gateway/orchestrator/chat.go 追加（span 注入模型名/provider/耗时/状态码）
  depends: T008, T021
- [x] T085 [P] P2 性能验证：tests/benchmark/bench_test.go（并发 100 请求，P99 < 200ms 排除上游耗时）
  depends: T081
- [x] T086 P1 联调验证：对接 control-plane 真实接口验证模型列表、Key 校验、配置变更
  depends: T081, T071
  depends: T010@control-plane

**✓ 检查点**：全部接口测试通过，trace 可观测，满足交付质量标准

---

## 交付策略

### MVP 优先（仅 P0 场景）
1. 完成阶段 1 + 阶段 2（基础设施与数据层）
2. 完成 US-01（普通调用）→ 独立验证 → 接口可调用
3. 完成 US-02（流式调用）→ 独立验证 → SSE 正常
4. 完成 US-03（模型选择）→ 独立验证 → 列表+可用性
5. 完成 US-04（错误映射）→ 独立验证 → 三类分治
6. 阶段 4 健康检查 → MVP 交付

### 场景间依赖
- US-01: 无场景间依赖（可独立交付）
- US-02: 依赖 US-01 的 Handler 和 Orchestrator 基础
- US-03: 依赖阶段 2 的 Registry 和缓存封装
- US-04: 依赖 US-01 的 Orchestrator（错误集成点）
- US-05: 依赖 US-04 的错误映射规则（扩展厂商需实现错误转换）
- US-06: 依赖阶段 2 的 Registry + 跨模块 control-plane 接口就绪

### 跨模块依赖
- control-plane 完成渠道管理 + 模型接口后，本模块才能做联调验证（T086）
- control-plane 完成 pub/sub 发布逻辑后，本模块 US-06 可联调（T071）

---

## 任务统计

| 指标 | 数值 |
| :--- | :--- |
| 总任务数 | 38 |
| P0（必须） | 27 |
| P1（重要） | 10 |
| P2（可选） | 1 |
| 可并行任务 | 12 |
| 预计工时 | 52h |
