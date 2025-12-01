# 变更记录 — CHG-20260526-001 Key ↔ 模型组绑定与透明暴露

## 基本信息

| 属性 | 内容 |
| :--- | :--- |
| **变更编号** | CHG-20260526-001 |
| **变更名称** | Key ↔ 模型组绑定与透明暴露 |
| **归档时间** | 2026-06-03 |

## 变更概述

在 Key CRUD 基础上新增模型组绑定能力：员工创建 Key 时可选择多个模型组，查询 Key 时返回绑定组列表，支持 Key 创建后增删绑定。gateway-api 的 /v1/models 接口改为基于 Key 绑定返回模型组列表（替代原 /v1/me/models），组名调用时校验该组在 Key 绑定范围内。

## 涉及模块

| 模块 | 操作 | 说明 |
| :--- | :--- | :--- |
| control-plane | 更新 | Key CRUD 增加模型组绑定：创建时选择、查询时返回、增量变更绑定 + 可选组列表查询 + 供 gateway-api 读取的内部接口 |
| gateway-api | 更新 | /v1/models 基于 Key 绑定组返回模型组列表；组名调用前校验 Key 绑定范围；调用 control-plane 内部接口获取绑定 |

## 关键决策

- Key 创建与绑定在同一 DB 事务内完成，保证原子性
- 绑定变更采用 PATCH 增量 {add, remove} 风格，前端一次调用完成增删
- /v1/models 返回模型组名称列表而非个体模型名，组成为 Key 对外暴露的"模型"概念
- gateway-api 通过 control-plane 内部接口实时查询 Key 绑定组，无本地缓存
- 绑定变更通过 Redis 通知 gateway-api 即时生效

## 制品清单

- [x] change.yaml — 变更元数据 + 功能范围边界
- [x] brainstorm.md — 头脑风暴记录
- [x] integration.md — 跨模块协调视图
- [x] control-plane/spec.delta.md — 需求变更
- [x] control-plane/hld.delta.md — 概要设计变更
- [x] control-plane/design.delta.md — 详细设计变更
- [x] control-plane/data-model.delta.md — 数据模型变更
- [x] control-plane/contracts.delta.md — 接口契约变更
- [x] control-plane/task.delta.md — 任务清单
- [x] gateway-api/spec.delta.md — 需求变更
- [x] gateway-api/hld.delta.md — 概要设计变更
- [x] gateway-api/design.delta.md — 详细设计变更
- [x] gateway-api/data-model.delta.md — 数据模型变更
- [x] gateway-api/contracts.delta.md — 接口契约变更
- [x] gateway-api/task.delta.md — 任务清单