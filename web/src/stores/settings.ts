import { defineStore } from 'pinia'
import { ref } from 'vue'
import { getPublicSettings as fetchPublicSettings, getSystemSettings as fetchSystemSettings, updateSystemSettings, testWebhook, testEmail } from '@/services/control-plane'
import type { SystemSettings, PublicSettings } from '@/types/settings'

function defaultPublicSettings(): PublicSettings {
  return { site_name: 'LLM Gateway', site_logo: '', site_subtitle: '', api_base_url: '', rate_limit_per_minute: 60, max_concurrency_per_key: 10, supplier_name: '某公司', pricing_usd_to_cny_rate: 7.2 }
}

function defaultSystemSettings(): SystemSettings {
  return {
    site_name: 'LLM Gateway', site_logo: '', site_subtitle: '', api_base_url: '',
    rate_limit_per_minute: 60, max_concurrency_per_key: 10,
    default_user_rpm: 60, default_user_max_concurrency: 10,
    default_service_key_rpm: 120, default_service_key_max_concurrency: 20,
    oidc_issuer_url: '', oidc_client_id: '', oidc_client_secret: '',
    oidc_redirect_url: '', oidc_scopes: 'openid profile email', oidc_enabled: false,
    webhook_enabled: false, webhook_url: '', webhook_secret: '', webhook_events: 'health_check_failed',
    email_enabled: false, smtp_host: 'mail.example.com', smtp_port: 587,
    smtp_from: 'llm-gateway@example.com', smtp_password: '', smtp_tls_skip_verify: false,
    default_max_switches: 2, default_same_model_retries: 1, default_failover_cooldown_seconds: 60,
    supplier_name: '某公司',
    pricing_usd_to_cny_rate: 7.2,
    budget_positive_enforcement_enabled: false,
    budget_zero_block_enabled: false,
    budget_shadow_enabled: false,
    budget_v2_total_enforcement_enabled: false,
  }
}

export const useSettingsStore = defineStore('settings', () => {
  const publicSettings = ref<PublicSettings>(defaultPublicSettings())
  const systemSettings = ref<SystemSettings>(defaultSystemSettings())
  const loaded = ref(false)
  const loading = ref(false)

  async function loadPublicSettings() {
    try {
      const response = await fetchPublicSettings()
      publicSettings.value = response.data
    } catch {
      publicSettings.value = defaultPublicSettings()
    }
  }

  async function loadSystemSettings() {
    loading.value = true
    try {
      const response = await fetchSystemSettings()
      systemSettings.value = response.data
      publicSettings.value = { ...response.data }
      loaded.value = true
    } catch {
      systemSettings.value = defaultSystemSettings()
    } finally {
      loading.value = false
    }
  }

  async function saveSettings(settings: SystemSettings): Promise<{ ok: boolean; message?: string }> {
    try {
      await updateSystemSettings(settings)
      systemSettings.value = { ...settings }
      publicSettings.value = { ...settings }
      return { ok: true }
    } catch (err: unknown) {
      const message = (err as { response?: { data?: { error?: { message?: string } } } })?.response?.data?.error?.message
      return { ok: false, message: message || '保存失败，请重试' }
    }
  }

  function getBaseUrl(): string {
    if (publicSettings.value?.api_base_url) {
      return publicSettings.value.api_base_url.replace(/\/+$/, '')
    }
    return window.location.origin
  }

  function getSiteName(): string {
    return publicSettings.value?.site_name || 'LLM Gateway'
  }

  function getSiteLogo(): string {
    return publicSettings.value?.site_logo || ''
  }

  return {
    publicSettings,
    systemSettings,
    loaded,
    loading,
    loadPublicSettings,
    loadSystemSettings,
    saveSettings,
    getBaseUrl,
    getSiteName,
    getSiteLogo,
    testWebhook,
    testEmail,
  }
})