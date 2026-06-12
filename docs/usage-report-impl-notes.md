# 阶段一离线用量周报 · 实现指引（Impl Notes）

> 给实现者的权威落点。实现依据：`docs/recon.md`（数据面+口径）、`docs/report-schema.md`（report JSON 契约+派生算法）；本文补三条：①同岗分位数据源修正；②架构绑定决策；③A1–A5 阈值修正与渲染三层约束。

## 0. 三条读作守则（贯穿实现）
- **不直连 DB**：数据一律从 `/api/v1/*` HTTP 拉，工具零库权限。
- **不新增服务/不改运行时**：不加 `cmd/` 服务、不改 `router.go`、不写采集链路；核心逻辑做成**共享纯库**（阶段二直接 import）。
- **三层数据流**：事实层(聚合程序，纯数字) → 判断层(规则引擎，算基线/分位/归因/命中/语义标签) → 表达层(大模型，只回填 headline/suggestions 措辞)。数字与判定前两层全部按确定性产出；表达层只引用 `fact_ref`，**禁止裸数字**。

## 1. 关键修正：同岗分位有数据源（可用 HTTP 拿，不必直连 DB）
- `users` 表有 `job_family/job_category/job_title`（`internal/auth/model/user.go:19-21`），员工花名册导入写入。
- 但 `/api/v1/usage/*` 与 `/api/v1/org/users`(SearchUsers) 都不返回岗位字段。
- **可用端点**：`GET /api/v1/org/departments/:dept_id/members`（`GetDeptMembers`，权限 `PermMemberView`；`internal/orgservice/handler/org_handler.go` + `repository/user_org_repo.go:133-160` 已 SELECT `job_title/job_family/job_category`）。
- **拼 rosters**：`/api/v1/org/dept-studio-map` + `/api/v1/org/studios` + `/api/v1/org/departments` 枚举部门 → 逐部门拉成员 → 本地拼出 `(user_id, dept_id, studio_id, job_category/job_family/job_title)` 去重清单。
- 需要 `PermMemberView` 的管理员 JWT；阶段一用 admin token。**阶段二**把该清单改为 control-plane 每日 job 物化（`user_org`/角色表）。
- 落入 `docs/org-roster.md`（由 backend-eng 负责细化，见 t5）。

## 2. 架构绑定决策（来自后端架构专家；避免阶段二返工）
```
tools/usage-report/            # 独立入口，属主 module github.com/example/llm-gateway
├── main.go  config.go         # CLI + Viper（周期/级别/范围/输出路径）
└── client/usage_client.go     # 唯一含网络层，调 /api/v1/usage/*、/api/v1/org/*
internal/observability/report/ # ★ 共享纯库（阶段二 job 直接 import），不动 cmd/router/runtime
├── report.go  metric.go       # 契约 + 数据源无关的归一化中间表示 DailyMetric
├── derive/baseline.go         # 28天基线 / 同群分位
├── derive/anomalies.go        # A1–A5
└── render/render_md.go render_html.go  # MD/HTML，阶段二 web 复用
```
- 数据源适配：现在 `adapter/from_http.go`，阶段二加 `from_db.go`/`from_engine.go`，都归一化为 `DailyMetric`，**派生/装配/渲染零重写**。
- 派生与装配：`derive.Derive(ctx, []DailyMetric, CohortDef, RuleSet) (*Report, error)` 纯函数。

## 3. A1–A5 阈值修正（来自成本护栏专家；复核后生效）
- **通用**：基线必须**物化**（`baseline_quantiles`），参考窗**钉死**为事件日之前的 28 个完整自然日；分位用精确分位（`percentile_cont`）；命中事件记录 `rule_version + baseline_window + baseline_value` 以精确重演。产出 USD 与 ¥（记录汇率）。口径：`status=200` 成功、失败 `status!=200`；`total_tokens=prompt+completion+reasoning`（不含 cached）；小时 `ts AT TIME ZONE 'Asia/Shanghai'`；成功=200。
- **A1 程序失控**：10 分钟调用 > `max(60, 个人P99)` 且中位间隔 <10s 且**连续 ≥15 分钟（用 1 分钟粒度滑动窗口判定，勿用 tumbling）**。豁免：`key_type=service` 白名单、`__system_`、备案。
- **A2 成本燃烧**：15 分钟成本 > `max(¥50, 个人28天同窗中位×5)`，**另加绝对封顶 `HARD_CAP_A2=¥200/15min` 独立触发**（防重用户 ×5 过松）。币种：USD 为事实源，固定 `cny_per_usd`（默认 7.2）换算并记录。
- **A3 重试循环**：**不要按状态码分组**（会与失败率冗余）。改为：`segment=(key_id,model)`，段内 `req≥20 ∧ fail_rate≥30% ∧ 单一 status_code 占失败≥80% ∧ failed≥6`，失败定义 `status≥400`。区分上游故障(5xx→归上游)与客户端僵死(429/400)。
- **A4 超长上下文**：①单条 >80% 偏噪 → 加「窗口内近上限请求 ≥2」或降 P2；②「模型上限」用**有效输入预算** `context_length − max_output_tokens`（组内取最小正值）；③**分支2(IO比同场景P99) 依赖 `scene` 埋点，未埋点前默认禁用/灰度**。
- **A5 非工作时段**：00:00–07:00 或周末成本 > `max(同岗位P99, ¥100)` 且**非备案**。①小样本降级链 `job_title→job_category→job_family→dept→global→仅floor`，`MIN_SAMPLE=5`；②两窗口(深夜/周末)按 `窗口×岗位` 分桶；③备案带 `until` 过期，防永久豁免；④补 `HARD_CAP_A5`。
- **命中输出统一 schema**（判断层四要素，可作依据）：
  `{rule, rule_version, severity(P0..P3), window, actual_value, threshold_value, baseline_value, baseline_window, impact_cost=Σest_cost_usd(命中请求), excess_cost=max(0,actual−baseline), matched_count, dimensions, evidence}`。

## 4. 渲染三层约束（来自 UX/信息架构专家；MD/HTML 从同一 JSON）
- **facts(事实层)**：一切数值+单位/币种/口径/时间窗/基准，叶子值，无措辞。
- **judgments(判断层)**：规则算出的结论，**必须附证据** `{verdict, threshold, actual, baseline, delta, impact_cost, direction, severity, dimension, reason_refs}`；机器可复算，是报告的信任主体。
- **expression(表达层)**：分节顺序/标题/徽标 + LLM 措辞 + 建议(owner+截止+what-if+采纳)。**数字只许 `fact_ref` 引用；渲染器对 LLM 文本做校验（任何裸 `\d` 落在 `{...}` 之外→视为幻觉→拒用/降级模板兜底）**。AI 解读带 `置信 + 非处置依据` 徽标，永远排在判定之后。
- **版式**：MD=GFM（front-matter `facts_digest` 供对账，结论 `> `、事实 `` `inline` ``、严重度 `[P0..P3]`）；HTML=自包含 inline CSS/SVG（复用现有 token：`--bg-surface/-border-default/--text-primary/--brand-green/--severity-P0..P3/--chart-*`），单页主区块≤5 个其余 `<details>` 折叠，图表 inline SVG 带 `data-fact`。结论→异常→建议前移，规模趋势/结构分布/变化归因居中，附录(口径/对账清单)兜底。个人不跨岗排名、同岗匿名分位。

## 5. 约束红线
- 成功=`status=200`；个人画像/岗位人均排除 `key_type='service'`；部门 Service Key 块仅 `= service` 且归属当前部门；`dept_id` 归一 `d.dept_id=cl.dept_id OR d.id::text=cl.dept_id`；CNY=`est_cost_usd×rate`（记录 rate/source/at）。
- 判定性分析全部确定性、可复现、可作依据；不做 Prompt 正文采集/内容级合规、不做跨岗位裸露排名、模型不做判官。

## 6. `total_tokens` 口径钉死（backend-eng 评审发现 3 方漂移）
`usage_daily` 无 `total_tokens` 存储列，聚合层从 `prompt_tokens / completion_tokens / reasoning_tokens` 分列现算；且 `collector.go:666` `uncached := PromptTokens − CachedTokens − CacheWriteTokens` 证明 `cached_tokens`/`cache_write_tokens` 是 `prompt_tokens` 的**子集**（非增量）。
- **报告口径（唯一）**：`total_tokens = prompt_tokens + completion_tokens + reasoning_tokens`；`cached_tokens`/`cache_write_tokens` **不再并入 total**（它们是 prompt 子集），单列用于 `cache_ratio = cached / prompt`。
- **对账基准**：以 `GET /api/v1/usage/dashboard`（GetDashboard）、`/usage/dashboard/trend`、`/usage/dashboard/models` 这一派为准（它们即 `SUM(prompt+completion+reasoning)` 不含 cached）。
- **禁止跨端点混拼**：`/usage/dashboard/users*` 与 `DashboardUsersEnhanced` 用的是 `SUM(call_logs.total_tokens)` 存储列（≈prompt+completion，提供商口径，可能不含 reasoning / 含 cached），**不得作为报告 total_tokens 来源或与 GetDashboard 对账**。若 per-user 端点只给存储列，报告用组件口径重算或明确标注口径差异。
- 若真实数据校验后确认 `reasoning` 实际已被 `completion` 包含（部分提供商语义），则在 `summary` 单列 `reasoning_tokens` 展示而不并入 total，避免重复计数；以对账实测为准，口径变化需记录 reason。
- A5 的「非备案」在阶段一**无备案治理表**，先按纯时间窗口（00:00–07:00 / 周末）触发并标注「阶段二补备案豁免」；A4 的模型上下文上限取自 `model_catalog`（实现时定位 HTTP 端点，缺少则按文档 `context_length − max_output_tokens` 保守取最小正值并记录来源）。
