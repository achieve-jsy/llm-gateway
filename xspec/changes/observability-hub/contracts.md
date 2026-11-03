# observability-hub Web 后端接口契约

## 1. 概览

**本契约描述**：observability-hub 模块的 RESTful API 接口（用量查询与调用内容获取）。

| 属性 | 内容 |
| :--- | :--- |
| **生产方（Provider）** | observability-hub（control-plane 进程内） |
| **消费方（Consumer）** | ops-console（Vue 3 前端） |
| **当前版本** | `v1.0.0` |
| **最后更新** | 2026-06-04 |

---

## 2. 公共约定

| 约定 | 内容 |
| :--- | :--- |
| **Base URL** | `/api/v1`（复用 control-plane 路由前缀） |
| **数据格式** | JSON，UTF-8，Content-Type: `application/json` |
| **认证方式** | JWT Bearer Token（`Authorization: Bearer {jwt_token}`） |
| **时间戳格式** | 日期：`YYYY-MM-DD`；日期时间：ISO 8601 UTC |

**幂等性定义**：

| 方法 | 默认幂等 |
| :---: | :---: |
| GET | ✅ |

---

## 3. 端点索引

| 端点 | 方法 | 路径 | 用途 | 认证 | 幂等 |
| :--- | :---: | :--- | :--- | :---: | :---: |
| 用量查询 | GET | `/v1/usage` | 按条件查询聚合用量与成本 | JWT | ✅ |
| 调用内容获取 | GET | `/v1/usage/payload/:request_id` | 按 request_id 检索调用正文 | JWT | ✅ |

---

## 4. 端点详细设计

### 4.1 用量查询 (GET /v1/usage)

- **接口描述**：从 usage_daily 聚合表按条件查询用量数据，支持多维度过滤和分组，含分桶成本（prompt/completion/reasoning/cached）和延迟指标（avg_ttft / p95_ttft / avg_duration / p95_duration）。三级 RBAC 强制注入 WHERE 条件。
- **请求方法**：`GET`
- **请求路径**：`/api/v1/usage`

**请求参数**：

| 参数名 | 类型 | 位置 | 必填 | 描述 | 示例 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| `from` | string | query | 是 | 开始日期 | `2026-06-01` |
| `to` | string | query | 是 | 结束日期（≥ from） | `2026-06-04` |
| `model` | string | query | 否 | 模型名过滤 | `deepseek-chat` |
| `provider_type` | string | query | 否 | 厂商过滤 | `openai` |
| `is_stream` | boolean | query | 否 | 流式过滤 | `true` |
| `group_by` | string | query | 否 | 聚合维度，默认 day | `model` |

**group_by 枚举值**：`day`（默认）、`model`、`dept`、`studio`、`key`、`provider`

**请求示例**：

```bash
curl -X GET \
  'https://gateway.example.com/api/v1/usage?from=2026-06-01&to=2026-06-04&group_by=model' \
  -H 'Authorization: Bearer {jwt_token}'
```

**成功响应（200）**：

```json
{
  "items": [
    {
      "stat_date": "2026-06-03",
      "model": "deepseek-chat",
      "provider_type": "deepseek",
      "is_stream": false,
      "request_count": 1523,
      "prompt_tokens": 2458000,
      "completion_tokens": 890000,
      "reasoning_tokens": 0,
      "cached_tokens": 120000,
      "avg_ttft_ms": 850.5,
      "p95_ttft_ms": 2100.0,
      "avg_duration_ms": 3200.3,
      "p95_duration_ms": 8500.0,
      "prompt_cost": 0.712,
      "completion_cost": 1.566,
      "reasoning_cost": 0,
      "cached_cost": 0.018,
      "total_cost": 2.296
    }
  ],
  "total": {
    "total_requests": 1523,
    "prompt_tokens": 2458000,
    "completion_tokens": 890000,
    "reasoning_tokens": 0,
    "cached_tokens": 120000,
    "total_tokens": 3468000,
    "prompt_cost": 0.712,
    "completion_cost": 1.566,
    "reasoning_cost": 0,
    "cached_cost": 0.018,
    "total_cost": 2.296,
    "avg_ttft_ms": 850.5,
    "p95_ttft_ms": 2100.0,
    "avg_duration_ms": 3200.3,
    "p95_duration_ms": 8500.0
  }
}
```

**RBAC 规则**：

| 角色 | 数据范围 | 越权处理 |
| :--- | :--- | :--- |
| 管理员 | 全量，无过滤 | — |
| 部门负责人 | `WHERE dept_id = caller.dept_id` | 传其他 dept_id 静默覆盖 |
| 普通员工 | `WHERE user_id = caller.user_id` | 传其他 user_id/dept_id 静默覆盖 |

**业务规则**：
- from > to 返回 400
- 日期范围超过 90 天返回 400
- 无数据时返回空数组 + total 全零，不报错
- 结果按 total_cost DESC 排序

### 4.2 调用内容获取 (GET /v1/usage/payload/:request_id)

- **接口描述**：按 request_id 检索指定调用的完整 request messages 和响应正文。访问控制严格：管理员可查任意，普通员工仅可查本人记录。
- **请求方法**：`GET`
- **请求路径**：`/api/v1/usage/payload/:request_id`

**请求参数**：

| 参数名 | 类型 | 位置 | 必填 | 描述 |
| :--- | :--- | :--- | :--- | :--- |
| `request_id` | uuid | path | 是 | 请求唯一标识 |

**请求示例**：

```bash
curl -X GET \
  'https://gateway.example.com/api/v1/usage/payload/550e8400-e29b-41d4-a716-446655440000' \
  -H 'Authorization: Bearer {jwt_token}'
```

**成功响应（200）**：

```json
{
  "request_id": "550e8400-e29b-41d4-a716-446655440000",
  "model": "deepseek-chat",
  "messages": [
    {"role": "system", "content": "You are a helpful assistant."},
    {"role": "user", "content": "Hello!"}
  ],
  "response_text": "Hello! How can I assist you today?",
  "system_fingerprint": "fp_abc123",
  "tools": null,
  "byte_size": 245,
  "truncated": false,
  "storage_backend": "pg",
  "created_at": "2026-06-04T10:30:00Z"
}
```

**业务规则**：
- 管理员/数据合规角色：可查任意 request_id
- 普通员工：仅可查 user_id 匹配的记录，查他人返回 403
- 部门负责人：仅可查 dept_id 匹配的记录
- 所有访问记录审计日志

---

## 5. 错误码

### 统一错误响应

```json
{
  "error_code": "E4001",
  "message": "日期范围起始日期不能晚于结束日期",
  "request_id": "req_abc123"
}
```

### 错误码列表

| 错误码 | HTTP | 语义 | 触发条件 | 可重试 |
| :--- | :---: | :--- | :--- | :---: |
| `E4001` | 400 | InvalidRequest | from > to / 范围 > 90天 / 无效 group_by | 否 |
| `E4011` | 401 | Unauthorized | JWT 无效或缺失 | 否（需刷新 Token） |
| `E4031` | 403 | Forbidden | 非管理员查看他人调用内容 | 否 |
| `E4041` | 404 | NotFound | request_id 不存在 | 否 |
| `E5001` | 500 | InternalError | 数据库连接失败等未预期异常 | 是 |
| `E5031` | 503 | ServiceUnavailable | PostgreSQL 不可用 | 是 |

---

## 6. 版本策略与 Breaking Change 规则

**当前版本**：`v1`（URL 前缀 `/api/v1/`）

**Breaking Change**：删除端点、删除响应字段、修改字段类型、修改错误码语义

**非 Breaking Change**：新增端点、新增响应可选字段、新增错误码

**废弃流程**：Header 加 `Deprecated: true` + `Sunset` 日期，保留 ≥ 3 个月后移除

---

## 7. 非功能约束

| 约束类型 | 值 | 超出行为 |
| :--- | :--- | :--- |
| P95 查询延迟 | ≤ 200ms | 监控告警 |
| 单次请求超时 | 30s | E5031 |
| 日期范围 | ≤ 90 天 | E4001 |

---

## 8. 接口安全

- HTTPS 强制（生产环境）
- JWT 认证（复用 control-plane 中间件）
- ViewInjector 中间件 + Service 层双重 RBAC
- 越权尝试静默降级（不报错，避免信息泄漏）
- call_payloads 访问记录审计日志

---

## 9. 变更历史

| 版本号 | 更新时间 | 变更类型 | 变更描述 |
| :--- | :--- | :---: | :--- |
| v1.0.0 | 2026-06-04 | — | 初始版本（CHG-20260603-001） |
