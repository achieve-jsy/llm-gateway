import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useSettingsStore } from '@/stores/settings'

vi.mock('@/services/control-plane', () => ({
  getPublicSettings: vi.fn(),
  getSystemSettings: vi.fn(),
  updateSystemSettings: vi.fn(),
  testWebhook: vi.fn(),
  exportSettings: vi.fn(),
  importSettings: vi.fn(),
}))

import { getPublicSettings, getSystemSettings, updateSystemSettings } from '@/services/control-plane'

const mockedGetPublic = vi.mocked(getPublicSettings)
const mockedGetSystem = vi.mocked(getSystemSettings)
const mockedUpdate = vi.mocked(updateSystemSettings)

const systemSettingsData = {
  site_name: 'My Gateway',
  site_logo: 'data:image/png;base64,abc123',
  site_subtitle: 'Awesome API Gateway',
  api_base_url: 'https://api.example.com'
}

const publicSettingsData = {
  site_name: 'My Gateway',
  site_logo: 'data:image/png;base64,abc123',
  site_subtitle: 'Awesome API Gateway',
  api_base_url: 'https://api.example.com'
}

function createStore() {
  return useSettingsStore()
}

describe('useSettingsStore', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setActivePinia(createPinia())
  })

  describe('initial state', () => {
    it('should initialize with default values', () => {
      const store = createStore()
      expect(store.loaded).toBe(false)
      expect(store.loading).toBe(false)
      expect(store.publicSettings).toEqual({
        site_name: 'LLM Gateway',
        site_logo: '',
        site_subtitle: '',
        api_base_url: '',
        rate_limit_per_minute: 60,
        max_concurrency_per_key: 10,
      })
      expect(store.systemSettings).toEqual({
        site_name: 'LLM Gateway',
        site_logo: '',
        site_subtitle: '',
        api_base_url: '',
        rate_limit_per_minute: 60,
        max_concurrency_per_key: 10,
        oidc_issuer_url: '',
        oidc_client_id: '',
        oidc_client_secret: '',
        oidc_redirect_url: '',
        oidc_scopes: 'openid profile email',
        oidc_enabled: false,
        webhook_enabled: false,
        webhook_url: '',
        webhook_secret: '',
        webhook_events: 'health_check_failed',
      })
    })
  })

  describe('loadPublicSettings', () => {
    it('should fetch public settings and update state', async () => {
      mockedGetPublic.mockResolvedValueOnce({ data: { ...publicSettingsData } } as any)
      const store = createStore()

      await store.loadPublicSettings()

      expect(mockedGetPublic).toHaveBeenCalledTimes(1)
      expect(store.publicSettings).toEqual(publicSettingsData)
    })

    it('should use defaults when API call fails', async () => {
      mockedGetPublic.mockRejectedValueOnce(new Error('network error'))
      const store = createStore()

      await store.loadPublicSettings()

      expect(store.publicSettings.site_name).toBe('LLM Gateway')
      expect(store.publicSettings.site_logo).toBe('')
    })

    it('should keep state unchanged on error', async () => {
      mockedGetPublic.mockRejectedValueOnce(new Error('boom'))
      const store = createStore()
      store.publicSettings = { ...publicSettingsData }
      store.systemSettings = { ...systemSettingsData }

      await store.loadPublicSettings()

      expect(store.publicSettings.site_name).toBe('LLM Gateway')
    })
  })

  describe('loadSystemSettings', () => {
    it('should fetch system settings and update both stores', async () => {
      mockedGetSystem.mockResolvedValueOnce({ data: { ...systemSettingsData } } as any)
      const store = createStore()

      await store.loadSystemSettings()

      expect(mockedGetSystem).toHaveBeenCalledTimes(1)
      expect(store.systemSettings).toEqual(systemSettingsData)
      expect(store.publicSettings).toEqual(systemSettingsData)
      expect(store.loaded).toBe(true)
      expect(store.loading).toBe(false)
    })

    it('should set loading to true during fetch', () => {
      mockedGetSystem.mockImplementationOnce(() => new Promise(() => {}))
      const store = createStore()

      store.loadSystemSettings()

      expect(store.loading).toBe(true)
    })

    it('should set loading to false after fetch completes', async () => {
      mockedGetSystem.mockResolvedValueOnce({ data: { ...systemSettingsData } } as any)
      const store = createStore()

      await store.loadSystemSettings()

      expect(store.loading).toBe(false)
    })

    it('should set loading to false after fetch fails', async () => {
      mockedGetSystem.mockRejectedValueOnce(new Error('boom'))
      const store = createStore()

      await store.loadSystemSettings()

      expect(store.loading).toBe(false)
    })

    it('should not set loaded to true on failure', async () => {
      mockedGetSystem.mockRejectedValueOnce(new Error('boom'))
      const store = createStore()

      await store.loadSystemSettings()

      expect(store.loaded).toBe(false)
    })

    it('should use defaults when API call fails', async () => {
      mockedGetSystem.mockRejectedValueOnce(new Error('network error'))
      const store = createStore()

      await store.loadSystemSettings()

      expect(store.systemSettings.site_name).toBe('LLM Gateway')
    })
  })

  describe('saveSettings', () => {
    it('should call updateSystemSettings with correct payload', async () => {
      mockedUpdate.mockResolvedValueOnce(undefined)
      const store = createStore()

      const result = await store.saveSettings(systemSettingsData)

      expect(mockedUpdate).toHaveBeenCalledTimes(1)
      expect(mockedUpdate).toHaveBeenCalledWith(systemSettingsData)
      expect(result).toBe(true)
    })

    it('should update both stores after successful save', async () => {
      mockedUpdate.mockResolvedValueOnce(undefined)
      const store = createStore()

      await store.saveSettings(systemSettingsData)

      expect(store.systemSettings).toEqual(systemSettingsData)
      expect(store.publicSettings).toEqual(systemSettingsData)
    })

    it('should return false when save fails', async () => {
      mockedUpdate.mockRejectedValueOnce(new Error('save error'))
      const store = createStore()

      const result = await store.saveSettings(systemSettingsData)

      expect(result).toBe(false)
    })

    it('should not update local state when save fails', async () => {
      mockedUpdate.mockRejectedValueOnce(new Error('save error'))
      const store = createStore()
      const original = { ...store.systemSettings }

      await store.saveSettings(systemSettingsData)

      expect(store.systemSettings).toEqual(original)
    })

    it('should handle empty strings correctly', async () => {
      mockedUpdate.mockResolvedValueOnce(undefined)
      const store = createStore()
      const emptySettings = { site_name: '', site_logo: '', site_subtitle: '', api_base_url: '' }

      const result = await store.saveSettings(emptySettings)

      expect(result).toBe(true)
      expect(mockedUpdate).toHaveBeenCalledWith(emptySettings)
    })
  })

  describe('getBaseUrl', () => {
    it('should return api_base_url without trailing slash', () => {
      const store = createStore()
      store.publicSettings = { ...publicSettingsData, api_base_url: 'https://api.example.com/' }

      const url = store.getBaseUrl()

      expect(url).toBe('https://api.example.com')
    })

    it('should return api_base_url as-is if no trailing slash', () => {
      const store = createStore()
      store.publicSettings = { ...publicSettingsData, api_base_url: 'https://api.example.com' }

      const url = store.getBaseUrl()

      expect(url).toBe('https://api.example.com')
    })

    it('should return current origin when api_base_url is empty', () => {
      const store = createStore()
      store.publicSettings = { ...publicSettingsData, api_base_url: '' }

      const url = store.getBaseUrl()

      expect(url).toBe(window.location.origin)
    })

    it('should return current origin when publicSettings is null', () => {
      const store = createStore()
      store.publicSettings = null as any

      const url = store.getBaseUrl()

      expect(url).toBe(window.location.origin)
    })
  })

  describe('getSiteName', () => {
    it('should return site_name from public settings', () => {
      const store = createStore()
      store.publicSettings = { ...publicSettingsData, site_name: 'Custom Gateway' }

      expect(store.getSiteName()).toBe('Custom Gateway')
    })

    it('should return default when publicSettings is null', () => {
      const store = createStore()
      store.publicSettings = null as any

      expect(store.getSiteName()).toBe('LLM Gateway')
    })

    it('should return default when site_name is empty', () => {
      const store = createStore()
      store.publicSettings = { ...publicSettingsData, site_name: '' }

      expect(store.getSiteName()).toBe('LLM Gateway')
    })
  })

  describe('getSiteLogo', () => {
    it('should return site_logo from public settings', () => {
      const store = createStore()
      store.publicSettings = { ...publicSettingsData, site_logo: 'data:image/png;base64,xyz' }

      expect(store.getSiteLogo()).toBe('data:image/png;base64,xyz')
    })

    it('should return empty string when site_logo is empty', () => {
      const store = createStore()
      store.publicSettings = { ...publicSettingsData, site_logo: '' }

      expect(store.getSiteLogo()).toBe('')
    })

    it('should return empty string when publicSettings is null', () => {
      const store = createStore()
      store.publicSettings = null as any

      expect(store.getSiteLogo()).toBe('')
    })
  })
})