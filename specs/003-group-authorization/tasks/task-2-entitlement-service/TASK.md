# Task 2: 控制面授权服务（Entitlement CRUD + UsableGroups 计算）

## 背景

模型组授权功能的核心后端逻辑：
- 提供「设置开放模式」「授权工作室」「下放/开通部门」「撤销授权」「查询授权列表」五个 API
- 计算 `UsableGroups`（某 Key/部门可用的模型组集合），在 verify-key 时下发给网关
- 授权变更后触发 KeyCache 主动失效（即时生效）

本任务依赖 Task-1（数据模型已就绪），与 Task-3（网关改造）、Task-4（前端）可并行开发（接口契约已定义）。

## 需要完成的功能

### 1. EntitlementRepository

新文件：`internal/controlplane/repository/entitlement_repo.go`

核心方法：
- `Create(ctx, ent *model.GroupEntitlement) error`
- `Delete(ctx, id uuid.UUID) error`
- `SetEnabled(ctx, id uuid.UUID, enabled bool) error`
- `ListByGroup(ctx, groupID uint, scopeType string) ([]model.GroupEntitlement, error)`
- `ListByScope(ctx, scopeType, scopeID string) ([]model.GroupEntitlement, error)`
- `FindOne(ctx, groupID uint, scopeType, scopeID string) (*model.GroupEntitlement, error)`
- `EnabledControlledGroupIDs(ctx, deptID, studioID string) ([]uint, error)` — 联级判定查询

### 2. EntitlementService

新文件：`internal/controlplane/service/entitlement_service.go`

核心方法：
- `SetOpenMode(ctx, groupID uint, openMode string, operatorID uuid.UUID) error` — 设置开放模式
- `GrantStudio(ctx, groupID uint, studioID string, grantedBy uuid.UUID) error` — 平台授权工作室
- `GrantDept(ctx, groupID uint, deptID string, grantedBy uuid.UUID) error` — 工作室/部门负责人开通
- `Revoke(ctx, entitlementID uuid.UUID, operatorID uuid.UUID) error` — 撤销（触发失效事件）
- `ListEntitlements(ctx, groupID uint, scopeType string) ([]EntitlementItem, error)`
- `ComputeUsableGroups(ctx, deptID, studioID string) ([]string, error)` — 可用集合算法

**可用集合算法**：
```
UsableGroups(deptID, studioID) =
  所有 open_mode='public' 且 enabled=true 的组名
  ∪
  open_mode='controlled' 且 enabled=true 的组中：
    同时存在 (group_id, scope_type='studio', scope_id=studioID, enabled=true)
    和       (group_id, scope_type='dept', scope_id=deptID, enabled=true)
    的组名
```

**RBAC 校验规则**：
- `SetOpenMode` → 仅 `role=admin`
- `GrantStudio` → 仅 `role=admin`
- `GrantDept` → `role=studio_admin`（scope 包含该工作室）或 `role=dept_admin`（scope 包含该部门），且该组已授权给对应工作室
- `Revoke` → 对应上级角色

### 3. EntitlementHandler

新文件：`internal/controlplane/handler/entitlement_handler.go`

路由注册在 `internal/controlplane/router/router.go` 的 admin 组下：

```
PUT    /api/v1/groups/:id/open-mode      → SetOpenMode
POST   /api/v1/groups/:id/entitlements   → Grant (根据 scope_type 走不同逻辑)
DELETE /api/v1/groups/:id/entitlements/:eid → Revoke
GET    /api/v1/groups/:id/entitlements   → List
```

### 4. verify-key 集成

修改 `internal/controlplane/service/key_service.go` 的 VerifyKey 方法：
- 调用 `entitlementService.ComputeUsableGroups(key.DeptID, key.StudioID)`
- 将结果放入 verify-key 响应的 `usable_groups` 字段

修改 `internal/auth/dto/internal.go` 的 `VerifyKeyResponse`：
- Data 内新增 `UsableGroups []string \`json:"usable_groups"\``

### 5. 缓存失效

授权变更（Grant/Revoke/SetOpenMode 改为 controlled→public）时：
- 发布事件，触发与该组相关 Key 的 KeyCache 主动失效
- 参考现有 `internal/controlplane/event` 模式

## 验证

- 所有新增 API 单元测试覆盖（happy path + RBAC 拒绝 + 级联判定）
- `go build ./...` 通过
- verify-key 响应含 `usable_groups` 字段

## 关键约定

- 遵循现有三件套模式（handler/service/repository）
- 遵循现有路由注册模式（参考 `router.go` 中的 admin 路由组）
- 遵循现有事件发布模式（参考 `event/` 目录）
- RBAC 校验参考 `internal/orgservice/` 中的 UserRole 和 ScopeType

## 相关文件

- `internal/controlplane/router/router.go` — 路由注册
- `internal/controlplane/service/key_service.go` — VerifyKey 方法
- `internal/controlplane/event/` — 事件模式
- `internal/auth/dto/internal.go` — VerifyKeyResponse
- `internal/orgservice/model/models.go` — RBAC 角色定义
- `specs/003-group-authorization/spec.md` §8-§11 — 需求与接口契约
