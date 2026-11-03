# 组织工作台改造（原型驱动，2026-11-03）

## 一句话方向
把"组织"从**同步维护的对象**升级为**权限 + 成本 + 授权的统一工作台**：工作室 → 团队 → 成员三层，
每层显示负责人（EHR，只读、多值、只增不删）+ 管理员（RBAC，可授可撤）+ 规模；
成员行显示配额/预算/模型授权摘要，展开即配置；角色以 `员工/团队管理员/工作室管理员/平台管理员` 四种徽标呈现。

## 原型（三张图）确认的设计
1. 图1 团队管理：左树 工作室→团队；右详情 = 团队人数 + 团队管理员(多人) +【设置管理员】；成员卡带徽标。
2. 图2 账号/角色：**一人一行**（不是授权记录一行）；列 = 姓名+角色徽标/工号/岗位/所属团队/邮箱/创建时间；
   筛选 = 搜索 + 角色 + 团队 + 时间；行内【设置角色】。
3. 图3 团队成员细节：工作室头部（人数/管理员/设置）；左侧团队卡显示 EHR 负责人（`傅彭彭等3人负责`）+ 人数；
   右侧成员表 姓名+徽标/岗位/配额/预算规则/模型授权；行展开 = 成员配置三卡（配额/预算/模型授权），
   文案"来自团队默认配额，调整后单独生效"；工作室级导航 tabs（团队/成员、Service Key、模型授权、团队用量、团队预算、申请审批）。

## 用户决策（2026-11-03）
- **A 方案**：账号页一人一行，主徽标取最高权限（admin > studio_admin > dept_admin > user=员工），其余角色折叠显示；不改授权校验逻辑。
- P0 + P1 + P2 一起做；UI 按原型改造。

## 关键语义（护栏）
- **负责人 ≠ 管理员**：负责人来自 `org_node_leaders`（EHR 同步，只增不删，无编辑入口）；管理员来自 `user_roles`（可授可撤）。
  UI 文案必须分开：`负责人（EHR 同步）` / `管理员`。
- 命名统一：`dept_admin → 团队管理员`、`studio_admin → 工作室管理员`；内部 role 值不变。
- 继承语义：配额 `inherit|override`（已有）；预算 `dept_daily`(团队池) ↔ `member`(成员单独)（已有）；
  模型授权只有 studio/dept 两级（无成员级），成员行显示继承后的可用模型组数，展开面板的"管理授权"落到团队。
- 团队默认配额的"团队级配置"当前不存在（只有全局默认 + 成员覆盖）→ 不新增三级继承，显示继承自全局默认。

## API（Lead 已固化为契约，见 tests/contract/contracts.json 的 org_workspace）
- `GET /api/v1/admin/org/accounts`：keyword/role/studio_id/dept_id/page/page_size → 一人一行 + roles[] + role_counts。
- `GET /api/v1/admin/org/studios/:id/overview`：studio 的 member_count/admins/leaders + teams[]（每个团队 count/admins/leaders）。
- `GET /api/v1/admin/org/departments/:dept_id/members`：在既有字段上补 `is_dept_admin` 与
  `config.quota{rpm,max_concurrency,source}` / `config.budget{monthly,daily,weekly,source}` / `config.entitlement{group_count,source}`。

## 复用（不新建）
配额弹窗 `api/quota.ts` + OrgTreePage 现有 `openUserQuotaDialog`；角色授权 `/org/roles/grant|revoke`；
预算 `/api/v1/budgets/context` 与 BudgetManagePage 组件；模型授权 `/admin/groups/:id/entitlements`；
用量 `/api/v1/usage/dashboard/user-detail`。

## 不做
一人一角色强约束、团队管理员审批流、角色变更历史、头像上传、团队级默认配额三级继承。

## 验收
- 账号页：一人一行、四个徽标正确、角色/团队筛选生效、设置角色后行内即时更新。
- 工作台：左侧团队卡显示 EHR 负责人（多人时"等N人负责"）；头部显示人数与管理员并可设置。
- 成员表：徽标 + 配额/预算/模型授权三列摘要；展开面板可改配额并显示"继承/已单独设置"。
- 全部 go test / vitest 绿（除 2 个预算页历史失败），并部署测试环境实测。
