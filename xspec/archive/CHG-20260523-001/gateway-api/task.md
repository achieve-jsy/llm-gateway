# gateway-api 后端任务清单

## 文档信息

| 属性 | 内容 |
| :--- | :--- |
| **所属变更** | CHG-20260523-001 |
| **模块名称** | gateway-api |
| **模块类型** | web-backend |
| **创建时间** | 2026-05-23 |

---

## 阶段 1：环境与基础

**目标**：auth 依赖配置、中间件入口、路由与鉴权基础完成

- [x] T001 [P] P0 增加 auth 服务地址、超时、JWKS 配置读取与注入 `internal/gateway/config/config.go`, `cmd/gateway/main.go`
- [x] T002 P0 路由与中间件链改造，用 `AuthMiddleware` 替换旧 `APIKeyAuth`，补 `GET /v1/me/models` 路由 `internal/gateway/server/router.go`, `internal/gateway/server/router_test.go`
  depends: T001
- [x] T003 P0 实现统一鉴权中间件骨架，完成 Bearer 提取、`sk-`/JWT 分流、Context 注入入口 `internal/gateway/middleware/auth.go`, `internal/gateway/middleware/middleware_test.go`
  depends: T001
- [x] T004 [P] P0 实现 auth 校验客户端 `POST /internal/v1/auth/verify-key`，含 500ms 超时与无重试策略 `internal/gateway/infra/auth_client.go`, `internal/gateway/infra/auth_client_test.go`
  depends: T001, T020@auth
- [x] T005 [P] P0 实现 JWT 验签支持（JWKS 拉取、缓存、过期校验） `internal/gateway/middleware/auth.go`, `internal/gateway/middleware/middleware_test.go`
  depends: T001, T026@auth

**✓ 检查点**：中间件链切换到 AuthMiddleware，API Key / JWT 两条认证路径均可独立验证

---

## 阶段 2：数据层

**目标**：运行时身份上下文和用户模型结构就绪

- [x] T010 [P] P0 定义 `UserIdentity` 与 Context 读取约定 `internal/gateway/model/identity.go`, `internal/gateway/model/context.go`
- [x] T011 [P] P0 定义 `UserModel` / `MeModelsResponse` 等响应结构 `internal/gateway/model/identity.go`, `internal/gateway/model/chat.go`
- [x] T012 P0 扩展 Registry/Provider 查询元数据能力，支持 `owned_by/type/enabled` 和按身份过滤 `internal/gateway/provider/registry.go`, `internal/gateway/provider/registry_test.go`, `internal/gateway/model/provider.go`
  depends: T011
- [x] T013 P0 统一鉴权与模型错误码映射 `internal/gateway/model/error.go`, `internal/gateway/errormap/mapper.go`, `internal/gateway/middleware/middleware.go`
  depends: T003

**✓ 检查点**：`UserIdentity`、`UserModel`、错误码与 registry 扩展可编译可测试

---

## 阶段 3：功能实现

### US-AUTH-01 — API Key / JWT 鉴权链路 (P0)

**目标**：统一入口可识别 API Key 和员工 JWT，并注入身份上下文
**独立验证**：`/v1/chat/completions` 携带有效 `sk-` Key 可通过；无 Key/无效 Key/禁用 Key 被拒绝；`/v1/me/models` 携带员工 JWT 可通过
**对应验收标准**：AC-AUTH-01, AC-AUTH-02, AC-AUTH-03

- [x] T020 P0 实现 API Key 鉴权链路：格式校验、verify-key 调用、`user_id/key_id/department_id` 注入 `internal/gateway/middleware/auth.go`, `internal/gateway/infra/auth_client.go`
  depends: T003, T004, T010, T020@auth
- [x] T021 [P] P0 实现员工 JWT 鉴权链路：签名/过期校验、`user_id/department_id` 注入 `internal/gateway/middleware/auth.go`, `internal/gateway/middleware/middleware_test.go`
  depends: T003, T005, T010, T024@auth
- [x] T022 P0 让聊天业务链路消费身份上下文而非旧简单 token 放行 `internal/gateway/handler/chat.go`, `internal/gateway/orchestrator/chat.go`, `internal/gateway/server/router_test.go`
  depends: T020, T013
- [x] T023 P0 收口鉴权错误语义：缺失 Key、无效 Key、禁用 Key、auth 不可达、无效 JWT `internal/gateway/middleware/auth.go`, `internal/gateway/model/error.go`, `internal/gateway/server/router_test.go`
  depends: T020, T021, T013

**✓ 检查点**：统一鉴权链路可独立验证，错误码语义稳定

### US-MODELS-01 — 当前身份可用模型查询 (P0)

**目标**：`/v1/models` 与 `/v1/me/models` 基于身份返回一致的可用模型范围
**独立验证**：携带员工 JWT 调用 `/v1/me/models` 返回 OpenAI 风格 list；无可用模型返回 `200 + []`
**对应验收标准**：AC-MODELS-01, AC-MODELS-02

- [x] T024 [P] P0 改造 `/v1/models` 为按身份过滤可用模型 `internal/gateway/handler/models.go`, `internal/gateway/provider/registry.go`, `internal/gateway/handler/models_test.go`
  depends: T012, T020, T021
- [x] T025 [P] P0 实现 `GET /v1/me/models` Handler 与路由 `internal/gateway/handler/me_handler.go`, `internal/gateway/handler/me_handler_test.go`, `internal/gateway/server/router.go`
  depends: T011, T021, T012
- [x] T026 P0 在编排层统一"模型是否可用"判断，确保模型调用与列表结果一致 `internal/gateway/orchestrator/chat.go`, `internal/gateway/orchestrator/chat_test.go`
  depends: T024, T025
- [x] T027 [P] P1 补齐空列表与响应兼容处理 `internal/gateway/handler/models.go`, `internal/gateway/handler/me_handler.go`, `internal/gateway/handler/models_test.go`, `internal/gateway/handler/me_handler_test.go`
  depends: T024, T025
- [ ] T027 [P] P1 补齐空列表与响应兼容处理 `internal/gateway/handler/models.go`, `internal/gateway/handler/me_handler.go`, `internal/gateway/handler/models_test.go`, `internal/gateway/handler/me_handler_test.go`
  depends: T024, T025

**✓ 检查点**：`/v1/models` 与 `/v1/me/models` 结果语义一致，空列表返回 200

---

## 阶段 4：集成与收尾

**目标**：完成路由、中间件、联调与集成测试

- [ ] T040 [P] P0 路由与中间件集成测试，覆盖 `/v1/chat/completions`、`/v1/models`、`/v1/me/models` 的未认证/已认证/豁免路径 `internal/gateway/server/router_test.go`, `internal/gateway/middleware/middleware_test.go`
  depends: T022, T023, T027
- [ ] T041 [P] P1 auth 联调测试：verify-key 成功/禁用/超时、JWT 路径 `tests/integration/gateway_auth_test.go`
  depends: T023, T020@auth, T026@auth
- [ ] T042 [P] P1 模型权限联调与前端对接验证 `tests/integration/gateway_models_test.go`, `web/src/services/gateway-api.ts`, `web/src/services/gateway-api.test.ts`
  depends: T025, T027, T014@ops-console

**✓ 检查点**：受保护接口认证、授权、错误码和前端消费契约全部通过

---

## 任务统计

| 指标 | 数值 |
| :--- | :--- |
| 总任务数 | 15 |
| P0（必须） | 12 |
| P1（重要） | 3 |
| P2（可选） | 0 |
| 可并行任务 | 9 |
| 预计工时 | 18h |
