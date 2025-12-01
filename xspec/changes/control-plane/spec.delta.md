# control-plane 需求变更 — CHG-20260603-001

> 基线：xspec/modules/control-plane/spec.md (v1.0)

## 变更摘要

模型定价体系改造：从「手工 apply-diff 抄价」改为「读取时解析」，默认直读 LiteLLM 目录价格；Catalog 同步导入多桶价格（reasoning/cached 分桶）；新增统一取价入口 ResolveModelPricing 供成本估算使用，返回完整的多桶报价结构；移除 diff/apply-diff 接口。

## 新增需求

### US-NEW-01：模型定价双来源（P0）

作为管理员，模型的有效价格默认自动跟随 Catalog 同步的 LiteLLM 价格（含缓存价/阶梯价），无需手工操作；当需要使用实付价时，可将某模型切换为 manual 模式并手填单价。

- 新增字段 price_mode：catalog（默认，直读 model_catalog）/ manual（使用 models 表的自定义价格）
- catalog 模式下：定价随 Catalog 同步自动更新，管理员零维护
- manual 模式下：允许为每个价格桶独立填价（prompt / completion / reasoning / cached），切换时强制至少 prompt 和 completion 非零
- models 表已有 input_price / output_price 字段扩展为：input_price / output_price / reasoning_price / cached_price（仅 manual 模式生效）
- Catalog 同步时新增导入字段：`output_cost_per_reasoning_token`、`cache_read_input_token_cost`

### US-NEW-02：统一取价入口 ResolveModelPricing（P0）

作为系统，提供 ResolveModelPricing 函数作为唯一的价格解析入口，按 price_mode 决定从 model_catalog 还是 models 表取价，输出标准化的 ModelPricing 结构（含 prompt/completion/reasoning/cached 四桶单价）。

- 供 gateway-core 的 collector 通过内存价表查询使用
- 内存价表定时刷新（如 5min），从 models JOIN model_catalog 构建
- 模型缺价时返回空 + missing 标记，调用方据此计 est_cost=0 并告警
- 某桶无独立价格时在结构中标记 fallback，由成本估算引擎按降级规则处理

### US-NEW-03：移除 diff/apply-diff 闸门（P1）

作为系统，删除 ComputePriceDiff / ApplyPriceDiff 逻辑及对应的 API 端点，简化管理员心智模型。

- 删除：GET /api/v1/model-catalog/diff
- 删除：POST /api/v1/model-catalog/apply-diff
- 保留：auto-bind / batch-bind-catalog（仍需 catalog_id 建立模型-目录对应关系）
- 保留：sync / sync-status / search / by-source（目录同步与检索）

## 修改需求

- 原 spec §6.2「模型管理」→ 模型创建/编辑增加 price_mode 字段（catalog/manual），编辑时如切换到 manual 需校验 input_price / output_price 非零
- 原 spec §6.5「Catalog 同步」→ 同步完成后不再触发价格 diff 比对；catalog 价格变更直接通过 catalog 模式的模型实时生效

## 新增验收标准

| 编号 | Given（前置状态） | When（触发动作） | Then（预期结果） | 优先级 |
| :--- | :--- | :--- | :--- | :--- |
| AC-NEW-01 | Given 模型 A 的 price_mode=catalog 且 model_catalog 有该模型的价格 | When 成本估算请求该模型的价格 | Then 返回 model_catalog 中的完整价格（含缓存价/阶梯价） | P0 |
| AC-NEW-02 | Given 模型 B 的 price_mode=manual 且 input_price=3.0, output_price=15.0 | When 成本估算请求该模型的价格 | Then 返回手填的 input=3.0, output=15.0（忽略 catalog 价格） | P0 |
| AC-NEW-03 | Given Catalog 同步更新了某模型的价格 | When price_mode=catalog 的该模型下次被调用 | Then 使用更新后的新价格计算成本 | P0 |
| AC-NEW-04 | Given 管理员尝试将模型切换为 manual 模式 | When input_price 或 output_price 为 0 | Then 校验失败，返回错误提示 | P0 |
| AC-NEW-05 | Given 某模型 price_mode=catalog 但未绑定 catalog_id | When 成本估算请求价格 | Then 返回缺价标记（missing=true），est_cost=0 | P1 |
| AC-NEW-06 | Given 客户端请求 GET /api/v1/model-catalog/diff | When 请求发出 | Then 返回 404（端点已移除） | P1 |
| AC-NEW-07 | Given Catalog 同步了 qwen3.6-plus 的 output_cost_per_reasoning_token=$3.52/M | When ResolveModelPricing(qwen3.6-plus) | Then 返回四桶价格，reasoning_price=$3.52/M | P0 |
| AC-NEW-08 | Given 管理员将模型切换为 manual 模式 | When 填入 prompt/completion 价但 reasoning_price 留空 | Then reasoning_cost 在成本估算时标记 fallback，按 completion 价降级 | P0 |
| AC-NEW-09 | Given Catalog 同步了某模型的 cache_read_input_token_cost=$0.15/M | When ResolveModelPricing | Then cached_price=$0.15/M | P0 |

## 修改验收标准

- 原 AC-05（Catalog 同步后 model_catalog 正确更新）→ 不变，但不再要求触发 diff 比对
