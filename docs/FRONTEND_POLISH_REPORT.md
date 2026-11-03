# 前端设计优化报告 (feat/frontend-design-polish)

> 基于 `main` 分支 `commit` 的完整审计
> 审计人：FE Hermes | 标准：前端设计原则知识库 + vue-admin-responsive patterns

---

## 一、严重问题 (P0 — 必须立即修复)

### 1.1 双品牌色冲突 🔴 Critical

**问题**：`tailwind.config.js` 定义 `primary: #2BA8A2`（teal 青色），但 `variables.scss` 定义品牌绿 `#36B34E`。两套颜色系统互相矛盾，任何使用 Tailwind `text-primary` / `bg-primary` class 的组件会显示错误颜色。

**设计依据**：Consistency & Standards (Nielsen #4)、Single Source of Truth

**修复**：将 `tailwind.config.js` 的 primary 对齐到品牌绿 `#36B34E`，同步 light/dark 变体。

**文件**：`tailwind.config.js`

### 1.2 Emoji 作为 UI 图标 🔴 Critical

**问题**：项目中有 **27 处** emoji 被用作 UI 图标，违反设计红线。

| 文件 | Emoji 用途 | 问题 |
|------|-----------|------|
| `App.vue:164` | `☀️` / `🌙` 主题切换 | 跨平台渲染不一致 |
| `MyUsagePage.vue:77,367` | `💰` / `⚡` 区块标题 | emoji 代替 SVG icon |
| `UnifiedEntryPage.vue` | `🌱⚡💎🎯🌙🔥💰` persona 标签 | 大量 emoji 作为功能图标 |
| `MyBudgetPage.vue:166` | `⚡` 提示信息 | emoji 装饰 |
| `OpsErrorDetailModal.vue:82` | `⚡` 延迟指标 | emoji 代替 icon |

**设计依据**：设计红线 — "禁止 emoji 作为 UI 图标"

**修复方案**：
- 主题切换 → Element Plus `<Sunny />` / `<Moon />` icon
- 区块标题 → Element Plus icon 或 inline SVG
- Persona 标签 → 保留 emoji 作为**内容装饰**（非功能图标），这是可接受的

**文件**：`App.vue`, `MyUsagePage.vue`, `MyBudgetPage.vue`, `OpsErrorDetailModal.vue`

---

## 二、高优先级问题 (P1 — 应该修复)

### 2.1 `!important` 使用 + 硬编码色值

**问题**：~30 处 `!important`，其中约 10 处可接受（移动端 Dialog 覆盖），但有多处不可接受：

| 文件 | 问题代码 | 应改为 |
|------|---------|--------|
| `NotificationBell.vue:356` | `color: #1a1a1a !important` | `color: var(--el-text-color-primary)` |
| `NotificationBell.vue:358` | `color: #1F8A3B !important` | `color: var(--el-color-primary)` |
| `AnnouncementDialog.vue:214-220` | 多处硬编码色值 + `!important` | 使用 Element Plus 语义变量 |
| `AnnouncementDialog.vue:217` | `color: #1a1a1a !important` | `color: var(--el-text-color-primary)` |

**硬编码色值统计**：共 **50 处**，主要集中在：
- `NotificationBell.vue` — 20+ 处 `#1a1a1a` / `#64748b` / `#94a3b8` / `#1F8A3B`
- `LoginPage.vue` — 10+ 处（可接受，登录页是独立视觉系统）
- `AnnouncementBanner.vue` — 3 处语义色（可接受，属于状态色）

**设计依据**：设计红线 — "永远不写 `!important`"、Design Token 单一来源

**修复**：
1. `NotificationBell.vue` — 替换所有硬编码色值为 `var(--el-*)` 变量
2. `AnnouncementDialog.vue` — 替换硬编码色值，移除不必要的 `!important`
3. 保留移动端 Dialog 的 `width: 95% !important`（Element Plus teleport 必须）

### 2.2 字号系统不一致

**问题**：实际使用了 **18 种字号**，大量游离于类型系统之外：

```
类型系统（规范）: 12/14/16/20/24/32/40/48
实际使用:         10/11/12/13/14/15/16/17/18/20/22/24/36/38/44/48/52/64
```

**关键问题**：
- **9px** (3处) — 低于可读性底线，热力图标签
- **11px** (10处) — 接近下限，辅助信息
- **13px** (13处) — 不在系统中（应统一到 12px 或 14px）
- **15px / 17px / 22px** — 各 1-2 处，不在系统中

**设计依据**：类型系统 12/14/16/20/24/32/40/48

**修复**：
- 9px → 10px（已是 `--font-size-xxs` token）
- 13px → 12px（辅助信息）或 14px（正文）
- 15px → 14px / 17px → 16px / 22px → 20px
- 保留 11px 作为 `--font-size-xs` 的降级（已有 token 定义）

### 2.3 间距不遵循 4px 网格

**问题**：gap/margin 值中有大量非 4px 倍数：

| 非标准值 | 出现次数 | 建议修正 |
|---------|---------|---------|
| 1px | 1 | 0 或 2px |
| 2px | 4 | 4px（最小间距单位） |
| 3px | 3 | 4px |
| 5px | 1 | 4px |
| 6px | **14** | 8px |
| 10px | **9** | 8px 或 12px |

**设计依据**：4px 网格系统

**修复**：批量替换 gap 值到 4px 倍数（不修改 padding/margin 中合理的小值如 `border-radius`、`border-width`）。

---

## 三、中优先级问题 (P2 — 建议修复)

### 3.1 Focus Ring 不统一

**问题**：
- 表单 focus: `box-shadow: 0 0 0 2px var(--color-info)` — 蓝色
- 菜单活跃态: `background: var(--brand-green)` 填充 — 绿色
- 按钮 focus: Element Plus 默认 outline — 品牌色

**修复**：统一为 `box-shadow: 0 0 0 2px var(--el-color-primary-light-5)` 品牌绿半透明环。

### 3.2 巨型组件文件

**问题**：

| 文件 | 行数 | 建议 |
|------|------|------|
| `MyUsagePage.vue` | **1896 行** | 拆分为 6-8 个子组件 |
| `OpsDashboardHeader.vue` | **1545 行** | 拆分为 4-5 个子组件 |
| `UsageDashboardPage.vue` | ~2000 行 | 拆分为 6-8 个子组件 |

**设计依据**：单一职责原则、组件化体系

**本次范围**：标注 TODO，不实际拆分（影响面太大，需要单独 feature 分支）。

### 3.3 Tailwind 配置冗余

**问题**：`tailwind.config.js` 定义了完整的颜色/阴影系统，但项目 95% 用 SCSS。配置中有 `coral` (#EF6C4A)、`amber` (#FFC400) 等从未使用的颜色。

**修复**：精简 tailwind.config.js，删除未使用的颜色定义，保留与 SCSS 对齐的品牌色。

### 3.4 variables.scss 缺失 Token

**问题**：`frontend-design-guidelines.md` 已标注的技术债：
- `--font-title-size` — 未定义
- `--font-title-weight` — 未定义
- `--font-family` — 未定义（虽然有 `--font-family-sans`）

**修复**：在 `variables.scss` 中补齐缺失的 token。

---

## 四、低优先级 (P3 — 锦上添花)

### 4.1 暗色模式 NotificationBell

`NotificationBell.vue` 的 popover 样式全部硬编码浅色，暗色模式下文字不可读。需要添加 `html.dark` 适配。

### 4.2 动画信手拈来

部分页面缺少过渡动画：
- Dialog 打开/关闭（Element Plus 默认已有）
- 列表项添加/删除（应加 `<TransitionGroup>`）
- 数据刷新时的数字翻转效果

### 4.3 可访问性 aria-label 缺失

侧边栏按钮、主题切换、筛选器等缺少 `aria-label`。

---

## 五、修改执行计划

| 序号 | 修改项 | 文件 | 预计影响 |
|------|--------|------|---------|
| 1 | 统一 Tailwind 品牌色 | `tailwind.config.js` | 消除视觉 bug |
| 2 | 替换 emoji 为 SVG icon | `App.vue`, `MyUsagePage.vue`, `MyBudgetPage.vue`, `OpsErrorDetailModal.vue` | 跨平台一致性 |
| 3 | 清理 NotificationBell 硬编码色值 | `NotificationBell.vue` | Dark Mode 可用 |
| 4 | 清理 AnnouncementDialog 硬编码色值 | `AnnouncementDialog.vue` | Token 一致性 |
| 5 | 9px → 10px 字号修正 | 3 个文件 | 可读性 |
| 6 | 补齐 variables.scss 缺失 Token | `variables.scss` | 类型系统完整 |
| 7 | 精简 tailwind.config.js | `tailwind.config.js` | 减少冗余 |
| 8 | Focus Ring 统一 | `global.scss` | 无障碍 |

---

## 六、不修改（记录原因）

| 项 | 原因 |
|----|------|
| 巨型组件拆分 | 影响面大，需单独分支 |
| `!important` (Dialog 移动端) | Element Plus teleport 导致，技术上不可避免 |
| LoginPage.vue 硬编码色值 | 登录页是独立视觉系统（视频背景 + 半透明），不强制走 token |
| AnnouncementBanner.vue 状态色 | 属于语义状态色，允许集中定义 |
| UnifiedEntryPage.vue persona emoji | 属于**内容装饰**（非功能图标），设计原则允许 |

---

*报告生成时间：2026-06-26 | 分支：feat/frontend-design-polish*
