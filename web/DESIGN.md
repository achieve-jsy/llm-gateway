# DESIGN.md — 某公司大模型网关 · 管理后台

> AI 编码助手 / 设计协作的**视觉事实源**。遵循 [getdesign.md](https://getdesign.md/)（Google DESIGN.md spec）格式。
> 落地实现见 `web/src/styles/variables.scss`（token）与 `web/src/styles/global.scss`（组件样式），交互规范见 `docs/frontend-design-guidelines.md`。

---

## 1. 设计基调 (Design Identity)

- **北极星参考**：[Wise](https://getdesign.md/) —— *"Bright green accent, friendly and clear"*。浅色画布 + 单一自信的绿色主调 + 友好圆角。沿用其设计哲学，但**色相锁定为品牌自有翠绿**（`#36B34E`），不是 Wise 的柠檬绿。
- **气质关键词**：清新 / 自然 / 友好 / 清晰 / 信任。
- **同源约束**：管理后台的色彩体系必须与**登录页**（`web/src/views/auth/LoginPage.vue`）完全一致，确保「登录 → 进入面板」视觉连续、不跳色。
- **主色策略**：单一主色（绿）+ 中性灰阶 + 语义状态色。绿色**只用于「品牌 / 主操作 / 选中 / 链接 / 小面积 chrome 强调」**，不做正文字色、不做大面积背景。
- **可读性第一**：正文、标题、KPI 数字等**关键文字一律用近黑 slate**（`--el-text-color-primary: #1E293B`），保证对比度与长时阅读不累；绝不把主文字色设成品牌绿（历史上 `#1F8A3B` 当正文导致全平台发绿、晃眼，已废弃）。

---

## 2. 色彩 (Color Tokens)

### 2.1 品牌绿（登录页同源基准）

| Token | 值 | 用途 |
|---|---|---|
| `--brand-green` | `#36B34E` | 品牌主绿、主操作 |
| `--brand-green-light` | `#62C96C` | 渐变高光端 |
| `--brand-green-deep` | `#2A9C4B` | 渐变深色端、按压态 |
| `--brand-green-text` | `#2F9E4A` | 品牌文字强调 |

> 品牌渐变统一：`linear-gradient(135deg, var(--brand-green-light), var(--brand-green-deep))`。

### 2.2 主色阶 (Primary Scale · 明色模式)

| Token | 值 |
|---|---|
| `--el-color-primary` | `#36B34E` |
| `--el-color-primary-light-3` | `#5DC370` |
| `--el-color-primary-light-5` | `#84D293` |
| `--el-color-primary-light-7` | `#ACE1B6` |
| `--el-color-primary-light-9` | `#EAF7EC` |
| `--el-color-primary-dark-2` | `#1F8A3B` |

### 2.3 语义状态色 (Semantic)

> 语义色必须**和品牌草绿 `#36B34E` 拉开**，各自语义清晰、互不混淆。

| Token | 值 | 含义 | 备注 |
|---|---|---|---|
| `--el-color-success` | `#0EA371` | 成功 / 健康 | 偏冷的祖母绿，与品牌草绿区分；标签深色态文字用 `#0B8A5F` |
| `--el-color-warning` | `#E6A23C` | 警告 | 统一为经典琥珀橙（旧 `#EAB308`/`#FFD23F`/`#E6B800` 全部归一）；标签文字用 `#B97916` |
| `--el-color-danger` | `#E74C3C` | 危险 / 删除 | — |
| `--el-color-info` | `#5DADE2` | 信息 / 中性提示 | — |

### 2.4 背景 / 文字 / 边框 / 填充

| 类别 | Token | 明色 | 暗色 (`html.dark`) |
|---|---|---|---|
| 页面背景 | `--el-bg-color-page` | `#F1F9F2` | `#0C2613` |
| 卡片背景 | `--el-bg-color` | `#FFFFFF` | `#143523` |
| 文字主色（标题/正文/KPI） | `--el-text-color-primary` | `#1E293B` | `#EAF7EC` |
| 文字常规 | `--el-text-color-regular` | `#334155` | `#B9DCC0` |
| 文字次要 | `--el-text-color-secondary` | `#64748B` | `#84C592` |
| 占位文字 | `--el-text-color-placeholder` | `#94A3B8` | `#4F9362` |
| 边框 | `--el-border-color` | `#D6EBD9` | `#1F5230` |
| 填充 | `--el-fill-color` | `#EAF7EC` | `#1F5230` |

> **阴影统一带绿色调**：`--el-box-shadow: 0 2px 10px rgba(54, 179, 78, 0.10)`（即 `#36B34E` 的 10% alpha）。

### 2.5 图表调色板 (Chart Palette)

首位固定为品牌绿，其余保持高区分度（语义图中 输入=品牌绿、输出=成功祖母绿、缓存=信息蓝、成本=金）：

```
['#36B34E', '#0EA371', '#E0A100', '#5DADE2', '#8B5CF6', '#EC4899', '#E67E22', '#14B8A6']
```

---

## 3. 形状与圆角 (Shape)

| Token | 值 | 备注 |
|---|---|---|
| `--el-border-radius-base` | `12px` | 卡片 / 输入 / 弹窗 |
| `--el-border-radius-small` | `8px` | 标签 / 小控件 |
| `--el-border-radius-round` | `999px` | 胶囊按钮 / chip |

- 按钮圆角 `10px`，带轻微弹性按压（`scale(0.96)`）。
- 整体偏「友好圆角」，避免直角硬边。

---

## 4. 字体 (Typography)

- 字体族：`-apple-system, BlinkMacSystemFont, 'PingFang SC', 'Microsoft YaHei', sans-serif`
- 基准字号：`14px`
- 标题 / 品牌文字加粗（`700~800`），辅以品牌绿 `--el-text-color-primary`。

---

## 5. 组件约定 (Component Rules)

> **品牌绿强调文字**（logo / 表头 / 链接 / Tab 激活 / 弹窗标题等小面积 chrome）统一用深绿 `--el-color-primary-dark-2`（`#1F8A3B`）。**表单 label、KPI、正文等用中性色**，不要用绿，避免绿色过载。

- **主按钮**：品牌绿底白字 + 绿色调投影；hover 提亮到 `light-3`。
- **侧边菜单选中项**：品牌绿底白字 + 绿投影；hover 浅绿底 `--el-fill-color`。
- **表格表头**：浅绿底 `#EAF7EC` + 深绿字 `#1F8A3B`，行 hover 浅绿。
- **Tag**：使用语义类型（primary/success/warning/danger/info），不裸写色块。
- **输入聚焦**：绿色 2px inset + 4px 绿色光晕 `rgba(54,179,78,.15)`。

---

## 6. 铁律 (Hard Rules)

1. **禁止硬编码十六进制**：页面/组件颜色一律引用 `--el-*` / `--brand-*` CSS 变量。改主题只改 `variables.scss`。
2. **绿色当文字必须用深绿**：品牌主绿 `#36B34E` 仅用于「填充按钮 / 选中态」（白字绿底）。当**文字色**用（表格操作列链接、内联 `el-link`、文字按钮、Markdown 链接）时，明色模式一律用深绿 `#1F8A3B`（dark-2），否则 `#36B34E` 白底对比仅 ~2.9:1、看不清。已在 `global.scss` 全局兜底，新写链接无需重复处理。
3. **暗色必配**：任何新增颜色都要在 `html.dark` 下给出对应值。
4. **登录页同源**：不得引入与登录页冲突的第二套主色（历史上的 Teal `#2BA8A2` 体系已全量废弃）。
5. **状态双通道**：状态/健康不能只靠颜色，需「颜色 + 文字/图标」。

---

## 变更记录

- 统一为「某公司绿」体系：废弃旧 Teal `#2BA8A2`，主色锁定登录页同源翠绿 `#36B34E`，明/暗双模与图表调色板全量对齐。
- 设计语言定稿：① 成功色由 `#27AE60` 改为偏冷祖母绿 `#0EA371`，与品牌草绿拉开；② 警告色归一为琥珀橙 `#E6A23C`（清理 `#FFD23F`/`#E6B800`/`#EAB308` 等多套）；③ 主文字色由品牌绿 `#1F8A3B` 改为近黑 slate `#1E293B`，表单 label 改中性色，绿色仅保留给品牌/操作/选中/链接等小面积 chrome——降低视觉疲劳、保证关键文字清晰。
