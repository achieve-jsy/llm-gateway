# ops-console 接口契约变更 — CHG-20260523-001

> 基线：xspec/modules/ops-console/contracts.md (v1.1)
> 关联：design.delta.md / data-model.delta.md

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
