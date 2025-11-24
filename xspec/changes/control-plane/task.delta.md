# control-plane 后端任务清单

## 文档信息

| 属性 | 内容 |
| :--- | :--- |
| **所属变更** | CHG-20260603-001 |
| **模块名称** | control-plane |
| **模块类型** | web-backend |
| **创建时间** | 2026-06-04 |

---

## 任务格式

```
- [ ] T{NNN} [P?] {优先级} 任务描述（含具体文件路径）
  depends: T{NNN}, T{NNN}@{other-module}
```

| 标记 | 含义 |
| :--- | :--- |
| `T{NNN}` | 模块内唯一编号，从 T001 起编 |
| `[P]` | 可并行：操作不同文件、无依赖未完成任务 |
| 优先级 | P0（必须）/ P1（重要）/ P2（可选） |
| `depends:` | 依赖标注（缩进换行）。模块内：`T010`；跨模块：`T020@gateway-core`。无依赖则不写此行 |

---

## 阶段 1：环境与基础

**目标**：ModelPricing 结构体、CatalogSync 字段扩展、DB 迁移就绪

- [x] T001 [P] P0 ModelPricing 四桶结构体定义，新建或扩展 `internal/controlplane/model/pricing.go`（PromptPrice / CompletionPrice / ReasoningPrice *float64 / CachedPrice *float64 / PricingMissing / FallbackReasoning）
- [x] T002 [P] P0 Model GORM 实体扩展 `internal/controlplane/model/model.go`（新增字段 PriceMode string `gorm:"default:catalog"` / ReasoningPrice *float64 / CachedPrice *float64 及 Validate 方法：manual 模式校验 input_price > 0 && output_price > 0）
- [x] T003 [P] P0 ModelCatalog GORM 实体扩展 `internal/controlplane/model/model_catalog.go`（新增字段 ReasoningPrice *float64 / CachedPrice *float64）
- [x] T004 P0 数据库迁移脚本 `migrations/033_add_pricing_fields.sql`（model_catalog 新增 reasoning_price NUMERIC / cached_price NUMERIC 列；models 新增 price_mode VARCHAR(20) DEFAULT 'catalog' / reasoning_price NUMERIC / cached_price NUMERIC 列，全部 ADD COLUMN IF NOT EXISTS）

**✓ 检查点**：ModelPricing 结构体编译通过，迁移脚本可执行

---

## 阶段 2：数据层

**目标**（与阶段 1 合并，数据模型在阶段 1 已定义，无独立 Repository 新建）

**✓ 检查点**：已有 Repository 可正常使用新增字段

---

## 阶段 3：功能实现

**目标**：按用户故事实现定价改造——Catalog 同步字段扩展 → 统一取价 → price_mode 管理 → 价表下发 → 清理 diff

### US-NEW-02 — 统一取价入口 ResolveModelPricing (P0)

**目标**：提供 ResolveModelPricing 函数作为唯一价格解析入口，按 price_mode 决定取价来源，输出标准四桶 ModelPricing
**独立验证**：调用 ResolveModelPricing(catalog 模式模型) 返回 catalog 价格；调用 ResolveModelPricing(manual 模式模型) 返回手填价格
**对应验收标准**：AC-NEW-01, AC-NEW-02, AC-NEW-05, AC-NEW-07, AC-NEW-08, AC-NEW-09

- [x] T010 P0 ResolveModelPricing 实现 `internal/controlplane/service/pricing_service.go`（新建；函数签名 `ResolveModelPricing(ctx, modelID) → *ModelPricing, error`；逻辑：查 models 表获取 price_mode + catalog_id；catalog 模式 JOIN model_catalog 取四桶价格；manual 模式直接取 models 表字段；降级规则：reasoning_price 为 NULL → 用 completion_price + fallback_reasoning=true；cached_price 为 NULL → 0；prompt/completion 缺失 → pricing_missing=true）
  depends: T001, T002, T003

**✓ 检查点**：catalog 模式模型返回 LiteLLM 同步价格，manual 模式返回手填价格，降级逻辑正确

---

### US-NEW-01 — 模型定价双来源 (P0)

**目标**：Catalog 同步导入 reasoning/cached 分桶价格；模型支持 price_mode 切换（catalog/manual），manual 模式校验 input/output_price > 0
**独立验证**：同步 Catalog 后，qwen3.6-plus 的 reasoning_price 正确导入；模型切换 manual 模式后校验生效
**对应验收标准**：AC-NEW-01, AC-NEW-03, AC-NEW-04, AC-NEW-07, AC-NEW-08, AC-NEW-09

- [x] T020 P0 CatalogSyncService 扩展 `internal/controlplane/service/catalog_sync_service.go`（在 LiteLLM JSON 解析中新增：output_cost_per_reasoning_token → reasoning_price；cache_read_input_token_cost → cached_price；两个字段均可为 null）
  depends: T003
- [x] T021 P0 ModelService 创建模型扩展 `internal/controlplane/service/model_service.go`（Create 方法新增 price_mode 字段处理；默认值 "catalog"；manual 模式校验 input_price > 0 && output_price > 0；reasoning_price / cached_price 可选）
  depends: T002, T010
- [x] T022 P0 ModelService 更新模型扩展 `internal/controlplane/service/model_service.go`（Update 方法新增：catalog → manual 切换校验 input_price/output_price > 0；manual → catalog 切换清除 reasoning_price / cached_price 为 null）
  depends: T021

**✓ 检查点**：Catalog 同步后 model_catalog 含 reasoning/cached 价格；模型创建/更新 price_mode 校验正确

---

### US-NEW-02（续）— 价表下发 API (P0)

**目标**：新增 GET /internal/v1/pricing 接口，返回全量模型四桶价格，供 gateway-core MemoryPricingCache 定时拉取
**独立验证**：curl GET /internal/v1/pricing 返回所有模型的四桶价格
**对应验收标准**：AC-NEW-01, AC-NEW-02

- [x] T030 P0 PricingHandler 实现 `internal/controlplane/handler/pricing_handler.go`（新建；GetAllPricing 方法：查询所有 models 行，逐模型调用 ResolveModelPricing，组装 map[modelName]ModelPricing JSON 响应；认证 InternalAuth: X-Internal-Token）
  depends: T010
- [x] T031 P0 路由注册 `internal/controlplane/router/router.go`（在 internal := r.Group("/internal/v1") 下注册 `internal.GET("/pricing", pricingHandler.GetAllPricing)`，中间件 InternalAuth()）
  depends: T030

**✓ 检查点**：gateway-core 可通过 GET /internal/v1/pricing 拉取全量四桶价表

---

### US-NEW-03 — 移除 diff/apply-diff 闸门 (P1)

**目标**：删除 Catalog 价格差异相关端点和逻辑，简化管理员心智模型
**独立验证**：GET /api/v1/model-catalog/diff 返回 404；同步后不再触发 ComputePriceDiff
**对应验收标准**：AC-NEW-06

- [x] T040 P1 删除 diff/apply-diff 端点 `internal/controlplane/handler/catalog_handler.go`（删除 ComputePriceDiff / ApplyPriceDiff 方法及对应路由注册；删除 GET /api/v1/model-catalog/diff、POST /api/v1/model-catalog/apply-diff）
- [x] T041 [P] P1 删除 diff 逻辑 `internal/controlplane/service/catalog_service.go`（删除 ComputePriceDiff / ApplyPriceDiff 函数；同步完成后不再触发任何 diff 比对逻辑）
  depends: T040
- [x] T042 [P] P1 ModelService 响应扩展（模型列表/详情 GET 接口的响应 JSON 新增 price_mode / reasoning_price / cached_price 字段）
  depends: T002

**✓ 检查点**：diff/apply-diff 端点 404，同步后无 diff 日志

---

## 阶段 4：集成与收尾

**目标**：测试、编译验证、宪章合规

- [x] T050 P0 单元测试 `tests/controlplane/service/pricing_service_test.go`（Cover: catalog 模式取 catalog 价、manual 模式取手填价、reasoning 降级、cached=null 计 0、缺价 pricing_missing、未绑定 catalog_id 缺价）
  depends: T010
- [ ] T051 [P] P0 单元测试 `tests/controlplane/service/model_service_test.go`（Cover: 创建 manual 模式校验 input/output_price > 0、catalog→manual 切换校验、manual→catalog 切换清除）
  depends: T021
- [ ] T052 [P] P0 单元测试 `tests/controlplane/handler/pricing_handler_test.go`（Cover: InternalAuth 校验、正常返回全量价表、空模型列表）
  depends: T030
- [x] T053 P0 编译验证 `go build ./cmd/control-plane` + `go test ./internal/controlplane/... -count=1`
  depends: T031, T040
- [ ] T054 [P] P1 AI Slop 清理（检查新增文件无 AI 代码异味）
  depends: T053

**✓ 检查点**：所有测试通过，编译无警告

---

## 交付策略

### MVP 优先（仅 P0）
1. 完成阶段 1（T001-T004）
2. 完成 ResolveModelPricing（T010）→ 取价入口可用
3. 完成 Catalog 同步扩展（T020）→ 分桶价格导入
4. 完成 ModelService 扩展（T021-T022）→ price_mode 管理
5. 完成价表下发 API（T030-T031）→ gateway-core 可拉取价表
6. MVP 可验证：gateway-core BatchWriter 可正确计算多桶成本

### 场景间依赖
- US-NEW-02 (ResolveModelPricing)：依赖 T001-T003 结构体定义
- US-NEW-01 (CatalogSync + ModelService)：依赖 US-NEW-02 ResolveModelPricing
- 价表下发 (T030)：依赖 US-NEW-02 ResolveModelPricing
- US-NEW-03 (移除 diff)：独立，可并行

---

## 任务统计

| 指标 | 数值 |
| :--- | :--- |
| 总任务数 | 19 |
| P0（必须） | 13 |
| P1（重要） | 6 |
| P2（可选） | 0 |
| 可并行任务 | 8 |
| 预计工时 | 24h |
