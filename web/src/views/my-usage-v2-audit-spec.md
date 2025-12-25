# 严格 UI/UX 审计 + 修复 — "我的用量" 页面 (V2)

## 文件
`web/src/views/MyUsagePage.vue` (~2133 行, 单文件 Vue 组件)

## 背景
这是一个 LLM API 网关的个人用量看板。用户反馈了以下具体问题：

## 🔴 已确认的 Bug（必须修复）

### Bug 1: 货币单位不一致
- **现象**: "模型使用明细"表格（约 line 291）的费用列用 `$`（美元），但统计卡片（line 112）、Token 成本效率表（line 230）、每百万 Token（line 235）全部用 `¥`（人民币）
- **根因**: Line 291 `${{ fmtCost(row.est_cost_usd) }` 没有改成 CNY
- **修复**: 改为 `¥{{ fmtCny(row.est_cost_usd) }`，与全页统一。同时检查调用明细表（约 line 412-420）的费用列是否也需要统一

### Bug 2: Claude 模型输出占比显示 0%
- **现象**: Token 成本效率表中，Claude 系列模型的"输出占比"列显示 0%
- **根因（数据库验证）**: Claude 模型的 `cached_tokens` 在数据库中全为 0（后端未提取 Anthropic 的 `cache_read_input_tokens`）。导致：
  - `prompt_tokens` 包含了所有输入（含缓存读取），值巨大（如 claude-sonnet-4-6: 831M prompt vs 2.3M completion）
  - `tokenOutputPct()` 计算: `completion / (prompt + completion)` = 2.3M / 833M ≈ 0.28%，被 `Math.round()` 变成 0%
  - 同样 `cacheHitRate()` 也显示 0%（因为 cached_tokens=0）
- **修复方案**:
  1. `tokenOutputPct()` 改用 `toFixed(1)` 替代 `Math.round()`，保留一位小数
  2. 当 `cached_tokens === 0` 且模型名包含 "claude" 时，在缓存命中率列显示 "N/A*"（带 tooltip 说明"该模型的缓存数据暂不可用"）
  3. 在"输入占比"和"输出占比"旁边加一个小提示图标（当 cached_tokens=0 且 prompt_tokens > 100000 时），tooltip 说明"输入 Token 可能包含缓存读取，实际比例可能偏差"

### Bug 3: 零使用模型展示
- **现象**: Token 成本效率表和模型消耗排行中，出现了从未产生有效 token 的模型（如 `claude-4`、`claude-haiku-4-5` 等，数据库中 prompt_tokens=0, completion_tokens=0）
- **根因**: `costEfficiencyRows`（line 904-908）和 `modelTokenRanking`（line 897-902）没有过滤 `totalTokens === 0` 的记录
- **修复**: 在 `.map()` 后加 `.filter(r => r.totalTokens > 0)`

### Bug 4: 暗色主题颜色对比度不足
- **现象**: 部分文字颜色在深色背景上看不清
- **已知问题色值**: `#9CA3AF`（灰色）在 `#0F172A`（深蓝背景）上对比度仅约 5.5:1，勉强达标但视觉上不清晰。特别是小字体和辅助文字
- **修复**: 
  - 将所有 `#9CA3AF` 提升到 `#B0B8C4` 或更亮
  - 表格行 hover 背景色加深以提高选中状态的可读性
  - 检查所有 `rgba()` 透明度值，确保文字对比度 ≥ 7:1

## 🟡 需要审计的方向（请严格检查并给出建议）

### 审计方向 1: 信息架构 — 用户真正想看什么？
这个页面是给**普通用户**（不是管理员）看的。用户真正关心的是：
1. **我花了多少钱？** — 总花费、日均花费、趋势
2. **我用了哪些模型？哪个最贵？** — 模型使用分布、成本排行
3. **我的使用习惯是什么？** — 高峰时段、平均每次调用 token 量、流式 vs 同步比例
4. **效率如何？** — 缓存命中率、输入/输出比、每百万 token 成本

请审查当前页面的信息层次是否合理，是否突出了用户最关心的内容。

### 审计方向 2: 交互友好性
- 表格排序在服务端分页下是否合理？（当前排序指示器在前端，但数据是服务端分页）
- 筛选器交互是否流畅？（日期选择、模型名搜索、Key 选择）
- 图表交互 — 饼图点击下钻、趋势图 hover 提示
- 导出功能 — 按钮文案是否准确

### 审计方向 3: 数据展示优化
- 大数字的格式化（如 831,441,417 tokens 应该显示为 "831.4M" 而非 "831441417"）
- 成本小数位数 — ¥0.001 这种太小的数字是否需要特殊处理
- 时间戳 — 必须使用北京时间 (UTC+8)，不能用 toISOString()
- 空数据状态 — 某些筛选组合下可能无数据，empty state 是否友好

### 审计方向 4: 视觉设计
- 整体配色方案是否在暗色主题下和谐
- 卡片间距、边框、阴影是否一致
- 图表颜色是否与页面主题协调
- 移动端/窄屏下的布局是否合理

## 约束条件
1. **单文件** — 所有改动只在 `web/src/views/MyUsagePage.vue` 中
2. **暗色主题** — 页面背景 `#0F172A`，卡片 `#1E293B`，使用 Element Plus CSS 变量优先
3. **北京时间** — 所有时间显示 UTC+8，使用 `new Date(y, m-1, d)` 代替 `new Date(str)`
4. **人民币** — 所有成本显示统一使用 `¥`，后端返回的是 USD 字段，需要 `× 7.2` 换算
5. **Element Plus** — 使用 `el-table`、`el-date-picker`、`el-tag`、`el-tooltip` 等组件
6. **Chart.js** — 图表使用原生 Canvas + Chart.js，不引入其他图表库
7. **构建验证** — 改完后必须能通过 `cd web && node node_modules/vite/bin/vite.js build`

## 输出要求
1. **先审计** — 列出你发现的所有问题（按严重程度排序）
2. **再修复** — 直接修改代码修复 Bug 1-4，并实施你认为最重要的 3-5 个审计建议
3. **构建验证** — 运行 `cd web && node node_modules/vite/bin/vite.js build` 确保无编译错误
4. **不要** 修改 API 接口、路由、其他文件

## 当前关键数据结构（供参考）

### StatsRow (API 返回的汇总行)
```typescript
interface StatsRow {
  stat_date?: string
  model?: string
  req_count: number
  prompt_tokens: number
  completion_tokens: number
  cached_tokens: number      // Claude 模型此字段为 0（后端 bug）
  cache_write_tokens: number  // Claude 模型此字段为 0
  reasoning_tokens: number
  total_tokens: number
  est_cost_usd: number        // 美元，前端需 ×7.2 转人民币
}
```

### 实际生产数据样例（Claude 模型）
```
claude-sonnet-4-6:  req=5326, prompt=831M, completion=2.3M, cached=0, reasoning=0
claude-opus-4-6:    req=3903, prompt=396M, completion=1.6M, cached=0, reasoning=0
deepseek-v4-pro:    req=16148, prompt=2.0B, completion=6.0M, cached=1.2B  ← 非 Claude 模型有缓存数据
```

### 现有格式化函数
```typescript
fmtCny(usd: number) → (usd * 7.2).toFixed(2)  // USD → CNY
fmtCost(usd: number) → usd.toFixed(4)          // 原始 USD
fmtTokens(n: number) → 带 K/M/B 后缀
fmtNum(n: number) → 千分位格式化
fmtDate(s: string) → 格式化日期（北京时间）
```
