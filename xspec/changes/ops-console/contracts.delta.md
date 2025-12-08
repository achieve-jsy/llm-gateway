# ops-console 接口契约变更 — CHG-20260609-001

> 基线：xspec/modules/ops-console/contracts.md (v1.0)

## 变更摘要

新增组织管理模块和看板模块消费的 API 调用列表，以及对应的请求/响应类型和错误处理约定。

---

## 新增接口调用

### 组织管理 API 消费

| 接口 | 方法 | 路径 | 调用组件 | 说明 |
| :--- | :--- | :--- | :--- | :--- |
| 工作室列表 | GET | /api/v1/org/studios | OrgTreePage, RolePage | 管理页和角色指定下拉 |
| 工作室详情 | GET | /api/v1/org/studios/:id | OrgTreePage | 节点点击详情 |
| 部门列表 | GET | /api/v1/org/departments | MappingPage, RolePage | 映射表格和角色指定下拉 |
| 未映射部门 | GET | /api/v1/org/departments/unmapped | MappingPage | 角标数量 + 快速入口 |
| 映射列表 | GET | /api/v1/org/dept-studio-map | MappingPage | 映射维护表格 |
| 映射导入（预览） | POST | /api/v1/org/dept-studio-map/import?dry_run=true | ImportDialog | diff 预览 |
| 映射导入（写入） | POST | /api/v1/org/dept-studio-map/import | ImportDialog | 确认写入 |
| 手工设置映射 | PUT | /api/v1/org/departments/:id/studio | MappingPage | 行内操作 |
| 派发角色 | POST | /api/v1/org/roles/grant | GrantDialog | 指定负责人 |
| 卸任角色 | POST | /api/v1/org/roles/revoke | RolePage | 卸任操作 |
| 用户角色列表 | GET | /api/v1/org/roles/:user_id | RolePage | 列表展示 |

### 看板 API 消费

| 接口 | 方法 | 路径 | 调用组件 | 说明 |
| :--- | :--- | :--- | :--- | :--- |
| 分层用量看板 | GET | /api/v1/usage/dashboard | MgmtPanel | 管理看板数据 |
| 个人用量 | GET | /usage/me | PersonalPanel | 复用现有，无变更 |

---

## 请求/响应类型

### 映射导入响应（新增）

完整响应格式见 `xspec/changes/org-service/contracts.md` §3.2（与后端对齐）。

前端处理逻辑：
- `dry_run=true` 响应 → 渲染 diff 表格（added/updated/unchanged）
- `dry_run=false` 响应 → 关闭弹窗，刷新映射列表，更新未映射角标

### 角色派发/卸任

请求格式见 `xspec/changes/org-service/contracts.md` §4.1–4.2（与后端对齐）。

### 分层看板响应

```typescript
// 与 control-plane contracts.delta.md §GET /api/v1/usage/dashboard 对齐
// 响应类型见 src/types/dashboard.ts 中 DashboardResponse
```

---

## 错误处理约定

| 错误码 | HTTP | 前端处理 |
| :--- | :--- | :--- |
| `org.forbidden` | 403 | ElMessage.error「无权操作该组织范围」 |
| `org.import.invalid_studio` | 422 | 展示 `invalid_rows` 列表，保持弹窗打开 |
| `org.import.file_too_large` | 413 | ElMessage.error「文件大小超过 1MB 限制」 |
| `org.import.too_many_rows` | 422 | ElMessage.error「导入条数超过 500 条限制」 |
| `org.duplicate_name` | 422 | 表单字段下方展示错误提示 |
| `org_pending_studio` | 403 | （非组织管理接口）统一 403 拦截器处理 |
| 通用 500 | 500 | ElMessage.error「服务器内部错误，请稍后重试」|

---

## 废弃接口

不适用
