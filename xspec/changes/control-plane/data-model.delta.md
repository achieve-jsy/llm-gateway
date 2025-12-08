# control-plane 数据模型变更 — CHG-20260609-001

> 基线：xspec/modules/control-plane/spec.md (v1.0)（无 data-model.md 基线）

## 变更摘要

新增 5 张 org 相关表，并在 users 表追加 dept_id 字段。

---

## 新增

所有新增表定义见 `xspec/changes/org-service/data-model.md` §2（org-service 与 control-plane 共用 PostgreSQL，表定义集中在 org-service 数据模型文档中）。

新增 migration 文件：`migrations/NNN_add_org_tables.sql`（N 跟随现有最大序号+1）。

## 修改

### users 表新增字段

| 字段 | 类型 | 说明 |
| :--- | :--- | :--- |
| `dept_id` | VARCHAR(64) NULL FK → departments.id | 员工所属部门（SSO 归集后写入，存量用户为 NULL） |

## 不变

渠道、模型、模型组、Key、Catalog 等现有表结构不变。
