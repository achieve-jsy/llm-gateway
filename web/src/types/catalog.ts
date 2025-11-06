/**
 * 模型 Catalog 自动拉取相关类型（对应 /api/v1/model-catalog/*）。
 *
 * 后端契约：
 *   GET  /api/v1/model-catalog/search?q=&provider=&mode=&limit=20
 *   GET  /api/v1/model-catalog/by-source?source_model_id=xxx
 *   POST /api/v1/model-catalog/sync          → 202 {job_id,status,source}
 *   GET  /api/v1/model-catalog/diff
 *   POST /api/v1/model-catalog/apply-diff
 *   POST /api/v1/models/from-catalog
 *   POST /api/v1/models/:id/reset-override
 */

/** Catalog 中的一条上游模型记录（search/by-source/diff.catalog 均使用） */
export interface ModelCatalog {
  id: string
  source: 'litellm'
  source_model_id: string
  display_name: string | null
  provider: string | null
  mode: string | null
  context_length: number
  max_output_tokens: number
  input_price_per_million: number
  output_price_per_million: number
  cache_read_price: number | null
  cache_write_price: number | null
  supports_function_calling: boolean
  supports_vision: boolean
  supports_prompt_caching: boolean
  supports_response_schema: boolean
  supports_tool_choice: boolean
  /** ISO 时间戳；为 null 表示尚未公布废弃时间 */
  deprecation_date: string | null
  /** 最近一次从上游 catalog 拉取并落库的时间，用于"同步于 …" 提示 */
  synced_at: string
}

/** 价格/能力差异条目（来自 GET /api/v1/model-catalog/diff） */
export interface PriceDiffEntry {
  model_id: string
  model_name: string
  catalog_id: string
  catalog_source_model_id: string
  local_input_price: number
  local_output_price: number
  catalog_input_price: number
  catalog_output_price: number
  /** 本地价格是否处于"已 override"状态（true 表示不会被自动应用覆盖） */
  is_overridden: boolean
  /** 相对本地的变动百分比；正值表示上涨 */
  input_delta_pct: number
  output_delta_pct: number
  catalog_deprecation_date: string | null
}

/** POST /api/v1/model-catalog/sync 触发同步任务的返回 */
export interface SyncTriggerResponse {
  job_id: string
  status: string
  source: string
}

/** POST /api/v1/model-catalog/apply-diff 的返回 */
export interface ApplyDiffResponse {
  updated: number
  skipped: number
  failed_ids: string[]
}

/** POST /api/v1/models/from-catalog 的请求体 */
export interface CreateFromCatalogPayload {
  catalog_id: string
  provider_id: string
  name: string
}

/** 可被恢复跟随目录的字段集合（与 POST /api/v1/models/:id/reset-override 对齐） */
export type ResettableOverrideField = 'context_length' | 'max_output_tokens'

/** auto-bind 返回的成功项（已写库） */
export interface AutoBindMatch {
  model_id: string
  model_name: string
  catalog_id: string
  catalog_source_model_id: string
}

/** auto-bind 候选条目（同名多个目录条目时给 admin 二选一） */
export interface AutoBindCandidate {
  catalog_id: string
  catalog_source_model_id: string
  provider: string
}

/** auto-bind 一对多歧义（不会写库，仅返回候选） */
export interface AutoBindAmbiguity {
  model_id: string
  model_name: string
  candidates: AutoBindCandidate[]
}

/** POST /api/v1/model-catalog/auto-bind 的返回 */
export interface AutoBindResult {
  matched: AutoBindMatch[]
  ambiguous: AutoBindAmbiguity[]
  unmatched: number
}

/** POST /api/v1/models/:id/bind-catalog 的请求体（CAT-2） */
export interface BindCatalogPayload {
  catalog_id: string
  /** true 时同步把价格/容量/上游 ID 重置为新 catalog 的当前值；默认 false 仅切换关联 */
  reset_fields?: boolean
}

/** POST /api/v1/models/batch-bind-catalog 的请求体单条（CAT-2） */
export interface BatchBindItem {
  model_id: string
  catalog_id: string
  reset_fields?: boolean
}

/** POST /api/v1/models/batch-bind-catalog 的返回（CAT-2） */
export interface BatchBindResult {
  applied: number
  /** 失败时 model_id → 错误原因；成功时为空对象 */
  failed_ids: Record<string, string>
}
