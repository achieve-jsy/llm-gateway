# 技术方案 · Task 4：前端 — 模型组开放模式 + 授权管理 UI

## 1. 调研结论

| 方面 | 现状 | 影响 |
|------|------|------|
| 模型组列表页 | `web/src/views/GroupListPage.vue`（注意不在 `views/admin/` 下） | 直接改造此文件 |
| API 层 | `web/src/api/groups.ts` 采用 `groupsApi` 对象封装模式 | 沿用对象封装，扩展 entitlement 方法 |
| 类型定义 | `web/src/types/group.ts` 已有 Group 等类型，无 open_mode | 扩展 Group + 新增 GroupEntitlement |
| HTTP 客户端 | `web/src/services/http.ts` 单例 Axios，统一拦截器 | 直接 import http 使用 |
| 组件目录 | `web/src/components/` 有 GroupEditDialog.vue 参考 | 新建 GroupEntitlementDialog.vue |
| 权限体系 | auth store 提供 `isAdmin` / `isManager` / `effectiveRole`，OrgTreePage 用 `isGlobalAdmin` + `myStudioIds` 做细粒度控制 | 授权管理按钮按角色分级显示 |
| 组织架构数据 | `web/src/stores/org.ts` 提供 `fetchStudios` / `fetchDepartments` | 复用，无需新建 store |
| 路由 | `/admin/groups` 已有 `requiresAdmin: true` | 无需修改路由，授权管理入口在页面内按角色控制 |

## 2. 改动清单

### 2.1 类型扩展（`web/src/types/group.ts`）

```ts
// Group 新增字段
open_mode: 'public' | 'controlled'  // 默认 'public'

// 新增类型
interface GroupEntitlement {
  id: string
  group_id: number
  scope_type: 'studio' | 'dept'
  scope_id: string
  scope_name: string
  enabled: boolean
  granted_by: string
  created_at: string
}
```

### 2.2 API 扩展（`web/src/api/groups.ts`）

在 `groupsApi` 对象中新增 4 个方法：

| 方法 | 端点 | 用途 |
|------|------|------|
| `setGroupOpenMode(id, openMode)` | `PUT /api/v1/groups/:id/open-mode` | 切换开放模式 |
| `listEntitlements(id, scopeType?)` | `GET /api/v1/groups/:id/entitlements` | 查询授权列表 |
| `grantEntitlement(id, data)` | `POST /api/v1/groups/:id/entitlements` | 添加授权 |
| `revokeEntitlement(id, entitlementId)` | `DELETE /api/v1/groups/:id/entitlements/:eid` | 撤销授权 |

### 2.3 列表页改造（`web/src/views/GroupListPage.vue`）

- 新增「开放模式」列：`el-tag` 绿色=公开 / 橙色=受控
- 管理员可点击切换开放模式（`el-popconfirm` 确认）
- 新增「授权管理」操作按钮（仅受控组显示，admin 可见）
- 点击打开 `GroupEntitlementDialog`

### 2.4 授权管理组件（`web/src/components/GroupEntitlementDialog.vue`）

**组件定位**：`el-dialog`（非抽屉），嵌入 GroupListPage。

**内容布局**：
- 顶部显示当前模型组名称 + 开放模式标签
- 两个 Tab：「工作室授权」（admin 专属）和「部门开通」（studio_admin / dept_admin）
- 每个 Tab 包含：`el-table` 列表 + 「添加」按钮
- 添加流程：`el-select` 选择工作室/部门 → 确认提交
- 撤销操作：`el-popconfirm` 确认

**权限控制**：
- admin → 两个 Tab 都可见
- studio_admin → 仅「部门开通」Tab，且只能操作自己工作室下已授权工作室的组
- dept_admin → 仅能查看，不能操作（当前阶段）
- user → 不显示授权管理入口

## 3. 文件变更矩阵

| 文件 | 操作 | 行数预估 |
|------|------|---------|
| `web/src/types/group.ts` | 修改 | +15 行 |
| `web/src/api/groups.ts` | 修改 | +30 行 |
| `web/src/views/GroupListPage.vue` | 修改 | +60 行 |
| `web/src/components/GroupEntitlementDialog.vue` | 新建 | ~350 行 |

## 4. 不涉及

- 不新增路由（复用 `/admin/groups`）
- 不新建 store（复用 org store 的工作室/部门数据）
- 不改后端（API 接口已按 spec 定义）
- 不改权限系统（复用现有 `effectiveRole` / `isAdmin`）
