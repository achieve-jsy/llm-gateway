# 阶段一用量周报 T6 代码级评审（终版，覆盖 T4 PENDING）

> 作者：code-reviewer（t6 代码级部分）。覆盖并取代 t4 review.md 的「待 T3 / PENDING」，基于 `.worktrees/from-main` 当前稳定代码（t7 对账修正后 `go build/vet/test` 全绿、`make build` 目标 `cmd/gateway`+`cmd/control-plane` 不受影响）。
> 评审对象：`tools/usage-report/`（薄入口）+ `internal/observability/report/`（纯库）。独立 Go 二进制；只读、零库、不改 router/runtime、不新增端点。
> **本次范围（代码级）**：正确性/可维护性/安全性、口径不漂移、sql-analyst 已知残留（report-schema.md §10.6）标注核验、存量编译错是否为仓库既有。
> **真实数据对账 + A1–A5 真实命中校准**因需 admin JWT，作为**独立后续事项**（captain 拿到 token 后再补），本次**不阻塞**。
> **评审结论**：构建/校验/口径/安全/数据来源/大部分残留标注——通过；发现 4 项代码缺陷（1 致命、1 高、2 中）+ 1 项测试不足 + 1 项残留标注未落到输出层。

> **t10 修复状态（本轮已落地）**：下列缺陷已由 code-reviewer 于 t10 修复，`go build/vet/test` 全绿、`make build` 不受影响、gofmt 干净、未改动任何既有 tracked 文件。
> - ✅ **1.1 单元级事实为空**：`aggregateDaily` 按 `-level` 设 `ScopeID`（personal=user_id/department=dept_id/studio=studio_id）+ `fetchScopeLogs` 服务端 `dept_name`/`studio_id` 过滤 + studio 客户端按 `StudioName` 兜底。
> - ✅ **1.2 28 天基线物化**：新增 `ReportInput.BaselineMetrics`，工具拉期前 28 完整自然日（`from-28d ~ from-1d`）成员口径日聚合，`buildBaselineSeries` 用之；无基线窗数据时降级为「当前周期近似(未物化,仅供自一致参考)」并如实标注。
> - ✅ **输出层标注**：`baseline.go` Note 与 `render_md.go`/`render_html.go` 基线标题均附「基线窗=期前28日物化 / 当前周期近似」；`main.go` `meta.qa` 增基线来源说明。
> - ✅ **1.3 时区**：`ToCallEvent` 解析后统一 `.In(Asia/Shanghai)`。
> - ✅ **1.4 单元级 A1–A5 语义**：异常判定跑在成员调用流（`nonService` 剔除 service），并注入 `Dimensions["scope"]` + 单元级 Note 说明 A1/A2 个人语义仅在 personal 完整成立。
> - ✅ **1.5 测试**：新增 `TestA1ProgramLoadHit`/`TestA2CostAnomalyHit`/`TestUnitDepartmentFact`(department 事实非空)/`TestBaselineMaterialized`/`TestBaselineDegraded`，并在 `TestDeriveAndRender` 补 A3 命中断言。
> - ⭕ **1.6 接受不改**（单元级归因/同岗分位天然为空，属设计取舍）。
>
> 下列各节为**评审时的原始发现**（保留用于追溯），已在上述 t10 修复中解决。


---

## 0. 结论速览

| 维度 | 结论 | 说明 |
|---|---|---|
| 构建/校验 | ✅ | `go build ./internal/observability/report/...` + `./tools/usage-report/...`、`go vet`（两包）、`go test ./internal/observability/report/...`、`make build`(cmd/gateway+cp) 全绿，gofmt 干净。 |
| 口径 | ✅ | `total_tokens=prompt+completion+reasoning`（不含 cached/cache_write）、`success=status=200`、个人/岗位人均排除 `key_type='service'`、dept_id 归一、CNY=`est_cost_usd×rate`（记 rate/source/at）——均与 impl-notes 一致；**未混拼 `/usage/dashboard/users*` 存储列**，对账基准 `GetDashboard`。 |
| 安全/运行时 | ✅ | 零库、零新端点、不改 router/runtime；admin JWT 仅内存（`cfg.Token`/`client.token`），不写 roster.json/report/日志，错误仅 path+body 不含 token；唯组织元数据，不取请求正文/PII；stdlib only；make build 不受影响。 |
| 数据来源落地 | ✅/⚠️ | A4 上下文上限来自 `model_catalog`（`context_length - max_output_tokens`，client:389）；A5 仅时间窗触发（阶段一无备案表，anomalies:447-448）。 |
| 代码缺陷 | 🔴/🟠 | 1) 单元级事实为空；2) 28 天基线自引用；3) ts 时区；4) 单元级 A1–A5 语义错位。 |
| 测试 | 🟡 | 仅 personal 单测，A1/A2 命中未断言、单元级未覆盖（1.5）。 |
| 残留标注 | ✅/⚠️ | A4 scene / A5 floor / personal diff 已注明；**28 天基线近似未在报告输出层标注**（见 §2.5）。 |
| 真实对账/A1–A5 真实命中 | ⏸️ | 独立后续（需 admin JWT），见 §6。 |

---

## 1. 代码缺陷（按严重度）

### 1.1 🔴 部门/工作室级事实为空（影响所有单元级报告）
- **位置**：`tools/usage-report/main.go:199`（`aggregateDaily` 硬编码 `Level: "personal", ScopeID: c.UserID`）+ `internal/observability/report/derive/derive.go:151`（`buildUnitFact` `if u.ScopeID != m.ScopeID { continue }`）+ `main.go:162`（`fetchScopeLogs` 拉数 `FetchLogs(...,"","")` 未服务端过滤单元）+ `main.go:182-184`（studio 客户端过滤为空操作）。
- **根因**：`DailyMetric.ScopeID` 语义应为 personal=user_id / department=dept_id / studio=studio_id（`metric.go:16`），但 `aggregateDaily` 不问 `-level` 都置 `user_id`；`buildUnitFact` 以 `u.ScopeID(dept_id/studio_id) != m.ScopeID(user_id)` 匹配 → 全部丢弃。单元级拉数未做范围过滤，studio 过滤为空操作。
- **影响**：`--level department|studio` 的 `UnitFact`（member/service）全 0，judgment/expression 随之为空；`reconcileWithDashboard` 单元级显示「差 = -GetDashboard.TotalTokens」巨差（空事实症状，非真口径差）。阶段一默认 `-level personal` 才正确。
- **修法**：`aggregateDaily` 按 level 设 `ScopeID`（department→`DeptID`、studio→`StudioID`）+ `fetchScopeLogs` 传 `dept_name`/`studio_id` 服务端过滤；或 `buildUnitFact` 按 `m.DeptID`/`m.StudioID` 分组（`DailyMetric` 需加这 2 字段）。
- **决策点**：阶段一是否交付 department|studio；不交付则 `-level` 拒绝/标注「未支持，勿用」，避免误导性空报告。

### 1.2 🔴 28 天基线未真正实现（自引用/循环，且输出层未标注）
- **位置**：`derive/derive.go:312-336`（`buildBaselineSeries`：`vals` 取当前周期 `byDay`，`current`=当前周期日均）；`derive/anomalies.go:197-206`（A2「28 天同窗中位」用当前周期内分钟窗成本中位数）。
- **影响**：基线的 median/MAD/稳健上界/分位 ≈ 当前周期自身分布，无法刻画「相对历史」偏离；A2「×5 基线」实际是「相对当前周期自身中位 ×5」。且 `derive/baseline.go:121` Note 写「28 天稳健基线：中位数+1.4826×MAD」——**在报告输出层被当作真实 28 天基线呈现**，而实际是当前周期近似（见 §2.5 标注缺口）。
- **修法**：工具拉两段（当前窗 `[from,to]` + 前 28 天 `[from-28d, from-1d]`）作为基线序列传入 `ReportInput`（增 `BaselineMetrics`）；或明确降级为「自一致参考」并在 `Meta.QA`/`NotComputed` 注明。

### 1.3 🟠 ts 时区（A5 非工作时段 + 日切可能 +8h 偏移）
- **位置**：`client/usage_client.go:407-410` `ToCallEvent` 解析 naive ts 按 UTC（`time.Parse("2006-01-02 15:04:05", ts)`）。若后端 `ts` 为无时区/UTC 串，则 `c.TS.Hour()`（A5：anomalies:465）与 `c.TS.Format("2006-01-02")`（日切：main.go:195）相对 Asia/Shanghai +8h。
- **需实测**后端 `ts` 格式；RFC3339 带 `+08:00` 则无碍。建议解析后强制 `.In(asiaShanghaiLoc)`（location 可由 config 注入）。

### 1.4 🟠 A1–A5 在部门/工作室粒度语义错位
- **位置**：`main.go:174-176` 仅 personal 排除 service；department/studio 不排除 service 且聚合为单元调用流。A1/A2（窗口/间隔/28 天中位）为「个人」规则，对整部门/工作室聚合流运行在语义上不成立；A5 off 占比会混入 service。
- 当前被 1.1 掩盖；即使修 1.1，也应明确 A1–A5 仅 personal 语义，在单元级降级 `NotComputed` 或按「成员粒度」运行。

### 1.5 🟡 测试覆盖不足
- **位置**：`derive_test.go:36` 仅 `TestDeriveAndRender`（只 `-level personal`）；断言 A5/A4 Hit（63-76），A3 场景存在但未断言，A1/A2 命中未断言，department/studio 未覆盖 → 1.1 未被测试捕获。
- 建议补：A1/A2 命中断言、单元（department/studio）用例、基线循环守卫断言。

### 1.6 🟡 单元级归因/同岗分位天然为空（设计取舍，可接受，非 bug）
- `derive.go:307-310` `topModelsFromUnit` 恒 nil → 单元级 `Attribution` 空；`derive.go:364-375` `cohortCurrentMetric` 仅 personal 非 0 + `buildCohort` 在 `cfg.Scope=dept_id/studio_id` 时 `roster[cfg.Scope]` 查不到用户 → 单元级 `peer.supported=false`（`NotComputed`）。同岗分位本质 per-user，属预期，但报告应注明。
- （注：`poolFor` 已实现 §10.9 的 4 级回退链：同 studio+同 jobKey → 全公司同 jobKey → 同 studio 任意 → 全公司任意，见 `cohort.go:133-157`。）

---

## 2. 口径对账（静态核验，与 impl-notes 一致）

| 口径 | 结果 | 代码 |
|---|---|---|
| success = `status=200` | ✅ | `main.go:203-207` |
| `total_tokens=prompt+completion+reasoning`（不含 cached/cache_write） | ✅ | `metric.go:31,55`；`main.go:213`；`ToCallEvent`(client:421) |
| 个人/岗位人均排除 `key_type='service'` | ✅ | `main.go:42-44,174-176`；cohort/derive 均排除 |
| 部门 Service Key 块仅 `=service` 且归属当前部门 | ✅（仅 personal 生效；单元级空被 1.1） | `derive.go:154-165` |
| dept_id 归一 | ✅ | `main.go:178-181` |
| CNY=`est_cost_usd×rate`（记 rate/source/at） | ✅ | `main.go:73`；config `-fx-rate`；A2/A5 rate 折算 |
| 对账基准 `GetDashboard`，不混拼 `users*` | ✅ | `main.go:253-274`；`client.FetchDashboard` |

- ⚠️ `reconcileWithDashboard` personal 已注明「GetDashboard(scope=user) 含 service key、personal 画像排除 service → 差异≈service 贡献」；**单元级因 1.1 空事实会显示巨大差值，须先修 1.1**。

---

## 2.5 sql-analyst 已知残留（report-schema.md §10.6）标注核验

| 残留 | 是否在输出/代码层面标注 | 证据 |
|---|---|---|
| **28 天基线为近似、未物化** | ⚠️ **仅在文档+代码注释**（`report-schema.md §10.6/§10.9`；`derive.go:313/331` 注释），**未落到报告输出层**：`baseline.go:121` Note 仍写「28 天稳健基线」，`render_md.go:83` 标题「28 天基线」——消费者会误读为真实 28 天基线。 | `baseline.go:121`；`render_md.go:83`；`derive.go:331` |
| **A4 分支2（IO 比同场景 P99）未启用** | ✅ 已标注 | `anomalies.go:432` Note「分支2 依赖 scene 埋点，未埋点前默认禁用/灰度（阶段一不启用）」 |
| **A5 同岗位 P99 + 降解链回退仅 floor** | ✅ 已标注 | `anomalies.go:447`；`a.BaselineWindow="max(同岗位P99,¥100)；阶段一降解链→仅floor"`(496)；Note(519)；`peerLevel="仅floor(¥100)"`(477) |
| **personal GetDashboard 含 service key 对账差** | ✅ 已标注 | `main.go:254/271-272`「personal 范围 GetDashboard(scope=user) 含 service key，而 personal 排除 service → 差异≈service 贡献」 |
| A4 模型上下文未命中回退 `A4MinPromptAbsolute` + 记录来源 | ✅ 已标注 | `anomalies.go:392-395/417-421`（BaselineWindow 标「保守下限(未录上下文)」或「有效输入预算」） |
| A5 无备案治理表 → 纯时间窗触发（阶段二补豁免带 until） | ✅ 已标注 | `anomalies.go:448/519` Note |

**结论**：除「28 天基线近似」外，其余残留均在代码/输出层面正确标注。**建议**：在 `Meta.QA.Alignment`/`Baseline.Note` 增加「阶段一基线为当前周期近似（未拉前 28 天物化窗），仅供自一致参考」的显式说明，避免误读为真实历史基线。

---

## 3. 安全 / 运行时边界

- ✅ **不直连 DB**：全走 `/api/v1/usage/*`、`/org/*`、`/model-catalog/*`（client）；无 repo/service/DB 调用。
- ✅ **不新增端点、不改 router/runtime**：纯新增 `internal/observability/report/` + `tools/usage-report/`；主仓库 `tools/usage-report` 已清理；`make build`(cmd/gateway+cp) 不受影响。
- ✅ **密钥**：admin JWT 仅 `cfg.Token`/`client.token`（内存）；不写文件/report/日志；错误仅 path+body，不含 token。
- ✅ **无 PII/请求正文**：花名册只取 id/name/email/job_* /studio/dept；不取请求正文/响应体用户内容；roster 不落盘；报告仅 metadata（匿名分位，不指名）。
- ✅ **依赖**：stdlib only（net/http/encoding/json/time；client 引用 `internal/observability/report` 仅取类型）。
- ⚠️ **权限**：`/org` 全量 roster 需 admin(`IsGlobal`)+`PermMemberView`；非 admin 仅可见自己范围、普通用户 403。已以 `skipped_depts` + `NotComputed` 处理 403/404/429/5xx；客户端 429/5xx 指数退避（3 次），非 admin 越权 403 不重试。

---

## 4. A1–A5 确定性 / 命中校准（合成用例）

- 引擎确定性可复现；命中必附 evidence（阈值/实际/基线/影响成本/窗口），未命中附 `basis`（anomalies.go）。
- **当前测试覆盖**：断言 A5（周末非工作成本 > HARD_CAP）、A4（近上限 prompt ≥2）；A3 场景存在未断言；A1/A2 命中未断言（见 1.5）。
- **阈值**（阶段一默认，来自 impl-notes/T4）：`metric.go DefaultRuleSet(135-168)`。
- **已落地误报防控**：样本不足（<2 或 <`A5MinCallCount`）→ `basis` 未命中；A1/A2/A5 有绝对下限/floor/HARD_CAP；A3 区分上游 5xx vs 客户端 4xx；A4 用保守下限 `A4MinPromptAbsolute`；同岗 <5 → `NotComputed`（不跨岗裸排名，peer 匿名）。
- ⏸️ **真实数据 A1–A5 命中/误报校准**：需 admin token 在真实周期跑一遍人工比对（见 §6），独立后续。

---

## 5. 存量 test/contract 编译错（仓库既有，非本轮引入）

`go build ./...` / `go vet ./...` 被以下**既有**编译错阻断（与本工具同机 `codex/from-main` 分支、优先级在我重构之前，我零改动既有 tracked 文件）：
- `test/bedrock_model_id_check.go:98` `undefined: bedrock.NewListFoundationModelsPaginator`
- `tests/contract/model_tier_budget_contract_test.go:253` `BudgetTierHandlerService` 缺 `QueryScopedTierUsage`
- `tests/gateway/orchestrator/{group_auth_test,handler/embeddings_test}.go` `fakeScheduler`/`fakeEmbeddingScheduler` 的 `UpdateSticky` 签名不符
- `tests/gateway/middleware/auth_jwt_studio_test.go:98` `AuthMiddleware` 缺 `JWTManager` 参数

这些已在 t9 报告中归为「无关存量」，由 captain 作为独立事项上报需求方；**非本次 report/usage-report 引入**。本工具 scoped 构建（report 纯库 + usage-report 入口 + make build）不受影响。

---

## 6. 真实数据对账 + A1–A5 真实命中校准（独立后续，需 admin JWT）

- **状态**：控制面 `:8081` 可达且 `/api/v1/*` 需 JWTAuth（401）；本环境无 admin token。按 org-roster.md 安全红线（admin JWT 仅内存/枚举、不锻造、不读出/不落盘），交由 operator 在注入 token 后执行。
- **运维命令（token 经 env 注入不落盘）**：
```bash
USAGE_REPORT_TOKEN=<admin JWT> go run ./tools/usage-report \
  -base http://127.0.0.1:8081/api/v1 -level personal -scope <user_id> \
  -from <YYYY-MM-DD> -to <YYYY-MM-DD> -json-out /tmp/report.json -debug
# 对账（report.json facts_digest.total_tokens vs GetDashboard summary.total_tokens）：
curl -s -H "Authorization: Bearer $USAGE_REPORT_TOKEN" \
  'http://127.0.0.1:8081/api/v1/usage/dashboard?scope=user&id=<user_id>&start=<from>&end=<to>&page=1&page_size=20'
```
- **预期差异**：personal 报告排除 service，而 `GetDashboard(scope=user)` 含该用户 service key → 差异≈service 贡献（已注明）。

---

## 7. 建议优先级

1. **必修 1.1**（单元级空事实）——先拍板阶段一是否交付 department|studio；交付则修 `aggregateDaily`/`buildUnitFact`+服务端过滤，否则 `-level` 拒绝/标注。
2. **必修 1.2**（28 天基线）——拉前 28 天窗，或降级为「自一致参考」并在 `Meta.QA`/`Baseline.Note` 显式标注（同时补 §2.5 的输出层标注缺口）。
3. **验证 1.3**（ts 时区）——确认真实 `ts` 格式，必要时强制 `Asia/Shanghai`。
4. **明确 1.4**（A1–A5 仅 personal 语义；单元级降级 `NotComputed`）。
5. **补 1.5 测试**（A1/A2 命中断言 + 单元级用例）。

> 说明：上述修复 captain 已新建 **t10**（code-reviewer）承接。本评审仅记录发现，不直接大改；我只写了 `docs/review.md`，未触碰实现/口径定稿文件。

---

## 附：源码索引

- `internal/observability/report/`: `report.go`(契约)、`metric.go`(DailyMetric/CallEvent/CohortDef/RuleSet)、`derive/{derive,baseline,anomalies}.go`、`render/{render_md,render_html}.go`、`derive/derive_test.go`。
- `tools/usage-report/`: `main.go`、`config.go`、`cohort.go`、`client/usage_client.go`、`README.md`。
- 规范文档：`docs/recon.md`(T1)、`docs/report-schema.md`(T2)、`docs/org-roster.md`(T5)、`docs/usage-report-impl-notes.md`。
