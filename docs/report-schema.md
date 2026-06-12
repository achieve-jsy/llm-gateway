# 用量周报 · report JSON 契约与派生算法设计（T2）

> 阶段一离线用量周报工具（`tools/usage-report`）设计稿。目标：走 `/api/v1/usage/*` 拉数，
> 复用网关**三层聚合**（`call_logs` 明细 → `usage_daily`/`user_daily_stats`/`model_daily_stats` 日预聚合 → 报告派生层），
> 派生「28 天历史基线 / 同岗位使用效能分位 / A1–A5 分钟窗口异常」，装配 **report JSON**，再渲染 **MD/HTML**。
> 口径必须与**现有用量看板**对账一致。
>
> 本文是 T2 产物，输入是仓库源码（`internal/observability/*`、`internal/orgservice/*`、`internal/controlplane/router/router.go`），
> 未依赖 T1 的 `docs/recon.md`（T1 尚未落盘）。字段级 schema 与派生算法以**可被 T3 落地、T4 对账**为标准。
> 凡标注「🔴 待对账确认」的参数，默认值来自本文推导或文档，须在 T4 用真实数据校准。

---

## 0. TL;DR（给实现者）

1. **只走 HTTP**：工具是**独立离线 Gobin**，`go build ./tools/usage-report`，**不直连 DB**、零库权限，
   复用网关现成 RBAC + 查询逻辑（`/api/v1/usage/*`、`/api/v1/com/my/usage*`、`/api/v1/org/*`）。
2. **三层数据流**在工具内严格分层：`fact`（纯聚合数字，只回填，不改算）→ `judgment`（基线/分位/归因/命中/标签/换算，算法可复算）
   → `expression`（headline/suggestions 措辞，只读、不引入新数字、不做新判定）。
3. **A1–A5 是「分钟窗口」规则**，定义在 §5；阈值一律**参数化**（来自 `alert_rules` 种子或工具配置），不写死。
4. **CNY 是换算**，不是判定：`est_cost_usd`（USD）是唯一事实源；CNY 在 judgment 层派生，显式记录 `rate/source/at`。
5. **口径对齐表**见 §6，T4 对账以此为基准。

---

## 1. 数据来源与三层聚合（recon 摘要）

### 1.1 三层聚合

| 层 | 表/接口 | 内容 | 复用方式 |
|---|---|---|---|
| L0 明细 | `call_logs`（`internal/observability/model/call_log.go`） | 每请求一行：`ts, key_id, key_type, user_id, dept_id, studio_id, model, group_name, provider, prompt/cached/cache_write/completion/reasoning/total_tokens, status, upstream_status, duration_ms, first_token_ms, est_cost_usd, stream, failover_used, reasoning_effort, inbound_endpoint,...` | 工具**不直接查**；由服务端聚合落库 |
| L1 日预聚合 | `usage_daily`、`user_daily_stats`、`model_daily_stats`（`model/usage_daily.go`、`model/user_daily_stats.go`、`model/model_daily_stats.go`） | 按 `stat_date` 的聚合；`RollupService` 每小时重算 + 启动补跑（`service/rollup_service.go`） | 工具经接口读取 |
| L2 报告派生 | 工具内 `judgment` 层 | 28 天基线、同岗位分位、A1–A5 命中、归因、标签、CNY | 工具自行派生 |

**L1 三张表的口径**（这些就是「看板口径」，报告必须对齐）：

- `usage_daily` 聚合 SQL（`repository/usage_daily_repo.go` 与 `rollup_service.Run`）：
  `COUNT(*) AS req_count`，`COUNT(*) FILTER (WHERE status = 200) AS success_count`，`SUM(prompt/cached/cache_write/completion/reasoning_tokens)`，
  `SUM(est_cost_usd)`，`GROUP BY date(ts), key_id, COALESCE(dept_id,''), COALESCE(studio_id,''), model`。
- `user_daily_stats`（`aggregateUserDailyStats`）：**排除 Service Key**（`WhereExcludeServiceKeyLogsCL`），
  `COUNT(DISTINCT model)` 用 `cardinality(array_agg(DISTINCT unnest(model_list)))`（防跨小时重复计数），
  `hourly_distribution` 为 `jsonb_object_agg(hour_key, hour_count)`。
- `model_daily_stats`（`aggregateModelDailyStats`）：含 `p50/p90_duration_ms`、`avg_first_token_ms`、`user_count/dept_count`。

### 1.2 组织维度关节（报告需要 JOIN 才能拿到岗位/工作室）

- `users`：`id`(UUID)、`dept_id`(UUID→`departments.id`)、`job_family`/`job_category`/`job_title`、`dept_source`、`login_status`（`internal/auth/model/user.go`）。
- `departments`：`id`(UUID)、`dept_id`(SSO 字符串)。**dept_id 归一化**：`LEFT JOIN departments d ON (d.dept_id = cl.dept_id OR d.id::text = cl.dept_id)`，取值 `COALESCE(d.dept_id, cl.dept_id)`。
- `dept_studio_map`：`dept_id → studio_id`，`dsm.dept_id = COALESCE(d.dept_id, cl.dept_id)`。
- 组织层级：`company → studio → dept → user`（`docs/ua-behavior-analysis/product-design.md` §组织范围，L1 平台/L2 单元(studio·dept)/L3 个人）。

### 1.3 工具实际可调的 HTTP 端点（控制面 `router.go`）

| 端点 | 权限 | 用途（报告拉数） |
|---|---|---|
| `GET /api/v1/my/usage` | 本人（`my`） | 个人维度聚合（工具内自动排除 Service Key） |
| `GET /api/v1/my/usage/percentiles` | 本人 | 全站近 30 天 P90 标签阈值（基线/标签阈值，**非同岗位**，见 §6 差异） |
| `GET /api/v1/usage?from&to&group_by=day` | `PermUsageViewAll` | 按日期/模型/部门/工作室的聚合序列（做 28 天基线的原始序列） |
| `GET /api/v1/usage?group_by=model` / `dept` | `PermUsageViewAll` | 模型构成 / 部门构成 |
| `GET /api/v1/usage/dashboard/users` | `PermUsageViewScoped` | 用户级增强统计（`DashboardUsersEnhanced`：summary/rows/top_models/vs_avg；**排除 Service Key**） |
| `GET /api/v1/usage/dashboard/user-detail` | `PermUsageViewScoped` | 单用户：summary + `daily_trend` + `model_distribution` + `hourly_distribution`（Asia/Shanghai） |
| `GET /api/v1/usage/dashboard` | `PermUsageViewScoped` | 作用域看板总览 |
| `GET /api/v1/usage/dashboard/models` | `PermUsageViewScoped` | 模型排行（含延迟分位、错误率、长尾标记） |
| `GET /api/v1/usage/dashboard/model-detail` | `PermUsageViewScoped` | 单模型详情 |
| `GET /api/v1/org/departments/:dept_id/members` | `PermMemberView` | 部门成员（**含 `job_family/job_category/job_title`**） |
| `GET /api/v1/org/users` | `PermOrgUserSearch` | 用户搜索（含岗位字段，取 job_category） |
| `GET /api/v1/org/studios` | 管理 | 工作室清单与 dept 映射 |

> 报告「同岗位分位」需要 `job_category`，来自 `/org/*`（岗位字段不在 usage 聚合里）。工具需以管理账号持有
> 对应权限；若权限不足，降级为「不输出岗位分位」并标注 `peer.supported=false`（见 §5.2）。

---

## 2. report JSON 顶层契约

```jsonc
{
  "schema_version": "1.0",
  "meta": {
    "tool": "usage-report",
    "level": "personal | department | studio",   // 本次报告的级别
    "period": { "from": "2026-07-20", "to": "2026-07-26", "type": "week", "days": 7, "label": "2026 第28周" },
    "scope": {
      // personal: user_id + name + job_category + studio_id + dept_id
      // department: dept_id + dept_name + studio_id
      // studio: studio_id + studio_name
      "id": "…", "name": "…", "studio_id": "…", "dept_id": "…",
      "job_category": "…", "job_family": "…", "job_title": "…"
    },
    "generated_at": "2026-07-27T09:00:00+08:00",
    "data_retrieved_at": "2026-07-27T09:00:10+08:00",
    "api_endpoints_used": ["/api/v1/my/usage", "/api/v1/usage?group_by=day", "…"],
    "currency": {
      "base": "USD",
      "report_currency": "CNY",
      "usd_cny_rate": 7.20,                 // 🔴 待对账：来源见 §5.4
      "usd_cny_rate_source": "config:usage_report.usd_cny_rate",
      "usd_cny_rate_at": "2026-07-27T09:00:00+08:00"
    },
    "qa": {
      "口径_alignment": ["success=status=200", "时区=Asia/Shanghai", "个人维度排除 key_type='service'", "…"],
      "anomaly_enabled": true,
      "peer_percentile_enabled": true,
      "note": "…"
    }
  },

  // ===== 事实层：只回填接口裸数字，不改算 =====
  "fact": { /* 见 §3，按 level 展开为 personal/department/studio 三选一 */ },

  // ===== 判断层：算法可复算 =====
  "judgment": {
    "baseline": { /* §5.1 28 天基线 */ },
    "peer":     { /* §5.2 同岗位使用效能分位 */ },
    "efficiency": { /* §5.3 效率指标与 CNY 换算 */ },
    "anomalies": [ /* §5.4 A1–A5 命中，0..n 条 */ ],
    "attribution": { /* §5.5 归因/主因（判定性） */ }
  },

  // ===== 表达层：只读回填措辞，不引新数字/新判定 =====
  "expression": {
    "headline": "…",          // 一句话结论（措辞，引用 fact/judgment 字段）
    "insights": [ "…", "…" ], // 每条 insight 为措辞，只能引用已存在字段
    "suggestions": [ "…", "…" ],
    "tone": "cost-transparency | efficiency-guidance",
    "lang": "zh-CN",
    "audience": "engineer | manager | nontechnical"   // 决定用成本/价值语言
  }
}
```

**分层铁律（T3 必须遵守）**：`fact` 是上游 raw 的拷贝；`judgment` 只由算法产出（基线/分位/命中/归因/标签/换算）；
`expression` **禁止**新增任何数字，**禁止**新增任何判定，**只能**引用 `fact`/`judgment` 中已存在的字段并写成人话。
MD/HTML 渲染器**只读 JSON**，不二次计算。

---

## 3. 事实层（fact）字段级 schema

> `fact` 各指标**默认所有口径开关**与现有看板一致（见 §6）。工具收到接口返回后**只做拷贝**，不做加总、不做比率、不做基线。

### 3.1 `fact.personal`（个人，L3）

| 字段 | 类型 | 含义 | 来源（接口/口径） | 层 |
|---|---|---|---|---|
| `scope_id` | string | user_id | `/api/v1/my/usage` | fact |
| `scope_name` | string | 用户名 | same | fact |
| `stat_date` | string | 报告日期标识（YYYY-MM-DD） | range | fact |
| `req_count` | int64 | 调用数 | `COUNT(*)` | fact |
| `success_count` | int64 | 成功数 | `FILTER (WHERE status=200)` | fact |
| `error_count` | int64 | 失败数（=req-success） | 派生差 | fact |
| `success_rate` | float | 成功率 % | `success/req*100` | **判断层**（比率，见 §5.3） |
| `prompt_tokens` | int64 | 输入 token | `SUM(prompt_tokens)` | fact |
| `cached_tokens` | int64 | 缓存命中 token | `SUM(cached_tokens)` | fact |
| `cache_write_tokens` | int64 | 缓存写入 token | `SUM(cache_write_tokens)` | fact |
| `completion_tokens` | int64 | 输出 token | `SUM(completion_tokens)` | fact |
| `reasoning_tokens` | int64 | 推理 token | `SUM(reasoning_tokens)` | fact |
| `total_tokens` | int64 | 总 token（=prompt+completion，**含 cached**；若接口只回 prompt/completion 则自算） | 接口或派生 | fact |
| `est_cost_usd` | float | 成本 USD | `SUM(est_cost_usd)` | fact |
| `model_count` | int | 用过模型数 | `COUNT(DISTINCT model)` | fact |
| `active_days` | int | 活跃天数 | `COUNT(DISTINCT stat_date)` | fact |
| `hourly_distribution` | object | `{"0":n,..."23":n}` 调用时段 | `user_daily_stats.hourly_distribution` / `/my/usage` | fact |
| `daily_avg_calls` | float | 日均调用 | `req_count / active_days` | **判断层**（派生，见 §5.3） |
| `avg_duration_ms` | float | 平均耗时 | `AVG(duration_ms)` | fact |
| `p90_duration_ms` | int | P90 耗时 | `model_daily_stats` | fact |
| `top_models` | array | Top 模型（成本/调用/token） | `/usage/dashboard/user-detail` model_distribution | fact |

> 个人口径：**排除 `key_type='service'`**（复用 `WhereExcludeServiceKeyLogs`），以 agent 身份调 `/my/usage` 已内置；
> 若以管理员身份调聚合接口，工具必须显式加 `WhereExcludeServiceKeyLogs`，否则对不上账（见 §6 差异#2）。

### 3.2 `fact.department` / `fact.studio`（单元，L2）

单元维度在 `usage_daily` 切片基础上，按「成员 vs 程序」拆成两块（对齐看板 `DashboardUsersEnhanced` + `ServiceCostEstimate` 回填）：

```jsonc
"department": {
  "scope_id": "…", "scope_name": "…", "studio_id": "…",
  "member": {               // 成员调用（exclude service key）
    "user_count": 12, "active_user_count": 10,
    "req_count": 1234, "success_count": 1200, "error_count": 34,
    "prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0,
    "est_cost_usd": 0, "model_count": 8,
    "per_capita": {         // 人均（判断层派生，见 §5.3）
      "req_count": 103, "est_cost_usd": 0, "success_rate": 0, "active_days": 0
    }
  },
  "service_key": {          // 程序 Service Key 块：仅 key_type='service' 且归属当前部门
    "req_count": 0, "success_count": 0, "est_cost_usd": 0, "total_tokens": 0,
    "active_days": 0, "notes": "程序流量单列，不吞进成员行为"
  },
  "by_dept_member": [],     // 成员维度明细（/usage/dashboard/users rows）
  "model_distribution": []  // 模型构成
}
```

`studio` 结构同 `department`，但 `member.service_key` 需要**跨 dept 汇总到 studio**，且 `by_member` 可聚合到 dept。
**单元口径差异（关键）**：成员/人均维度排除 Service Key；程序维度 `only key_type='service' AND 归属当前 dept/studio`；
dept_id 归一化 `d.dept_id = cl.dept_id OR d.id::text = cl.dept_id`，studio 经 `dept_studio_map`。

---

## 4. 字段 → 三层归属速查

| 字段 | 产出层 | 说明 |
|---|---|---|
| `req_count/success_count/tokens/est_cost_usd/model_count/active_days/hourly_distribution/duration` | 事实层 | 接口裸数字 |
| `success_rate` `error_rate` `daily_avg_calls` `per_capita.*` `cost_per_1k` `cache_hit_rate` `output_ratio` `reasoning_share` | **判断层（比率/派生）** | 由 fact 派生，算法一致 |
| `baseline.*`（median/MAD/sigma/dev_sigma/percentile） | **判断层** | §5.1 |
| `peer.*`（percentile_rank/band/pool/sample_n） | **判断层** | §5.2 |
| `efficiency.cost_usd_cny` | **判断层（换算）** | §5.4，非判定 |
| `anomalies[]`（rule/window/actual/threshold/baseline/impact/evidence） | **判断层（判定）** | §5.4 A1–A5 |
| `attribution`（主因/占比） | **判断层（归因）** | 由 fact 占比计算 |
| `headline/insights/suggestions/tone/lang` | 表达层 | 只读措辞 |

> 语义标签（如「深夜档」「高缓存」）属判断层**且必须带相对基准**（如 P90），不做绝对阈值/红绿灯
> （对齐 `docs/user-behavior-analysis-unified-design.md` §相对基准不画红绿灯）。

---

## 5. 派生算法设计

### 5.1 28 天历史基线（衡量「当前值偏离自己历史多少」）

对任一指标 `m`（调用数、成本 USD、token、成功率、cache_hit_rate …），在 **28 天**（`period.from-27d … period.to`）逐日序列上：

```
X = [m(d1), m(d2), …, m(d28)]        // 当日指标值；只取 active 日（有调用/有数据的日）
median = median(X)
MAD    = median(|x - median| for x in X)
σ_rob  = 1.4826 × MAD                // 与 1.4826×MAD 的关系：当 X 正态时 σ_rob ≈ 标准差
percentile = 当前值在 X 中的百分位（百分位 = 超过当前值的比例，用逆 ECDF / PERCENT_RANK；等价于「当前处于自身历史 Pxx」）
dev_sigma = (current - median) / σ_rob   // 偏离稳健 σ；|dev_sigma| 越大偏离越远
```

**关键细节**：
- 只用 **success / 非空** 日；`X` 为空或有效样本 `< 3` 天 → `baseline.supported=false`，`note` 说明样本不足，**不产出判断**。
- `σ_rob == 0`（所有日相同）→ `dev_sigma` 记为 `0`（无偏离），不除零。
- 报告当前值用 `current` 字段保留；`median/std_sigma/percentile/dev_sigma` 作为其相对参照。
- 「当前」可以是整周期（week 汇总）或单日；须在 `baseline.window` 标注（如 `current_window="week"`）。
- **对照现有看板**：看板用 `usage_daily` 30 天 P90（`MyPercentiles`），本设计用 28 天 `median+1.4826×MAD`；
  两者**口径不同**，报告必须标注使用了哪种（见 §6 差异#3）。

```jsonc
"baseline": {
  "metric": "est_cost_usd",          // 被衡量指标
  "window_days": 28,
  "sample_days": 24,                 // 有效样本天数
  "supported": true,
  "current": 12.34,
  "median": 8.10,
  "mad": 2.10,
  "sigma_rob": 3.11,
  "dev_sigma": 1.36,                 // (current - median)/sigma_rob
  "percentile": 0.81,                // 当前处于历史 P81
  "trend": "up" | "flat" | "down",   // 由 current vs median + dev_sigma 符号判定
  "note": "28 天稳健基线，仅 active 日；样本不足时 supported=false"
}
```

### 5.2 同岗位使用效能分位（匿名，不跨岗位裸成本排名）

设计铁律：**只给「同岗位」匿名分位，不做跨岗位裸成本排名**（`docs/ua-analysis-feedback-research.md`、`docs/user-behavior-analysis-ux-ia.md`）。

**定义对标池**（按优先级回退，首个满足 `pool_n >= min_sample` 的适用）：

| 优先级 | 池 | 说明 |
|---|---|---|
| 1 | 同**工作室** + 同 **job_category** | 主池 |
| 2 | 全公司**同 job_category**（跨工作室） | `pool_n < 5` 时回退 |
| 3 | 同工作室（不限岗位） | 仍 `< 5` 再回退 |
| 4 | 全公司（不限岗位，跨岗） | 兜底；**此级仅未命中则不输出 rank**（避免跨岗位比较） |

- `min_sample = 5`（样本 < 5 视为不可信，逐级回退；**兜底级仍 < 5 则 `supported=false`，不产出 rank**，与反馈研究「分位样本过少仍展示」为违规一致）。
- **排除项**：个人维度排除 `key_type='service'`；单元维度把 Service Key 单列不并入成员池。
- **分母为 0 / 空值不参与**：指标不可计算者（如 `est_cost_usd=0` 时 `cost_per_1k` 无定义、`tokens=0`）从池中剔除。
- **指标**（同岗位使用效能）主用 `cost_per_1k_tokens`（每千 token 成本），辅以 `cache_hit_rate`、`calls_per_active_day`、`unique_models`：
  - `cost_per_1k_tokens = est_cost_usd / (total_tokens/1000)`（`total_tokens>0` 才计算）
  - 个人/成员在报告期内（`period`）取值；岗位成员来自 `/org/*` 的 `job_category`，效能指标来自 `/usage/dashboard/users`。
- `job_category` 取值顺序：`users.job_category` → 空则 `job_family` → 空则 `job_title` → 空则该用户不入任何岗位池（标 `job_category=""`，只进兜底池或不产出）。

```jsonc
"peer": {
  "metric": "cost_per_1k_tokens",
  "pool_type": "studio+job_category",   // 实际命中层级
  "pool_n": 8,
  "sample_n": 7,                        // 有效（分母>0）样本
  "supported": true,
  "current": 0.82,                      // 本人该指标值
  "percentile_rank": 0.75,              // 本人处于同岗位前 75%（值越高成本越高）→ 措辞「高于同岗位75%的同仁」
  "band": "P50-P75",
  "p25": 0.4, "p50": 0.6, "p75": 0.9, "p90": 1.2,
  "vs_peer": "+37%",                    // (current - p50)/p50（误差范围）
  "anonymized": true,
  "note": "匿名分位，不指向具体人、不跨岗位排名；sample<5 已回退"
}
```

> 效能越高**成本越低**（`cost_per_1k` 越低越好），因此 `percentile_rank` 高 → 成本高于同岗位 → 措辞用「偏贵」。
> 若主指标为效率类（cache_hit_rate 越高越好），`percentile_rank` 方向相反，`expression` 用「偏低/偏省」措辞，须在 `metric_direction`（`cost` vs `efficiency`）标注。

### 5.3 使用效能/效率派生指标（判断层，全部可复算）

| 指标 | 公式 | 空值/零分母 |
|---|---|---|
| `success_rate` | `success_count / req_count * 100` | `req_count=0` → 不产出（判层标注 `n/a`） |
| `error_rate` | `error_count / req_count * 100` | 同上 |
| `daily_avg_calls` | `req_count / active_days` | `active_days=0` → n/a |
| `cost_per_1k_tokens` | `est_cost_usd / (total_tokens/1000)` | `total_tokens=0` → 剔除参与分位 |
| `cache_hit_rate` | `cached_tokens / prompt_tokens * 100` | `prompt_tokens=0` → n/a |
| `output_ratio` | `completion_tokens / (prompt_tokens - cached_tokens)` | 分母 ≤0 → n/a |
| `reasoning_share` | `reasoning_tokens / completion_tokens` | `completion_tokens=0` → n/a |

> 这些比率既不是接口裸值、也不是措辞，属**判断层**；算法一致（公式固定），可复算、可对账。零分母一律 `n/a`，**不参与**后续基线/分位/异常计算（避免污染）。

### 5.4 A1–A5 分钟窗口异常规则

> A1–A5 为**分钟级滑动窗口**上的行为异常，比日聚合粒度更细，用于识别「短时失控」。与老文档里另一套
> A1–A8（`user-behavior-analysis-backend-architecture.md`、`docs/ua-behavior-analysis/product-strategy-recommendation.md`）
> **不是同一套**：老 A1–A8 是日/周级规则型异常（成本突增/出错率/预算…）；本工具 A1–A5 是**分钟窗口**异常。
> 本文按任务给定语义与 A1 示例定义 A1–A5，阈值**参数化**、**默认值待对账**；T4 在真实数据上校准命中率/误报率。

**通用口径**：
- 时间基准：**Asia/Shanghai**；`ts` 为 UTC，必须先 `ts AT TIME ZONE 'Asia/Shanghai'` 后切片。
- 切片对象：**单个 `key` 或 `user`**（按报告级别）；异常主体区分 `key_type`——**A5 只评估个人维度**（排除 Service Key，避免批处理误报）；
  A1–A4 可评估个人/单元（但须在 `evidence` 标注 key_type，且单元维度把 Service Key 单列）。
- 每张卡输出：`{rule, name, window, actual, threshold, baseline, impact_cost_cny, evidence[], confidence, action}`。
- `evidence` 为**事实引用**（窗口内的样例调用/聚合值），无依据不下结论（T4 可解释性要求）。
- 阈值全部来自 `alert_rules` 种子（`A1..A5` 前缀）或工具配置 `usage_report.anomaly_thresholds`，**不写死代码**；实测命中率过低/过高时调阈值而非删规则。

| 规则 | 名称 | 语义 | 触发条件（默认） | 判定层 |
|---|---|---|---|---|
| **A1** | 程序失控 | 单 key/user 短时高频近等间隔循环调用（疑似死循环/失控脚本） | 在 **10 分钟**滑动窗口内：`window_call_count > max(60, 个人/主体 P99_10min)` **且** `median(Δts within window) < 10s` **且** 连续命中 ≥ **15 分钟** | 判定 |
| **A2** | 成本燃烧 | 单主体的成本在短窗口内异常飙升（远超自身历史稳健基线） | **30 分钟**窗口：`window_cost > max(baseline_median + 5×σ_rob, 个人/主体 P99_30min)` **且** `window_cost ≥ 绝对下限`(默认 ¥50 对应 USD 或 8 USD) **且** 连续 ≥ **1 小时** | 判定 |
| **A3** | 重试循环 | 同一主体/模型在短窗口内大量失败 + 重复重试（429/5xx/上游错） | **5 分钟**窗口：`error_rate = 失败/总调用 ≥ 50%` **且** 同 `user+model+group_name+inbound_endpoint` 重试次数 ≥ **10** **且** `median(Δts) < 3s` | 判定 |
| **A4** | 超长上下文 | 调用携带异常巨大上下文（体量/效率问题） | **15 分钟**窗口：`calls with prompt_tokens > max(个人/主体 P99_prompt_28d, 模型上下文上限×0.9)` 的占比 ≥ 30% **且** 绝对次数 ≥ 5 | 判定 |
| **A5** | 非工作时段 | 个人在非工作时段（组织核心时段外）发生大量调用 | 报告期内：`off_hours_share = 非工作时段调用/总调用 > baseline_share + 3×σ_rob` **且** 绝对调用数 ≥ 20 **且** 个人维度（排除 Service Key） | 判定 |

**A5 非工作时段定义（默认，待对账）**：
- 组织核心工作时段默认 `Asia/Shanghai 周一–周五 09:00–19:00`；非工作时段为其补集（含 22:00–08:00 深夜档）。
- 用**相对基准**对齐铁律「不画红绿灯」：`off_hours_share` 与该主体自身 28 天 `off_hours_share` 稳健基线（`median + 3×σ_rob`）比较，
  而非绝对阈值；避免对本来就晚班的岗位（如运维/国际协作）误报。
- 方向判定：`off_hours_share > baseline` 才算法定为「反常的深夜/非工作时段」；若该主体历史即常深夜（属正常习惯），不命中。

**A1 示例（任务给定）解析**：`A1 = 10分钟[窗口调用数] > max(60, 个人P99_10min) 且 median(Δts in窗口) < 10s 且 连续 ≥ 15 分钟`。
实现上理解为「近等间隔、高密度、持续的调用丛」= 失控循环；`60` 为绝对下限（防止低用量用户 P99 过小被误报），
`个人P99_10min` 为该主体自身在基线窗口内 10 分钟调用数分布的第 99 分位（自适应阈值）。

### 5.5 成本 CNY 换算点

- **唯一事实源**：`est_cost_usd`（USD，`decimal(18,6)`），来自服务端聚合；**绝不**在 fact 层改币种。
- **CNY 在 judgment 层派生**（换算，不构成判定）：

```
est_cost_cny = est_cost_usd × usd_cny_rate
```

- **比率来源**（三选一，须显式记录）：① 工具配置 `usage_report.usd_cny_rate`（默认，含 `rate_at`）；② 环境变量
  `USAGE_REPORT_USD_CNY_RATE`；③ 实时汇率接口（需网络，记 `rate_source`）。
- **必须携带 `rate / rate_source / rate_at`**，确保可复算、可审计；报告尾部注明「换算比率 ¥7.20/$（来源×，时间×）」。
- 铁律对齐：`docs/guardrails.md` A3「价格单位统一」——**USD 是唯一存储/事实单位**，CNY 仅展示（report_currency）。
- 引擎内所有成本异常（A2 成本燃烧、基线成本、per_capita 成本）**内部一律用 USD** 判定，仅在**输出渲染**时转 CNY，避免判断层货币混用。

---

## 6. 与现有看板口径核对（对账基准）

下表是「现有看板」已实现口径（源码 `internal/observability/*`）。报告工具**必须**与之**逐项一致**；差异项必须在 `meta.qa.口径_alignment` 显式记录并对账。

| # | 口径 | 现有看板实现 | 报告工具要求 | 一致？ |
|---|---|---|---|---|
| 1 | 成功判定 | `status = 200`（`COUNT(*) FILTER (WHERE status=200)`） | 同 | ✅ |
| 2 | 时区 | 小时分布/切片用 `ts AT TIME ZONE 'Asia/Shanghai'`；`date(ts)` 天粒度在同一时区语义 | 同（所有窗口切片先转 Asia/Shanghai） | ✅ |
| 3 | 个人/岗位人均排除 Service Key | `WhereExcludeServiceKeyLogs`（`COALESCE(NULLIF(key_type,''),'personal') != 'service'`） | 个人维度、岗位池、member 人均均排除 | ✅ |
| 4 | 部门 Service Key 单列 | 仅 `key_type='service'` 且归属当前部门；`DashboardUsersEnhanced` 的 `ServiceCostEstimate` 回填逻辑 | 单元维度 `member` 与 `service_key` 分离，`member+service` 合计对账 | ✅ |
| 5 | dept_id 归一化 | `LEFT JOIN departments d ON (d.dept_id = cl.dept_id OR d.id::text = cl.dept_id)`，取 `COALESCE(d.dept_id, cl.dept_id)` | 同（拉数时带 JOIN；若接口已归一则直接复用） | ✅ |
| 6 | studio 映射 | `dept_studio_map dsm ON dsm.dept_id = COALESCE(d.dept_id, cl.dept_id)` | 同 | ✅ |
| 7 | 成本/Token/调用 | `SUM(est_cost_usd)`、`SUM(prompt/completion/..._tokens)`、`COUNT(*)`；`total_tokens` = prompt+completion | 同（只回填） | ✅ |
| 8 | 环比期间 | 上一等长期间 `[from - duration, from)` | 同（recon/看板 B10 修复） | ✅ |
| 9 | 全站 P90 标签阈值 | `/my/usage/percentiles`：30 天 `usage_daily` 用户级 P90 | 报告若引用 P90 标签须与此一致 | ✅（但见 #3） |
| 10 | **28 天基线** | 看板**未提供**（看板用 30 天 P90） | 本工具**新增**：`median+1.4826×MAD` 稳健基线 | ⚠️ 新口径，须与看板 P90 标注区别 |
| 11 | 同岗位分位 | 看板**未提供**（`/my/usage/percentiles` 是全站非岗位） | 本工具新增：studio+job_category 匿名分位，样本<5 回退 | ⚠️ 新口径，非看板现有 |
| 12 | 成本币种 | `est_cost_usd`（USD） | 报告展示可加 CNY（`USD × rate`），USD 为事实源 | ✅（CNY 为派生展示） |
| 13 | 程序流量不吞个人行为 | 个人/岗位人均排除 Service Key；平台维度单列程序调用 | 同 | ✅ |

**对账注意**：
- **差异#10/#11 是新增口径**，看板没有；报告必须**标注来源**（写成「28 天稳健基线」「同岗位匿名分位」），
  避免与看板「30 天 P90」「全站 P90」混淆。T4 对账时逐项核对数值一致性（调用/成本/Token），并验证这两项新口径在真实数据上可复算。
- **个人裸成本/跨岗位排名一律不做**（铁律）；只出「同岗位 % 分位」+「相对自身基线」，并带 `note` 说明非问责。
- 单元维度 `member + service_key` 应等于该单元全部调用（对账钩子）；若不等，说明程序流量归属或 dept 归一有误，需在报告 `qa` 标记。

---

## 7. 渲染约定（仅供 T3，勿在模板二次计算）

- MD 与 HTML 模板**只读** `schema_version` 的 JSON，直接取 `expression.headline/insights/suggestions` + `judgment.anomalies[]` + 关键 fact，**不做任何数学计算**。
- 异常卡渲染 `rule + name + window + threshold/actual/baseline + impact_cost_cny + evidence`，命中即**带证据**、无证据则输 `evidence:[]` 并 `note`。
- 涉及人名的个人级一律只显示「我的/你」，单元级显示汇总；岗位分位始终写「同岗位 Pxx」而不点名。
- CNY 显示统一注明换算比率（`rate/source/at`）。
- `expr` 措辞与判定解耦：`headline` 引用 `expr` 生成措辞时**不得**新增数字；`insights[]` 里每条类似「本周成本较你 28 天基线 +37%」的**数字必须来自 judgment**已存在字段，不允许模板自己算。

---

## 8. CLI 契约（呼应 T3，便于对齐）

```
usage-report --level personal|department|studio \
             --period week|month --from YYYY-MM-DD --to YYYY-MM-DD \
             --scope <user_id|dept_id|studio_id> \
             --output out/report.md --format md|html \
             [--base-url https://gateway] [--token $TOKEN] [--usd-cny-rate 7.20]
```

- 一次运行产一个 `level` 的报告 JSON + MD/HTML；工具不写 DB、不改网关/控制面运行时（独立 `tools/usage-report` 入口，不影响 `make build`）。
- `--format` 只影响渲染，不影响 JSON 事实层。

---

## 9. 待办 / 风险（供 captain 与 T4）

- 🔴 **A1–A5 精确阈值**：本文给的是参数化默认值（A1 按任务示例、A2–A5 由语义推导）；须以权威文档/真实数据校准，T4 校验命中与误报。
- 🔴 **CNY 汇率取值**：默认配置值 `7.20`，需确认源与时效；正式化建议走汇率接口或引 `rate_at`。
- 🔴 **job_category 获取链路**：`/org/*` 权限（`PermMemberView/PermOrgUserSearch`）是否与 `/usage/*`（`PermUsageViewScoped/All`）同账号具备；不足则降级 `peer.supported=false`。
- 🔴 **单元维度程序流量对账**：`member + service_key` 对账钩子要在 T4 用真实数据验证，杜绝程序流量吞进成员或漏统计。
- 本设计**未引入**请求正文/内容语义/PII（只做元数据行为），符合 t1 红线。

---

## 10. 对账修正说明（T7 · 对 t3 模块化实现 vs 本契约 & impl-notes）

> 目标：以 `report-schema.md` 为主、`recon.md` + `usage-report-impl-notes.md` 为辅，逐字段对账模块化实现
> （`internal/observability/report/` 纯库 + `tools/usage-report/` 入口），修正与契约/口径的漂移。
> 实现已从早期「平铺占位」重构为三层纯库；T7 在**稳定后（go build/vet 全绿）**的代码上做增量对账修正。

### 10.1 字段/阈值：由「占位」改为「契约/impl-notes」

| 修正点 | 占位(早期实现) | 修正后(契约/impl-notes §3) | 位置 |
|---|---|---|---|
| A1 持续窗口 | `streak >= A1Sustain/A1Window`（整除=1，放宽为单窗即命中） | `streak >= A1SustainMinutes`（1 分钟粒度滑动窗，连续≥15min 才算） | `derive/anomalies.go A1` |
| A2 计数 | `MatchedCount = int(cost/1)`（把成本当调用数） | `MatchedCount = countIn(best_window)`（窗口内调用数） | `derive/anomalies.go A2` |
| A5 阈值 | 单一 `A5HardCapChina=¥100` 兼作 floor+cap | 拆 `A5FloorCNY=¥100`（max(同岗P99,¥100) 的 floor）与 `A5HardCapCNY=¥1000`（HARD_CAP_A5，独立触发→P1） | `metric.go RuleSet` + `anomalies.go A5` |
| A5 命中单位 | `ActualValue=offShare`（占比）配 CNY 阈值（单位不一致） | `ActualValue=offCost(USD)`，阈值同 USD；占比作 dimension 佐证 | `anomalies.go A5` |
| A5 降解链/备案 | 无 | 标注降解链终点「仅floor」+ 阶段二补备案豁免（带 until） | `anomalies.go A5` Note |
| A4 有效输入预算 | —— | `ModelContextBudget=context_length−max_output_tokens`（组内取最小正值）；未录模型回退 `A4MinPromptAbsolute` 并记来源 | `client.ModelContextBudget` + `anomalies.go A4` |
| A3 段口径 | 按状态码分组 | `segment=(key_id,model)`，`req≥20 ∧ fail_rate≥30% ∧ 单一 status 占失败≥80% ∧ failed≥6`，失败=`status≥400`，区分上游(5xx)/客户端(429/400) | `anomalies.go A3` |

### 10.2 阈值参数化（全部集中 `RuleSet/DefaultRuleSet`，避免硬编码）

- A1：`A1WindowMinutes/A1SustainMinutes/A1MinCallsAbsolute/A1MaxMedianIntervalS`
- A2：`A2WindowMinutes/A2BaselineMedianMultiple/A2AbsMinUSD/A2HardCapCNY`
- A3：`A3WindowMinutes/A3MinSegmentReq/A3FailRateThresh/A3DominantFailShare/A3MinFailed`
- A4：`A4WindowMinutes/A4NearLimitRatio/A4MinNearLimit/A4MinPromptAbsolute`
- A5：`A5OffHourStart/A5OffHourEnd/A5MinCallCount/A5FloorCNY/A5HardCapCNY`（新增 `A5FloorCNY/HardCapCNY`；删除废弃的 `A5BaselineMultiple/A5AbsBaselineShift`）
- 汇率：`USDCNYRate=7.2`（`--fx-rate` 覆盖）

### 10.3 `total_tokens` 同源与对账基准（impl-notes §6）

- `total_tokens = prompt + completion + reasoning`；`cached_tokens/cache_write_tokens` 为 `prompt` 子集，**不计入**（单列供 `cache_ratio`）。全链路一致：`DailyMetric.TotalTokens`、`aggregateDaily`、`ToCallEvent`、`PersonalFact/UnitMember.TotalTokens`。
- **对账基准**：新增 `client.FetchDashboard`（调 `GET /api/v1/usage/dashboard`），`main.go` 用 `reconcileWithDashboard` 把报告 total_tokens 与 GetDashboard 比较并写入 `QA.Alignment`（均=`SUM(prompt+completion+reasoning)`）。
- **禁用混拼**：本工具从未使用 `/usage/dashboard/users*`/`DashboardUsersEnhanced` 的 `SUM(call_logs.total_tokens)` 存储列；对账仅与 GetDashboard 一派互证。
- personal 范围 GetDashboard(`scope=user`) 会包含该用户 service key 用量，而 personal 画像排除 `key_type='service'` → 对账差异≈service key 贡献，已在 QA 注明(预期差异)。

### 10.4 排除口径（impl-notes §5）

- 成功=`status=200`（`aggregateDaily` 用 `c.Status==200`）。
- 个人画像/岗位人均排除 `key_type='service'`：personal 级别默认 `key_type=personal` 下发 `/usage/logs`（④），并发层再兜底剔除 service；同岗分位（`cohort.go`）同样跳过 service。
- 部门 Service Key 块仅 `=service` 且归属部门（`buildUnitFact` 将 service 单列 `UnitService`）。

### 10.5 CNY（impl-notes §5）

- `CNY = est_cost_usd × usd_cny_rate`；`Meta.Currency` 记录 `{base:"USD", report_currency:"CNY", usd_cny_rate, rate_source:"config", rate_at}`；`Efficiency.CostUSDCNY` 同口径；A2/A5 命中时记录 USD 与 ¥。

### 10.6 已知残留限制（阶段一，已在代码 Note/QA 标注）

- **28 天基线未真正物化**：`buildBaselineSeries` 目前取本期 metrics 序列作近似（工具未拉基线窗数据）；impl-notes §3「参考窗=事件日前 28 完整自然日、基线物化」待工具侧补拉期前 28d 数据后达完全一致。
- **A4 分支2（IO 比同场景 P99）**：依赖 `scene` 埋点，未埋点前禁用/灰度。
- **A5 同岗位 P99 + 降解链**：需每用户非工作成本（t8 同岗分位接入），阶段一回退「仅floor」。
- **A4/A5 模型上下文**：`model_catalog` 端点已用 `/model-catalog/search`，未命中回退保守 `A4MinPromptAbsolute` 并记录来源。

### 10.7 校验

- `go build ./internal/observability/report/... ./tools/usage-report/...` 通过；`go vet` 通过；`go test ./internal/observability/report/...` 通过（`derive_test` 覆盖 A5/A4 命中、MD/HTML 渲染、建议生成）。

### 10.8 花名册/同岗分位实现 vs org-roster.md 核验（T7 范围补充）

> 对象：`tools/usage-report/cohort.go`（poolFor/jobSlot/buildCohort）与 `client/usage_client.go`。
> 依据：`docs/org-roster.md`（t5）。

**一致（已核验）：**
- active 部门枚举（`FetchDepartments(ctx,"active")`）；分页按 `total` 推进（`/org/departments` 不回显 page/page_size，依 `total`）。
- studio 解析优先级：`departments.studio_id` → `dept_studio_map` → ""（§3-C）。
- job_category 取值链：`jobSlot`=job_category→job_family→job_title→""（§3-C）。
- user_id 去重（roster map 以 user_id 为键）；样本<5 回退链 + derive 判 `supported=false`。
- 403/404/429 记 `skipped_depts`（单部门失败不中断整次运行，§3-B）；429/5xx 指数退避重试 3 次（§4.5，`isRetryable` only 429/≥500）。
- admin JWT 仅内存/env（`USAGE_REPORT_TOKEN`），无 roster.json/敏感写盘（§6）；`meta.qa` 标注 `users.dept_id IS NULL` 覆盖缺口（§4.1）。

**修正的漂移（1 处）：**
- `poolFor` 回退链缺少 §4.2 第 4 级「全公司（任意岗位）」：原「同 studio（任意）」后直接返回，<5 则（不再尝试公司任意）交 derive 判不支持。已补第 4 级（同studio+同category → 全公司同category → 同studio任意 → 全公司任意）。

**可接受偏差（文档建议/理论不出现，非漂移）：**
- §3-C user_id 冲突消解（保留 `dept_source='manual'`/`has_conflict` 那次 + conflict 标记）未实现 —— SQL 保证每用户仅属一部门（§1.1），理论不发生。
- `roster.json` 本地缓存（TTL≈24h）未实现 —— 每次运行重取，正确性不受影响（去缓存优化）。
- `coverage.{dept_count,member_count,skipped_depts}` 结构化未完全落地 —— skipped 数经 `NotComputed("同岗分位-覆盖")` 透出（doc 为可选缓存格式，非报告必需）。

**校验：** `go build/vet` 全绿；`go test -count=1 ./internal/observability/report/... ./tools/usage-report/...` 通过。

### 10.9 对账点（对现有 GetDashboard）与待对账项 🔴

**对账点（已实现，`tools/usage-report/main.go reconcileWithDashboard`）：**
- 以 `GET /api/v1/usage/dashboard`（GetDashboard，PermUsageViewScoped）为 total_tokens 对账基准；报告事实层 total_tokens 取自 `/usage/logs` 逐调用聚合（=prompt+completion+reasoning）。
- 二者同源公式（`SUM(prompt+completion+reasoning)`，不含 cached）；对账比较 total_tokens/call_count，差异写入 `Meta.QA.Alignment`。
- **预期差异**：personal 范围 GetDashboard(`scope=user`) 会包含该用户 service key 用量，而 personal 画像排除 `key_type='service'` → 对账差≈service key 贡献（已注明，非口径错误）。
- 全程**未混拼** `/usage/dashboard/users*`/`DashboardUsersEnhanced` 的 `SUM(call_logs.total_tokens)` 存储列。
- 历史口径漂移记录：aggregation 层从 `prompt/completion/reasoning` 分列现算；`collector.go:666` 证明 `cached/cache_write` 是 `prompt` 子集，故不计入。

**🔴 待对账项 / 风险（阶段一，均在代码 Note/QA 标注）：**
1. **28 天基线未真正物化**：`derive/buildBaselineSeries` 目前用本期 metrics 序列作近似（工具侧未拉「事件日前 28 个完整自然日」基线窗数据）——偏离 impl-notes §3「基线必须物化、参考窗钉死为日前 28 完整自然日」。**建议**：工具侧补拉基线窗聚合（`from-28d ~ from-1d`）+ `ReportInput` 增 `BaselineMetrics` 字段。
2. **A4 分支2（IO 比同场景 P99）**：依赖 `scene` 埋点，未埋点前默认禁用/灰度（impl-notes §3 允许）；若启用需补 `scene` 字段接入。
3. **A5 同岗位 P99 + 岗位降级链**：需每用户非工作成本（t8 同岗分位→同日内分桶），阶段一按降解链回退「仅floor ¥100」；`HARD_CAP_A5=¥1000` 独立触发→P1。
4. **admin JWT 权限**：需 `IsGlobal` 且具 `PermMemberView/PermUsageViewAll` 的 admin token 才能拉全量部门成员与用量；否则 `peer.supported=false`/局部范围（org-roster §7）。
5. **`/org/departments` 不回显 page/page_size**：已按 `total` 推进分页（无误，但要留意大部门分页上限）。
6. **无部门用户覆盖缺口**：`users.dept_id IS NULL` 用户不入 roster → 无岗位分位；报告 `meta.qa` 已标注（org-roster §4.1）。
7. **A4 模型上下文上限来源**：`/model-catalog/search` 未命中时回退 `A4MinPromptAbsolute`（默认 10 万）并记录来源；阶段一未接完整 catalog 同步。
8. **总量口径（provider 语义）**：若真实数据确认 `reasoning` 实际已被 `completion` 包含（部分提供商），则 `summary` 单列 `reasoning_tokens` 展示而不并入 total，避免重复计数（以对账实测为准，口径变更需记 reason，impl-notes §6）。

**✅ 已满足（复确认）：** success=`status=200`；失败 A3=`status≥400`；`ts AT TIME ZONE 'Asia/Shanghai'`（A5/小时）；personal 排除 `key_type='service'`；部门 Service Key 单列且归属部门；`dept_id` 归一（工具侧按 dept_name/dept_id 匹配）；CNY=`est_cost_usd×rate` 且记录 rate/source/at；渲染三层（facts/judgment/expression）+ 表达层幻觉校验（render_html `validateExpression/sanitize/containsBareDigit`）。
