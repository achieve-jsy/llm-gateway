# control-plane Web 后端接口契约

## 1. 概览

**本契约描述**：control-plane 模块的 RESTful API 接口，包含渠道配置管理、模型注册管理（供 ops-console 消费）以及配置查询内部接口（供 gateway-api 消费）。

| 属性 | 内容 |
| :--- | :--- |
| **所属变更** | CHG-20260521-001 |
| **生产方（Provider）** | control-plane 服务 |
| **消费方（Consumer）** | ops-console（管理接口）、gateway-api（内部接口） |
| **当前版本** | `v1.1.0` |
| **最后更新** | 2026-05-25 |
| **修改人** | OpenCode |

### 修订记录

| 版本号 | 修改日期 | 修改内容摘要 | 修改人 |
| :--- | :--- | :--- | :--- |
| v1.2.0 | 2026-05-25 | 新增 Key 管理端点（6 个）及内部 Key 验证端点（1 个）— CHG-20260523-001 | OpenCode |
| v1.3.0 | 2026-05-26 | 新增模型组管理 9 个端点 + 故障切换配置 — CHG-20260525-001 | OpenCode |

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

---

## 10. 变更记录 — CHG-20260523-001

## 变更摘要

新增 Key 管理端点（6 个）供 ops-console 消费（JWT 认证）及内部 Key 验证端点（1 个）供 auth 消费（预共享 Token 认证）。新增错误码 E4093（key_name_conflict）、E4042（key_not_found）、E4031（key_forbidden）、E4223（key_limit_exceeded）、E4094（key_already_disabled）、E4095（key_already_enabled）。现有端点、错误码、认证方式均不受影响。

---

## 1. 版本信息变更

| 属性 | 旧值 | 新值 |
| :--- | :--- | :--- |
| **当前版本** | v1.1.0 | v1.2.0 |
| **最后更新** | 2026-05-23 | 2026-05-23 |

---

## 2. 端点索引变更

> 在现有 13 个端点基础上新增 7 个端点，按非 Breaking Change 规则在当前版本 v1 下发布。

### 2.1 新增端点

| 端点 | 方法 | 路径 | 用途 | 认证 | 幂等 |
| :--- | :---: | :--- | :--- | :---: | :---: |
| 创建 Key | POST | `/api/v1/keys` | 员工自助创建 API Key（明文仅返回一次） | JWT | 否 |
| Key 列表 | GET | `/api/v1/keys` | 分页查询当前用户自有 Key（Key 值脱敏） | JWT | 是 |
| Key 详情 | GET | `/api/v1/keys/:id` | 查询单个 Key 详情（Key 值脱敏） | JWT | 是 |
| 禁用 Key | PATCH | `/api/v1/keys/:id/disable` | 禁用自有 Key，立即生效 | JWT | 是 |
| 启用 Key | PATCH | `/api/v1/keys/:id/enable` | 启用自有 Key，立即生效 | JWT | 是 |
| 删除 Key | DELETE | `/api/v1/keys/:id` | 永久删除自有 Key | JWT | 是 |
| Key 验证（内部） | POST | `/internal/v1/keys/verify` | 验证 Key 有效性，返回身份链路 | 内部Token | 是 |

---

## 3. 新增端点详细设计

### 3.1 创建 Key

* **接口描述**：员工自助创建 API Key。Key 明文仅在本次响应中返回一次，后续任何接口不提供明文。Key 以 AES-256-GCM 加密存储，SHA-256 hash 用于高效查找。Key 格式：`sk-` + 32 位安全随机字符。
* **请求方法**：`POST`
* **请求路径**：`/api/v1/keys`

#### 请求参数

| 参数名 | 类型 | 位置 | 必填 | 描述 | 示例 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| name | string | body | 是 | Key 名称标识，长度 2–64，仅允许字母数字下划线连字符 | `"personal-key"` |

> `user_id` 从 JWT 中提取，不可由请求方指定。

**请求示例**：

```bash
curl -X POST \
  'https://control-plane.internal/api/v1/keys' \
  -H 'Authorization: Bearer {jwt_token}' \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "personal-key"
  }'
```

#### 响应

**成功响应**（201 Created）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": "880e8400-e29b-41d4-a716-446655440002",
    "name": "personal-key",
    "key": "sk-a1B2c3D4e5F6g7H8i9J0k1L2m3N4o5P6",
    "key_hint": "o5P6",
    "enabled": true,
    "created_at": "2026-05-23T14:00:00Z",
    "updated_at": "2026-05-23T14:00:00Z"
  },
  "request_id": "req-uuid"
}
```

**响应参数说明**：

| 参数路径 | 类型 | 描述 |
| :--- | :--- | :--- |
| data.id | string(uuid) | Key 唯一标识 |
| data.name | string | Key 名称 |
| data.key | string | **Key 明文（仅此一次返回）**，格式 `sk-` + 32 位字符 |
| data.key_hint | string | Key 末尾 4 位，用于后续脱敏展示参考 |
| data.enabled | boolean | 是否启用，创建时默认 true |
| data.created_at | string(datetime) | 创建时间 |
| data.updated_at | string(datetime) | 更新时间 |

> **安全提示**：`data.key` 字段包含完整 Key 明文，**仅在本次响应中出现**。请妥善保管，后续任何接口不再返回 Key 明文。如遗失明文 Key，需删除旧 Key 并重新创建。

**可能的错误码**：`E4001`（参数校验失败）、`E4011`（认证失败）、`E4093`（同用户下名称冲突）、`E4223`（Key 数量已达上限）

---

### 3.2 Key 列表

* **接口描述**：分页查询当前登录用户自有 Key 列表。Key 值以脱敏形式展示（仅保留末尾 4 位），不返回完整明文。
* **请求方法**：`GET`
* **请求路径**：`/api/v1/keys`

#### 请求参数

| 参数名 | 类型 | 位置 | 必填 | 描述 | 示例 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| page | integer | query | 否 | 页码（默认 1） | `1` |
| page_size | integer | query | 否 | 每页条数（默认 20，最大 100） | `20` |
| enabled | boolean | query | 否 | 按启用状态过滤 | `true` |

> 仅返回当前 JWT 中 `user_id` 名下的 Key，不可通过参数指定查询其他用户。

**请求示例**：

```bash
curl -X GET \
  'https://control-plane.internal/api/v1/keys?page=1&page_size=20' \
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
        "id": "880e8400-e29b-41d4-a716-446655440002",
        "name": "personal-key",
        "masked_key": "****o5P6",
        "key_hint": "o5P6",
        "enabled": true,
        "created_at": "2026-05-23T14:00:00Z",
        "updated_at": "2026-05-23T14:00:00Z"
      },
      {
        "id": "990e8400-e29b-41d4-a716-446655440003",
        "name": "backup-key",
        "masked_key": "****XY12",
        "key_hint": "XY12",
        "enabled": false,
        "created_at": "2026-05-22T10:00:00Z",
        "updated_at": "2026-05-23T12:30:00Z"
      }
    ],
    "total": 2,
    "page": 1,
    "page_size": 20
  },
  "request_id": "req-uuid"
}
```

**响应参数说明**：

| 参数路径 | 类型 | 描述 |
| :--- | :--- | :--- |
| data.items[].id | string(uuid) | Key 唯一标识 |
| data.items[].name | string | Key 名称 |
| data.items[].masked_key | string | 脱敏 Key 值，格式 `"****"` + 末尾 4 位，如 `"****o5P6"` |
| data.items[].key_hint | string | Key 末尾 4 位（用于 UI 定位区分） |
| data.items[].enabled | boolean | 启用状态（true=启用, false=禁用） |
| data.items[].created_at | string(datetime) | 创建时间 |
| data.items[].updated_at | string(datetime) | 更新时间 |

**可能的错误码**：`E4001`（分页参数非法）、`E4011`（认证失败）

---

### 3.3 Key 详情

* **接口描述**：查询单个 Key 详情（仅限自有 Key）。Key 值以脱敏形式展示。
* **请求方法**：`GET`
* **请求路径**：`/api/v1/keys/:id`

#### 请求参数

| 参数名 | 类型 | 位置 | 必填 | 描述 | 示例 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| id | string(uuid) | path | 是 | Key ID | `"880e8400-e29b-41d4-a716-446655440002"` |

**请求示例**：

```bash
curl -X GET \
  'https://control-plane.internal/api/v1/keys/880e8400-e29b-41d4-a716-446655440002' \
  -H 'Authorization: Bearer {jwt_token}'
```

#### 响应

**成功响应**（200 OK）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": "880e8400-e29b-41d4-a716-446655440002",
    "name": "personal-key",
    "masked_key": "****o5P6",
    "key_hint": "o5P6",
    "enabled": true,
    "created_at": "2026-05-23T14:00:00Z",
    "updated_at": "2026-05-23T14:00:00Z"
  },
  "request_id": "req-uuid"
}
```

**可能的错误码**：`E4011`（认证失败）、`E4031`（非自有 Key）、`E4042`（Key 不存在）

---

### 3.4 禁用 Key

* **接口描述**：禁用自有 Key。禁用后该 Key 立即不可用于任何 API 调用，gateway-api 在鉴权环节拒绝。
* **请求方法**：`PATCH`
* **请求路径**：`/api/v1/keys/:id/disable`

#### 请求参数

| 参数名 | 类型 | 位置 | 必填 | 描述 | 示例 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| id | string(uuid) | path | 是 | Key ID | `"880e8400-e29b-41d4-a716-446655440002"` |

**请求示例**：

```bash
curl -X PATCH \
  'https://control-plane.internal/api/v1/keys/880e8400-e29b-41d4-a716-446655440002/disable' \
  -H 'Authorization: Bearer {jwt_token}'
```

#### 响应

**成功响应**（200 OK）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": "880e8400-e29b-41d4-a716-446655440002",
    "name": "personal-key",
    "masked_key": "****o5P6",
    "key_hint": "o5P6",
    "enabled": false,
    "created_at": "2026-05-23T14:00:00Z",
    "updated_at": "2026-05-23T15:00:00Z"
  },
  "request_id": "req-uuid"
}
```

**幂等行为**：对已处于禁用状态的 Key 再次调用，返回 200 OK 且状态不变，message 为 `"Key 已处于禁用状态"`。

**可能的错误码**：`E4011`（认证失败）、`E4031`（非自有 Key）、`E4042`（Key 不存在）、`E4094`（Key 已处于禁用状态）

---

### 3.5 启用 Key

* **接口描述**：启用自有 Key。启用后该 Key 恢复可用于 API 调用。
* **请求方法**：`PATCH`
* **请求路径**：`/api/v1/keys/:id/enable`

#### 请求参数

| 参数名 | 类型 | 位置 | 必填 | 描述 | 示例 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| id | string(uuid) | path | 是 | Key ID | `"880e8400-e29b-41d4-a716-446655440002"` |

**请求示例**：

```bash
curl -X PATCH \
  'https://control-plane.internal/api/v1/keys/880e8400-e29b-41d4-a716-446655440002/enable' \
  -H 'Authorization: Bearer {jwt_token}'
```

#### 响应

**成功响应**（200 OK）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": "880e8400-e29b-41d4-a716-446655440002",
    "name": "personal-key",
    "masked_key": "****o5P6",
    "key_hint": "o5P6",
    "enabled": true,
    "created_at": "2026-05-23T14:00:00Z",
    "updated_at": "2026-05-23T15:05:00Z"
  },
  "request_id": "req-uuid"
}
```

**幂等行为**：对已处于启用状态的 Key 再次调用，返回 200 OK 且状态不变，message 为 `"Key 已处于启用状态"`。

**可能的错误码**：`E4011`（认证失败）、`E4031`（非自有 Key）、`E4042`（Key 不存在）、`E4095`（Key 已处于启用状态）

---

### 3.6 删除 Key

* **接口描述**：永久删除自有 Key。删除后该 Key 不可恢复，通过该 Key 的任何后续调用均返回 Key 不存在。
* **请求方法**：`DELETE`
* **请求路径**：`/api/v1/keys/:id`

#### 请求参数

| 参数名 | 类型 | 位置 | 必填 | 描述 | 示例 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| id | string(uuid) | path | 是 | Key ID | `"880e8400-e29b-41d4-a716-446655440002"` |

**请求示例**：

```bash
curl -X DELETE \
  'https://control-plane.internal/api/v1/keys/880e8400-e29b-41d4-a716-446655440002' \
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

**幂等行为**：对已删除 Key 再次调用，返回 404 E4042 `"Key 不存在或已被删除"`。

**可能的错误码**：`E4011`（认证失败）、`E4031`（非自有 Key）、`E4042`（Key 不存在）

---

### 3.7 Key 验证（内部接口）

* **接口描述**：验证 API Key 的有效性，返回 Key 状态及用户身份链路（API Key → User → Department）。仅供 auth 内部调用，不对公网暴露。
* **请求方法**：`POST`
* **请求路径**：`/internal/v1/keys/verify`

#### 请求参数

| 参数名 | 类型 | 位置 | 必填 | 描述 | 示例 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| key | string | body | 是 | 需要验证的 API Key 明文 | `"sk-a1B2c3D4e5F6g7H8i9J0k1L2m3N4o5P6"` |

**请求示例**：

```bash
curl -X POST \
  'https://control-plane.internal/internal/v1/keys/verify' \
  -H 'Authorization: Bearer {internal_token}' \
  -H 'Content-Type: application/json' \
  -d '{
    "key": "sk-a1B2c3D4e5F6g7H8i9J0k1L2m3N4o5P6"
  }'
```

#### 响应

**成功响应 — Key 有效**（200 OK）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "key_id": "880e8400-e29b-41d4-a716-446655440002",
    "status": "active",
    "user": {
      "user_id": "770e8400-e29b-41d4-a716-446655440001",
      "name": "张三",
      "department_id": "660e8400-e29b-41d4-a716-446655440000"
    }
  },
  "request_id": "req-uuid"
}
```

**成功响应 — Key 已禁用**（200 OK）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "key_id": "880e8400-e29b-41d4-a716-446655440002",
    "status": "disabled",
    "user": null
  },
  "request_id": "req-uuid"
}
```

**成功响应 — Key 不存在**（200 OK）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "status": "not_found"
  },
  "request_id": "req-uuid"
}
```

**响应参数说明**：

| 参数路径 | 类型 | 描述 |
| :--- | :--- | :--- |
| data.key_id | string(uuid) | Key 唯一标识（status 为 active 或 disabled 时返回） |
| data.status | string | Key 状态枚举：`active`（有效）、`disabled`（已禁用）、`not_found`（不存在） |
| data.user.user_id | string(uuid) | 绑定用户 ID |
| data.user.name | string | 用户名称 |
| data.user.department_id | string(uuid) | 所属部门 ID |

> **内部接口约定**：
> - 此接口仅对 auth 暴露，通过网络隔离和预共享 Token 双重保护。
> - `status` 为 `disabled` 或 `not_found` 时不返回 `user` 对象（或为 null）。
> - auth 应缓存 `active` 状态的结果（建议 TTL ≤ 60s），以降低内部调用频率。
> - `status` 由调用方决定是否放行，control-plane 仅提供验证结果不做策略决策。

**可能的错误码**：`E4001`（key 参数缺失或格式非法）、`E4011`（内部 Token 认证失败）、`E5001`（数据库查询异常等内部错误）

---

## 4. 错误码变更

### 4.1 新增错误码

| 错误码 | HTTP 状态 | 语义 | 触发条件 | 可重试 |
| :--- | :---: | :--- | :--- | :---: |
| E4031 | 403 | Key 操作权限拒绝 | 尝试操作非自有 Key（user_id 不匹配） | 否 |
| E4042 | 404 | Key 不存在 | 指定 ID 的 Key 不存在或已被删除 | 否 |
| E4093 | 409 | Key 名称冲突 | 同用户下 Key 名称已存在 | 否（需换名） |
| E4094 | 409 | Key 已处于禁用状态 | 对已禁用的 Key 再次执行禁用操作 | 否 |
| E4095 | 409 | Key 已处于启用状态 | 对已启用的 Key 再次执行启用操作 | 否 |
| E4223 | 422 | Key 数量已达上限 | 用户启用 Key 数量超过配置上限（默认 10） | 否（需先禁用或删除其他 Key） |

### 4.2 业务错误码补充说明

| 错误码 | 场景 | message 示例 |
| :--- | :--- | :--- |
| E4031 | 操作他人 Key | `"无权操作该 Key"` |
| E4042 | Key 不存在或已删除 | `"Key 不存在或已被删除"` |
| E4093 | 创建 Key 时名称冲突 | `"Key 名称 'my-key' 已存在"` |
| E4094 | 重复禁用 | `"Key 已处于禁用状态"` |
| E4095 | 重复启用 | `"Key 已处于启用状态"` |
| E4223 | Key 数量达上限 | `"已达最大 Key 数量限制（10），请先禁用或删除不再使用的 Key"` |

### 4.3 已有错误码（不变更）

现有错误码 `E4001`、`E4011`、`E4041`、`E4091`、`E4092`、`E4221`、`E4222`、`E5021`、`E4291`、`E5001` 保持不变。

---

## 5. 版本策略适用

**当前版本**：`v1` — 新增 7 个端点按 **非 Breaking Change** 规则在当前版本发布：

| 变更类型 | 规则符合性 |
| :--- | :--- |
| 新增端点 | 符合 — 非 Breaking Change 允许新增端点 |
| 新增响应字段 | 符合 — 新增字段为可选或仅在新增端点中出现 |
| 新增错误码 | 符合 — 不修改已有错误码含义 |
| 新增路径 `/api/v1/keys/*` | 符合 — 新增 URL 路径前缀，不修改已有路径 |
| 新增路径 `/internal/v1/keys/verify` | 符合 — 新增内部路径，不修改已有路径 |

---

## 6. 接口安全

**管理接口（/api/v1/keys/*）：**

- 认证方式：JWT Bearer Token（RS256），要求 token 中含 `user_id`
- Token 来源：ops-console 从公司统一身份平台获取
- Key 归属控制：所有操作从 JWT 提取 `user_id`，Service 层校验 `key.user_id == jwt.user_id`
- 明文保护：Key 明文仅在创建响应中返回一次，后续接口只返回脱敏值

**内部接口（/internal/v1/keys/verify）：**

- 认证方式：预共享 Token
- 网络隔离：仅 auth 可调用，不暴露对外网关
- 返回信息：仅返回验证必要的身份字段（key_id, status, user_id, name, department_id），不返回 Key 密文

---

## 7. 变更历史

| 版本号 | 更新时间 | 变更类型 | 变更描述 | 变更人 |
| :--- | :--- | :---: | :--- | :--- |
| v1.2.0 | 2026-05-23 | 新增 | 新增 Key CRUD 管理端点（6 个）+ 内部 Key 验证端点（1 个）；新增错误码 E4031/E4042/E4093/E4094/E4095/E4223 | OpenCode |

---

## 11. 变更记录 — CHG-20260525-001

## 变更摘要

新增模型组管理端点（5 个）及组内模型管理端点（3 个）供 ops-console 消费（JWT 认证），新增 Scheduler 内部配置端点（1 个）供 scheduler 消费（预共享 Token 认证）。新增错误码 E4096（group_name_conflict）、E4043（group_not_found）、E4224（invalid_strategy）、E4225（strategy_switch_warning）。现有端点、错误码、认证方式均不受影响。

---

## 1. 版本信息变更

| 属性 | 旧值 | 新值 |
| :--- | :--- | :--- |
| **当前版本** | v1.2.0 | v1.3.0 |
| **最后更新** | 2026-05-25 | 2026-05-25 |

---

## 2. 端点索引变更

> 在现有 20 个端点（13 个管理 + 7 个 Key 管理）基础上新增 9 个端点，按非 Breaking Change 规则在当前版本 v1 下发布。

### 2.1 新增端点

| 端点 | 方法 | 路径 | 用途 | 认证 | 幂等 |
| :--- | :---: | :--- | :--- | :---: | :---: |
| 创建模型组 | POST | `/api/v1/groups` | 创建新的模型组并配置路由策略 | JWT | 否 |
| 模型组列表 | GET | `/api/v1/groups` | 分页查询模型组列表，支持搜索 | JWT | 是 |
| 模型组详情 | GET | `/api/v1/groups/:id` | 查询模型组详情（含组内模型列表） | JWT | 是 |
| 更新模型组 | PUT | `/api/v1/groups/:id` | 更新组名称、描述、策略类型 | JWT | 是 |
| 删除模型组 | DELETE | `/api/v1/groups/:id` | 软删除模型组（设为禁用 + 标记删除时间） | JWT | 是 |
| 添加模型到组 | POST | `/api/v1/groups/:id/models` | 将已有模型添加到模型组（含策略参数） | JWT | 否 |
| 组内模型列表 | GET | `/api/v1/groups/:id/models` | 查询组内模型列表 | JWT | 是 |
| 从组移除模型 | DELETE | `/api/v1/groups/:id/models/:model_id` | 从模型组中移除指定模型 | JWT | 是 |
| Scheduler 组配置（内部） | GET | `/api/v1/internal/groups` | 返回所有启用模型组的完整配置 | 内部Token | 是 |

---

## 3. 新增端点详细设计

### 3.1 创建模型组

- **接口描述**：创建新的模型组，指定组名称、描述和路由策略类型。组创建后默认为启用状态。
- **请求方法**：`POST`
- **请求路径**：`/api/v1/groups`

#### 请求参数

| 参数名 | 类型 | 位置 | 必填 | 描述 | 示例 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| name | string | body | 是 | 模型组名称，2–64 字符，全局唯一 | `"日常推理"` |
| description | string | body | 否 | 模型组描述，最长 512 字符 | `"用于日常对话推理任务的模型组"` |
| strategy | string | body | 是 | 路由策略枚举 | `"round_robin"` |

**请求示例**：

```bash
curl -X POST \
  'https://control-plane.internal/api/v1/groups' \
  -H 'Authorization: Bearer {jwt_token}' \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "日常推理",
    "description": "用于日常对话推理任务的模型组",
    "strategy": "round_robin"
  }'
```

#### 响应

**成功响应**（201 Created）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": 1,
    "name": "日常推理",
    "description": "用于日常对话推理任务的模型组",
    "strategy": "round_robin",
    "enabled": true,
    "model_count": 0,
    "created_at": "2026-05-25T10:00:00Z",
    "updated_at": "2026-05-25T10:00:00Z"
  },
  "request_id": "req-uuid"
}
```

**响应参数说明**：

| 参数路径 | 类型 | 描述 |
| :--- | :--- | :--- |
| data.id | integer | 模型组唯一标识（自增 ID） |
| data.name | string | 模型组名称 |
| data.description | string | 模型组描述 |
| data.strategy | string | 路由策略：round_robin / priority / weighted |
| data.enabled | boolean | 是否启用 |
| data.model_count | integer | 组内模型数量 |
| data.created_at | string(datetime) | 创建时间 |
| data.updated_at | string(datetime) | 更新时间 |

**可能的错误码**：`E4001`（参数校验失败）、`E4011`（认证失败）、`E4096`（组名称冲突）、`E4224`（策略类型非法）

---

### 3.2 模型组列表

- **接口描述**：分页查询模型组列表，支持按名称模糊搜索。不返回已软删除的组。
- **请求方法**：`GET`
- **请求路径**：`/api/v1/groups`

#### 请求参数

| 参数名 | 类型 | 位置 | 必填 | 描述 | 示例 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| page | integer | query | 否 | 页码（默认 1） | `1` |
| page_size | integer | query | 否 | 每页条数（默认 20，最大 100） | `20` |
| search | string | query | 否 | 按名称模糊搜索 | `"推理"` |
| enabled | boolean | query | 否 | 按启用状态过滤 | `true` |

**请求示例**：

```bash
curl -X GET \
  'https://control-plane.internal/api/v1/groups?page=1&page_size=20&search=推理' \
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
        "id": 1,
        "name": "日常推理",
        "description": "用于日常对话推理任务的模型组",
        "strategy": "round_robin",
        "enabled": true,
        "model_count": 3,
        "created_at": "2026-05-25T10:00:00Z",
        "updated_at": "2026-05-25T10:00:00Z"
      },
      {
        "id": 2,
        "name": "高智能推理",
        "description": "用于复杂推理任务的高质量模型组",
        "strategy": "priority",
        "enabled": true,
        "model_count": 2,
        "created_at": "2026-05-25T11:00:00Z",
        "updated_at": "2026-05-25T11:00:00Z"
      }
    ],
    "total": 2,
    "page": 1,
    "page_size": 20
  },
  "request_id": "req-uuid"
}
```

**响应参数说明**：

| 参数路径 | 类型 | 描述 |
| :--- | :--- | :--- |
| data.items[].id | integer | 模型组 ID |
| data.items[].name | string | 模型组名称 |
| data.items[].description | string | 模型组描述 |
| data.items[].strategy | string | 路由策略 |
| data.items[].enabled | boolean | 启用状态 |
| data.items[].model_count | integer | 组内模型数量 |
| data.items[].created_at | string(datetime) | 创建时间 |
| data.items[].updated_at | string(datetime) | 更新时间 |

**可能的错误码**：`E4001`（分页参数非法）、`E4011`（认证失败）

---

### 3.3 模型组详情

- **接口描述**：查询单个模型组详情，包含组内模型列表（含策略参数 priority/weight）。
- **请求方法**：`GET`
- **请求路径**：`/api/v1/groups/:id`

#### 请求参数

| 参数名 | 类型 | 位置 | 必填 | 描述 | 示例 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| id | integer | path | 是 | 模型组 ID | `1` |

**请求示例**：

```bash
curl -X GET \
  'https://control-plane.internal/api/v1/groups/1' \
  -H 'Authorization: Bearer {jwt_token}'
```

#### 响应

**成功响应**（200 OK）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": 1,
    "name": "日常推理",
    "description": "用于日常对话推理任务的模型组",
    "strategy": "round_robin",
    "enabled": true,
    "model_count": 2,
    "models": [
      {
        "model_id": "660e8400-e29b-41d4-a716-446655440001",
        "model_name": "qwen-turbo",
        "model_type": "chat",
        "provider_name": "bailian-main",
        "priority": null,
        "weight": null
      },
      {
        "model_id": "660e8400-e29b-41d4-a716-446655440002",
        "model_name": "deepseek-chat",
        "model_type": "chat",
        "provider_name": "deepseek-main",
        "priority": null,
        "weight": null
      }
    ],
    "created_at": "2026-05-25T10:00:00Z",
    "updated_at": "2026-05-25T10:00:00Z"
  },
  "request_id": "req-uuid"
}
```

**可能的错误码**：`E4011`（认证失败）、`E4043`（模型组不存在或已被删除）

---

### 3.4 更新模型组

- **接口描述**：更新模型组名称、描述和策略类型。策略切换时如旧参数不兼容新策略，返回警告但不阻断操作。
- **请求方法**：`PUT`
- **请求路径**：`/api/v1/groups/:id`

#### 请求参数

| 参数名 | 类型 | 位置 | 必填 | 描述 | 示例 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| id | integer | path | 是 | 模型组 ID | `1` |
| name | string | body | 是 | 模型组名称 | `"日常推理"` |
| description | string | body | 否 | 模型组描述 | `"更新后的描述"` |
| strategy | string | body | 是 | 路由策略枚举 | `"priority"` |

**请求示例**：

```bash
curl -X PUT \
  'https://control-plane.internal/api/v1/groups/1' \
  -H 'Authorization: Bearer {jwt_token}' \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "日常推理",
    "description": "更新后的描述",
    "strategy": "priority"
  }'
```

#### 响应

**成功响应**（200 OK）：

```json
{
  "code": 0,
  "message": "策略已变更为 'priority'，原有 weight 参数可能不兼容，请重新配置组内模型",
  "data": {
    "id": 1,
    "name": "日常推理",
    "description": "更新后的描述",
    "strategy": "priority",
    "enabled": true,
    "model_count": 2,
    "created_at": "2026-05-25T10:00:00Z",
    "updated_at": "2026-05-25T12:00:00Z"
  },
  "request_id": "req-uuid"
}
```

**策略切换行为说明**：
- 从 `round_robin` → `priority`：组内模型缺少 priority 值，message 提示需重新配置
- 从 `weighted` → `round_robin`：原有 weight 参数保留但不生效，scheduler 按轮询处理
- 从 `priority` → `weighted`：原有 priority 参数保留但不生效，message 提示需重新配置
- 相同策略不切换：直接返回成功，无额外提示

**可能的错误码**：`E4001`（参数校验失败）、`E4011`（认证失败）、`E4043`（模型组不存在）、`E4096`（名称冲突）、`E4224`（策略类型非法）、`E4225`（策略切换警告，非阻断）

---

### 3.5 删除模型组

- **接口描述**：软删除模型组（设 enabled=false + 标记 deleted_at）。组内模型关联保留不清除，组恢复后关联关系完整保留。已删除的组在 scheduler 配置读取中自动过滤。
- **请求方法**：`DELETE`
- **请求路径**：`/api/v1/groups/:id`

#### 请求参数

| 参数名 | 类型 | 位置 | 必填 | 描述 | 示例 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| id | integer | path | 是 | 模型组 ID | `1` |

**请求示例**：

```bash
curl -X DELETE \
  'https://control-plane.internal/api/v1/groups/1' \
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

**幂等行为**：对已删除的组再次调用，返回 404 E4043 `"模型组不存在或已被删除"`。

**可能的错误码**：`E4011`（认证失败）、`E4043`（模型组不存在或已被删除）

---

### 3.6 添加模型到组

- **接口描述**：将已有模型添加到指定模型组。根据组的策略类型，需提供对应的策略参数（priority 或 weight）。同一模型不可在同一组内重复添加，但可跨组添加。
- **请求方法**：`POST`
- **请求路径**：`/api/v1/groups/:id/models`

#### 请求参数

| 参数名 | 类型 | 位置 | 必填 | 描述 | 示例 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| id | integer | path | 是 | 模型组 ID | `2` |
| model_id | string(uuid) | body | 是 | 模型 ID（models 表 UUID） | `"660e8400-e29b-41d4-a716-446655440001"` |
| priority | integer | body | 条件必填 | 优先级（strategy=priority 时必填，正整数） | `1` |
| weight | integer | body | 条件必填 | 权重值（strategy=weighted 时必填，1–100） | `50` |

**请求示例**：

```bash
# 优先级策略 — 添加模型
curl -X POST \
  'https://control-plane.internal/api/v1/groups/2/models' \
  -H 'Authorization: Bearer {jwt_token}' \
  -H 'Content-Type: application/json' \
  -d '{
    "model_id": "660e8400-e29b-41d4-a716-446655440001",
    "priority": 1
  }'

# 加权策略 — 添加模型
curl -X POST \
  'https://control-plane.internal/api/v1/groups/3/models' \
  -H 'Authorization: Bearer {jwt_token}' \
  -H 'Content-Type: application/json' \
  -d '{
    "model_id": "660e8400-e29b-41d4-a716-446655440001",
    "weight": 50
  }'

# 轮询策略 — 添加模型（无需策略参数）
curl -X POST \
  'https://control-plane.internal/api/v1/groups/1/models' \
  -H 'Authorization: Bearer {jwt_token}' \
  -H 'Content-Type: application/json' \
  -d '{
    "model_id": "660e8400-e29b-41d4-a716-446655440001"
  }'
```

#### 响应

**成功响应**（201 Created）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "group_id": 2,
    "model_id": "660e8400-e29b-41d4-a716-446655440001",
    "model_name": "qwen-turbo",
    "model_type": "chat",
    "priority": 1,
    "weight": null,
    "created_at": "2026-05-25T13:00:00Z"
  },
  "request_id": "req-uuid"
}
```

**可能的错误码**：`E4001`（参数校验失败）、`E4011`（认证失败）、`E4041`（模型不存在）、`E4043`（模型组不存在）、`E4096`（模型已在组内）

---

### 3.7 组内模型列表

- **接口描述**：查询指定模型组内的模型列表，返回模型信息及策略参数。
- **请求方法**：`GET`
- **请求路径**：`/api/v1/groups/:id/models`

#### 请求参数

| 参数名 | 类型 | 位置 | 必填 | 描述 | 示例 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| id | integer | path | 是 | 模型组 ID | `1` |

**请求示例**：

```bash
curl -X GET \
  'https://control-plane.internal/api/v1/groups/1/models' \
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
        "group_id": 1,
        "model_id": "660e8400-e29b-41d4-a716-446655440001",
        "model_name": "qwen-turbo",
        "model_type": "chat",
        "provider_name": "bailian-main",
        "priority": null,
        "weight": null,
        "created_at": "2026-05-25T10:05:00Z"
      },
      {
        "group_id": 1,
        "model_id": "660e8400-e29b-41d4-a716-446655440002",
        "model_name": "deepseek-chat",
        "model_type": "chat",
        "provider_name": "deepseek-main",
        "priority": null,
        "weight": null,
        "created_at": "2026-05-25T10:10:00Z"
      }
    ],
    "total": 2
  },
  "request_id": "req-uuid"
}
```

**可能的错误码**：`E4011`（认证失败）、`E4043`（模型组不存在）

---

### 3.8 从组移除模型

- **接口描述**：从模型组中移除指定模型。移除后该模型在该组中不再参与路由，但不影响该模型在其他组的路由。
- **请求方法**：`DELETE`
- **请求路径**：`/api/v1/groups/:id/models/:model_id`

#### 请求参数

| 参数名 | 类型 | 位置 | 必填 | 描述 | 示例 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| id | integer | path | 是 | 模型组 ID | `1` |
| model_id | string(uuid) | path | 是 | 模型 ID | `"660e8400-e29b-41d4-a716-446655440001"` |

**请求示例**：

```bash
curl -X DELETE \
  'https://control-plane.internal/api/v1/groups/1/models/660e8400-e29b-41d4-a716-446655440001' \
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

**幂等行为**：对已不在组内的模型再次调用，返回 200 OK 且 message 为 `"模型不在该组内"`。

**可能的错误码**：`E4011`（认证失败）、`E4043`（模型组不存在）

---

### 3.9 Scheduler 组配置（内部接口）

- **接口描述**：返回所有启用且未软删除的模型组完整配置，包含组内模型列表、渠道信息（含解密后凭证）和策略参数。供 scheduler 启动时全量拉取或定期刷新配置使用。
- **请求方法**：`GET`
- **请求路径**：`/api/v1/internal/groups`

#### 请求参数

无请求参数。

**请求示例**：

```bash
curl -X GET \
  'https://control-plane.internal/api/v1/internal/groups' \
  -H 'Authorization: Bearer {internal_token}'
```

#### 响应

**成功响应**（200 OK）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "groups": [
      {
        "id": 1,
        "name": "日常推理",
        "strategy": "round_robin",
        "models": [
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
            },
            "priority": null,
            "weight": null
          },
          {
            "model_name": "deepseek-chat",
            "model_type": "chat",
            "provider": {
              "id": "660e8400-e29b-41d4-a716-446655440005",
              "name": "deepseek-main",
              "type": "deepseek",
              "endpoint": "https://api.deepseek.com/v1",
              "credentials": {
                "api_key": "sk-deepseekkey1234"
              }
            },
            "priority": null,
            "weight": null
          }
        ]
      },
      {
        "id": 2,
        "name": "高智能",
        "strategy": "priority",
        "models": [
          {
            "model_name": "qwen-max",
            "model_type": "chat",
            "provider": {
              "id": "550e8400-e29b-41d4-a716-446655440000",
              "name": "bailian-main",
              "type": "bailian",
              "endpoint": "https://dashscope.aliyuncs.com/compatible-mode/v1",
              "credentials": {
                "api_key": "sk-abcdefghijklmnop"
              }
            },
            "priority": 1,
            "weight": null
          },
          {
            "model_name": "deepseek-r1",
            "model_type": "chat",
            "provider": {
              "id": "660e8400-e29b-41d4-a716-446655440005",
              "name": "deepseek-main",
              "type": "deepseek",
              "endpoint": "https://api.deepseek.com/v1",
              "credentials": {
                "api_key": "sk-deepseekkey1234"
              }
            },
            "priority": 2,
            "weight": null
          }
        ]
      },
      {
        "id": 3,
        "name": "成本优化",
        "strategy": "weighted",
        "models": [
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
            },
            "priority": null,
            "weight": 70
          },
          {
            "model_name": "qwen-plus",
            "model_type": "chat",
            "provider": {
              "id": "550e8400-e29b-41d4-a716-446655440000",
              "name": "bailian-main",
              "type": "bailian",
              "endpoint": "https://dashscope.aliyuncs.com/compatible-mode/v1",
              "credentials": {
                "api_key": "sk-abcdefghijklmnop"
              }
            },
            "priority": null,
            "weight": 30
          }
        ]
      }
    ],
    "updated_at": "2026-05-25T12:00:00Z"
  },
  "request_id": "req-uuid"
}
```

**响应参数说明**：

| 参数路径 | 类型 | 描述 |
| :--- | :--- | :--- |
| data.groups[].id | integer | 模型组 ID |
| data.groups[].name | string | 模型组名称 |
| data.groups[].strategy | string | 路由策略枚举：round_robin / priority / weighted |
| data.groups[].models[].model_name | string | 模型名称（scheduler 路由匹配键） |
| data.groups[].models[].model_type | string | 模型类型（chat/embedding） |
| data.groups[].models[].provider.id | string(uuid) | 渠道 ID |
| data.groups[].models[].provider.name | string | 渠道名称 |
| data.groups[].models[].provider.type | string | 渠道类型 |
| data.groups[].models[].provider.endpoint | string | API 端点 URL |
| data.groups[].models[].provider.credentials | object | 解密后的完整凭证（仅内部接口返回） |
| data.groups[].models[].priority | integer\|null | 优先级值（仅 priority 策略有效） |
| data.groups[].models[].weight | integer\|null | 权重值（仅 weighted 策略有效，1–100） |
| data.updated_at | string(datetime) | 配置最后更新时间（用于缓存判断） |

> **安全说明**：此接口返回完整凭证明文，仅供 scheduler 内部调用，通过预共享 Token 认证 + 网络隔离保护。接口仅返回 `enabled=true` 且未软删除（`deleted_at IS NULL`）的模型组，组内仅包含 `enabled=true` 的模型及其对应 `enabled=true` 的渠道。

**可能的错误码**：`E4011`（认证失败）、`E5001`（凭证解密失败等内部错误）

---

## 4. 错误码变更

### 4.1 新增错误码

| 错误码 | HTTP 状态 | 语义 | 触发条件 | 可重试 |
| :--- | :---: | :--- | :--- | :---: |
| E4043 | 404 | 模型组不存在 | 指定 ID 的模型组不存在或已被软删除 | 否 |
| E4096 | 409 | 模型组名称冲突 | 创建或更新时组名称与已有组重复 | 否（需换名） |
| E4224 | 422 | 策略类型非法 | strategy 字段不属于 round_robin / priority / weighted | 否 |
| E4225 | 422 | 策略切换参数警告 | 策略类型变更后旧参数可能不兼容新策略（非阻断） | — |

> **错误码分配说明**：E4091–E4095 已被 Key CRUD 功能占用，故组名称冲突使用 E4096。E4041、E4042 分别被通用资源不存在和 Key 不存在占用，故组不存在使用 E4043。

### 4.2 业务错误码补充说明

| 错误码 | 场景 | message 示例 |
| :--- | :--- | :--- |
| E4043 | 模型组不存在或已删除 | `"模型组不存在或已被删除"` |
| E4096 | 创建组时名称冲突 | `"模型组名称 '日常推理' 已存在"` |
| E4096 | 更新组时名称冲突 | `"模型组名称 '高智能' 已存在，请使用其他名称"` |
| E4224 | 非法策略值 | `"无效的策略类型 'xxx'，支持：round_robin, priority, weighted"` |
| E4225 | 策略切换警告 | `"策略已变更为 'round_robin'，原有 priority/weight 参数可能不兼容，请重新配置组内模型"` |

### 4.3 已有错误码（不变更）

现有错误码 `E4001`、`E4011`、`E4031`、`E4041`、`E4042`、`E4091`–`E4095`、`E4221`–`E4223`、`E4291`、`E5021`、`E5001` 保持不变。模型不存在使用已有 `E4041`；模型已在组内重复添加使用已有 `E4091`（通用冲突错误码）。

---

## 5. 版本策略适用

**当前版本**：`v1` — 新增 9 个端点按 **非 Breaking Change** 规则在当前版本发布：

| 变更类型 | 规则符合性 |
| :--- | :--- |
| 新增端点 | 符合 — 非 Breaking Change 允许新增端点 |
| 新增响应字段 | 符合 — 新增字段为可选或仅在新增端点中出现 |
| 新增错误码 | 符合 — 不修改已有错误码含义 |
| 新增路径 `/api/v1/groups/*` | 符合 — 新增 URL 路径前缀，不修改已有路径 |
| 新增路径 `/api/v1/internal/groups` | 符合 — 新增内部路径，不修改已有路径 |

---

## 6. 接口安全

**管理接口（/api/v1/groups/*）：**

- 认证方式：JWT Bearer Token（RS256），要求 token 中含 `role: admin`
- Token 来源：ops-console 从公司统一身份平台获取
- 授权控制：仅平台管理员可操作模型组（admin role），普通用户通过 Key CRUD 端点管理自有 Key

**内部接口（/api/v1/internal/groups）：**

- 认证方式：预共享 Token
- 网络隔离：仅 scheduler 可调用，不暴露对外网关
- 返回信息：返回解密后的完整渠道凭证，供 scheduler 构建路由表使用
- 数据范围：仅返回 `enabled=true` 且未软删除的组及其模型和渠道

---

## 7. 变更历史

| 版本号 | 更新时间 | 变更类型 | 变更描述 | 变更人 |
| :--- | :--- | :---: | :--- | :--- |
| v1.3.0 | 2026-05-25 | 新增 | 新增模型组管理端点（5 个）+ 组内模型管理端点（3 个）+ Scheduler 内部配置端点（1 个）；新增错误码 E4043/E4096/E4224/E4225 | OpenCode |

---

# control-plane 接口契约变更 — CHG-20260526-001

> 基线：xspec/modules/control-plane/contracts.md (v1.3.0)

## 变更摘要

新增 4 个端点：Key 组绑定管理（PATCH 增量变更 + GET 查询）、可选模型组列表查询、供 gateway-api 读取绑定的内部接口。修改 Key 创建和查询端点追加 group_ids/bound_groups 字段。

## 新增接口

### 1. PATCH /api/v1/keys/:id/groups — 增量变更 Key 组绑定

认证：JWT (Bearer Token)

**请求体**：

```json
{
  "add": ["uuid-g1", "uuid-g2"],
  "remove": ["uuid-g3"]
}
```

| 字段 | 类型 | 必填 | 说明 |
| :--- | :--- | :--- | :--- |
| add | string[] | 否 | 要新增绑定的模型组 ID 列表 |
| remove | string[] | 否 | 要移除绑定的模型组 ID 列表 |

> add 和 remove 至少提供一个非空数组。

**成功响应** (200)：

```json
{
  "key_id": "uuid-key-1",
  "bound_groups": [
    {"id": "uuid-g1", "name": "日常推理", "enabled": true},
    {"id": "uuid-g2", "name": "高智能", "enabled": true}
  ]
}
```

**错误响应**：

| HTTP 状态 | 错误码 | 说明 |
| :---: | :--- | :--- |
| 400 | GROUP_NOT_FOUND | add 中包含不存在的模型组 ID |
| 400 | GROUP_ALREADY_BOUND | add 中包含已绑定该 Key 的组 |
| 400 | GROUP_NOT_BOUND | remove 中包含未绑定该 Key 的组 |
| 400 | INVALID_REQUEST | add 和 remove 同时为空 |
| 404 | KEY_NOT_FOUND | key_id 不存在 |

### 2. GET /api/v1/keys/:id/groups — 查询 Key 绑定组列表

认证：JWT (Bearer Token)

**成功响应** (200)：

```json
{
  "key_id": "uuid-key-1",
  "bound_groups": [
    {"id": "uuid-g1", "name": "日常推理", "enabled": true, "strategy": "round_robin"},
    {"id": "uuid-g2", "name": "高智能", "enabled": false, "strategy": "priority"}
  ]
}
```

| 字段 | 类型 | 说明 |
| :--- | :--- | :--- |
| key_id | string | Key 唯一标识 |
| bound_groups[].id | string | 模型组唯一标识 |
| bound_groups[].name | string | 模型组名称 |
| bound_groups[].enabled | bool | 组启用状态 |
| bound_groups[].strategy | string | 组路由策略（round_robin / priority / weighted） |

**错误响应**：

| HTTP 状态 | 错误码 | 说明 |
| :---: | :--- | :--- |
| 404 | KEY_NOT_FOUND | key_id 不存在 |

### 3. GET /api/v1/groups/available — 查询可选模型组列表

认证：JWT (Bearer Token)

> 返回所有已启用模型组，供 Key 创建/管理时选择。本期不做权限过滤。

**成功响应** (200)：

```json
{
  "groups": [
    {"id": "uuid-g1", "name": "日常推理", "description": "日常对话和轻量推理场景", "strategy": "round_robin"},
    {"id": "uuid-g2", "name": "高智能", "description": "复杂推理和代码生成", "strategy": "priority"}
  ]
}
```

**错误响应**：

| HTTP 状态 | 错误码 | 说明 |
| :---: | :--- | :--- |
| — | — | 本接口无业务错误，无可用组时返回空数组 |

### 4. GET /internal/v1/keys/:id/groups — 内部：查询 Key 绑定组

认证：内部 Token（X-Internal-Token Header）

> 供 gateway-api 调用，返回 Key 绑定的模型组列表及启停状态。

**成功响应** (200)：

```json
[
  {"id": "uuid-g1", "name": "日常推理", "enabled": true},
  {"id": "uuid-g2", "name": "高智能", "enabled": true}
]
```

**错误响应**：

| HTTP 状态 | 错误码 | 说明 |
| :---: | :--- | :--- |
| 401 | UNAUTHORIZED | 内部 Token 无效 |
| 404 | KEY_NOT_FOUND | key_id 不存在 |

## 修改接口

### POST /api/v1/keys — 创建 Key（追加 group_ids 参数）

**请求体变更**：

```json
{
  "name": "我的日常Key",
  "group_ids": ["uuid-g1", "uuid-g2"]
}
```

| 字段 | 类型 | 必填 | 变更 | 说明 |
| :--- | :--- | :--- | :--- | :--- |
| name | string | 是 | 原有 | Key 名称标识 |
| group_ids | string[] | 否 | **新增** | 可选，创建时绑定的模型组 ID 列表；不传则创建无绑定 Key |

**响应体变更**（201）：

```json
{
  "key": "sk-abc123...xyz",
  "key_id": "uuid-key-1",
  "name": "我的日常Key",
  "status": "enabled",
  "created_at": "2026-05-26T10:00:00Z",
  "bound_groups": [
    {"id": "uuid-g1", "name": "日常推理"},
    {"id": "uuid-g2", "name": "高智能"}
  ]
}
```

新增字段 `bound_groups`：创建后 Key 当前绑定的模型组名称列表。

### GET /api/v1/keys — 查询 Key 列表（追加 bound_groups）

**响应体变更**：每个 Key 条目追加 `bound_groups` 字段：

```json
{
  "keys": [
    {
      "id": "uuid-key-1",
      "name": "我的日常Key",
      "key_suffix": "xyz1",
      "status": "enabled",
      "created_at": "2026-05-26T10:00:00Z",
      "bound_groups": [
        {"id": "uuid-g1", "name": "日常推理"},
        {"id": "uuid-g2", "name": "高智能"}
      ]
    }
  ]
}
```

### GET /api/v1/keys/:id — 查询 Key 详情（追加 bound_groups）

与列表接口一致，追加 `bound_groups` 字段。