# gateway-core 概要设计变更 — CHG-20260603-001

> 基线：xspec/modules/gateway-core/spec.md (v1.0) — 无 hld.md 基线，首次增量设计

## 变更摘要

在请求热路径末端新增异步用量采集管道（Collector → channel → BatchWriter），采集 reasoning_tokens / cached_tokens / TTFT / provider 路由 / is_stream / session_id 等 11 维字段，通过非阻塞 channel 异步批量写入 PostgreSQL；EstimateCost 离线化至 BatchWriter 后台 goroutine。

## 架构变更

```mermaid
flowchart TD
    subgraph "gateway-core 变更增量"
        CTX["Gin Context\n(Orchestrator 注入 TTFT/延迟/provider/stream/session)"]
        COL["UsageCollector\n(中间件 c.Next 之后)"]
        CH["chan UsageEvent\n(容量 10000)"]
        BW["BatchWriter\n(200条/1s 触发)"]
        SW["StreamUsageParser\n(解析末尾 chunk usage)"]
    end
    
    subgraph "已有组件（不变）"
        AUTH["AuthMiddleware"]
        ORCH["Orchestrator"]
        PROV["Provider Router"]
    end
    
    AUTH --> ORCH
    ORCH --> PROV
    ORCH -.->|写入 context: ttft_ms / duration_ms<br>/ provider_type / is_stream / session_id| CTX
    CTX -->|c.Next() 之后| COL
    COL -->|非阻塞 send| CH
    CH --> BW
    SW -->|流式收尾回填 reasoning_tokens| COL
    BW -->|批量 INSERT ON CONFLICT| PG_CL[("PostgreSQL (call_logs)")]
    BW -->|独立 writer| PG_PL[("PostgreSQL (call_payloads)")]
```

### 主线操作（纳秒/微秒级）

```mermaid
sequenceDiagram
    participant Client
    participant Main as 主线 Goroutine
    participant Orch as Orchestrator
    participant Ch as channel(10000)
    participant BG as BatchWriter Goroutine
    participant PC as 内存价表
    participant PG as PostgreSQL
    
    Client->>Main: 请求
    Main->>Orch: 转发
    Note over Orch: 记录 startTime/TTFT/providerDuration<br>(3 次纳秒级 time.Since)
    
    Orch-->>Main: 响应正文
    Main-->>Client: 响应返回（主线解放）
    
    Note over Main: c.Next() 返回
    Main->>Main: 从 context 提取 11 字段 + 构造 struct (&lt; 5μs)
    Main->>Ch: select send (非阻塞, &lt; 1μs)
    
    Note over BG: 独立 goroutine（离线）
    BG->>Ch: 批量消费 (200条/1s)
    BG->>PC: EstimateCost (离线查 Map)
    PC-->>BG: 四桶单价
    BG->>BG: prompt_cost = tokens × price<br>reasoning_cost = tokens × reasoning_price
    BG->>PG: INSERT call_logs (ON CONFLICT DO NOTHING)
```

## 技术选型变更

| 领域 | 原选型 | 新选型 | 理由 |
| :--- | :--- | :--- | :--- |
| 异步投递 | 无 | buffered channel (10000) + select default | 宪章要求热路径无阻塞；队列满丢弃 |
| 批量写入 | 无 | goroutine + Ticker(1s) + Counter(200) | 平衡延迟与吞吐；独立于主线 |
| 幂等写入 | 无 | ON CONFLICT(request_id) DO NOTHING | 防 failover 重投重复 |
| 成本计算 | 无 | EstimateCost 在 BatchWriter 中离线执行 | 主线零计算，不阻塞响应返回 |
| 价表刷新 | 无 | time.Ticker(5min) 拉取 control-plane 价表 | 热路径本地查 Map，不远程调用 |

## 新增组件

| 组件 | 位置 | 职责 |
| :--- | :--- | :--- |
| `UsageCollector` | `internal/gateway/middleware/` | c.Next() 后从 context 提取 11 字段，构造 UsageEvent，非阻塞 send(channel) |
| `BatchWriter` | `internal/gateway/infra/` | 从 channel 批量消费，EstimateCost 离线计算四桶成本，INSERT ON CONFLICT 幂等写入 call_logs |
| `StreamUsageParser` | `internal/gateway/orchestrator/` | 流式收尾解析末尾 chunk 的 usage（含 reasoning_tokens/cached_tokens），回填 context |
| `MemoryPricingCache` | `internal/gateway/infra/` | 每 5min 从 control-plane `/internal/v1/pricing` 拉取全量多桶价表，本地 Map 缓存 |

## 宪章合规

| 原则 | 检查结果 |
| :--- | :--- |
| 不开倒车 | ✅ 新增组件，不修改已有中间件核心逻辑；batch writer 失败不影响主流程 |
| 服务稳定 | ✅ channel + batch 异步，不阻塞热路径；幂等写入安全 |
| 服务快速 | ✅ select default 非阻塞投递；TTFT 提取在已有 context 上增量计算（纳秒级） |
| 架构清晰 | ✅ gateway 不直连 PG 做配置查询（call_logs 写入是数据管道，非配置查询） |
