# 健康监控页面（Health Monitor Dashboard）技术设计

## 1. 概述

新增一个独立的"健康监控"页面，以时间线方式展示各模型端点的健康状态，支持回溯查看历史检测结果。

### 1.1 页面效果

- 顶部：标题 + 回溯时间选择器（6h / 12h / 24h / 7d）+ 刷新按钮
- 主体：按模型端点分组的卡片列表，每张卡片包含：
  - 模型端点名称（如 "Claude Messages"）
  - 健康率百分比（带颜色标签：绿 ≥95%、橙 ≥80%、红 <80%）
  - 提供商数量 / 密钥数量
  - 时间线色块条（每个色块对应一次检测结果）

### 1.2 与现有功能的关系

当前健康检查功能嵌入在"渠道管理"页面，以单个 Provider 为粒度展示当前状态。新页面的区别：
- **维度不同**：按模型端点（Model Endpoint）聚合，而非单个 Provider
- **时间维度**：展示历史时间线，而非仅当前状态
- **定位不同**：全局概览型仪表盘 vs 管理操作页

---

## 2. 后端设计

### 2.1 新增 API

#### `GET /api/v1/health-check/timeline`

**查询参数：**

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| lookback | string | "6h" | 回溯时间窗口，可选：6h, 12h, 24h, 7d |
| target_type | string | "" | 可选过滤：provider / model，为空则返回全部 |

**响应结构：**

```json
{
  "data": {
    "lookback": "6h",
    "groups": [
      {
        "target_name": "Claude Messages",
        "target_type": "model",
        "target_id": "uuid-xxx",
        "provider_count": 2,
        "key_count": 4,
        "health_percent": 98,
        "total_checks": 72,
        "success_checks": 71,
        "timeline": [
          {
            "checked_at": "2026-05-22T10:04:16Z",
            "success": true,
            "response_time_ms": 156
          },
          {
            "checked_at": "2026-05-22T10:09:16Z",
            "success": false,
            "response_time_ms": 0,
            "error_message": "timeout"
          }
        ]
      }
    ]
  }
}
```

**说明：**
- `groups` 按 `target_name` 分组（同一模型端点可能对应多个 provider，合并展示）
- `timeline` 按时间升序排列，每条记录对应一次实际检测
- `health_percent` = success_checks / total_checks * 100（取整）
- `provider_count` / `key_count` 需要联表查询 providers 和 credentials

### 2.2 实现位置

```
internal/controlplane/
├── handler/health_check_handler.go   // 新增 GetTimeline 方法
├── service/health_check_service.go   // 新增 GetTimeline 业务逻辑
└── repository/health_check_repo.go   // 新增 GetLogsByTimeRange 查询
```

### 2.3 Repository 层新增方法

```go
// GetLogsByTimeRange 获取指定时间范围内的检测记录
func (r *HealthCheckRepo) GetLogsByTimeRange(since time.Time, targetType string) ([]model.HealthCheckLog, error) {
    query := r.db.Where("checked_at >= ?", since).Order("checked_at ASC")
    if targetType != "" {
        query = query.Where("target_type = ?", targetType)
    }
    var logs []model.HealthCheckLog
    err := query.Find(&logs).Error
    return logs, err
}
```

### 2.4 Service 层聚合逻辑

```go
type TimelineGroup struct {
    TargetName    string              `json:"target_name"`
    TargetType    string              `json:"target_type"`
    TargetID      uuid.UUID           `json:"target_id"`
    ProviderCount int                 `json:"provider_count"`
    KeyCount      int                 `json:"key_count"`
    HealthPercent int                 `json:"health_percent"`
    TotalChecks   int                 `json:"total_checks"`
    SuccessChecks int                 `json:"success_checks"`
    Timeline      []TimelineEntry     `json:"timeline"`
}

type TimelineEntry struct {
    CheckedAt      time.Time `json:"checked_at"`
    Success        bool      `json:"success"`
    ResponseTimeMs int       `json:"response_time_ms"`
    ErrorMessage   string    `json:"error_message,omitempty"`
}

type TimelineResult struct {
    Lookback string          `json:"lookback"`
    Groups   []TimelineGroup `json:"groups"`
}
```

聚合逻辑：
1. 查询时间范围内所有 logs
2. 按 `target_name` 分组（因为同名模型可能有多个 provider 提供）
3. 对每组计算 health_percent
4. 联查 providers 表获取 provider_count 和关联的 credentials 数量

### 2.5 性能考虑

- 现有索引 `idx_health_check_logs_checked_at` 和 `idx_health_check_logs_target` 已能支撑时间范围查询
- 7 天回溯、5 分钟间隔约产生 ~2000 条/端点记录，在可接受范围内
- 若后续数据量增大，可考虑增加预聚合表（当前阶段不需要）

---

## 3. 前端设计

### 3.1 新增文件

```
web/src/
├── views/admin/HealthMonitorPage.vue    // 主页面
├── components/admin/HealthTimeline.vue  // 时间线色块组件
├── services/control-plane.ts            // 新增 getHealthTimeline API 方法
└── types/health.ts                      // 新增类型定义
```

### 3.2 类型定义 (`types/health.ts`)

```typescript
export interface TimelineEntry {
  checked_at: string
  success: boolean
  response_time_ms: number
  error_message?: string
}

export interface TimelineGroup {
  target_name: string
  target_type: string
  target_id: string
  provider_count: number
  key_count: number
  health_percent: number
  total_checks: number
  success_checks: number
  timeline: TimelineEntry[]
}

export interface TimelineResult {
  lookback: string
  groups: TimelineGroup[]
}
```

### 3.3 API 方法

```typescript
// services/control-plane.ts 新增
export async function getHealthTimeline(lookback: string = '6h'): Promise<TimelineResult> {
  const response = await http.get<ApiResponse<TimelineResult>>(
    '/api/v1/health-check/timeline',
    { params: { lookback } }
  )
  return response.data.data
}
```

### 3.4 页面组件设计 (`HealthMonitorPage.vue`)

**核心功能：**
- 回溯时间切换（6h / 12h / 24h / 7d）
- 自动刷新（可选，每 60s）
- 手动刷新按钮
- 按健康率排序（最差的在最上面，方便关注问题）

**布局结构：**
```
┌─────────────────────────────────────────────────────┐
│  健康监控                    回溯时间：[6小时 ▾] [↻] │
├─────────────────────────────────────────────────────┤
│  ┌─────────────────────────────────────────────────┐│
│  │ Claude Messages  [98%]                          ││
│  │ 2个提供商 / 4个密钥                              ││
│  │ ████████████████████████████████████████████     ││
│  │ 10:04                                    16:04  ││
│  └─────────────────────────────────────────────────┘│
│  ┌─────────────────────────────────────────────────┐│
│  │ OpenAI Chat  [97%]                              ││
│  │ ...                                             ││
│  └─────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────┘
```

### 3.5 时间线组件设计 (`HealthTimeline.vue`)

**实现方式：** 纯 CSS flex 布局，不引入图表库。

```vue
<template>
  <div class="health-timeline">
    <div
      v-for="(entry, i) in timeline"
      :key="i"
      class="timeline-block"
      :class="getBlockClass(entry)"
      :title="getTooltip(entry)"
    />
  </div>
</template>
```

**颜色映射：**
- 成功 + 响应时间 < 1000ms → 深绿 `#22c55e`
- 成功 + 响应时间 ≥ 1000ms → 浅绿/黄绿 `#84cc16`
- 失败 → 红色 `#ef4444`
- 无数据 → 深灰 `#4b5563`

**色块尺寸：**
- 宽度：4-6px，根据容器宽度和数据点数量自适应
- 高度：32px
- 间距：1px
- 圆角：2px

### 3.6 路由注册

```typescript
// router/index.ts 新增
{
  path: '/admin/health-monitor',
  name: 'HealthMonitor',
  component: () => import('@/views/admin/HealthMonitorPage.vue'),
  meta: { title: '健康监控' }
}
```

### 3.7 侧边栏菜单

```vue
<!-- App.vue 新增菜单项（放在"统一接入"下方） -->
<el-menu-item index="/admin/health-monitor">
  <el-icon><Monitor /></el-icon>
  <span>健康监控</span>
</el-menu-item>
```

---

## 4. 数据流

```
┌─────────────┐     定时触发      ┌─────────────────┐
│ HealthCheck │ ───────────────→ │ health_check_logs│
│  Scheduler  │   写入检测结果    │    (PostgreSQL)  │
└─────────────┘                  └────────┬────────┘
                                          │
                                          │ 查询（按时间范围）
                                          ▼
┌─────────────┐   GET /timeline  ┌─────────────────┐
│   前端页面   │ ←─────────────── │  Timeline API   │
│ (Vue 组件)  │   返回聚合数据    │   (Go Handler)  │
└─────────────┘                  └─────────────────┘
```

---

## 5. 实现步骤

### Phase 1：后端 API（预计工作量：2-3h）
1. `health_check_repo.go` 新增 `GetLogsByTimeRange` 方法
2. `health_check_service.go` 新增 `GetTimeline` 聚合逻辑
3. `health_check_handler.go` 新增 `GetTimeline` handler
4. 路由注册新端点

### Phase 2：前端页面（预计工作量：3-4h）
1. 新增 `types/health.ts` 类型定义
2. `control-plane.ts` 新增 API 方法
3. 实现 `HealthTimeline.vue` 时间线组件
4. 实现 `HealthMonitorPage.vue` 主页面
5. 注册路由 + 侧边栏菜单

### Phase 3：优化（可选）
- 添加自动刷新
- 点击色块显示详情弹窗
- 响应时间趋势图（hover 时显示）
- 导出健康报告

---

## 6. 开放问题

| # | 问题 | 建议 |
|---|------|------|
| 1 | 时间线粒度：每次检测一个色块 vs 固定间隔聚合？ | 建议"每次检测一个色块"，更直观；数据量大时再考虑聚合 |
| 2 | 分组维度：按模型端点名称还是按 target_id？ | 建议按 target_name 分组，因为同一模型可能由多个 provider 提供 |
| 3 | provider_count / key_count 如何获取？ | 联查 providers 表 + credentials，按模型名称关联 |
| 4 | 是否需要支持点击跳转到具体 Provider？ | Phase 1 不做，后续可加 |
| 5 | 页面是否需要权限控制？ | 跟随现有路由守卫逻辑，当前开发阶段直接放行 |
