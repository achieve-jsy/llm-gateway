# Task-2 技术方案：控制面授权服务（Entitlement CRUD + UsableGroups 计算）

## 1. 调研结论

### 1.1 代码库分析

| 组件 | 现状 | 结论 |
|------|------|------|
| **三层架构** | handler→service→repo，依赖注入在 `router.go` | 严格遵循 |
| **RBAC** | `perm.go` 定义 Permission → `registry.go` 注册 ScopeConstraint → `guard(perm)` 做路由中间件 | 新增 2 个权限 |
| **事件系统** | `event.Publisher` → Redis pub/sub `config:change` → gateway subscriber | 新增事件类型 |
| **缓存失效** | `Publisher.InvalidateKeyAuthCache(keyHash)` 直删 Redis；`key_updated` 事件清 local cache | 组合使用 |
| **VerifyKey** | 返回 `VerifyResult` → handler 序列化 → gateway 反序列化为 `VerifyKeyResp` | 加 `UsableGroups` |
| **Task-1 交付** | `Group.OpenMode` ✅、`GroupEntitlement` model ✅、`GroupRepo` 辅助方法 ✅、迁移 ❌ | 需补迁移 |

### 1.2 已有可复用代码

- `GroupRepo.FindNamesByOpenMode("public")` — 查公开组名
- `GroupRepo.FindNamesByIDs([]uint)` — 按 ID 查组名
- `GroupRepo.UpdateOpenMode(id, mode)` — 更新开放模式
- `event.Publisher.InvalidateKeyAuthCache(ctx, keyHash)` — 删 Redis 缓存
- `service.EventPublisher` 接口 + `publisherAdapter` — KeyService 已用的模式
- `model.EntitlementScopeStudio` / `model.EntitlementScopeDept` — scope 常量
- `rbac.Actor.HasStudio(id)` / `HasDept(id)` — 数据级权限判定

## 2. 架构设计

### 2.1 依赖关系图

```
router.go (DI 装配)
    ├── EntitlementRepo ──────────► group_entitlements 表
    ├── EntitlementService ───────► EntitlementRepo + GroupRepo + KeyRepo + Publisher
    └── EntitlementHandler ──────► EntitlementService + rbac.Actor

KeyService.VerifyKey
    └── EntitlementService.ComputeUsableGroups(deptID, studioID)
```

### 2.2 UsableGroups 算法

```
UsableGroups(dept_id, studio_id) =
  { g.Name | g.open_mode='public' AND g.enabled=true }
  ∪
  { g.Name | g.open_mode='controlled' AND g.enabled=true
    AND ∃ entitlement(group_id, scope='studio', scope_id=studio_id, enabled=true)
    AND ∃ entitlement(group_id, scope='dept',   scope_id=dept_id,   enabled=true)
  }
```

### 2.3 缓存失效策略

授权变更时采用**双管齐下**：

1. **Redis 直接删** — 查出受影响部门/工作室下所有 Key 的 key_hash，调 `InvalidateKeyAuthCache` 删 `key:auth:verify:{hash}` + `gateway:keycache:{hash}`
2. **事件发布** — 发布 `entitlement_changed` 事件（resourceType=`entitlement`），供 gateway 未来订阅处理 local cache

### 2.4 RBAC 权限模型

| Permission | ScopeConstraint | 说明 |
|------------|-----------------|------|
| `PermEntitlementManage` | `ScopeGlobal` | admin: 设开放模式 + 授权工作室 |
| `PermEntitlementGrantDept` | `ScopeStudio` | studio_admin: 下放/开通部门（handler 内进一步校验 scope） |

## 3. 实现清单

### 3.1 新建文件（6 个）

| 文件 | 说明 |
|------|------|
| `migrations/049_add_group_entitlements_table.up.sql` | DDL: open_mode + group_entitlements 表 + 索引 |
| `migrations/049_add_group_entitlements_table.down.sql` | 回滚 DDL |
| `internal/controlplane/repository/entitlement_repo.go` | EntitlementRepository |
| `internal/controlplane/service/entitlement_service.go` | EntitlementService + ComputeUsableGroups |
| `internal/controlplane/handler/entitlement_handler.go` | 4 个 REST handler |
| `specs/..task-2../REPORT.md` | 实施报告 |

### 3.2 修改文件（6 个）

| 文件 | 改动 |
|------|------|
| `internal/controlplane/event/change_event.go` | +6 事件类型常量 |
| `internal/orgservice/rbac/permission.go` | +2 Permission 常量 |
| `internal/orgservice/rbac/registry.go` | +2 Registry 条目 |
| `internal/controlplane/handler/errcodes.go` | +3 错误码 |
| `internal/controlplane/service/key_service.go` | +entitlementSvc 依赖, VerifyKey 计算 usable_groups |
| `internal/controlplane/router/router.go` | DI 装配 + 路由注册 |
| `internal/auth/dto/internal.go` | +UsableGroups 字段 |
