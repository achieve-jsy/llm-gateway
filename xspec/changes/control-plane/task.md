# control-plane 任务清单 — CHG-20260603-001

> 基线：xspec/modules/control-plane/task.md

---

## 阶段 1：环境与基础

- [x] T001 P0 数据库迁移：models 表新增 price_mode 字段 `migrations/035_price_mode.sql`
  - ALTER TABLE models ADD COLUMN price_mode VARCHAR(10) NOT NULL DEFAULT 'catalog'
  - 存量 backfill：全部默认 catalog
  - AC-PRICE-04

- [x] T002 [P] P0 model_catalog 表同步补充缓存/阶梯价字段
  - catalog_sync_service 填充 cache_read_price / cache_write_price / tier_threshold / input_price_above_tier / output_price_above_tier
  - 来源：LiteLLM model_prices_and_context_window.json
  depends: T001

✓ 检查点：迁移执行成功，model_catalog 同步后含缓存/阶梯价字段

---

## 阶段 2：数据层

- [x] T003 P0 定义 ModelPricing 核心类型 `internal/core/model_pricing.go`
  - InputPerMtok / OutputPerMtok / CachedInputPerMtok / CacheWritePerMtok / Tiers
  - 单位：USD / 1M tokens

- [x] T004 [P] P0 更新 models GORM 模型 `internal/controlplane/model/model.go`
  - 新增 PriceMode 字段
  depends: T001

✓ 检查点：GORM 模型编译通过，数据库 AUTOMIGRATE 对齐

---

## 阶段 3：功能实现

### 3.1 US-PRICING-03 — 定价快照与历史锁定 (P0)

> **目标**：定价变更时通过 price_mode + 内存价表刷新实现"历史锁定"
> **独立验证**：变更模型定价 → ResolveModelPricing 返回新价 → 内存价表 5min 内刷新
> **AC 覆盖**：AC-PRICE-04, AC-PRICE-05

- [x] T005 P0 实现 ResolveModelPricing `internal/controlplane/pricing/resolver.go`
  - price_mode=catalog → 从 model_catalog 解析（含缓存/阶梯价）
  - price_mode=manual → 读 models.input_price/output_price
  - 缺价返回 (ModelPricing{}, true)
  - AC-PRICE-01, AC-PRICE-02
  depends: T003, T004

- [x] T006 P0 实现 PricingTable 内存价表 `internal/controlplane/pricing/table.go`
  - map[modelKey]core.ModelPricing
  - Refresh(ctx) 从 models JOIN model_catalog 全量加载
  - 5min cron 触发，atomic swap
  depends: T005

- [x] T007 [P] P1 实现阶梯定价 applyTier 逻辑 `internal/controlplane/pricing/tier.go`
  - 按 Token 数匹配区间上限
  - 区间不重叠校验
  - AC-PRICE-03
  depends: T005

- [x] T008 P0 注册内部接口 `GET /internal/v1/models/pricing` `internal/controlplane/handler/internal_handler.go`
  - 返回全量内存价表 JSON
  - 认证：X-Internal-Token
  depends: T006

✓ 检查点：内存价表刷新成功；ResolveModelPricing catalog 模式返回缓存/阶梯价；manual 模式返回实付价

### 3.2 US-PRICING-01 — 模型输入/输出单价配置 (P0)

- [x] T009 P0 ModelHandler 追加 price_mode 字段 `internal/controlplane/handler/model_handler.go`
  - POST /api/v1/models → 接收 price_mode + input_price + output_price
  - PUT /api/v1/models/:id → 同
  - GET /api/v1/models/:id → 返回 price_mode + effective_price
  depends: T004

- [x] T010 [P] P0 ModelService 定价校验 `internal/controlplane/service/model_service.go`
  - price_mode=manual 时 input_price/output_price 必须 > 0
  - 校验拒绝负数单价
  depends: T009

✓ 检查点：创建模型时可指定 price_mode；查询模型详情返回 effective_price

### 3.3 删除 diff/apply 闸门 (P0)

- [x] T011 P0 删除 ComputePriceDiff `internal/controlplane/service/catalog_service.go`
- [x] T012 [P] P0 删除 ApplyPriceDiff `internal/controlplane/service/catalog_service.go`
- [x] T013 [P] P0 删除路由 `internal/controlplane/router/router.go`
  - 删除 GET /model-catalog/diff
  - 删除 POST /model-catalog/apply-diff

✓ 检查点：编译通过，删除的路由 404 正常

---

## 阶段 4：集成与收尾

- [x] T014 P1 单元测试：ResolveModelPricing `controlplane/pricing/resolver_test.go`
  - catalog 模式含缓存/阶梯价
  - manual 模式
  - 缺价场景
  depends: T005

- [x] T015 [P] P1 单元测试：PricingTable 刷新 `controlplane/pricing/table_test.go`
  - 全量刷新、atomic swap、缺失模型
  depends: T006

- [ ] T016 [P] P1 集成测试：pricing API 端点 `tests/integration/pricing_api_test.go`
  - 创建模型含 price_mode、查询 effective_price
  - 内部接口返回全量价表
  depends: T008, T009

- [x] T017 P1 前端兼容性验证
  - ModelDialog / ModelListPage 新增 price_mode 开关
  - 删除「价格差异」tab
  - 确认 opencode 插件不受影响
  depends: T009, T013

✓ 检查点：全部 AC-PRICE-01~05 通过