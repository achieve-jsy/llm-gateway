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
