import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import QuotaEditDialog from '../QuotaEditDialog.vue'

const {
  getUserQuota,
  getServiceKeyQuota,
  getModelQuota,
  getGroupQuota,
  setUserQuota,
  setServiceKeyQuota,
} = vi.hoisted(() => ({
  getUserQuota: vi.fn(),
  getServiceKeyQuota: vi.fn(),
  getModelQuota: vi.fn(),
  getGroupQuota: vi.fn(),
  setUserQuota: vi.fn(),
  setServiceKeyQuota: vi.fn(),
}))

vi.mock('@/api/quota', () => ({
  QUOTA_DEFAULTS: {
    user: { rpm: 60, max_concurrency: 10 },
    service_key: { rpm: 120, max_concurrency: 20 },
    group: { rpm: 0, max_concurrency: 0 },
    model: { rpm: 0, max_concurrency: 0 },
  },
  getUserQuota,
  setUserQuota,
  getServiceKeyQuota,
  setServiceKeyQuota,
  getModelQuota,
  setModelQuota: vi.fn(),
  getGroupQuota,
  setGroupQuota: vi.fn(),
}))

vi.mock('element-plus', () => ({
  ElMessage: {
    success: vi.fn(),
    warning: vi.fn(),
    error: vi.fn(),
  },
}))

const stubs = {
  ElDialog: { template: '<section><slot /><slot name="footer" /></section>' },
  ElAlert: { props: ['title'], template: '<div>{{ title }}</div>' },
  ElForm: { template: '<form><slot /></form>' },
  ElFormItem: { props: ['label'], template: '<label>{{ label }}<slot /></label>' },
  ElInputNumber: {
    props: ['modelValue'],
    emits: ['update:modelValue'],
    template: '<input :value="modelValue ?? \'\'" />',
  },
  ElTag: { template: '<span><slot /></span>' },
  ElButton: { template: '<button><slot /></button>' },
  ElPopconfirm: { props: ['title'], template: '<div>{{ title }}<slot name="reference" /></div>' },
}

function quotaResponse(
  subjectType: 'user' | 'service_key' | 'group' | 'model',
  source: string,
  overrides: Record<string, unknown> = {},
) {
  return {
    data: {
      data: {
        rpm: 0,
        max_concurrency: 0,
        source,
        subject_type: subjectType,
        subject_id: 'subject-1',
        ...overrides,
      },
    },
  }
}

async function mountDialog(subjectType: 'user' | 'service_key' | 'group' | 'model') {
  const wrapper = mount(QuotaEditDialog, {
    props: {
      visible: false,
      subjectType,
      subjectId: 'subject-1',
      subjectLabel: '测试主体',
    },
    global: { stubs },
  })
  await wrapper.setProps({ visible: true })
  await flushPromises()
  return wrapper
}

describe('QuotaEditDialog 配额层级语义', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it.each([
    ['model', getModelQuota, '模型'],
    ['group', getGroupQuota, '模型组'],
  ] as const)('%s 未设置时显示未单独限制及主体层约束', async (subjectType, getter, subjectName) => {
    getter.mockResolvedValue(quotaResponse(subjectType, 'unset'))

    const wrapper = await mountDialog(subjectType)

    expect(wrapper.text()).toContain('留空 = 不额外限制')
    expect(wrapper.text()).toContain('未单独限制')
    expect(wrapper.text()).toContain(`${subjectName}未设置配额时不额外限制，仍受用户或 Service Key 配额约束`)
    expect(wrapper.text()).toContain('清除后将不再单独限制')
    expect(wrapper.text()).not.toContain('默认 60 RPM / 10 并发')
  })

  it.each([
    ['user', getUserQuota, '60', '10'],
    ['service_key', getServiceKeyQuota, '120', '20'],
  ] as const)('%s 留空时继续显示继承系统默认', async (subjectType, getter, rpm, concurrency) => {
    getter.mockResolvedValue(quotaResponse(subjectType, subjectType === 'user' ? 'default_user' : 'default_service_key'))

    const wrapper = await mountDialog(subjectType)

    expect(wrapper.text()).toContain(`留空 = 继承系统默认 (${rpm})`)
    expect(wrapper.text()).toContain(`系统默认 ${rpm} RPM / ${concurrency} 并发`)
    expect(wrapper.text()).toContain('清除后将继承系统默认')
    expect(wrapper.text()).not.toContain('未单独限制')
  })

  it('用户仅覆盖 RPM 时按原始字段初始化并保留并发继承', async () => {
    getUserQuota.mockResolvedValue(quotaResponse('user', 'override', {
      rpm: 321,
      max_concurrency: 10,
      configured_rpm: 321,
      configured_max_concurrency: null,
      rpm_source: 'override',
      max_concurrency_source: 'default_user',
      default_rpm: 60,
      default_max_concurrency: 10,
    }))
    setUserQuota.mockResolvedValue({})

    const wrapper = await mountDialog('user')
    const inputs = wrapper.findAll('input')
    expect(inputs[0].attributes('value')).toBe('321')
    expect(inputs[1].attributes('value')).toBe('')
    expect(wrapper.text()).toContain('自定义')
    expect(wrapper.text()).toContain('用户默认')

    await wrapper.findAll('button').find(button => button.text() === '保存')!.trigger('click')
    await flushPromises()

    expect(setUserQuota).toHaveBeenCalledWith('subject-1', {
      rpm: 321,
      max_concurrency: null,
    })
  })

  it('Service Key 仅覆盖并发时按原始字段初始化并保留 RPM 继承', async () => {
    getServiceKeyQuota.mockResolvedValue(quotaResponse('service_key', 'override', {
      rpm: 120,
      max_concurrency: 7,
      configured_rpm: null,
      configured_max_concurrency: 7,
      rpm_source: 'default_service_key',
      max_concurrency_source: 'override',
      default_rpm: 120,
      default_max_concurrency: 20,
    }))
    setServiceKeyQuota.mockResolvedValue({})

    const wrapper = await mountDialog('service_key')
    const inputs = wrapper.findAll('input')
    expect(inputs[0].attributes('value')).toBe('')
    expect(inputs[1].attributes('value')).toBe('7')
    expect(wrapper.text()).toContain('SK默认')
    expect(wrapper.text()).toContain('自定义')

    await wrapper.findAll('button').find(button => button.text() === '保存')!.trigger('click')
    await flushPromises()

    expect(setServiceKeyQuota).toHaveBeenCalledWith('subject-1', {
      rpm: null,
      max_concurrency: 7,
    })
  })

  it.each([
    ['model', getModelQuota],
    ['group', getGroupQuota],
  ] as const)('%s 部分覆盖时空字段仍表示不额外限制', async (subjectType, getter) => {
    getter.mockResolvedValue(quotaResponse(subjectType, 'override', {
      rpm: 0,
      max_concurrency: 5,
      configured_rpm: null,
      configured_max_concurrency: 5,
      rpm_source: 'unset',
      max_concurrency_source: 'override',
    }))

    const wrapper = await mountDialog(subjectType)
    const inputs = wrapper.findAll('input')
    expect(inputs[0].attributes('value')).toBe('')
    expect(inputs[1].attributes('value')).toBe('5')
    const effectiveItems = wrapper.findAll('.quota-effective-item')
    expect(effectiveItems[0].text()).toContain('RPM：未单独限制')
    expect(effectiveItems[0].text()).not.toContain('RPM：不限制')
    expect(effectiveItems[1].text()).toContain('并发：5')
    expect(effectiveItems[1].text()).toContain('自定义')
  })
})
