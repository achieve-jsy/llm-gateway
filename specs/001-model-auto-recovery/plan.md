# Implementation Plan: 模型自动恢复 (Model Auto Recovery)

**Branch**: `001-model-auto-recovery` | **Date**: 2026-06-10 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/001-model-auto-recovery/spec.md`

## Summary

实现模型自动恢复机制：当模型因连续健康检测失败被系统自动禁用后，系统继续探测该模型的可用性，并在模型恢复正常后自动重新启用。同时提供模型状态变更日志看板，记录所有自动/手动的启用/禁用操作及原因。

**核心技术方案**：
1. 参考现有 Provider 的 `Status` 字段设计，为 Model 实体新增 `Status` 字段区分"手动禁用"和"自动禁用"
2. 修改健康检测逻辑，对"自动禁用"状态的模型继续探测
3. 新增模型状态变更日志表和 API/UI

## Technical Context

**Language/Version**: Go 1.26 + Gin (后端) / Vue 3 + Element Plus + Vite + Tailwind (前端)

**Primary Dependencies**:
- GORM (ORM)
- Redis (配置同步、缓存)
- PostgreSQL (控制面持久化)

**Storage**: PostgreSQL（新增 `model_status_logs` 表，修改 `models` 表）

**Testing**: Go test (tests/ 目录) + Vitest (前端单元)

**Target Platform**: Linux server (Docker Compose 部署)

**Project Type**: 双服务架构 (gateway + control-plane)，本功能仅涉及 control-plane

**Performance Goals**: 健康检测不阻塞主流程，状态变更日志查询 P95 < 200ms

**Constraints**: 
- 不破坏现有 API 兼容性
- 配置变更支持热加载
- 遵循 gateway 不直连数据库的架构约束

**Scale/Scope**: 预计支持数百个模型的健康检测和状态追踪

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 评估 | 说明 |
|------|------|------|
| **原则一：不开倒车** | ✅ PASS | 新增功能不修改现有接口行为；需补充测试用例覆盖自动恢复流程 |
| **原则二：服务稳定** | ✅ PASS | 对外 `/v1/*` 接口无变化；新增 `Status` 字段向后兼容（默认值=1） |
| **原则三：服务快速** | ✅ PASS | 健康检测已有并发限制（sem=10）；状态日志写入异步不阻塞主流程 |
| **原则四：架构清晰** | ✅ PASS | 仅修改 control-plane；gateway 通过 Redis snapshot 感知状态变化 |
| **原则五：交互至上** | ✅ PASS | 状态变更日志看板提供即时反馈和可操作信息 |
| **开发阶段特别说明** | ⚠️ 注意 | 开发环节不更改 `deploy/` 和 `config/` 文件；测试用例按项目阶段决定是否编写 |

## Project Structure

### Documentation (this feature)

```text
specs/001-model-auto-recovery/
├── plan.md              # This file
├── research.md          # Phase 0 output - 技术决策研究
├── data-model.md        # Phase 1 output - 数据模型设计
├── quickstart.md        # Phase 1 output - 验证指南
├── contracts/           # Phase 1 output - API 契约
│   ├── model-status-log-api.md
│   └── health-check-config-api.md
└── tasks.md             # Phase 2 output (NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
# 后端 (control-plane)
internal/controlplane/
├── model/
│   ├── model.go              # [MODIFY] 新增 Status 字段
│   └── model_status_log.go   # [NEW] 状态变更日志实体
├── repository/
│   ├── model_repo.go         # [MODIFY] 新增 AutoEnableModel, 修改 AutoDisableModel
│   └── model_status_log_repo.go  # [NEW] 日志 CRUD
├── service/
│   ├── health_check_service.go   # [MODIFY] 恢复检测逻辑
│   ├── model_service.go          # [MODIFY] 状态变更记录日志
│   └── model_status_log_service.go # [NEW] 日志查询服务
├── handler/
│   ├── model_handler.go          # [MODIFY] 批量状态变更记录日志
│   └── model_status_log_handler.go # [NEW] 日志查询 API
└── router/
    └── router.go             # [MODIFY] 注册新 API 路由

# 前端 (web)
web/src/
├── views/admin/
│   ├── ModelListPage.vue         # [MODIFY] 添加状态历史入口
│   └── ModelStatusLogPage.vue    # [NEW] 状态变更日志看板
├── api/
│   └── model.ts              # [MODIFY] 新增状态日志 API
└── router/
    └── index.ts              # [MODIFY] 注册新路由

# 数据库迁移
atlas-migrations/
└── 20260610XXXXXX_add_model_status.sql  # [NEW] 新增字段和表

tests/
└── contract/
    └── model_status_log_test.go  # [NEW] 契约测试（按阶段决定是否编写）
```

**Structure Decision**: 遵循现有 control-plane 的分层结构（model → repository → service → handler），参考 Provider 的 Status 字段设计模式（status=1/2/3 区分启用/手动禁用/自动禁用）。

## Complexity Tracking

> 无宪章违规，无需记录。
