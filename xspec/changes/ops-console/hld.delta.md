# ops-console 概要设计变更 — CHG-20260609-001

> 基线：xspec/modules/ops-console/hld.md (v1.3)

## 变更摘要

新增两个主功能模块：组织管理（树形视图 + 映射维护 + 角色管理）和分层用量看板（个人/管理两区 Tab + 面包屑下钻），复用现有 Vue 3 + Element Plus + Pinia 技术栈，通过新增路由和组件扩展现有后台导航体系。

---

## 架构变更

```mermaid
flowchart TD
    subgraph OC["ops-console（变更部分）"]
        Nav["侧边栏导航\n（新增「组织管理」「用量看板」菜单项）"]

        subgraph OrgModule["组织管理模块（新增）"]
            OrgTree["OrgTreePage\n组织树视图"]
            MappingPage["MappingPage\n映射维护"]
            RolePage["RolePage\n角色管理"]
        end

        subgraph DashModule["用量看板模块（新增）"]
            UsageTab["UsageDashboardPage\n个人 / 管理 Tab"]
            PersonalDash["个人看板\n（复用现有用量组件）"]
            MgmtDash["管理看板\n面包屑下钻"]
        end
    end

    Nav --> OrgModule
    Nav --> DashModule
    OrgModule -->|REST /api/v1/org/*| CP["control-plane"]
    DashModule -->|REST /api/v1/usage/dashboard| CP
    DashModule -->|GET /usage/me| CP
```

### 新增路由

| 路由 | 页面组件 | 权限 | 说明 |
| :--- | :--- | :--- | :--- |
| `/admin/org` | `OrgTreePage.vue` | admin | 组织树视图 |
| `/admin/org/mapping` | `MappingPage.vue` | admin | 映射维护 |
| `/admin/org/roles` | `RolePage.vue` | admin | 角色管理 |
| `/admin/usage/dashboard` | `UsageDashboardPage.vue` | dept_admin / studio_admin / admin | 分层用量看板 |

---

## 技术选型变更

无新增技术选型，全部复用 Vue 3 + Element Plus + Vite + Pinia。

---

## 新增组件

### 组织管理模块

```mermaid
flowchart TD
    OrgTreePage --> OrgTree["OrgTree.vue\n树形组件（el-tree）"]
    OrgTreePage --> NodeDetail["NodeDetail.vue\n节点详情（成员数/负责人/映射状态）"]

    MappingPage --> MappingTable["MappingTable.vue\n映射表格（含状态 badge）"]
    MappingPage --> ImportDialog["ImportDialog.vue\nCSV 上传 → diff 预览 → 确认"]
    MappingPage --> UnmappedAlert["UnmappedAlert.vue\n未映射部门角标 + 快速入口"]

    RolePage --> RoleList["RoleList.vue\n角色列表表格"]
    RolePage --> GrantDialog["GrantDialog.vue\n指定负责人表单（搜索员工+选角色+选管辖域）"]
```

**核心交互流程（映射导入）**：

```mermaid
sequenceDiagram
    participant Admin as 管理员
    participant MappingPage as MappingPage
    participant ImportDialog as ImportDialog.vue
    participant CP as control-plane

    Admin->>MappingPage: 点击「批量导入」
    MappingPage->>ImportDialog: 打开弹窗
    Admin->>ImportDialog: 上传 CSV 文件
    ImportDialog->>CP: POST /api/v1/org/dept-studio-map/import (dry_run=true)
    CP-->>ImportDialog: diff{新增N条, 变更M条, 跳过K条}
    ImportDialog->>ImportDialog: 渲染 diff 预览表格
    Admin->>ImportDialog: 确认写入
    ImportDialog->>CP: POST /api/v1/org/dept-studio-map/import (confirm=true)
    CP-->>ImportDialog: 200 OK
    ImportDialog->>MappingPage: 关闭, 刷新列表
    MappingPage->>MappingPage: 更新未映射角标数量
```

### 分层用量看板模块

```mermaid
flowchart TD
    UsageDashboardPage --> TabSwitch["el-tabs\n「个人」/「管理」"]
    TabSwitch -->|个人 Tab| PersonalPanel["PersonalPanel.vue\n复用现有用量组件\nGET /usage/me"]
    TabSwitch -->|管理 Tab| MgmtPanel["MgmtPanel.vue\n管理看板"]
    MgmtPanel --> Breadcrumb["el-breadcrumb\n面包屑（公司→工作室→部门→成员）"]
    MgmtPanel --> ScopeCards["ScopeCards.vue\n总量/排行/趋势卡片"]
    ScopeCards -->|"scope=company|studio|dept"| DashAPI["GET /api/v1/usage/dashboard"]
```

**面包屑下钻流程**：

```mermaid
sequenceDiagram
    participant Admin as 管理员(admin)
    participant MgmtPanel as MgmtPanel
    participant CP as control-plane

    Admin->>MgmtPanel: 进入管理看板（默认全公司视图）
    MgmtPanel->>CP: GET /api/v1/usage/dashboard?scope=company
    CP-->>MgmtPanel: 工作室排行列表
    Admin->>MgmtPanel: 点击某工作室
    MgmtPanel->>MgmtPanel: 面包屑追加「工作室A」
    MgmtPanel->>CP: GET /api/v1/usage/dashboard?scope=studio&id=S001
    CP-->>MgmtPanel: 部门排行列表
    Admin->>MgmtPanel: 点击某部门
    MgmtPanel->>MgmtPanel: 面包屑追加「部门B」
    MgmtPanel->>CP: GET /api/v1/usage/dashboard?scope=dept&id=D001
    CP-->>MgmtPanel: 成员排行列表
```

**角色差异化渲染规则**：

| 用户角色 | 个人 Tab | 管理 Tab | 管理看板默认视图 |
| :--- | :--- | :--- | :--- |
| user | ✅ 可见 | ❌ 不渲染 | — |
| dept_admin | ✅ 可见 | ✅ 可见 | 本部门视图（scope=dept） |
| studio_admin | ✅ 可见 | ✅ 可见 | 本工作室视图（scope=studio） |
| admin | ✅ 可见 | ✅ 可见 | 全公司视图（scope=company） |

---

## 状态管理扩展

新增两个 Pinia store：

| Store | 职责 |
| :--- | :--- |
| `useOrgStore` | 缓存组织树数据、未映射部门计数、当前选中节点 |
| `useDashboardStore` | 缓存当前 scope/id 选择、面包屑路径栈、用量数据 |

---

## 宪章合规检查

| 原则 | 检查结果 |
| :--- | :--- |
| 零破坏性变更 | ✅ 新增路由和组件，不修改现有页面逻辑 |
| 服务快速 | ✅ 页面按需加载（Vue Router 懒加载），Dashboard API 分页请求 |
| 交互至上 | ✅ 核心操作（导入映射、指定负责人）不超过 3 步；越权访问有明确提示 |
| 架构清晰 | ✅ 组织管理和看板作为独立功能模块，不与现有渠道/模型管理混杂 |
