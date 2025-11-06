import { defineStore } from 'pinia'
import { ref } from 'vue'
import { getPublicSettings as fetchPublicSettings, getSystemSettings as fetchSystemSettings, updateSystemSettings } from '@/services/control-plane'
import type { SystemSettings, PublicSettings } from '@/types/settings'

export const useSettingsStore = defineStore('settings', () => {
  const publicSettings = ref<PublicSettings | null>(null)
  const systemSettings = ref<SystemSettings | null>(null)
  const loaded = ref(false)
  const loading = ref(false)

  async function loadPublicSettings() {
    try {
      const response = await fetchPublicSettings()
      publicSettings.value = response.data
    } catch {
      publicSettings.value = { site_name: 'LLM Gateway', api_base_url: '' }
    }
  }

  async function loadSystemSettings() {
    loading.value = true
    try {
      const response = await fetchSystemSettings()
      systemSettings.value = response.data
      publicSettings.value = { site_name: response.data.site_name, api_base_url: response.data.api_base_url }
      loaded.value = true
    } catch {
      systemSettings.value = { site_name: 'LLM Gateway', api_base_url: '' }
    } finally {
      loading.value = false
    }
  }

  async function saveSettings(settings: SystemSettings): Promise<boolean> {
    try {
      await updateSystemSettings(settings)
      systemSettings.value = { ...settings }
      publicSettings.value = { site_name: settings.site_name, api_base_url: settings.api_base_url }
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
  }
})