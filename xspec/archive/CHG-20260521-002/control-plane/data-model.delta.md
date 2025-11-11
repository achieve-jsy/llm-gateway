# control-plane 数据模型变更 — CHG-20260521-002

> 基线：xspec/modules/control-plane/data-model.md (v1.0)

## 变更摘要

本次不新增数据表。同步功能写入现有 models 表（批量 INSERT + UPDATE enabled 字段）。

## 数据模型变更

### 新增

不适用

### 修改

不适用

### 删除

不适用

## 说明

同步模型功能复用现有 `models` 表结构：
- 批量注册：INSERT INTO models (name, provider_id, model_type, enabled) VALUES ... 其中 enabled=false
- 下架禁用：UPDATE models SET enabled=false WHERE name IN (...) AND provider_id=?

无需新增字段、索引或迁移脚本。
