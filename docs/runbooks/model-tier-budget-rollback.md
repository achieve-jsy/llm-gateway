# 模型等级预算回滚 Runbook

## 普通回滚

先关闭正数阻断，保留可观测影子态：

```bash
export BUDGET_TIER_MASTER_ENABLED=true
export BUDGET_TIER_ACCOUNTING_ENABLED=true
export BUDGET_TIER_POSITIVE_ENFORCEMENT_ENABLED=false
export BUDGET_TIER_ZERO_BLOCK_ENABLED=true
export BUDGET_TIER_SHADOW_ENABLED=true
export BUDGET_V2_TOTAL_ENFORCEMENT_ENABLED=false
export BUDGET_LEGACY_TOTAL_WRITE_ENABLED=true
```

重新部署当前版本以发布新 snapshot。确认非 `$0` 请求恢复后，如应用版本异常：

```bash
cd /opt/llm-gateway
bash deploy-blue-green.sh --rollback
```

回滚后验证 `/ready`、旧总预算、调用日志写入和旧 key 双写。不要删除 v2 keys、等级快照或历史日志。

## 紧急全关闭

当 `$0` 误拦截、权限顺序错误或 Redis v2 数据不可解释时：

```bash
export BUDGET_TIER_MASTER_ENABLED=false
export BUDGET_TIER_ACCOUNTING_ENABLED=false
export BUDGET_TIER_POSITIVE_ENFORCEMENT_ENABLED=false
export BUDGET_TIER_ZERO_BLOCK_ENABLED=false
export BUDGET_TIER_SHADOW_ENABLED=false
export BUDGET_V2_TOTAL_ENFORCEMENT_ENABLED=false
export BUDGET_LEGACY_TOTAL_WRITE_ENABLED=true
```

重新部署 Control Plane/Gateway，使 snapshot 进入安全模式。旧总预算仍由 legacy 路径执行。

## 数据恢复

1. 冻结配置写入并保存异常时间窗、snapshot revision 与 request IDs。
2. 调用 `POST /api/v1/budget/rebuild-counters`；若 `cutover_ready=false`，不得启用任何 v2 enforcement。
3. 以 `call_logs` 为事实源重建当前日/周/月 generation；只计入
   `budget_tier_accounting_eligible=true` 的 tier 日志。
4. 确认 inflight 归零、Stream drain 完成和 lease 仍归当前 rebuild 后，才允许原子切换 generation。
5. 禁止手工覆盖活动 counter、删除历史 generation 或回退 Expand migration。

## 数据库原则

本功能迁移为 Expand migration，应用回滚不执行 down migration。保留新表/列供旧版本忽略；修复通过后续向前迁移完成。

## 回滚完成标准

- 非 `$0` 和 `$0` 行为符合当前开关。
- 旧总预算无回归，调用日志继续写入。
- snapshot revision 在所有实例一致。
- Redis 与日志差异恢复到 `< max(0.1%, $1/day)`。
- 审计记录包含操作者、原因、前后模式和关联事件。
