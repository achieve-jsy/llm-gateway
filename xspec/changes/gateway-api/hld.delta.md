# gateway-api 概要设计变更 — CHG-20260603-001

> 基线：xspec/modules/gateway-api/hld.md (v1.3)

## 变更摘要

在现有 Logger 中间件和 Orchestrator 基础上新增埋点采集链路：c.Next() 完成后构造 UsageEvent（含 key/user/dept/studio 维度 + 缓存 token 分桶 + 流式 usage 回填），调用 EstimateCost 锁价算成本，通过进程内 channel 异步批量写入 call_logs。

## 架构变更

```mermaid
flowchart TD
    subgraph GW["gateway-api 进程（变更部分）"]
        MID["AuthMiddleware<br/>（变更：解析 key→user→dept→studio）"]
        ORCH["Orchestrator<br/>（变更：流式 usage 回填）"]
        COLL["Collector<br/>（新增）"]
        CH["channel 缓冲<br/>（新增）"]
        BW["batch writer<br/>（新增）"]
    end

    subgraph EXIST["现有（不变）"]
        ROUTER["Router / Handler"]
        PROV["Provider Adapters"]
    end

    MID -->|identity 注入| ROUTER
    ROUTER --> ORCH
    ORCH -->|c.Next() 后| COLL
    COLL -->|Emit| CH
    CH --> BW
    BW -->|批量 INSERT| DB["PostgreSQL call_logs"]
    PROV --> ORCH
```

## 新增组件

| 组件 | 位置 | 职责 |
| :--- | :--- | :--- |
| Collector | internal/gateway/observability/collector.go | 在 Logger 中间件 c.Next() 后构造 UsageEvent、调用 EstimateCost 算成本、投递 channel |
| batch writer | internal/gateway/observability/writer.go | goroutine 消费 channel，按 N=200 或 T=1s 批量 GORM INSERT call_logs，ON CONFLICT DO NOTHING 幂等 |
| UsageEvent | internal/gateway/observability/event.go | 数据契约：request_id / ts / key_id / user_id / dept_id / studio_id / model / tokens / status / duration / est_cost |

## 变更组件

| 组件 | 变更 | 说明 |
| :--- | :--- | :--- |
| AuthMiddleware | 修改 | 从 Key 缓存或 control-plane 回源解析 key_id/user_id/dept_id/studio_id，写入 model.UserIdentity 供 Logger 读取 |
| Orchestrator | 修改 | 流式调用：注入 stream_options.include_usage=true，末尾 chunk 解析 usage 回填 gin.Context；Anthropic 专有字段归一化 |
| Logger 中间件 | 修改 | c.Next() 后调用 Collector.Emit()，补齐 cached/cache_write/reasoning 字段 |

## 关键决策

| 决策 | 选择 | 理由 |
| :--- | :--- | :--- |
| 异步写入方式 | 进程内 channel + 批量写（L1） | 热路径非阻塞，万级/天在途丢失可接受，不引入 Redis Stream |
| 流式 usage 采集 | stream_options.include_usage=true | 使上游在末尾 chunk 返回 usage，与非流式同口径 |
| 身份解析 | 复用 KeyCache + 回源 control-plane | 已有 infra/key_cache.go，不新增外部依赖 |
| 成本计算时机 | 采集时锁价算好 | 明细行直接存 est_cost，聚合永不需重算历史 |

## 技术选型变更

无。沿用 Go + Gin + GORM + Redis 技术栈，不引入新依赖。