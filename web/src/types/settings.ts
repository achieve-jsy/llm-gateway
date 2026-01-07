export interface SystemSettings {
  site_name: string
  site_logo: string
  site_subtitle: string
  api_base_url: string
  rate_limit_per_minute: number
  max_concurrency_per_key: number
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
  default_max_switches: number
  default_same_model_retries: number
  default_failover_cooldown_seconds: number
  supplier_name: string
  pricing_usd_to_cny_rate: number
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