# gateway-api 接口契约变更 — CHG-20260526-001

> 基线：xspec/modules/gateway-api/contracts.md

## 变更摘要

修改 `GET /v1/models` 接口（原 `/v1/me/models`）：返回内容从 Key 绑定的模型组名称列表替代原来的个体模型列表。其他端点（`POST /v1/chat/completions` 等）接口签名不变，但行为变更：组名调用前增加了 Key 绑定范围校验。

## 修改接口

### GET /v1/models — 查询可用模型列表（替代原 GET /v1/me/models）

认证：Bearer Token (API Key)

> 返回当前 Key 绑定的模型组名称列表，格式兼容 OpenAI `/v1/models`。

**成功响应** (200)：

```json
{
  "object": "list",
  "data": [
    {
      "id": "日常推理",
      "object": "model",
      "created": 1716710400,
      "owned_by": "organization"
    },
    {
      "id": "高智能",
      "object": "model",
      "created": 1716710400,
      "owned_by": "organization"
    }
  ]
}
```

| 字段 | 类型 | 说明 |
| :--- | :--- | :--- |
| object | string | 固定值 `"list"` |
| data[].id | string | 模型组名称（即 publicName，作为调用时的 model 参数） |
| data[].object | string | 固定值 `"model"` |
| data[].created | integer | Unix 时间戳（秒） |
| data[].owned_by | string | 固定值 `"organization"` |

**与变更前差异**：

| 维度 | 变更前（/v1/me/models） | 变更后（/v1/models） |
| :--- | :--- | :--- |
| 端点路径 | `/v1/me/models` | `/v1/models` |
| 认证方式 | JWT (员工登录态) | API Key (Bearer sk-xxx) |
| data[].id 含义 | 个体模型名（如 `qwen-plus`） | 模型组名（如 `日常推理`） |
| 过滤依据 | user → department → 部门模型权限 | key → key_group_bindings → 组列表 |

**边界行为**：

| 场景 | 返回 |
| :--- | :--- |
| Key 未绑定任何模型组 | 空列表 `{"object":"list","data":[]}` |
| Key 绑定的组内无可用模型 | 组仍在列表中出现（不标注不可用——`/v1/models` 只列组，不负责可用性深度判断） |
| control-plane 超时（500ms） | 空列表，记录 WARN 日志 |

### POST /v1/chat/completions — 组名调用（行为变更）

**接口签名不变**。行为变更：model 字段传入的组名会被 `GroupAwareDispatcher` 校验是否在 Key 绑定范围内。

**错误响应（新增）**：

```json
{
  "error": {
    "message": "模型组不可用，请检查你的 API Key 是否有权使用该模型组",
    "type": "model_unavailable",
    "code": "model_unavailable"
  }
}
```

| 触发条件 | HTTP 状态 | 类型 |
| :--- | :---: | :--- |
| model 为组名且该组不在 Key 绑定范围 | 400 | model_unavailable |
| model 为组名但 control-plane 绑定校验不可达 | 400 | model_unavailable |

**与变更前差异**：

| 维度 | 变更前 | 变更后 |
| :--- | :--- | :--- |
| 组名校验 | 仅校验组名是否存在（GC 中） | 先校组名存在 + 再校是否在 Key 绑定范围内 |
| 校验源 | GroupNameCache（本地） | GroupNameCache（本地）+ control-plane（HTTP） |
| 未绑定组名 | 可调用（无绑定概念） | 被拒绝，返回 model_unavailable |