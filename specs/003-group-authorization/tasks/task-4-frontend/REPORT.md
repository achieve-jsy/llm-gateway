# Task 4 实施报告 · 前端 — 模型组开放模式 + 授权管理 UI

## 实施概要

基于 spec.md §11 接口契约与 TASK.md 要求，完成了模型组列表页的开放模式展示/切换和授权管理抽屉面板。

## 变更清单

| 文件 | 操作 | 行数变更 | 说明 |
|------|------|---------|------|
| `web/src/types/group.ts` | 修改 | +28 行 | 新增 `OpenMode`、`GroupEntitlement`、`GrantEntitlementRequest`、`EntitlementListResponse` 类型；`Group` 增加 `open_mode` 字段 |
| `web/src/api/groups.ts` | 修改 | +22 行 | 扩展 `groupsApi` 新增 `setOpenMode`、`listEntitlements`、`grantEntitlement`、`revokeEntitlement` 四个方法 |
| `web/src/views/GroupListPage.vue` | 修改 | +75 行 | 新增「开放模式」列（可点击切换）；操作列加「授权」按钮（受控组可见）；集成 `GroupEntitlementDialog` |
| `web/src/components/GroupEntitlementDialog.vue` | 新建 | 359 行 | 抽屉面板，含「工作室授权」+「部门开通」两个 Tab，支持增删操作 |

## 功能实现

### 1. 模型组列表 — 开放模式列

- 位置：策略列右侧，宽度 110px
- 样式：`el-tag` 绿色 = 公开（success）/ 橙色 = 受控（warning）
- 交互：admin 点击标签触发 `el-popconfirm` 确认切换 → 调用 `PUT /api/v1/groups/:id/open-mode`
- 权限：仅 `authStore.isAdmin` 可操作，其他角色只读标签

### 2. 模型组列表 — 授权按钮

- 位置：操作列中「编辑」之后、「删除」之前
- 显示条件：`canManageEntitlements && row.open_mode === 'controlled'`
- 点击打开 `GroupEntitlementDialog` 抽屉

### 3. GroupEntitlementDialog 抽屉面板

**结构**：
- `el-drawer`（680px 宽，右侧滑入，`destroy-on-close`）
- 双 Tab：工作室授权 / 部门开通
- 嵌套 `el-dialog` 两个添加弹窗（工作室选择 / 部门选择）

**工作室授权 Tab**：
- `el-table` 展示已授权工作室列表（名称、授权人、时间、操作）
- 添加按钮 → 下拉选择工作室（自动过滤已授权项）
- 撤销按钮带 `el-popconfirm` 二次确认（提示"撤销后下属部门将同时失效"）

**部门开通 Tab**：
- 顶部 `el-select` 按工作室过滤（选择已授权的工作室列表）
- `el-table` 展示已开通部门（名称、所属工作室、授权人、时间、操作）
- 添加按钮 → 下拉选择部门（自动过滤已开通项 + 仅显示已授权工作室下的部门）
- 选择部门时显示所属工作室提示

**数据加载**：
- 抽屉打开时并行加载 3 个接口（`listEntitlements` + `org/studios` + `org/departments`）
- watch `visible` 变化触发加载，关闭时清空数据

### 4. 权限控制

- 路由 `/admin/groups` 已有 `meta: { requiresAdmin: true }` → 仅 admin 可访问
- 页面内 `canManageEntitlements = computed(() => authStore.isAdmin)` 控制开放模式切换和授权按钮可见性

## 设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 抽屉 vs 对话框 | `el-drawer`（680px） | 匹配 `OrgTreePage.vue` 模式；空间充足承载双 Tab |
| API 位置 | 追加到 `groupsApi` 对象 | 与现有 `types/group.ts` + `api/groups.ts` 架构一致 |
| 类型组织 | 扩展 `types/group.ts` | 避免新建类型文件，保持 group 相关类型集中 |
| 部门列表来源 | 复用 `GET /api/v1/org/departments` | 利用已有组织架构 API，无需新建接口 |

## 验证结果

- **LSP 诊断**：4 个文件均 0 error
- **类型安全**：未使用 `as any` 或 `@ts-ignore`
- **样式**：scoped CSS，使用项目 CSS 变量（`--el-text-color-secondary` 等）

## 待联调验证

- [ ] 控制面 Task-2 API 实现后联调：`PUT /api/v1/groups/:id/open-mode`、entitlement CRUD
- [ ] 验证 `open_mode` 字段在后端列表接口中的返回
- [ ] 验证授权撤销后级联失效（studio disabled → dept 不可用）
- [ ] 端到端：设 controlled → 授权工作室 → 开通部门 → 验证 /v1/models 可见列表
