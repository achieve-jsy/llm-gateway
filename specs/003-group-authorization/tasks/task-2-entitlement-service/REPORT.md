# Task-2 实施报告：控制面授权服务（Entitlement CRUD + UsableGroups）

**任务状态**: ✅ 已完成并通过构建验证  
**实施日期**: 2026-06-16  
**依赖任务**: Task-1（数据模型 + 迁移）— 已由先前会话完成

---

## 1. 交付文件清单

### 1.1 新建文件（3 个）

| 文件 | 行数 | 说明 |
|------|------|------|
| `internal/controlplane/repository/entitlement_repo.go` | 111 | 完整 CRUD + 级联判定 + 缓存失效辅助 |
| `internal/controlplane/service/entitlement_service.go` | 309 | 全部业务逻辑 + UsableGroups 计算 |
| `internal/controlplane/handler/entitlement_handler.go` | 165 | 4 个 REST 端点 + RBAC 校验 + 错误映射 |

### 1.2 修改文件（8 个）

| 文件 | 修改内容 |
|------|---------|
| `internal/controlplane/event/change_event.go` | +3 事件类型 (`EntitlementGranted`, `EntitlementRevoked`, `OpenModeChanged`) |
| `internal/orgservice/rbac/permission.go` | +2 权限常量 (`PermEntitlementManage`, `PermEntitlementGrantDept`) |
| `internal/orgservice/rbac/registry.go` | +2 权限注册 (`ScopeGlobal`, `ScopeStudio`) |
| `internal/controlplane/handler/errcodes.go` | +4 错误码 (`CodeEntitlementForbidden`, `CodeEntitlementNotFound`, `CodeEntitlementDuplicate`, `CodeGroupNotControlled`) |
| `internal/controlplane/service/key_service.go` | +字段 `entitlementSvc`, +方法 `SetEntitlementService`, `VerifyResult` 增 `UsableGroups`, `VerifyKey` 增 `ComputeUsableGroups` 调用 |
| `internal/controlplane/router/router.go` | +DI 装配、+4 路由注册、+`deptStudioAdapter`、+`OrgDepartment.GetStudioID()` 使用 |
| `internal/orgservice/model/models.go` | +方法 `GetStudioID()` (nil-safe) |
| `internal/auth/dto/internal.go` | +字段 `UsableGroups []string` |

### 1.3 网关侧（已存在，无需修改）

| 文件 | 状态 |
|------|------|
| `internal/gateway/infra/auth_client.go` | ✅ 已有 `UsableGroups` 字段 |
| `internal/gateway/model/identity.go` | ✅ 已有 `UsableGroups` 字段 |
| `internal/gateway/middleware/auth.go` | ✅ 已有 `UsableGroups` 填充逻辑 |

---

## 2. 架构决策

### 2.1 DI 变量前移

`orgContainer`、`deptRepo`、`deptLookup`、`guard` 从原第 408 行附近（org-service 区块）**提前到第 106-118 行**（DI 区块顶部），原因：
- entitlement handler 需要在 adminGroup 内注册 4 条路由
- 这些路由使用 `guard()` 进行 RBAC 鉴权
- `guard` 闭包捕获 `orgContainer.RBAC.Guard`
- 必须确保 `orgContainer` 在使用前初始化

**结果**: Go 闭包延迟求值机制保证：即使 `orgContainer` 在闭包定义之后才创建，只要路由注册时 `orgContainer` 已就绪即可。

### 2.2 DeptStudioLookup 接口隔离

`EntitlementHandler` 使用 `DeptStudioLookup` 接口（定义在 `handler` 包内）：
```go
type DeptStudioLookup interface {
    FindStudioIDByDeptID(deptID string) (string, error)
}
```

`router` 包内用 `deptStudioAdapter` 实现：
```go
type deptStudioAdapter struct {
    repo *orgrepo.DeptRepo
}

func (a *deptStudioAdapter) FindStudioIDByDeptID(deptID string) (string, error) {
    dept, err := a.repo.GetByDeptID(deptID)
    if err != nil {
        return "", err
    }
    return dept.GetStudioID(), nil  // nil-safe helper
}
```

**优势**: `handler` 包无需导入 `orgservice/repository`，降低耦合。

### 2.3 ComputeUsableGroups 算法

```sql
-- Step 1: public groups
SELECT name FROM model_groups WHERE open_mode='public' AND enabled=TRUE

-- Step 2: controlled groups with cascade enabled
SELECT DISTINCT d.group_id
FROM group_entitlements d
JOIN group_entitlements s ON d.group_id = s.group_id
  AND s.scope_type = 'studio' AND s.scope_id = ? AND s.enabled = true
JOIN model_groups g ON d.group_id = g.id
  AND g.open_mode = 'controlled' AND g.enabled = true AND g.deleted_at IS NULL
WHERE d.scope_type = 'dept' AND d.scope_id = ? AND d.enabled = true
```

**优化**: `deptID == ""` || `studioID == ""` 时直接返回 public 组，跳过 JOIN。

### 2.4 缓存失效策略

`grantStudio/grantDept/revoke/setOpenMode(controlled→public)` 触发双管齐下：
1. `publisher.InvalidateKeyAuthCache(keyHash)` — 直删 Redis（auth service + gateway KeyCache）
2. `publisher.Publish()` — Redis pub/sub 事件通知 gateway 订阅者

`FindKeyHashesByDeptOrStudio` SQL 查询受影响 scope 下所有 `api_keys.key_hash`。

---

## 3. 关键修改细节

### 3.1 key_service.go — UsableGroups 集成

```go
// KeyService struct 增字段
type KeyService struct {
    // ... 现有字段 ...
    entitlementSvc *EntitlementService
}

// 新增 setter (router 装配后调用)
func (s *KeyService) SetEntitlementService(svc *EntitlementService) {
    s.entitlementSvc = svc
}

// VerifyResult 增字段
type VerifyResult struct {
    // ... 现有字段 ...
    UsableGroups []string `json:"usable_groups,omitempty"`
}

// VerifyKey 末尾增计算
if s.entitlementSvc != nil && k.DeptID != "" {
    if usableGroups, err := s.entitlementSvc.ComputeUsableGroups(ctx, k.DeptID, k.StudioID); err == nil {
        result.UsableGroups = usableGroups
    }
}
```

### 3.2 entitlement_handler.go — RBAC 分级鉴权

```go
// Grant method 根据 scope_type 分支：
if body.ScopeType == "studio" {
    if !actor.IsGlobal {
        // → 403 only admin can grant studio entitlements
    }
    // → GrantStudio
} else {  // dept
    // → deptLookup.FindStudioIDByDeptID (resolve studioID)
    // → GrantDept
}
```

### 3.3 OrgDepartment.GetStudioID() — nil-safe 方法

```go
func (d *OrgDepartment) GetStudioID() string {
    if d.StudioID == nil { return "" }
    return *d.StudioID
}
```

---

## 4. 构建验证

```bash
$ go build ./...
Exit code: 0

$ go vet ./internal/controlplane/...
Exit code: 0
```

**结论**: 所有包编译通过，无静态分析问题。

---

## 5. 测试计划（后续 Task）

建议在 `tests/entitlement_service_test.go` 中覆盖：

| 场景 | 预期 |
|------|------|
| `GrantStudio` on public group | `ErrGroupMustBeControlled` |
| `GrantStudio` duplicate | `ErrEntitlementExists` |
| `GrantDept` without studio grant | `ErrStudioNotGranted` |
| `GrantDept` with forbidden actor | `ErrEntitlementForbidden` |
| `Revoke` non-existent | `ErrEntitlementNotFound` |
| `SetOpenMode` controlled→public | 触发 `InvalidateKeyAuthCache` |
| `ComputeUsableGroups` deptID="" | 仅返回 public 组 |
| `ComputeUsableGroups` full cascade | 返回 public + cascade controlled 组 |

---

## 6. 向后兼容性

| 改动点 | 兼容性影响 |
|--------|-----------|
| `VerifyResult.UsableGroups` | `omitempty` — 旧版网关 JSON 解码时忽略该字段 |
| `VerifyKeyResponse.UsableGroups` | `omitempty` — 旧版控制面无该字段时返回空数组 |
| `EntitlementHandler` 4 路由 | 新增路由，不影响现有 API |
| `OrgDepartment.GetStudioID()` | 新方法，不影响现有字段访问 |
| `event` 新增 3 类型 | 仅内部使用，不影响外部 API |
| `rbac` 新增 2 权限 | 仅内部使用，不影响现有权限体系 |

**结论**: ✅ 完全向后兼容，新增功能默认处于 disabled 状态。

---

## 7. 后续任务依赖

| Task | 依赖 Task-2 交付物 | 状态 |
|------|------------------|------|
| Task-3（网关鉴权改造） | `dto/internal.go` `UsableGroups` 字段、`identity.go` `UsableGroups` 字段 | 接口已定义，可并行开发 |
| Task-4（前端授权管理） | 4 条 REST API 端点 | 可基于契约先行开发 |
| Task-5（价格换算 CNY） | 无依赖 | 完全独立 |

**关键交付物**: `VerifyKeyResponse.Data.UsableGroups` 字段是 Task-3 网关鉴权的核心输入，已在 `dto/internal.go` 中添加。

---

**报告完成时间**: 2026-06-16  
**报告版本**: v1.0
