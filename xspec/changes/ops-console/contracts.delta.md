# ops-console 接口契约变更 — CHG-20260521-002

> 基线：xspec/modules/ops-console/contracts.md (v1.0)

## 变更摘要

前端新增 2 个 API 调用方法，对应 control-plane 的同步模型接口。

## 新增接口调用

### syncModels(providerId: string)

- 请求：POST /api/v1/providers/:id/sync-models
- 响应类型：

```typescript
interface SyncResult {
  added: SyncModelItem[]
  existing: SyncModelItem[]
  removed: SyncModelItem[]
  total_upstream: number
  total_local: number
  unsupported: boolean
}

interface SyncModelItem {
  id: string
  owned_by: string
}
```

### confirmSync(providerId: string, payload)

- 请求：POST /api/v1/providers/:id/sync-models/confirm
- 请求体：

```typescript
interface ConfirmSyncPayload {
  models_to_add: string[]
  disable_removed?: boolean  // 默认 true
}
```

- 响应类型：

```typescript
interface ConfirmSyncResult {
  registered: number
  skipped: number
  disabled: number
}
```

## 错误处理

| HTTP 状态 | 前端处理 |
| :--- | :--- |
| 400 (sync_not_supported) | 弹窗显示"该渠道类型不支持自动同步" |
| 400 (provider_disabled) | ElMessage.warning("请先启用渠道") |
| 502 (upstream_error) | ElMessage.error("厂商接口调用失败：" + message) |
