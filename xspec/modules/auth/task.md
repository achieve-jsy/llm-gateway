# auth 后端任务清单

## 文档信息

| 属性 | 内容 |
| :--- | :--- |
| **所属变更** | CHG-20260523-001 |
| **模块名称** | auth |
| **模块类型** | web-backend |
| **创建时间** | 2026-05-23 |

---

## 任务格式

```
- [ ] T{NNN} [P?] {优先级} 任务描述（含具体文件路径）
  depends: T{NNN}, T{NNN}@{other-module}
```

| 标记 | 含义 |
| :--- | :--- |
| `T{NNN}` | 模块内唯一编号，从 T001 起编 |
| `[P]` | 可并行：操作不同文件、无依赖未完成任务 |
| 优先级 | P0（必须）/ P1（重要）/ P2（可选） |
| `depends:` | 依赖标注（缩进换行）。模块内：`T010`；跨模块：`T020@control-plane`。无依赖则不写此行 |

---

## 阶段 1：环境与基础

**目标**：auth 服务骨架、配置、中间件、加密与基础设施就绪

- [x] T001 [P] P0 统一 auth 文档中的接口路径与 JWT 生命周期口径，收敛 `/auth/oidc/*`、`/.well-known/jwks.json` 与 `30min + 7d rotation` 约定
  文件：`xspec/changes/auth/spec.md`, `xspec/changes/auth/hld.md`, `xspec/changes/auth/design.md`, `xspec/changes/auth/contracts.md`
- [x] T002 [P] P0 搭建 auth 服务启动入口、路由分组、配置加载与健康检查
  文件：`cmd/auth/main.go`, `internal/auth/config/config.go`, `internal/auth/handler/health.go`
  depends: T001
- [x] T003 [P] P0 建立请求链基础中间件（RequestID、结构化日志、Recovery、RateLimit）
  文件：`internal/auth/middleware/requestid.go`, `internal/auth/middleware/logger.go`, `internal/auth/middleware/recovery.go`, `internal/auth/middleware/ratelimit.go`
  depends: T002
- [x] T004 [P] P0 实现 RS256/JWKS 基础能力，包含私钥加载、`kid` 注入、JWKS 输出结构
  文件：`internal/auth/crypto/jwt.go`, `internal/auth/handler/jwks.go`, `internal/auth/dto/token.go`
  depends: T002
- [x] T005 [P] P0 接入 PostgreSQL / Redis / HTTP client 基础设施与启动期依赖检查
  文件：`cmd/auth/main.go`, `internal/auth/config/config.go`, `internal/auth/infra/postgres.go`, `internal/auth/infra/redis.go`, `internal/auth/infra/http.go`
  depends: T002

**✓ 检查点**：auth 服务可启动，健康检查可访问，基础配置与依赖注入正常

---

## 阶段 2：数据层

**目标**：用户、部门、开发用户模型与仓储就绪

- [x] T010 [P] P0 落地 users / departments 初始迁移与索引
  文件：`migrations/001_init_schema.up.sql`, `migrations/001_init_schema.down.sql`
  depends: T005
- [x] T011 [P] P1 落地 dev_users 开发环境专用迁移
  文件：`migrations/002_dev_users.up.sql`, `migrations/002_dev_users.down.sql`
  depends: T005
- [x] T012 [P] P0 定义用户、部门、开发用户实体与 GORM 映射
  文件：`internal/auth/model/user.go`, `internal/auth/model/dept.go`, `internal/auth/model/dev_user.go`
  depends: T010, T011
- [x] T013 [P] P0 实现用户与部门仓储（FindBySub/FindByDeptID/Create/Update/FindByID）
  文件：`internal/auth/repository/user.go`, `internal/auth/repository/dept.go`
  depends: T012
- [x] T014 [P] P1 实现开发用户仓储（FindByUsername/Create）
  文件：`internal/auth/repository/dev_user.go`
  depends: T012

**✓ 检查点**：users / departments / dev_users 迁移与仓储可读写，索引与唯一约束生效

---

## 阶段 3：功能实现

**目标**：按用户故事实现 SSO、JWT、API Key 鉴权与开发模式回退

### US-SSO-01 / US-SSO-02 — SSO 登录与用户部门写入 (P0)

**目标**：员工完成 SSO 登录后可建立平台登录态，并将用户/部门数据落库
**独立验证**：完成一次 OIDC 登录回调后，数据库存在对应 users/departments 记录，返回 access/refresh token
**对应验收标准**：AC-01, AC-02, AC-03, AC-04, AC-05, AC-06, AC-07, AC-08

- [x] T020 P0 实现 OIDC 登录发起与 state 存储、CSRF 防护
  文件：`internal/auth/service/oidc.go`, `internal/auth/handler/auth.go`, `internal/auth/dto/auth.go`
  depends: T003, T005
- [x] T021 P0 实现 OIDC 回调处理：code 兑换、id_token 验签、claims 提取与必填字段校验
  文件：`internal/auth/service/oidc.go`, `internal/auth/handler/auth.go`, `internal/auth/dto/auth.go`
  depends: T020
- [x] T022 [P] P0 实现用户/部门 Upsert：首次创建、重复登录更新、部门缺失可空
  文件：`internal/auth/service/user.go`, `internal/auth/repository/user.go`, `internal/auth/repository/dept.go`
  depends: T013, T021
- [x] T023 [P] P0 实现登录态签发：SSO 成功后生成 access token + refresh token
  文件：`internal/auth/service/token.go`, `internal/auth/crypto/jwt.go`, `internal/auth/dto/token.go`
  depends: T004, T022

**✓ 检查点**：SSO 登录、数据写入、JWT 签发独立可演示

### US-JWT-01 / US-JWT-02 — JWT 登录态与验证 (P0)

**目标**：管理接口和员工自服务接口可基于 JWT 持续识别用户身份
**独立验证**：携带有效 JWT 请求管理接口可通过，过期/篡改 token 被拒绝
**对应验收标准**：AC-09, AC-10, AC-11, AC-23

- [x] T024 P0 实现 JWT 验证中间件：签名、过期、用户存在性、角色注入
  文件：`internal/auth/middleware/jwt_auth.go`, `internal/auth/service/token.go`, `internal/auth/repository/user.go`
  depends: T023, T013
- [x] T025 [P] P1 实现 Refresh Token Rotation、黑名单与登出
  文件：`internal/auth/handler/token.go`, `internal/auth/service/token.go`, `internal/auth/dto/token.go`
  depends: T023, T024
- [x] T026 [P] P1 实现 JWKS 对外端点与定时密钥轮换支持
  文件：`internal/auth/handler/jwks.go`, `internal/auth/crypto/jwt.go`
  depends: T004, T025

**✓ 检查点**：JWT 校验、刷新、登出与 JWKS 能独立验证

### US-SSO-03 — 开发模式账号密码回退 (P1)

**目标**：开发环境可用账号密码登录，生产环境严格禁用
**独立验证**：dev 模式下账号密码登录成功，production 模式同接口返回 403
**对应验收标准**：AC-21

- [x] T027 [P] P1 实现开发模式登录、bcrypt 校验与生产禁用保护
  文件：`internal/auth/handler/auth.go`, `internal/auth/service/user.go`, `internal/auth/repository/dev_user.go`, `internal/auth/dto/auth.go`
  depends: T014, T023

**✓ 检查点**：dev 登录与 production 禁用策略可独立验证

### US-KEY-AUTH-01 / US-KEY-AUTH-02 / US-KEY-AUTH-03 — API Key 鉴权链路 (P0)

**目标**：请求携带 `sk-` Key 时，auth 可解析出 `user_id/key_id/department_id` 并拒绝失效 Key
**独立验证**：调用 `POST /internal/v1/auth/verify-key` 返回 active/disabled/not_found，gateway-api 可消费该结果
**对应验收标准**：AC-12, AC-13, AC-14, AC-15, AC-16, AC-17, AC-18, AC-19, AC-20, AC-22

- [x] T028 [P] P0 实现 control-plane 验证客户端与内部 DTO，按契约调用 `POST /internal/v1/keys/verify`
  文件：`internal/auth/service/key_auth.go`, `internal/auth/dto/internal.go`, `internal/auth/config/config.go`
  depends: T005, T013, T020@control-plane
- [x] T029 P0 实现 API Key 核心鉴权链路：Bearer 提取、格式校验、状态判断、上下文注入
  文件：`internal/auth/service/key_auth.go`, `internal/auth/middleware/api_key_auth.go`, `internal/auth/errors/errors.go`
  depends: T028
- [x] T030 [P] P0 实现 Key 状态缓存、singleflight 合并与缓存回填
  文件：`internal/auth/service/key_auth.go`, `internal/auth/config/config.go`
  depends: T028
- [ ] T031 [P] P0 订阅 `config:change` 通道，过滤 key 事件并失效 `key:*` 缓存
  文件：`internal/auth/service/cache_invalidator.go`, `cmd/auth/main.go`
  depends: T030, T024@control-plane
- [ ] T032 [P] P1 处理无部门归属、混合凭证与旧硬编码 token 废弃边界
  文件：`internal/auth/service/key_auth.go`, `internal/auth/middleware/api_key_auth.go`
  depends: T029

**✓ 检查点**：API Key 鉴权、中间件注入、缓存与失效机制可独立演示

---

## 阶段 4：集成与收尾

**目标**：路由装配、联调、测试、日志脱敏与发布门禁完成

- [ ] T040 [P] P0 集成 auth 路由装配：公开接口、内部接口、dev 路由按环境注册
  文件：`cmd/auth/main.go`, `internal/auth/handler/auth.go`, `internal/auth/handler/token.go`, `internal/auth/handler/internal.go`, `internal/auth/handler/jwks.go`
  depends: T023, T024, T025, T026, T027, T029
- [ ] T041 [P] P0 补齐单元测试：OIDC、用户 Upsert、JWT、KeyAuth、CacheInvalidator、中间件、异常映射
  文件：`internal/auth/service/oidc_test.go`, `internal/auth/service/user_test.go`, `internal/auth/service/token_test.go`, `internal/auth/service/key_auth_test.go`, `internal/auth/service/cache_invalidator_test.go`, `internal/auth/middleware/api_key_auth_test.go`, `internal/auth/middleware/jwt_auth_test.go`, `internal/auth/handler/auth_test.go`, `internal/auth/handler/token_test.go`
  depends: T022, T024, T031
- [ ] T042 [P] P0 补齐集成测试：SSO 回调、JWT 刷新、verify-key、缓存命中/失效
  文件：`internal/auth/repository/repository_integration_test.go`, `internal/auth/service/oidc_integration_test.go`, `internal/auth/service/token_integration_test.go`, `internal/auth/service/key_auth_integration_test.go`
  depends: T040, T041, T028@control-plane
- [ ] T043 [P] P1 完成日志脱敏、依赖清单与发布前检查
  文件：`internal/auth/middleware/logger.go`, `internal/auth/service/oidc.go`, `internal/auth/service/token.go`, `internal/auth/service/key_auth.go`, `xspec/changes/auth/contracts.md`, `xspec/changes/auth/design.md`
  depends: T040, T042

**✓ 检查点**：auth 模块具备上线条件，联调与测试闭环完成

---

## 交付策略

### MVP 优先（仅 P0 用户故事）
1. 完成阶段 1 + 阶段 2（基础设施与数据层）
2. 完成 SSO 登录与 JWT 登录态（T020-T024）
3. 完成 API Key 鉴权主链路（T028-T031）
4. 完成路由装配与测试（T040-T042）

### 故事间依赖
- US-SSO-01 / US-SSO-02：为 JWT 与 Key 鉴权提供用户身份基础
- US-JWT-01 / US-JWT-02：为 ops-console 员工页面和管理接口提供登录态
- US-KEY-AUTH-*：依赖 control-plane 的 verify 契约，但实现上独立于 ops-console 页面
- US-SSO-03：仅 dev 模式依赖，不阻塞主链路上线

---

## 任务统计

| 指标 | 数值 |
| :--- | :--- |
| 总任务数 | 22 |
| P0（必须） | 17 |
| P1（重要） | 5 |
| P2（可选） | 0 |
| 可并行任务 | 15 |
| 预计工时 | 28h |
