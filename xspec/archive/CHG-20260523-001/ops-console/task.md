# ops-console 前端任务清单

## 文档信息

| 属性 | 内容 |
| :--- | :--- |
| **所属变更** | CHG-20260523-001 |
| **模块名称** | ops-console |
| **模块类型** | web-frontend |
| **创建时间** | 2026-05-23 |

---

## 阶段 1：环境与基础

**目标**：员工端认证状态、路由、导航和 HTTP 拦截器就绪

- [x] T001 P0 认证状态改造：把现有 dev-token/cookie 逻辑升级为 JWT Bearer 会话模型 `web/src/stores/auth.ts`, `web/src/types/auth.ts`
  depends: T020@auth
- [x] T002 [P] P0 新增认证 API 与 HTTP 拦截器：实现 callback、refresh、logout 与 Bearer 注入 `web/src/services/auth.ts`, `web/src/services/http.ts`
  depends: T001, T024@auth
- [x] T003 P0 路由与守卫改造：新增 `/auth/callback`、`/home`、`/keys`、`/models` 和根路由落点 `web/src/router/index.ts`, `web/src/main.ts`
  depends: T001, T002
- [x] T004 [P] P1 双角色壳层与导航改造：employee/admin 菜单隔离并保留管理员菜单 `web/src/App.vue`, `web/src/stores/auth.ts`
  depends: T001, T003

**✓ 检查点**：未登录/employee/admin 三态导航和路由守卫生效

---

## 阶段 2：数据层

**目标**：员工端 Key / 模型 / 登录态 TypeScript 类型与服务映射就绪

- [x] T010 [P] P0 新增认证类型与持久化结构 `web/src/types/auth.ts`, `web/src/stores/auth.ts`
- [x] T011 [P] P0 新增 Key 类型、分页响应和映射函数 `web/src/types/api-key.ts`, `web/src/services/control-plane.ts`
- [x] T012 [P] P0 新增 `/v1/me/models` 响应类型与映射函数 `web/src/types/model.ts`, `web/src/services/gateway-api.ts`
- [x] T013 P1 收口 auth/key/model 错误文案与状态码归一 `web/src/services/http.ts`, `web/src/services/auth.ts`, `web/src/services/control-plane.ts`, `web/src/services/gateway-api.ts`
  depends: T002, T011, T012

**✓ 检查点**：类型定义完整，服务层可直接消费后端契约

---

## 阶段 3：功能实现

### US-LOGIN-01 — SSO 登录回调与登录态建立 (P0)

**目标**：员工能通过 SSO 完成回调换 token 并进入首页
**独立验证**：访问 `/auth/callback?code=...&state=...` 成功后跳转 `/home`，登录态写入 store
**对应验收标准**：AC-LOGIN-01

- [x] T020 P0 实现登录入口页与 SSO 跳转 `web/src/views/auth/LoginPage.vue`, `web/src/router/index.ts`, `web/src/stores/auth.ts`
  depends: T003, T020@auth
- [x] T021 P0 实现 OIDC 回调页：解析 code/state，调用 callback，写入 store 并跳转首页 `web/src/views/auth/OAuthCallbackPage.vue`, `web/src/services/auth.ts`, `web/src/stores/auth.ts`
  depends: T002, T003, T024@auth

**✓ 检查点**：登录闭环可演示，对应 AC-LOGIN-01

### US-HOME-01 — 首页欢迎与使用文档 (P0)

**目标**：员工登录后可看到欢迎信息、curl 示例与 SDK 示例
**独立验证**：访问 `/home`，可看到姓名/部门/文档入口
**对应验收标准**：AC-HOME-01

- [x] T022 [P] P0 实现员工首页与欢迎信息、文档区块 `web/src/views/home/HomePage.vue`, `web/src/router/index.ts`
  depends: T003, T021

**✓ 检查点**：首页信息与使用文档可独立展示

### US-KEY-PAGE-01 — 我的 Key 页面 (P0)

**目标**：员工可查看、创建、禁用、启用、删除自己的 Key
**独立验证**：访问 `/keys`，完成 Key 列表/创建/状态切换/删除
**对应验收标准**：AC-KEY-PAGE-01, AC-KEY-PAGE-02, AC-KEY-PAGE-03

- [x] T023 [P] P0 实现 Key 列表页基础能力：列表加载、脱敏值、空态、失败态、刷新 `web/src/views/key/KeyListPage.vue`, `web/src/services/control-plane.ts`, `web/src/types/api-key.ts`
- [x] T024 P0 实现创建 Key 弹窗：名称校验、成功后一次性明文展示、复制到剪贴板、关闭清空 `web/src/components/key/KeyCreateDialog.vue`, `web/src/views/key/KeyListPage.vue`, `web/src/services/control-plane.ts`
- [x] T025 [P] P0 实现 Key 禁用/启用/删除操作与确认交互 `web/src/views/key/KeyListPage.vue`, `web/src/services/control-plane.ts`
  depends: T023, T025@control-plane

**✓ 检查点**：Key 自助管理完整可演示，对应 AC-KEY-PAGE-01/02/03

### US-MODEL-PAGE-01 — 当前身份可用模型页 (P0)

**目标**：员工可查看当前身份可用模型列表
**独立验证**：访问 `/models`，展示 `/v1/me/models` 返回结果，空列表时展示空态
**对应验收标准**：AC-MODEL-PAGE-01

- [x] T026 [P] P0 实现员工模型页：展示名称、类型、状态、空态、错误态和刷新 `web/src/views/model/ModelListPage.vue`, `web/src/services/gateway-api.ts`, `web/src/types/model.ts`
  depends: T012, T003, T025@gateway-api

**✓ 检查点**：模型列表页可独立验证，对应 AC-MODEL-PAGE-01

---

## 阶段 4：集成与收尾

**目标**：员工端页面、导航、联调和测试闭环完成

- [ ] T040 P0 员工端整体集成：导航、默认落地页、页面可达性、employee/admin 隔离 `web/src/App.vue`, `web/src/router/index.ts`, `web/src/views/home/HomePage.vue`, `web/src/views/key/KeyListPage.vue`, `web/src/views/model/ModelListPage.vue`
  depends: T004, T021, T022, T023, T025, T026
- [ ] T041 [P] P1 补齐前端测试：auth store、HTTP 拦截器、回调页、Key 页、创建弹窗、首页、模型页 `web/src/stores/auth.spec.ts`, `web/src/services/http.test.ts`, `web/src/views/auth/OAuthCallbackPage.spec.ts`, `web/src/views/key/KeyListPage.spec.ts`, `web/src/components/key/KeyCreateDialog.spec.ts`, `web/src/views/home/HomePage.spec.ts`, `web/src/views/model/ModelListPage.spec.ts`
  depends: T021, T022, T024, T025, T026
- [ ] T042 [P] P1 文案、响应式与回归收尾 `web/src/App.vue`, `web/src/views/auth/LoginPage.vue`, `web/src/views/auth/OAuthCallbackPage.vue`, `web/src/views/home/HomePage.vue`, `web/src/views/key/KeyListPage.vue`, `web/src/components/key/KeyCreateDialog.vue`, `web/src/views/model/ModelListPage.vue`
  depends: T040

**✓ 检查点**：员工端页面与管理员端共存，联调和回归完成

---

## 任务统计

| 指标 | 数值 |
| :--- | :--- |
| 总任务数 | 15 |
| P0（必须） | 12 |
| P1（重要） | 3 |
| P2（可选） | 0 |
| 可并行任务 | 9 |
| 预计工时 | 18h |
