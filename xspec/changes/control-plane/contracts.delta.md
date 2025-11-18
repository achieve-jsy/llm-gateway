# control-plane 接口契约变更 — CHG-20260603-001

> 基线：xspec/modules/control-plane/contracts.md (v1.4.0)

## 变更摘要

删除 2 个端点（diff/apply-diff），models 相关接口新增 price_mode 字段。新增内部接口供成本估算消费。

## 删除接口

| 接口 | 方法 | 路径 | 说明 |
| :--- | :--- | :--- | :--- |
| 价格差异查询 | GET | /api/v1/model-catalog/diff | 删除 |
| 应用价格差异 | POST | /api/v1/model-catalog/apply-diff | 删除 |

## 修改接口

### POST /api/v1/models — 创建模型

**请求体新增**：

```json
{
  "name": "qwen-plus",
  "provider_id": "uuid",
  "type": "chat",
  "price_mode": "catalog",
  "catalog_id": "uuid"
}
```

| 字段 | 类型 | 必填 | 变更 | 说明 |
| :--- | :--- | :--- | :--- | :--- |
| price_mode | string | 否 | 新增 | catalog=跟随目录价（默认），manual=使用 input_price/output_price |
| catalog_id | string | 否 | 新增 | 绑定 LiteLLM 目录条目 |

### PUT /api/v1/models/:id — 编辑模型

**请求体新增**：同创建接口，追加 price_mode 字段。

### GET /api/v1/models/:id — 查询模型详情

**响应体新增**：

```json
{
  "id": "uuid",
  "name": "qwen-plus",
  "price_mode": "catalog",
  "input_price": 0.002,
  "output_price": 0.006,
  "effective_price": {
    "input_per_mtok": 2.0,
    "output_per_mtok": 6.0,
    "cached_input_per_mtok": 0.25,
    "cache_write_per_mtok": 2.0
  }
}
```

| 字段 | 说明 |
| :--- | :--- |
| price_mode | catalog / manual |
| effective_price | 当前生效价格（catalog 模式从 model_catalog 解析，manual 模式从 input/output_price） |

## 新增内部接口

### GET /internal/v1/models/pricing — 查询全量价表

**认证**：内部 Token（X-Internal-Token Header）

> 供 gateway-api 和 observability-hub 消费，返回全量模型定价数据。

**成功响应** (200)：

```json
[
  {
    "model_key": "qwen-plus",
    "input_per_mtok": 2.0,
    "output_per_mtok": 6.0,
    "cached_input_per_mtok": 0.25,
    "cache_write_per_mtok": null,
    "tiers": [
      {
        "up_to_tokens": 200000,
        "input_per_mtok": 1.5,
        "output_per_mtok": 4.5
      }
    ]
  }
]
```

**错误响应**：

| HTTP 状态 | 错误码 | 说明 |
| :---: | :--- | :--- |
| 401 | UNAUTHORIZED | 内部 Token 无效 |