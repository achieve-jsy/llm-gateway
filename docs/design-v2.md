# LLM Gateway 前端设计规范 v2

> 面向数据密集型管理后台的视觉与交互规范。
> 基于 Airtable / Sentry / Supabase 设计语言提炼，结合品牌翠绿 #36B34E。
> 本文档是 `web/` 前端视觉的**唯一事实源**，替代此前分散的多份设计文档。

---

## 0. 现状诊断与调整方向

### 0.1 核心问题

| # | 问题 | 现状表现 | 用户感知 |
|---|------|---------|---------|
| 1 | **品牌色泛滥** | 表头绿色、hover 绿色、阴影绿色、focus 绿色光环 — 页面"到处是绿" | 视觉噪音大，分不清什么是可交互的 |
| 2 | **视觉重量过重** | box-shadow 0 2px 10px、border-radius 12px、component-size 34px | 数据密度低，信息扫描效率差 |
| 3 | **主题冲突** | ops-console 文档定义暗色+青色(#00E5FF)，实际用亮色+绿色(#36B34E)，用量页又定义了深蓝(#0F172A) | 页面之间风格割裂 |
| 4 | **字号层级缺失** | `--font-title-size` / `--font-title-weight` 未定义，fallback 到系统默认 | 标题/正文无清晰层级，看着累 |
| 5 | **等宽字体缺失** | API Key、Endpoint、Token 数量等技术内容用普通字体渲染 | 数据对齐差，数字不易辨读 |
| 6 | **表格密度不当** | 表头全绿背景、border 属性开启、行高偏大 | 表格视觉噪音大，长列表扫描疲劳 |

### 0.2 大厂参考选型

从 54 个大厂设计系统中筛选出 3 个最契合"数据密集型管理后台"的参考：

| 参考 | 核心理念 | 借鉴点 | 不采用点 |
|------|---------|--------|---------|
| **Airtable** | 浅色画布 + 结构化数据管理 | 正 letter-spacing、蓝调阴影、12px 按钮圆角、Inter 字体 | 品牌蓝不适用，改用品牌绿 |
| **Sentry** | 信息密度优先 + 功能至上 | 紧凑间距、uppercase 表头、清晰的三态处理 | 暗色主题非主力，仅参考信息密度 |
| **Supabase** | 极简 + 强调色克制 | 强调色仅用于 CTA/链接/border（不用作大面积填充）、weight 400 为主 | 暗色主题非主力，pill 按钮不适合管理后台 |

### 0.3 设计北极星（修订）

**从 "Wise 风格" 调整为 "Airtable 信息架构 + Supabase 强调色克制"**：

- 浅色画布保持 ✅
- 品牌绿保留，但**严格限定使用场景** — 只有 CTA 按钮、链接、活跃态才用绿色
- 表头/hover/focus 回归**中性灰** — 数据区不应有品牌色干扰
- 圆角从 12px 降至 **8px** — 更紧凑、更专业

---

## 1. 设计 Token

### 1.1 颜色系统

#### 品牌色（严格限定使用范围）

| Token | 值 | 用途 |
|-------|-----|------|
| `--brand-green` | `#36B34E` | **仅**：主 CTA 按钮背景、链接 hover 下划线 |
| `--brand-green-dark` | `#1F8A3B` | **仅**：链接/文字按钮默认色（明色模式） |
| `--brand-green-light` | `#62C96C` | **仅**：按钮 hover 态 |
| `--brand-green-bg` | `#EAF7EC` | **仅**：品牌按钮 hover 底色、选中行高亮（极淡） |

> ⚠️ **禁止**将品牌绿用于：表头背景、卡片边框、全局 box-shadow、input focus 外光环、hover 整行背景。

#### 中性色（数据区的基底）

| Token | Light | Dark | 用途 |
|-------|-------|------|------|
| `--bg-page` | `#F8FAFC` | `#0F172A` | 页面底色（slate-50） |
| `--bg-surface` | `#FFFFFF` | `#1E293B` | 卡片/表格/对话框底色 |
| `--bg-subtle` | `#F1F5F9` | `#1E293B` | 工具栏/表头等次级底色 |
| `--border-default` | `#E2E8F0` | `#334155` | 默认边框（slate-200） |
| `--border-light` | `#F1F5F9` | `#1E293B` | 淡边框 |
| `--border-strong` | `#CBD5E1` | `#475569` | 强调边框 |

#### 文字色

| Token | Light | Dark | 用途 |
|-------|-------|------|------|
| `--text-primary` | `#0F172A` | `#F1F5F9` | 标题、重要数据 |
| `--text-regular` | `#334155` | `#CBD5E1` | 正文、表格内容 |
| `--text-secondary` | `#64748B` | `#94A3B8` | 次要标签、描述 |
| `--text-placeholder` | `#94A3B8` | `#475569` | 占位文字 |

#### 语义色

| Token | 值 | 用途 |
|-------|-----|------|
| `--color-success` | `#059669` | 状态正常、启用 |
| `--color-warning` | `#D97706` | 警告、待处理 |
| `--color-danger` | `#DC2626` | 错误、禁用、删除 |
| `--color-info` | `#2563EB` | 信息提示 |

> 语义色使用 Tailwind CSS 600 色阶，确保 AA 对比度。

### 1.2 字体系统

```css
/* 正文字体 */
--font-family: 'Inter', -apple-system, BlinkMacSystemFont, 'PingFang SC', 'Microsoft YaHei', sans-serif;

/* 等宽字体 — 技术内容专用 */
--font-mono: 'JetBrains Mono', 'SF Mono', 'Fira Code', 'Cascadia Code', Consolas, monospace;
```

#### 字号层级

| 角色 | 大小 | 字重 | 行高 | Letter-Spacing | 示例 |
|------|------|------|------|----------------|------|
| **Display** | 28px | 700 | 1.2 | -0.5px | 仪表盘大数字 |
| **Page Title** | 20px | 600 | 1.3 | -0.3px | 页面标题 "渠道管理" |
| **Section Title** | 16px | 600 | 1.4 | 0 | 卡片标题、区域标题 |
| **Body** | 14px | 400 | 1.6 | 0.01em | 正文、表格内容 |
| **Body Small** | 13px | 400 | 1.5 | 0.01em | 次要描述、表单标签 |
| **Caption** | 12px | 500 | 1.4 | 0.02em | 标签、徽章、时间戳 |
| **Table Header** | 12px | 600 | 1.4 | 0.05em | 表头文字（英文 uppercase，中文保持原样） |
| **Code/Data** | 13px | 400 | 1.5 | 0 | 等宽：API Key、Endpoint、Token 数 |

> **关键变更**：新增 `--font-mono`，所有 API Key、Endpoint URL、Token 数量、成本金额等技术内容统一使用等宽字体。
>
> ⚠️ **字体加载策略**：Google Fonts CDN 在中国大陆被封锁。必须自托管：
> 1. 下载 Inter + JetBrains Mono 的 WOFF2 文件到 `web/public/fonts/`
> 2. 在 `global.scss` 中用 `@font-face` + `font-display: swap` 声明
> 3. 如加载失败，系统字体（PingFang SC / Microsoft YaHei）无缝 fallback

#### 使用等宽字体的 CSS 类

```css
.mono-text,
.key-masked,
.mono-data,
td.mono,
.endpoint-text,
.cost-value,
.token-count {
  font-family: var(--font-mono);
  font-feature-settings: 'tnum'; /* 等宽数字 */
  font-variant-numeric: tabular-nums;
}
```

### 1.2.1 图表色板

| Token | 值 | 用途 |
|-------|-----|------|
| `--chart-1` | `#3B82F6` | 蓝 — 主数据系列 |
| `--chart-2` | `#10B981` | 绿 — 次数据系列 |
| `--chart-3` | `#F59E0B` | 黄 — 警告/成本 |
| `--chart-4` | `#EF4444` | 红 — 错误/下降 |
| `--chart-5` | `#8B5CF6` | 紫 — 辅助系列 |
| `--chart-6` | `#EC4899` | 粉 — 辅助系列 |
| `--chart-7` | `#06B6D4` | 青 — 辅助系列 |
| `--chart-8` | `#F97316` | 橙 — 辅助系列 |

> 图表色板与 my-usage-v3 的 chartColors 序列一致，确保跨页面图表风格统一。

### 1.2.2 语义背景色（Tag/Badge 用）

| Token | Light | Dark | 用途 |
|-------|-------|------|------|
| `--color-success-bg` | `#ECFDF5` | `#064E3B` | 成功标签背景 |
| `--color-warning-bg` | `#FFFBEB` | `#78350F` | 警告标签背景 |
| `--color-danger-bg` | `#FEF2F2` | `#7F1D1D` | 错误标签背景 |
| `--color-info-bg` | `#EFF6FF` | `#1E3A5F` | 信息标签背景 |

### 1.3 间距系统

基准单位：**4px**，常用倍数：

| Token | 值 | 用途 |
|-------|-----|------|
| `--space-1` | 4px | 图标与文字间距 |
| `--space-2` | 8px | 紧凑元素间距 |
| `--space-3` | 12px | 表单字段间距 |
| `--space-4` | 16px | 卡片内边距、区域间距 |
| `--space-5` | 20px | 页面内边距 |
| `--space-6` | 24px | 区域间距 |
| `--space-8` | 32px | 大区域分隔 |

### 1.4 圆角

| Token | 值 | 用途 |
|-------|-----|------|
| `--radius-sm` | 4px | 标签、徽章、小元素 |
| `--radius-base` | 8px | 按钮、输入框、卡片 |
| `--radius-lg` | 12px | 对话框、大面板 |
| `--radius-full` | 999px | 状态指示灯、头像 |

> **关键变更**：基础圆角从 12px → **8px**。12px 的圆润感适合营销页面，但数据密集型后台需要更紧凑、更方正的视觉。

### 1.5 阴影

| Token | 值 | 用途 |
|-------|-----|------|
| `--shadow-sm` | `0 1px 2px rgba(0,0,0,0.05)` | 卡片默认 |
| `--shadow-base` | `0 1px 3px rgba(0,0,0,0.1), 0 1px 2px rgba(0,0,0,0.06)` | 悬浮卡片 |
| `--shadow-lg` | `0 4px 6px rgba(0,0,0,0.07), 0 2px 4px rgba(0,0,0,0.06)` | 弹出层、对话框 |
| `--shadow-none` | `none` | 表格（用 border 代替 shadow） |

> **关键变更**：
> - 去除所有绿色调阴影（`rgba(54, 179, 78, 0.xx)`）
> - 表格**不用阴影**，改用 `1px solid var(--border-default)` 边框 — 参考 Airtable
> - 按钮阴影仅在主 CTA 保留，且为中性灰色调

---

## 2. 组件规范

### 2.1 表格（核心组件）

表格是管理后台最核心的组件。当前问题：绿色表头 + border + stripe + shadow 四重叠加，视觉噪音极大。

**调整方向**：

| 属性 | 当前值 | 新值 | 理由 |
|------|--------|------|------|
| 表头背景 | `#EAF7EC`（绿色） | `var(--bg-subtle)` = `#F1F5F9`（中性灰） | 数据区不应有品牌色 |
| 表头文字 | `#1F8A3B`（绿色） | `var(--text-secondary)` = `#64748B` | 表头是辅助信息，不应抢占视觉 |
| border 属性 | `border`（全开） | 去掉 `border`，保留 `stripe` | 全边框太密，stripe 足够区分行 |
| box-shadow | `0 2px 10px rgba(54,179,78,0.08)` | `none` | 表格用 border 定边，不用 shadow |
| hover 背景 | `#EAF7EC`（绿色） | `var(--bg-subtle)` = `#F1F5F9` | 中性反馈 |
| 行高 | Element Plus 默认(~50px) | `--el-table-row-height: 44px` | 稍紧凑，提升数据密度 |
| 圆角 | 10px | 8px | 与全局 radius-base 一致 |

```css
/* 新表格样式 */
.el-table {
  border-radius: var(--radius-base);
  border: 1px solid var(--border-default);
  box-shadow: none; /* 去掉绿色阴影 */
}

.el-table th.el-table__cell {
  background-color: var(--bg-subtle) !important;
  color: var(--text-secondary);
  font-weight: 600;
  font-size: 12px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  /* 不再用绿色文字和绿色背景 */
}

.el-table .el-table__row:hover > td {
  background-color: var(--bg-subtle) !important;
  /* 中性灰 hover，不再是绿色 */
}
```

### 2.2 按钮

| 变体 | 背景 | 文字 | 边框 | 圆角 | 阴影 | 用途 |
|------|------|------|------|------|------|------|
| **Primary** | `#36B34E` | `#FFF` | none | 8px | `0 1px 2px rgba(0,0,0,0.1)` | 主操作（每页 ≤ 1 个） |
| **Default** | `#FFF` | `var(--text-regular)` | `1px solid var(--border-default)` | 8px | none | 次操作 |
| **Text/Link** | transparent | `var(--brand-green-dark)` | none | 4px | none | 表格内操作 |
| **Danger** | `#DC2626` | `#FFF` | none | 8px | none | 删除/禁用 |

> **关键变更**：
> - Primary 按钮阴影从 `rgba(54,179,78,0.22)` → `rgba(0,0,0,0.1)` — 中性阴影
> - 按钮字重从 `600` 降至 `500` — 参考 Supabase 的克制
> - 按钮尺寸从 `--el-component-size: 34px` → `32px`

### 2.3 输入框

```css
.el-input__wrapper {
  border-radius: var(--radius-base); /* 8px — 与全局圆角一致 */
  box-shadow: 0 0 0 1px var(--border-default) inset !important;
}

.el-input__wrapper:hover {
  box-shadow: 0 0 0 1px var(--border-strong) inset !important;
  /* 不再变绿 — 中性灰加深 */
}

.el-input__wrapper.is-focus {
  box-shadow: 0 0 0 2px #2563EB inset, 0 0 0 4px rgba(37,99,235,0.12) !important;
  /* focus 环用蓝色（信息色），而非品牌绿 — 参考 Sentry */
}
```

### 2.4 对话框

```css
.el-dialog {
  border-radius: var(--radius-lg); /* 12px */
  box-shadow: var(--shadow-lg); /* 中性灰阴影 */
}

.el-dialog__header {
  background: var(--bg-subtle); /* 中性灰底色，不再有绿色渐变 */
}

.el-dialog__title {
  font-weight: 600; /* 从 800 降至 600 */
  font-size: 16px; /* 从 18px 降至 16px */
  color: var(--text-primary); /* 近黑色，不再是绿色 */
}
```

### 2.5 侧边栏

| 属性 | 当前值 | 新值 |
|------|--------|------|
| 宽度 | 216px | 200px |
| Logo 文字色 | `#1F8A3B`（绿） | `var(--text-primary)` + 左侧 3px 绿色竖线 |
| 菜单激活色 | Element Plus 默认绿 | 背景 `var(--brand-green-bg)` + 文字 `var(--brand-green-dark)` |
| 菜单字号 | 默认 | 14px weight 500 |

### 2.6 页面骨架

```
┌──────────────────────────────────────────────────┐
│  Top Bar (52px, sticky)         ☀ 👤             │
├────────────┬─────────────────────────────────────┤
│            │  Page Header                        │
│  Sidebar   │  ┌─ H2 页面标题 ──┬── 操作区 ──┐   │
│  (200px)   │  │  20px/600      │ search + CTA│   │
│            │  └────────────────┴────────────┘   │
│            │                                     │
│            │  Content                            │
│            │  ┌─────────────────────────────────┐│
│            │  │  表格 / 卡片 / 表单              ││
│            │  │  (border, no shadow)             ││
│            │  └─────────────────────────────────┘│
│            │                                     │
│            │  Pagination (right-aligned)         │
└────────────┴─────────────────────────────────────┘
```

---

## 3. 强调色使用规则（最重要的规则）

> **"当你什么都是绿色的时候，绿色就什么都不是了。"**

### ✅ 允许使用品牌绿的场景

1. **Primary 按钮**的背景色
2. **文字链接**和**表格操作按钮**（text/link 类型）的默认色
3. **Sidebar 激活项**的文字色和左侧指示条
4. **开关(switch)**的激活色
5. **Logo** 旁的品牌名

### ❌ 禁止使用品牌绿的场景

1. ~~表头背景~~  → 用 `--bg-subtle`（中性灰）
2. ~~表头文字~~  → 用 `--text-secondary`
3. ~~行 hover 背景~~ → 用 `--bg-subtle`
4. ~~input focus 光环~~ → 用蓝色 `#2563EB`
5. ~~全局 box-shadow~~ → 用中性灰 `rgba(0,0,0,0.xx)`
6. ~~卡片/表格边框~~ → 用 `--border-default`
7. ~~对话框标题~~ → 用 `--text-primary`
8. ~~对话框 header 渐变~~ → 用 `--bg-subtle` 纯色

### 强调色密度参照（Supabase 原则）

每个视口内品牌绿出现的"面积比"应 **< 5%**：
- 1 个 Primary 按钮 ≈ 100×32px = 3200px²
- 2-3 个文字链接 ≈ 总计 ~1000px²
- 1 个 switch ≈ ~500px²
- **总计 < 5000px²** — 在一屏 1920×900 的视口中占比 ~0.3%

---

## 4. 数据展示规范

### 4.1 数字与金额

```css
/* 所有数字使用等宽数字特性 */
.kpi-value,
.cost-value,
.token-count,
.stat-number {
  font-family: var(--font-mono);
  font-feature-settings: 'tnum';
  font-variant-numeric: tabular-nums;
  font-weight: 600;
  color: var(--text-primary);
}
```

### 4.2 状态标识

使用**颜色 + 文字**双通道（WCAG 1.4.1）：

| 状态 | el-tag type | 附加 |
|------|-------------|------|
| 启用/正常 | `success` | 🟢 + "启用" |
| 禁用/停止 | `info` | ⚫ + "已禁用" |
| 异常/错误 | `danger` | 🔴 + "异常" |
| 警告/待处理 | `warning` | 🟡 + "待处理" |

### 4.3 技术内容

| 内容类型 | 字体 | 样式 |
|----------|------|------|
| API Key (`sk-****`) | `var(--font-mono)` | 13px, `letter-spacing: -0.02em` |
| Endpoint URL | `var(--font-mono)` | 13px, 可 truncate |
| Token 数量 | `var(--font-mono)` + `tabular-nums` | 右对齐 |
| 成本金额 (¥) | `var(--font-mono)` + `tabular-nums` | 右对齐，2 位小数 |
| 模型名称 | `var(--font-mono)` | 13px |
| 时间戳 | 普通字体 | 13px, 格式 `YYYY-MM-DD HH:mm` |

---

## 5. 暗色模式

暗色模式作为一等公民维护，但**不**是默认模式。

| 要素 | Light Mode | Dark Mode |
|------|------------|-----------|
| 页面底色 | `#F8FAFC` (slate-50) | `#0F172A` (slate-900) |
| 卡片底色 | `#FFFFFF` | `#1E293B` (slate-800) |
| 边框 | `#E2E8F0` (slate-200) | `#334155` (slate-700) |
| 品牌绿 | `#36B34E` | `#5DC370`（稍亮，保证对比） |
| 表头/hover | `#F1F5F9` (slate-100) | `#1E293B` (slate-800) |

> 注意：暗色模式**同样**遵守强调色克制原则。不要因为是暗色就把绿色放大。

---

## 6. 迁移清单（按优先级排序）

### P0 — 立即修复（影响全局观感）

| # | 修改项 | 文件 | 改动 |
|---|--------|------|------|
| 1 | 表格去绿 | `global.scss` L354-377 | 表头/hover 改为中性灰 |
| 2 | 阴影去绿 | `global.scss` L68-69, L306-307, L351, L383 | 所有绿色调 shadow → 中性灰 |
| 3 | focus 去绿 | `global.scss` L423-429 | input focus ring → 蓝色 |
| 4 | 对话框去绿 | `global.scss` L380-413 | header 渐变 → 纯色灰底, 标题色 → text-primary |
| 5 | 页面底色微调 | `variables.scss` L37 | `#F1F9F2` → `#F8FAFC` (去掉绿色调) |
| 6 | 边框去绿 | `variables.scss` L49-51 | `#D6EBD9` → `#E2E8F0` |
| 6.5 | input hover/focus 去绿 | `global.scss` L422-429 | hover→中性灰, focus→蓝圈 |

### P1 — 字号与字体（影响可读性）

| # | 修改项 | 文件 | 改动 |
|---|--------|------|------|
| 7 | 引入 Inter 字体 | `index.html` + `variables.scss` | Google Fonts CDN + font-family |
| 8 | 引入 JetBrains Mono | `index.html` + `variables.scss` | CDN + `--font-mono` |
| 9 | 补齐 title token | `variables.scss` | 新增 `--font-title-size: 20px` 等 |
| 10 | 表格行高调整 | `global.scss` | 添加 `--el-table-row-height: 44px` |
| 11 | 等宽类应用 | 各 .vue 页面 | 技术内容添加 `.mono-text` class |

### P2 — 组件微调（提升精致度）

| # | 修改项 | 文件 | 改动 |
|---|--------|------|------|
| 12 | **圆角统一（从 P2 提升）** | `variables.scss` | `--el-border-radius-base: 8px` (从12px降) — 一行改动，全局生效 |
| 13 | 按钮尺寸 | `global.scss` | `--el-component-size: 32px` (从34px降) |
| 14 | 按钮字重 | `global.scss` L291 | `font-weight: 500` (从600降) |
| 16 | 侧边栏 Logo 色 | `global.scss` L47 | 绿色 → text-primary + 绿色竖线 |
| 17 | 表单标签间距 | `global.scss` L453 | `margin-bottom: 18px` → `16px` |

### P3 — 渐进优化

| # | 修改项 | 说明 |
|---|--------|------|
| 18 | 侧边栏宽度 216→200px | 收益小、风险大，延后处理 |
| 19 | 表格去掉 `border` 属性 | 改用 `stripe` + 外边框，减少视觉噪音 |
| 19 | 添加 skeleton loading | 表格加载态使用 `el-skeleton` |
| 20 | 页面 header 抽组件 | 统一 18 个页面的 header 模式 |
| 21 | 图表配色标准化 | 参考 my-usage-v3 的 chartColors 序列 |

---

## 7. 对比示意（Before / After）

### 表头

```
Before:  background: #EAF7EC  |  color: #1F8A3B  |  font-weight: 600
         ┌──────────────────────────────────────┐
         │  🟩🟩🟩 绿色表头 🟩🟩🟩             │  ← 太抢眼
         └──────────────────────────────────────┘

After:   background: #F1F5F9  |  color: #64748B  |  font-weight: 600
         ┌──────────────────────────────────────┐
         │  🔲🔲🔲 中性灰表头 🔲🔲🔲           │  ← 安静，让数据说话
         └──────────────────────────────────────┘
```

### 按钮阴影

```
Before:  box-shadow: 0 4px 12px rgba(54, 179, 78, 0.22)
         ┌──────────┐
         │ 新建渠道  │ ▓▓▒▒░░  ← 绿色光晕
         └──────────┘

After:   box-shadow: 0 1px 2px rgba(0, 0, 0, 0.1)
         ┌──────────┐
         │ 新建渠道  │ ▓▓░░    ← 中性微影，干净
         └──────────┘
```

### Input Focus

```
Before:  绿色双线边框 + 绿色外光环
         ┌──────────────────┐
         │ 🔍 搜索...        │ ░░▒▒🟩🟩▒▒░░  ← 绿圈
         └──────────────────┘

After:   蓝色双线边框 + 蓝色外光环
         ┌──────────────────┐
         │ 🔍 搜索...        │ ░░▒▒🔵🔵▒▒░░  ← 蓝圈（信息色）
         └──────────────────┘
```

---

## 8. 统一主题声明

> 本规范统一所有页面的视觉风格，以下旧文档中的冲突定义**不再有效**：

| 旧文档 | 冲突内容 | 处理 |
|--------|---------|------|
| `xspec/modules/ops-console/design.md` | 暗色 #121212 + 青色 #00E5FF | ❌ 废弃，统一用本规范亮色方案 |
| `web/src/views/my-usage-v3-redesign-spec.md` | 深蓝 #0F172A + 蓝色 #3B82F6 | ⚠️ 仅作为暗色模式参考，主色仍为品牌绿 |
| `docs/frontend-design-guidelines.md` v1 | "Wise 北极星" + 绿色泛用 | ✅ 本规范 v2 替代，强调色克制 |

---

## 9. 附录：CSS 变量完整定义

```css
:root {
  /* ===== 品牌色（克制使用）===== */
  --brand-green: #36B34E;
  --brand-green-dark: #1F8A3B;
  --brand-green-light: #62C96C;
  --brand-green-bg: #EAF7EC;

  /* ===== 中性色 ===== */
  --bg-page: #F8FAFC;
  --bg-surface: #FFFFFF;
  --bg-subtle: #F1F5F9;
  --border-default: #E2E8F0;
  --border-light: #F1F5F9;
  --border-strong: #CBD5E1;

  /* ===== 文字 ===== */
  --text-primary: #0F172A;
  --text-regular: #334155;
  --text-secondary: #64748B;
  --text-placeholder: #94A3B8;

  /* ===== 语义色 ===== */
  --color-success: #059669;
  --color-warning: #D97706;
  --color-danger: #DC2626;
  --color-info: #2563EB;

  /* ===== 字体 ===== */
  --font-family: 'Inter', -apple-system, BlinkMacSystemFont, 'PingFang SC', 'Microsoft YaHei', sans-serif;
  --font-mono: 'JetBrains Mono', 'SF Mono', 'Fira Code', Consolas, monospace;
  --font-size-base: 14px;
  --font-size-sm: 13px;
  --font-size-xs: 12px;
  --font-size-title: 20px;
  --font-size-section: 16px;
  --font-size-display: 28px;

  /* ===== 圆角 ===== */
  --radius-sm: 4px;
  --radius-base: 8px;
  --radius-lg: 12px;
  --radius-full: 999px;

  /* ===== 阴影 ===== */
  --shadow-sm: 0 1px 2px rgba(0, 0, 0, 0.05);
  --shadow-base: 0 1px 3px rgba(0, 0, 0, 0.1), 0 1px 2px rgba(0, 0, 0, 0.06);
  --shadow-lg: 0 4px 6px rgba(0, 0, 0, 0.07), 0 2px 4px rgba(0, 0, 0, 0.06);

  /* ===== Element Plus 映射 ===== */
  --el-color-primary: var(--brand-green);
  --el-color-primary-light-3: #5DC370;
  --el-color-primary-light-5: #84D293;
  --el-color-primary-light-7: #ACE1B6;
  --el-color-primary-light-9: var(--brand-green-bg);
  --el-color-primary-dark-2: var(--brand-green-dark);
  --el-color-success: var(--color-success);
  --el-color-warning: var(--color-warning);
  --el-color-danger: var(--color-danger);
  --el-color-info: var(--color-info);
  --el-bg-color-page: var(--bg-page);
  --el-bg-color: var(--bg-surface);
  --el-bg-color-overlay: var(--bg-surface);
  --el-text-color-primary: var(--text-primary);
  --el-text-color-regular: var(--text-regular);
  --el-text-color-secondary: var(--text-secondary);
  --el-text-color-placeholder: var(--text-placeholder);
  --el-border-color: var(--border-default);
  --el-border-color-light: var(--border-light);
  --el-border-color-lighter: var(--bg-page);
  --el-fill-color-blank: var(--bg-surface);
  --el-fill-color: var(--bg-subtle);
  --el-fill-color-light: var(--bg-page);
  --el-border-radius-base: var(--radius-base);
  --el-border-radius-small: var(--radius-sm);
  --el-border-radius-round: var(--radius-full);
  --el-font-family: var(--font-family);
  --el-font-size-base: var(--font-size-base);
  --el-box-shadow: var(--shadow-base);
  --el-box-shadow-light: var(--shadow-sm);
}

/* ===== Dark Mode ===== */
html.dark {
  --bg-page: #0F172A;
  --bg-surface: #1E293B;
  --bg-subtle: #263242;
  --border-default: #334155;
  --border-light: #1E293B;
  --border-strong: #475569;
  --text-primary: #F1F5F9;
  --text-regular: #CBD5E1;
  --text-secondary: #94A3B8;
  --text-placeholder: #475569;
  --brand-green: #5DC370;
  --brand-green-dark: #84D293;
  --brand-green-bg: #1E293B; /* 暗色模式保持中性灰，不用绿色 */
  --shadow-sm: 0 1px 2px rgba(0, 0, 0, 0.2);
  --shadow-base: 0 1px 3px rgba(0, 0, 0, 0.3);
  --shadow-lg: 0 4px 6px rgba(0, 0, 0, 0.3);
}
```

---

*Created: 2026-06-21 | Reviewed by: Hermes Agent + Cursor Agent (Opus)*
*替代: frontend-design-guidelines.md v1, ops-console/design.md 视觉部分*
*Cursor Review 评分: 问题诊断 9/10, 颜色系统 8/10, 组件规范 9/10, 整体可行性 9/10*
*已统一意见: 字体自托管、暗色模式分层、图表色板、圆角P1提升、侧边栏P3延后*
