# gateway-core 接口契约变更 — CHG-20260603-001

> 基线：xspec/modules/gateway-core/spec.md (v1.0)

## 变更摘要

gateway-core 不新增对外接口，但新增了一个内部 API 消费（从 control-plane 拉取价表）。**对外 `/v1/*` 接口无任何变更**。

## 无对外接口变更

gateway-core 的公共 API（`/v1/models`、`/v1/chat/completions`、`/v1/embeddings`、`/health`、`/ready`）无变更。所有采集逻辑在现有中间件链中透明完成。

## 新增内部 API 消费

### GET /internal/v1/pricing（消费方）

**接口说明**：从 control-plane 拉取全量模型多桶价表，供 MemoryPricingCache 本地缓存。

**生产方**：control-plane（见 `xspec/changes/control-plane/contracts.delta.md`）

**消费方**：gateway-core — `internal/gateway/infra/pricing_cache.go`

**请求**：

```http
GET /internal/v1/pricing HTTP/1.1
Host: control-plane:8081
X-Internal-Token: {INTERNAL_TOKEN}
```

**响应格式（200）**：

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
| `models` | object | key 为模型名，value 为 ModelPricing |
| `models.{name}.prompt_price` | number | Prompt 单价（USD / 1M tokens） |
| `models.{name}.completion_price` | number | Completion 单价（USD / 1M tokens） |
| `models.{name}.reasoning_price` | number \| null | Reasoning 单价，null 表示无独立价格（降级用 completion_price） |
| `models.{name}.cached_price` | number \| null | 缓存命中单价，null 表示无独立价格（不计费） |

**认证方式**：`X-Internal-Token` Header（与现有 `/internal/v1/*` 鉴权方式一致）

**刷新策略**：gateway-core 每 5 分钟拉取一次，启动时立即拉取

**失败处理**：
- 拉取超时（10s）：保留上一份缓存，记录 `pricing_cache_refresh_errors_total`
- 返回非 200：同上
- 启动时拉取失败：使用空价表（所有调用标记 `pricing_missing`）

## 无移除/废弃接口

本次变更不涉及任何接口的移除或废弃。
