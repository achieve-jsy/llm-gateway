export interface SyncModelItem {
  id: string
  owned_by: string
}

export interface SyncResult {
  added: SyncModelItem[]
  existing: SyncModelItem[]
  removed: SyncModelItem[]
  total_upstream: number
  total_local: number
  unsupported: boolean
}

export interface ConfirmSyncPayload {
  models_to_add: string[]
  disable_removed?: boolean
}

export interface ConfirmSyncResult {
  registered: number
  skipped: number
  disabled: number
}
