export interface SystemSettings {
  site_name: string
  site_logo: string
  site_subtitle: string
  api_base_url: string
  oidc_enabled: boolean
  oidc_provider_name: string
  oidc_client_id: string
  oidc_client_secret: string
  oidc_issuer_url: string
  oidc_redirect_uri: string
  oidc_scopes: string
  webhook_enabled: boolean
  webhook_url: string
  webhook_secret: string
  webhook_events: string
  rate_limit_enabled: boolean
  rate_limit_per_minute: number
  max_concurrency_per_key: number
  rate_limit_rpm: number
  rate_limit_tpd: number
  default_max_switches: number
  default_same_model_retries: number
  default_failover_cooldown_seconds: number
}

export interface PublicSettings {
  site_name: string
  site_logo: string
  oidc_enabled: boolean
  oidc_provider_name: string
}
