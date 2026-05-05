import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import BudgetEditDrawer from '../BudgetEditDrawer.vue'
import type { BudgetItem } from '@/types/budget'
import type { ModelTier, TierAdjustmentPreview } from '@/types/model-tier'

const mockGetBudgetList = vi.fn()
const mockListModelTiers = vi.fn()
const mockListBudgetTierLimits = vi.fn()
const mockGetBudgetTierUsage = vi.fn()
const mockPreviewBudgetAdjustment = vi.fn()
const mockAdjustBudget = vi.fn()
const mockPreviewBudgetTierBatch = vi.fn()
const mockApplyBudgetTierBatch = vi.fn()
const mockMessageSuccess = vi.fn()
const mockMessageError = vi.fn()
const mockMessageWarning = vi.fn()
const mockConfirm = vi.fn()

vi.mock('@/services/control-plane', () => ({
  getBudgetList: (...args: unknown[]) => mockGetBudgetList(...args),
  listModelTiers: (...args: unknown[]) => mockListModelTiers(...args),
  listBudgetTierLimits: (...args: unknown[]) => mockListBudgetTierLimits(...args),
  getBudgetTierUsage: (...args: unknown[]) => mockGetBudgetTierUsage(...args),
  previewBudgetAdjustment: (...args: unknown[]) => mockPreviewBudgetAdjustment(...args),
  adjustBudget: (...args: unknown[]) => mockAdjustBudget(...args),
  previewBudgetTierBatch: (...args: unknown[]) => mockPreviewBudgetTierBatch(...args),
  applyBudgetTierBatch: (...args: unknown[]) => mockApplyBudgetTierBatch(...args),
}))

vi.mock('element-plus', async () => {
  const actual = await vi.importActual<typeof import('element-plus')>('element-plus')
  return {
    ...actual,
    ElMessage: {
      success: (...args: unknown[]) => mockMessageSuccess(...args),
      error: (...args: unknown[]) => mockMessageError(...args),
      warning: (...args: unknown[]) => mockMessageWarning(...args),
      info: vi.fn(),
    },
    ElMessageBox: {
      confirm: (...args: unknown[]) => mockConfirm(...args),
    },
  }
})

const studioAllocation: BudgetItem = {
  allocation_id: 'alloc-studio',
  scope_type: 'studio',
  scope_id: 'studio-1',
  label: '效率平台',
  amount_usd: 5000,
  daily_amount_usd: -1,
  weekly_amount_usd: -1,
  period: 'monthly',
  enabled: true,
  used_usd: 1000,
  used_ratio: 0.2,
}

const deptAllocation: BudgetItem = {
  allocation_id: 'alloc-dept',
  scope_type: 'dept_daily',
  scope_id: 'dept-1',
  label: 'AI 研发部',
  amount_usd: 1000,
  daily_amount_usd: -1,
  weekly_amount_usd: -1,
  period: 'monthly',
  enabled: true,
  used_usd: 200,
  used_ratio: 0.2,
}

const serviceAllocation: BudgetItem = {
  ...deptAllocation,
  allocation_id: 'alloc-dept-service',
  scope_type: 'dept_service',
  label: 'AI 研发部 · Service Key 池',
  amount_usd: 200,
}

const tiers: ModelTier[] = [{
  id: 'tier-premium',
  name: '高级',
  description: '高能力模型',
  color: '#8b5cf6',
  sort_order: 1,
  enabled: true,
  archived_at: null,
  group_count: 3,
  budget_count: 0,
  history_count: 0,
  reference_count: 3,
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-01T00:00:00Z',
}]

function mountDrawer() {
  return mount(BudgetEditDrawer, {
    props: {
      modelValue: true,
      scopeType: 'studio',
      scopeId: 'studio-1',
      title: '效率平台',
      canEdit: true,
    },
    global: { plugins: [ElementPlus] },
    attachTo: document.body,
  })
}

function mountDeptServiceDrawer() {
  return mount(BudgetEditDrawer, {
    props: {
      modelValue: true,
      scopeType: 'dept',
      scopeId: 'dept-1',
      title: 'AI 研发部',
      canEdit: true,
      initialAllocation: serviceAllocation,
    },
    global: { plugins: [ElementPlus] },
    attachTo: document.body,
  })
}

function validTierPreview(itemID = 'alloc-studio:tier-premium:monthly'): TierAdjustmentPreview {
  return {
    preview_token: 'preview-token',
    expires_at: '2026-07-15T00:05:00Z',
    data_as_of: '2026-07-15T00:00:00Z',
    stale: false,
    items: [{
      item_id: itemID,
      old_limit_usd: undefined,
      new_limit_usd: '120.000000',
      current_used_usd: '0.000000',
      below_used: false,
      zero_block: false,
      stale: false,
      status: 'valid',
    }],
  }
}

describe('BudgetEditDrawer', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockGetBudgetList.mockResolvedValue([deptAllocation, studioAllocation])
    mockListModelTiers.mockResolvedValue(tiers)
    mockListBudgetTierLimits.mockResolvedValue([])
    mockGetBudgetTierUsage.mockResolvedValue([])
    mockPreviewBudgetAdjustment.mockResolvedValue({ stale: false, zero_block: false, below_used: false, replaces_pending_schedule: false, preview_version: 'v-total' })
    mockAdjustBudget.mockResolvedValue({})
    mockPreviewBudgetTierBatch.mockResolvedValue(validTierPreview())
    mockApplyBudgetTierBatch.mockResolvedValue({ items: [{ item_id: 'alloc-studio:tier-premium:monthly', status: 'applied' }] })
    mockConfirm.mockResolvedValue(true)
  })

  it('事业部抽屉保存等级子预算时使用事业部 allocation_id', async () => {
    const wrapper = mountDrawer()
    await flushPromises()

    const vm = wrapper.vm as any
    vm.draft.tiers['tier-premium'].monthly = { enabled: true, configured: true, value: '120' }
    vm.dirty = true
    await vm.save()
    await flushPromises()

    expect(mockPreviewBudgetTierBatch).toHaveBeenCalledWith(expect.objectContaining({
      operation: 'set_fixed',
      items: [expect.objectContaining({
        item_id: 'alloc-studio:tier-premium:monthly',
        allocation_id: 'alloc-studio',
        model_tier_id: 'tier-premium',
        period_type: 'monthly',
        value: '120.000000',
      })],
    }))
    expect(mockApplyBudgetTierBatch).toHaveBeenCalledWith({
      preview_token: 'preview-token',
      confirm_item_ids: ['alloc-studio:tier-premium:monthly'],
    })
    expect(mockMessageSuccess).toHaveBeenCalledWith('总预算与等级子预算已更新')
  })

  it('打开抽屉按预算主体 scope 查询日周月等级已用，避免 allocation_id 查不到实时用量', async () => {
    mountDeptServiceDrawer()
    await flushPromises()

    expect(mockGetBudgetTierUsage).toHaveBeenCalledWith({
      scope_type: 'dept_service',
      scope_id: 'dept-1',
      period_type: 'monthly',
    })
    expect(mockGetBudgetTierUsage).toHaveBeenCalledWith({
      scope_type: 'dept_service',
      scope_id: 'dept-1',
      period_type: 'weekly',
    })
    expect(mockGetBudgetTierUsage).toHaveBeenCalledWith({
      scope_type: 'dept_service',
      scope_id: 'dept-1',
      period_type: 'daily',
    })
  })

  it('总预算移除限制时映射为 -1 并提交', async () => {
    mockListBudgetTierLimits.mockResolvedValue([])
    const wrapper = mountDrawer()
    await flushPromises()

    const vm = wrapper.vm as any
    vm.originalDraft.total.weekly = { configured: true, value: '100' }
    vm.draft.total.weekly = { configured: false, value: '' }
    vm.dirty = true
    await vm.save()
    await flushPromises()

    expect(mockPreviewBudgetAdjustment).toHaveBeenCalledWith(expect.objectContaining({
      period_type: 'weekly',
      target_limit_usd: '-1.000000',
    }))
  })

  it('移除等级限制时走 clear 批量预览与应用', async () => {
    mockListBudgetTierLimits.mockResolvedValue([{
      id: 'limit-premium',
      budget_allocation_id: 'alloc-studio',
      model_tier_id: 'tier-premium',
      model_tier_name: '高级',
      period_type: 'monthly',
      base_limit_usd: '80.000000',
      effective_limit_usd: '80.000000',
      enabled: true,
    }])
    mockPreviewBudgetTierBatch.mockResolvedValue({
      preview_token: 'clear-token',
      stale: false,
      items: [{ item_id: 'alloc-studio:tier-premium:monthly:clear', status: 'valid' }],
    } as TierAdjustmentPreview)
    mockApplyBudgetTierBatch.mockResolvedValue({
      items: [{ item_id: 'alloc-studio:tier-premium:monthly:clear', status: 'applied' }],
    })

    const wrapper = mountDrawer()
    await flushPromises()

    const vm = wrapper.vm as any
    expect(vm.draft.tiers['tier-premium'].monthly).toMatchObject({ configured: true, value: '80' })
    vm.draft.tiers['tier-premium'].monthly = { enabled: true, configured: false, value: '' }
    vm.dirty = true
    await vm.save()
    await flushPromises()

    expect(mockPreviewBudgetTierBatch).toHaveBeenCalledWith(expect.objectContaining({
      operation: 'clear',
      effective_mode: 'immediate',
      items: [expect.objectContaining({
        item_id: 'alloc-studio:tier-premium:monthly:clear',
        allocation_id: 'alloc-studio',
        model_tier_id: 'tier-premium',
        period_type: 'monthly',
      })],
    }))
    expect(mockApplyBudgetTierBatch).toHaveBeenCalledWith({
      preview_token: 'clear-token',
      confirm_item_ids: ['alloc-studio:tier-premium:monthly:clear'],
    })
    expect(mockMessageSuccess).toHaveBeenCalledWith('总预算与等级子预算已更新')
  })

  it('等级子预算批量应用返回单项失败时不提示成功', async () => {
    mockApplyBudgetTierBatch.mockResolvedValue({
      items: [{ item_id: 'alloc-studio:tier-premium:monthly', status: 'failed', error_code: 'ORG_FORBIDDEN' }],
    })
    const wrapper = mountDrawer()
    await flushPromises()

    const vm = wrapper.vm as any
    vm.draft.tiers['tier-premium'].monthly = { enabled: true, configured: true, value: '120' }
    vm.dirty = true
    await vm.save()
    await flushPromises()

    expect(mockMessageSuccess).not.toHaveBeenCalled()
    expect(mockMessageError).toHaveBeenCalledWith('ORG_FORBIDDEN')
  })

  it('兼容列表接口返回 base_limit_usd 字段并正确识别变更', async () => {
    mockListBudgetTierLimits.mockResolvedValue([{
      id: 'limit-premium',
      budget_allocation_id: 'alloc-studio',
      model_tier_id: 'tier-premium',
      model_tier_name: '高级',
      period_type: 'monthly',
      base_limit_usd: '80.000000',
      effective_limit_usd: '80.000000',
      enabled: true,
    }])
    const wrapper = mountDrawer()
    await flushPromises()

    const vm = wrapper.vm as any
    expect(vm.draft.tiers['tier-premium'].monthly).toMatchObject({ configured: true, value: '80' })

    vm.draft.tiers['tier-premium'].monthly = { enabled: true, configured: true, value: '120' }
    vm.dirty = true
    await vm.save()
    await flushPromises()

    expect(mockPreviewBudgetTierBatch).toHaveBeenCalledWith(expect.objectContaining({
      items: [expect.objectContaining({
        allocation_id: 'alloc-studio',
        model_tier_id: 'tier-premium',
        value: '120.000000',
      })],
    }))
  })

  it('Service Key 池配置优先回显 initialAllocation 而不是成员日常池', async () => {
    mockGetBudgetList.mockResolvedValue([deptAllocation, serviceAllocation])
    mockApplyBudgetTierBatch.mockResolvedValue({ items: [{ item_id: 'alloc-dept-service:tier-premium:monthly', status: 'applied' }] })
    mockPreviewBudgetTierBatch.mockResolvedValue(validTierPreview('alloc-dept-service:tier-premium:monthly'))
    const wrapper = mountDeptServiceDrawer()
    await flushPromises()

    const vm = wrapper.vm as any
    expect(vm.selectedAllocation.allocation_id).toBe('alloc-dept-service')
    vm.draft.tiers['tier-premium'].monthly = { enabled: true, configured: true, value: '50' }
    vm.dirty = true
    await vm.save()
    await flushPromises()

    expect(mockPreviewBudgetTierBatch).toHaveBeenCalledWith(expect.objectContaining({
      items: [expect.objectContaining({
        allocation_id: 'alloc-dept-service',
        item_id: 'alloc-dept-service:tier-premium:monthly',
      })],
    }))
  })

  it('同时修改总预算和等级子预算时先应用等级子预算，再串行应用总预算', async () => {
    const order: string[] = []
    mockApplyBudgetTierBatch.mockImplementation(async () => {
      order.push('tier-apply')
      return { items: [{ item_id: 'alloc-studio:tier-premium:monthly', status: 'applied' }] }
    })
    mockAdjustBudget.mockImplementation(async (payload) => {
      order.push(`total-${payload.period_type}`)
      return {}
    })
    const wrapper = mountDrawer()
    await flushPromises()

    const vm = wrapper.vm as any
    vm.draft.total.monthly = { configured: true, value: '1200' }
    vm.draft.total.weekly = { configured: true, value: '300' }
    vm.draft.tiers['tier-premium'].monthly = { enabled: true, configured: true, value: '120' }
    vm.dirty = true
    await vm.save()
    await flushPromises()

    expect(order).toEqual(['tier-apply', 'total-weekly', 'total-monthly'])
  })

  it('新建成员额度时先保存总预算再写等级子预算', async () => {
    const memberSeed: BudgetItem = {
      scope_type: 'member',
      scope_id: 'user-1',
      parent_scope_id: 'dept-1',
      label: '张三',
      amount_usd: 0,
      weekly_amount_usd: -1,
      daily_amount_usd: -1,
      period: '',
      enabled: true,
      used_usd: 0,
      used_ratio: 0,
    }
    const memberCreated: BudgetItem = {
      ...memberSeed,
      allocation_id: 'alloc-member-1',
      amount_usd: 200,
    }

    mockGetBudgetList
      .mockResolvedValueOnce([deptAllocation])
      .mockResolvedValueOnce([deptAllocation, memberCreated])
    mockListBudgetTierLimits.mockResolvedValue([])
    mockGetBudgetTierUsage.mockResolvedValue([])
    mockPreviewBudgetAdjustment.mockResolvedValue({
      preview_version: 'v1',
      stale: false,
      zero_block: false,
      below_used: false,
      replaces_pending_schedule: false,
    })
    mockPreviewBudgetTierBatch.mockResolvedValue(validTierPreview('alloc-member-1:tier-premium:monthly'))
    mockApplyBudgetTierBatch.mockResolvedValue({
      items: [{ item_id: 'alloc-member-1:tier-premium:monthly', status: 'applied' }],
    })

    const order: string[] = []
    mockAdjustBudget.mockImplementation(async () => {
      order.push('total')
      return {}
    })
    mockApplyBudgetTierBatch.mockImplementation(async () => {
      order.push('tier')
      return { items: [{ item_id: 'alloc-member-1:tier-premium:monthly', status: 'applied' }] }
    })

    const wrapper = mount(BudgetEditDrawer, {
      props: {
        modelValue: true,
        scopeType: 'dept',
        scopeId: 'dept-1',
        title: '张三',
        canEdit: true,
        initialAllocation: memberSeed,
      },
      global: { plugins: [ElementPlus] },
      attachTo: document.body,
    })
    await flushPromises()

    const vm = wrapper.vm as any
    expect(vm.selectedAllocation.scope_type).toBe('member')
    expect(wrapper.text()).toContain('成员额度（含等级子预算）')

    vm.draft.total.monthly = { configured: true, value: '200' }
    vm.draft.tiers['tier-premium'].monthly = { enabled: true, configured: true, value: '80' }
    vm.dirty = true
    await vm.save()
    await flushPromises()

    expect(order).toEqual(['total', 'tier'])
    expect(mockAdjustBudget).toHaveBeenCalledWith(expect.objectContaining({
      scope_type: 'member',
      scope_id: 'user-1',
      period_type: 'monthly',
    }))
    expect(mockPreviewBudgetTierBatch).toHaveBeenCalledWith(expect.objectContaining({
      items: [expect.objectContaining({
        allocation_id: 'alloc-member-1',
        item_id: 'alloc-member-1:tier-premium:monthly',
      })],
    }))
  })
})
