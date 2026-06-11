# 覆盖度与可靠性审计报告（t3）

审计范围：LLM Gateway 控制面操作审计日志（.worktrees/audit-log-review）。方法：将 router.go 全部写接口（POST/PATCH/PUT/DELETE）与 auditRouteMap、手动埋点逐一比对，并审阅中间件启发式回退、Recorder 写路径、迁移目录。

## 一、按严重度分组的缺陷

### [严重] 1. 配额/豁免手动埋点未 MarkRecorded -> 同一请求写两条审计，action 命名分裂
- 证据：quota_handler.go:226-243（recordAudit 不调用 audit.MarkRecorded）、quota_exemption_handler.go:69-80/104-115（同样只 Record 不标记）；中间件 audit.go:254 仅在 audit.IsRecorded(c) 时才跳过。
- 影响：PATCH /users|keys|groups|models/:id/quota、POST/DELETE /quota/exemptions 这 6 个写接口，成功请求被写两次：一次手动（action="quota.user.update", resource_type="user"），一次中间件启发式（action="user.quota.update", resource_type="users"）。失败请求只记一次。审计量翻倍、命名分裂、按 action 检索不可靠。对照组 service_key_handler.go:470-475 的 recordAudit 正确带 MarkRecorded。
- 修复：quota/quota_exemption 的 recordAudit 内补 audit.MarkRecorded(c)，与 service_key_handler 对齐；或改为中间件自动审计 + audit.SetActionOverride。

### [严重] 2. /internal/v1/* 与 auth/opencode 全部写接口零审计
- 证据：internalGroup 仅挂 InternalAuth 无 AuditMiddleware（router.go:767-777、815-819）；internalOrgGroup（router.go:991-995）同样无审计；顶层 POST /auth/oidc/callback、/auth/refresh、/auth/logout、/auth/dev/login、POST /api/v1/opencode-config/callback（router.go:67-69、77、388）。
- 影响：POST /internal/v1/keys/verify、/auth/verify-key、/budget/evaluate-alert、/video/tasks、PATCH /video/tasks/:id(+/status)、/org/auth/check-scope 等高权限内部写操作完全无留痕；登录/登出也零审计（types.go:85-86 的 ActionUserLogin/ActionUserLogout 常量从未被使用，属死代码）。
- 修复：internalGroup 追加审计中间件（或对 verify-key/evaluate-alert/video/tasks 手动埋点）；auth 登录/登出补审计。

### [高] 3. inferResourceID 只认 id/model_id/eid，漏 allocation_id/dept_id -> resource_id 大量为空
- 证据：audit.go:578-589 仅 c.Param("id"/"model_id"/"eid")。
- 影响：PUT /budgets/:allocation_id/tier-limits（router.go:501，已映射 ActionBudgetTierLimitSet/ResourceBudget）落库时 resource_id 恒为空；PUT /dept-policies/:dept_id/key-creation（router.go:872）同样为空。预算分层限额等敏感变更无法定位到具体 allocation。且 model_id/eid 分支在「同时存在 id」的路由里是死分支（id 恒优先生效）。
- 修复：inferResourceID 增加 allocation_id/dept_id/user_id/subject_id；或在映射表按路由显式指定 resourceID 参数名。

### [高] 4. 大量写接口未入 auditRouteMap，靠 inferAction 启发式产生散乱/错误命名
- 证据：audit.go:494-536（inferAction 用 singularize+methodToVerb 拼字符串）；以下写接口不在映射表且无手动埋点：providers/quick-onboard(-preview)（474-475）、groups/:id/reset-capability（713）、expansion-requests PATCH :id（853）、key-requests create/cancel/decision（862/868/869）、dept-policies PUT（872）、channel-monitors/channel-monitor-templates 全部 7 条写（876-887）、ops 写接口全部 17 条（599-651）、budgets/multi-period、budgets/batch/preview、budgets/batch/apply、budgets/tier-limits/batch/preview（837/839/840/502）。
- 影响：产出 "op.alert-rule.create"、"service-key.access-all-group.create"、"expansion-request.update"、"key-request.create" 等与官方常量（provider.create、service_key.*、expansion.*）风格不一致的 action；resource_type 取未单数化原始段（"service-keys"、"ops"）。审计检索/告警规则几乎无法稳定匹配。
- 修复：把这些写接口补进 auditRouteMap 并统一用 types.go 常量；或至少让 inferAction 的 singularize/resourceType 与常量命名一致。

### [高] 5. Service Key 双轨埋点：4 条 MarkRecorded、2 条靠启发式，命名分裂
- 证据：service_key_handler.go:115/237/323/374 手动埋点（create/toggle/ip-allowlist/delete）且 recordAudit 带 MarkRecorded（470-475）；但 UpdateGroups(408-439) 与 ToggleAccessAllGroups(441-467) 无埋点、无 MarkRecorded，且不在 auditRouteMap（audit.go:114-118 只含 status/ip-allowlist/delete）。
- 影响：同一资源域 6 个写操作出现两套命名——手动 "service_key.create/toggle/delete"（resource "service_key"）vs 启发式 "service-key.group.create"/"service-key.access-all-group.create"（resource "service-keys"）。映射表 service-key 4 条（audit.go:115-118）因 handler 先 MarkRecorded 成死代码。
- 修复：为 UpdateGroups/ToggleAccessAllGroups 补手动埋点（或删 service-key 手动埋点统一走中间件 + SetActionOverride）。

### [中] 6. Record 写失败静默吞掉；CreateAsync 死代码且裸 goroutine
- 证据：service/audit_log_service.go:67-73 写库失败仅 slog.Warn，无告警/重试/指标；repository/audit_log_repo.go:24-32 CreateAsync 用裸 goroutine 且 _ = err 吞错，grep 确认全仓库无调用方（死代码）。
- 影响：DB 抖动或 audit_logs 表缺失时审计静默全丢，运维无法感知；死代码误导后人以为有异步路径。
- 修复：审计写入改异步队列+有限重试+失败计数指标（Prometheus/OTel）+落文件兜底；删除 CreateAsync。

### [中] 7. auditUserCache sync.Map 无过期淘汰 -> 缓慢无限增长
- 证据：audit.go:178-220——entry 带 expireAt 但只在命中过期时被覆写，从不 Delete；无清理 goroutine。
- 影响：每出现一个新 userID 就永久驻留一条（name/dept 字符串），长时间运行内存缓慢泄漏。
- 修复：加定时清理或改带 TTL 的 LRU（golang-lru expirable），或记录上次清理时间轮询扫删。

### [中] 8. 双迁移目录并存且内容不一致 -> 迁移混乱风险
- 证据：migrations/（001..061 编号 + 自己的 atlas.sum）与 atlas-migrations/（时间戳 + atlas.sum）并存；audit_logs 表只在 atlas-migrations/20260624010000_add_audit_logs_and_ip_allowlist.sql，migrations/ 里 grep audit_logs 为 0；atlas.hcl 与 .gitlab-ci.yml 均只指向 file://atlas-migrations（atlas.hcl:14/23/32/41、.gitlab-ci.yml:286-287），而 AGENTS.md 却写「新增 migrations/NNN_*.sql」。
- 影响：若任何部署/本地仍走旧 migrations/（golang-migrate）路径，audit_logs 表不会被创建，AuditLogService.Record 全部静默失败（叠加缺陷 6 -> 审计零留存）；文档与 CI 现实矛盾误导新人。
- 修复：明确 Atlas 为唯一迁移源，删除或冻结 legacy migrations/（含其 atlas.sum），更新 AGENTS.md 迁移说明。

### [低] 9. auditRouteMap 约 35 处内联 action + 6 处内联 resource 字符串未入 types.go 常量
- 证据：audit.go:31、41-50、53-58、70、74、77-79、82、88、97、99-100、103-108、111-112、143-152、156-157 大量 audit.Action("...") / audit.Resource("studio"/"department"/"role")。
- 影响：命名散乱易拼错（"model.bind_catalog" 与 "tag.create" 并存），grep 定位困难，无法编译期防错。
- 修复：全部提升为 types.go 常量。

### [低] 10. MarkRecorded 后中间件直接 return，丢失富字段
- 证据：audit.go:254-256——手动埋点路由（service-key 4 条）跳过中间件组装，缺 details 里的 request_body/response_body/duration_ms/full_route/request_id/referer/request_headers。
- 影响：手动审计记录信息量低于自动记录，检索/取证维度不一致（手动记录 status_code 恒 200、无 UA）。
- 修复：手动埋点统一补齐字段，或改造为 SetActionOverride + SetResourceName + SetBeforeAfter + SetScope 让中间件统一落库。

### [低] 11. 邮件告警审计可能双写（两条独立路径）
- 证据：router.go:227 emailNotifier.SetAuditRecorder(auditSvc)（notifier.go:163-182 recordAudit）与 router.go:262 dispatcher.SetAuditRecorder(newEmailAuditAdapter(auditSvc))（email_audit_adapter.go:18-34）同时挂到 budgetAlert 两条分发通道（sharedNotifier vs dispatcher）。
- 影响：同一封预算告警邮件可能产生两条 action="email.send_alert" 记录；均无 user_id/user_name。
- 修复：统一邮件审计出口，去重。

## 二、审计盲区清单（写接口未覆盖明细）

A. 完全无审计（不在 adminGroup，无手动埋点）：
- POST /internal/v1/keys/verify（773）、POST /internal/v1/auth/verify-key（774）、POST /internal/v1/budget/evaluate-alert（815）
- POST /internal/v1/video/tasks（817）、PATCH /internal/v1/video/tasks/:id（818）、PATCH /internal/v1/video/tasks/:id/status（819）
- POST /internal/v1/org/auth/check-scope（994）
- POST /auth/oidc/callback（67）、/auth/refresh（68）、/auth/logout（69）、/auth/dev/login（77，条件注册）
- POST /api/v1/opencode-config/callback（388）

B. 仅靠 inferAction 启发式（adminGroup 内、无映射、无手动埋点）：
- providers：POST /providers/quick-onboard/preview（474）、POST /providers/quick-onboard（475）
- groups：POST /groups/:id/reset-capability（713）
- expansion：PATCH /expansion-requests/:id（853）
- key-requests：POST /key-requests（862）、POST /key-requests/:id/cancel（868）、POST /key-requests/:id/decision（869）
- dept-policies：PUT /dept-policies/:dept_id/key-creation（872）
- budgets：PUT /budgets/multi-period（837）、POST /budgets/batch/preview（839）、POST /budgets/batch/apply（840）、POST /budgets/tier-limits/batch/preview（502）
- channel-monitors：POST /channel-monitors（876）、PUT /channel-monitors/:id（878）、DELETE /channel-monitors/:id（879）、POST /channel-monitors/:id/run（880）、POST /channel-monitor-templates（884）、PUT /channel-monitor-templates/:id（886）、DELETE /channel-monitor-templates/:id（887）
- ops（全部 17 条）：POST /ops/alert-rules（599）、PUT /ops/alert-rules/:id（600）、DELETE /ops/alert-rules/:id（601）、PUT /ops/alert-events/:id/status（604）、POST /ops/alert-silences（605）、POST /ops/system-logs/cleanup（610）、PUT /ops/runtime/logging（614）、POST /ops/runtime/logging/reset（615）、PUT /ops/settings/metric-thresholds（622）、PUT /ops/settings/dashboard（624）、PUT /ops/settings/advanced（626）、PUT /ops/settings/email-notification（640）、POST /ops/settings/email-notification/test（641）、PUT /ops/settings/alert-runtime（645）、PUT /ops/error-logs/:id/resolve（649）、PUT /ops/request-errors/:id/resolve（650）、PUT /ops/upstream-errors/:id/resolve（651）

C. 双写（手动埋点但未 MarkRecorded）：
- PATCH /users/:id/quota（902）、PATCH /keys/:id/quota（905）、PATCH /groups/:id/quota（908）、PATCH /models/:id/quota（911）、POST /quota/exemptions（919）、DELETE /quota/exemptions/:id（920）

D. 命名分裂（service-key 未手动埋点、靠启发式）：
- POST /service-keys/:id/groups（829）、POST /service-keys/:id/access-all-groups（830）

## 三、最该先修的 5 条
1. 修 quota/quota_exemption 双写：recordAudit 统一 MarkRecorded（消灭双写 + action 分裂）。
2. 给 /internal/v1/* 关键写接口（verify-key、budget/evaluate-alert、video/tasks、org/auth/check-scope）补审计。
3. 补 inferResourceID 的 allocation_id/dept_id，并把 ops/channel-monitor/key-request/expansion PATCH/budgets 批量子路由全部纳入 auditRouteMap（消灭启发式盲区）。
4. 审计写失败从 slog.Warn 升级为异步队列+重试+指标+落文件兜底，删除死代码 CreateAsync。
5. 收敛迁移目录：Atlas 为唯一源，删除/冻结 legacy migrations/（含其 atlas.sum），同步 AGENTS.md，确保 audit_logs 表在所有环境必然创建。