# API Contract: 模型状态变更日志

**Feature**: 001-model-auto-recovery | **Date**: 2026-06-10

本文档定义模型状态变更日志的 API 契约。

---

## 1. 获取模型状态变更日志列表

### Endpoint

```
GET /api/v1/model-status-logs
```

### 认证

需要 JWT 认证（`JWTAuth` 中间件）

### 请求参数 (Query)

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `model_id` | `uuid` | 否 | - | 按模型 ID 筛选 |
| `provider_id` | `uuid` | 否 | - | 按渠道 ID 筛选 |
| `change_type` | `string` | 否 | - | 按变更类型筛选（auto_disabled/auto_recovered/manual_disabled/manual_enabled） |
| `start_time` | `string` | 否 | - | 开始时间（ISO 8601 格式） |
| `end_time` | `string` | 否 | - | 结束时间（ISO 8601 格式） |
| `page` | `int` | 否 | `1` | 页码 |
| `page_size` | `int` | 否 | `20` | 每页条数（最大 100） |

### 响应

#### 成功 (200 OK)

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "total": 42,
    "page": 1,
    "page_size": 20,
    "items": [
      {
        "id": "550e8400-e29b-41d4-a716-446655440000",
        "model_id": "660e8400-e29b-41d4-a716-446655440001",
        "model_name": "gpt-4-turbo",
        "provider_id": "770e8400-e29b-41d4-a716-446655440002",
        "provider_name": "OpenAI Production",
        "change_type": "auto_disabled",
        "reason": "连续 3 次健康检测失败",
        "error_message": "upstream timeout after 15s",
        "operator_id": null,
        "operator_name": "",
        "created_at": "2026-06-10T08:30:00Z"
      },
      {
        "id": "550e8400-e29b-41d4-a716-446655440003",
        "model_id": "660e8400-e29b-41d4-a716-446655440001",
        "model_name": "gpt-4-turbo",
        "provider_id": "770e8400-e29b-41d4-a716-446655440002",
        "provider_name": "OpenAI Production",
        "change_type": "auto_recovered",
        "reason": "连续 2 次健康检测成功",
        "error_message": "",
        "operator_id": null,
        "operator_name": "",
        "created_at": "2026-06-10T08:45:00Z"
      }
    ]
  }
}
```

#### 错误响应

| HTTP 状态码 | code | message | 场景 |
|-------------|------|---------|------|
| 400 | 1001 | "invalid model_id format" | model_id 格式错误 |
| 400 | 1002 | "invalid time format" | 时间格式错误 |
| 401 | 2001 | "unauthorized" | 未认证 |
| 500 | 5001 | "internal server error" | 服务器内部错误 |

---

## 2. 获取单个模型的状态变更历史

### Endpoint

```
GET /api/v1/models/:id/status-logs
```

### 认证

需要 JWT 认证（`JWTAuth` 中间件）

### 路径参数

| 参数 | 类型 | 说明 |
|------|------|------|
| `id` | `uuid` | 模型 ID |

### 请求参数 (Query)

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `limit` | `int` | 否 | `10` | 返回条数（最大 50） |

### 响应

#### 成功 (200 OK)

```json
{
  "code": 0,
  "message": "success",
  "data": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440003",
      "model_id": "660e8400-e29b-41d4-a716-446655440001",
      "model_name": "gpt-4-turbo",
      "provider_id": "770e8400-e29b-41d4-a716-446655440002",
      "provider_name": "OpenAI Production",
      "change_type": "auto_recovered",
      "reason": "连续 2 次健康检测成功",
      "error_message": "",
      "operator_id": null,
      "operator_name": "",
      "created_at": "2026-06-10T08:45:00Z"
    },
    {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "model_id": "660e8400-e29b-41d4-a716-446655440001",
      "model_name": "gpt-4-turbo",
      "provider_id": "770e8400-e29b-41d4-a716-446655440002",
      "provider_name": "OpenAI Production",
      "change_type": "auto_disabled",
      "reason": "连续 3 次健康检测失败",
      "error_message": "upstream timeout after 15s",
      "operator_id": null,
      "operator_name": "",
      "created_at": "2026-06-10T08:30:00Z"
    }
  ]
}
```

#### 错误响应

| HTTP 状态码 | code | message | 场景 |
|-------------|------|---------|------|
| 400 | 1001 | "invalid model_id format" | ID 格式错误 |
| 404 | 4001 | "model not found" | 模型不存在 |
| 401 | 2001 | "unauthorized" | 未认证 |

---

## 3. 变更类型说明

| change_type | 中文说明 | 触发场景 |
|-------------|----------|----------|
| `auto_disabled` | 自动禁用 | 模型连续健康检测失败达到阈值（默认 3 次） |
| `auto_recovered` | 自动恢复 | 被自动禁用的模型连续检测成功达到阈值（默认 2 次） |
| `manual_disabled` | 手动禁用 | 管理员在界面点击"禁用"按钮 |
| `manual_enabled` | 手动启用 | 管理员在界面点击"启用"按钮 |

---

## 4. 前端集成说明

### 日志看板页面路由

```
/admin/model-status-logs
```

### 模型详情页入口

在模型列表页 (`/admin/models`) 的每行操作列添加"状态历史"按钮，点击后展开抽屉或跳转到筛选后的日志列表。

### 时间展示

使用相对时间格式（如"5 分钟前"、"2 小时前"），hover 显示完整时间戳。

### 变更类型标签颜色

| change_type | 颜色 | Element Plus Tag 类型 |
|-------------|------|----------------------|
| `auto_disabled` | 红色 | `danger` |
| `auto_recovered` | 绿色 | `success` |
| `manual_disabled` | 橙色 | `warning` |
| `manual_enabled` | 蓝色 | `primary` |
