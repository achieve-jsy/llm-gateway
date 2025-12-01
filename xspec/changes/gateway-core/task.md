# gateway-core 任务清单

**变更**：CHG-20260609-001 — 组织架构
**模块类型**：web-backend（变更）
**变更基线**：v1.0

---

## 阶段 1：环境与基础

> ⚠️ 依赖 control-plane verify-key 接口扩展完成后联调

- [x] T001 P0 定义 context key 常量 `internal/gateway/middleware/org_check.go`（新建文件）：`CtxKeyDeptID = "dept_id"`、`CtxKeyStudioID = "studio_id"`
- [x] T005 P0 在 `internal/gateway/middleware/org_check.go` 中实现 `OrgCheckMiddleware()` gin.HandlerFunc：判断 `dept_id != "" && studio_id == ""` 则 `c.AbortWithStatusJSON(403, ...)` 返回结构化 `org_pending_studio` 错误
- [x] T006 P0 修改 `internal/gateway/server/router.go`：在 `/v1/*` 路由组的 `authMiddleware.Handle()` 之后追加 `middleware.OrgCheckMiddleware()`

> ✓ 检查点：文件编译通过（`go build ./internal/gateway/...`）

---

## 阶段 2：数据层（结构体扩展）

- [x] T002 P0 修改 `internal/gateway/infra/auth_client.go`：VerifyKeyResponse struct 新增 `DeptID string json:"dept_id"` 和 `StudioID string json:"studio_id"` 字段（AC-GW-N2 相关）

> ✓ 检查点：编译通过，现有 auth_client 测试仍全绿

---

## 阶段 3：功能实现

### 3.1 OrgContext 注入（AC-GW-N3）

**目标**：鉴权通过后将 dept_id/studio_id 注入 Gin context 供下游使用
**独立验证**：鉴权通过的请求中 `c.GetString("org_dept_id")` 返回正确值
**对应 AC**：AC-GW-N3

- [ ] T003 P0 修改 `internal/gateway/middleware/auth.go`：在 VerifyKey 通过后追加 `c.Set(middleware.CtxKeyDeptID, resp.DeptID)` 和 `c.Set(middleware.CtxKeyStudioID, resp.StudioID)`
  depends: T002, T001
- [ ] T004 P0 为 auth middleware 编写单元测试（覆盖 AC-GW-N3：context 中 dept_id/studio_id 被正确注入；mock verify-key 响应）
  depends: T003

> ✓ 检查点：单元测试通过

### 3.2 PendingStudio 拦截中间件（AC-GW-N1、AC-GW-N2）

**目标**：pending_studio 员工调用模型时返回 403 + 明确提示
**独立验证**：`dept_id` 非空且 `studio_id` 为空时调用 `/v1/chat/completions` 返回 403 + `org_pending_studio`
**对应 AC**：AC-GW-N1、AC-GW-N2

- [ ] T005 P0 在 `internal/gateway/middleware/org_check.go` 中实现 `OrgCheckMiddleware()` gin.HandlerFunc：判断 `dept_id != "" && studio_id == ""` 则 `c.AbortWithStatusJSON(403, ...)` 返回结构化 `org_pending_studio` 错误
  depends: T003, T001
- [ ] T006 P0 修改 `internal/gateway/server/router.go`：在 `/v1/*` 路由组的 `authMiddleware.Handle()` 之后追加 `middleware.OrgCheckMiddleware()`
  depends: T005
- [ ] T007 P0 为 OrgCheckMiddleware 编写单元测试 `internal/gateway/middleware/org_check_test.go`（覆盖 AC-GW-N1：pending 时 403 含正确 code；AC-GW-N2：studio_id 非空时正常通过；dept_id 为空时不触发拦截）
  depends: T005

> ✓ 检查点：单元测试全绿；`pending_studio` 场景返回 `{"error":{"code":"org_pending_studio",...}}`

---

## 阶段 4：集成与收尾

- [ ] T008 P0 联调验证：配合 control-plane verify-key 扩展，端到端测试 pending_studio 拦截（需 T007@control-plane 完成）
  depends: T006, T007@control-plane
- [ ] T009 P0 运行全量测试（`make test`），确保现有鉴权流程、热加载、流式转发均不受影响
  depends: T008
- [ ] T010 P0 Code Review：确认 OrgCheckMiddleware 在路由链中位置正确（AuthMiddleware 之后、Scheduler 之前），不影响 P99 延迟（无 IO）
  depends: T009
