# control-plane 概要设计变更 — CHG-20260521-002

> 基线：xspec/modules/control-plane/hld.md (v1.0)

## 变更摘要

新增模型同步接口 POST /api/v1/providers/:id/sync-models，复用现有 Handler → Service → Repository 分层结构。

## 架构变更

无架构变更。新增一个接口端点，复用现有中间件链和认证体系。

## 技术选型变更

无变更。使用现有 Go + Gin + GORM 栈。

## 新增组件

| 组件 | 位置 | 职责 |
| :--- | :--- | :--- |
| SyncHandler | handler/sync_handler.go | 处理同步请求，调用 SyncService |
| SyncService | service/sync_service.go | 编排同步逻辑：调厂商 API → 对比本地 → 分类 → 批量写入/禁用 |

## 核心流程

```mermaid
sequenceDiagram
    participant Admin as 管理员
    participant API as SyncHandler
    participant Svc as SyncService
    participant Upstream as 厂商 GET /models
    participant DB as ModelRepo

    Admin->>API: POST /providers/:id/sync-models
    API->>Svc: SyncModels(providerID)
    Svc->>Upstream: GET {endpoint}/models (带凭证)
    Upstream-->>Svc: 模型列表
    Svc->>DB: 查询本地已注册模型(provider_id)
    Svc->>Svc: 对比分类(新增/已存在/已下架)
    Svc-->>API: SyncResult{added[], existing[], removed[]}
    API-->>Admin: 返回同步结果

    Admin->>API: POST /providers/:id/sync-models/confirm
    Note over API: body: {model_names: [...]}
    API->>Svc: ConfirmSync(providerID, modelNames)
    Svc->>DB: 批量 Create models (enabled=false)
    Svc->>DB: 批量 Update removed models (enabled=false)
    Svc-->>API: 注册结果
    API-->>Admin: 成功注册 X 个
```
