# org-service 任务清单

**变更**：CHG-20260609-001 — 组织架构
**模块类型**：web-backend（新增）
**文件路径前缀**：`internal/orgservice/`

---

## 阶段 1：环境与基础结构

> ⚠️ 此阶段完成前不启动后续工作

- [x] T001 [P] P0 创建 orgservice package 目录结构 `internal/orgservice/{handler,service,repository,bootstrap,model}/`
- [x] T002 [P] P0 定义领域模型类型 `internal/orgservice/model/models.go`（Studio、OrgDepartment、DeptStudioMap、UserRole、AggregateRequest/Result 等 Go struct）
- [x] T003 P0 在 `cmd/control-plane/main.go` 或初始化文件中注入 orgservice 依赖（共享 DB、Redis 连接）

> ✓ 检查点：package 结构编译通过（`go build ./internal/orgservice/...`）

---

## 阶段 2：数据层

### 2.1 数据库迁移

- [x] T004 [P] P0 编写 migration 文件 `migrations/043_add_org_tables.up.sql`：创建 studios、departments、dept_studio_map、user_roles、org_migration_log 表及所有索引
- [x] T005 P0 在 `migrations/043_add_org_tables.up.sql` 中追加 `ALTER TABLE departments ADD COLUMN studio_id/status/owner_id`

### 2.2 Repository 层

- [x] T006 P0 实现 `internal/orgservice/repository/studio_repo.go`：StudioRepository（Create、List、GetByID、DeleteIfEmpty）
- [x] T007 P0 实现 `internal/orgservice/repository/dept_repo.go`：DeptRepository（UpsertDepartment、GetByID、ListByStudio、ListPending、UpdateStatus、SetOwner）
- [x] T008 P0 实现 `internal/orgservice/repository/mapping_repo.go`：MappingRepository（GetMapping、UpsertMappingBatch、ListMappings）
- [x] T009 P0 实现 `internal/orgservice/repository/role_repo.go`：RoleRepository（InsertRole、DeleteRole、GetUserRoles、GetRolesByScope）
- [x] T010 P0 实现 `internal/orgservice/repository/user_org_repo.go`：UpsertUser（含 dept_id）、GetUserDeptID、InsertMigrationLog

> ✓ 检查点：所有 repository 单元测试通过（`go test ./internal/orgservice/repository/...`）

---

## 阶段 3：功能实现

### 3.1 Bootstrap 初始管理员（AC-BS-01~04）

**目标**：解决全新系统冷启动无 admin 问题
**独立验证**：全新数据库 + BOOTSTRAP_ADMIN 配置 → 白名单账号登录后 user_roles 写入 admin 记录
**对应 AC**：AC-BS-01、AC-BS-02、AC-BS-03、AC-BS-04

- [x] T011 P0 实现 `internal/orgservice/bootstrap/bootstrap.go`：Bootstrap.Checker（Init、CheckAndGrant、state 状态机：STANDBY/CLOSED）
- [x] T013 P0 实现 `internal/orgservice/service/org_service.go`：OrgService.AggregateUser（upsert dept、查映射、upsert user、调 Bootstrap、返回 AggregateResult）
- [x] T014 P0 实现部门迁移检测逻辑（获取旧 dept_id，变更时调用 InsertMigrationLog）
- [x] T016 P0 实现 `internal/orgservice/service/studio_service.go`：StudioService（Create、List、GetByID、Delete）含管辖域校验
- [x] T017 P0 实现 `internal/orgservice/handler/org_handler.go`：组织树/工作室/部门相关 HTTP handler
- [x] T018 P0 在 `internal/controlplane/router/router.go` 注册 `/api/v1/org/studios`、`/api/v1/org/departments`、`/api/v1/org/departments/unmapped` 路由（JWTAuth + admin）
- [x] T019 P0 实现 `internal/orgservice/service/mapping_service.go`：MappingService（ParseAndValidate CSV、ImportMappings 单事务 upsert+回填、SetDeptStudio 手工）
- [x] T020 P0 实现 `internal/orgservice/handler/mapping_handler.go`：ListMappings、ImportMappings（支持 dry_run）、SetDeptStudio
- [x] T021 P0 在路由中注册 `/api/v1/org/dept-studio-map` 及 import 接口
- [x] T023 P0 实现 `internal/orgservice/service/role_service.go`：RoleService（Grant、Revoke、CheckScope、GetUserRoles）
- [x] T024 P0 在 RoleService 中实现 Redis 缓存（`org:role:<userID>` TTL=60s，Grant/Revoke 后主动 DEL）
- [x] T025 P0 实现 `internal/orgservice/handler/role_handler.go`：GrantRole、RevokeRole、GetUserRoles
- [x] T026 P0 在路由中注册 `/api/v1/org/roles/grant`、`/api/v1/org/roles/revoke`、`/api/v1/org/roles/:user_id`，以及内部接口 `/internal/org/auth/check-scope`、`/internal/org/tree`

> ✓ 检查点：bootstrap 单元测试全绿

### 3.2 SSO 登录归集（AC-ORG-01~04）

**目标**：员工 SSO 登录后自动挂载到正确部门/工作室节点
**独立验证**：模拟 SSO callback → 调用 AggregateUser → 验证 departments / users 表 upsert 结果
**对应 AC**：AC-ORG-01、AC-ORG-02、AC-ORG-03、AC-ORG-04

- [ ] T013 P0 实现 `internal/orgservice/service/org_service.go`：OrgService.AggregateUser（upsert dept、查映射、upsert user、调 Bootstrap、返回 AggregateResult）
  depends: T006, T007, T008, T010, T011
- [ ] T014 P0 实现部门迁移检测逻辑（获取旧 dept_id，变更时调用 InsertMigrationLog）
  depends: T013
- [ ] T015 P0 为 OrgService.AggregateUser 编写单元测试 `internal/orgservice/service/org_service_test.go`（覆盖 AC-ORG-01~04：有映射/无映射/幂等/部门迁移）
  depends: T013, T014

> ✓ 检查点：归集单元测试全绿

### 3.3 组织实体管理（AC-ORG-01~04）

**目标**：提供工作室/部门查询管理接口
**独立验证**：调用 GET /api/v1/org/studios 返回列表；POST 创建后 GET 可查到

- [ ] T016 P0 实现 `internal/orgservice/service/studio_service.go`：StudioService（Create、List、GetByID、Delete）含管辖域校验
  depends: T006, T009
- [ ] T017 P0 实现 `internal/orgservice/handler/org_handler.go`：组织树/工作室/部门相关 HTTP handler
  depends: T016
- [ ] T018 P0 在 `internal/controlplane/router/router.go` 注册 `/api/v1/org/studios`、`/api/v1/org/departments`、`/api/v1/org/departments/unmapped` 路由（JWTAuth + admin）
  depends: T017, T003

### 3.4 部门↔工作室映射维护（AC-MAP-01~05）

**目标**：支持批量 CSV 导入和手工设置映射，并自动回填 pending_studio 部门
**独立验证**：上传合法 CSV → 映射写入 + dept status 回填 active
**对应 AC**：AC-MAP-01、AC-MAP-02、AC-MAP-03、AC-MAP-04、AC-MAP-05

- [ ] T019 P0 实现 `internal/orgservice/service/mapping_service.go`：MappingService（ParseAndValidate CSV、ImportMappings 单事务 upsert+回填、SetDeptStudio 手工）
  depends: T007, T008
- [ ] T020 P0 实现 `internal/orgservice/handler/mapping_handler.go`：ListMappings、ImportMappings（支持 dry_run）、SetDeptStudio
  depends: T019
- [ ] T021 P0 在路由中注册 `/api/v1/org/dept-studio-map` 及 import 接口
  depends: T020, T018
- [ ] T022 P0 为 MappingService 编写集成测试 `tests/orgservice/mapping_service_test.go`（覆盖 AC-MAP-01~05：合法导入、非法 studio_id 回滚、手工设置、幂等）
  depends: T019

> ✓ 检查点：映射接口手动测试通过（dry_run 预览 + 确认写入）

### 3.5 RBAC 角色派发与鉴权（AC-ROLE-01~06，AC-SYS-01~03）

**目标**：角色派发/卸任/管辖域鉴权，含 Redis 缓存
**独立验证**：grant 后查询 user_roles 记录存在；越权操作返回 403
**对应 AC**：AC-ROLE-01~06

- [ ] T023 P0 实现 `internal/orgservice/service/role_service.go`：RoleService（Grant、Revoke、CheckScope、GetUserRoles）
  depends: T009
- [ ] T024 P0 在 RoleService 中实现 Redis 缓存（`org:role:<userID>` TTL=60s，Grant/Revoke 后主动 DEL）
  depends: T023
- [ ] T025 P0 实现 `internal/orgservice/handler/role_handler.go`：GrantRole、RevokeRole、GetUserRoles
  depends: T023
- [ ] T026 P0 在路由中注册 `/api/v1/org/roles/grant`、`/api/v1/org/roles/revoke`、`/api/v1/org/roles/:user_id`，以及内部接口 `/internal/org/auth/check-scope`、`/internal/org/tree`
  depends: T025, T018
- [ ] T027 P0 为 RoleService 编写单元测试 `internal/orgservice/service/role_service_test.go`（覆盖 AC-ROLE-01~06：派发、越权 403、卸任回退、幂等、actor_id 记录；mock Redis）
  depends: T023, T024
- [ ] T028 P0 Redis 降级测试：mock Redis 不可用时 CheckScope 回退直查 PG（AC-SYS 相关）
  depends: T024

> ✓ 检查点：角色 API 手动测试通过（grant/revoke/越权 403）

---

## 阶段 4：集成与收尾

- [ ] T029 P0 接入 `internal/controlplane/handler/auth_handler.go`：OAuth 回调末尾调用 `orgSvc.AggregateUser`（含降级逻辑，失败不阻断登录）
  depends: T013, T003
- [ ] T030 P0 扩展 Key 验证响应 `internal/controlplane/service/key_auth_service.go`：VerifyKeyResult 新增 DeptID/StudioID，查询 `users.dept_id` + `departments.studio_id`
  depends: T010, T007
- [ ] T031 P1 实现指标上报：归集成功/失败计数器、未映射部门 Gauge、鉴权延迟 Histogram（参考 `internal/observability/`）
  depends: T013, T023
- [ ] T032 P1 性能验证：鉴权 P95 < 10ms（Redis 命中路径 benchmark）、映射导入 500 条 < 5s（`go test -bench`）
  depends: T024, T019
- [ ] T033 P0 运行全量测试确保不开倒车（`make test`）
  depends: T029, T030
- [ ] T034 P0 Code Review：检查 orgservice package 只被 control-plane router/handler 调用，无反向依赖
  depends: T033
