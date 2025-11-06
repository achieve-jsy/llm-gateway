# control-plane 任务清单 — CHG-20260521-002

## 文档信息

| 属性 | 内容 |
| :--- | :--- |
| **所属变更** | CHG-20260521-002 |
| **模块名称** | control-plane |
| **模块类型** | web-backend (modified) |
| **创建时间** | 2026-05-21 |

---

## 阶段 1：接口与 Service 实现

**目标**：同步模型的后端接口完整可用

- [x] T001 P0 SyncService 实现 `internal/controlplane/service/sync_service.go`（SyncModels：解密凭证→调厂商 GET /models→对比本地→分类返回）
- [x] T002 P0 SyncService 确认注册 `internal/controlplane/service/sync_service.go`（ConfirmSync：批量创建 enabled=false + 禁用 removed）
  depends: T001
- [x] T003 P0 SyncHandler 实现 `internal/controlplane/handler/sync_handler.go`（POST /providers/:id/sync-models + POST /providers/:id/sync-models/confirm）
  depends: T001, T002
- [x] T004 P0 路由注册 `internal/controlplane/router/router.go`（新增 2 个端点到 adminGroup）
  depends: T003

**✓ 检查点**：curl 调用 sync-models 接口返回分类结果；confirm 接口批量写入成功

---

## 阶段 2：边界处理

**目标**：所有边界场景正确处理

- [x] T005 P0 不支持同步的渠道类型处理（Anthropic/Bedrock adapter 无 /models → 返回 400 sync_not_supported）
  depends: T001
- [x] T006 P0 空列表防误禁用（上游返回空 → 不执行禁用，返回提示）
  depends: T002
- [x] T007 P1 渠道禁用状态校验（禁用渠道不允许同步 → 返回 400）
  depends: T003

**✓ 检查点**：Anthropic 渠道同步返回 unsupported；空列表不触发禁用

---

## 阶段 3：集成测试

- [x] T008 P1 集成测试 `tests/integration/sync_test.go`（覆盖：正常同步、重复跳过、下架禁用、空列表、不支持类型）
  depends: T004, T005, T006

**✓ 检查点**：全部测试通过

---

## 交付策略

| 项目 | 说明 |
| :--- | :--- |
| 总任务数 | 8 |
| P0 | 6 |
| P1 | 2 |
| 预计工时 | 4h |
