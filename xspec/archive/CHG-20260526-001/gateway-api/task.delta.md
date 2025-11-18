# gateway-api 任务清单 — CHG-20260526-001

> 基线：xspec/modules/gateway-api/task.md

---

## 阶段 1：环境与基础

- [x] T001 P0 实现 controlplane.Client（HTTP 调用 control-plane 内部接口） `internal/gateway/binding/client.go`
  - 方法：GetKeyGroups(ctx, keyID) → []BoundGroup
  - 超时 500ms，认证 Header：X-Internal-Token
  - 解析 JSON → []BoundGroup{ID, Name, Enabled}
- [x] T002 [P] P0 定义 GroupInfo 内存结构 `internal/gateway/binding/client.go`（BoundGroup struct）
  depends: T001

✓ 检查点：Client 能成功调用 control-plane `/internal/v1/keys/:id/groups` 并反序列化

---

## 阶段 2：数据层

> gateway-api 无状态，无需数据层任务。
> 消费 control-plane `/internal/v1/keys/:id/groups` 实时数据，无本地持久化。

---

## 阶段 3：功能实现

### 3.1 US-GROUP-MODELS-01 — /v1/models 返回模型组列表（P0）

> **目标**：`GET /v1/models` 基于 Key 绑定返回模型组名称列表，兼容 OpenAI 格式
> **独立验证**：用已绑定组的 Key 调 `/v1/models` → 返回组名列表；未绑定则空列表
> **AC 覆盖**：AC-GM-01, AC-GM-02, AC-MODELS-01, AC-MODELS-02

- [x] T003 P0 修改 ModelsHandler：调 controlplane.Client.GetKeyGroups → 转换为 OpenAI 格式 `internal/gateway/handler/bound_models.go`
  depends: T001, T002
- [x] T004 P0 路由变更：将 /v1/me/models 替换为 /v1/models `internal/gateway/server/router.go`
  depends: T003

✓ 检查点：绑定 2 个组的 Key 调用 /v1/models → 返回 2 个组名（AC-GM-01）；未绑定 Key → 空列表（AC-MODELS-02）；CP 超时 → 空列表不报错

### 3.2 US-GROUP-MODELS-02 — 组名调用绑定校验（P0）

> **目标**：GroupAwareDispatcher 在组名路由前校验该组在 Key 绑定范围内
> **独立验证**：绑定的组 → 调用成功；未绑定的组 → 被拒绝
> **AC 覆盖**：AC-GM-03, AC-GM-04, AC-GM-05

- [x] T005 P0 修改 GroupAwareDispatcher.Dispatch：追加绑定校验逻辑 `internal/gateway/orchestrator/group_dispatcher.go`
  depends: T001
  - 判定组名 → 调 cpClient.GetKeyGroups → containsGroup 比对
  - 不在范围 → 返回 ErrModelGroupUnavailable
  - CP 不可达 → 安全优先，拒绝请求

✓ 检查点：已绑定组"日常推理" → 调度成功（AC-GM-04）；未绑定组"高智能" → 返回 model_unavailable（AC-GM-03）；绑定变更后新请求即时生效

---

## 阶段 4：集成与收尾

- [x] T006 P0 定义 ErrModelGroupUnavailable 错误及错误码 `internal/gateway/model/error.go`
  depends: T005
- [ ] T007 [P] P1 集成测试：/v1/models 返回组列表（含空绑定/CP超时场景） `tests/integration/models_test.go`
  depends: T004
- [ ] T008 [P] P1 集成测试：组名调用绑定校验（含绑定通过/未绑定/CP不可达/组名不存在） `tests/integration/binding_dispatch_test.go`
  depends: T006
- [ ] T009 P1 跨模块联调：与 control-plane 验证绑定查询完整链路 `tests/integration/cross_module_test.go`
  depends: T008, T017@control-plane