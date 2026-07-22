# Token 使用分析 v0.6 优化实施方案

> 基线文档：`Token使用分析-维度与角度.md`，版本 v0.6，日期 2026-09-01。
>
> 目标：把当前“结构已搭好”的实现收敛为口径可信、权限正确、可持续生成、可供个人、部门和工作室实际使用的正式功能。
>
> 本方案只以 v0.6 为验收基线。健康评分、员工排名、自动定责等后续设想不纳入本轮。

## 0. 范围确认与逐节映射

本项目本轮所称“Token 使用分析”，只围绕基线文档的以下五部分建设：

| v0.6 章节 | 本轮实现对象 | 主要代码边界 | 完成判据 |
|---|---|---|---|
| §1 从什么维度评判 | 成本事实 + 效率问题，彼此分列 | `internal/observability/report/`、`service/report_orchestrator.go` | 只有成本与效率两个维度，不生成成本分、效率分或综合评分 |
| §2 从什么角度评判 | 三类成本桶、四类效率角度、会话近似、历史/同岗基线、判定证据等级 | gateway 成本采集、`session_fact.go`、`baseline_service.go`、`report_provider.go` | 成本可对账；效率按上下文/交互/生成/选型解释；证据不足只到疑似 |
| §3 需要关注的情况 | 3.1-3.5 五类用法问题 + 3.6 独立待核查异常 | `issue_*.go`、`anomaly_offhours.go`、`cost_attribution.go`、`semantic_upgrade.go` | 每类都有看/判定/排除/次数占比/费用归因；3.6 不进入 Y 轴且形成核查闭环 |
| §4 成员整合评估 | 组织中位数、X/Y、四象限、颜色、描边、分群、点选详情 | `report/quadrant.go`、`cluster.go`、`MemberQuadrantChart.vue`、成员列表组件 | 只评同期有调用的人；右下视为正常；不按总成本排名 |
| §5 三份报告 | 个人、部门、工作室的差异化内容和顺序 | Report JSON、`render_md.go`、`UsageReportPage.vue` 及子组件 | 三份报告逐块覆盖原文要求，页面/Markdown/HTML 数字同源 |

### 0.1 明确纳入范围

- Token 与成本原子数据采集，以及保证三分法正确所需的字段。
- 会话事实、五类规则、费用归因、基线和数据质量。
- 成员四象限、分群、问题成员与正常成员划分。
- 个人、部门、工作室三份报告及其 API、RBAC、快照和前端。
- 3.6 待核查记录、反馈和非工作时段备案闭环。
- 为保证上述结果可复现、可运行所必需的定时任务、索引、测试和监控。

### 0.2 明确不纳入范围

- 不做 A/B/C 健康评级、成本分、效率分或综合绩效分；现有 `health_rating.go` 不接入 v0.6 页面与结论。
- 不做员工成本排行榜，也不以成本高低直接产生负面结论。
- 不把模型维度变成第四个成本桶；模型只作为三类成本的价格放大器和切片。
- 不自动认定违规、违纪、滥用；3.6 的自动结果始终只是待核查线索。
- 不扩展为通用审计日志、运维告警、预算管理或模型健康项目。Service Key、归档和预算数据只在支撑 v0.6 报告口径时使用。
- 不在本轮引入 v0.7 的评分、亮点卡或与 v0.6 无关的画像标签。

### 0.3 关键解释决策

- “先看成本，再看效率”落地为报告先展示基础数据和成本构成，再展示相关效率问题；后端仍计算全部五类规则，避免因主成本桶变化而漏掉问题。
- “单信号只标记，两个条件同时成立才定性”落地为统一证据等级：一个结构信号为 `suspected`，两个独立信号且排除项通过才可 `confirmed`。任何规则的例外都要在 `rule_version` 和验收用例中明示。
- “对照本人历史和同岗位”仅用于解释与规则基线，不用于跨岗位排名。组织四象限的 X 轴按文档要求使用当前组织成员成本中位数。
- “默认本周”只表示默认查询周期。用户选择日/月后，标题、环比周期、快照键和所有文案都随周期变化。

## 1. 结论与原则

当前可复用的部分包括：成本/效率分层、五类问题模型、个人与组织报告结构、四象限与分群纯函数、Service Key 排除、严重异常独立于 Y 轴、RBAC 基础设施和 Report JSON 校验。

本轮不推倒重写，但必须先修正 5 个会改变业务结论的基础问题：

1. 成本三分桶与 `est_cost_usd` 不守恒，缓存写入成本会丢失。
2. 五类问题费用按“用户”去重，Y 轴退化为最大单项，而不是去重后的五类合计。
3. 报告 scope 没有形成前端选择、服务端规范化、数据层过滤的完整闭环。
4. 报告快照忽略 `period_type`，开放周期又没有刷新策略，可能串周期或长期陈旧。
5. 严重异常没有生成 `anomaly_review`，反馈接口没有可更新的记录。

实施遵循以下原则：

- **先事实，后判定**：总成本、Token、请求、组织归属先对账，规则结果才能进入报告。
- **成本与效率分账**：成本回答“花在哪”，效率回答“为什么多花”，不生成综合分。
- **按原子费用去重**：同一请求的同一费用桶只能归一个主因，不同请求、不同费用桶可以分别归因。
- **缺字段不伪装完整**：缺真实会话、当前用户输入、任务标签或价格时，输出 `suspected`/`not_computed`，并展示覆盖率。
- **开放周期看实时，关闭周期看快照**：本周持续变化，已结束周/月可复现。
- **Token 只给线索**：3.6 始终从 `pending_review` 开始，自动分析不输出定责结论。

## 2. 目标数据链路

```text
Gateway UsageEvent
  -> call_logs 原子事实（Token、四项成本、身份快照、会话/场景元数据）
  -> 日聚合 user_daily_stats / model_daily_stats / usage_daily
  -> 周月聚合 user_period_stats + baseline_quantiles
  -> ReportFactLoader（一次取 scope 当期、上期及候选证据）
  -> 五类规则输出 RuleEvaluation + 原子费用 Claim
  -> AttributionEngine 按 request_id + cost_bucket 清算
  -> Report Assembler（基础数据、成员评估、严重异常、结论）
  -> report_snapshot（关闭周期最终版；开放周期短缓存）
  -> Web / Markdown / HTML 同源渲染
```

报告链路的唯一事实源仍是控制面的 PostgreSQL。gateway 不增加数据库查询，只负责在既有异步观测事件中补齐原子字段。

## 3. 成本维度设计

### 3.1 建立可对账的四项内部成本

对外仍展示 v0.6 三块：

- 非缓存输入：`uncached_input_cost_usd`
- 缓存：`cache_read_cost_usd + cache_write_cost_usd`
- 输出：`output_cost_usd`

内部必须保存四项成本，新增：

```text
call_logs.cache_write_cost_usd numeric(18,8) NOT NULL DEFAULT 0
```

采集侧改为一个函数一次返回完整结果，禁止 `EstimateCost()` 算总价、Collector 再用另一套价格重算分项：

```go
type CostResult struct {
    UncachedInputUSD float64
    CacheReadUSD     float64
    CacheWriteUSD    float64
    OutputUSD        float64
    TotalUSD         float64
    EffectivePrices  PriceSnapshot
}
```

这样同时解决阶梯价下“总价使用阶梯单价、分项仍使用基础单价”的偏差。写入前强制校验：

```text
abs(total - (uncached_input + cache_read + cache_write + output)) <= 1e-8
```

报告契约增加显式 `total_cost_usd`，不再把三个展示字段相加当权威总成本。`CostBreakdown.Validate()` 同时校验展示三块之和与总成本一致。

### 3.2 历史数据兼容

对旧 `call_logs`：

```text
legacy_cache_write_cost = GREATEST(
  est_cost_usd - input_cost_usd - cache_read_cost_usd - output_cost_usd,
  0
)
```

回填只把可解释的正残差归入缓存写入；负残差或超容差记录不静默修正，计入 `cost_bucket_coverage`。报告展示“成本分桶覆盖率”，低于 99% 时提示分桶为估算。

需要同步更新：`UsageEvent`、`CallLog`、BatchWriter、`user_daily_stats`、`user_period_stats`、`usage_settlement_daily`、Report JSON 和前端类型。

### 3.3 失败请求口径

一次加载后拆成两组数据：

- `billing_rows`：全部请求，用于总成本、三分桶、总请求数和失败费用。
- `behavior_rows`：仅 `status = 200`，用于会话装配和 3.1-3.5 行为判断。

报告基础数据同时返回：

```json
{
  "request_count": 100,
  "success_count": 94,
  "failed_count": 6,
  "failed_request_cost_usd": 1.23
}
```

失败成本已经包含在三块和总成本里，只做“无效消耗”标记，不再进入行为问题的命中次数与会话结构。

### 3.4 成本维度验收

- 任意报告 `total_cost_usd = SUM(call_logs.est_cost_usd)`，误差不超过 `$0.01`。
- 三个展示桶之和等于总成本；缓存桶包含读和写。
- 阶梯价格、缓存读、缓存写、普通输入、输出均有单测。
- 失败请求不会触发 3.1-3.5，但其成本仍在总成本和对应桶中。

## 4. 效率维度与费用归因

### 4.1 改成原子费用 Claim

现有规则只返回聚合后的 `UsageIssue`，丢失了“哪笔请求的哪块费用被命中”。应改成内部结构：

```go
type CostBucket string // uncached_input | cache_read | cache_write | output

type CostClaim struct {
    IssueType   string
    RequestID   string
    Bucket      CostBucket
    ClaimedUSD  float64
    AffectedUSD float64
    Confidence  float64
}

type RuleEvaluation struct {
    Issue  report.UsageIssue
    Claims []CostClaim
}
```

费用原子的唯一键为 `(request_id, bucket)`。归因引擎对同一原子上的多个 Claim 选一个主因：默认按 `ClaimedUSD` 最大，其次按固定 `IssueTypeOrder` 保证确定性。

正确对账关系是：

```text
attributed_total = 所有获胜 Claim 的合计
unattributed_cost = total_cost - attributed_total
overlap_cost = 未获胜 Claim 的声明金额，仅作“共同命中”说明

attributed_total <= total_cost
```

`overlap_cost` 是重复解释，不是另一笔真实成本，因此不得从 `total_cost` 再扣除。删除当前 `total = attributed + overlap + residual` 的错误假设。

### 4.2 五类规则如何产生 Claim

| 规则 | 费用原子 | 具体归因 |
|---|---|---|
| 前缀污染 | 命中段各请求的 `uncached_input/cache_read/cache_write` | 命中段对应输入与缓存费用 |
| 挤牙膏 | 首轮之后各请求的输入与缓存原子 | 额外轮次造成的费用 |
| 多任务不切会话 | 首个任务切换点之后各请求的输入与缓存原子 | 继承旧历史的费用 |
| 输出失控 | 命中请求的 `output` 原子 | 超出模型输出 P75 的比例费用 |
| 杀鸡用牛刀 | 命中请求的四项成本原子 | 实际档位与合格轻量替代的逐桶价差 |

规则层必须返回命中 request/session ID，不能只返回聚合金额。一个会话规则最终也要展开为会话内请求级 Claim，才能和请求级规则正确去重。

### 4.3 修复 Y 轴与主导问题

Y 轴严格定义为：

```text
issue_cost_ratio = sum(winning_claim.claimed_usd) / member_total_cost_usd
```

主导问题取清算后 `attributed_cost_usd` 最大者。增加统一的重要性阈值：

```text
materiality = max($0.01, member_total_cost_usd * 1%)
```

最大归因低于该阈值时颜色显示“正常”，但 `is_problem_member` 仍按 v0.6 独立判断：高于组织 Y 中位、任一规则 `confirmed`、或命中 3.6。

验收必须加入反例：同一用户有两个不重叠请求，分别被不同规则归因 `$10` 和 `$8`，Y 轴分子必须是 `$18`；同一请求同一费用桶被两条规则命中时才择一。

## 5. 五种情况逐项优化

### 5.1 会话事实是 3.1-3.3 的前置能力

当前所有会话均由缓存链/时间推断，`real_session_coverage` 却固定返回 100%。目标模型新增可空字段：

```text
session_id                text
session_id_source         varchar(16)  // client | gateway | inferred
turn_index                integer
current_user_input_tokens bigint
current_input_source      varchar(16)  // provider | tokenizer | estimate
reset_flag                boolean NULL // NULL 表示未知，不等于 false
task_kind                 varchar(32)
scene                     varchar(32)
finish_reason             varchar(32)
```

采集优先级：客户端显式会话号 > 协议字段 > 网关可稳定推导键 > 离线 inferred。`current_user_input_tokens` 只计算本轮新增的最后一条用户消息，不能继续用包含历史的 `prompt_tokens` 代替。

在字段覆盖未达到阈值前：

- 3.1-3.3 基于 inferred 会话的命中上限为 `suspected`。
- 缺 `current_user_input_tokens` 时，3.2 返回 `not_computed`；不得把总 Prompt 小于 30 当本轮短输入。
- `reset_flag IS NULL` 表示未知；证据中不得写成“无 reset”。

### 5.2 前缀污染

保留“输入 >500k、连续至少 3 轮、缓存命中 <30%”的现有主体规则。调整点：

- 使用成功请求组成会话。
- inferred 会话只到 `suspected`；client/gateway 会话且连续段证据完整才可 `confirmed`。
- “超长但命中稳定”只进入排除证据，不生成污染 Claim。
- 每个命中段输出 request IDs 和逐桶 Claim，支持费用去重与下钻。

### 5.3 挤牙膏

判定输入改为 `current_user_input_tokens`，条件为：短输入占比 >=50%、会话轮次 >=8、且同一任务。

- 有 `task_kind` 或合规语义证据证明同一任务：可 `confirmed`。
- 有当前输入 Token 但缺任务证据：`suspected`。
- 连当前输入 Token 都缺：`not_computed`。
- 单轮短问、批处理、工具连续轮进入排除项。
- 建议固定为“首轮写清需求”，不与 3.3 的“新任务开新会话”混用。

### 5.4 多任务不切会话

条件保持“超长 + 缓存命中不差 + 未 reset + 任务切换”。其中 reset 使用三态，任务切换优先读 `task_kind`，其次使用经过脱敏的语义信号，最后才使用 Prompt 突增代理。

- 四项均有强证据才 `confirmed`。
- Prompt 突增代理只能 `suspected`。
- 同任务 Agent/IDE 工具轮和已 reset 会话必须排除。
- Claim 从真实或推定任务切换点后的 request 开始，不从整个用户周期开始。

### 5.5 输出失控

按 v0.6 落实“双条件定性”：

1. 主信号：`有效输入 < P25 且输出 > P75`，或 `输出/有效输入 >20`。
2. 强化信号：同一用户重复至少 N 次，或使用高阶模型。

主信号成立但没有强化信号时只标 `suspected`；两者成立才 `confirmed`。`finish_reason=length`、重复退化输出可作为强化证据，但不能绕开数据质量说明。

当前 `OutputRunawayRows` 没有填充 `finish_reason/output_text`，因此应二选一：

- v0.6 首发只使用 Token、分位、重复次数和模型档位；删除未接通字段造成的伪能力描述。
- 后续完成采集后再启用 finish reason/循环检测，并独立提升规则版本。

归因保持“超出模型输出 P75 的输出费”。基线缺失时不得出现“命中但归因 0”，应为 `not_computed` 或仅提示数量异常。

### 5.6 杀鸡用牛刀

修正三个口径：

- `SessionTurns` 必须来自统一 SessionFact，不能把每个 request 强制写成 1。
- 档位查询使用真实 `routed_model`，不是可能为模型组别名的请求 `model`。
- `request_share` 分母是“疑似简单请求数”，不是全部请求数。

轻量替代必须同时满足：同能力组、可用、低于当前档位、支持本次所需能力。溢价按同一 Token 结构重算四个成本桶，包含缓存读写差价。缺场景标签时维持 `suspected`。

### 5.7 首阶段语义执行器：OpenCode CLI

首阶段不在 control-plane 在线请求内直接调用模型，而由离线 `report-gen` 使用 OpenCode CLI 复核规则候选：

```bash
go run ./cmd/report-gen \
  -level personal \
  -user USER_ID \
  -start 2026-09-01 \
  -period week \
  -semantic \
  -sem-backend opencode \
  -sem-model biangfeng-gateway/bf-glm-5.3-flash
```

CLI 调用固定使用 `--format json --agent token-usage-judge`，证据经 stdin 传递，不放进命令参数。专用 Agent 位于 `.opencode/agents/token-usage-judge.md`，关闭文件、Shell、网络和子任务工具；每次判断使用独立会话，结束后尽力删除。报告任务默认使用 `$USER_CACHE_DIR/llm-gateway/opencode-data` 隔离日常 OpenCode 会话库。静态 provider 可额外设置 `SEMANTIC_OPENCODE_PURE=true`；当前 `biangfeng-gateway` 模型由发现插件注入，必须保留插件加载。进程超时、退出异常、JSONL `error` 事件、输出不符合词表或置信度越界时，都降级为 `uncertain`，保留原数值规则的 `suspected`。

CLI 只参与 3.2、3.3、3.5、3.6 的语义复核；成本、Token、分位、费用归因、四象限和最终状态合成仍由 Go 代码负责。组织报告最多并行启动 2 个 CLI 判断，避免本机资源失控。线上 HTTP 报告接口暂不启用 CLI，待离线黄金样本确认效果后再决定迁移到常驻 OpenCode SDK worker。

## 6. 数据质量与基线

### 6.1 数据质量改为真实计算

`DataQuality` 至少返回：

- `real_session_coverage`
- `current_input_coverage`
- `task_scene_coverage`
- `price_catalog_coverage`
- `position_label_coverage`
- `failed_cost_coverage`
- `cost_bucket_coverage`
- `semantic_evidence_coverage`

覆盖率按当前 scope 和周期计算，禁止硬编码。真实会话字段尚未上线时必须返回 0，并展示“按近似会话分析”。规则的 `verdict` 与覆盖率联动由后端完成，前端只解释，不自行改判定。

### 6.2 基线禁止未来数据泄漏

历史报告读取基线时必须满足：

```text
baseline.window_end <= report.period.from
```

同一 scope/metric 取 `window_end` 最大的一条。当前按“最新物化结果”读取会让旧报告使用未来数据，破坏可复现性。

基线目标窗口为 8-12 周，而 `call_logs` 默认只保留约 30 天。推荐两阶段处理：

1. 首发将实际窗口和样本量写入报告，窗口不足时降级，不冒充 56 天基线。
2. 新增长期 `behavior_metric_daily` 直方图/分位摘要，从日摘要合并 56-84 天基线；若不新增摘要表，则必须把 `call_logs` 保留期提高到至少 91 天并完成容量评估。

### 6.3 修复日/周期聚合

- `user_daily_stats` 比率显式转 `numeric`，避免 PostgreSQL 整数除法得到 0/1。
- 重写跨小时模型去重，避免 `array_agg(DISTINCT unnest(...))` 的非法聚合；在独立 CTE 中按用户 `COUNT(DISTINCT model)`。
- 所有日边界统一使用 `(ts AT TIME ZONE :tz)::date`，不依赖数据库会话时区。
- `user_period_stats.model_count` 不能用 `MAX(日模型数)` 冒充周期 distinct；日层保存模型集合，或增加 `user_model_daily` 后再精确上卷。
- 当前周/月需要随日聚合更新；定时任务不能只在周一/月初计算一次当前周期。
- 用户跨部门时按事件发生时的 `call_logs` 组织快照归属。日/周期表主键需保留 `dept_id/studio_id` 维度，避免 `MAX(dept_id)` 把历史归属覆盖掉。

## 7. Report JSON 契约

在现有 `Report` 上增加 `overview`，统一承载三份报告的基础数据：

```json
{
  "overview": {
    "total_cost_usd": 0,
    "member_cost_usd": 0,
    "service_cost_usd": 0,
    "request_count": 0,
    "success_count": 0,
    "failed_count": 0,
    "active_users": 0,
    "session_count": 0,
    "model_count": 0,
    "token_breakdown": {},
    "cost_breakdown": {},
    "models": [],
    "departments": []
  }
}
```

口径分层：

- 个人：只含 personal key。
- 部门/工作室总览：包含 personal + service，总量中明确拆出 `member_cost_usd/service_cost_usd`。
- 四象限和五类个人行为：只使用 personal key。
- 工作室 `departments[]` 返回各部门同口径基础数据，不做排名字段。

`MemberPoint` 补齐并在前端类型中公开：`token_breakdown`、`cost_breakdown`、`is_problem_member`、`safety_detail`。序列化前移除所有 `_attr_by_session`、`_attr_by_request` 等内部证据字段，正文样本默认不进入快照或管理页面。

## 8. Scope、RBAC 与 API

### 8.1 明确两个入口

- `GET /api/v1/my/usage/report`：所有登录用户可访问，只能读取本人，服务端忽略外部 user ID。
- `GET /api/v1/usage/report`：管理角色访问，参数为 `level + scope_id + period_type + period_start`。

管理端增加 `GET /api/v1/usage/report/scopes`，只返回当前 actor 可选的工作室、部门和成员。全局管理员请求 department/studio/personal 时必须显式给 `scope_id`；普通管理角色仅有一个范围时可默认选中。

### 8.2 数据层必须同时应用两种过滤

所有 ReportDataProvider 查询必须同时应用：

1. actor 可见范围；
2. 当前报告目标 scope。

当前只在 `Users()` 过滤目标部门/工作室，后续 `CallRows()` 只按用户过滤，会把用户在其他部门时段的调用带入。应让 provider 持有规范化后的 `ResolvedScope`，每个查询统一调用 alias-aware predicate。

`TraceBodies()` 不能把带 `call_logs.` 前缀的 scope 直接套在 `call_traces` 上。应 `JOIN call_logs cl ON cl.request_id=ct.request_id`，在 `cl` 上做 RBAC/scope，再读取 `ct.request_body`。

### 8.3 周期参数规范化

- `day`：保持所选日期。
- `week`：规范化为所在周周一。
- `month`：规范化为所在月 1 日。

API 响应返回规范化后的周期。前端日期选择器同步显示该日期，避免同一周产生七个缓存键。

## 9. 快照与严重异常闭环

### 9.1 修正快照身份

唯一键至少包含：

```text
(level, scope_id, period_type, period_start, schema_version, rule_version)
```

增加 `snapshot_state`（`preview|final`）、`source_max_ts` 和 `generated_at`：

- 开放周期：不永久命中旧快照；采用 5 分钟短缓存或 `refresh=true` 重算。
- 关闭周期：周期结束并等待观测延迟后生成 `final`，默认直接读取。
- 规则版本变化：产生新快照，不覆盖旧规则历史。

新增 `ReportSnapshotCron`：每日完成上一日，周一完成上一周，月初完成上一月。它与现有运维邮件 `ScheduledReportService` 是不同职责，不能复用后者的简单汇总文本冒充 v0.6 报告。

### 9.2 异常记录与快照同事务写入

生成报告时，对每个命中 3.6 的成员 upsert `anomaly_review`：

```text
unique(user_id, period_type, period_start, rule_version)
```

记录金额、占比、时段、模型、脱敏 Key、证据摘要和初始 `pending_review`。反馈接口按精确周期更新一条记录，禁止空 `period_start` 批量终裁多个周期。

反馈状态不写死在不可变报告 JSON 中。读取报告时把 `anomaly_review` 当前状态 overlay 到个人或成员 `safety_detail`，这样反馈后刷新页面立即生效，也不会篡改原始判定证据。

### 9.3 备案流程

为 `offhours_filing` 补 CRUD、RBAC 和审计日志；增加 `end_at > start_at` 约束。`confirmed_ok` 可由负责人选择是否沉淀为备案，但必须明确备案范围与过期时间，不能永久豁免。

展示术语统一为“非工作时段未备案消耗”或“疑似非工作场景消耗”，不要在人工确认前直接称为“与工作无关成本”。

## 10. 三份报告页面设计

### 10.1 共用顶部区域

- Scope 选择器：个人/部门/工作室，随角色只显示可访问项。
- 周期分段控件：日/周/月；日期自动归一。
- 数据状态：开放周期/最终快照、数据截止时间、规则版本。
- 基础数据带：总成本、Token 结构、调用成功/失败、活跃人数/会话数、模型费用占比。

### 10.2 个人报告

顺序严格为：基础数据 → 严重异常 → 成本构成 → 五类问题 → 结论。

- 给普通用户在“我的用量”增加“使用分析”页签或 `/my/usage/report` 页面。
- 五类卡按归因费用降序，`not_hit` 默认折叠。
- 建议使用固定模板，每类只给一个动作。
- 不展示其他成员、组织成本排名或内部证据字段。

### 10.3 部门报告

顺序为：部门基础数据 → 严重异常人员清单 → 四象限 → 分群 → 问题成员 → 正常成员。

- 基础数据拆 personal/service 成本，附活跃人数和人均个人成本。
- 严重异常从 `members[].safety_detail` 聚合为置顶表，不再检查顶层 `report.safety_anomaly`。
- 问题成员列表直接使用后端 `is_problem_member`，不要在前端按象限重新推导。
- 正常成员展示姓名、Token、成本、调用次数；右下明确显示“高用量、使用正常”。

### 10.4 工作室报告

在部门报告基础上增加：

- 各部门基础数据表，字段与工作室总览一致。
- 严重异常清单带部门。
- 四象限相对工作室中位数；点形或可见标签区分部门。
- 问题成员按部门分组；正常成员不另铺长表。

### 10.5 图表与文案修正

- 人数较少时散点直接显示姓名；人数多时默认 hover，支持搜索定位。
- 岗位差异显著时提供岗位筛选或分面，不把跨岗位绝对成本当结论。
- 日/月报告不得继续显示“本周总成本”“本周结论”。
- “已定性”改为“证据充分”更符合非定责语境；所有严重异常文案保留“待核查”。
- 报告路由加入菜单；普通用户入口与管理入口分开。

## 11. 性能、隐私与可观测性

### 11.1 性能

当前组织报告按成员重复查询当期、上期、输出、选型和异常数据。优化为 `ReportFactLoader.LoadScopeFacts()`：

- 当期和上期各一次窄列查询，按 user 分桶。
- 岗位、模型档位、备案窗口批量读取。
- 语义正文只读取规则候选 request IDs，并设置每报告调用上限。
- 关闭周期由后台任务生成；HTTP 只读快照。

补充索引：`call_logs(studio_id, ts)`、确认 `call_logs(dept_id, ts)` 和 `call_logs(user_id, ts)` 实际存在；快照按完整业务键建立唯一索引；异常按 user+period 建唯一索引。所有 schema 改动严格走 Atlas diff/hash/validate。

### 11.2 隐私

- 默认报告和快照不保存请求正文或 `content_samples`。
- 语义层只发送脱敏后的最小意图摘要，开关默认关闭，调用记录保留模型、规则、置信度、request ID 哈希，不记录正文。
- 明细下钻继续走 RBAC，并记录审计日志。
- Key 展示脱敏；IP、User-Agent 不进入报告 JSON。

### 11.3 指标与告警

新增：

- `usage_report_generate_duration_seconds{level}`
- `usage_report_snapshot_hit_total{state}`
- `usage_report_generation_failures_total{stage}`
- `usage_report_rows_scanned_total{source}`
- `usage_report_cost_reconciliation_gap_usd`
- `usage_report_rule_hits_total{issue,verdict}`
- `usage_report_data_coverage{field}`
- `usage_report_anomaly_pending_total`

日志带 `level/scope_id/period_type/period_start/rule_version`，不带正文。

## 12. 实施阶段与预期效果

### Phase 0：先锁口径和测试样本

- 建立 10-20 个黄金样本，覆盖三种成本、五类规则、规则重叠、失败请求、Service Key、跨部门、零用量、开放周期和关闭周期。
- 把 v0.6 公式写成 contract tests。

**效果**：后续每次修改都能判断“指标真的变准了”，而不只看页面是否能打开。

### Phase 1：P0 正确性修复

- 四项成本采集及三桶对账。
- request+bucket 级归因，修正 Y 轴。
- scope 全链路、独立 self API、TraceBodies 权限查询。
- 快照完整键和开放周期刷新。
- anomaly_review 生成、overlay、精确反馈。
- 修复日聚合整数除法和非法模型去重 SQL。

**效果**：成本、象限、权限、反馈和周期报告具备生产可信度。

### Phase 2：v0.6 报告内容补齐

- Report `overview/models/departments` 契约。
- 个人、部门、工作室缺失区块。
- Service Key 单列、问题成员按服务端标志分类。
- scope 选择器、菜单、自助个人报告、动态周期文案。

**效果**：三个角色看到的页面与 v0.6 第 5 节逐项一致。

### Phase 3：规则精度提升

- 采集 session/current user input/reset/task/scene/finish reason。
- 修正 3.2、3.3、3.4、3.5 的分母、双条件和真实会话逻辑。
- 真实计算数据质量，基线禁止未来泄漏。

**效果**：降低误报/漏报，让 suspected 与 confirmed 真正反映证据强度。

### Phase 4：规模化与历史复现

- Scope 级批量取数，消除成员 N 倍查询。
- 关闭周期快照任务和失败重试。
- 长期基线摘要或 91 天明细保留方案。
- 归档前报告完成度硬闸。

**效果**：工作室级报告稳定生成，历史报告可复现，不依赖 30 天热明细碰运气。

## 13. 测试与验收清单

### 后端单元测试

- 成本四项与总成本在普通价、阶梯价、缓存写价格下守恒。
- 两个不重叠问题费用相加；同一 request+bucket 重叠时只计一个主因。
- 失败请求计入成本但不进入行为规则。
- 五类规则分别覆盖 confirmed/suspected/not_hit/not_computed 和排除项。
- 周/月起始日规范化；快照日/周/月互不串读。
- 组织变更用户只贡献目标 scope 内的事件。

### PostgreSQL 集成测试

- 使用真实 PostgreSQL 验证 `percentile_cont`、`AT TIME ZONE`、JSONB、唯一索引和 upsert；SQLite 只保留纯逻辑测试。
- 日聚合、周期聚合与明细在同一闭合周期完成成本、请求、Token 对账。
- 管理角色跨 scope 请求返回 403；self API 不能读取他人。
- 语义开启时 manager 的 `call_traces` 查询仍受 scope 约束且 SQL 可执行。
- 报告生成后产生 anomaly_review；反馈后再次读取能看到新状态。

### 前端测试

- 三种角色可见 level/scope 正确，API 请求一定带规范 scope ID。
- 日/周/月切换后标题和周期一致。
- 部门/工作室严重异常置顶清单显示成员数据。
- `is_problem_member=true` 的成员不会进入正常列表。
- 正常列表展示 Token、成本、调用次数；右下文案不含负面判断。
- 数据覆盖不足、全正常、零用量、请求失败均有明确状态。

### 上线验收指标

- 抽样报告成本与 SQL `SUM(est_cost_usd)` 差异 <= `$0.01`。
- 所有成员 `0 <= issue_cost_ratio <= 1`，且与原子 Claim 复算一致。
- 日、周、月同起始日分别命中正确快照。
- 关闭周期重复读取内容一致；开放周期在约定新鲜度内更新。
- 跨角色、跨部门、跨工作室越权用例全部拒绝。
- 3.6 命中、待核查、反馈、回显、备案形成完整闭环。
- 工作室范围的报告生成耗时和扫描行数达到压测预算后再开放默认入口。

## 14. 文件级实施清单

| 优先级 | 任务 | 主要文件 | 产出与验收 |
|---|---|---|---|
| P0 | 成本四项化与守恒 | `internal/gateway/observability/cost.go`、`collector.go`、`event.go`、`internal/observability/model/call_log.go` | 普通价/阶梯价/缓存读写测试中，四项之和等于 `est_cost_usd` |
| P0 | 成本字段迁移与回填 | `internal/observability/model/*`、`tools/atlas-loader/main.go`、`atlas-migrations/` | Atlas 生成迁移；旧数据残差回填；覆盖率可查询 |
| P0 | 原子 Claim 归因 | `issue_*.go`、`cost_attribution.go`、`report_orchestrator.go` | 不重叠问题正确相加，重叠的同一费用原子只计一次 |
| P0 | Scope/RBAC 收口 | `report_handler.go`、`report_provider.go`、`usage_policy.go` | 每条查询同时受 actor scope 和 report scope 限制 |
| P0 | 快照键与新鲜度 | `report_snapshot.go`、`report_snapshot_service.go`、`report_handler.go` | 日/周/月与规则版本互不串读；开放周期按 TTL 更新 |
| P0 | 严重异常闭环 | `anomaly_review.go`、`report_snapshot_service.go`、`report_orchestrator.go` | 生成、反馈、回显、备案均可端到端完成 |
| P1 | 数据质量实算 | `report_provider.go`、`report.go`、`ReportDataQuality.vue` | 每个覆盖率可由同 scope SQL 复算，缺字段不显示 100% |
| P1 | 基线时序修正 | `baseline_service.go`、`report_baseline_inline.go` | 历史报告只读报告期之前的基线，返回样本数和实际窗口 |
| P1 | 聚合 SQL 修正 | `rollup_service.go`、`period_rollup.go`、对应 cron | 比率、模型数、时区、当前周期刷新与明细对账 |
| P1 | Report 契约补齐 | `internal/observability/report/report.go`、`assembler.go`、`web/src/types/usage-report.ts` | `overview/models/departments` 三端一致 |
| P1 | 三类页面补齐 | `UsageReportPage.vue`、`components/usage-report/`、`App.vue`、router | v0.6 第 5 节逐块可见，scope 可选，自助入口可达 |
| P1 | 组织查询批量化 | 新增 `report_fact_loader.go`，收敛 `report_provider.go` | DB 查询次数不随成员数线性增长 |
| P2 | 精确会话与场景采集 | gateway handler/orchestrator、`UsageEvent`、`CallLog` | 会话、当前输入、reset、task/scene、finish reason 有覆盖率 |
| P2 | 语义增强隐私收口 | `semantic_upgrade.go`、快照序列化、审计模块 | 默认关闭；正文不落报告；仅候选摘要出域且可审计 |

建议每个表格行拆成独立 PR。涉及 schema 的 PR 严格执行 Atlas 流程；成本字段与会话字段使用不同迁移，避免一次迁移扩大回滚面。

## 15. 预期优化效果

| 目标 | 当前风险 | 上线后可观察效果 |
|---|---|---|
| 成本可信 | 缓存写和阶梯价导致分桶不守恒 | 报告与账单抽样差异稳定在 `$0.01` 内 |
| 象限可信 | Y 轴近似只保留最大问题类型 | 原子 Claim 复算与页面 Y 值完全一致 |
| 降低误报 | 总 Prompt 冒充当轮输入、未知 reset 冒充未 reset | 3.2/3.3 的 `not_computed/suspected` 比例可解释，人工误报率下降 |
| 降低漏报 | 非重叠问题被错误互斥 | 多类真实问题可同时贡献 Y 轴，不再只显示一种 |
| 角色可用 | 页面不传 scope，普通用户没有入口 | 本人、部门负责人、工作室负责人、平台管理员均能进入正确范围 |
| 周期稳定 | 日/周/月快照串读，开放周期不刷新 | 关闭周期可复现，开放周期有明确数据截止时间 |
| 核查闭环 | 有反馈按钮但没有待核查记录 | 负责人反馈成功率可监控，刷新后状态即时一致 |
| 页面完整 | 缺基础数据和组织分层内容 | 三份报告逐块满足 v0.6 第 5 节 |
| 查询可控 | 组织报告按成员放大 DB 查询 | 查询次数固定化，工作室报告 P95 达到压测目标 |
| 隐私可控 | 摘要样本可能进入快照 | 快照不含正文，语义调用最小化、脱敏且可审计 |

## 16. 发布与回滚

数据库改动采用 Expand-Contract：先加列和新索引，再双写/回填，确认覆盖率后切读，最后才移除旧逻辑。所有迁移通过 `make atlas-diff` 生成，执行 `atlas migrate hash --env local` 和 `atlas migrate validate --env local`，不得手改 `atlas.sum`。

发布顺序：

1. schema 扩展与兼容读取；
2. gateway 双写新成本/会话字段；
3. control-plane 新归因和新快照影子计算；
4. 对账新旧报告并观察至少一个完整周周期；
5. 前端按角色灰度开放；
6. 关闭旧快照读取和用户级伪归因。

回滚时保留新增列和新表，只切回旧读路径；不做立即 DROP。新旧报告使用不同 `rule_version`，确保灰度与回滚期间不会互相覆盖。

## 17. 最终 Definition of Done

只有同时满足以下条件，才可宣称“v0.6 全部满足”：

1. 成本三分桶与总成本对账，缓存写入和失败成本口径正确。
2. 五类规则在正确粒度产生 Claim，Y 轴为去重后的五类合计。
3. 会话/场景缺失时按规则降级，不再伪造 100% 覆盖或强结论。
4. 个人、部门、工作室报告逐块覆盖 v0.6 第 5 节。
5. Scope 选择、RBAC、快照身份、新鲜度和历史复现均有集成测试。
6. 严重异常在组织报告置顶，并完成待核查记录、反馈回显和备案排除闭环。
7. 页面、Markdown、HTML 消费同一 Report JSON，关键数字逐项一致。
8. Go 测试、PostgreSQL 集成测试、前端组件测试、生产构建和 Atlas 校验全部通过。
