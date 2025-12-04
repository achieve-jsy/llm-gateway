# ops-console 数据模型变更 — CHG-20260609-001

> 基线：xspec/modules/ops-console/data-model.md (v1.0)

## 变更摘要

新增组织管理和看板模块所需的前端 TypeScript 接口类型定义、两个 Pinia Store 及其状态结构。

---

## 新增

### 组织管理类型定义（`src/types/org.ts`）

```typescript
export interface Studio {
  id: string
  name: string
  owner_id: string
  dept_count?: number
  created_at: string
}

export interface Department {
  id: string
  name: string
  studio_id: string | null
  studio_name?: string
  status: 'active' | 'pending_studio'
  owner_id: string | null
  member_count?: number
  created_at: string
}

export interface DeptStudioMapping {
  dept_id: string
  dept_name: string
  studio_id: string
  studio_name: string
  source: 'import' | 'manual'
  updated_at: string
}

export interface UserRole {
  role: 'user' | 'dept_admin' | 'studio_admin' | 'admin'
  scope_type: 'self' | 'dept' | 'studio' | 'company'
  scope_id: string
  scope_name?: string
  granted_by: string
  created_at: string
}

export interface ImportDiff {
  added: Array<{ dept_id: string; studio_id: string }>
  updated: Array<{ dept_id: string; old_studio_id: string; new_studio_id: string }>
  unchanged: number
}

export interface ImportResult {
  updated: number
  skipped: number
  backfilled: number
}
```

### 看板类型定义（`src/types/dashboard.ts`）

```typescript
export type DashScope = 'company' | 'studio' | 'dept' | 'user'

export interface DashboardSummary {
  total_tokens: number
  cost_estimate: number
  user_count: number
  call_count: number
}

export interface DashboardBreakdownItem {
  id: string
  name: string
  total_tokens: number
  cost_estimate: number
  user_count: number
  rank: number
}

export interface DashboardResponse {
  scope: DashScope
  id: string
  scope_name: string
  period: { start: string; end: string }
  summary: DashboardSummary
  breakdown: DashboardBreakdownItem[]
  total: number
  page: number
  page_size: number
}

export interface BreadcrumbItem {
  scope: DashScope
  id: string
  name: string
}
```

### useOrgStore（`src/stores/org.ts`）

```typescript
interface OrgState {
  studios: Studio[]
  departments: Department[]
  unmappedDepts: Department[]
  unmappedCount: number
  loading: boolean
  lastFetchAt: number | null
}
```

**Actions**：
- `fetchStudios()` → GET /api/v1/org/studios
- `fetchDepartments(status?)` → GET /api/v1/org/departments
- `fetchUnmapped()` → GET /api/v1/org/departments/unmapped
- `importMappings(file, dryRun)` → POST /api/v1/org/dept-studio-map/import
- `setDeptStudio(deptId, studioId)` → PUT /api/v1/org/departments/:id/studio

### useDashboardStore（`src/stores/dashboard.ts`）

```typescript
interface DashboardState {
  breadcrumbs: BreadcrumbItem[]   // 面包屑栈
  currentData: DashboardResponse | null
  loading: boolean
  period: { start: string; end: string }
  page: number
  pageSize: number
}
```

**Actions**：
- `drillDown(item: BreadcrumbItem)` → 追加面包屑，请求新 scope 数据
- `popToLevel(index: number)` → 截断面包屑到指定层级，重新请求
- `fetchDashboard()` → GET /api/v1/usage/dashboard（按当前 breadcrumbs 最后一项构造参数）
- `setPeriod(start, end)` → 更新时间范围并重新请求

---

## 修改

### AuthStore 新增字段

```typescript
// src/stores/auth.ts（现有 store 追加）
interface AuthState {
  // ...现有字段...
  role: 'user' | 'dept_admin' | 'studio_admin' | 'admin'  // 新增：来自 JWT claims
  dept_id: string   // 新增：来自 JWT claims
  studio_id: string // 新增：来自 JWT claims（pending_studio 时为空）
}
```

JWT 解析时新增读取 `role`、`dept_id`、`studio_id` 三个 claims 字段。

## 不变

现有渠道、模型、Key 相关类型定义不变。
