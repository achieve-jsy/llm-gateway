# control-plane 接口契约变更 — CHG-20260521-002

> 基线：xspec/modules/control-plane/contracts.md (v1.0)

## 变更摘要

新增 2 个同步模型相关端点。

## 新增接口

### POST /api/v1/providers/:id/sync-models

* **接口描述**：从上游厂商拉取模型列表并与本地对比
* **认证**：Bearer Token（JWT）
* **请求参数**：无 body，provider ID 在 URL path 中

**成功响应**（200）：

```json
{
  "data": {
    "added": [{"id": "qwen-max", "owned_by": "dashscope"}],
    "existing": [{"id": "qwen-turbo", "owned_by": "dashscope"}],
    "removed": [{"id": "old-model", "owned_by": "dashscope"}],
    "total_upstream": 244,
    "total_local": 5,
    "unsupported": false
  }
}
```

**错误响应**：

| HTTP 状态 | error.code | 触发条件 |
| :--- | :--- | :--- |
| 400 | provider_disabled | 渠道未启用 |
| 400 | sync_not_supported | 渠道类型不支持自动同步（Anthropic/Bedrock） |
| 404 | not_found | 渠道不存在 |
| 502 | upstream_error | 厂商 API 调用失败 |

---

### POST /api/v1/providers/:id/sync-models/confirm

* **接口描述**：确认批量注册选中的模型 + 禁用已下架模型
* **认证**：Bearer Token（JWT）

**请求体**：

| 参数名 | 类型 | 必填 | 描述 |
| :--- | :--- | :--- | :--- |
| models_to_add | string[] | 是 | 要注册的模型名称列表 |
| disable_removed | boolean | 否 | 是否禁用已下架模型，默认 true |

**成功响应**（200）：

```json
{
  "data": {
    "registered": 5,
    "skipped": 1,
    "disabled": 2
  }
}
```

**错误响应**：

| HTTP 状态 | error.code | 触发条件 |
| :--- | :--- | :--- |
| 400 | invalid_request | models_to_add 为空 |
| 404 | not_found | 渠道不存在 |
