# 模型等级预算灰度上线 Runbook

## 前置门禁

- 全部 Gateway `/ready` 返回 `capabilities.tier_snapshot_schema=1`、`redis_v2=true`。
- Atlas validate、空库 apply、旧 schema 升级、Go/前端/集成测试全部通过。
- Redis Lua p99 `<5ms`，预算检查额外 p99 `<2ms`，CPU 增幅 `<5%`。
- 迁移只做 Expand，不删除或收窄旧字段。

## 完整周期影子态

先固定为：

```text
master=true
accounting=true
positive_enforcement=false
zero_block=false
shadow=true
v2_total_enforcement=false
legacy_total_write=true
```

启用月额度时至少观察一个完整自然月；只启用周额度时至少观察一个完整上海预算周。每天调用
`POST /api/v1/budget/rebuild-counters`，归档响应 JSONL、指标截图和异常说明。

进入阻断前必须同时满足：

- 每个 scope/tier 的差异 `< max(0.1%, $1/day)`，且 24 小时内无增长趋势。
- `gateway_budget_fail_open_total{dimension="tier"}` 不持续增长。
- `controlplane_budget_config_sync_total{result="failed"}` 24 小时为 0。
- 所有 Gateway snapshot revision 一致。

## `$0` 静态禁用

保持正数 enforcement 关闭，仅把 `zero_block=true`。按内部 Key 10 分钟 → 单部门 24 小时 → 全量扩大。
每阶段验证 HTTP 402、`error.code=tier_budget_exceeded`、上游调用数不增加；Redis 故障时 `$0` 仍须阻断。

## 正数额度

切换为 `master=true, accounting=true, positive=true, zero=true, shadow=false,
v2_total=false, legacy_total_write=true`。灰度范围由 Control Plane 中已配置 tier limit 的 scope 控制：

1. 内部 Key 1%
2. 单部门 5%
3. 单工作室 25%
4. 50%
5. 100%

每阶段至少观察 24 小时并跨一次上海日边界。非预期 402 `<0.1%`，对账、延迟和 CPU 必须继续满足门禁。

## 自动停止条件

任一条件触发立即停止扩大并关闭 `positive_enforcement`：

- 非预期等级 402 `>=0.1%` 持续 5 分钟。
- 对账差异超过 `max(0.1%, $1/day)`。
- tier fail-open 连续 5 分钟增长，或 Redis Lua p99 `>=5ms` 持续 10 分钟。
- Gateway 额外 p99 `>=2ms` 或 CPU 增幅 `>=5%` 持续 15 分钟。
- revision 分裂超过 60 秒，等级快照缺失率 `>0.01%`。
- `$0` 故障态未阻断、403/402 顺序泄露、schema drift 或重复记账。

触发后执行 [回滚 Runbook](model-tier-budget-rollback.md)。
