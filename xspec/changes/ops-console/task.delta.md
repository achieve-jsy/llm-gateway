# ops-console 前端任务清单

## 文档信息

| 属性 | 内容 |
| :--- | :--- |
| **所属变更** | CHG-20260603-001 |
| **模块名称** | ops-console |
| **模块类型** | web-frontend |
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
| `depends:` | 依赖标注（缩进换行）。模块内：`T010`；跨模块：`T020@observability-hub`。无依赖则不写此行 |

---

## 阶段 1：环境与基础

**目标**：类型定义、路由配置、API 封装、状态管理就绪

- [ ] T001 [P] P0 用量相关 TypeScript 类型定义 `web/src/types/usage.ts`（新建：UsageItem / UsageSummary / UsageTrendPoint / UsageQueryParams / UsageResponse / PayloadData，对齐 data-model.delta.md §新增类型定义）
- [ ] T002 [P] P0 模型定价类型扩展 `web/src/types/model.ts`（新增 ModelPriceBucket 接口：prompt_price / completion_price / reasoning_price? / cached_price?；ModelFormData 扩展 price_mode / reasoning_price / cached_price 字段）
- [ ] T003 P0 路由配置 `web/src/router/index.ts`（新增 `/admin/usage` → UsagePage + `/admin/usage/global` → GlobalUsagePage，后者 meta.roles: ['admin']；新增路由守卫：非管理员访问 global 时重定向到 `/admin/usage` + toast 提示）
- [ ] T004 [P] P0 API 调用层 `web/src/api/usage.ts`（新建：getUsage(params) → GET /v1/usage；getPayload(requestId) → GET /v1/usage/payload/:id；统一错误处理 handleApiError：400/401/403/404/500 映射）
  depends: T001
- [ ] T005 [P] P0 Pinia usageStore `web/src/stores/usage.ts`（新建：items / summary / trend / loading / error / filters 状态；fetchUsage / fetchTrend / resetFilters actions）
  depends: T001

**✓ 检查点**：类型编译通过，路由可访问空白壳页面，API 层和 Store 就绪

---

## 阶段 2：数据层

（与阶段 1 合并，前端无独立数据层）

---

## 阶段 3：功能实现

**目标**：按用户故事实现页面——个人用量页 → 全局看板 → Catalog UI 简化 → 模型编辑多桶定价

### US-NEW-01 — 个人用量页 (P0)

**目标**：员工可查看自己的用量详情（总量卡片 / 按模型拆分表格 / 延迟指标 / 趋势图）和用量趋势
**独立验证**：登录后访问 `/admin/usage`，选择日期范围后展示概览卡片 + 模型表格 + 延迟指标 + 趋势图
**对应验收标准**：AC-NEW-01, AC-NEW-02, AC-NEW-03, AC-NEW-08, AC-NEW-09, AC-NEW-10

- [ ] T020 [P] P0 UsageTrendChart 共享组件 `web/src/components/UsageTrendChart.vue`（新建；Props: data/ metric/ granularity/ loading；echarts 折线/柱状图；粒度切换 el-radio-group 日/周/月 emit change；tooltip 显示数值+日期；window resize 自适应）
- [ ] T021 P0 UsagePage 页面 `web/src/views/admin/UsagePage.vue`（新建；布局：日期选择器 + 概览卡片（总请求数/Token/成本） + 模型拆分表格 + 延迟指标卡片 + UsageTrendChart×2（用量趋势 + 延迟趋势）；表格含 reasoning_cost / cached_cost / avg_ttft / p95_ttft 列；高消耗行 highlight-row CSS class）
  depends: T003, T004, T005, T020
- [ ] T022 P1 UsagePage 三态处理（loading: el-skeleton 骨架屏模拟；empty: el-empty "暂无用量数据"；error: el-alert type="error" + 重试按钮）
  depends: T021

**✓ 检查点**：个人用量页可独立访问，展示总量/模型/延迟/趋势，loading/empty/error 三态正常

---

### US-NEW-02 — 全局用量看板 (P1)

**目标**：管理员查看全公司用量概览（总量 + 按模型维度排行 + 按部门维度排行 + 趋势图）
**独立验证**：管理员登录访问 `/admin/usage/global`，选择日期范围后展示全公司总量 + 模型/部门排行
**对应验收标准**：AC-NEW-04, AC-NEW-05

- [ ] T030 P1 GlobalUsagePage 页面 `web/src/views/admin/GlobalUsagePage.vue`（新建；布局：日期选择器 + 概览卡片（全公司总量） + ECharts 柱状图×2（按模型排行 + 按部门排行）+ UsageTrendChart 全公司趋势；路由守卫拦截非管理员重定向 + toast 提示）
  depends: T003, T004, T005, T020
- [ ] T031 [P] P1 GlobalUsagePage 三态处理（loading: el-skeleton；empty: el-empty；error: el-alert + 重试）
  depends: T030

**✓ 检查点**：管理员可访问全局看板，非管理员被路由守卫拦截

---

### US-NEW-03 — Catalog UI 简化：删除价格差异 Tab (P1)

**目标**：Catalog 管理页面移除「价格差异」tab 及相关组件，仅保留「目录同步」和「模型对应」
**独立验证**：访问 Catalog 页面，无「价格差异」tab
**对应验收标准**：AC-NEW-06

- [ ] T040 P1 CatalogPage 简化 `web/src/views/admin/CatalogPage.vue`（删除 el-tab-pane label="价格差异"；删除 PriceDiffPanel.vue 组件引用；删除 ApplyDiffButton.vue 组件引用；删除 fetchPriceDiff / applyPriceDiff API 调用；删除 diffItems / diffLoading 状态变量）

**✓ 检查点**：Catalog 页面仅剩「目录同步」+「模型对应」两个 tab

---

### US-NEW-04 — 模型编辑增加 price_mode 开关 (P1)

**目标**：模型创建/编辑对话框增加「定价模式」开关，manual 模式展示四桶定价输入框
**独立验证**：编辑模型，切换 price_mode 为 manual 后展示四桶输入，Prompt/Completion 必填校验
**对应验收标准**：AC-NEW-07, AC-NEW-11

- [ ] T050 P1 ModelEditDialog 多桶定价 `web/src/views/admin/components/ModelEditDialog.vue`（新增 price_mode el-radio-group：跟随目录（默认）/ 手动填写；catalog 模式：只读展示 catalog 四桶价格；manual 模式：展示四桶 el-input-number（Prompt/Completion 必填 + 校验 > 0；Reasoning/Cached 选填留空降级）；切换 manual→catalog 时清除手填价格字段；单位标注 USD/1M tokens）
  depends: T002
- [ ] T051 [P] P1 模型表单提交校验 `web/src/api/model.ts`（createModel / updateModel 请求体新增 price_mode / reasoning_price / cached_price 字段；提交前调用 validateModelForm：manual 模式校验 input_price > 0 && output_price > 0）
  depends: T050

**✓ 检查点**：模型编辑可切换 price_mode，manual 模式校验生效

---

## 阶段 4：集成与收尾

**目标**：联调验证、三态检查、安全性、性能

- [ ] T060 P1 与 observability-hub 接口联调（确认 GET /v1/usage 返回数据格式与前端类型一致；确认 GET /v1/usage/payload/:id 权限校验；确认三态覆盖：正常/loading/empty/error/越权）
  depends: T021, T043@observability-hub, T052@observability-hub
- [ ] T061 [P] P0 XSS 防护检查（确认所有用户输出经框架转义；v-html 使用前 sanitize）
- [ ] T062 [P] P1 响应式适配（确认用量页在 1440px / 1024px / 768px 下布局正常）
- [ ] T063 [P] P1 可访问性检查（ARIA label、键盘导航、对比度）
- [ ] T064 [P] P1 前端构建验证 `cd web && npx vue-tsc --noEmit && npx vite build`（确认无类型错误和构建错误）
  depends: T021, T030, T040, T050
- [ ] T065 [P] P2 性能优化（ECharts 懒加载 / UsageTrendChart 增量更新；日期切换防抖 300ms）
  depends: T064

**✓ 检查点**：全部页面端到端走通，安全检查通过，构建无错误

---

## 交付策略

### MVP 优先（仅 P0）
1. 完成阶段 1（T001-T005）→ 基础设施
2. 完成 US-NEW-01 个人用量页（T020-T022）→ P0 核心页面可演示
3. 完成阶段 4 安全检查（T061）→ 合规发布

### 场景间依赖
- US-NEW-01 (个人用量页)：无故事间依赖（可独立交付）
- US-NEW-02 (全局看板)：依赖 US-NEW-01 的 UsageTrendChart 共享组件
- US-NEW-03 (Catalog 简化)：独立，可并行
- US-NEW-04 (model price_mode)：独立，可并行

---

## 任务统计

| 指标 | 数值 |
| :--- | :--- |
| 总任务数 | 21 |
| P0（必须） | 9 |
| P1（重要） | 11 |
| P2（可选） | 1 |
| 可并行任务 | 11 |
| 预计工时 | 32h |
