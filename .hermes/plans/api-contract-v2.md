# Dashboard 后端 API 接口契约 v2（Phase A + Phase B）

> 分支：`feat/dashboard-backend-upgrade`
> 基线：`main` @ `890222d`
> 日期：2026-06-28

---

## 1. 接口变更总览

| 接口 | 方法 | 路径 | 变更类型 |
|------|------|------|----------|
| 用户排行 | GET | `/api/v1/usage/dashboard/users` | ⚠️ **响应格式升级** |
| 模型排行 | GET | `/api/v1/usage/dashboard/models` | ⚠️ **响应格式升级** |
| 用户详情 | GET | `/api/v1/usage/dashboard/user-detail` | 🆕 **新增** |
| 模型详情 | GET | `/api/v1/usage/dashboard/model-detail` | 🆕 **新增** |
| 主看板 | GET | `/api/v1/usage/dashboard` | ✅ 不变（已有 model_name 筛选） |
| 趋势 | GET | `/api/v1/usage/dashboard/trend` | ✅ 不变 |
| Top 模型 | GET | `/api/v1/usage/dashboard/user-top-models` | ✅ 不变 |

---

## 2. ⚠️ GET `/api/v1/usage/dashboard/users`（增强版）

### 请求参数（Query）

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `from` | string | 否 | 起始日期 `YYYY-MM-DD`，默认 30 天前 |
| `to` | string | 否 | 结束日期 `YYYY-MM-DD`，默认今天 |
| `studio_id` | string | 否 | 工作室筛选 |
| `dept_id` | string | 否 | 部门筛选 |
| `model_name` | string | 否 | 🆕 **模型名称模糊筛选**（ILIKE） |
| `page` | int | 否 | 页码，默认 1 |
| `page_size` | int | 否 | 每页条数，默认 20，最大 100 |

### 响应

```json
{
  "summary": {
    "total_users": 156,
    "total_cost_usd": 4523.67,
    "total_calls": 89234,
    "active_days": 28,
    "avg_cost_per_call": 0.0507
  },
  "rows": [
    {
      "user_id": "abc-123",
      "user_name": "张三",
      "dept_name": "AI研发部",
      "total_tokens": 12500000,
      "prompt_tokens": 8000000,
      "completion_tokens": 4500000,
      "est_cost_usd": 325.50,
      "call_count": 1234,
      "error_count": 15,
      "model_count": 5,
      "cost_ratio": 7.20,
      "cost_change_pct": 12.50,
      "cost_per_1k_tokens": 0.0260,
      "error_rate": 1.22,
      "top_models": [
        {
          "model": "gpt-4o",
          "est_cost_usd": 200.30,
          "call_count": 800,
          "total_tokens": 8000000
        },
        {
          "model": "claude-sonnet-4-20250514",
          "est_cost_usd": 100.10,
          "call_count": 300,
          "total_tokens": 3500000
        },
        {
          "model": "deepseek-v3",
          "est_cost_usd": 25.10,
          "call_count": 134,
          "total_tokens": 1000000
        }
      ],
      "vs_avg": {
        "cost_ratio_vs_avg": 6.56,
        "call_count_vs_avg": 2.15,
        "error_rate_vs_avg": -0.38
      }
    }
  ],
  "page": 1,
  "page_size": 20,
  "total_count": 156
}
```

### 🆕 新增字段说明

| 字段 | 类型 | 说明 |
|------|------|------|
| `summary.total_users` | int | 时间段内活跃用户总数 |
| `summary.total_cost_usd` | float | 总成本（美元） |
| `summary.total_calls` | int | 总调用次数 |
| `summary.active_days` | int | 有数据的天数 |
| `summary.avg_cost_per_call` | float | 平均每次调用成本 |
| `rows[].cost_ratio` | float | 该用户成本占总成本百分比 |
| `rows[].cost_change_pct` | float | 环比变化百分比（正=增长，负=下降） |
| `rows[].cost_per_1k_tokens` | float | 每千 token 成本 |
| `rows[].error_rate` | float | 错误率百分比 |
| `rows[].top_models` | array | 该用户 Top 3 模型（按成本降序） |
| `rows[].top_models[].model` | string | 模型名 |
| `rows[].top_models[].est_cost_usd` | float | 该模型成本 |
| `rows[].top_models[].call_count` | int | 该模型调用次数 |
| `rows[].top_models[].total_tokens` | int | 该模型 token 总量 |
| `rows[].vs_avg.cost_ratio_vs_avg` | float | 成本占比与均值的差（正=高于平均） |
| `rows[].vs_avg.call_count_vs_avg` | float | 调用量 / 平均调用量（倍数） |
| `rows[].vs_avg.error_rate_vs_avg` | float | 错误率与均值的差（百分点） |

### 🗑️ 已移除字段

无（旧版所有字段保留，仅新增）

---

## 3. ⚠️ GET `/api/v1/usage/dashboard/models`（增强版）

### 请求参数（Query）

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `start` | string | 否 | 起始日期 `YYYY-MM-DD`，默认 30 天前 |
| `end` | string | 否 | 结束日期 `YYYY-MM-DD`，默认今天 |
| `limit` | int | 否 | 返回条数，默认 30，最大 100 |
| `user_id` | string | 否 | 用户筛选 |
| `status` | string | 否 | 状态筛选：`success` / `error` |

### 响应

```json
{
  "rows": [
    {
      "model": "gpt-4o",
      "calls": 45000,
      "total_tokens": 280000000,
      "cost": 2100.50,
      "avg_duration_ms": 3200,
      "p90_duration_ms": 8500,
      "error_rate": 1.35,
      "avg_first_token_ms": 850,
      "is_long_tail": false
    },
    {
      "model": "deepseek-r1-0528",
      "calls": 120,
      "total_tokens": 500000,
      "cost": 2.30,
      "avg_duration_ms": 15000,
      "p90_duration_ms": 32000,
      "error_rate": 5.00,
      "avg_first_token_ms": 4200,
      "is_long_tail": true
    }
  ]
}
```

### 🆕 新增字段说明

| 字段 | 类型 | 说明 |
|------|------|------|
| `rows[].avg_duration_ms` | int | 平均响应时间（毫秒） |
| `rows[].p90_duration_ms` | int | P90 响应时间（毫秒） |
| `rows[].error_rate` | float | 错误率百分比 |
| `rows[].avg_first_token_ms` | int | 平均首 token 延迟（毫秒） |
| `rows[].is_long_tail` | bool | 是否长尾模型（调用量 ≤ P10 阈值） |

### 🗑️ 已移除字段

无

---

## 4. 🆕 GET `/api/v1/usage/dashboard/user-detail`

### 请求参数（Query）

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `user_id` | string | **是** | 用户 ID |
| `from` | string | 否 | 起始日期 `YYYY-MM-DD`，默认 30 天前 |
| `to` | string | 否 | 结束日期 `YYYY-MM-DD`，默认今天 |

### 响应

```json
{
  "summary": {
    "total_calls": 1234,
    "success_count": 1219,
    "total_tokens": 12500000,
    "est_cost_usd": 325.50,
    "model_count": 5,
    "avg_duration_ms": 2800.5,
    "error_rate": 1.22
  },
  "daily_trend": [
    {
      "date": "2026-06-01",
      "calls": 45,
      "cost": 12.30,
      "tokens": 500000
    },
    {
      "date": "2026-06-02",
      "calls": 52,
      "cost": 14.80,
      "tokens": 580000
    }
  ],
  "model_distribution": [
    {
      "model": "gpt-4o",
      "calls": 800,
      "cost": 200.30,
      "tokens": 8000000
    },
    {
      "model": "claude-sonnet-4-20250514",
      "calls": 300,
      "cost": 100.10,
      "tokens": 3500000
    }
  ],
  "hourly_distribution": [
    { "hour": 0, "calls": 5 },
    { "hour": 1, "calls": 2 },
    { "hour": 9, "calls": 85 },
    { "hour": 10, "calls": 120 },
    { "hour": 14, "calls": 95 }
  ]
}
```

### 字段说明

| 字段路径 | 类型 | 说明 |
|----------|------|------|
| `summary.total_calls` | int | 总调用次数 |
| `summary.success_count` | int | 成功次数（status=200） |
| `summary.total_tokens` | int | 总 token |
| `summary.est_cost_usd` | float | 估算成本 |
| `summary.model_count` | int | 使用模型数 |
| `summary.avg_duration_ms` | float | 平均响应时间 |
| `summary.error_rate` | float | 错误率 % |
| `daily_trend[]` | array | 每日趋势（按日期升序） |
| `daily_trend[].date` | string | 日期 |
| `daily_trend[].calls` | int | 当日调用量 |
| `daily_trend[].cost` | float | 当日成本 |
| `daily_trend[].tokens` | int | 当日 token |
| `model_distribution[]` | array | 模型分布（按成本降序） |
| `hourly_distribution[]` | array | 24 小时分布（按小时升序，仅包含有数据的小时） |
| `hourly_distribution[].hour` | int | 小时（0-23） |
| `hourly_distribution[].calls` | int | 该小时调用量 |

---

## 5. 🆕 GET `/api/v1/usage/dashboard/model-detail`

### 请求参数（Query）

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `model` | string | **是** | 模型名称（精确匹配） |
| `from` | string | 否 | 起始日期 `YYYY-MM-DD`，默认 30 天前 |
| `to` | string | 否 | 结束日期 `YYYY-MM-DD`，默认今天 |

### 响应

```json
{
  "summary": {
    "total_calls": 45000,
    "success_count": 44392,
    "total_tokens": 280000000,
    "est_cost_usd": 2100.50,
    "user_count": 89,
    "dept_count": 12,
    "avg_duration_ms": 3200.0,
    "p90_duration_ms": 8500.0,
    "avg_first_token_ms": 850.0,
    "error_rate": 1.35
  },
  "daily_trend": [
    {
      "date": "2026-06-01",
      "calls": 1500,
      "cost": 68.20,
      "tokens": 9200000
    }
  ],
  "user_distribution": [
    {
      "user_id": "abc-123",
      "user_name": "张三",
      "dept_name": "AI研发部",
      "calls": 800,
      "cost": 200.30,
      "tokens": 8000000
    },
    {
      "user_id": "def-456",
      "user_name": "李四",
      "dept_name": "产品部",
      "calls": 650,
      "cost": 180.50,
      "tokens": 7200000
    }
  ]
}
```

### 字段说明

| 字段路径 | 类型 | 说明 |
|----------|------|------|
| `summary.total_calls` | int | 总调用次数 |
| `summary.success_count` | int | 成功次数 |
| `summary.total_tokens` | int | 总 token |
| `summary.est_cost_usd` | float | 估算成本 |
| `summary.user_count` | int | 使用该模型的用户数 |
| `summary.dept_count` | int | 使用该模型的部门数 |
| `summary.avg_duration_ms` | float | 平均响应时间 |
| `summary.p90_duration_ms` | float | P90 响应时间 |
| `summary.avg_first_token_ms` | float | 平均首 token 延迟 |
| `summary.error_rate` | float | 错误率 % |
| `daily_trend[]` | array | 每日趋势（按日期升序） |
| `user_distribution[]` | array | 用户分布 Top 20（按成本降序） |
| `user_distribution[].user_id` | string | 用户 ID |
| `user_distribution[].user_name` | string | 用户名 |
| `user_distribution[].dept_name` | string | 部门名 |
| `user_distribution[].calls` | int | 该用户对此模型的调用次数 |
| `user_distribution[].cost` | float | 该用户在此模型上的成本 |
| `user_distribution[].tokens` | int | 该用户在此模型上的 token |

---

## 6. 日期参数约定

- `from` / `to`：闭区间 `[from, to]`，后端自动扩展 `toExcl = to + 1 day` 做 `<` 比较
- 格式：`YYYY-MM-DD`
- 默认值：`from` = 30 天前，`to` = 今天
- 环比计算：上期 = `[from - duration, from)`，其中 `duration = to - from + 1 day`

## 7. 权限

所有接口统一需要 `PermUsageViewScoped` 权限，由路由层 Guard 中间件控制。
数据可见范围由 Actor 的 `StudioIDs` / `DeptIDs` 自动过滤（已内置于查询）。
