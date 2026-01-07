# 工作流任务总览

## 并行关系图

```
Task-1 (数据模型+迁移)
  │
  ├──→ Task-2 (控制面授权服务) ──→ Task-4 (前端)
  │         │
  │         └──→ Task-3 (网关鉴权改造)
  │
  └──→ Task-5 (价格换算CNY) ← 完全独立，可立即并行
```

## 任务列表

| # | 任务 | 依赖 | 可并行 | 工作量 |
|---|------|------|--------|--------|
| 1 | [数据模型 + 迁移](task-1-data-model/TASK.md) | 无 | 立即开始 | S |
| 2 | [控制面授权服务](task-2-entitlement-service/TASK.md) | Task-1 | Task-1 完成后 | L |
| 3 | [网关鉴权改造](task-3-gateway-auth/TASK.md) | Task-2 DTO | 接口已定义可并行 | M |
| 4 | [前端授权管理](task-4-frontend/TASK.md) | Task-2 API | 接口已定义可并行 | M |
| 5 | [价格换算人民币](task-5-pricing-cny/TASK.md) | 无 | 立即开始 | M |

## 推荐执行策略

**Wave 1（立即并行）**：
- Task-1：数据模型 — 最先完成（≤30min）
- Task-5：价格换算 — 完全独立，无依赖

**Wave 2（Task-1 完成后并行）**：
- Task-2：控制面服务 — 核心逻辑
- Task-3：网关改造 — 接口契约已定义，可按契约先行开发
- Task-4：前端 — 接口契约已定义，可 mock 先行开发

## 每个任务的委派方式

```bash
# Task 1 - 数据模型（快速任务，走 atlas 迁移 skill）
task(category="quick", load_skills=["atlas-db-migration"], ...)

# Task 2 - 控制面授权服务（核心后端逻辑）
task(category="deep", load_skills=[], ...)

# Task 3 - 网关鉴权改造（后端改造）
task(category="unspecified-high", load_skills=[], ...)

# Task 4 - 前端授权管理（UI 开发）
task(category="visual-engineering", load_skills=["frontend-ui-ux-engineer"], ...)

# Task 5 - 价格换算（前端 + 少量后端配置）
task(category="visual-engineering", load_skills=["frontend-ui-ux-engineer"], ...)
```

## 集成验证（所有任务完成后）

1. 控制面→网关全链路：设 controlled → 授权 → verify-key 含 usable_groups → 网关放行/拒绝
2. 前端完整流程：设开放模式 → 授权管理 → 可见列表刷新
3. 价格展示：CNY 切换正确换算
4. 存量兼容：旧 Key 行为不变
