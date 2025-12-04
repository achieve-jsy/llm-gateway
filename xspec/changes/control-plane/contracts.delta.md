# control-plane 接口契约变更 — CHG-20260609-001

> 基线：xspec/modules/control-plane/spec.md (v1.0)（无 contracts.md 基线）

## 变更摘要

新增 `/api/v1/org/*` 组织管理接口、`/api/v1/usage/dashboard` 分层看板接口；扩展 `/internal/v1/auth/verify-key` 响应字段。

---

## 新增接口

### 组织管理 API（/api/v1/org/*）

完整接口定义见 `xspec/changes/org-service/contracts.md` §2–§5。

control-plane router 将这些接口**代理到同进程的 orgservice handler**，对外 URL 前缀为 `/api/v1/org/`。

### GET /api/v1/usage/dashboard — 分层用量看板

**鉴权**：JWTAuth；user 角色直接 403

**Query 参数**：

| 参数 | 类型 | 必填 | 说明 |
| :--- | :--- | :--- | :--- |
| scope | string | 否 | `company`\|`studio`\|`dept`；角色为 dept_admin/studio_admin 时强制覆盖 |
| id | string | 否 | scope 对应的组织节点 ID；admin 时必填（company 除外） |
| start | string | 否 | 起始日期 YYYY-MM-DD，默认 30 天前 |
| end | string | 否 | 结束日期 YYYY-MM-DD，默认今天 |
| page | int | 否 | 默认 1 |
| page_size | int | 否 | 默认 20，最大 100 |

**响应 200**：
```json
{
  "scope": "studio",
  "id": "S001",
  "scope_name": "研发工作室",
  "period": {"start": "2026-05-01", "end": "2026-06-09"},
  "summary": {
    "total_tokens": 1500000,
    "cost_estimate": 12.50,
    "user_count": 25,
    "call_count": 3200
  },
  "breakdown": [
    {
      "id": "D001",
      "name": "前端部",
      "total_tokens": 800000,
      "cost_estimate": 6.50,
      "user_count": 12,
      "rank": 1
    }
  ],
  "total": 3,
  "page": 1,
  "page_size": 20
}
```

**错误**：
- `403` — user 角色访问
- `422` — scope 与角色不匹配（如 dept_admin 请求 scope=studio）

---

## 修改接口

### POST /internal/v1/auth/verify-key — 响应扩展

**变更前**：
```json
{"user_id": "u123", "allowed_groups": ["gpt-4-level"]}
```

**变更后**（向后兼容，新增字段）：
```json
{
  "user_id": "u123",
  "allowed_groups": ["gpt-4-level"],
  "dept_id": "D001",
  "studio_id": "S001"
}
```

- `dept_id`：员工所属部门 ID，未归集时为空字符串 `""`
- `studio_id`：员工所属工作室 ID，pending_studio 时为空字符串 `""`
- 已有消费方（gateway-core）只读取已知字段，新字段不影响现有逻辑

---

## 废弃接口

不适用
