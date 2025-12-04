# control-plane 需求变更 — CHG-20260609-001

> 基线：xspec/modules/control-plane/spec.md (v1.0)

## 变更摘要

在控制面接入组织归集钩子和角色派发路由，将 org-service 的组织管理能力通过控制面统一对外暴露；同时扩展用量查询 API，支持按 dept/studio/company 三个维度聚合。

---

## 新增需求

### N1. SSO 归集钩子（P0）

- 在 OAuth 回调处理完成后，调用 org-service 归集接口（内部 HTTP），将员工挂载到对应部门
- 归集调用异步/同步均可，但需确保登录流程不因 org-service 异常而中断（降级：归集失败记录日志，不阻塞登录）
- 归集结果写入 JWT claims：`dept_id`、`studio_id`（映射成功时）、`role`、`scope_id`

### N2. 组织管理 API 路由（P0）

- 在控制面路由层挂载 `/api/v1/org/*` 路径，代理到 org-service（或直接集成为同进程包）
- 路由覆盖：工作室管理、部门查询、映射维护（导入/手工）、角色派发/卸任、未映射部门清单
- 鉴权：复用现有 `JWTAuth` 中间件，按 org-service 定义的角色权限限制访问

### N3. 分层用量 Dashboard API（P1）

- 新增接口：`GET /api/v1/usage/dashboard?scope=dept|studio|company&id=...`
- 按当前用户角色注入过滤条件：`dept_admin` 锁 `dept_id`；`studio_admin` 锁 `studio_id`（含下属部门）；`admin` 无过滤
- 复用 observability-hub 的 `usage_daily`，通过 `dept_studio_map` 关联聚合 studio 维度数据
- 鉴权拦截：普通员工（`user`）请求此接口直接返回 403

---

## 修改需求

### M1. Key 验证响应扩展（P0）

- 原：`/internal/v1/auth/verify-key` 返回 `{user_id, allowed_groups}`
- 改：新增返回 `dept_id`、`studio_id`（组织信息）；gateway-core 拿到后注入请求上下文用于 observability 埋点

### M2. JWT claims 扩展（P0）

- 原：JWT payload 含 `user_id`、`email`、`roles`
- 改：新增 `dept_id`、`studio_id`（org-service 归集后写入）；`studio_id` 为空表示部门尚未映射（pending_studio）

---

## 新增验收标准

| 编号 | Given | When | Then | 优先级 |
| :--- | :--- | :--- | :--- | :--- |
| AC-CP-N1 | 员工完成 SSO OAuth 回调 | 登录流程结束 | org-service 归集钩子被调用；JWT claims 含 dept_id | P0 |
| AC-CP-N2 | org-service 归集服务暂时不可用 | 员工 SSO 登录 | 登录仍成功（降级），日志记录归集失败，JWT 中 dept_id 为空 | P0 |
| AC-CP-N3 | dept_admin 调用 /api/v1/usage/dashboard | 请求管理看板数据 | 返回值仅包含本部门数据，不含其他部门 | P1 |
| AC-CP-N4 | user 角色调用 /api/v1/usage/dashboard | 请求管理看板数据 | 返回 403 | P1 |
| AC-CP-M1 | gateway-core 调用 verify-key | Key 验证成功 | 响应中包含 dept_id 和 studio_id 字段 | P0 |

---

## 修改验收标准

- AC-08（原：SSO 回调）→ 增加后置断言：「JWT 中包含 dept_id 字段，已归集员工 dept_id 非空」
