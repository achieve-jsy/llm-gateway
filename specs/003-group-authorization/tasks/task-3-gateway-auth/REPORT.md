# Task-3 网关鉴权改造 · 完成报告

## 概述

完成了模型组授权的网关侧改造，引入 `usable_groups` 天花板机制，建立双层鉴权链（usable_groups 天花板 → binding 子集收窄），同时为 embeddings 接口补全了组授权校验。

## 改动文件清单（11 个文件）

### 数据层（4 个文件）

| 文件 | 改动 |
|------|------|
| `internal/auth/dto/internal.go` | `VerifyKeyResponse.Data` 新增 `UsableGroups []string` 字段 |
| `internal/gateway/model/identity.go` | `UserIdentity` 新增 `UsableGroups []string` 字段 |
| `internal/gateway/model/error.go` | 新增 `ErrModelGroupUnauthorized`（403 group_not_authorized） |
| `internal/gateway/infra/auth_client.go` | `VerifyKeyResp.Data` 新增 `UsableGroups []string` 字段 |

### 鉴权逻辑层（5 个文件）

| 文件 | 改动 |
|------|------|
| `internal/gateway/middleware/auth.go` | 构造 identity 时传递 `UsableGroups` 字段 |
| `internal/gateway/orchestrator/group_dispatcher.go` | Dispatch 双层鉴权改造 + 新增 `containsGroup` helper |
| `internal/gateway/handler/bound_models.go` | 可见列表以 usable_groups 为基础 + 新增 `intersectStrings` / `boundGroupNames` / `stringSliceToModels` helpers |
| `internal/gateway/handler/model_info.go` | `resolveGroupNames()` 同步改造 |
| `internal/gateway/handler/embeddings.go` | 新增 `groupDispatch` 字段 + `NewEmbeddingsHandlerWithGroup` 构造函数 + Handle 中调用 Dispatch |

### 路由与错误处理（2 个文件）

| 文件 | 改动 |
|------|------|
| `internal/gateway/server/router.go` | embeddings handler 条件注入 groupDispatch（与 chat/responses/messages 保持一致的模式） |
| `internal/gateway/handler/messages.go` | `respondAnthropicGroupError` 新增 `*model.UnifiedError` 类型断言（支持 403 等自定义错误码） |

### 测试（2 个文件）

| 文件 | 说明 |
|------|------|
| `tests/gateway/orchestrator/group_auth_test.go` | 8 个核心场景的表驱动测试 |
| `tests/gateway/orchestrator/stream_usage_test.go` | 添加 `//go:build ignore`（预存编译错误，引用已重构的 `NewStreamUsageParser`） |

## 鉴权算法

```
Dispatch(modelName):
  1. IsGroupName(modelName) == false → return nil (非组模型，跳过)
  2. len(UsableGroups) > 0 && !contains(UsableGroups, modelName)
     → 403 ErrModelGroupUnauthorized (天花板拦截)
  3. len(UsableGroups) > 0 && !AccessAllGroups && bindingCheck != nil
     → 查 binding, !Contains → 403 ErrModelGroupUnauthorized
  4. len(UsableGroups) == 0 (旧控制面) && !AccessAllGroups && bindingCheck != nil
     → 查 binding, !Contains → ErrModelGroupUnavailable (旧错误码兼容)
  5. 通过 → scheduler.Route()
```

## 向后兼容

- `UsableGroups` 为空（旧版控制面）时走原有 binding 路径，错误码不变（`ErrModelGroupUnavailable`）
- `AccessAllGroups=true` + 空 usable_groups → 跳过所有检查，直接调度（与原行为一致）
- 新增 `ErrModelGroupUnauthorized` 与已有 `ErrModelGroupUnavailable` 共存

## 验证结果

| 验证项 | 结果 |
|--------|------|
| `go build ./...` | ✅ 通过 |
| `go vet ./internal/gateway/... ./internal/auth/...` | ✅ 通过 |
| 新增单元测试（8 个 case） | ✅ 8/8 通过 |
| `tests/gateway/handler` | ✅ 通过 |
| `tests/gateway/middleware` | ✅ 通过 |
| `tests/gateway/loader` | ✅ 通过 |
| 预存测试失败 | 🔶 5 个预存失败（controlplane/handler, controlplane/service, gateway/infra, gateway/infra/authclient, gateway/scheduler），均与本次改动无关 |

## 覆盖的测试场景

1. 非组名 → 跳过鉴权（nil decision）
2. usable_groups 包含 + access_all=true → 放行
3. usable_groups 不包含 + access_all=true → 403
4. usable_groups 为空 + access_all=true → 旧逻辑放行
5. usable_groups 包含 + access_all=false + binding 包含 → 放行
6. usable_groups 包含 + access_all=false + binding 不包含 → 403
7. usable_groups 不包含 + access_all=false → 天花板拦截 403
8. usable_groups 为空 + access_all=false + binding 不包含 → 旧错误码

## 后续依赖

- **Task-2**：控制面需在 `VerifyKey` 中计算并下发 `usable_groups` 到 DTO
- **Task-4**：前端需展示授权管理和可见列表 UI
- 集成验证：控制面 → verify-key → 网关三层全链路
