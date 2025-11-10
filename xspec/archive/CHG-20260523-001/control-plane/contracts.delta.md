# control-plane Web 后端接口契约变更 — CHG-20260523-001

> 基线：xspec/modules/control-plane/contracts.md (v1.1.0)

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
