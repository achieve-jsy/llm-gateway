# control-plane 任务清单

**变更**：CHG-20260609-001 — 组织架构
**模块类型**：web-backend（变更）
**变更基线**：v1.0

---

## 阶段 1：环境与基础

> ⚠️ 此阶段完成前不启动后续工作（依赖 org-service package 就绪）

- [x] T001 P0 确认 orgservice package 已注入到 control-plane 初始化上下文（`cmd/control-plane/main.go`：DB、Redis 连接传入 orgservice）
- [x] T009 P0 在 `internal/controlplane/router/router.go` 注册完整 org 路由组（JWTAuth + admin/studio_admin 权限），包括所有 `/api/v1/org/*` 和 `/internal/org/*` 路径

> ✓ 检查点：control-plane 编译通过（`go build ./cmd/control-plane/...`）

---

## 阶段 2：数据层

- [ ] T002 P0 确认 migration 文件已就绪并在 staging 环境执行（`migrations/NNN_add_org_tables.sql`），验证 users.dept_id 字段存在
  depends: T004@org-service, T005@org-service

---

## 阶段 3：功能实现

### 3.1 SSO 归集钩子（AC-CP-N1、AC-CP-N2）

**目标**：OAuth 回调完成后自动触发员工归集
**独立验证**：员工登录后 JWT claims 含 dept_id，数据库 users.dept_id 已更新
**对应 AC**：AC-CP-N1、AC-CP-N2

- [ ] T003 P0 修改 `internal/controlplane/handler/auth_handler.go`：在 JWT 签发前插入 `orgSvc.AggregateUser()` 调用，降级处理（失败不阻断登录，dept_id/studio_id 为空）
  depends: T013@org-service, T001
- [ ] T004 P0 修改 JWT claims 写入逻辑：在 token payload 中添加 `dept_id`、`studio_id` 字段（来自 AggregateResult）
  depends: T003
- [ ] T005 P0 为归集钩子编写集成测试 `tests/controlplane/auth_aggregate_test.go`（覆盖 AC-CP-N1：回调后 JWT 含 dept_id；AC-CP-N2：org-service 不可用时登录仍成功）
  depends: T003, T004

> ✓ 检查点：测试账号登录后 JWT 解码可见 dept_id 字段

### 3.2 verify-key 响应扩展（AC-CP-M1）

**目标**：内部 verify-key 接口返回组织身份信息供 gateway-core 使用
**独立验证**：gateway-core 调用 verify-key 后响应 JSON 含 dept_id/studio_id
**对应 AC**：AC-CP-M1

- [ ] T006 P0 修改 `internal/controlplane/service/key_auth_service.go`：VerifyKeyResult struct 新增 DeptID/StudioID；VerifyKey 方法中查询 users.dept_id 和关联的 departments.studio_id
  depends: T030@org-service, T001
- [ ] T007 P0 修改 `internal/controlplane/handler/auth_handler.go`（内部 verify-key handler）：响应体序列化包含 dept_id/studio_id
  depends: T006
- [ ] T008 P0 为 verify-key 扩展编写单元测试（覆盖 AC-CP-M1：响应含两个新字段；pending_studio 时 studio_id 为空字符串）
  depends: T007

> ✓ 检查点：`curl /internal/v1/auth/verify-key` 响应 JSON 含 dept_id/studio_id

### 3.3 组织管理 API 路由（AC-CP-N1 相关）

**目标**：control-plane 路由层挂载 orgservice handler，对外暴露 /api/v1/org/* 接口
**对应 AC**：AC-OC-01~07（前端消费）

- [ ] T009 P0 在 `internal/controlplane/router/router.go` 注册完整 org 路由组（JWTAuth + admin/studio_admin 权限），包括所有 `/api/v1/org/*` 和 `/internal/org/*` 路径
  depends: T026@org-service, T001

> ✓ 检查点：`GET /api/v1/org/studios` 返回 200（admin token）；403（普通 token）

### 3.4 分层用量 Dashboard API（AC-CP-N3、AC-CP-N4）

**目标**：按角色管辖域聚合用量数据
**独立验证**：dept_admin token 调用 dashboard 只返回本部门数据；user token 返回 403
**对应 AC**：AC-CP-N3、AC-CP-N4

- [ ] T010 P1 新增 `internal/controlplane/handler/usage_dashboard_handler.go`：GetDashboard（从 JWT 读 role/scope，注入过滤条件，查 usage_daily JOIN dept_studio_map）
  depends: T009, T001
- [ ] T011 P1 在路由中注册 `GET /api/v1/usage/dashboard`（JWTAuth，user 角色直接 403）
  depends: T010
- [ ] T012 P1 为 Dashboard 编写集成测试（覆盖 AC-CP-N3：dept_admin 只见本部门；AC-CP-N4：user 返回 403；admin 可见全局）
  depends: T010, T011

> ✓ 检查点：用不同角色 token 请求 dashboard，scope 过滤符合预期

---

## 阶段 4：集成与收尾

- [ ] T013 P0 运行全量测试（`make test`）确保不开倒车，特别验证现有 SSO 登录流程仍正常
  depends: T005, T008, T012
- [ ] T014 P0 Code Review：verify-key 响应扩展向后兼容（新字段不影响现有 gateway-core 行为）
  depends: T013
