# Task-2 技术方案：控制面授权服务（Entitlement CRUD + UsableGroups 计算）

## 1. 现状总结

### 1.1 Task-1 已交付（无需新建迁移）

| 交付项 | 位置 | 状态 |
|--------|------|------|
| `Group.OpenMode` 字段 | `model/group.go:23` | ✅ `varchar(16)` default `'public'`，`CHECK IN ('public','controlled')` |
| `GroupEntitlement` model | `model/group_entitlement.go` | ✅ 含 `EntitlementScopeStudio` / `EntitlementScopeDept` 常量 |
| `GroupEntitlement` 注册 atlas-loader | `tools/atlas-loader/main.go:34` | ✅ |
| Atlas 迁移 SQL | `atlas-migrations/20260616122543_add_group_open_mode_and_entitlements.sql` | ✅ `open_mode` 列 + `group_entitlements` 表 + 3 个索引 |
| `GroupRepo` 辅助方法 | `repository/group_repo.go` | ✅ `FindNamesByOpenMode`、`FindNamesByIDs`、`UpdateOpenMode` |

### 1.2 代码库模式参考

| 层 | 参考文件 | 关键模式 |
|----|----------|----------|
| Repository | `repository/group_repo.go` | struct + `NewXxxRepo(db)` + GORM 查询 |
| Service | `service/key_service.go` | struct + `EventPublisher` 接口注入 + `errors.New()` 语义错误 |
| Handler | `handler/group_handler.go` | `XxxServiceInterface` 接口 + `c.ShouldBindJSON` + `errors.Is(err, ...) ` 错误映射 |
| 路由注册 | `router/router.go` | DI 在 `Setup()` 函数内 + `adminGroup` 下注册 |
| 权限 | `rbac/permission.go` + `rbac/registry.go` | `Permission` 常量 + `ScopeConstraint` 映射 |
| 事件 | `event/change_event.go` + `event/publisher.go` | `ChangeEventType` 常量 + `NewChangeEvent()` |
| verify-key | `service/key_service.go:410-468` → `dto/internal.go` → `gateway/infra/auth_client.go` | 三端 DTO 同步 |

## 2. 技术方案

### 2.1 需要新建的文件（4 个）

| # | 文件路径 | 说明 |
|---|----------|------|
| 1 | `internal/controlplane/repository/entitlement_repo.go` | EntitlementRepository：CRUD + 级联判定查询 |
| 2 | `internal/controlplane/service/entitlement_service.go` | EntitlementService：授权业务逻辑 + ComputeUsableGroups |
| 3 | `internal/controlplane/handler/entitlement_handler.go` | EntitlementHandler：REST 端点 |
| 4 | `specs/003-group-authorization/tasks/task-2-entitlement-service/REPORT.md` | 实施报告 |

### 2.2 需要修改的文件（6 个）

| # | 文件路径 | 改动说明 |
|---|----------|----------|
| 5 | `internal/controlplane/event/change_event.go` | 新增 3 个事件类型常量 |
| 6 | `internal/orgservice/rbac/permission.go` | 新增 2 个 Permission 常量 |
| 7 | `internal/orgservice/rbac/registry.go` | 注册新权限的 ScopeConstraint |
| 8 | `internal/controlplane/handler/errcodes.go` | 新增 3 个错误码 |
| 9 | `internal/controlplane/service/key_service.go` | VerifyKey 注入 EntitlementService，计算 usable_groups 并放入 VerifyResult |
| 10 | `internal/controlplane/router/router.go` | DI 连接 EntitlementRepo/Service/Handler，注册 4 条路由 |
| 11 | `internal/auth/dto/internal.go` | VerifyKeyResponse.Data 新增 `UsableGroups []string` 字段 |

### 2.3 EntitlementRepository 方法签名

```go
type EntitlementRepo struct { db *gorm.DB }

func NewEntitlementRepo(db *gorm.DB) *EntitlementRepo

func (r *EntitlementRepo) Create(ent *model.GroupEntitlement) error
  — ON CONFLICT 唯一约束时返回 ErrEntitlementAlreadyExists

func (r *EntitlementRepo) GetByID(id uuid.UUID) (*model.GroupEntitlement, error)

func (r *EntitlementRepo) Delete(id uuid.UUID) error

func (r *EntitlementRepo) SetEnabled(id uuid.UUID, enabled bool) error

func (r *EntitlementRepo) ListByGroup(groupID uint, scopeType string) ([]model.GroupEntitlement, error)
  — scopeType 为空时不筛选

func (r *EntitlementRepo) ListByScope(scopeType, scopeID string) ([]model.GroupEntitlement, error)

func (r *EntitlementRepo) FindOne(groupID uint, scopeType, scopeID string) (*model.GroupEntitlement, error)

func (r *EntitlementRepo) EnabledControlledGroupIDs(deptID, studioID string) ([]uint, error)
  — 联级判定：返回同时存在 enabled dept 行 + enabled studio 行的 controlled 组 ID
  — SQL: 自连接 group_entitlements，JOIN model_groups WHERE open_mode='controlled' AND enabled=true

func (r *EntitlementRepo) InvalidateKeyHashesByScope(ctx context.Context, publisher *event.Publisher, scopeType, scopeID string)
  — 查出该 scope 下所有 api_keys.key_hash，逐个调 publisher.InvalidateKeyAuthCache
  — 同时发布 entitlement_changed 事件供 gateway 清除本地缓存
```

### 2.4 EntitlementService 方法签名

```go
var (
    ErrEntitlementNotFound   = errors.New("entitlement not found")
    ErrEntitlementExists     = errors.New("entitlement already exists")
    ErrGroupMustBeControlled = errors.New("group must be controlled to grant entitlements")
    ErrStudioNotGranted      = errors.New("studio must be granted before dept can be granted")
    ErrForbidden             = errors.New("insufficient permission for this operation")
)

type EntitlementItem struct {
    ID        uuid.UUID  `json:"id"`
    GroupID   uint       `json:"group_id"`
    ScopeType string     `json:"scope_type"`
    ScopeID   string     `json:"scope_id"`
    Enabled   bool       `json:"enabled"`
    GrantedBy *uuid.UUID `json:"granted_by,omitempty"`
    CreatedAt time.Time  `json:"created_at"`
}

type EntitlementService struct {
    repo      *repository.EntitlementRepo
    groupRepo *repository.GroupRepo
    publisher *event.Publisher
}

func NewEntitlementService(repo *repository.EntitlementRepo, groupRepo *repository.GroupRepo, publisher *event.Publisher) *EntitlementService

func (s *EntitlementService) SetOpenMode(ctx context.Context, groupID uint, openMode string, operatorID uuid.UUID) error
  — 仅 admin 调用（handler 已 guard）。如果从 controlled→public，触发缓存失效。

func (s *EntitlementService) GrantStudio(ctx context.Context, groupID uint, studioID string, grantedBy uuid.UUID) (*EntitlementItem, error)
  — 仅 admin（handler guard）。校验 group.open_mode='controlled'。

func (s *EntitlementService) GrantDept(ctx context.Context, groupID uint, deptID string, grantedBy uuid.UUID, actor *rbac.Actor) (*EntitlementItem, error)
  — studio_admin/dept_admin 调用（handler guard）。
  — 额外校验：1）group.open_mode='controlled' 2）存在该 dept 所属 studio 的 enabled studio 行
  — actor.HasDept(deptID) 数据级权限

func (s *EntitlementService) Revoke(ctx context.Context, entitlementID uuid.UUID, operatorID uuid.UUID) error
  — 查出行记录 → Delete/Disable → 触发缓存失效

func (s *EntitlementService) ListEntitlements(ctx context.Context, groupID uint, scopeType string) ([]EntitlementItem, error)

func (s *EntitlementService) ComputeUsableGroups(ctx context.Context, deptID, studioID string) ([]string, error)
  — 算法：public 组名 ∪ controlled 组中 EnabledControlledGroupIDs 对应的组名
  — deptID 为空时仅返回 public 组
```

### 2.5 EntitlementHandler 端点

| 路由 | 方法 | Guard 权限 | 说明 |
|------|------|-----------|------|
| `PUT /api/v1/groups/:id/open-mode` | `SetOpenMode` | `PermEntitlementManage`（ScopeGlobal） | 设置开放模式 |
| `POST /api/v1/groups/:id/entitlements` | `Grant` | 根据 scope_type 分支判断 | scope_type=studio 需 ScopeGlobal；scope_type=dept 需 ScopeStudio + 数据级校验 |
| `DELETE /api/v1/groups/:id/entitlements/:eid` | `Revoke` | ScopeStudio（handler 内进一步校验） | 撤销授权 |
| `GET /api/v1/groups/:id/entitlements` | `List` | ScopeManage（任意管理角色可查） | 查询授权列表 |

### 2.6 RBAC 权限

```go
// permission.go 新增
PermEntitlementManage    Permission = "entitlement:manage"       // 平台管理员：设开放模式 + 授权工作室
PermEntitlementGrantDept Permission = "entitlement:grant:dept"   // 工作室负责人：下放/开通部门

// registry.go 注册
PermEntitlementManage:    {ScopeGlobal},   // 仅 admin
PermEntitlementGrantDept: {ScopeStudio},   // studio_admin（自己工作室范围）或 admin（全局）
```

### 2.7 事件类型

```go
// change_event.go 新增
EntitlementGranted   ChangeEventType = "entitlement_granted"
EntitlementRevoked   ChangeEventType = "entitlement_revoked"
EntitlementSetMode   ChangeEventType = "entitlement_set_mode"
```

### 2.8 缓存失效策略

授权变更（Grant / Revoke / SetOpenMode 变为 controlled→public）时：

1. **Redis 直删**：查出受影响 scope 下所有 `api_keys.key_hash`，调用 `publisher.InvalidateKeyAuthCache(keyHash)` 删除两层 Redis 缓存
2. **Pub/Sub 事件**：发布 `entitlement_changed`（resourceType=`entitlement`），gateway 订阅者可清除本地 KeyCache

实现方式：`EntitlementRepo.InvalidateKeyHashesByScope` 执行 SQL 查询 + 循环调用 `InvalidateKeyAuthCache`。

### 2.9 ComputeUsableGroups 算法 SQL

```sql
-- public 组（一步到位）
SELECT name FROM model_groups
WHERE open_mode = 'public' AND enabled = true AND deleted_at IS NULL

UNION

-- controlled 组需要联级判定（dept 行 + studio 行 同时 enabled）
SELECT mg.name
FROM model_groups mg
WHERE mg.open_mode = 'controlled' AND mg.enabled = true AND mg.deleted_at IS NULL
  AND mg.id IN (
    SELECT dept_ent.group_id
    FROM group_entitlements dept_ent
    JOIN group_entitlements studio_ent
      ON studio_ent.group_id = dept_ent.group_id
     AND studio_ent.scope_type = 'studio'
     AND studio_ent.scope_id = ?   -- studioID
     AND studio_ent.enabled = true
    WHERE dept_ent.scope_type = 'dept'
      AND dept_ent.scope_id = ?   -- deptID
      AND dept_ent.enabled = true
  )
```

### 2.10 verify-key 集成

**修改链路**：

```
KeyService.VerifyKey()
  → 调 s.entitlementSvc.ComputeUsableGroups(k.DeptID, k.StudioID)
  → 将结果填入 VerifyResult.UsableGroups
```

**DTO 三端同步**：

| 文件 | 字段 | 类型 |
|------|------|------|
| `service/key_service.go` `VerifyResult` | `UsableGroups []string` `json:"usable_groups,omitempty"` | 新增 |
| `dto/internal.go` `VerifyKeyResponse.Data` | `UsableGroups []string` `json:"usable_groups,omitempty"` | 新增 |
| `model/key.go` `ApiKey` | 不改（DeptID/StudioID 已有） | — |

**注意**：gateway 侧消费（`UserIdentity` + `auth_client.go` + `group_dispatcher.go`）属于 **Task-3**，不在本任务范围内。但 DTO 字段需提前加好以保证接口契约。

### 2.11 错误码

```go
// errcodes.go 新增
CodeEntitlementConflict = "entitlement_conflict"   // 409 已存在
CodeEntitlementNotFound = "entitlement_not_found"   // 404
CodeEntitlementForbidden = "entitlement_forbidden"  // 403 数据级权限不足
CodeGroupNotControlled  = "group_not_controlled"    // 422 组不是 controlled 模式
CodeStudioNotGranted    = "studio_not_granted"      // 422 工作室未被下放
```

### 2.12 KeyService 改造

```go
// key_service.go 修改

type KeyService struct {
    repo            *repository.KeyRepo
    keyGroupSvc     *KeyGroupService
    entitlementSvc  EntitlementServiceInterface  // 新增
    publisher       EventPublisher
}

// VerifyResult 新增字段
type VerifyResult struct {
    ...
    UsableGroups []string `json:"usable_groups,omitempty"`  // 新增
}

// VerifyKey 方法中增加调用
func (s *KeyService) VerifyKey(ctx context.Context, keyRaw string) (*VerifyResult, error) {
    ...
    var usableGroups []string
    if s.entitlementSvc != nil && k.DeptID != "" {
        usableGroups, _ = s.entitlementSvc.ComputeUsableGroups(ctx, k.DeptID, k.StudioID)
    }
    
    return &VerifyResult{
        ...
        UsableGroups: usableGroups,
    }, nil
}
```

### 2.13 router.go 接线

```go
// router.go Setup() 中新增（在 groupSvc 附近）
entitlementRepo := repository.NewEntitlementRepo(db)
entitlementSvc := service.NewEntitlementService(entitlementRepo, groupRepo, publisher)

// 注入到 KeyService
keySvc := service.NewKeyService(keyRepo, keyGroupSvc, service.NewEventPublisherAdapter(publisher))
keySvc.SetEntitlementService(entitlementSvc)

// Handler + 路由注册（在 adminGroup 内）
entitlementHandler := handler.NewEntitlementHandler(entitlementSvc)
adminGroup.PUT("/groups/:id/open-mode", guard(rbac.PermEntitlementManage), entitlementHandler.SetOpenMode)
adminGroup.POST("/groups/:id/entitlements", entitlementHandler.Grant)  // Grant 内部按 scope_type 分支判断权限
adminGroup.DELETE("/groups/:id/entitlements/:eid", guard(rbac.PermEntitlementGrantDept), entitlementHandler.Revoke)
adminGroup.GET("/groups/:id/entitlements", entitlementHandler.List)
```

## 3. 实施分解与委派策略

| 步骤 | 文件 | 类别 | 委派方式 |
|------|------|------|---------|
| 1 | `event/change_event.go` | quick | 直接 edit |
| 2 | `rbac/permission.go` + `rbac/registry.go` | quick | 直接 edit |
| 3 | `handler/errcodes.go` | quick | 直接 edit |
| 4 | `repository/entitlement_repo.go` | unspecified-high | 自己写（与 service 紧密耦合） |
| 5 | `service/entitlement_service.go` | unspecified-high | 自己写（核心算法） |
| 6 | `handler/entitlement_handler.go` | unspecified-high | 自己写 |
| 7 | `service/key_service.go` | quick | 直接 edit（注入 + 调用） |
| 8 | `dto/internal.go` | quick | 直接 edit |
| 9 | `router/router.go` | quick | 直接 edit |
| 10 | `go build` 验证 | — | bash |
| 11 | 单元测试 | deep | sub-agent |
| 12 | REPORT.md | writing | 自己写 |

由于所有文件紧密耦合（接口定义跨文件引用），我决定自己按序编写所有步骤以保证一致性。
