# control-plane Web 后端接口契约

## 1. 概览

**本契约描述**：control-plane 模块的 RESTful API 接口，包含渠道配置管理、模型注册管理（供 ops-console 消费）以及配置查询内部接口（供 gateway-api 消费）。

| 属性 | 内容 |
| :--- | :--- |
| **所属变更** | CHG-20260521-001 |
| **生产方（Provider）** | control-plane 服务 |
| **消费方（Consumer）** | ops-console（管理接口）、gateway-api（内部接口） |
| **当前版本** | `v1.1.0` |
| **最后更新** | 2026-05-23 |
| **修改人** | OpenCode |

---

## 2. 公共约定

> 适用于本契约所有端点的全局规则。

| 约定 | 内容 |
| :--- | :--- |
| **Base URL（管理接口）** | `/api/v1` |
| **Base URL（内部接口）** | `/internal/v1` |
| **数据格式** | JSON，UTF-8，Content-Type: `application/json` |
| **认证方式（管理接口）** | Bearer Token（JWT RS256），要求 `role: admin` |
| **认证方式（内部接口）** | Bearer Token（预共享内部 Token） |
| **时间戳格式** | ISO 8601 UTC（`2026-05-21T10:30:00Z`） |
| **ID 格式** | UUID v4（`550e8400-e29b-41d4-a716-446655440000`） |

**统一响应结构（成功）：**

```json
{
  "code": 0,
  "message": "success",
  "data": {},
  "request_id": "uuid"
}
```

**统一响应结构（列表查询）：**

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "items": [],
    "total": 100,
    "page": 1,
    "page_size": 20
  },
  "request_id": "uuid"
}
```

**幂等性定义**：相同请求多次执行，服务端状态与执行一次相同。

| 方法 | 默认幂等 |
| :---: | :---: |
| GET | 是 |
| PUT | 是（整体替换） |
| DELETE | 是 |
| POST | 否 |
| PATCH | 否 |

**分页约定**：offset-based 分页，参数 `page`（页码，从 1 开始）+ `page_size`（每页条数，默认 20，最大 100）。

---

## 3. 端点索引

| 端点 | 方法 | 路径 | 用途 | 认证 | 幂等 |
| :--- | :---: | :--- | :--- | :---: | :---: |
| 创建渠道 | POST | `/api/v1/providers` | 创建新的 LLM 渠道配置 | JWT | 否 |
| 渠道列表 | GET | `/api/v1/providers` | 分页查询渠道列表 | JWT | 是 |
| 渠道详情 | GET | `/api/v1/providers/:id` | 查询单个渠道详情（凭证脱敏） | JWT | 是 |
| 更新渠道 | PUT | `/api/v1/providers/:id` | 更新渠道配置信息 | JWT | 是 |
| 变更渠道状态 | PATCH | `/api/v1/providers/:id/status` | 启用或禁用渠道 | JWT | 否 |
| 删除渠道 | DELETE | `/api/v1/providers/:id` | 删除渠道（有模型时阻断） | JWT | 是 |
| 注册模型 | POST | `/api/v1/models` | 在指定渠道下注册模型 | JWT | 否 |
| 模型列表 | GET | `/api/v1/models` | 分页查询模型列表（支持按渠道过滤） | JWT | 是 |
| 更新模型 | PUT | `/api/v1/models/:id` | 更新模型配置 | JWT | 是 |
| 删除模型 | DELETE | `/api/v1/models/:id` | 删除模型注册 | JWT | 是 |
| 可用模型列表 | GET | `/internal/v1/models/available` | 返回所有启用模型及渠道信息 | 内部Token | 是 |
| 同步模型（拉取对比） | POST | `/api/v1/providers/:id/sync-models` | 从厂商拉取模型列表并与本地对比 | JWT | 否 |
| 确认同步（批量注册+禁用） | POST | `/api/v1/providers/:id/sync-models/confirm` | 确认批量注册选中模型并禁用已下架模型 | JWT | 否 |

---

## 4. 端点详细设计

### 4.1 创建渠道

* **接口描述**：创建新的 LLM 渠道配置，凭证将被加密存储。
* **请求方法**：`POST`
* **请求路径**：`/api/v1/providers`

#### 请求参数

| 参数名 | 类型 | 位置 | 必填 | 描述 | 示例 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| name | string | body | 是 | 渠道名称，2-64字符，唯一 | `"bailian-main"` |
| type | string | body | 是 | 渠道类型枚举 | `"bailian"` |
| endpoint | string | body | 是 | API 端点 URL | `"https://dashscope.aliyuncs.com/compatible-mode/v1"` |
| credentials | object | body | 是 | 凭证信息（JSON对象） | `{"api_key": "sk-xxx"}` |

**请求示例**：

```bash
curl -X POST \
  'https://control-plane.internal/api/v1/providers' \
  -H 'Authorization: Bearer {jwt_token}' \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "bailian-main",
    "type": "bailian",
    "endpoint": "https://dashscope.aliyuncs.com/compatible-mode/v1",
    "credentials": {
      "api_key": "sk-abcdefghijklmnop"
    }
  }'
```

#### 响应

**成功响应**（201 Created）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "name": "bailian-main",
    "type": "bailian",
    "endpoint": "https://dashscope.aliyuncs.com/compatible-mode/v1",
    "credential_hint": "***mnop",
    "enabled": true,
    "created_at": "2026-05-21T10:30:00Z",
    "updated_at": "2026-05-21T10:30:00Z"
  },
  "request_id": "req-uuid"
}
```

**响应参数说明**：

| 参数路径 | 类型 | 描述 |
| :--- | :--- | :--- |
| data.id | string(uuid) | 渠道唯一标识 |
| data.name | string | 渠道名称 |
| data.type | string | 渠道类型 |
| data.endpoint | string | API 端点 URL |
| data.credential_hint | string | 凭证脱敏提示（仅尾4位） |
| data.enabled | boolean | 是否启用 |
| data.created_at | string(datetime) | 创建时间 |
| data.updated_at | string(datetime) | 更新时间 |

**可能的错误码**：`E4001`（参数校验失败）、`E4011`（认证失败）、`E4091`（名称冲突）

---

### 4.2 渠道列表

* **接口描述**：分页查询渠道列表，支持按启用状态和类型过滤。
* **请求方法**：`GET`
* **请求路径**：`/api/v1/providers`

#### 请求参数

| 参数名 | 类型 | 位置 | 必填 | 描述 | 示例 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| page | integer | query | 否 | 页码（默认1） | `1` |
| page_size | integer | query | 否 | 每页条数（默认20，最大100） | `20` |
| enabled | boolean | query | 否 | 按启用状态过滤 | `true` |
| type | string | query | 否 | 按渠道类型过滤 | `"bailian"` |

**请求示例**：

```bash
curl -X GET \
  'https://control-plane.internal/api/v1/providers?page=1&page_size=20&enabled=true' \
  -H 'Authorization: Bearer {jwt_token}'
```

#### 响应

**成功响应**（200 OK）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "items": [
      {
        "id": "550e8400-e29b-41d4-a716-446655440000",
        "name": "bailian-main",
        "type": "bailian",
        "endpoint": "https://dashscope.aliyuncs.com/compatible-mode/v1",
        "credential_hint": "***mnop",
        "enabled": true,
        "created_at": "2026-05-21T10:30:00Z",
        "updated_at": "2026-05-21T10:30:00Z"
      }
    ],
    "total": 5,
    "page": 1,
    "page_size": 20
  },
  "request_id": "req-uuid"
}
```

**可能的错误码**：`E4001`（分页参数非法）、`E4011`（认证失败）

---

### 4.3 渠道详情

* **接口描述**：查询单个渠道详情，凭证脱敏返回。
* **请求方法**：`GET`
* **请求路径**：`/api/v1/providers/:id`

#### 请求参数

| 参数名 | 类型 | 位置 | 必填 | 描述 | 示例 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| id | string(uuid) | path | 是 | 渠道ID | `"550e8400-e29b-41d4-a716-446655440000"` |

**请求示例**：

```bash
curl -X GET \
  'https://control-plane.internal/api/v1/providers/550e8400-e29b-41d4-a716-446655440000' \
  -H 'Authorization: Bearer {jwt_token}'
```

#### 响应

**成功响应**（200 OK）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "name": "bailian-main",
    "type": "bailian",
    "endpoint": "https://dashscope.aliyuncs.com/compatible-mode/v1",
    "credential_hint": "***mnop",
    "enabled": true,
    "model_count": 3,
    "created_at": "2026-05-21T10:30:00Z",
    "updated_at": "2026-05-21T10:30:00Z"
  },
  "request_id": "req-uuid"
}
```

**响应参数说明**：

| 参数路径 | 类型 | 描述 |
| :--- | :--- | :--- |
| data.model_count | integer | 该渠道下注册的模型数量 |

**可能的错误码**：`E4011`（认证失败）、`E4041`（渠道不存在）

---

### 4.4 更新渠道

* **接口描述**：更新渠道配置信息，支持更新名称、端点、凭证。
* **请求方法**：`PUT`
* **请求路径**：`/api/v1/providers/:id`

#### 请求参数

| 参数名 | 类型 | 位置 | 必填 | 描述 | 示例 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| id | string(uuid) | path | 是 | 渠道ID | `"550e8400-..."` |
| name | string | body | 是 | 渠道名称 | `"bailian-main"` |
| type | string | body | 是 | 渠道类型 | `"bailian"` |
| endpoint | string | body | 是 | API 端点 URL | `"https://..."` |
| credentials | object | body | 否 | 凭证信息（不传则保持不变） | `{"api_key": "sk-new"}` |

**请求示例**：

```bash
curl -X PUT \
  'https://control-plane.internal/api/v1/providers/550e8400-e29b-41d4-a716-446655440000' \
  -H 'Authorization: Bearer {jwt_token}' \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "bailian-main",
    "type": "bailian",
    "endpoint": "https://dashscope.aliyuncs.com/compatible-mode/v1",
    "credentials": {
      "api_key": "sk-newkeyvalue1234"
    }
  }'
```

#### 响应

**成功响应**（200 OK）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "name": "bailian-main",
    "type": "bailian",
    "endpoint": "https://dashscope.aliyuncs.com/compatible-mode/v1",
    "credential_hint": "***1234",
    "enabled": true,
    "created_at": "2026-05-21T10:30:00Z",
    "updated_at": "2026-05-21T11:00:00Z"
  },
  "request_id": "req-uuid"
}
```

**可能的错误码**：`E4001`（参数校验失败）、`E4011`（认证失败）、`E4041`（渠道不存在）、`E4091`（名称冲突）

---

### 4.5 变更渠道状态

* **接口描述**：启用或禁用渠道。禁用时级联禁用其下所有模型。
* **请求方法**：`PATCH`
* **请求路径**：`/api/v1/providers/:id/status`

#### 请求参数

| 参数名 | 类型 | 位置 | 必填 | 描述 | 示例 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| id | string(uuid) | path | 是 | 渠道ID | `"550e8400-..."` |
| enabled | boolean | body | 是 | 目标状态 | `false` |

**请求示例**：

```bash
curl -X PATCH \
  'https://control-plane.internal/api/v1/providers/550e8400-e29b-41d4-a716-446655440000/status' \
  -H 'Authorization: Bearer {jwt_token}' \
  -H 'Content-Type: application/json' \
  -d '{"enabled": false}'
```

#### 响应

**成功响应**（200 OK）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "name": "bailian-main",
    "type": "bailian",
    "endpoint": "https://dashscope.aliyuncs.com/compatible-mode/v1",
    "credential_hint": "***mnop",
    "enabled": false,
    "created_at": "2026-05-21T10:30:00Z",
    "updated_at": "2026-05-21T12:00:00Z"
  },
  "request_id": "req-uuid"
}
```

**说明**：禁用渠道时，该渠道下所有模型的 `enabled` 字段自动设为 `false`。启用渠道时不会自动恢复模型状态（需逐个启用）。

**可能的错误码**：`E4001`（参数校验失败）、`E4011`（认证失败）、`E4041`（渠道不存在）

---

### 4.6 删除渠道

* **接口描述**：删除渠道配置。如果渠道下仍有注册模型，则拒绝删除。
* **请求方法**：`DELETE`
* **请求路径**：`/api/v1/providers/:id`

#### 请求参数

| 参数名 | 类型 | 位置 | 必填 | 描述 | 示例 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| id | string(uuid) | path | 是 | 渠道ID | `"550e8400-..."` |

**请求示例**：

```bash
curl -X DELETE \
  'https://control-plane.internal/api/v1/providers/550e8400-e29b-41d4-a716-446655440000' \
  -H 'Authorization: Bearer {jwt_token}'
```

#### 响应

**成功响应**（200 OK）：

```json
{
  "code": 0,
  "message": "success",
  "data": null,
  "request_id": "req-uuid"
}
```

**可能的错误码**：`E4011`（认证失败）、`E4041`（渠道不存在）、`E4092`（渠道下有模型，不可删除）

---

### 4.7 注册模型

* **接口描述**：在指定渠道下注册一个模型。模型名称全局唯一，不可与已有模型重名。
* **请求方法**：`POST`
* **请求路径**：`/api/v1/models`

#### 请求参数

| 参数名 | 类型 | 位置 | 必填 | 描述 | 示例 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| name | string | body | 是 | 模型名称，2-128字符，全局唯一 | `"qwen-turbo"` |
| provider_id | string(uuid) | body | 是 | 所属渠道ID | `"550e8400-..."` |
| model_type | string | body | 是 | 模型类型枚举 | `"chat"` |

**请求示例**：

```bash
curl -X POST \
  'https://control-plane.internal/api/v1/models' \
  -H 'Authorization: Bearer {jwt_token}' \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "qwen-turbo",
    "provider_id": "550e8400-e29b-41d4-a716-446655440000",
    "model_type": "chat"
  }'
```

#### 响应

**成功响应**（201 Created）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": "660e8400-e29b-41d4-a716-446655440001",
    "name": "qwen-turbo",
    "provider_id": "550e8400-e29b-41d4-a716-446655440000",
    "provider_name": "bailian-main",
    "model_type": "chat",
    "enabled": true,
    "created_at": "2026-05-21T10:35:00Z",
    "updated_at": "2026-05-21T10:35:00Z"
  },
  "request_id": "req-uuid"
}
```

**响应参数说明**：

| 参数路径 | 类型 | 描述 |
| :--- | :--- | :--- |
| data.id | string(uuid) | 模型唯一标识 |
| data.name | string | 模型名称 |
| data.provider_id | string(uuid) | 所属渠道ID |
| data.provider_name | string | 所属渠道名称（便于展示） |
| data.model_type | string | 模型类型（chat/embedding） |
| data.enabled | boolean | 是否启用 |
| data.created_at | string(datetime) | 创建时间 |
| data.updated_at | string(datetime) | 更新时间 |

**可能的错误码**：`E4001`（参数校验失败）、`E4011`（认证失败）、`E4041`（渠道不存在）、`E4091`（模型名称冲突）、`E4221`（渠道已禁用）

---

### 4.8 模型列表

* **接口描述**：分页查询模型列表，支持按渠道ID和模型类型过滤。
* **请求方法**：`GET`
* **请求路径**：`/api/v1/models`

#### 请求参数

| 参数名 | 类型 | 位置 | 必填 | 描述 | 示例 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| page | integer | query | 否 | 页码（默认1） | `1` |
| page_size | integer | query | 否 | 每页条数（默认20，最大100） | `20` |
| provider_id | string(uuid) | query | 否 | 按渠道ID过滤 | `"550e8400-..."` |
| model_type | string | query | 否 | 按模型类型过滤 | `"chat"` |
| enabled | boolean | query | 否 | 按启用状态过滤 | `true` |

**请求示例**：

```bash
curl -X GET \
  'https://control-plane.internal/api/v1/models?page=1&page_size=20&provider_id=550e8400-e29b-41d4-a716-446655440000' \
  -H 'Authorization: Bearer {jwt_token}'
```

#### 响应

**成功响应**（200 OK）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "items": [
      {
        "id": "660e8400-e29b-41d4-a716-446655440001",
        "name": "qwen-turbo",
        "provider_id": "550e8400-e29b-41d4-a716-446655440000",
        "provider_name": "bailian-main",
        "model_type": "chat",
        "enabled": true,
        "created_at": "2026-05-21T10:35:00Z",
        "updated_at": "2026-05-21T10:35:00Z"
      }
    ],
    "total": 15,
    "page": 1,
    "page_size": 20
  },
  "request_id": "req-uuid"
}
```

**可能的错误码**：`E4001`（分页参数非法）、`E4011`（认证失败）

---

### 4.9 更新模型

* **接口描述**：更新模型配置信息。
* **请求方法**：`PUT`
* **请求路径**：`/api/v1/models/:id`

#### 请求参数

| 参数名 | 类型 | 位置 | 必填 | 描述 | 示例 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| id | string(uuid) | path | 是 | 模型ID | `"660e8400-..."` |
| name | string | body | 是 | 模型名称 | `"qwen-turbo"` |
| provider_id | string(uuid) | body | 是 | 所属渠道ID | `"550e8400-..."` |
| model_type | string | body | 是 | 模型类型 | `"chat"` |
| enabled | boolean | body | 是 | 是否启用 | `true` |

**请求示例**：

```bash
curl -X PUT \
  'https://control-plane.internal/api/v1/models/660e8400-e29b-41d4-a716-446655440001' \
  -H 'Authorization: Bearer {jwt_token}' \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "qwen-turbo",
    "provider_id": "550e8400-e29b-41d4-a716-446655440000",
    "model_type": "chat",
    "enabled": true
  }'
```

#### 响应

**成功响应**（200 OK）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": "660e8400-e29b-41d4-a716-446655440001",
    "name": "qwen-turbo",
    "provider_id": "550e8400-e29b-41d4-a716-446655440000",
    "provider_name": "bailian-main",
    "model_type": "chat",
    "enabled": true,
    "created_at": "2026-05-21T10:35:00Z",
    "updated_at": "2026-05-21T14:00:00Z"
  },
  "request_id": "req-uuid"
}
```

**可能的错误码**：`E4001`（参数校验失败）、`E4011`（认证失败）、`E4041`（模型不存在）、`E4091`（名称冲突）、`E4221`（目标渠道已禁用）

---

### 4.10 删除模型

* **接口描述**：删除模型注册。
* **请求方法**：`DELETE`
* **请求路径**：`/api/v1/models/:id`

#### 请求参数

| 参数名 | 类型 | 位置 | 必填 | 描述 | 示例 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| id | string(uuid) | path | 是 | 模型ID | `"660e8400-..."` |

**请求示例**：

```bash
curl -X DELETE \
  'https://control-plane.internal/api/v1/models/660e8400-e29b-41d4-a716-446655440001' \
  -H 'Authorization: Bearer {jwt_token}'
```

#### 响应

**成功响应**（200 OK）：

```json
{
  "code": 0,
  "message": "success",
  "data": null,
  "request_id": "req-uuid"
}
```

**可能的错误码**：`E4011`（认证失败）、`E4041`（模型不存在）

---

### 4.11 可用模型列表（内部接口）

* **接口描述**：返回所有启用的模型及其所属渠道的完整信息（含解密后的凭证），供 gateway-api 构建路由表使用。
* **请求方法**：`GET`
* **请求路径**：`/internal/v1/models/available`

#### 请求参数

无请求参数。

**请求示例**：

```bash
curl -X GET \
  'https://control-plane.internal/internal/v1/models/available' \
  -H 'Authorization: Bearer {internal_token}'
```

#### 响应

**成功响应**（200 OK）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "items": [
      {
        "model_name": "qwen-turbo",
        "model_type": "chat",
        "provider": {
          "id": "550e8400-e29b-41d4-a716-446655440000",
          "name": "bailian-main",
          "type": "bailian",
          "endpoint": "https://dashscope.aliyuncs.com/compatible-mode/v1",
          "credentials": {
            "api_key": "sk-abcdefghijklmnop"
          }
        }
      },
      {
        "model_name": "text-embedding-v3",
        "model_type": "embedding",
        "provider": {
          "id": "550e8400-e29b-41d4-a716-446655440000",
          "name": "bailian-main",
          "type": "bailian",
          "endpoint": "https://dashscope.aliyuncs.com/compatible-mode/v1",
          "credentials": {
            "api_key": "sk-abcdefghijklmnop"
          }
        }
      }
    ],
    "updated_at": "2026-05-21T12:00:00Z"
  },
  "request_id": "req-uuid"
}
```

**响应参数说明**：

| 参数路径 | 类型 | 描述 |
| :--- | :--- | :--- |
| data.items[].model_name | string | 模型名称（gateway-api 路由匹配键） |
| data.items[].model_type | string | 模型类型（chat/embedding） |
| data.items[].provider.id | string(uuid) | 渠道ID |
| data.items[].provider.name | string | 渠道名称 |
| data.items[].provider.type | string | 渠道类型 |
| data.items[].provider.endpoint | string | API 端点 URL |
| data.items[].provider.credentials | object | 解密后的完整凭证（仅内部接口返回） |
| data.updated_at | string(datetime) | 配置最后更新时间（用于缓存判断） |

**安全说明**：此接口返回完整凭证明文，仅供内部可信服务调用，通过预共享 Token 认证 + 网络隔离保护。

**可能的错误码**：`E4011`（认证失败）、`E5001`（凭证解密失败等内部错误）

---

### 4.12 同步模型（拉取对比）

* **接口描述**：从上游厂商拉取模型列表并与本地已注册模型对比，返回分类结果（新增/已存在/已下架）。
* **请求方法**：`POST`
* **请求路径**：`/api/v1/providers/:id/sync-models`

#### 请求参数

| 参数名 | 类型 | 位置 | 必填 | 描述 | 示例 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| id | string(uuid) | path | 是 | 渠道ID | `"550e8400-..."` |

**请求示例**：

```bash
curl -X POST \
  'https://control-plane.internal/api/v1/providers/550e8400-e29b-41d4-a716-446655440000/sync-models' \
  -H 'Authorization: Bearer {jwt_token}'
```

#### 响应

**成功响应**（200 OK）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "added": [
      {"id": "qwen-max", "owned_by": "dashscope"}
    ],
    "existing": [
      {"id": "qwen-turbo", "owned_by": "dashscope"}
    ],
    "removed": [
      {"id": "old-model", "owned_by": "dashscope"}
    ],
    "total_upstream": 244,
    "total_local": 5,
    "unsupported": false
  },
  "request_id": "req-uuid"
}
```

**响应参数说明**：

| 参数路径 | 类型 | 描述 |
| :--- | :--- | :--- |
| data.added | array | 上游有但本地无的模型列表（待注册） |
| data.added[].id | string | 上游模型标识 |
| data.added[].owned_by | string | 上游模型归属 |
| data.existing | array | 上游和本地都存在的模型列表 |
| data.removed | array | 本地有但上游不存在的模型列表（已下架） |
| data.total_upstream | integer | 上游模型总数 |
| data.total_local | integer | 本地已注册模型数 |
| data.unsupported | boolean | 该渠道类型是否不支持自动同步 |

**可能的错误码**：`E4011`（认证失败）、`E4041`（渠道不存在）、`E4222`（渠道类型不支持自动同步）、`E5021`（厂商 API 调用失败）

---

### 4.13 确认同步（批量注册+禁用）

* **接口描述**：确认批量注册选中的模型，同时禁用已下架模型。所有新注册模型默认 enabled=false。
* **请求方法**：`POST`
* **请求路径**：`/api/v1/providers/:id/sync-models/confirm`

#### 请求参数

| 参数名 | 类型 | 位置 | 必填 | 描述 | 示例 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| id | string(uuid) | path | 是 | 渠道ID | `"550e8400-..."` |
| models_to_add | string[] | body | 是 | 要注册的模型名称列表 | `["qwen-max", "qwen-plus"]` |
| disable_removed | boolean | body | 否 | 是否禁用已下架模型，默认 true | `true` |

**请求示例**：

```bash
curl -X POST \
  'https://control-plane.internal/api/v1/providers/550e8400-e29b-41d4-a716-446655440000/sync-models/confirm' \
  -H 'Authorization: Bearer {jwt_token}' \
  -H 'Content-Type: application/json' \
  -d '{
    "models_to_add": ["qwen-max", "qwen-plus", "qwen-vl-max"],
    "disable_removed": true
  }'
```

#### 响应

**成功响应**（200 OK）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "registered": 3,
    "skipped": 0,
    "disabled": 2
  },
  "request_id": "req-uuid"
}
```

**响应参数说明**：

| 参数路径 | 类型 | 描述 |
| :--- | :--- | :--- |
| data.registered | integer | 成功注册的模型数量 |
| data.skipped | integer | 因重名跳过的模型数量 |
| data.disabled | integer | 因下架被禁用的模型数量 |

**可能的错误码**：`E4001`（参数校验失败，models_to_add 为空）、`E4011`（认证失败）、`E4041`（渠道不存在）

---

## 5. 错误码

### 5.1 统一错误响应结构

> 所有错误统一格式，端点设计中只引用错误码。

```json
{
  "code": -1,
  "error_code": "E4001",
  "message": "请求参数校验失败：name 字段为必填项",
  "request_id": "550e8400-e29b-41d4-a716-446655440099"
}
```

### 5.2 通用错误码列表

| 错误码 | HTTP 状态 | 语义 | 触发条件 | 可重试 |
| :--- | :---: | :--- | :--- | :---: |
| E4001 | 400 | 请求参数校验失败 | 缺少必填字段、字段格式不合法、枚举值非法 | 否 |
| E4011 | 401 | 认证失败 | Token 缺失、过期、签名验证失败 | 否（需重新登录） |
| E4041 | 404 | 资源不存在 | 指定 ID 的渠道或模型不存在 | 否 |
| E4091 | 409 | 名称冲突 | 渠道名称或模型名称已存在 | 否（需换名） |
| E4092 | 409 | 存在关联资源 | 删除渠道时其下仍有注册模型 | 否（需先删模型） |
| E4221 | 422 | 业务规则校验失败 | 在已禁用的渠道下注册模型 | 否（需先启用渠道） |
| E4222 | 400 | 渠道类型不支持自动同步 | 渠道类型（如 Anthropic/Bedrock）不支持模型列表接口 | 否 |
| E5021 | 502 | 厂商 API 调用失败 | 厂商接口超时、网络错误或返回异常 | 是（可重试） |
| E4291 | 429 | 请求频率超限 | 超过限流阈值 | 是（等待 Retry-After） |
| E5001 | 500 | 内部服务错误 | 未预期异常（数据库错误、加密失败等） | 是（可重试） |

### 5.3 业务错误码补充说明

| 错误码 | 场景 | message 示例 |
| :--- | :--- | :--- |
| E4091 | 创建渠道时名称重复 | `"渠道名称 'bailian-main' 已存在"` |
| E4091 | 注册模型时名称重复 | `"模型名称 'qwen-turbo' 已存在"` |
| E4092 | 删除渠道时存在模型 | `"渠道下仍有 3 个注册模型，请先移除"` |
| E4221 | 在禁用渠道下注册模型 | `"渠道 'bailian-main' 当前已禁用，无法注册模型"` |
| E4222 | 同步模型时渠道类型不支持 | `"该渠道类型不支持自动同步，请使用预置模型列表"` |
| E5021 | 同步模型时厂商 API 调用失败 | `"厂商 API 调用超时"` |

---

## 6. 版本策略与 Breaking Change 规则

**当前版本**：`v1`（体现在 URL 路径前缀 `/api/v1` 和 `/internal/v1`）

**Breaking Change（必须升大版本）：**
- 删除已有端点
- 删除响应中的已有字段
- 修改字段类型或语义
- 修改现有错误码的含义
- 新增请求必填字段
- 修改 URL 路径结构

**非 Breaking Change（可在当前版本直接发布）：**
- 新增端点
- 新增响应中的可选字段
- 新增新的错误码（不修改已有）
- 新增查询参数（可选）
- 新增枚举值（如新的 provider type）

**废弃流程**：
1. 在响应 Header 中加 `Deprecated: true` 及 `Sunset: <日期>`
2. 保留至少 3 个月后方可移除
3. 移除前通过内部公告通知所有已知消费方（ops-console、gateway-api 团队）

---

## 7. 非功能约束（适用时填）

| 约束类型 | 值 | 超出时的行为 |
| :--- | :--- | :--- |
| 管理接口请求频率上限 | 60 次/分钟/IP | 返回 E4291 + `Retry-After` 头 |
| 内部接口请求频率上限 | 不限制（信任的内部服务） | — |
| 单次请求超时 | 30s（网关层控制） | 返回 504 Gateway Timeout |
| 请求体最大大小 | 1 MB | 返回 E4001 |
| 列表查询最大 page_size | 100 | 超过时截断为 100 |

---

## 8. 接口安全

### 8.1 认证方式

**管理接口（/api/v1/*）：**
- 认证方式：JWT Bearer Token（RS256 签名）
- Token 来源：ops-console 从公司统一身份平台获取
- 验证逻辑：control-plane 使用公钥验证签名 + 过期时间 + role 字段包含 `admin`
- Token 传递：`Authorization: Bearer {jwt_token}`

**内部接口（/internal/v1/*）：**
- 认证方式：预共享 Token
- Token 来源：部署时通过环境变量配置，gateway-api 和 control-plane 使用相同的 Token
- 验证逻辑：直接比对 Token 字符串
- Token 传递：`Authorization: Bearer {internal_token}`

### 8.2 安全措施

* **HTTPS 强制**：所有外部调用必须通过 HTTPS；内部服务间通信走服务网格 mTLS
* **输入验证**：所有请求参数通过 Gin Binding + validator 标签严格校验，拒绝非法输入
* **凭证保护**：管理接口永远不返回完整凭证；内部接口通过网络隔离和 Token 认证双重保护
* **速率限制**：管理接口按 IP 限流，防止暴力攻击和滥用
* **日志脱敏**：请求/响应日志中不记录 credentials 字段和 Authorization 头的完整内容
* **错误信息**：500 错误不暴露内部堆栈和数据库细节，仅返回通用错误信息

---

## 9. 变更历史

| 版本号 | 更新时间 | 变更类型 | 变更描述 | 变更人 |
| :--- | :--- | :---: | :--- | :--- |
| v1.0.0 | 2026-05-21 | — | 初始版本：渠道 CRUD + 模型 CRUD + 内部查询接口 | OpenCode |
| v1.1.0 | 2026-05-23 | 新增 | 新增同步模型接口（sync-models + confirm），新增错误码 E4222、E5021 | OpenCode |
