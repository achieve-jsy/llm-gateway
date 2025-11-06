export interface SystemSettings {
  site_name: string
  api_base_url: string
}

export interface PublicSettings {
  site_name: string
  api_base_url: string
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