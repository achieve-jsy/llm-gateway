# Task-2 控制面授权服务 — 实施报告

## 执行概要

**状态**: ✅ 已完成并验证（`go build ./...` 通过，`go vet` 通过）

Task-2 实现了模型组授权的核心控制面逻辑：EntitlementService（业务逻辑）、EntitlementHandler（REST API）、以及与 verify-key 的 UsableGroups 集成。共 **3 个新文件创建，6 个文件修改**，覆盖完整的三层架构（handler → service → repository）。

---

## 交付物清单

### 1. Repository 层

#### `internal/controlplane/repository/entitlement_repo.go` *(新建，111 行)*

实现 `EntitlementRepo` 结构体，提供完整的数据访问层：

| 方法 | 用途 |
|------|------|
| `Create(ent *model.GroupEntitlement) error` | 创建授权记录 |
| `FindByID(id uuid.UUID)` | 按 ID 查找 |
| `Delete(id uuid.UUID) error` | 删除授权记录（撤销） |
| `SetEnabled(id uuid.UUID, enabled bool) error` | 启用/禁用授权 |
| `ListByGroup(groupID, scopeType)` | 按组+可选 scope 类型列表 |
| `ListByScope(scopeType, scopeID)` | 按 scope 列表 |
| `FindOne(groupID, scopeType, scopeID)` | 精确查找唯一记录 |
| **`EnabledControlledGroupIDs(deptID, studioID)`** | **核心级联判定 SQL** |
| **`FindKeyHashesByDeptOrStudio(deptID, studioID)`** | 缓存失效辅助查询 |

`EnabledControlledGroupIDs` 实现 spec 中的级联判定算法：self-JOIN `group_entitlements` 表，同时匹配 dept 行和 studio 行且均为 enabled，JOIN `model_groups` 验证组为 `controlled` 模式且 `enabled=true`。

#### `internal/controlplane/service/entitlement_service.go` *(新建，309 行)*

实现 `EntitlementService` 结构体，封装完整业务逻辑：

**依赖注入**: `*repository.EntitlementRepo` + `*repository.GroupRepo` + `*event.Publisher`

**5 个错误变量** (package-level):
```
ErrEntitlementNotFound, ErrEntitlementExists
ErrGroupMustBeControlled, ErrStudioNotGranted
ErrEntitlementForbidden
```

| 方法 | 业务逻辑 |
|------|---------|
| `SetOpenMode` | 更新组开放模式；`controlled→public` 时触发缓存失效 |
| `GrantStudio` | 创建 studio scope 授权，验证 group 为 controlled |
| `GrantDept` | 创建 dept scope 授权，验证 studio 行已存在且 enabled |
| `Revoke` | 删除授权记录，触发缓存失效 |
| `ListEntitlements` | 返回带 `GroupName` 的 `[]EntitlementItem` |
| **`ComputeUsableGroups`** | **public 组 ∪ controlled 组中级联判定通过的组名集合** |

**缓存失效策略**: `invalidateByGroup` + `invalidateByScope` → 通过 `FindKeyHashesByDeptOrStudio` 查出受影响的 key_hash 列表，调用 `publisher.InvalidateKeyAuthCache(hash)` 主动删除 Redis 两层缓存。

**`ComputeUsableGroups` 实现**: `groupRepo.FindNamesByOpenMode("public")` ∪ `groupRepo.FindNamesByIDs(entitlementRepo.EnabledControlledGroupIDs(...))` → `mergeUnique(...)` 合并为无重复 slice。

#### `internal/controlplane/handler/entitlement_handler.go` *(重写，165 行)*

实现完整的 REST handler，符合项目现有 handler 模式：

**接口定义**:
- `EntitlementServiceInterface` — 使用标准 `context.Context`（非 `*gin.Context`）
- `DeptStudioLookup` — 抽象 dept→studio 解析，便于测试注入 mock

**4 个 REST 端点**:

| 方法 | 路由 | 功能 | RBAC |
|------|------|------|------|
| PUT | `/groups/:id/open-mode` | 设置组开放模式 | `PermEntitlementManage` (ScopeGlobal) |
| POST | `/groups/:id/entitlements` | 授权工作室或部门 | studio: admin only, dept: `actor.IsGlobal ∨ actor.HasStudio` |
| DELETE | `/groups/:id/entitlements/:eid` | 撤销授权 | Guard 保护 |
| GET | `/groups/:id/entitlements?scope_type=` | 查询授权列表 | Guard 保护 |

**错误映射** (`respondError`): 使用 `errors.Is` 精确匹配 service 层错误，返回对应的 HTTP 状态码和 error code：
- `ErrGroupNotFound` → 404 `CodeGroupNotFound`
- `ErrEntitlementNotFound` → 404 `CodeEntitlementNotFound`
- `ErrEntitlementExists` → 409 `CodeEntitlementDuplicate`
- `ErrGroupMustBeControlled` → 422 `CodeGroupNotControlled`
- `ErrStudioNotGranted` → 422 `CodeGroupNotControlled`
- `ErrEntitlementForbidden` → 403 `CodeEntitlementForbidden`

---

### 2. verify-key 集成

#### `internal/controlplane/service/key_service.go` *(修改)*

- **`VerifyResult` struct**: 新增 `UsableGroups []string` 字段 (json: `usable_groups,omitempty`)
- **`KeyService` struct**: 新增 `entitlementSvc *EntitlementService` 字段
- **`SetEntitlementService(svc)` setter**: 用于 router 注入（避免构造函数循环依赖）
- **`VerifyKey` 方法**: 在返回结果前调用 `entitlementSvc.ComputeUsableGroups(ctx, k.DeptID, k.StudioID)`，计算 key 持有者可用模型组集合

**容错设计**: 若 `entitlementSvc == nil` 或 `k.DeptID == ""`（旧 key 无部门），跳过计算返回 nil slice；错误用 `_` 忽略（不影响 verify-key 主流程）。

---

### 3. RBAC 权限点

#### `internal/orgservice/rbac/permission.go` *(修改)*

新增 2 个 Permission 常量：
```go
PermEntitlementManage    = "entitlement:manage"      // 平台管理员：设开放模式
PermEntitlementGrantDept = "entitlement:grant_dept"   // 部门下放授权
```

#### `internal/orgservice/rbac/registry.go` *(修改)*

注册 ScopeConstraint：
- `PermEntitlementManage` → `ScopeGlobal` (仅 admin)
- `PermEntitlementGrantDept` → `ScopeStudio` (studio_admin 或 admin)

---

### 4. 事件类型

#### `internal/controlplane/event/change_event.go` *(修改)*

新增 3 个事件类型：
```go
EntitlementGranted  = "entitlement_granted"
EntitlementRevoked  = "entitlement_revoked"
OpenModeChanged     = "open_mode_changed"
```

---

### 5. HTTP 错误码

#### `internal/controlplane/handler/errcodes.go` *(修改)*

新增 4 个错误码常量：
- `CodeEntitlementForbidden` (403)
- `CodeEntitlementNotFound` (404)
- `CodeEntitlementDuplicate` (409)
- `CodeGroupNotControlled` (422)

---

### 6. 数据库模型扩展

#### `internal/orgservice/model/models.go` *(修改)*

为 `OrgDepartment` 结构体添加 `GetStudioID() string` 方法：nil-safe 解引用 `*string` → `string`，使 handler 的 dept-studio 解析无需 nil 检查。

---

### 7. DI 与路由配置

#### `internal/controlplane/router/router.go` *(修改)*

**DI 接线** (lines 106-120):
1. `entitlementRepo := repository.NewEntitlementRepo(db)`
2. `entitlementSvc := service.NewEntitlementService(entitlementRepo, groupRepo, publisher)`
3. `keySvc.SetEntitlementService(entitlementSvc)` — 注入到 key 验证流程
4. `deptRepo := orgrepo.NewDeptRepo(db)`
5. `deptLookup := &deptStudioAdapter{repo: deptRepo}` — dept→studio 适配器

**`deptStudioAdapter`** (lines 485-500): 实现 `handler.DeptStudioLookup` 接口，通过 `DeptRepo.GetByDeptID` + `OrgDepartment.GetStudioID()` 解析 dept→studio 关系。

**guard 闭包** (lines 112-114): 在 entitlement 路由块内定义闭包，捕获 `orgContainer.RBAC.Guard` 方法引用，复用 `OrgContainer.RBAC` 实例。

**4 条授权路由** (lines 423-428): 注册在 adminGroup 下，使用 `guard(perm)` 闭包包裹：
```go
adminGroup.PUT("/groups/:id/open-mode", guard(PermEntitlementManage), handler.SetOpenMode)
adminGroup.POST("/groups/:id/entitlements", guard(PermEntitlementGrantDept), handler.Grant)
adminGroup.DELETE("/groups/:id/entitlements/:eid", guard(PermEntitlementGrantDept), handler.Revoke)
adminGroup.GET("/groups/:id/entitlements", guard(PermEntitlementGrantDept), handler.List)
```

**初始化顺序修复**: `orgContainer` 在 lines 107-108 提前创建，解决了原 `orgContainer` 在 ~line 400 创建时与 entitlement 路由依赖的时序冲突。

---

## 架构决策

### 1. `DeptStudioLookup` 接口抽象

Handler 依赖 `DeptStudioLookup` 接口而非直接依赖 `DeptRepo`，原因：
- 解耦 handler 与 orgservice 层
- 支持测试时注入 mock adapter
- 符合项目现有 handler 接口注入模式

### 2. `guard` 闭包 vs 方法值

使用闭包 `func(perm) gin.HandlerFunc { return func(c) { orgContainer.RBAC.Guard(perm)(c) } }` 而非直接赋值 `guard := orgContainer.RBAC.Guard`，原因是需要在闭包内动态调用 `Guard` 方法，避免在 org service 初始化前引用未创建的 `orgContainer`。

### 3. verify-key 容错设计

`ComputeUsableGroups` 的失败不影响 verify-key 主流程：
- 若 `entitlementSvc` 未注入 → 返回 nil slice（旧 gateway 兼容）
- 若 `DeptID` 为空（旧 key）→ 返回 nil slice
- 若查询错误 → `_` 忽略错误，返回 nil slice

这确保了 entitlement 功能上线不会破坏现有 key 验证。

### 4. 缓存失效策略

**双管齐下**:
1. **Redis 直删**: `publisher.InvalidateKeyAuthCache(hash)` 在 Grant/Revoke/OpenModeChanged 时直接删除 `key:auth:verify:{hash}` 和 `gateway:keycache:{hash}` 两个 Redis key
2. **Pub/Sub 事件**: 同时发布 `entitlement_granted` / `entitlement_revoked` / `open_mode_changed` 事件，供 gateway 订阅处理本地 KeyCache 清除（TTL 兜底 5min）

---

## 验证结果

```
go build ./...  →  exit code: 0
go vet ./internal/controlplane/...  →  exit code: 0
```

---

## 遗留事项（Task-3 范畴）

- **Gateway 端 UsableGroups 消费**: Task-3 需在 `group_dispatcher.go` 中用 `identity.UsableGroups` 替换原有的 `AccessAllGroups + KeyGroupBinding` 判定逻辑
- **Gateway 订阅 entitlement 事件**: Task-3 需在 gateway subscriber 中添加对 `entitlement_*` 事件的处理，主动清除本地 KeyCache
- **前端授权管理**: Task-4 需实现前端 UI 调用 PUT/POST/DELETE/GET entitlement 端点
