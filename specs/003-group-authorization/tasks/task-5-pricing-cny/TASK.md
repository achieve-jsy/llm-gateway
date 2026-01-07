# Task 5: 价格换算人民币（USD→CNY）

## 背景

当前系统所有价格/成本以 **美元（USD）** 计：
- 数据库 `usage_daily.est_cost_usd`、`call_logs` 中的 cost 字段均为 USD
- `ModelPricing` 结构体中的 `InputPerMtok`、`OutputPerMtok` 等单价均为 USD/百万token
- 前端 `UsageCostPage.vue` 全部用 `$` 符号显示

用户需要看到人民币（CNY）金额，方便对内计费参考。

## 需要完成的功能

### 1. 汇率配置

在控制面设置中新增一个可配置的 USD→CNY 汇率，管理员可随时修改。

**方案**：复用现有 Settings 体系（`SettingsPage.vue` + 对应后端），新增一个配置项：

后端（控制面）：
- 在系统设置表或配置中新增 `pricing_usd_to_cny_rate` 字段，类型 `float64`，默认 `7.2`
- 提供获取接口（可能已有 `GET /api/v1/settings` 返回全部设置）
- 管理员可修改（`PUT /api/v1/settings` 或类似）

### 2. 前端显示改造

文件：`web/src/views/admin/UsageCostPage.vue`

改造要点：
- **新增币种切换按钮**（USD / CNY toggle），默认显示 CNY
- 当选择 CNY 时，所有金额 × 汇率，符号从 `$` 改为 `¥`
- 改造范围：
  - 统计卡片总成本：`${{ fmtCost(...) }}` → `¥{{ fmtCost(... * rate) }}`（或 `$`）
  - 均次成本
  - 表格中每行的 `est_cost_usd` 列
  - 明细弹窗中的输入/输出/合计费用
  - 图表 tooltip 和 Y 轴标签
  - 导出 CSV 时标注币种

### 3. 汇率工具函数

新增工具函数（可放在 `web/src/utils/currency.ts`）：

```ts
export interface CurrencyConfig {
  currency: 'USD' | 'CNY'
  rate: number  // USD→CNY 汇率
}

export function convertCost(usd: number, config: CurrencyConfig): number {
  return config.currency === 'CNY' ? usd * config.rate : usd
}

export function currencySymbol(currency: 'USD' | 'CNY'): string {
  return currency === 'CNY' ? '¥' : '$'
}

export function formatCost(usd: number, config: CurrencyConfig): string {
  const value = convertCost(usd, config)
  const symbol = currencySymbol(config.currency)
  // 保持现有 fmtCost 的精度逻辑
  if (value >= 1) return `${symbol}${value.toFixed(2)}`
  return `${symbol}${value.toFixed(4).replace(/0+$/, '').replace(/\.$/, '')}`
}
```

### 4. 汇率数据获取

前端启动/刷新时从后端拉取汇率配置：
- 从 settings 接口获取 `pricing_usd_to_cny_rate`
- 存入 composable 或 store 中供页面使用
- 默认值 7.2（后端无配置时 fallback）

### 5. 设置页面入口

在管理设置页面（`SettingsPage.vue`）中，添加「价格汇率」配置项：
- 标签：「USD→CNY 汇率」
- 输入框：数字，默认 7.2
- 说明文字：「用于用量统计页面的人民币换算展示，不影响实际计费数据」

### 6. 其他涉及价格显示的页面

检查并改造（如果有 cost 显示）：
- `web/src/views/admin/usage/UsageDashboardPage.vue` — 用量看板
- `web/src/views/MyUsagePage.vue` — 个人用量页
- 其他含 `$` + cost 显示的页面

原则：所有对用户展示费用的地方都支持 CNY 切换。

## 验证

- 管理设置中可修改汇率
- UsageCostPage 切换 CNY 后金额 = USD × 汇率
- 切换 USD 后恢复原始美元显示
- 导出 CSV 列名标注当前币种
- 图表 tooltip/Y轴 随币种变化

## 关键约定

- 不改变数据库存储（始终存 USD），仅为**前端展示层**换算
- 汇率手动配置（不接外部汇率 API，避免依赖外部服务）
- 默认显示 CNY（中国团队主要用户）
- 保持 `fmtCost` 原有精度规则，只替换符号和乘以汇率

## 相关文件

- `web/src/views/admin/UsageCostPage.vue` — 主要改造页面（2072 行）
- `web/src/views/admin/usage/UsageDashboardPage.vue` — 用量看板
- `web/src/views/MyUsagePage.vue` — 个人用量
- `web/src/views/admin/SettingsPage.vue` — 设置页面
- `internal/observability/model/usage_daily.go` — est_cost_usd 字段
- `internal/core/types.go` — ModelPricing 结构（确认单位为 USD）
