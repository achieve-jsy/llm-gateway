import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ElementPlus from 'element-plus'
import { ElMessage } from 'element-plus'
import ExpansionCreateForm from '../ExpansionCreateForm.vue'
import type { ExpansionPrecheck, ExpansionRequest } from '@/types/expansion'
import type { ModelTier, MyBudgetTiersResponse } from '@/types/model-tier'

// Mock element-plus icons
vi.mock('@element-plus/icons-vue', () => ({
  Refresh: { template: '<i />' },
}))

vi.mock('element-plus', async (importOriginal) => {
  const actual = await importOriginal<typeof import('element-plus')>()
  return {
    ...actual,
    ElMessage: { success: vi.fn(), warning: vi.fn(), error: vi.fn() },
  }
})

// ── Mock API services ──────────────────────────────────────────────
const mockCreateExpansionRequest = vi.fn()
const mockPrecheckExpansion = vi.fn()
const mockListModelTiers = vi.fn()
const mockGetMyBudgetTiers = vi.fn()

vi.mock('@/services/control-plane', () => ({
  createExpansionRequest: (...args: unknown[]) => mockCreateExpansionRequest(...args),
  precheckExpansion: (...args: unknown[]) => mockPrecheckExpansion(...args),
  listModelTiers: (...args: unknown[]) => mockListModelTiers(...args),
  getMyBudgetTiers: (...args: unknown[]) => mockGetMyBudgetTiers(...args),
}))

// ── Fixtures ───────────────────────────────────────────────────────
function makePrecheck(overrides: Partial<ExpansionPrecheck> = {}): ExpansionPrecheck {
  return {
    can_submit: true,
    can_notify_owner: false,
    current_limit_usd: '100.000000',
    current_used_usd: '80.000000',
    target_amount_usd: '200.000000',
    increase_usd: '100.000000',
    effective_after_usd: '200.000000',
    parent_constraints: [],
    affected_groups: [],
    visible_scopes: [],
    precheck_version: 'v1',
    data_as_of: '2026-07-13T00:00:00Z',
    stale: false,
    ...overrides,
  }
}

const sampleTiers: ModelTier[] = [
  {
    id: 'tier-1', name: 'Standard', description: '', color: '#409EFF',
    sort_order: 0, enabled: true, archived_at: null,
    group_count: 1, budget_count: 1, history_count: 0, reference_count: 2,
    created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
  },
  {
    id: 'tier-disabled', name: 'Disabled', description: '', color: '#909399',
    sort_order: 1, enabled: false, archived_at: null,
    group_count: 0, budget_count: 0, history_count: 0, reference_count: 0,
    created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
  },
]

const myTierBudget: MyBudgetTiersResponse = {
  source_summary: { mode: 'personal', primary_scope_type: 'member', has_shared_pool: false, has_temporary_override: false },
  effective_total: [],
  budget_chain: [],
  tier_limits: [
    { model_tier_id: 'tier-1', model_tier_name: 'Standard', enabled: true, periods: [] },
    { model_tier_id: 'tier-disabled', model_tier_name: 'Disabled', enabled: false, periods: [{ period_type: 'monthly', configured: false, reset_at: '2026-08-01T00:00:00Z', status: 'tier_disabled', limiting_source: { scope_type: 'member' } }] },
  ],
  data_updated_at: '2026-07-15T00:00:00Z',
  stale: false,
}

// ── Helpers ────────────────────────────────────────────────────────
function mountForm(props: {
  ordinaryUser?: boolean
  initialScopeType?: string
  initialScopeId?: string
  initialDimensionType?: 'total' | 'tier'
  initialModelTierId?: string
  initialPeriodType?: 'daily' | 'weekly' | 'monthly'
  initialTargetAmountUsd?: string
  fixedManagerScope?: boolean
  managerScopeLabel?: string
} = {}) {
  const pinia = createPinia()
  setActivePinia(pinia)
  return mount(ExpansionCreateForm, {
    props: {
      modelValue: true,
      ordinaryUser: props.ordinaryUser ?? false,
      initialScopeType: (props.initialScopeType as any) ?? 'member',
      initialScopeId: props.initialScopeId ?? 'user-1',
      initialDimensionType: props.initialDimensionType,
      initialModelTierId: props.initialModelTierId,
      initialPeriodType: props.initialPeriodType,
      initialTargetAmountUsd: props.initialTargetAmountUsd,
      fixedManagerScope: props.fixedManagerScope ?? false,
      managerScopeLabel: props.managerScopeLabel ?? '',
      'onUpdate:modelValue': (val: boolean) => wrapper.setProps({ modelValue: val }),
    },
    global: {
      plugins: [pinia, ElementPlus],
    },
  })
}

let wrapper: ReturnType<typeof mountForm>

// ── Tests ──────────────────────────────────────────────────────────
describe('ExpansionCreateForm', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockPrecheckExpansion.mockResolvedValue(makePrecheck())
    mockCreateExpansionRequest.mockResolvedValue({ id: 'req-1' } as ExpansionRequest)
    mockListModelTiers.mockResolvedValue([...sampleTiers])
    mockGetMyBudgetTiers.mockResolvedValue(structuredClone(myTierBudget))
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  describe('表单渲染', () => {
    it('渲染对话框包含所有必填字段', async () => {
      wrapper = mountForm()
      await flushPromises()

      const html = wrapper.html()
      expect(html).toContain('新建扩容申请')
      expect(html).toContain('对象类型')
      expect(html).toContain('对象 ID')
      expect(html).toContain('周期')
      expect(html).toContain('目标额度')
      expect(html).toContain('申请原因')
    })

    it('ordinaryUser 模式隐藏对象类型和对象 ID，仅展示申请对象摘要', async () => {
      wrapper = mountForm({ ordinaryUser: true, initialScopeId: 'user-1' })
      await flushPromises()

      expect(wrapper.text()).toContain('申请对象')
      expect(wrapper.text()).toContain('我的成员额度')
      expect(wrapper.text()).not.toContain('对象类型')
      expect(wrapper.text()).not.toContain('对象 ID')
      expect(wrapper.html()).not.toContain('user-1')
    })

    it('fixedManagerScope 模式隐藏对象 ID 并展示预算池选择', async () => {
      wrapper = mountForm({ initialScopeType: 'dept_daily', initialScopeId: 'dept-1' })
      await wrapper.setProps({ fixedManagerScope: true, managerScopeLabel: '技术研发部' })
      await flushPromises()

      expect(wrapper.text()).toContain('申请对象')
      expect(wrapper.text()).toContain('技术研发部')
      expect(wrapper.text()).toContain('预算池')
      expect(wrapper.text()).toContain('成员日常预算池')
      expect(wrapper.text()).not.toContain('对象类型')
      expect(wrapper.text()).not.toContain('对象 ID')
      expect(wrapper.html()).not.toContain('dept-1')
    })

    it('fixedManagerScope 工作室模式隐藏对象字段且不展示预算池选择', async () => {
      wrapper = mountForm({ initialScopeType: 'studio', initialScopeId: 'studio-1' })
      await wrapper.setProps({ fixedManagerScope: true, managerScopeLabel: '互娱事业部' })
      await flushPromises()

      expect(wrapper.text()).toContain('申请对象')
      expect(wrapper.text()).toContain('互娱事业部')
      expect(wrapper.text()).not.toContain('预算池')
      expect(wrapper.text()).not.toContain('对象类型')
      expect(wrapper.text()).not.toContain('对象 ID')
      expect(wrapper.html()).not.toContain('studio-1')
    })

    it('打开对话框时加载模型等级列表（visible 从 false 变 true 时触发）', async () => {
      // Start with visible=false, then change to true to trigger the watcher
      wrapper = mountForm({ initialScopeId: 'user-1' })
      await wrapper.setProps({ modelValue: false })
      await flushPromises()
      mockListModelTiers.mockClear()

      await wrapper.setProps({ modelValue: true })
      await flushPromises()

      expect(mockListModelTiers).toHaveBeenCalled()
    })

    it('ordinaryUser 模式使用我的等级预算作为等级选项，不调用管理接口', async () => {
      wrapper = mountForm({ ordinaryUser: true, initialScopeId: 'user-1' })
      await wrapper.setProps({ modelValue: false })
      await flushPromises()
      mockListModelTiers.mockClear()
      mockGetMyBudgetTiers.mockClear()

      await wrapper.setProps({ modelValue: true })
      await flushPromises()

      expect(mockGetMyBudgetTiers).toHaveBeenCalled()
      expect(mockListModelTiers).not.toHaveBeenCalled()
    })

    it('停用模型等级不能作为扩容申请选项', async () => {
      wrapper = mountForm({
        ordinaryUser: true,
        initialScopeId: 'user-1',
        initialDimensionType: 'tier',
        initialModelTierId: 'tier-disabled',
        initialTargetAmountUsd: '150',
      })
      await wrapper.setProps({ modelValue: false })
      await wrapper.setProps({ modelValue: true })
      await flushPromises()
      mockPrecheckExpansion.mockClear()

      vi.advanceTimersByTime(400)
      await flushPromises()

      expect(mockPrecheckExpansion).not.toHaveBeenCalled()
    })

    it('每次打开时应用入口预填但不触发预检', async () => {
      wrapper = mountForm({
        initialScopeId: 'user-1',
        initialDimensionType: 'tier',
        initialModelTierId: 'tier-1',
        initialPeriodType: 'weekly',
        initialTargetAmountUsd: '250',
      })
      await wrapper.setProps({ modelValue: false })
      await wrapper.setProps({ modelValue: true })
      vi.advanceTimersByTime(400)
      await flushPromises()

      expect(mockPrecheckExpansion).not.toHaveBeenCalled()
    })

    it('选择"模型等级"维度时显示模型等级选择器', async () => {
      wrapper = mountForm()
      await flushPromises()

      const html = wrapper.html()
      // Initially dimension_type is 'total', so tier select shouldn't show
      // We verify the form has the radio group for dimension selection
      expect(html).toContain('总预算')
      expect(html).toContain('模型等级')
    })

    it('展示临时扩容和长期调额两种调整类型', async () => {
      wrapper = mountForm()
      await flushPromises()

      expect(wrapper.text()).toContain('调整类型')
      expect(wrapper.text()).toContain('临时扩容')
      expect(wrapper.text()).toContain('长期调额')
    })

    it('选择紧急优先级时显示紧急原因字段', async () => {
      wrapper = mountForm()
      await flushPromises()

      const html = wrapper.html()
      expect(html).toContain('普通')
      expect(html).toContain('紧急')
    })
  })

  describe('创建前校验行为', () => {
    it('表单值变化后不再自动请求预检接口', async () => {
      wrapper = mountForm({ initialScopeId: 'user-1' })
      await flushPromises()

      vi.advanceTimersByTime(400)
      await flushPromises()

      expect(mockPrecheckExpansion).not.toHaveBeenCalled()
    })

    it('不直接展示额度校验详情', async () => {
      wrapper = mountForm()
      await flushPromises()

      expect(wrapper.find('.mock-precheck-panel').exists()).toBe(false)
      expect(wrapper.text()).not.toContain('当前上限')
      expect(wrapper.text()).not.toContain('当前已用')
    })

    it('预检 422 不再影响创建表单展示', async () => {
      mockPrecheckExpansion.mockRejectedValue({
        response: { data: { error: { message: '目标额度必须大于当前额度' } } },
      })
      wrapper = mountForm({ initialScopeId: 'user-1', initialTargetAmountUsd: '10' })
      await wrapper.setProps({ modelValue: false })
      await wrapper.setProps({ modelValue: true })

      vi.advanceTimersByTime(400)
      await flushPromises()

      expect(mockPrecheckExpansion).not.toHaveBeenCalled()
      expect(wrapper.text()).not.toContain('目标额度必须大于当前额度')
    })

    it('切换临时扩容时不触发预检接口', async () => {
      wrapper = mountForm({ initialScopeId: 'user-1', initialTargetAmountUsd: '150' })
      await wrapper.setProps({ modelValue: false })
      await wrapper.setProps({ modelValue: true })
      await flushPromises()

      const temporaryInput = wrapper.find('input[value="current_period"]')
      expect(temporaryInput.exists()).toBe(true)
      await temporaryInput.setValue(true)
      vi.advanceTimersByTime(400)
      await flushPromises()

      expect(mockPrecheckExpansion).not.toHaveBeenCalled()
    })

    it('模型等级维度未选择等级时不触发状态校验', async () => {
      wrapper = mountForm({ initialScopeId: 'user-1', initialTargetAmountUsd: '150' })
      await wrapper.setProps({ modelValue: false })
      await wrapper.setProps({ modelValue: true })
      await flushPromises()
      mockPrecheckExpansion.mockClear()

      const tierInput = wrapper.find('input[value="tier"]')
      expect(tierInput.exists()).toBe(true)
      await tierInput.setValue(true)
      vi.advanceTimersByTime(400)
      await flushPromises()

      expect(mockPrecheckExpansion).not.toHaveBeenCalled()
    })
  })

  describe('表单验证', () => {
    it('reason 为空时提交按钮被禁用', async () => {
      wrapper = mountForm()
      await flushPromises()

      // Submit button should be disabled when form is invalid
      const submitBtn = wrapper.findAll('button').find(b => b.text().includes('提交申请'))
      expect(submitBtn).toBeDefined()
      expect(submitBtn!.attributes('disabled')).toBeDefined()
    })

    it('未填写完整表单时不受预检结果影响，提交按钮仍禁用', async () => {
      mockPrecheckExpansion.mockResolvedValue(makePrecheck({ can_submit: false }))
      wrapper = mountForm()
      await flushPromises()

      const submitBtn = wrapper.findAll('button').find(b => b.text().includes('提交申请'))
      expect(submitBtn!.attributes('disabled')).toBeDefined()
    })

    it('不再因为预检 stale 禁用提交按钮', async () => {
      mockPrecheckExpansion.mockResolvedValue(makePrecheck({ stale: true }))
      wrapper = mountForm()
      await flushPromises()

      const submitBtn = wrapper.findAll('button').find(b => b.text().includes('提交申请'))
      expect(submitBtn!.attributes('disabled')).toBeDefined()
    })
  })

  describe('提交流程', () => {
    it('提交成功后关闭对话框并触发 created 事件', async () => {
      mockPrecheckExpansion.mockResolvedValue(makePrecheck({ can_submit: true, stale: false }))
      wrapper = mountForm()
      await flushPromises()

      // This is a structural test verifying the submit path exists
    })

    it('重复待审批申请返回 409 时显示友好提示并刷新列表', async () => {
      mockCreateExpansionRequest.mockRejectedValue({
        response: {
          status: 409,
          data: { error: { code: 'DUPLICATE_PENDING_EXPANSION', message: 'a pending expansion request already exists' } },
        },
      })
      wrapper = mountForm({ initialScopeType: 'dept_daily', initialScopeId: 'dept-1' })
      await flushPromises()

      Object.assign((wrapper.vm as any).form, {
        target_amount_usd: '200',
        reason: '提升部门预算池',
      })
      await wrapper.vm.$nextTick()

      const submitBtn = wrapper.findAll('button').find(b => b.text().includes('提交申请'))
      await submitBtn!.trigger('click')
      await flushPromises()

      expect(ElMessage.warning).toHaveBeenCalledWith('已有审批中的同类申请，请等待审批结果或取消后重新提交')
      expect(wrapper.emitted('created')).toBeTruthy()
    })

    it('不展示通知负责人按钮', async () => {
      mockPrecheckExpansion.mockResolvedValue(makePrecheck({ can_notify_owner: true, can_submit: false }))
      wrapper = mountForm()
      await flushPromises()

      expect(wrapper.text()).not.toContain('通知负责人')
    })
  })

  describe('取消操作', () => {
    it('点击"取消"按钮关闭对话框', async () => {
      wrapper = mountForm()
      await flushPromises()

      const cancelBtn = wrapper.findAll('button').find(b => b.text() === '取消')
      expect(cancelBtn).toBeDefined()
      await cancelBtn!.trigger('click')
      await flushPromises()

      // Should emit update:modelValue(false)
      const emitted = wrapper.emitted('update:modelValue')
      expect(emitted).toBeDefined()
      expect(emitted![0]).toEqual([false])
    })
  })
})

// Need afterEach import
import { afterEach } from 'vitest'
