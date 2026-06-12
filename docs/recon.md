# LLM Gateway · 阶段一离线用量周报——数据面摸底（recon）

> 目标：为「离线用量周报工具」建立数据面地图。工具**不直连 DB，只走 HTTP**（`/api/v1/usage/*`、`/my/usage/*`），复用现有三层聚合 SQL 的**产物**，并在其上派生出周报特有指标（28 天基线 / 同岗分位 / A1–A5 章节），最终装配 `report JSON` 再渲染 MD/HTML。口径必须与现有看板**对账一致**。
>
> 工作分支：`codex/from-main`（worktree `.worktrees/from-main`）。
> 代码来源文件：
> - `internal/observability/model/{usage_daily,user_daily_stats,model_daily_stats,call_log}.go`
> - `internal/observability/repository/usage_daily_repo.go`
> - `internal/observability/service/rollup_service.go`
> - `internal/observability/handler/{usage_handler,dashboard_enhanced}.go`
> - `internal/controlplane/router/router.go`（`/api/v1/usage/*`、`/my/usage/*` 注册处）
> - `internal/orgservice/rbac/policy/usage_policy.go`、`actor.go`（权限/范围）
> - `internal/auth/model/user.go`（岗位字段）、`internal/orgservice/model/models.go`（花名册岗位）

---

## 0. 结论摘要

1. 数据面被「控制面 control-plane `:8081`」独占，全部挂在 `adminGroup := r.Group("/api/v1")`（`JWTAuth()`）之下；权限分两档：`PermUsageViewAll`（全量）与 `PermUsageViewScoped`（按 actor 数据范围）。
2. 三层聚合表已齐备：`call_logs`（原始）→ `usage_daily`（key/dept/studio/model 日粒度）→ `user_daily_stats`（用户日粒度）+ `model_daily_stats`（模型/工作室日粒度）。
3. 关键口径（一致化后直接抄进离线工具 SQL）已确认：成功=`status=200`；小时粒度=`ts AT TIME ZONE 'Asia/Shanghai'`；个人画像/岗位人均**排除** `key_type='service'`；部门 Service Key 块**只算** `key_type='service'` 且归属当前部门；dept_id 归一 `d.dept_id = cl.dept_id OR d.id::text = cl.dept_id`。
4. **报告所需大部分「现成」原始/聚合字段均可由 `/api/v1/usage*` 返回**；但「周报特有派生指标」——28 天基线、同岗分位、A1–A5 章节——**现有接口不直接提供**，需离线工具在上游数据之上二次聚合（Windows 端做百分位/环比/同岗分组）。
5. **接入注意**：做「全公司」周报需要具有 `PermUsageViewAll` 的 admin token（调用 `/api/v1/usage` 或 `/api/v1/usage/logs`）；`/api/v1/usage/dashboard/*` 是 `PermUsageViewScoped`，**返回结果会随 caller 的 actor 范围收窄**，不能当全局口径用（见 §4）。

---

## 1. 端点总览

### 1.1 鉴权与权限底座

- `adminGroup` 统一挂 `JWTAuth()` + `AuditMiddleware(实际仅在写操作路径生效，读接口走中间件不落审计)`。
- 权限粒度由 `guard(perm)` 中间件注入 `Actor`（`rbac.GetActor(c)`），`Actor` 含 `IsGlobal` / `StudioIDs` / `DeptIDs`。
- `UsagePolicy.EffectiveScope` / `QueryScope` 决定数据可见范围：
  - `admin`（IsGlobal）：无过滤。
  - `studio_admin`：`call_logs.studio_id IN (actor.StudioIDs)`。
  - `dept_admin`：`call_logs.dept_id IN ? OR dept_id IN (SELECT id::text FROM departments WHERE dept_id IN ?) OR user_id IN (SELECT u2.id::text FROM users u2 JOIN departments d2 ON d2.id=u2.dept_id WHERE d2.dept_id IN ?)`。
  - 普通 user：`call_logs.user_id = actor.UserID`。

> 也就是说 `PermUsageViewScoped` 的接口**结果随权限收窄**；`PermUsageViewAll` 才是全局。离线周报若要全公司口径，必须用 admin 且走 `PermUsageViewAll` 端点。

### 1.2 `/api/v1` 下用量相关端点（JWT）

| 方法 | 路径 | 权限 guard | Handler | 用途 / 返回 |
|------|------|-----------|---------|------------|
| GET | `/api/v1/usage` | `PermUsageViewAll` | `usageH.Query` | 聚合用量，`group_by=day\|week\|month\|model\|dept\|key`，含 `total` 行与 `total_count/page/page_size` |
| GET | `/api/v1/usage/logs` | `PermUsageViewAll` | `usageH.Logs` | 调用明细分页（含 `key_type`、`status`、成本、延迟等行级字段） |
| GET | `/api/v1/usage/logs/:request_id/trace` | `PermUsageViewAll` | `usageH.Trace` | 单条请求完整 trace |
| GET | `/api/v1/usage-logs` | `PermUsageViewAll` | `usageH.Logs` | `/usage/logs` 的别名（历史兼容） |
| GET | `/api/v1/usage/options` | —（JWT 内，无 guard） | `usageH.Options` | 返回 provider / dept / studio 下拉枚举 |
| GET | `/api/v1/usage/dashboard` | `PermUsageViewScoped` | `usageH2.GetDashboard` | 分层看板 `scope=company\|studio\|dept\|user`，summary+breakdown |
| GET | `/api/v1/usage/dashboard/trend` | `PermUsageViewScoped` | `usageH2.DashboardTrend` | 按日 trend（calls/errors/tokens/cost） |
| GET | `/api/v1/usage/dashboard/models` | `PermUsageViewScoped` | `usageH2.DashboardModelsEnhanced` | 模型 Top N（含 avg/p90 duration、error_rate、is_long_tail） |
| GET | `/api/v1/usage/dashboard/users` | `PermUsageViewScoped` | `usageH2.DashboardUsersEnhanced` | 用户排行（含 cost_ratio、cost_change_pct、cost_per_1k_tokens、error_rate、top_models、vs_avg、summary） |
| GET | `/api/v1/usage/dashboard/user-top-models` | `PermUsageViewScoped` | `usageH2.DashboardUserTopModels` | 每用户 Top N 模型 |
| GET | `/api/v1/usage/dashboard/user-detail` | `PermUsageViewScoped` | `usageH2.DashboardUserDetail` | 单用户详情（summary + daily_trend + model_distribution + hourly_distribution） |
| GET | `/api/v1/usage/dashboard/model-detail` | `PermUsageViewScoped` | `usageH2.DashboardModelDetail` | 单模型详情（summary + daily_trend + user_distribution） |
| GET | `/api/v1/usage/scoped/logs` | `PermUsageViewScoped` | `usageH2.ScopedLogs` | 按角色自动注入 scope 的明细（元数据，无 request/response 大字段） |

> `/usage/dashboard/*` 与 `/usage/scoped/logs` 用独立的 `usageH2`（`NewUsageHandler(NewRollupService, db)`）在 router 末尾注册，与上面 `usageH`（`usage/logs`、`usage`、`options`、`my/*`）是**两个实例**，但同一 DB。

### 1.3 `/api/v1/my/usage/*` 当前用户隔离

| 方法 | 路径 | Handler | 说明 |
|------|------|---------|------|
| GET | `/api/v1/my/usage` | `usageH.MyQuery` | 当前登录用户的聚合；`user_id` 取自 JWT（`c.GetString("user_id")`）；`group_by=day\|model\|key\|hour` |
| GET | `/api/v1/my/usage/percentiles` | `usageH.MyPercentiles` | 全站用户近 30 天 P90 阈值（daily_avg_calls / cache_hit_pct / output_ratio / unique_models / reasoning_share），Redis 缓存 1h |
| GET | `/api/v1/my/usage/logs` | `usageH.MyLogs` | 当前用户明细（`UserID=userID` 隔离） |
| GET | `/api/v1/my/usage/logs/:request_id/trace` | `usageH.Trace` | 单条 trace |

> `MyPercentiles` 已被 Redis 缓存（key `usage:percentiles:p90:30d`），且其 SQL 直接对 `usage_daily`（未经 service-key 排除！）按 `user_id` 分组算 P90——**与个人画像排除 service 的口径不完全一致**，离线工具引用时要注意：它是「全站用户」百分位，不是「同岗」百分位。

### 1.4 `/api/v1/my/usage` 与 `/api/v1/usage` 的 group_by 能力差异

| group_by | `/api/v1/usage`（Query） | `/my/usage`（MyQuery） |
|----------|-------------------------|------------------------|
| hour | ❌（call_logs 路径支持，但 Query 默认 path 只有 long-range 走 usage_daily，hour 在 queryFromUsageDaily 无分支→实际只走 call_logs 且需无过滤+>90d 才走 usage_daily） | ✅（`ts AT TIME ZONE 'Asia/Shanghai'`，用于「出没时段」画像） |
| day / week / month | ✅ | ✅（day） |
| model | ✅ | ✅ |
| dept | ✅ | ❌ |
| key | ✅ | ✅ |
| studio | 支持（`studio_id` 过滤 + model 等） | ❌ |

> 离线工具拉「工作日/小时画像」用 `/api/v1/my/usage?group_by=hour`（个人），或 `/api/v1/usage?group_by=day&...`（day）。周报多为日/周粒度，够用。

---

## 2. 精确口径（对账基线）

以下口径从源码逐条确认，离线工具的新增 SQL / 聚合必须与之完全一致，才能对账吻合。

### 2.1 成功 / 失败 / 错误率

- **成功**：`status = 200`（`COUNT(*) FILTER (WHERE status = 200)` 即 success_count）。
- **失败/错误**：`status != 200`（看板用 `COUNT(*) FILTER (WHERE status != 200)`）。
- **成功率的保守口径**（GetDashboard）：`SuccessRate = (CallCount-ErrorCount)/CallCount*100`，`CallCount==0` 时取 100。
- `total_tokens = prompt_tokens + completion_tokens + reasoning_tokens`（注意**不含** cached_tokens）；usage_daily 表里没有 `total_tokens` 列，需在查询侧 `SUM(prompt_tokens+completion_tokens+reasoning_tokens)`。

### 2.2 时区 / 小时 / 日

- `call_logs.ts` 为 `timestamptz`（UTC 存储）。
- **小时粒度**：统一 `date_trunc('hour', ts AT TIME ZONE 'Asia/Shanghai')`（MyQuery hour 分支、DashboardUserDetail hourly_distribution 都这么写），否则「出没时段」整体偏移 8 小时。
- **日粒度**：`usage_daily` 聚合用 `date(ts)`（写法见 rollup `UpsertRange` / `Run`），取决于 DB 会话时区；看板日粒度在 call_logs 用 `ts::date`。**潜在对账坑**：`date(ts)` 依赖会话 `TimeZone`，若会话不是上海时区，usage_daily 的日切可能与 call_logs 的 `AT TIME ZONE 'Asia/Shanghai'` 日切不一致。离线工具做「对账」建议统一用 `ts AT TIME ZONE 'Asia/Shanghai'` 再取日/小时，或显式 `SET TIME ZONE 'Asia/Shanghai'`。
- 小时分布（画像）：`EXTRACT(HOUR FROM (ts AT TIME ZONE 'Asia/Shanghai'))`。

### 2.3 个人画像 / 岗位人均——排除 Service Key

- 常量：`WhereExcludeServiceKeyLogs = COALESCE(NULLIF(key_type,''),'personal') != 'service'`（无别名）；`WhereExcludeServiceKeyLogsCL`（别名 `cl.`）。
- 所有「成员/用户/岗位人均」聚合都必须带此条件，把程序（Service Key）调用排除，只算人。
- `key_type` 空串归一为 `personal`（`COALESCE(NULLIF(key_type,''),'personal')`）；`KeyTypePersonal="personal"`，`KeyTypeService="service"`。

### 2.4 部门 Service Key 块——只算 service 且归属当前部门

- 常量：`WhereServiceKeyLogsCL = COALESCE(NULLIF(cl.key_type,''),'personal') = 'service'`。
- 在 dept scope 下，成员排行用 `WhereExcludeServiceKeyLogsCL`，Service 块用 `WhereServiceKeyLogsCL`，**二者共用同一 `whereClause`（含部门/模型/状态/端点/工作室安全过滤）**，从而保证 `成员 + 程序 = 总量` 可对账（见 `GetDashboard` 的 `if scope == "dept"` 段）。
- Service 块回填到 `summary.ServiceCostEstimate / ServiceCallCount / ServiceTotalTokens`，并叠加进 `summary.CostEstimate / CallCount / TotalTokens / ErrorCount`。

### 2.5 dept_id 归一化

- **统一 JOIN**：`LEFT JOIN departments d ON (d.dept_id = cl.dept_id OR d.id::text = cl.dept_id)`（`call_logs.dept_id` 历史上混用 SSO `EhrDeptID` 与 `departments.id` UUID）。
- **工作室映射**：`LEFT JOIN dept_studio_map dsm ON dsm.dept_id = COALESCE(d.dept_id, cl.dept_id)`（先归一 dept_id，再取 studio）。
- **用户 → 部门**：`users.dept_id` 为 `*uuid.UUID`；`u.id::text = cl.user_id`。
- 取部门名/ID：`COALESCE(d.dept_id, cl.dept_id)`（group）/ `COALESCE(d.dept_name, cl.dept_id)`（name）。
- 这是所有「按部门/工作室」聚合的对账关键——离线工具做部门维度时必须复刻此 JOIN，否则同一部门会拆成两行。

### 2.6 时间范围 / 分页 / 常量

- `/api/v1/usage`、`/api/v1/usage/logs`、`/api/v1/my/usage*`：解析 `from/to`，支持 `YYYY-MM-DD`（天精度）与 `YYYY-MM-DDTHH:mm`（小时精度）；**范围上限 90 天**。
- 天精度语义：`to` 为午夜时 `ts < to + 24h`（含整天）；小时精度 `ts <= to`。
- 分页：非时间维 `page`/`page_size`（默认 20 或 50）；时间维不分页（返回全量 daily 序列）。
- `usage_daily` 兜底：`Query` 仅在「无精细过滤 && 跨度>90d」时走 usage_daily，否则走 call_logs 实时——**注意：离线工具若要稳定读预聚合成品而非实时 call_logs，不要长时间跨度（>90d 会被路由到 usage_daily，反而更慢），周报周期远小于 90d 没问题**。

### 2.7 三层聚合链路（复用产物）

| 层级 | 表 | PK / 粒度 | 聚合内容 | 说明 |
|------|----|----------|---------|------|
| L1 | `call_logs` | 请求级 | 原始每请求明细（含 token、cost、status、duration、key_type、dept/studio、model） | 事实源（重建/对账唯一事实源） |
| L2 | `usage_daily` | `(stat_date, key_id, dept_id, studio_id, model)` | req/success/prompt/cached/cache_write/completion/reasoning/est_cost | 由 rollup 每小时整日重算 UPSERT |
| L3a | `user_daily_stats` | `(stat_date, user_id)` | 用户日粒度：req/success/error/model_count/tokens/cost/hourly_distribution | **已排除** service（`WhereExcludeServiceKeyLogsCL`）；studio/dept 经 JOIN 取 |
| L3b | `model_daily_stats` | `(stat_date, model, studio_id)` | 模型日粒度：req/success/user_count/dept_count/active_days/tokens/cost/duration 分位 | 同模型跨工作室分行 |

> 「对账」建议：以 `call_logs` 为事实源核对 `usage_daily`/`user_daily_stats`/`model_daily_stats` 的 SUM 是否一致；`user_daily_stats` 已排除 service，`usage_daily` 未排除——两者做对账时要加 service 过滤再去比。

---

## 3. 报告字段 → 数据来源映射

> 说明：A1–A5 章节名与字段名以报告 `report JSON` schema（后续阶段定义）为准；本表映射**可用的构建块**，并标注「现成（接口直接返回 / 表直接有）」还是「派生（需在上游数据上计算）」。

### 3.1 现成字段（接口 / 表直接可得）

| 报告字段 | 来源端点 / 表字段 | 现成？ |
|----------|------------------|--------|
| 总调用量 req_count / call_count | `/usage` rows[i].req_count；`/usage/dashboard` summary.call_count | 现成 |
| 成功数 success_count | `/usage` rows[i].success_count；dashboard summary.call_count-error_count | 现成 |
| 失败数 error_count | `/usage/dashboard` summary.error_count；`/usage/logs` 需自己 `status!=200` 统计 | 现成 |
| 成功率 success_rate | dashboard 计算 `(call-error)/call`；`/usage` 需 `success_count/req_count` | 现成（近似）/ 派生 |
| prompt_tokens | `/usage` rows[i].prompt_tokens | 现成 |
| cached_tokens | `/usage` rows[i].cached_tokens | 现成 |
| completion_tokens | `/usage` rows[i].completion_tokens | 现成 |
| reasoning_tokens | `/usage/logs`（LogRow.reasoning_tokens）；`/usage` 需确认包含否 | 现成（logs）/ 派生（usage 列缺失） |
| total_tokens | dashboard `SUM(prompt+completion+reasoning)`（不含 cached） | 派生/现成 |
| est_cost_usd / cost | `/usage` rows[i].est_cost_usd；dashboard summary.cost_estimate | 现成 |
| cache_hit_pct | `MyPercentiles.p90_cache_hit_pct`（P90 阈值不是单用户值）；`/usage/logs` 可算 `cached/prompt` | 派生（P90）/ 单值派生 |
| output_ratio（completion 相对非缓存 prompt） | `MyPercentiles.p90_output_ratio`（P90） | 派生 |
| unique_models / model_count | `/usage/dashboard/users` rows[i].model_count；`/usage/logs` `COUNT(DISTINCT model)` | 现成/派生 |
| avg_duration_ms | `/usage/dashboard/models` avg_duration_ms；`model_daily_stats` | 现成 |
| p50/p90_duration_ms | `/usage/dashboard/models` p90_duration_ms；`model_daily_stats` p50/p90 | 现成（models 只 p90） |
| avg_first_token_ms | `/usage/dashboard/models` avg_first_token_ms | 现成 |
| active_days | `/usage/dashboard/users` summary.active_days | 现成 |
| active_users / user_count | `/usage/dashboard/users` summary.total_users | 现成（排除 service） |
| 每日趋势 (date,calls,errors,tokens,cost) | `/usage/dashboard/trend` rows | 现成 |
| top_models（用户 Top N） | `/usage/dashboard/user-top-models` | 现成 |
| 用户 Top 模型明细 | `/usage/dashboard/users` rows[i].top_models | 现成 |
| 用户 vs 平均 (cost_ratio/call_count/error_rate vs avg) | `/usage/dashboard/users` rows[i].vs_avg | 现成 |
| 用户成本占比 / 环比 / 每千token成本 / 错误率 | `/usage/dashboard/users` rows[i].cost_ratio / cost_change_pct / cost_per_1k_tokens / error_rate | 现成 |
| 部门/工作室维度 | `/usage/dashboard?scope=dept\|studio`；`/usage?group_by=dept\|key` | 现成 |
| 小时分布（个人出没时段） | `/my/usage?group_by=hour`；`/usage/dashboard/user-detail` hourly_distribution | 现成 |
| 模型用户/部门分布 | `/usage/dashboard/model-detail` user_distribution | 现成 |
| provider/dept/studio 枚举 | `/usage/options` | 现成 |

### 3.2 派生字段（现有接口**不直接给**，需离线工具二次计算）

| 报告字段 | 计算基线 | 所需上游数据 | 说明 |
|----------|----------|--------------|------|
| **28 天基线**（人均/部门/公司成本、tokens、调用量基准） | 最近 28 天（或自然周，上海时区）同口径均值/合计 | `/usage?group_by=day` 或 `/usage/dashboard/*` 取 28 天序列，再按人/部门除 | 需把周期起点对齐 Shanghai 日切；要对账就是同一套口径 SQL |
| **同岗分位**（某用户在某指标处于同岗位的分位） | 按 `users.job_title / job_family / job_category`（岗位族群/类别/岗位）分组的 PX 分位 | 用户聚合 + `users` 岗位字段 join | **现有接口不返回岗位**，也无同岗分位端点；需离线工具拉用户清单（岗位）+ 每用户聚合值，服务器端或客户端算 `PERCENTILE_CONT` |
| **A1–A5 章节**（报告章节分组/聚合，具体字段待 schema） | 公司/工作室/部门/个人画像/部门程序块 五层 | 对应 scope 的 `/usage*` 聚合 | 章节结构来自报告设计；本 recon 只保证底层构建块可用 |
| **周环比 / 同比**（cost/tokens/calls 相对上周） | 上一周期同口径 | `/usage?group_by=week` 或按 day 序列自算 | dashboard 只在 users 做 cost_change_pct，层级周环比需自算 |
| **每千 token 成本 / 每次调用成本** | cost / tokens*1000；cost / calls | `/usage` 或 dashboard summary | `cost_per_1k_tokens` users 已给，公司/部门级需自算 |
| **模型长尾 / 健康度标注** | 调用量 < P10 或 <5 | `/usage/dashboard/models` 已给 `is_long_tail` | 现成（models）；其他维度需自算 |
| **成功/错误率趋势** | 每日 success_rate | `/usage/dashboard/trend` | 现成（trend 给了 errors/calls） |
| **岗位人均**（某岗位人均成本/tokens） | 同岗人员聚合 / 人数 | 岗位成员 + 每用户聚合 | **派生**，需 users 岗位 join |

### 3.3 「同岗分位 / 28 天基线 / A1–A5」依赖的数据缺口（重要）

离线工具若要做这三项，会撞到**现有接口不覆盖**的能力：

- **岗位字段**（`job_title/job_family/job_category`）只在 `users` / 花名册导入侧，**不出现在**任何 `/api/v1/usage/*` 返回里。做「同岗」必须额外拉用户（`internal/auth/model` 的 `users` 表字段）或用 `internal/v1` 的内部门户，否则无岗位信息。
- **同岗分位 / 28 天基线**无对应端点；只能拉日粒度序列后本地计算，或离线工具**直连只读 DB 跑这些派生 SQL**（若允许）。阶段一句话：**走 HTTP 拿上游聚合 → 本地派生**。
- `/my/usage/percentiles` 给的是**全站用户 P90 标签阈值**（30 天，且**未排除 service**），与「同岗分位 / 个人画像排除 service」口径不同，**不可直接当同岗分位用**，仅可作 P90 标签参考。

---

## 4. 阶段二/阶段三接入建议

- **Base URL**：控制面 `http://127.0.0.1:8081`（dev，直接打 control-plane）或 `:8090`（nginx 对外口，`/api/*` 反代 CP）。配置见 `config/dev.yaml`（`control_plane_url: http://127.0.0.1:8081`）。
- **鉴权**：需要 JWT（`Authorization: Bearer <access_token>`），且要 `PermUsageViewAll`（admin）才能拿全公司口径。离线工具应配置一个 admin 服务账号 token（或走 OAuth 配置里的 client-credentials），否则 `/usage/dashboard/*` 会被 actor 范围收窄。
- **拉数策略**：尽量用 `/api/v1/usage?group_by=day`（day/week）与 `/api/v1/usage/dashboard/*`（分层/排行/详情）复用后端聚合；「同岗分位/28 天基线」需用户在/岗位维度则要补用户清单。
- **对账建议**：同一时间窗内，(a) `/usage/dashboard` summary 与 (b) `/usage?group_by=day` 逐日求和，与 (c) 直接对 `call_logs`（如允许只读 DB）三者应一致；特别注意 day 粒度 `date(ts)`（会话时区）与 `AT TIME ZONE 'Asia/Shanghai'` 的切日差异，以及 `usage_daily`(未排除 service) 与 `user_daily_stats`(排除 service) 的差异。
- **阶段一范围**：只做「数据面映射 + 口径固化 + 工具骨架（HTTP 拉数 → 派生 → report JSON → MD/HTML）」，**不写进 gateway/control-plane 运行时**，不新增端点，不直连 DB。

---

## 附：关键源码常量速查

- 排 service：`WHERE COALESCE(NULLIF(key_type,''),'personal') != 'service'`（别名加 `cl.`）。
- 只 service：`WHERE COALESCE(NULLIF(key_type,''),'personal') = 'service'`（`cl.`）。
- dept 归一：`JOIN departments d ON (d.dept_id = cl.dept_id OR d.id::text = cl.dept_id)`。
- studio 归一：`JOIN dept_studio_map dsm ON dsm.dept_id = COALESCE(d.dept_id, cl.dept_id)`。
- 用户→部门：`users u ON u.id::text = cl.user_id`；`u.dept_id` 为 `*uuid.UUID`。
- 小时口径：`date_trunc('hour', ts AT TIME ZONE 'Asia/Shanghai')`；小时分布 `EXTRACT(HOUR FROM (ts AT TIME ZONE 'Asia/Shanghai'))`。
