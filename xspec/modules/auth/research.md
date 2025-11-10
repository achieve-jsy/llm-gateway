# auth 技术研究索引

## 0. 文档信息

| 属性 | 内容 |
| :--- | :--- |
| **所属变更** | CHG-20260523-001 |
| **模块名称** | auth |
| **模块类型** | web-backend |
| **创建时间** | 2026-05-23 |

---

## 1. 课题索引

> 所有研究课题的结论摘要，供 design / detail / task / implement 命令快速读取。
> 完整分析见各课题子文件（链接在「详情」列）；inline 表示内容直接写在本文件下方。
> **状态说明**：✅ 已决策 · 🔄 研究中 · ⏸️ 待研究 · ⚠️ 有遗留风险需跟进

| # | 课题 | 结论（一句话） | 决策 | 状态 | 详情 |
|---|------|--------------|------|------|------|
| 01 | RS256 密钥轮换 — 双公钥验证窗口 | 采用 JWKS 端点 + kid 匹配：auth 提供 `/.well-known/jwks.json` 端点返回多公钥集，JWT header 注入 kid 字段，验证方按 kid 匹配验证密钥，过渡窗口 7 天 | 方案 A（JWKS + kid） | ✅ | → research/001-rs256-key-rotation.md |
| 02 | Key 缓存主动失效 — Redis pub/sub | 复用 control-plane 已有 `config:change` 通道：auth 订阅同一通道按 ResourceType=="key" 过滤，收到 Key 事件后全量清空 `key:*` 缓存 | 方案 A（复用 config:change） | ✅ | → research/002-key-cache-invalidation.md |
| 03 | OIDC Claims 角色映射 — admin/user 判定 | 采用 YAML/Viper 配置映射：声明 claim_path + admin_match_values，OIDCService 从 id_token claims 读取指定 claim 与配置值比对，匹配则 admin 否则 user | 方案 A（配置映射） | ✅ | → research/003-oidc-role-mapping.md |
| 04 | Key 查询契约 — auth 与 control-plane 的 Key 验证接口 | 采用单次合并验证端点：`POST /internal/v1/keys/verify`，请求体携带 Key 原文，control-plane 一次返回 key_id + status + user_id + department_id，内部 Token 认证，200 + status 字段区分 Key 不存在/禁用/删除 | 方案 A（单次合并验证） | ✅ | → research/004-key-query-contract.md |

---

## 2. 内联课题（浅研究）

（所有课题已拆至 research/ 子文件：001–004）

---

## 3. 遗留风险汇总

| 风险 | 来源课题 | 可能性 | 影响 | 缓解措施 |
| :--- | :--- | :---: | :---: | :--- |
| JWKS 端点不可用时验证方无法获取新公钥 | R-03 | 低 | 中 | 验证方内存常驻缓存上次 JWKS 结果，定时 300s 刷新兜底；拉取失败记日志触发告警 |
| 过渡窗口过长增大旧公钥攻击面 | R-03 | 低 | 低 | 过渡窗口设 7 天（覆盖 Refresh Token 最大有效期），到期后移除旧 JWKS 条目 |
| kid 注入遗漏导致 JWT 无 kid 字段 | R-03 | 低 | 中 | 签发单元测试强制校验 JWT header 含 kid 字段；JWKS 验证方对无 kid 的 JWT 拒绝验证 |
| Redis pub/sub 消息丢失（auth 断线期间） | R-04 | 低 | 中 | TTL 60s 兜底保证最坏 60s 感知；auth 启动后订阅成功消息不丢失 |
| 全量清空缓存后短暂穿透到 control-plane | R-04 | 低 | 低 | singleflight 合并并发穿透；Key 变更频率极低穿透量 < 5 QPS |
| 公司 SSO 不提供角色/组 claim | R-05 | 中 | 中 | claim_path 缺失时 fallback 到 default_role="user"；管理员通过 ops-console 手动修改 users.role |
| SSO claim 值格式与配置不匹配 | R-05 | 中 | 中 | R-01 联调时与 IT 部门确认 claim 结构和值格式 |
| 配置映射规则被错误修改导致非预期 admin | R-05 | 低 | 高 | 配置通过环境变量/Secret 注入不进入代码库；admin 规则变更需审批流程 |
| control-plane 日志泄漏 Key 原文（未做请求体脱敏） | R-06 | 中 | 高 | control-plane 中间件新增 /internal/v1/keys/verify 端点请求体脱敏规则；上线前代码审查强制检查 |
| control-plane 查询 P95 > 50ms 导致 auth 中间件 P95 超标 | R-06 | 低 | 高 | Redis 缓存命中率 ≥ 90% + singleflight 合并穿透；control-plane JOIN 单次查询避免两次 DB 往返 |
| control-plane 不可用时 auth 中间件全部穿透失败 | R-06 | 低 | 高 | Redis 缓存兜底已缓存 Key（有限降级窗口 TTL 60s）；不可用时返回"服务暂不可用"不放行 |