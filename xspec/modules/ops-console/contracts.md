# ops-console 接口契约

## 1. 概述

| 属性 | 内容 |
| :--- | :--- |
| **所属变更** | CHG-20260521-001 |
| **模块名称** | ops-console |
| **模块类型** | web-frontend |
| **创建时间** | 2026-05-21 |
| **修改人** | OpenCode |

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
