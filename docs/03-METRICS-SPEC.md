# 模型网关 — 指标量化算法设计

| 项 | 值 |
| :--- | :--- |
| 文档版本 | v0.2 |
| 最后更新 | 2026-05-21 |
| 状态 | 初稿（待评审） |
| 相关文档 | [`00-PRD.md`](./00-PRD.md) · [`01-TECH-STACK.md`](./01-TECH-STACK.md) · [`02-ROADMAP.md`](./02-ROADMAP.md) |

---

## 1. 文档目的

本文档定义模型网关的**核心业务指标**的量化算法：如何采集、如何计算、数据来源、存储方式，以及每个指标对业务决策的指导价值。面向开发人员，指导程序实现。

---

## 1.1 指标观测两层体系

本项目的指标观测分为两层，面向不同受众、不同展示位置：

| 层 | 面向 | 数据形态 | 展示位置 | 实现方式 |
| :--- | :--- | :--- | :--- | :--- |
| **用户行为层** | 平台用户（员工） | 个人用量、调用记录、Token 进度、成本 | 网关管理后台 UI | Postgres `usage_daily` → 后台 API → 前端 |
| **产品指标层** | 项目管理方（团队/项目管理平台） | 北极星指标、采用率、超额率、TTFT | 公司项目管理平台 | 网关 `GET /metrics`（Prometheus 格式）→ 独立采集服务 → 项目管理平台 |

### 架构图

```
┌─────────────────────────────────────────────────────────┐
│                   模型网关服务                            │
│                                                         │
│  ┌─────────────────────┐    ┌────────────────────────┐  │
│  │ 用户行为层数据       │    │ 产品指标层数据          │  │
│  │                     │    │                        │  │
│  │ Postgres usage_daily│    │ Prometheus Registry    │  │
│  │ (用量/Token/成本)   │    │ (Counters/Histograms/ │  │
│  │                     │    │  Gauges)              │  │
│  └──────────┬──────────┘    └───────────┬────────────┘  │
│             │                           │               │
│             ▼                           ▼               │
│  ┌─────────────────────┐    ┌────────────────────────┐  │
│  │ GET /api/v1/usage/* │    │ GET /metrics           │  │
│  │ (JSON, 业务API)     │    │ (Prometheus text格式)  │  │
│  └──────────┬──────────┘    └───────────┬────────────┘  │
└─────────────┼───────────────────────────┼───────────────┘
              │                           │
              ▼                           ▼
   ┌─────────────────────┐    ┌────────────────────────┐
   │  管理后台 UI         │    │  独立采集服务           │
   │  (Vue 3)            │    │  (定时 scrape /metrics │
   │                     │    │   → 转发到项目管理平台) │
   │  展示给：平台用户    │    │                        │
   └─────────────────────┘    └───────────┬────────────┘
                                          │
                                          ▼
                              ┌────────────────────────┐
                              │  公司项目管理平台       │
                              │  (产品指标看板)        │
                              └────────────────────────┘
```

### 设计原则

1. **网关服务本身不依赖 Prometheus Server / Grafana**——只暴露 `/metrics` 端点，零外部依赖
2. **采集服务与网关完全解耦**——独立进程，定时 HTTP GET 拉取 `/metrics`，挂了不影响业务
3. **Prometheus text format 是行业标准**——未来如果需要接 Grafana/AlertManager，零成本接入
4. **用户行为数据走 Postgres → 业务 API**——用量、Token、成本等展示在管理后台，与指标层互不干扰

---

## 2. 指标总览

| # | 指标名称 | 类型 | 采集方式 | 引入版本 | 对齐 PRD 价值 |
| :-: | :--- | :--- | :--- | :--- | :--- |
| M1 | 接入 API 的时间成本 | 体验指标 | 线下问卷 + 埋点辅助 | V0.1+ | ④ 极简接入 |
| M2 | 超限额用户比例 | 管控指标 | 系统自动计算 | V0.4+ | ⑦ 成本可控 |
| M3 | 接入网关员工数 | 采用指标 | 系统自动统计 | V0.1+ | 北极星 |
| M4 | 网关日均调用次数 | 规模指标 | 系统自动统计 | V0.1+ | 北极星 |
| M5 | 网关日均 Token 消耗 | 成本指标 | 系统自动统计 | V0.4+ | ⑦ 成本可控 |
| M6 | 网关调用平均时延（TTFT） | 质量指标 | 系统自动测量 | V0.1+ | 北极星（TTFT P95） |
| M7 | 接口健康可用率（SLA） | 质量指标 | 系统自动计算 | V0.1+ | 北极星 |
| M8 | 缓存命中率 | 效率指标 | 系统自动计算 | V0.6+ | ⑤ 可观测 |
| M9 | 报错率 | 质量指标 | 系统自动计算 | V0.6+ | ⑤ 可观测 |

> M1–M6 为用户确认的核心指标，M7–M9 为基于行业实践建议补充的指标。

---

## 3. 各指标详细设计

### M1: 接入 API 的时间成本

#### 3.1.1 定义

用户从"决定使用网关"到"成功发出第一个 API 调用"所花费的时间。衡量产品的接入门槛和极简程度。

#### 3.1.2 当前痛点与目标

| 场景 | 当前（LiteLLM） | 目标 |
| :--- | :--- | :--- |
| 手动配置 | ~10 分钟（查看 UI → 找可用模型 → 手动写配置 → 调试） | ≤ 2 分钟 |
| 自动配置（目标态） | 不支持 | Key 创建后自动拉起模型组 → 自动生成 OpenCode 配置 → 0 配置接入 |

#### 3.1.3 采集方案

**方案 A：线下访谈（定性，MVP 阶段主用）**

```
频率：每个版本发布后 1 周内
样本：≥ 10 名新接入用户（含不同技术水平）
方式：结构化问卷 + 计时观察
问题：
  1. 从打开平台到第一次成功调用，花了多长时间？
  2. 过程中遇到哪些卡点？
  3. 是否需要查文档？查了几次？
```

**方案 B：埋点辅助（定量，V0.2+ 自动化）**

```
事件序列：
  E1: user_key_created        — 用户创建 Key 的时刻
  E2: user_first_api_call     — 该 Key 第一次成功调用 /v1/* 的时刻

计算：
  onboarding_time = E2.timestamp - E1.timestamp

存储：写入 Postgres 用户事件表
  CREATE TABLE user_events (
    user_id     UUID NOT NULL,
    event_type  VARCHAR(50) NOT NULL,
    timestamp   TIMESTAMPTZ NOT NULL,
    metadata    JSONB
  );
```

**方案 C：配置生成跟踪（V0.2+ 进阶）**

如果实现了"Key 自动拉起模型组 + 自动生成 opencode 配置"功能：

```
事件序列：
  E1: user_key_created
  E2: config_auto_generated       — 系统为用户自动生成配置的时刻
  E3: user_config_downloaded      — 用户下载/应用配置的时刻
  E4: user_first_api_call         — 首次成功调用

自动接入时间 = E4 - E1（理想情况应 < 60s）
手动干预时间 = E4 - E3（如果用户没有手动干预，此值为 0）
```

#### 3.1.4 输出与展示

| 输出 | 粒度 | 用途 |
| :--- | :--- | :--- |
| 中位接入时间 | 按版本 | 版本间对比，衡量优化效果 |
| P90 接入时间 | 按版本 | 长尾用户的体验 |
| 自动 vs 手动配置比例 | 按周 | 衡量自动化功能的采纳度 |

#### 3.1.5 价值

- 直接衡量"极简接入"核心价值的达成程度
- 指导产品迭代方向：哪些环节是用户卡点
- 量化"Key 自动拉起模型组"等功能的 ROI

---

### M2: 超限额用户比例

#### 3.2.1 定义

在给定统计周期内，实际 Token 消耗超出其套餐配额的用户占总活跃用户的比例。

#### 3.2.2 算法

```
// 输入
active_users = 统计周期内至少调用过 1 次的用户集合
quota_exceeded_users = 统计周期内触发过"配额超出"拦截的用户集合

// 计算
quota_exceed_rate = |quota_exceeded_users| / |active_users| × 100%

// 分层计算（按套餐等级）
for each plan_level in [低, 中, 高]:
    exceed_rate[plan_level] = 
        count(users in plan_level AND exceeded) / 
        count(users in plan_level AND active) × 100%
```

#### 3.2.3 数据来源

```sql
-- 从 Postgres quota_consumption_periods 表 + usage_daily 表关联计算
-- 每日定时任务统计

SELECT 
    tp.plan_level,
    COUNT(DISTINCT CASE WHEN ud.total_tokens > tp.token_limit THEN ud.user_id END) AS exceeded_users,
    COUNT(DISTINCT ud.user_id) AS active_users,
    ROUND(
        COUNT(DISTINCT CASE WHEN ud.total_tokens > tp.token_limit THEN ud.user_id END)::numeric / 
        NULLIF(COUNT(DISTINCT ud.user_id), 0) * 100, 2
    ) AS exceed_rate_pct
FROM usage_daily ud
JOIN users u ON ud.user_id = u.id
JOIN token_plans tp ON u.plan_id = tp.id
WHERE ud.date BETWEEN :period_start AND :period_end
GROUP BY tp.plan_level;
```

#### 3.2.4 触发时机与存储

| 计算频率 | 存储位置 | 用途 |
| :--- | :--- | :--- |
| 实时（调用时） | Redis `rl:quota:{user_id}:{period}` | 限流决策（429 响应） |
| 每日汇总 | Postgres `metrics_daily` | 管理员看板、趋势分析 |
| 每周/月报 | Postgres `metrics_period` | 套餐策略调整依据 |

#### 3.2.5 告警规则

```yaml
# 超额率 > 20% 时预警（说明套餐设置过低或有异常使用）
- alert: HighQuotaExceedRate
  expr: gateway_quota_exceed_rate > 20
  for: 1h
  labels:
    severity: warning
  annotations:
    summary: "超限额用户比例过高 ({{ $value }}%)"
```

#### 3.2.6 价值

| 指标状态 | 解读 | 行动 |
| :--- | :--- | :--- |
| 超额率 < 5% | 套餐设置合理 | 维持现状 |
| 超额率 5%–20% | 部分用户有更高需求 | 考虑上调基础套餐或提供升级路径 |
| 超额率 > 20% | 套餐与实际需求严重不匹配 | 紧急评估套餐策略，避免影响正常工作 |
| 特定工种超额率高 | 该工种的模型使用强度被低估 | 按工种调整套餐等级 |

---

### M3: 接入网关员工数

#### 3.3.1 定义

在网关系统中创建过 API Key 且在统计周期内有实际调用行为的员工数量。

#### 3.3.2 算法

```
// 注册用户数（宽口径）
registered_users = COUNT(DISTINCT users WHERE has_api_key = true)

// 活跃用户数（窄口径，推荐作为北极星）
active_users(period) = COUNT(DISTINCT user_id FROM usage_daily 
                             WHERE date BETWEEN period_start AND period_end
                             AND requests > 0)

// DAU / WAU / MAU
dau = active_users(today)
wau = active_users(last_7_days)
mau = active_users(last_30_days)

// 用户粘性
stickiness = dau / mau × 100%    // 行业标准：开发者工具 20%–40% 为健康
```

#### 3.3.3 数据来源

```sql
-- DAU: 从 Redis 实时计数（usage:buf）或 Postgres usage_daily
SELECT COUNT(DISTINCT user_id) AS dau
FROM usage_daily
WHERE date = CURRENT_DATE AND requests > 0;

-- MAU: 30 天滚动窗口
SELECT COUNT(DISTINCT user_id) AS mau
FROM usage_daily
WHERE date >= CURRENT_DATE - INTERVAL '30 days' AND requests > 0;

-- 新增用户趋势
SELECT date, COUNT(*) AS new_users
FROM users
WHERE created_at >= :start_date
GROUP BY date
ORDER BY date;
```

#### 3.3.4 Prometheus 指标

```
# HELP gateway_active_users_total 活跃用户数（按周期）
# TYPE gateway_active_users_total gauge
gateway_active_users_total{period="daily"}  42
gateway_active_users_total{period="weekly"} 85
gateway_active_users_total{period="monthly"} 102
```

#### 3.3.5 目标对齐

| 阶段 | 目标 | 来源 |
| :--- | :--- | :--- |
| MVP | 试点部门接入（10–20 人） | ROADMAP §2 |
| RC | ≥ LiteLLM 现有 100 人 | PRD §6.1 |
| GA+6 月 | 覆盖全员 | PRD §6.1 |

---

### M4: 网关日均调用次数

#### 3.4.1 定义

网关每天成功完成的模型调用请求数（不含被限流拦截的 429 请求，不含健康检查等内部请求）。

#### 3.4.2 算法

```
// 基础统计
daily_requests = COUNT(requests WHERE date = today 
                       AND path LIKE '/v1/%' 
                       AND status_code NOT IN (429, 401, 403)
                       AND is_internal = false)

// 多维分解
daily_requests_by_model    = GROUP BY model
daily_requests_by_provider = GROUP BY provider
daily_requests_by_dept     = GROUP BY dept_id
daily_requests_by_user     = GROUP BY user_id

// 增长率
growth_rate_mom = (this_month_avg - last_month_avg) / last_month_avg × 100%
// PRD 目标：月环比 ≥ 15%
```

#### 3.4.3 数据来源

**实时计数（Prometheus）：**
```
# 已有指标直接复用
gateway_requests_total{provider, model, status}

# 日均调用 = 当日成功请求总数
daily_requests = sum(increase(gateway_requests_total{status=~"2.."}[24h]))
```

**持久化（Postgres `usage_daily`）：**
```sql
SELECT SUM(requests) AS daily_requests
FROM usage_daily
WHERE date = CURRENT_DATE;

-- 趋势
SELECT date, SUM(requests) AS daily_requests
FROM usage_daily
WHERE date >= CURRENT_DATE - INTERVAL '30 days'
GROUP BY date
ORDER BY date;
```

#### 3.4.4 派生指标

| 派生指标 | 公式 | 用途 |
| :--- | :--- | :--- |
| QPS 峰值 | `max(rate(gateway_requests_total[1m]))` | 容量规划 |
| 每用户日均调用 | `daily_requests / dau` | 用户粘性 |
| 模型使用分布 | `requests_by_model / total_requests` | 模型热度 |

---

### M5: 网关日均 Token 消耗

#### 3.5.1 定义

网关每天实际消耗的 Token 总量，区分输入 Token（prompt_tokens）和输出 Token（completion_tokens），并关联成本估算。

#### 3.5.2 算法

```
// Token 统计（每次调用完成时采集）
tokens_in  = response.usage.prompt_tokens
tokens_out = response.usage.completion_tokens
tokens_total = tokens_in + tokens_out

// 日聚合
daily_tokens_in  = SUM(tokens_in  WHERE date = today)
daily_tokens_out = SUM(tokens_out WHERE date = today)
daily_tokens     = daily_tokens_in + daily_tokens_out

// 成本估算
cost_per_call = tokens_in  × model.pricing.input_per_token
              + tokens_out × model.pricing.output_per_token

daily_cost = SUM(cost_per_call WHERE date = today)

// 多维聚合（复用 usage_daily 表结构）
// 粒度：user_id × dept_id × model_group × model × date
```

#### 3.5.3 模型定价表

```yaml
# config/model_pricing.yaml（运营配置，可热更新）
models:
  qwen-max:
    input_per_1k_tokens: 0.02      # ¥ / 1K tokens
    output_per_1k_tokens: 0.06
  deepseek-chat:
    input_per_1k_tokens: 0.001
    output_per_1k_tokens: 0.002
  claude-sonnet:
    input_per_1k_tokens: 0.003     # $ / 1K tokens
    output_per_1k_tokens: 0.015
    currency: USD
```

#### 3.5.4 数据来源

```sql
-- 从 usage_daily 读取
SELECT 
    date,
    SUM(tokens_in) AS total_tokens_in,
    SUM(tokens_out) AS total_tokens_out,
    SUM(tokens_in + tokens_out) AS total_tokens,
    SUM(cost_estimate) AS total_cost
FROM usage_daily
WHERE date = CURRENT_DATE
GROUP BY date;

-- 按部门的成本排行
SELECT 
    d.name AS department,
    SUM(ud.cost_estimate) AS dept_cost,
    SUM(ud.tokens_in + ud.tokens_out) AS dept_tokens
FROM usage_daily ud
JOIN departments d ON ud.dept_id = d.id
WHERE ud.date >= CURRENT_DATE - INTERVAL '30 days'
GROUP BY d.name
ORDER BY dept_cost DESC;
```

#### 3.5.5 Prometheus 指标

```
# HELP gateway_tokens_total 累计消耗 Token 数
# TYPE gateway_tokens_total counter
gateway_tokens_total{type="input", provider="bailian", model="qwen-max"} 1523400
gateway_tokens_total{type="output", provider="bailian", model="qwen-max"} 892100

# HELP gateway_cost_total 累计成本（人民币，分）
# TYPE gateway_cost_total counter
gateway_cost_total{provider="bailian", model="qwen-max"} 15234
```

#### 3.5.6 价值

- **预算管控**：按部门/模型维度拆分成本，支撑套餐策略调整
- **趋势预测**：基于历史 Token 消耗做线性外推，预警预算超支
- **ROI 衡量**：每 ¥1 Token 成本带来的业务产出（需要结合业务方反馈）

---

### M6: 网关调用平均时延（TTFT 为主）

#### 3.6.1 定义

对于流式请求，衡量从网关收到请求到用户收到第一个 Token 的时间（TTFT）。这是用户感知最直接的延迟指标。

#### 3.6.2 时延分层模型

```
总延迟 = 网关处理延迟 + 上游响应延迟

其中：
  网关处理延迟 = 鉴权 + 限流检查 + 路由决策 + 请求转发开销
               = 通常 5–20ms（Go 语言优势，目标 P95 ≤ 20ms）
  
  上游响应延迟 = 上游排队 + 模型推理（TTFT 大头）
               = 通常 500ms–5000ms（因模型/负载而异）

关键洞察：
  - 语言切换（Python→Go）节省 ~几百毫秒，对用户几乎无感
  - 真正的优化空间在"路由策略"——选择当前时延最低的渠道
  - TTFT 的优化效果才是对用户有感的
```

#### 3.6.3 TTFT 测量算法

```go
// 在 adapter 层流式响应中测量
func (a *Adapter) ChatStream(ctx context.Context, req Request) (StreamReader, error) {
    startTime := time.Now()
    
    stream, err := a.upstream.ChatStream(ctx, req)
    if err != nil {
        return nil, err
    }
    
    return &instrumentedStream{
        inner:     stream,
        startTime: startTime,
        onFirstChunk: func() {
            ttft := time.Since(startTime)
            // 记录 TTFT
            metrics.TTFTHistogram.WithLabelValues(provider, model).Observe(ttft.Seconds())
            // 写入 OTLP span event
            span.AddEvent("first_token", trace.WithAttributes(
                attribute.Float64("ttft_ms", float64(ttft.Milliseconds())),
            ))
        },
    }, nil
}
```

#### 3.6.4 分位数计算（Prometheus Histogram）

```
# Histogram 桶定义（针对 LLM 场景优化）
gateway_ttft_seconds_bucket{le="0.1"}     # 100ms
gateway_ttft_seconds_bucket{le="0.25"}    # 250ms
gateway_ttft_seconds_bucket{le="0.5"}     # 500ms
gateway_ttft_seconds_bucket{le="1.0"}     # 1s
gateway_ttft_seconds_bucket{le="2.0"}     # 2s
gateway_ttft_seconds_bucket{le="5.0"}     # 5s
gateway_ttft_seconds_bucket{le="10.0"}    # 10s
gateway_ttft_seconds_bucket{le="30.0"}    # 30s
gateway_ttft_seconds_bucket{le="+Inf"}

# 分位数查询（PromQL）
# P50
histogram_quantile(0.50, rate(gateway_ttft_seconds_bucket[5m]))
# P95（北极星指标）
histogram_quantile(0.95, rate(gateway_ttft_seconds_bucket[5m]))
# P99
histogram_quantile(0.99, rate(gateway_ttft_seconds_bucket[5m]))
```

#### 3.6.5 路由策略对 TTFT 的优化

```
// 多决策负载均衡评分函数（V0.7 实现）
score(channel) = w1 × health_score 
              + w2 × (1 - normalize(latency_p95)) 
              + w3 × (1 - error_rate) 
              + w4 × static_weight

// 其中 latency_p95 来自进程内滚动窗口（5min 窗口）
// 优先选择 score 最高的渠道，从而降低整体 TTFT
```

#### 3.6.6 A/B 对比算法（与 LiteLLM 对比）

```sql
-- 同模型、同时间段，对比两个网关的 TTFT
-- 方法：双跑期（V1.0 灰度切流时）
-- 同一个请求按比例发到 LiteLLM 和新网关，对比 TTFT

-- 从 Prometheus 查询对比
-- 新网关 P95 TTFT
histogram_quantile(0.95, rate(gateway_ttft_seconds_bucket{source="new"}[1h]))
-- LiteLLM P95 TTFT（需要在 LiteLLM 侧也部署 Prometheus exporter）
histogram_quantile(0.95, rate(litellm_ttft_seconds_bucket{source="old"}[1h]))

-- 改善率
improvement = (litellm_p95 - gateway_p95) / litellm_p95 × 100%
-- 目标：≥ 30%
```

#### 3.6.7 网关自身延迟（控制面指标）

```
# 单独测量网关处理开销（不含上游等待时间）
gateway_overhead_seconds = request_forwarded_at - request_received_at

# Prometheus
gateway_overhead_duration_seconds{le="0.005"}   # 5ms
gateway_overhead_duration_seconds{le="0.010"}   # 10ms
gateway_overhead_duration_seconds{le="0.020"}   # 20ms（P95 目标）
gateway_overhead_duration_seconds{le="0.050"}   # 50ms
gateway_overhead_duration_seconds{le="+Inf"}
```

#### 3.6.8 价值

| 场景 | 通过 TTFT 指标驱动的行动 |
| :--- | :--- |
| 某渠道 TTFT 突增 | 路由策略自动降低该渠道权重，切到其他渠道 |
| 整体 TTFT 趋势上升 | 触发告警，运维检查上游渠道状态 |
| A/B 对比未达 30% | 分析瓶颈（网关 vs 渠道），针对性优化 |
| 不同模型 TTFT 差异大 | 辅助用户选型（展示在健康看板） |

---

### M7: 接口健康可用率（SLA）

#### 3.7.1 定义

网关 `/v1/*` 接口非 5xx 响应的占比。排除上游错误透传（如上游 429 不算网关不可用）。

#### 3.7.2 算法

```
// 严格定义：只有网关自身导致的 5xx 才算不可用
gateway_errors = COUNT(responses WHERE status_code >= 500 
                       AND error_source = "gateway")

total_requests = COUNT(responses WHERE path LIKE '/v1/%')

sla = (1 - gateway_errors / total_requests) × 100%

// 目标：≥ 99.9%（即每万次请求允许 ≤ 10 次网关级 5xx）
```

#### 3.7.3 错误来源分类

| HTTP 状态码 | 来源 | 是否计入 SLA 分母 | 是否计入 SLA 分子（失败） |
| :--- | :--- | :--- | :--- |
| 2xx | - | ✅ | ❌ |
| 400 | 客户端 | ✅ | ❌ |
| 401/403 | 网关鉴权 | ✅ | ❌ |
| 429 | 网关限流 | ✅ | ❌（正常行为） |
| 502 | 上游不可用 | ✅ | ❌（非网关故障） |
| 500 | **网关内部错误** | ✅ | ✅ |
| 503 | **网关过载** | ✅ | ✅ |

#### 3.7.4 Prometheus 查询

```promql
# SLA 计算（5 分钟窗口）
1 - (
  sum(rate(gateway_requests_total{status=~"5..", error_source="gateway"}[5m]))
  /
  sum(rate(gateway_requests_total[5m]))
) 
```

---

### M8: 缓存命中率

#### 3.8.1 定义

KV 缓存命中的请求占可缓存请求总数的比例。

#### 3.8.2 算法

```
cache_hit_rate = gateway_cache_hits_total / 
                 (gateway_cache_hits_total + gateway_cache_misses_total) × 100%

// 排除不可缓存的请求（stream=true 在 V1.0 前不缓存）
// 排除配置了 no-cache 的请求
```

#### 3.8.3 成本节省估算

```
cache_savings = SUM(
    hit_request.estimated_tokens × model.pricing.per_token
) over period

// 这就是"如果这些请求没命中缓存，会花多少钱"
```

---

### M9: 报错率

#### 3.9.1 定义

所有非 2xx 响应（排除客户端错误 4xx 中的 400/401/403/429）占总请求的比例。

#### 3.9.2 分层算法

```
// 整体报错率（目标 ≤ 0.5%）
overall_error_rate = COUNT(status >= 500 OR status == 408 OR status == 502) 
                     / total_requests × 100%

// 核心厂商报错率（目标 ≤ 0.3%）
core_provider_error_rate = COUNT(errors WHERE provider IN ('bailian', 'deepseek', 'bedrock'))
                           / COUNT(requests WHERE provider IN ('bailian', 'deepseek', 'bedrock'))
                           × 100%
```

---

## 4. 数据流架构

### 4.1 网关进程内采集

```
┌──────────────────────────────────────────────────────────────────────────┐
│                        网关请求处理流程                                    │
│                                                                          │
│  请求进入 → 鉴权 → 限流检查 → 路由决策 → 上游调用 → 响应/流式            │
│     │         │        │          │         │          │                 │
│     ▼         ▼        ▼          ▼         ▼          ▼                 │
│  [记录开始] [auth耗时] [限流命中?] [选择渠道] [TTFT测量] [token计数]      │
└─────┬────────┬─────────┬──────────┬─────────┬──────────┬─────────────────┘
      │        │         │          │         │          │
      ▼        ▼         ▼          ▼         ▼          ▼
┌──────────────────────────────────────────────────────────────────────────┐
│                     指标采集层（进程内，两路并行）                           │
│                                                                          │
│  ┌─ 产品指标层 ─────────────────┐  ┌─ 用户行为层 ──────────────────────┐ │
│  │ Prometheus Registry          │  │ 用量 Buffer                       │ │
│  │ • gateway_requests_total     │  │ (user_id × model × date)         │ │
│  │ • gateway_ttft_seconds       │  │ • requests / tokens_in/out / cost│ │
│  │ • gateway_tokens_total       │  │                                   │ │
│  │ • gateway_active_users_total │  │                                   │ │
│  │ • gateway_quota_exceed_rate  │  │                                   │ │
│  │ • gateway_overhead_seconds   │  │                                   │ │
│  └──────────────┬───────────────┘  └────────────────┬──────────────────┘ │
└─────────────────┼──────────────────────────────────────┼─────────────────┘
                  │                                      │
                  ▼                                      ▼
      ┌───────────────────────┐              ┌───────────────────────┐
      │ GET /metrics          │              │ Postgres usage_daily  │
      │ (Prometheus text fmt) │              │ (每 60s async flush)  │
      └───────────┬───────────┘              └───────────┬───────────┘
                  │                                      │
                  ▼                                      ▼
      ┌───────────────────────┐              ┌───────────────────────┐
      │ 独立采集服务           │              │ GET /api/v1/usage/*   │
      │ (cron scrape, 5min)  │              │ (JSON 业务接口)       │
      └───────────┬───────────┘              └───────────┬───────────┘
                  │                                      │
                  ▼                                      ▼
      ┌───────────────────────┐              ┌───────────────────────┐
      │ 公司项目管理平台       │              │ 管理后台 UI (Vue 3)  │
      │ (产品指标看板)        │              │ (员工看自己的数据)    │
      └───────────────────────┘              └───────────────────────┘
```

### 4.2 `/metrics` 端点设计（Prometheus text format）

网关暴露 `GET /metrics`，返回标准 Prometheus exposition format。示例输出：

```prometheus
# HELP gateway_requests_total 网关完成的模型调用总数
# TYPE gateway_requests_total counter
gateway_requests_total{provider="bailian",model="qwen-max",status="200"} 15234
gateway_requests_total{provider="deepseek",model="deepseek-chat",status="200"} 8921
gateway_requests_total{provider="bailian",model="qwen-max",status="500"} 3

# HELP gateway_ttft_seconds 流式请求首Token时延
# TYPE gateway_ttft_seconds histogram
gateway_ttft_seconds_bucket{provider="bailian",model="qwen-max",le="0.5"} 12000
gateway_ttft_seconds_bucket{provider="bailian",model="qwen-max",le="1.0"} 14500
gateway_ttft_seconds_bucket{provider="bailian",model="qwen-max",le="2.0"} 15100
gateway_ttft_seconds_bucket{provider="bailian",model="qwen-max",le="5.0"} 15200
gateway_ttft_seconds_bucket{provider="bailian",model="qwen-max",le="+Inf"} 15234
gateway_ttft_seconds_sum{provider="bailian",model="qwen-max"} 10523.4
gateway_ttft_seconds_count{provider="bailian",model="qwen-max"} 15234

# HELP gateway_tokens_total 累计消耗Token数
# TYPE gateway_tokens_total counter
gateway_tokens_total{type="input",provider="bailian",model="qwen-max"} 1523400
gateway_tokens_total{type="output",provider="bailian",model="qwen-max"} 892100

# HELP gateway_active_users_total 活跃用户数
# TYPE gateway_active_users_total gauge
gateway_active_users_total{period="daily"} 42
gateway_active_users_total{period="weekly"} 85
gateway_active_users_total{period="monthly"} 102

# HELP gateway_quota_exceed_rate 超限额用户比例(%)
# TYPE gateway_quota_exceed_rate gauge
gateway_quota_exceed_rate{plan_level="low"} 12.5
gateway_quota_exceed_rate{plan_level="medium"} 5.2
gateway_quota_exceed_rate{plan_level="high"} 1.0

# HELP gateway_overhead_seconds 网关自身处理延迟(不含上游)
# TYPE gateway_overhead_seconds histogram
gateway_overhead_seconds_bucket{le="0.005"} 14000
gateway_overhead_seconds_bucket{le="0.010"} 15000
gateway_overhead_seconds_bucket{le="0.020"} 15200
gateway_overhead_seconds_bucket{le="+Inf"} 15234

# HELP gateway_onboarding_seconds 用户从创建Key到首次调用的时间
# TYPE gateway_onboarding_seconds histogram
gateway_onboarding_seconds_bucket{le="60"} 25
gateway_onboarding_seconds_bucket{le="120"} 38
gateway_onboarding_seconds_bucket{le="300"} 45
gateway_onboarding_seconds_bucket{le="600"} 48
gateway_onboarding_seconds_bucket{le="+Inf"} 50

# HELP gateway_cache_hits_total 缓存命中次数
# TYPE gateway_cache_hits_total counter
gateway_cache_hits_total 3200

# HELP gateway_cache_misses_total 缓存未命中次数
# TYPE gateway_cache_misses_total counter
gateway_cache_misses_total 12034

# HELP gateway_ratelimit_rejects_total 限流拒绝次数
# TYPE gateway_ratelimit_rejects_total counter
gateway_ratelimit_rejects_total{type="tpm"} 23
gateway_ratelimit_rejects_total{type="rpm"} 15
gateway_ratelimit_rejects_total{type="concurrent"} 8
gateway_ratelimit_rejects_total{type="quota"} 42
```

### 4.3 独立采集服务设计

采集服务是一个**极简的独立进程**，与网关代码库无关：

```
职责：定时 GET 网关的 /metrics → 解析 Prometheus text → 转换格式 → POST 到项目管理平台

技术选型：
  - 语言：Go / Python / 任意（建议 Go 保持技术栈统一）
  - 部署：独立容器 or 独立进程
  - 频率：每 5 分钟 scrape 一次（可配置）
  - 依赖：仅需 HTTP 访问网关 /metrics + 项目管理平台 API

伪代码：
  loop every 5min:
    resp = HTTP GET "http://gateway:8080/metrics"
    metrics = parse_prometheus_text(resp.body)
    payload = transform_to_pm_format(metrics)
    HTTP POST "https://pm-platform.company.com/api/metrics" with payload
```

### 4.4 分阶段引入策略

| 阶段 | `/metrics` 端点 | 独立采集服务 | Prometheus Server | Grafana |
| :--- | :--- | :--- | :--- | :--- |
| **V0.1–V0.4** | ✅ 代码中埋好，暴露端点 | ✅ 部署，对接项目管理平台 | ❌ 不需要 | ❌ 不需要 |
| **V0.5–V0.6** | ✅ 指标逐步完善 | ✅ | 可选（如需 Grafana 看板） | 可选 |
| **V0.7+** | ✅ | ✅ | 推荐（告警规则） | 推荐（运维看板） |

> **核心原则**：MVP 阶段网关的 `docker-compose` 只有 Postgres + Redis + 网关 + 前端。`/metrics` 端点是代码内置的（几十行），不增加任何外部依赖。Prometheus/Grafana 是未来的可选增强。

---

## 5. 存储方案总结

### 5.1 指标与存储层映射

| 指标 | 用户行为层（Postgres） | 产品指标层（/metrics） | 独立采集服务转发 |
| :--- | :--- | :--- | :--- |
| M1 接入时间 | `user_events`（Key创建/首次调用事件） | `gateway_onboarding_seconds`（histogram） | ✅ → 项目管理平台 |
| M2 超额率 | `usage_daily` + `token_plans` 关联查询 | `gateway_quota_exceed_rate`（gauge, 按 plan_level） | ✅ → 项目管理平台 |
| M3 活跃用户 | `usage_daily` 去重统计 | `gateway_active_users_total`（gauge, 按 period） | ✅ → 项目管理平台 |
| M4 日均调用 | `usage_daily.requests` 聚合 | `gateway_requests_total`（counter） | ✅ → 项目管理平台 |
| M5 Token 消耗 | `usage_daily.tokens_in/out/cost` | `gateway_tokens_total`（counter, 按 type） | ✅ → 项目管理平台 |
| M6 TTFT | —（用户侧不需要原始值） | `gateway_ttft_seconds`（histogram） | ✅ → 项目管理平台 |
| M7 SLA | — | `gateway_requests_total`（按 status 计算） | ✅ → 项目管理平台 |
| M8 缓存命中 | — | `gateway_cache_hits/misses_total`（counter） | ✅ → 项目管理平台 |
| M9 报错率 | — | `gateway_requests_total`（按 status 计算） | ✅ → 项目管理平台 |

### 5.2 两层数据的关系

```
同一次 API 调用会同时写入两层：

  调用完成 ─┬─► 用户行为层：累加到进程内 buffer → 60s flush → Postgres usage_daily
            │    （给管理后台 UI：我的用量、部门排行、Token 进度）
            │
            └─► 产品指标层：Prometheus counter/histogram 原子递增
                 （给 /metrics → 独立采集服务 → 项目管理平台）

两层独立运作，互不依赖。任一层故障不影响另一层。
```

### 5.3 Go 实现要点

```go
// 使用 prometheus/client_golang 标准库
import "github.com/prometheus/client_golang/prometheus"
import "github.com/prometheus/client_golang/prometheus/promhttp"

// 注册指标（启动时一次性）
var (
    requestsTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "gateway_requests_total",
            Help: "网关完成的模型调用总数",
        },
        []string{"provider", "model", "status"},
    )
    
    ttftHistogram = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "gateway_ttft_seconds",
            Help:    "流式请求首Token时延",
            Buckets: []float64{0.1, 0.25, 0.5, 1.0, 2.0, 5.0, 10.0, 30.0},
        },
        []string{"provider", "model"},
    )
    
    activeUsers = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "gateway_active_users_total",
            Help: "活跃用户数",
        },
        []string{"period"},
    )
)

// 暴露端点（Gin 路由注册）
router.GET("/metrics", gin.WrapH(promhttp.Handler()))

// 业务代码中记录（每次调用完成时）
requestsTotal.WithLabelValues(provider, model, statusCode).Inc()
ttftHistogram.WithLabelValues(provider, model).Observe(ttft.Seconds())
tokensTotal.WithLabelValues("input", provider, model).Add(float64(promptTokens))
tokensTotal.WithLabelValues("output", provider, model).Add(float64(completionTokens))

// 定时任务更新 gauge 类指标（如活跃用户数、超额率，每 5 分钟从 DB 查询刷新）
go func() {
    ticker := time.NewTicker(5 * time.Minute)
    for range ticker.C {
        dau := queryDAU(db)
        activeUsers.WithLabelValues("daily").Set(float64(dau))
        // ...
    }
}()
```

---

## 6. 看板建议

### 6.1 用户行为层看板（管理后台 UI 内）

> 数据源：Postgres `usage_daily` → 业务 API → 前端渲染

| 面板 | 面向用户 | 刷新频率 |
| :--- | :--- | :--- |
| 我的用量（今日/本周/本月 Token + 调用 + 成本） | 所有员工 | 5min |
| 我的套餐进度（已用/总配额，进度条） | 所有员工 | 5min |
| 部门用量排行 + 趋势 | 管理员 | 5min |
| 模型维度用量（哪个模型用得最多） | 管理员 | 5min |

### 6.2 产品指标层看板（项目管理平台）

> 数据源：独立采集服务 scrape `/metrics` → 转发到项目管理平台

| 面板 | 对应指标 | 采集频率 |
| :--- | :--- | :--- |
| 活跃用户数（DAU/WAU/MAU）及趋势 | M3 `gateway_active_users_total` | 5min |
| 日均请求数 + 月环比增长 | M4 `gateway_requests_total` | 5min |
| 日均 Token 消耗 + 成本估算 | M5 `gateway_tokens_total` | 5min |
| TTFT P95（整体 + 按模型分） | M6 `gateway_ttft_seconds` | 5min |
| 超限额用户比例（按套餐等级） | M2 `gateway_quota_exceed_rate` | 5min |
| SLA 可用率 | M7 计算自 `gateway_requests_total` | 5min |
| 接入时间中位数 | M1 `gateway_onboarding_seconds` | 5min |

### 6.3 可选运维看板（V0.6+ Grafana，仅在接入 Prometheus Server 后可用）

> 这是增强项，MVP 阶段不需要。当团队决定引入 Prometheus + Grafana 时才启用。

| 面板 | 数据源 | 用途 |
| :--- | :--- | :--- |
| 各模型实时 TTFT P50/P95 | Prometheus 查询 | 实时运维 |
| 各渠道报错率 + 告警 | Prometheus + AlertManager | 故障响应 |
| 缓存命中率趋势 | Prometheus | 缓存策略调优 |
| 并发请求数 | Prometheus | 容量规划 |
| OTLP 出口健康度 | Prometheus | 自监控 |

---

## 7. 建议补充的指标

基于行业实践（LiteLLM / Portkey / OpenRouter），建议后续版本考虑引入：

| 指标 | 说明 | 价值 | 建议版本 |
| :--- | :--- | :--- | :--- |
| **每用户日均调用数** | `daily_requests / dau` | 衡量用户粘性和依赖程度 | V0.4+ |
| **模型多样性指数** | 每用户使用的不同模型数 | 衡量用户是否充分利用平台能力 | V0.5+ |
| **Fallback 触发率** | 主渠道不可用时触发 fallback 的比例 | 衡量高可用策略的有效性 | V0.3+ |
| **Inter-Token Latency** | 流式响应中 token 之间的间隔 | 衡量流式体验的流畅度 | V0.7+ |
| **Token 预测准确率** | 限额 check 时预估 token vs 实际 token | 避免误拒或漏放 | V0.4+ |
| **成本节省率** | 缓存命中 + 路由优化节省的成本 | 量化平台 ROI | V0.7+ |

---

## 8. 变更历史

| 版本 | 日期 | 变更说明 |
| :--- | :--- | :--- |
| v0.1 | 2026-05-21 | 初版：6 个核心指标 + 3 个补充指标的量化算法设计 |
| v0.2 | 2026-05-21 | 明确两层观测体系（用户行为层 vs 产品指标层）；/metrics 采用 Prometheus 行业标准格式；新增独立采集服务架构；新增分阶段引入策略（MVP 不需要 Prometheus Server）；补充 Go 实现要点 |
