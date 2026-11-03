export interface ModelCatalog {
  id: string
  source: string
  model_id: string
  display_name: string
  description: string
  model_type: string
  context_length: number
  max_output_tokens: number
  input_price: number
  output_price: number
  created_at: string
  updated_at: string
}

export interface PriceDiffEntry {
  catalog_id: string
  model_id: string
  field: string
  old_value: number
  new_value: number
}

export interface SyncTriggerResponse {
  triggered: boolean
  message: string
}

export interface ApplyDiffResponse {
  applied: number
  skipped: number
}

export interface CreateFromCatalogPayload {
  catalog_id: string
  provider_id: string
  name?: string
}

export type ResettableOverrideField = 'context_length' | 'max_output_tokens'

export interface AutoBindResult {
  bound: number
  skipped: number
}

export interface BindCatalogPayload {
  catalog_id: string
  override_fields?: ResettableOverrideField[]
}

export interface BatchBindItem {
  model_id: string
  catalog_id: string
}

export interface BatchBindResult {
  bound: number
  failed: number
}

export interface CatalogBindingStats {
  total: number
  bound: number
  unbound: number
}

export interface CatalogSyncStatus {
  last_sync_at: string
  status: string
  message: string
}
