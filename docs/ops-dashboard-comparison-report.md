# Sub2API → Sunny Canyon 运维看板对比分析报告

> 生成时间: 2026-06-30  
> 对比对象: sub2api (`/Users/jsy/sub2api_final`) vs sunny-canyon (LLM Gateway)

---

## 一、总体架构对比

| 维度 | Sub2API | Sunny Canyon (LLM Gateway) |
|------|---------|---------------------------|
| **前端框架** | Vue 3.4+ / **TailwindCSS** | Vue 3 / **Element Plus** |
| **样式体系** | Tailwind utility classes + scoped CSS | CSS Variables + scoped `.ops-*` 命名空间 |
| **图表库** | 未明确（推测 Chart.js / apexcharts） | ECharts |
| **i18n** | vue-i18n（多语言） | 硬编码中文（无 i18n） |
| **路由** | vue-router `/admin/ops` | vue-router `/admin/ops` |
| **API 前缀** | `/api/v1/admin/ops/*` | `/api/v1/ops/*` |
| **主页面文件** | `OpsDashboard.vue` (832 行) | `OpsDashboardPage.vue` (603 行) |
| **子组件数** | 19 个 `.vue` 文件 | 18 个 `.vue` 文件 |
| **WebSocket** | 原生 WS（含 h2c），`subscribeQPS()` | 未实现（无实时推送） |

---

## 二、界面布局对比（逐行分析）

### 2.1 顶部工具栏 (Header)

| 特性 | Sub2API | Sunny Canyon | 差距 |
|------|---------|-------------|------|
| 标题 | 多语言，无图标 | "运维监控" + `DataBoard` 图标 | 风格差异 |
| Platform 下拉 | `Select` 组件，4 选项 | `el-select`，5 选项（含"其他"） | 基本一致 |
| Group 下拉 | 动态加载 + 按 platform 级联过滤 | 动态加载 + 按 platform 级联过滤 | ✅ 一致 |
| 时间范围 | 5m/30m/1h/6h/24h + **自定义时间弹窗** | 5m/30m/1h/6h/24h + **自定义时间弹窗** | ✅ 一致 |
| 查询模式 | auto / raw / preagg（三档切换） | ❌ **无查询模式切换** | **缺失** |
| 刷新按钮 | ✅ | ✅ | 一致 |
| 自动刷新 | ✅ 带倒计时 + 暂停/恢复按钮 | ✅ 带倒计时，但无暂停按钮 | 部分缺失 |
| 全屏切换 | ✅ (含 query 参数 `fullscreen=1`) | ✅ (含 query 参数 `fullscreen=1`) | ✅ 一致 |
| 请求详情入口 | ✅ Header 内按钮 | ✅ Header 内按钮 | ✅ 一致 |
| 错误详情入口 | ✅ Header 内按钮 | ✅ Header 内按钮 | ✅ 一致 |
| 设置入口 | ✅ | ✅ | ✅ 一致 |
| 告警规则入口 | ✅ | ✅ | ✅ 一致 |

### 2.2 健康评分 (Health Score)

| 特性 | Sub2API | Sunny Canyon | 差距 |
|------|---------|-------------|------|
| **可视化** | SVG 环形进度条（彩色） | SVG 环形进度条（彩色） | ✅ 一致 |
| **数值显示** | 0-100 整数 + 颜色编码 | 0-100 整数 + 颜色编码 | ✅ 一致 |
| **Hover 诊断** | popover 展示各维度评分 | popover 展示各维度评分 | ✅ 一致 |
| **空闲态** | 无特殊处理 | ✅ "空闲" 标签 + 灰色环 | **sunny-canyon 更优** |
| **颜色阈值** | 可配置 (`OpsMetricThresholds`) | 可配置 | ✅ 一致 |

### 2.3 实时流量 (Realtime Traffic)

| 特性 | Sub2API | Sunny Canyon | 差距 |
|------|---------|-------------|------|
| 展示区域 | Header 右侧独立区域 | Header 右侧独立区域 | ✅ 一致 |
| QPS/TPS | current + peak + avg | current + peak | avg 缺失 |
| 时间窗口切换 | 1min/5min/30min/1h 按钮组 | ✅ 1min/5min/30min/1h 按钮组 | ✅ 一致 |
| 刷新按钮 | ✅ | ✅ | 一致 |
| **WebSocket 实时推送** | ✅ `subscribeQPS()` 自动重连 | ❌ 仅定时轮询 | **重大缺失** |
| 状态显示 | connecting/connected/reconnecting/offline | ❌ | **缺失** |

### 2.4 指标卡片网格 (Metrics Grid)

| 卡片 | Sub2API | Sunny Canyon | 差距 |
|------|---------|-------------|------|
| 请求总量 | ✅ + Tokens 子项 + "钻取"链接 | ✅ + Tokens 子项 + "钻取"按钮 | ✅ 一致 |
| 成功率 (SLA) | ✅ + 进度条 + 颜色编码 | ✅ + 进度条 + 颜色编码 | ✅ 一致 |
| 错误统计 | ✅ 4xx/5xx + excl 429/529 | ✅ 4xx/5xx + excl 429/529 | ✅ 一致 |
| 延迟 P99 | ✅ + P95/P90/P50/Avg/Max 网格 | ✅ + P95/P90/P50/Avg/Max 网格 | ✅ 一致 |
| 首字延迟 P99 (TTFT) | ✅ + P95/P90/P50/Avg/Max 网格 | ✅ + P95/P90/P50/Avg/Max 网格 | ✅ 一致 |
| 上游错误率 | ✅ + excl 429/529 + 限流量 | ✅ + excl 429/529 + 限流量 | ✅ 一致 |

### 2.5 系统指标栏 (System Metrics Bar)

| 指标 | Sub2API | Sunny Canyon | 差距 |
|------|---------|-------------|------|
| CPU 使用率 | ✅ + 颜色编码 | ✅ + 颜色编码 | ✅ 一致 |
| 内存使用 | ✅ MB / 总 MB | ✅ MB / 总 MB | ✅ 一致 |
| DB 连接池 | ✅ active / max + 百分比 | ✅ active / max + 百分比 | ✅ 一致 |
| Redis 连接池 | ✅ total / pool_size + 百分比 | ✅ total / pool_size + 百分比 | ✅ 一致 |
| Goroutine 数 | ✅ | ✅ | ✅ 一致 |
| **Jobs 状态** | ✅ 后台任务健康检测 | ✅ 后台任务健康检测 | ✅ 一致 |
| **并发队列深度** | ✅ `concurrency_queue_depth` | ❌ | **缺失** |
| **账号切换数** | ✅ `account_switch_count` | ❌ (有 switch rate chart) | 形式不同 |
| **DB/Redis 健康状态** | ✅ `db_ok` / `redis_ok` 布尔值 | ❌ | **缺失** |

### 2.6 第一行：并发 + 切换率 (Row 1)

| 特性 | Sub2API | Sunny Canyon | 差距 |
|------|---------|-------------|------|
| **布局** | `lg:grid-cols-4`: 并发(1) + 切换率(1) + 吞吐量(2) | `grid-cols-2`: 并发(1) + 切换率(1)；吞吐量独占下一行 | 布局差异 |
| OpsConcurrencyCard | ✅ provider/group/account 三级 + user 并发 | ✅ provider/group/account 三级 + user 并发 | ✅ 基本一致 |
| OpsSwitchRateTrendChart | ✅ 5h 固定窗口 | ✅ 5h 固定窗口 | ✅ 一致 |
| **账号可用性统计** | ✅ `OpsAccountAvailability` (rate_limit/error/overload) | ✅ `account-availability` 接口 | ✅ 一致 |

### 2.7 第二行：流量趋势 (Throughput Trend)

| 特性 | Sub2API | Sunny Canyon | 差距 |
|------|---------|-------------|------|
| 图表名称 | OpsThroughputTrendChart（2列宽） | OpsTrafficTrendChart（独占一行全宽） | 布局差异 |
| QPS/TPS 双轴折线 | ✅ | ✅ | 一致 |
| 按 Platform 分组 | ✅ `by_platform` 饼图/标签 | ✅ `by_provider` 饼图/标签 | 命名不同 |
| Top Groups | ✅ `top_groups` 排名表 | ✅ `top_groups` 排名表 | ✅ 一致 |
| 点击平台/组下钻 | ✅ | ✅ (placeholder) | 实现完整度 |

### 2.8 第三行：可视化分析 (Visual Analysis, 3 列)

| 图表 | Sub2API | Sunny Canyon | 差距 |
|------|---------|-------------|------|
| **延迟分布** | OpsLatencyChart（柱状图） | OpsLatencyHistogram（柱状图） | ✅ 一致 |
| **错误分布** | OpsErrorDistributionChart（环形图） | OpsErrorDistributionChart（环形图） | ✅ 一致 |
| **错误趋势** | OpsErrorTrendChart（折线图 + 按钮） | OpsErrorTrendChart（折线图 + 按钮） | ✅ 一致 |

### 2.9 空闲状态提示

| 特性 | Sub2API | Sunny Canyon | 差距 |
|------|---------|-------------|------|
| 提示 UI | ❌ 无特殊处理 | ✅ "空闲状态" 提示条 | **sunny-canyon 更优** |

### 2.10 Token 统计卡片 (Token Stats)

| 特性 | Sub2API | Sunny Canyon | 差距 |
|------|---------|-------------|------|
| 名称 | OpsOpenAITokenStatsCard | OpsTokenStatsCard | 命名差异 |
| Feature Flag | ✅ (可隐藏) | ✅ (可隐藏) | ✅ 一致 |
| 时间范围 | 30m/1h/1d/15d/30d | 待确认 | 待核实 |

### 2.11 告警事件卡片 (Alert Events)

| 特性 | Sub2API | Sunny Canyon | 差距 |
|------|---------|-------------|------|
| 列表展示 | ✅ | ✅ | 一致 |
| Feature Flag | ✅ | ✅ | 一致 |
| 标记已解决 | ✅ | ✅ | 一致 |
| 创建静默 | ✅ `createAlertSilence` | ✅ | 一致 |

### 2.12 错误日志表 (Error Log Table)

| 特性 | Sub2API | Sunny Canyon | 差距 |
|------|---------|-------------|------|
| 独立表组件 | OpsErrorLogTable ✅ | OpsErrorLogTable ✅ | 一致 |
| 分页 | ✅ | ✅ | 一致 |
| 全屏隐藏 | ❌ | ✅ (`!isFullscreen`) | sunny-canyon 优化 |

### 2.13 系统日志表 (System Log Table)

| 特性 | Sub2API | Sunny Canyon | 差距 |
|------|---------|-------------|------|
| 独立表组件 | OpsSystemLogTable ✅ | OpsSystemLogTable ✅ | 一致 |
| 全屏隐藏 | ❌ | ✅ | sunny-canyon 优化 |

---

## 三、弹窗/模态框对比

| 弹窗 | Sub2API | Sunny Canyon | 差距 |
|------|---------|-------------|------|
| 设置对话框 | OpsSettingsDialog（BaseDialog） | OpsSettingsDialog（Element Dialog） | ✅ 一致 |
| 告警规则管理 | OpsAlertRulesCard（BaseDialog） | OpsAlertRulesCard（el-dialog） | ✅ 一致 |
| 错误列表钻取 | OpsErrorDetailsModal（request/upstream 分 tab） | OpsErrorDetailsModal | ✅ 一致 |
| 单条错误详情 | OpsErrorDetailModal | OpsErrorDetailModal | ✅ 一致 |
| 请求详情列表 | OpsRequestDetailsModal | OpsRequestDetailsModal | ✅ 一致 |
| 邮件通知配置 | OpsEmailNotificationCard | OpsEmailNotificationCard | ✅ 一致 |
| 运行时设置 | OpsRuntimeSettingsCard | OpsRuntimeSettingsCard | ✅ 一致 |

---

## 四、后端 API 端点对比

### 4.1 Dashboard 核心 API

| Sub2API 端点 | Sunny Canyon 端点 | 状态 |
|-------------|-------------------|------|
| `GET /admin/ops/dashboard/overview` | `GET /ops/dashboard` | ✅ 已有 |
| `GET /admin/ops/dashboard/snapshot-v2` | ❌ | **缺失**（聚合接口） |
| `GET /admin/ops/dashboard/throughput-trend` | `GET /ops/throughput-trend` | ✅ 已有 |
| `GET /admin/ops/dashboard/latency-histogram` | `GET /ops/latency-histogram` | ✅ 已有 |
| `GET /admin/ops/dashboard/error-trend` | `GET /ops/error-trend` | ✅ 已有 |
| `GET /admin/ops/dashboard/error-distribution` | `GET /ops/error-distribution` | ✅ 已有 |
| `GET /admin/ops/dashboard/openai-token-stats` | `GET /ops/token-stats` | ✅ 已有 |
| `GET /admin/ops/traffic-trend` (QPS/TPS 时序) | `GET /ops/traffic-trend` | ✅ 已有 |
| `GET /admin/ops/switch-rate-trend` | `GET /ops/switch-rate-trend` | ✅ 已有 |

### 4.2 并发与可用性 API

| Sub2API 端点 | Sunny Canyon 端点 | 状态 |
|-------------|-------------------|------|
| `GET /admin/ops/concurrency` | `GET /ops/concurrency` | ✅ 已有 |
| `GET /admin/ops/user-concurrency` | `GET /ops/user-concurrency` | ✅ 已有 |
| `GET /admin/ops/account-availability` | `GET /ops/account-availability` | ✅ 已有 |
| `GET /admin/ops/realtime-traffic` | `GET /ops/realtime-traffic` | ✅ 已有 |

### 4.3 WebSocket 实时推送

| 端点 | Sub2API | Sunny Canyon | 现状 |
|------|---------|-------------|------|
| `/admin/ops/ws/qps` | ✅ WebSocket | ❌ | **缺失** |

### 4.4 错误管理 API

| Sub2API 端点 | Sunny Canyon 端点 | 状态 |
|-------------|-------------------|------|
| `GET /admin/ops/request-errors` | `GET /ops/request-errors` | ✅ 已有 |
| `GET /admin/ops/upstream-errors` | `GET /ops/upstream-errors` | ✅ 已有 |
| `GET /admin/ops/request-errors/:id` | `GET /ops/request-errors/:id` | ✅ 已有 |
| `GET /admin/ops/upstream-errors/:id` | `GET /ops/upstream-errors/:id` | ✅ 已有 |
| `PUT /admin/ops/request-errors/:id/resolve` | `PUT /ops/error-logs/:id/resolve` | ✅ 已有 |
| `GET .../request-errors/:id/upstream-errors` | `GET /ops/request-errors/:id/correlation` | ✅ 已有 |
| `GET /admin/ops/requests` | `GET /ops/requests` | ✅ 已有 |

### 4.5 告警 API

| Sub2API 端点 | Sunny Canyon 端点 | 状态 |
|-------------|-------------------|------|
| CRUD `/admin/ops/alert-rules` | CRUD `/ops/alert-rules` | ✅ 已有 |
| GET `/admin/ops/alert-events` | GET `/ops/alert-events` | ✅ 已有 |
| PUT `/admin/ops/alert-events/:id/status` | ✅ | ✅ 已有 |
| POST `/admin/ops/alert-silences` | POST `/ops/alert-silences` | ✅ 已有 |

### 4.6 系统日志 / 设置 API

| Sub2API 端点 | Sunny Canyon 端点 | 状态 |
|-------------|-------------------|------|
| `GET /admin/ops/system-logs` | `GET /ops/system-logs` | ✅ 已有 |
| `POST /admin/ops/system-logs/cleanup` | `POST /ops/system-logs/cleanup` | ✅ 已有 |
| `GET /admin/ops/system-logs/health` | `GET /ops/system-logs/health` | ✅ 已有 |
| `GET/PUT /admin/ops/runtime/logging` | `GET/PUT /ops/runtime/logging` | ✅ 已有 |
| `GET/PUT /admin/ops/settings/metric-thresholds` | `GET/PUT /ops/settings/metric-thresholds` | ✅ 已有 |
| `GET/PUT /admin/ops/settings/dashboard` | `GET/PUT /ops/settings/dashboard` | ✅ 已有 |
| `GET/PUT /admin/ops/settings/advanced` | `GET/PUT /ops/settings/advanced` | ✅ 已有 |
| `GET/PUT /admin/ops/email-notification/config` | `GET/PUT /ops/settings/email-notification` | ✅ 已有 |
| `GET/PUT /admin/ops/runtime/alert` | `GET/PUT /ops/settings/alert-runtime` | ✅ 已有 |

---

## 五、数值统计准确性分析

### 5.1 已有的数据采集链路 ✅

```
Gateway middleware/usage_collector.go
  ├─ Redis 计数器写入: ops:qps:{minute}, ops:tps:{minute}  (10min TTL)
  └─ BatchWriter → PostgreSQL call_logs
       ├─ RollupCron (hourly) → usage_daily
       └─ TraceWriter → call_traces
```

- **QPS/TPS**: Redis 窗口计数 + DB 聚合，数据源可靠
- **延迟百分位**: 从 `call_logs` 的 `duration_ms` 和 `first_token_ms` 计算
- **错误分类**: `status` 字段精确区分 4xx/5xx/429/529
- **Token 用量**: 每请求记录 prompt/completion/cached/reasoning tokens
- **系统指标**: 30s 采样 CPU/内存/连接池/Goroutine

### 5.2 数值统计潜在问题

| 问题 | 影响 | 严重程度 |
|------|------|----------|
| **无 preagg 模式** | 大量历史数据时查询慢 | 中 |
| **Redis 窗口 TTL 仅 10min** | 超过 10min 时间范围只能从 DB 聚合 | 低 |
| **无 snapshot-v2 聚合接口** | 首屏需发起 6 个并行请求，弱网体验差 | 低 |
| **health_score 算法差异** | sub2api 综合 SLA + 内存 + CPU + goroutine；sunny-canyon 需确认实际公式 | 低 |
| **QPS 峰值统计** | 前端 `peakQPS` 从 overview 读取，但 DB 无独立 peak 存储，可能依赖 Redis 窗口最大值 | 低 |

---

## 六、功能缺失清单（需补齐）

### 高优先级

| # | 缺失项 | Sub2API 对应 | 影响 |
|---|--------|-------------|------|
| 1 | **查询模式切换** (auto/raw/preagg) | Header 工具栏下拉 | 用户无法选择 raw 或 preagg 数据源 |
| 2 | **自动刷新暂停/恢复** | Header 工具栏暂停按钮 | 用户只能开关，无法暂停后再恢复 |
| 3 | **WebSocket 实时 QPS 推送** | `subscribeQPS()` + 重连 + 心跳 | 前端只能靠轮询刷新，延迟高 |

### 中优先级

| # | 缺失项 | Sub2API 对应 | 影响 |
|---|--------|-------------|------|
| 4 | 并发队列深度展示 | System Metrics `concurrency_queue_depth` | 系统指标栏缺少一项关键指标 |
| 5 | DB/Redis 健康布尔值 | `db_ok` / `redis_ok` | 无法一眼看到基础设施是否正常 |
| 6 | snapshot-v2 聚合接口 | `/admin/ops/dashboard/snapshot-v2` | 首屏性能优化 |
| 7 | QPS/TPS avg (平均) 展示 | Realtime Traffic `avg` 字段 | 只能看到 current/peak |
| 8 | Snapshot V2 (overview + trend + error_trend 三次合一) | 减少请求数 | 性能优化 |
| 9 | QueryMode 传递到后端 | `?mode=auto\|raw\|preagg` | 后端需支持不同查询模式 |

### 低优先级

| # | 缺失项 | Sub2API 对应 | 影响 |
|---|--------|-------------|------|
| 10 | 全屏浮动工具栏（时间 + 刷新 + 退出） | `ops-fullscreen-bar` | 全屏时只能 ESC 退出 |
| 11 | WS 连接状态指示 | `OpsWSStatus` 图标 | 无法感知实时推送状态 |
| 12 | Stale connection 检测 | `staleTimeoutMs` + `staleTimer` | 连接静默断开时无法感知 |
| 13 | URL query 参数 deep linking | `?tr=1h&fullscreen=1` + platform/group | sunny-canyon 已有 but less complete |

---

## 七、sunny-canyon 独有优势

| 特性 | 说明 |
|------|------|
| **健康检查系统** | Provider/Model/Group 多级定时探测 + 健康时间线 + 自动恢复 |
| **模型状态日志** | 状态变更历史记录可追溯 |
| **组织层级** | studio > dept > user 三级用量看板 |
| **预算管理** | 多维度预算管控 |
| **性能排行** | 按 group/model 的 P50/P95 + success rate 排名 |
| **空闲状态 UI** | 无流量时友好提示 + 灰色健康环 |
| **错误日志独立表** | 从 System Log 中分离出 Error Log 专属视图 |
| **全屏自动隐藏底部表** | 全屏查看时 System Log / Error Log 自动隐藏 |

---

## 八、UI 风格差异总结

| 维度 | Sub2API | Sunny Canyon |
|------|---------|-------------|
| 设计系统 | TailwindCSS utility | Element Plus 组件 |
| 色彩体系 | Tailwind 默认色板 | Element Plus 主题色 |
| 卡片样式 | 圆角 + shadow 轻量风 | 边框 + 内阴影 企业风 |
| 加载骨架屏 | `OpsDashboardSkeleton` | `OpsDashboardSkeleton` ✅ |
| 全屏模式 | class 切换 + 自定义 bg | class 切换 + CSS 变量 |
| 图表 | 推测 Chart.js/Apex | ECharts |
| Tooltip | `HelpTooltip` 自定义 | `el-tooltip` Element 原生 |
| Dialog | `BaseDialog` 自定义 | `el-dialog` Element 原生 |
| 数据格式化 | `formatNumber()` 工具函数 | `formatNumber()` 工具函数 |

---

## 九、实施建议（如需一模一样复刻）

### Phase 1：补齐功能缺失（1-2 天）

1. **Header 工具栏添加"查询模式"下拉** — 纯前端 + 后端 ops_handler 新增 `mode` 参数
2. **自动刷新暂停按钮** — Header 内增加暂停/恢复状态切换
3. **并发队列深度 & DB/Redis 健康值** — System Metrics Bar 新增 2-3 个指标项

### Phase 2：WebSocket 实时推送（2-3 天）

1. 后端新增 `/api/v1/ops/ws/qps` WebSocket 端点
2. 前端实现 `subscribeQPS()` + 自动重连 + 状态管理
3. Header Realtime Traffic 改为 WS 推送驱动

### Phase 3：性能优化（1 天）

1. 后端新增 `snapshot-v2` 聚合接口 (overview + throughput_trend + error_trend)
2. 前端首屏改为单次请求
3. 添加 `mode=preagg` 支持预聚合查询

### Phase 4：微调 & 样式对齐（1-2 天）

1. 布局调整：第一行从 `grid-cols-2` 改为 `grid-cols-4`（并发 1 + 切换率 1 + 流量趋势 2）
2. 全屏浮动工具栏补充时间选择 + 刷新按钮
3. 全屏模式下错误日志/系统日志自动隐藏

---

## 十、结论

**sunny-canyon 的运维看板已经实现了 sub2api 约 90% 的功能**。两者的核心架构（组件拆分、数据模型、API 设计）高度一致，说明 sunny-canyon 在移植时已做了充分的参考。

主要差距集中在：
1. **实时性**：缺少 WebSocket 推送，全靠轮询
2. **性能**：缺少聚合接口，首屏请求数多
3. **细节交互**：查询模式切换、暂停按钮、全屏浮动工具栏等
4. **系统指标完整性**：并发队列深度、基础设施健康布尔值

数值统计方面，两个项目的数据采集链路（Gateway middleware → Redis → BatchWriter → DB → Ops Handler）基本一致，统计准确性不会有本质差异。
