# observability-hub Web 后端接口契约

## 1. 概览

**本契约描述**：observability-hub 的用量与成本只读查询接口。

| 属性 | 内容 |
| :--- | :--- |
| **生产方（Provider）** | observability-hub |
| **消费方（Consumer）** | ops-console（后续）、管理员/员工/部门负责人 |
| **所属变更** | CHG-20260603-001 |
| **创建时间** | 2026-06-03 |

## 2. 公共约定

| 约定 | 内容 |
| :--- | :--- |
| 协议 | HTTPS |
| 数据格式 | JSON |
| 认证方式 | JWT Bearer Token / API Key（复用网关 AuthMiddleware） |
| 日期格式 | YYYY-MM-DD |
| 价格单位 | USD/1M tokens |
| 成本单位 | USD（NUMERIC(18,6)） |

## 3. 端点索引

| 方法 | 路径 | 说明 | 认证 |
| :--- | :--- | :--- | :--- |
| GET | /v1/usage | 用量与成本查询 | JWT / API Key |
| GET | /health | 存活探针 | 无 |
| GET | /ready | 就绪探针 | 无 |

## 4. 端点详细设计

### 4.1 GET /v1/usage — 用量与成本查询

**认证**：JWT Bearer Token / API Key

**查询参数**：

| 参数 | 类型 | 必填 | 说明 |
| :--- | :--- | :--- | :--- |
| from | string | 是 | 起始日期（YYYY-MM-DD） |
| to | string | 是 | 结束日期（YYYY-MM-DD） |
| key_id | string | 否 | 按 Key 过滤（受视角约束） |
| user_id | string | 否 | 按用户过滤（受视角约束） |
| dept_id | string | 否 | 按部门过滤（受视角约束） |
| studio_id | string | 否 | 预留 |
| model | string | 否 | 按模型过滤 |
| group_by | string | 否 | 聚合维度：day / model / dept / studio / key（默认 day） |

**成功响应** (200)：

```json
{
  "from": "2026-06-01",
  "to": "2026-06-03",
  "group_by": "model",
  "rows": [
    {
      "model": "gpt-4o",
      "req_count": 12030,
      "prompt_tokens": 48200000,
      "cached_tokens": 31000000,
      "completion_tokens": 2100000,
      "est_cost_usd": 96.42
    },
    {
      "model": "claude-sonnet-4",
      "req_count": 8800,
      "prompt_tokens": 52000000,
      "cached_tokens": 15000000,
      "completion_tokens": 4100000,
      "est_cost_usd": 142.10
    }
  ],
  "total": {
    "req_count": 20830,
    "prompt_tokens": 100200000,
    "cached_tokens": 46000000,
    "completion_tokens": 6200000,
    "est_cost_usd": 238.52
  }
}
```

**字段说明**：

| 字段 | 类型 | 说明 |
| :--- | :--- | :--- |
| rows[].model | string | 模型标识 |
| rows[].req_count | int | 调用次数 |
| rows[].success_count | int | 成功次数（status=200） |
| rows[].prompt_tokens | int | 输入 Token 总数 |
| rows[].cached_tokens | int | 缓存命中 Token 总数 |
| rows[].completion_tokens | int | 输出 Token 总数 |
| rows[].est_cost_usd | number | 估算成本（USD） |
| total | object | 所有行的汇总值 |

**三级数据视角（鉴权注入）**：

| 角色 | 可见范围 | 注入 WHERE 条件 |
| :--- | :--- | :--- |
| 管理员 | 全量 | 无额外过滤 |
| 部门负责人 | 本部门 | `dept_id = caller.dept_id` |
| 普通员工 | 仅自己 | `user_id = caller.user_id` |

视角条件在服务层强制注入，前端传参不能越权放大范围。

## 5. 错误码

| HTTP 状态 | 错误码 | 说明 |
| :---: | :--- | :--- |
| 400 | INVALID_DATE_RANGE | from > to 或日期格式非法 |
| 400 | DATE_RANGE_TOO_LARGE | 时间范围超过 90 天 |
| 401 | UNAUTHORIZED | 未认证 |
| 403 | FORBIDDEN | 越权（如员工查询其他部门） |
| 500 | INTERNAL_ERROR | 数据库异常 |

## 6. 版本策略与 Breaking Change 规则

当前版本 v1，路径前缀 `/v1/usage`。后续兼容性变更时通过路径版本号区分（如 `/v2/usage`）。Breaking change 规则：
- 移除字段：至少一个版本提前标注 deprecated
- 新增必填字段：视为 breaking change，需升版本号
- 新增可选字段：非 breaking change

## 7. 非功能约束（适用时填）

| 约束 | 目标值 |
| :--- | :--- |
| P95 响应时间 | ≤ 500ms |
| 可用性 | ≥ 99.5% |
| 查询时间范围上限 | 90 天 |

## 8. 接口安全

| 策略 | 说明 |
| :--- | :--- |
| 认证 | 网关 AuthMiddleware，JWT / API Key |
| 授权 | 三级视角，服务层强制注入 |
| 越权防护 | 前端传参不可绕过 scope_filter |
| 访问审计 | 预留审计日志位（谁何时查了哪条） |
| 数据脱敏 | 计量表不存正文，Key 只存 key_id |

## 9. 变更历史

| 版本 | 日期 | 变更 | 作者 |
| :--- | :--- | :--- | :--- |
| v1.0 | 2026-06-03 | 初稿创建 | OpenCode |