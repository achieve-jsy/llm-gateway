# control-plane 数据模型变更 — CHG-20260603-001

> 基线：xspec/modules/control-plane/spec.md (v1.0)

## 变更摘要

model_catalog 表新增 `reasoning_price`、`cached_price` 列（LiteLLM 同步导入）；models 表新增 `price_mode`、`reasoning_price`、`cached_price` 列（手动定价支持）。

## model_catalog 表变更

### 新增列

```sql
ALTER TABLE model_catalog
    ADD COLUMN IF NOT EXISTS reasoning_price NUMERIC DEFAULT NULL,
    ADD COLUMN IF NOT EXISTS cached_price    NUMERIC DEFAULT NULL;
```

### 字段说明

| 列名 | 类型 | 说明 | 来源 |
| :--- | :--- | :--- | :--- |
| `reasoning_price` | NUMERIC | 推理 token 单价（USD / 1M tokens） | LiteLLM `output_cost_per_reasoning_token` |
| `cached_price` | NUMERIC | 缓存命中 token 单价（USD / 1M tokens） | LiteLLM `cache_read_input_token_cost` |

### 已有字段（不变）

| 列名 | 类型 | 说明 |
| :--- | :--- | :--- |
| `id` | UUID PK | 主键 |
| `model_name` | VARCHAR(255) | 模型名称 |
| `input_price` | NUMERIC | Prompt 单价 |
| `output_price` | NUMERIC | Completion 单价 |
| ... | ... | 其他已有字段 |

## models 表变更

### 新增列

```sql
ALTER TABLE models
    ADD COLUMN IF NOT EXISTS price_mode      VARCHAR(20) NOT NULL DEFAULT 'catalog',
    ADD COLUMN IF NOT EXISTS reasoning_price NUMERIC DEFAULT NULL,
    ADD COLUMN IF NOT EXISTS cached_price    NUMERIC DEFAULT NULL;
```

### 字段说明

| 列名 | 类型 | 说明 |
| :--- | :--- | :--- |
| `price_mode` | VARCHAR(20) | 定价模式：`catalog`（跟随目录）/ `manual`（手动填写） |
| `reasoning_price` | NUMERIC | 推理 token 单价（USD / 1M tokens），仅 manual 模式生效 |
| `cached_price` | NUMERIC | 缓存命中 token 单价（USD / 1M tokens），仅 manual 模式生效 |

### 已有字段（不变）

| 列名 | 类型 | 说明 |
| :--- | :--- | :--- |
| `id` | UUID PK | 主键 |
| `name` | VARCHAR(255) | 模型名 |
| `input_price` | NUMERIC | Prompt 单价（manual 模式使用） |
| `output_price` | NUMERIC | Completion 单价（manual 模式使用） |
| `catalog_id` | UUID FK → model_catalog | 关联的 Catalog 条目 |
| ... | ... | 其他已有字段 |

### price_mode 约束

应用层校验（GORM 不强制 ENUM）：

```go
const (
    PriceModeCatalog = "catalog"
    PriceModeManual  = "manual"
)

func (m *Model) Validate() error {
    if m.PriceMode != PriceModeCatalog && m.PriceMode != PriceModeManual {
        return errors.New("price_mode must be 'catalog' or 'manual'")
    }
    if m.PriceMode == PriceModeManual {
        if m.InputPrice == nil || *m.InputPrice <= 0 {
            return errors.New("manual mode requires input_price > 0")
        }
        if m.OutputPrice == nil || *m.OutputPrice <= 0 {
            return errors.New("manual mode requires output_price > 0")
        }
    }
    return nil
}
```

## 迁移文件

迁移脚本：`migrations/NNN_add_pricing_fields.sql`

```sql
-- NNN_add_pricing_fields.sql
-- 变更：CHG-20260603-001 — 用量采集与成本估算
-- 描述：model_catalog 新增 reasoning/cached 价格列；models 新增 price_mode 及手动价格列

-- 1. model_catalog 新增分桶价格列
ALTER TABLE model_catalog
    ADD COLUMN IF NOT EXISTS reasoning_price NUMERIC DEFAULT NULL,
    ADD COLUMN IF NOT EXISTS cached_price    NUMERIC DEFAULT NULL;

-- 2. models 新增 price_mode + 手动价格列
ALTER TABLE models
    ADD COLUMN IF NOT EXISTS price_mode      VARCHAR(20) NOT NULL DEFAULT 'catalog',
    ADD COLUMN IF NOT EXISTS reasoning_price NUMERIC DEFAULT NULL,
    ADD COLUMN IF NOT EXISTS cached_price    NUMERIC DEFAULT NULL;
```

## 注意事项

- 所有新增列 `DEFAULT NULL`，已有行不受影响
- `price_mode` 默认 `'catalog'`，已有模型的行为完全不变（继续跟随 catalog 价格）
- `NUMERIC` 类型（无精度限制）用于价格字段，与 LiteLLM 的 `float` 精度匹配
- 手动模式下 `reasoning_price` / `cached_price` 为 NULL 时，取价逻辑按降级规则处理（见 design.delta.md）
