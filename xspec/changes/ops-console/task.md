# ops-console 任务清单 — CHG-20260521-002

## 文档信息

| 属性 | 内容 |
| :--- | :--- |
| **所属变更** | CHG-20260521-002 |
| **模块名称** | ops-console |
| **模块类型** | web-frontend (modified) |
| **创建时间** | 2026-05-21 |

---

## 阶段 1：类型与 API 封装

**目标**：前端类型和 API 方法就绪

- [x] T001 [P] P0 TypeScript 类型定义 `web/src/types/sync.ts`（SyncResult, SyncModelItem, ConfirmSyncPayload, ConfirmSyncResult）
- [x] T002 P0 API 方法 `web/src/services/control-plane.ts`（新增 syncModels + confirmSync 方法）
  depends: T001

**✓ 检查点**：TypeScript 编译通过，方法可被组件导入

---

## 阶段 2：组件实现

**目标**：同步弹窗组件完整可交互

- [x] T003 P0 SyncModelsDialog.vue `web/src/components/admin/SyncModelsDialog.vue`（弹窗骨架：标题+统计栏+列表+底部操作）
  depends: T002
- [x] T004 P0 模型列表展示（checkbox + 名称 + 状态标签：新增/已存在/已下架）
  depends: T003
- [x] T005 P0 关键词过滤搜索框（实时过滤列表）
  depends: T004
- [x] T006 P0 一键全选（仅选"新增"状态模型）+ 确认注册按钮
  depends: T004
- [x] T007 P0 确认注册调用 confirmSync API + 成功/失败提示
  depends: T006, T002

**✓ 检查点**：弹窗可打开，列表渲染正确，过滤生效，勾选注册正常

---

## 阶段 3：页面集成

**目标**：渠道列表页集成同步按钮

- [x] T008 P0 ProviderListPage.vue 新增"同步模型"按钮（操作列，禁用渠道置灰）
  depends: T003
- [x] T009 P0 点击按钮调用 syncModels → 打开 SyncModelsDialog → 关闭后刷新
  depends: T007, T008

**✓ 检查点**：渠道列表点击同步→弹窗展示→勾选注册→成功提示

---

## 阶段 4：联调

- [x] T010 P0 前后端联调验证（真实百炼渠道同步 + 勾选注册）
  depends: T009, T004@control-plane

**✓ 检查点**：端到端同步流程通过

---

## 交付策略

| 项目 | 说明 |
| :--- | :--- |
| 总任务数 | 10 |
| P0 | 10 |
| P1 | 0 |
| 预计工时 | 5h |
| 跨模块依赖 | T010 depends: T004@control-plane |
