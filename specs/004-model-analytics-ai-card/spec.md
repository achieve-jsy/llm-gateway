# 实施方案 · 模型用量分析 · AI 分析卡增强 · 004

> 状态：实施方案 · 待开发
> 关联：003-model-performance-dashboard（用量看板页）、web/prototype（对齐原型）、飞书需求规格第 8 章 + 技术方案第 5 章（已定稿增强设计）
> 分支：claude/model-analytics-ai-card（基线 4ae13801）

---

## 1. 一句话描述

在真实管理后台 `/admin/usage/dashboard`（`web/src/views/admin/usage/UsageDashboardPage.vue`）落地 **AI 分析卡**，对齐原型 `web/prototype/` 的增强三段式设计（随筛选联动 + 数字锚定下钻 + 全局中位数参照 + 异步加载），后端新增 `/analytics/insight` 端点承载洞察。

---

## 2. 现状

| 项 | 状态 |
|---|---|
| 后端 `model-hourly` / `model-users` 端点 | ✅ 已实现（`internal/observability/handler/model_analytics.go`，基线 4ae13801），数据源 `call_logs` |
| 前端真实页面 | ✅ 存在 `UsageDashboardPage.vue` + 同目录 `components/`（KpiCards / TrendChartSection / RankingTable 等） |
| AI 分析卡组件 | ❌ 无，需新建 |
| 前端接入 model-hourly/model-users | ❌ 未接入（api 层无对应调用） |
| 筛选控件 | ⚠️ `UsageFilterBar.vue` 有 dept/studio 选项但页面未传，**缺 tier（等级）筛选** |
| 原型 | ✅ `web/prototype/model-analytics.js`：`renderInsight` / `currentScope` / `drillHtml` 完整逻辑（对齐参考） |

---

## 3. 目标与成功标准

| # | 目标 | 衡量方式 |
|---|------|----------|
| G1 | 真实页面出现 AI 分析卡，三段式 事实/原因/建议 | 页面渲染出三段 + 范围标签 |
| G2 | 分析范围随筛选联动 | 筛选部门/模型/等级后卡片范围标签与内容切换 |
| G3 | 每条洞察锚定真实数字 + 可下钻 | 洞察文本引用数字、带下钻链接且点击生效 |
| G4 | 人均对比用全局中位数参照 | 筛选后人均口径不变，可横向比较 |
| G5 | 异步加载不阻塞页面 | 骨架屏 + 按范围缓存 + 防抖 |
| G6 | 代码质量 | 后端 `go build ./...` / `go vet` / `go test` 通过；前端 `npm run build` 通过 |

**不做（MVP）**：真 LLM 接入（规则引擎即 MVP）、跨周期对比页、自动刷新、告警推送。

---

## 4. 数据契约

### 4.1 复用已有端点（前端新接入）

- `GET /api/v1/usage/dashboard/model-hourly?start&end&model_name&tier_name&group_by` → `{hourly:[{hour,group_key,prompt_tokens,cached_tokens,completion_tokens,est_cost_usd,call_count}], daily:[...]}`
- `GET /api/v1/usage/dashboard/model-users?start&end&model_name&tier_name&dept_name&group_by=dept&period=day` → `{dept_rows:[{dept_name,user_count,cost_per_user,cost_pct}], summary:{total_users,total_cost_usd}}`

### 4.2 新增端点（Agent A）

`POST /api/v1/analytics/insight`（guard `PermUsageViewScoped`）

```json
// Request
{ "start": "2026-07-28", "end": "2026-07-30",
  "scope": { "kind": "dept", "key": "数据平台部" } }   // kind: company|dept|model|tier

// Response
{ "scope": { "kind": "dept", "key": "数据平台部", "label": "部门：数据平台部" },
  "gmed": 210.5,                                    // 全司当前周期人均中位数（全局参照，不受筛选影响）
  "metrics": { "cost": 668.07, "prev_cost": 530.0, "delta_pct": 26.1,
               "budget_pct": 15.9, "high_tier_pct": 66.0, "cache_rate": 31.2 },
  "segments": {
    "facts":        [ { "text": "...", "drill": { "dept": "X", "model": "Y", "scroll": "overview" } } ],
    "causes":       [ { "text": "...", "drill": {...} } ],
    "suggestions":  [ { "text": "...", "drill": {...}, "est": true } ]
  } }
```

`drill` 缺省字段省略；`text` 为已拼好的中文洞察（含数字锚定）；估算建议带 `est: true`。规则引擎在 Go 端实现（对齐原型 `renderInsight` 的规则：环比主因 = 绝对变动最大模型；人均超标 = 部门人均/全司中位数 ≥ 2（≥3 严重）；省额 = L3 头部模型费用 × 30%）。

---

## 5. 后端实现（Agent A）

- 新文件 `internal/observability/handler/analytics_insight.go`
- 结构：`InsightRequest` / `InsightScope` / `InsightResponse` / `Segment` / `Drill`；handler `AnalyticsInsight`
- 复用：`parseDateRangeFromTo`、`policy.UsagePolicy.QueryScope(actor)`、`obsmodel.WhereExcludeServiceKeyLogs`、`dept_name`/`user_id` 聚合模式（对齐 `model_analytics.go` 既有 SQL 风格）
- 实现：范围判定（company=全司/ dept / model / tier）→ 取当前区间 + 上一区间两段聚合 → 计算 事实/原因/建议 三段文本
- `router.go` 注册 `POST /api/v1/analytics/insight`（`guard(rbac.PermUsageViewScoped)`）
- 单元测试 `analytics_insight_test.go`：对确定性 mock 行集断言三段文本命中预期数字与 drill

---

## 6. 前端实现（Agent B + C）

### 6.1 新组件 `AiAnalysisCard.vue`（Agent B，纯展示）

- 位置：`web/src/views/admin/usage/components/AiAnalysisCard.vue`
- 职责：渲染三段式卡片（头像 + 范围标签 + 事实/原因/建议 + footer 说明），接收 props，不外发请求
- Props：`scopeLabel: string`、`segments: { tag: '事实'|'原因'|'建议', html: string }[]`、`loading: boolean`
- Emit：`drill(payload: {dept?, model?, scroll})`（父页面处理下钻行为）
- 对齐原型视觉：`web/prototype/model-analytics-system-prototype.html` 的 `.ai-card/.ai-hd/.ai-sec/.ai-drill/.ai-flag.est` 样式，映射到 `variables.scss` 品牌绿 token（`--brand-green*` / `--el-text-color-*`），天然支持 `html.dark`
- 骨架屏：`loading=true` 时显示三行灰条占位

### 6.2 数据层（Agent C）

- `web/src/api/usage.ts`：新增 `getInsight(params)`（POST `/analytics/insight`）、`getModelHourly`、`getModelUsers`（封装 http.get）
- `web/src/types/dashboard.ts` 或新 `types/analytics.ts`：`InsightScope/InsightResponse/Segment/Drill` 类型
- 组装：页面取当前筛选 state + 日期区间 → 调 insight → 渲染组件

### 6.3 集成 + 筛选联动（Agent C）

- `UsageDashboardPage.vue`：引入 `AiAnalysisCard`，传 scopeLabel/segments/loading；实现 `drill` 处理（下钻部门展开对应行、下钻模型滚动到模型区）
- `UsageFilterBar.vue`：确认 dept/studio 透传；**补 tier 等级筛选**（对齐原型 chips-model/tier/dept 三组）
- 范围联动：由当前筛选推导 company/dept/model/tier（对齐原型 `currentScope()`）
- 异步：加载骨架 + 按「范围+区间」Map 缓存 + 筛选防抖（数百 ms）

---

## 7. 测试与验收

```bash
# 后端
cd <repo> && go build ./... && go vet ./internal/observability/... && go test ./internal/observability/...
# 前端
cd web && npm run build    # 或对应的 type-check / lint
```

验收清单（对齐原型，逐项核对浏览器）：
- [ ] 卡片三段渲染、范围标签正确
- [ ] 筛选部门/模型/等级后卡片联动更新
- [ ] 洞察数字与下钻路径真实可点（下钻到部门明细/模型行）
- [ ] 人均对比引用全局中位数（筛选后基准不变）
- [ ] 异步骨架 + 缓存 + 防抖生效，页面不阻塞

---

## 8. 里程碑与 agent 分工

| 里程碑 | 内容 | 负责 |
|---|---|---|
| M0 | 实施方案（本文件） | 主 agent |
| M1 | 后端 `/analytics/insight` + 单测 | Agent A |
| M2 | 前端 `AiAnalysisCard.vue` 组件 | Agent B |
| M3 | 集成 + 筛选 + 数据层 + 异步 | Agent C |
| M4 | 集成验证（build + test + 预览对齐） | 主 agent |

**文件归属（避免冲突）**：
- A：`internal/observability/handler/analytics_insight.go`、`analytics_insight_test.go`、`router.go`
- B：`web/src/views/admin/usage/components/AiAnalysisCard.vue`
- C：`web/src/api/usage.ts`、`web/src/types/analytics.ts`、`UsageDashboardPage.vue`、`UsageFilterBar.vue`

---

## 9. 风险

| 风险 | 对策 |
|---|---|
| 前后端契约漂移（text/drill 结构） | 契约先定死（§4.2），B/C 按契约实现，A 输出对齐；M4 集成验证 |
| 前端 build 慢 / 类型报错 | Agent B/C 完成后主 agent 统一 build，早暴露 |
| 下钻行为与既有页面结构耦合 | 下钻先做轻量（滚动 + 高亮），不做跨路由跳转 |
