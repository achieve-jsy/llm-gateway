# 技术研究：Key 缓存主动失效 — Redis pub/sub 通道方案

> **用途**：单个研究课题的完整深度分析文件。
> 本文件由 `xf-research` subagent 写入，存放于 `xspec/changes/auth/research/002-key-cache-invalidation.md`。

---

## 1. 概览（必填）

**研究问题**：auth 模块如何通过 Redis pub/sub 在 Key 状态变更时 ≤1s 内主动失效本地缓存？

**决策截止**：auth 模块详细设计开始前

**决策者**：架构负责人 + auth/control-plane 模块开发负责人

**上游依赖**：

| 文档 | 说明 |
| :--- | :--- |
| HLD | auth 模块概要设计 §5.3 API Key 认证方案（状态感知延迟 ≤1s）、§6.2 缓存策略 |
| HLD delta | control-plane CHG-20260523-001 hld.delta.md（Key CRUD + Notifier 发布） |
| 代码 | control-plane `internal/controlplane/event/publisher.go`（已有 Publisher 实现） |
| 代码 | gateway-api `internal/gateway/infra/subscriber.go`（已有 Subscriber 实现） |

---

## 2. 问题背景与约束条件（必填）

**为什么需要做这个决策？**

auth 模块的 `KeyAuthService` 使用 Redis 缓存 Key 状态（TTL 60s），实现 API Key 鉴权链路的快速解析。HLD 要求 Key 状态变更（禁用/启用/删除）后鉴权中间件感知延迟 ≤1s。纯 TTL 过期策略的最坏延迟为 60s，远不满足 ≤1s 要求。需要一种主动失效机制：当 control-plane 中 Key 状态变更时，主动通知 auth 模块删除对应缓存条目。

control-plane 已有成熟的 `ChangeNotifier` + Redis pub/sub 机制（channel `config:change`），用于通知 gateway-api 配置变更。本课题需决定 auth 模块是否复用同一通道，还是使用独立通道。

**硬约束（不可违反）：**

- Key 状态变更感知延迟必须 ≤ 1s（HLD §8.1 质量目标明确要求）
- 不得引入额外的消息中间件（如 Kafka、RabbitMQ）——宪章约束技术栈为 Redis
- 必须复用 control-plane 已有的 ChangeNotifier/Publisher 模式——与 gateway-api 保持架构一致性

**软约束（有偏好）：**

- 优先选实现改动量最小的方案 → 对应 §5「实现复杂度」维度
- 优先选与现有代码模式最一致的方案 → 对应 §5「架构一致性」维度
- 优先选运维简单的方案（无需管理多个通道） → 对应 §5「运维成本」维度

**不在本次研究范围内：**

- Key 缓存 TTL 时长调整（60s 已在 HLD 中确定）
- control-plane Key CRUD 接口契约定义（属 R-02 研究范围）
- 缓存击穿防护（singleflight 已在 HLD 中确定）

---

## 3. 前提假设（必填）

- control-plane 的 Key CRUD 服务（本次变更新增）会通过已有 `Publisher` 发布 Key 状态变更事件
- Key 状态变更频率极低（每天 < 100 次），不会对 Redis pub/sub 通道造成拥塞
- auth 模块与 control-plane 共享同一 Redis 实例（项目部署架构假设）
- auth 模块启动时能正常订阅 Redis pub/sub 通道；若启动期间有消息丢失，TTL 60s 兜底

---

## 4. 候选方案（必填）

| 方案 | 简介 | 版本 / 来源 |
| :--- | :--- | :--- |
| **A：复用 `config:change` 通道** | auth 模块订阅 control-plane 已有的 `config:change` Redis pub/sub 通道，收到消息后按 `ResourceType` 字段过滤：仅处理 `resource_type == "key"` 的事件。收到 Key 事件后删除对应 Redis 缓存条目 `key:{hash}`。 | 项目现有代码：`event/publisher.go`、`infra/subscriber.go` |
| **B：独立 `key_status_changes` 通道** | control-plane 为 Key 状态变更新增独立的 Redis pub/sub 通道 `key:status:change`，auth 模块仅订阅此通道。Key 事件不在 `config:change` 通道发布。 | 新通道设计 |
| **C：内部 HTTP 回调** | control-plane 在 Key 状态变更后直接 HTTP 调用 auth 模块的内部缓存失效端点（如 `POST /internal/v1/cache/invalidate`），auth 收到后删除缓存。 | webhook 模式 |

**未纳入评估的方案：**

| 方案 | 排除原因 |
| :--- | :--- |
| Kafka 消息队列 | 违反硬约束：项目技术栈为 Redis，不引入 Kafka |
| PostgreSQL LISTEN/NOTIFY | 项目 Redis pub/sub 已成熟运行，无需引入另一种消息机制 |

---

## 5. 评估维度与权重（必填）

| 维度 | 权重 | 衡量方式 |
| :--- | :---: | :--- |
| 实现复杂度 | 30% | auth 端和 control-plane 端各需多少新增代码/配置变更 |
| 架构一致性 | 25% | 是否与 gateway-api 已有的订阅模式保持一致 |
| 运维成本 | 20% | Redis 通道管理复杂度；监控/告警覆盖难度 |
| 可靠性 | 15% | 消息丢失风险；断线恢复能力 |
| 性能 | 10% | 消息过滤开销；通道拥塞风险 |

---

## 6. 方案对比矩阵（必填）

| 维度（权重） | A：复用 config:change | B：独立 key:status:change | C：内部 HTTP 回调 |
| :--- | :---: | :---: | :---: |
| 实现复杂度（30%） | 5 | 3 | 2 |
| 架构一致性（25%） | 5 | 3 | 1 |
| 运维成本（20%） | 5 | 3 | 2 |
| 可靠性（15%） | 4 | 4 | 2 |
| 性能（10%） | 4 | 5 | 2 |
| **加权总分（满分 5.0）** | **4.70** | **3.40** | **1.90** |

**评分依据：**

- **实现复杂度**：A：auth 端仅需复制 gateway-api 的 Subscriber 模式（已有参考实现），加 ResourceType 过滤回调；control-plane 仅在 KeyService 中复用已有 `publishEvent()` 方法，零新增基础设施代码；B：需在 control-plane 新增独立 Publisher（不同通道），auth 新增独立 Subscriber，需维护两套 pub/sub 基础设施；C：需在 auth 新增 HTTP 端点、在 control-plane 新增 HTTP 调用逻辑、处理超时/重试/认证等复杂度
- **架构一致性**：A：与 gateway-api 完全一致——订阅同一个 `config:change` 通道，按 ResourceType 过滤。这是项目已验证的模式；B：引入独立通道破坏一致性——auth 和 gateway-api 使用不同的订阅模式；C：引入 HTTP 回调完全偏离现有 pub/sub 模式
- **运维成本**：A：单一通道监控，已有告警覆盖；B：需监控两个通道，多一套告警配置；C：需监控 HTTP 回调可用性，重试队列，运维复杂度最高
- **可靠性**：A/B 评分相同：Redis pub/sub 不持久化消息，订阅方断线期间消息丢失。但两者均有 TTL 60s 兜底；C：HTTP 回调需处理网络分区、超时、重试，可靠性最低
- **性能**：A：需要过滤非 Key 事件（极少量 CPU 开销，Key 事件频率极低）；B：纯 Key 事件通道，零过滤开销；C：HTTP 调用增加网络延迟和超时风险

---

## 7. 深度分析（适用时填）

### 关键差异：ChangeEvent 扩展 vs 独立通道

**control-plane 已有的 ChangeEvent 体系**：

项目已有 `ChangeEvent` 结构体（`internal/controlplane/event/change_event.go`），字段为 `Type / ResourceType / ResourceID / Timestamp`。当前已有事件类型覆盖 Provider 和 Model 的 CRUD 操作。

control-plane 的 CHG-20260523-001（Key CRUD 变更）已在 HLD delta 中明确 KeyService 使用 Notifier 发布事件。这意味着 Key 事件将自然通过 `Publisher.Publish()` 发布到 `config:change` 通道——只需在 `change_event.go` 中新增 Key 相关的 `ChangeEventType` 常量：

```go
KeyCreated  ChangeEventType = "key_created"
KeyDisabled ChangeEventType = "key_disabled"
KeyEnabled  ChangeEventType = "key_enabled"
KeyDeleted  ChangeEventType = "key_deleted"
```

**方案 A 的 auth 端实现要点**：

auth 模块复用 gateway-api 的 `Subscriber` 模式，但回调函数仅处理 `ResourceType == "key"` 的消息：

```go
// auth 模块的 Key 缓存失效订阅器
callback := func(ev ChangeEvent) {
    if ev.ResourceType != "key" {
        return // 过滤非 Key 事件
    }
    // 根据 ev.Type 执行缓存失效
    keyPattern := fmt.Sprintf("key:*") // 或精确匹配 key:{hash_of(ev.ResourceID)}
    rdb.Del(ctx, keyPattern)           // 删除对应缓存条目
}
subscriber := NewSubscriber(rdb, callback)
```

**方案 A 的缓存失效精确度**：

ChangeEvent 中 `ResourceID` 是 Key 的 UUID（数据库 id），而非 `sk-xxx` Key 明文。auth 模块的 Redis 缓存 Key 是 `key:{sha256(sk-xxx)}`——无法直接从 ResourceID（UUID）映射到 Redis Key（sha256 hash）。

两种精确失效策略：
1. **全量清空**：收到任何 Key 事件后 `DEL key:*`（删除所有 Key 缓存条目）。简单可靠，但会导致短暂的全量缓存穿透（极低频操作，可接受）
2. **精确失效**：在 Key 缓存中同时存储 `key_id → key_hash` 的反向映射（新增 Redis Key `key_id:{uuid} → sha256_hash`），收到事件后查询反向映射再精确删除。更精确但需额外维护

**推荐策略**：全量清空（方案 1）。理由：Key 状态变更频率极低（每天 < 100 次），全量清空后的缓存穿透量极小（被 singleflight 合并），且实现最简、无额外 Redis Key 维护成本。

---

## 8. 最终结论（必填）

**选择方案**：A — 复用 `config:change` 通道

**核心理由**：
- 实现改动量最小：auth 端复用已有 Subscriber 模式（+ ResourceType 过滤），control-plane 仅新增 4 个 ChangeEventType 常量，零新增基础设施代码
- 架构一致性最高：与 gateway-api 订阅同一个通道同一事件格式，运维/监控/告警完全复用
- Key 事件频率极低，通道拥塞和过滤开销均为零风险

**被否决方案的关键原因**：

| 方案 | 否决原因 |
| :--- | :--- |
| B：独立通道 | 需维护两套 pub/sub 基础设施，架构不一致，运维多一套监控。收益仅为"免去过滤"——但 Key 事件频率极低，过滤开销忽略不计 |
| C：HTTP 回调 | 需新增 HTTP 端点/调用/超时/重试/认证全套基础设施，复杂度远高于 pub/sub。可靠性也最低（网络分区/超时风险） |

---

## 9. 遗留风险与后续行动（必填）

**已知风险**：

| 飱险 | 可能性 | 影响 | 缓解措施 |
| :--- | :---: | :---: | :--- |
| Redis pub/sub 消息丢失（auth 订阅方断线期间） | 低 | 中 | TTL 60s 兜底：断线期间最坏延迟 60s；auth 启动时订阅成功后消息不丢失；生产环境 Redis 高可用部署 |
| 全量清空缓存后短暂穿透到 control-plane | 低 | 低 | singleflight 合并并发穿透请求；Key 事件频率极低，穿透量 < 5 QPS；control-plane P95 ≤ 50ms |
| auth 订阅与 gateway-api 订阅互相干扰 | 极低 | 低 | Redis pub/sub 模型：一个消息可被多个订阅方独立接收，无干扰风险 |

**后续行动**：

- [ ] 在 control-plane 的 `change_event.go` 中新增 Key 相关 ChangeEventType 常量（key_created / key_disabled / key_enabled / key_deleted）
- [ ] 在 control-plane KeyService 中复用已有 `publishEvent()` 方法发布 Key 状态变更事件
- [ ] 在 auth HLD §6.2 缓存策略中补充 pub/sub 主动失效机制说明（复用 config:change 通道 + ResourceType 过滤 + 全量清空策略）
- [ ] 在 auth 详细设计中实现 Key 缓存失效 Subscriber（参考 gateway-api Subscriber 模式）
- [ ] 在 auth HLD §8.2 日志中补充 Key 缓存失效事件日志（收到 pub/sub 消息时记录 key_id 和缓存清空动作）