# control-plane 后端任务清单

## 文档信息

| 属性 | 内容 |
| :--- | :--- |
| **所属变更** | CHG-20260521-001 |
| **模块名称** | control-plane |
| **模块类型** | web-backend (added) |
| **创建时间** | 2026-05-21 |

---

## 任务格式

```
- [ ] T{NNN} [P?] {优先级} 任务描述（含具体文件路径）
  depends: T{NNN}
```

| 标记 | 含义 |
| :--- | :--- |
| `T{NNN}` | 模块内唯一编号，从 T001 起编 |
| `[P]` | 可并行：操作不同文件、无依赖未完成任务 |
| 优先级 | P0（必须）/ P1（重要）/ P2（可选） |
| `depends:` | 依赖标注（缩进换行）。无依赖则不写此行 |

---

## 阶段 1：环境与基础

**目标**：项目结构、依赖、中间件、公共工具就绪

- [x] T001 [P] P0 项目初始化：创建 `cmd/control-plane/main.go` 入口，初始化 Go module 依赖（gin, gorm, go-redis, jwt）
- [x] T002 [P] P0 Gin 路由注册与中间件骨架 `internal/controlplane/router/router.go`（JWT 认证中间件、内部 Token 中间件、日志中间件、Recovery 中间件）
- [x] T003 [P] P0 PostgreSQL 连接配置 `internal/controlplane/infra/postgres.go`（GORM 初始化、连接池配置）
- [x] T004 [P] P0 Redis 连接配置 `internal/controlplane/infra/redis.go`（go-redis 客户端初始化）
- [x] T005 [P] P0 凭证加密工具函数 `internal/controlplane/pkg/crypto/aes.go`（AES-256-GCM 加密/解密 + 脱敏展示函数）

**✓ 检查点**：项目可编译启动，GET /healthz 返回 200，加密工具单元测试通过

---

## 阶段 2：数据层

**目标**：数据库表结构、迁移脚本、GORM 模型与 Repository 就绪

- [x] T010 [P] P0 数据库迁移脚本 `migrations/001_create_providers.sql`（创建 providers 表，含 id/name/type/endpoint/encrypted_credentials/enabled/created_at/updated_at，唯一索引 providers_name_unique）
- [x] T011 [P] P0 数据库迁移脚本 `migrations/002_create_models.sql`（创建 models 表，含 id/name/provider_id(FK)/model_type/enabled/created_at/updated_at，唯一索引 models_name_unique，索引 models_provider_id_idx/models_enabled_idx）
- [x] T012 P0 GORM 模型定义 `internal/controlplane/model/provider.go`（Provider 结构体，字段映射）
  depends: T010
- [x] T013 P0 GORM 模型定义 `internal/controlplane/model/model.go`（Model 结构体，字段映射，关联 Provider）
  depends: T011
- [x] T014 P0 Provider Repository `internal/controlplane/repository/provider_repo.go`（Create/GetByID/List/Update/UpdateStatus/Delete/ExistsByName）
  depends: T012, T003
- [x] T015 P0 Model Repository `internal/controlplane/repository/model_repo.go`（Create/GetByID/List/Update/Delete/ExistsByName/CountByProviderID/ListAvailable）
  depends: T013, T003

**✓ 检查点**：迁移脚本可执行，Repository 层单元测试（mock DB）通过

---

## 阶段 3：功能实现

**目标**：按业务场景实现核心逻辑和 API，每个场景独立可测

### US-01 — 渠道配置管理 (P0)

**目标**：管理员可对渠道进行完整 CRUD 及启用/禁用操作，凭证加密存储、脱敏展示
**独立验证**：POST /api/v1/providers 创建渠道成功，GET 返回脱敏凭证，PATCH 启用/禁用生效
**对应验收标准**：创建渠道成功、凭证加密脱敏、禁用渠道级联、删除有模型渠道阻断

- [x] T020 P0 Provider Service 层 `internal/controlplane/service/provider_service.go`（CreateProvider：校验名称唯一 + 凭证加密存储）
  depends: T014, T005
- [x] T021 P0 Provider Service 层：GetProvider/ListProviders（查询时凭证脱敏返回）
  depends: T020
- [x] T022 P0 Provider Service 层：UpdateProvider（更新基本信息 + 凭证重新加密）
  depends: T020
- [x] T023 P0 Provider Service 层：ToggleProviderStatus（启用/禁用 + 级联更新下属模型 enabled 状态）
  depends: T020, T015
- [x] T024 P0 Provider Service 层：DeleteProvider（删除前校验是否存在关联模型，存在则返回 409 阻断）
  depends: T020, T015
- [x] T025 P0 Provider Handler 层 `internal/controlplane/handler/provider_handler.go`（POST /api/v1/providers：参数校验 + 调用 CreateProvider）
  depends: T020, T002
- [x] T026 P0 Provider Handler 层：GET /api/v1/providers（列表 + 分页）、GET /api/v1/providers/:id
  depends: T021, T002
- [x] T027 P0 Provider Handler 层：PUT /api/v1/providers/:id
  depends: T022, T002
- [x] T028 P0 Provider Handler 层：PATCH /api/v1/providers/:id/status
  depends: T023, T002
- [x] T029 P0 Provider Handler 层：DELETE /api/v1/providers/:id
  depends: T024, T002

**✓ 检查点**：渠道 CRUD 接口可独立调用，凭证脱敏正确，级联禁用与删除阻断逻辑正确

### US-02 — 模型注册管理 (P0)

**目标**：管理员可对模型进行完整 CRUD，模型名全局唯一，可按渠道筛选
**独立验证**：POST /api/v1/models 创建模型成功并关联渠道，GET 列表可按 provider_id 过滤
**对应验收标准**：创建模型关联渠道、模型名唯一

- [x] T030 P0 Model Service 层 `internal/controlplane/service/model_service.go`（CreateModel：校验模型名唯一 + 校验关联渠道存在且启用）
  depends: T015, T014
- [x] T031 P0 Model Service 层：ListModels（支持按 provider_id 筛选）、GetModel
  depends: T030
- [x] T032 P0 Model Service 层：UpdateModel（更新时重新校验模型名唯一性）
  depends: T030
- [x] T033 P0 Model Service 层：DeleteModel
  depends: T030
- [x] T034 P0 Model Handler 层 `internal/controlplane/handler/model_handler.go`（POST /api/v1/models：参数校验 + 调用 CreateModel）
  depends: T030, T002
- [x] T035 P0 Model Handler 层：GET /api/v1/models（列表 + 分页 + provider_id 过滤）
  depends: T031, T002
- [x] T036 P0 Model Handler 层：PUT /api/v1/models/:id
  depends: T032, T002
- [x] T037 P0 Model Handler 层：DELETE /api/v1/models/:id
  depends: T033, T002

**✓ 检查点**：模型 CRUD 接口可独立调用，模型名唯一性约束生效，关联渠道校验正确

### US-03 — 配置变更通知 (P0)

**目标**：渠道或模型发生变更时通过 Redis pub/sub 通知 gateway-api 刷新配置
**独立验证**：创建/更新/删除渠道或模型后，Redis channel `config:change` 收到变更事件
**对应验收标准**：gateway-api 能感知配置变更

- [x] T040 P0 变更事件结构定义 `internal/controlplane/event/change_event.go`（事件类型枚举：provider_created/updated/disabled/enabled/deleted, model_created/updated/deleted；事件体含资源类型+ID+时间戳）
  depends: T004
- [x] T041 P0 Redis 发布器 `internal/controlplane/event/publisher.go`（Publish 方法：序列化事件 JSON → PUBLISH 到 config:change channel）
  depends: T040
- [x] T042 P0 在 Provider Service 中集成事件发布（创建/更新/启用禁用/删除操作后调用 Publish）
  depends: T041, T020
- [x] T043 P0 在 Model Service 中集成事件发布（创建/更新/删除操作后调用 Publish）
  depends: T041, T030

**✓ 检查点**：变更操作后 Redis SUBSCRIBE config:change 可收到正确格式的事件消息

### US-04 — 内部接口 (P0)

**目标**：gateway-api 可通过内部接口获取当前可用模型列表
**独立验证**：携带内部 Token 调用 GET /internal/v1/models/available 返回启用的模型列表（含渠道信息）
**对应验收标准**：gateway-api 可正确消费可用模型列表

- [x] T044 P0 内部 Token 认证中间件 `internal/controlplane/middleware/internal_auth.go`（校验请求头中的内部 Token）
  depends: T002
- [x] T045 P0 内部接口 Service 层 `internal/controlplane/service/internal_service.go`（ListAvailableModels：查询 enabled=true 的模型，JOIN 渠道信息，返回含 provider endpoint/type 的完整模型列表）
  depends: T015, T014
- [x] T046 P0 内部接口 Handler 层 `internal/controlplane/handler/internal_handler.go`（GET /internal/v1/models/available：调用 ListAvailableModels 返回结果）
  depends: T045, T044

**✓ 检查点**：内部 Token 认证有效，接口返回完整可用模型列表（含渠道 endpoint 和凭证解密信息）

---

## 阶段 4：集成与收尾

**目标**：集成测试、安全加固、迁移验证

- [x] T050 P0 集成测试 `tests/integration/provider_test.go`（覆盖渠道 CRUD 全流程 + 级联 + 阻断）
  depends: T029, T028
- [x] T051 P0 集成测试 `tests/integration/model_test.go`（覆盖模型 CRUD 全流程 + 唯一性 + 关联校验）
  depends: T037
- [x] T052 P0 集成测试 `tests/integration/internal_test.go`（内部接口 + Token 认证 + 返回数据完整性）
  depends: T046
- [x] T053 [P] P1 安全加固：SQL 注入防护验证（确认所有查询均使用参数化）、凭证不泄漏验证（确认日志和响应中无明文凭证）
  depends: T029, T037
- [x] T054 [P] P1 迁移脚本端到端验证 `tests/migration_test.go`（空库执行全部迁移 → 验证表结构和索引正确）
  depends: T010, T011
- [x] T055 [P] P2 日志规范化（统一请求日志格式、敏感信息过滤）
  depends: T002

**✓ 检查点**：全部集成测试通过，凭证无泄漏，迁移脚本可重复执行

---

## 交付策略

### MVP 优先（仅 P0 场景）
1. 完成阶段 1（T001–T005）→ 项目可启动
2. 完成阶段 2（T010–T015）→ 数据层就绪
3. 完成 US-01（T020–T029）→ 渠道管理可用
4. 完成 US-02（T030–T037）→ 模型管理可用
5. 完成 US-03（T040–T043）→ 变更通知可用
6. 完成 US-04（T044–T046）→ 内部接口可用
7. 完成阶段 4（T050–T052）→ 集成测试通过

### 场景间依赖
- US-01: 无场景间依赖（可独立交付）
- US-02: 依赖 US-01 的 Provider Repository（需渠道存在才能关联模型）
- US-03: 依赖 US-01 和 US-02 的 Service 层（在业务操作中嵌入事件发布）
- US-04: 依赖 US-02 的 Model Repository（查询可用模型列表）

### 并行建议
- 阶段 1 全部任务可并行执行（操作不同文件）
- 阶段 2 中 T010/T011 可并行，T014/T015 可并行
- 阶段 3 中 US-01 和 US-02 的 Service 层可并行开发（Repository 就绪后）
- US-03 和 US-04 可并行（分别依赖不同的 Service）

---

## 任务统计

| 指标 | 数值 |
| :--- | :--- |
| 总任务数 | 40 |
| P0（必须） | 37 |
| P1（重要） | 2 |
| P2（可选） | 1 |
| 可并行任务 | 12 |
| 预计工时 | 32h |
