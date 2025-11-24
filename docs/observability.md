# 可观测性

## 三层架构

| 层级 | 技术 | 用途 |
|------|------|------|
| 日志 | Go `log/slog` | 结构化请求日志、上游调用日志 |
| 链路追踪 | OpenTelemetry | 分布式追踪、span 传播 |
| 埋点采集 | 自研 Collector | Token 用量、成本计算、调用明细 |

## 日志

### 日志来源

Gateway 使用 Go 标准库 `log/slog` 进行结构化日志记录：

| 来源 | 级别 | 说明 |
|------|------|------|
| Logger 中间件 | INFO | 请求完成摘要（method/path/status/duration/model/identity） |
| LLMClient Hooks | DEBUG/INFO/WARN | 上游调用开始/完成/错误 |
| RequestBodyLogger | DEBUG | 请求体（仅 DEBUG 级别） |

### 日志字段

```json
{
  "time": "2024-01-01T12:00:00Z",
  "level": "INFO",
  "msg": "request",
  "request_id": "uuid",
  "method": "POST",
  "path": "/v1/chat/completions",
  "status": 200,
  "duration_ms": 1234,
  "model": "gpt-4",
  "user_id": "user-uuid",
  "key_id": "key-uuid",
  "provider": "openai",
  "upstream_status": 200,
  "prompt_tokens": 100,
  "completion_tokens": 50,
  "total_tokens": 150,
  "stream": true,
  "failover_used": false
}
```

### 日志级别配置

通过环境变量或代码设置（当前无独立配置项，默认 INFO）。

## 链路追踪（OpenTelemetry）

### 启用方式

```yaml
# config/dev.yaml
otlp:
  enabled: true
  endpoint: "localhost:4317"
```

或环境变量：

```bash
OTLP_ENABLED=true
OTLP_ENDPOINT=localhost:4317
```

### Span 结构

```
Tracing Middleware (入口 span, SpanKindServer)
    ├── http.method, http.path, request_id, status_code, model
    │
    └── ChatOrchestrator (非流式)
    │       └── upstream.chat (SpanKindClient)
    │               ├── llm.model, llm.messages_count
    │               ├── llm.prompt_tokens, llm.completion_tokens, llm.total_tokens
    │               └── llm.provider, llm.upstream_model, llm.upstream_status_code
    │
    └── StreamOrchestrator (流式)
            └── upstream.chat_stream (SpanKindClient)
                    ├── llm.model, llm.messages_count
                    ├── stream.chunk_count
                    └── llm.prompt_tokens, llm.completion_tokens, llm.total_tokens
```

### 基础设施

项目提供 Docker Compose 配置启动 OTel Collector + Jaeger：

```bash
# 启动可观测性组件
docker compose --profile observability up -d

# Jaeger UI
open http://localhost:16686
```

### OTel Collector 配置

`deploy/otel-collector-config.yaml`：

```yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
      http:
        endpoint: 0.0.0.0:4318

processors:
  batch:

exporters:
  jaeger:
    endpoint: jaeger:14250
    tls:
      insecure: true
  logging:
    loglevel: debug

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [jaeger, logging]
```

## 埋点采集

### 数据流

```
请求 → TraceCapture 中间件 → UsageCollector 中间件
                                      │
                                      ▼
                              Collector (channel)
                                      │
                                      ▼
                              BatchWriter (批量写入)
                                      │
                    ┌─────────────────┼─────────────────┐
                    ▼                                   ▼
              call_logs 表                       call_traces 表
                    │
                    ▼
           RollupCron (每小时)
                    │
                    ▼
            usage_daily 表
```

### 数据表

| 表 | 写入方式 | 用途 |
|----|----------|------|
| `call_logs` | 实时批量（200条/批 或 1秒） | 调用明细（身份/路由/Token/耗时/成本） |
| `call_traces` | 实时批量 | 请求/响应体（JSONB）+ 各阶段耗时 |
| `usage_daily` | 每小时聚合 | 按天统计（兜底跨 90 天查询） |

### 采集字段

`call_logs` 核心字段：

| 分类 | 字段 |
|------|------|
| 身份 | key_id, user_id, dept_id, studio_id, key_name, user_name, dept_name |
| 路由 | model, routed_model, routed_via_group, group_name, provider |
| Token | prompt_tokens, completion_tokens, total_tokens, cached_tokens |
| 耗时 | duration_ms, auth_ms, route_ms, upstream_ms, ttfb_ms |
| 成本 | input_cost, output_cost, total_cost, price_snapshot |
| 状态 | status_code, upstream_status_code, failover_used |

### 日志开关

API Key 级别 `log_enabled` 字段控制是否写入 `call_traces`（请求/响应体）。Token 用量和成本始终写入 `call_logs`。

### 系统探针过滤

`__system_` 前缀的 Key 完全跳过埋点和 trace（用于健康检查等内部调用）。

## 成本计算

### 厂商适配

`cost.go` 的 `ExtractCostInput` 归一化三种厂商的缓存 token 约定：

| 厂商 | 字段 | 映射 |
|------|------|------|
| OpenAI | `prompt_tokens_details.cached_tokens` | `cached_tokens` |
| DeepSeek | `prompt_cache_hit_tokens` | `cached_tokens` |
| Anthropic | `usage.cache_read_input_tokens` | `cached_tokens` |

### 定价模型

- 价格单位：**USD per 1,000,000 tokens**
- 支持阶梯定价、缓存折扣
- `price_mode`：`catalog`（读 model_catalog）或 `manual`（使用 model 表价格）

## API 查询端点

| 端点 | 说明 |
|------|------|
| `GET /api/v1/usage` | 聚合查询（按日期/模型/Key/部门） |
| `GET /api/v1/usage/logs` | 明细查询 |
| `GET /api/v1/usage/logs/:request_id/trace` | 单条 Trace |
| `GET /api/v1/usage/options` | 过滤枚举（模型/部门/Key 列表） |
| `GET /api/v1/my/usage` | 个人统计 |
| `GET /api/v1/my/usage/logs` | 个人明细 |

## 本地开发

```bash
# 启动完整可观测性栈
make dev-infra-full

# 包含：
# - PostgreSQL + Redis
# - OTel Collector (4317/4318)
# - Jaeger (16686)
# - Langfuse (可选)
```

启用追踪后访问 http://localhost:16686 查看 Jaeger UI。
