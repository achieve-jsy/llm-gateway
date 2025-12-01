# ops-console 接口契约变更 — CHG-20260603-001

> 基线：xspec/modules/ops-console/spec.md (v1.0)

## 变更摘要

新增前端调用 2 个后端 API（用量查询 + 调用内容获取）；修改模型创建/更新 API 调用（新增 price_mode 字段）；删除 Catalog 价格差异 API 调用。

## 新增 API 调用

### GET /v1/usage — 用量查询

**调用方**：UsagePage.vue、GlobalUsagePage.vue

**请求**：

```typescript
// web/src/api/usage.ts
export function getUsage(params: UsageQueryParams): Promise<UsageResponse> {
  return request({
    url: '/v1/usage',
    method: 'get',
    params: {
      from: params.from,        // YYYY-MM-DD, 必填
      to: params.to,            // YYYY-MM-DD, 必填
      model: params.model,      // 可选
      provider_type: params.provider_type,  // 可选
      is_stream: params.is_stream,          // 可选
      group_by: params.group_by || 'model',  // 默认 model
    }
  })
}
```

**请求示例**：

```http
GET /v1/usage?from=2026-06-01&to=2026-06-04&group_by=model
Authorization: Bearer {jwt_token}
```

**响应类型**：`UsageResponse`（见 data-model.delta.md §新增类型定义）

**错误处理**：

| HTTP 状态 | 处理方式 |
| :--- | :--- |
| 200 | 正常渲染 |
| 400 | `el-alert type="error"` + 错误信息："日期范围不合法" |
| 401 | 触发全局 JWT 过期处理（跳转登录） |
| 500 | `el-alert type="error"` + "服务异常，请稍后重试" + 重试按钮 |

### GET /v1/usage/payload/:request_id — 调用内容获取

**调用方**：UsagePage.vue（模型拆分表格行点击展开）

**请求**：

```typescript
// web/src/api/usage.ts
export function getPayload(requestId: string): Promise<PayloadData> {
  return request({
    url: `/v1/usage/payload/${requestId}`,
    method: 'get'
  })
}
```

**响应类型**：`PayloadData`（见 data-model.delta.md §新增类型定义）

**错误处理**：

| HTTP 状态 | 处理方式 |
| :--- | :--- |
| 200 | 弹窗展示调用内容 |
| 403 | `ElMessage.warning('无权查看该调用内容')` |
| 404 | `ElMessage.warning('调用记录不存在')` |

## 修改 API 调用

### POST /api/v1/models — 创建模型

**请求体新增字段**：

```typescript
interface CreateModelRequest {
  // ... 已有字段
  price_mode?: 'catalog' | 'manual'   // 默认 'catalog'
  reasoning_price?: number | null      // manual 模式可选
  cached_price?: number | null         // manual 模式可选
}
```

### PUT /api/v1/models/:id — 更新模型

**请求体新增字段**：同 POST

**前端校验**（提交前）：

```typescript
function validateModelForm(form: ModelFormData): boolean {
  if (form.price_mode === 'manual') {
    if (!form.input_price || form.input_price <= 0) {
      ElMessage.error('手动模式下 Prompt 单价必须大于 0')
      return false
    }
    if (!form.output_price || form.output_price <= 0) {
      ElMessage.error('手动模式下 Completion 单价必须大于 0')
      return false
    }
  }
  return true
}
```

## 删除 API 调用

| API | 调用位置 | 替代 |
| :--- | :--- | :--- |
| GET /api/v1/model-catalog/diff | CatalogPage.vue (PriceDiffPanel) | 无（功能移除） |
| POST /api/v1/model-catalog/apply-diff | CatalogPage.vue (ApplyDiffButton) | 无（功能移除） |

## 三态处理规范

所有新增页面必须实现以下三种状态：

| 状态 | 组件 | 触发条件 | 实现 |
| :--- | :--- | :--- | :--- |
| **loading** | `el-skeleton` | `loading === true` | UsagePage: 骨架屏模拟 3 卡片 + 表格 + 2 图表。GlobalUsagePage: 骨架屏模拟 3 卡片 + 2 图表 + 趋势图 |
| **empty** | `el-empty` | `loading === false && items.length === 0` | UsagePage: `description="暂无用量数据"` + `:image-size="120"`。GlobalUsagePage: 同上 |
| **error** | `el-alert` | `error !== null` | `type="error"` + `show-icon` + `:title="error"` + 操作区重试按钮 |
| **normal** | 数据渲染 | `loading === false && items.length > 0 && error === null` | 各组件正常渲染 |

## 性能约束

| 指标 | 目标 | 实现方式 |
| :--- | :--- | :--- |
| 首屏渲染 | < 1s | 骨架屏 + 并行请求 |
| 图表渲染 | < 500ms | ECharts 增量更新 |
| 日期切换 | < 300ms | 带 loading 的防抖查询 |

## 标准错误格式

所有 API 错误响应遵循统一格式：

```typescript
interface ApiError {
  error_code: string      // 机器可读错误码，如 'INVALID_DATE_RANGE'
  message: string         // 人类可读描述
  request_id: string      // 请求追踪 ID
}
```

**前端处理策略**：

```typescript
function handleApiError(error: AxiosError<ApiError>) {
  const data = error.response?.data
  const status = error.response?.status

  switch (status) {
    case 400:
      usageStore.error = data?.message || '请求参数不合法'
      break
    case 401:
      // 全局拦截器已处理：跳转登录
      break
    case 403:
      ElMessage.warning(data?.message || '无权访问')
      break
    case 404:
      ElMessage.warning(data?.message || '资源不存在')
      break
    case 500:
    default:
      usageStore.error = '服务异常，请稍后重试'
      break
  }
}
```

## 路由守卫契约

### 管理员页面访问控制

```typescript
// GlobalUsagePage 路由守卫逻辑
router.beforeEach((to, _from, next) => {
  // 1. 公开页面放行
  if (to.meta.public) { next(); return }

  // 2. 未登录跳转登录
  const authStore = useAuthStore()
  if (!authStore.isLoggedIn) { next('/login'); return }

  // 3. 角色检查（新增）
  if (to.meta.roles) {
    const roles = to.meta.roles as string[]
    if (!roles.includes(authStore.role || '')) {
      ElMessage.warning('仅管理员可访问该页面')
      next('/admin/usage')  // 降级到个人用量页
      return
    }
  }

  next()
})
```

**守卫触发条件**：

| 路由 | meta.roles | 非管理员行为 |
| :--- | :--- | :--- |
| `/admin/usage` | 无 | 正常访问 |
| `/admin/usage/global` | `['admin']` | 重定向到 `/admin/usage` + 提示 |
