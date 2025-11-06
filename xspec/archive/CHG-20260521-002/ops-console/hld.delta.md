# ops-console 概要设计变更 — CHG-20260521-002

> 基线：xspec/modules/ops-console/hld.md (v1.0)

## 变更摘要

渠道列表页操作列新增"同步模型"按钮，点击后弹出同步结果弹窗，支持过滤+勾选+批量注册。

## 架构变更

无架构变更。在现有渠道管理页基础上新增组件。

## 技术选型变更

无变更。使用现有 Vue 3 + Element Plus 栈。

## 新增组件

| 组件 | 位置 | 职责 |
| :--- | :--- | :--- |
| SyncModelsDialog.vue | components/admin/ | 同步结果弹窗：展示列表、过滤、勾选、确认注册 |

## 核心交互流程

```mermaid
sequenceDiagram
    participant Admin as 管理员
    participant Page as ProviderListPage
    participant Dialog as SyncModelsDialog
    participant API as control-plane

    Admin->>Page: 点击"同步模型"按钮
    Page->>API: POST /providers/:id/sync-models
    API-->>Page: SyncResult
    Page->>Dialog: 打开弹窗(传入 result)
    Admin->>Dialog: 输入关键词过滤
    Dialog->>Dialog: 实时过滤列表
    Admin->>Dialog: 一键全选 / 手动勾选
    Admin->>Dialog: 点击"确认注册"
    Dialog->>API: POST /providers/:id/sync-models/confirm
    API-->>Dialog: 注册成功
    Dialog->>Page: 关闭弹窗，刷新模型列表
```
