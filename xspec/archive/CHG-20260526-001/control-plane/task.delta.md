# control-plane 任务清单 — CHG-20260526-001

> 基线：xspec/modules/control-plane/task.md

---

## 阶段 1：环境与基础

- [x] T001 P0 数据库迁移：创建 key_group_bindings 表 `migrations/024_create_key_group_bindings.sql`
  - `AC-KB-01`

✓ 检查点：迁移执行成功，表结构和索引验证通过

---

## 阶段 2：数据层

- [x] T002 [P] P0 定义 KeyGroupBinding GORM 模型 `internal/controlplane/model/key_group_binding.go`
  depends: T001
- [x] T003 P0 实现 KeyGroupRepository（BatchInsert/BatchDelete/FindByKeyID） `internal/controlplane/repository/key_group_repo.go`
  depends: T002

✓ 检查点：Repository 单元测试通过（增删查均覆盖）

---

## 阶段 3：功能实现

### 3.1 US-KEY-BIND-04 — 可选模型组列表查询（P1）

> **目标**：员工在创建/管理 Key 时能看到所有可选的启用模型组
> **独立验证**：调用 `GET /api/v1/groups/available`，返回已启用模型组列表
> **AC 覆盖**：AC-KB-08

- [x] T004 P1 在 GroupHandler 中新增 ListAvailable 方法 `internal/controlplane/handler/key_handler.go`（路由至 keyHandler）
  depends: T003
- [x] T005 P1 实现 GroupService.ListAvailableGroups `internal/controlplane/service/key_group_service.go`
  depends: T003
- [x] T006 P1 注册路由 GET /api/v1/groups/available `internal/controlplane/router/router.go`
  depends: T004

✓ 检查点：`GET /api/v1/groups/available` 能独立验证——无组时返回空数组，有组时返回名称+策略类型

### 3.2 US-KEY-BIND-01 — 创建 Key 时选择模型组（P0）

> **目标**：创建 Key 时可选绑定模型组，绑定与 Key 在同一事务完成
> **独立验证**：创建 Key 传 group_ids → 查询 Key 绑定组 → 验证绑定一致
> **AC 覆盖**：AC-KB-01, AC-KB-02

- [x] T007 P0 实现 KeyGroupService.BindGroups（事务内绑定，校验组存在+启用） `internal/controlplane/service/key_group_service.go`
- [x] T008 P0 修改 KeyService.CreateKey：接收 groupIDs 参数，调 BindGroups `internal/controlplane/service/key_service.go`
- [x] T009 P0 修改 KeyHandler POST /api/v1/keys：接收 group_ids 参数，响应追加 bound_groups `internal/controlplane/handler/key_handler.go`

✓ 检查点：创建 Key 含组绑定 → 断言 Key 详情中 bound_groups 与请求一致（AC-KB-01）；不传 group_ids → Key 创建成功但 bound_groups 为空（AC-KB-02）

### 3.3 US-KEY-BIND-02 — 查询 Key 返回绑定组（P0）

> **目标**：Key 列表和详情接口返回 bound_groups 字段
> **独立验证**：已创建绑定 Key → GET list/detail → bound_groups 正确返回
> **AC 覆盖**：AC-KB-03, AC-KB-04

- [x] T010 P0 在 KeyRepository 中新增 FindKeyGroups 查询（JOIN model_groups） `internal/controlplane/repository/key_group_repo.go`
- [x] T011 [P] P0 修改 KeyHandler GET list/detail：追加 bound_groups 字段 `internal/controlplane/handler/key_handler.go`
- [x] T012 P0 实现 KeyGroupService.GetKeyGroups 含禁用组标注 `internal/controlplane/service/key_group_service.go`

✓ 检查点：GET /api/v1/keys/:id → bound_groups 含组名和 enabled 状态（AC-KB-03）；组被禁用后标注 enabled:false（AC-KB-04）

### 3.4 US-KEY-BIND-03 — 变更 Key 的模型组绑定（P0）

> **目标**：PATCH 增量变更绑定（add+remove），变更后立即生效
> **独立验证**：增绑组 → 调用能通过；解绑组 → 被拒绝
> **AC 覆盖**：AC-KB-05, AC-KB-06, AC-KB-07

- [x] T013 P0 实现 KeyHandler 绑定管理方法（ListKeyGroups + UpdateKeyGroups） `internal/controlplane/handler/key_handler.go`
- [x] T014 P0 实现 KeyGroupService.UpdateBindings（校验+去重+事务+通知） `internal/controlplane/service/key_group_service.go`
- [x] T015 P0 注册路由 GET+POST /api/v1/keys/:id/groups `internal/controlplane/router/router.go`

✓ 检查点：PATCH add G2 → 绑定含 G1+G2（AC-KB-05）；PATCH remove G1 → 仅 G2（AC-KB-06）；绑定变更后新请求即时生效

### 3.5 内部接口（供 gateway-api）

> **目标**：提供内部接口供 gateway-api 查询 Key 绑定组
> **AC 覆盖**：跨模块集成

- [x] T016 P0 在 InternalHandler 中新增 GetKeyGroups 端点 `internal/controlplane/handler/internal_handler.go`
- [x] T017 P0 注册内部路由 GET /internal/v1/keys/:id/groups `internal/controlplane/router/router.go`

✓ 检查点：带内部 Token 调用返回 [{id, name, enabled}]；无效 Token 返回 401

---

## 阶段 4：集成与收尾

- [x] T018 P0 错误处理完善：定义 KeyGroup 相关错误码常量 `internal/controlplane/handler/errcodes.go`
- [ ] T019 [P] P1 集成测试：Key 创建含组绑定全链路（创建→查询→变更→内部接口） `tests/integration/key_group_test.go`
  depends: T017
- [ ] T020 [P] P1 单元测试：KeyGroupRepository 增删查 `repository/key_group_repository_test.go`
  depends: T003
- [ ] T021 [P] P1 单元测试：KeyGroupService 绑定逻辑（含去重/校验/错误场景） `services/key_group_service_test.go`
  depends: T014
- [ ] T022 P1 Redis 通知验证：绑定变更后 gateway-api 侧收到通知 `tests/integration/redis_notify_test.go`
  depends: T015