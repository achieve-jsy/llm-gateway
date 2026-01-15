# 前端设计规范 (Frontend Design Guidelines)

> 管理后台 (`web/`，Vue3 + Element Plus) 的 UI/UX 规范，是前端视觉与交互的**事实源**。
> 每条规范都标注了背后的 UX 原则与依据，便于评审时援引。底层设计 token 见 `web/src/styles/variables.scss`、全局组件样式见 `web/src/styles/global.scss`。
>
> 配套 Cursor 规则：`.cursor/rules/frontend-ui.mdc`（编辑 `web/**/*.vue` 时自动生效）。

## 0. 总则

1. **复用优先**：先用 Element Plus 组件 + 已有全局样式 token，不要在页面内造轮子、写一次性硬编码颜色/字号。
2. **单一来源**：颜色、字体、圆角、间距走 `variables.scss` 的 CSS 变量；格式化函数走 `web/src/utils/`。
3. **遵循平台惯例 (Jakob's Law)**：尽量沿用 Element Plus 的默认交互范式，用户已在其他后台习得，不要自创。

---

## 1. 设计 Token（颜色 / 字体 / 圆角）

**依据**：Design Tokens / Single Source of Truth、Consistency & Standards (Nielsen #4)

- 颜色一律用 `--el-color-*`、`--el-text-color-*`、`--el-border-color-*`、`--el-fill-color-*`，**禁止**在页面里写 `#xxxxxx` 硬编码（语义状态色除外，且应集中定义）。
- 主色 teal `#2BA8A2`，成功/警告/危险/信息见 `variables.scss`。
- 暗色模式：任何新增颜色都要在 `html.dark` 下给出对应值，否则暗色下会失真。
- **标题字号/字重**：使用统一 token（见下「待补齐 token」），不要每页 `font-size: 16px` 各写一套。

> ⚠️ 已知债务：页面中引用的 `--font-title-size` / `--font-title-weight` / `--font-family` 当前**未在 `variables.scss` 定义**，导致标题样式 fallback。新建页面前应先补齐这组 token（或改用 `--el-font-size-*`）。

---

## 2. 页面骨架

**依据**：Consistency & Standards、Aesthetic-Usability

所有 admin 页面统一结构：

```
page-header（标题 H2 + 右侧操作区）
  → 工具栏（搜索 / 筛选 / 主操作）
  → 内容区（表格 / 卡片 / 表单）
  → 分页（右对齐）
```

- 标题用 `.page-title`，操作区用 `.header-actions`（`flex` + `gap:10px` + `flex-wrap`）。
- 窄屏 (`<900px`) header 改为纵向堆叠（已有响应式样式可复用）。
- 这套 header/分页样板**应抽成公共组件或复用现有写法**，避免每页重复粘贴。

---

## 3. 工具栏与操作密度

**依据**：**Hick's Law**（选项越多决策越慢）、**Miller's Law**（7±2）、Progressive Disclosure

- 顶部工具栏**主操作 ≤ 1～2 个**（如「新建/注册」放最右，`type="primary"`）。
- **批量操作收进 `批量操作(N)` 下拉**，且仅在选中行 > 0 时出现，不要平铺多个批量按钮。
- 多个筛选项（>3）应收进可折叠筛选区或 `el-popover`，避免一行排满。
- **表格操作列按钮 ≤ 3 个**高频项，其余收进 `⋯` (`el-dropdown`)。

```vue
<!-- ❌ 避免：批量按钮平铺 + 工具栏过载 -->
<el-button>批量启用</el-button><el-button>批量禁用</el-button>
<el-button>批量打标签</el-button><el-button>批量删除</el-button>

<!-- ✅ 推荐：选中后才出现的下拉 -->
<el-dropdown v-if="selected.length" @command="onBatch">
  <el-button>批量操作 ({{ selected.length }})<el-icon><ArrowDown /></el-icon></el-button>
  <template #dropdown>
    <el-dropdown-menu>
      <el-dropdown-item command="enable">启用</el-dropdown-item>
      <el-dropdown-item command="disable">禁用</el-dropdown-item>
      <el-dropdown-item command="delete" divided>删除</el-dropdown-item>
    </el-dropdown-menu>
  </template>
</el-dropdown>
```

---

## 4. 三态：加载 / 空 / 错误（强制）

**依据**：Visibility of System Status (Nielsen #1)、Empty States、Skeleton Loading

每个列表/数据页**必须**显式处理三态，不允许加载期空白闪烁、无数据一片空白：

- **加载态**：`v-loading` 或 `el-skeleton`（表格用 `v-loading` 包裹）。
- **空态**：`el-empty`，并尽量带「引导主操作」按钮（如「新建第一个渠道」）。
- **错误态**：`ElMessage.error` 提示 + 可重试入口（按钮或重新加载）。

```vue
<el-table v-loading="loading" :data="rows">…</el-table>
<el-empty v-if="!loading && !rows.length" description="暂无数据">
  <el-button type="primary" @click="openCreate">新建</el-button>
</el-empty>
```

---

## 5. 操作反馈一致性（强制）

**依据**：Visibility of System Status、Feedback

- **所有写操作**（含行内编辑/开关切换/拖拽排序）：成功 `ElMessage.success('xxx成功')`，失败 `ElMessage.error('xxx失败')` 并**回滚 UI 状态**。
- **禁止「静默成功」**：行内改优先级/权重等保存后必须有提示，否则用户不知是否生效。
- 异步按钮加 `:loading`，防重复提交。

---

## 6. 危险 / 破坏性操作确认（强制 + 统一）

**依据**：Error Prevention (Nielsen #5)、Confirmation for Destructive Actions

统一确认范式，避免目前「popconfirm / MessageBox / 无确认」三套并存：

| 操作类型 | 范式 | 说明 |
|---|---|---|
| 单行删除（轻量） | `el-popconfirm` | 就近确认 |
| 批量删除 / 影响线上流量（禁用渠道/模型/模型组） | `ElMessageBox.confirm` | 文案含**对象名 + 后果**，`type` 用 `warning`/`error` |
| 不可逆操作 | `ElMessageBox.confirm` + 明确「不可恢复」 | — |

- **影响线上的 `el-switch`（如停用生产模型）不可零确认直接生效**：要么二次确认，要么提供「撤销」。

```ts
await ElMessageBox.confirm(
  `确定禁用模型「${row.name}」？禁用后该模型将停止对外服务`,
  '禁用模型', { type: 'warning', confirmButtonText: '确定', cancelButtonText: '取消' }
)
```

---

## 7. 状态可视化与可访问性

**依据**：WCAG 1.4.1（不依赖颜色单一通道）、Accessibility

- 状态/健康等**不能只靠颜色**：颜色 + 文字 / 图标双通道（如健康用「色点 + 文字 + tooltip」）。
- `el-tag` 状态色统一用语义类型 (`success`/`warning`/`danger`/`info`)，不要自定义裸色块。
- 可点击的弱操作（如带 dashed 下划线的名称）保持可识别的视觉提示。

---

## 8. 表单

**依据**：Miller's Law、Error Prevention、Recognition over Recall (#6)

- 表单分组：**每组字段 ≤ 7**，超出用 `el-divider`/分步/Tab 分段。
- 必填项校验用 `el-form` rules，**提交前校验**，错误就近显示，不要只在提交后报。
- 危险/高风险字段（如凭证、endpoint）给占位示例与说明 (`tooltip`/`form-item` 提示)。
- 默认值合理化：能给默认值的别留空让用户猜。

---

## 9. 复用与 DRY

**依据**：DRY / Single Source of Truth、Consistency

- 日期/token/货币等格式化**统一走 `web/src/utils/`**（`format.ts`、`currency.ts`），禁止页面内重复实现 `formatDateTime` 之类。
- 重复出现的 UI 片段（page-header、状态标签、健康指示器）应抽成公共组件 `web/src/components/`。

---

## 10. 响应式

**依据**：Responsive Design、Mobile-first 兜底

- 复用 `global.scss` 已有断点（1024 / 768 / 640 / 480）。
- 窄屏：工具栏纵向堆叠、表格横向滚动、对话框近全屏、表单 label 顶部对齐（已有全局样式覆盖，沿用即可）。

---

## 如何用 uxuiprinciples.com 复核

1. 新页面/大改动上线前，把关键页面截图丢进其 **AI Validator（免费）** 做一次启发式复核。
2. 评审中如需「权威依据」，可在本规范对应条目补充该站的原则编号与学术引用。

---

## 变更记录

- 初版：基于对 `ProviderListPage` / `ModelListPage` 等核心页面的 UX 审计建立。
