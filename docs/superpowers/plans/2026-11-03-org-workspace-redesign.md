# Plan: 组织工作台改造（2026-11-03）

Spec: docs/superpowers/specs/2026-11-03-org-workspace-redesign.md
分支: test-ehr-sync-merge（工作目录 /Users/jsy/Public/self_code/llm-gateway-test）

## 已由 Lead 完成（共享层，勿改）
- `web/src/types/org.ts`：RoleKey/RoleGrantItem/AccountRow/AccountListResult/AccountBrief/OrgLeaderBrief/TeamCard/StudioOverview/MemberConfigSummary
- `web/src/stores/org.ts`：`fetchAccounts`、`studioOverview`/`fetchStudioOverview`
- `tests/contract/contracts.json`：org_workspace 契约块

## T1 后端（owner: be-org-api）
1. `internal/orgservice/repository/account_repo.go`：`ListAccounts(ctx, filter)`→ 一人一行，
   roles 聚合 + 主角色优先级 + role_counts；团队/工作室名 join；keyword 打 name/employee_id/job_title/email。
2. `internal/orgservice/repository/org_overview_repo.go`：`StudioOverview(ctx, studioID)`→ 工作室人数/管理员/负责人 + teams[]
   （每个团队人数/管理员/负责人）；`pending_dept_count`。
3. `internal/orgservice/repository/user_org_repo.go`：`ListMembersByDeptID` 增 `is_dept_admin` 与三块 config 摘要
   （quota 读 quota 覆盖表、budget 读 budget_allocations 的 member/dept_daily、entitlement 读 group_entitlements 的 studio+dept）。
4. `internal/orgservice/handler/account_handler.go` + `org_overview_handler.go`；router 挂
   `GET /admin/org/accounts`、`GET /admin/org/studios/:id/overview`（adminGroup）。
5. 测试：`tests/orgservice/repository/*`、`tests/controlplane/handler/*` 覆盖主角色优先级、筛选、负责人多人、摘要字段。

## T2 账号/角色页（owner: fe-accounts）
- 改造 `web/src/views/admin/org/RolePage.vue`：一人一行表格（姓名+徽标/工号/岗位/所属团队/邮箱/创建时间）、
  搜索 + 角色下拉 + 团队下拉 + 时间区间、行内【设置角色】复用 grant/revoke、分页、role_counts 统计卡；
  徽标命名：平台管理员/工作室管理员/团队管理员/员工；多角色折叠显示。
- 组件放 `web/src/components/org/account/`（新文件），不动 OrgTreePage.vue。

## T3 团队工作台（owner: fe-team）
- 改造 `web/src/views/admin/org/OrgTreePage.vue` 为工作室工作台：头部（人数/管理员/【设置工作室管理员】/负责人）、
  左侧团队卡（人数/EHR 负责人"等N人负责"/【设置团队管理员】）、右侧成员表（徽标/岗位/配额/预算规则/模型授权）、
  行展开 = 成员配置三卡（配额可改，预算/模型授权显示继承并给跳转）、tabs（团队/成员、Service Key、模型授权、团队用量、团队预算、申请审批，先挂现有页面/组件）。
- 组件放 `web/src/components/org/team/`（新文件）。

## T4 集成验收（owner: lead）
- go build/test、vite build、vitest；契约核对；提交推送 test；部署后实测账号页/工作台/成员面板；ledger 记录。
