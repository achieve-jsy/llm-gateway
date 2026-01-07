# Task 3: 网关鉴权改造（调度 + 可见列表 + embeddings）

## 背景

网关当前的模型组授权逻辑：
- `group_dispatcher.go`：`AccessAllGroups=true` 直接放行；`=false` 查 `key_group_bindings` 是否包含目标组
- `bound_models.go`：`AccessAllGroups=true` 返回所有组；`=false` 返回 Key 绑定的组
- embeddings 不走组鉴权

改造后：
- 授权天花板 = `usable_groups`（由控制面 verify-key 计算下发），任何 Key 不可越过
- `AccessAllGroups=true` → 可用 usable_groups 全集
- `AccessAllGroups=false` → usable_groups ∩ key_group_bindings
- embeddings 同样受组授权约束

本任务依赖 Task-2 的 DTO 变更（VerifyKeyResponse 新增 `usable_groups`），但可并行开发（接口已定义）。

## 需要完成的功能

### 1. UserIdentity 新增 UsableGroups

文件：`internal/gateway/model/identity.go`

```go
type UserIdentity struct {
    UserID          string   `json:"user_id"`
    KeyID           string   `json:"key_id"`
    DepartmentID    string   `json:"department_id"`
    StudioID        string   `json:"studio_id,omitempty"`
    AccessAllGroups bool     `json:"access_all_groups"`
    LogEnabled      bool     `json:"log_enabled"`
    UsableGroups    []string `json:"usable_groups"` // NEW
}
```

### 2. auth_client.go 解析 usable_groups

文件：`internal/gateway/infra/auth_client.go`

verify-key 响应中新增 `usable_groups` 字段，解析到 `UserIdentity.UsableGroups`。

### 3. group_dispatcher.go 改造

文件：`internal/gateway/orchestrator/group_dispatcher.go`

改造 `Dispatch` 方法的鉴权逻辑：

```go
func (d *GroupAwareDispatcher) Dispatch(c *gin.Context, modelName string) (*scheduler.RouteDecision, error) {
    if !d.nameCache.IsGroupName(modelName) {
        return nil, nil
    }

    identityVal, exists := c.Get("identity")
    if exists {
        if id, ok := identityVal.(*model.UserIdentity); ok && id.KeyID != "" {
            // 第一层：usable_groups 天花板（不可越过）
            if len(id.UsableGroups) > 0 && !containsGroup(id.UsableGroups, modelName) {
                return nil, model.ErrModelGroupUnauthorized // 403：部门未开通该受控组
            }
            // 第二层：access_all_groups=false 时额外收窄到手动绑定子集
            if !id.AccessAllGroups && d.bindingCheck != nil {
                groups, err := d.bindingCheck.GetKeyGroups(c.Request.Context(), id.KeyID)
                if err != nil {
                    return nil, model.ErrModelGroupUnavailable
                }
                if !binding.ContainsGroup(groups, modelName) {
                    return nil, model.ErrModelGroupUnavailable
                }
            }
        }
    }

    // ... 后续调度逻辑不变
}
```

**注意**：需要新增错误类型 `ErrModelGroupUnauthorized`（区别于现有的 `ErrModelGroupUnavailable`），对应 HTTP 403 和 `group_not_authorized` 错误码。

### 4. bound_models.go 改造

文件：`internal/gateway/handler/bound_models.go`

改造 `Handle` 方法：

```go
func (h *BoundModelsHandler) Handle(c *gin.Context) {
    // ... identity 获取逻辑不变 ...

    // 如果有 usable_groups，用它作为可见列表基础
    if len(id.UsableGroups) > 0 {
        if id.AccessAllGroups {
            // 返回 usable_groups 全集
            items := buildModelsFromNames(id.UsableGroups, now)
            c.JSON(http.StatusOK, core.ModelsResponse{Object: "list", Data: items})
            return
        }
        // access_all_groups=false: usable_groups ∩ key_group_bindings
        groups, err := h.client.GetKeyGroups(c.Request.Context(), id.KeyID)
        // ... 取交集 ...
    }

    // 兼容旧逻辑（usable_groups 为空时，fallback 到旧行为）
    // ...
}
```

### 5. model_info.go 同步改造

文件：`internal/gateway/handler/model_info.go`

同 bound_models 逻辑，确保 `/v1/model/info` 等端点也受 usable_groups 约束。

### 6. embeddings 补鉴权

找到 embeddings handler（通常在 `internal/gateway/handler/` 或 orchestrator 中），确保其调用路径也经过 group_dispatcher 的鉴权（或共享同一前置校验逻辑）。

### 7. 新增错误类型

文件：在 `internal/gateway/model/` 中（或现有错误定义处）：

```go
var ErrModelGroupUnauthorized = errors.New("group_not_authorized")
```

确保此错误在 handler 层映射为 HTTP 403 + JSON body `{"error": {"type": "group_not_authorized", "message": "..."}}`。

## 验证

- 单元测试覆盖：
  - usable_groups 包含目标组 → 放行
  - usable_groups 不包含目标组 → 403
  - access_all_groups=true + usable_groups 包含 → 放行
  - access_all_groups=true + usable_groups 不包含 → 403
  - access_all_groups=false + usable_groups 包含 + binding 包含 → 放行
  - access_all_groups=false + usable_groups 包含 + binding 不包含 → 拒绝
  - usable_groups 为空（兼容旧逻辑）→ 走旧 binding 逻辑
- `go build ./...` 通过

## 关键约定

- 保持向后兼容：如果 `usable_groups` 为空（旧版控制面），fallback 到现有逻辑
- 不要删除现有 binding 相关代码，仅在外层加 usable_groups 天花板
- 错误码区分：`group_not_authorized`（403，授权问题）vs `model_group_unavailable`（503，服务问题）

## 相关文件

- `internal/gateway/model/identity.go` — UserIdentity
- `internal/gateway/orchestrator/group_dispatcher.go` — 调度鉴权
- `internal/gateway/handler/bound_models.go` — 可见列表
- `internal/gateway/handler/model_info.go` — 模型信息
- `internal/gateway/infra/auth_client.go` — verify-key 解析
- `internal/auth/dto/internal.go` — VerifyKeyResponse（Task-2 改）
- `specs/003-group-authorization/spec.md` §10 — 可用集合算法与放行逻辑
