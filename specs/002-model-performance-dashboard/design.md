# 模型性能看板规格设计 · Model Performance Dashboard · 002

> 状态：草案 draft · 待确认
> 作者：Sisyphus (根据 2026-06-16 对话整理)
> 关联：已上线的对齐修复 `feat/health-timeline-alignment-fix`（已合并到 test）

---

## 1. 背景与动机

现有运维控制台已有两个健康监控页面：

| 页面 | 路径 | 数据源 | 回答的问题 |
|------|------|--------|-----------|
| 模型健康监控 `HealthMonitorPage` | `/admin/health-monitor` | `health_check_logs`（定时探针） | 模型是否在线 |
| 模型组健康监控 `GroupHealthMonitorPage` | `/admin/group-health-monitor` | `health_check_logs`（定时探针） | 模型组是否在线 |

**问题**：探针全绿 ≠ 真的好用。上游限流、context 过长、缓存命中率低、429 风暴等情况，探针探测不到。**模型组的"可用性"有了，但"性能"维度缺失**。

而 `call_logs` 表已经在真实流量层面记录了极丰富的性能字段（`duration_ms`、`first_token_ms`、`stage_*_ms`、`failover_used` 等），但从未被利用到可视化上。

**本需求的核心目标**：把真实请求性能数据可视化到运维页面，让用户回答 **"现在好不好用"**。

---

## 2. 用户视角区分（已确认）

系统里有一个关键抽象：

```
用户视角看到的            后端实际跑的
─────────────────         ──────────────────────────
claude-sonnet-4-6    →   [deepseek-v4-pro]  (替换组)
middle-level         →   [glm-5.1, deepseek-v4-pro, qwen3.6-plus] (轮询)
low-level            →   [GLM-5.1-FP8, deepseek-v4-flash]
deepseek-v4-flash    →   [deepseek-v4-flash]
```

**用户只认 group name**，不关心背后是 deepseek 还是 glm。所以看板需要两个 Tab：

| Tab | 视角 | 数据分组维度 | 主要用户 |
|-----|------|--------------|----------|
| **Tab 1：模型组视角** | 对外暴露的"模型" | `group_name`（默认展开） | 普通用户、产品、运维 |
| **Tab 2：模型视角** | 后端真实模型 + 渠道 | `model` + `provider` | 运维、SRE |

**模型组视角的卡片**需要支持折叠展开，展开后显示组内各模型流量占比与性能差异：

```
┌────────────────────────────────────────────┐
│  middle-level · 轮询 · 3 模型              │
│  1h · 342 请求 · 成功率 99.8% · failover 0 │
│                                            │
│  总延迟    P50 1.8s   P95 4.2s             │
│  ↑ 当前 P50 处于 24h 的 P60 水平（正常）     │
│  首 Token  P50 420ms  P95 980ms            │
│                                            │
│  ▸ 展开组内模型                             │
│    ├── glm-5.1         52% 流量 P50=1.6s   │
│    ├── deepseek-v4-pro 31% 流量 P50=2.1s   │
│    └── qwen3.6-plus    17% 流量 P50=1.9s   │
└────────────────────────────────────────────┘
```

---

## 3. 设计哲学（已确认）

**不定阈值，让数据说话。**

> 不同模型的速度本身就不一样（claude 与 deepseek-v4-flash 不可比），硬定 "P95 超过 X 秒就算不健康" 没意思。**让数据自己告诉"当前值在历史基线里的位置"。**

### 百分位对比指标

每个性能指标同时输出两个值：

1. **当前窗口值**（1h/6h 的 P50、P95、TTFT 等）
2. **"当前处于 24h 的 Px 水平"**

举例：

| 描述 | 解读 |
|------|------|
| "当前 P50 = 1.8s，处于 24h 的 **P60** 水平" | 比平时稍慢，正常波动 |
| "当前 P50 = 4.5s，处于 24h 的 **P92** 水平" | 明显偏慢，值得排查 |
| "当前 P50 = 0.8s，处于 24h 的 **P15** 水平" | 比平时更快 |

### 健康度显示（不用红绿灯）

不在 UI 上画红黄绿状态灯。直接显示原始数值 + 百分位标注，**让运维自己判断**。理由：用户信任自己的经验比信任我们定义的阈值更合理。

---

## 4. 时间窗口（已确认）

| 窗口 | 用途 | 是否可做基线 |
|------|------|-----------|
| **1h**（默认） | 看"现在怎么样" | 否 |
| **6h** | 看"今天趋势" | 否 |
| **24h** | 看"整体基线" + 作为 1h/6h 的百分位参照 | ✅ 是 |

**默认展示 1h**。用户打开看板就是想知道"现在好不好用"。切换 6h/24h 是为看趋势和对比基线。

---

## 5. 数据源与字段

### 主表：`call_logs`

每张卡片要聚合以下字段：

| 字段 | 语义 |
|------|------|
| `duration_ms` | 总请求耗时（P50 / P95 / P99） |
| `first_token_ms` | 首 Token 延迟 TTFT（P50 / P95） |
| `stage_auth_ms` / `stage_route_ms` / `stage_upstream_ms` / `stage_ttfb_ms` | 阶段耗时瀑布（可折叠显示瓶颈） |
| `status`（HTTP 状态码） | 用于算成功率，区分 4xx/5xx |
| `upstream_status` | 区分网关错误 vs 上游错误 |
| `failover_used` | failover 触发次数 |
| `routed_via_group` | 区分"经过组路由"与"直连 model" |
| `group_name` | 模型组归属（分组维度） |
| `model` / `routed_model` / `provider` | 模型视角的分组维度 |

### 不用的表

- `health_check_logs`：属于"可用性"，不在本需求范围（已在现有监控页面）
- `usage_daily`：用于计费视图，不在本需求范围

---

## 6. 性能卡片字段清单

每张卡片（无论组视图还是模型视图）应显示：

### 基础信息（卡片头部）

- 名称（`group_name` 或 `model + provider`）
- 路由策略（轮询/优先级/加权）——仅组视图
- 模型数量（组视图）—— 仅组视图
- 时间窗口标签（1h / 6h / 24h）

### 核心指标（卡片正文）

| 指标 | 计算方式 |
|------|--------|
| 请求数 | `COUNT(*)` |
| 成功率 | `SUM(status BETWEEN 200 AND 299) / COUNT(*)` |
| failover 次数 | `SUM(failover_used = true)` |
| 总延迟 P50 / P95 | `percentile_cont(0.5) / (0.95)` 于 `duration_ms` |
| 首 Token P50 / P95 | `percentile_cont(0.5) / (0.95)` 于 `first_token_ms` |
| **"当前处于 24h Pxx"** | 窗口 P50 在 24h 各小时 P50 分布中的百分位 |

### 可选展开明细（组内 / 模型维度）

- 组内各模型流量占比（`COUNT(model) / COUNT(*)` 按 `group_name` 过滤）
- 组内各模型的 P50 / P95 / 成功率
- 错误码分布（可选）：429、5xx 各多少

---

## 7. API 设计

### GET `/api/v1/health/performance`

**参数**（Query）：

| 参数 | 类型 | 默认 | 说明 |
|------|------|------|------|
| `window` | `1h / 6h / 24h` | `1h` | 统计窗口 |
| `view` | `group / model` | `group` | 视角 |
| `search` | string | 空 | 名称过滤（模糊） |
| `limit` | int | 50 | 分页 |
| `offset` | int | 0 | 分页 |

**响应**（简化结构）：

```json
{
  "window": "1h",
  "generated_at": "2026-06-16T14:30:00Z",
  "items": [
    {
      "name": "middle-level",
      "strategy": "round_robin",
      "model_count": 3,
      "req_count": 342,
      "success_rate": 0.998,
      "p50_ms": 1800,
      "p95_ms": 4200,
      "ttft_p50_ms": 420,
      "ttft_p95_ms": 980,
      "failover_count": 0,
      "pct_in_24h_p50": 60,
      "err_429": 0,
      "err_5xx": 1,
      "members": [
        {
          "model_name": "glm-5.1",
          "provider": "zhipu",
          "traffic_pct": 0.52,
          "req_count": 178,
          "p50_ms": 1600,
          "p95_ms": 3800,
          "success_rate": 1.0
        }
      ]
    }
  ],
  "total": 15
}
```

### WebSocket `wss://<host>/api/v1/health/performance/live`

- 每 **10s** 推送一次 `{ type: "perf_update", window, items }`
- client 连接时通过 query string 指定 `?window=1h&view=group`
- 支持心跳 ping/pong（30s 一次）
- 重连：客户端使用指数退避（初始 1s，最大 30s）

> 备选：若 WebSocket 实现复杂，第一版可先用 **HTTP 短轮询（10s）**，后续切 WS。

---

## 8. 页面结构（已确认）

**位置**：运维控制台，**新增独立页面** `/admin/performance`（不替换现有 HealthMonitor）。

> 用户选择方案 B：独立页面，与可用性监控（HealthMonitor）通过顶部导航并列。**不放在同个 Tab 页签下分页**（因数据源、刷新策略、复杂度差异较大，独立页面更清晰）。

**页面布局**：

```
┌────────────────────────────────────────────────────────────────┐
│  模型性能看板                                                  │
├────────────────────────────────────────────────────────────────┤
│  [● 模型组视角] [模型视角]         [1h] [6h] [24h]   🔍搜索  ↻ │
├────────────────────────────────────────────────────────────────┤
│                                                                │
│  ┌─────────────────────────────────────────────┐              │
│  │  claude-sonnet-4-6 · 轮询 · 1 模型          │              │
│  │  1h · 120 请求 · 成功率 100% · failover 0   │              │
│  │  总延迟    P50 2.1s  P95 5.3s               │              │
│  │  ↑ 当前 P50 处于 24h 的 P72（偏慢）          │              │
│  │  首 Token  P50 380ms P95 890ms              │              │
│  │  ▸ 展开组内模型                              │              │
│  └─────────────────────────────────────────────┘              │
│                                                                │
│  ┌─────────────────────────────────────────────┐              │
│  │  middle-level · 轮询 · 3 模型               │              │
│  │  ...                                        │              │
│  └─────────────────────────────────────────────┘              │
│                                                                │
│  ┄┄┄ 共 12 个模型组 ┄┄┄                                        │
└────────────────────────────────────────────────────────────────┘
```

---

## 9. 与既有页面的关系

| 页面 | 数据源 | 刷新 | 回答什么 |
|------|--------|------|---------|
| `/admin/health-monitor`（已有） | `health_check_logs` 探针 | 60s | **模型在线吗** |
| `/admin/group-health-monitor`（已有） | `health_check_logs` 探针 | 60s | **模型组在线吗** |
| **`/admin/performance`**（新增） | `call_logs` 真实请求 | 10s WS | **模型好不好用** |

三者**互相补充，不替代**。可以在 `/admin/performance` 卡片顶部放一个极简的"在线/离线"状态点，从 `/admin/health-monitor` 数据拿，便于交叉参照。

---

## 10. MVP 范围（推荐）

### Step 1 · 基础版（3-5 天，建议先做）

- [ ] 后端 `GET /api/v1/health/performance`（HTTP 接口，60s 短轮询也可以）
- [ ] 后端 SQL 聚合（`call_logs` + 百分位计算）
- [ ] 前端 `/admin/performance` 页面：卡片布局 + 两个 Tab
- [ ] 时间窗口切换（1h / 6h / 24h）
- [ ] 组内模型折叠明细
- [ ] 名称搜索 + 请求数倒序

### Step 2 · 实时版（2-3 天）

- [ ] WebSocket 推送（10s 一次）
- [ ] "处于 24h 的 Pxx 水平" 百分位指标
- [ ] 错误码分布（429 / 5xx）
- [ ] 自动重连

### Step 3 · 深度交互（可选迭代）

- [ ] 点击卡片 → 抽屉详情（24h 趋势折线图）
- [ ] 延迟分布直方图
- [ ] 慢请求日志列表（点击看 `call_traces`）
- [ ] 多组对比视图

---

## 11. 验收标准（MVP）

- [ ] 运维用户打开 `/admin/performance`，1 秒内能看到 1h 内请求量前 15 的模型组/模型
- [ ] 每个卡片能清楚看到 P50 / P95 / TTFT / 成功率 / failover
- [ ] 切换 1h/6h/24h 时数值实时刷新（< 3s）
- [ ] 展开组内模型能看到流量占比与性能差异
- [ ] 模型组的卡片数量与 `/admin/group-health-monitor` 数量基本一致（仅计算 `routed_via_group=true` 的组）
- [ ] 不写死阈值。所有"正常/偏慢/异常"由用户根据百分位自行判断

---

## 12. 风险与开放问题

### 风险

1. **冷启动**：刚上线时没有 24h 基线，百分位指标暂时不显示。
2. **数据量**：`call_logs` 增长快。`percentile_cont` 对大表查询压力大。需在 `ts` 索引上加 window 过滤（已有 `(ts)` 索引）。
3. **group_name 缺失**：直连调用（`routed_via_group=false`）的 `group_name` 为空，需在模型视图聚合时用 `model` 字段。
4. **Claude 替换组**：claude-sonnet-4-6 等替换组 100% 流量打到 deepseek-v4-pro，从模型视角会看到一个 deepseek-v4-pro 被 N 个组共用——需在卡片上标注"被 N 个组使用"。

### 开放问题（待与产品确认）

1. **`/admin/performance` 是否需要权限控制**？目前 `/admin/health-monitor` 仅 admin 可见，`/admin/group-health-monitor` 全部登录用户可见。本页面建议：**全部登录用户可见** —— 这是用户视角数据，对 API 消费方也有参考价值。
2. **是否包含无流量的模型组**？默认只显示过去 1h 有请求的。是否要"显示全部"开关？
3. **是否需要"对比两个模型组"**？可以作为 v2 需求。
4. **WebSocket 第一版就必须上吗**？10s 短轮询实现更简单。如果产品接受轮询，建议 Step 1 用 HTTP 轮询，Step 2 切 WS。

---

## 13. 不在本需求内的事（明确排除）

- ❌ 替代 `HealthMonitorPage` / `GroupHealthMonitorPage`（可用性探针监控保留）
- ❌ 计费统计/成本视图（`usage_daily` 是另一条线）
- ❌ 告警推送（邮件/Slack/飞书通知）——属于告警系统
- ❌ 慢请求下钻（`call_traces` 详情）——v3 再做
- ❌ 健康检查阈值、auto-recovery 阈值调整
- ❌ 模型自动恢复（001 已覆盖）

---

## 附：SQL 参考（用于后端实现）

```sql
-- 模型组视角 1h
WITH stats AS (
  SELECT
    group_name,
    COUNT(*) AS req_count,
    SUM(CASE WHEN status BETWEEN 200 AND 299 THEN 1 ELSE 0 END)::float / COUNT(*) AS success_rate,
    percentile_cont(0.5)  WITHIN GROUP (ORDER BY duration_ms)    AS p50_ms,
    percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms)    AS p95_ms,
    percentile_cont(0.5)  WITHIN GROUP (ORDER BY first_token_ms) AS ttft_p50,
    percentile_cont(0.95) WITHIN GROUP (ORDER BY first_token_ms) AS ttft_p95,
    SUM(CASE WHEN failover_used THEN 1 ELSE 0 END) AS failover_count,
    SUM(CASE WHEN status = 429 THEN 1 ELSE 0 END)  AS err_429,
    SUM(CASE WHEN status >= 500 THEN 1 ELSE 0 END) AS err_5xx
  FROM call_logs
  WHERE ts > NOW() - INTERVAL '1 hour'
    AND routed_via_group = true
  GROUP BY group_name
)
SELECT * FROM stats ORDER BY req_count DESC;
```

```sql
-- 组内模型明细（点击展开时按需请求，或预聚合）
SELECT
  model,
  COUNT(*) AS req_count,
  COUNT(*) * 1.0 / SUM(COUNT(*)) OVER () AS traffic_pct,
  percentile_cont(0.5)  WITHIN GROUP (ORDER BY duration_ms) AS p50_ms,
  percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms) AS p95_ms,
  SUM(CASE WHEN status BETWEEN 200 AND 299 THEN 1 ELSE 0 END)::float / COUNT(*) AS success_rate
FROM call_logs
WHERE ts > NOW() - INTERVAL '1 hour'
  AND routed_via_group = true
  AND group_name = 'middle-level'
GROUP BY model;
```

```sql
-- 24h 基线（用于百分位计算）
WITH hourly_p50 AS (
  SELECT
    group_name,
    date_trunc('hour', ts) AS hour,
    percentile_cont(0.5) WITHIN GROUP (ORDER BY duration_ms) AS hour_p50
  FROM call_logs
  WHERE ts > NOW() - INTERVAL '24 hours'
    AND routed_via_group = true
  GROUP BY group_name, hour
)
SELECT group_name, hour_p50 FROM hourly_p50;
```
