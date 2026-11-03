import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { setActivePinia, createPinia } from 'pinia'
import SettingsPage from '../SettingsPage.vue'
import { elementStubsKebab } from '@/test-utils/element-stubs'

vi.mock('element-plus', () => ({
  ElMessage: {
    success: vi.fn(),
    error: vi.fn(),
    warning: vi.fn(),
    info: vi.fn()
  }
}))

vi.mock('@/services/control-plane', () => ({
  getSystemSettings: vi.fn(),
  updateSystemSettings: vi.fn(),
  getPublicSettings: vi.fn(),
  testWebhook: vi.fn(),
  exportSettings: vi.fn(),
  importSettings: vi.fn(),
}))

import { ElMessage } from 'element-plus'
import { getSystemSettings, updateSystemSettings } from '@/services/control-plane'

const mockedGetSystem = vi.mocked(getSystemSettings)
const mockedUpdate = vi.mocked(updateSystemSettings)

const systemSettingsData = {
  site_name: 'My Gateway',
  site_logo: 'data:image/png;base64,abc',
  site_subtitle: 'My Subtitle',
  api_base_url: 'https://api.example.com'
}

const ImageUploadStub = defineComponent({
  name: 'ImageUpload',
  props: ['modelValue', 'mode', 'uploadLabel', 'removeLabel', 'hint', 'maxSize'],
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    return () =>
      h('div', {
        class: 'image-upload-stub',
        'data-mode': props.mode,
        'data-current': props.modelValue || ''
      }, [
        h('button', {
          class: 'stub-upload-btn',
          onClick: () => emit('update:modelValue', 'data:image/png;base64,newlogo')
        }, props.uploadLabel || '上传'),
        props.modelValue
          ? h('button', {
              class: 'stub-remove-btn',
              onClick: () => emit('update:modelValue', '')
            }, props.removeLabel || '移除')
          : null
      ])
  }
})

function mountPage() {
  return mount(SettingsPage, {
    global: {
      stubs: {
        ...elementStubsKebab,
        ImageUpload: ImageUploadStub
      }
    }
  })
}

function findSiteNameInput(wrapper: ReturnType<typeof mount>) {
  const inputs = wrapper.findAll('input[placeholder]')
  return inputs.find(el => el.element.getAttribute('placeholder') === 'LLM Gateway')
}

function findSubtitleInput(wrapper: ReturnType<typeof mount>) {
  const inputs = wrapper.findAll('input[placeholder]')
  return inputs.find(el => el.element.getAttribute('placeholder') === 'LLM API 统一网关')
}

function findApiUrlInput(wrapper: ReturnType<typeof mount>) {
  const inputs = wrapper.findAll('input[placeholder]')
  return inputs.find(el => el.element.getAttribute('placeholder') === '留空则自动使用当前访问域名')
}

function findSaveBtn(wrapper: ReturnType<typeof mount>) {
  const btns = wrapper.findAll('button')
  return btns.find(b => b.text() === '保存设置')
}

function findResetBtn(wrapper: ReturnType<typeof mount>) {
  const btns = wrapper.findAll('button')
  return btns.find(b => b.text() === '重置')
}

describe('SettingsPage.vue', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setActivePinia(createPinia())
    mockedGetSystem.mockImplementation(async () => ({
      data: { ...systemSettingsData }
    }) as any)
    mockedUpdate.mockResolvedValue(undefined as any)
  })

  describe('initial load', () => {
    it('should render page title', async () => {
      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.find('.page-title').exists()).toBe(true)
      expect(wrapper.find('.page-title').text()).toBe('系统设置')
    })

    it('should render page description', async () => {
      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.find('.page-desc').text()).toContain('站点品牌')
    })

    it('should load system settings on mount', async () => {
      mountPage()
      await flushPromises()

      expect(mockedGetSystem).toHaveBeenCalledTimes(1)
    })

    it('should populate form with loaded settings', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const nameInput = findSiteNameInput(wrapper)
      const subtitleInput = findSubtitleInput(wrapper)
      const apiInput = findApiUrlInput(wrapper)

      expect(nameInput).toBeDefined()
      expect((nameInput!.element as HTMLInputElement).value).toBe('My Gateway')
      expect((subtitleInput!.element as HTMLInputElement).value).toBe('My Subtitle')
      expect((apiInput!.element as HTMLInputElement).value).toBe('https://api.example.com')
    })

    it('should render image upload stub', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const stub = wrapper.find('.image-upload-stub')
      expect(stub.exists()).toBe(true)
      expect(stub.attributes('data-current')).toContain('data:image/png;base64,abc')
    })

    it('should render six setting cards', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const cards = wrapper.findAll('.settings-card')
      expect(cards).toHaveLength(6)
    })

    it('should render card with site brand header', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const cardTitles = wrapper.findAll('.card-title')
      expect(cardTitles[0].text()).toBe('站点品牌')
    })

    it('should render card with access config header', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const cardTitles = wrapper.findAll('.card-title')
      expect(cardTitles[1].text()).toBe('接入配置')
    })

    it('should show preview URL', async () => {
      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.find('.preview-url').exists()).toBe(true)
      expect(wrapper.find('.preview-url').text()).toContain('https://api.example.com/v1')
    })

    it('should show saved indicator initially', async () => {
      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.find('.saved-indicator').exists()).toBe(true)
      expect(wrapper.find('.saved-indicator').text()).toContain('已保存')
    })
  })

  describe('form interaction', () => {
    it('should show change indicator when site name is modified', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const input = findSiteNameInput(wrapper)
      await input!.setValue('New Name')
      await flushPromises()

      expect(wrapper.find('.change-indicator').exists()).toBe(true)
      expect(wrapper.find('.change-indicator').text()).toContain('未保存的更改')
    })

    it('should show change indicator when api_base_url is modified', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const input = findApiUrlInput(wrapper)
      await input!.setValue('https://new-api.example.com')
      await flushPromises()

      expect(wrapper.find('.change-indicator').exists()).toBe(true)
    })

    it('should update preview URL when api_base_url changes', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const input = findApiUrlInput(wrapper)
      await input!.setValue('https://custom.example.com')
      await flushPromises()

      expect(wrapper.find('.preview-url').text()).toContain('https://custom.example.com/v1')
    })

    it('should show default origin when api_base_url is empty', async () => {
      mockedGetSystem.mockImplementation(async () => ({
        data: { ...systemSettingsData, api_base_url: '' }
      }) as any)
      const wrapper = mountPage()
      await flushPromises()

      expect(wrapper.find('.preview-url').text()).toContain('/v1')
    })
  })

  describe('save', () => {
    it('should call updateSystemSettings with form values', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const input = findSiteNameInput(wrapper)
      await input!.setValue('Updated Gateway')
      await flushPromises()

      const saveBtn = findSaveBtn(wrapper)
      await saveBtn!.trigger('click')
      await flushPromises()

      expect(mockedUpdate).toHaveBeenCalledTimes(1)
      expect(mockedUpdate).toHaveBeenCalledWith({
        site_name: 'Updated Gateway',
        site_logo: 'data:image/png;base64,abc',
        site_subtitle: 'My Subtitle',
        api_base_url: 'https://api.example.com',
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

    it('should show success message on save', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const input = findSiteNameInput(wrapper)
      await input!.setValue('Updated')
      await flushPromises()

      const saveBtn = findSaveBtn(wrapper)
      await saveBtn!.trigger('click')
      await flushPromises()

      expect(ElMessage.success).toHaveBeenCalledWith('设置已保存')
    })

    it('should hide change indicator after save', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const input = findSiteNameInput(wrapper)
      await input!.setValue('Updated')
      await flushPromises()
      expect(wrapper.find('.change-indicator').exists()).toBe(true)

      const saveBtn = findSaveBtn(wrapper)
      await saveBtn!.trigger('click')
      await flushPromises()

      expect(wrapper.find('.saved-indicator').exists()).toBe(true)
      expect(wrapper.find('.change-indicator').exists()).toBe(false)
    })

    it('should show error message when save fails', async () => {
      mockedUpdate.mockRejectedValueOnce(new Error('boom'))
      const wrapper = mountPage()
      await flushPromises()

      const input = findSiteNameInput(wrapper)
      await input!.setValue('Updated')
      await flushPromises()

      const saveBtn = findSaveBtn(wrapper)
      await saveBtn!.trigger('click')
      await flushPromises()

      expect(ElMessage.error).toHaveBeenCalledWith('保存失败，请重试')
    })

    it('should trim string fields before saving', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const nameInput = findSiteNameInput(wrapper)
      const subtitleInput = findSubtitleInput(wrapper)
      await nameInput!.setValue('  Spaced Name  ')
      await subtitleInput!.setValue('  Spaced Subtitle  ')
      await flushPromises()

      const saveBtn = findSaveBtn(wrapper)
      await saveBtn!.trigger('click')
      await flushPromises()

      expect(mockedUpdate).toHaveBeenCalledWith(expect.objectContaining({
        site_name: 'Spaced Name',
        site_subtitle: 'Spaced Subtitle'
      }))
    })
  })

  describe('reset', () => {
    it('should reset form to last saved values', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const input = findSiteNameInput(wrapper)
      await input!.setValue('Changed Name')
      await flushPromises()

      const resetBtn = findResetBtn(wrapper)
      await resetBtn!.trigger('click')
      await flushPromises()

      const nameInput = findSiteNameInput(wrapper)
      expect((nameInput!.element as HTMLInputElement).value).toBe('My Gateway')
    })

    it('should hide change indicator after reset', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const input = findSiteNameInput(wrapper)
      await input!.setValue('Changed')
      await flushPromises()
      expect(wrapper.find('.change-indicator').exists()).toBe(true)

      const resetBtn = findResetBtn(wrapper)
      await resetBtn!.trigger('click')
      await flushPromises()

      expect(wrapper.find('.saved-indicator').exists()).toBe(true)
      expect(wrapper.find('.change-indicator').exists()).toBe(false)
    })

    it('should restore logo when reset', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const uploadBtn = wrapper.find('.stub-upload-btn')
      await uploadBtn.trigger('click')
      await flushPromises()

      const resetBtn = findResetBtn(wrapper)
      await resetBtn!.trigger('click')
      await flushPromises()

      expect(wrapper.find('.image-upload-stub').attributes('data-current')).toContain('data:image/png;base64,abc')
    })
  })

  describe('image upload interaction', () => {
    it('should update logo when upload stub emits', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const uploadBtn = wrapper.find('.stub-upload-btn')
      await uploadBtn.trigger('click')
      await flushPromises()

      expect(wrapper.find('.image-upload-stub').attributes('data-current')).toContain('data:image/png;base64,newlogo')
      expect(wrapper.find('.change-indicator').exists()).toBe(true)
    })

    it('should clear logo when remove stub emits', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const removeBtn = wrapper.find('.stub-remove-btn')
      await removeBtn.trigger('click')
      await flushPromises()

      expect(wrapper.find('.image-upload-stub').attributes('data-current')).toBe('')
      expect(wrapper.find('.change-indicator').exists()).toBe(true)
    })

    it('should save with updated logo', async () => {
      const wrapper = mountPage()
      await flushPromises()

      const uploadBtn = wrapper.find('.stub-upload-btn')
      await uploadBtn.trigger('click')
      await flushPromises()

      const saveBtn = findSaveBtn(wrapper)
      await saveBtn!.trigger('click')
      await flushPromises()

      expect(mockedUpdate).toHaveBeenCalledWith(expect.objectContaining({
        site_logo: 'data:image/png;base64,newlogo'
      }))
    })
  })

  describe('error handling', () => {
    it('should handle load failure gracefully', async () => {
      mockedGetSystem.mockRejectedValueOnce(new Error('network error'))
      const wrapper = mountPage()
      await flushPromises()

      const nameInput = findSiteNameInput(wrapper)
      expect(nameInput).toBeDefined()
    })
  })
})