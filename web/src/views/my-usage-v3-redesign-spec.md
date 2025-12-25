# 任务：重新设计"我的用量"页面 — 用户使用习惯分析视角

## 核心理念
从"系统管理员看报表"转向"用户看自己的消费习惯"。
用户最关心的 4 个问题：
1. **我花了多少钱？** — 花费总览 + 趋势
2. **我用了什么？** — 模型偏好 + 成本分布
3. **我什么时候用？** — 时间习惯 + 高峰时段
4. **用得值不值？** — 效率指标 + 性价比

## 约束条件
1. **单文件**：所有改动只在 `web/src/views/MyUsagePage.vue`
2. **不改后端**：只用现有 API（`/api/v1/my/usage` + `/api/v1/my/usage/logs`）
3. **暗色主题**：背景 `#0F172A`，卡片 `#1E293B`，边框 `#334155`
4. **人民币**：所有成本用 `¥`，USD × 7.2
5. **北京时间**：所有时间显示 UTC+8
6. **Element Plus**：使用 `el-*` 组件
7. **Chart.js**：图表用原生 Canvas + Chart.js（已引入）
8. **构建验证**：`cd web && node node_modules/vite/bin/vite.js build` 必须通过

## 现有 API 数据结构

### GET /api/v1/my/usage
```
参数: from, to, group_by(day|model|key), model_name, group_name, key_id, status
返回: {
  stats: StatsRow[],      // 按 group_by 聚合的行
  model_stats: StatsRow[], // 按模型聚合（始终返回）
  total: { req_count, prompt_tokens, completion_tokens, cached_tokens, est_cost_usd, ... }
}

StatsRow = {
  stat_date?, model?, key_name?,
  req_count, prompt_tokens, completion_tokens, cached_tokens,
  cache_write_tokens, reasoning_tokens, total_tokens, est_cost_usd,
  first_seen?, last_seen?
}
```

### GET /api/v1/my/usage/logs
```
参数: from, to, page, page_size, key_id, key_name, model_name, group_name, status
返回: {
  logs: LogRow[], total: number
}

LogRow = {
  request_id, timestamp, model_name, key_name, group_name,
  status_code, is_stream, prompt_tokens, completion_tokens,
  cached_tokens, total_tokens, est_cost_usd,
  latency_ms, first_token_ms, input_price_snapshot, output_price_snapshot,
  input_cost_usd, output_cost_usd, cache_read_cost_usd,
  user_id, user_name
}
```

## 页面布局设计（从上到下）

### 区域 0: 顶部控制栏（保持不变）
- 日期范围选择器（默认最近 30 天）
- 快捷按钮：今天 / 7天 / 30天 / 本月
- 筛选器：模型名、Key、状态
- 保持现有逻辑不变

### 区域 1: 💰 花费总览（4 张卡片 + 趋势图）

**4 张 KPI 卡片（一行排列）：**
```
┌─────────────┐ ┌─────────────┐ ┌─────────────┐ ┌─────────────┐
│  总花费 ¥XX  │ │ 日均花费 ¥XX │ │  总请求 XX  │ │ 总Token XX  │
│  ↑12% vs上期 │ │  ¥XX/天     │ │  ↑8% vs上期 │ │  ↑15% vs上期│
└─────────────┘ └─────────────┘ └─────────────┘ └─────────────┘
```
- 数据来源：`total` + `prevPeriodTotal`（已有环比逻辑）
- 日均花费 = 总花费 / 天数
- 每个卡片显示环比箭头（↑绿/↓红 或反过来看语义）

**日花费趋势图（折线图，占满宽度）：**
```
┌──────────────────────────────────────────────────────────┐
│  📈 每日花费趋势                                          │
│  ┌─────────────────────────────────────────────────────┐ │
│  │  ╱╲    ╱╲                                           │ │
│  │ ╱  ╲╱╱╱  ╲╱╲     ← 折线图，带面积填充               │ │
│  │╱           ╲                                         │ │
│  └─────────────────────────────────────────────────────┘ │
│  显示：日均线（虚线）+ 实际日花费                          │
└──────────────────────────────────────────────────────────┘
```
- X 轴：日期，Y 轴：花费（¥）
- 面积填充用渐变色
- hover 显示日期 + 金额 + 请求数
- 画一条日均虚线

### 区域 2: 🤖 模型偏好（两列布局）

**左列：模型使用分布（环形图 + 列表）**
```
┌────────────────────────┐
│  🤖 我的模型偏好        │
│  ┌──────────┐          │
│  │  🍩环形图  │ 模型排行  │
│  │          │ 1. deepseek ¥XX │
│  └──────────┘ 2. claude ¥XX  │
│               3. qwen   ¥XX  │
└────────────────────────┘
```
- 环形图：按请求数分布，中间显示总请求数
- 右侧列表：按花费排序，显示模型名 + 花费 + 占比条

**右列：模型性价比对比（横向柱状图）**
```
┌────────────────────────┐
│  📊 性价比对比          │
│  deepseek  ████ ¥0.8/M │
│  claude    ██   ¥3.2/M │
│  qwen      █    ¥0.5/M │
│  (每百万 token 成本)    │
└────────────────────────┘
```
- 横向柱状图，按每百万 token 成本排序
- 柱子颜色与环形图一致
- 底部标注实际请求数

### 区域 3: 📅 使用时间习惯（两列布局）

**左列：小时分布（柱状图）**
```
┌────────────────────────┐
│  ⏰ 我什么时候用最多？   │
│  ┌────────────────────┐│
│  │ ▓▓                 ││
│  │ ▓▓ ▓▓     ▓▓      ││
│  │ ▓▓ ▓▓ ▓▓ ▓▓ ▓▓   ││
│  │ 0  4  8  12 16 20  ││
│  └────────────────────┘│
│  高峰: 09:00-11:00     │
│  平均每小时 XX 次请求   │
└────────────────────────┘
```
- 数据来源：从 `logs` API 取 `timestamp`，前端按小时聚合
- 24 根柱子，颜色深浅表示密度
- 底部标注高峰时段（取 top-3 小时）
- **注意**：时间戳必须转北京时间再取小时

**右列：工作日 vs 周末（对比卡片）**
```
┌────────────────────────┐
│  📆 工作日 vs 周末       │
│  ┌────────┐ ┌────────┐ │
│  │ 工作日  │ │  周末   │ │
│  │ ¥XX/天 │ │ ¥XX/天 │ │
│  │ XX次/天│ │ XX次/天│ │
│  └────────┘ └────────┘ │
│  你最忙的一天: 周三     │
│  你最闲的一天: 周日     │
└────────────────────────┘
```
- 数据来源：从 `stats`（group_by=day）前端计算工作日/周末日均
- 最忙/最闲那天取 req_count 最大/最小

### 区域 4: ⚡ 效率洞察（三列卡片）

```
┌──────────────┐ ┌──────────────┐ ┌──────────────┐
│ ⚡ 平均响应    │ │ 💾 缓存利用率 │ │ 📐 输入输出比 │
│   XXX ms     │ │    XX%       │ │   X : 1      │
│ 首Token XXms │ │ 节省了 ¥XX   │ │ 输出占比 XX%  │
└──────────────┘ └──────────────┘ └──────────────┘
```

**平均响应卡片：**
- 平均 latency_ms（从 logs 聚合）
- 平均 first_token_ms（流式请求）
- 数据来源：logs API 取前 100 条，前端计算 avg

**缓存利用率卡片：**
- 总 cached_tokens / 总 prompt_tokens 百分比
- "节省约 ¥XX"（缓存命中率 × 输入成本 × 0.9，因为缓存读取通常 1/10 价格）
- 排除 Claude 模型（cached_tokens=0 的 bug）

**输入输出比卡片：**
- prompt_tokens : completion_tokens 的简化比
- 输出占总 token 的百分比
- 说明含义（高输出比 = 生成更多内容）

### 区域 5: 📋 模型使用明细（保留现有表格，优化）
- 保留现有的 Token 成本效率表格
- 保留现有的模型消耗排行饼图
- 但把这部分放到页面下方（不再是视觉焦点）
- 添加一个"展开/收起"按钮，默认折叠

### 区域 6: 📜 调用明细（保留现有表格）
- 保留现有的调用记录表格 + 分页
- 保留 Trace 抽屉
- 保持现有逻辑

## 数据获取策略

### 需要的 API 调用
```javascript
// 1. 汇总统计（已有）
GET /api/v1/my/usage?from=X&to=Y&group_by=day    → dailyStats
GET /api/v1/my/usage?from=X&to=Y&group_by=model   → modelStats  
GET /api/v1/my/usage?from=X&to=Y&group_by=key     → keyStats

// 2. 环比数据（已有）
GET /api/v1/my/usage?from=prevFrom&to=prevTo&group_by=day → prevPeriod

// 3. 小时分布（新增前端逻辑）
GET /api/v1/my/usage/logs?from=X&to=Y&page=1&page_size=100
→ 取 timestamp 字段，前端按小时(北京时间)聚合

// 4. 效率指标（新增前端逻辑）
// 从上面的 logs 数据中计算 avg latency, avg first_token_ms
```

### 前端计算逻辑
```javascript
// 小时分布
function hourlyDistribution(logs) {
  const buckets = Array(24).fill(0)
  logs.forEach(log => {
    const d = new Date(log.timestamp)
    const hour = d.getUTCHours() + 8  // UTC→北京时间
    buckets[hour % 24]++
  })
  return buckets
}

// 工作日 vs 周末
function weekdayVsWeekend(dailyStats) {
  let wdCost = 0, wdCount = 0, wdDays = 0
  let weCost = 0, weCount = 0, weDays = 0
  dailyStats.forEach(row => {
    const d = new Date(row.stat_date)
    const dow = d.getDay() // 0=Sun, 6=Sat
    if (dow === 0 || dow === 6) {
      weCost += row.est_cost_usd; weCount += row.req_count; weDays++
    } else {
      wdCost += row.est_cost_usd; wdCount += row.req_count; wdDays++
    }
  })
  return {
    weekday: { costPerDay: wdCost / (wdDays||1) * 7.2, reqPerDay: wdCount / (wdDays||1) },
    weekend: { costPerDay: weCost / (weDays||1) * 7.2, reqPerDay: weCount / (weDays||1) }
  }
}

// 最忙/最闲日
function busiestAndQuietestDay(dailyStats) {
  const sorted = [...dailyStats].sort((a,b) => b.req_count - a.req_count)
  return { busiest: sorted[0], quietest: sorted[sorted.length-1] }
}

// 日均花费
function dailyAvgCost(total, days) { return total / (days || 1) }

// 缓存节省估算
function cacheSavings(total) {
  // 缓存读取通常比正常输入便宜 90%
  // 节省 = cached_tokens * input_price * 0.9
  // 简化：用 cached_tokens / prompt_tokens * input_cost * 0.9
  const cacheRatio = total.cached_tokens / (total.prompt_tokens || 1)
  return cacheRatio * (total.est_cost_usd || 0) * 0.9 * 7.2
}

// 输入输出比
function inputOutputRatio(total) {
  const p = total.prompt_tokens || 1
  const c = total.completion_tokens || 1
  const ratio = (p / c).toFixed(1)
  return `${ratio} : 1`
}
```

## 视觉风格指南

### 颜色系统
```css
/* 背景 */
--mu-bg: #0F172A;
--mu-surface: #1E293B;
--mu-border: #334155;

/* 文字 */
--mu-text-primary: #F1F5F9;
--mu-text-secondary: #94A3B8;
--mu-text-muted: #64748B;

/* 强调色 */
--mu-blue: #3B82F6;
--mu-green: #10B981;
--mu-cyan: #06B6D4;
--mu-orange: #F59E0B;
--mu-red: #EF4444;
--mu-purple: #8B5CF6;
--mu-pink: #EC4899;

/* 图表颜色序列 */
chartColors: ['#3B82F6', '#10B981', '#F59E0B', '#EF4444', '#8B5CF6', '#EC4899', '#06B6D4', '#F97316']
```

### 卡片样式
```css
.insight-card {
  background: var(--mu-surface);
  border: 1px solid var(--mu-border);
  border-radius: 12px;
  padding: 20px;
}
.insight-card h3 {
  font-size: 14px;
  color: var(--mu-text-secondary);
  margin-bottom: 12px;
}
.kpi-value {
  font-size: 28px;
  font-weight: 700;
  color: var(--mu-text-primary);
}
.kpi-sub {
  font-size: 12px;
  color: var(--mu-text-muted);
}
.trend-up { color: #10B981; }   /* 请求量上升=好 */
.trend-down { color: #EF4444; } /* 请求量下降=差 */
.cost-up { color: #EF4444; }    /* 花费上升=差 */
.cost-down { color: #10B981; }  /* 花费下降=好 */
```

### 图表配置
```javascript
const chartDefaults = {
  responsive: true,
  maintainAspectRatio: false,
  plugins: {
    legend: { labels: { color: '#94A3B8' } },
    tooltip: {
      backgroundColor: '#1E293B',
      borderColor: '#334155',
      borderWidth: 1,
      titleColor: '#F1F5F9',
      bodyColor: '#94A3B8',
    }
  },
  scales: {
    x: { ticks: { color: '#64748B' }, grid: { color: 'rgba(51,65,85,0.5)' } },
    y: { ticks: { color: '#64748B' }, grid: { color: 'rgba(51,65,85,0.5)' } }
  }
}
```

## 实现步骤

1. **先读取现有代码** — 理解当前结构和已有逻辑
2. **重构模板** — 按新布局重写 template 部分
3. **添加新的 computed/data** — hourlyDistribution, weekdayVsWeekend, busiestDay 等
4. **添加图表渲染** — 花费趋势折线图、小时分布柱状图、性价比柱状图
5. **保留现有功能** — 筛选器、表格、Trace 抽屉、导出等
6. **构建验证** — `cd web && node node_modules/vite/bin/vite.js build`

## 不要做的事
- ❌ 不修改后端 API
- ❌ 不添加新路由
- ❌ 不引入新的 npm 依赖
- ❌ 不修改其他文件
- ❌ 不删除现有的表格和明细功能（移到下方折叠）
