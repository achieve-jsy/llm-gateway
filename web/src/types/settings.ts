export interface SystemSettings {
  site_name: string
  site_logo: string
  site_subtitle: string
  api_base_url: string
  rate_limit_per_minute: number
  max_concurrency_per_key: number
  default_user_rpm: number
  default_user_max_concurrency: number
  default_service_key_rpm: number
  default_service_key_max_concurrency: number
  oidc_issuer_url: string
  oidc_client_id: string
  oidc_client_secret: string
  oidc_redirect_url: string
  oidc_scopes: string
  oidc_enabled: boolean
  webhook_enabled: boolean
  webhook_url: string
  webhook_secret: string
  webhook_events: string
  email_enabled: boolean
  smtp_host: string
  smtp_port: number
  smtp_from: string
  smtp_password: string
  smtp_tls_skip_verify: boolean
  default_max_switches: number
  default_same_model_retries: number
  default_failover_cooldown_seconds: number
  supplier_name: string
  pricing_usd_to_cny_rate: number
  budget_master_enabled: boolean
  budget_accounting_enabled: boolean
  budget_positive_enforcement_enabled: boolean
  budget_zero_block_enabled: boolean
  budget_shadow_enabled: boolean
  budget_v2_total_enforcement_enabled: boolean
  budget_legacy_total_write_enabled: boolean
  sticky_default_enabled: boolean
  sticky_default_ttl_seconds: number
}

export interface PublicSettings {
  site_name: string
  site_logo: string
  site_subtitle: string
  api_base_url: string
  rate_limit_per_minute: number
  max_concurrency_per_key: number
  supplier_name: string
  pricing_usd_to_cny_rate: number
}

export interface SettingsResponse {
  code: number
  message: string
  data: SystemSettings
}

export interface PublicSettingsResponse {
  code: number
  message: string
  data: PublicSettings
}

export interface NotificationChannelRule {
  notification_type: string
  channel: 'inapp' | 'email'
  enabled: boolean
}

// 通知渠道配置（per-type per-channel 精细化配置）
export interface NotificationChannelConfig {
  notification_type: string
  channel: 'webhook' | 'email'
  enabled: boolean
  webhook_url?: string | null
  webhook_secret?: string | null
  subject_template?: string | null
  body_template?: string | null
  created_at: string
  updated_at: string
}

// 通知配置元数据（类型定义 + 可用变量 + 支持渠道）
export interface NotificationConfigMetadata {
  types: Array<{
    name: string
    displayName: string
    description: string
    defaultVariables: string[]
    channels: string[]
  }>
  channels: Array<{
    name: string
    displayName: string
  }>
}

// 通知配置列表响应
export interface NotificationConfigListResponse {
  code: number
  message: string
  data: NotificationChannelConfig[]
}

// 通知配置单个响应
export interface NotificationConfigResponse {
  code: number
  message: string
  data: NotificationChannelConfig
}

// Webhook 端点（多 webhook fan-out：同类型下多个端点，一条告警发往所有启用端点）
export interface NotificationWebhookTarget {
  id: string
  notification_type: string
  name: string
  url: string
  secret?: string | null
  enabled: boolean
  sort_order: number
  created_at: string
  updated_at: string
}

// Webhook 端点列表响应
export interface NotificationWebhookTargetListResponse {
  code: number
  message: string
  data: NotificationWebhookTarget[]
}

// Webhook 端点单个响应
export interface NotificationWebhookTargetResponse {
  code: number
  message: string
  data: NotificationWebhookTarget
}
