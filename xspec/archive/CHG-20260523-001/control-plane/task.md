# control-plane 后端任务清单

## 文档信息

| 属性 | 内容 |
| :--- | :--- |
| **所属变更** | CHG-20260523-001 |
| **模块名称** | control-plane |
| **模块类型** | web-backend |
| **创建时间** | 2026-05-23 |

---

## 阶段 1：环境与基础

**目标**：Key 领域组件、路由、错误码与内部接口口径就绪

- [x] T001 [P] P0 新增 Key 领域骨架与错误码补齐 `internal/controlplane/model/key.go`, `internal/controlplane/handler/errcodes.go`, `internal/controlplane/handler/key_handler.go`
- [x] T002 P0 装配 Key 组件与路由，注入 `/api/v1/keys*` 和 `/internal/v1/keys/verify` `internal/controlplane/router/router.go`, `internal/controlplane/service/key_service.go`, `internal/controlplane/repository/key_repo.go`
  depends: T001
- [x] T003 P1 收敛内部接口口径，统一 `POST /internal/v1/keys/verify` 契约与内部 Token 认证 `internal/controlplane/router/router.go`, `internal/controlplane/handler/internal_handler.go`, `internal/controlplane/service/key_service.go`
  depends: T002

**✓ 检查点**：Key 路由与内部验证入口已注册，模块骨架可编译

---

## 阶段 2：数据层

**目标**：`api_keys` 表、GORM 模型、仓储与验证查询就绪

- [x] T010 [P] P0 新增 `api_keys` 迁移脚本与索引 `migrations/018_add_api_keys.sql`
- [x] T011 [P] P0 定义 Key GORM 模型映射 `internal/controlplane/model/key.go`
  depends: T010
- [x] T012 P0 实现 Key Repository CRUD 与验证查询 `internal/controlplane/repository/key_repo.go`, `internal/controlplane/model/key.go`
  depends: T011
- [ ] T013 [P] P1 仓储级测试覆盖 `key_hash` 查询、状态更新、物理删除、用户链路读取 `internal/controlplane/repository/key_repo_test.go`
  depends: T012

**✓ 检查点**：api_keys 表与仓储可读写，哈希查找与索引工作正常

---

## 阶段 3：功能实现

### US-KEY-01 — 员工自助创建 API Key (P0)

**目标**：员工可创建一次性明文返回的 `sk-` Key
**独立验证**：调用 `POST /api/v1/keys` 返回 201，响应中仅本次出现明文 Key
**对应验收标准**：AC-KEY-01

- [x] T020 P0 实现创建 Key 业务逻辑：数量上限、随机生成、SHA-256、AES-256-GCM、哈希冲突重试 `internal/controlplane/service/key_service.go`, `internal/controlplane/pkg/crypto/aes.go`, `internal/controlplane/event/publisher.go`
  depends: T012
- [x] T021 P0 实现创建 Key 管理接口与 JWT 提取 `user_id` `internal/controlplane/handler/key_handler.go`, `internal/controlplane/router/router.go`, `internal/controlplane/middleware/middleware.go`
  depends: T020, T002

**✓ 检查点**：创建接口返回一次性明文，库内仅存 `key_hash/encrypted_key`

### US-KEY-02 — 员工查看自有 Key (P0)

**目标**：员工仅能查看本人 Key 列表/详情，且只见脱敏值
**独立验证**：调用 `GET /api/v1/keys` / `GET /api/v1/keys/:id` 返回本人数据与 `masked_key`
**对应验收标准**：AC-KEY-02

- [x] T022 [P] P0 实现 Key 列表/详情 Service，生成 `masked_key` 与 `key_hint` `internal/controlplane/service/key_service.go`, `internal/controlplane/repository/key_repo.go`
  depends: T012
- [x] T023 P0 实现列表/详情 Handler 与分页参数校验 `internal/controlplane/handler/key_handler.go`, `internal/controlplane/router/router.go`
  depends: T022

**✓ 检查点**：列表/详情仅返回本人 Key，完整明文不可见

### US-KEY-03 — 员工禁用/启用/删除自有 Key (P0)

**目标**：员工可安全禁用、启用、删除自有 Key
**独立验证**：依次调用 disable/enable/delete 接口，状态变更与错误语义正确
**对应验收标准**：AC-KEY-03, AC-KEY-06

- [x] T024 P0 实现状态切换与删除 Service，含 `FOR UPDATE` 归属校验和事件发布 `internal/controlplane/service/key_service.go`, `internal/controlplane/repository/key_repo.go`, `internal/controlplane/event/change_event.go`, `internal/controlplane/event/publisher.go`
  depends: T012
- [x] T025 P0 实现 disable/enable/delete Handler `internal/controlplane/handler/key_handler.go`, `internal/controlplane/router/router.go`
  depends: T024
- [x] T026 [P] P1 预留用户注销后批量禁用能力 `internal/controlplane/service/key_service.go`, `internal/controlplane/repository/key_repo.go`
  depends: T024

**✓ 检查点**：非本人 Key 返回 403，重复禁用/启用有明确幂等语义，删除后返回 404

### US-KEY-04 — Key 与 user_id 强绑定 (P0)

**目标**：内部验证接口返回完整 `API Key → User → Department` 链路
**独立验证**：调用 `POST /internal/v1/keys/verify` 返回 `key_id/status/user{user_id,name,department_id}`
**对应验收标准**：AC-KEY-04

- [x] T027 P0 实现 `VerifyKey` Service：格式校验、`key_hash` 查找、用户与部门链路查询 `internal/controlplane/service/key_service.go`, `internal/controlplane/repository/key_repo.go`
  depends: T012, T003
- [x] T028 P0 实现内部验证 Handler/路由 `internal/controlplane/handler/internal_handler.go`, `internal/controlplane/router/router.go`, `internal/controlplane/middleware/middleware.go`
  depends: T027
- [x] T029 [P] P1 补齐 JWT `user_id` 提取与 claims 适配 `internal/controlplane/auth/token.go`, `internal/controlplane/middleware/middleware.go`
  depends: T021, T028

**✓ 检查点**：内部验证稳定返回身份链路，用户不存在按 `not_found` 表现

### US-KEY-05 — 禁用 Key 立即失效 (P1)

**目标**：Key 被禁用后下一次验证请求立即被拒绝
**独立验证**：禁用成功后，随后的 verify 请求返回 `disabled`
**对应验收标准**：AC-KEY-05

- [ ] T030 P1 补充“禁用后立即失效”验证测试与缓存边界说明 `internal/controlplane/service/key_service_test.go`, `internal/controlplane/handler/internal_handler_test.go`, `internal/controlplane/handler/key_handler_test.go`
  depends: T024, T028

**✓ 检查点**：disable 后下一次 verify 直接拒绝，满足即时失效要求

---

## 阶段 4：集成与收尾

**目标**：测试、日志脱敏、接口说明与联调闭环完成

- [ ] T040 [P] P0 Service 单元测试覆盖创建、列表、状态切换、删除、验证链路 `internal/controlplane/service/key_service_test.go`
  depends: T020, T022, T024, T027
- [ ] T041 [P] P0 Handler 单元测试覆盖 6 个管理端点 + 1 个内部端点 `internal/controlplane/handler/key_handler_test.go`, `internal/controlplane/handler/internal_handler_test.go`
  depends: T021, T023, T025, T028
- [ ] T042 P0 集成测试与迁移验证 `tests/integration/key_test.go`, `tests/migration_test.go`, `migrations/016_create_api_keys.sql`
  depends: T010, T025, T028
- [ ] T043 [P] P1 安全与日志收尾，确认不泄露明文 Key `internal/controlplane/handler/key_handler.go`, `internal/controlplane/handler/internal_handler.go`, `api/swagger/doc.go`
  depends: T021, T025, T028

**✓ 检查点**：Key CRUD 与 verify 内部接口具备上线条件

---

## 任务统计

| 指标 | 数值 |
| :--- | :--- |
| 总任务数 | 17 |
| P0（必须） | 14 |
| P1（重要） | 3 |
| P2（可选） | 0 |
| 可并行任务 | 10 |
| 预计工时 | 22h |
