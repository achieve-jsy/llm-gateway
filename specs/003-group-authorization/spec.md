# 需求规格说明书 · 模型组授权与开放 · 003

> 状态：需求草稿 · 待评审
> 需求方：平台运营 / 安全
> 优先级：P1（直接影响模型访问控制与成本管控）
> 关联：org RBAC（已上线）、Key 管理（现有）

---

## 1. 一句话描述

为模型组引入「公开 / 受控」开放模式，去掉原 Key↔组手动绑定的粗放授权，建立**平台→工作室→部门**三级授权链，使模型组的可见与可用范围可按组织结构精细管控。

---

## 2. 背景与问题

### 现状

当前模型组的访问控制依赖两个机制：

1. **Key 级 `access_all_groups` 万能开关**：为 `true` 时该 Key 可访问所有已启用的模型组。
2. **`key_group_bindings` 手动绑定**：为 `false` 时只能访问绑定表中配置的模型组。

```
现有授权判定（group_dispatcher.go）：
┌─────────────────────────────────────────────┐
│ AccessAllGroups == true?  → 直接放行        │
│ AccessAllGroups == false? → 查 key_group_   │
│                              bindings        │
│                              是否包含目标组   │
└─────────────────────────────────────────────┘
```

### 痛点

1. **无组织维度管控**：即使 org RBAC 已有四角色（user / dept_admin / studio_admin / admin），但模型组授权不与组织架构挂钩，无法按工作室/部门粒度控制高端模型的访问。
2. **万能开关过于粗放**：`access_all_groups=true` 一开就是全部组，无法区分公开模型与需管控的高端模型。
3. **手动绑定管理成本高**：每个 Key 需逐一绑定模型组，模型组新增/人员变动时需批量调整。
4. **缺乏分级开放能力**：无法表达"DeepSeek 全员可用，Claude Opus 仅授权部门可用"。

### 相关现有结构

| 实体 | 表 | 说明 |
|------|------|------|
| `Group` | `model_groups` | 模型组，当前无开放模式字段 |
| `ApiKey` | `api_keys` | 含 `access_all_groups`、`dept_id`、`studio_id` |
| `KeyGroupBinding` | `key_group_bindings` | Key↔组手动绑定 |
| `Studio` | `studios` | 工作室 |
| `OrgDepartment` | `departments` | 部门（含 `dept_id`、`studio_id`） |
| `DeptStudioMap` | `dept_studio_map` | 部门↔工作室映射 |
| `UserRole` | `user_roles` | RBAC 角色（`admin/studio_admin/dept_admin/user`） |
| `UserIdentity`（网关） | 内存结构 | 含 `DepartmentID`、`StudioID`、`AccessAllGroups` |
| `VerifyKeyResponse` | DTO | verify-key 返回 Key 信息 + User 信息 |

---

## 3. 目标与成功标准

### 主要目标

| # | 目标 | 衡量方式 |
|---|------|----------|
| G1 | 公开模型组全员自动可用，无需配置 | 新建 Key 即可访问所有 public 组 |
| G2 | 受控模型组按组织架构精细授权 | 只有已开通部门的 Key 可调用 |
| G3 | 授权变更即时生效 | 撤销后下次调用立即 403 |
| G4 | 兼容存量 Key 行为 | 上线不中断现有流量 |
| G5 | 授权操作有组织层级约束 | 工作室负责人只能在被下放的范围内开通 |

### 成功标准

- SC-1：受控组未开通部门的调用 100% 被拒（403 group_not_authorized）
- SC-2：公开组任何 Key 均可调用，不受部门/工作室限制
- SC-3：撤销工作室授权后，下属部门开通**即时失效**（不等 TTL）
- SC-4：上线后存量 Key 行为不变（存量组默认 public）

---

## 4. 用户与场景

### 目标用户

| 用户 | 主要操作 | 关注点 |
|------|----------|--------|
| 平台管理员 | 设组开放模式、授权工作室 | 全局管控、成本控制 |
| 工作室负责人 | 在授权范围内下放给部门 | 部门使用范围 |
| 部门负责人 | 在下放范围内开通本部门 | 本部门可用模型 |
| 普通员工 / Service Key | 调用模型组 | 我能用哪些模型 |

### 典型场景

1. **全量公开**：平台管理员将 DeepSeek、Qwen 等平价模型组设为 `public`，全员自动可见可用。
2. **精细管控**：Claude Opus 设为 `controlled`，仅授权给 AI 研发工作室；工作室负责人为旗下研发部门逐一开通。
3. **撤销授权**：管理员撤销某工作室的 Claude Opus 授权 → 该工作室下所有部门即时不可用。
4. **新人入职**：新员工 Key 的 `access_all_groups=true`（改义后）→ 自动获得 public 组 + 本部门已开通的 controlled 组，无需手动绑定。
5. **调用鉴权**：用户调用 `claude-opus` → 网关检查该组是否在 usable_groups（public ∪ 本部门已开通 controlled）中 → 在则放行，不在则 403。

---

## 5. 业务流程

### 授权流程

```
平台管理员 设组开放模式 → 受控组授权工作室范围
                ↓
工作室负责人 在授权内下放给部门
                ↓
部门负责人 在下放内开通本部门受控组
                ↓
公开组 平台自动全推 ──→ 员工可用 = 所有公开组 ∪ 本部门已开通受控组
```

### 调用鉴权流程

```
用户/Service Key 调用模型组
         ↓
  该组在可用集合内？
    ├── 是 → 放行，进入调度
    └── 否 → 拒绝 403 group_not_authorized
```

### 级联回收

```
平台撤销工作室授权（studio 行 disabled）
         ↓
判定时联级：dept 行可用前提 = 其 studio 行 enabled
         ↓
该工作室下所有部门即时不可用
（无需逐行删 dept 行，查询联级判定即可；可选异步清理）
```

---

## 6. 开放模式定义

| 开放模式 | 值 | 可见/可用规则 | 示例 |
|---------|------|-------------|------|
| **公开** | `public` | 全公司默认可见可用，无需授权配置 | DeepSeek、Qwen 平价模型 |
| **受控** | `controlled` | 默认不可见，须经「下放 + 开通」两步后对应部门才可用 | Claude Opus、GPT-4 |

---

## 7. User Scenarios & Testing *(mandatory)*

### User Story 1 - 公开组全员可用 (Priority: P1)

作为员工，我希望公开模型组无需任何配置即可使用，使平价模型的使用零门槛。

**Acceptance Scenarios**:

1. **Given** 模型组 `deepseek-chat` 的 `open_mode=public`，**When** 任意 Key 调用 `/v1/chat/completions` 指定该组，**Then** 正常放行进入调度。
2. **Given** 模型组 `deepseek-chat` 的 `open_mode=public`，**When** 用户请求 `/v1/models`，**Then** 该组出现在可见列表中。
3. **Given** Key 的 `access_all_groups=false` 且无手动绑定，**When** 调用 public 组，**Then** 仍然放行（public 组不受 Key 绑定限制）。

---

### User Story 2 - 受控组部门级授权 (Priority: P1)

作为平台管理员，我希望能将高端模型组设为受控，仅对已授权的部门开放。

**Acceptance Scenarios**:

1. **Given** `claude-opus` 为 `controlled`，部门 A 已开通、部门 B 未开通，**When** 部门 A 员工调用 `claude-opus`，**Then** 放行。**When** 部门 B 员工调用，**Then** 403 `group_not_authorized`。
2. **Given** `claude-opus` 为 `controlled` 且未授权给任何工作室，**When** 任何员工请求 `/v1/models`，**Then** 该组不出现在列表中。
3. **Given** 部门 A 已开通 `claude-opus`，**When** 部门 A 员工请求 `/v1/models`，**Then** `claude-opus` 出现在列表中。

---

### User Story 3 - 工作室级授权与下放 (Priority: P1)

作为平台管理员，我希望先将受控组授权给工作室，再由工作室负责人下放给具体部门，实现逐级管控。

**Acceptance Scenarios**:

1. **Given** 平台管理员将 `claude-opus` 授权给工作室 S1（创建 `scope_type=studio` 行），**When** S1 工作室负责人为部门 D1 开通（创建 `scope_type=dept` 行），**Then** D1 部门员工可见可用 `claude-opus`。
2. **Given** `claude-opus` 仅授权给工作室 S1，**When** 工作室 S2 负责人尝试为其部门开通，**Then** 操作被拒绝（无 studio 授权）。
3. **Given** 工作室 S1 下有部门 D1（已开通）和 D2（未开通），**When** D2 员工调用 `claude-opus`，**Then** 403。

---

### User Story 4 - 级联撤销 (Priority: P1)

作为平台管理员，我希望撤销工作室授权后其下属部门的开通资格即时失效。

**Acceptance Scenarios**:

1. **Given** 工作室 S1 已被授权 `claude-opus`，S1 下部门 D1 已开通，**When** 管理员撤销 S1 的 `claude-opus` 授权（studio 行 disabled），**Then** D1 员工下次调用 `claude-opus` 返回 403。
2. **Given** 撤销 S1 授权后，**When** D1 员工请求 `/v1/models`，**Then** `claude-opus` 不再出现。
3. **Given** 撤销后管理员重新授权 S1，**When** D1 的 dept 行仍 enabled，**Then** D1 员工重新可用。

---

### User Story 5 - access_all_groups 语义修正 (Priority: P1)

作为系统，`access_all_groups=true` 表示该 Key 可用「用户授权范围内的所有组」而不是全局所有组。

**Acceptance Scenarios**:

1. **Given** Key 的 `access_all_groups=true`，Key 属于部门 D1，`claude-opus` 为 controlled 且 D1 已开通，**When** 调用 `claude-opus`，**Then** 放行。
2. **Given** Key 的 `access_all_groups=true`，Key 属于部门 D1，`claude-opus` 为 controlled 但 D1 未开通，**When** 调用 `claude-opus`，**Then** 403。
3. **Given** Key 的 `access_all_groups=true`，**When** 调用 public 组，**Then** 始终放行。
4. **Given** Key 的 `access_all_groups=false`，Key 属于 D1 已开通 `claude-opus`，但 Key 未手动绑定该组，**When** 调用 `claude-opus`，**Then** 403（需要手动绑定子集内才行）。

---

### User Story 6 - 可见列表改造 (Priority: P2)

作为员工，`/v1/models` 返回的列表应反映我实际可用的模型组（public + 本部门已开通 controlled）。

**Acceptance Scenarios**:

1. **Given** 3 个 public 组 + 2 个 controlled 组（其中 1 个已对本部门开通），**When** 请求 `/v1/models`，**Then** 返回 4 个组。
2. **Given** `access_all_groups=true`，**When** 请求 `/v1/models`，**Then** 返回 public ∪ 本部门已开通 controlled 的全集（不再返回全局所有组）。

---

### User Story 7 - embeddings 补鉴权 (Priority: P2)

作为系统，embeddings 接口也需受模型组授权约束。

**Acceptance Scenarios**:

1. **Given** `claude-opus` 为 controlled 且部门未开通，**When** 调用 `/v1/embeddings` 指定该组，**Then** 403。

---

### Edge Cases

- **存量迁移**：上线时所有组默认 `open_mode=public`，行为不变；管理员按需逐个改为 controlled。
- **dept_id 为空的 Key**：旧 Key 可能无 dept_id，此时只能用 public 组（controlled 需要 dept 维度判定）。
- **Key 的 dept_id 与 studio_id 关系**：通过 `dept_studio_map` 解析 Key 所属 studio，用于 studio 级联判定。
- **passthrough 路由**：`/v1/p/*` 直通不走模型组调度，不受本机制影响（默认仅白名单/管理员可用）。
- **模型组被禁用（enabled=false）**：无论授权状态如何，禁用组不可用。

---

## 8. Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 模型组 MUST 支持 `open_mode` 字段（`public` / `controlled`），默认 `public`。
- **FR-002**: 公开组（public）MUST 对全公司所有 Key 自动可见可用，无需授权。
- **FR-003**: 受控组（controlled）MUST 默认不可见不可用，仅通过三级授权链开通。
- **FR-004**: 授权链 MUST 为：平台管理员授权工作室 → 工作室负责人下放部门 → 部门开通。
- **FR-005**: 授权数据 MUST 通过统一的 `group_entitlements` 表存储，`scope_type` 区分层级。
- **FR-006**: 级联撤销 MUST 通过查询联级实现——dept 行可用的前提是其 studio 行 enabled。
- **FR-007**: `access_all_groups` 语义 MUST 修正为「可用 usable_groups 全集」（public ∪ 本部门已开通 controlled），不越过部门授权边界。
- **FR-008**: `access_all_groups=false` 时可用范围 MUST 为 `usable_groups ∩ key_group_bindings`。
- **FR-009**: 网关可见列表（`/v1/models`）MUST 返回 usable_groups 而非全局所有组。
- **FR-010**: 网关调用前置鉴权 MUST 检查目标组是否在 usable_groups 中。
- **FR-011**: embeddings 接口 MUST 同样受模型组授权约束。
- **FR-012**: 授权变更 MUST 触发 KeyCache 主动失效（即时生效，不等 TTL）。
- **FR-013**: 上线迁移 MUST 将存量组默认设为 `public`，保证零影响上线。
- **FR-014**: 授权操作 MUST 校验操作人的 RBAC 权限（平台管理员/工作室负责人/部门负责人）。

### Non-Functional Requirements

- **NFR-001**: usable_groups 计算延迟 < 50ms（控制面计算，下发快照）。
- **NFR-002**: 授权变更到网关生效延迟 < 5s（主动失效 + 重新拉取）。
- **NFR-003**: group_entitlements 表支持 10K+ 条记录，查询性能稳定。

---

## 9. Key Entities

### Group（模型组）- 变更

| 字段 | 类型 | 变更 | 说明 |
|------|------|------|------|
| `open_mode` | `varchar(16)` | **新增** | `public` / `controlled`，默认 `public` |

### GroupEntitlement（模型组授权）- 新建

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | `uuid` PK | 主键 |
| `group_id` | `uint` NOT NULL | 关联模型组 |
| `scope_type` | `varchar(16)` NOT NULL | `studio` / `dept` |
| `scope_id` | `varchar(64)` NOT NULL | studio UUID 或 SSO dept_id |
| `enabled` | `bool` DEFAULT true | 是否有效 |
| `granted_by` | `uuid` | 操作人 |
| `created_at` | `timestamptz` | 创建时间 |
| `updated_at` | `timestamptz` | 更新时间 |

**唯一约束**：`(group_id, scope_type, scope_id)`

### UserIdentity（网关）- 变更

| 字段 | 类型 | 变更 | 说明 |
|------|------|------|------|
| `UsableGroups` | `[]string` | **新增** | 可用模型组名集合（public ∪ 本部门已开通 controlled） |

### VerifyKeyResponse（DTO）- 变更

| 字段 | 类型 | 变更 | 说明 |
|------|------|------|------|
| `usable_groups` | `[]string` | **新增** | 控制面计算后下发到网关 |

---

## 10. 可用集合算法

```
UsableGroups(dept_id, studio_id) =
  AllPublicGroups
  ∪
  ControlledGroups WHERE:
    group_entitlements 有 dept 行 (group_id, scope_type=dept, scope_id=dept_id, enabled=true)
    AND
    group_entitlements 有 studio 行 (group_id, scope_type=studio, scope_id=studio_id, enabled=true)
```

**放行判定逻辑**：

```
// 授权天花板 = usable_groups（任何 Key 都不可越过）
if target_group NOT IN usable_groups:
    return 403 group_not_authorized

// access_all_groups 决定是否再收窄
if access_all_groups == true:
    pass  // 可用 usable_groups 全集
else:
    if target_group NOT IN key_group_bindings:
        return 403 group_not_authorized

// 放行，进入调度
```

---

## 11. 接口设计

### 11.1 设置模型组开放模式

```
PUT /api/v1/groups/:id/open-mode
```

**权限**：平台管理员（`role=admin`）

**Request Body**:
```json
{ "open_mode": "controlled" }
```

**Response**: 200 OK

---

### 11.2 授权工作室（平台管理员）

```
POST /api/v1/groups/:id/entitlements
```

**权限**：平台管理员

**Request Body**:
```json
{ "scope_type": "studio", "scope_id": "<studio_uuid>" }
```

**Response**: 201 Created

---

### 11.3 下放/开通部门

```
POST /api/v1/groups/:id/entitlements
```

**权限**：工作室负责人（`role=studio_admin, scope_id=<studio>`）或部门负责人（`role=dept_admin, scope_id=<dept>`）

**Request Body**:
```json
{ "scope_type": "dept", "scope_id": "d_12345" }
```

**前置校验**：
- 该组必须为 `controlled`
- 该组必须已授权给操作人所属工作室（存在 studio 行 enabled）

**Response**: 201 Created

---

### 11.4 撤销授权

```
DELETE /api/v1/groups/:id/entitlements/:entitlement_id
```

**权限**：对应上级角色

**Response**: 204 No Content

**副作用**：触发 KeyCache 主动失效事件

---

### 11.5 查询组授权列表

```
GET /api/v1/groups/:id/entitlements?scope_type=studio|dept
```

**权限**：平台管理员 / 工作室负责人（仅看本工作室下）

**Response**:
```json
{
  "items": [
    { "id": "uuid", "scope_type": "studio", "scope_id": "s_uuid", "scope_name": "AI研发", "enabled": true, "granted_by": "admin_uuid", "created_at": "..." },
    { "id": "uuid", "scope_type": "dept", "scope_id": "d_123", "scope_name": "NLP组", "enabled": true, "granted_by": "studio_admin_uuid", "created_at": "..." }
  ]
}
```

---

### 11.6 可见列表（网关改造）

现有接口 `GET /v1/models` 行为变更：

- **之前**：`access_all_groups=true` → 返回所有 enabled 的组；`=false` → 返回 key_group_bindings 绑定的组
- **之后**：返回 `usable_groups`（public ∪ 本部门已开通 controlled）；`access_all_groups=false` 时进一步收窄为 `usable_groups ∩ key_group_bindings`

---

## 12. 改造点清单

| 类型 | 位置 | 改什么 |
|------|------|--------|
| 🔧 模型 | `internal/controlplane/model/group.go` + 迁移 | Group 加 `open_mode` 字段 |
| 🆕 模型/迁移 | `internal/controlplane/model/group_entitlement.go` + 迁移 | 新建 `group_entitlements` 表 |
| 🆕 CP 三件套 | `handler/entitlement_handler.go` | 授权 CRUD API |
| 🆕 CP 三件套 | `service/entitlement_service.go` | 授权逻辑 + RBAC 校验 + 可用集合计算 |
| 🆕 CP 三件套 | `repository/entitlement_repo.go` | 授权数据访问 |
| 🔧 verify-key | `service/key_service.go` VerifyKey | 计算 usable_groups 并下发 |
| 🔧 DTO | `internal/auth/dto/internal.go` | VerifyKeyResponse 增 `usable_groups` |
| 🔧 网关身份 | `internal/gateway/model/identity.go` | UserIdentity 增 `UsableGroups` |
| 🔧 网关 auth | `internal/gateway/infra/auth_client.go` | 解析 usable_groups 到 identity |
| 🔧 调度鉴权 | `internal/gateway/orchestrator/group_dispatcher.go` | 放行依据改为 usable_groups + binding 子集 |
| 🔧 可见列表 | `internal/gateway/handler/bound_models.go` | 返回 usable_groups 而非全量 |
| 🔧 model_info | `internal/gateway/handler/model_info.go` | 同步改造 |
| 🔧 embeddings | embeddings handler | 补组授权前置 |
| 🔧 缓存失效 | `internal/controlplane/event` + KeyCache | 授权变更触发主动失效 |
| 🔧 前端 | `web/src/views/admin/` | 组列表加 open_mode 标记、授权管理 UI |

---

## 13. 与现有机制的关系（A6 收口）

| 机制 | 处置 | 说明 |
|------|------|------|
| `access_all_groups` | **保留，语义修正** | 「可用 usable_groups 全集」而非「全局所有组」 |
| `key_group_bindings` | **保留，作为缩小子集** | `access_all_groups=false` 时，可用范围 = usable_groups ∩ 手动绑定 |
| 放行判定 | **天花板 = usable_groups** | 任何 Key 都不可超出，手动绑定只能在天花板内选子集 |

**迁移策略**：上线时所有存量组默认 `open_mode=public`（行为不变）；需管控的组由管理员逐个改为 `controlled` 并配授权，平滑切换。

---

## 14. 非功能需求

### 性能

- usable_groups 计算（verify-key 时）< 50ms
- KeyCache 失效到重建 < 5s
- group_entitlements 查询（含联级判定）< 20ms

### 兼容

- 与现有 `key_group_bindings` 并存
- 与 org RBAC 权限点对齐（需新增 permission 常量）
- 前端侧边栏新增入口，不影响现有页面

### 可观测

- 授权变更记录审计日志
- 网关 403 日志包含 `reason=group_not_authorized` 和 `dept_id`

---

## 15. 不做的事（明确排除）

- ❌ 不废弃 key_group_bindings（本期并存）
- ❌ 不引入用户级粒度授权（最小粒度是部门）
- ❌ 不做授权审批流（本期直接操作，无 workflow）
- ❌ 不改 passthrough 路由的鉴权逻辑
- ❌ 不做「模型组分组/tag」能力（不关联）
- ❌ 不做预算/quota 与授权的关联（另一条线）

---

## 16. 验收标准

| # | 场景 | 验收 |
|---|------|------|
| AC-1 | 公开组（public） | 任意部门员工/Service Key 直接可用，无需授权 |
| AC-2 | 受控组（controlled）未开通 | 可见列表不含、调用 403 group_not_authorized |
| AC-3 | 部门开通后 | 本部门可见可用，其他部门仍不可见 |
| AC-4 | 平台撤销 studio 授权 | 下属部门即时失效（实时联级判定） |
| AC-5 | embeddings 调用受控组 | 同样受鉴权约束 |
| AC-6 | `access_all_groups=true` Key | 只能用 usable_groups（不能越权访问未开通受控组） |
| AC-7 | `access_all_groups=false` Key 手动绑定受控组但部门未开通 | 403（天花板不可越过） |
| AC-8 | 存量迁移 | 所有组默认 public，上线零影响 |
| AC-9 | 授权变更后 | KeyCache 主动失效，下次调用即用最新授权 |
| AC-10 | 工作室负责人操作 | 只能为自己工作室下的部门开通已下放的组 |

---

## 附录

### 术语表

| 术语 | 含义 |
|------|------|
| 公开组（public） | `open_mode=public`，全员自动可用 |
| 受控组（controlled） | `open_mode=controlled`，需逐级授权 |
| usable_groups | 某 Key/部门实际可用的组集合 = public ∪ 本部门已开通 controlled |
| 下放 | 平台将受控组授权给工作室；或工作室将权限延伸到部门 |
| 开通 | 部门负责人为本部门启用某受控组 |
| 级联回收 | 撤销上级授权 → 下级即时失效 |
| 联级判定 | dept 行可用条件 = 该 dept 的 studio 行也 enabled |

### 相关资源

- Org RBAC 模型：`internal/orgservice/model/models.go`
- Key 模型：`internal/controlplane/model/key.go`
- 组模型：`internal/controlplane/model/group.go`
- 网关调度：`internal/gateway/orchestrator/group_dispatcher.go`
- verify-key DTO：`internal/auth/dto/internal.go`
- 可见列表：`internal/gateway/handler/bound_models.go`
