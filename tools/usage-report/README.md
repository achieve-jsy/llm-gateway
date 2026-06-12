# usage-report —— 阶段一离线用量周报工具

只读、零库权限、复用网关现有 RBAC/查询逻辑。数据面全部走 HTTP（`/api/v1/usage/*`、`/api/v1/my/usage*`、`/api/v1/org/*`、`/api/v1/model-catalog/*`），**不直连 DB、不新增端点、不改 gateway/control-plane 运行时**（`make build` 不受影响）。

## 架构（纯库 + 薄入口）

- 核心派生在 **`internal/observability/report/`**（纯库，数据源无关）：
  - `report.go` / `metric.go` —— 三层层级契约（fact/judgment/expression）+ 归一化中间表示 `DailyMetric`/`CallEvent`/`CohortDef`/`RuleSet`。
  - `derive/` —— `Derive(ctx, ReportInput, RuleSet)` 纯函数；`baseline.go`（28 天稳健基线 median+1.4826×MAD、同岗匿名分位）；`anomalies.go`（A1–A5，统一命中 schema）。
  - `render/` —— `render_md.go`/`render_html.go` 只读 Report，含表达层幻觉校验（裸数字降级）。
- 薄入口在 **`tools/usage-report/`**（`main.go`/`config.go`/`client/usage_client.go`/`cohort.go`）：HTTP 拉数 → 归一化 → `derive.Derive` → 渲染。

## 口径（与看板对账）

- 成功 = `status=200`。
- **`total_tokens = prompt + completion + reasoning`**（不含 cached/cache_write）；对账基准 `GET /api/v1/usage/dashboard`。
- 个人画像/岗位人均**排除** `key_type='service'`；部门 Service Key 块仅 `=service` 且归属当前部门。
- dept_id 归一：`d.dept_id = cl.dept_id OR d.id::text = cl.dept_id`；小时粒度 `ts AT TIME ZONE 'Asia/Shanghai'`。
- CNY 为换算（非判定）：`est_cost_usd × rate`（记录 `rate/source/at`，默认 7.2）。

## A1–A5（阶段一默认阈值，🔴 待 T4 用真实数据校准）

| 规则 | 判定 |
|------|------|
| A1 程序失控 | 10min 调用 > max(60, 个人P99_10min) 且中位间隔 <10s 且连续 ≥15min（1min 粒度滑动窗；豁免 service/`__system_`/备案） |
| A2 成本燃烧 | 15min 成本 > max(¥50, 个人28天同窗中位×5)；另 **HARD_CAP ¥200/15min** 独立触发 |
| A3 重试循环 | 段=(key_id,model)：req≥20 ∧ fail_rate≥30% ∧ 单一 status 占失败≥80% ∧ failed≥6（区分上游 5xx / 客户端 429/400） |
| A4 超长上下文 | 15min 窗口「prompt>80% 有效输入预算(context_length−max_output_tokens，来自 model_catalog)」≥2；单条 >80% 偏噪 |
| A5 非工作时段 | 00:00–08:00 或周末成本 > max(同岗P99, ¥100) 且非备案；阶段一无备案表 → 纯时间窗口触发并标注"阶段二补备案豁免" |

同岗分位样本 <5 回退（同 category+studio → 公司同 category → studio），仍 <5 → `NotComputed`（无依据不下结论）。

## 花名册（roster）取数与安全（规范见 `docs/org-roster.md`）

- 只走 HTTP：`/org/departments?status=active`（按 `total` 分页）→ 逐部门 `/org/departments/:dept_id/members?page_size=100` → `/org/dept-studio-map`。
- studio 解析：`departments.studio_id`（非空）→ `dept_studio_map.studio_id` → `""`。
- 同岗键取值链：`job_category → job_family → job_title → ""`。
- 去重以 `user_id` 为键（每用户单部门）；403/404/429/5xx 记录 `skipped_depts` 并继续（客户端 429/5xx 指数退避重试 3 次，非 admin 越权 403 不重试）。
- **必须 admin（IsGlobal）+ `PermMemberView` JWT** 才能拉全量（非 admin 只可见自己范围/普通用户 403）。
- 覆盖缺口：`users.dept_id IS NULL` 用户不在任何 `members` 中 → 不入 roster，报告在 `meta.qa` 标注（见 `docs/org-roster.md` §4）。
- 安全红线：admin JWT 仅在内存/env（`--token`/`USAGE_REPORT_TOKEN`），**不写** roster.json/report/日志；只取组织元数据，不取请求正文/PII。

## 运行

```bash
go build ./tools/usage-report
./usage-report \
  --base http://127.0.0.1:8081/api/v1 \
  --token $TOKEN \
  --level personal --scope <user_id> \
  --from 2026-08-01 --to 2026-08-07 \
  --json-out out/report.json --md-out out/report.md --html-out out/report.html
```

参数见 `config.go`；`--level` 为 `personal|department|studio`。需具有 `PermUsageViewAll` 的 token（全公司口径）；同岗分位需 `PermMemberView`。

## 测试

```bash
go test ./internal/observability/report/...
```
