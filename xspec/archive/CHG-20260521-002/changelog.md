# 变更记录 — CHG-20260521-002 一键同步模型列表

## 基本信息

| 属性 | 内容 |
| :--- | :--- |
| **变更编号** | CHG-20260521-002 |
| **变更名称** | 一键同步模型列表 |
| **归档时间** | 2026-05-23 |

## 变更概述

为渠道管理新增"同步模型"能力：管理员可一键从上游厂商拉取模型列表，系统自动对比本地去重并标记下架模型为禁用，管理员在弹窗中勾选需要的模型后批量注册。同步接口拆为两步（先拉取对比、再确认注册），空列表时不做任何禁用操作以防止网络故障导致误禁用。

## 涉及模块

| 模块 | 操作 | 说明 |
| :--- | :--- | :--- |
| control-plane | 更新 (v1.0 → v1.1) | 新增 POST sync-models + POST sync-models/confirm 两个端点，SyncHandler + SyncService |
| ops-console | 更新 (v1.0 → v1.1) | 渠道列表页新增同步按钮，同步结果弹窗含过滤、勾选、一键全选功能 |

## 关键决策

- **同步接口拆为两步**：先拉取对比 → 再确认注册。管理员需在中间环节勾选，不能全量盲导入。备选一步完成（全量导入后逐个禁用）体验差，未采纳。
- **下架检测防误禁用**：上游返回空列表时不执行禁用，仅提示。网络故障可能导致空返回，全量禁用影响严重。备选无论如何都执行未采纳。
- **不支持同步的渠道类型**：Anthropic/Bedrock adapter 无 /models 接口，返回 400 sync_not_supported 提示。

## 制品清单

- [x] change.yaml — 变更元数据 + 功能范围边界
- [x] brainstorm.md — 头脑风暴记录
- [x] integration.md — 跨模块协调视图
- [x] control-plane/spec.delta.md — 需求增量（US-SYNC-01/02/03 + AC-S01~S06）
- [x] control-plane/hld.delta.md — 概要设计增量（SyncHandler + SyncService + 同步流程）
- [x] control-plane/design.delta.md — 详细设计增量（两步接口设计 + 错误处理）
- [x] control-plane/data-model.delta.md — 数据模型增量（复用现有 models 表）
- [x] control-plane/contracts.delta.md — 接口契约增量（2 个新端点）
- [x] control-plane/task.md — 任务清单（8 个任务，全部完成）
- [x] ops-console/spec.delta.md — 需求增量（US-SYNC-UI-01/02 + AC-UI-01~05）
- [x] ops-console/hld.delta.md — 概要设计增量（SyncModelsDialog + 交互流程）
- [x] ops-console/design.delta.md — 详细设计增量（弹窗组件设计 + 状态管理）
- [x] ops-console/data-model.delta.md — 数据模型增量（TypeScript 接口定义）
- [x] ops-console/contracts.delta.md — 接口契约增量（syncModels + confirmSync）
- [x] ops-console/task.md — 任务清单（10 个任务，全部完成）