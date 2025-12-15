# Tasks: 模型自动恢复 (Model Auto Recovery)

**Input**: Design documents from `/specs/001-model-auto-recovery/`

**Prerequisites**: plan.md ✅, spec.md ✅, research.md ✅, data-model.md ✅, contracts/ ✅

**Tests**: 根据项目宪章"开发阶段特别说明"，本阶段**不编写测试用例**，只关注业务代码。

**Organization**: 任务按 User Story 组织，支持独立实现和测试。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行执行（不同文件，无依赖）
- **[Story]**: 所属 User Story（US1 = 自动恢复，US2 = 日志看板）
- 描述中包含精确的文件路径

## Path Conventions

- **后端**: `internal/controlplane/` (model → repository → service → handler)
- **前端**: `web/src/` (views/admin, api, router)
- **迁移**: `atlas-migrations/`

---

## Phase 1: Setup (数据库迁移)

**Purpose**: 数据模型基础变更，为后续功能实现做准备

- [ ] T001 创建数据库迁移文件 `atlas-migrations/20260610XXXXXX_add_model_status.sql`，包含：
  - Model 表新增 `status` 和 `consecutive_recovery_successes` 字段
  - 数据修复：`UPDATE models SET status = 2 WHERE enabled = false`
  - 新建 `model_status_logs` 表及索引
  - HealthCheckConfig 表新增 `model_recovery_enabled` 和 `model_recovery_success_threshold` 字段

- [ ] T002 [P] 修改 `internal/controlplane/model/model.go`，新增 `Status` 和 `ConsecutiveRecoverySuccesses` 字段及状态常量

- [ ] T003 [P] 创建 `internal/controlplane/model/model_status_log.go`，定义 ModelStatusLog 实体和 ChangeType 常量

- [ ] T004 [P] 修改 `internal/controlplane/model/health_check.go`，新增 `ModelRecoveryEnabled` 和 `ModelRecoverySuccessThreshold` 字段

**Checkpoint**: 数据模型定义完成，迁移文件就绪

---

## Phase 2: Foundational (基础设施)

**Purpose**: User Story 实现的前置依赖（Repository 层和路由注册）

**⚠️ CRITICAL**: User Story 阶段必须等待本阶段完成

- [ ] T005 修改 `internal/controlplane/repository/model_repo.go`，新增以下方法：
  - `AutoDisableModel(id, reason, errorMsg)` - 修改现有方法，设置 `status=3` 并记录日志
  - `AutoEnableModel(id)` - 新增，设置 `status=1`, `enabled=true`，重置计数器
  - `IncrementRecoverySuccesses(id)` - 新增，递增恢复成功计数
  - `ResetRecoverySuccesses(id)` - 新增，重置恢复成功计数
  - `GetAutoDisabledModels()` - 新增，查询 `status=3` 的模型列表

- [ ] T006 [P] 创建 `internal/controlplane/repository/model_status_log_repo.go`，实现：
  - `Create(log *ModelStatusLog)` - 创建日志记录
  - `ListByModelID(modelID, limit)` - 按模型查询日志
  - `ListWithFilter(filter, page, pageSize)` - 带筛选的分页查询

- [ ] T007 修改 `internal/controlplane/router/router.go`，预留日志 API 路由位置（实际注册在 US2 阶段）

**Checkpoint**: Repository 层就绪，可开始 User Story 实现

---

## Phase 3: User Story 1 - 被禁用模型自动恢复 (Priority: P1) 🎯 MVP

**Goal**: 模型因连续健康检测失败被自动禁用后，系统继续探测该模型，恢复正常后自动重新启用

**Independent Test**: 模拟模型故障 → 自动禁用 → 模型恢复 → 自动启用的完整流程

### Implementation for User Story 1

- [ ] T008 [US1] 修改 `internal/controlplane/service/health_check_service.go` 中的 `runBackgroundModelCheck` 方法：
  - 修改过滤逻辑：`status=3` 的模型继续参与检测，`status=2` 的模型跳过
  - 读取配置 `ModelRecoveryEnabled` 和 `ModelRecoverySuccessThreshold`

- [ ] T009 [US1] 在 `internal/controlplane/service/health_check_service.go` 中实现恢复检测逻辑：
  - 对 `status=3` 的模型，检测成功时调用 `IncrementRecoverySuccesses`
  - 检测失败时调用 `ResetRecoverySuccesses`
  - 连续成功达阈值时调用 `AutoEnableModel` 并记录恢复日志

- [ ] T010 [US1] 修改 `internal/controlplane/service/health_check_service.go` 中的自动禁用逻辑：
  - 调用修改后的 `AutoDisableModel`，传入错误信息
  - 确保设置 `status=3` 而非仅设置 `enabled=false`

- [ ] T011 [US1] 修改 `internal/controlplane/service/model_service.go` 中的 `BatchUpdateModelStatus` 方法：
  - 手动禁用时设置 `status=2`
  - 手动启用时设置 `status=1`
  - 记录状态变更日志（manual_disabled / manual_enabled）

- [ ] T012 [US1] 修改 `internal/controlplane/handler/model_handler.go` 中的 `BatchUpdateModelStatus` 处理：
  - 从请求上下文获取操作人信息（user_id, user_name）
  - 传递给 service 层用于日志记录

- [ ] T013 [US1] 修改 `internal/controlplane/handler/health_handler.go`（如有）或在设置 handler 中：
  - 更新健康检测配置 API，支持新增的 `model_recovery_enabled` 和 `model_recovery_success_threshold` 字段

**Checkpoint**: 自动恢复核心逻辑完成，可通过 API 验证

---

## Phase 4: User Story 2 - 模型状态变更日志看板 (Priority: P2)

**Goal**: 运维人员可在管理界面查看模型状态变更的完整历史

**Independent Test**: 触发模型状态变更后，检查日志看板的展示

### Implementation for User Story 2 - Backend

- [ ] T014 [US2] 创建 `internal/controlplane/service/model_status_log_service.go`，实现：
  - `ListLogs(filter, page, pageSize)` - 带筛选的日志列表
  - `GetLogsByModelID(modelID, limit)` - 单个模型的日志历史

- [ ] T015 [US2] 创建 `internal/controlplane/handler/model_status_log_handler.go`，实现：
  - `GET /api/v1/model-status-logs` - 日志列表 API
  - `GET /api/v1/models/:id/status-logs` - 单个模型日志 API

- [ ] T016 [US2] 修改 `internal/controlplane/router/router.go`，注册日志 API 路由：
  - `adminGroup.GET("/model-status-logs", modelStatusLogHandler.List)`
  - `adminGroup.GET("/models/:id/status-logs", modelStatusLogHandler.GetByModelID)`

### Implementation for User Story 2 - Frontend

- [ ] T017 [P] [US2] 修改 `web/src/api/model.ts`，新增日志 API 调用方法：
  - `getModelStatusLogs(params)` - 获取日志列表
  - `getModelStatusLogsByID(modelId, limit)` - 获取单个模型日志

- [ ] T018 [P] [US2] 创建 `web/src/views/admin/ModelStatusLogPage.vue`，实现：
  - 日志列表表格（时间、模型、渠道、变更类型、原因、操作人）
  - 筛选器（模型、渠道、变更类型、时间范围）
  - 变更类型使用颜色标签（auto_disabled=红色, auto_recovered=绿色, manual_disabled=橙色, manual_enabled=蓝色）
  - 分页支持

- [ ] T019 [US2] 修改 `web/src/views/admin/ModelListPage.vue`：
  - 在操作列添加"状态历史"按钮
  - 点击后打开抽屉或跳转到筛选后的日志页面

- [ ] T020 [US2] 修改 `web/src/router/index.ts`，注册日志页面路由：
  - `/admin/model-status-logs` → ModelStatusLogPage

- [ ] T021 [US2] 修改健康检测设置对话框（在 ModelListPage.vue 或独立组件中）：
  - 新增"模型恢复"配置区域
  - 启用自动恢复开关
  - 恢复阈值输入框（1-10）

**Checkpoint**: 日志看板功能完成，可通过界面查看状态变更历史

---

## Phase 5: Polish & Cross-Cutting Concerns

**Purpose**: 代码清理、文档更新、端到端验证

- [ ] T022 按 quickstart.md 执行端到端验证：
  - 场景 1: 模型自动禁用流程
  - 场景 2: 模型自动恢复流程
  - 场景 3: 手动禁用不触发自动恢复
  - 场景 4: 状态变更日志看板

- [ ] T023 [P] 代码审查和清理：
  - 确保新增代码符合现有代码风格
  - 检查错误处理完整性
  - 确保日志记录规范

- [ ] T024 [P] 更新 API 文档（如有 Swagger 注解）：
  - 新增日志 API 的文档注解
  - 更新健康检测配置 API 的文档

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Setup)**: 无依赖 - 可立即开始
- **Phase 2 (Foundational)**: 依赖 Phase 1 完成 - **阻塞所有 User Story**
- **Phase 3 (US1)**: 依赖 Phase 2 完成
- **Phase 4 (US2)**: 依赖 Phase 2 完成，与 US1 **可并行**（但建议先完成 US1）
- **Phase 5 (Polish)**: 依赖 US1 和 US2 完成

### User Story Dependencies

- **User Story 1 (P1)**: 核心功能，建议优先完成
- **User Story 2 (P2)**: 依赖 US1 中的状态日志记录逻辑，建议在 US1 之后

### Within Each Phase

- Setup: T002/T003/T004 可并行
- Foundational: T005 需先于 T006（日志 repo 依赖 model repo 的日志记录调用模式）
- US1: T008 → T009 → T010 按顺序；T011/T012/T013 可在 T010 后并行
- US2: T014 → T015 → T016 按顺序；T017/T018 可并行，T019/T020/T021 在 T18 后

### Parallel Opportunities

- T002, T003, T004 可同时执行（不同文件）
- T006 与 T005 可并行（但 T006 可能需要了解 T005 的接口）
- T017, T018 可同时执行（前端不同文件）

---

## Parallel Example: Phase 1 Setup

```bash
# 并行执行 Model 层变更：
Task T002: "修改 internal/controlplane/model/model.go"
Task T003: "创建 internal/controlplane/model/model_status_log.go"
Task T004: "修改 internal/controlplane/model/health_check.go"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. 完成 Phase 1: Setup（数据库迁移和模型定义）
2. 完成 Phase 2: Foundational（Repository 层）
3. 完成 Phase 3: User Story 1（自动恢复核心逻辑）
4. **STOP and VALIDATE**: 通过 API 验证自动恢复功能
5. 可部署 MVP 版本

### Incremental Delivery

1. Setup + Foundational → 基础就绪
2. User Story 1 → 核心功能可用 → **MVP 可交付**
3. User Story 2 → 可观测性增强 → 完整功能交付
4. Polish → 代码质量和文档完善

---

## Notes

- [P] 任务 = 不同文件，无依赖
- [Story] 标签用于追溯任务所属 User Story
- 根据宪章，本阶段**不编写测试用例**
- 每个任务或逻辑组完成后提交
- 在任何 Checkpoint 处可暂停验证功能独立性
- 避免：模糊任务描述、同文件冲突、跨 Story 强依赖
