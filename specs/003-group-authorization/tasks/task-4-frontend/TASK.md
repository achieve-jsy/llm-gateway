# Task 4: 前端 — 模型组开放模式 + 授权管理 UI

## 背景

模型组列表页面（`/admin/groups`）需要：
1. 展示每个组的 `open_mode`（公开/受控标签）
2. 平台管理员可设置组的开放模式
3. 平台管理员可授权/撤销工作室
4. 工作室/部门负责人可下放/开通/撤销部门

本任务依赖 Task-2 的 API 已定义（接口契约已明确），可并行开发。

## 需要完成的功能

### 1. 模型组列表页加 open_mode 标签

文件：`web/src/views/admin/GroupListPage.vue`（或对应模型组管理页面）

- 每个组旁显示标签：`公开`（绿色）/ `受控`（橙色）
- 管理员可点击切换开放模式（调用 `PUT /api/v1/groups/:id/open-mode`）

### 2. 授权管理面板

新增或在组详情中嵌入「授权管理」Tab/抽屉：

**工作室授权（仅 admin 可见）**：
- 列出已授权的工作室（`GET /api/v1/groups/:id/entitlements?scope_type=studio`）
- 可添加工作室（下拉选择工作室列表 → `POST /api/v1/groups/:id/entitlements`）
- 可撤销工作室（`DELETE /api/v1/groups/:id/entitlements/:eid`）

**部门开通（studio_admin / dept_admin 可见）**：
- 列出已开通的部门
- 可添加部门（在已下放工作室范围内选择部门）
- 可撤销

### 3. API 集成

新文件或扩展：`web/src/api/groups.ts`

```ts
// 设置开放模式
export function setGroupOpenMode(groupId: number, openMode: 'public' | 'controlled') {
  return http.put(`/api/v1/groups/${groupId}/open-mode`, { open_mode: openMode })
}

// 授权/开通
export function grantEntitlement(groupId: number, scopeType: 'studio' | 'dept', scopeId: string) {
  return http.post(`/api/v1/groups/${groupId}/entitlements`, { scope_type: scopeType, scope_id: scopeId })
}

// 撤销
export function revokeEntitlement(groupId: number, entitlementId: string) {
  return http.delete(`/api/v1/groups/${groupId}/entitlements/${entitlementId}`)
}

// 查询授权列表
export function listEntitlements(groupId: number, scopeType?: string) {
  return http.get(`/api/v1/groups/${groupId}/entitlements`, { params: { scope_type: scopeType } })
}
```

### 4. 类型定义

扩展 `web/src/types/` 或在对应文件中：

```ts
interface GroupEntitlement {
  id: string
  scope_type: 'studio' | 'dept'
  scope_id: string
  scope_name: string
  enabled: boolean
  granted_by: string
  created_at: string
}

// Group 类型扩展
interface Group {
  // ... 现有字段
  open_mode: 'public' | 'controlled'
}
```

### 5. 权限控制

- 根据当前用户角色（从 auth store 获取），控制按钮可见性：
  - admin → 可见所有操作
  - studio_admin → 只能看/操作自己工作室下的部门
  - dept_admin → 只能看/操作自己部门
  - user → 不显示授权管理入口

## UI 设计参考

- 开放模式标签：使用 Element Plus `<el-tag>`，绿色=公开，橙色=受控
- 授权列表：使用 `<el-table>`，列：域类型、名称、授权人、时间、操作
- 添加授权：使用 `<el-dialog>` 弹窗 + `<el-select>` 选择工作室/部门
- 撤销确认：使用 `<el-popconfirm>`

## 验证

- 页面正常渲染无报错
- 切换开放模式 API 调用成功
- 添加/撤销授权 API 调用成功
- 权限控制：普通用户看不到管理入口

## 关键约定

- 遵循项目现有 Vue3 + Element Plus + Composition API 风格
- 参考 `web/src/views/admin/` 下现有页面的代码结构
- 遵循现有 `web/src/api/` 下的 HTTP 请求封装模式
- 中文 UI

## 相关文件

- `web/src/views/admin/` — 管理面页面参考
- `web/src/api/groups.ts` — 现有 group API
- `web/src/stores/auth.ts` — 用户角色信息
- `specs/003-group-authorization/spec.md` §11 — 接口契约
