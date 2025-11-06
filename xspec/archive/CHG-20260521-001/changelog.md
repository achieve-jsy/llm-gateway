# 变更记录 — CHG-20260521-001

## 变更概述

**变更名称**：统一接入强兼容主链路
**变更编号**：CHG-20260521-001
**完成日期**：2026-05-21
**涉及模块**：gateway-api, control-plane, ops-console

## 变更内容

### 新增模块

- **gateway-api** (web-backend)：统一 OpenAI 兼容入口，多厂商转发（百炼/DeepSeek/Anthropic/Bedrock），流式+普通+错误三类分治
- **control-plane** (web-backend)：渠道配置管理、模型注册管理、Provider Registry、配置变更通知、内部接口
- **ops-console** (web-frontend)：统一接入能力页、渠道管理页（schema 驱动动态表单）、模型管理页、标签管理页

### 关键设计决策

1. **Provider Registry 模式**：渠道定义表 + vendor_options 配置驱动，新增 OpenAI 兼容厂商零代码
2. **凭证结构化存储**：credentials 改为 JSON 加密存储，按 credential_schema 校验+脱敏
3. **通道适配层**：基于 new-api relay 层重写，统一 SSE 解析 + reasoning_content 兼容
4. **模型标签系统**：独立 model_tags 表 + 多对多关联，替代硬编码 capabilities 枚举
5. **vendor_options**：URL 构造/认证方式/流式特性等厂商差异配置化

### 制品清单

| 模块 | 制品 |
| :--- | :--- |
| gateway-api | spec.md, hld.md, design.md, data-model.md, contracts.md, task.md |
| control-plane | spec.md, hld.md, design.md, data-model.md, contracts.md, task.md |
| ops-console | spec.md, hld.md, design.md, data-model.md, contracts.md, task.md |
