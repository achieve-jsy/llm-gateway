# EHR 组织数据单一来源改造（去掉手工维护）

- 日期：2026-11-03
- 状态：设计已确认，待实施
- 前置设计：`2026-11-02-ehr-org-tree-daily-sync-design.md`（每日全量同步，已上线 test）

## 1. 背景

EHR 每日 03:00 全量同步已经跑通：100 个部门、15 个事业部、547 名员工、离职 127 人软删除。
但它写的是**新表** `ai_app_studios / ai_app_departments / ehr_employees`，而后台页面（组织树、工作室、成员）、RBAC、Key 组织快照读的是**老表** `studios / departments / dept_studio_map`。

结果是两套数据并存，老表靠 7 个手工入口维护，长期必然打架。

### 1.1 目标

1. 组织/人员数据**只有 EHR 一个来源**，删掉全部手工写入口
2. 老表成为 EHR 的镜像，页面 / RBAC / Key 快照零改动继续工作
3. 同步改为**拉全量、写差量**；改名只改属性，ID 终身稳定
4. 删除的部门/工作室**软删除**，历史用量记录仍能关联
5. 管理员可在页面**手动触发**一次同步，同步中不可重复触发

### 1.2 非目标

- 不动角色授权（`POST /org/roles/grant|revoke` 保留，权限不是组织数据）
- 不做 EHR 数据的人工修正界面（EHR 错了就回 EHR 改）
- 不做同步进度条 / 任务队列 / 同步历史列表（有审计日志）
- 不迁 `parent_id` / `departmentPath`：组织页仍按"工作室 → 部门"两层展示

## 2. 现状与硬约束

### 2.1 老表被谁依赖

| 依赖方 | 引用的 ID |
|---|---|
| `users.dept_id`（uuid） | `departments.id` |
| `user_roles.scope_id`（8 条：dept 5 / studio 2 / company 1） | `studios.id` / `departments.dept_id` |
| `api_keys.dept_id/studio_id`（17 条有 studio） | `studios.id` 文本快照 |
| `call_logs.dept_id/studio_id/studio_name` | 同上，逐行快照 |
| `usage_daily`（**PK 含 dept_id/studio_id**） | 同上 |
| `model_daily_stats`（**PK 含 studio_id**） | 同上 |

**硬约束**：`studios.id` 一旦生成终身不变；同步永不 delete + insert。
否则 `usage_daily` / `model_daily_stats` 的主键快照会分叉成两组，历史账单按工作室断成两截。

### 2.2 实测存量（test 环境，2026-11-03）

| 对象 | 数量 | 备注 |
|---|---|---|
| `studios` | 18 | EHR 事业部 15 个，含 2 个隐藏 |
| `departments` | 56 | 同步后应为 85（100 − 15 事业部） |
| `dept_studio_map` | 56 | 54 import + 2 manual |
| `users.dept_id` 非空 | 599 | |
| `user_roles` | 8 | |
| `api_keys` 有 studio 快照 | 17 | |

工作室名字对账：

- **精确同名 11 个**
- **老表独有 7 个**：`XLab`（疑似 ↔ EHR `AI-Lab` 改名）、`test`、`互娱事业部`、`创新事业部`、`战略发展中心`、`数字内容创新工作室`、`测试工作室AI`
- **EHR 独有 4 个**：`AI-Lab`、`广州公共美术中心`、`杭州集星社`、`棋牌技术中心`

## 3. 数据模型

### 3.1 迁移 1（阶段 A 之前）

```sql
ALTER TABLE studios
  ADD COLUMN ehr_department_id   varchar(64),
  ADD COLUMN visible             boolean NOT NULL DEFAULT true,
  ADD COLUMN source              varchar(16) NOT NULL DEFAULT 'manual',
  ADD COLUMN leader_name         varchar(128) NOT NULL DEFAULT '',
  ADD COLUMN leader_passport     varchar(64)  NOT NULL DEFAULT '',
  ADD COLUMN leader_employee_id  varchar(64)  NOT NULL DEFAULT '';

CREATE UNIQUE INDEX uniq_studios_ehr_dept ON studios (ehr_department_id)
  WHERE ehr_department_id IS NOT NULL;
```

- `ehr_department_id` = EHR 事业部部门 ID，**唯一匹配键**（不是 name）
- `visible`：EHR 里消失时置 false，行保留
- `source`：`ehr` / `manual`
- `leader_*`：从 `ai_app_studios` 保留下来，页面暂不展示（避免丢数据）

### 3.2 零 DDL 的表

- `departments`：`dept_id` 就是 EHR 部门 ID，天然业务键；`status` 是 varchar，新增 `'deleted'` 值不需要迁移
- `dept_studio_map`：PK 是 `dept_id`；`source` 新增 `'ehr'` 值

### 3.3 状态语义

| 表 | 字段 | 值 | 含义 |
|---|---|---|---|
| `departments` | `status` | `active` | 有工作室归属 |
| | | `pending_studio` | 推不出归属（孤儿部门），页面可视化 |
| | | `deleted` | EHR 里没了（软删） |
| `studios` | `visible` | `true` / `false` | false = 隐藏或已删除（软删） |

**关键**：两张表都**不用** `gorm.DeletedAt`。用普通列 + 组织接口显式过滤，否则用量 join 会被全局软删过滤掉。

**`visible` 是单向开关**：同步和对账只能把它置 `false`（EHR 消失），**永远不自动置回 `true`**。人工隐藏的工作室（元气工作室、公共关系事业部）因此不会在某次同步后被"复活"。恢复只能人工 UPDATE。

### 3.4 两套视图

| 视图 | 过滤 | 效果 |
|---|---|---|
| 组织树 / 工作室列表 / 成员列表 | `status <> 'deleted'`、`visible = true` | 已删数据不可见 |
| 用量报表 / 账单 / 调用日志 | **不过滤**，join 照常 | 历史数据仍显示名称，标注「（已删除）」 |

## 4. 同步流程（拉全量、写差量）

### 4.1 写入映射

| EHR | 老表 | 匹配键 | 写入字段 |
|---|---|---|---|
| `type=2` 事业部（15） | `studios` | `ehr_department_id` | name / visible / leader_* / source='ehr' |
| `type≠2` 部门（85） | `departments` | `dept_id` | dept_name / studio_id / status |
| 部门→事业部 | `dept_studio_map` | `dept_id` | studio_id / source='ehr' / updated_by='ehr-sync' |
| 人员 | `ehr_employees` | （不变） | |

事业部**不写进 `departments`**，避免页面出现"工作室"和"同名部门"两行。

### 4.2 差量算法

拉取始终全量（EHR 部门接口无 delta 参数、无修改时间字段，且只有全量能发现删除）。

```
for each EHR 事业部:
    按 ehr_department_id 查老表
    不存在        → INSERT（新增计数）
    存在且字段变化 → UPDATE 只变动的列（更新计数）
    存在且无变化   → 零写入
EHR 里没有的 source='ehr' 工作室 → visible=false（删除计数）

部门、映射同理，按 dept_id。
```

参与 diff 的列：部门 `dept_name / studio_id / status`；工作室 `name / visible / leader_*`。其余字段不参与。

### 4.3 同步结果

`HubSyncResult` 增加差量统计：`StudiosInserted/Updated/Deleted`、`DepartmentsInserted/Updated/Deleted`、`MappingsUpdated`。
写入 `system_configs` 供状态接口读取。**日常应为个位数**，突然涨到几十就是上游异常信号。

### 4.4 状态计算

- 部门能沿 `parentId` 链走到某个 `type=2` 节点 → `studio_id` 有值，`status='active'`
- 走不到（`parentId` 为空的 7 个孤儿）→ `studio_id=NULL`，`status='pending_studio'`

### 4.5 保留的自动写入点

SSO 登录时 `upsertUser` 遇到部门行缺失仍会兜底 INSERT（只补 `dept_name`），否则 EHR 未同步到时人登不进来。该兜底**不碰** `studio_id / status / source`。

## 5. 一次性迁移（上线前人工执行，不进 atlas）

### 5.1 dry-run 报告

输出四分类（同名 / 改名 / 老表独有 / EHR 独有），交付给人确认。

### 5.2 回填

```sql
-- 改名认领（示例：XLab ↔ AI-Lab）
UPDATE studios SET ehr_department_id = '<EHR事业部ID>' WHERE id = '<老工作室uuid>';

-- EHR 里存在但人工隐藏：仍是 source='ehr'，只是 visible=false（同步照常更新它的 name）
UPDATE studios SET visible = false WHERE name IN ('元气工作室','公共关系事业部');

-- 老表独有的手工残留：不再属于任何同步来源
UPDATE studios SET visible = false, source = 'manual'
 WHERE name IN ('test','测试工作室AI','互娱事业部','创新事业部','战略发展中心','数字内容创新工作室');
-- 注意：XLab 若确认为 AI-Lab 的旧名，走上面的"改名认领"，不要放这里
```

- `source='ehr'` 的行：同步负责更新 name / leader_*，但不改 `visible`
- `source='manual'` 的行：同步完全忽略（不更新、不参与对账）
- **老表独有且未认领的手工工作室一律 `visible=false`**，否则页面会永久悬挂一批没有人的空工作室；确需保留的由人工单独指定

### 5.3 引用检查

回填前必须核对：`user_roles` 的 2 条 studio scope、`api_keys` 的 17 条 studio 快照，是否落在将被 `visible=false` 或改名的行上。命中的逐条列出，由人决定（权限仍生效但页面不可见）。

## 6. 手动触发同步

### 6.1 接口

- 写：`POST /api/v1/admin/ai-app-hub/sync` —— **已存在，不改**（`scope=all` 仅 admin；`department/studio` 额外允许该工作室负责人）
- 读（新增）：`GET /api/v1/admin/ai-app-hub/sync-status` —— admin

```json
{
  "last_sync_at": "2026-11-03T13:50:16+08:00",
  "status": "success",
  "scope": "all",
  "error": "",
  "departments": 100,
  "employees": 547,
  "unmapped_departments": 7,
  "resigned_accounts_disabled": 127,
  "diff": { "studios": {"inserted":0,"updated":1,"deleted":0},
            "departments": {"inserted":0,"updated":3,"deleted":0} },
  "running": false
}
```

`running` 来自进程内锁状态，形状参照 `GET /api/v1/model-catalog/sync-status`。

### 6.2 页面

挂在 `OrgTreePage` 顶部，不新建页面：

```
┌─ 数据同步 ─────────────────────────────────────────┐
│ 上次同步：2026-11-03 13:50  ✅ 成功                 │
│ 部门 100 · 人员 547 · 未归属 7 · 离职关停 127       │
│ 本次变更：工作室 +0 ~1 · 部门 +0 ~3                 │
│                              [ 立即同步 ]（仅 admin）│
└────────────────────────────────────────────────────┘
```

### 6.3 交互：三层防重复

| 层 | 机制 | 覆盖场景 |
|---|---|---|
| 前端锁 | 点击即 `syncing=true` + 按钮 disabled + spinner「同步中…」，响应后复位 | 用户连点 |
| 服务端锁 | `sync.mu.TryLock()` → 409 `sync in progress` | 多标签页 / 多机器 |
| 状态兜底 | 进页面先读 `sync-status.running`，true 则按钮直接置「同步中」 | 刷新页面 / 切页面回来 |

- 409 **不弹红色报错**，切回「同步中」并提示「同步进行中（可能凌晨定时任务正在跑）」
- 前端 POST 超时 60s（后端 `syncTimeout=30s`）；超时 → 按钮复位 + 提示查看状态卡
- 不做轮询：同步是阻塞式 HTTP

### 6.4 审计

每次手动触发写一条 `audit_log`：`action=ai_app_hub.manual_sync`、actor、scope、差量统计、耗时。

## 7. 拆除清单

### 7.1 后端（7 个写入口）

| 路由 | 一并删除 |
|---|---|
| `POST /org/studios`、`DELETE /org/studios/:id` | `org_handler.CreateStudio/DeleteStudio` |
| `PUT /org/departments/:id/studio` | `mapping_handler.SetDeptStudio`、`mapping_service.SetDeptStudio` |
| `POST /org/dept-studio-map/import` | `mapping_handler.ImportMappings` |
| `POST /org/employees/import` | `employee_import_handler.Import` |
| `PUT /org/users/:id/department`、`.../follow-sso` | `dept_assign_handler.ChangeDepartment/FollowSSO` |

RBAC 权限位（`PermStudioCreate/Delete`、`PermMappingEdit`、`PermEmployeeImport`、`PermDeptChange`、`PermSSORevert`）**保留定义**，只删路由上的 `guard`——这些 perm 可能已写入角色数据，删定义需要额外迁移且无收益。

### 7.2 前端

- `OrgTreePage`：导入员工按钮、新建/删除工作室、编辑映射
- `stores/org.ts`：对应 5 个方法

### 7.3 表与代码（阶段 B）

- `DROP TABLE ai_app_departments; DROP TABLE ai_app_studios;`
- 删除其 model / repository / 同步代码中的相关分支
- 保留 `ehr_employees`

## 8. 发布顺序与回滚

```
① 迁移 1（只加列）
② 人工回填（dry-run → 确认改名 → 回填 ehr_department_id → 隐藏名单）
③ 部署阶段 A：同步写老表 + 双写 ai_app_*（回滚保险）+ 砍 7 入口 + 状态接口/按钮
④ 验证两天（组织树/成员/用量/差量统计）
⑤ 迁移 2 + 删同步里 ai_app_* 相关代码（拆 ai_app_studios/ai_app_departments）
```

- ② 必须先于 ③：否则第一天同步会把 15 个事业部建成新行
- 阶段 A 回滚：回退镜像，老表已被覆盖的数据不回滚（本就是事实数据）
- 阶段 B 回滚：重建两张 `ai_app_*` 表，数据可从老表重新投影

## 9. 测试策略

| 类型 | 断言 |
|---|---|
| diff 表驱动单测 | 新增→INSERT / 改名→只 UPDATE name / 换归属→UPDATE studio_id / 消失→软删 / 无变化→零写入 |
| ID 稳定性 | 改名后 `studios.id` 不变；`api_keys.studio_id` 仍能 join |
| 软删不影响用量 | 部门 deleted 后，`call_logs`/`usage_daily` 历史 join 仍返回名称 |
| 增量幂等 | 连跑两次，第二次差量全 0 |
| 并发 | 第二次 POST 返回 409；`sync-status.running=true` |
| 回归 | 现有 `tests/controlplane/service/ai_app_hub_*` 断言从新表改到老表 |
| 人工验收 | test 环境：组织树 100 部门 / 15 工作室（2 隐藏）/ 孤儿 7；用量页历史工作室名照常 |

## 10. 风险与开放问题

| 风险 | 缓解 |
|---|---|
| 改名认领错（XLab↔AI-Lab 误判） | dry-run 报告人工确认；错认只是显示名错，ID 不会串 |
| 隐藏的工作室被引用了权限/Key | 回填前的引用检查逐条列出 |
| 差量算法有 bug 导致漏更新 | 提供 `?full=1` 强制全量覆盖开关（运维手段），并在状态接口暴露差量数字 |
| 孤儿部门永远 pending | 页面常驻提示，属 EHR 数据问题；需要时在 EHR 补父节点 |
| 同步时长（`syncTimeout=30s` 同时覆盖部门与人员两阶段） | 人员 61 次 batch 请求实测约 2.8s；生产如翻倍需拆超时 |
