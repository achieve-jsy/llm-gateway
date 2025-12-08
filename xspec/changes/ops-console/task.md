# ops-console 任务清单

**变更**：CHG-20260609-001 — 组织架构
**模块类型**：web-frontend（变更）
**变更基线**：v1.3

---

## 阶段 1：环境与基础

> ⚠️ 此阶段完成前不启动后续工作

- [x] T001 [P] P0 新增类型定义文件 `web/src/types/org.ts`（Studio、Department、DeptStudioMapping、UserRole、ImportDiff、ImportResult）
- [x] T002 [P] P0 新增类型定义文件 `web/src/types/dashboard.ts`（DashScope、DashboardSummary、DashboardBreakdownItem、DashboardResponse、BreadcrumbItem）
- [x] T004 [P] P0 新建 Pinia store `web/src/stores/org.ts`：useOrgStore
- [x] T005 [P] P0 新建 Pinia store `web/src/stores/dashboard.ts`：useDashboardStore
- [x] T007 P0 在 `web/src/router/index.ts` 注册新路由并添加角色守卫（AC-OC-07）
- [x] T008 P0 新建 `web/src/views/admin/org/OrgTreePage.vue`（AC-OC-01）
- [x] T011 P0 新建 `web/src/views/admin/org/MappingPage.vue`（AC-OC-02）
- [x] T015 P0 新建 `web/src/views/admin/org/RolePage.vue`（AC-OC-03）
- [x] T018 P1 新建 `web/src/views/admin/usage/UsageDashboardPage.vue`（AC-OC-04~06）
- [ ] T003 P0 扩展 `web/src/stores/auth.ts`：AuthState 新增 `role`、`dept_id`、`studio_id` 字段，在 JWT 解码后写入（来自 claims）
  depends: T001
- [ ] T004 [P] P0 新建 Pinia store `web/src/stores/org.ts`：useOrgStore（studios、departments、unmappedDepts、unmappedCount 状态 + fetchStudios/fetchDepartments/fetchUnmapped/importMappings/setDeptStudio actions）
  depends: T001
- [ ] T005 [P] P0 新建 Pinia store `web/src/stores/dashboard.ts`：useDashboardStore（breadcrumbs、currentData、period 状态 + drillDown/popToLevel/fetchDashboard/setPeriod actions）
  depends: T002
- [ ] T006 P0 扩展侧边栏导航 `web/src/components/layout/Sidebar.vue`（或对应导航组件）：新增「组织管理」入口（admin 可见）和「用量看板」入口（管理角色可见），基于 `authStore.role` 动态渲染
  depends: T003

> ✓ 检查点：TypeScript 编译通过（`npx tsc --noEmit`）；侧边栏按角色正确显示/隐藏

---

## 阶段 2：路由与权限

- [ ] T007 P0 在 `web/src/router/index.ts` 注册新路由：`/admin/org`、`/admin/org/mapping`、`/admin/org/roles`、`/admin/usage/dashboard`；并在路由守卫中添加角色校验（admin 限 org 路由；管理角色限 dashboard）
  depends: T003

> ✓ 检查点：普通员工访问 `/admin/org` 被重定向到 403 页（AC-OC-07）

---

## 阶段 3：功能实现

### 3.1 组织树视图（US-ORG-01，AC-OC-01）

**目标**：管理员可看到公司→工作室→部门树，待映射部门有红色 badge
**独立验证**：admin 登录后访问 `/admin/org`，树节点展示正确，pending_studio 部门有红色 badge
**对应 AC**：AC-OC-01、AC-OC-07

- [ ] T008 P0 新建 `web/src/views/admin/org/OrgTreePage.vue`：左侧 el-tree（工作室/部门节点）+ 右侧 NodeDetail 面板；mounted 时调用 `orgStore.fetchStudios()` + `orgStore.fetchDepartments()`
  depends: T004, T007
- [ ] T009 P0 实现树节点 badge 渲染：`status === 'pending_studio'` 显示红色「待映射」标记
  depends: T008
- [ ] T010 P0 新建 `web/src/components/org/NodeDetail.vue`：展示节点详情（成员数、负责人、映射状态）
  depends: T008

> ✓ 检查点：AC-OC-01 验证通过（待映射部门有红色 badge）；AC-OC-07 验证通过（普通员工访问被拦截）

### 3.2 映射维护（US-ORG-02，AC-OC-02）

**目标**：管理员可批量导入 CSV 映射并预览 diff 后确认写入，未映射角标实时更新
**独立验证**：上传合法 CSV → 展示 diff 预览 → 确认 → 映射列表刷新 + 角标更新
**对应 AC**：AC-OC-02

- [ ] T011 P0 新建 `web/src/views/admin/org/MappingPage.vue`：映射表格（dept_name / studio_name / source / status）+ 批量导入按钮 + 未映射角标
  depends: T004, T007
- [ ] T012 P0 新建 `web/src/components/org/ImportDialog.vue`：el-upload 文件选择 → dry_run=true 请求预览 diff → 表格展示 added/updated/unchanged → 确认按钮写入
  depends: T011
- [ ] T013 P0 实现 ImportDialog 状态机（Idle→Uploading→Previewing→Confirming→Success/Error）
  depends: T012
- [ ] T014 P0 行内手工修改映射：工作室下拉（来自 orgStore.studios）→ 调用 PUT `/api/v1/org/departments/:id/studio`
  depends: T011

> ✓ 检查点：AC-OC-02 端到端验证（dry_run 预览正确，确认后映射列表更新，角标变化）

### 3.3 角色管理（US-ORG-03，AC-OC-03）

**目标**：管理员可指定/卸任工作室/部门负责人
**独立验证**：指定后角色列表出现新记录，管辖域正确
**对应 AC**：AC-OC-03

- [ ] T015 P0 新建 `web/src/views/admin/org/RolePage.vue`：角色列表表格（用户名/邮箱/角色/管辖域/操作）+ 指定负责人按钮
  depends: T004, T007
- [ ] T016 P0 新建 `web/src/components/org/GrantDialog.vue`：员工搜索（防抖 500ms）→ 角色选择（radio）→ 管辖域下拉 → 确认调用 POST `/api/v1/org/roles/grant`
  depends: T015
- [ ] T017 P0 卸任功能：确认弹窗 → 调用 POST `/api/v1/org/roles/revoke` → 刷新列表
  depends: T015

> ✓ 检查点：AC-OC-03 验证通过（指定后列表出现记录，管辖域正确显示）

### 3.4 分层用量看板（US-DASH-01、US-DASH-02，AC-OC-04~06）

**目标**：全员有个人 Tab；管理角色有管理 Tab，支持面包屑下钻
**独立验证**：普通员工只有「个人」Tab；dept_admin 管理 Tab 只显示本部门数据
**对应 AC**：AC-OC-04、AC-OC-05、AC-OC-06

- [ ] T018 P1 新建 `web/src/views/admin/usage/UsageDashboardPage.vue`：顶部 el-tabs（「个人」/「管理」）；「个人」Tab 复用现有用量组件（GET /usage/me）；「管理」Tab 按 `authStore.role` 决定是否渲染（AC-OC-04）
  depends: T005, T003, T007
- [ ] T019 P1 新建 `web/src/components/dashboard/MgmtPanel.vue`：el-breadcrumb + ScopeCards + 排行表格；mounted 时调用 `dashStore.fetchDashboard()`
  depends: T018, T005
- [ ] T020 P1 实现面包屑下钻逻辑：点击排行行 → `dashStore.drillDown({id, name, scope})` → 重新请求；点击面包屑上级 → `dashStore.popToLevel(index)` → 重新请求（AC-OC-06）
  depends: T019
- [ ] T021 P1 实现管辖域强制过滤：dept_admin 进入管理看板时强制 scope=dept；studio_admin 强制 scope=studio（AC-OC-05）
  depends: T018

> ✓ 检查点：AC-OC-04（普通员工无管理 Tab）；AC-OC-05（dept_admin 看板只显示本部门）；AC-OC-06（admin 面包屑下钻正确）

---

## 阶段 4：集成与收尾

- [ ] T022 P0 与后端 `/api/v1/org/*` 接口联调：验证全部 CRUD 场景（依赖 T026@org-service、T009@control-plane 完成）
  depends: T017, T026@org-service, T009@control-plane
- [ ] T023 P1 与 `/api/v1/usage/dashboard` 联调：验证 scope 过滤和面包屑下钻（依赖 T011@control-plane 完成）
  depends: T020, T011@control-plane
- [ ] T024 P1 前端 E2E 测试（Playwright）：org 管理完整流程 + 看板 Tab 切换
  depends: T022, T023
- [ ] T025 P0 运行 `npx vitest run` 确保现有测试不退化
  depends: T022
- [ ] T026 P0 Code Review：路由守卫角色校验完整；无硬编码角色字符串（改用常量）
  depends: T025
