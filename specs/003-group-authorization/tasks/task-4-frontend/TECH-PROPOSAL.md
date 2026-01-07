# Task 4 技术方案 · 模型组开放模式 + 授权管理 UI

## 1. 现状分析

### 现有文件清单

| 文件 | 现状 | 改造方式 |
|------|------|----------|
| `web/src/types/group.ts` | Group 类型无 `open_mode` 字段 | **扩展**：增加 `open_mode` |
| `web/src/api/groups.ts` | `groupsApi` 对象封装 CRUD | **追加**：新增 4 个 API 方法 |
| `web/src/views/GroupListPage.vue` | 表格含名称/策略/模型数/能力/状态/时间/操作 | **改造**：增加开放模式列 + 授权按钮 |
| `web/src/components/` | 无授权相关组件 | **新建**：`GroupEntitlementDialog.vue` |

### 代码库模式遵循要点

- **API 层**：对象字面量模式 `groupsApi.xxx()`，导入 `http` + `ApiResponse` 类型
- **组件**：`v-model:visible` + `destroy-on-close` + 文件级 `<style scoped>`
- **权限**：`useAuthStore()` → `effectiveRole === 'admin'`
- **标签**：`el-tag` + `type` 属性（success/warning/info/danger）
- **操作按钮**：`el-button text type="primary" size="small"` + `el-popconfirm`

## 2. 设计决策

### Q1：授权管理 UI 用什么形式？

**决策：`el-drawer` 侧滑抽屉**

理由：
- 模型组列表页已有 `GroupEditDialog`（`el-dialog`），再加一个 dialog 会形成嵌套弹窗交互不一致
- `OrgTreePage.vue` 已使用 `el-drawer` 展示成员详情（640px），交互一致
- 授权管理涉及列表（工作室授权 + 部门开通），抽屉宽度充足，可承载 tab + table

### Q2：开放模式切换用什么交互？

**决策：行内 `el-tag` 可点击切换（仅 admin）**

理由：
- 任务要求「管理员可点击切换开放模式」，行内切换最简洁
- 用 `el-popconfirm` 确认操作（与删除操作风格一致）
- 对 studio_admin/dept_admin/user 只展示只读标签

### Q3：新文件还是追加到现有文件？

**决策：最小化新建文件**

| 内容 | 位置 |
|------|------|
| 类型扩展 | 追加到 `web/src/types/group.ts` |
| API 方法 | 追加到 `web/src/api/groups.ts` |
| 授权管理面板 | 新建 `web/src/components/GroupEntitlementDialog.vue` |
| 列表页改造 | 修改 `web/src/views/GroupListPage.vue` |

### Q4：权限分级怎么处理？

**决策：基于 `authStore.effectiveRole` 的分层可见性**

| 角色 | 开放模式 | 授权列 | 授权抽屉内容 |
|------|---------|--------|-------------|
| admin | 点击切换 + 确认 | 「管理授权」按钮 | 全部：工作室 + 部门 |
| studio_admin | 只读标签 | 「查看授权」按钮 | 部门 tab（自己工作室范围） |
| dept_admin | 隐藏列 | 不显示 | 不显示 |
| user | 隐藏列 | 不显示 | 不显示 |

注意：当前 `GroupListPage` 路由 `requiresAdmin: true`，意味着只有 admin 能进入此页面。
但 spec 说 studio_admin/dept_admin 也需要部分可见性 → **不修改路由守卫**（保持 requiresAdmin），
因为路由已限定 admin-only，页面内的角色检查主要是为后续扩展准备。

**简化决策：当前路由只允许 admin，因此所有操作对 admin 可见。后续如需开放给其他角色再调整路由。**

## 3. 实现清单

### 3.1 类型扩展（`types/group.ts`）

```ts
export type OpenMode = 'public' | 'controlled'

// Group 接口追加字段
// open_mode: OpenMode

// 新增
export interface GroupEntitlement {
  id: string
  group_id: number
  scope_type: 'studio' | 'dept'
  scope_id: string
  scope_name: string
  enabled: boolean
  granted_by: string
  granted_by_name?: string
  created_at: string
  updated_at: string
}

export interface GrantEntitlementRequest {
  scope_type: 'studio' | 'dept'
  scope_id: string
}

export interface EntitlementListResponse {
  items: GroupEntitlement[]
}
```

### 3.2 API 扩展（`api/groups.ts`）

在 `groupsApi` 对象追加：

```ts
setOpenMode(groupId: number, openMode: OpenMode) { ... }
listEntitlements(groupId: number, scopeType?: string) { ... }
grantEntitlement(groupId: number, data: GrantEntitlementRequest) { ... }
revokeEntitlement(groupId: number, entitlementId: string) { ... }
```

同时新增辅助 API（用于「添加授权」对话框中的下拉选择）：

```ts
listStudios() { ... }   // GET /api/v1/org/studios
```

### 3.3 GroupListPage.vue 改造

新增表格列：
1. **开放模式列**（宽 100px）：`el-tag` 显示（绿色=公开、橙色=受控），admin 可点击 + `el-popconfirm` 确认切换
2. **操作列扩展**（宽 220px → 原 160px）：追加「授权」按钮（仅 admin），点击打开抽屉

### 3.4 GroupEntitlementDialog.vue（新建）

`el-drawer`（width=640px），包含：
- **Tab 1：工作室授权**（admin）
  - `el-table` 列出已授权工作室
  - 顶部「添加工作室」按钮 → `el-dialog` 弹窗选择工作室
  - 每行「撤销」按钮 → `el-popconfirm`
- **Tab 2：部门开通**（admin/studio_admin）
  - `el-table` 列出已开通部门
  - 顶部「开通部门」按钮 → 选择部门下拉
  - 每行「撤销」按钮 → `el-popconfirm`

## 4. 不做的事

- 不做前端分页（授权列表通常较短，一次拉取）
- 不做授权变更的实时通知（后端已通过 KeyCache 失效实现）
- 不修改路由守卫（保持 admin-only）
- 不加侧边栏入口（授权管理嵌入组列表页操作列）
