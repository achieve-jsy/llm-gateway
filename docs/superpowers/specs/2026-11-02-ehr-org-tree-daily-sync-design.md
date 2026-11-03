# EHR 组织树每日定时同步 · 设计

- 状态：待评审（2026-11-02）
- 分支：`feat/ai-app-hub`
- 上游：`docs/superpowers/specs/2026-10-28-ai-app-hub-sync-design.md`（同步子系统 v1）、`docs/ai-app-hub-design.md` §4.1
- 范围：只改「调度方式 + 删除对账 + 失败语义 + 可观测」，**不改表结构，不新增迁移**

## 1. 目标与验收

目标：每天 **03:00（Asia/Shanghai）** 对 EHR 整棵部门树自动跑一次全量同步；多实例只跑一个；失败一定能在管理页状态里看见；EHR 里撤销的工作室在本地不再展示。

验收（全部可测）：

1. 03:00 到点自动执行一次 `scope=all`，结束写 `ai_app_hub.last_sync_at/status`。
2. 蓝绿两套 control-plane 同时在线时，同一时刻只有一套执行（Redis leader 锁）。
3. 进程在 03:00 时不在线（重启/发布），起来后补跑一次；同一天不会重复跑。
4. EHR 拉取失败最多重试 3 次；全失败写 `status=failed` + 错误摘要，本地数据保持不变。
5. EHR 中消失的 `departmentType=2` 部门，对应 `ai_app_studios` 行 `visible=false`；`source='manual'` 行与 `ai_apps` 数据不受影响。
6. 手动触发的三种 scope 行为不变；非 `all` 不触发任何隐藏。

## 2. 非目标

- 不做员工/人员同步（EHR `employees` 路由 403）。
- 不新增运行历史表、不接告警通道（只在 `system_configs` 与日志留痕）。
- 不改 `ai_app_departments` 表结构（理由见 §6.2）。
- 不改 H5 / 管理端 UI。

## 3. 现状与问题（证据）

| # | 问题 | 位置 |
|---|---|---|
| 1 | 周期锚在进程启动时间，不是墙上时钟（`time.After(interval)`），永远对不上"每天 3 点"，每次重启/蓝绿切换都漂 | `internal/controlplane/service/ai_app_hub_cron.go` |
| 2 | 启动先跑一次，每次重启都白写 99 行 | 同上 |
| 3 | 只有进程内 `sync.Mutex`，蓝绿两实例会同时同步 | `ai_app_hub_sync.go`（`TryLock`） |
| 4 | 失败只 `slog.Error`，无重试、无补跑 | `ai_app_hub_cron.go` |
| 5 | 只 upsert 不删除：EHR 撤销的工作室永久留在列表里，与 `docs/ai-app-hub-design.md` §4.1「已撤销 → `visible=false`」不符 | `ai_app_hub_sync.go`（`syncStudios` 只写不隐藏） |
| 6 | 周期只读一次 env（`EHR_SYNC_INTERVAL`），且与"按时刻"语义混用 | `router.go`（`aiAppHubSyncInterval`） |

## 4. 方案总览

保持不变：`ehrclient.FetchDepartments` 一次拉全树、`AIAppHubSyncService.Sync` 按 scope 写库、HTTP 手动触发、`system_configs` 状态键。

本设计改四件事：

1. **触发**：`ai_app_hub_cron.go` 从间隔轮询改成「1 分钟 tick + cron 表达式 + 墙上时钟判定 + 跨重启补跑」。
2. **互斥**：tick 抢 Redis leader 锁，锁比 `context` 长命（TTL 10min），蓝绿互斥。
3. **对账**：`scope=all` 结束时按 EHR 全量 ID 集合隐藏已撤销的工作室（一条批量 UPDATE）。
4. **失败**：单次调度最多 3 次尝试（保护式重试），全失败视为当天失败，等下一次到点。

调度机制不引入新范式：照抄 `internal/controlplane/service/scheduled_report_service.go` 的 ticker + `robfig/cron` 解析 + `SetNX` 锁 + Lua 释锁写法（`github.com/robfig/cron/v3` 已在 `go.mod`）。

## 5. 调度设计

### 5.1 配置

| env | 默认 | 语义 |
|---|---|---|
| `AI_APP_HUB_SYNC_CRON` | `0 3 * * *` | 5 字段标准 cron；**显式设为空串 = 关闭调度**（运维开关） |
| `AI_APP_HUB_SYNC_TZ` | `Asia/Shanghai` | `time.LoadLocation`；失败回退 `time.Local`，再失败回退 UTC 并 `slog.Warn` |

删除 `EHR_SYNC_INTERVAL` 与 `aiAppHubSyncInterval()`：只留一个入口，避免"间隔"与"按时刻"两套语义并存。
启动门槛沿用现有 `aiAppHubEHRConfigured()`（`EHR_BASE_URL` / `EHR_API_TOKEN` / `EHR_ORG_ID` 三者齐全）。

### 5.2 到点判定（含跨重启补跑）

每 60s 一次 tick，抢到锁后：

1. 读 Redis `ai_app_hub:sync:last_success`（RFC3339，TTL 30 天）。
2. **没有记录**（首次部署 / 键过期）→ 立即执行一次，作为冷启动补齐。
3. **有记录** → `next = schedule.Next(lastSuccess.In(loc))`；`now.In(loc) >= next` 才执行。

这套判定天然满足"错过就补、跑过不重复"：

- 昨天 03:00 成功，今天 03:00 时进程不在线，10:20 起来 → `next` = 今天 03:00 ≤ now → 补跑一次。
- 今天 03:00 已成功 → `next` = 明天 03:00 → 不跑。
- 连续重启 → `last_success` 只有成功才更新，不会因为重启而重复跑。

### 5.3 互斥

- 锁 key `ai_app_hub:sync:leader`，value 为 `hostname:pid`，TTL 10 分钟（覆盖最坏重试窗口，见 §5.4）。
- 释锁用 Lua 比较 value 后删除（避免删掉别人的锁），写法照抄 `scheduled_report_service.go`。
- `State` 为 nil（控制面不会发生，仅作防御）→ 不启动调度并 `slog.Warn`；`SetNX` 报错 → 本轮跳过（宁可漏跑一次也不并发写）。

### 5.4 失败与重试

- 一次调度最多 3 次尝试：0s / 30s / 2min；每次尝试沿用 `Sync` 内部 30s 超时。
- 任一次成功即结束，写 `status=success`；3 次全失败写 `status=failed` + `last_sync_error`（截断 500 字），等下一次到点。
- 重试只对"上游失败"生效；`ErrSyncInProgress`（手动触发正在进行）直接跳过本轮，不算失败。
- 退避通过注入 `sleepFn` 便于测试，不真 sleep。
- **失败冷却**：3 次全失败后写 `ai_app_hub:sync:last_failure`，30 分钟内不再起新的一轮（否则上游宕机时每 1 分钟 tick 都会再打 3 次，一天上千次）。成功会清掉这个键。

## 6. 删除对账（仅 `scope=all`）

### 6.1 规则

`Sync` 在写完部门与工作室、回填完 `studio_id` 之后，若 `scope.Kind==all`：

1. 取本次 EHR 返回的部门 ID 集合 `keep`。
2. 执行 `HideStudiosNotInDepartments(ctx, keep)`：

```sql
UPDATE ai_app_studios
   SET visible = false, updated_at = now()
 WHERE source = 'ehr'
   AND department_id IS NOT NULL
   AND department_id <> ''
   AND visible = true
   AND department_id NOT IN (keep...)
```

3. 影响行数记为 `deleted_studios`，写入响应体与状态键。

安全护栏：

- `items` 为空时**不做任何隐藏**（`ehrclient` 已把空结果转成 `ErrEmptyResult`，服务层再防一次），避免"接口返回空 → 全部隐藏"。
- **部分树护栏**：EHR 是单次不分页调用，"200 但条目少了一大截"（上游部分故障、org 范围/权限变化）与"真的撤并"无法区分。因此本次 `len(items)` 低于上次成功部门数的 80%（且上次 ≥ 10 个）时，本轮跳过隐藏并记 `slog.Error`；成功同步把 `len(items)` 写进 `ai_app_hub.last_sync_departments` 作为下次的基线。代价：真出现一次性大规模撤并（>20%）需要连着跑两次或手工同步才生效。
- 只动 `source='ehr'` 且有 `department_id` 的行：手工工作室（`department_id IS NULL`）与 `manual` 覆盖行完全不受影响。
- 不物理删除 `ai_app_studios` 与 `ai_apps`：应用挂在工作室下，删行会丢运营数据；`visible=false` 即可从 H5 消失。
- 隐藏是可逆的：运营在管理端把 `visible` 打开即可恢复。
- **不自动复活**：部门重新出现在 EHR 时，`UpdateStudioEHRFields` 不写 `visible`，避免同步把运营的显隐决策改回去。

### 6.2 为什么不同时标记 `ai_app_departments`

部门镜像行今天的唯一消费者是同步自身（回填 `studio_id`）与后续 H5 的"用户部门 → 事业部"上溯。EHR 撤销某节点后：

- 物理删行会断掉上溯链路（用户仍可能带着旧 dept_id 登录）；
- 加 `visible/deleted_at` 列需要一次 Atlas 迁移，而现在没有任何展示面会读它。

结论：本轮**不动** `ai_app_departments`；等 H5/管理端要展示部门树时再加列，届时一次迁移解决。

### 6.3 幂等

同一批数据重复对账，UPDATE 命中 0 行；`deleted_studios=0`。

## 7. 接口与契约

`POST /api/v1/admin/ai-app-hub/sync` 响应体新增字段（纯增量，老前端不受影响）：

```json
{"scope":"all","departments":99,"studios":12,"hidden":2,"deleted_studios":1,"duration_ms":842}
```

按仓库约定同步更新 `tests/contract/contracts.json` 中该接口的响应结构。

## 8. 影响清单

| 文件 | 改动 |
|---|---|
| `internal/controlplane/service/ai_app_hub_cron.go` | 重写：cron 解析、ticker、leader 锁、last_success、补跑、重试 |
| `internal/controlplane/service/ai_app_hub_sync.go` | `HubSyncResult` 加 `DeletedStudios`；`all` 范围追加对账；状态键写入 |
| `internal/controlplane/repository/ai_app_hub_repo.go` | 新增 `HideStudiosNotInDepartments` |
| `internal/controlplane/router/router.go` | 接线改配置（cron/tz），删除 `aiAppHubSyncInterval` |
| `tests/controlplane/service/ai_app_hub_cron_test.go` | 重写：判定、补跑、锁、重试 |
| `tests/controlplane/service/ai_app_hub_sync_test.go` 等 | 补删除对账用例 |
| `tests/contract/contracts.json` | 响应字段 |

无 schema 变更，无新依赖。

## 9. 测试策略

| 层 | 用例 | 断言 |
|---|---|---|
| 到点判定 | 表驱动：`last_success` / `now` / 表达式 / 时区组合 | 该跑 / 不该跑；UTC 容器下 19:00 UTC = 次日 03:00 上海 |
| 补跑 | 昨天成功、今天 10:20 启动 | 跑一次；再 tick 不再跑 |
| 首次 | 无 `last_success` | 立即跑一次 |
| 互斥 | miniredis，两个调度器同时 tick | 只有一个执行；释锁后另一个可执行 |
| 重试 | 前两次报错、第三次成功 | 调用 3 次，`status=success`；三次全败 `status=failed` |
| 空结果 | fetcher 返回空 | 不执行隐藏，返回错误 |
| 对账 | 3 个 studio（在集合内 / 不在集合内 / `manual`） | 只有"不在集合内且 source=ehr"被隐藏；`manual` 与 `ai_apps` 不变 |
| 非 all | `scope=department` | 不触发隐藏 |
| 幂等 | 连续两次 `all` | 第二次 `deleted_studios=0` |

## 10. 已知取舍与风险

1. **部门行不标记**（§6.2）：库里会留存 EHR 已撤销的部门节点，等 H5 需要区分时再加列。
2. **`visible` 一个布尔值承担两件事**（同步隐藏 / 运营隐藏）：无法区分来源。要区分就加 `hidden_by` 列，本轮不做。
3. **03:00 是 EHR 的什么窗口未知**：需与接口中心确认该时段可运维（有维护窗口就改 `AI_APP_HUB_SYNC_CRON`，不用发版）。
4. **时区依赖镜像里的 tzdata**：`deploy/docker/control-plane.Dockerfile` 已装 tzdata 且 `ENV TZ=Asia/Shanghai`；`LoadLocation` 失败有 `time.Local` 与 UTC 两级回退，不会 panic。
5. **多副本**：leader 锁已支持多副本，但锁 TTL 固定 10 分钟；若未来单次同步超过 10 分钟（树规模暴涨），需改锁续期。当前 99 条约 1–3 秒。

## 11. 发布与回滚

- 无需数据库迁移；env 全部有默认值，合并即生效。
- 停用调度：`AI_APP_HUB_SYNC_CRON=""`（重启控制面生效），无需回滚代码。
- 回滚代码后行为回到"启动即跑 + 6 小时间隔"，已发生的 `visible=false` 不会被撤销（数据层不依赖调度器）。
