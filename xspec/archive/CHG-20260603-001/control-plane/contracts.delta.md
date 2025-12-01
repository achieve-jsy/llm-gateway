# control-plane 接口契约变更 — CHG-20260603-001

> 基线：xspec/modules/control-plane/spec.md (v1.0)

## 变更摘要

新增 1 个内部接口（GET /internal/v1/pricing），修改 2 个管理接口（模型创建/更新），删除 2 个接口（diff/apply-diff）。

## 新增接口

### GET /internal/v1/pricing — 全量价表下发

**接口说明**：返回所有已配置模型的四桶价格（含降级逻辑），供 gateway-core MemoryPricingCache 定时拉取。

**路由**：`GET /internal/v1/pricing`

**认证**：`InternalAuth` — 请求头 `X-Internal-Token: {INTERNAL_TOKEN}`

**请求参数**：无

**成功响应（200）**：

```json
{
  "models": {
    "deepseek-chat": {
      "prompt_price": 0.29,
      "completion_price": 1.76,
      "reasoning_price": null,
      "cached_price": null
    },
    "qwen3.6-plus": {
      "prompt_price": 0.29,
      "completion_price": 1.76,
      "reasoning_price": 3.52,
      "cached_price": null
    },
    "claude-sonnet-4-20250514": {
      "prompt_price": 3.0,
      "completion_price": 15.0,
      "reasoning_price": null,
      "cached_price": 0.15
    }
  }
}
```

**响应字段说明**：

| 字段 | 类型 | 说明 |
| :--- | :--- | :--- |
| `models` | object | key 为模型名 |
| `models.{name}.prompt_price` | number | Prompt 单价（USD / 1M tokens） |
| `models.{name}.completion_price` | number | Completion 单价（USD / 1M tokens） |
| `models.{name}.reasoning_price` | number \| null | Reasoning 单价，null = 降级到 completion_price |
| `models.{name}.cached_price` | number \| null | 缓存命中单价，null = 不计费（0） |

**错误响应**：

| 状态码 | 错误码 | 说明 |
| :--- | :--- | :--- |
| 401 | E4011 | `X-Internal-Token` 无效或缺失 |
| 500 | E5001 | 数据库查询失败 |

## 修改接口

### POST /api/v1/models — 创建模型（修改）

**请求体新增字段**：

| 字段 | 类型 | 必填 | 说明 |
| :--- | :--- | :--- | :--- |
| `price_mode` | string | 否 | 定价模式：`catalog`（默认）/ `manual` |
| `reasoning_price` | number \| null | 否 | 推理 token 单价（USD/1M tokens），manual 模式可选 |
| `cached_price` | number \| null | 否 | 缓存命中单价（USD/1M tokens），manual 模式可选 |

**校验规则**：

| 条件 | 行为 |
| :--- | :--- |
| `price_mode = "manual"` 且 `input_price` ≤ 0 | 返回 400：`"手动模式下 Prompt 单价必须大于 0"` |
| `price_mode = "manual"` 且 `output_price` ≤ 0 | 返回 400：`"手动模式下 Completion 单价必须大于 0"` |
| `price_mode` 既非 `catalog` 也非 `manual` | 返回 400：`"price_mode 必须为 'catalog' 或 'manual'"` |

### PUT /api/v1/models/:id — 更新模型（修改）

**请求体新增字段**：同 POST 接口。

**校验规则**：同 POST 接口的 manual 模式校验，外加：

| 条件 | 行为 |
| :--- | :--- |
| 切换 `catalog` → `manual`，`input_price` 或 `output_price` ≤ 0 | 返回 400 |
| 切换 `manual` → `catalog` | 清除 `reasoning_price` 和 `cached_price`（设为 null），此后价格跟随 catalog |

## 删除接口

| 方法 | 路径 | 原用途 | 替代方案 |
| :--- | :--- | :--- | :--- |
| GET | /api/v1/model-catalog/diff | 计算价格差异 | 不再需要（catalog 价格实时生效） |
| POST | /api/v1/model-catalog/apply-diff | 应用价格差异 | 不再需要（catalog 价格实时生效） |

**客户端影响**：调用上述端点的客户端将收到 `404 Not Found`。

## 无影响的接口

以下接口不变：

| 接口 | 说明 |
| :--- | :--- |
| POST /api/v1/model-catalog/sync | Catalog 同步（新增字段导入对调用方透明） |
| GET /api/v1/model-catalog/sync-status | 同步状态查询 |
| GET /api/v1/model-catalog/search | Catalog 搜索 |
| POST /api/v1/model-catalog/auto-bind | 自动绑定 |
| POST /api/v1/model-catalog/batch-bind | 批量绑定 |
| GET /api/v1/models | 模型列表（响应新增 price_mode 等字段） |
| GET /api/v1/models/:id | 模型详情（响应新增 price_mode 等字段） |
