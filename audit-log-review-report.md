# 审计日志全面审计报告

> 分支：audit-log-review（worktree，基于 origin/main @ 6cee4d55）
> 分析方式：AgentTeams 三成员并行审计（安全/数据合规、性能/可扩展性、覆盖度/可靠性）+ captain 独立复核
> 结论：**是，当前审计日志问题很大。** 存在「审计日志本身明文记录 API Key」与「写请求体 >64KB 被静默截断导致功能不可用」两类 Critical 缺陷，另有多项高/中危问题。

## 一、Critical（严重，须立即处理）

### C1. API Key / 上游凭证明文写入审计日志（密钥泄漏）
- 根因：`middleware/audit.go` 的 `sanitizeBody` 只对 JSON **顶层** key 脱敏（不递归），且敏感词表 `sensitiveKeys` 不含 `key`/credentials。
- 泄漏链（captain 已独立复核确认）：
  - `key_handler.go:124` CreateKey、`:361` ResetKey、`:392` RevealKey 均返回 `{"data":{...,"key": plaintext}}`，明文 key 在 `data.key` 内层，顶层只有 `data`，不命中脱敏。
  - 中间件捕获响应体后 `details["response_body"]` 原样落库（audit.go:291-299）。
  - `provider_service.go` 的 `credentials` 字段（值为字符串化 JSON，含上游 api_key）同样被写入 `details.request_body`。
- 影响：任何持 `audit:view:scoped` 的角色或只读 DB 账号即可从审计日志回收明文密钥。
- 修复：递归脱敏 + 纳入 key/credentials/sk- 前缀；或 reveal/reset/create 走手动埋点 MarkRecorded，只记 masked 值、不落 body。

### C2. 写请求体 >64KB 被静默截断（功能直接不可用）
- 证据：`middleware/audit.go:239-242` 用 `io.LimitReader(body, 64*1024)` 读取后**直接替换** body，剩余流被丢弃，注释「还原供下游处理」与实现不符。
- 影响：员工导入（multipart 10MB）、settings import、批量模型/预算等 >64KB 写请求在进 handler 前被截断 → unexpected EOF / 400，功能失效。
- 修复：改用 `io.TeeReader` 完整还原；或按 ContentLength 门控（>阈值/multipart 不捕获 body 只记元数据）。

## 二、High（高）

### H1. 每个管理面写操作同步 INSERT，`CreateAsync` 是死代码
- `audit.go:359 → audit_log_service.go:67 → repo.Create` 全链路同步；`repository/audit_log_repo.go:25` 的 CreateAsync 零调用。
- 影响：批量写场景串行同步 INSERT 放大尾延迟、占用共享 DB 连接池（审计失败不阻断业务，但延迟非零成本）。
- 修复：有界 channel + 批量 flush 的异步写（参考同项目 `NewSystemLogWriter`）。

### H2. `audit_logs` 无保留期/无清理，表无限膨胀
- `repository/audit_log_repo.go:74 DeleteBefore` 零调用；`ops_handler.go CleanupSystemLogs` 只清 system_logs。
- 量化：估 ~200 万行/年、~8-12GB/年，永久累积，List 的 count+offset 深分页随之退化。
- 修复：接通 DeleteBefore 到清理 cron + 按 created_at 分区/定时 drop。

### H3. 客户端 IP 可伪造
- `audit.go:569-577 extractClientIP` 直接信任 X-Forwarded-For / X-Real-IP（来自客户端）。
- 修复：仅信任可信反代覆盖的头或 `gin.SetTrustedProxies` + `c.ClientIP()`，并 `net.ParseIP` 校验。

### H4. 审计盲区：/internal/v1/*、auth、opencode 写接口零审计
- `internalGroup`（router.go:767-819）仅挂 InternalAuth 无 AuditMiddleware；登录/登出也零审计；`ActionUserLogin/Logout` 为死常量。
- 影响：verify-key、budget/evaluate-alert、video/tasks、org/auth/check-scope 等高权限写操作无留痕。
- 修复：internalGroup/auth 追加审计中间件或手动埋点。

### H5. quota/quota_exemption 手动埋点未 MarkRecorded → 双写 + action 分裂
- `quota_handler.go:226-243`、`quota_exemption_handler.go:69-80/104-115` 只 Record 不 MarkRecorded，中间件再记一条。
- 影响：同一请求写两条（`quota.user.update` vs `user.quota.update`），审计量翻倍、命名分裂、检索不可靠。
- 修复：recordAudit 内补 `audit.MarkRecorded(c)`。

### H6. inferResourceID 漏 allocation_id/dept_id，resource_id 大量为空
- `audit.go:578-589` 只取 id/model_id/eid，漏 allocation_id/dept_id/user_id。
- 影响：`PUT /budgets/:allocation_id/tier-limits` 等落库 resource_id 恒空，无法定位到具体 allocation。

## 三、Medium（中）

- M1. 大量写接口（ops 17 条、channel-monitor 7 条、key-request、expansion PATCH、budgets 批量等 ~40 条）未入 auditRouteMap，靠 inferAction 启发式产生 `op.alert-rule.create` 等散乱命名。
- M2. Service Key 双轨埋点：4 条手动（含 MarkRecorded）+ 2 条靠启发式，同一资源域两套命名。
- M3. Record 写失败仅 slog.Warn 静默吞掉，无告警/重试/指标（叠加 C 类「迁移缺失」风险 = 审计静默全丢）。
- M4. `lookupUserIdentity` 用 `id::text = ?` 函数转换，使 uuid 主键走全表扫描。
- M5. `dept_id`(text) 建 btree 索引 + 缺组合索引 + count+OFFSET 深分页慢。
- M6. 同一 body 被 sanitizeBody/inferResourceName 重复 json.Valid/Unmarshal/Marshal 3~5 次。
- M7. `auditUserCache` sync.Map 无过期淘汰，内存缓慢无限增长。
- M8. 双迁移目录并存（migrations/ vs atlas-migrations/），audit_logs 只在 atlas-migrations；AGENTS.md 与 CI 现实矛盾，若走 legacy 路径则表缺失 + 审计静默失败。
- M9. details 无条件存完整 request/response body + 员工 PII + before/after，违反数据最小化/GDPR。

## 四、Low（低）

- L1. truncate 按字节截断切断 UTF-8（中文姓名/资源名乱码、可能产生非法 JSON）。
- L2. responseBodyWriter 只覆写 Write 未覆写 WriteString，部分响应体漏捕获。
- L3. auditRouteMap ~35 处内联 action/6 处内联 resource 字符串未入 types.go 常量。
- L4. MarkRecorded 后中间件直接 return，手动记录丢失 request/response body、UA、duration 等富字段。
- L5. 邮件告警审计双路径可能双写（emailNotifier + dispatcher 两条 AuditRecorder）。
- L6. DeleteBefore 硬删除无哈希链/WORM，append-only 仅靠「无写路由」保证。

## 五、修复优先级路线图

- **P0（立即）**：C1 密钥明文落库、C2 请求体截断 —— 一个泄漏密钥、一个打挂导入功能。
- **P1（本周）**：H5 双写去重、H1 异步写、H2 保留策略/分区、H3 IP 伪造、H6 resource_id 补参。
- **P2（迭代）**：H4 内部接口审计盲区、M1/M2 命名收敛、M3 写入可靠性（DLQ+指标）、M4/M5 索引与分页、M8 迁移目录收敛、M9 最小化脱敏。

---
*完整三份原始报告见各任务 output；覆盖度明细（盲区清单 A/B/C/D）另见 `audit-coverage-report.md`。*
