# 预算 402 用户可读提示（v2）

## 目标

Gateway 因总预算或等级预算拦截返回 HTTP 402 时，`error.message` 使用带周期/等级细节的中文提示，便于终端用户与客户端直接展示。同时生产只走 v2 预算路径，不再依赖旧总预算双写。

## 背景

- 旧路径 `BudgetExceededError` 已有中文「预算已用尽：{标签}，请联系负责人扩容」。
- v2 `budget.ExceededError` 当前固定英文 `budget limit exceeded`，用户看不到「用尽」语义与周期/等级信息。
- 生产将开启 v2 总预算拦截与等级正数/$0 拦截，并关闭 `legacy_total_write`；402 文案只服务 v2 Gate。

## 范围

**做**

- 仅改 `internal/gateway/budget` 中 `ExceededError.WriteHTTPResponse`（及可抽的 message 构造函数）。
- 补充/更新单元测试，断言中文 message 与既有 `code`/`type`/`gate`/`additional_constraints`。

**不做**

- 不改管理面 UI、不改 call_logs schema。
- 不改拦截判定逻辑、不改 Redis key。
- 不扩展新的 JSON 错误字段（`message_en` 等）。
- 不要求同步改旧 `middleware.BudgetExceededError`（旧写路径关闭后不再作为主路径）。

## 响应契约

保持现有结构，仅替换 `error.message`：

```json
{
  "error": {
    "message": "超高级模型月预算已用尽，请联系负责人扩容",
    "type": "tier_budget_exceeded",
    "code": "tier_budget_exceeded",
    "gate": "budget"
  },
  "additional_constraints": []
}
```

- HTTP：`402 Payment Required`
- `code` / `type`：不变（`budget_exceeded` | `tier_budget_exceeded`）
- `gate`：`budget`
- `additional_constraints`：行为不变

## message 规则

输入取自 `ExceededError.Primary`：`Dimension`、`PeriodType`、`TierName`、`StaticZero`。

周期文案：

| `PeriodType` | 文案片段 |
|--------------|----------|
| `daily` | 日 |
| `weekly` | 周 |
| `monthly` | 月 |
| 其他/空 | 周期（兜底，不应出现） |

主体规则：

| 条件 | message |
|------|---------|
| `Dimension=tier` 且 `StaticZero` 且有 `TierName` | `{TierName}{周期}预算为 0，无法调用，请联系负责人开通` |
| `Dimension=tier` 且 `StaticZero` 且无 `TierName` | `该模型等级{周期}预算为 0，无法调用，请联系负责人开通` |
| `Dimension=tier` 且非零 且有 `TierName` | `{TierName}{周期}预算已用尽，请联系负责人扩容` |
| `Dimension=tier` 且非零 且无 `TierName` | `该模型等级{周期}预算已用尽，请联系负责人扩容` |
| `Dimension=total` 且 `StaticZero` | `总预算{周期}限额为 0，无法调用，请联系负责人开通` |
| `Dimension=total` 且非零 | `总预算{周期}限额已用尽，请联系负责人扩容` |

示例：

- `超高级模型月预算已用尽，请联系负责人扩容`
- `总预算日限额已用尽，请联系负责人扩容`
- `超高级模型日预算为 0，无法调用，请联系负责人开通`

不在 message 中暴露 `scope_id`、内部 UUID、Redis key。

## 实现要点

1. 新增纯函数（如 `exceededMessage(Constraint) string`），`WriteHTTPResponse` 只调用它填 `error.message`。
2. 未知 `PeriodType` 用「周期」兜底，保证永不返回空 message。
3. 不把 `Label`（部门/事业部展示名）拼进默认文案，避免与「总预算/等级」语义混淆；后续若产品要带 scope 标签可另开需求。

## 运营开关（发布侧，非本 PR 代码）

生产 GitLab 变量建议：

| 变量 | 值 |
|------|----|
| `PROD_BUDGET_TIER_MASTER_ENABLED` | `true` |
| `PROD_BUDGET_TIER_ACCOUNTING_ENABLED` | `true` |
| `PROD_BUDGET_TIER_POSITIVE_ENFORCEMENT_ENABLED` | `true` |
| `PROD_BUDGET_TIER_ZERO_BLOCK_ENABLED` | `true` |
| `PROD_BUDGET_V2_TOTAL_ENFORCEMENT_ENABLED` | `true` |
| `PROD_BUDGET_TIER_SHADOW_ENABLED` | `false` |
| `PROD_BUDGET_LEGACY_TOTAL_WRITE_ENABLED` | `false` |

开关变更靠 `deploy-prod` 注入；本设计文档对应的代码变更是 402 文案。

## 测试

- 表驱动：覆盖 total/tier × daily/weekly/monthly × 有/无 TierName × StaticZero。
- 既有 `TestMiddleware_IncludesBudgetTierInfoIn402Response`：增加对中文 message 的断言（补 `TierName`/`PeriodType`）。

## 验收

1. 单元测试通过。
2. 本地或测试环境打开拦截后，超额请求 402 的 `error.message` 符合上表。
3. `code`/`type` 仍为机器可读英文常量，客户端兼容不受影响。
