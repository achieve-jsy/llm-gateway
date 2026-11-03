export interface SyncResultModel {
  id: string
  owned_by: string
}

export interface SyncResult {
  added: SyncResultModel[]
  existing: SyncResultModel[]
  removed: SyncResultModel[]
  total_upstream: number
  total_local: number
  unsupported: boolean
}

export interface ConfirmSyncPayload {
  model_ids: string[]
}

export interface ConfirmSyncResult {
  synced: number
}

export interface CheckModelsPayload {
  model_ids?: string[]
}

export interface CheckModelsResult {
  total: number
  success: number
  failed: number
  results: Array<{ model_id: string; success: boolean; error?: string }>
}
