# 变更记录 — CHG-20260523-001 SSO 登录 + Key 自助管理

## 基本信息

| 属性 | 内容 |
| :--- | :--- |
| **变更编号** | CHG-20260523-001 |
| **变更名称** | SSO 登录 + Key 自助管理 |
| **归档时间** | 2026-05-25 |

## 变更概述

实现公司 SSO（OIDC）对接与员工登录体系，建立"身份 → Key → 调用追溯"闭环。员工可自助创建和管理 API Key（sk- 前缀、明文仅返回一次、AES-256-GCM 加密存储），通过脱敏列表查看自有 Key（仅展示末尾 4 位），执行禁用/启用/删除操作。gateway-api 集成 API Key 鉴权中间件实现 API Key → User → Department 身份解析链路，新增 GET /v1/me/models 基于当前身份的模型查询接口。ops-console 新增 OIDC 登录回调页、Key 管理页面、模型列表页面和首页使用文档。

## 涉及模块

| 模块 | 操作 | 说明 |
| :--- | :--- | :--- |
| auth | 新增 | SSO（OIDC）对接、JWT 签发、API Key 鉴权中间件、用户与部门数据写入 |
| control-plane | 更新 | 新增 Key CRUD 接口（创建/列表/禁用/启用/删除）及 Key 与 user_id 强绑定逻辑 |
| gateway-api | 更新 | 集成 API Key 鉴权中间件，新增 GET /v1/me/models 接口 |
| ops-console | 更新 | 新增 OIDC 登录回调页、Key 管理页面、模型列表页面、首页使用文档 |

## 关键决策

- Key 格式采用 `sk-` 前缀 + UUID v7 生成，明文仅在创建响应中返回一次，存储层以 AES-256-GCM 加密密文保存并辅以 SHA-256 hash 快速查找
- Key 与 user_id 强绑定不可更改，构成 API Key → User → Department 完整鉴权链路
- 禁用 Key 立即生效，不依赖延迟刷新机制；控制台和 API 调用在鉴权环节即时拒绝
- 密钥由网关统一生成，后续版本再考虑用户自定义前缀；单用户 Key 数量上限待产品确认
- 鉴权拆分为 auth 模块（SSO/JWT/中间件）与 control-plane（Key CRUD），auth 通过内部预共享 Token 调用 control-plane 验证 Key

## 制品清单

- [x] change.yaml — 变更元数据 + 功能范围边界
- [x] brainstorm.md — 头脑风暴记录
- [x] integration.md — 跨模块协调视图
- [x] auth/spec.md — 模块级需求规范
- [x] auth/hld.md — 概要设计
- [x] auth/design.md — 详细设计
- [x] auth/data-model.md — 数据模型
- [x] auth/contracts.md — 接口契约
- [x] auth/research.md — 技术研究索引
- [x] auth/research/*.md — 深研究子文件
- [x] control-plane/spec.delta.md — 需求增量变更
- [x] control-plane/hld.delta.md — 概要设计增量变更
- [x] control-plane/design.delta.md — 详细设计增量变更
- [x] control-plane/data-model.delta.md — 数据模型增量变更
- [x] control-plane/contracts.delta.md — 接口契约增量变更
- [x] gateway-api/spec.delta.md — 需求增量变更
- [x] gateway-api/hld.delta.md — 概要设计增量变更
- [x] gateway-api/design.delta.md — 详细设计增量变更
- [x] gateway-api/data-model.delta.md — 数据模型增量变更
- [x] gateway-api/contracts.delta.md — 接口契约增量变更
- [x] ops-console/spec.delta.md — 需求增量变更
- [x] ops-console/hld.delta.md — 概要设计增量变更
- [x] ops-console/design.delta.md — 详细设计增量变更
- [x] ops-console/data-model.delta.md — 数据模型增量变更
- [x] ops-console/contracts.delta.md — 接口契约增量变更
- [x] auth/task.md — 模块任务清单
- [x] control-plane/task.md — 模块任务清单
- [x] gateway-api/task.md — 模块任务清单
- [x] ops-console/task.md — 模块任务清单