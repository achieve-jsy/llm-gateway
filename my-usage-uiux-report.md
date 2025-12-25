两张截图内容相同（都是页面上半部分：顶部统计卡片 + 筛选栏），下半部分（图表完整渲染、汇总表、调用明细表、Drawer）我主要依据 1489 行源码进行审查。以下是审查报告。

---

# 「我的用量」页面 UI/UX 审查报告

## 1. 总体评分：**6.5 / 10**

一句话总结：**信息密度与细节打磨（mini-bar、tooltip 明细、timeline）达到了优秀的产品级水准，但存在「暗色模式大面积失效、表格排序/导出存在欺骗性、图表标题与内容不符」三类硬伤，属于"看着精致、用起来踩坑"的典型。**

---

## 2. 严重问题（必须修复）

### 🔴 P0-1｜暗色模式大面积失效（筛选栏 / 预设按钮 / 图表）

页面顶部有月亮图标（应用支持暗色模式），但本页多处**硬编码了亮色十六进制值**，切到暗色后会出现"白底白字 / 白块"。

证据（筛选栏整块写死白底）：

```1108:1134:web/src/views/MyUsagePage.vue
.mu-filter {
  ...
  background: #fff;
  border-radius: 10px;
  ...
  border: 1px solid #ebeef5;
  box-shadow: 0 1px 4px rgba(0,0,0,.04);
}
.mu-preset-btn {
  ...
  border: 1px solid #dcdfe6;
  background: #f5f7fa;
  color: #606266;
  ...
}
.mu-preset-btn:hover { border-color: #409eff; color: #409eff; background: #ecf5ff; }
.mu-preset-btn--active { border-color: #409eff; color: #fff; background: #409eff; ... }
```

图表轴线/文字颜色同样写死，暗色下几乎不可读：

```994:995:web/src/views/MyUsagePage.vue
x: { grid: { color: 'rgba(125,125,125,0.12)' }, border: { display: false }, ticks: { color: '#94A3B8', font: { size: 10 }, maxRotation: 30 } },
y: { grid: { color: 'rgba(125,125,125,0.12)' }, border: { display: false }, ticks: { color: '#94A3B8', ... } }
```

Drawer 内消息角色 / 错误块也写死亮色（`#fef3c7 / #dbeafe / #d1fae5 / #fef2f2`），见 1389–1407 行。

**影响**：所有暗色模式用户，筛选栏不可用、图表文字看不清。
**修复**：

```css
.mu-filter {
  background: var(--mu-surface);
  border: 1px solid var(--mu-border);
  border-radius: 14px; /* 同时修复圆角不一致，见 P2-1 */
}
.mu-preset-btn {
  border: 1px solid var(--el-border-color);
  background: var(--el-fill-color-light);
  color: var(--el-text-color-regular);
}
.mu-preset-btn:hover { border-color: var(--el-color-primary); color: var(--el-color-primary); background: var(--el-color-primary-light-9); }
.mu-preset-btn--active { border-color: var(--el-color-primary); color: #fff; background: var(--el-color-primary); }
```

图表需读取 CSS 变量并**监听主题切换重渲**（用 `getComputedStyle` 取 `--el-text-color-secondary` / `--el-border-color`，在主题切换时调用 `renderTrend/renderDist`）。

---

### 🔴 P1-2｜表格"排序"具有欺骗性（仅排当前分页）

调用明细表是**服务端分页**（`loadLogs` 带 `page/page_size`），但列上挂了 `sortable`：

```333:336:web/src/views/MyUsagePage.vue
<el-table-column prop="duration_ms" label="耗时" min-width="70" align="right" sortable>
```

`sortable`（非 `sortable="custom"`）只会对**当前页 50 条**做客户端排序。用户点击"费用倒序"，以为看到的是全量 Top 费用，实际只是这 50 条里的最大值——**这是会导致错误决策的数据假象**。

**修复**：改为 `sortable="custom"`，监听 `@sort-change` 把 `sort_by/order` 传给后端 `loadLogs`；后端不支持则**直接去掉 `sortable`**，避免误导。汇总表（全量、客户端）保留 `sortable` 没问题。

---

### 🔴 P1-3｜「导出」只导当前页，且导出字段是内部英文名

```1045:1051:web/src/views/MyUsagePage.vue
function handleExport() {
  if (!logRows.value.length) return
  const keys = Object.keys(logRows.value[0] || {})
  const csv = [keys.join(','), ...logRows.value.map(...)]
  ...
}
```

两个问题：
1. **只导出 `logRows`（当前页）**，但按钮文案是"导出"，用户预期是导出筛选范围全量。
2. CSV 表头是 `request_id,ts,key_id,...` 这种内部字段名，非业务可读列名；含换行的 `user_agent` 会破坏 CSV 结构。

**修复**：新增后端导出接口（带筛选参数，流式全量），或前端循环分页拉全量；表头映射成中文列名；用专门的 CSV 转义（处理引号/逗号/换行）。

---

### 🔴 P1-4｜「Top 模型」图表在"按天"维度下显示的是日期，标题与内容不符

`renderDist` 的分组 key 跟随 `groupBy`：

```1006:1011:web/src/views/MyUsagePage.vue
const key = groupBy.value === 'model' ? (r.model || '未知') : fmtDate(r.stat_date)
```

而标题恒为"Top 模型"：

```168:168:web/src/views/MyUsagePage.vue
<span class="mu-chart-card__title">Top 模型</span>
```

默认 `groupBy='day'`，所以**默认打开页面，"Top 模型"图里画的是日期排行**（截图右下角也确实是 `2026-06-10 / 2026-06-12` 而非模型名）。标题与内容直接矛盾。

**修复**：标题改为计算属性 `groupBy==='model' ? 'Top 模型' : 'Top 日期'`；或让该图永远按模型聚合（与 `groupBy` 解耦），更符合"分布"语义。

---

### 🔴 P1-5｜预设按钮主色与品牌主色冲突（蓝 vs 青绿）

应用主色是青绿（标题渐变、stat 强调条、`--el-color-primary` 都是 teal 系），但预设按钮 hover/active 写死了 Element 默认蓝 `#409eff`（见 P0-1 代码）。同一屏出现两套"主色"，破坏品牌一致性。修复随 P0-1 一起用 `var(--el-color-primary)`。

---

## 3. 改善建议（强烈建议，P2）

### 🟠 P2-1｜卡片圆角不统一（14px vs 10px）
通用规则定义 14px，`.mu-filter` 又覆盖成 10px，且只有筛选栏带 `box-shadow`，其余卡片无阴影 → 视觉层级不齐。统一为 14px、统一阴影策略（要么都有要么都无）。

### 🟠 P2-2｜日期计算用 UTC，存在跨时区/跨天错位
`fmt = d => d.toISOString().split('T')[0]` 取的是 **UTC 日期**：

```620:623:web/src/views/MyUsagePage.vue
const fmt = (d: Date) => d.toISOString().split('T')[0]
const today = new Date()
```

东八区用户在晚上 8 点后，"今天"会被算成 UTC 的明天/今天边界出错；`today` 还在模块加载时固定一次，页面跨午夜不更新。建议改用本地日期格式化（`getFullYear/getMonth/getDate` 拼接），并在 `applyPreset` 内重新 `new Date()`。

### 🟠 P2-3｜空数据时图表整块消失，无占位
`<div class="mu-charts" v-if="statsRows.length">` —— 无数据时图表区直接没了，而表格有精心设计的空态。体验不一致，且布局会跳动。建议给图表也加空态占位卡片。

### 🟠 P2-4｜Drawer 默认 Tab 落在可能为空的"提示词"
`openTrace` 里 `traceTab.value = 'prompt'`，但 Tabs 第一项是"调用信息(meta)"。trace 成功时强制跳到 prompt，若该请求无 messages 则首屏显示"无提示词数据"，观感差。建议默认停在信息量最稳的"调用信息"。

### 🟠 P2-5｜Chart 实例未在卸载时销毁（内存泄漏）
只在 `renderXxx` 里 `destroy()` 旧实例，**没有 `onUnmounted`**。离开页面后两个 Chart 实例及其事件监听残留。加：

```js
import { onUnmounted } from 'vue'
onUnmounted(() => { trendChart?.destroy(); distChart?.destroy() })
```

### 🟠 P2-6｜无请求竞态保护
快速切换筛选/分页时 `loadStats/loadLogs` 并发，返回顺序不保证，可能旧响应覆盖新结果。建议引入 `AbortController` 或请求序号校验。

### 🟠 P2-7｜响应式断点冗余/打架
`@1100→2列`、`@560→1fr 1fr`（与 2 列重复）、`@768→2列`、`@480→1列` 四套规则交叠，`max-width:560` 那条与 768 实际重复且互相覆盖。梳理成一套清晰断点（如 1100/768/480）。

### 🟠 P2-8｜触摸目标偏小
预设按钮 `padding:3px 10px`（约 24px 高）、info 图标 16px，均低于移动端 44×44 推荐。移动端建议加大预设按钮与可点区域。

### 🟠 P2-9｜均次成本 6 位小数噪声过大
卡片副标题 `均次 $0.016124`（截图可见）位数过多，扫读困难。统计卡这种概览位置建议 `fmtCost`（自适应 2–4 位）即可，6 位留给明细 tooltip。

---

## 4. 锦上添花（P3）

- **P3-1 死代码清理**：`Search`、`Key` 图标已 import 未使用；`filterModel`（标注"废弃"）、`filterKeyName` 未使用。删除以降噪。
- **P3-2 组件拆分**：1489 行单文件，建议拆 `StatCard.vue`、`UsageFilterBar.vue`、`TraceDrawer.vue`、`useUsageCharts.ts`，显著提升可维护性与可测性。
- **P3-3 `distMetric` 双重渲染**：既有 `watch(distMetric, …renderDist)` 又有模板 `@change="renderDist"`，切换一次渲两遍，去掉其一。
- **P3-4 mini-bar 配色语义不符**：成本 mini-bar 填充用金色（`--mu-cost`），而数值文字用红/橙/绿（`costColor`），同一格两套颜色语义。建议 bar 也随阈值变色或统一。
- **P3-5 缓存命中率可能 >100%**：表格内 `cached/prompt*100` 未 clamp；命中率超 100% 会让用户困惑，建议 `Math.min(…,100)`。
- **P3-6 筛选项无可见标签**：仅 placeholder，blur 后无提示语境；可加 `aria-label` 提升可访问性。
- **P3-7 行点击区域**：整行 `cursor:pointer` + `@row-click`，但行内 info/tooltip 图标也会触发抽屉；可对这些交互元素 `@click.stop`。

---

## 5. 亮点（值得保留与表扬）

1. **设计 token 体系清晰**：`.mu` 作用域内定义了一整套 `--mu-*` 语义变量（input/output/cache/cost/reason），大部分组件正确复用了 Element 变量——基础很扎实（可惜筛选栏/图表破了功）。
2. **Token / 成本明细 tooltip 极其专业**：输入(含缓存)、缓存读/写、推理 token、单价快照、分项费用全覆盖，信息分组 + 分隔线 + 配色，达到计费系统级别的透明度。
3. **mini-bar 内联可视化**：表格在数值旁嵌入相对进度条（按列 max 归一），让"谁大谁小"一眼可见，远胜纯数字表。
4. **Trace timeline**："鉴权→路由→上游→TTFT→总耗时"分阶段计时，含"计时异常"告警与错误态高亮，排障价值很高。
5. **细节格式化考究**：`fmtCost` 动态精度去尾零、`fmtTokens` 的 K/M/B、`tabular-nums` 等宽数字对齐——这些是高级感的来源。
6. **筛选可用性**：快捷预设 + 自定义区间 + 多维筛选 + `activeFilterCount` 一键清空，交互闭环完整。

---

### 修复优先级速览

| 级别 | 问题 | 一句话 |
|------|------|--------|
| P0 | 暗色模式失效 | 筛选栏/预设/图表/Drawer 硬编码亮色 |
| P1 | 排序欺骗 | 服务端分页 + 客户端 `sortable` 仅排当前页 |
| P1 | 导出误导 | 仅导当前页、英文字段名、无 CSV 转义 |
| P1 | 图表名实不符 | "Top 模型"在按天时画日期 |
| P1 | 主色冲突 | 预设按钮蓝 vs 品牌青绿 |
| P2 | 时区/圆角/空态/Tab/泄漏/竞态/断点/触摸/精度 | 见上 |

需要我直接动手修复其中的 P0/P1（暗色模式 + 排序 + 图表标题 + 主色），我可以切到实现模式开始改。
