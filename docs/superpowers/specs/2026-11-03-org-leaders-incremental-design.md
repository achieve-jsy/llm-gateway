# 组织负责人：增量覆盖与保留（2026-11-03）

## 背景与规则（来自产品文档，用户 2026-11-03 确认）
- 自动同步每日 02:00、手动触发、组织树以 EHR 全量对齐 —— 均已上线，本文不改。
- **负责人采用增量覆盖与保留**：节点负责人是**多值列表**；同步只"追加/更新属性"，**任何来源的旧负责人都不删**。
  例：EHR 负责人 甲 → 管理员手动加 乙 → EHR 改为 丙 ⇒ 最终列表 = 甲、乙、丙。
- 负责人信息覆盖**工作室与部门**两级。
- 手动新增是阶段 A 拆除手动维护后**唯一重开的手动入口**，仅限"加负责人"，不做删除/编辑（文档未要求，YAGNI）。

## 决策
1. **新表 `org_node_leaders`**（不往 studios/departments 塞 jsonb：要唯一约束防重、要按节点批量读）：
   `id uuid pk`、`node_type varchar(8)`('studio'|'dept')、`node_id uuid`（studios.id / departments.id，都是稳定主键）、
   `name/passport/employee_id`、`source varchar(8)`('ehr'|'manual')、`created_at/updated_at`。
   唯一索引按身份表达式：`employee_id` 非空用它，否则 `passport`，否则 `name` —— 同一人手动加过、EHR 又带来时只留一行。
2. **upsert 语义**：行存在 → 只更新 name/passport/employee_id 属性，**source 保持原值**（manual 不被 ehr 洗掉）；不存在 → 插入。
   永不 delete。旧 EHR 负责人（甲）因此天然保留。
3. **同步写路径**：`projectOrg` 工作室路在 UpsertStudio 后、部门路在 UpsertDepartment 后，若 EHR 节点带负责人则
   `UpsertLeader(source='ehr')`。部门路需要行 id → `UpsertDepartment` 签名改为返回 `(outcome, id, err)`。
   `studios.leader_*` 三列保留继续写（= EHR 当前负责人缓存），列表展示以 leaders 表为准。
4. **手动入口**：`POST /api/v1/admin/org/leaders` {node_type, node_id, keyword}；keyword 按
   `ehr_employees` 花名册解析（employee_id / group_e_number 精确，person_name 唯一匹配），查不到 404 ——
   负责人必须是真实存在的人，杜绝自由文本脏数据。命中后以花名册的 name/passport/employee_id 写 source='manual'。
   路由挂在既有 `adminGroup`（平台管理员）下，不新增 RBAC perm（避免动角色种子）。
5. **读**：`GET /api/v1/admin/org/leaders` 一次返回全表（量级 ~百行），前端按 node 分组；不改既有 list 响应形状。
6. **前端**：组织树节点行内显示负责人名单 + 每节点一个"+"按钮（ElMessageBox.prompt 输工号/姓名），不新建组件。

## 不做（明确砍掉）
- 负责人删除/编辑 UI 与 API；负责人变更历史；按负责人反查节点；candidates 搜索下拉（prompt 输入 + 404 提示够用）。

## 验收
- 二轮同步 + EHR 负责人改名/换人后：manual 行与旧 ehr 行都在，新 ehr 负责人追加进来（单测断言）。
- 手动加花名册外的人 → 404；重复加同一人 → 幂等不报错。
- 组织树节点可见负责人名单；"+" 可加人且刷新后可见。
