# 用量看板前端修复 + 用户排行重设计

## 分支：feat/dashboard-redesign（从 test 拉出）
## 项目根目录：~/llm_gateway/web/

---

## 背景
用量看板（UsageDashboardPage.vue）有几个问题需要修复：
1. **用户排行**没有按管辖范围筛选——所有负责人看到全部用户，应该只看自己管辖范围内的人
2. **用量明细（logs）下钻bug**——点到部门时应该只看该部门的日志，但当前只处理了 user scope
3. **趋势图和模型分布**同样只处理了 user scope，没有处理 dept/studio scope
4. 用户排行 UI 需要重新设计（更美观）

## 后端 API 已就绪
- `GET /api/v1/usage/dashboard/users` — 已支持 `studio_id` 和 `dept_id` 参数
- `GET /api/v1/usage/scoped/logs` — 已支持 `dept_id` 和 `studio_id` 参数（刚加）
- `GET /api/v1/usage/dashboard/trend` — 需检查是否支持 dept_id/studio_id（如不支持，前端先传，后端忽略也无副作用）
- `GET /api/v1/usage/dashboard/models` — 同上
- `GET /api/v1/usage/dashboard/user-top-models` — 已支持 studio_id 和 dept_id

---

## 任务 1：修复面包屑下钻联动（最重要）

当前 `breadcrumbs` 是一个数组，每项有 `{ scope: string, id: string, name: string }`。
scope 层级：company → studio → dept → user

### 当前问题
所有数据加载函数只处理了 `scope === 'user'` 的情况，忽略了 studio 和 dept。

### 修复方案

**loadLogs()（约 1286 行）：**
```typescript
// 当前代码只处理 user:
const last = breadcrumbs.value[breadcrumbs.value.length - 1]
if (last?.scope === 'user' && last?.id) {
  params.user_id = last.id
} else {
  if (filterUserName.value) params.user_name = filterUserName.value
}

// 改为：
const last = breadcrumbs.value[breadcrumbs.value.length - 1]
if (last?.scope === 'user' && last?.id) {
  params.user_id = last.id
} else if (last?.scope === 'dept' && last?.id) {
  params.dept_id = last.id
} else if (last?.scope === 'studio' && last?.id) {
  params.studio_id = last.id
} else {
  if (filterUserName.value) params.user_name = filterUserName.value
}
```

**loadTrend()（约 944 行）：** 同样加 dept_id/studio_id
**loadModels()（约 1124 行）：** 同样加 dept_id/studio_id
**loadUserRanking()（约 1394 行）：** 加 studio_id/dept_id 参数，让排行只显示范围内用户
**用户排行 top-models 请求（约 1407 行）：** 同样加 studio_id/dept_id

### 模式统一
建议提取一个 helper：
```typescript
function scopeParams(): Record<string, string> {
  const last = breadcrumbs.value[breadcrumbs.value.length - 1]
  if (!last?.id) return {}
  if (last.scope === 'user') return { user_id: last.id }
  if (last.scope === 'dept') return { dept_id: last.id }
  if (last.scope === 'studio') return { studio_id: last.id }
  return {}
}
```

---

## 任务 2：用户排行 UI 重设计

### 当前结构
用户排行是 UsageDashboardPage.vue 中 435-527 行的 template 区域 + 1364-1497 行的 script 区域。
**不需要抽离组件**，直接在原位重设计即可。

### 设计要求
严格遵循项目 Design v2 Tokens（`web/src/styles/variables.scss`）：
- 品牌绿 `#36B34E` **仅用于 CTA/链接/活跃态**，数据区用中性灰
- 圆角 `8px`（`--radius-base`）
- 字体 `Inter` + `JetBrains Mono`（数字）
- 阴影用中性灰（`--shadow-sm/base/lg`）
- 图表色板：`--chart-1` 到 `--chart-8`

### 重设计方向
1. **KPI 卡片行**：活跃用户、人均费用、人均 Token、人均调用 — 更精致，参考 Supabase dashboard 风格
2. **排行表格**：
   - 前三名用奖牌色（金/银/铜）或数字 badge
   - 费用列用绿色强调（`--brand-green-text`）
   - 错误数 > 0 用 `--color-danger` 色
   - Top3 模型用紧凑的 tag/badge
3. **环形图**：使用 `--chart-*` 色板，更专业的 tooltip
4. **空状态**：友好的空数据提示
5. **响应式**：移动端卡片堆叠

### 管辖范围提示
当 breadcrumbs 有 dept/studio scope 时，在用户排行区域顶部显示一个提示条：
"当前展示 **XX部门** 的用户排行" 带一个"查看全部"按钮

---

## 关键约束
- **不要修改后端代码**（后端已改好）
- **不要修改 UsageRecordTable 组件**
- **不要引入新依赖**
- 保持现有 CSS 变量体系（`--ud-*` scoped vars + Design v2 tokens）
- 不要改其他页面（MyUsagePage、UsageCostPage 等）
- UsageDashboardPage.vue 当前 2329 行，是个大文件，小心操作

## 验证
修改完成后：
1. `cd ~/llm_gateway/web && npm run build` 确保编译通过
2. 检查 git diff 确认改动合理
