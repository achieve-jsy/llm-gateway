# Task 1 完成报告: 数据模型 + 迁移

**状态**: ✅ 完成
**执行时间**: 2026-06-16

## 变更文件清单

| # | 文件 | 动作 | 说明 |
|---|------|------|------|
| 1 | `internal/controlplane/model/group.go` | 修改 | Group struct 新增 `OpenMode` 字段（第 23 行） |
| 2 | `internal/controlplane/model/group_entitlement.go` | 新建 | GroupEntitlement model + scope 常量（25 行） |
| 3 | `tools/atlas-loader/main.go` | 修改 | 注册 `GroupEntitlement` 和 `ModelStatusLog`（第 28、34 行） |
| 4 | `atlas-migrations/20260616122543_add_group_open_mode_and_entitlements.sql` | 新建 |Atlas 迁移 SQL（22 行） |
| 5 | `atlas-migrations/atlas.sum` | 更新 | 重新计算的校验和 |

## 技术决策

### 1. Atlas diff 漂移处理

运行 `make atlas-diff` 时，Atlas 检测到 GORM model 与上次迁移状态之间存在大量漂移（`health_check_configs`、`models`、`users` 等表的类型变更和新增列）。这些属于**预存 drift**，不是本次任务引入的。

**处理策略**：
- 注册了遗漏的 `ModelStatusLog` 到 atlas-loader（消除 DROP TABLE）
- 手动提纯迁移 SQL，仅保留本次任务相关的变更（`open_mode` + `group_entitlements`）
- 手动执行 `atlas migrate hash` 重新计算校验和
- 剩余 drift 应由后续独立迁移处理，避免混入本任务

### 2. 缺失索引补齐

data-model.md 要求 3 个索引，但 GORM model 仅声明了 2 个。补充了 `idx_ent_group_id` 索引到 GroupID 字段的 tag 中，并手动加入迁移 SQL。

### 3. group_id 类型

Atlas 生成 `group_id` 为 `bigint`（对应 Go 的 `uint`），而非 data-model.md 中的 `INTEGER`。PostgreSQL 中 `bigint` 是 `uint` 的正确等价类型，保持 Atlas 生成结果。

## 验证结果

| 验证项 | 结果 |
|--------|------|
| `go build ./...` | ✅ 通过 |
| `atlas migrate hash --env local` | ✅ 通过 |
| `atlas migrate validate --env local` | ✅ 通过 |
| SQL 审查（无意外 DROP） | ✅ 通过 |
| 存量兼容性（DEFAULT 'public'） | ✅ 存量组自动 public |

## 迁移 SQL 内容概要

```sql
-- model_groups: 新增 open_mode 字段
ALTER TABLE "model_groups" ADD COLUMN "open_mode" character varying(16) NOT NULL DEFAULT 'public';
ALTER TABLE "model_groups" ADD CONSTRAINT "chk_model_groups_open_mode" CHECK (...);

-- group_entitlements: 新建表
CREATE TABLE "group_entitlements" (...);

-- 索引
idx_ent_scope (scope_type, scope_id)
idx_ent_unique (group_id, scope_type, scope_id) -- UNIQUE
idx_ent_group_id (group_id)
```

## 下游影响

本任务为 Task 2+ 的前置依赖，以下组件现在可以基于新表结构开发：
- 控制面 entitlement 三件套（handler/service/repository）
- verify-key 改造（计算 usable_groups）
- 网关调度鉴权改造
- 前端授权管理 UI

## 注意事项

1. **未验证 apply**：按 Atlas skill 约定，本地只做了 diff + validate，apply 交给 CI
2. **预存 drift 未处理**：atlas-loader 中已注册 `ModelStatusLog`，但 users/health_check/models 表的类型漂移仍需后续独立迁移
3. **group_id 类型**：Atlas 生成 `bigint`（Go `uint` 的正确等价类型），data-model.md 写的 `INTEGER`，以 Atlas 为准
4. **级联删除**：已包含 `FOREIGN KEY (group_id) REFERENCES model_groups(id) ON DELETE CASCADE` 约束
