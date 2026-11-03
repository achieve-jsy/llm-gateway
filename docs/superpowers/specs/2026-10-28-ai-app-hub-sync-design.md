# AI 应用聚合页 · 同步子系统设计

- 状态：已确认（2026-10-28）
- 上游设计：`docs/ai-app-hub-design.md`（数据模型与整体架构）
- 本文档只覆盖**同步**，不涉及 H5、管理页 UI、应用 CRUD

## 1. 目标

把 EHR 组织数据同步拆成三种可独立触发的操作，并让"同步逻辑"与"触发方式"互不依赖：

| 操作 | 粒度 | 典型场景 |
|---|---|---|
| 全量同步 | 整个组织树（99 条） | 每日定时、首次初始化 |
| 单工作室同步 | 一个事业部 + 它的子树 | 工作室调整了负责人 / 新建了部门 |
| 单部门同步 | 一个节点 | 某个部门改名、换负责人 |

三种操作复用同一个服务入口，HTTP 接口和定时任务都只是触发器。

## 2. 非目标

- 不做员工/人员同步（`employees` 路由当前 403，未授权）
- 不做异步任务队列、进度查询、历史运行记录
- 不支持多副本并发同步（见 §9 风险）

## 3. 约束

1. **EHR 没有单节点接口**：`departments/{id}` 等路由全部 403，唯一可用的是 `departments?orgId=`（一次返回全部 99 条）。因此三种范围都只发**一次**请求，差别只在写库范围。
2. **不改 schema**：复用已提交的 4 张表；同步状态写进已有的 `system_configs`。
3. **单副本部署**：并发控制用进程内锁。
4. 同步失败不得清空本地数据。

## 4. 组件与边界

```
internal/aiapphub/ehrclient/         数据源。只做 HTTP + JSON 解析，不碰数据库
        │ FetchDepartments(ctx) []Department
        ▼
internal/controlplane/service/ai_app_hub_sync.go
        编排：拉一次 → 按 Scope 过滤 → 写部门 → 写工作室 → 回填 studio_id → 写状态
        ▲
internal/controlplane/handler/ai_app_hub_handler.go    HTTP 触发（解析 scope、鉴权）
internal/controlplane/service/ai_app_hub_cron.go       定时触发（每 6h，scope=all）
```

依赖方向单向：`handler → service → ehrclient`，`cron → service`。`ehrclient` 不依赖 GORM，`service` 不依赖 gin。

## 5. 范围语义

```go
type ScopeKind string // "all" | "department" | "studio"

type SyncScope struct {
    Kind ScopeKind
    ID   string // department 是 department_id；studio 是 ai_app_studios.id；all 为空
}
```

| Kind | ID | 写入范围 |
|---|---|---|
| `all` | 空 | 全部部门节点；全部 `departmentType=2` 的工作室行 |
| `department` | `department_id` | 仅该节点一行；该节点若是 `departmentType=2`，同时更新对应工作室行 |
| `studio` | `ai_app_studios.id` | 该工作室行 + 其子树内全部节点（用于回填 `studio_id`） |

`department` 范围**不写子节点**：下属分组要等全量或工作室同步才会更新。这是刻意的——单部门同步的语义是"修这一条"。

范围校验：`department` 的 ID 必须出现在本次拉取结果里，`studio` 的 ID 必须能在本地工作室表中找到且带 `department_id`，否则返回 `ErrScopeNotFound`（HTTP 404）。

## 6. 覆盖规则（字段级）

| 字段 | 新建时 | 已存在（`source='ehr'`） | 已存在（`source='manual'`） |
|---|---|---|---|
| `name` | 规则覆盖优先，否则 EHR 名称 | 覆盖 | 整行跳过 |
| `leader_name / leader_passport / leader_employee_id` | 写入 | 覆盖 | 整行跳过 |
| `visible` | `!HiddenDepartmentIDs[id]` | **不覆盖** | 跳过 |
| `sort_order` | 0 | **不覆盖** | 跳过 |

规则表写死在 `DefaultSyncRules()`：

- 隐藏：`0B3CF753-9D6D-46E8-89C2-8E752D2A460F`（公共关系事业部）、`08C6856B-D0D5-4FC4-830E-7D13441EA89F`（元气工作室）
- 改名：`53E83585-6CE3-48EF-8255-520BBAFCB156` → **技术研究工作室**

含义：同步只负责"EHR 里有的东西"，可见性和排序是运营决策，一旦设定就不会被同步改回去。

仓储因此需要两个方法（替代原计划的 `UpsertStudioByDepartment`）：

- `InsertStudioIfAbsent(ctx, *model.AIAppStudio) (inserted bool, err error)` —— `ON CONFLICT DO NOTHING`
- `UpdateStudioEHRFields(ctx, id uuid.UUID, name, leaderName, leaderPassport, leaderEmployeeID string) error`

## 7. 并发与失败

- 进程内 `sync.Mutex` + `TryLock`；抢不到 → `ErrSyncInProgress`（HTTP 409）
- 每次执行 `context.WithTimeout(30s)`
- `FetchDepartments` 失败 → 直接返回，**不写任何库**；本地保持上次成功状态
- 落库逐条 upsert，不开大事务；中途失败保留已写部分，下次同步幂等补齐
- 上溯找工作室祖先时最多 10 层，超出记 `slog.Warn` 并置 `studio_id = NULL`（防脏数据成环）


## 8. 接口契约

单一入口，用 `scope` 区分：

```http
POST /api/v1/admin/ai-app-hub/sync
Authorization: Bearer <JWT>
Content-Type: application/json

{"scope":"all"}
{"scope":"studio","id":"3f2b...uuid"}
{"scope":"department","id":"53E83585-6CE3-48EF-8255-520BBAFCB156"}
```

成功（200）：

```json
{"scope":"studio","departments":11,"studios":1,"hidden":0,"duration_ms":842}
```

| 状态码 | 场景 | 响应体 |
|---|---|---|
| 400 | scope 非法或 id 缺失 | `{"error":"invalid scope"}` |
| 403 | 无权限 | `{"error":"forbidden"}` |
| 404 | `ErrScopeNotFound` | `{"error":"scope not found"}` |
| 409 | `ErrSyncInProgress` | `{"error":"sync in progress"}` |
| 502 | EHR 拉取失败 | `{"error":"ehr upstream","detail":"..."}`（detail 便于前端提示"接口中心未授权"） |
| 200 | 成功 | 统计体，见上 |

## 9. 权限

| scope | 允许 |
|---|---|
| `all` | `user_roles.role IN ('admin','ai_app_admin')` |
| `studio` | 管理员，或该工作室的 `leader_passport` = 当前 passport |
| `department` | 管理员，或该部门上溯到的工作室负责人 |

判定复用 `AIAppHubService.ResolveViewer` 的结果（`IsAdmin` / `IsLeader` / `StudioIDs`），不新增权限表。

## 10. 状态留痕

复用 `SettingsRepo`（`internal/controlplane/repository/settings_repo.go`），写以下键（成功 3 个，失败再加 1 个）：

| key | 示例 |
|---|---|
| `ai_app_hub.last_sync_at` | `2026-10-28T15:20:11+08:00` |
| `ai_app_hub.last_sync_status` | `success` / `failed` |
| `ai_app_hub.last_sync_scope` | `all` / `studio:<uuid>` / `department:<id>` |

失败时 status=failed，并额外写 `ai_app_hub.last_sync_error`（截断到 500 字）。成功/失败都写，管理页永远能显示"最近一次同步"。


## 11. 定时任务

`StartAIAppHubSyncCron(ctx, svc, interval)`：默认 6 小时，启动后先跑一次，仿 `internal/controlplane/keyactivity/cron.go`（`time.After` 循环 + `ctx.Done()` + 单次 recover）。

环境变量 `EHR_BASE_URL` / `EHR_API_TOKEN` / `EHR_ORG_ID` 任一为空 → 不启动 cron，`slog.Warn` 一行，不影响服务启动。

## 12. 测试策略

| 层 | 方法 | 关键断言 |
|---|---|---|
| ehrclient | httptest | 路径/query/Authorization 头正确；403→ErrUnauthorized；非法 JSON→ErrUpstream |
| Sync | fake fetcher + sqlite | all 写 99 条；department 只写 1 条；studio 写子树；已有行 visible 不变；manual 行不被触碰 |
| 并发 | 两个 goroutine | 一个成功、一个 ErrSyncInProgress |
| 失败 | fetcher 返回 error | 库内数据与调用前一致 |
| handler | gin + httptest | 403/404/409/502 四种错误映射正确 |

## 13. 与既有实施计划的关系

上一版计划（docs/superpowers/plans/2026-10-28-ai-app-hub-backend.md）的 Task 4（同步服务）和 Task 10（同步触发）由本文档替代；其余任务（仓储、解析、权限、聚合、H5 接口、管理 CRUD）保持不变。计划的修订在下一步 writing-plans 完成。

## 14. 风险与后续

1. **多副本**：进程内锁在多副本下失效，可能出现两个副本同时写同一批行。当前单副本部署可接受；扩容时换 Redis 锁（仓库已有 Redis）。
2. **规则只在首现生效**：EHR 新增一个应排除的工作室，visible=false 只在它第一次入库时生效，之后需要运营手工关闭。
3. **单部门同步不覆盖子节点**：下属分组可能短暂滞后，属预期行为。
4. **EHR 权限缺口**：employees 未授权，人员维度无法同步；开通后可作为新的 Scope 扩展，接口签名不变。

