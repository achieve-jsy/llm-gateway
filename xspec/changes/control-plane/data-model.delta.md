# control-plane 数据模型变更 — CHG-20260603-001

> 基线：xspec/modules/control-plane/data-model.md (v1.4)

## 变更摘要

models 表新增 `price_mode` 字段（catalog/manual），input_price / output_price 列保留但语义变更。新增 model_catalog 缓存/阶梯价字段。

## 修改表

### models 表新增字段

```sql
ALTER TABLE models
  ADD COLUMN price_mode VARCHAR(10) NOT NULL DEFAULT 'catalog';
```

| 字段 | 类型 | 说明 |
| :--- | :--- | :--- |
| price_mode | VARCHAR(10) | catalog=读取时按 catalog_id 解析 LiteLLM 价；manual=使用本表 input/output_price |

**存量 backfill**：全部默认 catalog（运行时未用价，零风险）。

**回滚**：`DROP COLUMN price_mode`（input/output_price 列保留，语义还原）。

### model_catalog 表新增字段

同步 LiteLLM 价格时填充缓存/阶梯价字段（已有字段，同步逻辑补充）：

| 字段 | 说明 |
| :--- | :--- |
| cache_read_price | 缓存读取单价（来自 LiteLLM cache_read_input_token_cost） |
| cache_write_price | 缓存写入单价（来自 LiteLLM cache_creation_input_token_cost） |
| tier_threshold | 阶梯 Token 阈值 |
| input_price_above_tier | 超出阶梯的输入单价 |
| output_price_above_tier | 超出阶梯的输出单价 |

## 删除

无物理删除。input_price / output_price 列保留，语义从"模型定价"变为"仅 manual 模式生效的实付价"。

## 迁移方案

### 迁移 035

```sql
-- 035_price_mode.up.sql
ALTER TABLE models ADD COLUMN price_mode VARCHAR(10) NOT NULL DEFAULT 'catalog';
```

```sql
-- 035_price_mode.down.sql
ALTER TABLE models DROP COLUMN price_mode;
```

### 影响面

| 面 | 影响 |
| :--- | :--- |
| 线上路由 | 无（运行时本就不读价） |
| /v1/model/info | 无（仍走 catalog JOIN 能力字段） |
| API | 删除 diff / apply-diff 两个端点 |
| 成本估算 | 新增 ResolveModelPricing + 内存价表为唯一取价入口 |

### 与 opencode 插件兼容性

opencode 插件（tools/opencode-models-discovery）只调用 /v1/models 与 /v1/model/info，不消费价格字段。本次改造不影响插件行为。