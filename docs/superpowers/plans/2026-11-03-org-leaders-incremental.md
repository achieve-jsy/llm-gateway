# Plan: 组织负责人增量覆盖与保留（2026-11-03）

Spec: docs/superpowers/specs/2026-11-03-org-leaders-incremental-design.md

## Global Constraints
- 只增不删：leaders 表任何 sync 路径不得 DELETE；source 一旦 manual 不被 ehr 覆盖。
- 迁移走 atlas（hash 必更）；测试库若 CI 迁移慢，按 runbook 在测试主机手动 apply（--baseline 20260609082623 幂等）。
- 前端不新建组件；契约 tests/contract/contracts.json 同步。

## Tasks
1. 迁移 `atlas-migrations/20261104100000_org_node_leaders.sql` + `atlas migrate hash`。
2. model：`OrgNodeLeader` + NodeType/Source 常量（orgservice/model）。
3. repo：`UpsertLeader`（select-by-identity → update attrs / insert）、`ListLeaders`；
   `UpsertDepartment` 改返回 `(outcome, id uuid.UUID, err)`，同步改调用方与既有测试。
4. sync：`projectOrg` 两路接 `UpsertLeader(source=ehr)`（负责人全空则跳过）。
5. handler+router+audit：`POST /api/v1/admin/org/leaders`（花名册解析：employee_id/group_e_number 精确、person_name 唯一；404/400）、
   `GET /api/v1/admin/org/leaders`；audit 记 org.leader_add。
6. 测试：repo union 语义；sync 二轮 manual 保留 + ehr 换人追加；handler 解析/404/幂等。
7. 前端：types + store(fetchLeaders/addLeader) + OrgTreePage 节点行内名单与"+"弹窗；contracts.json。
8. 全量 go build/test、vite build、vitest；提交推送 test；部署后手动触发同步验证 leaders 落表。

## Review Focus
- UpsertLeader 的身份表达式与唯一索引是否一致（ON 冲突路径 vs select 路径）。
- UpsertDepartment 签名变更的所有调用方。
- 手动入口是否真的只在 adminGroup 下。
