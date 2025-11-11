# ops-console 接口契约

## 1. 概述

| 属性 | 内容 |
| :--- | :--- |
| **所属变更** | CHG-20260521-001 |
| **模块名称** | ops-console |
| **模块类型** | web-frontend |
| **创建时间** | 2026-05-21 |
| **修改人** | OpenCode |

### 修订记录

| 版本号 | 修改日期 | 修改内容摘要 | 修改人 |
| :--- | :--- | :--- | :--- |
| v1.0 | 2026-05-21 | 初稿创建 | OpenCode |
| v1.1 | 2026-05-23 | 新增 sync-models / sync-models/confirm 接口消费契约（CHG-20260521-002） | OpenCode |
| v1.2.0 | 2026-05-25 | 新增 Key CRUD 接口消费契约与 /v1/me/models 接口消费契约 — CHG-20260523-001 | OpenCode |
| v1.3.0 | 2026-05-26 | 新增模型组 9 个 API 消费端点 — CHG-20260525-001 | OpenCode |

本文档定义 ops-console 模块作为前端消费方所依赖的外部接口契约。本模块仅消费接口，不生产接口。

**接口关系总览**：

| 角色 | 模块 | 说明 |
| :--- | :--- | :--- |
| 消费方 | ops-console | 前端页面，调用 gateway-api 获取模型列表 |
| 生产方 | gateway-api | 后端网关 API，提供 OpenAI 兼容的模型列表接口 |

### 公共约定

| 约定项 | 内容 |
| :--- | :--- |
| Base URL | 同域部署，前端请求路径为 `/v1/models`（由 Vite 代理或 Nginx 反向代理转发到 gateway-api） |
| 认证方式 | JWT Cookie 自动携带（HttpOnly, Secure, SameSite=Strict），浏览器同域请求自动附加，前端无需手动处理 |
| 内容类型 | 请求 Accept: application/json，响应 Content-Type: application/json |
| 统一响应格式 | 成功：遵循 OpenAI API 标准格式；错误：`{"error": {"message": "...", "type": "..."}}` |
| 超时设置 | 前端 axios 默认超时 10s |
| 重试策略 | 前端不自动重试，提供手动刷新按钮 |

---

## 2. 接口列表

### 2.1 GET /v1/models

**基本信息**：

| 项 | 内容 |
| :--- | :--- |
| 接口路径 | /v1/models |
| 请求方法 | GET |
| 生产方 | gateway-api |
| 消费方 | ops-console |
| 用途 | 获取网关当前注册的可用模型列表 |
| 调用时机 | 统一接入页面 onMounted 时调用；用户点击刷新按钮时重新调用 |
| 协议标准 | 遵循 OpenAI Models List API 格式 |

**请求参数**：

无请求参数（无 Query String、无 Request Body）。

**请求头**：

| Header | 值 | 说明 |
| :--- | :--- | :--- |
| Cookie | JWT Cookie（自动携带） | 浏览器同域请求自动附加，后台统一认证 |
| Accept | application/json | 期望 JSON 格式响应 |

**成功响应**（HTTP 200）：

```json
{
  "object": "list",
  "data": [
    {
      "id": "gpt-4o",
      "object": "model",
      "created": 1700000000,
      "owned_by": "openai"
    },
    {
      "id": "qwen-turbo",
      "object": "model",
      "created": 1700000001,
      "owned_by": "alibaba"
    }
  ]
}
```

**响应字段说明**：

| 字段路径 | 类型 | 必填 | 说明 |
| :--- | :--- | :---: | :--- |
| object | string | 是 | 固定值 "list" |
| data | array | 是 | 模型对象数组，可能为空数组 |
| data[].id | string | 是 | 模型唯一标识（即模型名称） |
| data[].object | string | 是 | 固定值 "model" |
| data[].created | number | 是 | 模型创建时间戳（Unix 秒） |
| data[].owned_by | string | 是 | 模型归属厂商标识 |

**前端使用字段**：仅使用 `data[].id` 和 `data[].owned_by`，其余字段忽略。

**错误响应格式**：

```json
{
  "error": {
    "message": "错误描述信息",
    "type": "错误类型标识"
  }
}
```

**前端调用示例**：

```typescript
import axios from 'axios'

async function fetchModels(): Promise<ModelInfo[]> {
  const response = await axios.get<ModelsListResponse>('/v1/models')
  return response.data.data.map(item => ({
    id: item.id,
    ownedBy: item.owned_by
  }))
}
```

---

### 2.2 POST /api/v1/providers/:id/sync-models

**基本信息**：

| 项 | 内容 |
| :--- | :--- |
| 接口路径 | /api/v1/providers/:id/sync-models |
| 请求方法 | POST |
| 生产方 | control-plane |
| 消费方 | ops-console |
| 用途 | 触发指定渠道的模型同步，获取上游模型与本地模型的差异列表 |
| 调用时机 | 渠道列表页点击"同步模型"按钮时调用 |

**请求参数**：

| 参数 | 位置 | 类型 | 必填 | 说明 |
| :--- | :--- | :--- | :---: | :--- |
| id | URL Path | string | 是 | 渠道唯一标识 |

**成功响应**（HTTP 200）：

```typescript
interface SyncResult {
  added: SyncModelItem[]
  existing: SyncModelItem[]
  removed: SyncModelItem[]
  total_upstream: number
  total_local: number
  unsupported: boolean
}

interface SyncModelItem {
  id: string
  owned_by: string
}
```

**错误处理**：

| HTTP 状态 | error.type | 前端处理 |
| :--- | :--- | :--- |
| 400 | sync_not_supported | 弹窗显示"该渠道类型不支持自动同步" |
| 400 | provider_disabled | ElMessage.warning("请先启用渠道") |
| 502 | upstream_error | ElMessage.error("厂商接口调用失败：" + message) |

---

### 2.3 POST /api/v1/providers/:id/sync-models/confirm

**基本信息**：

| 项 | 内容 |
| :--- | :--- |
| 接口路径 | /api/v1/providers/:id/sync-models/confirm |
| 请求方法 | POST |
| 生产方 | control-plane |
| 消费方 | ops-console |
| 用途 | 确认批量注册勾选的新增模型，并可选禁用已下架模型 |
| 调用时机 | SyncModelsDialog 点击"确认注册"按钮时调用 |

**请求参数**：

| 参数 | 位置 | 类型 | 必填 | 说明 |
| :--- | :--- | :--- | :---: | :--- |
| id | URL Path | string | 是 | 渠道唯一标识 |

**请求体**：

```typescript
interface ConfirmSyncPayload {
  models_to_add: string[]
  disable_removed?: boolean  // 默认 true
}
```

**成功响应**（HTTP 200）：

```typescript
interface ConfirmSyncResult {
  registered: number
  skipped: number
  disabled: number
}
```

**错误处理**：

| HTTP 状态 | 前端处理 |
| :--- | :--- |
| 400 | ElMessage.error("请求参数错误") |
| 401 | 后台统一拦截器重定向到登录页 |
| 500 | ElMessage.error("注册失败，请稍后重试") |

---

## 3. 错误码与前端处理

| HTTP 状态码 | 错误场景 | error.type | 前端处理策略 | UI 表现 |
| :---: | :--- | :--- | :--- | :--- |
| 401 | JWT Cookie 无效或过期 | auth_error | 触发后台统一 axios 拦截器，自动重定向到登录页 | 用户离开当前页面，跳转至 SSO 登录页；登录成功后自动返回 |
| 403 | 用户无权限访问模型列表 | permission_error | 捕获错误，设置 error 状态为权限提示文案 | 模型发现区展示告警："暂无权限查看模型信息"（背景 #3A1C1C，文字 #FF453A） |
| 404 | 接口路径不存在（部署配置错误） | not_found | 捕获错误，设置 error 状态为服务不可用文案 | 模型发现区展示告警："服务暂时不可用，请稍后重试" |
| 500 | gateway-api 内部错误 | server_error | 捕获错误，设置 error 状态为服务不可用文案 | 模型发现区展示告警："服务暂时不可用，请稍后重试"，附刷新按钮 |
| 503 | gateway-api 服务不可用 | server_error | 捕获错误，设置 error 状态为服务不可用文案 | 模型发现区展示告警："服务暂时不可用，请稍后重试"，附刷新按钮 |
| 网络异常 | 请求超时/DNS 失败/连接中断 | - | 捕获 axios 网络错误（无 response 对象） | 模型发现区展示告警："网络异常，请刷新重试"，附刷新按钮 |

**处理原则**：

| 原则 | 说明 |
| :--- | :--- |
| 错误隔离 | 所有接口错误仅影响模型发现区，能力说明区始终正常展示 |
| 不自动重试 | 前端不做自动重试，提供手动刷新按钮由用户决定 |
| 401 全局处理 | 401 由后台统一 axios 拦截器处理，页面组件无需额外逻辑 |
| 错误文案区分 | 根据 HTTP 状态码展示不同的用户友好文案，避免暴露技术细节 |
| 告警样式统一 | 错误态使用告警样式：背景 #3A1C1C，文字 #FF453A，左边框 4px solid #FF453A |


---

## 4. 变更记录 — CHG-20260523-001

## 变更摘要

新增员工端认证接口消费（OIDC 回调、Token 刷新、登出）、Key 管理接口消费（查询/创建/禁用/启用/删除）、模型可用查询接口消费（/v1/me/models）；全部新增接口使用 JWT Bearer Token 认证（非 Cookie）；已有 control-plane providers/models 接口不变。

---

## 认证方式变更

| 项 | 变更前 | 变更后 |
| :--- | :--- | :--- |
| 认证方式 | JWT Cookie（HttpOnly, Secure） | 新增 JWT Bearer Token（`Authorization: Bearer {token}`）用于新增接口 |
| Token 来源 | 复用后台 SSO 登录产生的 Cookie | AuthStore 从 OIDC 回调换取 token 后写入 |
| 添加方式 | 浏览器自动携带 Cookie | axios 请求拦截器统一注入 Authorization 头 |
| 影响范围 | 已有接口（/v1/models） | 新增接口（/auth/*, /api/v1/keys/*, /v1/me/models） |

**axios 拦截器设计**：

```typescript
import axios from 'axios'
import { useAuthStore } from '@/stores/auth'

const apiClient = axios.create({
  baseURL: '',
  timeout: 10000,
  headers: { 'Content-Type': 'application/json' }
})

apiClient.interceptors.request.use((config) => {
  const authStore = useAuthStore()
  if (authStore.accessToken) {
    config.headers.Authorization = `Bearer ${authStore.accessToken}`
  }
  return config
})

apiClient.interceptors.response.use(
  (response) => response,
  async (error) => {
    const authStore = useAuthStore()
    if (error.response?.status === 401 && authStore.refreshToken) {
      try {
        await authStore.refreshAccessToken()
        error.config.headers.Authorization = `Bearer ${authStore.accessToken}`
        return apiClient.request(error.config)
      } catch {
        authStore.logout()
        window.location.href = '/login'
      }
    }
    return Promise.reject(error)
  }
)
```

---

## 新增接口

### 生产方：auth

#### POST /auth/oidc/callback

**基本信息**：

| 项 | 内容 |
| :--- | :--- |
| 接口路径 | /auth/oidc/callback |
| 请求方法 | POST |
| 生产方 | control-plane |
| 消费方 | ops-console |
| 用途 | OIDC 回调，用 SSO 授权码换取身份信息与 JWT Token |
| 调用时机 | LoginCallback.vue onMounted 解析 URL code 后调用 |

**请求体**：

| 参数 | 类型 | 必填 | 说明 |
| :--- | :--- | :---: | :--- |
| code | string | 是 | SSO 回调携带的授权码，一次性有效 |
| state | string | 是 | SSO 回调携带的 state 参数，防 CSRF |

```json
{
  "code": "auth_code_from_sso",
  "state": "random_state_string"
}
```

**成功响应**（HTTP 200）：

```json
{
  "accessToken": "eyJhbGciOiJIUzI1NiIs...",
  "refreshToken": "eyJhbGciOiJIUzI1NiIs...",
  "user": {
    "id": "emp_001",
    "name": "张三",
    "email": "zhangsan@example.com",
    "departmentId": "dept_engineering",
    "departmentName": "工程部"
  },
  "role": "employee"
}
```

**前端调用示例**：

```typescript
async function oidcCallback(code: string, state: string): Promise<LoginResponse> {
  const response = await apiClient.post<LoginResponse>('/auth/oidc/callback', { code, state })
  return response.data
}
```

**错误处理**：

| HTTP 状态 | 场景 | 前端处理 |
| :---: | :--- | :--- |
| 400 | code 已过期或格式无效 | 展示"登录失败，请重新尝试" + [重新登录] 按钮 |
| 400 | state 不匹配 | 展示"登录失败，请重新尝试" + [重新登录] 按钮 |
| 401 | code 已被消费 | 展示"登录失败，请重新尝试" + [重新登录] 按钮 |
| 500 | auth 内部错误 | 展示"认证服务暂时不可用，请稍后重试" + [重新登录] 按钮 |
| 网络异常 | 请求超时或连接中断 | 展示"认证服务暂时不可用，请稍后重试" + [重新登录] 按钮 |

---

#### POST /auth/refresh

**基本信息**：

| 项 | 内容 |
| :--- | :--- |
| 接口路径 | /auth/refresh |
| 请求方法 | POST |
| 生产方 | control-plane |
| 消费方 | ops-console |
| 用途 | 使用 refresh token 续期 access token（滚动刷新） |
| 调用时机 | axios 响应拦截器检测到 401 时自动调用；也可在 accessToken 即将过期时主动调用 |

**请求体**：

```json
{
  "refreshToken": "eyJhbGciOiJIUzI1NiIs..."
}
```

**成功响应**（HTTP 200）：

```json
{
  "accessToken": "eyJhbGciOiJIUzI1NiIs...",
  "refreshToken": "eyJhbGciOiJIUzI1NiIs..."
}
```

**前端调用示例**：

```typescript
async function refreshToken(refreshToken: string): Promise<RefreshResponse> {
  const response = await apiClient.post<RefreshResponse>('/auth/refresh', { refreshToken })
  return response.data
}
```

**错误处理**：

| HTTP 状态 | 场景 | 前端处理 |
| :---: | :--- | :--- |
| 400 | refreshToken 格式无效 | AuthStore.logout()，跳转 SSO 登录 |
| 401 | refreshToken 已过期 | AuthStore.logout()，跳转 SSO 登录 |
| 500 | 服务端错误 | AuthStore.logout()，跳转 SSO 登录 |
| 网络异常 | 请求超时或失败 | AuthStore.logout()，跳转 SSO 登录 |

---

#### POST /auth/logout

**基本信息**：

| 项 | 内容 |
| :--- | :--- |
| 接口路径 | /auth/logout |
| 请求方法 | POST |
| 生产方 | control-plane |
| 消费方 | ops-console |
| 用途 | 服务端注销当前 Token（标记 refreshToken 为无效），前端同步清空 AuthStore |
| 调用时机 | 用户主动点击"退出登录"时调用 |

**请求体**：

```json
{
  "refreshToken": "eyJhbGciOiJIUzI1NiIs..."
}
```

**成功响应**（HTTP 200）：

```json
{
  "code": 0,
  "message": "success",
  "data": null,
  "request_id": "req-uuid"
}
```

**前端调用示例**：

```typescript
async function logout(): Promise<void> {
  const authStore = useAuthStore()
  try {
    await apiClient.post('/auth/logout', { refreshToken: authStore.refreshToken })
  } finally {
    authStore.$reset()
    localStorage.removeItem('ops-console:auth')
    window.location.href = '/login'
  }
}
```

**错误处理**：

| HTTP 状态 | 场景 | 前端处理 |
| :---: | :--- | :--- |
| 任意错误 | 服务端注销失败 | 前端仍然执行 AuthStore.$reset() + 跳转登录（保证前端状态已清除） |

---

#### GET /api/v1/keys

**基本信息**：

| 项 | 内容 |
| :--- | :--- |
| 接口路径 | /api/v1/keys |
| 请求方法 | GET |
| 生产方 | control-plane |
| 消费方 | ops-console |
| 用途 | 查询当前登录员工名下的 Key 列表（脱敏） |
| 调用时机 | KeyListPage.vue onMounted；创建/禁用/启用/删除操作成功后刷新列表 |
| 认证 | Authorization: Bearer {accessToken} |

**请求参数**：无。

**请求头**：

| Header | 值 | 说明 |
| :--- | :--- | :--- |
| Authorization | Bearer {accessToken} | JWT Bearer token，由 axios 拦截器注入 |

**成功响应**（HTTP 200）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "items": [
      {
        "id": "key_001",
        "name": "生产环境 Key",
        "masked_key": "****abcd",
        "key_hint": "abcd",
        "enabled": true,
        "created_at": "2026-05-20T10:30:00Z",
        "updated_at": "2026-05-20T10:30:00Z"
      }
    ],
    "total": 1,
    "page": 1,
    "page_size": 20
  },
  "request_id": "req-uuid"
}
```

**前端调用示例**：

```typescript
async function listKeys(): Promise<ApiKey[]> {
  const response = await apiClient.get<ListKeysResponse>('/api/v1/keys')
  return mapKeysResponse(response.data)
}
```

**错误处理**：

| HTTP 状态 | 场景 | 前端处理 |
| :---: | :--- | :--- |
| 401 | Token 过期或无效 | axios 拦截器自动触发 refresh 或跳转登录 |
| 403 | 无权限（非本用户数据） | 展示"暂无权限查看 Key 列表" |
| 500 | 服务端错误 | 展示"Key 列表加载失败，请刷新重试" + [刷新] 按钮 |
| 网络异常 | 请求超时或中断 | 展示"网络异常，请刷新重试" + [刷新] 按钮 |

---

#### POST /api/v1/keys

**基本信息**：

| 项 | 内容 |
| :--- | :--- |
| 接口路径 | /api/v1/keys |
| 请求方法 | POST |
| 生产方 | control-plane |
| 消费方 | ops-console |
| 用途 | 为当前员工创建新的 API Key |
| 调用时机 | KeyCreateDialog.vue 点击"确认创建"时调用 |
| 认证 | Authorization: Bearer {accessToken} |

**请求体**：

| 参数 | 类型 | 必填 | 说明 |
| :--- | :--- | :---: | :--- |
| name | string | 是 | Key 名称，最多 64 字符 |

```json
{
  "name": "生产环境 Key"
}
```

**成功响应**（HTTP 201）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": "key_001",
    "name": "生产环境 Key",
    "key": "sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
    "key_hint": "abcd",
    "enabled": true,
    "created_at": "2026-05-20T10:30:00Z",
    "updated_at": "2026-05-20T10:30:00Z"
  },
  "request_id": "req-uuid"
}
```

**前端调用示例**：

```typescript
async function createKey(name: string): Promise<CreateKeyResponse> {
  const response = await apiClient.post<CreateKeyResponse>('/api/v1/keys', { name })
  return response.data
}
```

**错误处理**：

| HTTP 状态 | 场景 | 前端处理 |
| :---: | :--- | :--- |
| 400 | name 为空或格式无效 | 表单 name 字段下方展示错误提示 |
| 401 | Token 过期或无效 | axios 拦截器自动处理 |
| 409 | Key 数量已达上限 | ElMessage.error("Key 数量已达上限")，按钮不置灰由响应结果驱动 |
| 422 | name 校验失败 | 表单字段下方展示校验错误详情 |
| 500 | 创建失败 | ElMessage.error("创建失败，请稍后重试") |

---

#### PATCH /api/v1/keys/:id/disable

**基本信息**：

| 项 | 内容 |
| :--- | :--- |
| 接口路径 | /api/v1/keys/:id/disable |
| 请求方法 | PATCH |
| 生产方 | control-plane |
| 消费方 | ops-console |
| 用途 | 禁用指定的 API Key |
| 调用时机 | KeyListPage.vue 点击"禁用"按钮时调用 |
| 认证 | Authorization: Bearer {accessToken} |

**路径参数**：

| 参数 | 类型 | 必填 | 说明 |
| :--- | :--- | :---: | :--- |
| id | string | 是 | Key 唯一标识 |

**成功响应**（HTTP 200）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": "key_001",
    "name": "生产环境 Key",
    "masked_key": "****abcd",
    "key_hint": "abcd",
    "enabled": false,
    "created_at": "2026-05-20T10:30:00Z",
    "updated_at": "2026-05-20T11:00:00Z"
  },
  "request_id": "req-uuid"
}
```

**前端调用示例**：

```typescript
async function disableKey(id: string): Promise<void> {
  await apiClient.patch(`/api/v1/keys/${id}/disable`)
}

async function enableKey(id: string): Promise<void> {
  await apiClient.patch(`/api/v1/keys/${id}/enable`)
}
```

---

#### PATCH /api/v1/keys/:id/enable

**基本信息**：

| 项 | 内容 |
| :--- | :--- |
| 接口路径 | /api/v1/keys/:id/enable |
| 请求方法 | PATCH |
| 生产方 | control-plane |
| 消费方 | ops-console |
| 用途 | 启用指定的 API Key |
| 调用时机 | KeyListPage.vue 点击"启用"按钮时调用 |
| 认证 | Authorization: Bearer {accessToken} |

**路径参数**：

| 参数 | 类型 | 必填 | 说明 |
| :--- | :--- | :---: | :--- |
| id | string | 是 | Key 唯一标识 |

**成功响应**（HTTP 200）与 disable 接口一致，仅 `enabled=true`。

**错误处理**与 disable 接口一致。

**错误处理**：

| HTTP 状态 | 场景 | 前端处理 |
| :---: | :--- | :--- |
| 401 | Token 过期或无效 | axios 拦截器自动处理 |
| 403 | 操作他人的 Key | ElMessage.error("无权限操作此 Key") |
| 404 | Key 不存在 | ElMessage.error("Key 不存在或已被删除")，刷新列表 |
| 500 | 操作失败 | ElMessage.error("操作失败，请稍后重试") |

---

#### DELETE /api/v1/keys/:id

**基本信息**：

| 项 | 内容 |
| :--- | :--- |
| 接口路径 | /api/v1/keys/:id |
| 请求方法 | DELETE |
| 生产方 | control-plane |
| 消费方 | ops-console |
| 用途 | 删除指定的 API Key |
| 调用时机 | KeyListPage.vue 点击"删除"并确认后调用 |
| 认证 | Authorization: Bearer {accessToken} |

**路径参数**：

| 参数 | 类型 | 必填 | 说明 |
| :--- | :--- | :---: | :--- |
| id | string | 是 | Key 唯一标识 |

**成功响应**（HTTP 200）：

```json
{
  "code": 0,
  "message": "success",
  "data": null,
  "request_id": "req-uuid"
}
```

**前端调用示例**：

```typescript
async function deleteKey(id: string): Promise<void> {
  await apiClient.delete(`/api/v1/keys/${id}`)
}
```

**错误处理**：

| HTTP 状态 | 场景 | 前端处理 |
| :---: | :--- | :--- |
| 401 | Token 过期或无效 | axios 拦截器自动处理 |
| 403 | 操作他人的 Key | ElMessage.error("无权限操作此 Key") |
| 404 | Key 不存在 | ElMessage.error("Key 不存在或已被删除")，刷新列表 |
| 500 | 删除失败 | ElMessage.error("删除失败，请稍后重试") |

---

### 生产方：gateway-api

#### GET /v1/me/models

**基本信息**：

| 项 | 内容 |
| :--- | :--- |
| 接口路径 | /v1/me/models |
| 请求方法 | GET |
| 生产方 | gateway-api |
| 消费方 | ops-console |
| 用途 | 查询当前登录员工身份可用的模型列表（基于 User → Department 过滤） |
| 调用时机 | ModelListPage.vue onMounted |
| 认证 | Authorization: Bearer {accessToken} |
| 差异说明 | 与已有 /v1/models（全量已注册模型）不同，/v1/me/models 按请求方 JWT 身份做部门级过滤 |

**请求参数**：无。

**请求头**：

| Header | 值 | 说明 |
| :--- | :--- | :--- |
| Authorization | Bearer {accessToken} | JWT Bearer token，由 axios 拦截器注入 |

**成功响应**（HTTP 200）：

```json
{
  "object": "list",
  "data": [
    {
      "id": "gpt-4o",
      "object": "model",
      "owned_by": "openai",
      "type": "chat",
      "enabled": true
    },
    {
      "id": "qwen-turbo",
      "object": "model",
      "owned_by": "bailian",
      "type": "chat",
      "enabled": true
    },
    {
      "id": "text-embedding-v3",
      "object": "model",
      "owned_by": "bailian",
      "type": "embedding",
      "enabled": true
    }
  ]
}
```

**响应字段说明**：

| 字段路径 | 类型 | 必填 | 说明 |
| :--- | :--- | :---: | :--- |
| object | string | 是 | 固定值 `list` |
| data | array | 是 | 模型对象数组，可能为空数组 |
| data[].id | string | 是 | 模型唯一标识（名称） |
| data[].object | string | 是 | 固定值 `model` |
| data[].owned_by | string | 是 | 模型所属厂商标识 |
| data[].type | string | 是 | 模型类型：chat / completion / embedding / image / audio |
| data[].enabled | boolean | 是 | 当前身份是否可调用 |

**前端调用示例**：

```typescript
async function fetchMyModels(): Promise<ModelInfo[]> {
  const response = await apiClient.get<MeModelsResponse>('/v1/me/models')
  return mapMeModelsResponse(response.data)
}
```

**错误处理**：

| HTTP 状态 | 场景 | 前端处理 |
| :---: | :--- | :--- |
| 200 + 空 data | 当前身份无可用模型 | 展示空态："当前身份暂无可用模型" |
| 401 | Token 过期或无效 | axios 拦截器自动触发 refresh 或跳转登录 |
| 403 | 无权限 | 展示"暂无权限查看模型列表" |
| 500 | gateway-api 内部错误 | 展示"模型列表加载失败，请刷新重试" + [刷新] 按钮 |
| 网络异常 | 请求超时或中断 | 展示"网络异常，请刷新重试" + [刷新] 按钮 |

---

## 已有接口（不变更）

以下接口在本次变更中不变，继续用于管理员端页面，认证方式不变：

| 接口 | 认证方式 | 消费方页面 | 说明 |
| :--- | :--- | :--- | :--- |
| GET /v1/models | JWT Cookie | UnifiedEntryPage.vue | 全量已注册模型列表（已有） |
| GET /api/v1/providers | JWT Cookie (后台统一) | ProviderListPage.vue | 渠道列表（已有） |
| POST /api/v1/providers | JWT Cookie | ProviderListPage.vue | 创建渠道（已有） |
| PUT /api/v1/providers/:id | JWT Cookie | ProviderListPage.vue | 更新渠道（已有） |
| DELETE /api/v1/providers/:id | JWT Cookie | ProviderListPage.vue | 删除渠道（已有） |
| POST /api/v1/providers/:id/sync-models | JWT Cookie | ProviderListPage.vue | 同步模型（已有） |
| POST /api/v1/providers/:id/sync-models/confirm | JWT Cookie | ProviderListPage.vue | 确认注册（已有） |
| GET /api/v1/models | JWT Cookie | ModelListPage.vue（管理端） | 模型列表（已有） |
| POST /api/v1/models | JWT Cookie | ModelListPage.vue（管理端） | 注册模型（已有） |
| PUT /api/v1/models/:id | JWT Cookie | ModelListPage.vue（管理端） | 更新模型（已有） |
| DELETE /api/v1/models/:id | JWT Cookie | ModelListPage.vue（管理端） | 删除模型（已有） |
| GET /api/v1/models/check-name | JWT Cookie | ModelFormDialog.vue | 名称唯一性校验（已有） |

---

## 错误码总览（新增部分）

| HTTP 状态码 | 接口 | 错误场景 | 前端处理策略 | UI 表现 |
| :---: | :--- | :--- | :--- | :--- |
| 400 | POST /auth/oidc/callback | code 无效、过期或格式错误 | 设置 error 状态 | 登录失败页："登录失败，请重新尝试" + [重新登录] |
| 400 | POST /auth/refresh | refreshToken 格式无效 | AuthStore.logout() | 跳转 SSO 登录页 |
| 400 | POST /api/v1/keys | name 为空或无效 | 表单字段级错误 | name 输入框下方红色提示 |
| 401 | 全部新增接口 | Token 过期或无效 | axios 拦截器：自动 refresh → 失败则 logout | 无感知续期或跳转登录 |
| 401 | POST /auth/oidc/callback | code 已被消费 | 展示错误 | "登录失败，请重新尝试" |
| 403 | GET /api/v1/keys | 无权查看 | 展示错误态 | "暂无权限查看 Key 列表" |
| 403 | PATCH/DELETE /api/v1/keys/:id | 操作他人的 Key | ElMessage.error | "无权限操作此 Key" |
| 403 | GET /v1/me/models | 无权限 | 展示错误态 | "暂无权限查看模型列表" |
| 404 | PATCH/DELETE /api/v1/keys/:id | Key 不存在 | ElMessage.error + 刷新列表 | "Key 不存在或已被删除" |
| 409 | POST /api/v1/keys | Key 数量已达上限 | ElMessage.error | "Key 数量已达上限" |
| 422 | POST/PATCH /api/v1/keys | 字段校验失败 | 解析 errors 字段映射到表单项 | 表单字段下方展示校验错误 |
| 500 | POST /auth/oidc/callback | auth 内部错误 | 展示错误态 | "认证服务暂时不可用，请稍后重试" + [重新登录] |
| 500 | POST /auth/refresh | 服务端错误 | AuthStore.logout() | 跳转 SSO 登录 |
| 500 | GET/POST/PATCH/DELETE /api/v1/keys | control-plane 内部错误 | ElMessage.error / 展示错误态 + 刷新按钮 | "操作失败，请稍后重试" |
| 500 | GET /v1/me/models | gateway-api 内部错误 | 展示错误态 + 刷新按钮 | "模型列表加载失败，请刷新重试" |
| 网络异常 | 全部新增接口 | 超时、DNS 失败、连接中断 | 展示错误态 / ElMessage.error | "网络异常，请稍后重试" / "认证服务暂时不可用" |

**处理原则（新增）**：

| 原则 | 说明 |
| :--- | :--- |
| 401 自动续期 | axios 响应拦截器统一拦截 401 → 尝试 refreshToken → 失败后跳转登录；页面组件无需处理 |
| 认证接口不重试 | 回调、刷新、登出接口失败后不自动重试，由用户重新发起 |
| Key 操作即时刷新 | Key 创建/禁用/启用/删除成功后，前端自动刷新列表确保数据一致 |
| 明文不缓存 | plaintext 仅在弹窗内展示，不写入任何持久化存储 |
| 错误文案区分 | 根据 HTTP 状态码展示用户友好文案，避免暴露技术细节 |


## 5. 变更记录 — CHG-20260525-001

## 变更摘要

新增模型组管理相关接口的消费契约，全部接口生产方为 **control-plane**，消费方为 **ops-console 管理员端**。认证方式沿用已有管理端的 JWT Cookie / Bearer Token 认证体系。

---

## 1. 调用的后端接口

### 接口概览

| 序号 | 方法 | 接口路径 | 用途 | 调用时机 |
| :---: | :--- | :--- | :--- | :--- |
| 1 | POST | /api/v1/groups | 创建模型组 | GroupEditDialog 提交（create 模式） |
| 2 | GET | /api/v1/groups?search={keyword} | 搜索/列表模型组 | GroupListPage onMounted + 搜索输入 |
| 3 | GET | /api/v1/groups/:id | 获取模型组详情 | GroupEditDialog 打开（edit 模式） |
| 4 | PUT | /api/v1/groups/:id | 更新模型组 | GroupEditDialog 提交（edit 模式） |
| 5 | DELETE | /api/v1/groups/:id | 删除模型组 | GroupListPage 删除确认后 |
| 6 | POST | /api/v1/groups/:id/models | 添加模型到组 | GroupEditDialog 保存（含新增模型） |
| 7 | GET | /api/v1/groups/:id/models | 获取组内模型列表 | GroupEditDialog 打开（edit 模式） |
| 8 | DELETE | /api/v1/groups/:id/models/:model_id | 从组内移除模型 | GroupEditDialog 点击移除操作 |
| 9 | GET | /api/v1/models | 获取全量模型（选择器用） | ModelGroupSelector 打开 |

### 公共约定

| 约定项 | 内容 |
| :--- | :--- |
| Base URL | 同域部署，前端请求路径由 Vite 代理或 Nginx 反向代理转发到 control-plane |
| 认证方式 | 管理员端复用已有管理后台认证体系（JWT Cookie / Bearer Token，由 axios 拦截器统一注入） |
| 内容类型 | 请求 Content-Type: application/json；响应 Content-Type: application/json |
| 统一响应信封 | `{ "code": 0, "message": "success", "data": {...}, "request_id": "..." }` |
| 成功 code | `0` |
| 超时设置 | axios 默认超时 10s |
| 重试策略 | 前端不自动重试，提供手动操作入口（列表页刷新按钮、用户重新提交） |

---

## 2. 接口详细契约

### 2.1 POST /api/v1/groups — 创建模型组

**基本信息**：

| 项 | 内容 |
| :--- | :--- |
| 接口路径 | /api/v1/groups |
| 请求方法 | POST |
| 生产方 | control-plane |
| 消费方 | ops-console (GroupEditDialog) |

**请求体**：

| 参数 | 类型 | 必填 | 说明 |
| :--- | :--- | :---: | :--- |
| name | string | 是 | 组名称，最多 64 字符 |
| description | string | 否 | 组描述 |
| strategy | string | 是 | 路由策略：round_robin / priority / weighted |
| model_ids | string[] | 否 | 初始关联的模型 ID 列表 |
| priorities | Record\<string, number\> | 条件必填 | strategy=priority 时必传，model_id → priority 映射 |
| weights | Record\<string, number\> | 条件必填 | strategy=weighted 时必传，model_id → weight 映射 |

**请求示例**（priority 策略）：

```json
{
  "name": "日常推理",
  "description": "日常推理场景模型组",
  "strategy": "priority",
  "model_ids": ["model_001", "model_002", "model_003"],
  "priorities": {
    "model_001": 0,
    "model_002": 1,
    "model_003": 2
  }
}
```

**请求示例**（weighted 策略）：

```json
{
  "name": "高负载场景",
  "description": "高负载加权模型组",
  "strategy": "weighted",
  "model_ids": ["model_001", "model_002"],
  "weights": {
    "model_001": 70,
    "model_002": 30
  }
}
```

**请求示例**（round_robin 策略）：

```json
{
  "name": "默认轮询组",
  "description": "",
  "strategy": "round_robin",
  "model_ids": ["model_001", "model_002", "model_003"]
}
```

**成功响应**（HTTP 201）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": "group_001",
    "name": "日常推理",
    "description": "日常推理场景模型组",
    "strategy": "priority",
    "enabled": true,
    "model_count": 3,
    "created_at": "2026-05-25T10:00:00Z",
    "updated_at": "2026-05-25T10:00:00Z"
  },
  "request_id": "req-uuid-001"
}
```

**前端调用示例**：

```typescript
async function createGroup(
  form: GroupForm,
  models: GroupModel[]
): Promise<Group> {
  const body: CreateGroupRequest = {
    name: form.name.trim(),
    description: form.description?.trim() || '',
    strategy: form.strategy,
    model_ids: models.map(m => m.model_id)
  }
  if (form.strategy === 'priority') {
    body.priorities = Object.fromEntries(
      models.map(m => [m.model_id, m.priority!])
    )
  }
  if (form.strategy === 'weighted') {
    body.weights = Object.fromEntries(
      models.map(m => [m.model_id, m.weight!])
    )
  }
  const res = await apiClient.post<ApiResponse<Group>>('/api/v1/groups', body)
  return res.data.data
}
```

---

### 2.2 GET /api/v1/groups — 搜索/列表模型组

**基本信息**：

| 项 | 内容 |
| :--- | :--- |
| 接口路径 | /api/v1/groups |
| 请求方法 | GET |
| 生产方 | control-plane |
| 消费方 | ops-console (GroupListPage) |

**请求参数**：

| 参数 | 位置 | 类型 | 必填 | 说明 |
| :--- | :--- | :--- | :---: | :--- |
| search | Query | string | 否 | 按组名称模糊搜索，空值时返回全量列表 |

**请求示例**：

```
GET /api/v1/groups?search=日常
```

**成功响应**（HTTP 200）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "items": [
      {
        "id": "group_001",
        "name": "日常推理",
        "description": "日常推理场景模型组",
        "strategy": "priority",
        "enabled": true,
        "model_count": 3,
        "created_at": "2026-05-25T10:00:00Z",
        "updated_at": "2026-05-25T10:00:00Z"
      },
      {
        "id": "group_002",
        "name": "高负载场景",
        "description": "高负载加权模型组",
        "strategy": "weighted",
        "enabled": true,
        "model_count": 2,
        "created_at": "2026-05-25T10:30:00Z",
        "updated_at": "2026-05-25T10:30:00Z"
      }
    ],
    "total": 2
  },
  "request_id": "req-uuid-002"
}
```

**空列表响应**：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "items": [],
    "total": 0
  },
  "request_id": "req-uuid-003"
}
```

**前端调用示例**：

```typescript
async function fetchGroups(search?: string): Promise<GroupListItem[]> {
  const params = search ? { search } : {}
  const res = await apiClient.get<GroupListResponse>('/api/v1/groups', { params })
  return res.data.data.items.map(g => ({
    ...g,
    strategy_label: STRATEGY_LABEL_MAP[g.strategy]
  }))
}
```

---

### 2.3 GET /api/v1/groups/:id — 获取模型组详情

**基本信息**：

| 项 | 内容 |
| :--- | :--- |
| 接口路径 | /api/v1/groups/:id |
| 请求方法 | GET |
| 生产方 | control-plane |
| 消费方 | ops-console (GroupEditDialog edit 模式) |

**路径参数**：

| 参数 | 类型 | 必填 | 说明 |
| :--- | :--- | :---: | :--- |
| id | string | 是 | 模型组唯一标识 |

**请求示例**：

```
GET /api/v1/groups/group_001
```

**成功响应**（HTTP 200）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": "group_001",
    "name": "日常推理",
    "description": "日常推理场景模型组",
    "strategy": "priority",
    "enabled": true,
    "model_count": 3,
    "created_at": "2026-05-25T10:00:00Z",
    "updated_at": "2026-05-25T10:00:00Z"
  },
  "request_id": "req-uuid-004"
}
```

**前端调用示例**：

```typescript
async function getGroup(id: string): Promise<Group> {
  const res = await apiClient.get<GroupDetailResponse>(`/api/v1/groups/${id}`)
  return res.data.data
}
```

---

### 2.4 PUT /api/v1/groups/:id — 更新模型组

**基本信息**：

| 项 | 内容 |
| :--- | :--- |
| 接口路径 | /api/v1/groups/:id |
| 请求方法 | PUT |
| 生产方 | control-plane |
| 消费方 | ops-console (GroupEditDialog edit 模式) |

**路径参数**：

| 参数 | 类型 | 必填 | 说明 |
| :--- | :--- | :---: | :--- |
| id | string | 是 | 模型组唯一标识 |

**请求体**：

| 参数 | 类型 | 必填 | 说明 |
| :--- | :--- | :---: | :--- |
| name | string | 否 | 组名称，最多 64 字符 |
| description | string | 否 | 组描述 |
| strategy | string | 否 | 路由策略 |

**请求示例**：

```json
{
  "name": "日常推理（更新）",
  "description": "已更新描述",
  "strategy": "weighted"
}
```

**成功响应**（HTTP 200）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": "group_001",
    "name": "日常推理（更新）",
    "description": "已更新描述",
    "strategy": "weighted",
    "enabled": true,
    "model_count": 3,
    "created_at": "2026-05-25T10:00:00Z",
    "updated_at": "2026-05-25T11:00:00Z"
  },
  "request_id": "req-uuid-005"
}
```

**前端调用示例**：

```typescript
async function updateGroup(id: string, form: GroupForm): Promise<Group> {
  const body: UpdateGroupRequest = {
    name: form.name.trim(),
    description: form.description?.trim() || '',
    strategy: form.strategy
  }
  const res = await apiClient.put<ApiResponse<Group>>(`/api/v1/groups/${id}`, body)
  return res.data.data
}
```

---

### 2.5 DELETE /api/v1/groups/:id — 删除模型组

**基本信息**：

| 项 | 内容 |
| :--- | :--- |
| 接口路径 | /api/v1/groups/:id |
| 请求方法 | DELETE |
| 生产方 | control-plane |
| 消费方 | ops-console (GroupListPage) |

**路径参数**：

| 参数 | 类型 | 必填 | 说明 |
| :--- | :--- | :---: | :--- |
| id | string | 是 | 模型组唯一标识 |

**请求示例**：

```
DELETE /api/v1/groups/group_001
```

**成功响应**（HTTP 200）：

```json
{
  "code": 0,
  "message": "success",
  "data": null,
  "request_id": "req-uuid-006"
}
```

**前端调用示例**：

```typescript
async function deleteGroup(id: string): Promise<void> {
  await apiClient.delete(`/api/v1/groups/${id}`)
}
```

---

### 2.6 POST /api/v1/groups/:id/models — 添加模型到组

**基本信息**：

| 项 | 内容 |
| :--- | :--- |
| 接口路径 | /api/v1/groups/:id/models |
| 请求方法 | POST |
| 生产方 | control-plane |
| 消费方 | ops-console (GroupEditDialog 保存时) |

**路径参数**：

| 参数 | 类型 | 必填 | 说明 |
| :--- | :--- | :---: | :--- |
| id | string | 是 | 模型组唯一标识 |

**请求体**：

| 参数 | 类型 | 必填 | 说明 |
| :--- | :--- | :---: | :--- |
| model_ids | string[] | 是 | 要添加的模型 ID 列表 |
| priorities | Record\<string, number\> | 条件必填 | strategy=priority 时必传 |
| weights | Record\<string, number\> | 条件必填 | strategy=weighted 时必传 |

**请求示例**（priority 策略）：

```json
{
  "model_ids": ["model_001", "model_002"],
  "priorities": {
    "model_001": 0,
    "model_002": 1
  }
}
```

**请求示例**（weighted 策略）：

```json
{
  "model_ids": ["model_001", "model_002"],
  "weights": {
    "model_001": 70,
    "model_002": 30
  }
}
```

**成功响应**（HTTP 200）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "added_count": 2
  },
  "request_id": "req-uuid-007"
}
```

**前端调用示例**：

```typescript
async function addModelsToGroup(
  groupId: string,
  models: GroupModel[],
  strategy: Strategy
): Promise<void> {
  const body: AddModelToGroupRequest = {
    model_ids: models.map(m => m.model_id)
  }
  if (strategy === 'priority') {
    body.priorities = Object.fromEntries(
      models.map(m => [m.model_id, m.priority!])
    )
  }
  if (strategy === 'weighted') {
    body.weights = Object.fromEntries(
      models.map(m => [m.model_id, m.weight!])
    )
  }
  await apiClient.post(`/api/v1/groups/${groupId}/models`, body)
}
```

---

### 2.7 GET /api/v1/groups/:id/models — 获取组内模型列表

**基本信息**：

| 项 | 内容 |
| :--- | :--- |
| 接口路径 | /api/v1/groups/:id/models |
| 请求方法 | GET |
| 生产方 | control-plane |
| 消费方 | ops-console (GroupEditDialog edit 模式) |

**路径参数**：

| 参数 | 类型 | 必填 | 说明 |
| :--- | :--- | :---: | :--- |
| id | string | 是 | 模型组唯一标识 |

**请求示例**：

```
GET /api/v1/groups/group_001/models
```

**成功响应**（HTTP 200）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "items": [
      {
        "group_id": "group_001",
        "model_id": "model_001",
        "model_name": "gpt-4o",
        "priority": 0
      },
      {
        "group_id": "group_001",
        "model_id": "model_002",
        "model_name": "qwen-turbo",
        "priority": 1
      },
      {
        "group_id": "group_001",
        "model_id": "model_003",
        "model_name": "claude-sonnet",
        "priority": 2
      }
    ]
  },
  "request_id": "req-uuid-008"
}
```

**空列表响应**：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "items": []
  },
  "request_id": "req-uuid-009"
}
```

**前端调用示例**：

```typescript
async function getGroupModels(groupId: string): Promise<GroupModel[]> {
  const res = await apiClient.get<GroupModelsResponse>(
    `/api/v1/groups/${groupId}/models`
  )
  return res.data.data.items
}
```

---

### 2.8 DELETE /api/v1/groups/:id/models/:model_id — 从组内移除模型

**基本信息**：

| 项 | 内容 |
| :--- | :--- |
| 接口路径 | /api/v1/groups/:id/models/:model_id |
| 请求方法 | DELETE |
| 生产方 | control-plane |
| 消费方 | ops-console (GroupEditDialog) |

**路径参数**：

| 参数 | 类型 | 必填 | 说明 |
| :--- | :--- | :---: | :--- |
| id | string | 是 | 模型组唯一标识 |
| model_id | string | 是 | 模型唯一标识 |

**请求示例**：

```
DELETE /api/v1/groups/group_001/models/model_001
```

**成功响应**（HTTP 200）：

```json
{
  "code": 0,
  "message": "success",
  "data": null,
  "request_id": "req-uuid-010"
}
```

**前端调用示例**：

```typescript
async function removeModelFromGroup(
  groupId: string,
  modelId: string
): Promise<void> {
  await apiClient.delete(`/api/v1/groups/${groupId}/models/${modelId}`)
}
```

---

### 2.9 GET /api/v1/models — 获取全量模型（选择器用）

**基本信息**：

| 项 | 内容 |
| :--- | :--- |
| 接口路径 | /api/v1/models |
| 请求方法 | GET |
| 生产方 | control-plane |
| 消费方 | ops-console (ModelGroupSelector) |

**请求参数**：无。

**请求示例**：

```
GET /api/v1/models
```

**成功响应**（HTTP 200）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "items": [
      { "id": "model_001", "name": "gpt-4o" },
      { "id": "model_002", "name": "qwen-turbo" },
      { "id": "model_003", "name": "claude-sonnet" },
      { "id": "model_004", "name": "qwen-max" },
      { "id": "model_005", "name": "text-embedding-v3" }
    ]
  },
  "request_id": "req-uuid-011"
}
```

> 注：此接口与模型管理页已有的 `GET /api/v1/models` 为同一接口。选择器仅使用 `id` 和 `name` 字段，其余字段忽略。

**前端调用示例**：

```typescript
async function fetchAllModels(): Promise<ModelOption[]> {
  const res = await apiClient.get<ModelListForSelectorResponse>('/api/v1/models')
  return res.data.data.items
}
```

---

## 3. 错误处理

### 3.1 错误处理矩阵

| HTTP 状态 | 场景 | 错误码/类型 | 前端处理 | UI 表现 |
| :---: | :--- | :--- | :--- | :--- |
| 400 | 请求参数校验失败 | — | ElMessage.warning(后端 message) | 表单顶部或字段级错误提示 |
| 401 | 认证失效 | — | axios 拦截器统一处理 → 跳转登录 | 用户重定向至 SSO 登录 |
| 403 | 无管理员权限 | — | ElMessage.error("无权限执行此操作") | 红色 toast 提示 |
| 404 | group_not_found | — | ElMessage.error("模型组不存在或已被删除") → 关闭弹窗 → 刷新列表 | toast 提示 + 列表页刷新 |
| 409 | group_name_conflict | — | ElMessage.error(后端 message，如 "模型组名称已存在") | 红色 toast 提示，表单不清空 |
| 500 | 服务端内部错误 | — | ElMessage.error("操作失败，请重试") | 红色 toast 提示 |
| 502/503 | 上游服务不可用 | — | ElMessage.error("服务暂时不可用，请稍后重试") | 红色 toast 提示 |
| 网络异常 | 超时、DNS 失败、连接中断 | — | ElMessage.error("操作失败，请重试") | 红色 toast 提示 |

### 3.2 错误处理实现

**GroupListPage 列表加载错误**：

```typescript
async function fetchGroups() {
  state.loading = true
  state.error = null
  try {
    const groups = await groupsApi.list(state.searchKeyword || undefined)
    state.groups = groups
  } catch (err: any) {
    if (err.response?.status === 401) {
      return // 由 axios 拦截器统一处理
    }
    state.error = '加载失败，请刷新重试'
    state.groups = []
  } finally {
    state.loading = false
  }
}
```

**GroupEditDialog 提交错误**：

```typescript
async function handleSubmit() {
  // ... 校验 ...
  state.submitting = true
  try {
    if (props.mode === 'create') {
      await groupsApi.create(state.form, state.selectedModels)
    } else {
      await groupsApi.update(props.groupId!, state.form)
      // 增量同步模型变更
      await syncModels()
    }
    ElMessage.success(props.mode === 'create' ? '模型组创建成功' : '模型组更新成功')
    emit('success')
  } catch (err: any) {
    if (err.response?.status === 409) {
      ElMessage.error(err.response.data?.message || '模型组名称已存在')
    } else if (err.response?.status === 404) {
      ElMessage.error('模型组不存在或已被删除')
      emit('success') // 关闭弹窗并刷新列表
    } else if (err.response?.status === 400) {
      ElMessage.warning(err.response.data?.message || '请检查输入参数')
    } else if (err.response?.status !== 401) {
      ElMessage.error('操作失败，请重试')
    }
  } finally {
    state.submitting = false
  }
}
```

**ModelGroupSelector 加载错误**：

```typescript
async function loadModels() {
  state.loading = true
  try {
    const models = await modelsApi.list()
    state.allModels = models
    state.selectedIds = new Set(props.existingModelIds)
  } catch (err: any) {
    if (err.response?.status !== 401) {
      ElMessage.error('模型列表加载失败')
    }
  } finally {
    state.loading = false
  }
}
```

### 3.3 409 group_name_conflict 处理

```typescript
// control-plane 返回示例：
// HTTP 409
{
  "code": 409,
  "message": "模型组名称已存在",
  "data": null,
  "request_id": "req-uuid-xxx"
}

// 前端处理：展示后端 message 内容到 ElMessage.error
ElMessage.error(response.data.message)
// 弹窗不关闭，用户可修改名称后重新提交
```

### 3.4 404 group_not_found 处理

```typescript
// 场景：编辑时其他管理员已删除该组
// 前端处理：
ElMessage.error('模型组不存在或已被删除')
emit('success') // 关闭弹窗并触发列表刷新
```

### 3.5 处理原则

| 原则 | 说明 |
| :--- | :--- |
| 错误隔离 | 列表加载错误不影响搜索框和创建按钮的正常使用 |
| 不自动重试 | 提交失败不自动重试，用户可修改表单后重新提交 |
| 列表加载失败 | 展示页面级错误提示 + 刷新按钮；保留页面结构 |
| 401 全局处理 | 由 axios 拦截器统一处理，各组件无需额外逻辑 |
| 后端消息透传 | 409/400 等场景优先展示后端返回的 message，提供更精准的错误原因 |
| 弹窗错误不关闭 | 提交失败后弹窗保持打开，保留用户已填写的数据 |
