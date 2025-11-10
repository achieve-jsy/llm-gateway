import { defineStore } from 'pinia'
import { ref } from 'vue'
import { getPublicSettings as fetchPublicSettings, getSystemSettings as fetchSystemSettings, updateSystemSettings } from '@/services/control-plane'
import type { SystemSettings, PublicSettings } from '@/types/settings'

function defaultPublicSettings(): PublicSettings {
  return { site_name: 'LLM Gateway', site_logo: '', site_subtitle: '', api_base_url: '' }
}

function defaultSystemSettings(): SystemSettings {
  return { site_name: 'LLM Gateway', site_logo: '', site_subtitle: '', api_base_url: '' }
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

  async function saveSettings(settings: SystemSettings): Promise<boolean> {
    try {
      await updateSystemSettings(settings)
      systemSettings.value = { ...settings }
      publicSettings.value = { ...settings }
      return true
    } catch {
      return false
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
  }
})