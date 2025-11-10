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