# auth Web 后端接口契约

## 1. 概览

**本契约描述**：auth 模块对外提供员工登录态相关接口，对内提供统一认证验证接口，供 gateway-api 解析 API Key 身份链路。

| 属性 | 内容 |
| :--- | :--- |
| **生产方（Provider）** | auth |
| **消费方（Consumer）** | ops-console、gateway-api |
| **当前版本** | `v1.0.0` |
| **最后更新** | 2026-05-23 |

---

## 2. 公共约定

| 约定 | 内容 |
| :--- | :--- |
| **Base URL** | `/`（公开接口） / `/internal/v1`（内部接口） |
| **数据格式** | JSON，UTF-8，Content-Type: `application/json` |
| **认证方式** | 公开认证接口按端点约定；内部接口使用预共享 Bearer Token |
| **时间戳格式** | ISO 8601 UTC（`2026-05-23T10:30:00Z`） |

---

## 3. 端点索引

| 端点 | 方法 | 路径 | 用途 | 认证 | 幂等 |
| :--- | :---: | :--- | :--- | :---: | :---: |
| OIDC 回调换取登录态 | POST | `/auth/oidc/callback` | SSO 授权码换取用户信息和 JWT | 否 | 否 |
| 刷新登录态 | POST | `/auth/refresh` | 刷新 Access/Refresh Token | 否 | 否 |
| 注销登录态 | POST | `/auth/logout` | 注销当前 Refresh Token | 是 | 是 |
| 验证 API Key（内部） | POST | `/internal/v1/auth/verify-key` | 解析 API Key → User → Department | 是 | 是 |

---

## 4. 端点详细设计

### 4.1 POST /internal/v1/auth/verify-key

* **接口描述**：供 gateway-api 内部调用，验证 API Key 并返回鉴权结果。auth 可通过缓存和下游 control-plane 查询完成解析，但 gateway-api 不直接依赖 control-plane。
* **请求方法**：`POST`
* **请求路径**：`/internal/v1/auth/verify-key`

#### 请求参数

| 参数名 | 类型 | 位置 | 必填 | 描述 | 示例 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| key | string | body | 是 | 待验证的 API Key 明文 | `"sk-a1B2c3D4e5F6g7H8i9J0k1l2m3N4o5P6"` |

**请求示例**：

```bash
curl -X POST \
  'https://auth.internal/internal/v1/auth/verify-key' \
  -H 'Authorization: Bearer {internal_token}' \
  -H 'Content-Type: application/json' \
  -d '{
    "key": "sk-a1B2c3D4e5F6g7H8i9J0k1l2m3N4o5P6"
  }'
```

#### 响应

**成功响应 - Key 有效**（200 OK）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "status": "active",
    "key_id": "880e8400-e29b-41d4-a716-446655440002",
    "user": {
      "user_id": "770e8400-e29b-41d4-a716-446655440001",
      "name": "张三",
      "department_id": "660e8400-e29b-41d4-a716-446655440000"
    }
  },
  "request_id": "req-uuid"
}
```

**成功响应 - Key 已禁用**（200 OK）：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "status": "disabled"
  },
  "request_id": "req-uuid"
}
```

**成功响应 - Key 不存在**（200 OK）：

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
| data.status | string | 枚举：`active`、`disabled`、`not_found` |
| data.key_id | string(uuid) | Key 唯一标识，仅 `active` 时返回 |
| data.user.user_id | string(uuid) | 归属用户标识，仅 `active` 时返回 |
| data.user.name | string | 归属用户姓名，仅 `active` 时返回 |
| data.user.department_id | string(uuid) | 归属部门标识，仅 `active` 时返回，可为空 |

**可能的错误码**：`E4001`（key 参数缺失或格式非法）、`E4011`（内部 Token 认证失败）、`E5031`（下游 control-plane 不可用）

---

## 5. 错误码

### 5.1 统一错误响应结构

```json
{
  "error_code": "E4001",
  "message": "人类可读说明",
  "request_id": "用于追踪的请求唯一 ID"
}
```

### 5.2 业务错误码

| 错误码 | HTTP 状态 | 描述 | 可能原因 |
| :--- | :--- | :--- | :--- |
| E4001 | 400 | 请求参数非法 | 缺少 `key` 或 Key 格式不符合 `sk-` 前缀规范 |
| E4011 | 401 | 认证失败 | 内部调用未携带或携带了无效内部 Token |
| E5031 | 503 | 下游认证依赖不可用 | auth 无法从 control-plane 获得 Key 状态 |

---

## 6. 版本策略与 Breaking Change 规则

**当前版本**：`v1.0.0`

**Breaking Change（必须升大版本）：**
- 删除已有端点
- 删除响应中的已有字段
- 修改 `verify-key` 返回结构或 `status` 语义
- 修改 JWT / 内部 Token 的认证方式
- 新增请求必填字段

**非 Breaking Change（可在当前版本直接发布）：**
- 新增端点
- 新增响应中的可选字段
- 新增错误码（不修改已有语义）

**废弃流程**：
1. 在响应 Header 中加 `Deprecated: true` 及 `Sunset: <日期>`
2. 保留至少 3 个月后方可移除
3. 移除前通知所有已知消费方（ops-console、gateway-api）

---

## 7. 非功能约束（适用时填）

| 约束类型 | 值 | 超出时的行为 |
| :--- | :--- | :--- |
| OIDC 回调超时 | 5s | 返回 E5031 |
| verify-key 内部调用超时 | 500ms | 返回 E5031 |
| 请求体最大大小 | 1 MB | 返回 E4001 |
| Refresh Token 轮换窗口 | 7 天 | 过期后要求重新登录 |

---

## 8. 接口安全

### 8.1 认证方式

- `/auth/oidc/callback`、`/auth/refresh`：无需预先认证，由 SSO code 或 refresh token 完成身份续期
- `/auth/logout`：要求当前登录态 JWT 用于识别操作者，同时提交 refresh token 完成服务端失效
- `/internal/v1/auth/verify-key`：仅允许 gateway-api 通过预共享 Bearer Token 调用

### 8.2 安全措施

* **HTTPS / mTLS 强制**：内部和外部调用均走安全通道
* **输入验证**：严格校验 `sk-` Key 格式和 JWT/refresh token 基本格式
* **最小暴露**：内部验证接口仅返回 `key_id`、`status`、`user{user_id,name,department_id}` 等必要身份字段

---

## 9. 变更历史

| 版本号 | 更新时间 | 变更类型 | 变更描述 | 变更人 |
| :--- | :--- | :---: | :--- | :--- |
| v1.0.0 | 2026-05-23 | 新增 | 初始化 auth 公开认证接口与内部 verify-key 契约 | OpenCode |
