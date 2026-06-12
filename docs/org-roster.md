# 用户→岗位画像 HTTP 取数方案（t5）

> 问题：同岗分位需要 `users.job_family/job_category/job_title`，但这些字段**不出现在** `/api/v1/usage/*` 返回中。
> 已确认 `GET /api/v1/org/departments/:dept_id/members`（`OrgHandler.GetDeptMembers`，`PermMemberView`，
> `internal/orgservice/repository/user_org_repo.go:141-175` 的 `ListMembersByDeptID`）能返回岗位字段。
> 本方案核对端点返回结构与权限，并给出「全部部门→成员→(user_id, dept_id, studio_id, job_category)」的 HTTP 拼装路径、
> 去重/覆盖不全回退、以及 phase一可用 / phase二下沉 control-plane 每日 job 的建议。

**红线**：不直连 DB、只走 HTTP、需要持有 `PermMemberView` 的 **admin（IsGlobal）JWT**（普通角色拉不全）。

---

## 0. 结论（TL;DR）

1. **可行**：`/org/departments/:dept_id/members` 返回 `DeptMemberInfo`，JSON 含 `id`(user_id)、`name`、`email`、
   `job_title`、`job_family`、`job_category`、`dept_source`、`login_status`（`user_org_repo.go:127-139`）。
   **`studio_id` 不在成员返回里**，需从部门或 `dept_studio_map` 侧补（见 §1.2/§1.4）。
2. **必须用 admin JWT**：路由 guard 是 `PermMemberView`（`ScopeManage`，任意管理角色），但 handler 内部再校验 `CanViewDeptMembers`：
   admin 恒通过；`studio_admin` 只可见自己工作室下的部门；`dept_admin` 只可见自己部门；普通用户 403。
   因此**非 admin 拉不全**——要用一个 `IsGlobal` 的 admin token 遍历全部部门。
3. **主键用 user_id 锚定**：roster 以 `users.id::text`（= `call_logs.user_id`）为键，`dept_id/studio_id/job_category` 是其属性。
   同岗分位按 `(studio_id, job_category)` 分组即可，无需按 dept 层 join（避免 dept_id 双格式坑）。
4. **phase一 用 N 次 per-dept HTTP fan-out + 本地缓存 roster**（写盘 `roster.json`，TTL≈24h）。公司几百个部门、每部门 1~2 页，收敛。
5. **phase二 推荐下沉 control-plane**：每日 job 物化 `user_org_daily` 快照（user_id/dept_id/dept_name/studio_id/job_*），
   经 `InternalAuth` 内部端点一次性吐出，或落 Redis/DB 供工具单次读取 —— 消除 N 次 fan-out 与对长期 admin JWT 的依赖。

---

## 1. 端点核对（返回 JSON 字段 + 权限）

> 所有 orgGroup 端点挂在 `adminGroup.Group("/org")` 之下（`router.go:953`），即 `JWT + /api/v1/org`。
> 无 `guard()` 的端点 = 仅需 JWT；带 `guard()` 的 = 需对应权限且范围被 Actor 收窄。

### 1.1 `GET /api/v1/org/departments/:dept_id/members`（核心）

- **handler**：`OrgHandler.GetDeptMembers`（`org_handler.go:137`）。
- **guard**：`PermMemberView`（`router.go:969`）。handler 内再校验 `orgPolicy.CanViewDeptMembers`。
- **path 参数**：`:dept_id` 接受 **SSO dept_id**（`departments.dept_id` varchar）**或 UUID**（`departments.id`），先按 SSO 查、再按 UUID 查（`org_handler.go:142-145`）。
- **query 参数**：`page`（≥1，默认 1）、`page_size`（1..100，默认 20，`parsePagination`）。
- **返回**：`{"members": [...], "total": N, "page": P, "page_size": PS}`。
- **DeptMemberInfo JSON**（`user_org_repo.go:127-139`）：

| 字段 | 含义 | 备注 |
|---|---|---|
| `id` | 用户 ID（`users.id::text`） | **= `call_logs.user_id`，roster 主键** |
| `name` | 姓名 | |
| `email` | 邮箱 | |
| `joined_at` | `users.created_at`（UTC→YYYY-MM-DD） | |
| `employee_id` | 工号 | omitempty |
| `job_title` | 岗位 | `COALESCE(u.job_title,'')` |
| `job_family` | 岗位族群 | `COALESCE(u.job_family,'')` |
| `job_category` | 岗位类别 | `COALESCE(u.job_category,'')` |
| `dept_source` | `sso`/`manual` | `COALESCE` |
| `login_status` | `active`/`pending` | `COALESCE` |
| `has_conflict` | 是否有 dept 冲突 | `EXISTS(user_dept_conflicts)` |

- **取数 SQL**（`ListMembersByDeptID`，`user_org_repo.go:142-175`）：`FROM users u JOIN departments d ON d.id = u.dept_id WHERE d.dept_id = ? ORDER BY u.name LIMIT ? OFFSET ?`。
  → **一个用户只属于一个部门**（`users.dept_id` 单值 UUID），因此**不会跨部门重复**；但也意味着**不在任何部门下的用户不会出现**（见 §4 覆盖回退）。

### 1.2 `GET /api/v1/org/dept-studio-map`

- **handler**：`MappingHandler.ListMappings`（`mapping_handler.go:22`），**无 guard**（仅 JWT）。
- **query**：`page`、`page_size`。**返回**：`{"mappings": [...], "total": N, "page": P, "page_size": PS}`。
- **DeptStudioMap**：`dept_id`（SSO）、`studio_id`（uuid）、`source`、`updated_by`、`updated_at`（`model/models.go:51-59`）。
- **用途**：把 `dept_id` → `studio_id` 解析出来，作为 `departments.studio_id` 缺失时的兜底（见 §3-C）。

### 1.3 `GET /api/v1/org/studios`

- **handler**：`OrgHandler.ListStudios`（`org_handler.go:41`），**无 guard**（仅 JWT）。
- **返回**：`{"studios": [...], "total": N, "page": P, "page_size": PS}`；`Studio`：`id`、`name`、`owner_id`、`created_at`。
- **用途**：校名 / 统计用；roster 本身不需要（studio_id 走 dept/map），但若要「工作室名」标签可取。

### 1.4 `GET /api/v1/org/departments`

- **handler**：`MappingHandler.ListDepartments`（`mapping_handler.go:33`），**无 guard**（仅 JWT）。
- **query**：`status`（`active`/`pending_studio`）、`page`、`page_size`。
- **返回**：`{"departments": [OrgDepartment], "total": N}`（**注意未回显 page/page_size**，遍历时需自行按 `total` 推进）。
- **OrgDepartment JSON**（`model/models.go:30-38`）：`id`(UUID)、`dept_id`(SSO)、`dept_name`、`studio_id`(uuid|null)、`status`、`owner_id`、`created_at`。
- **用途**：枚举全部部门（拿到 SSO `dept_id` 作为 §1.1 的入参）＋ 取 `studio_id`。

### 1.5 `GET /api/v1/org/users`（**不适合**做 roster 源）

- **handler**：`OrgHandler.SearchUsers`，`guard` `PermOrgUserSearch`（`router.go:970`）。
- 返回 `UserSearchResult`：`id`、`name`、`email`、`dept_name`、`dept_id`——并**不包含** `job_*` 字段，且是**关键词搜索、限 20~50 条**（`user_org_repo.go:240-273`）。
- **结论**：不能用于「全量用户+岗位」名单；仅作兜底/补查个别用户 id。

---

## 2. 权限要点（crucial）

- guard `PermMemberView = "org:member:view"`，`ScopeManage`（任意管理角色，`registry.go`）。
- handler 二次校验 `OrgPolicy.CanViewDeptMembers(actor, dept.DeptID, deptStudioID)`（`org_policy.go:45-53`）：

| actor | 可见部门 |
|---|---|
| admin（IsGlobal） | **全部** |
| studio_admin | 仅自己 `StudioIDs` 内工作室的部门 |
| dept_admin | 仅自己 `DeptIDs` 内部门 |
| 普通 user | 无管理角色 → **403** |

→ **拼全量 roster 必须用 admin（IsGlobal）JWT**，且该账号已具备 `PermMemberView`。否则：非目标部门会 403 / 429；普通 user 全 403。
→ `org/departments`、`org/dept-studio-map`、`org/studios` 只需 JWT（无 guard），任何登录用户可列，但**成员**（§1.1）受限。

---

## 3. 拼装方案（phase一 · 离线工具）

> 目标产物：`roster = [ {user_id, name, email, dept_id(SSO), dept_name, studio_id, job_family, job_category, job_title, login_status, has_conflict, dept_source} ]`
> 以 `user_id` 为唯一键。

### 步骤 A —— 枚举全部部门

```
GET /api/v1/org/departments?page_size=100&page=1..ceil(total/100)
→ 累积 departments：{dept_id(SSO), dept_name, studio_id, status}
```
- 响应含 `total`，按 `total` 推进分页。
- 建议只保留 `status=active` 的部门进入成员抓取（`pending_studio` 的部门无工作室归属、且通常无成员；若要全量可一并抓，但 studio_id 为 null）。

### 步骤 B —— 逐部门拉成员

```
for each dept in departments:
    GET /api/v1/org/departments/{dept.dept_id}/members?page_size=100&page=1..ceil(dept.total/100)
    → 累积 DeptMemberInfo
```
- **注意路径参数还是 SSO dept_id**（也可传 UUID，两种都兼容）。
- **失败处理**：403（越权，见 §2）/ 404 / 429（限流）→ 记录该部门 skipped，继续；不要把整次运行当作失败。
- **请求量约 = 部门数（约每部门 1 页）**；`page_size=100`。

### 步骤 C —— 归一 / 去重 / studio 解析

- **studio_id 解析（优先级）**：① `departments.studio_id`（非空）→ ② `dept_studio_map.studio_id`（按 dept_id 查，`/org/dept-studio-map` 一次拉全 → map）→ ③ 空（该部门无工作室归属，落入同岗池 fallback 的第 2/4 级）。
- **job_category 取值链**（对齐 schema §5.2）：`job_category` 非空用之；否则 `job_family`；再否则 `job_title`；三者皆空 → `job_category=""`（不入任何同岗池，仅兜底/不产出）。
- **去重**：以 `user_id` 为键 `Map[user_id]roster`。理论上每用户只属一个部门（§1.1 SQL 保证），但仍做去重；若同一 user_id 出现在多个 dept（理论不会），保留 `dept_source='manual'` 或有 `has_conflict` 的那次，并标记 `conflict=true`。
- **透传**：`login_status`、`has_conflict` 原样保留；`pending`（花名册导入未登录）用户通常无用量 → 在分位池中因 `total_tokens=0`（或无调用）被自然剔除（§4），不污染池。

### 输出 schema（roster.json，缓存用）

```jsonc
{
  "snapshot_at": "2026-07-27T09:00:10+08:00",
  "generated_via": ["/api/v1/org/departments", "/api/v1/org/departments/:dept_id/members", "/api/v1/org/dept-studio-map"],
  "users": [
    {
      "user_id": "…", "name": "…", "email": "…",
      "dept_id": "SSO-dept-id", "dept_name": "…",
      "studio_id": "…" | null,
      "job_family": "…", "job_category": "…", "job_title": "…",
      "login_status": "active" | "pending", "dept_source": "sso"|"manual", "has_conflict": false
    }
  ],
  "coverage": { "dept_count": N, "member_count": M, "skipped_depts": ["dept_id", ...], "unassigned_hint": "users without dept not captured" }
}
```
- 缓存到本地盘（如 `~/.usage-report/roster.json` 或工具工作目录），**TTL≈24h**；仅在 report 需要且缓存过期时重拉。
- 报告期 = 本周，roster 只需「报告时点」的岗位归属即可（岗位/部门变更低频），不必逐日。

### 请求量 / 耗时

- 公司若 ~200 部门：`1(部门列表) + 1(映射) + ~200(成员) ` ≈ 202 次请求；每次 page_size=100 支持大部门。
- 属可接受（离线周报），但需带限流退避（429/5xx 退避重试 + 记录 skipped 部门）。

---

## 4. 覆盖不全的回退策略

1. **无部门（`users.dept_id IS NULL` 或 dept 未被识别）**：该类用户**不会**出现在任何 `members` 里 → 无 `job_category`。回退：job_category=""，schema §5.2 规定「不入任何岗位池」，仅当兜底池/不产出 `peer.supported=false`。**无全量「用户+岗位」端点**（`/org/users` 无岗位字段且限量）→ 这是**已知覆盖缺口**，须在报告 `meta.qa` 标注。
2. **同岗池样本不足（`pool_n < 5`）**：逐级回退（schema §5.2）→ 同工作室+同 job_category → 全公司同 job_category → 同工作室 → 全公司（兜底仍 <5 则 `supported=false`，不 rank）。
3. **studio_id 缺失**：该部门成员不以「工作室+岗位」入池，回退到「全公司同 job_category」（pool 优先级 2）或「全公司」（优先级 4）。
4. **权限不足（非 admin）**：只能拉到部分部门 → 标记 `coverage.skipped_depts`，报告 `peer.supported=false` 或明确「仅本 department 范围内」。
5. **限流/网络失败**：记录 skipped 部门，重试 3 次（指数退避），仍失败则 `coverage` 注明不完整。

---

## 5. Phase二（推荐）：下沉到 control-plane 每日 job

**为什么**：phase一 的 N 次 per-dept fan-out + 长期 admin JWT 是权宜。最佳形态是把「用户→岗位」快照做成**服务端每日材料**：

- **新增每日 job**（可复用 `rollupCron`/`budgetSnapshotPublisher` 的启动模型，或新增 `orgCron`）：扫描 `users + departments(+dept_studio_map)`，
  物化快照表 `user_org_daily`（或 `user_org_roster`）：`user_id, dept_id, dept_name, studio_id, job_family, job_category, job_title, login_status, snapshot_date`。
  - 与 `rollup_service.go` 每日整日重算同范式；零增量成本，低频率。
- **暴露一次性读取**（二选一）：
  - **内部端点** `GET /internal/v1/org/roster?as_of=YYYY-MM-DD`（`InternalAuth` 校验 `X-Internal-Token`，与 `/internal/v1/*` 同范式）一次性返回全量，供离线工具/其他内部消费。
  - 或 Snapshot 写 Redis key（`org:roster:{date}`），工具单 get。
- **收益**：① 单请求全量（消除 N 次 fan-out）；② 离线工具无需长期 admin JWT（改用内部 token 或只读快照）；③ 快照时点一致，避免 scatter-gather 不一致。

> phase一 可先用 §3 的 HTTP fan-out 验证全链路 + 产出 roster.json；phase二 再切到服务端快照，接口契约（roster 字段）不变，工具侧只换取数方式。

---

## 6. 安全与红线

- **不直连 DB**：全程 `/api/v1/org/*` HTTP。
- **admin JWT 只在内存/env**（`--token`/`USAGE_REPORT_TOKEN`），**绝不写入** roster.json / report JSON / MD/HTML / 日志 / stdout。请求头 `Authorization: Bearer`。
- 只取**元数据/组织归属**（user_id、岗位、部门、工作室），**不取** 请求正文/响应内容/PII（姓名/邮箱是组织管理必须，属最小必要；报告个人级只显示「我/你」）。
- 失败/限流时**不重放敏感原样**；退避重试，`skipped` 显式记录。
- 红线对齐：`docs/guardrails.md` A3（价格单位/口径统一）；设计铁律「不跨岗位裸成本排名，只同岗匿名分位」（schema §5.2）。

---

## 7. 待确认 / 风险

- 🔴 **admin token 可用性**：离线工具需一个 `IsGlobal` 且具 `PermMemberView` 的账号（admin 角色）。若无 admin，只能出「局部范围」报告，`peer.supported=false`。
- 🔴 **无部门用户覆盖缺口**：无「全量用户+岗位」端点，`users.dept_id IS NULL` 用户无法入 roster → 报告须标注该缺口（不影响有部门成员的岗位分位）。
- 🔴 **范围权限的 actor**：`guard(PermMemberView)` 会注入 Actor，但 `CanViewDeptMembers` 仍会把结果收窄到 operator 范围；**即使同账号，也必须是 admin 才能看全**。
- 🔴 **`/org/departments` 响应未回显 page/page_size**（`mapping_handler.go:41` 只返回 departments+total）：遍历页数时以 `total` 为准推进，别依赖回显字段。
- 🔴 **phase二 job 排期**：需 captain 确认是否本次纳入（影响任务划分）；phase一 已可用 HTTP fan-out 满足 28 天基线/同岗分位对岗位字段的需求。
