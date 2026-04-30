/** 凭证字段 Schema 定义 */
export interface CredentialFieldSchema {
  type: string
  required: boolean
  secret: boolean
  label: string
  placeholder?: string
  default?: string
}

/** 凭证 Schema（字段名 → 字段定义） */
export type CredentialSchema = Record<string, CredentialFieldSchema>

/** 厂商差异化配置 */
export interface VendorOptions {
  url_pattern?: string
  auth_header?: string
  requires_api_version?: boolean
  default_api_version?: string
  default_region?: string
  stream_usage_in_last_chunk?: boolean
  empty_choices_skip?: boolean
  supports_stream_options?: boolean
  supports_reasoning_content?: boolean
  extra_headers?: Record<string, string>
}

/** 渠道定义（Provider Registry 条目） */
export interface ProviderDefinition {
  id: string
  display_name: string
  adapter: string
  base_url: string
  credential_schema: CredentialSchema
  vendor_options: VendorOptions
  description: string
  icon_url: string
  doc_url: string
  default_models: string[]
  builtin: boolean
  enabled: boolean
}

/** 渠道/供应商信息 */
export interface Provider {
  id: string
  name: string
  definition_id: string
  type: string
  endpoint: string
  credentials: string
  enabled: boolean
  weight: number
  priority: number
  status: number
  test_model: string
  response_time: number
  balance: number
  remark: string
  max_retries: number
  timeout: number
  tag: string
  consecutive_failures: number
  last_check_error: string
  model_count: number
  created_at: string
  updated_at: string
}

/** 渠道状态枚举 */
export const ProviderStatus = {
  ENABLED: 1,
  MANUALLY_DISABLED: 2,
  AUTO_DISABLED: 3,
} as const

/** 创建/编辑渠道的请求体 */
export interface ProviderPayload {
  name: string
  definition_id: string
  type?: string
  endpoint: string
  credentials: string
  weight?: number
  priority?: number
  test_model?: string
  remark?: string
  max_retries?: number
  timeout?: number
  tag?: string
}

/** 快速接入：批量创建时每个待创建渠道的凭证输入 */
export interface QuickOnboardCredentialInput {
  /** 凭证 JSON 字符串，与 ProviderPayload.credentials 保持一致 */
  credentials: string
  /** 可选备注；会合并到该渠道 remark */
  remark?: string
}

/** 快速接入：每个渠道都要创建的模型输入 */
export interface QuickOnboardModelInput {
  name: string
  model_type?: 'chat' | 'embedding' | 'embedding_vl' | 'rerank' | 'rerank_vl' | 'video'
  upstream_model_id?: string
  description?: string
  context_length?: number
  max_output_tokens?: number
  price_mode?: 'catalog' | 'manual'
  input_price?: number
  output_price?: number
  weight?: number
}

export interface QuickOnboardProviderInput {
  definition_id: string
  type?: string
  endpoint: string
  weight?: number
  priority?: number
  test_model?: string
  remark?: string
  max_retries?: number
  timeout?: number
  tag?: string
}

/** 快速接入：批量创建同供应商多个 Key 渠道，并为每个渠道创建相同模型集 */
export interface QuickOnboardPayload {
  provider: QuickOnboardProviderInput
  provider_slug?: string
  purpose?: string
  name_prefix?: string
  credentials: QuickOnboardCredentialInput[]
  models: QuickOnboardModelInput[]
}

export interface QuickOnboardProviderPlan {
  index: number
  name: string
  credential: string
  models: string[]
  model_count: number
  remark?: string
  will_create: boolean
}

export interface QuickOnboardPreview {
  provider_count: number
  model_count: number
  matrix_count: number
  name_prefix: string
  providers: QuickOnboardProviderPlan[]
  warnings: string[]
}

export type QuickOnboardItemStatus = 'success' | 'failed'

export interface QuickOnboardProviderResult {
  index: number
  name: string
  provider_id?: string
  model_ids?: string[]
  model_count: number
  status: QuickOnboardItemStatus
  error?: string
}

export interface QuickOnboardCreateResult {
  provider_count: number
  succeeded_count: number
  failed_count: number
  model_count_per_provider: number
  matrix_count: number
  results: QuickOnboardProviderResult[]
}
