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
