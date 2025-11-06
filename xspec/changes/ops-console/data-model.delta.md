# ops-console 数据模型变更 — CHG-20260521-002

> 基线：xspec/modules/ops-console/data-model.md (v1.0)

## 变更摘要

前端新增同步结果相关的 TypeScript 接口定义。

## 新增类型

```typescript
/** 同步结果中的模型条目 */
interface SyncModelItem {
  id: string
  owned_by: string
}

/** 同步模型接口返回结果 */
interface SyncResult {
  added: SyncModelItem[]
  existing: SyncModelItem[]
  removed: SyncModelItem[]
  total_upstream: number
  total_local: number
  unsupported: boolean
}

/** 确认注册请求体 */
interface ConfirmSyncPayload {
  models_to_add: string[]
  disable_removed?: boolean
}

/** 确认注册返回结果 */
interface ConfirmSyncResult {
  registered: number
  skipped: number
  disabled: number
}
```

## 修改

不适用

## 删除

不适用
